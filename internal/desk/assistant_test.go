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
	"errors"
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

// The endpoint a key is bound to where the case is about the key rather than
// about the destination.
const (
	defaultTestEndpoint = "https://e.example/v1"
	defaultTestOrigin   = "https://e.example"
	defaultTestKind     = "anthropic"
)

// configureAnEndpoint puts one acceptable endpoint in the desk-level file.
//
// **Storing a key needs one**, and that is the whole of the binding: the key
// is kept together with the scheme, host and wire protocol of the endpoint
// configured at that instant, and is presented nowhere else. A key with
// nothing to bind to would be a key bound to whatever was configured next,
// which is the arrangement the binding replaces.
func configureAnEndpoint(t *testing.T, s *Server) {
	t.Helper()
	configureEndpoint(t, s, defaultTestKind, defaultTestEndpoint)
}

func configureEndpoint(t *testing.T, s *Server, kind, endpointURL string) {
	t.Helper()
	writeDeskConfig(t, s, fmt.Sprintf(
		`{"deskConfigVersion":1,"assistant":{"endpoint":`+
			`{"url":%q,"kind":%q,"model":"a-model","tools":[]}}}`, endpointURL, kind))
}

// storeKeyBoundTo configures one endpoint and stores the key against it,
// leaving the file for the caller to replace.
func storeKeyBoundTo(t *testing.T, s *Server, ts *httptest.Server, kind, endpointURL string) {
	t.Helper()
	configureEndpoint(t, s, kind, endpointURL)
	if status, body := storeKey(t, ts, testKey); status != http.StatusOK {
		t.Fatalf("store: %d %v", status, body)
	}
}

/* Custody ------------------------------------------------------------------ */

func TestAssistantKeyModeBits(t *testing.T) {
	// A configuration tree that already exists **before the desk starts**, at
	// the mode a umask of 022 produces. Validation and narrowing happen once,
	// when the store is pinned, so this is the moment at which a pre-existing
	// directory has to be dealt with — and `Mkdir` does nothing at all to a
	// directory that is already there, which is why the narrowing is
	// unconditional. `0755` and not `0777`: nobody else could have written
	// into it, so there is nothing that might already be there and it is
	// repaired. One anybody could write to is refused instead — see
	// TestCustodyRefusesAFormerlyWritableDirectory.
	config := t.TempDir()
	if err := os.Mkdir(filepath.Join(config, secretsDirName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(filepath.Join(config, secretsDirName), 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	s, ts, _ := assistantServerIn(t, config)
	// Storing binds, so there has to be something to bind to.
	configureAnEndpoint(t, s)

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
	// **The file is the key and the destination it was entered for**, in the
	// versioned record `readKey` insists on. Asserted on the bytes so the
	// format is pinned here as well as in the reader: a build that wrote a
	// bare key again would be a build presenting a credential with no binding.
	if got := onDiskKey(t, stored); got.Key != testKey {
		t.Errorf("stored key %q, want %q", got.Key, testKey)
	} else if got.Origin != defaultTestOrigin || got.Kind != defaultTestKind {
		t.Errorf("stored binding %s over %q", got.Origin, got.Kind)
	}
}

// onDiskKey reads the key file's own bytes as the record they are.
func onDiskKey(t *testing.T, data []byte) storedKeyFile {
	t.Helper()
	var record storedKeyFile
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("the key file is not the versioned record: %q", data)
	}
	if record.Version != storedKeyVersion {
		t.Errorf("assistantKeyVersion %d, want %d", record.Version, storedKeyVersion)
	}
	return record
}

func TestAssistantKeyReplacedAtomically(t *testing.T) {
	s, ts, _ := assistantServer(t)
	configureAnEndpoint(t, s)

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
	if got := onDiskKey(t, stored); got.Key != second {
		t.Errorf("stored %q, want %q", got.Key, second)
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
	s, ts, _ := assistantServer(t)
	configureAnEndpoint(t, s)

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
	s, ts, _ := assistantServer(t)
	configureAnEndpoint(t, s)

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
	s, ts, _ := assistantServer(t)
	configureAnEndpoint(t, s)

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
		status, body := storeKey(t, ts, strings.Repeat("k", (4<<10)+1))
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
	// **The bound is asserted, then a literal of that size is sent.** Building
	// the body from `maxKeyBytes` itself read well and made the mutation that
	// raises the bound allocate a gigabyte, which hangs the suite — and a
	// mutation that hangs has not been survived, it has not been tested. The
	// constant is checked first, so raising it fails here in microseconds.
	if maxKeyBytes != 4<<10 {
		t.Fatalf("maxKeyBytes = %d, want 4096", maxKeyBytes)
	}
	s, ts, _ := assistantServer(t)
	configureAnEndpoint(t, s)
	// Exactly the maximum is accepted: the envelope allowance exists so that
	// the quotes and braces around a maximal key do not refuse it.
	status, body := storeKey(t, ts, strings.Repeat("k", 4<<10))
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

/* The one write to the desk-level file ------------------------------------- */

// putDeskConfig sends one desk-level write.
func putDeskConfig(
	t *testing.T, ts *httptest.Server, assistant, ifMatch string,
) (int, map[string]any) {
	t.Helper()
	return sendJSON(t, ts, http.MethodPut, "/api/desk-config", map[string]any{
		"assistant": json.RawMessage(assistant),
		"ifMatch":   ifMatch,
	})
}

// deskConfigDigest is what `GET /api/desk-config` says the file hashes to.
func deskConfigDigest(t *testing.T, ts *httptest.Server) (string, bool) {
	t.Helper()
	status, body := sendJSON(t, ts, http.MethodGet, "/api/desk-config", nil)
	if status != http.StatusOK {
		t.Fatalf("read: status %d, body %v", status, body)
	}
	digest, _ := body["sha256"].(string)
	present, _ := body["present"].(bool)
	return digest, present
}

// geminiAssistant is the object a page would send to configure the wire this
// chunk added.
const geminiAssistant = `{"endpoint":{"url":"https://api.example.invalid/",` +
	`"kind":"gemini","model":"a-model","tools":["validate"]},"engine":"vercel",` +
	`"thinking":"on"}`

func TestDeskConfigReadCarriesTheDigestItsWriteWillQuote(t *testing.T) {
	s, ts, _ := assistantServer(t)
	// Absent: the sentinel, and not a digest of no bytes. The two are
	// different states and a page sends the sentinel back to create the file.
	if digest, present := deskConfigDigest(t, ts); digest != "" || present {
		t.Fatalf("an absent file answered %q / present %v", digest, present)
	}
	writeDeskConfig(t, s, `{"deskConfigVersion":1}`)
	digest, present := deskConfigDigest(t, ts)
	if !present {
		t.Fatal("present false for a file that is there")
	}
	if want := digestOf([]byte(`{"deskConfigVersion":1}`)); digest != want {
		t.Errorf("sha256 %q, want %q", digest, want)
	}
}

func TestDeskConfigWriteRoundTrips(t *testing.T) {
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, "{\n  \"deskConfigVersion\": 1,\n  \"identity\": {\n"+
		"    \"provider\": null\n  }\n}\n")
	before, _ := deskConfigDigest(t, ts)

	status, body := putDeskConfig(t, ts, geminiAssistant, before)
	if status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}
	// The answer is taken off the disk, so its digest is the file's.
	after, present := deskConfigDigest(t, ts)
	if !present {
		t.Fatal("the file is not there after a write")
	}
	if body["sha256"] != after {
		t.Errorf("the answer's sha256 %v is not the file's %q", body["sha256"], after)
	}
	if body["created"] != false {
		t.Errorf("created %v, want false for a file that was already there", body["created"])
	}
	// **The decoded slot, off the bytes that landed**, and not the request
	// echoed: the tier the page asked for, and the engine default it did not.
	slot, _ := body["assistant"].(map[string]any)
	if slot["thinking"] != "on" || slot["engine"] != "vercel" {
		t.Errorf("the answer's slot is %v", slot)
	}
	answered, _ := slot["endpoint"].(map[string]any)
	if answered["kind"] != "gemini" || answered["model"] != "a-model" {
		t.Errorf("the answer's endpoint is %v", answered)
	}

	// **The composed bytes decode to what the page sent.** Read back through
	// the decoder both sides share rather than compared as text.
	written, err := os.ReadFile(s.deskConfigPath())
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	decoded := decodeDeskFile(written)
	if decoded.refused() {
		t.Fatalf("the written file is refused: %v", decoded.Problems)
	}
	if decoded.Endpoint == nil {
		t.Fatal("the written file configures no endpoint")
	}
	if decoded.Endpoint.kind != "gemini" || decoded.Endpoint.model != "a-model" {
		t.Errorf("endpoint %+v", *decoded.Endpoint)
	}
	if decoded.Thinking != "on" || decoded.Engine != "vercel" {
		t.Errorf("engine %q thinking %q", decoded.Engine, decoded.Thinking)
	}
	// And the relay agrees, which is the only reason this route exists: the
	// endpoint the desk would now reach is the one the page asked for.
	endpoint, err := s.configuredEndpoint()
	if err != nil {
		t.Fatalf("configuredEndpoint: %v", err)
	}
	if endpoint.kind != "gemini" {
		t.Errorf("the desk would reach a %q endpoint", endpoint.kind)
	}
}

func TestDeskConfigWriteKeepsEveryOtherMemberByteForByte(t *testing.T) {
	// **The identity block is somebody's, and this route was asked about the
	// assistant.** It is carried across by its own bytes, in its own place, so
	// a rewrite of one member does not quietly restate the rest of the file.
	//
	// **Round 1 rewrote this case.** The block it used was already formatted
	// exactly as `json.Indent` emits, so the test passed while every retained
	// member was in fact being reflowed — it proved the fixture's shape rather
	// than the property. This one is compact on one line, with tabs and odd
	// spacing that nothing in this repository would emit, so it can only
	// survive by being copied.
	s, ts, _ := assistantServer(t)
	const identity = "  \"identity\": {\"provider\":{\"label\":\"Sign in\",   " +
		"\"issuer\":\"https://issuer.example.invalid/\",\t\"clientId\":\"abc\",\n" +
		"\t\t\t\"scopes\":[\"openid\",\"profile\"],\"audience\":null," +
		"\"showRemoteAvatar\":false,\"signOut\":\"local\"}}"
	writeDeskConfig(t, s, "{\n  \"deskConfigVersion\": 1,\n"+identity+",\n"+
		"  \"assistant\": {\n    \"endpoint\": null\n  }\n}\n")
	before, _ := deskConfigDigest(t, ts)
	if status, body := putDeskConfig(t, ts, geminiAssistant, before); status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}
	written, err := os.ReadFile(s.deskConfigPath())
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(written), identity) {
		t.Errorf("the identity block did not survive byte for byte:\n%s", written)
	}
	// Its place, too: `identity` was written before `assistant` and still is.
	if strings.Index(string(written), `"identity"`) >
		strings.Index(string(written), `"assistant"`) {
		t.Errorf("the members were reordered:\n%s", written)
	}
	if !strings.HasSuffix(string(written), "}\n") {
		t.Errorf("no trailing newline:\n%q", written)
	}
	// And the file still reads, which is what makes preserving somebody's
	// whitespace a service rather than a hazard.
	if decoded := decodeDeskFile(written); decoded.refused() {
		t.Fatalf("the rewritten file is refused: %v", decoded.Problems)
	}
}

func TestDeskConfigWriteRefusesAFileWithADuplicateTopLevelMember(t *testing.T) {
	// **Round 1.** Values came from a map and positions from the walk, so two
	// spellings of one name became the *last* value written at the *first*
	// position — a rewrite that silently changed what the file says.
	// `encoding/json` keeps the last and a reader in another language may keep
	// the first, so this desk will not compose over one at all.
	s, ts, _ := assistantServer(t)
	const original = "{\n  \"deskConfigVersion\": 1,\n" +
		"  \"organization\": {\"name\": \"first\"},\n" +
		"  \"organization\": {\"name\": \"second\"}\n}\n"
	writeDeskConfig(t, s, original)
	before, _ := deskConfigDigest(t, ts)
	status, body := putDeskConfig(t, ts, geminiAssistant, before)
	if status != http.StatusUnprocessableEntity || body["code"] != CodeDeskConfigRefused {
		t.Fatalf("status %d, body %v; want 422 %s", status, body, CodeDeskConfigRefused)
	}
	named := false
	problems, _ := body["problems"].([]any)
	for _, problem := range problems {
		entry, _ := problem.(map[string]any)
		if entry["key"] == "organization" {
			named = true
		}
	}
	if !named {
		t.Errorf("the duplicated member is not named: %v", body["problems"])
	}
	after, err := os.ReadFile(s.deskConfigPath())
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(after) != original {
		t.Errorf("the file was rewritten anyway:\n%s", after)
	}
}

func TestDeskConfigWriteRefusesAFileReplacedWhileItWasBeingStaged(t *testing.T) {
	// **Round 1, and the window the mutex could never cover.** The digest is
	// compared against the bytes the transaction read; an ordinary editor —
	// which takes no lock of this desk's — could replace `desk.json` between
	// that comparison and the rename, and the route overwrote its revision and
	// reported success. The comparison now runs again after staging and before
	// publishing, and this hook is that instant exactly.
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, "{\n  \"deskConfigVersion\": 1\n}\n")
	before, _ := deskConfigDigest(t, ts)

	const editors = "{\n  \"deskConfigVersion\": 1,\n  \"organization\": " +
		"{\n    \"name\": \"what the editor wrote\"\n  }\n}\n"
	swapped := false
	testHookBeforeConfigRename = func(path string) {
		if swapped {
			return
		}
		swapped = true
		if err := os.WriteFile(path, []byte(editors), 0o600); err != nil {
			t.Errorf("the editor's write failed: %v", err)
		}
	}
	t.Cleanup(func() { testHookBeforeConfigRename = nil })

	status, body := putDeskConfig(t, ts, geminiAssistant, before)
	if status != http.StatusConflict || body["code"] != CodeDeskConfigChanged {
		t.Fatalf("status %d, body %v; want 409 %s", status, body, CodeDeskConfigChanged)
	}
	if body["actualSha256"] != digestOf([]byte(editors)) {
		t.Errorf("actualSha256 %v, want the editor's bytes", body["actualSha256"])
	}
	// **The editor's file is what is there**, whole, and no staging file is
	// left beside it.
	after, err := os.ReadFile(s.deskConfigPath())
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(after) != editors {
		t.Errorf("the editor's write was overwritten:\n%s", after)
	}
	entries, err := os.ReadDir(s.configDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), configStagingPrefix) {
			t.Errorf("a staging file survived the refusal: %s", entry.Name())
		}
	}
}

func TestDeskConfigWriteCreatesAFileWhereThereWasNone(t *testing.T) {
	_, ts, _ := assistantServer(t)
	status, body := putDeskConfig(t, ts, geminiAssistant, "")
	if status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}
	if body["created"] != true {
		t.Errorf("created %v, want true", body["created"])
	}
	// The defaults are in the answer because they are in the decode, and a
	// page that read its own request back would not have them.
	slot, _ := body["assistant"].(map[string]any)
	if slot["engine"] != "vercel" || slot["thinking"] != "on" {
		t.Errorf("the answer's slot is %v", slot)
	}
	// The version is the chassis' own constant. A page that could choose it
	// could ask this desk to write a file it will not read.
	if _, present := deskConfigDigest(t, ts); !present {
		t.Fatal("no file was created")
	}
}

func TestDeskConfigWriteRefusesAMismatchedIfMatch(t *testing.T) {
	s, ts, _ := assistantServer(t)
	const original = "{\n  \"deskConfigVersion\": 1\n}\n"
	writeDeskConfig(t, s, original)

	for _, testCase := range []struct{ name, ifMatch string }{
		{"a digest of other bytes", digestOf([]byte("something else"))},
		{"the empty-file sentinel against a file that is there", ""},
		{"nothing at all", "                "},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			status, body := putDeskConfig(t, ts, geminiAssistant, testCase.ifMatch)
			if status != http.StatusConflict {
				t.Fatalf("status %d, want 409: %v", status, body)
			}
			if body["code"] != CodeDeskConfigChanged {
				t.Errorf("code %v, want %q", body["code"], CodeDeskConfigChanged)
			}
			// Both digests, the same discipline the file API follows: the page
			// can show what happened rather than overwrite a change nobody saw.
			if body["actualSha256"] != digestOf([]byte(original)) {
				t.Errorf("actualSha256 %v", body["actualSha256"])
			}
			// **And the file is unchanged, byte for byte.**
			after, err := os.ReadFile(s.deskConfigPath())
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(after) != original {
				t.Errorf("the file was written anyway:\n%s", after)
			}
		})
	}
	// A file the page believes is absent and is: the sentinel accepted, and
	// the positive control for the three refusals above.
	empty, tsEmpty, _ := assistantServer(t)
	if status, body := putDeskConfig(t, tsEmpty, geminiAssistant, ""); status != http.StatusOK {
		t.Fatalf("the sentinel was refused for an absent file: %d %v", status, body)
	}
	_ = empty
}

// watchConfigStaging records every staging file a desk-level write creates.
//
// **"Nothing was written" and "nothing was staged" are different claims**, and
// only the second rules out a refusal that happened after the bytes were
// already on the disk. Round 2 asked for this observable, and it is what makes
// the pre-staging comparison a safeguard a test can break rather than one the
// later comparison quietly stands in for.
func watchConfigStaging(t *testing.T) *[]string {
	t.Helper()
	staged := &[]string{}
	restore := testHookAfterConfigStaged
	testHookAfterConfigStaged = func(path string) { *staged = append(*staged, path) }
	t.Cleanup(func() { testHookAfterConfigStaged = restore })
	return staged
}

func TestDeskConfigWriteRefusesAStaleRequestBeforeStaging(t *testing.T) {
	// **Round 2.** The digest is compared twice — once against the bytes this
	// transaction read and again immediately before the rename — and every
	// ordinary stale-write test was satisfied by the second one alone. So the
	// first had no test of its own, and a mutation that removed it could not
	// fail. What the first one is *for* is that a request already known to be
	// stale never touches the disk: no staging file is created, nothing is
	// written and nothing has to be cleaned up.
	s, ts, _ := assistantServer(t)
	const original = "{\n  \"deskConfigVersion\": 1\n}\n"
	writeDeskConfig(t, s, original)

	staged := watchConfigStaging(t)
	status, body := putDeskConfig(t, ts, geminiAssistant, digestOf([]byte("other bytes")))
	if status != http.StatusConflict || body["code"] != CodeDeskConfigChanged {
		t.Fatalf("status %d, body %v; want 409 %s", status, body, CodeDeskConfigChanged)
	}
	if len(*staged) != 0 {
		t.Errorf("a request that was already stale staged %v", *staged)
	}
	// And the positive control, without which "nothing was staged" is
	// satisfied by a route that stages nothing ever.
	before, _ := deskConfigDigest(t, ts)
	if status, body := putDeskConfig(t, ts, geminiAssistant, before); status != http.StatusOK {
		t.Fatalf("the control write was refused: %d %v", status, body)
	}
	if len(*staged) != 1 {
		t.Fatalf("a write that landed staged %d file(s), want 1", len(*staged))
	}
	// Nothing of it is left behind.
	entries, err := os.ReadDir(s.configDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), configStagingPrefix) {
			t.Errorf("a staging file survived: %s", entry.Name())
		}
	}
}

func TestDeskConfigWriteRefusesWhatTheDecoderWouldRefuse(t *testing.T) {
	// **The round trip is the whole safety argument**, and every one of these
	// is a file the browser refuses whole. Nothing the page sent is written
	// without the composed bytes passing the decoder both sides share.
	for _, testCase := range []struct{ name, assistant, key string }{
		{
			"a key pasted into the endpoint",
			`{"endpoint":{"url":"https://api.example.invalid/","kind":"gemini",` +
				`"model":"m","tools":[],"apiKey":"nope"}}`,
			"assistant.endpoint.apiKey",
		},
		{
			"the vendor's own header name as a member",
			`{"endpoint":{"url":"https://api.example.invalid/","kind":"gemini",` +
				`"model":"m","tools":[],"x-goog-api-key":"nope"}}`,
			"assistant.endpoint.x-goog-api-key",
		},
		{
			"a kind nothing defines",
			`{"endpoint":{"url":"https://api.example.invalid/","kind":"Gemini",` +
				`"model":"m","tools":[]}}`,
			"assistant.endpoint.kind",
		},
		{
			"an absent tool list",
			`{"endpoint":{"url":"https://api.example.invalid/","kind":"gemini","model":"m"}}`,
			"assistant.endpoint.tools",
		},
		{
			"a tool outside the allow-list",
			`{"endpoint":{"url":"https://api.example.invalid/","kind":"gemini",` +
				`"model":"m","tools":["write_file"]}}`,
			"assistant.endpoint.tools",
		},
		{
			"a URL that is not https and not loopback",
			`{"endpoint":{"url":"http://models.example.invalid/","kind":"gemini",` +
				`"model":"m","tools":[]}}`,
			"assistant.endpoint.url",
		},
		{
			"an engine nobody certified",
			`{"endpoint":null,"engine":"something-else"}`,
			"assistant.engine",
		},
		{
			"a member the slot does not declare",
			`{"endpoint":null,"vendor":"someone"}`,
			"assistant.vendor",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			const original = "{\n  \"deskConfigVersion\": 1\n}\n"
			writeDeskConfig(t, s, original)
			before, _ := deskConfigDigest(t, ts)
			status, body := putDeskConfig(t, ts, testCase.assistant, before)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("status %d, want 422: %v", status, body)
			}
			if body["code"] != CodeDeskConfigRefused {
				t.Errorf("code %v, want %q", body["code"], CodeDeskConfigRefused)
			}
			// The decoder's own problems, key by key, so the page can say
			// which member rather than "configuration refused".
			named := false
			problems, _ := body["problems"].([]any)
			for _, problem := range problems {
				entry, _ := problem.(map[string]any)
				if entry["key"] == testCase.key {
					named = true
				}
			}
			if !named {
				t.Errorf("%q is not among the problems: %v", testCase.key, body["problems"])
			}
			// **And nothing was written.**
			after, err := os.ReadFile(s.deskConfigPath())
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(after) != original {
				t.Errorf("the file changed:\n%s", after)
			}
		})
	}
}

// putDeskConfigRaw sends one desk-level write as bytes, for the cases whose
// whole point is what is in the body rather than what it parses to.
func putDeskConfigRaw(t *testing.T, ts *httptest.Server, body []byte) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut,
		ts.URL+"/api/desk-config?token="+testToken, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp.StatusCode, decoded
}

func TestDeskConfigWriteRefusesABodyThatIsNotUTF8(t *testing.T) {
	// **Round 1.** Go's JSON decoder replaces an invalid byte inside a string
	// while decoding, and `json.RawMessage` keeps the original — so a `0xff`
	// in the model name decoded clean, validated clean, and would have been
	// written verbatim into a file every later read then refuses as not UTF-8.
	// The route would have composed a file its own reader will not open.
	s, ts, _ := assistantServer(t)
	const original = "{\n  \"deskConfigVersion\": 1\n}\n"
	writeDeskConfig(t, s, original)
	before, _ := deskConfigDigest(t, ts)

	// The byte is appended rather than written into a literal, so it is
	// unambiguously one byte and not an escape somebody has to read twice.
	body := append([]byte(`{"assistant":{"endpoint":{"url":"https://api.example.invalid/",`+
		`"kind":"gemini","model":"a-`), 0xff)
	body = append(body, []byte(`-model","tools":[]}},"ifMatch":"`+before+`"}`)...)
	if validUTF8(body) {
		t.Fatal("this case has to carry a byte that is not UTF-8")
	}
	status, answered := putDeskConfigRaw(t, ts, body)
	if status != http.StatusUnsupportedMediaType || answered["code"] != CodeNotUTF8 {
		t.Fatalf("status %d, body %v; want 415 %s", status, answered, CodeNotUTF8)
	}
	after, err := os.ReadFile(s.deskConfigPath())
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(after) != original {
		t.Errorf("the file was written anyway:\n%q", after)
	}
	// And what is on disk still reads: the state this refusal exists to keep.
	if _, _, rerr := s.readDeskFile(); rerr != nil {
		t.Errorf("the file no longer reads: %v", rerr)
	}
}

func TestDeskConfigWriteRefusesAnythingAfterTheObject(t *testing.T) {
	// One JSON value, and nothing behind it. A decoder that stops at the first
	// accepts a body two readers disagree about, which is the class this desk
	// refuses everywhere else.
	s, ts, _ := assistantServer(t)
	const original = "{\n  \"deskConfigVersion\": 1\n}\n"
	writeDeskConfig(t, s, original)
	before, _ := deskConfigDigest(t, ts)
	for _, tail := range []string{`{"assistant":null}`, `garbage`, `[]`, `"x"`} {
		body := []byte(`{"assistant":{"endpoint":null},"ifMatch":"` + before + `"} ` + tail)
		status, answered := putDeskConfigRaw(t, ts, body)
		if status != http.StatusBadRequest || answered["code"] != CodeBadRequest {
			t.Errorf("%q: status %d, body %v; want 400 %s",
				tail, status, answered, CodeBadRequest)
		}
	}
	after, err := os.ReadFile(s.deskConfigPath())
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(after) != original {
		t.Errorf("the file was written anyway:\n%s", after)
	}
}

func TestDeskConfigWriteRefusesAComposedFilePastTheReadBound(t *testing.T) {
	// **Round 1.** The request bound allowed an envelope, the composed file
	// was never bounded at all, and the read bound is what every later read
	// applies — so a file could be renamed into place and then refused by
	// every reader, this route's own read-back included. A file written and
	// then unreadable is worse than a write refused.
	s, ts, _ := assistantServer(t)
	// A file just under the read bound, so any addition at all takes the
	// composed one past it.
	filler := strings.Repeat("a", maxFileBytes-200)
	original := "{\n  \"deskConfigVersion\": 1,\n  \"organization\": {\n    \"name\": \"" +
		filler + "\"\n  }\n}\n"
	if len(original) > maxFileBytes {
		t.Fatalf("the fixture is %d bytes, past the read bound", len(original))
	}
	writeDeskConfig(t, s, original)
	before, present := deskConfigDigest(t, ts)
	if !present {
		t.Fatal("the fixture was not read back")
	}
	status, answered := putDeskConfig(t, ts, geminiAssistant, before)
	if status != http.StatusRequestEntityTooLarge || answered["code"] != CodeTooLarge {
		t.Fatalf("status %d, body %v; want 413 %s", status, answered, CodeTooLarge)
	}
	after, err := os.ReadFile(s.deskConfigPath())
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(after) != original {
		t.Errorf("the file was replaced by an oversized one (%d bytes)", len(after))
	}
}

func TestDeskConfigWriteRefusesASymlinkedFile(t *testing.T) {
	// The custody rule, on the way in: `desk.json` names the endpoint a
	// credential is presented to, so a name reached through a symlink is not
	// read — and therefore not written either. A write that treated the read's
	// refusal as absence would replace an out-pointing link with a regular
	// file.
	config := t.TempDir()
	elsewhere := filepath.Join(t.TempDir(), "somewhere.json")
	if err := os.WriteFile(elsewhere, []byte(`{"deskConfigVersion":1}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(config, deskConfigName)); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}
	_, ts, _ := assistantServerIn(t, config)
	status, body := putDeskConfig(t, ts, geminiAssistant, digestOf([]byte(`{"deskConfigVersion":1}`)))
	if status != http.StatusBadRequest || body["code"] != CodeNotAFile {
		t.Fatalf("status %d, body %v; want 400 %s", status, body, CodeNotAFile)
	}
	// The file the link points at is untouched.
	target, err := os.ReadFile(elsewhere)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(target) != `{"deskConfigVersion":1}` {
		t.Errorf("the symlink's target was written through:\n%s", target)
	}
}

func TestDeskConfigWriteRefusesWhereNoKeyCanBeKept(t *testing.T) {
	// A desk whose configuration directory is not safe to keep a credential in
	// keeps no configuration either: the store holds no descriptors at all, so
	// there is nothing to write through.
	_, ts := unusableDesk(t)
	status, body := putDeskConfig(t, ts, geminiAssistant, "")
	if status != http.StatusConflict || body["code"] != CodeAssistantUnusableStore {
		t.Fatalf("status %d, body %v", status, body)
	}
}

func TestDeskConfigWriteIsOwnerOnlyAndAtomic(t *testing.T) {
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, "{\n  \"deskConfigVersion\": 1\n}\n")
	before, _ := deskConfigDigest(t, ts)
	if status, body := putDeskConfig(t, ts, geminiAssistant, before); status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}
	info, err := os.Stat(s.deskConfigPath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode %#o, want 0600: this desk writes its own configuration for itself", got)
	}
	// No staging file left behind, and nothing but the file itself added.
	entries, err := os.ReadDir(s.configDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), configStagingPrefix) {
			t.Errorf("a staging file survived the write: %s", entry.Name())
		}
	}
}

func TestDeskConfigWriteSerialisesConcurrentWriters(t *testing.T) {
	// **Two writers, one digest, and exactly one of them lands.** The
	// compare-and-commit is under the same mutex every other write on this
	// desk takes, so the second finds a file that is no longer the one it read
	// and is refused rather than overwriting the first.
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, "{\n  \"deskConfigVersion\": 1\n}\n")
	before, _ := deskConfigDigest(t, ts)

	type outcome struct {
		status int
		body   map[string]any
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	for _, thinking := range []string{"on", "ultra"} {
		go func(tier string) {
			<-start
			status, body := putDeskConfig(t, ts,
				`{"endpoint":null,"thinking":"`+tier+`"}`, before)
			results <- outcome{status, body}
		}(thinking)
	}
	close(start)
	ok, conflicted := 0, 0
	for range 2 {
		result := <-results
		switch result.status {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflicted++
			if result.body["code"] != CodeDeskConfigChanged {
				t.Errorf("code %v", result.body["code"])
			}
		default:
			t.Errorf("status %d, body %v", result.status, result.body)
		}
	}
	if ok != 1 || conflicted != 1 {
		t.Fatalf("%d wrote and %d were refused; want exactly one of each", ok, conflicted)
	}
	// And what is on disk is one of the two, whole, rather than a blend.
	written, err := os.ReadFile(s.deskConfigPath())
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if decoded := decodeDeskFile(written); decoded.refused() {
		t.Fatalf("the file is not readable after two writers: %v", decoded.Problems)
	}
}

func TestDeskConfigWriteRefusesWithoutTheSessionGuard(t *testing.T) {
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, "{\n  \"deskConfigVersion\": 1\n}\n")
	body := strings.NewReader(`{"assistant":{"endpoint":null},"ifMatch":""}`)
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/desk-config", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
}

func TestDeskConfigWriteRefusesAForeignOrigin(t *testing.T) {
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, "{\n  \"deskConfigVersion\": 1\n}\n")
	req, err := http.NewRequest(http.MethodPut,
		ts.URL+"/api/desk-config?token="+testToken,
		strings.NewReader(`{"assistant":{"endpoint":null},"ifMatch":""}`))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Origin", "http://evil.example.invalid")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403", resp.StatusCode)
	}
}

func TestComposeDeskFileKeepsTheBytesItWasGiven(t *testing.T) {
	// **Numbers are the reason this composes rather than re-serialises.** A
	// decode into `map[string]any` and a re-encode turns `1e2` into `100` and
	// rounds an integer past a float64's precision, so what was checked would
	// not be what was stored.
	current := []byte(`{"deskConfigVersion":1,"panes":{"left":{"width":2e2}},` +
		`"organization":{"name":"acme"}}`)
	composed, problems := composeDeskFile(current, true, json.RawMessage(`{"endpoint":null}`))
	if len(problems) > 0 {
		t.Fatalf("compose: %v", problems)
	}
	if !strings.Contains(string(composed), "2e2") {
		t.Errorf("the number was re-serialised:\n%s", composed)
	}
	if !strings.HasSuffix(string(composed), "}\n") {
		t.Errorf("no trailing newline:\n%q", composed)
	}
	// The order of the file, kept, with the new member appended where there
	// was none.
	order := []string{`"deskConfigVersion"`, `"panes"`, `"organization"`, `"assistant"`}
	at := -1
	for _, name := range order {
		found := strings.Index(string(composed), name)
		if found <= at {
			t.Fatalf("%s is out of order:\n%s", name, composed)
		}
		at = found
	}
	// And the composed bytes are a file this desk reads.
	if decoded := decodeDeskFile(composed); decoded.refused() {
		t.Errorf("the composed file is refused: %v", decoded.Problems)
	}
}

func TestComposeDeskFileRefusesAFileItCannotCarryAcross(t *testing.T) {
	// Not an object, so there are no other members to preserve — and this
	// route will not silently drop what it could not read.
	//
	// **Round 2 added the last two.** The walk stopped as soon as there was no
	// next member, which is true of a truncated object as well as a closed
	// one and says nothing about what follows — so both of these were walked
	// happily and rewritten into well-formed JSON with their malformed or
	// trailing bytes dropped. This route replaces one member; it does not
	// tidy a file up on the way past.
	for _, current := range []string{
		`[1,2,3]`,
		`"a string"`,
		`not json at all`,
		`{"deskConfigVersion":1`,
		`{"deskConfigVersion":1,`,
		`{"deskConfigVersion":1} trailing`,
		`{"deskConfigVersion":1} {"deskConfigVersion":1}`,
	} {
		if _, problems := composeDeskFile([]byte(current), true,
			json.RawMessage(`{"endpoint":null}`)); len(problems) == 0 {
			t.Errorf("%q was composed over", current)
		}
	}
	// And a file that ends in whitespace is not a file with something after
	// it, which is the state every file this desk writes is in.
	for _, current := range []string{
		"{\n  \"deskConfigVersion\": 1\n}\n",
		`{"deskConfigVersion":1}   `,
	} {
		if _, problems := composeDeskFile([]byte(current), true,
			json.RawMessage(`{"endpoint":null}`)); len(problems) != 0 {
			t.Errorf("%q was refused: %v", current, problems)
		}
	}
}

func TestDeskConfigWriteRefusesAFileThatIsNotOneWholeObject(t *testing.T) {
	// Through the route, so the refusal is answered rather than only returned:
	// 422 with the decoder's own whole-file key, and the bytes on disk left
	// exactly as they were rather than repaired into valid JSON.
	for _, current := range []string{
		`{"deskConfigVersion":1`,
		`{"deskConfigVersion":1} trailing`,
	} {
		s, ts, _ := assistantServer(t)
		writeDeskConfig(t, s, current)
		before, present := deskConfigDigest(t, ts)
		if !present {
			t.Fatalf("%q was not read at all", current)
		}
		status, body := putDeskConfig(t, ts, geminiAssistant, before)
		if status != http.StatusUnprocessableEntity || body["code"] != CodeDeskConfigRefused {
			t.Fatalf("%q: status %d, body %v; want 422 %s",
				current, status, body, CodeDeskConfigRefused)
		}
		after, err := os.ReadFile(s.deskConfigPath())
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if string(after) != current {
			t.Errorf("%q was repaired into %q", current, after)
		}
	}
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

/* The key is bound to the destination it was entered for ------------------- */

func TestTheReviewsRetargetSequenceReachesTheAttackerWithNothing(t *testing.T) {
	// **Round 1's exact sequence, in order.** Code holding the session token:
	// reads the digest, PUTs an assistant object naming an endpoint of its
	// own, and then probes or relays — and used to receive the machine-held
	// key at that endpoint. The desk-level write is same-origin, so the origin
	// guard never applied; only the resulting upstream was foreign.
	//
	// The answer is not to take the write away. It is that the key travels
	// only to the destination it was entered for, and entering one is
	// something only a person at the keyboard can do.
	attacker := newUpstream(t, nil)
	s, ts, _ := assistantServer(t)

	// 0. A key, stored for the endpoint the author actually configured.
	authors := newUpstream(t, nil)
	storeKeyBoundTo(t, s, ts, "gemini", authors.server.URL)

	// 1. The digest.
	digest, present := deskConfigDigest(t, ts)
	if !present {
		t.Fatal("no file to read a digest from")
	}

	// 2. An accepted assistant object naming the attacker's endpoint. **This
	// succeeds**, and it is meant to: the write is what the route is for.
	status, body := putDeskConfig(t, ts, fmt.Sprintf(
		`{"endpoint":{"url":%q,"kind":"gemini","model":"m","tools":[]}}`,
		attacker.server.URL), digest)
	if status != http.StatusOK {
		t.Fatalf("the write was refused: %d %v", status, body)
	}
	// And it says so: the endpoint moved, the key did not, and somebody has to
	// enter one for the new destination.
	if body["keyRebindRequired"] != true {
		t.Errorf("keyRebindRequired %v, want true", body["keyRebindRequired"])
	}

	// 3. The probe, and the relay.
	probeCounter := countingProbes(t)
	status, body = postJSON(t, ts, "/api/assistant/probe")
	if status != http.StatusConflict || body["code"] != CodeAssistantKeyUnbound {
		t.Fatalf("probe: status %d, body %v; want 409 %s",
			status, body, CodeAssistantKeyUnbound)
	}
	if calls, to := probeCounter.seen(); calls != 0 {
		t.Fatalf("the probe made %d outbound request(s), to %v", calls, to)
	}

	relayCounter := countingRelays(t)
	resp, raw := relayGet(t, ts, "v1beta/models")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("relay: status %d, want 409: %s", resp.StatusCode, raw)
	}
	if got := codeOfBody(t, raw); got != CodeAssistantKeyUnbound {
		t.Errorf("relay code %q, want %q", got, CodeAssistantKeyUnbound)
	}
	if calls, to := relayCounter.seen(); calls != 0 {
		t.Fatalf("the relay made %d outbound request(s), to %v", calls, to)
	}

	// 4. **The attacker received nothing at all** — not a request without the
	// key, not a request at all.
	if seen := attacker.arrivals(); len(seen) != 0 {
		t.Fatalf("the attacker's endpoint saw %d request(s): %v", len(seen), seen)
	}
	// And no answer this desk gave carries the key either.
	if strings.Contains(raw, testKey) {
		t.Errorf("a refusal carried the key: %s", raw)
	}
}

func TestABindingSurvivesAPathOrQueryChangeAndNotAHostOrKindChange(t *testing.T) {
	// **The line is the origin.** A path or a query is the endpoint's own
	// routing and an author changes one without changing who is at the other
	// end; a host is a different party, and a kind is a different wire — and
	// the credential is presented differently on each.
	for _, testCase := range []struct {
		name, kind, url string
		bound           bool
	}{
		{"the same endpoint", "gemini", "%s", true},
		{"a path added", "gemini", "%s/v1beta", true},
		{"a query added", "gemini", "%s/?route=eu", true},
		{"the host in another case", "gemini", "%s", true},
		{"a different host", "gemini", "https://elsewhere.example.invalid/", false},
		{"a different scheme", "gemini", "https://127.0.0.1/", false},
		{"a different kind", "anthropic", "%s", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			u := newUpstream(t, nil)
			s, ts, _ := assistantServer(t)
			storeKeyBoundTo(t, s, ts, "gemini", u.server.URL)

			target := testCase.url
			if strings.Contains(target, "%s") {
				target = fmt.Sprintf(target, u.server.URL)
			}
			if testCase.name == "the host in another case" {
				target = strings.ToUpper(target[:len("http")]) + target[len("http"):]
			}
			configureEndpoint(t, s, testCase.kind, target)

			counter := countingRelays(t)
			resp, body := relayGet(t, ts, "v1beta/models")
			if testCase.bound {
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("status %d, want 200: %s", resp.StatusCode, body)
				}
				if calls, _ := counter.seen(); calls != 1 {
					t.Fatalf("%d outbound request(s), want 1", calls)
				}
				return
			}
			if resp.StatusCode != http.StatusConflict {
				t.Fatalf("status %d, want 409: %s", resp.StatusCode, body)
			}
			if got := codeOfBody(t, body); got != CodeAssistantKeyUnbound {
				t.Errorf("code %q, want %q", got, CodeAssistantKeyUnbound)
			}
			if calls, to := counter.seen(); calls != 0 {
				t.Fatalf("%d outbound request(s), to %v", calls, to)
			}
		})
	}
}

func TestABindingSurvivesARestart(t *testing.T) {
	// **It is in the file, not in memory.** A binding a restart forgot would
	// be a binding that lapses every time the desk is started, which is the
	// state it exists to prevent.
	config := t.TempDir()
	s, ts, _ := assistantServerIn(t, config)
	storeKeyBoundTo(t, s, ts, "gemini", "https://first.example.invalid/")

	// A second desk over the same directory: a restart, in the only form a
	// test can take it.
	next, nextTS, _ := assistantServerIn(t, config)
	stored, err := next.assistant.readKey()
	if err != nil {
		t.Fatalf("read after restart: %v", err)
	}
	if !stored.present || stored.key != testKey {
		t.Fatalf("the key did not survive: %+v", stored.present)
	}
	if stored.origin != "https://first.example.invalid" || stored.kind != "gemini" {
		t.Errorf("binding %s over %q", stored.origin, stored.kind)
	}
	// And the endpoint the second desk is asked about is still checked against
	// it.
	configureEndpoint(t, next, "gemini", "https://second.example.invalid/")
	counter := countingProbes(t)
	status, body := postJSON(t, nextTS, "/api/assistant/probe")
	if status != http.StatusConflict || body["code"] != CodeAssistantKeyUnbound {
		t.Fatalf("status %d, body %v", status, body)
	}
	if calls, to := counter.seen(); calls != 0 {
		t.Fatalf("%d outbound request(s), to %v", calls, to)
	}
}

func TestAnUnversionedKeyFileIsRefusedRatherThanRead(t *testing.T) {
	// A file this build cannot read as a *bound* key is not read as an unbound
	// one: a credential with no binding is exactly the state this mechanism
	// ends. The repair is one action, and the sentence names it.
	config := t.TempDir()
	s, ts, _ := assistantServerIn(t, config)
	configureAnEndpoint(t, s)
	if err := os.MkdirAll(s.secretsDir(), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, contents := range []string{
		testKey,                               // the format this replaces
		`{"key":"` + testKey + `"}`,           // JSON, and unversioned
		`{"assistantKeyVersion":2,"key":"x"}`, // a version this build does not read
	} {
		if err := os.WriteFile(s.assistantKeyPath(), []byte(contents), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		status, body := sendJSON(t, ts, http.MethodGet, "/api/assistant/key", nil)
		if status != http.StatusConflict || body["code"] != CodeAssistantKeyUnbound {
			t.Fatalf("%q: status %d, body %v", contents, status, body)
		}
		message, _ := body["error"].(string)
		if !strings.Contains(message, "store the key again") {
			t.Errorf("%q: the refusal does not name the repair: %q", contents, message)
		}
		if strings.Contains(message, testKey) {
			t.Errorf("the refusal carries the key: %q", message)
		}
		// And nothing reaches an endpoint on the strength of it.
		counter := countingProbes(t)
		if status, body := postJSON(t, ts, "/api/assistant/probe"); status != http.StatusConflict ||
			body["code"] != CodeAssistantKeyUnbound {
			t.Errorf("%q: probe status %d, body %v", contents, status, body)
		}
		if calls, to := counter.seen(); calls != 0 {
			t.Errorf("%q: %d outbound request(s), to %v", contents, calls, to)
		}
	}
}

func TestStoringAKeyNeedsAnEndpointToBindItTo(t *testing.T) {
	// A key with nothing to bind to would be a key bound to whatever is
	// configured next, which is the arrangement the binding replaces. The
	// state and the repair are the ones Admin already renders.
	_, ts, _ := assistantServer(t)
	status, body := storeKey(t, ts, testKey)
	if status != http.StatusConflict || body["code"] != CodeAssistantUnconfigured {
		t.Fatalf("status %d, body %v; want 409 %s", status, body, CodeAssistantUnconfigured)
	}
	// And a refused store keeps none of it: the read still says there is no key.
	if status, body := sendJSON(t, ts, http.MethodGet, "/api/assistant/key", nil); status !=
		http.StatusOK || body["present"] != false {
		t.Fatalf("read: status %d, body %v", status, body)
	}
}

func TestTheKeyReadReportsWhereTheKeyGoes(t *testing.T) {
	// The page needs it to say "key stored for <host>", which is the whole
	// user-facing half of the binding: a reader who can see the destination
	// can see that changing it means entering the key again. Neither member is
	// a secret — both are in the file the page already reads.
	s, ts, _ := assistantServer(t)
	storeKeyBoundTo(t, s, ts, "gemini", "https://gw.example.invalid/v1?route=eu")
	status, body := sendJSON(t, ts, http.MethodGet, "/api/assistant/key", nil)
	if status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}
	if body["present"] != true {
		t.Fatalf("present %v", body["present"])
	}
	// The origin, and neither the path nor the query: a binding is about who
	// is at the other end.
	if body["origin"] != "https://gw.example.invalid" {
		t.Errorf("origin %v", body["origin"])
	}
	if body["kind"] != "gemini" {
		t.Errorf("kind %v", body["kind"])
	}
	// And still not the key.
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), testKey) {
		t.Errorf("the key reached the page: %s", raw)
	}
}

func TestAWriteThatKeepsTheDestinationKeepsTheKey(t *testing.T) {
	// The positive control for `keyRebindRequired`: a write that changes the
	// model, the tools or the tier leaves the credential exactly where it was,
	// and the page is told so rather than asked to enter it again.
	u := newUpstream(t, nil)
	s, ts, _ := assistantServer(t)
	storeKeyBoundTo(t, s, ts, "gemini", u.server.URL)
	digest, _ := deskConfigDigest(t, ts)
	status, body := putDeskConfig(t, ts, fmt.Sprintf(
		`{"endpoint":{"url":%q,"kind":"gemini","model":"another-model",`+
			`"tools":["validate"]},"thinking":"ultra"}`, u.server.URL+"/v1beta"), digest)
	if status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}
	if body["keyRebindRequired"] != false {
		t.Errorf("keyRebindRequired %v, want false", body["keyRebindRequired"])
	}
	// And the relay still carries it.
	if resp, raw := relayGet(t, ts, "models"); resp.StatusCode != http.StatusOK {
		t.Fatalf("the relay refused after a same-destination write: %d %s",
			resp.StatusCode, raw)
	}
	if seen := u.only(t); seen.header.Get("x-goog-api-key") != testKey {
		t.Errorf("the endpoint did not receive the bound key: %v", seen.header)
	}
}

/* The endpoint the file names ---------------------------------------------- */

func TestConfiguredEndpointRefusals(t *testing.T) {
	s, ts, _ := assistantServer(t)
	// A key stored against an endpoint this desk accepts, before each refused
	// file is written over it — so every refusal below is the file's.
	storeKeyBoundTo(t, s, ts, defaultTestKind, defaultTestEndpoint)

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
			`{"deskConfigVersion":1,"assistant":{"endpoint":{"url":"https://e.example/v1","kind":"some-other-protocol",` +
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

func TestTheToolAllowListIsTheFiveReadOnlyOnes(t *testing.T) {
	// Held here as a declaration a reader can check against the page's own
	// list, which `assistant/enforcement.test.ts` reads out of this file.
	// `list_examples` is on it because the runtime's own `author_pack` prompt
	// tells the model to call it: a list without it grants a capability the
	// prompt then asks for and cannot have.
	want := []string{
		"get_schema", "list_examples", "get_example", "validate", "experimental_evaluate",
	}
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
	// raw is the whole request target as it arrived — path, query and all.
	//
	// Recorded because a credential in a URL is the thing the gemini arm must
	// not do, and a probe that put the key in `?key=` would leave `path` and
	// `headers` looking exactly as they do now. What that claim is about is
	// the bytes of the request line.
	raw string
}

func newStubEndpoint(t *testing.T, answer func(w http.ResponseWriter)) *stubEndpoint {
	t.Helper()
	stub := &stubEndpoint{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		stub.mu.Lock()
		stub.method, stub.path, stub.headers, stub.body = r.Method, r.URL.Path, r.Header.Clone(), string(body)
		stub.raw = r.URL.RequestURI()
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

// requestLine is the target the endpoint was asked for, query included.
func (s *stubEndpoint) requestLine() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.raw
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

func TestProbeSpeaksTheGeminiProtocol(t *testing.T) {
	// The native Gemini wire's own smallest legitimate request: the model
	// listing, bounded to one entry. A `GET`, which creates nothing, and one
	// this listing answers 401 to without a credential — so it exercises the
	// key rather than merely the route.
	stub := newStubEndpoint(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"models/a-model"}]}`))
	})

	status, body := probeAgainst(t, "gemini", stub.server.URL)
	if status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}
	if body["reachable"] != true {
		t.Errorf("reachable %v, want true (%v)", body["reachable"], body)
	}
	if body["diagnostic"] != "" {
		t.Errorf("diagnostic %q, want empty on a success", body["diagnostic"])
	}

	method, path, headers, sent := stub.saw()
	if method != http.MethodGet || path != "/v1beta/models" {
		t.Errorf("the probe sent %s %s, want GET /v1beta/models", method, path)
	}
	if sent != "" {
		t.Errorf("the probe sent a body: %q", sent)
	}
	// This protocol's own header, and neither of the other two protocols'.
	if got := headers.Values("x-goog-api-key"); len(got) != 1 || got[0] != testKey {
		t.Errorf("x-goog-api-key = %v, want exactly one %q", got, testKey)
	}
	if headers.Get("Authorization") != "" || headers.Get("x-api-key") != "" {
		t.Errorf("the gemini probe sent another protocol's credential header: %v", headers)
	}
	// **The key is not in the URL**, which is the one thing this protocol's
	// own documentation offers and this desk declines. Asserted on the request
	// line rather than on a parsed query, because what is being claimed is
	// about the bytes.
	line := stub.requestLine()
	if line != "/v1beta/models?pageSize=1" {
		t.Errorf("request line %q, want /v1beta/models?pageSize=1", line)
	}
	if strings.Contains(line, testKey) || strings.Contains(line, "key=") {
		t.Errorf("the key travelled in the URL: %q", line)
	}
}

func TestProbeKeepsAConfiguredQueryBeforeTheGeminiPageSize(t *testing.T) {
	// The configured query is the endpoint's own routing and keeps its place;
	// the one parameter this desk adds goes after it. The same order the relay
	// uses, and the reason it is one function.
	stub := newStubEndpoint(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"models":[]}`))
	})
	status, body := probeAgainst(t, "gemini", stub.server.URL+"/?route=eu")
	if status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}
	if got := stub.requestLine(); got != "/v1beta/models?route=eu&pageSize=1" {
		t.Errorf("request line %q, want the configured query first", got)
	}
	// **And the key is not in it**, with a configured query present — which is
	// the case the earlier version of this assertion did not cover: it tested
	// a query-free base only, so a configured `?key=…` would have gone
	// unnoticed. That spelling is refused at decode now, and this is the
	// assertion that the accepted spelling still carries no credential.
	if line := stub.requestLine(); strings.Contains(line, testKey) ||
		strings.Contains(line, "key=") {
		t.Errorf("the key travelled in the URL: %q", line)
	}
}

func TestAConfiguredQueryCarryingACredentialIsNeverProbed(t *testing.T) {
	// The other half: a configured query nobody should have written refuses
	// the whole file, so the probe has no endpoint to reach and makes no
	// request at all — the same gate a refused file has always had.
	counter := countingProbes(t)
	s, ts, _ := assistantServer(t)
	// The key is stored against the same endpoint without the query, so the
	// refusal below is the query's.
	storeKeyBoundTo(t, s, ts, "gemini", "https://gw.example.invalid/v1")
	writeDeskConfig(t, s, `{"deskConfigVersion":1,"assistant":{"endpoint":`+
		`{"url":"https://gw.example.invalid/v1?key=sk-nope","kind":"gemini",`+
		`"model":"m","tools":[]}}}`)
	status, body := postJSON(t, ts, "/api/assistant/probe")
	if status != http.StatusConflict || body["code"] != CodeAssistantUnconfigured {
		t.Fatalf("status %d, body %v", status, body)
	}
	if calls, to := counter.seen(); calls != 0 {
		t.Fatalf("a refused configuration made %d outbound request(s), to %v", calls, to)
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
			// Read with a checked assertion. A bare `.(string)` panicked when
			// a mutation made the probe refuse instead of answer — and a
			// panicking test is not a failing test: the harness reports it as
			// INCONCLUSIVE, so a mutation that *was* caught looked untested.
			got, ok := body["diagnostic"].(string)
			if !ok {
				t.Fatalf("no diagnostic in the answer: %v", body)
			}
			if got != tc.want {
				t.Errorf("diagnostic %q, want %q", got, tc.want)
			}
			if !contains(AssistantDiagnostics, got) {
				t.Errorf("%q is not in the declared vocabulary", got)
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

func TestProbeKeepsAConfiguredQueryStringAndAppendsToThePath(t *testing.T) {
	// **`base + "/models"` is wrong the moment a query string is allowed**, and
	// a query string is allowed because some gateways route on one. Appending
	// to the string sent `GET /v1?route=eu/models` — a request to a resource
	// nobody named — and the live drive is what caught it.
	stub := newStubEndpoint(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	})
	_, body := probeAgainst(t, "openai-compatible", stub.server.URL+"/v1?route=eu&tier=2")
	if body["reachable"] != true {
		t.Fatalf("body %v", body)
	}
	method, path, _, _ := stub.saw()
	if method != http.MethodGet || path != "/v1/models" {
		t.Errorf("the probe sent %s %s, want GET /v1/models", method, path)
	}
}

func TestAProbeAddressAppendsToThePathAndKeepsTheQuery(t *testing.T) {
	for _, tc := range []struct{ base, suffix, want string }{
		{"https://gw.example/v1", "/models", "https://gw.example/v1/models"},
		{"https://gw.example/v1?route=eu", "/models", "https://gw.example/v1/models?route=eu"},
		{"https://gw.example/v1/", "/models", "https://gw.example/v1/models"},
		{"https://gw.example", "/v1/messages", "https://gw.example/v1/messages"},
	} {
		if got := probeAddress(tc.base, tc.suffix); got != tc.want {
			t.Errorf("probeAddress(%q, %q) = %q, want %q", tc.base, tc.suffix, got, tc.want)
		}
	}
}

func TestAProbeAddressKeepsAnEscapedPathEscaped(t *testing.T) {
	// **`u.Path` is the decoded path**, and writing to it alone leaves
	// `RawPath` describing something else — which `String` resolves by
	// re-encoding from the decoded form. For `/tenant%2Fone` that turns one
	// segment into two and sends the credential to a different resource than
	// the one configured.
	for _, tc := range []struct{ base, suffix, want string }{
		{"https://gw.example/tenant%2Fone", "/models", "https://gw.example/tenant%2Fone/models"},
		{"https://gw.example/a%2eb", "/models", "https://gw.example/a%2eb/models"},
		{"https://gw.example/t%2Fone?route=eu", "/models",
			"https://gw.example/t%2Fone/models?route=eu"},
		{"https://gw.example/tenant%20one", "/models",
			"https://gw.example/tenant%20one/models"},
		// And the ordinary cases still hold.
		{"https://gw.example/v1", "/models", "https://gw.example/v1/models"},
		{"https://gw.example/v1/", "/models", "https://gw.example/v1/models"},
		{"https://gw.example", "/v1/messages", "https://gw.example/v1/messages"},
	} {
		if got := probeAddress(tc.base, tc.suffix); got != tc.want {
			t.Errorf("probeAddress(%q, %q) = %q, want %q", tc.base, tc.suffix, got, tc.want)
		}
	}
}

func TestAnEscapedPathSurvivesDecodingAndTheProbe(t *testing.T) {
	// End to end: what the endpoint is configured with is what it receives.
	// `httptest` reports `RequestURI` unparsed, which is the only place the
	// escaping is still visible.
	var seen string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.RequestURI
		w.WriteHeader(http.StatusOK)
	}))
	defer stub.Close()

	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, fmt.Sprintf(
		`{"deskConfigVersion":1,"assistant":{"endpoint":{"url":%q,"kind":"openai-compatible",`+
			`"model":"a-model","tools":[]}}}`, stub.URL+"/tenant%2Fone"))
	if status, _ := storeKey(t, ts, testKey); status != http.StatusOK {
		t.Fatal("store")
	}
	if status, body := postJSON(t, ts, "/api/assistant/probe"); status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}
	if seen != "/tenant%2Fone/models" {
		t.Errorf("the endpoint received %q, want the configured escaping intact", seen)
	}
}

// countedBody is a response body that records what was actually read from it.
//
// **The server's accepted writes are not evidence.** The test this replaced
// asserted that the endpoint's `Write` returned 64 KiB, which a socket buffer
// can accept in full while the client reads eight and closes — so it proved
// nothing about the drain it was named for. This is on the *client* side of
// the transport, where "read to the end" is a fact that can be observed.
type countedBody struct {
	reader io.Reader
	read   int
	eof    bool
}

func (b *countedBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read += n
	if errors.Is(err, io.EOF) {
		b.eof = true
	}
	return n, err
}

func (b *countedBody) Close() error { return nil }

// bodyTransport answers every request with a body of the given size and keeps
// the counter, so a test can ask how much of it was consumed.
type bodyTransport struct {
	size int
	body *countedBody
}

func (t *bodyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.body = &countedBody{reader: bytes.NewReader(bytes.Repeat([]byte("y"), t.size))}
	return &http.Response{
		StatusCode:    http.StatusInternalServerError,
		Status:        "500 Internal Server Error",
		Body:          t.body,
		Header:        make(http.Header),
		ContentLength: int64(t.size),
		Request:       r,
	}, nil
}

func TestTheProbeDrainsTheWholeBody(t *testing.T) {
	// The README says the body is drained; it used to stop at 8 KiB, which is
	// a different thing and leaves the connection unreusable. Measured where
	// the reading happens rather than where the writing does.
	const size = 64 << 10
	transport := &bodyTransport{size: size}
	restore := probeClient.Transport
	probeClient.Transport = transport
	t.Cleanup(func() { probeClient.Transport = restore })

	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, `{"deskConfigVersion":1,"assistant":{"endpoint":{`+
		`"url":"https://gw.example/v1","kind":"openai-compatible",`+
		`"model":"a-model","tools":[]}}}`)
	if status, _ := storeKey(t, ts, testKey); status != http.StatusOK {
		t.Fatal("store")
	}
	status, body := postJSON(t, ts, "/api/assistant/probe")
	if status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}

	if transport.body == nil {
		t.Fatal("the probe made no request")
	}
	if transport.body.read != size {
		t.Errorf("the probe read %d of %d bytes; the drain gave up early",
			transport.body.read, size)
	}
	if !transport.body.eof {
		t.Error("the probe never saw the end of the body")
	}
	// And none of it travelled, however long it was.
	if body["diagnostic"] != DiagnosticUnexpected {
		t.Errorf("diagnostic %v", body["diagnostic"])
	}
}
