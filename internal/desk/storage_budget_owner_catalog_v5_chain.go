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
func storageOwnerV5Retains(low, high storageOwnerV5Catalog) bool {
	if low.UpgradeOrigin != high.UpgradeOrigin {
		return false
	}
	for _, group := range [][2]any{{low.LineagePlans, high.LineagePlans}, {low.LineageCompletions, high.LineageCompletions}, {low.LineageContinuations, high.LineageContinuations}, {low.LineageStampCompletions, high.LineageStampCompletions}} {
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
func storageOwnerV5AppendOnlyKind(low, high storageOwnerV5Catalog) bool {
	if low.Version != 5 || high.Version != 5 || low.UpgradeOrigin != (storageOwnerV5UpgradeOrigin{}) || high.UpgradeOrigin != (storageOwnerV5UpgradeOrigin{}) || low.Transition == high.Transition || !storageOwnerV5Retains(low, high) {
		return false
	}
	dp, dc, df := len(high.LineagePlans)-len(low.LineagePlans), len(high.LineageCompletions)-len(low.LineageCompletions), len(high.LineageContinuations)-len(low.LineageContinuations)
	ds := len(high.LineageStampCompletions) - len(low.LineageStampCompletions)
	if high.TransitionKind == storageOwnerV5Enrollment {
		return dp == 0 && dc == 0 && df == 0 && ds == 0 && high.TransitionPlan == (storagebudget.ID{}) && storageOwnerV4EnrollmentRetains(low.old(), high.old())
	}
	if !reflect.DeepEqual(storageOwnerV4OldProjection(low.old()), storageOwnerV4OldProjection(high.old())) {
		return false
	}
	if high.BeforeDefinitionsDigest != low.AfterDefinitionsDigest || high.AfterDefinitionsDigest != low.AfterDefinitionsDigest || high.ExpectedQRevision != low.ExpectedQRevision {
		return false
	}
	// Kind5 is reserved: there is no accepted v5 upgrade/bridge.
	switch high.TransitionKind {
	case storageOwnerV5Plan:
		if dp != 1 || dc != 0 || df != 0 || ds != 0 {
			return false
		}
	case storageOwnerV5Completion:
		if dp != 0 || dc != 1 || df != 0 || ds != 0 {
			return false
		}
	case storageOwnerV5Continuation:
		if dp != 0 || dc != 0 || df != 1 || ds != 0 {
			return false
		}
	case storageOwnerV5StampCompletion:
		if dp != 0 || dc != 0 || df != 0 || ds != 1 {
			return false
		}
	default:
		return false
	}
	// The one appended row must be precisely the transition's named plan.
	var added any
	switch high.TransitionKind {
	case storageOwnerV5Plan:
		added = high.LineagePlans
	case storageOwnerV5Completion:
		added = high.LineageCompletions
	case storageOwnerV5Continuation:
		added = high.LineageContinuations
	case storageOwnerV5StampCompletion:
		added = high.LineageStampCompletions
	}
	rows := reflect.ValueOf(added)
	for i := 0; i < rows.Len(); i++ {
		id := rows.Index(i).FieldByName("PlanID").Interface().(storagebudget.ID)
		if id != high.TransitionPlan {
			continue
		}
		switch high.TransitionKind {
		case storageOwnerV5Plan:
			for _, r := range low.LineagePlans {
				if r.PlanID == id {
					return false
				}
			}
		case storageOwnerV5Completion:
			for _, r := range low.LineageCompletions {
				if r.PlanID == id {
					return false
				}
			}
		case storageOwnerV5Continuation:
			for _, r := range low.LineageContinuations {
				if r.PlanID == id {
					return false
				}
			}
		case storageOwnerV5StampCompletion:
			for _, r := range low.LineageStampCompletions {
				if r.PlanID == id {
					return false
				}
			}
		}
		return true
	}
	return false
}
func storageOwnerV5Adjacent(low, high storageOwnerV5Catalog, lowRaw []byte) bool {
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
	return low.Phase == "active" && high.Phase == "prepared" && storageOwnerV5AppendOnlyKind(low, high)
}

type storageOwnerV5PeerClass uint8

const (
	storageOwnerV5Refuse storageOwnerV5PeerClass = iota
	storageOwnerV5PreparedDuplicatedProvisional
	storageOwnerV5ActiveDuplicatedProvisional
	storageOwnerV5AdjacentProvisional
	storageOwnerV5SavedPreparedReceiptProvisional
	storageOwnerV5ActiveNeedsExactPeer
	storageOwnerV5MalformedRecognized
)

type storageOwnerV5Classification struct {
	Class    storageOwnerV5PeerClass
	Selected int
	Catalog  storageOwnerV5Catalog
}

// Every successful result is provisional comparison data, not original custody,
// successful persistence, admission or authority to duplicate a bank. There is
// no old-format fallback, upgrade inference or producer/dispatcher integration.
func storageOwnerV5Classify(a, b []byte, r storageOwnerRecordV2, cl storageOwnerClaimV2) (storageOwnerV5Classification, error) {
	raw := [2][]byte{a, b}
	var v [2]storageOwnerV5Catalog
	var good [2]bool
	for i := 0; i < 2; i++ {
		c, e := storageOwnerV5Decode(raw[i], r, cl)
		v[i], good[i] = c, e == nil
	}
	refuse := func() (storageOwnerV5Classification, error) {
		return storageOwnerV5Classification{}, storagebudget.ErrBounds
	}
	if !good[0] && !good[1] {
		for _, bank := range raw {
			if len(bank) >= 8 && bytes.Equal(bank[:8], storageOwnerV5Magic[:]) {
				return storageOwnerV5Classification{Class: storageOwnerV5MalformedRecognized}, storagebudget.ErrBounds
			}
		}
		return refuse()
	}
	if good[0] && good[1] {
		if v[0].Sequence == v[1].Sequence {
			if !bytes.Equal(a, b) {
				return refuse()
			}
			class := storageOwnerV5PreparedDuplicatedProvisional
			if v[0].Phase == "active" {
				class = storageOwnerV5ActiveDuplicatedProvisional
			}
			return storageOwnerV5Classification{class, 0, v[0]}, nil
		}
		low, high := 0, 1
		if v[0].Sequence > v[1].Sequence {
			low, high = 1, 0
		}
		if !storageOwnerV5Adjacent(v[low], v[high], raw[low]) {
			return refuse()
		}
		return storageOwnerV5Classification{storageOwnerV5AdjacentProvisional, high, v[high]}, nil
	}
	i, j := 0, 1
	if !good[0] {
		i, j = 1, 0
	}
	// Missing/wrong-sized peers and any valid old frame never establish a v5
	// base. Checksum-valid recognized malformed frames are not torn writes.
	if len(raw[j]) != storageOwnerCatalogBytes || storageOwnerV5RecognizedChecksum(raw[j]) {
		return refuse()
	}
	if _, e := storageOwnerV4Decode(raw[j], r, cl); e == nil {
		return refuse()
	}
	if _, e := storageOwnerDecodeCatalogV3(raw[j], r, cl); e == nil {
		return refuse()
	}
	if _, e := storageOwnerDecodeCatalogV2(raw[j], r, cl); e == nil {
		return refuse()
	}
	c := v[i]
	if c.Phase == "prepared" {
		if c.TransitionKind != storageOwnerV5Completion && c.TransitionKind != storageOwnerV5StampCompletion && c.TransitionKind != storageOwnerV5Continuation {
			return refuse()
		}
		// C/S/F have saved factual rows. Replay exactly those bytes, under original
		// custody and current authorized identity; no later Q/J receipt recapture.
		return storageOwnerV5Classification{storageOwnerV5SavedPreparedReceiptProvisional, i, c}, nil
	}
	return storageOwnerV5Classification{storageOwnerV5ActiveNeedsExactPeer, i, c}, nil
}
func storageOwnerV5RecognizedChecksum(raw []byte) bool {
	if len(raw) != storageOwnerCatalogBytes {
		return false
	}
	if !bytes.Equal(raw[:8], storageOwnerV5Magic[:]) {
		return storageOwnerV4RecognizedChecksum(raw)
	}
	n := binary.LittleEndian.Uint32(raw[24:28])
	if n > storageOwnerCatalogBytes-64 {
		return false
	}
	h := sha256.New()
	h.Write(raw[:32])
	h.Write(raw[64 : 64+int(n)])
	return bytes.Equal(h.Sum(nil), raw[32:64])
}
