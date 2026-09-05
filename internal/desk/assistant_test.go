package desk

// What these tests are for.
//
// The assistant slot's whole claim is a custody claim: the key lives in one
// file on this machine, owner-only, and never leaves this process except into
// the request it was configured for. A claim like that is only worth what its
// tests can discriminate, so each one below is written to fail if the property
// it names is removed — the modes are read off the filesystem rather than from
// the constant, the log is a real buffer that is searched for the value, and
// the probe runs against an endpoint that records the headers it received.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

/* Helpers ------------------------------------------------------------------ */

// assistantServer is one chassis with a temporary desk-level directory and a
// log this test can read.
func assistantServer(t *testing.T) (*Server, *httptest.Server, *bytes.Buffer) {
	t.Helper()
	return assistantServerIn(t, t.TempDir())
}

// assistantServerIn is assistantServer with the desk-level directory chosen by
// the caller, for the cases that have to arrange that directory *before* the
// store validates and pins it.
func assistantServerIn(t *testing.T, config string) (*Server, *httptest.Server, *bytes.Buffer) {
	t.Helper()
	logged := &bytes.Buffer{}
	s, err := New(Config{
		ProjectDir:    t.TempDir(),
		JpackBin:      "jpack",
		Token:         testToken,
		Logger:        log.New(logged, "", 0),
		DeskConfigDir: config,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return s, ts, logged
}

// writeDeskConfig puts one desk-level file in place.
func writeDeskConfig(t *testing.T, s *Server, content string) {
	t.Helper()
	if err := os.MkdirAll(s.configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(s.deskConfigPath(), []byte(content), 0o644); err != nil {
		t.Fatalf("write desk.json: %v", err)
	}
}

// postJSON sends a bodiless POST carrying the token.
func postJSON(t *testing.T, ts *httptest.Server, path string) (int, map[string]any) {
	t.Helper()
	return sendJSON(t, ts, http.MethodPost, path, nil)
}

func sendJSON(
	t *testing.T, ts *httptest.Server, method, path string, body any,
) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	req, err := http.NewRequest(method, ts.URL+path+separator+"token="+testToken, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp.StatusCode, decoded
}

// rawBody is the response's bytes, for the assertions about what never travels.
func rawBody(t *testing.T, ts *httptest.Server, method, path string, body any) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, ts.URL+path+"?token="+testToken, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// storeKey puts one key through the endpoint that stores it.
func storeKey(t *testing.T, ts *httptest.Server, key string) (int, map[string]any) {
	t.Helper()
	return sendJSON(t, ts, http.MethodPut, "/api/assistant/key", map[string]string{"key": key})
}

// The key every test uses. Long enough to fingerprint, and distinctive enough
// that searching a log or a response body for it means something.
const testKey = "sk-desk-test-0123456789-abcdefghij"

/* Custody ------------------------------------------------------------------ */

func TestAssistantKeyModeBits(t *testing.T) {
	// A configuration tree that already exists, wide open, **before the desk
	// starts**. Validation and narrowing happen once, when the store is
	// pinned, so this is the moment at which a pre-existing loose directory
	// has to be dealt with — and `Mkdir` does nothing at all to a directory
	// that is already there, which is why the narrowing is unconditional.
	config := t.TempDir()
	if err := os.Mkdir(filepath.Join(config, secretsDirName), 0o777); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(filepath.Join(config, secretsDirName), 0o777); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	s, ts, _ := assistantServerIn(t, config)

	if status, body := storeKey(t, ts, testKey); status != http.StatusOK {
		t.Fatalf("store: %d %v", status, body)
	}

	dir, err := os.Stat(s.secretsDir())
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := dir.Mode().Perm(); got != 0o700 {
		t.Errorf("secrets directory is %#o, want 0700", got)
	}
	file, err := os.Stat(s.assistantKeyPath())
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if got := file.Mode().Perm(); got != 0o600 {
		t.Errorf("key file is %#o, want 0600", got)
	}
	stored, err := os.ReadFile(s.assistantKeyPath())
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	if string(stored) != testKey {
		t.Errorf("stored %q, want %q", stored, testKey)
	}
}

func TestAssistantKeyReplacedAtomically(t *testing.T) {
	s, ts, _ := assistantServer(t)

	if status, _ := storeKey(t, ts, testKey); status != http.StatusOK {
		t.Fatalf("first store: %d", status)
	}
	before, err := os.Stat(s.assistantKeyPath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	const second = "sk-desk-test-replacement-0987654321"
	if status, _ := storeKey(t, ts, second); status != http.StatusOK {
		t.Fatalf("second store: %d", status)
	}
	after, err := os.Stat(s.assistantKeyPath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	// **A different file at the same name.** This is what tells a rename from
	// a truncate-and-write: a write in place keeps the file it opened, and
	// leaves a window in which a reader sees neither key whole. `os.SameFile`
	// compares identity rather than contents, so it cannot be satisfied by the
	// bytes happening to differ.
	if os.SameFile(before, after) {
		t.Error("the key was written in place; a replace must publish a new file by rename")
	}
	if got := after.Mode().Perm(); got != 0o600 {
		t.Errorf("replaced key is %#o, want 0600", got)
	}
	stored, _ := os.ReadFile(s.assistantKeyPath())
	if string(stored) != second {
		t.Errorf("stored %q, want %q", stored, second)
	}

	// And nothing staged is left behind: the directory holds the key and
	// nothing else.
	entries, err := os.ReadDir(s.secretsDir())
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != assistantKeyName {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("secrets directory holds %v, want only %q", names, assistantKeyName)
	}
}

func TestAssistantKeyNeverInTheLog(t *testing.T) {
	s, ts, logged := assistantServer(t)

	// An endpoint that answers, so the probe's own path runs with the key in
	// hand rather than being skipped.
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer endpoint.Close()
	writeDeskConfig(t, s, fmt.Sprintf(
		`{"deskConfigVersion":1,"assistant":{"endpoint":{"url":%q,"kind":"openai-compatible","model":"m","tools":[]}}}`,
		endpoint.URL))

	if status, _ := storeKey(t, ts, testKey); status != http.StatusOK {
		t.Fatalf("store: %d", status)
	}
	if status, _ := postJSON(t, ts, "/api/assistant/probe"); status != http.StatusOK {
		t.Fatalf("probe: %d", status)
	}
	if status, _ := sendJSON(t, ts, http.MethodGet, "/api/assistant/key", nil); status != http.StatusOK {
		t.Fatalf("read: %d", status)
	}
	if status, _ := sendJSON(t, ts, http.MethodDelete, "/api/assistant/key", nil); status != http.StatusOK {
		t.Fatalf("delete: %d", status)
	}

	written := logged.String()
	// The log must have something in it, or this test proves nothing: an empty
	// buffer contains no key for the reason that it contains nothing at all.
	if !strings.Contains(written, "the assistant key was stored") {
		t.Fatalf("the log records nothing about the store, so the search below is vacuous: %q", written)
	}
	if strings.Contains(written, testKey) {
		t.Errorf("the key is in the log: %q", written)
	}
	// Nor any fragment of it long enough to matter.
	if strings.Contains(written, testKey[:16]) {
		t.Errorf("a prefix of the key is in the log: %q", written)
	}
}

func TestAssistantKeyNeverInAResponse(t *testing.T) {
	_, ts, _ := assistantServer(t)

	for _, call := range []struct {
		name   string
		method string
		body   any
	}{
		{"store", http.MethodPut, map[string]string{"key": testKey}},
		{"read", http.MethodGet, nil},
		{"delete", http.MethodDelete, nil},
	} {
		t.Run(call.name, func(t *testing.T) {
			status, body := rawBody(t, ts, call.method, "/api/assistant/key", call.body)
			if status != http.StatusOK {
				t.Fatalf("status %d: %s", status, body)
			}
			if strings.Contains(body, testKey) {
				t.Errorf("the key is in the response: %s", body)
			}
			// Four characters from each end is the most any answer may carry.
			if strings.Contains(body, testKey[:8]) {
				t.Errorf("more of the key than its fingerprint is in the response: %s", body)
			}
		})
	}
}

func TestAssistantKeyFingerprintShape(t *testing.T) {
	_, ts, _ := assistantServer(t)

	t.Run("absent", func(t *testing.T) {
		status, body := sendJSON(t, ts, http.MethodGet, "/api/assistant/key", nil)
		if status != http.StatusOK {
			t.Fatalf("status %d", status)
		}
		if body["present"] != false {
			t.Errorf("present %v, want false", body["present"])
		}
		if body["fingerprint"] != "" {
			t.Errorf("fingerprint %q, want empty", body["fingerprint"])
		}
	})

	t.Run("present", func(t *testing.T) {
		if status, _ := storeKey(t, ts, testKey); status != http.StatusOK {
			t.Fatal("store")
		}
		status, body := sendJSON(t, ts, http.MethodGet, "/api/assistant/key", nil)
		if status != http.StatusOK {
			t.Fatalf("status %d", status)
		}
		if body["present"] != true {
			t.Errorf("present %v, want true", body["present"])
		}
		want := testKey[:4] + "…" + testKey[len(testKey)-4:]
		if body["fingerprint"] != want {
			t.Errorf("fingerprint %q, want %q", body["fingerprint"], want)
		}
	})

	t.Run("gone after a delete", func(t *testing.T) {
		if status, _ := sendJSON(t, ts, http.MethodDelete, "/api/assistant/key", nil); status != http.StatusOK {
			t.Fatal("delete")
		}
		_, body := sendJSON(t, ts, http.MethodGet, "/api/assistant/key", nil)
		if body["present"] != false || body["fingerprint"] != "" {
			t.Errorf("after a delete: %v", body)
		}
	})
}

func TestAssistantFingerprintRefusesToDiscloseAShortKey(t *testing.T) {
	// Eight characters fingerprinted four-and-four is the whole key with an
	// ellipsis in the middle. The answer is no fingerprint, not a redaction
	// that redacts nothing.
	for _, key := range []string{"a", "12345678", "12345678901"} {
		if got := fingerprint(key); got != "" {
			t.Errorf("fingerprint(%q) = %q, want empty", key, got)
		}
	}
	if got := fingerprint("123456789012"); got != "1234…9012" {
		t.Errorf("fingerprint at the boundary = %q", got)
	}
	// Runes, not bytes: slicing UTF-8 in half would put replacement characters
	// on the page and call them a fingerprint.
	if got := fingerprint("αβγδεζηθικλμ"); got != "αβγδ…ικλμ" {
		t.Errorf("fingerprint of a non-ASCII key = %q", got)
	}
}

func TestAssistantKeyRefusals(t *testing.T) {
	_, ts, _ := assistantServer(t)

	t.Run("empty", func(t *testing.T) {
		status, body := storeKey(t, ts, "")
		if status != http.StatusBadRequest || body["code"] != CodeBadRequest {
			t.Fatalf("status %d, body %v", status, body)
		}
	})

	t.Run("whitespace only", func(t *testing.T) {
		status, body := storeKey(t, ts, "   \t\n  ")
		if status != http.StatusBadRequest {
			t.Fatalf("status %d, body %v", status, body)
		}
	})

	t.Run("a control character at either edge", func(t *testing.T) {
		// **These used to be accepted, and silently repaired.** The check ran
		// after `TrimSpace`, so a leading newline or a trailing tab was
		// trimmed away and the key stored — which made the stated contract
		// ("no control character") true only of the middle of a key. A
		// newline is not a stray space: it is the shape header injection
		// takes, and it is now refused wherever it sits.
		for _, tc := range []struct{ name, key string }{
			{"a leading newline", "\nsk-valid-looking-key"},
			{"a trailing newline", "sk-valid-looking-key\n"},
			{"a leading tab", "\tsk-valid-looking-key"},
			{"a trailing tab", "sk-valid-looking-key\t"},
			{"a leading carriage return", "\rsk-valid-looking-key"},
			{"a vertical tab in the middle", "sk-valid\vlooking-key"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				status, body := storeKey(t, ts, tc.key)
				if status != http.StatusBadRequest {
					t.Fatalf("status %d, body %v", status, body)
				}
				message, _ := body["error"].(string)
				if !strings.Contains(message, "control character") {
					t.Errorf("the refusal does not name the reason: %q", message)
				}
			})
		}
	})

	t.Run("an ordinary space at either edge is trimmed, not refused", func(t *testing.T) {
		// The other half of the same ruling, said out loud: only ordinary
		// whitespace is normalised, and only after the control check. A space
		// either side of a pasted key is a slip; a newline is not.
		status, body := storeKey(t, ts, "  sk-valid-looking-key  ")
		if status != http.StatusOK {
			t.Fatalf("status %d, body %v", status, body)
		}
		if body["fingerprint"] != "sk-v…-key" {
			t.Errorf("fingerprint %v — the key was not trimmed as stated", body["fingerprint"])
		}
		if status, _ := sendJSON(t, ts, http.MethodDelete, "/api/assistant/key", nil); status != http.StatusOK {
			t.Fatal("delete")
		}
	})

	t.Run("a control character", func(t *testing.T) {
		// A newline in a credential is header injection in an outbound
		// request, and it is also what a mis-paste looks like.
		status, body := storeKey(t, ts, "sk-good-prefix\r\nX-Evil: yes")
		if status != http.StatusBadRequest {
			t.Fatalf("status %d, body %v", status, body)
		}
		message, _ := body["error"].(string)
		if !strings.Contains(message, "control character") {
			t.Errorf("refusal does not name the reason: %q", message)
		}
		// And the refusal never quotes the value back.
		if strings.Contains(message, "sk-good-prefix") {
			t.Errorf("the refusal quotes the key: %q", message)
		}
	})

	t.Run("oversize", func(t *testing.T) {
		status, body := storeKey(t, ts, strings.Repeat("k", maxKeyBytes+1))
		if status != http.StatusRequestEntityTooLarge || body["code"] != CodeTooLarge {
			t.Fatalf("status %d, body %v", status, body)
		}
	})

	t.Run("far oversize is refused before it is buffered", func(t *testing.T) {
		status, body := storeKey(t, ts, strings.Repeat("k", 64<<10))
		if status != http.StatusRequestEntityTooLarge {
			t.Fatalf("status %d, body %v", status, body)
		}
	})

	t.Run("nothing was stored by any of them", func(t *testing.T) {
		_, body := sendJSON(t, ts, http.MethodGet, "/api/assistant/key", nil)
		if body["present"] != false {
			t.Errorf("a refused store left a key behind: %v", body)
		}
	})
}

func TestAssistantKeyAtTheSizeBoundary(t *testing.T) {
	_, ts, _ := assistantServer(t)
	// Exactly the maximum is accepted: the envelope allowance exists so that
	// the quotes and braces around a maximal key do not refuse it.
	status, body := storeKey(t, ts, strings.Repeat("k", maxKeyBytes))
	if status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}
}

/* The guard ---------------------------------------------------------------- */

func TestAssistantEndpointsAreGuarded(t *testing.T) {
	_, ts, _ := assistantServer(t)

	calls := []struct{ method, path string }{
		{http.MethodGet, "/api/desk-config"},
		{http.MethodGet, "/api/assistant/key"},
		{http.MethodPut, "/api/assistant/key"},
		{http.MethodDelete, "/api/assistant/key"},
		{http.MethodPost, "/api/assistant/probe"},
	}

	for _, call := range calls {
		t.Run(call.method+" "+call.path+" without a token", func(t *testing.T) {
			req, err := http.NewRequest(call.method, ts.URL+call.path, strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status %d, want 401", resp.StatusCode)
			}
			var body map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&body)
			if body["code"] != CodeUnauthorized {
				t.Errorf("code %v, want %s", body["code"], CodeUnauthorized)
			}
		})

		t.Run(call.method+" "+call.path+" from another origin", func(t *testing.T) {
			req, err := http.NewRequest(
				call.method, ts.URL+call.path+"?token="+testToken, strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			req.Header.Set("Origin", "https://elsewhere.example")
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status %d, want 403", resp.StatusCode)
			}
			var body map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&body)
			if body["code"] != CodeForbidden {
				t.Errorf("code %v, want %s", body["code"], CodeForbidden)
			}
		})
	}
}

func TestAGuardedStoreWritesNothing(t *testing.T) {
	// The guard has to run *before* the store, not beside it. A handler that
	// stored and then refused would pass every status assertion above.
	s, ts, _ := assistantServer(t)
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/assistant/key",
		strings.NewReader(`{"key":"`+testKey+`"}`))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if _, err := os.Stat(s.assistantKeyPath()); !os.IsNotExist(err) {
		t.Errorf("an unauthorized request stored a key: %v", err)
	}
}

/* The desk-level file ------------------------------------------------------ */

func TestDeskConfigRead(t *testing.T) {
	s, ts, _ := assistantServer(t)

	t.Run("absent is an answer, not a refusal", func(t *testing.T) {
		status, body := sendJSON(t, ts, http.MethodGet, "/api/desk-config", nil)
		if status != http.StatusOK {
			t.Fatalf("status %d, body %v", status, body)
		}
		if body["present"] != false {
			t.Errorf("present %v, want false", body["present"])
		}
		// The path is carried in both states, because it is what Admin tells
		// the reader to write.
		if body["path"] != s.deskConfigPath() {
			t.Errorf("path %v, want %q", body["path"], s.deskConfigPath())
		}
	})

	t.Run("present carries the bytes", func(t *testing.T) {
		writeDeskConfig(t, s, `{"deskConfigVersion":1}`)
		status, body := sendJSON(t, ts, http.MethodGet, "/api/desk-config", nil)
		if status != http.StatusOK {
			t.Fatalf("status %d", status)
		}
		if body["present"] != true {
			t.Errorf("present %v, want true", body["present"])
		}
		if body["content"] != `{"deskConfigVersion":1}` {
			t.Errorf("content %v", body["content"])
		}
	})

	t.Run("not UTF-8 is refused rather than mangled", func(t *testing.T) {
		if err := os.WriteFile(s.deskConfigPath(), []byte{0xff, 0xfe, 0x00}, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		status, body := sendJSON(t, ts, http.MethodGet, "/api/desk-config", nil)
		if status != http.StatusUnsupportedMediaType || body["code"] != CodeNotUTF8 {
			t.Fatalf("status %d, body %v", status, body)
		}
	})

	t.Run("a directory at the path is not a file", func(t *testing.T) {
		if err := os.Remove(s.deskConfigPath()); err != nil {
			t.Fatalf("remove: %v", err)
		}
		if err := os.Mkdir(s.deskConfigPath(), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		status, body := sendJSON(t, ts, http.MethodGet, "/api/desk-config", nil)
		if status != http.StatusBadRequest || body["code"] != CodeNotAFile {
			t.Fatalf("status %d, body %v", status, body)
		}
	})
}

func TestConfigDirHonoursXDG(t *testing.T) {
	// The README names one path. A build that resolved a different one on some
	// platform would make the README false there without saying so.
	t.Setenv("XDG_CONFIG_HOME", "/somewhere/config")
	if got := configDirFor(""); got != filepath.Join("/somewhere/config", "jpack-desk") {
		t.Errorf("with XDG_CONFIG_HOME set: %q", got)
	}
	// A relative value is ignored, as the specification says: honouring one
	// would resolve the desk's own configuration against whatever directory it
	// happened to be started in.
	t.Setenv("XDG_CONFIG_HOME", "relative/config")
	t.Setenv("HOME", "/home/someone")
	if got := configDirFor(""); got != filepath.Join("/home/someone", ".config", "jpack-desk") {
		t.Errorf("with a relative XDG_CONFIG_HOME: %q", got)
	}
	// And an explicit directory wins over both.
	if got := configDirFor("/explicit"); got != "/explicit" {
		t.Errorf("explicit: %q", got)
	}
}

/* The endpoint the file names ---------------------------------------------- */

func TestConfiguredEndpointRefusals(t *testing.T) {
	s, ts, _ := assistantServer(t)
	if status, _ := storeKey(t, ts, testKey); status != http.StatusOK {
		t.Fatal("store")
	}

	for _, tc := range []struct{ name, file, names string }{
		{
			"a member the endpoint does not declare",
			`{"deskConfigVersion":1,"assistant":{"endpoint":{"url":"https://e.example/v1","kind":"anthropic",` +
				`"model":"m","tools":[],"organization":"acme"}}}`,
			"organization",
		},
		{
			"a key pasted into the endpoint",
			`{"deskConfigVersion":1,"assistant":{"endpoint":{"url":"https://e.example/v1","kind":"anthropic",` +
				`"model":"m","tools":[],"apiKey":"sk-nope"}}}`,
			"apiKey",
		},
		{
			"a tool outside the allow-list",
			`{"deskConfigVersion":1,"assistant":{"endpoint":{"url":"https://e.example/v1","kind":"anthropic",` +
				`"model":"m","tools":["write_file"]}}}`,
			"write_file",
		},
		{
			"a kind this desk cannot speak",
			`{"deskConfigVersion":1,"assistant":{"endpoint":{"url":"https://e.example/v1","kind":"gemini",` +
				`"model":"m","tools":[]}}}`,
			"kind",
		},
		{
			"a URL that is not https and not loopback",
			`{"deskConfigVersion":1,"assistant":{"endpoint":{"url":"http://models.example/v1","kind":"anthropic",` +
				`"model":"m","tools":[]}}}`,
			"url",
		},
		{
			"no model",
			`{"deskConfigVersion":1,"assistant":{"endpoint":{"url":"https://e.example/v1","kind":"anthropic",` +
				`"model":"","tools":[]}}}`,
			"model",
		},
		{
			"a null endpoint",
			`{"deskConfigVersion":1,"assistant":{"endpoint":null}}`,
			"absent or null",
		},
		{
			"no assistant member at all",
			`{"deskConfigVersion":1}`,
			"absent or null",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeDeskConfig(t, s, tc.file)
			status, body := postJSON(t, ts, "/api/assistant/probe")
			if status != http.StatusConflict || body["code"] != CodeAssistantUnconfigured {
				t.Fatalf("status %d, body %v", status, body)
			}
			message, _ := body["error"].(string)
			if !strings.Contains(message, tc.names) {
				t.Errorf("the refusal does not name %q: %q", tc.names, message)
			}
		})
	}
}

func TestProbeRefusesWithoutAKey(t *testing.T) {
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, `{"deskConfigVersion":1,"assistant":{"endpoint":{"url":"https://e.example/v1",`+
		`"kind":"anthropic","model":"m","tools":[]}}}`)
	status, body := postJSON(t, ts, "/api/assistant/probe")
	if status != http.StatusConflict || body["code"] != CodeAssistantNoKey {
		t.Fatalf("status %d, body %v", status, body)
	}
	message, _ := body["error"].(string)
	if !strings.Contains(message, "no key is stored") {
		t.Errorf("the refusal does not name the missing key: %q", message)
	}
}

func TestTheToolAllowListIsTheFourReadOnlyOnes(t *testing.T) {
	// Held here as a declaration a reader can check against the page's own
	// list, which `assistant/enforcement.test.ts` reads out of this file.
	want := []string{"get_schema", "get_example", "validate", "experimental_evaluate"}
	if len(AssistantTools) != len(want) {
		t.Fatalf("AssistantTools = %v", AssistantTools)
	}
	for i, tool := range want {
		if AssistantTools[i] != tool {
			t.Fatalf("AssistantTools = %v, want %v", AssistantTools, want)
		}
	}
}

/* The probe ---------------------------------------------------------------- */

// stubEndpoint records what it was asked and answers what the test says.
type stubEndpoint struct {
	server  *httptest.Server
	mu      sync.Mutex
	method  string
	path    string
	headers http.Header
	body    string
}

func newStubEndpoint(t *testing.T, answer func(w http.ResponseWriter)) *stubEndpoint {
	t.Helper()
	stub := &stubEndpoint{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		stub.mu.Lock()
		stub.method, stub.path, stub.headers, stub.body = r.Method, r.URL.Path, r.Header.Clone(), string(body)
		stub.mu.Unlock()
		answer(w)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *stubEndpoint) saw() (string, string, http.Header, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.method, s.path, s.headers, s.body
}

// probeAgainst runs one probe end to end through the chassis.
func probeAgainst(t *testing.T, kind, endpoint string) (int, map[string]any) {
	t.Helper()
	status, raw := probeAgainstRaw(t, kind, endpoint)
	var body map[string]any
	_ = json.Unmarshal([]byte(raw), &body)
	return status, body
}

// probeAgainstRaw is probeAgainst with the answer's bytes, for the assertions
// about what never travels: a decoded map cannot show what a member contains
// inside a string this test never thought to look at.
func probeAgainstRaw(t *testing.T, kind, endpoint string) (int, string) {
	t.Helper()
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, fmt.Sprintf(
		`{"deskConfigVersion":1,"assistant":{"endpoint":{"url":%q,"kind":%q,`+
			`"model":"a-model","tools":[]}}}`, endpoint, kind))
	if status, body := storeKey(t, ts, testKey); status != http.StatusOK {
		t.Fatalf("store: %d %v", status, body)
	}
	return rawBody(t, ts, http.MethodPost, "/api/assistant/probe", nil)
}

func TestProbeSpeaksTheOpenAICompatibleProtocol(t *testing.T) {
	stub := newStubEndpoint(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"a-model"}]}`))
	})

	// TLS is not required on loopback, which is what makes an endpoint someone
	// runs on their own machine configurable at all. httptest serves plain
	// HTTP on 127.0.0.1, so this is that case exactly.
	status, body := probeAgainst(t, "openai-compatible", stub.server.URL+"/v1")
	if status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}
	if body["reachable"] != true {
		t.Errorf("reachable %v, want true (%v)", body["reachable"], body)
	}
	if body["status"] != float64(200) {
		t.Errorf("status %v, want 200", body["status"])
	}
	if body["diagnostic"] != "" {
		t.Errorf("diagnostic %q, want empty on a success", body["diagnostic"])
	}

	method, path, headers, _ := stub.saw()
	if method != http.MethodGet || path != "/v1/models" {
		t.Errorf("the probe sent %s %s, want GET /v1/models", method, path)
	}
	if got := headers.Get("Authorization"); got != "Bearer "+testKey {
		t.Errorf("Authorization %q", got)
	}
}

func TestProbeSpeaksTheAnthropicProtocol(t *testing.T) {
	stub := newStubEndpoint(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","content":[]}`))
	})

	status, body := probeAgainst(t, "anthropic", stub.server.URL)
	if status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}
	if body["reachable"] != true {
		t.Errorf("reachable %v, want true (%v)", body["reachable"], body)
	}

	method, path, headers, sent := stub.saw()
	if method != http.MethodPost || path != "/v1/messages" {
		t.Errorf("the probe sent %s %s, want POST /v1/messages", method, path)
	}
	// The credential goes in this protocol's own header, not the other one's.
	if got := headers.Get("x-api-key"); got != testKey {
		t.Errorf("x-api-key %q", got)
	}
	if headers.Get("Authorization") != "" {
		t.Errorf("the anthropic probe sent an Authorization header too")
	}
	if got := headers.Get("anthropic-version"); got == "" {
		t.Error("no anthropic-version header; this protocol refuses a request without one")
	}
	// The smallest legitimate request: one output token.
	var payload struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
	}
	if err := json.Unmarshal([]byte(sent), &payload); err != nil {
		t.Fatalf("the probe body is not JSON: %q", sent)
	}
	if payload.MaxTokens != 1 {
		t.Errorf("max_tokens %d, want 1", payload.MaxTokens)
	}
	if payload.Model != "a-model" {
		t.Errorf("model %q, want the configured one", payload.Model)
	}
}

func TestProbeReportsARefusedCredential(t *testing.T) {
	stub := newStubEndpoint(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid x-api-key"}}`))
	})

	status, body := probeAgainst(t, "anthropic", stub.server.URL)
	if status != http.StatusOK {
		t.Fatalf("a probe that reached a refusal is still an answered probe: %d", status)
	}
	// **A 401 is not reachable.** The host is there and it will not take this
	// credential, and reporting that as ready would describe a desk that
	// cannot make one call as configured.
	if body["reachable"] != false {
		t.Errorf("reachable %v, want false", body["reachable"])
	}
	if body["status"] != float64(401) {
		t.Errorf("status %v, want 401", body["status"])
	}
	// **One word from the fixed vocabulary, and none of what the endpoint
	// wrote.** The sentence used to be quoted; it is not any more, because a
	// body under the endpoint's control can carry a derived representation of
	// the credential that no substitution reliably finds.
	if body["diagnostic"] != DiagnosticUnauthorized {
		t.Errorf("diagnostic %q, want %q", body["diagnostic"], DiagnosticUnauthorized)
	}
}

func TestProbeNeverRepeatsWhatTheEndpointWrote(t *testing.T) {
	// The endpoint echoes the credential back — some do, in the name of being
	// helpful — and encodes it four ways besides. **None of it may travel.**
	//
	// This is why the sentence is no longer quoted at all. The previous design
	// removed the literal key from the body with one substitution, which is a
	// categorical promise ("never sent back to the browser") held by a
	// `strings.ReplaceAll`: base64, percent-encoding, JSON escaping, hex and
	// any partial echo walked straight past it.
	encoded := base64.StdEncoding.EncodeToString([]byte(testKey))
	urlEncoded := url.QueryEscape(testKey)
	hexed := hex.EncodeToString([]byte(testKey))
	half := testKey[:len(testKey)/2]
	stub := newStubEndpoint(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"the key ` + testKey + ` (` + encoded + `, ` +
			urlEncoded + `, ` + hexed + `, ` + half + `) is not valid"}}`))
	})

	_, raw := probeAgainstRaw(t, "anthropic", stub.server.URL)
	for name, forbidden := range map[string]string{
		"the key itself":  testKey,
		"base64":          encoded,
		"percent-encoded": urlEncoded,
		"hex":             hexed,
		"half of it":      half,
		"the sentence":    "is not valid",
	} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("%s reached the page: %s", name, raw)
		}
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("the answer is not JSON: %s", raw)
	}
	if body["diagnostic"] != DiagnosticUnauthorized {
		t.Errorf("diagnostic %q, want %q", body["diagnostic"], DiagnosticUnauthorized)
	}
}

func TestProbeDiagnosticsComeFromTheClosedVocabulary(t *testing.T) {
	// Every answer this can give, and nothing outside the list. A vocabulary
	// that grew a member nobody declared would be the endpoint's text coming
	// back by another route.
	for _, tc := range []struct {
		name   string
		status int
		want   string
	}{
		{"unauthorized", http.StatusUnauthorized, DiagnosticUnauthorized},
		{"forbidden", http.StatusForbidden, DiagnosticForbidden},
		{"not found", http.StatusNotFound, DiagnosticNotFound},
		{"anything else", http.StatusBadGateway, DiagnosticUnexpected},
		{"a redirect", http.StatusFound, DiagnosticUnexpected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := tc.status
			stub := newStubEndpoint(t, func(w http.ResponseWriter) {
				if status == http.StatusFound {
					w.Header().Set("Location", "https://elsewhere.example/")
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"message":"never quoted"}}`))
			})
			_, body := probeAgainst(t, "openai-compatible", stub.server.URL+"/v1")
			if body["diagnostic"] != tc.want {
				t.Errorf("diagnostic %v, want %q", body["diagnostic"], tc.want)
			}
			if !contains(AssistantDiagnostics, body["diagnostic"].(string)) {
				t.Errorf("%q is not in the declared vocabulary", body["diagnostic"])
			}
		})
	}
}

func TestProbeDoesNotFollowARedirect(t *testing.T) {
	// Go strips Authorization across hosts and knows nothing about x-api-key,
	// so a followed redirect could walk the anthropic credential to a host
	// nobody configured. The redirect is the answer instead.
	elsewhere := newStubEndpoint(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	})
	stub := newStubEndpoint(t, func(w http.ResponseWriter) {
		w.Header().Set("Location", elsewhere.server.URL+"/v1/messages")
		w.WriteHeader(http.StatusFound)
	})

	_, body := probeAgainst(t, "anthropic", stub.server.URL)
	if body["status"] != float64(302) {
		t.Errorf("status %v, want the redirect itself", body["status"])
	}
	if body["reachable"] != false {
		t.Errorf("reachable %v, want false", body["reachable"])
	}
	if _, _, headers, _ := elsewhere.saw(); headers != nil {
		t.Errorf("the probe followed the redirect and presented %v", headers.Get("x-api-key"))
	}
}

func TestProbeIsBounded(t *testing.T) {
	// An endpoint that answers far too late. With the bound removed this test
	// still finishes — after the endpoint's own delay — and fails on the
	// values, rather than hanging the suite.
	stub := newStubEndpoint(t, func(w http.ResponseWriter) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	})

	restore := probeTimeout
	probeTimeout = 50 * time.Millisecond
	t.Cleanup(func() { probeTimeout = restore })

	started := time.Now()
	_, body := probeAgainst(t, "openai-compatible", stub.server.URL+"/v1")
	elapsed := time.Since(started)

	if body["reachable"] != false {
		t.Errorf("reachable %v, want false", body["reachable"])
	}
	if body["status"] != float64(0) {
		t.Errorf("status %v, want 0 — no response arrived", body["status"])
	}
	if elapsed > time.Second {
		t.Errorf("the probe took %v; the bound did not apply", elapsed)
	}
	if body["diagnostic"] != DiagnosticTimeout {
		t.Errorf("diagnostic %v, want %q", body["diagnostic"], DiagnosticTimeout)
	}
}

func TestProbeTimeoutDefaultsToTenSeconds(t *testing.T) {
	// The value the README states. It is a var so a test can shorten it, and
	// this is what stops that flexibility from quietly becoming the default.
	if probeTimeout != 10*time.Second {
		t.Errorf("probeTimeout = %v, want 10s", probeTimeout)
	}
}

func TestProbeReportsAnEndpointThatIsNotThere(t *testing.T) {
	// A port nothing is listening on: a transport failure rather than an
	// answer, which is status 0 and the transport's own sentence.
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := closed.URL
	closed.Close()

	_, body := probeAgainst(t, "openai-compatible", url+"/v1")
	if body["reachable"] != false || body["status"] != float64(0) {
		t.Errorf("body %v", body)
	}
	if body["diagnostic"] != DiagnosticRefused {
		t.Errorf("diagnostic %v, want %q — nothing was listening", body["diagnostic"],
			DiagnosticRefused)
	}
}

func TestProbeMeasuresLatency(t *testing.T) {
	stub := newStubEndpoint(t, func(w http.ResponseWriter) {
		time.Sleep(25 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})
	_, body := probeAgainst(t, "openai-compatible", stub.server.URL+"/v1")
	latency, _ := body["latencyMs"].(float64)
	if latency < 20 {
		t.Errorf("latencyMs %v, want at least the endpoint's own delay", latency)
	}
}

func TestProbeAcceptsAnHTTPSURLWithoutReachingIt(t *testing.T) {
	// The URL rule is about transport and not about who is at the other end:
	// an https: URL is accepted whatever host it names, and this one simply
	// does not resolve. What matters is that it was refused by the network and
	// not by a list of hosts.
	_, body := probeAgainst(t, "anthropic", "https://nothing.invalid")
	if body["reachable"] != false {
		t.Errorf("body %v", body)
	}
	if _, ok := body["code"]; ok {
		t.Errorf("an https: URL was refused before it was tried: %v", body)
	}
}

func TestProbeEndpointDirectlyHonoursACallerDeadline(t *testing.T) {
	// The handler's bound and the caller's compose: whichever is shorter wins,
	// which is what makes a disconnected browser stop an outbound request.
	stub := newStubEndpoint(t, func(w http.ResponseWriter) {
		time.Sleep(time.Second)
		w.WriteHeader(http.StatusOK)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result := probeEndpoint(ctx, assistantEndpoint{
		url:   stub.server.URL + "/v1",
		kind:  "openai-compatible",
		model: "m",
	}, testKey)
	if result.Reachable || result.Status != 0 {
		t.Errorf("result %+v", result)
	}
}

func TestTheProbeLogsAnOriginAndNothingElse(t *testing.T) {
	// **A configured URL may carry a query string** — some gateways route on
	// one — and a query string is where a presigned link keeps its
	// credential. Logging the whole URL therefore falsified "the key is never
	// logged" for a configuration this desk accepts. Scheme and host is enough
	// to tell one endpoint from another in a log.
	stub := newStubEndpoint(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	})
	s, ts, logged := assistantServer(t)
	const routing = "sig=THIS-LOOKS-LIKE-A-SECRET&tenant=acme"
	writeDeskConfig(t, s, fmt.Sprintf(
		`{"deskConfigVersion":1,"assistant":{"endpoint":{"url":%q,"kind":"openai-compatible",`+
			`"model":"a-model","tools":[]}}}`, stub.server.URL+"/v1?"+routing))
	if status, body := storeKey(t, ts, testKey); status != http.StatusOK {
		t.Fatalf("store: %d %v", status, body)
	}
	if status, body := postJSON(t, ts, "/api/assistant/probe"); status != http.StatusOK {
		t.Fatalf("probe: %d %v", status, body)
	}

	written := logged.String()
	if !strings.Contains(written, "assistant probe") {
		t.Fatalf("the probe logged nothing, so the search below is vacuous: %q", written)
	}
	// The origin is there, so the line is still useful.
	if !strings.Contains(written, stub.server.URL) {
		t.Errorf("the log does not name the endpoint's origin: %q", written)
	}
	// And nothing past it.
	for _, forbidden := range []string{routing, "THIS-LOOKS-LIKE-A-SECRET", "sig=", "/v1?"} {
		if strings.Contains(written, forbidden) {
			t.Errorf("the log carries %q: %q", forbidden, written)
		}
	}
}

func TestALoggableOriginIsSchemeAndHostOnly(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"https://gw.example/v1?sig=secret", "https://gw.example"},
		{"https://gw.example:8443/v1", "https://gw.example:8443"},
		// The port is kept: it is what tells two endpoints on one host apart.
		{"http://127.0.0.1:11434/v1", "http://127.0.0.1:11434"},
		{"not a url at all", "the configured endpoint"},
	} {
		if got := loggableOrigin(tc.raw); got != tc.want {
			t.Errorf("loggableOrigin(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
