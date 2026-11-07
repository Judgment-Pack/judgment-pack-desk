package desk

import (
	"bytes"
	"math"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func ownerCatalogLink(c *storageOwnerCatalogV2, previous []byte) {
	copy(c.PreviousCatalogDigest[:], previous[32:64])
}
func ownerCatalogBeforeAll(c *storageOwnerCatalogV2) {
	c.BeforeDefinitionIDs = storageOwnerDefinitionIDs{[]storagebudget.ID{}, []storagebudget.ID{}, []storagebudget.ID{}, []storagebudget.ID{}}
	for _, r := range c.Roots {
		c.BeforeDefinitionIDs.Roots = append(c.BeforeDefinitionIDs.Roots, r.Definition.ID)
	}
	for _, w := range c.Workspaces {
		c.BeforeDefinitionIDs.Workspaces = append(c.BeforeDefinitionIDs.Workspaces, w.Definition.ID)
	}
	for _, f := range c.Filesystems {
		c.BeforeDefinitionIDs.Filesystems = append(c.BeforeDefinitionIDs.Filesystems, f.Definition.ID)
	}
	for _, v := range c.Constraints {
		c.BeforeDefinitionIDs.Constraints = append(c.BeforeDefinitionIDs.Constraints, v.Definition.ID)
	}
}
func TestStorageOwnerCatalogV2TwoFrameContinuity(t *testing.T) {
	prepared, r, cl := ownerCatalogFixture()
	first := ownerCatalogWire(ownerCatalogJSON(prepared), 1)
	active := ownerCatalogClone(prepared)
	active.Sequence = 2
	active.Phase = "active"
	ownerCatalogLink(&active, first)
	second := ownerCatalogWire(ownerCatalogJSON(active), 2)
	next := ownerCatalogClone(active)
	next.Sequence = 3
	next.Phase = "prepared"
	next.Transition = storagebudget.ID{8}
	ownerCatalogLink(&next, second)
	ownerCatalogBeforeAll(&next)
	ownerCatalogSeal(&next, &r, &cl)
	third := ownerCatalogWire(ownerCatalogJSON(next), 3)
	for _, name := range []string{"prepared-active", "active-next-prepared", "reverse-bank-order", "identical-duplicated-state"} {
		t.Run(name, func(t *testing.T) {
			a, b := first, second
			switch name {
			case "active-next-prepared":
				a, b = second, third
			case "reverse-bank-order":
				a, b = third, second
			case "identical-duplicated-state":
				a, b = first, first
			}
			got, err := storageOwnerDecodeCatalogPairV2(a, b, r, cl)
			if err != nil {
				t.Fatal(err)
			}
			if name == "identical-duplicated-state" {
				if !got.Duplicated || got.PeerLinked {
					t.Fatal("duplicate incorrectly claims a peer transition")
				}
			} else if !got.PeerLinked || got.Duplicated {
				t.Fatal("verified adjacency not reported")
			}
		})
	}
	for _, name := range []string{"conflicting-same-sequence", "gap", "wrong-predecessor-checksum", "prepared-prepared", "active-active", "changed-finalization-revision", "same-new-transition", "changed-old-workspace-mapping", "changed-old-attribution", "torn-peer-no-fallback", "sequence-wrap"} {
		t.Run(name, func(t *testing.T) {
			a, b := first, second
			v := ownerCatalogClone(active)
			switch name {
			case "conflicting-same-sequence":
				v = ownerCatalogClone(prepared)
				v.Transition = storagebudget.ID{8}
				b = ownerCatalogWire(ownerCatalogJSON(v), 1)
			case "gap":
				v.Sequence = 3
				b = ownerCatalogWire(ownerCatalogJSON(v), 3)
			case "wrong-predecessor-checksum":
				v.PreviousCatalogDigest[0] ^= 1
				b = ownerCatalogWire(ownerCatalogJSON(v), 2)
			case "prepared-prepared":
				v.Phase = "prepared"
				b = ownerCatalogWire(ownerCatalogJSON(v), 2)
			case "active-active":
				a = second
				v = ownerCatalogClone(next)
				v.Phase = "active"
				b = ownerCatalogWire(ownerCatalogJSON(v), 3)
			case "changed-finalization-revision":
				v.ExpectedQRevision++
				b = ownerCatalogWire(ownerCatalogJSON(v), 2)
			case "same-new-transition":
				a = second
				v = ownerCatalogClone(next)
				v.Transition = active.Transition
				b = ownerCatalogWire(ownerCatalogJSON(v), 3)
			case "changed-old-workspace-mapping":
				a = second
				v = ownerCatalogClone(next)
				v.Workspaces[1].OwnerKey = []byte("changed-retained-key")
				ownerCatalogSeal(&v, &r, &cl)
				b = ownerCatalogWire(ownerCatalogJSON(v), 3)
			case "changed-old-attribution":
				a = second
				v = ownerCatalogClone(next)
				v.Roots[2].References = []storagebudget.ID{}
				ownerCatalogSeal(&v, &r, &cl)
				b = ownerCatalogWire(ownerCatalogJSON(v), 3)
			case "torn-peer-no-fallback":
				b = append([]byte{}, second...)
				b[32] ^= 1
			case "sequence-wrap":
				v.Sequence = math.MaxUint64
				b = ownerCatalogWire(ownerCatalogJSON(v), math.MaxUint64)
			}
			got, err := storageOwnerDecodeCatalogPairV2(a, b, r, cl)
			if err == nil || got.PeerLinked || got.Low.Version != 0 {
				t.Fatal("invalid peer accepted or convenient fallback returned")
			}
		})
	}
	t.Run("single-page-does-not-prove-peer", func(t *testing.T) {
		v := ownerCatalogClone(next)
		v.PreviousCatalogDigest = storagebudget.Digest{55}
		raw := ownerCatalogWire(ownerCatalogJSON(v), 3)
		if _, err := storageOwnerDecodeCatalogV2(raw, r, cl); err != nil {
			t.Fatal(err)
		}
		if _, err := storageOwnerDecodeCatalogPairV2(second, raw, r, cl); err == nil {
			t.Fatal("opaque hash promoted into predecessor proof")
		}
	})
	t.Run("checksum-is-header-and-payload-not-padding", func(t *testing.T) {
		if !bytes.Equal(active.PreviousCatalogDigest[:], first[32:64]) {
			t.Fatal("wrong predecessor operand")
		}
		if active.PreviousCatalogDigest == ownerCatalogDigest("", string(first)) {
			t.Fatal("whole-frame domain substituted")
		}
	})
	for _, name := range []string{"first-active", "first-nonzero-previous", "first-nonempty-before", "later-zero-previous"} {
		t.Run(name, func(t *testing.T) {
			v := ownerCatalogClone(prepared)
			switch name {
			case "first-active":
				v.Phase = "active"
			case "first-nonzero-previous":
				v.PreviousCatalogDigest = storagebudget.Digest{1}
			case "first-nonempty-before":
				v.BeforeDefinitionIDs.Constraints = []storagebudget.ID{{10}}
			case "later-zero-previous":
				v.Sequence = 2
			}
			if err := v.validate(r, cl); err == nil {
				t.Fatal("bad scalar chain shape accepted")
			}
		})
	}
}
