package storagebudget

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// These are handwritten coordinator operands, never host/native qualification
// records. They do not supply custody, a complete transfer plan or activation.
func fanoutFixtureRequest(physical, attribution uint64) Request {
	r := request(physical)
	r.Workflow, r.Operation, r.Entries = "test-fanout", "original-plan-1", nil
	r.Components[0].Workspace, r.Components[0].WorkspaceGeneration = ID{}, 0
	r.Components[0].Attribution = Vector{attribution, 1, 0}
	r.Fanout = &WorkspaceFanout{Workspaces: []WorkspaceReference{{ident(10), 1}, {ident(11), 1}}, Memberships: []WorkspaceMembership{{3}}}
	return r
}

func fanoutCodecTicket(workspaces, groups int) Ticket {
	r := fanoutFixtureRequest(1, 1)
	t := Ticket{Token: Token{Slot: 0, ID: ident(99), Sequence: 1, Generation: 1}, State: Reserved, PolicyRevision: 3, Workflow: r.Workflow, Operation: r.Operation, Owner: owner, Fanout: &WorkspaceFanout{Memberships: make([]WorkspaceMembership, groups)}}
	for i := 0; i < workspaces; i++ {
		id := ID{128, byte(i >> 8), byte(i)}
		t.Fanout.Workspaces = append(t.Fanout.Workspaces, WorkspaceReference{id, 1})
		if i < MaxWorkspaces {
			for g := range t.Fanout.Memberships {
				t.Fanout.Memberships[g][i/8] |= 1 << uint(i%8)
			}
		}
	}
	for i := 0; i < groups; i++ {
		c := r.Components[0]
		c.Root = ident(byte(i + 2))
		t.Components = append(t.Components, c)
	}
	return t
}

func TestFanoutCodecBoundariesAndStrictVectors(t *testing.T) {
	a := HeldAnchor{Installation: ident(30), Binding: digest(30)}
	full := fanoutCodecTicket(1024, 16)
	m, err := encodeManifest(full, nil, a)
	must(t, err)
	if len(m) != 524288 || len(m[43152:]) != 481136 || !allZero(m[43152:]) {
		t.Fatal("fixed manifest or joint bound changed")
	}
	got, entries, err := decodeManifest(m, full, a)
	must(t, err)
	if len(entries) != 0 || !reflect.DeepEqual(got.Fanout, full.Fanout) || !sameComponents(got.Components, full.Components) {
		t.Fatal("full exact dictionary/matrix lost")
	}
	page, err := encodeTicket(full, a)
	must(t, err)
	if len(page) != 16384 || page[9] != 1 || m[9] != 1 {
		t.Fatal("variant or ticket length lost")
	}
	if _, err = unseal(page, 2, 0, a); err == nil {
		t.Fatal("old strict ticket parser accepted fanout")
	}
	if _, err = unseal(m, 3, 0, a); err == nil {
		t.Fatal("old strict manifest parser accepted fanout")
	}
	decoded, err := decodeTicket(page, 0, a)
	must(t, err)
	if decoded.Fanout == nil || len(decoded.Fanout.Workspaces) != 0 || len(decoded.Fanout.Memberships) != 0 {
		t.Fatal("ticket page fabricated manifest membership")
	}
	if err = validateTicket(decoded); err == nil {
		t.Fatal("page-only marker treated as a complete live envelope")
	}
	for _, row := range []struct {
		name   string
		change func(*Ticket)
	}{
		{"1025-workspaces", func(x *Ticket) { *x = fanoutCodecTicket(1025, 1) }},
		{"17-groups", func(x *Ticket) { *x = fanoutCodecTicket(1, 17) }},
		{"dictionary-duplicate", func(x *Ticket) { x.Fanout.Workspaces[1] = x.Fanout.Workspaces[0] }},
		{"dictionary-order", func(x *Ticket) {
			x.Fanout.Workspaces[0], x.Fanout.Workspaces[1] = x.Fanout.Workspaces[1], x.Fanout.Workspaces[0]
		}},
		{"zero-generation", func(x *Ticket) { x.Fanout.Workspaces[0].Generation = 0 }},
		{"zero-id", func(x *Ticket) { x.Fanout.Workspaces[0].ID = ID{} }},
		{"missing-group", func(x *Ticket) { x.Fanout.Memberships = nil }},
		{"unused-dictionary-member", func(x *Ticket) { x.Fanout.Memberships[0][0] = 1 }},
		{"outside-dictionary", func(x *Ticket) { x.Fanout.Memberships[0][127] = 128 }},
		{"flat-workspace-in-group", func(x *Ticket) { x.Components[0].Workspace, x.Components[0].WorkspaceGeneration = ident(10), 1 }},
		{"attribution-without-members", func(x *Ticket) { x.Fanout.Workspaces = nil; x.Fanout.Memberships[0] = WorkspaceMembership{} }},
		{"members-without-attribution", func(x *Ticket) { x.Components[0].Attribution = Vector{} }},
		{"overlapping-expanded-key", func(x *Ticket) {
			x.Components = append(x.Components, x.Components[0])
			x.Fanout.Memberships = append(x.Fanout.Memberships, x.Fanout.Memberships[0])
		}},
	} {
		t.Run(row.name, func(t *testing.T) {
			x := fanoutCodecTicket(2, 1)
			row.change(&x)
			if _, err := encodeManifest(x, nil, a); err == nil {
				t.Fatal("invalid operand encoded")
			}
		})
	}
	small := fanoutCodecTicket(2, 1)
	m, err = encodeManifest(small, nil, a)
	must(t, err)
	start := pageHeader + TicketBytes
	for _, row := range []struct {
		name  string
		at    int
		value byte
	}{
		{"oversized-dictionary-count", start + 1, 5},
		{"group-count", start + 2, 2},
		{"unknown-version", start + 3, 2},
		{"object-entry-lane", start + 4, 1},
		{"bitset-width", start + 6, 1},
		{"reserved-header", start + 8, 1},
		{"unused-membership-row", start + 16 + 2*24 + 128, 1},
		{"outside-dictionary-bit", start + 16 + 2*24 + 127, 128},
		{"nonzero-tail", len(m) - 1, 1},
		{"unknown-variant", 9, 2},
	} {
		t.Run(row.name, func(t *testing.T) {
			b := append([]byte(nil), m...)
			b[row.at] = row.value
			seal(b, 3, 0, 1, a)
			if _, _, err := decodeManifest(b, small, a); err == nil {
				t.Fatal("noncanonical manifest accepted")
			}
		})
	}
	if _, err = encodeManifest(small, request(1).Entries, a); err == nil {
		t.Fatal("fanout admitted an unbounded second object-plan lane")
	}
	// Physical-only fanout groups need neither a fabricated workspace nor a
	// zero-valued attribution row. Terminal removal cannot invent a member.
	physical := fanoutCodecTicket(0, 1)
	physical.Components[0].Attribution = Vector{}
	_, err = encodeManifest(physical, nil, a)
	must(t, err)
	physical.State, physical.Evidence = Terminal, digest(77)
	physical.Changes = []Change{{AttributionRemove: Vector{1, 0, 0}}}
	if err = validateTicket(physical); err == nil {
		t.Fatal("unattributed group removed workspace usage")
	}
}

func TestFanoutAdmissionExactWorkspaceStatusSettlementAndDetachment(t *testing.T) {
	q, path := fixture(t)
	r := fanoutFixtureRequest(3, 4)
	r.Components[0].SettlementPhysical = Vector{2, 0, 0}
	r.Components[0].SettlementAttribution = Vector{1, 0, 0}
	r.Components[0] = fixtureComponent(r.Components[0], fixtureRegistry().Roots[1])
	original := r
	original.Components, original.Fanout = append([]Component(nil), r.Components...), cloneFanout(r.Fanout)
	token, err := q.Reserve(t.Context(), r, observations())
	must(t, err)
	// Caller-owned slices cannot alter the durable or returned membership.
	r.Fanout.Workspaces[0].Generation = 999
	r.Fanout.Memberships[0][0] = 0
	r.Components[0].Attribution.Bytes = 999
	manifest, err := q.Manifest(t.Context(), token, owner)
	must(t, err)
	if !reflect.DeepEqual(manifest, original) {
		t.Fatal("caller mutation changed exact admitted request")
	}
	manifest.Fanout.Workspaces[0].ID = ident(99)
	manifest.Fanout.Memberships[0][0] = 1
	host, err := q.InspectHost(t.Context())
	must(t, err)
	host.Slots[token.Slot].Ticket.Fanout.Memberships[0][0] = 0
	host.Slots[token.Slot].Ticket.Fanout.Workspaces[0].Generation = 2
	action, err := q.InspectAction(t.Context())
	must(t, err)
	if !reflect.DeepEqual(action.View.Slots[token.Slot].Ticket.Fanout, original.Fanout) {
		t.Fatal("host/manifest aliases loaded membership")
	}
	action.View.Slots[token.Slot].Ticket.Fanout.Workspaces[0].Generation = 99
	must(t, q.locked(t.Context(), func() error {
		l, err := q.load()
		if err != nil {
			return err
		}
		view := hostInspection(l)
		view.Slots[token.Slot].Ticket.Fanout.Memberships[0][0] = 0
		view.Slots[token.Slot].Ticket.Fanout.Workspaces[0].Generation = 99
		if !sameFanout(l.tickets[token.Slot].Fanout, original.Fanout) {
			return errors.New("detached projection aliases the same loaded ticket")
		}
		return nil
	}))
	s, err := q.StatusSnapshot(t.Context())
	must(t, err)
	if s.Promises.Peak[History].Bytes != 3 || s.Promises.Settlement[History].Bytes != 2 || s.CapacityPromised[0].Peak.Bytes != 5 {
		t.Fatal("physical or capacity promise fanned out")
	}
	for _, w := range s.Workspaces {
		if w.Promises.Peak[History].Bytes != 4 || w.Promises.Settlement[History].Bytes != 1 {
			t.Fatal("workspace attribution lost or averaged")
		}
	}
	// Second request fits installation but violates EACH exact workspace peak.
	_, err = q.Reserve(t.Context(), fanoutFixtureRequest(1, 6), observations())
	if !errors.Is(err, ErrLimit) {
		t.Fatal("shared workspace limit bypassed", err)
	}
	token, err = q.Advance(t.Context(), token, owner, Staging, digest(41))
	must(t, err)
	fact := SettlementFact{Token: token, Owner: owner, Evidence: digest(42), Changes: []Change{{PhysicalAdd: Vector{3, 1, 0}, AttributionAdd: Vector{4, 1, 0}}}}
	terminal, err := q.Settle(t.Context(), fact)
	must(t, err)
	again, err := q.Settle(t.Context(), fact)
	must(t, err)
	if again != terminal {
		t.Fatal("same fact changed token")
	}
	read, root, witness := openFixture(t, path)
	defer read.Close()
	defer root.Close()
	defer witness.Close()
	s, err = read.StatusSnapshot(t.Context())
	must(t, err)
	if s.Installation[History].Bytes != 3 || s.Tickets.TerminalUnapplied != 1 {
		t.Fatal("terminal physical application doubled")
	}
	for _, w := range s.Workspaces {
		if w.Usage[History].Bytes != 4 {
			t.Fatal("restart lost independent workspace delta")
		}
	}
	must(t, q.Checkpoint(t.Context()))
	state, err := q.InspectHostState(t.Context())
	must(t, err)
	row := state.Slots[terminal.Slot]
	if row.Ticket.Fanout == nil || len(row.Ticket.Fanout.Memberships) != 0 {
		t.Fatal("retired manifest was dereferenced")
	}
	initial := token
	initial.Generation--
	must(t, q.ValidateAppliedRequest(t.Context(), state.Epoch, row, initial, original))
	bad := original
	bad.Fanout = cloneFanout(original.Fanout)
	bad.Fanout.Workspaces[0].Generation++
	if err = q.ValidateAppliedRequest(t.Context(), state.Epoch, row, initial, bad); err == nil {
		t.Fatal("applied proof accepted changed membership generation")
	}
	_, _, err = q.CaptureExclusiveCensus(t.Context())
	must(t, err)
	_, err = q.InspectEnrollment(t.Context())
	must(t, err)
	removal, err := q.Reserve(t.Context(), fanoutFixtureRequest(1, 1), observations())
	must(t, err)
	_, err = q.Settle(t.Context(), SettlementFact{Token: removal, Owner: owner, Evidence: digest(43), Changes: []Change{{PhysicalRemove: Vector{2, 0, 0}, AttributionRemove: Vector{3, 0, 0}}}})
	must(t, err)
	s, err = q.StatusSnapshot(t.Context())
	must(t, err)
	if s.Installation[History].Bytes != 1 {
		t.Fatal("shared physical removal applied more than once")
	}
	for _, w := range s.Workspaces {
		if w.Usage[History].Bytes != 1 {
			t.Fatal("exact per-member removal lost")
		}
	}
}

func TestFanoutGenerationMembershipAndWholeAdmissionRefusals(t *testing.T) {
	for _, row := range []struct {
		name   string
		change func(*Request)
	}{
		{"stale-workspace-generation", func(r *Request) { r.Fanout.Workspaces[1].Generation = 2 }},
		{"unknown-workspace", func(r *Request) { r.Fanout.Workspaces[1].ID = ident(12) }},
		{"changed-root-binding", func(r *Request) { r.Components[0].CapacityBinding = digest(98) }},
		{"missing-membership", func(r *Request) { r.Fanout.Memberships[0][0] = 1 }},
		{"separate-control-rejects-whole", func(r *Request) {
			c := request(1).Components[0]
			c.Root = ident(98)
			c.Workspace, c.WorkspaceGeneration, c.Attribution = ID{}, 0, Vector{}
			c.Category = Control
			r.Components = append(r.Components, c)
			r.Fanout.Memberships = append(r.Fanout.Memberships, WorkspaceMembership{})
		}},
	} {
		t.Run(row.name, func(t *testing.T) {
			q, path := fixture(t)
			before := configurationTestHashes(t, path)
			r := fanoutFixtureRequest(1, 1)
			row.change(&r)
			if _, err := q.Reserve(t.Context(), r, observations()); err == nil {
				t.Fatal("invalid complete admission returned token")
			}
			if !reflect.DeepEqual(before, configurationTestHashes(t, path)) {
				t.Fatal("partial-root reservation persisted")
			}
		})
	}
}

func TestFanoutFlatObligationsAndOldReaderCuts(t *testing.T) {
	q, path := fixture(t)
	var flat [3]Token
	for i := range flat {
		var err error
		flat[i], err = q.Reserve(t.Context(), request(1), observations())
		must(t, err)
	}
	var err error
	flat[1], err = q.Advance(t.Context(), flat[1], owner, Uncertain, digest(40))
	must(t, err)
	flat[2], err = q.Settle(t.Context(), SettlementFact{Token: flat[2], Owner: owner, Evidence: digest(41), Changes: []Change{{}}})
	must(t, err)
	a, err := os.ReadFile(filepath.Join(path, "tickets.a"))
	must(t, err)
	b, err := os.ReadFile(filepath.Join(path, "tickets.b"))
	must(t, err)
	_, err = q.Reserve(t.Context(), fanoutFixtureRequest(1, 1), observations())
	must(t, err)
	afterA, err := os.ReadFile(filepath.Join(path, "tickets.a"))
	must(t, err)
	afterB, err := os.ReadFile(filepath.Join(path, "tickets.b"))
	must(t, err)
	if !bytes.Equal(a[:3*TicketBytes], afterA[:3*TicketBytes]) || !bytes.Equal(b[:3*TicketBytes], afterB[:3*TicketBytes]) {
		t.Fatal("new variant rewrote old live/uncertain/terminal pages")
	}
	state, err := q.InspectHostState(t.Context())
	must(t, err)
	// Handwritten original JSON field shapes: the lineage consumer hashes this
	// exact byte form. This expected type intentionally has no Fanout member.
	type originalFlatTicket struct {
		Token               Token
		State               State
		PolicyRevision      uint64
		Workflow, Operation string
		Owner               Digest
		Manifest            Digest
		ManifestLength      uint32
		Components          []Component
		Changes             []Change
		Evidence            Digest
	}
	for _, i := range []int{0, 1, 2} {
		x := state.Slots[i].Ticket
		expected, err := json.Marshal(originalFlatTicket{x.Token, x.State, x.PolicyRevision, x.Workflow, x.Operation, x.Owner, x.Manifest, x.ManifestLength, x.Components, x.Changes, x.Evidence})
		must(t, err)
		body, err := json.Marshal(x)
		must(t, err)
		if !bytes.Equal(body, expected) {
			t.Fatal("new member changed old flat JSON lineage bytes")
		}
	}
	type originalFlatRequest struct {
		Workflow, Operation string
		Owner               Digest
		Components          []Component
		Entries             []Entry
	}
	x := request(1)
	expected, err := json.Marshal(originalFlatRequest{x.Workflow, x.Operation, x.Owner, x.Components, x.Entries})
	must(t, err)
	body, err := json.Marshal(x)
	must(t, err)
	if !bytes.Equal(body, expected) {
		t.Fatal("new member changed old flat request JSON bytes")
	}
	for i, tok := range flat {
		if state.Slots[i].Ticket.Token != tok || state.Slots[i].Ticket.Fanout != nil {
			t.Fatal("old exact token or flat interpretation lost")
		}
	}
	// This is the exact OLD parser's reserved-byte gate, exercised across BOTH
	// banks before any old-bank selection or retirement skip is possible.
	oldRejected := false
	for _, bank := range [][]byte{afterA, afterB} {
		for i := 0; i < Slots; i++ {
			if _, err := unseal(bank[i*TicketBytes:(i+1)*TicketBytes], 2, uint16(i), q.anchor); err != nil {
				oldRejected = true
			}
		}
	}
	if !oldRejected {
		t.Fatal("old strict bank parser could ignore fanout")
	}
	// Unknown variant in EITHER page refuses the new strict loader too, even
	// when the other bank remains readable. Nothing checkpoints a cheaper view.
	f, err := q.anchor.Root.OpenFile("tickets.b", os.O_RDWR, 0)
	must(t, err)
	unknown := append([]byte(nil), afterB[3*TicketBytes:4*TicketBytes]...)
	unknown[9] = 2
	seal(unknown, 2, 3, 1, q.anchor)
	_, err = f.WriteAt(unknown, 3*TicketBytes)
	must(t, err)
	must(t, f.Close())
	if _, err = q.Snapshot(t.Context()); !errors.Is(err, ErrUnknown) {
		t.Fatal("unknown peer page fell back", err)
	}
}

func TestFanoutManifestOnlyAndRetiredBankCut(t *testing.T) {
	q, path := fixture(t)
	first, err := q.Reserve(t.Context(), fanoutFixtureRequest(1, 1), observations())
	must(t, err)
	_, err = q.Settle(t.Context(), SettlementFact{Token: first, Owner: owner, Evidence: digest(43), Changes: []Change{{}}})
	must(t, err)
	must(t, q.Checkpoint(t.Context()))
	// Replacing the retired manifest before writing a flat successor cannot
	// resurrect old fanout debt or make its retired membership authoritative.
	q.fault = &ioFault{Name: "tickets.b", Phase: "write", Offset: 0}
	_, err = q.Reserve(t.Context(), request(1), observations())
	if !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	s, err := q.Snapshot(t.Context())
	must(t, err)
	if len(s.Tickets) != 0 || !s.Promised[History].zero() {
		t.Fatal("retired manifest replayed")
	}
	for _, name := range []string{"tickets.a", "tickets.b"} {
		bank, err := os.ReadFile(filepath.Join(path, name))
		must(t, err)
		if _, err = unseal(bank[:TicketBytes], 2, 0, q.anchor); err == nil {
			t.Fatal("old parser ignored a retired fanout bank")
		}
	}
	// Conversely a manifest-only fanout cut on an empty flat slot issues no
	// ticket: legacy page semantics are unchanged and no promise was returned.
	other, otherPath := fixture(t)
	other.fault = &ioFault{Name: "tickets.b", Phase: "write", Offset: 0}
	_, err = other.Reserve(t.Context(), fanoutFixtureRequest(1, 1), observations())
	if !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	s, err = other.Snapshot(t.Context())
	must(t, err)
	if len(s.Tickets) != 0 {
		t.Fatal("manifest-only cut fabricated admission")
	}
	for _, name := range []string{"tickets.a", "tickets.b"} {
		bank, err := os.ReadFile(filepath.Join(otherPath, name))
		must(t, err)
		_, err = unseal(bank[:TicketBytes], 2, 0, other.anchor)
		must(t, err)
	}
}

func TestFanoutReservationFailuresRetainMembershipAndPromise(t *testing.T) {
	for _, phase := range []string{"write", "sync", "close", "readback"} {
		t.Run(phase, func(t *testing.T) {
			q, path := fixture(t)
			r := fanoutFixtureRequest(6, 6)
			q.fault = &ioFault{Name: "tickets.b", Phase: phase, Offset: 0, Short: phase == "write"}
			if _, err := q.Reserve(t.Context(), r, observations()); !errors.Is(err, ErrUnknown) {
				t.Fatal("uncertain publication returned success", err)
			}
			read, root, witness := openFixture(t, path)
			defer read.Close()
			defer root.Close()
			defer witness.Close()
			s, err := read.StatusSnapshot(t.Context())
			if phase == "write" {
				if !errors.Is(err, ErrUnknown) {
					t.Fatal("torn page ignored", err)
				}
			} else {
				must(t, err)
				if s.Promises.Peak[History].Bytes != 6 || s.CapacityPromised[0].Peak.Bytes != 6 {
					t.Fatal("uncertain physical promise changed")
				}
				for _, w := range s.Workspaces {
					if w.Promises.Peak[History].Bytes != 6 {
						t.Fatal("uncertain member debt forgiven")
					}
				}
			}
			view, err := read.InspectRecovery(t.Context())
			must(t, err)
			if !view.ManifestValid[0] || !reflect.DeepEqual(view.ManifestReservations[0].Fanout, r.Fanout) {
				t.Fatal("original immutable fanout not retained")
			}
		})
	}
}

func TestFanoutRepairCannotShrinkOrInferMembership(t *testing.T) {
	q, _ := fixture(t)
	r := fanoutFixtureRequest(6, 6)
	_, err := q.Reserve(t.Context(), r, observations())
	must(t, err)
	view, err := q.InspectRecovery(t.Context())
	must(t, err)
	f := RepairFact{Fingerprint: view.Fingerprint, FormatEvidence: RepairFormatEvidence(q.anchor, view.Fingerprint), Evidence: digest(51), Policy: fixturePolicy(), Registry: fixtureRegistry(), Census: fixtureCensus()}
	for i := range f.Slots {
		f.Slots[i].Disposition = digest(52)
	}
	f.Slots[0] = RecoverySlot{Request: r}
	bad := f
	bad.Slots[0].Request.Fanout = cloneFanout(r.Fanout)
	bad.Slots[0].Request.Fanout.Workspaces = bad.Slots[0].Request.Fanout.Workspaces[:1]
	bad.Slots[0].Request.Fanout.Memberships[0][0] = 1
	if _, err = q.Repair(t.Context(), bad); !errors.Is(err, ErrAuthority) {
		t.Fatal("repair shrank readable member debt", err)
	}
	tokens, err := q.Repair(t.Context(), f)
	must(t, err)
	s, err := q.StatusSnapshot(t.Context())
	must(t, err)
	if s.Tickets.Uncertain != 1 || s.Promises.Peak[History].Bytes != 6 {
		t.Fatal("rebuild lost uncertain physical obligation")
	}
	for _, w := range s.Workspaces {
		if w.Promises.Peak[History].Bytes != 6 {
			t.Fatal("rebuild lost exact member obligation")
		}
	}
	if tokens[0].ID == (ID{}) {
		t.Fatal("no retained token")
	}
	// Losing the manifest does not convert its readable group page into a
	// zero-member reservation, even if the proposed replacement looks valid.
	file, err := q.anchor.Root.OpenFile("manifests", os.O_RDWR, 0)
	must(t, err)
	_, err = file.WriteAt([]byte{255}, pageHeader+TicketBytes+16)
	must(t, err)
	must(t, file.Close())
	view, err = q.InspectRecovery(t.Context())
	must(t, err)
	f.Fingerprint, f.FormatEvidence = view.Fingerprint, RepairFormatEvidence(q.anchor, view.Fingerprint)
	if _, err = q.Repair(t.Context(), f); !errors.Is(err, ErrAuthority) {
		t.Fatal("unreadable membership inferred from absence", err)
	}
}

func TestFanoutSettlementAndCheckpointCutsRetainOriginal(t *testing.T) {
	for _, cut := range []struct {
		name, file, phase string
		checkpoint, short bool
	}{
		{"settle-write", "tickets.a", "write", false, true},
		{"settle-sync", "tickets.a", "sync", false, false},
		{"settle-close", "tickets.a", "close", false, false},
		{"settle-readback", "tickets.a", "readback", false, false},
		{"checkpoint-write", "checkpoint.b", "write", true, true},
		{"checkpoint-sync", "checkpoint.b", "sync", true, false},
		{"checkpoint-close", "checkpoint.b", "close", true, false},
		{"checkpoint-readback", "checkpoint.b", "readback", true, false},
	} {
		t.Run(cut.name, func(t *testing.T) {
			q, _ := fixture(t)
			r := fanoutFixtureRequest(3, 4)
			token, err := q.Reserve(t.Context(), r, observations())
			must(t, err)
			fact := SettlementFact{Token: token, Owner: owner, Evidence: digest(60), Changes: []Change{{PhysicalAdd: Vector{3, 1, 0}, AttributionAdd: Vector{4, 1, 0}}}}
			if cut.checkpoint {
				_, err = q.Settle(t.Context(), fact)
				must(t, err)
			}
			q.fault = &ioFault{Name: cut.file, Phase: cut.phase, Offset: 0, Short: cut.short}
			if cut.checkpoint {
				err = q.Checkpoint(t.Context())
			} else {
				_, err = q.Settle(t.Context(), fact)
			}
			if !errors.Is(err, ErrUnknown) {
				t.Fatal("uncertain persistence reported completion", err)
			}
			s, readErr := q.StatusSnapshot(t.Context())
			if cut.short {
				if !errors.Is(readErr, ErrUnknown) {
					t.Fatal("torn peer ignored", readErr)
				}
			} else {
				must(t, readErr)
				if s.Installation[History].Bytes != 3 {
					t.Fatal("physical terminal delta repeated or forgotten")
				}
				for _, w := range s.Workspaces {
					if w.Usage[History].Bytes != 4 {
						t.Fatal("uncertain terminal lost exact workspace delta")
					}
				}
			}
			view, err := q.InspectRecovery(t.Context())
			must(t, err)
			if !view.ManifestValid[0] || !reflect.DeepEqual(view.ManifestReservations[0].Fanout, r.Fanout) {
				t.Fatal("original envelope erased by failed completion")
			}
		})
	}
}

func TestFanoutDisjointGroupsAndSixteenReferencesPlusControl(t *testing.T) {
	ticket := fanoutCodecTicket(16, 1)
	control := request(1).Components[0]
	control.Root, control.Workspace, control.WorkspaceGeneration, control.Category = ident(1), ID{}, 0, Control
	control.Attribution = Vector{}
	ticket.Components = append(ticket.Components, control)
	ticket.Fanout.Memberships = append(ticket.Fanout.Memberships, WorkspaceMembership{})
	count, physical := 0, uint64(0)
	must(t, validateTicket(ticket))
	must(t, walkComponents(ticket, func(_ int, c Component, _ bool) error { count++; physical += c.Physical.Bytes; return nil }))
	if count != 17 || physical != 2 {
		t.Fatal("sixteen references plus independent control collapsed")
	}
	// Same-root/category groups with disjoint sets preserve differing exact
	// per-member amounts. No averaging or implicit union is allowed.
	ticket = fanoutCodecTicket(2, 1)
	ticket.Fanout.Memberships[0][0] = 1
	c := ticket.Components[0]
	c.Physical = Vector{}
	c.Attribution.Bytes = 2
	c.Capacity[0].Peak = CapacityVector{}
	ticket.Components = append(ticket.Components, c)
	ticket.Fanout.Memberships = append(ticket.Fanout.Memberships, WorkspaceMembership{2})
	must(t, validateTicket(ticket))
	var amounts []uint64
	must(t, walkComponents(ticket, func(_ int, c Component, _ bool) error { amounts = append(amounts, c.Attribution.Bytes); return nil }))
	if !reflect.DeepEqual(amounts, []uint64{1, 2}) {
		t.Fatal("disjoint attribution amounts changed")
	}
	// The immutable manifest commitment includes every generation and bitset.
	a := HeldAnchor{Installation: ident(30), Binding: digest(30)}
	m, err := encodeManifest(ticket, nil, a)
	must(t, err)
	original := sha256.Sum256(m)
	ticket.Fanout.Workspaces[0].Generation++
	m, err = encodeManifest(ticket, nil, a)
	must(t, err)
	if sha256.Sum256(m) == original {
		t.Fatal("manifest did not bind exact generation")
	}
}
