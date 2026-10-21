package desk

import (
	"encoding/json"
	"reflect"
)

type assistantLinkOutput struct {
	Recovered     bool                    `json:"recovered,omitempty"`
	Attachment    assistantLinkAttachment `json:"attachment"`
	Stored        attachmentReceipt       `json:"stored"`
	AcquisitionID string                  `json:"acquisitionId"`
}

func validAssistantLinkOutputReference(file assistantLinkAttachment) bool {
	if file.Document == nil || file.Link == nil || file.ID != file.Document.ID || !attachmentID.MatchString(file.ID) || !adDigest(file.Document.Digest) || len(file.Document.Pages) > 500 || file.Document.AllowPartial || file.Text != "" || file.Name == "" || !adWebURL(file.Link.URL) || file.Link.ResolvedURL != "" && !adWebURL(file.Link.ResolvedURL) {
		return false
	}
	seen := map[int]bool{}
	for _, page := range file.Document.Pages {
		if page < 1 || page > 500 || seen[page] {
			return false
		}
		seen[page] = true
	}
	return true
}
func validateAssistantLinkOutput(run assistantRunRecord, out assistantDomainOutput, receipts map[string]assistantHostResult) error {
	link := out.Link
	if out.Website != nil || link == nil || out.RunID != run.ID || out.RequestKey != run.Binding.ExecutionKey || out.Search != nil || out.DialectAction != nil || out.Recipe != nil || out.TestProposal != nil || out.PolicyUpdate != nil || out.PolicyInterpretation != nil || out.PolicySource != nil || !reflect.DeepEqual(out.Draft, graphDraft{}) || link.AcquisitionID != out.OperationID || !validAssistantLinkOutputReference(link.Attachment) || link.Stored.ID != link.Attachment.ID || !assistantRunDigest(link.Stored.SHA256) {
		return errAssistantRunInvalid
	}
	producer := assistantDialectOperation(run, out.OperationID)
	if producer == nil || producer.Kind != "host" || producer.Name != assistantLinkTool || !contains([]string{"completed", "refused"}, producer.State) {
		return errAssistantRunInvalid
	}
	var in assistantHostIntent
	if readAssistantHostReceipt(producer.Intent, &in) != nil || in.CallID != out.CallID {
		return errAssistantRunInvalid
	}
	held, err := assistantLinkCustodyOf(run, producer.ID)
	if err != nil || held.Final.Receipt == nil || *held.Final.Receipt != link.Stored || held.Digest != link.Attachment.Document.Digest || held.Request.URL != link.Attachment.Link.URL || link.Recovered && !held.DispatchClosed {
		return errAssistantRunInvalid
	}
	args, err := readAssistantLinkArguments(in.Arguments)
	if err != nil || !link.Recovered && link.Attachment.Link.Anchor != args.Link.Anchor {
		return errAssistantRunInvalid
	}
	receipt, ok := receipts[producer.ID]
	if !ok {
		return errAssistantRunInvalid
	}
	var result struct {
		StructuredContent struct {
			DocumentID  string `json:"documentId"`
			Digest      string `json:"digest"`
			NeedsReview bool   `json:"needsReview"`
		} `json:"structuredContent"`
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(receipt.Result, &result) != nil {
		return errAssistantRunInvalid
	}
	if link.Recovered {
		if !result.IsError || producer.State != "refused" {
			return errAssistantRunInvalid
		}
		return nil
	}
	if result.StructuredContent.DocumentID != link.Attachment.ID || result.StructuredContent.Digest != held.Digest || result.IsError && (!result.StructuredContent.NeedsReview || !link.Attachment.Document.NeedsReview && len(link.Attachment.Document.Pages) > 0) {
		return errAssistantRunInvalid
	}
	return nil
}
func appendAssistantLinkOutputs(chat map[string]json.RawMessage, run assistantRunRecord) ([]assistantLinkAttachment, error) {
	raw := chat["documents"]
	if raw == nil {
		raw = []byte("[]")
	}
	rows, err := assistantChatArray(raw)
	if err != nil {
		return nil, err
	}
	seen := map[string]json.RawMessage{}
	for _, row := range rows {
		var file assistantLinkAttachment
		if json.Unmarshal(row, &file) != nil || file.ID == "" || seen[file.ID] != nil {
			return nil, errAssistantRunInvalid
		}
		seen[file.ID] = row
	}
	added := []assistantLinkAttachment{}
	for _, out := range run.DomainOutputs {
		if out.Kind != "link-document" {
			continue
		}
		file := out.Link.Attachment
		if previous := seen[file.ID]; previous != nil {
			var held assistantLinkAttachment
			if json.Unmarshal(previous, &held) != nil || !reflect.DeepEqual(held, file) {
				return nil, errAssistantRunConflict
			}
			continue
		}
		encoded := assistantChatJSON(file)
		rows = append(rows, encoded)
		seen[file.ID] = encoded
		added = append(added, file)
	}
	if len(rows) > 256 {
		return nil, errAssistantRunInvalid
	}
	if len(added) > 0 {
		chat["documents"] = assistantChatEncodeArray(rows)
	}
	return added, nil
}
func assistantLinkProjectionItem(run assistantRunRecord, call string, item map[string]any) error {
	for _, op := range run.Operations {
		if op.Kind != "host" || op.Name != assistantLinkTool || op.Result == nil {
			continue
		}
		var in assistantHostIntent
		var receipt assistantHostResult
		if readAssistantHostReceipt(op.Intent, &in) != nil || in.CallID != call || readAssistantHostReceipt(op.Result, &receipt) != nil {
			continue
		}
		args, err := readAssistantLinkArguments(in.Arguments)
		if err != nil {
			return err
		}
		detail := map[string]any{"url": args.Link.DisplayURL}
		var result struct {
			StructuredContent struct {
				DocumentID string `json:"documentId"`
				Digest     string `json:"digest"`
				Pages      []int  `json:"pages"`
			} `json:"structuredContent"`
			IsError bool `json:"isError"`
		}
		if json.Unmarshal(receipt.Result, &result) != nil {
			return errAssistantRunInvalid
		}
		if !result.IsError && result.StructuredContent.DocumentID != "" && len(result.StructuredContent.Pages) > 0 {
			detail["documentId"], detail["digest"], detail["pages"] = result.StructuredContent.DocumentID, result.StructuredContent.Digest, result.StructuredContent.Pages
		}
		item["link"] = detail
	}
	return nil
}
