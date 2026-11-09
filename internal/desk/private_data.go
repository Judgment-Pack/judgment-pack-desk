package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Private data uses the existing ownership rules, but owns no credential files.
// Walk through pinned parents and compare each opened directory with its entry.
// A missing folder is made the storage line's way (folderCreation's zero
// value); a Server makes its own with openPrivateDataRootBy and s.creation.
func openPrivateDataRoot(dir string, create bool) (*os.Root, error) {
	return openPrivateDataRootBy(dir, create, folderCreation{})
}

// openPrivateDataRootBy is openPrivateDataRoot with a missing folder made the
// way how says.
func openPrivateDataRootBy(dir string, create bool, how folderCreation) (*os.Root, error) {
	if !custodyChecked || !filepath.IsAbs(dir) || strings.ContainsAny(dir, "\x00\r\n") {
		return nil, errors.New("chat storage requires an absolute path on a platform with file ownership checks")
	}
	parts := splitPath(dir)
	root, err := os.OpenRoot(parts[0])
	if err != nil {
		return nil, err
	}
	label := parts[0]
	for index, part := range parts[1:] {
		label = filepath.Join(label, part)
		info, err := root.Lstat(part)
		if errors.Is(err, os.ErrNotExist) && create {
			created, _, createErr := how.makeFolder(root, part)
			if createErr != nil {
				root.Close()
				return nil, createErr
			}
			createErr = syncPrivateDirectory(root)
			if created != nil {
				createErr = errors.Join(createErr, created.Close())
			}
			if createErr != nil {
				root.Close()
				return nil, createErr
			}
			info, err = root.Lstat(part)
		}
		last := index == len(parts)-2
		if err == nil {
			err = safeDirectory(label, info, last)
		}
		if err != nil {
			root.Close()
			return nil, err
		}
		if last && needsNarrowing(info.Mode()) {
			root.Close()
			return nil, fmt.Errorf("chat storage %s must be private (directory permissions 0700); choose a new folder or correct its permissions", label)
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			root.Close()
			return nil, err
		}
		opened, err := next.Stat(".")
		if err == nil && !os.SameFile(info, opened) {
			err = errors.New("chat storage changed while being opened")
		}
		if err == nil {
			err = safeDirectory(label, opened, last)
		}
		if err == nil && last && needsNarrowing(opened.Mode()) {
			err = errors.New("chat storage permissions changed while being opened")
		}
		root.Close()
		if err != nil {
			next.Close()
			return nil, err
		}
		root = next
	}
	if len(parts) < 2 {
		root.Close()
		return nil, errors.New("choose a private data folder, not the filesystem root")
	}
	return root, nil
}

// errPrivateDataChanged is a private file that was not the same file when
// opened as when inspected: a moment's state, not the file's.
var errPrivateDataChanged = errors.New("private data changed while being opened")

func readPrivateData(root *os.Root, name string, limit int) ([]byte, error) {
	data, _, err := readPrivateFile(root, name, limit)
	return data, err
}

// readPrivateFile is readPrivateData, and the file it read, as the open
// file's own Stat gave it: what a caller that hands the file on by its path
// holds that path to.
func readPrivateFile(root *os.Root, name string, limit int) ([]byte, os.FileInfo, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if err = ownerOnlyFile(name, info.Mode()); err != nil {
		return nil, nil, withCode(CodeForbidden, err)
	}
	if err = ownedByUs(name, info); err != nil {
		return nil, nil, withCode(CodeForbidden, err)
	}
	file, err := root.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, nil, withCode(CodeForbidden, errPrivateDataChanged)
	}
	if err = ownerOnlyFile(name, opened.Mode()); err != nil {
		return nil, nil, withCode(CodeForbidden, err)
	}
	if err = ownedByUs(name, opened); err != nil {
		return nil, nil, withCode(CodeForbidden, err)
	}
	var data []byte
	if testHookPrivateRead != nil {
		err = testHookPrivateRead(name)
	}
	if err == nil {
		data, err = readBounded(file, limit)
	}
	var over overLimit
	switch {
	case errors.As(err, &over):
		return nil, nil, withCode(CodeTooLarge, fmt.Errorf("%s exceeds its %d-byte storage limit", name, limit))
	case err != nil:
		// **A failure to read is not "too large".** It says nothing of the
		// file, and is kept as itself, uncoded: a reader that decides from
		// the code what a file is (readDeskManifest, and so a start's sweep
		// of unfinished keys) must be able to tell "this is not a desk's
		// manifest" from "it could not be read now".
		return nil, nil, fmt.Errorf("%s could not be read: %w", name, err)
	}
	return data, opened, nil
}

// testHookPrivateRead runs before a private file's bytes are read, and is nil
// outside tests. An error it returns stands for a failure to read them, such
// as an I/O error, so a test can fail one read and let the next succeed.
var testHookPrivateRead func(name string) error

func writePrivateData(root *os.Root, name string, data []byte) error {
	_, err := writePrivateDataCommit(root, name, data)
	return err
}

// publicationObserved reports only whether Rename returned success. True is not
// a durability guarantee: directory Sync and verified read-back still follow.
// False does not prove rollback or grant retry permission after an error. The
// existing caller contract and every returned error remain unchanged.
func writePrivateDataCommit(root *os.Root, name string, data []byte) (publicationObserved bool, err error) {
	if name == dataLocationName {
		lease, e := storageCreatorAcquire(root)
		if e != nil {
			return false, e
		}
		defer func() { err = errors.Join(err, lease.Close()) }()
	}
	return writePrivateDataStaged(root, name, data)
}

// writeDataLocation writes the chat data location file in settings: with the
// storage line on, in the settings folder's creator window
// (writePrivateDataCommit); off, as v0.13.2 wrote it, with no window to be
// refused by a folder another request is making there.
func (s *Server) writeDataLocation(settings *os.Root, data []byte) error {
	if s.deskOwner().storageLineOff {
		_, err := writePrivateDataStaged(settings, dataLocationName, data)
		return err
	}
	return writePrivateData(settings, dataLocationName, data)
}

// writePrivateDataStaged is writePrivateDataCommit's write itself: staged,
// synced, renamed over name, and read back.
func writePrivateDataStaged(root *os.Root, name string, data []byte) (publicationObserved bool, err error) {
	file, stage, err := newDataStage(root)
	if err != nil {
		return publicationObserved, err
	}
	defer root.Remove(stage)
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return publicationObserved, err
	}
	if err = file.Chmod(custodyFileMode); err != nil {
		return publicationObserved, err
	}
	if err = file.Sync(); err != nil {
		return publicationObserved, err
	}
	if err = file.Close(); err != nil {
		return publicationObserved, err
	}
	if err = root.Rename(stage, name); err != nil {
		return publicationObserved, err
	}
	publicationObserved = true
	if err = syncPrivateDirectory(root); err != nil {
		return publicationObserved, err
	}
	read, err := readPrivateData(root, name, len(data))
	if err != nil {
		return publicationObserved, err
	}
	if !bytes.Equal(read, data) {
		return publicationObserved, withCode(CodeStale, errors.New("private data changed after writing; reload before continuing"))
	}
	return publicationObserved, nil
}

func decodeDataJSON(data []byte, into any) error {
	if !validUTF8(data) {
		return errors.New("private storage settings must be UTF-8")
	}
	if _, _, err := topLevelMembers(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("expected one JSON document")
	}
	return nil
}

// The lock file is stable and never renamed or removed. Every cooperating Desk
// sharing this configuration root serializes storage changes through this inode.
func (s *Server) privateDataLock(exclusive bool) (*privateDataShortLock, error) {
	return s.privateDataLockContext(context.Background(), exclusive)
}

func (s *Server) privateDataLockContext(ctx context.Context, exclusive bool) (*privateDataShortLock, error) {
	// Lazy storage demand must complete under the original enrollment lifetime
	// before this transaction takes the shared private-data lock.
	nonmatchingWait, err := s.defaultHistoryEnrollmentTurn(ctx, false)
	if err != nil {
		return nil, err
	}
	// Matching first-use joins have their own finite wait bound. Unrelated
	// enrollment waiting and the following queue/OS wait share this allowance.
	ctx, cancel := context.WithTimeout(ctx, privateDataLockWait-nonmatchingWait)
	defer cancel()
	return s.privateDataSettingsLockContext(ctx, exclusive)
}

// The settings-only startup probe must not demand a history namespace merely
// to inspect whether retained runs exist. It shares the same short transaction
// queue and OS lock; callers that access history first enter enrollment above.
func (s *Server) privateDataSettingsLockContext(ctx context.Context, exclusive bool) (*privateDataShortLock, error) {
	// The queue and OS lock share one overall bound, including time behind
	// another transaction in this Server. Other processes still use the OS lock.
	ctx, cancel := context.WithTimeout(ctx, privateDataLockWait)
	defer cancel()
	release, turn, err := s.privateDataTurns.acquireOriginal(ctx)
	if err != nil {
		return nil, withCode(CodeStale, fmt.Errorf("chat storage is busy or unavailable; finish active work and retry: %w", err))
	}
	file, err := s.openPrivateDataFileLock(ctx, ".data.lock", exclusive, true)
	if err != nil {
		release()
		return nil, err
	}
	return newPrivateDataShortLock(file, release, turn, exclusive), nil
}

// tryPrivateDataLockContext is a short transaction admission for callers that
// already own host custody. Both local admission and OS exclusion are try-only;
// the local turn remains held until the returned OS descriptor is closed.
func (s *Server) tryPrivateDataLockContext(ctx context.Context, exclusive bool) (*privateDataShortLock, error) {
	if _, err := s.defaultHistoryEnrollmentTurn(ctx, true); err != nil {
		if errors.Is(err, errPrivateDataTurnBusy) {
			return nil, withCode(CodeStale, fmt.Errorf("chat storage is busy or unavailable; finish active work and retry: %w", err))
		}
		return nil, err
	}
	release, turn, err := s.privateDataTurns.tryAcquireOriginal(ctx)
	if err != nil {
		return nil, withCode(CodeStale, fmt.Errorf("chat storage is busy or unavailable; finish active work and retry: %w", err))
	}
	file, err := s.openPrivateDataFileLock(ctx, ".data.lock", exclusive, false)
	if err != nil {
		release()
		return nil, err
	}
	lock := newPrivateDataShortLock(file, release, turn, exclusive)
	// A canceled admission never transfers custody, even if the OS try won.
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, lock.Close())
	}
	return lock, nil
}

func (s *Server) privateDataFileLock(name string, exclusive bool) (*os.File, error) {
	return s.openPrivateDataFileLock(context.Background(), name, exclusive, false)
}

func (s *Server) openPrivateDataFileLock(ctx context.Context, name string, exclusive, wait bool) (*os.File, error) {
	if !s.assistant.usable() {
		return nil, s.assistant.problem
	}
	file, err := s.assistant.root.OpenFile(name, os.O_CREATE|os.O_RDWR|openNoFollow|openNonBlocking, custodyFileMode)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil {
		err = ownerOnlyFile("private data lock", info.Mode())
	}
	if err == nil {
		err = ownedByUs("private data lock", info)
	}
	if err == nil {
		if wait {
			err = waitPrivateDataLock(ctx, file, exclusive)
		} else {
			err = lockPrivateData(file, exclusive)
		}
	}
	if err != nil {
		file.Close()
		return nil, withCode(CodeStale, fmt.Errorf("chat storage is busy or unavailable; finish active work and retry: %w", err))
	}
	return file, nil
}

func syncPrivateDirectory(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// Claim an empty store once; a second configuration must never replace its owner.
func claimPrivateData(root *os.Root, name string, data []byte) (err error) {
	if name == dataMarkerName {
		lease, e := storageCreatorAcquire(root)
		if e != nil {
			return e
		}
		defer func() { err = errors.Join(err, lease.Close()) }()
	}
	return claimPrivateFile(root, name, data)
}

// claimDataMarker claims an empty chat data folder for this Desk with its
// marker: with the storage line on, in the folder's creator window
// (claimPrivateData); off, as v0.13.2 claimed it. Either way the marker is
// made only where none is, so a second configuration never replaces it.
func (s *Server) claimDataMarker(root *os.Root, data []byte) error {
	if s.deskOwner().storageLineOff {
		return claimPrivateFile(root, dataMarkerName, data)
	}
	return claimPrivateData(root, dataMarkerName, data)
}

// claimPrivateFile is claimPrivateData's claim itself: name made exclusively,
// written and synced, with its folder.
func claimPrivateFile(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|openNoFollow, custodyFileMode)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return syncPrivateDirectory(root)
}

func checkPrivateDataPath(store *chatDataStore) error {
	if store.legacy {
		return nil
	}
	current, err := openPrivateDataRoot(store.location.Path, false)
	if err != nil {
		return err
	}
	defer current.Close()
	before, err := store.root.Stat(".")
	if err != nil {
		return err
	}
	after, err := current.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) {
		return errors.New("chat storage folder changed during the operation")
	}
	return nil
}

func newDataStage(root *os.Root) (*os.File, string, error) {
	token, err := NewToken()
	if err != nil {
		return nil, "", err
	}
	name := ".data-" + token
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR|openNoFollow, custodyFileMode)
	return file, name, err
}

// The FIFO turn and retained witness remain distinct lock authorities.
// privateDataTurnQueue prevents this Server from reacquiring its short storage
// lock ahead of existing waiters. It does not promise cross-process fairness.
// The head keeps its turn until its OS lock has been closed.
type privateDataTurnQueue struct {
	mu      sync.Mutex
	waiters []*privateDataTurnWitness
	changed chan struct{}
}

func (q *privateDataTurnQueue) acquire(ctx context.Context) (func(), error) {
	release, _, err := q.acquireOriginal(ctx)
	return release, err
}

// Only actual queue entry constructs this witness. It cannot be reconstructed
// from a file descriptor, incoming context or matching inode. Scheduling fields
// grant no retain, native permit or storage authority.
type privateDataTurnWitness struct {
	self  *privateDataTurnWitness
	queue *privateDataTurnQueue
	ready chan struct{}

	firstUse    privateDataFirstUseKey
	accountedAt time.Time
	nonmatching time.Duration
}

// HTTP dispatch retains the exact requester Server (including cached child
// wrappers). Neither paths nor caller-authored context values identify a join.
type privateDataFirstUseKey struct {
	construction *storageBudgetConstruction
	requester    *Server
}

func (key privateDataFirstUseKey) matches(other privateDataFirstUseKey) bool {
	return key.construction != nil && key.requester != nil && key == other
}

const privateDataFirstUseJoinWait = 15 * time.Second

func (w *privateDataTurnWitness) current() bool {
	if w == nil || w.self != w || w.queue == nil {
		return false
	}
	w.queue.mu.Lock()
	defer w.queue.mu.Unlock()
	return len(w.queue.waiters) > 0 && w.queue.waiters[0] == w && w.queue.waiters[0].ready == w.ready
}

func (q *privateDataTurnQueue) enqueueTurnLocked(key privateDataFirstUseKey, started time.Time) *privateDataTurnWitness {
	w := &privateDataTurnWitness{queue: q, ready: make(chan struct{}), firstUse: key}
	w.self = w
	if !started.IsZero() {
		w.accountedAt = time.Now()
		// Time before enqueue has no classified head observation.
		w.nonmatching = w.accountedAt.Sub(started)
	}
	q.waiters = append(q.waiters, w)
	if q.changed == nil {
		q.changed = make(chan struct{})
	}
	if len(q.waiters) == 1 {
		close(w.ready)
	}
	return w
}

func (q *privateDataTurnQueue) acquireOriginal(ctx context.Context) (func(), *privateDataTurnWitness, error) {
	q.mu.Lock()
	w := q.enqueueTurnLocked(privateDataFirstUseKey{}, time.Time{})
	queued := len(q.waiters) > 1
	q.mu.Unlock()

	// Overlay-only notification after enqueue and outside the queue mutex.
	if queued && testHookPrivateDataTurnQueued != nil {
		testHookPrivateDataTurnQueued(ctx)
	}
	release := q.releaseTurn(w)
	select {
	case <-ctx.Done():
	case <-w.ready:
	}
	// Cancellation wins even when the previous holder released concurrently.
	if err := ctx.Err(); err != nil {
		release()
		return nil, nil, err
	}
	return release, w, nil
}

// First-use joining has one absolute wait bound, plus the existing cumulative
// short-turn allowance behind unclassified or different demands. These timers
// end at acquisition; the original caller context still governs execution.
func (q *privateDataTurnQueue) acquireFirstUseOriginal(ctx context.Context, key privateDataFirstUseKey) (func(), *privateDataTurnWitness, error) {
	started := time.Now()
	deadline := started.Add(privateDataFirstUseJoinWait)
	if callerDeadline, ok := ctx.Deadline(); ok && callerDeadline.Before(deadline) {
		deadline = callerDeadline
	}
	absolute := time.NewTimer(time.Until(deadline))
	defer absolute.Stop()
	q.mu.Lock()
	w := q.enqueueTurnLocked(key, started)
	queued := len(q.waiters) > 1
	q.mu.Unlock()
	if queued && testHookPrivateDataTurnQueued != nil {
		testHookPrivateDataTurnQueued(ctx)
	}
	release := q.releaseTurn(w)
	for {
		q.mu.Lock()
		now := time.Now()
		q.accountFirstUseLocked(w, now)
		head := q.waiters[0] == w
		matching := q.firstUseHeadMatchesLocked(w)
		remaining := privateDataLockWait - w.nonmatching
		changed := q.changed
		q.mu.Unlock()

		// Check the caller before either readiness or a scheduling timeout.
		if err := ctx.Err(); err != nil {
			release()
			return nil, nil, err
		}
		if !time.Now().Before(deadline) || remaining <= 0 {
			release()
			return nil, nil, context.DeadlineExceeded
		}
		if head {
			return release, w, nil
		}
		var short *time.Timer
		var shortDone <-chan time.Time
		if !matching {
			short = time.NewTimer(time.Until(now.Add(remaining)))
			shortDone = short.C
		}
		select {
		case <-ctx.Done():
		case <-absolute.C:
		case <-shortDone:
		case <-w.ready:
		case <-changed:
		}
		if short != nil {
			short.Stop()
		}
	}
}

func (q *privateDataTurnQueue) firstUseHeadMatchesLocked(w *privateDataTurnWitness) bool {
	head := q.waiters[0]
	return head.self == head && head.queue == q && w.firstUse.matches(head.firstUse)
}

// Charge at every head transition, even if a waiter has not run since enqueue.
// Each witness accounts only while queued; promotion freezes its waiting total.
func (q *privateDataTurnQueue) accountFirstUseLocked(w *privateDataTurnWitness, now time.Time) {
	if w.accountedAt.IsZero() || q.waiters[0] == w {
		return
	}
	if !q.firstUseHeadMatchesLocked(w) {
		w.nonmatching += now.Sub(w.accountedAt)
	}
	w.accountedAt = now
}

var errPrivateDataTurnBusy = errors.New("private data has an active or queued transaction")

// tryAcquire never waits or passes an existing local transaction. The queue
// mutex itself is try-only because the caller may already own host custody.
func (q *privateDataTurnQueue) tryAcquire(ctx context.Context) (func(), error) {
	release, _, err := q.tryAcquireOriginal(ctx)
	return release, err
}

func (q *privateDataTurnQueue) tryAcquireOriginal(ctx context.Context) (func(), *privateDataTurnWitness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if !q.mu.TryLock() {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		return nil, nil, errPrivateDataTurnBusy
	}
	if err := ctx.Err(); err != nil {
		q.mu.Unlock()
		return nil, nil, err
	}
	if len(q.waiters) != 0 {
		q.mu.Unlock()
		return nil, nil, errPrivateDataTurnBusy
	}
	w := q.enqueueTurnLocked(privateDataFirstUseKey{}, time.Time{})
	q.mu.Unlock()
	release := q.releaseTurn(w)
	if err := ctx.Err(); err != nil {
		release()
		return nil, nil, err
	}
	return release, w, nil
}

func (q *privateDataTurnQueue) releaseTurn(w *privateDataTurnWitness) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			q.mu.Lock()
			defer q.mu.Unlock()
			for i, waiter := range q.waiters {
				if waiter == w {
					if i == 0 {
						now := time.Now()
						// O(n) in the existing queue depth; retain no transition history.
						for _, waiting := range q.waiters[1:] {
							q.accountFirstUseLocked(waiting, now)
						}
					}
					copy(q.waiters[i:], q.waiters[i+1:])
					q.waiters[len(q.waiters)-1] = nil
					q.waiters = q.waiters[:len(q.waiters)-1]
					if i == 0 {
						close(q.changed)
						// An idle queue retains no notification from its prior lifetime.
						q.changed = nil
						if len(q.waiters) > 0 {
							q.changed = make(chan struct{})
							close(q.waiters[0].ready)
						}
					}
					return
				}
			}
		})
	}
}

type privateDataShortLock struct {
	*os.File
	self      *privateDataShortLock
	turn      *privateDataTurnWitness
	exclusive bool
	mu        sync.Mutex
	closed    bool
	once      sync.Once
	release   func()
	closeErr  error
}

func newPrivateDataShortLock(file *os.File, release func(), turn *privateDataTurnWitness, exclusive bool) *privateDataShortLock {
	lock := &privateDataShortLock{File: file, release: release, turn: turn, exclusive: exclusive}
	lock.self = lock
	return lock
}

func (lock *privateDataShortLock) Close() error {
	lock.once.Do(func() {
		lock.mu.Lock()
		lock.closed = true
		lock.mu.Unlock()
		// Release actual custody before allowing the next local transaction.
		lock.closeErr = lock.File.Close()
		lock.release()
	})
	return lock.closeErr
}

// Overlay-only observer; nil in production, no return value or queue authority.
var testHookPrivateDataTurnQueued func(context.Context)
