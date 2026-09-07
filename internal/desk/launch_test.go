package desk

// The default project: the member that says which one, the two ends of the
// `PUT` that writes it, and the launch decision it feeds.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/* The launch decision -------------------------------------------------------*/

func TestResolveProjectDirPrefersTheArgument(t *testing.T) {
	// The command line is the more specific statement, and a configured
	// default that could override one would be a desk nobody can point
	// somewhere else.
	got, err := resolveProjectDir("/on/the/command/line", deskLaunchFile{
		path: "/config/desk.json", file: "/configured/jpack-desk.json"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "/on/the/command/line" {
		t.Errorf("resolved %q, want the argument", got)
	}
}

func TestResolveProjectDirTakesTheDirectoryOfTheConfiguredFile(t *testing.T) {
	got, err := resolveProjectDir("", deskLaunchFile{
		path: "/config/desk.json", file: "/home/someone/a-project/jpack-desk.json"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "/home/someone/a-project" {
		t.Errorf("resolved %q, want the configured file's directory", got)
	}
}

func TestResolveProjectDirRefusesWithNeitherAndNamesTheMember(t *testing.T) {
	// **Not the current directory.** A project chosen by where the process
	// happened to start is a project nobody chose, and every consequence of
	// that choice is silent. The refusal has to be actionable, so it names the
	// file to write and the member to write in it.
	_, err := resolveProjectDir("", deskLaunchFile{path: "/config/desk.json"})
	if err == nil {
		t.Fatal("no argument and no configured file was accepted")
	}
	for _, expected := range []string{"/config/desk.json", "project.file", "jpack-desk.json"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("the refusal does not name %q: %v", expected, err)
		}
	}
}

func TestResolveProjectDirSaysSoWhereThereIsNoConfigurationFileAtAll(t *testing.T) {
	// A machine with no configuration directory has no path to name, and the
	// refusal says that rather than printing an empty one.
	_, err := resolveProjectDir("", deskLaunchFile{})
	if err == nil {
		t.Fatal("accepted with nothing at all")
	}
	if !strings.Contains(err.Error(), "desk configuration file") {
		t.Errorf("refusal: %v", err)
	}
}

func TestResolveProjectDirReadsTheConfiguredFileThroughTheStore(t *testing.T) {
	// End to end, through the same custody-validated read every other read of
	// this file goes through: it names a directory this desk will then serve
	// out of, so a file anybody else could have written must not choose it.
	config := t.TempDir()
	project := t.TempDir()
	writeLaunchConfig(t, config, `{"deskConfigVersion":1,"project":{"file":`+
		quoted(filepath.Join(project, "jpack-desk.json"))+`}}`)
	got, err := ResolveProjectDir("", config)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != project {
		t.Errorf("resolved %q, want %q", got, project)
	}
	// And the argument still wins over it.
	other := t.TempDir()
	if got, err := ResolveProjectDir(other, config); err != nil || got != other {
		t.Errorf("argument: %q %v", got, err)
	}
}

func TestARefusedDeskFileNamesNoProject(t *testing.T) {
	// Any problem refuses the whole file, so a good `project.file` beside an
	// unknown key chooses nothing — the same rule the probe is held to. The
	// refusal travels, because a desk that ignored the member and then said
	// "no project directory" would send a reader to look at a member that is
	// already there and correct.
	config := t.TempDir()
	writeLaunchConfig(t, config,
		`{"deskConfigVersion":1,"colour":"blue","project":{"file":"/p/jpack-desk.json"}}`)
	_, err := ResolveProjectDir("", config)
	if err == nil {
		t.Fatal("a refused file chose a project")
	}
	if !strings.Contains(err.Error(), "colour") {
		t.Errorf("the refusal does not name the problem: %v", err)
	}
}

func TestAnAbsentDeskFileIsTheUsageErrorAndNotAFailedRead(t *testing.T) {
	config := t.TempDir()
	_, err := ResolveProjectDir("", config)
	if err == nil {
		t.Fatal("accepted with no file at all")
	}
	if !strings.Contains(err.Error(), "project.file") {
		t.Errorf("refusal: %v", err)
	}
}

// writeLaunchConfig puts one desk-level file in a directory the store will
// accept, at the mode a checkout leaves.
func writeLaunchConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, deskConfigName), []byte(content), 0o644); err != nil {
		t.Fatalf("write desk.json: %v", err)
	}
}

// quoted is `%q` for a path, so a separator survives into the JSON.
func quoted(path string) string {
	encoded, _ := json.Marshal(path)
	return string(encoded)
}

/* The chassis' own facts, on the read side ---------------------------------*/

func TestDeskConfigReadCarriesTheProjectAndRuntimeItWasLaunchedWith(t *testing.T) {
	// Admin prints where the project's configuration file is and which binary
	// this desk runs, and must invent neither: a page joining a directory to a
	// file name would be naming a path on a filesystem it cannot see.
	s, ts, _ := assistantServer(t)
	for _, when := range []string{"absent", "present"} {
		if when == "present" {
			writeDeskConfig(t, s, `{"deskConfigVersion":1}`)
		}
		status, body := sendJSON(t, ts, http.MethodGet, "/api/desk-config", nil)
		if status != http.StatusOK {
			t.Fatalf("%s: status %d, body %v", when, status, body)
		}
		project, _ := body["project"].(map[string]any)
		if project["dir"] != s.projectDir {
			t.Errorf("%s: project.dir %v, want %q", when, project["dir"], s.projectDir)
		}
		wantFile := filepath.Join(s.projectDir, "jpack-desk.json")
		if project["file"] != wantFile {
			t.Errorf("%s: project.file %v, want %q", when, project["file"], wantFile)
		}
		runtime, _ := body["runtime"].(map[string]any)
		if runtime["bin"] != s.cfg.JpackBin {
			t.Errorf("%s: runtime.bin %v, want %q", when, runtime["bin"], s.cfg.JpackBin)
		}
	}
}

func TestTheReportedProjectDirIsTheResolvedOne(t *testing.T) {
	// The chassis pins the project with its symlinks resolved, and everything
	// it does operates on that path. Reporting the one it was handed would
	// have Admin name a directory the file API never touches.
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	s, err := New(Config{
		ProjectDir: link, JpackBin: "jpack", Token: testToken, DeskConfigDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()
	ts := httptest.NewServer(s)
	defer ts.Close()
	_, body := sendJSON(t, ts, http.MethodGet, "/api/desk-config", nil)
	project, _ := body["project"].(map[string]any)
	resolved, _ := filepath.EvalSymlinks(real)
	if project["dir"] != resolved {
		t.Errorf("project.dir %v, want the resolved %q", project["dir"], resolved)
	}
}

/* The write, which now carries two members ---------------------------------*/

// putMembers sends one desk-level write with whichever members are given.
func putMembers(
	t *testing.T, ts *httptest.Server, body map[string]any,
) (int, map[string]any) {
	t.Helper()
	return sendJSON(t, ts, http.MethodPut, "/api/desk-config", body)
}

func TestDeskConfigWriteReplacesOnlyTheMembersItWasSent(t *testing.T) {
	// **Two Admin cards write two members of one file**, and neither sends the
	// other's. A member that is absent from the request is carried across with
	// every other member of the file rather than replaced, removed, or
	// re-rendered.
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, `{"deskConfigVersion":1}`)

	digest, _ := deskConfigDigest(t, ts)
	status, body := putMembers(t, ts, map[string]any{
		"assistant": json.RawMessage(geminiAssistant), "ifMatch": digest})
	if status != http.StatusOK {
		t.Fatalf("assistant: %d %v", status, body)
	}

	digest, _ = deskConfigDigest(t, ts)
	status, body = putMembers(t, ts, map[string]any{
		"project": json.RawMessage(`{"file":"/home/someone/a-project/jpack-desk.json"}`),
		"ifMatch": digest})
	if status != http.StatusOK {
		t.Fatalf("project: %d %v", status, body)
	}
	// The assistant this write never mentioned is still exactly what the first
	// one put there.
	assistant, _ := body["assistant"].(map[string]any)
	endpoint, _ := assistant["endpoint"].(map[string]any)
	if endpoint == nil || endpoint["kind"] != "gemini" {
		t.Fatalf("the assistant slot did not survive a project write: %v", body["assistant"])
	}
	project, _ := body["project"].(map[string]any)
	if project["file"] != "/home/someone/a-project/jpack-desk.json" {
		t.Errorf("project %v", body["project"])
	}

	// And the other direction: an assistant write leaves the project alone.
	digest, _ = deskConfigDigest(t, ts)
	status, body = putMembers(t, ts, map[string]any{
		"assistant": json.RawMessage(`{"endpoint":null}`), "ifMatch": digest})
	if status != http.StatusOK {
		t.Fatalf("assistant again: %d %v", status, body)
	}
	project, _ = body["project"].(map[string]any)
	if project["file"] != "/home/someone/a-project/jpack-desk.json" {
		t.Errorf("the project slot did not survive an assistant write: %v", body["project"])
	}
	// Read off the disk, not off the answer: the file is what the next launch
	// reads.
	_, data, err := s.readDeskFile()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if decoded := decodeDeskFile(data); decoded.ProjectFile !=
		"/home/someone/a-project/jpack-desk.json" {
		t.Errorf("on disk: %q", decoded.ProjectFile)
	}
}

func TestDeskConfigWriteRefusesABodyNamingNeitherMember(t *testing.T) {
	// A conditional commit that would change nothing is a request with no
	// meaning, and answering it 200 would report a write that did not happen.
	_, ts, _ := assistantServer(t)
	digest, _ := deskConfigDigest(t, ts)
	status, body := putMembers(t, ts, map[string]any{"ifMatch": digest})
	if status != http.StatusBadRequest {
		t.Fatalf("status %d, body %v", status, body)
	}
	if !strings.Contains(body["error"].(string), "project") {
		t.Errorf("the refusal does not name the members: %v", body["error"])
	}
}

func TestDeskConfigWriteRefusesAProjectFileTheDecoderWouldRefuse(t *testing.T) {
	// The composed bytes are decoded before any of them reach the disk, so a
	// relative path refuses the write with the decoder's own key — and this
	// desk cannot store a default project its own launch would then refuse.
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, `{"deskConfigVersion":1}`)
	digest, _ := deskConfigDigest(t, ts)
	for _, file := range []string{
		`"a-project/jpack-desk.json"`,
		`"/home/someone/a-project"`,
		`12`,
	} {
		status, body := putMembers(t, ts, map[string]any{
			"project": json.RawMessage(`{"file":` + file + `}`), "ifMatch": digest})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status %d, body %v", file, status, body)
		}
		problems, _ := body["problems"].([]any)
		if len(problems) == 0 {
			t.Fatalf("%s: no problems named", file)
		}
		first, _ := problems[0].(map[string]any)
		if first["key"] != "project.file" {
			t.Errorf("%s: refused by %v", file, first["key"])
		}
	}
	// Nothing was written by any of them.
	if now, _ := deskConfigDigest(t, ts); now != digest {
		t.Error("a refused write changed the file")
	}
}

func TestAWrittenDefaultProjectIsTheOneTheNextLaunchOpens(t *testing.T) {
	// The two halves meeting: what the card saves is what `ResolveProjectDir`
	// reads, through one file and one contract.
	config := t.TempDir()
	project := t.TempDir()
	s, ts, _ := assistantServerIn(t, config)
	_ = s
	digest, _ := deskConfigDigest(t, ts)
	status, body := putMembers(t, ts, map[string]any{
		"project": json.RawMessage(`{"file":` +
			quoted(filepath.Join(project, "jpack-desk.json")) + `}`),
		"ifMatch": digest})
	if status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}
	opened, err := ResolveProjectDir("", config)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if opened != project {
		t.Errorf("the next launch would open %q, want %q", opened, project)
	}
}
