package desk

import (
	"net/http"
	"reflect"
)

// Only explicit terminal ProjectChat/reconcile calls enter this path. It can
// observe a used allowance after cancellation, never call requestResearch or
// re-authorize/replay a workflow. Raw run/history/Stop APIs do not invoke it.
func (s *Server) reconcileAssistantResearchRequests(r *http.Request, id string) error {
	store := s.assistantRuns()
	_, err := store.transaction(r.Context(), func(ledger *assistantRunLedger) (bool, error) {
		run, err := assistantChatProjectionRun(ledger, id)
		if err != nil {
			return false, err
		}
		if err = s.authorizeAssistantRun(r, *run); err != nil {
			return false, err
		}
		pending := false
		for _, op := range run.Operations {
			if op.Name == assistantResearchOperation && op.State != "refused" {
				var in assistantResearchIntent
				if readAssistantHostReceipt(op.Intent, &in) != nil {
					return false, errAssistantRunInvalid
				}
				found := false
				for _, out := range run.DomainOutputs {
					if out.OperationID == in.HostOperationID {
						found = true
					}
				}
				if !found {
					pending = true
				}
			}
		}
		if !pending {
			return false, nil
		}
		root, _, _, selected, err := s.assistantChatProjectionOwner(r, store, run)
		if err != nil {
			return false, err
		}
		defer root.Close()
		if !assistantChatProjectionMatches(run, selected) || run.ChatProjection != nil {
			return false, errAssistantRunConflict
		}
		changed := false
		for i := range run.Operations {
			op := &run.Operations[i]
			if op.Name != assistantResearchOperation || op.State == "refused" {
				continue
			}
			var in assistantResearchIntent
			if readAssistantHostReceipt(op.Intent, &in) != nil {
				return false, errAssistantRunInvalid
			}
			found := false
			for _, out := range run.DomainOutputs {
				if out.OperationID == in.HostOperationID {
					found = true
				}
			}
			if found {
				continue
			}
			// The only recovery source is the exact same-scope used-grant/turn receipt.
			receipt, err := s.inspectResearchRequest(r, in.ResearchRequest)
			if err != nil {
				return false, &inputResearchRequestUnknown{err}
			}
			host := assistantDialectOperation(*run, in.HostOperationID)
			if host == nil || host.State == "completed" {
				return false, errAssistantRunConflict
			}
			var h assistantHostIntent
			if readAssistantHostReceipt(host.Intent, &h) != nil || h.DialectAction == nil {
				return false, errAssistantRunInvalid
			}
			readOp := assistantDialectOperation(*run, h.DialectAction.ReadOperationID)
			var held assistantHostResult
			if readOp == nil || readAssistantHostReceipt(readOp.Result, &held) != nil || held.DialectRead == nil {
				return false, errAssistantRunInvalid
			}
			if op.State == "completed" {
				var previous assistantResearchDecision
				if readAssistantHostReceipt(op.Result, &previous) != nil || previous.Receipt == nil || !sameResearchReceipt(*previous.Receipt, receipt) {
					return false, errAssistantRunConflict
				}
				receipt = *previous.Receipt
			} else {
				if op.State != "dispatched" && op.State != "uncertain" {
					return false, errAssistantRunDispatch
				}
				op.State = "completed"
				op.Result = assistantChatJSON(assistantResearchDecision{Receipt: &receipt})
			}
			// Preserve a retained unknown result byte-for-byte. A missing host result
			// becomes that same typed observation, never a fabricated model success.
			expected := assistantHostResult{Result: assistantDialectFailure(&inputResearchRequestUnknown{}).Value().JSON()}
			if host.Result == nil {
				host.State = "refused"
				host.Result = assistantChatJSON(expected)
			} else {
				var previous assistantHostResult
				if host.State != "refused" || readAssistantHostReceipt(host.Result, &previous) != nil || !reflect.DeepEqual(previous, expected) {
					return false, errAssistantRunConflict
				}
			}
			output := assistantResearchOutput(*h.DialectAction, *held.DialectRead, op.ID, receipt, true)
			run.DomainOutputs = append(run.DomainOutputs, assistantDomainOutput{Kind: "dialect-action", RunID: run.ID, RequestKey: run.Binding.ExecutionKey, OperationID: host.ID, CallID: h.CallID, DialectAction: output})
			changed = true
		}
		if changed {
			run.Revision++
		}
		return changed, nil
	})
	return err
}
func (s *Server) checkAssistantResearchEvidence(r *http.Request, run assistantRunRecord, p *assistantDialectActionOutput) error {
	if p.Research == nil || p.Action == nil {
		return errAssistantRunInvalid
	}
	op := assistantDialectOperation(run, p.Research.OperationID)
	var in assistantResearchIntent
	if op == nil || readAssistantHostReceipt(op.Intent, &in) != nil {
		return errAssistantRunInvalid
	}
	receipt, err := s.inspectResearchRequest(r, in.ResearchRequest)
	if err != nil {
		return err
	}
	if !sameResearchReceipt(receipt, p.Research.Receipt) {
		return errAssistantRunConflict
	}
	return nil
}
func assistantResearchUnobserved(run assistantRunRecord) bool {
	for _, op := range run.Operations {
		if op.Name != assistantResearchOperation || op.State == "refused" {
			continue
		}
		var in assistantResearchIntent
		if readAssistantHostReceipt(op.Intent, &in) != nil {
			return true
		}
		seen := false
		for _, out := range run.DomainOutputs {
			if out.OperationID == in.HostOperationID {
				seen = true
			}
		}
		if !seen {
			return true
		}
	}
	return false
}
