package desk

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const rehearsalAnswer = `{"outputVersion":"2","command":"experimental graph evaluate","status":"evaluated","kind":"non-normative-runtime-convention","experimental":true,"rehearsal":true,"label":"composite <runtime>  label","conformanceClaimReference":"CONFORMANCE.md","disposition":{"number":1.0,"text":"\u0026"},"nodes":[],"handoffs":[]}`
const rehearsalInputs = `{"screening":{"facts":{"screening":{"matches":"0"}},"evidence":{"screening-record":"present"}}}`

func rehearsalDesk(t *testing.T, project, bin string) (*Server, *bytes.Buffer) {
	t.Helper()
	logged := new(bytes.Buffer)
	s, err := New(Config{ProjectDir: project, JpackBin: bin, Token: testToken, Port: testPort, DeskConfigDir: t.TempDir(), Logger: log.New(logged, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, logged
}

func rehearsalPost(s *Server, id, input string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "http://localhost/api/graphs/evaluate?id="+id, strings.NewReader(input))
	bearer(r)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func rehearsalRig(t *testing.T) (*Server, *graphRig, *bytes.Buffer) {
	t.Helper()
	rig := newGraphRig(t)
	rig.answers(t, "list", listAnswer, 0)
	rig.answers(t, "evaluate", rehearsalAnswer, 0)
	// Capture stdin outside the project. A deciding call simulates a trail write.
	script, _ := os.ReadFile(rig.bin)
	script = bytes.Replace(script, []byte("f='"), []byte("if [ \"$3\" = evaluate ]; then\ncat > '"+rig.dir+"/input'\ncase \" $* \" in *' --rehearsal '*) ;; *) printf 'decision\\n' >> kept.jsonl ;; esac\nfi\nf='"), 1)
	if err := os.WriteFile(rig.bin, script, 0755); err != nil {
		t.Fatal(err)
	}
	s, logged := rehearsalDesk(t, t.TempDir(), rig.bin)
	return s, rig, logged
}

func TestGraphRow4RehearsalOnly(t *testing.T) {
	s, rig, _ := rehearsalRig(t)
	before := treeOf(t, s.projectDir)
	input := `{"node":{"facts":{"text":"$(touch INJECTED); ' ","number":1.0}}}`
	w := rehearsalPost(s, "onboarding", input)
	if w.Code != 200 || string(answerOf(t, w.Body.Bytes())) != rehearsalAnswer {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	want := "experimental graph evaluate onboarding.graph.json --inputs - --rehearsal --config jpack.json --format json"
	calls := rig.asked(t)
	if len(calls) != 2 || calls[1] != want {
		t.Fatalf("arguments: %q", calls)
	}
	got, _ := os.ReadFile(filepath.Join(rig.dir, "input"))
	if string(got) != input {
		t.Fatalf("stdin: %q", got)
	}
	sameProject(t, before, treeOf(t, s.projectDir), "a rehearsal keeps nothing")
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cacheable facts")
	}
}

func TestGraphRow4RequiresRehearsalTrue(t *testing.T) {
	s, rig, logged := rehearsalRig(t)
	for _, marker := range []string{"", `,"rehearsal":false`, `,"rehearsal":"true"`, `,"rehearsal":null`} {
		rig.answers(t, "evaluate", `{"outputVersion":"2","command":"experimental graph evaluate","status":"error","diagnostics":[{"node":"screening","message":"SECRET-FACT unknown flag: --rehearsal"}]`+marker+`}`, 3)
		w := rehearsalPost(s, "onboarding", "{}")
		if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "screening") || !strings.Contains(w.Body.String(), "unknown flag") || strings.Contains(w.Body.String(), `"answer":`) {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
	if rig.ran(t, "evaluate") != 4 || strings.Contains(logged.String(), "SECRET-FACT") {
		t.Fatalf("retry or facts logged: %s", logged)
	}
}

func TestGraphRow4InputsBoundBeforeRuntime(t *testing.T) {
	s, rig, _ := rehearsalRig(t)
	for _, input := range []string{"", "null", "[]", "1", `"facts"`, "{}{}", `{"x":"` + strings.Repeat("x", maxFileBytes-len(`{"x":""}`)+1) + `"}`, `{"x":`, `{"x":"` + strings.Repeat("x", maxFileBytes) + `"}`} {
		w := rehearsalPost(s, "onboarding", input)
		if w.Code != 400 && w.Code != 413 {
			t.Fatalf("input accepted: %d", w.Code)
		}
	}
	if len(rig.asked(t)) != 0 {
		t.Fatal("runtime ran on refused inputs")
	}
	input := `{"x":"` + strings.Repeat("x", maxFileBytes-len(`{"x":""}`)) + `"}`
	if w := rehearsalPost(s, "onboarding", input); w.Code != 200 {
		t.Fatalf("at bound: %d %s", w.Code, w.Body)
	}
}

func TestGraphRow4IDAndContainment(t *testing.T) {
	s, rig, _ := rehearsalRig(t)
	for _, id := range []string{"", "unknown"} {
		if w := rehearsalPost(s, id, "{}"); w.Code != 400 && w.Code != 404 {
			t.Fatalf("id accepted: %d", w.Code)
		}
	}
	for _, path := range []string{"../escape.json", "a/../../escape.json", "/outside/secret.json"} {
		rig.answers(t, "list", strings.Replace(listAnswer, "onboarding.graph.json", path, 1), 0)
		if w := rehearsalPost(s, "onboarding", "{}"); w.Code != 409 {
			t.Fatalf("path accepted: %d", w.Code)
		}
	}
	if rig.ran(t, "evaluate") != 0 {
		t.Fatal("evaluated unlisted or escaped graph")
	}
	rig.answers(t, "list", strings.Replace(listAnswer, "onboarding.graph.json", "a/../-graph.json", 1), 0)
	if w := rehearsalPost(s, "onboarding", "{}"); w.Code != 200 {
		t.Fatal(w.Body)
	}
	calls := rig.asked(t)
	if !strings.Contains(calls[len(calls)-1], "evaluate ./-graph.json --inputs") {
		t.Fatal(calls)
	}
}

func TestGraphRow4RedactsWithoutLogging(t *testing.T) {
	s, rig, logged := rehearsalRig(t)
	raw := strings.Replace(rehearsalAnswer, `"nodes":[]`, `"nodes":[{"path":"/private/SECRET-PATH/a.json","detail":"read /private/SECRET-PATH/a.json","facts":"SECRET-FACT"}]`, 1)
	rig.answers(t, "evaluate", raw, 0)
	w := rehearsalPost(s, "onboarding", "{}")
	if w.Code != 200 || strings.Contains(w.Body.String(), "SECRET-PATH") || !strings.Contains(w.Body.String(), "SECRET-FACT") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if strings.Contains(logged.String(), "SECRET") {
		t.Fatalf("logged: %s", logged)
	}
	// Listing diagnostics also use the non-logging error path.
	rig.answers(t, "list", `{"outputVersion":"2","command":"experimental graph list","status":"error","diagnostics":[{"message":"SECRET-FACT at /private/SECRET-PATH/a.json"}]}`, 1)
	w = rehearsalPost(s, "onboarding", "{}")
	if w.Code != 500 || strings.Contains(w.Body.String(), "SECRET-PATH") || strings.Contains(logged.String(), "SECRET") {
		t.Fatalf("failure: %d %s; log %s", w.Code, w.Body, logged)
	}
}

func TestGraphRow4RuntimeBoundsAndGuards(t *testing.T) {
	s, rig, _ := rehearsalRig(t)
	rig.answers(t, "evaluate", strings.Replace(rehearsalAnswer, "composite <runtime>  label", strings.Repeat("x", runtimeAnswerLimit), 1), 0)
	if w := rehearsalPost(s, "onboarding", "{}"); w.Code != 500 || !strings.Contains(w.Body.String(), "65536") {
		t.Fatalf("answer bound: %d %s", w.Code, w.Body)
	}
	for _, origin := range []string{"", "https://outside.invalid"} {
		r := httptest.NewRequest("POST", "http://localhost/api/graphs/evaluate?id=onboarding", strings.NewReader("{}"))
		if origin != "" {
			bearer(r)
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 401 && w.Code != 403 {
			t.Fatalf("guard: %d", w.Code)
		}
	}
	before := len(rig.asked(t))
	t.Setenv(runtimeConfigEnv, filepath.Join(t.TempDir(), "other.json"))
	if w := rehearsalPost(s, "onboarding", "{}"); w.Code != 409 {
		t.Fatalf("config guard: %d", w.Code)
	}
	if len(rig.asked(t)) != before {
		t.Fatal("guard ran runtime")
	}
	t.Setenv(runtimeConfigEnv, "")
	old := runtimeCommandTimeout
	runtimeCommandTimeout = 100 * time.Millisecond
	t.Cleanup(func() { runtimeCommandTimeout = old })
	script, _ := os.ReadFile(rig.bin)
	script = bytes.Replace(script, []byte("cat >"), []byte("while :; do :; done\ncat >"), 1)
	if err := os.WriteFile(rig.bin, script, 0755); err != nil {
		t.Fatal(err)
	}
	if w := rehearsalPost(s, "onboarding", "{}"); w.Code != 500 || !strings.Contains(w.Body.String(), "within") {
		t.Fatalf("timeout: %d %s", w.Code, w.Body)
	}
}

func TestGraphRow4PublishedRuntimeTrailUnchanged(t *testing.T) {
	bin := os.Getenv("JPACK_BIN")
	if bin == "" {
		t.Skip("set JPACK_BIN to the published runtime")
	}
	if !filepath.IsAbs(bin) {
		var err error
		bin, err = filepath.Abs(filepath.Join("..", "..", bin))
		if err != nil {
			t.Fatal(err)
		}
	}
	project := t.TempDir()
	entries, err := os.ReadDir("testdata/graphs")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join("testdata/graphs", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		writeProjectFile(t, project, entry.Name(), string(data))
	}
	s, _ := rehearsalDesk(t, project, bin)
	before := treeOf(t, project)
	w := rehearsalPost(s, "onboarding", rehearsalInputs)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"rehearsal":true`) || !strings.Contains(w.Body.String(), `"outcomeId":"approve"`) {
		t.Fatalf("configVersion 2: %d %s", w.Code, w.Body)
	}
	sameProject(t, before, treeOf(t, project), "configVersion 2 rehearsal")
	// Also test a real nonempty trail: v2 has no audit configuration at all.
	var config map[string]any
	if err := json.Unmarshal([]byte(before["jpack.json"].data), &config); err != nil {
		t.Fatal(err)
	}
	config["configVersion"] = "3"
	config["audit"] = map[string]any{"dir": "audit"}
	data, _ := json.Marshal(config)
	writeProjectFile(t, project, "jpack.json", string(data))
	dir, refusal := s.graphRuntime()
	if refusal != "" {
		t.Fatal(refusal)
	}
	if _, _, err := s.runGraphInput(t.Context(), dir, "evaluate", []byte(rehearsalInputs), "onboarding.graph.json", "--inputs", "-"); err != nil {
		t.Fatal(err)
	}
	trail, err := os.ReadFile(filepath.Join(project, "audit", "evaluations.jsonl"))
	if err != nil || bytes.Count(trail, []byte("\n")) != 3 {
		t.Fatalf("seed trail: %s %v", trail, err)
	}
	before = treeOf(t, project)
	w = rehearsalPost(s, "onboarding", rehearsalInputs)
	if w.Code != 200 {
		t.Fatalf("with trail: %d %s", w.Code, w.Body)
	}
	sameProject(t, before, treeOf(t, project), "existing trail after rehearsal")
}
