package desk

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

// TestResolveInProjectContainment is the table the whole API rests on. Each
// case names the family it belongs to, because the three are stopped by three
// different parts of the function and a test that only asked "was it refused?"
// could not tell which of them had stopped running.
func TestResolveInProjectContainment(t *testing.T) {
	s, _, project := filesServer(t)

	writeProjectFile(t, project, "jpack.json", `{"configVersion":"2"}`)
	writeProjectFile(t, project, "packs/a.pack.json", `{}`)

	// A place outside the project, and a file in it that must stay unreachable.
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.json")
	if err := os.WriteFile(secret, []byte(`{"secret":true}`), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	if runtime.GOOS != "windows" {
		// A symlink *inside* the project pointing at a file outside it. This is
		// the case a lexical check cannot see: the path never says `..`, and
		// `filepath.Clean` reports it as contained.
		if err := os.Symlink(secret, filepath.Join(project, "escape.json")); err != nil {
			t.Fatalf("symlink file: %v", err)
		}
		// And a symlinked *directory*, so the escape is in a path component
		// rather than in the leaf.
		if err := os.Symlink(outside, filepath.Join(project, "elsewhere")); err != nil {
			t.Fatalf("symlink dir: %v", err)
		}
	}

	allowed := []struct {
		name string
		rel  string
	}{
		{"a file at the root", "jpack.json"},
		{"a file in a subdirectory", "packs/a.pack.json"},
		{"a file that does not exist yet", "packs/new.pack.json"},
		{"a dot-dot that stays inside", "packs/../jpack.json"},
	}
	for _, tc := range allowed {
		t.Run("allowed/"+tc.name, func(t *testing.T) {
			abs, err := s.resolveInProject(tc.rel)
			if err != nil {
				t.Fatalf("resolveInProject(%q): %v", tc.rel, err)
			}
			if !strings.HasPrefix(abs, mustEval(t, project)) {
				t.Fatalf("resolved outside the project: %s", abs)
			}
		})
	}

	refused := []struct {
		name   string
		rel    string
		family string
	}{
		{"empty", "", "syntax"},
		{"the project itself", ".", "syntax"},
		{"bare dot-dot", "..", "lexical"},
		{"climbing out", "../secret.json", "lexical"},
		{"climbing out through a subdirectory", "packs/../../secret.json", "lexical"},
		{"deep climb", "a/b/c/../../../../secret.json", "lexical"},
		{"absolute", secret, "absolute"},
		{"absolute root", "/etc/passwd", "absolute"},
	}
	if runtime.GOOS != "windows" {
		refused = append(refused,
			struct {
				name   string
				rel    string
				family string
			}{"a symlink pointing out", "escape.json", "symlink"},
			struct {
				name   string
				rel    string
				family string
			}{"through a symlinked directory", "elsewhere/secret.json", "symlink"},
			struct {
				name   string
				rel    string
				family string
			}{"a new file under a symlinked directory", "elsewhere/new.json", "symlink"},
		)
	}
	for _, tc := range refused {
		t.Run("refused/"+tc.family+"/"+tc.name, func(t *testing.T) {
			if abs, err := s.resolveInProject(tc.rel); err == nil {
				t.Fatalf("resolveInProject(%q) allowed %s; expected refusal (%s)", tc.rel, abs, tc.family)
			}
		})
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", p, err)
	}
	return real
}

// TestSymlinkEscapeRefusedOverTheWire proves the containment is reached by the
// endpoints and not merely by the helper — on reads and on writes alike, which
// is the consistency this API promises.
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
		t.Fatalf("symlink: %v", err)
	}

	status, body := getJSON(t, ts, "/api/file?token="+testToken+"&path=escape.json")
	if status != http.StatusForbidden {
		t.Fatalf("read through a symlink: status %d, body %v", status, body)
	}
	if strings.Contains(body["error"].(string), "secret") {
		t.Fatalf("the refusal describes what is outside the project: %v", body["error"])
	}

	status, _ = putJSON(t, ts, WriteRequest{Path: "escape.json", Content: "{}", Override: true})
	if status != http.StatusForbidden {
		t.Fatalf("write through a symlink: status %d", status)
	}
	// And the file outside is untouched, which is the thing that actually
	// matters — a refusal that had already written would be no refusal.
	after, err := os.ReadFile(secret)
	if err != nil || string(after) != `{"secret":true}` {
		t.Fatalf("the file outside the project changed: %q, %v", string(after), err)
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
			putJSON(t, ts, WriteRequest{Path: "packs/a.pack.json", Content: body, Override: true})
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
	project := t.TempDir()
	target := filepath.Join(project, "adirectory")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := atomicWrite(target, []byte("{}")); err == nil {
		t.Fatal("expected the write to fail")
	}
	assertNoTempFiles(t, project)
}
