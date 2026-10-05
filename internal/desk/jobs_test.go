package desk

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestJobsUsesSessionAndOriginGuards(t *testing.T) {
	s, _ := newTestServer(t, false)
	for _, tc := range []struct {
		path, token, origin string
		want                int
	}{
		{"/api/operations/jobs", "", "", 401},
		{"/api/operations/runs", "", "", 401},
		{"/api/operations/runs", testToken, "https://elsewhere.example", 403},
		{"/api/operations/inputs/next", "", "", 401},
		{"/api/operations/input-profiles", "", "", 401},
		{"/api/operations/background-connections", "", "", 401},
		{"/api/operations/background-connections", testToken, "https://elsewhere.example", 403},
		{"/api/operations/inputs/next", testToken, "https://elsewhere.example", 403},
		{"/api/operations/runs/run_00000000000000000000000000000000/verification", "", "", 401},
		{"/api/operations/inputs/preview", "", "", 401},
		{"/api/operations/inputs/preview", testToken, "https://elsewhere.example", 403},
		{"/api/operations/inputs/preview", testToken, "", 503},
		{"/api/operations/jobs", testToken, "https://elsewhere.example", 403},
		{"/api/operations/unknown", testToken, "", 404},
		{"/api/operations/jobs", testToken, "", 503},
	} {
		r := httptest.NewRequest("GET", tc.path, nil)
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer "+tc.token)
		}
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s: %d %s", tc.path, w.Code, w.Body)
		}
	}
}
func TestJobsRealCompanionRecovery(t *testing.T) {
	bin := os.Getenv("JPACK_RUNNER_TEST_BIN")
	runtime := os.Getenv("JPACK_BIN")
	if bin == "" || runtime == "" {
		t.Skip("set JPACK_RUNNER_TEST_BIN and JPACK_BIN for companion integration")
	}
	config := t.TempDir()
	os.Chmod(config, 0700)
	s, ts := startDesk(t, Config{RunnerBin: bin, JpackBin: runtime, ProjectDir: t.TempDir(), DeskConfigDir: config, Token: testToken})
	defer ts.Close()
	defer s.Close()
	request := func(path string) int {
		r, _ := http.NewRequest("GET", ts.URL+path, nil)
		r.Header.Set("Authorization", "Bearer "+testToken)
		resp, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if code := request("/api/operations/status"); code != 200 {
		t.Fatal("companion unavailable", code)
	}
	s.jobs.mu.Lock()
	done := s.jobs.done
	s.jobs.cmd.Process.Kill()
	s.jobs.mu.Unlock()
	<-done
	if code := request("/api/operations/jobs"); code != 200 {
		t.Fatal("companion did not restart", code)
	}
	for _, path := range []string{"/api/operations/runs?q=Target&state=completed&review=true", "/api/operations/jobs?q=Target"} {
		if code := request(path); code != 200 {
			t.Fatal("list query unavailable", path, code)
		}
	}
	// Invalid filters must reach Runner validation, rather than being silently stripped.
	for _, path := range []string{"/api/operations/runs?state=invalid", "/api/operations/runs?review=invalid", "/api/operations/jobs?state=completed"} {
		if code := request(path); code != 400 {
			t.Fatal("list query was not forwarded", path, code)
		}
	}
	r := httptest.NewRequest("POST", "/api/operations/previews", strings.NewReader(`{"owner":"other"}`))
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("unsupported identity was accepted", w.Code, w.Body)
	}
}

func TestRunnerInputProfilesFile(t *testing.T) {
	if _, err := LoadRunnerInputProfiles("relative.json"); err == nil {
		t.Fatal("relative installation path accepted")
	}
	path := t.TempDir() + "/profiles.json"
	for _, body := range []string{`{}`, `null`, strings.Repeat(" ", 49<<10)} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadRunnerInputProfiles(path); err == nil {
			t.Fatal("invalid or oversized profiles accepted")
		}
	}
	if err := os.WriteFile(path, []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	if raw, err := LoadRunnerInputProfiles(path); err != nil || string(raw) != "[]" {
		t.Fatal(string(raw), err)
	}
}

func TestJobsV2PlannerAndProfilesThroughCompanion(t *testing.T) {
	bin, runtime := os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bin == "" || runtime == "" {
		t.Skip("requires isolated companion binaries")
	}
	config := t.TempDir()
	os.Chmod(config, 0700)
	profiles := `[{"id":"test","publicKey":"` + strings.Repeat("ab", 32) + `","class":"record","source":"records","authority":"test","shape":"mcp","adapter":{"name":"mcp","version":"1","digest":"sha256:` + strings.Repeat("a", 64) + `"},"endpoint":null,"tools":["lookup"]}]`
	s, ts := startDesk(t, Config{RunnerBin: bin, JpackBin: runtime, RunnerInputProfiles: []byte(profiles), ProjectDir: t.TempDir(), DeskConfigDir: config, Token: testToken})
	defer ts.Close()
	defer s.Close()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/operations/"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := call("GET", "input-profiles", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"test"`) || !strings.Contains(w.Body.String(), `"digest":"sha256:`) {
		t.Fatal(w.Code, w.Body)
	}
	w = call("POST", "inputs/next", `{"source":{"mapping":{"version":2,"case":{"facts":[{"target":"/score","source":"/score"}],"evidence":[]},"sources":[]},"case":{"score":7}}}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"facts":{"score":7}`) || !strings.Contains(w.Body.String(), `"class":"asserted"`) {
		t.Fatal(w.Code, w.Body)
	}
	w = call("POST", "inputs/next", `{"inputProfiles":[]}`)
	if w.Code != 400 {
		t.Fatal("browser trust override accepted", w.Code, w.Body)
	}
}

// recordedBootLine builds a desk from cfg with a recording Runner and returns
// the boot line Desk sends it. No socket is opened: New needs a port number,
// not a listener, and the Runner is reached over its standard input and output.
func recordedBootLine(t *testing.T, build func(Config) Config) map[string]json.RawMessage {
	t.Helper()
	dir := t.TempDir()
	received := filepath.Join(dir, "boot.json")
	runner := filepath.Join(dir, "runner")
	// Shell builtins only: Desk starts the Runner with an empty PATH.
	script := "#!/bin/sh\nIFS= read -r line\nprintf '%s\\n' \"$line\" > " + received + "\nprintf '{\"protocol\":\"jobs/1\",\"url\":\"http://127.0.0.1:9\"}\\n'\nwhile IFS= read -r _; do :; done\n"
	if err := os.WriteFile(runner, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	config := t.TempDir()
	os.Chmod(config, 0700)
	s, err := New(build(Config{RunnerBin: runner, JpackBin: filepath.Join(dir, "jpack"), ProjectDir: t.TempDir(), DeskConfigDir: config, Port: 1, Token: testToken}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if s.jobs == nil {
		t.Fatal("Jobs companion was not configured")
	}
	// The handshake follows the recorded line, so a returned endpoint means
	// the line is complete on disk.
	if _, _, err := s.jobs.endpoint(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(received)
	var boot map[string]json.RawMessage
	if err != nil || json.Unmarshal(raw, &boot) != nil {
		t.Fatalf("boot line was not one JSON object: %v %q", err, raw)
	}
	return boot
}

// From the command line to the Runner's boot line, through the step main uses:
// the policy is on unless the owner passes `=false`.
func TestJobsPolicyFlagsReachTheBootLine(t *testing.T) {
	for _, row := range []struct {
		args []string
		want string
	}{
		{nil, "true"},
		{[]string{"--runner-require-tested-releases"}, "true"},
		{[]string{"--runner-require-tested-releases=true"}, "true"},
		{[]string{"--runner-require-tested-releases=false"}, "false"},
		{[]string{"-runner-require-tested-releases=false"}, "false"},
	} {
		flags := flag.NewFlagSet("jpack-desk", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		policy := RegisterJobsPolicyFlags(flags)
		if err := flags.Parse(row.args); err != nil {
			t.Fatalf("%v: %v", row.args, err)
		}
		boot := recordedBootLine(t, policy.Apply)
		if got := string(boot["requireTestedReleases"]); got != row.want {
			t.Fatalf("%v: requireTestedReleases=%s, want %s", row.args, got, row.want)
		}
	}
}

// A Config built without the startup flags, by any caller or a main that lost
// its Apply, keeps the policy on: the field's zero value is the safe one.
func TestJobsPolicyIsOnWithoutTheStartupFlags(t *testing.T) {
	boot := recordedBootLine(t, func(cfg Config) Config { return cfg })
	if got := string(boot["requireTestedReleases"]); got != "true" {
		t.Fatalf("a Config without Apply booted the Runner with requireTestedReleases=%s", got)
	}
}

// The tested-release policy is the installation owner's startup choice. It
// reaches every desk's Runner boot line either way, so turning it off does not
// rest on the Runner's own default.
func TestRunnerRequireTestedReleasesReachesBootLine(t *testing.T) {
	for _, required := range []bool{true, false} {
		dir := t.TempDir()
		received := filepath.Join(dir, "boot.json")
		runner := filepath.Join(dir, "runner")
		// Shell builtins only: Desk starts the Runner with an empty PATH.
		script := "#!/bin/sh\nIFS= read -r line\nprintf '%s\\n' \"$line\" > " + received + "\nprintf '{\"protocol\":\"jobs/1\",\"url\":\"http://127.0.0.1:9\"}\\n'\nwhile IFS= read -r _; do :; done\n"
		if err := os.WriteFile(runner, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		config := t.TempDir()
		os.Chmod(config, 0700)
		// Creating the named desk below runs the runtime; this stand-in answers.
		writeStandInRuntime(t, filepath.Join(dir, "jpack"), reading(allConfigVersions), lockingAs(wantGatedConfig))
		s, ts := startDesk(t, Config{RunnerBin: runner, JpackBin: filepath.Join(dir, "jpack"), RunnerAllowUntestedReleases: !required, ProjectDir: t.TempDir(), DeskConfigDir: config, Token: testToken})
		t.Cleanup(func() { ts.Close(); s.Close() })
		// The handshake follows the recorded line, so a returned endpoint
		// means the line is complete on disk.
		booted := func(companion *jobsCompanion) map[string]json.RawMessage {
			t.Helper()
			if companion == nil {
				t.Fatal("Jobs companion was not configured")
			}
			if _, _, err := companion.endpoint(); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(received)
			var boot map[string]json.RawMessage
			if err != nil || json.Unmarshal(raw, &boot) != nil {
				t.Fatalf("boot line was not one JSON object: %v %q", err, raw)
			}
			if value, want := string(boot["requireTestedReleases"]), strconv.FormatBool(required); value != want {
				t.Fatalf("requireTestedReleases=%v produced %s", required, raw)
			}
			for _, key := range []string{"dir", "runtime", "workspace", "owner", "token", "inputRoot"} {
				if _, ok := boot[key]; !ok {
					t.Fatalf("boot line lost %s: %s", key, raw)
				}
			}
			return boot
		}
		booted(s.jobs)
		named := createTestDesk(t, ts, "Named")
		s.desksMu.Lock()
		child := s.desks[named.ID]
		s.desksMu.Unlock()
		if child == nil {
			t.Fatal("named desk is not open")
		}
		if boot := booted(child.jobs); string(boot["workspace"]) != `"`+named.ID+`"` {
			t.Fatal("named desk's Runner was not the one recorded", string(boot["workspace"]))
		}
	}
}

// End to end, with the pinned Runner and Runtime: with the policy on, Desk
// passes the Runner's release_untested refusal of a new job through; turned
// off with `=false`, the same kind of release becomes a job; and a job made
// while it was off keeps running once it is on.
func TestJobsRealCompanionTestedReleasePolicy(t *testing.T) {
	bin, runtime := os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bin == "" || runtime == "" {
		t.Skip("set JPACK_RUNNER_TEST_BIN and JPACK_BIN for companion integration")
	}
	config, project := t.TempDir(), t.TempDir()
	os.Chmod(config, 0700)
	pack, _ := json.Marshal(`{"specVersion":"0.2.0-draft","id":"https://example.invalid/judgment-packs/minimal","version":"0.1.0","title":"A minimal pack","decision":{"intent":"Decide the one thing this pack decides.","question":"Does this request proceed?"},"outcomes":[{"id":"proceed","label":"Proceed"},{"id":"hold","label":"Hold"}],"rules":[{"id":"always","description":"Proceed.","when":{"op":"literal","value":true},"outcome":"proceed","onUnknown":"escalate"}]}`)
	call := func(ts *httptest.Server, method, path, body string, header ...string) (int, []byte) {
		t.Helper()
		r, err := http.NewRequest(method, ts.URL+"/api/operations/"+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("Content-Type", "application/json")
		for i := 0; i+1 < len(header); i += 2 {
			r.Header.Set(header[i], header[i+1])
		}
		response, err := ts.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, data
	}
	untested := func(ts *httptest.Server) string {
		t.Helper()
		status, data := call(ts, "POST", "previews", `{"pack":`+string(pack)+`,"input":{"facts":{}}}`)
		var release struct{ ID, Tests string }
		if status != 201 || json.Unmarshal(data, &release) != nil || release.Tests != "not-run" || release.ID == "" {
			t.Fatalf("preview: %d %s", status, data)
		}
		return release.ID
	}
	createJob := func(ts *httptest.Server, release string) (int, []byte) {
		return call(ts, "POST", "jobs", `{"name":"Untested","releaseId":"`+release+`","reviewed":true}`)
	}

	off, offServer := startDesk(t, Config{RunnerBin: bin, JpackBin: runtime, RunnerAllowUntestedReleases: true, ProjectDir: project, DeskConfigDir: config, Token: testToken})
	earlier := untested(offServer)
	status, data := createJob(offServer, earlier)
	var job struct{ ID string }
	if status != 201 || json.Unmarshal(data, &job) != nil || job.ID == "" {
		t.Fatalf("policy off refused an untested release: %d %s", status, data)
	}
	offServer.Close()
	off.Close()

	// No policy field at all: the zero value keeps the policy on.
	on, onServer := startDesk(t, Config{RunnerBin: bin, JpackBin: runtime, ProjectDir: project, DeskConfigDir: config, Token: testToken})
	defer onServer.Close()
	defer on.Close()
	status, data = createJob(onServer, untested(onServer))
	var refusal struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if status != 409 || json.Unmarshal(data, &refusal) != nil || refusal.Error.Code != "release_untested" || refusal.Error.Message == "" {
		t.Fatalf("policy on created a job from an untested release: %d %s", status, data)
	}
	// The job made while the policy was off is the same job, and still runs.
	status, data = createJob(onServer, earlier)
	var again struct{ ID string }
	if status != 201 || json.Unmarshal(data, &again) != nil || again.ID != job.ID {
		t.Fatalf("earlier job refused: %d %s", status, data)
	}
	status, data = call(onServer, "POST", "jobs/"+job.ID+"/runs", `{"facts":{}}`, "Idempotency-Key", "after-policy")
	var run struct{ ID, State string }
	if status != 202 || json.Unmarshal(data, &run) != nil || run.ID == "" {
		t.Fatalf("earlier job's run refused: %d %s", status, data)
	}
	deadline := time.Now().Add(60 * time.Second)
	for run.State != "completed" {
		if run.State == "failed" || run.State == "interrupted" || time.Now().After(deadline) {
			t.Fatalf("earlier job's run did not complete: %s", data)
		}
		time.Sleep(200 * time.Millisecond)
		if status, data = call(onServer, "GET", "runs/"+run.ID, ""); status != 200 || json.Unmarshal(data, &run) != nil {
			t.Fatalf("run: %d %s", status, data)
		}
	}
}

// The page learns the installation's policy from the desk-config answer, on
// every desk, whether or not a desk-level file exists. `false` is stated, not
// left out, so the page can tell "off" from "not said".
func TestDeskConfigReportsTestedReleasesPolicy(t *testing.T) {
	for _, required := range []bool{true, false} {
		config := t.TempDir()
		os.Chmod(config, 0700)
		// Creating the named desk below runs the runtime; this stand-in answers.
		runtime := filepath.Join(t.TempDir(), "jpack")
		writeStandInRuntime(t, runtime, reading(allConfigVersions), lockingAs(wantGatedConfig))
		s, ts := startDesk(t, Config{JpackBin: runtime, RunnerAllowUntestedReleases: !required, ProjectDir: t.TempDir(), DeskConfigDir: config, Token: testToken})
		t.Cleanup(func() { ts.Close(); s.Close() })
		policy := func(id, state string, present bool) {
			t.Helper()
			status, data := deskCall(t, ts, "GET", "/api/desk-config", id, "", true)
			var answer struct {
				Present bool                       `json:"present"`
				Jobs    map[string]json.RawMessage `json:"jobs"`
			}
			if status != 200 || json.Unmarshal(data, &answer) != nil || answer.Present != present {
				t.Fatalf("%s: %d %s", state, status, data)
			}
			if value, want := string(answer.Jobs["requireTestedReleases"]), strconv.FormatBool(required); value != want {
				t.Fatalf("%s: requireTestedReleases=%v reported %q: %s", state, required, value, data)
			}
		}
		named := createTestDesk(t, ts, "Named").ID
		policy("", "no desk-level file", false)
		policy(named, "a named desk, no desk-level file", false)
		if err := os.WriteFile(s.deskConfigPath(), []byte(`{"deskConfigVersion":1}`), 0600); err != nil {
			t.Fatal(err)
		}
		policy("", "a desk-level file", true)
		policy(named, "a named desk, a desk-level file", true)
	}
}

func TestJobEventOnlyAcceptsScopedNonBrowserDelivery(t *testing.T) {
	s, _ := newTestServer(t, false)
	path := "/api/job-events/trg_" + strings.Repeat("0", 32)
	for _, tc := range []struct {
		method, token, origin string
		want                  int
	}{
		{"POST", "", "", 401}, {"POST", testToken, "", 401}, {"POST", strings.Repeat("a", 64), "http://localhost:5173", 401}, {"GET", strings.Repeat("a", 64), "", 401}, {"POST", strings.Repeat("a", 64), "", 503},
	} {
		r := httptest.NewRequest(tc.method, path, strings.NewReader(`{}`))
		r.Header.Set("Authorization", "Bearer "+tc.token)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s: %d %s", tc.method, w.Code, w.Body)
		}
	}
	for _, path := range []string{"/api/operations/jobs/job_" + strings.Repeat("0", 32) + "/triggers", "/api/operations/triggers/trg_" + strings.Repeat("0", 32) + "/state"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		r.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 64))
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("scoped token admitted to owner controls", w.Code)
		}
	}
}

func TestRunnerBackgroundConnectionsFile(t *testing.T) {
	if _, e := LoadRunnerConnections("relative.json"); e == nil {
		t.Fatal("relative authority accepted")
	}
	path := filepath.Join(t.TempDir(), "connections.json")
	for _, raw := range []string{`null`, `[]`, `{} {}`, `{"unknown":1}`} {
		if e := os.WriteFile(path, []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := LoadRunnerConnections(path); e == nil {
			t.Fatal("invalid config accepted", raw)
		}
	}
	raw := `{"cloud":[{"id":"google","subscription":"projects/example-project/subscriptions/desk-inbox","credentialsFile":"/private/adc.json"}],"gateway":[]}`
	if e := os.WriteFile(path, []byte(raw), 0600); e != nil {
		t.Fatal(e)
	}
	if b, e := LoadRunnerConnections(path); e != nil || string(b) != raw {
		t.Fatal(e)
	}
}

func TestRunnerCloudSubscriptionsBelongToOneDesk(t *testing.T) {
	a, b := strings.Repeat("a", 32), strings.Repeat("b", 32)
	config := runnerConnectionConfig{
		Cloud: []runnerCloudConnection{
			{ID: "startup", Subscription: "projects/test/subscriptions/startup", CredentialsFile: "/private/adc.json"},
			{ID: "alpha", Subscription: "projects/test/subscriptions/alpha", CredentialsFile: "/private/adc.json", Desk: a},
		},
		Gateway: []json.RawMessage{json.RawMessage(`{"profile":"shared","url":"https://gateway.example"}`)},
	}
	raw, _ := json.Marshal(config)
	for _, tc := range []struct {
		name, id, want string
		startup        bool
	}{
		{"startup", "", "startup", true},
		{"named owner", a, "alpha", false},
		{"another desk", b, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selected, err := runnerConnectionsForDesk(raw, tc.id, tc.startup)
			var got runnerConnectionConfig
			if err != nil || json.Unmarshal(selected, &got) != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(got.Cloud) != 0 {
					t.Fatal("unassigned desk can consume cloud signals")
				}
			} else if len(got.Cloud) != 1 || got.Cloud[0].ID != tc.want || got.Cloud[0].Desk != "" {
				t.Fatalf("incorrect Runner boot connection: %s", selected)
			}
			if len(got.Gateway) != 1 {
				t.Fatal("shared Gateway operation binding was lost")
			}
		})
	}
	config.Cloud[1].Subscription = config.Cloud[0].Subscription
	raw, _ = json.Marshal(config)
	if _, err := runnerConnectionsForDesk(raw, a, false); err == nil {
		t.Fatal("competing subscription consumers accepted")
	}
	config.Cloud[1].Subscription = "projects/test/subscriptions/alpha"
	config.Cloud[1].Desk = "../escape"
	raw, _ = json.Marshal(config)
	if _, err := runnerConnectionsForDesk(raw, a, false); err == nil {
		t.Fatal("invalid desk selector accepted")
	}
}

// Filtering has to reach Runner before pagination: otherwise a page of completed
// occurrences can hide a waiting preparation and leave no visible next-page link.
func TestJobsForwardsPreparationFilter(t *testing.T) {
	requests := make(chan string, 1)
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[],"next":0}`))
	}))
	defer companion.Close()
	s := &Server{jobs: &jobsCompanion{url: companion.URL, token: "test-private", done: make(chan struct{})}}
	tail := "jobs/job_" + strings.Repeat("0", 32) + "/occurrences"
	r := httptest.NewRequest("GET", "/api/operations/"+tail+"?preparations=1&after=40&untrusted=ignored", nil)
	w := httptest.NewRecorder()
	s.proxyJobs(w, r, tail, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if got := <-requests; got != "/v1/"+tail+"?after=40&preparations=1" {
		t.Fatal("preparation filter lost at Desk boundary", got)
	}
}

// The verification route forwards the export version a request asks for, as
// Runner reads it, and no other route forwards one at all.
func TestJobsForwardsVerificationVersionOnlyOnItsRoute(t *testing.T) {
	requests := make(chan string, 1)
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer companion.Close()
	s := &Server{jobs: &jobsCompanion{url: companion.URL, token: "test-private", done: make(chan struct{})}}
	run := "runs/run_" + strings.Repeat("0", 32)
	job := "jobs/job_" + strings.Repeat("0", 32)
	for _, tc := range []struct{ tail, query, runner string }{
		{run + "/verification", "version=5", "/v1/" + run + "/verification?version=5"},
		{run + "/verification", "version=4", "/v1/" + run + "/verification?version=4"},
		{run + "/verification", "version=3", "/v1/" + run + "/verification?version=3"},
		{run + "/verification", "version=2", "/v1/" + run + "/verification?version=2"},
		{run + "/verification", "", "/v1/" + run + "/verification"},
		{run + "/verification", "untrusted=ignored&version=5", "/v1/" + run + "/verification?version=5"},
		{run, "version=5", "/v1/" + run},
		{run + "/briefs", "version=5", "/v1/" + run + "/briefs"},
		{job + "/runs", "after=40&version=5", "/v1/" + job + "/runs?after=40"},
		{"runs", "version=5&version=4", "/v1/runs"},
		{"run-chain", "version=5", "/v1/run-chain"},
	} {
		r := httptest.NewRequest("GET", "/api/operations/"+tc.tail+"?"+tc.query, nil)
		w := httptest.NewRecorder()
		s.proxyJobs(w, r, tc.tail, "")
		if w.Code != 200 {
			t.Fatal(tc.tail, tc.query, w.Code, w.Body)
		}
		if got := <-requests; got != tc.runner {
			t.Errorf("%s?%s: forwarded %s, want %s", tc.tail, tc.query, got, tc.runner)
		}
	}
}

// Desk forwards a version only when the request asks once for 2, 3, 4 or 5,
// in a query that parses. Otherwise it refuses the request itself, and nothing
// reaches Runner: a lenient reading would forward one of repeated values, or
// the one value left after dropping a malformed pair.
func TestJobsRefusesAnyOtherVerificationVersion(t *testing.T) {
	forwarded := 0
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer companion.Close()
	s := &Server{jobs: &jobsCompanion{url: companion.URL, token: "test-private", done: make(chan struct{})}}
	tail := "runs/run_" + strings.Repeat("0", 32) + "/verification"
	for _, query := range []string{
		"version=3&version=2",
		"version=2&version=3",
		"version=3&version=3",
		"version=5&version=4",
		"version=5&version=5",
		"version=",
		"version",
		"version=1",
		"version=6",
		"version=05",
		"version=5%20",
		"version=v5",
		"version=five",
		"version=4.0",
		"version=%zz&version=5",
		"version=5&other=%zz",
		"version=5;other=1",
	} {
		forwarded = 0
		r := httptest.NewRequest("GET", "/api/operations/"+tail+"?"+query, nil)
		w := httptest.NewRecorder()
		s.proxyJobs(w, r, tail, "")
		if w.Code != 400 || forwarded != 0 || !strings.Contains(w.Body.String(), "version 2, 3, 4 or 5") {
			t.Errorf("%s: %d %s, forwarded %d", query, w.Code, w.Body, forwarded)
		}
	}
}

// A verification export, and the run it is made from, are read up to
// Runner's MaxExportSize: what verify-run reads of an export. Every other
// route keeps the ordinary limit.
func TestJobsReadsAnExportUpToRunnersLimit(t *testing.T) {
	// Runner's MaxExportSize, read from its source at v0.5.0 and again at v0.6.0,
	// where it is unchanged. Desk cannot import it.
	if runnerExportLimit != 19596893 {
		t.Fatal("Runner's MaxExportSize is 19596893, not", runnerExportLimit)
	}
	size := 0
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`"` + strings.Repeat("a", size-2) + `"`))
	}))
	defer companion.Close()
	s := &Server{jobs: &jobsCompanion{url: companion.URL, token: "test-private", done: make(chan struct{})}}
	run := "runs/run_" + strings.Repeat("0", 32)
	for _, tc := range []struct {
		tail   string
		size   int
		status int
	}{
		{run + "/verification", runnerAnswerLimit + 1, 200},
		{run + "/verification", runnerExportLimit, 200},
		{run + "/verification", runnerExportLimit + 1, 502},
		{run, runnerAnswerLimit + 1, 200},
		{run, runnerExportLimit, 200},
		{run, runnerExportLimit + 1, 502},
		{run + "/briefs", runnerAnswerLimit, 200},
		{run + "/briefs", runnerAnswerLimit + 1, 502},
		{"runs", runnerAnswerLimit + 1, 502},
		{"jobs/job_" + strings.Repeat("0", 32) + "/runs", runnerAnswerLimit + 1, 502},
	} {
		size = tc.size
		r := httptest.NewRequest("GET", "/api/operations/"+tc.tail, nil)
		w := httptest.NewRecorder()
		s.proxyJobs(w, r, tc.tail, "")
		if w.Code != tc.status {
			t.Errorf("%s, %d bytes: %d, want %d", tc.tail, tc.size, w.Code, tc.status)
		}
		if tc.status == 200 && w.Body.Len() != tc.size {
			t.Errorf("%s: relayed %d of %d bytes", tc.tail, w.Body.Len(), tc.size)
		}
	}
}

// Runner's chain of runs reaches the page as Runner sent it: the same bytes,
// its own type, and no query of the caller's. A refusal of Runner's passes
// through with its status and type.
func TestJobsPassesTheRunChainThroughUntouched(t *testing.T) {
	// Two lines as Runner ends them, and bytes that are not UTF-8 nor JSON: a
	// relay that decoded, re-encoded or trimmed the answer would change them.
	chain := []byte(`{"entryVersion":"1","trail":"` + strings.Repeat("a", 32) + `","sequence":1,"previous":"sha256:` + strings.Repeat("0", 64) + `","kind":"run","run":"run_` + strings.Repeat("1", 32) + `","auditDigest":"sha256:` + strings.Repeat("2", 64) + `"}` + "\n" +
		"{\"sequence\":2, \"note\":\"\xff\xfe\\u0026\"}  \n")
	type answer struct {
		status      int
		contentType []string
		body        []byte
	}
	var serve answer
	requests := make(chan *http.Request, 1)
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r
		w.Header()["Content-Type"] = serve.contentType
		w.WriteHeader(serve.status)
		w.Write(serve.body)
	}))
	defer companion.Close()
	s := &Server{jobs: &jobsCompanion{url: companion.URL, token: "test-private", done: make(chan struct{})}}
	refusal := []byte(`{"error":{"code":"store_error","message":"The run store could not be read.","retryable":true}}` + "\n")
	for _, tc := range []struct {
		name, query string
		serve       answer
		wantType    []string
	}{
		{"the chain", "", answer{200, []string{"application/jsonl"}, chain}, []string{"application/jsonl"}},
		{"the chain, asked with a query", "after=40&q=x&version=5", answer{200, []string{"application/jsonl"}, chain}, []string{"application/jsonl"}},
		{"an empty chain", "", answer{200, []string{"application/jsonl"}, nil}, []string{"application/jsonl"}},
		{"a refusal", "", answer{500, []string{"application/json"}, refusal}, []string{"application/json"}},
		{"no type", "", answer{200, nil, chain}, nil},
	} {
		serve = tc.serve
		r := httptest.NewRequest("GET", "/api/operations/run-chain?"+tc.query, nil)
		w := httptest.NewRecorder()
		s.proxyJobs(w, r, "run-chain", "")
		got := <-requests
		if got.Method != "GET" || got.URL.RequestURI() != "/v1/run-chain" || got.Header.Get("Authorization") != "Bearer test-private" {
			t.Errorf("%s: forwarded %s %s", tc.name, got.Method, got.URL.RequestURI())
		}
		if w.Code != tc.serve.status || !bytes.Equal(w.Body.Bytes(), tc.serve.body) {
			t.Errorf("%s: %d %q, want %d %q", tc.name, w.Code, w.Body.Bytes(), tc.serve.status, tc.serve.body)
		}
		if gotType := w.Result().Header.Values("Content-Type"); !slices.Equal(gotType, tc.wantType) {
			t.Errorf("%s: Content-Type %q, want %q", tc.name, gotType, tc.wantType)
		}
	}
}

// The chain is passed on whole or not at all. Past Desk's bound it is refused,
// never cut to the bound, and up to it every byte is passed on.
func TestJobsRefusesARunChainPastItsBound(t *testing.T) {
	// 65,536 entries of the longest line Runner writes, and its newline.
	if runChainLimit != 65536*1025 {
		t.Fatal("the chain's bound is 65,536 lines of 1025 bytes, not", runChainLimit)
	}
	var chain []byte
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/jsonl")
		w.Write(chain)
	}))
	defer companion.Close()
	s := &Server{jobs: &jobsCompanion{url: companion.URL, token: "test-private", done: make(chan struct{})}}
	for _, tc := range []struct {
		size, status int
	}{
		{runChainLimit, 200},
		{runChainLimit + 1, 502},
	} {
		line := append(bytes.Repeat([]byte("a"), 1024), '\n')
		chain = append(bytes.Repeat(line, tc.size/len(line)), bytes.Repeat([]byte("b"), tc.size%len(line))...)
		r := httptest.NewRequest("GET", "/api/operations/run-chain", nil)
		w := httptest.NewRecorder()
		s.proxyJobs(w, r, "run-chain", "")
		if w.Code != tc.status {
			t.Errorf("%d bytes: %d, want %d", tc.size, w.Code, tc.status)
		}
		switch {
		case tc.status == 200 && !bytes.Equal(w.Body.Bytes(), chain):
			t.Errorf("%d bytes: relayed %d bytes that are not the chain", tc.size, w.Body.Len())
		case tc.status != 200 && (bytes.Contains(w.Body.Bytes(), line) || w.Header().Get("Content-Type") != "application/json"):
			t.Errorf("%d bytes: a refusal carried the chain, or another type: %s %.80q", tc.size, w.Header().Get("Content-Type"), w.Body.Bytes())
		}
	}

	// Desk stops reading one byte past the bound. A chain ten times over it is
	// refused having taken little more than the bound from Runner, never read
	// whole and measured after: what Runner manages to write past the bound is
	// what the connection buffers before Desk lets it go. Measured on loopback
	// that is 4 to 13 MB; 64 MiB allows for a kernel that grows its socket
	// buffers to 32 MiB, and is still a tenth of what a whole read takes.
	line := append(bytes.Repeat([]byte("a"), 1024), '\n')
	page := bytes.Repeat(line, 1024)
	written := make(chan int, 1)
	flood := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/jsonl")
		total := 0
		for total < 10*runChainLimit {
			n, err := w.Write(page)
			total += n
			if err != nil {
				break
			}
		}
		written <- total
	}))
	defer flood.Close()
	s.jobs = &jobsCompanion{url: flood.URL, token: "test-private", done: make(chan struct{})}
	r := httptest.NewRequest("GET", "/api/operations/run-chain", nil)
	w := httptest.NewRecorder()
	s.proxyJobs(w, r, "run-chain", "")
	if w.Code != 502 || bytes.Contains(w.Body.Bytes(), line) || w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("a chain ten times the bound: %d %s %.80q", w.Code, w.Header().Get("Content-Type"), w.Body.Bytes())
	}
	select {
	case n := <-written:
		t.Logf("Runner wrote %d bytes of a %d-byte chain; the bound is %d", n, 10*runChainLimit, runChainLimit)
		if n > runChainLimit+64<<20 {
			t.Errorf("Runner wrote %d bytes of a %d-byte chain before Desk refused it: Desk read past its bound of %d", n, 10*runChainLimit, runChainLimit)
		}
	case <-time.After(30 * time.Second):
		t.Error("Runner was still writing the chain 30 seconds after Desk refused it")
	}
}

// A transfer that ends early is an error, never a shorter chain: Runner
// aborts the chain on a page it cannot read after the answer has begun, and a
// closed connection leaves a body short of its length or its last chunk.
func TestJobsAnAbortedRunChainIsAnError(t *testing.T) {
	page := `{"sequence":1}` + "\n"
	raw := func(response string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			connection, buffered, err := http.NewResponseController(w).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			buffered.WriteString(response)
			buffered.Flush()
			connection.Close()
		}
	}
	for _, tc := range []struct {
		name   string
		runner http.HandlerFunc
	}{
		{"aborted as Runner aborts it", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/jsonl")
			w.WriteHeader(200)
			w.Write([]byte(page))
			http.NewResponseController(w).Flush()
			panic(http.ErrAbortHandler)
		}},
		{"closed short of its length", raw("HTTP/1.1 200 OK\r\nContent-Type: application/jsonl\r\nContent-Length: 1000\r\n\r\n" + page)},
		{"closed before its last chunk", raw("HTTP/1.1 200 OK\r\nContent-Type: application/jsonl\r\nTransfer-Encoding: chunked\r\n\r\n" + strconv.FormatInt(int64(len(page)), 16) + "\r\n" + page + "\r\n")},
	} {
		companion := httptest.NewUnstartedServer(tc.runner)
		// The server's own note of a hijacked or aborted answer is not this test's.
		companion.Config.ErrorLog = log.New(io.Discard, "", 0)
		companion.Start()
		s := &Server{jobs: &jobsCompanion{url: companion.URL, token: "test-private", done: make(chan struct{})}}
		r := httptest.NewRequest("GET", "/api/operations/run-chain", nil)
		w := httptest.NewRecorder()
		s.proxyJobs(w, r, "run-chain", "")
		companion.Close()
		if w.Code != 502 || strings.Contains(w.Body.String(), page) || w.Header().Get("Content-Type") != "application/json" {
			t.Errorf("%s: %d %s %q", tc.name, w.Code, w.Header().Get("Content-Type"), w.Body)
		}
	}
}

// The chain has its own route, read with GET alone. Another method on it, or a
// path that only begins with it, is not a Jobs operation and reaches nothing.
func TestJobsReadsTheRunChainOnItsRouteAlone(t *testing.T) {
	var forwarded []string
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = append(forwarded, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/jsonl")
		w.Write([]byte("{}\n"))
	}))
	defer companion.Close()
	s, _ := newTestServer(t, false)
	s.jobs = &jobsCompanion{url: companion.URL, token: "test-private", done: make(chan struct{}), stop: make(chan struct{})}
	// The fake has no process to stop.
	t.Cleanup(func() { s.jobs.closed = true })
	call := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/api/operations/run-chain"); w.Code != 200 || w.Body.String() != "{}\n" || w.Header().Get("Content-Type") != "application/jsonl" || !slices.Equal(forwarded, []string{"GET /v1/run-chain"}) {
		t.Fatalf("the chain was not read: %d %s %q, forwarded %q", w.Code, w.Header().Get("Content-Type"), w.Body, forwarded)
	}
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/operations/run-chain"},
		{"PUT", "/api/operations/run-chain"},
		{"PATCH", "/api/operations/run-chain"},
		{"DELETE", "/api/operations/run-chain"},
		{"HEAD", "/api/operations/run-chain"},
		{"GET", "/api/operations/run-chain/"},
		{"GET", "/api/operations/run-chain/1"},
		{"GET", "/api/operations/run-chain%2F1"},
		{"GET", "/api/operations/run-chain.jsonl"},
		{"GET", "/api/operations/run-chains"},
		{"GET", "/api/operations/Run-Chain"},
		{"GET", "/api/operations/runs/run-chain"},
		{"GET", "/api/operations/run-chain/verification"},
	} {
		forwarded = nil
		if w := call(tc.method, tc.path); w.Code < 300 || len(forwarded) != 0 {
			t.Errorf("%s %s: %d, forwarded %q", tc.method, tc.path, w.Code, forwarded)
		}
	}
}

// A trigger token reads one thing through Desk: what became of an occurrence
// it created. Desk forwards exactly that read, built from the parsed
// identifiers, and passes the Runner's refusals through unchanged.
func TestJobEventResultForwardsOnlyTheParsedRead(t *testing.T) {
	trigger, occurrence := "trg_"+strings.Repeat("a1", 16), "occ_"+strings.Repeat("b2", 16)
	token := strings.Repeat("c3", 32)
	answer := `{"id":"` + occurrence + `","state":"submitted","run":{"state":"queued"}}`
	invalid := `{"error":{"code":"invalid_trigger_token","message":"The event credential is invalid.","retryable":false}}`
	missing := `{"error":{"code":"occurrence_not_found","message":"No occurrence with this ID was created with this credential.","retryable":false}}`
	var forwarded []*http.Request
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body.Close()
		forwarded = append(forwarded, r)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Header.Get("X-Trigger-Token") != token:
			w.WriteHeader(401)
			w.Write([]byte(invalid))
		case r.URL.Path != "/v1/triggers/"+trigger+"/occurrences/"+occurrence:
			w.WriteHeader(404)
			w.Write([]byte(missing))
		default:
			w.Write([]byte(answer))
		}
	}))
	defer companion.Close()
	s, ts, _ := assistantServer(t)
	named := createTestDesk(t, ts, "Events")
	s.desksMu.Lock()
	child := s.desks[named.ID]
	s.desksMu.Unlock()
	fake := func() *jobsCompanion {
		return &jobsCompanion{url: companion.URL, token: "owner-private", done: make(chan struct{}), stop: make(chan struct{})}
	}
	s.jobs, child.jobs = fake(), fake()
	// Neither fake has a process to stop.
	t.Cleanup(func() { s.jobs.closed, child.jobs.closed = true, true })
	read := "/api/job-events/" + trigger + "/occurrences/" + occurrence
	runnerRead := "/v1/triggers/" + trigger + "/occurrences/" + occurrence
	call := func(method, target, body string, header http.Header) *httptest.ResponseRecorder {
		t.Helper()
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		r := httptest.NewRequest(method, target, reader)
		r.Header = header
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	bearer := func(value string) http.Header { return http.Header{"Authorization": {"Bearer " + value}} }

	for _, tc := range []struct {
		name, target string
		header       http.Header
		status       int
		body, runner string
	}{
		{"startup desk", read, bearer(token), 200, answer, runnerRead},
		{"named desk", "/api/desks/" + named.ID + read[len("/api"):], bearer(token), 200, answer, runnerRead},
		// Escaped identifiers are decided, and forwarded, as the values they parse to.
		{"escaped identifiers", "/api/job-events/trg_%61" + trigger[5:] + "/occurrences/" + occurrence, bearer(token), 200, answer, runnerRead},
		{"rotated token", read, bearer(strings.Repeat("d4", 32)), 401, invalid, runnerRead},
		{"another occurrence", "/api/job-events/" + trigger + "/occurrences/occ_" + strings.Repeat("e5", 16), bearer(token), 404, missing, "/v1/triggers/" + trigger + "/occurrences/occ_" + strings.Repeat("e5", 16)},
	} {
		forwarded = nil
		header := tc.header.Clone()
		// Nothing else the caller sends travels with the read.
		header.Set("X-Trigger-Token", strings.Repeat("f6", 32))
		header.Set("Idempotency-Key", "caller-key")
		header.Set("Cookie", "session=caller")
		w := call("GET", tc.target, "", header)
		if w.Code != tc.status || w.Body.String() != tc.body {
			t.Fatalf("%s: %d %s", tc.name, w.Code, w.Body)
		}
		if len(forwarded) != 1 {
			t.Fatalf("%s: forwarded %d requests", tc.name, len(forwarded))
		}
		got := forwarded[0]
		if got.Method != "GET" || got.URL.RequestURI() != tc.runner || got.Header.Get("Authorization") != "Bearer owner-private" || got.Header.Get("X-Trigger-Token") != strings.TrimPrefix(tc.header.Get("Authorization"), "Bearer ") || got.Header.Get("Idempotency-Key") != "" || got.Header.Get("Cookie") != "" || got.ContentLength != 0 {
			t.Fatalf("%s: forwarded %s %s %v", tc.name, got.Method, got.URL.RequestURI(), got.Header)
		}
	}

	browser := bearer(token)
	browser.Set("Origin", "http://localhost:5173")
	emptyOrigin := bearer(token)
	emptyOrigin["Origin"] = []string{""}
	twice := bearer(token)
	twice.Add("Authorization", "Bearer "+token)
	session := bearer(token)
	session.Set("X-Jpack-Desk", named.ID)
	// A same-origin GET carries no Origin, only fetch metadata.
	sameOrigin := bearer(token)
	sameOrigin.Set("Sec-Fetch-Site", "same-origin")
	sameOrigin.Set("Sec-Fetch-Mode", "cors")
	for _, tc := range []struct {
		name, method, target, body string
		header                     http.Header
	}{
		{"no credential", "GET", read, "", http.Header{}},
		{"desk launch secret", "GET", read, "", bearer(testToken)},
		{"short token", "GET", read, "", bearer(token[1:])},
		{"token that is not hex", "GET", read, "", bearer(strings.ToUpper(token))},
		{"another scheme", "GET", read, "", http.Header{"Authorization": {"Basic " + token}}},
		{"bare token", "GET", read, "", http.Header{"Authorization": {token}}},
		{"two credentials", "GET", read, "", twice},
		{"browser origin", "GET", read, "", browser},
		{"empty origin", "GET", read, "", emptyOrigin},
		{"same-origin browser", "GET", read, "", sameOrigin},
		{"desk selector", "GET", read, "", session},
		{"query", "GET", read + "?after=1", "", bearer(token)},
		{"empty query", "GET", read + "?", "", bearer(token)},
		{"named desk empty query", "GET", "/api/desks/" + named.ID + read[len("/api"):] + "?", "", bearer(token)},
		{"body", "GET", read, "{}", bearer(token)},
		{"delivery method", "POST", read, "{}", bearer(token)},
		{"head", "HEAD", read, "", bearer(token)},
		{"preflight", "OPTIONS", read, "", browser},
		{"short trigger", "GET", "/api/job-events/" + trigger[:35] + "/occurrences/" + occurrence, "", bearer(token)},
		{"another record kind", "GET", "/api/job-events/job_" + trigger[4:] + "/occurrences/" + occurrence, "", bearer(token)},
		{"run for occurrence", "GET", "/api/job-events/" + trigger + "/occurrences/run_" + occurrence[4:], "", bearer(token)},
		{"uppercase occurrence", "GET", "/api/job-events/" + trigger + "/occurrences/" + strings.ToUpper(occurrence), "", bearer(token)},
		{"escaped separator", "GET", "/api/job-events/" + trigger + "/occurrences/" + occurrence + "%2Fcancel", "", bearer(token)},
		{"escaped traversal", "GET", "/api/job-events/" + trigger + "/occurrences/..%2F..%2Fjobs", "", bearer(token)},
		{"extra segment", "GET", read + "/cancel", "", bearer(token)},
		{"trailing slash", "GET", read + "/", "", bearer(token)},
		{"listing", "GET", "/api/job-events/" + trigger + "/occurrences", "", bearer(token)},
		{"dot segment", "GET", "/api/job-events/" + trigger + "/occurrences/" + occurrence + "/../" + occurrence, "", bearer(token)},
		{"unknown desk", "GET", "/api/desks/" + strings.Repeat("0", 32) + read[len("/api"):], "", bearer(token)},
		{"named desk origin", "GET", "/api/desks/" + named.ID + read[len("/api"):], "", browser},
		{"named desk same-origin browser", "GET", "/api/desks/" + named.ID + read[len("/api"):], "", sameOrigin},
	} {
		forwarded = nil
		w := call(tc.method, tc.target, tc.body, tc.header)
		if len(forwarded) != 0 || w.Code < 300 {
			t.Fatalf("%s: %d %s, forwarded %d", tc.name, w.Code, w.Body, len(forwarded))
		}
	}

	// The Runner path comes from the parsed identifiers, never from the
	// incoming path string, whatever that string is.
	forwarded = nil
	r := httptest.NewRequest("GET", "/elsewhere/"+trigger+"/occurrences/occ_"+strings.Repeat("e5", 16), nil)
	r.Header = bearer(token)
	r.SetPathValue("trigger", trigger)
	r.SetPathValue("occurrence", occurrence)
	w := httptest.NewRecorder()
	s.handleJobEventResult(w, r)
	if w.Code != 200 || len(forwarded) != 1 || forwarded[0].URL.RequestURI() != runnerRead {
		t.Fatalf("path not built from parsed identifiers: %d %s", w.Code, w.Body)
	}
}
