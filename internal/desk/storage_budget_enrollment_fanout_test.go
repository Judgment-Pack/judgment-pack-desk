package desk

import (
	"crypto/sha256"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageEnrollmentTicketDTOCarriesFlatAndFanoutExactly(t *testing.T) {
	flat := storagebudget.Ticket{Token: storagebudget.Token{Slot: 7, ID: storagebudget.ID{1}, Sequence: 3, Generation: 5}, State: storagebudget.Uncertain, PolicyRevision: 9, Workflow: "test-original", Operation: "test-plan", Owner: storagebudget.Digest{2}, Manifest: storagebudget.Digest{3}, ManifestLength: 524288, Components: []storagebudget.Component{{Root: storagebudget.ID{4}, RootGeneration: 1, Workspace: storagebudget.ID{5}, WorkspaceGeneration: 2, Category: storagebudget.Memory, Physical: storagebudget.Vector{Bytes: 6}, Attribution: storagebudget.Vector{Bytes: 7}}}, Evidence: storagebudget.Digest{8}}
	for _, fanout := range []bool{false, true} {
		t.Run(map[bool]string{false: "flat", true: "fanout"}[fanout], func(t *testing.T) {
			original := flat
			original.Components = append([]storagebudget.Component(nil), flat.Components...)
			if fanout {
				original.Components[0].Workspace, original.Components[0].WorkspaceGeneration = storagebudget.ID{}, 0
				original.Fanout = &storagebudget.WorkspaceFanout{Workspaces: []storagebudget.WorkspaceReference{{ID: storagebudget.ID{5}, Generation: 2}, {ID: storagebudget.ID{6}, Generation: 4}}, Memberships: []storagebudget.WorkspaceMembership{{3}}}
			}
			converted := storageEnrollmentPeakQTicketValue(original)
			if converted.State != storageEnrollmentPeakQUncertain || converted.Token.Slot != 7 || converted.PolicyRevision != 9 || converted.Operation != "test-plan" || len(converted.Components) != 1 || converted.Components[0].Attribution.Bytes != 7 {
				t.Fatal("actual conversion silently yielded a zero/partial ticket")
			}
			var roundtrip storagebudget.Ticket
			if !storageEnrollmentPeakCopy(reflect.ValueOf(&roundtrip).Elem(), reflect.ValueOf(converted)) || !reflect.DeepEqual(original, roundtrip) {
				t.Fatal("actual DTO conversion lost exact membership or metadata")
			}
			if fanout {
				converted.Fanout.Workspaces[0].Generation = 99
				converted.Fanout.Memberships[0][0] = 0
				if original.Fanout.Workspaces[0].Generation != 2 || original.Fanout.Memberships[0][0] != 3 || roundtrip.Fanout.Workspaces[0].Generation != 2 {
					t.Fatal("conversion aliases caller or roundtrip membership")
				}
			}
		})
	}
}

func storageEnrollmentFanoutTicketVector(count int) (storageEnrollmentPeakQTicket, *storageEnrollmentPeakQCheckpoint) {
	component := storageEnrollmentPeakQComponent{Root: storageEnrollmentPeakQID{2}, RootGeneration: 1, Owner: storageEnrollmentPeakQDigest{3}, Filesystem: storageEnrollmentPeakQID{1}, FilesystemGeneration: 1, Category: 1, Physical: storageEnrollmentPeakQVector{Bytes: 5, Files: 1}, Attribution: storageEnrollmentPeakQVector{Bytes: 5, Files: 1}, CapacityCount: 1, CapacityBinding: storageEnrollmentPeakQDigest{4}, Capacity: [4]storageEnrollmentPeakQCapacityPromise{{Constraint: storageEnrollmentPeakQID{4}, Generation: 1, Peak: storageEnrollmentPeakQCapacityVector{Bytes: 5, Inodes: 1}}}}
	t := storageEnrollmentPeakQTicket{Token: storageEnrollmentPeakQToken{Slot: 0, ID: storageEnrollmentPeakQID{7}, Sequence: 1, Generation: 3}, State: storageEnrollmentPeakQTerminal, PolicyRevision: 1, Workflow: "handwritten-test-only", Operation: "exact-closed-plan", Owner: storageEnrollmentPeakQDigest{3}, Manifest: storageEnrollmentPeakQDigest{8}, ManifestLength: 524288, Components: []storageEnrollmentPeakQComponent{component}, Changes: []storageEnrollmentPeakQChange{{PhysicalAdd: storageEnrollmentPeakQVector{Bytes: 5, Files: 1}, AttributionAdd: storageEnrollmentPeakQVector{Bytes: 5, Files: 1}}}, Evidence: storageEnrollmentPeakQDigest{9}, Fanout: &storageEnrollmentPeakQWorkspaceFanout{Memberships: make([]storageEnrollmentPeakQWorkspaceMembership, 1)}}
	cp := &storageEnrollmentPeakQCheckpoint{}
	cp.Applied[0].NextSequence = 1
	cp.Roots[0] = storageEnrollmentPeakQRoot{ID: component.Root, Owner: component.Owner, Filesystem: component.Filesystem, Generation: 1, CapacityCount: 1, CapacityBinding: component.CapacityBinding, CapacityRefs: [4]storageEnrollmentPeakQCapacityRef{{Constraint: storageEnrollmentPeakQID{4}, Generation: 1}}}
	cp.Filesystems[0] = storageEnrollmentPeakQFilesystem{ID: component.Filesystem, Generation: 1}
	cp.CapacityConstraints[0] = storageEnrollmentPeakQCapacityConstraint{ID: storageEnrollmentPeakQID{4}, Generation: 1, Resources: 3}
	for i := 0; i < count; i++ {
		id := storageEnrollmentPeakQID{byte(i/256 + 1), byte(i % 256)}
		t.Fanout.Workspaces = append(t.Fanout.Workspaces, storageEnrollmentPeakQWorkspaceReference{ID: id, Generation: uint64(i + 1)})
		if i < 1024 {
			t.Fanout.Memberships[0][i/8] |= 1 << uint(i%8)
			cp.Workspaces[i] = storageEnrollmentPeakQWorkspace{ID: id, Generation: uint64(i + 1)}
		}
	}
	return t, cp
}

func TestStorageEnrollmentIndependentFanoutExactAccountingAndLimits(t *testing.T) {
	ticket, cp := storageEnrollmentFanoutTicketVector(1024)
	storageTestMust(t, storageEnrollmentPeakQValidateTicket(ticket))
	storageTestMust(t, storageEnrollmentPeakQRegistered(cp, ticket, false))
	old := storageEnrollmentPeakQCloneTicket(ticket)
	old.State = storageEnrollmentPeakQReserved
	old.Changes = nil
	old.Token.Generation = 1
	next := storageEnrollmentPeakQCloneTicket(old)
	next.State = storageEnrollmentPeakQStaging
	next.Token.Generation = 2
	storageTestMust(t, storageEnrollmentPeakQLegalTransition(old, next, cp.Applied[0]))
	next.Fanout.Memberships[0][0] ^= 1
	if storageEnrollmentPeakQLegalTransition(old, next, cp.Applied[0]) == nil {
		t.Fatal("transition changed exact membership")
	}
	storageTestMust(t, storageEnrollmentPeakQTerminalApply(cp, ticket))
	if cp.Installation[1] != (storageEnrollmentPeakQVector{Bytes: 5, Files: 1}) || cp.Roots[0].Usage[1] != (storageEnrollmentPeakQVector{Bytes: 5, Files: 1}) {
		t.Fatal("physical charge repeated by member")
	}
	for _, w := range cp.Workspaces {
		if w.Usage[1] != (storageEnrollmentPeakQVector{Bytes: 5, Files: 1}) {
			t.Fatal("member attribution collapsed")
		}
	}
	before := *cp
	storageTestMust(t, storageEnrollmentPeakQTerminalApply(cp, ticket))
	if !reflect.DeepEqual(before, *cp) {
		t.Fatal("terminal reapplied")
	}
	for _, mutation := range []string{"1025", "unused", "duplicate", "membership", "generation", "unhydrated"} {
		t.Run(mutation, func(t *testing.T) {
			bad, c := storageEnrollmentFanoutTicketVector(1024)
			switch mutation {
			case "1025":
				bad, c = storageEnrollmentFanoutTicketVector(1025)
			case "unused":
				bad.Fanout.Memberships[0][0] &^= 1
			case "duplicate":
				bad.Fanout.Workspaces[1] = bad.Fanout.Workspaces[0]
			case "membership":
				bad.Fanout.Memberships = nil
			case "generation":
				bad.Fanout.Workspaces[0].Generation++
			case "unhydrated":
				bad.Fanout = &storageEnrollmentPeakQWorkspaceFanout{}
			}
			if storageEnrollmentPeakQValidateTicket(bad) == nil && storageEnrollmentPeakQRegistered(c, bad, false) == nil {
				t.Fatal("incorrect full membership accepted")
			}
		})
	}
}

func TestStorageEnrollmentIndependentFanoutManifestAndRetiredMarker(t *testing.T) {
	ticket, _ := storageEnrollmentFanoutTicketVector(1024)
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID{1}, Binding: storageEnrollmentPeakQDigest{2}}
	manifest, err := storageEnrollmentPeakQEncodeFanoutManifest(ticket, anchor)
	storageTestMust(t, err)
	ticket.Manifest = storageEnrollmentPeakQDigest(sha256.Sum256(manifest))
	parsed, err := storageEnrollmentPeakQDecodeFanoutManifest(append([]byte(nil), manifest...), ticket, anchor)
	storageTestMust(t, err)
	if !storageEnrollmentPeakQSameFanout(ticket.Fanout, parsed.Fanout) {
		t.Fatal("canonical manifest lost full set")
	}
	for _, offset := range []int{9, 128 + 16384 + 2, 128 + 16384 + 8, 128 + 16384 + 16 + 24576 + 128, 524287} {
		bad := append([]byte(nil), manifest...)
		bad[offset] ^= 1
		storageEnrollmentPeakQSeal(bad, 3, uint16(ticket.Token.Slot), ticket.Token.Sequence, anchor)
		changed := ticket
		changed.Manifest = storageEnrollmentPeakQDigest(sha256.Sum256(bad))
		if _, err = storageEnrollmentPeakQDecodeFanoutManifest(bad, changed, anchor); err == nil {
			t.Fatal("noncanonical manifest accepted", offset)
		}
	}
	marker := storageEnrollmentPeakQCloneTicket(ticket)
	marker.Fanout = &storageEnrollmentPeakQWorkspaceFanout{}
	if storageEnrollmentPeakQValidateTicket(marker) == nil {
		t.Fatal("retired marker became live membership")
	}
	page, err := storageEnrollmentPeakQEncodeTicket(marker, anchor)
	storageTestMust(t, err)
	if _, err = storageEnrollmentPeakQUnseal(append([]byte(nil), page...), 2, 0, anchor); err == nil {
		t.Fatal("legacy strict page parser accepted fanout")
	}
	decoded, err := storageEnrollmentPeakQDecodeTicket(page, 0, anchor)
	storageTestMust(t, err)
	if decoded.Fanout == nil || len(decoded.Fanout.Workspaces) != 0 {
		t.Fatal("page guessed retired membership")
	}
	var dto storagebudget.Ticket
	if !storageEnrollmentPeakCopy(reflect.ValueOf(&dto).Elem(), reflect.ValueOf(marker)) {
		t.Fatal("marker DTO copy failed")
	}
	body, err := storageEnrollmentPeakWriteTicket(dto)
	storageTestMust(t, err)
	roundtrip, err := storageEnrollmentPeakReadTicket(body)
	storageTestMust(t, err)
	if !reflect.DeepEqual(dto, roundtrip) || len(body) > 8244 {
		t.Fatal("retired serialized view changed bounds/identity")
	}
	dto.State = storagebudget.Uncertain
	if _, err = storageEnrollmentPeakWriteTicket(dto); err == nil {
		t.Fatal("unhydrated live dictionary serialized as terminal authority")
	}
}

func TestStorageOrdinaryFanoutCannotBecomeIdentityOrCreation(t *testing.T) {
	identity := storageIdentityIntentFixture()
	storageTestMust(t, identity.validate())
	identity.Core.Fanout = &storageIntentFanout{Binding: storagebudget.Digest{1}}
	if identity.validate() == nil {
		t.Fatal("ordinary reference binding became identity authority")
	}
	creation := storageCreationFixture(t, storageCreationHistory)
	storageTestMust(t, creation.validate())
	creation.Core.Fanout = &storageIntentFanout{Binding: storagebudget.Digest{1}}
	if creation.validate() == nil {
		t.Fatal("ordinary reference binding entered creation format")
	}
}

func TestStorageEnrollmentIndependentLookupFixedArrayBoundaries(t *testing.T) {
	c := &storageEnrollmentPeakQCheckpoint{}
	first, last, missing := storageEnrollmentPeakQID{1}, storageEnrollmentPeakQID{2}, storageEnrollmentPeakQID{3}
	c.Roots[0].ID = first
	c.Roots[storageEnrollmentPeakQMaxRoots-1].ID = last
	c.Workspaces[0].ID = first
	c.Workspaces[storageEnrollmentPeakQMaxWorkspaces-1].ID = last
	c.Filesystems[0].ID = first
	c.Filesystems[storageEnrollmentPeakQMaxFilesystems-1].ID = last
	c.CapacityConstraints[0].ID = first
	c.CapacityConstraints[storageEnrollmentPeakQMaxCapacityConstraints-1].ID = last
	for _, row := range []struct {
		name   string
		size   int
		lookup func(*storageEnrollmentPeakQCheckpoint, storageEnrollmentPeakQID) int
	}{
		{"root", storageEnrollmentPeakQMaxRoots, storageEnrollmentPeakQRootIndex}, {"workspace", storageEnrollmentPeakQMaxWorkspaces, storageEnrollmentPeakQWorkspaceIndex}, {"filesystem", storageEnrollmentPeakQMaxFilesystems, storageEnrollmentPeakQFilesystemIndex}, {"constraint", storageEnrollmentPeakQMaxCapacityConstraints, storageEnrollmentPeakQConstraintIndex},
	} {
		t.Run(row.name, func(t *testing.T) {
			if row.lookup(c, first) != 0 || row.lookup(c, last) != row.size-1 || row.lookup(c, missing) != -1 || row.lookup(c, storageEnrollmentPeakQID{}) != -1 {
				t.Fatal("independent boundary/missing/zero lookup changed")
			}
			if row.lookup(&storageEnrollmentPeakQCheckpoint{}, first) != -1 {
				t.Fatal("empty table supplies a definition")
			}
		})
	}
}
