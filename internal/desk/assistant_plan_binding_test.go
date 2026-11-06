package desk

import (
	"bytes"
	"context"
	"encoding/json"
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

func assistantBindingAcceptCurrent(t *testing.T, s *Server, token string, start assistantRunStart) (assistantRunStart, chatPlan) {
	t.Helper()
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	fields, err := assistantTestObject(doc.Chats[0])
	assistantRunMust(t, err)
	plans, err := assistantPlanSource(doc.Chats[0])
	assistantRunMust(t, err)
	if len(plans) != 1 || plans[0].Status != "proposed" {
		t.Fatal("actual current proposal absent")
	}
	plan := plans[0]
	plan.Status, plan.AcceptedAt = "accepted", "2026-10-24T12:00:00Z"
	start.AdmissionID = "accepted-work"
	fields["interaction"], fields["plans"] = assistantChatJSON("work"), assistantChatJSON([]chatPlan{plan})
	fields["serverRequest"] = assistantChatJSON(map[string]string{"requestId": start.RequestID, "admissionId": start.AdmissionID})
	doc.Chats[0] = assistantChatEncodeObject(fields)
	if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved = policyWorkspaceRead(t, s, token, "/api/conversations")
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	start.Revision = digestOf(doc.Chats[0])
	return start, plan
}

func assistantBindingActiveArgs(cp *contract.Checkpoint) json.RawMessage {
	raw, _ := json.Marshal(cp)
	v := runcontext.MustParseJSON(string(raw))
	rows := runcontext.Array()
	for i, row := range v.Get("plan").Items() {
		status := "pending"
		if i == 0 {
			status = "in_progress"
		}
		rows.Append(runcontext.ObjectOf("id", row.Get("id"), "step", row.Get("step"), "status", runcontext.String(status)))
	}
	return runcontext.ObjectOf("explanation", runcontext.String("Inspect the reviewed input"), "plan", rows).JSON()
}

func assistantBindingFreshness(t *testing.T, s *Server, token string, start assistantRunStart, plan chatPlan) {
	t.Helper()
	r := assistantProtocolRequest(token)
	identity, _, err := s.assistantRunIdentity(r)
	assistantRunMust(t, err)
	source, err := s.readAssistantRunSource(r, start, identity, s.assistantRuns().project)
	assistantRunMust(t, err)
	for _, ref := range plan.References {
		pin := runcontext.PlanReference{Kind: ref.Kind, ID: ref.ID, Digest: ref.Digest, LegacyRevision: ref.Revision}
		_, err := runcontext.ResolvePlanReference(t.Context(), pin, *source.PlanResolver)
		assistantRunMust(t, err)
	}
	path := filepath.Join(s.cfg.ProjectDir, "one.json")
	before, err := os.ReadFile(path)
	assistantRunMust(t, err)
	assistantRunMust(t, os.WriteFile(path, []byte("changed after source selection"), 0600))
	defer func() { assistantRunMust(t, os.WriteFile(path, before, 0600)) }()
	ref := plan.References[0]
	_, err = runcontext.ResolvePlanReference(t.Context(), runcontext.PlanReference{Kind: ref.Kind, ID: ref.ID, Digest: ref.Digest, LegacyRevision: ref.Revision}, *source.PlanResolver)
	if err == nil {
		t.Fatal("live resolver reused selected digest")
	}
	if w := policyCall(s, token, "POST", "/api/assistant-runs", start); w.Code != 409 {
		t.Fatal("changed acceptance reached dispatch", w.Code, w.Body.String())
	}
}

func TestAssistantPlanBindingActualCurrentEvidence(t *testing.T) {
	for _, engine := range []string{"api", "codex"} {
		t.Run(engine, func(t *testing.T) {
			var s *Server
			var accepted chatPlan
			var working atomic.Bool
			var planCalls, workCalls atomic.Int32
			var operation string
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				if !working.Load() {
					if planCalls.Add(1) == 1 {
						assistantPolicySSE(w, assistantPlanTool, json.RawMessage(assistantPlanCandidate), "")
					} else {
						assistantBriefSSE(w, "Review the proposed Plan.", "stop")
					}
					return
				}
				n := workCalls.Add(1)
				ledger := assistantAuthoringLedger(t, s)
				cp := ledger.Runs[len(ledger.Runs)-1].Checkpoint
				switch n {
				case 1:
					assistantBindingSeedCheck(t, cp, accepted)
					assistantPolicySSE(w, "update_plan", assistantBindingActiveArgs(cp), "")
				case 2:
					assistantPolicySSE(w, "list_decisions", json.RawMessage(`{}`), "")
				case 3:
					for _, d := range cp.Durable.Dispatches {
						if d.Kind == "host" && d.Tool == "list_decisions" && d.State == "result" {
							operation = d.ID
						}
					}
					if operation == "" {
						t.Error("actual root workspace read acknowledgement absent")
					}
					raw, _ := json.Marshal(cp)
					v := runcontext.MustParseJSON(string(raw))
					args := runcontext.ObjectOf("id", v.Get("plan").Items()[0].Get("id"), "expectedRevision", v.Get("planState").Get("revision"), "outcome", runcontext.String("completed"), "evidence", runcontext.ObjectOf("operations", runcontext.Array(runcontext.String(operation)), "artifacts", runcontext.Array()))
					assistantPolicySSE(w, "complete_plan_step", json.RawMessage(args.JSON()), "")
				default:
					assistantBriefSSE(w, "Reviewed input inspected.", "stop")
				}
			})
			mode := ""
			if engine == "codex" {
				mode = "read"
			}
			var token, trace string
			var start assistantRunStart
			s, token, start, trace = assistantProductionFixture(t, u.server.URL, "plan", mode)
			assistantPlanSeed(t, s)
			if engine == "codex" {
				peer := strings.Replace(assistantExecutionCodexPeer, "else: tool(offered('get_schema'),{})", "else: tool(offered('propose_plan'),json.loads("+string(assistantChatJSON(assistantPlanCandidate))+"))", 1)
				assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
			}
			proposal := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
			defer func() {
				if !t.Failed() {
					return
				}
				if engine == "codex" {
					for _, row := range assistantCodexLog(t, trace) {
						if string(row["type"]) != `"thread"` && string(row["type"]) != `"generation"` {
							raw, _ := json.Marshal(row)
							t.Logf("native completion diagnostic: %s", raw)
						}
					}
				}
				for _, rec := range assistantAuthoringLedger(t, s).Runs {
					if rec.Checkpoint != nil {
						raw, _ := json.Marshal(rec.Checkpoint)
						v := runcontext.MustParseJSON(string(raw))
						t.Logf("completion state: run=%s status=%s plan=%s planState=%s", rec.ID, rec.Checkpoint.Status, v.Get("plan").JSON(), v.Get("planState").JSON())
						if rec.Checkpoint.Durable != nil {
							for _, d := range rec.Checkpoint.Durable.Dispatches {
								if d.Kind == "host" {
									t.Logf("completion dispatch: id=%s tool=%s scope=%+v state=%s failure=%v result=%s", d.ID, d.Tool, d.Scope, d.State, d.Failure != nil, d.Result)
								}
							}
							t.Logf("completion observations: %+v", rec.Checkpoint.Durable.Observations)
						}
					}
					for _, ev := range rec.Events {
						if ev.Event.ToolCall != nil {
							t.Logf("public tool call: %+v", *ev.Event.ToolCall)
						}
						if ev.Event.Guardrail != nil || ev.Event.PlanCompletionRefused != nil || ev.Event.ToolResult != nil && (ev.Event.ToolResult.Name == "update_plan" || ev.Event.ToolResult.Name == "complete_plan_step") {
							raw, _ := json.Marshal(ev.Event)
							t.Logf("completion event: %s", raw)
						}
					}
					for _, op := range rec.Operations {
						t.Logf("physical: kind=%s name=%s state=%s id=%s", op.Kind, op.Name, op.State, op.ID)
					}
				}
			}()
			assistantProtocolWaitWithin(t, s, token, proposal.ID, "complete", 60*time.Second)
			start, accepted = assistantBindingAcceptCurrent(t, s, token, start)
			assistantBindingFreshness(t, s, token, start, accepted)
			working.Store(true)
			if engine == "codex" {
				peer := assistantBindingNativeCompletionPeer(t)
				assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
			}
			initial := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
			// Both full-offer Work fixtures perform an update, a real read and
			// evidence completion. Observe function separately from unresolved
			// strict journal/checkpoint latency; this is not a performance SLO.
			// Proposal/shared/production limits remain unchanged.
			// See docs/design/assistant-canonical-run-store.md.
			wait := 180 * time.Second
			assistantProtocolWaitWithin(t, s, token, initial.ID, "complete", wait)
			record := canonicalActivationRecord(t, s, initial.ID)
			canonicalActivationCheck(t, record)
			if engine == "api" {
				if workCalls.Load() != 4 {
					t.Fatal("completion loop changed", workCalls.Load())
				}
			} else {
				found := false
				for _, row := range assistantCodexLog(t, trace) {
					if string(row["type"]) == `"accepted-before-request"` {
						var cp contract.Checkpoint
						assistantRunMust(t, json.Unmarshal(row["checkpoint"], &cp))
						assistantBindingSeedCheck(t, &cp, accepted)
						found = true
					}
				}
				if !found || workCalls.Load() != 0 {
					t.Fatal("actual native current lineage absent")
				}
				operation = assistantBindingNativeCompletionCheck(t, record, accepted, assistantCodexLog(t, trace))
			}
			raw, _ := json.Marshal(record.Checkpoint)
			v := runcontext.MustParseJSON(string(raw))
			first, second := v.Get("plan").Items()[0], v.Get("plan").Items()[1]
			if first.Get("status").Str() != "completed" || second.Get("status").Str() != "pending" || first.Get("evidence").Get("operations").Items()[0].Str() != operation || len(record.Checkpoint.Durable.Observations) == 0 {
				t.Fatal("exact acknowledged operation did not close only active step")
			}
		})
	}
}

// Exercise completion against the real filesystem RunWriter's acknowledged
// book. Negative probes alter copies of that book, never durable observations.
func TestAssistantPlanBindingOperationEvidenceRefusals(t *testing.T) {
	_, _, _, writer, record := canonicalActivationAdmitted(t)
	e := &assistantRunExecution{record: record, writer: writer}
	input := &runcontext.Input{Execution: record.Checkpoint, CurrentRequest: &runcontext.CurrentRequest{Message: "Review", Input: "exact original"}}
	assistantRunMust(t, e.bindCanonicalInput(t.Context(), input))
	file := filepath.Join(t.TempDir(), "observed.txt")
	assistantRunMust(t, os.WriteFile(file, []byte("Observed exact bytes"), 0600))
	opened, err := runcontext.ContextualSession(t.Context(), runcontext.Session{Prompt: "Review the exact saved input", AllowConversation: true, Interactive: true, Model: &runcontext.ModelSession{Family: "gemini", Model: "fixture"}, Context: input, HostTools: []runcontext.HostTool{{Name: "inspect", Effect: "read", Taxonomy: &runcontext.ToolTaxonomy{Operation: "read", Effect: "read"}, Description: "Read exact supplied file", InputSchema: json.RawMessage(`{"type":"object"}`), Execute: func(context.Context, json.RawMessage) (runcontext.ToolResult, error) {
		raw, err := os.ReadFile(file)
		return runcontext.ResultOf([]runcontext.Content{runcontext.TextContent(string(raw))}, nil), err
	}}}})
	assistantRunMust(t, err)
	defer opened.Context.Close()
	tool := func(name string) runcontext.HostTool {
		for _, h := range opened.Session.HostTools {
			if h.Name == name {
				return h
			}
		}
		t.Fatal("tool absent", name)
		return runcontext.HostTool{}
	}
	_, err = tool("update_plan").Execute(t.Context(), json.RawMessage(`{"explanation":"Inspect supplied file","plan":[{"step":"Inspect exact bytes","status":"in_progress"},{"step":"Report","status":"pending"}]}`))
	assistantRunMust(t, err)
	_, err = tool("inspect").Execute(t.Context(), json.RawMessage(`{}`))
	assistantRunMust(t, err)
	c := opened.Context.Execution
	proof := c.PlanProof
	book, scope := proof()
	operation := ""
	for _, d := range book.Dispatches {
		if d.Tool == "inspect" {
			operation = d.ID
		}
	}
	if operation == "" || len(book.Observations) == 0 {
		t.Fatal("actual host evidence absent")
	}
	for _, mode := range []string{"unknown-operation", "failed", "uncertain", "wrong-scope", "revision-changed", "inactive"} {
		t.Run(mode, func(t *testing.T) {
			raw, _ := json.Marshal(book)
			var altered runcontext.DurableBook
			assistantRunMust(t, json.Unmarshal(raw, &altered))
			id, revision, task := operation, c.State().PlanState.Get("revision").Str(), c.State().Plan.Items()[0].Get("id").Str()
			want := "unsuccessful-operation"
			for i := range altered.Dispatches {
				d := &altered.Dispatches[i]
				if d.ID != operation {
					continue
				}
				switch mode {
				case "failed":
					d.Result = json.RawMessage(`{"isError":true}`)
				case "uncertain":
					d.State = "unknown"
				case "wrong-scope":
					d.Scope.Role = "critic"
					want = "wrong-scope"
				}
			}
			switch mode {
			case "unknown-operation":
				id = "unobserved-operation"
				want = "unknown-operation"
			case "revision-changed":
				revision = "sha256:" + strings.Repeat("f", 64)
				want = "revision-changed"
			case "inactive":
				task = c.State().Plan.Items()[1].Get("id").Str()
				want = "active-step-required"
			}
			c.PlanProof = func() (*runcontext.DurableBook, runcontext.DurableScope) { return &altered, scope }
			args := runcontext.ObjectOf("id", runcontext.String(task), "expectedRevision", runcontext.String(revision), "outcome", runcontext.String("completed"), "evidence", runcontext.ObjectOf("operations", runcontext.Array(runcontext.String(id)), "artifacts", runcontext.Array()))
			_, err := c.CompletionTool().Execute(t.Context(), args.JSON())
			if err == nil || !strings.Contains(err.Error(), want) || c.State().Plan.Items()[0].Get("status").Str() != "in_progress" {
				t.Fatal("invalid completion evidence accepted", mode, err)
			}
		})
	}
	c.PlanProof = proof
	args := runcontext.ObjectOf("id", c.State().Plan.Items()[0].Get("id"), "expectedRevision", c.State().PlanState.Get("revision"), "outcome", runcontext.String("completed"), "evidence", runcontext.ObjectOf("operations", runcontext.Array(runcontext.String(operation)), "artifacts", runcontext.Array()))
	_, err = c.CompletionTool().Execute(t.Context(), args.JSON())
	assistantRunMust(t, err)
	if c.State().Plan.Items()[0].Get("status").Str() != "completed" || c.State().Plan.Items()[1].Get("status").Str() != "pending" {
		t.Fatal("current completion changed another task")
	}
}

// Install a handwritten historical proposal through the real saved-CAS human
// acceptance path. The submitted rows remain historical throughout execution.
func assistantBindingHistorical(t *testing.T, s *Server, token string, start assistantRunStart) (assistantRunStart, chatPlan) {
	t.Helper()
	prep := assistantPlanSeed(t, s)
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	fields, err := assistantTestObject(doc.Chats[0])
	assistantRunMust(t, err)
	file, err := os.ReadFile(filepath.Join(s.cfg.ProjectDir, "one.json"))
	assistantRunMust(t, err)
	plan := chatPlan{ID: "historical-plan", Revision: 1, Objective: "Review historical references", Steps: []string{"Read the saved inputs", "Report findings"}, Questions: []string{}, References: []chatPlanReference{{Kind: "file", ID: "one.json", Revision: digestOf(file)}, {Kind: "dialect", ID: prep.Document.ID, Revision: prep.SHA256}}, Status: "proposed", CreatedAt: "2026-10-21T12:00:00Z"}
	fields["interaction"], fields["plans"] = assistantChatJSON("plan"), assistantChatJSON([]chatPlan{plan})
	doc.Chats[0] = assistantChatEncodeObject(fields)
	if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved = policyWorkspaceRead(t, s, token, "/api/conversations")
	plan.Status, plan.AcceptedAt = "accepted", "2026-10-24T12:00:00Z"
	fields["interaction"], fields["plans"] = assistantChatJSON("work"), assistantChatJSON([]chatPlan{plan})
	doc.Chats[0] = assistantChatEncodeObject(fields)
	if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved = policyWorkspaceRead(t, s, token, "/api/conversations")
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	start.Revision = digestOf(doc.Chats[0])
	return start, plan
}

func assistantBindingSeedCheck(t *testing.T, cp *contract.Checkpoint, plan chatPlan) {
	t.Helper()
	if cp == nil || len(cp.Plan) != len(plan.Steps) {
		t.Fatal("accepted tasks absent before provider dispatch")
	}
	raw, _ := json.Marshal(cp)
	v, err := runcontext.ParseJSON(raw)
	assistantRunMust(t, err)
	a := v.Get("planState").Get("accepted")
	if a.Get("id").Str() != plan.ID || a.Get("sourceRevision").Num() != float64(plan.Revision) || a.Get("objective").Str() != plan.Objective || a.Get("acceptedAt").Str() != plan.AcceptedAt || runcontext.CanonicalJSON(a.Get("references")) != runcontext.CanonicalJSON(runcontext.MustParseJSON(string(assistantChatJSON(plan.References)))) {
		t.Fatal("original acceptance lineage changed", a)
	}
	ids := map[string]bool{}
	for i, task := range v.Get("plan").Items() {
		id := task.Get("id").Str()
		origin := task.Get("origin")
		if id == "" || ids[id] || task.Get("step").Str() != plan.Steps[i] || task.Get("status").Str() != "pending" || origin.Get("plan").Str() != plan.ID || origin.Get("revision").Str() != a.Get("revision").Str() || origin.Get("index").Num() != float64(i) {
			t.Fatal("Agent task identity/origin was not seeded", task)
		}
		ids[id] = true
	}
	for i, ref := range a.Get("validatedReferences").Items() {
		want := plan.References[i]
		if want.Revision != "" {
			if !strings.HasPrefix(ref.Get("id").Str(), "plan-legacy-") || ref.Get("digest").Str() != want.Revision || ref.Has("revision") {
				t.Fatal("historical pin mapping changed", ref)
			}
		} else if ref.Get("id").Str() != want.ID || ref.Get("digest").Str() != want.Digest {
			t.Fatal("current pin mapping changed", ref)
		}
	}
	if a.Get("validatedReferences").Len() != len(plan.References) {
		t.Fatal("validated pins missing")
	}
}

func TestAssistantPlanBindingActualHistorical(t *testing.T) {
	for _, engine := range []string{"api", "codex"} {
		t.Run(engine, func(t *testing.T) {
			var s *Server
			var plan chatPlan
			var calls atomic.Int32
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				ledger := assistantAuthoringLedger(t, s)
				assistantBindingSeedCheck(t, ledger.Runs[0].Checkpoint, plan)
				assistantBriefSSE(w, "Accepted scope retained.", "stop")
			})
			mode := ""
			if engine == "codex" {
				mode = "read"
			}
			var token, trace string
			var start assistantRunStart
			s, token, start, trace = assistantProductionFixture(t, u.server.URL, "work", mode)
			start, plan = assistantBindingHistorical(t, s, token, start)
			assistantBindingFreshness(t, s, token, start, plan)
			if engine == "codex" {
				// Capture the actual acknowledged checkpoint at native turn/start,
				// before the peer emits a tool or final response.
				peer := strings.Replace(assistantExecutionCodexPeer, "prompt=p['input'][0]['text']", "log({'type':'accepted-before-request','checkpoint':runs[-1]['checkpoint']})\n  prompt=p['input'][0]['text']", 1)
				assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
			}
			initial := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
			assistantProtocolWaitWithin(t, s, token, initial.ID, "complete", 60*time.Second)
			record := canonicalActivationRecord(t, s, initial.ID)
			canonicalActivationCheck(t, record)
			if engine == "api" && calls.Load() != 1 {
				t.Fatal("unexpected API dispatch", calls.Load())
			}
			if engine == "codex" {
				found := false
				for _, row := range assistantCodexLog(t, trace) {
					if string(row["type"]) == `"accepted-before-request"` {
						var cp contract.Checkpoint
						assistantRunMust(t, json.Unmarshal(row["checkpoint"], &cp))
						assistantBindingSeedCheck(t, &cp, plan)
						found = true
					}
				}
				if !found || calls.Load() != 0 {
					t.Fatal("native pre-request witness absent")
				}
			}
			accepted := assistantCheckpointAcceptedPlan(record.Checkpoint)
			if accepted == nil || !bytes.Contains(accepted.JSON(), []byte(`"revision":"`+plan.References[0].Revision+`"`)) {
				t.Fatal("saved historical acceptance rewritten")
			}
		})
	}
}

func TestAssistantPlanBindingLegacyAndMissingLineage(t *testing.T) {
	s, token, start, _ := assistantProductionFixture(t, "https://example.invalid", "work", "")
	start, _ = assistantBindingHistorical(t, s, token, start)
	r := assistantProtocolRequest(token)
	identity, _, err := s.assistantRunIdentity(r)
	assistantRunMust(t, err)
	source, err := s.readAssistantRunSource(r, start, identity, s.assistantRuns().project)
	assistantRunMust(t, err)
	cp, err := s.assistantRunExecutor.Prepare(t.Context(), source)
	assistantRunMust(t, err)
	if !assistantAcceptedPlanMatches(runcontext.MustParseJSON(string(source.Plan.Snapshot)), &cp) {
		t.Fatal("prepared acceptance absent")
	}
	for _, mode := range []string{"accepted", "legacy", "required", "durable", "no-source", "changed-plan"} {
		t.Run(mode, func(t *testing.T) {
			held := source
			clone := cp
			record := assistantRunRecord{Binding: source.Binding, Checkpoint: &clone}
			if mode == "legacy" || mode == "required" || mode == "durable" {
				clone.PlanState = nil
			}
			if mode == "required" {
				record.CanonicalRequired = true
			}
			if mode == "durable" {
				clone.Durable = &contract.CheckpointDurableBook{}
			}
			if mode == "no-source" {
				held.Plan = nil
			}
			if mode == "changed-plan" {
				p := *held.Plan
				p.Snapshot = bytes.Replace(p.Snapshot, []byte("Review historical references"), []byte("Changed reviewed scope"), 1)
				held.Plan = &p
			}
			want := mode == "accepted" || mode == "legacy"
			if assistantRunSourceMatchesRecord(held, record) != want {
				t.Fatal("resume lineage disposition changed")
			}
			input := &runcontext.Input{Execution: &clone}
			if mode == "required" {
				input.RunStore = &runcontext.DurableRunInput{}
			}
			if (bindAssistantRunPlan(held, input) == nil) != want {
				t.Fatal("execution lineage disposition changed")
			}
		})
	}
	fresh := source
	fresh.PlanResolver = &runcontext.PlanResolver{}
	if !assistantRunFreshSourceEqual(source, fresh) {
		t.Fatal("capability closure changed source equality")
	}
	fresh.Plan = &assistantRunPlan{Snapshot: bytes.Clone(source.Plan.Snapshot), Locators: append([]assistantPlanReference{}, source.Plan.Locators...)}
	fresh.Plan.Locators[0].ID = "different.json"
	if assistantRunFreshSourceEqual(source, fresh) {
		t.Fatal("locator custody omitted from source equality")
	}
	if reflect.DeepEqual(fresh.Plan, source.Plan) {
		t.Fatal("fixture failed to alter locator")
	}
}

type assistantPlanBindingExecutor struct{ *assistantProtocolExecutor }

func (*assistantPlanBindingExecutor) assistantCanonicalRuns() {}
func (*assistantPlanBindingExecutor) Prepare(ctx context.Context, source assistantRunSource) (contract.Checkpoint, error) {
	// Use the saved-chat owner's actual preparation inputs, including native
	// config/profile/chat effort. An empty effort seeds a different policy from
	// the production factory when the selected native model has a shared tier.
	return (&assistantCreationExecutor{}).Prepare(ctx, source)
}

// Declare the fixture interaction before the actual contextual session enrolls
// its complete offer. Reopening the production Gate would replace its Runtime
// owner and is correctly refused; retain the existing domain registry as well.
func assistantPlanBindingOfferQuestion(x *assistantExecution) {
	x.Run.HostTools = append(x.Run.HostTools, runcontext.HostTool{Name: "ask_user", Description: "Ask a scope question.", Taxonomy: &runcontext.ToolTaxonomy{Operation: "ask", Effect: "pure"}, Interaction: "question", InputSchema: json.RawMessage(`{"type":"object"}`)})
}

func TestAssistantPlanBindingExplicitContinuation(t *testing.T) {
	for _, kind := range []string{"answer", "extend-budget"} {
		t.Run(kind, func(t *testing.T) {
			var requests atomic.Int32
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) == 1 {
					name, args := "ask_user", json.RawMessage(`{"title":"Scope","question":"Which scope?","selection":"single","options":[{"id":"one","label":"One"},{"id":"two","label":"Two"}]}`)
					if kind != "answer" {
						name, args = "validate", json.RawMessage(`{}`)
					}
					assistantPolicySSE(w, name, args, "")
				} else {
					assistantBriefSSE(w, "Exact accepted scope continued.", "stop")
				}
			})
			proceed := make(chan struct{})
			s, token, e := assistantExecutionFixtureSource(t, u.server.URL, "work", func(ctx context.Context, e *assistantRunExecution) error {
				<-proceed
				assistantExecutionRuntimeFixture(t, e.service.server, e)
				x, err := e.openAssistantExecution(ctx, assistantExecutionPrompt{Name: "served"})
				if err != nil {
					return err
				}
				defer x.Close()
				if kind == "answer" {
					assistantPlanBindingOfferQuestion(x)
				}
				if kind != "answer" && len(e.record.Actions) == 0 {
					cp := *x.Run.Context.Execution
					cp.Used = cp.Limit - 1
					if cp.Digest != nil {
						raw, _ := json.Marshal(cp)
						value, err := runcontext.ParseJSON(raw)
						if err != nil {
							return err
						}
						digest := runcontext.RecordDigest(value)
						cp.Digest = &digest
					}
					// Seed the existing near-ceiling allowance through the original
					// writer so its acknowledged revision advances with the store.
					if err := x.Run.Context.Checkpoint(ctx, cp); err != nil {
						return err
					}
					x.Run.Context.Execution = x.Run.Context.Persistence.Writer.Checkpoint()
				}
				return x.Adapter.Run(ctx, x.Run)
			}, func(s *Server, token string, start assistantRunStart) assistantRunStart {
				start = assistantPlanPrepareSource(t)(s, token, start)
				original := s.assistantRunExecutor.(*assistantProtocolExecutor)
				s.assistantRunExecutor = &assistantPlanBindingExecutor{original}
				start, _ = assistantBindingHistorical(t, s, token, start)
				return start
			})
			close(proceed)
			state := "awaiting-input"
			if kind != "answer" {
				state = "paused-budget"
			}
			paused := assistantProtocolWaitWithin(t, s, token, e.record.ID, state, 60*time.Second)
			before := canonicalActivationRecord(t, s, e.record.ID)
			accepted := assistantCheckpointAcceptedPlan(before.Checkpoint).JSON()
			if len(accepted) == 0 {
				t.Fatal("paused accepted lineage absent")
			}
			a := assistantContinuationActualAnswer(paused)
			if kind != "answer" {
				a = assistantContinuationAction(paused, kind)
			}
			path := filepath.Join(s.cfg.ProjectDir, "one.json")
			bytesBefore, err := os.ReadFile(path)
			assistantRunMust(t, err)
			assistantRunMust(t, os.WriteFile(path, []byte("changed while paused"), 0600))
			if w := policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", a); w.Code != 409 {
				t.Fatal("changed reference resumed", w.Code, w.Body.String())
			}
			if requests.Load() != 1 {
				t.Fatal("refused continuation dispatched")
			}
			assistantRunMust(t, os.WriteFile(path, bytesBefore, 0600))
			// Reading/reloading the retained canonical head is observational only.
			assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+paused.ID, nil), 200)
			if requests.Load() != 1 {
				t.Fatal("reload replayed work")
			}
			admitted := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", a), 200)
			next := assistantContinuationSuccessor(t, s, token, admitted)
			assistantProtocolWaitWithin(t, s, token, next.ID, "complete", 60*time.Second)
			after := canonicalActivationRecord(t, s, next.ID)
			canonicalActivationCheck(t, after)
			if requests.Load() != 2 || !bytes.Equal(accepted, assistantCheckpointAcceptedPlan(after.Checkpoint).JSON()) {
				t.Fatal("original Plan scope lost")
			}
			parent := canonicalActivationRecord(t, s, paused.ID)
			if kind == "answer" {
				if next.ID == paused.ID || before.Checkpoint.ID == after.Checkpoint.ID || after.Checkpoint.Used != 1 || len(after.Checkpoint.Grants) != 0 || !reflect.DeepEqual(before.Canonical, parent.Canonical) || !reflect.DeepEqual(before.Checkpoint, parent.Checkpoint) || !reflect.DeepEqual(before.Operations, parent.Operations) || len(after.AnswerHistory) != 1 {
					t.Fatal("successor inherited old execution or mutated complete parent")
				}
			} else if before.Checkpoint.ID != after.Checkpoint.ID || after.Checkpoint.Used != before.Checkpoint.Used+1 || !reflect.DeepEqual(before.Canonical.Commits, after.Canonical.Commits[:len(before.Canonical.Commits)]) {
				t.Fatal("budget continuation lost accounting or canonical receipts")
			}
			if len(parent.Actions) != 1 {
				t.Fatal("explicit action receipt absent")
			}
			exact, _ := json.Marshal(after.Canonical)
			assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", a), 200)
			again, _ := json.Marshal(canonicalActivationRecord(t, s, next.ID).Canonical)
			if requests.Load() != 2 || !bytes.Equal(exact, again) {
				t.Fatal("action redelivery replayed or rewrote execution")
			}
		})
	}
}
