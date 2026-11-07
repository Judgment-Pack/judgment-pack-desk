package storagebudget

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInspectHostStatePersistedAppliedDistinctFromReplay(t *testing.T) {
	q, path := fixture(t)
	token, e := q.Reserve(t.Context(), request(3), observations())
	must(t, e)
	token, e = q.Advance(t.Context(), token, owner, Staging, digest(50))
	must(t, e)
	fact := SettlementFact{Token: token, Owner: owner, Evidence: digest(51), Changes: []Change{{PhysicalAdd: Vector{3, 1, 0}, AttributionAdd: Vector{3, 1, 0}}}}
	terminal, e := q.Settle(t.Context(), fact)
	must(t, e)
	hashes := configurationTestHashes(t, path)
	first, e := q.InspectHostState(t.Context())
	must(t, e)
	inspection, e := q.InspectHost(t.Context())
	must(t, e)
	if inspection.Configuration.Revision != first.Revision || inspection.Configuration.Epoch != first.Epoch || inspection.Configuration.Maintenance != first.Maintenance || !reflect.DeepEqual(inspection.Slots, first.Slots) {
		t.Fatal("same-load host inspection omitted persisted tuples")
	}
	expectedRegistry := fixtureRegistry()
	if !reflect.DeepEqual(inspection.Configuration.Filesystems, expectedRegistry.Filesystems) || !reflect.DeepEqual(inspection.Configuration.CapacityConstraints, expectedRegistry.CapacityConstraints) || len(inspection.Roots) != 3 || len(inspection.Workspaces) != 2 {
		t.Fatal("complete v2 registry not projected")
	}
	for i, r := range expectedRegistry.Roots {
		got := inspection.Roots[i]
		if got.ID != r.ID || got.Owner != r.Owner || got.Identity != r.Identity || got.Filesystem != r.Filesystem || got.Workspace != r.Workspace || got.Generation != r.Generation || got.Locator != r.Locator || got.CapacityCount != r.CapacityCount || got.CapacityRefs != r.CapacityRefs || got.CapacityBinding != r.CapacityBinding {
			t.Fatal("immutable root capacity relation omitted")
		}
	}
	originalInspection, e := q.InspectHost(t.Context())
	must(t, e)
	inspection.Configuration.Filesystems[0].CapabilityEvidence = digest(99)
	inspection.Configuration.CapacityConstraints[0].Headroom.Bytes = 999
	inspection.Roots[0].CapacityRefs[0].Generation = 999
	inspection.Workspaces[0].Limits.Total.Bytes = 999
	inspection.Slots[terminal.Slot].Ticket.Components[0].Capacity[0].Peak.Bytes = 999
	inspection.Slots[terminal.Slot].Ticket.Changes[0].PhysicalAdd.Bytes = 999
	detachedInspection, e := q.InspectHost(t.Context())
	must(t, e)
	if !reflect.DeepEqual(originalInspection, detachedInspection) {
		t.Fatal("HostInspection aliases loaded metadata")
	}
	row := first.Slots[terminal.Slot]
	if first.Revision != 3 || first.Epoch != 1 || first.Maintenance || row.Ticket.Token != terminal || row.Ticket.State != Terminal || row.Ticket.Owner != owner || row.Ticket.Workflow != "chat" || row.Ticket.Operation != "step-1" || row.Ticket.Evidence != fact.Evidence || !reflect.DeepEqual(row.Ticket.Components, request(3).Components) || !reflect.DeepEqual(row.Ticket.Changes, fact.Changes) {
		t.Fatal("current ticket facts omitted or mixed")
	}
	if row.PersistedApplied.ID != (ID{}) || row.PersistedApplied.Sequence != 0 || row.PersistedApplied.Generation != 0 || row.PersistedApplied.Slot != terminal.Slot || row.PersistedNextSequence != 1 {
		t.Fatal("replayed in-memory terminal was reported as persisted incorporation")
	}
	// The ordinary snapshot includes the replayed usage and unincorporated
	// terminal, demonstrating why these facts must remain distinct.
	replay, e := q.Snapshot(t.Context())
	must(t, e)
	if replay.Installation[History] != (Vector{3, 1, 0}) || len(replay.Tickets) != 1 {
		t.Fatal("fixture did not produce unincorporated terminal replay")
	}
	reopened, root, witness := openFixture(t, path)
	defer root.Close()
	defer witness.Close()
	defer reopened.Close()
	again, e := reopened.InspectHostState(t.Context())
	must(t, e)
	if !reflect.DeepEqual(first, again) {
		t.Fatal("restart changed current/persisted distinction")
	}
	if !reflect.DeepEqual(hashes, configurationTestHashes(t, path)) {
		t.Fatal("host inspection checkpointed or wrote storage")
	}
	// Returned dynamic rows are detached even though every other field is a
	// bounded value. Neither mutation may change a later read.
	first.Slots[terminal.Slot].Ticket.Components[0].Physical.Bytes = 999
	first.Slots[terminal.Slot].Ticket.Changes[0].PhysicalAdd.Bytes = 999
	detached, e := q.InspectHostState(t.Context())
	must(t, e)
	if !reflect.DeepEqual(detached, again) {
		t.Fatal("host inspection aliases loaded ticket data")
	}
	must(t, q.Checkpoint(t.Context()))
	incorporated, e := q.InspectHostState(t.Context())
	must(t, e)
	if incorporated.Slots[terminal.Slot].PersistedApplied != terminal || incorporated.Slots[terminal.Slot].PersistedNextSequence != 2 || incorporated.Slots[terminal.Slot].Ticket.Token != terminal || incorporated.Slots[terminal.Slot].Ticket.State != Terminal || incorporated.Revision != first.Revision+1 {
		t.Fatal("durable checkpoint tuple not exposed exactly")
	}
	ordinary, e := q.Snapshot(t.Context())
	must(t, e)
	if len(ordinary.Tickets) != 0 {
		t.Fatal("ordinary snapshot did not retire incorporated ticket")
	}
	// The next operation can reuse Q's slot while persisted Applied remains the
	// exact old terminal; matching the slot number/current state would be wrong.
	next, e := q.Reserve(t.Context(), request(1), observations())
	must(t, e)
	reused, e := q.InspectHostState(t.Context())
	must(t, e)
	if next.Slot != terminal.Slot || next.ID == terminal.ID || next.Sequence != 2 || reused.Slots[next.Slot].Ticket.Token != next || reused.Slots[next.Slot].Ticket.State != Reserved || reused.Slots[next.Slot].PersistedApplied != terminal {
		t.Fatal("Q slot reuse lost full persisted identity")
	}
	newer, e := q.Settle(t.Context(), SettlementFact{Token: next, Owner: owner, Evidence: digest(52), Changes: []Change{{PhysicalAdd: Vector{1, 1, 0}, AttributionAdd: Vector{1, 1, 0}}}})
	must(t, e)
	must(t, q.Checkpoint(t.Context()))
	progress, e := q.InspectHostState(t.Context())
	must(t, e)
	if progress.Slots[next.Slot].PersistedApplied != newer || progress.Slots[next.Slot].PersistedApplied == terminal || progress.Slots[next.Slot].PersistedNextSequence != 3 || progress.Epoch != incorporated.Epoch {
		t.Fatal("later persisted sequence/identity not distinguished")
	}
}

func TestInspectHostStateMaintenanceAndEmptyRows(t *testing.T) {
	q, path := fixture(t)
	state, e := q.InspectHostState(t.Context())
	must(t, e)
	for i, row := range state.Slots {
		if row.Ticket.State != Empty || row.Ticket.Token.Slot != uint8(i) || row.PersistedApplied.Slot != uint8(i) || row.PersistedApplied.ID != (ID{}) || row.PersistedNextSequence != 1 {
			t.Fatalf("empty bounded row %d invalid", i)
		}
	}
	epoch, e := q.BeginMaintenance(t.Context(), state.Revision)
	must(t, e)
	hashes := configurationTestHashes(t, path)
	maintained, e := q.InspectHostState(t.Context())
	must(t, e)
	if !maintained.Maintenance || maintained.Epoch != epoch || maintained.Revision != state.Revision+1 {
		t.Fatal("maintenance revision/epoch omitted")
	}
	if !reflect.DeepEqual(hashes, configurationTestHashes(t, path)) {
		t.Fatal("maintenance inspection wrote storage")
	}
}

func TestInspectHostStateCorruptionAndUnknown(t *testing.T) {
	for _, name := range []string{"checkpoint.a", "tickets.b", "missing-checkpoint", "closed", "cancelled", "legacy"} {
		t.Run(name, func(t *testing.T) {
			q, path := fixture(t)
			ctx := t.Context()
			switch name {
			case "missing-checkpoint":
				must(t, os.Rename(filepath.Join(path, "checkpoint.a"), filepath.Join(path, "checkpoint-retained")))
			case "legacy":
				f, e := os.OpenFile(filepath.Join(path, "checkpoint.a"), os.O_WRONLY, 0)
				must(t, e)
				_, e = f.WriteAt([]byte("JPSTB001"), 0)
				must(t, e)
				must(t, f.Close())
			case "closed":
				must(t, q.Close())
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			default:
				f, e := os.OpenFile(filepath.Join(path, name), os.O_WRONLY, 0)
				must(t, e)
				_, e = f.WriteAt([]byte("!"), 0)
				must(t, e)
				must(t, f.Close())
			}
			before := map[string][]byte{}
			names, e := os.ReadDir(path)
			must(t, e)
			for _, n := range names {
				b, e := os.ReadFile(filepath.Join(path, n.Name()))
				must(t, e)
				before[n.Name()] = b
			}
			result, e := q.InspectHostState(ctx)
			if e == nil || !reflect.DeepEqual(result, HostState{}) {
				t.Fatal("unknown host state returned usable metadata")
			}
			inspected, inspectionErr := q.InspectHost(ctx)
			if inspectionErr == nil || !reflect.DeepEqual(inspected, HostInspection{}) {
				t.Fatal("failed same-load inspection exposed partial DTO")
			}
			for name, b := range before {
				got, e := os.ReadFile(filepath.Join(path, name))
				must(t, e)
				if !reflect.DeepEqual(got, b) {
					t.Fatal("refusal repaired or wrote unknown state")
				}
			}
		})
	}
}

func TestInspectHostStateFiniteCompleteTable(t *testing.T) {
	q, _ := fixture(t)
	p := fixturePolicy()
	p.Installation.Total.Bytes = MetadataBudget + 1024
	p.Installation.Category[History] = Vector{1024, 1024, 1024}
	p.Workspace.Category[History] = Vector{1024, 1024, 1024}
	initial, e := q.InspectHostState(t.Context())
	must(t, e)
	must(t, q.UpdatePolicy(t.Context(), initial.Revision, p))
	var tokens [Slots]Token
	for i := range tokens {
		tokens[i], e = q.Reserve(t.Context(), request(1), observations())
		must(t, e)
	}
	full, e := q.InspectHostState(t.Context())
	must(t, e)
	for i, row := range full.Slots {
		if row.Ticket.Token != tokens[i] || row.Ticket.State != Reserved || row.PersistedNextSequence != 1 || row.PersistedApplied.ID != (ID{}) {
			t.Fatalf("finite slot %d lost", i)
		}
	}
	if _, e = q.Reserve(t.Context(), request(1), observations()); !errors.Is(e, ErrBounds) {
		t.Fatal("finite Q slot exhaustion not refused")
	}
	after, e := q.InspectHostState(t.Context())
	must(t, e)
	if !reflect.DeepEqual(after, full) {
		t.Fatal("refused overflow changed host snapshot")
	}
}
