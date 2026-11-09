package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Retained rows are matched by semantic PlanID and compared in full. Inserting
// a new sorted row is permitted; altering, evicting or recapturing an old one is not.
func storageOwnerV4Retains(low, high storageOwnerV4Catalog) bool {
	if low.UpgradeOrigin != high.UpgradeOrigin {
		return false
	}
	for _, group := range [][2]any{{low.LineagePlans, high.LineagePlans}, {low.LineageCompletions, high.LineageCompletions}, {low.LineageContinuations, high.LineageContinuations}} {
		a, b := reflect.ValueOf(group[0]), reflect.ValueOf(group[1])
		j := 0
		for i := 0; i < a.Len(); i++ {
			id := a.Index(i).FieldByName("PlanID").Interface().(storagebudget.ID)
			for j < b.Len() {
				next := b.Index(j).FieldByName("PlanID").Interface().(storagebudget.ID)
				if bytes.Compare(next[:], id[:]) >= 0 {
					break
				}
				j++
			}
			if j == b.Len() || !reflect.DeepEqual(a.Index(i).Interface(), b.Index(j).Interface()) {
				return false
			}
			j++
		}
	}
	return true
}
func storageOwnerV4AppendOnlyKind(low, high storageOwnerV4Catalog) bool {
	if low.Transition == high.Transition || !storageOwnerV4Retains(low, high) {
		return false
	}
	dp, dc, df := len(high.LineagePlans)-len(low.LineagePlans), len(high.LineageCompletions)-len(low.LineageCompletions), len(high.LineageContinuations)-len(low.LineageContinuations)
	if high.TransitionKind == storageOwnerV4Enrollment {
		return dp == 0 && dc == 0 && df == 0 && high.TransitionPlan == (storagebudget.ID{}) && storageOwnerV4EnrollmentRetains(low.old(), high.old())
	}
	if !reflect.DeepEqual(storageOwnerV4OldProjection(low.old()), storageOwnerV4OldProjection(high.old())) {
		return false
	}
	if high.BeforeDefinitionsDigest != low.AfterDefinitionsDigest || high.AfterDefinitionsDigest != low.AfterDefinitionsDigest || high.ExpectedQRevision != low.ExpectedQRevision {
		return false
	}
	// A later v4 transition cannot claim to originate a second v3 bridge.
	switch high.TransitionKind {
	case storageOwnerV4Plan:
		if dp != 1 || dc != 0 || df != 0 {
			return false
		}
	case storageOwnerV4Completion:
		if dp != 0 || dc != 1 || df != 0 {
			return false
		}
	case storageOwnerV4Continuation:
		if dp != 0 || dc != 0 || df != 1 {
			return false
		}
	default:
		return false
	}
	// The one appended row must be precisely the transition's named plan.
	var added any
	switch high.TransitionKind {
	case storageOwnerV4Plan:
		added = high.LineagePlans
	case storageOwnerV4Completion:
		added = high.LineageCompletions
	case storageOwnerV4Continuation:
		added = high.LineageContinuations
	}
	rows := reflect.ValueOf(added)
	for i := 0; i < rows.Len(); i++ {
		id := rows.Index(i).FieldByName("PlanID").Interface().(storagebudget.ID)
		if id != high.TransitionPlan {
			continue
		}
		switch high.TransitionKind {
		case storageOwnerV4Plan:
			for _, r := range low.LineagePlans {
				if r.PlanID == id {
					return false
				}
			}
		case storageOwnerV4Completion:
			for _, r := range low.LineageCompletions {
				if r.PlanID == id {
					return false
				}
			}
		case storageOwnerV4Continuation:
			for _, r := range low.LineageContinuations {
				if r.PlanID == id {
					return false
				}
			}
		}
		return true
	}
	return false
}
func storageOwnerV4Adjacent(low, high storageOwnerV4Catalog, lowRaw []byte) bool {
	if len(lowRaw) != storageOwnerCatalogBytes || low.Sequence == math.MaxUint64 || high.Sequence != low.Sequence+1 {
		return false
	}
	var d storagebudget.Digest
	copy(d[:], lowRaw[32:64])
	if high.PreviousCatalogDigest != d {
		return false
	}
	if low.Phase == "prepared" && high.Phase == "active" {
		v := high
		v.Sequence = low.Sequence
		v.Phase = low.Phase
		v.PreviousCatalogDigest = low.PreviousCatalogDigest
		return reflect.DeepEqual(low, v)
	}
	return low.Phase == "active" && high.Phase == "prepared" && storageOwnerV4AppendOnlyKind(low, high)
}

type storageOwnerV4BridgeClass uint8

const (
	storageOwnerV4Refuse storageOwnerV4BridgeClass = iota
	storageOwnerV4OldPair
	storageOwnerV4BridgeNeedsDuplicate
	storageOwnerV4PreparedProvisionalBarrierUnproved
	storageOwnerV4PreparedDuplicatedProvisional
	storageOwnerV4AdjacentProvisional
	storageOwnerV4ActiveNeedsExactPeer
	storageOwnerV4ActiveDuplicatedProvisional
	storageOwnerV4MalformedRecognized
	storageOwnerV4SavedPreparedReceiptProvisional
)

type storageOwnerV4Classification struct {
	Class    storageOwnerV4BridgeClass
	Selected int
	Catalog  storageOwnerV4Catalog
}

// Classify never opens Q or establishes a domain/persistence predicate. In
// particular Active+torn and identical Active are provisional only: validate
// originals and the pre-copy predicate first, persist/copy exact raw bytes,
// then recheck full PlanFinalize (including both banks) before identity effects.
func storageOwnerV4Classify(a, b []byte, r storageOwnerRecordV2, cl storageOwnerClaimV2) (storageOwnerV4Classification, error) {
	raw := [2][]byte{a, b}
	var v [2]storageOwnerV4Catalog
	var good [2]bool
	var old [2]storageOwnerCatalogV2
	var oldGood [2]bool
	for i := 0; i < 2; i++ {
		c, err := storageOwnerV4Decode(raw[i], r, cl)
		v[i], good[i] = c, err == nil
		o, e := storageOwnerDecodeCatalogV3(raw[i], r, cl)
		old[i], oldGood[i] = storageOwnerCatalogV2(o), e == nil
	}
	if !good[0] && !good[1] {
		for _, bank := range raw {
			if len(bank) >= 8 && bytes.Equal(bank[:8], storageOwnerV4Magic[:]) {
				return storageOwnerV4Classification{Class: storageOwnerV4MalformedRecognized}, storagebudget.ErrBounds
			}
		}
		if _, err := storageOwnerDecodeCatalogPairV3(a, b, r, cl); err != nil {
			return storageOwnerV4Classification{}, storagebudget.ErrBounds
		}
		return storageOwnerV4Classification{Class: storageOwnerV4OldPair}, nil
	}
	if good[0] && good[1] {
		if v[0].Sequence == v[1].Sequence {
			if !bytes.Equal(a, b) {
				return storageOwnerV4Classification{}, storagebudget.ErrBounds
			}
			class := storageOwnerV4PreparedDuplicatedProvisional
			if v[0].Phase == "active" {
				class = storageOwnerV4ActiveDuplicatedProvisional
			} else if v[0].TransitionKind != storageOwnerV4BridgePlan {
				return storageOwnerV4Classification{}, storagebudget.ErrBounds
			}
			return storageOwnerV4Classification{Class: class, Selected: 0, Catalog: v[0]}, nil
		}
		low, high := 0, 1
		if v[0].Sequence > v[1].Sequence {
			low, high = 1, 0
		}
		if !storageOwnerV4Adjacent(v[low], v[high], raw[low]) {
			return storageOwnerV4Classification{}, storagebudget.ErrBounds
		}
		return storageOwnerV4Classification{Class: storageOwnerV4AdjacentProvisional, Selected: high, Catalog: v[high]}, nil
	}
	i, j := 0, 1
	if !good[0] {
		i, j = 1, 0
	}
	c := v[i]
	if oldGood[j] {
		o := old[j]
		origin, err := storageOwnerV4Origin(raw[j], r, cl)
		if err != nil || o.Sequence == math.MaxUint64 || c.Sequence != o.Sequence+1 || c.Phase != "prepared" || c.TransitionKind != storageOwnerV4BridgePlan || c.UpgradeOrigin != origin || c.PreviousCatalogDigest != origin.FrameDigest || c.Transition == o.Transition || !reflect.DeepEqual(storageOwnerV4OldProjection(o), storageOwnerV4OldProjection(c.old())) || len(c.LineagePlans) != 1 || len(c.LineageCompletions) != 0 || len(c.LineageContinuations) != 0 || c.LineagePlans[0].PlanID != c.TransitionPlan || c.BeforeDefinitionsDigest != o.AfterDefinitionsDigest || c.AfterDefinitionsDigest != o.AfterDefinitionsDigest || c.ExpectedQRevision != o.ExpectedQRevision {
			return storageOwnerV4Classification{}, storagebudget.ErrBounds
		}
		return storageOwnerV4Classification{Class: storageOwnerV4BridgeNeedsDuplicate, Selected: i, Catalog: c}, nil
	}
	// Only an original-sized blank/torn peer qualifies; missing arbitrary data
	// is not permission to create a new bank. Native bank custody is separate.
	if len(raw[j]) != storageOwnerCatalogBytes {
		return storageOwnerV4Classification{}, storagebudget.ErrBounds
	}
	if _, err := storageOwnerDecodeCatalogV2(raw[j], r, cl); err == nil {
		return storageOwnerV4Classification{}, storagebudget.ErrBounds
	}
	if c.Phase == "prepared" {
		if c.TransitionKind == storageOwnerV4Completion || c.TransitionKind == storageOwnerV4Continuation {
			// Verify the declared checksum independently of full envelope
			// validity. A reserved/header/padding/content violation can leave
			// that checksum valid; it must refuse rather than look torn.
			if storageOwnerV4RecognizedChecksum(raw[j]) {
				return storageOwnerV4Classification{}, storagebudget.ErrBounds
			}
			// A durable Prepared C/F can survive a torn Active publication into
			// its original peer. Carry this exact saved receipt to predicate 3;
			// do not infer the lost predecessor or recapture a reused Q/J slot.
			// Original custody/current authorized identity must validate before
			// the existing same-frame persistence and exact Active continuation.
			// Completion/from phase keys are shared/max once, with no new grant.
			return storageOwnerV4Classification{Class: storageOwnerV4SavedPreparedReceiptProvisional, Selected: i, Catalog: c}, nil
		}
		if c.TransitionKind != storageOwnerV4BridgePlan || len(c.LineagePlans) != 1 || len(c.LineageCompletions) != 0 || len(c.LineageContinuations) != 0 {
			return storageOwnerV4Classification{}, storagebudget.ErrBounds
		}
		return storageOwnerV4Classification{Class: storageOwnerV4PreparedProvisionalBarrierUnproved, Selected: i, Catalog: c}, nil
	}
	return storageOwnerV4Classification{Class: storageOwnerV4ActiveNeedsExactPeer, Selected: i, Catalog: c}, nil
}

// Keep the unchanged old enrollment continuity requirements as well as lineage
// retention. The v4 lineage tables cannot relax any old source/definition join.
func storageOwnerV4EnrollmentRetains(low, high storageOwnerCatalogV2) bool {
	if low.Policy != high.Policy || high.BeforeDefinitionsDigest != low.AfterDefinitionsDigest || !storageOwnerCatalogRetains(low, high) {
		return false
	}
	_, old, err := low.definitions()
	if err != nil {
		return false
	}
	before, _, err := high.definitions()
	if err != nil || !reflect.DeepEqual(old, before) {
		return false
	}
	for _, prior := range low.Roots {
		found := false
		for _, next := range high.Roots {
			if prior.Definition.ID == next.Definition.ID {
				a, err := low.sourceDigest(prior)
				if err != nil {
					return false
				}
				b, err := high.sourceDigest(next)
				if err != nil || a != b {
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

// This is only a conservative malformed-peer discriminator for the provisional
// saved C/F route, never a decoder or a grant. Header validity, zero padding and
// closed content deliberately do not participate: recognizable checksum-valid
// malformed envelopes must refuse too. An unbounded declared length is never
// sliced. Blank or checksum-damaged interrupted peers return false and remain
// subject to the route's separate original custody/runtime predicates.
func storageOwnerV4RecognizedChecksum(raw []byte) bool {
	if len(raw) != storageOwnerCatalogBytes {
		return false
	}
	if !bytes.Equal(raw[:8], storageOwnerV4Magic[:]) && !bytes.Equal(raw[:8], storageOwnerCatalogMagicV3[:]) && !bytes.Equal(raw[:8], storageOwnerCatalogMagic[:]) {
		return false
	}
	n := binary.LittleEndian.Uint32(raw[24:28])
	if n > storageOwnerCatalogBytes-64 {
		return false
	}
	end := 64 + int(n)
	h := sha256.New()
	h.Write(raw[:32])
	h.Write(raw[64:end])
	return bytes.Equal(h.Sum(nil), raw[32:64])
}
