package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Exercises the real chat, SDK, checkpoint API and restored answer in a disposable
// Desk. All model responses come from this fixture; no external provider is used.
func TestChatQuestionBrowser(t *testing.T) {
	script := os.Getenv("JPS_QUESTION_BROWSER")
	if script == "" {
		t.Skip("set JPS_QUESTION_BROWSER after building web/dist")
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(delta any, finish any) {
			b, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		var request struct{ Messages []struct{ Content string } }
		_ = json.Unmarshal(body, &request)
		content := ""
		if len(request.Messages) > 0 {
			content = request.Messages[len(request.Messages)-1].Content
		}
		latest := content
		if offset := strings.LastIndex(content, "ANSWER TO DESK QUESTION"); offset >= 0 {
			latest = content[offset:]
		}
		if strings.Contains(latest, `"kind":"help"`) {
			emit(map[string]any{"content": "Operations owns the workflow; risk owns oversight. Choose the buyer who can sponsor your pilot."}, nil)
			emit(map[string]any{}, "stop")
		} else if strings.Contains(string(body), "ANSWER TO DESK QUESTION") {
			if !strings.Contains(string(body), "Regional teams") || !strings.Contains(string(body), "Do not start research") {
				t.Error("Continuation lost context")
			}
			emit(map[string]any{"content": "We will focus on operations leaders at regional teams. Research has not started."}, nil)
			emit(map[string]any{}, "stop")
		} else {
			q := map[string]any{"title": "Buyer", "question": "Who should we focus on as the first buyer?", "selection": "single", "options": []any{
				map[string]string{"id": "operations", "label": "Head of operations", "description": "Owns recurring exception approvals."},
				map[string]string{"id": "risk", "label": "Risk or compliance lead", "description": "Owns oversight and decision records."},
				map[string]string{"id": "it", "label": "IT or systems lead", "description": "Owns systems and integrations."},
			}, "recommendedOptionId": "operations", "recommendation": "A starting hypothesis for recurring exception approvals."}
			raw, _ := json.Marshal(q)
			emit(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "ask-1", "type": "function", "function": map[string]string{"name": "ask_user", "arguments": string(raw)}}}}, nil)
			emit(map[string]any{}, "tool_calls")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	s, ts := startDesk(t, Config{Static: os.DirFS("../../web/dist"), ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken})
	defer ts.Close()
	defer s.Close()
	writeDeskConfig(t, s, fmt.Sprintf(`{"deskConfigVersion":1,"assistant":{"engine":"vercel","thinking":"off","endpoint":{"url":%q,"kind":"openai-compatible","models":["fixture-model"],"model":"fixture-model","tools":[]}}}`, upstream.URL))
	if code, body := storeKey(t, ts, testKey); code != http.StatusOK {
		t.Fatalf("store: %d %v", code, body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", script, ts.URL, testToken)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %s %v", out, err)
	} else {
		t.Log(string(out))
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("model calls: got %d, want question, help and one answer continuation", got)
	}
}
