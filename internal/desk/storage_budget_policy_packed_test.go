package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// The unchanged handwritten E1 fixture supplies source and all 64 actual slot
// operands. Expectations below are independently assembled scalar/row bytes;
// no native witness or successful application result is synthesized.
func policyEffectiveFixture(t *testing.T) (epComplete, storageEnrollmentPeakEvidence, storagePolicyEffectiveSet) {
	t.Helper()
	f := epEvidence(t, 0)
	e := epDecode(t, f)
	set := storagePolicyProjectionSet(e.Projections[0])
	if len(set.Workspaces) == 0 {
		t.Fatal("fixture has no immutable admission baseline")
	}
	return f, e, set
}

func TestStoragePolicyEffectiveTable(t *testing.T) {
	_, e, baseline := policyEffectiveFixture(t)
	changed := baseline
	changed.Workspaces = append([]storagebudget.RegisteredWorkspace{}, baseline.Workspaces...)
	changed.Workspaces[0].Limits.Total.Bytes--
	for _, tc := range []struct {
		name  string
		sets  []storagePolicyEffectiveSet
		count int
	}{
		{"distinct-historical-values", []storagePolicyEffectiveSet{baseline, changed}, 2},
		{"identical-only-interning", []storagePolicyEffectiveSet{changed, baseline, changed}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, sets, err := storagePolicyBuildEffectiveTable(e.Tickets, tc.sets, e.Inner.Catalog)
			storageTestMust(t, err)
			if len(sets) != tc.count {
				t.Fatal("different historical assignments were merged")
			}
			ticketRaw, err := storageEnrollmentPeakWriteTickets(e.Tickets)
			storageTestMust(t, err)
			unique := map[storagebudget.Limits]bool{}
			expected := 32 + len(ticketRaw)
			for _, s := range sets {
				expected += 490 + 6*len(s.Workspaces)
				for _, w := range s.Workspaces {
					unique[w.Limits] = true
				}
			}
			expected += 240 * len(unique)
			if len(raw) != expected || string(raw[:8]) != "DSKEFS1\x00" || binary.LittleEndian.Uint32(raw[28:32]) != uint32(expected) {
				t.Fatal("independent exact field budget", len(raw), expected)
			}
			tickets, decoded, err := storagePolicyReadEffectiveTable(raw, uint32(len(e.Tickets)), e.Inner.Catalog)
			storageTestMust(t, err)
			if !reflect.DeepEqual(tickets, e.Tickets) || !reflect.DeepEqual(decoded, sets) {
				t.Fatal("full tickets/effective operands changed")
			}
			// Caller mutations cannot affect the canonical interned result or wire bytes.
			original := sha256.Sum256(raw)
			owned := decoded[0].Workspaces[0].Limits
			decoded[0].Workspaces[0].Limits.Total.Bytes--
			if sha256.Sum256(raw) != original || sets[0].Workspaces[0].Limits != owned {
				t.Fatal("decoded aliases input or another result")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*storagePolicyEffectiveSet)
	}{
		{"stale-generation", func(s *storagePolicyEffectiveSet) { s.Workspaces[0].Generation++ }},
		{"foreign-member", func(s *storagePolicyEffectiveSet) { s.Workspaces[0].ID = storagebudget.ID{255, 255} }},
		{"duplicate-member", func(s *storagePolicyEffectiveSet) { s.Workspaces = append(s.Workspaces, s.Workspaces[0]) }},
		{"invalid-limit", func(s *storagePolicyEffectiveSet) { s.Workspaces[0].Limits = storagebudget.Limits{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := baseline
			s.Workspaces = append([]storagebudget.RegisteredWorkspace{}, baseline.Workspaces...)
			tc.change(&s)
			if _, _, err := storagePolicyBuildEffectiveTable(e.Tickets, []storagePolicyEffectiveSet{s}, e.Inner.Catalog); err == nil {
				t.Fatal("malformed effective operand accepted")
			}
		})
	}
	raw, _, err := storagePolicyBuildEffectiveTable(e.Tickets, []storagePolicyEffectiveSet{baseline, changed}, e.Inner.Catalog)
	storageTestMust(t, err)
	for _, tc := range []struct {
		name   string
		change func([]byte) []byte
	}{
		{"wrong-magic", func(b []byte) []byte { b[0] ^= 1; return b }},
		{"unknown-version", func(b []byte) []byte { b[8] = 2; return b }},
		{"count-before-allocation", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[16:20], 67585); return b }},
		{"trailing-byte", func(b []byte) []byte { return append(b, 0) }},
		{"missing-byte", func(b []byte) []byte { return b[:len(b)-1] }},
		{"invalid-pool-reference", func(b []byte) []byte {
			ticketLen := int(binary.LittleEndian.Uint32(b[20:24]))
			pool := int(binary.LittleEndian.Uint32(b[16:20]))
			at := 32 + ticketLen + 240*pool + 490 + 2
			binary.LittleEndian.PutUint32(b[at:at+4], uint32(pool))
			return b
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := tc.change(append([]byte{}, raw...))
			if _, _, err := storagePolicyReadEffectiveTable(bad, uint32(len(e.Tickets)), e.Inner.Catalog); err == nil {
				t.Fatal("noncanonical table accepted")
			}
		})
	}
}

func TestStoragePolicyEffectiveViews(t *testing.T) {
	_, e, baseline := policyEffectiveFixture(t)
	changed := baseline
	changed.Workspaces = append([]storagebudget.RegisteredWorkspace{}, baseline.Workspaces...)
	changed.Workspaces[0].Limits.Total.Bytes--
	_, sets, err := storagePolicyBuildEffectiveTable(e.Tickets, []storagePolicyEffectiveSet{baseline, changed}, e.Inner.Catalog)
	storageTestMust(t, err)
	views := []storageEnrollmentPeakView{e.Views[0], e.Views[0]}
	views[0].Effective = &baseline
	views[1].Effective = &changed
	t.Run("full-64-slots-and-independent-sets", func(t *testing.T) {
		raw, err := storagePolicyWriteViews(views, e.Inner.Catalog, e.Tickets, sets)
		storageTestMust(t, err)
		decoded, err := storagePolicyReadViews(raw, 2, e.Inner.Catalog, e.Tickets, sets)
		storageTestMust(t, err)
		for i, v := range decoded {
			if !reflect.DeepEqual(v.Slots, e.Views[0].Slots) || !reflect.DeepEqual(v.Effective, views[i].Effective) {
				t.Fatal("full slot or effective content changed", i)
			}
		}
		if storagePolicyViewProjectionPair(decoded[1], e.Projections[0], e.Inner.Catalog) {
			t.Fatal("new value substituted for historical origin")
		}
		if !storagePolicyViewProjectionPair(decoded[0], e.Projections[0], e.Inner.Catalog) {
			t.Fatal("exact original pair refused")
		}
		if _, err := storageEnrollmentPeakReadViews(raw, 2, e.Inner.Catalog, e.Tickets); err == nil {
			t.Fatal("legacy reader accepted tagged rows")
		}
		if _, err := storageEnrollmentPeakWriteViews(views, e.Inner.Catalog, e.Tickets); err == nil {
			t.Fatal("legacy writer silently erased effective metadata")
		}
		last := append([]byte{}, raw...)
		binary.LittleEndian.PutUint16(last[len(last)-2:], uint16(len(sets)))
		if _, err := storagePolicyReadViews(last, 2, e.Inner.Catalog, e.Tickets, sets); err == nil {
			t.Fatal("out-of-table set reference accepted")
		}
	})
	t.Run("selected-q-set-and-full-equality", func(t *testing.T) {
		cp := e.Checkpoint
		q := *cp.Q
		cp.Q = &q
		for i := range q.Workspaces {
			if q.Workspaces[i].ID == storageEnrollmentPeakQID(changed.Workspaces[0].ID) {
				storageEnrollmentPeakCopy(reflect.ValueOf(&q.Workspaces[i].Limits).Elem(), reflect.ValueOf(changed.Workspaces[0].Limits))
			}
		}
		selected := []storageEnrollmentPeakView{views[1]}
		cp.View = 0
		anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(e.Installation), Binding: storageEnrollmentPeakQDigest(e.QBinding)}
		selectedRaw, err := storageEnrollmentPeakQEncodeCheckpoint(cp.Q, anchor)
		storageTestMust(t, err)
		cp.Checkpoints[cp.Bank] = sha256.Sum256(selectedRaw)
		scope := e.Scope
		copy(scope[120:152], cp.Checkpoints[cp.Bank][:])
		// V1 checkpoint path remains usable here: the extra outer mode/set frame is
		// closed even when an independent peer is absent in this legacy fixture.
		raw, err := storagePolicyWriteCheckpoint(cp, e.Inner.Catalog, selected, scope, sets)
		storageTestMust(t, err)
		got, err := storagePolicyReadCheckpoint(raw, e.Inner.Catalog, selected, scope, sets)
		storageTestMust(t, err)
		if !reflect.DeepEqual(got.Q, cp.Q) {
			t.Fatal("final complete selected Q changed")
		}
		bad := append([]byte{}, raw...)
		wrong, err := storagePolicySetIndex(baseline, sets, e.Inner.Catalog)
		storageTestMust(t, err)
		binary.LittleEndian.PutUint16(bad[2:4], wrong)
		if _, err := storagePolicyReadCheckpoint(bad, e.Inner.Catalog, selected, scope, sets); err == nil {
			t.Fatal("selected set mismatch accepted")
		}
		q.Workspaces[0].Known = !q.Workspaces[0].Known
		if _, err := storagePolicyWriteCheckpoint(cp, e.Inner.Catalog, selected, scope, sets); err == nil {
			t.Fatal("caller mutation escaped final full-Q validation")
		}
	})
}

func TestStoragePolicySparseCodec(t *testing.T) {
	c, _, r, cl := ownerV6Fixture(t, 3, false)
	defs, err := c.sourceDefinitions(r, cl)
	storageTestMust(t, err)
	first := defs.Workspaces[0]
	row := storageOwnerV6Override{ID: first.ID, Generation: first.Generation, Limits: first.Limits}
	row.Limits.Total.Bytes--
	raw, err := storageOwnerV6EncodeSparse([]storageOwnerV6Override{row}, defs)
	storageTestMust(t, err)
	// Exact fixed ID16/gen8/Limits240 layout, not JSON or current default values.
	want := make([]byte, 266)
	binary.LittleEndian.PutUint16(want, 1)
	copy(want[2:18], row.ID[:])
	binary.LittleEndian.PutUint64(want[18:26], row.Generation)
	copy(want[26:], storagePolicyLimitsBytes(row.Limits))
	if !bytes.Equal(raw, want) {
		t.Fatal("fixed sparse wire representation")
	}
	got, err := storageOwnerV6DecodeSparse(raw, defs)
	storageTestMust(t, err)
	if !reflect.DeepEqual(got, []storageOwnerV6Override{row}) {
		t.Fatal("sparse value changed")
	}
	for _, tc := range []struct {
		name   string
		change func([]byte) []byte
	}{
		{"stale-generation", func(b []byte) []byte { b[18]++; return b }},
		{"foreign-id", func(b []byte) []byte { b[2] ^= 255; return b }},
		{"equal-baseline-is-noncanonical", func(b []byte) []byte { copy(b[26:], storagePolicyLimitsBytes(first.Limits)); return b }},
		{"trailing-byte", func(b []byte) []byte { return append(b, 0) }},
		{"count-over-bound", func(b []byte) []byte { binary.LittleEndian.PutUint16(b, 1025); return b }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := storageOwnerV6DecodeSparse(tc.change(append([]byte{}, raw...)), defs); err == nil {
				t.Fatal("noncanonical sparse rows accepted")
			}
		})
	}
}

func policyPackedFixture(t *testing.T) (storageOwnerV6PackedCatalog, storageOwnerCatalogV2, []byte, epComplete, storageCompositionLineage) {
	t.Helper()
	_, prior, old, _ := storageSuccessorScopeVector(t, 2)
	return policyPackedFromEvidence(t, prior, old)
}

func policyPackedFromEvidence(t *testing.T, prior epComplete, old []byte) (storageOwnerV6PackedCatalog, storageOwnerCatalogV2, []byte, epComplete, storageCompositionLineage) {
	t.Helper()
	sourceRaw, err := storageOwnerPackedSourceOriginal([2][]byte{old, old}, prior.raw, prior.record, prior.claim)
	storageTestMust(t, err)
	source, _, lineage, _, err := storageOwnerPackedSourceImage(sourceRaw, prior.raw, prior.record, prior.claim)
	storageTestMust(t, err)
	_, defs, err := source.definitions()
	storageTestMust(t, err)
	after := defs.Policy
	after.Workspace.Total.Bytes--
	transition := storagebudget.PolicyTransition{Original: storagebudget.PolicyCommitment{Installation: prior.record.Installation, Binding: prior.record.QBinding, Revision: 13, Epoch: 2, Checkpoint: sha256.Sum256(prior.cp[3]), Tickets: [2]storagebudget.Digest{sha256.Sum256(prior.tickets[0]), sha256.Sum256(prior.tickets[1])}, Manifests: storagebudget.Digest{71}}, Before: storageOwnerV6Policy(defs.Policy), After: storageOwnerV6Policy(after)}
	inverse, err := storagebudget.EncodePolicyTransition(transition)
	storageTestMust(t, err)
	c := storageOwnerV6PackedCatalog{Version: 6, Encoding: "packed-binary-1", Sequence: binary.LittleEndian.Uint64(old[16:24]) + 1, TransitionKind: storageOwnerV6PolicyTransition, Phase: "prepared", Transition: storagebudget.ID{92}, Source: sourceRaw, EffectivePolicy: after, Overrides: []byte{0, 0}, PolicyTransition: inverse}
	copy(c.PreviousCatalogDigest[:], old[32:64])
	c.BeforeEffectiveDigest, err = storageOwnerV6EffectiveDigest(defs)
	storageTestMust(t, err)
	effective, err := storageOwnerV6Expand(defs, after, []storageOwnerV6Override{})
	storageTestMust(t, err)
	c.AfterEffectiveDigest, err = storageOwnerV6EffectiveDigest(effective)
	storageTestMust(t, err)
	raw, err := storageOwnerV6PackedEncode(c, prior.raw, prior.record, prior.claim)
	storageTestMust(t, err)
	return c, source, raw, prior, lineage
}

func TestStoragePolicyPackedSourceAndProjection(t *testing.T) {
	c, source, raw, f, lineage := policyPackedFixture(t)
	t.Run("full-source-and-outer-retained", func(t *testing.T) {
		got, err := storageOwnerV6PackedDecode(raw, f.raw, f.record, f.claim)
		storageTestMust(t, err)
		if !reflect.DeepEqual(got, c) {
			t.Fatal("packed DTO changed")
		}
		if _, err := storageOwnerV6Decode(raw, f.record, f.claim); err == nil {
			t.Fatal("legacy V6 accepted different closed source/override schema")
		}
		if _, err := storageOwnerV5Decode(raw, f.record, f.claim); err == nil {
			t.Fatal("V5 accepted V6")
		}
		final := c
		final.Sequence++
		final.Phase = "active"
		copy(final.PreviousCatalogDigest[:], raw[32:64])
		finalRaw, err := storageOwnerV6PackedEncode(final, f.raw, f.record, f.claim)
		storageTestMust(t, err)
		p, err := storagePolicyBeforeProjection(source, source, finalRaw)
		storageTestMust(t, err)
		storageTestMust(t, storagePolicyRetainProjectionPeer(&p, [2][]byte{raw, finalRaw}, 1, f.raw, f.record, f.claim))
		set := storagePolicyProjectionSet(p)
		table, sets, err := storagePolicyBuildEffectiveTable(nil, []storagePolicyEffectiveSet{set}, source)
		storageTestMust(t, err)
		if len(table) == 0 {
			t.Fatal("set table absent")
		}
		e := epDecode(t, f)
		encoded, err := storagePolicyWriteProjection(p, source, e.OriginalV3, &lineage, sets, f.record, f.claim)
		storageTestMust(t, err)
		if len(encoded) > 135168 || string(encoded[:8]) != "DSKV6P1\x00" || binary.LittleEndian.Uint32(encoded[28:32]) != uint32(len(encoded)) {
			t.Fatal("tagged projection exact framing")
		}
		restored, err := storagePolicyReadProjection(encoded, source, e.OriginalV3, &lineage, sets, f.record, f.claim)
		storageTestMust(t, err)
		outer, err := storagePolicyProjectionOuter(restored)
		storageTestMust(t, err)
		if !bytes.Equal(outer, finalRaw) || storageEnrollmentPeakProjectionFullDigest(restored) != sha256.Sum256(finalRaw) || !bytes.Equal(restored.SavedOuter, p.SavedOuter) {
			t.Fatal("complete original native outer/source lost")
		}
		pair, err := storagePolicyProjectionPair(restored)
		storageTestMust(t, err)
		if !bytes.Equal(pair[0], raw) || !bytes.Equal(pair[1], finalRaw) {
			t.Fatal("native original peer was lost")
		}
		if _, err := storageEnrollmentPeakReadProjectionV2(encoded, source, e.OriginalV3, &lineage, f.record, f.claim); err == nil {
			t.Fatal("old projection reader accepted tagged branch")
		}
		if _, err := storageEnrollmentPeakWriteProjectionV2(p, source, e.OriginalV3, &lineage, f.record, f.claim); err == nil {
			t.Fatal("old projection writer erased retained policy")
		}
		changed := append([]storagePolicyEffectiveSet{}, sets...)
		changed[0].Workspaces = append([]storagebudget.RegisteredWorkspace{}, sets[0].Workspaces...)
		changed[0].Workspaces[0].Limits.Total.Bytes--
		if _, err := storagePolicyReadProjection(encoded, source, e.OriginalV3, &lineage, changed, f.record, f.claim); err == nil {
			t.Fatal("wrong historical values accepted against exact outer header")
		}
		for _, at := range []int{8, 16, 17, 18, 19, 20, 22, 23, 24, 26, 32, 96, 112, 2255, 2256, 2287} {
			bad := append([]byte{}, encoded...)
			bad[at] ^= 1
			if _, err := storagePolicyReadProjection(bad, source, e.OriginalV3, &lineage, sets, f.record, f.claim); err == nil {
				t.Fatal("tagged projection mutation accepted", at)
			}
		}
	})
	t.Run("raw-source-lineage-and-evidence-refusals", func(t *testing.T) {
		bad := append([]byte{}, f.raw...)
		bad[len(bad)-1] ^= 1
		if _, err := storageOwnerV6PackedDecode(raw, bad, f.record, f.claim); err == nil {
			t.Fatal("different source evidence accepted")
		}
		sourceDTO, l, err := storageOwnerDecodePackedSource(c.Source)
		storageTestMust(t, err)
		lr, err := l.encode()
		storageTestMust(t, err)
		expected, err := lineage.encode()
		storageTestMust(t, err)
		if !bytes.Equal(lr, expected) || sourceDTO.Encoding != 1 {
			t.Fatal("source lineage/source kind changed")
		}
		badSource := append([]byte{}, c.Source...)
		badSource[len(badSource)-1] = ' '
		if _, _, err := storageOwnerDecodePackedSource(badSource); err == nil {
			t.Fatal("noncanonical source accepted")
		}
	})
}

// This valid second-edit witness fixes the native-peer reconstruction operand
// that a current/retained packed pair must keep. Source and the new Q inverse
// do not contain the previous completed V6 catalog checksum.
func TestStoragePolicyPackedRetainedPeerOperand(t *testing.T) {
	first, source, firstRaw, f, _ := policyPackedFixture(t)
	firstFinal := first
	firstFinal.Sequence++
	firstFinal.Phase = "active"
	copy(firstFinal.PreviousCatalogDigest[:], firstRaw[32:64])
	previousRaw, err := storageOwnerV6PackedEncode(firstFinal, f.raw, f.record, f.claim)
	storageTestMust(t, err)
	_, defs, err := source.definitions()
	storageTestMust(t, err)
	old, err := storageOwnerV6Expand(defs, firstFinal.EffectivePolicy, []storageOwnerV6Override{})
	storageTestMust(t, err)
	second := firstFinal
	second.Sequence++
	second.Phase = "prepared"
	second.Transition = storagebudget.ID{94}
	second.EffectivePolicy.Workspace.Total.Bytes--
	copy(second.PreviousCatalogDigest[:], previousRaw[32:64])
	second.BeforeEffectiveDigest, err = storageOwnerV6EffectiveDigest(old)
	storageTestMust(t, err)
	after, err := storageOwnerV6Expand(defs, second.EffectivePolicy, []storageOwnerV6Override{})
	storageTestMust(t, err)
	second.AfterEffectiveDigest, err = storageOwnerV6EffectiveDigest(after)
	storageTestMust(t, err)
	original, err := storagebudget.DecodePolicyTransition(first.PolicyTransition)
	storageTestMust(t, err)
	original.Original.Revision += 3
	original.Original.Epoch++
	original.Original.Checkpoint[0]++
	original.Before = storageOwnerV6Policy(firstFinal.EffectivePolicy)
	original.After = storageOwnerV6Policy(second.EffectivePolicy)
	second.PolicyTransition, err = storagebudget.EncodePolicyTransition(original)
	storageTestMust(t, err)
	preparedRaw, err := storageOwnerV6PackedEncode(second, f.raw, f.record, f.claim)
	storageTestMust(t, err)
	final := second
	final.Sequence++
	final.Phase = "active"
	copy(final.PreviousCatalogDigest[:], preparedRaw[32:64])
	finalRaw, err := storageOwnerV6PackedEncode(final, f.raw, f.record, f.claim)
	storageTestMust(t, err)
	if !bytes.Equal(final.Source, first.Source) {
		t.Fatal("policy edit changed immutable Source")
	}
	sourceDTO, _, err := storageOwnerDecodePackedSource(final.Source)
	storageTestMust(t, err)
	var sourceChecksum storagebudget.Digest
	copy(sourceChecksum[:], sourceDTO.Core[32:64])
	if second.PreviousCatalogDigest == sourceChecksum {
		t.Fatal("witness did not distinguish previous V6 from source")
	}
	guessed := final
	guessed.Sequence--
	guessed.Phase = "prepared"
	guessed.PreviousCatalogDigest = sourceChecksum
	guessedRaw, err := storageOwnerV6PackedEncode(guessed, f.raw, f.record, f.claim)
	storageTestMust(t, err)
	if bytes.Equal(guessedRaw, preparedRaw) || bytes.Equal(guessedRaw[32:64], final.PreviousCatalogDigest[:]) {
		t.Fatal("source checksum reconstructed a different native V6 peer")
	}
	// Retaining this one exact scalar restores the complete peer; a commitment
	// to the peer checksum itself cannot supply the absent scalar's bytes.
	guessed.PreviousCatalogDigest = second.PreviousCatalogDigest
	restored, err := storageOwnerV6PackedEncode(guessed, f.raw, f.record, f.claim)
	storageTestMust(t, err)
	if !bytes.Equal(restored, preparedRaw) {
		t.Fatal("exact peer scalar failed full reconstruction")
	}
	decoded, err := storageOwnerV6PackedDecode(finalRaw, f.raw, f.record, f.claim)
	storageTestMust(t, err)
	if decoded.PreviousCatalogDigest != storagebudget.Digest([32]byte(preparedRaw[32:64])) {
		t.Fatal("native peer checksum not bound by the selected final")
	}
	for _, phase := range []string{"active", "aborted"} {
		for _, bank := range []byte{0, 1} {
			name := phase + "-bank" + string(rune('0'+bank))
			t.Run(name, func(t *testing.T) {
				end := final
				if phase == "aborted" {
					end.Phase = "aborted"
					end.EffectivePolicy = firstFinal.EffectivePolicy
				}
				endRaw, err := storageOwnerV6PackedEncode(end, f.raw, f.record, f.claim)
				storageTestMust(t, err)
				actual := [2][]byte{}
				actual[bank] = endRaw
				actual[1-bank] = preparedRaw
				projection, err := storagePolicyBeforeProjection(source, source, endRaw)
				storageTestMust(t, err)
				wrongBank := projection
				if err := storagePolicyRetainProjectionPeer(&wrongBank, actual, 1-bank, f.raw, f.record, f.claim); err == nil {
					t.Fatal("bank contrary to actual originals accepted")
				}
				storageTestMust(t, storagePolicyRetainProjectionPeer(&projection, actual, bank, f.raw, f.record, f.claim))
				pair, err := storagePolicyProjectionPair(projection)
				storageTestMust(t, err)
				if !bytes.Equal(pair[0], actual[0]) || !bytes.Equal(pair[1], actual[1]) {
					t.Fatal("second-edit restart lost native pair")
				}
				meta := *projection.Policy
				meta.PeerPrevious[0] ^= 1
				bad := projection
				bad.Policy = &meta
				if _, err := storagePolicyProjectionPair(bad); err == nil {
					t.Fatal("wrong peer scalar accepted")
				}
				_, sets, err := storagePolicyBuildEffectiveTable(nil, []storagePolicyEffectiveSet{projection.Policy.Effective}, source)
				storageTestMust(t, err)
				evidence := epDecode(t, f)
				lineage := storageCompositionEmptyLineage()
				encoded, err := storagePolicyWriteProjection(projection, source, evidence.OriginalV3, &lineage, sets, f.record, f.claim)
				storageTestMust(t, err)
				back, err := storagePolicyReadProjection(encoded, source, evidence.OriginalV3, &lineage, sets, f.record, f.claim)
				storageTestMust(t, err)
				pair, err = storagePolicyProjectionPair(back)
				storageTestMust(t, err)
				if !bytes.Equal(pair[0], actual[0]) || !bytes.Equal(pair[1], actual[1]) {
					t.Fatal("peer changed after table/projection roundtrip")
				}
				for _, cut := range []int{2256, 2271, 2287} {
					if _, err := storagePolicyReadProjection(encoded[:cut], source, evidence.OriginalV3, &lineage, sets, f.record, f.claim); err == nil {
						t.Fatal("truncated peer scalar accepted", cut)
					}
				}
			})
		}
	}

}
