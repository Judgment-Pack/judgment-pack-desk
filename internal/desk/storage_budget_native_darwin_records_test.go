package desk

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Handwritten public-ABI fixture, not bytes recorded from the implementation.
func storageNativeVolumeFixture() []byte {
	p := make([]byte, 120)
	binary.LittleEndian.PutUint32(p[0:4], 120)
	binary.LittleEndian.PutUint32(p[4:8], 0x80000000)
	binary.LittleEndian.PutUint32(p[8:12], 0x3086001c)
	binary.LittleEndian.PutUint64(p[24:32], 10000)
	binary.LittleEndian.PutUint64(p[32:40], 3000)
	binary.LittleEndian.PutUint64(p[40:48], 2000)
	binary.LittleEndian.PutUint64(p[48:56], 4000) // intentionally != Size-Free
	binary.LittleEndian.PutUint32(p[56:60], 0x00800001)
	binary.LittleEndian.PutUint32(p[72:76], 0x00820001)
	copy(p[88:104], []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	binary.LittleEndian.PutUint64(p[104:112], 6000)
	binary.LittleEndian.PutUint64(p[112:120], 1000)
	return p
}

func TestStorageNativeDarwinVolumePacking(t *testing.T) {
	got, err := storageNativeDarwinVolumeRecord(storageNativeVolumeFixture())
	if err != nil || got.Size != 10000 || got.Free != 3000 || got.Available != 2000 || got.Used != 4000 || got.Quota != 6000 || got.Reserve != 1000 || got.UUID[15] != 16 || got.Capabilities[0] != 0x00800001 || got.ValidCapabilities[0] != 0x00820001 {
		t.Fatalf("%+v %v", got, err)
	}
	// Supported zero is retained as RAW zero, not interpreted as unlimited.
	p := storageNativeVolumeFixture()
	for i := 104; i < 120; i++ {
		p[i] = 0
	}
	got, err = storageNativeDarwinVolumeRecord(p)
	if err != nil || got.Quota != 0 || got.Reserve != 0 {
		t.Fatalf("raw zero: %+v %v", got, err)
	}
}

func TestStorageNativeDarwinVolumeRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func([]byte) []byte
		err    error
	}{
		{"short", func(p []byte) []byte { return p[:119] }, storagebudget.ErrBounds},
		{"extra", func(p []byte) []byte { return append(p, 0) }, storagebudget.ErrBounds},
		{"truncated-fullsize", func(p []byte) []byte { binary.LittleEndian.PutUint32(p[:4], 124); return p }, storagebudget.ErrBounds},
		{"no-returned-mask", func(p []byte) []byte { p[7] = 0; return p }, storagebudget.ErrUnsupported},
		{"invalid-packed-quota", func(p []byte) []byte {
			binary.LittleEndian.PutUint32(p[8:12], 0x2086001c)
			for i := 104; i < 112; i++ {
				p[i] = 0
			}
			return p
		}, storagebudget.ErrUnsupported},
		{"unexpected-mask", func(p []byte) []byte { p[12] = 1; return p }, storagebudget.ErrUnsupported},
		{"undefined", func(p []byte) []byte { binary.LittleEndian.PutUint64(p[40:48], math.MaxUint64); return p }, storagebudget.ErrUnknown},
		{"negative-offt", func(p []byte) []byte { binary.LittleEndian.PutUint64(p[104:112], 1<<63); return p }, storagebudget.ErrBounds},
		{"zero-uuid", func(p []byte) []byte {
			for i := 88; i < 104; i++ {
				p[i] = 0
			}
			return p
		}, storagebudget.ErrUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := storageNativeDarwinVolumeRecord(tc.mutate(storageNativeVolumeFixture()))
			if got != (storageNativeDarwinVolumeRaw{}) || !errors.Is(err, tc.err) {
				t.Fatalf("%+v %v; want zero %v", got, err, tc.err)
			}
		})
	}
}

func storageNativeObjectFixture() []byte {
	p := make([]byte, 52)
	binary.LittleEndian.PutUint32(p[:4], 52)
	binary.LittleEndian.PutUint32(p[4:8], 0x82000000)
	binary.LittleEndian.PutUint32(p[20:24], 0xd0)
	binary.LittleEndian.PutUint64(p[24:32], 0x0102030405060708)
	binary.LittleEndian.PutUint64(p[32:40], 0x1112131415161718)
	binary.LittleEndian.PutUint32(p[40:44], 0x01020304)
	binary.LittleEndian.PutUint32(p[44:48], 0x11223344)
	binary.LittleEndian.PutUint32(p[48:52], 0x89abcdef)
	return p
}

func TestStorageNativeDarwinObjectPacking(t *testing.T) {
	got, err := storageNativeDarwinObjectRecord(storageNativeObjectFixture())
	if err != nil || got.FileID != 0x0102030405060708 || got.LinkID != 0x1112131415161718 || got.RealDevice != 0x01020304 || got.RealFSID != [2]int32{0x11223344, -1985229329} {
		t.Fatalf("%+v %v", got, err)
	}
	for _, offset := range []int{4, 20} {
		p := storageNativeObjectFixture()
		binary.LittleEndian.PutUint32(p[offset:offset+4], 0)
		got, err := storageNativeDarwinObjectRecord(p)
		if got != (storageNativeDarwinObjectRaw{}) || !errors.Is(err, storagebudget.ErrUnsupported) {
			t.Fatalf("missing object mask: %+v %v", got, err)
		}
	}
	p := storageNativeObjectFixture()
	binary.LittleEndian.PutUint64(p[32:40], 0)
	if _, err := storageNativeDarwinObjectRecord(p); !errors.Is(err, storagebudget.ErrUnknown) {
		t.Fatal(err)
	}
}

func storageNativeTypedObjectFixture() []byte {
	// Exact56-byte public packed record, deliberately unaligned ID fields.
	return []byte{
		0x38, 0, 0, 0, 8, 0, 0, 0x82, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xd0, 0, 0, 0,
		1, 0, 0, 0, 8, 7, 6, 5, 4, 3, 2, 1, 0x18, 0x17, 0x16, 0x15, 0x14, 0x13, 0x12, 0x11,
		0x24, 0x23, 0x22, 0x21, 0x34, 0x33, 0x32, 0x31, 0xf4, 0xf3, 0xf2, 0xf1,
	}
}

func TestStorageNativeDarwinTypedObjectPacking(t *testing.T) {
	for _, tc := range []struct {
		vnode uint32
		kind  uint8
	}{{1, 2}, {2, 1}} {
		p := storageNativeTypedObjectFixture()
		binary.LittleEndian.PutUint32(p[24:28], tc.vnode)
		got, err := storageNativeDarwinTypedObjectRecord(p)
		if err != nil || got.Kind != tc.kind || got.VnodeType != tc.vnode || got.FileID != 0x0102030405060708 || got.LinkID != 0x1112131415161718 || got.RealDevice != 0x21222324 || uint32(got.RealFSID[0]) != 0x31323334 || uint32(got.RealFSID[1]) != 0xf1f2f3f4 || !got.Started.IsZero() {
			t.Fatalf("%+v %v", got, err)
		}
	}
}

func TestStorageNativeDarwinTypedObjectRefusals(t *testing.T) {
	check := func(p []byte, want error) {
		t.Helper()
		got, err := storageNativeDarwinTypedObjectRecord(p)
		if got != (storageNativeDarwinTypedObjectRaw{}) || !errors.Is(err, want) {
			t.Fatalf("%+v %v want %v", got, err, want)
		}
	}
	for _, vnode := range []uint32{0, 3, 4, 5, 6, 7, 8, 9, 10, math.MaxUint32} {
		p := storageNativeTypedObjectFixture()
		binary.LittleEndian.PutUint32(p[24:28], vnode)
		check(p, storagebudget.ErrUnknown)
	}
	check(storageNativeObjectFixture(), storagebudget.ErrBounds)
	for _, size := range []int{52, 55, 57} {
		p := storageNativeTypedObjectFixture()
		binary.LittleEndian.PutUint32(p[:4], uint32(size))
		check(p, storagebudget.ErrBounds)
	}
	check(storageNativeTypedObjectFixture()[:55], storagebudget.ErrBounds)
	check(append(storageNativeTypedObjectFixture(), 0), storagebudget.ErrBounds)
	for _, tc := range []struct {
		offset int
		bit    uint32
	}{{4, 0x80000000}, {4, 8}, {4, 0x02000000}, {20, 0x10}, {20, 0x40}, {20, 0x80}} {
		p := storageNativeTypedObjectFixture()
		mask := binary.LittleEndian.Uint32(p[tc.offset : tc.offset+4])
		binary.LittleEndian.PutUint32(p[tc.offset:tc.offset+4], mask&^tc.bit)
		check(p, storagebudget.ErrUnsupported)
	}
	for _, offset := range []int{4, 8, 12, 16, 20} {
		p := storageNativeTypedObjectFixture()
		p[offset] |= 1
		check(p, storagebudget.ErrUnsupported)
	}
	for _, offset := range []int{28, 36} {
		for _, value := range []uint64{0, math.MaxUint64} {
			p := storageNativeTypedObjectFixture()
			binary.LittleEndian.PutUint64(p[offset:offset+8], value)
			check(p, storagebudget.ErrUnknown)
		}
	}
}
