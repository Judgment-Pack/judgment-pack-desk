package desk

import (
	"encoding/base64"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// This is an accumulator projection, not a complete owner/catalog fixture.
func TestStorageOwnerCatalogV2AggregateProjection(t *testing.T) {
	for _, field := range []string{"ConfigPath", "HomePath", "Folder", "HistoryIdentity", "Name", "Path", "Previous", "Relative"} {
		t.Run(field, func(t *testing.T) {
			raw := []byte(`"` + base64.StdEncoding.EncodeToString([]byte{0xff}) + `"`)
			p := storageOwnerCatalogScanner{storageOwnerJSONPreflight: storageOwnerJSONPreflight{raw: raw}, sourceBytes: (4 << 20) - 1}
			if !p.value(reflect.TypeOf([]byte{}), field) || p.sourceBytes != 4<<20 {
				t.Fatal("inclusive raw-byte bound differs")
			}
			p = storageOwnerCatalogScanner{storageOwnerJSONPreflight: storageOwnerJSONPreflight{raw: raw}, sourceBytes: 4 << 20}
			if p.value(reflect.TypeOf([]byte{}), field) {
				t.Fatal("overbudget decoded occurrence accepted")
			}
		})
	}
	t.Run("owner-key-excluded", func(t *testing.T) {
		p := storageOwnerCatalogScanner{storageOwnerJSONPreflight: storageOwnerJSONPreflight{raw: []byte(`"eA=="`)}, sourceBytes: 4 << 20}
		if !p.value(reflect.TypeOf([]byte{}), "OwnerKey") || p.sourceBytes != 4<<20 {
			t.Fatal("logical key counted as path")
		}
	})
	t.Run("equal-occurrences-not-deduplicated", func(t *testing.T) {
		p := storageOwnerCatalogScanner{storageOwnerJSONPreflight: storageOwnerJSONPreflight{raw: []byte(`"eA==""eA=="`)}, sourceBytes: (4 << 20) - 1}
		if !p.byteString("Path") || p.byteString("Path") {
			t.Fatal("equal bytes escaped aggregate")
		}
	})
}

func ownerCatalogPathByteTotal(c storageOwnerCatalogV2) int {
	n := len(c.OriginalOwner.ConfigPath) + len(c.OriginalOwner.HomePath)
	for _, h := range c.OriginalOwner.Entries {
		n += len(h.Folder) + len(h.HistoryIdentity)
	}
	for _, o := range c.Objects {
		n += len(o.Name)
	}
	for _, v := range c.OwnerControls {
		n += len(v.Name) + len(v.Location.Path) + len(v.Location.Previous)
	}
	for _, r := range c.Roots {
		n += len(r.Source.Path) + len(r.Source.Relative)
	}
	for _, w := range c.Workspaces {
		n += len(w.HistoryIdentity)
	}
	return n
}
func ownerCatalogChainOfLength(n int) []byte {
	if n == 0 {
		return []byte{}
	}
	count := (n + 1025) / 1025
	chars := n - (count - 1)
	parts := []string{}
	for i := 0; i < count; i++ {
		take := chars - (count - i - 1)
		if take > 1024 {
			take = 1024
		}
		parts = append(parts, strings.Repeat("x", take))
		chars -= take
	}
	return []byte(strings.Join(parts, "/"))
}
func TestStorageOwnerCatalogV2CompleteAggregateBoundary(t *testing.T) {
	c, r, cl := ownerCatalogFixture()
	for i := 0; i < 64; i++ {
		idx := uint32(len(c.Objects))
		po := storageOwnerPersistentObject{1, [16]byte{1}, uint64(100 + i), 0, 1, 1}
		c.Objects = append(c.Objects, storageOwnerObject{Role: 20, Source: 3, Parent: math.MaxUint32, Name: []byte{}, Persistent: po, OwnerClass: 2, ModeClass: 1})
		root := storageOwnerRoot{Definition: storagebudget.RegisteredRoot{ID: storagebudget.ID{byte(i + 8)}, Filesystem: storagebudget.ID{1}, Generation: 1, Locator: fmt.Sprintf("external:%d", i), CapacityCount: 1, CapacityRefs: [4]storagebudget.CapacityRef{{Constraint: storagebudget.ID{10}, Generation: 1}}}, Role: 6, Category: 2, References: []storagebudget.ID{}, Source: storageOwnerRootSource{Kind: 6, OwnerKey: []byte(fmt.Sprintf("original-family:%d", i)), Path: []byte(fmt.Sprintf("/external/%d", i)), Relative: []byte{}, ControlIndex: math.MaxUint32}, DirectoryObject: idx, DomainObject: idx, WorkObject: math.MaxUint32, Membership: c.Roots[2].Membership}
		c.Roots = append(c.Roots, root)
	}
	for i := 7; i < 70; i++ {
		c.Roots[i].Source.Relative = ownerCatalogChainOfLength(65599)
	}
	remaining := (4 << 20) - ownerCatalogPathByteTotal(c)
	if remaining <= 0 || remaining > 65599 {
		t.Fatal("handwritten aggregate construction outside last-chain bounds")
	}
	c.Roots[70].Source.Relative = ownerCatalogChainOfLength(remaining)
	ownerCatalogSeal(&c, &r, &cl)
	t.Run("exact-four-mib-complete-graph", func(t *testing.T) {
		if ownerCatalogPathByteTotal(c) != 4<<20 {
			t.Fatal("incorrect inclusive total")
		}
		raw, err := storageOwnerEncodeCatalogV2(c, r, cl)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := storageOwnerDecodeCatalogV2(raw, r, cl); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("one-decoded-byte-over", func(t *testing.T) {
		v := ownerCatalogClone(c)
		v.Roots[70].Source.Relative = ownerCatalogChainOfLength(remaining + 1)
		if v.bounded() {
			t.Fatal("author aggregate overflow accepted")
		}
		if storageOwnerCatalogPreflight([]byte(ownerCatalogJSON(v))) {
			t.Fatal("raw aggregate overflow accepted")
		}
	})
}

func TestStorageOwnerCatalogV2IndexFreeExpansion(t *testing.T) {
	c, r, cl := ownerCatalogFixture()
	old := append([]storageOwnerRoot{}, c.Roots...)
	// Two unrelated PO keys precede all existing objects. A new control parent
	// also precedes config, shifting both index domains independently.
	for i := range c.Objects {
		if c.Objects[i].Parent != math.MaxUint32 {
			c.Objects[i].Parent += 2
		}
	}
	for i := range c.OwnerControls {
		c.OwnerControls[i].ParentObject += 2
	}
	for i := range c.Roots {
		v := &c.Roots[i]
		v.DirectoryObject += 2
		if v.DomainObject != math.MaxUint32 {
			v.DomainObject += 2
		}
		if v.WorkObject != math.MaxUint32 {
			v.WorkObject += 2
		}
		if v.Source.ControlKind == 1 {
			v.Source.ControlIndex += 2
		} else if v.Source.ControlKind == 2 {
			v.Source.ControlIndex++
		}
		for j := range v.Membership.ExistingObjects {
			v.Membership.ExistingObjects[j] += 2
		}
	}
	pa := storageOwnerPersistentObject{1, [16]byte{1}, 9, 0, 1, 1}
	pb := storageOwnerPersistentObject{1, [16]byte{1}, 10, 0, 1, 1}
	c.Objects = append([]storageOwnerObject{{Role: 20, Source: 3, Parent: math.MaxUint32, Name: []byte{}, Persistent: pa, OwnerClass: 2, ModeClass: 1}, {Role: 21, Source: 3, Parent: 0, Name: []byte("history"), Persistent: pb, OwnerClass: 2, ModeClass: 1}}, c.Objects...)
	c.OwnerControls = append([]storageOwnerControl{{Role: 2, ParentObject: 0, Name: []byte("data-location.json"), OwnerKind: 2, OwnerKey: []byte(strings.Repeat("f", 64)), Location: storageOwnerLocation{Version: 1, Path: []byte("/new/history"), Previous: []byte{}, Selected: pb}, MaxBytes: 16384}}, c.OwnerControls...)
	a := ownerCatalogClone(c).Roots[2]
	a.Definition.ID = storagebudget.ID{8}
	a.Definition.Workspace = storagebudget.ID{}
	a.Definition.Locator = "new-family"
	a.Role = 6
	a.Category = 2
	a.References = []storagebudget.ID{}
	a.DirectoryObject = 0
	a.DomainObject = 0
	a.WorkObject = math.MaxUint32
	a.Source = storageOwnerRootSource{Kind: 6, OwnerKey: []byte("new-family"), Path: []byte("/new"), Relative: []byte{}, ControlIndex: math.MaxUint32}
	a.Membership.ExistingObjects = []uint32{}
	b := a
	b.Definition.ID = storagebudget.ID{9}
	b.Definition.Locator = "new-history"
	b.Role = 4
	b.Category = 0
	b.DirectoryObject = 1
	b.DomainObject = 18
	b.Source = storageOwnerRootSource{Kind: 4, OwnerKey: []byte(strings.Repeat("f", 64)), Path: []byte("/new/history"), ParentRoot: storagebudget.ID{8}, Relative: []byte("history"), ControlKind: 2, ControlIndex: 0, ControlSelection: 1}
	c.Roots = append(c.Roots, a, b)
	ownerCatalogSeal(&c, &r, &cl)
	t.Run("unrelated-object-and-control-indices-shift", func(t *testing.T) {
		if err := c.validate(r, cl); err != nil {
			t.Fatal(err)
		}
		for i, prior := range old {
			if prior.Definition != c.Roots[i].Definition {
				t.Fatal("unrelated index shift changed an existing immutable row")
			}
		}
	})
}
