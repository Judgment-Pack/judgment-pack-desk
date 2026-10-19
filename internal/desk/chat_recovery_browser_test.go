package desk

import (
	"context"
	"encoding/json"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type recoveryBrowserProvider struct{ agentStub }

func (a *recoveryBrowserProvider) Status(context.Context, string, bool) (codexbridge.Status, error) {
	return codexbridge.Status{Provider: "openai", AuthMethod: "subscription", Agent: "codex", Runtime: "available", Account: "connected"}, nil
}
func (a *recoveryBrowserProvider) Models(context.Context) ([]codexbridge.Model, error) {
	return []codexbridge.Model{{ID: "model", Name: "Model", Efforts: []string{"medium"}, DefaultEffort: "medium"}}, nil
}

// Actual Go socket, browser adapter and persisted transcript; no paid inference.
func TestChatRecoveryBrowser(t *testing.T) {
	script := os.Getenv("JPS_RECOVERY_BROWSER")
	if script == "" {
		t.Skip("set JPS_RECOVERY_BROWSER after building web/dist")
	}
	s, initial := startDesk(t, Config{Static: os.DirFS("../../web/dist"), ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken})
	defer initial.Close()
	defer s.Close()
	var calls atomic.Int32
	var tools atomic.Int32
	release := make(chan struct{}, 1)
	stub := &recoveryBrowserProvider{}
	stub.run = func(ctx context.Context, _ codexbridge.Owner, r codexbridge.RunRequest, emit func(codexbridge.RunEvent) error, tool codexbridge.ToolHandler) error {
		if calls.Add(1) > 1 {
			return emit(codexbridge.RunEvent{Type: "message", ID: strings.Repeat("d", 48), Text: "The unfinished response is complete. The earlier input list was kept.", Phase: "final"})
		}
		name := ""
		for _, candidate := range r.Tools {
			if strings.HasPrefix(candidate.Description, "list_dialects:") {
				name = candidate.Name
			}
		}
		if name == "" {
			t.Error("list_dialects was not offered")
			return codexbridge.ErrRun
		}
		tools.Add(1)
		if _, err := tool(ctx, codexbridge.ToolCall{ID: strings.Repeat("c", 48), Name: name, Arguments: json.RawMessage(`{}`)}); err != nil {
			return err
		}
		if err := emit(codexbridge.RunEvent{Type: "retry", Attempt: 1}); err != nil {
			return err
		}
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		return codexbridge.ErrRetries
	}
	if s.codex != nil {
		s.codex.Close()
	}
	s.codex = stub
	writeDeskConfig(t, s, `{"deskConfigVersion":1,"assistant":{"engine":"codex","agent":{"provider":"openai","authMethod":"subscription","model":"model","tools":[]}}}`)
	ts := initial
	ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/test/release" {
			release <- struct{}{}
			w.WriteHeader(204)
			return
		}
		s.ServeHTTP(w, r)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", script, ts.URL, testToken)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %s %v", out, err)
	} else {
		t.Log(string(out))
	}
	if calls.Load() != 2 || tools.Load() != 1 {
		t.Fatalf("runs %d tools %d", calls.Load(), tools.Load())
	}
}
