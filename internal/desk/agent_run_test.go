package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
	"github.com/coder/websocket"
)

type agentStub struct {
	accountStub
	run func(context.Context, codexbridge.Owner, codexbridge.RunRequest, func(codexbridge.RunEvent) error, codexbridge.ToolHandler) error
}

func (a *agentStub) Run(ctx context.Context, o codexbridge.Owner, r codexbridge.RunRequest, e func(codexbridge.RunEvent) error, h codexbridge.ToolHandler) error {
	return a.run(ctx, o, r, e, h)
}
func agentServer(t *testing.T, runner *agentStub) (*Server, *httptest.Server, string) {
	t.Helper()
	s, _, token := providerTestServer(t)
	s.codex = runner
	if err := os.WriteFile(s.deskConfigPath(), []byte(`{"deskConfigVersion":1,"assistant":{"engine":"codex","agent":{"provider":"openai","authMethod":"subscription","model":"model","tools":[]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(s)
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	s.cfg.Port, _ = strconv.Atoi(u.Port())
	return s, server, token
}
func agentSocket(t *testing.T, server *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	return agentDeskSocket(t, server, token, "")
}
func agentDeskSocket(t *testing.T, server *httptest.Server, token, query string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/agent/run"+query, &websocket.DialOptions{
		Subprotocols: []string{wsProtocol, "jpack-desk-session." + token},
		HTTPHeader:   http.Header{"Origin": []string{server.URL}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.CloseNow() })
	return ws
}
func writeAgent(t *testing.T, ws *websocket.Conn, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err = ws.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}
func readAgent(t *testing.T, ws *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if json.Unmarshal(data, &value) != nil {
		t.Fatal("not JSON")
	}
	return value
}
func startAgent(t *testing.T, ws *websocket.Conn) string {
	writeAgent(t, ws, map[string]any{"type": "start", "request": map[string]any{"model": "model", "prompt": "prompt", "instructions": "instructions", "tools": []any{}}})
	value := readAgent(t, ws)
	if value["type"] != "started" {
		t.Fatalf("no start: %+v", value)
	}
	return value["runId"].(string)
}
func TestAgentSocketToolRoundTripAndTerminalRedaction(t *testing.T) {
	stub := &agentStub{}
	stub.run = func(ctx context.Context, o codexbridge.Owner, r codexbridge.RunRequest, emit func(codexbridge.RunEvent) error, tool codexbridge.ToolHandler) error {
		if o.ID == "" || o.Session.Err() != nil || r.Model != "model" {
			return errors.New("bad admission")
		}
		answer, err := tool(ctx, codexbridge.ToolCall{ID: "opaque-call", Name: "desk_runtime_0", Arguments: json.RawMessage(`{"rehearsal":false}`)})
		if err != nil {
			return err
		}
		if answer.Text != "runtime result" {
			return errors.New("wrong reply")
		}
		if err = emit(codexbridge.RunEvent{Type: "message", ID: "message", Text: "Complete", Phase: "final"}); err != nil {
			return err
		}
		return errors.New("PRIVATE_SENTINEL")
	}
	_, server, token := agentServer(t, stub)
	ws := agentSocket(t, server, token)
	runID := startAgent(t, ws)
	call := readAgent(t, ws)
	if call["type"] != "tool-call" {
		t.Fatal(call)
	}
	writeAgent(t, ws, map[string]any{"type": "tool-result", "runId": runID, "callId": "opaque-call", "answer": map[string]any{"text": "runtime result", "isError": false}})
	event := readAgent(t, ws)
	if event["type"] != "event" {
		t.Fatal(event)
	}
	end := readAgent(t, ws)
	if end["type"] != "end" || end["error"] != "run-failed" {
		t.Fatal(end)
	}
}
func TestAgentSocketRejectsCrossRunDuplicateAndUnknownMessages(t *testing.T) {
	for _, mode := range []string{"cross-run", "unsolicited", "duplicate", "unknown", "restart", "disconnect", "cancel", "session", "policy"} {
		t.Run(mode, func(t *testing.T) {
			stopped := make(chan struct{})
			stub := &agentStub{}
			stub.run = func(ctx context.Context, _ codexbridge.Owner, _ codexbridge.RunRequest, _ func(codexbridge.RunEvent) error, tool codexbridge.ToolHandler) error {
				defer close(stopped)
				_, err := tool(ctx, codexbridge.ToolCall{ID: "call", Name: "desk_runtime_0", Arguments: json.RawMessage(`{}`)})
				if mode == "duplicate" && err == nil {
					<-ctx.Done()
					return ctx.Err()
				}
				return err
			}
			s, server, token := agentServer(t, stub)
			ws := agentSocket(t, server, token)
			runID := startAgent(t, ws)
			_ = readAgent(t, ws)
			switch mode {
			case "disconnect":
				_ = ws.CloseNow()
			case "session":
				s.sessions.revoke(token)
			case "policy":
				s.signIn.mu.Lock()
				s.signIn.cancelEpoch()
				s.signIn.mu.Unlock()
			case "cancel":
				writeAgent(t, ws, map[string]any{"type": "cancel", "runId": runID})
			case "restart":
				writeAgent(t, ws, map[string]any{"type": "start", "runId": runID, "request": map[string]any{}})
			default:
				body := map[string]any{"type": "tool-result", "runId": runID, "callId": "call", "answer": map[string]any{"text": "ok", "isError": false}}
				if mode == "cross-run" {
					body["runId"] = "other"
				}
				if mode == "unsolicited" {
					body["callId"] = "other"
				}
				if mode == "unknown" {
					body["command"] = "exec"
				}
				writeAgent(t, ws, body)
				if mode == "duplicate" {
					writeAgent(t, ws, body)
				}
			}
			select {
			case <-stopped:
			case <-time.After(2 * time.Second):
				t.Fatal("socket did not cancel pending tool")
			}
		})
	}
}
func TestAgentSocketRequiresBrowserSessionAndSameOrigin(t *testing.T) {
	_, server, token := agentServer(t, &agentStub{})
	for _, tc := range []struct {
		token, origin string
		status        int
	}{
		{"", server.URL, 401}, {strings.Repeat("b", 48), server.URL, 401}, {token, "https://untrusted.invalid", 403},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		protocols := []string{wsProtocol}
		if tc.token != "" {
			protocols = append(protocols, "jpack-desk-session."+tc.token)
		}
		ws, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/agent/run", &websocket.DialOptions{Subprotocols: protocols, HTTPHeader: http.Header{"Origin": []string{tc.origin}}})
		cancel()
		if ws != nil {
			ws.CloseNow()
		}
		if err == nil || response == nil || response.StatusCode != tc.status {
			t.Fatalf("guard: %v response %v", err, response)
		}
	}
}

func TestAgentSocketNamedDeskRun(t *testing.T) {
	s, server, _ := assistantServer(t)
	desk := createTestDesk(t, server, "Research")
	token, err := s.sessions.create("local-user", nil)
	if err != nil {
		t.Fatal(err)
	}
	child := s.desks[desk.ID]
	if err := os.WriteFile(child.deskConfigPath(), []byte(`{"deskConfigVersion":1,"assistant":{"engine":"codex","agent":{"provider":"openai","authMethod":"subscription","model":"model","models":["model"],"tools":[]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	// Exercise the real router and selected desk with no native inference.
	child.codex = &agentStub{run: func(_ context.Context, _ codexbridge.Owner, _ codexbridge.RunRequest, emit func(codexbridge.RunEvent) error, _ codexbridge.ToolHandler) error {
		return emit(codexbridge.RunEvent{Type: "message", ID: "message", Text: "Complete", Phase: "final"})
	}}
	ws := agentDeskSocket(t, server, token, "?desk="+desk.ID)
	runID := startAgent(t, ws)
	if event := readAgent(t, ws); event["type"] != "event" || event["runId"] != runID {
		t.Fatal("named desk did not emit a response", event)
	}
	if end := readAgent(t, ws); end["type"] != "end" || end["runId"] != runID || end["error"] != "" {
		t.Fatal("named desk did not complete", end)
	}
}

func TestAgentSocketRejectsUnrelatedQueries(t *testing.T) {
	s, server, token := agentServer(t, &agentStub{})
	s.cfg.deskID = strings.Repeat("a", 32)
	// Admission must reject a different desk, duplicate or extra parameters,
	// credentials, and malformed queries even when the caller has a session.
	for _, query := range []string{"?", "?desk=", "?desk=" + strings.Repeat("b", 32), "?desk=" + s.cfg.deskID + "&desk=" + s.cfg.deskID, "?desk=" + s.cfg.deskID + "&extra=1", "?token=PRIVATE_SENTINEL", "?desk=%zz"} {
		r := httptest.NewRequest("GET", server.URL+"/api/agent/run"+query, nil)
		r.Header.Set("Sec-WebSocket-Protocol", wsProtocol+", jpack-desk-session."+token)
		w := httptest.NewRecorder()
		s.handleAgentRun(w, r)
		if w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), "PRIVATE_SENTINEL") {
			t.Fatalf("query admission: status %d", w.Code)
		}
	}
}

func TestAgentSocketEnforcesSavedModelPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, config, model string
		allowed             bool
	}{
		{"legacy default", `{"model":"model","tools":[]}`, "model", true},
		{"legacy denies other", `{"model":"model","tools":[]}`, "second", false},
		{"explicit other allowed", `{"model":"model","models":["model","second"],"tools":[]}`, "second", true},
		{"explicit other denied", `{"model":"model","models":["model"],"tools":[]}`, "second", false},
		{"nothing allowed", `{"model":null,"models":[],"tools":[]}`, "model", false},
		{"invalid config", `{"model":"model","models":null,"tools":[]}`, "model", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := make(chan string, 1)
			stub := &agentStub{run: func(_ context.Context, _ codexbridge.Owner, r codexbridge.RunRequest, _ func(codexbridge.RunEvent) error, _ codexbridge.ToolHandler) error {
				called <- r.Model
				return nil
			}}
			s, server, token := agentServer(t, stub)
			var agent map[string]any
			if err := json.Unmarshal([]byte(tc.config), &agent); err != nil {
				t.Fatal(err)
			}
			agent["provider"] = "openai"
			agent["authMethod"] = "subscription"
			config, _ := json.Marshal(map[string]any{"deskConfigVersion": 1, "assistant": map[string]any{"engine": "codex", "agent": agent}})
			if err := os.WriteFile(s.deskConfigPath(), config, 0600); err != nil {
				t.Fatal(err)
			}
			ws := agentSocket(t, server, token)
			writeAgent(t, ws, map[string]any{"type": "start", "request": map[string]any{"model": tc.model, "prompt": "test", "instructions": "test", "tools": []any{}}})
			if got := readAgent(t, ws); got["type"] != "started" {
				t.Fatal(got)
			}
			end := readAgent(t, ws)
			if end["type"] != "end" {
				t.Fatal(end)
			}
			if tc.allowed {
				if end["error"] != "" {
					t.Fatal(end)
				}
				select {
				case model := <-called:
					if model != tc.model {
						t.Fatal(model)
					}
				default:
					t.Fatal("allowed model did not run")
				}
			} else {
				if end["error"] != "model-not-allowed" {
					t.Fatal(end)
				}
				select {
				case <-called:
					t.Fatal("unapproved model ran")
				default:
				}
			}
		})
	}
}
func TestAgentModelPolicyRereadsDiskAndFailsClosed(t *testing.T) {
	s, _, _ := agentServer(t, &agentStub{})
	if !s.agentModelAllowed("model") {
		t.Fatal("legacy default missing")
	}
	for _, config := range []string{`{}`, `{"deskConfigVersion":1,"assistant":{"engine":"openai-compatible"}}`, `invalid`, string([]byte{0xff})} {
		if err := os.WriteFile(s.deskConfigPath(), []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		if s.agentModelAllowed("model") {
			t.Fatal("invalid or changed policy authorized")
		}
	}
	if err := os.Remove(s.deskConfigPath()); err != nil {
		t.Fatal(err)
	}
	if s.agentModelAllowed("model") {
		t.Fatal("missing policy authorized")
	}
}
