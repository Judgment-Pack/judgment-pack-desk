package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageEnrollmentPeakSourceAbsenceAndChildVectors(t *testing.T) {
	_, c, b, r, cl := epCatalogs(t)
	if err := storageEnrollmentPeakValidateCatalog(c, r, cl); err != nil {
		t.Fatal("absent private: source observations need no prior Q root", err)
	}
	sourceRaw := epSourceBytes(b)
	got, err := storageEnrollmentPeakEncodeSource(b)
	if err != nil || !bytes.Equal(got, sourceRaw) {
		t.Fatal("complete source bytes", err)
	}
	parent := c.Roots[len(c.Roots)-1]
	if parent.Definition.CapacityBinding == b.CapacityBinding {
		t.Fatal("source mapping reused as root-specific Q binding")
	}
	mapping, err := storageEnrollmentPeakSourceMapping(b, c, parent)
	if err != nil || mapping != epMapping(b, c, parent) {
		t.Fatal("full original source-mapping material", err)
	}
	max := b
	max.OwnerKey = bytes.Repeat([]byte{'o'}, 4096)
	max.HistoryIdentity = bytes.Repeat([]byte{'h'}, 4096)
	raw, err := storageEnrollmentPeakEncodeSource(max)
	if err != nil || len(raw) != 8543 {
		t.Fatal("source exact max", len(raw), err)
	}
	for _, bad := range [][]byte{raw[:8542], append(append([]byte{}, raw...), 0)} {
		if _, err := storageEnrollmentPeakDecodeSource(bad); err == nil {
			t.Fatal("source length")
		}
	}
	max.OwnerKey = append(max.OwnerKey, 'x')
	if _, err := storageEnrollmentPeakEncodeSource(max); err == nil {
		t.Fatal("source key cap")
	}
	// Append an independently observed child, keeping the complete Role7 row.
	child := parent
	child.Definition.ID = storagebudget.ID{9}
	child.Definition.Locator = "captured-child:9"
	child.Role = 6
	child.Source = storageOwnerRootSource{Kind: 8, OwnerKey: append([]byte{}, b.OwnerKey...), Path: []byte{}, ParentRoot: parent.Definition.ID, Relative: []byte(".desk-private"), ControlIndex: math.MaxUint32}
	child.DirectoryObject = uint32(len(c.Objects))
	child.Membership.ExistingObjects = []uint32{}
	c.Objects = append(c.Objects, storageOwnerObject{Role: 20, Source: 3, Parent: math.MaxUint32, Name: []byte{}, Persistent: epNative(101, 1), OwnerClass: 2, ModeClass: 1})
	// The original handwritten generic oracle uses typed-empty Home for Role6;
	// Kind8 is a new source discriminator inside that unchanged old domain.
	child.Definition.Identity = ownerCatalogIdentity(c, child)
	constraints := []storagebudget.CapacityConstraint{}
	for _, v := range c.Constraints {
		constraints = append(constraints, v.Definition)
	}
	d := child.Definition
	child.Definition.CapacityBinding, err = storagebudget.CapacityBindingFor(storagebudget.Root{ID: d.ID, Owner: d.Owner, Identity: d.Identity, Filesystem: d.Filesystem, Generation: d.Generation, CapacityCount: d.CapacityCount, CapacityRefs: d.CapacityRefs}, c.Filesystems[0].Definition, constraints)
	if err != nil {
		t.Fatal(err)
	}
	c.Roots = append(c.Roots, child)
	epSealDefinitions(&c)
	if err := storageEnrollmentPeakValidateCatalog(c, r, cl); err != nil {
		t.Fatal("captured child own unchanged Q binding", err)
	}
	if child.Definition.CapacityBinding == parent.Definition.CapacityBinding || !reflect.DeepEqual(parent, c.Roots[len(c.Roots)-2]) {
		t.Fatal("distinct root-specific bindings / exact parent retention")
	}
	child.Role = 3
	if identity, err := storageEnrollmentPeakChildIdentity(c, child); err != nil {
		t.Fatal(err)
	} else {
		role6 := child
		role6.Role = 6
		if other, _ := storageEnrollmentPeakChildIdentity(c, role6); identity == other {
			t.Fatal("role omitted")
		}
	}
	for _, mutate := range []func(*storageOwnerCatalogV2){func(v *storageOwnerCatalogV2) {
		v.Roots[len(v.Roots)-1].Definition.CapacityBinding = parent.Definition.CapacityBinding
	}, func(v *storageOwnerCatalogV2) { v.Roots[len(v.Roots)-1].Source.ParentRoot = storagebudget.ID{99} }, func(v *storageOwnerCatalogV2) { v.Roots[len(v.Roots)-1].Source.Relative = []byte("other") }, func(v *storageOwnerCatalogV2) { v.Roots[len(v.Roots)-1].Source.Kind = 9 }, func(v *storageOwnerCatalogV2) { v.Roots[len(v.Roots)-2].Source.Relative[0] ^= 1 }, func(v *storageOwnerCatalogV2) { v.Roots[len(v.Roots)-2].Membership.ProfileEvidence[0] ^= 1 }} {
		bad := ownerCatalogClone(c)
		mutate(&bad)
		epSealDefinitions(&bad)
		if storageEnrollmentPeakValidateCatalog(bad, r, cl) == nil {
			t.Fatal("forged source/root mapping")
		}
	}
	old := c
	old.Version = 3
	if _, err := storageOwnerDecodeCatalogV3(ownerAdvisoryWire(old), r, cl); err == nil {
		t.Fatal("old vocabulary accepted new authority rows")
	}
	for _, field := range []string{"home", "mapping", "workspace", "profile", "constraint"} {
		bad := ownerCatalogClone(c)
		bb := b
		switch field {
		case "home":
			bb.HomeSource[0] ^= 1
		case "mapping":
			bb.CapacityBinding[0] ^= 1
		case "workspace":
			bad.Workspaces[0].HistoryIdentity = []byte("changed")
		case "profile":
			bad.Profiles[0].CounterEvidence[0] ^= 1
		case "constraint":
			bad.Constraints[0].Definition.Headroom.Bytes++
		}
		if storageEnrollmentPeakSourceJoins(bb, bad, bad.Roots[len(bad.Roots)-2]) {
			t.Fatal("source provenance mutation", field)
		}
	}
}

func TestStorageEnrollmentPeakRegistrationDigestVectors(t *testing.T) {
	const raw = `{"Roots":[],"Workspaces":[],"Filesystems":[],"CapacityConstraints":[]}`
	const hash = "f57a97c18302f31ebe27b0c237155931573d61180534d03a04ef485dd2daa7c4"
	r := storagebudget.Registration{Roots: []storagebudget.Root{}, Workspaces: []storagebudget.Workspace{}, Filesystems: []storagebudget.Filesystem{}, CapacityConstraints: []storagebudget.CapacityConstraint{}}
	if string(epJSON(r)) != raw {
		t.Fatal("exact DTO fields/order/empty arrays")
	}
	digest, err := storageEnrollmentPeakRegistrationDigest(r)
	if err != nil || hex.EncodeToString(digest[:]) != hash {
		t.Fatal("literal raw/hash", err)
	}
	for _, mutation := range []string{`{"Workspaces":[],"Roots":[],"Filesystems":[],"CapacityConstraints":[]}`, `{"Roots":null,"Workspaces":[],"Filesystems":[],"CapacityConstraints":[]}`, raw + "\n", `{"Roots":[],"Workspaces":[],"Filesystems":[]}`} {
		if sha256.Sum256([]byte(mutation)) == digest {
			t.Fatal("order/empty/missing/newline mutation")
		}
	}
	r.Roots = nil
	if _, err := storageEnrollmentPeakRegistrationDigest(r); err == nil {
		t.Fatal("nil arrays repaired")
	}

	// A complete nonempty comparison-material byte oracle fixes every original
	// nested Root field. It is a digest vector, never a Q registration grant.
	const fullRaw = `{"Roots":[{"ID":[1,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Owner":[2,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Identity":[3,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Filesystem":[4,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Workspace":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Generation":5,"Locator":"\u003c\u003e\u0026","Known":false,"Usage":[{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0}],"CapacityCount":1,"CapacityRefs":[{"Constraint":[6,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Generation":7},{"Constraint":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Generation":0},{"Constraint":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Generation":0},{"Constraint":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Generation":0}],"CapacityBinding":[8,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]}],"Workspaces":[],"Filesystems":[],"CapacityConstraints":[]}`
	const fullHash = "70ae325d98cb29076ce42eeddfc0eb1e8ee275729e51fa5841741f5b1d2d5b58"
	full := storagebudget.Registration{Roots: []storagebudget.Root{{ID: storagebudget.ID{1}, Owner: storagebudget.Digest{2}, Identity: storagebudget.Digest{3}, Filesystem: storagebudget.ID{4}, Generation: 5, Locator: "<>&", CapacityCount: 1, CapacityRefs: [4]storagebudget.CapacityRef{{Constraint: storagebudget.ID{6}, Generation: 7}}, CapacityBinding: storagebudget.Digest{8}}}, Workspaces: []storagebudget.Workspace{}, Filesystems: []storagebudget.Filesystem{}, CapacityConstraints: []storagebudget.CapacityConstraint{}}
	if string(epJSON(full)) != fullRaw {
		t.Fatal("literal nonempty fields/order/escaping")
	}
	fd, err := storageEnrollmentPeakRegistrationDigest(full)
	if err != nil || hex.EncodeToString(fd[:]) != fullHash {
		t.Fatal("literal complete Root raw/hash", err)
	}
	_, c, _, _, _ := epCatalogs(t)
	q := epQ0(c)
	refs := [4][]uint16{{7}, {}, {}, {}}
	derived, err := storageEnrollmentPeakRegistration(c, refs, q)
	if err != nil {
		t.Fatal(err)
	}
	if derived.Roots[0].Known || derived.Roots[0].Usage != ([9]storagebudget.Vector{}) {
		t.Fatal("normalized original Register fields")
	}
	typ := reflect.TypeOf(derived.Roots[0])
	want := []string{"ID", "Owner", "Identity", "Filesystem", "Workspace", "Generation", "Locator", "Known", "Usage", "CapacityCount", "CapacityRefs", "CapacityBinding"}
	for i, name := range want {
		if typ.Field(i).Name != name {
			t.Fatal("root declaration order")
		}
	}
	expected := `{"Roots":[` + fmt.Sprintf(`{"ID":%s,"Owner":%s,"Identity":%s,"Filesystem":%s,"Workspace":%s,"Generation":%d,"Locator":%s,"Known":false,"Usage":%s,"CapacityCount":%d,"CapacityRefs":%s,"CapacityBinding":%s}`, epJSON(derived.Roots[0].ID), epJSON(derived.Roots[0].Owner), epJSON(derived.Roots[0].Identity), epJSON(derived.Roots[0].Filesystem), epJSON(derived.Roots[0].Workspace), derived.Roots[0].Generation, epJSON(derived.Roots[0].Locator), epJSON(derived.Roots[0].Usage), derived.Roots[0].CapacityCount, epJSON(derived.Roots[0].CapacityRefs), epJSON(derived.Roots[0].CapacityBinding)) + `],"Workspaces":[],"Filesystems":[],"CapacityConstraints":[]}`
	if string(epJSON(derived)) != expected {
		t.Fatal("all original nested fields")
	}
	good, _ := storageEnrollmentPeakRegistrationDigest(derived)
	for i := 0; i < typ.NumField(); i++ {
		var mutated storagebudget.Registration
		epInto(&mutated, derived)
		v := reflect.ValueOf(&mutated.Roots[0]).Elem().Field(i)
		switch v.Kind() {
		case reflect.Array:
			v.Index(0).Set(reflect.Zero(v.Index(0).Type()))
			if reflect.DeepEqual(mutated, derived) {
				if v.Index(0).Kind() == reflect.Uint8 {
					v.Index(0).SetUint(255)
				} else {
					v.Index(0).Field(0).SetUint(1)
				}
			}
		case reflect.Uint64, reflect.Uint8:
			v.SetUint(v.Uint() + 1)
		case reflect.Bool:
			v.SetBool(true)
		case reflect.String:
			v.SetString(v.String() + "<>&\u2028")
		}
		d, _ := storageEnrollmentPeakRegistrationDigest(mutated)
		if d == good {
			t.Fatal("field excluded from full Registration", typ.Field(i).Name)
		}
	}
	c.Workspaces[0].Definition.Limits = storagebudget.Limits{}
	workspace, err := storageEnrollmentPeakRegistration(c, [4][]uint16{{}, {0}, {}, {}}, q)
	if err != nil || !reflect.DeepEqual(workspace.Workspaces[0].Limits, c.Policy.Workspace) {
		t.Fatal("only original zero-Limits default", err)
	}
}

func epDispatch(e storageEnrollmentPeakEvidence, step byte, generation uint64, previous storagebudget.Digest) []byte {
	w := epBytes{}
	w.WriteString("DSKEPS1\x00")
	w.fixed(uint32(1))
	w.fixed(uint32(192))
	w.fixed(generation)
	w.fixed(e.Installation)
	w.fixed(e.QBinding)
	w.fixed(e.Bridge)
	w.fixed(previous)
	w.fixed(uint32(256))
	w.fixed(uint32(0))
	checksumAt := w.Len()
	w.Write(make([]byte, 32))
	b := make([]byte, 4096)
	copy(b, w.Bytes())
	body := epBytes{}
	body.fixed([4]byte{0, 1, 3, 7}[step])
	body.fixed(step)
	body.fixed(uint16(0))
	beforeRev, beforeEpoch := e.BeforeRevision, e.BeforeEpoch
	before := e.Checkpoint.Checkpoints[e.Checkpoint.Bank]
	after := storagebudget.Digest{}
	if step > 0 {
		after = e.Digests.Q[step-1]
		if step > 1 {
			before = e.Digests.Q[step-2]
			beforeRev += uint64(step - 1)
			beforeEpoch++
		}
	}
	body.fixed(beforeRev)
	body.fixed(beforeEpoch)
	body.fixed(before)
	body.fixed(after)
	body.fixed(e.Bridge)
	body.fixed(e.Digests.Origin)
	body.fixed(e.Controls[2])
	body.fixed(e.Controls[3])
	copy(b[192:], body.Bytes())
	digest := epHash("desk-enrollment-step-peak-v1", b)
	copy(b[checksumAt:checksumAt+32], digest[:])
	return b
}
func epMarker(e storageEnrollmentPeakEvidence, phase byte, previous storagebudget.Digest) storageEnrollmentPeakMarker {
	return storageEnrollmentPeakMarker{Phase: phase, Bank: e.Bank, Installation: e.Installation, QBinding: e.QBinding, Bridge: e.Bridge, Previous: previous, Before: e.Before, Inner: epHash("desk-enrollment-inner-catalog-peak-v1", e.Inner.Raw), Evidence: sha256.Sum256(e.Raw), Extent: e.Extent, Controls: e.Controls, CoveredEpoch: e.CoveredEpoch, Q0Revision: e.BeforeRevision, Q0Epoch: e.BeforeEpoch, Q3Revision: e.AfterRevision, Q3Epoch: e.AfterEpoch, Record: e.Record, Claim: e.Claim, Ready: e.Ready}
}
func epOriginal(f epComplete, e storageEnrollmentPeakEvidence) storageEnrollmentPeakOriginalComparison {
	c := storageEnrollmentPeakOriginalComparison{Ready: e.Ready, Project: e.Project, Catalogs: [2][]byte{ownerAdvisoryWire(f.before), ownerAdvisoryWire(f.before)}, Checkpoints: [2][]byte{f.cp[0], f.cp[0]}, Tickets: f.tickets}
	for i := range c.Targets {
		c.Targets[i] = e.Scope.target(byte(i + 1))
		c.Extents[i] = binary.LittleEndian.Uint64(e.Scope[1372+i*32:])
	}
	for i := range c.Hosts {
		for j := range c.Hosts[i] {
			c.Hosts[i][j] = make([]byte, 16384)
		}
	}
	return c
}
func TestStorageEnrollmentPeakEnvelopeDispatchAndQVectors(t *testing.T) {
	f := epEvidence(t, 1)
	e, err := storageEnrollmentPeakDecodeEvidence(f.raw, f.record, f.claim)
	if err != nil {
		t.Fatal(err)
	}
	original := epOriginal(f, e)
	if err := storageEnrollmentPeakCompareOriginal(e, original, f.record, f.claim); err != nil {
		t.Fatal("exact original comparison", err)
	}
	for step := byte(0); step <= 3; step++ {
		prior := storagebudget.Digest{}
		if step > 0 {
			prior = sha256.Sum256(epDispatch(e, step-1, uint64(step), storagebudget.Digest{1}))
		}
		if step == 1 {
			prior = sha256.Sum256(epDispatch(e, 0, 1, storagebudget.Digest{}))
		}
		raw := epDispatch(e, step, uint64(step)+1, prior)
		d, err := storageEnrollmentPeakDecodeDispatch(raw, e)
		if err != nil {
			t.Fatal("dispatch literal", step, err)
		}
		written, err := storageEnrollmentPeakEncodeDispatch(d, e)
		if err != nil || !bytes.Equal(written, raw) {
			t.Fatal("dispatch framing", err)
		}
		banks := [2][]byte{f.cp[0], f.cp[0]}
		if step > 0 {
			banks[step%2] = f.cp[step]
			banks[1-step%2] = f.cp[step-1]
		}
		q, err := storageEnrollmentPeakCompareQ(e, banks, f.tickets, d)
		if err != nil || q.Stage != int(step) || q.UnresolvedQAttempt {
			t.Fatal("actual exact after", step, q, err)
		}
		if step > 0 {
			beforeBanks := [2][]byte{f.cp[0], f.cp[0]}
			if step > 1 {
				beforeBanks[(step-1)%2] = f.cp[step-1]
				beforeBanks[step%2] = f.cp[step-2]
			}
			q, err := storageEnrollmentPeakCompareQ(e, beforeBanks, f.tickets, d)
			if err != nil || !q.UnresolvedQAttempt || q.Stage != int(step)-1 {
				t.Fatal("spent attempt plus actual prestate must stay unresolved", step, q, err)
			}
		}
	}
	// Exact normal envelope bytes are checked against a separately authored
	// prefix and marker, including the fixed compact 8320-byte representation.
	m := epMarker(e, 1, storagebudget.Digest{1})
	outer := storageEnrollmentPeakEnvelope{Kind: 2, BeforeCodec: 0, Sequence: 4, Scope: e.Scope, Marker: &m}
	normal, err := storageEnrollmentPeakEncodeEnvelope(outer, f.record, f.claim)
	if err != nil {
		t.Fatal(err)
	}
	prefix := make([]byte, 4160)
	prefix[0] = 2
	prefix[2] = 255
	epSet32(prefix[4:], 4096)
	epSet32(prefix[12:], 4096)
	copy(prefix[16:48], e.Scope[88:120])
	copy(prefix[48:64], e.Scope[8:24])
	copy(prefix[64:], e.Scope[:])
	marker := epBytes{}
	marker.fixed(uint32(7))
	marker.fixed(m.Phase)
	marker.fixed(m.Bank)
	marker.fixed(uint16(0))
	marker.fixed(m.Installation)
	marker.fixed(m.QBinding)
	marker.fixed(m.Bridge)
	for _, d := range []storagebudget.Digest{m.Previous, m.Before, m.Inner, m.Evidence} {
		marker.fixed(d)
	}
	marker.fixed(m.Extent)
	for _, p := range m.Controls {
		marker.fixed(p)
	}
	for _, n := range []uint64{m.CoveredEpoch, m.Q0Revision, m.Q0Epoch, m.Q3Revision, m.Q3Epoch} {
		marker.fixed(n)
	}
	for _, d := range []storagebudget.Digest{m.Record, m.Claim, m.Ready} {
		marker.fixed(d)
	}
	mb := make([]byte, 4096)
	copy(mb, marker.Bytes())
	expected := epFrame("DSKONP1\x00", append(prefix, mb...), 4, true)
	if !bytes.Equal(normal, expected) {
		t.Fatal("normal envelope oracle")
	}
	if _, err := storageEnrollmentPeakDecodeEnvelope(normal[:8320], f.record, f.claim, false); err != nil {
		t.Fatal("compact normal", err)
	}
	d0 := epDispatch(e, 0, 1, storagebudget.Digest{})
	input := storageEnrollmentPeakComparisonInput{Catalogs: [2][]byte{normal, normal}, Evidence: f.raw, Dispatch: [2][]byte{d0, d0}, CurrentCheckpoints: [2][]byte{f.cp[0], f.cp[0]}, CurrentTickets: f.tickets, Original: original}
	comparison, err := storageEnrollmentPeakCompare(input, f.record, f.claim)
	if err != nil || comparison.Q.Stage != 0 {
		t.Fatal("complete detached comparison", err)
	}
	d1 := epDispatch(e, 1, 2, sha256.Sum256(d0))
	input.Dispatch[1] = d1
	comparison, err = storageEnrollmentPeakCompare(input, f.record, f.claim)
	if err != nil || !comparison.Q.UnresolvedQAttempt {
		t.Fatal("no Q receipt manufactured", err)
	}
	bad := append([]byte{}, normal...)
	copy(bad, "DSKONE1\x00")
	if _, _, err := storageEnrollmentPeakDecodePair(bad, normal, f.record, f.claim); err != errStorageEnrollmentPeakMigration {
		t.Fatal("explicit older family migration cut", err)
	}
	for _, size := range []int{0, 63, 8319, 8388607} {
		if _, err := storageEnrollmentPeakDecodeEnvelope(normal[:size], f.record, f.claim, true); err == nil {
			t.Fatal("outer truncation", size)
		}
	}
	// Initial first Fence preserves exact full original v3 bytes and uses raw
	// iff the original payload is below the specified threshold.
	old := ownerAdvisoryWire(f.before)
	scope := e.Scope
	scope[321] = 0
	clear(scope[1080:1220])
	snapshot := storageEnrollmentPeakSnapshot{Kind: 0, Bank: 255, Full: sha256.Sum256(old), Inner: epHash("desk-enrollment-original-v3-inner-peak-v1", old[64:64+int(binary.LittleEndian.Uint32(old[24:]))]), Before: old}
	fence, err := storageEnrollmentPeakEncodeEnvelope(storageEnrollmentPeakEnvelope{Kind: 0, BeforeCodec: 0, Sequence: 2, Scope: scope, Snapshot: &snapshot}, f.record, f.claim)
	if err != nil {
		t.Fatal("first Fence", err)
	}
	if _, bank, err := storageEnrollmentPeakDecodePair(fence, old, f.record, f.claim); err != nil || bank != 0 {
		t.Fatal("first Fence plus exact retained v3", err)
	}
	captured, err := storageEnrollmentPeakEncodeEnvelope(storageEnrollmentPeakEnvelope{Kind: 1, BeforeCodec: 0, Sequence: 3, Scope: e.Scope, Snapshot: &snapshot}, f.record, f.claim)
	if err != nil {
		t.Fatal(err)
	}
	if _, bank, err := storageEnrollmentPeakDecodePair(fence, captured, f.record, f.claim); err != nil || bank != 1 {
		t.Fatal("capture transition", err)
	}
}

func epResealEvidence(raw []byte) {
	clear(raw[1189:1221])
	d := epHash("desk-enrollment-bank-peak-v1", raw)
	copy(raw[1189:1221], d[:])
}
func TestStorageEnrollmentPeakEvidenceRefusalVectors(t *testing.T) {
	f := epEvidence(t, 1)
	if _, err := storageEnrollmentPeakDecodeEvidence(f.raw, f.record, f.claim); err != nil {
		t.Fatal("positive twin", err)
	}
	// Header positions are independently summed: directory begins at613 and
	// checksum at1189. Fresh checksums ensure structural refusals are exercised.
	for _, v := range []struct {
		name   string
		offset int
		change func([]byte)
	}{{"features", 42, func(b []byte) { b[0] = 3 }}, {"reserved", 1221, func(b []byte) { b[0] = 1 }}, {"scope-tail", 8191, func(b []byte) { b[0] = 1 }}, {"header-tail", 8192, func(b []byte) { b[0] = 1 }}, {"kind", 613, func(b []byte) { epSet16(b, 2) }}, {"flags", 615, func(b []byte) { epSet16(b, 1) }}, {"count", 617, func(b []byte) { epSet32(b, 2) }}, {"overlap", 621, func(b []byte) { epSet64(b, 0) }}, {"overflow", 629, func(b []byte) { epSet64(b, math.MaxUint64) }}, {"maximum", 637, func(b []byte) { epSet64(b, 8388609) }}, {"digest", 645, func(b []byte) { b[0] ^= 1 }}, {"after-revision", 348, func(b []byte) { b[0] ^= 1 }}} {
		t.Run(v.name, func(t *testing.T) {
			bad := append([]byte{}, f.raw...)
			v.change(bad[v.offset:])
			epResealEvidence(bad)
			if _, err := storageEnrollmentPeakDecodeEvidence(bad, f.record, f.claim); err == nil {
				t.Fatal("structural mutation accepted")
			}
		})
	}
	for i := range f.sections {
		t.Run(fmt.Sprintf("section-%d-corrupt", i+1), func(t *testing.T) {
			bad := append([]byte{}, f.raw...)
			offset := f.evidence.Sections[i].Offset
			if len(f.sections[i]) != 0 {
				bad[offset] ^= 128
			} else {
				bad[offset-1] ^= 1
			}
			epResealEvidence(bad)
			if _, err := storageEnrollmentPeakDecodeEvidence(bad, f.record, f.claim); err == nil {
				t.Fatal("section mutation")
			}
		})
	}
	// F5: actual Q3 only exists in Armed/Completed; both rows are closed zero in
	// Phase0 regardless of their diagnostic state. No state grants authority.
	for phase := byte(0); phase < 3; phase++ {
		f := epEvidence(t, phase)
		e, err := storageEnrollmentPeakDecodeEvidence(f.raw, f.record, f.claim)
		if err != nil {
			t.Fatal(err)
		}
		for state := byte(0); state < 4; state++ {
			raw := append([]byte{}, f.sections[8]...)
			start := 174 + 24*int(e.Scope[326])
			for i := range e.Board.Groups {
				raw[start+i*115+38] = state
			}
			if _, err := storageEnrollmentPeakReadBoard(raw, e.Scope, phase, e.Digests, e.Inner.Catalog, len(e.Views), len(e.Projections), len(e.Witnesses)); err != nil {
				t.Fatal("diagnostic state has no authority meaning", state, err)
			}
		}
		for _, id := range []uint16{23, 58} {
			raw := append([]byte{}, f.sections[8]...)
			start := 174 + 24*int(e.Scope[326]) // board fixed366 at union8 ->198 at union1
			found := false
			for i := range e.Board.Groups {
				offset := start + i*115
				if binary.LittleEndian.Uint16(raw[offset:]) != id {
					continue
				}
				found = true
				if phase == 0 {
					raw[offset+75] = 1
				} else {
					clear(raw[offset+75 : offset+107])
				}
				if _, err := storageEnrollmentPeakReadBoard(raw, e.Scope, phase, e.Digests, e.Inner.Catalog, len(e.Views), len(e.Projections), len(e.Witnesses)); err == nil {
					t.Fatal("F5 forged or missing actual Q3", phase, id)
				}
			}
			if !found {
				t.Fatal("F5 fixture row offset", id)
			}
		}
		census := append([]byte{}, f.sections[7]...)
		if phase == 0 {
			census[0] = 1
		} else {
			clear(census[:32])
		}
		if _, err := storageEnrollmentPeakReadCensus(census, e.Predictions[1], phase); err == nil {
			t.Fatal("unarmed/measured Census boundary")
		}
	}
}

func TestStorageEnrollmentPeakMaximumStructuralVectors(t *testing.T) {
	// Max union8 preserves four original refs per parent and distinct FS fields.
	_, r, _ := ownerAdvisoryFixture(false)
	s := epScope(1, true, r, [2]uint64{}, [2]uint64{71303168, 71303168})
	s[326] = 8
	clear(s[440:1080])
	groups := epGroups(1, [2]uint64{71303168, 71303168}, 1, 255, 255, 0)
	var margin [2]storagebudget.CapacityVector
	for _, g := range groups {
		p := epEstimate(g)
		margin[g.Parent-1].Bytes += p.Bytes
		margin[g.Parent-1].Inodes += p.Inodes
	}
	for i := 0; i < 8; i++ {
		b := s[440+i*80 : 520+i*80]
		b[0] = byte(i + 1)
		epSet64(b[16:], 1)
		p := i / 4
		b[24] = byte(1 << p)
		epSet64(b[32:], margin[p].Bytes)
		epSet64(b[40:], margin[p].Inodes)
		b[48+p*16] = byte(p + 1)
	}
	epScopeHash(&s)
	if _, err := storageEnrollmentPeakDecodeScope(s[:]); err != nil {
		t.Fatal("8 union /4 each original parent", err)
	}
	shared := epScope(1, true, r, [2]uint64{}, [2]uint64{71303168, 71303168})
	shared[504] = 2
	epScopeHash(&shared)
	if _, err := storageEnrollmentPeakDecodeScope(shared[:]); err != nil {
		t.Fatal("shared constraint carries both complete FS mappings", err)
	}
	bad := s
	bad[326] = 9
	epScopeHash(&bad)
	if _, err := storageEnrollmentPeakDecodeScope(bad[:]); err == nil {
		t.Fatal("union overflow")
	}
	// Largest full Ticket body, preserving all16 components and16 changes.
	ticket := storagebudget.Ticket{Token: storagebudget.Token{Slot: 0, ID: storagebudget.ID{1}, Sequence: 1, Generation: 1}, State: storagebudget.Terminal, PolicyRevision: 1, Workflow: strings.Repeat("w", 128), Operation: strings.Repeat("o", 128), Owner: storagebudget.Digest{1}, Manifest: storagebudget.Digest{2}, ManifestLength: 524288, Evidence: storagebudget.Digest{3}, Components: []storagebudget.Component{}, Changes: []storagebudget.Change{}}
	for i := 0; i < 16; i++ {
		ticket.Components = append(ticket.Components, storagebudget.Component{Filesystem: storagebudget.ID{1}, FilesystemGeneration: 1, Root: storagebudget.ID{byte(i + 1)}, RootGeneration: 1, Owner: storagebudget.Digest{1}, Physical: storagebudget.Vector{Bytes: 1}, CapacityCount: 1, Capacity: [4]storagebudget.CapacityPromise{{Constraint: storagebudget.ID{1}, Generation: 1, Peak: storagebudget.CapacityVector{Bytes: 1}}}, CapacityBinding: storagebudget.Digest{1}})
		ticket.Changes = append(ticket.Changes, storagebudget.Change{})
	}
	body := epTicketBody(ticket)
	if len(body) != 8244 {
		t.Fatal("max body arithmetic", len(body))
	}
	decoded, err := storageEnrollmentPeakReadTicket(body)
	if err != nil || !reflect.DeepEqual(decoded, ticket) {
		t.Fatal("max full ticket", err)
	}
	if b, err := storageEnrollmentPeakWriteTicket(ticket); err != nil || !bytes.Equal(b, body) {
		t.Fatal("max handwritten body oracle", err)
	}
	if _, err := storageEnrollmentPeakReadTicket(append(body, 0)); err == nil {
		t.Fatal("trailing full Ticket byte")
	}
	// Full4224 dictionary entries:66 distinct generations for every typed slot.
	bodies := [][]byte{}
	for generation := uint64(0); generation < 66; generation++ {
		for slot := byte(0); slot < 64; slot++ {
			bodies = append(bodies, epTicketBody(storagebudget.Ticket{Token: storagebudget.Token{Slot: slot, Generation: generation}, Components: []storagebudget.Component{}, Changes: []storagebudget.Change{}}))
		}
	}
	sort.Slice(bodies, func(i, j int) bool { return bytes.Compare(bodies[i], bodies[j]) < 0 })
	w := epBytes{}
	for i, body := range bodies {
		w.fixed(uint16(i))
		w.fixed(uint16(len(body)))
		w.fixed(epHash("desk-enrollment-ticket-e1", body))
		w.Write(body)
		for w.Len()%8 != 0 {
			w.WriteByte(0)
		}
	}
	if tickets, err := storageEnrollmentPeakReadTickets(w.Bytes(), 4224); err != nil || len(tickets) != 4224 {
		t.Fatal("full4224 dictionary", err)
	}
	if _, err := storageEnrollmentPeakReadTickets(w.Bytes(), 4225); err == nil {
		t.Fatal("dictionary count cap")
	}
	// Exact maximum Census shape in physical order; this is a section-only
	// bound fixture and makes no claim that arbitrary rows are an original Q.
	q := &storageEnrollmentPeakQCheckpoint{}
	for i := range q.Roots {
		q.Roots[i].ID = storageEnrollmentPeakQID{1, byte(i >> 8), byte(i)}
		q.Roots[i].Generation = 1
	}
	for i := range q.Workspaces {
		q.Workspaces[i].ID = storageEnrollmentPeakQID{2, byte(i >> 8), byte(i)}
		q.Workspaces[i].Generation = 1
	}
	census := epCensus(q, false)
	if len(census) != 1228836 {
		t.Fatal("Census max bytes")
	}
	if _, err := storageEnrollmentPeakReadCensus(census, q, 0); err != nil {
		t.Fatal("max unarmed physical ordered shape", err)
	}
	badC := append([]byte{}, census...)
	badC[58] = 1
	if _, err := storageEnrollmentPeakReadCensus(badC, q, 0); err == nil {
		t.Fatal("non-measured Stage0 usage")
	}
	sum := uint64(65536)
	for _, m := range [9]uint64{8388608, 8388608, 35143680, 1081344, 8921088, 2113536, 2097152, 2097152, 65536} {
		sum += m
	}
	if sum != 68362240 || (sum+65535)&^uint64(65535) != 68419584 || 71303168-68419584 != 2883584 || 66*135168 != 8921088 {
		t.Fatal("unchanged whole evidence room")
	}
	if _, err := storageEnrollmentPeakReadProjection(make([]byte, 135169), storageOwnerCatalogV2{}, nil, storageOwnerRecordV2{}, storageOwnerClaimV2{}); err == nil {
		t.Fatal("projection concrete byte cap")
	}
}

func epOriginFixture(t *testing.T) (storageEnrollmentPeakEvidence, storageEnrollmentPeakOriginalComparison, epComplete) {
	f := epEvidence(t, 1)
	e, err := storageEnrollmentPeakDecodeEvidence(f.raw, f.record, f.claim)
	if err != nil {
		t.Fatal(err)
	}
	s := epScope(1, true, f.record, [2]uint64{}, [2]uint64{3145728, 3145728})
	copy(s[88:184], e.Scope[88:184])
	copy(s[440:456], e.Scope[440:456])
	epScopeHash(&s)
	e.Scope = s
	e.Extent = 3145728
	witnesses := epBytes{}
	origins := epBytes{}
	coverage := epBytes{}
	origins.fixed(uint16(64))
	coverage.fixed(uint16(64))
	actual := epOriginal(f, e)
	for slot := 0; slot < 64; slot++ {
		d := f.before.Roots[0].Definition
		fs := f.before.Filesystems[0].Definition
		component := storagebudget.Component{Filesystem: d.Filesystem, FilesystemGeneration: fs.Generation, Root: d.ID, RootGeneration: d.Generation, Owner: d.Owner, Category: 4, Physical: storagebudget.Vector{Bytes: 1}, CapacityCount: d.CapacityCount, CapacityBinding: d.CapacityBinding}
		for i := 0; i < int(d.CapacityCount); i++ {
			component.Capacity[i] = storagebudget.CapacityPromise{Constraint: d.CapacityRefs[i].Constraint, Generation: d.CapacityRefs[i].Generation, Peak: storagebudget.CapacityVector{Bytes: 1}}
		}
		prepared := storageIntentRecord{Slot: uint16(slot), Generation: 1, Phase: storageIntentPrepared, OperationID: storagebudget.ID{byte(slot + 1)}, CASBinding: storagebudget.Digest{1}, AdmissionEpoch: 1, NewLength: 1, Components: []storagebudget.Component{component}, RootIdentities: []storagebudget.Digest{d.Identity}, FilesystemIdentities: []storagebudget.Digest{fs.Identity}, Changes: []storagebudget.Change{{}}, Target: "target", Stage: "stage", Workflow: storageHostWorkflow, Operation: fmt.Sprintf("handwritten:%d", slot), Locators: []string{d.Locator}}
		// These are foreign pinned J2 pages, never peak-codec output or live writes.
		p0, err := storageEncodeIntent(prepared, f.record.Installation, f.record.QBinding)
		if err != nil {
			t.Fatal(err)
		}
		rejected := prepared
		rejected.Generation = 2
		rejected.Phase = storageIntentRejected
		rejected.PreviousPageDigest = sha256.Sum256(p0)
		rejected.Outcome = storageIntentNotAdmitted
		rejected.Rejection = storageIntentQuota
		p1, err := storageEncodeIntent(rejected, f.record.Installation, f.record.QBinding)
		if err != nil {
			t.Fatal(err)
		}
		actual.Hosts[slot] = [2][]byte{p0, p1}
		for bank, page := range [][]byte{p0, p1} {
			h := epBytes{}
			h.fixed(uint16(slot))
			h.fixed(byte(bank))
			h.fixed(byte(2))
			h.fixed(uint64(bank + 1))
			h.fixed(prepared.OperationID)
			h.fixed(sha256.Sum256(page))
			h.fixed(uint16(slot))
			h.fixed(bank == 1)
			phase := storageIntentPrepared
			if bank == 1 {
				phase = storageIntentRejected
			}
			h.fixed(byte(phase))
			h.fixed(byte(0))
			h.fixed(byte(0))
			header := make([]byte, 128)
			copy(header, h.Bytes())
			witnesses.Write(header)
			witnesses.Write(page)
		}
		origin := epBytes{}
		origin.fixed(storagebudget.ID{byte(slot + 1)})
		origin.fixed(storagebudget.ID{7})
		origin.fixed(uint64(1))
		origin.fixed(uint64(10))
		origin.fixed(uint16(0))
		origin.fixed(uint16(0))
		origin.fixed(uint64(1) << slot)
		origin.fixed(uint16(2))
		origin.fixed(uint16(2 * slot))
		origin.fixed(uint16(2*slot + 1))
		od := epHash("desk-enrollment-origin-record-e1", origin.Bytes())
		origins.Write(origin.Bytes())
		origins.fixed(od)
		coverage.fixed(uint16(slot))
		coverage.fixed(uint16(slot))
		coverage.fixed(uint16(2*slot + 1))
		coverage.fixed(uint64(2))
		coverage.fixed(prepared.OperationID)
		coverage.fixed(sha256.Sum256(p1))
		coverage.fixed(uint64(2))
		coverage.fixed(e.Bridge)
		coverage.fixed(byte(1))
	}
	if witnesses.Len() != 2113536 {
		t.Fatal("128 exact witness bound")
	}
	board := epBoard(s, f.source, 1, e.Digests.Q, e.Digests.Origin)
	groupEnd := 174 + 24*int(s[326]) + len(epGroups(1, [2]uint64{3145728, 3145728}, 1, 255, 255, 0))*115
	tailEnd := groupEnd + 4 + 96 + 10 + 1 + len(epSourceBytes(f.source))
	joined := append([]byte{}, board[:groupEnd]...)
	joined = append(joined, origins.Bytes()...)
	joined = append(joined, coverage.Bytes()...)
	joined = append(joined, board[groupEnd+4:tailEnd]...)
	newBoard := make([]byte, 65536)
	copy(newBoard, joined)
	e.Sections[5].Count = 128
	e.Sections[5].Raw = append([]byte{}, witnesses.Bytes()...)
	e.Sections[8].Raw = newBoard
	last := uint64(65536)
	for i := range e.Sections {
		last = (last + 7) &^ uint64(7)
		e.Sections[i].Offset = last
		e.Sections[i].Length = uint64(len(e.Sections[i].Raw))
		e.Sections[i].Digest = sha256.Sum256(e.Sections[i].Raw)
		last += e.Sections[i].Length
	}
	e.Packed = last - 65536
	raw := epEvidenceBytes(e)
	full, err := storageEnrollmentPeakDecodeEvidence(raw, f.record, f.claim)
	if err != nil || len(full.Board.Origins) != 64 || len(full.Board.Coverage) != 64 || len(full.Witnesses) != 128 {
		t.Fatal("full64 origin/128 witness complete evidence", err)
	}
	if err := storageEnrollmentPeakCompareOriginal(full, actual, f.record, f.claim); err != nil {
		t.Fatal("all128 exact original pages", err)
	}
	encodedWitnesses, err := storageEnrollmentPeakWriteWitnesses(full.Witnesses, full.Installation, full.QBinding)
	if err != nil || !bytes.Equal(encodedWitnesses, witnesses.Bytes()) {
		t.Fatal("handwritten maximum witness bytes", err)
	}
	return full, actual, f
}
func TestStorageEnrollmentPeakMaximumOriginWitnessJoins(t *testing.T) {
	full, _, _ := epOriginFixture(t)
	if !storageEnrollmentPeakRetainsOrigins(full, full) {
		t.Fatal("retained complete origins")
	}
	retainedBad := full
	retainedBad.Board.Origins = append([]storageEnrollmentPeakOrigin{}, full.Board.Origins...)
	retainedBad.Board.Origins[0].Bridge[0]++
	if storageEnrollmentPeakRetainsOrigins(retainedBad, full) {
		t.Fatal("retained original source origin changed")
	}
	for _, mutation := range []string{"origin-view", "origin-mask", "coverage-generation", "coverage-bridge", "selected-bank", "source-map", "raw-page", "ticket-absence"} {
		bad := full
		bad.Board.Origins = append([]storageEnrollmentPeakOrigin{}, full.Board.Origins...)
		bad.Board.Coverage = append([]storageEnrollmentPeakCoverage{}, full.Board.Coverage...)
		bad.Witnesses = append([]storageEnrollmentPeakWitness{}, full.Witnesses...)
		bad.Views = append([]storageEnrollmentPeakView{}, full.Views...)
		switch mutation {
		case "origin-view":
			bad.Views[0].Epoch++
		case "origin-mask":
			bad.Board.Origins[0].Slots |= 2
		case "coverage-generation":
			bad.Board.Coverage[0].Generation++
		case "coverage-bridge":
			bad.Board.Coverage[0].Bridge[0]++
		case "selected-bank":
			bad.Witnesses[1].Selected = false
		case "source-map":
			bad.Witnesses[1].Core.RootIdentities = append([]storagebudget.Digest{}, bad.Witnesses[1].Core.RootIdentities...)
			bad.Witnesses[1].Core.RootIdentities[0][0] ^= 1
		case "raw-page":
			bad.Witnesses[1].Page = append([]byte{}, bad.Witnesses[1].Page...)
			bad.Witnesses[1].Page[76] ^= 1
		case "ticket-absence":
			core := bad.Witnesses[1].Core
			bad.Views[0].Slots[0].Ticket = storagebudget.Ticket{State: storagebudget.Reserved, Workflow: storageHostWorkflow, Operation: storageIntentOperation(core), Owner: full.QBinding, Components: core.Components}
		}
		if storageEnrollmentPeakProofs(&bad) {
			t.Fatal("origin/witness/source/coverage mutation", mutation)
		}
	}
}
