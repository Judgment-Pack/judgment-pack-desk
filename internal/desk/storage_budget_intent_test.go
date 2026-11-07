package desk

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageIntentTestID(n byte) (v storagebudget.ID)         { v[0] = n; return }
func storageIntentTestDigest(n byte) (v storagebudget.Digest) { v[0] = n; return }

// Hand-authored maximum: all sixteen components, every scalar, both tokens,
// all strings at the original B1 bound, and the final phase/outcome.
func storageIntentTestRecord() storageIntentRecord {
	r := storageIntentRecord{Slot: 63, Generation: 9, Phase: storageIntentRetired, OperationID: storageIntentTestID(1), PreviousPageDigest: storageIntentTestDigest(2), CASBinding: storageIntentTestDigest(3), AdmissionEpoch: 4, PolicyRevision: 5, RetirementEpoch: 6, OldExists: true, OldIdentity: storageDirectoryIdentity{7, 8}, OldLength: 9, OldLinkCount: 1, StageExists: true, StageIdentity: storageDirectoryIdentity{10, 11}, NewLength: 1 << 60, Token: storagebudget.Token{Slot: 62, ID: storageIntentTestID(12), Sequence: 13, Generation: 15}, PreviousToken: storagebudget.Token{Slot: 62, ID: storageIntentTestID(12), Sequence: 13, Generation: 14}, Outcome: storageIntentPublished, Evidence: storageIntentTestDigest(16), Target: strings.Repeat("t", 1024), Stage: strings.Repeat("s", 1024), Workflow: strings.Repeat("w", 128), Operation: strings.Repeat("o", 128)}
	for i := 0; i < 16; i++ {
		r.Components = append(r.Components, storagebudget.Component{Filesystem: storageIntentTestID(17), FilesystemGeneration: 18, Root: storageIntentTestID(byte(32 + i)), RootGeneration: 19, Owner: storageIntentTestDigest(20), Workspace: storageIntentTestID(21), WorkspaceGeneration: 22, Category: uint8(i % 9), Physical: storagebudget.Vector{Bytes: 1 << 59, Files: 1 << 39, Directories: 1 << 39}, Attribution: storagebudget.Vector{Bytes: 1 << 59, Files: 1 << 39, Directories: 1 << 39}, SettlementPhysical: storagebudget.Vector{Bytes: 1 << 59, Files: 1 << 39, Directories: 1 << 39}, SettlementAttribution: storagebudget.Vector{Bytes: 1 << 59, Files: 1 << 39, Directories: 1 << 39}})
		last := &r.Components[len(r.Components)-1]
		last.CapacityCount = 4
		last.CapacityBinding = storageIntentTestDigest(84)
		for j := range last.Capacity {
			last.Capacity[j] = storagebudget.CapacityPromise{Constraint: storageIntentTestID(byte(80 + j)), Generation: uint64(85 + j), Peak: storagebudget.CapacityVector{Bytes: 1 << 60, Inodes: 1 << 40}}
		}
		r.RootIdentities = append(r.RootIdentities, storageIntentTestDigest(byte(48+i)))
		r.FilesystemIdentities = append(r.FilesystemIdentities, storageIntentTestDigest(64))
		r.Changes = append(r.Changes, storagebudget.Change{PhysicalAdd: storagebudget.Vector{Bytes: 1 << 60, Files: 1 << 40, Directories: 1 << 40}, PhysicalRemove: storagebudget.Vector{Bytes: 1 << 60, Files: 1 << 40, Directories: 1 << 40}, AttributionAdd: storagebudget.Vector{Bytes: 1 << 60, Files: 1 << 40, Directories: 1 << 40}, AttributionRemove: storagebudget.Vector{Bytes: 1 << 60, Files: 1 << 40, Directories: 1 << 40}})
		r.Locators = append(r.Locators, strings.Repeat("l", 256))
	}
	return r
}
func storageIntentTestPrepared() storageIntentRecord {
	r := storageIntentTestRecord()
	r.Slot = 0
	r.Generation = 1
	r.Phase = storageIntentPrepared
	r.PreviousPageDigest = storagebudget.Digest{}
	r.PolicyRevision = 0
	r.RetirementEpoch = 0
	r.StageExists = false
	r.StageIdentity = storageDirectoryIdentity{}
	r.Token = storagebudget.Token{}
	r.PreviousToken = storagebudget.Token{}
	r.Outcome = storageIntentUnobserved
	r.Evidence = storagebudget.Digest{}
	r.Components = r.Components[:1]
	r.RootIdentities = r.RootIdentities[:1]
	r.FilesystemIdentities = r.FilesystemIdentities[:1]
	r.Changes = []storagebudget.Change{{}}
	r.Locators = r.Locators[:1]
	r.Target = "target.json"
	r.Stage = ".stage"
	r.Workflow = "fixture"
	r.Operation = "operation"
	return r
}
func storageIntentReseal(b []byte) { clear(b[76:108]); d := sha256.Sum256(b); copy(b[76:108], d[:]) }
func TestStorageHostIntentMaximumClosedVector(t *testing.T) {
	r := storageIntentTestRecord()
	installation, binding := storageIntentTestID(70), storageIntentTestDigest(71)
	b, e := storageEncodeIntent(r, installation, binding)
	storageTestMust(t, e)
	if len(b) != 16384 || binary.LittleEndian.Uint32(b[72:76]) != 15663 || !storageAllZero(b[15791:]) || len(b[15791:]) != 593 {
		t.Fatal("maximum layout/capacity changed")
	}
	if b[12] != 7 || b[232] != 1 || b[265] != 1 || b[356] != 1 || b[357] != 0 || b[390] != 16 || binary.LittleEndian.Uint16(b[9351:9353]) != 1024 || binary.LittleEndian.Uint16(b[11663:11665]) != 256 {
		t.Fatal("handwritten member offsets changed")
	}
	got, e := storageDecodeIntent(b, 63, installation, binding)
	storageTestMust(t, e)
	if !reflect.DeepEqual(got, r) {
		t.Fatal("maximum vector member lost")
	}
	if 64*2*len(b) != storageIntentBytes {
		t.Fatal("allocation mismatch")
	}
}
func TestStorageHostIntentRefusesMalformedMembers(t *testing.T) {
	installation, binding := storageIntentTestID(70), storageIntentTestDigest(71)
	valid, e := storageEncodeIntent(storageIntentTestRecord(), installation, binding)
	storageTestMust(t, e)
	// Each row is authored from the closed layout, not obtained by running a
	// serializer mutation recorder. Re-sealing isolates schema validation.
	rows := []struct {
		name   string
		offset int
		value  byte
	}{
		{"magic", 0, 0}, {"version", 8, 3}, {"slot", 10, 64}, {"phase-empty", 12, 0}, {"phase-new", 12, 9}, {"flags", 13, 1}, {"reserved-header", 15, 1}, {"generation", 16, 0}, {"installation", 24, 0}, {"binding", 40, 0}, {"body-length", 72, 0}, {"header-tail", 108, 1}, {"operation-id", 128, 0}, {"previous-page", 144, 0}, {"cas-binding", 176, 0}, {"epoch", 208, 0}, {"policy", 216, 0}, {"retirement-epoch", 224, 0}, {"old-bool", 232, 2}, {"old-links", 257, 2}, {"stage-bool", 265, 2}, {"outcome", 356, 4}, {"rejection", 357, 1}, {"evidence", 358, 0}, {"components-zero", 390, 0}, {"components-over", 390, 17}, {"component-category", 495, 9}, {"capacity-count-zero", 592, 0}, {"capacity-count-over", 592, 5}, {"capacity-id", 593, 0}, {"capacity-generation", 609, 0}, {"capacity-bytes-over", 624, 255}, {"capacity-inodes-over", 632, 255}, {"capacity-order", 633, 79}, {"capacity-binding", 753, 0}, {"component-padding", 785, 1}, {"target-text", 9353, 0}, {"target-length", 9352, 5}, {"stage-text", 10379, '/'}, {"workflow-text", 11405, 0}, {"operation-text", 11535, 0}, {"locator-text", 11665, 0}, {"tail", 15791, 1},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			b := append([]byte(nil), valid...)
			b[row.offset] = row.value
			storageIntentReseal(b)
			if _, e := storageDecodeIntent(b, 63, installation, binding); e == nil {
				t.Fatal("malformed member accepted")
			}
		})
	}
	for _, row := range []struct {
		size  int
		label string
	}{{0, "empty"}, {15791, "encoded-only"}, {16383, "short"}, {16385, "long"}} {
		t.Run("page-size-"+row.label, func(t *testing.T) {
			b := make([]byte, row.size)
			copy(b, valid)
			if _, e := storageDecodeIntent(b, 63, installation, binding); e == nil {
				t.Fatal("page bound accepted")
			}
		})
	}
	b := append([]byte(nil), valid...)
	b[400] ^= 1
	if _, e := storageDecodeIntent(b, 63, installation, binding); e == nil {
		t.Fatal("unsealed component accepted")
	}
}
func TestStorageHostIntentStrictBankSelection(t *testing.T) {
	installation, binding := storageIntentTestID(70), storageIntentTestDigest(71)
	zero := make([]byte, storageIntentPage)
	prepared := storageIntentTestPrepared()
	p, e := storageEncodeIntent(prepared, installation, binding)
	storageTestMust(t, e)
	selected, e := storageSelectIntent(zero, zero, 0, installation, binding)
	storageTestMust(t, e)
	if selected.record.Phase != storageIntentEmpty {
		t.Fatal("not empty")
	}
	selected, e = storageSelectIntent(p, zero, 0, installation, binding)
	storageTestMust(t, e)
	reserved := storageNextIntent(selected, storageIntentReserved)
	reserved.Token = storagebudget.Token{Slot: 3, ID: storageIntentTestID(80), Sequence: 1, Generation: 1}
	reserved.PolicyRevision = 9
	r, e := storageEncodeIntent(reserved, installation, binding)
	storageTestMust(t, e)
	for _, pair := range [][2][]byte{{p, r}, {r, p}, {r, r}} {
		s, e := storageSelectIntent(pair[0], pair[1], 0, installation, binding)
		storageTestMust(t, e)
		if s.record.Generation != 2 {
			t.Fatal("wrong bank")
		}
	}
	torn := append([]byte(nil), r...)
	torn[300] ^= 1
	conflicting := append([]byte(nil), r...)
	conflicting[128] = 99
	storageIntentReseal(conflicting)
	gap := reserved
	gap.Generation = 4
	g, e := storageEncodeIntent(gap, installation, binding)
	storageTestMust(t, e)
	for _, pair := range [][2][]byte{{p, torn}, {torn, p}, {zero, r}, {r, zero}, {r, conflicting}, {p, g}} {
		if _, e := storageSelectIntent(pair[0], pair[1], 0, installation, binding); e == nil {
			t.Fatal("unknown bank fell back to cheaper page")
		}
	}
	changed := reserved
	changed.Target = "different"
	bad, e := storageEncodeIntent(changed, installation, binding)
	storageTestMust(t, e)
	if _, e = storageSelectIntent(p, bad, 0, installation, binding); e == nil {
		t.Fatal("immutable relation changed")
	}
}
func TestStorageHostIntentRetirementProof(t *testing.T) {
	r := storageIntentTestRecord()
	var state storagebudget.HostState
	state.Epoch = r.RetirementEpoch
	s := &state.Slots[r.Token.Slot]
	s.Ticket.Token = r.Token
	s.Ticket.State = storagebudget.Terminal
	if storageIntentRetiredProof(r, state) {
		t.Fatal("current terminal mistaken for durable incorporation")
	}
	s.PersistedApplied = r.Token
	s.PersistedNextSequence = r.Token.Sequence + 1
	if !storageIntentRetiredProof(r, state) {
		t.Fatal("full incorporated tuple refused")
	}
	s.Ticket.Token.ID = storageIntentTestID(99)
	s.Ticket.Token.Sequence++
	if !storageIntentRetiredProof(r, state) {
		t.Fatal("new current ticket erased actual old incorporation")
	}
	s.PersistedApplied.ID = storageIntentTestID(100)
	s.PersistedApplied.Sequence++
	s.PersistedNextSequence++
	if !storageIntentRetiredProof(r, state) {
		t.Fatal("later actual applied tuple refused")
	}
	state.Epoch++
	if storageIntentRetiredProof(r, state) {
		t.Fatal("later tuple crossed epoch without proof")
	}
	s.PersistedApplied = r.Token
	if !storageIntentRetiredProof(r, state) {
		t.Fatal("exact persisted tuple lost across epoch")
	}
	s.PersistedApplied.Generation--
	if storageIntentRetiredProof(r, state) {
		t.Fatal("slot/sequence-only proof accepted")
	}
}
func TestStorageHostIntentDefinitelyRejectedExactError(t *testing.T) {
	for _, e := range []error{storagebudget.ErrLimit, storagebudget.ErrHeadroom, storagebudget.ErrBusy} {
		if storageDefinitelyRejected(e) == storageIntentNoRejection {
			t.Fatal("known refusal not classified")
		}
		if storageDefinitelyRejected(errors.Join(e, storagebudget.ErrUnknown)) != storageIntentNoRejection {
			t.Fatal("joined unknown became definite")
		}
	}
	for _, e := range []error{nil, storagebudget.ErrUnknown, storagebudget.ErrBounds, storagebudget.ErrStale, errors.Join(storagebudget.ErrLimit)} {
		if storageDefinitelyRejected(e) != storageIntentNoRejection {
			t.Fatal("unproven refusal became definite")
		}
	}
}

func TestStorageHostIntentEveryPhaseAndOutcome(t *testing.T) {
	installation, binding := storageIntentTestID(70), storageIntentTestDigest(71)
	r := storageIntentTestPrepared()
	page, e := storageEncodeIntent(r, installation, binding)
	storageTestMust(t, e)
	slot := storageIntentSlot{r, page, 0}
	for _, phase := range []storageIntentPhase{storageIntentReserved, storageIntentStaging, storageIntentPublishing, storageIntentSettling, storageIntentSettled, storageIntentRetired} {
		next := storageNextIntent(slot, phase)
		switch phase {
		case storageIntentReserved:
			next.Token = storagebudget.Token{Slot: 2, ID: storageIntentTestID(3), Sequence: 4, Generation: 5}
			next.PolicyRevision = 6
		case storageIntentStaging:
			next.PreviousToken = next.Token
			next.Token.Generation++
			next.Evidence = next.CASBinding
		case storageIntentPublishing:
			next.StageExists = true
			next.StageIdentity = storageDirectoryIdentity{7, 8}
		case storageIntentSettling:
			next.Outcome = storageIntentPublished
			next.Evidence = storageIntentTestDigest(9)
			next.Changes[0] = storagebudget.Change{PhysicalAdd: storagebudget.Vector{Bytes: 1, Files: 1}, AttributionAdd: storagebudget.Vector{Bytes: 1, Files: 1}}
		case storageIntentSettled:
			next.PreviousToken = next.Token
			next.Token.Generation++
		case storageIntentRetired:
			next.RetirementEpoch = next.AdmissionEpoch
		}
		b, e := storageEncodeIntent(next, installation, binding)
		storageTestMust(t, e)
		selected, e := storageSelectIntent(slot.page, b, 0, installation, binding)
		storageTestMust(t, e)
		if !reflect.DeepEqual(next, selected.record) {
			t.Fatal("phase member lost")
		}
		slot = selected
	}
	for _, kind := range []storageIntentRejection{storageIntentQuota, storageIntentHeadroom, storageIntentBusy} {
		prepared := storageIntentTestPrepared()
		p, e := storageEncodeIntent(prepared, installation, binding)
		storageTestMust(t, e)
		rejected := storageNextIntent(storageIntentSlot{prepared, p, 0}, storageIntentRejected)
		rejected.Outcome = storageIntentNotAdmitted
		rejected.Rejection = kind
		b, e := storageEncodeIntent(rejected, installation, binding)
		storageTestMust(t, e)
		if _, e = storageSelectIntent(p, b, 0, installation, binding); e != nil {
			t.Fatal(e)
		}
	}
	aborted := storageIntentTestRecord()
	aborted.Phase = storageIntentSettling
	aborted.RetirementEpoch = 0
	aborted.Outcome = storageIntentAborted
	aborted.Changes = make([]storagebudget.Change, 16)
	b, e := storageEncodeIntent(aborted, installation, binding)
	storageTestMust(t, e)
	got, e := storageDecodeIntent(b, 63, installation, binding)
	storageTestMust(t, e)
	if got.Outcome != storageIntentAborted {
		t.Fatal("closed no-effect outcome lost")
	}
	previous := slot
	reuse := storageIntentTestPrepared()
	reuse.Generation = previous.record.Generation + 1
	reuse.PreviousPageDigest = sha256.Sum256(previous.page)
	reuse.OperationID = storageIntentTestID(99)
	if !storageIntentTransition(previous.record, reuse, previous.page) {
		t.Fatal("fresh finite slot generation refused")
	}
	reuse.OperationID = previous.record.OperationID
	if storageIntentTransition(previous.record, reuse, previous.page) {
		t.Fatal("operation identity replayed on slot reuse")
	}
	previous.record.Generation = ^uint64(0)
	reuse.Generation = 0
	if storageIntentTransition(previous.record, reuse, previous.page) {
		t.Fatal("generation overflow accepted")
	}
}

// The vectorized scan is the same exact predicate as a byte loop: every byte
// of every length, including block boundaries and an 8 MiB bank tail, counts.
func TestStorageAllZeroChecksEveryByte(t *testing.T) {
	loop := func(b []byte) bool {
		for _, v := range b {
			if v != 0 {
				return false
			}
		}
		return true
	}
	block := len(storageZeroBlock)
	for _, n := range []int{0, 1, 7, 8, 9, 63, 64, 65, block - 1, block, block + 1, 2*block + 3, storageOwnerCatalogBytes} {
		b := make([]byte, n)
		if !storageAllZero(b) || !storageEnrollmentPeakZero(b) || !storageEnrollmentPeakQAllZero(b) {
			t.Fatal("zero buffer refused", n)
		}
		for _, i := range []int{0, n / 2, block - 1, block, n - 1} {
			if i < 0 || i >= n {
				continue
			}
			b[i] = 0x80
			if storageAllZero(b) || storageAllZero(b) != loop(b) || storageEnrollmentPeakZero(b) || storageEnrollmentPeakQAllZero(b) {
				t.Fatal("nonzero byte accepted", n, i)
			}
			b[i] = 0
		}
	}
	if storageZeroBlock != ([len(storageZeroBlock)]byte{}) {
		t.Fatal("the shared zero block was written")
	}
}
