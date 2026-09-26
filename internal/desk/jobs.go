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
	s.jobs = &jobsCompanion{bin: s.cfg.RunnerBin, runtime: s.cfg.JpackBin, dir: filepath.Join(s.configDir, "jobs", digestOf([]byte(s.projectDir))), workspace: digestOf([]byte(s.projectDir)), owner: "local-owner:" + digestOf([]byte(s.configDir)), stop: make(chan struct{})}
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
	boot := map[string]string{"dir": j.dir, "runtime": j.runtime, "workspace": j.workspace, "owner": j.owner, "token": j.token}
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

var jobsPath = regexp.MustCompile(`^(status|previews|inputs/preview|jobs|jobs/job_[a-f0-9]{32}|jobs/job_[a-f0-9]{32}/runs|runs/run_[a-f0-9]{32}|jobs/job_[a-f0-9]{32}/briefs|runs/run_[a-f0-9]{32}/briefs)$`)

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, "/api/operations/")
	if !jobsPath.MatchString(tail) || (r.Method != http.MethodGet && r.Method != http.MethodPost) {
		writeJSONCoded(w, 404, CodeBadRequest, "Unknown Jobs operation.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
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
	query := url.Values{}
	if after := r.URL.Query().Get("after"); after != "" {
		query.Set("after", after)
	}
	request.URL.RawQuery = query.Encode()
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
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
	body, err := readBounded(response.Body, 16<<20)
	if err != nil {
		writeJSONCoded(w, 502, CodeBadRequest, "The runner response exceeded its limit.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)
	w.Write(body)
}
