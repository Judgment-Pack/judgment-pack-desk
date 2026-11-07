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
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func epInto(dst, src any) {
	if err := json.Unmarshal(epJSON(src), dst); err != nil {
		panic(err)
	}
}
func epSourceBytes(b storageEnrollmentPeakSource) []byte {
	w := epBytes{}
	w.fixed(b.Installation)
	w.fixed(b.QBinding)
	w.fixed(b.Project)
	w.fixed(b.Workspace)
	w.fixed(b.WorkspaceGeneration)
	w.data(b.OwnerKey)
	w.data(b.HistoryIdentity)
	w.fixed(b.HomeSource)
	w.fixed(b.Filesystem)
	w.fixed(b.FilesystemIdentity)
	w.fixed(b.FilesystemGeneration)
	w.fixed(b.CapacityCount)
	w.fixed(b.CapacityRefs)
	w.fixed(b.CapacityBinding)
	w.data(b.Namespace)
	w.fixed(b.Generation)
	return w.Bytes()
}
func epHomeSource(b storageEnrollmentPeakSource, c storageOwnerCatalogV2) storagebudget.Digest {
	// Standalone Kind1's full typed empty Registry and Entry are explicit. Kind2
	// source-entry selection is separately covered by source mutation fixtures.
	emptyRegistry := `{"Role":0,"Parent":` + ownerCatalogJSON(storageOwnerPersistentObject{}) + `,"Name":"","OwnerKind":0,"OwnerKey":"","Location":` + ownerCatalogJSON(storageOwnerLocation{Path: []byte{}, Previous: []byte{}}) + `,"RelationDigest":` + ownerCatalogJSON(storagebudget.Digest{}) + `,"MaxBytes":0}`
	emptyEntry := `{"DeskID":"","Folder":"","Kind":0,"HistoryIdentity":"","StorageOwner":"","RunnerWorkspace":""}`
	raw := fmt.Sprintf(`{"Installation":%s,"QBinding":%s,"Project":%s,"Workspace":%s,"OwnerKey":%s,"HistoryIdentity":%s,"Kind":%d,"ConfigPath":%s,"HomeOwner":%s,"HomePath":%s,"HomeObject":%s,"Registry":%s,"Entry":%s}`, epJSON(b.Installation), epJSON(b.QBinding), epJSON(b.Project), epJSON(c.Workspaces[0]), epJSON(b.OwnerKey), epJSON(b.HistoryIdentity), c.OriginalOwner.Kind, epJSON(c.OriginalOwner.ConfigPath), epJSON(c.OriginalOwner.HomeOwner), epJSON(c.OriginalOwner.HomePath), epJSON(c.OriginalOwner.HomeObject), emptyRegistry, emptyEntry)
	return epHash("desk-project-private-home-source-peak-v1", []byte(raw))
}
func epMapping(b storageEnrollmentPeakSource, c storageOwnerCatalogV2, parent storageOwnerRoot) storagebudget.Digest {
	p := parent.Membership
	objects := []string{}
	for _, i := range p.ExistingObjects {
		objects = append(objects, ownerCatalogObject(c, i))
	}
	membership := fmt.Sprintf(`{"Backend":%d,"ProfileID":%d,"ProfileVersion":%d,"ProfileEvidence":%s,"Pool":%s,"VolumeUUID":%s,"ContainerUUID":%s,"User":%s,"Group":%s,"Project":%s,"Creation":%d,"ExistingObjects":[%s]}`, p.Backend, p.ProfileID, p.ProfileVersion, epJSON(p.ProfileEvidence), epJSON(p.Pool), epJSON(p.VolumeUUID), epJSON(p.ContainerUUID), epJSON(p.User), epJSON(p.Group), epJSON(p.Project), p.Creation, strings.Join(objects, ","))
	raw := fmt.Sprintf(`{"Installation":%s,"QBinding":%s,"Project":%s,"Workspace":%s,"OwnerKey":%s,"HistoryIdentity":%s,"HomeSource":%s,"Namespace":%s,"Generation":%d,"Filesystem":%s,"CapacityCount":%d,"CapacityRefs":%s,"Constraints":%s,"Membership":%s,"Profiles":%s}`, epJSON(b.Installation), epJSON(b.QBinding), epJSON(b.Project), epJSON(c.Workspaces[0].Definition), epJSON(b.OwnerKey), epJSON(b.HistoryIdentity), epJSON(b.HomeSource), epJSON(b.Namespace), b.Generation, epJSON(c.Filesystems[0]), b.CapacityCount, epJSON(b.CapacityRefs), epJSON(c.Constraints), membership, epJSON(c.Profiles))
	return epHash("desk-project-private-source-mapping-peak-v1", []byte(raw))
}
func epSealDefinitions(c *storageOwnerCatalogV2) {
	roots, workspaces, filesystems, constraints := []string{}, []string{}, []string{}, []string{}
	old := [4][]string{{}, {}, {}, {}}
	has := func(ids []storagebudget.ID, id storagebudget.ID) bool {
		for _, x := range ids {
			if x == id {
				return true
			}
		}
		return false
	}
	for _, r := range c.Roots {
		s := ownerCatalogJSON(r.Definition)
		roots = append(roots, s)
		if has(c.BeforeDefinitionIDs.Roots, r.Definition.ID) {
			old[0] = append(old[0], s)
		}
	}
	for _, r := range c.Workspaces {
		s := ownerCatalogJSON(r.Definition)
		workspaces = append(workspaces, s)
		if has(c.BeforeDefinitionIDs.Workspaces, r.Definition.ID) {
			old[1] = append(old[1], s)
		}
	}
	for _, r := range c.Filesystems {
		s := ownerCatalogJSON(r.Definition)
		filesystems = append(filesystems, s)
		if has(c.BeforeDefinitionIDs.Filesystems, r.Definition.ID) {
			old[2] = append(old[2], s)
		}
	}
	for _, r := range c.Constraints {
		s := ownerCatalogJSON(r.Definition)
		constraints = append(constraints, s)
		if has(c.BeforeDefinitionIDs.Constraints, r.Definition.ID) {
			old[3] = append(old[3], s)
		}
	}
	material := func(v [4][]string) []byte {
		return []byte(`{"Policy":` + ownerCatalogJSON(c.Policy) + `,"Roots":[` + strings.Join(v[0], ",") + `],"Workspaces":[` + strings.Join(v[1], ",") + `],"Filesystems":[` + strings.Join(v[2], ",") + `],"Constraints":[` + strings.Join(v[3], ",") + `]}`)
	}
	c.BeforeDefinitionsDigest = epHash("desk-storage-owner-definitions-v2", material(old))
	c.AfterDefinitionsDigest = epHash("desk-storage-owner-definitions-v2", material([4][]string{roots, workspaces, filesystems, constraints}))
}
func epCatalogs(t *testing.T) (storageOwnerCatalogV2, storageOwnerCatalogV2, storageEnrollmentPeakSource, storageOwnerRecordV2, storageOwnerClaimV2) {
	t.Helper()
	before, r, cl := ownerAdvisoryFixture(false)
	return epCatalogsFrom(t, before, r, cl)
}

func epCatalogsFrom(t *testing.T, before storageOwnerCatalogV2, r storageOwnerRecordV2, cl storageOwnerClaimV2) (storageOwnerCatalogV2, storageOwnerCatalogV2, storageEnrollmentPeakSource, storageOwnerRecordV2, storageOwnerClaimV2) {
	t.Helper()
	raw := ownerAdvisoryWire(before)
	after := ownerCatalogClone(before)
	after.Version = 1
	// The logical successor is Prepared even when its original V3 is Active.
	// The original sequence remains part of the strict evidence relation.
	after.Phase = "prepared"
	after.Sequence = before.Sequence + 1
	after.Transition = storagebudget.ID{9}
	copy(after.PreviousCatalogDigest[:], raw[32:64])
	after.ExpectedQRevision = 11
	for _, x := range before.Roots {
		after.BeforeDefinitionIDs.Roots = append(after.BeforeDefinitionIDs.Roots, x.Definition.ID)
	}
	for _, x := range before.Workspaces {
		after.BeforeDefinitionIDs.Workspaces = append(after.BeforeDefinitionIDs.Workspaces, x.Definition.ID)
	}
	for _, x := range before.Filesystems {
		after.BeforeDefinitionIDs.Filesystems = append(after.BeforeDefinitionIDs.Filesystems, x.Definition.ID)
	}
	for _, x := range before.Constraints {
		after.BeforeDefinitionIDs.Constraints = append(after.BeforeDefinitionIDs.Constraints, x.Definition.ID)
	}
	project := epNative(100, 1)
	index := uint32(len(after.Objects))
	after.Objects = append(after.Objects, storageOwnerObject{Role: 20, Source: 3, Parent: math.MaxUint32, Name: []byte{}, Persistent: project, OwnerClass: 2, ModeClass: 1})
	parentSourceKey := []byte("original-project-owner")
	if after.Workspaces[0].OwnerKind == 3 {
		parentSourceKey = append([]byte{}, after.Workspaces[0].OwnerKey...)
	}
	parent := storageOwnerRoot{Definition: storagebudget.RegisteredRoot{ID: storagebudget.ID{8}, Owner: after.QBinding, Filesystem: after.Filesystems[0].Definition.ID, Workspace: after.Workspaces[0].Definition.ID, Generation: 1, Locator: "project-private-parent:8", CapacityCount: after.Roots[0].Definition.CapacityCount, CapacityRefs: after.Roots[0].Definition.CapacityRefs}, Role: 7, Category: 4, References: []storagebudget.ID{after.Workspaces[0].Definition.ID}, Source: storageOwnerRootSource{Kind: 7, OwnerKey: parentSourceKey, Path: []byte{}, Relative: []byte{}, ControlIndex: math.MaxUint32}, DirectoryObject: index, DomainObject: math.MaxUint32, WorkObject: math.MaxUint32, Membership: after.Roots[0].Membership}
	parent.Membership.ExistingObjects = []uint32{}
	b := storageEnrollmentPeakSource{Installation: after.Installation, QBinding: after.QBinding, Project: project, Workspace: parent.Definition.Workspace, WorkspaceGeneration: 1, OwnerKey: parent.Source.OwnerKey, HistoryIdentity: after.Workspaces[0].HistoryIdentity, Filesystem: parent.Definition.Filesystem, FilesystemIdentity: after.Filesystems[0].Definition.Identity, FilesystemGeneration: 1, CapacityCount: parent.Definition.CapacityCount, CapacityRefs: parent.Definition.CapacityRefs, Namespace: []byte(".desk-private"), Generation: 1}
	b.HomeSource = epHomeSource(b, after)
	b.CapacityBinding = epMapping(b, after, parent)
	parent.Source.Relative = epSourceBytes(b)
	parent.Definition.Identity = epHash("desk-project-private-parent-scope-e1", parent.Source.Relative)
	constraints := []storagebudget.CapacityConstraint{}
	for _, v := range after.Constraints {
		constraints = append(constraints, v.Definition)
	}
	d := parent.Definition
	var err error
	parent.Definition.CapacityBinding, err = storagebudget.CapacityBindingFor(storagebudget.Root{ID: d.ID, Owner: d.Owner, Identity: d.Identity, Filesystem: d.Filesystem, Generation: d.Generation, CapacityCount: d.CapacityCount, CapacityRefs: d.CapacityRefs}, after.Filesystems[0].Definition, constraints)
	if err != nil {
		t.Fatal(err)
	}
	after.Roots = append(after.Roots, parent)
	epSealDefinitions(&after)
	return before, after, b, r, cl
}
func epQ0(c storageOwnerCatalogV2) *storageEnrollmentPeakQCheckpoint {
	q := &storageEnrollmentPeakQCheckpoint{Generation: 10, Epoch: 1}
	epInto(&q.Policy.Installation, c.Policy.Installation)
	epInto(&q.Policy.Workspace, c.Policy.Workspace)
	q.Policy.ObservationMaxAge = time.Duration(c.Policy.ObservationMaxAgeNanos)
	for i, d := range c.Roots {
		epInto(&q.Roots[i], d.Definition)
	}
	for i, d := range c.Workspaces {
		epInto(&q.Workspaces[i], d.Definition)
	}
	for i, d := range c.Filesystems {
		epInto(&q.Filesystems[i], d.Definition)
	}
	for i, d := range c.Constraints {
		epInto(&q.CapacityConstraints[i], d.Definition)
	}
	for i := range q.Applied {
		q.Applied[i].NextSequence = 1
	}
	return q
}
func epQSeal(raw []byte, kind byte, index uint16, generation uint64, record storageOwnerRecordV2) {
	copy(raw, "JPSTB002")
	raw[8] = kind
	epSet16(raw[10:], index)
	epSet64(raw[16:], generation)
	copy(raw[24:40], record.Installation[:])
	copy(raw[40:72], record.QBinding[:])
	clear(raw[72:104])
	d := sha256.Sum256(raw)
	copy(raw[72:104], d[:])
}
func epQBytes(q *storageEnrollmentPeakQCheckpoint, record storageOwnerRecordV2) []byte {
	b := make([]byte, 4194304)
	w := epBytes{}
	w.fixed(q.Epoch)
	w.fixed(q.Maintenance)
	w.fixed(q.Installation)
	copy(b[128:], w.Bytes())
	w.Reset()
	w.fixed(q.Policy.Installation)
	w.fixed(q.Policy.Workspace)
	w.fixed(uint64(q.Policy.ObservationMaxAge))
	copy(b[16384:], w.Bytes())
	for i, v := range q.Roots {
		if v.ID == (storageEnrollmentPeakQID{}) {
			continue
		}
		w.Reset()
		w.fixed(v.ID)
		w.fixed(v.Owner)
		w.fixed(v.Identity)
		w.fixed(v.Filesystem)
		w.fixed(v.Workspace)
		w.fixed(v.Generation)
		w.fixed(v.Known)
		w.fixed(uint16(len(v.Locator)))
		text := make([]byte, 256)
		copy(text, v.Locator)
		w.Write(text)
		w.fixed(v.Usage)
		w.fixed(v.CapacityCount)
		w.fixed(v.CapacityRefs)
		w.fixed(v.CapacityBinding)
		copy(b[32768+i*768:], w.Bytes())
	}
	for i, v := range q.Workspaces {
		if v.ID == (storageEnrollmentPeakQID{}) {
			continue
		}
		w.Reset()
		w.fixed(v.ID)
		w.fixed(v.Generation)
		w.fixed(v.Known)
		w.fixed(v.Usage)
		w.fixed(v.Limits)
		copy(b[3178496+i*512:], w.Bytes())
	}
	for i, v := range q.Filesystems {
		if v.ID == (storageEnrollmentPeakQID{}) {
			continue
		}
		w.Reset()
		w.fixed(v)
		copy(b[3702784+i*256:], w.Bytes())
	}
	for i, v := range q.Applied {
		w.Reset()
		w.fixed(v)
		copy(b[3735552+i*128:], w.Bytes())
	}
	for i, v := range q.CapacityConstraints {
		if v.ID == (storageEnrollmentPeakQID{}) {
			continue
		}
		w.Reset()
		w.fixed(v)
		copy(b[3743744+i*128:], w.Bytes())
	}
	epQSeal(b, 1, 0, q.Generation, record)
	return b
}
func epEmptyTickets(record storageOwnerRecordV2) [2][]byte {
	var pages [2][]byte
	pages[0] = make([]byte, 1048576)
	for i := 0; i < 64; i++ {
		epQSeal(pages[0][i*16384:(i+1)*16384], 2, uint16(i), 0, record)
	}
	pages[1] = append([]byte{}, pages[0]...)
	return pages
}
func epTicketBody(t storagebudget.Ticket) []byte {
	w := epBytes{}
	w.fixed(t.Token)
	w.fixed(uint8(t.State))
	w.fixed(t.PolicyRevision)
	w.data([]byte(t.Workflow))
	w.data([]byte(t.Operation))
	w.fixed(t.Owner)
	w.fixed(t.Manifest)
	w.fixed(t.ManifestLength)
	w.fixed(t.Evidence)
	w.fixed(uint8(len(t.Components)))
	w.fixed(uint8(len(t.Changes)))
	for _, v := range t.Components {
		w.fixed(v)
	}
	for _, v := range t.Changes {
		w.fixed(v)
	}
	return w.Bytes()
}
func epView(c storageOwnerCatalogV2, q *storageEnrollmentPeakQCheckpoint) (storageEnrollmentPeakView, []byte) {
	v := storageEnrollmentPeakView{Revision: q.Generation, Epoch: q.Epoch, Maintenance: q.Maintenance, Policy: c.Policy}
	for k, n := range []int{len(c.Filesystems), len(c.Constraints), len(c.Roots), len(c.Workspaces)} {
		v.DefinitionRefs[k] = []uint16{}
		for i := 0; i < n; i++ {
			v.DefinitionRefs[k] = append(v.DefinitionRefs[k], uint16(i))
		}
	}
	w := epBytes{}
	w.fixed(v.Revision)
	w.fixed(v.Epoch)
	w.fixed(v.Maintenance)
	w.fixed(v.Policy)
	for _, refs := range v.DefinitionRefs {
		w.fixed(uint16(len(refs)))
		for _, i := range refs {
			w.fixed(i)
		}
	}
	for i := 0; i < 64; i++ {
		v.Slots[i] = storagebudget.HostSlotState{Ticket: storagebudget.Ticket{Token: storagebudget.Token{Slot: byte(i)}, Components: []storagebudget.Component{}, Changes: []storagebudget.Change{}}, PersistedApplied: storagebudget.Token{Slot: byte(i)}, PersistedNextSequence: 1}
		w.fixed(uint16(i))
		w.fixed(v.Slots[i].PersistedApplied)
		w.fixed(uint64(1))
	}
	v.Raw = append([]byte{}, w.Bytes()...)
	return v, w.Bytes()
}
func epCheckpoint(c storageOwnerCatalogV2, q *storageEnrollmentPeakQCheckpoint, cp []byte, tickets [2][]byte) []byte {
	w := epBytes{}
	w.fixed(q.Generation)
	w.fixed(q.Epoch)
	w.fixed(q.Maintenance)
	w.fixed(c.Policy)
	w.fixed(q.Installation)
	for kind, n := range []int{len(c.Roots), len(c.Workspaces), len(c.Filesystems), len(c.Constraints)} {
		w.fixed(uint16(n))
		for i := 0; i < n; i++ {
			w.fixed(uint16(i))
			w.fixed(uint16(i))
			if kind == 0 {
				w.fixed(q.Roots[i].Known)
				w.fixed(q.Roots[i].Usage)
			}
			if kind == 1 {
				w.fixed(q.Workspaces[i].Known)
				w.fixed(q.Workspaces[i].Usage)
			}
		}
	}
	for _, a := range q.Applied {
		w.fixed(a)
	}
	w.fixed(byte(0))
	d := sha256.Sum256(cp)
	w.fixed(d)
	w.fixed(d)
	for slot := 0; slot < 64; slot++ {
		for bank := 0; bank < 2; bank++ {
			w.fixed(sha256.Sum256(tickets[bank][slot*16384 : (slot+1)*16384]))
		}
	}
	w.Write(make([]byte, 64))
	w.fixed(uint16(0))
	return w.Bytes()
}
func epProjection(c storageOwnerCatalogV2, dict storageOwnerCatalogV2, outer []byte) []byte {
	w := epBytes{}
	w.fixed(uint16(1))
	w.fixed(byte(0))
	w.fixed(byte(0))
	w.fixed(uint32(0))
	w.fixed(c.Version)
	w.fixed(c.Installation)
	w.fixed(c.QBinding)
	w.fixed(c.Sequence)
	w.data([]byte(c.Phase))
	w.fixed(c.Transition)
	w.fixed(c.PreviousCatalogDigest)
	w.fixed(c.BeforeDefinitionsDigest)
	w.fixed(c.AfterDefinitionsDigest)
	w.fixed(c.ExpectedQRevision)
	w.fixed(c.Policy)
	w.fixed(uint16(0))
	w.fixed(c.OriginalOwner.RegistryControl)
	for _, n := range []int{len(c.Objects), len(c.OwnerControls), len(c.Roots), len(c.Workspaces), len(c.Filesystems), len(c.Constraints), len(c.Profiles), len(c.OriginalOwner.Entries)} {
		w.fixed(uint16(n))
		for i := 0; i < n; i++ {
			w.fixed(uint16(i))
		}
	}
	for _, ids := range [][]storagebudget.ID{c.BeforeDefinitionIDs.Roots, c.BeforeDefinitionIDs.Workspaces, c.BeforeDefinitionIDs.Filesystems, c.BeforeDefinitionIDs.Constraints} {
		w.fixed(uint16(len(ids)))
		for _, id := range ids {
			w.fixed(id)
		}
	}
	w.fixed(uint16(0))
	w.data(outer[:64])
	b := append([]byte{}, w.Bytes()...)
	epSet32(b[4:], uint32(len(b)))
	_ = dict
	return b
}

// Real F measures the retained original Control root at the unchanged minimum.
// Phase0 retains the exact same row IDs/order but every Usage/Evidence byte zero.
func epCensus(q *storageEnrollmentPeakQCheckpoint, armed bool) []byte {
	w := epBytes{}
	evidence := storagebudget.Digest{}
	if armed {
		evidence[0] = 7
	}
	w.fixed(evidence)
	roots := []storageEnrollmentPeakQRoot{}
	workspaces := []storageEnrollmentPeakQWorkspace{}
	for _, r := range q.Roots {
		if r.ID != (storageEnrollmentPeakQID{}) {
			roots = append(roots, r)
		}
	}
	for _, r := range q.Workspaces {
		if r.ID != (storageEnrollmentPeakQID{}) {
			workspaces = append(workspaces, r)
		}
	}
	w.fixed(uint16(len(roots)))
	for _, r := range roots {
		w.fixed(r.ID)
		w.fixed(r.Generation)
		usage := [9]storagebudget.Vector{}
		if armed && r.ID == q.Roots[0].ID {
			usage[4] = storagebudget.Vector{Bytes: 67108864, Files: 16, Directories: 1}
		}
		w.fixed(usage)
	}
	w.fixed(uint16(len(workspaces)))
	for _, r := range workspaces {
		w.fixed(r.ID)
		w.fixed(r.Generation)
		w.fixed([9]storagebudget.Vector{})
	}
	return w.Bytes()
}

// Handwritten expected after state, independent of the codec's Census projection.
func epMeasuredQ(q *storageEnrollmentPeakQCheckpoint) {
	q.Maintenance = false
	q.Installation = [9]storageEnrollmentPeakQVector{}
	q.Installation[4] = storageEnrollmentPeakQVector{Bytes: 67108864, Files: 16, Directories: 1}
	for i := range q.Roots {
		if q.Roots[i].ID != (storageEnrollmentPeakQID{}) {
			q.Roots[i].Known = true
			q.Roots[i].Usage = [9]storageEnrollmentPeakQVector{}
			if i == 0 {
				q.Roots[i].Usage[4] = storageEnrollmentPeakQVector{Bytes: 67108864, Files: 16, Directories: 1}
			}
		}
	}
	for i := range q.Workspaces {
		if q.Workspaces[i].ID != (storageEnrollmentPeakQID{}) {
			q.Workspaces[i].Known = true
			q.Workspaces[i].Usage = [9]storageEnrollmentPeakQVector{}
		}
	}
}

func epGroup(g storageEnrollmentPeakGroup) []byte {
	w := epBytes{}
	w.fixed(g.ID)
	w.fixed(g.Parent)
	w.fixed(g.Target)
	w.fixed(g.Bytes)
	w.fixed(g.Files)
	w.fixed(g.Directories)
	w.fixed(g.MetadataOps)
	w.fixed(g.Completion)
	w.fixed(g.State)
	w.fixed(byte(0))
	w.fixed(g.Native)
	w.fixed(g.Expected)
	w.fixed(g.Generation)
	return w.Bytes()
}
func epBoard(s storageEnrollmentPeakScope, b storageEnrollmentPeakSource, phase byte, q [3]storagebudget.Digest, origin storagebudget.Digest) []byte {
	w := epBytes{}
	w.fixed(uint16(2))
	w.Write(s[24:40])
	w.Write(s[216:286])
	w.Write(s[184:216])
	w.fixed(s[326])
	for i := 0; i < int(s[326]); i++ {
		w.Write(s[440+i*80 : 464+i*80])
	}
	w.Write(s[360:384])
	groups := epGroups(s.path(), [2]uint64{binary.LittleEndian.Uint64(s[344:]), binary.LittleEndian.Uint64(s[352:])}, s[322], s[323], s[324], s[325])
	sum := uint64(0)
	for _, g := range groups {
		sum += g.Bytes
	}
	w.fixed(sum)
	w.Write(s[384:400])
	w.Write(s[322:326])
	w.fixed(byte(len(groups)))
	for _, g := range groups {
		g.Native = s.target(g.Target)
		if g.ID >= 16 && g.ID <= 20 || g.ID >= 51 && g.ID <= 55 {
			g.Expected = origin
		}
		switch g.ID {
		case 21, 56:
			g.Expected = q[0]
		case 22, 57:
			g.Expected = q[1]
		case 23, 58:
			if phase != 0 {
				g.Expected = q[2]
			}
		}
		w.Write(epGroup(g))
	}
	w.fixed(uint16(0))
	w.fixed(uint16(0))
	w.Write(make([]byte, 96))
	for _, refs := range [][]uint16{{7}, {}, {}, {}} {
		w.fixed(uint16(len(refs)))
		for _, i := range refs {
			w.fixed(i)
		}
	}
	w.fixed(byte(7))
	w.Write(epSourceBytes(b))
	out := make([]byte, 65536)
	copy(out, w.Bytes())
	return out
}

type epComplete struct {
	before, after storageOwnerCatalogV2
	record        storageOwnerRecordV2
	claim         storageOwnerClaimV2
	source        storageEnrollmentPeakSource
	scope         storageEnrollmentPeakScope
	q             [4]*storageEnrollmentPeakQCheckpoint
	cp            [4][]byte
	tickets       [2][]byte
	sections      [9][]byte
	raw           []byte
	evidence      storageEnrollmentPeakEvidence
}

func epEvidence(t *testing.T, phase byte) epComplete {
	return epEvidenceExtents(t, phase, [2]uint64{262144, 262144})
}
func epEvidenceExtents(t *testing.T, phase byte, final [2]uint64) epComplete {
	t.Helper()
	before, after, source, record, claim := epCatalogs(t)
	return epEvidenceFromCatalogs(t, phase, final, before, after, source, record, claim)
}

func epEvidenceFromCatalogs(t *testing.T, phase byte, final [2]uint64, before, after storageOwnerCatalogV2, source storageEnrollmentPeakSource, record storageOwnerRecordV2, claim storageOwnerClaimV2) epComplete {
	t.Helper()
	f := epComplete{before: before, after: after, source: source, record: record, claim: claim}
	f.q[0] = epQ0(f.before)
	f.cp[0] = epQBytes(f.q[0], f.record)
	f.tickets = epEmptyTickets(f.record)
	view, viewRaw := epView(f.before, f.q[0])
	f.scope = epScope(1, true, f.record, [2]uint64{}, final)
	epSet64(f.scope[1624:1632], f.before.Sequence)
	d := sha256.Sum256(ownerAdvisoryWire(f.before))
	copy(f.scope[88:120], d[:])
	d = sha256.Sum256(f.cp[0])
	copy(f.scope[120:152], d[:])
	d = sha256.Sum256(viewRaw)
	copy(f.scope[152:184], d[:])
	copy(f.scope[440:456], f.before.Constraints[0].Definition.ID[:])
	epScopeHash(&f.scope)
	for i := 1; i <= 3; i++ {
		next := *f.q[i-1]
		next.Generation++
		if i == 1 {
			next.Epoch++
			next.Maintenance = true
		}
		if i == 2 {
			epInto(&next.Roots[7], f.after.Roots[7].Definition)
		}
		if i == 3 {
			epMeasuredQ(&next)
		}
		f.q[i] = &next
		f.cp[i] = epQBytes(&next, f.record)
	}
	old := ownerAdvisoryWire(f.before)
	n := binary.LittleEndian.Uint32(old[24:])
	f.sections[0] = append([]byte{}, old[:64+int(n)]...)
	controls := [4]storageOwnerPersistentObject{}
	for i := range controls {
		controls[i] = f.scope.target(byte(i + 1))
	}
	inner, err := storageEnrollmentPeakEncodeInner(storageEnrollmentPeakInner{Catalog: f.after, Controls: controls}, f.record, f.claim)
	if err != nil {
		t.Fatal("inner", err)
	}
	f.sections[1] = inner
	dictionary := epBytes{}
	for i := 0; i < 64; i++ {
		body := epTicketBody(view.Slots[i].Ticket)
		dictionary.fixed(uint16(i))
		dictionary.fixed(uint16(len(body)))
		dictionary.fixed(epHash("desk-enrollment-ticket-e1", body))
		dictionary.Write(body)
		for dictionary.Len()%8 != 0 {
			dictionary.WriteByte(0)
		}
	}
	f.sections[2] = append([]byte{}, dictionary.Bytes()...)
	f.sections[3] = viewRaw
	f.sections[4] = epProjection(f.before, f.after, old)
	f.sections[5] = []byte{}
	f.sections[6] = epCheckpoint(f.before, f.q[0], f.cp[0], f.tickets)
	f.sections[7] = epCensus(f.q[2], phase != 0)
	registration := storagebudget.Registration{Roots: []storagebudget.Root{}, Workspaces: []storagebudget.Workspace{}, Filesystems: []storagebudget.Filesystem{}, CapacityConstraints: []storagebudget.CapacityConstraint{}}
	var newRoot storagebudget.Root
	epInto(&newRoot, f.after.Roots[7].Definition)
	registration.Roots = append(registration.Roots, newRoot)
	rd := sha256.Sum256(epJSON(registration))
	originBytes := epBytes{}
	originBytes.fixed(f.record.Installation)
	originBytes.fixed(f.record.QBinding)
	originBytes.Write(f.scope[24:40])
	originBytes.fixed(f.source.Project)
	originBytes.fixed(rd)
	origin := epHash("desk-enrollment-origin-e1", originBytes.Bytes())
	digests := [3]storagebudget.Digest{sha256.Sum256(f.cp[1]), sha256.Sum256(f.cp[2]), sha256.Sum256(f.cp[3])}
	f.sections[8] = epBoard(f.scope, f.source, phase, digests, origin)
	// Full evidence framing is authored independently, including its directory,
	// exact offsets and checksum field location (no codec writer is the oracle).
	e := storageEnrollmentPeakEvidence{Generation: 1, Extent: 262144, Phase: phase, Bank: 0, Installation: f.record.Installation, QBinding: f.record.QBinding, Bridge: storagebudget.ID{9}, Ready: storagebudget.Digest{5}, Before: sha256.Sum256(old), BeforeInner: sha256.Sum256(epJSON(f.before)), AfterInner: sha256.Sum256(epJSON(f.after)), BeforeRevision: 10, BeforeEpoch: 1, AfterRevision: 13, AfterEpoch: 2, Controls: controls, Project: f.source.Project, RegistrationDigest: rd, CensusDigest: sha256.Sum256(f.sections[7]), CoveredEpoch: 2, Scope: f.scope}
	if phase != 0 {
		e.Generation = uint64(phase) + 2
		e.Previous = storagebudget.Digest{phase}
		if phase == 2 {
			e.Bank = 1
			armed := epEvidenceFromCatalogs(t, 1, final, before, after, source, record, claim)
			e.Previous = sha256.Sum256(armed.raw)
		}
	}
	e.Extent = final[e.Bank]
	recordRaw, err := storageOwnerEncodeRecordV2(f.record)
	if err != nil {
		t.Fatal(err)
	}
	claimRaw, err := storageOwnerEncodeClaimV2(f.claim)
	if err != nil {
		t.Fatal(err)
	}
	e.Record = sha256.Sum256(recordRaw)
	e.Claim = sha256.Sum256(claimRaw)
	counts := [9]uint32{1, 1, 64, 1, 1, 0, 1, 1, 1}
	last := 65536
	for i, section := range f.sections {
		last = (last + 7) &^ 7
		e.Sections[i] = storageEnrollmentPeakSection{Count: counts[i], Offset: uint64(last), Length: uint64(len(section)), Digest: sha256.Sum256(section), Raw: section}
		last += len(section)
	}
	e.Packed = uint64(last - 65536)
	if uint64(last) > e.Extent {
		t.Fatal("authored fixture extent", last)
	}
	f.raw = epEvidenceBytes(e)
	f.evidence = e
	return f
}
func epEvidenceBytes(e storageEnrollmentPeakEvidence) []byte {
	b := make([]byte, e.Extent)
	w := epBytes{}
	w.WriteString("DSKEPP1\x00")
	w.fixed(uint32(1))
	w.fixed(uint32(65536))
	w.fixed(e.Generation)
	w.fixed(e.Packed)
	w.fixed(e.Extent)
	w.fixed(e.Phase)
	w.fixed(e.Bank)
	w.fixed(uint16(7))
	w.fixed(e.Installation)
	w.fixed(e.QBinding)
	w.fixed(e.Bridge)
	for _, d := range []storagebudget.Digest{e.Previous, e.Record, e.Claim, e.Ready, e.Before, e.BeforeInner, e.AfterInner} {
		w.fixed(d)
	}
	for _, v := range []uint64{e.BeforeRevision, e.BeforeEpoch, e.AfterRevision, e.AfterEpoch} {
		w.fixed(v)
	}
	for _, p := range e.Controls {
		w.fixed(p)
	}
	w.fixed(e.Project)
	w.fixed(e.RegistrationDigest)
	w.fixed(e.CensusDigest)
	w.fixed(e.CoveredEpoch)
	w.fixed(uint16(9))
	maxima := [9]uint64{8388608, 8388608, 35143680, 1081344, 8921088, 2113536, 2097152, 2097152, 65536}
	for i, s := range e.Sections {
		w.fixed(uint16(i + 1))
		w.fixed(uint16(0))
		w.fixed(s.Count)
		w.fixed(s.Offset)
		w.fixed(s.Length)
		w.fixed(maxima[i])
		w.fixed(s.Digest)
		copy(b[s.Offset:s.Offset+s.Length], s.Raw)
	}
	checksumOffset := w.Len()
	w.Write(make([]byte, 32))
	copy(b, w.Bytes())
	copy(b[4096:8192], e.Scope[:])
	digest := epHash("desk-enrollment-bank-peak-v1", b)
	copy(b[checksumOffset:checksumOffset+32], digest[:])
	return b
}

func TestStorageEnrollmentPeakCompleteEvidenceVectors(t *testing.T) {
	for phase := byte(0); phase < 3; phase++ {
		t.Run(fmt.Sprint(phase), func(t *testing.T) {
			f := epEvidence(t, phase)
			e, err := storageEnrollmentPeakDecodeEvidence(f.raw, f.record, f.claim)
			if err != nil {
				t.Fatal("complete evidence", err)
			}
			if e.Phase != phase || len(e.Tickets) != 64 || len(e.Views) != 1 || len(e.Projections) != 1 || len(e.Board.Groups) != 58 || e.Registration.Roots[0].ID != storagebudget.ID([16]byte{8}) {
				t.Fatal("complete typed source/section join")
			}
			for i := 0; i < 2; i++ {
				if !bytes.Equal(e.PredictionBytes[i], f.cp[i+1]) {
					t.Fatal("Q1/Q2 exact original bytes")
				}
			}
			if phase != 0 && !bytes.Equal(e.PredictionBytes[2], f.cp[3]) {
				t.Fatal("Q3 exact original bytes")
			}
			encoded, err := storageEnrollmentPeakEncodeEvidence(e, f.record, f.claim)
			if err != nil || !bytes.Equal(encoded, f.raw) {
				t.Fatal("handwritten evidence framing", err)
			}
			if phase == 0 && e.Digests.Q[2] != (storagebudget.Digest{}) {
				t.Fatal("unavailable Q3 invented")
			}
			checks := []struct {
				name string
				got  []byte
				err  error
				want []byte
			}{}
			tickets, te := storageEnrollmentPeakWriteTickets(e.Tickets)
			checks = append(checks, struct {
				name string
				got  []byte
				err  error
				want []byte
			}{"tickets", tickets, te, f.sections[2]})
			views, ve := storageEnrollmentPeakWriteViews(e.Views, e.Inner.Catalog, e.Tickets)
			checks = append(checks, struct {
				name string
				got  []byte
				err  error
				want []byte
			}{"views", views, ve, f.sections[3]})
			cp, ce := storageEnrollmentPeakWriteCheckpoint(e.Checkpoint, e.Inner.Catalog, e.Views, e.Scope)
			checks = append(checks, struct {
				name string
				got  []byte
				err  error
				want []byte
			}{"checkpoint", cp, ce, f.sections[6]})
			census, fe := storageEnrollmentPeakWriteCensus(e.Census, e.Predictions[1], phase)
			checks = append(checks, struct {
				name string
				got  []byte
				err  error
				want []byte
			}{"census", census, fe, f.sections[7]})
			board, be := storageEnrollmentPeakWriteBoard(e.Board, e.Scope, phase, e.Digests, e.Inner.Catalog, len(e.Views), len(e.Projections), len(e.Witnesses))
			checks = append(checks, struct {
				name string
				got  []byte
				err  error
				want []byte
			}{"board", board, be, f.sections[8]})
			projection, pe := storageEnrollmentPeakWriteProjection(e.Projections[0], e.Inner.Catalog, e.OriginalV3, f.record, f.claim)
			checks = append(checks, struct {
				name string
				got  []byte
				err  error
				want []byte
			}{"projection", projection, pe, f.sections[4]})
			for _, check := range checks {
				if check.err != nil || !bytes.Equal(check.got, check.want) {
					t.Fatal("handwritten section writer", check.name, check.err)
				}
			}
		})
	}
}
