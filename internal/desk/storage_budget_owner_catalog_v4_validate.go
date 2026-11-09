package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"math"
	"path/filepath"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageOwnerV4PlanBounded(p storageOwnerV4LineagePlan) bool {
	if len(p.HistoryIdentity) == 0 || len(p.HistoryIdentity) > 4096 || len(p.AccountingOwnerKey) == 0 || len(p.AccountingOwnerKey) > 256 || len(p.IdentityInitial) == 0 || len(p.IdentityInitial) > 512 || p.IdentityFromCleared == nil || len(p.IdentityFromCleared) > 512 {
		return false
	}
	b, err := json.Marshal(p)
	return err == nil && len(b) <= storageOwnerV4PlanBytes
}
func storageOwnerV4RowBounds(c storageOwnerV4Catalog) bool {
	if len(c.LineagePlans) > 1024 || len(c.LineageCompletions) > 1024 || len(c.LineageContinuations) > 1024 || c.LineagePlans == nil || c.LineageCompletions == nil || c.LineageContinuations == nil {
		return false
	}
	for _, p := range c.LineagePlans {
		if !storageOwnerV4PlanBounded(p) {
			return false
		}
	}
	// Count complete canonical arrays, including array punctuation.
	for _, group := range []struct {
		rows             any
		rowMax, totalMax int
	}{{c.LineagePlans, 4096, 2 << 20}, {c.LineageCompletions, 2048, 1 << 20}, {c.LineageContinuations, 8192, 2 << 20}} {
		rows := reflect.ValueOf(group.rows)
		for i := 0; i < rows.Len(); i++ {
			b, err := json.Marshal(rows.Index(i).Interface())
			if err != nil || len(b) > group.rowMax {
				return false
			}
		}
		b, err := json.Marshal(group.rows)
		if err != nil || len(b) > group.totalMax {
			return false
		}
	}
	return true
}
func (c storageOwnerV4Catalog) validate(r storageOwnerRecordV2, cl storageOwnerClaimV2) error {
	if c.Version != 4 || c.TransitionKind < 1 || c.TransitionKind > 5 || !c.old().bounded() || !storageOwnerV4RowBounds(c) {
		return storagebudget.ErrBounds
	}
	// Reuse the exact v3 semantics of every unchanged original row. The copy's
	// version is 3 solely for detached validation; no old decoder accepts v4.
	if err := c.old().validateVersion(r, cl, 3); err != nil {
		return err
	}
	if c.UpgradeOrigin != (storageOwnerV4UpgradeOrigin{}) {
		o := c.UpgradeOrigin
		if o.Version != 3 || o.Sequence == 0 || o.FrameDigest == (storagebudget.Digest{}) || o.OldProjectionDigest == (storagebudget.Digest{}) || c.Sequence <= o.Sequence {
			return storagebudget.ErrBounds
		}
	} else if c.TransitionKind == storageOwnerV4BridgePlan {
		return storagebudget.ErrBounds
	}
	if c.TransitionKind == storageOwnerV4Enrollment {
		if c.TransitionPlan != (storagebudget.ID{}) {
			return storagebudget.ErrBounds
		}
	} else if c.TransitionPlan == (storagebudget.ID{}) {
		return storagebudget.ErrBounds
	}
	if c.TransitionKind == storageOwnerV4BridgePlan {
		offset := uint64(1)
		if c.Phase == "active" {
			offset = 2
		}
		if c.UpgradeOrigin.Sequence > math.MaxUint64-offset || c.Sequence != c.UpgradeOrigin.Sequence+offset || len(c.LineagePlans) != 1 || len(c.LineageCompletions) != 0 || len(c.LineageContinuations) != 0 {
			return storagebudget.ErrBounds
		}
		if c.Phase == "prepared" && c.PreviousCatalogDigest != c.UpgradeOrigin.FrameDigest {
			return storagebudget.ErrBounds
		}
		d, err := storageOwnerV4OldDigest(c.old())
		if err != nil || d != c.UpgradeOrigin.OldProjectionDigest {
			return storagebudget.ErrBounds
		}
	}
	plans := make(map[storagebudget.ID]storageOwnerV4LineagePlan, len(c.LineagePlans))
	for i, p := range c.LineagePlans {
		if p.PlanID == (storagebudget.ID{}) || i > 0 && bytes.Compare(c.LineagePlans[i-1].PlanID[:], p.PlanID[:]) >= 0 || c.validatePlan(p, r) != nil {
			return storagebudget.ErrBounds
		}
		plans[p.PlanID] = p
	}
	operations := map[storagebudget.ID]bool{}
	completed := make(map[storagebudget.ID]bool, len(c.LineageCompletions))
	for i, row := range c.LineageCompletions {
		p, ok := plans[row.PlanID]
		d, err := storageOwnerV4PlanDigest(p)
		if !ok || err != nil || row.PlanDigest != d || i > 0 && bytes.Compare(c.LineageCompletions[i-1].PlanID[:], row.PlanID[:]) >= 0 || !row.Initial.valid(2) || row.Initial.PublishedBytesDigest != sha256.Sum256(p.IdentityInitial) {
			return storagebudget.ErrBounds
		}
		b, _ := json.Marshal(row)
		if len(b) > int(p.CompletionRoom) {
			return storagebudget.ErrBounds
		}
		if operations[row.Initial.OperationID] {
			return storagebudget.ErrBounds
		}
		operations[row.Initial.OperationID] = true
		completed[row.PlanID] = true
	}
	continued := make(map[storagebudget.ID]bool, len(c.LineageContinuations))
	for i, row := range c.LineageContinuations {
		p, ok := plans[row.PlanID]
		d, err := storageOwnerV4PlanDigest(p)
		if !ok || err != nil || !completed[row.PlanID] || len(p.IdentityFromCleared) == 0 || row.PlanDigest != d || i > 0 && bytes.Compare(c.LineageContinuations[i-1].PlanID[:], row.PlanID[:]) >= 0 || !row.valid(p) {
			return storagebudget.ErrBounds
		}
		b, _ := json.Marshal(row)
		if len(b) > int(p.ContinuationRoom) {
			return storagebudget.ErrBounds
		}
		if operations[row.Rewrite.OperationID] {
			return storagebudget.ErrBounds
		}
		operations[row.Rewrite.OperationID] = true
		if row.StampReceipt != (storageOwnerV4Receipt{}) {
			if operations[row.StampReceipt.OperationID] {
				return storagebudget.ErrBounds
			}
			operations[row.StampReceipt.OperationID] = true
		}
		continued[row.PlanID] = true
	}
	if c.TransitionKind != storageOwnerV4Enrollment {
		p, ok := plans[c.TransitionPlan]
		if !ok {
			return storagebudget.ErrBounds
		}
		switch c.TransitionKind {
		case storageOwnerV4Plan:
			if completed[p.PlanID] || continued[p.PlanID] {
				return storagebudget.ErrBounds
			}
		case storageOwnerV4Completion:
			if !completed[p.PlanID] || continued[p.PlanID] {
				return storagebudget.ErrBounds
			}
		case storageOwnerV4Continuation:
			if !continued[p.PlanID] {
				return storagebudget.ErrBounds
			}
		}
	}
	_, err := storageOwnerV4ClosureSize(c)
	return err
}

func (c storageOwnerV4Catalog) validatePlan(p storageOwnerV4LineagePlan, r storageOwnerRecordV2) error {
	if p.Installation != c.Installation || p.QBinding != c.QBinding || !p.ProjectObject.valid(1) || !p.PrivateObject.valid(1) || p.ConfigObject != r.Config || !p.ConfigObject.valid(1) || p.ProviderRelationDigest == (storagebudget.Digest{}) {
		return storagebudget.ErrBounds
	}
	initial, err := parseIdentity(p.IdentityInitial)
	if err != nil {
		return storagebudget.ErrBounds
	}
	if initial.From == "" {
		if len(p.IdentityFromCleared) != 0 || p.ContinuationRoom != 0 {
			return storagebudget.ErrBounds
		}
	} else {
		cleared := initial
		cleared.From = ""
		if !bytes.Equal(cleared.line(), p.IdentityFromCleared) {
			return storagebudget.ErrBounds
		}
	}
	old := c.old()
	var w storageOwnerWorkspace
	found := false
	for _, row := range c.Workspaces {
		if row.Definition.ID == p.Workspace.ID {
			w = row
			found = true
			break
		}
	}
	wd, err := storageOwnerV4WorkspaceDigest(w)
	if !found || err != nil || c.OriginalOwner.Kind != 1 || w.OwnerKind != 3 || !storageOwnerHex(string(w.OwnerKey), 64) || w.Definition.ID != storageDerivedID("desk-storage-workspace-v2", c.Installation, w.OwnerKey) || p.Workspace.Generation != w.Definition.Generation || p.Workspace.Digest != wd || !bytes.Equal(w.OwnerKey, p.AccountingOwnerKey) || !bytes.Equal(w.HistoryIdentity, p.HistoryIdentity) {
		return storagebudget.ErrBounds
	}
	found = false
	for _, row := range c.Roots {
		if row.Definition.ID != p.PrivateRoot.ID {
			continue
		}
		// Only the original standalone .desk-private Control boundary qualifies.
		// Managed Role3, retained descendants and unrelated Role6 scopes retain
		// their original meaning and cannot supply this lineage relation.
		if row.Role != 6 || row.Category != storagebudget.Control || row.Source.Kind != 6 || !bytes.Equal(row.Source.OwnerKey, w.OwnerKey) || !bytes.Equal(row.Source.Path, []byte(filepath.Join(initial.Path, ".desk-private"))) || row.Source.ParentRoot != (storagebudget.ID{}) || len(row.Source.Relative) != 0 || row.Source.ControlKind != 0 || row.Source.ControlIndex != storageOwnerNoControl || row.Source.ControlSelection != 0 || row.Definition.Workspace != w.Definition.ID || len(row.References) != 1 || row.References[0] != w.Definition.ID || row.Definition.Generation != p.PrivateRoot.Generation || row.Definition.Identity != p.PrivateRoot.Identity || old.Objects[row.DirectoryObject].Persistent != p.PrivateObject {
			return storagebudget.ErrBounds
		}
		linked := false
		for _, id := range row.References {
			linked = linked || id == p.Workspace.ID
		}
		if !linked {
			return storagebudget.ErrBounds
		}
		sd, err := old.sourceDigest(row)
		if err != nil || p.PrivateRoot.SourceDigest != sd {
			return storagebudget.ErrBounds
		}
		found = true
		break
	}
	if !found {
		return storagebudget.ErrBounds
	}
	// Catalog consistency compares the selected-provider relation, without
	// claiming that serialized absence proves positive original producer absence.
	var selected *storageOwnerRoot
	for i := range c.Roots {
		row := &c.Roots[i]
		if row.Role == 5 {
			for _, id := range row.References {
				if id == w.Definition.ID {
					if selected != nil {
						return storagebudget.ErrBounds
					}
					selected = row
				}
			}
		}
	}
	pd, err := storageOwnerV4ProviderDigest(old, w, selected)
	if err != nil || pd != p.ProviderRelationDigest {
		return storagebudget.ErrBounds
	}
	completion, continuation, err := storageOwnerV4Rooms(p)
	if err != nil || p.CompletionRoom != completion || p.ContinuationRoom != continuation {
		return storagebudget.ErrBounds
	}
	return nil
}
func (r storageOwnerV4Receipt) valid(kind uint8) bool {
	return r.OperationID != (storagebudget.ID{}) && storageIntentToken(r.Terminal) && r.PersistedApplied == r.Terminal && r.PersistedNextSequence > r.Terminal.Sequence && r.RetirementEpoch > 0 && r.IntentDigest != (storagebudget.Digest{}) && r.TicketDigest != (storagebudget.Digest{}) && r.PublishedObject.valid(kind) && r.PublicationEvidence != (storagebudget.Digest{}) && ((kind == 1 && r.PublishedBytesDigest == (storagebudget.Digest{})) || (kind == 2 && r.PublishedBytesDigest != (storagebudget.Digest{})))
}
func (f storageOwnerV4FromContinuation) valid(p storageOwnerV4LineagePlan) bool {
	z := storageOwnerPersistentObject{}
	if !f.Rewrite.valid(2) || f.Rewrite.PublishedBytesDigest != sha256.Sum256(p.IdentityFromCleared) {
		return false
	}
	switch f.StampDisposition {
	case storageOwnerV4Moved:
		return f.StampSourceBefore.valid(1) && f.StampTargetBefore == z && f.StampSourceAfter == z && f.StampTargetAfter == f.StampSourceBefore && f.StampReceipt.valid(1) && f.StampReceipt.PublishedObject == f.StampTargetAfter && f.StampReceipt.OperationID != f.Rewrite.OperationID
	case storageOwnerV4BothPresent:
		return f.StampSourceBefore.valid(1) && f.StampTargetBefore.valid(1) && f.StampSourceBefore != f.StampTargetBefore && f.StampSourceAfter == f.StampSourceBefore && f.StampTargetAfter == f.StampTargetBefore && f.StampReceipt == (storageOwnerV4Receipt{})
	case storageOwnerV4SourceAbsent:
		return f.StampSourceBefore == z && f.StampSourceAfter == z && (f.StampTargetBefore == z || f.StampTargetBefore.valid(1)) && f.StampTargetAfter == f.StampTargetBefore && f.StampReceipt == (storageOwnerV4Receipt{})
	}
	return false
}
