package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func storageRequest(t *testing.T, ts *httptest.Server, path string, body any) (int, storageStatus, string) {
	t.Helper()
	method := "GET"
	var input []byte
	if body != nil {
		method = "POST"
		input, _ = json.Marshal(body)
	}
	req, _ := http.NewRequest(method, ts.URL+path, bytes.NewReader(input))
	bearer(req)
	response, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	var status storageStatus
	if response.StatusCode == 200 {
		if err := json.Unmarshal(data, &status); err != nil {
			t.Fatal(err)
		}
	}
	return response.StatusCode, status, string(data)
}
func storageGet(t *testing.T, ts *httptest.Server) storageStatus {
	t.Helper()
	code, value, body := storageRequest(t, ts, "/api/storage", nil)
	if code != 200 {
		t.Fatalf("storage: %d %s", code, body)
	}
	return value
}
func privateFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func mustBytes(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s: %q %v; want %q", path, got, err, want)
	}
}

const storageChat = `{"version":1,"chats":[{"id":"one","composer":"private draft","attachments":[{"text":"retained source"}]}]}`

func TestStorageDefaultAndLegacy(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "legacy"}[legacy], func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			if legacy {
				privateFixture(t, filepath.Join(s.configDir, s.conversationName()), storageChat)
			}
			status := storageGet(t, ts)
			want := s.defaultChatDataPath()
			if legacy {
				want = s.configDir
			}
			if status.Path != want || status.Legacy != legacy {
				t.Fatalf("%+v", status)
			}
			code, reply := chatRequest(t, ts, "GET", "", "", true, "")
			if code != 200 {
				t.Fatal(code)
			}
			if legacy {
				if string(reply.Content) != storageChat {
					t.Fatal("legacy history lost")
				}
			} else {
				if reply.SHA256 != "absent" {
					t.Fatal("empty visit created history")
				}
				if _, err := os.Stat(filepath.Join(want, s.conversationName())); !os.IsNotExist(err) {
					t.Fatal("empty history file created", err)
				}
			}
		})
	}
}

func TestStorageMoveAllProjectsAndFreshWriters(t *testing.T) {
	config := t.TempDir()
	a, ats, _ := assistantServerIn(t, config)
	b, bts, _ := assistantServerIn(t, config)
	for _, ts := range []*httptest.Server{ats, bts} {
		if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
			t.Fatal(code)
		}
	}
	privateFixture(t, filepath.Join(config, "secrets", "unrelated"), "protected credential")
	before := storageGet(t, ats)
	if before.ProjectCount != 2 || before.ProjectBytes != int64(len(storageChat)) {
		t.Fatalf("%+v", before)
	}
	target := filepath.Join(t.TempDir(), "moved")
	code, after, body := storageRequest(t, ats, "/api/storage/move", moveChatData{Path: target, Revision: before.Revision})
	if code != 200 {
		t.Fatalf("move: %d %s", code, body)
	}
	if after.Path != target || after.PreviousPath != before.Path || after.Revision == before.Revision {
		t.Fatalf("%+v", after)
	}
	for _, s := range []*Server{a, b} {
		mustBytes(t, filepath.Join(target, s.conversationName()), storageChat)
		mustBytes(t, filepath.Join(before.Path, s.conversationName()), storageChat)
	}
	mustBytes(t, filepath.Join(config, "secrets", "unrelated"), "protected credential")
	if _, err := os.Stat(filepath.Join(target, "secrets")); !os.IsNotExist(err) {
		t.Fatal("credentials moved")
	}
	next := `{"version":1,"chats":[{"id":"two"}]}`
	if code, _ := chatRequest(t, bts, "PUT", digestOf([]byte(storageChat)), next, true, ""); code != 200 {
		t.Fatal("other server did not follow move", code)
	}
	mustBytes(t, filepath.Join(target, b.conversationName()), next)
	mustBytes(t, filepath.Join(before.Path, b.conversationName()), storageChat)
	if code, _ := chatRequest(t, bts, "PUT", digestOf([]byte(storageChat)), storageChat, true, ""); code != 409 {
		t.Fatal("stale write accepted", code)
	}
	restarted, rts := startDesk(t, b.cfg)
	defer restarted.Close()
	defer rts.Close()
	if code, reply := chatRequest(t, rts, "GET", "", "", true, ""); code != 200 || string(reply.Content) != next {
		t.Fatal("restart lost history", code)
	}
}

func TestStorageMoveRefusalsPreserveOriginal(t *testing.T) {
	for _, kind := range []string{"stale", "project", "parent", "same", "nested", "public", "symlink", "nonempty", "foreign-store", "corrupt-history", "pointer-change", "disk-failure", "path-substitution", "active-work", "other-writer"} {
		t.Run(kind, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
				t.Fatal(code)
			}
			before := storageGet(t, ts)
			request := moveChatData{Path: filepath.Join(t.TempDir(), "target"), Revision: before.Revision}
			switch kind {
			case "stale":
				request.Revision = "stale"
			case "project":
				request.Path = filepath.Join(s.projectDir, "private")
			case "parent":
				request.Path = filepath.Dir(before.Path)
			case "same":
				request.Path = before.Path
			case "nested":
				request.Path = filepath.Join(before.Path, "nested")
			case "public":
				if err := os.Mkdir(request.Path, 0755); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(t.TempDir(), request.Path); err != nil {
					t.Fatal(err)
				}
			case "nonempty":
				os.Mkdir(request.Path, 0700)
				privateFixture(t, filepath.Join(request.Path, "keep"), "existing")
			case "foreign-store":
				os.Mkdir(request.Path, 0700)
				privateFixture(t, filepath.Join(request.Path, dataMarkerName), `{"version":1,"owner":"another"}`)
			case "corrupt-history":
				privateFixture(t, filepath.Join(before.Path, "conversations-"+strings.Repeat("a", 64)+".json"), `{"version":2,"chats":[]}`)
			case "pointer-change":
				testBeforeDataMoveCommit = func() error {
					return os.WriteFile(filepath.Join(s.configDir, dataLocationName), []byte(`{"version":5}`), 0600)
				}
			case "disk-failure":
				testBeforeDataMoveCommit = func() error { return errors.New("disk full") }
			case "path-substitution":
				testBeforeDataMoveCommit = func() error {
					if err := os.Rename(request.Path, request.Path+"-original"); err != nil {
						return err
					}
					return os.Mkdir(request.Path, 0700)
				}
			case "active-work":
				lock, err := s.privateDataFileLock(".data-work.lock", false)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
			case "other-writer":
				lock, err := s.privateDataLock(true)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
			}
			defer func() { testBeforeDataMoveCommit = nil }()
			code, _, body := storageRequest(t, ts, "/api/storage/move", request)
			if code == 200 {
				t.Fatalf("%s accepted: %s", kind, body)
			}
			mustBytes(t, filepath.Join(before.Path, s.conversationName()), storageChat)
			if kind != "pointer-change" && kind != "other-writer" {
				if status := storageGet(t, ts); status.Path != before.Path {
					t.Fatal("failure changed pointer", status.Path)
				}
			}
			if kind == "disk-failure" {
				if _, err := os.Stat(filepath.Join(request.Path, s.conversationName())); !os.IsNotExist(err) {
					t.Fatal("failed copy left history in destination", err)
				}
			}
		})
	}
}

func TestStorageWorkLockAllowsCheckpointWrites(t *testing.T) {
	s, ts, _ := assistantServer(t)
	lock, err := s.privateDataFileLock(".data-work.lock", false)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal("running work cannot save a checkpoint", code)
	}
}

func TestStorageGuardsAndInvalidPointer(t *testing.T) {
	s, ts, _ := assistantServer(t)
	for _, endpoint := range []string{"/api/storage", "/api/storage/move"} {
		method := "GET"
		if strings.HasSuffix(endpoint, "move") {
			method = "POST"
		}
		for _, origin := range []string{"", "https://foreign.example"} {
			req, _ := http.NewRequest(method, ts.URL+endpoint, strings.NewReader(`{}`))
			if origin != "" {
				bearer(req)
				req.Header.Set("Origin", origin)
			}
			response, err := ts.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			want := 401
			if origin != "" {
				want = 403
			}
			if response.StatusCode != want {
				t.Fatal(endpoint, response.StatusCode)
			}
		}
	}
	before := storageGet(t, ts)
	for _, data := range []string{`null`, `{"version":8,"path":"/tmp"}`, `{"version":1,"path":"relative"}`, `{"version":1,"version":1,"path":"/tmp"}`} {
		privateFixture(t, filepath.Join(s.configDir, dataLocationName), data)
		code, _, _ := storageRequest(t, ts, "/api/storage", nil)
		if code == 200 {
			t.Fatal("invalid pointer accepted", data)
		}
		if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code == 200 {
			t.Fatal("write bypassed invalid pointer")
		}
	}
	if _, err := os.Stat(filepath.Join(before.Path, s.conversationName())); !os.IsNotExist(err) {
		t.Fatal("invalid pointer created history")
	}
}

func TestStorageCanceledAndInterruptedCopy(t *testing.T) {
	for _, kind := range []string{"cancel", "copy-failure"} {
		t.Run(kind, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			chatRequest(t, ts, "PUT", "absent", storageChat, true, "")
			before := storageGet(t, ts)
			target := filepath.Join(t.TempDir(), "target")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			request := httptest.NewRequest("POST", "http://localhost/api/storage/move", nil).WithContext(ctx)
			writer := httptest.NewRecorder()
			copier := copyChatData
			if kind == "cancel" {
				testBeforeDataMoveCommit = func() error { cancel(); return nil }
				defer func() { testBeforeDataMoveCommit = nil }()
			} else {
				copier = func(ctx context.Context, source, target *chatDataStore, copied *[]string) error {
					if err := copyChatData(ctx, source, target, copied); err != nil {
						return err
					}
					return syscall.ENOSPC
				}
			}
			s.changeChatStorage(writer, request, moveChatData{Path: target, Revision: before.Revision}, copier)
			if writer.Code == 200 {
				t.Fatal("interrupted copy activated")
			}
			if current := storageGet(t, ts); current.Path != before.Path {
				t.Fatal("interrupted copy changed location")
			}
			mustBytes(t, filepath.Join(before.Path, s.conversationName()), storageChat)
			if _, err := os.Stat(filepath.Join(target, s.conversationName())); !os.IsNotExist(err) {
				t.Fatal("interrupted target kept partial data", err)
			}
		})
	}
}
func TestStorageLockAcrossProcesses(t *testing.T) {
	if path := os.Getenv("JPACK_STORAGE_LOCK_TEST"); path != "" {
		file, err := os.OpenFile(path, os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		err = lockPrivateData(file, true)
		if (err != nil) != (os.Getenv("JPACK_STORAGE_LOCK_EXPECT") == "held") {
			t.Fatal("unexpected cross-process lock result", err)
		}
		return
	}
	s, _, _ := assistantServer(t)
	file, err := s.privateDataLock(true)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"held", "free"} {
		if state == "free" {
			file.Close()
		}
		child := exec.Command(executable, "-test.run=^TestStorageLockAcrossProcesses$")
		child.Env = append(os.Environ(), "JPACK_STORAGE_LOCK_TEST="+filepath.Join(s.configDir, ".data.lock"), "JPACK_STORAGE_LOCK_EXPECT="+state)
		if output, err := child.CombinedOutput(); err != nil {
			t.Fatalf("child: %v %s", err, output)
		}
	}
}

func TestStorageBlocksRelocationDuringRelayRequests(t *testing.T) {
	for _, kind := range []string{"model", "gateway"} {
		t.Run(kind, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			released := false
			defer func() {
				if !released {
					close(release)
				}
			}()
			upstream := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			})
			var s *Server
			var ts *httptest.Server
			var url string
			if kind == "model" {
				s, ts, _ = relayDesk(t, "openai-compatible", upstream)
				url = relayURL(ts, "chat/completions")
			} else {
				s, ts = researchDesk(t, upstream)
				url = researchURL(ts, "acquire")
			}
			before := storageGet(t, ts)
			req, _ := http.NewRequest("POST", url, strings.NewReader(`{}`))
			authorizeAsPage(t, ts, req)
			done := make(chan error, 1)
			go func() {
				response, err := ts.Client().Do(req)
				if err == nil {
					_, err = io.Copy(io.Discard, response.Body)
					response.Body.Close()
					if response.StatusCode != 200 {
						err = fmt.Errorf("upstream response %d", response.StatusCode)
					}
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("relay did not reach the upstream")
			}
			target := filepath.Join(t.TempDir(), "moved")
			if code, _, body := storageRequest(t, ts, "/api/storage/move", moveChatData{Path: target, Revision: before.Revision}); code != 409 {
				t.Fatal("move was not blocked by active relay", code, body)
			}
			if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
				t.Fatal("running relay blocked checkpoint persistence", code)
			}
			close(release)
			released = true
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("relay did not finish")
			}
			if code, _, body := storageRequest(t, ts, "/api/storage/move", moveChatData{Path: target, Revision: before.Revision}); code != 200 {
				t.Fatal("move remained blocked after relay ended", code, body)
			}
			mustBytes(t, filepath.Join(target, s.conversationName()), storageChat)
		})
	}
}
