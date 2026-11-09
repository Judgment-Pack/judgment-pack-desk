package desk

import (
	"bytes"
	"context"
	"errors"
	"reflect"
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
	if l.recoveryErr != nil {
		return nil, errAssistantRunRecovery
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
	return w.changeAt(ctx, result, nil, fn)
}

// Actions use the live writer but retain their exact optimistic revision.
// A stale client refusal is not a storage failure in that writer.
func (w *assistantRunWriter) changeAt(ctx context.Context, result bool, expected *uint64, fn func(*assistantRunRecord) (bool, error)) error {
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
		if w.lease.recoveryErr != nil {
			return w.fail(errAssistantRunRecovery)
		}
	}
	return w.changeLocked(ctx, result, expected, func(change func(*assistantRunLedger) (bool, error)) (assistantRunLedger, error) {
		return w.lease.store.transaction(ctx, change)
	}, fn)
}

// The caller owns writer and (unless result delivery) the executor lease.
// Share revision, duplicate and failure behavior with guarded reservations.
func (w *assistantRunWriter) changeLocked(ctx context.Context, result bool, expected *uint64, transaction func(func(*assistantRunLedger) (bool, error)) (assistantRunLedger, error), fn func(*assistantRunRecord) (bool, error)) error {
	stale := false
	ledger, err := transaction(func(ledger *assistantRunLedger) (bool, error) {
		run, err := assistantRunBound(ledger, w.binding, w.id)
		if err != nil {
			return false, err
		}
		if expected != nil && run.Revision != *expected {
			stale = true
			return false, errAssistantRunConflict
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
		if stale {
			return err
		}
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
func (w *assistantRunWriter) changeDialectDraft(ctx context.Context, owner *assistantDialectDraftPutOwner, stage string, expected assistantDialectDraftIntent, fn func(*assistantRunRecord) (bool, error)) (err error) {
	if !owner.original() || owner.writer != w {
		return errAssistantRunScope
	}
	g, err := owner.acquireReservationGuard(ctx, stage, expected)
	if err != nil {
		return err
	} // No writer failure latch without its mutex.
	defer func() {
		if err == nil {
			err = g.Check(ctx)
		}
		// A Close failure is uncertain and must latch while the writer is held.
		// Close releases that mutex, so perform the final check and data/private
		// cleanup before releasing the host holds below.
		err = errors.Join(err, g.closeData())
		if err != nil {
			w.fail(err)
		}
		err = errors.Join(err, g.Close())
	}()
	var saved assistantRunLedger
	err = w.changeLocked(ctx, false, nil, func(change func(*assistantRunLedger) (bool, error)) (assistantRunLedger, error) {
		var transactionErr error
		saved, transactionErr = w.lease.store.transactionDialectDraftLocked(ctx, g, change)
		return saved, transactionErr
	}, fn)
	if err == nil {
		err = g.adoptSaved(saved)
	}
	return err
}

func (w *assistantRunWriter) fail(err error) error {
	if w.failure == nil {
		w.failure = err
	}
	return err
}

func (w *assistantRunWriter) Checkpoint(ctx context.Context, state contract.Checkpoint, preview ...[]assistantRunPreview) error {
	return w.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		cp, err := readAssistantRunCheckpoint(state, run.Binding)
		if err != nil {
			return false, err
		}
		if sameAssistantRunCheckpoint(cp, run.Checkpoint) {
			return setAssistantRunPreview(run, preview), nil
		}
		if run.Observation != nil || assistantCanonicalActive(*run) {
			return false, errAssistantRunConflict
		}
		if assistantRunTerminal(run.State) {
			return false, errAssistantRunDispatch
		}
		if run.Checkpoint != nil && (cp.ID != run.Checkpoint.ID || cp.Used < run.Checkpoint.Used) {
			return false, errAssistantRunConflict
		}
		setAssistantRunPreview(run, preview)
		run.Checkpoint = cp
		if run.State == "admitted" {
			run.State = "running"
		}
		return true, nil
	})
}
func (w *assistantRunWriter) Event(ctx context.Context, id string, event contract.RunEvent, preview ...[]assistantRunPreview) error {
	return w.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		changed, err := appendAssistantRunEvent(run, id, event)
		if changed && err == nil {
			setAssistantRunPreview(run, preview)
		}
		return changed, err
	})
}
func appendAssistantRunEvent(run *assistantRunRecord, id string, event contract.RunEvent) (bool, error) {
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
}
func (w *assistantRunWriter) Intent(ctx context.Context, operation assistantRunOperation) error {
	return w.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		return appendAssistantRunIntent(run, operation)
	})
}
func appendAssistantRunIntent(run *assistantRunRecord, operation assistantRunOperation) (bool, error) {
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
		case "awaiting-input":
			if assistantRunPlanReference(*run) != nil || assistantRunApprovalPause(*run) && run.Checkpoint.Status == "paused" {
				expected = "paused"
			} else if question, err := pendingAssistantRunQuestion(*run); err == nil && question != nil {
				expected = "complete"
			}
		case "paused-budget":
			if assistantRunApprovalPause(*run) {
				return false, errAssistantRunConflict
			}
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
		if state != "running" {
			if err := finishAssistantCanonicalEvents(run); err != nil {
				return false, err
			}
		}
		return true, nil
	})
}

// Stop closes even an admission whose initial checkpoint could not be written.
// It never erases observed results or changes a terminal run into fresh work.
func stopAssistantRun(run *assistantRunRecord) {
	if assistantRunTerminal(run.State) {
		return
	}
	run.State = "stopped"
	if run.Observation != nil && assistantCanonicalActive(*run) && (run.Checkpoint.Status == "running" || run.Checkpoint.Status == "paused") {
		// No original-writer acknowledgement exists. Preserve the head and say
		// interrupted, rather than manufacture a canonical stopped checkpoint.
		run.State = "interrupted"
	}
	if run.Checkpoint != nil && !assistantCanonicalActive(*run) {
		run.Checkpoint.Status = "stopped"
		for i := range run.Checkpoint.Operations {
			if run.Checkpoint.Operations[i].Status == "running" {
				run.Checkpoint.Operations[i].Status = "interrupted"
			}
		}
	}
	for i := range run.Operations {
		if run.Operations[i].State == "dispatched" {
			run.Operations[i].State = "uncertain"
		}
	}
}
func (w *assistantRunWriter) Stop(ctx context.Context) error {
	return w.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		if assistantRunTerminal(run.State) {
			return false, nil
		}
		stopAssistantRun(run)
		if err := finishAssistantCanonicalEvents(run); err != nil {
			return false, err
		}
		return true, nil
	})
}

// Allocate and append under the same persisted-run transaction. No factory or
// transport-local counter can reuse an identity after an explicit continuation.
func nextAssistantRunIdentity(run *assistantRunRecord, kind string) string {
	for n := uint64(len(run.Operations) + len(run.Events) + 1); ; n++ {
		id := assistantExecutionID(kind, n)
		occupied := false
		for _, op := range run.Operations {
			if op.ID == id || op.CallID == id {
				occupied = true
				break
			}
		}
		for _, event := range run.Events {
			if event.ID == id {
				occupied = true
				break
			}
		}
		if !occupied {
			return id
		}
	}
}
func (w *assistantRunWriter) boundIntent(ctx context.Context, kind, name string, intent func(string) ([]byte, error)) (operation assistantRunOperation, err error) {
	err = w.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		id := nextAssistantRunIdentity(run, kind)
		raw, err := intent(id)
		if err != nil {
			return false, err
		}
		operation = assistantRunOperation{ID: id, CallID: id, Kind: kind, Name: name, Intent: raw}
		return appendAssistantRunIntent(run, operation)
	})
	return operation, err
}
func (w *assistantRunWriter) boundEvent(ctx context.Context, event contract.RunEvent, preview ...[]assistantRunPreview) error {
	return w.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		changed, err := appendAssistantRunEvent(run, nextAssistantRunIdentity(run, "event"), event)
		if changed && err == nil {
			setAssistantRunPreview(run, preview)
		}
		return changed, err
	})
}

func setAssistantRunPreview(run *assistantRunRecord, preview [][]assistantRunPreview) bool {
	if len(preview) == 0 || reflect.DeepEqual(run.DisplayPreview, preview[0]) {
		return false
	}
	run.DisplayPreview = preview[0]
	return true
}
