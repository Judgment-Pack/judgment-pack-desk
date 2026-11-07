package desk

import (
	"context"
	"encoding/json"
	"reflect"
)

func assistantPlanBindingLocatorsEqual(meta *assistantPlanMetadata, refs []assistantPlanReference) bool {
	if meta == nil || len(meta.Bindings) != len(refs) {
		return false
	}
	for i, ref := range refs {
		if meta.Bindings[i].Locator != ref {
			return false
		}
	}
	return true
}

func assistantPlanCurrentReferences(legacy []chatPlanReference, bindings []assistantPlanBinding) []chatPlanReference {
	current := make([]chatPlanReference, 0, len(legacy))
	for i, ref := range legacy {
		current = append(current, chatPlanReference{Kind: bindings[i].Kind, ID: bindings[i].ID, Digest: ref.Revision})
	}
	return current
}

// Caller holds private-data custody. Current handles are usable only as part of
// the exact host-proposed Plan retained by one completed private operation. A
// conversation-only import can still display/export its rows, but cannot invent
// this custody. Historical locator rows do not require a journal they never had.
func (s *Server) assistantPlanLocatorsLocked(source []byte, plan chatPlan, identity *assistantRunBinding) ([]assistantPlanReference, error) {
	refs := make([]assistantPlanReference, 0, len(plan.References))
	current := false
	for _, ref := range plan.References {
		if !validChatPlanReference(ref) {
			return nil, errAssistantRunInvalid
		}
		current = current || ref.Revision == ""
		refs = append(refs, assistantPlanReference{ref.Kind, ref.ID})
	}
	if !current {
		return refs, nil
	}
	var chat struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(source, &chat) != nil || chat.ID == "" {
		return nil, errAssistantRunScope
	}
	store, err := s.openChatData()
	if err != nil {
		return nil, err
	}
	defer store.root.Close()
	runs := s.assistantRunsLocked(store.root)
	ledger, err := runs.readLocked(context.Background(), store)
	if err != nil {
		return nil, err
	}
	want := plan
	want.Status, want.AcceptedAt = "proposed", ""
	found := 0
	for _, run := range ledger.Runs {
		for _, out := range run.DomainOutputs {
			if out.Kind != "plan" || out.Plan == nil || out.Plan.ID != plan.ID || out.Plan.Revision != plan.Revision {
				continue
			}
			// Ambiguous rows refuse even when only one looks usable.
			found++
			if found != 1 || run.Historical || run.Binding.Project != runs.project || run.Binding.Owner.Kind != "chat" || run.Binding.Owner.ID != chat.ID || identity != nil && !assistantRunIdentityMatches(run.Binding, *identity) || !reflect.DeepEqual(*out.Plan, want) {
				return nil, errAssistantRunScope
			}
			op := assistantDialectOperation(run, out.OperationID)
			var intent assistantHostIntent
			if op == nil || readAssistantHostReceipt(op.Intent, &intent) != nil || intent.Plan == nil || len(intent.Plan.Bindings) == 0 {
				return nil, errAssistantRunInvalid
			}
			if !assistantCanonicalHostSucceeded(run, *op, intent) {
				return nil, errAssistantRunScope
			}
			// Ledger validation already checked the completed operation, exact
			// arguments/result/output linkage and deterministic scoped handles.
			a, err := readAssistantPlanArguments(intent.Arguments)
			if err != nil {
				return nil, err
			}
			locators, err := assistantPlanReferences(source, a.References)
			if err != nil {
				return nil, err
			}
			if !assistantPlanBindingLocatorsEqual(intent.Plan, locators) {
				return nil, errAssistantRunScope
			}
			refs = locators
		}
	}
	if found != 1 {
		return nil, errAssistantRunScope
	}
	return refs, nil
}
