package desk

import (
	"context"
	"errors"
	"os"
	"sync"
)

// A read view guards the records used by a research/decision admission. Close
// releases the guard; Get after Close is invalid. Reads are workspace scoped and
// share one consistent view, including selected records in another collection.
// The guard is separate from an ownership lease or permission to run a job.
type inputPreparationReader interface {
	Get(string) (inputPreparationReply, error)
}
type inputPreparationRead interface {
	inputPreparationReader
	Close() error
}

var errInputPreparationReadClosed = errors.New("dialect collection read view is closed")

type fileInputPreparationRead struct {
	mu   sync.Mutex
	ctx  context.Context
	root *os.Root
	lock *os.File
}

func (f fileInputPreparationRepository) Read(ctx context.Context) (inputPreparationRead, error) {
	view, err := f.openTransaction(ctx, false, true)
	if err != nil {
		return nil, err
	}
	return view, nil
}
func (f fileInputPreparationRepository) TryRead(ctx context.Context) (inputPreparationRead, error) {
	view, err := f.openTransaction(ctx, false, false)
	if err != nil {
		return nil, err
	}
	return view, nil
}
func (f fileInputPreparationRepository) openTransaction(ctx context.Context, write, wait bool) (*fileInputPreparationRead, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := f.open(write)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = errors.Join(errInputPreparationStoreMissing, err)
		}
		return nil, err
	}
	lock, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	view := &fileInputPreparationRead{ctx: ctx, root: root, lock: lock}
	if wait {
		err = waitPrivateDataLock(ctx, lock, write)
	} else {
		err = lockPrivateData(lock, write)
	}
	if err != nil {
		view.Close()
		return nil, errors.Join(errInputPreparationBusy, err)
	}
	if err = ctx.Err(); err != nil {
		view.Close()
		return nil, err
	}
	return view, nil
}
func (v *fileInputPreparationRead) Get(id string) (inputPreparationReply, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.root == nil {
		return inputPreparationReply{}, errInputPreparationReadClosed
	}
	if err := v.ctx.Err(); err != nil {
		return inputPreparationReply{}, err
	}
	if !inputPreparationID.MatchString(id) {
		return inputPreparationReply{}, errors.New("invalid dialect collection identity")
	}
	return readInputPreparation(v.root, id)
}
func (v *fileInputPreparationRead) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.root == nil {
		return nil
	}
	err := errors.Join(v.lock.Close(), v.root.Close())
	v.root = nil
	return err
}
