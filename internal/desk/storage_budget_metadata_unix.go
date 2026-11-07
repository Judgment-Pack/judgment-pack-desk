//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"os"
	"syscall"
)

// Reported blocks are metadata, not evidence of exclusive allocation or reclaim.
func storageReportedAllocation(info os.FileInfo) (uint64, bool) {
	if info == nil {
		return 0, false
	}
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok || s == nil || s.Blocks < 0 || uint64(s.Blocks) > ^uint64(0)/512 {
		return 0, false
	}
	return uint64(s.Blocks) * 512, true
}

func storageFileIdentity(info os.FileInfo) (storageDirectoryIdentity, error) {
	if info == nil {
		return storageDirectoryIdentity{}, errStorageCustody
	}
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return storageDirectoryIdentity{}, errStorageCustody
	}
	return storageDirectoryIdentity{uint64(s.Dev), uint64(s.Ino)}, nil
}
