package desk

import (
	"context"
	"errors"
	"os"
)

type fileEvaluationJournal struct {
	root *os.Root
	id   string
}

func (j fileEvaluationJournal) Read(ctx context.Context) (evaluationSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return evaluationSnapshot{}, err
	}
	raw, err := readPrivateData(j.root, "record.json", inputEvaluationLimit)
	if err != nil {
		return evaluationSnapshot{}, err
	}
	return decodeEvaluationSnapshot(raw, j.id)
}
func (j fileEvaluationJournal) CompareAndSwap(ctx context.Context, b inputEvaluation, revision string) (evaluationSnapshot, error) {
	raw, err := encodeEvaluationJournal(b, j.id)
	if err != nil {
		return evaluationSnapshot{}, err
	}
	// Separate from the directory's coordinator lease. No database/storage lock
	// spans Runner IO, and concurrent controls cannot bypass the coordinator.
	lock, err := j.root.OpenFile("journal.lock", os.O_CREATE|os.O_RDWR|openNoFollow|openNonBlocking, 0600)
	if err != nil {
		return evaluationSnapshot{}, err
	}
	defer lock.Close()
	if err = waitPrivateDataLock(ctx, lock, true); err != nil {
		return evaluationSnapshot{}, err
	}
	before, err := j.Read(ctx)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return evaluationSnapshot{}, err
	}
	if err == nil && (revision == "" || before.SHA256 != revision) || errors.Is(err, os.ErrNotExist) && revision != "" {
		return evaluationSnapshot{}, errEvaluationConflict
	}
	if err == nil {
		if err = validateEvaluationTransition(before.Document, b); err != nil {
			return evaluationSnapshot{}, err
		}
	}
	if err = ctx.Err(); err != nil {
		return evaluationSnapshot{}, err
	}
	if err = writePrivateData(j.root, "record.json", raw); err != nil {
		return evaluationSnapshot{}, err
	}
	return decodeEvaluationSnapshot(raw, j.id)
}
