//go:build darwin

package storagebudget

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// One checked full-sync operation at the original Sync boundary. In particular,
// ENOTSUP is returned to the existing uncertain-outcome path; it cannot silently
// become a weaker fsync success. This changes no WriteAt, Close or readback call.
func syncFile(f *os.File) error {
	if f == nil {
		return os.ErrInvalid
	}
	raw, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var syncErr error
	err = raw.Control(func(fd uintptr) {
		_, syncErr = unix.FcntlInt(fd, unix.F_FULLFSYNC, 0)
	})
	return errors.Join(err, syncErr)
}
