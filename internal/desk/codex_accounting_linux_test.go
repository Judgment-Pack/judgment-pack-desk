package desk

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// Normal host constructors start this test executable directly, with exactly
// the native arguments. No launch override, installed CLI or real auth is used.
func init() {
	if len(os.Args) == 4 && os.Args[1] == "app-server" && os.Args[2] == "--stdio" && os.Args[3] == "--strict-config" && os.Getenv("CODEX_HOME") != "" {
		accountingHostPeer()
		os.Exit(0)
	}
}
func accountingHostPeer() {
	send := func(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var f struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &f) != nil {
			os.Exit(8)
		}
		reply := func(v any) { send(map[string]any{"id": f.ID, "result": v}) }
		switch f.Method {
		case "initialize":
			reply(map[string]string{"userAgent": "jps_desk/0.157.1 (Linux)"})
		case "initialized":
		case "account/read":
			reply(map[string]any{"requiresOpenaiAuth": true, "account": map[string]string{"type": "chatgpt", "planType": "plus", "email": "PRIVATE_AUTH_SENTINEL"}})
		case "model/list":
			var p struct {
				Hidden bool `json:"includeHidden"`
			}
			_ = json.Unmarshal(f.Params, &p)
			// Handwritten catalog fixture; a pin change must update this test explicitly.
			models := []string{"gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5"}
			if !p.Hidden {
				models = []string{"gpt-6-sol"}
			}
			rows := []any{}
			for _, model := range models {
				rows = append(rows, map[string]any{"model": model, "displayName": model, "defaultReasoningEffort": "medium", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "medium"}}})
			}
			reply(map[string]any{"data": rows})
		case "thread/start":
			cwd, _ := os.Getwd()
			reply(map[string]any{"thread": map[string]string{"id": "fixture-thread"}, "model": "gpt-6-sol", "modelProvider": "openai", "approvalPolicy": "never", "cwd": cwd, "reasoningEffort": "medium"})
		case "turn/start":
			reply(map[string]any{"turn": map[string]string{"id": "fixture-turn"}})
			send(map[string]any{"method": "thread/tokenUsage/updated", "params": json.RawMessage(`{"threadId":"fixture-thread","turnId":"fixture-turn","tokenUsage":{"last":{"inputTokens":2,"cachedInputTokens":0,"outputTokens":3,"reasoningOutputTokens":1,"totalTokens":5},"total":{"inputTokens":2,"cachedInputTokens":0,"outputTokens":3,"reasoningOutputTokens":1,"totalTokens":5}}}`)})
			send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "fixture-thread", "turn": map[string]string{"id": "fixture-turn", "status": "completed"}}})
		default:
			os.Exit(9)
		}
	}
}
func TestCodexAccountingHostConstructorsAndNoPublicMapping(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	sink := codexbridge.NewAccountingSink()
	cfg := Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), JpackBin: executable, CodexBin: executable, CodexAccounting: sink, Port: 8791, Token: testToken}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg.parent = s
	cfg.ProjectDir = t.TempDir()
	cfg.CodexAccounting = nil
	child, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if child.codex != s.codex {
		t.Fatal("named desk did not share root manager")
	}
	run := func(account providerAccountManager, want uint64) {
		runner, ok := account.(providerRunner)
		if !ok {
			t.Fatal("production manager cannot run")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		owner := codexbridge.Owner{ID: "host-fixture", Session: ctx, Policy: ctx}
		err := runner.Run(ctx, owner, codexbridge.RunRequest{Model: "gpt-6-sol", Effort: "medium", Prompt: "Fixture", Instructions: "Host fixture"}, func(codexbridge.RunEvent) error { return nil }, func(context.Context, codexbridge.ToolCall) (codexbridge.ToolAnswer, error) {
			t.Error("unexpected tool")
			return codexbridge.ToolAnswer{}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case report := <-sink.Reports():
			if report.Invocation != want || !report.ApplicationSuccess || !report.OwnedClientReaped || report.Usage.Total.Total.Value != 5 || report.Incomplete {
				t.Fatalf("host sink not injected: %+v", report)
			}
		default:
			t.Fatal("constructor lost accounting sink")
		}
	}
	run(child.codex, 1)
	model := "gpt-6-sol"
	connection := aiConnection{ID: testAIAlpha, Name: "Fixture", Enabled: true, Assistant: AssistantSlotView{Engine: "codex", Thinking: "off", Agent: &AssistantAgentConfig{Provider: "openai", AuthMethod: "subscription", Model: &model, Models: []string{model}, Tools: []string{}}}}
	data, _ := json.Marshal(aiRegistry{Version: 1, DefaultConnection: connection.ID, Connections: []aiConnection{connection}})
	if err := s.assistant.writeConfigNamed(aiConnectionsFile, data, nil); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://localhost", nil)
	r.Header.Set(aiConnectionHeader, connection.ID)
	account, err := child.accountForConnection(r)
	if err != nil {
		t.Fatal(err)
	}
	if account == s.codex {
		t.Fatal("named account reused legacy profile")
	}
	run(account, 2)
	token, err := s.sessions.create("fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	response := providerRequest(s, "GET", "/api/model-providers", token, "", "")
	if response.Code != 200 || strings.Contains(strings.ToLower(response.Body.String()), "accounting") || strings.Contains(response.Body.String(), "fixture-thread") {
		t.Fatal("private accounting exposed through HTTP")
	}
	var injected Config
	if err := json.Unmarshal([]byte(`{"CodexAccounting":{},"codexAccounting":{}}`), &injected); err != nil || injected.CodexAccounting != nil {
		t.Fatal("JSON configuration can inject accounting")
	}
	// Nil is the default, including for a normal production constructor.
	cfg.parent = nil
	cfg.ProjectDir = t.TempDir()
	cfg.DeskConfigDir = t.TempDir()
	cfg.CodexAccounting = nil
	plain, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	if plain.cfg.CodexAccounting != nil {
		t.Fatal("accounting enabled by default")
	}
	before := sink.Dropped()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := plain.codex.(providerRunner).Run(ctx, codexbridge.Owner{}, codexbridge.RunRequest{}, func(codexbridge.RunEvent) error { return nil }, func(context.Context, codexbridge.ToolCall) (codexbridge.ToolAnswer, error) {
		return codexbridge.ToolAnswer{}, nil
	}); err == nil {
		t.Fatal("invalid run accepted")
	}
	select {
	case <-sink.Reports():
		t.Fatal("nil-sink constructor reported to another host sink")
	default:
	}
	if sink.Dropped() != before {
		t.Fatal("nil sink affected accounting")
	}
}
