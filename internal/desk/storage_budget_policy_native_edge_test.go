package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Existing handwritten source graphs and complete E1 sections are composed
// here. They are pure comparison vectors, never captured native witnesses.
func policyNativeFixture(t *testing.T, selected byte) ([2][]byte, storageOwnerCatalogV2, epComplete, storageCompositionLineage) {
	t.Helper()
	return policyNativeFixtureFrom(t, selected, false)
}
func policyNativeFixtureFrom(t *testing.T, selected byte, from bool) ([2][]byte, storageOwnerCatalogV2, epComplete, storageCompositionLineage) {
	t.Helper()
	_, base, r, cl := ownerV4Fixture(t, from)
	old, err := storageOwnerDecodeCatalogV3(base, r, cl)
	storageTestMust(t, err)
	before, after, source, r, cl := epCatalogsFrom(t, storageOwnerCatalogV2(old), r, cl)
	f := epEvidenceFromCatalogs(t, 2, [2]uint64{262144, 262144}, before, after, source, r, cl)
	e := epDecode(t, f)
	core := compositionTestMarkerCore(e, 3, binary.LittleEndian.Uint64(e.Scope[1624:1632])+5, storagebudget.Digest{1})
	c, catalog, prepared, f, lineage := policyPackedFromEvidence(t, f, core)
	final := c
	final.Sequence++
	final.Phase = "active"
	copy(final.PreviousCatalogDigest[:], prepared[32:64])
	active, err := storageOwnerV6PackedEncode(final, f.raw, f.record, f.claim)
	storageTestMust(t, err)
	actual := [2][]byte{prepared, prepared}
	actual[selected] = active
	return actual, catalog, f, lineage
}
func policyNativePlan(t *testing.T) storageCompositionLineage {
	t.Helper()
	rows, _, _ := ownerV5Fixture(t, false)
	l := storageCompositionEmptyLineage()
	l.Rows.LineagePlans = append([]storageOwnerV5LineagePlan{}, rows.LineagePlans...)
	l.Order[0] = []storagebudget.ID{rows.LineagePlans[0].PlanID}
	l.Initial = []storageCompositionInitial{{Mode: 1}}
	return l
}

func TestStoragePolicyNativeSourceEdges(t *testing.T) {
	for _, selected := range []byte{0, 1} {
		t.Run(fmt.Sprintf("bank%d", selected), func(t *testing.T) {
			actual, catalog, f, _ := policyNativeFixture(t, selected)
			if f.before.Phase != "active" || f.before.Sequence != 2 || f.after.Phase != "prepared" || f.after.Sequence != 3 {
				t.Fatal("original Active V3 or its distinct Prepared successor changed")
			}
			armed := epEvidenceFromCatalogs(t, 1, [2]uint64{262144, 262144}, f.before, f.after, f.source, f.record, f.claim)
			if epDecode(t, f).Previous != sha256.Sum256(armed.raw) {
				t.Fatal("Ready evidence does not retain its same-original Armed predecessor")
			}
			capsule, core, lineage, policy, sparse, err := storageOwnerV6CaptureBefore(actual, selected, f.raw, catalog, f.record, f.claim)
			storageTestMust(t, err)
			wire, err := capsule.encode()
			storageTestMust(t, err)
			decoded, err := storageOwnerV6ReadBefore(wire)
			storageTestMust(t, err)
			restored, err := decoded.restore(core, lineage, policy, sparse, catalog, f.record, f.claim)
			storageTestMust(t, err)
			for i := range actual {
				if !bytes.Equal(restored[i], actual[i]) {
					t.Fatal("original pair lost", i)
				}
			}
			next := policyNativePlan(t)
			storageTestMust(t, next.validateGraph(catalog, f.record))
			id := next.Rows.LineagePlans[0].PlanID
			published, err := storageOwnerV6NativeAppend(actual, selected, f.raw, catalog, next, storageCompositionPlan, id, f.record, f.claim)
			storageTestMust(t, err)
			outer, err := storageEnrollmentPeakDecodeEnvelopeVariant(core, f.record, f.claim, true, false)
			storageTestMust(t, err)
			if outer.Marker == nil || outer.Marker.Previous != (storagebudget.Digest{1}) {
				t.Fatal("bare original nonzero previous operand lost")
			}
			var expected [65]byte
			full := sha256.Sum256(core)
			copy(expected[1:33], full[:])
			copy(expected[33:65], outer.Marker.Previous[:])
			for _, raw := range published {
				v, err := storageOwnerV6NativeDecode(raw, f.record, f.claim)
				storageTestMust(t, err)
				if v.Stage.BeforePeer != expected {
					t.Fatal("complete bare original peer operand changed")
				}
			}
			policyNativePeerHalfRefusals(t, actual, selected, published[1-selected], f)
			split := actual
			split[1-selected] = published[1-selected]
			pending, err := storageOwnerV6NativeReadPair(split[0], split[1], nil, f.raw, f.record, f.claim)
			storageTestMust(t, err)
			if !pending.RecoveryOnly || pending.Selected != 1-selected || len(pending.Banks[selected].Stage.Lineage.Order[0]) != 0 || len(pending.Banks[1-selected].Stage.Lineage.Order[0]) != 1 {
				t.Fatal("pending split lineage was adopted or erased")
			}
			final, err := storageOwnerV6NativeReadPair(published[0], published[1], nil, nil, f.record, f.claim)
			storageTestMust(t, err)
			if final.RecoveryOnly || final.Selected != selected || !storageOwnerV6NativeSameEffective(final.Banks[0], final.Banks[1]) || !bytes.Equal(final.Banks[selected].Stage.Core, core) || final.Banks[selected].Stage.Sequence == binary.LittleEndian.Uint64(core[16:24]) {
				t.Fatal("native/core/effective domains collapsed")
			}
			oldBefore, _, _, _, _, err := storageOwnerV6CaptureBefore(published, selected, f.raw, catalog, f.record, f.claim)
			storageTestMust(t, err)
			if oldBefore.Kind != 2 {
				t.Fatal("native endpoint became historical packed image")
			}
			// Current actual originals remain independent of every reconstructed buffer.
			snapshot := sha256.Sum256(published[0])
			restored[0][64] ^= 1
			if sha256.Sum256(published[0]) != snapshot {
				t.Fatal("caller buffer alias")
			}
			if _, err := storageCompositionReadPair(published[0], published[1], f.record, f.claim); err == nil {
				t.Fatal("legacy native reader accepted V6")
			}
			if _, err := storageCompositionCaptureProjection(split[0], split[1], f.record, f.claim); err == nil {
				t.Fatal("old completed-only projection adopted mixed source cut")
			}
		})
	}
}

func TestStoragePolicyNativeBeforeRefusals(t *testing.T) {
	actual, catalog, f, _ := policyNativeFixture(t, 1)
	capsule, core, lineage, policy, sparse, err := storageOwnerV6CaptureBefore(actual, 1, f.raw, catalog, f.record, f.claim)
	storageTestMust(t, err)
	for _, tc := range []struct {
		name   string
		change func(*storageOwnerV6BeforePair)
	}{
		{"selected-bank", func(p *storageOwnerV6BeforePair) { p.Selected ^= 1 }},
		{"original-header", func(p *storageOwnerV6BeforePair) { p.Headers[0][16] ^= 1 }},
		{"full-native-hash", func(p *storageOwnerV6BeforePair) { p.Full[1][0] ^= 1 }},
		{"prepared-previous", func(p *storageOwnerV6BeforePair) { p.Packed.PeerPrevious[0] ^= 1 }},
		{"old-order-cutoff", func(p *storageOwnerV6BeforePair) { p.Cut[0][0] = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := capsule
			tc.change(&bad)
			if _, err := bad.restore(core, lineage, policy, sparse, catalog, f.record, f.claim); err == nil {
				t.Fatal("changed operand accepted")
			}
		})
	}
	raw, err := capsule.encode()
	storageTestMust(t, err)
	for _, tc := range []struct {
		name   string
		change func([]byte) []byte
	}{
		{"truncated", func(b []byte) []byte { return b[:len(b)-1] }},
		{"trailing", func(b []byte) []byte { return append(b, 0) }},
		{"unknown-recipe", func(b []byte) []byte { b[8] = 3; return b }},
		{"reserved", func(b []byte) []byte { b[10] = 1; return b }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := storageOwnerV6ReadBefore(tc.change(append([]byte{}, raw...))); err == nil {
				t.Fatal("malformed recipe accepted")
			}
		})
	}
	foreign := f.record
	foreign.Config.Object++
	if _, _, _, _, _, err := storageOwnerV6CaptureBefore(actual, 1, f.raw, catalog, foreign, f.claim); err == nil {
		t.Fatal("foreign original record accepted")
	}
}

func TestStoragePolicyNativePolicyRoundTrip(t *testing.T) {
	for _, selected := range []byte{0, 1} {
		for _, abort := range []bool{false, true} {
			t.Run(fmt.Sprintf("bank%d-abort%t", selected, abort), func(t *testing.T) {
				actual, catalog, f, _ := policyNativeFixture(t, selected)
				l := policyNativePlan(t)
				native, err := storageOwnerV6NativeAppend(actual, selected, f.raw, catalog, l, storageCompositionPlan, l.Rows.LineagePlans[0].PlanID, f.record, f.claim)
				storageTestMust(t, err)
				endpoint, err := storageOwnerV6NativeDecode(native[selected], f.record, f.claim)
				storageTestMust(t, err)
				rawSource, err := storageOwnerV6NativePolicySource(native, selected, f.raw, catalog, f.record, f.claim)
				storageTestMust(t, err)
				_, defs, err := catalog.definitions()
				storageTestMust(t, err)
				rows, err := storageOwnerV6DecodeSparse(endpoint.Overrides, defs)
				storageTestMust(t, err)
				before, err := storageOwnerV6Expand(defs, endpoint.Policy, rows)
				storageTestMust(t, err)
				afterPolicy := endpoint.Policy
				afterPolicy.Workspace.Total.Bytes--
				member := before.Workspaces[0]
				changedLimits := member.Limits
				changedLimits.Total.Bytes--
				changedRows, err := storageOwnerV6CanonicalSet(defs, rows, member.ID, member.Generation, changedLimits)
				storageTestMust(t, err)
				changedSparse, err := storageOwnerV6EncodeSparse(changedRows, defs)
				storageTestMust(t, err)
				after, err := storageOwnerV6Expand(defs, afterPolicy, changedRows)
				storageTestMust(t, err)
				original, err := storagebudget.DecodePolicyTransition(mustPolicyNativeOld(t, actual[selected], f))
				storageTestMust(t, err)
				original.Original.Revision += 3
				original.Original.Epoch++
				original.Original.Checkpoint[0]++
				original.Before, original.After = storageOwnerV6Policy(endpoint.Policy), storageOwnerV6Policy(afterPolicy)
				original.Workspace, original.WorkspaceGeneration, original.BeforeLimits, original.AfterLimits = member.ID, member.Generation, member.Limits, changedLimits
				inverse, err := storagebudget.EncodePolicyTransition(original)
				storageTestMust(t, err)
				c := storageOwnerV6PackedCatalog{Version: 6, Encoding: "packed-binary-1", Sequence: endpoint.Stage.Sequence + 1, TransitionKind: storageOwnerV6PolicyTransition, Phase: "prepared", Transition: storagebudget.ID{99}, Source: rawSource, EffectivePolicy: afterPolicy, Overrides: changedSparse, PolicyTransition: inverse}
				copy(c.PreviousCatalogDigest[:], native[selected][32:64])
				c.BeforeEffectiveDigest, err = storageOwnerV6EffectiveDigest(before)
				storageTestMust(t, err)
				c.AfterEffectiveDigest, err = storageOwnerV6EffectiveDigest(after)
				storageTestMust(t, err)
				storageTestMust(t, storageOwnerV6NativePolicyEntry(native, selected, f.raw, catalog, c, f.record, f.claim))
				for _, stale := range []struct{ epoch, revision uint64 }{{endpoint.Boundary.Epoch + 1, endpoint.Boundary.MinimumRevision}, {endpoint.Boundary.Epoch, endpoint.Boundary.MinimumRevision - 1}} {
					bad := c
					inverse := original
					inverse.Original.Epoch, inverse.Original.Revision = stale.epoch, stale.revision
					bad.PolicyTransition, err = storagebudget.EncodePolicyTransition(inverse)
					storageTestMust(t, err)
					if storageOwnerV6NativePolicyEntry(native, selected, f.raw, catalog, bad, f.record, f.claim) == nil {
						t.Fatal("policy entry ignored native current epoch/revision floor")
					}
				}
				prepared, err := storageOwnerV6PackedEncode(c, f.raw, f.record, f.claim)
				storageTestMust(t, err)
				final := c
				final.Sequence++
				final.Phase = "active"
				if abort {
					final.Phase = "aborted"
					final.EffectivePolicy = endpoint.Policy
					final.Overrides = append([]byte{}, endpoint.Overrides...)
				}
				copy(final.PreviousCatalogDigest[:], prepared[32:64])
				committed, err := storageOwnerV6PackedEncode(final, f.raw, f.record, f.claim)
				storageTestMust(t, err)
				// A later policy endpoint is not a native publication. In the
				// abort case the effective values are equal, so they alone cannot refuse it.
				mixed := native
				mixed[1-selected] = committed
				if _, err := storageOwnerV6NativeReadPair(mixed[0], mixed[1], nil, f.raw, f.record, f.claim); err == nil {
					t.Fatal("higher packed endpoint became a native publication")
				}
				rotated := [2][]byte{prepared, prepared}
				rotated[selected] = committed
				cap, oldCore, retained, policy, sparse, err := storageOwnerV6CaptureBefore(rotated, selected, f.raw, catalog, f.record, f.claim)
				storageTestMust(t, err)
				pair, err := cap.restore(oldCore, retained, policy, sparse, catalog, f.record, f.claim)
				storageTestMust(t, err)
				if !bytes.Equal(pair[0], rotated[0]) || !bytes.Equal(pair[1], rotated[1]) || !reflect.DeepEqual(retained, l) {
					t.Fatal("repeated policy/source original operands lost after rotation")
				}
				_, older, _, _, err := storageOwnerV6PackedSourceForPolicy(final, f.raw, f.record, f.claim)
				storageTestMust(t, err)
				if !bytes.Equal(older[0], native[0]) || !bytes.Equal(older[1], native[1]) {
					t.Fatal("old native policy/lineage recreated from wrong values")
				}
				projection, err := storagePolicyBeforeProjection(catalog, catalog, committed)
				storageTestMust(t, err)
				storageTestMust(t, storagePolicyRetainProjectionPeer(&projection, rotated, selected, f.raw, f.record, f.claim))
				_, sets, err := storagePolicyBuildEffectiveTable(nil, []storagePolicyEffectiveSet{projection.Policy.Effective}, catalog)
				storageTestMust(t, err)
				e := epDecode(t, f)
				wire, err := storagePolicyWriteProjection(projection, catalog, e.OriginalV3, &l, sets, f.record, f.claim)
				storageTestMust(t, err)
				if string(wire[:8]) != "DSKV6P2\x00" || len(wire) > 135168 {
					t.Fatal("native-source policy projection framing")
				}
				decoded, err := storagePolicyReadProjection(wire, catalog, e.OriginalV3, &l, sets, f.record, f.claim)
				storageTestMust(t, err)
				previousPair, err := storagePolicyProjectionPair(decoded)
				storageTestMust(t, err)
				if !bytes.Equal(previousPair[0], rotated[0]) || !bytes.Equal(previousPair[1], rotated[1]) {
					t.Fatal("policy native-source recipe changed after historical rotation")
				}

			})
		}
	}
}
func mustPolicyNativeOld(t *testing.T, raw []byte, f epComplete) []byte {
	t.Helper()
	p, err := storageOwnerV6PackedDecode(raw, f.raw, f.record, f.claim)
	storageTestMust(t, err)
	return p.PolicyTransition
}

func TestStoragePolicyNativeLineageCuts(t *testing.T) {
	for _, selected := range []byte{0, 1} {
		t.Run(fmt.Sprintf("bank%d", selected), func(t *testing.T) {
			actual, catalog, f, _ := policyNativeFixtureFrom(t, selected, true)
			rows, r, cl := ownerV5Fixture(t, true)
			rows = ownerV5Active(t, rows, r, cl)
			id := rows.LineagePlans[0].PlanID
			l := storageCompositionEmptyLineage()
			l.Rows.LineagePlans = append([]storageOwnerV5LineagePlan{}, rows.LineagePlans...)
			l.Order[0] = []storagebudget.ID{id}
			l.Initial = []storageCompositionInitial{{Mode: 1}}
			stages := []struct {
				name    string
				kind    byte
				lineage storageCompositionLineage
			}{{"P", storageCompositionPlan, l}}
			rows = ownerV5Active(t, ownerV5Complete(t, rows, r, cl), r, cl)
			l.Rows.LineageCompletions = append([]storageOwnerV5LineageCompletion{}, rows.LineageCompletions...)
			l.Order[1] = []storagebudget.ID{id}
			stages = append(stages, struct {
				name    string
				kind    byte
				lineage storageCompositionLineage
			}{"C", storageCompositionCompletion, l})
			rows = ownerV5Active(t, ownerV5Stamp(t, rows, storageOwnerV5BothPresent, r, cl), r, cl)
			l.Rows.LineageStampCompletions = append([]storageOwnerV5StampCompletionRow{}, rows.LineageStampCompletions...)
			l.Order[2] = []storagebudget.ID{id}
			stages = append(stages, struct {
				name    string
				kind    byte
				lineage storageCompositionLineage
			}{"S", storageCompositionStamp, l})
			rows = ownerV5Continue(t, rows, r, cl)
			l.Rows.LineageContinuations = append([]storageOwnerV5FromContinuation{}, rows.LineageContinuations...)
			l.Order[3] = []storagebudget.ID{id}
			stages = append(stages, struct {
				name    string
				kind    byte
				lineage storageCompositionLineage
			}{"F", storageCompositionContinuation, l})
			for _, stage := range stages {
				t.Run(stage.name, func(t *testing.T) {
					next, err := storageOwnerV6NativeAppend(actual, selected, f.raw, catalog, stage.lineage, stage.kind, id, f.record, f.claim)
					storageTestMust(t, err)
					partial := actual
					partial[1-selected] = next[1-selected]
					pair, err := storageOwnerV6NativeReadPair(partial[0], partial[1], nil, f.raw, f.record, f.claim)
					storageTestMust(t, err)
					if !pair.RecoveryOnly {
						t.Fatal("Prepared effect became normal endpoint")
					}
					final, err := storageOwnerV6NativeReadPair(next[0], next[1], nil, nil, f.record, f.claim)
					storageTestMust(t, err)
					if final.RecoveryOnly || final.Selected != selected {
						t.Fatal("final publication relation")
					}
					for _, bank := range final.Banks {
						if bank.Boundary != (storagePolicyQBoundary{3, 16}) {
							t.Fatal("P/C/S/F changed completed policy Q boundary")
						}
					}
					before, core, retained, policy, sparse, err := storageOwnerV6CaptureBefore(next, selected, f.raw, catalog, f.record, f.claim)
					storageTestMust(t, err)
					// Exact original bytes still reconstruct after a later immutable suffix.
					restored, err := before.restore(core, l, policy, sparse, catalog, f.record, f.claim)
					storageTestMust(t, err)
					if !bytes.Equal(restored[0], next[0]) || !bytes.Equal(restored[1], next[1]) || !reflect.DeepEqual(retained, stage.lineage) {
						t.Fatal("old chronological cut lost after C/S/F retention")
					}
					actual = next
				})
			}
		})
	}
}

func TestStoragePolicyNativeHistoricalProjection(t *testing.T) {
	actual, catalog, f, _ := policyNativeFixture(t, 1)
	_, defs, err := catalog.definitions()
	storageTestMust(t, err)
	if _, err := storageOwnerV6EncodeSparse(nil, defs); err == nil {
		t.Fatal("external nil sparse input was normalized instead of refused")
	}
	l := policyNativePlan(t)
	native, err := storageOwnerV6NativeAppend(actual, 1, f.raw, catalog, l, storageCompositionPlan, l.Rows.LineagePlans[0].PlanID, f.record, f.claim)
	storageTestMust(t, err)
	p, err := storagePolicyBeforeNativeProjection(catalog, catalog, native, 1, f.raw, f.record, f.claim)
	storageTestMust(t, err)
	_, sets, err := storagePolicyBuildEffectiveTable(nil, []storagePolicyEffectiveSet{p.Native.Effective}, catalog)
	storageTestMust(t, err)
	e := epDecode(t, f)
	raw, err := storagePolicyWriteProjection(p, catalog, e.OriginalV3, &l, sets, f.record, f.claim)
	storageTestMust(t, err)
	if len(raw) > 135168 || string(raw[:8]) != "DSKV6H1\x00" {
		t.Fatal("native projection framing")
	}
	got, err := storagePolicyReadProjection(raw, catalog, e.OriginalV3, &l, sets, f.record, f.claim)
	storageTestMust(t, err)
	pair, err := storagePolicyNativeProjectionPair(got)
	storageTestMust(t, err)
	if !bytes.Equal(pair[0], native[0]) || !bytes.Equal(pair[1], native[1]) || storageEnrollmentPeakProjectionFullDigest(got) != sha256.Sum256(native[1]) {
		t.Fatal("physical original lost")
	}
	wrong := append([]storagePolicyEffectiveSet{}, sets...)
	wrong[0].Workspaces = append([]storagebudget.RegisteredWorkspace{}, sets[0].Workspaces...)
	wrong[0].Workspaces[0].Limits.Total.Bytes--
	if _, err := storagePolicyReadProjection(raw, catalog, e.OriginalV3, &l, wrong, f.record, f.claim); err == nil {
		t.Fatal("foreign historical assignment accepted")
	}
	for _, at := range []int{8, 10, 16, 18, 24, 32, 48, 112, 144} {
		bad := append([]byte{}, raw...)
		bad[at] ^= 1
		if _, err := storagePolicyReadProjection(bad, catalog, e.OriginalV3, &l, sets, f.record, f.claim); err == nil {
			t.Fatal("changed retained operand accepted", at)
		}
	}
	if _, err := storageEnrollmentPeakReadProjectionV2(raw, catalog, e.OriginalV3, &l, f.record, f.claim); err == nil {
		t.Fatal("legacy reader accepted native tag")
	}
}

func TestStoragePolicyNativeR1Schedule(t *testing.T) {
	for _, path := range []byte{2, 3} {
		for _, selected := range []byte{0, 1} {
			t.Run(fmt.Sprintf("path%d-bank%d", path, selected), func(t *testing.T) {
				actual, catalog, f, _ := policyNativeFixture(t, selected)
				capsule, oldCore, _, _, _, err := storageOwnerV6CaptureBefore(actual, selected, f.raw, catalog, f.record, f.claim)
				storageTestMust(t, err)
				_, _, _, scope := storageSuccessorScopeVector(t, path)
				scope[322] = selected
				epSet64(scope[1624:1632], binary.LittleEndian.Uint64(oldCore[16:24]))
				copy(scope[88:120], capsule.Full[selected][:])
				copy(scope[40:56], f.record.Installation[:])
				copy(scope[56:88], f.record.QBinding[:])
				epReplan(&scope)
				normalized := scope
				clear(normalized[184:216])
				normalized[321] = 0
				clear(normalized[1080:1220])
				digest := epHash("desk-enrollment-control-peak-v2", normalized[:])
				copy(scope[184:216], digest[:])
				snapshot, _, err := storageOwnerV6NativeSnapshot(actual, selected, f.raw, catalog, scope, nil, f.record, f.claim)
				storageTestMust(t, err)
				envelope := storageEnrollmentPeakEnvelope{Kind: 0, BeforeCodec: 1, Sequence: binary.LittleEndian.Uint64(scope[1624:1632]) + 1, Scope: scope, Snapshot: &snapshot}
				fence, err := storageEnrollmentPeakEncodeEnvelopeNative(envelope, f.record, f.claim, true, true)
				storageTestMust(t, err)
				if _, err := storageEnrollmentPeakDecodeEnvelopeVariant(fence, f.record, f.claim, true, true); err == nil {
					t.Fatal("old core reader accepted native-only Snapshot")
				}
				// Handwritten next-Q coordinates are independent of the old E1
				// (1/10 -> 2/13) retained by f and of decoded new marker fields.
				const nextQ0Epoch, nextQ0Revision uint64 = 3, 16
				const nextQ3Epoch, nextQ3Revision uint64 = 4, 19
				current := actual
				type step struct {
					group                     uint16
					kind, phase, offset, bank byte
					dup                       bool
				}
				steps := []step{{9, 0, 255, 1, 1 - selected, false}, {10, 0, 255, 1, selected, true}, {11, 2, 1, 2, selected, false}, {12, 2, 1, 2, 1 - selected, true}, {13, 2, 2, 3, 1 - selected, false}, {14, 2, 3, 4, selected, false}, {15, 2, 3, 4, 1 - selected, true}}
				if path == 3 {
					steps = []step{{9, 0, 255, 1, 1 - selected, false}, {10, 0, 255, 1, selected, true}, {67, 2, 0, 2, selected, false}, {68, 2, 0, 2, 1 - selected, true}, {11, 2, 1, 3, 1 - selected, false}, {12, 2, 1, 3, selected, true}, {13, 2, 2, 4, selected, false}, {14, 2, 3, 5, 1 - selected, false}, {15, 2, 3, 5, selected, true}}
				}
				var lastCore []byte
				for _, step := range steps {
					t.Run(fmt.Sprintf("group%d", step.group), func(t *testing.T) {
						core := fence
						if step.dup {
							core = lastCore
						} else if step.kind == 2 {
							e := epDecode(t, f)
							e.BeforeEpoch, e.BeforeRevision = nextQ0Epoch, nextQ0Revision
							e.AfterEpoch, e.AfterRevision, e.CoveredEpoch = nextQ3Epoch, nextQ3Revision, nextQ3Epoch
							e.Scope = scope
							copy(e.Bridge[:], scope[24:40])
							copy(e.Before[:], scope[88:120])
							for i := range e.Controls {
								e.Controls[i] = scope.target(byte(i + 1))
							}
							// Exact evidence bank/extent belongs to this new scope,
							// including path3's larger pre-admitted final files.
							e.Bank = scope[323]
							if path == 2 && step.phase != 3 || path == 3 && (step.phase == 0 || step.phase == 3) {
								e.Bank = 1 - e.Bank
							}
							e.Extent = scope.final(e.Bank)
							var previous storagebudget.Digest
							copy(previous[:], lastCore[32:64])
							core = compositionTestMarkerCore(e, step.phase, binary.LittleEndian.Uint64(scope[1624:1632])+uint64(step.offset), previous)
						}
						peer := current[1-step.bank]
						raw, err := storageOwnerV6NativePublication(actual, selected, f.raw, catalog, scope, step.group, core, peer, f.record, f.claim)
						storageTestMust(t, err)
						if step.dup && !bytes.Equal(raw, peer) {
							t.Fatal("duplicate rewrote actual surviving bytes")
						}
						decoded, err := storageOwnerV6NativeDecode(raw, f.record, f.claim)
						storageTestMust(t, err)
						expected := storagePolicyQBoundary{nextQ0Epoch, nextQ0Revision}
						if step.kind == 2 && step.phase == 3 {
							expected = storagePolicyQBoundary{nextQ3Epoch, nextQ3Revision}
						}
						if decoded.Boundary != expected {
							t.Fatal("R1 cut rebound early or completion failed exact genuine Q3 rebind")
						}
						if step.group == 9 {
							// First Fence binds both halves to the independently retained Snapshot.
							for _, relative := range []int{161, 193} {
								bad := append([]byte{}, raw...)
								bad[64+520+relative] ^= 1
								compositionTestSeal(bad)
								if _, _, err := storageOwnerV6Payload(bad); err != nil {
									t.Fatal("mutation did not retain valid outer frame", err)
								}
								if _, err := storageOwnerV6NativeDecode(bad, f.record, f.claim); err == nil {
									t.Fatal("changed first-Fence original peer accepted", relative)
								}
							}
						}
						if decoded.Stage.Sequence != binary.LittleEndian.Uint64(actual[selected][16:24])+uint64(step.offset) || decoded.NativeBase != binary.LittleEndian.Uint64(actual[selected][16:24]) || !bytes.Equal(decoded.Stage.Core, core) {
							t.Fatal("physical/source sequence or frame changed")
						}
						current[step.bank] = raw
						pair, err := storageOwnerV6NativeReadPair(current[0], current[1], &catalog, f.raw, f.record, f.claim)
						storageTestMust(t, err)
						if step.group == 15 {
							if pair.RecoveryOnly || !pair.Duplicated {
								t.Fatal("completed duplicate did not close exact tail")
							}
						} else if !pair.RecoveryOnly {
							t.Fatal("pending cut became an endpoint")
						}
						lastCore = core
					})
				}
			})
		}
	}
}

func TestStoragePolicyNativeFutureClosure(t *testing.T) {
	actual, catalog, f, _ := policyNativeFixture(t, 1)
	l := policyNativePlan(t)
	pair, err := storageOwnerV6NativeAppend(actual, 1, f.raw, catalog, l, storageCompositionPlan, l.Rows.LineagePlans[0].PlanID, f.record, f.claim)
	storageTestMust(t, err)
	native, err := storageOwnerV6NativeDecode(pair[1], f.record, f.claim)
	storageTestMust(t, err)
	got, err := storageOwnerV6NativeClosureSize(native)
	storageTestMust(t, err)
	room, err := l.closureSize()
	storageTestMust(t, err)
	expectedCore := 64 + 4160 + 256 + 8320 + 96 + storageEnrollmentPeakSourceMaximum
	if got.Core != expectedCore || got.Lineage != room || got.Sparse != len(native.Overrides) || got.Recipe != 4096 || got.Future != 64+800+expectedCore+room+len(native.Overrides)+4096 || got.Current != int(binary.LittleEndian.Uint32(pair[1][24:28]))+64 {
		t.Fatal("current plus all owed envelope/receipt room is not exact")
	}
	// The unchanged hard maxima jointly fit the binary native carrier. This is
	// an arithmetic upper bound, not a claim that unrelated maxima coexist.
	upper := 64 + 800 + expectedCore + storageCompositionLineageMax + 270338 + 4096
	if upper > 8388608 {
		t.Fatal("unchanged source/lineage caps no longer close native continuation")
	}
	bad := native
	bad.Stage.Lineage = l
	bad.Stage.Lineage.Initial = nil
	if _, err := storageOwnerV6NativeClosureSize(bad); err == nil {
		t.Fatal("lost original Initial accepted by future closure")
	}
}

func TestStoragePolicyRetainedVariantObligationClosure(t *testing.T) {
	f, terminal := epForeignTicket(t, true)
	e := epDecode(t, f)
	// This pure closure vector retains a factual Reserved variant while its
	// already-admitted terminal remains owed. It does not qualify an E1
	// original checkpoint with a live slot: that checkpoint requires retirement.
	live := terminal
	live.Token.Generation = 1
	live.State = storagebudget.Reserved
	live.Evidence = storagebudget.Digest{}
	live.Changes = nil
	if storageEnrollmentPeakQValidateTicket(storageEnrollmentPeakQTicketValue(live)) != nil || storageEnrollmentPeakQRegistered(f.q[0], storageEnrollmentPeakQTicketValue(live), false) != nil {
		t.Fatal("retained Reserved variant lost complete original registration")
	}
	set := storagePolicyProjectionSet(e.Projections[0])
	in := storagePolicyClosureInput{Historical: []storagebudget.Ticket{live}, Sets: []storagePolicyEffectiveSet{set}, Dictionary: e.Inner.Catalog}
	in.Current[live.Token.Slot].Ticket = live
	request := storagebudget.Request{Workflow: live.Workflow, Operation: live.Operation, Owner: live.Owner, Components: append([]storagebudget.Component{}, live.Components...)}
	in.Owed = []storagePolicyOwedTerminal{{Plan: storagebudget.ID{94}, Action: 1, Token: live.Token, Manifest: live.Manifest, Request: request}}
	old, err := storagePolicyObligationClosure(in)
	storageTestMust(t, err)
	if old.Symbols != 1 || old.Tickets != 2 {
		t.Fatal("retained live variant replaced rather than additive terminal room")
	}
	in.Current[live.Token.Slot].Ticket = terminal
	in.Owed[0].Token = terminal.Token
	in.Owed[0].Terminal = &terminal
	realized, err := storagePolicyObligationClosure(in)
	storageTestMust(t, err)
	if realized.Symbols != 0 || realized.Tickets != 3 || realized.Bytes > old.Bytes {
		t.Fatal("mandatory terminal realization spent unpledged room or lost retained variant")
	}
	before := in.Historical[0]
	in.Owed[0].Manifest[0] = 1
	if _, err := storagePolicyObligationClosure(in); err == nil || !reflect.DeepEqual(before, in.Historical[0]) {
		t.Fatal("wrong manifest admitted or retained bytes mutated")
	}
}

func TestStoragePolicyClosureAdmissionRefusalPreservesOwed(t *testing.T) {
	f, terminal := epForeignTicket(t, true)
	e := epDecode(t, f)
	input := storagePolicyClosureInput{Sets: []storagePolicyEffectiveSet{storagePolicyProjectionSet(e.Projections[0])}, Dictionary: e.Inner.Catalog}
	// Each hand-authored historical terminal uses an original registered root.
	// Together with the canonical Empty value this reaches the closed 4224 row
	// boundary. This is a pure closure vector, not 66 factual native captures.
	for i := uint64(1); i <= 4223; i++ {
		row := terminal
		binary.LittleEndian.PutUint64(row.Token.ID[:8], i)
		row.Token.Sequence = i
		if storageEnrollmentPeakQRegistered(f.q[0], storageEnrollmentPeakQTicketValue(row), false) != nil {
			t.Fatal("historical row lost original registration")
		}
		input.Historical = append(input.Historical, row)
	}
	before, err := storagePolicyObligationClosure(input)
	storageTestMust(t, err)
	if before.Tickets != 4224 {
		t.Fatal("exact factual boundary")
	}
	t.Run("new-admission-refuses", func(t *testing.T) {
		proposed := input
		proposed.Owed = []storagePolicyOwedTerminal{{Plan: storagebudget.ID{99}, Action: 1, Uninstantiated: true}}
		if _, err := storagePolicyObligationClosure(proposed); err == nil {
			t.Fatal("new promise spent pledged table capacity")
		}
	})
	t.Run("old-obligations-still-close", func(t *testing.T) {
		after, err := storagePolicyObligationClosure(input)
		storageTestMust(t, err)
		if after != before {
			t.Fatal("refused admission changed retained current closure")
		}
	})
}

// Full expected native frame assembled independently of the production prefix,
// frame, fixed-field and lineage encoders. This fixture has a nonempty P block.
func policyNativeExpectedFrame(v storageOwnerV6NativeBank, block []byte) []byte {
	coreN := 64 + int(binary.LittleEndian.Uint32(v.Stage.Core[24:28]))
	raw := make([]byte, 8388608)
	copy(raw, "DSKOWN6\x00")
	binary.LittleEndian.PutUint32(raw[8:12], 6)
	binary.LittleEndian.PutUint32(raw[12:16], 64)
	binary.LittleEndian.PutUint64(raw[16:24], v.Stage.Sequence)
	binary.LittleEndian.PutUint32(raw[24:28], uint32(800+coreN+len(block)+len(v.Overrides)))
	b := raw[64:864]
	copy(b, "DSKV6N1\x00")
	binary.LittleEndian.PutUint16(b[8:10], 1)
	binary.LittleEndian.PutUint32(b[12:16], uint32(coreN))
	binary.LittleEndian.PutUint32(b[16:20], uint32(len(block)))
	binary.LittleEndian.PutUint32(b[20:24], uint32(len(v.Overrides)))
	policy := epBytes{}
	policy.fixed(v.Policy)
	copy(b[32:520], policy.Bytes())
	prefix := b[520:776]
	binary.LittleEndian.PutUint16(prefix[:2], 3)
	prefix[2], prefix[3], prefix[4], prefix[5] = v.Stage.CoreCodec, v.Stage.Publication, v.Stage.Transition, v.Stage.BeforeBank
	binary.LittleEndian.PutUint32(prefix[8:12], uint32(coreN))
	binary.LittleEndian.PutUint32(prefix[12:16], uint32(len(block)))
	copy(prefix[16:32], v.Stage.TransitionID[:])
	copy(prefix[32:64], v.Stage.Previous[:])
	d := sha256.Sum256(v.Stage.Core)
	copy(prefix[64:96], d[:])
	d = sha256.Sum256(block)
	copy(prefix[96:128], d[:])
	copy(prefix[128:160], v.Stage.FirstBase[:])
	copy(prefix[160:225], v.Stage.BeforePeer[:])
	binary.LittleEndian.PutUint64(prefix[225:233], v.NativeBase)
	binary.LittleEndian.PutUint64(prefix[233:241], v.Boundary.Epoch)
	binary.LittleEndian.PutUint64(prefix[241:249], v.Boundary.MinimumRevision)
	copy(raw[864:], v.Stage.Core[:coreN])
	copy(raw[864+coreN:], block)
	copy(raw[864+coreN+len(block):], v.Overrides)
	compositionTestSeal(raw)
	return raw
}
func TestStoragePolicyNativeWire(t *testing.T) {
	actual, catalog, f, _ := policyNativeFixture(t, 1)
	l := policyNativePlan(t)
	pair, err := storageOwnerV6NativeAppend(actual, 1, f.raw, catalog, l, storageCompositionPlan, l.Rows.LineagePlans[0].PlanID, f.record, f.claim)
	storageTestMust(t, err)
	v, err := storageOwnerV6NativeDecode(pair[1], f.record, f.claim)
	storageTestMust(t, err)
	block := compositionTestBlock(compositionTestPlansJSON(l.Rows.LineagePlans...), l.Order, l.Initial)
	want := policyNativeExpectedFrame(v, block)
	if !bytes.Equal(want, pair[1]) {
		t.Fatal("independent complete native frame disagrees")
	}
	for _, tc := range []struct {
		name  string
		at    int
		value byte
	}{
		{"payload-tag", 64, 0}, {"payload-version", 72, 2}, {"prefix-version", 584, 1}, {"publication", 587, 4}, {"transition", 588, 7}, {"before-bank", 589, 2}, {"core-digest", 648, 1}, {"lineage-digest", 680, 1}, {"native-reserved", 840, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := append([]byte{}, want...)
			bad[tc.at] = tc.value
			if bytes.Equal(bad, want) {
				bad[tc.at] ^= 1
			}
			compositionTestSeal(bad)
			if _, err := storageOwnerV6NativeDecode(bad, f.record, f.claim); err == nil {
				t.Fatal("malformed independently framed native image accepted")
			}
		})
	}
}

// Resealing proves refusal belongs to the original-pair relation, not the
// outer checksum. Each comparison retains the independent actual low bank.
func policyNativePeerHalfRefusals(t *testing.T, actual [2][]byte, selected byte, prepared []byte, f epComplete) {
	t.Helper()
	for _, tc := range []struct {
		name     string
		relative int
	}{{"core-digest", 161}, {"original-previous", 193}} {
		t.Run(tc.name, func(t *testing.T) {
			bad := append([]byte{}, prepared...)
			bad[64+520+tc.relative] ^= 1
			compositionTestSeal(bad)
			if _, _, err := storageOwnerV6Payload(bad); err != nil {
				t.Fatal("mutation did not retain valid outer frame", err)
			}
			pair := actual
			pair[1-selected] = bad
			if _, err := storageOwnerV6NativeReadPair(pair[0], pair[1], nil, f.raw, f.record, f.claim); err == nil {
				t.Fatal("changed original peer half accepted")
			}
		})
	}
}

func TestStoragePolicyNativeComposedOriginalPeerBinding(t *testing.T) {
	for _, selected := range []byte{0, 1} {
		t.Run(fmt.Sprintf("bank%d", selected), func(t *testing.T) {
			actual, catalog, f, _ := policyNativeFixture(t, selected)
			prepared, err := storageOwnerV6PackedDecode(actual[1-selected], f.raw, f.record, f.claim)
			storageTestMust(t, err)
			_, source, _, evidence, err := storageOwnerPackedSourceImage(prepared.Source, f.raw, f.record, f.claim)
			storageTestMust(t, err)
			core := source[0]
			marker, err := storageEnrollmentPeakDecodeEnvelopeVariant(core, f.record, f.claim, true, false)
			storageTestMust(t, err)
			if marker.Marker == nil || marker.Marker.Previous != (storagebudget.Digest{1}) {
				t.Fatal("completed source marker lost its nonzero previous")
			}
			founding := ownerAdvisoryWire(f.before)
			original, err := storageOwnerDecodeCatalogV3(founding, f.record, f.claim)
			storageTestMust(t, err)
			full := sha256.Sum256(founding)
			if original.PreviousCatalogDigest == (storagebudget.Digest{}) || !bytes.Equal(evidence.Scope[88:120], full[:]) {
				t.Fatal("independent original evidence does not bind founding operand")
			}
			var expected [65]byte
			copy(expected[1:33], full[:])
			copy(expected[33:65], original.PreviousCatalogDigest[:])
			lineage := compositionTestBlock([]byte(compositionEmptyJSON), [4][]storagebudget.ID{}, nil)
			// Independent complete physical frame: the peer belongs to the
			// retained original, not the current completed marker's predecessor.
			stage := storageCompositionBank{Sequence: 45, CoreCodec: 2, Publication: storageCompositionActive, Transition: storageCompositionEnrollment, BeforeBank: marker.Scope[322], TransitionID: marker.Marker.Bridge, Previous: storageCompositionChecksum{18}, BeforePeer: expected}
			physical := compositionTestFrame(stage, core, lineage)
			composed := [2][]byte{physical, physical}
			pair, err := storageCompositionReadPair(composed[0], composed[1], f.record, f.claim)
			storageTestMust(t, err)
			if pair.RecoveryOnly || pair.Banks[pair.Selected].BeforePeer != expected {
				t.Fatal("completed composed original rejected or changed")
			}
			prepared.Source, err = storageOwnerPackedSourceOriginal(composed, f.raw, f.record, f.claim)
			storageTestMust(t, err)
			prepared.Sequence = 46
			copy(prepared.PreviousCatalogDigest[:], physical[32:64])
			praw, err := storageOwnerV6PackedEncode(prepared, f.raw, f.record, f.claim)
			storageTestMust(t, err)
			active := prepared
			active.Sequence++
			active.Phase = "active"
			copy(active.PreviousCatalogDigest[:], praw[32:64])
			araw, err := storageOwnerV6PackedEncode(active, f.raw, f.record, f.claim)
			storageTestMust(t, err)
			actual = [2][]byte{praw, praw}
			actual[selected] = araw
			next := policyNativePlan(t)
			published, err := storageOwnerV6NativeAppend(actual, selected, f.raw, catalog, next, storageCompositionPlan, next.Rows.LineagePlans[0].PlanID, f.record, f.claim)
			storageTestMust(t, err)
			final, err := storageOwnerV6NativeReadPair(published[0], published[1], nil, nil, f.record, f.claim)
			storageTestMust(t, err)
			if final.RecoveryOnly || final.Selected != selected {
				t.Fatal("composed source did not reach native endpoint")
			}
			for _, bank := range final.Banks {
				if bank.Stage.BeforePeer != expected {
					t.Fatal("complete composed original peer operand changed")
				}
			}
			policyNativePeerHalfRefusals(t, actual, selected, published[1-selected], f)
		})
	}
}

func TestStoragePolicyNativePackedPairRefusal(t *testing.T) {
	actual, _, f, _ := policyNativeFixture(t, 1)
	if _, err := storageOwnerV6NativeReadPair(actual[1], actual[1], nil, f.raw, f.record, f.claim); err == nil {
		t.Fatal("two packed endpoints became a native publication")
	}
}

// Empty chronological groups have one internal form, matching strict decode
// and prefix reconstruction without changing their count-only wire fields.
func TestStoragePolicyNativeCanonicalLineage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		lineage storageCompositionLineage
		rows    []byte
	}{
		{"empty", storageCompositionEmptyLineage(), []byte(compositionEmptyJSON)},
		{"plan", policyNativePlan(t), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := tc.rows
			if rows == nil {
				rows = compositionTestPlansJSON(tc.lineage.Rows.LineagePlans...)
			}
			for _, order := range tc.lineage.Order {
				if order == nil {
					t.Fatal("constructed empty chronological group is not canonical")
				}
			}
			expected := compositionTestBlock(rows, tc.lineage.Order, tc.lineage.Initial)
			raw, err := tc.lineage.encode()
			storageTestMust(t, err)
			if !bytes.Equal(raw, expected) {
				t.Fatal("canonical in-memory form changed independent lineage bytes")
			}
			decoded, err := storageCompositionReadLineage(expected)
			storageTestMust(t, err)
			prefix, err := decoded.prefix(storageOwnerV6LineageCut(tc.lineage))
			storageTestMust(t, err)
			if !reflect.DeepEqual(decoded, tc.lineage) || !reflect.DeepEqual(prefix, tc.lineage) {
				t.Fatal("whole original lineage changed after strict decode/prefix")
			}
		})
	}
}

// Pure host dispatch operands exercise the same private producer/selector used
// by the real epoch. This constructs no custody, permission or native effect.
func TestStoragePolicyNativeHostEnvelopeDispatch(t *testing.T) {
	for _, selected := range []byte{0, 1} {
		t.Run(fmt.Sprintf("bank%d", selected), func(t *testing.T) {
			actual, catalog, f, _ := policyNativeFixture(t, selected)
			capsule, core, _, _, _, err := storageOwnerV6CaptureBefore(actual, selected, f.raw, catalog, f.record, f.claim)
			storageTestMust(t, err)
			_, _, _, scope := storageSuccessorScopeVector(t, 3)
			scope[322] = selected
			epSet64(scope[1624:1632], binary.LittleEndian.Uint64(core[16:24]))
			copy(scope[88:120], capsule.Full[selected][:])
			copy(scope[40:56], f.record.Installation[:])
			copy(scope[56:88], f.record.QBinding[:])
			epReplan(&scope)
			normalized := scope
			clear(normalized[184:216])
			normalized[321] = 0
			clear(normalized[1080:1220])
			digest := epHash("desk-enrollment-control-peak-v2", normalized[:])
			copy(scope[184:216], digest[:])
			snapshot, _, err := storageOwnerV6NativeSnapshot(actual, selected, f.raw, catalog, scope, nil, f.record, f.claim)
			storageTestMust(t, err)
			outer := storageEnrollmentPeakEnvelope{Kind: 0, BeforeCodec: 1, Sequence: binary.LittleEndian.Uint64(scope[1624:1632]) + 1, Scope: scope, Snapshot: &snapshot}
			prior := epDecode(t, f)
			cell := &storageInstallation{ownerRecord: &f.record, ownerClaim: f.claim, ownerCatalog: catalog, enrollmentEndpoint: &prior}
			phase := &storageEnrollmentEpoch{c: &storageBudgetConstruction{ref: &storageInstallationRef{cell: cell}}, original: &storageEnrollmentPeakOriginalComparison{Catalogs: actual}, previousEvidence: &prior}
			phase.self = phase
			fence, err := phase.encodeEnrollmentEnvelope(outer)
			storageTestMust(t, err)
			decoded, err := phase.enrollmentEnvelope(fence, true)
			storageTestMust(t, err)
			if decoded.Snapshot == nil || decoded.Snapshot.Encoding != 6 || decoded.Scope != scope {
				t.Fatal("original host producer lost Snapshot6")
			}
			if _, err = storageEnrollmentPeakEncodeEnvelope(outer, f.record, f.claim); err == nil {
				t.Fatal("legacy producer accepted native snapshot")
			}
			if _, err = storageEnrollmentPeakDecodeEnvelopeVariant(fence, f.record, f.claim, true, true); err == nil {
				t.Fatal("legacy-composition reader accepted native snapshot")
			}
			current := actual
			for _, group := range []uint16{9, 10} {
				shape, err := storageCompositionPublication(scope.path(), selected, group)
				storageTestMust(t, err)
				raw, err := storageOwnerV6NativePublication(actual, selected, f.raw, catalog, scope, group, fence, current[1-shape.Bank], f.record, f.claim)
				storageTestMust(t, err)
				current[shape.Bank] = raw
				selectedOuter, bank, err := phase.enrollmentCatalogPair(current)
				storageTestMust(t, err)
				if selectedOuter.Scope != scope || selectedOuter.Snapshot == nil || selectedOuter.Snapshot.Encoding != 6 {
					t.Fatal("native host pair selector lost Fence")
				}
				v, err := storageOwnerV6NativeDecode(current[bank], f.record, f.claim)
				storageTestMust(t, err)
				if !bytes.Equal(v.Stage.Core, fence) {
					t.Fatal("selector substituted actual physical bank for comparison core")
				}
				comparisonCore, err := phase.enrollmentCore(current[bank])
				storageTestMust(t, err)
				if !bytes.Equal(comparisonCore, fence) {
					t.Fatal("native Q preflight lost the original comparison core")
				}
				comparison, err := phase.enrollmentEnvelope(comparisonCore, true)
				if err != nil || !reflect.DeepEqual(comparison, decoded) {
					t.Fatal("native Q preflight lost the exact original envelope", err)
				}
				if _, err := phase.enrollmentEnvelope(current[bank], true); !errors.Is(err, storagebudget.ErrBounds) {
					t.Fatal("native physical frame accepted as an unwrapped core", err)
				}
				if group == 10 && !bytes.Equal(current[0], current[1]) {
					t.Fatal("duplicate changed actual whole native bytes")
				}
				if _, err = storageCompositionReadPair(current[0], current[1], f.record, f.claim); err == nil {
					t.Fatal("legacy physical reader accepted native carrier")
				}
			}
		})
	}
}
