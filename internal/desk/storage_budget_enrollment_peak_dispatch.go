package desk

import (
	"bytes"
	"crypto/sha256"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

type storageEnrollmentPeakDispatch struct {
	Generation                  uint64
	Installation                storagebudget.ID
	Binding                     storagebudget.Digest
	Bridge                      storagebudget.ID
	Previous                    storagebudget.Digest
	Mask, Step                  uint8
	BeforeRevision, BeforeEpoch uint64
	Before, After               storagebudget.Digest
	Origin                      storagebudget.Digest
	Controls                    [2]storageOwnerPersistentObject
	Raw                         []byte
}

func storageEnrollmentPeakDecodeDispatch(raw []byte, e storageEnrollmentPeakEvidence) (storageEnrollmentPeakDispatch, error) {
	var d storageEnrollmentPeakDispatch
	if len(raw) != 4096 {
		return d, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw[:192]}
	if !bytes.Equal(r.take(8), []byte("DSKEPS1\x00")) || r.u32() != 1 || r.u32() != 192 {
		return d, storagebudget.ErrBounds
	}
	d.Generation = r.u64()
	d.Installation = r.id()
	d.Binding = r.digest()
	d.Bridge = r.id()
	d.Previous = r.digest()
	if r.u32() != 256 || r.u32() != 0 {
		return d, storagebudget.ErrBounds
	}
	checksumOffset := r.pos
	checksum := r.digest()
	if !r.zeroTail() || !storageEnrollmentPeakZero(raw[448:]) {
		return d, storagebudget.ErrBounds
	}
	h := sha256.New()
	h.Write([]byte("desk-enrollment-step-peak-v1\x00"))
	h.Write(raw[:checksumOffset])
	h.Write(make([]byte, 32))
	h.Write(raw[checksumOffset+32:])
	if !bytes.Equal(checksum[:], h.Sum(nil)) {
		return d, storagebudget.ErrBounds
	}
	r = storageEnrollmentPeakReader{raw: raw[192:448]}
	d.Mask = r.u8()
	d.Step = r.u8()
	reserved := r.u16()
	d.BeforeRevision = r.u64()
	d.BeforeEpoch = r.u64()
	d.Before = r.digest()
	d.After = r.digest()
	bridge := r.id()
	d.Origin = r.digest()
	d.Controls[0] = r.native()
	d.Controls[1] = r.native()
	masks := [4]byte{0, 1, 3, 7}
	if !r.zeroTail() || reserved != 0 || d.Generation == 0 || d.Step > 3 || d.Mask != masks[d.Step] || d.Installation != e.Installation || d.Binding != e.QBinding || d.Bridge != e.Bridge || bridge != e.Bridge || d.Origin != e.Digests.Origin || d.Controls[0] != e.Controls[2] || d.Controls[1] != e.Controls[3] || e.Phase == 0 && d.Step != 0 || d.Generation == 1 && d.Previous != (storagebudget.Digest{}) || d.Generation > 1 && d.Previous == (storagebudget.Digest{}) {
		return storageEnrollmentPeakDispatch{}, storagebudget.ErrBounds
	}
	beforeRev, beforeEpoch := e.BeforeRevision, e.BeforeEpoch
	before := e.Checkpoint.Checkpoints[e.Checkpoint.Bank]
	after := storagebudget.Digest{}
	if d.Step > 0 {
		after = e.Digests.Q[d.Step-1]
		if after == (storagebudget.Digest{}) {
			return storageEnrollmentPeakDispatch{}, storagebudget.ErrBounds
		}
		if d.Step > 1 {
			before = e.Digests.Q[d.Step-2]
			beforeRev += uint64(d.Step - 1)
			beforeEpoch++
		}
	}
	if d.BeforeRevision != beforeRev || d.BeforeEpoch != beforeEpoch || d.Before != before || d.After != after {
		return storageEnrollmentPeakDispatch{}, storagebudget.ErrBounds
	}
	d.Raw = append([]byte{}, raw...)
	return d, nil
}
func storageEnrollmentPeakEncodeDispatch(d storageEnrollmentPeakDispatch, e storageEnrollmentPeakEvidence) ([]byte, error) {
	w := storageEnrollmentPeakWriter{max: 192}
	w.put([]byte("DSKEPS1\x00"))
	w.u32(1)
	w.u32(192)
	w.u64(d.Generation)
	w.put(d.Installation[:])
	w.put(d.Binding[:])
	w.put(d.Bridge[:])
	w.put(d.Previous[:])
	w.u32(256)
	w.u32(0)
	checksumOffset := len(w.raw)
	w.put(make([]byte, 32))
	raw := make([]byte, 4096)
	copy(raw, w.raw)
	body := storageEnrollmentPeakWriter{max: 256}
	body.u8(d.Mask)
	body.u8(d.Step)
	body.u16(0)
	body.u64(d.BeforeRevision)
	body.u64(d.BeforeEpoch)
	body.put(d.Before[:])
	body.put(d.After[:])
	body.put(d.Bridge[:])
	body.put(d.Origin[:])
	for _, p := range d.Controls {
		body.put(storageEnrollmentPeakNativeBytes(p))
	}
	if w.bad || body.bad {
		return nil, storagebudget.ErrBounds
	}
	copy(raw[192:], body.raw)
	digest := storageEnrollmentPeakHash("desk-enrollment-step-peak-v1", raw)
	copy(raw[checksumOffset:checksumOffset+32], digest[:])
	if _, err := storageEnrollmentPeakDecodeDispatch(raw, e); err != nil {
		return nil, err
	}
	return raw, nil
}
func storageEnrollmentPeakDecodeDispatchPair(a, b []byte, current storageEnrollmentPeakEvidence, previous *storageEnrollmentPeakEvidence) (storageEnrollmentPeakDispatch, int, error) {
	var pages [2]storageEnrollmentPeakDispatch
	for i, raw := range [][]byte{a, b} {
		context := current
		if len(raw) != 4096 {
			return storageEnrollmentPeakDispatch{}, 0, storagebudget.ErrBounds
		}
		if !bytes.Equal(raw[72:88], current.Bridge[:]) {
			if previous == nil || previous.Phase != 2 || !bytes.Equal(raw[72:88], previous.Bridge[:]) {
				return storageEnrollmentPeakDispatch{}, 0, storagebudget.ErrBounds
			}
			context = *previous
		}
		d, err := storageEnrollmentPeakDecodeDispatch(raw, context)
		if err != nil {
			return storageEnrollmentPeakDispatch{}, 0, err
		}
		pages[i] = d
	}
	if pages[0].Generation == pages[1].Generation {
		if !bytes.Equal(a, b) || pages[0].Bridge != current.Bridge {
			return storageEnrollmentPeakDispatch{}, 0, storagebudget.ErrBounds
		}
		return pages[0], 0, nil
	}
	bank := 0
	if pages[1].Generation > pages[0].Generation {
		bank = 1
	}
	low, high := pages[1-bank], pages[bank]
	if low.Generation == ^uint64(0) || high.Generation != low.Generation+1 || high.Previous != sha256.Sum256(low.Raw) || high.Bridge != current.Bridge {
		return storageEnrollmentPeakDispatch{}, 0, storagebudget.ErrBounds
	}
	if low.Bridge == high.Bridge {
		if high.Step != low.Step+1 {
			return storageEnrollmentPeakDispatch{}, 0, storagebudget.ErrBounds
		}
	} else if low.Step != 3 || high.Step != 0 || previous == nil || low.Bridge != previous.Bridge {
		return storageEnrollmentPeakDispatch{}, 0, storagebudget.ErrBounds
	}
	return high, bank, nil
}

// The classifier is deliberately not a permission or replay API. A consumed
// dispatch with exact actual-before returns UnresolvedQAttempt, never a retry.
type storageEnrollmentPeakQComparison struct {
	Stage              int
	UnresolvedQAttempt bool
}

func storageEnrollmentPeakCompareQ(e storageEnrollmentPeakEvidence, checkpoints [2][]byte, tickets [2][]byte, dispatch storageEnrollmentPeakDispatch) (storageEnrollmentPeakQComparison, error) {
	return storageEnrollmentPeakCompareQOperands(e, checkpoints, tickets, dispatch, nil)
}
func storageEnrollmentPeakCompareQOperands(e storageEnrollmentPeakEvidence, checkpoints [2][]byte, tickets [2][]byte, dispatch storageEnrollmentPeakDispatch, operands *storageEnrollmentOperands) (storageEnrollmentPeakQComparison, error) {
	var result storageEnrollmentPeakQComparison
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(e.Installation), Binding: storageEnrollmentPeakQDigest(e.QBinding)}
	var q [2]*storageEnrollmentPeakQCheckpoint
	var checkpointDigests [2]storagebudget.Digest
	for i := range q {
		decoded, err := operands.checkpoint(checkpoints[i], anchor)
		if err != nil {
			return result, err
		}
		q[i] = decoded.Value
		checkpointDigests[i] = decoded.Digest
		if len(tickets[i]) != 64*16384 {
			return result, storagebudget.ErrBounds
		}
	}
	bank := 0
	if q[1].Generation > q[0].Generation {
		bank = 1
	}
	if q[bank].Generation-q[1-bank].Generation > 1 || q[0].Generation == q[1].Generation && !bytes.Equal(checkpoints[0], checkpoints[1]) {
		return result, storagebudget.ErrBounds
	}
	stage := -1
	expected := [][]byte{e.Checkpoint.Checkpoint, e.PredictionBytes[0], e.PredictionBytes[1], e.PredictionBytes[2]}
	for i, raw := range expected {
		if len(raw) != 0 && bytes.Equal(raw, checkpoints[bank]) {
			stage = i
		}
	}
	if stage < 0 || e.Phase == 0 && stage != 0 || e.Phase == 2 && stage != 3 {
		return result, storagebudget.ErrBounds
	}
	wantBank := e.Checkpoint.Bank
	if stage%2 == 1 {
		wantBank = 1 - wantBank
	}
	if bank != int(wantBank) {
		return result, storagebudget.ErrBounds
	}
	if stage == 0 {
		for i := 0; i < 2; i++ {
			if checkpointDigests[i] != e.Checkpoint.Checkpoints[i] {
				return result, storagebudget.ErrBounds
			}
		}
	} else if !bytes.Equal(checkpoints[1-bank], expected[stage-1]) {
		return result, storagebudget.ErrBounds
	}
	for i := 0; i < 64; i++ {
		var values [2]storageEnrollmentPeakQTicket
		for j := 0; j < 2; j++ {
			page := tickets[j][i*16384 : (i+1)*16384]
			decoded, err := operands.ticket(page, i, anchor)
			if err != nil {
				return result, err
			}
			if decoded.Digest != e.Checkpoint.Tickets[i][j] {
				return result, storagebudget.ErrBounds
			}
			t := decoded.Value
			values[j] = t
		}
		selected := 0
		if values[1].Token.Generation > values[0].Token.Generation {
			selected = 1
		}
		high, low := values[selected], values[1-selected]
		if high.Token.Generation-low.Token.Generation > 1 || high.Token.Generation == low.Token.Generation && !bytes.Equal(tickets[0][i*16384:(i+1)*16384], tickets[1][i*16384:(i+1)*16384]) || high.Token.Generation != low.Token.Generation && storageEnrollmentPeakQLegalTransition(low, high, q[bank].Applied[i]) != nil || selected != int(e.Checkpoint.TicketBanks[i]) || high.State != storageEnrollmentPeakQEmpty && !storageEnrollmentPeakQRetired(high, q[bank].Applied[i]) {
			return result, storagebudget.ErrBounds
		}
	}
	if dispatch.Bridge != e.Bridge || int(dispatch.Step) < stage || int(dispatch.Step) > stage+1 {
		return result, storagebudget.ErrBounds
	}
	result.Stage = stage
	result.UnresolvedQAttempt = int(dispatch.Step) == stage+1
	return result, nil
}
func storageEnrollmentPeakQLegalTransition(old, next storageEnrollmentPeakQTicket, a storageEnrollmentPeakQApplied) error {
	if old.Token.Generation == ^uint64(0) || next.Token.Generation != old.Token.Generation+1 {
		return storagebudget.ErrUnknown
	}
	if old.State == storageEnrollmentPeakQEmpty || storageEnrollmentPeakQRetired(old, a) {
		if next.State != storageEnrollmentPeakQReserved || next.Token.Sequence != a.NextSequence || next.Token.ID.zero() {
			return storagebudget.ErrUnknown
		}
		return nil
	}
	if old.Token.ID != next.Token.ID || old.Token.Sequence != next.Token.Sequence || old.Owner != next.Owner || old.Workflow != next.Workflow || old.Operation != next.Operation || old.PolicyRevision != next.PolicyRevision || old.ManifestLength != next.ManifestLength || old.Manifest != next.Manifest || !storageEnrollmentPeakQSameFanout(old.Fanout, next.Fanout) || len(old.Components) != len(next.Components) || old.State == storageEnrollmentPeakQTerminal {
		return storagebudget.ErrUnknown
	}
	for i, v := range old.Components {
		if v != next.Components[i] {
			return storagebudget.ErrUnknown
		}
	}
	if next.State == storageEnrollmentPeakQTerminal || next.State == storageEnrollmentPeakQUncertain || old.State == storageEnrollmentPeakQReserved && next.State == storageEnrollmentPeakQStaging || old.State == storageEnrollmentPeakQStaging && next.State == storageEnrollmentPeakQDispatched {
		return nil
	}
	return storagebudget.ErrUnknown
}

type storageEnrollmentPeakOriginalComparison struct {
	Ready       storagebudget.Digest
	Project     storageOwnerPersistentObject
	Catalogs    [2][]byte
	Checkpoints [2][]byte
	Tickets     [2][]byte
	Hosts       [64][2][]byte
	Targets     [8]storageOwnerPersistentObject
	Extents     [8]uint64
}

func storageEnrollmentPeakCompareOriginal(e storageEnrollmentPeakEvidence, c storageEnrollmentPeakOriginalComparison, record storageOwnerRecordV2, claim storageOwnerClaimV2) error {
	return storageEnrollmentPeakCompareOriginalOperands(e, c, record, claim, nil)
}
func storageEnrollmentPeakCompareOriginalOperands(e storageEnrollmentPeakEvidence, c storageEnrollmentPeakOriginalComparison, record storageOwnerRecordV2, claim storageOwnerClaimV2, operands *storageEnrollmentOperands) error {
	if int(e.Bank) > 1 || c.Extents[e.Bank] != e.Extent || c.Ready == (storagebudget.Digest{}) || c.Ready != e.Ready || c.Project != e.Project {
		return storagebudget.ErrBounds
	}
	for i, p := range c.Targets {
		d := e.Scope[1360+i*32 : 1360+(i+1)*32]
		before, final := storageEnrollmentPeakU64(d[4:12]), storageEnrollmentPeakU64(d[12:20])
		if p != e.Scope.target(byte(i+1)) || c.Extents[i] > final || c.Extents[i] < before || !(e.Phase == 0 && (e.Scope.path() == 1 || e.Scope.path() == 3)) && c.Extents[i] != final {
			return storagebudget.ErrBounds
		}
	}
	nativeBefore := e.Version == 3
	if nativeBefore {
		pair, selected, err := storagePolicyEvidenceNativeBefore(e, record, claim)
		if err != nil || selected != e.Scope[322] || e.Scope.path() == 1 || !bytes.Equal(pair[0], c.Catalogs[0]) || !bytes.Equal(pair[1], c.Catalogs[1]) || sha256.Sum256(pair[selected]) != e.Before {
			return storagebudget.ErrBounds
		}
	}
	physicalBefore := e.Version == 2
	if physicalBefore {
		pair, err := operands.composition(e, record, claim)
		if err != nil || !bytes.Equal(c.Catalogs[0], pair.Banks[0].Raw) || !bytes.Equal(c.Catalogs[1], pair.Banks[1].Raw) {
			return storagebudget.ErrBounds
		}
		c.Catalogs = [2][]byte{pair.Banks[0].Core, pair.Banks[1].Core}
	}
	if nativeBefore {
		// Exact actual two-bank reconstruction above replaces only the legacy
		// duplicate-core comparison, never original Q/host/native joins below.
	} else if e.Scope.path() == 1 {
		pair, err := operands.catalog(c.Catalogs, record, claim)
		if err != nil {
			return err
		}
		bank := 1
		if storageEnrollmentPeakU64(c.Catalogs[0][16:24]) > storageEnrollmentPeakU64(c.Catalogs[1][16:24]) {
			bank = 0
		}
		if physicalBefore && bytes.Equal(c.Catalogs[0], c.Catalogs[1]) {
			bank = int(e.Scope[322])
		}
		if e.Scope.hasOriginalPeer() {
			peer, err := operands.peer(e.Scope, e.OriginalV3, record, claim, physicalBefore)
			restored := peer.Raw
			if err != nil || !bytes.Equal(restored[0], c.Catalogs[0]) || !bytes.Equal(restored[1], c.Catalogs[1]) {
				return storagebudget.ErrBounds
			}
		}
		if pair.High.Sequence != storageEnrollmentPeakU64(e.Scope[1624:1632]) || bank != int(e.Scope[322]) || sha256.Sum256(c.Catalogs[bank]) != e.Before {
			return storagebudget.ErrBounds
		}
	} else {
		old, bank, err := storageEnrollmentPeakDecodePairOperands(c.Catalogs[0], c.Catalogs[1], record, claim, physicalBefore, operands)
		if physicalBefore && bytes.Equal(c.Catalogs[0], c.Catalogs[1]) {
			bank = int(e.Scope[322])
		}
		if err != nil || old.Kind != 2 || old.Marker == nil || old.Marker.Phase != 3 || !bytes.Equal(c.Catalogs[0], c.Catalogs[1]) || bank != int(e.Scope[322]) || sha256.Sum256(c.Catalogs[bank]) != e.Before {
			return storagebudget.ErrBounds
		}
	}
	if e.Scope.hasOriginalPeer() {
		restored, err := storageEnrollmentPeakRestoreOriginalCheckpointsOperands(e.Checkpoint, e.Inner.Catalog, operands)
		if err != nil || !bytes.Equal(restored[0], c.Checkpoints[0]) || !bytes.Equal(restored[1], c.Checkpoints[1]) {
			return storagebudget.ErrBounds
		}
	}

	if operands == nil {
		for i := 0; i < 2; i++ {
			if sha256.Sum256(c.Checkpoints[i]) != e.Checkpoint.Checkpoints[i] {
				return storagebudget.ErrBounds
			}
			for slot := 0; slot < 64; slot++ {
				if len(c.Tickets[i]) != 64*16384 || sha256.Sum256(c.Tickets[i][slot*16384:(slot+1)*16384]) != e.Checkpoint.Tickets[slot][i] {
					return storagebudget.ErrBounds
				}
			}
		}
	} else {
		for i := 0; i < 2; i++ {
			anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(e.Installation), Binding: storageEnrollmentPeakQDigest(e.QBinding)}
			cp, decodeErr := operands.checkpoint(c.Checkpoints[i], anchor)
			if decodeErr != nil || cp.Digest != e.Checkpoint.Checkpoints[i] {
				return storagebudget.ErrBounds
			}
			for slot := 0; slot < 64; slot++ {
				if len(c.Tickets[i]) != 64*16384 {
					return storagebudget.ErrBounds
				}
				ticket, decodeErr := operands.ticket(c.Tickets[i][slot*16384:(slot+1)*16384], slot, anchor)
				if decodeErr != nil || ticket.Digest != e.Checkpoint.Tickets[slot][i] {
					return storagebudget.ErrBounds
				}
			}
		}
	}
	comparisonContext := e
	comparisonContext.Phase = 1
	selected, err := storageEnrollmentPeakCompareQOperands(comparisonContext, c.Checkpoints, c.Tickets, storageEnrollmentPeakDispatch{Bridge: e.Bridge, Step: 0}, operands)
	if err != nil || selected.Stage != 0 {
		return storagebudget.ErrBounds
	}
	for slot, pages := range c.Hosts {
		for bank, page := range pages {
			if len(page) != 16384 {
				return storagebudget.ErrBounds
			}
			found := false
			for _, w := range e.Witnesses {
				if int(w.Slot) == slot && int(w.Bank) == bank {
					if !bytes.Equal(w.Page, page) {
						return storagebudget.ErrBounds
					}
					found = true
				}
			}
			if !found && !storageEnrollmentPeakZero(page) {
				return storagebudget.ErrBounds
			}
		}
	}
	return nil
}
func storageEnrollmentPeakJoinMarker(outer storageEnrollmentPeakEnvelope, e storageEnrollmentPeakEvidence, ready storagebudget.Digest) error {
	if outer.Kind != 2 || outer.Marker == nil || outer.Scope != e.Scope || sha256.Sum256(e.Raw) != outer.Marker.Evidence || outer.Marker.Bank != e.Bank || outer.Marker.Extent != e.Extent || outer.Marker.Inner != storageEnrollmentPeakInnerDigest(e.Inner.Raw) || outer.Marker.Record != e.Record || outer.Marker.Claim != e.Claim || outer.Marker.Ready != e.Ready || e.Ready != ready || outer.Marker.Q0Revision != e.BeforeRevision || outer.Marker.Q0Epoch != e.BeforeEpoch || outer.Marker.Q3Revision != e.AfterRevision || outer.Marker.Q3Epoch != e.AfterEpoch {
		return storagebudget.ErrBounds
	}
	m := outer.Marker
	if m.Phase == 0 && e.Bank != storageEnrollmentPeakEvidenceBank(e.Scope, 0) {
		return storagebudget.ErrBounds
	}
	if m.Phase == 0 && e.Phase != 0 || m.Phase == 1 && e.Phase != 1 || m.Phase == 2 && e.Phase != 1 || m.Phase == 3 && e.Phase != 2 {
		return storagebudget.ErrBounds
	}
	return nil
}
