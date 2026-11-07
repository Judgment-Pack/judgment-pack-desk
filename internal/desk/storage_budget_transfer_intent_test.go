package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Syntactic fixtures are deliberately detached. They never install or issue a
// native qualification, construct an original owner or authorize an effect.
func storageTransferVector() storageTransferRecord {
	id := func(v byte) (out storagebudget.ID) { out[0] = v; return }
	digest := func(v byte) (out storagebudget.Digest) { out[0] = v; return }
	native := storageOwnerPersistentObject{Backend: 1, VolumeUUID: [16]byte{1}, Object: 1, Incarnation: 1, IncarnationKind: 1, ObjectKind: 1}
	component := storagebudget.Component{Filesystem: id(3), FilesystemGeneration: 1, Root: id(4), RootGeneration: 1, Owner: digest(2), Category: storagebudget.Control, Physical: storagebudget.Vector{Bytes: 1, Files: 1}, CapacityCount: 1, CapacityBinding: digest(6), Capacity: [4]storagebudget.CapacityPromise{{Constraint: id(5), Generation: 1, Peak: storagebudget.CapacityVector{Bytes: 1, Inodes: 1}}}}
	return storageTransferRecord{Generation: 1, Phase: storageTransferPrepared, Mode: storageTransferExport, Operation: id(1), Installation: id(2), Owner: digest(2), QBinding: digest(2), Epoch: 1, PolicyRevision: 1, HostGeneration: 1, Catalog: digest(3), Changes: make([]storagebudget.Change, 1), Settings: native, Source: native, Parent: native, Basis: native, Requester: native, RequesterWorkspace: id(8), RequesterGeneration: 1, ArchiveBytes: 1, ArchiveDigest: digest(8), CreatedAt: "2026-11-01T00:00:00Z", Groups: []storageTransferGroup{{Component: component, RootIdentity: digest(4), FilesystemIdentity: digest(5), Locator: "original", Native: native, Membership: digest(7)}}}
}

func TestStorageWholeTransferClosedRecordAndUnknownCuts(t *testing.T) {
	r := storageTransferVector()
	raw, err := storageTransferEncode(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1384448 || storageTransferHeader != 204800 || storageTransferBase != 65536 || storageTransferBankBytes*2 != 2768896 {
		t.Fatal("closed extent changed")
	}
	decoded, err := storageTransferDecode(raw)
	if err != nil {
		t.Fatal(err)
	}
	again, err := storageTransferEncode(decoded)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatal("not canonical", err)
	}
	for _, at := range []int{16, 95, 128, 4096, 57344, 61440, 65536, len(raw) - 1} {
		t.Run(string(rune('a'+at%26))+strings.Repeat("x", at%3), func(t *testing.T) {
			bad := append([]byte{}, raw...)
			bad[at] ^= 1
			if _, e := storageTransferDecode(bad); e == nil {
				t.Fatal("changed committed bytes accepted", at)
			}
		})
	}
	// Header checksum alone cannot bless a modified immutable output/body.
	bad := append([]byte{}, raw...)
	bad[storageTransferHeader] ^= 1
	if _, err = storageTransferDecode(bad); err == nil {
		t.Fatal("unobserved body accepted")
	}
	// Neither one intact bank nor a lower generation permits torn-peer fallback.
	if _, err = storageTransferDecodePair([2][]byte{raw, bad}); err == nil {
		t.Fatal("torn peer ignored")
	}
	reserved := decoded
	reserved.Generation++
	reserved.Phase = storageTransferReserved
	reserved.Token = storagebudget.Token{Slot: 0, ID: storagebudget.ID{9}, Sequence: 1, Generation: 1}
	reserved.InitialToken = reserved.Token
	reservedRaw, err := storageTransferEncode(reserved)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := storageTransferDecodePair([2][]byte{raw, reservedRaw})
	if err != nil || pair.Banks[pair.Selected].Token != reserved.Token {
		t.Fatal("original reservation lost", err)
	}
	data := reserved
	data.Generation++
	data.Phase = storageTransferDataIntent
	data.Token.Generation++
	dataRaw, err := storageTransferEncode(data)
	if err != nil {
		t.Fatal(err)
	}
	pair, err = storageTransferDecodePair([2][]byte{reservedRaw, dataRaw})
	if err != nil || pair.Banks[pair.Selected].Phase != storageTransferDataIntent || !reflect.DeepEqual(pair.Banks[pair.Selected].Groups, decoded.Groups) {
		t.Fatal("DataIntent mapping lost", err)
	}
	// All-zero returned rows after DataIntent are unknown effects, not absence.
	if pair.Banks[pair.Selected].ArchiveState != 0 || pair.Banks[pair.Selected].Token != data.Token {
		t.Fatal("unexpected returned facts")
	}
	settled := data
	settled.Generation++
	settled.Phase = storageTransferSettled
	settled.Token.Generation++
	if _, err = storageTransferEncode(settled); err == nil {
		t.Fatal("unobserved archive forgiven")
	}
	// Updating a header's plan hash does not authorize a different old promise.
	changed := append([]byte{}, dataRaw...)
	changed[200] ^= 1
	clear(changed[96:128])
	sum := sha256.Sum256(changed[:4096])
	copy(changed[96:128], sum[:])
	if _, err = storageTransferDecodePair([2][]byte{reservedRaw, changed}); err == nil {
		t.Fatal("different original mapping accepted")
	}
}

func TestStorageWholeTransferReferenceWindowsAndPathBoundaries(t *testing.T) {
	for _, count := range []int{1, 31, 32, 33, 4096, 8191, 8191 + 4096 + 16} {
		next := 0
		for i := 0; i < (count+31)/32; i++ {
			first, last, err := storageTransferParentWindow(count, i)
			if err != nil || first != next || last-first > 32 || last <= first {
				t.Fatal(count, i, first, last, err)
			}
			next = last
		}
		if next != count {
			t.Fatal("window omitted parents", count, next)
		}
		if _, _, err := storageTransferParentWindow(count, (count+31)/32); !errors.Is(err, storagebudget.ErrBounds) {
			t.Fatal("extra observation pass", count, err)
		}
	}
	for _, n := range []int{33, 8191} {
		path := "/" + strings.Repeat("a/", n-1) + "a"
		parts, err := storageTransferPathParts(path)
		if err != nil || len(parts) != n {
			t.Fatal(n, err)
		}
	}
	path := "/" + strings.Repeat("a/", 8191) + "a"
	if _, err := storageTransferPathParts(path); !errors.Is(err, storagebudget.ErrBounds) {
		t.Fatal("unrepresentable chain accepted", err)
	}
	// A supported long lexical path does not use a full-path native syscall.
	long := "/" + strings.Repeat(strings.Repeat("a", 200)+"/", 24) + "last"
	if len(long) <= 4096 {
		t.Fatal("bad handwritten long fixture")
	}
	if parts, err := storageTransferPathParts(long); err != nil || len(parts) != 25 {
		t.Fatal("4096-byte implementation limit returned", err)
	}
	for _, bad := range []string{"relative", "/a/../b", "/a//b", "/a\x00b", strings.Repeat("a", 16385)} {
		if _, err := storageTransferPathParts(bad); err == nil {
			t.Fatal("bad path accepted")
		}
	}
}

func TestStorageWholeTransferControlScheduleIsFinite(t *testing.T) {
	plan := storageTransferControlPlan()
	if len(plan) != 23 {
		t.Fatal(len(plan))
	}
	var writes [2]uint64
	for i, g := range plan {
		if int(g.ID) != i+1 || g.Parent < 1 || g.Parent > 2 || g.Files != 0 || g.Directories != 0 {
			t.Fatal("unexpected new control allocation", g)
		}
		writes[g.Parent-1] += g.Bytes
	}
	if writes != [2]uint64{5677056, 8962048} {
		t.Fatal("complete publication schedule changed", writes)
	}
	peaks, err := storageTransferControlPeaks()
	if err != nil || peaks != [2]storagebudget.CapacityVector{{Bytes: 42598400, Inodes: 34}, {Bytes: 30801920, Inodes: 12}} {
		t.Fatal(peaks, err)
	}
	// Whole-bank extents, header publication and the existing Q lengths remain
	// distinct. These arithmetic vectors do not assert native allocation bounds.
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], uint32(storageTransferBankBytes))
	if binary.LittleEndian.Uint32(word[:]) != 1384448 {
		t.Fatal("bank encoding overflow")
	}
}

func storageTransferHostVector(t *testing.T) storageIntentRecord {
	t.Helper()
	bank := storageTransferVector()
	raw, err := storageTransferEncode(bank)
	if err != nil {
		t.Fatal(err)
	}
	bank, err = storageTransferDecode(raw)
	if err != nil {
		t.Fatal(err)
	}
	r := storageIntentRecord{Slot: 0, Generation: 1, Phase: storageIntentPrepared, OperationID: bank.Operation, CASBinding: bank.Plan, AdmissionEpoch: 1, NewLength: 1, Workflow: storageTransferWorkflow, Operation: storageTransferOperation(bank.Operation, bank.Plan), Components: []storagebudget.Component{bank.Groups[0].Component}, RootIdentities: []storagebudget.Digest{bank.Groups[0].RootIdentity}, FilesystemIdentities: []storagebudget.Digest{bank.Groups[0].FilesystemIdentity}, Changes: make([]storagebudget.Change, 1), Locators: []string{bank.Groups[0].Locator}, Transfer: &storageIntentTransfer{Mode: storageTransferExport, Source: bank.Source, Settings: bank.Settings, Catalog: bank.Catalog, Membership: storagebudget.Digest{11}, Workspace: bank.RequesterWorkspace, WorkspaceGeneration: bank.RequesterGeneration}}
	return r
}

func TestStorageWholeTransferHINT9ClosedProjectionAndOldForms(t *testing.T) {
	r := storageTransferHostVector(t)
	installation := storagebudget.ID{2}
	binding := r.Components[0].Owner
	raw, err := storageEncodeIntent(r, installation, binding)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 16384 || string(raw[:8]) != "DSKHINT9" || r.encodedLength() != 15791 || storageTransferIntentBytes != 2*1026 || storageTransferIntentMeaningfulBytes != 1866 || storageTransferIntentBytes-storageTransferIntentMeaningfulBytes != 186 {
		t.Fatal("closed page/path projection changed")
	}
	got, err := storageDecodeIntent(raw, 0, installation, binding)
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatal("projection lost", err)
	}
	again, err := storageEncodeIntent(got, installation, binding)
	if err != nil || !bytes.Equal(again, raw) {
		t.Fatal("noncanonical HINT9", err)
	}
	// Neither old discriminator can reinterpret these bytes, even after a new
	// header checksum; their original path grammar and zero suffix still apply.
	for _, version := range []byte{2, 8} {
		old := append([]byte{}, raw...)
		copy(old[:8], []byte("DSKHINT0"))
		old[7] = '0' + version
		binary.LittleEndian.PutUint16(old[8:10], uint16(version))
		clear(old[76:108])
		sum := sha256.Sum256(old)
		copy(old[76:108], sum[:])
		if _, err := storageDecodeIntent(old, 0, installation, binding); err == nil {
			t.Fatal("new projection relabeled as old", version)
		}
	}
	// Locate the exact closed projection using the original fixed field board:
	// 263 bytes precede count; sixteen400+32+32+96 rows then2052 path bytes.
	// The writer's first35-byte source tuple follows the one-byte mode.
	prefix := 128 + 16 + 32 + 32 + 8 + 8 + 8 + 1 + 16 + 8 + 8 + 1 + 16 + 8 + 33 + 33 + 1 + 1 + 32 + 1 + 16*(400+32+32+96)
	for _, offset := range []int{prefix + storageTransferIntentMeaningfulBytes, prefix + storageTransferIntentBytes - 1} {
		bad := append([]byte{}, raw...)
		bad[offset] = 1
		clear(bad[76:108])
		sum := sha256.Sum256(bad)
		copy(bad[76:108], sum[:])
		if _, err := storageDecodeIntent(bad, 0, installation, binding); err == nil {
			t.Fatal("reserved projection byte accepted", offset)
		}
	}
	// Pure portable metadata does not install qualification or authorize a route.
}

func TestStorageWholeTransferHINT9TailReconstructsCanonicalFullOperand(t *testing.T) {
	r := storageTransferHostVector(t)
	creation := storageCreationFixture(t, storageCreationHistory)
	r.OperationID = creation.Tail.Bridge
	r.Operation = "5c00000000000000000000000000000000"
	// The handwritten helper's deterministic ID representation is independent
	// of the string above; encode its exact bytes as ordinary operation data.
	r.Operation = storageHexID(r.OperationID)
	r.Transfer.Mode = storageTransferMove
	r.Transfer.Empty = true
	r.Transfer.Tail = creation.Tail
	full := make([]byte, 4096)
	if !storageCreationTailCodec(full, &creation.Tail, false) {
		t.Fatal("tail fixture")
	}
	if !storageAllZero(full[1316:]) {
		t.Fatal("meaningful tail exceeded closed field board")
	}
	projection := make([]byte, storageTransferIntentBytes)
	codec := storageIntentCodec{b: projection}
	if !storageTransferIntentCodec(&codec, r.Transfer, false) || codec.at != 2052 {
		t.Fatal("compact tail encoding")
	}
	var decoded storageIntentTransfer
	codec = storageIntentCodec{b: projection}
	if !storageTransferIntentCodec(&codec, &decoded, true) || !reflect.DeepEqual(decoded.Tail, creation.Tail) {
		t.Fatal("full tail not retained")
	}
	restored := make([]byte, 4096)
	if !storageCreationTailCodec(restored, &decoded.Tail, false) || !bytes.Equal(full, restored) {
		t.Fatal("canonical zero suffix lost")
	}
	// The eight closed capacity rows occupy fixed slots, including zero rows
	// after CapacityCount. Altering one unused row must fail, not disappear when
	// the compact tail is expanded back into its original4096-byte form.
	// Tail starts after411 bytes; requester bytes follow its1316 bytes.
	tailStart := 1 + 35 + 32 + 33 + 32 + 16 + 32 + 32 + 4*35 + 32 + 24 + 1 + 1
	used := len(creation.Tail.Capacity)
	if used < 8 {
		bad := append([]byte{}, projection...)
		bad[tailStart+676+used*80] = 1
		codec = storageIntentCodec{b: bad}
		if storageTransferIntentCodec(&codec, &decoded, true) {
			t.Fatal("noncanonical unused capacity row accepted")
		}
	}
}

func storageTransferProjectedArchiveFixture(t *testing.T, phase storageTransferPhase) (storageTransferRecord, storageOwnerCatalogV2, storageIntentRecord) {
	t.Helper()
	catalog := storageTransferAdmissionCatalog(1)
	catalog.Roots[0].References = []storagebudget.ID{catalog.Workspaces[0].Definition.ID}
	bank := storageTransferVector()
	bank.HostGeneration = 1
	bank.RequesterWorkspace = catalog.Workspaces[0].Definition.ID
	bank.RequesterGeneration = 7
	var err error
	bank.Groups, err = storageTransferBuildGroups(catalog, bank.Source, []storageTransferCharge{{root: catalog.Roots[0].Definition.ID, category: storagebudget.Control, references: storageTransferSourceReferences, physical: storagebudget.Vector{Bytes: 1, Files: 1}, attribution: storagebudget.Vector{Bytes: 1, Files: 1}, capacity: storagebudget.CapacityVector{Bytes: 1, Inodes: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	bank.Catalog, err = storageTransferCatalogDigest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	bank.Phase = phase
	bank.Generation = 8
	bank.InitialToken = storagebudget.Token{Slot: 0, ID: storagebudget.ID{9}, Sequence: 1, Generation: 1}
	bank.Token = bank.InitialToken
	bank.Token.Generation++
	corePhase := storageIntentStaging
	if phase == storageTransferRetired {
		bank.Token.Generation++
		bank.ArchiveState = 3
		bank.ArchiveNative = bank.Source
		bank.ArchiveNative.ObjectKind = 2
		bank.ArchiveNative.Object = 9
		corePhase = storageIntentRetired
	}
	raw, err := storageTransferEncode(bank)
	if err != nil {
		t.Fatal(err)
	}
	bank, err = storageTransferDecode(raw)
	if err != nil {
		t.Fatal(err)
	}
	core, err := storageTransferProjectIntent(bank, catalog, 0, 8, storagebudget.Digest{7}, corePhase)
	if err != nil {
		t.Fatal(err)
	}
	return bank, catalog, core
}

func TestStorageWholeTransferHistoricalAppliedWitnessSurvivesBankReuse(t *testing.T) {
	for _, mode := range []string{"export", "move-with-complete-tail"} {
		t.Run(mode, func(t *testing.T) {
			bank, catalog, core := storageTransferProjectedArchiveFixture(t, storageTransferRetired)
			if mode != "export" {
				creation := storageCreationFixture(t, storageCreationHistory)
				bank.Mode = storageTransferMove
				bank.OldAbsent = true
				bank.ArchiveBytes = 0
				bank.ArchiveDigest = storagebudget.Digest{}
				bank.ArchiveNative = storageOwnerPersistentObject{}
				bank.ArchiveState = 0
				bank.Tail = creation.Tail
				bank.Tail.Installation = bank.Installation
				bank.Tail.QBinding = bank.QBinding
				bank.Tail.Bridge = bank.Operation
				bank.Tail.Config = bank.Settings
				bank.Requester = bank.Tail.Project
				bank.NewPointer = []byte(`{"version":1,"path":"/new-target","previous":"/original"}`)
				bank.ExistingPrefix = uint16(len("/new-target"))
				bank.PointerState = 2
				bank.PointerNative = bank.Source
				bank.PointerNative.ObjectKind = 2
				bank.PointerNative.Object = 13
				bank.TargetBefore = catalog.Objects[1].Persistent
				bank.TargetEmpty = true
				bank.TargetExisting = storagebudget.Vector{Bytes: 1, Files: 1, Directories: 1}
				bank.MarkerBefore = bank.PointerNative
				bank.MarkerBefore.Object = 14
				// The move operands define a new handwritten plan, not the export
				// plan retained by the decoded fixture. Keep the encoder's stale-plan
				// rejection intact and let it bind these complete move operands.
				bank.Plan = storagebudget.Digest{}
				raw, e := storageTransferEncode(bank)
				if e != nil {
					t.Fatal(e)
				}
				bank, e = storageTransferDecode(raw)
				if e != nil {
					t.Fatal(e)
				}
				core, e = storageTransferProjectIntent(bank, catalog, 0, 8, storagebudget.Digest{7}, storageIntentRetired)
				if e != nil {
					t.Fatal(e)
				}
			}
			page, err := storageEncodeIntent(core, catalog.Installation, catalog.QBinding)
			if err != nil {
				t.Fatal(err)
			}
			fanout, err := storageTransferCatalogIntentFanout(core, catalog)
			if err != nil {
				t.Fatal(err)
			}
			ticket := storagebudget.Ticket{Token: core.Token, State: storagebudget.Terminal, PolicyRevision: core.PolicyRevision, Workflow: storageTransferWorkflow, Operation: core.Operation, Owner: catalog.QBinding, Components: append([]storagebudget.Component{}, core.Components...), Changes: append([]storagebudget.Change{}, core.Changes...), Evidence: core.Evidence, Fanout: fanout, ManifestLength: storagebudget.ManifestBytes}
			initial := ticket
			initial.Token = core.Transfer.Initial
			manifest, err := storageEnrollmentPeakQEncodeFanoutManifest(storageEnrollmentPeakQTicketValue(initial), storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(catalog.Installation), Binding: storageEnrollmentPeakQDigest(catalog.QBinding)})
			if err != nil {
				t.Fatal(err)
			}
			ticket.Manifest = sha256.Sum256(manifest)
			row := storagebudget.HostSlotState{Ticket: ticket, PersistedApplied: core.Token, PersistedNextSequence: 2}
			evidence, err := storageTransferSettlementEvidence(bank, catalog)
			if err != nil || evidence != core.Evidence {
				t.Fatal("bank and historical projection disagree on original settlement", err)
			}
			state := storagebudget.HostState{}
			state.Slots[core.Token.Slot] = row
			if !storageTransferApplied(bank, catalog, state) {
				t.Fatal("original bank cannot verify exact compact applied operand")
			}
			if !storageIntentFanoutRetiredOperand(core, catalog, row) {
				t.Fatal("exact original applied request refused")
			}
			// This is a pure original-view/manifest operand test, not a fabricated P or
			// execution report. The later full-bank bytes are deliberately unrelated.
			original, err := storageTransferEncode(bank)
			if err != nil {
				t.Fatal(err)
			}
			newer := storageTransferVector()
			newer.Operation[0] = 44
			newer.Previous = sha256.Sum256(original)
			replacement, err := storageTransferEncode(newer)
			if err != nil {
				t.Fatal(err)
			}
			mixed, err := storageTransferDecodePair([2][]byte{original, replacement})
			if err != nil || !mixed.RecoveryOnly {
				t.Fatal("new Prepared must retain original retired bank", err)
			}
			reused, err := storageTransferDecodePair([2][]byte{replacement, replacement})
			if err != nil || reused.RecoveryOnly || reused.Banks[reused.Selected].Operation != newer.Operation {
				t.Fatal("bad complete bank reuse", err)
			}
			if bytes.Equal(replacement, page) || bank.Operation == newer.Operation {
				t.Fatal("bad reuse fixture")
			}
			if !storageIntentFanoutRetiredOperand(core, catalog, row) {
				t.Fatal("historical proof read mutable transfer bank")
			}
			witnesses := []storageEnrollmentPeakWitness{{Slot: 0, Bank: 0, Version: 9, Generation: core.Generation, Operation: core.OperationID, Digest: sha256.Sum256(page), Selected: true, Phase: byte(core.Phase), Branch: 1, Page: page, Core: core}}
			encoded, err := storageEnrollmentPeakWriteWitnesses(witnesses, catalog.Installation, catalog.QBinding)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := storageEnrollmentPeakReadWitnesses(encoded, 1, catalog.Installation, catalog.QBinding)
			if err != nil || decoded[0].Core.Transfer == nil || !reflect.DeepEqual(decoded[0].Core, core) {
				t.Fatal("historical full projection lost", err)
			}
			if mode != "export" {
				full := make([]byte, storageCreationTailBytes)
				want := make([]byte, storageCreationTailBytes)
				tail := decoded[0].Core.Transfer.Tail
				if !storageCreationTailCodec(full, &tail, false) || !storageCreationTailCodec(want, &bank.Tail, false) || !bytes.Equal(full, want) {
					t.Fatal("bank reuse lost complete original tail")
				}
			}
			bad := row
			bad.PersistedApplied.Generation--
			if storageIntentFanoutRetiredOperand(core, catalog, bad) {
				t.Fatal("digest/terminal text replaced original applied tuple")
			}
			bad = row
			bad.Ticket.Manifest[0] ^= 1
			if storageIntentFanoutRetiredOperand(core, catalog, bad) {
				t.Fatal("wrong original manifest accepted")
			}
		})
	}
}

func TestStorageWholeTransferUncertainProjectionRequiresOriginalBanks(t *testing.T) {
	bank, catalog, core := storageTransferProjectedArchiveFixture(t, storageTransferDataIntent)
	if !storageTransferIntentOriginalBank(core, bank, catalog) {
		t.Fatal("exact original DataIntent not inspectable")
	}
	before := append([]storagebudget.Component{}, core.Components...)
	initial := core.Transfer.Initial
	full, err := storageTransferEncode(bank)
	if err != nil {
		t.Fatal(err)
	}
	torn := append([]byte{}, full...)
	torn[storageTransferHeader] ^= 1
	if _, err = storageTransferDecodePair([2][]byte{full, torn}); err == nil {
		t.Fatal("uncertain original ignored torn peer")
	}
	unknown := bank
	unknown.Operation[0] ^= 1
	if storageTransferIntentOriginalBank(core, unknown, catalog) {
		t.Fatal("different operation adopted")
	}
	// A DataIntent is nonterminal even if the portable observation hash is made
	// nonzero. No original bank, terminal/applied tuple or cleanup is inferred.
	e := storageEnrollmentPeakEvidence{}
	storageEnrollmentPeakPut16(e.Scope[:2], 3)
	e.Scope[2] = 2
	if storageTransferTailBound(core, e) {
		t.Fatal("unretired promise discharged")
	}
	copy := core
	transfer := *core.Transfer
	copy.Transfer = &transfer
	copy.Transfer.Observed = storagebudget.Digest{99}
	if storageTransferIntentValid(copy) == nil || storageTransferTailBound(copy, e) {
		t.Fatal("observation hash treated as completion")
	}
	if core.Transfer.Initial != initial || !reflect.DeepEqual(core.Components, before) {
		t.Fatal("original reservation changed")
	}
}

// This handwritten schedule follows the actual alternate-bank publication,
// rather than manufacturing a pair of adjacent encodings for each assertion.
func TestStorageWholeTransferAlternatingBankSelectedPredecessor(t *testing.T) {
	encode := func(r storageTransferRecord) []byte {
		t.Helper()
		b, e := storageTransferEncode(r)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	preparedRaw := encode(storageTransferVector())
	prepared, err := storageTransferDecode(preparedRaw)
	if err != nil {
		t.Fatal(err)
	}
	pair := [2][]byte{preparedRaw, append([]byte{}, preparedRaw...)}
	reserved := prepared
	reserved.Generation = 2
	reserved.Phase = storageTransferReserved
	reserved.Token = storagebudget.Token{Slot: 0, ID: storagebudget.ID{9}, Sequence: 1, Generation: 1}
	reserved.InitialToken = reserved.Token
	reservedRaw := encode(reserved)
	if err = storageTransferPersistenceBefore(pair, 1, reservedRaw, true); err != nil {
		t.Fatal("first header", err)
	}
	pair[1] = reservedRaw
	data := reserved
	data.Generation = 3
	data.Phase = storageTransferDataIntent
	data.Token.Generation = 2
	dataRaw := encode(data)
	if storageTransferTransition(prepared, data) {
		t.Fatal("fixture did not skip overwritten bank predecessor")
	}
	if err = storageTransferPersistenceBefore(pair, 0, dataRaw, true); err != nil {
		t.Fatal("selected Reserved must admit next DataIntent", err)
	}
	if err = storageTransferPersistenceBefore(pair, 1, dataRaw, true); !errors.Is(err, storagebudget.ErrStale) {
		t.Fatal("selected bank overwrite admitted", err)
	}
	pair[0] = dataRaw
	completion := data
	completion.Generation = 4
	completion.Phase = storageTransferCompletionIntent
	completionRaw := encode(completion)
	if err = storageTransferPersistenceBefore(pair, 1, completionRaw, true); err != nil {
		t.Fatal("completion header", err)
	}
	pair[1] = completionRaw
	observed := completion
	observed.Generation = 5
	observed.Phase = storageTransferObserved
	observed.ArchiveState = 3
	observed.ArchiveNative = observed.Source
	observed.ArchiveNative.ObjectKind = 2
	observed.ArchiveNative.Object = 17
	observedRaw := encode(observed)
	if err = storageTransferPersistenceBefore(pair, 0, observedRaw, false); err != nil {
		t.Fatal("observed result body", err)
	}
	pair[0] = observedRaw
	selected, err := storageTransferDecodePair(pair)
	if err != nil || !selected.RecoveryOnly {
		t.Fatal("result-body cut must remain recovery-only", err)
	}
	if err = storageTransferPersistenceBefore(pair, 1, observedRaw, false); err != nil {
		t.Fatal("exact observed mirror", err)
	}
	settled := observed
	settled.Generation = 6
	settled.Phase = storageTransferSettled
	settled.Token.Generation = 3
	settledRaw := encode(settled)
	if err = storageTransferPersistenceBefore(pair, 1, settledRaw, true); !errors.Is(err, storagebudget.ErrStale) {
		t.Fatal("unmirrored result advanced", err)
	}
	pair[1] = append([]byte{}, observedRaw...)
	if err = storageTransferPersistenceBefore(pair, 1, settledRaw, true); err != nil {
		t.Fatal("settled from mirrored observed", err)
	}
	// A checksum-valid unrelated operation, nonadjacent generation, changed
	// immutable plan and an unknown peer each refuse before native dispatch.
	for name, mutate := range map[string]func([2][]byte) [2][]byte{
		"unknown-peer":   func(p [2][]byte) [2][]byte { p[0] = append([]byte{}, p[0]...); p[0][96] ^= 1; return p },
		"generation-gap": func(p [2][]byte) [2][]byte { r := data; r.Generation = 19; p[0] = encode(r); return p },
		"other-operation": func(p [2][]byte) [2][]byte {
			r := data
			r.Operation[0]++
			r.Plan = storagebudget.Digest{}
			p[0] = encode(r)
			return p
		},
		"different-plan": func(p [2][]byte) [2][]byte {
			r := data
			r.CreatedAt = "2026-11-02T00:00:00Z"
			r.Plan = storagebudget.Digest{}
			p[0] = encode(r)
			return p
		},
	} {
		t.Run(name, func(t *testing.T) {
			before := mutate([2][]byte{dataRaw, completionRaw})
			if e := storageTransferPersistenceBefore(before, 0, observedRaw, false); e == nil {
				t.Fatal("inconsistent original pair admitted")
			}
		})
	}
}

func TestStorageWholeTransferRejectedTerminalAndReuseCuts(t *testing.T) {
	if storageTransferPrepared != 1 || storageTransferReserved != 2 || storageTransferDataIntent != 3 || storageTransferPointerIntent != 4 || storageTransferCompletionIntent != 5 || storageTransferObserved != 6 || storageTransferSettled != 7 || storageTransferRetired != 8 || storageTransferRejected != 9 {
		t.Fatal("existing phase numbers changed")
	}
	encode := func(r storageTransferRecord) []byte {
		t.Helper()
		raw, e := storageTransferEncode(r)
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	preparedRaw := encode(storageTransferVector())
	prepared, err := storageTransferDecode(preparedRaw)
	if err != nil {
		t.Fatal(err)
	}
	rejected := prepared
	rejected.Generation = 2
	rejected.Phase = storageTransferRejected
	rejectedRaw := encode(rejected)
	rejected, err = storageTransferDecode(rejectedRaw)
	if err != nil {
		t.Fatal(err)
	}
	if !storageTransferTransition(prepared, rejected) || rejected.Plan != prepared.Plan || rejected.Body != prepared.Body {
		t.Fatal("definite rejection changed original plan/body")
	}
	pair, err := storageTransferDecodePair([2][]byte{preparedRaw, rejectedRaw})
	if err != nil || pair.RecoveryOnly || pair.Banks[pair.Selected].Phase != storageTransferRejected {
		t.Fatal("rejection terminal lost", err)
	}
	if err = storageTransferPersistenceBefore([2][]byte{preparedRaw, preparedRaw}, 1, rejectedRaw, true); err != nil {
		t.Fatal("rejection must fit one preadmitted header group", err)
	}
	// The decoder recognizes this known reuse cut as comparison data only.
	// Actual issuance additionally requires the original HINT9/no-ticket proof.
	next := storageTransferVector()
	next.Operation[0] = 31
	next.Previous = sha256.Sum256(rejectedRaw)
	nextRaw := encode(next)
	if err = storageTransferPersistenceBefore([2][]byte{preparedRaw, rejectedRaw}, 0, nextRaw, false); err != nil {
		t.Fatal("strict rejected predecessor", err)
	}
	cut, err := storageTransferDecodePair([2][]byte{nextRaw, rejectedRaw})
	if err != nil || !cut.RecoveryOnly || cut.Banks[cut.Selected].Operation != next.Operation {
		t.Fatal("reuse cut lost original rejection", err)
	}
	if err = storageTransferPersistenceBefore([2][]byte{nextRaw, rejectedRaw}, 1, nextRaw, false); err != nil {
		t.Fatal("exact prepared mirror", err)
	}
	// Neither unknown original Q outcomes nor another plan are transformed into
	// rejected metadata. Each malformed returned observation refuses encoding.
	for name, mutate := range map[string]func(*storageTransferRecord){
		"token": func(r *storageTransferRecord) {
			r.Token = storagebudget.Token{ID: storagebudget.ID{9}, Sequence: 1, Generation: 1}
		},
		"initial-token": func(r *storageTransferRecord) {
			r.InitialToken = storagebudget.Token{ID: storagebudget.ID{9}, Sequence: 1, Generation: 1}
		},
		"archive-result":  func(r *storageTransferRecord) { r.ArchiveState = 4 },
		"archive-native":  func(r *storageTransferRecord) { r.ArchiveNative = r.Source; r.ArchiveNative.ObjectKind = 2 },
		"marker-result":   func(r *storageTransferRecord) { r.MarkerState = 4 },
		"pointer-attempt": func(r *storageTransferRecord) { r.PointerState = 1 },
		"settlement-fact": func(r *storageTransferRecord) { r.Changes[0].PhysicalAdd.Bytes = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			r := rejected
			r.Changes = append([]storagebudget.Change{}, r.Changes...)
			mutate(&r)
			if _, e := storageTransferEncode(r); e == nil {
				t.Fatal("rejection forged an effect/token/result")
			}
		})
	}
	reserved := prepared
	reserved.Generation = 2
	reserved.Phase = storageTransferReserved
	reserved.Token = storagebudget.Token{ID: storagebudget.ID{9}, Sequence: 1, Generation: 1}
	reserved.InitialToken = reserved.Token
	reservedRaw := encode(reserved)
	reserved, err = storageTransferDecode(reservedRaw)
	if err != nil {
		t.Fatal(err)
	}
	forged := rejected
	forged.Generation = 3
	if storageTransferTransition(reserved, forged) || storageTransferTransition(rejected, reserved) {
		t.Fatal("rejection reached from or reopened into admitted phase")
	}
	for name, mutate := range map[string]func(*storageTransferRecord){
		"wrong-previous":         func(r *storageTransferRecord) { r.Previous = storagebudget.Digest{9} },
		"different-installation": func(r *storageTransferRecord) { r.Installation[0]++ },
		"different-q": func(r *storageTransferRecord) {
			r.QBinding[0]++
			r.Owner = r.QBinding
			r.Groups[0].Component.Owner = r.Owner
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := next
			r.Groups = append([]storageTransferGroup{}, next.Groups...)
			mutate(&r)
			raw := encode(r)
			if _, e := storageTransferDecodePair([2][]byte{raw, rejectedRaw}); e == nil {
				t.Fatal("unbound reuse cut admitted")
			}
		})
	}
}

func TestStorageWholeTransferCompactCleanupAndRejectedTransitions(t *testing.T) {
	prepared := storageTransferHostVector(t)
	creation := storageCreationFixture(t, storageCreationHistory)
	prepared.Transfer.Mode = storageTransferMove
	prepared.Transfer.Tail = creation.Tail
	prepared.Transfer.Tail.Bridge = prepared.OperationID
	prepared.Transfer.Empty = true
	prepared.Transfer.OldPointerNative = epNative(401, 2)
	prepared.Transfer.OldPointerDigest = storagebudget.Digest{42}
	installation := prepared.Transfer.Tail.Installation
	binding := prepared.Components[0].Owner
	prepared.Transfer.Tail.QBinding = binding
	page, err := storageEncodeIntent(prepared, installation, binding)
	if err != nil {
		t.Fatal(err)
	}
	rejected := prepared
	rt := *prepared.Transfer
	rejected.Transfer = &rt
	rejected.Generation++
	rejected.Phase = storageIntentRejected
	rejected.PreviousPageDigest = sha256.Sum256(page)
	rejected.Outcome = storageIntentNotAdmitted
	rejected.Rejection = storageIntentQuota
	if _, err = storageEncodeIntent(rejected, installation, binding); err != nil || !storageTransferIntentTransition(prepared, rejected, page) {
		t.Fatal("definite rejection lost immutable old pointer facts", err)
	}
	changed := rejected
	ct := *rejected.Transfer
	changed.Transfer = &ct
	ct.OldPointerDigest[1] = 1
	if storageTransferIntentTransition(prepared, changed, page) {
		t.Fatal("rejection changed original raw-pointer commitment")
	}
	publishing := prepared
	pt := *prepared.Transfer
	publishing.Transfer = &pt
	publishing.Generation = 4
	publishing.PreviousPageDigest = storagebudget.Digest{12}
	publishing.Phase = storageIntentPublishing
	publishing.PolicyRevision = 1
	pt.Initial = storagebudget.Token{ID: storagebudget.ID{9}, Sequence: 1, Generation: 1}
	publishing.PreviousToken = pt.Initial
	publishing.Token = pt.Initial
	publishing.Token.Generation++
	page, err = storageEncodeIntent(publishing, installation, binding)
	if err != nil {
		t.Fatal(err)
	}
	settling := publishing
	st := *publishing.Transfer
	settling.Transfer = &st
	settling.Generation++
	settling.PreviousPageDigest = sha256.Sum256(page)
	settling.Phase = storageIntentSettling
	settling.Outcome = storageIntentAborted
	st.Observed = storagebudget.Digest{71}
	st.Cleanup = storageTransferCleanup{Stage: 4, Marker: 4, Closure: 7, Effects: storagebudget.Digest{72}}
	st.Selection = storageTransferIntentSelection(settling)
	settling.Evidence = storageTransferIntentEvidence(settling)
	if _, err = storageEncodeIntent(settling, installation, binding); err != nil || !storageTransferIntentTransition(publishing, settling, page) {
		t.Fatal("complete observed cleanup cannot advance publishing", err)
	}
	for _, mutate := range []func(*storageIntentTransfer){func(v *storageIntentTransfer) { v.Cleanup.Outputs = 1 }, func(v *storageIntentTransfer) { v.Cleanup.Closure = 3 }, func(v *storageIntentTransfer) { v.Cleanup.Stage = 1 }, func(v *storageIntentTransfer) { v.Cleanup.Effects = storagebudget.Digest{} }, func(v *storageIntentTransfer) { v.OldPointerNative = storageOwnerPersistentObject{} }, func(v *storageIntentTransfer) { v.OldAbsent = true }} {
		bad := settling
		bt := *settling.Transfer
		bad.Transfer = &bt
		mutate(&bt)
		bt.Selection = storageTransferIntentSelection(bad)
		bad.Evidence = storageTransferIntentEvidence(bad)
		if _, err = storageEncodeIntent(bad, installation, binding); err == nil {
			t.Fatal("incomplete/forged cleanup operands accepted")
		}
	}
}

func TestStorageWholeTransferFinitePairReadBoard(t *testing.T) {
	// Initial pair; both original peers before all4 full+6 header writes;
	// separate readback for each write. Q/host retain their own finite reads.
	const want = uint64(26*1384448 + 6*4096)
	if storageTransferBankReadBound() != want {
		t.Fatal("pair observations omitted from control read bound")
	}
}

func TestStorageWholeTransferRejectedIssuerRequiresOriginalNoTicketProof(t *testing.T) {
	// Forged comparison graphs exercise the issuer boundary's refusal before any
	// native/Q call. These tests cannot create a successful original invocation.
	catalog := storageTransferAdmissionCatalog(1)
	catalog.Roots[0].References = []storagebudget.ID{catalog.Workspaces[0].Definition.ID}
	bank := storageTransferVector()
	bank.RequesterWorkspace = catalog.Workspaces[0].Definition.ID
	bank.RequesterGeneration = 7
	var err error
	bank.Groups, err = storageTransferBuildGroups(catalog, bank.Source, []storageTransferCharge{{root: catalog.Roots[0].Definition.ID, category: storagebudget.Control, references: storageTransferSourceReferences, physical: storagebudget.Vector{Bytes: 1, Files: 1}, attribution: storagebudget.Vector{Bytes: 1, Files: 1}, capacity: storagebudget.CapacityVector{Bytes: 1, Inodes: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	bank.Catalog, err = storageTransferCatalogDigest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := storageTransferEncode(bank)
	if err != nil {
		t.Fatal(err)
	}
	bank, err = storageTransferDecode(raw)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := storageTransferProjectIntent(bank, catalog, 0, 1, storagebudget.Digest{}, storageIntentPrepared)
	if err != nil {
		t.Fatal(err)
	}
	page, err := storageEncodeIntent(prepared, catalog.Installation, catalog.QBinding)
	if err != nil {
		t.Fatal(err)
	}
	rejected := bank
	rejected.Generation++
	rejected.Phase = storageTransferRejected
	rejectedRaw, err := storageTransferEncode(rejected)
	if err != nil {
		t.Fatal(err)
	}
	rejected, err = storageTransferDecode(rejectedRaw)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := storageTransferRejectedProjection(rejected, catalog, 0, 2, sha256.Sum256(page), storageIntentBusy)
	if err != nil {
		t.Fatal(err)
	}
	for _, which := range []string{"missing-return", "missing-rejection", "missing-proof", "mismatched-epoch", "maintenance", "mismatched-component-ticket"} {
		t.Run(which, func(t *testing.T) {
			owner := &storageBudgetOwner{}
			registry := &chatTransferLifetimes{owner: owner}
			p := &chatTransferPreparation{registry: registry}
			p.self = p
			epoch := &storageEnrollmentEpoch{transfer: p, original: &storageEnrollmentPeakOriginalComparison{}}
			epoch.self = epoch
			p.epoch = epoch
			status := storagebudget.HostState{Epoch: bank.Epoch}
			op := &chatTransferOperation{preparation: p, catalog: catalog, record: rejected, fullBanks: [2][]byte{raw, rejectedRaw}, host: storageIntentSlot{record: prepared, page: page}, controlOriginal: epoch.original, reserveReturned: true, reserveError: storagebudget.ErrBusy, rejectedProof: &status}
			op.self = op
			p.operation = op
			switch which {
			case "missing-return":
				op.reserveReturned = false
			case "missing-rejection":
				op.reserveError = nil
			case "missing-proof":
				op.rejectedProof = nil
			case "mismatched-epoch":
				status.Epoch++
			case "maintenance":
				status.Maintenance = true
			case "mismatched-component-ticket":
				status.Slots[0].Ticket = storagebudget.Ticket{State: storagebudget.Reserved, Workflow: storageTransferWorkflow, Operation: projection.Operation, Owner: bank.QBinding}
			}
			if err := owner.requireOriginalTransferProjection(t.Context(), op, projection); !errors.Is(err, storagebudget.ErrAuthority) {
				t.Fatal("forged rejection publication reached native currentness", err)
			}
		})
	}
}

func TestStorageWholeTransferPreRenameResidueCannotSettleZero(t *testing.T) {
	for _, fault := range []string{"pointer-stage-write", "pointer-stage-sync", "pointer-stage-close", "pointer-stage-readback", "restore-stage-write", "export-stage-close"} {
		t.Run(fault, func(t *testing.T) {
			record := storageTransferRecord{Mode: storageTransferMove, PointerState: 0, Cleanup: storageTransferCleanup{Stage: 4, Marker: 4, Closure: 7, Effects: storagebudget.Digest{1}}}
			board := []storageTransferOrdinal{{kind: storageTransferPointerOrdinal}}
			results := []byte{1}
			if strings.HasPrefix(fault, "restore") {
				record.Mode = storageTransferRestore
				record.InputBytes = 1
				record.ArchiveState = 1
				board = []storageTransferOrdinal{{kind: storageTransferStageOrdinal}, {kind: storageTransferPointerOrdinal}}
				results = []byte{1, 4}
			}
			if strings.HasPrefix(fault, "export") {
				record.Mode = storageTransferExport
				record.ArchiveState = 1
				board = []storageTransferOrdinal{{kind: storageTransferStageOrdinal}, {kind: storageTransferServingOrdinal}}
				results = []byte{1, 4}
			}
			op := &chatTransferOperation{returned: record, board: board, results: results}
			op.self = op
			if changes, err := op.originalTransferChanges(); err == nil {
				t.Fatal("entered unknown stage residue settled zero", changes)
			}
		})
	}
	// A summary, even complete and census-bound, cannot forgive an entered
	// pointer ordinal. Only its explicit original not-entered receipt permits
	// the zero-pointer abort arm; no decoded label supplies that receipt.
	op := &chatTransferOperation{returned: storageTransferRecord{Mode: storageTransferMove, Cleanup: storageTransferCleanup{Stage: 4, Marker: 4, Closure: 7, Effects: storagebudget.Digest{1}}}, board: []storageTransferOrdinal{{kind: storageTransferPointerOrdinal}}, results: []byte{2}}
	op.self = op
	if _, err := op.originalTransferChanges(); err == nil {
		t.Fatal("entered pointer without cleanup proof forgiven")
	}
	op.results[0] = 4
	if changes, err := op.originalTransferChanges(); err != nil || len(changes) != 0 {
		t.Fatal("exact no-entry abort refused", err)
	}
}

func storageTransferOwningAbortVector(t *testing.T, kind byte) (storageTransferRecord, storageOwnerCatalogV2, storageEnrollmentPeakSource, storageIntentRecord, storagebudget.HostState) {
	t.Helper()
	catalog := storageTransferAdmissionCatalog(1)
	for i := range catalog.Roots {
		catalog.Roots[i].References = []storagebudget.ID{catalog.Workspaces[0].Definition.ID}
		catalog.Roots[i].Role = 4
		catalog.Roots[i].Source = storageOwnerRootSource{Kind: 4, Path: []byte([]string{"/original", "/target"}[i]), ControlIndex: storageOwnerNoControl}
	}
	catalog.Roots[1].Source.Kind = kind
	bank := storageTransferVector()
	bank.Mode = storageTransferMove
	bank.ArchiveBytes = 0
	bank.ArchiveDigest = storagebudget.Digest{}
	bank.CreatedAt = ""
	bank.ArchiveState = 0
	bank.RequesterWorkspace = catalog.Workspaces[0].Definition.ID
	bank.RequesterGeneration = 7
	bank.TargetBefore = catalog.Objects[1].Persistent
	bank.TargetExisting = storagebudget.Vector{Bytes: 1, Files: 1, Directories: 1}
	bank.TargetEmpty = true
	bank.MarkerBefore = bank.Source
	bank.MarkerBefore.Object = 99
	bank.MarkerBefore.ObjectKind = 2
	bank.NewPointer = []byte(`{"version":1,"path":"/target","previous":"/original"}`)
	bank.ExistingPrefix = uint16(len("/target"))
	bank.OldAbsent = true
	creation := storageCreationFixture(t, storageCreationHistory)
	bank.Tail = creation.Tail
	bank.Tail.Installation = bank.Installation
	bank.Tail.QBinding = bank.QBinding
	bank.Tail.Bridge = bank.Operation
	bank.Tail.Config = bank.Settings
	bank.Requester = bank.Tail.Project
	bank.Phase = storageTransferRetired
	bank.Generation = 8
	bank.InitialToken = storagebudget.Token{Slot: 0, ID: storagebudget.ID{9}, Sequence: 1, Generation: 1}
	bank.Token = bank.InitialToken
	bank.Token.Generation += 2
	bank.Cleanup = storageTransferCleanup{Stage: 4, Marker: 4, Closure: 7, Effects: storagebudget.Digest{71}}
	var err error
	bank.Groups, err = storageTransferBuildGroups(catalog, bank.Source, []storageTransferCharge{{root: catalog.Roots[1].Definition.ID, category: storagebudget.History, references: storageTransferSourceReferences, physical: storagebudget.Vector{Bytes: 1, Files: 1}, attribution: storagebudget.Vector{Bytes: 1, Files: 1}, capacity: storagebudget.CapacityVector{Bytes: 65536, Inodes: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	bank.Changes = make([]storagebudget.Change, len(bank.Groups))
	bank.Catalog, err = storageTransferCatalogDigest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := storageTransferEncode(bank)
	if err != nil {
		t.Fatal(err)
	}
	bank, err = storageTransferDecode(raw)
	if err != nil {
		t.Fatal(err)
	}
	core, err := storageTransferProjectIntent(bank, catalog, 0, 8, storagebudget.Digest{7}, storageIntentRetired)
	if err != nil {
		t.Fatal(err)
	}
	fanout, err := storageTransferCatalogIntentFanout(core, catalog)
	if err != nil {
		t.Fatal(err)
	}
	ticket := storagebudget.Ticket{Token: core.Token, State: storagebudget.Terminal, PolicyRevision: core.PolicyRevision, Workflow: storageTransferWorkflow, Operation: core.Operation, Owner: catalog.QBinding, Components: append([]storagebudget.Component{}, core.Components...), Changes: append([]storagebudget.Change{}, core.Changes...), Evidence: core.Evidence, Fanout: fanout, ManifestLength: storagebudget.ManifestBytes}
	initial := ticket
	initial.Token = core.Transfer.Initial
	manifest, err := storageEnrollmentPeakQEncodeFanoutManifest(storageEnrollmentPeakQTicketValue(initial), storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(catalog.Installation), Binding: storageEnrollmentPeakQDigest(catalog.QBinding)})
	if err != nil {
		t.Fatal(err)
	}
	ticket.Manifest = sha256.Sum256(manifest)
	state := storagebudget.HostState{Epoch: bank.Epoch}
	state.Slots[0] = storagebudget.HostSlotState{Ticket: ticket, PersistedApplied: core.Token, PersistedNextSequence: 2}
	source := storageEnrollmentPeakSource{Project: bank.Tail.Project, Default: &storageEnrollmentDefaultSource{Workflow: 6, Settings: bank.Settings, SourceNative: bank.Source, Selected: bank.TargetBefore}}
	return bank, catalog, source, core, state
}

func TestStorageTransferOwningKind4AndKind11AbortAppliedProof(t *testing.T) {
	for _, kind := range []byte{4, 11} {
		t.Run(string(rune('0'+kind)), func(t *testing.T) {
			_, catalog, source, core, state := storageTransferOwningAbortVector(t, kind)
			row := catalog.Roots[1]
			if !storageTransferOwningAbortCore(core, source, catalog, row, state) {
				t.Fatal("exact owning abort/applied cleanup refused", kind)
			}
			for _, cut := range []string{"summary-before-failed-settle", "applied-without-retired", "missing-applied", "pointer-change", "unknown-close", "wrong-source", "wrong-settings", "root-generation", "forged-before"} {
				bad := core
				transfer := *core.Transfer
				bad.Transfer = &transfer
				view := state
				scope := source
				d := *source.Default
				scope.Default = &d
				before := ownerCatalogClone(catalog)
				root := row
				switch cut {
				case "summary-before-failed-settle":
					view.Slots[0].Ticket.State = storagebudget.Staging
					view.Slots[0].PersistedApplied = storagebudget.Token{}
				case "applied-without-retired":
					bad.Phase = storageIntentSettled
				case "missing-applied":
					view.Slots[0].PersistedApplied = storagebudget.Token{}
				case "pointer-change":
					transfer.PointerState = 2
				case "unknown-close":
					transfer.Cleanup.Closure = 3
				case "wrong-source":
					d.SourceNative.Object++
				case "wrong-settings":
					d.Settings.Object++
				case "root-generation":
					root.Definition.Generation++
				case "forged-before":
					before.Roots[0].Source.Path = []byte("/forged")
				}
				if storageTransferOwningAbortCore(bad, scope, before, root, view) {
					t.Fatal("abort proof accepted missing exact chain", kind, cut)
				}
			}
		})
	}
}

func TestStorageTransferRejectionDependencyUsesOriginalCatalogAndSource(t *testing.T) {
	for _, kind := range []byte{4, 11} {
		_, catalog, _, core, _ := storageTransferOwningAbortVector(t, kind)
		core.Phase = storageIntentRejected
		core.Outcome = storageIntentNotAdmitted
		core.Token = storagebudget.Token{}
		core.Transfer.Initial = storagebudget.Token{}
		core.Transfer.Cleanup = storageTransferCleanup{}
		dependency, err := storageTransferNamespaceDependency(core, catalog)
		if err != nil || dependency != nil {
			t.Fatal("unchanged owning rejection acquired a namespace P dependency", kind, err)
		}
		if _, err = storageTransferNamespaceDependency(core, storageOwnerCatalogV2{}); err == nil {
			t.Fatal("missing original catalog treated as no dependency")
		}
	}
	row := storageTransferNamespaceVector(t, 22, "target", false, storagebudget.ID{})
	source, err := storageEnrollmentPeakDecodeSource(row.Source.Relative)
	if err != nil {
		t.Fatal(err)
	}
	catalog := storageTransferAdmissionCatalog(1)
	row.Definition = catalog.Roots[1].Definition
	row.DirectoryObject = 1
	catalog.Objects[1].Persistent = source.Default.Selected
	catalog.Roots[1] = row
	core := storageIntentRecord{OperationID: storagebudget.ID{1}, Phase: storageIntentRejected, Outcome: storageIntentNotAdmitted, Transfer: &storageIntentTransfer{Mode: storageTransferMove, Settings: source.Default.Settings, Source: source.Default.SourceNative, Empty: true}, Components: []storagebudget.Component{{Root: row.Definition.ID, RootGeneration: row.Definition.Generation, Filesystem: row.Definition.Filesystem, Owner: row.Definition.Owner}}, RootIdentities: []storagebudget.Digest{row.Definition.Identity}}
	core.Transfer.Catalog, err = storageTransferCatalogDigest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	dependency, err := storageTransferNamespaceDependency(core, catalog)
	if err != nil || dependency == nil {
		t.Fatal("dependent namespace lost its original source relation", err)
	}
	core.Transfer.Source.Object++
	if _, err = storageTransferNamespaceDependency(core, catalog); err == nil {
		t.Fatal("retained rejection accepted different original source")
	}
}

func TestStorageTransferRetainedViewFullAndCompactFanoutAgree(t *testing.T) {
	catalog, _, _ := storageTransferRetainedViewsFixture(t, []storagebudget.ID{})
	bank := storageTransferVector()
	bank.Installation, bank.Owner, bank.QBinding = catalog.Installation, catalog.QBinding, catalog.QBinding
	bank.Settings, bank.Parent, bank.Basis = catalog.Objects[0].Persistent, catalog.Objects[0].Persistent, catalog.Objects[0].Persistent
	bank.Source = catalog.Objects[catalog.Roots[3].DirectoryObject].Persistent
	bank.Requester = epNative(100, 1)
	bank.RequesterWorkspace, bank.RequesterGeneration = storagebudget.ID{1}, 1
	var err error
	bank.Groups, err = storageTransferBuildGroups(catalog, bank.Source, []storageTransferCharge{{root: catalog.Roots[3].Definition.ID, category: storagebudget.Control, references: storageTransferSourceReferences, physical: storagebudget.Vector{Bytes: 1, Files: 1}, attribution: storagebudget.Vector{Bytes: 1, Files: 1}, capacity: storagebudget.CapacityVector{Bytes: 1, Inodes: 1}}})
	if err != nil || len(bank.Groups) != 1 {
		t.Fatal("one owning source group", err)
	}
	// A is both the component root and the original source. The group builder
	// chooses the first equivalent selector, RootReferences, for their equal A.
	if bank.Groups[0].Flags != storageTransferRootReferences {
		t.Fatal("equal root/source cohort did not use the canonical root selector")
	}
	bank.Changes = make([]storagebudget.Change, 1)
	bank.Catalog, err = storageTransferCatalogDigest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	// Independent original membership material: A={workspace1}, while the
	// retired source and target views each retain their own {workspace1,2}.
	material := epBytes{}
	material.fixed(bank.Catalog)
	material.fixed(catalog.Roots[3].Definition.ID)
	material.fixed(byte(storagebudget.Control))
	material.fixed(uint64(1))
	material.fixed(storagebudget.ID{1})
	material.fixed(uint64(1))
	if bank.Groups[0].Membership != epHash("desk-storage-whole-transfer-reference-set-v1", material.Bytes()) {
		t.Fatal("source group included a namespace cohort")
	}
	bank.Phase, bank.Generation = storageTransferRetired, 8
	bank.InitialToken = storagebudget.Token{Slot: 0, ID: storagebudget.ID{9}, Sequence: 1, Generation: 1}
	bank.Token = bank.InitialToken
	bank.Token.Generation = 3
	bank.ArchiveState = 3
	bank.ArchiveNative = epNative(220, 2)
	raw, err := storageTransferEncode(bank)
	if err != nil {
		t.Fatal(err)
	}
	bank, err = storageTransferDecode(raw)
	if err != nil {
		t.Fatal(err)
	}
	core, err := storageTransferProjectIntent(bank, catalog, 0, 8, storagebudget.Digest{7}, storageIntentRetired)
	if err != nil {
		t.Fatal("full-record projection", err)
	}
	want := &storagebudget.WorkspaceFanout{Workspaces: []storagebudget.WorkspaceReference{{ID: storagebudget.ID{1}, Generation: 1}}, Memberships: []storagebudget.WorkspaceMembership{{1}}}
	full, err := storageTransferReferences(bank, catalog)
	if err != nil || !reflect.DeepEqual(full, want) {
		t.Fatal("full fanout differs from handwritten A", err)
	}
	compact, err := storageTransferCatalogIntentFanout(core, catalog)
	if err != nil || !reflect.DeepEqual(compact, want) {
		t.Fatal("compact fanout differs from handwritten A", err)
	}
	membership := epHash("desk-storage-whole-transfer-original-reference-map-v1", epJSON(struct {
		Catalog storagebudget.Digest
		Fanout  storagebudget.WorkspaceFanout
	}{bank.Catalog, *want}))
	if core.Transfer.Membership != membership {
		t.Fatal("compact exact original membership digest changed")
	}
	ticket := storagebudget.Ticket{Token: core.Token, State: storagebudget.Terminal, PolicyRevision: core.PolicyRevision, Workflow: storageTransferWorkflow, Operation: core.Operation, Owner: catalog.QBinding, Components: append([]storagebudget.Component{}, core.Components...), Changes: append([]storagebudget.Change{}, core.Changes...), Evidence: core.Evidence, Fanout: want, ManifestLength: storagebudget.ManifestBytes}
	initial := ticket
	initial.Token = core.Transfer.Initial
	manifest, err := storageEnrollmentPeakQEncodeFanoutManifest(storageEnrollmentPeakQTicketValue(initial), storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(catalog.Installation), Binding: storageEnrollmentPeakQDigest(catalog.QBinding)})
	if err != nil {
		t.Fatal(err)
	}
	ticket.Manifest = sha256.Sum256(manifest)
	state := storagebudget.HostState{Epoch: bank.Epoch}
	state.Slots[0] = storagebudget.HostSlotState{Ticket: ticket, PersistedApplied: core.Token, PersistedNextSequence: 2}
	if !storageTransferApplied(bank, catalog, state) || !storageIntentFanoutRetiredOperand(core, catalog, state.Slots[0]) || !storageTransferIntentOriginalBank(core, bank, catalog) {
		t.Fatal("exact applied/retirement comparison refused beside retired views")
	}
	// The source selector has the same original A, but depends on source
	// membership. Keep this independent twin for the changed-cohort refusal.
	sourceCore := core
	sourceTransfer := *core.Transfer
	sourceCore.Transfer = &sourceTransfer
	sourceTransfer.Selectors[0] = storageTransferSourceReferences
	sourceTransfer.Selection = storageTransferIntentSelection(sourceCore)
	sourceCore.Evidence = storageTransferIntentEvidence(sourceCore)
	if fanout, err := storageTransferCatalogIntentFanout(sourceCore, catalog); err != nil || !reflect.DeepEqual(fanout, want) {
		t.Fatal("explicit original source selector lost A", err)
	}
	for _, cut := range []string{"duplicate-owner", "unknown-source", "foreign-source", "changed-source-cohort", "membership-digest", "original-refs"} {
		badCatalog := ownerCatalogClone(catalog)
		bad := core
		if cut == "changed-source-cohort" {
			bad = sourceCore
		}
		transfer := *bad.Transfer
		bad.Transfer = &transfer
		switch cut {
		case "duplicate-owner":
			duplicate := badCatalog.Roots[3]
			duplicate.Definition.ID = storagebudget.ID{93}
			badCatalog.Roots = append(badCatalog.Roots, duplicate)
			transfer.Catalog, _ = storageTransferCatalogDigest(badCatalog)
		case "unknown-source":
			transfer.Source = epNative(999, 1)
		case "foreign-source", "changed-source-cohort":
			// The provider is another owning root with B={workspace2}. A root
			// selector still reconstructs A; a source selector must detect B.
			transfer.Source = badCatalog.Objects[badCatalog.Roots[4].DirectoryObject].Persistent
			if transfer.Source == core.Transfer.Source || !reflect.DeepEqual(badCatalog.Roots[4].References, []storagebudget.ID{{2}}) {
				t.Fatal("foreign source fixture lost its distinct owning cohort")
			}
		case "membership-digest":
			transfer.Membership[0] ^= 1
		case "original-refs":
			badCatalog.Roots[3].References = []storagebudget.ID{{2}}
			transfer.Catalog, _ = storageTransferCatalogDigest(badCatalog)
		}
		// Rebind detached checksums, retaining the original bank and applied Q
		// operands. Recomputed hashes cannot establish original source identity.
		transfer.Selection = storageTransferIntentSelection(bad)
		bad.Evidence = storageTransferIntentEvidence(bad)
		fanout, err := storageTransferCatalogIntentFanout(bad, badCatalog)
		if cut == "foreign-source" {
			// Fanout reconstruction checks the selected reference sets, not source
			// identity: RootReferences remains A even with a different source.
			if err != nil || !reflect.DeepEqual(fanout, want) {
				t.Fatal("root selector changed with foreign source cohort", err)
			}
			if bad.Evidence == core.Evidence || storageIntentFanoutAppliedOperand(bad, catalog.Installation, catalog.QBinding, state.Slots[0], fanout) {
				t.Fatal("foreign source accepted against original applied Q evidence")
			}
		} else if err == nil {
			t.Fatal("changed compact membership accepted", cut)
		}
		if storageTransferIntentOriginalBank(bad, bank, badCatalog) || storageIntentFanoutRetiredOperand(bad, badCatalog, state.Slots[0]) {
			t.Fatal("changed source/membership accepted by original bank or retirement proof", cut)
		}
	}
}
