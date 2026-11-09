//go:build darwin

package desk

import (
	"context"
	"errors"
	"math"
	"os"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
	"golang.org/x/sys/unix"
)

func storageNativeDarwinError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.ENOTTY) {
		return errors.Join(storagebudget.ErrUnsupported, err)
	}
	return errors.Join(storagebudget.ErrUnknown, err)
}

func storageNativeDirectoryFD(fd int) error {
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return storageNativeDarwinError(err)
	}
	if flags&unix.O_EVTONLY != 0 {
		return storagebudget.ErrUnsupported
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return storageNativeDarwinError(err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return os.ErrInvalid
	}
	return nil
}

func storageNativeStatfs(ctx context.Context, f *os.File) (storageNativeStatfsRaw, error) {
	out := storageNativeStatfsRaw{Started: time.Now()}
	err := storageNativeControl(ctx, f, func(fd int) error {
		if err := storageNativeDirectoryFD(fd); err != nil {
			return err
		}
		var stat unix.Statfs_t
		if err := unix.Fstatfs(fd, &stat); err != nil {
			return storageNativeDarwinError(err)
		}
		out.Type, out.FSID = uint64(stat.Type), stat.Fsid.Val
		out.BlockSize = int64(stat.Bsize)
		out.Blocks, out.FreeBlocks, out.AvailableBlocks = stat.Blocks, stat.Bfree, stat.Bavail
		out.Files, out.FreeFiles, out.Flags = stat.Files, stat.Ffree, uint64(stat.Flags)
		return nil
	})
	if err != nil {
		return storageNativeStatfsRaw{}, err
	}
	return out, nil
}

// The normal descriptor is borrowed, never duplicated or reopened. Kind is only
// an observed type, not evidence of original named-owner custody.
func storageNativeObjectFD(fd int) (uint8, error) {
	if fd < 0 {
		return 0, os.ErrInvalid
	}
	if fd > math.MaxInt32 {
		return 0, storagebudget.ErrBounds
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return 0, storageNativeDarwinError(err)
	}
	if flags&unix.O_EVTONLY != 0 {
		return 0, storagebudget.ErrUnsupported
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return 0, storageNativeDarwinError(err)
	}
	return storageNativeObjectKind(flags, uint32(stat.Mode))
}

// A pure classifier permits handwritten type/flag fixtures without native probes.
func storageNativeObjectKind(flags int, mode uint32) (uint8, error) {
	if flags&unix.O_EVTONLY != 0 {
		return 0, storagebudget.ErrUnsupported
	}
	switch mode & unix.S_IFMT {
	case unix.S_IFDIR:
		return storageNativeObjectDirectory, nil
	case unix.S_IFREG:
		return storageNativeObjectRegular, nil
	default:
		return 0, os.ErrInvalid
	}
}
