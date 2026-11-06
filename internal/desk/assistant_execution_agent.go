package desk

import (
	"context"
	"encoding/json"
	"errors"
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
func (e *assistantRunExecution) assistantAgent(scopes ...*assistantExecutionInvocation) (*adapter.Agent, error) {
	var scope *assistantExecutionInvocation
	if len(scopes) != 0 {
		scope = scopes[0]
	}
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
		if scope != nil {
			if err := scope.check(ctx); err != nil {
				return err
			}
		}
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
		if scope != nil {
			if err := scope.check(ctx); err != nil {
				return err
			}
		}
		settlement := newAssistantNativeSettlement(ctx, e)
		if scope != nil {
			if err := scope.native(settlement); err != nil {
				return err
			}
		}
		stopping, _ := ctx.Value(assistantAgentStopKey{}).(*assistantAgentStop)
		caller := ctx
		if stopping != nil {
			stopping.reset()
			// The package cancels its provider attempt when a Tool asks to stop.
			// Preserve the outer caller separately from that attempt context.
			caller = stopping.parent
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
		// Process release is not callback join or result acknowledgement. Record
		// the actual capability result; missing/duplicate/late proof stays unknown.
		bridge.ProviderReleased = func() {
			acknowledged := false
			if release := callbacks.ProviderRelease; release != nil && release.Acknowledge != nil {
				acknowledged = release.Acknowledge(release.Invocation)
			}
			settlement.release(acknowledged)
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
		var toolErr error
		events := []adapter.AgentEvent{}
		bytes := 0
		runErr := runner.Run(ctx, owner, bridge, func(event codexbridge.RunEvent) error {
			token, err := settlement.begin("")
			if err != nil {
				return err
			}
			defer settlement.end(token)
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
			err = callbacks.Event(ctx, held)
			settlement.eventResult(err)
			return err
		}, func(callCtx context.Context, call codexbridge.ToolCall) (_ codexbridge.ToolAnswer, returnedErr error) {
			token, err := settlement.begin(call.ID)
			if err != nil {
				return codexbridge.ToolAnswer{}, err
			}
			defer settlement.end(token)
			mu.Lock()
			defer mu.Unlock()
			defer func() {
				if returnedErr != nil && toolErr == nil {
					toolErr = returnedErr
				}
			}()
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
			if err = settlement.admit(token, op); err != nil {
				return codexbridge.ToolAnswer{}, err
			}
			if stopping != nil {
				stopping.begin(call.ID, assistantNativePlanAlias(request.Tools, call.Name))
			}
			answer, callErr := callbacks.Tool(context.WithValue(context.WithValue(callCtx, assistantHostCallbackKey{}, op.ID), assistantHostCallbackAliasKey{}, call.Name), adapter.AgentToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
			if len(answer.Text) > 1<<20 {
				return codexbridge.ToolAnswer{}, codexbridge.ErrLimit
			}
			outcome := assistantAgentOutcome{Answer: &answer, Failure: assistantAgentFailure(callErr)}
			approval, plan := false, false
			if stopping != nil && callErr != nil {
				outcome.Stop = stopping.label()
				if outcome.Stop == "approval" {
					approval = settlement.approvalCallback(token, stopping) == nil
					if !approval {
						outcome.Stop = ""
					}
				}
				if outcome.Stop == "plan" {
					plan = settlement.planCallback(token, stopping) == nil
					if !plan {
						outcome.Stop = ""
					}
				}
				if outcome.Stop != "" {
					outcome.Failure = nil
				}
			}
			result, _ := json.Marshal(outcome)
			// writer.Result already bounds persistence of observed results after
			// cancellation. No new dispatch receives a replacement context.
			err = e.Result(callCtx, op.ID, "completed", result)
			settlement.receipt(token, approval, err)
			if err != nil {
				return codexbridge.ToolAnswer{}, err
			}
			if stopping != nil {
				stopping.finish(call.ID, callErr != nil && (stopping.label() != "approval" && stopping.label() != "plan" || approval || plan))
			}
			return codexbridge.ToolAnswer{Text: answer.Text, IsError: answer.IsError}, callErr
		})
		// Manager may return while its cancelled callback goroutine is still
		// returning. Close admission first and join within the owner deadline;
		// never block indefinitely on the callback's serialization mutex.
		if err := settlement.closeAndJoin(e.ctx); err != nil {
			e.cancel()
			if writeErr := e.writer.Err(); writeErr != nil {
				return writeErr
			}
			return err
		}
		if err := e.writer.Err(); err != nil {
			e.cancel()
			return err
		}
		mu.Lock()
		closed = true
		// The supported AgentRun route returns the exact Tool error once the
		// actual provider is released and every callback has joined. A Manager
		// cancellation caused by that Tool is not the caller's cancellation.
		settlement.mu.Lock()
		toolReturned := toolErr != nil && settlement.closed && settlement.joinedClosed && !settlement.aborted && settlement.releases == 1 && settlement.released
		settlement.mu.Unlock()
		// The selected Manager returns ErrRun immediately on a Tool error,
		// before reading another frame. Other provider/protocol errors survive.
		managerToolStop := runErr == nil || runErr == codexbridge.ErrRun || errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded)
		toolReturned = toolReturned && managerToolStop && e.Authorize(e.ctx) == nil
		if scope != nil && scope.check(e.ctx) != nil {
			toolReturned = false
		}
		if err := caller.Err(); err != nil {
			runErr, toolReturned = err, false
		} else if err := e.ctx.Err(); err != nil {
			runErr, toolReturned = err, false
		} else if toolReturned {
			runErr = toolErr
		}
		knownStop := stopping != nil && stopping.acknowledged() && e.Authorize(e.ctx) == nil
		if knownStop && stopping.label() == "approval" {
			knownStop = settlement.approvalModel(dispatch.operation) == nil
		}
		if knownStop && stopping.label() == "plan" {
			knownStop = settlement.planModel(dispatch.operation) == nil
		}
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
		err = e.Result(ctx, dispatch.operation.ID, state, result)
		settlement.modelReceipt(err, runErr == nil || knownStop, stopKind == "approval")
		if err != nil {
			return err
		}
		// Only the bridge's own pre-admission sentinel permits the published
		// adapter's bounded retries. No wrapper or callback error is retried.
		if !admitted && runErr == codexbridge.ErrBusy {
			if release := callbacks.ProviderRelease; release != nil && release.NotAdmitted != nil {
				settlement.notAdmitted(release.NotAdmitted(release.Invocation))
			}
			return adapter.ErrBusy
		}
		if err := caller.Err(); err != nil {
			e.cancel()
			return err
		}
		if err := e.ctx.Err(); err != nil && toolReturned {
			return err
		}
		if runErr != nil && !knownStop && (!toolReturned || failure.IsCancelled(runErr)) {
			e.cancel()
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
	if code, detail := codexHostFailure(err); code != "" {
		held = failure.Authored(code, detail, "agent").Failure
	}
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
	execution  string
	planAlias  string
	plan       *assistantPlanDecisionReference
	retained   bool
}

func (s *assistantAgentStop) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.call, s.kind, s.retained = "", "", false
	s.planAlias, s.plan = "", nil
}

// Only the exact alias in the admitted native request can identify update_plan.
func assistantNativePlanAlias(tools []adapter.AgentTool, name string) string {
	for _, tool := range tools {
		if tool.Name == name && strings.HasPrefix(tool.Description, "update_plan:") {
			return name
		}
	}
	return ""
}
func (s *assistantAgentStop) begin(id string, planAlias ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.call, s.planAlias = id, ""
	if len(planAlias) == 1 {
		s.planAlias = planAlias[0]
	}
}
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
	case event.ApprovalRequested != nil && event.ApprovalRequested.Purpose != nil && *event.ApprovalRequested.Purpose == "plan-drift" && s.planAlias != "" && event.ApprovalRequested.Plan != nil && s.execution != "" && event.ApprovalRequested.Plan.Execution == s.execution:
		held := event.ApprovalRequested
		ref := &assistantPlanDecisionReference{held.CallID, held.Plan.Revision, held.Plan.CandidateRevision}
		if validAssistantRunPlanDecisionReference(ref) {
			s.kind, s.plan = "plan", ref
		}
	case event.ApprovalRequested != nil && event.ApprovalRequested.CallID == s.call:
		s.kind = "approval"
	}
}
func (s *assistantAgentStop) approvalFor(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.parent.Err() == nil && s.call == id && s.kind == "approval"
}

// The Plan approval id intentionally differs from the native callback id.
// This candidate still requires original-head and callback receipt qualification.
func (s *assistantAgentStop) planFor(id, alias string) *assistantPlanDecisionReference {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parent.Err() != nil || s.call != id || s.kind != "plan" || s.planAlias == "" || s.planAlias != alias || s.plan == nil {
		return nil
	}
	copy := *s.plan
	return &copy
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
		if run.Context != nil && run.Context.Execution != nil {
			stop.execution = run.Context.Execution.ID
		}
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
