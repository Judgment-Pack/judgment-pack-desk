package desk

import (
	"context"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// assistantCanonicalFixtureCommit is how a test executor of a canonical run
// (one that keeps assistantCanonicalRuns, as the brief, authoring and
// production executors do) publishes its events and its checkpoint. Since
// Agent v0.13.2 such a run keeps a canonical journal: an adapter binds the
// run's input to Agent's durable writer (bindCanonicalInput), the writer
// commits each event and checkpoint there, and the store projects the root
// events into the public run. A public event or checkpoint written beside the
// journal is refused (validateAssistantCanonicalProjection), and a Stop
// finalizes through the binding the adapter installed (finalizeCanonicalStop).
// The fixture binds as an adapter does and writes through that writer: each
// event, then the checkpoint with its status set to status, or no checkpoint
// where status is empty.
func assistantCanonicalFixtureCommit(ctx context.Context, e *assistantRunExecution, status string, events ...contract.RunEvent) error {
	input := &runcontext.Input{}
	if err := e.bindCanonicalInput(ctx, input); err != nil {
		return err
	}
	binding := input.Persistence
	if binding == nil {
		return errAssistantRunScope
	}
	e.canonicalMu.Lock()
	generation := e.generation
	e.canonicalMu.Unlock()
	owned := e.generationContext(ctx, generation)
	for _, event := range events {
		if _, err := binding.Writer.Event(owned, *binding, event); err != nil {
			e.changed(err)
			return err
		}
	}
	e.changed(nil)
	if status == "" {
		return nil
	}
	cp := binding.Writer.Checkpoint()
	if cp == nil {
		return errAssistantRunScope
	}
	next := *cp
	next.Status = status
	err := input.Checkpoint(ctx, next)
	e.changed(err)
	return err
}
