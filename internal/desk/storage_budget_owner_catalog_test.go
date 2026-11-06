package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// These fixtures are authored data, never recordings from a codec run. The
// material oracle spells every JSON field/order/domain independently below.
func ownerCatalogJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
func ownerCatalogDigest(domain, material string) storagebudget.Digest {
	return sha256.Sum256([]byte(domain + "\x00" + material))
}
func ownerCatalogObject(c storageOwnerCatalogV2, index uint32) string {
	o := storageOwnerObject{Name: []byte{}}
	var parent storageOwnerPersistentObject
	if index != math.MaxUint32 {
		o = c.Objects[index]
		if o.Parent != math.MaxUint32 {
			parent = c.Objects[o.Parent].Persistent
		}
	}
	return fmt.Sprintf(`{"Role":%d,"Source":%d,"Parent":%s,"Name":%s,"Persistent":%s,"OwnerClass":%d,"ModeClass":%d,"FixedLength":%d}`, o.Role, o.Source, ownerCatalogJSON(parent), ownerCatalogJSON(o.Name), ownerCatalogJSON(o.Persistent), o.OwnerClass, o.ModeClass, o.FixedLength)
}
func ownerCatalogSource(c storageOwnerCatalogV2, r storageOwnerRoot) string {
	s := r.Source
	fixed := ownerCatalogObject(c, math.MaxUint32)
	replace := `{"Role":0,"Parent":` + ownerCatalogJSON(storageOwnerPersistentObject{}) + `,"Name":"","OwnerKind":0,"OwnerKey":"","SelectedPath":"","Selected":` + ownerCatalogJSON(storageOwnerPersistentObject{}) + `}`
	if s.ControlKind == 1 {
		fixed = ownerCatalogObject(c, s.ControlIndex)
	}
	if s.ControlKind == 2 {
		v := c.OwnerControls[s.ControlIndex]
		replace = fmt.Sprintf(`{"Role":%d,"Parent":%s,"Name":%s,"OwnerKind":%d,"OwnerKey":%s,"SelectedPath":%s,"Selected":%s}`, v.Role, ownerCatalogJSON(c.Objects[v.ParentObject].Persistent), ownerCatalogJSON(v.Name), v.OwnerKind, ownerCatalogJSON(v.OwnerKey), ownerCatalogJSON(s.Path), ownerCatalogJSON(c.Objects[r.DirectoryObject].Persistent))
	}
	home := `{"DeskID":"","Folder":"","Kind":0,"HistoryIdentity":"","StorageOwner":"","RunnerWorkspace":""}`
	if r.Role == 3 && c.OriginalOwner.Kind == 2 {
		for _, h := range c.OriginalOwner.Entries {
			if h.DeskID == string(s.OwnerKey) {
				home = ownerCatalogJSON(h)
			}
		}
	}
	return fmt.Sprintf(`{"Kind":%d,"OwnerKey":%s,"Path":%s,"ParentRoot":%s,"Relative":%s,"ControlKind":%d,"FixedControl":%s,"ReplaceableControl":%s,"Home":%s}`, s.Kind, ownerCatalogJSON(s.OwnerKey), ownerCatalogJSON(s.Path), ownerCatalogJSON(s.ParentRoot), ownerCatalogJSON(s.Relative), s.ControlKind, fixed, replace, home)
}
func ownerCatalogIdentity(c storageOwnerCatalogV2, r storageOwnerRoot) storagebudget.Digest {
	m := r.Membership
	existing := []string{}
	for _, idx := range m.ExistingObjects {
		existing = append(existing, ownerCatalogObject(c, idx))
	}
	membership := fmt.Sprintf(`{"Backend":%d,"ProfileID":%d,"ProfileVersion":%d,"ProfileEvidence":%s,"Pool":%s,"VolumeUUID":%s,"ContainerUUID":%s,"User":%s,"Group":%s,"Project":%s,"Creation":%d,"ExistingObjects":[%s]}`, m.Backend, m.ProfileID, m.ProfileVersion, ownerCatalogJSON(m.ProfileEvidence), ownerCatalogJSON(m.Pool), ownerCatalogJSON(m.VolumeUUID), ownerCatalogJSON(m.ContainerUUID), ownerCatalogJSON(m.User), ownerCatalogJSON(m.Group), ownerCatalogJSON(m.Project), m.Creation, strings.Join(existing, ","))
	fixed := []string{}
	for i, o := range c.Objects {
		if r.Role == 1 && o.Role >= 4 && o.Role <= 17 || r.Role == 2 && o.Role >= 12 && o.Role <= 17 {
			fixed = append(fixed, ownerCatalogObject(c, uint32(i)))
		}
	}
	d := r.Definition
	source := ownerCatalogDigest("desk-storage-owner-source-relation-v2", ownerCatalogSource(c, r))
	raw := fmt.Sprintf(`{"ID":%s,"Owner":%s,"Filesystem":%s,"Generation":%d,"Role":%d,"Category":%d,"Directory":%s,"Domain":%s,"Work":%s,"SourceRelation":%s,"Membership":%s,"FixedControls":[%s]}`, ownerCatalogJSON(d.ID), ownerCatalogJSON(d.Owner), ownerCatalogJSON(d.Filesystem), d.Generation, r.Role, r.Category, ownerCatalogObject(c, r.DirectoryObject), ownerCatalogObject(c, r.DomainObject), ownerCatalogObject(c, r.WorkObject), ownerCatalogJSON(source), membership, strings.Join(fixed, ","))
	return ownerCatalogDigest("desk-storage-owner-root-identity-v2", raw)
}
func ownerCatalogSeal(c *storageOwnerCatalogV2, record *storageOwnerRecordV2, claim *storageOwnerClaimV2) {
	for i, p := range c.Profiles {
		raw := fmt.Sprintf(`{"ID":%d,"Version":%d,"Backend":%d,"PersistentEvidence":%s,"MappingEvidence":%s,"QuotaEvidence":%s,"InodeEvidence":%s,"CounterEvidence":%s,"AllowanceEvidence":%s}`, p.ID, p.Version, p.Backend, ownerCatalogJSON(p.PersistentEvidence), ownerCatalogJSON(p.MappingEvidence), ownerCatalogJSON(p.QuotaEvidence), ownerCatalogJSON(p.InodeEvidence), ownerCatalogJSON(p.CounterEvidence), ownerCatalogJSON(p.AllowanceEvidence))
		c.Profiles[i].Evidence = ownerCatalogDigest("desk-storage-owner-profile-v2", raw)
	}
	record.ProfileSetDigest = ownerCatalogDigest("desk-storage-owner-profile-set-v2", `{"Profiles":`+ownerCatalogJSON(c.Profiles)+`}`)
	record.BindingMaterialDigest, record.QBinding, _ = storageOwnerRecordDigests(*record)
	claim.ProfileSetDigest = record.ProfileSetDigest
	claim.Config = record.Config
	c.QBinding = record.QBinding
	for i, v := range c.OwnerControls {
		raw := fmt.Sprintf(`{"Role":%d,"Parent":%s,"Name":%s,"OwnerKind":%d,"OwnerKey":%s,"Location":%s}`, v.Role, ownerCatalogJSON(c.Objects[v.ParentObject].Persistent), ownerCatalogJSON(v.Name), v.OwnerKind, ownerCatalogJSON(v.OwnerKey), ownerCatalogJSON(v.Location))
		c.OwnerControls[i].RelationDigest = ownerCatalogDigest("desk-storage-owner-control-relation-v2", raw)
	}
	constraints := []storagebudget.CapacityConstraint{}
	for _, v := range c.Constraints {
		constraints = append(constraints, v.Definition)
	}
	for i := range c.Roots {
		r := &c.Roots[i]
		r.Definition.Owner = c.QBinding
		r.Membership.ProfileEvidence = c.Profiles[0].Evidence
		r.Definition.Identity = ownerCatalogIdentity(*c, *r)
		var f storagebudget.Filesystem
		for _, v := range c.Filesystems {
			if v.Definition.ID == r.Definition.Filesystem {
				f = v.Definition
			}
		}
		d := r.Definition
		r.Definition.CapacityBinding, _ = storagebudget.CapacityBindingFor(storagebudget.Root{ID: d.ID, Owner: d.Owner, Identity: d.Identity, Filesystem: d.Filesystem, Generation: d.Generation, CapacityCount: d.CapacityCount, CapacityRefs: d.CapacityRefs}, f, constraints)
	}
	// Exact old/new definitions are assembled independently, without calling
	// the production projection. These lists contain immutable rows only.
	rootRows, workspaceRows, fsRows, constraintRows := []string{}, []string{}, []string{}, []string{}
	oldRoots, oldWorkspaces, oldFS, oldConstraints := []string{}, []string{}, []string{}, []string{}
	has := func(ids []storagebudget.ID, id storagebudget.ID) bool {
		for _, v := range ids {
			if v == id {
				return true
			}
		}
		return false
	}
	for _, v := range c.Roots {
		s := ownerCatalogJSON(v.Definition)
		rootRows = append(rootRows, s)
		if has(c.BeforeDefinitionIDs.Roots, v.Definition.ID) {
			oldRoots = append(oldRoots, s)
		}
	}
	for _, v := range c.Workspaces {
		s := ownerCatalogJSON(v.Definition)
		workspaceRows = append(workspaceRows, s)
		if has(c.BeforeDefinitionIDs.Workspaces, v.Definition.ID) {
			oldWorkspaces = append(oldWorkspaces, s)
		}
	}
	for _, v := range c.Filesystems {
		s := ownerCatalogJSON(v.Definition)
		fsRows = append(fsRows, s)
		if has(c.BeforeDefinitionIDs.Filesystems, v.Definition.ID) {
			oldFS = append(oldFS, s)
		}
	}
	for _, v := range c.Constraints {
		s := ownerCatalogJSON(v.Definition)
		constraintRows = append(constraintRows, s)
		if has(c.BeforeDefinitionIDs.Constraints, v.Definition.ID) {
			oldConstraints = append(oldConstraints, s)
		}
	}
	material := func(r, w, f, k []string) string {
		return `{"Policy":` + ownerCatalogJSON(c.Policy) + `,"Roots":[` + strings.Join(r, ",") + `],"Workspaces":[` + strings.Join(w, ",") + `],"Filesystems":[` + strings.Join(f, ",") + `],"Constraints":[` + strings.Join(k, ",") + `]}`
	}
	c.BeforeDefinitionsDigest = ownerCatalogDigest("desk-storage-owner-definitions-v2", material(oldRoots, oldWorkspaces, oldFS, oldConstraints))
	c.AfterDefinitionsDigest = ownerCatalogDigest("desk-storage-owner-definitions-v2", material(rootRows, workspaceRows, fsRows, constraintRows))
}

func ownerCatalogFixture() (storageOwnerCatalogV2, storageOwnerRecordV2, storageOwnerClaimV2) {
	record, _ := ownerTestRecord()
	claim := ownerTestClaimValue()
	limits := storagebudget.Limits{Total: storagebudget.Vector{Bytes: 1 << 30, Files: 100000, Directories: 10000}}
	for i := range limits.Category {
		limits.Category[i] = limits.Total
	}
	c := storageOwnerCatalogV2{Version: 2, Installation: storagebudget.ID{1}, Sequence: 1, Phase: "prepared", Transition: storagebudget.ID{9}, ExpectedQRevision: 1, Policy: storageOwnerPolicy{limits, limits, 1000000000}, BeforeDefinitionIDs: storageOwnerDefinitionIDs{[]storagebudget.ID{}, []storagebudget.ID{}, []storagebudget.ID{}, []storagebudget.ID{}}, OriginalOwner: storageOwnerSource{Kind: 1, ConfigPath: []byte("/config"), HomeOwner: ownerTestHome, HomePath: []byte{}, RegistryControl: math.MaxUint32, Entries: []storageOwnerHomeRelation{}}, Objects: []storageOwnerObject{}, OwnerControls: []storageOwnerControl{}, Roots: []storageOwnerRoot{}, Workspaces: []storageOwnerWorkspace{}, Filesystems: []storageOwnerFilesystem{}, Constraints: []storageOwnerConstraint{}, Profiles: []storageOwnerProfile{}}
	po := func(n uint64, kind uint8) storageOwnerPersistentObject {
		return storageOwnerPersistentObject{1, [16]byte{1}, n, 0, 1, kind}
	}
	// Persistent key order deliberately differs from numeric role order.
	roles := []uint8{1, 2, 6, 7, 8, 9, 10, 4, 5, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 23, 21, 22, 24, 20, 21}
	names := []string{"", "storage-budget", ".storage-bootstrap.lock", ".storage-use.lock", ".home-use.lock", "storage-owner.a", "storage-owner.b", "storage-installation.claim.json", "storage-installation.json", "storage-host-intents", "lock", "checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests", ".data.lock", ".data-work.lock", "", ".jpack-data.json", "", "", "d5-provider.json", "private", "history"}
	lengths := []uint64{0, 0, 0, 0, 0, 8388608, 8388608, 4096, 4096, 2097152, 0, 4194304, 4194304, 1048576, 1048576, 33554432, 0, 0, 0, 99, 0, 0, 159, 0, 0}
	parents := []uint32{math.MaxUint32, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, math.MaxUint32, 18, math.MaxUint32, math.MaxUint32, 21, 0, 23}
	for i, role := range roles {
		source, owner, kind := uint8(1), uint8(1), uint8(2)
		if role == 1 || role == 2 || role >= 20 && role <= 22 {
			kind = 1
		}
		if i >= 18 && i <= 22 {
			source = 3
			owner = 2
		}
		c.Objects = append(c.Objects, storageOwnerObject{role, source, parents[i], []byte(names[i]), po(uint64(11+i), kind), owner, kind, lengths[i]})
	}
	c.Profiles = append(c.Profiles, storageOwnerProfile{ID: 1, Version: 1, Backend: 1, PersistentEvidence: storagebudget.Digest{1}, MappingEvidence: storagebudget.Digest{2}, QuotaEvidence: storagebudget.Digest{3}, InodeEvidence: storagebudget.Digest{4}, CounterEvidence: storagebudget.Digest{5}, AllowanceEvidence: storagebudget.Digest{6}})
	c.Constraints = append(c.Constraints, storageOwnerConstraint{Definition: storagebudget.CapacityConstraint{ID: storagebudget.ID{10}, Identity: storagebudget.Digest{10}, Generation: 1, Resources: 3, Headroom: storagebudget.CapacityVector{Bytes: 100, Inodes: 10}}, Kind: 1, Pool: storagebudget.ID{1}, VolumeUUID: [16]byte{1}})
	c.Filesystems = append(c.Filesystems, storageOwnerFilesystem{Definition: storagebudget.Filesystem{ID: storagebudget.ID{1}, Identity: storagebudget.Digest{20}, Generation: 1, InodeModel: 1, CapabilityEvidence: storagebudget.Digest{4}, ByteConstraint: storagebudget.ID{10}, InodeConstraint: storagebudget.ID{10}}, Backend: 1, VolumeUUID: [16]byte{1}, PersistentProfile: 1, InodeProfile: 1})
	c.Workspaces = append(c.Workspaces, storageOwnerWorkspace{storagebudget.RegisteredWorkspace{ID: storagebudget.ID{1}, Generation: 1, Limits: limits}, 1, []byte(strings.Repeat("a", 64)), []byte("/project")}, storageOwnerWorkspace{storagebudget.RegisteredWorkspace{ID: storagebudget.ID{2}, Generation: 1, Limits: limits}, 3, []byte("retained-scope"), []byte("/retained-project")})
	c.OwnerControls = append(c.OwnerControls, storageOwnerControl{Role: 2, ParentObject: 0, Name: []byte("data-location.json"), OwnerKind: 2, OwnerKey: []byte(strings.Repeat("b", 64)), Location: storageOwnerLocation{Version: 1, Path: []byte("/data/a"), Previous: []byte{}, Selected: po(31, 1)}, MaxBytes: 16384})
	membership := storageOwnerMembership{Backend: 1, ProfileID: 1, ProfileVersion: 1, Pool: storagebudget.ID{1}, VolumeUUID: [16]byte{1}, User: storageOwnerSubject{State: 1, Kind: 1, NamespaceMap: storagebudget.Digest{2}}, Group: storageOwnerSubject{State: 2, Kind: 2, NamespaceMap: storagebudget.Digest{2}}, Project: storageOwnerSubject{State: 1, Kind: 3, NamespaceMap: storagebudget.Digest{2}}, Creation: 1, ExistingObjects: []uint32{}}
	rootRoles := []uint8{1, 2, 3, 4, 5, 6, 4}
	dirs := []uint32{0, 1, 18, 20, 21, 23, 24}
	categories := []uint8{4, 4, 2, 0, 1, 3, 0}
	paths := []string{"/config", "/config/storage-budget", "/desk/private", "/data/a", "/provider", "/config/private", "/config/private/history"}
	for i, role := range rootRoles {
		r := storageOwnerRoot{Definition: storagebudget.RegisteredRoot{ID: storagebudget.ID{byte(i + 1)}, Filesystem: storagebudget.ID{1}, Generation: 1, Locator: fmt.Sprintf("root:%d", i+1), CapacityCount: 1, CapacityRefs: [4]storagebudget.CapacityRef{{Constraint: storagebudget.ID{10}, Generation: 1}}}, Role: role, Category: categories[i], References: []storagebudget.ID{}, Source: storageOwnerRootSource{Kind: role, OwnerKey: []byte(ownerTestHome), Path: []byte(paths[i]), Relative: []byte{}, ControlIndex: math.MaxUint32}, DirectoryObject: dirs[i], DomainObject: 16, WorkObject: math.MaxUint32, Membership: membership}
		switch i {
		case 0:
			r.WorkObject = 17
		case 1:
			r.DomainObject = math.MaxUint32
			r.Source.ParentRoot = storagebudget.ID{1}
			r.Source.Relative = []byte("storage-budget")
		case 2:
			r.Source.OwnerKey = []byte(strings.Repeat("c", 32))
			r.Source.ControlKind = 1
			r.Source.ControlIndex = 19
			r.References = []storagebudget.ID{{1}}
			r.Definition.Workspace = storagebudget.ID{1}
		case 3:
			r.Source.OwnerKey = []byte(strings.Repeat("b", 64))
			r.Source.ControlKind = 2
			r.Source.ControlIndex = 0
			r.Source.ControlSelection = 1
		case 4:
			r.Source.OwnerKey = []byte(strings.Repeat("d", 64))
			r.Source.ControlKind = 1
			r.Source.ControlIndex = 22
			r.DomainObject = 21
			r.References = []storagebudget.ID{{2}}
		case 5:
			r.Source.OwnerKey = []byte("specific-writer/scope")
			r.Source.ParentRoot = storagebudget.ID{1}
			r.Source.Relative = []byte("private")
			r.DomainObject = 23
			r.Membership.Creation = 2
			r.Membership.ExistingObjects = []uint32{9}
		case 6:
			r.Source.OwnerKey = []byte(strings.Repeat("e", 32))
			r.Source.ParentRoot = storagebudget.ID{6}
			r.Source.Relative = []byte("history")
		}
		c.Roots = append(c.Roots, r)
	}
	ownerCatalogSeal(&c, &record, &claim)
	return c, record, claim
}

func ownerCatalogClone(c storageOwnerCatalogV2) storageOwnerCatalogV2 {
	var out storageOwnerCatalogV2
	if err := json.Unmarshal([]byte(ownerCatalogJSON(c)), &out); err != nil {
		panic(err)
	}
	return out
}
func ownerCatalogWire(payload string, seq uint64) []byte {
	raw := make([]byte, 8<<20)
	copy(raw, "DSKOWN2\x00")
	raw[8] = 2
	raw[12] = 64
	binary.LittleEndian.PutUint64(raw[16:24], seq)
	binary.LittleEndian.PutUint32(raw[24:28], uint32(len(payload)))
	copy(raw[64:], payload)
	ownerTestChecksum(raw)
	return raw
}

func TestStorageOwnerCatalogV2HandwrittenEncoding(t *testing.T) {
	c, record, claim := ownerCatalogFixture()
	t.Run("complete-legacy-prepared", func(t *testing.T) {
		raw, err := storageOwnerEncodeCatalogV2(c, record, claim)
		if err != nil {
			t.Fatal(err)
		}
		want := ownerCatalogWire(ownerCatalogJSON(c), 1)
		if !bytes.Equal(raw, want) {
			t.Fatal("exact8MiB envelope differs")
		}
		got, err := storageOwnerDecodeCatalogV2(want, record, claim)
		if err != nil || ownerCatalogJSON(got) != ownerCatalogJSON(c) {
			t.Fatalf("decode: %v", err)
		}
		clear(want)
		if string(got.OriginalOwner.ConfigPath) != "/config" {
			t.Fatal("input alias")
		}
	})
	t.Run("active-same-complete-map", func(t *testing.T) {
		v := ownerCatalogClone(c)
		v.Phase = "active"
		v.Sequence = 2
		v.PreviousCatalogDigest = storagebudget.Digest{99}
		for _, r := range v.Roots {
			v.BeforeDefinitionIDs.Roots = append(v.BeforeDefinitionIDs.Roots, r.Definition.ID)
		}
		for _, w := range v.Workspaces {
			v.BeforeDefinitionIDs.Workspaces = append(v.BeforeDefinitionIDs.Workspaces, w.Definition.ID)
		}
		v.BeforeDefinitionIDs.Filesystems = []storagebudget.ID{{1}}
		v.BeforeDefinitionIDs.Constraints = []storagebudget.ID{{10}}
		ownerCatalogSeal(&v, &record, &claim)
		if v.BeforeDefinitionsDigest != v.AfterDefinitionsDigest {
			t.Fatal("same-definition transition changed digest")
		}
		if _, err := storageOwnerEncodeCatalogV2(v, record, claim); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("index-free-material-oracle", func(t *testing.T) {
		for _, r := range c.Roots {
			got, err := c.rootIdentity(r)
			if err != nil || got != ownerCatalogIdentity(c, r) {
				t.Fatal("independent material oracle mismatch")
			}
		}
	})
}
