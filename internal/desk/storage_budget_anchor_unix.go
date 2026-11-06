//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"os"
	"syscall"
)

func storageDirectoryID(root *os.Root) (storageDirectoryIdentity, error) {
	info, e := root.Stat(".")
	if e != nil || !storagePrivate(info, true) {
		return storageDirectoryIdentity{}, errStorageCustody
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return storageDirectoryIdentity{}, errStorageCustody
	}
	return storageDirectoryIdentity{uint64(stat.Dev), uint64(stat.Ino)}, nil
}
func storageUnlock(file *os.File) error { return syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }
