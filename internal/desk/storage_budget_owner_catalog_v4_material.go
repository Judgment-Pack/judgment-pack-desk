package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// D1 and D4 declaration order is the reviewed digest contract. These DTOs
// retain complete original rows, never a selected subset of definition hashes.
type storageLineageProviderRelationMaterialV1 struct {
	Version         uint32
	Present         bool
	Installation    storagebudget.ID
	QBinding        storagebudget.Digest
	Workspace       storagebudget.ID
	WorkspaceDigest storagebudget.Digest
	Definition      storagebudget.RegisteredRoot
	SourceDigest    storagebudget.Digest
	References      []storagebudget.ID
	Directory       storageOwnerObjectMaterial
	Marker          storageOwnerObjectMaterial
}
type storageLineageUpgradeOldProjectionV1 struct {
	Installation  storagebudget.ID
	QBinding      storagebudget.Digest
	Policy        storageOwnerPolicy
	OriginalOwner storageOwnerSource
	Objects       []storageOwnerObject
	OwnerControls []storageOwnerControl
	Roots         []storageOwnerRoot
	Workspaces    []storageOwnerWorkspace
	Filesystems   []storageOwnerFilesystem
	Constraints   []storageOwnerConstraint
	Profiles      []storageOwnerProfile
}
type storageLineageTicketMaterialV1 struct {
	Installation storagebudget.ID
	QBinding     storagebudget.Digest
	Ticket       storagebudget.Ticket
}

func storageOwnerV4OldProjection(c storageOwnerCatalogV2) storageLineageUpgradeOldProjectionV1 {
	return storageLineageUpgradeOldProjectionV1{c.Installation, c.QBinding, c.Policy, c.OriginalOwner, c.Objects, c.OwnerControls, c.Roots, c.Workspaces, c.Filesystems, c.Constraints, c.Profiles}
}
func storageOwnerV4OldDigest(c storageOwnerCatalogV2) (storagebudget.Digest, error) {
	if !c.bounded() {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	b, err := json.Marshal(c)
	if err != nil || !storageOwnerCatalogPreflight(b) {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	return storageOwnerDigest("desk-storage-lineage-upgrade-old-projection-v1", storageOwnerV4OldProjection(c))
}
func storageOwnerV4WorkspaceDigest(w storageOwnerWorkspace) (storagebudget.Digest, error) {
	if w.Definition.ID == (storagebudget.ID{}) || w.Definition.Generation == 0 || !storageOwnerLimitsValid(w.Definition.Limits) || w.OwnerKind < 1 || w.OwnerKind > 3 || len(w.OwnerKey) == 0 || len(w.OwnerKey) > 256 || len(w.HistoryIdentity) == 0 || len(w.HistoryIdentity) > 4096 {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	return storageOwnerDigest("desk-storage-lineage-workspace-v1", w)
}

// selected=nil expresses the closed absent comparison operand. It does not
// prove positive producer absence. Both present and absent require independent
// original selection/custody validation at integration; no bool grants it here.
func storageOwnerV4ProviderDigest(c storageOwnerCatalogV2, w storageOwnerWorkspace, selected *storageOwnerRoot) (storagebudget.Digest, error) {
	if !c.bounded() || c.Installation == (storagebudget.ID{}) || c.QBinding == (storagebudget.Digest{}) {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	wd, err := storageOwnerV4WorkspaceDigest(w)
	if err != nil {
		return storagebudget.Digest{}, err
	}
	m := storageLineageProviderRelationMaterialV1{Version: 1, Installation: c.Installation, QBinding: c.QBinding, Workspace: w.Definition.ID, WorkspaceDigest: wd, References: []storagebudget.ID{}, Directory: storageOwnerEmptyObjectMaterial(), Marker: storageOwnerEmptyObjectMaterial()}
	if selected != nil {
		b, err := json.Marshal(c)
		if err != nil || !storageOwnerCatalogPreflight(b) {
			return storagebudget.Digest{}, storagebudget.ErrBounds
		}
		p := *selected
		found := false
		for _, row := range c.Roots {
			if reflect.DeepEqual(row, p) {
				found = true
				break
			}
		}
		if !found || p.Role != 5 || len(p.References) == 0 || len(p.References) > 1024 || p.Source.ControlKind != 1 || int(p.DirectoryObject) >= len(c.Objects) || int(p.Source.ControlIndex) >= len(c.Objects) {
			return storagebudget.Digest{}, storagebudget.ErrBounds
		}
		linked := false
		for i, id := range p.References {
			if i > 0 && bytes.Compare(p.References[i-1][:], id[:]) >= 0 {
				return storagebudget.Digest{}, storagebudget.ErrBounds
			}
			linked = linked || id == w.Definition.ID
		}
		marker := c.Objects[p.Source.ControlIndex]
		if !linked || marker.Role != 24 || marker.Parent != p.DirectoryObject || string(marker.Name) != "d5-provider.json" {
			return storagebudget.Digest{}, storagebudget.ErrBounds
		}
		for _, idx := range []uint32{p.DomainObject, p.WorkObject} {
			if idx != storageOwnerNoObject && int(idx) >= len(c.Objects) {
				return storagebudget.Digest{}, storagebudget.ErrBounds
			}
		}
		for _, idx := range p.Membership.ExistingObjects {
			if int(idx) >= len(c.Objects) {
				return storagebudget.Digest{}, storagebudget.ErrBounds
			}
		}
		for _, o := range c.Objects {
			if o.Parent != storageOwnerNoObject && int(o.Parent) >= len(c.Objects) {
				return storagebudget.Digest{}, storagebudget.ErrBounds
			}
		}
		identity, err := c.rootIdentity(p)
		if err != nil || identity != p.Definition.Identity {
			return storagebudget.Digest{}, storagebudget.ErrBounds
		}
		source, err := c.sourceDigest(p)
		if err != nil {
			return storagebudget.Digest{}, err
		}
		m.Present = true
		m.Definition = p.Definition
		m.SourceDigest = source
		m.References = p.References
		m.Directory = c.objectMaterial(p.DirectoryObject)
		m.Marker = c.objectMaterial(p.Source.ControlIndex)
	}
	return storageOwnerDigest("desk-storage-lineage-provider-relation-v1", m)
}
func storageOwnerV4PlanDigest(p storageOwnerV4LineagePlan) (storagebudget.Digest, error) {
	if !storageOwnerV4PlanBounded(p) {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	return storageOwnerDigest("desk-storage-lineage-plan-v1", p)
}
func storageOwnerV4Operation(d storagebudget.Digest, action string) (string, error) {
	if d == (storagebudget.Digest{}) || (action != "create" && action != "stamp" && action != "clear") {
		return "", storagebudget.ErrBounds
	}
	return "p:" + hex.EncodeToString(d[:]) + ":" + action, nil
}
func storageOwnerV4IdentityDigest(raw []byte) (storagebudget.Digest, error) {
	if len(raw) == 0 || len(raw) > 512 {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	if _, err := parseIdentity(raw); err != nil {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	return sha256.Sum256(raw), nil
}

// D5 requires complete original v3 decoding before extracting its checksum.
func storageOwnerV4Origin(raw []byte, r storageOwnerRecordV2, cl storageOwnerClaimV2) (storageOwnerV4UpgradeOrigin, error) {
	c, err := storageOwnerDecodeCatalogV3(raw, r, cl)
	if err != nil {
		return storageOwnerV4UpgradeOrigin{}, err
	}
	old := storageOwnerCatalogV2(c)
	if old.Phase != "active" {
		return storageOwnerV4UpgradeOrigin{}, storagebudget.ErrBounds
	}
	d, err := storageOwnerV4OldDigest(old)
	if err != nil {
		return storageOwnerV4UpgradeOrigin{}, err
	}
	out := storageOwnerV4UpgradeOrigin{Version: 3, Sequence: old.Sequence, OldProjectionDigest: d}
	copy(out.FrameDigest[:], raw[32:64])
	return out, nil
}

// D6 hashes actual supplied bytes, not a reconstructed page. The result is a
// detached commitment; selected-original-page custody and native facts remain
// mandatory elsewhere, and a fabricated value never establishes those facts.
func storageOwnerV4IntentDigest(raw []byte, slot int, installation storagebudget.ID, binding storagebudget.Digest, expected storageIntentRecord) (storagebudget.Digest, error) {
	got, err := storageDecodeIntent(raw, slot, installation, binding)
	if err != nil || got.Phase != storageIntentRetired || got.Outcome != storageIntentPublished || !reflect.DeepEqual(got, expected) {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	return sha256.Sum256(raw), nil
}

// D7 takes the complete original Terminal ticket. Bounds/mapping validation
// belongs to its original inspector; this helper also refuses missing/oversize
// metadata before Marshal. It neither captures a factual receipt nor opens Q.
func storageOwnerV4TicketDigest(installation storagebudget.ID, binding storagebudget.Digest, t storagebudget.Ticket) (storagebudget.Digest, error) {
	if installation == (storagebudget.ID{}) || binding == (storagebudget.Digest{}) || t.Owner != binding || t.State != storagebudget.Terminal || !storageIntentToken(t.Token) || t.PolicyRevision == 0 || !storageIntentText(t.Workflow, 128, false) || !storageIntentText(t.Operation, 128, false) || len(t.Components) == 0 || len(t.Components) > storagebudget.MaxComponents || len(t.Changes) != len(t.Components) || t.Evidence == (storagebudget.Digest{}) {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	if t.Manifest == (storagebudget.Digest{}) && t.ManifestLength != 0 || t.Manifest != (storagebudget.Digest{}) && t.ManifestLength != storagebudget.ManifestBytes {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	for i, component := range t.Components {
		if component.Root == (storagebudget.ID{}) || component.RootGeneration == 0 || component.Filesystem == (storagebudget.ID{}) || component.FilesystemGeneration == 0 || component.Owner == (storagebudget.Digest{}) || component.Category >= storagebudget.Categories || t.Fanout == nil && component.Workspace == (storagebudget.ID{}) && (component.Attribution != (storagebudget.Vector{}) || component.SettlementAttribution != (storagebudget.Vector{})) || component.Workspace != (storagebudget.ID{}) && component.WorkspaceGeneration == 0 || component.CapacityCount == 0 || component.CapacityBinding == (storagebudget.Digest{}) || component.CapacityCount > storagebudget.MaxCapacityRefs {
			return storagebudget.Digest{}, storagebudget.ErrBounds
		}
		for _, v := range []storagebudget.Vector{component.Physical, component.Attribution, component.SettlementPhysical, component.SettlementAttribution, t.Changes[i].PhysicalAdd, t.Changes[i].PhysicalRemove, t.Changes[i].AttributionAdd, t.Changes[i].AttributionRemove} {
			if !storageIntentVector(v) {
				return storagebudget.Digest{}, storagebudget.ErrBounds
			}
		}
		physical := storagebudget.Vector{Bytes: component.Physical.Bytes + component.SettlementPhysical.Bytes, Files: component.Physical.Files + component.SettlementPhysical.Files, Directories: component.Physical.Directories + component.SettlementPhysical.Directories}
		attribution := storagebudget.Vector{Bytes: component.Attribution.Bytes + component.SettlementAttribution.Bytes, Files: component.Attribution.Files + component.SettlementAttribution.Files, Directories: component.Attribution.Directories + component.SettlementAttribution.Directories}
		if !storageIntentVector(physical) || !storageIntentVector(attribution) || physical == (storagebudget.Vector{}) && attribution == (storagebudget.Vector{}) {
			return storagebudget.Digest{}, storagebudget.ErrBounds
		}
		for j, ref := range component.Capacity {
			if j >= int(component.CapacityCount) {
				if ref != (storagebudget.CapacityPromise{}) {
					return storagebudget.Digest{}, storagebudget.ErrBounds
				}
				continue
			}
			if ref.Constraint == (storagebudget.ID{}) || ref.Generation == 0 || ref.Peak.Bytes > 1<<60 || ref.Peak.Inodes > 1<<40 || physical == (storagebudget.Vector{}) && ref.Peak != (storagebudget.CapacityVector{}) || j > 0 && bytes.Compare(component.Capacity[j-1].Constraint[:], ref.Constraint[:]) >= 0 {
				return storagebudget.Digest{}, storagebudget.ErrBounds
			}
		}
		for j := 0; j < i; j++ {
			prior := t.Components[j]
			if t.Fanout == nil && prior.Root == component.Root && prior.Workspace == component.Workspace && prior.Category == component.Category {
				return storagebudget.Digest{}, storagebudget.ErrBounds
			}
		}
		change := t.Changes[i]
		if !storageOwnerV4VectorLE(change.PhysicalAdd, physical) || !storageOwnerV4VectorLE(change.AttributionAdd, attribution) {
			return storagebudget.Digest{}, storagebudget.ErrBounds
		}
	}
	qt := storageEnrollmentPeakQTicketValue(t)
	if storageEnrollmentPeakQValidateTicketPage(qt) != nil {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	if t.Fanout != nil && (len(t.Fanout.Workspaces) != 0 || len(t.Fanout.Memberships) != 0) && storageEnrollmentPeakQValidateFanout(qt) != nil {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	return storageOwnerDigest("desk-storage-lineage-ticket-v1", storageLineageTicketMaterialV1{installation, binding, t})
}

func storageOwnerV4VectorLE(a, b storagebudget.Vector) bool {
	return a.Bytes <= b.Bytes && a.Files <= b.Files && a.Directories <= b.Directories
}
