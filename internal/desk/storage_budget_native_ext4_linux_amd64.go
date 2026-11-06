//go:build linux && amd64

package desk

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"runtime"
	"time"
	"unsafe"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
	"golang.org/x/sys/unix"
)

// Public Linux x86-64 UAPI: _IOR('f',44,struct fsuuid) uses the 8-byte
// flexible-array header, NOT the 24-byte supplied storage. FSGETXATTR uses
// _IOR('X',31,struct fsxattr), whose public layout is 28 bytes. This wrapper is
// architecture-specific so another architecture cannot inherit guessed encoding.
const (
	storageNativeExt4GetUUID = 0x8008662c
	storageNativeFSGetXAttr  = 0x801c581f
	// _IOR('v',1,long) encodes size8 on amd64; ext4 writes only int4.
	storageNativeFSGetVersion = 0x80087601
)

func storageNativeExt4(ctx context.Context, f *os.File) (storageNativeExt4Raw, error) {
	out := storageNativeExt4Raw{Started: time.Now()}
	err := storageNativeControl(ctx, f, func(fd int) error {
		if err := storageNativeDirectoryFD(fd); err != nil {
			return err
		}
		var stat unix.Statfs_t
		if err := unix.Fstatfs(fd, &stat); err != nil {
			return storageNativeLinuxError(err)
		}
		if stat.Type != unix.EXT4_SUPER_MAGIC {
			return storagebudget.ErrUnsupported
		}
		var uuid [24]byte
		binary.LittleEndian.PutUint32(uuid[:4], 16)
		if err := storageNativeLinuxIOCTL(fd, storageNativeExt4GetUUID, uuid[:]); err != nil {
			return err
		}
		parsed, err := storageNativeExt4UUIDRecord(uuid[:])
		if err != nil {
			return err
		}
		var attrs [28]byte
		if err := storageNativeLinuxIOCTL(fd, storageNativeFSGetXAttr, attrs[:]); err != nil {
			return err
		}
		project, flags, err := storageNativeExt4ProjectRecord(attrs[:])
		if err != nil {
			return err
		}
		out.UUID = parsed
		out.ParentXFlags, out.ParentProjectID = flags, project
		// These are prospective-child INPUTS only. Feature state, inheritance,
		// credentials, idmapping and quota enforcement must be qualified by the
		// future binding. No child membership or effective quota is asserted.
		return nil
	})
	if err != nil {
		return storageNativeExt4Raw{}, err
	}
	return out, nil
}

func storageNativeLinuxIOCTL(fd int, request uintptr, p []byte) error {
	if !(request == storageNativeExt4GetUUID && len(p) == 24 || request == storageNativeFSGetXAttr && len(p) == 28 || request == storageNativeFSGetVersion && len(p) == 8) {
		return storagebudget.ErrBounds
	}
	if fd < 0 {
		return os.ErrInvalid
	}
	if fd > math.MaxInt32 {
		return storagebudget.ErrBounds
	}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request, uintptr(unsafe.Pointer(&p[0])))
	runtime.KeepAlive(p)
	if errno != 0 {
		return storageNativeLinuxError(errno)
	}
	return nil
}

func storageNativeExt4UUIDRecord(p []byte) ([16]byte, error) {
	var uuid [16]byte
	if len(p) != 24 || binary.LittleEndian.Uint32(p[:4]) != 16 || binary.LittleEndian.Uint32(p[4:8]) != 0 {
		return uuid, storagebudget.ErrBounds
	}
	copy(uuid[:], p[8:24])
	if uuid == [16]byte{} {
		return uuid, storagebudget.ErrUnknown
	}
	return uuid, nil
}

func storageNativeExt4ProjectRecord(p []byte) (project, flags uint32, err error) {
	if len(p) != 28 {
		return 0, 0, storagebudget.ErrBounds
	}
	for _, value := range p[20:28] { // reserved fsx_pad, zeroed by the public getter.
		if value != 0 {
			return 0, 0, storagebudget.ErrUnknown
		}
	}
	return binary.LittleEndian.Uint32(p[12:16]), binary.LittleEndian.Uint32(p[:4]), nil
}

func storageNativeExt4Object(ctx context.Context, f *os.File) (storageNativeExt4ObjectRaw, error) {
	var out storageNativeExt4ObjectRaw
	started := time.Now()
	err := storageNativeControl(ctx, f, func(fd int) error {
		kind, err := storageNativeObjectFD(fd)
		if err != nil {
			return err
		}
		var stat unix.Statfs_t
		if err := unix.Fstatfs(fd, &stat); err != nil {
			return storageNativeLinuxError(err)
		}
		if stat.Type != unix.EXT4_SUPER_MAGIC {
			return storagebudget.ErrUnsupported
		}
		out, err = storageNativeReadExt4Object(fd, kind, storageNativeLinuxIOCTL)
		return err
	})
	if err != nil {
		return storageNativeExt4ObjectRaw{}, err
	}
	out.Started = started
	return out, nil
}

// Explicit callback leaf; production supplies only the fixed read-only ioctl.
func storageNativeReadExt4Object(fd int, kind uint8, query func(int, uintptr, []byte) error) (storageNativeExt4ObjectRaw, error) {
	var zero storageNativeExt4ObjectRaw
	if fd < 0 || query == nil || (kind != storageNativeObjectDirectory && kind != storageNativeObjectRegular) {
		return zero, os.ErrInvalid
	}
	if fd > math.MaxInt32 {
		return zero, storagebudget.ErrBounds
	}
	var uuid [24]byte
	binary.LittleEndian.PutUint32(uuid[:4], 16)
	if err := query(fd, storageNativeExt4GetUUID, uuid[:]); err != nil {
		return zero, storageNativeLinuxError(err)
	}
	parsed, err := storageNativeExt4UUIDRecord(uuid[:])
	if err != nil {
		return zero, err
	}
	var attrs [28]byte
	if err := query(fd, storageNativeFSGetXAttr, attrs[:]); err != nil {
		return zero, storageNativeLinuxError(err)
	}
	project, flags, err := storageNativeExt4ProjectRecord(attrs[:])
	if err != nil {
		return zero, err
	}
	var generation [8]byte
	if err := query(fd, storageNativeFSGetVersion, generation[:]); err != nil {
		return zero, storageNativeLinuxError(err)
	}
	incarnation, err := storageNativeExt4GenerationRecord(generation[:])
	if err != nil {
		return zero, err
	}
	return storageNativeExt4ObjectRaw{Kind: kind, UUID: parsed, ProjectID: project, XFlags: flags, InodeGeneration: incarnation}, nil
}

// The upper4 bytes are unused capacity, not a returned field or sentinel.
// Every uint32 bit pattern, including zero and all-ones, is a raw value.
func storageNativeExt4GenerationRecord(p []byte) (uint32, error) {
	if len(p) != 8 {
		return 0, storagebudget.ErrBounds
	}
	return binary.LittleEndian.Uint32(p[:4]), nil
}
