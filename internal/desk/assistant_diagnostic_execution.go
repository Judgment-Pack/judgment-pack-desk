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
	opened, err := runcontext.ContextualSession(ctx, runcontext.Session{Prompt: assistantDiagnosticInstruction, AllowConversation: true, Context: input, Critic: "off", Agent: &runcontext.AgentSession{Model: source.Binding.Model, Effort: r.Agent.Effort}, Tools: []runcontext.RuntimeTool{{Name: "get_schema"}}, PolicyLegacyModels: &named, PolicyFamilies: adapter.PolicyFamilies(r, named), PolicyConnections: append(adapter.PolicyConnectionIDs(r), named.Critic, named.Child)})
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
	emit := func(event contract.RunEvent) {
		if eventErr != nil {
			return
		}
		persist, finish := e.diagnosticPersistenceContext(ctx)
		defer finish()
		switch {
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
	g := gate.New(gate.Options{Runtime: runtime.Call, Granted: []string{"get_schema"}, Emit: emit})
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
	agent, err := e.assistantAgent()
	if err != nil {
		return err
	}
	run := &adapter.Run{ModelName: e.record.Binding.Model, Agent: agent, Prompt: assistantDiagnosticInstruction, AllowConversation: true, Critic: "off", Gate: g.Calls(), Tools: phase.Offer(offer).Tools, Context: input, Emit: emit}
	if err = assistantCanonicalAgent(agentadapter.New()).Run(ctx, run); err != nil {
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
		emit(failure.Event(cause, "desk"))
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
