//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package desk

import (
	"errors"
	"os"
)

func lockPrivateData(*os.File, bool) error {
	return errors.New("this platform does not support the Desk's private-data lock")
}
