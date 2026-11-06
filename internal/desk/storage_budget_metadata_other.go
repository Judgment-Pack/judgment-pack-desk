//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package desk

import "os"

func storageFileIdentity(os.FileInfo) (storageDirectoryIdentity, error) {
	return storageDirectoryIdentity{}, errStorageCustody
}

func storageReportedAllocation(os.FileInfo) (uint64, bool) { return 0, false }
