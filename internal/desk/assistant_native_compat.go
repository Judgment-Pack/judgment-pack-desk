package desk

import (
	"context"
	"encoding/json"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

const assistantNativePlanAfterApproval = "NativePlanAfterApprovalUnsupported: this native response cannot continue a Plan decision after an answered action approval. Existing response evidence was left untouched; nothing more was run."
const assistantNativePlanDelegation = "NativePlanDelegationUnsupported: this native response cannot continue a released Plan pause with delegated children enabled. The retained Plan and results were left untouched; nothing more was run."

// Agent #309/#317: inspect the effective canonical head before PreparePlanDecision
// consumes the person's decision. A stale Input cannot hide an earlier answered
// approval or released native invocation. This neither consumes nor repairs them.
func (e *assistantRunExecution) guardAssistantNativePlanContinuation(ctx context.Context, input *runcontext.Input, children bool) error {
	if e.source.Connection.Assistant.Engine != "codex" || !assistantCanonicalActive(e.record) {
		return nil
	}
	if input == nil || input.Execution == nil || input.Persistence == nil || input.Persistence.Writer == nil {
		return errAssistantRunScope
	}
	e.canonicalMu.Lock()
	current := input.Persistence == e.canonicalBinding && !e.stopRequested
	e.canonicalMu.Unlock()
	if !current {
		return errAssistantRunScope
	}
	if err := e.Authorize(ctx); err != nil {
		return err
	}
	writer := input.Persistence.Writer
	head, err := writer.Store.Load(ctx, writer.ID)
	if err != nil {
		return err
	}
	if head == nil {
		return nil
	}
	value, err := runcontext.ParseJSON(head.Checkpoint)
	if err != nil {
		return err
	}
	cp, err := runcontext.ReadExecution(value)
	if err != nil {
		return err
	}
	if cp.ID != input.Execution.ID {
		return errAssistantRunScope
	}
	if value.Get("planState").Get("pending") != nil && cp.Approval != nil && (cp.Approval.State == "executed" || cp.Approval.State == "answered") {
		return assistantNativeCompatibilityRefusal(assistantNativePlanAfterApproval)
	}
	if children && cp.Durable != nil {
		for _, dispatch := range cp.Durable.Dispatches {
			if dispatch.Kind == "agent" && dispatch.PlanPause != nil {
				return assistantNativeCompatibilityRefusal(assistantNativePlanDelegation)
			}
		}
	}
	return nil
}

func assistantNativeCompatibilityRefusal(detail string) error {
	held := failure.New("configuration", "context", "not-started")
	held.Detail = &detail
	return failure.NewError(held)
}

// The public view and saved work share this display-only shape. Text strings
// come directly from retained canonical result content, never JSON.stringify.
type assistantApprovalResultDisplay struct {
	Available    bool     `json:"available"`
	Text         []string `json:"text"`
	OtherContent []string `json:"otherContent"`
}
type assistantApprovalResultView struct {
	CallID  string                         `json:"callId"`
	Name    string                         `json:"name"`
	IsError bool                           `json:"isError"`
	Result  assistantApprovalResultDisplay `json:"result"`
}

func assistantApprovalResultViewOf(cp *contract.Checkpoint) *assistantApprovalResultView {
	if cp == nil || cp.Approval == nil {
		return nil
	}
	approval := cp.Approval
	if approval.State != "executed" && approval.State != "answered" {
		return nil
	}
	held := &assistantApprovalResultView{CallID: approval.CallID, Name: approval.Name, Result: assistantApprovalResultDisplay{Text: []string{}, OtherContent: []string{}}}
	if approval.Result == nil {
		return held
	}
	held.Result.Available = true
	for _, content := range approval.Result.Content {
		if part := content.Option1; part != nil && part.Type != nil && *part.Type == "text" && part.Text != nil {
			held.Result.Text = append(held.Result.Text, *part.Text)
		} else if part != nil && part.Type != nil && assistantRunText(*part.Type, 200) {
			held.Result.OtherContent = append(held.Result.OtherContent, *part.Type)
		} else if content.ToolResultImagePart != nil && assistantRunText(content.ToolResultImagePart.Kind, 200) {
			held.Result.OtherContent = append(held.Result.OtherContent, content.ToolResultImagePart.Kind)
		} else {
			held.Result.OtherContent = append(held.Result.OtherContent, "non-text")
		}
	}
	if len(approval.Result.StructuredContent) > 0 {
		held.Result.OtherContent = append(held.Result.OtherContent, "structuredContent")
	}
	held.IsError = approval.Result.IsError != nil && *approval.Result.IsError
	return held
}

// Reuse the bounded already-authorized journal in this snapshot. Historical
// checkpoints remain private; only original result text and content types cross
// the presentation boundary. Deduplication uses canonical call identity.
func assistantApprovalResults(run assistantRunRecord) []assistantApprovalResultView {
	results := []assistantApprovalResultView{}
	ids := map[string]int{}
	retain := func(cp *contract.Checkpoint) {
		item := assistantApprovalResultViewOf(cp)
		if item == nil {
			return
		}
		if index, ok := ids[item.CallID]; ok {
			if !results[index].Result.Available {
				results[index] = *item
			}
			return
		}
		ids[item.CallID] = len(results)
		results = append(results, *item)
	}
	if run.Canonical != nil {
		for _, stored := range run.Canonical.Events {
			var envelope runcontext.StoredRunEnvelope
			if json.Unmarshal(stored.Bytes, &envelope) != nil || envelope.Scope.Execution != run.Canonical.Head.RunID || envelope.Scope.Task != "" {
				continue
			}
			if event := envelope.Event.ExecutionCheckpoint; event != nil {
				retain(&event.Checkpoint)
			}
		}
	}
	retain(run.Checkpoint)
	return results
}
