package desk

import (
	"bytes"
	"encoding/json"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"reflect"
	"strings"
)

// These references identify one retained transition, never executable arguments.
type assistantPlanDecisionReference struct {
	CallID            string `json:"callId"`
	PlanRevision      string `json:"planRevision"`
	CandidateRevision string `json:"candidateRevision"`
}

func validAssistantRunPlanDecisionReference(r *assistantPlanDecisionReference) bool {
	return r != nil && assistantRunText(r.CallID, 256) && strings.HasPrefix(r.PlanRevision, "sha256:") && assistantRunDigest(strings.TrimPrefix(r.PlanRevision, "sha256:")) && strings.HasPrefix(r.CandidateRevision, "sha256:") && assistantRunDigest(strings.TrimPrefix(r.CandidateRevision, "sha256:"))
}
func assistantCheckpointHasPendingPlan(cp *contract.Checkpoint) bool {
	if cp == nil {
		return false
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		return true
	}
	value, err := runcontext.ParseJSON(raw)
	return err != nil || value.Get("planState").Get("pending") != nil
}

// Published validation precedes every interpretation. A malformed or ambiguous
// checkpoint cannot offer a runnable decision, including in historical reads.
func assistantRunPlanReference(run assistantRunRecord) *assistantPlanDecisionReference {
	cp := run.Checkpoint
	if run.Historical || cp == nil || cp.Status != "paused" || cp.Approval != nil && cp.Approval.State == "asked" {
		return nil
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		return nil
	}
	value, err := runcontext.ParseJSON(raw)
	if err != nil {
		return nil
	}
	if _, err = runcontext.ReadExecution(value); err != nil {
		return nil
	}
	state := value.Get("planState")
	pending := state.Get("pending")
	if pending == nil || pending.Get("purpose").Str() != "plan-drift" || pending.Get("revision").Str() != state.Get("revision").Str() {
		return nil
	}
	ref := &assistantPlanDecisionReference{pending.Get("callId").Str(), pending.Get("revision").Str(), pending.Get("candidate").Get("planState").Get("revision").Str()}
	if !validAssistantRunPlanDecisionReference(ref) {
		return nil
	}
	return ref
}

// Keep required decision scalars intact at the durable boundary before Go can
// turn a missing or null boolean into false. Existing receipt shapes are unchanged.
func (r *assistantRunActionReceipt) UnmarshalJSON(raw []byte) error {
	type receipt assistantRunActionReceipt
	var decoded receipt
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	if decoded.Kind == "plan-decision" {
		if err := readAssistantDialectCustody(raw, reflect.TypeOf(receipt{})); err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return errAssistantRunInvalid
		}
		var decision map[string]json.RawMessage
		if json.Unmarshal(fields["decision"], &decision) != nil || decision == nil || !bytes.Equal(decision["approved"], []byte("true")) && !bytes.Equal(decision["approved"], []byte("false")) {
			return errAssistantRunInvalid
		}
		if !validAssistantRunPlanDecisionReference(decoded.PlanDecision) || decoded.Decision == nil || decoded.Decision.CallID != decoded.PlanDecision.CallID {
			return errAssistantRunInvalid
		}
	}
	*r = assistantRunActionReceipt(decoded)
	return nil
}
