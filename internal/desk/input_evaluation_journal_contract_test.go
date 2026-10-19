package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func evaluationJournalFixture(t *testing.T, backend string) (evaluationJournal, inputEvaluation, *os.Root) {
	t.Helper()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	b := inputEvaluation{Version: 1, ID: "eval_" + strings.Repeat("a", 32), PreparationID: "prep_" + strings.Repeat("b", 32), Revision: "reviewed", RecipeVersion: 1, Name: "Reviewed records", ReleaseID: "rel_" + strings.Repeat("c", 32), CreatedAt: "2026-10-15T12:00:00Z", State: "review", Rows: []inputEvaluationRow{{RecordID: "row-1", Input: json.RawMessage(`{"facts":{"number":24,"decimal":"24","zero":0,"false":false,"null":null}}`), State: "pending"}}}
	var j evaluationJournal = fileEvaluationJournal{root: root, id: b.ID}
	if backend == "memory" {
		j = newMemoryEvaluationJournals().Batch(b.ID)
	}
	return j, b, root
}
func TestEvaluationJournalContract(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			j, b, root := evaluationJournalFixture(t, backend)
			ctx := t.Context()
			if _, err := j.Read(ctx); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("missing", err)
			}
			if _, err := j.CompareAndSwap(ctx, b, "missing"); !errors.Is(err, errEvaluationConflict) {
				t.Fatal("update created missing journal", err)
			}
			saved, err := j.CompareAndSwap(ctx, b, "")
			if err != nil || saved.SHA256 == "" {
				t.Fatal(err)
			}
			if _, err := j.CompareAndSwap(ctx, b, ""); !errors.Is(err, errEvaluationConflict) {
				t.Fatal("duplicate create", err)
			}
			saved.Document.Rows[0].Input[0] = '['
			before, err := j.Read(ctx)
			if err != nil || !bytes.Equal(before.Document.Rows[0].Input, b.Rows[0].Input) {
				t.Fatal("aliased create result", err)
			}
			before.Document.Rows[0].Problem = "caller mutation"
			again, err := j.Read(ctx)
			if err != nil || again.Document.Rows[0].Problem != "" {
				t.Fatal("aliased read", err)
			}
			var wg sync.WaitGroup
			result := make(chan error, 2)
			for _, text := range []string{"one", "two"} {
				wg.Go(func() {
					candidate := b
					candidate.Problem = text
					_, err := j.CompareAndSwap(ctx, candidate, saved.SHA256)
					result <- err
				})
			}
			wg.Wait()
			close(result)
			passed, conflicted := 0, 0
			for err := range result {
				if err == nil {
					passed++
				} else if errors.Is(err, errEvaluationConflict) {
					conflicted++
				} else {
					t.Fatal(err)
				}
			}
			if passed != 1 || conflicted != 1 {
				t.Fatal("lost/stale write", passed, conflicted)
			}
			newest, err := j.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := j.Read(cancelled); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if _, err := j.CompareAndSwap(cancelled, newest.Document, newest.SHA256); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			invalid := newest.Document
			invalid.ID = "wrong"
			if _, err := j.CompareAndSwap(ctx, invalid, newest.SHA256); err == nil {
				t.Fatal("invalid identity written")
			}
			current, err := j.Read(ctx)
			if err != nil || current.SHA256 != newest.SHA256 {
				t.Fatal("refused write changed revision", err)
			}
			other, _, _ := evaluationJournalFixture(t, backend)
			if _, err := other.Read(ctx); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("cross-workspace journal", err)
			}
			if backend == "memory" {
				if _, err := root.Stat("record.json"); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("file fallback", err)
				}
			}
		})
	}
}
func TestEvaluationJournalFreezesIntentAndReceipts(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			j, b, _ := evaluationJournalFixture(t, backend)
			b.ConfirmedAt = "2026-10-15T12:01:00Z"
			b.State = "running"
			b.JobID = "job_" + strings.Repeat("d", 32)
			b.Rows[0].RunID = "run_" + strings.Repeat("e", 32)
			b.Rows[0].State = "completed"
			b.Rows[0].Result = json.RawMessage(`{"disposition":{"kind":"outcome","outcomeId":"keep"}}`)
			b.Rows[0].Signing = json.RawMessage(`{"status":"failed"}`)
			first, err := j.CompareAndSwap(t.Context(), b, "")
			if err != nil {
				t.Fatal(err)
			}
			mutations := map[string]func(*inputEvaluation){
				"release": func(b *inputEvaluation) { b.ReleaseID = "different" }, "name": func(b *inputEvaluation) { b.Name = "different" }, "recipe": func(b *inputEvaluation) { b.RecipeVersion++ }, "confirmation": func(b *inputEvaluation) { b.ConfirmedAt = "" }, "job": func(b *inputEvaluation) { b.JobID = "" }, "row removal": func(b *inputEvaluation) { b.Rows = nil }, "selected row append": func(b *inputEvaluation) { b.Rows = append(b.Rows, inputEvaluationRow{RecordID: "row-2"}) }, "fact type": func(b *inputEvaluation) { b.Rows[0].Input = json.RawMessage(`{"facts":{"number":"24"}}`) }, "run": func(b *inputEvaluation) { b.Rows[0].RunID = "" }, "replay": func(b *inputEvaluation) { b.Rows[0].State = "pending" }, "result": func(b *inputEvaluation) { b.Rows[0].Result = json.RawMessage(`{}`) }, "signing": func(b *inputEvaluation) { b.Rows[0].Signing = nil },
			}
			for name, mutate := range mutations {
				t.Run(name, func(t *testing.T) {
					snapshot, err := j.Read(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					mutate(&snapshot.Document)
					if _, err := j.CompareAndSwap(t.Context(), snapshot.Document, snapshot.SHA256); err == nil {
						t.Fatal("changed retained intent")
					}
				})
			}
			saved, err := j.Read(t.Context())
			if err != nil || saved.SHA256 != first.SHA256 {
				t.Fatal("failed transition changed history", err)
			}
		})
	}
}
func TestEvaluationJournalOutboxRetainsBytesAndBaseline(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			j, b, _ := evaluationJournalFixture(t, backend)
			initial, err := j.CompareAndSwap(t.Context(), b, "")
			if err != nil {
				t.Fatal(err)
			}
			b.ConfirmedAt = "2026-10-15T12:01:00Z"
			b.State = "queued"
			b.Rows = nil
			b.Subscription = &inputSubscription{Mode: "changed", IncludeExisting: false, SourceClass: "asserted"}
			snapshot, err := j.CompareAndSwap(t.Context(), b, initial.SHA256)
			if err != nil {
				t.Fatal("subscription confirmation", err)
			}
			b.Subscription.Initialized = true
			b.Subscription.PolicyDigest = "policy"
			b.Subscription.Baseline = map[string]string{"baseline": "digest"}
			b.Subscription.TriggerID = "trg_" + strings.Repeat("d", 32)
			b.Rows = []inputEvaluationRow{{RecordID: "row-1", RecordDigest: "digest", EventID: "input-" + strings.Repeat("e", 64), EventAt: "2026-10-15T12:01:01Z", State: "pending", Input: initial.Document.Rows[0].Input}}
			snapshot, err = j.CompareAndSwap(t.Context(), b, snapshot.SHA256)
			if err != nil {
				t.Fatal(err)
			}
			for _, mutate := range []func(*inputEvaluation){func(b *inputEvaluation) { b.Rows[0].EventAt = "later" }, func(b *inputEvaluation) { b.Rows[0].EventID = "different" }, func(b *inputEvaluation) { b.Rows[0].RecordDigest = "different" }, func(b *inputEvaluation) { b.Rows[0].Input = nil }, func(b *inputEvaluation) { b.Subscription.Baseline = map[string]string{} }, func(b *inputEvaluation) { b.Subscription.Mode = "new" }, func(b *inputEvaluation) { b.Subscription.Initialized = false }, func(b *inputEvaluation) { b.Subscription.TriggerID = "" }} {
				candidate, err := j.Read(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				mutate(&candidate.Document)
				if _, err := j.CompareAndSwap(t.Context(), candidate.Document, candidate.SHA256); err == nil {
					t.Fatal("outbox identity changed")
				}
			}
			b.Rows[0].OccurrenceID = "occ_" + strings.Repeat("f", 32)
			b.Rows[0].State = "queued"
			b.Rows[0].Input = nil
			receipt, err := j.CompareAndSwap(t.Context(), b, snapshot.SHA256)
			if err != nil {
				t.Fatal("valid receipt refused", err)
			}
			b.Rows[0].Input = initial.Document.Rows[0].Input
			if _, err := j.CompareAndSwap(t.Context(), b, receipt.SHA256); err == nil {
				t.Fatal("acknowledged delivery reset")
			}
		})
	}
}
func TestEvaluationFileJournalLegacyReadUncertaintyAndLockWait(t *testing.T) {
	j, b, root := evaluationJournalFixture(t, "file")
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := writePrivateData(root, "record.json", raw); err != nil {
		t.Fatal(err)
	}
	saved, err := j.Read(t.Context())
	if err != nil || saved.SHA256 != digestOf(raw) {
		t.Fatal("legacy revision changed", err)
	}
	again, err := readPrivateData(root, "record.json", inputEvaluationLimit)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatal("read rewrote journal", err)
	}
	reads := 0
	testHookPrivateRead = func(name string) error {
		if name == "record.json" {
			reads++
			if reads == 2 {
				return errors.New("read-back unavailable")
			}
		}
		return nil
	}
	t.Cleanup(func() { testHookPrivateRead = nil })
	checkpoint := evaluationCheckpoint{journal: j, revision: saved.SHA256}
	b.ConfirmedAt = "confirmed"
	b.State = "queued"
	if err := checkpoint.retain(b); err == nil {
		t.Fatal("uncertain write acknowledged")
	}
	testHookPrivateRead = nil
	if err := checkpoint.retain(b); err == nil {
		t.Fatal("uncertain checkpoint reused")
	}
	recovered, err := j.Read(t.Context())
	if err != nil || recovered.Document.ConfirmedAt != "confirmed" {
		t.Fatal("committed work lost", err)
	}
	lock, err := root.OpenFile("journal.lock", os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = lockPrivateData(lock, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	if _, err := j.CompareAndSwap(ctx, recovered.Document, recovered.SHA256); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unbounded lock", err)
	}
}
func TestEvaluationJournalUnreadableIsNotEmpty(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			j, b, root := evaluationJournalFixture(t, backend)
			if backend == "file" {
				if err := writePrivateData(root, "record.json", []byte(`{}`)); err != nil {
					t.Fatal(err)
				}
			} else {
				j.(memoryEvaluationJournal).entry.raw = []byte(`{}`)
			}
			if _, err := j.Read(t.Context()); err == nil || errors.Is(err, os.ErrNotExist) {
				t.Fatal("corruption hidden", err)
			}
			if _, err := j.CompareAndSwap(t.Context(), b, ""); err == nil {
				t.Fatal("corrupt journal replaced")
			}
		})
	}
}
