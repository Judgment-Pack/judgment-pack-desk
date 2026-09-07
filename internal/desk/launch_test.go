package desk

// The default project: the member that says which one, the two ends of the
// `PUT` that writes it, and the launch decision it feeds.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

/* The launch decision -------------------------------------------------------*/

func TestResolveProjectDirPrefersTheArgument(t *testing.T) {
	// The command line is the more specific statement, and a configured
	// default that could override one would be a desk nobody can point
	// somewhere else.
	chosen, err := resolveProjectDir("/on/the/command/line", deskLaunchFile{
		path: "/config/desk.json", file: "/configured/jpack-desk.json"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if chosen.dir != "/on/the/command/line" {
		t.Errorf("resolved %q, want the argument", chosen.dir)
	}
}

func TestResolveProjectDirTakesTheDirectoryOfTheConfiguredFile(t *testing.T) {
	project, file := aProject(t)
	chosen, err := resolveProjectDir("", deskLaunchFile{path: "/config/desk.json", file: file})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if chosen.dir != project {
		t.Errorf("resolved %q, want %q", chosen.dir, project)
	}
	// The identities travel with the decision, so the pinning can prove it is
	// still the directory that was validated.
	if chosen.dirInfo == nil || chosen.fileInfo == nil {
		t.Error("a configured default carried no identity to pin against")
	}
}

func TestResolveProjectDirIsTheCurrentDirectoryWithNeither(t *testing.T) {
	// The default this desk has always had, and the state most desks are in.
	chosen, err := resolveProjectDir("", deskLaunchFile{path: "/config/desk.json"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if chosen.dir != "." {
		t.Errorf("resolved %q, want the current directory", chosen.dir)
	}
	// And with no configuration file at all, which is the same answer.
	if other, err := resolveProjectDir("", deskLaunchFile{}); err != nil || other.dir != "." {
		t.Errorf("with nothing at all: %q %v", other.dir, err)
	}
}

func TestAConfiguredDefaultThisHostCannotOpenRefusesTheLaunch(t *testing.T) {
	// **Never a silent fall back to the current directory.** Somebody who
	// configured a default and got some other project would have no way to see
	// that the member they wrote was ignored, so an unusable one is named.
	dir, _ := aProject(t)
	notAFile := filepath.Join(dir, "sub")
	if err := os.MkdirAll(notAFile, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	elsewhere := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(elsewhere, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	link := filepath.Join(dir, "linked-away")
	if err := os.MkdirAll(link, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	linked := filepath.Join(link, projectConfigName)
	if err := os.Symlink(elsewhere, linked); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, each := range []struct {
		what string
		file string
		says string
	}{
		{"a file that is not there", filepath.Join(dir, "gone", projectConfigName), "resolved"},
		{"a file whose parent is not there either",
			filepath.Join(dir, "gone", "x", projectConfigName), "resolved"},
		{"a directory", notAFile, "regular file"},
		{"a link to another name", linked, "rather than a"},
		{"the filesystem root", "/" + projectConfigName, "filesystem root"},
		{"a path this host reads as relative", `C:\p\` + projectConfigName, "absolute path"},
	} {
		t.Run(each.what, func(t *testing.T) {
			_, err := resolveProjectDir("", deskLaunchFile{path: "/config/desk.json", file: each.file})
			if err == nil {
				t.Fatalf("%s was honoured", each.what)
			}
			for _, expected := range []string{"project.file", "/config/desk.json", each.says} {
				if !strings.Contains(err.Error(), expected) {
					t.Errorf("the refusal does not name %q: %v", expected, err)
				}
			}
		})
	}
}

func TestAWindowsShapedDefaultRefusesRatherThanOpeningTheLaunchDirectory(t *testing.T) {
	// **The shared corpus proves decoder parity, not that a value is
	// actionable on the host that launches.** A drive-letter path is one path
	// component to `path/filepath` here, so without the host's own
	// absoluteness check the desk resolves it **relative to wherever it was
	// launched** — the exact accident this member exists to remove.
	//
	// **Round 2 found the first version of this proving nothing.** It used the
	// backslash fixture and a temporary directory with nothing in it, so a
	// desk with the `IsAbs` check removed still refused — at the basename
	// check, for a path that does not exist. What it asserted was a diagnostic
	// word. So the forward-slash spelling is used too, and the tree it names is
	// **built inside the launch directory**: with the check removed the desk
	// opens `<cwd>/C:/p`, and this fails on what it observed rather than on
	// what the message said.
	if runtime.GOOS == "windows" {
		t.Skip("the shape is native here")
	}
	// **Read before the working directory moves**, because the corpus is found
	// relative to this package and `t.Chdir` would put it out of reach.
	fixtures := map[string]string{}
	for _, name := range []string{
		"accepted-project-file-windows-slashes",
		"accepted-project-file-windows",
	} {
		fixtures[name] = readAcceptedFixture(t, name)
	}
	elsewhere := t.TempDir()
	// `<cwd>/C:/p/jpack-desk.json`, so a launch-directory-relative resolution
	// finds a real, well-named project rather than nothing.
	relative := filepath.Join(elsewhere, "C:", "p")
	if err := os.MkdirAll(relative, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(relative, projectConfigName), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Chdir(elsewhere)

	for fixture, text := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			config := t.TempDir()
			writeLaunchConfig(t, config, text)
			got, err := ResolveProjectDir("", config)
			if err == nil {
				t.Fatalf("the Windows-shaped default resolved to %q", got)
			}
			// **What it must not have done**, said as itself: the launch
			// directory's own tree is what a relative resolution finds.
			if got != "" {
				t.Errorf("it resolved to %q", got)
			}
			resolvedRelative, _ := filepath.EvalSymlinks(relative)
			if got == relative || got == resolvedRelative {
				t.Errorf("it opened the launch-directory-relative project %q", got)
			}
		})
	}
}

// readAcceptedFixture is a fixture the shared corpus **accepts**, so a test
// built on one cannot drift from what the decoders admit.
func readAcceptedFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir(t), name+".json"))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if decoded := decodeDeskFile(data); decoded.refused() {
		t.Fatalf("%s is no longer accepted: %v", name, decoded.Problems)
	}
	return string(data)
}

func TestResolveProjectDirReadsTheConfiguredFileThroughTheStore(t *testing.T) {
	// End to end, through the same custody-validated read every other read of
	// this file goes through: it names a directory this desk will then serve
	// out of, so a file anybody else could have written must not choose it.
	config := t.TempDir()
	project, file := aProject(t)
	writeLaunchConfig(t, config, `{"deskConfigVersion":1,"project":{"file":`+quoted(file)+`}}`)
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

func TestAnAbsentDeskFileIsTheCurrentDirectoryAndNotAFailedRead(t *testing.T) {
	config := t.TempDir()
	got, err := ResolveProjectDir("", config)
	if err != nil || got != "." {
		t.Fatalf("with no desk-level file: %q %v", got, err)
	}
}

// writeLaunchConfig puts one desk-level file in a directory the store will
// accept, at the mode a checkout leaves.
func writeLaunchConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, deskConfigName), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", deskConfigName, err)
	}
}

// aProject puts an empty `jpack-desk.json` in a fresh directory and answers
// where it is, because a configured default now has to be a file that is
// actually there.
func aProject(t *testing.T) (dir, file string) {
	t.Helper()
	dir = t.TempDir()
	file = filepath.Join(dir, projectConfigName)
	if err := os.WriteFile(file, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write %s: %v", projectConfigName, err)
	}
	return dir, file
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
	// The only project a page may nominate: the one this desk is running in.
	thisProject := filepath.Join(s.projectDir, projectConfigName)

	digest, _ := deskConfigDigest(t, ts)
	status, body := putMembers(t, ts, map[string]any{
		"assistant": json.RawMessage(geminiAssistant), "ifMatch": digest})
	if status != http.StatusOK {
		t.Fatalf("assistant: %d %v", status, body)
	}

	digest, _ = deskConfigDigest(t, ts)
	status, body = putMembers(t, ts, map[string]any{
		"project": json.RawMessage(`{"file":` + quoted(thisProject) + `}`),
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
	if project["file"] != thisProject {
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
	if project["file"] != thisProject {
		t.Errorf("the project slot did not survive an assistant write: %v", body["project"])
	}
	// Read off the disk, not off the answer: the file is what the next launch
	// reads.
	_, data, err := s.readDeskFile()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if decoded := decodeDeskFile(data); decoded.ProjectFile != thisProject {
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
	s, ts, _ := assistantServerIn(t, config)
	// **The project this desk is running in**, which is the only one a page
	// may nominate. Its own configuration file has to exist for the launch to
	// honour it, so it is written here.
	file := filepath.Join(s.projectDir, projectConfigName)
	if err := os.WriteFile(file, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	digest, _ := deskConfigDigest(t, ts)
	status, body := putMembers(t, ts, map[string]any{
		"project": json.RawMessage(`{"file":` + quoted(file) + `}`),
		"ifMatch": digest})
	if status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}
	opened, err := ResolveProjectDir("", config)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if opened != s.projectDir {
		t.Errorf("the next launch would open %q, want %q", opened, s.projectDir)
	}
}

/* What a page may persist, and what it may not ----------------------------- */

func TestAPageMayNominateOnlyTheProjectThisDeskIsRunningIn(t *testing.T) {
	// **The key-retarget class, in the shape this member takes it.** The desk
	// pins one project root and serves the file API through it; `project.file`
	// chooses the root of the *next* launch. A page that could write any path
	// could hand its successor an authority the page never had — round 1's
	// example is `/jpack-desk.json`, which would pin `/` and serve the host —
	// so the page may write this project's own file, or null, and nothing else.
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, `{"deskConfigVersion":1}`)
	before, _ := deskConfigDigest(t, ts)

	for _, file := range []string{
		`"/jpack-desk.json"`,
		`"/etc/jpack-desk.json"`,
		`"` + filepath.Join(t.TempDir(), projectConfigName) + `"`,
	} {
		status, body := putMembers(t, ts, map[string]any{
			"project": json.RawMessage(`{"file":` + file + `}`), "ifMatch": before})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status %d, body %v", file, status, body)
		}
		if body["code"] != CodeDeskConfigRefused {
			t.Errorf("%s: code %v", file, body["code"])
		}
		problems, _ := body["problems"].([]any)
		if len(problems) != 1 {
			t.Fatalf("%s: problems %v", file, body["problems"])
		}
		first, _ := problems[0].(map[string]any)
		if first["key"] != "project.file" {
			t.Errorf("%s: refused by %v", file, first["key"])
		}
		if first["reason"] != pageMayNominateOnlyThisProject {
			t.Errorf("%s: reason %v", file, first["reason"])
		}
	}
	// **Nothing was written by any of them**, which is the half that matters:
	// a refusal that had already landed the value would be no refusal at all.
	if now, present := deskConfigDigest(t, ts); now != before || !present {
		t.Errorf("a refused nomination changed the file: %q, was %q", now, before)
	}
}

func TestAPageMayNominateThisProjectAndMayWithdrawADefault(t *testing.T) {
	// The two things a page may do: nominate the project it is already
	// serving, which grants it nothing it does not already have, and withdraw
	// a default, which takes authority away.
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, `{"deskConfigVersion":1}`)
	thisProject := filepath.Join(s.projectDir, projectConfigName)

	digest, _ := deskConfigDigest(t, ts)
	status, body := putMembers(t, ts, map[string]any{
		"project": json.RawMessage(`{"file":` + quoted(thisProject) + `}`), "ifMatch": digest})
	if status != http.StatusOK {
		t.Fatalf("nominating this project: %d %v", status, body)
	}
	project, _ := body["project"].(map[string]any)
	if project["file"] != thisProject {
		t.Errorf("project %v, want %q", body["project"], thisProject)
	}

	digest, _ = deskConfigDigest(t, ts)
	status, body = putMembers(t, ts, map[string]any{
		"project": json.RawMessage(`{"file":null}`), "ifMatch": digest})
	if status != http.StatusOK {
		t.Fatalf("withdrawing: %d %v", status, body)
	}
	project, _ = body["project"].(map[string]any)
	if project["file"] != nil {
		t.Errorf("project %v, want null", body["project"])
	}
}

func TestTheReviewsRootNominationDoesNotSurviveIntoTheNextLaunch(t *testing.T) {
	// End to end on the finding as it was written: the PUT the review named,
	// then the resolution the next argument-less launch would make. The root
	// must not be `/` — and, because the write was refused, the desk-level
	// file names no project at all.
	config := t.TempDir()
	_, ts, _ := assistantServerIn(t, config)
	digest, _ := deskConfigDigest(t, ts)
	status, _ := putMembers(t, ts, map[string]any{
		"project": json.RawMessage(`{"file":"/jpack-desk.json"}`), "ifMatch": digest})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("the root nomination was answered %d", status)
	}
	opened, err := ResolveProjectDir("", config)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if opened != "." {
		t.Errorf("the next launch would open %q, want the current directory", opened)
	}
}

func TestAHandEditedRootDefaultRefusesTheLaunchRatherThanPinningTheRoot(t *testing.T) {
	// The operator's own editor is not gated by the route above, so the
	// launch's own validation is what stands between a hand-written
	// `/jpack-desk.json` and a desk serving the host.
	config := t.TempDir()
	writeLaunchConfig(t, config, `{"deskConfigVersion":1,"project":{"file":"/jpack-desk.json"}}`)
	opened, err := ResolveProjectDir("", config)
	if err == nil {
		t.Fatalf("the root was honoured as %q", opened)
	}
	if !strings.Contains(err.Error(), "filesystem root") {
		t.Errorf("refusal: %v", err)
	}
}

func TestADeskLevelWriteStatesItsDigestOrIsRefused(t *testing.T) {
	// **An omitted `ifMatch` is not the empty sentinel.** Where the file is
	// absent the actual digest is the empty string too, so a body carrying no
	// `ifMatch` compared equal and created the file — a write with no
	// precondition, from a route whose whole argument is that the commit is
	// conditional.
	s, ts, _ := assistantServer(t)
	thisProject := filepath.Join(s.projectDir, projectConfigName)
	for _, body := range []map[string]any{
		{"project": json.RawMessage(`{"file":` + quoted(thisProject) + `}`)},
		{"assistant": json.RawMessage(`{"endpoint":null}`)},
	} {
		status, answer := putMembers(t, ts, body)
		if status != http.StatusBadRequest {
			t.Fatalf("status %d, body %v", status, answer)
		}
		if !strings.Contains(answer["error"].(string), "ifMatch") {
			t.Errorf("the refusal does not name the member: %v", answer["error"])
		}
	}
	// And no file was created by either, which is the state that made the
	// omission invisible in the first place.
	if _, err := os.Stat(s.deskConfigPath()); !os.IsNotExist(err) {
		t.Errorf("a write with no precondition created %s", s.deskConfigPath())
	}
	// The sentinel, stated, still creates one.
	status, answer := putMembers(t, ts, map[string]any{
		"assistant": json.RawMessage(`{"endpoint":null}`), "ifMatch": ""})
	if status != http.StatusOK {
		t.Fatalf("the stated sentinel was refused: %d %v", status, answer)
	}
}

/* Validated and pinned, or nothing ----------------------------------------- */

// swapAt installs a hook that renames `victim` away and puts a symlink to
// `replacement` in its place, exactly once, the first time it fires.
//
// This is the swap round 2 described, performed at the instant it would
// matter: after the launch has validated the configured file and the directory
// it is in, and before the directory is opened.
func swapAt(t *testing.T, victim, replacement string) {
	t.Helper()
	done := false
	testHookBeforePinningProject = func(string) {
		if done {
			return
		}
		done = true
		moved := victim + ".moved"
		if err := os.Rename(victim, moved); err != nil {
			t.Fatalf("rename: %v", err)
		}
		if err := os.Symlink(replacement, victim); err != nil {
			t.Fatalf("symlink: %v", err)
		}
	}
	t.Cleanup(func() { testHookBeforePinningProject = nil })
}

func TestALaunchPinsTheDirectoryItValidatedOrRefuses(t *testing.T) {
	// **The window round 2 found.** Validation returned the parent as a
	// *pathname* and the server resolved that pathname again, so a principal
	// who can rename inside the parent could move the validated directory away
	// and point its name at another tree between the two. The desk must serve
	// the directory whose `jpack-desk.json` it checked, or serve none.
	holder := t.TempDir()
	project := filepath.Join(holder, "a-project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(project, projectConfigName)
	if err := os.WriteFile(file, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// The tree the attacker would rather this desk served.
	elsewhere := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(elsewhere, projectConfigName), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(holder, "probe")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	config := t.TempDir()
	writeLaunchConfig(t, config, `{"deskConfigVersion":1,"project":{"file":`+quoted(file)+`}}`)
	swapAt(t, project, elsewhere)

	pinned, err := OpenProject("", config)
	if pinned != nil {
		defer pinned.Close()
	}
	// **Never the replacement**, which is the whole assertion. Either answer
	// is safe — the descriptor may still be the original directory in its new
	// location, or the identity check may refuse — and the tree the attacker
	// installed must not be it.
	resolvedElsewhere, _ := filepath.EvalSymlinks(elsewhere)
	if err == nil && pinned.Dir() == resolvedElsewhere {
		t.Fatalf("the desk pinned the replacement tree %q", pinned.Dir())
	}
	if err == nil {
		// It pinned something: prove it is the directory that was validated,
		// by identity rather than by name.
		moved, statErr := os.Lstat(project + ".moved")
		if statErr != nil {
			t.Fatalf("lstat: %v", statErr)
		}
		if !os.SameFile(moved, pinned.info) {
			t.Errorf("pinned %q, which is neither the validated directory nor a refusal",
				pinned.Dir())
		}
	}
}

func TestPinningRefusesADirectoryThatChangedUnderItsName(t *testing.T) {
	// The same window, closed at the other end: `OpenProjectRoot` inspects a
	// name and then opens it, and what it holds afterwards has to be what it
	// inspected. Here the validated identity is a different directory
	// altogether, which is what a completed swap looks like to the check.
	one := t.TempDir()
	two := t.TempDir()
	pinned, err := OpenProjectRoot(one)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer pinned.Close()
	other, err := os.Lstat(two)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	chosen := projectChoice{dir: one, dirInfo: other, fileInfo: other}
	if err := chosen.stillTheOneValidated(pinned); err == nil {
		t.Fatal("a directory that is not the one validated was accepted")
	}
}

func TestPinningRefusesAConfigurationFileReplacedInsideTheSameDirectory(t *testing.T) {
	// The directory may be the one that was validated and the *file* that
	// chose it may not be. Both are checked, and this is the second.
	dir, file := aProject(t)
	pinned, err := OpenProjectRoot(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer pinned.Close()
	was, err := os.Lstat(file)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	// Replaced the way an editor replaces a file: the new one is created
	// while the old one still holds its inode, then renamed over. The name is
	// the same afterwards and the identity is not, which is the whole point.
	staged := file + ".new"
	if err := os.WriteFile(staged, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Rename(staged, file); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if now, err := os.Lstat(file); err != nil || os.SameFile(was, now) {
		t.Skipf("this filesystem reused the identity, so there is nothing to tell apart: %v", err)
	}
	chosen := projectChoice{dir: dir, dirInfo: pinned.info, fileInfo: was}
	if err := chosen.stillTheOneValidated(pinned); err == nil {
		t.Fatal("a replaced configuration file was accepted")
	}
	// And the file that was validated is accepted.
	now, err := os.Lstat(file)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	chosen.fileInfo = now
	if err := chosen.stillTheOneValidated(pinned); err != nil {
		t.Errorf("the validated pair was refused: %v", err)
	}
}

func TestAnArgumentIsPinnedThroughTheSameCheck(t *testing.T) {
	// Both launch shapes pin what they validated. An argument names a
	// directory this desk was told to serve, so there is no earlier identity
	// to compare — but the descriptor must still be the directory that was
	// inspected, which is `OpenProjectRoot`'s own `SameFile`.
	dir, _ := aProject(t)
	pinned, err := OpenProject(dir, t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer pinned.Close()
	resolved, _ := filepath.EvalSymlinks(dir)
	if pinned.Dir() != resolved {
		t.Errorf("pinned %q, want %q", pinned.Dir(), resolved)
	}
	held, err := os.Lstat(resolved)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if !os.SameFile(held, pinned.info) {
		t.Error("the descriptor is not the directory that was inspected")
	}
	// A file is not a project directory.
	if _, err := OpenProjectRoot(filepath.Join(dir, projectConfigName)); err == nil {
		t.Error("a regular file was pinned as a project")
	}
}

func TestTheServerServesTheRootItWasHanded(t *testing.T) {
	// The descriptor crosses the boundary, not a name for one — so there is no
	// second resolution inside `New` for anything to change under.
	dir, _ := aProject(t)
	pinned, err := OpenProjectRoot(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	s, err := New(Config{Root: pinned, JpackBin: "jpack", Token: testToken,
		DeskConfigDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()
	if s.projectDir != pinned.Dir() {
		t.Errorf("the server serves %q, want %q", s.projectDir, pinned.Dir())
	}
	if s.root != pinned.root {
		t.Error("the server opened a root of its own rather than serving the one it was handed")
	}
}

func TestAHandedRootIsReleasedWhenTheServerRefusesToStart(t *testing.T) {
	// This server owns the descriptor it was handed, on failure as on success:
	// a caller that also closed it would double-close, and one that closed
	// nothing would leak a descriptor at startup.
	dir, _ := aProject(t)
	pinned, err := OpenProjectRoot(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := New(Config{Root: pinned, JpackBin: "jpack"}); err == nil {
		t.Fatal("a server with no token was built")
	}
	if _, err := pinned.root.Stat("."); err == nil {
		t.Error("the descriptor was still open after the refusal")
	}
}

func TestAnEmptyProjectObjectIsRefusedRatherThanTreatedAsAWithdrawal(t *testing.T) {
	// **An omission is not a withdrawal.** The contract spells clearing as
	// `{"file": null}`; `{}` replaced the member with an empty object, which
	// the decoder reads as no default — so a client that meant nothing by
	// leaving the member out silently cleared an operator's own setting.
	s, ts, _ := assistantServer(t)
	// A foreign default, hand-edited: exactly what the page may not touch.
	foreign := filepath.Join(t.TempDir(), projectConfigName)
	writeDeskConfig(t, s, `{"deskConfigVersion":1,"project":{"file":`+quoted(foreign)+`}}`)
	_, before, err := s.readDeskFile()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	digest, _ := deskConfigDigest(t, ts)

	status, body := putMembers(t, ts, map[string]any{
		"project": json.RawMessage(`{}`), "ifMatch": digest})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, body %v", status, body)
	}
	problems, _ := body["problems"].([]any)
	if len(problems) != 1 {
		t.Fatalf("problems %v", body["problems"])
	}
	first, _ := problems[0].(map[string]any)
	if first["key"] != "project.file" || first["reason"] != projectFileMustBeStated {
		t.Errorf("refused by %v: %v", first["key"], first["reason"])
	}
	// **Byte for byte**, because the failure this replaces was a silent edit
	// rather than a wrong answer.
	_, after, err := s.readDeskFile()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("the file changed:\n%s\nwas\n%s", after, before)
	}

	// And the positive it must not take with it: a stated null still withdraws.
	status, body = putMembers(t, ts, map[string]any{
		"project": json.RawMessage(`{"file":null}`), "ifMatch": digest})
	if status != http.StatusOK {
		t.Fatalf("a stated null was refused: %d %v", status, body)
	}
	project, _ := body["project"].(map[string]any)
	if project["file"] != nil {
		t.Errorf("project %v, want null", body["project"])
	}
}

func TestOnlyTheExactSpellingThisDeskReportedIsAccepted(t *testing.T) {
	// **One value, and it is the one this desk handed the page.** Anything
	// that makes two spellings compare equal is a second rule about which
	// spellings mean it — and the alternate one is what gets stored, so the
	// file then carries a string this desk never reported. Round 2 found a
	// `TrimSpace`; every other alternate spelling was already refused, and
	// this holds all of them together.
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, `{"deskConfigVersion":1}`)
	exact := s.projectPaths().File
	digest, _ := deskConfigDigest(t, ts)

	// A symlinked route to the same file, so the table is not only lexical.
	linked := filepath.Join(t.TempDir(), "link")
	symlinked := ""
	if err := os.Symlink(s.projectDir, linked); err == nil {
		symlinked = filepath.Join(linked, projectConfigName)
	}

	spellings := []string{
		" " + exact,
		exact + " ",
		"\t" + exact + "\n",
		filepath.Dir(exact) + "/./" + projectConfigName,
		filepath.Dir(exact) + "/../" + filepath.Base(filepath.Dir(exact)) + "/" + projectConfigName,
		filepath.Dir(exact) + "//" + projectConfigName,
		exact + "/",
		strings.ToUpper(exact),
	}
	if symlinked != "" {
		spellings = append(spellings, symlinked)
	}
	for _, spelling := range spellings {
		if spelling == exact {
			t.Fatalf("%q is the exact spelling, so it proves nothing here", spelling)
		}
		status, body := putMembers(t, ts, map[string]any{
			"project": json.RawMessage(`{"file":` + quoted(spelling) + `}`), "ifMatch": digest})
		if status != http.StatusUnprocessableEntity {
			t.Errorf("%q: status %d, body %v", spelling, status, body)
			continue
		}
		problems, _ := body["problems"].([]any)
		first, _ := problems[0].(map[string]any)
		if first["key"] != "project.file" {
			t.Errorf("%q: refused by %v", spelling, first["key"])
		}
	}
	// The file is untouched by all of them, and the exact spelling still works.
	if now, _ := deskConfigDigest(t, ts); now != digest {
		t.Error("a refused spelling changed the file")
	}
	if status, body := putMembers(t, ts, map[string]any{
		"project": json.RawMessage(`{"file":` + quoted(exact) + `}`), "ifMatch": digest,
	}); status != http.StatusOK {
		t.Fatalf("the exact spelling was refused: %d %v", status, body)
	}
	// And what landed is that spelling and not a normalisation of one.
	_, data, err := s.readDeskFile()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if decoded := decodeDeskFile(data); decoded.ProjectFile != exact {
		t.Errorf("stored %q, want %q", decoded.ProjectFile, exact)
	}
}
