package desk

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageOwnerV4MaxDigest() (d storagebudget.Digest) {
	for i := range d {
		d[i] = 255
	}
	return
}
func storageOwnerV4MaxID() (d storagebudget.ID) {
	for i := range d {
		d[i] = 255
	}
	return
}
func storageOwnerV4MaxObject(kind uint8) storageOwnerPersistentObject {
	return storageOwnerPersistentObject{Backend: 2, VolumeUUID: [16]byte(storageOwnerV4MaxID()), Object: math.MaxUint64, Incarnation: math.MaxUint64, IncarnationKind: 2, ObjectKind: kind}
}
func storageOwnerV4MaxReceipt(kind uint8) storageOwnerV4Receipt {
	token := storagebudget.Token{Slot: storagebudget.Slots - 1, ID: storageOwnerV4MaxID(), Sequence: math.MaxUint64 - 1, Generation: math.MaxUint64}
	bytesDigest := storageOwnerV4MaxDigest()
	if kind == 1 {
		bytesDigest = storagebudget.Digest{}
	}
	return storageOwnerV4Receipt{storageOwnerV4MaxID(), token, token, math.MaxUint64, math.MaxUint64, storageOwnerV4MaxDigest(), storageOwnerV4MaxDigest(), storageOwnerV4MaxObject(kind), bytesDigest, storageOwnerV4MaxDigest()}
}
func storageOwnerV4CompletionShadow(p storageOwnerV4LineagePlan) storageOwnerV4LineageCompletion {
	return storageOwnerV4LineageCompletion{p.PlanID, storageOwnerV4MaxDigest(), storageOwnerV4MaxReceipt(2)}
}
func storageOwnerV4ContinuationShadow(p storageOwnerV4LineagePlan) storageOwnerV4FromContinuation {
	// A moved outcome has two maximum-width directory tuples and a full stamp
	// receipt. Both-present has four tuples but no stamp receipt; choose by actual
	// canonical encoded size, avoiding a guessed constant for either branch.
	source, target := storageOwnerV4MaxObject(1), storageOwnerV4MaxObject(1)
	target.Object--
	moved := storageOwnerV4FromContinuation{PlanID: p.PlanID, PlanDigest: storageOwnerV4MaxDigest(), StampDisposition: storageOwnerV4Moved, StampSourceBefore: source, StampTargetAfter: source, StampReceipt: storageOwnerV4MaxReceipt(1), Rewrite: storageOwnerV4MaxReceipt(2)}
	kept := storageOwnerV4FromContinuation{PlanID: p.PlanID, PlanDigest: storageOwnerV4MaxDigest(), StampDisposition: storageOwnerV4BothPresent, StampSourceBefore: source, StampTargetBefore: target, StampSourceAfter: source, StampTargetAfter: target, Rewrite: storageOwnerV4MaxReceipt(2)}
	a, _ := json.Marshal(moved)
	b, _ := json.Marshal(kept)
	if len(b) > len(a) {
		return kept
	}
	return moved
}

// Rooms are computed before D3 and do not depend on actual future digest bytes.
// Shadows are sizing operands only; they are never committed or factual proof.
func storageOwnerV4Rooms(p storageOwnerV4LineagePlan) (uint32, uint32, error) {
	c, err := json.Marshal(storageOwnerV4CompletionShadow(p))
	if err != nil || len(c) > storageOwnerV4CompletionBytes {
		return 0, 0, storagebudget.ErrBounds
	}
	fsize := 0
	if len(p.IdentityFromCleared) > 0 {
		f, err := json.Marshal(storageOwnerV4ContinuationShadow(p))
		if err != nil || len(f) > storageOwnerV4ContinuationBytes {
			return 0, 0, storagebudget.ErrBounds
		}
		fsize = len(f)
	}
	return uint32(len(c)), uint32(fsize), nil
}

// ClosureSize reserves all missing C/F rows at once, with the full retained old
// projection and the widest future envelope. An unrelated transition cannot
// spend pledged room. Existing factual rows are retained exactly in this copy.
func storageOwnerV4ClosureSize(c storageOwnerV4Catalog) (int, error) {
	if !c.old().bounded() || !storageOwnerV4RowBounds(c) {
		return 0, storagebudget.ErrBounds
	}
	shadow := c
	shadow.LineageCompletions = append([]storageOwnerV4LineageCompletion{}, c.LineageCompletions...)
	shadow.LineageContinuations = append([]storageOwnerV4FromContinuation{}, c.LineageContinuations...)
	completed, continued := map[storagebudget.ID]bool{}, map[storagebudget.ID]bool{}
	for _, row := range c.LineageCompletions {
		completed[row.PlanID] = true
	}
	for _, row := range c.LineageContinuations {
		continued[row.PlanID] = true
	}
	for _, p := range c.LineagePlans {
		cr, fr, err := storageOwnerV4Rooms(p)
		if err != nil || p.CompletionRoom != cr || p.ContinuationRoom != fr {
			return 0, storagebudget.ErrBounds
		}
		if !completed[p.PlanID] {
			shadow.LineageCompletions = append(shadow.LineageCompletions, storageOwnerV4CompletionShadow(p))
		}
		if len(p.IdentityFromCleared) > 0 && !continued[p.PlanID] {
			shadow.LineageContinuations = append(shadow.LineageContinuations, storageOwnerV4ContinuationShadow(p))
		}
	}
	sort.Slice(shadow.LineageCompletions, func(i, j int) bool {
		return bytes.Compare(shadow.LineageCompletions[i].PlanID[:], shadow.LineageCompletions[j].PlanID[:]) < 0
	})
	sort.Slice(shadow.LineageContinuations, func(i, j int) bool {
		return bytes.Compare(shadow.LineageContinuations[i].PlanID[:], shadow.LineageContinuations[j].PlanID[:]) < 0
	})
	// Future receipt publication can follow another enrollment. Its Before IDs
	// then name every currently retained definition, including rows not in the
	// current enrollment's older Before projection.
	shadow.BeforeDefinitionIDs = storageOwnerDefinitionIDs{[]storagebudget.ID{}, []storagebudget.ID{}, []storagebudget.ID{}, []storagebudget.ID{}}
	for _, row := range c.Roots {
		shadow.BeforeDefinitionIDs.Roots = append(shadow.BeforeDefinitionIDs.Roots, row.Definition.ID)
	}
	for _, row := range c.Workspaces {
		shadow.BeforeDefinitionIDs.Workspaces = append(shadow.BeforeDefinitionIDs.Workspaces, row.Definition.ID)
	}
	for _, row := range c.Filesystems {
		shadow.BeforeDefinitionIDs.Filesystems = append(shadow.BeforeDefinitionIDs.Filesystems, row.Definition.ID)
	}
	for _, row := range c.Constraints {
		shadow.BeforeDefinitionIDs.Constraints = append(shadow.BeforeDefinitionIDs.Constraints, row.Definition.ID)
	}
	shadow.Sequence = math.MaxUint64
	shadow.Phase = "prepared"
	shadow.Transition = storageOwnerV4MaxID()
	shadow.TransitionPlan = storageOwnerV4MaxID()
	shadow.TransitionKind = storageOwnerV4BridgePlan
	shadow.PreviousCatalogDigest = storageOwnerV4MaxDigest()
	shadow.BeforeDefinitionsDigest = storageOwnerV4MaxDigest()
	shadow.AfterDefinitionsDigest = storageOwnerV4MaxDigest()
	shadow.ExpectedQRevision = math.MaxUint64
	if !storageOwnerV4RowBounds(shadow) {
		return 0, storagebudget.ErrBounds
	}
	b, err := json.Marshal(shadow)
	if err != nil || len(b) > storageOwnerCatalogBytes-64 {
		return 0, storagebudget.ErrBounds
	}
	// Exercise the exact scanner on the complete maximum tree too. It preserves
	// all existing source/list bounds; no size estimate bypasses preallocation.
	if !storageOwnerV4Preflight(b) {
		return 0, storagebudget.ErrBounds
	}
	return len(b), nil
}
