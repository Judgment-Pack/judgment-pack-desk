package desk

import (
	"bytes"
	"math"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// PeerLinked distinguishes an actual adjacent link from identical duplicated
// pages. Neither case asserts actual Q state or selects a usable live bank.
// Single-page decoding never sets this bit or implies a verified predecessor.
type storageOwnerCatalogPair struct {
	Low        storageOwnerCatalogV2
	High       storageOwnerCatalogV2
	PeerLinked bool
	Duplicated bool
}

func storageOwnerDecodeCatalogPairV2(a, b []byte, r storageOwnerRecordV2, claim storageOwnerClaimV2) (storageOwnerCatalogPair, error) {
	return storageOwnerDecodeCatalogPairVersion(a, b, r, claim, 2)
}

// V3 pairs preserve the same exact predecessor operand and immutable-row rules;
// a pair cannot cross semantic versions or perform an original-owner rebind.
func storageOwnerDecodeCatalogPairV3(a, b []byte, r storageOwnerRecordV2, claim storageOwnerClaimV2) (storageOwnerCatalogPair, error) {
	return storageOwnerDecodeCatalogPairVersion(a, b, r, claim, 3)
}
func storageOwnerDecodeCatalogPairVersion(a, b []byte, r storageOwnerRecordV2, claim storageOwnerClaimV2, version uint32) (storageOwnerCatalogPair, error) {
	decode := storageOwnerDecodeCatalogV2
	if version == 3 {
		decode = func(raw []byte, r storageOwnerRecordV2, claim storageOwnerClaimV2) (storageOwnerCatalogV2, error) {
			c, e := storageOwnerDecodeCatalogV3(raw, r, claim)
			return storageOwnerCatalogV2(c), e
		}
	} else if version != 2 {
		return storageOwnerCatalogPair{}, storagebudget.ErrBounds
	}
	x, err := decode(a, r, claim)
	if err != nil {
		return storageOwnerCatalogPair{}, err
	}
	y, err := decode(b, r, claim)
	if err != nil {
		return storageOwnerCatalogPair{}, err
	}
	if x.Sequence == y.Sequence {
		if !bytes.Equal(a, b) {
			return storageOwnerCatalogPair{}, storagebudget.ErrBounds
		}
		return storageOwnerCatalogPair{Low: x, High: y, Duplicated: true}, nil
	}
	low, high, lowFrame := x, y, a
	if x.Sequence > y.Sequence {
		low, high, lowFrame = y, x, b
	}
	var digest storagebudget.Digest
	copy(digest[:], lowFrame[32:64])
	if low.Sequence == math.MaxUint64 || high.Sequence != low.Sequence+1 || high.PreviousCatalogDigest != digest {
		return storageOwnerCatalogPair{}, storagebudget.ErrBounds
	}
	switch {
	case low.Phase == "prepared" && high.Phase == "active":
		// Compare the complete detached payload after excluding precisely the
		// three permitted finalization annotations. This preserves old rows,
		// exact source observations and ExpectedQRevision as well as hashes.
		v := high
		v.Sequence = low.Sequence
		v.Phase = low.Phase
		v.PreviousCatalogDigest = low.PreviousCatalogDigest
		if !reflect.DeepEqual(low, v) {
			return storageOwnerCatalogPair{}, storagebudget.ErrBounds
		}
	case low.Phase == "active" && high.Phase == "prepared":
		if low.Transition == high.Transition || low.Policy != high.Policy || high.BeforeDefinitionsDigest != low.AfterDefinitionsDigest {
			return storageOwnerCatalogPair{}, storagebudget.ErrBounds
		}
		_, old, err := low.definitions()
		if err != nil {
			return storageOwnerCatalogPair{}, err
		}
		before, _, err := high.definitions()
		if err != nil || !reflect.DeepEqual(old, before) {
			return storageOwnerCatalogPair{}, storagebudget.ErrBounds
		}
		if !storageOwnerCatalogRetains(low, high) {
			return storageOwnerCatalogPair{}, storagebudget.ErrBounds
		}
		// Definitions cover Q rows; retain the exact original immutable source
		// projection too. Mutable full observations/Selection may change only
		// subject to the separate actual original move/owner workflow.
		for _, prior := range low.Roots {
			found := false
			for _, next := range high.Roots {
				if prior.Definition.ID == next.Definition.ID {
					a, err := low.sourceDigest(prior)
					if err != nil {
						return storageOwnerCatalogPair{}, err
					}
					b, err := high.sourceDigest(next)
					if err != nil || a != b {
						return storageOwnerCatalogPair{}, storagebudget.ErrBounds
					}
					found = true
					break
				}
			}
			if !found {
				return storageOwnerCatalogPair{}, storagebudget.ErrBounds
			}
		}
	default:
		return storageOwnerCatalogPair{}, storagebudget.ErrBounds
	}
	return storageOwnerCatalogPair{Low: low, High: high, PeerLinked: true}, nil
}

func storageOwnerCatalogRetains(low, high storageOwnerCatalogV2) bool {
	a, b := low.OriginalOwner, high.OriginalOwner
	if a.Kind != b.Kind || !bytes.Equal(a.ConfigPath, b.ConfigPath) || a.HomeOwner != b.HomeOwner || !bytes.Equal(a.HomePath, b.HomePath) || a.HomeObject != b.HomeObject {
		return false
	}
	for _, old := range a.Entries {
		found := false
		for _, next := range b.Entries {
			if old.DeskID == next.DeskID {
				if !reflect.DeepEqual(old, next) {
					return false
				}
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	objectIndex := make(map[storageOwnerPersistentObject]uint32, len(high.Objects))
	for i, o := range high.Objects {
		objectIndex[o.Persistent] = uint32(i)
	}
	for i, old := range low.Objects {
		j, ok := objectIndex[old.Persistent]
		if !ok || !reflect.DeepEqual(low.objectMaterial(uint32(i)), high.objectMaterial(j)) {
			return false
		}
	}
	for _, old := range low.OwnerControls {
		found := false
		for _, next := range high.OwnerControls {
			if low.Objects[old.ParentObject].Persistent == high.Objects[next.ParentObject].Persistent && bytes.Equal(old.Name, next.Name) {
				if old.Role != next.Role || old.OwnerKind != next.OwnerKind || !bytes.Equal(old.OwnerKey, next.OwnerKey) || old.MaxBytes != next.MaxBytes {
					return false
				}
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, old := range low.Workspaces {
		found := false
		for _, next := range high.Workspaces {
			if old.Definition.ID == next.Definition.ID {
				if !reflect.DeepEqual(old, next) {
					return false
				}
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, old := range low.Filesystems {
		found := false
		for _, next := range high.Filesystems {
			if old.Definition.ID == next.Definition.ID {
				if old != next {
					return false
				}
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, old := range low.Constraints {
		found := false
		for _, next := range high.Constraints {
			if old.Definition.ID == next.Definition.ID {
				if old != next {
					return false
				}
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, old := range low.Roots {
		found := false
		for _, next := range high.Roots {
			if old.Definition.ID == next.Definition.ID {
				if !reflect.DeepEqual(old.References, next.References) {
					return false
				}
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
