package desk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	agentadapter "github.com/Judgment-Pack/judgment-pack-agent/go/adapter/agent"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/delegate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/eino"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
)

// Private host construction only. This is deliberately not registered as a
// production executor: domain tools, client actions and final journal CAS must
// be migrated before a saved-chat purpose can become available.
type assistantExecution struct {
	Run        *adapter.Run
	Adapter    adapter.Adapter
	runtime    *assistantRuntime
	gate       *gate.Gate
	invocation *assistantExecutionInvocation
	notices    *assistantGateNoticeRelay
	prompt     assistantExecutionPrompt
	retained   bool

	// Only immutable Runtime method guidance is retained across an approval.
	// Chat/request/document context is rebuilt for each actual adapter opening.
	methodText            string
	methodCriticText      string
	methodCriticReady     bool
	methodPrompt          assistantExecutionPrompt
	methodSource          string
	methodOwner           *assistantRuntimeOwner
	methodOwnerIdentity   gate.RuntimeExecutionOwner
	methodRuntimeIdentity *runcontext.RuntimeIdentity
}

type assistantExecutionPrompt struct {
	Name      string
	Arguments map[string]string
}

const assistantRetainedGuidanceChanged = "RetainedRuntimeGuidanceChanged: the original Runtime owner, source, prompt or identity changed. Nothing was sent."

func assistantRetainedGuidanceRefusal() error {
	held := failure.Authored("stale-state", assistantRetainedGuidanceChanged, "context")
	held.Failure.Outcome = "not-started"
	return held
}

func cloneAssistantExecutionPrompt(prompt assistantExecutionPrompt) assistantExecutionPrompt {
	copy := assistantExecutionPrompt{Name: prompt.Name}
	if prompt.Arguments != nil {
		copy.Arguments = make(map[string]string, len(prompt.Arguments))
		for key, value := range prompt.Arguments {
			copy.Arguments[key] = value
		}
	}
	return copy
}

func cloneAssistantRuntimeIdentity(identity *runcontext.RuntimeIdentity) *runcontext.RuntimeIdentity {
	if identity == nil {
		return nil
	}
	copy := *identity
	return &copy
}

// Match the immutable facts used by assistantRunFreshSourceEqual. Host closures
// and the current transcript are not retained method-guidance facts.
func assistantExecutionMethodSource(source assistantRunSource) (string, error) {
	raw, err := json.Marshal(struct {
		Binding           assistantRunBinding
		Original, Request []byte
		Plan              *assistantRunPlan
	}{source.Binding, source.Original, source.Request, source.Plan})
	if err != nil {
		return "", err
	}
	return digestOf(raw), nil
}

func (e *assistantRunExecution) retainedAssistantMethod(ctx context.Context, held *assistantExecution, prompt assistantExecutionPrompt, input *runcontext.Input) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := e.ctx.Err(); err != nil {
		return "", err
	}
	source, err := assistantExecutionMethodSource(e.source)
	if err != nil || held == nil || !held.retained || held.methodText == "" || !held.methodCriticReady || held.methodSource != source || !sameAssistantExecutionPrompt(prompt, held.methodPrompt) || held.runtime == nil || held.runtime.execution != e || held.runtime.owner == nil || held.runtime.owner != held.methodOwner || held.invocation == nil || held.invocation.owner != e || input == nil || input.Persistence == nil || input.Persistence.Writer == nil || held.invocation.binding == nil || held.invocation.binding.Writer == nil || held.invocation.binding.Writer.Store != input.Persistence.Writer.Store || held.invocation.binding.Writer.ID != input.Persistence.Writer.ID || !e.runtimeContinuationAlive(held.runtime) {
		return "", assistantRetainedGuidanceRefusal()
	}
	original, current := held.methodRuntimeIdentity, input.Runtime
	if (original == nil) != (current == nil) || original != nil && (original.Identity != current.Identity || original.Version != current.Version) {
		return "", assistantRetainedGuidanceRefusal()
	}
	owner, err := held.runtime.owner.Current(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if e.ctx.Err() != nil {
			return "", e.ctx.Err()
		}
		return "", assistantRetainedGuidanceRefusal()
	}
	if owner != held.methodOwnerIdentity {
		return "", assistantRetainedGuidanceRefusal()
	}
	return held.methodText, nil
}

func (x *assistantExecution) Close() {
	if !x.retained {
		if x.invocation != nil {
			e := x.invocation.owner
			e.canonicalMu.Lock()
			if e.gateNotices == x.notices {
				e.gateNotices = nil
			}
			e.canonicalMu.Unlock()
		}
		x.runtime.Close()
	}
}

func (e *assistantRunExecution) assistantExecutionModels() (runcontext.RunModels, error) {
	if e.source.Start.Purpose == "brief" || e.source.Start.Purpose == assistantSettingsObservationPurpose {
		return runcontext.ReadRunModels(runcontext.RunIdentity{Connection: e.record.Binding.ConnectionID, Model: e.record.Binding.Model}, runcontext.NamedModels{})
	}
	named := runcontext.NamedModels{}
	connection := e.source.Connection.Assistant
	if connection.Engine == "codex" && connection.Agent != nil {
		named.Critic, named.Child = connection.Agent.CriticModel, connection.Agent.ChildModel
	} else if connection.Engine == "vercel" && connection.Endpoint != nil {
		named.Critic, named.Child = connection.Endpoint.CriticModel, connection.Endpoint.ChildModel
		if connection.Endpoint.Capacity != 0 {
			named.ConcurrentRequests = &connection.Endpoint.Capacity
		}
	} else {
		return runcontext.RunModels{}, errAssistantRunUnavailable
	}
	models, err := runcontext.ReadRunModels(runcontext.RunIdentity{Connection: e.record.Binding.ConnectionID, Model: e.record.Binding.Model}, named)
	if err != nil {
		return models, err
	}
	allowed := connectionModels(&e.source.Connection, &e.source.Profile)
	if !contains(allowed, models.Critic) || !contains(allowed, models.Child) {
		return models, errAssistantRunScope
	}
	return models, nil
}

func (e *assistantRunExecution) openAssistantExecution(ctx context.Context, prompt assistantExecutionPrompt) (*assistantExecution, error) {
	return e.reopenAssistantExecution(ctx, prompt, nil, nil)
}

func (e *assistantRunExecution) reopenAssistantExecution(ctx context.Context, prompt assistantExecutionPrompt, held *assistantExecution, prepared *runcontext.Input) (_ *assistantExecution, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if held != nil && !sameAssistantExecutionPrompt(prompt, held.methodPrompt) {
		return nil, assistantRetainedGuidanceRefusal()
	}
	creation, err := compileAssistantCreationInput(e.source)
	if err != nil {
		return nil, err
	}
	creationReview := creation != nil && contains([]string{"establish-cases", "propose-correction"}, creation.Action.Kind)
	// Saved chat uses method guidance only. Request/document prompt arguments
	// belong to explicit authoring panes; accepting them here would duplicate or
	// smuggle the retained request into instruction material.
	if assistantAuthoringPurpose(e.source.Start.Purpose) {
		expected, promptErr := assistantAuthoringPrompt(e.source)
		if promptErr != nil || !sameAssistantExecutionPrompt(prompt, expected) {
			return nil, errAssistantRunInvalid
		}
	} else if len(prompt.Arguments) != 0 {
		return nil, errAssistantRunInvalid
	}
	if err := e.Authorize(ctx); err != nil {
		if e.ctx.Err() != nil {
			return nil, e.ctx.Err()
		}
		if held != nil && errors.Is(err, errAssistantRunScope) {
			return nil, assistantRetainedGuidanceRefusal()
		}
		return nil, err
	}
	checkpoint := e.record.Checkpoint
	if assistantCanonicalActive(e.record) {
		latest, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
		if err != nil || !assistantCanonicalActive(latest) || validateAssistantCanonicalProjection(latest) != nil {
			return nil, &runcontext.ContinuationNotRecoverable{}
		}
		checkpoint = latest.Checkpoint
	}
	// A saved-chat answer already completed initial source/custody preparation
	// before admission. Consume its exact value once; later reopens stay fresh.
	var chat *assistantChatInput
	if held == nil {
		e.canonicalMu.Lock()
		chat, e.initialInput = e.initialInput, nil
		e.canonicalMu.Unlock()
	}
	if chat == nil {
		chat, err = e.prepareAssistantInput(ctx)
		if err != nil {
			return nil, err
		}
	}
	var g *gate.Gate
	saveCheckpoint := func(ctx context.Context, cp contract.Checkpoint) error {
		if cp.ID != checkpoint.ID || cp.RequestKey != e.record.Binding.ExecutionKey {
			e.cancel()
			return errAssistantRunScope
		}
		if g != nil {
			count := int64(g.TestCalls())
			cp.TestCalls = &count
		}
		return e.Checkpoint(ctx, cp)
	}
	input := &runcontext.Input{Chat: e.source.chatScope(), Phase: e.record.Binding.Phase, Connection: e.record.Binding.ConnectionID, Authorize: e.Authorize, Checkpoint: saveCheckpoint, Execution: checkpoint, Approval: e.decision, CurrentRequest: &chat.Request, Documents: chat.Documents, CurrentState: chat.State}
	if held != nil {
		if prepared == nil || prepared.Persistence == nil || held.invocation == nil || !held.notices.parked(held.invocation.generation) {
			return nil, errAssistantRunScope
		}
		input.Persistence, input.RunStore = prepared.Persistence, prepared.RunStore
		input.Approval, input.Chat, input.Runtime = prepared.Approval, prepared.Chat, prepared.Runtime
	}
	if err := e.bindCanonicalInput(ctx, input); err != nil {
		return nil, err
	}
	if err := bindAssistantRunPlan(e.source, input); err != nil {
		return nil, err
	}
	if err := e.guardAssistantNativePlanContinuation(ctx, input, creation == nil); err != nil {
		return nil, err
	}
	input, err = e.prepareAssistantPlanDecision(ctx, input)
	if err != nil {
		return nil, err
	}
	checkpoint = input.Execution
	offMemory := false
	memory := &offMemory
	if !assistantAuthoringPurpose(e.source.Start.Purpose) && creation == nil {
		memory, err = e.assistantChatMemory(ctx)
	}
	if err != nil {
		return nil, err
	}
	input.Memory = memory
	var critic *assistantCriticSnapshot
	if assistantCriticEnabled(chat, e.record.Binding.Phase) && runcontext.CriticForcedOff(runcontext.CriticForcedFacts{Purpose: chat.CriticPurpose}) == "" {
		critic, err = e.holdAssistantCritic(ctx)
		if err != nil {
			return nil, err
		}
	}
	host, err := e.service.server.assistantPromptHost(ctx)
	if err != nil {
		return nil, err
	}
	var runtime *assistantRuntime
	if held == nil {
		runtime, err = e.assistantRuntime(ctx)
	} else {
		runtime = held.runtime
		if runtime == nil || !e.runtimeContinuationAlive(runtime) {
			return nil, assistantRetainedGuidanceRefusal()
		}
	}
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			runtime.Close()
		}
	}()
	var promptText string
	if held != nil {
		// Agent must consume the explicit approval and acknowledge its running
		// checkpoint before any new Runtime dispatch. Reuse only the original
		// method guidance after exact retained-owner/currentness checks.
		promptText, err = e.retainedAssistantMethod(ctx, held, prompt, input)
		if err != nil {
			return nil, err
		}
	}
	var invocation *assistantExecutionInvocation
	var notices *assistantGateNoticeRelay
	if assistantCanonicalActive(e.record) {
		invocation, err = newAssistantExecutionInvocation(ctx, e)
		if err != nil {
			return nil, err
		}
		if held == nil {
			notices = &assistantGateNoticeRelay{}
		} else {
			notices = held.notices
		}
		input.Authorize = invocation.check
		// Stop and generation installation share canonicalMu. A prepared sink
		// cannot be installed after Stop or against a replaced binding, even
		// though its captured authority would also reject later publication.
		e.canonicalMu.Lock()
		if e.stopRequested || e.lifecycle != "running" || e.generation != invocation.generation || e.canonicalBinding != invocation.binding || e.canonicalStore != invocation.store || held == nil && e.gateNotices != nil || held != nil && e.gateNotices != notices {
			e.canonicalMu.Unlock()
			return nil, errAssistantRunScope
		}
		if err = notices.install(invocation); err != nil {
			e.canonicalMu.Unlock()
			return nil, err
		}
		if held == nil {
			e.gateNotices = notices
		}
		e.canonicalMu.Unlock()
	}
	emit := func(event contract.RunEvent) {
		if invocation != nil {
			invocation.deliver(event)
			return
		}
		if event.MessageProgress != nil {
			_ = e.Progress(ctx, *event.MessageProgress)
			return // live prefixes never enter the durable journal
		}
		if event.ExecutionCheckpoint != nil {
			_ = saveCheckpoint(ctx, event.ExecutionCheckpoint.Checkpoint)
			return
		}
		if event.BudgetPaused != nil {
			_ = saveCheckpoint(ctx, event.BudgetPaused.Checkpoint)
			return
		}
		err := e.writer.boundEvent(ctx, event, e.boundaryProgressPreview(event.Message != nil))
		e.changed(err)
	}
	endpoint := e.source.Connection.Assistant.Endpoint
	var granted []string
	if e.source.Connection.Assistant.Engine == "codex" && e.source.Connection.Assistant.Agent != nil {
		granted = e.source.Connection.Assistant.Agent.Tools
	} else if endpoint != nil {
		granted = endpoint.Tools
	} else {
		return nil, errAssistantRunUnavailable
	}
	if creation != nil {
		// Independent review cannot evaluate the draft or retrieve its rules.
		// Research continuation uses only canonical read host capabilities.
		granted = nil
	}
	testCalls := 0
	if checkpoint.TestCalls != nil {
		testCalls = int(*checkpoint.TestCalls)
	}
	gateEmit := emit
	if invocation != nil {
		gateEmit = notices.emit
	}
	options := gate.Options{Runtime: runtime.Call, RuntimeActions: runtime.owner.port(), Granted: granted, TestCalls: testCalls, Emit: gateEmit}
	if held == nil {
		g = gate.New(options)
	} else {
		g = held.gate
		if g == nil || g.Calls() != held.Run.Gate {
			return nil, errAssistantRunScope
		}
	}
	bindings := &assistantGateBindings{invocation: invocation}
	bindings.add(g)
	var registry *assistantHostRegistry
	if creationReview {
		registry, err = newAssistantHostRegistry(nil)
	} else if assistantAuthoringPurpose(e.source.Start.Purpose) {
		registry, err = e.assistantAuthoringTools()
	} else {
		registry, err = e.assistantConversationTools(runtime, chat.Policy)
	}
	if err != nil {
		return nil, err
	}
	if creation != nil && creation.Action.Kind == "research-choice" {
		reads := []runcontext.HostTool{}
		for _, tool := range registry.tools {
			if assistantHostDeclaredRead(tool) {
				reads = append(reads, tool)
			}
		}
		registry, err = newAssistantHostRegistry(reads)
		if err != nil {
			return nil, err
		}
	}
	// Runtime supplies its actual offer; only the actual adapter session may
	// enroll host and package declarations later.
	offer := g.Offer()
	if held == nil {
		offer, err = g.Open(ctx, nil)
		if err != nil {
			return nil, err
		}
		phase := gate.LoopPhase(offer)
		if e.record.Binding.Phase == "plan" {
			phase = gate.PlanPhase(offer)
		}
		g.Enter(phase)
	}
	phase := g.Phase()
	// Domain filtering precedes context construction and is independent of the
	// gate's initially absent host metadata. Package actions come from context.
	domainOffer := contract.ToolOffer{HostTools: registry.declarations()}
	domainPhase := gate.LoopPhase(domainOffer)
	if e.record.Binding.Phase == "plan" {
		domainPhase = gate.PlanPhase(domainOffer)
	}
	if held == nil {
		promptText, err = runtime.Prompt(ctx, prompt)
		if err != nil {
			return nil, err
		}
		// Host-only immutable metadata from the actual original transport.
		// It never enlarges the model offer or authorizes an action.
		descriptor, descriptorErr := runtime.rpc(ctx, "tools/call", map[string]any{"name": "describe_runtime", "arguments": map[string]any{}})
		if descriptorErr != nil {
			return nil, descriptorErr
		}
		input.Runtime = assistantRuntimeCacheIdentity(descriptor)
	}
	methodText := promptText
	packID := ""
	if e.source.Start.Purpose == "chat" {
		packID, _, _ = assistantSavedPackLink(e.source.Original)
	}
	if packID != "" || chat.Policy != nil && chat.Policy.ID != "" || creation != nil && creation.Action.Kind == "research-choice" {
		off := false
		input.ArtifactProposals = &off
	}
	if e.source.Profile.Context != nil {
		raw, marshalErr := json.Marshal(e.source.Profile.Context)
		if marshalErr != nil {
			return nil, marshalErr
		}
		value, parseErr := runcontext.ParseJSON(raw)
		if parseErr != nil {
			return nil, parseErr
		}
		input.Configuration, err = runcontext.ReadConfiguration(value)
		if err != nil {
			return nil, err
		}
	}
	promptText += "\n\n" + chat.Transcript + "\n\nTHE PERSON'S MESSAGE\n" + chat.Request.Input
	purpose := e.source.Start.Purpose
	if assistantAuthoringPurpose(purpose) {
		purpose = ""
	}
	if chat.CriticPurpose != "" {
		purpose = chat.CriticPurpose
	}
	hostTools := registry.offered(gateHostPhase{names: domainPhase.Host})
	if invocation != nil {
		for i := range hostTools {
			execute := hostTools[i].Execute
			hostTools[i].Execute = func(callCtx context.Context, args json.RawMessage) (runcontext.ToolResult, error) {
				if err := invocation.check(callCtx); err != nil {
					return runcontext.ToolResult{}, err
				}
				return execute(callCtx, args)
			}
		}
	}
	run := &adapter.Run{PolicyProject: assistantMemoryProposalPolicy(e.record.Binding), HostTools: hostTools, Purpose: purpose, ModelName: e.record.Binding.Model, Thinking: chat.Thinking, Critic: assistantCriticChoice(chat), CriticProject: e.source.Profile.Critic, CriticReasoning: chat.CriticReasoning, ExistingPack: critic != nil && critic.path != "", Gate: g.Calls(), Tools: phase.Offer(offer).Tools, Prompt: promptText, AllowConversation: true, Interactive: true, Context: input, Host: host, Emit: emit}
	if critic != nil {
		if held == nil {
			run.TestPrompt, err = runtime.criticPrompt(ctx)
			if err != nil {
				return nil, err
			}
		} else {
			// retainedAssistantMethod already qualified the exact original owner,
			// source and identity, including completed critic discovery. Empty
			// preserves its acknowledged lack of test_pack support.
			run.TestPrompt = held.methodCriticText
		}
		for _, row := range critic.cases {
			run.Cases = append(run.Cases, append(json.RawMessage(nil), row...))
		}
	}
	var base adapter.Adapter = eino.New()
	if e.source.Connection.Assistant.Engine == "codex" {
		run.Agent, err = e.assistantAgent(invocation)
		base = assistantCanonicalAgent(agentadapter.New())
	} else {
		run.Model, err = e.assistantModel(invocation)
		if endpoint != nil {
			run.Wire = endpoint.Kind
		}
	}
	if err != nil {
		return nil, err
	}
	if endpoint != nil && endpoint.Capabilities != nil {
		raw, marshalErr := json.Marshal(endpoint.Capabilities)
		if marshalErr != nil {
			return nil, marshalErr
		}
		run.Capabilities = &contract.CapabilityEndpointCapabilities{}
		if err = json.Unmarshal(raw, run.Capabilities); err != nil {
			return nil, err
		}
	}
	run.Models = func() (runcontext.RunModels, error) {
		if invocation != nil {
			if err := invocation.check(ctx); err != nil {
				return runcontext.RunModels{}, err
			}
		}
		if err := e.Authorize(ctx); err != nil {
			return runcontext.RunModels{}, err
		}
		return e.assistantExecutionModels()
	}
	run.Children = func(ctx context.Context, profile contract.DelegatedTaskProfile, childEmit func(contract.RunEvent)) (*gate.Calls, []contract.ToolOfferRuntimeTool, error) {
		if invocation != nil {
			if err := invocation.check(ctx); err != nil {
				return nil, nil, err
			}
		}
		if err := e.Authorize(ctx); err != nil {
			return nil, nil, err
		}
		calls, tools := bindings.child(g, profile, gate.Options{Runtime: runtime.Call, Granted: granted, Turn: g, Emit: childEmit})
		return calls, tools, nil
	}
	if creation != nil {
		run.Children = nil
	}
	// Native restoration in delegate.With may enroll before reaching inner.Run.
	if err := bindings.bind(run); err != nil {
		return nil, err
	}
	base = bindings.adapter(base)
	base = assistantHostAdapter(base, e.record.Binding, chat.Request)
	base = e.canonicalAdapter(base, g)
	outer := delegate.With(base, delegate.Options{})
	if invocation != nil {
		outer = invocation.adapter(outer)
	}
	methodSource, err := assistantExecutionMethodSource(e.source)
	if err != nil {
		return nil, err
	}
	return &assistantExecution{Run: run, Adapter: outer, runtime: runtime, gate: g, invocation: invocation, notices: notices, prompt: cloneAssistantExecutionPrompt(prompt), methodText: methodText, methodCriticText: run.TestPrompt, methodCriticReady: true, methodPrompt: cloneAssistantExecutionPrompt(prompt), methodSource: methodSource, methodOwner: runtime.owner, methodOwnerIdentity: runtime.owner.identity, methodRuntimeIdentity: cloneAssistantRuntimeIdentity(input.Runtime)}, nil
}

func (r *assistantRuntime) rpc(ctx context.Context, method string, params any) (json.RawMessage, error) {
	frame, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	reply, err := r.Call(ctx, frame)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if json.Unmarshal(reply, &envelope) != nil || envelope.Error != nil || envelope.Result == nil {
		return nil, errors.New("the Runtime refused the assistant host request")
	}
	return envelope.Result, nil
}

// Read the served prompt's own arguments and messages. Desk has no copy of a
// pack schema or prompt assembler, and omitted required arguments refuse.
func (r *assistantRuntime) Prompt(ctx context.Context, request assistantExecutionPrompt) (string, error) {
	listed, err := r.rpc(ctx, "prompts/list", map[string]any{})
	if err != nil {
		return "", err
	}
	var catalog struct {
		Prompts []struct {
			Name      string `json:"name"`
			Arguments []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
			} `json:"arguments"`
		} `json:"prompts"`
	}
	if json.Unmarshal(listed, &catalog) != nil {
		return "", errAssistantRunInvalid
	}
	found := false
	for _, prompt := range catalog.Prompts {
		if prompt.Name != request.Name {
			continue
		}
		if found {
			return "", errAssistantRunInvalid
		}
		found = true
		for _, argument := range prompt.Arguments {
			if _, present := request.Arguments[argument.Name]; argument.Required && !present {
				return "", errAssistantRunInvalid
			}
		}
	}
	if !found {
		return "", errAssistantRunUnavailable
	}
	arguments := request.Arguments
	if arguments == nil {
		arguments = map[string]string{}
	}
	answer, err := r.rpc(ctx, "prompts/get", map[string]any{"name": request.Name, "arguments": arguments})
	if err != nil {
		return "", err
	}
	var prompt struct {
		Messages []struct {
			Content struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(answer, &prompt) != nil || len(prompt.Messages) == 0 {
		return "", errAssistantRunInvalid
	}
	var parts []string
	for _, message := range prompt.Messages {
		if message.Content.Type != "text" {
			return "", errAssistantRunInvalid
		}
		parts = append(parts, message.Content.Text)
	}
	text := strings.Join(parts, "\n\n")
	if text == "" {
		return "", errAssistantRunInvalid
	}
	return text, nil
}

func (e *assistantRunExecution) boundIntent(ctx context.Context, kind, name string, intent func(string) ([]byte, error)) (assistantRunOperation, error) {
	if err := e.Authorize(ctx); err != nil {
		e.cancel()
		return assistantRunOperation{}, err
	}
	op, err := e.writer.boundIntent(ctx, kind, name, intent)
	e.changed(err)
	return op, err
}
