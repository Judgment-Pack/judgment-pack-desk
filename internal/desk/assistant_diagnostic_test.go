package desk

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

func assistantDiagnosticFixture(t *testing.T, mode string) (*Server, string, *codexbridge.Manager, string) {
	t.Helper()
	s, token, e, m, trace := assistantCodexFixture(t, mode, "work")
	e.cancel()
	<-e.done
	peer := strings.Replace(assistantExecutionCodexPeer, "elif method=='model/list':", "elif method=='account/logout':\n  with open(trace+'.signed-out','w') as f: f.write('yes')\n  reply({})\n elif method=='model/list':", 1)
	peer = strings.Replace(peer, "'account':{'type':'chatgpt','planType':'plus'}", "'account':None if os.path.exists(trace+'.signed-out') else {'type':'chatgpt','planType':'plus'}", 1)
	peer = strings.Replace(peer, "if mode=='delegate' and index==1:", "if mode=='prose': finish()\n  elif mode=='delegate' and index==1:", 1)
	assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
	assistantExecutionRuntimeFixture(t, s, e)
	s.assistantRunExecutor = nil
	return s, token, m, trace
}
func assistantDiagnosticCall(t *testing.T, s *Server, token, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+token)
	registry, err := s.readAIRegistry()
	assistantRunMust(t, err)
	r.Header.Set(aiConnectionHeader, testAIAlpha)
	r.Header.Set(aiRevisionHeader, registry.Connections[0].Revision)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func assistantDiagnosticCapture(t *testing.T, s *Server, token, id string) assistantRunStart {
	t.Helper()
	w := assistantDiagnosticCall(t, s, token, "POST", "/api/assistant-diagnostics/reference", map[string]string{"requestId": id})
	if w.Code != 200 {
		t.Fatalf("capture %d %s", w.Code, w.Body.String())
	}
	var reply assistantDiagnosticReference
	assistantRunMust(t, json.Unmarshal(w.Body.Bytes(), &reply))
	if reply.Start == nil {
		t.Fatal("no reference")
	}
	return *reply.Start
}
func TestAssistantDiagnosticActualNativeCheckpoint(t *testing.T) {
	s, token, _, trace := assistantDiagnosticFixture(t, "tool")
	runtime := os.Getenv("DESK_DIALECT_MAPPING_RUNTIME")
	if runtime == "" {
		t.Skip("pinned offline Runtime fixture is not configured")
	}
	s.cfg.JpackBin = runtime
	start := assistantDiagnosticCapture(t, s, token, "diagnostic-one")
	if len(assistantCodexLog(t, trace)) != 0 {
		t.Fatal("capture dispatched")
	}
	view := assistantProtocolView(t, assistantDiagnosticCall(t, s, token, "POST", "/api/assistant-runs", start), 201)
	complete := assistantProtocolWait(t, s, token, view.ID, "complete")
	result := policyCall(s, token, "GET", "/api/assistant-runs/"+view.ID+"/connection-diagnostic", nil)
	if result.Code != 200 || strings.TrimSpace(result.Body.String()) != `{"succeeded":true}` {
		t.Fatalf("diagnostic result %d %s checkpoint=%+v events=%+v", result.Code, result.Body.String(), complete.Checkpoint, complete.Events)
	}
	if complete.Checkpoint.Unit != "provider-managed" || complete.Checkpoint.Used != 0 || complete.Checkpoint.Limit != 1 || complete.Checkpoint.RequestCap != nil {
		t.Fatalf("native accounting %+v", complete.Checkpoint)
	}
	logs := assistantCodexLog(t, trace)
	generations := 0
	for _, row := range logs {
		var kind string
		_ = json.Unmarshal(row["type"], &kind)
		if kind != "generation" {
			continue
		}
		generations++
		var request struct {
			DynamicTools []struct {
				Description string `json:"description"`
			} `json:"dynamicTools"`
		}
		assistantRunMust(t, json.Unmarshal(row["request"], &request))
		if len(request.DynamicTools) != 1 || !strings.HasPrefix(request.DynamicTools[0].Description, "get_schema:") {
			t.Fatalf("open native offer %s", row["request"])
		}
	}
	if generations != 1 {
		t.Fatalf("generations %d", generations)
	}
	again := assistantProtocolView(t, assistantDiagnosticCall(t, s, token, "POST", "/api/assistant-runs", start), 200)
	if again.ID != view.ID {
		t.Fatal("duplicate admission")
	}
	lookup := assistantDiagnosticCall(t, s, token, "GET", "/api/assistant-diagnostics/requests/diagnostic-one", nil)
	if lookup.Code != 200 || !strings.Contains(lookup.Body.String(), view.ID) {
		t.Fatal("lost ACK lookup", lookup.Code, lookup.Body.String())
	}
	assistantRunClientJSON(t, "diagnostic-actual.json", complete)
	newSession := policyTestSession(t, s)
	if response := policyCall(s, newSession, "GET", "/api/assistant-runs/"+view.ID+"/connection-diagnostic", nil); response.Code != 404 {
		t.Fatal("new session inherited success", response.Code)
	}
	// The real private decoder and backup/restore path share the owner enum.
	store := s.assistantRuns()
	held, err := store.transaction(t.Context(), func(*assistantRunLedger) (bool, error) { return false, nil })
	assistantRunMust(t, err)
	raw, _ := json.Marshal(held)
	if _, err := decodeAssistantRunStorage(store.name, raw); err != nil {
		t.Fatal("strict private roundtrip", err)
	}
	backup := policyCall(s, token, "GET", "/api/storage/backup", nil)
	if backup.Code != 200 {
		t.Fatal("diagnostic backup", backup.Code, backup.Body.String())
	}
	restored := policyRestoreCall(t, s, token, filepath.Join(t.TempDir(), "restore"), backup.Body.Bytes())
	if restored.Code != 200 {
		t.Fatal("diagnostic restore", restored.Code, restored.Body.String())
	}
	historical := assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+view.ID, nil), 200)
	if !historical.Historical || historical.Owner.Kind != assistantDiagnosticPurpose || historical.Checkpoint.Limit != complete.Checkpoint.Limit || historical.Checkpoint.Used != 0 || historical.Checkpoint.RequestCap != nil {
		t.Fatal("restore lost native diagnostic checkpoint")
	}
	assistantRunClientJSON(t, "diagnostic-restored.json", historical)
	if response := policyCall(s, token, "GET", "/api/assistant-runs/"+view.ID+"/connection-diagnostic", nil); response.Code != 404 {
		t.Fatal("restored diagnostic became current success")
	}
}
func TestAssistantDiagnosticRejectsStaleAndExecutableReferences(t *testing.T) {
	for _, change := range []string{"model", "profile", "schema", "account", "runtime", "session"} {
		t.Run(change, func(t *testing.T) {
			s, token, m, _ := assistantDiagnosticFixture(t, "tool")
			start := assistantDiagnosticCapture(t, s, token, "stale-one")
			switch change {
			case "session":
				token = policyTestSession(t, s)
			case "account":
				assistantRunMust(t, m.Logout(t.Context()))
			case "profile":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, assistantProfilePath), []byte(`{"profileVersion":1,"codex":{"inherit":true},"projectInstructions":"changed"}`), 0600))
			case "runtime":
				s.cfg.JpackBin = "missing-diagnostic-runtime"
			default:
				registry, err := s.readAIRegistry()
				assistantRunMust(t, err)
				if change == "schema" {
					registry.Connections[0].Assistant.Agent.Tools = []string{"validate"}
				} else {
					model := "child-model"
					registry.Connections[0].Assistant.Agent.Model = &model
				}
				raw, err := encodeAIRegistry(registry.aiRegistry)
				assistantRunMust(t, err)
				assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
			}
			response := assistantDiagnosticCall(t, s, token, "POST", "/api/assistant-runs", start)
			if response.Code == 201 || response.Code == 200 {
				t.Fatalf("stale source dispatched %s", response.Body.String())
			}
		})
	}
	s, token, _, _ := assistantDiagnosticFixture(t, "tool")
	for _, body := range []any{map[string]any{}, map[string]any{"requestId": ""}, map[string]any{"requestId": strings.Repeat("x", 101)}, map[string]any{"requestId": "one", "prompt": "override"}, map[string]any{"requestId": "one", "model": "child-model"}, map[string]any{"requestId": "one", "tools": []string{"validate"}}} {
		if w := assistantDiagnosticCall(t, s, token, "POST", "/api/assistant-diagnostics/reference", body); w.Code != 400 {
			t.Fatal("executable/invalid source accepted", w.Code, w.Body.String())
		}
	}
}
func TestAssistantDiagnosticStopAndRestartNoReplay(t *testing.T) {
	s, token, _, trace := assistantDiagnosticFixture(t, "hang")
	start := assistantDiagnosticCapture(t, s, token, "stop-one")
	view := assistantProtocolView(t, assistantDiagnosticCall(t, s, token, "POST", "/api/assistant-runs", start), 201)
	deadline := time.Now().Add(4 * time.Second)
	for len(assistantCodexLog(t, trace)) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	current := assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+view.ID, nil), 200)
	stopped := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+view.ID+"/actions", assistantRunAction{ID: "stop", Kind: "cancel", Revision: current.Revision}), 200)
	if stopped.State != "stopped" {
		t.Fatal("not durably stopped")
	}
	assistantProtocolWait(t, s, token, view.ID, "stopped")
	before := len(assistantCodexLog(t, trace))
	again := assistantProtocolView(t, assistantDiagnosticCall(t, s, token, "POST", "/api/assistant-runs", start), 200)
	if again.ID != view.ID || len(assistantCodexLog(t, trace)) != before {
		t.Fatal("stopped run replayed")
	}
	result := policyCall(s, token, "GET", "/api/assistant-runs/"+view.ID+"/connection-diagnostic", nil)
	if strings.Contains(result.Body.String(), `true`) {
		t.Fatal("stopped success")
	}
}
func TestAssistantDiagnosticSchemaResponseBounds(t *testing.T) {
	for _, raw := range []string{"", `{}`, `{"result":{"content":[]}}`, `{"result":{"isError":true,"content":[{"type":"text","text":"bad"}]}}`, `{"result":{"content":[{"type":"text","text":""}]}}`, `{"error":{"message":"refused"}}`, strings.Repeat("x", 1<<20+1)} {
		if assistantDiagnosticSchemaResult([]byte(raw)) {
			t.Fatal("invalid schema succeeded")
		}
	}
}

func TestAssistantDiagnosticRefusesProseAndNonSchemaTools(t *testing.T) {
	for _, mode := range []string{"prose", "unoffered", "question", "budget", "malformed", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			s, token, _, _ := assistantDiagnosticFixture(t, mode)
			start := assistantDiagnosticCapture(t, s, token, "refused-one")
			run := assistantProtocolView(t, assistantDiagnosticCall(t, s, token, "POST", "/api/assistant-runs", start), 201)
			done := assistantProtocolWait(t, s, token, run.ID, "stopped")
			result := policyCall(s, token, "GET", "/api/assistant-runs/"+run.ID+"/connection-diagnostic", nil)
			if result.Code != 200 || strings.Contains(result.Body.String(), "true") {
				t.Fatal("unproven success", mode, result.Code, result.Body.String())
			}
			for _, held := range done.Events {
				if held.Event.Proposal != nil || held.Event.UserQuestion != nil || held.Event.ApprovalRequested != nil || held.Event.DelegationRequested != nil {
					t.Fatal("diagnostic gained domain/interaction rights", held)
				}
			}
			assistantRunClientJSON(t, "diagnostic-refused-"+mode+".json", done)
		})
	}
}
func TestAssistantDiagnosticLateCredentialMutationClosesSchemaDispatch(t *testing.T) {
	s, token, _, trace := assistantDiagnosticFixture(t, "callback-wait")
	start := assistantDiagnosticCapture(t, s, token, "late-one")
	run := assistantProtocolView(t, assistantDiagnosticCall(t, s, token, "POST", "/api/assistant-runs", start), 201)
	assistantCodexWait(t, trace+".waiting")
	auth := filepath.Join(filepath.Dir(trace), "account/profile/auth.json")
	assistantRunMust(t, os.WriteFile(auth, []byte(`{"fixture":"PRIVATE-REPLACED-ACCOUNT"}`), 0600))
	assistantRunMust(t, os.WriteFile(trace+".release", []byte("release"), 0600))
	stopped := assistantProtocolWait(t, s, token, run.ID, "stopped")
	for _, op := range stopped.Operations {
		if op.Kind == "runtime" && op.Name == "tools/call" {
			t.Fatal("stale account reached schema", op)
		}
	}
	result := policyCall(s, token, "GET", "/api/assistant-runs/"+run.ID+"/connection-diagnostic", nil)
	if result.Code == 200 && strings.Contains(result.Body.String(), "true") {
		t.Fatal("stale account became success")
	}
}
func TestAssistantDiagnosticSharedOwnerVector(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "assistant", "serverRuns", "fixtures", "diagnostic.json"))
	assistantRunMust(t, err)
	var view assistantRunView
	assistantRunMust(t, json.Unmarshal(raw, &view))
	store, binding, cp := assistantRunFixture(t)
	binding.Owner = view.Owner
	binding.RequestID = view.RequestID
	if binding.Owner.Kind != assistantDiagnosticPurpose || !aiIDPattern.MatchString(binding.Owner.ID) {
		t.Fatal("owner vector")
	}
	lease, err := store.Acquire(t.Context())
	assistantRunMust(t, err)
	defer lease.Close()
	record, created, err := lease.Admit(t.Context(), binding, view.AdmissionID, digestOf(raw), func() error { return nil })
	assistantRunMust(t, err)
	if !created {
		t.Fatal("vector not admitted")
	}
	writer, err := lease.Writer(t.Context(), binding, record.ID)
	assistantRunMust(t, err)
	assistantRunMust(t, writer.Checkpoint(t.Context(), cp))
	saved := assistantRunRaw(t, store)
	if _, err := decodeAssistantRunStorage(store.name, saved); err != nil {
		t.Fatal(err)
	}
	malformed := bytes.Replace(saved, []byte(`"kind":"connection-diagnostic"`), []byte(`"kind":"diagnostic"`), 1)
	if _, err := decodeAssistantRunStorage(store.name, malformed); err == nil {
		t.Fatal("unknown owner restored")
	}
}

func TestAssistantDiagnosticActualSchemaRefusals(t *testing.T) {
	for _, mode := range []string{"denied", "malformed", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			s, token, _, _ := assistantDiagnosticFixture(t, "tool")
			result := `{'isError':True,'content':[{'type':'text','text':'denied'}]}`
			if mode == "malformed" {
				result = `{}`
			}
			if mode == "oversized" {
				result = `{'content':[{'type':'text','text':'x'*(1024*1024+1)}]}`
			}
			peer := strings.Replace(assistantExecutionPeer, " answer={'jsonrpc':", " if name=='get_schema': result="+result+"\n answer={'jsonrpc':", 1)
			assistantRunMust(t, os.WriteFile(s.cfg.JpackBin, []byte(peer), 0700))
			start := assistantDiagnosticCapture(t, s, token, "schema-refused")
			run := assistantProtocolView(t, assistantDiagnosticCall(t, s, token, "POST", "/api/assistant-runs", start), 201)
			wait := 15 * time.Second
			if mode == "oversized" {
				// This actual response is retained in both the Runtime receipt and
				// canonical checkpoint. Strict ledger validation repeatedly decodes
				// that MiB payload; allow bounded processing time on CI runners.
				wait = 45 * time.Second
			}
			done := assistantProtocolWaitWithin(t, s, token, run.ID, "stopped", wait)
			if mode == "oversized" {
				receipts := 0
				for _, op := range done.Operations {
					if op.Kind == "runtime" && op.Name == "tools/call" && op.State == "completed" && len(op.Result) > 1<<20 {
						receipts++
					}
				}
				if receipts != 1 {
					t.Fatalf("oversized schema did not retain exactly one completed oversized Runtime receipt: %d", receipts)
				}
			}
			resultView := policyCall(s, token, "GET", "/api/assistant-runs/"+run.ID+"/connection-diagnostic", nil)
			if resultView.Code != 200 || strings.Contains(resultView.Body.String(), "true") {
				t.Fatal("invalid schema became success", resultView.Code, resultView.Body.String())
			}
		})
	}
}
func TestAssistantDiagnosticSessionEndFinalizesWithoutReplay(t *testing.T) {
	s, token, _, trace := assistantDiagnosticFixture(t, "hang")
	start := assistantDiagnosticCapture(t, s, token, "ended-one")
	run := assistantProtocolView(t, assistantDiagnosticCall(t, s, token, "POST", "/api/assistant-runs", start), 201)
	deadline := time.Now().Add(4 * time.Second)
	for len(assistantCodexLog(t, trace)) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	host := s.assistantRunService()
	host.mu.Lock()
	execution := host.active[run.ID]
	host.mu.Unlock()
	if execution == nil {
		t.Fatal("native run was not active")
	}
	s.sessions.revoke(token)
	select {
	case <-execution.done:
	case <-time.After(8 * time.Second):
		t.Fatal("session end did not settle")
	}
	stopped, err := execution.writer.lease.store.Read(t.Context(), execution.record.Binding, run.ID)
	assistantRunMust(t, err)
	if stopped.State != "stopped" || stopped.Checkpoint.Status != "stopped" {
		t.Fatal("session end left checkpoint active")
	}
	current := policyTestSession(t, s)
	if response := policyCall(s, current, "GET", "/api/assistant-runs/"+run.ID, nil); response.Code != 404 {
		t.Fatal("replacement session inherited private diagnostic", response.Code)
	}
	count := len(assistantCodexLog(t, trace))
	if response := assistantDiagnosticCall(t, s, current, "POST", "/api/assistant-runs", start); response.Code == 201 {
		t.Fatal("session end replayed")
	}
	if len(assistantCodexLog(t, trace)) != count {
		t.Fatal("session end dispatched")
	}
}

func TestAssistantDiagnosticNativeModelRefusalDoesNotGenerate(t *testing.T) {
	s, token, _, trace := assistantDiagnosticFixture(t, "tool")
	peer := filepath.Join(filepath.Dir(trace), "peer.py")
	raw, err := os.ReadFile(peer)
	assistantRunMust(t, err)
	changed := strings.Replace(string(raw), "for m in ['shared-model','child-model']", "for m in ['child-model']", 1)
	assistantRunMust(t, os.WriteFile(peer, []byte(changed), 0600))
	start := assistantDiagnosticCapture(t, s, token, "missing-model")
	run := assistantProtocolView(t, assistantDiagnosticCall(t, s, token, "POST", "/api/assistant-runs", start), 201)
	assistantProtocolWait(t, s, token, run.ID, "stopped")
	for _, row := range assistantCodexLog(t, trace) {
		var kind string
		_ = json.Unmarshal(row["type"], &kind)
		if kind == "generation" {
			t.Fatal("refused current model generated")
		}
	}
}

func TestAssistantDiagnosticServerRestartNeverReplays(t *testing.T) {
	s, token, _, trace := assistantDiagnosticFixture(t, "hang")
	start := assistantDiagnosticCapture(t, s, token, "restart-one")
	run := assistantProtocolView(t, assistantDiagnosticCall(t, s, token, "POST", "/api/assistant-runs", start), 201)
	deadline := time.Now().Add(4 * time.Second)
	for len(assistantCodexLog(t, trace)) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	assistantRunMust(t, s.Close())
	generations := len(assistantCodexLog(t, trace))
	restarted, err := New(s.cfg)
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = restarted.Close() })
	current := policyTestSession(t, restarted)
	if response := assistantDiagnosticCall(t, restarted, current, "POST", "/api/assistant-runs", start); response.Code == 200 || response.Code == 201 {
		t.Fatal("restart readmitted old diagnostic", response.Code)
	}
	ledger, err := restarted.assistantRuns().transaction(t.Context(), func(*assistantRunLedger) (bool, error) { return false, nil })
	assistantRunMust(t, err)
	found := false
	for _, held := range ledger.Runs {
		if held.ID == run.ID {
			found = true
			if !assistantRunTerminal(held.State) || held.Checkpoint.Status != "stopped" {
				t.Fatal("restart left owner active", held.State)
			}
		}
	}
	if !found || len(assistantCodexLog(t, trace)) != generations {
		t.Fatal("restart lost receipt or regenerated")
	}
}
