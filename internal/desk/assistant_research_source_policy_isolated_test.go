package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Supplemental root-only coverage. The complete Work registry fixture and actual
// producer remain mandatory integration/release gates. This does not qualify
// delegation, native execution, E6 enrollment, or the full model tool offer.
func TestAssistantResearchSourceIsolatedAtomicAdmission(t *testing.T) {
	for _, mode := range []string{"exact-once", "grant-after-review-only", "replaced", "revoked", "expired", "foreign-session", "changed-limits", "changed-companies", "changed-source", "source-revoked", "changed-recipe", "run-lock-race", "lost-ack"} {
		if !t.Run(mode, func(t *testing.T) {
			var saved inputPreparationReply
			var steps atomic.Int32
			var runtimeNames []string
			wire := runcontext.WireName("desk-research-workflow", assistantResearchTool)
			u := newUpstream(t, nil)
			u.mu.Lock()
			u.respond = func(w http.ResponseWriter, _ *http.Request) {
				n := steps.Add(1)
				// newUpstream consumes the HTTP body before calling this responder.
				// Inspect the actual captured arrival through its synchronized snapshot.
				seen := u.arrivals()
				if len(seen) != int(n) {
					t.Error("scripted provider capture count does not match request step", len(seen), n)
					http.Error(w, "unexpected capture count", http.StatusBadRequest)
					return
				}
				assistantResearchSourceIsolatedOffer(t, seen[n-1].body, wire, runtimeNames)
				last := int32(4)
				if mode == "exact-once" {
					last = 6
				}
				if n > last {
					t.Error("unexpected extra scripted provider request", n)
					http.Error(w, "script exhausted", http.StatusBadRequest)
					return
				}
				finish := "tool_calls"
				var delta map[string]any
				if n < last {
					name, args := wire, assistantResearchArgs(saved)
					if n == 1 || mode == "exact-once" && n == 4 {
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
			}
			u.mu.Unlock()
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
			var checkIdentity func()
			policy := e.assistantResearchSourcePolicy(approval)
			first := true
			var policyCalls atomic.Int32
			wrapped := func(call runcontext.ApprovalRequest) string {
				policyCalls.Add(1)
				// Observe the actual factory read and original ownership before the
				// production policy is allowed to prepare its disposition.
				checkIdentity()
				beforePolicy := assistantExecutionRecord(t, e)
				a, argErr := readAssistantDialectResearchArguments(call.Args)
				readOp, read, readErr := assistantDialectReadOperation(beforePolicy, assistantResearchTool, a)
				if argErr != nil || readErr != nil || !validAssistantDialectActionRead(read) || read.Revision != saved.SHA256 || readOp.Kind != "host" {
					t.Error("source policy was reached without the actual current recipe read", argErr, readErr)
					return "refuse"
				}
				var readIntent assistantHostIntent
				if readAssistantHostReceipt(readOp.Intent, &readIntent) != nil || !readIntent.Root || readIntent.SourceBinding != nil || !assistantParentHostCall(beforePolicy, "read_dialect_recipe", readIntent) {
					t.Error("recipe read lost local root canonical custody")
					return "refuse"
				}
				answer := policy(call)
				// Bounded fixture diagnostics: observe the actual policy answer;
				// do not retry preparation or manufacture a claim on refusal.
				binding, bindingErr := assistantResearchApprovalBinding(approval)
				source, sourceErr := json.Marshal(call.Source)
				t.Logf("source policy call=%d answer=%q wireMatches=%t toolMatches=%t argsMatch=%t sourceMatches=%t", policyCalls.Load(), answer, call.Name == wire, call.Tool == assistantResearchTool, assistantHostArgumentsMatch(call.Args, []byte(assistantResearchArgs(saved))), bindingErr == nil && sourceErr == nil && assistantHostSourceCallRecordMatches(binding, source))
				if call.Name != wire || call.Tool != assistantResearchTool || !assistantHostArgumentsMatch(call.Args, []byte(assistantResearchArgs(saved))) || bindingErr != nil || sourceErr != nil || !assistantHostSourceCallRecordMatches(binding, source) {
					t.Error("actual policy call did not carry exact wire/tool/closed arguments/source")
					return "refuse"
				}
				if answer != "allow" {
					t.Errorf("actual source policy refused the scripted call: %q", answer)
					return answer
				}
				if !first {
					return answer
				}
				first = false
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
				if contains([]string{"replaced", "revoked", "expired", "foreign-session", "changed-limits", "changed-companies", "changed-source"}, mode) {
					_, err := owner.journal.Update(t.Context(), func(current *inputResearchRun) error {
						switch mode {
						case "replaced", "revoked":
							current.Approval.RevokedAt = time.Now().UTC().Format(time.RFC3339Nano)
						case "expired":
							now := time.Now().UTC()
							current.Approval.ApprovedAt = now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
							current.Approval.ExpiresAt = now.Add(-time.Hour).Format(time.RFC3339Nano)
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
						fresh.Approval = nil
						fresh.State = "review"
						root, err := s.inputResearchRoot(false)
						assistantRunMust(t, err)
						defer root.Close()
						other, err := openInputResearch(root, fresh.ID, true)
						assistantRunMust(t, err)
						defer other.Close()
						// A second candidate starts unapproved. Only the authenticated
						// endpoint below may give it a real matching allowance.
						assistantRunMust(t, (fileResearchJournal{root: other, id: fresh.ID}).Create(t.Context(), fresh))
						s.researchJournals = nil
						w := assistantResearchHTTP(t, s, token, "/api/input-preparations/"+fresh.PreparationID+"/research/"+fresh.ID+"/authorize", saved.SHA256, fmt.Sprintf(`{"chatId":%q,"minutes":60}`, e.record.Binding.Owner.ID))
						if w.Code != http.StatusOK {
							t.Error("real replacement allowance refused", w.Code, w.Body.String())
						}
					}
				}
				return answer
			}
			isolated, identity := assistantResearchSourceIsolatedRun(t, e, x, approval, wrapped)
			checkIdentity = identity
			for _, tool := range isolated.Tools {
				runtimeNames = append(runtimeNames, tool.Name)
			}
			checkIdentity()
			frozenOriginal, err := owner.journal.Read(t.Context())
			assistantRunMust(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			err = x.Adapter.Run(ctx, isolated)
			checkIdentity()
			if err != nil {
				t.Logf("isolated adapter result type=%T error=%v", err, err)
			}
			if mode != "lost-ack" {
				assistantRunMust(t, err)
			} else if steps.Load() != 2 || owner.queued.Load() != 1 {
				t.Fatal("lost acknowledgement did not follow actual admission", err)
			}
			wantSteps := int32(4)
			if mode == "exact-once" {
				wantSteps = 6
			}
			if mode != "lost-ack" && steps.Load() != wantSteps {
				t.Fatal("scripted provider requests did not complete exactly", steps.Load())
			}
			got, err := owner.journal.Read(t.Context())
			assistantRunMust(t, err)
			wantQueued := contains([]string{"exact-once", "lost-ack"}, mode)
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
				if replacement.State != "review" || replacement.Approval == nil || replacement.Approval.UsedAt != "" || researchApprovalState(replacement, time.Now()) != "ready" {
					t.Fatal("admission substituted a newer matching allowance")
				}
			}
			if mode == "exact-once" {
				assistantResearchUsed(t, s, rr, run)
			}
			record := assistantExecutionRecord(t, e)
			assistantRunMust(t, assistantRecipeImport(t, e, record))
			assistantResearchSourceIsolatedCustody(t, record, approval, frozenOriginal, mode)
			wantPolicyCalls := int32(1)
			if mode == "exact-once" {
				wantPolicyCalls = 3
			}
			if policyCalls.Load() != wantPolicyCalls {
				t.Fatal("wrong actual SourceGate policy invocation count", policyCalls.Load(), wantPolicyCalls)
			}
			if !wantQueued && mode != "grant-after-review-only" {
				refused := false
				for _, envelope := range record.Events {
					if result := envelope.Event.ToolResult; result != nil && (result.Name == wire || result.Name == assistantResearchTool) && result.IsError {
						refused = true
					}
				}
				if !refused || len(record.DomainOutputs) != 0 {
					t.Fatal("invalidated source disposition did not retain the actual refused tool outcome")
				}
			}
			if mode == "grant-after-review-only" {
				if len(record.DomainOutputs) != 1 || record.DomainOutputs[0].DialectAction == nil || record.DomainOutputs[0].DialectAction.Research == nil || record.DomainOutputs[0].DialectAction.Research.Receipt.Status != "review-required" {
					t.Fatal("frozen review-only did not retain the real review card")
				}
			}
			if mode == "exact-once" {
				callIDs := assistantResearchSourceCanonicalCalls(t, record, approval, []byte(assistantResearchArgs(saved)), 3)
				assistantResearchSourceReplayResults(t, record, approval, callIDs)
				assistantResearchSourceProviderResults(t, u.arrivals(), record, callIDs, []int{2, 3, 5}, []byte(assistantResearchArgs(saved)))
				assistantResearchSourceReplayMutations(t, record)
				assistantResearchSourcePolicyMutations(t, e, record)
				assistantResearchSourceIsolatedDuplicatePreparation(t, e, record)
			}
			if mode == "lost-ack" {
				if !assistantResearchUnobserved(record) || len(record.DomainOutputs) != 0 {
					t.Fatal("lost acknowledgement did not retain the unobserved workflow outcome")
				}
				select {
				case <-e.done:
				case <-time.After(5 * time.Second):
					t.Fatal("uncertain sourced workflow did not close the chat execution")
				}
				beforeCalls, beforeSteps := owner.calls.Load(), steps.Load()
				reconstructed := &assistantRunExecution{service: &assistantRunService{server: s}, request: e.request.Clone(t.Context()), record: record}
				assistantRunMust(t, reconstructed.ProjectChat(t.Context()))
				assistantRunMust(t, reconstructed.ProjectChat(t.Context()))
				recovered := assistantExecutionRecord(t, e)
				assistantRunMust(t, assistantRecipeImport(t, e, recovered))
				if owner.calls.Load() != beforeCalls || steps.Load() != beforeSteps || len(recovered.DomainOutputs) != 1 || recovered.DomainOutputs[0].DialectAction == nil || recovered.DomainOutputs[0].DialectAction.Research == nil || !recovered.DomainOutputs[0].DialectAction.Research.Recovered {
					t.Fatal("reconciliation failed to observe the exact source receipt without replay")
				}
			}
			before := assistantChatJSON(got)
			beforeSteps, beforeUpdates := steps.Load(), owner.calls.Load()
			q := inputResearchRequest{ChatID: e.record.Binding.Owner.ID, PreparationID: run.PreparationID, Revision: run.Revision, TurnID: e.record.Binding.RequestID}
			if wantQueued {
				_, err = s.inspectResearchRequest(e.request.WithContext(t.Context()), q)
				assistantRunMust(t, err)
			}
			after, err := owner.journal.Read(t.Context())
			assistantRunMust(t, err)
			if !bytes.Equal(before, assistantChatJSON(after)) || len(after.Attempts) != 0 || steps.Load() != beforeSteps || owner.calls.Load() != beforeUpdates {
				t.Fatal("historical observation replayed worker work")
			}
		}) {
			// The first unexplained failure is evidence; do not keep invoking
			// later scenarios under an unestablished contract.
			return
		}
	}
}

// This is an explicitly authored root workflow subsystem, composed from the
// original production factory products. The original full registry is untouched.
func assistantResearchSourceIsolatedRun(t *testing.T, e *assistantRunExecution, x *assistantExecution, approval assistantSourceApproval, policy runcontext.ApprovalPolicy) (*adapter.Run, func()) {
	t.Helper()
	if x.Run == nil || x.Run.Context == nil || x.Run.Context.Persistence == nil || x.Run.Context.RunStore == nil || !x.Run.AllowConversation || !x.Run.Interactive {
		t.Fatal("isolated fixture needs the original interactive canonical run")
	}
	original := x.Run
	input, persistence := original.Context, original.Context.Persistence
	writer, store, runID := persistence.Writer, input.RunStore.Store, input.RunStore.ID
	if writer == nil || writer.Store != store || writer.ID != runID || !persistence.Root {
		t.Fatal("fixture did not retain the original root writer/store")
	}
	originalTools := append([]runcontext.HostTool(nil), original.HostTools...)
	isolated := *original
	isolated.HostTools = make([]runcontext.HostTool, 0, 2)
	counts := map[string]int{}
	for _, tool := range original.HostTools {
		if tool.Name != "read_dialect_recipe" && tool.Name != assistantResearchTool {
			continue
		}
		counts[tool.Name]++
		if tool.Execute == nil {
			t.Fatal("production factory handler missing", tool.Name)
		}
		if tool.Name == "read_dialect_recipe" && (tool.Source != nil || tool.Effect != "read" || tool.Taxonomy == nil || tool.Taxonomy.Operation != "read" || tool.Taxonomy.Effect != "read") {
			t.Fatal("local factory recipe read declaration changed")
		}
		isolated.HostTools = append(isolated.HostTools, tool)
	}
	if len(isolated.HostTools) != 2 || counts["read_dialect_recipe"] != 1 || counts[assistantResearchTool] != 1 {
		t.Fatal("root workflow surface must contain exactly one of each factory tool", counts)
	}
	binding, err := assistantResearchApprovalBinding(approval)
	assistantRunMust(t, err)
	row := approval.Snapshot
	found := false
	for _, base := range assistantSourceBaseCatalog() {
		if base.AdapterID == "desk-research-workflow" {
			found = row.ToolListDigest == base.ToolListDigest && reflect.DeepEqual(row.CompleteDeclarations, base.CompleteDeclarations)
		}
	}
	if !found || len(row.CompleteDeclarations) != 1 || row.CompleteDeclarations[0].Taxonomy.Operation != "act" || row.CompleteDeclarations[0].Taxonomy.Effect != "act" || row.CompleteDeclarations[0].EffectClass != "act" || row.CompleteDeclarations[0].Approval != "required" {
		t.Fatal("approval must retain the complete production workflow source declaration")
	}
	var check func()
	current := func(ctx context.Context) error {
		check()
		defer check()
		lock, err := e.service.server.privateDataLockContext(ctx, true)
		if err != nil {
			return err
		}
		defer lock.Close()
		return e.authorizeAssistantResearchSourceLocked(ctx, binding)
	}
	for i, tool := range isolated.HostTools {
		if tool.Name == assistantResearchTool {
			if assistantResearchAlreadyEnrolled(t, tool, approval) {
				source := *tool.Source
				previous := source.CurrentIdentity
				source.CurrentIdentity = func(ctx context.Context) (contract.ToolOfferIdentity, error) {
					if err := current(ctx); err != nil {
						return contract.ToolOfferIdentity{}, err
					}
					identity, err := previous(ctx)
					if err != nil {
						return contract.ToolOfferIdentity{}, err
					}
					return identity, current(ctx)
				}
				tool.Source, tool.Approve = &source, policy
				isolated.HostTools[i] = tool
			} else {
				isolated.HostTools[i], err = assistantExternalSourceTool(tool, approval, current, nil, policy)
				assistantRunMust(t, err)
			}
		}
	}
	// delegate.With documents this composition: nil Children forwards to inner
	// before coordinator construction. All other original capabilities survive.
	isolated.Children = nil
	check = func() {
		t.Helper()
		if isolated.Context != input || original.Context != input || input.Persistence != persistence || input.Persistence.Writer != writer || writer.Store != store || input.RunStore == nil || input.RunStore.Store != store || input.RunStore.ID != runID || isolated.Gate != original.Gate || isolated.Children != nil || !isolated.AllowConversation || !isolated.Interactive {
			t.Fatal("isolated root workflow changed original ownership or capabilities")
		}
		if len(original.HostTools) != len(originalTools) {
			t.Fatal("original full Work registry was mutated")
		}
		for i, tool := range original.HostTools {
			held := originalTools[i]
			if tool.Name != held.Name || tool.Source != held.Source || tool.Taxonomy != held.Taxonomy || reflect.ValueOf(tool.Execute).Pointer() != reflect.ValueOf(held.Execute).Pointer() {
				t.Fatal("original full Work factory product was mutated", held.Name)
			}
		}
	}
	check()
	return &isolated, check
}

// Inspect the actual HTTP provider request. Domain capabilities are exactly the
// selected products; the shared boundary may add only its known package tools.
func assistantResearchSourceIsolatedOffer(t *testing.T, requestBody []byte, wire string, runtimeNames []string) {
	t.Helper()
	var body struct {
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.NewDecoder(io.LimitReader(bytes.NewReader(requestBody), 4<<20)).Decode(&body); err != nil {
		t.Error("scripted provider request could not be inspected", err)
		return
	}
	allowed := map[string]bool{"read_dialect_recipe": true, wire: true}
	for _, name := range runtimeNames {
		allowed[name] = true
	}
	// These are package-owned declarations, not host-selected capabilities.
	for _, name := range []string{"propose_edit", "propose_delete", "propose_insert", "undo_edit", runcontext.Remember, "read_context", "list_context", runcontext.ReadTool, runcontext.SearchTool, "update_plan", "complete_plan_step", "request_budget_increase", runcontext.LoadTool, runcontext.LoadSkill, runcontext.UnloadSkill, runcontext.RequestReview, "ask_user"} {
		allowed[name] = true
	}
	counts := map[string]int{}
	for _, tool := range body.Tools {
		counts[tool.Function.Name]++
		if tool.Type != "function" || !allowed[tool.Function.Name] || counts[tool.Function.Name] != 1 {
			t.Error("unexpected or duplicate advertised capability", tool.Function.Name)
		}
	}
	if counts["read_dialect_recipe"] != 1 || counts[wire] != 1 || counts[assistantResearchTool] != 0 || counts["delegate_tasks"] != 0 {
		t.Error("scripted provider did not receive the exact root workflow surface", counts)
	}
}

// Assert custody on produced records only. The import validator independently
// checks the entire ledger, and its existing mutation vectors run on this record.
func assistantResearchSourceIsolatedCustody(t *testing.T, record assistantRunRecord, approval assistantSourceApproval, frozen inputResearchRun, mode string) {
	t.Helper()
	binding, err := assistantResearchApprovalBinding(approval)
	assistantRunMust(t, err)
	policies := map[string]assistantResearchSourceDisposition{}
	for _, op := range record.Operations {
		if op.Name != assistantResearchPolicyOperation {
			continue
		}
		var d assistantResearchSourceDisposition
		assistantRunMust(t, readAssistantHostReceipt(op.Intent, &d))
		if d.RunID != record.ID || d.ExecutionKey != record.Binding.ExecutionKey || d.Request.ChatID != record.Binding.Owner.ID || d.Request.TurnID != record.Binding.RequestID || !reflect.DeepEqual(d.Source, binding) {
			t.Fatal("produced policy lost exact execution/source custody")
		}
		readOp := assistantDialectOperation(record, d.ReadOperationID)
		if readOp == nil || readOp.Name != "read_dialect_recipe" || readOp.Kind != "host" || readOp.State != "completed" {
			t.Fatal("produced policy does not reference a completed factory recipe read")
		}
		var in assistantHostIntent
		var result assistantHostResult
		assistantRunMust(t, readAssistantHostReceipt(readOp.Intent, &in))
		assistantRunMust(t, readAssistantHostReceipt(readOp.Result, &result))
		if !in.Root || in.SourceBinding != nil || !assistantParentHostCall(record, readOp.Name, in) || result.DialectRead == nil || !validAssistantDialectActionRead(*result.DialectRead) {
			t.Fatal("produced policy recipe read lost root/local canonical custody")
		}
		if len(policies) == 0 {
			if mode == "grant-after-review-only" {
				if d.Allowance.AllowanceID != "" || d.Allowance.AllowanceRevision != "" {
					t.Fatal("review-only policy acquired an allowance")
				}
			} else if !researchDispositionMatches(d.Allowance, frozen) {
				t.Fatal("first policy did not freeze the exact original ready allowance")
			}
		} else if mode == "exact-once" && d.Allowance.AllowanceID != "" {
			t.Fatal("same-turn original receipt observation prepared another admission")
		}
		policies[op.ID] = d
	}
	wantPolicies := 1
	if len(policies) != wantPolicies {
		t.Fatal("wrong produced policy count", len(policies), wantPolicies)
	}
	claims, dispatches := map[string]bool{}, map[string]bool{}
	for _, op := range record.Operations {
		if op.Name != assistantResearchClaimOperation {
			continue
		}
		var c assistantResearchSourceClaim
		assistantRunMust(t, readAssistantHostReceipt(op.Intent, &c))
		if _, ok := policies[c.PolicyID]; !ok || claims[c.PolicyID] || dispatches[c.DurableDispatchID] || c.CallID == "" || c.CallbackID != "" {
			t.Fatal("produced API claim is foreign, duplicate or has fabricated native custody")
		}
		claims[c.PolicyID], dispatches[c.DurableDispatchID] = true, true
		matched := false
		for _, host := range record.Operations {
			if host.Kind != "host" || host.Name != assistantResearchTool {
				continue
			}
			var in assistantHostIntent
			assistantRunMust(t, readAssistantHostReceipt(host.Intent, &in))
			if in.DurableDispatchID == c.DurableDispatchID && in.CallID == c.CallID && in.Root && assistantParentHostCall(record, host.Name, in) && assistantResearchSourceHostMatches(record, host, in, c.PolicyID) {
				matched = true
			}
		}
		if !matched {
			t.Fatal("produced source claim has no actual canonical root host operation")
		}
	}
	wantClaims := wantPolicies
	if mode == "source-revoked" {
		wantClaims = 0
	}
	if len(claims) != wantClaims {
		t.Fatal("wrong actual policy/claim association count", len(claims), wantClaims)
	}
}

// Import-only adversarial mutation of the actual produced policy, inserted
// before its real claim. It never calls policy/Execute or supplies execution
// evidence; the live duplicate-preparation scenario remains a separate gate.
func assistantResearchSourceIsolatedDuplicatePreparation(t *testing.T, e *assistantRunExecution, record assistantRunRecord) {
	t.Helper()
	t.Run("duplicate-unclaimed-policy-import", func(t *testing.T) {
		var mutated assistantRunRecord
		assistantRunMust(t, json.Unmarshal(assistantChatJSON(record), &mutated))
		for i, op := range mutated.Operations {
			if op.Name != assistantResearchPolicyOperation {
				continue
			}
			duplicate := op
			duplicate.ID, duplicate.CallID = "duplicate-source-policy", "duplicate-source-policy"
			mutated.Operations = append(mutated.Operations[:i+1], append([]assistantRunOperation{duplicate}, mutated.Operations[i+1:]...)...)
			if err := assistantRecipeImport(t, e, mutated); err == nil {
				t.Fatal("duplicate pending policy imported")
			}
			return
		}
		t.Fatal("actual produced policy missing from import mutation basis")
	})
}

// Provider IDs and canonical event IDs are different identity domains. Discover
// calls in journal order, then require exact arguments, closed source, root
// scope and a unique acknowledged call/result pair for each scripted invocation.
func assistantResearchSourceCanonicalCalls(t *testing.T, record assistantRunRecord, approval assistantSourceApproval, args []byte, want int) []string {
	t.Helper()
	binding, err := assistantResearchApprovalBinding(approval)
	assistantRunMust(t, err)
	if record.Canonical == nil || record.Checkpoint == nil {
		t.Fatal("source calls lack canonical journal/checkpoint")
	}
	var ids []string
	seen := map[string]bool{}
	for i, envelope := range record.Events {
		call := envelope.Event.ToolCall
		if call == nil || call.Name != binding.WireName {
			continue
		}
		if call.CallID == nil || *call.CallID == "" {
			t.Fatal("source call lacks canonical correlation", i)
		}
		id := *call.CallID
		if seen[id] {
			t.Fatal("duplicate canonical source call correlation", id)
		}
		seen[id] = true
		if !assistantHostArgumentsMatch(call.Args, args) {
			t.Fatal("canonical source call changed authored arguments", id)
		}
		raw, err := json.Marshal(call.Source)
		assistantRunMust(t, err)
		if !assistantHostSourceCallRecordMatches(binding, raw) {
			t.Fatal("canonical source call changed closed source", id)
		}
		calls, results := 0, 0
		for j, row := range record.Events {
			if other := row.Event.ToolCall; other != nil && other.CallID != nil && *other.CallID == id {
				calls++
			}
			if result := row.Event.ToolResult; result != nil && result.CallID != nil && *result.CallID == id {
				results++
				if j <= i {
					t.Fatal("canonical source result precedes its call", id)
				}
				if result.Name != call.Name {
					t.Fatal("canonical result wire differs from its exact call", id)
				}
				raw, err := json.Marshal(result.Source)
				assistantRunMust(t, err)
				if !assistantHostSourceCallRecordMatches(binding, raw) {
					t.Fatal("canonical source result changed closed source", id)
				}
			}
		}
		if calls != 1 || results != 1 {
			t.Fatal("canonical correlation does not have exactly one call and result", id, calls, results)
		}
		storedCalls, storedResults := 0, 0
		for _, row := range record.Canonical.Events {
			var env runcontext.StoredRunEnvelope
			assistantRunMust(t, json.Unmarshal(row.Bytes, &env))
			isCall := env.Event.ToolCall != nil && env.Event.ToolCall.CallID != nil && *env.Event.ToolCall.CallID == id
			isResult := env.Event.ToolResult != nil && env.Event.ToolResult.CallID != nil && *env.Event.ToolResult.CallID == id
			if !isCall && !isResult {
				continue
			}
			if env.Scope.Execution != record.Checkpoint.ID || env.Scope.Role != "author" || env.Scope.Task != "" {
				t.Fatal("canonical source pair left original root author scope", id, env.Scope)
			}
			if isCall {
				storedCalls++
				if !reflect.DeepEqual(env.Event.ToolCall, call) {
					t.Fatal("projected source call differs from acknowledged canonical call", id)
				}
			}
			if isResult {
				storedResults++
				for _, projected := range record.Events {
					result := projected.Event.ToolResult
					if result != nil && result.CallID != nil && *result.CallID == id && !reflect.DeepEqual(result, env.Event.ToolResult) {
						t.Fatal("projected source result differs from acknowledged canonical result", id)
					}
				}
			}
		}
		if storedCalls != 1 || storedResults != 1 {
			t.Fatal("source pair lacks unique canonical acknowledgements", id, storedCalls, storedResults)
		}
		ids = append(ids, id)
	}
	if len(ids) != want {
		t.Fatal("canonical sourced invocation count differs from authored script", len(ids), want)
	}
	return ids
}

// Check the next actual provider request after each authored step. A provider
// tool result must answer the authored wire ID, while its content agrees with
// that invocation's independently validated canonical result. Do not compare
// provider IDs to canonical correlations or derive the script from captured IDs.
func assistantResearchSourceProviderResults(t *testing.T, arrivals []arrival, record assistantRunRecord, canonicalIDs []string, steps []int, args []byte) {
	t.Helper()
	if len(canonicalIDs) != len(steps) {
		t.Fatal("provider/canonical script cardinality differs")
	}
	for i, step := range steps {
		if step >= len(arrivals) {
			t.Fatal("missing next provider request for scripted tool result", step)
		}
		var request struct {
			Messages []struct {
				Role       string `json:"role"`
				ToolCallID string `json:"tool_call_id"`
				Content    string `json:"content"`
				ToolCalls  []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"messages"`
		}
		assistantRunMust(t, json.Unmarshal(arrivals[step].body, &request))
		var original *contract.ToolResult
		for _, row := range record.Events {
			result := row.Event.ToolResult
			if result != nil && result.CallID != nil && *result.CallID == canonicalIDs[i] {
				if original != nil {
					t.Fatal("ambiguous canonical result for provider check", canonicalIDs[i])
				}
				original = result
			}
		}
		if original == nil {
			t.Fatal("missing canonical result for provider check", canonicalIDs[i])
		}
		id := fmt.Sprintf("source-call-%d", step)
		calls, results, callIndex := 0, 0, -1
		for j, message := range request.Messages {
			for _, call := range message.ToolCalls {
				if call.ID != id {
					continue
				}
				calls++
				callIndex = j
				if message.Role != "assistant" {
					t.Fatal("provider history changed authored call role", id, message.Role)
				}
				if call.Function.Name != original.Name {
					t.Fatal("provider history changed authored call wire", id, call.Function.Name)
				}
				if !assistantHostArgumentsMatch([]byte(call.Function.Arguments), args) {
					t.Fatal("provider history changed authored call arguments", id)
				}
			}
			if message.Role != "tool" || message.ToolCallID != id {
				continue
			}
			results++
			if callIndex < 0 || j <= callIndex {
				t.Fatal("provider result precedes authored provider call", id)
			}
			content := message.Content
			if strings.HasPrefix(content, runcontext.Opening+" ") {
				fields := strings.Fields(content)
				if len(fields) < 4 {
					t.Fatal("malformed provider retrieved-data frame", id)
				}
				marker := fields[3]
				data, ok := runcontext.UnframeData(content, marker)
				if !ok || content != runcontext.FrameData(original.Name, data, marker) {
					t.Fatal("provider result changed source framing", id)
				}
				content = data
			}
			value, err := runcontext.ParseJSON([]byte(content))
			assistantRunMust(t, err)
			if !assistantExternalEventResultEqual(original, runcontext.ReadToolResult(value)) {
				t.Fatal("provider result differs from exact canonical result", id, canonicalIDs[i])
			}
		}
		if calls != 1 || results != 1 {
			t.Fatal("provider history lacks unique authored call/result pair", id, calls, results)
		}
	}
}

// The three model-visible calls share one physical effect. Check the exact
// canonical result and original producer/source rather than counting cards as
// executions. The later call follows another genuine recipe read.
func assistantResearchSourceReplayResults(t *testing.T, record assistantRunRecord, approval assistantSourceApproval, callIDs []string) {
	t.Helper()
	if assistantResearchPolicyPending(record) {
		t.Fatal("durable replay left a pending source policy")
	}
	if len(record.DomainOutputs) != 1 {
		t.Fatal("durable replay changed physical output count", len(record.DomainOutputs))
	}
	output := record.DomainOutputs[0]
	if output.DialectAction == nil || output.DialectAction.Research == nil {
		t.Fatal("original research output missing")
	}
	producer := assistantDialectOperation(record, output.DialectAction.Research.OperationID)
	var request assistantResearchIntent
	if producer == nil {
		t.Fatal("original research producer missing")
	}
	if producer.State != "completed" {
		t.Fatal("original research producer not completed", producer.State)
	}
	assistantRunMust(t, readAssistantHostReceipt(producer.Intent, &request))
	host := assistantDialectOperation(record, request.HostOperationID)
	var in assistantHostIntent
	var result assistantHostResult
	if host == nil {
		t.Fatal("original producer host missing", request.HostOperationID)
	}
	assistantRunMust(t, readAssistantHostReceipt(host.Intent, &in))
	assistantRunMust(t, readAssistantHostReceipt(host.Result, &result))
	if len(callIDs) == 0 || in.CallID != callIDs[0] {
		t.Fatal("original host is not the first canonical source call", in.CallID, callIDs)
	}
	if !in.Root || !assistantCanonicalParentHostCall(record, host.Name, in) {
		t.Fatal("original host lost exact root canonical dispatch/call custody")
	}
	if !assistantResearchSourceHostMatches(record, *host, in, request.SourcePolicyID) {
		t.Fatal("original host lost its original source policy/claim association")
	}
	binding, err := assistantResearchApprovalBinding(approval)
	assistantRunMust(t, err)
	value, err := runcontext.ParseJSON(result.Result)
	assistantRunMust(t, err)
	original := runcontext.ReadToolResult(value)
	counts := map[string]int{}
	for _, envelope := range record.Events {
		event := envelope.Event.ToolResult
		if event == nil || event.CallID == nil || !contains(callIDs, *event.CallID) {
			continue
		}
		if event.Name != binding.WireName {
			t.Fatal("replay result changed source wire", *event.CallID, event.Name)
		}
		if event.Source == nil {
			t.Fatal("replay result lost original source", *event.CallID)
		}
		if !assistantExternalEventResultEqual(event, original) {
			t.Fatal("replay did not return exact original physical result", *event.CallID)
		}
		raw, err := json.Marshal(event.Source)
		assistantRunMust(t, err)
		if !assistantHostSourceCallRecordMatches(binding, raw) {
			t.Fatal("replay altered the original closed source record")
		}
		counts[*event.CallID]++
	}
	for _, id := range callIDs {
		if counts[id] != 1 {
			t.Fatal("missing or duplicate actual replay result", id, counts[id])
		}
	}
	physical, policies, claims, requests := 0, 0, 0, 0
	for _, op := range record.Operations {
		switch {
		case op.Kind == "host" && op.Name == assistantResearchTool:
			physical++
		case op.Name == assistantResearchPolicyOperation:
			policies++
		case op.Name == assistantResearchClaimOperation:
			claims++
		case op.Name == assistantResearchOperation:
			requests++
		}
	}
	if physical != 1 || policies != 1 || claims != 1 || requests != 1 {
		t.Fatal("replay created another physical admission association", physical, policies, claims, requests)
	}
	var imported assistantRunRecord
	assistantRunMust(t, json.Unmarshal(assistantChatJSON(record), &imported))
	policy := assistantDialectOperation(imported, request.SourcePolicyID)
	var d assistantResearchSourceDisposition
	assistantRunMust(t, readAssistantHostReceipt(policy.Intent, &d))
	if replay, err := assistantResearchSourceReplay(imported, d); !replay || err != nil || assistantResearchPolicyPending(imported) {
		t.Fatal("retained import lost the exact replay or left a pending preparation", replay, err)
	}
}

// These are import-only adversarial mutations of actual producer output. None
// calls Execute, prepares approval, supplies a claim or changes expected proof.
func assistantResearchSourceReplayMutations(t *testing.T, record assistantRunRecord) {
	t.Helper()
	if len(record.DomainOutputs) != 1 || record.DomainOutputs[0].DialectAction == nil || record.DomainOutputs[0].DialectAction.Research == nil {
		t.Fatal("mutation basis missing unique original research output")
	}
	producer := assistantDialectOperation(record, record.DomainOutputs[0].DialectAction.Research.OperationID)
	if producer == nil {
		t.Fatal("mutation basis missing original producer")
	}
	var request assistantResearchIntent
	assistantRunMust(t, readAssistantHostReceipt(producer.Intent, &request))
	host := assistantDialectOperation(record, request.HostOperationID)
	if host == nil {
		t.Fatal("mutation basis missing original physical host")
	}
	var original assistantHostIntent
	assistantRunMust(t, readAssistantHostReceipt(host.Intent, &original))
	if original.CallID == "" || original.DurableDispatchID == "" {
		t.Fatal("mutation basis missing canonical call/dispatch identity")
	}
	for _, mode := range []string{"unknown", "claimed", "failed", "no-result", "no-host", "host-refused", "physical-result", "no-claim", "claim-call", "no-output", "duplicate-output", "output-receipt", "policy-source", "final-source", "final-result", "no-final", "duplicate-dispatch", "duplicate-host", "native-callback"} {
		t.Run("replay-"+mode, func(t *testing.T) {
			var run assistantRunRecord
			assistantRunMust(t, json.Unmarshal(assistantChatJSON(record), &run))
			var d assistantResearchSourceDisposition
			for _, op := range run.Operations {
				if op.ID == request.SourcePolicyID {
					assistantRunMust(t, readAssistantHostReceipt(op.Intent, &d))
					break
				}
			}
			if mode == "duplicate-dispatch" {
				for _, dispatch := range run.Checkpoint.Durable.Dispatches {
					if dispatch.ID == original.DurableDispatchID {
						run.Checkpoint.Durable.Dispatches = append(run.Checkpoint.Durable.Dispatches, dispatch)
						break
					}
				}
			}
			for i := range run.Checkpoint.Durable.Dispatches {
				dispatch := &run.Checkpoint.Durable.Dispatches[i]
				if dispatch.ID != original.DurableDispatchID {
					continue
				}
				switch mode {
				case "unknown", "claimed":
					dispatch.State = mode
				case "failed":
					detail := "authored refusal"
					dispatch.Failure = &contract.Failure{Version: 1, Code: "permission", Source: "tool", Outcome: "not-started", Detail: &detail}
				case "no-result":
					dispatch.Result = nil
				}
			}
			for i := range run.Operations {
				op := &run.Operations[i]
				if op.ID == host.ID {
					switch mode {
					case "no-host":
						op.Name = "other-host"
					case "host-refused":
						op.State = "refused"
					case "native-callback":
						var in assistantHostIntent
						assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
						in.CallbackID = "foreign-callback"
						op.Intent = assistantChatJSON(in)
					case "physical-result":
						var result assistantHostResult
						assistantRunMust(t, readAssistantHostReceipt(op.Result, &result))
						result.Result = []byte(`{"content":[]}`)
						op.Result = assistantChatJSON(result)
					}
				}
				if op.Name == assistantResearchClaimOperation {
					var held assistantResearchSourceClaim
					assistantRunMust(t, readAssistantHostReceipt(op.Intent, &held))
					if held.PolicyID != request.SourcePolicyID {
						continue
					}
					switch mode {
					case "no-claim":
						op.Name = "other-claim"
					case "claim-call":
						var claim assistantResearchSourceClaim
						assistantRunMust(t, readAssistantHostReceipt(op.Intent, &claim))
						claim.CallID = "foreign-call"
						op.Intent = assistantChatJSON(claim)
					}
				}
				if op.ID == request.SourcePolicyID && mode == "policy-source" {
					var policy assistantResearchSourceDisposition
					assistantRunMust(t, readAssistantHostReceipt(op.Intent, &policy))
					policy.Source.ApprovalID = strings.Repeat("d", 32)
					op.Intent = assistantChatJSON(policy)
				}
			}
			if mode == "duplicate-host" {
				for _, op := range run.Operations {
					if op.ID == host.ID {
						run.Operations = append(run.Operations, op)
						break
					}
				}
			}
			for i := range run.Events {
				event := run.Events[i].Event.ToolResult
				if event == nil || event.CallID == nil || *event.CallID != original.CallID {
					continue
				}
				switch mode {
				case "final-source":
					event.Source = nil
				case "final-result":
					event.Text = "foreign result"
				case "no-final":
					run.Events[i].Event.ToolResult = nil
				}
			}
			switch mode {
			case "no-output":
				run.DomainOutputs = nil
			case "duplicate-output":
				run.DomainOutputs = append(run.DomainOutputs, run.DomainOutputs[0])
			case "output-receipt":
				run.DomainOutputs[0].DialectAction.Research.Receipt.RunID = "research_99999999999999999999999999999999"
			}
			if replay, err := assistantResearchSourceReplay(run, d); !replay || err == nil {
				t.Fatal("corrupt or uncertain original effect accepted for replay", replay, err)
			}
		})
	}
}

// Changes happen after the original effect and before the actual later sourced
// call. The original allowance is never replaced by saved replay metadata.
func TestAssistantResearchSourceReplayCurrentness(t *testing.T) {
	for _, mode := range []string{"late-grant", "consumed-allowance-revocation-refused", "source-revoked", "configuration-changed", "recipe-changed"} {
		if !t.Run(mode, func(t *testing.T) {
			var saved inputPreparationReply
			var steps atomic.Int32
			var refusedRevokes atomic.Int32
			var change func()
			var runtimeNames []string
			wire := runcontext.WireName("desk-research-workflow", assistantResearchTool)
			u := newUpstream(t, nil)
			u.mu.Lock()
			u.respond = func(w http.ResponseWriter, _ *http.Request) {
				n := steps.Add(1)
				seen := u.arrivals()
				if len(seen) != int(n) || n > 4 {
					t.Error("unexpected replay provider request", n, len(seen))
					http.Error(w, "script exhausted", http.StatusBadRequest)
					return
				}
				assistantResearchSourceIsolatedOffer(t, seen[n-1].body, wire, runtimeNames)
				if n == 3 {
					change()
				}
				finish := "tool_calls"
				var delta map[string]any
				if n <= 3 {
					name, args := wire, assistantResearchArgs(saved)
					if n == 1 {
						name, args = "read_dialect_recipe", string(assistantChatJSON(map[string]string{"id": saved.Document.ID}))
					}
					delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("source-call-%d", n), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}
				} else {
					delta, finish = map[string]any{"role": "assistant", "content": "Report the retained workflow outcome."}, "stop"
				}
				raw, _ := json.Marshal(map[string]any{"id": "source-replay-fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: "+string(raw)+"\n\ndata: {\"id\":\"source-replay-fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\""+finish+"\"}]}\n\ndata: [DONE]\n\n")
			}
			u.mu.Unlock()
			completed := make(chan struct{})
			s, token, e := assistantResearchSourceBrowserFixtureWithCompletion(t, u.server.URL, completed)
			seeded, rr, run := assistantResearchSeed(t, s, token, e, mode != "late-grant")
			saved = seeded
			approval := assistantResearchSourceApprovalFixture(t, s, token)
			x := assistantExecutionOpen(t, s, e)
			owner := &assistantResearchTestOwner{journal: s.researchJournal(rr, run.ID)}
			s.researchJournals = owner
			change = func() {
				switch mode {
				case "late-grant":
					w := assistantResearchHTTP(t, s, token, "/api/input-preparations/"+run.PreparationID+"/research/"+run.ID+"/authorize", saved.SHA256, fmt.Sprintf(`{"chatId":%q,"minutes":60}`, e.record.Binding.Owner.ID))
					if w.Code != http.StatusOK {
						t.Error("late actual grant refused", w.Code, w.Body.String())
					}
				case "consumed-allowance-revocation-refused":
					before, err := owner.journal.Read(t.Context())
					assistantRunMust(t, err)
					if before.Approval == nil || before.Approval.UsedAt == "" || before.Approval.RevokedAt != "" {
						t.Fatal("original effect did not retain an unrevoked consumed allowance")
					}
					beforeJournal, beforeReceipt := assistantChatJSON(before), assistantChatJSON(before.Approval)
					beforeUpdates, beforeQueued := owner.calls.Load(), owner.queued.Load()
					w := policyCall(s, token, "POST", "/api/input-research-approvals/"+e.record.Binding.Owner.ID+"/revoke", map[string]string{"runId": run.ID})
					if w.Code != http.StatusConflict {
						t.Fatal("consumed allowance revocation was not refused", w.Code, w.Body.String())
					}
					refusedRevokes.Add(1)
					after, err := owner.journal.Read(t.Context())
					assistantRunMust(t, err)
					// The refused Update is an attempted mutation, never a successful
					// journal change or a second admission of physical work.
					if !bytes.Equal(beforeJournal, assistantChatJSON(after)) || !bytes.Equal(beforeReceipt, assistantChatJSON(after.Approval)) || owner.calls.Load() != beforeUpdates+1 || owner.queued.Load() != beforeQueued {
						t.Fatal("refused revoke changed the exact consumed receipt, journal, or physical effect")
					}
				case "source-revoked":
					w := policyCall(s, token, "POST", "/api/assistant-source-approvals/"+e.record.Binding.Owner.ID+"/revoke", assistantSourceRevokeRequest{approval.ID, approval.Revision})
					if w.Code != http.StatusOK {
						t.Error("actual source revocation refused", w.Code, w.Body.String())
					}
				case "configuration-changed":
					writeDeskConfig(t, s, researchDeskFile("https://example.invalid/changed"))
				case "recipe-changed":
					document := saved.Document
					document.Name = "Changed after original sourced effect"
					_, err := s.preparationRepository().CompareAndSwap(t.Context(), document, saved.SHA256)
					assistantRunMust(t, err)
				}
			}
			policy := e.assistantResearchSourcePolicy(approval)
			var policyCalls atomic.Int32
			isolated, identity := assistantResearchSourceIsolatedRun(t, e, x, approval, func(call runcontext.ApprovalRequest) string {
				policyCalls.Add(1)
				return policy(call)
			})
			for _, tool := range isolated.Tools {
				runtimeNames = append(runtimeNames, tool.Name)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			assistantRunMust(t, x.Adapter.Run(ctx, isolated))
			identity()
			if steps.Load() != 4 {
				t.Fatal("actual replay script did not finish", steps.Load())
			}
			wantRefusedRevokes := int32(0)
			if mode == "consumed-allowance-revocation-refused" {
				wantRefusedRevokes = 1
			}
			if refusedRevokes.Load() != wantRefusedRevokes {
				t.Fatal("unexpected refused consumed-allowance revocation attempts", refusedRevokes.Load())
			}
			got, err := owner.journal.Read(t.Context())
			assistantRunMust(t, err)
			queued := int32(1)
			if mode == "late-grant" {
				queued = 0
				if got.Approval == nil || got.Approval.UsedAt != "" || researchApprovalState(got, time.Now()) != "ready" {
					t.Fatal("review-only replay consumed or changed a later grant")
				}
			} else if got.Approval == nil || got.Approval.UsedAt == "" {
				t.Fatal("original effect did not consume its original allowance")
			}
			if owner.queued.Load() != queued || len(got.Attempts) != 0 {
				t.Fatal("replay queued another effect or started worker work", owner.queued.Load(), queued)
			}
			record := assistantExecutionRecord(t, e)
			assistantRunMust(t, assistantRecipeImport(t, e, record))
			callIDs := assistantResearchSourceCanonicalCalls(t, record, approval, []byte(assistantResearchArgs(saved)), 2)
			assistantResearchSourceProviderResults(t, u.arrivals(), record, callIDs, []int{2, 3}, []byte(assistantResearchArgs(saved)))
			wantCalls := callIDs[:1]
			wantPolicyCalls := int32(1)
			if mode == "late-grant" || mode == "consumed-allowance-revocation-refused" {
				wantCalls = callIDs
				wantPolicyCalls = 2
			} else {
				refused := 0
				for _, envelope := range record.Events {
					if event := envelope.Event.ToolResult; event != nil && event.CallID != nil && *event.CallID == callIDs[1] && event.IsError {
						refused++
					}
				}
				if refused != 1 {
					t.Fatal("changed current source/recipe returned an old successful result", refused)
				}
				if mode == "recipe-changed" {
					wantPolicyCalls = 2
				}
			}
			if policyCalls.Load() != wantPolicyCalls {
				t.Fatal("unexpected actual replay policy calls", policyCalls.Load(), wantPolicyCalls)
			}
			assistantResearchSourceReplayResults(t, record, approval, wantCalls)
			if mode == "late-grant" && record.DomainOutputs[0].DialectAction.Research.Receipt.Status != "review-required" {
				t.Fatal("late grant upgraded the original review-only result")
			}
			// The adapter has completed its canonical checkpoint, while the original
			// owning Execute is still open. Projection must retain this distinction.
			currentIdentity, _, err := s.assistantRunIdentity(e.request.Clone(t.Context()))
			assistantRunMust(t, err)
			if record.State != "running" || record.Checkpoint == nil || record.Checkpoint.Status != "complete" || record.ID != e.record.ID || !reflect.DeepEqual(record.Binding, e.record.Binding) || !assistantRunIdentityMatches(currentIdentity, record.Binding) || record.Historical || record.Binding.Project != e.writer.lease.store.project || record.Binding.Owner.Kind != "chat" || record.Canonical == nil || record.Canonical.Head.RunID != e.canonicalRunID() {
				t.Fatal("fixture lost the admitted running owner or completed canonical checkpoint")
			}
			assistantRunMust(t, validateAssistantCanonicalProjection(record))
			e.service.mu.Lock()
			active := e.service.active[record.ID] == e
			e.service.mu.Unlock()
			e.canonicalMu.Lock()
			generation := e.generation
			running := e.lifecycle == "running" && !e.stopRequested && e.ctx.Err() == nil
			e.canonicalMu.Unlock()
			if !active || !running {
				t.Fatal("fixture owner retired before its completion signal")
			}
			before, beforeOutputs := assistantChatJSON(record.Operations), assistantChatJSON(record.DomainOutputs)
			beforeJournal, beforeUpdates := assistantChatJSON(got), owner.calls.Load()
			close(completed)
			select {
			case <-e.done:
			case <-time.After(6 * time.Second):
				t.Fatal("original completed service execution did not retire")
			}
			retired := assistantExecutionRecord(t, e)
			if retired.State != "complete" || retired.Checkpoint == nil || retired.Checkpoint.Status != "complete" || retired.ID != record.ID || !reflect.DeepEqual(retired.Binding, record.Binding) || retired.Canonical == nil || retired.Canonical.Head.RunID != record.Canonical.Head.RunID || !bytes.Equal(before, assistantChatJSON(retired.Operations)) || !bytes.Equal(beforeOutputs, assistantChatJSON(retired.DomainOutputs)) {
				t.Fatal("normal owner completion changed immutable identity or physical workflow history")
			}
			assistantRunMust(t, validateAssistantCanonicalProjection(retired))
			assistantRunMust(t, assistantRecipeImport(t, e, retired))
			e.service.mu.Lock()
			active = e.service.active[record.ID] != nil
			e.service.mu.Unlock()
			e.canonicalMu.Lock()
			closed := e.lifecycle == "closed" && e.generation == generation && e.stopRequested && e.ctx.Err() != nil
			e.canonicalMu.Unlock()
			if active || !closed {
				t.Fatal("completed fixture retained an active owner or generation")
			}
			retiredJournal, err := owner.journal.Read(t.Context())
			assistantRunMust(t, err)
			if !bytes.Equal(beforeJournal, assistantChatJSON(retiredJournal)) || owner.calls.Load() != beforeUpdates || owner.queued.Load() != queued || steps.Load() != 4 || len(u.arrivals()) != 4 || policyCalls.Load() != wantPolicyCalls {
				t.Fatal("normal owner completion started new research or provider work")
			}
			record = retired
			reconstructed := &assistantRunExecution{service: &assistantRunService{server: s}, request: e.request.Clone(t.Context()), record: record}
			assistantRunMust(t, reconstructed.ProjectChat(t.Context()))
			assistantRunMust(t, reconstructed.ProjectChat(t.Context()))
			after := assistantExecutionRecord(t, e)
			assistantRunMust(t, assistantRecipeImport(t, e, after))
			afterJournal, err := owner.journal.Read(t.Context())
			assistantRunMust(t, err)
			if after.State != "complete" || after.ChatProjection == nil || !after.ChatProjection.Acknowledged || !bytes.Equal(before, assistantChatJSON(after.Operations)) || !bytes.Equal(beforeOutputs, assistantChatJSON(after.DomainOutputs)) || !bytes.Equal(beforeJournal, assistantChatJSON(afterJournal)) || owner.calls.Load() != beforeUpdates || owner.queued.Load() != queued || steps.Load() != 4 || len(u.arrivals()) != 4 || policyCalls.Load() != wantPolicyCalls || assistantResearchPolicyPending(after) || len(after.DomainOutputs) != 1 {
				t.Fatal("reconstruction turned replay history into new authority or work")
			}
		}) {
			return
		}
	}
}
