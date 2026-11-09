package desk

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

var errStorageEnrollmentPeakMigration = errors.New("storage enrollment migration required")

func storageEnrollmentPeakKnownOld(raw []byte) bool {
	if len(raw) < 8 {
		return false
	}
	switch string(raw[:8]) {
	case "DSKONE1\x00", "DSKEPO1\x00", "DSKEST1\x00":
		return true
	}
	return false
}

// Selection is a byte/link comparison, not an original-owner capability. Full
// prefix inputs are checked separately after selecting a Fence/Captured image.
func storageEnrollmentPeakDecodePair(a, b []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakEnvelope, int, error) {
	return storageEnrollmentPeakDecodePairVariant(a, b, record, claim, false)
}

func storageEnrollmentPeakDecodePairVariant(a, b []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2, composition bool) (storageEnrollmentPeakEnvelope, int, error) {
	return storageEnrollmentPeakDecodePairOperands(a, b, record, claim, composition, nil)
}
func storageEnrollmentPeakDecodePairOperands(a, b []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2, composition bool, operands *storageEnrollmentOperands) (storageEnrollmentPeakEnvelope, int, error) {
	return storageEnrollmentPeakDecodePairNative(a, b, record, claim, composition, operands, false)
}

func storageEnrollmentPeakDecodePairNative(a, b []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2, composition bool, operands *storageEnrollmentOperands, native bool) (storageEnrollmentPeakEnvelope, int, error) {
	if storageEnrollmentPeakKnownOld(a) || storageEnrollmentPeakKnownOld(b) {
		return storageEnrollmentPeakEnvelope{}, 0, errStorageEnrollmentPeakMigration
	}
	var values [2]storageEnrollmentPeakEnvelope
	var peak [2]bool
	for i, raw := range [][]byte{a, b} {
		if len(raw) != 8388608 {
			return storageEnrollmentPeakEnvelope{}, 0, storagebudget.ErrBounds
		}
		if string(raw[:8]) == "DSKOWN3\x00" {
			if _, err := storageOwnerDecodeCatalogV3(raw, record, claim); err != nil {
				return storageEnrollmentPeakEnvelope{}, 0, err
			}
			continue
		}
		var e storageEnrollmentPeakEnvelope
		var err error
		if native {
			e, err = storageEnrollmentPeakDecodeEnvelopeNative(raw, record, claim, true, true, true)
		} else {
			e, err = operands.envelope(raw, record, claim, true, composition)
		}
		if err != nil {
			return storageEnrollmentPeakEnvelope{}, 0, err
		}
		values[i] = e
		peak[i] = true
	}
	if !peak[0] && !peak[1] {
		return storageEnrollmentPeakEnvelope{}, 0, storagebudget.ErrBounds
	}
	if peak[0] != peak[1] {
		bank := 0
		if peak[1] {
			bank = 1
		}
		e := values[bank]
		old := a
		if bank == 0 {
			old = b
		}
		if e.Kind != 0 || e.Scope.path() != 1 || e.Snapshot == nil || int(e.Scope[322]) != 1-bank || !bytes.Equal(e.Snapshot.Before, old) {
			return storageEnrollmentPeakEnvelope{}, 0, storagebudget.ErrBounds
		}
		return e, bank, nil
	}
	if values[0].Sequence == values[1].Sequence {
		if !bytes.Equal(a, b) || values[0].Kind == 1 || values[0].Marker != nil && values[0].Marker.Phase == 2 {
			return storageEnrollmentPeakEnvelope{}, 0, storagebudget.ErrBounds
		}
		return values[0], 0, nil
	}
	bank := 0
	if values[1].Sequence > values[0].Sequence {
		bank = 1
	}
	low, high := values[1-bank], values[bank]
	lowRaw := a
	if bank == 0 {
		lowRaw = b
	}
	if low.Sequence == ^uint64(0) || high.Sequence != low.Sequence+1 {
		return storageEnrollmentPeakEnvelope{}, 0, storagebudget.ErrBounds
	}
	if low.Scope != high.Scope {
		if storageEnrollmentPeakCaptureMatches(low.Scope, high.Scope) {
			if low.Kind != 0 || high.Kind != 1 || bank != int(high.Scope[322]) {
				return storageEnrollmentPeakEnvelope{}, 0, storagebudget.ErrBounds
			}
		} else if high.Kind != 0 || low.Kind != 2 || low.Marker == nil || low.Marker.Phase != 3 || high.Snapshot == nil || !bytes.Equal(high.Snapshot.Before, lowRaw) || bank != 1-int(high.Scope[322]) {
			return storageEnrollmentPeakEnvelope{}, 0, storagebudget.ErrBounds
		}
		return high, bank, nil
	}
	if high.Kind != 2 || high.Marker == nil || !bytes.Equal(high.Marker.Previous[:], lowRaw[32:64]) {
		return storageEnrollmentPeakEnvelope{}, 0, storagebudget.ErrBounds
	}
	if low.Kind == 2 {
		if low.Marker == nil || high.Marker.Phase != low.Marker.Phase+1 {
			return storageEnrollmentPeakEnvelope{}, 0, storagebudget.ErrBounds
		}
	} else if low.Kind == 0 {
		if high.Scope.path() == 1 || high.Scope.path() == 3 && high.Marker.Phase != 0 || high.Scope.path() == 2 && high.Marker.Phase != 1 {
			return storageEnrollmentPeakEnvelope{}, 0, storagebudget.ErrBounds
		}
	} else if low.Kind == 1 {
		if high.Marker.Phase != 1 {
			return storageEnrollmentPeakEnvelope{}, 0, storagebudget.ErrBounds
		}
	}
	want := 1 - int(high.Scope[322])
	if high.Marker.Phase == 0 || high.Marker.Phase == 2 {
		want = 1 - want
	}
	if high.Scope.path() == 2 {
		want = 1 - want
	}
	if bank != want {
		return storageEnrollmentPeakEnvelope{}, 0, storagebudget.ErrBounds
	}
	return high, bank, nil
}

func storageEnrollmentPeakCompareSnapshot(current storageEnrollmentPeakEnvelope, previous storageEnrollmentPeakEvidence, record storageOwnerRecordV2, claim storageOwnerClaimV2) error {
	if current.Snapshot == nil || current.Snapshot.Kind != 1 || previous.Phase != 2 {
		return storagebudget.ErrBounds
	}
	x := current.Snapshot
	if sha256.Sum256(previous.Raw) != x.EvidenceDigest || x.Evidence != previous.Controls[x.Bank] || x.Bank != previous.Bank || x.Extent != previous.Extent || x.Q3Revision != previous.AfterRevision || x.Q3Epoch != previous.AfterEpoch || storageEnrollmentPeakInnerDigest(previous.Inner.Raw) != x.Inner {
		return storagebudget.ErrBounds
	}
	old, err := storageEnrollmentPeakDecodeEnvelopeVariant(x.Before, record, claim, true, x.Encoding == 4 || x.Encoding == 6 || previous.Version >= 2)
	if err != nil || storageEnrollmentPeakJoinMarker(old, previous, previous.Ready) != nil {
		return storagebudget.ErrBounds
	}
	return nil
}
func storageEnrollmentPeakMatchCompleteOriginRows(e storageEnrollmentPeakEvidence) bool {
	return storageEnrollmentPeakMatchCompleteOriginRowsOperands(e, nil)
}
func storageEnrollmentPeakMatchCompleteOriginRowsOperands(e storageEnrollmentPeakEvidence, operands *storageEnrollmentOperands) bool {
	if len(e.Views) > len(e.Board.Origins)+2 || len(e.Projections) > len(e.Board.Origins)+2 {
		return false
	}
	if e.Scope.path() != 1 {
		if e.Board.Prior.Epoch != e.BeforeEpoch || e.Board.Prior.Q3Revision > e.BeforeRevision {
			matched := false
			if e.Version == 3 {
				for _, p := range e.Projections {
					if storageEnrollmentPeakProjectionFullDigest(p) == e.Before && storagePolicyProjectionQBoundary(p, e.BeforeEpoch, e.BeforeRevision) {
						matched = true
					}
				}
			}
			if !matched {
				return false
			}
		}
		found := false
		for _, p := range e.Projections {
			if p.Kind != 1 || operands.projectionDigest(p) != e.Before {
				continue
			} // The full compact marker was already validated by the projection reader.
			markerRaw := p.SavedOuter[64+4160:]
			m, err := storageEnrollmentPeakReadMarker(markerRaw, func() storageEnrollmentPeakScope {
				var s storageEnrollmentPeakScope
				copy(s[:], p.SavedOuter[128:4224])
				return s
			}())
			if err != nil || m.Bridge != e.Board.Prior.Bridge || m.Q3Revision != e.Board.Prior.Q3Revision || m.Q3Epoch != e.Board.Prior.Epoch || m.Evidence != e.Board.Prior.Evidence {
				return false
			}
			found = true
		}
		if !found {
			return false
		}
	}
	return true
}

// This is the complete pure comparison entry point for the selected outer
// reference and provided original snapshots. It deliberately has no boolean
// "qualified"/"approved" shortcut and returns no live effect handle.
type storageEnrollmentPeakComparisonInput struct {
	Catalogs           [2][]byte
	Evidence           []byte
	Dispatch           [2][]byte
	CurrentCheckpoints [2][]byte
	CurrentTickets     [2][]byte
	Original           storageEnrollmentPeakOriginalComparison
	PreviousEvidence   []byte
	ArmedEvidence      []byte
	Prefix             storageEnrollmentPeakPrefix
}
type storageEnrollmentPeakComparison struct {
	Envelope     storageEnrollmentPeakEnvelope
	CatalogBank  int
	Evidence     storageEnrollmentPeakEvidence
	Dispatch     storageEnrollmentPeakDispatch
	DispatchBank int
	Q            storageEnrollmentPeakQComparison
}

func storageEnrollmentPeakCompare(input storageEnrollmentPeakComparisonInput, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakComparison, error) {
	return storageEnrollmentPeakCompareJoined(input, record, claim, nil)
}

// The ordinary comparison remains strict. Only original fixed-file recovery
// supplies the complete phase-boundary operands to this shared engine.
func storageEnrollmentPeakCompareJoined(input storageEnrollmentPeakComparisonInput, record storageOwnerRecordV2, claim storageOwnerClaimV2, boundary *storageEnrollmentPeakCurrentBoundary) (storageEnrollmentPeakComparison, error) {
	return storageEnrollmentPeakCompareOperands(input, record, claim, boundary, nil)
}
func storageEnrollmentPeakCompareOperands(input storageEnrollmentPeakComparisonInput, record storageOwnerRecordV2, claim storageOwnerClaimV2, boundary *storageEnrollmentPeakCurrentBoundary, operands *storageEnrollmentOperands) (storageEnrollmentPeakComparison, error) {
	var out storageEnrollmentPeakComparison
	var retained *storageCompositionLineage
	cores := input.Catalogs
	var e storageEnrollmentPeakEnvelope
	var bank int
	var err error
	native := storagePolicyActualPair(input.Catalogs)
	wrapped := len(input.Catalogs[0]) == storageOwnerCatalogBytes && string(input.Catalogs[0][:8]) == "DSKONL1\x00"
	if native {
		selected := byte(0)
		if storageEnrollmentPeakU64(input.Catalogs[1][16:24]) > storageEnrollmentPeakU64(input.Catalogs[0][16:24]) {
			selected = 1
		}
		v, e0 := storageOwnerV6NativeDecode(input.Catalogs[selected], record, claim)
		if e0 != nil {
			return out, e0
		}
		lineage := v.Stage.Lineage
		retained = &lineage
		var before *storageOwnerCatalogV2
		if len(input.PreviousEvidence) != 0 {
			p, e0 := storageEnrollmentPeakDecodeEvidenceWithLineage(input.PreviousEvidence, retained, record, claim)
			if e0 != nil {
				return out, e0
			}
			before = &p.Inner.Catalog
		}
		pair, e0 := storageOwnerV6NativeReadPair(input.Catalogs[0], input.Catalogs[1], before, input.PreviousEvidence, record, claim)
		if e0 != nil {
			return out, e0
		}
		bank = int(pair.Selected)
		cores = [2][]byte{pair.Banks[0].Stage.Core, pair.Banks[1].Stage.Core}
		e, err = storageEnrollmentPeakDecodeEnvelopeNative(cores[bank], record, claim, true, true, true)
	} else if wrapped {
		pair, e0 := operands.composedPair(input.Catalogs, record, claim)
		if e0 != nil {
			return out, e0
		}
		bank = int(pair.Selected)
		lineage := pair.Banks[pair.Selected].Lineage
		retained = &lineage
		cores = [2][]byte{pair.Banks[0].Core, pair.Banks[1].Core}
		e, err = operands.envelope(cores[bank], record, claim, true, true)
	} else {
		e, bank, err = storageEnrollmentPeakDecodePairOperands(cores[0], cores[1], record, claim, false, operands)
	}
	if err != nil {
		return out, err
	}
	out.Envelope = e
	out.CatalogBank = bank
	if e.Kind != 2 { // Complete prefix comparison still returns no Q authority.
		if len(input.Evidence) != 0 || len(input.ArmedEvidence) != 0 || len(input.Dispatch[0]) != 0 || len(input.Dispatch[1]) != 0 || storageEnrollmentPeakComparePrefix(input, e, record, claim) != nil {
			return storageEnrollmentPeakComparison{}, storagebudget.ErrBounds
		}
		return out, nil
	}
	if len(input.Prefix.Catalog)+len(input.Prefix.Projection)+len(input.Prefix.Tickets)+len(input.Prefix.Views)+len(input.Prefix.Checkpoint) != 0 || input.Prefix.TicketCount != 0 || input.Prefix.ViewCount != 0 || input.Prefix.SourceKind != 0 || len(input.Prefix.Source) != 0 || input.Prefix.FreshOrigin != (storagebudget.ID{}) || len(input.Prefix.OriginalStep3) != 0 {
		return storageEnrollmentPeakComparison{}, storagebudget.ErrBounds
	}

	selected, err := operands.evidence(input.Evidence, retained, record, claim)
	if err != nil || storageEnrollmentPeakJoinMarker(e, selected, input.Original.Ready) != nil || !storageEnrollmentPeakMatchCompleteOriginRowsOperands(selected, operands) {
		return storageEnrollmentPeakComparison{}, storagebudget.ErrBounds
	}
	if storageEnrollmentPeakCompareOriginalOperands(selected, input.Original, record, claim, operands) != nil {
		return storageEnrollmentPeakComparison{}, storagebudget.ErrBounds
	}
	if selected.Phase == 2 {
		armed, err := operands.evidence(input.ArmedEvidence, retained, record, claim)
		if err != nil || !storageEnrollmentPeakJoinArmed(selected, armed) {
			return storageEnrollmentPeakComparison{}, storagebudget.ErrBounds
		}
		var peer storageEnrollmentPeakEnvelope
		if native {
			peer, err = storageEnrollmentPeakDecodeEnvelopeNative(cores[1-bank], record, claim, true, true, true)
		} else {
			peer, err = operands.envelope(cores[1-bank], record, claim, true, wrapped)
		}
		if err != nil {
			return storageEnrollmentPeakComparison{}, err
		}
		if peer.Marker != nil && peer.Marker.Phase == 2 && storageEnrollmentPeakJoinMarker(peer, armed, input.Original.Ready) != nil {
			return storageEnrollmentPeakComparison{}, storagebudget.ErrBounds
		}
	} else if len(input.ArmedEvidence) != 0 {
		return storageEnrollmentPeakComparison{}, storagebudget.ErrBounds
	}
	out.Evidence = selected
	var previous *storageEnrollmentPeakEvidence
	if len(input.PreviousEvidence) != 0 {
		p, err := operands.evidence(input.PreviousEvidence, retained, record, claim)
		if err != nil {
			return storageEnrollmentPeakComparison{}, err
		}
		previous = &p
	}
	if selected.Scope.path() != 1 && previous == nil {
		if boundary == nil || storageEnrollmentPeakValidateCurrentBoundary(*boundary, input, selected, record, claim) != nil {
			return storageEnrollmentPeakComparison{}, storagebudget.ErrBounds
		}
	}
	dispatch, dBank, err := storageEnrollmentPeakDecodeDispatchPair(input.Dispatch[0], input.Dispatch[1], selected, previous)
	if err != nil && previous == nil && boundary != nil {
		dispatch, dBank, err = storageEnrollmentPeakDecodeBoundaryDispatch(input.Dispatch, selected, record, claim)
	}
	if err != nil {
		return storageEnrollmentPeakComparison{}, err
	}
	q, err := storageEnrollmentPeakCompareQOperands(selected, input.CurrentCheckpoints, input.CurrentTickets, dispatch, operands)
	if err != nil {
		return storageEnrollmentPeakComparison{}, err
	}
	out.Dispatch = dispatch
	out.DispatchBank = dBank
	if e.Marker.Phase == 0 && q.Stage != 0 || e.Marker.Phase == 1 && q.Stage > 0 && !bytes.Equal(input.Catalogs[0], input.Catalogs[1]) || e.Marker.Phase == 2 && q.Stage < 2 || e.Marker.Phase == 3 && q.Stage != 3 {
		return storageEnrollmentPeakComparison{}, storagebudget.ErrBounds
	}
	if selected.Scope.path() != 1 {
		if previous != nil {
			if !storageEnrollmentPeakOriginalPeerRetained(selected.Scope, previous.Scope) || previous.Phase != 2 || sha256.Sum256(previous.Raw) != selected.Board.Prior.Evidence || !storageEnrollmentPeakRetainsOriginsOperands(selected, *previous, operands) || !bytes.Equal(selected.OriginalV3, previous.OriginalV3) {
				return storageEnrollmentPeakComparison{}, storagebudget.ErrBounds
			}
			old, _, err := storageEnrollmentPeakDecodeCurrentPairOperands(input.Original.Catalogs, selected, record, claim, operands)
			if err != nil || storageEnrollmentPeakJoinMarker(old, *previous, previous.Ready) != nil {
				return storageEnrollmentPeakComparison{}, storagebudget.ErrBounds
			}
		}
	}
	out.Q = q
	return out, nil
}

// A still-selected retained raw host history keeps its complete original view,
// catalog projection and source origin. Row references may be rebased, and an
// evicted/reused host slot need not retain the old no-longer-referenced origin.
func storageEnrollmentPeakRetainsOrigins(current, previous storageEnrollmentPeakEvidence) bool {
	return storageEnrollmentPeakRetainsOriginsOperands(current, previous, nil)
}
func storageEnrollmentPeakRetainsOriginsOperands(current, previous storageEnrollmentPeakEvidence, operands *storageEnrollmentOperands) bool {
	for _, w := range current.Witnesses {
		if !w.Selected {
			continue
		}
		for _, old := range previous.Witnesses {
			if !old.Selected || old.Slot != w.Slot || !bytes.Equal(old.Page, w.Page) {
				continue
			}
			if int(w.Origin) >= len(current.Board.Origins) || int(old.Origin) >= len(previous.Board.Origins) {
				return false
			}
			a, b := current.Board.Origins[w.Origin], previous.Board.Origins[old.Origin]
			if a.ID != b.ID || a.Bridge != b.Bridge || a.Epoch != b.Epoch || a.Revision != b.Revision {
				return false
			}
			av, bv := current.Views[a.View], previous.Views[b.View]
			aq, err := storageEnrollmentPeakViewCheckpoint(av, current.Inner.Catalog)
			if err != nil {
				return false
			}
			bq, err := storageEnrollmentPeakViewCheckpoint(bv, previous.Inner.Catalog)
			if err != nil || !reflect.DeepEqual(aq, bq) || !reflect.DeepEqual(av.Slots, bv.Slots) {
				return false
			}
			ap, bp := current.Projections[a.Projection], previous.Projections[b.Projection]
			if operands.projectionDigest(ap) != operands.projectionDigest(bp) || !reflect.DeepEqual(ap.Catalog, bp.Catalog) || !bytes.Equal(ap.SavedOuter, bp.SavedOuter) {
				return false
			}
			for _, peer := range previous.Witnesses {
				if peer.Slot != old.Slot {
					continue
				}
				found := false
				for _, next := range current.Witnesses {
					if next.Slot == peer.Slot && next.Bank == peer.Bank && bytes.Equal(next.Page, peer.Page) {
						found = true
					}
				}
				if !found {
					return false
				}
			}
		}
	}
	return true
}
