package desk

import (
	"context"
	"encoding/json"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// Only the retained pending transition can identify a Plan decision. A proposed
// candidate never supplies approval, revisions, a replacement Plan or a writer.
func (e *assistantRunExecution) prepareAssistantPlanDecision(ctx context.Context, input *runcontext.Input) (*runcontext.Input, error) {
	if input == nil || input.Execution == nil {
		return nil, errAssistantRunScope
	}
	raw, err := json.Marshal(input.Execution)
	if err != nil {
		return nil, err
	}
	value, err := runcontext.ParseJSON(raw)
	if err != nil {
		return nil, err
	}
	pending := value.Get("planState").Get("pending")
	if pending == nil {
		// Non-Plan approvals continue through their existing adapter path. An
		// unassociated decision cannot fall through into a new Runtime/model turn.
		if input.Approval != nil && (input.Execution.Approval == nil || input.Execution.Approval.State != "asked" || input.Execution.Approval.CallID != input.Approval.CallID) {
			return nil, errAssistantRunScope
		}
		return input, nil
	}
	if _, err := runcontext.ReadExecution(value); err != nil {
		return nil, err
	}
	if input.Approval == nil || input.Approval.CallID != pending.Get("callId").Str() || input.Plan == nil || input.PlanResolver == nil || input.Persistence == nil || input.Persistence != e.canonicalBinding {
		return nil, errAssistantRunScope
	}
	if !validAssistantRunPlanDecisionReference(e.planDecision) || e.planDecision.CallID != input.Approval.CallID {
		return nil, errAssistantRunScope
	}
	expected := runcontext.PlanDecisionExpectation{Revision: e.planDecision.PlanRevision, CandidateRevision: e.planDecision.CandidateRevision}
	prepared, events, err := runcontext.PreparePlanDecision(ctx, input, expected)
	if err != nil {
		return nil, err
	}
	// The published operation has already committed its scoped events atomically.
	// Delivery must never pass through the host-event publication ingress.
	for _, event := range events {
		if err := e.deliverCanonicalEvent(ctx, event); err != nil {
			return nil, err
		}
	}
	return prepared, nil
}
