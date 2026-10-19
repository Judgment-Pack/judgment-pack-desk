package desk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestPolicyManualRealMCPRefusesOperationWithJobsUnavailable(t *testing.T) {
	runtime := os.Getenv("JPACK_BIN")
	if runtime == "" {
		t.Skip("set JPACK_BIN for actual nonoperational MCP boundary fixture")
	}
	if !filepath.IsAbs(runtime) {
		t.Fatal("Runtime binary must be absolute")
	}
	f, _ := admissionPrepareFixture(t, "committed")
	f.s.cfg.JpackBin = runtime
	if err := os.MkdirAll(filepath.Join(f.s.projectDir, "packs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.s.projectDir, "packs", "literal.json"), []byte(reviewPack), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := `{"configVersion":"3","packs":{"literal":{"path":"packs/literal.json"}},"audit":{"dir":"audit"}}`
	if err := os.WriteFile(filepath.Join(f.s.projectDir, "jpack.json"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	jpackIn(t, runtime, f.s.projectDir, "packs", "lock", "--config", "jpack.json")
	records := func() int {
		raw, err := os.ReadFile(filepath.Join(f.s.projectDir, "audit", "evaluations.jsonl"))
		if os.IsNotExist(err) {
			return 0
		}
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, b := range raw {
			if b == '\n' {
				n++
			}
		}
		return n
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	open := func(s *Server) *rpcSession {
		ts := httptest.NewServer(s)
		t.Cleanup(ts.Close)
		token := policyTestSession(t, s)
		ws, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws", &websocket.DialOptions{Subprotocols: upgradeOffer(token), HTTPHeader: http.Header{}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ws.CloseNow() })
		ws.SetReadLimit(readLimit)
		rpc := &rpcSession{t, ctx, ws}
		rpc.initialize()
		return rpc
	}
	// Discriminating positive control: this locked project normally records an
	// operation through the unchanged unconfigured relay.
	old := open(f.s)
	old.call(10, "experimental_evaluate", map[string]any{"pack_id": "literal", "facts": "{}"})
	if records() != 1 {
		t.Fatal("baseline operational control did not record one audit")
	}
	old.ws.CloseNow()
	if _, err := enableAdmissionFixture(t, f, nil); err != nil {
		t.Fatal(err)
	}
	s, err := f.configured(t)
	if err != nil {
		t.Fatal(err)
	}
	if s.jobs != nil {
		t.Fatal("fixture must have Jobs unavailable")
	}
	rpc := open(s)
	rpc.send(map[string]any{"jsonrpc": "2.0", "id": 11, "method": "tools/call", "params": map[string]any{"name": "experimental_evaluate", "arguments": map[string]any{"pack_id": "literal", "facts": "{}"}}})
	for {
		_, raw, err := rpc.ws.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var reply struct {
			ID    int
			Error struct {
				Code int
				Data struct{ Code string }
			}
		}
		if json.Unmarshal(raw, &reply) != nil {
			t.Fatal("invalid relay reply")
		}
		if reply.ID != 11 {
			continue
		}
		if reply.Error.Code != -32003 || reply.Error.Data.Code != "policy-execution-required" {
			t.Fatal("operational call reached Runtime", string(raw))
		}
		break
	}
	rpc.call(12, "experimental_evaluate", map[string]any{"pack_id": "literal", "facts": "{}", "rehearsal": true})
	rpc.call(13, "get_schema", map[string]any{})
	if records() != 1 {
		t.Fatal("refused operation or rehearsal changed operational audit")
	}
}
