//go:build darwin && (arm64 || amd64)

package desk

import (
	"context"
	"math"
	"os"
	"syscall"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
	"golang.org/x/sys/unix"
)

// Sysctl reads actual system identity. No Linux ABI or advisory profile hash
// can qualify this route; exact source/build and native results remain required.
func storageEnrollmentNativeSystemFacts(ctx context.Context) (storageEnrollmentNativeSystem, error) {
	var out storageEnrollmentNativeSystem
	if err := ctx.Err(); err != nil {
		return out, err
	}
	var err error
	out.KernelRelease, err = unix.Sysctl("kern.osrelease")
	if err != nil {
		return storageEnrollmentNativeSystem{}, storageNativeDarwinError(err)
	}
	out.KernelVersion, err = unix.Sysctl("kern.version")
	if err != nil {
		return storageEnrollmentNativeSystem{}, storageNativeDarwinError(err)
	}
	return out, ctx.Err()
}

func storageEnrollmentNativeParentObservation(ctx context.Context, f *os.File) (storageEnrollmentNativeParentFacts, error) {
	stat, err := storageNativeStatfs(ctx, f)
	if err != nil {
		return storageEnrollmentNativeParentFacts{}, err
	}
	if stat.Type != 26 {
		return storageEnrollmentNativeParentFacts{}, storagebudget.ErrUnsupported
	}
	// Darwin Statfs has no f_frsize/project-flags field. Zero means absent,
	// not a fabricated fragment size or absence of other APFS constraints.
	return storageEnrollmentNativeParentFacts{Backend: 2, Type: stat.Type, Flags: stat.Flags, BlockSize: stat.BlockSize}, nil
}

// syscall.Pwrite invokes the generated pwrite once. A short, EINTR or other
// error closes this invocation; there is no retry or fallback to WriteAt.
func storageEnrollmentNativeWrite(ctx context.Context, f *os.File, data []byte, offset int64) error {
	if len(data) == 0 || len(data) > 65536 || offset < 0 || offset > math.MaxInt64-int64(len(data)) {
		return storagebudget.ErrBounds
	}
	return storageNativeControl(ctx, f, func(fd int) error {
		n, err := syscall.Pwrite(fd, data, offset)
		return storageEnrollmentNativeWriteResult(n, len(data), err)
	})
}

// Density is a legacy allocation-premise primitive. Scope3 does not request
// it, and APFS is never certified dense/unshared by numeric statfs fields.
func storageEnrollmentNativeDense(context.Context, *os.File, uint64) error {
	return storagebudget.ErrUnsupported
}

// Unlike os.File.Sync, this operation has no ENOTSUP-to-fsync fallback. A
// directory or device that cannot honor this exact command refuses; successful
// native durability still needs source/build and real failure/death evidence.
func storageEnrollmentNativeSync(ctx context.Context, f *os.File) error {
	return storageNativeControl(ctx, f, func(fd int) error {
		_, err := unix.FcntlInt(uintptr(fd), unix.F_FULLFSYNC, 0)
		return storageNativeDarwinError(err)
	})
}
