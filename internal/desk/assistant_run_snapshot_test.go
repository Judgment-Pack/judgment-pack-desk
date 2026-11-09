package desk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Pause the final context check after decoding and whole-ledger validation. A
// concurrent writer must complete while this read still owns its snapshot.
type assistantSnapshotContext struct {
	context.Context
	armed   atomic.Bool
	reached chan struct{}
	release chan struct{}
	cancel  bool
}

func (c *assistantSnapshotContext) Err() error {
	if c.armed.CompareAndSwap(true, false) {
		close(c.reached)
		<-c.release
		if c.cancel {
			return context.Canceled
		}
	}
	return c.Context.Err()
}

func TestAssistantRunSnapshotReleasesCustodyAndOwnsBytes(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "snapshot", true: "canceled"}[cancel], func(t *testing.T) {
			store, binding, _, record, _, writer := assistantRunAdmitted(t)
			before, err := store.Read(t.Context(), binding, record.ID)
			assistantRunMust(t, err)
			ctx := &assistantSnapshotContext{Context: t.Context(), reached: make(chan struct{}), release: make(chan struct{}), cancel: cancel}
			testHookPrivateRead = func(name string) error {
				if name == store.name {
					ctx.armed.Store(true)
				}
				return nil
			}
			defer func() { testHookPrivateRead = nil }()
			type result struct {
				run assistantRunRecord
				err error
			}
			resultCh := make(chan result, 1)
			go func() { run, err := store.Read(ctx, binding, record.ID); resultCh <- result{run, err} }()
			select {
			case <-ctx.reached:
			case <-time.After(5 * time.Second):
				t.Fatal("read did not reach validated snapshot")
			}
			// Only the reader uses ctx; the writer's context cannot consume the barrier.
			writeCtx, stop := context.WithTimeout(t.Context(), 5*time.Second)
			writeErr := writer.Intent(writeCtx, assistantRunOp("later-operation"))
			stop()
			close(ctx.release)
			got := <-resultCh
			assistantRunMust(t, writeErr)
			if cancel {
				if !errors.Is(got.err, context.Canceled) || !reflect.DeepEqual(got.run, assistantRunRecord{}) {
					t.Fatal("cancellation returned a partial snapshot", got.err)
				}
			} else {
				assistantRunMust(t, got.err)
				if !reflect.DeepEqual(got.run, before) {
					t.Fatal("read observed a later write")
				}
				if len(got.run.Checkpoint.Operations) > 0 {
					got.run.Checkpoint.Operations[0].Name = "caller-change"
				}
			}
			after, err := store.Read(t.Context(), binding, record.ID)
			assistantRunMust(t, err)
			if len(after.Operations) != 1 || after.Operations[0].ID != "later-operation" {
				t.Fatal("later write lost or snapshot aliased storage")
			}
		})
	}
}

func TestAssistantRunSnapshotRefusalsAndMissing(t *testing.T) {
	store, binding, _, record, lease, _ := assistantRunAdmitted(t)
	siblingBinding := binding
	siblingBinding.Owner.ID = "other-chat"
	_, _, err := lease.Admit(t.Context(), siblingBinding, "sibling", digestOf([]byte("other body")), func() error { return nil })
	assistantRunMust(t, err)
	original := assistantRunRaw(t, store)
	data, err := store.server.openChatData()
	assistantRunMust(t, err)
	path := filepath.Join(data.location.Path, store.name)
	data.root.Close()
	for _, kind := range []string{"unrelated-record", "malformed", "namespace", "symlink", "permissions", "missing"} {
		t.Run(kind, func(t *testing.T) {
			assistantRunMust(t, os.WriteFile(path, original, 0600))
			other := store
			switch kind {
			case "unrelated-record":
				var ledger assistantRunLedger
				assistantRunMust(t, json.Unmarshal(original, &ledger))
				ledger.Runs[1].State = "invalid"
				raw, err := json.Marshal(ledger)
				assistantRunMust(t, err)
				assistantRunMust(t, os.WriteFile(path, raw, 0600))
			case "malformed":
				assistantRunMust(t, os.WriteFile(path, []byte(`{"version":1`), 0600))
			case "namespace":
				other.project = strings.Repeat("f", 64)
				other.name = "assistant-runs-" + other.project + ".json"
			case "symlink":
				target := filepath.Join(t.TempDir(), "ledger")
				assistantRunMust(t, os.WriteFile(target, original, 0600))
				assistantRunMust(t, os.Remove(path))
				assistantRunMust(t, os.Symlink(target, path))
				defer func() { assistantRunMust(t, os.Remove(path)); assistantRunMust(t, os.WriteFile(path, original, 0600)) }()
			case "permissions":
				assistantRunMust(t, os.Chmod(path, 0644))
				defer func() { assistantRunMust(t, os.Chmod(path, 0600)) }()
			case "missing":
				assistantRunMust(t, os.Remove(path))
			}
			ledger, err := other.readSnapshot(t.Context())
			if kind == "missing" {
				assistantRunMust(t, err)
				if ledger.Version != 1 || ledger.Project != store.project || ledger.Runs == nil || len(ledger.Runs) != 0 {
					t.Fatal("missing ledger changed")
				}
				if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("read materialized missing ledger")
				}
			} else if err == nil || !reflect.DeepEqual(ledger, assistantRunLedger{}) {
				t.Fatal("unsafe snapshot returned", kind, err)
			}
			if _, err := other.Read(t.Context(), binding, record.ID); err == nil {
				t.Fatal("Read accepted refusal")
			}
			if _, err := other.lookup(t.Context(), binding, record.ID, "", false); err == nil {
				t.Fatal("lookup accepted refusal")
			}
		})
	}
}
