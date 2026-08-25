package desk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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
		// The refusal may echo the path the caller asked for — that is theirs
		// already. What it must not do is name where the link pointed, which
		// would describe the filesystem outside the project to whoever asked.
		if message, ok := body["error"].(string); ok && strings.Contains(message, outside) {
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

	// Reading, not only listing: the read path opens through the root too, and
	// a read that rejoined the configured pathname would find the other tree.
	status, read := getJSON(t, ts, "/api/file?token="+testToken+"&path=mine.json")
	if status != http.StatusOK || read["content"] != `{"mine":true}` {
		t.Fatalf("reading through the pinned root: status %d, %v", status, read)
	}
	if status, _ := getJSON(t, ts, "/api/file?token="+testToken+"&path=theirs.json"); status == http.StatusOK {
		t.Fatal("a file from the retargeted tree was readable")
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
	project := t.TempDir()
	fifo := filepath.Join(project, "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	srv, err := New(Config{ProjectDir: project, JpackBin: "jpack", Token: testToken, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	// This server is deliberately not closed through t.Cleanup. A handler that
	// blocks on the FIFO — exactly what this test exists to catch — would keep
	// httptest.Server.Close waiting forever, and the failure would arrive as a
	// whole-suite timeout naming no test at all. A named failure is worth a
	// leaked listener in a build that is already broken.
	ts := httptest.NewServer(srv)

	// A client deadline, so the request gives up rather than the test.
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(ts.URL + "/api/file?token=" + testToken + "&path=pipe")
	if err != nil {
		ts.CloseClientConnections()
		t.Fatalf("reading a FIFO did not answer within the deadline — the open blocked: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("a FIFO was read as a document: status %d", resp.StatusCode)
	}

	// And it is not listed as a document either.
	_, body := getJSON(t, ts, "/api/files?token="+testToken)
	for _, raw := range body["files"].([]any) {
		if raw.(map[string]any)["path"] == "pipe" {
			t.Fatal("a FIFO was listed as a document")
		}
	}
	ts.Close()
}

// TestWriteRefusesWhatItCouldNotRead pins that a refused read is not read as an
// absent file.
//
// "There is no such file" is the only refusal that means this write creates
// one. Treating any other — a directory, a file too large — as absence lets an
// override write proceed against a target it was never allowed to read, and the
// write must carry the *read's own* refusal rather than a 404 claiming it would
// have created something or a 500 from a rename that was allowed to try.
func TestWriteRefusesWhatItCouldNotRead(t *testing.T) {
	_, ts, project := filesServer(t)
	if err := os.Mkdir(filepath.Join(project, "adirectory"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	big := make([]byte, maxFileBytes+16)
	if err := os.WriteFile(filepath.Join(project, "big.json"), big, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	for _, tc := range []struct {
		rel  string
		want int
	}{
		{"adirectory", http.StatusBadRequest},
		{"big.json", http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.rel, func(t *testing.T) {
			status, body := putJSON(t, ts, WriteRequest{Path: tc.rel, Content: "{}", Override: true})
			if status != tc.want {
				t.Fatalf("write to %s: status %d, want %d (%v)", tc.rel, status, tc.want, body)
			}
		})
	}
	if info, err := os.Stat(filepath.Join(project, "adirectory")); err != nil || !info.IsDir() {
		t.Fatalf("the directory was replaced: %v %v", info, err)
	}
	if info, err := os.Stat(filepath.Join(project, "big.json")); err != nil || info.Size() != int64(len(big)) {
		t.Fatalf("the oversized file was overwritten: %v %v", info, err)
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

/* Round-2 findings --------------------------------------------------------- */

// TestExcludedDirectoriesAreEndpointExclusions pins that the documented
// exclusions are refusals and not merely omissions from the listing. A GET or
// PUT that walked into one would also write debris the startup cleanup never
// collects, because cleanup skips those trees.
func TestExcludedDirectoriesAreEndpointExclusions(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, ".git/config", "[core]")
	writeProjectFile(t, project, "node_modules/pkg/index.js", "// no")

	for _, rel := range []string{
		".git/config", "node_modules/pkg/index.js", "dist/app.js",
		".venv/pyvenv.cfg", "vendor/x/y.go", "packs/.git/config",
	} {
		t.Run(rel, func(t *testing.T) {
			if status, body := getJSON(t, ts, "/api/file?token="+testToken+"&path="+urlEscape(rel)); status != http.StatusForbidden {
				t.Fatalf("read: status %d, %v", status, body)
			}
			if status, _ := putJSON(t, ts, WriteRequest{Path: rel, Content: "{}", Override: true}); status != http.StatusForbidden {
				t.Fatalf("write: status %d", status)
			}
		})
	}
	if data, _ := os.ReadFile(filepath.Join(project, ".git", "config")); string(data) != "[core]" {
		t.Fatalf(".git/config was written: %q", string(data))
	}
}

// TestSymlinkedPathsAreRefusedByBothVerbs pins the consistency the README
// claims.
//
// An in-root symlink to an in-root file is the case that used to differ: GET
// followed it and returned the target's bytes, while a save renamed over the
// *link*. One name, two objects, and the editor showing one while the save
// replaced the other.
func TestSymlinkedPathsAreRefusedByBothVerbs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "packs/real.pack.json", `{"id":"real"}`)
	if err := os.Symlink("real.pack.json", filepath.Join(project, "packs", "alias.pack.json")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := os.Symlink("packs", filepath.Join(project, "linkdir")); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}

	for _, rel := range []string{"packs/alias.pack.json", "linkdir/real.pack.json"} {
		t.Run(rel, func(t *testing.T) {
			status, body := getJSON(t, ts, "/api/file?token="+testToken+"&path="+urlEscape(rel))
			if status != http.StatusForbidden {
				t.Fatalf("read: status %d, %v", status, body)
			}
			if status, _ := putJSON(t, ts, WriteRequest{Path: rel, Content: "{}", Override: true}); status != http.StatusForbidden {
				t.Fatalf("write: status %d", status)
			}
		})
	}
	// The real file is untouched and still reachable by its own name.
	if status, body := getJSON(t, ts, "/api/file?token="+testToken+"&path=packs/real.pack.json"); status != http.StatusOK {
		t.Fatalf("the real file became unreadable: %d %v", status, body)
	}
	// And neither link is listed.
	_, listing := getJSON(t, ts, "/api/files?token="+testToken)
	for _, raw := range listing["files"].([]any) {
		if p := raw.(map[string]any)["path"].(string); strings.Contains(p, "alias") || strings.HasPrefix(p, "linkdir/") {
			t.Fatalf("a symlink was listed: %s", p)
		}
	}
}

// TestListingSaysWhenItIsPartial pins that a thinned listing says so.
//
// A directory the walk cannot read is a real condition, and answering 200 with
// the files it did manage is indistinguishable from answering 200 for a smaller
// project. The `partial` member is what makes the two different answers.
func TestListingSaysWhenItIsPartial(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs Unix permissions and a non-root user")
	}
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", "{}")
	writeProjectFile(t, project, "locked/secret.json", "{}")
	locked := filepath.Join(project, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	status, body := getJSON(t, ts, "/api/files?token="+testToken)
	if status != http.StatusOK {
		t.Fatalf("list: status %d", status)
	}
	partial, ok := body["partial"].([]any)
	if !ok || len(partial) == 0 {
		t.Fatalf("an unreadable subtree was not reported: %v", body)
	}
	if !strings.Contains(body["note"].(string), "PARTIAL") {
		t.Fatalf("the note does not say the listing is partial: %v", body["note"])
	}
	// What it could read is still reported.
	listed := map[string]bool{}
	for _, raw := range body["files"].([]any) {
		listed[raw.(map[string]any)["path"].(string)] = true
	}
	if !listed["jpack.json"] {
		t.Fatalf("a readable file was dropped: %v", listed)
	}
}

// TestWriteHoldsTheLockAcrossTheCommit proves the mutex is held, rather than
// hoping the scheduler produces the right answer.
//
// The first request is stopped *inside* the critical section. If the second can
// reach its own current-bytes read while that is true, the compare and the
// commit are not one decision and the barrier test only ever passed by luck.
func TestWriteHoldsTheLockAcrossTheCommit(t *testing.T) {
	_, ts, project := filesServer(t)
	const base = `{"id":"a"}`
	writeProjectFile(t, project, "packs/a.pack.json", base)
	const other = `{"id":"b"}`
	writeProjectFile(t, project, "packs/b.pack.json", other)
	baseDigest := digestOf([]byte(base))
	otherDigest := digestOf([]byte(other))

	var entries atomic.Int32
	inside := make(chan struct{})
	hold := make(chan struct{})
	later := make(chan string, 4)
	var release sync.Once
	unblock := func() { release.Do(func() { close(hold) }) }
	// Whatever happens — including a t.Fatal below — the held request must be
	// let go, or the server's own Close waits on it forever and the failure
	// becomes a hang.
	t.Cleanup(func() {
		testHookAfterLockEntry = nil
		unblock()
	})

	testHookAfterLockEntry = func(rel string) {
		_ = rel
		if entries.Add(1) == 1 {
			close(inside)
			<-hold
			return
		}
		later <- rel
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		putJSONNoFatal(ts, WriteRequest{Path: "packs/a.pack.json", Content: `{"id":"first"}`, BaseSHA256: baseDigest})
	}()
	<-inside // the first request holds the lock and is not moving

	second := make(chan int, 1)
	// A *different* file, deliberately. With a per-path lock the second request
	// would take a different lock and sail straight in, and this test would pass
	// against the implementation it exists to rule out.
	go func() {
		status, _ := putJSONNoFatal(ts, WriteRequest{Path: "packs/b.pack.json", Content: `{"id":"second"}`, BaseSHA256: otherDigest})
		second <- status
	}()

	// The second request must not get inside while the first is held there. A
	// barrier placed only *before* the lock proves nothing: the scheduler is
	// free to produce the expected 200/409 by luck.
	select {
	case rel := <-later:
		t.Fatalf("a second write entered the critical section for %s while the first held it", rel)
	case status := <-second:
		t.Fatalf("a second write completed with %d while the first held the lock", status)
	case <-time.After(300 * time.Millisecond):
	}

	unblock()
	<-done
	// It targets a different file with a correct base, so once the lock is free
	// it simply succeeds. What was under test is that it could not proceed
	// while the first held the mutex.
	if status := <-second; status != http.StatusOK {
		t.Fatalf("the second write should have succeeded once the lock was free: status %d", status)
	}
	if got := entries.Load(); got != 2 {
		t.Fatalf("expected two entries into the critical section, got %d", got)
	}
}

// TestOriginRefusesEmptyDelimiters pins "nothing else" against the two forms
// that parse to empty rather than to a value.
func TestOriginRefusesEmptyDelimiters(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", "{}")

	for _, origin := range []string{ts.URL + "?", ts.URL + "#", ts.URL + "?#"} {
		t.Run(origin, func(t *testing.T) {
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
}

// TestRuntimeAndFileAPIShareOneProject pins the two authorities together.
//
// The runtime is started from a pathname — a subprocess cannot inherit the
// desk's directory descriptor portably — so the pathname it is given must be
// the one the root was pinned from. Resolving `ProjectDir` once at construction
// is what makes a retargeted symlink unable to split them.
func TestRuntimeAndFileAPIShareOneProject(t *testing.T) {
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
	link := filepath.Join(parent, "project")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	s, err := New(Config{ProjectDir: link, JpackBin: "jpack", Token: testToken, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	// Repoint after startup: neither half may follow.
	if err := os.Remove(link); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.Symlink(other, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	resolvedReal, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if s.runtimeWorkingDir() != resolvedReal {
		t.Fatalf("the runtime would start in %s, not %s", s.runtimeWorkingDir(), resolvedReal)
	}
	if s.root.Name() != resolvedReal {
		t.Fatalf("the file API root is %s, not %s", s.root.Name(), resolvedReal)
	}
}

// TestWatcherRefusesToReportSuccessWithNoWatches pins the startup failure.
//
// `filepath.WalkDir` does not follow a symlink handed to it as the walk root,
// so a symlinked project directory used to install zero watches and report
// success: live reload silently off, and nothing said. The resolved path is what
// the server passes now, and a genuinely unwatchable tree is an error.
func TestWatcherRefusesToReportSuccessWithNoWatches(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	parent := t.TempDir()
	real := filepath.Join(parent, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(parent, "project")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// The unresolved symlink is what used to install nothing.
	if w, err := newWatcher(link, log.New(io.Discard, "", 0), func(string) {}); err == nil {
		w.Close()
		t.Fatal("watching a symlinked root reported success with no watches")
	}
	// The resolved path — what the server actually passes — works.
	w, err := newWatcher(real, log.New(io.Discard, "", 0), func(string) {})
	if err != nil {
		t.Fatalf("watching the resolved root: %v", err)
	}
	w.Close()
}

/* Round-3 findings --------------------------------------------------------- */

// TestWalkStopsAtARepeatedAncestor pins the cycle guard.
//
// A tree can contain itself without a single symlink: a bind mount, or on
// filesystems that allow it a directory hard link. The depth cap alone does not
// save you — two aliases per level and the work doubles until the cap — so the
// walk compares each opened directory's identity against the directories open
// above it.
func TestWalkStopsAtARepeatedAncestor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bind mounts are not available here")
	}
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", "{}")
	writeProjectFile(t, project, "deep/a.json", "{}")

	inner := filepath.Join(project, "deep", "loop")
	if err := os.Mkdir(inner, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A bind mount of the project onto a directory inside it. Without
	// privileges this is not available, and the test says so rather than
	// pretending to have run.
	if out, err := exec.Command("mount", "--bind", project, inner).CombinedOutput(); err != nil {
		t.Skipf("cannot bind-mount (needs privileges): %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("umount", inner).Run() })

	done := make(chan map[string]any, 1)
	go func() {
		_, body := getJSON(t, ts, "/api/files?token="+testToken)
		done <- body
	}()
	select {
	case body := <-done:
		partial, _ := body["partial"].([]any)
		found := false
		for _, p := range partial {
			if strings.Contains(p.(string), "ancestors") {
				found = true
			}
		}
		if !found {
			t.Fatalf("the repeated ancestor was not reported: %v", body["partial"])
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the listing did not finish: the walk followed the cycle")
	}
}

// TestWalkIsBoundedInAWideDirectory pins the entry budget.
//
// A directory nobody excluded can hold more entries than anyone wants read into
// one response. The budget is what turns that into a partial answer rather than
// an unbounded one, and the answer says so.
func TestWalkIsBoundedInAWideDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("writes a great many files")
	}
	_, ts, project := filesServer(t)
	wide := filepath.Join(project, "wide")
	if err := os.Mkdir(wide, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Just past the budget, so the walk must stop and say it stopped.
	for i := range maxWalkEntries + 32 {
		if err := os.WriteFile(filepath.Join(wide, fmt.Sprintf("f%06d.json", i)), []byte("{}"), 0o644); err != nil {
			t.Skipf("could not create %d files: %v", i, err)
		}
	}

	status, body := getJSON(t, ts, "/api/files?token="+testToken)
	if status != http.StatusOK {
		t.Fatalf("list: status %d", status)
	}
	partial, _ := body["partial"].([]any)
	found := false
	for _, p := range partial {
		if strings.Contains(p.(string), "stopped after") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a listing past the budget did not say it stopped: %v", body["partial"])
	}
	if len(body["files"].([]any)) > maxWalkEntries+1 {
		t.Fatalf("the listing returned %d files, past the budget", len(body["files"].([]any)))
	}
}

// TestExcludedNamesMatchWholeComponentsCaseInsensitively pins both halves of the
// rule at once.
//
// `node_modules.json` is an ordinary document and must keep working; `.GIT` and
// `NODE_MODULES` are the same directories as their lowercase spellings on a
// case-insensitive volume, and an exclusion a spelling walks around is not one.
func TestExcludedNamesMatchWholeComponentsCaseInsensitively(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "node_modules.json", `{"id":"a document"}`)
	writeProjectFile(t, project, "packs/vendor.pack.json", `{"id":"also a document"}`)

	for _, rel := range []string{"node_modules.json", "packs/vendor.pack.json"} {
		t.Run("allowed/"+rel, func(t *testing.T) {
			if status, body := getJSON(t, ts, "/api/file?token="+testToken+"&path="+urlEscape(rel)); status != http.StatusOK {
				t.Fatalf("read: status %d, %v", status, body)
			}
		})
	}
	for _, rel := range []string{
		".GIT/config", "NODE_MODULES/x.js", "Node_Modules/x.js",
		"Dist/app.js", "VENDOR/x.go", "packs/.Git/config",
	} {
		t.Run("refused/"+rel, func(t *testing.T) {
			if status, body := getJSON(t, ts, "/api/file?token="+testToken+"&path="+urlEscape(rel)); status != http.StatusForbidden {
				t.Fatalf("read: status %d, %v", status, body)
			}
			if status, _ := putJSON(t, ts, WriteRequest{Path: rel, Content: "{}", Override: true}); status != http.StatusForbidden {
				t.Fatalf("write: status %d", status)
			}
		})
	}

	// A regular file bearing an excluded directory's name is omitted from the
	// listing too, because GET and PUT refuse that path.
	if err := os.WriteFile(filepath.Join(project, "vendor"), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, listing := getJSON(t, ts, "/api/files?token="+testToken)
	listed := map[string]bool{}
	for _, raw := range listing["files"].([]any) {
		listed[raw.(map[string]any)["path"].(string)] = true
	}
	if listed["vendor"] {
		t.Fatal("a regular file named vendor was listed, but the endpoints refuse it")
	}
	if !listed["node_modules.json"] {
		t.Fatalf("an ordinary document was excluded: %v", listed)
	}
}

// TestUnreadableFileGoesToPartialRatherThanTheOversizedShape pins the
// distinction.
//
// An empty digest is a documented shape meaning "too large to hash". Using it
// for a permission error says "too large" about a file that is not.
func TestUnreadableFileGoesToPartialRatherThanTheOversizedShape(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs Unix permissions and a non-root user")
	}
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", "{}")
	locked := writeProjectFile(t, project, "unreadable.json", "{}")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o644) })

	_, body := getJSON(t, ts, "/api/files?token="+testToken)
	partial, _ := body["partial"].([]any)
	found := false
	for _, p := range partial {
		if strings.Contains(p.(string), "unreadable.json") {
			found = true
		}
	}
	if !found {
		t.Fatalf("an unreadable file was not reported as partial: %v", body["partial"])
	}
}

// TestWriteReleasesTheLockBeforeEncoding pins that a stalled reader cannot hold
// the write mutex.
//
// The response is encoded to a ResponseWriter, which is client-speed: a caller
// that stops reading its socket holds that write for as long as it likes.
// Inside the lock, one such client stops every later save on the machine.
func TestWriteReleasesTheLockBeforeEncoding(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "a.json", "{}")
	writeProjectFile(t, project, "b.json", "{}")

	// A body big enough that the response cannot sit in one socket buffer, and
	// a client that reads none of it.
	big := strings.Repeat("x", 2<<20)
	stalled := make(chan struct{})
	go func() {
		defer close(stalled)
		payload, _ := json.Marshal(WriteRequest{Path: "a.json", Content: big, Override: true})
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/file?token="+testToken, bytes.NewReader(payload))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		// Deliberately not reading the body: hold the response open, then go.
		time.Sleep(2 * time.Second)
		resp.Body.Close()
	}()

	// While that one is stalled mid-response, another save must still complete.
	deadline := time.After(10 * time.Second)
	done := make(chan int, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		status, _ := putJSONNoFatal(ts, WriteRequest{Path: "b.json", Content: `{"id":"b"}`, Override: true})
		done <- status
	}()
	select {
	case status := <-done:
		if status != http.StatusOK {
			t.Fatalf("the second save answered %d", status)
		}
	case <-deadline:
		t.Fatal("a second save could not complete while a client stalled reading its response")
	}
	<-stalled
}
