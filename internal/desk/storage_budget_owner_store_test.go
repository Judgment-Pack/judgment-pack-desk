package desk

import (
	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
	"reflect"
	"testing"
)

// Decimal values are handwritten from the closed initializer/publication
// inventory, not generated from a saved catalog, available space or fixture.
func TestStorageOwnerFiniteBootstrapInventory(t *testing.T) {
	want := map[string]storageOwnerBootstrapEffect{
		"config/claim": {4096, 1, 0, 0}, "config/record": {4096, 1, 0, 0},
		"config/bootstrap": {0, 1, 0, 0}, "config/use": {0, 1, 0, 0},
		"config/intent": {2097152, 1, 0, 0}, "config/catalog-a-create": {8388608, 1, 0, 0},
		"config/catalog-b-create": {8388608, 1, 0, 0}, "config/data-lock": {0, 1, 0, 0},
		"config/work-lock": {0, 1, 0, 0}, "config/anchor": {0, 0, 1, 0},
		"config/prepared": {8388608, 0, 0, 1}, "config/active": {8388608, 0, 0, 1},
		"config/ready": {32, 1, 0, 0}, "config/ready-complete": {0, 0, 0, 1},
		"anchor/lock": {0, 1, 0, 0}, "anchor/initialize": {44040192, 5, 0, 0},
		"anchor/register": {4194304, 0, 0, 1}, "anchor/maintenance": {4194304, 0, 0, 1},
		"anchor/census": {4194304, 0, 0, 1}, "anchor/complete-state": {44040192, 0, 0, 7},
		"config/claim-complete": {4096, 0, 0, 1}, "config/record-complete": {4096, 0, 0, 1},
	}
	if !reflect.DeepEqual(storageOwnerBootstrapEffects, want) {
		t.Fatal("finite source inventory changed")
	}
	var total storagebudget.CapacityVector
	for _, e := range want {
		v, err := storageAdvisoryEstimate(e.bytes, e.files, e.directories, e.metadata)
		if err != nil {
			t.Fatal(err)
		}
		total.Bytes += v.Bytes
		total.Inodes += v.Inodes
	}
	if total != (storagebudget.CapacityVector{Bytes: 281149440, Inodes: 61}) {
		t.Fatal("per-group advisory arithmetic changed", total)
	}
}
