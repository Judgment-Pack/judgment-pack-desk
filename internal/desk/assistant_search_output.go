package desk

import (
	"bytes"
	"encoding/json"
	"reflect"
)

type assistantSearchOutput struct {
	Recovered     bool                     `json:"recovered,omitempty"`
	Reference     assistantSearchReference `json:"reference"`
	Stored        attachmentReceipt        `json:"stored"`
	AcquisitionID string                   `json:"acquisitionId"`
	Step          assistantSearchStep      `json:"step"`
}

func validateAssistantSearchOutput(run assistantRunRecord, out assistantDomainOutput, receipts map[string]assistantHostResult) error {
	s := out.Search
	if s == nil || out.RunID != run.ID || out.RequestKey != run.Binding.ExecutionKey || out.DialectAction != nil || out.Recipe != nil || out.TestProposal != nil || out.PolicyUpdate != nil || out.PolicyInterpretation != nil || out.PolicySource != nil || !reflect.DeepEqual(out.Draft, graphDraft{}) || s.AcquisitionID != out.OperationID || !validAssistantSearchReference(s.Reference) || s.Reference.ID != s.Stored.ID || !assistantRunDigest(s.Stored.SHA256) || !s.Step.Submitted || s.Step.Query != s.Reference.Request.Query || s.Step.Reference == nil || !reflect.DeepEqual(*s.Step.Reference, s.Reference) || !researchSourceName.MatchString(s.Step.Provider) {
		return errAssistantRunInvalid
	}
	producer := assistantDialectOperation(run, out.OperationID)
	if producer == nil || producer.Name != assistantSearchTool || producer.Kind != "host" || producer.State != "completed" && !(s.Recovered && producer.State == "refused") {
		return errAssistantRunInvalid
	}
	var in assistantHostIntent
	if readAssistantHostReceipt(producer.Intent, &in) != nil || in.CallID != out.CallID {
		return errAssistantRunInvalid
	}
	held, err := assistantSearchCustodyOf(run, producer.ID)
	if err != nil || s.Recovered && !held.DispatchClosed || held.Final.Receipt == nil || *held.Final.Receipt != s.Stored || held.Digest != s.Reference.Digest || held.Request != s.Reference.Request {
		return errAssistantRunInvalid
	}
	receipt, ok := receipts[producer.ID]
	if !ok {
		return errAssistantRunInvalid
	}
	// Compare the exact emitted structured step rather than independently trusting
	// a browser-shaped card or a different operation's attachment identifier.
	var result struct {
		StructuredContent struct {
			Step assistantSearchStep `json:"searchStep"`
		} `json:"structuredContent"`
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(receipt.Result, &result) != nil {
		return errAssistantRunInvalid
	}
	if s.Recovered {
		if !result.IsError || producer.State != "refused" {
			return errAssistantRunInvalid
		}
		return nil
	}
	if result.IsError || !reflect.DeepEqual(result.StructuredContent.Step, s.Step) {
		return errAssistantRunInvalid
	}
	return nil
}
func appendAssistantSearchOutputs(chat map[string]json.RawMessage, run assistantRunRecord) ([]assistantSearchReference, error) {
	raw := chat["searches"]
	if raw == nil {
		raw = []byte("[]")
	}
	rows, err := assistantChatArray(raw)
	if err != nil {
		return nil, err
	}
	seen := map[string]json.RawMessage{}
	for _, row := range rows {
		var ref assistantSearchReference
		if json.Unmarshal(row, &ref) != nil || !validAssistantSearchReference(ref) || seen[ref.ID] != nil {
			return nil, errAssistantRunInvalid
		}
		seen[ref.ID] = row
	}
	added := []assistantSearchReference{}
	for _, out := range run.DomainOutputs {
		if out.Kind != "search" {
			continue
		}
		ref := out.Search.Reference
		encoded := assistantChatJSON(ref)
		if previous := seen[ref.ID]; previous != nil {
			var held assistantSearchReference
			if json.Unmarshal(previous, &held) != nil || held != ref {
				return nil, errAssistantRunConflict
			}
			continue
		}
		rows = append(rows, encoded)
		seen[ref.ID] = encoded
		added = append(added, ref)
	}
	if len(rows) > 64 {
		return nil, errAssistantRunInvalid
	}
	if len(added) > 0 {
		chat["searches"] = assistantChatEncodeArray(rows)
	}
	return added, nil
}
func assistantSearchProjectionItem(run assistantRunRecord, call string, item map[string]any) error {
	for _, op := range run.Operations {
		if op.Kind != "host" || op.Name != assistantSearchTool || op.Result == nil {
			continue
		}
		var intent assistantHostIntent
		var receipt assistantHostResult
		if readAssistantHostReceipt(op.Intent, &intent) != nil || intent.CallID != call || readAssistantHostReceipt(op.Result, &receipt) != nil {
			continue
		}
		var result struct {
			StructuredContent map[string]json.RawMessage `json:"structuredContent"`
		}
		if json.Unmarshal(receipt.Result, &result) != nil {
			return errAssistantRunInvalid
		}
		if suggestion := result.StructuredContent["researchSuggestion"]; bytes.Equal(suggestion, []byte("true")) {
			item["researchSuggestion"] = true
		}
		if raw := result.StructuredContent["searchStep"]; raw != nil {
			var step assistantSearchStep
			if json.Unmarshal(raw, &step) != nil {
				return errAssistantRunInvalid
			}
			item["search"] = step
		}
		if raw := result.StructuredContent["searchFailure"]; raw != nil {
			var code string
			if json.Unmarshal(raw, &code) != nil {
				return errAssistantRunInvalid
			}
			item["failure"] = code
		}
	}
	return nil
}
