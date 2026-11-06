package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func moveCurrentMaximumVector() storageMoveCurrent {
	// Independent canonical identity at the unchanged exact512-byte bound.
	body := []byte(`{"id":"` + strings.Repeat("a", 64) + `","path":"/` + strings.Repeat("x", 374) + `","folder":"18446744073709551615:18446744073709551615"}` + "\n")
	receipt := ownerV5Receipt(11, body, 2)
	return storageMoveCurrent{PlanID: storagebudget.ID{1}, PlanDigest: storagebudget.Digest{2}, CompletionDigest: storagebudget.Digest{3}, Current: &storageMovePublication{RequestDigest: storagebudget.Digest{4}, Receipt: receipt, Body: body}, PendingIntent: storagebudget.Digest{5}}
}

func TestStorageMoveCurrentProjection(t *testing.T) {
	t.Run("artifact-absent", func(t *testing.T) {
		v := moveCurrentMaximumVector()
		row := storageMoveCurrent{PlanID: v.PlanID, PlanDigest: v.PlanDigest, CompletionDigest: v.CompletionDigest, Current: v.Current}
		raw, err := storageMoveEncodeCurrent(row)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) != 922 {
			t.Fatalf("independent maximum row %d", len(raw))
		}
		got, err := storageMoveDecodeCurrent(raw)
		if err != nil || !reflect.DeepEqual(got, row) {
			t.Fatal("storage-owned row without artifact owner", err)
		}
	})
	t.Run("replace-stale-and-detached", func(t *testing.T) {
		v := moveCurrentMaximumVector()
		old := storageMoveCurrent{PlanID: v.PlanID, PlanDigest: v.PlanDigest, CompletionDigest: v.CompletionDigest, Current: v.Current}
		rows, err := storageMoveReplaceCurrent([]storageMoveCurrent{}, nil, old)
		if err != nil {
			t.Fatal(err)
		}
		// Same P and origin, new exact pending descriptor; no move-history append.
		next := old
		next.PendingIntent = storagebudget.Digest{18}
		after, err := storageMoveReplaceCurrent(rows, &old, next)
		if err != nil || len(after) != 1 {
			t.Fatal("replace", err)
		}
		if _, err = storageMoveReplaceCurrent(after, &old, next); err != storagebudget.ErrStale {
			t.Fatal("stale whole value accepted", err)
		}
		foreign := next
		foreign.PlanDigest[0]++
		if _, err = storageMoveReplaceCurrent(after, &next, foreign); err != storagebudget.ErrStale {
			t.Fatal("origin changed", err)
		}
		after[0].Current.Body[0] = 'x'
		if rows[0].Current.Body[0] != '{' || next.Current.Body[0] != '{' {
			t.Fatal("projection aliases original body")
		}
	})
	t.Run("table-bound-and-canonical", func(t *testing.T) {
		v := moveCurrentMaximumVector()
		rows := make([]storageMoveCurrent, 1024)
		for i := range rows {
			rows[i] = storageMoveCurrent{PlanID: storagebudget.ID{byte((i + 1) >> 8), byte(i + 1)}, PlanDigest: v.PlanDigest, CompletionDigest: v.CompletionDigest, Current: v.Current}
		}
		raw, err := storageMoveEncodeCurrents(rows)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) != 946180 {
			t.Fatalf("exact maximum table bytes %d", len(raw))
		}
		got, err := storageMoveDecodeCurrents(raw)
		if err != nil || !reflect.DeepEqual(got, rows) {
			t.Fatal("full closed table", err)
		}
		if _, err = storageMoveEncodeCurrents(append(rows, rows[0])); err == nil {
			t.Fatal("1025rows accepted")
		}
		rows[1] = rows[0]
		if _, err = storageMoveEncodeCurrents(rows); err == nil {
			t.Fatal("duplicate P accepted")
		}
		if _, err = storageMoveDecodeCurrents(append(raw, 0)); err == nil {
			t.Fatal("trailing table bytes accepted")
		}
	})
}

func moveStorageBankFixture(t *testing.T) (storageCompositionBank, storageMoveCurrent, storageOwnerRecordV2, storageOwnerClaimV2) {
	t.Helper()
	catalog, record, claim := ownerV5Fixture(t, false)
	catalog = ownerV5Complete(t, catalog, record, claim)
	coreCatalog := catalog.old()
	coreCatalog.Phase = "active"
	core := ownerAdvisoryWire(coreCatalog)
	lineage := storageCompositionEmptyLineage()
	lineage.Rows.LineagePlans = catalog.LineagePlans
	lineage.Rows.LineageCompletions = catalog.LineageCompletions
	p := catalog.LineagePlans[0]
	lineage.Order[0] = []storagebudget.ID{p.PlanID}
	lineage.Order[1] = []storagebudget.ID{p.PlanID}
	lineage.Initial = []storageCompositionInitial{{Mode: 1}}
	wire, err := lineage.encode()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := storageOwnerV5PlanDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	row := storageMoveCurrent{PlanID: p.PlanID, PlanDigest: digest, CompletionDigest: sha256.Sum256(storageMoveReceiptWire(catalog.LineageCompletions[0].Initial)), PendingIntent: storagebudget.Digest{91}}
	bank := storageCompositionBank{Sequence: 40, CoreCodec: 1, Publication: storageCompositionActive, Transition: storageCompositionCompletion, BeforeBank: 0, TransitionID: p.PlanID, Previous: storageCompositionChecksum{18}, CoreFull: storageCompositionCoreDigest(sha256.Sum256(core)), LineageDigest: sha256.Sum256(wire), FirstBase: storagebudget.Digest{19}, Core: core, Lineage: lineage}
	copy(bank.BeforePeer[1:33], bank.CoreFull[:])
	copy(bank.BeforePeer[33:65], coreCatalog.PreviousCatalogDigest[:])
	return bank, row, record, claim
}
func TestStorageMoveBankCurrentness(t *testing.T) {
	for _, which := range []string{"bank0", "bank1"} {
		t.Run(which, func(t *testing.T) {
			low, row, record, claim := moveStorageBankFixture(t)
			original, err := low.encode(record, claim)
			if err != nil {
				t.Fatal("original P/C bank", err)
			}
			prepared := low
			prepared.Sequence++
			prepared.Transition = storageCompositionMove
			prepared.TransitionID = storagebudget.ID{71}
			prepared.Publication = storageCompositionPrepared
			prepared.Moves = []storageMoveCurrent{row}
			copy(prepared.Previous[:], original[32:64])
			pending, err := prepared.encode(record, claim)
			if err != nil {
				t.Fatal("pending", err)
			}
			if binary.LittleEndian.Uint16(pending[64:66]) != 2 || len(pending) != 8388608 {
				t.Fatal("explicit version/extent")
			}
			pairBytes := [2][]byte{original, pending}
			selected := byte(1)
			if which == "bank0" {
				pairBytes = [2][]byte{pending, original}
				selected = 0
			}
			pair, err := storageCompositionReadPair(pairBytes[0], pairBytes[1], record, claim)
			if err != nil || !pair.RecoveryOnly || pair.Selected != selected {
				t.Fatal("pending selection", err)
			}
			active := prepared
			active.Sequence++
			active.Publication = storageCompositionActive
			copy(active.Previous[:], pending[32:64])
			identity, err := parseIdentity(low.Lineage.Rows.LineagePlans[0].IdentityInitial)
			if err != nil {
				t.Fatal(err)
			}
			identity.Path = "/moved-current-project"
			body := identity.line()
			receipt := ownerV5Receipt(8, body, 2)
			receipt.OperationID = prepared.TransitionID
			latest := row
			latest.PendingIntent = storagebudget.Digest{}
			latest.Current = &storageMovePublication{RequestDigest: row.PendingIntent, Receipt: receipt, Body: body}
			active.Moves = []storageMoveCurrent{latest}
			committed, err := active.encode(record, claim)
			if err != nil {
				t.Fatal("committed", err)
			}
			pairBytes = [2][]byte{pending, committed}
			if which == "bank0" {
				pairBytes = [2][]byte{committed, pending}
			}
			pair, err = storageCompositionReadPair(pairBytes[0], pairBytes[1], record, claim)
			if err != nil || pair.RecoveryOnly || pair.Selected != selected {
				t.Fatal("latest selected", err)
			}
			// Keep immutable P/C and source bytes exact. Only latest projection changed.
			if !reflect.DeepEqual(pair.Banks[pair.Selected].Lineage, low.Lineage) || !bytes.Equal(pair.Banks[pair.Selected].Core, low.Core) {
				t.Fatal("origin changed")
			}
			wrong := active
			wrong.Moves = append([]storageMoveCurrent{}, active.Moves...)
			wrong.Moves[0].Current = &storageMovePublication{RequestDigest: storagebudget.Digest{92}, Receipt: receipt, Body: body}
			bad, err := wrong.encode(record, claim)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = storageCompositionReadPair(pending, bad, record, claim); err == nil {
				t.Fatal("wrong immutable request accepted")
			}
			wrong = active
			wrong.Moves = nil
			if _, err = wrong.encode(record, claim); err == nil {
				t.Fatal("move hidden in legacy frame accepted")
			}
		})
	}
}

func TestStorageMoveOperandFactoring(t *testing.T) {
	v := moveCurrentMaximumVector()
	pending := storageMoveCurrent{PlanID: v.PlanID, PlanDigest: v.PlanDigest, CompletionDigest: v.CompletionDigest, PendingIntent: storagebudget.Digest{71}}
	committed := pending
	committed.Current, committed.PendingIntent = v.Current, storagebudget.Digest{}
	pendingSet, committedSet := []storageMoveCurrent{pending}, []storageMoveCurrent{committed}
	t.Run("actual-set-reconstruction", func(t *testing.T) {
		// Two retained banks need different actual sets. A later source may
		// already retain both; repeated bytes share storage, not authority.
		table, selectors, err := storageMoveInternOperands([][]storageMoveCurrent{pendingSet, committedSet, pendingSet})
		if err != nil || len(table.Sets) != 2 || selectors[0] != selectors[2] || selectors[0] == selectors[1] {
			t.Fatal("exact actual interning", err)
		}
		for i, set := range [][]storageMoveCurrent{pendingSet, committedSet} {
			original, err := table.reconstruct([]uint16{selectors[i]})
			if err != nil || len(original.Sets) != 1 || !reflect.DeepEqual(original.Sets[0], set) {
				t.Fatal("original table not reconstructed", i, err)
			}
			original.Sets[0][0].PlanDigest[0] ^= 1
			got, err := table.selectSet(selectors[i])
			if err != nil || !reflect.DeepEqual(got, set) {
				t.Fatal("reconstructed alias", err)
			}
		}
		wire, err := storageMoveEncodeOperandTable(table)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = storageMoveDecodeOperandTable(append(wire, 0)); err == nil {
			t.Fatal("trailing table byte accepted")
		}
		if _, err = table.reconstruct([]uint16{uint16(len(table.Sets))}); err == nil {
			t.Fatal("foreign selector accepted")
		}
		if _, err = table.reconstruct([]uint16{0, 0}); err == nil {
			t.Fatal("duplicate original table accepted")
		}
		if _, err = table.reconstruct([]uint16{1, 0}); err == nil {
			t.Fatal("reordered original table accepted")
		}
	})
	t.Run("native-prefix-original-local-selection", func(t *testing.T) {
		bank, _, _, _ := moveStorageBankFixture(t)
		// This is prefix/operand framing only; the native E1/Q/pair issuer
		// remains a separate mandatory join and is not fabricated here.
		for _, set := range [][]storageMoveCurrent{pendingSet, committedSet} {
			bank.Moves = set
			prefix, err := storageOwnerV6NativeStagePrefix(bank, 12, storagePolicyQBoundary{4, 9})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, err = storageOwnerV6NativeReadStagePrefix(prefix, bank.Sequence, bank.Core, bank.Lineage); err == nil {
				t.Fatal("context-free reader gained rows")
			}
			table, _, _, err := storageMoveBankOperands(bank)
			if err != nil {
				t.Fatal(err)
			}
			got, _, _, err := storageOwnerV6NativeReadStagePrefixWithOperands(prefix, bank.Sequence, bank.Core, bank.Lineage, table)
			if err != nil || !reflect.DeepEqual(got.Moves, set) {
				t.Fatal("retained original table selection", err)
			}
			bad := bytes.Clone(prefix)
			binary.LittleEndian.PutUint16(bad[249:251], 1)
			if _, _, _, err = storageOwnerV6NativeReadStagePrefixWithOperands(bad, bank.Sequence, bank.Core, bank.Lineage, table); err == nil {
				t.Fatal("foreign local selector accepted")
			}
			bad = bytes.Clone(prefix)
			binary.LittleEndian.PutUint32(bad[251:255], binary.LittleEndian.Uint32(bad[251:255])+1)
			if _, _, _, err = storageOwnerV6NativeReadStagePrefixWithOperands(bad, bank.Sequence, bank.Core, bank.Lineage, table); err == nil {
				t.Fatal("wrong original table length accepted")
			}
		}
	})
	t.Run("old-inline-counterexamples", func(t *testing.T) {
		a, err := storageMoveEncodeCurrents(pendingSet)
		if err != nil {
			t.Fatal(err)
		}
		b, err := storageMoveEncodeCurrents(committedSet)
		if err != nil {
			t.Fatal(err)
		}
		if len(a) != 121 || len(b) != 928 {
			t.Fatal("actual maximum row-set operands", len(a), len(b))
		}
		// These are the concrete discarded inline layouts, not a refusal
		// claim about the factored format or a source-valid native execution.
		if 2424+736+8+len(a)+len(b) != 4217 || 2424+736+8+2*len(b) != 5024 {
			t.Fatal("counterexample changed")
		}
		if 4217 <= storageOwnerV6BeforeMax || 5024 <= storageOwnerV6BeforeMax {
			t.Fatal("existing cap changed")
		}
	})
}

// Reuse independently authored valid P/C and completed E1 graphs. These bytes
// are pure source/recipe vectors; no Q admission or actual native result is
// inferred from constructing a comparison receipt.
func moveNativeCompleteVector(t *testing.T, selected byte) ([2][]byte, storageOwnerCatalogV2, epComplete, storageMoveCurrent) {
	t.Helper()
	actual, catalog, f, _ := policyNativeFixture(t, selected)
	rows, r, cl := ownerV5Fixture(t, false)
	l := storageCompositionEmptyLineage()
	p := rows.LineagePlans[0]
	l.Rows.LineagePlans = append([]storageOwnerV5LineagePlan{}, rows.LineagePlans...)
	l.Order[0], l.Initial = []storagebudget.ID{p.PlanID}, []storageCompositionInitial{{Mode: 1}}
	var err error
	actual, err = storageOwnerV6NativeAppend(actual, selected, f.raw, catalog, l, storageCompositionPlan, p.PlanID, f.record, f.claim)
	storageTestMust(t, err)
	rows = ownerV5Complete(t, rows, r, cl)
	l.Rows.LineageCompletions = append([]storageOwnerV5LineageCompletion{}, rows.LineageCompletions...)
	l.Order[1] = []storagebudget.ID{p.PlanID}
	actual, err = storageOwnerV6NativeAppend(actual, selected, f.raw, catalog, l, storageCompositionCompletion, p.PlanID, f.record, f.claim)
	storageTestMust(t, err)
	d, err := storageOwnerV5PlanDigest(p)
	storageTestMust(t, err)
	row := storageMoveCurrent{PlanID: p.PlanID, PlanDigest: d, CompletionDigest: sha256.Sum256(storageMoveReceiptWire(rows.LineageCompletions[0].Initial)), PendingIntent: storagebudget.Digest{71}}
	return actual, catalog, f, row
}

func TestStorageMoveNativeOperandPair(t *testing.T) {
	for _, name := range []string{"bank0", "bank1"} {
		t.Run(name, func(t *testing.T) {
			selected := byte(0)
			if name == "bank1" {
				selected = 1
			}
			actual, catalog, f, row := moveNativeCompleteVector(t, selected)
			operation := storagebudget.ID{72}
			pending, bank, err := storageMoveNativePublication(actual, selected, f.raw, catalog, operation, []storageMoveCurrent{row}, f.record, f.claim)
			storageTestMust(t, err)
			actual[bank] = pending
			if bank != 1-selected {
				t.Fatal("pending changed wrong bank")
			}
			// Abort has a closed distinct inverse, without inventing a current
			// publication receipt or retaining an unreferenced pending-only row.
			aborted, abortBank, err := storageMoveNativePublication(actual, bank, f.raw, catalog, operation, []storageMoveCurrent{}, f.record, f.claim)
			storageTestMust(t, err)
			abortPair := actual
			abortPair[abortBank] = aborted
			ap, err := storageOwnerV6NativeReadPair(abortPair[0], abortPair[1], nil, nil, f.record, f.claim)
			storageTestMust(t, err)
			if ap.RecoveryOnly || len(ap.Banks[ap.Selected].Stage.Moves) != 0 {
				t.Fatal("factual abort did not restore prior set")
			}
			original, _, err := row.origin(ap.Banks[ap.Selected].Stage.Lineage)
			storageTestMust(t, err)
			identity, err := parseIdentity(original.IdentityInitial)
			storageTestMust(t, err)
			identity.Path = "/moved-original"
			body := identity.line()
			receipt := ownerV5Receipt(8, body, 2)
			receipt.OperationID = operation
			committed := row
			committed.PendingIntent = storagebudget.Digest{}
			committed.Current = &storageMovePublication{RequestDigest: row.PendingIntent, Receipt: receipt, Body: body}
			active, activeBank, err := storageMoveNativePublication(actual, bank, f.raw, catalog, operation, []storageMoveCurrent{committed}, f.record, f.claim)
			storageTestMust(t, err)
			actual[activeBank] = active
			if activeBank != selected {
				t.Fatal("terminal changed wrong bank")
			}
			before, core, lineage, policy, sparse, err := storageOwnerV6CaptureBefore(actual, selected, f.raw, catalog, f.record, f.claim)
			storageTestMust(t, err)
			if before.Kind != 3 || len(before.MoveOperands.Sets) != 2 {
				t.Fatal("actual two-bank operands missing")
			}
			recipe, err := before.encode()
			storageTestMust(t, err)
			if len(recipe) != 744 || len(recipe) > storageOwnerV6BeforeMax {
				t.Fatal("exact factored recipe", len(recipe))
			}
			decoded, err := storageOwnerV6ReadBefore(recipe)
			storageTestMust(t, err)
			if _, err = decoded.restore(core, lineage, policy, sparse, catalog, f.record, f.claim); err == nil {
				t.Fatal("recipe gained operand authority without bytes")
			}
			decoded.MoveOperands = before.MoveOperands
			restored, err := decoded.restore(core, lineage, policy, sparse, catalog, f.record, f.claim)
			storageTestMust(t, err)
			if !bytes.Equal(restored[0], actual[0]) || !bytes.Equal(restored[1], actual[1]) {
				t.Fatal("original full banks changed")
			}
			source, err := storageOwnerV6NativePolicySource(actual, selected, f.raw, catalog, f.record, f.claim)
			storageTestMust(t, err)
			dto, _, err := storageOwnerDecodePackedSource(source)
			storageTestMust(t, err)
			if dto.Version != 2 || dto.Encoding != 4 || !bytes.Equal(dto.Physical, recipe) {
				t.Fatal("factored source version/recipe")
			}
			// Mutate each actual original operand independently, keeping valid
			// canonical table framing. Full original bank binding must refuse.
			for half := range before.MoveOperands.Sets {
				wire, e := storageMoveEncodeOperandTable(before.MoveOperands)
				storageTestMust(t, e)
				badTable, e := storageMoveDecodeOperandTable(wire)
				storageTestMust(t, e)
				badTable.Sets[half][0].PlanDigest[0] ^= 1
				badTable, mapping, e := storageMoveInternOperands(badTable.Sets)
				storageTestMust(t, e)
				bad := before
				bad.MoveOperands = badTable
				for i, old := range before.NativeMoveMap {
					bad.NativeMoveMap[i] = make([]uint16, len(old))
					for j, index := range old {
						bad.NativeMoveMap[i][j] = mapping[index]
					}
				}
				if _, e = bad.restore(core, lineage, policy, sparse, catalog, f.record, f.claim); e == nil {
					t.Fatal("foreign original operand accepted", half)
				}
			}
		})
	}
}

func moveRequestVector(t *testing.T) (storageMoveRequest, storageIdentityIntent, storageCompositionLineage) {
	t.Helper()
	bank, selector, _, _ := moveStorageBankFixture(t)
	plan, receipt, err := selector.origin(bank.Lineage)
	storageTestMust(t, err)
	identity, err := parseIdentity(plan.IdentityInitial)
	storageTestMust(t, err)
	previous := identity.Path
	identity.Path = "/request-destination"
	r := storageMoveRequest{Installation: plan.Installation, Binding: plan.QBinding, Operation: storagebudget.ID{73}, Plan: plan.PlanID, PlanDigest: selector.PlanDigest, CompletionDigest: selector.CompletionDigest, ConfigRevision: storagebudget.Digest{74}, Previous: []byte(previous), Destination: []byte(identity.Path), Parent: plan.PrivateObject, ExpectedOld: receipt.PublishedObject, PayloadDigest: sha256.Sum256(identity.line())}
	old, after, err := r.operands(bank.Lineage, nil)
	storageTestMust(t, err)
	j := storageIdentityIntentFixture()
	j.Action, j.PlanDigest, j.Parent, j.ExpectedOld, j.PayloadDigest, j.Presence = storageIdentityIntentMove, r.PlanDigest, r.Parent, r.ExpectedOld, r.PayloadDigest, 17
	j.Core.OperationID = r.Operation
	j.Core.CASBinding, err = r.digest()
	storageTestMust(t, err)
	j.Core.Target, j.Core.Stage = startupIdentityName, identityStagingPrefix+hex.EncodeToString(r.Operation[:])
	j.Core.Operation = "p:" + hex.EncodeToString(r.PlanDigest[:]) + ":move"
	j.Core.OldExists, j.Core.OldLength, j.Core.OldLinkCount, j.Core.NewLength = true, uint64(len(old)), 1, uint64(len(after))
	j.Core.OldIdentity = storageDirectoryIdentity{Device: 1, Inode: 2}
	storageTestMust(t, j.validate())
	return r, j, bank.Lineage
}
func TestStorageMoveTypedRequest(t *testing.T) {
	t.Run("exact-origin-and-action", func(t *testing.T) {
		r, j, lineage := moveRequestVector(t)
		raw, err := r.encode()
		storageTestMust(t, err)
		got, err := storageMoveReadRequest(raw)
		storageTestMust(t, err)
		if !reflect.DeepEqual(got, r) || !r.identityBound(j, lineage, nil) {
			t.Fatal("immutable original request join")
		}
		old, after, err := r.operands(lineage, nil)
		storageTestMust(t, err)
		before, _ := parseIdentity(old)
		next, _ := parseIdentity(after)
		if before.ID != next.ID || before.Folder != next.Folder || before.From != next.From || next.Path != string(r.Destination) || bytes.Equal(old, after) {
			t.Fatal("admitted identity semantics changed")
		}
		got.Destination[0] = 'x'
		if !bytes.Equal(r.Destination, []byte("/request-destination")) {
			t.Fatal("decoded request aliases source")
		}
		j.Action = storageIdentityIntentInitialCAS
		j.Core.Operation = "p:" + hex.EncodeToString(r.PlanDigest[:]) + ":initial-cas"
		if r.identityBound(j, lineage, nil) {
			t.Fatal("founding CAS token treated as move action")
		}
	})
	t.Run("foreign-stale-and-bounds", func(t *testing.T) {
		r, j, lineage := moveRequestVector(t)
		for _, change := range []func(*storageMoveRequest){
			func(v *storageMoveRequest) { v.PlanDigest[0] ^= 1 },
			func(v *storageMoveRequest) { v.CompletionDigest[0] ^= 1 },
			func(v *storageMoveRequest) { v.ExpectedOld.Object++ },
			func(v *storageMoveRequest) { v.Parent.Object++ },
			func(v *storageMoveRequest) { v.Binding[0] ^= 1 },
			func(v *storageMoveRequest) { v.Previous = []byte("/foreign-path") },
			func(v *storageMoveRequest) { v.BeforeCurrent = storagebudget.Digest{3} },
			func(v *storageMoveRequest) { v.Destination = []byte("/" + strings.Repeat("x", 512)) },
		} {
			bad := r
			change(&bad)
			if bad.identityBound(j, lineage, nil) {
				t.Fatal("foreign original request accepted")
			}
		}
		raw, err := r.encode()
		storageTestMust(t, err)
		for _, n := range []int{0, 7, len(raw) - 1} {
			if _, err = storageMoveReadRequest(raw[:n]); err == nil {
				t.Fatal("truncated request", n)
			}
		}
		if _, err = storageMoveReadRequest(append(raw, 0)); err == nil {
			t.Fatal("trailing request accepted")
		}
	})
	t.Run("closed-j5-subformat", func(t *testing.T) {
		r, j, _ := moveRequestVector(t)
		raw, err := storageEncodeIdentityIntent(j, r.Installation, r.Binding)
		storageTestMust(t, err)
		if string(raw[:8]) != "DSKHINT5" || raw[13] != 3 || binary.LittleEndian.Uint16(raw[storageIdentityIntentExtension:storageIdentityIntentExtension+2]) != 3 || len(raw) != 16384 {
			t.Fatal("explicit move subformat")
		}
		got, err := storageDecodeIdentityIntent(raw, int(j.Core.Slot), r.Installation, r.Binding)
		storageTestMust(t, err)
		if !reflect.DeepEqual(got, j) {
			t.Fatal("move J operand loss")
		}
		for _, tag := range []byte{0, 1, 2, 4} {
			bad := bytes.Clone(raw)
			bad[13] = tag
			storageIdentityIntentSeal(bad)
			if _, err = storageDecodeIdentityIntent(bad, int(j.Core.Slot), r.Installation, r.Binding); err == nil {
				t.Fatal("move relabeled into another format", tag)
			}
		}
		legacy := j
		legacy.Action = storageIdentityIntentInitialCAS
		legacy.Core.Operation = "p:" + hex.EncodeToString(r.PlanDigest[:]) + ":initial-cas"
		old, err := storageEncodeIdentityIntent(legacy, r.Installation, r.Binding)
		storageTestMust(t, err)
		if old[13] != 1 || binary.LittleEndian.Uint16(old[storageIdentityIntentExtension:storageIdentityIntentExtension+2]) != 2 {
			t.Fatal("legacy changed")
		}
		old[13] = 3
		binary.LittleEndian.PutUint16(old[storageIdentityIntentExtension:storageIdentityIntentExtension+2], 3)
		storageIdentityIntentSeal(old)
		if _, err = storageDecodeIdentityIntent(old, int(j.Core.Slot), r.Installation, r.Binding); err == nil {
			t.Fatal("legacy action acquired move subformat")
		}
	})
}

func TestStorageMoveBootstrapOriginalJoin(t *testing.T) {
	r, j, lineage := moveRequestVector(t)
	selector := storageMoveCurrent{PlanID: r.Plan, PlanDigest: r.PlanDigest, CompletionDigest: r.CompletionDigest}
	plan, origin, err := selector.originOperands(lineage)
	storageTestMust(t, err)
	old, after, err := r.operands(lineage, nil)
	storageTestMust(t, err)
	requestDigest, err := r.digest()
	storageTestMust(t, err)
	pending := selector
	pending.PendingIntent = requestDigest
	commit := selector
	receipt := ownerV5Receipt(8, after, 2)
	receipt.OperationID = r.Operation
	commit.Current = &storageMovePublication{RequestDigest: requestDigest, Receipt: receipt, Body: after}
	join := func(rows []storageMoveCurrent, path []byte, native storageOwnerPersistentObject, body []byte, request *storageMoveRequest, journal *storageIdentityIntent) (storageMoveBootstrap, error) {
		return storageMoveBootstrapJoin(lineage, rows, plan.ProjectObject, plan.PrivateObject, plan.ConfigObject, native, path, body, request, journal)
	}
	t.Run("unchanged-original", func(t *testing.T) {
		got, e := join(nil, r.Previous, origin.PublishedObject, old, nil, nil)
		storageTestMust(t, e)
		if got.Pending || got.Current != nil || !bytes.Equal(got.Plan.HistoryIdentity, plan.HistoryIdentity) || !bytes.Equal(got.Plan.AccountingOwnerKey, plan.AccountingOwnerKey) {
			t.Fatal("original accounting changed")
		}
		if _, e = join(nil, r.Destination, origin.PublishedObject, old, nil, nil); e == nil {
			t.Fatal("plain moved path granted original history")
		}
	})
	t.Run("committed-intent-removed", func(t *testing.T) {
		got, e := join([]storageMoveCurrent{commit}, r.Destination, receipt.PublishedObject, after, nil, nil)
		storageTestMust(t, e)
		if got.Pending || got.Current == nil || !reflect.DeepEqual(got.Plan, plan) {
			t.Fatal("committed original tuple changed")
		}
		got.Plan.HistoryIdentity[0] ^= 1
		got.Current.Current.Body[0] ^= 1
		if !bytes.Equal(commit.Current.Body, after) || !bytes.Equal(lineage.Rows.LineagePlans[0].HistoryIdentity, plan.HistoryIdentity) {
			t.Fatal("bootstrap operands alias original rows")
		}
		if _, e = join([]storageMoveCurrent{commit}, r.Destination, origin.PublishedObject, after, nil, nil); e == nil {
			t.Fatal("copied/replaced identity accepted")
		}
	})
	t.Run("pending-before-and-after", func(t *testing.T) {
		for _, path := range [][]byte{r.Previous, r.Destination} {
			got, e := join([]storageMoveCurrent{pending}, path, r.ExpectedOld, old, &r, &j)
			storageTestMust(t, e)
			if !got.Pending {
				t.Fatal("pending move declared complete")
			}
		}
		staged := j
		staged.Core.Generation = 9
		staged.Core.PreviousPageDigest = storagebudget.Digest{85}
		staged.Core.Phase = storageIntentStaging
		staged.Core.PolicyRevision = 1
		staged.Core.PreviousToken = storagebudget.Token{Slot: 1, ID: storagebudget.ID{86}, Sequence: 2, Generation: 1}
		staged.Core.Token = staged.Core.PreviousToken
		staged.Core.Token.Generation++
		staged.Core.StageExists = true
		staged.NativePhase = storageIdentityEntered
		staged.Presence = 19
		staged.AllocatedStage = storageIdentityIntentPO(87, 2)
		staged.AllocatedLinks = 1
		staged.Facts = 127
		storageTestMust(t, staged.validate())
		got, e := join([]storageMoveCurrent{pending}, r.Destination, staged.AllocatedStage, after, &r, &staged)
		storageTestMust(t, e)
		if !got.Pending {
			t.Fatal("unretired native observation completed move")
		}
		if _, e = join([]storageMoveCurrent{pending}, r.Previous, staged.AllocatedStage, after, &r, &staged); e == nil {
			t.Fatal("impossible old-path/rewritten-identity accepted")
		}
		if _, e = join([]storageMoveCurrent{pending}, r.Destination, staged.AllocatedStage, after, &r, &j); e == nil {
			t.Fatal("unrecorded staged native accepted")
		}
	})
	t.Run("foreign-and-stale", func(t *testing.T) {
		if _, e := join([]storageMoveCurrent{pending}, r.Destination, r.ExpectedOld, old, nil, nil); e == nil {
			t.Fatal("missing pending intent accepted")
		}
		bad := r
		bad.Operation[0] ^= 1
		if _, e := join([]storageMoveCurrent{pending}, r.Destination, r.ExpectedOld, old, &bad, &j); e == nil {
			t.Fatal("foreign request accepted")
		}
		for _, field := range []string{"project", "private", "config"} {
			p, private, c := plan.ProjectObject, plan.PrivateObject, plan.ConfigObject
			switch field {
			case "project":
				p.Object++
			case "private":
				private.Object++
			case "config":
				c.Object++
			}
			if _, e := storageMoveBootstrapJoin(lineage, []storageMoveCurrent{commit}, p, private, c, receipt.PublishedObject, r.Destination, after, nil, nil); e == nil {
				t.Fatal("foreign original native accepted", field)
			}
		}
	})
}

func TestStorageMoveIntentV2(t *testing.T) {
	request, _, lineage := moveRequestVector(t)
	old, _, err := request.operands(lineage, nil)
	storageTestMust(t, err)
	identity, err := parseIdentity(old)
	storageTestMust(t, err)
	base := projectMoveIntent{Version: 2, Previous: string(request.Previous), Destination: string(request.Destination), Folder: identity.Folder, Identity: identity.ID, ConfigRevision: hex.EncodeToString(request.ConfigRevision[:]), Storage: &request}
	t.Run("artifact-absent", func(t *testing.T) {
		raw, e := storageMoveEncodeIntent(base)
		storageTestMust(t, e)
		got, e := storageMoveReadIntent(raw)
		storageTestMust(t, e)
		if !reflect.DeepEqual(got, base) || len(raw) > storageMoveIntentMaximum {
			t.Fatal("exact absent-artifact intent")
		}
		got.Storage.Previous[0] ^= 1
		if !bytes.Equal(request.Previous, []byte(base.Previous)) {
			t.Fatal("intent decoder alias")
		}
	})
	t.Run("exact-optional-locators", func(t *testing.T) {
		before := dialectArtifactLocationRecord{Version: 2, Owner: strings.Repeat("a", 64), Kind: "file", LegacySHA256: strings.Repeat("b", 64), FolderIdentity: identity.Folder, WorkDomain: strings.Repeat("c", 64), Location: dialectArtifactLocation{Kind: "absolute", PathBytes: request.Previous}}
		after := before
		after.Location.PathBytes = request.Destination
		br, e := marshalDialectArtifactLocation(before)
		storageTestMust(t, e)
		ar, e := marshalDialectArtifactLocation(after)
		storageTestMust(t, e)
		v := base
		v.Retained = &dialectArtifactProjectMovePlan{Version: 1, Owner: before.Owner, Kind: "file", LegacySHA256: before.LegacySHA256, MarkerSHA256: strings.Repeat("d", 64), Namespace: strings.Repeat("e", 64), NativeScopeSHA256: strings.Repeat("f", 64), WorkDomain: before.WorkDomain, WorkSHA256: strings.Repeat("1", 64), Before: br, After: ar}
		bound := request
		planDigest, planErr := storageMoveArtifactPlan(v.Retained)
		storageTestMust(t, planErr)
		bound.Artifact = &storageMoveArtifactRequest{Plan: planDigest, Root: storagebudget.ID{81}, Parent: epNative(82, 1), Before: epNative(83, 2), BeforeBytes: uint64(len(br)), AfterBytes: uint64(len(ar))}
		v.Storage = &bound
		raw, e := storageMoveEncodeIntent(v)
		storageTestMust(t, e)
		got, e := storageMoveReadIntent(raw)
		storageTestMust(t, e)
		if !reflect.DeepEqual(got, v) || !bytes.Equal(got.Retained.Before, br) || !bytes.Equal(got.Retained.After, ar) {
			t.Fatal("original optional artifact bytes changed")
		}
		// No artifact location format or path domain changed. This compact
		// owner intent stores the original accepted locator bytes verbatim.
		if _, e = readDialectArtifactLocationRecord(got.Retained.Before); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("closed-frame-and-legacy", func(t *testing.T) {
		raw, e := storageMoveEncodeIntent(base)
		storageTestMust(t, e)
		for _, offset := range []int{12, 48} {
			bad := bytes.Clone(raw)
			bad[offset] = 1
			if _, e = storageMoveReadIntent(bad); e == nil {
				t.Fatal("reserved header accepted", offset)
			}
		}
		bad := append(bytes.Clone(raw), 0)
		binary.LittleEndian.PutUint32(bad[8:12], uint32(len(bad)))
		sum := sha256.Sum256(bad[64:])
		copy(bad[16:48], sum[:])
		if _, e = storageMoveReadIntent(bad); e == nil {
			t.Fatal("resealed trailing intent accepted")
		}
		bad = bytes.Clone(raw)
		bad[len(bad)-1] = 2
		sum = sha256.Sum256(bad[64:])
		copy(bad[16:48], sum[:])
		if _, e = storageMoveReadIntent(bad); e == nil {
			t.Fatal("unknown retained tag accepted")
		}
		legacy := base
		legacy.Version = 1
		legacy.Storage = nil
		old, e := json.Marshal(legacy)
		storageTestMust(t, e)
		if _, e = storageMoveReadIntent(old); e == nil {
			t.Fatal("legacy JSON relabeled as V2")
		}
		badIntent := base
		badIntent.Previous = "/foreign"
		if _, e = storageMoveEncodeIntent(badIntent); e == nil {
			t.Fatal("request/path disagreement accepted")
		}
	})
	t.Run("unchanged-joint-producer-cap", func(t *testing.T) {
		// Closed DTO maxima, including both native35 tuples. Existing4839
		// JSON plan bounds the combined raw byte-string operands by floor3/4;
		// fixed keys/digests make the true bound tighter. These are monotone
		// size shadows, not fabricated source or successful native witnesses.
		requestMaximum := 318 + 4 + 2*startupIdentityLimit + 1 + storageMoveFixedTailMaximum*140 + 135
		// These mandatory JSON members are outside the Before/After base64
		// strings in every file plan, regardless of member order or path shape.
		fixedJSONBytes := len(`"version":1`) + len(`"kind":"file"`) + 7*64
		fileMaximum := 64 + 2 + requestMaximum + 32 + 2 + 41 + 1 + 224 + 1 + 4 + 3*(dialectArtifactProjectMovePlanBytes-fixedJSONBytes)/4
		if requestMaximum != 2042 || fileMaximum != 5688 || fileMaximum > storageMoveIntentMaximum || storageMoveIntentMaximum != 5898 || dialectArtifactLocationRecordBytes != 5907 {
			t.Fatal("closed joint cap", requestMaximum, fileMaximum)
		}
	})
}

func TestStorageMoveTailContracts(t *testing.T) {
	t.Run("cross-root-settlement", func(t *testing.T) {
		request, j, _ := moveRequestVector(t)
		peer := j.Core.Components[0]
		peer.Root, peer.RootGeneration, peer.Filesystem = storagebudget.ID{81}, 9, storagebudget.ID{82}
		peer.Workspace, peer.WorkspaceGeneration = storagebudget.ID{83}, 4
		peer.Category = storagebudget.History
		j.Core.Components = append(j.Core.Components, peer)
		j.Core.RootIdentities = append(j.Core.RootIdentities, storagebudget.Digest{84})
		j.Core.FilesystemIdentities = append(j.Core.FilesystemIdentities, storagebudget.Digest{85})
		j.Core.Locators = append(j.Core.Locators, "history")
		tail := storageMoveTail{Kind: 2, Root: peer.Root, Generation: peer.RootGeneration, Before: epNative(86, 2), BeforeDigest: storagebudget.Digest{87}, AfterDigest: storagebudget.Digest{88}, BeforeBytes: 19, AfterBytes: 31}
		request.Tails = []storageMoveTail{tail}
		raw, err := storageMoveEncodeTails(request.Tails)
		storageTestMust(t, err)
		j.MoveTails = storageMoveTailJournal{Plan: sha256.Sum256(raw), Count: 1, Allocated: 1, Entered: 1, Done: 1}
		j.MoveTails.Component[0], j.MoveTails.BeforeBytes[0], j.MoveTails.AfterBytes[0], j.MoveTails.BeforeExists[0], j.MoveTails.Staged[0] = 1, 19, 31, true, epNative(89, 2)
		j.Core.Phase, j.Core.Outcome = storageIntentSettling, storageIntentPublished
		if !j.MoveTails.valid(j.Core) || !storageMoveTailRequestBound(request.Tails, j.MoveTails, j.Core.Components) {
			t.Fatal("independent original root summary")
		}
		changes, err := storageIdentitySettlementChanges(j)
		storageTestMust(t, err)
		if changes[0].PhysicalAdd != (storagebudget.Vector{Bytes: j.Core.NewLength, Files: 1}) || changes[0].PhysicalRemove != (storagebudget.Vector{Bytes: j.Core.OldLength, Files: 1}) {
			t.Fatal("identity root changed by history tail")
		}
		if changes[1].PhysicalAdd != (storagebudget.Vector{Bytes: 31, Files: 1}) || changes[1].PhysicalRemove != (storagebudget.Vector{Bytes: 19, Files: 1}) || changes[1].AttributionAdd != changes[1].PhysicalAdd || changes[1].AttributionRemove != changes[1].PhysicalRemove {
			t.Fatal("history root not independently settled", changes[1])
		}
		wrong := append([]storagebudget.Component{}, j.Core.Components...)
		wrong[1].RootGeneration++
		if storageMoveTailRequestBound(request.Tails, j.MoveTails, wrong) {
			t.Fatal("foreign root generation accepted")
		}
		j.Core.Outcome = storageIntentAborted
		j.MoveTails.Allocated, j.MoveTails.Entered, j.MoveTails.Done = 0, 0, 0
		j.MoveTails.Staged[0] = storageOwnerPersistentObject{}
		changes, err = storageIdentitySettlementChanges(j)
		storageTestMust(t, err)
		if !reflect.DeepEqual(changes, make([]storagebudget.Change, 2)) {
			t.Fatal("abort invented physical changes")
		}
	})
	t.Run("unknown-prefix-refusal", func(t *testing.T) {
		_, j, _ := moveRequestVector(t)
		j.MoveTails = storageMoveTailJournal{Plan: storagebudget.Digest{1}, Count: 1}
		j.MoveTails.AfterBytes[0] = 1
		bad := j.MoveTails
		bad.Entered = 1
		if bad.valid(j.Core) {
			t.Fatal("entered without original allocation")
		}
		bad = j.MoveTails
		bad.Done = 1
		if bad.valid(j.Core) {
			t.Fatal("done without original entered effect")
		}
		after := j.MoveTails
		after.Allocated = 1
		after.Staged[0] = epNative(91, 2)
		if !storageMoveTailJournalTransition(j.MoveTails, after) {
			t.Fatal("original allocated observation refused")
		}
		changed := after
		changed.Staged[0] = epNative(92, 2)
		if storageMoveTailJournalTransition(after, changed) || storageMoveTailJournalTransition(after, j.MoveTails) {
			t.Fatal("unknown original native regressed/replaced")
		}
		j.Core.Phase, j.Core.Outcome = storageIntentSettling, storageIntentPublished
		if j.MoveTails.valid(j.Core) {
			t.Fatal("terminal before owed tail completion")
		}
	})
	t.Run("applied-current-and-reuse", func(t *testing.T) {
		row := moveCurrentMaximumVector()
		row.PendingIntent = storagebudget.Digest{}
		receipt := row.Current.Receipt
		status := storagebudget.HostState{Epoch: receipt.RetirementEpoch}
		slot := &status.Slots[receipt.Terminal.Slot]
		slot.PersistedApplied, slot.PersistedNextSequence = receipt.Terminal, receipt.PersistedNextSequence
		if !storageMoveAppliedCurrent(row, storagebudget.ID{1}, storagebudget.Digest{2}, status) {
			t.Fatal("persisted original receipt not covered")
		}
		slot.PersistedApplied.Sequence++
		slot.PersistedApplied.ID = storagebudget.ID{93}
		slot.PersistedNextSequence = slot.PersistedApplied.Sequence + 1
		if !storageMoveAppliedCurrent(row, storagebudget.ID{1}, storagebudget.Digest{2}, status) {
			t.Fatal("later applied slot cannot erase current native publication")
		}
		slot.PersistedApplied.Sequence = receipt.Terminal.Sequence
		if storageMoveAppliedCurrent(row, storagebudget.ID{1}, storagebudget.Digest{2}, status) {
			t.Fatal("same ordinal foreign applied tuple accepted")
		}
	})
}

func TestStorageMoveControlProvision(t *testing.T) {
	for _, name := range []string{"valid-original", "wrong-parent", "oversize", "malformed", "retained-after-settlement"} {
		t.Run(name, func(t *testing.T) {
			owner, config, dir := storageWriterFixture(t, false)
			original := storageCensusConfigParent.provision(false)
			if !storageMoveControlProvisionFits() || original != (storagebudget.Vector{Bytes: 4 << 20, Files: 16, Directories: 4}) {
				t.Fatal("unchanged full provision")
			}
			// The complete covered partition is 2,121,728bytes/8files. Actual
			// catalog/packet/transfer/staging operands remain separately charged.
			if storageIntentBytes+2*storageRecordLimit+(16<<10) != 2121728 {
				t.Fatal("closed control maximum changed")
			}
			raw := []byte(`{"version":1,"previous":"/old","destination":"/new","folder":"1:2","identity":"` + strings.Repeat("a", 64) + `","configRevision":""}`)
			target := config
			if name == "wrong-parent" {
				for _, root := range owner.orderedRoots() {
					if root.qControl {
						target = root
					}
				}
			}
			if name == "oversize" {
				raw = bytes.Repeat([]byte{'x'}, (16<<10)+1)
			}
			if name == "malformed" {
				raw = []byte(`{"version":1}`)
			}
			storageTestMust(t, os.WriteFile(filepath.Join(dir, projectMoveName), raw, 0600))
			info, err := config.root.Lstat(projectMoveName)
			storageTestMust(t, err)
			covered, err := target.censusCovered(t.Context(), projectMoveName, info)
			switch name {
			case "wrong-parent":
				if err != nil || covered {
					t.Fatal("Q borrowed original config provision", err)
				}
			case "oversize", "malformed":
				if err == nil || covered {
					t.Fatal("arbitrary bytes borrowed provision")
				}
			default:
				storageTestMust(t, err)
				if !covered {
					t.Fatal("bounded original intent omitted")
				}
				if name == "retained-after-settlement" {
					// Coverage belongs to the original fixed control, not a live Q token.
					// The intent must remain fully covered when no live reservation exists.
					status, e := owner.ref.cell.q.InspectHostState(t.Context())
					storageTestMust(t, e)
					for _, row := range status.Slots {
						if storageUnretired(row) {
							t.Fatal("fixture has live ticket")
						}
					}
					before, e := owner.measureCensus(t.Context(), owner.orderedRoots())
					storageTestMust(t, e)
					storageTestMust(t, os.Remove(filepath.Join(dir, projectMoveName)))
					after, e := owner.measureCensus(t.Context(), owner.orderedRoots())
					storageTestMust(t, e)
					if !reflect.DeepEqual(before.Roots, after.Roots) || !reflect.DeepEqual(before.Workspaces, after.Workspaces) {
						t.Fatal("removal forgave separately charged data or changed fixed floor")
					}
				}
			}
		})
	}
}

// These are actual original-host journeys, separately gated by the matching
// current-source native proof. Pure codec vectors do not qualify these calls.
func TestStorageMoveOriginalHistoryJourneys(t *testing.T) {
	for _, cut := range []string{"artifact-absent-repeat", "artifact-present-delta", "intent-claimed", "admitted-before-stage"} {
		t.Run(cut, func(t *testing.T) {
			project := t.TempDir()
			identity := strings.Repeat("d", 64)
			storageTestMust(t, os.Mkdir(filepath.Join(project, startupIdentityDir), 0700))
			privateFixture(t, filepath.Join(project, startupIdentityDir, startupIdentityName), identityLine(identity, project, ""))
			s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: standInRuntime(t), Token: testToken, Logger: log.New(&bytes.Buffer{}, "", 0), DeskConfigDir: t.TempDir()})
			t.Cleanup(func() { ts.Close(); s.Close() })
			if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
				t.Fatal("first original chat", code)
			}
			if s.storageReadOnly || s.storageConstruction == nil || s.storageConstruction.owner == nil {
				t.Fatal("actual native source not ready")
			}
			locatorOwner, locatorBefore := "", 0
			if cut == "artifact-present-delta" {
				owner := enrollDialectArtifactMoveServer(t, s)
				locatorOwner = owner.Owner
				locatorBefore = len(dialectArtifactOwnerRead(t, dialectArtifactLocationPath(s, locatorOwner)))
				if _, e := s.artifactRepository().Put(t.Context(), []byte("original move tail bytes")); e != nil {
					t.Fatal(e)
				}
			}
			storageTestMust(t, s.storageConstruction.completeOriginalIdentityFor(t.Context(), s))
			original, err := s.storageConstruction.owner.readOriginalPolicy(t.Context())
			storageTestMust(t, err)
			if len(original.Lineage.Rows.LineagePlans) == 0 {
				t.Fatal("original P absent")
			}
			if original.Native {
				strict, e := storageOwnerV6NativeReadPair(original.Actual[0], original.Actual[1], &original.Catalog, original.Evidence.Raw, *s.storageConstruction.ref.cell.ownerRecord, s.storageConstruction.ref.cell.ownerClaim)
				if e != nil || strict.Selected != original.Selected {
					t.Fatal("original native semantic bank changed", e)
				}
			} else if original.Packed == nil && string(original.Actual[0][:8]) == "DSKONL1\x00" {
				strict, e := storageCompositionReadPair(original.Actual[0], original.Actual[1], *s.storageConstruction.ref.cell.ownerRecord, s.storageConstruction.ref.cell.ownerClaim)
				if e != nil || strict.RecoveryOnly || strict.Selected != original.Selected {
					t.Fatal("original composition semantic bank changed", e)
				}
			}
			// Repetition of an already completed original is a read/currentness
			// join, not a second group, native effect or settlement admission.
			launch := s.storageConstruction.identityLaunch
			if launch == nil {
				t.Fatal("original launch missing")
			}
			groups := append([]storageIdentityLaunchGroup{}, launch.groups...)
			applications := append([]storageIdentityLaunchApplication{}, launch.applications...)
			for i := range applications {
				applications[i].components = append([]storagebudget.Component{}, applications[i].components...)
			}
			peak, debt := launch.peak, launch.debt
			repeatQ, e := s.storageConstruction.ref.cell.q.InspectAction(t.Context())
			storageTestMust(t, e)
			storageTestMust(t, s.storageConstruction.completeOriginalIdentityFor(t.Context(), s))
			afterRepeat, e := s.storageConstruction.owner.readOriginalPolicy(t.Context())
			storageTestMust(t, e)
			afterRepeatQ, e := s.storageConstruction.ref.cell.q.InspectAction(t.Context())
			storageTestMust(t, e)
			if !reflect.DeepEqual(repeatQ, afterRepeatQ) || !bytes.Equal(original.Actual[0], afterRepeat.Actual[0]) || !bytes.Equal(original.Actual[1], afterRepeat.Actual[1]) || !reflect.DeepEqual(original.Lineage, afterRepeat.Lineage) || !reflect.DeepEqual(groups, launch.groups) || !reflect.DeepEqual(applications, launch.applications) || peak != launch.peak || debt != launch.debt {
				t.Fatal("completed repeat changed original effects, budget or provenance")
			}
			beforeQ, err := s.storageConstruction.ref.cell.q.InspectAction(t.Context())
			storageTestMust(t, err)
			destination := filepath.Join(t.TempDir(), "moved")
			preview := movePreview(t, ts, destination)
			injected := errors.New("bounded original move cut")
			if cut == "intent-claimed" || cut == "admitted-before-stage" {
				testProjectMoveStep = func(at string) error {
					if at == cut {
						return injected
					}
					return nil
				}
			}
			t.Cleanup(func() { testProjectMoveStep = nil })
			code, _ := moveRequest(t, ts, "move", preview)
			testProjectMoveStep = nil
			if cut == "intent-claimed" || cut == "admitted-before-stage" {
				if code == 200 {
					t.Fatal("cut acknowledged success")
				}
				raw, err := os.ReadFile(filepath.Join(s.configDir, projectMoveName))
				storageTestMust(t, err)
				captured, err := s.storageConstruction.ref.cell.q.InspectAction(t.Context())
				storageTestMust(t, err)
				if cut == "intent-claimed" && !reflect.DeepEqual(beforeQ, captured) {
					t.Fatal("pre-admission cut reserved or changed Q")
				}
				again, _, _ := startedAt(t, s, project)
				if !again.storageReadOnly || !errors.Is(again.storageProblem, errStorageEnrollmentUnresolved) {
					t.Fatal("unknown prefix became writable", again.storageProblem)
				}
				after, err := again.storageConstruction.ref.cell.q.InspectAction(t.Context())
				storageTestMust(t, err)
				if !reflect.DeepEqual(captured, after) {
					t.Fatal("uncertain restart repeated Q effects")
				}
				kept, err := os.ReadFile(filepath.Join(again.configDir, projectMoveName))
				storageTestMust(t, err)
				if !bytes.Equal(raw, kept) {
					t.Fatal("uncertain restart removed/replaced original intent")
				}
				return
			}
			if code != 200 {
				t.Fatal("artifact-absent move", code)
			}
			if cut == "artifact-present-delta" {
				locatorAfter := len(dialectArtifactOwnerRead(t, dialectArtifactLocationPath(s, locatorOwner)))
				seen := 0
				storageTestMust(t, storageTestCompletedMoveJournal(t.Context(), s.storageConstruction.owner, func(ctx context.Context, table *storageIntentTable) error {
					for _, j := range table.identity {
						if j == nil || j.Action != storageIdentityIntentMove || j.Core.Phase != storageIntentRetired {
							continue
						}
						seen++
						last := int(j.MoveTails.Count) - 1
						if last < 0 || j.MoveTails.BeforeBytes[last] != uint64(locatorBefore) || j.MoveTails.AfterBytes[last] != uint64(locatorAfter) {
							t.Fatal("exact locator lengths missing from original settlement")
						}
						component := j.Core.Components[j.MoveTails.Component[last]]
						root := s.storageConstruction.owner.roots[component.Root]
						if root == nil || !root.configControl || component.Category != storagebudget.Control {
							t.Fatal("locator charged to wrong original root")
						}
						if j.MoveTails.Count < 1 || j.MoveTails.Done != (1<<j.MoveTails.Count)-1 {
							t.Fatal("artifact tail not fully settled")
						}
						changes, e := storageIdentitySettlementChanges(*j)
						if e != nil {
							return e
						}
						if !reflect.DeepEqual(changes, j.Core.Changes) {
							t.Fatal("original locator delta not in same settlement")
						}
					}
					return nil
				}))
				if seen != 1 {
					t.Fatal("own retired move missing", seen)
				}
			}
			again, againTS, _ := startedAt(t, s, destination)
			if again.storageReadOnly {
				t.Fatal("original restart", again.storageProblem)
			}
			if _, err = os.Lstat(filepath.Join(again.configDir, projectMoveName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("intent not acknowledged", err)
			}
			// A second restart needs only the strict latest current row, not removed J
			// or intent bytes, and a subsequent move must replace that one row.
			twice, twiceTS, _ := startedAt(t, again, destination)
			_ = againTS
			if twice.storageReadOnly {
				t.Fatal("intent-removed restart", twice.storageProblem)
			}
			destination2 := filepath.Join(t.TempDir(), "moved-again")
			code, _ = moveRequest(t, twiceTS, "move", movePreview(t, twiceTS, destination2))
			if code != 200 {
				t.Fatal("repeated original move", code)
			}
			final, _, _ := startedAt(t, twice, destination2)
			if final.storageReadOnly {
				t.Fatal("repeated restart", final.storageProblem)
			}
			current, err := final.storageConstruction.owner.readOriginalPolicy(t.Context())
			storageTestMust(t, err)
			if !reflect.DeepEqual(original.Lineage, current.Lineage) || len(current.Moves) != 1 {
				t.Fatal("immutable origin changed or move history appended")
			}
			history, err := final.storageConstruction.originalStorageHistory(t.Context())
			storageTestMust(t, err)
			if history != project {
				t.Fatal("current path replaced admitted original history")
			}
			if cut == "artifact-present-delta" {
				return
			}
			if _, err = os.Lstat(filepath.Join(destination2, ".desk-private", dialectArtifactOwnerMarker)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("artifact owner manufactured", err)
			}
		})
	}
}

// Read-only J inspection after a completed move, when no startup mutation or
// registered move epoch remains for withControlOriginal. As in
// withStartupMoveControl, the strict original decode runs once with the caller
// context; the same try-only controlMu, original bootstrap EX and 250 ms window
// then bound the J decode, and both physical banks must equal the decoded
// images before and after it. Every J receipt/mapping check still runs.
func storageTestCompletedMoveJournal(ctx context.Context, o *storageBudgetOwner, run func(context.Context, *storageIntentTable) error) (err error) {
	if !o.valid() {
		return storagebudget.ErrAuthority
	}
	original, err := o.readOriginalPolicy(ctx)
	if err != nil {
		return err
	}
	if original.RecoveryOnly {
		return errStorageEnrollmentUnresolved
	}
	ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	if !o.controlMu.TryLock() {
		return storagebudget.ErrBusy
	}
	defer o.controlMu.Unlock()
	gate, err := storageOpenOriginalControl(o.ref.cell, storageBootstrapName)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, gate.file.Close()) }()
	if lockPrivateData(gate.file, true) != nil {
		return storagebudget.ErrBusy
	}
	if err = gate.current(); err != nil {
		return err
	}
	// controlOriginalCurrent without its startup/registered exclusion check.
	images := func() error {
		var actual [2][]byte
		for i, name := range []string{"storage-owner.a", "storage-owner.b"} {
			file, err := storageOpenOriginalControl(o.ref.cell, name)
			if err != nil {
				return err
			}
			actual[i] = make([]byte, storageOwnerCatalogBytes)
			n, readErr := file.file.ReadAt(actual[i], 0)
			err = errors.Join(readErr, file.current(), file.file.Close())
			if err != nil || n != len(actual[i]) {
				return errors.Join(errStorageCustody, err)
			}
		}
		if !storageControlOriginalImages(&original, actual) {
			return errStorageCustody
		}
		return ctx.Err()
	}
	if err = images(); err != nil {
		return err
	}
	if err = o.withOriginalIntentTableOriginal(ctx, &original, run); err != nil {
		return err
	}
	return images()
}

// These cuts leave an actual admitted Pending move for the outer New path.
// They separately require abort at the original path, completion at the moved
// inode, and completion with Q already retired but no active current row.
func TestStorageMovePendingRecoveryFinalization(t *testing.T) {
	for _, cut := range []string{"before-rename", "after-rename", "terminal-before-current"} {
		t.Run(cut, func(t *testing.T) {
			project := t.TempDir()
			identity := strings.Repeat("e", 64)
			storageTestMust(t, os.Mkdir(filepath.Join(project, startupIdentityDir), 0700))
			privateFixture(t, filepath.Join(project, startupIdentityDir, startupIdentityName), identityLine(identity, project, ""))
			s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: standInRuntime(t), Token: testToken, Logger: log.New(&bytes.Buffer{}, "", 0), DeskConfigDir: t.TempDir()})
			t.Cleanup(func() { ts.Close(); s.Close() })
			if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
				t.Fatal("first original admission", code)
			}
			storageTestMust(t, s.storageConstruction.completeOriginalIdentityFor(t.Context(), s))
			original, err := s.storageConstruction.owner.readOriginalPolicy(t.Context())
			storageTestMust(t, err)
			identityBefore, err := os.ReadFile(filepath.Join(project, startupIdentityDir, startupIdentityName))
			storageTestMust(t, err)
			projectBefore, err := os.Stat(project)
			storageTestMust(t, err)
			destination := filepath.Join(t.TempDir(), "moved")
			injected := errors.New("pending move finalization cut")
			previousHook := testProjectMoveStep
			reached := 0
			testProjectMoveStep = func(at string) error {
				if at == cut {
					reached++
					return injected
				}
				return nil
			}
			t.Cleanup(func() { testProjectMoveStep = previousHook })
			code, _ := moveRequest(t, ts, "move", movePreview(t, ts, destination))
			testProjectMoveStep = previousHook
			if reached != 1 || cut == "before-rename" && code != 409 || cut != "before-rename" && code != 200 {
				t.Fatal("original cut not reached exactly once", reached, code)
			}
			intent, found, err := readProjectMove(s.assistant.root)
			storageTestMust(t, err)
			if !found || intent.Storage == nil {
				t.Fatal("admitted original intent absent")
			}
			request := *intent.Storage
			readJournal := func(server *Server) storageIdentityIntent {
				t.Helper()
				var out storageIdentityIntent
				count := 0
				storageTestMust(t, server.storageConstruction.owner.withControl(t.Context(), func(_ context.Context, table *storageIntentTable) error {
					for _, j := range table.identity {
						if j != nil && j.Core.OperationID == request.Operation {
							out, count = *j, count+1
						}
					}
					return nil
				}))
				if count != 1 {
					t.Fatal("original action not unique", count)
				}
				return out
			}
			before := readJournal(s)
			beforeQ, err := s.storageConstruction.ref.cell.q.InspectAction(t.Context())
			storageTestMust(t, err)
			if cut == "terminal-before-current" {
				if before.Core.Phase != storageIntentRetired || before.NativePhase != storageIdentityTargetDurable || before.Core.Token.Generation != 3 {
					t.Fatal("cut was not factual terminal-before-current")
				}
			} else if before.Core.Phase != storageIntentStaging || before.NativePhase != storageIdentityPayloadReady || before.Core.Token.Generation != 2 {
				t.Fatal("cut was not the original dispatched move")
			}
			prefix, err := s.storageConstruction.owner.readOriginalPolicy(t.Context())
			storageTestMust(t, err)
			pending := 0
			for _, row := range prefix.Moves {
				if row.PlanID == request.Plan && row.PendingIntent == before.Core.CASBinding {
					pending++
				}
			}
			if pending != 1 {
				t.Fatal("cut lacks exact pending publication")
			}
			next := destination
			if cut == "before-rename" {
				next = project
			}
			again, againTS, _ := startedAt(t, s, next)
			if again.storageReadOnly || again.storageProblem != nil || again.storageConstruction == nil || !again.storageConstruction.owner.admissionOpen() {
				t.Fatal("outer startup did not restore admission", again.storageProblem)
			}
			after := readJournal(again)
			afterQ, err := again.storageConstruction.ref.cell.q.InspectAction(t.Context())
			storageTestMust(t, err)
			terminal := after.Core.Token
			if after.Core.OperationID != request.Operation || terminal.ID != before.Core.Token.ID || terminal.Slot != before.Core.Token.Slot || terminal.Sequence != before.Core.Token.Sequence || terminal.Generation != 3 || after.Core.Phase != storageIntentRetired {
				t.Fatal("recovery replaced or repeated the original action")
			}
			slot := afterQ.View.Slots[terminal.Slot]
			if slot.Ticket.State != storagebudget.Terminal || slot.Ticket.Token != terminal || slot.PersistedApplied != terminal || slot.PersistedNextSequence <= terminal.Sequence {
				t.Fatal("original terminal/applied/next not durably covered")
			}
			if cut == "terminal-before-current" && !reflect.DeepEqual(beforeQ, afterQ) {
				t.Fatal("already retired move repeated Q effects")
			}
			expectedOutcome := storageIntentPublished
			if cut == "before-rename" {
				expectedOutcome = storageIntentAborted
			}
			if after.Core.Outcome != expectedOutcome {
				t.Fatal("wrong recovery outcome")
			}
			current, err := again.storageConstruction.owner.readOriginalPolicy(t.Context())
			storageTestMust(t, err)
			if !reflect.DeepEqual(original.Lineage, current.Lineage) {
				t.Fatal("recovery changed immutable P/C/F")
			}
			if _, err = os.Lstat(filepath.Join(again.configDir, projectMoveName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("successful outer recovery retained intent", err)
			}
			projectAfter, err := os.Stat(next)
			storageTestMust(t, err)
			if !os.SameFile(projectBefore, projectAfter) {
				t.Fatal("recovery replaced the original project")
			}
			identityAfter, err := os.ReadFile(filepath.Join(next, startupIdentityDir, startupIdentityName))
			storageTestMust(t, err)
			if cut == "before-rename" && !bytes.Equal(identityBefore, identityAfter) {
				t.Fatal("abort rewrote the original identity")
			}
			// No receipt or transient flags substitute for a real next admission.
			status, snapshot := chatRequest(t, againTS, "GET", "", "", true, "")
			if status != 200 {
				t.Fatal("recovered history unavailable", status)
			}
			if code, _ := chatRequest(t, againTS, "PUT", snapshot.SHA256, strings.Replace(storageChat, `"one"`, `"after-recovery"`, 1), true, ""); code != 200 {
				t.Fatal("subsequent original admission refused", code)
			}
			twice, _, _ := startedAt(t, again, next)
			if twice.storageReadOnly || !twice.storageConstruction.owner.admissionOpen() {
				t.Fatal("intent-removed second restart not writable", twice.storageProblem)
			}
			still, err := os.ReadFile(filepath.Join(next, startupIdentityDir, startupIdentityName))
			storageTestMust(t, err)
			if !bytes.Equal(identityAfter, still) {
				t.Fatal("second restart repeated identity publication")
			}
		})
	}
}

func TestStorageMoveArtifactSettlement(t *testing.T) {
	for _, name := range []string{"exact-plan-and-root", "wrong-root", "changed-plan", "unknown-native", "finite-group-ids", "maximum-request-and-j5"} {
		t.Run(name, func(t *testing.T) {
			request, j, _ := moveRequestVector(t)
			if name == "maximum-request-and-j5" {
				// Closed codec maxima are not a native authority or a source-valid
				// immutable P. Actual origin admission is tested separately above.
				request.Previous = []byte("/" + strings.Repeat("a", 511))
				request.Destination = []byte("/" + strings.Repeat("b", 511))
				for k := byte(1); k <= 4; k++ {
					request.Tails = append(request.Tails, storageMoveTail{Kind: k, Root: j.Core.Components[0].Root, Generation: 1, Before: epNative(uint64(130+k), 2), BeforeDigest: storagebudget.Digest{k}, AfterDigest: storagebudget.Digest{k, 1}, BeforeBytes: 1, AfterBytes: 1})
				}
				request.Artifact = &storageMoveArtifactRequest{Plan: storagebudget.Digest{5}, Root: j.Core.Components[0].Root, Parent: epNative(140, 1), Before: epNative(141, 2), BeforeBytes: 5907, AfterBytes: 5907}
				raw, e := request.encode()
				storageTestMust(t, e)
				tooLong := request
				tooLong.Previous = append(bytes.Clone(request.Previous), 'x')
				if _, e = tooLong.encode(); e == nil {
					t.Fatal("text limit+1 accepted")
				}
				tooLarge := *request.Artifact
				tooLarge.AfterBytes = 5908
				if _, e = storageMoveEncodeArtifact(&tooLarge); e == nil {
					t.Fatal("locator limit+1 accepted")
				}
				if len(raw) != 2042 {
					t.Fatal("maximum request", len(raw))
				}
				decoded, e := storageMoveReadRequest(raw)
				storageTestMust(t, e)
				if !reflect.DeepEqual(decoded, request) {
					t.Fatal("maximum operands lost")
				}
				j.Core.CASBinding, e = request.digest()
				storageTestMust(t, e)
				j.MoveTails.Count = 5
				j.MoveTails.Plan, e = request.tailCommit()
				storageTestMust(t, e)
				for i := 0; i < 5; i++ {
					j.MoveTails.BeforeExists[i] = true
					j.MoveTails.BeforeBytes[i] = 1
					j.MoveTails.AfterBytes[i] = 1
				}
				j.MoveTails.BeforeBytes[4], j.MoveTails.AfterBytes[4] = 5907, 5907
				page, e := storageEncodeIdentityIntent(j, request.Installation, request.Binding)
				storageTestMust(t, e)
				if storageIdentityMoveEncoded != 16375 || storageIdentityMoveBody != 16247 || len(page) != 16384 {
					t.Fatal("unchanged J extent")
				}
				read, e := storageDecodeIdentityIntent(page, int(j.Core.Slot), request.Installation, request.Binding)
				storageTestMust(t, e)
				if !reflect.DeepEqual(read, j) {
					t.Fatal("five-tail J lost original operands")
				}
				page[16375] = 1
				storageIdentityIntentSeal(page)
				if _, e = storageDecodeIdentityIntent(page, int(j.Core.Slot), request.Installation, request.Binding); e == nil {
					t.Fatal("tail padding acquired authority")
				}
				return
			}
			plan := &dialectArtifactProjectMovePlan{Version: 1, Owner: strings.Repeat("a", 64), Kind: "injected", LegacySHA256: strings.Repeat("b", 64), MarkerSHA256: strings.Repeat("c", 64), Namespace: strings.Repeat("d", 64), NativeScopeSHA256: strings.Repeat("e", 64), WorkDomain: strings.Repeat("f", 64), WorkSHA256: strings.Repeat("1", 64), Provider: strings.Repeat("2", 64), ProviderIdentitySHA256: strings.Repeat("3", 64), ProviderOwnerSHA256: strings.Repeat("4", 64), ProviderBindingSHA256: strings.Repeat("5", 64)}
			digest, err := storageMoveArtifactPlan(plan)
			storageTestMust(t, err)
			request.Artifact = &storageMoveArtifactRequest{Plan: digest}
			if !request.artifactPlanBound(plan) {
				t.Fatal("exact existing provider plan not bound")
			}
			if name == "changed-plan" {
				copy := *plan
				copy.Namespace = strings.Repeat("6", 64)
				if request.artifactPlanBound(&copy) {
					t.Fatal("changed retained plan accepted")
				}
				return
			}
			artifact := &storageMoveArtifactRequest{Plan: digest, Root: j.Core.Components[0].Root, Parent: epNative(111, 1), Before: epNative(112, 2), BeforeBytes: 23, AfterBytes: 37}
			request.Artifact = artifact
			commitment, err := request.tailCommit()
			storageTestMust(t, err)
			j.MoveTails = storageMoveTailJournal{Plan: commitment, Count: 1}
			j.MoveTails.BeforeBytes[0], j.MoveTails.AfterBytes[0], j.MoveTails.BeforeExists[0] = 23, 37, true
			if !request.tailBound(j.MoveTails, j.Core.Components) {
				t.Fatal("original locator summary")
			}
			switch name {
			case "wrong-root":
				wrong := append([]storagebudget.Component{}, j.Core.Components...)
				wrong[0].Root = storagebudget.ID{120}
				if request.tailBound(j.MoveTails, wrong) {
					t.Fatal("locator charged to foreign root")
				}
			case "unknown-native":
				j.MoveTails.Allocated, j.MoveTails.Entered = 1, 1
				j.MoveTails.Staged[0] = epNative(113, 2)
				changed := j.MoveTails
				changed.Staged[0] = epNative(114, 2)
				if storageMoveTailJournalTransition(j.MoveTails, changed) {
					t.Fatal("unknown locator write adopted another inode")
				}
				j.Core.Phase, j.Core.Outcome = storageIntentSettling, storageIntentPublished
				if j.MoveTails.valid(j.Core) {
					t.Fatal("entered unknown locator write settled as success")
				}
			case "finite-group-ids":
				groups, e := storageIdentityFiniteGroups(storageIdentityIntentMove, false, true)
				storageTestMust(t, e)
				used := map[uint16]bool{}
				for _, g := range groups {
					if used[g.ID] {
						t.Fatal("host/Q/native group collision", g.ID)
					}
					used[g.ID] = true
				}
				for n := uint16(1); n <= 29; n++ {
					if !used[storageIdentityHostGroup(storageIdentityIntentMove, n)] {
						t.Fatal("unfunded J prefix", n)
					}
				}
			default:
				j.MoveTails.Allocated, j.MoveTails.Entered, j.MoveTails.Done = 1, 1, 1
				j.MoveTails.Staged[0] = epNative(113, 2)
				j.Core.Phase, j.Core.Outcome = storageIntentSettling, storageIntentPublished
				changes, e := storageIdentitySettlementChanges(j)
				storageTestMust(t, e)
				if changes[0].PhysicalAdd.Bytes != j.Core.NewLength+37 || changes[0].PhysicalRemove.Bytes != j.Core.OldLength+23 || changes[0].PhysicalAdd.Files != 2 || changes[0].PhysicalRemove.Files != 2 {
					t.Fatal("locator delta omitted or charged twice", changes[0])
				}
			}
		})
	}
}

// Pure actual-frame operands through move -> policy commit/abort -> R1
// Snapshot. This tests byte reconstruction, not a native execution claim.
func TestStorageMovePolicyR1Projection(t *testing.T) {
	for _, name := range []string{"bank0-commit", "bank0-abort", "bank1-commit", "bank1-abort"} {
		t.Run(name, func(t *testing.T) {
			selected := byte(0)
			if strings.HasPrefix(name, "bank1") {
				selected = 1
			}
			actual, catalog, f, row := moveNativeCompleteVector(t, selected)
			operation := storagebudget.ID{72}
			pending, bank, err := storageMoveNativePublication(actual, selected, f.raw, catalog, operation, []storageMoveCurrent{row}, f.record, f.claim)
			storageTestMust(t, err)
			actual[bank] = pending
			plan, _, err := row.originOperands(func() storageCompositionLineage {
				v, e := storageOwnerV6NativeDecode(pending, f.record, f.claim)
				storageTestMust(t, e)
				return v.Stage.Lineage
			}())
			storageTestMust(t, err)
			identity, err := parseIdentity(plan.IdentityInitial)
			storageTestMust(t, err)
			identity.Path = "/move-policy-r1"
			receipt := ownerV5Receipt(8, identity.line(), 2)
			receipt.OperationID = operation
			committed := row
			committed.PendingIntent = storagebudget.Digest{}
			committed.Current = &storageMovePublication{RequestDigest: row.PendingIntent, Receipt: receipt, Body: identity.line()}
			active, bank, err := storageMoveNativePublication(actual, bank, f.raw, catalog, operation, []storageMoveCurrent{committed}, f.record, f.claim)
			storageTestMust(t, err)
			actual[bank] = active
			endpoint, err := storageOwnerV6NativeDecode(active, f.record, f.claim)
			storageTestMust(t, err)
			source, err := storageOwnerV6NativePolicySource(actual, bank, f.raw, catalog, f.record, f.claim)
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
			limits := member.Limits
			limits.Total.Bytes--
			changed, err := storageOwnerV6CanonicalSet(defs, rows, member.ID, member.Generation, limits)
			storageTestMust(t, err)
			sparse, err := storageOwnerV6EncodeSparse(changed, defs)
			storageTestMust(t, err)
			after, err := storageOwnerV6Expand(defs, afterPolicy, changed)
			storageTestMust(t, err)
			seed, _, _, _ := policyNativeFixture(t, selected)
			inverse, err := storagebudget.DecodePolicyTransition(mustPolicyNativeOld(t, seed[selected], f))
			storageTestMust(t, err)
			inverse.Original.Revision += 3
			inverse.Original.Epoch++
			inverse.Original.Checkpoint[0]++
			inverse.Before, inverse.After = storageOwnerV6Policy(endpoint.Policy), storageOwnerV6Policy(afterPolicy)
			inverse.Workspace, inverse.WorkspaceGeneration, inverse.BeforeLimits, inverse.AfterLimits = member.ID, member.Generation, member.Limits, limits
			transition, err := storagebudget.EncodePolicyTransition(inverse)
			storageTestMust(t, err)
			prepared := storageOwnerV6PackedCatalog{Version: 6, Encoding: "packed-binary-1", Sequence: endpoint.Stage.Sequence + 1, TransitionKind: storageOwnerV6PolicyTransition, Phase: "prepared", Transition: storagebudget.ID{99}, Source: source, EffectivePolicy: afterPolicy, Overrides: sparse, PolicyTransition: transition}
			copy(prepared.PreviousCatalogDigest[:], active[32:64])
			prepared.BeforeEffectiveDigest, err = storageOwnerV6EffectiveDigest(before)
			storageTestMust(t, err)
			prepared.AfterEffectiveDigest, err = storageOwnerV6EffectiveDigest(after)
			storageTestMust(t, err)
			storageTestMust(t, storageOwnerV6NativePolicyEntry(actual, bank, f.raw, catalog, prepared, f.record, f.claim))
			p, err := storageOwnerV6PackedEncode(prepared, f.raw, f.record, f.claim)
			storageTestMust(t, err)
			final := prepared
			final.Sequence++
			final.Phase = "active"
			if strings.HasSuffix(name, "abort") {
				final.Phase = "aborted"
				final.EffectivePolicy = endpoint.Policy
				final.Overrides = bytes.Clone(endpoint.Overrides)
			}
			copy(final.PreviousCatalogDigest[:], p[32:64])
			a, err := storageOwnerV6PackedEncode(final, f.raw, f.record, f.claim)
			storageTestMust(t, err)
			rotated := [2][]byte{p, p}
			rotated[bank] = a
			capsule, core, lineage, policy, override, err := storageOwnerV6CaptureBefore(rotated, bank, f.raw, catalog, f.record, f.claim)
			storageTestMust(t, err)
			restored, err := capsule.restore(core, lineage, policy, override, catalog, f.record, f.claim)
			storageTestMust(t, err)
			if !bytes.Equal(restored[0], rotated[0]) || !bytes.Equal(restored[1], rotated[1]) {
				t.Fatal("policy rotation changed exact moved operands")
			}
			for _, path := range []byte{2, 3} {
				_, _, _, scope := storageSuccessorScopeVector(t, path)
				scope[322] = bank
				epSet64(scope[1624:1632], binary.LittleEndian.Uint64(core[16:24]))
				copy(scope[88:120], capsule.Full[bank][:])
				copy(scope[40:56], f.record.Installation[:])
				copy(scope[56:88], f.record.QBinding[:])
				epReplan(&scope)
				normalized := scope
				clear(normalized[184:216])
				normalized[321] = 0
				clear(normalized[1080:1220])
				digest := epHash("desk-enrollment-control-peak-v2", normalized[:])
				copy(scope[184:216], digest[:])
				snapshot, _, e := storageOwnerV6NativeSnapshot(rotated, bank, f.raw, catalog, scope, nil, f.record, f.claim)
				storageTestMust(t, e)
				envelope := storageEnrollmentPeakEnvelope{Kind: 0, BeforeCodec: 1, Sequence: binary.LittleEndian.Uint64(scope[1624:1632]) + 1, Scope: scope, Snapshot: &snapshot}
				wire, e := storageEnrollmentPeakEncodeEnvelopeNative(envelope, f.record, f.claim, true, true)
				storageTestMust(t, e)
				decoded, e := storageEnrollmentPeakDecodeEnvelopeNative(wire, f.record, f.claim, true, true, true)
				storageTestMust(t, e)
				if decoded.Snapshot == nil || decoded.Snapshot.Encoding != 6 {
					t.Fatal("R1 dropped actual native source")
				}
			}
		})
	}
}

// Complete pure comparison operands: the existing native P/C vector supplies
// its original graph/lineage, while the independently handwritten Evidence2
// conversion retains the exact physical before pair. This is not live custody.
func moveLegacyR1CompleteVector(t *testing.T, selected byte) ([2][]byte, storageOwnerCatalogV2, storageEnrollmentPeakEvidence, storageMoveCurrent, storageOwnerRecordV2, storageOwnerClaimV2) {
	t.Helper()
	actual, catalog, f, row := moveNativeCompleteVector(t, selected)
	original, err := storageOwnerV6NativeDecode(actual[selected], f.record, f.claim)
	if err != nil {
		t.Fatal("legacy R1 fixture: read-native-P-C", err)
	}
	lineage := original.Stage.Lineage
	e := epDecode(t, f)
	before := ownerAdvisoryWire(f.before)
	physical, _ := compositionTestBasePair(before, 1, f.before.PreviousCatalogDigest)
	if selected == 0 {
		physical[0], physical[1] = physical[1], physical[0]
	}
	projection := compositionTestProjection(physical)
	s := e.Scope
	binary.LittleEndian.PutUint16(s[:2], 2)
	// This is the founding path1 fixture, whose completed bank is the
	// opposite of the original before bank and whose Completed offset is5.
	if s.path() != 1 {
		t.Fatal("legacy R1 fixture changed founding path")
	}
	s[322] = 1 - selected
	epReplan(&s)
	d := sha256.Sum256(before)
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
	// Scope's before bank changes Board's explicit bank tuple and group targets,
	// not only its Scope hash. Rebuild the same handwritten original Board.
	originBytes := epBytes{}
	originBytes.fixed(f.record.Installation)
	originBytes.fixed(f.record.QBinding)
	originBytes.Write(s[24:40])
	originBytes.fixed(f.source.Project)
	originBytes.fixed(e.RegistrationDigest)
	origin := epHash("desk-enrollment-origin-e1", originBytes.Bytes())
	qDigests := [3]storagebudget.Digest{sha256.Sum256(f.cp[1]), sha256.Sum256(f.cp[2]), sha256.Sum256(f.cp[3])}
	e.Sections[8].Raw = epBoard(s, f.source, e.Phase, qDigests, origin)
	cp := e.Checkpoint
	cp.PeerCheckpoint = append([]byte{}, f.cp[0]...)
	e.Sections[6].Raw, err = storageEnrollmentPeakWriteCheckpoint(cp, e.Inner.Catalog, e.Views, s)
	if err != nil {
		t.Fatal("legacy R1 fixture: checkpoint-v2", err)
	}
	e.Sections[4].Raw = append(append([]byte{}, f.sections[4]...), projection[:]...)
	evidence, err := storageEnrollmentPeakDecodeEvidenceWithLineage(compositionTestEvidenceV2Frame(e), &lineage, f.record, f.claim)
	if err != nil {
		t.Fatal("legacy R1 fixture: decode-evidence-v2", err)
	}
	if evidence.Version != 2 {
		t.Fatal("genuine composition evidence lost")
	}
	core := compositionTestMarkerCore(evidence, 3, binary.LittleEndian.Uint64(s[1624:1632])+5, storagebudget.Digest{83})
	block, err := lineage.encode()
	if err != nil {
		t.Fatal("legacy R1 fixture: encode-lineage", err)
	}
	stage := original.Stage
	stage.Sequence, stage.Publication, stage.Transition = 80, storageCompositionActive, storageCompositionEnrollment
	stage.TransitionID, stage.BeforeBank = evidence.Bridge, 1-selected
	stage.FirstBase = storagebudget.Digest{}
	stage.Core, stage.CoreFull = core, storageCompositionCoreDigest(sha256.Sum256(core))
	stage.LineageDigest = sha256.Sum256(block)
	stage.BeforePeer = storageCompositionDuplicateCorePeer(before, f.record, f.claim)
	stage.Raw = nil
	raw, err := stage.encode(f.record, f.claim)
	if err != nil {
		t.Fatal("legacy R1 fixture: encode-legacy-carrier", err)
	}
	if _, err = stage.validateEvidence(evidence.Raw, evidence.Ready, f.record, f.claim); err != nil {
		t.Fatal("legacy R1 evidence join", err)
	}
	pair, err := storageCompositionReadPair(raw, raw, f.record, f.claim)
	if err != nil || pair.Selected != selected || !pair.Duplicated || pair.RecoveryOnly {
		t.Fatal("legacy completed bank relation", err)
	}
	return [2][]byte{raw, raw}, catalog, evidence, row, f.record, f.claim
}

func TestStorageMoveLegacyR1BeginPublication(t *testing.T) {
	for _, name := range []string{"bank0", "bank1"} {
		t.Run(name, func(t *testing.T) {
			selected := byte(0)
			if name == "bank1" {
				selected = 1
			}
			actual, catalog, evidence, row, record, claim := moveLegacyR1CompleteVector(t, selected)
			before := [2]storagebudget.Digest{sha256.Sum256(actual[0]), sha256.Sum256(actual[1])}
			endpoint, err := storageOwnerV6NativeReadEndpoint(actual[selected], evidence.Raw, record, claim)
			storageTestMust(t, err)
			// Native-only evidence validation remains strict. The actual legacy
			// endpoint is joined instead by the original composition reader above.
			if _, err = endpoint.validateEvidence(evidence.Raw, record, claim); !errors.Is(err, storagebudget.ErrBounds) {
				t.Fatal("Version2 relabeled native", err)
			}
			forged := endpoint
			forged.Stage.Raw = nil
			forged.NativeBase = forged.Stage.Sequence - 5
			nativeRaw, err := forged.encode(record, claim)
			storageTestMust(t, err)
			if _, _, err = storageMoveNativePublication([2][]byte{nativeRaw, nativeRaw}, selected, evidence.Raw, catalog, storagebudget.ID{91}, []storageMoveCurrent{row}, record, claim); err == nil {
				t.Fatal("actual native Enrollment accepted Version2 evidence")
			}
			wrongEvidence := append([]byte{}, evidence.Raw...)
			wrongEvidence[len(wrongEvidence)-1] = 1
			if _, _, err = storageMoveNativePublication(actual, selected, wrongEvidence, catalog, storagebudget.ID{91}, []storageMoveCurrent{row}, record, claim); err == nil {
				t.Fatal("malformed original evidence accepted")
			}
			operation := storagebudget.ID{91}
			pending, bank, err := storageMoveNativePublication(actual, selected, evidence.Raw, catalog, operation, []storageMoveCurrent{row}, record, claim)
			storageTestMust(t, err)
			view, err := storageOwnerV6NativeDecode(pending, record, claim)
			storageTestMust(t, err)
			closure, err := storageOwnerV6NativeClosureSize(view)
			storageTestMust(t, err)
			if closure.Future > 135168 {
				t.Fatal("small original fixture omitted future closure")
			}
			if bank != 1-selected {
				t.Fatal("pending selected wrong original bank")
			}
			current := actual
			current[bank] = pending
			if _, e := storageOwnerV6NativeReadPair(current[0], current[1], nil, nil, record, claim); e == nil {
				t.Fatal("mixed actual legacy peer accepted without its original evidence")
			}
			strict, err := storageOwnerV6NativeReadPair(current[0], current[1], &catalog, evidence.Raw, record, claim)
			if err != nil || strict.Selected != bank || !strict.RecoveryOnly {
				t.Fatal("pending relation", err)
			}
			if _, _, err = storageMoveNativePublication(current, 1-bank, evidence.Raw, catalog, operation, []storageMoveCurrent{}, record, claim); err == nil {
				t.Fatal("nonduplicate selected mismatch accepted")
			}
			if _, _, err = storageMoveNativePublication(current, bank, evidence.Raw, catalog, storagebudget.ID{92}, []storageMoveCurrent{}, record, claim); err == nil {
				t.Fatal("foreign operation completed recovery-only pair")
			}
			aborted, abortBank, err := storageMoveNativePublication(current, bank, evidence.Raw, catalog, operation, []storageMoveCurrent{}, record, claim)
			storageTestMust(t, err)
			abortPair := current
			abortPair[abortBank] = aborted
			strict, err = storageOwnerV6NativeReadPair(abortPair[0], abortPair[1], nil, evidence.Raw, record, claim)
			if err != nil || strict.RecoveryOnly || strict.Selected != abortBank || len(strict.Banks[strict.Selected].Stage.Moves) != 0 {
				t.Fatal("abort original inverse", err)
			}
			plan, _, err := row.origin(endpoint.Stage.Lineage)
			storageTestMust(t, err)
			identity, err := parseIdentity(plan.IdentityInitial)
			storageTestMust(t, err)
			identity.Path = "/legacy-r1-moved"
			body := identity.line()
			receipt := ownerV5Receipt(8, body, 2)
			receipt.OperationID = operation
			committed := row
			committed.PendingIntent = storagebudget.Digest{}
			committed.Current = &storageMovePublication{RequestDigest: row.PendingIntent, Receipt: receipt, Body: body}
			active, activeBank, err := storageMoveNativePublication(current, bank, evidence.Raw, catalog, operation, []storageMoveCurrent{committed}, record, claim)
			storageTestMust(t, err)
			current[activeBank] = active
			strict, err = storageOwnerV6NativeReadPair(current[0], current[1], nil, evidence.Raw, record, claim)
			if err != nil || strict.RecoveryOnly || strict.Selected != activeBank || activeBank != selected {
				t.Fatal("committed original relation", err)
			}
			if before != ([2]storagebudget.Digest{sha256.Sum256(actual[0]), sha256.Sum256(actual[1])}) {
				t.Fatal("pure preparation changed originals")
			}
		})
	}
}

// These are pure selected-bootstrap/source comparison vectors. Named root
// pointers stand for independently held witnesses; no native custody is inferred.
func TestStorageMovePrivateSourceCorrespondence(t *testing.T) {
	for _, name := range []string{"committed-intent-removed", "pending-before", "pending-after", "foreign-native", "wrong-original-tuple", "missing-current-witness", "foreign-current-parent", "unregistered-root"} {
		t.Run(name, func(t *testing.T) {
			request, j, lineage := moveRequestVector(t)
			selector := storageMoveCurrent{PlanID: request.Plan, PlanDigest: request.PlanDigest, CompletionDigest: request.CompletionDigest}
			plan, origin, err := selector.originOperands(lineage)
			storageTestMust(t, err)
			old, after, err := request.operands(lineage, nil)
			storageTestMust(t, err)
			digest, err := request.digest()
			storageTestMust(t, err)
			row := selector
			path, native, body := request.Destination, origin.PublishedObject, after
			var req *storageMoveRequest
			var journal *storageIdentityIntent
			if strings.HasPrefix(name, "pending-") {
				row.PendingIntent = digest
				req, journal = &request, &j
				body = old
				if name == "pending-before" {
					path = request.Previous
				}
			} else {
				receipt := ownerV5Receipt(8, after, 2)
				receipt.OperationID = request.Operation
				row.Current = &storageMovePublication{RequestDigest: digest, Receipt: receipt, Body: after}
				native = receipt.PublishedObject
			}
			joined, err := storageMoveBootstrapJoin(lineage, []storageMoveCurrent{row}, plan.ProjectObject, plan.PrivateObject, plan.ConfigObject, native, path, body, req, journal)
			storageTestMust(t, err)
			graph, record, claim := ownerV5Fixture(t, false)
			graph = ownerV5Complete(t, graph, record, claim)
			catalog := graph.old()
			var original storageOwnerRoot
			for _, r := range catalog.Roots {
				if r.Definition.ID == plan.PrivateRoot.ID {
					original = r
				}
			}
			project, root := new(os.Root), new(os.Root)
			source := &storageOriginalSourceRoot{root: root, persistent: plan.PrivateObject, path: filepath.Join(string(path), startupIdentityDir), key: string(original.Source.OwnerKey), role: original.Role, category: original.Category, witnesses: []storageNamedRoot{{parent: project, root: root, name: startupIdentityDir}}}
			before, _ := json.Marshal(catalog)
			want := true
			switch name {
			case "foreign-native":
				source.persistent.Object++
				want = false
			case "wrong-original-tuple":
				joined.Plan.PrivateRoot.SourceDigest[0] ^= 1
				want = false
			case "missing-current-witness":
				source.witnesses = nil
				want = false
			case "foreign-current-parent":
				source.witnesses[0].parent = new(os.Root)
				want = false
			case "unregistered-root":
				original.Definition.ID[0] ^= 1
				want = false
			}
			if got := storageMovePrivateSourceMatches(joined, catalog, original, source, project, string(path)); got != want {
				t.Fatal("original/current source comparison", got, want)
			}
			current, _ := json.Marshal(catalog)
			if !bytes.Equal(before, current) {
				t.Fatal("correspondence rewrote original catalog")
			}
		})
	}
}
