package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// These byte layouts are authored here independently. No expected wrapper,
// lineage block, checksum or publication table is obtained from its encoder.
const compositionEmptyJSON = `{"LineagePlans":[],"LineageCompletions":[],"LineageContinuations":[],"LineageStampCompletions":[]}`

func compositionTestBlock(j []byte, order [4][]storagebudget.ID, initial []storageCompositionInitial) []byte {
	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(j)))
	out.Write(j)
	for _, ids := range order {
		_ = binary.Write(&out, binary.LittleEndian, uint16(len(ids)))
	}
	for _, ids := range order {
		for _, id := range ids {
			out.Write(id[:])
		}
	}
	for _, v := range initial {
		out.WriteByte(v.Mode)
		out.WriteByte(v.ExpectedOld.Backend)
		out.Write(v.ExpectedOld.VolumeUUID[:])
		_ = binary.Write(&out, binary.LittleEndian, v.ExpectedOld.Object)
		_ = binary.Write(&out, binary.LittleEndian, v.ExpectedOld.Incarnation)
		out.WriteByte(v.ExpectedOld.IncarnationKind)
		out.WriteByte(v.ExpectedOld.ObjectKind)
	}
	return out.Bytes()
}

func compositionTestSeal(raw []byte) {
	n := int(binary.LittleEndian.Uint32(raw[24:28]))
	h := sha256.New()
	h.Write(raw[:32])
	h.Write(raw[64 : 64+n])
	copy(raw[32:64], h.Sum(nil))
}

func compositionTestFrame(v storageCompositionBank, core, lineage []byte) []byte {
	// Full expected image, including independently fixed prefix offsets and tail.
	n := 64 + int(binary.LittleEndian.Uint32(core[24:28]))
	out := make([]byte, 8388608)
	copy(out[:8], []byte{'D', 'S', 'K', 'O', 'N', 'L', '1', 0})
	binary.LittleEndian.PutUint32(out[8:12], 1)
	binary.LittleEndian.PutUint32(out[12:16], 64)
	binary.LittleEndian.PutUint64(out[16:24], v.Sequence)
	binary.LittleEndian.PutUint32(out[24:28], uint32(256+n+len(lineage)))
	p := out[64:320]
	binary.LittleEndian.PutUint16(p[:2], 1)
	p[2], p[3], p[4], p[5] = v.CoreCodec, v.Publication, v.Transition, v.BeforeBank
	binary.LittleEndian.PutUint32(p[8:12], uint32(n))
	binary.LittleEndian.PutUint32(p[12:16], uint32(len(lineage)))
	copy(p[16:32], v.TransitionID[:])
	copy(p[32:64], v.Previous[:])
	d := sha256.Sum256(core)
	copy(p[64:96], d[:])
	d = sha256.Sum256(lineage)
	copy(p[96:128], d[:])
	copy(p[128:160], v.FirstBase[:])
	copy(p[160:225], v.BeforePeer[:])
	copy(out[320:320+n], core[:n])
	copy(out[320+n:], lineage)
	compositionTestSeal(out)
	return out
}

func compositionTestV3(t *testing.T) (storageCompositionBank, storageOwnerRecordV2, storageOwnerClaimV2) {
	t.Helper()
	c, r, cl := ownerAdvisoryFixture(false)
	c.Sequence, c.Phase, c.PreviousCatalogDigest = 12, "active", storagebudget.Digest{33}
	core := ownerAdvisoryWire(c) // existing handwritten original-core input
	l := storageCompositionEmptyLineage()
	block := compositionTestBlock([]byte(compositionEmptyJSON), [4][]storagebudget.ID{}, nil)
	v := storageCompositionBank{Sequence: 40, CoreCodec: 1, Publication: 2, Transition: 1, BeforeBank: 0, TransitionID: storagebudget.ID{17}, Previous: storageCompositionChecksum{18}, CoreFull: storageCompositionCoreDigest(sha256.Sum256(core)), LineageDigest: sha256.Sum256(block), FirstBase: storagebudget.Digest{19}, Core: core, Lineage: l}
	copy(v.BeforePeer[1:33], v.CoreFull[:])
	copy(v.BeforePeer[33:65], c.PreviousCatalogDigest[:])
	return v, r, cl
}

func TestStorageCompositionEmptyWireAndCorePhysicalDigests(t *testing.T) {
	v, r, cl := compositionTestV3(t)
	lineage := compositionTestBlock([]byte(compositionEmptyJSON), [4][]storagebudget.ID{}, nil)
	if len(lineage) != 110 {
		t.Fatal("independent empty block width", len(lineage))
	}
	expected := compositionTestFrame(v, v.Core, lineage)
	actual, err := v.encode(r, cl)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatal("complete independent composition image", err)
	}
	got, err := storageCompositionDecode(expected, r, cl)
	if err != nil || !bytes.Equal(got.Core, v.Core) || got.CoreFull != v.CoreFull || got.Sequence != 40 || got.LineageDigest != sha256.Sum256(lineage) {
		t.Fatal("exact core reconstruction", err)
	}
	physical := storageCompositionPhysicalDigest(sha256.Sum256(expected))
	if physical == storageCompositionPhysicalDigest(v.CoreFull) || bytes.Equal(expected[32:64], v.Core[32:64]) {
		t.Fatal("physical/core/checksum operands conflated")
	}
	if _, err := storageOwnerDecodeCatalogPairV3(v.Core, expected, r, cl); err == nil {
		t.Fatal("actual old V3 reader crossed first wrapper")
	}
	if _, _, err := storageEnrollmentPeakDecodePair(v.Core, expected, r, cl); err == nil {
		t.Fatal("actual old Peak reader crossed unknown wrapper")
	}
	for _, at := range []int{0, 8, 12, 28, 64, 70, 72, 76, 128, 160, 224, 225, 289, 320, len(expected) - 1} {
		t.Run(fmt.Sprint(at), func(t *testing.T) {
			bad := append([]byte{}, expected...)
			bad[at] ^= 1
			// A valid outer checksum must not excuse malformed prefix/core/tail.
			compositionTestSeal(bad)
			if _, err := storageCompositionDecode(bad, r, cl); err == nil {
				t.Fatal("accepted altered independent field", at)
			}
		})
	}
	for _, mutate := range []func([]byte){
		func(b []byte) { clear(b[16:24]) },
		func(b []byte) { clear(b[80:96]) },
		func(b []byte) { clear(b[96:128]) },
		func(b []byte) { b[67] = 3 }, // CorePublication is not a V3 phase
		func(b []byte) { b[68] = 7 },
		func(b []byte) { b[69] = 2 },
		func(b []byte) { b[224] = 2 }, // unknown before-peer mode
		func(b []byte) { clear(b[225:257]) },
		func(b []byte) { clear(b[192:224]) }, // first-base commitment missing
	} {
		bad := append([]byte{}, expected...)
		mutate(bad)
		compositionTestSeal(bad)
		if _, err := storageCompositionDecode(bad, r, cl); err == nil {
			t.Fatal("accepted unsupported closed combination")
		}
	}
}

func TestStorageCompositionFirstBasePhysicalFinalization(t *testing.T) {
	v, r, cl := compositionTestV3(t)
	v.Publication = 1
	block := compositionTestBlock([]byte(compositionEmptyJSON), [4][]storagebudget.ID{}, nil)
	p := compositionTestFrame(v, v.Core, block)
	active := v
	active.Sequence = 41
	active.Publication = 2
	copy(active.Previous[:], p[32:64])
	a := compositionTestFrame(active, active.Core, block)
	for _, pair := range [][2][]byte{{p, a}, {a, p}} {
		got, err := storageCompositionReadPair(pair[0], pair[1], r, cl)
		if err != nil || got.RecoveryOnly || got.Duplicated || got.Banks[got.Selected].Sequence != 41 {
			t.Fatal("exact first-base P/A", err)
		}
	}
	got, err := storageCompositionReadPair(p, p, r, cl)
	if err != nil || !got.Duplicated || !got.RecoveryOnly {
		t.Fatal("both Prepared is provisional", err)
	}
	for _, mutate := range []func([]byte){func(b []byte) { binary.LittleEndian.PutUint64(b[16:24], 42) }, func(b []byte) { b[96] ^= 1 }, func(b []byte) { b[192] ^= 1 }} {
		bad := append([]byte{}, a...)
		mutate(bad)
		compositionTestSeal(bad)
		if _, err := storageCompositionReadPair(p, bad, r, cl); err == nil {
			t.Fatal("accepted changed physical finalization")
		}
	}
	if _, err := storageCompositionSequence(math.MaxUint64, 1); err == nil {
		t.Fatal("sequence wrap")
	}
}

// Independent plan wire DTOs. Native and nested field orders are authored here;
// their values are fixture inputs, never JSON from the production row encoder.
type compositionTestNative struct {
	Backend                     uint8
	VolumeUUID                  [16]byte
	Object, Incarnation         uint64
	IncarnationKind, ObjectKind uint8
}
type compositionTestWorkspace struct {
	ID         storagebudget.ID
	Generation uint64
	Digest     storagebudget.Digest
}
type compositionTestPrivate struct {
	ID                     storagebudget.ID
	Generation             uint64
	Identity, SourceDigest storagebudget.Digest
}
type compositionTestPlan struct {
	PlanID, Installation                                                      storagebudget.ID
	QBinding                                                                  storagebudget.Digest
	Workspace                                                                 compositionTestWorkspace
	PrivateRoot                                                               compositionTestPrivate
	ProjectObject, PrivateObject, ConfigObject                                compositionTestNative
	ProviderRelationDigest                                                    storagebudget.Digest
	HistoryIdentity, AccountingOwnerKey, IdentityInitial, IdentityFromCleared []byte
	CompletionRoom, ContinuationRoom, StampRoom                               uint32
}

func compositionTestPlanDTO(p storageOwnerV5LineagePlan) compositionTestPlan {
	native := func(n storageOwnerPersistentObject) compositionTestNative {
		return compositionTestNative{n.Backend, n.VolumeUUID, n.Object, n.Incarnation, n.IncarnationKind, n.ObjectKind}
	}
	return compositionTestPlan{p.PlanID, p.Installation, p.QBinding, compositionTestWorkspace{p.Workspace.ID, p.Workspace.Generation, p.Workspace.Digest}, compositionTestPrivate{p.PrivateRoot.ID, p.PrivateRoot.Generation, p.PrivateRoot.Identity, p.PrivateRoot.SourceDigest}, native(p.ProjectObject), native(p.PrivateObject), native(p.ConfigObject), p.ProviderRelationDigest, p.HistoryIdentity, p.AccountingOwnerKey, p.IdentityInitial, p.IdentityFromCleared, p.CompletionRoom, p.ContinuationRoom, p.StampRoom}
}
func compositionTestPlansJSON(plans ...storageOwnerV5LineagePlan) []byte {
	rows := struct {
		LineagePlans            []compositionTestPlan
		LineageCompletions      []int
		LineageContinuations    []int
		LineageStampCompletions []int
	}{[]compositionTestPlan{}, []int{}, []int{}, []int{}}
	for _, p := range plans {
		rows.LineagePlans = append(rows.LineagePlans, compositionTestPlanDTO(p))
	}
	b, err := json.Marshal(rows)
	if err != nil {
		panic(err)
	}
	return b
}

func TestStorageCompositionHistoricalLineageAppendOrder(t *testing.T) {
	input, _, _ := ownerV5Fixture(t, false)
	first, second := input.LineagePlans[0], input.LineagePlans[0]
	first.PlanID, second.PlanID = storagebudget.ID{9}, storagebudget.ID{3}
	var err error
	first.CompletionRoom, first.StampRoom, first.ContinuationRoom, err = storageOwnerV5Rooms(first)
	if err != nil {
		t.Fatal(err)
	}
	second.CompletionRoom, second.StampRoom, second.ContinuationRoom, err = storageOwnerV5Rooms(second)
	if err != nil {
		t.Fatal(err)
	}
	old := storageCompositionEmptyLineage()
	old.Rows.LineagePlans = []storageOwnerV5LineagePlan{first}
	old.Order[0] = []storagebudget.ID{{9}}
	old.Initial = []storageCompositionInitial{{Mode: 1}}
	current := storageCompositionEmptyLineage()
	current.Rows.LineagePlans = []storageOwnerV5LineagePlan{second, first}
	current.Order[0] = []storagebudget.ID{{9}, {3}}
	native := storageOwnerPersistentObject{Backend: second.PrivateObject.Backend, VolumeUUID: second.PrivateObject.VolumeUUID, Object: 99, Incarnation: 1, IncarnationKind: second.PrivateObject.IncarnationKind, ObjectKind: 2}
	current.Initial = []storageCompositionInitial{{Mode: 1}, {Mode: 2, ExpectedOld: native}}
	expectedOld := compositionTestBlock(compositionTestPlansJSON(first), [4][]storagebudget.ID{{{9}}}, []storageCompositionInitial{{Mode: 1}})
	expectedCurrent := compositionTestBlock(compositionTestPlansJSON(second, first), [4][]storagebudget.ID{{{9}, {3}}}, []storageCompositionInitial{{Mode: 1}, {Mode: 2, ExpectedOld: native}})
	for _, tc := range []struct {
		l        storageCompositionLineage
		expected []byte
	}{{old, expectedOld}, {current, expectedCurrent}} {
		actual, err := tc.l.encode()
		if err != nil || !bytes.Equal(actual, tc.expected) {
			t.Fatal("independent nonempty lineage bytes", err)
		}
		decoded, err := storageCompositionReadLineage(tc.expected)
		if err != nil || !decoded.valid() {
			t.Fatal("strict lineage decode", err)
		}
	}
	prior, err := current.prefix([4]uint16{1, 0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	priorRaw, err := prior.encode()
	if err != nil || !bytes.Equal(priorRaw, expectedOld) {
		t.Fatal("exact history lost after lower PlanID append", err)
	}
	size, err := current.closureSize()
	if err != nil || size-len(expectedCurrent) != int(first.CompletionRoom+second.CompletionRoom)+1+32 {
		t.Fatal("pending C rows and binary IDs not reserved", size, err)
	}
	for _, count := range [][4]uint16{{3, 0, 0, 0}, {0, 1, 0, 0}, {1, 0, 1, 0}, {1, 0, 0, 1}} {
		if _, err := current.prefix(count); err == nil {
			t.Fatal("invalid historical prefix", count)
		}
	}
	low := storageCompositionBank{Lineage: old}
	high := storageCompositionBank{Lineage: current, Transition: 3, TransitionID: storagebudget.ID{3}}
	if !storageCompositionLineageAppend(low, high) {
		t.Fatal("append before JSON sort position rejected")
	}
	changed := high
	changed.Lineage.Initial = append([]storageCompositionInitial{}, high.Lineage.Initial...)
	changed.Lineage.Initial[0] = storageCompositionInitial{Mode: 2, ExpectedOld: native}
	if storageCompositionLineageAppend(low, changed) {
		t.Fatal("recaptured original InitialOperand")
	}
	changed = high
	changed.Lineage.Order[0] = []storagebudget.ID{{3}, {9}}
	if storageCompositionLineageAppend(low, changed) {
		t.Fatal("rewrote immutable chronological list")
	}
	if _, err := storageCompositionInitialBinding(first, storageCompositionInitial{Mode: 1}); err == nil {
		t.Fatal("absent-create became original CAS")
	}
	binding, err := storageCompositionInitialBinding(second, current.Initial[1])
	if err != nil {
		t.Fatal(err)
	}
	mutated := current.Initial[1]
	mutated.ExpectedOld.Object++
	other, err := storageCompositionInitialBinding(second, mutated)
	if err != nil || other == binding {
		t.Fatal("CAS native is not bound", err)
	}
}

func TestStorageCompositionLineageClosedWire(t *testing.T) {
	for _, j := range []string{
		`{"LineagePlans":null,"LineageCompletions":[],"LineageContinuations":[],"LineageStampCompletions":[]}`,
		`{"LineageCompletions":[],"LineagePlans":[],"LineageContinuations":[],"LineageStampCompletions":[]}`,
		`{"LineagePlans":[],"LineagePlans":[],"LineageContinuations":[],"LineageStampCompletions":[]}`,
		`{"LineagePlans":[],"LineageCompletions":[],"LineageContinuations":[],"LineageStampCompletions":[],"Extra":0}`,
		compositionEmptyJSON + " ",
	} {
		if _, err := storageCompositionReadLineage(compositionTestBlock([]byte(j), [4][]storagebudget.ID{}, nil)); err == nil {
			t.Fatal("accepted malformed lineage object", j)
		}
	}
	valid := compositionTestBlock([]byte(compositionEmptyJSON), [4][]storagebudget.ID{}, nil)
	for _, at := range []int{0, 1, 2, 3, len(valid) - 8, len(valid) - 1} {
		bad := append([]byte{}, valid...)
		bad[at] ^= 1
		if _, err := storageCompositionReadLineage(bad); err == nil {
			t.Fatal("accepted altered count/length", at)
		}
	}
	if _, err := storageCompositionReadLineage(append(valid, 0)); err == nil {
		t.Fatal("trailing lineage byte")
	}
}

func TestStorageCompositionE1PublicationTableAndUnchangedCosts(t *testing.T) {
	// Each literal bank is the expected b=0 bank. b=1 swaps actual a/b; no
	// expected table or cost is obtained from the production shape helper.
	want := []struct {
		path                           byte
		group, completion              uint16
		bank, kind, phase, offset, pub byte
		duplicate                      bool
	}{
		{1, 60, 80, 1, 0, 255, 1, 3, false}, {1, 61, 81, 0, 0, 255, 1, 3, true}, {1, 66, 86, 0, 1, 255, 2, 3, false}, {1, 11, 46, 1, 2, 1, 3, 3, false}, {1, 12, 47, 0, 2, 1, 3, 3, true}, {1, 13, 48, 0, 2, 2, 4, 3, false}, {1, 14, 49, 1, 2, 3, 5, 2, false}, {1, 15, 50, 0, 2, 3, 5, 2, true},
		{2, 9, 44, 1, 0, 255, 1, 3, false}, {2, 10, 45, 0, 0, 255, 1, 3, true}, {2, 11, 46, 0, 2, 1, 2, 3, false}, {2, 12, 47, 1, 2, 1, 2, 3, true}, {2, 13, 48, 1, 2, 2, 3, 3, false}, {2, 14, 49, 0, 2, 3, 4, 2, false}, {2, 15, 50, 1, 2, 3, 4, 2, true},
		{3, 9, 44, 1, 0, 255, 1, 3, false}, {3, 10, 45, 0, 0, 255, 1, 3, true}, {3, 67, 87, 0, 2, 0, 2, 3, false}, {3, 68, 88, 1, 2, 0, 2, 3, true}, {3, 11, 46, 1, 2, 1, 3, 3, false}, {3, 12, 47, 0, 2, 1, 3, 3, true}, {3, 13, 48, 0, 2, 2, 4, 3, false}, {3, 14, 49, 1, 2, 3, 5, 2, false}, {3, 15, 50, 0, 2, 3, 5, 2, true},
	}
	for _, w := range want {
		for b := byte(0); b < 2; b++ {
			got, err := storageCompositionPublication(w.path, b, w.group)
			expected := storageCompositionPublicationShape{w.group, w.completion, w.bank ^ b, w.kind, w.phase, w.offset, w.pub, w.duplicate}
			if err != nil || !reflect.DeepEqual(got, expected) {
				t.Fatal("closed publication shape", w.path, b, w.group, got, err)
			}
		}
	}
	for path := byte(1); path <= 3; path++ {
		for b := byte(0); b < 2; b++ {
			groups := storageEnrollmentPeakPlanShape(storageEnrollmentPeakScheduleShape{Path: path, CatalogBank: b, Final: [2]uint64{262144, 262144}})
			count, rawB, policy, inodes := 0, uint64(0), uint64(0), uint64(0)
			for _, g := range groups {
				if g.ID == 14 || g.ID == 49 || g.ID == 15 || g.ID == 50 {
					count++
					rawB += g.Bytes
					policy += 2*((g.Bytes+65535)/65536)*65536 + 65536*(g.Files+g.Directories+g.MetadataOps+4)
					inodes += g.Files + g.Directories + 2
					if g.MetadataOps != 16 || g.Files != 0 || g.Directories != 0 {
						t.Fatal("changed existing final group")
					}
				}
			}
			if count != 4 || rawB != 16777216 || policy != 38797312 || inodes != 8 {
				t.Fatal("extra completion/normalization budget", count, rawB, policy, inodes)
			}
		}
	}
	for _, v := range []struct {
		path, b byte
		g       uint16
	}{{0, 0, 14}, {4, 0, 14}, {1, 2, 14}, {2, 0, 66}, {1, 0, 67}, {3, 0, 16}} {
		if _, err := storageCompositionPublication(v.path, v.b, v.g); err == nil {
			t.Fatal("unknown path/bank/group accepted", v)
		}
	}
}

func TestStorageCompositionExactPhysicalBeforeProjection(t *testing.T) {
	v, r, cl := compositionTestV3(t)
	v.Publication = 1
	block := compositionTestBlock([]byte(compositionEmptyJSON), [4][]storagebudget.ID{}, nil)
	p := compositionTestFrame(v, v.Core, block)
	active := v
	active.Publication = 2
	active.Sequence++
	copy(active.Previous[:], p[32:64])
	a := compositionTestFrame(active, active.Core, block)
	input, _, _ := ownerV5Fixture(t, false)
	retained := storageCompositionEmptyLineage()
	retained.Rows.LineagePlans = input.LineagePlans
	retained.Order[0] = []storagebudget.ID{input.LineagePlans[0].PlanID}
	retained.Initial = []storageCompositionInitial{{Mode: 1}}
	for _, pair := range [][2][]byte{{a, p}, {p, a}} {
		projection, err := storageCompositionCaptureProjection(pair[0], pair[1], r, cl)
		if err != nil {
			t.Fatal(err)
		}
		var expected [712]byte
		copy(expected[:320], pair[0][:320])
		copy(expected[320:640], pair[1][:320])
		for i, raw := range pair {
			digest := sha256.Sum256(raw)
			copy(expected[648+32*i:680+32*i], digest[:])
		}
		if !bytes.Equal(projection[:], expected[:]) {
			t.Fatal("independent projection layout")
		}
		// Later retained P does not alter the zero-count historical JSON, logs or
		// companion bytes. Both actual old PHYSICAL images must return exactly.
		got, err := projection.restore(v.Core, retained, r, cl)
		if err != nil || !bytes.Equal(got[0], pair[0]) || !bytes.Equal(got[1], pair[1]) {
			t.Fatal("old physical before changed after lineage append", err)
		}
		for _, at := range []int{0, 16, 64, 72, 128, 160, 225, 319, 320, 640, 648, 680, 711} {
			bad := projection
			bad[at] ^= 1
			if _, err := bad.restore(v.Core, retained, r, cl); err == nil {
				t.Fatal("accepted physical prefix/cutoff/fullSHA mutation", at)
			}
		}
	}
}

// An independently packed existing E1 marker is an input to the new wrapper
// comparison. This is synthetic comparison data; it proves no native write,
// selected-evidence persistence or actual Q/source lifetime.
func compositionTestMarkerCore(e storageEnrollmentPeakEvidence, phase byte, sequence uint64, previous storagebudget.Digest) []byte {
	w := epBytes{}
	w.fixed(uint32(7))
	w.fixed(phase)
	w.fixed(e.Bank)
	w.fixed(uint16(0))
	w.fixed(e.Installation)
	w.fixed(e.QBinding)
	w.fixed(e.Bridge)
	w.fixed(previous)
	w.fixed(e.Before)
	w.fixed(epHash("desk-enrollment-inner-catalog-peak-v1", e.Inner.Raw))
	w.fixed(sha256.Sum256(e.Raw))
	w.fixed(e.Extent)
	for _, native := range e.Controls {
		w.fixed(native)
	}
	w.fixed(e.CoveredEpoch)
	w.fixed(e.BeforeRevision)
	w.fixed(e.BeforeEpoch)
	w.fixed(e.AfterRevision)
	w.fixed(e.AfterEpoch)
	w.fixed(e.Record)
	w.fixed(e.Claim)
	w.fixed(e.Ready)
	marker := make([]byte, 4096)
	copy(marker, w.Bytes())
	payload := make([]byte, 8256)
	payload[0], payload[2] = 2, 255
	if e.Scope.path() != 1 {
		payload[1] = 1
	}
	binary.LittleEndian.PutUint32(payload[4:8], 4096)
	binary.LittleEndian.PutUint32(payload[12:16], 4096)
	copy(payload[16:48], e.Scope[88:120])
	copy(payload[48:64], e.Scope[8:24])
	copy(payload[64:4160], e.Scope[:])
	copy(payload[4160:], marker)
	return epFrame("DSKONP1\x00", payload, sequence, true)
}

func compositionTestProjection(pair [2][]byte) storageCompositionProjection {
	var p storageCompositionProjection
	for bank, raw := range pair {
		copy(p[320*bank:320*(bank+1)], raw[:320])
		d := sha256.Sum256(raw)
		copy(p[648+32*bank:680+32*bank], d[:])
	}
	return p // the independently authored fixtures below have zero historical rows
}

func compositionTestBasePair(core []byte, codec byte, previous storagebudget.Digest) ([2][]byte, storageCompositionProjection) {
	block := compositionTestBlock([]byte(compositionEmptyJSON), [4][]storagebudget.ID{}, nil)
	v := storageCompositionBank{Sequence: 40, CoreCodec: codec, Publication: 1, Transition: 1, TransitionID: storagebudget.ID{17}, Previous: storageCompositionChecksum{18}, FirstBase: storagebudget.Digest{19}}
	d := sha256.Sum256(core)
	copy(v.BeforePeer[1:33], d[:])
	copy(v.BeforePeer[33:65], previous[:])
	p := compositionTestFrame(v, core, block)
	v.Sequence, v.Publication = 41, 2
	copy(v.Previous[:], p[32:64])
	a := compositionTestFrame(v, core, block)
	pair := [2][]byte{a, p}
	return pair, compositionTestProjection(pair)
}

func TestStorageCompositionSnapshot4ExactCoreAndPhysical(t *testing.T) {
	for _, path := range []byte{2, 3} {
		_, prior, old, scope := storageSuccessorScopeVector(t, path)
		previous := epDecode(t, prior)
		pair, physical := compositionTestBasePair(old, 2, storagebudget.Digest{1})
		capsule := storageSuccessorIntentVector(storageSuccessorSourceVector(t, 2), 10, 2, 0)
		intent, err := storageEnrollmentReadSuccessorIntent(capsule)
		storageTestMust(t, err)
		full := sha256.Sum256(old)
		inner := epHash("desk-enrollment-inner-catalog-peak-v1", previous.Inner.Raw)
		ed := sha256.Sum256(previous.Raw)
		x := make([]byte, 256)
		x[0], x[1] = 1, 4
		binary.LittleEndian.PutUint32(x[4:8], 8256)
		binary.LittleEndian.PutUint32(x[8:12], uint32(8320+len(capsule)+712))
		copy(x[16:80], old[:64])
		copy(x[80:112], full[:])
		copy(x[112:144], inner[:])
		epAtNative(x[144:179], previous.Controls[previous.Bank])
		x[179] = previous.Bank
		binary.LittleEndian.PutUint64(x[180:188], previous.Extent)
		copy(x[188:220], ed[:])
		binary.LittleEndian.PutUint64(x[220:228], previous.AfterRevision)
		binary.LittleEndian.PutUint64(x[228:236], previous.AfterEpoch)
		snapshot := append(append(append(append([]byte{}, x...), old[:8320]...), capsule...), physical[:]...)
		meta := make([]byte, 4160)
		meta[1], meta[2] = 1, 4
		binary.LittleEndian.PutUint32(meta[4:8], 4096)
		binary.LittleEndian.PutUint32(meta[8:12], uint32(len(snapshot)))
		copy(meta[16:48], full[:])
		copy(meta[48:64], scope[8:24])
		copy(meta[64:], scope[:])
		want := epFrame("DSKONP1\x00", append(meta, snapshot...), 7, true)
		value := storageEnrollmentPeakEnvelope{Kind: 0, BeforeCodec: 1, Sequence: 7, Scope: scope, Snapshot: &storageEnrollmentPeakSnapshot{Kind: 1, Full: full, Inner: inner, Evidence: previous.Controls[previous.Bank], Bank: previous.Bank, Extent: previous.Extent, EvidenceDigest: ed, Q3Revision: previous.AfterRevision, Q3Epoch: previous.AfterEpoch, Before: old, Intent: &intent, Physical: physical}}
		got, err := storageEnrollmentPeakEncodeEnvelope(value, prior.record, prior.claim)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("independent Snapshot4 wire", path, err)
		}
		decoded, err := storageEnrollmentPeakDecodeEnvelope(want, prior.record, prior.claim, true)
		if err != nil || decoded.Snapshot.Encoding != 4 || decoded.Snapshot.Physical != physical || !bytes.Equal(decoded.Snapshot.Before, old) {
			t.Fatal("physical/intent polluted core", err)
		}
		lineage := storageCompositionEmptyLineage()
		restored, err := decoded.Snapshot.Physical.restore(decoded.Snapshot.Before, lineage, prior.record, prior.claim)
		if err != nil || !bytes.Equal(restored[0], pair[0]) || !bytes.Equal(restored[1], pair[1]) {
			t.Fatal("Snapshot4 lost actual old banks", err)
		}
		for _, at := range []int{0, 1, 4, 8, 236, len(snapshot) - 712, len(snapshot) - 64, len(snapshot) - 1} {
			bad := append([]byte{}, snapshot...)
			bad[at] ^= 128
			parsed, err := storageEnrollmentPeakReadSnapshot(bad, scope, 4, prior.record, prior.claim)
			if err == nil {
				_, err = parsed.Physical.restore(parsed.Before, lineage, prior.record, prior.claim)
			}
			if err == nil {
				t.Fatal("Snapshot4 accepted mutation", at)
			}
		}
		if _, err := storageEnrollmentPeakReadSnapshot(snapshot, scope, 3, prior.record, prior.claim); err == nil {
			t.Fatal("encoding3 admitted physical extension")
		}
		value.Snapshot.Intent = nil
		if _, err := storageEnrollmentPeakEncodeEnvelope(value, prior.record, prior.claim); err == nil {
			t.Fatal("successor Snapshot4 dropped C1 intent")
		}
	}
}

// The legacy enrollment packet fixture starts from a Prepared V3 image. A
// composition first base requires an Active endpoint. Finalize the complete
// original Prepared tuple, then carry its sequence/checksum through the inner
// catalog, Scope and all packet commitments before adding composition bytes.
// Original section codecs and handwritten fixture serializers remain unchanged.
func compositionTestActiveEvidence(t *testing.T) epComplete {
	t.Helper()
	return compositionTestActiveEvidencePhase(t, 0)
}

func compositionTestActiveEvidencePhase(t *testing.T, phase byte) epComplete {
	t.Helper()
	f := epEvidence(t, phase)
	invalid, _ := compositionTestBasePair(ownerAdvisoryWire(f.before), 1, f.before.PreviousCatalogDigest)
	if _, err := storageCompositionReadPair(invalid[0], invalid[1], f.record, f.claim); err == nil {
		t.Fatal("composition admitted Prepared founding V3")
	}
	e := epDecode(t, f)
	prepared := ownerAdvisoryWire(f.before)
	f.before.Sequence++
	f.before.Phase = "active"
	copy(f.before.PreviousCatalogDigest[:], prepared[32:64])
	core := ownerAdvisoryWire(f.before)
	if pair, err := storageOwnerDecodeCatalogPairV3(prepared, core, f.record, f.claim); err != nil || !pair.PeerLinked || pair.High.Phase != "active" {
		t.Fatal("Active founding finalization", err)
	}
	if pair, err := storageOwnerDecodeCatalogPairV3(core, core, f.record, f.claim); err != nil || !pair.Duplicated || pair.High.Phase != "active" {
		t.Fatal("Active founding fixture", err)
	}
	f.after.Sequence = f.before.Sequence + 1
	copy(f.after.PreviousCatalogDigest[:], core[32:64])
	e.Before = sha256.Sum256(core)
	e.BeforeInner = sha256.Sum256(epJSON(f.before))
	e.AfterInner = sha256.Sum256(epJSON(f.after))
	copy(f.scope[88:120], e.Before[:])
	binary.LittleEndian.PutUint64(f.scope[1624:1632], f.before.Sequence)
	epScopeHash(&f.scope)
	e.Scope = f.scope
	n := int(binary.LittleEndian.Uint32(core[24:28]))
	f.sections[0] = append([]byte{}, core[:64+n]...)
	var err error
	f.sections[1], err = storageEnrollmentPeakEncodeInner(storageEnrollmentPeakInner{Catalog: f.after, Controls: e.Controls}, f.record, f.claim)
	storageTestMust(t, err)
	f.sections[4] = epProjection(f.before, f.after, core)
	// Board header: version2 + Bridge16 + two native35 + Scope hash32.
	f.sections[8] = append([]byte{}, f.sections[8]...)
	copy(f.sections[8][88:120], f.scope[184:216])
	last := uint64(65536)
	for i, raw := range f.sections {
		last = (last + 7) &^ uint64(7)
		e.Sections[i].Raw = raw
		e.Sections[i].Offset = last
		e.Sections[i].Length = uint64(len(raw))
		e.Sections[i].Digest = sha256.Sum256(raw)
		last += uint64(len(raw))
	}
	e.Packed = last - 65536
	f.evidence = e
	f.raw = epEvidenceBytes(e)
	return f
}

// The original physical checksum and the selected comparison-core checksum
// have distinct meanings. A complete resealed E1 packet must retain the latter
// as its inner predecessor, even when both physical banks wrap the same core.
func TestStorageCompositionHistorySuccessorCorePredecessor(t *testing.T) {
	f := compositionTestActiveEvidence(t)
	core := ownerAdvisoryWire(f.before)
	base, _ := compositionTestBasePair(core, 1, f.before.PreviousCatalogDigest)
	for bank := byte(0); bank < 2; bank++ {
		t.Run(fmt.Sprintf("bank%d", bank), func(t *testing.T) {
			actual := base
			if bank == 1 {
				actual = [2][]byte{base[1], base[0]}
			}
			p := &storageEnrollmentEpoch{}
			cores, selected, err := p.originalCorePair(actual, f.record, f.claim)
			storageTestMust(t, err)
			if selected != int(bank) || !bytes.Equal(cores[selected], core) || bytes.Equal(actual[selected][32:64], core[32:64]) {
				t.Fatal("original physical/core selection fixture lost distinct checksums")
			}
			check := func(previous []byte) (storageEnrollmentPeakEvidence, error) {
				e := epDecode(t, f)
				after := f.after
				copy(after.PreviousCatalogDigest[:], previous)
				e.AfterInner = sha256.Sum256(epJSON(after))
				inner, err := storageEnrollmentPeakEncodeInner(storageEnrollmentPeakInner{Catalog: after, Controls: e.Controls}, f.record, f.claim)
				if err != nil {
					return storageEnrollmentPeakEvidence{}, err
				}
				e.Sections[1].Raw = inner
				epRelayout(&e)
				// epEvidenceBytes independently reseals the complete frame and
				// section commitments, so refusal is not a stale outer hash.
				return storageEnrollmentPeakDecodeEvidence(epEvidenceBytes(e), f.record, f.claim)
			}
			got, err := check(cores[selected][32:64])
			storageTestMust(t, err)
			if !bytes.Equal(got.Inner.Catalog.PreviousCatalogDigest[:], core[32:64]) || !bytes.Equal(got.Projections[0].SavedOuter[32:64], core[32:64]) {
				t.Fatal("complete successor lost the independent original core predecessor")
			}
			if _, err := check(actual[selected][32:64]); !errors.Is(err, storagebudget.ErrBounds) {
				t.Fatal("complete successor admitted physical checksum as inner predecessor", err)
			}
			wrong := append([]byte{}, core[32:64]...)
			wrong[0] ^= 1
			if _, err := check(wrong); !errors.Is(err, storagebudget.ErrBounds) {
				t.Fatal("complete successor admitted changed original core predecessor", err)
			}
		})
	}
}

func TestStorageCompositionSnapshot4InitialPhysicalBankSelection(t *testing.T) {
	f := compositionTestActiveEvidence(t)
	core := ownerAdvisoryWire(f.before)
	base, _ := compositionTestBasePair(core, 1, f.before.PreviousCatalogDigest)
	n := int(binary.LittleEndian.Uint32(core[24:28]))
	block := compositionTestBlock([]byte(compositionEmptyJSON), [4][]storagebudget.ID{}, nil)
	for bank := byte(0); bank < 2; bank++ {
		pair := base
		if bank == 1 {
			pair = [2][]byte{base[1], base[0]}
		}
		physical := compositionTestProjection(pair)
		s := f.scope
		binary.LittleEndian.PutUint16(s[:2], 2)
		s[321], s[322] = 0, bank
		clear(s[1080:1220])
		d := sha256.Sum256(core)
		copy(s[1640:1672], d[:])
		copy(s[1672:1704], f.before.PreviousCatalogDigest[:])
		s[1704] = 1
		epReplan(&s)
		normalized := s
		clear(normalized[184:216])
		normalized[321] = 0
		clear(normalized[1080:1220])
		d = epHash("desk-enrollment-control-peak-v2", normalized[:])
		copy(s[184:216], d[:])
		x := make([]byte, 256)
		x[1], x[179] = 4, 255
		binary.LittleEndian.PutUint32(x[4:8], uint32(n))
		binary.LittleEndian.PutUint32(x[8:12], uint32(n+712))
		copy(x[16:80], core[:64])
		full := sha256.Sum256(core)
		copy(x[80:112], full[:])
		inner := epHash("desk-enrollment-original-v3-inner-peak-v1", core[64:64+n])
		copy(x[112:144], inner[:])
		snapshot := append(append(append([]byte{}, x...), core[64:64+n]...), physical[:]...)
		meta := make([]byte, 4160)
		meta[2] = 4
		binary.LittleEndian.PutUint32(meta[4:8], 4096)
		binary.LittleEndian.PutUint32(meta[8:12], uint32(len(snapshot)))
		copy(meta[16:48], full[:])
		copy(meta[48:64], s[8:24])
		copy(meta[64:], s[:])
		want := epFrame("DSKONP1\x00", append(meta, snapshot...), f.before.Sequence+1, true)
		value := storageEnrollmentPeakEnvelope{Kind: 0, BeforeCodec: 0, Sequence: f.before.Sequence + 1, Scope: s, Snapshot: &storageEnrollmentPeakSnapshot{Kind: 0, Bank: 255, Full: full, Inner: inner, Before: core, Physical: physical}}
		got, err := storageEnrollmentPeakEncodeEnvelope(value, f.record, f.claim)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("initial Snapshot4 exact raw variant", bank, err)
		}
		decoded, err := storageEnrollmentPeakDecodeEnvelope(want, f.record, f.claim, true)
		if err != nil || decoded.Snapshot.Intent != nil || !bytes.Equal(decoded.Snapshot.Before, core) {
			t.Fatal("initial variant reconstructed wrong bytes", err)
		}
		if bank == 0 {
			if _, err := storageEnrollmentPeakDecodeScope(s[:]); err == nil {
				t.Fatal("legacy Scope decoder admitted physical bank0 core tie")
			}
		}
		// Marker-only frames have no Snapshot4 suffix from which to infer
		// composition. All three live initial phases must retain the same
		// exact epoch context, while context-free bank0 refusal stays strict.
		captured := s
		captured[321] = 15
		copy(captured[1080:1220], f.scope[1080:1220])
		originalPair, err := storageCompositionReadPair(pair[0], pair[1], f.record, f.claim)
		storageTestMust(t, err)
		lineage := originalPair.Banks[originalPair.Selected].Lineage
		c := &storageBudgetConstruction{
			startupComposed: true, startupLineage: &lineage,
			ref: &storageInstallationRef{cell: &storageInstallation{ownerRecord: &f.record, ownerClaim: f.claim}},
		}
		p := &storageEnrollmentEpoch{c: c}
		for _, phase := range []struct {
			name   string
			value  byte
			offset uint64
		}{
			{"prepared", 1, 3},
			{"definitions", 2, 4},
			{"completed", 3, 5},
		} {
			t.Run(fmt.Sprintf("bank%d-%s", bank, phase.name), func(t *testing.T) {
				e := f.evidence
				e.Bank, e.Extent = 1, captured.final(1)
				marker := epMarker(e, phase.value, epHash("handwritten initial marker predecessor", []byte{bank, phase.value}))
				wantMarker := storageEnrollmentPeakEnvelope{Kind: 2, Sequence: f.before.Sequence + phase.offset, Scope: captured, Marker: &marker}
				markerRaw, err := p.encodeEnrollmentEnvelope(wantMarker)
				storageTestMust(t, err)
				decodedMarker, err := p.enrollmentEnvelope(markerRaw, true)
				if err != nil || !reflect.DeepEqual(decodedMarker, wantMarker) || decodedMarker.Snapshot != nil {
					t.Fatal("epoch marker codec lost exact founding bank, phase or before image", err)
				}
				// Independent fixed header/marker bytes use original fixture
				// operands, not any output from the epoch or marker writer.
				prefix := make([]byte, 4160)
				prefix[0], prefix[2] = 2, 255
				epSet32(prefix[4:], 4096)
				epSet32(prefix[12:], 4096)
				copy(prefix[16:48], captured[88:120])
				copy(prefix[48:64], captured[8:24])
				copy(prefix[64:], captured[:])
				body := epBytes{}
				body.fixed(uint32(7))
				body.fixed(phase.value)
				body.fixed(byte(1))
				body.fixed(uint16(0))
				body.fixed(e.Installation)
				body.fixed(e.QBinding)
				body.fixed(e.Bridge)
				for _, d := range []storagebudget.Digest{marker.Previous, e.Before, epHash("desk-enrollment-inner-catalog-peak-v1", e.Inner.Raw), sha256.Sum256(e.Raw)} {
					body.fixed(d)
				}
				body.fixed(captured.final(1))
				for _, control := range e.Controls {
					body.fixed(control)
				}
				for _, value := range []uint64{e.CoveredEpoch, e.BeforeRevision, e.BeforeEpoch, e.AfterRevision, e.AfterEpoch} {
					body.fixed(value)
				}
				for _, d := range []storagebudget.Digest{e.Record, e.Claim, e.Ready} {
					body.fixed(d)
				}
				markerBytes := make([]byte, 4096)
				copy(markerBytes, body.Bytes())
				independent := epFrame("DSKONP1\x00", append(prefix, markerBytes...), f.before.Sequence+phase.offset, true)
				if !bytes.Equal(markerRaw, independent) {
					t.Fatal("epoch context changed the independent closed marker bytes")
				}
				// Q reads the complete physical target, not this core frame.
				// Author that wrapper from independent original operands too.
				wrapper := storageCompositionBank{Sequence: 42 + uint64(phase.value), CoreCodec: 2, Publication: storageCompositionCorePublication, Transition: storageCompositionEnrollment, BeforeBank: bank}
				if phase.value == 3 {
					wrapper.Publication = storageCompositionActive
				}
				copy(wrapper.TransitionID[:], captured[24:40])
				copy(wrapper.Previous[:], pair[bank][32:64])
				copy(wrapper.BeforePeer[1:33], full[:])
				copy(wrapper.BeforePeer[33:65], f.before.PreviousCatalogDigest[:])
				physicalMarker := compositionTestFrame(wrapper, independent, block)
				comparisonCore, err := p.enrollmentCore(physicalMarker)
				storageTestMust(t, err)
				if !bytes.Equal(comparisonCore, independent) {
					t.Fatal("physical Q preflight substituted or lost original marker bytes")
				}
				physicalDecoded, err := p.enrollmentEnvelope(comparisonCore, true)
				if err != nil || !reflect.DeepEqual(physicalDecoded, wantMarker) {
					t.Fatal("physical Q preflight lost the exact original marker", err)
				}
				if _, err := p.enrollmentEnvelope(physicalMarker, true); !errors.Is(err, storagebudget.ErrBounds) {
					t.Fatal("core reader accepted a whole physical marker", err)
				}
				unheld := &storageEnrollmentEpoch{c: &storageBudgetConstruction{ref: c.ref}}
				if _, err := unheld.enrollmentCore(physicalMarker); !errors.Is(err, storagebudget.ErrBounds) {
					t.Fatal("physical Q preflight accepted an unretained lineage", err)
				}
				// A correctly resealed wrapper still cannot change the admitted
				// bridge or selected original bank. These are content refusals,
				// not merely checksum-corruption vectors.
				for _, at := range []int{69, 80} {
					bad := append([]byte{}, physicalMarker...)
					bad[at] ^= 1
					compositionTestSeal(bad)
					if _, err := p.enrollmentCore(bad); !errors.Is(err, storagebudget.ErrBounds) {
						t.Fatal("physical Q preflight accepted a changed original operand", at, err)
					}
				}
				legacyRaw, legacyErr := storageEnrollmentPeakEncodeEnvelope(wantMarker, f.record, f.claim)
				withoutContext := &storageEnrollmentEpoch{c: &storageBudgetConstruction{ref: c.ref}}
				_, contextErr := withoutContext.enrollmentEnvelope(markerRaw, true)
				if bank == 0 {
					if !errors.Is(legacyErr, storagebudget.ErrBounds) || !errors.Is(contextErr, storagebudget.ErrBounds) {
						t.Fatal("context-free marker codec admitted physical bank0 core tie", legacyErr, contextErr)
					}
					if _, err := storageEnrollmentPeakDecodeEnvelope(markerRaw, f.record, f.claim, true); !errors.Is(err, storagebudget.ErrBounds) {
						t.Fatal("legacy marker decoder admitted physical bank0 core tie", err)
					}
				} else if legacyErr != nil || contextErr != nil || !bytes.Equal(legacyRaw, markerRaw) {
					t.Fatal("ordinary bank1 marker changed with composition context", legacyErr, contextErr)
				}
			})
		}

		v := storageCompositionBank{Sequence: 42, CoreCodec: 2, Publication: 3, Transition: 2, BeforeBank: bank}
		copy(v.TransitionID[:], s[24:40])
		copy(v.Previous[:], pair[bank][32:64])
		copy(v.BeforePeer[1:33], full[:])
		copy(v.BeforePeer[33:65], f.before.PreviousCatalogDigest[:])
		fence := compositionTestFrame(v, want, block)
		actual := pair
		actual[1-bank] = fence
		chosen, err := storageCompositionReadPair(actual[0], actual[1], f.record, f.claim)
		if err != nil || chosen.Selected != 1-bank || !chosen.RecoveryOnly {
			t.Fatal("first wrapped Fence overwrote selected physical bank or lost core proof", bank, err)
		}
	}
}

// Independent version2 packet directory/header. All unchanged sections are
// original fixture inputs; the new version, physical suffix and maxima are
// explicitly authored here rather than obtained from the new packet writer.
func compositionTestEvidenceV2Frame(e storageEnrollmentPeakEvidence) []byte {
	b := make([]byte, e.Extent)
	w := epBytes{}
	w.WriteString("DSKEPP2\x00")
	w.fixed(uint32(2))
	w.fixed(uint32(65536))
	w.fixed(e.Generation)
	last := uint64(65536)
	for i := range e.Sections {
		s := &e.Sections[i]
		s.Offset = (last + 7) &^ uint64(7)
		s.Length = uint64(len(s.Raw))
		s.Digest = sha256.Sum256(s.Raw)
		last = s.Offset + s.Length
	}
	w.fixed(last - 65536)
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
	for _, n := range []uint64{e.BeforeRevision, e.BeforeEpoch, e.AfterRevision, e.AfterEpoch} {
		w.fixed(n)
	}
	for _, p := range e.Controls {
		w.fixed(p)
	}
	w.fixed(e.Project)
	w.fixed(e.RegistrationDigest)
	w.fixed(e.CensusDigest)
	w.fixed(e.CoveredEpoch)
	w.fixed(uint16(9))
	maxima := [9]uint64{8388608, 8388608, 35143680, 1081344, 8968080, 2113536, 4718592, 2097152, 65536}
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
	at := w.Len()
	w.Write(make([]byte, 32))
	copy(b, w.Bytes())
	copy(b[4096:8192], e.Scope[:])
	d := epHash("desk-enrollment-bank-peak-v1", b)
	copy(b[at:at+32], d[:])
	return b
}

func TestStorageCompositionEvidence2HistoricalPhysicalRetention(t *testing.T) {
	f := compositionTestActiveEvidence(t)
	e := epDecode(t, f)
	core := ownerAdvisoryWire(f.before)
	pair, _ := compositionTestBasePair(core, 1, f.before.PreviousCatalogDigest)
	// This captured Scope selects physical bank1; preserve that native target.
	pair = [2][]byte{pair[1], pair[0]}
	physical := compositionTestProjection(pair)
	lineage := storageCompositionEmptyLineage()
	s := e.Scope
	binary.LittleEndian.PutUint16(s[:2], 2)
	d := sha256.Sum256(core)
	copy(s[1640:1672], d[:])
	copy(s[1672:1704], f.before.PreviousCatalogDigest[:])
	s[1704] = 1
	normalized := s
	clear(normalized[184:216])
	normalized[321] = 0
	clear(normalized[1080:1220])
	d = epHash("desk-enrollment-control-peak-v2", normalized[:])
	copy(s[184:216], d[:])
	e.Scope, e.Version, e.CompositionLineage = s, 2, &lineage
	e.Sections[8].Raw = append([]byte{}, f.sections[8]...)
	copy(e.Sections[8].Raw[88:120], s[184:216])
	// The already separately covered checkpoint-v2 codec supplies an unchanged
	// input section. Expected Evidence2 framing/physical fields remain handwritten.
	cp := e.Checkpoint
	cp.PeerCheckpoint = append([]byte{}, f.cp[0]...)
	var err error
	e.Sections[6].Raw, err = storageEnrollmentPeakWriteCheckpoint(cp, e.Inner.Catalog, e.Views, s)
	storageTestMust(t, err)
	e.Sections[4].Raw = append(append([]byte{}, f.sections[4]...), physical[:]...)
	// Wrong physical selection and a stale Board Scope binding remain refusals.
	badSelection := e
	wrongPhysical := compositionTestProjection([2][]byte{pair[1], pair[0]})
	badSelection.Sections[4].Raw = append(append([]byte{}, f.sections[4]...), wrongPhysical[:]...)
	if _, err := storageEnrollmentPeakDecodeEvidenceWithLineage(compositionTestEvidenceV2Frame(badSelection), &lineage, f.record, f.claim); err == nil {
		t.Fatal("Evidence2 accepted wrong selected physical bank")
	}
	badBoard := e
	badBoard.Sections[8].Raw = append([]byte{}, f.sections[8]...)
	if _, err := storageEnrollmentPeakDecodeEvidenceWithLineage(compositionTestEvidenceV2Frame(badBoard), &lineage, f.record, f.claim); err == nil {
		t.Fatal("Evidence2 accepted stale Board Scope binding")
	}
	want := compositionTestEvidenceV2Frame(e)
	got, err := storageEnrollmentPeakDecodeEvidenceWithLineage(want, &lineage, f.record, f.claim)
	if err != nil {
		t.Fatal("independent Evidence2", err)
	}
	if got.Version != 2 || got.Projections[0].Physical != physical || got.Before != sha256.Sum256(core) || got.Sections[4].Length != uint64(len(f.sections[4])+712) {
		t.Fatal("core/physical evidence commitments confused")
	}
	restored, err := got.compositionBefore(f.record, f.claim)
	if err != nil || !bytes.Equal(restored.Banks[0].Raw, pair[0]) || !bytes.Equal(restored.Banks[1].Raw, pair[1]) {
		t.Fatal("evidence lost historical physical before", err)
	}
	for _, name := range []string{"retired-physical-bank-hashes", "retired-core-is-not-physical", "retired-missing-lineage", "retired-malformed-lineage", "retired-wrong-bank"} {
		t.Run(name, func(t *testing.T) {
			input := got
			switch name {
			case "retired-missing-lineage":
				input.CompositionLineage = nil
			case "retired-malformed-lineage":
				bad := storageCompositionEmptyLineage()
				bad.Order[0] = []storagebudget.ID{{99}}
				input.CompositionLineage = &bad
			case "retired-wrong-bank":
				input.Scope[322] ^= 1
			}
			hashes, e := storageEnrollmentCreationBeforeDigests(input, core, f.record, f.claim)
			if name == "retired-missing-lineage" || name == "retired-malformed-lineage" || name == "retired-wrong-bank" {
				if e == nil {
					t.Fatal("unsupported original reconstruction accepted")
				}
				return
			}
			if e != nil || hashes != ([2]storagebudget.Digest{sha256.Sum256(pair[0]), sha256.Sum256(pair[1])}) {
				t.Fatal("retired original physical banks changed", e)
			}
			if name == "retired-core-is-not-physical" && (hashes[0] == sha256.Sum256(core) || hashes[1] == sha256.Sum256(core) || hashes[0] == hashes[1]) {
				t.Fatal("core/physical or bank identity collapsed")
			}
		})
	}
	encoded, err := storageEnrollmentPeakEncodeEvidence(got, f.record, f.claim)
	if err != nil || !bytes.Equal(encoded, want) {
		t.Fatal("Evidence2 writer differs from handwritten frame", err)
	}
	if _, err := storageEnrollmentPeakDecodeEvidence(want, f.record, f.claim); err == nil {
		t.Fatal("unbound evidence accepted wrapped history")
	}
	for _, at := range []int{0, 8, 320, 640, 648, 711} {
		bad := e
		bad.Sections = e.Sections
		bad.Sections[4].Raw = append([]byte{}, e.Sections[4].Raw...)
		bad.Sections[4].Raw[len(f.sections[4])+at] ^= 1
		if _, err := storageEnrollmentPeakDecodeEvidenceWithLineage(compositionTestEvidenceV2Frame(bad), &lineage, f.record, f.claim); err == nil {
			t.Fatal("Evidence2 accepted physical mutation", at)
		}
	}
	if 65536+8388608+8388608+35143680+1081344+8968080+2113536+4718592+2097152+65536 != 71030672 || (71030672+65535)&^65535 != 71041024 {
		t.Fatal("unchanged finite packet capacity")
	}
}

func TestStorageCompositionFirstBaseOriginalCommitmentAndCuts(t *testing.T) {
	c, r, cl := ownerAdvisoryFixture(false)
	c.Sequence, c.Phase, c.PreviousCatalogDigest = 12, "active", storagebudget.Digest{33}
	duplicate := ownerAdvisoryWire(c)
	low := c
	low.Sequence--
	low.Phase = "prepared"
	low.PreviousCatalogDigest = storagebudget.Digest{34}
	lowRaw := ownerAdvisoryWire(low)
	high := c
	copy(high.PreviousCatalogDigest[:], lowRaw[32:64])
	highRaw := ownerAdvisoryWire(high)
	_, prior, completed, _ := storageSuccessorScopeVector(t, 2)
	for _, tc := range []struct {
		name              string
		originals         [2][]byte
		record            storageOwnerRecordV2
		claim             storageOwnerClaimV2
		bank, codec, mode byte
		peerPrevious      storagebudget.Digest
	}{
		{"V3-duplicate", [2][]byte{duplicate, duplicate}, r, cl, 1, 1, 0, c.PreviousCatalogDigest},
		{"V3-linked-bank0", [2][]byte{highRaw, lowRaw}, r, cl, 0, 1, 1, low.PreviousCatalogDigest},
		{"V3-linked-bank1", [2][]byte{lowRaw, highRaw}, r, cl, 1, 1, 1, low.PreviousCatalogDigest},
		{"E1-completed", [2][]byte{completed, completed}, prior.record, prior.claim, 0, 2, 0, storagebudget.Digest{1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recordRaw, err := storageOwnerEncodeRecordV2(tc.record)
			storageTestMust(t, err)
			claimRaw, err := storageOwnerEncodeClaimV2(tc.claim)
			storageTestMust(t, err)
			ready := make([]byte, 32)
			ready[0] = 7
			host := storagebudget.HostInspection{Configuration: storagebudget.Configuration{Revision: 10, Epoch: 1}}
			hostJSON, _ := json.Marshal(host)
			h := sha256.New()
			h.Write([]byte("desk-owner-composition-first-base-v1\x00"))
			h.Write(tc.originals[0])
			h.Write(tc.originals[1])
			h.Write(recordRaw)
			h.Write(claimRaw)
			h.Write(ready)
			h.Write(hostJSON)
			var commitment storagebudget.Digest
			copy(commitment[:], h.Sum(nil))
			id := storagebudget.ID{91}
			plan, err := storageCompositionPlanFirstBase(tc.originals, id, recordRaw, claimRaw, ready, host, tc.record, tc.claim)
			if err != nil || plan.Commitment != commitment || plan.BeforeBank != tc.bank || plan.FirstBank != 1-tc.bank {
				t.Fatal("first-base original plan", err)
			}
			core := tc.originals[tc.bank]
			v := storageCompositionBank{Sequence: binary.LittleEndian.Uint64(core[16:24]) + 1, CoreCodec: tc.codec, Publication: 1, Transition: 1, BeforeBank: tc.bank, TransitionID: id, FirstBase: commitment}
			copy(v.Previous[:], core[32:64])
			v.BeforePeer[0] = tc.mode
			d := sha256.Sum256(tc.originals[1-tc.bank])
			copy(v.BeforePeer[1:33], d[:])
			copy(v.BeforePeer[33:65], tc.peerPrevious[:])
			block := compositionTestBlock([]byte(compositionEmptyJSON), [4][]storagebudget.ID{}, nil)
			prepared := compositionTestFrame(v, core, block)
			v.Sequence++
			v.Publication = 2
			copy(v.Previous[:], prepared[32:64])
			active := compositionTestFrame(v, core, block)
			if !bytes.Equal(plan.Prepared, prepared) || !bytes.Equal(plan.Active, active) {
				t.Fatal("first-base images differ from independent wire")
			}
			mixed := tc.originals
			mixed[1-tc.bank] = prepared
			both := [2][]byte{prepared, prepared}
			final := both
			final[1-tc.bank] = active
			for stage, pair := range [][2][]byte{mixed, both, final} {
				recovered, cut, err := storageCompositionRecoverFirstBase(pair, recordRaw, claimRaw, ready, host, tc.record, tc.claim)
				if err != nil || cut != byte(stage+1) || !bytes.Equal(recovered.Prepared, prepared) || !bytes.Equal(recovered.Active, active) {
					t.Fatal("exact original cut", stage, err)
				}
				changed := host
				changed.Configuration.Revision++
				if _, _, err := storageCompositionRecoverFirstBase(pair, recordRaw, claimRaw, ready, changed, tc.record, tc.claim); err == nil {
					t.Fatal("changed Q inspection accepted")
				}
				for bank := 0; bank < 2; bank++ {
					bad := pair
					bad[bank] = append([]byte{}, pair[bank]...)
					bad[bank][len(bad[bank])-1] = 1
					if _, _, err := storageCompositionRecoverFirstBase(bad, recordRaw, claimRaw, ready, host, tc.record, tc.claim); err == nil {
						t.Fatal("torn or invalid peer accepted", stage, bank)
					}
				}
				if _, err := storageOwnerDecodeCatalogPairV3(pair[0], pair[1], tc.record, tc.claim); err == nil {
					t.Fatal("old V3 reader crossed first wrapper")
				}
				if _, _, err := storageEnrollmentPeakDecodePair(pair[0], pair[1], tc.record, tc.claim); err == nil {
					t.Fatal("old E1 reader crossed first wrapper")
				}
			}
		})
	}
}

// This exercises the startup selector and pending-requester caller with an
// actual original construction/Q. Only comparison catalog images are supplied;
// no native qualification, entry key, publication or successful continuation is
// manufactured. RecoverFirstBase must retain the exact existing Active operand.
func TestStorageCompositionStartupFoundingRequesterCuts(t *testing.T) {
	for _, beforeBank := range []byte{0, 1} {
		t.Run(fmt.Sprintf("before-bank%d", beforeBank), func(t *testing.T) {
			cfg := ownerActualConfig(t)
			storageTestMust(t, os.Chmod(cfg.ProjectDir, 0700))
			s, c, _ := ownerCrashConstruction(t, cfg)
			storageTestMust(t, c.loadOrEnrollOwner(t.Context()))
			record, claim := *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim
			recordRaw, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, storageRecordName))
			storageTestMust(t, err)
			claimRaw, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, storageClaimName))
			storageTestMust(t, err)
			ready, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, storageReadyName))
			storageTestMust(t, err)
			q, err := c.ref.cell.q.InspectAction(t.Context())
			storageTestMust(t, err)
			// Reuse the independent V3/wrapper wire fixtures with this original
			// construction's real record, graph and unchanged strict Q input.
			low := ownerCatalogClone(c.ref.cell.ownerCatalog)
			low.Sequence, low.Phase, low.PreviousCatalogDigest = 11, "prepared", storagebudget.Digest{34}
			lowRaw := ownerAdvisoryWire(low)
			high := ownerCatalogClone(low)
			high.Sequence, high.Phase = 12, "active"
			copy(high.PreviousCatalogDigest[:], lowRaw[32:64])
			highRaw := ownerAdvisoryWire(high)
			originals := [2][]byte{lowRaw, lowRaw}
			originals[beforeBank] = highRaw
			hostJSON, err := json.Marshal(q.View)
			storageTestMust(t, err)
			h := sha256.New()
			h.Write([]byte("desk-owner-composition-first-base-v1\x00"))
			h.Write(originals[0])
			h.Write(originals[1])
			h.Write(recordRaw)
			h.Write(claimRaw)
			h.Write(ready)
			h.Write(hostJSON)
			v := storageCompositionBank{Sequence: 13, CoreCodec: 1, Publication: 1, Transition: 1, BeforeBank: beforeBank, TransitionID: storagebudget.ID{91}}
			copy(v.FirstBase[:], h.Sum(nil))
			copy(v.Previous[:], highRaw[32:64])
			v.BeforePeer[0] = 1
			d := sha256.Sum256(lowRaw)
			copy(v.BeforePeer[1:33], d[:])
			copy(v.BeforePeer[33:65], low.PreviousCatalogDigest[:])
			block := compositionTestBlock([]byte(compositionEmptyJSON), [4][]storagebudget.ID{}, nil)
			prepared := compositionTestFrame(v, highRaw, block)
			v.Sequence, v.Publication = 14, 2
			copy(v.Previous[:], prepared[32:64])
			active := compositionTestFrame(v, highRaw, block)
			mixed, final := originals, [2][]byte{prepared, prepared}
			mixed[1-beforeBank] = prepared
			final[1-beforeBank] = active
			writePair := func(pair [2][]byte) {
				t.Helper()
				for bank, name := range []string{"storage-owner.a", "storage-owner.b"} {
					storageTestMust(t, os.WriteFile(filepath.Join(cfg.DeskConfigDir, name), pair[bank], 0600))
				}
			}
			both := [2][]byte{prepared, prepared}
			for stage, pair := range [][2][]byte{mixed, both, final} {
				writePair(pair)
				_, _, err := c.selectStartupEnrollment(t.Context(), record, claim)
				storageTestMust(t, err)
				if !c.startupFirstBase {
					t.Fatal("founding cut was classified as ordinary lineage", stage)
				}
				storageTestMust(t, c.validateStartupCompositionQ(t.Context(), record, claim))
				plan, local, err := c.pendingStartupOriginal(t.Context())
				if err != nil || plan != nil || local != nil {
					t.Fatal("founding caller did not retain primary continuation", stage, err)
				}
				recovered, cut, err := storageCompositionRecoverFirstBase(pair, recordRaw, claimRaw, ready, q.View, record, claim)
				if err != nil || cut != byte(stage+1) || !bytes.Equal(recovered.Active, active) {
					t.Fatal("caller lost exact existing Active continuation operand", stage, err)
				}
				if stage == 1 {
					p, err := c.beginStartupEnrollment(t.Context())
					storageTestMust(t, err)
					selectErr := p.selectStartupOriginalRequester(t.Context())
					primary := p.requester == s && p.startupProject == nil
					finishErr := p.finish(selectErr)
					if finishErr != nil || !primary {
						t.Fatal("founding requester escaped original primary", finishErr)
					}
				}
				// A Base wrapper alone is not an ordinary admission/lineage proof.
				c.startupFirstBase = false
				if _, _, err := c.pendingStartupOriginal(t.Context()); err == nil {
					t.Fatal("unvalidated Base accepted", stage)
				}
				c.startupFirstBase = true
			}
			// The retained selection is exact: a later physical change cannot
			// borrow its startup flag, even when the changed frame is valid.
			writePair(both)
			_, _, err = c.selectStartupEnrollment(t.Context(), record, claim)
			storageTestMust(t, err)
			writePair(final)
			if _, _, err := c.pendingStartupOriginal(t.Context()); err == nil {
				t.Fatal("changed physical pair borrowed retained founding selection")
			}
			other := v
			other.TransitionID = storagebudget.ID{92}
			incompatible := compositionTestFrame(other, highRaw, block)
			for _, bad := range [][2][]byte{{prepared, incompatible}, {prepared, append([]byte{}, prepared...)}} {
				if bytes.Equal(bad[0], bad[1]) {
					bad[1][len(bad[1])-1] = 1
				}
				writePair(bad)
				if _, _, err := c.selectStartupEnrollment(t.Context(), record, claim); err == nil {
					t.Fatal("incompatible/malformed founding input accepted by startup")
				}
				if _, _, err := c.pendingStartupOriginal(t.Context()); err == nil {
					t.Fatal("incompatible/malformed founding input accepted by caller")
				}
			}
			writePair(both)
			_, _, err = c.selectStartupEnrollment(t.Context(), record, claim)
			storageTestMust(t, err)
			storageTestMust(t, c.ref.cell.q.Checkpoint(t.Context()))
			if _, _, err := c.pendingStartupOriginal(t.Context()); err == nil {
				t.Fatal("changed full Q inspection accepted by founding caller")
			}
		})
	}
}

func TestStorageCompositionFirstBaseNearCapacityIntactRefusal(t *testing.T) {
	c, r, cl := epMaxV3(t)
	before := ownerAdvisoryWire(c)
	if _, err := storageOwnerDecodeCatalogV3(before, r, cl); err != nil {
		t.Fatal("old near-capacity fixture invalid", err)
	}
	recordRaw, err := storageOwnerEncodeRecordV2(r)
	storageTestMust(t, err)
	claimRaw, err := storageOwnerEncodeClaimV2(cl)
	storageTestMust(t, err)
	ready := make([]byte, 32)
	ready[0] = 7
	host := storagebudget.HostInspection{Configuration: storagebudget.Configuration{Revision: 10, Epoch: 1}}
	digest := sha256.Sum256(before)
	if _, err := storageCompositionPlanFirstBase([2][]byte{before, before}, storagebudget.ID{91}, recordRaw, claimRaw, ready, host, r, cl); err == nil {
		t.Fatal("near-capacity core silently truncated or fit claimed")
	}
	if sha256.Sum256(before) != digest {
		t.Fatal("capacity refusal altered original")
	}
}

func TestStorageCompositionSuccessorOriginalInputsV2(t *testing.T) {
	f, prior, old, scope := storageSuccessorScopeVector(t, 2)
	base, _ := compositionTestBasePair(old, 2, storagebudget.Digest{1})
	for bank := byte(0); bank < 2; bank++ {
		s := scope
		s[322] = bank
		epReplan(&s)
		normalized := s
		clear(normalized[184:216])
		normalized[321] = 0
		clear(normalized[1080:1220])
		d := epHash("desk-enrollment-control-peak-v2", normalized[:])
		copy(s[184:216], d[:])
		actual := base
		if bank == 1 {
			actual = [2][]byte{base[1], base[0]}
		}
		physical := compositionTestProjection(actual)
		original := storageEnrollmentPeakOriginalComparison{Catalogs: actual, Checkpoints: [2][]byte{f.cp[0], f.cp[0]}, Tickets: f.tickets}
		for slot := range original.Hosts {
			for b := range original.Hosts[slot] {
				original.Hosts[slot][b] = make([]byte, 16384)
			}
		}
		step := epPeerDispatch(epDecode(t, prior))[s[324]]
		ready := f.record.QBinding[:]
		h := sha256.New()
		h.Write([]byte("desk-successor-fence-original-inputs-v2\x00"))
		h.Write([]byte{1, 0})
		h.Write(s[:])
		for _, raw := range original.Catalogs {
			h.Write(raw)
		}
		var length [8]byte
		binary.LittleEndian.PutUint64(length[:], uint64(len(prior.raw)))
		h.Write(length[:])
		h.Write(prior.raw)
		h.Write(ready)
		for _, raw := range original.Checkpoints {
			h.Write(raw)
		}
		for _, raw := range original.Tickets {
			h.Write(raw)
		}
		for _, pair := range original.Hosts {
			for _, raw := range pair {
				h.Write(raw)
			}
		}
		h.Write(step)
		h.Write(old)
		h.Write(old)
		h.Write(physical[:])
		var want storagebudget.Digest
		copy(want[:], h.Sum(nil))
		got, err := storageEnrollmentSuccessorCompositionInputs(s, original, prior.raw, ready, step, physical, prior.record, prior.claim)
		if err != nil || got != want {
			t.Fatal("complete independent physical/core tuple", bank, err)
		}
		coreOnly := original
		coreOnly.Catalogs = [2][]byte{old, old}
		oldDigest, err := storageEnrollmentSuccessorInputs(s, coreOnly, prior.raw, ready, step)
		storageTestMust(t, err)
		if oldDigest == want {
			t.Fatal("core-only commitment promoted to physical v2")
		}
		for _, at := range []int{0, 320, 640, 648, 711} {
			bad := physical
			bad[at] ^= 1
			if _, err := storageEnrollmentSuccessorCompositionInputs(s, original, prior.raw, ready, step, bad, prior.record, prior.claim); err == nil {
				t.Fatal("accepted altered exact physical projection", at)
			}
		}
		other := s
		other[322] = 1 - bank
		if _, err := storageEnrollmentSuccessorCompositionInputs(other, original, prior.raw, ready, step, physical, prior.record, prior.claim); err == nil {
			t.Fatal("selected physical bank lost")
		}
	}
	if 100671530+16777216+712 != 117449458 {
		t.Fatal("unchanged streaming tuple bound")
	}
}

func TestStorageCompositionCompletedPhysicalEndpointR1(t *testing.T) {
	f := epEvidence(t, 2)
	block := compositionTestBlock([]byte(compositionEmptyJSON), [4][]storagebudget.ID{}, nil)
	for path := byte(1); path <= 3; path++ {
		for before := byte(0); before < 2; before++ {
			t.Run(fmt.Sprintf("path%d-before%d", path, before), func(t *testing.T) {
				oldExtent, final := [2]uint64{}, [2]uint64{262144, 262144}
				if path == 2 {
					oldExtent = final
				}
				if path == 3 {
					oldExtent = [2]uint64{131072, 131072}
				}
				scope := epScope(path, true, f.record, oldExtent, final)
				scope[322] = before
				epSet64(scope[1624:1632], 11)
				epReplan(&scope)
				e := f.evidence
				e.Scope = scope
				copy(e.Bridge[:], scope[24:40])
				copy(e.Before[:], scope[88:120])
				for i := range e.Controls {
					e.Controls[i] = scope.target(byte(i + 1))
				}
				dOffset, cBank := byte(4), 1-before
				if path == 2 {
					dOffset, cBank = 3, before
				}
				e.Bank = 0
				if path == 2 {
					e.Bank = 1
				}
				e.Extent = 262144
				dCore := compositionTestMarkerCore(e, 2, 11+uint64(dOffset), storagebudget.Digest{61})
				var corePrevious storagebudget.Digest
				copy(corePrevious[:], dCore[32:64])
				e.Bank = 1
				if path == 2 {
					e.Bank = 0
				}
				cCore := compositionTestMarkerCore(e, 3, 12+uint64(dOffset), corePrevious)
				d := storageCompositionBank{Sequence: 40 + uint64(dOffset), CoreCodec: 2, Publication: 3, Transition: 2, BeforeBank: before, TransitionID: e.Bridge, Previous: storageCompositionChecksum{55}, CoreFull: storageCompositionCoreDigest(sha256.Sum256(dCore)), LineageDigest: sha256.Sum256(block), Core: dCore, Lineage: storageCompositionEmptyLineage()}
				copy(d.BeforePeer[1:33], scope[88:120])
				d.BeforePeer[33] = 56
				dRaw := compositionTestFrame(d, dCore, block)
				c := d
				c.Sequence++
				c.Publication = 2
				c.Core = cCore
				c.CoreFull = storageCompositionCoreDigest(sha256.Sum256(cCore))
				copy(c.Previous[:], dRaw[32:64])
				cRaw := compositionTestFrame(c, cCore, block)
				banks := [2][]byte{dRaw, dRaw}
				banks[cBank] = cRaw
				pair, err := storageCompositionReadPair(banks[0], banks[1], f.record, f.claim)
				if err != nil || !pair.RecoveryOnly || pair.Duplicated || pair.Selected != cBank {
					t.Fatal("C/D must select only original completion", err)
				}
				if _, err := storageCompositionCaptureProjection(banks[0], banks[1], f.record, f.claim); err == nil {
					t.Fatal("C/D opened a new successor")
				}
				if storagePolicyOriginalPairBank(banks, 1-cBank, pair.Selected, pair.Duplicated) {
					t.Fatal("nonduplicated selector disagreement accepted")
				}
				// RecoveryOnly is an independent original refusal, even when
				// the higher-header selector and decoded bank already agree.
				if !pair.RecoveryOnly || !storagePolicyOriginalPairBank(banks, cBank, pair.Selected, pair.Duplicated) {
					t.Fatal("pending pair lost independent recovery classification")
				}
				pair, err = storageCompositionReadPair(cRaw, cRaw, f.record, f.claim)
				if err != nil || pair.RecoveryOnly || !pair.Duplicated || pair.Selected != cBank {
					t.Fatal("exact C/C endpoint/primary tie", err)
				}
				if !storagePolicyOriginalPairBank([2][]byte{cRaw, cRaw}, 0, pair.Selected, pair.Duplicated) {
					t.Fatal("strict completed duplicate bank lost to header tie")
				}
				if storagePolicyOriginalPairBank([2][]byte{cRaw, cRaw}, 0, 2, true) || storagePolicyOriginalPairBank([2][]byte{cRaw[:len(cRaw)-1], cRaw}, 0, 1, true) {
					t.Fatal("invalid selector/frame length accepted")
				}
				// Native publication uses a separate physical sequence domain;
				// the same independently fixed scope still selects cBank.
				native := storageOwnerV6NativeBank{Stage: c, NativeBase: 40, Boundary: storagePolicyQBoundary{e.AfterEpoch, e.AfterRevision}, Policy: f.after.Policy, Overrides: []byte{0, 0}}
				nativeRaw, nativeErr := native.encode(f.record, f.claim)
				storageTestMust(t, nativeErr)
				// An enrollment founds its own lineage: no first base below it.
				founded := native
				founded.Stage.FirstBase = storagebudget.Digest{0x5a}
				if _, nativeErr = founded.encode(f.record, f.claim); nativeErr == nil {
					t.Fatal("native enrollment carried a first base")
				}
				nativePair, nativeErr := storageOwnerV6NativeReadPair(nativeRaw, nativeRaw, nil, nil, f.record, f.claim)
				if nativeErr != nil || nativePair.RecoveryOnly || !nativePair.Duplicated || nativePair.Selected != cBank || !storagePolicyOriginalPairBank([2][]byte{nativeRaw, nativeRaw}, 0, nativePair.Selected, nativePair.Duplicated) {
					t.Fatal("native completed semantic bank lost", nativeErr)
				}
				brokenNative := append([]byte{}, nativeRaw...)
				brokenNative[len(brokenNative)-1] = 1
				if _, nativeErr = storageOwnerV6NativeReadPair(nativeRaw, brokenNative, nil, nil, f.record, f.claim); nativeErr == nil {
					t.Fatal("malformed native pair selected")
				}
				actual, err := c.encode(f.record, f.claim)
				if err != nil || !bytes.Equal(actual, cRaw) {
					t.Fatal("final Active tag must be in existing C image", err)
				}
				if _, err := storageCompositionCaptureProjection(cRaw, cRaw, f.record, f.claim); err != nil {
					t.Fatal("completed endpoint has no next-operation before", err)
				}
				for _, mutate := range []func([]byte){
					func(raw []byte) { raw[67] = 3 },
					func(raw []byte) { binary.LittleEndian.PutUint64(raw[16:24], c.Sequence+1) },
					func(raw []byte) { raw[96] ^= 1 },
					func(raw []byte) { raw[69] ^= 1 },
				} {
					bad := append([]byte{}, cRaw...)
					mutate(bad)
					compositionTestSeal(bad)
					broken := banks
					broken[cBank] = bad
					if _, err := storageCompositionReadPair(broken[0], broken[1], f.record, f.claim); err == nil {
						t.Fatal("nonlisted final edge")
					}
				}
				wrong := banks[0]
				banks[0], banks[1] = banks[1], wrong
				if _, err := storageCompositionReadPair(banks[0], banks[1], f.record, f.claim); err == nil {
					t.Fatal("swapped actual final banks")
				}
				bad := append([]byte{}, cRaw...)
				bad[257] ^= 1
				compositionTestSeal(bad)
				if _, err := storageCompositionReadPair(cRaw, bad, f.record, f.claim); err == nil {
					t.Fatal("differing same-sequence completed duplicate")
				}
				bad = append([]byte{}, dRaw...)
				bad[67] = 2
				compositionTestSeal(bad)
				if _, err := storageCompositionDecode(bad, f.record, f.claim); err == nil {
					t.Fatal("standalone Definitions retag")
				}
			})
		}
	}
}

func TestStorageIdentityJ5ActionWireAndEnteredBarrier(t *testing.T) {
	for _, action := range []byte{2, 3, 4} {
		r := storageIdentityIntentFixture()
		r.Action = action
		name := "clear"
		if action == 3 {
			name = "stamp"
			r.Core.NewLength = 0
			r.PayloadDigest = storagebudget.Digest{}
			r.AllocatedStage = storageIdentityIntentPO(44, 1)
			r.Presence = 3
		} else {
			if action == 4 {
				name = "initial-cas"
			}
			r.Core.OldExists, r.Core.OldIdentity, r.Core.OldLength, r.Core.OldLinkCount = true, storageDirectoryIdentity{Device: 1, Inode: 45}, r.Core.NewLength, 1
			r.ExpectedOld = storageIdentityIntentPO(45, 2)
			r.Presence = 17
		}
		r.Core.Operation = fmt.Sprintf("p:%x:%s", r.PlanDigest, name)
		storageTestMust(t, r.validate())
		raw, err := storageEncodeIdentityIntent(r, storageIdentityIntentID(90), storageIdentityIntentHash(91))
		storageTestMust(t, err)
		if string(raw[:8]) != "DSKHINT5" || raw[8] != 5 || raw[9] != 0 || raw[13] != 1 || !storageAllZero(raw[14:16]) || raw[15791] != 2 || raw[15792] != 0 || raw[15793] != action || raw[15794] != 0 || raw[15795] != byte(r.Presence) || !storageAllZero(raw[16175:]) {
			t.Fatal("J5 exact wire discriminators", action)
		}
		decoded, err := storageDecodeIdentityIntent(raw, int(r.Core.Slot), storageIdentityIntentID(90), storageIdentityIntentHash(91))
		storageTestMust(t, err)
		if !reflect.DeepEqual(decoded, r) {
			t.Fatal("J5 action operands lost", action)
		}
		bad := append([]byte{}, raw...)
		bad[13] = 2
		clear(bad[76:108])
		sum := sha256.Sum256(bad)
		copy(bad[76:108], sum[:])
		if _, err := storageDecodeIdentityIntent(bad, int(r.Core.Slot), storageIdentityIntentID(90), storageIdentityIntentHash(91)); err == nil {
			t.Fatal("identity accepted local codec", action)
		}
		bad = append([]byte{}, raw...)
		copy(bad[:8], "DSKHINT3")
		bad[8], bad[13], bad[15791] = 3, 0, 1
		clear(bad[76:108])
		sum = sha256.Sum256(bad)
		copy(bad[76:108], sum[:])
		if _, err := storageDecodeIdentityIntent(bad, int(r.Core.Slot), storageIdentityIntentID(90), storageIdentityIntentHash(91)); err == nil {
			t.Fatal("J3 reinterpreted action", action)
		}
		if storageIdentityPublicationMaterial(r).Version != 2 {
			t.Fatal("J5 used old publication domain operand")
		}
	}
}

func TestStorageIdentityFiniteOriginalActionEnvelopes(t *testing.T) {
	// Independent accepted contract table, including both Q checkpoints and
	// the added CAS Entered page. No value is obtained from an execution trace.
	for _, tc := range []struct {
		action                                                                          byte
		local                                                                           bool
		freshCount                                                                      int
		freshRaw, freshPolicy, freshInodes, recoveryRaw, recoveryPolicy, recoveryInodes uint64
	}{
		{1, false, 25, 9176576, 38273024, 52, 53216768, 127664128, 54},
		{2, false, 26, 9192960, 38928384, 54, 53233152, 128319488, 56},
		{4, false, 26, 9192960, 38928384, 54, 53233152, 128319488, 56},
		{3, false, 19, 9125888, 31850496, 38, 53166080, 121241600, 40},
		{0, true, 25, 9175044, 62586880, 53, 53215236, 151977984, 55},
	} {
		for _, recovery := range []bool{false, true} {
			groups, err := storageIdentityFiniteGroups(tc.action, tc.local, recovery)
			storageTestMust(t, err)
			raw, policy, err := storageIdentityFiniteTotals(groups)
			storageTestMust(t, err)
			count, wantRaw, wantPolicy, wantInodes := tc.freshCount, tc.freshRaw, tc.freshPolicy, tc.freshInodes
			if recovery {
				count++
				wantRaw, wantPolicy, wantInodes = tc.recoveryRaw, tc.recoveryPolicy, tc.recoveryInodes
			}
			if len(groups) != count || raw != wantRaw || policy.Bytes != wantPolicy || policy.Inodes != wantInodes {
				t.Fatal("finite original action bound", tc.action, tc.local, recovery, len(groups), raw, policy)
			}
		}
	}
}

// These are independent application boards, not native qualification records.
// Shared root attribution must survive the complete original component union.
func TestStorageIdentityLaunchComponentUnionPreservesSharedMappings(t *testing.T) {
	original := storagebudget.Component{Filesystem: storagebudget.ID{1}, FilesystemGeneration: 2, Root: storagebudget.ID{3}, RootGeneration: 4, Owner: storagebudget.Digest{5}, Workspace: storagebudget.ID{6}, WorkspaceGeneration: 7, Category: storagebudget.Control, Physical: storagebudget.Vector{Bytes: 11, Files: 1}, Attribution: storagebudget.Vector{Bytes: 13, Files: 1}, SettlementPhysical: storagebudget.Vector{Bytes: 17, Files: 2, Directories: 1}, SettlementAttribution: storagebudget.Vector{Bytes: 19, Files: 2, Directories: 1}, CapacityCount: 2, CapacityBinding: storagebudget.Digest{8}, Capacity: [4]storagebudget.CapacityPromise{{Constraint: storagebudget.ID{20}, Generation: 9, Peak: storagebudget.CapacityVector{Bytes: 23, Inodes: 2}}, {Constraint: storagebudget.ID{21}, Generation: 10, Peak: storagebudget.CapacityVector{Bytes: 29, Inodes: 3}}}}
	additional := original
	additional.Physical = storagebudget.Vector{Bytes: 31, Files: 2, Directories: 1}
	additional.Attribution = storagebudget.Vector{Bytes: 37, Files: 3, Directories: 1}
	additional.SettlementPhysical = storagebudget.Vector{Bytes: 41, Files: 4, Directories: 2}
	additional.SettlementAttribution = storagebudget.Vector{Bytes: 43, Files: 5, Directories: 3}
	additional.Capacity[0].Peak = storagebudget.CapacityVector{Bytes: 47, Inodes: 5}
	additional.Capacity[1].Peak = storagebudget.CapacityVector{Bytes: 53, Inodes: 7}
	shared := original
	shared.Workspace = storagebudget.ID{60}
	shared.WorkspaceGeneration = 8
	shared.Physical, shared.SettlementPhysical = storagebudget.Vector{}, storagebudget.Vector{}
	shared.Capacity[0].Peak, shared.Capacity[1].Peak = storagebudget.CapacityVector{}, storagebudget.CapacityVector{}
	before := []storagebudget.Component{original, shared}
	fresh := []storagebudget.Component{additional}
	got, err := storageIdentityComponentUnion(before, fresh)
	storageTestMust(t, err)
	want := original
	want.Physical = storagebudget.Vector{Bytes: 42, Files: 3, Directories: 1}
	want.Attribution = storagebudget.Vector{Bytes: 50, Files: 4, Directories: 1}
	want.SettlementPhysical = storagebudget.Vector{Bytes: 58, Files: 6, Directories: 3}
	want.SettlementAttribution = storagebudget.Vector{Bytes: 62, Files: 7, Directories: 4}
	want.Capacity[0].Peak = storagebudget.CapacityVector{Bytes: 70, Inodes: 7}
	want.Capacity[1].Peak = storagebudget.CapacityVector{Bytes: 82, Inodes: 10}
	if !reflect.DeepEqual(got, []storagebudget.Component{want, shared}) {
		t.Fatal("shared mapping or independent promise lost", got)
	}
	if !reflect.DeepEqual(before, []storagebudget.Component{original, shared}) || !reflect.DeepEqual(fresh, []storagebudget.Component{additional}) {
		t.Fatal("input board mutated")
	}
}

func TestStorageIdentityLaunchComponentUnionRefusesLostDebtAndIncompatibleAliases(t *testing.T) {
	base := storagebudget.Component{Filesystem: storagebudget.ID{1}, FilesystemGeneration: 1, Root: storagebudget.ID{2}, RootGeneration: 1, Owner: storagebudget.Digest{3}, Workspace: storagebudget.ID{4}, WorkspaceGeneration: 1, Category: storagebudget.Control, SettlementPhysical: storagebudget.Vector{Bytes: 1}, CapacityCount: 1, CapacityBinding: storagebudget.Digest{5}, Capacity: [4]storagebudget.CapacityPromise{{Constraint: storagebudget.ID{6}, Generation: 1, Peak: storagebudget.CapacityVector{Bytes: 10, Inodes: 1}}}}
	for name, change := range map[string]func(*storagebudget.Component){
		"root generation":       func(c *storagebudget.Component) { c.RootGeneration++ },
		"owner":                 func(c *storagebudget.Component) { c.Owner[0]++ },
		"filesystem generation": func(c *storagebudget.Component) { c.FilesystemGeneration++ },
		"workspace generation":  func(c *storagebudget.Component) { c.WorkspaceGeneration++ },
		"capacity generation":   func(c *storagebudget.Component) { c.Capacity[0].Generation++ },
		"capacity binding":      func(c *storagebudget.Component) { c.CapacityBinding[0]++ },
	} {
		t.Run(name, func(t *testing.T) {
			bad := base
			change(&bad)
			if _, err := storageIdentityComponentUnion([]storagebudget.Component{base}, []storagebudget.Component{bad}); err == nil {
				t.Fatal("different original mapping aliased")
			}
		})
	}
	missing := base
	missing.SettlementPhysical = storagebudget.Vector{}
	if _, err := storageIdentityComponentUnion([]storagebudget.Component{missing}, nil); err == nil {
		t.Fatal("retained zero-peak row lost completion debt")
	}
	attributionOnly := base
	attributionOnly.SettlementPhysical = storagebudget.Vector{}
	attributionOnly.SettlementAttribution = storagebudget.Vector{Bytes: 31, Files: 1}
	attributionOnly.Capacity[0].Peak = storagebudget.CapacityVector{}
	if got, err := storageIdentityComponentUnion([]storagebudget.Component{attributionOnly}, nil); err != nil || !reflect.DeepEqual(got, []storagebudget.Component{attributionOnly}) {
		t.Fatal("valid attribution-only completion debt lost", got, err)
	}
	for _, peak := range []storagebudget.CapacityVector{{Bytes: 1}, {Inodes: 1}} {
		bad := attributionOnly
		bad.Capacity[0].Peak = peak
		if _, err := storageIdentityComponentUnion([]storagebudget.Component{bad}, nil); err == nil {
			t.Fatal("attribution-only mapping acquired a physical capacity peak", peak)
		}
		if _, err := storageIdentityComponentUnion(nil, []storagebudget.Component{bad}); err == nil {
			t.Fatal("fresh attribution-only mapping acquired a physical capacity peak", peak)
		}
	}
	rows := make([]storagebudget.Component, 17)
	for i := range rows {
		rows[i] = base
		rows[i].Root = storagebudget.ID{byte(i + 1)}
	}
	if _, err := storageIdentityComponentUnion(nil, rows); err == nil {
		t.Fatal("17-component application admitted")
	}
	for _, bytes := range []uint64{134217727, 134217728} {
		row := base
		row.SettlementPhysical.Bytes = bytes
		_, err := storageIdentityComponentUnion(nil, []storagebudget.Component{row})
		if (err == nil) != (bytes == 134217727) {
			t.Fatal("strict per-application raw settlement ceiling", bytes, err)
		}
	}
	bad := base
	bad.Capacity[1] = storagebudget.CapacityPromise{Constraint: storagebudget.ID{7}, Generation: 1}
	if _, err := storageIdentityComponentUnion(nil, []storagebudget.Component{bad}); err == nil {
		t.Fatal("hidden capacity reference admitted")
	}
	bad = base
	bad.CapacityCount = 5
	if _, err := storageIdentityComponentUnion(nil, []storagebudget.Component{bad}); err == nil {
		t.Fatal("capacity reference count widened")
	}
}

func TestStorageIdentityLaunchCheckedOriginalCommitments(t *testing.T) {
	// Infrastructure, original owner spend, current Q promise and the complete
	// pending board coexist. This sum is independent of the group estimator.
	parts := []storagebudget.CapacityVector{{Bytes: 101, Inodes: 2}, {Bytes: 211, Inodes: 3}, {Bytes: 307, Inodes: 5}, {Bytes: 401, Inodes: 7}}
	var got storagebudget.CapacityVector
	for _, part := range parts {
		var err error
		got, err = storageIdentityAddCapacity(got, part)
		storageTestMust(t, err)
	}
	if got != (storagebudget.CapacityVector{Bytes: 1020, Inodes: 17}) {
		t.Fatal("original commitment omitted", got)
	}
	for _, pair := range [][2]storagebudget.CapacityVector{
		{{Bytes: 1 << 60}, {Bytes: 1}},
		{{Inodes: 1 << 40}, {Inodes: 1}},
		{{Bytes: (1 << 60) + 1}, {}},
		{{Inodes: (1 << 40) + 1}, {}},
	} {
		if _, err := storageIdentityAddCapacity(pair[0], pair[1]); err == nil {
			t.Fatal("overflow accepted", pair)
		}
	}
	if _, err := storageIdentityAddVector(storagebudget.Vector{Files: 1 << 40}, storagebudget.Vector{Files: 1}); err == nil {
		t.Fatal("file overflow hidden by byte bound")
	}
	if _, err := storageIdentityAddVector(storagebudget.Vector{Directories: 1 << 40}, storagebudget.Vector{Directories: 1}); err == nil {
		t.Fatal("directory overflow hidden by byte bound")
	}
}

func TestStorageIdentitySharedSettlementOriginalVectors(t *testing.T) {
	// Handwritten factual deltas. The independent 17/19 debt vectors and peaks
	// belong to admission only; neither becomes a second physical file effect.
	leader := storageIdentityIntentFixture().Core.Components[0]
	leader.Workspace, leader.WorkspaceGeneration = storageIdentityIntentID(201), 7
	leader.Physical, leader.Attribution = storagebudget.Vector{Bytes: 512, Files: 1}, storagebudget.Vector{Bytes: 512, Files: 1}
	leader.SettlementPhysical, leader.SettlementAttribution = storagebudget.Vector{Bytes: 17, Files: 2}, storagebudget.Vector{Bytes: 19, Files: 3}
	peer := leader
	peer.Workspace, peer.WorkspaceGeneration = storageIdentityIntentID(202), 11
	peer.Physical, peer.SettlementPhysical = storagebudget.Vector{}, storagebudget.Vector{}
	peer.SettlementAttribution = storagebudget.Vector{Bytes: 23, Files: 4}
	for i := range peer.Capacity {
		peer.Capacity[i].Peak = storagebudget.CapacityVector{}
	}
	unrelated := leader
	unrelated.Root = storageIdentityIntentID(203)
	unrelated.Workspace = storageIdentityIntentID(204)
	base := storageIdentityIntent{Core: storageIntentRecord{Components: []storagebudget.Component{leader, peer, unrelated}, RootIdentities: []storagebudget.Digest{{31}, {31}, {32}}, FilesystemIdentities: []storagebudget.Digest{{41}, {41}, {41}}, Locators: []string{"original-private", "original-private", "unrelated"}, NewLength: 13, OldLength: 29, Outcome: storageIntentPublished}}
	for _, tc := range []struct {
		name    string
		action  byte
		old     uint64
		outcome storageIntentOutcome
		want    []storagebudget.Change
	}{
		{"create-two-workspaces", 1, 0, storageIntentPublished, []storagebudget.Change{{PhysicalAdd: storagebudget.Vector{Bytes: 13, Files: 1}, AttributionAdd: storagebudget.Vector{Bytes: 13, Files: 1}}, {AttributionAdd: storagebudget.Vector{Bytes: 13, Files: 1}}, {}}},
		{"clear-replaces29-with13", 2, 29, storageIntentPublished, []storagebudget.Change{{PhysicalAdd: storagebudget.Vector{Bytes: 13, Files: 1}, PhysicalRemove: storagebudget.Vector{Bytes: 29, Files: 1}, AttributionAdd: storagebudget.Vector{Bytes: 13, Files: 1}, AttributionRemove: storagebudget.Vector{Bytes: 29, Files: 1}}, {AttributionAdd: storagebudget.Vector{Bytes: 13, Files: 1}, AttributionRemove: storagebudget.Vector{Bytes: 29, Files: 1}}, {}}},
		{"initial-same-bytes-keeps-add-and-remove", 4, 13, storageIntentPublished, []storagebudget.Change{{PhysicalAdd: storagebudget.Vector{Bytes: 13, Files: 1}, PhysicalRemove: storagebudget.Vector{Bytes: 13, Files: 1}, AttributionAdd: storagebudget.Vector{Bytes: 13, Files: 1}, AttributionRemove: storagebudget.Vector{Bytes: 13, Files: 1}}, {AttributionAdd: storagebudget.Vector{Bytes: 13, Files: 1}, AttributionRemove: storagebudget.Vector{Bytes: 13, Files: 1}}, {}}},
		{"stamp-zero", 3, 0, storageIntentPublished, []storagebudget.Change{{}, {}, {}}},
		{"abort-zero", 1, 0, storageIntentAborted, []storagebudget.Change{{}, {}, {}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			r.Action, r.Core.OldLength, r.Core.Outcome = tc.action, tc.old, tc.outcome
			before := append([]storagebudget.Component{}, r.Core.Components...)
			got, e := storageIdentitySettlementChanges(r)
			storageTestMust(t, e)
			if !reflect.DeepEqual(got, tc.want) || !reflect.DeepEqual(before, r.Core.Components) {
				t.Fatal("factual projection or original debt changed", got)
			}
		})
	}
	for _, mutate := range []func(*storageIdentityIntent){
		func(r *storageIdentityIntent) { r.Core.Components[1].Owner[0] ^= 1 },
		func(r *storageIdentityIntent) { r.Core.Components[1].RootGeneration++ },
		func(r *storageIdentityIntent) { r.Core.Components[1].FilesystemGeneration++ },
		func(r *storageIdentityIntent) { r.Core.Components[1].Capacity[0].Generation++ },
		func(r *storageIdentityIntent) { r.Core.Components[1].CapacityBinding[0] ^= 1 },
		func(r *storageIdentityIntent) { r.Core.Components[1].Workspace = r.Core.Components[0].Workspace },
		func(r *storageIdentityIntent) { r.Core.RootIdentities[1][0] ^= 1 },
		func(r *storageIdentityIntent) { r.Core.FilesystemIdentities[1][0] ^= 1 },
		func(r *storageIdentityIntent) { r.Core.Locators[1] = "foreign" },
	} {
		r := base
		r.Core.Components = append([]storagebudget.Component{}, base.Core.Components...)
		r.Core.RootIdentities = append([]storagebudget.Digest{}, base.Core.RootIdentities...)
		r.Core.FilesystemIdentities = append([]storagebudget.Digest{}, base.Core.FilesystemIdentities...)
		r.Core.Locators = append([]string{}, base.Core.Locators...)
		mutate(&r)
		if _, e := storageIdentitySettlementChanges(r); e == nil {
			t.Fatal("malformed original peer became unrelated/accepted")
		}
	}
}
func TestStorageIdentityOriginalModeAndRequestContract(t *testing.T) {
	p := storageOwnerV5LineagePlan{}
	// Use the canonical encoder rather than rely on JSON field order here. The
	// expected original mode selection remains independent of the implementation.
	identity := identityRecord{ID: strings.Repeat("a", 64), Path: "/original", Folder: "1:2"}
	p.IdentityInitial = identity.line()
	got, e := storageIdentityRequiredActions(p, storageCompositionInitial{Mode: 1})
	storageTestMust(t, e)
	if !reflect.DeepEqual(got, []byte{1}) {
		t.Fatal("positive absence lost original create", got)
	}
	if _, e = storageIdentityRequiredActions(p, storageCompositionInitial{Mode: 1, ExpectedOld: storageIdentityIntentPO(51, 2)}); e == nil {
		t.Fatal("mode1 accepted nonzero old tuple")
	}
	got, e = storageIdentityRequiredActions(p, storageCompositionInitial{Mode: 2, ExpectedOld: storageIdentityIntentPO(51, 2)})
	storageTestMust(t, e)
	if !reflect.DeepEqual(got, []byte{4}) {
		t.Fatal("legacy identity bypassed same-bytes CAS", got)
	}
	identity.From = strings.Repeat("b", 64)
	p.IdentityInitial = identity.line()
	got, e = storageIdentityRequiredActions(p, storageCompositionInitial{Mode: 1})
	storageTestMust(t, e)
	if !reflect.DeepEqual(got, []byte{1, 3, 2}) {
		t.Fatal("original C/S/F order changed", got)
	}
	r := storageIdentityIntentFixture()
	request, e := storageIdentityRequest(r, storageIdentityIntentHash(91))
	storageTestMust(t, e)
	expected := storagebudget.Request{Workflow: storageHostWorkflow, Operation: "single:" + storageHexID(r.Core.OperationID), Owner: storageIdentityIntentHash(91), Components: append([]storagebudget.Component{}, r.Core.Components...), Entries: []storagebudget.Entry{{Kind: storagebudget.ObjectEntry, Component: 0, Target: "project.json", Stage: ".identity-stage", OldIdentity: r.Core.CASBinding, NewIdentity: r.Core.CASBinding, Bytes: 100, Files: 1}}}
	if !reflect.DeepEqual(request, expected) {
		t.Fatal("original outer Q/entry contract changed", request)
	}
}

func TestStorageIdentitySharedTerminalValidation(t *testing.T) {
	for _, action := range []byte{1, 2, 3, 4} {
		t.Run(fmt.Sprint(action), func(t *testing.T) {
			r := storageIdentityIntentState(storageIdentityTargetDurable)
			r.Action = action
			r.Core.NewLength = 13
			r.Core.Phase = storageIntentSettling
			r.Core.Outcome = storageIntentPublished
			leader := r.Core.Components[0]
			leader.Workspace, leader.WorkspaceGeneration = storageIdentityIntentID(201), 1
			leader.Attribution = leader.Physical
			leader.SettlementAttribution = storagebudget.Vector{Bytes: 21, Files: 1}
			peer := leader
			peer.Workspace = storageIdentityIntentID(202)
			peer.Physical, peer.SettlementPhysical = storagebudget.Vector{}, storagebudget.Vector{}
			peer.SettlementAttribution = storagebudget.Vector{Bytes: 37, Files: 2}
			for i := range peer.Capacity {
				peer.Capacity[i].Peak = storagebudget.CapacityVector{}
			}
			r.Core.Components = []storagebudget.Component{leader, peer}
			r.Core.RootIdentities = append(r.Core.RootIdentities, r.Core.RootIdentities[0])
			r.Core.FilesystemIdentities = append(r.Core.FilesystemIdentities, r.Core.FilesystemIdentities[0])
			r.Core.Locators = append(r.Core.Locators, r.Core.Locators[0])
			name, domain := "create", "desk-storage-identity-publication-v1"
			if action != 1 {
				domain = "desk-storage-identity-action-publication-v2"
				name = "clear"
				if action == 4 {
					name = "initial-cas"
				}
				r.Core.OldExists = true
				r.Core.OldIdentity = storageDirectoryIdentity{Device: 1, Inode: 77}
				r.Core.OldLength = 29
				if action == 4 {
					r.Core.OldLength = 13
				}
				r.Core.OldLinkCount = 1
				r.ExpectedOld = storageIdentityIntentPO(77, 2)
				r.Presence = 27
				r.LinkedStage = storageOwnerPersistentObject{}
				r.LinkedStageLinks = 0
				r.LinkedTargetLinks = 1
			}
			if action == 3 {
				name = "stamp"
				r.Core.NewLength = 0
				r.Core.OldExists = false
				r.Core.OldIdentity = storageDirectoryIdentity{}
				r.Core.OldLength, r.Core.OldLinkCount = 0, 0
				r.Core.StageExists = false
				r.PayloadDigest = storagebudget.Digest{}
				r.ExpectedOld = storageOwnerPersistentObject{}
				r.AllocatedStage = storageIdentityIntentPO(78, 1)
				r.Target = r.AllocatedStage
				r.AllocatedLinks, r.LinkedTargetLinks, r.FinalTargetLinks = 0, 0, 0
				r.Presence = 11
				r.Facts = 127
			}
			r.Core.Operation = fmt.Sprintf("p:%x:%s", r.PlanDigest, name)
			var err error
			r.Core.Changes, err = storageIdentitySettlementChanges(r)
			storageTestMust(t, err)
			r.Core.Evidence, err = storageOwnerDigest(domain, storageIdentityPublicationMaterial(r))
			storageTestMust(t, err)
			storageTestMust(t, r.validate())
			if action != 3 {
				bad := r
				bad.Core.Changes = append([]storagebudget.Change{}, r.Core.Changes...)
				bad.Core.Changes[1] = storagebudget.Change{}
				bad.Core.Evidence, err = storageOwnerDigest(domain, storageIdentityPublicationMaterial(bad))
				storageTestMust(t, err)
				if bad.validate() == nil {
					t.Fatal("self-consistent digest hid missing original peer settlement")
				}
			}
		})
	}
}
