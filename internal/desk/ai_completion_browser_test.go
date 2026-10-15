package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

// Tests Gemini output truncation, bounded synthesis and persisted safe diagnostics in both placements.
// Only local fixture responses are used; no provider is contacted.
func TestAICompletionBrowser(t *testing.T) {
	script := os.Getenv("JPS_COMPLETION_BROWSER")
	if script == "" {
		t.Skip("set JPS_COMPLETION_BROWSER after building web/dist")
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if call == 2 {
			time.Sleep(700 * time.Millisecond)
		}
		reason := "STOP"
		parts := []any{map[string]any{"text": "Recovery completed using retained work."}}
		if call == 1 || call == 3 || call == 4 {
			reason = "MAX_TOKENS"
			parts = []any{}
		}

		if call >= 7 {
			offset := 100000
			if call == 11 {
				offset = 0
			}
			parts = []any{map[string]any{"functionCall": map[string]any{"name": "list_context", "args": map[string]any{"offset": offset}}}}
			if call == 12 {
				parts = []any{map[string]any{"text": "The different action completed using retained results."}}
			}
		}
		b, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": parts}, "finishReason": reason}}, "usageMetadata": map[string]any{"promptTokenCount": 100, "candidatesTokenCount": 0, "thoughtsTokenCount": 8192, "totalTokenCount": 8292}})
		fmt.Fprintf(w, "data: %s\n\n", b)
	}))
	defer upstream.Close()
	s, ts := startDesk(t, Config{Static: os.DirFS("../../web/dist"), ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken, CodexBin: "off"})
	defer ts.Close()
	defer s.Close()
	writeDeskConfig(t, s, fmt.Sprintf(`{"deskConfigVersion":1,"assistant":{"engine":"vercel","thinking":"off","endpoint":{"url":%q,"kind":"gemini","models":["gemini-fixture"],"model":"gemini-fixture","tools":[]}}}`, upstream.URL))
	if code, body := storeKey(t, ts, testKey); code != http.StatusOK {
		t.Fatalf("key %d %s", code, body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", script, ts.URL, testToken)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser %v: %s", err, out)
	} else {
		t.Log(string(out))
	}
	if calls.Load() != 18 {
		t.Fatalf("got %d calls, want 18 (completion, retries, useful recovery and refused repetition)", calls.Load())
	}
}
