//go:build linux

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

func storageNativeLinuxError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOTTY) || errors.Is(err, unix.EOPNOTSUPP) {
		return errors.Join(storagebudget.ErrUnsupported, err)
	}
	// Preserve EACCES/EPERM (errors.Is(err, os.ErrPermission)), rather than
	// interpreting denied measurement as no quota or no restriction.
	return errors.Join(storagebudget.ErrUnknown, err)
}

func storageNativeDirectoryFD(fd int) error {
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return storageNativeLinuxError(err)
	}
	if flags&unix.O_PATH != 0 {
		return storagebudget.ErrUnsupported
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return storageNativeLinuxError(err)
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
			return storageNativeLinuxError(err)
		}
		out.Type, out.FSID = uint64(stat.Type), stat.Fsid.Val
		out.BlockSize, out.FragmentSize = int64(stat.Bsize), int64(stat.Frsize)
		out.Blocks, out.FreeBlocks, out.AvailableBlocks = stat.Blocks, stat.Bfree, stat.Bavail
		out.Files, out.FreeFiles, out.Flags = stat.Files, stat.Ffree, uint64(stat.Flags)
		return nil
	})
	if err != nil {
		return storageNativeStatfsRaw{}, err
	}
	return out, nil
}

// This is a mount-view observation, not durable filesystem/pool identity. The
// ordinary mode is valid for comparisons only while the original normal file
// description remains continuously held. No close/reopen can retain its proof.
func storageNativeLinuxObject(ctx context.Context, f *os.File) (storageNativeLinuxObjectRaw, error) {
	var out storageNativeLinuxObjectRaw
	started := time.Now()
	err := storageNativeControl(ctx, f, func(fd int) error {
		if err := storageNativeDirectoryFD(fd); err != nil {
			return err
		}
		var err error
		out, err = storageNativeReadLinuxObject(fd, func(fd, mask int, stat *unix.Statx_t) error {
			return unix.Statx(fd, "", unix.AT_EMPTY_PATH, mask, stat)
		})
		return err
	})
	if err != nil {
		return storageNativeLinuxObjectRaw{}, err
	}
	out.Started = started
	return out, nil
}

// The callback is an explicit leaf argument, not a replaceable global backend.
// Tests supply handwritten syscall responses; production supplies only Statx.
func storageNativeReadLinuxObject(fd int, query func(int, int, *unix.Statx_t) error) (storageNativeLinuxObjectRaw, error) {
	return storageNativeReadLinuxObjectKind(fd, storageNativeObjectDirectory, query)
}

func storageNativeReadLinuxObjectKind(fd int, kind uint8, query func(int, int, *unix.Statx_t) error) (storageNativeLinuxObjectRaw, error) {
	if fd < 0 || query == nil || (kind != storageNativeObjectDirectory && kind != storageNativeObjectRegular) {
		return storageNativeLinuxObjectRaw{}, os.ErrInvalid
	}
	if fd > math.MaxInt32 {
		return storageNativeLinuxObjectRaw{}, storagebudget.ErrBounds
	}
	const required = unix.STATX_TYPE | unix.STATX_MODE | unix.STATX_INO | unix.STATX_UID | unix.STATX_GID
	var stat unix.Statx_t
	err := query(fd, required|unix.STATX_MNT_ID_UNIQUE, &stat)
	mountKind := storageNativeMountUnique
	if errors.Is(err, unix.EINVAL) || err == nil && stat.Mask&unix.STATX_MNT_ID_UNIQUE == 0 {
		// Older kernels ignore unknown request bits or may reject them. Retry
		// once for the public ordinary ID, on the very same retained FD.
		stat = unix.Statx_t{}
		err = query(fd, required|unix.STATX_MNT_ID, &stat)
		mountKind = storageNativeMountOrdinary
	}
	if err != nil {
		return storageNativeLinuxObjectRaw{}, storageNativeLinuxError(err)
	}
	mountMask := uint32(unix.STATX_MNT_ID_UNIQUE)
	if mountKind == storageNativeMountOrdinary {
		mountMask = unix.STATX_MNT_ID
	}
	if stat.Mask&uint32(required) != uint32(required) || stat.Mask&mountMask == 0 {
		return storageNativeLinuxObjectRaw{}, storagebudget.ErrUnsupported
	}
	expectedType := uint16(unix.S_IFDIR)
	if kind == storageNativeObjectRegular {
		expectedType = unix.S_IFREG
	}
	if stat.Mnt_id == 0 || stat.Mode&unix.S_IFMT != expectedType {
		return storageNativeLinuxObjectRaw{}, storagebudget.ErrUnknown
	}
	return storageNativeLinuxObjectRaw{ReturnedMask: stat.Mask, DeviceMajor: stat.Dev_major, DeviceMinor: stat.Dev_minor, Inode: stat.Ino, UID: stat.Uid, GID: stat.Gid, Mode: stat.Mode, MountID: stat.Mnt_id, MountKind: mountKind}, nil
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
		return 0, storageNativeLinuxError(err)
	}
	if flags&unix.O_PATH != 0 {
		return 0, storagebudget.ErrUnsupported
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return 0, storageNativeLinuxError(err)
	}
	return storageNativeObjectKind(flags, uint32(stat.Mode))
}

// A pure classifier permits handwritten type/flag fixtures without native probes.
func storageNativeObjectKind(flags int, mode uint32) (uint8, error) {
	if flags&unix.O_PATH != 0 {
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

func storageNativeLinuxObjectMetadata(ctx context.Context, f *os.File) (storageNativeLinuxObjectRaw, error) {
	var out storageNativeLinuxObjectRaw
	started := time.Now()
	err := storageNativeControl(ctx, f, func(fd int) error {
		kind, err := storageNativeObjectFD(fd)
		if err != nil {
			return err
		}
		out, err = storageNativeReadLinuxObjectKind(fd, kind, func(fd, mask int, stat *unix.Statx_t) error {
			return unix.Statx(fd, "", unix.AT_EMPTY_PATH, mask, stat)
		})
		return err
	})
	if err != nil {
		return storageNativeLinuxObjectRaw{}, err
	}
	out.Started = started
	return out, nil
}
