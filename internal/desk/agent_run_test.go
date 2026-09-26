package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	server := httptest.NewServer(s)
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	s.cfg.Port, _ = strconv.Atoi(u.Port())
	return s, server, token
}
func agentSocket(t *testing.T, server *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/agent/run", &websocket.DialOptions{
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
