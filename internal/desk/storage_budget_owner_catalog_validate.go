package desk

import (
	"bytes"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageOwnerLimitsValid(l storagebudget.Limits) bool {
	valid := func(v storagebudget.Vector) bool {
		return v.Bytes > 0 && v.Bytes <= 1<<60 && v.Files > 0 && v.Files <= 1<<40 && v.Directories > 0 && v.Directories <= 1<<40
	}
	if !valid(l.Total) {
		return false
	}
	for _, v := range l.Category {
		if !valid(v) {
			return false
		}
	}
	return true
}
func (p storageOwnerPolicy) valid() bool {
	if !storageOwnerLimitsValid(p.Installation) || !storageOwnerLimitsValid(p.Workspace) || p.ObservationMaxAgeNanos == 0 || p.ObservationMaxAgeNanos > 3600000000000 {
		return false
	}
	for _, v := range []storagebudget.Vector{p.Installation.Total, p.Installation.Category[storagebudget.Control]} {
		if v.Bytes < storagebudget.MetadataBudget || v.Files < 16 || v.Directories < 1 {
			return false
		}
	}
	return true
}
func storageOwnerSubjectValid(s storageOwnerSubject, kind uint8) bool {
	return s.State >= 1 && s.State <= 3 && s.Kind == kind && ((s.State == 3) == (s.Constraint != (storagebudget.ID{})))
}
func (c storageOwnerCatalogV2) validate(record storageOwnerRecordV2, claim storageOwnerClaimV2) error {
	return c.validateVersion(record, claim, 2)
}
func (c storageOwnerCatalogV2) validateVersion(record storageOwnerRecordV2, claim storageOwnerClaimV2, version uint32) error {
	if (version != 2 && version != 3) || !c.bounded() || !claim.matches(record) || c.Version != version || c.Installation != record.Installation || c.QBinding != record.QBinding || c.Sequence == 0 || c.Phase != "prepared" && c.Phase != "active" || c.Transition == (storagebudget.ID{}) || c.ExpectedQRevision == 0 || !c.Policy.valid() {
		return storagebudget.ErrBounds
	}
	if len(c.Objects)+len(c.OwnerControls) > 8192 || len(c.Roots) > 4096 || len(c.Workspaces) > 1024 || len(c.Filesystems) > 128 || len(c.Constraints) > 512 || len(c.Profiles) > 64 || len(c.OriginalOwner.Entries) > 1024 {
		return storagebudget.ErrBounds
	}
	if c.Sequence == 1 {
		if c.Phase != "prepared" || c.PreviousCatalogDigest != (storagebudget.Digest{}) || len(c.BeforeDefinitionIDs.Roots)+len(c.BeforeDefinitionIDs.Workspaces)+len(c.BeforeDefinitionIDs.Filesystems)+len(c.BeforeDefinitionIDs.Constraints) != 0 {
			return storagebudget.ErrBounds
		}
	} else if c.PreviousCatalogDigest == (storagebudget.Digest{}) {
		return storagebudget.ErrBounds
	}
	if err := c.validateOriginal(record); err != nil {
		return err
	}
	if err := c.validateObjects(record); err != nil {
		return err
	}
	if err := c.validateControls(); err != nil {
		return err
	}
	if err := c.validateTables(record); err != nil {
		return err
	}
	if err := c.validateRoots(); err != nil {
		return err
	}
	if err := c.validateCapacityRelations(); err != nil {
		return err
	}
	if err := c.validateUses(); err != nil {
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
	return nil
}

func (c storageOwnerCatalogV2) validateTables(record storageOwnerRecordV2) error {
	profiles := map[uint16]storageOwnerProfile{}
	for i, p := range c.Profiles {
		if p.ID == 0 || p.Version == 0 || p.Backend < 1 || p.Backend > 2 || i > 0 && c.Profiles[i-1].ID >= p.ID {
			return storagebudget.ErrBounds
		}
		d, err := p.digest()
		if err != nil || d != p.Evidence {
			return storagebudget.ErrBounds
		}
		if c.Version == 3 && !storageOwnerAdvisoryProfileValid(p) {
			return storagebudget.ErrBounds
		}
		profiles[p.ID] = p
	}
	d, err := storageOwnerDigest("desk-storage-owner-profile-set-v2", storageOwnerProfileSetMaterial{c.Profiles})
	if err != nil || d != record.ProfileSetDigest {
		return storagebudget.ErrBounds
	}
	for i, w := range c.Workspaces {
		if w.Definition.ID == (storagebudget.ID{}) || w.Definition.Generation == 0 || !storageOwnerLimitsValid(w.Definition.Limits) || i > 0 && bytes.Compare(c.Workspaces[i-1].Definition.ID[:], w.Definition.ID[:]) >= 0 || w.OwnerKind < 1 || w.OwnerKind > 3 || len(w.OwnerKey) == 0 || len(w.OwnerKey) > 4096 || len(w.HistoryIdentity) == 0 || len(w.HistoryIdentity) > 4096 {
			return storagebudget.ErrBounds
		}
		if !c.workspaceRelation(w) {
			return storagebudget.ErrBounds
		}
	}
	identities := map[storagebudget.Digest]bool{}
	for i, v := range c.Constraints {
		d := v.Definition
		if d.ID == (storagebudget.ID{}) || d.Identity == (storagebudget.Digest{}) || identities[d.Identity] || d.Generation == 0 || d.Resources == 0 || d.Resources&^uint8(3) != 0 || d.Headroom.Bytes > 1<<60 || d.Headroom.Inodes > 1<<40 || d.Resources&1 == 0 && d.Headroom.Bytes != 0 || d.Resources&2 == 0 && d.Headroom.Inodes != 0 || i > 0 && bytes.Compare(c.Constraints[i-1].Definition.ID[:], d.ID[:]) >= 0 || v.Kind < 1 || v.Kind > 6 && (c.Version != 3 || v.Kind != 7) || v.Pool == (storagebudget.ID{}) {
			return storagebudget.ErrBounds
		}
		identities[d.Identity] = true
		if v.Kind >= 2 && v.Kind <= 4 {
			if !storageOwnerSubjectValid(v.Subject, v.Kind-1) || v.Subject.State != 3 || v.Subject.Constraint != d.ID {
				return storagebudget.ErrBounds
			}
		} else if v.Subject != (storageOwnerSubject{}) {
			return storagebudget.ErrBounds
		}
	}
	for i, f := range c.Filesystems {
		d := f.Definition
		if d.ID == (storagebudget.ID{}) || d.Identity == (storagebudget.Digest{}) || d.Generation == 0 || d.CapabilityEvidence == (storagebudget.Digest{}) || f.Backend < 1 || f.Backend > 2 || f.VolumeUUID == ([16]byte{}) || i > 0 && bytes.Compare(c.Filesystems[i-1].Definition.ID[:], d.ID[:]) >= 0 {
			return storagebudget.ErrBounds
		}
		if f.Backend == 1 && f.ContainerUUID != ([16]byte{}) || c.Version == 2 && f.Backend == 2 && f.ContainerUUID == ([16]byte{}) {
			return storagebudget.ErrBounds
		}
		p, pok := profiles[f.PersistentProfile]
		n, nok := profiles[f.InodeProfile]
		if !pok || !nok || p.Backend != f.Backend || n.Backend != f.Backend || d.CapabilityEvidence != n.InodeEvidence {
			return storagebudget.ErrBounds
		}
		for _, prior := range c.Filesystems[:i] {
			if prior.Backend == f.Backend && prior.VolumeUUID == f.VolumeUUID {
				return storagebudget.ErrBounds
			}
		}
	}
	return nil
}

func storageOwnerHistoryIdentity(b []byte) bool {
	return validProjectHistoryPath(string(b)) || len(b) == 37 && string(b[:5]) == "desk:" && storageOwnerHex(string(b[5:]), 32)
}
func (c storageOwnerCatalogV2) workspaceRelation(w storageOwnerWorkspace) bool {
	key := string(w.OwnerKey)
	switch w.OwnerKind {
	case 1:
		return storageOwnerHex(key, 64) && validProjectHistoryPath(string(w.HistoryIdentity))
	case 2:
		if !storageOwnerHex(key, 32) {
			return false
		}
		if c.OriginalOwner.Kind == 1 {
			return string(w.HistoryIdentity) == "desk:"+key
		}
		for _, h := range c.OriginalOwner.Entries {
			if h.DeskID == key {
				return bytes.Equal(w.HistoryIdentity, h.HistoryIdentity)
			}
		}
		return false
	case 3:
		return len(w.OwnerKey) <= 256 && !bytes.ContainsAny(w.OwnerKey, "\x00\r\n") && storageOwnerHistoryIdentity(w.HistoryIdentity)
	}
	return false
}

func (c storageOwnerCatalogV2) validateRoots() error {
	rootIDs := map[storagebudget.ID]int{}
	workspaceIDs := map[storagebudget.ID]bool{}
	fsIDs := map[storagebudget.ID]storageOwnerFilesystem{}
	constraints := map[storagebudget.ID]storageOwnerConstraint{}
	profiles := map[uint16]storageOwnerProfile{}
	constraintDTOs := make([]storagebudget.CapacityConstraint, 0, len(c.Constraints))
	for _, w := range c.Workspaces {
		workspaceIDs[w.Definition.ID] = true
	}
	for _, f := range c.Filesystems {
		fsIDs[f.Definition.ID] = f
	}
	for _, v := range c.Constraints {
		constraints[v.Definition.ID] = v
		constraintDTOs = append(constraintDTOs, v.Definition)
	}
	for _, p := range c.Profiles {
		profiles[p.ID] = p
	}
	directories := map[uint32]bool{}
	boundaries := map[uint32]int{}
	// A selected source boundary has one original selector, independently of
	// its persistent object and Q root ID. Both strings preserve exact raw
	// comparison bytes; neither is interpreted as a resolver or authority.
	type boundarySelector struct {
		Kind     uint8
		OwnerKey string
		Path     string
	}
	selectors := map[boundarySelector]bool{}
	identities := map[storagebudget.Digest]bool{}
	roles := [7]int{}
	totalRefs := 0
	for i, r := range c.Roots {
		d := r.Definition
		if d.ID == (storagebudget.ID{}) || d.Owner != c.QBinding || d.Identity == (storagebudget.Digest{}) || identities[d.Identity] || d.Generation == 0 || !storageOwnerText(d.Locator, 256, false) || i > 0 && bytes.Compare(c.Roots[i-1].Definition.ID[:], d.ID[:]) >= 0 || r.Role < 1 || r.Role > 6 || r.Category >= 9 || int(r.DirectoryObject) >= len(c.Objects) || directories[r.DirectoryObject] || c.Objects[r.DirectoryObject].ModeClass != 1 || len(r.References) > 1024 || !storageOwnerSortedIDs(r.References) || d.CapacityCount == 0 || d.CapacityCount > 4 {
			return storagebudget.ErrBounds
		}
		rootIDs[d.ID] = i
		directories[r.DirectoryObject] = true
		identities[d.Identity] = true
		roles[r.Role]++
		totalRefs += len(r.References)
		if totalRefs > 65536 {
			return storagebudget.ErrBounds
		}
		if d.Workspace != (storagebudget.ID{}) && !workspaceIDs[d.Workspace] {
			return storagebudget.ErrBounds
		}
		for _, id := range r.References {
			if !workspaceIDs[id] {
				return storagebudget.ErrBounds
			}
		}
		f, ok := fsIDs[d.Filesystem]
		if !ok {
			return storagebudget.ErrBounds
		}
		m := r.Membership
		p, ok := profiles[m.ProfileID]
		if !ok || m.ProfileVersion != p.Version || m.Backend != p.Backend || m.ProfileEvidence != p.Evidence || m.Backend != f.Backend || m.VolumeUUID != f.VolumeUUID || m.ContainerUUID != f.ContainerUUID || m.Creation < 1 || m.Creation > 2 || len(m.ExistingObjects) > 8192 || m.Pool == (storagebudget.ID{}) {
			return storagebudget.ErrBounds
		}
		if c.Objects[r.DirectoryObject].Persistent.Backend != m.Backend || c.Objects[r.DirectoryObject].Persistent.VolumeUUID != m.VolumeUUID {
			return storagebudget.ErrBounds
		}
		fp := profiles[f.PersistentProfile]
		ip := profiles[f.InodeProfile]
		if fp.PersistentEvidence != p.PersistentEvidence || ip.InodeEvidence != p.InodeEvidence || f.Definition.CapabilityEvidence != p.InodeEvidence {
			return storagebudget.ErrBounds
		}
		if m.Backend == 1 {
			if m.ContainerUUID != ([16]byte{}) || m.Pool != f.Definition.ID || !c.subjectValid(m.User, 1) || !c.subjectValid(m.Group, 2) || !c.subjectValid(m.Project, 3) {
				return storagebudget.ErrBounds
			}
		} else if c.Version == 3 && m.Pool != f.Definition.ID {
			return storagebudget.ErrBounds
		} else if m.User != (storageOwnerSubject{}) || m.Group != (storageOwnerSubject{}) || m.Project != (storageOwnerSubject{}) {
			return storagebudget.ErrBounds
		}
		if c.Version == 3 && m.Backend == 1 {
			for _, subject := range []storageOwnerSubject{m.User, m.Group, m.Project} {
				if subject.State != 4 && (p.MappingEvidence == (storagebudget.Digest{}) || p.QuotaEvidence == (storagebudget.Digest{})) {
					return storagebudget.ErrBounds
				}
			}
		}
		for j, idx := range m.ExistingObjects {
			if int(idx) >= len(c.Objects) || j > 0 && m.ExistingObjects[j-1] >= idx || c.Objects[idx].ModeClass != 2 {
				return storagebudget.ErrBounds
			}
		}
		for _, subject := range []storageOwnerSubject{m.User, m.Group, m.Project} {
			if subject.State == 3 {
				v, ok := constraints[subject.Constraint]
				if !ok || v.Kind != subject.Kind+1 || v.Subject != subject || v.Pool != m.Pool || v.VolumeUUID != m.VolumeUUID || v.ContainerUUID != m.ContainerUUID {
					return storagebudget.ErrBounds
				}
				found := false
				for j := 0; j < int(d.CapacityCount); j++ {
					found = found || d.CapacityRefs[j].Constraint == subject.Constraint
				}
				if !found {
					return storagebudget.ErrBounds
				}
			}
		}
		if err := c.validateRootRole(r); err != nil {
			return err
		}
		if c.Objects[r.DirectoryObject].Source == 3 && c.Objects[r.DirectoryObject].Parent == storageOwnerNoObject {
			key := boundarySelector{r.Source.Kind, string(r.Source.OwnerKey), string(r.Source.Path)}
			if selectors[key] {
				return storagebudget.ErrBounds
			}
			selectors[key] = true
			boundaries[r.DirectoryObject]++
		}
		identity, err := c.rootIdentity(r)
		if err != nil || identity != d.Identity {
			return storagebudget.ErrBounds
		}
		root := storagebudget.Root{ID: d.ID, Owner: d.Owner, Identity: d.Identity, Filesystem: d.Filesystem, Workspace: d.Workspace, Generation: d.Generation, Locator: d.Locator, CapacityCount: d.CapacityCount, CapacityRefs: d.CapacityRefs, CapacityBinding: d.CapacityBinding}
		binding, err := storagebudget.CapacityBindingFor(root, f.Definition, constraintDTOs)
		if err != nil || binding != d.CapacityBinding {
			return storagebudget.ErrBounds
		}
	}
	if roles[1] != 1 || roles[2] != 1 {
		return storagebudget.ErrBounds
	}
	for i, o := range c.Objects {
		if o.Source == 3 && o.Parent == storageOwnerNoObject && boundaries[uint32(i)] != 1 {
			return storagebudget.ErrBounds
		}
	}
	for _, r := range c.Roots {
		if err := c.validateRootGraph(r, rootIDs); err != nil {
			return err
		}
	}
	return nil
}

func (c storageOwnerCatalogV2) validateRootRole(r storageOwnerRoot) error {
	s := r.Source
	o := c.Objects[r.DirectoryObject]
	if s.Kind != r.Role || len(s.OwnerKey) == 0 || len(s.OwnerKey) > 4096 || !storageOwnerAbsolute(s.Path) || !storageOwnerRelative(s.Relative) || s.ControlKind > 2 {
		return storagebudget.ErrBounds
	}
	for _, idx := range []uint32{r.DomainObject, r.WorkObject} {
		if idx != storageOwnerNoObject && int(idx) >= len(c.Objects) {
			return storagebudget.ErrBounds
		}
	}
	domainRole := uint8(0)
	if r.DomainObject != storageOwnerNoObject {
		domainRole = c.Objects[r.DomainObject].Role
	}
	if r.WorkObject != storageOwnerNoObject && c.Objects[r.WorkObject].Role != 19 {
		return storagebudget.ErrBounds
	}
	if s.ControlKind == 0 {
		if s.ControlIndex != storageOwnerNoControl || s.ControlSelection != 0 {
			return storagebudget.ErrBounds
		}
	} else if s.ControlKind == 1 {
		if int(s.ControlIndex) >= len(c.Objects) || s.ControlSelection != 0 {
			return storagebudget.ErrBounds
		}
	} else {
		if int(s.ControlIndex) >= len(c.OwnerControls) || s.ControlSelection < 1 || s.ControlSelection > 3 {
			return storagebudget.ErrBounds
		}
		v := c.OwnerControls[s.ControlIndex]
		if v.Role != 2 || !bytes.Equal(v.OwnerKey, s.OwnerKey) {
			return storagebudget.ErrBounds
		}
		current := bytes.Equal(s.Path, v.Location.Path) && o.Persistent == v.Location.Selected
		previous := len(v.Location.Previous) > 0 && bytes.Equal(s.Path, v.Location.Previous) && o.Persistent == v.Location.PreviousSelected
		if s.ControlSelection == 1 && !current || s.ControlSelection == 2 && !previous {
			return storagebudget.ErrBounds
		}
		if s.ControlSelection == 3 {
			old := false
			for _, id := range c.BeforeDefinitionIDs.Roots {
				old = old || id == r.Definition.ID
			}
			if !old || current || previous {
				return storagebudget.ErrBounds
			}
		}
	}
	validPrivateControl := s.ControlKind == 0 || s.ControlKind == 1 && c.Objects[s.ControlIndex].Role == 23 && c.Objects[s.ControlIndex].Parent == r.DirectoryObject
	switch r.Role {
	case 1:
		if o.Role != 1 || r.Category != 4 || domainRole != 18 || s.ControlKind != 0 || string(s.OwnerKey) != c.OriginalOwner.HomeOwner || !bytes.Equal(s.Path, c.OriginalOwner.ConfigPath) || s.ParentRoot != (storagebudget.ID{}) || len(s.Relative) != 0 {
			return storagebudget.ErrBounds
		}
	case 2:
		path := append(append([]byte{}, c.OriginalOwner.ConfigPath...), []byte("/storage-budget")...)
		if bytes.Equal(c.OriginalOwner.ConfigPath, []byte("/")) {
			path = []byte("/storage-budget")
		}
		if o.Role != 2 || r.Category != 4 || r.DomainObject != storageOwnerNoObject || r.WorkObject != storageOwnerNoObject || s.ControlKind != 0 || string(s.OwnerKey) != c.OriginalOwner.HomeOwner || !bytes.Equal(s.Path, path) || string(s.Relative) != "storage-budget" {
			return storagebudget.ErrBounds
		}
	case 3, 6:
		if o.Role != 20 || !(domainRole == 18 || r.DomainObject == r.DirectoryObject) || !validPrivateControl {
			return storagebudget.ErrBounds
		}
		if r.Role == 3 {
			if !storageOwnerHex(string(s.OwnerKey), 32) {
				return storagebudget.ErrBounds
			}
			if c.OriginalOwner.Kind == 2 {
				found := false
				for _, h := range c.OriginalOwner.Entries {
					if h.DeskID == string(s.OwnerKey) {
						found = true
					}
				}
				if !found {
					return storagebudget.ErrBounds
				}
			}
		}
	case 4:
		if o.Role != 20 && o.Role != 21 || r.Category != 0 || domainRole != 18 || s.ControlKind != 0 && s.ControlKind != 2 || !storageOwnerIdentityString(string(s.OwnerKey)) {
			return storagebudget.ErrBounds
		}
	case 5:
		if o.Role != 22 || r.Category != 1 || r.DomainObject != r.DirectoryObject || r.WorkObject != storageOwnerNoObject || s.ControlKind != 1 || c.Objects[s.ControlIndex].Role != 24 || c.Objects[s.ControlIndex].Parent != r.DirectoryObject || !storageOwnerHex(string(s.OwnerKey), 64) {
			return storagebudget.ErrBounds
		}
	}
	return nil
}

func (c storageOwnerCatalogV2) validateRootGraph(r storageOwnerRoot, ids map[storagebudget.ID]int) error {
	s := r.Source
	if s.ParentRoot == (storagebudget.ID{}) {
		if c.Objects[r.DirectoryObject].Parent != storageOwnerNoObject {
			return storagebudget.ErrBounds
		}
		return nil
	}
	i, ok := ids[s.ParentRoot]
	if !ok {
		return storagebudget.ErrBounds
	}
	parent := c.Roots[i]
	if r.Role == 2 && parent.Role != 1 {
		return storagebudget.ErrBounds
	}
	if r.Role == 1 {
		return storagebudget.ErrBounds
	}
	seen := map[storagebudget.ID]bool{r.Definition.ID: true}
	next := s.ParentRoot
	for depth := 0; next != (storagebudget.ID{}); depth++ {
		if depth >= 64 || seen[next] {
			return storagebudget.ErrBounds
		}
		seen[next] = true
		j, ok := ids[next]
		if !ok {
			return storagebudget.ErrBounds
		}
		next = c.Roots[j].Source.ParentRoot
	}
	chain := []byte{}
	idx := r.DirectoryObject
	depth := 0
	for idx != parent.DirectoryObject {
		if depth >= 64 || idx == storageOwnerNoObject {
			return storagebudget.ErrBounds
		}
		o := c.Objects[idx]
		if o.Parent == storageOwnerNoObject {
			return storagebudget.ErrBounds
		}
		if len(chain) > 0 {
			chain = append(append(append([]byte{}, o.Name...), '/'), chain...)
		} else {
			chain = append([]byte{}, o.Name...)
		}
		idx = o.Parent
		depth++
	}
	if !bytes.Equal(s.Relative, chain) || len(chain) == 0 {
		return storagebudget.ErrBounds
	}
	path := append([]byte{}, parent.Source.Path...)
	if !bytes.Equal(path, []byte("/")) {
		path = append(path, '/')
	}
	path = append(path, chain...)
	if !bytes.Equal(s.Path, path) {
		return storagebudget.ErrBounds
	}
	return nil
}

// Unknown is an absent fact, never a positive subject-zero or disabled claim.
func (c storageOwnerCatalogV2) subjectValid(s storageOwnerSubject, kind uint8) bool {
	if c.Version == 3 && s.State == 4 {
		return s.Kind == kind && s.Subject == 0 && s.NamespaceMap == (storagebudget.Digest{}) && s.Constraint == (storagebudget.ID{})
	}
	return storageOwnerSubjectValid(s, kind)
}

// Recognition here is detached structure only, not installation/native qualification.
func storageOwnerAdvisoryProfileValid(p storageOwnerProfile) bool {
	return (p.ID == 4097 && p.Backend == 1 || p.ID == 4098 && p.Backend == 2) && p.Version == 2 &&
		p.PersistentEvidence != (storagebudget.Digest{}) && p.InodeEvidence != (storagebudget.Digest{}) && p.CounterEvidence != (storagebudget.Digest{}) &&
		p.AllowanceEvidence == storageAdvisoryPolicyEvidence()
}
