package desk

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testAIAlpha = "ai-111111111111111111111111"
const testAIBeta = "ai-222222222222222222222222"

func aiTestConnection(id, name, url string) aiConnection {
	model := "shared-model"
	return aiConnection{ID: id, Name: name, Enabled: true, Assistant: AssistantSlotView{Engine: "vercel", Thinking: "off", Endpoint: &AssistantEndpointView{URL: url, Kind: "openai-compatible", Models: []string{model}, Model: &model, Tools: []string{}}}}
}
func aiTestRequest(t *testing.T, ts *httptest.Server, method, path string, body any, headers map[string]string) (int, []byte) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	r, err := http.NewRequest(method, ts.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	bearer(r)
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	answer, err := ts.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer answer.Body.Close()
	data, _ := io.ReadAll(answer.Body)
	return answer.StatusCode, data
}
func saveTestAIRegistry(t *testing.T, s *Server, ts *httptest.Server, value aiRegistry) aiRegistryReply {
	t.Helper()
	before, err := s.readAIRegistry()
	if err != nil {
		t.Fatal(err)
	}
	code, raw := aiTestRequest(t, ts, http.MethodPut, "/api/ai-connections", value, map[string]string{"If-Match": before.SHA256})
	if code != 200 {
		t.Fatalf("registry save: %d %s", code, raw)
	}
	var next aiRegistryReply
	if json.Unmarshal(raw, &next) != nil {
		t.Fatal("invalid registry response")
	}
	return next
}
func TestAIConnectionsLegacyMigrationKeepsCredentialsAndDefault(t *testing.T) {
	s, ts, _ := assistantServer(t)
	legacy := `{"deskConfigVersion":1,"assistant":{"engine":"codex","thinking":"off","endpoint":{"url":"https://api.example.invalid/v1","kind":"openai-compatible","models":["shared-model"],"model":"shared-model","tools":[]},"agent":{"provider":"openai","authMethod":"subscription","models":["shared-model"],"model":"shared-model","tools":[]}}}`
	writeDeskConfig(t, s, legacy)
	if err := s.assistant.storeKey(storedKey{present: true, key: "private-legacy-key", origin: "https://api.example.invalid", kind: "openai-compatible"}); err != nil {
		t.Fatal(err)
	}
	value, err := s.readAIRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Connections) != 2 || value.DefaultConnection != "legacy-codex" {
		t.Fatal("legacy target was not preserved")
	}
	if _, err := os.Stat(filepath.Join(s.configDir, aiConnectionsFile)); !os.IsNotExist(err) {
		t.Fatal("reading migrated files")
	}
	next := saveTestAIRegistry(t, s, ts, value.aiRegistry)
	for _, c := range next.Connections {
		if c.ID == "legacy-api" {
			code, raw := aiTestRequest(t, ts, "GET", "/api/assistant/key", nil, map[string]string{aiConnectionHeader: c.ID})
			if code != 200 || !strings.Contains(string(raw), `"bound":true`) || bytes.Contains(raw, []byte("private-legacy-key")) {
				t.Fatal("legacy credential lost or disclosed")
			}
		}
	}
	original, _ := os.ReadFile(s.deskConfigPath())
	if string(original) != legacy {
		t.Fatal("legacy configuration changed")
	}
	stored, err := s.assistant.readKey()
	if err != nil || stored.key != "private-legacy-key" {
		t.Fatal("credential moved or changed")
	}
	if !bytes.Equal(original, []byte(legacy)) {
		t.Fatal("migration altered legacy bytes")
	}
}
func TestAIConnectionsRouteSameModelToSeparateCredentialsAndDesks(t *testing.T) {
	a, b := newUpstream(t, nil), newUpstream(t, nil)
	s, ts, _ := assistantServer(t)
	registry := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Work", a.server.URL), aiTestConnection(testAIBeta, "Personal", b.server.URL)}})
	for i, c := range registry.Connections {
		key := []string{"alpha-secret-for-test", "beta-secret-for-test"}[i]
		headers := map[string]string{aiConnectionHeader: c.ID, aiRevisionHeader: c.Revision}
		code, _ := aiTestRequest(t, ts, "PUT", "/api/assistant/key", map[string]string{"key": key}, headers)
		if code != 200 {
			t.Fatalf("key store %d", code)
		}
		code, _ = aiTestRequest(t, ts, "POST", "/api/assistant/relay/v1/chat/completions", map[string]any{"model": "shared-model", "messages": []any{}}, headers)
		if code != 200 {
			t.Fatalf("relay %d", code)
		}
	}
	for i, u := range []*upstream{a, b} {
		got := u.only(t)
		if got.header.Get("Authorization") != "Bearer "+[]string{"alpha-secret-for-test", "beta-secret-for-test"}[i] {
			t.Fatal("credential crossed connection boundary")
		}
		if got.header.Get(aiConnectionHeader) != "" || got.header.Get(aiRevisionHeader) != "" {
			t.Fatal("internal selector leaked upstream")
		}
	}
	alpha := createTestDesk(t, ts, "Alpha")
	beta := createTestDesk(t, ts, "Beta")
	for i, d := range []string{alpha.ID, beta.ID} {
		id := registry.Connections[i].ID
		profile := `{"profileVersion":2,"inherit":false,"connections":["` + id + `"],"defaultConnection":"` + id + `","models":{}}`
		if err := os.WriteFile(filepath.Join(s.desks[d].projectDir, assistantProfilePath), []byte(profile), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for i, d := range []string{alpha.ID, beta.ID} {
		for j, c := range registry.Connections {
			r := httptest.NewRequest("GET", "http://localhost", nil)
			r.Header.Set(aiConnectionHeader, c.ID)
			r.Header.Set(aiRevisionHeader, c.Revision)
			_, e := s.desks[d].selectedAIConnection(r, true)
			if (e == nil) != (i == j) {
				t.Fatal("connection grant crossed desks")
			}
		}
	}
	raw, _ := os.ReadFile(filepath.Join(s.configDir, aiConnectionsFile))
	if bytes.Contains(raw, []byte("secret-for-test")) {
		t.Fatal("credentials in registry")
	}
}
func TestAIConnectionsChangesCannotRetargetBoundCalls(t *testing.T) {
	a, b := newUpstream(t, nil), newUpstream(t, nil)
	s, ts, _ := assistantServer(t)
	first := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "A", a.server.URL), aiTestConnection(testAIBeta, "B", b.server.URL)}})
	c := first.Connections[0]
	headers := map[string]string{aiConnectionHeader: c.ID, aiRevisionHeader: c.Revision}
	if code, _ := aiTestRequest(t, ts, "PUT", "/api/assistant/key", map[string]string{"key": "test-original-key"}, headers); code != 200 {
		t.Fatal(code)
	}
	changed := first.aiRegistry
	changed.DefaultConnection = testAIBeta
	next := saveTestAIRegistry(t, s, ts, changed)
	body := map[string]any{"model": "shared-model", "messages": []any{}}
	if code, _ := aiTestRequest(t, ts, "POST", "/api/assistant/relay/v1/chat/completions", body, headers); code != 200 {
		t.Fatal("default change disrupted pinned connection", code)
	}
	if len(a.arrivals()) != 1 || len(b.arrivals()) != 0 {
		t.Fatal("default switch rerouted call")
	}
	changed = next.aiRegistry
	changed.Connections[0].Assistant.Endpoint.URL = b.server.URL
	next = saveTestAIRegistry(t, s, ts, changed)
	if code, _ := aiTestRequest(t, ts, "POST", "/api/assistant/relay/v1/chat/completions", body, headers); code != 409 {
		t.Fatal("stale target admitted", code)
	}
	headers[aiRevisionHeader] = next.Connections[0].Revision
	if code, _ := aiTestRequest(t, ts, "POST", "/api/assistant/relay/v1/chat/completions", body, headers); code != 409 {
		t.Fatal("credential followed changed origin", code)
	}
	changed = next.aiRegistry
	changed.Connections = changed.Connections[1:]
	saveTestAIRegistry(t, s, ts, changed)
	if code, _ := aiTestRequest(t, ts, "POST", "/api/assistant/relay/v1/chat/completions", body, headers); code != 409 {
		t.Fatal("removed target fell back", code)
	}
	if code, _ := aiTestRequest(t, ts, "POST", "/api/assistant/relay/v1/chat/completions", body, nil); code != 409 {
		t.Fatal("unidentified request used legacy config", code)
	}
	if len(a.arrivals()) != 1 || len(b.arrivals()) != 0 {
		t.Fatal("refused traffic reached provider")
	}
}
func TestAIConnectionsCASAndValidation(t *testing.T) {
	s, ts, _ := assistantServer(t)
	registry := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "A", "https://example.invalid")}})
	renamed := registry.aiRegistry
	renamed.Connections[0].Name = "Renamed"
	saveTestAIRegistry(t, s, ts, renamed)
	code, _ := aiTestRequest(t, ts, "PUT", "/api/ai-connections", registry.aiRegistry, map[string]string{"If-Match": registry.SHA256})
	if code != 409 {
		t.Fatal("stale writer admitted")
	}
	for _, raw := range []string{`{"version":1,"version":1,"defaultConnection":"","connections":[]}`, `{"version":1,"defaultConnection":"missing","connections":[]}`, `{"version":1,"defaultConnection":"","connections":[],"apiKey":"bad"}`} {
		if _, e := decodeAIRegistry([]byte(raw)); e == nil {
			t.Fatal("invalid registry accepted")
		}
	}
	r := httptest.NewRequest("GET", "http://localhost", nil)
	r.Header.Set(aiConnectionHeader, "../secrets/assistant")
	if _, e := s.selectedAIConnection(r, false); e == nil {
		t.Fatal("path selector admitted")
	}
}
func TestAIConnectionsAccountsAreIndependent(t *testing.T) {
	s, ts, _ := assistantServer(t)
	model := "shared-model"
	a := aiConnection{ID: testAIAlpha, Name: "A", Enabled: true, Assistant: AssistantSlotView{Engine: "codex", Thinking: "off", Agent: &AssistantAgentConfig{Provider: "openai", AuthMethod: "subscription", Model: &model, Models: []string{model}, Tools: []string{}}}}
	b := a
	b.ID = testAIBeta
	b.Name = "B"
	saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: a.ID, Connections: []aiConnection{a, b}})
	aa, bb := &accountStub{}, &accountStub{}
	s.aiAccounts.values = map[string]providerAccountManager{a.ID: aa, b.ID: bb}
	for _, c := range []aiConnection{a, b} {
		r := httptest.NewRequest("GET", "http://localhost", nil)
		r.Header.Set(aiConnectionHeader, c.ID)
		account, e := s.accountForConnection(r)
		if e != nil {
			t.Fatal(e)
		}
		if c.ID == a.ID && account != aa || c.ID == b.ID && account != bb {
			t.Fatal("account crossed connections")
		}
	}
	s.closeAIAccounts()
	if !aa.closed || !bb.closed {
		t.Fatal("account managers not closed")
	}
}

// Admin must be able to discover models before a connection is enabled in a
// desk. That exception must not grant permission to make an inference call.
func TestAIConnectionsDiscoveryDoesNotGrantInference(t *testing.T) {
	upstream := newUpstream(t, nil)
	s, ts, _ := assistantServer(t)
	registry := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Work", upstream.server.URL)}})
	c := registry.Connections[0]
	headers := map[string]string{aiConnectionHeader: c.ID, aiRevisionHeader: c.Revision}
	if code, _ := aiTestRequest(t, ts, "PUT", "/api/assistant/key", map[string]string{"key": "discovery-test-key"}, headers); code != 200 {
		t.Fatal(code)
	}
	profilePath := filepath.Join(s.projectDir, assistantProfilePath)
	if err := os.WriteFile(profilePath, []byte(`{"profileVersion":2,"inherit":false,"connections":[],"defaultConnection":null}`), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _ := aiTestRequest(t, ts, "GET", "/api/assistant/relay/v1/models", nil, headers); code != 200 {
		t.Fatal("admin discovery was tied to desk selection", code)
	}
	body := map[string]any{"model": "shared-model", "messages": []any{}}
	if code, _ := aiTestRequest(t, ts, "POST", "/api/assistant/relay/v1/chat/completions", body, headers); code != 409 {
		t.Fatal("discovery granted inference", code)
	}
	if err := os.WriteFile(profilePath, []byte(`{"profileVersion":2,"inherit":false,"connections":["`+c.ID+`"],"defaultConnection":"`+c.ID+`","models":{"`+c.ID+`":{"inherit":false,"models":["desk-only"],"model":"desk-only"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _ := aiTestRequest(t, ts, "POST", "/api/assistant/relay/v1/chat/completions", body, headers); code != 403 {
		t.Fatal("shared default bypassed desk model list", code)
	}
	body["model"] = "desk-only"
	if code, _ := aiTestRequest(t, ts, "POST", "/api/assistant/relay/v1/chat/completions", body, headers); code != 200 {
		t.Fatal("desk model override was not honored", code)
	}
	changed := registry.aiRegistry
	changed.Connections[0].Enabled = false
	changed.DefaultConnection = ""
	next := saveTestAIRegistry(t, s, ts, changed)
	headers[aiRevisionHeader] = next.Connections[0].Revision
	if code, _ := aiTestRequest(t, ts, "POST", "/api/assistant/relay/v1/chat/completions", body, headers); code != 409 {
		t.Fatal("disabled connection admitted inference", code)
	}
	if len(upstream.arrivals()) != 2 {
		t.Fatal("refused request reached provider")
	}
}
