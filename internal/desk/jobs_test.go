package desk

import (
	"net/http"
	"net/http/httptest"
	"os"
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
	r := httptest.NewRequest("POST", "/api/operations/previews", strings.NewReader(`{"owner":"other"}`))
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("unsupported identity was accepted", w.Code, w.Body)
	}
}
