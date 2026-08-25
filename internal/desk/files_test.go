package desk

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// filesServer stands one chassis up over a fresh project tree. No runtime
// binary is involved: the file API never talks to one, which is the whole
// reason it can be tested here rather than end to end.
func filesServer(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	project := t.TempDir()
	s, err := New(Config{
		ProjectDir: project,
		JpackBin:   "jpack",
		Token:      testToken,
		Logger:     log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return s, ts, project
}

func writeProjectFile(t *testing.T, project, rel, content string) string {
	t.Helper()
	abs := filepath.Join(project, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return abs
}

func getJSON(t *testing.T, ts *httptest.Server, path string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

// putJSONNoFatal is putJSON for a goroutine: it reports a transport failure as
// a zero status rather than calling t.Fatalf, which is only defined on the
// test's own goroutine — there it stops that goroutine and the test carries on
// believing everything is fine.
func putJSONNoFatal(ts *httptest.Server, req WriteRequest) (int, map[string]any) {
	payload, err := json.Marshal(req)
	if err != nil {
		return 0, nil
	}
	r, err := http.NewRequest(http.MethodPut, ts.URL+"/api/file?token="+testToken, bytes.NewReader(payload))
	if err != nil {
		return 0, nil
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

func putJSON(t *testing.T, ts *httptest.Server, req WriteRequest) (int, map[string]any) {
	t.Helper()
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r, err := http.NewRequest(http.MethodPut, ts.URL+"/api/file?token="+testToken, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

/* Containment ------------------------------------------------------------- */

// TestWireRelativePathRefusesOnItsOwn names the lexical layer.
//
// Two independent things stand between a client path and the disk: this, and
// the pinned root. The root refuses a superset, so a test that went through a
// handler and asked only "was it refused?" stays green with this layer deleted.
// Asking it directly is the only way to notice it has stopped running.
func TestWireRelativePathRefusesOnItsOwn(t *testing.T) {
	refused := map[string]string{
		"empty":                 "",
		"the project itself":    ".",
		"bare dot-dot":          "..",
		"climbing out":          "../secret.json",
		"climbing through":      "packs/../../secret.json",
		"deep climb":            "a/b/c/../../../../secret.json",
		"absolute":              "/etc/passwd",
		"windows drive":         "C:/secret.json",
		"unc":                   "//host/share/secret.json",
		"backslash traversal":   `..\secret.json`,
		"backslash separator":   `packs\a.pack.json`,
		"nul byte":              "packs/a\x00.json",
		"a staging file":        ".jpack-desk-abc.tmp",
		"a staging file nested": "packs/.jpack-desk-abc.tmp",
	}
	for name, rel := range refused {
		t.Run("refused/"+name, func(t *testing.T) {
			if clean, err := wireRelativePath(rel); err == nil {
				t.Fatalf("wireRelativePath(%q) allowed %q", rel, clean)
			}
		})
	}
	allowed := map[string]string{
		"jpack.json":          "jpack.json",
		"packs/a.pack.json":   "packs/a.pack.json",
		"packs/../jpack.json": "jpack.json",
		"./packs/a.pack.json": "packs/a.pack.json",
		"packs//a.pack.json":  "packs/a.pack.json",
	}
	for rel, want := range allowed {
		t.Run("allowed/"+rel, func(t *testing.T) {
			clean, err := wireRelativePath(rel)
			if err != nil {
				t.Fatalf("wireRelativePath(%q): %v", rel, err)
			}
			if clean != want {
				t.Fatalf("cleaned to %q, want %q", clean, want)
			}
		})
	}
}

// TestSymlinkEscapeRefusedOverTheWire proves the containment is reached by the
// endpoints — on reads and on writes alike, which is the consistency this API
// promises.
func TestSymlinkEscapeRefusedOverTheWire(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	_, ts, project := filesServer(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.json")
	if err := os.WriteFile(secret, []byte(`{"secret":true}`), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	if err := os.Symlink(secret, filepath.Join(project, "escape.json")); err != nil {
		t.Fatalf("symlink file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "elsewhere")); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}

	for _, rel := range []string{"escape.json", "elsewhere/secret.json", "elsewhere/new.json"} {
		status, body := getJSON(t, ts, "/api/file?token="+testToken+"&path="+rel)
		if status != http.StatusForbidden && status != http.StatusNotFound {
			t.Fatalf("read %s: status %d, body %v", rel, status, body)
		}
		if message, ok := body["error"].(string); ok && strings.Contains(message, "secret") {
			t.Fatalf("the refusal describes what is outside the project: %v", message)
		}
		if status, _ := putJSON(t, ts, WriteRequest{Path: rel, Content: "{}", Override: true}); status == http.StatusOK {
			t.Fatalf("write %s succeeded", rel)
		}
	}
	// The file outside is untouched, which is what actually matters — a
	// refusal that had already written would be no refusal.
	after, err := os.ReadFile(secret)
	if err != nil || string(after) != `{"secret":true}` {
		t.Fatalf("the file outside the project changed: %q, %v", string(after), err)
	}
	// And it is not listed, with no target or size disclosed.
	_, listing := getJSON(t, ts, "/api/files?token="+testToken)
	for _, raw := range listing["files"].([]any) {
		if p := raw.(map[string]any)["path"].(string); p == "escape.json" || strings.HasPrefix(p, "elsewhere/") {
			t.Fatalf("a symlink was listed: %s", p)
		}
	}
}

// TestSymlinkSwapBetweenCheckAndUse is the time-of-check/time-of-use case.
//
// The hook fires at the one instant an attacker would need: the path has been
// validated and the operation has not yet happened. It replaces an approved
// directory with a symlink pointing outside the project — the swap that defeats
// any design that validates a pathname and then opens it by name.
//
// Every route must still refuse, or write nothing outside the root.
func TestSymlinkSwapBetweenCheckAndUse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}

	swapDirForSymlinkOut := func(t *testing.T, project, outside string) func(string) {
		t.Helper()
		var once sync.Once
		return func(string) {
			once.Do(func() {
				victim := filepath.Join(project, "packs")
				if err := os.RemoveAll(victim); err != nil {
					t.Errorf("remove: %v", err)
					return
				}
				if err := os.Symlink(outside, victim); err != nil {
					t.Errorf("symlink: %v", err)
				}
			})
		}
	}

	t.Run("GET", func(t *testing.T) {
		_, ts, project := filesServer(t)
		writeProjectFile(t, project, "packs/a.pack.json", `{"id":"a"}`)
		outside := t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, "a.pack.json"), []byte(`{"secret":true}`), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		testHookAfterResolve = swapDirForSymlinkOut(t, project, outside)
		t.Cleanup(func() { testHookAfterResolve = nil })

		status, body := getJSON(t, ts, "/api/file?token="+testToken+"&path=packs/a.pack.json")
		if status == http.StatusOK {
			t.Fatalf("read followed the swapped directory: %v", body)
		}
	})

	t.Run("PUT", func(t *testing.T) {
		_, ts, project := filesServer(t)
		writeProjectFile(t, project, "packs/a.pack.json", `{"id":"a"}`)
		outside := t.TempDir()
		victimOutside := filepath.Join(outside, "a.pack.json")
		if err := os.WriteFile(victimOutside, []byte(`{"secret":true}`), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		testHookAfterResolve = swapDirForSymlinkOut(t, project, outside)
		t.Cleanup(func() { testHookAfterResolve = nil })

		status, _ := putJSON(t, ts, WriteRequest{
			Path: "packs/a.pack.json", Content: `{"owned":true}`, Override: true,
		})
		if status == http.StatusOK {
			t.Fatal("write followed the swapped directory")
		}
		after, err := os.ReadFile(victimOutside)
		if err != nil || string(after) != `{"secret":true}` {
			t.Fatalf("the file outside the project was written: %q, %v", string(after), err)
		}
	})

	t.Run("list", func(t *testing.T) {
		_, ts, project := filesServer(t)
		writeProjectFile(t, project, "packs/a.pack.json", `{"id":"a"}`)
		outside := t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, "secret.json"), []byte(`{"secret":true}`), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		testHookAfterResolve = swapDirForSymlinkOut(t, project, outside)
		t.Cleanup(func() { testHookAfterResolve = nil })

		_, body := getJSON(t, ts, "/api/files?token="+testToken)
		for _, raw := range body["files"].([]any) {
			if p := raw.(map[string]any)["path"].(string); strings.Contains(p, "secret") {
				t.Fatalf("the listing walked outside the project: %s", p)
			}
		}
	})
}

// TestRetargetedProjectRootIsNotAdopted pins the root itself.
//
// The desk is started against a symlinked path — an ordinary way to reach a
// project — and the symlink is then repointed at another tree. A chassis that
// re-resolved its root per request would serve the new tree; this one holds a
// descriptor to the directory it was given.
func TestRetargetedProjectRootIsNotAdopted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	parent := t.TempDir()
	real := filepath.Join(parent, "real")
	other := filepath.Join(parent, "other")
	for _, dir := range []string{real, other} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(real, "mine.json"), []byte(`{"mine":true}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(other, "theirs.json"), []byte(`{"theirs":true}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	link := filepath.Join(parent, "project")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	s, err := New(Config{ProjectDir: link, JpackBin: "jpack", Token: testToken, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()
	ts := httptest.NewServer(s)
	defer ts.Close()

	// Repoint the root after the server was built.
	if err := os.Remove(link); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.Symlink(other, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, body := getJSON(t, ts, "/api/files?token="+testToken)
	listed := map[string]bool{}
	for _, raw := range body["files"].([]any) {
		listed[raw.(map[string]any)["path"].(string)] = true
	}
	if listed["theirs.json"] {
		t.Fatal("the chassis adopted the retargeted root")
	}
	if !listed["mine.json"] {
		t.Fatalf("the chassis lost the root it was given: %v", listed)
	}
}

/* Authorization ----------------------------------------------------------- */

func TestFileAPIRequiresToken(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", "{}")

	for _, path := range []string{"/api/files", "/api/file?path=jpack.json"} {
		if status, _ := getJSON(t, ts, path); status != http.StatusUnauthorized {
			t.Fatalf("%s without a token: status %d", path, status)
		}
		if status, _ := getJSON(t, ts, path+"?token=wrong"); status != http.StatusUnauthorized {
			t.Fatalf("%s with a wrong token: status %d", path, status)
		}
	}

	payload, _ := json.Marshal(WriteRequest{Path: "jpack.json", Content: "{}", Override: true})
	r, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/file", bytes.NewReader(payload))
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("write without a token: status %d", resp.StatusCode)
	}
	if data, _ := os.ReadFile(filepath.Join(project, "jpack.json")); string(data) != "{}" {
		t.Fatalf("an unauthorized write reached the disk")
	}
}

func TestFileAPIRejectsForeignOrigin(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", "{}")

	r, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/files?token="+testToken, nil)
	r.Header.Set("Origin", "http://evil.example")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin: status %d", resp.StatusCode)
	}
}

/* Listing and reading ----------------------------------------------------- */

func TestListSkipsWhatIsNotAProjectDocument(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", `{"configVersion":"2"}`)
	writeProjectFile(t, project, "packs/a.pack.json", `{"id":"a"}`)
	writeProjectFile(t, project, "node_modules/big/index.js", "// no")
	writeProjectFile(t, project, ".git/config", "[core]")

	status, body := getJSON(t, ts, "/api/files?token="+testToken)
	if status != http.StatusOK {
		t.Fatalf("list: status %d", status)
	}
	listed := map[string]bool{}
	for _, raw := range body["files"].([]any) {
		entry := raw.(map[string]any)
		listed[entry["path"].(string)] = true
		if entry["sha256"].(string) == "" {
			t.Fatalf("%s listed with no digest", entry["path"])
		}
	}
	for _, want := range []string{"jpack.json", "packs/a.pack.json"} {
		if !listed[want] {
			t.Fatalf("%s missing from the listing: %v", want, listed)
		}
	}
	for _, unwanted := range []string{"node_modules/big/index.js", ".git/config"} {
		if listed[unwanted] {
			t.Fatalf("%s should not be listed", unwanted)
		}
	}
}

func TestReadReturnsBytesAndTheirDigest(t *testing.T) {
	_, ts, project := filesServer(t)
	const content = `{"id":"a","nested":{"unicode":"café →"}}`
	writeProjectFile(t, project, "packs/a.pack.json", content)

	status, body := getJSON(t, ts, "/api/file?token="+testToken+"&path=packs/a.pack.json")
	if status != http.StatusOK {
		t.Fatalf("read: status %d, %v", status, body)
	}
	if body["content"] != content {
		t.Fatalf("content round-trip: %q", body["content"])
	}
	if got, want := body["sha256"].(string), digestOf([]byte(content)); got != want {
		t.Fatalf("digest %s, want %s", got, want)
	}
}

func TestReadRefusesBytesThatAreNotText(t *testing.T) {
	// JSON strings are UTF-8, and the encoder would substitute U+FFFD rather
	// than fail. Handing back a mangled document that the editor could then
	// save over the real one is the failure this refuses.
	_, ts, project := filesServer(t)
	abs := filepath.Join(project, "binary.bin")
	if err := os.WriteFile(abs, []byte{0xff, 0xfe, 0x00, 0x01}, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	status, body := getJSON(t, ts, "/api/file?token="+testToken+"&path=binary.bin")
	if status != http.StatusUnsupportedMediaType {
		t.Fatalf("read of non-text: status %d, %v", status, body)
	}
}

/* Writing ----------------------------------------------------------------- */

func TestWriteIsAtomicAndReadsBackWhatLanded(t *testing.T) {
	_, ts, project := filesServer(t)
	const before = `{"id":"a"}`
	writeProjectFile(t, project, "packs/a.pack.json", before)
	const after = `{"id":"a","version":"0.2.0"}`

	status, body := putJSON(t, ts, WriteRequest{
		Path:       "packs/a.pack.json",
		Content:    after,
		BaseSHA256: digestOf([]byte(before)),
	})
	if status != http.StatusOK {
		t.Fatalf("write: status %d, %v", status, body)
	}
	// The answer is a read-back off the disk, not an echo of the request.
	if body["content"] != after {
		t.Fatalf("read-back content: %q", body["content"])
	}
	if got, want := body["sha256"].(string), digestOf([]byte(after)); got != want {
		t.Fatalf("read-back digest %s, want %s", got, want)
	}
	onDisk, err := os.ReadFile(filepath.Join(project, "packs", "a.pack.json"))
	if err != nil || string(onDisk) != after {
		t.Fatalf("disk: %q, %v", string(onDisk), err)
	}

	// No staging file survives a successful write.
	assertNoTempFiles(t, filepath.Join(project, "packs"))
}

func TestWriteCreatesAFileAndSaysSo(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "packs/existing.pack.json", "{}")

	status, body := putJSON(t, ts, WriteRequest{Path: "packs/new.pack.json", Content: `{"id":"new"}`})
	if status != http.StatusOK {
		t.Fatalf("create: status %d, %v", status, body)
	}
	if body["created"] != true {
		t.Fatalf("a created file did not report created: %v", body)
	}
	if _, err := os.Stat(filepath.Join(project, "packs", "new.pack.json")); err != nil {
		t.Fatalf("the file was not created: %v", err)
	}
}

func TestWriteIntoAMissingDirectorySaysSoRatherThanRefusingContainment(t *testing.T) {
	// The path is inside the project. Reporting it as an escape would send a
	// contributor hunting a security problem that is not there.
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", "{}")

	status, body := putJSON(t, ts, WriteRequest{Path: "packs/new.pack.json", Content: "{}"})
	if status != http.StatusNotFound {
		t.Fatalf("write into a missing directory: status %d, %v", status, body)
	}
	message := body["error"].(string)
	if !strings.Contains(message, "packs") || !strings.Contains(message, "does not exist") {
		t.Fatalf("unhelpful message: %q", message)
	}
	if strings.Contains(message, "not inside the project") {
		t.Fatalf("a missing directory was reported as a containment failure: %q", message)
	}
}

func TestWritePreservesTheFileMode(t *testing.T) {
	// CreateTemp makes 0600. A save that silently narrowed a document's mode
	// would be an edit nobody asked for.
	if runtime.GOOS == "windows" {
		t.Skip("mode semantics differ on Windows")
	}
	_, ts, project := filesServer(t)
	abs := writeProjectFile(t, project, "packs/a.pack.json", "{}")
	if err := os.Chmod(abs, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if status, body := putJSON(t, ts, WriteRequest{
		Path: "packs/a.pack.json", Content: `{"id":"a"}`, BaseSHA256: digestOf([]byte("{}")),
	}); status != http.StatusOK {
		t.Fatalf("write: status %d, %v", status, body)
	}
	info, err := os.Stat(abs)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode after write: %v", info.Mode().Perm())
	}
}

// TestWriteReplacesAReadOnlyFile pins that the save replaces the *directory
// entry* rather than writing through the file — which is what makes it atomic,
// and is observable exactly here: renaming over a read-only file succeeds where
// opening it for truncation would be refused. The mode is carried across, so a
// read-only document stays read-only.
func TestWriteReplacesAReadOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores the write bit, so this cannot discriminate")
	}
	_, ts, project := filesServer(t)
	abs := writeProjectFile(t, project, "packs/a.pack.json", "{}")
	if err := os.Chmod(abs, 0o444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	status, body := putJSON(t, ts, WriteRequest{
		Path: "packs/a.pack.json", Content: `{"id":"a"}`, BaseSHA256: digestOf([]byte("{}")),
	})
	if status != http.StatusOK {
		t.Fatalf("write over a read-only file: status %d, %v", status, body)
	}
	onDisk, err := os.ReadFile(abs)
	if err != nil || string(onDisk) != `{"id":"a"}` {
		t.Fatalf("disk: %q, %v", string(onDisk), err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o444 {
		t.Fatalf("mode after replacing a read-only file: %v", info.Mode().Perm())
	}
}

func TestStaleWriteIsRefusedWithBothDigests(t *testing.T) {
	_, ts, project := filesServer(t)
	const loaded = `{"id":"a"}`
	const changedUnderneath = `{"id":"a","edited":"by someone else"}`
	abs := writeProjectFile(t, project, "packs/a.pack.json", loaded)

	// Someone else writes between the editor's load and its save.
	if err := os.WriteFile(abs, []byte(changedUnderneath), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	status, body := putJSON(t, ts, WriteRequest{
		Path:       "packs/a.pack.json",
		Content:    `{"id":"a","edited":"by me"}`,
		BaseSHA256: digestOf([]byte(loaded)),
	})
	if status != http.StatusConflict {
		t.Fatalf("stale write: status %d, %v", status, body)
	}
	if got, want := body["expectedSha256"], digestOf([]byte(loaded)); got != want {
		t.Fatalf("expected digest %v, want %v", got, want)
	}
	if got, want := body["actualSha256"], digestOf([]byte(changedUnderneath)); got != want {
		t.Fatalf("actual digest %v, want %v", got, want)
	}
	if body["exists"] != true {
		t.Fatalf("a changed file reported as absent: %v", body)
	}
	// The refusal wrote nothing.
	onDisk, _ := os.ReadFile(abs)
	if string(onDisk) != changedUnderneath {
		t.Fatalf("a refused write reached the disk: %q", string(onDisk))
	}
	assertNoTempFiles(t, filepath.Join(project, "packs"))
}

func TestStaleWriteProceedsWithOverride(t *testing.T) {
	_, ts, project := filesServer(t)
	abs := writeProjectFile(t, project, "packs/a.pack.json", `{"id":"a"}`)
	if err := os.WriteFile(abs, []byte(`{"id":"changed"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	const mine = `{"id":"mine"}`
	status, body := putJSON(t, ts, WriteRequest{
		Path:       "packs/a.pack.json",
		Content:    mine,
		BaseSHA256: digestOf([]byte(`{"id":"a"}`)),
		Override:   true,
	})
	if status != http.StatusOK {
		t.Fatalf("override: status %d, %v", status, body)
	}
	if body["content"] != mine {
		t.Fatalf("override read-back: %q", body["content"])
	}
}

func TestWritingANewFileThatIsNotNewIsRefused(t *testing.T) {
	// The editor believed it was creating a file. It is not, and overwriting
	// something the user never opened is exactly the surprise the digest is
	// there to prevent.
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "packs/a.pack.json", `{"id":"already here"}`)

	status, body := putJSON(t, ts, WriteRequest{Path: "packs/a.pack.json", Content: "{}"})
	if status != http.StatusConflict {
		t.Fatalf("write as new: status %d, %v", status, body)
	}
	if body["expectedSha256"] != "" {
		t.Fatalf("expected digest should be empty for a believed-new file: %v", body)
	}
	if body["exists"] != true {
		t.Fatalf("exists should be true: %v", body)
	}
}

func TestWritingOverADeletedFileIsRefused(t *testing.T) {
	_, ts, project := filesServer(t)
	const loaded = `{"id":"a"}`
	abs := writeProjectFile(t, project, "packs/a.pack.json", loaded)
	if err := os.Remove(abs); err != nil {
		t.Fatalf("remove: %v", err)
	}
	status, body := putJSON(t, ts, WriteRequest{
		Path: "packs/a.pack.json", Content: "{}", BaseSHA256: digestOf([]byte(loaded)),
	})
	if status != http.StatusConflict {
		t.Fatalf("write over deleted: status %d, %v", status, body)
	}
	if body["exists"] != false {
		t.Fatalf("a deleted file reported as present: %v", body)
	}
	if body["actualSha256"] != "" {
		t.Fatalf("a deleted file reported a digest: %v", body)
	}
}

// TestCompetingWritesFromOneBaseProduceOneWinner is the conditional commit.
//
// Two writers load the same bytes and both save without override. The check and
// the rename are one decision, so exactly one may succeed: if both pass the
// digest check and both rename, the second silently discards the first, and
// both clients are told they saved.
//
// The barrier is what makes it deterministic rather than a race that usually
// goes the right way. Neither request may proceed until both are inside the
// handler, which is the interleaving a lock has to survive.
func TestCompetingWritesFromOneBaseProduceOneWinner(t *testing.T) {
	_, ts, project := filesServer(t)
	const base = `{"id":"a"}`
	abs := writeProjectFile(t, project, "packs/a.pack.json", base)
	baseDigest := digestOf([]byte(base))

	var arrived sync.WaitGroup
	arrived.Add(2)
	release := make(chan struct{})
	var once sync.Once
	testHookAfterResolve = func(rel string) {
		if rel != "packs/a.pack.json" {
			return
		}
		arrived.Done()
		<-release
	}
	t.Cleanup(func() { testHookAfterResolve = nil })
	go func() {
		arrived.Wait()
		once.Do(func() { close(release) })
	}()

	type outcome struct {
		status int
	}
	results := make(chan outcome, 2)
	for _, content := range []string{`{"id":"first"}`, `{"id":"second"}`} {
		go func(content string) {
			status, _ := putJSONNoFatal(ts, WriteRequest{
				Path: "packs/a.pack.json", Content: content, BaseSHA256: baseDigest,
			})
			results <- outcome{status}
		}(content)
	}

	statuses := []int{}
	for range 2 {
		statuses = append(statuses, (<-results).status)
	}
	ok, conflicted := 0, 0
	for _, status := range statuses {
		switch status {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflicted++
		default:
			t.Fatalf("unexpected status %d (all: %v)", status, statuses)
		}
	}
	if ok != 1 || conflicted != 1 {
		t.Fatalf("expected one 200 and one 409, got %v — the digest check is not held across the commit", statuses)
	}
	final, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(final) != `{"id":"first"}` && string(final) != `{"id":"second"}` {
		t.Fatalf("the file is not any whole write: %q", string(final))
	}
	assertNoTempFiles(t, filepath.Join(project, "packs"))
}

func TestConcurrentWritesLeaveOneWholeDocument(t *testing.T) {
	// Last write wins, and the race is not what is under test: what is under
	// test is that no interleaving leaves a half-written file. Every writer
	// overrides, so every one of them is allowed through, and the file must
	// still be exactly one of the contents at the end.
	_, ts, project := filesServer(t)
	abs := writeProjectFile(t, project, "packs/a.pack.json", "{}")

	const writers = 8
	contents := make([]string, writers)
	done := make(chan struct{}, writers)
	for i := range writers {
		contents[i] = `{"writer":` + strings.Repeat("9", i+1) + `}`
		go func(body string) {
			defer func() { done <- struct{}{} }()
			putJSONNoFatal(ts, WriteRequest{Path: "packs/a.pack.json", Content: body, Override: true})
		}(contents[i])
	}
	for range writers {
		<-done
	}

	final, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, candidate := range contents {
		if string(final) == candidate {
			assertNoTempFiles(t, filepath.Join(project, "packs"))
			return
		}
	}
	t.Fatalf("the file is not any whole write: %q", string(final))
}

// assertNoTempFiles fails when a staging file was left behind. The temporary
// file lives in the target's own directory — rename is only atomic within a
// filesystem — so a leak would sit next to the document it was staging.
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".jpack-desk-") {
			t.Fatalf("a staging file was left behind: %s", e.Name())
		}
	}
}

func TestAtomicWriteCleansUpWhenTheRenameCannotHappen(t *testing.T) {
	// The failure surface: staging succeeded, the replace did not. Renaming
	// onto a directory fails on every platform, which makes it a portable way
	// to reach that branch without a fault-injection layer.
	s, _, project := filesServer(t)
	if err := os.Mkdir(filepath.Join(project, "adirectory"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := s.atomicWrite("adirectory", []byte("{}")); err == nil {
		t.Fatal("expected the write to fail")
	}
	assertNoTempFiles(t, project)
}

// TestStagingFilesAreNeitherListedNorWatched pins the exclusion.
//
// A staging file is this desk mid-save. Listing it exposes a half-written
// document to a second tab, and watching it fires a change notification for a
// file nobody edited — in the middle of the save that made it.
func TestStagingFilesAreNeitherListedNorWatched(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", "{}")
	writeProjectFile(t, project, ".jpack-desk-deadbeef.tmp", "half a document")
	writeProjectFile(t, project, "packs/.jpack-desk-cafe.tmp", "half a document")

	_, body := getJSON(t, ts, "/api/files?token="+testToken)
	for _, raw := range body["files"].([]any) {
		if p := raw.(map[string]any)["path"].(string); strings.Contains(p, ".jpack-desk-") {
			t.Fatalf("a staging file was listed: %s", p)
		}
	}
	// And neither can be read or written through the API by name.
	if status, _ := getJSON(t, ts, "/api/file?token="+testToken+"&path=.jpack-desk-deadbeef.tmp"); status != http.StatusForbidden {
		t.Fatalf("reading a staging file: status %d", status)
	}
	if status, _ := putJSON(t, ts, WriteRequest{Path: ".jpack-desk-new.tmp", Content: "{}", Override: true}); status != http.StatusForbidden {
		t.Fatalf("writing a staging file: status %d", status)
	}
}

// TestStaleStagingFilesAreClearedAtStartup pins the cleanup. A crash between
// staging and rename leaves one behind; a project should not slowly fill with
// the debris of interrupted saves.
func TestStaleStagingFilesAreClearedAtStartup(t *testing.T) {
	project := t.TempDir()
	writeProjectFile(t, project, "jpack.json", "{}")
	writeProjectFile(t, project, ".jpack-desk-stale.tmp", "left by a crash")
	writeProjectFile(t, project, "packs/.jpack-desk-stale.tmp", "left by a crash")
	// Not ours, and not touched.
	writeProjectFile(t, project, "keep.tmp", "somebody else's")

	s, err := New(Config{ProjectDir: project, JpackBin: "jpack", Token: testToken, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	for _, gone := range []string{".jpack-desk-stale.tmp", "packs/.jpack-desk-stale.tmp"} {
		if _, err := os.Stat(filepath.Join(project, filepath.FromSlash(gone))); !os.IsNotExist(err) {
			t.Fatalf("%s survived startup", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(project, "keep.tmp")); err != nil {
		t.Fatalf("a file this desk does not own was removed: %v", err)
	}
}

// TestReadRefusesWhatIsNotARegularFile pins the handle-based type check. A FIFO
// opened for reading blocks until somebody writes to it, which would hold a
// handler forever.
func TestReadRefusesWhatIsNotARegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no mkfifo on Windows")
	}
	_, ts, project := filesServer(t)
	fifo := filepath.Join(project, "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		status, _ := getJSON(t, ts, "/api/file?token="+testToken+"&path=pipe")
		if status == http.StatusOK {
			t.Errorf("a FIFO was read as a document")
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("reading a FIFO blocked the handler")
	}
	// And it is not listed.
	_, body := getJSON(t, ts, "/api/files?token="+testToken)
	for _, raw := range body["files"].([]any) {
		if raw.(map[string]any)["path"] == "pipe" {
			t.Fatal("a FIFO was listed as a document")
		}
	}
}

// TestReadIsBoundedByTheReaderNotTheStat pins the LimitReader. A size read from
// metadata is a claim about a moment; the bound has to be on the read itself.
func TestReadIsBoundedByTheReaderNotTheStat(t *testing.T) {
	_, ts, project := filesServer(t)
	big := make([]byte, maxFileBytes+1024)
	for i := range big {
		big[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(project, "big.json"), big, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	status, body := getJSON(t, ts, "/api/file?token="+testToken+"&path=big.json")
	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized read: status %d, %v", status, body)
	}
	// It is still listed — it is really there — with no digest, because it was
	// not read.
	_, listing := getJSON(t, ts, "/api/files?token="+testToken)
	found := false
	for _, raw := range listing["files"].([]any) {
		entry := raw.(map[string]any)
		if entry["path"] == "big.json" {
			found = true
			if entry["sha256"] != "" {
				t.Fatalf("an unread file was listed with a digest: %v", entry["sha256"])
			}
		}
	}
	if !found {
		t.Fatal("an oversized file was omitted from the listing rather than listed without a digest")
	}
}

/* Origin and the browser's own defences ----------------------------------- */

// TestOriginMatchIsStrict pins scheme-and-host matching rather than host-only.
//
// A browser will not mint most of these, but the prose promises same-origin
// semantics and a check that reads only the host does not deliver them.
func TestOriginMatchIsStrict(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", "{}")
	host := strings.TrimPrefix(ts.URL, "http://")

	refused := map[string]string{
		"another site":        "http://evil.example",
		"a different scheme":  "https://" + host,
		"a path-bearing form": "http://" + host + "/evil",
		"a query-bearing one": "http://" + host + "?x=1",
		"userinfo":            "http://user@" + host,
		"an opaque form":      "http:evil",
		"scheme only":         "http://",
		"not a URL at all":    "null",
	}
	for name, origin := range refused {
		t.Run("refused/"+name, func(t *testing.T) {
			r, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/files?token="+testToken, nil)
			r.Header.Set("Origin", origin)
			resp, err := http.DefaultClient.Do(r)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("Origin %q: status %d", origin, resp.StatusCode)
			}
		})
	}

	t.Run("allowed/the origin we were served under", func(t *testing.T) {
		r, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/files?token="+testToken, nil)
		r.Header.Set("Origin", ts.URL)
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("same origin: status %d", resp.StatusCode)
		}
	})
}

// TestCrossOriginWriteIsRefusedAtBothLayers documents a defence that was
// accidental until it was written down.
//
// A page on another site cannot send this PUT from a browser: the JSON content
// type makes it a non-simple request, so the browser must preflight, and the
// chassis answers no CORS permission at all — no `Access-Control-Allow-Origin`,
// no handler for OPTIONS. That is the first layer, and it belongs to the
// browser rather than to us.
//
// The second layer is ours and does not depend on the first: a PUT that arrives
// with a foreign Origin anyway — a non-browser client, or a browser bug — is
// refused by the same guard every other endpoint uses. Both are asserted here
// so that neither can be removed on the assumption that the other is enough.
func TestCrossOriginWriteIsRefusedAtBothLayers(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", `{"id":"a"}`)

	// The browser's layer: nothing grants permission to preflight.
	preflight, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/file?token="+testToken, nil)
	preflight.Header.Set("Origin", "http://evil.example")
	preflight.Header.Set("Access-Control-Request-Method", "PUT")
	preflight.Header.Set("Access-Control-Request-Headers", "content-type")
	resp, err := http.DefaultClient.Do(preflight)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	defer resp.Body.Close()
	if allow := resp.Header.Get("Access-Control-Allow-Origin"); allow != "" {
		t.Fatalf("the chassis granted a cross-origin permission: %q", allow)
	}
	if resp.StatusCode == http.StatusOK && resp.Header.Get("Access-Control-Allow-Methods") != "" {
		t.Fatal("the chassis answered a preflight with method permission")
	}

	// Our layer: the request itself, sent anyway.
	payload, _ := json.Marshal(WriteRequest{Path: "jpack.json", Content: "{}", Override: true})
	put, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/file?token="+testToken, bytes.NewReader(payload))
	put.Header.Set("Origin", "http://evil.example")
	put.Header.Set("Content-Type", "application/json")
	written, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	defer written.Body.Close()
	if written.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin PUT: status %d", written.StatusCode)
	}
	if data, _ := os.ReadFile(filepath.Join(project, "jpack.json")); string(data) != `{"id":"a"}` {
		t.Fatalf("a cross-origin write reached the disk: %q", string(data))
	}
}

// TestViteProxyShapeNeedsDevMode models the header shape the dev server
// actually sends.
//
// The Vite proxy forwards the browser's Origin and — with changeOrigin off —
// the browser-facing Host too, so both name the dev server. A host-only check
// would see them match and accept, which would make the documented `--dev-token`
// requirement a fiction. With the scheme-and-host check they differ from the
// chassis' own origin, so the request is refused unless dev mode is on.
func TestViteProxyShapeNeedsDevMode(t *testing.T) {
	const devOrigin = "http://localhost:5173"

	for _, dev := range []bool{false, true} {
		t.Run(map[bool]string{false: "production refuses", true: "--dev-token accepts"}[dev], func(t *testing.T) {
			project := t.TempDir()
			if err := os.WriteFile(filepath.Join(project, "jpack.json"), []byte("{}"), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			s, err := New(Config{
				ProjectDir: project, JpackBin: "jpack", Token: testToken,
				DevMode: dev, Logger: log.New(io.Discard, "", 0),
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer s.Close()
			ts := httptest.NewServer(s)
			defer ts.Close()

			r, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/files?token="+testToken, nil)
			// The shape `changeOrigin: true` produces: Host rewritten to the
			// chassis' own, the browser's Origin forwarded unchanged. The two
			// differ, so the check actually decides.
			r.Header.Set("Origin", devOrigin)
			resp, err := http.DefaultClient.Do(r)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer resp.Body.Close()
			want := http.StatusForbidden
			if dev {
				want = http.StatusOK
			}
			if resp.StatusCode != want {
				t.Fatalf("dev=%v: status %d, want %d", dev, resp.StatusCode, want)
			}
		})
	}
}

// TestHandlersRefuseTraversalOverTheWire is the call-site test.
//
// TestWireRelativePathRefusesOnItsOwn asks the validator directly, which is
// what notices its guards being deleted. This asks the *handlers*, which is
// what notices the validator no longer being called at all — a mutation the
// direct test cannot see, because the function it tests is still perfectly
// correct and simply unreferenced.
func TestHandlersRefuseTraversalOverTheWire(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", `{"id":"mine"}`)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.json")
	if err := os.WriteFile(secret, []byte(`{"secret":true}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	for _, rel := range []string{
		"../" + filepath.Base(outside) + "/secret.json",
		"..",
		"/etc/passwd",
		"packs/../../secret.json",
		".jpack-desk-x.tmp",
	} {
		t.Run("read/"+rel, func(t *testing.T) {
			status, body := getJSON(t, ts, "/api/file?token="+testToken+"&path="+urlEscape(rel))
			if status == http.StatusOK {
				t.Fatalf("read %q succeeded: %v", rel, body)
			}
		})
		t.Run("write/"+rel, func(t *testing.T) {
			status, _ := putJSON(t, ts, WriteRequest{Path: rel, Content: "{}", Override: true})
			if status == http.StatusOK {
				t.Fatalf("write %q succeeded", rel)
			}
		})
	}
	// Nothing outside changed, and the one legitimate file is untouched.
	if data, _ := os.ReadFile(secret); string(data) != `{"secret":true}` {
		t.Fatalf("a file outside the project changed: %q", string(data))
	}
	if data, _ := os.ReadFile(filepath.Join(project, "jpack.json")); string(data) != `{"id":"mine"}` {
		t.Fatalf("the project file changed: %q", string(data))
	}
}

func urlEscape(s string) string { return url.QueryEscape(s) }
