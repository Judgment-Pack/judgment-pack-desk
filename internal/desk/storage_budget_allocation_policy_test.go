package desk

import (
	"encoding/hex"
	"errors"
	"math"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageAdvisoryEstimate(t *testing.T) {
	// Handmade expectations from the version-2 policy; no native inode model
	// changes these authored demands or turns them into measured capacity.
	cases := []struct {
		name                         string
		bytes, files, dirs, metadata uint64
		want                         storagebudget.CapacityVector
	}{
		{"empty", 0, 0, 0, 0, storagebudget.CapacityVector{}},
		{"one byte and file", 1, 1, 0, 0, storagebudget.CapacityVector{Bytes: 458752, Inodes: 3}},
		{"exact block", 65536, 0, 0, 0, storagebudget.CapacityVector{Bytes: 393216, Inodes: 2}},
		{"second block", 65537, 0, 0, 0, storagebudget.CapacityVector{Bytes: 524288, Inodes: 2}},
		{"namespace only", 0, 1, 1, 0, storagebudget.CapacityVector{Bytes: 393216, Inodes: 4}},
		{"empty file", 0, 1, 0, 0, storagebudget.CapacityVector{Bytes: 327680, Inodes: 3}},
		{"directory", 0, 0, 1, 0, storagebudget.CapacityVector{Bytes: 327680, Inodes: 3}},
		{"metadata only", 0, 0, 0, 1, storagebudget.CapacityVector{Bytes: 327680, Inodes: 2}},
		{"multiple metadata", 0, 0, 0, 2, storagebudget.CapacityVector{Bytes: 393216, Inodes: 2}},
		{"mixed group", 65537, 2, 3, 4, storagebudget.CapacityVector{Bytes: 1114112, Inodes: 7}},
		{"policy cap exact", 0, 1048574, 0, 0, storagebudget.CapacityVector{Bytes: 68719607808, Inodes: 1048576}},
		// Estimation does not grant admission or hide demand above the cap.
		{"policy cap exceeded", 0, 1048575, 0, 0, storagebudget.CapacityVector{Bytes: 68719673344, Inodes: 1048577}},
		{"large finite metadata product", 0, 281474976710651, 0, 0, storagebudget.CapacityVector{Bytes: 18446744073709486080, Inodes: 281474976710653}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := storageAdvisoryEstimate(tc.bytes, tc.files, tc.dirs, tc.metadata)
			if err != nil || got != tc.want {
				t.Fatalf("estimate = %+v, %v; want %+v", got, err, tc.want)
			}
			if got.Inodes < tc.files || got.Inodes-tc.files < tc.dirs {
				t.Fatal("policy demand falls below unchanged namespace lower bound")
			}
		})
	}
}

func TestStorageAdvisoryStartupGroupEstimate(t *testing.T) {
	// Four directory groups, one empty-file group, two mode changes and five
	// current-state persistence completions; no extra operation is authorized.
	groups := [][4]uint64{
		{0, 0, 1, 0}, {0, 0, 1, 0}, {0, 0, 1, 0}, {0, 0, 1, 0},
		{0, 1, 0, 0},
		{0, 0, 0, 1}, {0, 0, 0, 1},
		{0, 0, 0, 1}, {0, 0, 0, 1}, {0, 0, 0, 1}, {0, 0, 0, 1}, {0, 0, 0, 1},
	}
	var total storagebudget.CapacityVector
	for _, group := range groups {
		demand, err := storageAdvisoryEstimate(group[0], group[1], group[2], group[3])
		if err != nil {
			t.Fatal(err)
		}
		total.Bytes += demand.Bytes
		total.Inodes += demand.Inodes
	}
	if want := (storagebudget.CapacityVector{Bytes: 3932160, Inodes: 29}); total != want {
		t.Fatalf("startup groups = %+v; want %+v", total, want)
	}
}

func TestStorageAdvisoryEstimateOverflow(t *testing.T) {
	cases := []struct {
		name                         string
		bytes, files, dirs, metadata uint64
	}{
		{"rounding", math.MaxUint64, 0, 0, 0},
		{"byte multiplication", 1 << 63, 0, 0, 0},
		{"namespace addition", 0, math.MaxUint64, 1, 0},
		{"inode margin", 0, math.MaxUint64 - 1, 0, 0},
		{"metadata addition", 0, 1, 0, math.MaxUint64},
		{"group metadata margin", 0, 0, 0, math.MaxUint64 - 3},
		{"metadata multiplication", 0, 1 << 48, 0, 0},
		{"final byte addition", (1 << 63) - 131072, 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := storageAdvisoryEstimate(tc.bytes, tc.files, tc.dirs, tc.metadata)
			if !errors.Is(err, storagebudget.ErrBounds) || got != (storagebudget.CapacityVector{}) {
				t.Fatalf("overflow = %+v, %v; want zero and ErrBounds", got, err)
			}
		})
	}
}

func TestStorageAdvisoryPolicyMaterial(t *testing.T) {
	const wantMaterial = "6465736b2d61647669736f72792d616c6c6f636174696f6e2d76320000000100000000000200000000000000040000000000000002000000000000000000100000000000000000200000000000000080000000006400000000000000000400000000000000f2052a0100000080b2e60e00000000"
	const wantEvidence = "71f4092583354855d89378e56c116eff2079496e2eec46be5d05e0d8b2b1e06e"
	material := storageAdvisoryPolicyMaterial()
	if got := hex.EncodeToString(material); got != wantMaterial {
		t.Fatalf("policy material = %s; want %s", got, wantMaterial)
	}
	evidence := storageAdvisoryPolicyEvidence()
	if got := hex.EncodeToString(evidence[:]); got != wantEvidence {
		t.Fatalf("policy evidence = %s; want %s", got, wantEvidence)
	}
	material[0] = 0
	if got := hex.EncodeToString(storageAdvisoryPolicyMaterial()); got != wantMaterial {
		t.Fatal("caller mutated shared policy material")
	}
	if storageAdvisoryInodePolicyCap != 1048576 {
		t.Fatal("fixed authored inode-policy cap changed")
	}
}
