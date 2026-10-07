package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// The answers to #276's first review round, each held by a test.

// lockWatchingWriter records, at every header and body write, whether the
// desk-wide writes lock was held at that instant.
type lockWatchingWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
	lock   *sync.Mutex
	held   []bool
}

func (w *lockWatchingWriter) Header() http.Header { return w.header }
func (w *lockWatchingWriter) WriteHeader(status int) {
	w.status = status
	w.note()
}
func (w *lockWatchingWriter) Write(b []byte) (int, error) {
	w.note()
	return w.body.Write(b)
}
func (w *lockWatchingWriter) note() {
	if w.lock.TryLock() {
		w.lock.Unlock()
		w.held = append(w.held, false)
		return
	}
	w.held = append(w.held, true)
}

// `s.writes` is every desk's write lock. A registry save decides its answer
// under it and writes that answer after releasing it, so a slow reader of a
// mebibyte of answer holds no other desk's writes — whether the save landed
// or was refused.
func TestTheRegistryAnswerIsWrittenAfterTheLockIsReleased(t *testing.T) {
	s, ts, _ := assistantServer(t)
	put := func(value aiRegistry, ifMatch string) *lockWatchingWriter {
		t.Helper()
		raw, _ := json.Marshal(value)
		r := httptest.NewRequest(http.MethodPut, ts.URL+"/api/ai-connections", bytes.NewReader(raw))
		bearer(r)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", ifMatch)
		w := &lockWatchingWriter{header: http.Header{}, lock: s.writes}
		s.handleAIConnections(w, r)
		return w
	}
	before, err := s.readAIRegistry()
	if err != nil {
		t.Fatal(err)
	}
	value := aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Work", "https://api.example.invalid")}}
	for _, tc := range []struct {
		name    string
		ifMatch string
		status  int
	}{{"a save that lands", before.SHA256, http.StatusOK}, {"a stale save", before.SHA256, http.StatusConflict}} {
		w := put(value, tc.ifMatch)
		if w.status != tc.status {
			t.Fatalf("%s answered %d %s", tc.name, w.status, w.body.String())
		}
		if len(w.held) == 0 {
			t.Fatalf("%s wrote nothing", tc.name)
		}
		for _, held := range w.held {
			if held {
				t.Fatalf("%s wrote its answer while every desk's writes lock was held", tc.name)
			}
		}
	}
}

// aiAgentRunOutcome is one ChatGPT run through the socket, on a registry
// holding one ChatGPT connection: the error its end frame carried, and whether
// the account's runner was reached.
func aiAgentRunOutcome(t *testing.T, enabled, withRevision bool) (string, bool) {
	t.Helper()
	called := make(chan struct{}, 1)
	stub := &agentStub{run: func(_ context.Context, _ codexbridge.Owner, _ codexbridge.RunRequest, emit func(codexbridge.RunEvent) error, _ codexbridge.ToolHandler) error {
		called <- struct{}{}
		return emit(codexbridge.RunEvent{Type: "message", ID: "message", Text: "Complete", Phase: "final"})
	}}
	s, server, token := agentServer(t, &agentStub{})
	model := "model"
	c := aiConnection{ID: testAIAlpha, Name: "Work", Enabled: enabled, Assistant: AssistantSlotView{Engine: "codex", Thinking: "off", Agent: &AssistantAgentConfig{Provider: "openai", AuthMethod: "subscription", Model: &model, Models: []string{model}, Tools: []string{}}}}
	defaultConnection := ""
	if enabled {
		defaultConnection = c.ID
	}
	saved := saveTestAIRegistry(t, s, server, aiRegistry{Version: 1, DefaultConnection: defaultConnection, Connections: []aiConnection{c}})
	s.aiAccounts.values = map[string]providerAccountManager{c.ID: stub}
	query := "?connection=" + c.ID
	if withRevision {
		query += "&revision=" + saved.Connections[0].Revision
	}
	ws := agentDeskSocket(t, server, token, query)
	runID := startAgent(t, ws)
	for {
		frame := readAgent(t, ws)
		if frame["type"] != "end" {
			continue
		}
		if frame["runId"] != runID {
			t.Fatalf("an end for another run: %+v", frame)
		}
		select {
		case <-called:
			return frame["error"].(string), true
		default:
			return frame["error"].(string), false
		}
	}
}

// A ChatGPT connection that is disabled does not run, and a run that does not
// say which revision of its connection it was bound to does not run either:
// the account's runner is never reached.
func TestAChatGPTRunNeedsAnEnabledConnectionAndItsRevision(t *testing.T) {
	if refusal, ran := aiAgentRunOutcome(t, true, true); refusal != "" || !ran {
		t.Fatalf("an enabled connection with its revision: %q, ran %v", refusal, ran)
	}
	if refusal, ran := aiAgentRunOutcome(t, false, true); refusal != "model-not-allowed" || ran {
		t.Fatalf("a disabled connection: %q, ran %v", refusal, ran)
	}
	if refusal, ran := aiAgentRunOutcome(t, true, false); refusal != "model-not-allowed" || ran {
		t.Fatalf("a run naming no revision: %q, ran %v", refusal, ran)
	}
}

// A relayed request names the revision of the connection it was bound to; one
// that names none is refused, and nothing reaches the provider.
func TestARelayedRequestWithNoRevisionIsNotSent(t *testing.T) {
	upstream := newUpstream(t, nil)
	s, ts, _ := assistantServer(t)
	saved := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Work", upstream.server.URL)}})
	c := saved.Connections[0]
	if code, _ := aiTestRequest(t, ts, http.MethodPut, "/api/assistant/key", map[string]string{"key": testKey}, map[string]string{aiConnectionHeader: c.ID, aiRevisionHeader: c.Revision}); code != http.StatusOK {
		t.Fatal(code)
	}
	body := map[string]any{"model": "shared-model", "messages": []any{}}
	if code, raw := aiTestRequest(t, ts, http.MethodPost, "/api/assistant/relay/v1/chat/completions", body, map[string]string{aiConnectionHeader: c.ID}); code != http.StatusConflict {
		t.Fatalf("a request naming no revision answered %d %s", code, raw)
	}
	if len(upstream.arrivals()) != 0 {
		t.Fatal("a request naming no revision reached the provider")
	}
	if code, raw := aiTestRequest(t, ts, http.MethodPost, "/api/assistant/relay/v1/chat/completions", body, map[string]string{aiConnectionHeader: c.ID, aiRevisionHeader: c.Revision}); code != http.StatusOK {
		t.Fatalf("the same request with its revision answered %d %s", code, raw)
	}
}

// The model a request names is read by its exact name, as a provider reads
// it. A second `model` member, in the same case or another, is refused rather
// than chosen between: Go's decoder matches names without case and keeps the
// last one.
func TestTheModelIsReadByItsExactName(t *testing.T) {
	allowed := []string{"shared-model"}
	for body, want := range map[string]bool{
		`{"model":"shared-model","messages":[]}`:                  true,
		`{"messages":[{"model":"x"}],"model":"shared-model"}`:     true,
		`{"model":"not-allowed","Model":"shared-model"}`:          false,
		`{"Model":"shared-model"}`:                                false,
		`{"MODEL":"shared-model","messages":[]}`:                  false,
		`{"model":"not-allowed","model":"shared-model"}`:          false,
		`{"model":["shared-model"]}`:                              false,
		`{"messages":[]}`:                                         false,
		`["shared-model"]`:                                        false,
		`{"model":"shared-model"`:                                 false,
		`{"model":"shared-model"} {"model":"other"}`:              false,
		`{"mOdel":"shared-model","model":"shared-model"}`:         false,
		`{"model":"shared-model","nested":{"Model":"other"}}`:     true,
		`{"model":"shared-model","list":[{"MODEL":"other"},1,2]}`: true,
	} {
		if got := profileAPIModelAllowed("openai-compatible", "v1/chat/completions", []byte(body), allowed); got != want {
			t.Errorf("%s: allowed %v, want %v", body, got, want)
		}
	}

	// And through the relay, with the desk's list for the connection.
	upstream := newUpstream(t, nil)
	s, ts, _ := assistantServer(t)
	saved := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Work", upstream.server.URL)}})
	c := saved.Connections[0]
	headers := map[string]string{aiConnectionHeader: c.ID, aiRevisionHeader: c.Revision}
	if code, _ := aiTestRequest(t, ts, http.MethodPut, "/api/assistant/key", map[string]string{"key": testKey}, headers); code != http.StatusOK {
		t.Fatal(code)
	}
	profile := `{"profileVersion":2,"inherit":false,"connections":["` + c.ID + `"],"defaultConnection":"` + c.ID + `","models":{"` + c.ID + `":{"inherit":false,"models":["shared-model"],"model":"shared-model"}}}`
	if err := os.WriteFile(filepath.Join(s.projectDir, assistantProfilePath), []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequest(http.MethodPost, ts.URL+"/api/assistant/relay/v1/chat/completions", strings.NewReader(`{"model":"not-allowed","Model":"shared-model","messages":[]}`))
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
	answer.Body.Close()
	if answer.StatusCode != http.StatusForbidden || len(upstream.arrivals()) != 0 {
		t.Fatalf("a model named twice answered %d, %d arrivals", answer.StatusCode, len(upstream.arrivals()))
	}
}

// A connection's name is counted in characters, as the page counts it, and a
// name over the bound is refused in a sentence about the name rather than one
// asking somebody to repair a file.
func TestAConnectionNameIsCountedInCharacters(t *testing.T) {
	s, ts, _ := assistantServer(t)
	// Two-byte and four-byte characters: 128 of them is 384 bytes, and 192
	// UTF-16 units, and one name.
	at := strings.Repeat("é", maxAIConnectionName/2) + strings.Repeat("😀", maxAIConnectionName/2)
	saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, at, "https://api.example.invalid")}})
	before, err := s.readAIRegistry()
	if err != nil {
		t.Fatal(err)
	}
	over := at + "é"
	code, raw := aiTestRequest(t, ts, http.MethodPut, "/api/ai-connections", aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, over, "https://api.example.invalid")}}, map[string]string{"If-Match": before.SHA256})
	if code != http.StatusUnprocessableEntity || !bytes.Contains(raw, []byte("1 to 128 characters")) || bytes.Contains(raw, []byte("repair")) {
		t.Fatalf("a name of %d characters answered %d %s", len([]rune(over)), code, raw)
	}
}
