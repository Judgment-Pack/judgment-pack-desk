package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageEnrollmentOperandsEvidenceStrictEquivalence(t *testing.T) {
	for _, phase := range []byte{0, 1, 2} {
		f := epEvidence(t, phase)
		want, err := storageEnrollmentPeakDecodeEvidence(f.raw, f.record, f.claim)
		storageTestMust(t, err)
		var operands storageEnrollmentOperands
		input := append([]byte(nil), f.raw...)
		got, err := operands.evidence(input, nil, f.record, f.claim)
		storageTestMust(t, err)
		if !reflect.DeepEqual(got, want) {
			t.Fatal("first strict decode differs", phase)
		}
		age := operands.next
		// Neither decoder input nor a deeply nested returned model is retained.
		input[len(input)-1] ^= 1
		got.Raw[0] ^= 1
		got.Sections[0].Raw[0] ^= 1
		got.OriginalV3[0] ^= 1
		got.Checkpoint.Q.Generation++
		got.Predictions[0].Generation++
		got.PredictionBytes[0][0] ^= 1
		again, err := operands.evidence(f.raw, nil, f.record, f.claim)
		storageTestMust(t, err)
		if operands.next != age || !reflect.DeepEqual(again, want) {
			t.Fatal("retained evidence was poisoned or not reused", phase)
		}
		if _, err = operands.evidence(input, nil, f.record, f.claim); !errors.Is(err, storagebudget.ErrBounds) {
			t.Fatal("same-generation changed bytes reused", phase, err)
		}
		for _, change := range []string{"record", "claim"} {
			record, claim := f.record, f.claim
			if change == "record" {
				record.Nonce[0] ^= 1
			} else {
				claim.Nonce[0] ^= 1
			}
			_, strict := storageEnrollmentPeakDecodeEvidenceWithLineage(f.raw, nil, record, claim)
			_, reused := operands.evidence(f.raw, nil, record, claim)
			if strict == nil || !errors.Is(reused, strict) {
				t.Fatal("changed context reused", change, reused, strict)
			}
		}
		// Even context ignored by this legacy variant is an exact key operand.
		lineage := storageCompositionEmptyLineage()
		_, err = operands.evidence(f.raw, &lineage, f.record, f.claim)
		storageTestMust(t, err)
		before := operands.next
		lineage.Initial = append(lineage.Initial, storageCompositionInitial{Mode: 1})
		_, err = operands.evidence(f.raw, &lineage, f.record, f.claim)
		storageTestMust(t, err)
		if operands.next == before {
			t.Fatal("lineage context aliased its caller")
		}
	}
}

func TestStorageEnrollmentOperandsComparisonKeepsActualJoins(t *testing.T) {
	f := epEvidence(t, 1)
	e := epDecode(t, f)
	completed := epDecode(t, epEvidence(t, 2))
	normal := epNormal(t, completed, 3, 6, storagebudget.Digest{1}, f.record, f.claim)
	input := storageEnrollmentPeakComparisonInput{Catalogs: [2][]byte{normal, normal}, Evidence: completed.Raw, ArmedEvidence: e.Raw, Dispatch: epPeerDispatch(e), CurrentCheckpoints: [2][]byte{f.cp[2], f.cp[3]}, CurrentTickets: f.tickets, Original: epOriginal(f, completed)}
	var operands storageEnrollmentOperands
	want, err := storageEnrollmentPeakCompare(input, f.record, f.claim)
	storageTestMust(t, err)
	for i := 0; i < 2; i++ {
		got, err := storageEnrollmentPeakCompareOperands(input, f.record, f.claim, nil, &operands)
		storageTestMust(t, err)
		if !reflect.DeepEqual(got, want) {
			t.Fatal("strict and reused comparison differ")
		}
		got.Evidence.Checkpoint.Q.Generation++
		got.Evidence.Raw[0] ^= 1
		got.Envelope.Marker.Phase = 0
	}
	for _, change := range []string{"evidence", "armed", "catalog", "dispatch", "checkpoint", "ticket", "ready", "target", "extent", "host"} {
		bad := storageEnrollmentOperandClone(input)
		switch change {
		case "evidence":
			bad.Evidence[len(bad.Evidence)-1] ^= 1
		case "armed":
			bad.ArmedEvidence = nil
		case "catalog":
			bad.Catalogs[0][len(bad.Catalogs[0])-1] ^= 1
		case "dispatch":
			bad.Dispatch[0][100] ^= 1
		case "checkpoint":
			bad.CurrentCheckpoints[0][100] ^= 1
		case "ticket":
			bad.CurrentTickets[0][100] ^= 1
		case "ready":
			bad.Original.Ready[0] ^= 1
		case "target":
			bad.Original.Targets[0] = storageOwnerPersistentObject{}
		case "extent":
			bad.Original.Extents[0]--
		case "host":
			bad.Original.Hosts[0][0][0] ^= 1
		}
		_, strict := storageEnrollmentPeakCompare(bad, f.record, f.claim)
		_, reused := storageEnrollmentPeakCompareOperands(bad, f.record, f.claim, nil, &operands)
		if strict == nil || reused == nil || !errors.Is(reused, strict) {
			t.Fatal("actual join changed", change, reused, strict)
		}
	}
	// A consumed dispatch plus actual-before must remain unresolved on warm data.
	for stage := byte(0); stage < 3; stage++ {
		banks := [2][]byte{f.cp[0], f.cp[0]}
		if stage > 0 {
			banks[stage%2] = f.cp[stage]
			banks[1-stage%2] = f.cp[stage-1]
		}
		draw := epDispatch(e, stage+1, uint64(stage)+2, sha256.Sum256(epDispatch(e, stage, uint64(stage)+1, storagebudget.Digest{1})))
		d, err := storageEnrollmentPeakDecodeDispatch(draw, e)
		storageTestMust(t, err)
		strict, err := storageEnrollmentPeakCompareQ(e, banks, f.tickets, d)
		storageTestMust(t, err)
		reused, err := storageEnrollmentPeakCompareQOperands(e, banks, f.tickets, d, &operands)
		storageTestMust(t, err)
		if strict != reused || reused.Stage != int(stage) || !reused.UnresolvedQAttempt {
			t.Fatal("consumed Q attempt became reusable", stage)
		}
	}
	_, err = operands.envelope(normal, f.record, f.claim, true, false)
	storageTestMust(t, err)
	age := operands.next
	_, err = operands.envelope(normal, f.record, f.claim, true, true)
	storageTestMust(t, err)
	if operands.next == age {
		t.Fatal("composition variant reused another interpretation")
	}
	_, strict := storageEnrollmentPeakDecodeEnvelopeVariant(normal, f.record, f.claim, false, false)
	_, reused := operands.envelope(normal, f.record, f.claim, false, false)
	if (strict == nil) != (reused == nil) || strict != nil && !errors.Is(reused, strict) {
		t.Fatal("full/compact variant changed")
	}
}

func TestStorageEnrollmentOperandsPhysicalLineageIsolation(t *testing.T) {
	// Extend the established handwritten Evidence2 fixture construction.
	f := compositionTestActiveEvidence(t)
	e := epDecode(t, f)
	core := ownerAdvisoryWire(f.before)
	pair, _ := compositionTestBasePair(core, 1, f.before.PreviousCatalogDigest)
	pair = [2][]byte{pair[1], pair[0]}
	physical := compositionTestProjection(pair)
	lineage := storageCompositionEmptyLineage()
	s := e.Scope
	binary.LittleEndian.PutUint16(s[:2], 2)
	d := sha256.Sum256(core)
	copy(s[1640:1672], d[:])
	copy(s[1672:1704], f.before.PreviousCatalogDigest[:])
	s[1704] = 1
	normalized := s
	clear(normalized[184:216])
	normalized[321] = 0
	clear(normalized[1080:1220])
	d = epHash("desk-enrollment-control-peak-v2", normalized[:])
	copy(s[184:216], d[:])
	e.Scope, e.Version, e.CompositionLineage = s, 2, &lineage
	e.Sections[8].Raw = append([]byte(nil), f.sections[8]...)
	copy(e.Sections[8].Raw[88:120], s[184:216])
	cp := e.Checkpoint
	cp.PeerCheckpoint = append([]byte(nil), f.cp[0]...)
	var err error
	e.Sections[6].Raw, err = storageEnrollmentPeakWriteCheckpoint(cp, e.Inner.Catalog, e.Views, s)
	storageTestMust(t, err)
	e.Sections[4].Raw = append(append([]byte(nil), f.sections[4]...), physical[:]...)
	raw := compositionTestEvidenceV2Frame(e)
	var operands storageEnrollmentOperands
	got, err := operands.evidence(raw, &lineage, f.record, f.claim)
	storageTestMust(t, err)
	before, err := operands.composition(got, f.record, f.claim)
	storageTestMust(t, err)
	if !bytes.Equal(before.Banks[0].Raw, pair[0]) || !bytes.Equal(before.Banks[1].Raw, pair[1]) {
		t.Fatal("physical original reconstruction changed")
	}
	before.Banks[0].Raw[0] ^= 1
	got.CompositionLineage.Initial = append(got.CompositionLineage.Initial, storageCompositionInitial{Mode: 1})
	again, err := operands.evidence(raw, &lineage, f.record, f.claim)
	storageTestMust(t, err)
	restored, err := operands.composition(again, f.record, f.claim)
	storageTestMust(t, err)
	if !bytes.Equal(restored.Banks[0].Raw, pair[0]) || len(again.CompositionLineage.Initial) != 0 {
		t.Fatal("returned physical model poisoned retained operands")
	}
	lineage.Initial = append(lineage.Initial, storageCompositionInitial{Mode: 1})
	_, strict := storageEnrollmentPeakDecodeEvidenceWithLineage(raw, &lineage, f.record, f.claim)
	_, reused := operands.evidence(raw, &lineage, f.record, f.claim)
	if strict == nil || !errors.Is(reused, strict) {
		t.Fatal("changed physical lineage reused", reused, strict)
	}
}

func TestStorageEnrollmentOperandsBoundUsesStrictFallback(t *testing.T) {
	var operands storageEnrollmentOperands
	for i := byte(0); i < 12; i++ {
		raw := []byte{i}
		got, err := storageEnrollmentOperandGet(&operands, storageEnrollmentOperandEvidence, raw, i, func(b []byte) ([]byte, error) { return append([]byte(nil), b...), nil })
		storageTestMust(t, err)
		if !bytes.Equal(got, raw) {
			t.Fatal("bounded replacement changed value")
		}
	}
	n := 0
	for _, e := range operands.entries {
		if e.kind == storageEnrollmentOperandEvidence {
			n++
		}
	}
	if n != 6 || operands.bytes > storageEnrollmentOperandLimit {
		t.Fatal("packet retention grew past fixed roles")
	}
	// Aliases make the conservative accounting exceed its ceiling with only
	// one MiB allocated. The valid value must still return through strict code.
	large := make([][]byte, 512)
	page := make([]byte, 1<<20)
	for i := range large {
		large[i] = page
	}
	age := operands.next
	got, err := storageEnrollmentOperandGet(&operands, storageEnrollmentOperandEvidence, []byte{99}, 99, func([]byte) ([][]byte, error) { return large, nil })
	storageTestMust(t, err)
	if len(got) != len(large) || operands.next != age || operands.bytes > storageEnrollmentOperandLimit {
		t.Fatal("retention ceiling became a validity limit")
	}
}

// The single-copy detach returns the same complete deep copy: equal values,
// preserved nil/empty distinctions and exact lengths, and no reachable byte,
// pointer target, nested slice, map or interface value shared with the source.
func TestStorageEnrollmentOperandCloneDetachesEveryReference(t *testing.T) {
	type named []byte
	type inner struct {
		Raw   []byte
		Named named
		Big   [4]uint64
		Text  string
	}
	type value struct {
		Bytes   []byte
		Empty   []byte
		Nil     []byte
		Pointer *inner
		Rows    []inner
		Fixed   [2]inner
		Map     map[string][]byte
		Any     any
		Scalars [3]storagebudget.Digest
	}
	text := string([]byte("retained-text"))
	src := value{
		Bytes:   append(make([]byte, 0, 64), 1, 2, 3),
		Empty:   []byte{},
		Pointer: &inner{Raw: []byte{4}, Named: named{5}, Big: [4]uint64{6}, Text: text},
		Rows:    []inner{{Raw: []byte{7}}, {Named: named{8}}},
		Fixed:   [2]inner{{Raw: []byte{9}}, {Text: text}},
		Map:     map[string][]byte{"k": {10}},
		Any:     &inner{Raw: []byte{11}},
		Scalars: [3]storagebudget.Digest{{12}},
	}
	for _, freeze := range []bool{false, true} {
		got := storageEnrollmentOperandCloneValue(reflect.ValueOf(&src).Elem(), freeze).Interface().(value)
		if !reflect.DeepEqual(got, src) {
			t.Fatal("clone changed the value", freeze)
		}
		if got.Nil != nil || got.Empty == nil || len(got.Empty) != 0 || cap(got.Bytes) != len(got.Bytes) || cap(got.Pointer.Raw) != 1 || cap(got.Pointer.Named) != 1 {
			t.Fatal("clone changed nil, empty or exact lengths", freeze)
		}
		got.Bytes[0] ^= 1
		got.Pointer.Raw[0] ^= 1
		got.Pointer.Named[0] ^= 1
		got.Pointer.Big[0]++
		got.Rows[0].Raw[0] ^= 1
		got.Rows[1].Named[0] ^= 1
		got.Fixed[0].Raw[0] ^= 1
		got.Map["k"][0] ^= 1
		got.Any.(*inner).Raw[0] ^= 1
		got.Scalars[0][0] ^= 1
		if src.Bytes[0] != 1 || src.Pointer.Raw[0] != 4 || src.Pointer.Named[0] != 5 || src.Pointer.Big[0] != 6 || src.Rows[0].Raw[0] != 7 || src.Rows[1].Named[0] != 8 || src.Fixed[0].Raw[0] != 9 || src.Map["k"][0] != 10 || src.Any.(*inner).Raw[0] != 11 || src.Scalars[0][0] != 12 {
			t.Fatal("clone shares mutable data with its source", freeze)
		}
		if got.Pointer == src.Pointer || got.Any.(*inner) == src.Any.(*inner) {
			t.Fatal("clone shares a pointer target", freeze)
		}
	}
}

// Pointer-free views read the exact retained strict decode without cloning it,
// keep the strict refusal for other bytes, and cannot return an aliasing type.
func TestStorageEnrollmentOperandViewsReadRetainedFactsOnly(t *testing.T) {
	f := epEvidence(t, 1)
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(f.record.Installation), Binding: storageEnrollmentPeakQDigest(f.record.QBinding)}
	strict, err := storageEnrollmentPeakQDecodeCheckpoint(append([]byte(nil), f.cp[0]...), anchor)
	storageTestMust(t, err)
	var operands storageEnrollmentOperands
	for round := 0; round < 2; round++ {
		got, err := operands.checkpointFacts(f.cp[0], anchor)
		storageTestMust(t, err)
		if got.Generation != strict.Generation || got.Applied != strict.Applied || got.Digest != sha256.Sum256(f.cp[0]) || operands.next != 1 {
			t.Fatal("checkpoint facts differ from the strict decode or were not reused", round)
		}
		got.Applied[0].NextSequence++
	}
	full, err := storageEnrollmentOperandGet(&operands, storageEnrollmentOperandCheckpoint, f.cp[0], anchor, storageEnrollmentCheckpointDecoder(anchor))
	storageTestMust(t, err)
	if !reflect.DeepEqual(full.Value, strict) || operands.next != 1 {
		t.Fatal("a view changed or replaced the retained model")
	}
	bad := append([]byte(nil), f.cp[0]...)
	bad[200] ^= 1
	_, strictErr := storageEnrollmentPeakQDecodeCheckpoint(append([]byte(nil), bad...), anchor)
	_, viewErr := operands.checkpointFacts(bad, anchor)
	if strictErr == nil || !errors.Is(viewErr, strictErr) {
		t.Fatal("changed checkpoint bytes reused facts", viewErr, strictErr)
	}
	e := epDecode(t, f)
	if e.Scope.hasOriginalPeer() {
		_, _, strictPeer := storageEnrollmentPeakRestoreOriginalPeerVariant(e.Scope, e.OriginalV3, f.record, f.claim, false)
		for round := 0; round < 2; round++ {
			before := operands.next
			err := operands.peerValid(e.Scope, e.OriginalV3, f.record, f.claim, false)
			if (err == nil) != (strictPeer == nil) || round == 1 && strictPeer == nil && operands.next != before {
				t.Fatal("peer validation differs from strict restoration or was not reused", round, err, strictPeer)
			}
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("an aliasing view was accepted")
		}
	}()
	_, _ = storageEnrollmentOperandView(&operands, storageEnrollmentOperandCheckpoint, f.cp[0], anchor, storageEnrollmentCheckpointDecoder(anchor), func(v storageEnrollmentCheckpointOperand) *storageEnrollmentPeakQCheckpoint { return v.Value })
}

// Sub-decodes reused within one epoch's operands return the strict evidence.
func TestStorageEnrollmentOperandsSharedSubDecodesStayStrict(t *testing.T) {
	for _, phase := range []byte{0, 1, 2} {
		f := epEvidence(t, phase)
		want, err := storageEnrollmentPeakDecodeEvidence(f.raw, f.record, f.claim)
		storageTestMust(t, err)
		var operands storageEnrollmentOperands
		for round := 0; round < 2; round++ {
			before := operands.next
			got, err := storageEnrollmentPeakDecodeEvidenceOperands(append([]byte(nil), f.raw...), nil, f.record, f.claim, &operands)
			storageTestMust(t, err)
			if !reflect.DeepEqual(got, want) {
				t.Fatal("reused sub-decodes changed the evidence", phase, round)
			}
			if round == 1 && operands.next != before {
				t.Fatal("identical sub-decodes were not reused", phase)
			}
			for _, b := range [][]byte{got.Checkpoint.Checkpoint, got.Checkpoint.PeerCheckpoint, got.OriginalV3} {
				if len(b) > 0 {
					b[0] ^= 1
				}
			}
		}
	}
}
