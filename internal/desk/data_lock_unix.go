//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
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
