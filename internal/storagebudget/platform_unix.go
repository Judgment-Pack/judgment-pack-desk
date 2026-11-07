//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package storagebudget

import (
	"errors"
	"os"
	"syscall"
)

const noFollow = syscall.O_NOFOLLOW
const nonBlocking = syscall.O_NONBLOCK

func privateInfo(info os.FileInfo, directory bool) bool {
	if info == nil {
		return false
	}
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok || s.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0077 != 0 {
		return false
	}
	if directory {
		return info.IsDir() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0700 == 0700
	}
	return info.Mode().IsRegular() && s.Nlink == 1 && info.Mode().Perm()&0600 == 0600
}
func flock(f *os.File, exclusive bool) error {
	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	return syscall.Flock(int(f.Fd()), mode|syscall.LOCK_NB)
}
func unflock(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
func lockBusy(e error) bool    { return errors.Is(e, syscall.EWOULDBLOCK) }
