package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

// These markers are installed at the actual adapter/callback boundaries, never
// read from restored context, arguments, model output or browser metadata.
type assistantHostRootKey struct{}
type assistantHostCallbackKey struct{}
type assistantHostPreconditionKey struct{}
type assistantHostPrecondition struct{ fresh func(context.Context) error }

func withAssistantHostPrecondition(ctx context.Context, fresh func(context.Context) error) context.Context {
	return context.WithValue(ctx, assistantHostPreconditionKey{}, assistantHostPrecondition{fresh: fresh})
}
func recheckAssistantHost(ctx context.Context) error {
	if held, ok := ctx.Value(assistantHostPreconditionKey{}).(assistantHostPrecondition); ok {
		return held.fresh(ctx)
	}
	return nil
}

func assistantHostAdapter(inner adapter.Adapter, binding assistantRunBinding, request runcontext.CurrentRequest) adapter.Adapter {
	return adapter.Func(func(ctx context.Context, run *adapter.Run) error {
		return inner.Run(context.WithValue(ctx, assistantHostRootKey{}, run.Context != nil && run.Context.CurrentRequest != nil && *run.Context.CurrentRequest == request && run.Context.Execution != nil && run.Context.Execution.RequestKey == binding.ExecutionKey), run)
	})
}

type assistantHostRegistry struct{ tools []runcontext.HostTool }

func newAssistantHostRegistry(tools []runcontext.HostTool) (*assistantHostRegistry, error) {
	seen := map[string]bool{}
	for _, t := range tools {
		if t.Name == "" || seen[t.Name] || t.Execute == nil || !json.Valid(t.InputSchema) || !contains([]string{"read", "proposal"}, t.Effect) || (t.Delegation != "" && (t.Delegation != "read" || t.Effect != "read")) {
			return nil, errAssistantRunInvalid
		}
		seen[t.Name] = true
	}
	return &assistantHostRegistry{tools: append([]runcontext.HostTool(nil), tools...)}, nil
}
func (r *assistantHostRegistry) declarations() []contract.ToolOfferHostTool {
	out := make([]contract.ToolOfferHostTool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t.Declaration())
	}
	return out
}
func (r *assistantHostRegistry) offered(phase gateHostPhase) []runcontext.HostTool {
	out := []runcontext.HostTool{}
	for _, t := range r.tools {
		if phase.host(t.Name) {
			out = append(out, t)
		}
	}
	return out
}

// Keep the registry independent of engine-specific offer construction.
type gateHostPhase struct{ names []string }

func (p gateHostPhase) host(name string) bool { return contains(p.names, name) }

func assistantHostArguments(raw json.RawMessage, into any) error {
	if len(raw) > 6*maxFileBytes || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return failure.ToolInput("Provide a bounded JSON object with the declared arguments.")
	}
	// configMembers refuses duplicate members before decoding can discard them.
	if _, err := configMembers(raw); err != nil {
		return failure.ToolInput("Provide one value for each declared argument.")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(into) != nil || d.Decode(new(any)) != io.EOF {
		return failure.ToolInput("Use only the declared arguments with their declared types.")
	}
	return nil
}
func assistantHostText(text string) (runcontext.ToolResult, error) {
	if len(text) > 1024*1024 {
		return runcontext.ToolResult{}, failure.ToolInput("This decision context exceeds 1 MiB. Read fewer packs at a time.")
	}
	return runcontext.ResultOf([]runcontext.Content{runcontext.TextContent(text)}, nil), nil
}
func assistantHostJSON(value any) (runcontext.ToolResult, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return runcontext.ToolResult{}, err
	}
	return assistantHostText(string(raw))
}
func assistantHostFailure(err error) runcontext.ToolResult {
	// Internal process/storage details are never model-facing failure prose.
	if _, ok := err.(*failure.Error); !ok {
		err = failure.Authored("configuration", "The assistant tool could not complete. Inspect retained work before trying another operation.", "desk")
	}
	text, raw := failure.ToolFailure(err)
	structured, _ := runcontext.ParseJSON(raw)
	return runcontext.ResultOf([]runcontext.Content{runcontext.TextContent(text)}, structured).WithIsError(true)
}
