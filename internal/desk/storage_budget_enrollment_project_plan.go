package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Sorting a candidate dictionary changes indices only. Existing semantic
// material/identity and source-control relations are preserved exactly.
func storageEnrollmentSortObjects(c *storageOwnerCatalogV2) error {
	old := append([]storageOwnerObject{}, c.Objects...)
	sort.Slice(c.Objects, func(i, j int) bool {
		return storageOwnerPOCompare(c.Objects[i].Persistent, c.Objects[j].Persistent) < 0
	})
	indices := map[storageOwnerPersistentObject]uint32{}
	for i, row := range c.Objects {
		if _, ok := indices[row.Persistent]; ok {
			return storagebudget.ErrBounds
		}
		indices[row.Persistent] = uint32(i)
	}
	translate := func(index uint32) (uint32, error) {
		if index == storageOwnerNoObject {
			return index, nil
		}
		if int(index) >= len(old) {
			return 0, storagebudget.ErrBounds
		}
		next, ok := indices[old[index].Persistent]
		if !ok {
			return 0, storagebudget.ErrBounds
		}
		return next, nil
	}
	for i := range c.Objects {
		var err error
		c.Objects[i].Parent, err = translate(c.Objects[i].Parent)
		if err != nil {
			return err
		}
	}
	for i := range c.OwnerControls {
		var err error
		c.OwnerControls[i].ParentObject, err = translate(c.OwnerControls[i].ParentObject)
		if err != nil {
			return err
		}
	}
	for i := range c.Roots {
		r := &c.Roots[i]
		for _, index := range []*uint32{&r.DirectoryObject, &r.DomainObject, &r.WorkObject} {
			next, err := translate(*index)
			if err != nil {
				return err
			}
			*index = next
		}
		for j, index := range r.Membership.ExistingObjects {
			next, err := translate(index)
			if err != nil {
				return err
			}
			r.Membership.ExistingObjects[j] = next
		}
		sort.Slice(r.Membership.ExistingObjects, func(i, j int) bool { return r.Membership.ExistingObjects[i] < r.Membership.ExistingObjects[j] })
		if r.Source.ControlKind == 1 {
			next, err := translate(r.Source.ControlIndex)
			if err != nil {
				return err
			}
			r.Source.ControlIndex = next
		}
	}
	return nil
}

func storageEnrollmentHomeSourceDigest(b storageEnrollmentPeakSource, c storageOwnerCatalogV2) (storagebudget.Digest, error) {
	m := storageEnrollmentPeakHomeSourceMaterial{Installation: b.Installation, QBinding: b.QBinding, Project: b.Project, OwnerKey: b.OwnerKey, HistoryIdentity: b.HistoryIdentity, Kind: c.OriginalOwner.Kind, ConfigPath: c.OriginalOwner.ConfigPath, HomeOwner: c.OriginalOwner.HomeOwner, HomePath: c.OriginalOwner.HomePath, HomeObject: c.OriginalOwner.HomeObject, Registry: storageEnrollmentPeakRegistryMaterial{Name: []byte{}, OwnerKey: []byte{}, Location: storageOwnerLocation{Path: []byte{}, Previous: []byte{}}}, Entry: storageOwnerHomeRelation{Folder: []byte{}, HistoryIdentity: []byte{}}}
	found := false
	for _, w := range c.Workspaces {
		if w.Definition.ID == b.Workspace && w.Definition.Generation == b.WorkspaceGeneration {
			m.Workspace = w
			found = true
		}
	}
	if !found {
		return storagebudget.Digest{}, storagebudget.ErrAuthority
	}
	if m.Kind == 2 {
		if int(c.OriginalOwner.RegistryControl) >= len(c.OwnerControls) {
			return storagebudget.Digest{}, storagebudget.ErrBounds
		}
		v := c.OwnerControls[c.OriginalOwner.RegistryControl]
		m.Registry = storageEnrollmentPeakRegistryMaterial{Role: v.Role, Parent: c.Objects[v.ParentObject].Persistent, Name: v.Name, OwnerKind: v.OwnerKind, OwnerKey: v.OwnerKey, Location: v.Location, RelationDigest: v.RelationDigest, MaxBytes: v.MaxBytes}
		matches := 0
		for _, entry := range c.OriginalOwner.Entries {
			if entry.RunnerWorkspace == string(m.Workspace.OwnerKey) && bytes.Equal(entry.HistoryIdentity, m.Workspace.HistoryIdentity) {
				m.Entry = entry
				matches++
			}
		}
		if matches != 1 {
			return storagebudget.Digest{}, storagebudget.ErrAuthority
		}
	} else if m.Kind != 1 {
		return storagebudget.Digest{}, storagebudget.ErrAuthority
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return storagebudget.Digest{}, err
	}
	return storageEnrollmentPeakHash("desk-project-private-home-source-peak-v1", raw), nil
}

// Opaque Q IDs are derived from the retained random bridge and actual project
// native, so a captured pre-staging restart reconstructs identical catalog
// bytes/extents without persisting another registry. These IDs grant no custody.
func storageEnrollmentProjectDefinitionID(kind string, installation, bridge storagebudget.ID, project storageOwnerPersistentObject) storagebudget.ID {
	raw := append(append(append([]byte{}, installation[:]...), bridge[:]...), storageEnrollmentPeakNativeBytes(project)...)
	digest := storageEnrollmentPeakHash("desk-enrollment-project-definition-"+kind+"-v1", raw)
	var id storagebudget.ID
	copy(id[:], digest[:16])
	return id
}

func (p *storageEnrollmentEpoch) planOriginalProjectScope(ctx context.Context) (storageOwnerCatalogV2, storageEnrollmentPeakSource, error) {
	return p.planOriginalProjectScopeSource(ctx, nil)
}

// A recovery source is immutable captured comparison input. It selects only
// the exact original workspace/project relation, never a newly discovered key.
func (p *storageEnrollmentEpoch) planOriginalProjectScopeSource(ctx context.Context, retained *storageEnrollmentPeakSource) (storageOwnerCatalogV2, storageEnrollmentPeakSource, error) {
	var catalog storageOwnerCatalogV2
	var source storageEnrollmentPeakSource
	if p == nil || p.self != p || p.original == nil || p.initialInspection == nil || p.qualified == nil || p.scope != nil && (!p.startup || p.scope.path() != 1 && retained == nil || p.scope[321] != 15) {
		return catalog, source, storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return catalog, source, err
	}
	if p.previousEvidence != nil && p.qualified.record.Version < 2 {
		return catalog, source, errStorageEnrollmentQualification
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	cores, bank, err := p.originalCorePair(p.original.Catalogs, record, claim)
	if err != nil {
		return catalog, source, err
	}
	var before storageOwnerCatalogV2
	if p.previousEvidence != nil {
		outer, _, e := storageEnrollmentPeakDecodePairVariant(cores[0], cores[1], record, claim, string(p.original.Catalogs[0][:8]) == "DSKONL1\x00" || storagePolicyActualPair(p.original.Catalogs))
		if e != nil || outer.Marker == nil || outer.Marker.Phase != 3 || !bytes.Equal(cores[0], cores[1]) {
			return catalog, source, storagebudget.ErrBounds
		}
		before = p.previousEvidence.Inner.Catalog
	} else {
		pair, e := storageOwnerDecodeCatalogPairV3(cores[0], cores[1], record, claim)
		if e != nil {
			return catalog, source, e
		}
		before = pair.High
	}
	raw, err := json.Marshal(before)
	if err != nil {
		return catalog, source, err
	}
	if err = storageEnrollmentPeakCatalogJSON(raw, &catalog); err != nil {
		return catalog, source, err
	}
	if catalog.Sequence > ^uint64(0)-5 || p.initialInspection.View.Configuration.Revision > ^uint64(0)-3 {
		return catalog, source, storagebudget.ErrBounds
	}
	catalog.Version = 1
	catalog.Sequence++
	catalog.Phase = "prepared"
	catalog.ExpectedQRevision = p.initialInspection.View.Configuration.Revision + 1
	if p.startup && p.scope != nil {
		copy(catalog.Transition[:], p.scope[24:40])
	} else {
		catalog.Transition, err = storagebudget.NewID()
		if err != nil {
			return catalog, source, err
		}
	}
	copy(catalog.PreviousCatalogDigest[:], cores[bank][32:64])
	catalog.BeforeDefinitionIDs = storageOwnerDefinitionIDs{Roots: []storagebudget.ID{}, Workspaces: []storagebudget.ID{}, Filesystems: []storagebudget.ID{}, Constraints: []storagebudget.ID{}}
	for _, row := range before.Roots {
		catalog.BeforeDefinitionIDs.Roots = append(catalog.BeforeDefinitionIDs.Roots, row.Definition.ID)
	}
	for _, row := range before.Workspaces {
		catalog.BeforeDefinitionIDs.Workspaces = append(catalog.BeforeDefinitionIDs.Workspaces, row.Definition.ID)
	}
	for _, row := range before.Filesystems {
		catalog.BeforeDefinitionIDs.Filesystems = append(catalog.BeforeDefinitionIDs.Filesystems, row.Definition.ID)
	}
	for _, row := range before.Constraints {
		catalog.BeforeDefinitionIDs.Constraints = append(catalog.BeforeDefinitionIDs.Constraints, row.Definition.ID)
	}
	var workspace *storageOwnerWorkspace
	key, history := "", ""
	if retained != nil {
		if retained.Default != nil || retained.ActionPageDigest != (storagebudget.Digest{}) || retained.Project != p.original.Project || !storageEnrollmentPeakHomeSourceMatches(*retained, before) {
			return catalog, source, errStorageCustody
		}
		key, history = string(retained.OwnerKey), string(retained.HistoryIdentity)
	} else {
		key, err = p.originalAccountingKey(ctx)
		if err != nil {
			return catalog, source, err
		}
		history, err = p.originalHistoryIdentity(ctx)
		if err != nil {
			return catalog, source, err
		}
		if entry := p.originalHomeEntry(); entry != nil {
			key = entry.RunnerWorkspace
		}
	}
	for i := range catalog.Workspaces {
		row := &catalog.Workspaces[i]
		if string(row.OwnerKey) == key && string(row.HistoryIdentity) == history && (retained == nil || row.Definition.ID == retained.Workspace && row.Definition.Generation == retained.WorkspaceGeneration) {
			if workspace != nil {
				return catalog, source, storagebudget.ErrAuthority
			}
			workspace = row
		}
	}
	if workspace == nil {
		return catalog, source, errStorageCustody
	}
	profile, err := storageHostInstalledProfile(p.original.Project.Backend)
	if err != nil {
		return catalog, source, err
	}
	installed := false
	for _, old := range catalog.Profiles {
		if old == profile {
			installed = true
		}
	}
	if !installed {
		return catalog, source, storagebudget.ErrUnsupported
	}
	var fs storageOwnerFilesystem
	found := false
	for _, old := range catalog.Filesystems {
		if old.Backend == p.original.Project.Backend && old.VolumeUUID == p.original.Project.VolumeUUID {
			if found {
				return catalog, source, storagebudget.ErrBounds
			}
			fs = old
			found = true
		}
	}
	advisory := storageOwnerAdvisoryConstraint(record.Installation).Definition
	if !found {
		id := storageEnrollmentProjectDefinitionID("filesystem", record.Installation, catalog.Transition, p.original.Project)
		identity, err := storageOwnerDigest("desk-storage-original-volume-v3", p.original.Project.VolumeUUID)
		if err != nil {
			return catalog, source, err
		}
		definition := storagebudget.Filesystem{ID: id, Identity: identity, Generation: 1, InodeModel: storagebudget.InodesFinite, CapabilityEvidence: profile.InodeEvidence, ByteConstraint: advisory.ID, InodeConstraint: advisory.ID}
		if profile.Backend == 2 {
			definition.InodeModel = storagebudget.InodesDynamic
			definition.InodeConstraint = storagebudget.ID{}
		}
		fs = storageOwnerFilesystem{Definition: definition, Backend: profile.Backend, VolumeUUID: p.original.Project.VolumeUUID, PersistentProfile: profile.ID, InodeProfile: profile.ID}
		catalog.Filesystems = append(catalog.Filesystems, fs)
	}
	objectIndex := uint32(len(catalog.Objects))
	for _, old := range catalog.Objects {
		if old.Persistent == p.original.Project {
			return catalog, source, storagebudget.ErrAuthority
		}
	}
	catalog.Objects = append(catalog.Objects, storageOwnerObject{Role: 20, Source: 3, Parent: storageOwnerNoObject, Name: []byte{}, Persistent: p.original.Project, OwnerClass: 2, ModeClass: 1})
	rootID := storageEnrollmentProjectDefinitionID("root", record.Installation, catalog.Transition, p.original.Project)
	root := storageOwnerRoot{Definition: storagebudget.RegisteredRoot{ID: rootID, Owner: record.QBinding, Filesystem: fs.Definition.ID, Workspace: workspace.Definition.ID, Generation: 1, Locator: "project-private-parent:" + storageHexID(rootID), CapacityCount: 1, CapacityRefs: [4]storagebudget.CapacityRef{{Constraint: advisory.ID, Generation: advisory.Generation}}}, Role: 7, Category: storagebudget.Control, References: []storagebudget.ID{workspace.Definition.ID}, Source: storageOwnerRootSource{Kind: 7, OwnerKey: append([]byte{}, workspace.OwnerKey...), Path: []byte{}, Relative: []byte{}, ControlIndex: storageOwnerNoControl}, DirectoryObject: objectIndex, DomainObject: storageOwnerNoObject, WorkObject: storageOwnerNoObject, Membership: storageOwnerMembership{Backend: profile.Backend, ProfileID: profile.ID, ProfileVersion: profile.Version, ProfileEvidence: profile.Evidence, Pool: fs.Definition.ID, VolumeUUID: p.original.Project.VolumeUUID, Creation: 1, ExistingObjects: []uint32{}}}
	if profile.Backend == 1 {
		root.Membership.User = storageOwnerSubject{State: 4, Kind: 1}
		root.Membership.Group = storageOwnerSubject{State: 4, Kind: 2}
		root.Membership.Project = storageOwnerSubject{State: 4, Kind: 3}
	}
	source = storageEnrollmentPeakSource{Installation: record.Installation, QBinding: record.QBinding, Project: p.original.Project, Workspace: workspace.Definition.ID, WorkspaceGeneration: workspace.Definition.Generation, OwnerKey: append([]byte{}, workspace.OwnerKey...), HistoryIdentity: append([]byte{}, workspace.HistoryIdentity...), Filesystem: fs.Definition.ID, FilesystemIdentity: fs.Definition.Identity, FilesystemGeneration: fs.Definition.Generation, CapacityCount: root.Definition.CapacityCount, CapacityRefs: root.Definition.CapacityRefs, Namespace: []byte(".desk-private"), Generation: 1}
	source.HomeSource, err = storageEnrollmentHomeSourceDigest(source, catalog)
	if err != nil {
		return catalog, source, err
	}
	source.CapacityBinding, err = storageEnrollmentPeakSourceMapping(source, catalog, root)
	if err != nil {
		return catalog, source, err
	}
	root.Source.Relative, err = storageEnrollmentPeakEncodeSource(source)
	if err != nil {
		return catalog, source, err
	}
	root.Definition.Identity = storageEnrollmentPeakHash("desk-project-private-parent-scope-e1", root.Source.Relative)
	constraints := []storagebudget.CapacityConstraint{}
	for _, row := range catalog.Constraints {
		constraints = append(constraints, row.Definition)
	}
	root.Definition.CapacityBinding, err = storagebudget.CapacityBindingFor(storageOwnerRootValue(root.Definition), fs.Definition, constraints)
	if err != nil {
		return catalog, source, err
	}
	catalog.Roots = append(catalog.Roots, root)
	if err = storageEnrollmentSortObjects(&catalog); err != nil {
		return catalog, source, err
	}
	sort.Slice(catalog.Roots, func(i, j int) bool {
		return bytes.Compare(catalog.Roots[i].Definition.ID[:], catalog.Roots[j].Definition.ID[:]) < 0
	})
	sort.Slice(catalog.Filesystems, func(i, j int) bool {
		return bytes.Compare(catalog.Filesystems[i].Definition.ID[:], catalog.Filesystems[j].Definition.ID[:]) < 0
	})
	a, b, err := catalog.definitions()
	if err != nil {
		return catalog, source, err
	}
	catalog.BeforeDefinitionsDigest, err = storageOwnerDigest("desk-storage-owner-definitions-v2", a)
	if err != nil {
		return catalog, source, err
	}
	catalog.AfterDefinitionsDigest, err = storageOwnerDigest("desk-storage-owner-definitions-v2", b)
	if err != nil {
		return catalog, source, err
	}
	if err = storageEnrollmentPeakValidateCatalog(catalog, record, claim); err != nil {
		return catalog, source, err
	}
	if !storageEnrollmentRetainsOriginal(before, catalog) {
		return catalog, source, errStorageCustody
	}
	if retained != nil {
		a, e1 := storageEnrollmentPeakEncodeSource(source)
		b, e2 := storageEnrollmentPeakEncodeSource(*retained)
		if e1 != nil || e2 != nil || !bytes.Equal(a, b) {
			return catalog, source, errStorageCustody
		}
	}
	return catalog, source, p.held(ctx)
}

// The physical originals remain retained by the caller. This projection only
// chooses the independently validated core pair and actual physical high bank.
func storageEnrollmentOriginalCorePair(raw [2][]byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([2][]byte, int, error) {
	if len(raw[0]) != storageOwnerCatalogBytes || len(raw[1]) != storageOwnerCatalogBytes {
		return [2][]byte{}, 0, storagebudget.ErrBounds
	}
	if string(raw[0][:8]) == "DSKONL1\x00" || string(raw[1][:8]) == "DSKONL1\x00" {
		pair, err := storageCompositionReadPair(raw[0], raw[1], record, claim)
		if err != nil || pair.RecoveryOnly {
			return [2][]byte{}, 0, storagebudget.ErrBounds
		}
		return [2][]byte{pair.Banks[0].Core, pair.Banks[1].Core}, int(pair.Selected), nil
	}
	bank := 1
	if storageEnrollmentPeakU64(raw[0][16:24]) > storageEnrollmentPeakU64(raw[1][16:24]) {
		bank = 0
	}
	if string(raw[bank][:8]) == "DSKOWN3\x00" {
		_, err := storageOwnerDecodeCatalogPairV3(raw[0], raw[1], record, claim)
		return raw, bank, err
	}
	_, selected, err := storageEnrollmentPeakDecodePair(raw[0], raw[1], record, claim)
	return raw, selected, err
}
