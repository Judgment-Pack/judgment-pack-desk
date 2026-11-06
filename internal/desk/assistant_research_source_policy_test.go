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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// Same authored browser fixture as the established recipe tests, selecting the
// canonical owner at initial admission (never upgrading a retained execution).
func assistantResearchSourceBrowserFixture(t *testing.T, upstreamURL string) (*Server, string, *assistantRunExecution) {
	t.Helper()
	return assistantResearchSourceBrowserFixtureWithCompletion(t, upstreamURL, nil)
}

// A supplied signal lets the admitted service owner observe normal completion
// after the test drives its adapter. A nil signal retains the original lifetime.
func assistantResearchSourceBrowserFixtureWithCompletion(t *testing.T, upstreamURL string, completed <-chan struct{}) (*Server, string, *assistantRunExecution) {
	t.Helper()
	entered := make(chan *assistantRunExecution, 1)
	s, token, start, executor := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		entered <- e
		select {
		case <-completed:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	s.assistantRunExecutor = &assistantCanonicalProtocolExecutor{executor}
	connection := aiTestConnection(testAIAlpha, "Recipe fixture", upstreamURL)
	connection.Assistant.Endpoint.Tools = []string{"validate", "experimental_evaluate", "get_schema"}
	raw, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{connection}})
	assistantRunMust(t, err)
	assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, assistantProfilePath), []byte(`{"profileVersion":1,"api":{"inherit":true}}`), 0600))
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	var chat map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal(doc.Chats[0], &chat))
	for key, value := range map[string]any{"title": "Recipe review", "composer": "", "pinned": false, "archived": false, "updatedAt": "2026-10-20T00:00:00Z", "mode": "draft", "view": "chat"} {
		chat[key] = assistantChatJSON(value)
	}
	doc.Chats[0] = assistantChatJSON(chat)
	if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved = policyWorkspaceRead(t, s, token, "/api/conversations")
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	start.Revision = digestOf(doc.Chats[0])
	origin, _ := endpointOrigin(upstreamURL)
	assistantRunMust(t, s.assistant.storeKeyNamed(connectionKeyName(&connection), storedKey{present: true, key: "host-fixture-key", origin: origin, kind: connection.Assistant.Endpoint.Kind}))
	view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	select {
	case e := <-entered:
		if e.record.ID != view.ID || !assistantCanonicalActive(e.record) {
			t.Fatal("wrong canonical execution")
		}
		t.Cleanup(func() {
			e.cancel()
			select {
			case <-e.done:
			case <-time.After(3 * time.Second):
				t.Error("execution did not drain")
			}
		})
		return s, token, e
	case <-time.After(3 * time.Second):
		t.Fatal("execution did not start")
	}
	return nil, "", nil
}

func assistantResearchSourceApprovalFixture(t *testing.T, s *Server, token string) assistantSourceApproval {
	t.Helper()
	row := assistantSourceTestRow(t, assistantSourceTestView(t, s, token), "desk-research-workflow")
	approved := assistantSourceTestApprove(t, s, token, row)
	store, err := s.openChatData()
	assistantRunMust(t, err)
	defer store.root.Close()
	project, err := assistantRunNamespace(store.root, s.conversationIdentity())
	assistantRunMust(t, err)
	ledger, err := readAssistantSourceLedger(store.root, project)
	assistantRunMust(t, err)
	for _, approval := range ledger.Approvals {
		if approval.ID == approved.Approval.ID {
			return approval
		}
	}
	t.Fatal("reviewed source approval missing")
	return assistantSourceApproval{}
}

// Retain actual production enrollment and instrument its exact host policy.
// A raw standalone fixture still uses the same approval wrapper.
func assistantResearchSourceEnroll(t *testing.T, e *assistantRunExecution, x *assistantExecution, approval assistantSourceApproval, policy runcontext.ApprovalPolicy) {
	t.Helper()
	binding, err := assistantResearchApprovalBinding(approval)
	assistantRunMust(t, err)
	current := func(ctx context.Context) error {
		lock, err := e.service.server.privateDataLockContext(ctx, true)
		if err != nil {
			return err
		}
		defer lock.Close()
		return e.authorizeAssistantResearchSourceLocked(ctx, binding)
	}
	for i, tool := range x.Run.HostTools {
		if tool.Name != assistantResearchTool {
			continue
		}
		if assistantResearchAlreadyEnrolled(t, tool, approval) {
			tool.Approve = policy
			x.Run.HostTools[i] = tool
		} else {
			sourced, err := assistantExternalSourceTool(tool, approval, current, nil, policy)
			assistantRunMust(t, err)
			x.Run.HostTools[i] = sourced
		}
		return
	}
	t.Fatal("workflow tool absent")
}

func TestAssistantResearchSourceDispositionAtomicAdmission(t *testing.T) {
	for _, mode := range []string{"exact-once", "grant-after-review-only", "replaced", "revoked", "expired", "used-other-turn", "foreign-session", "changed-limits", "changed-companies", "changed-source", "source-revoked", "changed-recipe", "run-lock-race", "lost-ack", "duplicate-policy"} {
		t.Run(mode, func(t *testing.T) {
			var saved inputPreparationReply
			var steps atomic.Int32
			wire := runcontext.WireName("desk-research-workflow", assistantResearchTool)
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				n := steps.Add(1)
				finish := "tool_calls"
				var delta map[string]any
				if n <= 3 {
					name, args := wire, assistantResearchArgs(saved)
					if n == 1 {
						name, args = "read_dialect_recipe", string(assistantChatJSON(map[string]string{"id": saved.Document.ID}))
					}
					if n == 3 && mode != "exact-once" {
						name, args = "read_dialect_recipe", string(assistantChatJSON(map[string]string{"id": saved.Document.ID}))
					}
					delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("source-call-%d", n), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}
				} else {
					delta, finish = map[string]any{"role": "assistant", "content": "Report the retained workflow outcome."}, "stop"
				}
				raw, _ := json.Marshal(map[string]any{"id": "source-workflow-fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: "+string(raw)+"\n\ndata: {\"id\":\"source-workflow-fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\""+finish+"\"}]}\n\ndata: [DONE]\n\n")
			})
			s, token, e := assistantResearchSourceBrowserFixture(t, u.server.URL)
			seeded, rr, run := assistantResearchSeed(t, s, token, e, mode != "grant-after-review-only")
			saved = seeded
			approval := assistantResearchSourceApprovalFixture(t, s, token)
			x := assistantExecutionOpen(t, s, e)
			owner := &assistantResearchTestOwner{journal: s.researchJournal(rr, run.ID)}
			if mode == "lost-ack" {
				owner.after = func() {}
			}
			if mode == "run-lock-race" {
				owner.before = func(_ context.Context, current *inputResearchRun) error {
					current.Recipe.Limits.Pages++
					return nil
				}
			}
			s.researchJournals = owner
			policy := e.assistantResearchSourcePolicy(approval)
			first := true
			var policyCalls atomic.Int32
			wrapped := func(call runcontext.ApprovalRequest) string {
				policyCalls.Add(1)
				answer := policy(call)
				// Bounded fixture diagnostics: observe the actual policy answer;
				// do not retry preparation or manufacture a claim on refusal.
				binding, bindingErr := assistantResearchApprovalBinding(approval)
				source, sourceErr := json.Marshal(call.Source)
				t.Logf("source policy call=%d answer=%q wireMatches=%t toolMatches=%t argsMatch=%t sourceMatches=%t", policyCalls.Load(), answer, call.Name == wire, call.Tool == assistantResearchTool, assistantHostArgumentsMatch(call.Args, []byte(assistantResearchArgs(saved))), bindingErr == nil && sourceErr == nil && assistantHostSourceCallRecordMatches(binding, source))
				if answer != "allow" || !first {
					return answer
				}
				first = false
				if mode == "duplicate-policy" && policy(call) != "refuse" {
					t.Error("a second preparation replaced the pending disposition")
				}
				if mode == "grant-after-review-only" {
					w := assistantResearchHTTP(t, s, token, "/api/input-preparations/"+run.PreparationID+"/research/"+run.ID+"/authorize", saved.SHA256, fmt.Sprintf(`{"chatId":%q,"minutes":60}`, e.record.Binding.Owner.ID))
					if w.Code != 200 {
						t.Error("late real allowance refused", w.Code, w.Body.String())
					}
					return answer
				}
				if mode == "changed-recipe" {
					document := saved.Document
					document.Name = "Changed after policy preparation"
					_, err := s.preparationRepository().CompareAndSwap(t.Context(), document, saved.SHA256)
					assistantRunMust(t, err)
				}
				if mode == "source-revoked" {
					w := policyCall(s, token, "POST", "/api/assistant-source-approvals/"+e.record.Binding.Owner.ID+"/revoke", assistantSourceRevokeRequest{approval.ID, approval.Revision})
					if w.Code != 200 {
						t.Error("source revocation refused", w.Code, w.Body.String())
					}
				}
				if contains([]string{"replaced", "revoked", "expired", "used-other-turn", "foreign-session", "changed-limits", "changed-companies", "changed-source"}, mode) {
					_, err := owner.journal.Update(t.Context(), func(current *inputResearchRun) error {
						switch mode {
						case "replaced", "revoked":
							current.Approval.RevokedAt = time.Now().UTC().Format(time.RFC3339Nano)
						case "expired":
							now := time.Now().UTC()
							current.Approval.ApprovedAt = now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
							current.Approval.ExpiresAt = now.Add(-time.Hour).Format(time.RFC3339Nano)
						case "used-other-turn":
							current.State = "queued"
							current.Approval.UsedAt = time.Now().UTC().Format(time.RFC3339Nano)
							current.Approval.TurnID = "another-turn"
						case "foreign-session":
							current.Approval.SessionScope = strings.Repeat("e", 64)
						case "changed-limits":
							current.Recipe.Limits.Pages++
						case "changed-companies":
							current.Companies = []inputResearchEntity{{ID: "company-changed", Kind: "company", Name: "Changed company", URL: "https://example.invalid/changed", Fields: map[string]inputResearchValue{}}}
						case "changed-source":
							current.Gateway.Authority = "gateway:changed"
						}
						return nil
					})
					assistantRunMust(t, err)
					if mode == "replaced" {
						fresh, err := owner.journal.Read(t.Context())
						assistantRunMust(t, err)
						fresh.ID = "research_99999999999999999999999999999999"
						fresh.Approval.RevokedAt = ""
						root, err := s.inputResearchRoot(false)
						assistantRunMust(t, err)
						defer root.Close()
						other, err := openInputResearch(root, fresh.ID, true)
						assistantRunMust(t, err)
						defer other.Close()
						// Use the actual independent journal, not the single-run test owner.
						assistantRunMust(t, (fileResearchJournal{root: other, id: fresh.ID}).Create(t.Context(), fresh))
						s.researchJournals = nil
					}
				}
				return answer
			}
			assistantResearchSourceEnroll(t, e, x, approval, wrapped)
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			err := x.Adapter.Run(ctx, x.Run)
			if mode != "lost-ack" {
				assistantRunMust(t, err)
			}
			got, err := owner.journal.Read(t.Context())
			assistantRunMust(t, err)
			wantQueued := contains([]string{"exact-once", "duplicate-policy", "lost-ack"}, mode)
			if (owner.queued.Load() == 1) != wantQueued || owner.queued.Load() > 1 || len(got.Attempts) != 0 {
				assistantResearchSourceAdmissionDiagnostic(t, e, saved, got, steps.Load(), policyCalls.Load(), owner.calls.Load())
				t.Fatalf("wrong atomic queue count %d, mode %s", owner.queued.Load(), mode)
			}
			if mode == "grant-after-review-only" && (got.State != "review" || got.Approval == nil || got.Approval.UsedAt != "") {
				t.Fatal("late allowance upgraded the frozen review-only decision")
			}
			if mode == "replaced" {
				root, err := s.inputResearchRoot(false)
				assistantRunMust(t, err)
				defer root.Close()
				other, err := openInputResearch(root, "research_99999999999999999999999999999999", false)
				assistantRunMust(t, err)
				defer other.Close()
				replacement, err := (fileResearchJournal{root: other, id: "research_99999999999999999999999999999999"}).Read(t.Context())
				assistantRunMust(t, err)
				if replacement.State != "review" || replacement.Approval.UsedAt != "" {
					t.Fatal("admission substituted a newer matching allowance")
				}
			}
			if mode == "exact-once" || mode == "duplicate-policy" {
				assistantResearchUsed(t, s, rr, run)
			}
			record := assistantExecutionRecord(t, e)
			assistantRunMust(t, assistantRecipeImport(t, e, record))
			policies := 0
			for _, operation := range record.Operations {
				if operation.Name == assistantResearchPolicyOperation {
					policies++
				}
			}
			if policies == 0 {
				t.Fatal("fixture never reached the actual source policy")
			}
			if mode == "grant-after-review-only" {
				if len(record.DomainOutputs) != 1 || record.DomainOutputs[0].DialectAction == nil || record.DomainOutputs[0].DialectAction.Research == nil || record.DomainOutputs[0].DialectAction.Research.Receipt.Status != "review-required" {
					t.Fatal("frozen review-only did not retain the real review card")
				}
			}
			if mode == "exact-once" {
				callIDs := assistantResearchSourceCanonicalCalls(t, record, approval, []byte(assistantResearchArgs(saved)), 2)
				assistantResearchSourceReplayResults(t, record, approval, callIDs)
				assistantResearchSourceProviderResults(t, u.arrivals(), record, callIDs, []int{2, 3}, []byte(assistantResearchArgs(saved)))
				assistantResearchSourcePolicyMutations(t, e, record)
			}
			if mode == "lost-ack" {
				select {
				case <-e.done:
				case <-time.After(5 * time.Second):
					t.Fatal("uncertain sourced workflow did not close the chat execution")
				}
				beforeCalls := owner.calls.Load()
				reconstructed := &assistantRunExecution{service: &assistantRunService{server: s}, request: e.request.Clone(t.Context()), record: record}
				assistantRunMust(t, reconstructed.ProjectChat(t.Context()))
				assistantRunMust(t, reconstructed.ProjectChat(t.Context()))
				recovered := assistantExecutionRecord(t, e)
				assistantRunMust(t, assistantRecipeImport(t, e, recovered))
				if owner.calls.Load() != beforeCalls || len(recovered.DomainOutputs) != 1 || recovered.DomainOutputs[0].DialectAction == nil || recovered.DomainOutputs[0].DialectAction.Research == nil || !recovered.DomainOutputs[0].DialectAction.Research.Recovered {
					t.Fatal("reconciliation failed to observe the exact source receipt without replay")
				}
			}
			before := assistantChatJSON(got)
			q := inputResearchRequest{ChatID: e.record.Binding.Owner.ID, PreparationID: run.PreparationID, Revision: run.Revision, TurnID: e.record.Binding.RequestID}
			if wantQueued {
				_, err = s.inspectResearchRequest(e.request.WithContext(t.Context()), q)
				assistantRunMust(t, err)
			}
			after, err := owner.journal.Read(t.Context())
			assistantRunMust(t, err)
			if !bytes.Equal(before, assistantChatJSON(after)) || len(after.Attempts) != 0 {
				t.Fatal("historical observation replayed worker work")
			}
		})
	}
}

// Failure-only observations keep the original positive admission assertion.
// Inspect only the original journal and retained assistant record; no request,
// policy, claim, provider, worker, reconciliation or fresh-grant scan is run.
func assistantResearchSourceAdmissionDiagnostic(t *testing.T, e *assistantRunExecution, saved inputPreparationReply, original inputResearchRun, providerSteps, policyCalls, journalUpdates int32) {
	t.Helper()
	t.Logf("source admission providerSteps=%d policyCalls=%d journalUpdates=%d originalState=%q originalAllowanceState=%q originalSnapshot=%s", providerSteps, policyCalls, journalUpdates, original.State, researchApprovalState(original, time.Now()), researchAllowanceRevision(original))
	record, err := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
	if err != nil {
		t.Logf("source admission retained record read failed: %v", err)
		return
	}
	a, argErr := readAssistantDialectResearchArguments([]byte(assistantResearchArgs(saved)))
	_, read, readErr := assistantDialectReadOperation(record, assistantResearchTool, a)
	t.Logf("source admission canonical=%t phase=%q currentRead=%t validRead=%t policyLedgerValid=%t operations=%d events=%d outputs=%d", assistantCanonicalActive(record), record.Binding.Phase, argErr == nil && readErr == nil, readErr == nil && validAssistantDialectActionRead(read), validateAssistantResearchSourcePolicies(record) == nil, len(record.Operations), len(record.Events), len(record.DomainOutputs))
	shown := 0
	for _, op := range record.Operations {
		if op.Name != assistantResearchPolicyOperation && op.Name != assistantResearchClaimOperation && op.Name != assistantResearchOperation && op.Name != assistantResearchTool && op.Name != "read_dialect_recipe" {
			continue
		}
		if shown == 12 {
			t.Log("source admission further relevant operations omitted")
			break
		}
		shown++
		t.Logf("source admission operation id=%q name=%q kind=%q state=%q", op.ID, op.Name, op.Kind, op.State)
		if op.Name == assistantResearchPolicyOperation {
			var d assistantResearchSourceDisposition
			if readAssistantHostReceipt(op.Intent, &d) == nil {
				t.Logf("source admission policy read=%q allowance=%q reviewOnly=%t pinnedOriginal=%t snapshotMatchesOriginal=%t sourceApproval=%q", d.ReadOperationID, d.Allowance.AllowanceID, d.Allowance.AllowanceID == "", d.Allowance.AllowanceID == original.ID, researchDispositionMatches(d.Allowance, original), d.Source.ApprovalID)
			}
		}
		if op.Name == assistantResearchClaimOperation {
			var claim assistantResearchSourceClaim
			if readAssistantHostReceipt(op.Intent, &claim) == nil {
				t.Logf("source admission claim policy=%q dispatch=%q call=%q nativeCallback=%t", claim.PolicyID, claim.DurableDispatchID, claim.CallID, claim.CallbackID != "")
			}
		}
		if op.Kind == "host" {
			var in assistantHostIntent
			var result assistantHostResult
			if readAssistantHostReceipt(op.Intent, &in) == nil {
				t.Logf("source admission host root=%t sourced=%t parentMatches=%t nativeMatches=%t", in.Root, in.SourceBinding != nil, assistantParentHostCall(record, op.Name, in), assistantNativeHostCall(record, op.Name, in))
			}
			if readAssistantHostReceipt(op.Result, &result) == nil {
				t.Logf("source admission host retainedRecipe=%t", result.DialectRead != nil)
			}
		}
	}
	shown = 0
	for _, envelope := range record.Events {
		event := envelope.Event
		if event.ToolCall == nil && event.ToolResult == nil && event.Guardrail == nil && event.Error == nil && event.ApprovalRequested == nil {
			continue
		}
		if shown == 12 {
			t.Log("source admission further call/result/guardrail/error/approval events omitted")
			break
		}
		shown++
		if call := event.ToolCall; call != nil {
			t.Logf("source admission event toolCall=%q sourced=%t", call.Name, call.Source != nil)
		}
		if result := event.ToolResult; result != nil {
			text := result.Text
			if len(text) > 512 {
				text = text[:512] + " [truncated]"
			}
			t.Logf("source admission event toolResult=%q isError=%t text=%q", result.Name, result.IsError, text)
		}
		if event.Guardrail != nil || event.Error != nil || event.ApprovalRequested != nil {
			t.Logf("source admission event guardrail=%t error=%t approvalRequested=%t", event.Guardrail != nil, event.Error != nil, event.ApprovalRequested != nil)
		}
	}
	if record.Checkpoint != nil && record.Checkpoint.Durable != nil {
		shown = 0
		for _, dispatch := range record.Checkpoint.Durable.Dispatches {
			if shown == 12 {
				t.Log("source admission further canonical dispatches omitted")
				break
			}
			shown++
			t.Logf("source admission canonical dispatch id=%q kind=%q tool=%q state=%q read=%t role=%q child=%t failure=%t", dispatch.ID, dispatch.Kind, dispatch.Tool, dispatch.State, dispatch.Read, dispatch.Scope.Role, dispatch.Scope.Task != nil, dispatch.Failure != nil)
		}
	}
}

func assistantResearchSourcePolicyMutations(t *testing.T, e *assistantRunExecution, record assistantRunRecord) {
	t.Helper()
	for _, mode := range []string{"foreign-run", "foreign-execution", "foreign-turn", "foreign-chat", "wrong-read", "wrong-args", "wrong-source", "wrong-wire", "claim-dispatch", "claim-call", "duplicate-claim", "extra-policy-field", "extra-claim-field"} {
		t.Run(mode, func(t *testing.T) {
			var mutated assistantRunRecord
			assistantRunMust(t, json.Unmarshal(assistantChatJSON(record), &mutated))
			for i := range mutated.Operations {
				op := &mutated.Operations[i]
				if strings.HasPrefix(mode, "claim-") || mode == "duplicate-claim" || mode == "extra-claim-field" {
					if op.Name != assistantResearchClaimOperation {
						continue
					}
					var claim assistantResearchSourceClaim
					assistantRunMust(t, readAssistantHostReceipt(op.Intent, &claim))
					switch mode {
					case "claim-dispatch":
						claim.DurableDispatchID = "foreign-dispatch"
					case "claim-call":
						claim.CallID = "foreign-call"
					case "duplicate-claim":
						duplicate := *op
						duplicate.ID, duplicate.CallID = "duplicate-claim", "duplicate-claim"
						mutated.Operations = append(mutated.Operations, duplicate)
					}
					op.Intent = assistantChatJSON(claim)
					if mode == "extra-claim-field" {
						op.Intent = append([]byte(`{"grant":true,`), op.Intent[1:]...)
					}
					break
				}
				if op.Name != assistantResearchPolicyOperation {
					continue
				}
				var d assistantResearchSourceDisposition
				assistantRunMust(t, readAssistantHostReceipt(op.Intent, &d))
				switch mode {
				case "foreign-run":
					d.RunID = "another-run"
				case "foreign-execution":
					d.ExecutionKey = "another-execution"
				case "foreign-turn":
					d.Request.TurnID = "another-turn"
				case "foreign-chat":
					d.Request.ChatID = "another-chat"
				case "wrong-read":
					d.ReadOperationID = "another-read"
				case "wrong-args":
					d.Arguments = []byte(`{"id":"prep_99999999999999999999999999999999","revision":"` + strings.Repeat("a", 64) + `"}`)
				case "wrong-source":
					d.Source.ApprovalID = strings.Repeat("f", 32)
				case "wrong-wire":
					d.Source.WireName = "unreviewed__request_dialect_research"
				}
				op.Intent = assistantChatJSON(d)
				if mode == "extra-policy-field" {
					op.Intent = append([]byte(`{"grant":true,`), op.Intent[1:]...)
				}
				break
			}
			if err := assistantRecipeImport(t, e, mutated); err == nil {
				t.Fatal("foreign or duplicate policy/claim custody imported")
			}
		})
	}
}

func TestAssistantResearchSourceClaimRootWorkOnly(t *testing.T) {
	for _, mode := range []string{"child-work", "root-plan"} {
		t.Run(mode, func(t *testing.T) {
			e := &assistantRunExecution{record: assistantRunRecord{Binding: assistantRunBinding{Phase: "work"}}}
			ctx := t.Context()
			if mode == "root-plan" {
				e.record.Binding.Phase = "plan"
				ctx = context.WithValue(ctx, assistantHostRootKey{}, true)
			}
			binding := assistantExternalBindingVector(assistantResearchTool)
			if _, err := e.claimAssistantResearchSourcePolicy(ctx, []byte(`{}`), binding); err == nil {
				t.Fatal("a non-root Work scope reached policy claim/admission")
			}
		})
	}
}
