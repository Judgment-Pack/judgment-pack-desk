//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package desk

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

// lockAuditShared takes flock(2)'s shared lock on file, as runtime 0.26.0's
// verifier does on these systems (`fssecure.platformLockShared`), waiting
// for a writer that holds the exclusive one, for at most wait. It returns the
// release.
//
// The lock belongs to the open file description, which is why it is flock
// and not a POSIX record lock: the runtime's writer takes flock, and only the
// same kind of lock excludes it. It is advisory: it holds every writer that
// asks for it, which the runtime's writers do.
//
// It asks without waiting and asks again, rather than waiting in the call,
// so that the wait is bounded and ends with the request. An answer that this
// file can never be locked here, the cases the runtime itself reads that way
// (EOPNOTSUPP, ENOTSUP, ENOSYS), is errAuditNoLock.
func lockAuditShared(ctx context.Context, file *os.File, wait time.Duration) (func(), error) {
	deadline := time.Now().Add(wait)
	pause := time.Millisecond
	for {
		err := flockAudit(file, syscall.LOCK_SH|syscall.LOCK_NB)
		switch {
		case err == nil:
			return func() { _ = flockAudit(file, syscall.LOCK_UN) }, nil
		case errors.Is(err, syscall.EWOULDBLOCK), errors.Is(err, syscall.EINTR):
		case errors.Is(err, syscall.EOPNOTSUPP), errors.Is(err, syscall.ENOTSUP), errors.Is(err, syscall.ENOSYS):
			return nil, errAuditNoLock
		default:
			return nil, err
		}
		if !time.Now().Before(deadline) {
			return nil, errAuditLockBusy
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pause):
		}
		pause = min(2*pause, 50*time.Millisecond)
	}
}

// flockAudit is one flock(2) call on file. A variable only so a test can
// stand in a file system that answers that it supports no flock.
var flockAudit = func(file *os.File, how int) error {
	conn, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var lockErr error
	if err := conn.Control(func(fd uintptr) { lockErr = syscall.Flock(int(fd), how) }); err != nil {
		return err
	}
	return lockErr
}
