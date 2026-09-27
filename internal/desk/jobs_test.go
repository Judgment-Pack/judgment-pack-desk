package desk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
