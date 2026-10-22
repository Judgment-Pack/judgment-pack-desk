package desk

// Plan is a review-only phase. This binding stores domain output; the canonical
// Agent remains responsible for dispatch, context, request accounting and events.
import (
	"context"
	"encoding/json"
	"reflect"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

const assistantPlanTool = "propose_plan"

type assistantPlanReference struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type assistantPlanArguments struct {
	Objective  string                   `json:"objective"`
	Steps      []string                 `json:"steps"`
	Questions  []string                 `json:"questions"`
	References []assistantPlanReference `json:"references"`
}

func readAssistantPlanArguments(raw json.RawMessage) (assistantPlanArguments, error) {
	var a assistantPlanArguments
	if _, err := assistantChatValue(raw); err != nil {
		return a, failure.ToolInput("Provide a valid Plan object with exact declared arguments.")
	}
	if err := readAssistantDialectCustody(raw, reflect.TypeOf(a)); err != nil {
		return a, failure.ToolInput("Use every declared Plan member exactly once, with its declared type.")
	}
	if err := assistantHostArguments(raw, &a); err != nil {
		return a, err
	}
	if !validPlanText(a.Objective, 2000) || a.Steps == nil || len(a.Steps) < 1 || len(a.Steps) > 20 || a.Questions == nil || len(a.Questions) > 10 || a.References == nil || len(a.References) > 6 {
		return a, failure.ToolInput("Provide a concise objective, one to twenty steps, up to ten questions and at most six additional references.")
	}
	for _, text := range append(append([]string{}, a.Steps...), a.Questions...) {
		if !validPlanText(text, 1000) {
			return a, failure.ToolInput("Each step and question must contain one to 1000 UTF-16 units of text.")
		}
	}
	for _, ref := range a.References {
		if !validAssistantPlanReference(ref) {
			return a, failure.ToolInput("Name a saved project file or Dialect using its exact reference ID.")
		}
	}
	return a, nil
}
func validAssistantPlanReference(ref assistantPlanReference) bool {
	if !validPlanText(ref.ID, 1024) {
		return false
	}
	if ref.Kind == "dialect" {
		return inputPreparationID.MatchString(ref.ID)
	}
	if ref.Kind != "file" {
		return false
	}
	path, err := wireRelativePath(ref.ID)
	return err == nil && path == ref.ID
}
func assistantPlanSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"objective":{"type":"string","maxLength":2000},"steps":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"string","maxLength":1000}},"questions":{"type":"array","maxItems":10,"items":{"type":"string","maxLength":1000}},"references":{"type":"array","maxItems":6,"items":{"type":"object","properties":{"kind":{"type":"string","enum":["file","dialect"]},"id":{"type":"string","maxLength":1024}},"required":["kind","id"],"additionalProperties":false}}},"required":["objective","steps","questions","references"],"additionalProperties":false}`)
}
func (e *assistantRunExecution) assistantPlanTools() []runcontext.HostTool {
	if e.record.Binding.Phase != "plan" {
		return nil
	}
	return []runcontext.HostTool{{Name: assistantPlanTool, Effect: "plan", InputSchema: assistantPlanSchema(), Description: "Save a reviewable Plan revision in this chat. Include ordered concrete steps and unresolved questions. Name additional saved project files or Dialect IDs; Desk pins current revisions and includes the linked object. The person must choose Start work; this grants no work, approval or compute allowance.", Execute: func(ctx context.Context, raw json.RawMessage) (runcontext.ToolResult, error) {
		a, err := readAssistantPlanArguments(raw)
		if err != nil {
			return assistantHostFailure(err), nil
		}
		if e.input == nil {
			return assistantHostFailure(errAssistantRunUnavailable), nil
		}
		e.input.planMu.Lock()
		defer e.input.planMu.Unlock()
		if err := e.Authorize(ctx); err != nil {
			return assistantHostFailure(err), nil
		}
		run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
		if err != nil {
			return assistantHostFailure(err), nil
		}
		in := assistantHostIntent{CallID: runcontext.CallIDOf(ctx), Arguments: raw}
		in.Root, _ = ctx.Value(assistantHostRootKey{}).(bool)
		in.CallbackID, _ = ctx.Value(assistantHostCallbackKey{}).(string)
		repeated := false
		for _, op := range run.Operations {
			repeated = repeated || op.ID == assistantPlanOperationID(in)
		}
		if !repeated {
			plans, err := assistantPlanSource(e.source.Original)
			if err != nil {
				return assistantHostFailure(err), nil
			}
			count := len(plans)
			for _, out := range run.DomainOutputs {
				if out.Kind == "plan" {
					count++
				}
			}
			if count >= 24 {
				return assistantHostFailure(failure.ToolInput("This chat has 24 Plan revisions. Start a new chat for another Plan.")), nil
			}
		}
		return e.invokeDomainHost(ctx, assistantPlanTool, raw, func(ctx context.Context, root bool) (runcontext.ToolResult, *assistantGraphRead, *assistantDomainOutput, error) {
			result, output, err := e.proposeAssistantPlan(ctx, a, root)
			return result, nil, output, err
		})
	}}}
}
