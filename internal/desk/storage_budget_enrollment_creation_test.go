package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// These are handwritten codec operands. They are never installed native
// qualification records, actual creation receipts or execution evidence.
func storageCreationFixture(t *testing.T, kind byte) storageCreationIntent {
	t.Helper()
	before := [8]uint64{0, 0, 0, 0, 8388608, 8388608, 4194304, 4194304}
	peaks, logical, err := storageCreationTailDimensions(false, before)
	storageTestMust(t, err)
	tail := storageCreationTail{Installation: storageIdentityIntentID(90), QBinding: storageIdentityIntentHash(91), Bridge: storageIdentityIntentID(92), Config: storageIdentityIntentPO(93, 1), Anchor: storageIdentityIntentPO(94, 1), Project: storageIdentityIntentPO(95, 1), Qualification: storageIdentityIntentHash(96), Before: before, MaximumExtent: storageEnrollmentPeakEvidenceMax, ParentPeaks: peaks, Logical: logical, Catalogs: [2]storagebudget.Digest{storageIdentityIntentHash(97), storageIdentityIntentHash(98)}}
	for i := 4; i < 8; i++ {
		tail.Targets[i] = storageIdentityIntentPO(uint64(100+i), 2)
	}
	var cap [80]byte
	cap[0] = 8
	storageEnrollmentPeakPut64(cap[16:24], 1)
	cap[24] = 3
	storageEnrollmentPeakPut64(cap[32:40], peaks[0].Bytes+peaks[1].Bytes)
	storageEnrollmentPeakPut64(cap[40:48], peaks[0].Inodes+peaks[1].Inodes)
	cap[48] = 5
	cap[64] = 5
	tail.Capacity = [][80]byte{cap}
	core := storageIdentityIntentFixture().Core
	core.Workflow = "desk-monotone-private-creation-v1"
	core.Target = ".desk-private"
	core.Stage = core.Target
	core.NewLength = 2
	core.Components[0].Category = storagebudget.Control
	core.Components[0].Physical = storagebudget.Vector{Bytes: 2, Files: 2, Directories: 2}
	r := storageCreationIntent{Core: core, Kind: kind, Branch: storageCreationContained, SelectedPath: "/project/.desk-private/dialect-drafts", Parents: []storageCreationParent{{Native: tail.Project, Role: 1}}, Effects: []storageCreationEffect{{Parent: 0, Kind: 1, Name: ".desk-private"}, {Parent: 1, Kind: 1, Name: "dialect-drafts"}, {Parent: 2, Kind: 2, Name: ".gitignore", Bytes: 2, Payload: sha256.Sum256([]byte("*\n"))}, {Parent: 2, Kind: 2, Name: "workflow.lock", Payload: sha256.Sum256(nil)}}, Tail: tail}
	if kind == storageCreationHistory {
		r.Branch = storageCreationConfigured
		r.SelectedPath = "/config/data"
		r.Core.Target = "data"
		r.Core.Stage = "data"
		r.Core.NewLength = 300
		r.Core.Components[0].Category = storagebudget.History
		r.Core.Components[0].Physical = storagebudget.Vector{Bytes: 300, Files: 2, Directories: 1}
		second := r.Core.Components[0]
		second.Root = storageIdentityIntentID(40)
		second.Category = storagebudget.Control
		r.Core.Components = append(r.Core.Components, second)
		r.Core.RootIdentities = append(r.Core.RootIdentities, storageIdentityIntentHash(41))
		r.Core.FilesystemIdentities = append(r.Core.FilesystemIdentities, storageIdentityIntentHash(42))
		r.Core.Locators = append(r.Core.Locators, "settings")
		r.Core.Changes = append(r.Core.Changes, storagebudget.Change{})
		r.Parents = []storageCreationParent{{Native: storageIdentityIntentPO(200, 1), Role: 4}, {Native: storageIdentityIntentPO(201, 1), Role: 5, Component: 1}}
		r.Effects = []storageCreationEffect{{Parent: 0, Kind: 1, Name: "data"}, {Parent: 2, Kind: 2, Name: dataMarkerName, Bytes: 100, Payload: storageIdentityIntentHash(43)}, {Parent: 1, Kind: 2, Name: dataLocationName, Bytes: 200, Payload: storageIdentityIntentHash(44)}}
	}
	if kind == storageCreationAncestor {
		r.Branch = storageCreationPersonal
		r.SelectedPath = "/personal/data/jpack-desk/history"
		r.Core.Target = "jpack-desk"
		r.Core.Stage = r.Core.Target
		r.Core.NewLength = 0
		r.Core.Components[0].Physical = storagebudget.Vector{Directories: 1}
		r.Parents[0].Role = 7
		r.Effects = []storageCreationEffect{{Parent: 0, Kind: 1, Name: "jpack-desk"}}
	}
	r.Core.CASBinding = r.planDigest()
	r.Core.Operation = "p:" + hex.EncodeToString(r.Core.CASBinding[:]) + ":create"
	return r
}

func TestStorageCreationClosedCodecAndLegacyRefusal(t *testing.T) {
	for _, kind := range []byte{storageCreationPrivate, storageCreationHistory, storageCreationAncestor} {
		r := storageCreationFixture(t, kind)
		storageTestMust(t, r.validate())
		if storageIdentityIntentCoreValidate(r.Core) == nil || r.Core.validate() == nil {
			t.Fatal("legacy validator admitted direct creation", kind)
		}
		raw, err := storageEncodeCreationIntent(r, r.Tail.Installation, r.Tail.QBinding)
		storageTestMust(t, err)
		if len(raw) != 16384 || string(raw[:8]) != "DSKHINT4" || binary.LittleEndian.Uint16(raw[8:10]) != 4 || binary.LittleEndian.Uint32(raw[72:76]) > storageCreationMaximumEncoded-128 || !storageAllZero(raw[storageCreationMaximumEncoded:]) {
			t.Fatal("closed direct-creation layout", kind)
		}
		decoded, err := storageDecodeCreationIntent(raw, 0, r.Tail.Installation, r.Tail.QBinding)
		storageTestMust(t, err)
		if !reflect.DeepEqual(decoded, r) {
			t.Fatal("lost creation or retained-tail field", kind)
		}
		if _, err = storageDecodeIntent(raw, 0, r.Tail.Installation, r.Tail.QBinding); err == nil {
			t.Fatal("J2 decoded J4")
		}
		if _, err = storageDecodeIdentityIntent(raw, 0, r.Tail.Installation, r.Tail.QBinding); err == nil {
			t.Fatal("J3 decoded J4")
		}
		if _, err = storageSelectIdentityIntent(raw, make([]byte, 16384), 0, r.Tail.Installation, r.Tail.QBinding); err == nil {
			t.Fatal("legacy selector admitted a J4 tail before explicit routing")
		}
		for _, offset := range []int{0, 8, 10, 12, 13, 16, 24, 40, 72, 108, storageCreationMaximumEncoded, 16383} {
			bad := append([]byte{}, raw...)
			bad[offset] ^= 1
			storageIdentityIntentSeal(bad)
			if _, err = storageDecodeCreationIntent(bad, 0, r.Tail.Installation, r.Tail.QBinding); err == nil {
				t.Fatal("header/reserved mutation admitted", kind, offset)
			}
		}
		if _, err = storageDecodeCreationIntent(raw[:16383], 0, r.Tail.Installation, r.Tail.QBinding); err == nil {
			t.Fatal("short page decoded")
		}
	}
}

// These bytes state the old full-row order independently. In compact mode
// the caller supplies handwritten prior-leader indices, not product decisions.
func storageCreationTextHandwritten(w *epBytes, value string, width int) {
	w.fixed(uint16(len(value)))
	w.Write([]byte(value))
	w.Write(make([]byte, width-len(value)))
}
func storageCreationCoreHandwritten(r storageIntentRecord, leaders map[int]int) []byte {
	w := epBytes{}
	w.fixed(r.OperationID)
	w.fixed(r.PreviousPageDigest)
	w.fixed(r.CASBinding)
	w.fixed(r.AdmissionEpoch)
	w.fixed(r.PolicyRevision)
	w.fixed(r.RetirementEpoch)
	w.fixed(r.NewLength)
	w.fixed(r.Token)
	w.fixed(r.PreviousToken)
	w.fixed(r.Outcome)
	w.fixed(r.Rejection)
	w.fixed(r.Evidence)
	w.fixed(byte(len(r.Components)))
	for i, v := range r.Components {
		if len(r.Components) > 3 {
			if leader, ok := leaders[i]; ok {
				w.fixed(byte(leader + 1))
				w.fixed(v.Workspace)
				w.fixed(v.WorkspaceGeneration)
				continue
			}
			w.fixed(byte(0))
		}
		w.fixed(v.Filesystem)
		w.fixed(v.FilesystemGeneration)
		w.fixed(v.Root)
		w.fixed(v.RootGeneration)
		w.fixed(v.Owner)
		w.fixed(v.Workspace)
		w.fixed(v.WorkspaceGeneration)
		w.fixed(v.Category)
		w.fixed(v.Physical)
		w.fixed(v.Attribution)
		w.fixed(v.SettlementPhysical)
		w.fixed(v.SettlementAttribution)
		w.fixed(v.CapacityCount)
		w.fixed(v.Capacity)
		w.fixed(v.CapacityBinding)
		w.Write(make([]byte, 6))
		w.fixed(r.RootIdentities[i])
		w.fixed(r.FilesystemIdentities[i])
		w.fixed(r.Changes[i])
		storageCreationTextHandwritten(&w, r.Locators[i], 256)
	}
	storageCreationTextHandwritten(&w, r.Target, 255)
	storageCreationTextHandwritten(&w, r.Workflow, 128)
	storageCreationTextHandwritten(&w, r.Operation, 128)
	return w.Bytes()
}
func storageCreationSuffixHandwrittenSize(r storageCreationIntent) int {
	n := 2 + 2 + 4096 + 1 + len(r.Parents)*37 + 1 + 4096
	for range r.Effects {
		n += 3 + 2 + 255 + 8 + 32 + 35
	}
	return n
}
func storageCreationSixteenWorkspaceFixture(t *testing.T) (storageCreationIntent, map[int]int) {
	t.Helper()
	r := storageCreationFixture(t, storageCreationHistory)
	third := r.Core.Components[0]
	third.Root = storageIdentityIntentID(45)
	third.Category = storagebudget.Control
	r.Core.Components = append(r.Core.Components, third)
	r.Core.RootIdentities = append(r.Core.RootIdentities, storageIdentityIntentHash(46))
	r.Core.FilesystemIdentities = append(r.Core.FilesystemIdentities, storageIdentityIntentHash(47))
	r.Core.Changes = append(r.Core.Changes, storagebudget.Change{})
	r.Core.Locators = []string{strings.Repeat("l", 256), strings.Repeat("m", 256), strings.Repeat("n", 256)}
	r.Core.Components[0].Workspace = storageIdentityIntentID(50)
	r.Core.Components[0].WorkspaceGeneration = 1
	r.Core.Components[0].Attribution = r.Core.Components[0].Physical
	leaders := map[int]int{}
	for i := 3; i < 16; i++ {
		v := r.Core.Components[0]
		v.Workspace = storageIdentityIntentID(byte(57 + i))
		v.WorkspaceGeneration = uint64(i)
		v.Attribution = v.Physical
		v.Physical = storagebudget.Vector{}
		v.SettlementPhysical = storagebudget.Vector{}
		v.SettlementAttribution = storagebudget.Vector{}
		for j := range v.Capacity {
			v.Capacity[j].Peak = storagebudget.CapacityVector{}
		}
		r.Core.Components = append(r.Core.Components, v)
		r.Core.RootIdentities = append(r.Core.RootIdentities, r.Core.RootIdentities[0])
		r.Core.FilesystemIdentities = append(r.Core.FilesystemIdentities, r.Core.FilesystemIdentities[0])
		r.Core.Locators = append(r.Core.Locators, r.Core.Locators[0])
		r.Core.Changes = append(r.Core.Changes, storagebudget.Change{})
		leaders[i] = 0
	}
	r.SelectedPath = "/" + strings.Repeat("p", 4095)
	r.Core.CASBinding = r.planDigest()
	r.Core.Operation = "p:" + hex.EncodeToString(r.Core.CASBinding[:]) + ":create"
	return r, leaders
}
func TestStorageCreationCompactAttributionKeepsLegacyCoreBytes(t *testing.T) {
	for n := 1; n <= 3; n++ {
		r := storageCreationFixture(t, storageCreationHistory).Core
		for len(r.Components) < n {
			v := r.Components[0]
			v.Root = storageIdentityIntentID(byte(90 + len(r.Components)))
			r.Components = append(r.Components, v)
			r.RootIdentities = append(r.RootIdentities, storageIdentityIntentHash(100))
			r.FilesystemIdentities = append(r.FilesystemIdentities, storageIdentityIntentHash(101))
			r.Changes = append(r.Changes, storagebudget.Change{})
			r.Locators = append(r.Locators, "third")
		}
		r.Components = r.Components[:n]
		r.RootIdentities = r.RootIdentities[:n]
		r.FilesystemIdentities = r.FilesystemIdentities[:n]
		r.Changes = r.Changes[:n]
		r.Locators = r.Locators[:n]
		want := storageCreationCoreHandwritten(r, nil)
		raw := make([]byte, 16384)
		codec := storageIntentCodec{b: raw}
		if !storageCreationCoreCodec(&codec, &r, false) || !bytes.Equal(raw[:codec.at], want) {
			t.Fatal("legacy core bytes changed", n)
		}
		var got storageIntentRecord
		reader := storageIntentCodec{b: want}
		if !storageCreationCoreCodec(&reader, &got, true) || reader.at != len(want) || !reflect.DeepEqual(got.Components, r.Components) || !reflect.DeepEqual(got.Changes, r.Changes) {
			t.Fatal("legacy core reconstruction changed", n)
		}
	}
}
func TestStorageCreationCompactSixteenAttributionRows(t *testing.T) {
	r, leaders := storageCreationSixteenWorkspaceFixture(t)
	storageTestMust(t, r.validate())
	want := storageCreationCoreHandwritten(r.Core, leaders)
	if len(want) != 3512 || want[212] != 16 || want[213] != 0 || want[213+819] != 0 || want[213+2*819] != 0 || want[213+3*819] != 1 {
		t.Fatal("handwritten16-row core widths/tags", len(want))
	}
	// Count16 exceeds the old reader's closed1..3 bound before any row read.
	if want[212] <= 3 {
		t.Fatal("compact rows lost old-reader refusal discriminator")
	}
	raw, err := storageEncodeCreationIntent(r, r.Tail.Installation, r.Tail.QBinding)
	storageTestMust(t, err)
	end := 128 + len(want) + storageCreationSuffixHandwrittenSize(r)
	if end != 12917 || int(binary.LittleEndian.Uint32(raw[72:76]))+128 != end || !bytes.Equal(raw[128:128+len(want)], want) {
		t.Fatal("legal16-row encoding/length differs from independent bytes", end)
	}
	got, err := storageDecodeCreationIntent(raw, 0, r.Tail.Installation, r.Tail.QBinding)
	storageTestMust(t, err)
	if !reflect.DeepEqual(got, r) {
		t.Fatal("lost full expanded attribution/identity/locator/settlement operands")
	}
	for _, kind := range []string{"forward", "chained", "other-leader", "zero-workspace", "duplicate-workspace", "generation"} {
		t.Run(kind, func(t *testing.T) {
			bad := append([]byte{}, raw...)
			at := 128 + 213 + 3*819
			switch kind {
			case "forward":
				bad[at] = 4
			case "chained":
				bad[at+25] = 4
			case "other-leader":
				bad[at] = 2
			case "zero-workspace":
				clear(bad[at+1 : at+17])
			case "duplicate-workspace":
				copy(bad[at+1:at+17], r.Core.Components[0].Workspace[:])
			case "generation":
				clear(bad[at+17 : at+25])
			}
			storageIdentityIntentSeal(bad)
			if _, err := storageDecodeCreationIntent(bad, 0, r.Tail.Installation, r.Tail.QBinding); err == nil {
				t.Fatal("accepted changed compact alias", kind)
			}
		})
	}
	for _, mutate := range []func(*storageCreationIntent){func(v *storageCreationIntent) { v.Core.Components[3].Attribution.Bytes++ }, func(v *storageCreationIntent) { v.Core.RootIdentities[3][0] ^= 1 }, func(v *storageCreationIntent) { v.Core.Locators[3] = "changed" }, func(v *storageCreationIntent) { v.Core.Components[3].CapacityBinding[0] ^= 1 }, func(v *storageCreationIntent) { v.Core.Changes[3].AttributionAdd.Bytes = 1 }} {
		bad := r
		bad.Core.Components = append([]storagebudget.Component{}, r.Core.Components...)
		bad.Core.RootIdentities = append([]storagebudget.Digest{}, r.Core.RootIdentities...)
		bad.Core.Locators = append([]string{}, r.Core.Locators...)
		bad.Core.Changes = append([]storagebudget.Change{}, r.Core.Changes...)
		mutate(&bad)
		bad.Core.CASBinding = bad.planDigest()
		bad.Core.Operation = "p:" + hex.EncodeToString(bad.Core.CASBinding[:]) + ":create"
		if _, err := storageEncodeCreationIntent(bad, bad.Tail.Installation, bad.Tail.QBinding); err == nil {
			t.Fatal("changed shared attribution operand accepted")
		}
	}
}

func TestStorageCreationCompactSettlementKeepsEveryAttribution(t *testing.T) {
	r, _ := storageCreationSixteenWorkspaceFixture(t)
	r.Core.Generation = 2
	r.Core.PreviousPageDigest = storageIdentityIntentHash(121)
	r.Core.Phase = storageIntentSettling
	r.Core.PolicyRevision = 7
	r.Core.PreviousToken = storagebudget.Token{Slot: 3, ID: storageIdentityIntentID(120), Sequence: 9, Generation: 2}
	r.Core.Token = r.Core.PreviousToken
	r.Core.Token.Generation++
	for i := range r.Effects {
		r.Effects[i].State = storageCreationDurable
		r.Effects[i].Native = storageIdentityIntentPO(uint64(700+i), r.Effects[i].Kind)
	}
	settled, err := storageCreationSettlement(r)
	storageTestMust(t, err)
	storageTestMust(t, settled.validate())
	history := storagebudget.Vector{Bytes: 100, Files: 1, Directories: 1}
	pointer := storagebudget.Vector{Bytes: 200, Files: 1}
	if settled.Core.Changes[0] != (storagebudget.Change{PhysicalAdd: history, AttributionAdd: history}) || settled.Core.Changes[1] != (storagebudget.Change{PhysicalAdd: pointer}) || settled.Core.Changes[2] != (storagebudget.Change{}) {
		t.Fatal("physical groups changed factual settlement")
	}
	for i := 3; i < 16; i++ {
		if settled.Core.Changes[i] != (storagebudget.Change{AttributionAdd: history}) {
			t.Fatal("lost or double-counted workspace attribution", i)
		}
	}
	raw, err := storageEncodeCreationIntent(settled, settled.Tail.Installation, settled.Tail.QBinding)
	storageTestMust(t, err)
	got, err := storageDecodeCreationIntent(raw, 0, settled.Tail.Installation, settled.Tail.QBinding)
	storageTestMust(t, err)
	if !reflect.DeepEqual(got.Core.Changes, settled.Core.Changes) {
		t.Fatal("compact terminal lost attribution settlement")
	}
	request, err := storageCreationRequest(settled)
	storageTestMust(t, err)
	if len(request.Components) != 16 || len(request.Entries) != 3 || request.Entries[0].Component != 0 || request.Entries[1].Component != 0 || request.Entries[2].Component != 1 {
		t.Fatal("canonical manifest lost full components or original effect mapping")
	}
}

func TestStorageCreationReferencedWorkspaceCensusIsComplete(t *testing.T) {
	// Handwritten comparison DTOs only. No live reservation, installed native
	// profile or owner capability is fabricated by this reference-census test.
	r := storageCreationFixture(t, storageCreationHistory)
	for i := range r.Core.Components {
		r.Core.Components[i].Capacity[0].Peak.Inodes = 3
	}
	r.Core.Components[0].Workspace = storageIdentityIntentID(50)
	r.Core.Components[0].WorkspaceGeneration = 1
	r.Core.Components[0].Attribution = r.Core.Components[0].Physical
	refs := []storagebudget.ID{r.Core.Components[0].Workspace}
	for i := 2; i < 16; i++ {
		v := r.Core.Components[0]
		v.Workspace = storageIdentityIntentID(byte(60 + i))
		v.WorkspaceGeneration = 1
		v.Attribution = v.Physical
		v.Physical = storagebudget.Vector{}
		v.SettlementPhysical = storagebudget.Vector{}
		v.SettlementAttribution = storagebudget.Vector{}
		for j := range v.Capacity {
			v.Capacity[j].Peak = storagebudget.CapacityVector{}
		}
		r.Core.Components = append(r.Core.Components, v)
		r.Core.RootIdentities = append(r.Core.RootIdentities, r.Core.RootIdentities[0])
		r.Core.FilesystemIdentities = append(r.Core.FilesystemIdentities, r.Core.FilesystemIdentities[0])
		r.Core.Locators = append(r.Core.Locators, r.Core.Locators[0])
		r.Core.Changes = append(r.Core.Changes, storagebudget.Change{})
		refs = append(refs, v.Workspace)
	}
	owner := &storageBudgetOwner{roots: map[storagebudget.ID]*storageRootBinding{}, filesystems: map[storagebudget.ID]storagebudget.Filesystem{}, workspaces: map[storagebudget.ID]storagebudget.Workspace{}, constraints: map[storagebudget.ID]storagebudget.CapacityConstraint{}}
	for i := 0; i < 2; i++ {
		component := r.Core.Components[i]
		root := storageOwnerRootValue(storagebudget.RegisteredRoot{ID: component.Root, Owner: component.Owner, Identity: r.Core.RootIdentities[i], Filesystem: component.Filesystem, Generation: component.RootGeneration, Locator: r.Core.Locators[i], CapacityCount: component.CapacityCount, CapacityBinding: component.CapacityBinding})
		for j := 0; j < int(root.CapacityCount); j++ {
			root.CapacityRefs[j] = storagebudget.CapacityRef{Constraint: component.Capacity[j].Constraint, Generation: component.Capacity[j].Generation}
		}
		binding := &storageRootBinding{row: root}
		if i == 0 {
			binding.references = refs
		} else {
			binding.configControl = true
		}
		owner.roots[root.ID] = binding
		owner.filesystems[component.Filesystem] = storagebudget.Filesystem{ID: component.Filesystem, Generation: component.FilesystemGeneration, Identity: r.Core.FilesystemIdentities[i]}
		for j := 0; j < int(root.CapacityCount); j++ {
			ref := root.CapacityRefs[j]
			owner.constraints[ref.Constraint] = storagebudget.CapacityConstraint{ID: ref.Constraint, Generation: ref.Generation, Resources: 3}
		}
	}
	// The two fixture roots share the same exact filesystem identity.
	r.Core.FilesystemIdentities[1] = r.Core.FilesystemIdentities[0]
	fs := owner.filesystems[r.Core.Components[0].Filesystem]
	fs.Identity = r.Core.FilesystemIdentities[0]
	owner.filesystems[fs.ID] = fs
	for _, id := range refs {
		owner.workspaces[id] = storagebudget.Workspace{ID: id, Generation: 1}
	}
	m := &storageCreationLive{record: r, phase: &storageEnrollmentEpoch{c: &storageBudgetConstruction{owner: owner}}}
	storageTestMust(t, m.validateReferencedComponents())
	for _, kind := range []string{"missing-original-reference", "unlisted-workspace", "shared-identity", "generation"} {
		t.Run(kind, func(t *testing.T) {
			bad := r
			bad.Core.Components = append([]storagebudget.Component{}, r.Core.Components...)
			bad.Core.RootIdentities = append([]storagebudget.Digest{}, r.Core.RootIdentities...)
			switch kind {
			case "missing-original-reference":
				bad.Core.Components = bad.Core.Components[:15]
				bad.Core.RootIdentities = bad.Core.RootIdentities[:15]
				bad.Core.FilesystemIdentities = bad.Core.FilesystemIdentities[:15]
				bad.Core.Locators = bad.Core.Locators[:15]
				bad.Core.Changes = bad.Core.Changes[:15]
			case "unlisted-workspace":
				bad.Core.Components[2].Workspace = storageIdentityIntentID(99)
			case "shared-identity":
				bad.Core.RootIdentities[2][0] ^= 1
			case "generation":
				bad.Core.Components[2].WorkspaceGeneration++
			}
			m.record = bad
			if m.validateReferencedComponents() == nil {
				t.Fatal("incomplete original attribution census accepted", kind)
			}
		})
	}
}

func TestStorageCreationClosedEffectsAndTailMutations(t *testing.T) {
	for _, mutate := range []func(*storageCreationIntent){
		func(r *storageCreationIntent) { r.Kind = 0 }, func(r *storageCreationIntent) { r.Branch = 0 }, func(r *storageCreationIntent) { r.SelectedPath = "relative" }, func(r *storageCreationIntent) { r.Core.Stage = "fake-stage" }, func(r *storageCreationIntent) { r.Core.Workflow = "desk-signing-lineage-v1" }, func(r *storageCreationIntent) { r.Effects[0].Name = "arbitrary" }, func(r *storageCreationIntent) { r.Effects[2].Parent = 0 }, func(r *storageCreationIntent) { r.Effects[0].State = storageCreationCaptured }, func(r *storageCreationIntent) { r.Effects[1].Native = storageIdentityIntentPO(300, 1) }, func(r *storageCreationIntent) { r.Effects[2].Bytes = 3 }, func(r *storageCreationIntent) { r.Parents[0].Component = 2 }, func(r *storageCreationIntent) { r.Tail.MaximumExtent++ }, func(r *storageCreationIntent) { r.Tail.ParentPeaks[0].Bytes-- }, func(r *storageCreationIntent) { r.Tail.Logical.Bytes-- }, func(r *storageCreationIntent) { r.Tail.Capacity[0][24] = 1 }, func(r *storageCreationIntent) { r.Tail.Capacity[0][32] ^= 1 }, func(r *storageCreationIntent) { r.Tail.Capacity[0][25] = 1 }, func(r *storageCreationIntent) { r.Tail.Targets[0] = storageIdentityIntentPO(301, 2) }, func(r *storageCreationIntent) { r.Tail.Targets[6].VolumeUUID[0] ^= 1 },
	} {
		r := storageCreationFixture(t, storageCreationPrivate)
		mutate(&r)
		r.Core.CASBinding = r.planDigest()
		r.Core.Operation = "p:" + hex.EncodeToString(r.Core.CASBinding[:]) + ":create"
		if r.validate() == nil {
			t.Fatal("widened effect/tail admitted")
		}
	}
	r := storageCreationFixture(t, storageCreationHistory)
	r.Branch = storageCreationPersonal
	r.Core.Target = "history"
	r.Core.Stage = "history"
	r.Effects[0].Name = "history"
	r.Core.CASBinding = r.planDigest()
	r.Core.Operation = "p:" + hex.EncodeToString(r.Core.CASBinding[:]) + ":create"
	storageTestMust(t, r.validate())
	r.Effects[2].Parent = 2
	r.Core.CASBinding = r.planDigest()
	r.Core.Operation = "p:" + hex.EncodeToString(r.Core.CASBinding[:]) + ":create"
	if r.validate() == nil {
		t.Fatal("location published inside history instead of original settings")
	}
}

func TestStorageCreationExactMaximumAndTailBound(t *testing.T) {
	r := storageCreationFixture(t, storageCreationHistory)
	r.Branch = storageCreationContained
	third := r.Core.Components[0]
	third.Root = storageIdentityIntentID(45)
	third.Category = storagebudget.Control
	r.Core.Components = append(r.Core.Components, third)
	r.Core.RootIdentities = append(r.Core.RootIdentities, storageIdentityIntentHash(46))
	r.Core.FilesystemIdentities = append(r.Core.FilesystemIdentities, storageIdentityIntentHash(47))
	r.Core.Locators = []string{strings.Repeat("l", 256), strings.Repeat("m", 256), strings.Repeat("n", 256)}
	r.Core.Changes = append(r.Core.Changes, storagebudget.Change{})
	r.SelectedPath = "/" + strings.Repeat("p", 4095)
	r.Core.CASBinding = r.planDigest()
	r.Core.Operation = "p:" + hex.EncodeToString(r.Core.CASBinding[:]) + ":create"
	storageTestMust(t, r.validate())
	raw, err := storageEncodeCreationIntent(r, r.Tail.Installation, r.Tail.QBinding)
	storageTestMust(t, err)
	// Every text field retains its old fixed padded width, even when its
	// legal value is shorter. Three full rows omit the compact mode tags and
	// thirteen attribution aliases, so this fixture is 328 bytes below the ceiling.
	expectedEnd := 128 + len(storageCreationCoreHandwritten(r.Core, nil)) + storageCreationSuffixHandwrittenSize(r)
	if int(binary.LittleEndian.Uint32(raw[72:76]))+128 != expectedEnd || expectedEnd != 12589 || storageCreationMaximumEncoded != 128+730+3*819+13*25+2+4098+1081+4096 || storageCreationMaximumEncoded > 16384 {
		t.Fatal("independent legal length or conservative field-width bound changed", binary.LittleEndian.Uint32(raw[72:76]), expectedEnd)
	}
	decoded, err := storageDecodeCreationIntent(raw, 0, r.Tail.Installation, r.Tail.QBinding)
	storageTestMust(t, err)
	if !reflect.DeepEqual(decoded, r) {
		t.Fatal("maximum record truncated")
	}
	for _, before := range [][8]uint64{{65536, 65536, 4096, 4096, 8388608, 8388608, 4194304, 4194304}, {71303168, 71303168, 4096, 4096, 8388608, 8388608, 4194304, 4194304}} {
		peaks, logical, err := storageCreationTailDimensions(true, before)
		storageTestMust(t, err)
		if logical.Files != 0 || logical.Bytes != 2*71303168-before[0]-before[1] {
			t.Fatal("existing allocation subtracted incorrectly")
		}
		for _, path := range []byte{2, 3} {
			for c := byte(0); c < 2; c++ {
				for e := byte(0); e < 2; e++ {
					for d := byte(0); d < 2; d++ {
						for q := byte(0); q < 2; q++ {
							var sums [2]storagebudget.CapacityVector
							shape := storageEnrollmentPeakScheduleShape{Path: path, CatalogBank: c, EvidenceBank: e, DispatchBank: d, CheckpointBank: q, Final: [2]uint64{71303168, 71303168}}
							for _, g := range storageEnrollmentPeakPlanShape(shape) {
								v, ok := storageEnrollmentPeakEstimate(g)
								if !ok {
									t.Fatal("finite schedule overflow")
								}
								sums[g.Parent-1].Bytes += v.Bytes
								sums[g.Parent-1].Inodes += v.Inodes
							}
							for i := range sums {
								if sums[i].Bytes > peaks[i].Bytes || sums[i].Inodes > peaks[i].Inodes {
									t.Fatal("tail ceiling omitted a physical bank order", path, c, e, d, q)
								}
							}
						}
					}
				}
			}
		}
	}
	// Reservation-only dimensions carry no valid encoded Scope or fake F.
	if _, err := storageEnrollmentPeakDecodeScope(make([]byte, 4096)); err == nil {
		t.Fatal("unissued schedule dimensions became Scope")
	}
	if bytes.Equal(raw, make([]byte, 16384)) {
		t.Fatal("empty maximum vector")
	}
}

func TestStorageCreationActualHelpersBorrowOriginalAndExcludeOtherConfig(t *testing.T) {
	cfg1, cfg2 := ownerActualConfig(t), ownerActualConfig(t)
	cfg2.ProjectDir = cfg1.ProjectDir
	first, second := ownerActualNew(t, cfg1), ownerActualNew(t, cfg2)
	if first.configDir == second.configDir || first.root == second.root {
		t.Fatal("fixture did not retain two actual configurations/descriptions")
	}
	lease, err := first.lockProjectOwned(t.Context(), 0)
	storageTestMust(t, err)
	defer func() { storageTestMust(t, lease.Close()) }()
	before, err := second.storageConstruction.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	// Both actual legacy creation entry points must honor the other
	// configuration's original EX before creating the shared first name.
	if root, _, err := openOwnFolder(second.root, []string{".desk-private", "dialect-drafts"}, true, "dialect drafts"); !errors.Is(err, storagebudget.ErrBusy) {
		if root != nil {
			root.Close()
		}
		t.Fatal("openOwnFolder ignored competing original project", err)
	}
	if root, err := second.reviewedCopies(true); !errors.Is(err, storagebudget.ErrBusy) {
		if root != nil {
			root.Close()
		}
		t.Fatal("reviewedCopies ignored competing original project", err)
	}
	if _, err := first.root.Lstat(".desk-private"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed creator entry changed namespace", err)
	}
	after, err := second.storageConstruction.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("competing creator refusal changed Q")
	}
	// The original holder explicitly lends its real descriptor. Borrow Close
	// drops only the borrowed edge; another description still cannot take EX.
	borrow, err := lease.borrow(t.Context(), first.root)
	storageTestMust(t, err)
	storageTestMust(t, borrow.current(t.Context()))
	storageTestMust(t, borrow.Close())
	probe, err := second.root.Open(".")
	storageTestMust(t, err)
	defer probe.Close()
	if err = lockProjectFile(probe); !lockHeld(err) {
		t.Fatal("borrow Close released original project EX", err)
	}
	copies, err := first.reviewedCopiesWithCreator(true, lease)
	storageTestMust(t, err)
	storageTestMust(t, copies.Close())
	if err = lockProjectFile(probe); !lockHeld(err) {
		t.Fatal("helper borrowed Close released original project EX", err)
	}
	storageTestMust(t, lease.Close())
	storageTestMust(t, lockProjectFile(probe))
}

func TestStorageCreationOriginalCloseRetainsBorrowedDescription(t *testing.T) {
	s := ownerActualNew(t, ownerActualConfig(t))
	lease, err := s.lockProjectOwned(t.Context(), 0)
	storageTestMust(t, err)
	borrow, err := lease.borrow(t.Context(), s.root)
	storageTestMust(t, err)
	if err = lease.Close(); !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("Close claimed borrowed original drained", err)
	}
	if _, err = lease.file.Stat(); err != nil {
		t.Fatal("Close released borrowed original description", err)
	}
	competing, err := s.root.Open(".")
	storageTestMust(t, err)
	defer competing.Close()
	if err = lockProjectFile(competing); !lockHeld(err) {
		t.Fatal("Close released EX before borrowed drain", err)
	}
	storageTestMust(t, borrow.Close())
	storageTestMust(t, lease.Close())
	storageTestMust(t, lockProjectFile(competing))
}

func TestStorageCreationContainedHistoryPreservesCategoryAndRootPartition(t *testing.T) {
	r := storageCreationFixture(t, storageCreationHistory)
	r.Branch = storageCreationContained
	// Same native parent and filesystem do not merge different categories.
	// The separately owned config host remains its own Control component.
	r.Parents[1].Native = r.Parents[0].Native
	r.Core.Components[1].Root = r.Core.Components[0].Root
	r.Core.RootIdentities[1] = r.Core.RootIdentities[0]
	r.Core.FilesystemIdentities[1] = r.Core.FilesystemIdentities[0]
	r.Core.Locators[1] = r.Core.Locators[0]
	host := r.Core.Components[1]
	host.Root = storageIdentityIntentID(45)
	host.Physical = storagebudget.Vector{}
	host.SettlementPhysical = storagebudget.Vector{Bytes: 65536}
	r.Core.Components = append(r.Core.Components, host)
	r.Core.RootIdentities = append(r.Core.RootIdentities, storageIdentityIntentHash(46))
	r.Core.FilesystemIdentities = append(r.Core.FilesystemIdentities, r.Core.FilesystemIdentities[0])
	r.Core.Locators = append(r.Core.Locators, "config")
	r.Core.Changes = append(r.Core.Changes, storagebudget.Change{})
	r.Core.CASBinding = r.planDigest()
	r.Core.Operation = "p:" + hex.EncodeToString(r.Core.CASBinding[:]) + ":create"
	storageTestMust(t, r.validate())
	raw, err := storageEncodeCreationIntent(r, r.Tail.Installation, r.Tail.QBinding)
	storageTestMust(t, err)
	got, err := storageDecodeCreationIntent(raw, 0, r.Tail.Installation, r.Tail.QBinding)
	storageTestMust(t, err)
	if len(got.Core.Components) != 3 || got.Core.Components[0].Category != storagebudget.History || got.Core.Components[1].Category != storagebudget.Control || got.Core.Components[2].Category != storagebudget.Control || got.Core.Components[0].Root != got.Core.Components[1].Root || got.Core.Components[1].Root == got.Core.Components[2].Root {
		t.Fatal("same parent/filesystem collapsed History, pointer Control or config-host Control")
	}
	bad := r
	bad.Parents = append([]storageCreationParent{}, r.Parents...)
	bad.Parents[1].Component = 0
	bad.Core.CASBinding = bad.planDigest()
	bad.Core.Operation = "p:" + hex.EncodeToString(bad.Core.CASBinding[:]) + ":create"
	if bad.validate() == nil {
		t.Fatal("pointer effect admitted on History component")
	}
	bad = r
	bad.Parents = append([]storageCreationParent{}, r.Parents...)
	bad.Parents[0].Component = 1
	bad.Core.CASBinding = bad.planDigest()
	bad.Core.Operation = "p:" + hex.EncodeToString(bad.Core.CASBinding[:]) + ":create"
	if bad.validate() == nil {
		t.Fatal("data/marker effects admitted on Control component")
	}
}

func TestStorageCreationFixedCreatorsRespectOriginalRootWindow(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	storageTestMust(t, err)
	defer root.Close()
	lease, err := storageCreatorAcquire(root)
	storageTestMust(t, err)
	if err = claimPrivateData(root, dataMarkerName, []byte(`{"version":1,"owner":"fixture"}`)); !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("marker creator escaped held root", err)
	}
	if _, err = writePrivateDataCommit(root, dataLocationName, []byte(`{"version":1,"path":"/fixture/history"}`)); !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("location creator escaped held parent", err)
	}
	if err = storageCreateDraftIgnore(root); !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("draft ignore creator escaped held root", err)
	}
	if file, err := storageCreateDraftWorkflowLock(root); !errors.Is(err, storagebudget.ErrBusy) {
		if file != nil {
			file.Close()
		}
		t.Fatal("draft workflow creator escaped held root", err)
	}
	for _, name := range []string{dataMarkerName, dataLocationName, ".gitignore", "workflow.lock"} {
		if _, err = root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("refused creator changed namespace", name, err)
		}
	}
	storageTestMust(t, lease.Close())
	storageTestMust(t, claimPrivateData(root, dataMarkerName, []byte(`{"version":1,"owner":"fixture"}`)))
	storageTestMust(t, storageCreateDraftIgnore(root))
	file, err := storageCreateDraftWorkflowLock(root)
	storageTestMust(t, err)
	storageTestMust(t, file.Close())
}

func TestStorageCreationManifestKeepsEveryDirectEffect(t *testing.T) {
	for _, kind := range []byte{storageCreationPrivate, storageCreationHistory, storageCreationAncestor} {
		r := storageCreationFixture(t, kind)
		request, err := storageCreationRequest(r)
		storageTestMust(t, err)
		if request.Workflow != r.Core.Workflow || request.Operation != r.Core.Operation || request.Owner != r.Tail.QBinding || !reflect.DeepEqual(request.Components, r.Core.Components) || len(request.Entries) != len(r.Effects) {
			t.Fatal("manifest omitted original action operands", kind)
		}
		paths := []string{".desk-private", ".desk-private/dialect-drafts", ".desk-private/dialect-drafts/.gitignore", ".desk-private/dialect-drafts/workflow.lock"}
		if kind == storageCreationHistory {
			paths = []string{"data", "data/" + dataMarkerName, dataLocationName}
		}
		if kind == storageCreationAncestor {
			paths = []string{"jpack-desk"}
		}
		var sum storagebudget.Vector
		for i, entry := range request.Entries {
			if entry.Kind != storagebudget.ObjectEntry || entry.Target != paths[i] || entry.Stage != entry.Target || entry.NewIdentity != r.Core.CASBinding || entry.OldIdentity != (storagebudget.Digest{}) {
				t.Fatal("manifest invented a stage, identity or namespace", kind, i)
			}
			effect := r.Effects[i]
			if entry.Bytes != effect.Bytes || effect.Kind == 1 && (entry.Directories != 1 || entry.Files != 0) || effect.Kind == 2 && (entry.Files != 1 || entry.Directories != 0) {
				t.Fatal("manifest lost exact effect vector", kind, i)
			}
			sum.Bytes += entry.Bytes
			sum.Files += entry.Files
			sum.Directories += entry.Directories
		}
		if kind == storageCreationPrivate && sum != (storagebudget.Vector{Bytes: 2, Files: 2, Directories: 2}) {
			t.Fatal("zero-byte workflow file or directories omitted")
		}
	}
}
func TestStorageCreationRetainedTailRoomIncludesAction(t *testing.T) {
	r := storageCreationFixture(t, storageCreationPrivate)
	before, _, _, _, _ := epCatalogs(t)
	q := epQ0(before)
	for i := range q.Roots {
		if q.Roots[i].ID != (storageEnrollmentPeakQID{}) {
			q.Roots[i].Known = true
		}
	}
	for i := range q.Workspaces {
		if q.Workspaces[i].ID != (storageEnrollmentPeakQID{}) {
			q.Workspaces[i].Known = true
		}
	}
	// Exact byte boundary for the retained tail plus the application growth;
	// generous namespace axes keep the required64MiB/16/1 policy floor valid.
	q.Policy.Installation.Total = storageEnrollmentPeakQVector{Bytes: 1 << 50, Files: 1 << 30, Directories: 1 << 30}
	for i := range q.Policy.Installation.Category {
		q.Policy.Installation.Category[i] = q.Policy.Installation.Total
	}
	// storageIdentityIntentFixture contributes a retained 128MiB settlement
	// promise in addition to the private action's exact two payload bytes.
	if len(r.Core.Components) != 1 || r.Core.Components[0].Physical.Bytes != 2 || r.Core.Components[0].SettlementPhysical.Bytes != 128<<20 {
		t.Fatal("handwritten action/settlement dimensions changed")
	}
	q.Policy.Installation.Category[storagebudget.Control].Bytes = q.Installation[storagebudget.Control].Bytes + r.Tail.Logical.Bytes + 2 + (128 << 20)
	storageTestMust(t, storageCreationLogicalRoom(r.Tail, r.Core.Components, q))
	// Limits are warnings (storageLimitsAdvisory): one byte past the combined
	// logical limit is admitted, not refused.
	q.Policy.Installation.Category[storagebudget.Control].Bytes--
	if err := storageCreationLogicalRoom(r.Tail, r.Core.Components, q); err != nil {
		t.Fatal("an advisory combined logical limit refused the tail and action", err)
	}
	var id storagebudget.ID
	copy(id[:], r.Tail.Capacity[0][:16])
	bytes, inodes := storageEnrollmentPeakU64(r.Tail.Capacity[0][32:40]), storageEnrollmentPeakU64(r.Tail.Capacity[0][40:48])
	observations := []storagebudget.CapacityObservation{{Constraint: id, Generation: 1, KnownResources: storagebudget.CapacityBytes | storagebudget.CapacityInodes, Available: storagebudget.CapacityVector{Bytes: bytes + 100, Inodes: inodes + 20}}}
	out, err := storageCreationCapacityRoom(r.Tail, observations)
	storageTestMust(t, err)
	if out[0].Available != (storagebudget.CapacityVector{Bytes: 100, Inodes: 20}) || observations[0].Available.Bytes != bytes+100 {
		t.Fatal("tail was not retained independently of Q action promises")
	}
	observations[0].Available.Bytes = bytes - 1
	if _, err = storageCreationCapacityRoom(r.Tail, observations); !errors.Is(err, storagebudget.ErrHeadroom) {
		t.Fatal("tail capacity shortfall was ignored", err)
	}
}

func TestStorageCreationLegacyConfigSelectionHasOnlyCapturedPointer(t *testing.T) {
	r := storageCreationFixture(t, storageCreationHistory)
	r.SelectedPath = "/original/config"
	r.Core.NewLength = 200
	r.Parents = []storageCreationParent{{Role: 6, Component: 255, Native: r.Tail.Config}, {Role: 5, Component: 1, Native: r.Tail.Config}}
	r.Effects = []storageCreationEffect{{Parent: 1, Kind: 2, Name: dataLocationName, Bytes: 200, Payload: storageIdentityIntentHash(44)}}
	r.Core.CASBinding = r.planDigest()
	r.Core.Operation = "p:" + hex.EncodeToString(r.Core.CASBinding[:]) + ":create"
	storageTestMust(t, r.validate())
	if !r.legacyConfigSelection() {
		t.Fatal("lost exact original config-pointer branch")
	}
	request, err := storageCreationRequest(r)
	storageTestMust(t, err)
	if len(request.Entries) != 1 || request.Entries[0].Target != dataLocationName || request.Entries[0].Files != 1 || request.Entries[0].Directories != 0 {
		t.Fatal("legacy selection invented a marker or History directory")
	}
	for _, mutate := range []func(*storageCreationIntent){func(v *storageCreationIntent) { v.Branch = storageCreationContained }, func(v *storageCreationIntent) { v.Parents[0].Component = 0 }, func(v *storageCreationIntent) { v.Parents[1].Native = storageIdentityIntentPO(999, 1) }, func(v *storageCreationIntent) { v.Effects[0].Name = dataMarkerName }} {
		bad := r
		bad.Parents = append([]storageCreationParent{}, r.Parents...)
		bad.Effects = append([]storageCreationEffect{}, r.Effects...)
		mutate(&bad)
		bad.Core.CASBinding = bad.planDigest()
		bad.Core.Operation = "p:" + hex.EncodeToString(bad.Core.CASBinding[:]) + ":create"
		if bad.validate() == nil {
			t.Fatal("widened pointer-only legacy action")
		}
	}
}

func TestStorageCreationNativeLedgerCompletePhaseAndSettlement(t *testing.T) {
	r := storageCreationFixture(t, storageCreationPrivate)
	// The token identities and object natives below are handwritten operands;
	// this test is structural and provides no installed native qualification.
	token := storagebudget.Token{Slot: 3, ID: storageIdentityIntentID(120), Sequence: 9, Generation: 2}
	pages := 0
	save := func(next storageCreationIntent) {
		t.Helper()
		storageTestMust(t, next.validate())
		raw, err := storageEncodeCreationIntent(r, r.Tail.Installation, r.Tail.QBinding)
		storageTestMust(t, err)
		next.Core.Generation = r.Core.Generation + 1
		next.Core.PreviousPageDigest = sha256.Sum256(raw)
		if !storageCreationIntentTransition(r, next, raw) {
			t.Fatal("legitimate monotone native transition refused", r.Core.Phase, next.Core.Phase)
		}
		r = next
		pages++
	}
	next := r
	next.Core.Generation++
	next.Core.PreviousPageDigest = storageIdentityIntentHash(121)
	next.Core.Phase = storageIntentReserved
	next.Core.Token = token
	next.Core.PolicyRevision = 7
	save(next)
	next = r
	next.Core.Phase = storageIntentStaging
	next.Core.PreviousToken = token
	next.Core.Token.Generation++
	next.Core.Evidence = r.Core.CASBinding
	save(next)
	for i := range r.Effects {
		next = r
		next.Effects = append([]storageCreationEffect{}, r.Effects...)
		next.Effects[i].State = storageCreationCaptured
		next.Effects[i].Native = storageIdentityIntentPO(uint64(130+i), r.Effects[i].Kind)
		save(next)
		next = r
		next.Effects = append([]storageCreationEffect{}, r.Effects...)
		next.Effects[i].State = storageCreationDurable
		save(next)
	}
	next = r
	next.Core.Phase = storageIntentPublishing
	save(next)
	next = r
	next.Core.Phase = storageIntentSettling
	next, err := storageCreationSettlement(next)
	storageTestMust(t, err)
	if len(next.Core.Changes) != 1 || next.Core.Changes[0] != (storagebudget.Change{PhysicalAdd: storagebudget.Vector{Bytes: 2, Files: 2, Directories: 2}}) {
		t.Fatal("factual settlement lost zero-byte file or directory", next.Core.Changes)
	}
	save(next)
	next = r
	next.Core.Phase = storageIntentSettled
	next.Core.PreviousToken = next.Core.Token
	next.Core.Token.Generation++
	save(next)
	next = r
	next.Core.Phase = storageIntentRetired
	next.Core.RetirementEpoch = r.Core.AdmissionEpoch
	save(next)
	if pages+1 != 15 {
		t.Fatal("native host ledger no longer fits exact fifteen-page bound", pages+1)
	}
	ticket := storagebudget.Ticket{Token: r.Core.Token, State: storagebudget.Terminal, Workflow: r.Core.Workflow, Operation: r.Core.Operation, Owner: r.Tail.QBinding, Components: append([]storagebudget.Component{}, r.Core.Components...)}
	if !storageCreationTicketMatches(r, ticket) {
		t.Fatal("complete original creation ticket refused")
	}
	for _, mutate := range []func(*storagebudget.Ticket){func(t *storagebudget.Ticket) { t.Owner[0] ^= 1 }, func(t *storagebudget.Ticket) { t.Workflow = "desk-host-v1" }, func(t *storagebudget.Ticket) { t.Operation += "x" }, func(t *storagebudget.Ticket) { t.Components = nil }, func(t *storagebudget.Ticket) { t.State = storagebudget.Empty }} {
		bad := ticket
		mutate(&bad)
		if storageCreationTicketMatches(r, bad) {
			t.Fatal("unrelated ticket admitted as original action")
		}
	}
	// Tail authority does not follow retirement alone.
	if storageCreationTailIncluded(r, storageEnrollmentPeakEvidence{}) {
		t.Fatal("retirement alone discharged creation tail")
	}
}

func TestStorageCreationSettlementRequiresEveryDurableOriginal(t *testing.T) {
	r := storageCreationFixture(t, storageCreationPrivate)
	for i := range r.Effects {
		r.Effects[i].State = storageCreationDurable
		r.Effects[i].Native = storageIdentityIntentPO(uint64(150+i), r.Effects[i].Kind)
	}
	for i := range r.Effects {
		bad := r
		bad.Effects = append([]storageCreationEffect{}, r.Effects...)
		bad.Effects[i].State = storageCreationCaptured
		if _, err := storageCreationSettlement(bad); err == nil {
			t.Fatal("uncertain capture was zero-settled or treated as durable", i)
		}
	}
}

// All state and factual rows are handwritten. The ordinary JSON+SHA digest is
// calculated independently of the settlement producer; encoded pages below
// are parser inputs, never recorded expected output or qualification receipts.
func storageCreationRestartRecords(t *testing.T, r storageCreationIntent, slot uint16) (storageCreationIntent, []byte, []byte) {
	t.Helper()
	r.Core.Slot = slot
	r.Core.Generation = 19
	r.Core.PreviousPageDigest = storageIdentityIntentHash(121)
	r.Core.Phase = storageIntentSettled
	r.Core.PolicyRevision = 7
	r.Core.Token = storagebudget.Token{Slot: 3, ID: storageIdentityIntentID(120), Sequence: 9, Generation: 3}
	r.Core.PreviousToken = r.Core.Token
	r.Core.PreviousToken.Generation = 2
	r.Core.Outcome = storageIntentPublished
	for i := range r.Effects {
		r.Effects[i].State = storageCreationDurable
		r.Effects[i].Native = storageIdentityIntentPO(uint64(130+i), r.Effects[i].Kind)
	}
	r.Core.Changes = make([]storagebudget.Change, len(r.Core.Components))
	switch r.Kind {
	case storageCreationHistory:
		r.Core.Changes[0].PhysicalAdd = storagebudget.Vector{Bytes: 100, Files: 1, Directories: 1}
		r.Core.Changes[1].PhysicalAdd = storagebudget.Vector{Bytes: 200, Files: 1}
	case storageCreationAncestor:
		r.Core.Changes[0].PhysicalAdd = storagebudget.Vector{Directories: 1}
	case storageCreationPrivate:
		r.Core.Changes[0].PhysicalAdd = storagebudget.Vector{Bytes: 2, Files: 2, Directories: 2}
	}
	for i, c := range r.Core.Components {
		if c.Workspace != (storagebudget.ID{}) {
			r.Core.Changes[i].AttributionAdd = r.Core.Changes[i].PhysicalAdd
		}
	}
	r.Core.CASBinding = r.planDigest()
	r.Core.Operation = "p:" + hex.EncodeToString(r.Core.CASBinding[:]) + ":create"
	canonical := r
	canonical.Core.Evidence = storagebudget.Digest{}
	canonical.Core.Token = storagebudget.Token{}
	canonical.Core.PreviousToken = storagebudget.Token{}
	canonical.Core.PreviousPageDigest = storagebudget.Digest{}
	canonical.Core.Generation = 0
	canonical.Core.Phase = 0
	canonical.Core.RetirementEpoch = 0
	material, err := json.Marshal(canonical)
	storageTestMust(t, err)
	h := sha256.New()
	h.Write([]byte("desk-monotone-creation-factual-settlement-v1"))
	h.Write([]byte{0})
	h.Write(material)
	copy(r.Core.Evidence[:], h.Sum(nil))
	storageTestMust(t, r.validate())
	before, err := storageEncodeCreationIntent(r, r.Tail.Installation, r.Tail.QBinding)
	storageTestMust(t, err)
	r.Core.Generation++
	r.Core.PreviousPageDigest = sha256.Sum256(before)
	r.Core.Phase = storageIntentRetired
	r.Core.RetirementEpoch = r.Core.AdmissionEpoch
	storageTestMust(t, r.validate())
	after, err := storageEncodeCreationIntent(r, r.Tail.Installation, r.Tail.QBinding)
	storageTestMust(t, err)
	return r, before, after
}
func TestStorageEnrollmentRetiredCreationSelectionKeepsUncoveredTail(t *testing.T) {
	for _, kind := range []byte{storageCreationHistory, storageCreationAncestor, storageCreationPrivate} {
		r, settled, retired := storageCreationRestartRecords(t, storageCreationFixture(t, kind), 0)
		e := storageEnrollmentPeakEvidence{Phase: 2, Installation: r.Tail.Installation, QBinding: r.Tail.QBinding}
		// Selecting original bytes preserves an obligation. It neither appends
		// a catalog child nor supplies a captured source, native member or domain.
		if storageCreationTailIncluded(r, e) {
			t.Fatal("uncovered creation tail was discharged by selection", kind)
		}
		for _, reverse := range []bool{false, true} {
			raw := make([]byte, storageIntentBytes)
			a, b := settled, retired
			if reverse {
				a, b = b, a
			}
			copy(raw, a)
			copy(raw[storageIntentPage:], b)
			selected, page, err := storageEnrollmentUncoveredCreation(raw, e)
			storageTestMust(t, err)
			if !storageCreationRecordEqual(selected, &r) || !bytes.Equal(page, retired) {
				t.Fatal("exact selected retired record was lost", kind, reverse)
			}
			bad := append([]byte{}, raw...)
			bad[128] ^= 1
			if _, _, err = storageEnrollmentUncoveredCreation(bad, e); err == nil {
				t.Fatal("corrupt original bank was bypassed")
			}
		}
		duplicate, otherBefore, otherAfter := storageCreationRestartRecords(t, storageCreationFixture(t, kind), 1)
		_ = duplicate
		raw := make([]byte, storageIntentBytes)
		copy(raw, settled)
		copy(raw[storageIntentPage:], retired)
		copy(raw[2*storageIntentPage:], otherBefore)
		copy(raw[3*storageIntentPage:], otherAfter)
		if _, _, err := storageEnrollmentUncoveredCreation(raw, e); err == nil {
			t.Fatal("two unresolved tails were merged")
		}
		raw = make([]byte, storageIntentBytes)
		e.Witnesses = []storageEnrollmentPeakWitness{{Selected: true, Slot: 0, Core: r.Core}}
		if _, _, err := storageEnrollmentUncoveredCreation(raw, e); err == nil {
			t.Fatal("lost original host history accepted as empty")
		}
		if _, _, err := storageEnrollmentUncoveredCreation(raw[:len(raw)-1], e); err == nil {
			t.Fatal("short original host allocation accepted")
		}
		changed := r
		changed.SelectedPath = "/changed/history"
		if storageCreationRecordEqual(&r, &changed) || storageCreationRecordEqual(&r, nil) {
			t.Fatal("detached or changed record compared equal")
		}
	}
}
func TestStorageEnrollmentRetiredCreationQRequiresExactPersistedTuple(t *testing.T) {
	for _, kind := range []byte{storageCreationHistory, storageCreationAncestor, storageCreationPrivate} {
		catalog, record, _ := ownerCatalogFixture()
		r := storageCreationFixture(t, kind)
		r.Tail.Installation = record.Installation
		r.Tail.QBinding = record.QBinding
		r, _, _ = storageCreationRestartRecords(t, r, 0)
		q := epQ0(catalog)
		q.Epoch = r.Core.RetirementEpoch
		for i := range catalog.Roots {
			q.Roots[i].Known = true
		}
		for i := range catalog.Workspaces {
			q.Workspaces[i].Known = true
		}
		q.Applied[r.Core.Token.Slot] = storageEnrollmentPeakQApplied{ID: storageEnrollmentPeakQID(r.Core.Token.ID), Sequence: r.Core.Token.Sequence, Generation: r.Core.Token.Generation, NextSequence: r.Core.Token.Sequence + 1}
		current := storagebudget.EnrollmentInspection{View: storagebudget.HostInspection{Configuration: storagebudget.Configuration{Revision: q.Generation, Epoch: q.Epoch, Policy: storagebudget.Policy{Installation: catalog.Policy.Installation, Workspace: catalog.Policy.Workspace}}}}
		current.View.Configuration.Policy.ObservationMaxAge = q.Policy.ObservationMaxAge
		for _, row := range catalog.Roots {
			current.View.Roots = append(current.View.Roots, row.Definition)
		}
		for _, row := range catalog.Workspaces {
			current.View.Workspaces = append(current.View.Workspaces, row.Definition)
		}
		for _, row := range catalog.Filesystems {
			current.View.Configuration.Filesystems = append(current.View.Configuration.Filesystems, row.Definition)
		}
		for _, row := range catalog.Constraints {
			current.View.Configuration.CapacityConstraints = append(current.View.Configuration.CapacityConstraints, row.Definition)
		}
		current.View.Slots[r.Core.Token.Slot] = storagebudget.HostSlotState{PersistedApplied: r.Core.Token, PersistedNextSequence: r.Core.Token.Sequence + 1}
		current.Checkpoints[0] = epQBytes(q, record)
		if !storageCreationRetiredQCurrent(r, current, catalog) {
			t.Fatal("complete handwritten strict retired tuple refused")
		}
		for i, mutate := range []func(*storagebudget.EnrollmentInspection){func(v *storagebudget.EnrollmentInspection) { v.View.Configuration.Epoch++ }, func(v *storagebudget.EnrollmentInspection) { v.View.Configuration.Maintenance = true }, func(v *storagebudget.EnrollmentInspection) {
			v.View.Slots[r.Core.Token.Slot].PersistedApplied.Generation++
		}, func(v *storagebudget.EnrollmentInspection) {
			v.View.Slots[r.Core.Token.Slot].PersistedNextSequence = r.Core.Token.Sequence
		}, func(v *storagebudget.EnrollmentInspection) { v.CheckpointBank = 2 }} {
			bad := current
			mutate(&bad)
			if storageCreationRetiredQCurrent(r, bad, catalog) {
				t.Fatal("unproven retirement accepted", i)
			}
		}
		badCatalog := ownerCatalogClone(catalog)
		badCatalog.AfterDefinitionsDigest[0] ^= 1
		if storageCreationRetiredQCurrent(r, current, badCatalog) {
			t.Fatal("changed original definitions accepted")
		}
		q.Roots[0].Known = false
		bad := current
		bad.Checkpoints[0] = epQBytes(q, record)
		if storageCreationRetiredQCurrent(r, bad, catalog) {
			t.Fatal("unknown current root accepted")
		}
	}
}

// Partial handwritten read-side operands stop at full-page equality, before
// held/native/source access. This is not an installed qualification, live member
// or successful recovery fixture. In particular Private must reach this common
// custody barrier rather than be rejected by an obsolete kind discriminator.
func TestStorageEnrollmentRetiredCreationComparatorRequiresFullOriginalPage(t *testing.T) {
	for _, kind := range []byte{storageCreationHistory, storageCreationAncestor, storageCreationPrivate} {
		r, _, legacyPage := storageCreationRestartRecords(t, storageCreationFixture(t, kind), 0)
		owner := storageOwnerRecordV2{Config: r.Tail.Config, Anchor: r.Tail.Anchor}
		p := &storageEnrollmentEpoch{
			startup:   true,
			c:         &storageBudgetConstruction{ref: &storageInstallationRef{cell: &storageInstallation{ownerRecord: &owner}}},
			original:  &storageEnrollmentPeakOriginalComparison{Project: r.Tail.Project},
			qualified: &storageEnrollmentQualification{digest: r.Tail.Qualification},
		}
		catalog := storageOwnerCatalogV2{Installation: r.Tail.Installation, QBinding: r.Tail.QBinding}
		// Legacy obligations remain migration-only, even with their exact page.
		for _, legacyInput := range [][]byte{nil, legacyPage[:len(legacyPage)-1], append(append([]byte{}, legacyPage...), 0), legacyPage} {
			if source, err := p.compareCapturedCreation(t.Context(), r, legacyInput, catalog); source != nil || !errors.Is(err, errStorageEnrollmentPeakMigration) {
				t.Fatal("legacy retired kind bypassed migration refusal", kind, err)
			}
		}
		// Choose current semantics on handwritten operands before constructing
		// settlement evidence and the linked retired page; never relabel a page.
		estimate := storageCreationFixture(t, kind)
		estimate.CapacityPolicy = storageEnrollmentCapacityEstimatePolicy
		estimate.Core.CASBinding = estimate.planDigest()
		estimate.Core.Operation = "p:" + hex.EncodeToString(estimate.Core.CASBinding[:]) + ":create"
		r, _, page := storageCreationRestartRecords(t, estimate, 0)
		for _, badPage := range [][]byte{nil, page[:len(page)-1], append(append([]byte{}, page...), 0)} {
			source, err := p.compareCapturedCreation(t.Context(), r, badPage, catalog)
			if source != nil || !errors.Is(err, errStorageCustody) {
				t.Fatal("supported retired kind bypassed full original page comparison", kind, err)
			}
		}
		p.qualified = nil
		if source, err := p.compareCapturedCreation(t.Context(), r, page, catalog); source != nil || !errors.Is(err, errStorageEnrollmentUnresolved) {
			t.Fatal("full page supplied missing original qualification", kind, err)
		}
	}
}

// Handwritten retained-before operands isolate the equality that joins a
// selected retired member to an already validated published Scope. These
// partial Scope values are data operands, never decoded packets or authority.
func TestStorageEnrollmentPublishedCreationRetainsExactBeforeLineage(t *testing.T) {
	catalog, record, _ := ownerCatalogFixture()
	raw := make([]byte, 8388608)
	copy(raw, []byte("handwritten complete retained before bytes"))
	r := storageCreationFixture(t, storageCreationHistory)
	r.Tail.Installation = record.Installation
	r.Tail.QBinding = record.QBinding
	r.Tail.Catalogs = [2]storagebudget.Digest{sha256.Sum256(raw), sha256.Sum256(raw)}
	r, _, _ = storageCreationRestartRecords(t, r, 0)
	var current, prior storageEnrollmentPeakScope
	for i := 0; i < 8; i++ {
		epAtNative(current[1080+35*i:1080+35*(i+1)], r.Tail.Targets[i])
		epAtNative(prior[1080+35*i:1080+35*(i+1)], r.Tail.Targets[i])
		epSet64(current[1360+i*32+4:], r.Tail.Before[i])
		epSet64(prior[1360+i*32+12:], r.Tail.Before[i])
	}
	q := epQ0(catalog)
	q.Epoch = r.Core.RetirementEpoch
	for i := range catalog.Roots {
		q.Roots[i].Known = true
	}
	for i := range catalog.Workspaces {
		q.Workspaces[i].Known = true
	}
	q.Applied[r.Core.Token.Slot] = storageEnrollmentPeakQApplied{ID: storageEnrollmentPeakQID(r.Core.Token.ID), Sequence: r.Core.Token.Sequence, Generation: r.Core.Token.Generation, NextSequence: r.Core.Token.Sequence + 1}
	checkpoint := epQBytes(q, record)
	storageTestMust(t, storageEnrollmentCreationBeforeLineage(r, current, prior, raw, checkpoint))
	for _, name := range []string{"current-native", "prior-native", "current-before", "prior-final", "catalog-bytes", "missing-catalog", "checkpoint-bytes"} {
		t.Run(name, func(t *testing.T) {
			c, p := current, prior
			before, cp := append([]byte{}, raw...), append([]byte{}, checkpoint...)
			switch name {
			case "current-native":
				c[1080] ^= 1
			case "prior-native":
				p[1080] ^= 1
			case "current-before":
				c[1364] ^= 1
			case "prior-final":
				p[1372] ^= 1
			case "catalog-bytes":
				before[128] ^= 1
			case "missing-catalog":
				before = nil
			case "checkpoint-bytes":
				cp[128] ^= 1
			}
			if storageEnrollmentCreationBeforeLineage(r, c, p, before, cp) == nil {
				t.Fatal("changed retained native/extent/catalog/Q operand accepted")
			}
		})
	}
	for _, name := range []string{"maintenance", "epoch", "token", "generation", "sequence", "next", "unknown-root"} {
		t.Run(name, func(t *testing.T) {
			bad := *q
			switch name {
			case "maintenance":
				bad.Maintenance = true
			case "epoch":
				bad.Epoch++
			case "token":
				bad.Applied[r.Core.Token.Slot].ID[0] ^= 1
			case "generation":
				bad.Applied[r.Core.Token.Slot].Generation++
			case "sequence":
				bad.Applied[r.Core.Token.Slot].Sequence++
			case "next":
				bad.Applied[r.Core.Token.Slot].NextSequence = r.Core.Token.Sequence
			case "unknown-root":
				bad.Roots[0].Known = false
			}
			if storageEnrollmentCreationBeforeLineage(r, current, prior, raw, epQBytes(&bad, record)) == nil {
				t.Fatal("retained retirement checkpoint does not prove exact original tuple")
			}
		})
	}
	// A missing or unrelated default packet cannot gain authority from the
	// lower equality helper's success above.
	malformed := storageEnrollmentPeakEvidence{Board: storageEnrollmentPeakBoard{Source: storageEnrollmentPeakSource{Namespace: []byte("data"), Default: &storageEnrollmentDefaultSource{Workflow: 1, Branch: storageCreationConfigured}}}}
	if _, _, _, err := storageEnrollmentPublishedCreation(malformed, record, storageOwnerClaimV2{}); err == nil {
		t.Fatal("partial default source accepted without exact packet lineage")
	}
}

func TestStorageCreationLocalPrivateJ5ZeroTail(t *testing.T) {
	r := storageCreationFixture(t, storageCreationPrivate)
	installation, binding := r.Tail.Installation, r.Tail.QBinding
	r.Kind, r.Tail = storageCreationLocalPrivate, storageCreationTail{}
	r.Core.Target, r.Core.Stage = "dialect-drafts", "dialect-drafts"
	r.Parents[0].Role = 2
	r.Effects = []storageCreationEffect{{Parent: 0, Kind: 1, Name: "dialect-drafts"}, {Parent: 1, Kind: 2, Name: ".gitignore", Bytes: 2, Payload: sha256.Sum256([]byte{42, 10})}, {Parent: 1, Kind: 2, Name: "workflow.lock", Payload: sha256.Sum256(nil)}}
	r.Core.Components[0].Physical.Directories = 1
	r.Core.CASBinding = r.planDigest()
	r.Core.Operation = "p:" + hex.EncodeToString(r.Core.CASBinding[:]) + ":create"
	storageTestMust(t, r.validate())
	raw, err := storageEncodeCreationIntent(r, installation, binding)
	storageTestMust(t, err)
	if string(raw[:8]) != "DSKHINT5" || raw[8] != 5 || raw[9] != 0 || raw[13] != 2 || !storageAllZero(raw[14:16]) {
		t.Fatal("local header discriminator")
	}
	end := 128 + int(binary.LittleEndian.Uint32(raw[72:76]))
	if end > 12917 || !storageAllZero(raw[end-4096:end]) {
		t.Fatal("local tail is not exact zero4096")
	}
	decoded, err := storageDecodeCreationIntent(raw, int(r.Core.Slot), installation, binding)
	storageTestMust(t, err)
	if !reflect.DeepEqual(decoded, r) {
		t.Fatal("local codec lost original operands")
	}
	bad := r
	bad.Tail.Bridge = storagebudget.ID{1}
	if bad.validate() == nil {
		t.Fatal("local accepted fake enrollment tail")
	}
	bad = r
	bad.Parents = append([]storageCreationParent{}, r.Parents...)
	bad.Parents[0].Role = 1
	if bad.validate() == nil {
		t.Fatal("local accepted project parent")
	}
	bad = r
	bad.Effects = append([]storageCreationEffect{}, r.Effects...)
	bad.Effects[0].Name = ".desk-private"
	bad.Core.CASBinding = bad.planDigest()
	bad.Core.Operation = "p:" + hex.EncodeToString(bad.Core.CASBinding[:]) + ":create"
	if bad.validate() == nil {
		t.Fatal("local accepted private-root creation")
	}
	if _, err := storageDecodeIntent(raw, int(r.Core.Slot), installation, binding); err == nil {
		t.Fatal("old J2 reader accepted J5")
	}
}

func TestStorageCreationLocalOriginalHeaderBindingAndJ4TailSeparation(t *testing.T) {
	j4 := storageCreationFixture(t, storageCreationPrivate)
	binding := j4.Tail.QBinding
	original, err := storageCreationRequest(j4)
	storageTestMust(t, err)
	if original.Owner != binding || len(original.Entries) != 4 || original.Entries[0].Target != ".desk-private" || original.Entries[3].Target != ".desk-private/dialect-drafts/workflow.lock" {
		t.Fatal("J4 original tail/header request changed")
	}
	local := j4
	local.Kind = storageCreationLocalPrivate
	local.Tail = storageCreationTail{}
	local.Core.Target = "dialect-drafts"
	local.Core.Stage = "dialect-drafts"
	local.Core.Components = append([]storagebudget.Component{}, j4.Core.Components...)
	local.Core.Components[0].Physical.Directories = 1
	local.Parents = []storageCreationParent{{Native: storageIdentityIntentPO(200, 1), Role: 2}}
	local.Effects = []storageCreationEffect{{Parent: 0, Kind: 1, Name: "dialect-drafts"}, {Parent: 1, Kind: 2, Name: ".gitignore", Bytes: 2, Payload: sha256.Sum256([]byte{42, 10})}, {Parent: 1, Kind: 2, Name: "workflow.lock", Payload: sha256.Sum256(nil)}}
	local.Core.CASBinding = local.planDigest()
	local.Core.Operation = "p:" + hex.EncodeToString(local.Core.CASBinding[:]) + ":create"
	storageTestMust(t, local.validate())
	if _, err = storageCreationRequest(local); err == nil {
		t.Fatal("zero local tail supplied a Q owner")
	}
	request, err := storageCreationRequestBound(local, binding)
	storageTestMust(t, err)
	if request.Owner != binding || len(request.Entries) != 3 || request.Entries[0].Target != "dialect-drafts" || request.Entries[1].Target != "dialect-drafts/.gitignore" || request.Entries[2].Target != "dialect-drafts/workflow.lock" || request.Entries[1].Bytes != 2 || request.Entries[2].Files != 1 || request.Entries[2].Bytes != 0 {
		t.Fatal("local header-bound closed manifest differs")
	}
	if !reflect.DeepEqual(local.Tail, storageCreationTail{}) || !reflect.DeepEqual(j4.Tail, storageCreationFixture(t, storageCreationPrivate).Tail) {
		t.Fatal("local issue mutated or borrowed J4 tail")
	}
	if _, err = storageCreationRequestBound(j4, storageIdentityIntentHash(44)); err == nil {
		t.Fatal("J4 accepted a different original header binding")
	}
	// An existing draft keeps both exact parents but one original accounting
	// group. Its two file names still resolve relative to the enrolled private.
	local.Parents = append(local.Parents, storageCreationParent{Native: storageIdentityIntentPO(201, 1), Role: 3})
	local.Effects = []storageCreationEffect{{Parent: 1, Kind: 2, Name: ".gitignore", Bytes: 2, Payload: sha256.Sum256([]byte{42, 10})}, {Parent: 1, Kind: 2, Name: "workflow.lock", Payload: sha256.Sum256(nil)}}
	local.Core.Components[0].Physical.Directories = 0
	local.Core.CASBinding = local.planDigest()
	local.Core.Operation = "p:" + hex.EncodeToString(local.Core.CASBinding[:]) + ":create"
	request, err = storageCreationRequestBound(local, binding)
	storageTestMust(t, err)
	if len(request.Entries) != 2 || request.Entries[0].Target != "dialect-drafts/.gitignore" || request.Entries[1].Target != "dialect-drafts/workflow.lock" {
		t.Fatal("local existing-child entries lost original parent relation")
	}
	// A completed original page must join this exact persisted token/facts.
	local.Core.Generation = 2
	local.Core.PreviousPageDigest = storageIdentityIntentHash(33)
	local.Core.Phase = storageIntentSettling
	local.Core.PolicyRevision = 7
	local.Core.Token = storagebudget.Token{Slot: 0, ID: storageIdentityIntentID(50), Sequence: 9, Generation: 3}
	local.Core.PreviousToken = local.Core.Token
	local.Core.PreviousToken.Generation--
	for i := range local.Effects {
		local.Effects[i].State = storageCreationDurable
		local.Effects[i].Native = storageIdentityIntentPO(uint64(210+i), 2)
	}
	local, err = storageCreationSettlement(local)
	storageTestMust(t, err)
	storageTestMust(t, local.validate())
	ticket := storagebudget.Ticket{State: storagebudget.Staging, Token: local.Core.Token, PolicyRevision: 7, Workflow: local.Core.Workflow, Operation: local.Core.Operation, Owner: binding, Components: local.Core.Components}
	if !storageCreationOriginalQMapping(local, ticket, binding) || storageCreationTicketMatches(local, ticket) {
		t.Fatal("local mapping did not require the actual header owner")
	}
	ticket.State = storagebudget.Terminal
	ticket.Token.Generation++
	ticket.Evidence = local.Core.Evidence
	ticket.Changes = append([]storagebudget.Change{}, local.Core.Changes...)
	if !storageCreationOriginalQMapping(local, ticket, binding) {
		t.Fatal("same original factual terminal continuation refused")
	}
	bad := ticket
	bad.Evidence[0] ^= 1
	if storageCreationOriginalQMapping(local, bad, binding) {
		t.Fatal("different terminal fact joined")
	}
	bad = ticket
	bad.Token.Sequence++
	if storageCreationOriginalQMapping(local, bad, binding) {
		t.Fatal("newer protocol slot substituted for original token")
	}
}

func TestStorageCreationCapacityEstimatePagesKeepOriginalObligations(t *testing.T) {
	for _, local := range []bool{false, true} {
		r := storageCreationFixture(t, storageCreationPrivate)
		installation, binding := r.Tail.Installation, r.Tail.QBinding
		if local {
			r.Kind, r.Tail = storageCreationLocalPrivate, storageCreationTail{}
			r.Core.Target, r.Core.Stage = "dialect-drafts", "dialect-drafts"
			r.Parents[0].Role = 2
			r.Effects = []storageCreationEffect{{Parent: 0, Kind: 1, Name: "dialect-drafts"}, {Parent: 1, Kind: 2, Name: ".gitignore", Bytes: 2, Payload: sha256.Sum256([]byte{42, 10})}, {Parent: 1, Kind: 2, Name: "workflow.lock", Payload: sha256.Sum256(nil)}}
			r.Core.Components[0].Physical.Directories = 1
			r.Core.CASBinding = r.planDigest()
			r.Core.Operation = "p:" + hex.EncodeToString(r.Core.CASBinding[:]) + ":create"
		}
		oldPage, err := storageEncodeCreationIntent(r, installation, binding)
		storageTestMust(t, err)
		old, err := storageDecodeCreationIntent(oldPage, int(r.Core.Slot), installation, binding)
		storageTestMust(t, err)
		if !errors.Is(old.requireCapacityEstimate(), errStorageEnrollmentPeakMigration) {
			t.Fatal("old tail/local obligation silently converted")
		}
		debt := old.Tail
		estimate := old
		estimate.CapacityPolicy = 2
		estimate.Core.CASBinding = estimate.planDigest()
		estimate.Core.Operation = "p:" + hex.EncodeToString(estimate.Core.CASBinding[:]) + ":create"
		page, err := storageEncodeCreationIntent(estimate, installation, binding)
		storageTestMust(t, err)
		magic := "DSKHINT6"
		if local {
			magic = "DSKHINT7"
		}
		if string(page[:8]) != magic || page[13] != 2 || bytes.Equal(page, oldPage) || estimate.Core.CASBinding == old.Core.CASBinding {
			t.Fatal("estimate page/plan identity confused")
		}
		got, err := storageDecodeCreationIntent(page, int(r.Core.Slot), installation, binding)
		storageTestMust(t, err)
		if !reflect.DeepEqual(got, estimate) || got.requireCapacityEstimate() != nil || !reflect.DeepEqual(old.Tail, debt) {
			t.Fatal("creation operands or old obligations lost")
		}
		again, err := storageEncodeCreationIntent(old, installation, binding)
		storageTestMust(t, err)
		if !bytes.Equal(again, oldPage) {
			t.Fatal("legacy inspection rewrote original bytes")
		}
		selected, err := storageDecodeCreationHostPage(page, int(r.Core.Slot), installation, binding)
		storageTestMust(t, err)
		if selected.Creation == nil || !reflect.DeepEqual(*selected.Creation, estimate) {
			t.Fatal("shared host reader dropped estimate identity")
		}
		// Relabeling the old page cannot change the old plan commitment.
		relabeled := append([]byte{}, oldPage...)
		copy(relabeled[:16], page[:16])
		clear(relabeled[76:108])
		sum := sha256.Sum256(relabeled)
		copy(relabeled[76:108], sum[:])
		if _, err := storageDecodeCreationIntent(relabeled, int(r.Core.Slot), installation, binding); err == nil {
			t.Fatal("old body accepted under estimate header")
		}
	}
}
