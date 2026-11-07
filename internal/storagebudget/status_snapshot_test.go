package storagebudget

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStatusSnapshotPromisesCountsAndDetachedRead(t *testing.T) {
	q, path := fixture(t)
	initial, err := q.StatusSnapshot(t.Context())
	must(t, err)
	p := initial.Policy
	p.Installation.Total.Bytes += 100
	p.Installation.Category[History] = Vector{100, 100, 100}
	p.Workspace.Category[History] = Vector{100, 100, 100}
	must(t, q.UpdatePolicy(t.Context(), initial.Revision, p))
	r := request(1)
	r.Components[0].SettlementPhysical = Vector{2, 0, 1}
	r.Components[0].Attribution = Vector{3, 2, 0}
	r.Components[0].SettlementAttribution = Vector{4, 0, 2}
	r.Components[0] = fixtureComponent(r.Components[0], fixtureRegistry().Roots[1])
	var tokens [5]Token
	for i := range tokens {
		tokens[i], err = q.Reserve(t.Context(), r, observations())
		must(t, err)
	}
	tokens[1], err = q.Advance(t.Context(), tokens[1], owner, Staging, digest(41))
	must(t, err)
	tokens[2], err = q.Advance(t.Context(), tokens[2], owner, Staging, digest(42))
	must(t, err)
	tokens[2], err = q.Advance(t.Context(), tokens[2], owner, Dispatched, digest(43))
	must(t, err)
	tokens[3], err = q.Advance(t.Context(), tokens[3], owner, Uncertain, digest(44))
	must(t, err)
	tokens[4], err = q.Settle(t.Context(), SettlementFact{Token: tokens[4], Owner: owner, Evidence: digest(45), Changes: []Change{{PhysicalAdd: Vector{1, 1, 0}, AttributionAdd: Vector{3, 2, 0}}}})
	must(t, err)
	before := configurationTestHashes(t, path)
	entries, err := os.ReadDir(path)
	must(t, err)
	// If the read attempted this existing checkpoint write hook it would fail.
	fault := &ioFault{Name: "checkpoint.a", Phase: "write", Offset: -1}
	q.fault = fault
	s, err := q.StatusSnapshot(t.Context())
	must(t, err)
	if q.fault != fault {
		t.Fatal("read attempted checkpoint persistence")
	}
	q.fault = nil
	if s.Tickets != (StatusTicketCounts{Nonempty: 5, Active: 3, Reserved: 1, Staging: 1, Dispatched: 1, Uncertain: 1, TerminalUnapplied: 1}) {
		t.Fatalf("wrong disjoint counts: %+v", s.Tickets)
	}
	if s.Installation[History] != (Vector{1, 1, 0}) || s.Workspaces[0].Usage[History] != (Vector{3, 2, 0}) {
		t.Fatal("terminal replay or independent workspace attribution lost")
	}
	if s.Promises.Peak[History] != (Vector{4, 4, 0}) || s.Promises.Settlement[History] != (Vector{8, 0, 4}) || s.Roots[1].Promises != s.Promises {
		t.Fatal("ordinary and settlement physical promises not separated")
	}
	if s.Workspaces[0].Promises.Peak[History] != (Vector{12, 8, 0}) || s.Workspaces[0].Promises.Settlement[History] != (Vector{16, 0, 8}) || s.Roots[2].Promises != (StatusPromises{}) || s.Workspaces[1].Promises != (StatusPromises{}) {
		t.Fatal("workspace promises confused with physical or another scope")
	}
	if !reflect.DeepEqual(s.CapacityPromised, []CapacityPromise{{Constraint: ident(60), Generation: 1, Peak: CapacityVector{12, 8}}}) {
		t.Fatal("capacity demand doubled settlement or included terminal")
	}
	if len(s.Roots) != 3 || len(s.Workspaces) != 2 || !reflect.DeepEqual(s.Filesystems, fixtureRegistry().Filesystems) || !reflect.DeepEqual(s.CapacityConstraints, fixtureRegistry().CapacityConstraints) || !s.Roots[1].Known || !s.Workspaces[0].Known {
		t.Fatal("registered definitions or knownness missing")
	}
	again, err := q.StatusSnapshot(t.Context())
	must(t, err)
	s.Policy.Installation.Total.Bytes = 1
	s.Roots[1].Usage[History].Bytes = 999
	s.Roots[1].Promises.Peak[History].Bytes = 999
	s.Roots[1].CapacityRefs[0].Generation = 999
	s.Workspaces[0].Limits.Total.Bytes = 999
	s.Workspaces[0].Promises.Settlement[History].Bytes = 999
	s.Filesystems[0].Generation = 999
	s.CapacityConstraints[0].Headroom.Bytes = 999
	s.CapacityPromised[0].Peak.Bytes = 999
	detached, err := q.StatusSnapshot(t.Context())
	must(t, err)
	afterEntries, err := os.ReadDir(path)
	must(t, err)
	if !reflect.DeepEqual(again, detached) || !reflect.DeepEqual(before, configurationTestHashes(t, path)) || !reflect.DeepEqual(entries, afterEntries) {
		t.Fatal("status aliases metadata, writes bytes or creates entries")
	}
	must(t, q.Checkpoint(t.Context()))
	persisted, err := q.StatusSnapshot(t.Context())
	must(t, err)
	if persisted.Tickets.TerminalUnapplied != 0 || persisted.Tickets.TerminalApplied != 1 || persisted.Installation != again.Installation || persisted.Promises != again.Promises {
		t.Fatal("persisted incorporation confused with replay")
	}
	// Reuse leaves an older persisted tuple in the same slot. Its terminal is
	// still unapplied until this new exact tuple is checkpointed.
	next, err := q.Reserve(t.Context(), request(1), observations())
	must(t, err)
	if next.Slot != tokens[4].Slot || next.ID == tokens[4].ID {
		t.Fatal("fixture did not reuse the retired slot")
	}
	_, err = q.Settle(t.Context(), SettlementFact{Token: next, Owner: owner, Evidence: digest(46), Changes: []Change{{}}})
	must(t, err)
	reused, err := q.StatusSnapshot(t.Context())
	must(t, err)
	if reused.Tickets.TerminalUnapplied != 1 || reused.Tickets.TerminalApplied != 0 {
		t.Fatal("terminal count matched only a slot or older applied tuple")
	}
}

func TestStatusSnapshotOneLoadAcrossIndependentTicketGeneration(t *testing.T) {
	q, _ := fixture(t)
	before, err := q.StatusSnapshot(t.Context())
	must(t, err)
	token, err := q.Reserve(t.Context(), request(2), observations())
	must(t, err)
	after, err := q.StatusSnapshot(t.Context())
	must(t, err)
	if before.Revision != after.Revision || before.Tickets.Nonempty != 0 || after.Tickets.Reserved != 1 || after.Promises.Peak[History] != (Vector{2, 1, 0}) {
		t.Fatal("checkpoint revision was used as ticket-set identity")
	}
	// Hold Q while a status call queues, then change both policy and the ticket
	// generation before releasing it. The queued read must see both changes.
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- q.locked(t.Context(), func() error {
			close(entered)
			<-release
			l, e := q.load()
			if e != nil {
				return e
			}
			l.cp.Policy.Installation.Total.Bytes++
			if e = q.saveCheckpoint(l); e != nil {
				return e
			}
			ticket := l.tickets[token.Slot]
			ticket.State = Staging
			ticket.Evidence = digest(70)
			ticket.Token.Generation++
			return q.saveTicket(l, ticket)
		})
	}()
	<-entered
	type result struct {
		s StatusSnapshot
		e error
	}
	started, read := make(chan struct{}), make(chan result, 1)
	go func() {
		close(started)
		s, e := q.StatusSnapshot(t.Context())
		read <- result{s, e}
	}()
	<-started
	close(release)
	must(t, <-done)
	got := <-read
	must(t, got.e)
	if got.s.Revision != after.Revision+1 || got.s.Policy.Installation.Total.Bytes != after.Policy.Installation.Total.Bytes+1 || got.s.Tickets.Staging != 1 || got.s.Tickets.Reserved != 0 || got.s.Promises != after.Promises {
		t.Fatal("queued read joined independent policy and ticket views")
	}
}

func TestStatusSnapshotUnknownMaintenanceAndRefusal(t *testing.T) {
	q, path := fixture(t)
	s, err := q.StatusSnapshot(t.Context())
	must(t, err)
	epoch, err := q.BeginMaintenance(t.Context(), s.Revision)
	must(t, err)
	r := fixtureRegistry().Roots[1]
	r.ID, r.Identity, r.Locator = ident(4), digest(4), "unknown-root"
	r.CapacityBinding, err = CapacityBindingFor(r, fixtureRegistry().Filesystems[0], fixtureRegistry().CapacityConstraints)
	must(t, err)
	must(t, q.Register(t.Context(), s.Revision+1, Registration{Roots: []Root{r}, Workspaces: []Workspace{{ID: ident(12), Generation: 1}}}))
	before := configurationTestHashes(t, path)
	unknown, err := q.StatusSnapshot(t.Context())
	must(t, err)
	if !unknown.Maintenance || unknown.Epoch != epoch || unknown.Roots[3].Known || unknown.Workspaces[2].Known || unknown.Installation[Control] != (Vector{MetadataBudget, 16, 1}) {
		t.Fatal("unknown scope fabricated known zero or erased private charges")
	}
	if !reflect.DeepEqual(before, configurationTestHashes(t, path)) {
		t.Fatal("unknown status persisted state")
	}
	for _, name := range []string{"corrupt", "legacy", "missing", "cancelled", "closed"} {
		t.Run(name, func(t *testing.T) {
			q, path := fixture(t)
			ctx := t.Context()
			switch name {
			case "corrupt", "legacy":
				f, e := os.OpenFile(filepath.Join(path, "checkpoint.a"), os.O_WRONLY, 0)
				must(t, e)
				b := []byte("!")
				if name == "legacy" {
					b = []byte("JPSTB001")
				}
				_, e = f.WriteAt(b, 0)
				must(t, e)
				must(t, f.Close())
			case "missing":
				must(t, os.Rename(filepath.Join(path, "checkpoint.a"), filepath.Join(path, "retained-checkpoint")))
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "closed":
				must(t, q.Close())
			}
			entries, e := os.ReadDir(path)
			must(t, e)
			before := map[string][]byte{}
			for _, entry := range entries {
				before[entry.Name()], e = os.ReadFile(filepath.Join(path, entry.Name()))
				must(t, e)
			}
			got, e := q.StatusSnapshot(ctx)
			if e == nil || !reflect.DeepEqual(got, StatusSnapshot{}) {
				t.Fatal("failed load exposed partial status")
			}
			after, e := os.ReadDir(path)
			must(t, e)
			if !reflect.DeepEqual(entries, after) {
				t.Fatal("refused read created bootstrap files")
			}
			for name, b := range before {
				got, e := os.ReadFile(filepath.Join(path, name))
				must(t, e)
				if !reflect.DeepEqual(got, b) {
					t.Fatal("refused read changed persistent bytes")
				}
			}
		})
	}
}

func TestStatusProjectionExactBounds(t *testing.T) {
	// Hand-authored loaded fixture isolates projection arithmetic from encoded
	// page-generation constraints; it does not claim parser/load coverage.
	registry := fixtureRegistry()
	l := &loaded{cp: &checkpoint{Generation: math.MaxUint64, Epoch: math.MaxUint64, Policy: fixturePolicy()}}
	copy(l.cp.Roots[:], registry.Roots)
	copy(l.cp.Workspaces[:], registry.Workspaces)
	copy(l.cp.Filesystems[:], registry.Filesystems)
	copy(l.cp.CapacityConstraints[:], registry.CapacityConstraints)
	c := request(1).Components[0]
	c.Physical = Vector{maxBytes - 1, maxCount - 1, 0}
	c.SettlementPhysical = Vector{1, 1, 0}
	c.Attribution, c.SettlementAttribution = c.Physical, c.SettlementPhysical
	c = fixtureComponent(c, registry.Roots[1])
	l.tickets[0] = Ticket{State: Reserved, Components: []Component{c}}
	s, err := projectStatus(l)
	must(t, err)
	if s.Revision != math.MaxUint64 || s.Epoch != math.MaxUint64 || s.Promises.Peak[History] != c.Physical || s.Promises.Settlement[History] != c.SettlementPhysical || s.CapacityPromised[0].Peak != (CapacityVector{maxBytes, maxCount}) {
		t.Fatal("exact maximum counters rounded, clamped or changed")
	}
	for _, name := range []string{"combined-bytes", "combined-files", "combined-directories", "capacity", "category"} {
		t.Run(name, func(t *testing.T) {
			bad := *l
			v := c
			switch name {
			case "combined-bytes":
				v.SettlementPhysical.Bytes++
			case "combined-files":
				v.SettlementPhysical.Files++
			case "combined-directories":
				v.Physical.Directories = maxCount
				v.SettlementPhysical.Directories = 1
			case "capacity":
				bad.tickets[1] = Ticket{State: Reserved, Components: []Component{fixtureComponent(Component{Root: ident(2), RootGeneration: 1, Owner: owner, Filesystem: ident(20), FilesystemGeneration: 1, Category: Retained, Physical: Vector{1, 0, 0}}, registry.Roots[1])}}
			case "category":
				v.Category = Categories
			}
			bad.tickets[0].Components = []Component{v}
			// registered() does not index category; the public load validates it.
			// Exercise the split helper's independent bound without bypassing load.
			if name == "category" {
				var p StatusPromises
				if !errors.Is(statusPromiseAdd(&p, v.Category, v.Physical, v.SettlementPhysical), ErrBounds) || p != (StatusPromises{}) {
					t.Fatal("category bound failed")
				}
				return
			}
			got, e := projectStatus(&bad)
			if !errors.Is(e, ErrBounds) || !reflect.DeepEqual(got, StatusSnapshot{}) {
				t.Fatal("overflow exposed partial or wrapped status")
			}
		})
	}
}

func TestStatusProjectionBoundedTableAndExactTerminalTuple(t *testing.T) {
	registry := fixtureRegistry()
	l := &loaded{cp: &checkpoint{Generation: 1, Policy: fixturePolicy()}}
	copy(l.cp.Roots[:], registry.Roots)
	copy(l.cp.Workspaces[:], registry.Workspaces)
	copy(l.cp.Filesystems[:], registry.Filesystems)
	copy(l.cp.CapacityConstraints[:], registry.CapacityConstraints)
	for i := range l.tickets {
		l.tickets[i] = Ticket{Token: Token{Slot: uint8(i), ID: ident(byte(i + 1)), Sequence: 7, Generation: 19}, State: Reserved, Components: request(1).Components}
	}
	s, err := projectStatus(l)
	must(t, err)
	if s.Tickets != (StatusTicketCounts{Nonempty: Slots, Active: Slots, Reserved: Slots}) || s.Promises.Peak[History] != (Vector{Slots, Slots, 0}) || s.CapacityPromised[0].Peak != (CapacityVector{Slots, Slots}) {
		t.Fatal("bounded full slot table omitted or duplicated a promise")
	}
	for i := range l.tickets {
		l.tickets[i].State = Terminal
		token := l.tickets[i].Token
		l.diskApplied[i] = applied{ID: token.ID, Sequence: token.Sequence, Generation: token.Generation, NextSequence: token.Sequence + 1}
	}
	// A differing ID, sequence or generation cannot be reported incorporated.
	l.diskApplied[0].ID = ident(99)
	l.diskApplied[1].Sequence--
	l.diskApplied[2].Generation--
	s, err = projectStatus(l)
	must(t, err)
	if s.Tickets != (StatusTicketCounts{Nonempty: Slots, TerminalUnapplied: 3, TerminalApplied: Slots - 3}) || s.Promises != (StatusPromises{}) || len(s.CapacityPromised) != 0 {
		t.Fatal("terminal incorporation requires all persisted tuple fields")
	}
	l.tickets[0] = Ticket{State: State(Terminal + 1)}
	s, err = projectStatus(l)
	if !errors.Is(err, ErrUnknown) || !reflect.DeepEqual(s, StatusSnapshot{}) {
		t.Fatal("unknown state exposed partial status")
	}
}
