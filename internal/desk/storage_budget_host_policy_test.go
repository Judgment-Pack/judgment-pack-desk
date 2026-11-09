package desk

import (
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageHostInitialPolicy(t *testing.T) {
	p := storageHostInitialPolicy()
	if p.Installation.Total != (storagebudget.Vector{Bytes: 25769803776, Files: 262144, Directories: 65536}) || p.Workspace.Total != (storagebudget.Vector{Bytes: 4294967296, Files: 65536, Directories: 16384}) {
		t.Fatal("host total policy changed")
	}
	i := [9]uint64{6442450944, 12884901888, 6442450944, 268435456, 536870912, 268435456, 8589934592, 4294967296, 1073741824}
	w := [9]uint64{1073741824, 2147483648, 1073741824, 33554432, 268435456, 67108864, 4294967296, 2147483648, 4294967296}
	for n := range i {
		if p.Installation.Category[n] != (storagebudget.Vector{Bytes: i[n], Files: 262144, Directories: 65536}) || p.Workspace.Category[n] != (storagebudget.Vector{Bytes: w[n], Files: 65536, Directories: 16384}) {
			t.Fatalf("category %d policy changed", n)
		}
	}
	if p.ObservationMaxAge != 5000000000 {
		t.Fatal("freshness changed")
	}
	// Every return owns its value; changing a caller copy cannot grant a later
	// launch more capacity or mutate the original installed defaults.
	p.Installation.Total.Files++
	if storageHostInitialPolicy().Installation.Total.Files != 262144 {
		t.Fatal("shared mutable host policy")
	}
}
