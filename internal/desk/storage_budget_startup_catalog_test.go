package desk

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageOwnerFreshCatalogEmptyReferences(t *testing.T) {
	for _, anchorRefs := range []struct {
		name string
		refs []storagebudget.ID
	}{{"nil", nil}, {"empty", []storagebudget.ID{}}} {
		t.Run(anchorRefs.name, func(t *testing.T) {
			// Use handwritten object/profile witnesses, then run the actual fresh
			// catalog constructor. No native observer or provider is invoked here.
			fixture, record, claim := ownerAdvisoryFixture(false)
			config := &storageOriginalSourceRoot{
				path: "/config", key: ownerTestHome, role: 1, category: storagebudget.Control,
				persistent: fixture.Objects[0].Persistent,
				workspaces: []storagebudget.ID{{2}, {1}, {2}},
			}
			anchor := &storageOriginalSourceRoot{
				path: "/config/storage-budget", key: ownerTestHome, role: 2, category: storagebudget.Control,
				persistent: fixture.Objects[1].Persistent, parent: config, relative: "storage-budget",
				workspaces: anchorRefs.refs,
			}
			x := &storageOriginalSources{
				owner: fixture.OriginalOwner, objects: fixture.Objects[:18],
				controls: []storageOwnerControl{}, workspaces: fixture.Workspaces,
				roots: []*storageOriginalSourceRoot{config, anchor},
			}
			view := storagebudget.HostInspection{Configuration: storagebudget.Configuration{Revision: 1, Policy: storageHostInitialPolicy()}}
			construction := &storageBudgetConstruction{}
			cat, err := construction.constructOwnerCatalog(t.Context(), x, record, claim, fixture.Profiles[0], view)
			if err != nil {
				t.Fatal("fresh construction", err)
			}
			anchorIndex := -1
			for i, root := range cat.Roots {
				if root.Role == 1 && !reflect.DeepEqual(root.References, []storagebudget.ID{{1}, {2}}) {
					t.Fatal("config workspace references changed", root.References)
				}
				if root.Role == 2 {
					anchorIndex = i
					if root.References == nil || len(root.References) != 0 || root.Definition.Workspace != (storagebudget.ID{}) || root.DomainObject != storageOwnerNoObject || root.WorkObject != storageOwnerNoObject {
						t.Fatal("anchor references or ownership changed", root)
					}
				}
			}
			if anchorIndex < 0 {
				t.Fatal("fresh catalog omitted anchor")
			}
			raw, err := storageOwnerEncodeCatalogV3(storageOwnerCatalogV3(cat), record, claim)
			if err != nil {
				t.Fatal("fresh catalog rejected by strict codec", err)
			}
			decoded, err := storageOwnerDecodeCatalogV3(raw, record, claim)
			if err != nil || !reflect.DeepEqual(cat, storageOwnerCatalogV2(decoded)) {
				t.Fatal("fresh catalog strict round trip", err)
			}
			// Null remains invalid on both author and decoder paths. Only the
			// constructor's canonical empty representation is corrected.
			cat.Roots[anchorIndex].References = nil
			if _, err = storageOwnerEncodeCatalogV3(storageOwnerCatalogV3(cat), record, claim); !errors.Is(err, storagebudget.ErrBounds) {
				t.Fatal("author accepted null references", err)
			}
			if _, err = storageOwnerDecodeCatalogV3(ownerAdvisoryWire(cat), record, claim); !errors.Is(err, storagebudget.ErrBounds) {
				t.Fatal("decoder accepted null references", err)
			}
		})
	}
}
