package desk

import (
	"context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Production dispatch is a closed list of saved, independently authorized
// source owners. Neither a request nor retained prose supplies an executor.
type assistantProductionExecutor struct{}

func assistantProductionPurpose(purpose string) (assistantRunExecutor, error) {
	switch purpose {
	case "chat":
		return &assistantCreationExecutor{}, nil
	case "brief":
		return &assistantBriefExecutor{}, nil
	case "author-pack", "describe-pack", "author-graph":
		return &assistantAuthoringExecutor{}, nil
	default:
		return nil, errAssistantRunScope
	}
}
func (*assistantProductionExecutor) Prepare(ctx context.Context, source assistantRunSource) (contract.Checkpoint, error) {
	executor, err := assistantProductionPurpose(source.Start.Purpose)
	if err != nil {
		return contract.Checkpoint{}, err
	}
	return executor.Prepare(ctx, source)
}
func (*assistantProductionExecutor) Execute(ctx context.Context, e *assistantRunExecution) error {
	executor, err := assistantProductionPurpose(e.source.Start.Purpose)
	if err != nil {
		return err
	}
	return executor.Execute(ctx, e)
}
