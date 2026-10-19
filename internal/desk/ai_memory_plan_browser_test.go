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

func TestAIMemoryPlanBrowser(t *testing.T) {
	script := os.Getenv("JPS_MEMORY_PLAN_BROWSER")
	if script == "" {
		t.Skip("set JPS_MEMORY_PLAN_BROWSER after building web/dist")
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		n := calls.Add(1)
		var request map[string]any
		json.Unmarshal(raw, &request)
		offered, _ := json.Marshal(request["tools"])
		if n <= 2 && (strings.Contains(string(offered), "remember_memory") || strings.Contains(string(offered), "request_dialect_research") || !strings.Contains(string(offered), "propose_plan")) {
			t.Error("invalid Plan capabilities", string(offered))
		}
		if n == 3 && strings.Contains(string(offered), "propose_plan") {
			t.Error("execution retained planning tool")
		}
		part := map[string]any{"text": "Plan ready for review."}
		reason := "STOP"
		if n == 1 {
			part = map[string]any{"functionCall": map[string]any{"name": "propose_plan", "args": map[string]any{"objective": "Prepare an outreach workflow", "steps": []string{"Inspect the dialect and pack", "Review the mapping before running"}, "questions": []string{}, "references": []any{}}}}
		}
		if n == 3 {
			part = map[string]any{"text": "The reviewed plan is now in work mode. Further actions retain their required approvals."}
			if !strings.Contains(string(raw), "Use concise outreach summaries.") {
				t.Error("approved memory not injected in execution")
			}
		}
		if n == 4 || n == 7 {
			key, text := "outreach-review", "Use bullet points for outreach reviews."
			if n == 7 {
				key, text = "intro-style", "Short introductions."
			}
			part = map[string]any{"functionCall": map[string]any{"name": "remember_memory", "args": map[string]any{"key": key, "text": text}}}
		}
		if n == 5 {
			part = map[string]any{"text": "The outreach preference is ready for memory review."}
		}
		if n == 6 {
			instructions, _ := json.Marshal(request["systemInstruction"])
			if !strings.Contains(string(instructions), "Use bullet points for outreach reviews.") {
				t.Error("chat-created memory missing from canonical recall")
			}
			part = map[string]any{"text": "Your approved outreach preferences are available."}
		}
		if n == 8 {
			part = map[string]any{"text": "Your explicit memory request has been saved."}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		body, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{part}}, "index": 0, "finishReason": reason}}})
		fmt.Fprintf(w, "data: %s\n\n", body)
	}))
	defer upstream.Close()
	s, ts := startDesk(t, Config{Static: os.DirFS("../../web/dist"), ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken, CodexBin: "off"})
	defer ts.Close()
	defer s.Close()
	writeDeskConfig(t, s, fmt.Sprintf(`{"deskConfigVersion":1,"assistant":{"engine":"vercel","thinking":"off","endpoint":{"url":%q,"kind":"gemini","models":["gemini-fixture"],"model":"gemini-fixture","tools":[]}}}`, upstream.URL))
	if code, body := storeKey(t, ts, testKey); code != http.StatusOK {
		t.Fatalf("store %d %s", code, body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", script, ts.URL, testToken)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("browser %v: %s", e, out)
	} else {
		t.Log(string(out))
	}
	if calls.Load() != 8 {
		t.Fatalf("got %d model requests", calls.Load())
	}
}
