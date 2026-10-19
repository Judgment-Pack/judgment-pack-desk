package desk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Test-only backend. Per-batch serialization does not cover Runner IO.
type memoryEvaluationJournals struct {
	mu      sync.Mutex
	entries map[string]*memoryEvaluationEntry
}
type memoryEvaluationEntry struct {
	gate chan struct{}
	raw  []byte
}
type memoryEvaluationJournal struct {
	entry *memoryEvaluationEntry
	id    string
}

func newMemoryEvaluationJournals() *memoryEvaluationJournals {
	return &memoryEvaluationJournals{entries: map[string]*memoryEvaluationEntry{}}
}
func (p *memoryEvaluationJournals) Batch(id string) evaluationJournal {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry := p.entries[id]
	if entry == nil {
		entry = &memoryEvaluationEntry{gate: make(chan struct{}, 1)}
		p.entries[id] = entry
	}
	return memoryEvaluationJournal{entry, id}
}
func (j memoryEvaluationJournal) locked(ctx context.Context, use func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !inputEvaluationID.MatchString(j.id) {
		return errors.New("Invalid evaluation ID.")
	}
	select {
	case j.entry.gate <- struct{}{}:
		defer func() { <-j.entry.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return use()
}
func (j memoryEvaluationJournal) Read(ctx context.Context) (evaluationSnapshot, error) {
	var snapshot evaluationSnapshot
	err := j.locked(ctx, func() error {
		if j.entry.raw == nil {
			return os.ErrNotExist
		}
		var err error
		snapshot, err = decodeEvaluationSnapshot(j.entry.raw, j.id)
		return err
	})
	return snapshot, err
}
func (j memoryEvaluationJournal) CompareAndSwap(ctx context.Context, b inputEvaluation, revision string) (evaluationSnapshot, error) {
	raw, err := encodeEvaluationJournal(b, j.id)
	if err != nil {
		return evaluationSnapshot{}, err
	}
	var saved evaluationSnapshot
	err = j.locked(ctx, func() error {
		if j.entry.raw == nil {
			if revision != "" {
				return errEvaluationConflict
			}
		} else {
			before, err := decodeEvaluationSnapshot(j.entry.raw, j.id)
			if err != nil {
				return err
			}
			if revision == "" || before.SHA256 != revision {
				return errEvaluationConflict
			}
			if err = validateEvaluationTransition(before.Document, b); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		j.entry.raw = append([]byte(nil), raw...)
		saved, err = decodeEvaluationSnapshot(j.entry.raw, j.id)
		return err
	})
	return saved, err
}
func assertNoEvaluationFiles(t *testing.T, s *Server) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(s.projectDir, inputEvaluationsDir, "*", "record.json"))
	if err != nil || len(paths) > 0 {
		t.Fatal("evaluation file fallback", paths, err)
	}
}
