//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"errors"
	"os"
	"syscall"
)

func lockPrivateData(file *os.File, exclusive bool) error {
	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	return syscall.Flock(int(file.Fd()), mode|syscall.LOCK_NB)
}

// lockHeld is whether lockPrivateData's error says another open file holds
// the lock now, rather than that none can be taken.
func lockHeld(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK)
}
