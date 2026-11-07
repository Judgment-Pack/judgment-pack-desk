package desk

import (
	"context"
	"sync"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
)

// Place inside delegate.With: the released coordinator installs its decorator
// before its first inner Run. Compose it once before opening a session; all
// children subsequently share the same stable writer and live turn counter.
func (e *assistantRunExecution) canonicalAdapter(inner adapter.Adapter, g *gate.Gate) adapter.Adapter {
	if !assistantCanonicalActive(e.record) {
		return inner
	}
	e.canonicalMu.Lock()
	original := e.canonicalBinding
	e.canonicalMu.Unlock()
	var installs sync.Mutex
	installed := map[*runcontext.RunWriter]bool{}
	return adapter.Func(func(ctx context.Context, run *adapter.Run) error {
		input := run.Context
		if input == nil || input.Persistence == nil || original == nil || input.Persistence.Writer != original.Writer || input.Persistence.Root && input.Persistence != original {
			return &runcontext.ContinuationIdentityChanged{}
		}
		installs.Lock()
		if !installed[input.Persistence.Writer] {
			installed[input.Persistence.Writer] = true
			binding := input.Persistence
			previous := binding.Writer.Decorate
			binding.Writer.Decorate = func(cp contract.Checkpoint) (contract.Checkpoint, error) {
				if previous != nil {
					var err error
					cp, err = previous(cp)
					if err != nil {
						return contract.Checkpoint{}, err
					}
				}
				if g != nil {
					count := int64(g.TestCalls())
					cp.TestCalls = &count
				}
				return cp, nil
			}
		}
		installs.Unlock()
		held := *run
		held.Context = input
		scoped, closeScope, err := e.assistantExternalScopedRun(ctx, &held)
		if err != nil {
			return err
		}
		defer closeScope()
		return inner.Run(ctx, scoped)
	})
}

// Agent has already atomically committed checkpoints and scoped events before
// Emit. Delivery never appends them again, including one-use Plan deliveries.
func (e *assistantRunExecution) canonicalRunID() string {
	if e.record.Observation != nil {
		return e.record.Observation.RunID
	}
	if e.record.Canonical != nil {
		return e.record.Canonical.Head.RunID
	}
	return ""
}

func (e *assistantRunExecution) deliverCanonicalEvent(ctx context.Context, event contract.RunEvent) error {
	e.canonicalMu.Lock()
	current := e.generation == 0 || e.generationMatches(ctx, e.generation) && !e.stopRequested && e.lifecycle == "running"
	e.canonicalMu.Unlock()
	if !current {
		return errAssistantRunScope
	}
	if event.MessageProgress != nil {
		return e.Progress(ctx, *event.MessageProgress)
	}
	id := e.canonicalRunID()
	if id == "" || event.ExecutionCheckpoint != nil && event.ExecutionCheckpoint.Checkpoint.ID != id || event.BudgetPaused != nil && event.BudgetPaused.Checkpoint.ID != id {
		e.changed(errAssistantRunScope)
		return errAssistantRunScope
	}
	if event.ExecutionCheckpoint != nil && event.ExecutionCheckpoint.Checkpoint.Status != "running" || event.BudgetPaused != nil {
		e.clearProgress()
	}
	err := e.saveProgressPreview(ctx, event.Message != nil)
	e.changed(err)
	return err
}

// Gate and host-origin notices do not traverse Agent's Boundary. Give them an
// explicit ingress, never infer publication by comparing serialized events.
func (e *assistantRunExecution) publishCanonicalHostEvent(ctx context.Context, event contract.RunEvent) error {
	if !assistantCanonicalActive(e.record) && e.record.Observation == nil {
		err := e.writer.boundEvent(ctx, event, e.boundaryProgressPreview(event.Message != nil))
		e.changed(err)
		return err
	}
	if event.MessageProgress != nil {
		return e.Progress(ctx, *event.MessageProgress)
	}
	e.canonicalMu.Lock()
	binding := e.canonicalBinding
	current := e.generation == 0 || e.generationMatches(ctx, e.generation) && !e.stopRequested && e.lifecycle == "running"
	e.canonicalMu.Unlock()
	if !current {
		return errAssistantRunScope
	}
	var err error
	if binding != nil {
		_, err = binding.Writer.Event(ctx, *binding, event)
	} else {
		err = &runcontext.ContinuationNotRecoverable{}
	}
	if err == nil {
		return e.deliverCanonicalEvent(ctx, event)
	}
	e.changed(err)
	return err
}

// Cancellation and binding installation share this lock so a late installation
// cannot reopen dispatch after the provider has been canceled.
func (e *assistantRunExecution) closeCanonicalDispatch() {
	// A Commit may hold canonicalMu while waiting for a lease retained by a
	// blocked pipe write. Interrupt the exact transport before entering that
	// chain; cancellation never waits for process reaping or writer/lease locks.
	if runtime := e.runtimeOwner.Load(); runtime != nil {
		runtime.cancel()
	}
	e.canonicalMu.Lock()
	defer e.canonicalMu.Unlock()
	e.stopRequested = true
	if e.canonicalStore != nil {
		e.canonicalStore.ingress.Lock()
		e.canonicalStore.accept = false
		e.canonicalStore.ingress.Unlock()
	}
	if binding := e.canonicalBinding; binding != nil {
		_ = binding.Writer.CloseDispatch(*binding)
	}
}

// Only the executor-return path finalizes. Action handlers request cancellation
// and await that owner without retaining the service mutex.
func (e *assistantRunExecution) finalizeCanonicalStop() error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(e.ctx), 5*time.Second)
	defer cancel()
	latest, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return err
	}
	if latest.Checkpoint != nil && latest.Checkpoint.Status != "running" && latest.Checkpoint.Status != "paused" {
		return nil
	}
	e.canonicalMu.Lock()
	binding := e.canonicalBinding
	generation := e.generation
	e.canonicalMu.Unlock()
	ctx = context.WithValue(ctx, assistantInvocationGenerationKey{}, assistantInvocationGeneration{owner: e, number: generation, finalizer: true})
	if binding == nil {
		if assistantCanonicalActive(latest) {
			return &runcontext.ContinuationNotRecoverable{}
		}
		return nil
	}
	_, err = binding.Writer.FinalizeStop(ctx, *binding)
	return err
}
