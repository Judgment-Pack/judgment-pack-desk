package desk

import (
	"encoding/binary"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Linux amd64 public UAPI layouts. Parsers are portable so their handwritten
// tests never query host quotas. No layout is reused as an unverified native ABI.
const (
	storageNativeQuotaStatusBytes = 160
	storageNativeQuotaInfoBytes   = 24
	storageNativeQuotaDiskBytes   = 112
)

func storageNativeQuotaStatusRecord(p []byte, kind storageNativeQuotaKind) (storageNativeQuotaStatusRaw, error) {
	var out storageNativeQuotaStatusRaw
	if len(p) != storageNativeQuotaStatusBytes {
		return out, storagebudget.ErrBounds
	}
	out.RequestedKind, out.Version = kind, p[0]
	out.Flags, out.InCore = binary.LittleEndian.Uint16(p[2:4]), binary.LittleEndian.Uint32(p[4:8])
	if _, err := out.state(); err != nil {
		return storageNativeQuotaStatusRaw{}, err
	}
	for i, offset := range [...]int{8, 32, 56} {
		out.Files[i] = storageNativeQuotaFileRaw{Inode: binary.LittleEndian.Uint64(p[offset : offset+8]), Blocks512: binary.LittleEndian.Uint64(p[offset+8 : offset+16]), Extents: binary.LittleEndian.Uint32(p[offset+16 : offset+20])}
		if !storageNativeQuotaZero(p[offset+20 : offset+24]) {
			return storageNativeQuotaStatusRaw{}, storagebudget.ErrUnknown
		}
	}
	for i := range out.GraceSeconds {
		out.GraceSeconds[i] = int32(binary.LittleEndian.Uint32(p[80+4*i : 84+4*i]))
		out.WarningLimits[i] = binary.LittleEndian.Uint16(p[92+2*i : 94+2*i])
	}
	if p[1] != 0 || !storageNativeQuotaZero(p[98:160]) {
		return storageNativeQuotaStatusRaw{}, storagebudget.ErrUnknown
	}
	return out, nil
}

func storageNativeQuotaInfoRecord(p []byte, kind storageNativeQuotaKind) (storageNativeQuotaInfoRaw, error) {
	var out storageNativeQuotaInfoRaw
	if _, err := storageNativeQuotaTypeFlag(kind); err != nil {
		return out, err
	}
	if len(p) != storageNativeQuotaInfoBytes {
		return out, storagebudget.ErrBounds
	}
	out.RequestedKind = kind
	out.BlockGraceSeconds, out.InodeGraceSeconds = binary.LittleEndian.Uint64(p[:8]), binary.LittleEndian.Uint64(p[8:16])
	out.Flags, out.Valid = binary.LittleEndian.Uint32(p[16:20]), binary.LittleEndian.Uint32(p[20:24])
	if out.Valid != 7 || out.Flags&^uint32(0x10001) != 0 {
		return storageNativeQuotaInfoRaw{}, storagebudget.ErrUnsupported
	}
	return out, nil
}

func storageNativeQuotaDiskRecord(p []byte, subject storageNativeQuotaSubject) (storageNativeQuotaDiskRaw, error) {
	var out storageNativeQuotaDiskRaw
	flag, err := storageNativeQuotaTypeFlag(subject.Kind)
	if err != nil {
		return out, err
	}
	if len(p) != storageNativeQuotaDiskBytes {
		return out, storagebudget.ErrBounds
	}
	if p[0] != 1 {
		return out, storagebudget.ErrUnsupported
	}
	if p[1] != flag || binary.LittleEndian.Uint32(p[4:8]) != subject.ID {
		return out, storagebudget.ErrUnknown
	}
	out.Subject, out.Version, out.Flags = subject, p[0], p[1]
	out.FieldMask, out.BlockUnit = binary.LittleEndian.Uint16(p[2:4]), 512
	fields := []*uint64{&out.HardBlocks, &out.SoftBlocks, &out.HardInodes, &out.SoftInodes, &out.UsedBlocks, &out.UsedInodes}
	for i, field := range fields {
		*field = binary.LittleEndian.Uint64(p[8+8*i : 16+8*i])
	}
	out.InodeWarnings, out.BlockWarnings = binary.LittleEndian.Uint16(p[64:66]), binary.LittleEndian.Uint16(p[66:68])
	out.InodeExpiry = storageNativeQuotaExpiry(binary.LittleEndian.Uint32(p[56:60]), p[68], out.FieldMask)
	out.BlockExpiry = storageNativeQuotaExpiry(binary.LittleEndian.Uint32(p[60:64]), p[69], out.FieldMask)
	out.RealtimeHardBlocks, out.RealtimeSoftBlocks, out.RealtimeUsedBlocks = binary.LittleEndian.Uint64(p[72:80]), binary.LittleEndian.Uint64(p[80:88]), binary.LittleEndian.Uint64(p[88:96])
	out.RealtimeExpiry = storageNativeQuotaExpiry(binary.LittleEndian.Uint32(p[96:100]), p[70], out.FieldMask)
	out.RealtimeWarnings = binary.LittleEndian.Uint16(p[100:102])
	if p[71] != 0 || !storageNativeQuotaZero(p[102:112]) || out.FieldMask&0x8000 == 0 && !storageNativeQuotaZero(p[68:71]) {
		return storageNativeQuotaDiskRaw{}, storagebudget.ErrUnknown
	}
	return out, nil
}

func storageNativeQuotaExpiry(low uint32, high byte, mask uint16) int64 {
	if mask&0x8000 != 0 {
		return int64(int8(high))<<32 | int64(low)
	}
	return int64(int32(low))
}

func storageNativeQuotaZero(p []byte) bool {
	for _, value := range p {
		if value != 0 {
			return false
		}
	}
	return true
}
