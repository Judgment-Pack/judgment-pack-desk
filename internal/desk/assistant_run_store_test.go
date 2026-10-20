package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func assistantRunFixture(t *testing.T) (assistantRunStore, assistantRunBinding, contract.Checkpoint) {
	t.Helper()
	if !custodyChecked {
		t.Skip("private store refuses custody on this platform")
	}
	base := t.TempDir()
	config := filepath.Join(base, "config")
	project := filepath.Join(base, "project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	private := openAssistantStore(config)
	if !private.usable() {
		t.Fatal(private.problem)
	}
	t.Cleanup(func() { private.Close() })
	server := &Server{cfg: Config{DeskConfigDir: config}, configDir: config, projectDir: project, assistant: private}
	store := server.assistantRuns()
	run, err := runcontext.ContextualSession(context.Background(), runcontext.Session{Prompt: "Review the exact saved input", Model: &runcontext.ModelSession{Family: "gemini", Model: "fixture"}, Context: &runcontext.Input{CurrentRequest: &runcontext.CurrentRequest{Message: "Review", Input: "exact original"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer run.Context.Close()
	cp, err := run.Context.Snapshot("")
	if err != nil {
		t.Fatal(err)
	}
	hash := func(s string) string { return digestOf([]byte(s)) }
	binding := assistantRunBinding{Principal: hash("principal"), Session: hash("session"), Policy: hash("policy"), Project: store.project, Owner: assistantRunOwner{Kind: "chat", ID: "chat-one", Revision: hash("owner-v1")}, RequestID: "user-message-1", RequestDigest: hash("exact original"), SourceDigests: []string{hash("source")}, ConnectionID: "legacy-api", ConnectionRevision: hash("connection-v1"), ProfileRevision: hash("profile-v1"), Model: "fixture", Phase: cp.Phase, Unit: cp.Unit, ExecutionKey: cp.RequestKey}
	return store, binding, cp
}
func assistantRunAdmitted(t *testing.T) (assistantRunStore, assistantRunBinding, contract.Checkpoint, assistantRunRecord, *assistantRunLease, *assistantRunWriter) {
	t.Helper()
	store, binding, cp := assistantRunFixture(t)
	lease, err := store.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lease.Close() })
	record, created, err := lease.Admit(context.Background(), binding, "admission", digestOf([]byte("body")), func() error { return nil })
	if err != nil || !created {
		t.Fatalf("admit %v %v", created, err)
	}
	writer, err := lease.Writer(context.Background(), binding, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.Checkpoint(context.Background(), cp); err != nil {
		t.Fatal(err)
	}
	return store, binding, cp, record, lease, writer
}
func assistantRunOp(id string) assistantRunOperation {
	return assistantRunOperation{ID: id, CallID: "call-" + id, Kind: "runtime", Name: "read_exact", Intent: []byte("{\"missing\":null, \"fact\":false}")}
}
func assistantRunMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestAssistantRunConcurrentAdmissionAndLostResponse(t *testing.T) {
	store, binding, _ := assistantRunFixture(t)
	lease, err := store.Acquire(context.Background())
	assistantRunMust(t, err)
	defer lease.Close()
	ctx := context.Background()
	var checks, created atomic.Int32
	ids := make(chan string, 24)
	errs := make(chan error, 24)
	var group sync.WaitGroup
	for range 24 {
		group.Add(1)
		go func() {
			defer group.Done()
			record, newRun, err := lease.Admit(ctx, binding, "stable-key", digestOf([]byte("body")), func() error { checks.Add(1); return nil })
			if newRun {
				created.Add(1)
			}
			ids <- record.ID
			errs <- err
		}()
	}
	group.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		assistantRunMust(t, err)
	}
	id := ""
	for got := range ids {
		if id == "" {
			id = got
		}
		if got != id {
			t.Fatal("duplicate run")
		}
	}
	if checks.Load() != 1 || created.Load() != 1 {
		t.Fatalf("fresh CAS/admissions %d/%d", checks.Load(), created.Load())
	}
	held, newRun, err := lease.Admit(ctx, binding, "stable-key", digestOf([]byte("body")), func() error {
		t.Error("stale owner CAS must not run for lost-response retry")
		return errAssistantRunConflict
	})
	assistantRunMust(t, err)
	if newRun || held.ID != id {
		t.Fatal("retry did not return saved admission")
	}
	for _, mutate := range []func(*assistantRunBinding){func(b *assistantRunBinding) { b.Session = digestOf([]byte("other")) }, func(b *assistantRunBinding) { b.Owner.Revision = digestOf([]byte("v2")) }, func(b *assistantRunBinding) { b.RequestDigest = digestOf([]byte("changed")) }, func(b *assistantRunBinding) { b.Model = "different" }, func(b *assistantRunBinding) { b.ConnectionRevision = digestOf([]byte("connection-v2")) }} {
		changed := binding
		mutate(&changed)
		if _, _, err = lease.Admit(ctx, changed, "stable-key", digestOf([]byte("body")), func() error { t.Error("mismatch reached CAS"); return nil }); !errors.Is(err, errAssistantRunConflict) {
			t.Fatalf("binding mismatch accepted: %v", err)
		}
		if _, err = store.Read(ctx, changed, id); !errors.Is(err, errAssistantRunScope) {
			t.Fatalf("changed reader accepted: %v", err)
		}
	}
	if _, _, err = lease.Admit(ctx, binding, "stable-key", digestOf([]byte("different body")), func() error { return nil }); !errors.Is(err, errAssistantRunConflict) {
		t.Fatal("body conflict accepted")
	}
	changed := binding
	changed.Principal = digestOf([]byte("another principal"))
	if _, err = store.Read(ctx, changed, id); !errors.Is(err, errAssistantRunScope) {
		t.Fatal("cross-principal read")
	}
	changed = binding
	changed.Project = digestOf([]byte("another project"))
	if _, err = store.Read(ctx, changed, id); !errors.Is(err, errAssistantRunScope) {
		t.Fatal("cross-project read")
	}
	if _, _, err = lease.Admit(ctx, binding, "new-key", digestOf([]byte("body")), func() error { t.Error("active owner must refuse before CAS"); return nil }); !errors.Is(err, errAssistantRunConflict) {
		t.Fatal("two active owner runs")
	}
}

func TestAssistantRunExactResultsAndContradiction(t *testing.T) {
	store, binding, _, record, lease, writer := assistantRunAdmitted(t)
	ctx := context.Background()
	op := assistantRunOp("one")
	assistantRunMust(t, writer.Intent(ctx, op))
	op.Intent[0] = '!'
	assistantRunMust(t, writer.Dispatch(ctx, "one"))
	result := []byte(" {\"false\":false,\"null\":null,\"decimal\":\"24.00\"} \n")
	original := bytes.Clone(result)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	assistantRunMust(t, writer.Result(canceled, "one", "completed", result))
	result[0] = '!'
	assistantRunMust(t, writer.Result(ctx, "one", "completed", original))
	reloaded, err := store.server.assistantRuns().Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	if !bytes.Equal(reloaded.Operations[0].Result, original) || !bytes.Equal(reloaded.Operations[0].Intent, assistantRunOp("one").Intent) {
		t.Fatal("exact original bytes lost or aliased")
	}
	// A stale writer may acknowledge the same observed completion, but cannot
	// overwrite it. No provider or Runtime callback exists in this ledger API.
	stale, err := lease.Writer(ctx, binding, record.ID)
	assistantRunMust(t, err)
	assistantRunMust(t, stale.Result(ctx, "one", "completed", original))
	if err = stale.Result(ctx, "one", "completed", []byte("different")); !errors.Is(err, errAssistantRunConflict) {
		t.Fatalf("contradiction accepted %v", err)
	}
	if err = writer.Dispatch(ctx, "one"); !errors.Is(err, errAssistantRunDispatch) {
		t.Fatalf("replay accepted %v", err)
	}
	// Empty is an exact result, distinct from no result, including after reload.
	writer, err = lease.Writer(ctx, binding, record.ID)
	assistantRunMust(t, err)
	assistantRunMust(t, writer.Intent(ctx, assistantRunOp("empty")))
	assistantRunMust(t, writer.Result(ctx, "empty", "refused", []byte{}))
	reloaded, err = store.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	if reloaded.Operations[1].Result == nil || len(reloaded.Operations[1].Result) != 0 {
		t.Fatal("empty result became missing")
	}
}

func TestAssistantRunPersistenceFailureClosesDispatch(t *testing.T) {
	for _, sink := range []string{"intent", "checkpoint", "dispatch"} {
		t.Run(sink, func(t *testing.T) {
			store, binding, cp, record, lease, writer := assistantRunAdmitted(t)
			ctx := context.Background()
			if sink == "dispatch" {
				assistantRunMust(t, writer.Intent(ctx, assistantRunOp("one")))
			}
			reads := 0
			injected := errors.New("read-back failed after possible commit")
			testHookPrivateRead = func(name string) error {
				if name == store.name {
					reads++
					if reads == 2 {
						return injected
					}
				}
				return nil
			}
			var err error
			switch sink {
			case "intent":
				err = writer.Intent(ctx, assistantRunOp("one"))
			case "checkpoint":
				cp.Used++
				err = writer.Checkpoint(ctx, cp)
			case "dispatch":
				err = writer.Dispatch(ctx, "one")
			}
			testHookPrivateRead = nil
			if !errors.Is(err, injected) || !errors.Is(writer.Err(), injected) {
				t.Fatalf("failed write not latched: %v / %v", err, writer.Err())
			}
			if err = writer.Dispatch(ctx, "one"); !errors.Is(err, injected) {
				t.Fatalf("next dispatch escaped latch: %v", err)
			}
			assistantRunMust(t, lease.Close())
			next, err := store.Acquire(ctx)
			assistantRunMust(t, err)
			defer next.Close()
			assistantRunMust(t, next.Recover(ctx))
			held, err := store.Read(ctx, binding, record.ID)
			assistantRunMust(t, err)
			if sink == "dispatch" && held.Operations[0].State != "uncertain" {
				t.Fatalf("uncertain durable dispatch lost: %+v", held.Operations)
			}
		})
	}
}

func TestAssistantRunRecoveryRetainsCompletedAndNeverReplays(t *testing.T) {
	store, binding, cp, record, lease, writer := assistantRunAdmitted(t)
	ctx := context.Background()
	cp.Operations = append(cp.Operations, contract.CheckpointOperation{ID: "3f71c7b0-7df4-4eb6-96f3-f3a1390dc3a0", Name: "read_exact", Args: json.RawMessage(`{"fact":false}`), Status: "running"})
	assistantRunMust(t, writer.Checkpoint(ctx, cp))
	if other, err := store.Acquire(ctx); err == nil {
		other.Close()
		t.Fatal("second executor acquired live project")
	}
	assistantRunMust(t, writer.Intent(ctx, assistantRunOp("complete")))
	assistantRunMust(t, writer.Dispatch(ctx, "complete"))
	assistantRunMust(t, writer.Result(ctx, "complete", "completed", []byte("exact completed result")))
	assistantRunMust(t, writer.Intent(ctx, assistantRunOp("unknown")))
	assistantRunMust(t, writer.Dispatch(ctx, "unknown"))
	assistantRunMust(t, writer.Intent(ctx, assistantRunOp("not-started")))
	assistantRunMust(t, lease.Close())
	next, err := store.Acquire(ctx)
	assistantRunMust(t, err)
	defer next.Close()
	assistantRunMust(t, next.Recover(ctx))
	held, err := store.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	if held.State != "interrupted" || held.Checkpoint.Status != "stopped" || held.Checkpoint.Operations[0].Status != "interrupted" || held.Operations[0].State != "completed" || string(held.Operations[0].Result) != "exact completed result" || held.Operations[1].State != "uncertain" || held.Operations[2].State != "not-started" {
		t.Fatalf("bad recovery: %+v", held)
	}
	revision := held.Revision
	assistantRunMust(t, next.Recover(ctx))
	again, err := store.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	if again.Revision != revision {
		t.Fatal("recovery rewrote terminal run")
	}
	fresh, err := next.Writer(ctx, binding, record.ID)
	assistantRunMust(t, err)
	if err = fresh.Dispatch(ctx, "unknown"); !errors.Is(err, errAssistantRunDispatch) {
		t.Fatal("uncertain operation replayed")
	}
	// A known late outcome can be retained after loss of the old executor lease.
	assistantRunMust(t, writer.Result(ctx, "unknown", "completed", []byte("late exact outcome")))
	held, err = store.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	if string(held.Operations[1].Result) != "late exact outcome" || held.State != "interrupted" {
		t.Fatal("late outcome lost or restarted run")
	}
}

func TestAssistantRunCanonicalCheckpointAndBoundedCustody(t *testing.T) {
	t.Run("canonical binding", func(t *testing.T) {
		_, _, cp, _, _, writer := assistantRunAdmitted(t)
		cp.RequestKey = digestOf([]byte("different request"))
		if err := writer.Checkpoint(context.Background(), cp); !errors.Is(err, errAssistantRunScope) {
			t.Fatalf("wrong request %v", err)
		}
		if writer.Err() == nil {
			t.Fatal("checkpoint failure did not close dispatch")
		}
	})
	t.Run("canonical schema", func(t *testing.T) {
		_, _, cp, _, _, writer := assistantRunAdmitted(t)
		cp.Status = "uncertain"
		if err := writer.Checkpoint(context.Background(), cp); err == nil {
			t.Fatal("host state admitted into canonical checkpoint")
		}
	})
	t.Run("checkpoint cap", func(t *testing.T) {
		_, _, cp, _, _, writer := assistantRunAdmitted(t)
		cp.Explanation = strings.Repeat("x", runcontext.MaxCheckpointBytes)
		if err := writer.Checkpoint(context.Background(), cp); err == nil {
			t.Fatal("oversize canonical checkpoint accepted")
		}
	})

	t.Run("capacity", func(t *testing.T) {
		store, binding, _, record, _, writer := assistantRunAdmitted(t)
		op := assistantRunOp("large")
		op.Intent = bytes.Repeat([]byte("x"), assistantRunRecordLimit)
		if err := writer.Intent(context.Background(), op); err == nil {
			t.Fatal("oversize encoded record accepted")
		}
		held, err := store.Read(context.Background(), binding, record.ID)
		assistantRunMust(t, err)
		if len(held.Operations) != 0 {
			t.Fatal("oversize record replaced existing work")
		}
	})
	for _, kind := range []string{"symlink", "permissions", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			store, binding, _, record, _, _ := assistantRunAdmitted(t)
			root, err := store.server.openChatData()
			assistantRunMust(t, err)
			name := filepath.Join(root.location.Path, store.name)
			root.root.Close()
			switch kind {
			case "symlink":
				assistantRunMust(t, os.Remove(name))
				target := filepath.Join(t.TempDir(), "outside")
				assistantRunMust(t, os.WriteFile(target, []byte("{}"), 0600))
				assistantRunMust(t, os.Symlink(target, name))
			case "permissions":
				assistantRunMust(t, os.Chmod(name, 0644))
			case "oversize":
				assistantRunMust(t, os.WriteFile(name, []byte(strings.Repeat(" ", assistantRunRecordLimit+1)), 0600))
			}
			if _, err = store.Read(context.Background(), binding, record.ID); err == nil {
				t.Fatal("unsafe record accepted")
			}
		})
	}
}

func TestAssistantRunSemanticEventsAndStaleWriter(t *testing.T) {
	store, binding, _, record, lease, writer := assistantRunAdmitted(t)
	ctx := context.Background()
	stale, err := lease.Writer(ctx, binding, record.ID)
	assistantRunMust(t, err)
	message := &contract.RunEventMessage{Type: "message", Text: "Final semantic answer"}
	assistantRunMust(t, writer.Event(ctx, "message-one", contract.RunEvent{Message: message}))
	message.Text = "changed caller data"
	assistantRunMust(t, writer.Event(ctx, "message-one", contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: "Final semantic answer"}}))
	if err = stale.Intent(ctx, assistantRunOp("stale")); !errors.Is(err, errAssistantRunConflict) {
		t.Fatalf("stale writer accepted %v", err)
	}
	held, err := store.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	if len(held.Operations) != 0 || len(held.Events) != 1 || held.Events[0].Event.Message.Text != "Final semantic answer" {
		t.Fatal("stale writer or caller alias changed saved state")
	}
	if err = writer.Event(ctx, "message-one", contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: "contradictory"}}); !errors.Is(err, errAssistantRunConflict) {
		t.Fatalf("event contradiction accepted %v", err)
	}
	for _, event := range []contract.RunEvent{{MessageProgress: &contract.RunEventMessageProgress{Type: "message_progress", Text: "whole prefix"}}, {Message: &contract.RunEventMessage{Type: "end", Text: "wrong union"}}, {Message: &contract.RunEventMessage{Type: "message"}, End: &contract.RunEventEnd{Type: "end"}}} {
		clean, err := lease.Writer(ctx, binding, record.ID)
		assistantRunMust(t, err)
		if err = clean.Event(ctx, "invalid", event); err == nil {
			t.Fatal("invalid/progress event was durable")
		}
		if err = clean.Dispatch(ctx, "missing"); err == nil || clean.Err() == nil {
			t.Fatal("event failure did not close admission")
		}
	}
	held, err = store.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	if len(held.Events) != 1 {
		t.Fatal("rejected event corrupted durable stream")
	}
}

func TestAssistantRunTerminalRetentionAndRecoveryRevision(t *testing.T) {
	store, binding, cp, record, lease, writer := assistantRunAdmitted(t)
	ctx := context.Background()
	// A paused client owner already has a stopped canonical checkpoint. Recovery
	// of a different active run must not change this record's revision.
	other := binding
	other.Owner.ID = "second-chat"
	second, _, err := lease.Admit(ctx, other, "second-admission", digestOf([]byte("second body")), func() error { return nil })
	assistantRunMust(t, err)
	secondWriter, err := lease.Writer(ctx, other, second.ID)
	assistantRunMust(t, err)
	stopped := cp
	stopped.Status = "stopped"
	assistantRunMust(t, secondWriter.Checkpoint(ctx, stopped))
	assistantRunMust(t, secondWriter.State(ctx, "awaiting-client"))
	before, err := store.Read(ctx, other, second.ID)
	assistantRunMust(t, err)
	assistantRunMust(t, lease.Recover(ctx))
	after, err := store.Read(ctx, other, second.ID)
	assistantRunMust(t, err)
	if after.Revision != before.Revision || after.State != "awaiting-client" {
		t.Fatal("recovery rewrote unrelated paused owner")
	}
	held, err := store.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	if held.State != "interrupted" {
		t.Fatal("active owner not interrupted")
	}
	if err = writer.Intent(ctx, assistantRunOp("stale-after-recover")); err == nil {
		t.Fatal("old executor writer dispatched after recovery")
	}
	// Complete a fresh owner, retain it, and admit the next request without
	// deleting the old run or its idempotency key.
	terminalBinding := binding
	terminalBinding.Owner.ID = "terminal-chat"
	terminal, _, err := lease.Admit(ctx, terminalBinding, "terminal", digestOf([]byte("terminal body")), func() error { return nil })
	assistantRunMust(t, err)
	terminalWriter, err := lease.Writer(ctx, terminalBinding, terminal.ID)
	assistantRunMust(t, err)
	complete := cp
	complete.Status = "complete"
	assistantRunMust(t, terminalWriter.Checkpoint(ctx, complete))
	assistantRunMust(t, terminalWriter.State(ctx, "complete"))
	_, created, err := lease.Admit(ctx, terminalBinding, "next", digestOf([]byte("next body")), func() error { return nil })
	assistantRunMust(t, err)
	if !created {
		t.Fatal("terminal owner blocked new request")
	}
	retained, err := store.Read(ctx, terminalBinding, terminal.ID)
	assistantRunMust(t, err)
	if retained.State != "complete" {
		t.Fatal("terminal run lost")
	}
	retry, created, err := lease.Admit(ctx, terminalBinding, "terminal", digestOf([]byte("terminal body")), func() error { t.Error("retained retry reached fresh CAS"); return nil })
	assistantRunMust(t, err)
	if created || retry.ID != terminal.ID {
		t.Fatal("terminal admission lost")
	}
}
