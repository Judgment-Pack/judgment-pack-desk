package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The credentials an AI connection stores are held the way this desk's one
// key always was (docs/ai-connections.md, "Storage and migration"): owner-only
// files in an owner-only folder under Desk's configuration directory, never in
// the project, never on the page or in the log, and read back and held to what
// was written before a save is reported. The registry that names where each
// one goes is held to the same rule.

// connectionKeyPath is where a connection's key lives, spelled out here rather
// than read from `connectionKeyName`, so that moving it is a change to a test.
func connectionKeyPath(s *Server, id string) string {
	return filepath.Join(s.secretsDir(), "assistant-"+id)
}

func TestAIConnectionKeyIsKeptAsTheDesksKeyIs(t *testing.T) {
	upstream := newUpstream(t, nil)
	s, ts, logged := assistantServer(t)
	registry := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Work", upstream.server.URL)}})
	c := registry.Connections[0]
	headers := map[string]string{aiConnectionHeader: c.ID, aiRevisionHeader: c.Revision}
	code, raw := aiTestRequest(t, ts, http.MethodPut, "/api/assistant/key", map[string]string{"key": testKey}, headers)
	if code != http.StatusOK || bytes.Contains(raw, []byte(testKey)) {
		t.Fatalf("store answered %d %s", code, raw)
	}
	code, raw = aiTestRequest(t, ts, http.MethodGet, "/api/assistant/key", nil, headers)
	if code != http.StatusOK || !bytes.Contains(raw, []byte(`"bound":true`)) || bytes.Contains(raw, []byte(testKey)) {
		t.Fatalf("read answered %d %s", code, raw)
	}

	if dir, err := os.Stat(s.secretsDir()); err != nil {
		t.Fatalf("the secrets folder: %v", err)
	} else if dir.Mode().Perm() != 0o700 {
		t.Fatalf("the secrets folder is %v, want 0700", dir.Mode().Perm())
	}
	if file, err := os.Lstat(connectionKeyPath(s, c.ID)); err != nil {
		t.Fatalf("the connection's key file: %v", err)
	} else if !file.Mode().IsRegular() || file.Mode().Perm() != 0o600 {
		t.Fatalf("the connection's key file is %v, want a regular 0600 file", file.Mode())
	}
	stored, err := os.ReadFile(connectionKeyPath(s, c.ID))
	if err != nil {
		t.Fatal(err)
	}
	origin, _ := endpointOrigin(upstream.server.URL)
	if got := onDiskKey(t, stored); got.Key != testKey || got.Origin != origin || got.Kind != "openai-compatible" {
		t.Fatalf("the key file holds %s over %q", got.Origin, got.Kind)
	}
	// One connection's key is not the desk's own key.
	if _, err := os.Lstat(s.assistantKeyPath()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the connection's key landed on the desk's own key: %v", err)
	}
	if registryFile, err := os.Lstat(filepath.Join(s.configDir, aiConnectionsFile)); err != nil {
		t.Fatalf("the registry: %v", err)
	} else if registryFile.Mode().Perm() != 0o600 {
		t.Fatalf("the registry is %v, want 0600", registryFile.Mode().Perm())
	}
	if registryBytes, _ := os.ReadFile(filepath.Join(s.configDir, aiConnectionsFile)); bytes.Contains(registryBytes, []byte(testKey)) {
		t.Fatal("the key is in the registry")
	}
	// Nothing in the project holds it.
	_ = filepath.WalkDir(s.projectDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if data, _ := os.ReadFile(path); bytes.Contains(data, []byte(testKey)) {
			t.Errorf("the key is in the project, at %s", entry.Name())
		}
		return nil
	})
	if strings.Contains(logged.String(), testKey) {
		t.Fatal("the key is in the log")
	}
}

// What the store reads back is what the next relayed request presents, so a
// file that reads back as anything other than what was written is not
// reported as stored — for a connection's key and for the desk's own.
func TestAKeyThatReadsBackOtherwiseIsNotReportedStored(t *testing.T) {
	const other = "sk-another-key-put-down-in-its-place"
	t.Run("a connection's key", func(t *testing.T) {
		upstream := newUpstream(t, nil)
		s, ts, _ := assistantServer(t)
		registry := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Work", upstream.server.URL)}})
		c := registry.Connections[0]
		origin, _ := endpointOrigin(upstream.server.URL)
		testHookAfterKeyStored = func(name string) {
			if err := s.assistant.storeKeyNamed(name, storedKey{present: true, key: other, origin: origin, kind: "openai-compatible"}); err != nil {
				t.Errorf("the other key: %v", err)
			}
		}
		t.Cleanup(func() { testHookAfterKeyStored = nil })
		code, raw := aiTestRequest(t, ts, http.MethodPut, "/api/assistant/key", map[string]string{"key": testKey}, map[string]string{aiConnectionHeader: c.ID, aiRevisionHeader: c.Revision})
		if code != http.StatusInternalServerError || bytes.Contains(raw, []byte(`"bound"`)) || bytes.Contains(raw, []byte(testKey)) || bytes.Contains(raw, []byte(other)) {
			t.Fatalf("a key that read back otherwise answered %d %s", code, raw)
		}
	})
	t.Run("the desk's own key", func(t *testing.T) {
		s, ts, _ := assistantServer(t)
		configureAnEndpoint(t, s)
		testHookAfterKeyStored = func(name string) {
			if err := s.assistant.storeKeyNamed(name, storedKey{present: true, key: other, origin: defaultTestOrigin, kind: defaultTestKind}); err != nil {
				t.Errorf("the other key: %v", err)
			}
		}
		t.Cleanup(func() { testHookAfterKeyStored = nil })
		code, body := storeKey(t, ts, testKey)
		if code != http.StatusInternalServerError || body["bound"] != nil || strings.Contains(fmt.Sprint(body), testKey) {
			t.Fatalf("a key that read back otherwise answered %d %v", code, body)
		}
	})
}

// A registry write is reported only as the bytes it wrote: a file that changed
// between the rename and the read that answers the page is not this save.
func TestARegistryThatReadsBackOtherwiseIsNotReportedSaved(t *testing.T) {
	s, ts, _ := assistantServer(t)
	before, err := s.readAIRegistry()
	if err != nil {
		t.Fatal(err)
	}
	other, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIBeta, Connections: []aiConnection{aiTestConnection(testAIBeta, "Other", "https://other.example.invalid")}})
	if err != nil {
		t.Fatal(err)
	}
	testHookAfterAIRegistryWrite = func(path string) {
		if err := os.WriteFile(path, other, 0o600); err != nil {
			t.Errorf("the other registry: %v", err)
		}
	}
	t.Cleanup(func() { testHookAfterAIRegistryWrite = nil })
	mine := aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Mine", "https://mine.example.invalid")}}
	code, raw := aiTestRequest(t, ts, http.MethodPut, "/api/ai-connections", mine, map[string]string{"If-Match": before.SHA256})
	if code != http.StatusConflict || bytes.Contains(raw, []byte(testAIAlpha)) || bytes.Contains(raw, []byte(testAIBeta)) {
		t.Fatalf("a registry that read back otherwise answered %d %s", code, raw)
	}
}

// The compare the write was admitted on is made again at the rename, so an
// ordinary editor's write that lands in between is kept, and the save is
// refused rather than laid over it.
func TestTheRegistryCompareHoldsUntilTheRename(t *testing.T) {
	s, ts, _ := assistantServer(t)
	before, err := s.readAIRegistry()
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIBeta, Connections: []aiConnection{aiTestConnection(testAIBeta, "Theirs", "https://theirs.example.invalid")}})
	if err != nil {
		t.Fatal(err)
	}
	testHookBeforeConfigRename = func(path string) {
		if filepath.Base(path) == aiConnectionsFile {
			if err := os.WriteFile(path, theirs, 0o600); err != nil {
				t.Errorf("their registry: %v", err)
			}
		}
	}
	t.Cleanup(func() { testHookBeforeConfigRename = nil })
	mine := aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Mine", "https://mine.example.invalid")}}
	if code, raw := aiTestRequest(t, ts, http.MethodPut, "/api/ai-connections", mine, map[string]string{"If-Match": before.SHA256}); code != http.StatusConflict {
		t.Fatalf("a save over a changed registry answered %d %s", code, raw)
	}
	if now, _ := os.ReadFile(filepath.Join(s.configDir, aiConnectionsFile)); !bytes.Equal(now, theirs) {
		t.Fatal("their registry was overwritten")
	}
}

// A stale save is refused at the read, before anything is put down: nothing
// is staged, and the newer registry is left as it is. (The compare at the
// rename, below, would refuse it too, but only after staging a file.)
func TestAStaleRegistrySaveStagesNothing(t *testing.T) {
	s, ts, _ := assistantServer(t)
	first := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "First", "https://api.example.invalid")}})
	renamed := first.aiRegistry
	renamed.Connections[0].Name = "Renamed"
	saveTestAIRegistry(t, s, ts, renamed)
	newer, _ := os.ReadFile(filepath.Join(s.configDir, aiConnectionsFile))
	staged := 0
	testHookAfterConfigStaged = func(string) { staged++ }
	t.Cleanup(func() { testHookAfterConfigStaged = nil })
	if code, raw := aiTestRequest(t, ts, http.MethodPut, "/api/ai-connections", first.aiRegistry, map[string]string{"If-Match": first.SHA256}); code != http.StatusConflict {
		t.Fatalf("a stale save answered %d %s", code, raw)
	}
	if staged != 0 {
		t.Fatalf("a stale save staged %d files", staged)
	}
	if now, _ := os.ReadFile(filepath.Join(s.configDir, aiConnectionsFile)); !bytes.Equal(now, newer) {
		t.Fatal("the newer registry was replaced")
	}
}

// A connection's method is its identity: an API connection does not become a
// ChatGPT one, or the reverse, under the same id and its stored credential.
func TestAConnectionsMethodIsNotEditable(t *testing.T) {
	s, ts, _ := assistantServer(t)
	saved := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Work", "https://api.example.invalid")}})
	before, _ := os.ReadFile(filepath.Join(s.configDir, aiConnectionsFile))
	changed := saved.aiRegistry
	model := "shared-model"
	changed.Connections[0].Assistant = AssistantSlotView{Engine: "codex", Thinking: "off", Agent: &AssistantAgentConfig{Provider: "openai", AuthMethod: "subscription", Model: &model, Models: []string{model}, Tools: []string{}}}
	if code, raw := aiTestRequest(t, ts, http.MethodPut, "/api/ai-connections", changed, map[string]string{"If-Match": saved.SHA256}); code != http.StatusUnprocessableEntity {
		t.Fatalf("a changed method answered %d %s", code, raw)
	}
	if after, _ := os.ReadFile(filepath.Join(s.configDir, aiConnectionsFile)); !bytes.Equal(before, after) {
		t.Fatal("the registry changed")
	}
}

// The registry names where a credential is sent, so a connection's settings
// pass the desk-level file's own reader, credential scan included: an address
// that carries a credential, or any setting that reader refuses, is refused,
// and nothing is written.
func TestARegistryConnectionCarryingACredentialIsRefused(t *testing.T) {
	for name, change := range map[string]func(*aiConnection){
		"a credential in the address's user": func(c *aiConnection) {
			c.Assistant.Endpoint.URL = "https://user:registry-secret-0123@api.example.invalid/v1"
		},
		"a credential in the address's query": func(c *aiConnection) {
			c.Assistant.Endpoint.URL = "https://api.example.invalid/v1?api_key=registry-secret-0123"
		},
		// A ChatGPT connection the reader refuses still decodes to an agent and
		// no endpoint, the shape such a connection has, so only the reader's
		// own verdict refuses it.
		"a ChatGPT connection with a tier the reader does not know": func(c *aiConnection) {
			model := "shared-model"
			c.Assistant = AssistantSlotView{Engine: "codex", Thinking: "registry-secret-0123", Agent: &AssistantAgentConfig{Provider: "openai", AuthMethod: "subscription", Model: &model, Models: []string{model}, Tools: []string{}}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			before, err := s.readAIRegistry()
			if err != nil {
				t.Fatal(err)
			}
			c := aiTestConnection(testAIAlpha, "Work", "https://api.example.invalid/v1")
			change(&c)
			value := aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{c}}
			code, raw := aiTestRequest(t, ts, http.MethodPut, "/api/ai-connections", value, map[string]string{"If-Match": before.SHA256})
			if code != http.StatusUnprocessableEntity || bytes.Contains(raw, []byte("registry-secret-0123")) {
				t.Fatalf("a connection the reader refuses answered %d %s", code, raw)
			}
			if _, err := os.Lstat(filepath.Join(s.configDir, aiConnectionsFile)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("a refused registry was written: %v", err)
			}
		})
	}
}

// registryOfSize is one API connection whose enabled models make the registry
// exactly `size` bytes once written. Each id is long so that the set stays a
// thousand or so entries.
func registryOfSize(t *testing.T, size int) aiRegistry {
	t.Helper()
	value := aiTestConnection(testAIAlpha, "Work", "https://api.example.invalid")
	// The default is a member of the set, and the set's first member is never
	// the one trimmed to size.
	models := []string{"shared-model"}
	for i := 0; ; i++ {
		models = append(models, fmt.Sprintf("model-%05d-%s", i, strings.Repeat("x", 1000)))
		value.Assistant.Endpoint.Models = models
		registry := aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{value}}
		written, _ := json2(registry)
		if len(written) >= size {
			last := models[len(models)-1]
			models[len(models)-1] = last[:len(last)-(len(written)-size)]
			value.Assistant.Endpoint.Models = models
			registry.Connections = []aiConnection{value}
			if written, _ := json2(registry); len(written) != size {
				t.Fatalf("built %d bytes, want %d", len(written), size)
			}
			return registry
		}
	}
}

// json2 is the registry's written form with no bound applied, which is what
// the bound is measured against; the test at the bound holds it to
// encodeAIRegistry's own.
func json2(value aiRegistry) ([]byte, error) {
	written, err := json.MarshalIndent(value, "", "  ")
	return append(written, '\n'), err
}

// Whatever the reader refuses, the writer refuses before writing: the bytes
// written are indented, and can be longer than the request that asked for
// them. At the bound the registry is written and read back; one byte over, it
// is refused and nothing is written.
func TestTheRegistryWriterRefusesWhatItsReaderRefuses(t *testing.T) {
	at := registryOfSize(t, maxAIRegistryBytes)
	written, err := encodeAIRegistry(at)
	if err != nil || len(written) != maxAIRegistryBytes {
		t.Fatalf("a registry at the bound was refused: %d bytes, %v", len(written), err)
	}
	if _, err := decodeAIRegistry(written); err != nil {
		t.Fatalf("the reader refused what the writer wrote at the bound: %v", err)
	}
	over := registryOfSize(t, maxAIRegistryBytes+1)
	if _, err := encodeAIRegistry(over); err == nil {
		t.Fatal("a registry over the bound was encoded for writing")
	}

	s, ts, _ := assistantServer(t)
	before, err := s.readAIRegistry()
	if err != nil {
		t.Fatal(err)
	}
	code, raw := aiTestRequest(t, ts, http.MethodPut, "/api/ai-connections", over, map[string]string{"If-Match": before.SHA256})
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("a registry over the bound once written answered %d %.200s", code, raw)
	}
	if _, err := os.Lstat(filepath.Join(s.configDir, aiConnectionsFile)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a registry over the bound was written: %v", err)
	}
}

// The desk's model preferences are a project file, read at most 64 KiB, and
// the page refuses to write more (assistantProfile.ts). At the bound the file
// is read; one byte over, it is a problem and never an inherited profile.
func TestTheAssistantProfileIsReadToItsBoundAndNoFurther(t *testing.T) {
	s, _, _ := assistantServer(t)
	head := `{"profileVersion":1,"api":{"inherit":false,"models":["only"],"model":"only"}}`
	at := head + strings.Repeat(" ", 65536-len(head))
	for _, tc := range []struct {
		body string
		read bool
	}{{at, true}, {at + " ", false}} {
		if err := os.WriteFile(filepath.Join(s.projectDir, assistantProfilePath), []byte(tc.body), 0o600); err != nil {
			t.Fatal(err)
		}
		profile, err := s.readAssistantProfile()
		if tc.read && (err != nil || profile.API == nil || profile.API.Inherit) {
			t.Fatalf("a profile of %d bytes was not read: %v", len(tc.body), err)
		}
		if !tc.read && (err == nil || profile != nil) {
			t.Fatalf("a profile of %d bytes was read as %+v", len(tc.body), profile)
		}
		// The decoder holds the same bound on its own, for bytes that reach it
		// some other way than this read.
		if _, err := decodeAssistantProfile([]byte(tc.body)); (err == nil) != tc.read {
			t.Fatalf("the decoder took a profile of %d bytes as %v", len(tc.body), err)
		}
	}
}

// A connection's id names its key file and its account folder, so it is one
// of this desk's own shapes and nothing else, in the registry and on a request.
func TestARegistryConnectionIdIsOneOfItsOwn(t *testing.T) {
	s, ts, _ := assistantServer(t)
	for _, id := range []string{"../evil", "ai-XYZ", "legacy-other", "ai-" + strings.Repeat("a", 25)} {
		before, err := s.readAIRegistry()
		if err != nil {
			t.Fatal(err)
		}
		value := aiRegistry{Version: 1, DefaultConnection: id, Connections: []aiConnection{aiTestConnection(id, "Work", "https://api.example.invalid")}}
		if code, raw := aiTestRequest(t, ts, http.MethodPut, "/api/ai-connections", value, map[string]string{"If-Match": before.SHA256}); code != http.StatusUnprocessableEntity {
			t.Fatalf("the id %q answered %d %s", id, code, raw)
		}
	}
	if _, err := os.Lstat(filepath.Join(s.configDir, aiConnectionsFile)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a registry with a foreign id was written: %v", err)
	}
}

// A request carries the revision of the connection it was bound to, and one
// whose connection has changed since — here only its reasoning, nothing that
// moves the credential — is refused rather than sent with today's settings.
func TestAStaleConnectionRevisionIsNotSent(t *testing.T) {
	upstream := newUpstream(t, nil)
	s, ts, _ := assistantServer(t)
	saved := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Work", upstream.server.URL)}})
	c := saved.Connections[0]
	headers := map[string]string{aiConnectionHeader: c.ID, aiRevisionHeader: c.Revision}
	if code, _ := aiTestRequest(t, ts, http.MethodPut, "/api/assistant/key", map[string]string{"key": testKey}, headers); code != http.StatusOK {
		t.Fatal(code)
	}
	changed := saved.aiRegistry
	changed.Connections[0].Assistant.Thinking = "on"
	next := saveTestAIRegistry(t, s, ts, changed)
	if next.Connections[0].Revision == c.Revision {
		t.Fatal("a changed connection kept its revision")
	}
	body := map[string]any{"model": "shared-model", "messages": []any{}}
	if code, raw := aiTestRequest(t, ts, http.MethodPost, "/api/assistant/relay/v1/chat/completions", body, headers); code != http.StatusConflict {
		t.Fatalf("a stale revision answered %d %s", code, raw)
	}
	if len(upstream.arrivals()) != 0 {
		t.Fatal("a stale request reached the provider")
	}
	headers[aiRevisionHeader] = next.Connections[0].Revision
	if code, raw := aiTestRequest(t, ts, http.MethodPost, "/api/assistant/relay/v1/chat/completions", body, headers); code != http.StatusOK {
		t.Fatalf("the current revision answered %d %s", code, raw)
	}
}
