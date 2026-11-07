package desk

import (
	"bytes"
	"crypto/sha256"
	"reflect"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Scope v2 uses exactly65 previously reserved bytes, without changing the4096
// byte extent. The tail belongs to the immutable founding v3 pair, not the
// immediately preceding peak marker. Kind1 is duplicate (v3 tie selects bank1),
// kind2 is linked active-high bank0, kind3 linked active-high bank1.
// The full peer digest is deliberately distinct from its predecessor checksum.
func storageEnrollmentPeakOriginalPeerTailValid(s storageEnrollmentPeakScope) bool {
	return storageEnrollmentPeakOriginalPeerTailValidVariant(s, false)
}

func storageEnrollmentPeakOriginalPeerTailValidVariant(s storageEnrollmentPeakScope, composition bool) bool {
	switch storageEnrollmentPeakU16(s[:2]) {
	case 1:
		return storageEnrollmentPeakZero(s[1640:])
	case 2, storageEnrollmentCapacityEstimateVersion:
		kind := s[1704]
		if kind < 1 || kind > 3 || storageEnrollmentPeakZero(s[1640:1672]) || !storageEnrollmentPeakZero(s[1705:]) {
			return false
		}
		bank := byte(1)
		if kind == 2 {
			bank = 0
		}
		return s.path() != 1 || s[322] == bank || composition && kind == 1 && s[322] == 0
	default:
		return false
	}
}

func storageEnrollmentPeakOriginalPeerRetained(next, before storageEnrollmentPeakScope) bool {
	return storageEnrollmentPeakU16(next[:2]) == storageEnrollmentPeakU16(before[:2]) && bytes.Equal(next[1640:1705], before[1640:1705])
}

// Called only with both actual original frames under original lifetime custody.
// Reconstruction below is pure byte evidence and never produces native custody.
func storageEnrollmentPeakRetainOriginalPeer(s *storageEnrollmentPeakScope, raws [2][]byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) error {
	if s == nil || !s.hasOriginalPeer() || s.path() != 1 {
		return storagebudget.ErrBounds
	}
	pair, err := storageOwnerDecodeCatalogPairV3(raws[0], raws[1], record, claim)
	if err != nil || pair.High.Phase != "active" {
		return storagebudget.ErrBounds
	}
	bank := byte(1)
	if storageEnrollmentPeakU64(raws[0][16:24]) > storageEnrollmentPeakU64(raws[1][16:24]) {
		bank = 0
	}
	if s[322] != bank || sha256.Sum256(raws[bank]) != storagebudget.Digest([32]byte(s[88:120])) {
		return storagebudget.ErrBounds
	}
	peer, err := storageOwnerDecodeCatalogV3(raws[1-bank], record, claim)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raws[1-bank])
	copy(s[1640:1672], digest[:])
	copy(s[1672:1704], peer.PreviousCatalogDigest[:])
	s[1704] = 1
	if pair.PeerLinked {
		s[1704] = 2 + bank
	}
	restored, _, err := storageEnrollmentPeakRestoreOriginalPeer(*s, raws[bank], record, claim)
	if err != nil || !bytes.Equal(restored[0], raws[0]) || !bytes.Equal(restored[1], raws[1]) {
		return storagebudget.ErrBounds
	}
	return nil
}

// V1 has no sufficient retained operand and explicitly refuses reconstruction.
// Only the existing prepared->active finalization relation is invertible. No
// path, guessed duplicate, digest preimage, active->prepared predecessor, or
// currently overwritten bank is used to supply missing historical information.
func storageEnrollmentPeakRestoreOriginalPeer(s storageEnrollmentPeakScope, selected []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([2][]byte, byte, error) {
	return storageEnrollmentPeakRestoreOriginalPeerVariant(s, selected, record, claim, false)
}

func storageEnrollmentPeakRestoreOriginalPeerVariant(s storageEnrollmentPeakScope, selected []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2, composition bool) ([2][]byte, byte, error) {
	var out [2][]byte
	fail := func() ([2][]byte, byte, error) { return [2][]byte{}, 0, storagebudget.ErrBounds }
	if !s.hasOriginalPeer() || !storageEnrollmentPeakOriginalPeerTailValidVariant(s, composition) {
		return fail()
	}
	high, err := storageOwnerDecodeCatalogV3(selected, record, claim)
	if err != nil || high.Phase != "active" {
		return fail()
	}
	canonical, err := storageOwnerEncodeCatalogV3(high, record, claim)
	if err != nil || !bytes.Equal(canonical, selected) {
		return fail()
	}
	bank := byte(1)
	if s[1704] == 2 {
		bank = 0
	}
	if s.path() == 1 && (high.Sequence != storageEnrollmentPeakU64(s[1624:1632]) || sha256.Sum256(selected) != storagebudget.Digest([32]byte(s[88:120]))) {
		return fail()
	}
	peer := append([]byte{}, selected...)
	if s[1704] != 1 {
		if high.Sequence < 2 {
			return fail()
		}
		low := high
		low.Sequence--
		low.Phase = "prepared"
		copy(low.PreviousCatalogDigest[:], s[1672:1704])
		peer, err = storageOwnerEncodeCatalogV3(low, record, claim)
		if err != nil {
			return fail()
		}
	} else if !bytes.Equal(high.PreviousCatalogDigest[:], s[1672:1704]) {
		return fail()
	}
	if sha256.Sum256(peer) != storagebudget.Digest([32]byte(s[1640:1672])) {
		return fail()
	}
	out[bank] = append([]byte{}, selected...)
	out[1-bank] = peer
	pair, err := storageOwnerDecodeCatalogPairV3(out[0], out[1], record, claim)
	if err != nil || pair.High.Phase != "active" || pair.Duplicated != (s[1704] == 1) || pair.PeerLinked != (s[1704] != 1) {
		return fail()
	}
	return out, bank, nil
}

// Compact independent Q peer bounds, from exact scalar widths (not JSON or
// compression): header16+maintenance1+policy488+installation216=721;
// four U16 counts=8; each occupied row includes physical index U16:
// root726=(16+64+32+8+2+256+1+216+1+96+32)+2,
// workspace483=(16+8+1+216+240)+2, filesystem123, constraint75;
// all64 applied rows40 each. Total3525721. The selected dictionary projection
// is <=1141656; v2's two U32 lengths add8 =>4667385 <4718592.
const storageEnrollmentPeakPeerCheckpointMax = 3525721
const storageEnrollmentPeakSelectedCheckpointMax = 1141656

func storageEnrollmentPeakWritePeerCheckpoint(q *storageEnrollmentPeakQCheckpoint) ([]byte, error) {
	if q == nil || storageEnrollmentPeakQValidateCheckpoint(q) != nil {
		return nil, storagebudget.ErrBounds
	}
	w := storageEnrollmentPeakWriter{max: storageEnrollmentPeakPeerCheckpointMax}
	w.u64(q.Generation)
	w.u64(q.Epoch)
	storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(q.Maintenance))
	var policy storageOwnerPolicy
	storageEnrollmentPeakCopy(reflect.ValueOf(&policy.Installation).Elem(), reflect.ValueOf(q.Policy.Installation))
	storageEnrollmentPeakCopy(reflect.ValueOf(&policy.Workspace).Elem(), reflect.ValueOf(q.Policy.Workspace))
	policy.ObservationMaxAgeNanos = uint64(q.Policy.ObservationMaxAge)
	storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(policy))
	storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(q.Installation))
	for _, array := range []reflect.Value{reflect.ValueOf(q.Roots), reflect.ValueOf(q.Workspaces), reflect.ValueOf(q.Filesystems), reflect.ValueOf(q.CapacityConstraints)} {
		count := 0
		for i := 0; i < array.Len(); i++ {
			if !array.Index(i).FieldByName("ID").IsZero() {
				count++
			}
		}
		w.u16(uint16(count))
		for i := 0; i < array.Len(); i++ {
			if array.Index(i).FieldByName("ID").IsZero() {
				continue
			}
			w.u16(uint16(i))
			storageEnrollmentPeakWriteFixed(&w, array.Index(i))
		}
	}
	for _, a := range q.Applied {
		storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(a))
	}
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	return w.raw, nil
}

func storageEnrollmentPeakReadPeerCheckpoint(raw []byte) (*storageEnrollmentPeakQCheckpoint, error) {
	if len(raw) > storageEnrollmentPeakPeerCheckpointMax {
		return nil, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw}
	q := &storageEnrollmentPeakQCheckpoint{Generation: r.u64(), Epoch: r.u64()}
	storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&q.Maintenance).Elem())
	var policy storageOwnerPolicy
	storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&policy).Elem())
	storageEnrollmentPeakCopy(reflect.ValueOf(&q.Policy.Installation).Elem(), reflect.ValueOf(policy.Installation))
	storageEnrollmentPeakCopy(reflect.ValueOf(&q.Policy.Workspace).Elem(), reflect.ValueOf(policy.Workspace))
	q.Policy.ObservationMaxAge = time.Duration(policy.ObservationMaxAgeNanos)
	storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&q.Installation).Elem())
	for _, array := range []reflect.Value{reflect.ValueOf(&q.Roots).Elem(), reflect.ValueOf(&q.Workspaces).Elem(), reflect.ValueOf(&q.Filesystems).Elem(), reflect.ValueOf(&q.CapacityConstraints).Elem()} {
		count := int(r.u16())
		if count > array.Len() {
			return nil, storagebudget.ErrBounds
		}
		previous := -1
		for i := 0; i < count; i++ {
			index := int(r.u16())
			if index <= previous || index >= array.Len() {
				return nil, storagebudget.ErrBounds
			}
			previous = index
			if root, ok := array.Index(index).Addr().Interface().(*storageEnrollmentPeakQRoot); ok {
				storageEnrollmentPeakReadPeerRoot(&r, root)
			} else {
				storageEnrollmentPeakReadFixed(&r, array.Index(index))
			}
			if array.Index(index).FieldByName("ID").IsZero() {
				return nil, storagebudget.ErrBounds
			}
		}
	}
	for i := range q.Applied {
		storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&q.Applied[i]).Elem())
	}
	if !r.done() || !policy.valid() || storageEnrollmentPeakQValidateCheckpoint(q) != nil {
		return nil, storagebudget.ErrBounds
	}
	canonical, err := storageEnrollmentPeakWritePeerCheckpoint(q)
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, storagebudget.ErrBounds
	}
	return q, nil
}

// Only the independent root row has a dynamic field. Its Locator keeps the
// original Q contract's 256-byte bound; the generic fixed reader stays closed.
func storageEnrollmentPeakReadPeerRoot(r *storageEnrollmentPeakReader, v *storageEnrollmentPeakQRoot) {
	storageEnrollmentPeakReadFixed(r, reflect.ValueOf(&v.ID).Elem())
	storageEnrollmentPeakReadFixed(r, reflect.ValueOf(&v.Owner).Elem())
	storageEnrollmentPeakReadFixed(r, reflect.ValueOf(&v.Identity).Elem())
	storageEnrollmentPeakReadFixed(r, reflect.ValueOf(&v.Filesystem).Elem())
	storageEnrollmentPeakReadFixed(r, reflect.ValueOf(&v.Workspace).Elem())
	v.Generation = r.u64()
	v.Locator = string(r.data(256))
	storageEnrollmentPeakReadFixed(r, reflect.ValueOf(&v.Known).Elem())
	storageEnrollmentPeakReadFixed(r, reflect.ValueOf(&v.Usage).Elem())
	v.CapacityCount = r.u8()
	storageEnrollmentPeakReadFixed(r, reflect.ValueOf(&v.CapacityRefs).Elem())
	storageEnrollmentPeakReadFixed(r, reflect.ValueOf(&v.CapacityBinding).Elem())
}

func storageEnrollmentPeakRestoreOriginalCheckpoints(p storageEnrollmentPeakCheckpointProjection, c storageOwnerCatalogV2) ([2][]byte, error) {
	return storageEnrollmentPeakRestoreOriginalCheckpointsOperands(p, c, nil)
}
func storageEnrollmentPeakRestoreOriginalCheckpointsOperands(p storageEnrollmentPeakCheckpointProjection, c storageOwnerCatalogV2, operands *storageEnrollmentOperands) ([2][]byte, error) {
	var out [2][]byte
	if p.Bank > 1 || len(p.Checkpoint) != 4194304 || len(p.PeerCheckpoint) != 4194304 {
		return out, storagebudget.ErrBounds
	}
	out[p.Bank] = p.Checkpoint
	out[1-p.Bank] = p.PeerCheckpoint
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(c.Installation), Binding: storageEnrollmentPeakQDigest(c.QBinding)}
	var q [2]storageEnrollmentCheckpointFacts
	for i := range q {
		var err error
		decoded, decodeErr := operands.checkpointFacts(out[i], anchor)
		err = decodeErr
		q[i] = decoded
		if err != nil || decoded.Digest != p.Checkpoints[i] {
			return [2][]byte{}, storagebudget.ErrBounds
		}
	}
	bank := 0
	if q[1].Generation > q[0].Generation {
		bank = 1
	}
	if bank != int(p.Bank) || q[bank].Generation-q[1-bank].Generation > 1 || q[0].Generation == q[1].Generation && !bytes.Equal(out[0], out[1]) {
		return [2][]byte{}, storagebudget.ErrBounds
	}
	return out, nil
}

// Framing is checked before any selected or peer interpretation.
func storageEnrollmentPeakCheckpointParts(raw []byte, s storageEnrollmentPeakScope) ([]byte, []byte, error) {
	if len(raw) < 8 || uint64(len(raw)) > storageEnrollmentPeakSectionMaximum(s, 6) {
		return nil, nil, storagebudget.ErrBounds
	}
	n, m := uint64(storageEnrollmentPeakU32(raw[:4])), uint64(storageEnrollmentPeakU32(raw[4:8]))
	if n > storageEnrollmentPeakSelectedCheckpointMax || m > storageEnrollmentPeakPeerCheckpointMax || 8+n+m != uint64(len(raw)) {
		return nil, nil, storagebudget.ErrBounds
	}
	return raw[8 : 8+n], raw[8+n:], nil
}

func storageEnrollmentPeakReadCheckpoint(raw []byte, c storageOwnerCatalogV2, views []storageEnrollmentPeakView, s storageEnrollmentPeakScope) (storageEnrollmentPeakCheckpointProjection, error) {
	return storageEnrollmentPeakReadCheckpointOperands(raw, c, views, s, nil)
}
func storageEnrollmentPeakReadCheckpointOperands(raw []byte, c storageOwnerCatalogV2, views []storageEnrollmentPeakView, s storageEnrollmentPeakScope, operands *storageEnrollmentOperands) (storageEnrollmentPeakCheckpointProjection, error) {
	if !s.hasOriginalPeer() {
		return storageEnrollmentPeakReadCheckpointV1(raw, c, views, s)
	}
	var zero storageEnrollmentPeakCheckpointProjection
	selected, peer, err := storageEnrollmentPeakCheckpointParts(raw, s)
	if err != nil {
		return zero, err
	}
	p, err := storageEnrollmentPeakReadCheckpointV1(selected, c, views, s)
	if err != nil {
		return zero, err
	}
	return storageEnrollmentPeakFinishCheckpointPeer(raw, peer, p, c, operands)
}

// Both callers have just strictly decoded the selected bytes in this same
// synchronous call. This shares no decoded operand with an external reader;
// nonnil epoch operands supply only pointer-free facts of the exact pages.
func storageEnrollmentPeakFinishCheckpointPeer(raw, peerRaw []byte, p storageEnrollmentPeakCheckpointProjection, c storageOwnerCatalogV2, operands *storageEnrollmentOperands) (storageEnrollmentPeakCheckpointProjection, error) {
	var zero storageEnrollmentPeakCheckpointProjection
	peer, err := storageEnrollmentPeakReadPeerCheckpoint(peerRaw)
	if err != nil {
		return zero, err
	}
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(c.Installation), Binding: storageEnrollmentPeakQDigest(c.QBinding)}
	p.PeerCheckpoint, err = storageEnrollmentPeakQEncodeCheckpoint(peer, anchor)
	if err != nil {
		return zero, err
	}
	if _, err = storageEnrollmentPeakRestoreOriginalCheckpointsOperands(p, c, operands); err != nil {
		return zero, err
	}
	p.Raw = append([]byte{}, raw...)
	return p, nil
}

func storageEnrollmentPeakWriteCheckpoint(p storageEnrollmentPeakCheckpointProjection, c storageOwnerCatalogV2, views []storageEnrollmentPeakView, s storageEnrollmentPeakScope) ([]byte, error) {
	if !s.hasOriginalPeer() {
		return storageEnrollmentPeakWriteCheckpointV1(p, c, views, s)
	}
	selected, validated, err := storageEnrollmentPeakWriteSelectedCheckpoint(p, c, views, s)
	if err != nil {
		return nil, err
	}
	if len(selected) > storageEnrollmentPeakSelectedCheckpointMax {
		return nil, storagebudget.ErrBounds
	}
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(c.Installation), Binding: storageEnrollmentPeakQDigest(c.QBinding)}
	peer, err := storageEnrollmentPeakQDecodeCheckpoint(append([]byte{}, p.PeerCheckpoint...), anchor)
	if err != nil {
		return nil, err
	}
	compact, err := storageEnrollmentPeakWritePeerCheckpoint(peer)
	if err != nil {
		return nil, err
	}
	w := storageEnrollmentPeakWriter{max: 4718592}
	w.u32(uint32(len(selected)))
	w.u32(uint32(len(compact)))
	w.put(selected)
	w.put(compact)
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	_, peerRaw, err := storageEnrollmentPeakCheckpointParts(w.raw, s)
	var decoded storageEnrollmentPeakCheckpointProjection
	if err == nil {
		decoded, err = storageEnrollmentPeakFinishCheckpointPeer(w.raw, peerRaw, validated, c, nil)
	}
	if err != nil || !reflect.DeepEqual(decoded.Q, p.Q) {
		return nil, storagebudget.ErrBounds
	}
	return w.raw, nil
}

// Version3 binds the selected projection to the same independently expanded
// set as its View. The arbitrary peer keeps its existing full independent
// codec; the additional frame never supplies or rewrites peer policy rows.
func storagePolicyReadCheckpoint(raw []byte, c storageOwnerCatalogV2, views []storageEnrollmentPeakView, s storageEnrollmentPeakScope, sets []storagePolicyEffectiveSet) (storageEnrollmentPeakCheckpointProjection, error) {
	return storagePolicyReadCheckpointOperands(raw, c, views, s, sets, nil)
}
func storagePolicyReadCheckpointOperands(raw []byte, c storageOwnerCatalogV2, views []storageEnrollmentPeakView, s storageEnrollmentPeakScope, sets []storagePolicyEffectiveSet, operands *storageEnrollmentOperands) (storageEnrollmentPeakCheckpointProjection, error) {
	var zero storageEnrollmentPeakCheckpointProjection
	if len(raw) < 8 || len(raw) > 4718592 || storageEnrollmentPeakU16(raw[:2]) != 1 || uint64(storageEnrollmentPeakU32(raw[4:8])) != uint64(len(raw)-8) {
		return zero, storagebudget.ErrBounds
	}
	index := int(storageEnrollmentPeakU16(raw[2:4]))
	if index >= len(sets) {
		return zero, storagebudget.ErrBounds
	}
	p, err := storageEnrollmentPeakReadCheckpointOperands(raw[8:], c, views, s, operands)
	if err != nil {
		return zero, err
	}
	if int(p.View) >= len(views) || views[p.View].Effective == nil {
		return zero, storagebudget.ErrBounds
	}
	expected, err := storagePolicySetIndex(*views[p.View].Effective, sets, c)
	if err != nil || int(expected) != index {
		return zero, storagebudget.ErrBounds
	}
	p.Raw = append([]byte{}, raw...)
	return p, nil
}
func storagePolicyWriteCheckpoint(p storageEnrollmentPeakCheckpointProjection, c storageOwnerCatalogV2, views []storageEnrollmentPeakView, s storageEnrollmentPeakScope, sets []storagePolicyEffectiveSet) ([]byte, error) {
	if int(p.View) >= len(views) || views[p.View].Effective == nil {
		return nil, storagebudget.ErrBounds
	}
	index, err := storagePolicySetIndex(*views[p.View].Effective, sets, c)
	if err != nil {
		return nil, err
	}
	// The original writer strictly validates selected bytes before decoding the
	// peer and retains its final full-Q equality. No operand survives this call.
	inner, err := storageEnrollmentPeakWriteCheckpoint(p, c, views, s)
	if err != nil || len(inner) > 4718592-8 {
		return nil, storagebudget.ErrBounds
	}
	w := storageEnrollmentPeakWriter{max: 4718592}
	w.u16(1)
	w.u16(index)
	w.u32(uint32(len(inner)))
	w.put(inner)
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	return w.raw, nil
}

// Construct pure phase bytes from a complete Staged packet and independently
// supplied F. This helper issues no measurement, publication or Q permission.
// Diagnostic group state/generation are the only optional completion annotations.
func storageEnrollmentPeakArmStaged(staged storageEnrollmentPeakEvidence, factual storagebudget.Census, diagnostics []storageEnrollmentPeakGroup, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	return storageEnrollmentPeakArmStagedOperands(staged, factual, diagnostics, record, claim, nil)
}
func storageEnrollmentPeakArmStagedOperands(staged storageEnrollmentPeakEvidence, factual storagebudget.Census, diagnostics []storageEnrollmentPeakGroup, record storageOwnerRecordV2, claim storageOwnerClaimV2, operands *storageEnrollmentOperands) ([]byte, error) {
	if staged.Phase != 0 || staged.Generation == ^uint64(0) || staged.Predictions[1] == nil {
		return nil, storagebudget.ErrBounds
	}
	return storageEnrollmentPeakArmPlannedOperands(staged, factual, staged.Generation+1, sha256.Sum256(staged.Raw), diagnostics, record, claim, operands)
}

func storageEnrollmentPeakArmPlanned(staged storageEnrollmentPeakEvidence, factual storagebudget.Census, generation uint64, previous storagebudget.Digest, diagnostics []storageEnrollmentPeakGroup, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	return storageEnrollmentPeakArmPlannedOperands(staged, factual, generation, previous, diagnostics, record, claim, nil)
}
func storageEnrollmentPeakArmPlannedOperands(staged storageEnrollmentPeakEvidence, factual storagebudget.Census, generation uint64, previous storagebudget.Digest, diagnostics []storageEnrollmentPeakGroup, record storageOwnerRecordV2, claim storageOwnerClaimV2, operands *storageEnrollmentOperands) ([]byte, error) {
	if staged.Phase != 0 || generation == 0 || previous == (storagebudget.Digest{}) || staged.Predictions[1] == nil {
		return nil, storagebudget.ErrBounds
	}
	q2 := staged.Predictions[1]
	f, err := storageEnrollmentCensusInQOrder(factual, q2)
	if err != nil {
		return nil, err
	}
	section, err := storageEnrollmentPeakWriteCensus(f, q2, 1)
	if err != nil || len(section) != len(staged.Sections[7].Raw) {
		return nil, storagebudget.ErrBounds
	}
	q3 := *q2
	q3.Generation++
	q3.Maintenance = false
	if err = storageEnrollmentPeakQPublishCensus(&q3, f); err != nil {
		return nil, err
	}
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(staged.Installation), Binding: storageEnrollmentPeakQDigest(staged.QBinding)}
	checkpoint, err := storageEnrollmentPeakQEncodeCheckpoint(&q3, anchor)
	if err != nil {
		return nil, err
	}
	digests := staged.Digests
	digests.Q[2] = sha256.Sum256(checkpoint)
	board := staged.Board
	board.Groups = append([]storageEnrollmentPeakGroup{}, board.Groups...)
	if diagnostics != nil && len(diagnostics) != len(board.Groups) {
		return nil, storagebudget.ErrBounds
	}
	for i := range board.Groups {
		g := &board.Groups[i]
		if g.ID == 23 || g.ID == 58 {
			g.Expected = digests.Q[2]
		}
		if diagnostics != nil {
			if diagnostics[i].ID != g.ID || diagnostics[i].State > 3 || diagnostics[i].Generation == 0 {
				return nil, storagebudget.ErrBounds
			}
			g.State = diagnostics[i].State
			g.Generation = diagnostics[i].Generation
		}
	}
	boardRaw, err := storageEnrollmentPeakWriteBoard(board, staged.Scope, 1, digests, staged.Inner.Catalog, len(staged.Views), len(staged.Projections), len(staged.Witnesses))
	if err != nil {
		return nil, err
	}
	armed := staged
	armed.Phase = 1
	armed.Bank = storageEnrollmentPeakEvidenceBank(staged.Scope, 1)
	armed.Generation = generation
	armed.Previous = previous
	armed.Extent = armed.Scope.final(armed.Bank)
	armed.Sections[7].Raw = section
	armed.CensusDigest = sha256.Sum256(section)
	armed.Sections[8] = storageEnrollmentPeakSection{Count: 1, Raw: boardRaw}
	return storageEnrollmentPeakEncodeEvidenceOperands(armed, record, claim, operands)
}

// These operands are complete actual fixed-file observations, not a Boolean
// approval or a retained allowance. Only the original recovery phase supplies
// them to the shared comparison engine after native/exclusion/reservation joins.
type storageEnrollmentPeakCurrentBoundary struct {
	Catalogs         [2][]byte
	ReferencedStaged []byte
}

func storageEnrollmentPeakImmediateBefore(e storageEnrollmentPeakEvidence, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakEnvelope, []byte, error) {
	if !e.Scope.hasOriginalPeer() || e.Scope.path() == 1 || !storageEnrollmentPeakMatchCompleteOriginRows(e) || !storageEnrollmentPeakProofs(&e) {
		return storageEnrollmentPeakEnvelope{}, nil, storagebudget.ErrBounds
	}
	for _, projection := range e.Projections {
		if projection.Kind != 1 || storageEnrollmentPeakProjectionFullDigest(projection) != e.Before {
			continue
		}
		raw := make([]byte, 8388608)
		copy(raw, projection.SavedOuter)
		before, err := storageEnrollmentPeakDecodeEnvelopeVariant(raw, record, claim, true, e.Version >= 2)
		if err != nil || before.Kind != 2 || before.Marker == nil || before.Marker.Phase != 3 || !storageEnrollmentPeakOriginalPeerRetained(e.Scope, before.Scope) || !storageEnrollmentRetainsOriginalWithDefault(projection.Catalog, e.Inner.Catalog, &e.Board.Source) {
			return storageEnrollmentPeakEnvelope{}, nil, storagebudget.ErrBounds
		}
		m := before.Marker
		prior := e.Board.Prior
		if e.Version != 3 && sha256.Sum256(raw) != e.Before || prior.Catalog != e.Before || m.Bridge != prior.Bridge || m.Q3Epoch != prior.Epoch || m.Q3Revision != prior.Q3Revision || m.Evidence != prior.Evidence || e.Version != 3 && m.Q3Epoch != e.BeforeEpoch || m.Q3Revision > e.BeforeRevision || m.Record != e.Record || m.Claim != e.Claim || m.Ready != e.Ready || m.Controls != e.Controls {
			return storageEnrollmentPeakEnvelope{}, nil, storagebudget.ErrBounds
		}
		if e.Version == 3 {
			actual, selected, err := storagePolicyEvidenceNativeBefore(e, record, claim)
			if err != nil || sha256.Sum256(actual[selected]) != e.Before || !storagePolicyProjectionQBoundary(projection, e.BeforeEpoch, e.BeforeRevision) {
				return storageEnrollmentPeakEnvelope{}, nil, storagebudget.ErrBounds
			}
		}
		if _, _, err = storageEnrollmentPeakRestoreOriginalPeerVariant(e.Scope, e.OriginalV3, record, claim, e.Version >= 2); err != nil {
			return storageEnrollmentPeakEnvelope{}, nil, err
		}
		return before, raw, nil
	}
	return storageEnrollmentPeakEnvelope{}, nil, storagebudget.ErrBounds
}

// At the growth R->Armed cut the R-referenced Stage-A is still physically
// intact. Armed overwrites Stage-B, never Stage-A. Reconstruct only Stage-B's
// deterministic annotations for its predecessor-digest comparison; missing or
// torn Stage-A refuses. No unreferenced packet replaces the R selection.
func storageEnrollmentPeakArmedJoinsReferencedStage(armed, stageA storageEnrollmentPeakEvidence, reference storageEnrollmentPeakEnvelope, record storageOwnerRecordV2, claim storageOwnerClaimV2) bool {
	if armed.Scope.path() != 3 || armed.Phase != 1 || stageA.Phase != 0 || stageA.Scope != armed.Scope || stageA.Bank != 1-armed.Scope[323] || armed.Bank != armed.Scope[323] || stageA.Generation > ^uint64(0)-2 || storageEnrollmentPeakJoinMarker(reference, stageA, armed.Ready) != nil {
		return false
	}
	stageB := stageA
	stageB.Bank = armed.Scope[323]
	stageB.Extent = stageB.Scope.final(stageB.Bank)
	stageB.Generation++
	stageB.Previous = sha256.Sum256(stageA.Raw)
	raw, err := storageEnrollmentPeakEncodeEvidence(stageB, record, claim)
	if err != nil || sha256.Sum256(raw) != armed.Previous {
		return false
	}
	stageB, err = storageEnrollmentPeakDecodeEvidenceWithLineage(raw, armed.CompositionLineage, record, claim)
	if err != nil {
		return false
	}
	expected, err := storageEnrollmentPeakArmStaged(stageB, storageEnrollmentFactualCensus(armed.Census), armed.Board.Groups, record, claim)
	return err == nil && bytes.Equal(expected, armed.Raw)
}

func storageEnrollmentPeakValidateCurrentBoundary(boundary storageEnrollmentPeakCurrentBoundary, input storageEnrollmentPeakComparisonInput, e storageEnrollmentPeakEvidence, record storageOwnerRecordV2, claim storageOwnerClaimV2) error {
	if !e.Scope.hasOriginalPeer() || e.Scope.path() == 1 || len(input.PreviousEvidence) != 0 {
		return storagebudget.ErrBounds
	}
	for i := range boundary.Catalogs {
		if !bytes.Equal(boundary.Catalogs[i], input.Catalogs[i]) {
			return storagebudget.ErrBounds
		}
	}
	outer, bank, err := storageEnrollmentPeakDecodeCurrentPair(boundary.Catalogs, e, record, claim)
	if err != nil || outer.Kind != 2 || outer.Marker == nil || outer.Marker.Phase < 1 || storageEnrollmentPeakJoinMarker(outer, e, input.Original.Ready) != nil {
		return storagebudget.ErrBounds
	}
	if _, _, err = storageEnrollmentPeakImmediateBefore(e, record, claim); err != nil {
		return err
	}
	if outer.Marker.Phase == 1 && !bytes.Equal(boundary.Catalogs[0], boundary.Catalogs[1]) {
		// A lone no-growth P still has its complete prior evidence available; it
		// uses the strict path. Growth may have already overwritten that old bank,
		// but must retain the actual linked R and its exact referenced Stage-A.
		if e.Scope.path() != 3 {
			return storagebudget.ErrBounds
		}
		peerCore := boundary.Catalogs[1-bank]
		if e.Version == 3 {
			wrapper, e0 := storageOwnerV6NativeDecode(peerCore, record, claim)
			if e0 != nil {
				return storagebudget.ErrBounds
			}
			peerCore = wrapper.Stage.Core
		} else if e.Version == 2 {
			wrapper, e0 := storageCompositionDecode(peerCore, record, claim)
			if e0 != nil {
				return storagebudget.ErrBounds
			}
			peerCore = wrapper.Core
		}
		reference, err := storageEnrollmentPeakDecodeEnvelopeVariant(peerCore, record, claim, true, e.Version >= 2)
		stage, err2 := storageEnrollmentPeakDecodeEvidenceWithLineage(boundary.ReferencedStaged, e.CompositionLineage, record, claim)
		if err != nil || err2 != nil || reference.Marker == nil || reference.Marker.Phase != 0 || !storageEnrollmentPeakArmedJoinsReferencedStage(e, stage, reference, record, claim) {
			return storagebudget.ErrBounds
		}
	}
	return nil
}

func storageEnrollmentPeakDecodeCurrentPair(raw [2][]byte, e storageEnrollmentPeakEvidence, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakEnvelope, int, error) {
	return storageEnrollmentPeakDecodeCurrentPairOperands(raw, e, record, claim, nil)
}
func storageEnrollmentPeakDecodeCurrentPairOperands(raw [2][]byte, e storageEnrollmentPeakEvidence, record storageOwnerRecordV2, claim storageOwnerClaimV2, operands *storageEnrollmentOperands) (storageEnrollmentPeakEnvelope, int, error) {
	if e.Version == 3 {
		for _, projection := range e.Projections {
			if storageEnrollmentPeakProjectionFullDigest(projection) != e.Before {
				continue
			}
			var pair [2][]byte
			var selected byte
			var err error
			if projection.Native != nil {
				pair, err = storagePolicyNativeProjectionPair(projection)
				selected = projection.Native.Before.Selected
			} else if projection.Policy != nil {
				pair, err = storagePolicyProjectionPair(projection)
				selected = projection.Policy.SelectedBank
			} else {
				continue
			}
			if err == nil && bytes.Equal(raw[0], pair[0]) && bytes.Equal(raw[1], pair[1]) {
				core := make([]byte, storageOwnerCatalogBytes)
				copy(core, projection.SavedOuter)
				outer, err := storageEnrollmentPeakDecodeEnvelopeVariant(core, record, claim, true, true)
				return outer, int(selected), err
			}
		}
		pair, err := storageOwnerV6NativeReadPair(raw[0], raw[1], &e.Inner.Catalog, e.Raw, record, claim)
		if err != nil {
			return storageEnrollmentPeakEnvelope{}, 0, err
		}
		outer, err := storageEnrollmentPeakDecodeEnvelopeNative(pair.Banks[pair.Selected].Stage.Core, record, claim, true, true, true)
		return outer, int(pair.Selected), err
	}
	if e.Version != 2 {
		return storageEnrollmentPeakDecodePairOperands(raw[0], raw[1], record, claim, false, operands)
	}
	pair, err := operands.composedPair(raw, record, claim)
	if err != nil || e.CompositionLineage == nil || !reflect.DeepEqual(pair.Banks[pair.Selected].Lineage, *e.CompositionLineage) {
		return storageEnrollmentPeakEnvelope{}, 0, storagebudget.ErrBounds
	}
	outer, err := operands.envelope(pair.Banks[pair.Selected].Core, record, claim, true, true)
	return outer, int(pair.Selected), err
}

// Recover only the cross-bridge Entered/old-Completed dispatch edge. The old
// page remains a checksum-validated raw predecessor, not a newly minted Q3
// receipt: no old operation is called, and the shared complete current-Q
// comparison must still prove the new bridge's exact Q0/mask0. Historical old
// Q2/Q3 digest bytes remain opaque if ordinary writes followed old completion.
func storageEnrollmentPeakDecodeBoundaryDispatch(raws [2][]byte, e storageEnrollmentPeakEvidence, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakDispatch, int, error) {
	fail := func() (storageEnrollmentPeakDispatch, int, error) {
		return storageEnrollmentPeakDispatch{}, 0, storagebudget.ErrBounds
	}
	if e.Phase != 1 {
		return fail()
	}
	bank := 1 - int(e.Scope[324])
	if bank < 0 || bank > 1 {
		return fail()
	}
	current, err := storageEnrollmentPeakDecodeDispatch(raws[bank], e)
	if err != nil || current.Step != 0 || current.Mask != 0 {
		return fail()
	}
	old, err := storageEnrollmentPeakDecodeCompletedPredecessor(raws[1-bank], e, record, claim)
	if err != nil || current.Generation != old.Generation+1 || current.Previous != sha256.Sum256(old.Raw) {
		return fail()
	}
	return current, bank, nil
}

func storageEnrollmentPeakDecodeCompletedPredecessor(oldRaw []byte, e storageEnrollmentPeakEvidence, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakDispatch, error) {
	fail := func() (storageEnrollmentPeakDispatch, error) {
		return storageEnrollmentPeakDispatch{}, storagebudget.ErrBounds
	}
	before, _, err := storageEnrollmentPeakImmediateBefore(e, record, claim)
	if err != nil {
		return fail()
	}
	if len(oldRaw) != 4096 {
		return fail()
	}
	r := storageEnrollmentPeakReader{raw: oldRaw[:192]}
	if string(r.take(8)) != "DSKEPS1\x00" || r.u32() != 1 || r.u32() != 192 {
		return fail()
	}
	old := storageEnrollmentPeakDispatch{Generation: r.u64(), Installation: r.id(), Binding: r.digest(), Bridge: r.id(), Previous: r.digest()}
	if r.u32() != 256 || r.u32() != 0 {
		return fail()
	}
	checksumOffset := r.pos
	checksum := r.digest()
	if !r.zeroTail() || !storageEnrollmentPeakZero(oldRaw[448:]) {
		return fail()
	}
	normalized := append([]byte{}, oldRaw...)
	clear(normalized[checksumOffset : checksumOffset+32])
	if storageEnrollmentPeakHash("desk-enrollment-step-peak-v1", normalized) != checksum {
		return fail()
	}
	r = storageEnrollmentPeakReader{raw: oldRaw[192:448]}
	old.Mask = r.u8()
	old.Step = r.u8()
	reserved := r.u16()
	old.BeforeRevision = r.u64()
	old.BeforeEpoch = r.u64()
	old.Before = r.digest()
	old.After = r.digest()
	bridge := r.id()
	old.Origin = r.digest()
	old.Controls = [2]storageOwnerPersistentObject{r.native(), r.native()}
	m := before.Marker
	if !r.zeroTail() || reserved != 0 || old.Generation == 0 || old.Generation == ^uint64(0) || old.Mask != 7 || old.Step != 3 || old.Installation != e.Installation || old.Binding != e.QBinding || old.Bridge != m.Bridge || bridge != old.Bridge || old.BeforeRevision != m.Q3Revision-1 || old.BeforeEpoch != m.Q3Epoch || old.Before == (storagebudget.Digest{}) || old.After == (storagebudget.Digest{}) || old.Origin == (storagebudget.Digest{}) || old.Previous == (storagebudget.Digest{}) || old.Controls != ([2]storageOwnerPersistentObject{e.Controls[2], e.Controls[3]}) {
		return fail()
	}
	if e.BeforeRevision == m.Q3Revision && old.After != e.Checkpoint.Checkpoints[e.Checkpoint.Bank] {
		return fail()
	}
	// Derive the old origin's registration/source binding from the complete
	// immediate-before catalog projection, independently of current dictionaries.
	var priorCatalog storageOwnerCatalogV2
	for _, p := range e.Projections {
		if storageEnrollmentPeakProjectionFullDigest(p) == e.Before {
			priorCatalog = p.Catalog
		}
	}
	registration := storagebudget.Registration{Roots: []storagebudget.Root{}, Workspaces: []storagebudget.Workspace{}, Filesystems: []storagebudget.Filesystem{}, CapacityConstraints: []storagebudget.CapacityConstraint{}}
	contains := func(ids []storagebudget.ID, id storagebudget.ID) bool {
		for _, v := range ids {
			if v == id {
				return true
			}
		}
		return false
	}
	for _, v := range priorCatalog.Roots {
		if !contains(priorCatalog.BeforeDefinitionIDs.Roots, v.Definition.ID) {
			registration.Roots = append(registration.Roots, storageOwnerRootValue(v.Definition))
		}
	}
	for _, v := range priorCatalog.Workspaces {
		if !contains(priorCatalog.BeforeDefinitionIDs.Workspaces, v.Definition.ID) {
			registration.Workspaces = append(registration.Workspaces, storagebudget.Workspace{ID: v.Definition.ID, Generation: v.Definition.Generation, Limits: v.Definition.Limits})
		}
	}
	for _, v := range priorCatalog.Filesystems {
		if !contains(priorCatalog.BeforeDefinitionIDs.Filesystems, v.Definition.ID) {
			registration.Filesystems = append(registration.Filesystems, v.Definition)
		}
	}
	for _, v := range priorCatalog.Constraints {
		if !contains(priorCatalog.BeforeDefinitionIDs.Constraints, v.Definition.ID) {
			registration.CapacityConstraints = append(registration.CapacityConstraints, v.Definition)
		}
	}
	digest, err := storageEnrollmentPeakRegistrationDigest(registration)
	if err != nil {
		return fail()
	}
	material := append(append(append(append(append([]byte{}, e.Installation[:]...), e.QBinding[:]...), old.Bridge[:]...), before.Scope[286:321]...), digest[:]...)
	if storageEnrollmentPeakHash("desk-enrollment-origin-e1", material) != old.Origin {
		return fail()
	}
	old.Raw = append([]byte{}, oldRaw...)
	return old, nil
}
