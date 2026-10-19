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

// Uses the real ChatGPT bridge callback, chat UI and checkpoint persistence.
// The account is scripted locally; no real model or source request is sent.
func TestCodexQuestionRecoveryBrowser(t *testing.T) {
	script := os.Getenv("JPS_CODEX_QUESTION_BROWSER")
	if script == "" {
		t.Skip("set JPS_CODEX_QUESTION_BROWSER after building web/dist")
	}
	s, ts := startDesk(t, Config{Static: os.DirFS("../../web/dist"), ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken})
	defer ts.Close()
	defer s.Close()
	var calls, corrected atomic.Int32
	stub := &recoveryBrowserProvider{}
	stub.run = func(ctx context.Context, _ codexbridge.Owner, r codexbridge.RunRequest, emit func(codexbridge.RunEvent) error, tool codexbridge.ToolHandler) error {
		calls.Add(1)
		if strings.Contains(r.Prompt, "ANSWER TO DESK QUESTION") {
			if !strings.Contains(r.Prompt, "Needs review") || !strings.Contains(r.Prompt, "Check the source date") {
				t.Error("answer context was lost")
			}
			return emit(codexbridge.RunEvent{Type: "message", ID: strings.Repeat("c", 48), Text: "The chosen answer is saved. Research has not started.", Phase: "final"})
		}
		name := ""
		for _, offered := range r.Tools {
			if strings.HasPrefix(offered.Description, "ask_user:") {
				name = offered.Name
			}
		}
		if name == "" {
			t.Error("question tool missing")
			return nil
		}
		question := map[string]any{"title": strings.Repeat("x", 41), "question": "How should overlapping employee ranges be handled?", "selection": "single", "options": []any{map[string]any{"id": "review", "label": "Needs review", "description": nil}, map[string]any{"id": "aside", "label": "Set aside", "description": ""}}, "recommendedOptionId": "review", "recommendation": "The evidence does not establish the target range."}
		raw, _ := json.Marshal(question)
		answer, err := tool(ctx, codexbridge.ToolCall{ID: strings.Repeat("a", 48), Name: name, Arguments: raw})
		if err != nil {
			return err
		}
		if !answer.IsError || !strings.Contains(answer.Text, "40 characters") || !strings.Contains(answer.Text, "call ask_user again") {
			t.Errorf("missing useful question correction feedback: %+v", answer)
		}
		corrected.Add(1)
		question["title"] = "Employee range"
		raw, _ = json.Marshal(question)
		_, err = tool(ctx, codexbridge.ToolCall{ID: strings.Repeat("b", 48), Name: name, Arguments: raw})
		return err
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
	if calls.Load() != 4 || corrected.Load() != 2 {
		t.Fatalf("calls=%d corrected=%d, want two asks and two explicit answers", calls.Load(), corrected.Load())
	}
}
