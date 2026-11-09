//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package storagebudget

import "os"

const noFollow = 0
const nonBlocking = 0

func privateInfo(os.FileInfo, bool) bool { return false }
func flock(*os.File, bool) error         { return ErrUnsupported }
func unflock(*os.File) error             { return ErrUnsupported }
func lockBusy(error) bool                { return false }
