package desk

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Saved chat and creation share the production factory, real chat source
// resolver and managed context.
type assistantCreationExecutor struct{}

func (*assistantCreationExecutor) Prepare(ctx context.Context, source assistantRunSource) (contract.Checkpoint, error) {
	if source.Start.Purpose != "chat" {
		return contract.Checkpoint{}, errAssistantRunScope
	}
	input, err := compileAssistantChatInput(source)
	if err != nil {
		return contract.Checkpoint{}, err
	}
	effort := ""
	if source.Connection.Assistant.Agent != nil {
		config := source.Connection.Assistant.Agent
		model := config.Model
		effort = config.Effort
		if prefs := assistantChatPreferences(source); prefs != nil && !prefs.Inherit {
			effort, model = prefs.Effort, prefs.Model
		}
		if model == nil || *model != source.Binding.Model {
			effort = ""
		}
	}
	if input.Reasoning != nil && input.Reasoning.Get("model").Str() == source.Binding.Model {
		effort = "" // A null override selects the model default.
		if value := input.Reasoning.Get("effort"); value != nil && value.IsString() {
			effort = value.Str()
		}
	}
	purpose := source.Start.Purpose
	if input.CriticPurpose != "" {
		purpose = input.CriticPurpose
	}
	return prepareAssistantContextSession(ctx, source, input, purpose, effort)
}

func (*assistantCreationExecutor) Execute(ctx context.Context, e *assistantRunExecution) (err error) {
	defer func() {
		if err == nil {
			return
		}
		retained, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer finish()
		if stopErr := e.writer.Stop(retained); stopErr == nil {
			_ = e.ProjectChat(retained)
		}
	}()
	handled, err := e.executeAssistantCreation(ctx, e.reviewAssistantCreation)
	if handled || err != nil {
		return err
	}
	execution, err := e.openAssistantExecution(ctx, assistantExecutionPrompt{Name: "author_pack"})
	if err != nil {
		return err
	}
	defer execution.Close()
	if err := execution.Adapter.Run(ctx, execution.Run); err != nil {
		return err
	}
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return err
	}
	if run.Checkpoint == nil || run.Checkpoint.Status != "complete" {
		return nil
	}
	if question, err := pendingAssistantRunQuestion(run); err != nil || question != nil {
		return err
	}
	if err := e.writer.State(ctx, "complete"); err != nil {
		return err
	}
	return e.ProjectChat(ctx)
}

func (e *assistantRunExecution) reviewAssistantCreation(ctx context.Context, input *assistantCreationInput) (json.RawMessage, error) {
	execution, err := e.openAssistantExecution(ctx, assistantExecutionPrompt{Name: "test_pack"})
	if err != nil {
		return nil, err
	}
	defer execution.Close()
	if err = execution.Adapter.Run(ctx, execution.Run); err != nil {
		return nil, err
	}
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil || run.Checkpoint == nil || run.Checkpoint.Status != "complete" {
		return nil, errAssistantRunScope
	}
	var proposal json.RawMessage
	for _, event := range run.Events {
		if event.Event.Proposal == nil {
			continue
		}
		if proposal != nil {
			return nil, errAssistantRunInvalid
		}
		proposal = assistantChatJSON(event.Event.Proposal.Document)
	}
	if proposal == nil {
		return nil, errAssistantRunInvalid
	}
	// The adapter has finished its single admitted review. The owning workflow
	// still has model-free validation/retest work before the run is terminal.
	cp := *run.Checkpoint
	cp.Status = "running"
	if err := e.Checkpoint(ctx, cp); err != nil {
		return nil, err
	}
	return proposal, nil
}
