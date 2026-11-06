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
