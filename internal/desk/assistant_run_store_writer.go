package desk

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// A writer owns one admitted run. Persistence failures latch: a future adapter's
// void Emit callback must cancel and inspect Err rather than silently continue.
// This writer grants no authorization; the runner still checks the live session,
// policy, owner and capability gate immediately before every external dispatch.
type assistantRunWriter struct {
	mu       sync.Mutex
	lease    *assistantRunLease
	binding  assistantRunBinding
	id       string
	revision uint64
	failure  error
}

func (l *assistantRunLease) Writer(ctx context.Context, binding assistantRunBinding, id string) (*assistantRunWriter, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil, errAssistantRunScope
	}
	run, err := l.store.Read(ctx, binding, id)
	if err != nil {
		return nil, err
	}
	return &assistantRunWriter{lease: l, binding: run.Binding, id: id, revision: run.Revision}, nil
}
func (w *assistantRunWriter) Err() error { w.mu.Lock(); defer w.mu.Unlock(); return w.failure }

// Mutation callbacks return exact duplicates before testing the writer revision.
// Result delivery may finish after cancellation/lease loss, but cannot dispatch
// anything. All other writes require the live exclusive executor lease.
func (w *assistantRunWriter) change(ctx context.Context, result bool, fn func(*assistantRunRecord) (bool, error)) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failure != nil && !result {
		return w.failure
	}
	if !result {
		w.lease.mu.Lock()
		defer w.lease.mu.Unlock()
		if w.lease.file == nil {
			return w.fail(errAssistantRunScope)
		}
	}
	ledger, err := w.lease.store.transaction(ctx, func(ledger *assistantRunLedger) (bool, error) {
		run, err := assistantRunBound(ledger, w.binding, w.id)
		if err != nil {
			return false, err
		}
		changed, err := fn(run)
		if err != nil || !changed {
			return changed, err
		}
		if !result && run.Revision != w.revision {
			return false, errAssistantRunConflict
		}
		run.Revision++
		return true, nil
	})
	if err != nil {
		return w.fail(err)
	}
	for _, run := range ledger.Runs {
		if run.ID == w.id {
			w.revision = run.Revision
			return nil
		}
	}
	return w.fail(errAssistantRunInvalid)
}
func (w *assistantRunWriter) fail(err error) error {
	if w.failure == nil {
		w.failure = err
	}
	return err
}

func (w *assistantRunWriter) Checkpoint(ctx context.Context, state contract.Checkpoint) error {
	return w.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		cp, err := readAssistantRunCheckpoint(state, run.Binding)
		if err != nil {
			return false, err
		}
		if sameAssistantRunCheckpoint(cp, run.Checkpoint) {
			return false, nil
		}
		if assistantRunTerminal(run.State) {
			return false, errAssistantRunDispatch
		}
		if run.Checkpoint != nil && (cp.ID != run.Checkpoint.ID || cp.Used < run.Checkpoint.Used) {
			return false, errAssistantRunConflict
		}
		run.Checkpoint = cp
		if run.State == "admitted" {
			run.State = "running"
		}
		return true, nil
	})
}
func (w *assistantRunWriter) Event(ctx context.Context, id string, event contract.RunEvent) error {
	return w.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		raw, err := assistantRunEventBytes(event)
		if err != nil {
			return false, err
		}
		if !assistantRunText(id, 128) {
			return false, errAssistantRunInvalid
		}
		for _, held := range run.Events {
			if held.ID == id {
				saved, _ := assistantRunEventBytes(held.Event)
				if bytes.Equal(saved, raw) {
					return false, nil
				}
				return false, errAssistantRunConflict
			}
		}
		if assistantRunTerminal(run.State) {
			return false, errAssistantRunDispatch
		}
		if len(run.Events) >= assistantRunEventLimit {
			return false, withCode(CodeTooLarge, errAssistantRunInvalid)
		}
		run.Events = append(run.Events, assistantRunEvent{ID: id, Event: event})
		return true, nil
	})
}
func (w *assistantRunWriter) Intent(ctx context.Context, operation assistantRunOperation) error {
	return w.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		if operation.State != "" && operation.State != "not-started" || operation.Result != nil {
			return false, errAssistantRunInvalid
		}
		for _, held := range run.Operations {
			if held.ID == operation.ID {
				if held.CallID == operation.CallID && held.Kind == operation.Kind && held.Name == operation.Name && bytes.Equal(held.Intent, operation.Intent) {
					return false, nil
				}
				return false, errAssistantRunConflict
			}
			if held.CallID == operation.CallID {
				return false, errAssistantRunConflict
			}
		}
		if assistantRunTerminal(run.State) || run.Checkpoint == nil {
			return false, errAssistantRunDispatch
		}
		if len(run.Operations) >= assistantRunOperationLimit {
			return false, withCode(CodeTooLarge, errAssistantRunInvalid)
		}
		operation.State = "not-started"
		run.Operations = append(run.Operations, operation)
		return true, nil
	})
}

// Dispatch returns success once only, after verified durable receipt. If the
// process dies between this write and the external call, recovery is uncertain;
// a replacement must never interpret that receipt as permission to call again.
func (w *assistantRunWriter) Dispatch(ctx context.Context, id string) error {
	return w.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		if assistantRunTerminal(run.State) || run.Checkpoint == nil || run.Checkpoint.Status != "running" {
			return false, errAssistantRunDispatch
		}
		for i := range run.Operations {
			if run.Operations[i].ID == id {
				if run.Operations[i].State != "not-started" {
					return false, errAssistantRunDispatch
				}
				run.Operations[i].State = "dispatched"
				return true, nil
			}
		}
		return false, errAssistantRunInvalid
	})
}
func (w *assistantRunWriter) Result(ctx context.Context, id, state string, result []byte) error {
	// The result is an observed outcome, never a retry. A canceled request must
	// still retain it; storage has its own bounded lock wait and write limits.
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return w.change(bounded, true, func(run *assistantRunRecord) (bool, error) {
		if result == nil || state != "completed" && state != "refused" {
			return false, errAssistantRunInvalid
		}
		for i := range run.Operations {
			op := &run.Operations[i]
			if op.ID != id {
				continue
			}
			if op.State == "completed" || op.State == "refused" {
				if op.State == state && bytes.Equal(op.Result, result) {
					return false, nil
				}
				return false, errAssistantRunConflict
			}
			if op.State != "dispatched" && op.State != "uncertain" && !(op.State == "not-started" && state == "refused") {
				return false, errAssistantRunDispatch
			}
			op.State = state
			op.Result = result
			return true, nil
		}
		return false, errAssistantRunInvalid
	})
}

// State is the host envelope, not a framework execution state. The owning
// workflow must persist the corresponding canonical checkpoint first.
func (w *assistantRunWriter) State(ctx context.Context, state string) error {
	return w.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		if run.State == state {
			return false, nil
		}
		if !contains([]string{"awaiting-input", "awaiting-client", "paused-budget", "running", "complete", "failed", "stopped"}, state) {
			return false, errAssistantRunInvalid
		}
		if assistantRunTerminal(run.State) || run.Checkpoint == nil {
			return false, errAssistantRunDispatch
		}
		expected := "stopped"
		switch state {
		case "complete":
			expected = "complete"
		case "paused-budget":
			expected = "paused"
		case "running":
			expected = "running"
		}
		if run.Checkpoint.Status != expected {
			return false, errAssistantRunConflict
		}
		for i := range run.Operations {
			op := &run.Operations[i]
			if state == "complete" && op.State != "completed" && op.State != "refused" {
				return false, errAssistantRunDispatch
			}
			if (state == "failed" || state == "stopped") && op.State == "dispatched" {
				op.State = "uncertain"
			}
		}
		run.State = state
		return true, nil
	})
}
