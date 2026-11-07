package storagebudget

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInspectEnrollmentSameLoadAndDetached(t *testing.T) {
	q, path := fixture(t)
	hashes := configurationTestHashes(t, path)
	got, err := q.InspectEnrollment(t.Context())
	must(t, err)
	// fixture's original coordinator sequence is Initialize1, Register2,
	// PublishCensus3, so A3 is selected over B2. Empty ticket ties choose A.
	if got.CheckpointBank != 0 || got.View.Configuration.Revision != 3 || got.View.Configuration.Epoch != 1 || got.View.Configuration.Maintenance {
		t.Fatal("wrong original checkpoint/view relation")
	}
	for bank, name := range []string{"checkpoint.a", "checkpoint.b"} {
		want, e := os.ReadFile(filepath.Join(path, name))
		must(t, e)
		if len(got.Checkpoints[bank]) != CheckpointBytes || !bytes.Equal(got.Checkpoints[bank], want) {
			t.Fatal("complete physical checkpoint bytes lost", name)
		}
	}
	for bank, name := range []string{"tickets.a", "tickets.b"} {
		want, e := os.ReadFile(filepath.Join(path, name))
		must(t, e)
		if len(got.Tickets[bank]) != Slots*TicketBytes || !bytes.Equal(got.Tickets[bank], want) {
			t.Fatal("inactive or selected ticket bank omitted", name)
		}
	}
	for slot, row := range got.View.Slots {
		if got.TicketBanks[slot] != 0 || row.Ticket.State != Empty || row.Ticket.Token.Slot != uint8(slot) || row.PersistedApplied != (Token{Slot: uint8(slot)}) || row.PersistedNextSequence != 1 {
			t.Fatal("full empty slot or selected bank lost", slot)
		}
	}
	got.Checkpoints[0][0] = 0
	got.Checkpoints[1][0] = 0
	got.Tickets[0][0] = 0
	got.Tickets[1][0] = 0
	got.View.Roots[0].CapacityRefs[0].Generation = 999
	again, err := q.InspectEnrollment(t.Context())
	must(t, err)
	if again.Checkpoints[0][0] != 'J' || again.Checkpoints[1][0] != 'J' || again.Tickets[0][0] != 'J' || again.Tickets[1][0] != 'J' || again.View.Roots[0].CapacityRefs[0].Generation == 999 {
		t.Fatal("observation aliases Q state")
	}
	if !reflect.DeepEqual(hashes, configurationTestHashes(t, path)) {
		t.Fatal("read-only capture changed persisted state")
	}
}

func TestInspectEnrollmentAllWorkflowPersistedEntry(t *testing.T) {
	q, path := fixture(t)
	r := request(3)
	r.Workflow = "foreign-original-workflow"
	token, err := q.Reserve(t.Context(), r, observations())
	must(t, err)
	before := configurationTestHashes(t, path)
	if got, err := q.InspectEnrollment(t.Context()); !errors.Is(err, ErrBusy) || !reflect.DeepEqual(got, EnrollmentInspection{}) {
		t.Fatal("active foreign ticket accepted or partial capture returned", err)
	}
	if !reflect.DeepEqual(before, configurationTestHashes(t, path)) {
		t.Fatal("active refusal wrote storage")
	}
	token, err = q.Settle(t.Context(), SettlementFact{Token: token, Owner: owner, Evidence: digest(51), Changes: []Change{{PhysicalAdd: Vector{3, 1, 0}, AttributionAdd: Vector{3, 1, 0}}}})
	must(t, err)
	before = configurationTestHashes(t, path)
	if got, err := q.InspectEnrollment(t.Context()); !errors.Is(err, ErrBusy) || !reflect.DeepEqual(got, EnrollmentInspection{}) {
		t.Fatal("unincorporated terminal replay accepted", err)
	}
	if !reflect.DeepEqual(before, configurationTestHashes(t, path)) {
		t.Fatal("capture incorporated terminal replay")
	}
	must(t, q.Checkpoint(t.Context()))
	got, err := q.InspectEnrollment(t.Context())
	must(t, err)
	row := got.View.Slots[token.Slot]
	if got.CheckpointBank != 1 || got.View.Configuration.Revision != 4 || row.Ticket.Token != token || row.Ticket.State != Terminal || row.PersistedApplied != token || row.PersistedNextSequence != 2 || got.TicketBanks[token.Slot] != 0 {
		t.Fatal("exact original terminal/persisted/next/bank relation lost")
	}
	// A selected terminal at generation2 links to Reserved generation1 and is
	// returned with both full bank bodies, not synthesized from the view.
	got.View.Slots[token.Slot].Ticket.Components[0].Physical.Bytes = 999
	got.View.Slots[token.Slot].Ticket.Changes[0].PhysicalAdd.Bytes = 999
	again, err := q.InspectEnrollment(t.Context())
	must(t, err)
	if again.View.Slots[token.Slot].Ticket.Components[0].Physical.Bytes == 999 || again.View.Slots[token.Slot].Ticket.Changes[0].PhysicalAdd.Bytes == 999 {
		t.Fatal("ticket slices not detached")
	}
	_, err = q.BeginMaintenance(t.Context(), 4)
	must(t, err)
	maintenance, err := q.InspectEnrollment(t.Context())
	must(t, err)
	if !maintenance.View.Configuration.Maintenance || maintenance.View.Configuration.Revision != 5 || maintenance.View.Configuration.Epoch != 2 || maintenance.CheckpointBank != 0 || !reflect.DeepEqual(maintenance.View.Slots, again.View.Slots) {
		t.Fatal("exact maintenance prefix cannot be inspected")
	}
}

func TestInspectEnrollmentRefusalsPreserveBytes(t *testing.T) {
	for _, name := range []string{"checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests"} {
		t.Run("legacy-"+name, func(t *testing.T) {
			q, path := fixture(t)
			f, err := os.OpenFile(filepath.Join(path, name), os.O_RDWR, 0)
			must(t, err)
			_, err = f.WriteAt([]byte("JPSTB001"), 0)
			must(t, err)
			must(t, f.Close())
			before := configurationTestHashes(t, path)
			got, err := q.InspectEnrollment(t.Context())
			if !errors.Is(err, ErrUnsupported) || !reflect.DeepEqual(got, EnrollmentInspection{}) || !reflect.DeepEqual(before, configurationTestHashes(t, path)) {
				t.Fatal("legacy carrier adopted or changed", err)
			}
		})
	}
	for _, name := range []string{"checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b"} {
		t.Run("torn-"+name, func(t *testing.T) {
			q, path := fixture(t)
			f, err := os.OpenFile(filepath.Join(path, name), os.O_RDWR, 0)
			must(t, err)
			_, err = f.WriteAt([]byte{255}, 100)
			must(t, err)
			must(t, f.Close())
			before := configurationTestHashes(t, path)
			got, err := q.InspectEnrollment(t.Context())
			if err == nil || !reflect.DeepEqual(got, EnrollmentInspection{}) || !reflect.DeepEqual(before, configurationTestHashes(t, path)) {
				t.Fatal("torn peer fell back or was repaired", err)
			}
		})
	}
	q, path := fixture(t)
	before := configurationTestHashes(t, path)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := q.InspectEnrollment(ctx)
	if err == nil || !reflect.DeepEqual(got, EnrollmentInspection{}) || !reflect.DeepEqual(before, configurationTestHashes(t, path)) {
		t.Fatal("canceled inspection returned metadata or wrote state", err)
	}
	must(t, q.Close())
	if got, err = q.InspectEnrollment(t.Context()); !errors.Is(err, os.ErrClosed) || !reflect.DeepEqual(got, EnrollmentInspection{}) {
		t.Fatal("closed coordinator returned capture", err)
	}
}

func TestInspectEnrollmentSerializesWithOriginalQ(t *testing.T) {
	q, _ := fixture(t)
	// Hold Q's actual original gate, then start both read and ticket mutation.
	// The capture may precede the Reserve and return the exact empty view, or
	// follow it and refuse. It cannot return a mixed checkpoint/ticket view.
	<-q.gate
	started := make(chan struct{})
	type captureResult struct {
		value EnrollmentInspection
		err   error
	}
	result := make(chan captureResult, 1)
	go func() {
		close(started)
		v, err := q.InspectEnrollment(t.Context())
		result <- captureResult{v, err}
	}()
	<-started
	reserved := make(chan error, 1)
	go func() { _, err := q.Reserve(t.Context(), request(3), observations()); reserved <- err }()
	q.gate <- struct{}{}
	got := <-result
	must(t, <-reserved)
	if got.err != nil {
		if !errors.Is(got.err, ErrBusy) || !reflect.DeepEqual(got.value, EnrollmentInspection{}) {
			t.Fatal("invalid serialized refusal", got.err)
		}
		return
	}
	if got.value.View.Configuration.Revision != 3 || got.value.CheckpointBank != 0 {
		t.Fatal("capture mixed Q generation")
	}
	for slot, row := range got.value.View.Slots {
		if row.Ticket.State != Empty || got.value.TicketBanks[slot] != 0 {
			t.Fatal("capture mixed active and pre-Reserve rows")
		}
		for bank := 0; bank < 2; bank++ {
			ticket, err := decodeTicket(got.value.Tickets[bank][slot*TicketBytes:(slot+1)*TicketBytes], slot, q.anchor)
			must(t, err)
			if ticket.State != Empty || ticket.Token != row.Ticket.Token {
				t.Fatal("same-load raw bank and view disagree")
			}
		}
	}
}

func TestInspectActionActiveUnappliedAndDetached(t *testing.T) {
	q, path := fixture(t)
	token, err := q.Reserve(t.Context(), request(3), observations())
	must(t, err)
	check := func(state State, bank uint8) {
		t.Helper()
		before := configurationTestHashes(t, path)
		got, e := q.InspectAction(t.Context())
		must(t, e)
		row := got.View.Slots[token.Slot]
		if row.Ticket.State != state || row.Ticket.Token != token || got.TicketBanks[token.Slot] != bank || got.CheckpointBank != 0 || got.View.Configuration.Revision != 3 || row.PersistedApplied != (Token{Slot: token.Slot}) || row.PersistedNextSequence != 1 {
			t.Fatal("action inspection changed original disk tuple or mixed generations", state, row)
		}
		for i, name := range []string{"checkpoint.a", "checkpoint.b"} {
			raw, e := os.ReadFile(filepath.Join(path, name))
			must(t, e)
			if !bytes.Equal(raw, got.Checkpoints[i]) {
				t.Fatal("raw checkpoint peer omitted")
			}
		}
		for i, name := range []string{"tickets.a", "tickets.b"} {
			raw, e := os.ReadFile(filepath.Join(path, name))
			must(t, e)
			if !bytes.Equal(raw, got.Tickets[i]) {
				t.Fatal("raw ticket peer omitted")
			}
		}
		if refusal, e := q.InspectEnrollment(t.Context()); !errors.Is(e, ErrBusy) || !reflect.DeepEqual(refusal, EnrollmentInspection{}) {
			t.Fatal("action observation weakened enrollment gate", e)
		}
		got.Checkpoints[0][0] ^= 255
		got.Tickets[1][0] ^= 255
		got.View.Slots[token.Slot].Ticket.Components[0].Physical.Bytes = 999
		if state == Terminal {
			got.View.Slots[token.Slot].Ticket.Changes[0].PhysicalAdd.Bytes = 999
		}
		again, e := q.InspectAction(t.Context())
		must(t, e)
		if again.Checkpoints[0][0] != 'J' || again.Tickets[1][0] != 'J' || again.View.Slots[token.Slot].Ticket.Components[0].Physical.Bytes == 999 || state == Terminal && again.View.Slots[token.Slot].Ticket.Changes[0].PhysicalAdd.Bytes == 999 {
			t.Fatal("action raw/decoded view aliases original load")
		}
		if !reflect.DeepEqual(before, configurationTestHashes(t, path)) {
			t.Fatal("read-only action inspection persisted replay or mutated bytes")
		}
	}
	check(Reserved, 1)
	token, err = q.Advance(t.Context(), token, owner, Staging, digest(61))
	must(t, err)
	check(Staging, 0)
	token, err = q.Settle(t.Context(), SettlementFact{Token: token, Owner: owner, Evidence: digest(62), Changes: []Change{{PhysicalAdd: Vector{3, 1, 0}, AttributionAdd: Vector{3, 1, 0}}}})
	must(t, err)
	check(Terminal, 1)
	must(t, q.Checkpoint(t.Context()))
	got, err := q.InspectAction(t.Context())
	must(t, err)
	if got.View.Slots[token.Slot].PersistedApplied != token || got.View.Slots[token.Slot].PersistedNextSequence != 2 || got.CheckpointBank != 1 {
		t.Fatal("action inspection lost actual incorporation")
	}
}
func TestInspectActionRefusalPreservesBytes(t *testing.T) {
	for _, name := range []string{"checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests"} {
		t.Run(name, func(t *testing.T) {
			q, path := fixture(t)
			_, err := q.Reserve(t.Context(), request(3), observations())
			must(t, err)
			f, err := os.OpenFile(filepath.Join(path, name), os.O_RDWR, 0)
			must(t, err)
			_, err = f.WriteAt([]byte{255}, 100)
			must(t, err)
			must(t, f.Close())
			before := configurationTestHashes(t, path)
			got, err := q.InspectAction(t.Context())
			if err == nil || !reflect.DeepEqual(got, EnrollmentInspection{}) || !reflect.DeepEqual(before, configurationTestHashes(t, path)) {
				t.Fatal("corrupt action peer/manifest adopted, repaired, or partially exported", err)
			}
		})
	}
	q, path := fixture(t)
	_, err := q.Reserve(t.Context(), request(3), observations())
	must(t, err)
	before := configurationTestHashes(t, path)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := q.InspectAction(ctx)
	if err == nil || !reflect.DeepEqual(got, EnrollmentInspection{}) || !reflect.DeepEqual(before, configurationTestHashes(t, path)) {
		t.Fatal("canceled action inspection exported/wrote state", err)
	}
	must(t, q.Close())
	got, err = q.InspectAction(t.Context())
	if !errors.Is(err, os.ErrClosed) || !reflect.DeepEqual(got, EnrollmentInspection{}) {
		t.Fatal("closed action inspector exported state", err)
	}
}

func TestValidateAppliedRequestPreservesRetiredManifestBoundary(t *testing.T) {
	q, path := fixture(t)
	request := request(3)
	initial, err := q.Reserve(t.Context(), request, observations())
	must(t, err)
	staged, err := q.Advance(t.Context(), initial, owner, Staging, digest(51))
	must(t, err)
	terminal, err := q.Settle(t.Context(), SettlementFact{Token: staged, Owner: owner, Evidence: digest(52), Changes: []Change{{PhysicalAdd: Vector{3, 1, 0}, AttributionAdd: Vector{3, 1, 0}}}})
	must(t, err)
	status, err := q.InspectHostState(t.Context())
	must(t, err)
	if err = q.ValidateAppliedRequest(t.Context(), status.Epoch, status.Slots[terminal.Slot], initial, request); err == nil {
		t.Fatal("replayed terminal became a disk-applied proof")
	}
	must(t, q.Checkpoint(t.Context()))
	status, err = q.InspectHostState(t.Context())
	must(t, err)
	before := configurationTestHashes(t, path)
	row := status.Slots[terminal.Slot]
	must(t, q.ValidateAppliedRequest(t.Context(), status.Epoch, row, initial, request))
	if _, err = q.Manifest(t.Context(), terminal, owner); !errors.Is(err, ErrStale) {
		t.Fatal("retired manifest exposed", err)
	}
	for _, mutation := range []string{"entry", "components", "initial-generation", "next", "applied", "evidence", "changes", "manifest", "epoch"} {
		t.Run(mutation, func(t *testing.T) {
			r, v, token, epoch := request, row, initial, status.Epoch
			r.Components = append([]Component{}, request.Components...)
			r.Entries = append([]Entry{}, request.Entries...)
			v.Ticket.Changes = append([]Change{}, row.Ticket.Changes...)
			switch mutation {
			case "entry":
				r.Entries[0].Target = "other.json"
			case "components":
				r.Components[0].SettlementPhysical.Bytes++
			case "initial-generation":
				token.Generation++
			case "next":
				v.PersistedNextSequence++
			case "applied":
				v.PersistedApplied.Generation++
			case "evidence":
				v.Ticket.Evidence[0] ^= 1
			case "changes":
				v.Ticket.Changes[0].PhysicalAdd.Bytes++
			case "manifest":
				v.Ticket.Manifest[0] ^= 1
			case "epoch":
				epoch++
			}
			if err := q.ValidateAppliedRequest(t.Context(), epoch, v, token, r); err == nil {
				t.Fatal("changed original accepted", mutation)
			}
		})
	}
	if !reflect.DeepEqual(before, configurationTestHashes(t, path)) {
		t.Fatal("request comparison wrote Q")
	}
	// Legitimate reuse may replace the payload. The old proof may neither read
	// that payload nor substitute the new ticket for this original terminal.
	next := request
	next.Operation = "step-2"
	_, err = q.Reserve(t.Context(), next, observations())
	must(t, err)
	if err = q.ValidateAppliedRequest(t.Context(), status.Epoch, row, initial, request); err == nil {
		t.Fatal("later ticket stood in for original")
	}
}

func TestInspectEnrollmentDecodedBanksKeepFreshReads(t *testing.T) {
	q, path := fixture(t)
	first, err := q.InspectEnrollment(t.Context())
	must(t, err)
	bank := int(first.CheckpointBank)
	names := []string{"checkpoint.a", "checkpoint.b"}
	raw := append([]byte(nil), first.Checkpoints[bank]...)
	cp, err := decodeCheckpoint(raw, q.anchor)
	must(t, err)
	cp.Policy.ObservationMaxAge++
	changed, err := encodeCheckpoint(cp, q.anchor)
	must(t, err)
	// A valid new page at the SAME generation must be decoded afresh.
	if cp.Generation != first.View.Configuration.Revision || bytes.Equal(changed, raw) {
		t.Fatal("same-generation fixture did not change bytes")
	}
	file, err := os.OpenFile(filepath.Join(path, names[bank]), os.O_WRONLY, 0)
	must(t, err)
	_, err = file.WriteAt(changed, 0)
	must(t, err)
	must(t, file.Close())
	again, err := q.InspectEnrollment(t.Context())
	must(t, err)
	if !bytes.Equal(again.Checkpoints[bank], changed) || again.View.Configuration.Policy.ObservationMaxAge != cp.Policy.ObservationMaxAge {
		t.Fatal("fresh same-generation page was hidden")
	}
	// Warming both banks cannot hide corruption, cancellation or file exchange.
	file, err = os.OpenFile(filepath.Join(path, names[bank]), os.O_WRONLY, 0)
	must(t, err)
	_, err = file.WriteAt([]byte{255}, 100)
	must(t, err)
	must(t, file.Close())
	before := configurationTestHashes(t, path)
	if _, err = q.InspectEnrollment(t.Context()); err == nil {
		t.Fatal("warm decoded bank accepted corrupt current bytes")
	}
	if !reflect.DeepEqual(before, configurationTestHashes(t, path)) {
		t.Fatal("strict refusal changed storage")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = q.InspectEnrollment(canceled); err == nil {
		t.Fatal("warm decoded bank accepted cancellation")
	}
}

func TestCoordinatorDecodedBanksStrictEquivalenceAndIsolation(t *testing.T) {
	q, path := fixture(t)
	raw, err := os.ReadFile(filepath.Join(path, "checkpoint.a"))
	must(t, err)
	strict, err := decodeCheckpoint(append([]byte(nil), raw...), q.anchor)
	must(t, err)
	must(t, q.locked(t.Context(), func() error {
		got, err := q.decodeCheckpointBank(0, raw)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(got, strict) {
			t.Fatal("first checkpoint differs")
		}
		got.Roots[0].Locator = "changed caller copy"
		got.Applied[0].NextSequence++
		again, err := q.decodeCheckpointBank(0, raw)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(again, strict) {
			t.Fatal("returned checkpoint poisoned retained bank")
		}
		saved := q.anchor.Binding
		q.anchor.Binding[0] ^= 1
		_, reuseErr := q.decodeCheckpointBank(0, raw)
		_, strictErr := decodeCheckpoint(append([]byte(nil), raw...), q.anchor)
		q.anchor.Binding = saved
		if strictErr == nil || !errors.Is(reuseErr, strictErr) {
			t.Fatal("changed anchor reused checkpoint", reuseErr, strictErr)
		}
		return nil
	}))
	token, err := q.Reserve(t.Context(), request(1), observations())
	must(t, err)
	action, err := q.InspectAction(t.Context())
	must(t, err)
	slot := int(token.Slot)
	must(t, q.locked(t.Context(), func() error {
		for bank := 0; bank < 2; bank++ {
			page := append([]byte(nil), action.Tickets[bank][slot*TicketBytes:(slot+1)*TicketBytes]...)
			strict, err := decodeTicket(append([]byte(nil), page...), slot, q.anchor)
			if err != nil {
				return err
			}
			got, err := q.decodeTicketBank(bank, slot, page)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(got, strict) {
				t.Fatal("ticket strict decode differs")
			}
			if len(got.Components) > 0 {
				got.Components[0].Physical.Bytes++
			}
			if len(got.Changes) > 0 {
				got.Changes[0].PhysicalAdd.Bytes++
			}
			got.Fanout = &WorkspaceFanout{Workspaces: []WorkspaceReference{{ID: ident(99)}}}
			again, err := q.decodeTicketBank(bank, slot, page)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(again, strict) {
				t.Fatal("returned ticket poisoned retained bank")
			}
			page[100] ^= 1
			if _, err = q.decodeTicketBank(bank, slot, page); err == nil {
				t.Fatal("same-generation corrupt ticket reused")
			}
		}
		return nil
	}))
	must(t, q.Close())
	if q.decoded[0].checkpoint != nil || q.decoded[1].ticketRaw[slot] != nil {
		t.Fatal("closed coordinator retained page data")
	}
}

// A load copies only its selected checkpoint bank. Mutating that loaded model
// must not reach either retained bank decode, and each later load still
// equals a fresh strict decode of the actual selected page.
func TestCoordinatorLoadCopiesSelectedCheckpointOnly(t *testing.T) {
	q, path := fixture(t)
	_, err := q.Reserve(t.Context(), request(1), observations())
	must(t, err)
	must(t, q.locked(t.Context(), func() error {
		for round := 0; round < 2; round++ {
			l, err := q.load()
			if err != nil {
				return err
			}
			name := []string{"checkpoint.a", "checkpoint.b"}[l.cpBank]
			raw, err := os.ReadFile(filepath.Join(path, name))
			if err != nil {
				return err
			}
			strict, err := decodeCheckpoint(raw, q.anchor)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(l.cp, strict) {
				t.Fatal("loaded checkpoint differs from strict decode", round)
			}
			for bank := range q.decoded {
				if q.decoded[bank].checkpoint == l.cp {
					t.Fatal("load returned a retained bank model", round)
				}
			}
			l.cp.Generation++
			l.cp.Roots[0].Locator = "changed loaded copy"
			l.cp.Applied[0].NextSequence++
			for bank := range q.decoded {
				if c := q.decoded[bank].checkpoint; c != nil && (c.Roots[0].Locator == "changed loaded copy" || c.Generation == l.cp.Generation && bank == l.cpBank) {
					t.Fatal("loaded checkpoint poisoned a retained bank", round, bank)
				}
			}
		}
		return nil
	}))
}
