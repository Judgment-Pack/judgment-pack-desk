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

// Exercise the real proposal, save and pane handoff with a scripted provider.
// No user accounts, saved conversations or paid model calls are used.
func TestInputChatBrowser(t *testing.T) {
	script := os.Getenv("JPS_INPUT_CHAT_BROWSER")
	bundle := os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	if script == "" || bundle == "" {
		t.Skip("set JPS_INPUT_CHAT_BROWSER and JPS_TEST_GATEWAY_BUNDLE after building web/dist")
	}
	s, ts := startDesk(t, Config{Static: os.DirFS("../../web/dist"), ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken})
	s.cfg.LocalGatewayBundle = bundle
	defer ts.Close()
	defer s.Close()
	// Admission is real; the worker is stopped so no provider work can run.
	s.inputResearchCancel()
	<-s.inputResearchDone
	var calls atomic.Int32
	stub := &recoveryBrowserProvider{}
	stub.run = func(ctx context.Context, _ codexbridge.Owner, r codexbridge.RunRequest, emit func(codexbridge.RunEvent) error, tool codexbridge.ToolHandler) error {
		call := calls.Add(1)
		if call > 1 {
			name := ""
			for _, candidate := range r.Tools {
				if strings.HasPrefix(candidate.Description, "request_dialect_research:") {
					name = candidate.Name
				}
			}
			if name == "" {
				t.Error("research request tool missing")
				return codexbridge.ErrRun
			}
			p, err := s.openInputPreparations(false)
			if err != nil {
				return err
			}
			defer p.Close()
			f, err := p.Open(".")
			if err != nil {
				return err
			}
			defer f.Close()
			entries, err := f.ReadDir(-1)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				id := strings.TrimSuffix(entry.Name(), ".json")
				if !inputPreparationID.MatchString(id) {
					continue
				}
				saved, err := readInputPreparation(p, id)
				if err != nil {
					return err
				}
				args, _ := json.Marshal(map[string]string{"id": id, "revision": saved.SHA256})
				if _, err := tool(ctx, codexbridge.ToolCall{ID: strings.Repeat("e", 48), Name: name, Arguments: args}); err != nil {
					return err
				}
				return emit(codexbridge.RunEvent{Type: "message", ID: strings.Repeat("f", 48), Text: "Research request handled.", Phase: "final"})
			}
			t.Error("saved recipe missing")
			return codexbridge.ErrRun
		}
		name := ""
		for _, candidate := range r.Tools {
			if strings.HasPrefix(candidate.Description, "propose_dialect_recipe:") {
				name = candidate.Name
			}
		}
		if name == "" {
			t.Error("recipe proposal tool missing")
			return codexbridge.ErrRun
		}
		if _, err := tool(ctx, codexbridge.ToolCall{ID: strings.Repeat("c", 48), Name: name, Arguments: json.RawMessage(`{"name":"Linked company research","intent":"Find companies for review.","target":"companies","fields":[{"name":"description","description":"Business description","type":"string"}],"criteria":[],"limits":{"records":3,"searches":0,"pages":6,"modelCalls":15}}`)}); err != nil {
			return err
		}
		return emit(codexbridge.RunEvent{Type: "message", ID: strings.Repeat("d", 48), Text: "Your dialect recipe is ready for review. Research has not started.", Phase: "final"})
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", script, ts.URL, testToken)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %s %v", out, err)
	} else {
		t.Log(string(out))
	}
	if calls.Load() != 3 {
		t.Fatalf("unexpected model calls: %d", calls.Load())
	}
}
