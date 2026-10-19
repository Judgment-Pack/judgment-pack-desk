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
		return writePrivateData(j.root, "record.json", raw)
	})
}
func (j fileResearchJournal) Update(ctx context.Context, change func(*inputResearchRun) error) (inputResearchRun, error) {
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
		if err := writePrivateData(j.root, "record.json", next); err != nil {
			return err
		}
		saved, err = decodeResearchJournal(next, j.id)
		return err
	})
	return saved, err
}
