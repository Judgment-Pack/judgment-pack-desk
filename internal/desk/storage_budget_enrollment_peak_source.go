package desk

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

type storageEnrollmentPeakSource struct {
	Installation              storagebudget.ID
	QBinding                  storagebudget.Digest
	Project                   storageOwnerPersistentObject
	Workspace                 storagebudget.ID
	WorkspaceGeneration       uint64
	OwnerKey, HistoryIdentity []byte
	HomeSource                storagebudget.Digest
	Filesystem                storagebudget.ID
	FilesystemIdentity        storagebudget.Digest
	FilesystemGeneration      uint64
	CapacityCount             uint8
	CapacityRefs              [4]storagebudget.CapacityRef
	CapacityBinding           storagebudget.Digest
	Namespace                 []byte
	Generation                uint64
	Default                   *storageEnrollmentDefaultSource
	ActionPageDigest          storagebudget.Digest // family6 only; full original selected J4 page
}

// Proposed cycle correction: full original source facts precede either new
// root identity or root-specific Q binding. This private comparison is not a
// substitute for original source custody, capture or installed qualification.
type storageEnrollmentPeakSourceMappingMaterial struct {
	Installation    storagebudget.ID
	QBinding        storagebudget.Digest
	Project         storageOwnerPersistentObject
	Workspace       storagebudget.RegisteredWorkspace
	OwnerKey        []byte
	HistoryIdentity []byte
	HomeSource      storagebudget.Digest
	Namespace       []byte
	Generation      uint64
	Filesystem      storageOwnerFilesystem
	CapacityCount   uint8
	CapacityRefs    [4]storagebudget.CapacityRef
	Constraints     []storageOwnerConstraint
	Membership      storageOwnerMembershipMaterial
	Profiles        []storageOwnerProfile
}
type storageEnrollmentPeakRegistryMaterial struct {
	Role           uint8
	Parent         storageOwnerPersistentObject
	Name           []byte
	OwnerKind      uint8
	OwnerKey       []byte
	Location       storageOwnerLocation
	RelationDigest storagebudget.Digest
	MaxBytes       uint32
}
type storageEnrollmentPeakHomeSourceMaterial struct {
	Installation    storagebudget.ID
	QBinding        storagebudget.Digest
	Project         storageOwnerPersistentObject
	Workspace       storageOwnerWorkspace
	OwnerKey        []byte
	HistoryIdentity []byte
	Kind            uint8
	ConfigPath      []byte
	HomeOwner       string
	HomePath        []byte
	HomeObject      storageOwnerPersistentObject
	Registry        storageEnrollmentPeakRegistryMaterial
	Entry           storageOwnerHomeRelation
}

func storageEnrollmentPeakHomeSourceMatches(b storageEnrollmentPeakSource, c storageOwnerCatalogV2) bool {
	m := storageEnrollmentPeakHomeSourceMaterial{Installation: b.Installation, QBinding: b.QBinding, Project: b.Project, OwnerKey: b.OwnerKey, HistoryIdentity: b.HistoryIdentity, Kind: c.OriginalOwner.Kind, ConfigPath: c.OriginalOwner.ConfigPath, HomeOwner: c.OriginalOwner.HomeOwner, HomePath: c.OriginalOwner.HomePath, HomeObject: c.OriginalOwner.HomeObject, Registry: storageEnrollmentPeakRegistryMaterial{Name: []byte{}, OwnerKey: []byte{}, Location: storageOwnerLocation{Path: []byte{}, Previous: []byte{}}}, Entry: storageOwnerHomeRelation{Folder: []byte{}, HistoryIdentity: []byte{}}}
	found := false
	for _, w := range c.Workspaces {
		if w.Definition.ID == b.Workspace && w.Definition.Generation == b.WorkspaceGeneration {
			m.Workspace = w
			found = true
		}
	}
	if !found {
		return false
	}
	hashMatches := func() bool {
		raw, err := json.Marshal(m)
		return err == nil && storageEnrollmentPeakHash("desk-project-private-home-source-peak-v1", raw) == b.HomeSource
	}
	if m.Kind == 1 {
		return hashMatches()
	}
	if m.Kind != 2 || int(c.OriginalOwner.RegistryControl) >= len(c.OwnerControls) {
		return false
	}
	v := c.OwnerControls[c.OriginalOwner.RegistryControl]
	if int(v.ParentObject) >= len(c.Objects) {
		return false
	}
	m.Registry = storageEnrollmentPeakRegistryMaterial{Role: v.Role, Parent: c.Objects[v.ParentObject].Persistent, Name: v.Name, OwnerKind: v.OwnerKind, OwnerKey: v.OwnerKey, Location: v.Location, RelationDigest: v.RelationDigest, MaxBytes: v.MaxBytes}
	matches := 0
	for _, entry := range c.OriginalOwner.Entries {
		if entry.RunnerWorkspace == string(m.Workspace.OwnerKey) && bytes.Equal(entry.HistoryIdentity, m.Workspace.HistoryIdentity) {
			m.Entry = entry
			if hashMatches() {
				matches++
			}
		}
	}
	return matches == 1
}
func storageEnrollmentPeakSourceMapping(b storageEnrollmentPeakSource, c storageOwnerCatalogV2, parent storageOwnerRoot) (storagebudget.Digest, error) {
	if !storageEnrollmentPeakHomeSourceMatches(b, c) {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	m := storageEnrollmentPeakSourceMappingMaterial{Installation: b.Installation, QBinding: b.QBinding, Project: b.Project, OwnerKey: b.OwnerKey, HistoryIdentity: b.HistoryIdentity, HomeSource: b.HomeSource, Namespace: b.Namespace, Generation: b.Generation, CapacityCount: b.CapacityCount, CapacityRefs: b.CapacityRefs, Constraints: []storageOwnerConstraint{}, Profiles: []storageOwnerProfile{}}
	wf, ff := false, false
	for _, w := range c.Workspaces {
		if w.Definition.ID == b.Workspace && w.Definition.Generation == b.WorkspaceGeneration {
			m.Workspace = w.Definition
			wf = true
		}
	}
	for _, f := range c.Filesystems {
		if f.Definition.ID == b.Filesystem && f.Definition.Identity == b.FilesystemIdentity && f.Definition.Generation == b.FilesystemGeneration {
			m.Filesystem = f
			ff = true
		}
	}
	if !wf || !ff || b.CapacityCount == 0 || b.CapacityCount > 4 {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	for i := 0; i < int(b.CapacityCount); i++ {
		found := false
		for _, v := range c.Constraints {
			if v.Definition.ID == b.CapacityRefs[i].Constraint && v.Definition.Generation == b.CapacityRefs[i].Generation {
				m.Constraints = append(m.Constraints, v)
				found = true
				break
			}
		}
		if !found {
			return storagebudget.Digest{}, storagebudget.ErrBounds
		}
	}
	p := parent.Membership
	m.Membership = storageOwnerMembershipMaterial{Backend: p.Backend, ProfileID: p.ProfileID, ProfileVersion: p.ProfileVersion, ProfileEvidence: p.ProfileEvidence, Pool: p.Pool, VolumeUUID: p.VolumeUUID, ContainerUUID: p.ContainerUUID, User: p.User, Group: p.Group, Project: p.Project, Creation: p.Creation, ExistingObjects: []storageOwnerObjectMaterial{}}
	for _, index := range p.ExistingObjects {
		if int(index) >= len(c.Objects) {
			return storagebudget.Digest{}, storagebudget.ErrBounds
		}
		m.Membership.ExistingObjects = append(m.Membership.ExistingObjects, c.objectMaterial(index))
	}
	required := map[uint16]bool{p.ProfileID: true, m.Filesystem.PersistentProfile: true, m.Filesystem.InodeProfile: true}
	for _, v := range c.Profiles {
		if required[v.ID] {
			m.Profiles = append(m.Profiles, v)
			delete(required, v.ID)
		}
	}
	if len(required) != 0 || len(m.Profiles) == 0 || len(m.Profiles) > 3 {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return storagebudget.Digest{}, err
	}
	if b.Default != nil {
		wrapped, err := json.Marshal(struct {
			Original storageEnrollmentPeakSourceMappingMaterial
			Default  storageEnrollmentDefaultSource
		}{m, *b.Default})
		if err != nil {
			return storagebudget.Digest{}, err
		}
		return storageEnrollmentPeakHash("desk-default-history-source-mapping-v1", wrapped), nil
	}
	return storageEnrollmentPeakHash("desk-project-private-source-mapping-peak-v1", raw), nil
}
func storageEnrollmentPeakDecodeSource(raw []byte) (storageEnrollmentPeakSource, error) {
	var s storageEnrollmentPeakSource
	r := storageEnrollmentPeakReader{raw: raw}
	s.Installation = r.id()
	s.QBinding = r.digest()
	s.Project = r.native()
	s.Workspace = r.id()
	s.WorkspaceGeneration = r.u64()
	s.OwnerKey = append([]byte{}, r.data(4096)...)
	s.HistoryIdentity = append([]byte{}, r.data(4096)...)
	s.HomeSource = r.digest()
	s.Filesystem = r.id()
	s.FilesystemIdentity = r.digest()
	s.FilesystemGeneration = r.u64()
	s.CapacityCount = r.u8()
	for i := range s.CapacityRefs {
		s.CapacityRefs[i] = storagebudget.CapacityRef{Constraint: r.id(), Generation: r.u64()}
	}
	s.CapacityBinding = r.digest()
	s.Namespace = append([]byte{}, r.data(13)...)
	s.Generation = r.u64()
	if !bytes.Equal(s.Namespace, []byte(".desk-private")) {
		extra, err := storageEnrollmentReadDefaultSource(&r)
		if err != nil {
			return storageEnrollmentPeakSource{}, err
		}
		s.Default = &extra
	} else if r.pos != len(r.raw) {
		if len(r.raw)-r.pos != 48 || !bytes.Equal(r.take(8), []byte("DSKPC1\x00\x00")) || r.u16() != 1 || r.u16() != 48 || r.u32() != 0 {
			return storageEnrollmentPeakSource{}, storagebudget.ErrBounds
		}
		s.ActionPageDigest = r.digest()
		if s.ActionPageDigest == (storagebudget.Digest{}) {
			return storageEnrollmentPeakSource{}, storagebudget.ErrBounds
		}
	}
	if !r.done() || len(raw) > storageEnrollmentPeakSourceMaximum || s.Installation == (storagebudget.ID{}) || s.QBinding == (storagebudget.Digest{}) || !s.Project.valid(1) || s.Workspace == (storagebudget.ID{}) || s.WorkspaceGeneration == 0 || len(s.OwnerKey) == 0 || len(s.HistoryIdentity) == 0 || s.HomeSource == (storagebudget.Digest{}) || s.Filesystem == (storagebudget.ID{}) || s.FilesystemIdentity == (storagebudget.Digest{}) || s.FilesystemGeneration == 0 || s.CapacityBinding == (storagebudget.Digest{}) || s.CapacityCount == 0 || s.CapacityCount > 4 || !storageEnrollmentDefaultSourceNamespace(s) || s.Generation == 0 {
		return storageEnrollmentPeakSource{}, storagebudget.ErrBounds
	}
	for i, v := range s.CapacityRefs {
		if i >= int(s.CapacityCount) {
			if v != (storagebudget.CapacityRef{}) {
				return storageEnrollmentPeakSource{}, storagebudget.ErrBounds
			}
		} else if v.Constraint == (storagebudget.ID{}) || v.Generation == 0 || i > 0 && bytes.Compare(s.CapacityRefs[i-1].Constraint[:], v.Constraint[:]) >= 0 {
			return storageEnrollmentPeakSource{}, storagebudget.ErrBounds
		}
	}
	return s, nil
}
func storageEnrollmentPeakEncodeSource(s storageEnrollmentPeakSource) ([]byte, error) {
	w := storageEnrollmentPeakWriter{max: storageEnrollmentPeakSourceMaximum}
	w.put(s.Installation[:])
	w.put(s.QBinding[:])
	w.put(storageEnrollmentPeakNativeBytes(s.Project))
	w.put(s.Workspace[:])
	w.u64(s.WorkspaceGeneration)
	w.data(s.OwnerKey, 4096)
	w.data(s.HistoryIdentity, 4096)
	w.put(s.HomeSource[:])
	w.put(s.Filesystem[:])
	w.put(s.FilesystemIdentity[:])
	w.u64(s.FilesystemGeneration)
	w.u8(s.CapacityCount)
	for _, v := range s.CapacityRefs {
		w.put(v.Constraint[:])
		w.u64(v.Generation)
	}
	w.put(s.CapacityBinding[:])
	w.data(s.Namespace, 13)
	w.u64(s.Generation)
	if s.ActionPageDigest != (storagebudget.Digest{}) {
		if s.Default != nil || !bytes.Equal(s.Namespace, []byte(".desk-private")) {
			return nil, storagebudget.ErrBounds
		}
		w.put([]byte("DSKPC1\x00\x00"))
		w.u16(1)
		w.u16(48)
		w.u32(0)
		w.put(s.ActionPageDigest[:])
	}
	if s.Default != nil {
		extra, err := storageEnrollmentWriteDefaultSource(*s.Default)
		if err != nil {
			return nil, err
		}
		w.put(extra)
	}
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	if _, err := storageEnrollmentPeakDecodeSource(w.raw); err != nil {
		return nil, err
	}
	return w.raw, nil
}

func storageEnrollmentPeakChildIdentity(c storageOwnerCatalogV2, r storageOwnerRoot) (storagebudget.Digest, error) {
	// The Kind8 rule explicitly uses the typed empty Home even for Role3.
	// Compute the original source material with that new discriminator, then
	// feed the unchanged original root material/domain. No old method changes.
	s := r.Source
	m := storageOwnerSourceMaterial{Kind: 8, OwnerKey: s.OwnerKey, Path: s.Path, ParentRoot: s.ParentRoot, Relative: s.Relative, ControlKind: 0, FixedControl: storageOwnerEmptyObjectMaterial(), ReplaceableControl: storageOwnerSelectedControlMaterial{Name: []byte{}, OwnerKey: []byte{}, SelectedPath: []byte{}}, Home: storageOwnerHomeRelation{Folder: []byte{}, HistoryIdentity: []byte{}}}
	d, err := storageOwnerDigest("desk-storage-owner-source-relation-v2", m)
	if err != nil {
		return storagebudget.Digest{}, err
	}
	v := r.Membership
	mm := storageOwnerMembershipMaterial{Backend: v.Backend, ProfileID: v.ProfileID, ProfileVersion: v.ProfileVersion, ProfileEvidence: v.ProfileEvidence, Pool: v.Pool, VolumeUUID: v.VolumeUUID, ContainerUUID: v.ContainerUUID, User: v.User, Group: v.Group, Project: v.Project, Creation: v.Creation, ExistingObjects: []storageOwnerObjectMaterial{}}
	for _, idx := range v.ExistingObjects {
		if int(idx) >= len(c.Objects) {
			return storagebudget.Digest{}, storagebudget.ErrBounds
		}
		mm.ExistingObjects = append(mm.ExistingObjects, c.objectMaterial(idx))
	}
	rd := r.Definition
	return storageOwnerDigest("desk-storage-owner-root-identity-v2", storageOwnerRootMaterial{ID: rd.ID, Owner: rd.Owner, Filesystem: rd.Filesystem, Generation: rd.Generation, Role: r.Role, Category: r.Category, Directory: c.objectMaterial(r.DirectoryObject), Domain: storageOwnerEmptyObjectMaterial(), Work: storageOwnerEmptyObjectMaterial(), SourceRelation: d, Membership: mm, FixedControls: []storageOwnerObjectMaterial{}})
}

func storageEnrollmentPeakValidateCatalog(c storageOwnerCatalogV2, record storageOwnerRecordV2, claim storageOwnerClaimV2) error {
	if c.Version != 1 || !storageEnrollmentPeakCatalogBounded(c) || !claim.matches(record) || c.Installation != record.Installation || c.QBinding != record.QBinding || c.Sequence == 0 || c.Phase != "prepared" || c.Transition == (storagebudget.ID{}) || c.ExpectedQRevision == 0 || !c.Policy.valid() {
		return storagebudget.ErrBounds
	}
	// Logical P remains Version1; its retained objects use the underlying V3
	// family, including the all-or-none transfer controls. The general V2
	// reader keeps its independent refusal of those objects.
	advisory := c
	advisory.Version = 3
	if c.validateOriginal(record) != nil || advisory.validateObjects(record) != nil || c.validateControls() != nil || c.validateUsesForDefault(true) != nil {
		return storagebudget.ErrBounds
	}
	if advisory.validateTables(record) != nil {
		return storagebudget.ErrBounds
	}
	roots := map[storagebudget.ID]storageOwnerRoot{}
	var parentNative = map[storageOwnerPersistentObject]bool{}
	directories := map[uint32]bool{}
	transferNamespaces := map[storageOwnerPersistentObject]map[string]bool{}
	identities := map[storagebudget.Digest]bool{}
	refs := 0
	roles := [3]int{}
	constraints := []storagebudget.CapacityConstraint{}
	for _, v := range c.Constraints {
		constraints = append(constraints, v.Definition)
	}
	for i, r := range c.Roots {
		d := r.Definition
		if d.ID == (storagebudget.ID{}) || d.Owner != c.QBinding || d.Identity == (storagebudget.Digest{}) || identities[d.Identity] || d.Generation == 0 || !storageOwnerText(d.Locator, 256, false) || i > 0 && bytes.Compare(c.Roots[i-1].Definition.ID[:], d.ID[:]) >= 0 || r.Role < 1 || r.Role > 8 || r.Category >= 9 || int(r.DirectoryObject) >= len(c.Objects) || c.Objects[r.DirectoryObject].ModeClass != 1 || d.CapacityCount == 0 || d.CapacityCount > 4 || !storageOwnerSortedIDs(r.References) {
			return storagebudget.ErrBounds
		}
		identities[d.Identity] = true
		roots[d.ID] = r
		refs += len(r.References)
		if refs > 65536 {
			return storagebudget.ErrBounds
		}
		if r.Role < 3 {
			roles[r.Role]++
		}
		if r.Role != 7 && !storageTransferCatalogView(r) {
			if directories[r.DirectoryObject] {
				return storagebudget.ErrBounds
			}
			directories[r.DirectoryObject] = true
		}
		var fs storageOwnerFilesystem
		var profile storageOwnerProfile
		foundF, foundP := false, false
		for _, f := range c.Filesystems {
			if f.Definition.ID == d.Filesystem {
				fs = f
				foundF = true
			}
		}
		for _, p := range c.Profiles {
			if p.ID == r.Membership.ProfileID {
				profile = p
				foundP = true
			}
		}
		m := r.Membership
		if !foundF || !foundP || m.ProfileVersion != profile.Version || m.Backend != profile.Backend || m.ProfileEvidence != profile.Evidence || m.Backend != fs.Backend || m.VolumeUUID != fs.VolumeUUID || m.ContainerUUID != fs.ContainerUUID || m.Pool != fs.Definition.ID || m.Creation < 1 || m.Creation > 2 || c.Objects[r.DirectoryObject].Persistent.Backend != m.Backend || c.Objects[r.DirectoryObject].Persistent.VolumeUUID != m.VolumeUUID {
			return storagebudget.ErrBounds
		}
		for j, idx := range m.ExistingObjects {
			if int(idx) >= len(c.Objects) || j > 0 && m.ExistingObjects[j-1] >= idx || c.Objects[idx].ModeClass != 2 {
				return storagebudget.ErrBounds
			}
		}
		fp, ip := storageOwnerProfile{}, storageOwnerProfile{}
		for _, p := range c.Profiles {
			if p.ID == fs.PersistentProfile {
				fp = p
			}
			if p.ID == fs.InodeProfile {
				ip = p
			}
		}
		if fp.PersistentEvidence != profile.PersistentEvidence || ip.InodeEvidence != profile.InodeEvidence || fs.Definition.CapabilityEvidence != profile.InodeEvidence {
			return storagebudget.ErrBounds
		}
		if m.Backend == 1 {
			for i, subject := range []storageOwnerSubject{m.User, m.Group, m.Project} {
				if !advisory.subjectValid(subject, uint8(i+1)) || subject.State != 4 && (profile.MappingEvidence == (storagebudget.Digest{}) || profile.QuotaEvidence == (storagebudget.Digest{})) {
					return storagebudget.ErrBounds
				}
			}
		} else if m.User != (storageOwnerSubject{}) || m.Group != (storageOwnerSubject{}) || m.Project != (storageOwnerSubject{}) {
			return storagebudget.ErrBounds
		}
		workspaceFound := d.Workspace == (storagebudget.ID{})
		for _, w := range c.Workspaces {
			workspaceFound = workspaceFound || w.Definition.ID == d.Workspace
		}
		if !workspaceFound {
			return storagebudget.ErrBounds
		}
		for _, id := range r.References {
			found := false
			for _, w := range c.Workspaces {
				found = found || w.Definition.ID == id
			}
			if !found {
				return storagebudget.ErrBounds
			}
		}
		root := storagebudget.Root{ID: d.ID, Owner: d.Owner, Identity: d.Identity, Filesystem: d.Filesystem, Workspace: d.Workspace, Generation: d.Generation, Locator: d.Locator, CapacityCount: d.CapacityCount, CapacityRefs: d.CapacityRefs, CapacityBinding: d.CapacityBinding}
		bind, err := storagebudget.CapacityBindingFor(root, fs.Definition, constraints)
		if err != nil || bind != d.CapacityBinding {
			return storagebudget.ErrBounds
		}
		switch r.Source.Kind {
		case 7:
			b, err := storageEnrollmentPeakDecodeSource(r.Source.Relative)
			if err != nil || b.Default != nil || b.ActionPageDigest != (storagebudget.Digest{}) || r.Role != 7 || r.Category != storagebudget.Control || r.DomainObject != storageOwnerNoObject || r.WorkObject != storageOwnerNoObject || len(r.Source.Path) != 0 || r.Source.ParentRoot != (storagebudget.ID{}) || r.Source.ControlKind != 0 || r.Source.ControlIndex != storageOwnerNoControl || r.Source.ControlSelection != 0 || !bytes.Equal(r.Source.OwnerKey, b.OwnerKey) || c.Objects[r.DirectoryObject].Persistent != b.Project || d.Identity != storageEnrollmentPeakHash("desk-project-private-parent-scope-e1", r.Source.Relative) || !storageEnrollmentPeakSourceJoins(b, c, r) || parentNative[b.Project] {
				return storagebudget.ErrBounds
			}
			parentNative[b.Project] = true
		case 9:
			b, err := storageEnrollmentPeakDecodeSource(r.Source.Relative)
			if err != nil || b.Default == nil || b.Default.Workflow != 2 && b.Default.Workflow != 4 || r.Role != 8 || r.Category != storagebudget.Control || r.DomainObject != storageOwnerNoObject || r.WorkObject != storageOwnerNoObject || !bytes.Equal(r.Source.Path, b.Default.SelectedPath) || r.Source.ParentRoot != (storagebudget.ID{}) || r.Source.ControlKind != 0 || r.Source.ControlIndex != storageOwnerNoControl || (r.Source.ControlSelection != 0 && !(b.Default.Workflow == 4 && r.Source.ControlSelection == 4)) || !bytes.Equal(r.Source.OwnerKey, b.OwnerKey) || c.Objects[r.DirectoryObject].Persistent != b.Default.Selected || d.Identity != storageEnrollmentAncestorSourceIdentity(b, r.Source.Relative) || !storageEnrollmentPeakSourceJoins(b, c, r) || b.Default.Workflow != 4 && parentNative[b.Default.Selected] {
				return storagebudget.ErrBounds
			}
			if b.Default.Workflow == 4 {
				if transferNamespaces[b.Default.Selected] == nil {
					transferNamespaces[b.Default.Selected] = map[string]bool{}
				}
				next := string(b.Default.Next)
				if r.Source.ControlSelection != 4 {
					if transferNamespaces[b.Default.Selected][next] {
						return storagebudget.ErrBounds
					}
					transferNamespaces[b.Default.Selected][next] = true
				}
			} else {
				parentNative[b.Default.Selected] = true
			}
		case 11:
			if storageTransferCapturedRootJoins(c, r) != nil {
				return storagebudget.ErrBounds
			}

		case 8:
			if r.Role != 3 && r.Role != 6 || r.Category != storagebudget.Control || r.DomainObject != storageOwnerNoObject || r.WorkObject != storageOwnerNoObject || len(r.Source.Path) != 0 || !bytes.Equal(r.Source.Relative, []byte(".desk-private")) || r.Source.ControlKind != 0 || r.Source.ControlIndex != storageOwnerNoControl || r.Source.ControlSelection != 0 {
				return storagebudget.ErrBounds
			}
			identity, err := storageEnrollmentPeakChildIdentity(c, r)
			if err != nil || identity != d.Identity {
				return storagebudget.ErrBounds
			}
		default:
			if r.Source.Kind < 1 || r.Source.Kind > 6 || storageEnrollmentPeakRootRole(c, r) != nil {
				return storagebudget.ErrBounds
			}
			identity, err := c.rootIdentity(r)
			if err != nil || identity != d.Identity {
				return storagebudget.ErrBounds
			}
		}
	}
	for i, o := range c.Objects {
		if o.Source == 3 && o.Parent == storageOwnerNoObject {
			used := false
			for _, r := range c.Roots {
				used = used || r.DirectoryObject == uint32(i)
			}
			if !used {
				return storagebudget.ErrBounds
			}
		}
	}
	if roles[1] != 1 || roles[2] != 1 || advisory.validateCapacityRelations() != nil {
		return storagebudget.ErrBounds
	}
	ids := map[storagebudget.ID]int{}
	for i, r := range c.Roots {
		ids[r.Definition.ID] = i
	}
	for _, r := range c.Roots {
		if r.Source.Kind == 8 {
			p, ok := roots[r.Source.ParentRoot]
			if !ok || p.Role != 7 || p.Source.Kind != 7 {
				return storagebudget.ErrBounds
			}
			b, err := storageEnrollmentPeakDecodeSource(p.Source.Relative)
			if err != nil || !bytes.Equal(r.Source.OwnerKey, b.OwnerKey) || r.Definition.Filesystem != p.Definition.Filesystem || r.Definition.Workspace != p.Definition.Workspace || r.Definition.CapacityCount != p.Definition.CapacityCount || r.Definition.CapacityRefs != p.Definition.CapacityRefs || !reflect.DeepEqual(r.References, p.References) || c.Objects[r.DirectoryObject].Persistent == b.Project {
				return storagebudget.ErrBounds
			}
		} else if r.Source.Kind != 7 && r.Source.Kind != 9 && r.Source.Kind != 11 && advisory.validateRootGraph(r, ids) != nil {
			return storagebudget.ErrBounds
		}
	}
	if _, err := storageTransferNamespaceRelations(c); err != nil {
		return err
	}
	before, after, err := c.definitions()
	if err != nil {
		return err
	}
	d, err := storageOwnerDigest("desk-storage-owner-definitions-v2", before)
	if err != nil || d != c.BeforeDefinitionsDigest {
		return storagebudget.ErrBounds
	}
	d, err = storageOwnerDigest("desk-storage-owner-definitions-v2", after)
	if err != nil || d != c.AfterDefinitionsDigest {
		return storagebudget.ErrBounds
	}
	raw, err := json.Marshal(c)
	if err != nil || !storageEnrollmentPeakCatalogPreflight(raw) {
		return storagebudget.ErrBounds
	}
	return nil
}
func storageEnrollmentPeakSourceJoins(b storageEnrollmentPeakSource, c storageOwnerCatalogV2, r storageOwnerRoot) bool {
	d := r.Definition
	focal := false
	for _, id := range r.References {
		focal = focal || id == b.Workspace
	}
	if !focal {
		return false
	}
	if b.Installation != c.Installation || b.QBinding != c.QBinding || b.Filesystem != d.Filesystem || b.Default == nil && d.Workspace != (storagebudget.ID{}) && b.Workspace != d.Workspace || b.Generation != d.Generation || b.CapacityCount != d.CapacityCount || b.CapacityRefs != d.CapacityRefs {
		return false
	}
	if b.Default != nil {
		found := false
		for _, id := range r.References {
			found = found || id == b.Workspace
		}
		if !found || d.Workspace != (storagebudget.ID{}) && d.Workspace != b.Workspace && b.Default.Workflow != 5 {
			return false
		}
	}
	mapping, err := storageEnrollmentPeakSourceMapping(b, c, r)
	return err == nil && mapping == b.CapacityBinding
}

// Exact attainable maximum: shared body8542 =107 prefix +2*4098 owner
// strings +217 fixed source fields +14 namespace +8 generation. Workflow2's
// trailer4564 =2 version +1 workflow +1 branch +4*35 natives +2*32 digests
// +4098 selected path +1 OwnSelf +257 next component. No alignment/padding.
const storageEnrollmentDefaultSourceMaximum = 13106
const storageEnrollmentDefaultSourceTrailerMaximum = 4564

// Transfer namespaces have13 bytes, one more than legacy history-base.
// Exact transfer maximum:8543 shared bytes +16921 trailer =25464.
// The added69 bytes remain inside existing Relative65599 and P/Q extents.
const storageEnrollmentPeakSourceMaximum = storageEnrollmentDefaultSourceMaximum + 12288 + 1 + 69
const storageEnrollmentTransferSourceTrailerMaximum = storageEnrollmentDefaultSourceTrailerMaximum + 12288 + 69

// This is a closed workflow discriminator in the shared source format, not a
// change to Kind7/8 or their literal .desk-private authority. Old source readers
// reject both new namespaces. Old complete readers also reject Board kind9/10.
type storageEnrollmentDefaultSource struct {
	Workflow, Branch                     byte // 1 final default History;2 selected ancestry scope
	Settings, Selected, Marker, Location storageOwnerPersistentObject
	LocationPayload, CreationPage        storagebudget.Digest
	SelectedPath                         []byte
	OwnSelf                              byte
	Next                                 []byte
	// Unpublished transfer workflows append count2+digest32+source-native35. Legacy trailers
	// end at Next and cannot consume or reinterpret this closed suffix.
	ChainCount   uint16
	ChainDigest  storagebudget.Digest
	SourceNative storageOwnerPersistentObject // original selected source, never rewritten on reuse
}

func storageEnrollmentDefaultSourceNamespace(s storageEnrollmentPeakSource) bool {
	if s.Default == nil {
		return bytes.Equal(s.Namespace, []byte(".desk-private"))
	}
	if s.ActionPageDigest != (storagebudget.Digest{}) {
		return false
	}
	d := s.Default
	if d.Branch < 1 || d.Branch > 3 || !d.Settings.valid(1) || !d.Selected.valid(1) || !storageEnrollmentSourcePath(d) || d.OwnSelf > 1 {
		return false
	}
	transfer := d.Workflow == 4 || d.Workflow == 5 || d.Workflow == 6
	if transfer {
		parts, err := storageTransferSourceChainParts(d)
		if err != nil || len(parts) != int(d.ChainCount) || !d.SourceNative.valid(1) || d.ChainDigest == (storagebudget.Digest{}) {
			return false
		}
	} else if d.ChainCount != 0 || d.ChainDigest != (storagebudget.Digest{}) || d.SourceNative != (storageOwnerPersistentObject{}) {
		return false
	}
	switch d.Workflow {
	case 6:
		return bytes.Equal(s.Namespace, []byte("transfer-root")) && d.OwnSelf == 1 && len(d.Next) == 0 && (d.Marker == (storageOwnerPersistentObject{}) || d.Marker.valid(2)) && d.Location == (storageOwnerPersistentObject{}) && d.LocationPayload == (storagebudget.Digest{}) && d.CreationPage == (storagebudget.Digest{})
	case 4:
		return bytes.Equal(s.Namespace, []byte("transfer-base")) && d.OwnSelf == 0 && storageOwnerCatalogNameComponent(d.Next) && len(d.Next) <= 255 && d.Marker == (storageOwnerPersistentObject{}) && d.Location == (storageOwnerPersistentObject{}) && d.LocationPayload == (storagebudget.Digest{})
	case 5:
		return bytes.Equal(s.Namespace, []byte("transfer-data")) && d.OwnSelf == 0 && len(d.Next) == 0 && d.Marker.valid(2) && d.Location.valid(2) && d.LocationPayload != (storagebudget.Digest{}) && d.CreationPage != (storagebudget.Digest{})
	case 3:
		return d.Branch != storageCreationContained && bytes.Equal(s.Namespace, []byte("legacy")) && d.Selected == d.Settings && d.Marker == (storageOwnerPersistentObject{}) && d.Location.valid(2) && d.LocationPayload != (storagebudget.Digest{}) && d.CreationPage != (storagebudget.Digest{}) && d.OwnSelf == 0 && len(d.Next) == 0
	case 1:
		if d.OwnSelf != 0 || len(d.Next) != 0 || !d.Marker.valid(2) || !d.Location.valid(2) || d.LocationPayload == (storagebudget.Digest{}) || d.CreationPage == (storagebudget.Digest{}) {
			return false
		}
		if d.Branch == storageCreationPersonal {
			return bytes.Equal(s.Namespace, []byte("history"))
		}
		return bytes.Equal(s.Namespace, []byte("data"))
	case 2:
		return d.Branch == storageCreationPersonal && bytes.Equal(s.Namespace, []byte("history-base")) && storageOwnerCatalogNameComponent(d.Next) && len(d.Next) <= 255 && d.Marker == (storageOwnerPersistentObject{}) && d.Location == (storageOwnerPersistentObject{}) && d.LocationPayload == (storagebudget.Digest{}) && (d.OwnSelf == 0 && d.CreationPage == (storagebudget.Digest{}) || d.OwnSelf == 1 && d.CreationPage != (storagebudget.Digest{}))
	}
	return false
}
func storageEnrollmentReadDefaultSource(r *storageEnrollmentPeakReader) (storageEnrollmentDefaultSource, error) {
	var d storageEnrollmentDefaultSource
	if r.u16() != 1 {
		return d, storagebudget.ErrBounds
	}
	d.Workflow = r.u8()
	d.Branch = r.u8()
	d.Settings = r.native()
	d.Selected = r.native()
	d.Marker = r.native()
	d.Location = r.native()
	d.LocationPayload = r.digest()
	d.CreationPage = r.digest()
	pathMax := 4096
	if d.Workflow == 4 || d.Workflow == 5 || d.Workflow == 6 {
		pathMax = storageTransferPointerBytes
	}
	d.SelectedPath = append([]byte{}, r.data(pathMax)...)
	d.OwnSelf = r.u8()
	d.Next = append([]byte{}, r.data(255)...)
	if d.Workflow == 4 || d.Workflow == 5 || d.Workflow == 6 {
		d.ChainCount = r.u16()
		d.ChainDigest = r.digest()
		d.SourceNative = r.native()
	}
	if r.bad {
		return storageEnrollmentDefaultSource{}, storagebudget.ErrBounds
	}
	return d, nil
}
func storageEnrollmentWriteDefaultSource(d storageEnrollmentDefaultSource) ([]byte, error) {
	w := storageEnrollmentPeakWriter{max: storageEnrollmentTransferSourceTrailerMaximum}
	w.u16(1)
	w.u8(d.Workflow)
	w.u8(d.Branch)
	for _, n := range []storageOwnerPersistentObject{d.Settings, d.Selected, d.Marker, d.Location} {
		w.put(storageEnrollmentPeakNativeBytes(n))
	}
	w.put(d.LocationPayload[:])
	w.put(d.CreationPage[:])
	pathMax := 4096
	if d.Workflow == 4 || d.Workflow == 5 || d.Workflow == 6 {
		pathMax = storageTransferPointerBytes
	}
	w.data(d.SelectedPath, pathMax)
	w.u8(d.OwnSelf)
	w.data(d.Next, 255)
	if d.Workflow == 4 || d.Workflow == 5 || d.Workflow == 6 {
		w.u16(d.ChainCount)
		w.put(d.ChainDigest[:])
		w.put(storageEnrollmentPeakNativeBytes(d.SourceNative))
	}
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	return w.raw, nil
}

// Both board readers use this same bounded source scan. It accepts no trailing
// source bytes: only their already versioned section's zero padding may follow.
func storageEnrollmentPeakScanSource(r *storageEnrollmentPeakReader) (storageEnrollmentPeakSource, error) {
	start := r.pos
	r.take(107)
	r.data(4096)
	r.data(4096)
	r.take(217)
	namespace := r.data(13)
	r.take(8)
	if !bytes.Equal(namespace, []byte(".desk-private")) {
		if _, err := storageEnrollmentReadDefaultSource(r); err != nil {
			return storageEnrollmentPeakSource{}, err
		}
	}
	if bytes.Equal(namespace, []byte(".desk-private")) && !r.bad && len(r.raw)-r.pos >= 8 && bytes.Equal(r.raw[r.pos:r.pos+8], []byte("DSKPC1\x00\x00")) {
		r.take(48)
	}
	if r.bad {
		return storageEnrollmentPeakSource{}, storagebudget.ErrBounds
	}
	return storageEnrollmentPeakDecodeSource(r.raw[start:r.pos])
}

func storageEnrollmentDefaultHistorySourceJoins(source storageEnrollmentPeakSource, c storageOwnerCatalogV2) bool {
	d := source.Default
	if d == nil || d.Workflow != 1 && d.Workflow != 3 && d.Workflow != 5 || !storageEnrollmentDefaultSourceNamespace(source) || source.Installation != c.Installation || source.QBinding != c.QBinding {
		return false
	}
	var root *storageOwnerRoot
	for i := range c.Roots {
		r := &c.Roots[i]
		if ((d.Workflow == 1 || d.Workflow == 5) && r.Role == 4 && (r.Source.Kind == 4 || d.Workflow == 5 && r.Source.Kind == 11) || d.Workflow == 3 && r.Role == 1 && r.Source.Kind == 1 && bytes.Equal(r.Source.Path, c.OriginalOwner.ConfigPath)) && int(r.DirectoryObject) < len(c.Objects) && c.Objects[r.DirectoryObject].Persistent == d.Selected && bytes.Equal(r.Source.Path, d.SelectedPath) {
			if root != nil {
				return false
			}
			root = r
		}
	}
	if root == nil || !storageEnrollmentPeakSourceJoins(source, c, *root) {
		return false
	}
	foundControl, foundMarker := false, d.Workflow == 3
	for _, control := range c.OwnerControls {
		if control.Role == 2 && int(control.ParentObject) < len(c.Objects) && c.Objects[control.ParentObject].Persistent == d.Settings && string(control.Name) == dataLocationName && bytes.Equal(control.OwnerKey, root.Source.OwnerKey) && control.Location.Version == 1 && bytes.Equal(control.Location.Path, d.SelectedPath) && (len(control.Location.Previous) == 0 || d.Workflow == 5) && control.Location.Selected == d.Selected {
			foundControl = true
		}
	}
	for _, object := range c.Objects {
		if object.Role == 23 && object.Persistent == d.Marker && int(object.Parent) < len(c.Objects) && c.Objects[object.Parent].Persistent == d.Selected && string(object.Name) == dataMarkerName {
			foundMarker = true
		}
	}
	return foundControl && foundMarker
}

// Only the private parent join projects the action operand away. All board,
// capsule and model comparisons retain the complete original child source.
func storageEnrollmentPrivateParentSource(s storageEnrollmentPeakSource) ([]byte, error) {
	if s.Default != nil || !bytes.Equal(s.Namespace, []byte(".desk-private")) {
		return nil, storagebudget.ErrBounds
	}
	s.ActionPageDigest = storagebudget.Digest{}
	return storageEnrollmentPeakEncodeSource(s)
}
