package desk

import (
	"encoding/json"
	"reflect"
)

type assistantWebsiteOutput struct {
	Recovered     bool                      `json:"recovered,omitempty"`
	Reference     assistantWebsiteReference `json:"reference"`
	Stored        attachmentReceipt         `json:"stored"`
	AcquisitionID string                    `json:"acquisitionId"`
}

func validateAssistantWebsiteCustody(run assistantRunRecord) error {
	entries := []assistantLinkCustody{}
	for _, v := range run.Websites {
		entries = append(entries, assistantLinkCustody(v))
	}
	return validateAssistantMaterialCustody(run, entries, assistantWebsiteTool)
}
func validateAssistantWebsiteOutput(run assistantRunRecord, out assistantDomainOutput, receipts map[string]assistantHostResult) error {
	v := out.Website
	if v == nil || out.Link != nil || out.Search != nil || out.DialectAction != nil || out.Recipe != nil || out.TestProposal != nil || out.PolicyUpdate != nil || out.PolicyInterpretation != nil || out.PolicySource != nil || !reflect.DeepEqual(out.Draft, graphDraft{}) || out.RunID != run.ID || out.RequestKey != run.Binding.ExecutionKey || out.OperationID != v.AcquisitionID || !validAssistantWebsiteReference(v.Reference) || v.Stored.ID != v.Reference.ID || !assistantRunDigest(v.Stored.SHA256) {
		return errAssistantRunInvalid
	}
	op := assistantDialectOperation(run, out.OperationID)
	if op == nil || op.Name != assistantWebsiteTool || !contains([]string{"completed", "refused"}, op.State) {
		return errAssistantRunInvalid
	}
	var in assistantHostIntent
	if readAssistantHostReceipt(op.Intent, &in) != nil || in.CallID != out.CallID {
		return errAssistantRunInvalid
	}
	held, err := assistantLinkCustodyOf(run, op.ID)
	if err != nil || !held.Discovery || held.Final.Receipt == nil || *held.Final.Receipt != v.Stored || held.Digest != v.Reference.Digest || held.Request.URL != v.Reference.Seed || v.Recovered && !held.DispatchClosed {
		return errAssistantRunInvalid
	}
	var result struct {
		StructuredContent struct {
			Website    string `json:"website"`
			Discovered int    `json:"discovered"`
			StopReason string `json:"stopReason"`
		} `json:"structuredContent"`
		IsError bool `json:"isError"`
	}
	receipt, ok := receipts[op.ID]
	if !ok || json.Unmarshal(receipt.Result, &result) != nil {
		return errAssistantRunInvalid
	}
	if v.Recovered {
		if !result.IsError || op.State != "refused" {
			return errAssistantRunInvalid
		}
		return nil
	}
	if result.IsError || result.StructuredContent.Website != v.Reference.Seed || result.StructuredContent.Discovered < 0 || result.StructuredContent.Discovered > 10 || !contains([]string{"finished", "depth-limit", "page-limit", "byte-limit", "time-limit", "link-limit", "request-limit", "robots-unavailable"}, result.StructuredContent.StopReason) {
		return errAssistantRunInvalid
	}
	return nil
}
func appendAssistantWebsiteOutputs(chat map[string]json.RawMessage, run assistantRunRecord) ([]assistantWebsiteReference, error) {
	refs := []assistantWebsiteReference{}
	if raw := chat["websites"]; raw != nil && json.Unmarshal(raw, &refs) != nil {
		return nil, errAssistantRunInvalid
	}
	ids := map[string]bool{}
	for _, ref := range refs {
		if !validAssistantWebsiteReference(ref) || ids[ref.ID] {
			return nil, errAssistantRunInvalid
		}
		ids[ref.ID] = true
	}
	added := []assistantWebsiteReference{}
	for _, out := range run.DomainOutputs {
		if out.Kind != "website" {
			continue
		}
		ref := out.Website.Reference
		found := false
		next := []assistantWebsiteReference{}
		for _, held := range refs {
			if held.ID == ref.ID {
				if held != ref {
					return nil, errAssistantRunConflict
				}
				found = true
			}
			if held.Seed != ref.Seed {
				next = append(next, held)
			}
		}
		next = append(next, ref)
		refs = next
		if !found {
			added = append(added, ref)
		}
	}
	if len(refs) > 16 {
		return nil, errAssistantRunInvalid
	}
	if len(added) > 0 {
		chat["websites"] = assistantChatJSON(refs)
	}
	return added, nil
}
