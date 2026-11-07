package desk

import (
	"bytes"
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

// This optional capability extends the SAME guarded collection read view. It
// is not a snapshot reconstructed from Get, nor permission to mutate/recover.
// Injected repositories must explicitly implement TryRawRead and preserve the
// original exact bytes, same exclusion/cancellation and Close/Get-after-Close
// contract. An ordinary repository/read view alone is deliberately unavailable.
type inputPreparationRawSnapshot struct {
	ID, Revision string
	Bytes        []byte
}
type inputPreparationRawRead interface {
	inputPreparationRead
	GetRaw(string) (inputPreparationRawSnapshot, error)
}
type inputPreparationRawRepository interface {
	TryRawRead(context.Context) (inputPreparationRawRead, error)
}

var errInputPreparationRawUnavailable = errors.New("exact dialect collection raw read is unavailable")

func tryInputPreparationRawRead(ctx context.Context, repo inputPreparationRepository) (inputPreparationRawRead, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	provider, ok := repo.(inputPreparationRawRepository)
	if !ok {
		return nil, errInputPreparationRawUnavailable
	}
	view, err := provider.TryRawRead(ctx)
	if err != nil {
		if view != nil {
			err = errors.Join(err, view.Close())
		}
		return nil, err
	}
	if view == nil {
		return nil, errInputPreparationRawUnavailable
	}
	if err = ctx.Err(); err != nil {
		return nil, errors.Join(err, view.Close())
	}
	return view, nil
}
func validateInputPreparationRaw(id, revision string, snapshot inputPreparationRawSnapshot) (inputPreparationRawSnapshot, error) {
	if !inputPreparationID.MatchString(id) || revision != "" && !retainedArtifactDigest.MatchString(revision) || snapshot.ID != id || len(snapshot.Bytes) > inputPreparationLimit || !retainedArtifactDigest.MatchString(snapshot.Revision) || snapshot.Revision != digestOf(snapshot.Bytes) {
		return inputPreparationRawSnapshot{}, errInputPreparationRawUnavailable
	}
	if revision != "" && snapshot.Revision != revision {
		return inputPreparationRawSnapshot{}, errInputPreparationConflict
	}
	if _, err := decodeInputPreparation(id, snapshot.Bytes); err != nil {
		return inputPreparationRawSnapshot{}, err
	}
	snapshot.Bytes = bytes.Clone(snapshot.Bytes)
	return snapshot, nil
}

// Validate and detach injected results too; no provider's typed Get is called
// and no JSON serialization fallback can relabel a revision as original bytes.
func readInputPreparationRaw(ctx context.Context, view inputPreparationRawRead, id, revision string) (inputPreparationRawSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return inputPreparationRawSnapshot{}, err
	}
	if view == nil {
		return inputPreparationRawSnapshot{}, errInputPreparationRawUnavailable
	}
	if !inputPreparationID.MatchString(id) || revision != "" && !retainedArtifactDigest.MatchString(revision) {
		return inputPreparationRawSnapshot{}, errInputPreparationRawUnavailable
	}
	raw, err := view.GetRaw(id)
	if err != nil {
		return inputPreparationRawSnapshot{}, err
	}
	if err = ctx.Err(); err != nil {
		return inputPreparationRawSnapshot{}, err
	}
	return validateInputPreparationRaw(id, revision, raw)
}

type fileInputPreparationRawRead struct {
	*fileInputPreparationRead
	owner *storageBudgetOwner // diagnostic fault target only
}

func (f fileInputPreparationRepository) TryRawRead(ctx context.Context) (inputPreparationRawRead, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.rawOpen == nil {
		return nil, errInputPreparationRawUnavailable
	}
	root, absent, err := f.rawOpen()
	if err != nil {
		if root != nil {
			err = errors.Join(err, root.Close())
		}
		return nil, err
	}
	if absent {
		if root != nil {
			return nil, errors.Join(errInputPreparationRawUnavailable, root.Close())
		}
		return nil, errors.Join(errInputPreparationStoreMissing, os.ErrNotExist)
	}
	if root == nil {
		return nil, errInputPreparationRawUnavailable
	}
	lock, err := root.Open(".")
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	view := &fileInputPreparationRawRead{fileInputPreparationRead: &fileInputPreparationRead{ctx: ctx, root: root, lock: lock}, owner: f.rawOwner}
	if err = lockPrivateData(lock, false); err != nil {
		return nil, errors.Join(errInputPreparationBusy, err, view.Close())
	}
	if err = ctx.Err(); err != nil {
		return nil, errors.Join(err, view.Close())
	}
	return view, nil
}
func (v *fileInputPreparationRawRead) GetRaw(id string) (inputPreparationRawSnapshot, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.root == nil {
		return inputPreparationRawSnapshot{}, errInputPreparationReadClosed
	}
	if err := v.ctx.Err(); err != nil {
		return inputPreparationRawSnapshot{}, err
	}
	raw, err := readInputPreparationRawFile(v.root, id)
	// The checked file helper has actually joined its descriptor Close before
	// this success-only seam. Failed opens/reads retain their original errors.
	if err == nil && v.owner != nil {
		err = v.owner.fail("retention-read", "preparation-file-close")
	}
	if err == nil {
		err = v.ctx.Err()
	}
	if err != nil {
		return inputPreparationRawSnapshot{}, err
	}
	return raw, nil
}
func (v *fileInputPreparationRawRead) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.root == nil {
		return nil
	}
	err := errors.Join(v.lock.Close(), v.root.Close())
	v.root = nil
	if v.owner != nil {
		err = errors.Join(err, v.owner.fail("retention-read", "preparation-view-close"))
	}
	return err
}
