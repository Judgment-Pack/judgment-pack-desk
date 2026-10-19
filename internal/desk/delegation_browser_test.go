package desk

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// Exercise the actual shared chat coordinator, persistence, UI and provider
// handoff against an exclusive scripted account. Never calls a paid provider.
func TestDelegationBrowser(t *testing.T) {
	script := os.Getenv("JPS_DELEGATION_BROWSER")
	if script == "" {
		t.Skip("set JPS_DELEGATION_BROWSER after building web/dist")
	}
	s, ts := startDesk(t, Config{Static: os.DirFS("../../web/dist"), ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken})
	defer ts.Close()
	defer s.Close()
	var active atomic.Bool
	var parents, children, continuations atomic.Int32
	stub := &recoveryBrowserProvider{}
	stub.run = func(ctx context.Context, _ codexbridge.Owner, r codexbridge.RunRequest, emit func(codexbridge.RunEvent) error, tool codexbridge.ToolHandler) error {
		if !active.CompareAndSwap(false, true) {
			return codexbridge.ErrBusy
		}
		defer active.Store(false)
		delegate := ""
		for _, offered := range r.Tools {
			if strings.HasPrefix(offered.Description, "delegate_tasks:") {
				delegate = offered.Name
			}
		}
		if delegate != "" {
			parents.Add(1)
			payload := `{"tasks":[{"title":"Review evidence","objective":"Review supplied evidence.","context":"Fixture evidence.","profile":"review"},{"title":"Check assumptions","objective":"Identify assumptions independently.","context":"Fixture assumptions.","profile":"analyze"}]}`
			if strings.Contains(r.Prompt, "Please test cancellation") {
				payload = `{"tasks":[{"title":"Long review","objective":"Wait for cancellation.","context":"CANCEL_CHILD","profile":"analyze"}]}`
			}
			_, err := tool(ctx, codexbridge.ToolCall{ID: strings.Repeat("a", 48), Name: delegate, Arguments: json.RawMessage(payload)})
			return err
		}
		if strings.Contains(r.Prompt, "PARENT CONTINUATION") {
			continuations.Add(1)
			if !strings.Contains(r.Prompt, "Retained child findings") {
				t.Error("parent did not receive retained findings")
			}
			return emit(codexbridge.RunEvent{Type: "message", ID: strings.Repeat("d", 48), Text: "The two delegated reviews are complete.", Phase: "final"})
		}
		children.Add(1)
		for _, offered := range r.Tools {
			if strings.HasPrefix(offered.Description, "propose_") || strings.HasPrefix(offered.Description, "request_dialect_research:") || strings.HasPrefix(offered.Description, "ask_user:") {
				t.Error("child received a parent-only tool")
			}
		}
		if strings.Contains(r.Prompt, "CANCEL_CHILD") {
			<-ctx.Done()
			return ctx.Err()
		}
		for _, offered := range r.Tools {
			if strings.HasPrefix(offered.Description, "list_dialects:") {
				if _, err := tool(ctx, codexbridge.ToolCall{ID: strings.Repeat("b", 48), Name: offered.Name, Arguments: json.RawMessage(`{}`)}); err != nil {
					return err
				}
				break
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1500 * time.Millisecond):
		}
		return emit(codexbridge.RunEvent{Type: "message", ID: strings.Repeat("c", 48), Text: "Retained child findings: the supplied context needs source review.", Phase: "final"})
	}
	if s.codex != nil {
		s.codex.Close()
	}
	s.codex = stub
	var config map[string]any
	json.Unmarshal([]byte(researchDeskFile("http://127.0.0.1:1")), &config)
	config["assistant"] = map[string]any{"engine": "codex", "agent": map[string]any{"provider": "openai", "authMethod": "subscription", "model": "model", "tools": []string{}}}
	raw, _ := json.Marshal(config)
	writeDeskConfig(t, s, string(raw))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", script, ts.URL, testToken)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %s %v", out, err)
	} else {
		t.Log(string(out))
	}
	if parents.Load() != 2 || continuations.Load() != 1 || children.Load() < 2 {
		t.Fatalf("unexpected dispatch: parents=%d children=%d continuations=%d", parents.Load(), children.Load(), continuations.Load())
	}
}
