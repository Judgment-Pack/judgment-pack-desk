package desk

import (
	"encoding/binary"
	"math"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Public attr.h values, checked against the SDK by the cgo bridge. Keeping the
// fixed record parser portable permits handwritten ABI tests without macOS or
// an installed program. Both supported Darwin architectures are little endian.
const (
	storageNativeReturnedAttrs          uint32 = 0x80000000
	storageNativeVolumeMask             uint32 = 0x00000004 | 0x00000008 | 0x00000010 | 0x00800000 | 0x00020000 | 0x00040000 | 0x10000000 | 0x20000000
	storageNativeFileIDMask             uint32 = 0x02000000
	storageNativeObjectTypeMask         uint32 = 0x8
	storageNativeTypedObjectRecordBytes        = 56
	storageNativeExtendedMask           uint32 = 0x10 | 0x40 | 0x80
	storageNativeVolumeRecordBytes             = 120
	storageNativeObjectRecordBytes             = 52
)

type storageNativeDarwinVolumeRaw struct {
	Started                                     time.Time
	UUID                                        [16]byte
	Size, Free, Available, Used, Quota, Reserve uint64
	Capabilities, ValidCapabilities             [4]uint32
}

type storageNativeDarwinObjectRaw struct {
	Started        time.Time
	FileID, LinkID uint64
	RealDevice     uint32
	RealFSID       [2]int32
}

// Exact requested, supported payload masks are mandatory. ATTR_VOL_INFO requests
// volume mode but has no payload and is not set in XNU's returned actual mask.
// PACK_INVAL_ATTRS keeps offsets fixed; its default-filled invalid fields still
// fail these masks. No unsupported field becomes a valid zero.
func storageNativeDarwinRecordHeader(p []byte, size int, expected [5]uint32) error {
	if len(p) != size || len(p) < 24 || binary.LittleEndian.Uint32(p[:4]) != uint32(size) {
		return storagebudget.ErrBounds
	}
	for i, mask := range expected {
		if binary.LittleEndian.Uint32(p[4+4*i:8+4*i]) != mask {
			return storagebudget.ErrUnsupported
		}
	}
	return nil
}

func storageNativeDarwinVolumeRecord(p []byte) (storageNativeDarwinVolumeRaw, error) {
	var out storageNativeDarwinVolumeRaw
	if err := storageNativeDarwinRecordHeader(p, storageNativeVolumeRecordBytes, [5]uint32{storageNativeReturnedAttrs, storageNativeVolumeMask}); err != nil {
		return out, err
	}
	// API packing order is Size/Free/Available/Used, capabilities, UUID,
	// quota/reserve. It is not simply numeric ordering of the attribute bits.
	fields := []*uint64{&out.Size, &out.Free, &out.Available, &out.Used, &out.Quota, &out.Reserve}
	for i, offset := range [...]int{24, 32, 40, 48, 104, 112} {
		value := binary.LittleEndian.Uint64(p[offset : offset+8])
		if value == math.MaxUint64 {
			return storageNativeDarwinVolumeRaw{}, storagebudget.ErrUnknown
		}
		if value > math.MaxInt64 { // off_t is signed, including other negative values.
			return storageNativeDarwinVolumeRaw{}, storagebudget.ErrBounds
		}
		*fields[i] = value
	}
	for i := range out.Capabilities {
		out.Capabilities[i] = binary.LittleEndian.Uint32(p[56+4*i : 60+4*i])
		out.ValidCapabilities[i] = binary.LittleEndian.Uint32(p[72+4*i : 76+4*i])
	}
	copy(out.UUID[:], p[88:104])
	if out.UUID == [16]byte{} {
		return storageNativeDarwinVolumeRaw{}, storagebudget.ErrUnknown
	}
	// Space sharing prevents inferring Used=Size-Free. Returned masks also
	// cannot distinguish native from VFS-synthesized Used. Quota zero semantics
	// and effective quota/container allowance remain unqualified raw facts.
	return out, nil
}

func storageNativeDarwinObjectRecord(p []byte) (storageNativeDarwinObjectRaw, error) {
	var out storageNativeDarwinObjectRaw
	if err := storageNativeDarwinRecordHeader(p, storageNativeObjectRecordBytes, [5]uint32{storageNativeReturnedAttrs | storageNativeFileIDMask, 0, 0, 0, storageNativeExtendedMask}); err != nil {
		return out, err
	}
	out.FileID = binary.LittleEndian.Uint64(p[24:32])
	out.LinkID = binary.LittleEndian.Uint64(p[32:40])
	out.RealDevice = binary.LittleEndian.Uint32(p[40:44])
	// The 64-bit fsid begins at a four-byte, not eight-byte, boundary.
	out.RealFSID[0] = int32(binary.LittleEndian.Uint32(p[44:48]))
	out.RealFSID[1] = int32(binary.LittleEndian.Uint32(p[48:52]))
	if out.FileID == 0 || out.FileID == math.MaxUint64 || out.LinkID == 0 || out.LinkID == math.MaxUint64 {
		return storageNativeDarwinObjectRaw{}, storagebudget.ErrUnknown
	}
	return out, nil
}

type storageNativeDarwinTypedObjectRaw struct {
	Started        time.Time
	Kind           uint8
	VnodeType      uint32
	FileID, LinkID uint64
	RealDevice     uint32
	RealFSID       [2]int32
}

// Four-byte ABI packing deliberately leaves the two IDs unaligned at28/36.
func storageNativeDarwinTypedObjectRecord(p []byte) (storageNativeDarwinTypedObjectRaw, error) {
	var out storageNativeDarwinTypedObjectRaw
	if err := storageNativeDarwinRecordHeader(p, storageNativeTypedObjectRecordBytes, [5]uint32{storageNativeReturnedAttrs | storageNativeObjectTypeMask | storageNativeFileIDMask, 0, 0, 0, storageNativeExtendedMask}); err != nil {
		return out, err
	}
	out.VnodeType = binary.LittleEndian.Uint32(p[24:28])
	switch out.VnodeType {
	case 1:
		out.Kind = storageNativeObjectRegular // public VREG
	case 2:
		out.Kind = storageNativeObjectDirectory // public VDIR
	default:
		return storageNativeDarwinTypedObjectRaw{}, storagebudget.ErrUnknown
	}
	out.FileID = binary.LittleEndian.Uint64(p[28:36])
	out.LinkID = binary.LittleEndian.Uint64(p[36:44])
	out.RealDevice = binary.LittleEndian.Uint32(p[44:48])
	out.RealFSID[0] = int32(binary.LittleEndian.Uint32(p[48:52]))
	out.RealFSID[1] = int32(binary.LittleEndian.Uint32(p[52:56]))
	if out.FileID == 0 || out.FileID == math.MaxUint64 || out.LinkID == 0 || out.LinkID == math.MaxUint64 {
		return storageNativeDarwinTypedObjectRaw{}, storagebudget.ErrUnknown
	}
	return out, nil
}
