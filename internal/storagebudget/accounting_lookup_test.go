package storagebudget

import "testing"

func TestAccountingLookupFixedArrayBoundaries(t *testing.T) {
	c := &checkpoint{}
	first, last, missing := ID{1}, ID{2}, ID{3}
	c.Roots[0].ID = first
	c.Roots[MaxRoots-1].ID = last
	c.Workspaces[0].ID = first
	c.Workspaces[MaxWorkspaces-1].ID = last
	c.Filesystems[0].ID = first
	c.Filesystems[MaxFilesystems-1].ID = last
	c.CapacityConstraints[0].ID = first
	c.CapacityConstraints[MaxCapacityConstraints-1].ID = last
	for _, row := range []struct {
		name   string
		size   int
		lookup func(*checkpoint, ID) int
	}{
		{"root", MaxRoots, rootIndex}, {"workspace", MaxWorkspaces, workspaceIndex}, {"filesystem", MaxFilesystems, filesystemIndex}, {"constraint", MaxCapacityConstraints, constraintIndex},
	} {
		t.Run(row.name, func(t *testing.T) {
			if row.lookup(c, first) != 0 || row.lookup(c, last) != row.size-1 || row.lookup(c, missing) != -1 || row.lookup(c, ID{}) != -1 {
				t.Fatal("exact boundary/missing/zero lookup changed")
			}
			if row.lookup(&checkpoint{}, first) != -1 {
				t.Fatal("empty table supplies a definition")
			}
		})
	}
}

// The vectorized scan is the same exact predicate as a byte loop at every
// length and position, including block boundaries and a whole checkpoint page.
func TestAllZeroChecksEveryByte(t *testing.T) {
	block := len(zeroBlock)
	for _, n := range []int{0, 1, 7, 8, 9, block - 1, block, block + 1, 2*block + 3, CheckpointBytes} {
		b := make([]byte, n)
		if !allZero(b) {
			t.Fatal("zero buffer refused", n)
		}
		for _, i := range []int{0, n / 2, block - 1, block, n - 1} {
			if i < 0 || i >= n {
				continue
			}
			b[i] = 1
			if allZero(b) {
				t.Fatal("nonzero byte accepted", n, i)
			}
			b[i] = 0
		}
	}
	if zeroBlock != ([len(zeroBlock)]byte{}) {
		t.Fatal("the shared zero block was written")
	}
}
