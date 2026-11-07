package desk

import (
	"context"
	"encoding/json"
	"errors"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This is a production-construction gate: full saved-chat host registry, real
// Eino adapter, actual HTTP relay, and test-owned Runtime process. It deliberately
// does not narrow Purpose/the registry to bypass upstream context graph defects.
// Run separately after the released graph/factory dependency gate is satisfied.
func TestAssistantRetainedProductionAPIApproval(t *testing.T) {
	for _, approved := range []bool{true, false} {
		name := "approve"
		if !approved {
			name = "decline"
		}
		t.Run(name, func(t *testing.T) {
			var requests, completed atomic.Int32
			u := newUpstream(t, func(w http.ResponseWriter, request *http.Request) {
				if requests.Add(1) == 1 {
					assistantPolicySSE(w, "experimental_evaluate", json.RawMessage(`{"rehearsal":false}`), "")
					return
				}
				assistantPolicySSE(w, "", nil, "The explicit Runtime decision is complete.")
			})
			opened := make(chan *assistantExecution, 1)
			s, token, e := assistantExecutionFixtureSource(t, u.server.URL, "work", func(ctx context.Context, e *assistantRunExecution) error {
				x, err := e.openAssistantExecution(ctx, assistantExecutionPrompt{Name: "served"})
				if err != nil {
					return err
				}
				opened <- x
				defer x.Close()
				executeErr := x.execute(ctx, func(ctx context.Context) error {
					completed.Add(1)
					return e.completeAssistantConversation(ctx)
				})
				if executeErr != nil {
					t.Logf("retained execute failure=%T scope=%t dispatch=%t invalid=%t canceled=%t deadline=%t", executeErr, errors.Is(executeErr, errAssistantRunScope), errors.Is(executeErr, errAssistantRunDispatch), errors.Is(executeErr, errAssistantRunInvalid), errors.Is(executeErr, context.Canceled), errors.Is(executeErr, context.DeadlineExceeded))
					// Observe before deferred Close and the owner finalizer rewrite the pause.
					assistantRetainedProductionSnapshot(t, e, x, requests.Load())
				}
				return executeErr
			}, func(s *Server, _ string, start assistantRunStart) assistantRunStart {
				s.assistantRunExecutor = &assistantCanonicalProtocolExecutor{s.assistantRunExecutor.(*assistantProtocolExecutor)}
				assistantP2ConfigureRuntime(t, s)
				return start
			})
			var held *assistantExecution
			select {
			case held = <-opened:
			case <-e.done:
				t.Fatalf("production opening failed: %+v", assistantExecutionRecord(t, e).State)
			case <-time.After(5 * time.Second):
				t.Fatal("production execution did not open")
			}
			defer func() {
				if t.Failed() {
					assistantRetainedProductionSnapshot(t, e, held, requests.Load())
				}
			}()
			paused := assistantRetainedWaitParked(t, s, token, e)
			before := assistantExecutionRecord(t, e)
			if paused.Checkpoint.Approval == nil || paused.Checkpoint.Approval.Runtime == nil || !held.notices.parked(held.invocation.generation) {
				t.Fatal("actual Runtime ask was not joined and retained")
			}
			e.canonicalMu.Lock()
			parked := e.lifecycle == "parked" && e.retainedRuntime == held.runtime
			originalBinding, originalStore := e.canonicalBinding, e.canonicalStore
			e.canonicalMu.Unlock()
			if !parked || !e.runtimeContinuationAlive(held.runtime) || completed.Load() != 0 {
				t.Fatal("owner/provider was lost or completion ran at pause")
			}
			methodCalls := assistantRetainedMethodCalls(t, before)
			// Initial method discovery lists served, then critic discovery lists
			// test_pack (absent from this peer, so it performs no second get).
			if methodCalls["prompts/list"] != 2 || methodCalls["prompts/get"] != 1 || methodCalls["describe_runtime"] != 1 || held.methodText == "" || !held.methodCriticReady || held.methodCriticText != "" || held.methodCriticText != held.Run.TestPrompt || strings.Contains(held.methodText, "THE PERSON'S MESSAGE") || held.methodOwner != held.runtime.owner || !reflect.DeepEqual(held.methodRuntimeIdentity, held.Run.Context.Runtime) {
				t.Fatalf("original immutable Runtime method evidence missing or includes dynamic chat context: methodCalls=%v methodEmpty=%t methodContainsDynamicMarker=%t methodOwnerMatches=%t runtimeIdentityMatches=%t", methodCalls, held.methodText == "", strings.Contains(held.methodText, "THE PERSON'S MESSAGE"), held.methodOwner == held.runtime.owner, reflect.DeepEqual(held.methodRuntimeIdentity, held.Run.Context.Runtime))
			}
			// Gate prepares the reviewed arguments before asking. The released
			// rehearsal contract must hold before this explicit decision as well
			// as at the physical Runtime call below.
			reviewedArgs, err := json.Marshal(paused.Checkpoint.Approval.Args)
			assistantRunMust(t, err)
			if string(reviewedArgs) != `{"rehearsal":true}` {
				t.Fatal("Runtime approval did not retain the exact reviewed rehearsal arguments")
			}
			assistantRetainedMethodRefusals(t, e, held)
			// Observe the actual successor error before owner finalization. This
			// wrapper delegates unchanged input/context to the retained callback.
			e.canonicalMu.Lock()
			resume := e.resumeInvocation
			e.resumeInvocation = func(ctx context.Context, next *runcontext.Input) error {
				err := resume(ctx, next)
				if err != nil {
					t.Logf("retained successor error=%T scope=%t dispatch=%t invalid=%t canceled=%t deadline=%t", err, errors.Is(err, errAssistantRunScope), errors.Is(err, errAssistantRunDispatch), errors.Is(err, errAssistantRunInvalid), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded))
					assistantRetainedProductionSnapshot(t, e, held, requests.Load())
				}
				return err
			}
			e.canonicalMu.Unlock()
			decision := assistantRunAction{ID: "production-explicit-decision", Kind: "approval", Revision: paused.Revision, CallID: paused.Checkpoint.Approval.CallID, ArgsDigest: paused.ApprovalReference.ArgsDigest, ShownDigest: &paused.ApprovalReference.ArgsDigest, Approved: &approved}
			assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", decision), 200)
			assistantProtocolWaitWithin(t, s, token, e.record.ID, "complete", 15*time.Second)
			after := assistantExecutionRecord(t, e)
			if !reflect.DeepEqual(methodCalls, assistantRetainedMethodCalls(t, after)) {
				t.Fatal("approval reopening redispatched immutable Runtime method/identity reads")
			}
			if requests.Load() != 2 || completed.Load() != 1 || len(after.Actions) != 1 || after.Checkpoint.ID != before.Checkpoint.ID || len(after.Canonical.Commits) < len(before.Canonical.Commits) || !reflect.DeepEqual(after.Canonical.Commits[:len(before.Canonical.Commits)], before.Canonical.Commits) {
				t.Fatal("explicit continuation replayed, lost completion, or reset the journal")
			}
			e.canonicalMu.Lock()
			sameStore := e.canonicalStore == originalStore && e.canonicalBinding != originalBinding && e.canonicalBinding.Writer.Store == originalBinding.Writer.Store && e.canonicalBinding.Writer.ID == originalBinding.Writer.ID && e.generation == held.invocation.generation+1
			e.canonicalMu.Unlock()
			if !sameStore || held.gate.Calls() != held.Run.Gate {
				t.Fatal("continuation changed original owner authority")
			}
			// Inspect the actual next HTTP request. A fixed peer completion alone does
			// not prove that the approved/refused result was delivered to the model.
			arrivals := u.arrivals()
			if len(arrivals) != 2 || arrivals[0].method != http.MethodPost || arrivals[1].method != http.MethodPost {
				t.Fatal("expected exactly two physical API POST requests")
			}
			// newUpstream retains the exact body before invoking its response
			// callback, where request.Body has already reached EOF.
			nextRequest := arrivals[1].body
			var payload any
			assistantRunMust(t, json.Unmarshal(nextRequest, &payload))
			var stringsInRequest []string
			var visit func(any)
			visit = func(value any) {
				switch v := value.(type) {
				case string:
					stringsInRequest = append(stringsInRequest, v)
				case []any:
					for _, item := range v {
						visit(item)
					}
				case map[string]any:
					for _, item := range v {
						visit(item)
					}
				}
			}
			visit(payload)
			methodSeen, requestSeen := false, false
			for _, text := range stringsInRequest {
				methodSeen = methodSeen || strings.Contains(text, held.methodText)
				requestSeen = requestSeen || strings.Contains(text, "Review exact input")
			}
			if !methodSeen || !requestSeen {
				t.Fatal("continued actual API request lost method guidance or rebuilt request context")
			}
			var delivered struct {
				CallID   string                            `json:"callId"`
				Approved bool                              `json:"approved"`
				Result   *contract.ToolResultMCPToolResult `json:"result"`
			}
			found := false
			for _, body := range stringsInRequest {
				at := strings.Index(body, runcontext.ApprovalContinuation)
				if at < 0 {
					continue
				}
				framed := strings.TrimPrefix(body[at+len(runcontext.ApprovalContinuation):], "\n")
				opening := strings.IndexByte(framed, '\n')
				if opening < 0 {
					continue
				}
				if json.NewDecoder(strings.NewReader(framed[opening+1:])).Decode(&delivered) == nil {
					found = true
					break
				}
			}
			if !found || delivered.Result == nil || after.Checkpoint.Approval == nil || after.Checkpoint.Approval.Result == nil || delivered.CallID != after.Checkpoint.Approval.CallID || delivered.Approved != approved {
				t.Fatal("actual API next request omitted the exact approval decision/result block")
			}
			expected := after.Checkpoint.Approval.Result
			if approved && (len(expected.Content) != 1 || expected.Content[0].Option1 == nil || expected.Content[0].Option1.Text == nil || *expected.Content[0].Option1.Text != "Exact fixture evaluation result.") {
				t.Fatal("canonical approval result changed the handwritten physical result")
			}
			if len(delivered.Result.Content) != len(expected.Content) {
				t.Fatal("actual API next request changed result content")
			}
			for index, part := range expected.Content {
				if part.Option1 != nil && part.Option1.Text != nil {
					got := delivered.Result.Content[index].Option1
					if got == nil || got.Text == nil || *got.Text != *part.Option1.Text {
						t.Fatal("actual API next request changed original result text")
					}
				}
			}
			physicalActs := 0
			for _, op := range after.Operations {
				if op.Kind == "runtime" && op.Name == "tools/call" {
					var call struct {
						Params struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						} `json:"params"`
					}
					assistantRunMust(t, json.Unmarshal(op.Intent, &call))
					if call.Params.Name == "experimental_evaluate" {
						physicalActs++
						if op.State != "completed" || len(op.Result) == 0 || string(call.Params.Arguments) != `{"rehearsal":true}` {
							t.Fatal("Runtime act lacked its exact result receipt")
						}
						var receipt struct {
							Result *contract.ToolResultMCPToolResult `json:"result"`
						}
						assistantRunMust(t, json.Unmarshal(op.Result, &receipt))
						if receipt.Result == nil || len(receipt.Result.Content) != 1 || receipt.Result.Content[0].Option1 == nil || receipt.Result.Content[0].Option1.Text == nil || *receipt.Result.Content[0].Option1.Text != "Exact fixture evaluation result." {
							t.Fatal("physical Runtime receipt changed the handwritten result")
						}
					}
				}
			}
			want := 0
			if approved {
				want = 1
			}
			if physicalActs != want {
				t.Fatalf("physical act count %d, want %d", physicalActs, want)
			}
		})
	}
}

// Count the actual original Runtime metadata receipts. Mutable tool reads are
// intentionally excluded; the retained correction may not cache their results.
func assistantRetainedMethodCalls(t *testing.T, run assistantRunRecord) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, operation := range run.Operations {
		if operation.Kind != "runtime" {
			continue
		}
		name := operation.Name
		if name == "tools/call" {
			var request struct {
				Params struct {
					Name string `json:"name"`
				} `json:"params"`
			}
			assistantRunMust(t, json.Unmarshal(operation.Intent, &request))
			name = request.Params.Name
		}
		if name != "prompts/list" && name != "prompts/get" && name != "describe_runtime" {
			continue
		}
		if operation.State != "completed" || len(operation.Result) == 0 {
			t.Fatal("method/identity read lacks original acknowledged receipt", name, operation.State)
		}
		counts[name]++
	}
	return counts
}

// Eligibility probes use the actual parked owner and its acknowledged original
// objects. They observe refusal only, without installing a continuation, issuing
// an action, editing checkpoint state, or claiming another provider journey.
func assistantRetainedMethodRefusals(t *testing.T, e *assistantRunExecution, held *assistantExecution) {
	t.Helper()
	before := assistantExecutionRecord(t, e)
	input := &runcontext.Input{Persistence: held.invocation.binding, Runtime: cloneAssistantRuntimeIdentity(held.Run.Context.Runtime)}
	text, err := e.retainedAssistantMethod(t.Context(), held, held.prompt, input)
	assistantRunMust(t, err)
	if text != held.methodText {
		t.Fatal("exact original owner did not retain method guidance")
	}
	for _, fault := range []string{"prompt-name", "prompt-arguments", "source", "owner-object", "owner-identity", "runtime-identity", "missing-method", "missing-critic-capture", "not-retained", "cancelled-caller"} {
		t.Run("retained-method/"+fault, func(t *testing.T) {
			candidate := *held
			prompt := cloneAssistantExecutionPrompt(held.prompt)
			prepared := *input
			ctx := t.Context()
			switch fault {
			case "prompt-name":
				prompt.Name = "another-method"
			case "prompt-arguments":
				prompt.Arguments = map[string]string{"input": "another-request"}
			case "source":
				candidate.methodSource = "another-source"
			case "owner-object":
				owner := *held.methodOwner
				candidate.methodOwner = &owner
			case "owner-identity":
				candidate.methodOwnerIdentity.ProcessID = "another-launch"
			case "runtime-identity":
				prepared.Runtime = &runcontext.RuntimeIdentity{Identity: "another-artifact", Version: "99.0.0"}
			case "missing-method":
				candidate.methodText = ""
			case "missing-critic-capture":
				candidate.methodCriticReady = false
			case "not-retained":
				candidate.retained = false
			case "cancelled-caller":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			_, err := e.retainedAssistantMethod(ctx, &candidate, prompt, &prepared)
			if fault == "cancelled-caller" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("caller cancellation was swallowed", err)
				}
			} else {
				var typed *failure.Error
				if !errors.As(err, &typed) || typed.Failure.Code != "stale-state" || typed.Failure.Source != "context" || typed.Failure.Outcome != "not-started" || typed.Failure.Detail == nil || !strings.HasPrefix(*typed.Failure.Detail, "RetainedRuntimeGuidanceChanged:") {
					t.Fatal("stale retained method lacked named pre-dispatch refusal", err)
				}
			}
			after := assistantExecutionRecord(t, e)
			if !reflect.DeepEqual(before.Canonical, after.Canonical) || !reflect.DeepEqual(before.Operations, after.Operations) || after.Checkpoint.Status != "paused" || after.Checkpoint.Approval == nil || after.Checkpoint.Approval.State != "asked" {
				t.Fatal("eligibility probe changed paused canonical evidence or dispatched work")
			}
		})
	}
}

// Failure-only structural evidence from the actual retained production fixture.
// Never print request/response bytes, checkpoint payloads or provider prose.
func assistantRetainedProductionSnapshot(t *testing.T, e *assistantRunExecution, held *assistantExecution, requests int32) {
	t.Helper()
	e.canonicalMu.Lock()
	lifecycle, stopped, generation := e.lifecycle, e.stopRequested, e.generation
	settlement, resume, retained := e.settlement, e.resumeInvocation != nil, e.retainedRuntime != nil
	currentBinding := e.canonicalBinding
	e.canonicalMu.Unlock()
	t.Logf("retained snapshot requests=%d lifecycle=%s stopped=%t generation=%d settlement=%t settlementSafe=%t resume=%t retainedRuntime=%t writerError=%T", requests, lifecycle, stopped, generation, settlement != nil, settlement.safe(nil), resume, retained, e.writer.Err())
	if held != nil && held.invocation != nil {
		s := held.invocation
		s.mu.Lock()
		models := append([]*assistantModelDispatch(nil), s.models...)
		t.Logf("retained invocation started=%t returned=%t closed=%t ended=%t active=%d failure=%T models=%d natives=%d", s.started, s.returned, s.closed, s.ended, s.active, s.failure, len(models), len(s.natives))
		s.mu.Unlock()
		_, proofErr := s.proof()
		e.canonicalMu.Lock()
		bindingCurrent := e.canonicalBinding == s.binding
		e.canonicalMu.Unlock()
		held.notices.mu.Lock()
		t.Logf("retained notices current=%t retired=%t active=%d failure=%T", held.notices.current == s, held.notices.retired == s, held.notices.active, held.notices.failure)
		held.notices.mu.Unlock()
		t.Logf("retained admission proof=%t noticesParked=%t writerFailed=%t bindingCurrent=%t generationCurrent=%t ownerCanceled=%t invocationCanceled=%t", proofErr == nil, held.notices.parked(s.generation), s.binding.Writer.Failed(), bindingCurrent, generation == s.generation, e.ctx.Err() != nil, s.ctx.Err() != nil)
		for i, d := range models {
			if i == 8 {
				t.Logf("retained model summaries omitted=%d", len(models)-i)
				break
			}
			t.Logf("retained model index=%d admitted=%t uncertain=%t handlerReturned=%t consumerClosed=%t consumerCloseFailed=%t receiptAcknowledged=%t physicalEOF=%t physicalClosed=%t physicalCloseFailed=%t", i, d.admitted.Load(), d.uncertain.Load(), d.handlerReturned.Load(), d.consumerClosed.Load(), d.consumerCloseFailed.Load(), d.receiptAcknowledged.Load(), d.physicalEOF.Load(), d.physicalClosed.Load(), d.physicalCloseFailed.Load())
		}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(e.ctx), time.Second)
	defer cancel()
	record, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		t.Logf("retained record readError=%T", err)
		return
	}
	status, approval, runtime := "", "", false
	pendingHandoff := false
	dispatches, operations := map[string]int{}, map[string]int{}
	if cp := record.Checkpoint; cp != nil {
		status = cp.Status
		if cp.Approval != nil {
			approval, runtime = cp.Approval.State, cp.Approval.Runtime != nil
		}
		if cp.Durable != nil {
			pendingHandoff = cp.Durable.PendingHandoff != nil
			for _, row := range cp.Durable.Dispatches {
				dispatches[row.Kind+"/"+row.State]++
			}
		}
	}
	for _, op := range record.Operations {
		operations[op.Kind+"/"+op.State]++
		if op.Kind == "runtime" && op.Name == "tools/call" {
			var call struct {
				Params struct {
					Name string `json:"name"`
				} `json:"params"`
			}
			if json.Unmarshal(op.Intent, &call) == nil && call.Params.Name == "experimental_evaluate" {
				t.Logf("retained physical Runtime act state=%s resultPresent=%t", op.State, len(op.Result) != 0)
			}
		}
	}
	t.Logf("retained record state=%s checkpoint=%s approval=%s runtimeApproval=%t pendingHandoff=%t dispatches=%v operations=%v", record.State, status, approval, runtime, pendingHandoff, dispatches, operations)
	if currentBinding != nil {
		acknowledged := currentBinding.Writer.Checkpoint()
		t.Logf("retained current canonical writerFailed=%t projectionValid=%t checkpointEqual=%t checkpointDifferentFields=%v", currentBinding.Writer.Failed(), validateAssistantCanonicalProjection(record) == nil, sameAssistantRunCheckpoint(acknowledged, record.Checkpoint), assistantRetainedCheckpointDifferentFields(acknowledged, record.Checkpoint))
	}
	if record.Canonical == nil {
		return
	}
	types, failures := map[string]int{}, 0
	for _, row := range record.Canonical.Events {
		var envelope struct {
			Event json.RawMessage `json:"event"`
		}
		var event struct {
			Type    string                                  `json:"type"`
			Failure *struct{ Code, Source, Outcome string } `json:"failure"`
		}
		if json.Unmarshal(row.Bytes, &envelope) != nil || len(envelope.Event) == 0 || json.Unmarshal(envelope.Event, &event) != nil {
			types["unreadable"]++
			continue
		}
		types[event.Type]++
		if event.Failure != nil {
			failures++
			if failures <= 8 {
				t.Logf("retained failure code=%.80s source=%.80s outcome=%.80s", event.Failure.Code, event.Failure.Source, event.Failure.Outcome)
			}
		}
	}
	t.Logf("retained canonical eventTypes=%v failures=%d", types, failures)
}

// Field names alone locate a divergent checkpoint without exposing retained data.
func assistantRetainedCheckpointDifferentFields(a, b *contract.Checkpoint) []string {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	var l, r map[string]json.RawMessage
	if json.Unmarshal(left, &l) != nil || json.Unmarshal(right, &r) != nil {
		return []string{"unreadable"}
	}
	names := map[string]bool{}
	for name := range l {
		names[name] = true
	}
	for name := range r {
		names[name] = true
	}
	var different []string
	for name := range names {
		if string(l[name]) != string(r[name]) {
			different = append(different, name)
		}
	}
	sort.Strings(different)
	if len(different) > 16 {
		different = append(different[:16], "omitted")
	}
	return different
}
