package desk

// One lock for each project's configuration and its lock (issue #284, the
// ADR-0010 line audit's finding 2).
//
// Two Desk processes can serve one project: with two configuration folders,
// or one. Each has its own review mutex and its own write mutex, which hold
// nothing against the other. Without a lock between them, one process's
// upgrade could publish jpack.json and run `packs lock`, the other could
// review that configuration and complete a signing upgrade over it, and the
// first, failing, could put back the configuration and the lock from before
// its own upgrade: the second's signing configuration gone, its key orphaned,
// and the records after it unsigned. The signing folder's lock
// (signing_lock.go) does not help: only a signing upgrade takes it, and two
// configuration folders have two.
//
// So every transaction that writes a project's jpack.json or its lock,
// signed or not, from its fresh reading to its last write or its rollback,
// takes flock's exclusive lock on the project's own folder, opened afresh
// through the folder Desk holds:
//
//   - an upgrade (`upgradeConfirmed`), and every file it puts back;
//   - a review's lock (`lockConfirmed`), and the lock it puts back;
//   - a start's writing of the project's identity (startup_identity.go).
//
// The project's folder, and not a file in `.desk-private/`: that folder is
// what the first upgrade makes, and a project that declares an audit folder
// of its own may never have one. The folder's own lock writes nothing into
// the project, and is the same lock for every Desk process that serves it,
// whatever configuration folder each has, and whatever path each was started
// through. No runtime takes a lock on it: the runtime locks its trail's
// files.
//
// The owner's actions wait a bounded time for it (projectLockWait), then
// refuse in plain words with no path. A start tries it once, and where it is
// held, changes nothing until the next start. **Where no lock can be taken
// here** (a build or a file system with no flock), nothing is written: the
// owner's actions refuse, and say why, and a start changes nothing (review
// round 1 of #296). No transaction runs without the exclusion.
//
// The lock belongs to the open file description, so a second descriptor of
// the folder, in this process or another, is excluded as another process
// would be. Closing the descriptor releases it, and a stopped process
// releases it. Desk opens every file with close-on-exec, so no runtime it
// starts inherits it.
//
// **And what a transaction puts back, it puts back only over what it wrote**
// (`putBack`): each file is compared with the bytes the transaction wrote,
// and the lock with the configuration the transaction wrote or locked, by the
// digest the lock pins (`restoreLock`), immediately before the old bytes are
// published. A lock read after the runtime ran is never taken for the one it
// wrote. A file that holds anything else was written since by another
// writer, and is left as it is, and said.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"sync"
	"time"
)

var (
	// errProjectBusy is a lock another descriptor holds: another Desk
	// process, or another transaction in this one, is changing the project's
	// configuration or its lock now.
	errProjectBusy = errors.New("another Desk process is changing this project's jpack.json or its lock")
	// errProjectNoLock is a lock that cannot be taken here at all.
	errProjectNoLock = errors.New("no lock can be taken on this project's folder here")
	// errPutBackChanged is a file that does not hold what the transaction
	// wrote: another writer changed it since.
	errPutBackChanged = errors.New("another writer changed it after Desk wrote it, so it was left as it is")
)

// projectBusyWords is what an owner's action that could not take the lock in
// time says, in words with no path; projectNoLockWords what one says where no
// lock can be taken here at all.
const (
	projectBusyWords   = "another Desk process is changing this project's jpack.json or its lock; try again."
	projectNoLockWords = "Desk can take no lock on this project's folder here, and without one it cannot keep another Desk process from changing jpack.json or its lock at the same time."
)

// projectLockWait bounds how long an owner's action waits for the lock. A
// variable only so a test can see the wait end without waiting it out.
var projectLockWait = 10 * time.Second

// lockProjectFile takes flock's exclusive lock on file, without waiting. A
// variable only so a test can stand in a file system that supports none.
var lockProjectFile = func(file *os.File) error { return lockPrivateData(file, true) }

// lockProject takes the lock on the project's folder, asked again while
// another descriptor holds it, for at most wait or until ctx ends: then
// errProjectBusy. errProjectNoLock where none can be taken here. With no
// wait, it asks once. It answers the release, which may be called more than
// once.
func (s *Server) lockProject(ctx context.Context, wait time.Duration) (unlock func(), err error) {
	file, err := s.root.Open(".")
	if err != nil {
		return nil, fmt.Errorf("%w: the folder could not be opened to lock it", errProjectNoLock)
	}
	deadline := time.Now().Add(wait)
	pause := 5 * time.Millisecond
	for {
		err := lockProjectFile(file)
		if err == nil {
			var release sync.Once
			return func() { release.Do(func() { file.Close() }) }, nil
		}
		if !lockHeld(err) {
			file.Close()
			return nil, fmt.Errorf("%w: %v", errProjectNoLock, err)
		}
		if !time.Now().Before(deadline) {
			file.Close()
			return nil, errProjectBusy
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, errProjectBusy
		case <-time.After(pause):
		}
		pause = min(2*pause, 100*time.Millisecond)
	}
}

// lockProjectFor is the lock an owner's action takes, bounded: the release,
// or the refusal, done names what was not done ("written", "locked"). Where
// no lock can be taken here, the action is refused too: no fallback without
// exclusion.
func (s *Server) lockProjectFor(ctx context.Context, what, done string) (func(), *lockFailure) {
	unlock, err := s.lockProject(ctx, projectLockWait)
	switch {
	case errors.Is(err, errProjectBusy):
		return nil, &lockFailure{http.StatusConflict, CodeBadRequest, "Nothing was " + done + ": " + projectBusyWords}
	case err != nil:
		s.log.Printf("desk: %s was refused, because this project's lock could not be taken: %v", what, err)
		return nil, &lockFailure{http.StatusConflict, CodeBadRequest, "Nothing was " + done + ": " + projectNoLockWords}
	}
	return unlock, nil
}

// testHookBeforePutBack runs after a file a transaction puts back is staged,
// immediately before it is compared again and published, and before a file
// it made is compared and removed. It is nil outside tests: a test is the
// writer there.
var testHookBeforePutBack func(name string)

// projectFile is what a project file held, or holds: its bytes, whether it
// was there, and an error where it could not be read, which tells nothing.
type projectFile struct {
	data    []byte
	present bool
	err     error
}

// readProjectFile reads name, a project file Desk writes, by the file API's
// rules for the path: what it holds now.
func (s *Server) readProjectFile(name string) projectFile {
	if err := s.refuseSymlinkedPath(name); err != nil {
		return projectFile{err: err}
	}
	data, _, err := s.readThroughRootWithin(name, maxFileBytes)
	switch {
	case codeOf(err) == CodeNotFound:
		return projectFile{}
	case err != nil:
		return projectFile{err: err}
	}
	return projectFile{data: data, present: true}
}

// holds is whether the project file read is wrote: the same bytes, or both
// absent.
func (f projectFile) holds(wrote projectFile) bool {
	return f.err == nil && wrote.err == nil && f.present == wrote.present && (!f.present || string(f.data) == string(wrote.data))
}

// putBack puts previous back at name, only where name still holds what the
// transaction wrote there: ours says so of what it holds now, nil where it
// does. It is asked after previous is staged, immediately before it is
// published, and before a file that was not there is removed. Anything else
// is ours's reason, or the reason it could not be read, and the file is left
// as it is. The caller holds the desk's write lock and the project's.
func (s *Server) putBack(name string, ours func(now projectFile) error, previous projectFile) error {
	if err := s.refuseSymlinkedPath(name); err != nil {
		return err
	}
	holds := func() error {
		now := s.readProjectFile(name)
		if now.err != nil {
			return fmt.Errorf("it could not be read now, so it was left as it is: %w", now.err)
		}
		return ours(now)
	}
	beforePutBack := func() {
		if testHookBeforePutBack != nil {
			testHookBeforePutBack(name)
		}
	}
	if previous.present {
		return s.atomicWriteChecked(name, previous.data, func(string) error { beforePutBack(); return holds() })
	}
	beforePutBack()
	if err := holds(); err != nil {
		return err
	}
	if err := s.root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// wroteBytes is putBack's test for a file the transaction wrote whole: it
// holds exactly those bytes.
func wroteBytes(wrote []byte) func(projectFile) error {
	return func(now projectFile) error {
		if !now.holds(projectFile{data: wrote, present: true}) {
			return errPutBackChanged
		}
		return nil
	}
}

// errLockNotOurs is a lock that pins another configuration than the one the
// transaction wrote or locked: another writer's.
var errLockNotOurs = errors.New("it is a lock of another configuration than the one Desk wrote or locked, so it is another writer's, and was left as it is")

// pinsConfig is putBack's test for the lock: it is no other transaction's
// lock. A lock that pins config, the digest of the configuration the
// transaction wrote or locked, is its own; a file that is not a lock at all
// (none, one cut short, one that names no configuration's digest) is no
// transaction's work; a lock of another configuration is another writer's,
// however soon after the transaction's runtime it was read.
func pinsConfig(config string) func(projectFile) error {
	return func(now projectFile) error {
		if !now.present {
			return nil
		}
		if pinned, _, err := lockedSet(now.data); err == nil && recordForm.MatchString(pinned.Config) && pinned.Config != config {
			return errLockNotOurs
		}
		return nil
	}
}
