package desk

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

func validAssistantPolicySource(s policyAuthoringSource) bool {
	return s.Version == 1 && policyIDForm.MatchString(s.PolicyID) && policyEventIDForm.MatchString(s.RevisionID) && strings.HasPrefix(s.RevisionID, "revision_") && (s.PublicationID == "" || policyEventIDForm.MatchString(s.PublicationID) && strings.HasPrefix(s.PublicationID, "publication_")) && s.Content.valid() && s.Content.Bytes > 0 && s.Content.Bytes <= policyCandidateByteLimit && s.CandidateDigest == s.Content.SHA256 && validAssistantPolicyFields(assistantPolicyFields{s.Title, s.Narrative, s.ArtifactVersion})
}
func assistantPolicyStaleResult(raw []byte) bool {
	result, _ := assistantPolicyUpdateResult(assistantPolicyProposal{Check: &assistantPolicyCheck{Scope: "draft-shape-and-revision", Status: "stale"}})
	return bytes.Equal(raw, result.Value().JSON())
}
func validateAssistantPolicyOutput(run assistantRunRecord, out assistantDomainOutput, receipts map[string]assistantHostResult) error {
	if !contains([]string{"policy-update", "policy-interpretation"}, out.Kind) || out.RunID != run.ID || out.RequestKey != run.Binding.ExecutionKey || out.Recipe != nil || !reflect.DeepEqual(out.Draft, graphDraft{}) {
		return errAssistantRunInvalid
	}
	if run.Binding.Phase == "plan" {
		return errAssistantRunInvalid
	}
	var producer *assistantRunOperation
	for i := range run.Operations {
		if run.Operations[i].ID == out.OperationID {
			producer = &run.Operations[i]
		}
	}
	if producer == nil || producer.Kind != "host" || !assistantPolicyTool(producer.Name) || !assistantHostProposal(producer.Name) {
		return errAssistantRunInvalid
	}
	var in assistantHostIntent
	if readAssistantHostReceipt(producer.Intent, &in) != nil || !in.Root || in.CallID != out.CallID {
		return errAssistantRunInvalid
	}
	if in.Policy == nil || !validAssistantPolicyHostBinding(*in.Policy) {
		return errAssistantRunInvalid
	}
	scope := in.Policy
	args, err := readAssistantPolicyArguments(producer.Name, in.Arguments)
	if err != nil {
		return errAssistantRunInvalid
	}
	receipt, ok := receipts[producer.ID]
	if !ok {
		return errAssistantRunInvalid
	}
	var expected []byte
	if out.Kind == "policy-update" {
		p := out.PolicyUpdate
		if producer.Name != "propose_policy_update" || p == nil || out.PolicyInterpretation != nil || out.PolicySource != nil || receipt.PolicySource != nil || scope.Request == nil || p.Version != 1 || !policyInterpretationIDForm.MatchString(p.ID) || p.PolicyID != scope.ID || p.AfterTurnID != run.Binding.RequestID || p.Revision != scope.Request.Revision || p.Revision != args.Revision || p.Before != scope.Request.Candidate || p.Candidate != (assistantPolicyFields{args.Title, args.Narrative, args.ArtifactVersion}) || !validAssistantPolicyFields(p.Candidate) || !policyOperationIDForm.MatchString(p.OperationID) || len(p.Save) != 0 || p.Check == nil || p.Check.Scope != "draft-shape-and-revision" || !contains([]string{"passed", "stale"}, p.Check.Status) {
			return errAssistantRunInvalid
		}
		if _, err := time.Parse(time.RFC3339Nano, p.CreatedAt); err != nil {
			return errAssistantRunInvalid
		}
		if p.ID != assistantPolicyRecordID(run.Binding.ExecutionKey, in.CallID, in.CallbackID, "proposal") || p.OperationID != assistantPolicyRecordID(run.Binding.ExecutionKey, in.CallID, in.CallbackID, "operation") {
			return errAssistantRunInvalid
		}
		if p.Check.Status == "passed" && producer.State != "completed" || p.Check.Status == "stale" && producer.State != "refused" {
			return errAssistantRunInvalid
		}
		result, err := assistantPolicyUpdateResult(*p)
		if err != nil {
			return err
		}
		expected = result.Value().JSON()
	} else {
		p, s := out.PolicyInterpretation, out.PolicySource
		if p == nil || s == nil || out.PolicyUpdate != nil || receipt.PolicySource == nil || *receipt.PolicySource != *s || producer.State != "completed" || p.Version != 1 || !policyInterpretationIDForm.MatchString(p.ID) || p.PolicyID != scope.ID || p.AfterTurnID != run.Binding.RequestID || p.OpenedChatID != "" || !validAssistantPolicySource(*s) || s.PolicyID != scope.ID || p.RevisionID != s.RevisionID || p.PublicationID != s.PublicationID {
			return errAssistantRunInvalid
		}
		expectedName := "propose_policy_pack_interpretation"
		if p.ArtifactKind == "graph" {
			expectedName = "propose_policy_graph_interpretation"
		} else if p.ArtifactKind != "pack" {
			return errAssistantRunInvalid
		}
		if producer.Name != expectedName || args.PublicationID != s.PublicationID || args.RevisionID != "" && args.RevisionID != s.RevisionID {
			return errAssistantRunInvalid
		}
		if _, err := time.Parse(time.RFC3339Nano, p.CreatedAt); err != nil {
			return errAssistantRunInvalid
		}
		if p.ID != assistantPolicyRecordID(run.Binding.ExecutionKey, in.CallID, in.CallbackID, "handoff") {
			return errAssistantRunInvalid
		}
		result, err := assistantPolicyInterpretationResult(*p, *s)
		if err != nil {
			return err
		}
		expected = result.Value().JSON()
	}
	if !bytes.Equal(expected, receipt.Result) {
		return errAssistantRunInvalid
	}
	return nil
}

func validateAssistantPolicyProjectionSource(source []byte, run assistantRunRecord) error {
	for _, out := range run.DomainOutputs {
		if out.Kind != "policy-update" && out.Kind != "policy-interpretation" {
			continue
		}
		scope, err := readAssistantPolicyScope(assistantRunSource{Original: source})
		if err != nil || scope == nil || scope.ID == "" {
			return errAssistantRunScope
		}
		for _, op := range run.Operations {
			if op.ID == out.OperationID {
				var in assistantHostIntent
				if readAssistantHostReceipt(op.Intent, &in) != nil || in.Policy == nil || in.Policy.ID != scope.ID || !reflect.DeepEqual(in.Policy.Request, scope.Request) {
					return errAssistantRunScope
				}
			}
		}
	}
	return nil
}

// Domain cards append only to their existing typed chat fields. The shared
// terminal writer still owns prepare/CAS/readback/ack and all original bytes.
func appendAssistantPolicyOutputs(chat map[string]json.RawMessage, outputs []assistantDomainOutput) error {
	for _, kind := range []string{"policy-update", "policy-interpretation"} {
		field := "policyProposals"
		if kind == "policy-interpretation" {
			field = "policyInterpretations"
		}
		selected := []assistantDomainOutput{}
		for _, out := range outputs {
			if out.Kind == kind {
				selected = append(selected, out)
			}
		}
		if len(selected) == 0 {
			continue
		}
		existing := chat[field]
		if existing == nil {
			existing = []byte("[]")
		}
		list, err := assistantChatArray(existing)
		if err != nil || len(list)+len(selected) > 16 {
			return errAssistantRunInvalid
		}
		seen := map[string]bool{}
		for _, raw := range list {
			var p struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &p) != nil || p.ID == "" || seen[p.ID] {
				return errAssistantRunInvalid
			}
			seen[p.ID] = true
		}
		for _, out := range selected {
			id := ""
			var value any
			if kind == "policy-update" {
				id = out.PolicyUpdate.ID
				value = out.PolicyUpdate
			} else {
				id = out.PolicyInterpretation.ID
				value = out.PolicyInterpretation
			}
			if seen[id] {
				return errAssistantRunConflict
			}
			seen[id] = true
			list = append(list, assistantChatJSON(value))
		}
		chat[field] = assistantChatEncodeArray(list)
	}
	return nil
}
