package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageIdentityIntentID(n byte) (x storagebudget.ID)       { x[0] = n; return }
func storageIdentityIntentHash(n byte) (x storagebudget.Digest) { x[0] = n; return }
func storageIdentityIntentPO(n uint64, kind uint8) storageOwnerPersistentObject {
	return storageOwnerPersistentObject{Backend: 1, VolumeUUID: [16]byte{1}, Object: n, Incarnation: 7, IncarnationKind: 1, ObjectKind: kind}
}

// Hand-authored source vectors, not serialized recordings or native witnesses.
func storageIdentityIntentFixture() storageIdentityIntent {
	c := storageIntentRecord{Slot: 0, Generation: 1, Phase: storageIntentPrepared, OperationID: storageIdentityIntentID(2), CASBinding: storageIdentityIntentHash(3), AdmissionEpoch: 4, NewLength: 100, Target: "project.json", Stage: ".identity-stage", Workflow: "desk-signing-lineage-v1"}
	c.Components = []storagebudget.Component{{Filesystem: storageIdentityIntentID(5), FilesystemGeneration: 1, Root: storageIdentityIntentID(6), RootGeneration: 1, Owner: storageIdentityIntentHash(7), Physical: storagebudget.Vector{Bytes: 512, Files: 2}, SettlementPhysical: storagebudget.Vector{Bytes: 128 << 20}, CapacityCount: 1, Capacity: [4]storagebudget.CapacityPromise{{Constraint: storageIdentityIntentID(8), Generation: 1, Peak: storagebudget.CapacityVector{Bytes: 129 << 20, Inodes: 2}}}, CapacityBinding: storageIdentityIntentHash(9)}}
	c.RootIdentities = []storagebudget.Digest{storageIdentityIntentHash(10)}
	c.FilesystemIdentities = []storagebudget.Digest{storageIdentityIntentHash(11)}
	c.Locators = []string{"private"}
	c.Changes = []storagebudget.Change{{}}
	r := storageIdentityIntent{Core: c, Action: 1, Presence: 1, PlanDigest: storageIdentityIntentHash(12), PayloadDigest: storageIdentityIntentHash(13), Parent: storageIdentityIntentPO(14, 1)}
	r.Core.Operation = "p:" + hex.EncodeToString(r.PlanDigest[:]) + ":create"
	return r
}
func storageIdentityIntentState(phase storageIdentityNativePhase) storageIdentityIntent {
	r := storageIdentityIntentFixture()
	r.Core.Generation = 9
	r.Core.PreviousPageDigest = storageIdentityIntentHash(15)
	r.Core.Phase = storageIntentStaging
	r.Core.PolicyRevision = 1
	r.Core.PreviousToken = storagebudget.Token{Slot: 1, ID: storageIdentityIntentID(16), Sequence: 2, Generation: 1}
	r.Core.Token = r.Core.PreviousToken
	r.Core.Token.Generation = 2
	r.NativePhase = phase
	if phase != storageIdentityNativeNone {
		r.Core.StageExists = true
		r.Presence = 3
		r.AllocatedStage = storageIdentityIntentPO(17, 2)
		r.AllocatedLinks = 1
	}
	switch phase {
	case storageIdentityPayloadReady:
		r.Facts = 127
	case storageIdentityLinkedObserved, storageIdentityTargetDurable:
		r.Core.Phase = storageIntentPublishing
		r.Presence = 15
		r.LinkedStage = storageIdentityIntentPO(17, 2)
		r.Target = storageIdentityIntentPO(17, 2)
		r.LinkedStageLinks = 2
		r.LinkedTargetLinks = 2
		r.Facts = 127
		if phase == storageIdentityTargetDurable {
			r.Facts = 32767
			r.FinalTargetLinks = 1
		}
	case storageIdentityAbortObserved:
		r.Facts = 2064384
	}
	return r
}
func storageIdentityIntentTerminal(r storageIdentityIntent, phase storageIntentPhase) storageIdentityIntent {
	r.Core.Phase = phase
	r.Core.Outcome = storageIntentPublished
	if r.NativePhase == storageIdentityAbortObserved {
		r.Core.Outcome = storageIntentAborted
	} else {
		r.Core.Changes[0] = storagebudget.Change{PhysicalAdd: storagebudget.Vector{Bytes: r.Core.NewLength, Files: 1}}
	}
	if phase == storageIntentSettled || phase == storageIntentRetired {
		r.Core.PreviousToken = r.Core.Token
		r.Core.Token.Generation++
	}
	if phase == storageIntentRetired {
		r.Core.RetirementEpoch = r.Core.AdmissionEpoch
	}
	r.Core.Evidence, _ = storageOwnerDigest("desk-storage-identity-publication-v1", storageIdentityPublicationMaterial(r))
	return r
}
func storageIdentityIntentSeal(b []byte) {
	clear(b[76:108])
	d := sha256.Sum256(b)
	copy(b[76:108], d[:])
}
func TestStorageIdentityIntentV3ExactLayout(t *testing.T) {
	r := storageIdentityIntentState(storageIdentityTargetDurable)
	b, err := storageEncodeIdentityIntent(r, storageIdentityIntentID(90), storageIdentityIntentHash(91))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 16384 || string(b[:8]) != "DSKHINT3" || binary.LittleEndian.Uint32(b[72:76]) != 16047 || binary.LittleEndian.Uint16(b[15791:15793]) != 1 || b[15793] != 1 || b[15794] != 4 || binary.LittleEndian.Uint16(b[15795:15797]) != 15 || b[15863] != 1 || binary.LittleEndian.Uint64(b[15880:15888]) != 14 || b[15897] != 1 || b[15898] != 1 || binary.LittleEndian.Uint64(b[16038:16046]) != 1 || binary.LittleEndian.Uint32(b[16070:16074]) != 32767 || !storageAllZero(b[16074:]) || len(b[16175:]) != 209 {
		t.Fatal("hand-authored wire offsets changed")
	}
	got, err := storageDecodeIdentityIntent(b, 0, storageIdentityIntentID(90), storageIdentityIntentHash(91))
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatal("round trip", err)
	}
	if _, err := storageDecodeIntent(b, 0, storageIdentityIntentID(90), storageIdentityIntentHash(91)); err == nil {
		t.Fatal("v2 accepted v3")
	}
}
func TestStorageIdentityIntentV3InvalidCrossproducts(t *testing.T) {
	phases := []storageIdentityNativePhase{0, 1, 2, 3, 4, 5, 6}
	for _, p := range phases {
		r := storageIdentityIntentState(p)
		if err := r.validate(); err != nil {
			t.Fatalf("native %d: %v", p, err)
		}
		for flags := uint16(0); flags < 64; flags++ {
			if flags == r.Presence {
				continue
			}
			bad := r
			bad.Presence = flags
			if bad.validate() == nil {
				t.Fatalf("native %d flags %d", p, flags)
			}
		}
		for core := storageIntentPrepared; core <= storageIntentRejected; core++ {
			if core == r.Core.Phase {
				continue
			}
			bad := r
			bad.Core.Phase = core
			if bad.validate() == nil {
				t.Fatalf("native %d core %d", p, core)
			}
		}
	}
	for _, mutate := range []func(*storageIdentityIntent){
		func(r *storageIdentityIntent) { r.NativePhase = 7 }, func(r *storageIdentityIntent) { r.Action = 2 }, func(r *storageIdentityIntent) { r.Facts |= 1 << 31 }, func(r *storageIdentityIntent) { r.ExpectedOld = storageIdentityIntentPO(99, 2) }, func(r *storageIdentityIntent) { r.Core.StageIdentity = storageDirectoryIdentity{1, 2} }, func(r *storageIdentityIntent) { r.AllocatedStage.IncarnationKind = 2 }, func(r *storageIdentityIntent) { r.AllocatedStage.Incarnation = 1 << 32 }, func(r *storageIdentityIntent) { r.Parent.ObjectKind = 2 }, func(r *storageIdentityIntent) { r.FinalTargetLinks = 2 }, func(r *storageIdentityIntent) { r.LinkedTargetLinks = 1 }, func(r *storageIdentityIntent) { r.Core.Operation = "p:short:create" }, func(r *storageIdentityIntent) { r.Core.NewLength = 513 },
	} {
		r := storageIdentityIntentState(storageIdentityTargetDurable)
		mutate(&r)
		if r.validate() == nil {
			t.Fatal("invalid native/core field accepted")
		}
	}
}
func TestStorageIdentityIntentV3ChecksumPaddingTorn(t *testing.T) {
	installation, binding := storageIdentityIntentID(90), storageIdentityIntentHash(91)
	b, err := storageEncodeIdentityIntent(storageIdentityIntentState(storageIdentityTargetDurable), installation, binding)
	if err != nil {
		t.Fatal(err)
	}
	for _, offset := range []int{13, 108, 15797, 16074, 16174, 16175, 16383} {
		x := append([]byte(nil), b...)
		x[offset] = 1
		storageIdentityIntentSeal(x)
		if _, err := storageDecodeIdentityIntent(x, 0, installation, binding); err == nil {
			t.Fatalf("padding %d", offset)
		}
	}
	x := append([]byte(nil), b...)
	x[15800] ^= 1
	if _, err := storageDecodeIdentityIntent(x, 0, installation, binding); err == nil {
		t.Fatal("checksum")
	}
	for _, n := range []int{0, 15791, 16175, 16383, 16385} {
		x := make([]byte, n)
		copy(x, b)
		if _, err := storageDecodeIdentityIntent(x, 0, installation, binding); err == nil {
			t.Fatal("length", n)
		}
	}
	torn := make([]byte, 16384)
	copy(torn, b[:8192])
	if _, err := storageSelectIdentityIntent(b, torn, 0, installation, binding); err == nil {
		t.Fatal("torn peer fell back")
	}
}

func storageIdentityIntentStates() []storageIdentityIntent {
	prepared := storageIdentityIntentFixture()
	prepared.Core.Generation = 9
	prepared.Core.PreviousPageDigest = storageIdentityIntentHash(15)
	reserved := storageIdentityIntentState(0)
	reserved.Core.Phase = storageIntentReserved
	reserved.Core.Token = reserved.Core.PreviousToken
	reserved.Core.PreviousToken = storagebudget.Token{}
	rejected := prepared
	rejected.Core.Phase = storageIntentRejected
	rejected.Core.Outcome = storageIntentNotAdmitted
	rejected.Core.Rejection = storageIntentQuota
	return []storageIdentityIntent{prepared, reserved, storageIdentityIntentState(0), storageIdentityIntentState(1), storageIdentityIntentState(2), storageIdentityIntentState(3), storageIdentityIntentState(4), storageIdentityIntentState(5), storageIdentityIntentState(6), storageIdentityIntentTerminal(storageIdentityIntentState(4), storageIntentSettling), storageIdentityIntentTerminal(storageIdentityIntentState(4), storageIntentSettled), storageIdentityIntentTerminal(storageIdentityIntentState(4), storageIntentRetired), rejected, storageIdentityIntentTerminal(storageIdentityIntentState(6), storageIntentSettling), storageIdentityIntentTerminal(storageIdentityIntentState(6), storageIntentSettled), storageIdentityIntentTerminal(storageIdentityIntentState(6), storageIntentRetired)}
}
func TestStorageIdentityIntentV3ClosedTransitionMatrix(t *testing.T) {
	// Authored state indices: P,R,S,A,Payload,Link,Durable,AbortP,AbortO,
	// published settling/settled/retired, rejected, aborted settling/settled/retired.
	allowed := map[[2]int]bool{{0, 1}: true, {0, 12}: true, {1, 2}: true, {2, 3}: true, {3, 4}: true, {3, 7}: true, {4, 5}: true, {5, 6}: true, {6, 9}: true, {7, 8}: true, {8, 13}: true, {9, 10}: true, {10, 11}: true, {13, 14}: true, {14, 15}: true}
	states := storageIdentityIntentStates()
	installation, binding := storageIdentityIntentID(90), storageIdentityIntentHash(91)
	for i, old := range states {
		op, err := storageEncodeIdentityIntent(old, installation, binding)
		if err != nil {
			t.Fatalf("state %d %v", i, err)
		}
		for j, template := range states {
			next := template
			next.Core.Generation = old.Core.Generation + 1
			next.Core.PreviousPageDigest = sha256.Sum256(op)
			got := storageIdentityIntentTransition(old, next, op)
			if got != allowed[[2]int{i, j}] {
				t.Fatalf("edge %d->%d got %v", i, j, got)
			}
		}
	}
	// A payload-ready abort retains all successful payload facts, rather than
	// erasing them to reach the allocated-only abort branch.
	old := states[4]
	op, _ := storageEncodeIdentityIntent(old, installation, binding)
	next := states[7]
	next.Facts = 127
	next.Core.Generation = 10
	next.Core.PreviousPageDigest = sha256.Sum256(op)
	if !storageIdentityIntentTransition(old, next, op) {
		t.Fatal("payload abort edge")
	}
	abortPage, _ := storageEncodeIdentityIntent(next, installation, binding)
	observed := states[8]
	observed.Facts = 2064384 | 127
	observed.Core.Generation = 11
	observed.Core.PreviousPageDigest = sha256.Sum256(abortPage)
	if !storageIdentityIntentTransition(next, observed, abortPage) {
		t.Fatal("payload abort observations")
	}
	for _, i := range []int{11, 12, 15} {
		old := states[i]
		op, _ := storageEncodeIdentityIntent(old, installation, binding)
		fresh := storageIdentityIntentFixture()
		fresh.Core.Generation = 10
		fresh.Core.PreviousPageDigest = sha256.Sum256(op)
		fresh.Core.OperationID = storageIdentityIntentID(33)
		if !storageIdentityIntentTransition(old, fresh, op) {
			t.Fatal("fresh after completed/rejected", i)
		}
	}
}
func TestStorageIdentityIntentV3ImmutableAndTokenEdges(t *testing.T) {
	old := storageIdentityIntentState(1)
	installation, binding := storageIdentityIntentID(90), storageIdentityIntentHash(91)
	op, _ := storageEncodeIdentityIntent(old, installation, binding)
	base := storageIdentityIntentState(2)
	base.Core.Generation = 10
	base.Core.PreviousPageDigest = sha256.Sum256(op)
	for _, mutate := range []func(*storageIdentityIntent){
		func(r *storageIdentityIntent) { r.Core.OperationID[0]++ }, func(r *storageIdentityIntent) { r.PlanDigest[0]++ }, func(r *storageIdentityIntent) { r.PayloadDigest[0]++ }, func(r *storageIdentityIntent) { r.Parent.Object++ }, func(r *storageIdentityIntent) { r.AllocatedStage.Incarnation++ }, func(r *storageIdentityIntent) { r.Core.Token.Generation++ }, func(r *storageIdentityIntent) { r.Core.PolicyRevision++ }, func(r *storageIdentityIntent) { r.Core.Locators = []string{"other"} }, func(r *storageIdentityIntent) { r.Core.Generation++ }, func(r *storageIdentityIntent) { r.Core.PreviousPageDigest[0]++ },
	} {
		r := base
		mutate(&r)
		if storageIdentityIntentTransition(old, r, op) {
			t.Fatal("immutable/token mutation")
		}
	}
}
func TestStorageIdentityIntentV3BanksAndLegacy(t *testing.T) {
	installation, binding := storageIdentityIntentID(90), storageIdentityIntentHash(91)
	zero := make([]byte, 16384)
	prepared := storageIdentityIntentFixture()
	p, _ := storageEncodeIdentityIntent(prepared, installation, binding)
	reserved := storageIdentityIntentState(0)
	reserved.Core.Phase = storageIntentReserved
	reserved.Core.Token = reserved.Core.PreviousToken
	reserved.Core.PreviousToken = storagebudget.Token{}
	reserved.Core.Generation = 2
	reserved.Core.PreviousPageDigest = sha256.Sum256(p)
	q, err := storageEncodeIdentityIntent(reserved, installation, binding)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2][]byte{{zero, zero}, {p, zero}, {zero, p}, {p, p}, {p, q}, {q, p}} {
		if _, err := storageSelectIdentityIntent(pair[0], pair[1], 0, installation, binding); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := storageSelectIdentityIntent(zero, q, 0, installation, binding); err == nil {
		t.Fatal("lost predecessor")
	}
	legacy := prepared.Core
	lp, err := storageEncodeIntent(legacy, installation, binding)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := storageSelectIdentityIntent(lp, zero, 0, installation, binding)
	if err != nil || selected.Kind != storageIdentityIntentBanksV2 {
		t.Fatal("legacy classification", err)
	}
	for _, pair := range [][2][]byte{{lp, p}, {p, lp}} {
		if _, err := storageSelectIdentityIntent(pair[0], pair[1], 0, installation, binding); !errors.Is(err, storagebudget.ErrUnsupported) {
			t.Fatal("mixed version", err)
		}
	}
	twin := append([]byte(nil), p...)
	twin[15831] ^= 1
	storageIdentityIntentSeal(twin)
	if _, err := storageSelectIdentityIntent(p, twin, 0, installation, binding); err == nil {
		t.Fatal("different duplicate")
	}
}
func TestStorageIdentityIntentV3CrashUnknownAndTerminalMapping(t *testing.T) {
	for _, phase := range []storageIdentityNativePhase{0, 1, 2, 5} {
		r := storageIdentityIntentState(phase)
		if storageIdentityIntentRecoveryBoundary(r, false) != storageIdentityRecoveryUnknown {
			t.Fatal("absence fabricated abort", phase)
		}
	}
	r := storageIdentityIntentState(6)
	if storageIdentityIntentRecoveryBoundary(r, false) != storageIdentityRecoveryNeedsAbortCompletion {
		t.Fatal("known abort must complete")
	}
	binding := storageIdentityIntentHash(91)
	for _, native := range []storageIdentityNativePhase{4, 6} {
		settling := storageIdentityIntentTerminal(storageIdentityIntentState(native), storageIntentSettling)
		ticket := storagebudget.Ticket{Token: settling.Core.Token, State: storagebudget.Staging, PolicyRevision: 1, Workflow: "storage-single-file-v1", Operation: "single:" + storageHexID(settling.Core.OperationID), Owner: binding, Components: settling.Core.Components}
		if !storageIdentityIntentTicketMapping(settling, ticket, binding) {
			t.Fatal("staging map")
		}
		ticket.Token.Generation++
		ticket.State = storagebudget.Terminal
		ticket.Changes = settling.Core.Changes
		ticket.Evidence = settling.Core.Evidence
		if !storageIdentityIntentTicketMapping(settling, ticket, binding) {
			t.Fatal("original terminal completion")
		}
		ticket.Workflow = settling.Core.Workflow
		if storageIdentityIntentTicketMapping(settling, ticket, binding) {
			t.Fatal("host semantic workflow used as Q outer workflow")
		}
		ticket.Workflow = "storage-single-file-v1"
		retired := storageIdentityIntentTerminal(storageIdentityIntentState(native), storageIntentRetired)
		var status storagebudget.HostState
		status.Epoch = retired.Core.RetirementEpoch
		status.Slots[ticket.Token.Slot].Ticket = ticket
		status.Slots[ticket.Token.Slot].PersistedApplied = ticket.Token
		status.Slots[ticket.Token.Slot].PersistedNextSequence = ticket.Token.Sequence + 1
		if !storageIdentityIntentExactRetired(retired, status, binding) {
			t.Fatal("exact current receipt")
		}
		status.Slots[ticket.Token.Slot].PersistedApplied.Sequence++
		status.Slots[ticket.Token.Slot].PersistedNextSequence++
		if storageIdentityIntentExactRetired(retired, status, binding) {
			t.Fatal("later history substituted for receipt")
		}
	}
	states := storageIdentityIntentStates()
	if storageIdentityIntentTicketMapping(states[12], storagebudget.Ticket{}, binding) {
		t.Fatal("rejected fabricated ticket")
	}
}
func TestStorageIdentityIntentV3ExactHashOperands(t *testing.T) {
	r := storageIdentityIntentTerminal(storageIdentityIntentState(4), storageIntentRetired)
	installation, binding := storageIdentityIntentID(90), storageIdentityIntentHash(91)
	raw, err := storageEncodeIdentityIntent(r, installation, binding)
	if err != nil {
		t.Fatal(err)
	}
	d, err := storageIdentityIntentDigest(raw, 0, installation, binding, r)
	if err != nil || d != sha256.Sum256(raw) {
		t.Fatal("full-page D6", err)
	}
	coreOnly := sha256.Sum256(raw[:15791])
	zeroSeal := append([]byte(nil), raw...)
	clear(zeroSeal[76:108])
	if d == coreOnly || d == sha256.Sum256(zeroSeal) {
		t.Fatal("D6 omitted extension/checksum")
	}
	wrong := r
	wrong.PayloadDigest[0]++
	if _, err := storageIdentityIntentDigest(raw, 0, installation, binding, wrong); err == nil {
		t.Fatal("expected record mismatch")
	}
	abort := storageIdentityIntentTerminal(storageIdentityIntentState(6), storageIntentRetired)
	ar, _ := storageEncodeIdentityIntent(abort, installation, binding)
	if _, err := storageIdentityIntentDigest(ar, 0, installation, binding, abort); err == nil {
		t.Fatal("aborted D6 success")
	}
	material := storageIdentityPublicationMaterial(r)
	b, err := json.Marshal(material)
	if err != nil {
		t.Fatal(err)
	}
	// Literal top-level field order is authored independently of DTO reflection.
	expected := []string{"Version", "PlanDigest", "Action", "OperationID", "CASBinding", "AdmissionEpoch", "PolicyRevision", "StagingToken", "Components", "RootIdentities", "FilesystemIdentities", "Locators", "Parent", "AllocatedStage", "LinkedStage", "Target", "ExpectedOld", "Presence", "AllocatedLinks", "LinkedStageLinks", "LinkedTargetLinks", "FinalTargetLinks", "PayloadDigest", "PayloadLength", "Facts", "Outcome"}
	decoder := json.NewDecoder(bytes.NewReader(b))
	if _, err := decoder.Token(); err != nil {
		t.Fatal(err)
	}
	for _, field := range expected {
		key, err := decoder.Token()
		if err != nil || key != field {
			t.Fatal("DTO order", key, field, err)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
	}
	if decoder.More() {
		t.Fatal("unreviewed DTO field")
	}
	prefixed := append([]byte("desk-storage-identity-publication-v1\x00"), b...)
	pd, err := storageIdentityPublicationDigest(r)
	if err != nil || pd != sha256.Sum256(prefixed) || pd != r.Core.Evidence {
		t.Fatal("exact publication operand", err)
	}
	if strings.Contains(string(b), "Evidence") || strings.Contains(string(b), "PreviousPageDigest") {
		t.Fatal("recursive digest operand")
	}
}
func TestStorageIdentityIntentV3MaximumCoreRetained(t *testing.T) {
	r := storageIdentityIntentState(storageIdentityTargetDurable)
	c := r.Core.Components[0]
	for i := 1; i < 16; i++ {
		next := c
		next.Root = storageIdentityIntentID(byte(100 + i))
		r.Core.Components = append(r.Core.Components, next)
		r.Core.RootIdentities = append(r.Core.RootIdentities, storageIdentityIntentHash(byte(120+i)))
		r.Core.FilesystemIdentities = append(r.Core.FilesystemIdentities, r.Core.FilesystemIdentities[0])
		r.Core.Locators = append(r.Core.Locators, strings.Repeat("l", 256))
		r.Core.Changes = append(r.Core.Changes, storagebudget.Change{})
	}
	r.Core.Target = strings.Repeat("t", 1024)
	r.Core.Stage = strings.Repeat("s", 1024)
	r.Core.NewLength = 512
	installation, binding := storageIdentityIntentID(90), storageIdentityIntentHash(91)
	b, err := storageEncodeIdentityIntent(r, installation, binding)
	if err != nil {
		t.Fatal(err)
	}
	if b[390] != 16 || binary.LittleEndian.Uint16(b[9351:9353]) != 1024 || binary.LittleEndian.Uint16(b[10377:10379]) != 1024 || binary.LittleEndian.Uint16(b[15533:15535]) != 256 || !storageAllZero(b[16175:]) {
		t.Fatal("full core bounds narrowed/overlap")
	}
	got, err := storageDecodeIdentityIntent(b, 0, installation, binding)
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatal(err)
	}
}
func TestStorageIdentityIntentV3D8CanonicalBytes(t *testing.T) {
	raw := []byte("{\"id\":\"0000000000000000000000000000000000000000000000000000000000000001\",\"path\":\"/project\"}\n")
	r := storageIdentityIntentFixture()
	r.Core.NewLength = uint64(len(raw))
	r.PayloadDigest = sha256.Sum256(raw)
	d, err := storageIdentityIntentPayloadOperand(r, raw)
	if err != nil || d != sha256.Sum256(raw) {
		t.Fatal("D8 canonical LF operand", err)
	}
	for _, bad := range [][]byte{raw[:len(raw)-1], append(append([]byte(nil), raw...), '\n'), []byte("{}\n"), make([]byte, 513)} {
		if _, err := storageIdentityIntentPayloadOperand(r, bad); err == nil {
			t.Fatal("noncanonical/bounded D8 accepted")
		}
	}
	other := append([]byte(nil), raw...)
	other[len(other)-4] = 'x'
	if _, err := storageIdentityIntentPayloadOperand(r, other); err == nil {
		t.Fatal("different exact bytes accepted")
	}
}
func TestStorageIdentityIntentV3FactCrossproductsAndEvidence(t *testing.T) {
	masks := []uint32{0, 127, 32767, 2064384, 2064511, 1 << 31}
	// Hand-authored legal facts by numeric native phase; no field is inferred
	// from successful serialization or a running native implementation.
	allowed := map[storageIdentityNativePhase]map[uint32]bool{0: {0: true}, 1: {0: true}, 2: {127: true}, 3: {127: true}, 4: {32767: true}, 5: {0: true, 127: true}, 6: {2064384: true, 2064511: true}}
	for phase := storageIdentityNativePhase(0); phase <= 6; phase++ {
		for _, facts := range masks {
			r := storageIdentityIntentState(phase)
			r.Facts = facts
			if (r.validate() == nil) != allowed[phase][facts] {
				t.Fatalf("native %d facts %d", phase, facts)
			}
		}
	}
	good := storageIdentityIntentTerminal(storageIdentityIntentState(4), storageIntentRetired)
	for _, mutate := range []func(*storageIdentityIntent){func(r *storageIdentityIntent) { r.Core.Evidence[0] ^= 1 }, func(r *storageIdentityIntent) { r.Core.Outcome = storageIntentAborted }, func(r *storageIdentityIntent) { r.Core.Changes = []storagebudget.Change{{}} }, func(r *storageIdentityIntent) { r.PayloadDigest[0] ^= 1 }, func(r *storageIdentityIntent) { r.Core.RetirementEpoch = 0 }} {
		r := good
		mutate(&r)
		if r.validate() == nil {
			t.Fatal("terminal evidence/outcome/delta")
		}
	}
}

func TestStorageIdentityIntentV3AbortCompletionExactPayloadFacts(t *testing.T) {
	// IP1-R1: completion records removal facts, never new payload-success facts.
	// These numeric masks are authored independently of the transition helper.
	cases := []struct {
		oldFacts, nextFacts uint32
		allowed             bool
	}{
		{0, 2064384, true},
		{0, 2064511, false},
		{127, 2064511, true},
		{127, 2064384, false},
	}
	for _, tc := range cases {
		old := storageIdentityIntentState(5)
		old.Facts = tc.oldFacts
		page, err := storageEncodeIdentityIntent(old, storageIdentityIntentID(90), storageIdentityIntentHash(91))
		if err != nil {
			t.Fatal(err)
		}
		next := storageIdentityIntentState(6)
		next.Facts = tc.nextFacts
		next.Core.Generation = 10
		next.Core.PreviousPageDigest = sha256.Sum256(page)
		if got := storageIdentityIntentTransition(old, next, page); got != tc.allowed {
			t.Fatalf("abort facts %d->%d: got %v", tc.oldFacts, tc.nextFacts, got)
		}
	}
}

// IP1-R2: test-local nested DTOs pin complete JSON spelling independently of
// production Token, Component, Vector, CapacityPromise and native tuple types.
// Neither this expectation nor its Evidence is projected from production code.
type storageIdentityExpectedToken struct {
	Slot       uint8
	ID         [16]byte
	Sequence   uint64
	Generation uint64
}
type storageIdentityExpectedVector struct {
	Bytes       uint64
	Files       uint64
	Directories uint64
}
type storageIdentityExpectedCapacityVector struct {
	Bytes  uint64
	Inodes uint64
}
type storageIdentityExpectedCapacity struct {
	Constraint [16]byte
	Generation uint64
	Peak       storageIdentityExpectedCapacityVector
}
type storageIdentityExpectedComponent struct {
	Filesystem            [16]byte
	FilesystemGeneration  uint64
	Root                  [16]byte
	RootGeneration        uint64
	Owner                 [32]byte
	Workspace             [16]byte
	WorkspaceGeneration   uint64
	Category              uint8
	Physical              storageIdentityExpectedVector
	Attribution           storageIdentityExpectedVector
	SettlementPhysical    storageIdentityExpectedVector
	SettlementAttribution storageIdentityExpectedVector
	CapacityCount         uint8
	Capacity              [4]storageIdentityExpectedCapacity
	CapacityBinding       [32]byte
}
type storageIdentityExpectedNative struct {
	Backend         uint8
	VolumeUUID      [16]byte
	Object          uint64
	Incarnation     uint64
	IncarnationKind uint8
	ObjectKind      uint8
}
type storageIdentityExpectedPublication struct {
	Version              uint32
	PlanDigest           [32]byte
	Action               uint8
	OperationID          [16]byte
	CASBinding           [32]byte
	AdmissionEpoch       uint64
	PolicyRevision       uint64
	StagingToken         storageIdentityExpectedToken
	Components           []storageIdentityExpectedComponent
	RootIdentities       [][32]byte
	FilesystemIdentities [][32]byte
	Locators             []string
	Parent               storageIdentityExpectedNative
	AllocatedStage       storageIdentityExpectedNative
	LinkedStage          storageIdentityExpectedNative
	Target               storageIdentityExpectedNative
	ExpectedOld          storageIdentityExpectedNative
	Presence             uint16
	AllocatedLinks       uint64
	LinkedStageLinks     uint64
	LinkedTargetLinks    uint64
	FinalTargetLinks     uint64
	PayloadDigest        [32]byte
	PayloadLength        uint64
	Facts                uint32
	Outcome              uint8
}

func storageIdentityIntentExpectedPublication(aborted bool) storageIdentityExpectedPublication {
	// Every value is hand-authored; no record/material helper is consulted.
	e := storageIdentityExpectedPublication{
		Version: 1, PlanDigest: [32]byte{12}, Action: 1, OperationID: [16]byte{2}, CASBinding: [32]byte{3}, AdmissionEpoch: 4, PolicyRevision: 17,
		StagingToken: storageIdentityExpectedToken{Slot: 5, ID: [16]byte{16}, Sequence: 27, Generation: 31},
		Components: []storageIdentityExpectedComponent{{
			Filesystem: [16]byte{5}, FilesystemGeneration: 6, Root: [16]byte{7}, RootGeneration: 8, Owner: [32]byte{9}, Workspace: [16]byte{10}, WorkspaceGeneration: 11, Category: 2,
			Physical: storageIdentityExpectedVector{Bytes: 512, Files: 2, Directories: 1}, Attribution: storageIdentityExpectedVector{Bytes: 450, Files: 2, Directories: 1},
			SettlementPhysical: storageIdentityExpectedVector{Bytes: 4096, Files: 2, Directories: 1}, SettlementAttribution: storageIdentityExpectedVector{Bytes: 50, Files: 1, Directories: 1},
			CapacityCount: 2, Capacity: [4]storageIdentityExpectedCapacity{
				{Constraint: [16]byte{20}, Generation: 22, Peak: storageIdentityExpectedCapacityVector{Bytes: 10000, Inodes: 7}},
				{Constraint: [16]byte{21}, Generation: 23, Peak: storageIdentityExpectedCapacityVector{Bytes: 20000, Inodes: 9}},
			}, CapacityBinding: [32]byte{24},
		}},
		RootIdentities: [][32]byte{{25}}, FilesystemIdentities: [][32]byte{{26}}, Locators: []string{"private/original"},
		Parent:         storageIdentityExpectedNative{Backend: 2, VolumeUUID: [16]byte{31, 32, 33}, Object: 101, Incarnation: 201, IncarnationKind: 2, ObjectKind: 1},
		AllocatedStage: storageIdentityExpectedNative{Backend: 2, VolumeUUID: [16]byte{31, 32, 33}, Object: 102, Incarnation: 202, IncarnationKind: 2, ObjectKind: 2},
		LinkedStage:    storageIdentityExpectedNative{Backend: 2, VolumeUUID: [16]byte{31, 32, 33}, Object: 102, Incarnation: 203, IncarnationKind: 2, ObjectKind: 2},
		Target:         storageIdentityExpectedNative{Backend: 2, VolumeUUID: [16]byte{31, 32, 33}, Object: 102, Incarnation: 204, IncarnationKind: 2, ObjectKind: 2},
		ExpectedOld:    storageIdentityExpectedNative{}, Presence: 15, AllocatedLinks: 1, LinkedStageLinks: 2, LinkedTargetLinks: 2, FinalTargetLinks: 1,
		PayloadDigest: [32]byte{13}, PayloadLength: 107, Facts: 32767, Outcome: 1,
	}
	if aborted {
		e.LinkedStage = storageIdentityExpectedNative{}
		e.Target = storageIdentityExpectedNative{}
		e.Presence = 3
		e.LinkedStageLinks = 0
		e.LinkedTargetLinks = 0
		e.FinalTargetLinks = 0
		e.Facts = 2064511
		e.Outcome = 2
	}
	return e
}
func storageIdentityIntentPublicationInput(aborted bool, phase storageIntentPhase) storageIdentityIntent {
	// This record is separately authored in production types. In particular its
	// token pair changes across phases, while the expected operand always names
	// staging generation31. Evidence is assigned by the test from independent DTO.
	c := storageIntentRecord{Slot: 0, Generation: 41, Phase: phase, OperationID: storagebudget.ID{2}, PreviousPageDigest: storagebudget.Digest{15}, CASBinding: storagebudget.Digest{3}, AdmissionEpoch: 4, PolicyRevision: 17, StageExists: true, NewLength: 107,
		Token: storagebudget.Token{Slot: 5, ID: storagebudget.ID{16}, Sequence: 27, Generation: 31}, PreviousToken: storagebudget.Token{Slot: 5, ID: storagebudget.ID{16}, Sequence: 27, Generation: 30}, Outcome: storageIntentPublished, Target: "project.json", Stage: ".identity-stage", Workflow: "desk-signing-lineage-v1", Operation: "p:0c" + strings.Repeat("00", 31) + ":create",
		Components: []storagebudget.Component{{Filesystem: storagebudget.ID{5}, FilesystemGeneration: 6, Root: storagebudget.ID{7}, RootGeneration: 8, Owner: storagebudget.Digest{9}, Workspace: storagebudget.ID{10}, WorkspaceGeneration: 11, Category: 2,
			Physical: storagebudget.Vector{Bytes: 512, Files: 2, Directories: 1}, Attribution: storagebudget.Vector{Bytes: 450, Files: 2, Directories: 1}, SettlementPhysical: storagebudget.Vector{Bytes: 4096, Files: 2, Directories: 1}, SettlementAttribution: storagebudget.Vector{Bytes: 50, Files: 1, Directories: 1}, CapacityCount: 2,
			Capacity: [4]storagebudget.CapacityPromise{{Constraint: storagebudget.ID{20}, Generation: 22, Peak: storagebudget.CapacityVector{Bytes: 10000, Inodes: 7}}, {Constraint: storagebudget.ID{21}, Generation: 23, Peak: storagebudget.CapacityVector{Bytes: 20000, Inodes: 9}}}, CapacityBinding: storagebudget.Digest{24},
		}}, RootIdentities: []storagebudget.Digest{{25}}, FilesystemIdentities: []storagebudget.Digest{{26}}, Locators: []string{"private/original"}, Changes: []storagebudget.Change{{PhysicalAdd: storagebudget.Vector{Bytes: 107, Files: 1}, AttributionAdd: storagebudget.Vector{Bytes: 107, Files: 1}}},
	}
	if phase == storageIntentSettled || phase == storageIntentRetired {
		c.Token.Generation = 32
		c.PreviousToken.Generation = 31
	}
	if phase == storageIntentRetired {
		c.RetirementEpoch = 48
	}
	r := storageIdentityIntent{Core: c, Action: 1, NativePhase: storageIdentityTargetDurable, Presence: 15, PlanDigest: storagebudget.Digest{12}, PayloadDigest: storagebudget.Digest{13},
		Parent:         storageOwnerPersistentObject{Backend: 2, VolumeUUID: [16]byte{31, 32, 33}, Object: 101, Incarnation: 201, IncarnationKind: 2, ObjectKind: 1},
		AllocatedStage: storageOwnerPersistentObject{Backend: 2, VolumeUUID: [16]byte{31, 32, 33}, Object: 102, Incarnation: 202, IncarnationKind: 2, ObjectKind: 2},
		LinkedStage:    storageOwnerPersistentObject{Backend: 2, VolumeUUID: [16]byte{31, 32, 33}, Object: 102, Incarnation: 203, IncarnationKind: 2, ObjectKind: 2},
		Target:         storageOwnerPersistentObject{Backend: 2, VolumeUUID: [16]byte{31, 32, 33}, Object: 102, Incarnation: 204, IncarnationKind: 2, ObjectKind: 2}, AllocatedLinks: 1, LinkedStageLinks: 2, LinkedTargetLinks: 2, FinalTargetLinks: 1, Facts: 32767,
	}
	if aborted {
		r.Core.Outcome = storageIntentAborted
		r.Core.Changes = []storagebudget.Change{{}}
		r.NativePhase = storageIdentityAbortObserved
		r.LinkedStage = storageOwnerPersistentObject{}
		r.Target = storageOwnerPersistentObject{}
		r.Presence = 3
		r.LinkedStageLinks = 0
		r.LinkedTargetLinks = 0
		r.FinalTargetLinks = 0
		r.Facts = 2064511
	}
	return r
}
func TestStorageIdentityIntentV3IndependentCompletePublicationOperand(t *testing.T) {
	for _, aborted := range []bool{false, true} {
		expected := storageIdentityIntentExpectedPublication(aborted)
		expectedJSON, err := json.Marshal(expected)
		if err != nil {
			t.Fatal(err)
		}
		expectedDigest := sha256.Sum256(append([]byte("desk-storage-identity-publication-v1\x00"), expectedJSON...))
		for _, phase := range []storageIntentPhase{storageIntentSettling, storageIntentSettled, storageIntentRetired} {
			r := storageIdentityIntentPublicationInput(aborted, phase)
			r.Core.Evidence = expectedDigest
			if err := r.validate(); err != nil {
				t.Fatalf("independently evidenced aborted=%v phase=%d: %v", aborted, phase, err)
			}
			got := storageIdentityPublicationMaterial(r)
			gotJSON, err := json.Marshal(got)
			if err != nil || !bytes.Equal(gotJSON, expectedJSON) {
				t.Fatalf("complete nested operand aborted=%v phase=%d mismatch: %v", aborted, phase, err)
			}
			if got.StagingToken.Slot != 5 || got.StagingToken.ID != (storagebudget.ID{16}) || got.StagingToken.Sequence != 27 || got.StagingToken.Generation != 31 {
				t.Fatal("staging token is not the authored original token")
			}
			if phase == storageIntentSettling && (r.Core.Token.Generation != 31 || r.Core.PreviousToken.Generation != 30) || phase != storageIntentSettling && (r.Core.Token.Generation != 32 || r.Core.PreviousToken.Generation != 31) {
				t.Fatal("authored phase token pair")
			}
			gotDigest, err := storageIdentityPublicationDigest(r)
			if err != nil || gotDigest != expectedDigest {
				t.Fatalf("independent publication digest aborted=%v phase=%d: %v", aborted, phase, err)
			}
		}
	}
}
