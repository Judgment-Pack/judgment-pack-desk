package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageOwnerV5PlanBounded(p storageOwnerV5LineagePlan) bool {
	if len(p.HistoryIdentity) == 0 || len(p.HistoryIdentity) > 4096 || len(p.AccountingOwnerKey) == 0 || len(p.AccountingOwnerKey) > 256 || len(p.IdentityInitial) == 0 || len(p.IdentityInitial) > 512 || p.IdentityFromCleared == nil || len(p.IdentityFromCleared) > 512 {
		return false
	}
	b, err := json.Marshal(p)
	return err == nil && len(b) <= storageOwnerV5PlanBytes
}
func storageOwnerV5RowBounds(c storageOwnerV5Catalog) bool {
	if len(c.LineagePlans) > 1024 || len(c.LineageCompletions) > 1024 || len(c.LineageContinuations) > 1024 || len(c.LineageStampCompletions) > 1024 || c.LineagePlans == nil || c.LineageCompletions == nil || c.LineageContinuations == nil || c.LineageStampCompletions == nil {
		return false
	}
	for _, p := range c.LineagePlans {
		if !storageOwnerV5PlanBounded(p) {
			return false
		}
	}
	for _, g := range []struct {
		rows             any
		rowMax, totalMax int
	}{
		{c.LineagePlans, 4096, 2 << 20}, {c.LineageCompletions, 2048, 1 << 20}, {c.LineageContinuations, 8192, 2 << 20}, {c.LineageStampCompletions, 8192, 2 << 20},
	} {
		rows := reflect.ValueOf(g.rows)
		for i := 0; i < rows.Len(); i++ {
			b, e := json.Marshal(rows.Index(i).Interface())
			if e != nil || len(b) > g.rowMax {
				return false
			}
		}
		b, e := json.Marshal(g.rows)
		if e != nil || len(b) > g.totalMax {
			return false
		}
	}
	return true
}
func storageOwnerV5PlanDigest(p storageOwnerV5LineagePlan) (storagebudget.Digest, error) {
	if !storageOwnerV5PlanBounded(p) {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	return storageOwnerDigest("desk-storage-lineage-plan-v2", p)
}
func storageOwnerV5StampDigest(s storageOwnerV5StampCompletionRow) (storagebudget.Digest, error) {
	b, e := json.Marshal(s)
	if e != nil || len(b) > storageOwnerV5StampBytes || s.PlanID == (storagebudget.ID{}) || s.PlanDigest == (storagebudget.Digest{}) || !s.valid() {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	return storageOwnerDigest("desk-storage-lineage-stamp-completion-v1", s)
}
func (s storageOwnerV5StampCompletionRow) valid() bool {
	// The stamp predicate is exactly the existing three supported observations.
	// S contains no rewrite receipt. This checks a serialized relation only.
	z := storageOwnerPersistentObject{}
	switch s.StampDisposition {
	case storageOwnerV5Moved:
		return s.StampSourceBefore.valid(1) && s.StampTargetBefore == z && s.StampSourceAfter == z && s.StampTargetAfter == s.StampSourceBefore && s.StampReceipt.valid(1) && s.StampReceipt.PublishedObject == s.StampTargetAfter
	case storageOwnerV5BothPresent:
		return s.StampSourceBefore.valid(1) && s.StampTargetBefore.valid(1) && s.StampSourceBefore != s.StampTargetBefore && s.StampSourceAfter == s.StampSourceBefore && s.StampTargetAfter == s.StampTargetBefore && s.StampReceipt == (storageOwnerV5Receipt{})
	case storageOwnerV5SourceAbsent:
		return s.StampSourceBefore == z && s.StampSourceAfter == z && (s.StampTargetBefore == z || s.StampTargetBefore.valid(1)) && s.StampTargetAfter == s.StampTargetBefore && s.StampReceipt == (storageOwnerV5Receipt{})
	}
	return false
}
func (f storageOwnerV5FromContinuation) stamp() storageOwnerV5StampCompletionRow {
	return storageOwnerV5StampCompletionRow{f.PlanID, f.PlanDigest, f.StampDisposition, f.StampSourceBefore, f.StampTargetBefore, f.StampSourceAfter, f.StampTargetAfter, f.StampReceipt}
}

// RowChain checks all actual rows and their individual pledges, independently
// of the pending-shadow size. An actual oversized row cannot hide behind a
// small total. It establishes consistency only, never original-object custody.
func storageOwnerV5RowChain(c storageOwnerV5Catalog) bool {
	if !storageOwnerV5RowBounds(c) {
		return false
	}
	plans := map[storagebudget.ID]storageOwnerV5LineagePlan{}
	for i, p := range c.LineagePlans {
		cr, sr, fr, e := storageOwnerV5Rooms(p)
		if p.PlanID == (storagebudget.ID{}) || i > 0 && bytes.Compare(c.LineagePlans[i-1].PlanID[:], p.PlanID[:]) >= 0 || e != nil || p.CompletionRoom != cr || p.StampRoom != sr || p.ContinuationRoom != fr {
			return false
		}
		plans[p.PlanID] = p
	}
	operations := map[storagebudget.ID]bool{}
	tokens := map[storagebudget.Token]bool{}
	take := func(r storageOwnerV5Receipt) bool {
		if operations[r.OperationID] || tokens[r.Terminal] {
			return false
		}
		operations[r.OperationID] = true
		tokens[r.Terminal] = true
		return true
	}
	completed := map[storagebudget.ID]bool{}
	stamped := map[storagebudget.ID]storageOwnerV5StampCompletionRow{}
	for i, row := range c.LineageCompletions {
		p, ok := plans[row.PlanID]
		d, e := storageOwnerV5PlanDigest(p)
		b, _ := json.Marshal(row)
		if !ok || e != nil || row.PlanDigest != d || i > 0 && bytes.Compare(c.LineageCompletions[i-1].PlanID[:], row.PlanID[:]) >= 0 || !row.Initial.valid(2) || row.Initial.PublishedBytesDigest != sha256.Sum256(p.IdentityInitial) || len(b) > int(p.CompletionRoom) || !take(row.Initial) {
			return false
		}
		completed[row.PlanID] = true
	}
	for i, row := range c.LineageStampCompletions {
		p, ok := plans[row.PlanID]
		d, e := storageOwnerV5PlanDigest(p)
		b, _ := json.Marshal(row)
		if !ok || e != nil || !completed[row.PlanID] || len(p.IdentityFromCleared) == 0 || row.PlanDigest != d || i > 0 && bytes.Compare(c.LineageStampCompletions[i-1].PlanID[:], row.PlanID[:]) >= 0 || !row.valid() || len(b) > int(p.StampRoom) {
			return false
		}
		if row.StampReceipt != (storageOwnerV5Receipt{}) && !take(row.StampReceipt) {
			return false
		}
		stamped[row.PlanID] = row
	}
	for i, row := range c.LineageContinuations {
		p, ok := plans[row.PlanID]
		d, e := storageOwnerV5PlanDigest(p)
		s, has := stamped[row.PlanID]
		b, _ := json.Marshal(row)
		if !ok || e != nil || !has || !completed[row.PlanID] || len(p.IdentityFromCleared) == 0 || row.PlanDigest != d || i > 0 && bytes.Compare(c.LineageContinuations[i-1].PlanID[:], row.PlanID[:]) >= 0 || row.stamp() != s || !row.valid(p) || len(b) > int(p.ContinuationRoom) || !take(row.Rewrite) {
			return false
		}
		// StampReceipt is intentionally repeated from retained S, not consumed again.
	}
	return true
}
func (c storageOwnerV5Catalog) validate(r storageOwnerRecordV2, cl storageOwnerClaimV2) error {
	if c.Version != 5 || c.TransitionKind < 1 || c.TransitionKind > 6 || c.TransitionKind == 5 || c.UpgradeOrigin != (storageOwnerV5UpgradeOrigin{}) || !c.old().bounded() || !storageOwnerV5RowChain(c) {
		return storagebudget.ErrBounds
	}
	if e := c.old().validateVersion(r, cl, 3); e != nil {
		return e
	}
	for _, p := range c.LineagePlans {
		if e := c.validatePlan(p, r); e != nil {
			return e
		}
	}
	if c.TransitionKind == storageOwnerV5Enrollment {
		if c.TransitionPlan != (storagebudget.ID{}) {
			return storagebudget.ErrBounds
		}
	} else {
		found, completed, stamped, continued := false, false, false, false
		for _, p := range c.LineagePlans {
			found = found || p.PlanID == c.TransitionPlan
		}
		for _, r := range c.LineageCompletions {
			completed = completed || r.PlanID == c.TransitionPlan
		}
		for _, r := range c.LineageStampCompletions {
			stamped = stamped || r.PlanID == c.TransitionPlan
		}
		for _, r := range c.LineageContinuations {
			continued = continued || r.PlanID == c.TransitionPlan
		}
		if c.TransitionPlan == (storagebudget.ID{}) || !found {
			return storagebudget.ErrBounds
		}
		switch c.TransitionKind {
		case storageOwnerV5Plan:
			if completed || stamped || continued {
				return storagebudget.ErrBounds
			}
		case storageOwnerV5Completion:
			if !completed || stamped || continued {
				return storagebudget.ErrBounds
			}
		case storageOwnerV5StampCompletion:
			if !stamped || continued {
				return storagebudget.ErrBounds
			}
		case storageOwnerV5Continuation:
			if !continued {
				return storagebudget.ErrBounds
			}
		}
	}
	_, e := storageOwnerV5ClosureSize(c)
	return e
}
