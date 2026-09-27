package codexbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunHelper(t *testing.T) {
	if os.Getenv("JPS_RUN_HELPER") != "1" {
		return
	}
	mode := os.Getenv("JPS_RUN_MODE")
	write := func(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
	note := func(method string, params any) { write(map[string]any{"method": method, "params": params}) }
	tool := func(name, thread, call string) {
		write(map[string]any{"id": "native-rpc", "method": "item/tool/call", "params": map[string]any{
			"threadId": thread, "turnId": "turn", "callId": call, "tool": name, "arguments": map[string]any{"rehearsal": false},
		}})
	}
	finish := func() {
		note("item/started", map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]string{"id": "message", "type": "agentMessage"}})
		note("item/agentMessage/delta", map[string]string{"threadId": "thread", "turnId": "turn", "itemId": "message", "delta": "Complete"})
		text := "Complete"
		if mode == "mismatch" {
			text = "different"
		}
		note("item/completed", map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]string{"id": "message", "type": "agentMessage", "phase": "final_answer", "text": text}})
		status := "completed"
		if mode == "failed" {
			status = "failed"
		}
		note("turn/completed", map[string]any{"threadId": "thread", "turn": map[string]string{"id": "turn", "status": status}})
	}
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 4096), maxFrame)
	for scan.Scan() {
		var f frame
		if json.Unmarshal(scan.Bytes(), &f) != nil {
			os.Exit(1)
		}
		reply := func(v any) { write(map[string]any{"id": f.ID, "result": v}) }
		switch f.Method {
		case "initialize":
			reply(map[string]string{"userAgent": "jps_desk/0.157.1 (Linux)"})
		case "initialized":
		case "account/read":
			var account any
			if _, err := os.Stat(filepath.Join(os.Getenv("CODEX_HOME"), "account.fixture")); err == nil {
				account = map[string]string{"type": "chatgpt", "planType": "plus", "email": "PRIVATE_SENTINEL"}
			}
			reply(map[string]any{"requiresOpenaiAuth": true, "account": account})
		case "account/logout":
			_ = os.Remove(filepath.Join(os.Getenv("CODEX_HOME"), "account.fixture"))
			reply(map[string]any{})
		case "model/list":
			var params struct {
				IncludeHidden bool `json:"includeHidden"`
			}
			_ = json.Unmarshal(f.Params, &params)
			if params.IncludeHidden {
				// The launch check lists everything; discovery below lists the
				// visible models this scenario offers.
				reply(map[string]any{"data": catalogRows()})
				continue
			}
			if mode == "models-cycle" {
				reply(map[string]any{"data": []any{}, "nextCursor": "again"})
				continue
			}
			if mode == "models-unsafe-default" {
				reply(map[string]any{"data": []any{map[string]any{"model": "model", "displayName": "Model", "defaultReasoningEffort": "ultra", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "ultra"}}}}})
				continue
			}
			if mode == "models-duplicate" {
				row := map[string]any{"model": "model", "displayName": "Model", "defaultReasoningEffort": "medium", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "medium"}}}
				reply(map[string]any{"data": []any{row, row}})
				continue
			}
			reply(map[string]any{"data": []any{map[string]any{"model": "model", "displayName": "Model", "hidden": false, "defaultReasoningEffort": "medium", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "medium"}}}}})
		case "thread/start":
			var args map[string]json.RawMessage
			_ = json.Unmarshal(f.Params, &args)
			if string(args["environments"]) != "[]" || string(args["runtimeWorkspaceRoots"]) != "[]" ||
				string(args["approvalPolicy"]) != `"never"` || string(args["allowProviderModelFallback"]) != "false" ||
				string(args["permissions"]) != `"jps"` {
				os.Exit(2)
			}
			provider := "openai"
			if mode == "bad-start" {
				provider = "other"
			}
			cwd, _ := os.Getwd()
			reply(map[string]any{"thread": map[string]string{"id": "thread"}, "model": "model", "modelProvider": provider, "approvalPolicy": "never", "cwd": cwd})
		case "turn/start":
			var args map[string]json.RawMessage
			_ = json.Unmarshal(f.Params, &args)
			if string(args["environments"]) != "[]" || string(args["permissions"]) != `"jps"` {
				os.Exit(3)
			}
			reply(map[string]any{"turn": map[string]string{"id": "turn"}})
			switch mode {
			case "hang":
			case "permission":
				write(map[string]any{"id": "request", "method": "item/commandExecution/requestApproval", "params": map[string]string{"secret": "PRIVATE_SENTINEL"}})
			case "wrong-thread":
				tool("desk_runtime_0", "other", "call")
			case "wrong-tool":
				tool("exec_command", "thread", "call")
			case "overflow":
				note("item/agentMessage/delta", map[string]string{"threadId": "thread", "turnId": "turn", "itemId": "message", "delta": strings.Repeat("x", (2<<20)+1)})
			case "reasoning-limit":
				for i := 0; i < 5; i++ {
					note("item/started", map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]string{"id": string(rune('a' + i)), "type": "reasoning"}})
				}
			default:
				tool("desk_runtime_0", "thread", "call")
			}
		case "":
			if mode == "duplicate" {
				tool("desk_runtime_0", "thread", "call")
			} else {
				finish()
			}
		default:
			os.Exit(4)
		}
	}
	os.Exit(0)
}
func runManager(t *testing.T, mode string) *Manager {
	t.Helper()
	m, _ := accountManager(t, "pending")
	m.launch = func(ctx context.Context, native *exec.Cmd) (*client, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRunHelper$")
		cmd.Env = append(native.Env, "JPS_RUN_HELPER=1", "JPS_RUN_MODE="+mode)
		cmd.Dir = native.Dir
		return startClient(ctx, cmd)
	}
	if err := os.WriteFile(filepath.Join(m.profile.codex, "account.fixture"), []byte("chatgpt"), 0600); err != nil {
		t.Fatal(err)
	}
	return m
}
func runRequest() RunRequest {
	return RunRequest{Model: "model", Prompt: "Check", Instructions: "Desk instructions", Tools: []Tool{{Name: "desk_runtime_0", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
}
func TestNativeRunUsesOnlyHostToolsAndClosedEvents(t *testing.T) {
	m := runManager(t, "normal")
	o, a, b := ownerFixture()
	defer a()
	defer b()
	var events []RunEvent
	calls := 0
	err := m.Run(context.Background(), o, runRequest(), func(event RunEvent) error { events = append(events, event); return nil }, func(ctx context.Context, call ToolCall) (ToolAnswer, error) {
		calls++
		if call.ID == "call" || call.Name != "desk_runtime_0" || string(call.Arguments) != `{"rehearsal":false}` {
			t.Fatalf("bad call: %+v", call)
		}
		return ToolAnswer{Text: "Verified by Desk"}, nil
	})
	if err != nil || calls != 1 || len(events) != 2 || events[0].Type != "text" || events[1].Text != "Complete" || events[1].Phase != "final" || events[0].ID != events[1].ID {
		t.Fatalf("run: %v calls %d events %+v", err, calls, events)
	}
	wire, _ := json.Marshal(events)
	if strings.Contains(string(wire), "PRIVATE") || strings.Contains(string(wire), `"message"`+":") {
		t.Fatal("native payload escaped")
	}
	if m.active != nil || m.client != nil {
		t.Fatal("completed run retained native writer")
	}
}
func TestRunRejectsForeignDuplicatedAndUnexpectedNativeWork(t *testing.T) {
	for _, mode := range []string{"permission", "wrong-thread", "wrong-tool", "duplicate", "failed", "mismatch", "bad-start", "overflow", "reasoning-limit"} {
		t.Run(mode, func(t *testing.T) {
			m := runManager(t, mode)
			o, a, b := ownerFixture()
			defer a()
			defer b()
			request := runRequest()
			if mode == "reasoning-limit" {
				request.Phase = "critic"
			}
			calls := 0
			err := m.Run(context.Background(), o, request, func(RunEvent) error { return nil }, func(context.Context, ToolCall) (ToolAnswer, error) { calls++; return ToolAnswer{Text: "ok"}, nil })
			if err == nil || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("bad run accepted or leaked: %v", err)
			}
			want := 0
			if mode == "duplicate" || mode == "failed" || mode == "mismatch" {
				want = 1
			}
			if calls != want {
				t.Fatalf("calls=%d want=%d", calls, want)
			}
			if m.active != nil || m.client != nil {
				t.Fatal("failed run not released")
			}
		})
	}
}
func TestRunCancellationLogoutAndShutdownStopPendingTool(t *testing.T) {
	for _, why := range []string{"cancel", "session", "policy", "logout", "close"} {
		t.Run(why, func(t *testing.T) {
			m := runManager(t, "normal")
			o, a, b := ownerFixture()
			defer a()
			defer b()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			release := make(chan struct{})
			defer close(release)
			done := make(chan error, 1)
			go func() {
				done <- m.Run(ctx, o, runRequest(), func(RunEvent) error { return nil }, func(context.Context, ToolCall) (ToolAnswer, error) {
					close(entered)
					<-release
					return ToolAnswer{Text: "late"}, nil
				})
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("no tool")
			}
			if _, err := m.Status(context.Background(), o.ID, false); !errors.Is(err, ErrBusy) {
				t.Fatalf("account read during run: %v", err)
			}
			if err := m.Run(ctx, o, runRequest(), func(RunEvent) error { return nil }, func(context.Context, ToolCall) (ToolAnswer, error) { return ToolAnswer{}, nil }); !errors.Is(err, ErrBusy) {
				t.Fatalf("second run: %v", err)
			}
			switch why {
			case "cancel":
				cancel()
			case "session":
				a()
			case "policy":
				b()
			case "logout":
				if err := m.Logout(context.Background()); err != nil {
					t.Fatal(err)
				}
			case "close":
				m.Close()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("canceled run succeeded")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("run did not stop")
			}
		})
	}
}
func TestRunRequiresSubscriptionAndValidLimits(t *testing.T) {
	m := runManager(t, "normal")
	o, a, b := ownerFixture()
	defer a()
	defer b()
	_ = os.Remove(filepath.Join(m.profile.codex, "account.fixture"))
	err := m.Run(context.Background(), o, runRequest(), func(RunEvent) error { return nil }, func(context.Context, ToolCall) (ToolAnswer, error) {
		t.Fatal("unauthenticated tool call")
		return ToolAnswer{}, nil
	})
	if !errors.Is(err, ErrSignIn) {
		t.Fatalf("unsigned run: %v", err)
	}
	for _, effort := range []string{"ultra", "arbitrary"} {
		req := runRequest()
		req.Effort = effort
		if _, err := threadParams(req, m.profile.work); err == nil {
			t.Fatal("unsupported reasoning admitted")
		}
	}
}

func TestModelDiscoveryAndSelectionAreSubscriptionBound(t *testing.T) {
	m := runManager(t, "normal")
	models, err := m.Models(context.Background())
	if err != nil || len(models) != 1 || models[0].ID != "model" || models[0].DefaultEffort != "medium" {
		t.Fatalf("models %#v: %v", models, err)
	}
	body, _ := json.Marshal(models)
	if strings.Contains(string(body), "PRIVATE_SENTINEL") {
		t.Fatal("account metadata escaped")
	}
	o, a, b := ownerFixture()
	defer a()
	defer b()
	for _, request := range []RunRequest{func() RunRequest { r := runRequest(); r.Model = "unlisted"; return r }(), func() RunRequest { r := runRequest(); r.Effort = "high"; return r }()} {
		err = m.Run(context.Background(), o, request, func(RunEvent) error { t.Fatal("unlisted selection emitted"); return nil }, func(context.Context, ToolCall) (ToolAnswer, error) {
			t.Fatal("unlisted selection executed")
			return ToolAnswer{}, nil
		})
		if err == nil {
			t.Fatal("unlisted model/effort admitted")
		}
	}
	if err = m.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		_, err = m.Models(context.Background())
		if !errors.Is(err, ErrBusy) || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !errors.Is(err, ErrSignIn) {
		t.Fatalf("signed-out discovery: %v", err)
	}
}
func TestModelDiscoveryBoundsAndUnsafeDefaults(t *testing.T) {
	for _, mode := range []string{"models-cycle", "models-duplicate", "models-unsafe-default"} {
		t.Run(mode, func(t *testing.T) {
			m := runManager(t, mode)
			models, err := m.Models(context.Background())
			if mode == "models-unsafe-default" {
				if err != nil || len(models) != 0 {
					t.Fatalf("unsafe default offered: %#v %v", models, err)
				}
			} else if err == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}
}
