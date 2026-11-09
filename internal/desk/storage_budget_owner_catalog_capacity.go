package desk

import (
	"bytes"
	"crypto/sha256"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// These joins describe the catalog's declared complete tuple. They do not
// certify actual enforcement, namespace mapping, quota observations or APFS
// dynamic-inode/counter capabilities; the installed original adapter must.
func (c storageOwnerCatalogV2) validateCapacityRelations() error {
	if c.Version == 3 {
		return c.validateAdvisoryCapacityRelations()
	}
	backings := map[storagebudget.ID]storageOwnerConstraint{}
	containers := map[[16]byte]storageOwnerConstraint{}
	volumes := map[[16]byte]storageOwnerConstraint{}
	fsByID := map[storagebudget.ID]storageOwnerFilesystem{}
	for _, f := range c.Filesystems {
		fsByID[f.Definition.ID] = f
	}
	for _, v := range c.Constraints {
		switch v.Kind {
		case 1:
			f, ok := fsByID[v.Pool]
			if !ok || f.Backend != 1 || v.VolumeUUID != f.VolumeUUID || v.ContainerUUID != ([16]byte{}) || v.Definition.Resources != 3 || backings[v.Pool].Definition.ID != (storagebudget.ID{}) {
				return storagebudget.ErrBounds
			}
			backings[v.Pool] = v
		case 2, 3, 4:
			f, ok := fsByID[v.Pool]
			if !ok || f.Backend != 1 || v.VolumeUUID != f.VolumeUUID || v.ContainerUUID != ([16]byte{}) || v.Definition.Resources != 3 {
				return storagebudget.ErrBounds
			}
		case 5:
			if v.Pool != v.Definition.ID || v.VolumeUUID != ([16]byte{}) || v.ContainerUUID == ([16]byte{}) || v.Definition.Resources != 1 || containers[v.ContainerUUID].Definition.ID != (storagebudget.ID{}) {
				return storagebudget.ErrBounds
			}
			containers[v.ContainerUUID] = v
		case 6:
			if v.VolumeUUID == ([16]byte{}) || v.ContainerUUID == ([16]byte{}) || v.Definition.Resources != 1 || volumes[v.VolumeUUID].Definition.ID != (storagebudget.ID{}) {
				return storagebudget.ErrBounds
			}
			volumes[v.VolumeUUID] = v
		}
	}
	// Multiple quota rows may belong to the same pool/class for distinct
	// subjects; a duplicate declared subject relation cannot create a new pool.
	for i, v := range c.Constraints {
		if v.Kind < 2 || v.Kind > 4 {
			continue
		}
		for _, p := range c.Constraints[:i] {
			if p.Kind == v.Kind && p.Pool == v.Pool && p.Subject.Subject == v.Subject.Subject && p.Subject.NamespaceMap == v.Subject.NamespaceMap {
				return storagebudget.ErrBounds
			}
		}
	}
	for _, v := range c.Constraints {
		if v.Kind == 6 {
			p, ok := containers[v.ContainerUUID]
			if !ok || v.Pool != p.Definition.ID {
				return storagebudget.ErrBounds
			}
		}
	}
	for _, f := range c.Filesystems {
		if f.Backend == 1 {
			b, ok := backings[f.Definition.ID]
			if !ok || f.Definition.InodeModel != storagebudget.InodesFinite || f.Definition.ByteConstraint != b.Definition.ID || f.Definition.InodeConstraint != b.Definition.ID {
				return storagebudget.ErrBounds
			}
		} else {
			b, bok := containers[f.ContainerUUID]
			v, vok := volumes[f.VolumeUUID]
			if !bok || !vok || v.ContainerUUID != f.ContainerUUID || v.Pool != b.Definition.ID || f.Definition.InodeModel != storagebudget.InodesDynamic || f.Definition.InodeConstraint != (storagebudget.ID{}) || f.Definition.ByteConstraint != b.Definition.ID {
				return storagebudget.ErrBounds
			}
		}
	}
	for _, r := range c.Roots {
		m := r.Membership
		f := fsByID[r.Definition.Filesystem]
		want := map[storagebudget.ID]bool{}
		if m.Backend == 1 {
			want[backings[f.Definition.ID].Definition.ID] = true
			for _, s := range []storageOwnerSubject{m.User, m.Group, m.Project} {
				if s.State == 3 {
					want[s.Constraint] = true
				}
			}
		} else {
			b := containers[m.ContainerUUID]
			v := volumes[m.VolumeUUID]
			if m.Pool != b.Definition.ID {
				return storagebudget.ErrBounds
			}
			want[b.Definition.ID] = true
			want[v.Definition.ID] = true
		}
		if int(r.Definition.CapacityCount) != len(want) {
			return storagebudget.ErrBounds
		}
		for i := 0; i < int(r.Definition.CapacityCount); i++ {
			if !want[r.Definition.CapacityRefs[i].Constraint] {
				return storagebudget.ErrBounds
			}
		}
		for _, idx := range m.ExistingObjects {
			o := c.Objects[idx]
			if o.Persistent.Backend != m.Backend || o.Persistent.VolumeUUID != m.VolumeUUID {
				return storagebudget.ErrBounds
			}
		}
	}
	return nil
}

func (c storageOwnerCatalogV2) validateUses() error { return c.validateUsesForDefault(false) }

// The Peak-v2 board independently proves the one absent-default control
// addition. Legacy catalog validation keeps its exact orphan-control refusal.
func (c storageOwnerCatalogV2) validateUsesForDefault(defaultHistory bool) error {
	for i, v := range c.OwnerControls {
		if v.Role != 2 {
			continue
		}
		used := false
		for _, r := range c.Roots {
			used = used || r.Role == 4 && r.Source.ControlKind == 2 && r.Source.ControlIndex == uint32(i)
			if defaultHistory && r.Role == 4 && r.Source.Kind == 4 && r.Source.ControlKind == 0 && int(r.DirectoryObject) < len(c.Objects) && c.Objects[r.DirectoryObject].Persistent == v.Location.Selected && bytes.Equal(r.Source.Path, v.Location.Path) && bytes.Equal(r.Source.OwnerKey, v.OwnerKey) && len(v.Location.Previous) == 0 {
				used = true
			}
			if defaultHistory && r.Role == 1 && r.Source.Kind == 1 && r.Source.ControlKind == 0 && int(r.DirectoryObject) < len(c.Objects) && c.Objects[r.DirectoryObject].Persistent == v.Location.Selected && bytes.Equal(r.Source.Path, c.OriginalOwner.ConfigPath) && bytes.Equal(r.Source.Path, v.Location.Path) && bytes.Equal(r.Source.OwnerKey, v.OwnerKey) && len(v.Location.Previous) == 0 {
				used = true
			}
		}
		if !used {
			return storagebudget.ErrBounds
		}
	}
	for i, o := range c.Objects {
		if o.Role != 23 && o.Role != 24 {
			continue
		}
		used := false
		for _, r := range c.Roots {
			if o.Role == 24 {
				used = used || r.Role == 5 && r.Source.ControlKind == 1 && r.Source.ControlIndex == uint32(i) && o.Parent == r.DirectoryObject
			} else {
				used = used || (r.Role == 3 || r.Role == 6) && r.Source.ControlKind == 1 && r.Source.ControlIndex == uint32(i) && o.Parent == r.DirectoryObject
				// A history root's source-control slot may already hold its
				// replaceable data pointer. Its exact fixed marker child remains
				// separately checked by that actual data owner's raw codec.
				used = used || r.Role == 4 && (r.Source.ControlKind == 0 || r.Source.ControlKind == 2) && o.Parent == r.DirectoryObject
			}
		}
		if !used {
			return storagebudget.ErrBounds
		}
	}
	return nil
}

const storageAdvisoryAvailabilityDomain = "desk-storage-advisory-availability-v1"

// This row is an installation policy axis, not a physical pool. Counter
// headroom is applied exactly once while deriving its observation.
func storageOwnerAdvisoryConstraint(installation storagebudget.ID) storageOwnerConstraint {
	h := sha256.New()
	h.Write([]byte(storageAdvisoryAvailabilityDomain))
	h.Write([]byte{0})
	h.Write(installation[:])
	h.Write(storageAdvisoryPolicyMaterial())
	var identity storagebudget.Digest
	copy(identity[:], h.Sum(nil))
	return storageOwnerConstraint{Definition: storagebudget.CapacityConstraint{
		ID: storageDerivedID(storageAdvisoryAvailabilityDomain, installation, nil), Identity: identity,
		Generation: 1, Resources: storagebudget.CapacityBytes | storagebudget.CapacityInodes}, Kind: 7, Pool: installation}
}

func (c storageOwnerCatalogV2) validateAdvisoryCapacityRelations() error {
	policy := storageOwnerAdvisoryConstraint(c.Installation)
	policyCount := 0
	fsByID := map[storagebudget.ID]storageOwnerFilesystem{}
	containers := map[[16]byte]storageOwnerConstraint{}
	volumes := map[[16]byte]storageOwnerConstraint{}
	profiles := map[uint16]storageOwnerProfile{}
	for _, p := range c.Profiles {
		profiles[p.ID] = p
	}
	for _, f := range c.Filesystems {
		fsByID[f.Definition.ID] = f
	}
	for i, v := range c.Constraints {
		switch v.Kind {
		case 7:
			policyCount++
			if v != policy {
				return storagebudget.ErrBounds
			}
		case 2, 3, 4:
			f, ok := fsByID[v.Pool]
			if !ok || f.Backend != 1 || v.VolumeUUID != f.VolumeUUID || v.ContainerUUID != ([16]byte{}) || v.Definition.Resources != 3 {
				return storagebudget.ErrBounds
			}
			for _, prior := range c.Constraints[:i] {
				if prior.Kind == v.Kind && prior.Pool == v.Pool && prior.Subject.Subject == v.Subject.Subject && prior.Subject.NamespaceMap == v.Subject.NamespaceMap {
					return storagebudget.ErrBounds
				}
			}
		case 5:
			if v.Pool != v.Definition.ID || v.VolumeUUID != ([16]byte{}) || v.ContainerUUID == ([16]byte{}) || v.Definition.Resources != 1 || containers[v.ContainerUUID].Definition.ID != (storagebudget.ID{}) {
				return storagebudget.ErrBounds
			}
			containers[v.ContainerUUID] = v
		case 6:
			if v.VolumeUUID == ([16]byte{}) || v.ContainerUUID == ([16]byte{}) || v.Definition.Resources != 1 || volumes[v.VolumeUUID].Definition.ID != (storagebudget.ID{}) {
				return storagebudget.ErrBounds
			}
			volumes[v.VolumeUUID] = v
		default:
			return storagebudget.ErrBounds
		}
	}
	if policyCount != 1 || c.Policy.ObservationMaxAgeNanos > storageAdvisoryInitialMaxAgeNS {
		return storagebudget.ErrBounds
	}
	for _, v := range volumes {
		container, ok := containers[v.ContainerUUID]
		if !ok || v.Pool != container.Definition.ID {
			return storagebudget.ErrBounds
		}
	}
	for _, f := range c.Filesystems {
		d := f.Definition
		if d.ByteConstraint != policy.Definition.ID {
			return storagebudget.ErrBounds
		}
		switch d.InodeModel {
		case storagebudget.InodesFinite:
			if d.InodeConstraint != policy.Definition.ID {
				return storagebudget.ErrBounds
			}
		case storagebudget.InodesDynamic:
			if d.InodeConstraint != (storagebudget.ID{}) {
				return storagebudget.ErrBounds
			}
		default:
			return storagebudget.ErrBounds
		}
		// The closed profiles identify concrete ext4 finite / APFS dynamic routes.
		if f.Backend == 1 && d.InodeModel != storagebudget.InodesFinite || f.Backend == 2 && d.InodeModel != storagebudget.InodesDynamic {
			return storagebudget.ErrBounds
		}
	}
	for _, r := range c.Roots {
		m := r.Membership
		f := fsByID[r.Definition.Filesystem]
		if m.Pool != f.Definition.ID {
			return storagebudget.ErrBounds
		}
		want := map[storagebudget.ID]bool{policy.Definition.ID: true}
		if m.Backend == 1 {
			for _, subject := range []storageOwnerSubject{m.User, m.Group, m.Project} {
				if subject.State == 3 {
					want[subject.Constraint] = true
				}
			}
		} else {
			if b, ok := containers[m.ContainerUUID]; ok {
				want[b.Definition.ID] = true
			}
			if v, ok := volumes[m.VolumeUUID]; ok {
				if v.ContainerUUID != m.ContainerUUID {
					return storagebudget.ErrBounds
				}
				want[v.Definition.ID] = true
			}
			if len(want) > 1 {
				p := profiles[m.ProfileID]
				if p.MappingEvidence == (storagebudget.Digest{}) || p.QuotaEvidence == (storagebudget.Digest{}) {
					return storagebudget.ErrBounds
				}
			}
		}
		if int(r.Definition.CapacityCount) != len(want) {
			return storagebudget.ErrBounds
		}
		for i := 0; i < int(r.Definition.CapacityCount); i++ {
			if !want[r.Definition.CapacityRefs[i].Constraint] {
				return storagebudget.ErrBounds
			}
		}
		for _, idx := range m.ExistingObjects {
			object := c.Objects[idx]
			if object.Persistent.Backend != m.Backend || object.Persistent.VolumeUUID != m.VolumeUUID {
				return storagebudget.ErrBounds
			}
		}
	}
	// Every optional native APFS row must join an actual declared filesystem.
	for uuid := range containers {
		found := false
		for _, f := range c.Filesystems {
			found = found || f.Backend == 2 && f.ContainerUUID == uuid
		}
		if !found {
			return storagebudget.ErrBounds
		}
	}
	for uuid, v := range volumes {
		found := false
		for _, f := range c.Filesystems {
			found = found || f.Backend == 2 && f.VolumeUUID == uuid && f.ContainerUUID == v.ContainerUUID
		}
		if !found {
			return storagebudget.ErrBounds
		}
	}
	return nil
}
