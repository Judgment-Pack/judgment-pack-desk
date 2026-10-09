package desk

// A project's graphs, the findings and the plan (ADR-0011, section 3, row 1).
// A stand-in runtime, by absolute path, prints the answers each test prepares
// and records its arguments, so these run where no runtime is installed. The
// last test drives the published runtime and skips without JPACK_BIN.

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// graphRig is a stand-in for `experimental graph list`, `validate` and
// `explain`, and the files that steer it.
type graphRig struct {
	bin, calls, dir string
}

// newGraphRig writes the stand-in. Each command prints `<command>.json` and
// exits with the number in `<command>.exit` (0 where there is none). Every run
// appends its arguments to calls. Builtins only.
func newGraphRig(t *testing.T) *graphRig {
	t.Helper()
	dir := t.TempDir()
	rig := &graphRig{bin: filepath.Join(dir, "jpack"), calls: filepath.Join(dir, "calls"), dir: dir}
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> '" + rig.calls + "'\n" +
		"[ \"$1 $2\" = 'experimental graph' ] || exit 64\n" +
		"f='" + dir + "/'\"$3\"\n" +
		"[ -e \"$f.json\" ] || exit 64\n" +
		"while IFS= read -r line || [ -n \"$line\" ]; do printf '%s\\n' \"$line\"; done < \"$f.json\"\n" +
		"if [ -e \"$f.exit\" ]; then IFS= read -r code < \"$f.exit\"; exit \"$code\"; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(rig.bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return rig
}

// answers sets what a command prints and the code it exits with.
func (rig *graphRig) answers(t *testing.T, command, body string, code int) {
	t.Helper()
	if os.WriteFile(filepath.Join(rig.dir, command+".json"), []byte(body), 0o600) != nil ||
		os.WriteFile(filepath.Join(rig.dir, command+".exit"), []byte(strconv.Itoa(code)+"\n"), 0o600) != nil {
		t.Fatal("could not prepare the stand-in's answer")
	}
}

// asked is the arguments of the runs so far, one line each.
func (rig *graphRig) asked(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(rig.calls)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

func (rig *graphRig) ran(t *testing.T, command string) int {
	t.Helper()
	n := 0
	for _, line := range rig.asked(t) {
		if strings.HasPrefix(line, "experimental graph "+command+" ") {
			n++
		}
	}
	return n
}

// graphsDesk is a startup desk over project, behind the stand-in.
func graphsDesk(t *testing.T, project, bin string) (*Server, *httptest.Server, *bytes.Buffer) {
	t.Helper()
	logged := &bytes.Buffer{}
	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: bin, Token: testToken, Logger: log.New(logged, "", 0)})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	return s, ts, logged
}

func graphGet(t *testing.T, ts *httptest.Server, path string) (int, []byte) {
	t.Helper()
	r, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	bearer(r)
	response, err := ts.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return response.StatusCode, data
}

// answerOf is the runtime's answer member of a route's answer, as bytes.
func answerOf(t *testing.T, data []byte) []byte {
	t.Helper()
	var wrapped struct {
		Answer json.RawMessage `json:"answer"`
	}
	if json.Unmarshal(data, &wrapped) != nil || wrapped.Answer == nil {
		t.Fatalf("no answer member in %s", data)
	}
	return wrapped.Answer
}

func errorOf(t *testing.T, data []byte) string {
	t.Helper()
	var refusal struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(data, &refusal)
	return refusal.Error
}

const (
	graphLabel = "non-normative-runtime-convention"
	// An answer with an odd order, a number written 1.0, an escaped angle
	// bracket and a space that the page must receive exactly as printed.
	validateAnswer = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"experimental graph validate","status":"invalid","kind":"` + graphLabel + `","zeta":1.0,"raw":"<b> & \u2028 \u0026","space":"a  b","formatVersion":"1","configPath":"jpack.json","configVersion":"2","summary":{"total":1,"passed":0,"failed":1},"graphs":[{"id":"onboarding","path":"onboarding.graph.json","status":"invalid","diagnostics":[{"code":"JPS-X","message":"a \u003c b  and a \u2028 c"}]}]}`
	listAnswer     = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"experimental graph list","status":"resolved","experimental":true,"kind":"` + graphLabel + `","configPath":"jpack.json","configVersion":"2","graphs":[{"id":"onboarding","path":"onboarding.graph.json","rowsDeclared":true}]}`
	explainAnswer  = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"experimental graph explain","status":"planned","kind":"` + graphLabel + `","graphPath":"onboarding.graph.json","steps":[{"order":1,"node":"screening","path":"a.pack.json","feeds":[]}]}`
)

func TestGraphRow1FindingsAreTheRuntimesBytes(t *testing.T) {
	rig := newGraphRig(t)
	rig.answers(t, "validate", validateAnswer, 1)
	_, ts, _ := graphsDesk(t, t.TempDir(), rig.bin)
	status, data := graphGet(t, ts, "/api/graphs/findings")
	if status != 200 {
		t.Fatalf("findings: %d %s", status, data)
	}
	if got := answerOf(t, data); string(got) != validateAnswer {
		t.Fatalf("the answer is not the runtime's bytes:\n got %s\nwant %s", got, validateAnswer)
	}
	if !strings.Contains(string(data), `"kind":"`+graphLabel+`"`) {
		t.Fatalf("the label is not on the page's answer: %s", data)
	}
	if want := "experimental graph validate --config jpack.json --format json"; len(rig.asked(t)) != 1 || rig.asked(t)[0] != want {
		t.Fatalf("asked %q, want %q", rig.asked(t), want)
	}
}

func TestGraphRow1APlanIsAskedForByTheListedPathAlone(t *testing.T) {
	rig := newGraphRig(t)
	rig.answers(t, "list", listAnswer, 0)
	rig.answers(t, "explain", explainAnswer, 0)
	_, ts, _ := graphsDesk(t, t.TempDir(), rig.bin)
	// A path from the page is not read: the extra members name nothing.
	status, data := graphGet(t, ts, "/api/graphs/plan?id=onboarding&path=/etc/passwd&graphPath=../x")
	if status != 200 {
		t.Fatalf("plan: %d %s", status, data)
	}
	if got := answerOf(t, data); string(got) != explainAnswer {
		t.Fatalf("the plan is not the runtime's bytes: %s", got)
	}
	asked := rig.asked(t)
	if len(asked) != 2 || asked[0] != "experimental graph list --config jpack.json --format json" ||
		asked[1] != "experimental graph explain onboarding.graph.json --config jpack.json --format json" {
		t.Fatalf("asked %q", asked)
	}
	// And afresh for each request.
	graphGet(t, ts, "/api/graphs/plan?id=onboarding")
	if rig.ran(t, "list") != 2 {
		t.Fatalf("the listing was not asked for afresh: %q", rig.asked(t))
	}
}

func TestGraphRow1AnIdThatNamesNoGraphIsRefusedBeforeExplain(t *testing.T) {
	rig := newGraphRig(t)
	rig.answers(t, "list", listAnswer, 0)
	rig.answers(t, "explain", explainAnswer, 0)
	_, ts, _ := graphsDesk(t, t.TempDir(), rig.bin)
	for _, id := range []string{"nope", "onboarding.graph.json", "../onboarding", "/etc/passwd", "Onboarding", "onboarding ", "-h", "onboarding\u2028"} {
		status, data := graphGet(t, ts, "/api/graphs/plan?id="+urlQuery(id))
		if status != http.StatusNotFound {
			t.Errorf("id %q: %d %s", id, status, data)
		}
	}
	if status, _ := graphGet(t, ts, "/api/graphs/plan"); status != http.StatusBadRequest {
		t.Errorf("no id: %d", status)
	}
	if n := rig.ran(t, "explain"); n != 0 {
		t.Fatalf("explain ran %d times for ids the listing does not have: %q", n, rig.asked(t))
	}
}

func urlQuery(s string) string {
	var out strings.Builder
	for _, b := range []byte(s) {
		out.WriteString("%" + strings.ToUpper(string("0123456789abcdef"[b>>4])+string("0123456789abcdef"[b&15])))
	}
	return out.String()
}

func TestGraphRow1AGraphWhosePathEscapesTheProjectIsNotPlanned(t *testing.T) {
	rig := newGraphRig(t)
	listing := func(path, detail string) string {
		entry := map[string]any{"id": "g", "path": path, "rowsDeclared": false}
		if detail != "" {
			entry["detail"] = detail
		}
		data, _ := json.Marshal(map[string]any{"outputVersion": "2", "command": "experimental graph list", "status": "resolved", "graphs": []any{entry}})
		return string(data)
	}
	rig.answers(t, "explain", explainAnswer, 0)
	_, ts, _ := graphsDesk(t, t.TempDir(), rig.bin)
	for _, c := range []struct{ name, path, detail string }{
		{"climbs out", "../outside.graph.json", ""},
		{"climbs out below a folder", "sub/../../outside.graph.json", ""},
		{"from a root", "/etc/outside.graph.json", ""},
		{"from a backslash", `\outside.graph.json`, ""},
		{"from a drive", `C:\outside.graph.json`, ""},
		{"empty", "", ""},
		{"the runtime could not read it", "inside.graph.json", "The file could not be read as one bounded regular file inside the configuration's own directory."},
	} {
		rig.answers(t, "list", listing(c.path, c.detail), 0)
		status, data := graphGet(t, ts, "/api/graphs/plan?id=g")
		if status != http.StatusConflict {
			t.Errorf("%s: %d %s", c.name, status, data)
		}
	}
	if n := rig.ran(t, "explain"); n != 0 {
		t.Fatalf("explain ran %d times for a path outside the project: %q", n, rig.asked(t))
	}
	// A name that starts with a dash is a name.
	rig.answers(t, "list", listing("-v.graph.json", ""), 0)
	if status, data := graphGet(t, ts, "/api/graphs/plan?id=g"); status != 200 {
		t.Fatalf("a dashed name: %d %s", status, data)
	}
	if asked := rig.asked(t); !strings.HasPrefix(asked[len(asked)-1], "experimental graph explain ./-v.graph.json ") {
		t.Fatalf("a name with a dash was passed as it is: %q", asked)
	}
	// Two entries under one id are not guessed between.
	two := `{"outputVersion":"2","command":"experimental graph list","status":"resolved","graphs":[{"id":"g","path":"a.json"},{"id":"g","path":"b.json"}]}`
	rig.answers(t, "list", two, 0)
	before := rig.ran(t, "explain")
	if status, _ := graphGet(t, ts, "/api/graphs/plan?id=g"); status != http.StatusConflict || rig.ran(t, "explain") != before {
		t.Fatalf("an ambiguous id was planned")
	}
}

// A project whose folder holds a space, a tab and a line separator, and
// answers that name it every way the runtime does.
func TestGraphRow1NoAbsolutePathReachesThePage(t *testing.T) {
	rig := newGraphRig(t)
	project := filepath.Join(t.TempDir(), "My Pro\tject\u2028 SECRET-NAME")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "Out side\tfolder\u2028 SECRET-OUTSIDE")
	quote := func(s string) string { return string(mustMarshal(s)) }
	insideAbs := filepath.Join(project, "onboarding.graph.json")
	outsideAbs := filepath.Join(outside, "o.graph.json")
	shown := displayedPath(insideAbs)
	validate := `{"outputVersion":"2","command":"experimental graph validate","status":"invalid","kind":"` + graphLabel + `","configPath":` + quote(filepath.Join(project, "jpack.json")) +
		`,"graphs":[{"id":"a","path":` + quote(insideAbs) + `,"rowsPath":` + quote(filepath.Join(outside, "r.json")) +
		`,"status":"invalid","diagnostics":[{"code":"JPS-GRAPH-ENTRY-READ","message":"The path ` + quote(shown)[1:len(quote(shown))-1] + ` resolves outside the configuration's own directory"}]},` +
		`{"id":"b","path":` + quote(outsideAbs) + `,"status":"invalid","diagnostics":[{"code":"X","message":"read ` + quote(outsideAbs)[1:len(quote(outsideAbs))-1] + `."},{"code":"Y","message":"see /opt/elsewhere-SECRET-OTHER/x.json now"}]},` +
		`{"id":"c","path":"sub dir/c.graph.json","status":"valid","diagnostics":[]}]}`
	explain := `{"outputVersion":"2","command":"experimental graph explain","status":"planned","kind":"` + graphLabel + `","configPath":` + quote(filepath.Join(project, "jpack.json")) +
		`,"graphPath":` + quote(insideAbs) + `,"steps":[{"order":1,"node":"n","path":` + quote(outsideAbs) + `,"detail":"The file ` + quote(shown)[1:len(quote(shown))-1] + ` could not be read","feeds":[{"from":"x","fact":"/screening/status"}]}]}`
	list := `{"outputVersion":"2","command":"experimental graph list","status":"resolved","graphs":[{"id":"a","path":"a.graph.json"}]}`
	rig.answers(t, "validate", validate, 1)
	rig.answers(t, "explain", explain, 0)
	rig.answers(t, "list", list, 0)
	_, ts, logged := graphsDesk(t, project, rig.bin)

	check := func(what string, data []byte) {
		t.Helper()
		for _, secret := range []string{"SECRET-NAME", "SECRET-OUTSIDE", "SECRET-OTHER", project, shown, outside, "My Pro", "Out side", filepath.Dir(outside)} {
			if strings.Contains(string(data), secret) {
				t.Errorf("%s: the page's answer holds %q: %s", what, secret, data)
			}
		}
	}
	status, data := graphGet(t, ts, "/api/graphs/findings")
	if status != 200 {
		t.Fatalf("findings: %d %s", status, data)
	}
	check("findings", data)
	var findings struct {
		Answer struct {
			Kind   string           `json:"kind"`
			Config string           `json:"configPath"`
			Graphs []map[string]any `json:"graphs"`
		} `json:"answer"`
	}
	if json.Unmarshal(data, &findings) != nil || findings.Answer.Kind != graphLabel {
		t.Fatalf("the label did not survive: %s", data)
	}
	// A relative path is shown as given, spaces and all.
	if findings.Answer.Graphs[2]["path"] != "sub dir/c.graph.json" || findings.Answer.Config == "" || strings.HasPrefix(findings.Answer.Config, "/") {
		t.Fatalf("a path member: %s", data)
	}
	if !strings.Contains(string(data), `"message":"read …."`) {
		t.Fatalf("a path named in a sentence was not replaced whole: %s", data)
	}
	if !strings.Contains(string(data), "resolves outside the configuration's own directory") {
		t.Fatalf("a sentence of the runtime's lost its words: %s", data)
	}
	status, data = graphGet(t, ts, "/api/graphs/plan?id=a")
	if status != 200 {
		t.Fatalf("plan: %d %s", status, data)
	}
	check("plan", data)
	// The fact pointer is not a path.
	if !strings.Contains(string(data), `"fact":"/screening/status"`) {
		t.Fatalf("a fact pointer was redacted as a path: %s", data)
	}
	// A sentence that names a path from outside the project keeps its words.
	if !strings.Contains(string(data), "The file … could not be read") {
		t.Fatalf("a detail lost its words: %s", data)
	}
	// The log of the owner's own desk keeps what the runtime said.
	if !strings.Contains(logged.String(), "SECRET-OUTSIDE") {
		t.Fatalf("the owner's log kept nothing of what was redacted")
	}
}

func TestGraphRow1AnAnswerOverTheBoundIsRefusedNotTruncated(t *testing.T) {
	rig := newGraphRig(t)
	over := `{"outputVersion":"2","command":"experimental graph validate","status":"valid","graphs":[],"pad":"` + strings.Repeat("x", runtimeAnswerLimit) + `"}`
	rig.answers(t, "validate", over, 0)
	_, ts, _ := graphsDesk(t, t.TempDir(), rig.bin)
	status, data := graphGet(t, ts, "/api/graphs/findings")
	if status != http.StatusInternalServerError || !strings.Contains(errorOf(t, data), "was larger than 65536 bytes") || strings.Contains(string(data), "xxxx") {
		t.Fatalf("an answer over the bound: %d %s", status, data)
	}
	// At the bound it is read: exactly 64 KiB, newline included.
	body := `{"outputVersion":"2","command":"experimental graph validate","status":"valid","graphs":[],"pad":"`
	fill := runtimeAnswerLimit - len(body) - len(`"}`) - 1
	atBound := body + strings.Repeat("x", fill) + `"}`
	rig.answers(t, "validate", atBound, 0)
	status, data = graphGet(t, ts, "/api/graphs/findings")
	if status != 200 || string(answerOf(t, data)) != atBound {
		t.Fatalf("an answer at the bound: %d %.200s", status, data)
	}
}

func TestGraphRow1ARuntimeThatHangsIsRefused(t *testing.T) {
	was := runtimeCommandTimeout
	runtimeCommandTimeout = time.Second
	t.Cleanup(func() { runtimeCommandTimeout = was })
	bin := filepath.Join(t.TempDir(), "jpack")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nwhile :; do :; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, ts, _ := graphsDesk(t, t.TempDir(), bin)
	ts.Client().Timeout = 15 * time.Second
	status, data := graphGet(t, ts, "/api/graphs/findings")
	if status != http.StatusInternalServerError || !strings.Contains(errorOf(t, data), "did not finish experimental graph validate --config jpack.json --format json within 1s") {
		t.Fatalf("a hung runtime: %d %s", status, data)
	}
}

func TestGraphRow1AnAnswerThatIsNotTheCommandsIsRefused(t *testing.T) {
	rig := newGraphRig(t)
	_, ts, _ := graphsDesk(t, t.TempDir(), rig.bin)
	for name, body := range map[string]string{
		"not JSON":        "no graphs here",
		"another command": `{"outputVersion":"2","command":"experimental graph list","status":"resolved"}`,
		"another version": `{"outputVersion":"3","command":"experimental graph validate","status":"valid"}`,
		"no status":       `{"outputVersion":"2","command":"experimental graph validate"}`,
	} {
		rig.answers(t, "validate", body, 0)
		if status, data := graphGet(t, ts, "/api/graphs/findings"); status != http.StatusInternalServerError {
			t.Errorf("%s: %d %s", name, status, data)
		}
	}
}

func TestGraphRow1ARuntimeRefusalIsShownInTheRuntimesWords(t *testing.T) {
	rig := newGraphRig(t)
	refusal := `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"experimental graph explain","status":"error","diagnostics":[{"code":"JPS-INPUT-READ","message":"The graph document could not be read as one bounded regular file or standard input stream."}]}`
	rig.answers(t, "list", listAnswer, 0)
	rig.answers(t, "explain", refusal, 4)
	_, ts, _ := graphsDesk(t, t.TempDir(), rig.bin)
	status, data := graphGet(t, ts, "/api/graphs/plan?id=onboarding")
	if status != 200 || string(answerOf(t, data)) != refusal {
		t.Fatalf("a refusal of the runtime's: %d %s", status, data)
	}
}

func TestGraphRow1TheRoutesAreNotOpenToAnonymousReaders(t *testing.T) {
	rig := newGraphRig(t)
	rig.answers(t, "validate", validateAnswer, 1)
	_, ts, _ := graphsDesk(t, t.TempDir(), rig.bin)
	for _, path := range []string{"/api/graphs/findings", "/api/graphs/plan?id=x"} {
		if status, _ := getAnon(t, ts, path); status != http.StatusUnauthorized && status != http.StatusForbidden {
			t.Errorf("%s without a credential: %d", path, status)
		}
	}
	if rig.ran(t, "validate") != 0 {
		t.Fatal("the runtime ran for a request nobody authorised")
	}
}

// The published runtime, over the runtime's own fixture project at
// configVersion "2", in a folder with a space, a tab and a line separator.
func TestGraphRow1WithThePublishedRuntime(t *testing.T) {
	bin := os.Getenv("JPACK_BIN")
	if bin == "" || !filepath.IsAbs(bin) {
		t.Skip("set JPACK_BIN to the published runtime")
	}
	project := filepath.Join(t.TempDir(), "Graph pro\tject\u2028 SECRET-REAL")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join("testdata", "graphs")
	entries, err := os.ReadDir(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(fixture, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(project, entry.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, ts, _ := graphsDesk(t, project, bin)

	// What the runtime prints, asked for directly, byte for byte.
	printed := func(args ...string) string {
		return strings.TrimSpace(string(jpackIn(t, bin, project, append([]string{"experimental", "graph"}, args...)...)))
	}
	status, data := graphGet(t, ts, "/api/graphs/findings")
	if status != 200 {
		t.Fatalf("findings: %d %s", status, data)
	}
	if got, want := string(answerOf(t, data)), printed("validate", "--config", "jpack.json", "--format", "json"); got != want {
		t.Fatalf("findings are not the runtime's bytes:\n got %s\nwant %s", got, want)
	}
	for _, word := range []string{`"status":"valid"`, `"kind":"` + graphLabel + `"`, `"graphSha256":"`} {
		if !strings.Contains(string(data), word) {
			t.Errorf("findings lack %s: %s", word, data)
		}
	}
	status, data = graphGet(t, ts, "/api/graphs/plan?id=onboarding")
	if status != 200 {
		t.Fatalf("plan: %d %s", status, data)
	}
	if got, want := string(answerOf(t, data)), printed("explain", "onboarding.graph.json", "--config", "jpack.json", "--format", "json"); got != want {
		t.Fatalf("the plan is not the runtime's bytes:\n got %s\nwant %s", got, want)
	}
	for _, word := range []string{`"status":"planned"`, `"kind":"` + graphLabel + `"`, `"steps":[`} {
		if !strings.Contains(string(data), word) {
			t.Errorf("the plan lacks %s: %s", word, data)
		}
	}
	if status, data := graphGet(t, ts, "/api/graphs/plan?id=nope"); status != http.StatusNotFound {
		t.Fatalf("an id the project does not declare: %d %s", status, data)
	}

	// A graph declared by an absolute path: the runtime gives the path back
	// verbatim, and none of it reaches the page.
	config, err := os.ReadFile(filepath.Join(project, "jpack.json"))
	if err != nil {
		t.Fatal(err)
	}
	var declared map[string]any
	if json.Unmarshal(config, &declared) != nil {
		t.Fatal("the fixture's jpack.json")
	}
	declared["graphs"].(map[string]any)["absolute"] = map[string]any{"path": filepath.Join(project, "onboarding.graph.json")}
	rewritten, _ := json.Marshal(declared)
	if err := os.WriteFile(filepath.Join(project, "jpack.json"), rewritten, 0o644); err != nil {
		t.Fatal(err)
	}
	status, data = graphGet(t, ts, "/api/graphs/findings")
	if status != 200 || !strings.Contains(string(data), `"id":"absolute"`) {
		t.Fatalf("findings with an absolute declaration: %d %s", status, data)
	}
	for _, secret := range []string{"SECRET-REAL", project, "Graph pro", displayedPath(project)} {
		if strings.Contains(string(data), secret) {
			t.Errorf("the page's answer holds %q: %s", secret, data)
		}
	}
	if status, data := graphGet(t, ts, "/api/graphs/plan?id=absolute"); status != http.StatusConflict || strings.Contains(string(data), "SECRET-REAL") {
		t.Fatalf("a graph declared by an absolute path: %d %s", status, data)
	}
}

func TestGraphRow1NothingRunsOverAnotherProjectsConfiguration(t *testing.T) {
	rig := newGraphRig(t)
	rig.answers(t, "validate", validateAnswer, 1)
	rig.answers(t, "list", listAnswer, 0)
	other := filepath.Join(t.TempDir(), "jpack.json")
	if err := os.WriteFile(other, []byte(`{"configVersion":"2"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(runtimeConfigEnv, other)
	_, ts, _ := graphsDesk(t, t.TempDir(), rig.bin)
	for _, path := range []string{"/api/graphs/findings", "/api/graphs/plan?id=onboarding"} {
		status, data := graphGet(t, ts, path)
		if status != http.StatusConflict || !strings.Contains(errorOf(t, data), "JPACK_CONFIG") || strings.Contains(string(data), other) {
			t.Errorf("%s: %d %s", path, status, data)
		}
	}
	if len(rig.asked(t)) != 0 {
		t.Fatalf("the runtime ran over another project's configuration: %q", rig.asked(t))
	}
}

// The listing cleans a path before it reads it; explain must be given that
// cleaned spelling, or a ".." after a symlinked folder leaves the project.
func TestGraphRow1AListedPathIsCleanedBeforeExplain(t *testing.T) {
	rig := newGraphRig(t)
	rig.answers(t, "explain", explainAnswer, 0)
	_, ts, _ := graphsDesk(t, t.TempDir(), rig.bin)
	for listed, want := range map[string]string{
		"sub/link/../onb.graph.json": "sub/onb.graph.json",
		"./a//b/../c.graph.json":     "a/c.graph.json",
		"sub/./g.graph.json":         "sub/g.graph.json",
		"x/../-v.graph.json":         "./-v.graph.json",
	} {
		data, _ := json.Marshal(map[string]any{"outputVersion": "2", "command": "experimental graph list", "status": "resolved", "graphs": []any{map[string]any{"id": "g", "path": listed}}})
		rig.answers(t, "list", string(data), 0)
		if status, body := graphGet(t, ts, "/api/graphs/plan?id=g"); status != 200 {
			t.Fatalf("%s: %d %s", listed, status, body)
		}
		asked := rig.asked(t)
		if got := asked[len(asked)-1]; !strings.HasPrefix(got, "experimental graph explain "+want+" --config") {
			t.Errorf("%s: asked %q, want the cleaned %q", listed, got, want)
		}
	}
	for _, listed := range []string{"a/../../x.graph.json", ".."} {
		data, _ := json.Marshal(map[string]any{"outputVersion": "2", "command": "experimental graph list", "status": "resolved", "graphs": []any{map[string]any{"id": "g", "path": listed}}})
		rig.answers(t, "list", string(data), 0)
		before := rig.ran(t, "explain")
		if status, _ := graphGet(t, ts, "/api/graphs/plan?id=g"); status != http.StatusConflict || rig.ran(t, "explain") != before {
			t.Errorf("%s was planned", listed)
		}
	}
}

func TestGraphRow1AnEmptyAnswerIsNotAnAnswer(t *testing.T) {
	rig := newGraphRig(t)
	rig.answers(t, "validate", "", 0)
	_, ts, _ := graphsDesk(t, t.TempDir(), rig.bin)
	status, data := graphGet(t, ts, "/api/graphs/findings")
	if status != http.StatusInternalServerError || !strings.Contains(errorOf(t, data), "did not answer as documented") || strings.Contains(string(data), `"answer"`) {
		t.Fatalf("an empty answer: %d %s", status, data)
	}
}

// With the published runtime: a ".." after a symlinked folder, and absolute
// paths outside the project in a folder named with a space.
func TestGraphRow1WithThePublishedRuntimeEscapes(t *testing.T) {
	bin := os.Getenv("JPACK_BIN")
	if bin == "" || !filepath.IsAbs(bin) {
		t.Skip("set JPACK_BIN to the published runtime")
	}
	project := t.TempDir()
	outside := filepath.Join(t.TempDir(), "Out side")
	fixture := filepath.Join("testdata", "graphs")
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"sanctions-screening-0.1.0.pack.json", "vendor-onboarding-0.1.0.pack.json", "onboarding.graph.json", "onboarding.rows.json"} {
		write(filepath.Join(project, name), read(name))
	}
	// sub/onb.graph.json is inside; <outside>/deep/../onb.graph.json is another file.
	write(filepath.Join(project, "sub", "onb.graph.json"), read("onboarding.graph.json"))
	write(filepath.Join(outside, "onb.graph.json"), strings.Replace(read("onboarding.graph.json"), "vendor-onboarding-flow", "outside-secret-graph", 1))
	if err := os.MkdirAll(filepath.Join(outside, "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "deep"), filepath.Join(project, "sub", "link")); err != nil {
		t.Skip("no symlinks here")
	}
	config := map[string]any{"configVersion": "2",
		"packs": map[string]any{
			"sanctions-screening": map[string]any{"path": "sanctions-screening-0.1.0.pack.json"},
			"vendor-onboarding":   map[string]any{"path": "vendor-onboarding-0.1.0.pack.json"},
			"far":                 map[string]any{"path": filepath.Join(outside, "SECRET-PACK", "v.pack.json")},
		},
		"graphs": map[string]any{
			"dotdot":     map[string]any{"path": "sub/link/../onb.graph.json"},
			"absmissing": map[string]any{"path": filepath.Join(outside, "SECRET-TAIL", "missing.graph.json")},
		}}
	data, _ := json.Marshal(config)
	write(filepath.Join(project, "jpack.json"), string(data))
	_, ts, _ := graphsDesk(t, project, bin)

	status, body := graphGet(t, ts, "/api/graphs/plan?id=dotdot")
	if strings.Contains(string(body), "outside-secret-graph") || (status == 200 && !strings.Contains(string(body), "vendor-onboarding-flow")) {
		t.Fatalf("the plan of a file outside the project: %d %s", status, body)
	}
	status, body = graphGet(t, ts, "/api/graphs/plan?id=absmissing")
	if status != http.StatusConflict || strings.Contains(string(body), "SECRET") || strings.Contains(string(body), "Out side") || strings.Contains(string(body), "side/") {
		t.Fatalf("an absolute graph path outside the project: %d %s", status, body)
	}
	status, body = graphGet(t, ts, "/api/graphs/findings")
	if status != 200 || strings.Contains(string(body), "SECRET") || strings.Contains(string(body), "side/") {
		t.Fatalf("findings with absolute paths outside the project: %d %s", status, body)
	}
}
