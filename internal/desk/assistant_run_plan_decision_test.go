package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func TestAssistantRunPlanDecisionHTTP(t *testing.T) {
	for _, approved := range []bool{true, false} {
		t.Run(map[bool]string{true: "accept", false: "reject"}[approved], func(t *testing.T) {
			var modelCalls int
			upstream := newUpstream(t, func(http.ResponseWriter, *http.Request) { modelCalls++ })
			s, token, e := assistantExecutionFixtureSource(t, upstream.server.URL, "work", func(ctx context.Context, e *assistantRunExecution) error {
				if e.decision == nil {
					// Follow the original input/critic capture order before the
					// handwritten pause. A continuation cannot acquire new custody
					// after its explicit action has already been admitted.
					chat, err := e.prepareAssistantInput(ctx)
					if err != nil {
						return err
					}
					input := &runcontext.Input{}
					if err := e.bindCanonicalInput(ctx, input); err != nil {
						return err
					}
					if err := bindAssistantRunPlan(e.source, input); err != nil {
						return err
					}
					if assistantCriticEnabled(chat, e.record.Binding.Phase) && runcontext.CriticForcedOff(runcontext.CriticForcedFacts{Purpose: chat.CriticPurpose}) == "" {
						if _, err := e.holdAssistantCritic(ctx); err != nil {
							return err
						}
					}
					cp := assistantPreparationPending(t, *input.Persistence.Writer.Checkpoint())
					if err := input.Persistence.Writer.Save(ctx, *input.Persistence, cp); err != nil {
						return err
					}
					assistantExecutionRuntimeFixture(t, e.service.server, e)
					return os.WriteFile(e.service.server.cfg.JpackBin, []byte("#!/usr/bin/env python3\nimport sys\nfor line in sys.stdin:\n open('plan-http-runtime-frame.json','w').write(line)\n sys.exit(72)\n"), 0700)
				}
				x, err := e.openAssistantExecution(ctx, assistantExecutionPrompt{Name: "served"})
				if x != nil {
					x.Close()
				}
				return err
			}, func(s *Server, token string, start assistantRunStart) assistantRunStart {
				start = assistantPlanPrepareSource(t)(s, token, start)
				s.assistantRunExecutor = &assistantPlanBindingExecutor{s.assistantRunExecutor.(*assistantProtocolExecutor)}
				start, _ = assistantBindingHistorical(t, s, token, start)
				return start
			})
			paused := assistantProtocolWait(t, s, token, e.record.ID, "awaiting-input")
			ref := paused.PlanDecisionReference
			if ref == nil || paused.ApprovalReference != nil {
				t.Fatal("missing distinct Plan reference")
			}
			before := assistantExecutionRecord(t, e)
			action := assistantRunAction{ID: "explicit-plan", Kind: "plan-decision", Revision: paused.Revision, CallID: ref.CallID, PlanRevision: ref.PlanRevision, CandidateRevision: ref.CandidateRevision, Approved: &approved}
			path := "/api/assistant-runs/" + paused.ID + "/actions"
			for _, mutate := range []func(*assistantRunAction){func(a *assistantRunAction) { a.CallID = "stale" }, func(a *assistantRunAction) { a.PlanRevision = "sha256:" + strings.Repeat("b", 64) }, func(a *assistantRunAction) { a.CandidateRevision = "sha256:" + strings.Repeat("c", 64) }, func(a *assistantRunAction) { a.Revision-- }, func(a *assistantRunAction) {
				*a = assistantRunAction{ID: "bypass", Kind: "extend-budget", Revision: paused.Revision}
			}, func(a *assistantRunAction) {
				*a = assistantRunAction{ID: "bypass", Kind: "answer", Revision: paused.Revision, QuestionID: "q", QuestionDigest: strings.Repeat("a", 64), Answer: &assistantRunAnswerDraft{Selected: []string{}, Text: new(string)}}
			}} {
				bad := action
				mutate(&bad)
				if w := policyCall(s, token, "POST", path, bad); w.Code != 409 {
					t.Fatalf("stale/bypass status %d: %s", w.Code, w.Body.String())
				}
				current := assistantExecutionRecord(t, e)
				if !bytes.Equal(assistantChatJSON(before), assistantChatJSON(current)) {
					t.Fatal("refusal changed retained bytes")
				}
			}
			assistantProtocolView(t, policyCall(s, token, "POST", path, action), 200)
			stopped := assistantProtocolWait(t, s, token, paused.ID, "stopped")
			after := assistantExecutionRecord(t, e)
			if len(after.Actions) != 1 || after.Actions[0].PlanDecision == nil || *after.Actions[0].PlanDecision != *ref || after.Actions[0].Decision.Approved != approved {
				t.Fatal("exact admitted receipt missing")
			}
			value := runcontext.MustParseJSON(string(assistantChatJSON(after.Checkpoint)))
			decisions := value.Get("planState").Get("decisions")
			if decisions.Len() != 1 || value.Get("planState").Get("pending") != nil || decisions.Items()[0].Get("candidateRevision").Str() != ref.CandidateRevision || decisions.Items()[0].Get("approved").Truth() != approved {
				t.Fatal("durable Plan decision missing")
			}
			count := 0
			for _, event := range after.Canonical.Events {
				if bytes.Contains(event.Bytes, []byte(`"plan_drift_decided"`)) {
					count++
				}
			}
			if count != 1 || modelCalls != 0 {
				t.Fatal("decision repeated/model called", count, modelCalls)
			}
			if _, err := os.Stat(filepath.Join(s.cfg.ProjectDir, "plan-http-runtime-frame.json")); err != nil {
				t.Fatal("Runtime not reached after decision", err)
			}
			assistantProtocolView(t, policyCall(s, token, "POST", path, action), 200)
			again := assistantExecutionRecord(t, e)
			if !bytes.Equal(assistantChatJSON(after), assistantChatJSON(again)) || stopped.Revision != again.Revision {
				t.Fatal("lost-ack retry replayed")
			}
			other := !approved
			action.Approved = &other
			if w := policyCall(s, token, "POST", path, action); w.Code != 409 {
				t.Fatal("different digest accepted", w.Code)
			}
		})
	}
}

func TestAssistantRunPlanDecisionBoundary(t *testing.T) {
	base := `{"id":"decision","kind":"plan-decision","revision":3,"callId":"call","planRevision":"sha256:` + strings.Repeat("a", 64) + `","candidateRevision":"sha256:` + strings.Repeat("b", 64) + `","approved":false}`
	for _, raw := range []string{base, strings.Replace(base, `"approved":false`, `"approved":true,"note":"reviewed"`, 1)} {
		var action assistantRunAction
		r, _ := http.NewRequest("POST", "/", strings.NewReader(raw))
		assistantRunMust(t, assistantRunRequest(r, &action))
		assistantRunMust(t, validateAssistantRunActionShape(action))
	}
	for _, raw := range []string{strings.Replace(base, `"callId"`, `"CallId"`, 1), strings.Replace(base, `"approved":false`, `"approved":null`, 1), strings.Replace(base, `"approved":false`, `"approved":false,"argsDigest":"`+strings.Repeat("a", 64)+`"`, 1), strings.Replace(base, `sha256:`+strings.Repeat("b", 64), strings.Repeat("b", 64), 1), strings.Replace(base, `"approved":false`, `"approved":false,"unknown":0`, 1)} {
		var action assistantRunAction
		r, _ := http.NewRequest("POST", "/", strings.NewReader(raw))
		if assistantRunRequest(r, &action) == nil && validateAssistantRunActionShape(action) == nil {
			t.Fatal("malformed action admitted", raw)
		}
	}
	var receipt assistantRunActionReceipt
	if json.Unmarshal([]byte(`{"id":"decision","digest":"`+strings.Repeat("a", 64)+`","kind":"plan-decision","decision":{"callId":"call","approved":true},"planDecision":{"callId":"call","planRevision":"sha256:`+strings.Repeat("a", 64)+`","candidateRevision":"sha256:`+strings.Repeat("b", 64)+`"}}`), &receipt) != nil {
		t.Fatal("receipt decode")
	}
	assistantRunMust(t, validateAssistantRunActionReceipts(assistantRunRecord{Actions: []assistantRunActionReceipt{receipt}}))
	validReceipt := string(assistantChatJSON(receipt))
	for _, raw := range []string{
		strings.Replace(validReceipt, `"approved":true`, `"approved":null`, 1),
		strings.Replace(validReceipt, `"approved":true`, `"Approved":true`, 1),
		strings.Replace(validReceipt, `"approved":true`, `"extra":true`, 1),
		strings.Replace(validReceipt, `"callId":"call","planRevision"`, `"CallId":"call","planRevision"`, 1),
		strings.Replace(validReceipt, `"approved":true`, `"approved":true,"approved":false`, 1),
	} {
		var malformed assistantRunActionReceipt
		if json.Unmarshal([]byte(raw), &malformed) == nil {
			t.Fatal("malformed retained decision admitted", raw)
		}
	}
	receipt.PlanDecision.CandidateRevision = "bad"
	if validateAssistantRunActionReceipts(assistantRunRecord{Actions: []assistantRunActionReceipt{receipt}}) == nil {
		t.Fatal("invalid retained expectation accepted")
	}
}

func assistantSharedPlanDecisionFixture(t *testing.T) (*runcontext.Value, *contract.Checkpoint, assistantPlanDecisionReference) {
	t.Helper()
	raw, err := os.ReadFile("../../web/src/assistant/serverRuns/fixtures/plan-decision.json")
	assistantRunMust(t, err)
	value, err := runcontext.ParseJSON(raw)
	assistantRunMust(t, err)
	cp, err := runcontext.ReadExecution(value.Get("checkpoint"))
	assistantRunMust(t, err)
	var ref assistantPlanDecisionReference
	assistantRunMust(t, json.Unmarshal(value.Get("planDecisionReference").JSON(), &ref))
	return value.Get("checkpoint"), cp, ref
}

// Keep these corruptions paired with the direct published-reader TS test.
func TestAssistantRunPlanDecisionSharedFixture(t *testing.T) {
	value, cp, ref := assistantSharedPlanDecisionFixture(t)
	if got := assistantRunPlanReference(assistantRunRecord{Checkpoint: cp}); got == nil || *got != ref {
		t.Fatal("shared fixture lost exact reference", got)
	}
	for name, mutate := range map[string]func(*runcontext.Value){
		"historical-version": func(v *runcontext.Value) { v.Set("version", runcontext.Number(1)) },
		"accepted-digest": func(v *runcontext.Value) {
			v.Get("planState").Get("accepted").Set("revision", runcontext.String("sha256:"+strings.Repeat("a", 64)))
		},
		"pending-revision": func(v *runcontext.Value) {
			v.Get("planState").Get("pending").Set("revision", runcontext.String("sha256:"+strings.Repeat("a", 64)))
		},
		"history-previous": func(v *runcontext.Value) {
			v.Get("planState").Get("pending").Get("candidate").Get("planState").Get("history").Items()[0].Set("previous", runcontext.String("sha256:"+strings.Repeat("a", 64)))
		},
		"departure-counter": func(v *runcontext.Value) {
			v.Get("planState").Get("pending").Get("candidate").Get("planState").Set("departuresDiscarded", runcontext.Number(1))
		},
		"candidate-allocator": func(v *runcontext.Value) {
			v.Get("planState").Get("pending").Get("candidate").Get("planState").Set("nextId", runcontext.Number(4))
		},
		"unknown-member": func(v *runcontext.Value) { v.Get("planState").Get("pending").Set("unknown", runcontext.Bool(true)) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := value.Clone()
			mutate(bad)
			if _, err := runcontext.ReadExecution(bad); err == nil {
				t.Fatal("corrupt shared checkpoint admitted")
			}
		})
	}
}

func TestAssistantRunPlanDecisionRefusals(t *testing.T) {
	for _, mode := range []string{"foreign-owner", "new-session", "revoked", "wrong-state", "asked-and-pending", "phase-changed", "unsupported-phase"} {
		t.Run(mode, func(t *testing.T) {
			original, _, ref := assistantSharedPlanDecisionFixture(t)
			executions := make(chan *assistantRunExecution, 1)
			s, token, start, executor := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
				executions <- e
				value := original.Clone()
				value.Set("requestKey", runcontext.String(e.record.Binding.ExecutionKey))
				value.Set("id", runcontext.String(e.record.Checkpoint.ID))
				value.Set("phase", runcontext.String(e.record.Binding.Phase))
				if mode == "asked-and-pending" {
					value.Set("approval", runcontext.MustParseJSON(`{"callId":"ordinary-call","name":"write_file","tool":"write_file","source":{"kind":"custom","id":"fixture","trust":"person"},"args":{"path":"reviewed.txt"},"state":"asked"}`))
				}
				cp, err := runcontext.ReadExecution(value)
				if err != nil {
					return err
				}
				return e.writer.Checkpoint(ctx, *cp)
			})
			if mode == "unsupported-phase" {
				saved := policyWorkspaceRead(t, s, token, "/api/conversations")
				var doc conversationDocument
				assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
				var chat map[string]any
				assistantRunMust(t, json.Unmarshal(doc.Chats[0], &chat))
				chat["interaction"] = "plan"
				doc.Chats[0] = assistantChatJSON(chat)
				response := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc)
				if response.Code != 200 {
					t.Fatalf("phase fixture save: %d %s", response.Code, response.Body.String())
				}
				current := policyWorkspaceRead(t, s, token, "/api/conversations")
				assistantRunMust(t, json.Unmarshal(current.Content, &doc))
				start.Revision = digestOf(doc.Chats[0])
			}
			started := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
			// An asked approval is an approval pause: the run awaits the person
			// (awaiting-input) even with a Plan pending, and withholds the Plan
			// reference, so the Plan decision below is refused.
			paused := assistantProtocolWait(t, s, token, started.ID, "awaiting-input")
			e := <-executions
			// Wait for finalization to drop the active executor before admission checks.
			<-e.done
			if mode == "wrong-state" {
				// Reach terminal state through the authenticated action boundary. The
				// finalized execution writer no longer owns a lease.
				assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", assistantRunAction{ID: "stop-before-plan", Kind: "cancel", Revision: paused.Revision}), 200)
				paused = assistantProtocolWait(t, s, token, paused.ID, "stopped")
			}
			before := assistantExecutionRecord(t, e)
			if mode == "asked-and-pending" && (!assistantRunApprovalPause(before) || assistantRunPlanReference(before) != nil) {
				t.Fatal("an asked approval did not withhold the pending Plan decision")
			}
			action := assistantRunAction{ID: "refused-plan", Kind: "plan-decision", Revision: paused.Revision, CallID: ref.CallID, PlanRevision: ref.PlanRevision, CandidateRevision: ref.CandidateRevision, Approved: new(bool)}
			requestToken := token
			want := 409
			switch mode {
			case "foreign-owner":
				var err error
				requestToken, err = s.sessions.createExpiring("another principal", nil, "", "")
				assistantRunMust(t, err)
				want = 404
			case "new-session":
				requestToken = policyTestSession(t, s)
				want = 404
			case "unsupported-phase":
				want = 404
			case "revoked":
				s.sessions.revoke(token)
				want = 401
			case "phase-changed":
				saved := policyWorkspaceRead(t, s, token, "/api/conversations")
				var doc conversationDocument
				assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
				var chat map[string]any
				assistantRunMust(t, json.Unmarshal(doc.Chats[0], &chat))
				chat["interaction"] = "plan"
				doc.Chats[0] = assistantChatJSON(chat)
				w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc)
				if w.Code != 200 {
					t.Fatalf("phase fixture save: %d %s", w.Code, w.Body.String())
				}
			}
			response := policyCall(s, requestToken, "POST", "/api/assistant-runs/"+paused.ID+"/actions", action)
			if response.Code != want {
				t.Fatalf("refusal %s: status %d want %d: %s", mode, response.Code, want, response.Body.String())
			}
			after := assistantExecutionRecord(t, e)
			if !bytes.Equal(assistantChatJSON(before), assistantChatJSON(after)) || executor.calls.Load() != 1 {
				t.Fatal("refusal changed retained bytes or relaunched")
			}
		})
	}
}
