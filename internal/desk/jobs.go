package desk

// Jobs execute in an independently versioned companion. The chassis forwards
// only this bounded contract; it does not interpret packs or own dispatch.
import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

func InstalledRunnerBinary() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	name := "jpack-runner"
	if strings.HasSuffix(executable, ".exe") {
		name += ".exe"
	}
	path := filepath.Join(filepath.Dir(executable), name)
	if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
		return path
	}
	return ""
}

type jobsCompanion struct {
	mu                                  sync.Mutex
	profiles                            json.RawMessage
	connections                         json.RawMessage
	inputRoot                           string
	requireTested                       bool
	bin, runtime, dir, workspace, owner string
	cmd                                 *exec.Cmd
	input                               io.WriteCloser
	done                                chan struct{}
	url, token                          string
	closed                              bool
	stop                                chan struct{}
}

func (s *Server) initJobs() {
	if s.cfg.RunnerBin == "" || !s.assistant.usable() {
		return
	}
	connections, err := runnerConnectionsForDesk(s.cfg.RunnerConnections, s.cfg.deskID, s.cfg.parent == nil)
	if err != nil {
		s.log.Printf("desk: Jobs background connections are invalid: %v", err)
		return
	}
	s.jobs = &jobsCompanion{connections: connections, inputRoot: s.projectDir, requireTested: s.cfg.RunnerRequireTested, profiles: append(json.RawMessage(nil), s.cfg.RunnerInputProfiles...), bin: s.cfg.RunnerBin, runtime: s.cfg.JpackBin, dir: filepath.Join(s.configDir, "jobs", digestOf([]byte(s.projectDir))), workspace: digestOf([]byte(s.projectDir)), owner: "local-owner:" + digestOf([]byte(s.configDir)), stop: make(chan struct{})}
	if s.cfg.deskID != "" {
		s.jobs.dir = filepath.Join(s.projectDir, ".desk-private", "jobs")
		s.jobs.workspace = s.cfg.deskID
	}
	// Resume durable queued work when Desk starts, without requiring an open tab.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			_, _, _ = s.jobs.endpoint()
			select {
			case <-s.jobs.stop:
				return
			case <-ticker.C:
			}
		}
	}()
}
func (j *jobsCompanion) endpoint() (string, string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return "", "", errors.New("runner closed")
	}
	if j.done != nil {
		select {
		case <-j.done:
			j.url = ""
			j.input.Close()
			j.input = nil
			j.done = nil
		default:
			return j.url, j.token, nil
		}
	}
	if !filepath.IsAbs(j.bin) || !filepath.IsAbs(j.runtime) {
		return "", "", errors.New("absolute runner and Runtime paths required")
	}
	if err := os.MkdirAll(j.dir, 0700); err != nil {
		return "", "", err
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", "", err
	}
	j.token = hex.EncodeToString(secret[:])
	cmd := exec.Command(j.bin)
	cmd.Env = []string{"LANG=C", "LC_ALL=C"}
	cmd.Dir = j.dir
	cmd.Stderr = io.Discard
	input, err := cmd.StdinPipe()
	if err != nil {
		return "", "", err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return "", "", err
	}
	if err = cmd.Start(); err != nil {
		input.Close()
		return "", "", err
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	boot := map[string]any{"dir": j.dir, "runtime": j.runtime, "workspace": j.workspace, "owner": j.owner, "token": j.token, "inputRoot": j.inputRoot}
	if len(j.connections) > 0 {
		var connections struct {
			Cloud   json.RawMessage `json:"cloud"`
			Gateway json.RawMessage `json:"gateway"`
		}
		if err = json.Unmarshal(j.connections, &connections); err != nil {
			input.Close()
			cmd.Process.Kill()
			<-done
			return "", "", errors.New("invalid background connections")
		}
		if len(connections.Cloud) > 0 {
			boot["cloudConnections"] = connections.Cloud
		}
		if len(connections.Gateway) > 0 {
			boot["gatewayConnections"] = connections.Gateway
		}
	}
	if len(j.profiles) > 0 {
		boot["inputProfiles"] = j.profiles
	}
	// Off is the Runner's default, so an installation without the policy
	// sends the same boot line as before.
	if j.requireTested {
		boot["requireTestedReleases"] = true
	}
	if err = json.NewEncoder(input).Encode(boot); err != nil {
		input.Close()
		cmd.Process.Kill()
		<-done
		return "", "", err
	}
	handshake := make(chan []byte, 1)
	go func() {
		reader := bufio.NewReader(io.LimitReader(output, 4096))
		line, _ := reader.ReadBytes('\n')
		handshake <- line
	}()
	var hello struct {
		URL      string `json:"url"`
		Protocol string `json:"protocol"`
	}
	select {
	case line := <-handshake:
		err = json.Unmarshal(line, &hello)
	case <-time.After(15 * time.Second):
		err = errors.New("runner startup timeout")
	}
	u, parseErr := url.Parse(hello.URL)
	if err != nil || parseErr != nil || hello.Protocol != "jobs/1" || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		input.Close()
		cmd.Process.Kill()
		<-done
		return "", "", errors.New("runner unavailable")
	}
	j.cmd, j.input, j.done, j.url = cmd, input, done, hello.URL
	return j.url, j.token, nil
}
func (j *jobsCompanion) close() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return
	}
	j.closed = true
	close(j.stop)
	if j.input != nil {
		j.input.Close()
	}
	if j.done != nil {
		select {
		case <-j.done:
		case <-time.After(5 * time.Second):
			j.cmd.Process.Kill()
			<-j.done
		}
	}
}

var jobsPath = regexp.MustCompile(`^(occurrences/occ_[a-f0-9]{32}/(cancel|reconcile)|status|background-connections|input-profiles|previews|inputs/preview|inputs/next|jobs|runs|jobs/job_[a-f0-9]{32}|jobs/job_[a-f0-9]{32}/runs|runs/run_[a-f0-9]{32}|jobs/job_[a-f0-9]{32}/triggers|jobs/job_[a-f0-9]{32}/triggers/preview|jobs/job_[a-f0-9]{32}/occurrences|triggers/trg_[a-f0-9]{32}/state|triggers/trg_[a-f0-9]{32}/rotate-key|runs/run_[a-f0-9]{32}/verification|jobs/job_[a-f0-9]{32}/briefs|runs/run_[a-f0-9]{32}/briefs)$`)

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, "/api/operations/")
	if !jobsPath.MatchString(tail) || (r.Method != http.MethodGet && r.Method != http.MethodPost) {
		writeJSONCoded(w, 404, CodeBadRequest, "Unknown Jobs operation.")
		return
	}
	s.proxyJobs(w, r, tail, "")
}

var (
	triggerIDPattern    = regexp.MustCompile(`^trg_[a-f0-9]{32}$`)
	occurrenceIDPattern = regexp.MustCompile(`^occ_[a-f0-9]{32}$`)
	eventTokenPattern   = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func (s *Server) handleJobEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("trigger")
	auth := r.Header.Get("Authorization")
	if r.Method != http.MethodPost || r.Header.Get("Origin") != "" || !triggerIDPattern.MatchString(id) || !strings.HasPrefix(auth, "Bearer ") || len(strings.TrimPrefix(auth, "Bearer ")) != 64 {
		writeJSONCoded(w, 401, CodeBadRequest, "A trigger-scoped event credential is required.")
		return
	}
	s.proxyJobs(w, r, "triggers/"+id+"/events", strings.TrimPrefix(auth, "Bearer "))
}

// handleJobEventResult is the one read a trigger token has: what became of an
// occurrence that token created. Desk decides on the parsed identifiers only.
// A request that is not exactly this shape (another method, a browser's
// marks, a query, even an empty one, a body, or anything but one bearer token)
// is refused, never trimmed into it. The Runner decides which occurrence the
// token may read; its 401 and 404 answers pass through unchanged, and no
// answer of Desk's own depends on whether an occurrence exists.
func (s *Server) handleJobEventResult(w http.ResponseWriter, r *http.Request) {
	trigger, occurrence := r.PathValue("trigger"), r.PathValue("occurrence")
	auth := r.Header.Values("Authorization")
	if r.Method != http.MethodGet || browserMarked(r) || r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || !triggerIDPattern.MatchString(trigger) || !occurrenceIDPattern.MatchString(occurrence) || len(auth) != 1 || !strings.HasPrefix(auth[0], "Bearer ") || !eventTokenPattern.MatchString(strings.TrimPrefix(auth[0], "Bearer ")) {
		writeJSONCoded(w, 401, CodeBadRequest, "A trigger-scoped event credential is required.")
		return
	}
	// A fresh request carries nothing of the caller's but its context: the
	// Runner path is built from the parsed identifiers, and no caller path,
	// query, body or header reaches it except the token.
	read, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "/", http.NoBody)
	if err != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid Jobs request.")
		return
	}
	s.proxyJobs(w, read, "triggers/"+trigger+"/occurrences/"+occurrence, strings.TrimPrefix(auth[0], "Bearer "))
}

// browserMarked reports what a browser adds and a script cannot: an Origin,
// sent on every cross-origin request, or fetch metadata, which current
// browsers send on every request, a same-origin GET without an Origin included.
func browserMarked(r *http.Request) bool {
	for name := range r.Header {
		if name == "Origin" || strings.HasPrefix(name, "Sec-Fetch-") {
			return true
		}
	}
	return false
}

var (
	runPath          = regexp.MustCompile(`^runs/run_[a-f0-9]{32}$`)
	verificationPath = regexp.MustCompile(`^runs/run_[a-f0-9]{32}/verification$`)
)

// runnerAnswerLimit is the most of a Runner answer Desk reads on most routes.
const runnerAnswerLimit = 16 << 20

// runnerExportLimit is Runner's MaxExportSize (v0.4.0, internal/runner/
// audit_bytes.go), the most of a verification export its verify-run reads:
// version 2's 8 MiB, and version 3's member carrying the audit record's bytes,
// at most 8 MiB of them in base64. Desk saves the answer as it comes, so an
// answer within this limit is a file that verify-run reads, by size, and a
// larger one is not. A run is a member of its own version-3 export: it carries
// the record twice, parsed and as those bytes, so it too can pass
// runnerAnswerLimit, but never its export's size. It is read to the same
// limit, since the download is on the run's page, which a run Desk refused
// to read would leave out of reach.
const runnerExportLimit = 8<<20 + len(`,"auditBytes":""`) + (8<<20+2)/3*4

// verificationVersion is the export version a request on a run's verification
// route asks Runner for: "" for none, which Runner answers with version 2, or
// exactly one "2" or "3". Anything else is refused rather than forwarded, as
// Runner refuses it. The query is parsed strictly, since a lenient reader
// keeps one of repeated values and drops malformed pairs, and would forward a
// version that the request did not ask for alone.
func verificationVersion(rawQuery string) (string, error) {
	query, err := url.ParseQuery(rawQuery)
	switch version, asked := query["version"]; {
	case err != nil:
		// Refused below.
	case !asked:
		return "", nil
	case len(version) == 1 && (version[0] == "2" || version[0] == "3"):
		return version[0], nil
	}
	return "", errors.New("invalid verification export version")
}

func (s *Server) proxyJobs(w http.ResponseWriter, r *http.Request, tail, eventToken string) {
	w.Header().Set("Cache-Control", "no-store")
	query := url.Values{}
	for _, key := range []string{"after", "q", "state", "review", "preparations"} {
		if value := r.URL.Query().Get(key); value != "" {
			query.Set(key, value)
		}
	}
	limit := runnerAnswerLimit
	switch {
	case verificationPath.MatchString(tail):
		version, err := verificationVersion(r.URL.RawQuery)
		if err != nil {
			writeJSONCoded(w, 400, CodeBadRequest, "Ask once for verification export version 2 or 3, in a well-formed query.")
			return
		}
		if version != "" {
			query.Set("version", version)
		}
		limit = runnerExportLimit
	case runPath.MatchString(tail):
		limit = runnerExportLimit
	}
	if s.jobs == nil {
		writeJSONCoded(w, 503, CodeBadRequest, "Jobs requires the local runner companion. Install jpack-runner beside Desk or start Desk with --runner.")
		return
	}
	endpoint, token, err := s.jobs.endpoint()
	if err != nil {
		writeJSONCoded(w, 503, CodeBadRequest, "The local runner is unavailable. Check its installation and private state directory.")
		return
	}
	data, err := readBounded(r.Body, 2<<20)
	if err != nil {
		writeJSONCoded(w, 413, CodeTooLarge, "Jobs requests are limited to 2 MiB.")
		return
	}
	request, err := http.NewRequestWithContext(r.Context(), r.Method, endpoint+"/v1/"+tail, bytes.NewReader(data))
	if err != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid Jobs request.")
		return
	}
	request.URL.RawQuery = query.Encode()
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	if eventToken != "" {
		request.Header.Set("X-Trigger-Token", eventToken)
	}
	request.Header.Set("Idempotency-Key", r.Header.Get("Idempotency-Key"))
	// Release checks perform four bounded Runtime invocations in sequence.
	timeout := 40 * time.Second
	if tail == "previews" {
		timeout = 130 * time.Second
	}
	client := http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		writeJSONCoded(w, 503, CodeBadRequest, "The runner did not respond. Retry a run submission with the same idempotency key.")
		return
	}
	defer response.Body.Close()
	body, err := readBounded(response.Body, limit)
	if err != nil {
		writeJSONCoded(w, 502, CodeBadRequest, "The runner response exceeded its limit.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)
	w.Write(body)
}

// LoadRunnerInputProfiles reads a file explicitly chosen by the installation
// owner. The Runner validates the profile schema and trust conflicts at boot.
func LoadRunnerInputProfiles(path string) (json.RawMessage, error) {
	if path == "" {
		return nil, nil
	}
	if !filepath.IsAbs(path) {
		return nil, errors.New("runner input profiles must use an absolute installation-owned path")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := readBounded(f, 48<<10)
	var profiles []json.RawMessage
	if err != nil || json.Unmarshal(data, &profiles) != nil || profiles == nil {
		return nil, errors.New("runner input profiles must be a JSON array up to 48 KiB")
	}
	return data, nil
}

// Only an installation-owned file can authorize unattended network connections.
// The browser sees connection IDs, subscription names and profile IDs only.
func LoadRunnerConnections(path string) (json.RawMessage, error) {
	if path == "" {
		return nil, nil
	}
	if !filepath.IsAbs(path) {
		return nil, errors.New("background connections need an absolute installation-owned path")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return nil, errors.New("background connections must be a regular file")
	}
	raw, e := readBounded(f, 16<<10)
	if e != nil {
		return nil, errors.New("background connections exceed 16 KiB")
	}
	if _, e = parseRunnerConnections(raw); e != nil {
		return nil, e
	}
	return raw, nil
}

// A pull subscription belongs to exactly one desk. Unscoped entries retain
// their original startup-desk meaning; new desks never become competing consumers.
type runnerCloudConnection struct {
	ID              string `json:"id"`
	Subscription    string `json:"subscription"`
	CredentialsFile string `json:"credentialsFile"`
	Desk            string `json:"desk,omitempty"`
}
type runnerConnectionConfig struct {
	Cloud   []runnerCloudConnection `json:"cloud,omitempty"`
	Gateway []json.RawMessage       `json:"gateway,omitempty"`
}

func parseRunnerConnections(raw []byte) (runnerConnectionConfig, error) {
	var config runnerConnectionConfig
	if decodeDataJSON(raw, &config) != nil || len(config.Cloud) > 8 || len(config.Gateway) > 32 || strings.TrimSpace(string(raw)) == "null" {
		return config, errors.New("invalid background connections configuration")
	}
	subscriptions := make(map[string]bool)
	for _, connection := range config.Cloud {
		if connection.Desk != "" && !deskIDPattern.MatchString(connection.Desk) {
			return config, errors.New("cloud connection desk must be a registered desk ID")
		}
		if connection.Subscription == "" || subscriptions[connection.Subscription] {
			return config, errors.New("each cloud subscription must belong to exactly one desk")
		}
		subscriptions[connection.Subscription] = true
	}
	return config, nil
}

func runnerConnectionsForDesk(raw json.RawMessage, id string, startup bool) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	config, err := parseRunnerConnections(raw)
	if err != nil {
		return nil, err
	}
	selected := runnerConnectionConfig{Gateway: config.Gateway}
	for _, connection := range config.Cloud {
		if connection.Desk == "" && !startup || connection.Desk != "" && connection.Desk != id {
			continue
		}
		// The selector is Desk's concern, not part of Runner's connection API.
		connection.Desk = ""
		selected.Cloud = append(selected.Cloud, connection)
	}
	return json.Marshal(selected)
}
