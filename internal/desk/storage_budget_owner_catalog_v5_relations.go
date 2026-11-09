package desk

import (
	"bytes"
	"crypto/sha256"
	"path/filepath"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func (c storageOwnerV5Catalog) validatePlan(p storageOwnerV5LineagePlan, r storageOwnerRecordV2) error {
	if p.Installation != c.Installation || p.QBinding != c.QBinding || !p.ProjectObject.valid(1) || !p.PrivateObject.valid(1) || p.ConfigObject != r.Config || !p.ConfigObject.valid(1) || p.ProviderRelationDigest == (storagebudget.Digest{}) {
		return storagebudget.ErrBounds
	}
	initial, err := parseIdentity(p.IdentityInitial)
	if err != nil {
		return storagebudget.ErrBounds
	}
	if initial.From == "" {
		if len(p.IdentityFromCleared) != 0 || p.ContinuationRoom != 0 || p.StampRoom != 0 {
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
	completion, stamp, continuation, err := storageOwnerV5Rooms(p)
	if err != nil || p.CompletionRoom != completion || p.ContinuationRoom != continuation || p.StampRoom != stamp {
		return storagebudget.ErrBounds
	}
	return nil
}
func (r storageOwnerV5Receipt) valid(kind uint8) bool {
	return r.OperationID != (storagebudget.ID{}) && storageIntentToken(r.Terminal) && r.PersistedApplied == r.Terminal && r.PersistedNextSequence > r.Terminal.Sequence && r.RetirementEpoch > 0 && r.IntentDigest != (storagebudget.Digest{}) && r.TicketDigest != (storagebudget.Digest{}) && r.PublishedObject.valid(kind) && r.PublicationEvidence != (storagebudget.Digest{}) && ((kind == 1 && r.PublishedBytesDigest == (storagebudget.Digest{})) || (kind == 2 && r.PublishedBytesDigest != (storagebudget.Digest{})))
}
func (f storageOwnerV5FromContinuation) valid(p storageOwnerV5LineagePlan) bool {
	z := storageOwnerPersistentObject{}
	if !f.Rewrite.valid(2) || f.Rewrite.PublishedBytesDigest != sha256.Sum256(p.IdentityFromCleared) {
		return false
	}
	switch f.StampDisposition {
	case storageOwnerV5Moved:
		return f.StampSourceBefore.valid(1) && f.StampTargetBefore == z && f.StampSourceAfter == z && f.StampTargetAfter == f.StampSourceBefore && f.StampReceipt.valid(1) && f.StampReceipt.PublishedObject == f.StampTargetAfter && f.StampReceipt.OperationID != f.Rewrite.OperationID
	case storageOwnerV5BothPresent:
		return f.StampSourceBefore.valid(1) && f.StampTargetBefore.valid(1) && f.StampSourceBefore != f.StampTargetBefore && f.StampSourceAfter == f.StampSourceBefore && f.StampTargetAfter == f.StampTargetBefore && f.StampReceipt == (storageOwnerV5Receipt{})
	case storageOwnerV5SourceAbsent:
		return f.StampSourceBefore == z && f.StampSourceAfter == z && (f.StampTargetBefore == z || f.StampTargetBefore.valid(1)) && f.StampTargetAfter == f.StampTargetBefore && f.StampReceipt == (storageOwnerV5Receipt{})
	}
	return false
}
