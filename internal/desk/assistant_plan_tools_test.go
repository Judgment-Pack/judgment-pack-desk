package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

const assistantPlanCandidate = `{"objective":"Review exact Δ 💡","steps":["Read the saved pack","Review the Dialect before starting"],"questions":[],"references":[{"kind":"file","id":"one.json"},{"kind":"dialect","id":"prep_00000000000000000000000000000001"}]}`

func assistantPlanPrepareSource(t *testing.T) func(*Server, string, assistantRunStart) assistantRunStart {
	t.Helper()
	return func(s *Server, token string, start assistantRunStart) assistantRunStart {
		assistantFixtureReviewOff(s)
		saved := policyWorkspaceRead(t, s, token, "/api/conversations")
		var doc conversationDocument
		assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
		chat, err := assistantTestObject(doc.Chats[0])
		assistantRunMust(t, err)
		for key, value := range map[string]any{"title": "Plan review", "composer": "", "pinned": false, "archived": false, "updatedAt": "2026-10-21T00:00:00Z", "mode": "draft", "view": "chat"} {
			chat[key] = assistantChatJSON(value)
		}
		doc.Chats[0] = assistantChatEncodeObject(chat)
		if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		saved = policyWorkspaceRead(t, s, token, "/api/conversations")
		assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
		start.Revision = digestOf(doc.Chats[0])
		return start
	}
}

func assistantPlanSeed(t *testing.T, s *Server) inputPreparationReply {
	t.Helper()
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte("{ \"number\":1.20, \"literal\":\"50\" }\n"), 0600))
	return assistantRecipeSeed(t, s)
}
func assistantPlanInvoke(t *testing.T, e *assistantRunExecution, call string, root bool, raw string) runcontext.ToolResult {
	t.Helper()
	args := json.RawMessage(raw)
	assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &call, Name: assistantPlanTool, Args: args}}))
	ctx := context.WithValue(runcontext.WithCallID(t.Context(), call), assistantHostRootKey{}, root)
	// Use a fresh registry to prove custody is execution-owned, not closure state.
	result, err := e.assistantPlanTools()[0].Execute(ctx, args)
	assistantRunMust(t, err)
	assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolResult: &contract.ToolResult{Type: "tool_result", CallID: &call, Name: assistantPlanTool, Text: result.Content[0].Text(), IsError: result.Err()}}))
	return result
}
func assistantPlanComplete(t *testing.T, e *assistantRunExecution) assistantRunRecord {
	t.Helper()
	record := assistantExecutionRecord(t, e)
	cp := *record.Checkpoint
	cp.Status = "complete"
	assistantRunMust(t, e.Checkpoint(t.Context(), cp))
	assistantRunMust(t, e.writer.State(t.Context(), "complete"))
	return assistantExecutionRecord(t, e)
}
func assistantPlanAPI(t *testing.T, arguments []string) (*Server, string, *assistantRunExecution, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1))
		finish := "stop"
		delta := map[string]any{"role": "assistant", "content": "Review the saved Plan. No work has started."}
		if n <= len(arguments) {
			finish = "tool_calls"
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "plan-call", "type": "function", "function": map[string]any{"name": assistantPlanTool, "arguments": arguments[n-1]}}}}
		}
		raw := assistantChatJSON(map[string]any{"id": "plan-fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: {\"id\":\"plan-fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", raw, finish)
	})
	s, token, e := assistantExecutionFixtureSource(t, u.server.URL, "plan", nil, assistantPlanPrepareSource(t))
	assistantPlanSeed(t, s)
	x := assistantExecutionOpen(t, s, e)
	x.Run.Critic = "off"
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	return s, token, e, &calls
}

func TestAssistantPlanActualProducersAndProjection(t *testing.T) {
	for _, engine := range []string{"api", "codex"} {
		t.Run(engine, func(t *testing.T) {
			var s *Server
			var token string
			var e *assistantRunExecution
			var calls *atomic.Int32
			if engine == "api" {
				first := strings.Replace(assistantPlanCandidate, `"questions":[]`, `"questions":["Which review?"]`, 1)
				s, token, e, calls = assistantPlanAPI(t, []string{first, assistantPlanCandidate})
				if calls.Load() != 3 {
					t.Fatal("actual Plan loop did not complete", calls.Load())
				}
			} else {
				var trace string
				s, token, e, _, trace = assistantCodexSourceFixture(t, "normal", "plan", testAIAlpha, `{"profileVersion":1,"codex":{"inherit":true}}`, assistantPlanPrepareSource(t))
				assistantPlanSeed(t, s)
				peer := strings.Replace(assistantExecutionCodexPeer, "else: tool(offered('get_schema'),{})", "else: tool(offered('propose_plan'),json.loads("+fmt.Sprintf("%q", assistantPlanCandidate)+"))", 1)
				assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
				x := assistantExecutionOpen(t, s, e)
				x.Run.Critic = "off"
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				defer cancel()
				assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
			}
			record := assistantExecutionRecord(t, e)
			want := 1
			if engine == "api" {
				want = 2
			}
			if len(record.DomainOutputs) != want {
				t.Fatalf("Plan payload absent: %+v", record.DomainOutputs)
			}
			for _, out := range record.DomainOutputs {
				if out.Plan == nil || out.Plan.Status != "proposed" || out.Plan.AcceptedAt != "" || len(out.Plan.References) != 2 {
					t.Fatal("Plan was accepted or references lost", out)
				}
				var in assistantHostIntent
				assistantRunMust(t, readAssistantHostReceipt(assistantDialectOperation(record, out.OperationID).Intent, &in))
				if !in.Root || engine == "codex" && in.CallbackID == "" {
					t.Fatal("root/native custody absent")
				}
			}
			assistantRunMust(t, assistantRecipeImport(t, e, record))
			assistantRunMust(t, e.writer.State(t.Context(), "complete"))
			assistantRunMust(t, s.prepareAssistantChatProjection(e.request.Clone(t.Context()), record.ID))
			assistantRunMust(t, s.writeAssistantChatProjection(e.request.Clone(t.Context()), record.ID))
			// Lost acknowledgement repeats exact prepared bytes, never the host call.
			assistantRunMust(t, e.ProjectChat(t.Context()))
			assistantRunMust(t, e.ProjectChat(t.Context()))
			if calls != nil && calls.Load() != 3 {
				t.Fatal("projection replayed model work")
			}
			saved := policyWorkspaceRead(t, s, token, "/api/conversations")
			var doc conversationDocument
			assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
			plans, err := assistantPlanSource(doc.Chats[0])
			assistantRunMust(t, err)
			if len(plans) != want || plans[len(plans)-1].Status != "proposed" || want == 2 && (plans[0].Status != "superseded" || len(plans[0].Questions) != 1) {
				t.Fatal("Plan revision/history projection differs", plans)
			}
			if bytes.Contains(saved.Content, []byte(`"interaction":"work"`)) {
				t.Fatal("projection started work")
			}
			if dir := os.Getenv("DESK_PLAN_READER_OUTPUTS"); dir != "" {
				assistantRunMust(t, os.WriteFile(filepath.Join(dir, engine+".json"), saved.Content, 0600))
			}
		})
	}
}

func TestAssistantPlanStrictArgumentsAndSavedState(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{}`, `{"objective":"","steps":["x"],"questions":[],"references":[]}`, strings.Replace(assistantPlanCandidate, `"objective":`, `"objective":"duplicate","objective":`, 1), strings.Replace(assistantPlanCandidate, `"questions":[]`, `"questions":null`, 1), strings.Replace(assistantPlanCandidate, `"id":"one.json"`, `"id":"one.json","revision":"forged"`, 1), strings.Replace(assistantPlanCandidate, `"id":"one.json"`, `"ID":"one.json"`, 1), strings.Replace(assistantPlanCandidate, `"file"`, `"unknown"`, 1), strings.Replace(assistantPlanCandidate, `"one.json"`, `"../escape"`, 1), strings.Replace(assistantPlanCandidate, `"Review exact Δ 💡"`, `"   "`, 1), strings.Replace(assistantPlanCandidate, `"Review exact Δ 💡"`, `"`+strings.Repeat("💡", 1001)+`"`, 1)} {
		if _, err := readAssistantPlanArguments([]byte(raw)); err == nil {
			t.Fatal("malformed Plan argument accepted", raw)
		}
	}
	for _, raw := range []string{`{"interaction":null}`, `{"interaction":"future"}`, `{"interaction":""}`, `{"plans":null}`, `{"plans":{}}`} {
		if _, err := assistantPlanSource([]byte(raw)); err == nil {
			t.Fatal("malformed saved Plan state accepted", raw)
		}
	}
	a, err := readAssistantPlanArguments([]byte(assistantPlanCandidate))
	assistantRunMust(t, err)
	if validPlanText("\uFEFF", 2000) || !validPlanText("\u0085", 2000) {
		t.Fatal("Plan ECMAScript whitespace differs")
	}
	if !validPlanText(strings.Repeat("💡", 1000), 2000) || validPlanText(strings.Repeat("💡", 1001), 2000) {
		t.Fatal("Plan UTF16 boundary differs")
	}
	if len(a.References) != 2 {
		t.Fatal("reference vector differs")
	}
}

func TestAssistantPlanCurrentReferences(t *testing.T) {
	s, _, _ := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
	saved := assistantPlanSeed(t, s)
	source := []byte(`{"pack":{"path":"one.json"},"graph":{"path":"one.json"},"input":{"id":"prep_00000000000000000000000000000001"}}`)
	refs, err := assistantPlanReferences(source, []assistantPlanReference{{"file", "one.json"}})
	assistantRunMust(t, err)
	if len(refs) != 2 || refs[0].ID != "one.json" || refs[1].ID != saved.Document.ID {
		t.Fatal("linked reference identity changed", refs)
	}
	view, err := s.assistantPlanView(t.Context(), refs)
	assistantRunMust(t, err)
	resolved, err := s.resolveAssistantPlanReferences(refs, view)
	assistantRunMust(t, err)
	assistantRunMust(t, view.Close())
	if resolved[1].Revision != saved.SHA256 {
		t.Fatal("Dialect revision replaced by model prose")
	}
	plan := chatPlan{References: resolved}
	assistantRunMust(t, s.checkCurrentAssistantPlan(source, plan))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte("changed"), 0600))
	if s.checkCurrentAssistantPlan(source, plan) == nil {
		t.Fatal("changed file accepted")
	}
	if _, err := assistantPlanReferences([]byte(`{"pack":{"path":"missing.json"}}`), nil); err != nil {
		t.Fatal("shape check should not pretend a file exists", err)
	}
	if s.checkCurrentAssistantPlan([]byte(`{"pack":{"path":"missing.json"}}`), plan) == nil {
		t.Fatal("changed linked identity accepted")
	}
}

func TestAssistantPlanPhaseAndPolicyOffers(t *testing.T) {
	for _, phase := range []string{"plan", "work"} {
		for _, policy := range []bool{false, true} {
			var s *Server
			var e *assistantRunExecution
			if policy {
				s, _, e, _, _ = assistantPolicyFixture(t, "http://127.0.0.1:1", phase, false, nil)
			} else {
				s, _, e = assistantExecutionFixture(t, "http://127.0.0.1:1", phase)
			}
			x := assistantExecutionOpen(t, s, e)
			found := false
			for _, tool := range x.Run.HostTools {
				if tool.Name == assistantPlanTool {
					found = true
					if tool.Effect != "plan" || tool.Delegation != "" {
						t.Fatal("Plan authority widened")
					}
				}
				if policy && !assistantPolicyTool(tool.Name) && tool.Name != assistantPlanTool {
					t.Fatal("Policy registry widened", tool.Name)
				}
			}
			if found != (phase == "plan") {
				t.Fatal("wrong phase Plan offer", phase, policy)
			}
		}
	}
	tool := (&assistantRunExecution{record: assistantRunRecord{Binding: assistantRunBinding{Phase: "plan"}}}).assistantPlanTools()[0]
	for _, change := range []func(*runcontext.HostTool){func(t *runcontext.HostTool) { t.Effect = "proposal" }, func(t *runcontext.HostTool) { t.Delegation = "read" }, func(t *runcontext.HostTool) { t.ContextPolicy = &runcontext.ToolContextPolicy{Reference: true} }, func(t *runcontext.HostTool) { t.Approval = "never" }} {
		copy := tool
		change(&copy)
		if _, err := newAssistantHostRegistry([]runcontext.HostTool{copy}); err == nil {
			t.Fatal("widened Plan declaration accepted")
		}
	}
}

func TestAssistantPlanNoChildProposal(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
	assistantPlanSeed(t, s)
	if !assistantPlanInvoke(t, e, "child-plan", false, assistantPlanCandidate).Err() || len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
		t.Fatal("child proposed Plan")
	}
}

func TestAssistantPlanRecoveryCustodyMembers(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
	assistantPlanSeed(t, s)
	if result := assistantPlanInvoke(t, e, "plan-one", true, assistantPlanCandidate); result.Err() {
		t.Fatal(result.Value().Text())
	}
	r := assistantExecutionRecord(t, e)
	assistantRunMust(t, assistantRecipeImport(t, e, r))
	ctx := context.WithValue(runcontext.WithCallID(t.Context(), "plan-one"), assistantHostRootKey{}, true)
	replay, err := e.assistantPlanTools()[0].Execute(ctx, json.RawMessage(assistantPlanCandidate))
	assistantRunMust(t, err)
	if replay.Err() || len(assistantExecutionRecord(t, e).DomainOutputs) != 1 {
		t.Fatal("completed Plan replay changed custody")
	}
	// Every new private Plan member is mandatory and survives checkpoint/import.
	for _, member := range []string{"id", "revision", "objective", "steps", "questions", "references", "status", "createdAt"} {
		changed := assistantVariantClone(t, r)
		fields, _ := assistantTestObject(assistantChatJSON(changed.DomainOutputs[0].Plan))
		delete(fields, member)
		raw, _ := json.Marshal(assistantRunLedger{Version: 1, Project: r.Binding.Project, Runs: []assistantRunRecord{changed}})
		var doc map[string]json.RawMessage
		assistantRunMust(t, json.Unmarshal(raw, &doc))
		runs, _ := assistantChatArray(doc["runs"])
		run, _ := assistantTestObject(runs[0])
		outputs, _ := assistantChatArray(run["domainOutputs"])
		out, _ := assistantTestObject(outputs[0])
		out["plan"] = assistantChatEncodeObject(fields)
		outputs[0] = assistantChatEncodeObject(out)
		run["domainOutputs"] = assistantChatEncodeArray(outputs)
		runs[0] = assistantChatEncodeObject(run)
		doc["runs"] = assistantChatEncodeArray(runs)
		if _, err := decodeAssistantRunStorage(e.writer.lease.store.name, assistantChatEncodeObject(doc)); err == nil {
			t.Fatal("missing Plan member accepted", member)
		}
	}
	for _, member := range []string{"kind", "id", "revision"} {
		planFields, _ := assistantTestObject(assistantChatJSON(r.DomainOutputs[0].Plan))
		refs, _ := assistantChatArray(planFields["references"])
		ref, _ := assistantTestObject(refs[0])
		delete(ref, member)
		refs[0] = assistantChatEncodeObject(ref)
		planFields["references"] = assistantChatEncodeArray(refs)
		if validateChatPlan(assistantChatJSON(map[string]json.RawMessage{"plans": assistantChatEncodeArray([]json.RawMessage{assistantChatEncodeObject(planFields)})})) == nil {
			t.Fatal("missing reference member accepted", member)
		}
	}
	for _, member := range []string{"id", "revision", "createdAt"} {
		changed := assistantVariantClone(t, r)
		for i := range changed.Operations {
			if changed.Operations[i].Name == assistantPlanTool {
				fields, _ := assistantTestObject(changed.Operations[i].Intent)
				meta, _ := assistantTestObject(fields["plan"])
				delete(meta, member)
				fields["plan"] = assistantChatEncodeObject(meta)
				changed.Operations[i].Intent = assistantChatEncodeObject(fields)
			}
		}
		if assistantRecipeImport(t, e, changed) == nil {
			t.Fatal("missing Plan intent member accepted", member)
		}
	}
	for _, change := range []func(*assistantRunRecord){func(r *assistantRunRecord) { r.DomainOutputs = nil }, func(r *assistantRunRecord) { r.DomainOutputs[0].Kind = "graph" }, func(r *assistantRunRecord) { r.DomainOutputs[0].Plan.References[0].Revision = strings.Repeat("f", 64) }, func(r *assistantRunRecord) { r.DomainOutputs[0].Plan.Status = "accepted" }} {
		changed := assistantVariantClone(t, r)
		change(&changed)
		// Revisions remain structurally valid custody, but current-owner checks
		// must refuse forged exact revisions at projection/acceptance.
		if reflect.DeepEqual(changed.DomainOutputs, r.DomainOutputs) {
			t.Fatal("fixture mutation did not apply")
		}
		if len(changed.DomainOutputs) > 0 && changed.DomainOutputs[0].Plan != nil && changed.DomainOutputs[0].Plan.References[0].Revision == strings.Repeat("f", 64) {
			view, err := s.assistantPlanView(t.Context(), []assistantPlanReference{{"dialect", r.DomainOutputs[0].Plan.References[1].ID}})
			assistantRunMust(t, err)
			err = s.checkAssistantPlanProjection(e.source.Original, changed, view)
			view.Close()
			if err == nil {
				t.Fatal("forged current reference accepted")
			}
		} else if assistantRecipeImport(t, e, changed) == nil {
			t.Fatal("changed Plan custody accepted")
		}
	}
	if dir := os.Getenv("DESK_PLAN_READER_OUTPUTS"); dir != "" {
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, "input-schema.json"), assistantPlanSchema(), 0600))
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, "custody.json"), assistantChatJSON(r), 0600))
	}
}

func TestAssistantPlanSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/assistant-plan-v1.json")
	assistantRunMust(t, err)
	var vectors struct {
		Plans, Accepted, Started []chatPlan
		Arguments                json.RawMessage
	}
	assistantRunMust(t, json.Unmarshal(raw, &vectors))
	for _, plans := range [][]chatPlan{vectors.Plans, vectors.Accepted, vectors.Started} {
		assistantRunMust(t, validateChatPlan(assistantChatJSON(map[string]any{"interaction": "plan", "plans": plans})))
	}
	if _, err := readAssistantPlanArguments(vectors.Arguments); err != nil {
		t.Fatal(err)
	}
	malformed := assistantChatJSON(map[string]any{"plans": vectors.Plans})
	malformed = bytes.Replace(malformed, []byte(`"createdAt":`), []byte(`"acceptedAt":"","createdAt":`), 1)
	if validateChatPlan(malformed) == nil {
		t.Fatal("explicit empty saved acceptance timestamp accepted")
	}
}

func TestAssistantPlanRecoveryRootCustody(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
	assistantPlanSeed(t, s)
	if result := assistantPlanInvoke(t, e, "root-custody", true, assistantPlanCandidate); result.Err() {
		t.Fatal(result.Value().Text())
	}
	r := assistantExecutionRecord(t, e)
	for i := range r.Operations {
		op := &r.Operations[i]
		if op.Name != assistantPlanTool {
			continue
		}
		var in assistantHostIntent
		assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
		in.Root = false
		id := assistantPlanOperationID(in)
		op.ID, op.CallID = id, id
		op.Intent = assistantChatJSON(in)
		r.DomainOutputs[0].OperationID = id
	}
	if assistantRecipeImport(t, e, r) == nil {
		t.Fatal("child Plan custody imported")
	}
}

func TestAssistantPlanSavedContext(t *testing.T) {
	_, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
	raw, err := os.ReadFile("testdata/assistant-plan-v1.json")
	assistantRunMust(t, err)
	fields, err := assistantTestObject(raw)
	assistantRunMust(t, err)
	chat, err := assistantTestObject(e.source.Original)
	assistantRunMust(t, err)
	chat["plans"] = fields["plans"]
	source := e.source
	source.Original = assistantChatEncodeObject(chat)
	source.Binding.SourceDigests = []string{digestOf(source.Original)}
	source.Binding.Owner.Revision = digestOf(source.Original)
	compiled, err := compileAssistantChatInput(source)
	assistantRunMust(t, err)
	found := false
	for _, doc := range compiled.Documents {
		if doc.ID == "chat-plan-history" {
			found = true
			if !sameDialectJSON(doc.Value.JSON(), fields["plans"]) || doc.Kind != "history" {
				t.Fatal("Plan history changed or promoted to authority")
			}
		}
	}
	if !found {
		t.Fatal("saved Plan history absent from canonical context")
	}
}
