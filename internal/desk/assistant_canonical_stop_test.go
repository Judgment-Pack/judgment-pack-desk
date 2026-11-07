package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/delegate"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

func TestAssistantCanonicalStopOriginalWriter(t *testing.T) {
	for _, early := range []bool{false, true} {
		t.Run(map[bool]string{false: "installed", true: "late-install"}[early], func(t *testing.T) {
			store, binding, _, writer, record := canonicalActivationAdmitted(t)
			e := &assistantRunExecution{record: record, writer: writer, ctx: context.Background()}
			input := &runcontext.Input{Execution: record.Checkpoint}
			assistantRunMust(t, e.bindCanonicalInput(t.Context(), input))
			if early {
				e.closeCanonicalDispatch()
			}
			before, err := store.Read(t.Context(), binding, record.ID)
			assistantRunMust(t, err)
			var original *runcontext.RunBinding
			savedByAdapter := false
			wrapped := e.canonicalAdapter(adapter.Func(func(ctx context.Context, run *adapter.Run) error {
				original = run.Context.Persistence
				err := original.Writer.Save(ctx, *original, *record.Checkpoint)
				savedByAdapter = err == nil
				return err
			}), nil)
			runErr := wrapped.Run(t.Context(), &adapter.Run{Context: input})
			if early {
				if runErr != failure.Cancelled || savedByAdapter {
					t.Fatal("closed late-installed writer admitted Save", runErr)
				}
				after, err := store.Read(t.Context(), binding, record.ID)
				assistantRunMust(t, err)
				if after.Revision != before.Revision || !reflect.DeepEqual(after.Canonical, before.Canonical) {
					t.Fatal("closed late-installed writer persisted or dispatched")
				}
			} else {
				assistantRunMust(t, runErr)
				if !savedByAdapter {
					t.Fatal("installed writer did not save")
				}
			}
			if e.canonicalBinding != original {
				t.Fatal("replaced original binding")
			}
			e.closeCanonicalDispatch()
			assistantRunMust(t, e.finalizeCanonicalStop())
			// The canonical acknowledgement precedes the outer terminal envelope.
			saved, err := store.Read(t.Context(), binding, record.ID)
			assistantRunMust(t, err)
			if saved.Checkpoint.Status != "stopped" || assistantRunTerminal(saved.State) {
				t.Fatal("missing ordered stopped acknowledgement")
			}
			assistantRunMust(t, writer.Stop(t.Context()))
			saved, err = store.Read(t.Context(), binding, record.ID)
			assistantRunMust(t, err)
			if saved.State != "stopped" {
				t.Fatal("outer run not stopped")
			}
			assistantRunMust(t, validateAssistantCanonicalProjection(saved))
			count := len(saved.Canonical.Commits)
			assistantRunMust(t, e.finalizeCanonicalStop())
			saved, err = store.Read(t.Context(), binding, record.ID)
			assistantRunMust(t, err)
			if len(saved.Canonical.Commits) != count {
				t.Fatal("duplicate stop committed")
			}
		})
	}
}

func TestAssistantCanonicalStopStaleActionDoesNotPoisonOriginal(t *testing.T) {
	store, binding, _, writer, record := canonicalActivationAdmitted(t)
	stale := record.Revision - 1
	called := false
	err := writer.changeAt(t.Context(), false, &stale, func(*assistantRunRecord) (bool, error) { called = true; return true, nil })
	if !errors.Is(err, errAssistantRunConflict) || called || writer.Err() != nil {
		t.Fatal("stale action mutated or poisoned writer", err)
	}
	assistantRunMust(t, writer.changeAt(t.Context(), false, &record.Revision, func(run *assistantRunRecord) (bool, error) {
		run.Actions = append(run.Actions, assistantRunActionReceipt{ID: "stop", Kind: "cancel", Digest: digestOf([]byte("exact-action"))})
		return true, nil
	}))
	saved, err := store.Read(t.Context(), binding, record.ID)
	assistantRunMust(t, err)
	if writer.revision != saved.Revision || len(saved.Actions) != 1 || assistantRunTerminal(saved.State) {
		t.Fatal("original writer lost action revision")
	}
}

// Stop traverses the HTTP action, original live writers and executor-return
// owner after real durable claims; no replacement writer supplies the receipt.
func TestAssistantCanonicalStopActionRetainsResultsNoReplay(t *testing.T) {
	entered := make(chan *assistantRunExecution, 1)
	var toolCalls, modelCalls atomic.Int32
	exact := runcontext.ReadToolResult(runcontext.MustParseJSON(`{"content":[{"type":"text","text":"exact earlier result"}],"opaque":{"nested":[null,0,false]}}`))
	s, token, start, executor := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		input := &runcontext.Input{Execution: e.record.Checkpoint}
		if err := e.bindCanonicalInput(ctx, input); err != nil {
			return err
		}
		wrapped := e.canonicalAdapter(adapter.Func(func(ctx context.Context, run *adapter.Run) error {
			p := run.Context.Persistence
			if err := p.Writer.Save(ctx, *p, *e.record.Checkpoint); err != nil {
				return err
			}
			_, err := runcontext.InvokeDurable(ctx, p, "host", "source", "write", []byte(`{"value":1}`), false, func([]byte) (runcontext.ToolResult, error) { toolCalls.Add(1); return exact, nil }, nil, nil)
			if err != nil {
				return err
			}
			_, err = runcontext.InvokeDurableModel(ctx, p, "source", "fixture", []byte(`{"request":"exact"}`), func([]byte) (*http.Response, error) {
				modelCalls.Add(1)
				entered <- e
				<-ctx.Done()
				return nil, ctx.Err()
			}, func(*http.Response) error { t.Error("canceled model verified"); return nil })
			return err
		}), nil)
		return wrapped.Run(ctx, &adapter.Run{Context: input})
	})
	s.assistantRunExecutor = &assistantCanonicalProtocolExecutor{executor}
	initial := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	var e *assistantRunExecution
	select {
	case e = <-entered:
	case <-time.After(8 * time.Second):
		t.Fatal("model claim not reached")
	}
	before, err := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
	assistantRunMust(t, err)
	if len(before.Checkpoint.Durable.Dispatches) != 2 || before.Checkpoint.Durable.Dispatches[1].State != "claimed" {
		t.Fatal("missing physical claim")
	}
	completed := before.Checkpoint.Durable.Dispatches[0]
	path := "/api/assistant-runs/" + initial.ID + "/actions"
	stale := assistantRunAction{ID: "stale-stop", Kind: "cancel", Revision: before.Revision - 1}
	if response := policyCall(s, token, "POST", path, stale); response.Code != 409 {
		t.Fatal("stale Stop accepted", response.Code)
	}
	action := assistantRunAction{ID: "stop-original", Kind: "cancel", Revision: before.Revision}
	stopped := assistantProtocolView(t, policyCall(s, token, "POST", path, action), 200)
	if stopped.State != "stopped" || stopped.Checkpoint.Status != "stopped" {
		t.Fatal("Stop was not acknowledged")
	}
	after, err := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
	assistantRunMust(t, err)
	if !reflect.DeepEqual(after.Checkpoint.Durable.Dispatches[0], completed) || after.Checkpoint.Durable.Dispatches[1].State != "unknown" || len(after.Actions) != 1 || after.Actions[0].Digest != assistantRunActionDigest(action) || e.writer.revision != after.Revision {
		t.Fatal("original writer lost exact result, claim or action revision")
	}
	assistantRunMust(t, validateAssistantCanonicalProjection(after))
	if len(after.Canonical.Events) < 2 {
		t.Fatal("missing canonical terminal events")
	}
	var checkpoint, end runcontext.StoredRunEnvelope
	assistantRunMust(t, json.Unmarshal(after.Canonical.Events[len(after.Canonical.Events)-2].Bytes, &checkpoint))
	assistantRunMust(t, json.Unmarshal(after.Canonical.Events[len(after.Canonical.Events)-1].Bytes, &end))
	if checkpoint.Event.ExecutionCheckpoint == nil || checkpoint.Event.ExecutionCheckpoint.Checkpoint.Status != "stopped" || end.Event.End == nil {
		t.Fatal("canonical stopped checkpoint/end order lost")
	}
	duplicate := assistantProtocolView(t, policyCall(s, token, "POST", path, action), 200)
	if duplicate.Revision != stopped.Revision {
		t.Fatal("duplicate Stop committed")
	}
	changed := action
	changed.Revision++
	if response := policyCall(s, token, "POST", path, changed); response.Code != 409 {
		t.Fatal("changed Stop duplicate accepted", response.Code)
	}
	assistantRunMust(t, s.Close())
	restarted, err := New(s.cfg)
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = restarted.Close() })
	restarted.assistantRunExecutor = &assistantCanonicalProtocolExecutor{executor}
	current := policyTestSession(t, restarted)
	restored := assistantProtocolView(t, policyCall(restarted, current, "GET", "/api/assistant-runs/"+initial.ID, nil), 200)
	if response := policyCall(restarted, current, "POST", "/api/assistant-runs", start); response.Code == 201 {
		t.Fatal("restart readmitted stopped request")
	}
	if restored.ID != initial.ID || restored.State != "stopped" || toolCalls.Load() != 1 || modelCalls.Load() != 1 || executor.calls.Load() != 1 {
		t.Fatal("restart replayed stopped physical work")
	}
}

func TestAssistantCanonicalStopPreboundHostEvents(t *testing.T) {
	store, binding, _, writer, record := canonicalActivationAdmitted(t)
	e := &assistantRunExecution{record: record, writer: writer, ctx: context.Background(), service: &assistantRunService{subscribers: map[uint64]assistantRunSubscriber{}}}
	first := &runcontext.Input{}
	assistantRunMust(t, e.bindCanonicalInput(t.Context(), first))
	original := first.Persistence
	assistantRunMust(t, e.publishCanonicalHostEvent(t.Context(), contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: "Runtime preflight"}}))
	next := &runcontext.Input{}
	assistantRunMust(t, e.bindCanonicalInput(t.Context(), next))
	if next.Persistence != original {
		t.Fatal("preflight replaced original writer")
	}
	wrong := *original
	if err := e.bindCanonicalInput(t.Context(), &runcontext.Input{Persistence: &wrong}); err == nil {
		t.Fatal("accepted foreign binding")
	}
	called := false
	wrapped := e.canonicalAdapter(adapter.Func(func(context.Context, *adapter.Run) error { called = true; return nil }), nil)
	if err := wrapped.Run(t.Context(), &adapter.Run{Context: &runcontext.Input{Persistence: &wrong}}); err == nil || called {
		t.Fatal("foreign root entered adapter")
	}
	e.closeCanonicalDispatch()
	assistantRunMust(t, e.finalizeCanonicalStop())
	before, err := store.Read(t.Context(), binding, record.ID)
	assistantRunMust(t, err)
	if err := original.Writer.Save(t.Context(), *original, *record.Checkpoint); err == nil {
		t.Fatal("late checkpoint reopened stopped writer")
	}
	if _, err := original.Writer.Event(t.Context(), *original, contract.RunEvent{End: &contract.RunEventEnd{Type: "end"}}); err == nil {
		t.Fatal("late event reopened stopped writer")
	}
	after, err := store.Read(t.Context(), binding, record.ID)
	assistantRunMust(t, err)
	if before.Revision != after.Revision || !reflect.DeepEqual(before.Canonical, after.Canonical) {
		t.Fatal("late commit changed stopped acknowledgement")
	}
	assistantRunMust(t, validateAssistantCanonicalProjection(after))
}

func TestAssistantCanonicalStopStorageFailureStaysLatched(t *testing.T) {
	store, binding, _, writer, record := canonicalActivationAdmitted(t)
	e := &assistantRunExecution{record: record, writer: writer, ctx: context.Background()}
	input := &runcontext.Input{}
	assistantRunMust(t, e.bindCanonicalInput(t.Context(), input))
	fault := errors.New("handwritten canonical storage fault")
	testHookPrivateRead = func(string) error { return fault }
	defer func() { testHookPrivateRead = nil }()
	err := input.Persistence.Writer.Save(t.Context(), *input.Persistence, *record.Checkpoint)
	if err == nil || !input.Persistence.Writer.Failed() {
		t.Fatal("storage failure did not latch")
	}
	testHookPrivateRead = nil
	e.closeCanonicalDispatch()
	if err := e.finalizeCanonicalStop(); err == nil {
		t.Fatal("failed original writer acknowledged Stop")
	}
	saved, err := store.Read(t.Context(), binding, record.ID)
	assistantRunMust(t, err)
	if saved.Checkpoint.Status == "stopped" || assistantRunTerminal(saved.State) {
		t.Fatal("failed original writer fabricated terminal acknowledgement")
	}
	if !input.Persistence.Writer.Failed() {
		t.Fatal("finalization cleared storage failure")
	}
}

type assistantStopBlockingStore struct{ runcontext.RunStore }

func (s assistantStopBlockingStore) Commit(ctx context.Context, _ runcontext.RunCommit) (runcontext.RunReceipt, error) {
	<-ctx.Done()
	return runcontext.RunReceipt{}, ctx.Err()
}
func TestAssistantCanonicalStopFinalizationBound(t *testing.T) {
	store, binding, _, writer, record := canonicalActivationAdmitted(t)
	ctx, cancel := context.WithCancel(context.Background())
	e := &assistantRunExecution{record: record, writer: writer, ctx: ctx}
	input := &runcontext.Input{}
	assistantRunMust(t, e.bindCanonicalInput(t.Context(), input))
	original := input.Persistence
	original.Writer.Store = assistantStopBlockingStore{original.Writer.Store}
	e.closeCanonicalDispatch()
	cancel()
	started := time.Now()
	err := e.finalizeCanonicalStop()
	elapsed := time.Since(started)
	if err == nil || elapsed < 4*time.Second || elapsed > 6*time.Second || !original.Writer.Failed() {
		t.Fatal("uncanceled bounded finalization lost failure", elapsed, err)
	}
	if err := e.finalizeCanonicalStop(); err == nil || time.Since(started) > 7*time.Second {
		t.Fatal("bounded failure was retried or cleared", err)
	}
	saved, err := store.Read(t.Context(), binding, record.ID)
	assistantRunMust(t, err)
	if saved.Checkpoint.Status == "stopped" || assistantRunTerminal(saved.State) {
		t.Fatal("timed out persistence fabricated Stop acknowledgement")
	}
}

// The full delegation coordinator has its own registry-dependent regression.
// This check isolates released preparation, binding ownership and decoration.
func TestAssistantCanonicalStopSharedWriterPreparation(t *testing.T) {
	store, binding, _, writer, record := canonicalActivationAdmitted(t)
	e := &assistantRunExecution{record: record, writer: writer, ctx: context.Background()}
	input := &runcontext.Input{}
	assistantRunMust(t, e.bindCanonicalInput(t.Context(), input))
	original := input.Persistence
	decorations, roots, children := 0, 0, 0
	// The coordinator installs its decorator before invoking the host adapter.
	original.Writer.Decorate = func(cp contract.Checkpoint) (contract.Checkpoint, error) { decorations++; return cp, nil }
	wrapped := delegate.With(e.canonicalAdapter(adapter.Func(func(ctx context.Context, run *adapter.Run) error {
		p := run.Context.Persistence
		if p.Writer != original.Writer {
			t.Fatal("delegate preparation replaced writer")
		}
		if p.Root {
			roots++
			if p != original {
				t.Fatal("delegate preparation replaced root binding")
			}
			return p.Writer.Save(ctx, *p, *p.Writer.Checkpoint())
		}
		children++
		if p == original || p.Scope.Task != "fixture-child" || p.Scope.Execution != "fixture-child" {
			t.Fatal("child took root authority")
		}
		return nil
	}), nil), delegate.Options{})
	for range 2 {
		assistantRunMust(t, wrapped.Run(t.Context(), &adapter.Run{Context: input}))
	}
	child := &runcontext.Input{}
	runcontext.ChildPersistence(input, child, "fixture-child")
	assistantRunMust(t, wrapped.Run(t.Context(), &adapter.Run{Context: child}))
	if decorations != 2 || roots != 2 || children != 1 || e.canonicalBinding != original {
		t.Fatal("decoration duplicated or binding ownership changed", decorations, roots, children)
	}
	after, err := store.Read(t.Context(), binding, record.ID)
	assistantRunMust(t, err)
	assistantRunMust(t, validateAssistantCanonicalProjection(after))
}
