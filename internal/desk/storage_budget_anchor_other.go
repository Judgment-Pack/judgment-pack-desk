//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package desk

import "os"

func storageDirectoryID(*os.Root) (storageDirectoryIdentity, error) {
	return storageDirectoryIdentity{}, errStorageCustody
}
func storageUnlock(*os.File) error { return errStorageCustody }
