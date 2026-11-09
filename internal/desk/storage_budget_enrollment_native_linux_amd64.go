//go:build linux && amd64

package desk

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
	"golang.org/x/sys/unix"
)

func storageEnrollmentNativeSystemFacts(ctx context.Context) (storageEnrollmentNativeSystem, error) {
	var out storageEnrollmentNativeSystem
	if err := ctx.Err(); err != nil {
		return out, err
	}
	var u unix.Utsname
	if err := unix.Uname(&u); err != nil {
		return out, err
	}
	text := func(b []byte) string {
		for i, c := range b {
			if c == 0 {
				return string(b[:i])
			}
		}
		return string(b)
	}
	out.KernelRelease, out.KernelVersion = text(u.Release[:]), text(u.Version[:])
	return out, ctx.Err()
}

// The pinned syscall.Pwrite implementation calls the generated pwrite once;
// it does not use os.File.WriteAt/internal-poll's short/EINTR retry loop. The
// latter remains unchanged for Q's one original 4MiB API invocation.
func storageEnrollmentNativeWrite(ctx context.Context, f *os.File, data []byte, offset int64) error {
	if len(data) == 0 || len(data) > 65536 || offset < 0 || offset > math.MaxInt64-int64(len(data)) {
		return storagebudget.ErrBounds
	}
	return storageNativeControl(ctx, f, func(fd int) error {
		n, err := syscall.Pwrite(fd, data, offset)
		return storageEnrollmentNativeWriteResult(n, len(data), err)
	})
}

// Linux UAPI fs.h/fiemap.h: _IOWR('f',11,struct fiemap32), followed by
// fiemap_extent56 rows. A no-SYNC request is read-only. The finite 2048-row
// carrier covers worst 4KiB fragmentation of an existing 8MiB catalog bank.
func storageEnrollmentNativeDense(ctx context.Context, f *os.File, length uint64) error {
	count := storageEnrollmentDenseCount(length)
	if count == 0 {
		return storagebudget.ErrBounds
	}
	buf := make([]byte, 32+56*count)
	binary.LittleEndian.PutUint64(buf[8:16], length)
	binary.LittleEndian.PutUint32(buf[24:28], uint32(count))
	err := storageNativeControl(ctx, f, func(fd int) error {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), 0xc020660b, uintptr(unsafe.Pointer(&buf[0])))
		runtime.KeepAlive(buf)
		if errno != 0 {
			return errno
		}
		return nil
	})
	if err != nil {
		return err
	}
	return storageEnrollmentNativeDenseRecord(buf, length)
}

func storageEnrollmentNativeParentObservation(ctx context.Context, f *os.File) (storageEnrollmentNativeParentFacts, error) {
	stat, err := storageNativeStatfs(ctx, f)
	if err != nil {
		return storageEnrollmentNativeParentFacts{}, err
	}
	ext, err := storageNativeExt4(ctx, f)
	if err != nil {
		return storageEnrollmentNativeParentFacts{}, err
	}
	return storageEnrollmentNativeParentFacts{Backend: 1, Type: stat.Type, Flags: stat.Flags, BlockSize: stat.BlockSize, FragmentSize: stat.FragmentSize, ProjectFlags: ext.ParentXFlags}, nil
}

func storageEnrollmentNativeSync(ctx context.Context, f *os.File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.Sync()
}
