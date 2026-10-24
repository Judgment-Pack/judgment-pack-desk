package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/turn"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

const assistantCriticDisposition = `{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}`
const assistantCriticProposal = "Proposed.\n\n```json\n{\"proposal\":{\"kind\":\"create\",\"document\":{\"id\":\"p\"},\"unknowns\":[]}}\n```"

func assistantCriticValue(t *testing.T, raw string) *runcontext.Value {
	t.Helper()
	v, err := assistantChatValue([]byte(raw))
	assistantRunMust(t, err)
	return v
}

func TestAssistantCriticCases(t *testing.T) {
	suite := assistantCriticValue(t, `{"cases":[
{"id":"same","origin":"manual","rationale":"Saved focus","row":{"id":"same","facts":{"number":4,"string":"4","zero":0,"false":false,"null":null},"expectedDisposition":`+assistantCriticDisposition+`,"expectedHandoffTarget":null,"evidenceAvailability":{"receipt":false},"supportedExtensions":[],"origin":"import","focus":"row focus","specSection":"section","cites":["citation"],"unknown":"omit"}},
{"id":"error","origin":"ai","rationale":"","row":{"id":"error","facts":{},"expectedErrorClass":"missing-fact","expectedErrorPhase":"evaluation"}},
{"id":"gone","origin":"manual","rationale":"","row":{"id":"gone","facts":{},"expectedDisposition":{"kind":"approve"}}},
{"id":"uncomparable","origin":"manual","rationale":"","row":{"id":"uncomparable","facts":"bad","expectedDisposition":{"kind":"approve"}}},
{"id":"no-kind","origin":"manual","rationale":"","row":{"id":"no-kind","facts":{},"expectedDisposition":{}}},
{"id":"exploratory","origin":"manual","rationale":"","row":{"id":"exploratory","facts":{}}}],"deleted":["gone"]}`)
	declared := assistantCriticValue(t, `[{"id":"same","facts":{"changed":true},"expectedDisposition":{"kind":"decline"}}, {"id":"declared","facts":{"missing":null},"expectedDisposition":{"kind":"approve"},"expectedHandoffTarget":false,"rationale":"Declared focus","expectationSource":"not instruction"}]`)
	rows, err := assistantCriticCases(declared, suite)
	assistantRunMust(t, err)
	raw, _ := json.Marshal(rows)
	parsed, err := turn.ReadCriticCases(raw)
	assistantRunMust(t, err)
	if len(parsed) != 3 || parsed[0].ID != "same" || parsed[0].Source != "test" || parsed[1].ID != "error" || parsed[2].ID != "declared" {
		t.Fatal(string(raw))
	}
	want := assistantCriticValue(t, `{"source":"test","id":"same","facts":{"number":4,"string":"4","zero":0,"false":false,"null":null},"expectedDisposition":`+assistantCriticDisposition+`,"expectedHandoffTarget":null,"evidenceAvailability":{"receipt":false},"supportedExtensions":[],"origin":"import","focus":"Saved focus","specSection":"section","cites":["citation"]}`)
	if got := assistantCriticValue(t, string(rows[0])); !bytes.Equal(canonicalApprovalJSON(got.JSON()), canonicalApprovalJSON(want.JSON())) || got.Get("facts").Has("absent") {
		t.Fatal("typed case or saved precedence changed", string(rows[0]))
	}
	if !bytes.Contains(rows[1], []byte(`"expectedErrorPhase":"evaluation"`)) || !bytes.Contains(rows[1], []byte(`"origin":"ai"`)) || bytes.Contains(raw, []byte("expectationSource")) {
		t.Fatal(string(raw))
	}
	many := runcontext.Array()
	for i := range 240 {
		many.Append(assistantCriticValue(t, fmt.Sprintf(`{"id":"n%d","facts":{},"expectedDisposition":{"kind":"approve"}}`, i)))
	}
	rows, err = assistantCriticCases(many, nil)
	assistantRunMust(t, err)
	if len(rows) != 200 || !bytes.Contains(rows[199], []byte(`"id":"n199"`)) {
		t.Fatal("published list limit changed", len(rows))
	}
	for _, raw := range []string{`{"cases":null}`, `{"cases":[{"id":"x","row":{"id":"y"},"origin":"manual","rationale":""}]}`, `{"cases":[],"deleted":[1]}`} {
		if _, err := assistantCriticCases(nil, assistantCriticValue(t, raw)); err == nil {
			t.Fatal("contradictory suite admitted", raw)
		}
	}
}

// Resave through authenticated routes before admitting the new run. Browser or
// model Runtime paths and restored checks never enter this fixture's authority.
func assistantCriticResave(t *testing.T, s *Server, token string, old *assistantRunExecution, review, phase string, linked bool, cases ...string) *assistantRunExecution {
	t.Helper()
	chat := assistantCriticValue(t, string(old.source.Original))
	chat.Set("interaction", runcontext.String(phase))
	if contains([]string{"off", "auto", "on"}, review) {
		chat.Set("critic", runcontext.String(review))
	} else if review != "absent" {
		chat.Set("adversarialReview", assistantCriticValue(t, review))
	}
	chat.Get("checkpoint").Get("state").Set("cases", assistantCriticValue(t, `[{"id":"case-one","facts":{"amount":"4","zero":0,"false":false,"null":null},"expectedDisposition":`+assistantCriticDisposition+`,"rationale":"Exact declared focus"}]`))
	if len(cases) != 0 {
		chat.Get("checkpoint").Get("state").Set("cases", assistantCriticValue(t, cases[0]))
	}
	chat.Get("checkpoint").Get("state").Set("execution", assistantCriticValue(t, `{"approval":{"state":"granted"},"grants":["historical-not-authority"],"deskCriticInputV1":"caller-value-not-authority"}`))
	chat.Get("checkpoint").Set(assistantCriticCheckpointMember, runcontext.String("caller-value-not-authority"))
	if linked {
		// Linked saved-pack context resolves its exact project declaration before
		// authoring; a browser link and saved file alone cannot establish it.
		assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, runtimeConfigName), []byte(`{"configVersion":"5","packs":{"p":{"path":"pack.json"}}}`), 0600))
		assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "pack.json"), []byte(`{"id":"p","title":"Saved target"}`), 0600))
		chat.Set("pack", assistantCriticValue(t, `{"id":"p","path":"pack.json","digest":"historic-link-digest"}`))
		saved := policyWorkspaceRead(t, s, token, "/api/pack-tests")
		if w := policyWorkspaceCall(s, token, "PUT", "/api/pack-tests", saved.SHA256, json.RawMessage(`{"version":1,"suites":{"p":{"revision":1,"cases":[{"id":"case-one","name":"Saved case","revision":1,"origin":"manual","rationale":"Saved suite focus","sources":[],"sourceMappings":{},"row":{"id":"case-one","facts":{"saved":"4","zero":0,"false":false,"null":null},"expectedDisposition":`+assistantCriticDisposition+`}}],"runs":[],"recovered":[]}}}`)); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	return assistantChatResave(t, s, token, old, chat.Text(), 201)
}

func assistantCriticRuntimeFixture(t *testing.T, s *Server, e *assistantRunExecution, support string) {
	t.Helper()
	assistantExecutionRuntimeFixture(t, s, e)
	peer := assistantExecutionPeer
	if support != "missing" {
		argument := "[]"
		if support == "required" {
			argument = "[{'name':'pack','required':True}]"
		}
		peer = strings.Replace(peer, "{'name':'served','arguments':[]}", "{'name':'test_pack','arguments':"+argument+"},{'name':'served','arguments':[]}", 1)
	}
	peer = strings.Replace(peer, "'text':'SERVED-RUNTIME-PROMPT'", "'text':('RUNTIME-TEST-GUIDANCE' if name=='test_pack' else 'SERVED-RUNTIME-PROMPT')", 1)
	peer = strings.Replace(peer, "  mode=p.get('arguments',{}).get('fixture','')", `  if name=='experimental_evaluate':
   text='{"status":"evaluated","rehearsal":true}' if json.loads(p['arguments']['facts']).get('n') is False else '{"status":"evaluated","rehearsal":true,"disposition":`+assistantCriticDisposition+`}'
   result={'content':[{'type':'text','text':text}]}
   print(json.dumps({'jsonrpc':'2.0','id':q['id'],'result':result}),flush=True);continue
  mode=p.get('arguments',{}).get('fixture','')`, 1)
	assistantRunMust(t, os.WriteFile(s.cfg.JpackBin, []byte(peer), 0700))
}

func assistantCriticOpen(t *testing.T, s *Server, e *assistantRunExecution, support string) *assistantExecution {
	t.Helper()
	assistantCriticRuntimeFixture(t, s, e, support)
	x, err := e.openAssistantExecution(context.Background(), assistantExecutionPrompt{Name: "served"})
	assistantRunMust(t, err)
	t.Cleanup(x.Close)
	return x
}

func assistantCriticAPISetup(t *testing.T, s *Server) {
	t.Helper()
	registry, err := s.readAIRegistry()
	assistantRunMust(t, err)
	endpoint := registry.Connections[0].Assistant.Endpoint
	endpoint.Models, endpoint.CriticModel = []string{"shared-model", "child-model"}, "child-model"
	raw, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: registry.DefaultConnection, Connections: registry.Connections})
	assistantRunMust(t, err)
	assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
}

func assistantCriticSSE(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	value, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": text}, "finish_reason": nil}}})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", value)
	_, _ = io.WriteString(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":13,\"completion_tokens\":7,\"total_tokens\":20}}\n\ndata: [DONE]\n\n")
}

func TestAssistantCriticActualAPI(t *testing.T) {
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		var input struct{ Model string }
		_ = json.NewDecoder(r.Body).Decode(&input)
		text := assistantCriticProposal
		if input.Model == "child-model" {
			text = "Independent critic findings."
		}
		assistantCriticSSE(w, text)
	})
	s, token, old := assistantExecutionFixture(t, u.server.URL, "work")
	old.cancel()
	<-old.done
	assistantCriticAPISetup(t, s)
	e := assistantCriticResave(t, s, token, old, "true", "work", true)
	x := assistantCriticOpen(t, s, e, "served")
	before, err := x.Run.Gate.CallTool(context.Background(), "get_schema", json.RawMessage(`{}`))
	assistantRunMust(t, err)
	if before.IsError {
		t.Fatal("initial completed reader failed")
	}
	priorReceipts := assistantExecutionRecord(t, e).Operations
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	arrivals := u.arrivals()
	if len(arrivals) != 2 || !bytes.Contains(arrivals[1].body, []byte(`"model":"child-model"`)) || !bytes.Contains(arrivals[1].body, []byte("RUNTIME-TEST-GUIDANCE")) {
		t.Fatal("named actual critic did not run", arrivals)
	}
	var body struct {
		Tools []struct{ Function struct{ Name string } }
	}
	assistantRunMust(t, json.Unmarshal(arrivals[1].body, &body))
	names := []string{}
	for _, tool := range body.Tools {
		names = append(names, tool.Function.Name)
	}
	if !reflect.DeepEqual(names, []string{"read_window", "search", "validate", "get_schema", "experimental_evaluate"}) {
		t.Fatal("critic offer expanded or lost its readers", names)
	}
	assistantCriticAssertResult(t, e, x, 2)
	if current := assistantExecutionRecord(t, e).Operations; len(current) < len(priorReceipts) || !reflect.DeepEqual(priorReceipts, current[:len(priorReceipts)]) {
		t.Fatal("critic discarded or rewrote completed physical receipts")
	}
	if len(x.Run.Cases) != 1 || !bytes.Contains(x.Run.Cases[0], []byte(`"source":"test"`)) || !bytes.Contains(x.Run.Cases[0], []byte(`"saved":"4"`)) || !bytes.Contains(arrivals[1].body, []byte("Saved suite focus")) {
		t.Fatal("actual critic lost authenticated saved-test precedence", x.Run.Cases)
	}
	for _, held := range assistantExecutionRecord(t, e).Events {
		if figures := held.Event.Critique; figures != nil && (figures.Figures.Requests != 1 || figures.Figures.InputTokens == nil || *figures.Figures.InputTokens != 13 || figures.Figures.OutputTokens == nil || *figures.Figures.OutputTokens != 7) {
			t.Fatal("critic reported usage lost", figures.Figures)
		}
	}
	if !bytes.Contains(arrivals[0].body, []byte("Review exact input")) || bytes.Contains(arrivals[1].body, []byte("Review exact input")) {
		t.Fatal("critic received parent transcript")
	}
}

func assistantCriticAssertResult(t *testing.T, e *assistantRunExecution, x *assistantExecution, used int64) {
	t.Helper()
	record := assistantExecutionRecord(t, e)
	if record.Checkpoint.Status != "complete" || record.Checkpoint.Used != used || record.Checkpoint.TestCalls == nil || *record.Checkpoint.TestCalls != 0 || x.gate.TestCalls() != 0 {
		t.Fatalf("critic checkpoint/count lost: %+v", record.Checkpoint)
	}
	if record.Checkpoint.Approval != nil || len(record.Checkpoint.Grants) != 0 || x.Run.Context.Approval != nil {
		t.Fatal("historical approval or allowance was restored")
	}
	var fingerprint string
	assistantRunMust(t, json.Unmarshal(record.Checkpoint.AdditionalMembers[assistantCriticCheckpointMember], &fingerprint))
	if !assistantRunDigest(fingerprint) || fingerprint != e.input.critic.fingerprint {
		t.Fatal("canonical snapshot lost host-bound critic custody")
	}
	critique, proposal, testResult, proposalRefused := false, false, false, false
	for _, held := range record.Events {
		event := held.Event
		if event.Critique != nil {
			critique = len(event.Critique.Cases) == 1 && event.Critique.Cases[0].Agrees && event.Critique.Cases[0].Case == "case-one" && !event.Critique.Refuted && len(event.Critique.Incomplete) == 0 && event.Critique.Figures != nil && event.Critique.Figures.Model == "child-model"
		}
		if event.Proposal != nil {
			proposal = event.Proposal.Critique != nil && !event.Proposal.Critique.Refuted
		}
		if event.Guardrail != nil && event.Guardrail.Tool == "proposal" && event.Guardrail.Action == "refused" {
			proposalRefused = true
		}
		if event.ToolResult != nil && event.ToolResult.Name == "experimental_evaluate" {
			testResult = !event.ToolResult.IsError
		}
		if event.ApprovalRequested != nil {
			t.Fatal("critic restored approval")
		}
	}
	linked, _, err := assistantSavedPackLink(e.source.Original)
	assistantRunMust(t, err)
	// The canonical critic still reviews the candidate. Linked saved packs
	// deliver proposals only through their checked host tool, so generic
	// artifact delivery must be explicitly refused after that review.
	if linked != "" {
		if proposal || !proposalRefused {
			t.Fatal("linked generic proposal escaped its canonical guard", record.Events)
		}
	} else if !proposal || proposalRefused {
		t.Fatal("canonical artifact proposal changed", record.Events)
	}
	if !critique || !testResult {
		t.Fatalf("canonical critic evidence missing: %+v", record.Events)
	}
	for _, op := range record.Operations {
		if op.State != "completed" || len(op.Result) == 0 {
			t.Fatalf("completed receipt lost: %+v", op)
		}
	}
}

func TestAssistantCriticActualCodex(t *testing.T) {
	s, token, old, _, trace := assistantCodexFixture(t, "critic", "work")
	old.cancel()
	<-old.done
	e := assistantCriticResave(t, s, token, old, "true", "work", false)
	// A hand-authored protocol extension still runs through the actual private
	// Manager. It responds to one author turn and one independent critic turn.
	peer := strings.Replace(assistantExecutionCodexPeer, " text='Fixture answer.'", " text="+strconvQuotePython(assistantCriticProposal)+" if index==1 else 'Independent critic findings.'", 1)
	peer = strings.Replace(peer, "  if mode in ['hang','discovery-wait']: continue", "  if mode=='critic': finish();continue\n  if mode in ['hang','discovery-wait']: continue", 1)
	assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
	x := assistantCriticOpen(t, s, e, "served")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	assistantCriticAssertResult(t, e, x, 0)
	if cp := assistantExecutionRecord(t, e).Checkpoint; cp.Unit != "provider-managed" || len(cp.Completions) != 0 || len(cp.Prompts) != 2 {
		t.Fatal("native invocation accounting invented model requests", cp)
	}
	generations := []map[string]json.RawMessage{}
	for _, row := range assistantCodexLog(t, trace) {
		if string(row["type"]) == `"generation"` {
			generations = append(generations, row)
		}
	}
	if len(generations) != 2 {
		t.Fatal("critic native invocations", len(generations))
	}
	var request struct {
		Model        string
		DynamicTools []struct{ Description string }
	}
	assistantRunMust(t, json.Unmarshal(generations[1]["request"], &request))
	if request.Model != "child-model" {
		t.Fatal("named native critic model", request.Model)
	}
	names := []string{}
	for _, tool := range request.DynamicTools {
		name, _, _ := strings.Cut(tool.Description, ":")
		names = append(names, name)
	}
	if !reflect.DeepEqual(names, []string{"validate", "get_schema", "experimental_evaluate", "read_window", "search"}) {
		t.Fatal("native critic offer", names)
	}
	var criticPrompt string
	assistantRunMust(t, json.Unmarshal(generations[1]["prompt"], &criticPrompt))
	if !strings.Contains(criticPrompt, "RUNTIME-TEST-GUIDANCE") || strings.Contains(criticPrompt, "Review exact input") {
		t.Fatal("native critic scope", criticPrompt)
	}
	// The published Agent carries the current saved effort into its named
	// critic. The physical request retains that effort and canonical phase.
	modelOps := 0
	for _, op := range assistantExecutionRecord(t, e).Operations {
		if op.Kind != "model" {
			continue
		}
		modelOps++
		var held adapter.AgentRequest
		assistantRunMust(t, json.Unmarshal(op.Intent, &held))
		if modelOps == 2 && (held.Phase != "critic" || held.Model != "child-model" || held.Effort != "medium") {
			t.Fatalf("critic phase/model/effort: %q %q %q", held.Phase, held.Model, held.Effort)
		}
	}
	var turnRequest struct {
		Effort string `json:"effort"`
	}
	assistantRunMust(t, json.Unmarshal(generations[1]["turn"], &turnRequest))
	if turnRequest.Effort != "medium" {
		t.Fatal("actual critic model effort", turnRequest.Effort)
	}
}

func strconvQuotePython(text string) string { raw, _ := json.Marshal(text); return string(raw) }

func TestAssistantCriticControlsAndSupport(t *testing.T) {
	for _, tc := range []struct {
		name, review, phase, support string
		proposal                     bool
		wantCalls                    int
	}{
		{"off", "false", "work", "served", true, 1},
		{"default", "absent", "work", "served", false, 1},
		{"explanation", "true", "work", "served", false, 1},
		{"plan", "true", "plan", "served", false, 1},
		{"missing-test-guidance", "true", "work", "missing", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				text := "Explanation only."
				if tc.proposal {
					text = assistantCriticProposal
				}
				assistantCriticSSE(w, text)
			})
			s, token, old := assistantExecutionFixture(t, u.server.URL, "work")
			e := assistantCriticResave(t, s, token, old, tc.review, tc.phase, false)
			x := assistantCriticOpen(t, s, e, tc.support)
			if tc.review == "false" || tc.phase == "plan" {
				if x.Run.TestPrompt != "" || len(x.Run.Cases) != 0 {
					t.Fatal("review-disabled run bound critic inputs")
				}
				for _, row := range assistantExecutionRuntimeLog(t, s) {
					if bytes.Contains(row["params"], []byte("test_pack")) {
						t.Fatal("review-disabled run fetched testing guidance")
					}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
			if len(u.arrivals()) != tc.wantCalls || x.gate.TestCalls() != 0 {
				t.Fatalf("extra critic or test request: model=%d want=%d tests=%d events=%+v", len(u.arrivals()), tc.wantCalls, x.gate.TestCalls(), assistantExecutionRecord(t, e).Events)
			}
			found := false
			for _, held := range assistantExecutionRecord(t, e).Events {
				if held.Event.Critique != nil {
					found = strings.Contains(held.Event.Critique.Text, "no test_pack prompt from the runtime") && len(held.Event.Critique.Checks) == 0
				}
				if tc.phase == "plan" && held.Event.Proposal != nil {
					t.Fatal("Plan proposed artifact")
				}
			}
			if (tc.support == "missing") != found {
				t.Fatal("missing Runtime testing support was hidden")
			}
		})
	}
	t.Run("required-request-argument", func(t *testing.T) {
		s, token, old := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
		e := assistantCriticResave(t, s, token, old, "true", "work", false)
		assistantCriticRuntimeFixture(t, s, e, "required")
		if x, err := e.openAssistantExecution(context.Background(), assistantExecutionPrompt{Name: "served"}); err == nil {
			x.Close()
			t.Fatal("invented required testing prompt argument")
		}
		for _, row := range assistantExecutionRuntimeLog(t, s) {
			if string(row["method"]) == `"prompts/get"` && bytes.Contains(row["params"], []byte("test_pack")) {
				t.Fatal("request-bearing test prompt fetched")
			}
		}
	})
}

func assistantCriticChangeSuite(t *testing.T, s *Server, token string) {
	t.Helper()
	saved := policyWorkspaceRead(t, s, token, "/api/pack-tests")
	if w := policyWorkspaceCall(s, token, "PUT", "/api/pack-tests", saved.SHA256, json.RawMessage(`{"version":1,"suites":{"p":{"revision":2,"cases":[],"runs":[],"recovered":[]}}}`)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestAssistantCriticSnapshotRefusesChange(t *testing.T) {
	for _, boundary := range []string{"runtime-suite", "model-suite", "model-pack", "reconstructed-suite"} {
		t.Run(boundary, func(t *testing.T) {
			u := newUpstream(t, nil)
			s, token, old := assistantExecutionFixture(t, u.server.URL, "work")
			e := assistantCriticResave(t, s, token, old, "true", "work", true)
			x := assistantCriticOpen(t, s, e, "served")
			before := len(assistantExecutionRuntimeLog(t, s))
			if boundary == "model-pack" {
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "pack.json"), []byte(`{"id":"p","title":"Changed target"}`), 0600))
			} else {
				assistantCriticChangeSuite(t, s, token)
			}
			if boundary == "runtime-suite" {
				_, err := x.Run.Gate.CallTool(context.Background(), "validate", json.RawMessage(`{}`))
				if err == nil {
					t.Fatal("stale suite crossed gated Runtime boundary")
				}
			} else if boundary == "reconstructed-suite" {
				continued := *e
				if next, err := continued.openAssistantExecution(context.Background(), assistantExecutionPrompt{Name: "served"}); err == nil {
					defer next.Close()
					response, err := next.Run.Model(context.Background(), "chat/completions", assistantExecutionRequest())
					if response != nil {
						_, _ = io.Copy(io.Discard, response.Body)
						_ = response.Body.Close()
					}
					if err == nil && e.ctx.Err() == nil {
						t.Fatal("reconstruction silently refreshed reviewed target")
					}
				}
			} else {
				response, err := x.Run.Model(context.Background(), "chat/completions", assistantExecutionRequest())
				if response != nil {
					_, _ = io.Copy(io.Discard, response.Body)
					_ = response.Body.Close()
				}
				if err == nil && e.ctx.Err() == nil {
					t.Fatal("stale target crossed model boundary")
				}
			}
			if len(u.arrivals()) != 0 || boundary != "reconstructed-suite" && len(assistantExecutionRuntimeLog(t, s)) != before {
				t.Fatal("stale snapshot dispatched")
			}
		})
	}
}

func TestAssistantCriticSharedTestCount(t *testing.T) {
	s, token, old := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	e := assistantCriticResave(t, s, token, old, "true", "work", false)
	x := assistantCriticOpen(t, s, e, "served")
	child, _, err := x.Run.Children(context.Background(), contract.DelegatedTaskProfile{ID: "fixture", RuntimeTools: []string{"experimental_test_cases"}}, func(contract.RunEvent) {})
	assistantRunMust(t, err)
	for range 2 {
		result, err := child.CallTool(context.Background(), "experimental_test_cases", json.RawMessage(`{"pack":"{}","matrix":"{}"}`))
		assistantRunMust(t, err)
		if !result.IsError {
			t.Fatal("ungranted child test dispatched")
		}
	}
	x.Run.Gate.BeginCritic()
	defer x.Run.Gate.EndCritic()
	for range 2 {
		result, err := x.Run.Gate.CallTool(context.Background(), "experimental_test_cases", json.RawMessage(`{"pack":"{}","matrix":"{}"}`))
		assistantRunMust(t, err)
		if !result.IsError {
			t.Fatal("ungranted critic test dispatched", result)
		}
	}
	if x.gate.TestCalls() != 2 {
		t.Fatal("critic changed the child's shared turn count", x.gate.TestCalls())
	}
}

func TestAssistantCriticQueuedSnapshot(t *testing.T) {
	for _, engine := range []string{"api", "codex"} {
		t.Run(engine, func(t *testing.T) {
			u := newUpstream(t, nil)
			var s *Server
			var token, trace string
			var old *assistantRunExecution
			if engine == "api" {
				s, token, old = assistantExecutionFixture(t, u.server.URL, "work")
			} else {
				s, token, old, _, trace = assistantCodexFixture(t, "discovery-wait", "work")
				// If the fence is removed, finish the observed generation so the
				// witness fails on outbound work, never on a fixture timeout.
				peer := strings.Replace(assistantExecutionCodexPeer, "  if mode in ['hang','discovery-wait']: continue", "  if mode=='hang': continue", 1)
				assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
			}
			e := assistantCriticResave(t, s, token, old, "true", "work", true)
			x := assistantCriticOpen(t, s, e, "served")
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			joined := make(chan struct{})
			releaseSlots := func() {}
			defer func() {
				cancel()
				releaseSlots()
				select {
				case <-joined:
				case <-time.After(5 * time.Second):
					t.Error("queued fixture worker did not stop during cleanup")
				}
			}()
			done := make(chan error, 1)
			if engine == "api" {
				releaseSlots = assistantFixtureRelayReservations(s.relaySlots)
				go func() {
					defer close(joined)
					response, err := x.Run.Model(ctx, "chat/completions", assistantExecutionRequest())
					if response != nil {
						_, _ = io.Copy(io.Discard, response.Body)
						_ = response.Body.Close()
					}
					done <- err
				}()
				deadline := time.Now().Add(3 * time.Second)
				for len(s.relayQueue) == 0 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if len(s.relayQueue) == 0 {
					t.Fatal("actual relay queue not reached")
				}
			} else {
				go func() {
					defer close(joined)
					done <- x.Run.Agent.Run(ctx, assistantCodexRequest(), adapter.AgentCallbacks{Event: func(context.Context, adapter.AgentEvent) error { return nil }, Tool: func(context.Context, adapter.AgentToolCall) (adapter.AgentToolAnswer, error) {
						return adapter.AgentToolAnswer{}, nil
					}})
				}()
				assistantCodexWait(t, trace+".waiting")
			}
			assistantCriticChangeSuite(t, s, token)
			if engine == "api" {
				releaseSlots()
			} else {
				assistantRunMust(t, os.WriteFile(trace+".release", []byte("release"), 0600))
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("changed suite did not stop queued dispatch")
			}
			if len(u.arrivals()) != 0 || e.ctx.Err() == nil {
				t.Fatal("stale critic input crossed physical dispatch")
			}
			if engine == "codex" {
				for _, row := range assistantCodexLog(t, trace) {
					if string(row["type"]) == `"generation"` {
						t.Fatal("stale suite reached native generation")
					}
				}
			}
		})
	}
}

func TestAssistantCriticTargetRefusal(t *testing.T) {
	for _, link := range []string{`null`, `{"id":"p","path":"../pack.json","digest":"old"}`, `{"id":"other","path":"pack.json","digest":"old"}`, `{"id":"p","path":"/runtime/pack.json","digest":"old"}`} {
		t.Run(link, func(t *testing.T) {
			s, token, old := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
			assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "pack.json"), []byte(`{"id":"p"}`), 0600))
			chat := assistantCriticValue(t, string(old.source.Original))
			chat.Set("adversarialReview", runcontext.Bool(true)).Set("pack", assistantCriticValue(t, link))
			e := assistantChatResave(t, s, token, old, chat.Text(), 201)
			assistantCriticRuntimeFixture(t, s, e, "served")
			if x, err := e.openAssistantExecution(context.Background(), assistantExecutionPrompt{Name: "served"}); err == nil {
				x.Close()
				t.Fatal("unbound target admitted")
			}
			if _, err := os.Stat(filepath.Join(s.cfg.ProjectDir, "execution-fixture.jsonl")); !os.IsNotExist(err) {
				t.Fatal("contradictory target started Runtime work", err)
			}
		})
	}
}

func TestAssistantCriticUncomparedAndMismatch(t *testing.T) {
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		text := assistantCriticProposal
		if bytes.Contains(raw, []byte("REFUTATION PASS")) {
			text = "Independent findings."
		}
		assistantCriticSSE(w, text)
	})
	s, token, old := assistantExecutionFixture(t, u.server.URL, "work")
	e := assistantCriticResave(t, s, token, old, "true", "work", false, `[{"id":"wrong","facts":{"n":0},"expectedDisposition":{"kind":"outcome","outcomeId":"decline"}}, {"id":"agrees","facts":{"n":"0"},"expectedDisposition":{"kind":"outcome","outcomeId":"approve"}}, {"id":"uncompared","facts":{"n":false},"expectedDisposition":{"kind":"outcome","outcomeId":"approve"}}]`)
	x := assistantCriticOpen(t, s, e, "served")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	found := false
	for _, held := range assistantExecutionRecord(t, e).Events {
		if critique := held.Event.Critique; critique != nil {
			found = critique.Refuted && len(critique.Cases) == 2 && !critique.Cases[0].Agrees && critique.Cases[1].Agrees && reflect.DeepEqual(critique.Incomplete, []string{"uncompared"})
		}
	}
	if !found || len(x.Run.Cases) != 3 || !bytes.Contains(x.Run.Cases[0], []byte(`"outcomeId":"decline"`)) {
		t.Fatal("contradictory outcome or untested case was rewritten", assistantExecutionRecord(t, e).Events)
	}
}

func TestAssistantCriticTargetBytes(t *testing.T) {
	s, token, old := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	e := assistantCriticResave(t, s, token, old, "true", "work", true)
	raw := []byte("{ \"id\":\"p\", \"threshold\":9007199254740993, \"rate\":1.234567890123456789 }\n")
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "pack.json"), raw, 0600))
	x := assistantCriticOpen(t, s, e, "served")
	if x.Run.Context.CurrentRequest.Input != "Review exact input" || e.input.critic.packDigest != digestOf(raw) {
		t.Fatal("opaque target bytes were converted into input or reserialized")
	}
	assistantRunMust(t, e.recheckAssistantCritic(context.Background()))
	actual, err := os.ReadFile(filepath.Join(s.cfg.ProjectDir, "pack.json"))
	assistantRunMust(t, err)
	if !bytes.Equal(actual, raw) {
		t.Fatal("critic rewrote target bytes")
	}
}

func TestAssistantCriticDurableCustody(t *testing.T) {
	for _, mode := range []string{"same", "suite", "pack", "missing", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			s, token, old := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
			e := assistantCriticResave(t, s, token, old, "true", "work", true)
			x := assistantCriticOpen(t, s, e, "served")
			x.Close()
			record := assistantExecutionRecord(t, e)
			// Exercise the published reader's actual JSON round trip, then discard
			// all in-memory input custody as the server does for a durable resume.
			wire, err := json.Marshal(record.Checkpoint)
			assistantRunMust(t, err)
			value, err := runcontext.ParseJSON(wire)
			assistantRunMust(t, err)
			cp, err := runcontext.ReadExecution(value)
			assistantRunMust(t, err)
			if !bytes.Equal(cp.AdditionalMembers[assistantCriticCheckpointMember], record.Checkpoint.AdditionalMembers[assistantCriticCheckpointMember]) {
				t.Fatal("published checkpoint dropped private consistency metadata")
			}
			if mode == "suite" {
				assistantCriticChangeSuite(t, s, token)
			}
			if mode == "pack" {
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "pack.json"), []byte(`{"id":"p","title":"Changed after checkpoint"}`), 0600))
			}
			if mode == "missing" {
				delete(cp.AdditionalMembers, assistantCriticCheckpointMember)
				assistantRunMust(t, e.Checkpoint(context.Background(), *cp))
			}
			if mode == "conflict" {
				cp.AdditionalMembers[assistantCriticCheckpointMember] = json.RawMessage(`"caller-value-not-authority"`)
				assistantRunMust(t, e.Checkpoint(context.Background(), *cp))
			}
			continued := *e
			continued.input = &assistantChatCustody{}
			continued.record = assistantExecutionRecord(t, e)
			next, err := continued.openAssistantExecution(context.Background(), assistantExecutionPrompt{Name: "served"})
			if next != nil {
				defer next.Close()
			}
			if mode == "same" {
				assistantRunMust(t, err)
				if next.Run.Context.Approval != nil || len(next.Run.Context.Execution.Grants) != 0 {
					t.Fatal("private consistency metadata restored authority")
				}
			} else if err == nil {
				t.Fatal("fresh custody silently reviewed a new target or accepted missing/conflicting resumed evidence")
			}
		})
	}
}

func TestAssistantCriticCanonicalCustody(t *testing.T) {
	s, token, old := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	e := assistantCriticResave(t, s, token, old, "true", "work", false)
	x := assistantCriticOpen(t, s, e, "served")
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`"caller-value-not-authority"`)} {
		cp := *e.record.Checkpoint
		cp.AdditionalMembers = map[string]json.RawMessage{}
		if raw != nil {
			cp.AdditionalMembers[assistantCriticCheckpointMember] = raw
		}
		x.Run.Emit(contract.RunEvent{ExecutionCheckpoint: &contract.RunEventExecutionCheckpoint{Type: "execution_checkpoint", Checkpoint: cp}})
		var fingerprint string
		assistantRunMust(t, json.Unmarshal(assistantExecutionRecord(t, e).Checkpoint.AdditionalMembers[assistantCriticCheckpointMember], &fingerprint))
		if fingerprint != e.input.critic.fingerprint {
			t.Fatal("canonical callback replaced private critic custody")
		}
	}
}

// Done is evaluated by the actual Runtime serialization select. The signal
// proves the call reached that wait before the suite changes; no sleep guesses.
type assistantCriticQueueContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *assistantCriticQueueContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestAssistantCriticQueuedRuntime(t *testing.T) {
	s, token, old := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	e := assistantCriticResave(t, s, token, old, "true", "work", true)
	x := assistantCriticOpen(t, s, e, "served")
	before := len(assistantExecutionRuntimeLog(t, s))
	x.runtime.serial <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wait := &assistantCriticQueueContext{Context: ctx, waiting: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := x.Run.Gate.CallTool(wait, "validate", json.RawMessage(`{}`)); done <- err }()
	select {
	case <-wait.waiting:
	case <-ctx.Done():
		<-x.runtime.serial
		t.Fatal("actual Runtime queue was not reached")
	}
	assistantCriticChangeSuite(t, s, token)
	<-x.runtime.serial
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stale suite crossed serialized Runtime dispatch")
		}
	case <-ctx.Done():
		t.Fatal("queued Runtime refusal did not settle")
	}
	if len(assistantExecutionRuntimeLog(t, s)) != before || e.ctx.Err() == nil {
		t.Fatal("stale suite reached physical Runtime or retained admission")
	}
}

// Explicit saved booleans retain their meaning; absence inherits canonical Auto.
func TestAssistantCriticReleasedControlMapping(t *testing.T) {
	for _, tc := range []struct {
		name   string
		review *bool
		want   string
	}{
		{"absent", nil, "auto"},
		{"off", new(false), "off"},
		{"on", new(true), "on"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chat := &assistantChatInput{Review: tc.review}
			if got, _ := runcontext.CriticMode(runcontext.CriticSettings{Critic: assistantCriticMode(chat), AdversarialReview: chat.Review}, runcontext.CriticTable{}); got != tc.want {
				t.Fatalf("critic control: got %q, want %q", got, tc.want)
			}
			if got, want := assistantCriticEnabled(chat, "work"), tc.want != "off"; got != want {
				t.Fatalf("work critic enabled: got %t, want %t", got, want)
			}
			if assistantCriticEnabled(chat, "plan") {
				t.Fatal("Plan enabled critic inputs")
			}
		})
	}
}
