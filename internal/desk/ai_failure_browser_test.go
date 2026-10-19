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

// Tests real Gemini SDK errors, persisted recovery and both chat placements.
// Only local fixture responses are used; no provider is contacted.
func TestAIFailureBrowser(t *testing.T) {
	script := os.Getenv("JPS_FAILURE_BROWSER")
	if script == "" {
		t.Skip("set JPS_FAILURE_BROWSER after building web/dist")
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 1 || call == 3 {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("request-id", "req_fixture")
			status := 429
			if call == 3 {
				status = 401
			} else {
				w.Header().Set("Retry-After", "5")
			}
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":{"message":"PRIVATE_PROVIDER_SENTINEL","code":`+fmt.Sprint(status)+`}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		b, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": "Recovery completed using retained work."}}}, "finishReason": "STOP"}}})
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
	if calls.Load() != 3 {
		t.Fatalf("got %d calls, want 3 (no automatic replay)", calls.Load())
	}
}
