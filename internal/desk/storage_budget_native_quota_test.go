package desk

import (
	"errors"
	"math"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageNativeQuotaFiniteFixture() storageNativeQuotaDiskRaw {
	return storageNativeQuotaDiskRaw{FilesystemType: 0xef53, Subject: storageNativeQuotaSubject{0, 42}, Version: 1, Flags: 1, BlockUnit: 512, HardBlocks: 8, SoftBlocks: 6, UsedBlocks: 2, HardInodes: 20, SoftInodes: 18, UsedInodes: 3}
}

func TestStorageNativeQuotaConservativeRemaining(t *testing.T) {
	for _, tc := range []struct {
		name             string
		hard, soft, used uint64
		want             storageNativeQuotaResource
	}{
		{"hard-cap", 8, 0, 2, storageNativeQuotaResource{storageNativeQuotaFinite, 3585, 1024, 2561}},
		{"soft-cap", 0, 6, 2, storageNativeQuotaResource{storageNativeQuotaFinite, 2561, 1024, 1537}},
		{"smaller-soft", 8, 6, 2, storageNativeQuotaResource{storageNativeQuotaFinite, 2561, 1024, 1537}},
		{"smaller-hard", 6, 8, 2, storageNativeQuotaResource{storageNativeQuotaFinite, 2561, 1024, 1537}},
		{"exhausted", 2, 0, 2, storageNativeQuotaResource{storageNativeQuotaFinite, 513, 1024, 0}},
		{"over-limit", 2, 0, 3, storageNativeQuotaResource{storageNativeQuotaFinite, 513, 1536, 0}},
		{"zero-not-infinity", 0, 0, 2, storageNativeQuotaResource{storageNativeQuotaNoFiniteLimitReported, 0, 1024, 0}},
		{"rounding-cushion", 2, 0, 1, storageNativeQuotaResource{storageNativeQuotaFinite, 513, 512, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := storageNativeQuotaFiniteFixture()
			raw.HardBlocks, raw.SoftBlocks, raw.UsedBlocks = tc.hard, tc.soft, tc.used
			raw.BlockExpiry = 1 << 38
			got, err := storageNativeQuotaExt4Remaining(raw)
			if err != nil || got.Bytes != tc.want || got.Inodes != (storageNativeQuotaResource{storageNativeQuotaFinite, 18, 3, 15}) {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
	// Pending blocks already enter VFS UsedBlocks before the getter rounds up;
	// never subtract them or rely on a future flush. This fixture tests only the
	// supplied count's arithmetic, not execution of the kernel accounting path.
	raw := storageNativeQuotaFiniteFixture()
	raw.UsedBlocks = 5
	got, err := storageNativeQuotaExt4Remaining(raw)
	if err != nil || got.Bytes.Available != 1 {
		t.Fatalf("pending count arithmetic: %+v %v", got, err)
	}
	raw = storageNativeQuotaFiniteFixture()
	raw.HardInodes, raw.SoftInodes = 0, 0
	got, err = storageNativeQuotaExt4Remaining(raw)
	if err != nil || got.Inodes.State != storageNativeQuotaNoFiniteLimitReported || got.Inodes.Available != 0 {
		t.Fatalf("zero inode caps: %+v %v", got, err)
	}
}

func TestStorageNativeQuotaNormalizationRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*storageNativeQuotaDiskRaw)
		err    error
	}{
		{"backend", func(r *storageNativeQuotaDiskRaw) { r.FilesystemType = 0x58465342 }, storagebudget.ErrUnsupported},
		{"version", func(r *storageNativeQuotaDiskRaw) { r.Version = 0 }, storagebudget.ErrUnsupported},
		{"subject", func(r *storageNativeQuotaDiskRaw) { r.Subject.Kind = 3 }, storagebudget.ErrBounds},
		{"flags", func(r *storageNativeQuotaDiskRaw) { r.Flags = 4 }, storagebudget.ErrUnsupported},
		{"unit", func(r *storageNativeQuotaDiskRaw) { r.BlockUnit = 1024 }, storagebudget.ErrUnsupported},
		{"setter-mask-not-validity", func(r *storageNativeQuotaDiskRaw) { r.FieldMask = 0x1000 }, storagebudget.ErrUnsupported},
		{"realtime", func(r *storageNativeQuotaDiskRaw) { r.RealtimeUsedBlocks = 1 }, storagebudget.ErrUnsupported},
		{"byte-overflow", func(r *storageNativeQuotaDiskRaw) { r.HardBlocks = math.MaxUint64 }, storagebudget.ErrBounds},
		{"byte-bound", func(r *storageNativeQuotaDiskRaw) { r.UsedBlocks = (1<<60)/512 + 1 }, storagebudget.ErrBounds},
		{"inode-bound", func(r *storageNativeQuotaDiskRaw) { r.HardInodes = 1<<40 + 1 }, storagebudget.ErrBounds},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := storageNativeQuotaFiniteFixture()
			tc.mutate(&raw)
			got, err := storageNativeQuotaExt4Remaining(raw)
			if got != (storageNativeQuotaRemaining{}) || !errors.Is(err, tc.err) {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}
