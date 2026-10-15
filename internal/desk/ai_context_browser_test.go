package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"sync/atomic"
	"testing"
	"time"
)

// Real Gemini adapter, shared main/pane chat, persisted restored draft and status
// animation. All inference is local fixture data, with no paid model calls.
func TestAIContextGeminiBrowser(t *testing.T) {
	script := os.Getenv("JPS_CONTEXT_BROWSER")
	if script == "" {
		t.Skip("set JPS_CONTEXT_BROWSER after building web/dist")
	}
	release := make(chan struct{}, 2)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 180000 {
			t.Error("unbounded context on Gemini wire")
		}
		call := calls.Add(1)
		if call <= 3 {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		if call >= 3 {
			part := map[string]any{"text": "The missing-size exception runs before the no-routing rule. No changes were saved."}
			if call < 22 {
				id := regexp.MustCompile(`[0-9a-f-]{36}:current-request:[0-9a-f]{16}`).FindString(string(raw))
				if id == "" {
					t.Error("current request reference missing")
				}
				part = map[string]any{"functionCall": map[string]any{"name": "read_context", "args": map[string]any{"id": id, "path": "/input", "limit": 256}}}
			} else {
				var request map[string]any
				_ = json.Unmarshal(raw, &request)
				if offered, ok := request["tools"].([]any); ok && len(offered) > 0 {
					t.Error("tools still offered on final request")
				}
				if !bytes.Contains(raw, []byte("This is the final request within this turn budget")) {
					t.Error("final answer instruction missing")
				}
			}
			w.Header().Set("Content-Type", "text/event-stream")
			body, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{part}}, "finishReason": "STOP"}}})
			fmt.Fprintf(w, "data: %s\n\n", body)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		body, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": "Gemini fixture response complete."}}}, "index": 0, "finishReason": "STOP"}}})
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
	handler := ts.Config.Handler
	ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/test/release" {
			release <- struct{}{}
			w.WriteHeader(204)
			return
		}
		handler.ServeHTTP(w, r)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", script, ts.URL, testToken)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser %v: %s", err, out)
	} else {
		t.Log(string(out))
	}
	if calls.Load() != 22 {
		t.Fatalf("got %d model requests", calls.Load())
	}
}
