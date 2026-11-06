package desk

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	agentadapter "github.com/Judgment-Pack/judgment-pack-agent/go/adapter/agent"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
)

type assistantDiagnosticExecutor struct{}

func assistantDiagnosticInput(source assistantRunSource) (*runcontext.Input, error) {
	off := false
	input := &runcontext.Input{Phase: "work", Connection: source.Binding.ConnectionID, ArtifactProposals: &off, Memory: &off, CurrentRequest: &runcontext.CurrentRequest{Message: assistantDiagnosticInstruction, Input: assistantDiagnosticInstruction}}
	if source.Profile.Context != nil {
		raw, err := json.Marshal(source.Profile.Context)
		if err != nil {
			return nil, err
		}
		value, err := runcontext.ParseJSON(raw)
		if err != nil {
			return nil, err
		}
		input.Configuration, err = runcontext.ReadConfiguration(value)
		if err != nil {
			return nil, err
		}
	}
	return input, nil
}
func (*assistantDiagnosticExecutor) Prepare(ctx context.Context, source assistantRunSource) (contract.Checkpoint, error) {
	if source.Start.Purpose != assistantDiagnosticPurpose || source.Binding.Owner.Kind != assistantDiagnosticPurpose {
		return contract.Checkpoint{}, errAssistantRunScope
	}
	input, err := assistantDiagnosticInput(source)
	if err != nil {
		return contract.Checkpoint{}, err
	}
	named := runcontext.RunModels{Critic: source.Binding.Model, Child: source.Binding.Model}
	r := &adapter.Run{ModelName: source.Binding.Model, Context: input, Agent: &adapter.Agent{Model: source.Binding.Model, Effort: assistantBriefEffort(source)}}
	// Preparation establishes accounting without observing Runtime or declaring tools.
	// Execute obtains the served schema declaration through its own gate.
	opened, err := runcontext.ContextualSession(ctx, runcontext.Session{Prompt: assistantDiagnosticInstruction, AllowConversation: true, Context: input, Critic: "off", Agent: &runcontext.AgentSession{Model: source.Binding.Model, Effort: r.Agent.Effort}, PolicyLegacyModels: &named, PolicyFamilies: adapter.PolicyFamilies(r, named), PolicyConnections: append(adapter.PolicyConnectionIDs(r), named.Critic, named.Child)})
	if err != nil {
		return contract.Checkpoint{}, err
	}
	defer opened.Context.Close()
	// Native execution is provider-managed: v0.9.1 reserves LimitTo for API
	// request caps and refuses it here. Keep the released native accounting;
	// the closed schema offer and diagnostic receipt checks own this action.
	return opened.Context.Snapshot("")
}
func (*assistantDiagnosticExecutor) Execute(ctx context.Context, e *assistantRunExecution) error {
	if err := e.Authorize(ctx); err != nil {
		return err
	}
	runtime, err := e.assistantRuntime(ctx)
	if err != nil {
		return err
	}
	defer runtime.Close()
	var eventErr error
	deliver := func(event contract.RunEvent, host bool) {
		if eventErr != nil {
			return
		}
		persist, finish := e.diagnosticPersistenceContext(ctx)
		defer finish()
		switch {
		case assistantCanonicalActive(e.record):
			if event.BudgetPaused != nil || event.Proposal != nil || event.UserQuestion != nil || event.ApprovalRequested != nil || event.DelegationRequested != nil {
				eventErr = errAssistantRunScope
			} else if host {
				eventErr = e.publishCanonicalHostEvent(persist, event)
			} else {
				eventErr = e.deliverCanonicalEvent(persist, event)
			}
		case event.MessageProgress != nil:
			_ = e.Progress(ctx, *event.MessageProgress)
		case event.ExecutionCheckpoint != nil:
			eventErr = e.Checkpoint(ctx, event.ExecutionCheckpoint.Checkpoint)
		case event.BudgetPaused != nil:
			eventErr = errAssistantRunScope
		case event.Proposal != nil || event.UserQuestion != nil || event.ApprovalRequested != nil || event.DelegationRequested != nil:
			eventErr = errAssistantRunScope
		default:
			eventErr = e.writer.boundEvent(persist, event, e.boundaryProgressPreview(false))
			e.changed(eventErr)
		}
		if eventErr != nil {
			e.cancel()
		}
	}
	emit := func(event contract.RunEvent) { deliver(event, false) }
	hostEmit := func(event contract.RunEvent) { deliver(event, true) }
	// Retain the exact physical receipt before enforcing the diagnostic's existing
	// schema observation bound. An oversized schema must not enter canonical
	// context merely because the final success check would later reject it.
	observe := func(ctx context.Context, message json.RawMessage) (json.RawMessage, error) {
		reply, err := runtime.Call(ctx, message)
		if err != nil || len(reply) <= 1<<20 {
			return reply, err
		}
		var request struct {
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if json.Unmarshal(message, &request) == nil && request.Method == "tools/call" && request.Params.Name == "get_schema" {
			return nil, failure.Authored("invalid-response", "The schema response exceeded the connection check's observation limit.", "desk")
		}
		return reply, nil
	}
	g := gate.New(gate.Options{Runtime: observe, Granted: []string{"get_schema"}, Emit: hostEmit})
	offer, err := g.Open(ctx, nil)
	if err != nil {
		return err
	}
	if len(offer.Tools) != 1 || offer.Tools[0].Name != "get_schema" {
		return errAssistantRunScope
	}
	phase := gate.LoopPhase(offer)
	g.Enter(phase)
	input, err := assistantDiagnosticInput(e.source)
	if err != nil {
		return err
	}
	input.Authorize, input.Checkpoint, input.Execution = e.Authorize, e.Checkpoint, e.record.Checkpoint
	if err = e.bindCanonicalInput(ctx, input); err != nil {
		return err
	}
	agent, err := e.assistantAgent()
	if err != nil {
		return err
	}
	run := &adapter.Run{ModelName: e.record.Binding.Model, Agent: agent, Prompt: assistantDiagnosticInstruction, AllowConversation: true, Critic: "off", Gate: g.Calls(), Tools: phase.Offer(offer).Tools, Context: input, Emit: emit}
	bindings := &assistantGateBindings{}
	bindings.add(g)
	if err = bindings.bind(run); err != nil {
		return err
	}
	if err = e.canonicalAdapter(bindings.adapter(assistantCanonicalAgent(agentadapter.New())), g).Run(ctx, run); err != nil {
		return err
	}
	if eventErr != nil {
		return eventErr
	}
	latest, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return err
	}
	latest.State = "complete"
	if !assistantDiagnosticSucceeded(latest) {
		cause := failure.Authored("invalid-response", "The connection check did not complete one successful schema read through Desk.", "desk")
		hostEmit(failure.Event(cause, "desk"))
		return errors.New("connection diagnostic has no successful current schema observation")
	}
	return nil
}

// Cancellation closes dispatch immediately. Receipts and canonical termination
// still persist within the existing writer/lease, whose CAS rejects stale or
// already-stopped work. This never creates a new execution or clears a failure.
func (e *assistantRunExecution) diagnosticPersistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if e.source.Start.Purpose == assistantDiagnosticPurpose {
		return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	}
	return ctx, func() {}
}
