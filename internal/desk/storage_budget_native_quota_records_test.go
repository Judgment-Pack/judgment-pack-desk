package desk

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageNativeQuotaCommands(t *testing.T) {
	for _, tc := range []struct {
		op   storageNativeQuotaOp
		base uint32
		size int
	}{
		{storageNativeQuotaStatusOp, 0x580800, 160}, {storageNativeQuotaInfoOp, 0x80000500, 24}, {storageNativeQuotaDiskOp, 0x580300, 112},
	} {
		for kind := storageNativeQuotaKind(0); kind < 3; kind++ {
			command, size, err := storageNativeQuotaCommand(tc.op, kind)
			if err != nil || command != tc.base+uint32(kind) || size != tc.size {
				t.Fatalf("op=%d kind=%d: %x %d %v", tc.op, kind, command, size, err)
			}
		}
	}
	for _, tc := range []struct {
		op   storageNativeQuotaOp
		kind storageNativeQuotaKind
	}{{0, 0}, {4, 0}, {1, 3}} {
		if command, size, err := storageNativeQuotaCommand(tc.op, tc.kind); command != 0 || size != 0 || !errors.Is(err, storagebudget.ErrBounds) {
			t.Fatalf("bad command accepted: %x %d %v", command, size, err)
		}
	}
}

// Manually authored public fs_quota_statv version1 layout, not recorded output.
func storageNativeQuotaStatusFixture() []byte {
	p := make([]byte, 160)
	p[0] = 1
	p[2] = 0x3f
	binary.LittleEndian.PutUint32(p[4:8], 17)
	binary.LittleEndian.PutUint64(p[8:16], 101)
	binary.LittleEndian.PutUint64(p[16:24], 102)
	binary.LittleEndian.PutUint32(p[24:28], 103)
	binary.LittleEndian.PutUint64(p[32:40], 201)
	binary.LittleEndian.PutUint64(p[40:48], 202)
	binary.LittleEndian.PutUint32(p[48:52], 203)
	binary.LittleEndian.PutUint64(p[56:64], 301)
	binary.LittleEndian.PutUint64(p[64:72], 302)
	binary.LittleEndian.PutUint32(p[72:76], 303)
	binary.LittleEndian.PutUint32(p[80:84], 600)
	binary.LittleEndian.PutUint32(p[84:88], 601)
	binary.LittleEndian.PutUint32(p[88:92], 602)
	p[92], p[94], p[96] = 7, 8, 9
	return p
}

func TestStorageNativeQuotaStatusPacking(t *testing.T) {
	got, err := storageNativeQuotaStatusRecord(storageNativeQuotaStatusFixture(), storageNativeQuotaProject)
	if err != nil || got.RequestedKind != 2 || got.Version != 1 || got.Flags != 63 || got.InCore != 17 || got.Files[0] != (storageNativeQuotaFileRaw{101, 102, 103}) || got.Files[1] != (storageNativeQuotaFileRaw{201, 202, 203}) || got.Files[2] != (storageNativeQuotaFileRaw{301, 302, 303}) || got.GraceSeconds != [3]int32{600, 601, 602} || got.WarningLimits != [3]uint16{7, 8, 9} {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestStorageNativeQuotaStatusStates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags uint16
		kind  storageNativeQuotaKind
		want  uint8
		err   error
	}{
		{"user-enforced", 3, 0, storageNativeQuotaStateEnforced, nil},
		{"group-enforced", 12, 1, storageNativeQuotaStateEnforced, nil},
		{"project-enforced", 48, 2, storageNativeQuotaStateEnforced, nil},
		{"group-accounting-only", 4, 1, storageNativeQuotaStateAccounting, nil},
		{"group-disabled-user-active", 3, 1, storageNativeQuotaStateDisabled, nil},
		{"project-disabled-group-active", 12, 2, storageNativeQuotaStateDisabled, nil},
		{"zero-status-not-proof", 0, 0, 0, storagebudget.ErrUnknown},
		{"enforcement-without-accounting", 2, 0, 0, storagebudget.ErrUnknown},
		{"other-type-inconsistent", 0x21, 0, 0, storagebudget.ErrUnknown},
		{"unknown-flags", 0x43, 0, 0, storagebudget.ErrUnsupported},
		{"unknown-type", 3, 3, 0, storagebudget.ErrBounds},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (storageNativeQuotaStatusRaw{Version: 1, Flags: tc.flags, RequestedKind: tc.kind}).state()
			if got != tc.want || !errors.Is(err, tc.err) {
				t.Fatalf("%d %v", got, err)
			}
		})
	}
}

func TestStorageNativeQuotaStatusRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func([]byte) []byte
		err    error
	}{
		{"short", func(p []byte) []byte { return p[:159] }, storagebudget.ErrBounds},
		{"extra", func(p []byte) []byte { return append(p, 0) }, storagebudget.ErrBounds},
		{"version", func(p []byte) []byte { p[0] = 2; return p }, storagebudget.ErrUnknown},
		{"header-pad", func(p []byte) []byte { p[1] = 1; return p }, storagebudget.ErrUnknown},
		{"file-pad", func(p []byte) []byte { p[52] = 1; return p }, storagebudget.ErrUnknown},
		{"tail-pad", func(p []byte) []byte { p[159] = 1; return p }, storagebudget.ErrUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := storageNativeQuotaStatusRecord(tc.mutate(storageNativeQuotaStatusFixture()), 0)
			if got != (storageNativeQuotaStatusRaw{}) || !errors.Is(err, tc.err) {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func storageNativeQuotaInfoFixture() []byte {
	p := make([]byte, 24)
	binary.LittleEndian.PutUint64(p[:8], 604800)
	binary.LittleEndian.PutUint64(p[8:16], 3600)
	binary.LittleEndian.PutUint32(p[16:20], 0x10001)
	p[20] = 7
	return p
}

func TestStorageNativeQuotaInfoValidity(t *testing.T) {
	got, err := storageNativeQuotaInfoRecord(storageNativeQuotaInfoFixture(), 1)
	if err != nil || got.RequestedKind != 1 || got.BlockGraceSeconds != 604800 || got.InodeGraceSeconds != 3600 || got.Flags != 0x10001 || got.Valid != 7 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, mask := range []uint32{0, 1, 3, 6, 15} {
		p := storageNativeQuotaInfoFixture()
		binary.LittleEndian.PutUint32(p[20:24], mask)
		got, err := storageNativeQuotaInfoRecord(p, 0)
		if got != (storageNativeQuotaInfoRaw{}) || !errors.Is(err, storagebudget.ErrUnsupported) {
			t.Fatalf("valid=%x %+v %v", mask, got, err)
		}
	}
	p := storageNativeQuotaInfoFixture()
	p[17] = 1
	if _, err := storageNativeQuotaInfoRecord(p, 0); !errors.Is(err, storagebudget.ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := storageNativeQuotaInfoRecord(p[:23], 0); !errors.Is(err, storagebudget.ErrBounds) {
		t.Fatal(err)
	}
}

func storageNativeQuotaDiskFixture() []byte {
	p := make([]byte, 112)
	p[0], p[1] = 1, 1
	binary.LittleEndian.PutUint32(p[4:8], 42)
	for i, value := range []uint64{8, 6, 20, 18, 2, 3} {
		binary.LittleEndian.PutUint64(p[8+8*i:16+8*i], value)
	}
	binary.LittleEndian.PutUint32(p[56:60], 1234)
	binary.LittleEndian.PutUint32(p[60:64], 2345)
	p[64], p[66] = 4, 5
	return p
}

func TestStorageNativeQuotaDiskPacking(t *testing.T) {
	for _, tc := range []struct {
		kind storageNativeQuotaKind
		flag byte
	}{{0, 1}, {1, 4}, {2, 2}} {
		p := storageNativeQuotaDiskFixture()
		p[1] = tc.flag
		got, err := storageNativeQuotaDiskRecord(p, storageNativeQuotaSubject{tc.kind, 42})
		if err != nil || got.Subject != (storageNativeQuotaSubject{tc.kind, 42}) || got.Version != 1 || got.Flags != tc.flag || got.FieldMask != 0 || got.BlockUnit != 512 || got.HardBlocks != 8 || got.SoftBlocks != 6 || got.HardInodes != 20 || got.SoftInodes != 18 || got.UsedBlocks != 2 || got.UsedInodes != 3 || got.InodeExpiry != 1234 || got.BlockExpiry != 2345 || got.InodeWarnings != 4 || got.BlockWarnings != 5 {
			t.Fatalf("%+v %v", got, err)
		}
	}
	// The setter field mask must not be required as a GET validity bitmap.
	p := storageNativeQuotaDiskFixture()
	binary.LittleEndian.PutUint16(p[2:4], 0x1000)
	if got, err := storageNativeQuotaDiskRecord(p, storageNativeQuotaSubject{0, 42}); err != nil || got.FieldMask != 0x1000 {
		t.Fatalf("mask lost: %+v %v", got, err)
	}
}

func TestStorageNativeQuotaExpiryPacking(t *testing.T) {
	for _, tc := range []struct {
		name string
		mask uint16
		low  uint32
		high byte
		want int64
	}{
		{"signed32", 0, 0xffffffff, 0, -1},
		{"positive40", 0x8000, 17, 1, 4294967313},
		{"negative40", 0x8000, 17, 255, -4294967279},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := storageNativeQuotaDiskFixture()
			binary.LittleEndian.PutUint16(p[2:4], tc.mask)
			binary.LittleEndian.PutUint32(p[56:60], tc.low)
			p[68] = tc.high
			got, err := storageNativeQuotaDiskRecord(p, storageNativeQuotaSubject{0, 42})
			if err != nil || got.InodeExpiry != tc.want || got.FieldMask != tc.mask {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func TestStorageNativeQuotaDiskRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func([]byte) []byte
		err    error
	}{
		{"short", func(p []byte) []byte { return p[:111] }, storagebudget.ErrBounds},
		{"extra", func(p []byte) []byte { return append(p, 0) }, storagebudget.ErrBounds},
		{"version", func(p []byte) []byte { p[0] = 2; return p }, storagebudget.ErrUnsupported},
		{"wrong-subject-type", func(p []byte) []byte { p[1] = 4; return p }, storagebudget.ErrUnknown},
		{"wrong-subject-id", func(p []byte) []byte { p[4] = 43; return p }, storagebudget.ErrUnknown},
		{"non-bigtime-high", func(p []byte) []byte { p[68] = 1; return p }, storagebudget.ErrUnknown},
		{"pad2", func(p []byte) []byte { p[71] = 1; return p }, storagebudget.ErrUnknown},
		{"pad3", func(p []byte) []byte { p[102] = 1; return p }, storagebudget.ErrUnknown},
		{"tail", func(p []byte) []byte { p[111] = 1; return p }, storagebudget.ErrUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := storageNativeQuotaDiskRecord(tc.mutate(storageNativeQuotaDiskFixture()), storageNativeQuotaSubject{0, 42})
			if got != (storageNativeQuotaDiskRaw{}) || !errors.Is(err, tc.err) {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}
