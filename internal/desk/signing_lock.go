package desk

// One lock for everything that changes keys under Desk's signing folder
// (issue #230).
//
// Two Desk processes can share one configuration folder: Desk started on two
// projects shares `~/.config/jpack-desk`. Without a lock, one process's start
// could sweep away a key another was creating, between "saw a marker" and
// "removed the files", and the same for a rotation's recovery. So every
// change to keys there takes flock's exclusive lock on the signing folder's
// own descriptor, opened through the folder Desk holds, from its first
// inspection to its last effect:
//
//   - a desk's key creation: the marker, generate, the checks, the manifest,
//     and the marker's removal;
//   - the start's sweep of unfinished creations (`sweepUnfinishedKeys`);
//   - a rotation, all its steps (`rotateKey`);
//   - the start's recovery of rotations (`recoverRotations`).
//
// The sweep and the recovery try once. Where another process holds the lock
// they change nothing, and say so in Desk's log, for the next start: they
// never wait on, or race, a live creation or rotation. A creation or a
// rotation waits a bounded time (signingLockWait), then refuses in plain
// words with no path.
//
// The lock belongs to the open file description, so a second descriptor of
// the folder, in this process or another, is excluded as another process
// would be. Closing the descriptor releases it; a stopped process releases
// it. Desk opens every file with close-on-exec, so no runtime it starts
// inherits it.
//
// **Where no lock can be taken** (a build or a file system with no flock), a
// creation and a rotation go on as before the lock, and the sweep and the
// recovery change nothing: nothing is removed without the lock.
//
// Every process that changes keys there must take this same lock, on this
// same folder: lockSigning and lockSigningWithin are the helpers for it.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

var (
	// errSigningBusy is a lock another descriptor holds: another Desk process,
	// or another change in this one, is changing keys in the folder now.
	errSigningBusy = errors.New("another Desk process is changing keys in this configuration folder")
	// errSigningNoLock is a lock that cannot be taken here at all.
	errSigningNoLock = errors.New("no lock can be taken on Desk's signing folder here")
)

// signingBusyWords is what a creation or a rotation that could not take the
// lock in time says, in words with no path.
const signingBusyWords = "another Desk process is changing keys in this configuration folder; try again."

// signingLockWait bounds how long a creation or a rotation waits for the
// lock. A variable only so a test can see the wait end without waiting it out.
var signingLockWait = 10 * time.Second

// lockSigningFile takes flock's exclusive lock on file, without waiting. A
// variable only so a test can stand in a file system that supports none.
var lockSigningFile = func(file *os.File) error { return lockPrivateData(file, true) }

// lockSigning takes the lock on the signing folder dir holds, once, without
// waiting: errSigningBusy where another descriptor holds it, errSigningNoLock
// where none can be taken here. It answers the release, which may be called
// more than once.
func lockSigning(dir *signingDir) (unlock func(), err error) {
	file, err := dir.root.Open(".")
	if err != nil {
		return nil, fmt.Errorf("%w: the folder could not be opened to lock it", errSigningNoLock)
	}
	if err := lockSigningFile(file); err != nil {
		file.Close()
		if lockHeld(err) {
			return nil, errSigningBusy
		}
		return nil, fmt.Errorf("%w: %v", errSigningNoLock, err)
	}
	var release sync.Once
	return func() { release.Do(func() { file.Close() }) }, nil
}

// lockSigningWithin is lockSigning, asked again while another descriptor
// holds the lock, for at most wait or until ctx ends: then errSigningBusy.
func lockSigningWithin(ctx context.Context, dir *signingDir, wait time.Duration) (unlock func(), err error) {
	deadline := time.Now().Add(wait)
	pause := 5 * time.Millisecond
	for {
		unlock, err := lockSigning(dir)
		if !errors.Is(err, errSigningBusy) || !time.Now().Before(deadline) {
			return unlock, err
		}
		select {
		case <-ctx.Done():
			return nil, errSigningBusy
		case <-time.After(pause):
		}
		pause = min(2*pause, 100*time.Millisecond)
	}
}
