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
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/turn"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
)

// Private host construction only. This is deliberately not registered as a
// production executor: domain tools, client actions and final journal CAS must
// be migrated before a saved-chat purpose can become available.
type assistantExecution struct {
	Run     *adapter.Run
	Adapter adapter.Adapter
	runtime *assistantRuntime
	gate    *gate.Gate
}

type assistantExecutionPrompt struct {
	Name      string
	Arguments map[string]string
}

func (x *assistantExecution) Close() { x.runtime.Close() }

func (e *assistantRunExecution) assistantExecutionModels() (runcontext.RunModels, error) {
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

func (e *assistantRunExecution) openAssistantExecution(ctx context.Context, prompt assistantExecutionPrompt) (_ *assistantExecution, err error) {
	// Saved chat uses method guidance only. Request/document prompt arguments
	// belong to explicit authoring panes; accepting them here would duplicate or
	// smuggle the retained request into instruction material.
	if len(prompt.Arguments) != 0 {
		return nil, errAssistantRunInvalid
	}
	if err := e.Authorize(ctx); err != nil {
		return nil, err
	}
	chat, err := e.prepareAssistantChatInput(ctx)
	if err != nil {
		return nil, err
	}
	memory, err := e.assistantChatMemory(ctx)
	if err != nil {
		return nil, err
	}
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
	runtime, err := e.assistantRuntime(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			runtime.Close()
		}
	}()
	var g *gate.Gate
	saveCheckpoint := func(ctx context.Context, cp contract.Checkpoint) error {
		if cp.ID != e.record.Checkpoint.ID || cp.RequestKey != e.record.Binding.ExecutionKey {
			e.cancel()
			return errAssistantRunScope
		}
		if g != nil {
			count := int64(g.TestCalls())
			cp.TestCalls = &count
		}
		cp = assistantCriticCheckpoint(cp, critic)
		return e.Checkpoint(ctx, cp)
	}
	emit := func(event contract.RunEvent) {
		if event.MessageProgress != nil {
			return // live prefixes are not durable semantic events
		}
		if event.ExecutionCheckpoint != nil {
			_ = saveCheckpoint(ctx, event.ExecutionCheckpoint.Checkpoint)
			return
		}
		if event.BudgetPaused != nil {
			_ = saveCheckpoint(ctx, event.BudgetPaused.Checkpoint)
			return
		}
		err := e.writer.boundEvent(ctx, event)
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
	testCalls := 0
	if e.record.Checkpoint.TestCalls != nil {
		testCalls = int(*e.record.Checkpoint.TestCalls)
	}
	options := gate.Options{Runtime: runtime.Call, Granted: granted, TestCalls: testCalls, Emit: emit}
	g = gate.New(options)
	// The coordinator supplies the canonical declaration/function; the host
	// gate permits this interaction without granting any domain action.
	interaction := "delegation"
	registry, err := e.assistantConversationTools(runtime, chat.Policy)
	if err != nil {
		return nil, err
	}
	declarations := append(registry.declarations(), contract.ToolOfferHostTool{Name: turn.DelegateTool, Interaction: &interaction})
	offer, err := g.Open(ctx, declarations)
	if err != nil {
		return nil, err
	}
	phase := gate.LoopPhase(offer)
	if e.record.Binding.Phase == "plan" {
		phase = gate.PlanPhase(offer)
	}
	g.Enter(phase)
	promptText, err := runtime.Prompt(ctx, prompt)
	if err != nil {
		return nil, err
	}
	// Metadata is host-only, retained through the actual transport. It is never
	// added to the model's tool offer or used as permission to enlarge it.
	if _, err = runtime.rpc(ctx, "tools/call", map[string]any{"name": "describe_runtime", "arguments": map[string]any{}}); err != nil {
		return nil, err
	}
	input := &runcontext.Input{Phase: e.record.Binding.Phase, Connection: e.record.Binding.ConnectionID, Authorize: e.Authorize, Checkpoint: saveCheckpoint, Execution: e.record.Checkpoint, Approval: e.decision, CurrentRequest: &chat.Request, Documents: chat.Documents, CurrentState: chat.State, Memory: memory}
	packID, _, _ := assistantSavedPackLink(e.source.Original)
	if packID != "" || chat.Policy != nil && chat.Policy.ID != "" {
		off := false
		input.ArtifactProposals = &off
	}
	if critic != nil {
		checkpoint := assistantCriticCheckpoint(*input.Execution, critic)
		input.Execution = &checkpoint
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
	if chat.CriticPurpose != "" {
		purpose = chat.CriticPurpose
	}
	run := &adapter.Run{HostTools: registry.offered(gateHostPhase{names: phase.Host}), Purpose: purpose, ModelName: e.record.Binding.Model, Thinking: chat.Thinking, Critic: chat.Critic, CriticProject: e.source.Profile.Critic, CriticReasoning: chat.CriticReasoning, Review: chat.Review, ExistingPack: critic != nil && critic.path != "", Gate: g.Calls(), Tools: phase.Offer(offer).Tools, Prompt: promptText, AllowConversation: true, Interactive: true, Context: input, Host: host, Emit: emit}
	if critic != nil {
		run.TestPrompt, err = runtime.criticPrompt(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range critic.cases {
			run.Cases = append(run.Cases, append(json.RawMessage(nil), row...))
		}
	}
	var base adapter.Adapter = eino.New()
	if e.source.Connection.Assistant.Engine == "codex" {
		run.Agent, err = e.assistantAgent()
		base = assistantCanonicalAgent(agentadapter.New())
	} else {
		run.Model, err = e.assistantModel()
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
		if err := e.Authorize(ctx); err != nil {
			return runcontext.RunModels{}, err
		}
		return e.assistantExecutionModels()
	}
	run.Children = func(ctx context.Context, profile contract.DelegatedTaskProfile, childEmit func(contract.RunEvent)) (*gate.Calls, []contract.ToolOfferRuntimeTool, error) {
		if err := e.Authorize(ctx); err != nil {
			return nil, nil, err
		}
		child := gate.New(gate.Options{Runtime: runtime.Call, Granted: granted, Turn: g, Emit: childEmit})
		child.SetOffer(offer)
		childPhase := gate.ChildPhase(profile, offer, phase)
		child.Enter(childPhase)
		return child.Calls(), childPhase.Offer(offer).Tools, nil
	}
	base = assistantHostAdapter(base, e.record.Binding, chat.Request)
	return &assistantExecution{Run: run, Adapter: delegate.With(base, delegate.Options{}), runtime: runtime, gate: g}, nil
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
