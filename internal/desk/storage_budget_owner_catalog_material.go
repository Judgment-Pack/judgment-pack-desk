package desk

import "github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"

// These material DTOs deliberately contain persistent values rather than
// serialized indices. Keep their field order aligned with the closed contract.
type storageOwnerObjectMaterial struct {
	Role        uint8
	Source      uint8
	Parent      storageOwnerPersistentObject
	Name        []byte
	Persistent  storageOwnerPersistentObject
	OwnerClass  uint8
	ModeClass   uint8
	FixedLength uint64
}
type storageOwnerControlMaterial struct {
	Role      uint8
	Parent    storageOwnerPersistentObject
	Name      []byte
	OwnerKind uint8
	OwnerKey  []byte
	Location  storageOwnerLocation
}
type storageOwnerSelectedControlMaterial struct {
	Role         uint8
	Parent       storageOwnerPersistentObject
	Name         []byte
	OwnerKind    uint8
	OwnerKey     []byte
	SelectedPath []byte
	Selected     storageOwnerPersistentObject
}
type storageOwnerSourceMaterial struct {
	Kind               uint8
	OwnerKey           []byte
	Path               []byte
	ParentRoot         storagebudget.ID
	Relative           []byte
	ControlKind        uint8
	FixedControl       storageOwnerObjectMaterial
	ReplaceableControl storageOwnerSelectedControlMaterial
	Home               storageOwnerHomeRelation
}
type storageOwnerMembershipMaterial struct {
	Backend         uint8
	ProfileID       uint16
	ProfileVersion  uint16
	ProfileEvidence storagebudget.Digest
	Pool            storagebudget.ID
	VolumeUUID      [16]byte
	ContainerUUID   [16]byte
	User            storageOwnerSubject
	Group           storageOwnerSubject
	Project         storageOwnerSubject
	Creation        uint8
	ExistingObjects []storageOwnerObjectMaterial
}
type storageOwnerRootMaterial struct {
	ID             storagebudget.ID
	Owner          storagebudget.Digest
	Filesystem     storagebudget.ID
	Generation     uint64
	Role           uint8
	Category       uint8
	Directory      storageOwnerObjectMaterial
	Domain         storageOwnerObjectMaterial
	Work           storageOwnerObjectMaterial
	SourceRelation storagebudget.Digest
	Membership     storageOwnerMembershipMaterial
	FixedControls  []storageOwnerObjectMaterial
}
type storageOwnerProfileMaterial struct {
	ID                 uint16
	Version            uint16
	Backend            uint8
	PersistentEvidence storagebudget.Digest
	MappingEvidence    storagebudget.Digest
	QuotaEvidence      storagebudget.Digest
	InodeEvidence      storagebudget.Digest
	CounterEvidence    storagebudget.Digest
	AllowanceEvidence  storagebudget.Digest
}
type storageOwnerProfileSetMaterial struct{ Profiles []storageOwnerProfile }

func storageOwnerEmptyObjectMaterial() storageOwnerObjectMaterial {
	return storageOwnerObjectMaterial{Name: []byte{}}
}
func (c storageOwnerCatalogV2) objectMaterial(index uint32) storageOwnerObjectMaterial {
	if index == storageOwnerNoObject {
		return storageOwnerEmptyObjectMaterial()
	}
	o := c.Objects[index]
	var parent storageOwnerPersistentObject
	if o.Parent != storageOwnerNoObject {
		parent = c.Objects[o.Parent].Persistent
	}
	return storageOwnerObjectMaterial{o.Role, o.Source, parent, o.Name, o.Persistent, o.OwnerClass, o.ModeClass, o.FixedLength}
}
func (c storageOwnerCatalogV2) controlDigest(v storageOwnerControl) (storagebudget.Digest, error) {
	m := storageOwnerControlMaterial{v.Role, c.Objects[v.ParentObject].Persistent, v.Name, v.OwnerKind, v.OwnerKey, v.Location}
	return storageOwnerDigest("desk-storage-owner-control-relation-v2", m)
}
func (c storageOwnerCatalogV2) sourceDigest(r storageOwnerRoot) (storagebudget.Digest, error) {
	s := r.Source
	m := storageOwnerSourceMaterial{Kind: s.Kind, OwnerKey: s.OwnerKey, Path: s.Path, ParentRoot: s.ParentRoot, Relative: s.Relative, ControlKind: s.ControlKind,
		FixedControl: storageOwnerEmptyObjectMaterial(), ReplaceableControl: storageOwnerSelectedControlMaterial{Name: []byte{}, OwnerKey: []byte{}, SelectedPath: []byte{}},
		Home: storageOwnerHomeRelation{Folder: []byte{}, HistoryIdentity: []byte{}}}
	switch s.ControlKind {
	case 1:
		m.FixedControl = c.objectMaterial(s.ControlIndex)
	case 2:
		v := c.OwnerControls[s.ControlIndex]
		// CATC1: THIS root's immutable selection, never full Location, digest,
		// other branch, ControlSelection or a serialized index.
		m.ReplaceableControl = storageOwnerSelectedControlMaterial{v.Role, c.Objects[v.ParentObject].Persistent, v.Name, v.OwnerKind, v.OwnerKey, s.Path, c.Objects[r.DirectoryObject].Persistent}
	}
	if r.Role == 3 && c.OriginalOwner.Kind == 2 {
		for _, h := range c.OriginalOwner.Entries {
			if h.DeskID == string(s.OwnerKey) {
				m.Home = h
				break
			}
		}
	}
	return storageOwnerDigest("desk-storage-owner-source-relation-v2", m)
}
func (c storageOwnerCatalogV2) rootIdentity(r storageOwnerRoot) (storagebudget.Digest, error) {
	s, err := c.sourceDigest(r)
	if err != nil {
		return storagebudget.Digest{}, err
	}
	v := r.Membership
	mm := storageOwnerMembershipMaterial{v.Backend, v.ProfileID, v.ProfileVersion, v.ProfileEvidence, v.Pool, v.VolumeUUID, v.ContainerUUID, v.User, v.Group, v.Project, v.Creation, []storageOwnerObjectMaterial{}}
	for _, idx := range v.ExistingObjects {
		mm.ExistingObjects = append(mm.ExistingObjects, c.objectMaterial(idx))
	}
	d := r.Definition
	m := storageOwnerRootMaterial{d.ID, d.Owner, d.Filesystem, d.Generation, r.Role, r.Category, c.objectMaterial(r.DirectoryObject), c.objectMaterial(r.DomainObject), c.objectMaterial(r.WorkObject), s, mm, []storageOwnerObjectMaterial{}}
	for i, o := range c.Objects {
		if r.Role == 1 && o.Role >= 4 && o.Role <= 17 || r.Role == 2 && o.Role >= 12 && o.Role <= 17 {
			m.FixedControls = append(m.FixedControls, c.objectMaterial(uint32(i)))
		}
	}
	return storageOwnerDigest("desk-storage-owner-root-identity-v2", m)
}
func (p storageOwnerProfile) digest() (storagebudget.Digest, error) {
	m := storageOwnerProfileMaterial{p.ID, p.Version, p.Backend, p.PersistentEvidence, p.MappingEvidence, p.QuotaEvidence, p.InodeEvidence, p.CounterEvidence, p.AllowanceEvidence}
	return storageOwnerDigest("desk-storage-owner-profile-v2", m)
}
