package desk

import (
	"context"
	"errors"
	"os"
)

// This adapter owns record.json only. The caller owns the run directory and
// the separate execution/admission leases, which must outlive this handle.
type fileResearchJournal struct {
	root *os.Root
	id   string
}

func (j fileResearchJournal) Read(ctx context.Context) (inputResearchRun, error) {
	if err := ctx.Err(); err != nil {
		return inputResearchRun{}, err
	}
	raw, err := readPrivateData(j.root, "record.json", inputResearchRecordLimit)
	if err != nil {
		return inputResearchRun{}, err
	}
	return decodeResearchJournal(raw, j.id)
}
func (j fileResearchJournal) locked(ctx context.Context, change func() error) error {
	if !inputResearchID.MatchString(j.id) {
		return errors.New("Invalid research ID.")
	}
	lock, err := j.root.Open(".")
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = waitPrivateDataLock(ctx, lock, true); err != nil {
		return err
	}
	return change()
}
func (j fileResearchJournal) Create(ctx context.Context, run inputResearchRun) error {
	return j.create(ctx, run, j.write)
}
func (j fileResearchJournal) write(raw []byte) error {
	return writePrivateData(j.root, "record.json", raw)
}
func (j fileResearchJournal) create(ctx context.Context, run inputResearchRun, persist func([]byte) error) error {
	raw, err := encodeResearchJournal(run, j.id)
	if err != nil {
		return err
	}
	return j.locked(ctx, func() error {
		// A corrupt or unreadable journal is never treated as an empty slot.
		if _, err := j.root.Lstat("record.json"); err == nil {
			return os.ErrExist
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return persist(raw)
	})
}
func (j fileResearchJournal) Update(ctx context.Context, change func(*inputResearchRun) error) (inputResearchRun, error) {
	return j.update(ctx, change, j.write)
}
func (j fileResearchJournal) update(ctx context.Context, change func(*inputResearchRun) error, persist func([]byte) error) (inputResearchRun, error) {
	var saved inputResearchRun
	err := j.locked(ctx, func() error {
		raw, err := readPrivateData(j.root, "record.json", inputResearchRecordLimit)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		next, err := changeResearchJournal(raw, j.id, change)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := persist(next); err != nil {
			return err
		}
		saved, err = decodeResearchJournal(next, j.id)
		return err
	})
	return saved, err
}

// The default file adapter keeps its original read, validation, transaction and
// callback-once semantics. Only calls that reach its existing persistence point
// can report pressure. Injected external researchJournal providers retain their
// own contracts and are deliberately not wrapped as if they were local files.
type runtimeFileResearchJournal struct {
	fileResearchJournal
	pressure    runtimeStoragePressureReporter
	beforeStage func(storagePersistenceTarget) error
}

func (j runtimeFileResearchJournal) persist(operation string, raw []byte) (bool, error) {
	if j.beforeStage != nil {
		if err := j.beforeStage(storagePersistenceTarget{operation: operation, name: j.id}); err != nil {
			return false, err
		}
	}
	return writePrivateDataCommit(j.root, "record.json", raw)
}

func (j runtimeFileResearchJournal) Create(ctx context.Context, run inputResearchRun) error {
	var persistenceErr error
	var publicationObserved bool
	err := j.create(ctx, run, func(raw []byte) error {
		publicationObserved, persistenceErr = j.persist(storagePersistenceResearchCreate, raw)
		return persistenceErr
	})
	// create has released its file lock before this volatile update.
	j.pressure.report(persistenceErr, publicationObserved)
	return err
}

func (j runtimeFileResearchJournal) Update(ctx context.Context, change func(*inputResearchRun) error) (inputResearchRun, error) {
	var persistenceErr error
	var publicationObserved bool
	saved, err := j.update(ctx, change, func(raw []byte) error {
		publicationObserved, persistenceErr = j.persist(storagePersistenceResearchUpdate, raw)
		return persistenceErr
	})
	j.pressure.report(persistenceErr, publicationObserved)
	return saved, err
}
