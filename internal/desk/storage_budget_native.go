package desk

import (
	"context"
	"errors"
	"math"
	"os"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// These leaves return native facts only. They do not establish original custody,
// classify a backend's inode model, construct durable IDs/generations, discover
// complete quota membership, or return a healthy storageFilesystemSample.
type storageNativeStatfsRaw struct {
	Started                             time.Time
	Type                                uint64
	FSID                                [2]int32
	BlockSize, FragmentSize             int64
	Blocks, FreeBlocks, AvailableBlocks uint64
	Files, FreeFiles                    uint64
	Flags                               uint64
}

type storageNativeByteCounts struct{ Total, Available uint64 }

const (
	storageNativeMountOrdinary uint8 = 1
	storageNativeMountUnique   uint8 = 2
)

type storageNativeLinuxObjectRaw struct {
	Started                  time.Time
	ReturnedMask             uint32
	DeviceMajor, DeviceMinor uint32
	Inode                    uint64
	UID, GID                 uint32
	Mode                     uint16
	MountID                  uint64
	MountKind                uint8
}

type storageNativeExt4Raw struct {
	Started                       time.Time
	UUID                          [16]byte
	ParentProjectID, ParentXFlags uint32
}

// storageNativeBlockBytes applies an explicitly selected unit. Linux uses
// positive Frsize (Bsize only when Frsize is zero); Darwin uses Bsize, not Iosize.
// A successful conversion is not evidence that a count includes every quota or
// reserve, nor a physical allocation guarantee. Undefined sentinels are rejected
// before arithmetic. Valid zero availability means exhaustion.
func storageNativeBlockBytes(s storageNativeStatfsRaw, unit int64) (storageNativeByteCounts, error) {
	if unit <= 0 {
		return storageNativeByteCounts{}, storagebudget.ErrBounds
	}
	if s.Blocks == 0 || s.Blocks == math.MaxUint64 || s.FreeBlocks == math.MaxUint64 || s.AvailableBlocks == math.MaxUint64 {
		return storageNativeByteCounts{}, storagebudget.ErrUnknown
	}
	if s.FreeBlocks > s.Blocks || s.AvailableBlocks > s.FreeBlocks {
		return storageNativeByteCounts{}, storagebudget.ErrUnknown
	}
	u := uint64(unit)
	if s.Blocks > math.MaxUint64/u || s.AvailableBlocks > math.MaxUint64/u {
		return storageNativeByteCounts{}, storagebudget.ErrBounds
	}
	total, available := s.Blocks*u, s.AvailableBlocks*u
	if total > 1<<60 || available > 1<<60 {
		return storageNativeByteCounts{}, storagebudget.ErrBounds
	}
	return storageNativeByteCounts{Total: total, Available: available}, nil
}

func storageNativeLinuxBlockBytes(s storageNativeStatfsRaw) (storageNativeByteCounts, error) {
	unit := s.FragmentSize
	if unit == 0 {
		unit = s.BlockSize
	}
	return storageNativeBlockBytes(s, unit)
}

func storageNativeDarwinBlockBytes(s storageNativeStatfsRaw) (storageNativeByteCounts, error) {
	if s.BlockSize == math.MaxUint32 { // Darwin's Bsize field is uint32.
		return storageNativeByteCounts{}, storagebudget.ErrUnknown
	}
	return storageNativeBlockBytes(s, s.BlockSize)
}

// The caller must independently establish a finite inode inventory. In
// particular APFS sentinel/zero counts cannot select this interpretation or
// manufacture an InodesDynamic capability. A zero total does not establish a
// finite inventory from these raw counters alone.
func storageNativeFiniteInodes(s storageNativeStatfsRaw) (uint64, error) {
	if s.Files == 0 || s.Files == math.MaxUint64 || s.FreeFiles == math.MaxUint64 || s.FreeFiles > s.Files {
		return 0, storagebudget.ErrUnknown
	}
	if s.Files > 1<<40 || s.FreeFiles > 1<<40 {
		return 0, storagebudget.ErrBounds
	}
	return s.FreeFiles, nil
}

// The input is an already established finite byte limit, not a raw quota sentinel.
// Zero means a finite exhausted limit here; this helper never infers unlimited.
func storageNativeFiniteByteRemaining(limit, used uint64) (uint64, error) {
	if limit > 1<<60 || used > 1<<60 {
		return 0, storagebudget.ErrBounds
	}
	if used >= limit {
		return 0, nil
	}
	return limit - used, nil
}

// Control keeps the exact os.File description alive during native access. It
// borrows, never closes or reopens it. Each OS leaf additionally checks that it
// is a normal descriptor of its required kind inside Control. The owner must retain that
// description across comparisons; an ordinary mount ID is lifetime-scoped.
func storageNativeControl(ctx context.Context, f *os.File, query func(int) error) error {
	if ctx == nil || f == nil || query == nil {
		return os.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := f.SyscallConn()
	if err != nil {
		return errors.Join(storagebudget.ErrUnknown, err)
	}
	var queryErr error
	controlErr := raw.Control(func(fd uintptr) {
		if fd > uintptr(math.MaxInt) {
			queryErr = storagebudget.ErrBounds
			return
		}
		queryErr = query(int(fd))
	})
	if controlErr != nil {
		controlErr = errors.Join(storagebudget.ErrUnknown, controlErr)
	}
	// A context cannot interrupt an uninterruptible filesystem call. Checking
	// afterward prevents accepting a completed fact after cancellation.
	return errors.Join(controlErr, queryErr, ctx.Err())
}
