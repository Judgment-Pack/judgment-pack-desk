package desk

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func assistantNativePlanNotice(execution string, ref *assistantPlanDecisionReference) contract.RunEvent {
	purpose := "plan-drift"
	return contract.RunEvent{ApprovalRequested: &contract.RunEventApprovalRequested{Purpose: &purpose, CallID: ref.CallID, Plan: &contract.RunEventApprovalRequestedPlan{Execution: execution, Revision: ref.PlanRevision, CandidateRevision: ref.CandidateRevision}}}
}

func TestAssistantNativePlanStopDistinctCallbackIdentity(t *testing.T) {
	ref := &assistantPlanDecisionReference{"person-plan-decision", "sha256:" + strings.Repeat("a", 64), "sha256:" + strings.Repeat("b", 64)}
	for _, fault := range []string{"exact", "wrong-alias", "domain-description", "wrong-execution", "wrong-purpose", "malformed-revision", "cancelled-parent"} {
		t.Run(fault, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stop := &assistantAgentStop{parent: ctx, execution: "original-execution"}
			tools := []adapter.AgentTool{{Name: "desk_package_plan", Description: "update_plan: track reviewed work"}}
			if fault == "domain-description" {
				tools[0].Description = "read_document: update_plan: source data"
			}
			alias := tools[0].Name
			if fault == "wrong-alias" {
				alias = "unoffered-alias"
			}
			stop.begin("native-call", assistantNativePlanAlias(tools, alias))
			notice := assistantNativePlanNotice(stop.execution, ref)
			switch fault {
			case "wrong-execution":
				notice.ApprovalRequested.Plan.Execution = "another-execution"
			case "wrong-purpose":
				purpose := "host-action"
				notice.ApprovalRequested.Purpose = &purpose
			case "malformed-revision":
				notice.ApprovalRequested.Plan.CandidateRevision = "unknown"
			case "cancelled-parent":
				cancel()
			}
			stop.observe(notice)
			actual := stop.planFor("native-call", tools[0].Name)
			if (actual != nil) != (fault == "exact") || actual != nil && !reflect.DeepEqual(actual, ref) {
				t.Fatal("Plan candidate crossed callback/alias/execution/cancellation boundary", fault, actual)
			}
			if stop.approvalFor("native-call") {
				t.Fatal("Plan candidate acquired action-approval stop identity")
			}
		})
	}
	// Existing action approvals still require their exact native callback ID.
	stop := &assistantAgentStop{parent: t.Context()}
	stop.begin("native-act")
	stop.observe(contract.RunEvent{ApprovalRequested: &contract.RunEventApprovalRequested{CallID: "native-act"}})
	if !stop.approvalFor("native-act") || stop.planFor("native-act", "native-act") != nil {
		t.Fatal("action approval qualification changed")
	}
}

// This is an independently authored receipt seam in the real admitted store.
// It is not provider-release or end-to-end evidence; the actual native journey
// in assistant_native_compat_test.go supplies that separate positive witness.
func assistantNativePlanProofFixture(t *testing.T) (*assistantNativeSettlement, assistantRunOperation, context.Context) {
	t.Helper()
	_, _, e := assistantExecutionFixtureSource(t, "https://example.invalid", "work", nil, func(s *Server, token string, start assistantRunStart) assistantRunStart {
		start = assistantPlanPrepareSource(t)(s, token, start)
		s.assistantRunExecutor = &assistantPlanBindingExecutor{s.assistantRunExecutor.(*assistantProtocolExecutor)}
		start, _ = assistantBindingHistorical(t, s, token, start)
		return start
	})
	ctx := e.generationContext(t.Context(), e.generation)
	scope := newAssistantNativeSettlement(ctx, e)
	model, err := e.boundIntent(ctx, "model", e.record.Binding.Model, func(string) ([]byte, error) {
		return assistantChatJSON(adapter.AgentRequest{Model: e.record.Binding.Model, Prompt: "Handwritten original model receipt fixture."}), nil
	})
	assistantRunMust(t, err)
	assistantRunMust(t, e.Dispatch(ctx, model.ID))
	callback, err := e.boundIntent(ctx, "callback", "desk_package_plan", func(string) ([]byte, error) {
		return []byte(`{"invocation":"original-model","call":{"id":"native-call","name":"desk_package_plan","arguments":{}}}`), nil
	})
	assistantRunMust(t, err)
	assistantRunMust(t, e.Dispatch(ctx, callback.ID))
	// The published original writer owns its durable book before the authored
	// pending transition is saved; no scheduling or authority is fabricated.
	_, err = runcontext.InvokeDurable(ctx, e.canonicalBinding, "host", "fixture", "inspect", []byte(`{}`), true, func([]byte) (runcontext.ToolResult, error) {
		return runcontext.ResultOf([]runcontext.Content{runcontext.TextContent("fixture read")}, nil), nil
	}, nil, nil)
	assistantRunMust(t, err)
	cp := assistantPreparationPending(t, *e.canonicalBinding.Writer.Checkpoint())
	assistantRunMust(t, e.canonicalBinding.Writer.Save(ctx, *e.canonicalBinding, cp))
	ref := assistantRunPlanReference(assistantExecutionRecord(t, e))
	if ref == nil {
		t.Fatal("original retained pending Plan absent")
	}
	token, err := scope.begin("native-call")
	assistantRunMust(t, err)
	assistantRunMust(t, scope.admit(token, callback))
	stop := &assistantAgentStop{parent: ctx, execution: cp.ID}
	stop.begin("native-call", "desk_package_plan")
	stop.observe(assistantNativePlanNotice(cp.ID, ref))
	assistantRunMust(t, scope.planCallback(token, stop))
	head := assistantExecutionRecord(t, e).Canonical.Head
	result := assistantChatJSON(assistantAgentOutcome{Stop: "plan", Answer: &adapter.AgentToolAnswer{Text: "This exact Plan awaits a decision."}})
	assistantRunMust(t, e.Result(ctx, callback.ID, "completed", result))
	// Physical callback receipts advance only the host revision. They must not
	// invalidate the retained canonical Plan-head identity.
	if !reflect.DeepEqual(head, assistantExecutionRecord(t, e).Canonical.Head) {
		t.Fatal("physical callback receipt rewrote canonical Plan head")
	}
	scope.receipt(token, false, nil)
	scope.end(token)
	scope.release(true)
	assistantRunMust(t, scope.closeAndJoin(e.ctx))
	return scope, model, ctx
}

func TestAssistantNativePlanSettlementExactHeadAndRelease(t *testing.T) {
	for _, fault := range []string{"exact", "stale-canonical-head", "wrong-pending-tuple", "missing-release", "duplicate-release", "unknown-receipt", "unjoined-callback", "cancelled-owner", "cancelled-caller"} {
		t.Run(fault, func(t *testing.T) {
			scope, model, ctx := assistantNativePlanProofFixture(t)
			if err := scope.planModel(model); err != nil {
				t.Fatal("exact authored receipt seam failed before mutation", err)
			}
			switch fault {
			case "stale-canonical-head":
				head, err := scope.binding.Writer.Store.Load(ctx, scope.binding.Writer.ID)
				assistantRunMust(t, err)
				_, err = scope.binding.Writer.Store.Commit(ctx, runcontext.RunCommit{RunID: scope.binding.Writer.ID, ExpectedRevision: head.Revision, CommitID: "competing-plan-receipt", Checkpoint: head.Checkpoint, Events: [][]byte{}})
				assistantRunMust(t, err)
			case "wrong-pending-tuple":
				scope.plan.CandidateRevision = "sha256:" + strings.Repeat("f", 64)
			case "missing-release":
				scope.releases, scope.released = 0, false
			case "duplicate-release":
				scope.release(true)
			case "unknown-receipt":
				scope.receiptFailed = true
			case "unjoined-callback":
				scope.joinedClosed = false
			case "cancelled-owner":
				scope.execution.cancel()
			case "cancelled-caller":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				if _, err := scope.planCheckpoint(cancelled, scope.plan); !errors.Is(err, context.Canceled) {
					t.Fatal("caller cancellation was swallowed", err)
				}
				return
			}
			if (scope.planModel(model) == nil) != (fault == "exact") {
				t.Fatal("Plan qualified without exact current head, original receipts, release/join and live owner", fault)
			}
			if scope.approvalSettled() {
				t.Fatal("Plan proof qualified action-approval retention")
			}
			// The proof itself is observational and never dispatches a model or
			// grants authority. Its only model row is this authored seam's row.
			current := assistantExecutionRecord(t, scope.execution)
			models := 0
			for _, operation := range current.Operations {
				if operation.Kind == "model" {
					models++
				}
			}
			if models != 1 {
				t.Fatal("proof qualification dispatched work")
			}
		})
	}
}
