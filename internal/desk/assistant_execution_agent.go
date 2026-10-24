package desk

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// This capability retains the server-selected account and owner. Neither a
// browser callback nor an agent request can select an account or supply a fence.
func (e *assistantRunExecution) assistantAgent() (*adapter.Agent, error) {
	if err := e.Authorize(e.ctx); err != nil {
		return nil, err
	}
	s := e.service.server
	config := e.source.Connection.Assistant
	if config.Engine != "codex" || config.Agent == nil {
		return nil, errAssistantRunUnavailable
	}
	var chat *assistantChatInput
	var err error
	if e.source.Start.Purpose != "brief" && e.source.Start.Purpose != assistantDiagnosticPurpose {
		chat, err = compileAssistantExecutionInput(e.source)
		if err != nil {
			return nil, err
		}
	}
	// Resolve the retained validated profile using connectionModels' preference
	// selection and inheritance rules. A non-inherited preference with omitted
	// effort deliberately requests the model default, rather than the shared value.
	effort := config.Agent.Effort
	model := config.Agent.Model
	preferences := assistantChatPreferences(e.source)
	if preferences != nil && !preferences.Inherit {
		effort = preferences.Effort
		model = preferences.Model
	}
	// Shared/project effort belongs to its selected default model. A saved
	// chat override belongs to the model it explicitly names (reasoning.ts).
	if model == nil || *model != e.record.Binding.Model {
		effort = ""
	}
	if chat != nil && chat.Reasoning != nil && chat.Reasoning.Get("model").Str() == e.record.Binding.Model {
		effort = ""
		if choice := chat.Reasoning.Get("effort"); choice.IsString() {
			effort = choice.Str()
		}
	}
	r := e.request.Clone(e.ctx)
	r.Header.Set(aiConnectionHeader, e.record.Binding.ConnectionID)
	r.Header.Set(aiRevisionHeader, e.record.Binding.ConnectionRevision)
	account, err := s.accountForConnection(r)
	if err != nil {
		return nil, err
	}
	// Require the actual private bridge, rather than another provider loop.
	switch account.(type) {
	case *codexbridge.Manager, *codexbridge.LazyManager:
	default:
		return nil, errAssistantRunUnavailable
	}
	runner := account.(providerRunner)
	_, session, err := s.assistantRunIdentity(r)
	if err != nil {
		return nil, err
	}
	s.signIn.mu.Lock()
	policy := s.signIn.epoch
	s.signIn.mu.Unlock()
	owner := codexbridge.Owner{ID: s.sessions.handle(bearerOf(r)), Session: session, Policy: policy}
	enabled := func(ctx context.Context, model string) error {
		if err := e.Authorize(ctx); err != nil {
			e.cancel()
			return err
		}
		if e.source.Start.Purpose == "brief" {
			if err := e.recheckAssistantBriefInput(ctx); err != nil {
				e.cancel()
				return err
			}
		}
		if !s.connectionAgentAllowed(r.WithContext(ctx), model) {
			e.cancel()
			return errAssistantRunScope
		}
		return nil
	}
	return &adapter.Agent{Model: e.record.Binding.Model, Effort: effort, Enabled: enabled, Run: func(ctx context.Context, request adapter.AgentRequest, callbacks adapter.AgentCallbacks) error {
		if callbacks.Event == nil || callbacks.Tool == nil {
			return errAssistantRunInvalid
		}
		stopping, _ := ctx.Value(assistantAgentStopKey{}).(*assistantAgentStop)
		if stopping != nil {
			stopping.reset()
		}
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(e.ctx, cancel)
		defer stop()
		defer cancel()
		if err := enabled(ctx, request.Model); err != nil {
			return err
		}
		if e.source.Start.Purpose == assistantDiagnosticPurpose {
			tools := []adapter.AgentTool{}
			for _, tool := range request.Tools {
				if strings.HasPrefix(tool.Description, "get_schema:") {
					tools = append(tools, tool)
				}
			}
			request.Tools = tools
			if request.Model != e.record.Binding.Model || len(tools) != 1 {
				return errAssistantRunScope
			}
		}
		intent, err := json.Marshal(request)
		if err != nil {
			return err
		}
		dispatch := &assistantModelDispatch{execution: e, operation: assistantRunOperation{Kind: "model", Name: request.Model, Intent: intent}}
		if err := dispatch.prepare(ctx); err != nil {
			return err
		}
		bridge := codexbridge.RunRequest{Model: request.Model, Prompt: request.Prompt, Instructions: request.Instructions, Effort: request.Effort, Phase: request.Phase}
		for _, tool := range request.Tools {
			bridge.Tools = append(bridge.Tools, codexbridge.Tool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
		}
		admitted := false
		bridge.AuthorizeAdmission = func(ctx context.Context) error {
			admitted = true // subsequent errors can never mean account-busy admission
			return enabled(ctx, request.Model)
		}
		bridge.BeforeGeneration = func(ctx context.Context) error {
			if err := enabled(ctx, request.Model); err != nil {
				return err
			}
			return dispatch.dispatch(ctx)
		}
		// Manager validates native identity, offer, duplicate calls and bounded
		// payloads. Keep callbacks serialized and close their retained pairing
		// before returning even when cancellation races a host callback.
		var mu sync.Mutex
		closed := false
		events := []adapter.AgentEvent{}
		bytes := 0
		runErr := runner.Run(ctx, owner, bridge, func(event codexbridge.RunEvent) error {
			mu.Lock()
			defer mu.Unlock()
			if closed || !dispatch.admitted.Load() {
				return errAssistantRunDispatch
			}
			if err := enabled(ctx, request.Model); err != nil {
				return err
			}
			held := adapter.AgentEvent{Type: event.Type, ID: event.ID, Text: event.Text, Phase: event.Phase, Attempt: event.Attempt}
			bytes += len(event.Text) + 256
			if bytes > assistantRunRecordLimit/2 {
				return codexbridge.ErrLimit
			}
			events = append(events, held)
			return callbacks.Event(ctx, held)
		}, func(callCtx context.Context, call codexbridge.ToolCall) (codexbridge.ToolAnswer, error) {
			mu.Lock()
			defer mu.Unlock()
			if closed || !dispatch.admitted.Load() {
				return codexbridge.ToolAnswer{}, errAssistantRunDispatch
			}
			if err := enabled(callCtx, request.Model); err != nil {
				return codexbridge.ToolAnswer{}, err
			}
			body, err := json.Marshal(struct {
				Invocation string               `json:"invocation"`
				Call       codexbridge.ToolCall `json:"call"`
			}{dispatch.operation.ID, call})
			if err != nil {
				return codexbridge.ToolAnswer{}, err
			}
			op, err := e.boundIntent(callCtx, "callback", call.Name, func(string) ([]byte, error) { return body, nil })
			if err != nil {
				return codexbridge.ToolAnswer{}, err
			}
			if err = e.Dispatch(callCtx, op.ID); err != nil {
				return codexbridge.ToolAnswer{}, err
			}
			if stopping != nil {
				stopping.begin(call.ID)
			}
			answer, callErr := callbacks.Tool(context.WithValue(callCtx, assistantHostCallbackKey{}, op.ID), adapter.AgentToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
			if len(answer.Text) > 1<<20 {
				return codexbridge.ToolAnswer{}, codexbridge.ErrLimit
			}
			outcome := assistantAgentOutcome{Answer: &answer, Failure: assistantAgentFailure(callErr)}
			if stopping != nil && callErr != nil {
				outcome.Stop = stopping.label()
				if outcome.Stop != "" {
					outcome.Failure = nil
				}
			}
			result, _ := json.Marshal(outcome)
			if err = e.Result(callCtx, op.ID, "completed", result); err != nil {
				return codexbridge.ToolAnswer{}, err
			}
			if stopping != nil {
				stopping.finish(call.ID, callErr != nil)
			}
			return codexbridge.ToolAnswer{Text: answer.Text, IsError: answer.IsError}, callErr
		})
		mu.Lock()
		closed = true
		knownStop := stopping != nil && stopping.acknowledged() && e.Authorize(e.ctx) == nil
		heldFailure := assistantAgentFailure(runErr)
		stopKind := ""
		if knownStop {
			stopKind = stopping.label()
			heldFailure = nil
		}
		if heldFailure != nil && !dispatch.admitted.Load() {
			heldFailure.Outcome = "not-started"
			if !admitted && runErr == codexbridge.ErrBusy {
				held := failure.Normalize(adapter.ErrBusy, "agent", time.Time{})
				heldFailure = &held
			}
		}
		result, err := json.Marshal(assistantAgentOutcome{Events: events, Failure: heldFailure, Stop: stopKind})
		mu.Unlock()
		if err != nil {
			e.cancel()
			return err
		}
		state := "completed"
		if !dispatch.admitted.Load() {
			state = "refused"
		}
		if err = e.Result(ctx, dispatch.operation.ID, state, result); err != nil {
			return err
		}
		// Only the bridge's own pre-admission sentinel permits the published
		// adapter's bounded retries. No wrapper or callback error is retried.
		if !admitted && runErr == codexbridge.ErrBusy {
			return adapter.ErrBusy
		}
		if runErr != nil {
			if !knownStop {
				e.cancel()
			}
		}
		return runErr
	}}, nil
}

type assistantAgentOutcome struct {
	Stop    string                   `json:"stop,omitempty"`
	Events  []adapter.AgentEvent     `json:"events,omitempty"`
	Answer  *adapter.AgentToolAnswer `json:"answer,omitempty"`
	Failure *contract.Failure        `json:"failure,omitempty"`
}

func assistantAgentFailure(err error) *contract.Failure {
	if err == nil {
		return nil
	}
	// An admitted failure remains an observed unknown outcome, regardless of
	// whether its error happens to resemble a not-started busy failure.
	held := failure.Normalize(err, "agent", time.Time{})
	held.Outcome = "unknown"
	return &held
}

// Only the host's wrapper around the published adapter can create this marker.
// Typed canonical events must occur inside the matching retained callback; a
// caller canceling an invocation cannot manufacture a known handoff or pause.
type assistantAgentStopKey struct{}
type assistantAgentStop struct {
	mu         sync.Mutex
	parent     context.Context
	call, kind string
	retained   bool
}

func (s *assistantAgentStop) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.call, s.kind, s.retained = "", "", false
}
func (s *assistantAgentStop) begin(id string) { s.mu.Lock(); defer s.mu.Unlock(); s.call = id }
func (s *assistantAgentStop) observe(event contract.RunEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.call == "" {
		return
	}
	switch {
	case event.DelegationRequested != nil && event.DelegationRequested.CallID != nil && *event.DelegationRequested.CallID == s.call:
		s.kind = "delegation"
	case event.UserQuestion != nil && event.UserQuestion.ID != "":
		s.kind = "question"
	case event.ApprovalRequested != nil && event.ApprovalRequested.CallID == s.call:
		s.kind = "approval"
	}
}
func (s *assistantAgentStop) finish(id string, stopped bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retained = s.call == id && s.kind != "" && stopped
	s.call = ""
}
func (s *assistantAgentStop) acknowledged() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.retained && s.parent.Err() == nil
}
func assistantCanonicalAgent(inner adapter.Adapter) adapter.Adapter {
	return adapter.Func(func(ctx context.Context, run *adapter.Run) error {
		stop := &assistantAgentStop{parent: ctx}
		held := *run
		held.Emit = func(event contract.RunEvent) { stop.observe(event); run.Emit(event) }
		return inner.Run(context.WithValue(ctx, assistantAgentStopKey{}, stop), &held)
	})
}

func (s *assistantAgentStop) label() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parent.Err() != nil {
		return ""
	}
	return s.kind
}
