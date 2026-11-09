package storagebudget

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

func ident(v byte) (x ID)      { x[0] = v; return }
func digest(v byte) (x Digest) { x[0] = v; return }

var owner = digest(9)

func fixturePolicy() Policy {
	full := Vector{MetadataBudget + 10, 1024, 1024}
	var p Policy
	p.Installation.Total = full
	p.Workspace.Total = Vector{1024, 1024, 1024}
	p.ObservationMaxAge = time.Minute
	for i := 0; i < Categories; i++ {
		p.Installation.Category[i] = full
		p.Workspace.Category[i] = Vector{1024, 1024, 1024}
	}
	p.Installation.Category[History] = Vector{10, 10, 10}
	p.Workspace.Category[History] = Vector{10, 10, 10}
	return p
}
func fixtureRegistry() Registration {
	r := Registration{
		Roots: []Root{
			{ID: ident(1), Owner: owner, Identity: digest(1), Filesystem: ident(20), Generation: 1, Locator: "control"},
			{ID: ident(2), Owner: owner, Identity: digest(2), Filesystem: ident(20), Workspace: ident(10), Generation: 1, Locator: "history-a"},
			{ID: ident(3), Owner: owner, Identity: digest(3), Filesystem: ident(21), Workspace: ident(11), Generation: 1, Locator: "history-b"}},
		Workspaces:          []Workspace{{ID: ident(10), Generation: 1, Limits: fixturePolicy().Workspace}, {ID: ident(11), Generation: 1, Limits: fixturePolicy().Workspace}},
		Filesystems:         []Filesystem{{ID: ident(20), Identity: digest(20), Generation: 1, InodeModel: InodesFinite, CapabilityEvidence: digest(80), ByteConstraint: ident(60), InodeConstraint: ident(60)}, {ID: ident(21), Identity: digest(21), Generation: 1, InodeModel: InodesFinite, CapabilityEvidence: digest(81), ByteConstraint: ident(61), InodeConstraint: ident(61)}},
		CapacityConstraints: []CapacityConstraint{{ID: ident(60), Identity: digest(60), Generation: 1, Resources: CapacityBytes | CapacityInodes, Headroom: CapacityVector{100, 10}}, {ID: ident(61), Identity: digest(61), Generation: 1, Resources: CapacityBytes | CapacityInodes, Headroom: CapacityVector{100, 10}}},
	}
	for i := range r.Roots {
		f := r.Filesystems[0]
		if r.Roots[i].Filesystem == ident(21) {
			f = r.Filesystems[1]
		}
		r.Roots[i].CapacityCount = 1
		r.Roots[i].CapacityRefs[0] = CapacityRef{f.ByteConstraint, 1}
		b, e := CapacityBindingFor(r.Roots[i], f, r.CapacityConstraints)
		if e != nil {
			panic(e)
		}
		r.Roots[i].CapacityBinding = b
	}
	return r
}
func fixtureComponent(c Component, root Root) Component {
	c.CapacityCount = root.CapacityCount
	c.CapacityBinding = root.CapacityBinding
	p, _, e := c.total()
	if e != nil {
		panic(e)
	}
	for i := 0; i < int(root.CapacityCount); i++ {
		peak := CapacityVector{p.Bytes, p.Files + p.Directories}
		if peak.Inodes > 0 && peak.Bytes == 0 {
			peak.Bytes = 1
		} // handwritten synthetic metadata allowance
		c.Capacity[i] = CapacityPromise{root.CapacityRefs[i].Constraint, root.CapacityRefs[i].Generation, peak}
	}
	return c
}
func fixtureCensus() Census {
	var control [Categories]Vector
	control[Control] = Vector{MetadataBudget, 16, 1}
	return Census{Evidence: digest(7), Roots: []RootMeasurement{{ident(1), 1, control}, {ID: ident(2), Generation: 1}, {ID: ident(3), Generation: 1}}, Workspaces: []WorkspaceMeasurement{{ID: ident(10), Generation: 1}, {ID: ident(11), Generation: 1}}}
}
func request(n uint64) Request {
	r := Request{Workflow: "chat", Operation: "step-1", Owner: owner, Components: []Component{{Root: ident(2), RootGeneration: 1, Owner: owner, Filesystem: ident(20), FilesystemGeneration: 1, Workspace: ident(10), WorkspaceGeneration: 1, Category: History, Physical: Vector{n, 1, 0}, Attribution: Vector{n, 1, 0}}}, Entries: []Entry{{Kind: 1, Component: 0, Target: "chat.json", Stage: "stage-ticket", NewIdentity: digest(4), Bytes: n, Files: 1}}}
	r.Components[0] = fixtureComponent(r.Components[0], fixtureRegistry().Roots[1])
	return r
}
func observations() []CapacityObservation {
	return []CapacityObservation{{Constraint: ident(60), Generation: 1, Available: CapacityVector{1 << 30, 1 << 20}, At: time.Now(), KnownResources: CapacityBytes | CapacityInodes}, {Constraint: ident(61), Generation: 1, Available: CapacityVector{1 << 30, 1 << 20}, At: time.Now(), KnownResources: CapacityBytes | CapacityInodes}}
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func openFixture(t *testing.T, path string) (*Coordinator, *os.Root, *os.File) {
	t.Helper()
	r, e := os.OpenRoot(path)
	must(t, e)
	w, e := r.OpenFile("lock", os.O_RDWR|noFollow, 0)
	must(t, e)
	q, e := OpenHeld(HeldAnchor{r, w, ident(30), digest(30), false})
	must(t, e)
	return q, r, w
}
func fixture(t *testing.T) (*Coordinator, string) {
	t.Helper()
	path := t.TempDir()
	must(t, os.Chmod(path, 0700))
	r, e := os.OpenRoot(path)
	must(t, e)
	w, e := r.OpenFile("lock", os.O_CREATE|os.O_EXCL|os.O_RDWR|noFollow, 0600)
	must(t, e)
	q, e := OpenHeld(HeldAnchor{r, w, ident(30), digest(30), false})
	must(t, e)
	t.Cleanup(func() { q.Close(); w.Close(); r.Close() })
	must(t, q.InitializeHeld(t.Context(), fixturePolicy()))
	must(t, q.Register(t.Context(), 1, fixtureRegistry()))
	must(t, q.PublishCensus(t.Context(), 1, fixtureCensus()))
	return q, path
}

func TestHandwrittenAccountingVectors(t *testing.T) {
	var rows []struct {
		Name                                                 string
		Used, Promised, Limit                                uint64
		UsedFiles, PromisedFiles, FileLimit                  uint64
		UsedDirectories, PromisedDirectories, DirectoryLimit uint64
		Allowed                                              bool
	}
	b, e := os.ReadFile("testdata/accounting.json")
	must(t, e)
	must(t, json.Unmarshal(b, &rows))
	for _, r := range rows {
		t.Run(r.Name, func(t *testing.T) {
			l := fixturePolicy().Installation
			for i := range l.Category {
				l.Category[i] = Vector{maxBytes, maxCount, maxCount}
			}
			l.Total = Vector{maxBytes, maxCount, maxCount}
			v := l.Category[History]
			if r.Limit > 0 {
				v.Bytes = r.Limit
			}
			if r.FileLimit > 0 {
				v.Files = r.FileLimit
			}
			if r.DirectoryLimit > 0 {
				v.Directories = r.DirectoryLimit
			}
			l.Category[History] = v
			var used, p [Categories]Vector
			used[History] = Vector{r.Used, r.UsedFiles, r.UsedDirectories}
			p[History] = Vector{r.Promised, r.PromisedFiles, r.PromisedDirectories}
			e := limitsCheck(used, p, l)
			if (e == nil) != r.Allowed {
				t.Fatalf("allowed=%v, error=%v", r.Allowed, e)
			}
		})
	}
}
func TestHandwrittenStateVectors(t *testing.T) {
	var rows []struct {
		Old, New string
		Allowed  bool
	}
	b, e := os.ReadFile("testdata/states.json")
	must(t, e)
	must(t, json.Unmarshal(b, &rows))
	states := map[string]State{"empty": Empty, "reserved": Reserved, "staging": Staging, "dispatched": Dispatched, "uncertain": Uncertain, "terminal": Terminal}
	base := Ticket{Token: Token{0, ident(1), 1, 1}, State: Reserved, Owner: owner, Components: request(1).Components, Manifest: digest(3)}
	for _, r := range rows {
		t.Run(r.Old+" to "+r.New, func(t *testing.T) {
			old, new := base, base
			old.State = states[r.Old]
			new.State = states[r.New]
			new.Token.Generation = 2
			a := applied{NextSequence: 1}
			if old.State == Empty {
				old = Ticket{Token: Token{Slot: 0}}
				new.Token.Generation = 1
			}
			e := legalTransition(old, new, a)
			if (e == nil) != r.Allowed {
				t.Fatalf("allowed=%v got %v", r.Allowed, e)
			}
		})
	}
}
func TestAtomicAdmissionAndRestart(t *testing.T) {
	q, path := fixture(t)
	token, e := q.Reserve(t.Context(), request(6), observations())
	must(t, e)
	_, e = q.Reserve(t.Context(), request(5), observations())
	if !errors.Is(e, ErrLimit) {
		t.Fatalf("wanted quota, got %v", e)
	}
	// Exercise the original checkpoint API, whose fixed encoder and writeAt
	// path publish a complete 4 MiB page with checked Sync/Close/readback.
	// Extent checks alone do not establish the syscall's write size.
	if CheckpointBytes != 4194304 {
		t.Fatal("original checkpoint operation no longer has the required 4 MiB extent")
	}
	must(t, q.Checkpoint(t.Context()))
	for _, name := range []string{"checkpoint.a", "checkpoint.b"} {
		info, e := os.Stat(filepath.Join(path, name))
		must(t, e)
		if !info.Mode().IsRegular() || info.Size() != 4194304 {
			t.Fatal("original checkpoint extent changed", name, info.Size())
		}
	}
	must(t, q.Close())
	other, r, w := openFixture(t, path)
	defer other.Close()
	defer r.Close()
	defer w.Close()
	s, e := other.Snapshot(t.Context())
	must(t, e)
	plan, e := other.Manifest(t.Context(), token, owner)
	must(t, e)
	if len(plan.Entries) != 1 || plan.Entries[0].Target != "chat.json" {
		t.Fatal("lost exact manifest")
	}
	if s.Promised[History].Bytes != 6 || len(s.Tickets) != 1 || s.Tickets[0].Token != token {
		t.Fatalf("lost promise: %+v", s)
	}
}

// **Advisory limits admit, charge and report; headroom still refuses.** With
// AdvisoryLimits, a promise past the History limit is admitted and charged, and
// Snapshot reports usage and promises over the limit. A promise that would
// leave less than the disk's headroom is still refused, and admits nothing.
func TestAdvisoryLimitsAdmitButHeadroomStillRefuses(t *testing.T) {
	q, path := fixture(t)
	must(t, q.Close())
	r, e := os.OpenRoot(path)
	must(t, e)
	w, e := r.OpenFile("lock", os.O_RDWR|noFollow, 0)
	must(t, e)
	advisory, e := OpenHeld(HeldAnchor{Root: r, LockWitness: w, Installation: ident(30), Binding: digest(30), AdvisoryLimits: true})
	must(t, e)
	t.Cleanup(func() { advisory.Close(); w.Close(); r.Close() })
	first, e := advisory.Reserve(t.Context(), request(6), observations())
	must(t, e)
	second, e := advisory.Reserve(t.Context(), request(5), observations())
	if e != nil {
		t.Fatalf("an advisory limit refused growth: %v", e)
	}
	if first == second {
		t.Fatal("the second promise reused the first")
	}
	s, e := advisory.Snapshot(t.Context())
	must(t, e)
	if s.Promised[History].Bytes != 11 || s.Promised[History].le(fixturePolicy().Installation.Category[History]) {
		t.Fatalf("the promises over the limit were not charged: %+v", s.Promised[History])
	}
	o := observations()
	o[0].Available.Bytes = 100
	if _, e = advisory.Reserve(t.Context(), request(1), o); !errors.Is(e, ErrHeadroom) {
		t.Fatalf("headroom was not kept with advisory limits: %v", e)
	}
	s, e = advisory.Snapshot(t.Context())
	must(t, e)
	if s.Promised[History].Bytes != 11 || len(s.Tickets) != 2 {
		t.Fatal("a refused promise was admitted")
	}
}
func TestMultipleFilesystemsAllOrNone(t *testing.T) {
	q, _ := fixture(t)
	r := request(4)
	c := r.Components[0]
	c.Root = ident(3)
	c.Filesystem = ident(21)
	c.Workspace = ident(11)
	c = fixtureComponent(c, fixtureRegistry().Roots[2])
	r.Components = append(r.Components, c)
	o := observations()
	o[1].Available.Bytes = 100
	_, e := q.Reserve(t.Context(), r, o)
	if !errors.Is(e, ErrHeadroom) {
		t.Fatalf("wanted headroom, got %v", e)
	}
	s, e := q.Snapshot(t.Context())
	must(t, e)
	if len(s.Tickets) != 0 || !s.Promised[History].zero() {
		t.Fatal("partial admission")
	}
}
func TestSharedAttributionAndNamespaceBounds(t *testing.T) {
	q, _ := fixture(t)
	r := request(6)
	c := r.Components[0]
	c.Workspace = ident(11)
	c.Physical = Vector{}
	c = fixtureComponent(c, fixtureRegistry().Roots[1])
	r.Components = append(r.Components, c)
	_, e := q.Reserve(t.Context(), r, observations())
	must(t, e)
	s, e := q.Snapshot(t.Context())
	must(t, e)
	if s.Promised[History].Bytes != 6 {
		t.Fatal("workspace attribution multiplied physical charge")
	}
	r = request(0)
	r.Components[0].Physical = Vector{Files: 10}
	r.Components[0].Attribution = Vector{Files: 10}
	r.Components[0] = fixtureComponent(r.Components[0], fixtureRegistry().Roots[1])
	_, e = q.Reserve(t.Context(), r, observations())
	if !errors.Is(e, ErrLimit) {
		t.Fatalf("zero-byte files bypassed bound: %v", e)
	}
}
func TestSettlementExactlyOnceAndSlotReuse(t *testing.T) {
	q, _ := fixture(t)
	tok, e := q.Reserve(t.Context(), request(6), observations())
	must(t, e)
	f := SettlementFact{Token: tok, Owner: owner, Evidence: digest(8), Changes: []Change{{PhysicalAdd: Vector{4, 1, 0}, AttributionAdd: Vector{4, 1, 0}}}}
	terminal, e := q.Settle(t.Context(), f)
	must(t, e)
	same, e := q.Settle(t.Context(), f)
	must(t, e)
	if same != terminal {
		t.Fatal("settlement not idempotent")
	}
	s, e := q.Snapshot(t.Context())
	must(t, e)
	if s.Installation[History].Bytes != 4 {
		t.Fatal("delta not once")
	}
	must(t, q.Checkpoint(t.Context()))
	s, e = q.Snapshot(t.Context())
	must(t, e)
	if s.Installation[History].Bytes != 4 {
		t.Fatal("delta applied twice")
	}
	next, e := q.Reserve(t.Context(), request(6), observations())
	must(t, e)
	if next.Slot != tok.Slot || next.Sequence <= tok.Sequence {
		t.Fatal("slot not safely reused")
	}
	_, e = q.Reserve(t.Context(), request(1), observations())
	if !errors.Is(e, ErrLimit) {
		t.Fatalf("reuse minted credit: %v", e)
	}
}
func TestReverseSlotDependentSettlement(t *testing.T) {
	for _, row := range []struct {
		name   string
		change Vector
	}{
		{"files", Vector{6, 1, 0}},
		{"directories", Vector{6, 0, 1}},
	} {
		t.Run(row.name, func(t *testing.T) {
			q, path := fixture(t)
			cleanup := request(1)
			cleanup.Operation = "cleanup"
			publication := request(6)
			publication.Operation = "publication"
			publication.Components[0].Physical = row.change
			publication.Components[0].Attribution = row.change
			publication.Components[0] = fixtureComponent(publication.Components[0], fixtureRegistry().Roots[1])
			publication.Entries[0].Files = row.change.Files
			publication.Entries[0].Directories = row.change.Directories
			low, e := q.Reserve(t.Context(), cleanup, observations())
			must(t, e)
			high, e := q.Reserve(t.Context(), publication, observations())
			must(t, e)
			if low.Slot >= high.Slot {
				t.Fatal("regression requires reverse slot settlement order")
			}
			addition := SettlementFact{Token: high, Owner: owner, Evidence: digest(8), Changes: []Change{{PhysicalAdd: row.change, AttributionAdd: row.change}}}
			highTerminal, e := q.Settle(t.Context(), addition)
			must(t, e)
			added, e := q.Snapshot(t.Context())
			must(t, e)
			if added.Installation[History] != row.change {
				t.Fatal("publication lost physical namespace accounting")
			}
			for _, root := range added.Roots {
				want := Vector{}
				if root.ID == ident(2) {
					want = row.change
				}
				if root.Usage[History] != want {
					t.Fatal("publication charged wrong root or category")
				}
			}
			for _, workspace := range added.Workspaces {
				want := Vector{}
				if workspace.ID == ident(10) {
					want = row.change
				}
				if workspace.Usage[History] != want {
					t.Fatal("publication charged wrong workspace or multiplied attribution")
				}
			}
			// The original owner has observed this exact object's deletion under
			// its domain lease; B1 records those facts without repeating effects.
			removal := SettlementFact{Token: low, Owner: owner, Evidence: digest(6), Changes: []Change{{PhysicalRemove: row.change, AttributionRemove: row.change}}}
			lowTerminal, e := q.Settle(t.Context(), removal)
			must(t, e)
			other, r, w := openFixture(t, path)
			defer other.Close()
			defer r.Close()
			defer w.Close()
			assertZero := func() Snapshot {
				t.Helper()
				s, e := other.Snapshot(t.Context())
				must(t, e)
				if !s.Installation[History].zero() || !s.Promised[History].zero() {
					t.Fatal("dependent settlements lost or duplicated installation credit")
				}
				for _, root := range s.Roots {
					if !root.Usage[History].zero() {
						t.Fatal("dependent settlements lost root credit")
					}
				}
				for _, workspace := range s.Workspaces {
					if !workspace.Usage[History].zero() {
						t.Fatal("dependent settlements lost or multiplied workspace credit")
					}
				}
				return s
			}
			s := assertZero()
			if len(s.Tickets) != 1 || s.Tickets[0].Token != lowTerminal {
				t.Fatal("prior terminal not checkpoint-retired or new terminal retired early")
			}
			same, e := other.Settle(t.Context(), addition)
			must(t, e)
			if same != highTerminal {
				t.Fatal("incorporated addition not idempotent")
			}
			same, e = other.Settle(t.Context(), removal)
			must(t, e)
			if same != lowTerminal || assertZero().Revision != s.Revision {
				t.Fatal("exact repeated settlement changed durable accounting")
			}
			must(t, other.Checkpoint(t.Context()))
			reopened, rr, ww := openFixture(t, path)
			defer reopened.Close()
			defer rr.Close()
			defer ww.Close()
			other = reopened
			if len(assertZero().Tickets) != 0 {
				t.Fatal("checkpoint did not retire exact terminal tuples")
			}
			lowNext, e := other.Reserve(t.Context(), request(4), observations())
			must(t, e)
			highNext, e := other.Reserve(t.Context(), request(6), observations())
			must(t, e)
			if lowNext.Slot != low.Slot || highNext.Slot != high.Slot || lowNext.Sequence != low.Sequence+1 || highNext.Sequence != high.Sequence+1 || lowNext.ID == low.ID || highNext.ID == high.ID {
				t.Fatal("slots reused without exact checkpoint and sequence advancement")
			}
			_, e = other.Reserve(t.Context(), request(1), observations())
			if !errors.Is(e, ErrLimit) {
				t.Fatalf("replayed deletion minted quota: %v", e)
			}
		})
	}
}
func TestSettlementPrerequisiteCheckpointCloseFailure(t *testing.T) {
	q, path := fixture(t)
	low, e := q.Reserve(t.Context(), request(1), observations())
	must(t, e)
	high, e := q.Reserve(t.Context(), request(6), observations())
	must(t, e)
	_, e = q.Settle(t.Context(), SettlementFact{Token: high, Owner: owner, Evidence: digest(8), Changes: []Change{{PhysicalAdd: Vector{6, 1, 0}, AttributionAdd: Vector{6, 1, 0}}}})
	must(t, e)
	// Pin both lower-slot pages before the prerequisite publication. A complete
	// newer checkpoint may survive the reported Close failure, but neither
	// lower-slot page may be touched until successful prerequisite return.
	var before [2][]byte
	must(t, q.locked(t.Context(), func() error {
		l, e := q.load()
		if e != nil {
			return e
		}
		for i, name := range []string{"tickets.a", "tickets.b"} {
			before[i], e = q.readAt(name, int64(low.Slot)*TicketBytes, TicketBytes)
			if e != nil {
				return e
			}
		}
		name := "checkpoint.a"
		if l.cpBank == 0 {
			name = "checkpoint.b"
		}
		q.fault = &ioFault{Name: name, Phase: "close", Offset: 0}
		return nil
	}))
	removal := SettlementFact{Token: low, Owner: owner, Evidence: digest(6), Changes: []Change{{PhysicalRemove: Vector{6, 1, 0}, AttributionRemove: Vector{6, 1, 0}}}}
	_, e = q.Settle(t.Context(), removal)
	if !errors.Is(e, ErrUnknown) || q.fault != nil {
		t.Fatalf("prerequisite Close failure not returned: %v", e)
	}
	for i, name := range []string{"tickets.a", "tickets.b"} {
		after, e := q.readAt(name, int64(low.Slot)*TicketBytes, TicketBytes)
		must(t, e)
		if string(before[i]) != string(after) {
			t.Fatal("failed prerequisite wrote the new terminal")
		}
	}
	other, r, w := openFixture(t, path)
	defer other.Close()
	defer r.Close()
	defer w.Close()
	s, e := other.Snapshot(t.Context())
	must(t, e)
	if s.Installation[History] != (Vector{6, 1, 0}) || s.Promised[History] != (Vector{1, 1, 0}) || len(s.Tickets) != 1 || s.Tickets[0].Token != low || s.Tickets[0].State != Reserved {
		t.Fatal("uncertain checkpoint lost original reservation or applied deletion")
	}
	plan, e := other.Manifest(t.Context(), low, owner)
	must(t, e)
	if len(plan.Entries) != 1 || plan.Entries[0].Target != "chat.json" {
		t.Fatal("failed prerequisite lost recoverable original plan")
	}
	// Re-submit only the owner's identical persistence fact; no effect executes.
	_, e = other.Settle(t.Context(), removal)
	must(t, e)
	must(t, other.Checkpoint(t.Context()))
	s, e = other.Snapshot(t.Context())
	must(t, e)
	if !s.Installation[History].zero() || !s.Promised[History].zero() || len(s.Tickets) != 0 {
		t.Fatal("owned persistence reconciliation duplicated or lost credit")
	}
}
func TestRetiredManifestNeverDereferenced(t *testing.T) {
	q, _ := fixture(t)
	tok, e := q.Reserve(t.Context(), request(6), observations())
	must(t, e)
	_, e = q.Settle(t.Context(), SettlementFact{Token: tok, Owner: owner, Evidence: digest(8), Changes: []Change{{}}})
	must(t, e)
	must(t, q.Checkpoint(t.Context()))
	// A new immutable manifest lands, then reservation persistence refuses before
	// its ticket write. Old terminal + older reserved pages still coexist.
	q.fault = &ioFault{Name: "tickets.b", Phase: "write", Offset: 0}
	_, e = q.Reserve(t.Context(), request(6), observations())
	if !errors.Is(e, ErrUnknown) {
		t.Fatal(e)
	}
	s, e := q.Snapshot(t.Context())
	must(t, e)
	if len(s.Tickets) != 0 || !s.Promised[History].zero() {
		t.Fatal("retired manifest read or old delta replayed")
	}
}
func TestReservationIOFailures(t *testing.T) {
	for _, phase := range []string{"write", "sync", "close", "readback"} {
		t.Run(phase, func(t *testing.T) {
			q, path := fixture(t)
			q.fault = &ioFault{Name: "tickets.b", Phase: phase, Offset: 0, Short: phase == "write"}
			_, e := q.Reserve(t.Context(), request(6), observations())
			if !errors.Is(e, ErrUnknown) {
				t.Fatalf("wanted uncertain refusal, got %v", e)
			}
			other, r, w := openFixture(t, path)
			defer other.Close()
			defer r.Close()
			defer w.Close()
			s, e := other.Snapshot(t.Context())
			if phase == "write" {
				if !errors.Is(e, ErrUnknown) {
					t.Fatalf("torn new page was silently ignored: %v", e)
				}
			} else {
				must(t, e)
				if s.Promised[History].Bytes != 6 || len(s.CapacityPromised) != 1 || s.CapacityPromised[0].Peak != (CapacityVector{6, 1}) {
					t.Fatal("durable possible reservation lost")
				}
			}
		})
	}
}
func TestSettlementAndCheckpointTornWrites(t *testing.T) {
	for _, stage := range []string{"settlement", "checkpoint"} {
		t.Run(stage, func(t *testing.T) {
			q, _ := fixture(t)
			tok, e := q.Reserve(t.Context(), request(6), observations())
			must(t, e)
			f := SettlementFact{Token: tok, Owner: owner, Evidence: digest(8), Changes: []Change{{}}}
			if stage == "settlement" {
				q.fault = &ioFault{Name: "tickets.a", Phase: "write", Offset: 0, Short: true}
				_, e = q.Settle(t.Context(), f)
			} else {
				_, e = q.Settle(t.Context(), f)
				must(t, e)
				q.fault = &ioFault{Name: "checkpoint.b", Phase: "write", Offset: 0, Short: true}
				e = q.Checkpoint(t.Context())
			}
			if !errors.Is(e, ErrUnknown) {
				t.Fatal(e)
			}
			_, e = q.Snapshot(t.Context())
			if !errors.Is(e, ErrUnknown) {
				t.Fatal("torn page granted credit", e)
			}
			v, e := q.InspectRecovery(t.Context())
			must(t, e)
			if !v.ManifestValid[0] || v.ManifestReservations[0].Components[0].Physical.Bytes != 6 || v.ManifestReservations[0].Components[0].Capacity[0].Peak != (CapacityVector{6, 1}) {
				t.Fatal("conservative reservation not recoverable")
			}
		})
	}
}
func TestOwnerRepairRetainsPromiseAndAllowsRecovery(t *testing.T) {
	q, _ := fixture(t)
	q.fault = &ioFault{Name: "tickets.b", Phase: "write", Offset: 0, Short: true}
	_, e := q.Reserve(t.Context(), request(6), observations())
	if !errors.Is(e, ErrUnknown) {
		t.Fatal(e)
	}
	v, e := q.InspectRecovery(t.Context())
	must(t, e)
	f := RepairFact{Fingerprint: v.Fingerprint, FormatEvidence: RepairFormatEvidence(q.anchor, v.Fingerprint), Evidence: digest(5), Policy: fixturePolicy(), Registry: fixtureRegistry(), Census: fixtureCensus()}
	for i := range f.Slots {
		f.Slots[i].Disposition = digest(6)
	}
	f.Slots[0] = RecoverySlot{Request: request(6), Entries: request(6).Entries}
	tokens, e := q.Repair(t.Context(), f)
	must(t, e)
	s, e := q.Snapshot(t.Context())
	must(t, e)
	if s.Maintenance || s.Promised[History].Bytes != 6 || len(s.Tickets) != 1 || s.Tickets[0].State != Uncertain {
		t.Fatal("repair erased obligation or stayed locked")
	}
	_, e = q.Reserve(t.Context(), request(5), observations())
	if !errors.Is(e, ErrLimit) {
		t.Fatal(e)
	}
	_, e = q.Settle(t.Context(), SettlementFact{Token: tokens[0], Owner: owner, Evidence: digest(8), Changes: []Change{{}}})
	must(t, e)
	must(t, q.Checkpoint(t.Context()))
	_, e = q.Reserve(t.Context(), request(6), observations())
	must(t, e)
}
func TestManifestAndPolicyFormatBounds(t *testing.T) {
	q, _ := fixture(t)
	r := request(1)
	r.Components = make([]Component, MaxComponents+1)
	_, e := q.Reserve(t.Context(), r, observations())
	if !errors.Is(e, ErrBounds) {
		t.Fatal(e)
	}
	r = request(1)
	r.Entries = make([]Entry, MaxEntries+1)
	_, e = q.Reserve(t.Context(), r, observations())
	if !errors.Is(e, ErrBounds) {
		t.Fatal(e)
	}
	r = request(1)
	r.Entries = make([]Entry, 600)
	for i := range r.Entries {
		r.Entries[i] = Entry{Kind: 1, Target: strings.Repeat("a", 1024), Stage: strings.Repeat("b", 1024)}
	}
	_, e = q.Reserve(t.Context(), r, observations())
	if !errors.Is(e, ErrBounds) {
		t.Fatalf("cumulative manifest bytes unbounded: %v", e)
	}
	r = request(1)
	r.Entries[0].Target = "../foreign"
	_, e = q.Reserve(t.Context(), r, observations())
	if !errors.Is(e, ErrBounds) {
		t.Fatal(e)
	}
	s, e := q.Snapshot(t.Context())
	must(t, e)
	if len(s.Tickets) != 0 {
		t.Fatal("bounds refusal created ticket")
	}
	p := fixturePolicy()
	p.Installation.Total.Bytes = ^uint64(0)
	if p.valid() {
		t.Fatal("unbounded policy accepted")
	}
}
func TestOriginalStableLockCustody(t *testing.T) {
	q, path := fixture(t)
	other, r, w := openFixture(t, path)
	defer other.Close()
	defer r.Close()
	defer w.Close()
	must(t, flock(q.lock, true))
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, e := other.Snapshot(ctx)
	must(t, unflock(q.lock))
	if !errors.Is(e, ErrBusy) {
		t.Fatalf("independent lock descriptions did not contend: %v", e)
	}
	must(t, q.anchor.Root.Rename("lock", "old-lock"))
	replacement, e := q.anchor.Root.OpenFile("lock", os.O_CREATE|os.O_EXCL|os.O_RDWR|noFollow, 0600)
	must(t, e)
	replacement.Close()
	_, e = q.Reserve(t.Context(), request(1), observations())
	if !errors.Is(e, ErrAuthority) {
		t.Fatal("replaced lock admitted growth", e)
	}
}
func TestProcessAdmissionHelper(t *testing.T) {
	if os.Getenv("JP_STORAGE_HELPER") != "1" {
		return
	}
	q, r, w := openFixture(t, os.Getenv("JP_STORAGE_ROOT"))
	defer q.Close()
	defer r.Close()
	defer w.Close()
	fmt.Println("ready")
	line, e := bufio.NewReader(os.Stdin).ReadString('\n')
	must(t, e)
	if line != "go\n" {
		t.Fatal("barrier")
	}
	if cut := os.Getenv("JP_STORAGE_CUT"); cut != "" {
		processAdmissionCut(t, q, cut)
		return
	}
	operand := request(6)
	if os.Getenv("JP_STORAGE_FANOUT") == "1" {
		operand = fanoutFixtureRequest(1, 6)
	}
	_, e = q.Reserve(t.Context(), operand, observations())
	switch {
	case e == nil:
		fmt.Println("admitted")
	case errors.Is(e, ErrLimit):
		fmt.Println("refused")
	default:
		t.Fatal(e)
	}
}
func TestDistinctProcessesCannotOverAdmit(t *testing.T) {
	distinctProcessesAdmission(t, false)
}
func TestDistinctProcessesFanoutCannotOverAdmit(t *testing.T) {
	distinctProcessesAdmission(t, true)
}
func distinctProcessesAdmission(t *testing.T, fanout bool) {
	t.Helper()
	q, path := fixture(t) // pipes below remain owned by their exact command
	exe, e := os.Executable()
	must(t, e)
	var commands []*exec.Cmd
	var inputs []ioWriteCloser
	var readers []*bufio.Reader
	for i := 0; i < 2; i++ {
		cmd := exec.Command(exe, "-test.run=^TestProcessAdmissionHelper$", "-test.count=1")
		mode := "0"
		if fanout {
			mode = "1"
		}
		cmd.Env = append(os.Environ(), "JP_STORAGE_HELPER=1", "JP_STORAGE_ROOT="+path, "JP_STORAGE_FANOUT="+mode)
		in, e := cmd.StdinPipe()
		must(t, e)
		out, e := cmd.StdoutPipe()
		must(t, e)
		cmd.Stderr = os.Stderr
		must(t, cmd.Start())
		commands = append(commands, cmd)
		inputs = append(inputs, in)
		readers = append(readers, bufio.NewReader(out))
	}
	t.Cleanup(func() {
		for _, c := range commands {
			if c.ProcessState == nil {
				_ = c.Process.Kill()
				_ = c.Wait()
			}
		}
	})
	for _, r := range readers {
		line, e := r.ReadString('\n')
		must(t, e)
		if line != "ready\n" {
			t.Fatal(line)
		}
	}
	for _, in := range inputs {
		_, e := in.Write([]byte("go\n"))
		must(t, e)
		must(t, in.Close())
	}
	winners := 0
	for i, r := range readers {
		line, e := r.ReadString('\n')
		must(t, e)
		if line == "admitted\n" {
			winners++
		} else if line != "refused\n" {
			t.Fatal(line)
		}
		must(t, commands[i].Wait())
	}
	if winners != 1 {
		t.Fatalf("winners=%d", winners)
	}
	s, e := q.Snapshot(t.Context())
	must(t, e)
	want := uint64(6)
	if fanout {
		want = 1
	}
	if s.Promised[History].Bytes != want || len(s.Tickets) != 1 {
		t.Fatal("processes over-admitted")
	}
	if fanout {
		status, err := q.StatusSnapshot(t.Context())
		must(t, err)
		for _, w := range status.Workspaces {
			if w.Promises.Peak[History].Bytes != 6 {
				t.Fatal("process admission lost shared workspace promise")
			}
		}
	}
}

type ioWriteCloser interface {
	Write([]byte) (int, error)
	Close() error
}

func TestHandwrittenDimensionVector(t *testing.T) {
	var rows []struct {
		Name  string
		Value uint8
	}
	b, e := os.ReadFile("testdata/dimensions.json")
	must(t, e)
	must(t, json.Unmarshal(b, &rows))
	expected := map[string]uint8{"history": History, "retained": Retained, "attachments": Attachments, "memory": Memory, "control": Control, "disposable": Disposable, "binaries": Binaries, "transient": Transient, "settlement": Settlement}
	if len(rows) != Categories {
		t.Fatal("missing dimension")
	}
	for _, r := range rows {
		v, ok := expected[r.Name]
		if !ok || v != r.Value {
			t.Fatal(r)
		}
		delete(expected, r.Name)
	}
	if len(expected) != 0 {
		t.Fatal(expected)
	}
}
func TestFreshObservationAndCountHeadroom(t *testing.T) {
	for _, kind := range []string{"missing", "stale", "future", "unknown", "inodes"} {
		t.Run(kind, func(t *testing.T) {
			q, _ := fixture(t)
			o := observations()
			want := ErrUnknown
			switch kind {
			case "missing":
				o = nil
			case "stale":
				o[0].At = time.Now().Add(-2 * time.Minute)
			case "future":
				o[0].At = time.Now().Add(time.Hour)
			case "unknown":
				o[0].KnownResources = 0
			case "inodes":
				o[0].Available.Inodes = 10
				want = ErrHeadroom
			}
			_, e := q.Reserve(t.Context(), request(1), o)
			if !errors.Is(e, want) {
				t.Fatalf("wanted %v got %v", want, e)
			}
			s, e := q.Snapshot(t.Context())
			must(t, e)
			if len(s.Tickets) != 0 {
				t.Fatal("observation refusal wrote ticket")
			}
		})
	}
}
func TestConflictingSameGenerationIsUnknown(t *testing.T) {
	q, _ := fixture(t)
	tok, e := q.Reserve(t.Context(), request(6), observations())
	must(t, e)
	l, e := q.load()
	must(t, e)
	conflict := l.tickets[tok.Slot]
	conflict.Operation = "different-operation"
	b, e := encodeTicket(conflict, q.anchor)
	must(t, e)
	must(t, q.writeAt("tickets.a", 0, b))
	_, e = q.Snapshot(t.Context())
	if !errors.Is(e, ErrUnknown) {
		t.Fatalf("ambiguous generation admitted: %v", e)
	}
}
func TestRepairRejectsShrinkingOrStaleOwnerFacts(t *testing.T) {
	q, _ := fixture(t)
	_, e := q.Reserve(t.Context(), request(6), observations())
	must(t, e)
	v, e := q.InspectRecovery(t.Context())
	must(t, e)
	f := RepairFact{Fingerprint: v.Fingerprint, FormatEvidence: RepairFormatEvidence(q.anchor, v.Fingerprint), Evidence: digest(5), Policy: fixturePolicy(), Registry: fixtureRegistry(), Census: fixtureCensus()}
	for i := range f.Slots {
		f.Slots[i].Disposition = digest(6)
	}
	f.Slots[0] = RecoverySlot{Request: request(5)}
	_, e = q.Repair(t.Context(), f)
	if !errors.Is(e, ErrAuthority) {
		t.Fatalf("repair shrank unreadable outcome: %v", e)
	}
	f.Slots[0] = RecoverySlot{Request: request(6)}
	_, e = q.Repair(t.Context(), f)
	if !errors.Is(e, ErrAuthority) {
		t.Fatalf("repair discarded original locators: %v", e)
	}
	f.Slots[0].Entries = request(6).Entries
	f.Fingerprint = digest(1)
	_, e = q.Repair(t.Context(), f)
	if !errors.Is(e, ErrStale) {
		t.Fatalf("stale facts accepted: %v", e)
	}
}
func TestPolicyReductionKeepsPriorPromise(t *testing.T) {
	q, _ := fixture(t)
	_, e := q.Reserve(t.Context(), request(6), observations())
	must(t, e)
	s, e := q.Snapshot(t.Context())
	must(t, e)
	p := fixturePolicy()
	p.Installation.Category[History].Bytes = 5
	must(t, q.UpdatePolicy(t.Context(), s.Revision, p))
	_, e = q.Reserve(t.Context(), request(1), observations())
	if !errors.Is(e, ErrLimit) {
		t.Fatalf("lowered policy granted growth: %v", e)
	}
	s, e = q.Snapshot(t.Context())
	must(t, e)
	if s.Promised[History].Bytes != 6 {
		t.Fatal("policy reduction erased prior promise")
	}
}

func indexedID(n int) (id ID) { id[0] = byte(n); id[1] = byte(n >> 8); id[2] = byte(n >> 16); return }
func indexedDigest(n int) (d Digest) {
	d[0] = byte(n)
	d[1] = byte(n >> 8)
	d[2] = byte(n >> 16)
	return
}
func TestMaximumSerializedTablesAndCumulativeLayout(t *testing.T) {
	if CoordinatorBytes != 2*CheckpointBytes+2*Slots*TicketBytes+Slots*ManifestBytes {
		t.Fatal("cumulative layout arithmetic changed")
	}
	if CoordinatorBytes != 44040192 {
		t.Fatal("coordinator byte count changed")
	}
	if appliedOffset+Slots*128 != 3743744 {
		t.Fatal("applied table end changed")
	}
	c := &checkpoint{Generation: 1, Epoch: 1, Maintenance: true, Policy: fixturePolicy()}
	for i := range c.Applied {
		c.Applied[i].NextSequence = 1
	}
	for i := range c.Filesystems {
		c.Filesystems[i] = Filesystem{ID: indexedID(i + 1), Identity: indexedDigest(i + 1), Generation: 1, InodeModel: InodesFinite, CapabilityEvidence: indexedDigest(i + 1), ByteConstraint: indexedID(i*4 + 1), InodeConstraint: indexedID(i*4 + 1)}
	}
	for i := range c.CapacityConstraints {
		c.CapacityConstraints[i] = CapacityConstraint{ID: indexedID(i + 1), Identity: indexedDigest(i + 1), Generation: 1, Resources: CapacityBytes | CapacityInodes, Headroom: CapacityVector{100, 10}}
	}
	for i := range c.Workspaces {
		c.Workspaces[i] = Workspace{ID: indexedID(i + 1), Generation: 1, Limits: c.Policy.Workspace}
	}
	for i := range c.Roots {
		r := Root{ID: indexedID(i + 1), Owner: owner, Identity: indexedDigest(i + 1), Filesystem: c.Filesystems[i%MaxFilesystems].ID, Workspace: c.Workspaces[i%MaxWorkspaces].ID, Generation: 1, Locator: strings.Repeat("x", 256)}
		r.CapacityCount = 4
		for j := range r.CapacityRefs {
			r.CapacityRefs[j] = CapacityRef{indexedID((i%MaxFilesystems)*4 + j + 1), 1}
		}
		sort.Slice(r.CapacityRefs[:], func(a, b int) bool {
			return bytes.Compare(r.CapacityRefs[a].Constraint[:], r.CapacityRefs[b].Constraint[:]) < 0
		})
		var rows []CapacityConstraint
		for _, v := range c.CapacityConstraints {
			rows = append(rows, v)
		}
		binding, err := CapacityBindingFor(r, c.Filesystems[i%MaxFilesystems], rows)
		must(t, err)
		r.CapacityBinding = binding
		for k := range r.Usage {
			r.Usage[k] = Vector{1, 1, 1}
			c.Installation[k].Bytes++
			c.Installation[k].Files++
			c.Installation[k].Directories++
		}
		c.Roots[i] = r
	}
	a := HeldAnchor{Installation: ident(30), Binding: digest(30)}
	b, e := encodeCheckpoint(c, a)
	must(t, e)
	if len(b) != 4194304 {
		t.Fatal(len(b))
	}
	decoded, e := decodeCheckpoint(b, a)
	must(t, e)
	if decoded.Roots[4095].Locator != c.Roots[4095].Locator || decoded.Roots[4095].CapacityRefs != c.Roots[4095].CapacityRefs || decoded.CapacityConstraints[511] != c.CapacityConstraints[511] || decoded.Installation != c.Installation || decoded.Workspaces[1023].ID != c.Workspaces[1023].ID {
		t.Fatal("max tables lost fields")
	}
	b[len(b)-1] = 1
	seal(b, 1, 0, 1, a)
	_, e = decodeCheckpoint(b, a)
	if !errors.Is(e, ErrUnknown) {
		t.Fatal("nonzero reserved capacity accepted", e)
	}
	ticket := Ticket{Token: Token{63, ident(1), 1, 1}, State: Terminal, PolicyRevision: 1, Owner: owner, Workflow: strings.Repeat("w", 128), Operation: strings.Repeat("o", 128), Manifest: digest(2), ManifestLength: ManifestBytes, Evidence: digest(3)}
	for i := 0; i < MaxComponents; i++ {
		ticket.Components = append(ticket.Components, Component{Root: indexedID(i + 1), RootGeneration: 1, Owner: owner, Filesystem: ident(20), FilesystemGeneration: 1, Workspace: indexedID(i + 1), WorkspaceGeneration: 1, Category: uint8(i % Categories), Physical: Vector{1, 1, 1}, Attribution: Vector{1, 1, 1}, SettlementPhysical: Vector{1, 1, 1}, SettlementAttribution: Vector{1, 1, 1}})
		last := &ticket.Components[len(ticket.Components)-1]
		last.CapacityCount = 4
		last.CapacityBinding = digest(90)
		for j := range last.Capacity {
			last.Capacity[j] = CapacityPromise{indexedID(j + 1), 1, CapacityVector{2, 4}}
		}
		ticket.Changes = append(ticket.Changes, Change{PhysicalAdd: Vector{1, 1, 1}, AttributionAdd: Vector{1, 1, 1}})
	}
	b, e = encodeTicket(ticket, a)
	must(t, e)
	if len(b) != 16384 {
		t.Fatal(len(b))
	}
	got, e := decodeTicket(b, 63, a)
	must(t, e)
	if !sameComponents(got.Components, ticket.Components) || !changesEqual(got.Changes, ticket.Changes) {
		t.Fatal("max ticket lost fields")
	}
	if _, e = next(^uint64(0)); !errors.Is(e, ErrBounds) {
		t.Fatal("generation wrapped")
	}
}

func TestRepairRequiresLostManifestEvidence(t *testing.T) {
	q, _ := fixture(t)
	_, e := q.Reserve(t.Context(), request(6), observations())
	must(t, e)
	must(t, q.writeAt("manifests", 0, make([]byte, ManifestBytes))) // synthetic corruption, never a production cleanup
	v, e := q.InspectRecovery(t.Context())
	must(t, e)
	f := RepairFact{Fingerprint: v.Fingerprint, FormatEvidence: RepairFormatEvidence(q.anchor, v.Fingerprint), Evidence: digest(5), Policy: fixturePolicy(), Registry: fixtureRegistry(), Census: fixtureCensus()}
	for i := range f.Slots {
		f.Slots[i].Disposition = digest(6)
	}
	f.Slots[0] = RecoverySlot{Request: request(6), Entries: request(6).Entries}
	_, e = q.Repair(t.Context(), f)
	if !errors.Is(e, ErrAuthority) {
		t.Fatalf("missing exact plan was guessed: %v", e)
	}
	f.Slots[0].Reconstruction = digest(8)
	tokens, e := q.Repair(t.Context(), f)
	must(t, e)
	plan, e := q.Manifest(t.Context(), tokens[0], owner)
	must(t, e)
	if len(plan.Entries) != 1 || plan.Entries[0].Stage != "stage-ticket" || plan.Components[0].Physical.Bytes != 6 {
		t.Fatal("owner reconstruction lost promise or exact plan")
	}
}

// Handwritten complete-configuration vectors exercise each added result member,
// all filesystem fields, detached mutation, coherent revision and zero-on-error.
func TestConfigurationSnapshotDetachedAndReadOnly(t *testing.T) {
	q, path := fixture(t)
	before, e := q.ConfigurationSnapshot(t.Context())
	must(t, e)
	if before.Revision != 3 || before.Epoch != 1 || before.Maintenance || before.Policy != fixturePolicy() || len(before.Filesystems) != 2 || before.Filesystems[0] != fixtureRegistry().Filesystems[0] || before.Filesystems[1] != fixtureRegistry().Filesystems[1] {
		t.Fatalf("incomplete configuration: %+v", before)
	}
	hashes := configurationTestHashes(t, path)
	before.Filesystems[0] = Filesystem{}
	before.Policy.Installation.Total.Bytes = 1
	same, e := q.ConfigurationSnapshot(t.Context())
	must(t, e)
	if same.Policy != fixturePolicy() || same.Filesystems[0] != fixtureRegistry().Filesystems[0] {
		t.Fatal("snapshot aliases durable configuration")
	}
	if !reflect.DeepEqual(hashes, configurationTestHashes(t, path)) {
		t.Fatal("snapshot wrote storage")
	}
	p := fixturePolicy()
	p.ObservationMaxAge = 2 * time.Minute
	p.Installation.Total.Bytes++
	must(t, q.UpdatePolicy(t.Context(), same.Revision, p))
	updated, e := q.ConfigurationSnapshot(t.Context())
	must(t, e)
	if updated.Revision != same.Revision+1 || updated.Policy != p || updated.Epoch != same.Epoch || updated.Maintenance != same.Maintenance || !reflect.DeepEqual(updated.Filesystems, same.Filesystems) {
		t.Fatal("mixed configuration revision")
	}
	epoch, e := q.BeginMaintenance(t.Context(), updated.Revision)
	must(t, e)
	maintained, e := q.ConfigurationSnapshot(t.Context())
	must(t, e)
	if !maintained.Maintenance || maintained.Epoch != epoch || maintained.Revision != updated.Revision+1 || maintained.Policy != p {
		t.Fatal("maintenance state omitted")
	}
}
func configurationTestHashes(t *testing.T, path string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	for _, name := range []string{"lock", "checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests"} {
		b, e := os.ReadFile(filepath.Join(path, name))
		must(t, e)
		out[name] = sha256.Sum256(b)
	}
	return out
}
func TestConfigurationSnapshotCorruptionAndUnknown(t *testing.T) {
	for _, name := range []string{"checkpoint.a", "tickets.b", "missing"} {
		t.Run(name, func(t *testing.T) {
			q, path := fixture(t)
			if name == "missing" {
				must(t, os.Rename(filepath.Join(path, "checkpoint.a"), filepath.Join(path, "checkpoint-retained")))
			} else {
				f, e := os.OpenFile(filepath.Join(path, name), os.O_WRONLY, 0)
				must(t, e)
				_, e = f.WriteAt([]byte("!"), 0)
				must(t, e)
				must(t, f.Close())
			}
			// Compare all surviving exact fixture bytes, including renamed evidence.
			names, e := os.ReadDir(path)
			must(t, e)
			before := map[string][32]byte{}
			for _, n := range names {
				b, e := os.ReadFile(filepath.Join(path, n.Name()))
				must(t, e)
				before[n.Name()] = sha256.Sum256(b)
			}
			result, e := q.ConfigurationSnapshot(t.Context())
			if e == nil || !reflect.DeepEqual(result, Configuration{}) {
				t.Fatalf("unknown configuration returned data: %+v %v", result, e)
			}
			for name, want := range before {
				b, e := os.ReadFile(filepath.Join(path, name))
				must(t, e)
				if sha256.Sum256(b) != want {
					t.Fatal("refusal repaired or wrote storage")
				}
			}
		})
	}
}
func TestConfigurationSnapshotFiniteFilesystems(t *testing.T) {
	q, path := fixture(t)
	_ = path
	snap, e := q.ConfigurationSnapshot(t.Context())
	must(t, e)
	_, e = q.BeginMaintenance(t.Context(), snap.Revision)
	must(t, e)
	snap, e = q.ConfigurationSnapshot(t.Context())
	must(t, e)
	r := Registration{}
	for i := 2; i < MaxFilesystems; i++ {
		var id ID
		id[0] = byte(i)
		id[1] = byte(i >> 8)
		id[15] = 1
		var identity Digest
		copy(identity[:], id[:])
		r.Filesystems = append(r.Filesystems, Filesystem{ID: id, Identity: identity, Generation: uint64(i + 1), InodeModel: InodesFinite, CapabilityEvidence: identity, ByteConstraint: ident(60), InodeConstraint: ident(60)})
	}
	must(t, q.Register(t.Context(), snap.Revision, r))
	full, e := q.ConfigurationSnapshot(t.Context())
	must(t, e)
	if len(full.Filesystems) != MaxFilesystems || full.Filesystems[MaxFilesystems-1] != r.Filesystems[len(r.Filesystems)-1] {
		t.Fatal("finite configuration rows truncated")
	}
	var id ID
	id[0] = 255
	id[15] = 2
	if e = q.Register(t.Context(), full.Revision, Registration{Filesystems: []Filesystem{{ID: id, Identity: digest(254), Generation: 1}}}); !errors.Is(e, ErrBounds) {
		t.Fatalf("finite capacity overflow accepted: %v", e)
	}
	after, e := q.ConfigurationSnapshot(t.Context())
	must(t, e)
	if !reflect.DeepEqual(full, after) {
		t.Fatal("failed registration changed configuration")
	}
}

// No runtime fixture is recorded from the initializer. Each syscall name and
// phase below is handwritten. Failed partial initialization is never repaired.
func TestInitializeHeldHandwrittenFaultBoundaries(t *testing.T) {
	var faults []ioFault
	for _, name := range []string{"checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests"} {
		for _, phase := range []string{"initialize-create", "initialize-write", "initialize-sync", "initialize-close"} {
			faults = append(faults, ioFault{Name: name, Phase: phase, Offset: 0})
		}
		faults = append(faults, ioFault{Name: name, Phase: "initialize-write", Offset: 0, Short: true})
	}
	faults = append(faults, ioFault{Name: ".", Phase: "initialize-sync", Offset: 0}, ioFault{Name: ".", Phase: "initialize-close", Offset: 0}, ioFault{Name: ".", Phase: "initialize-readback", Offset: 0})
	for _, fault := range faults {
		fault := fault
		label := fault.Name + "-" + fault.Phase
		if fault.Short {
			label += "-short"
		}
		t.Run(label, func(t *testing.T) {
			path := t.TempDir()
			must(t, os.Chmod(path, 0700))
			root, e := os.OpenRoot(path)
			must(t, e)
			defer root.Close()
			witness, e := root.OpenFile("lock", os.O_CREATE|os.O_EXCL|os.O_RDWR|noFollow, 0600)
			must(t, e)
			defer witness.Close()
			q, e := OpenHeld(HeldAnchor{root, witness, ident(30), digest(30), false})
			must(t, e)
			defer q.Close()
			q.fault = &fault
			if e = q.InitializeHeld(t.Context(), fixturePolicy()); !errors.Is(e, ErrUnknown) {
				t.Fatalf("bootstrap fault acknowledged: %v", e)
			}
			if q.fault != nil {
				t.Fatal("handwritten fault boundary was not reached")
			}
			names, e := os.ReadDir(path)
			must(t, e)
			before := map[string][32]byte{}
			for _, n := range names {
				b, e := os.ReadFile(filepath.Join(path, n.Name()))
				must(t, e)
				before[n.Name()] = sha256.Sum256(b)
			}
			// If no file was created there is no partial shape; the host's separate
			// immutable claim still governs whether its whole bootstrap can retry.
			if len(names) > 1 {
				if e = q.InitializeHeld(t.Context(), fixturePolicy()); !errors.Is(e, ErrUnknown) {
					t.Fatal("partial shape reinitialized")
				}
			}
			current, e := os.ReadDir(path)
			must(t, e)
			if len(current) != len(names) {
				t.Fatal("partial retry changed namespace")
			}
			for name, want := range before {
				b, e := os.ReadFile(filepath.Join(path, name))
				must(t, e)
				if sha256.Sum256(b) != want {
					t.Fatal("partial retry changed retained bytes")
				}
			}
			second, e := OpenHeld(HeldAnchor{root, witness, ident(30), digest(30), false})
			must(t, e)
			must(t, second.Close())
		})
	}
}

// Reuse the actual two-process admission fixture. Error phases are deliberate
// ioFault injections; the following PID death and flock release are real.
func processAdmissionCut(t *testing.T, q *Coordinator, cut string) {
	t.Helper()
	if cut != "before-reserve" {
		switch cut {
		case "reserved":
		case "write", "sync", "close", "readback":
			q.fault = &ioFault{Name: "tickets.b", Phase: cut, Offset: 0, Short: cut == "write"}
		default:
			t.Fatal("unknown process cut")
		}
		_, err := q.Reserve(t.Context(), request(6), observations())
		if cut == "reserved" {
			must(t, err)
		} else if !errors.Is(err, ErrUnknown) {
			t.Fatalf("injected publication must stay uncertain: %v", err)
		}
	}
	// Hold the original Q exclusion description across the barrier. A peer
	// must wait until the process is dead, then load all original disk banks.
	must(t, flock(q.lock, true))
	fmt.Println("cut-" + cut)
	_, err := bufio.NewReader(os.Stdin).ReadString('\n')
	t.Fatalf("process cut was not killed: %v", err)
}

func TestProcessDeathRetainsOriginalAdmissionAndUncertainty(t *testing.T) {
	// A normal nonzero/test-timeout exit or a different signal is not this cut.
	for _, row := range []struct {
		signaled bool
		signal   syscall.Signal
		want     bool
	}{
		{true, syscall.SIGKILL, true},
		{false, -1, false},
		{true, syscall.SIGTERM, false},
		{false, syscall.SIGKILL, false},
	} {
		if got := processCutSIGKILL(row.signaled, row.signal); got != row.want {
			t.Fatalf("Q death status predicate: signaled=%v signal=%v got=%v", row.signaled, row.signal, got)
		}
	}
	for _, cut := range []string{"before-reserve", "reserved", "write", "sync", "close", "readback"} {
		t.Run(cut, func(t *testing.T) {
			q, path := fixture(t)
			exe, err := os.Executable()
			must(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestProcessAdmissionHelper$", "-test.count=1", "-test.timeout=15s")
			cmd.Env = append(os.Environ(), "JP_STORAGE_HELPER=1", "JP_STORAGE_ROOT="+path, "JP_STORAGE_CUT="+cut)
			in, err := cmd.StdinPipe()
			must(t, err)
			defer in.Close()
			out, err := cmd.StdoutPipe()
			must(t, err)
			cmd.Stderr = os.Stderr
			must(t, cmd.Start())
			done := false
			defer func() {
				if !done {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			reader := bufio.NewReader(out)
			line, err := reader.ReadString('\n')
			must(t, err)
			if line != "ready\n" {
				t.Fatalf("missing ready barrier: %q", line)
			}
			_, err = in.Write([]byte("go\n"))
			must(t, err)
			line, err = reader.ReadString('\n')
			must(t, err)
			if line != "cut-"+cut+"\n" {
				t.Fatalf("missing cut barrier: %q", line)
			}
			blocked, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
			_, err = q.Snapshot(blocked)
			stop()
			if !errors.Is(err, ErrBusy) {
				t.Fatalf("original process lock bypassed: %v", err)
			}
			must(t, cmd.Process.Kill())
			waitErr := cmd.Wait()
			done = true
			if waitErr == nil || ctx.Err() != nil {
				t.Fatalf("wanted explicit PID death, wait=%v context=%v", waitErr, ctx.Err())
			}
			if cmd.ProcessState == nil {
				t.Fatal("Q death child has no joined process state")
			}
			status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
			if !ok || !processCutSIGKILL(status.Signaled(), status.Signal()) {
				t.Fatalf("Q death child did not terminate by SIGKILL: %v", cmd.ProcessState)
			}
			snapshot, err := q.Snapshot(t.Context())
			if cut == "before-reserve" {
				must(t, err)
				if len(snapshot.Tickets) != 0 || !snapshot.Promised[History].zero() {
					t.Fatal("pre-admission death invented debt")
				}
				_, err = q.Reserve(t.Context(), request(6), observations())
				must(t, err) // positive control: same original lock/Q admits after death.
				return
			}
			want := ErrLimit
			if cut == "write" {
				want = ErrUnknown
				if !errors.Is(err, ErrUnknown) {
					t.Fatalf("torn page ignored after death: %v", err)
				}
			} else {
				must(t, err)
				if len(snapshot.Tickets) != 1 || snapshot.Tickets[0].State != Reserved || snapshot.Tickets[0].Owner != owner || snapshot.Promised[History].Bytes != 6 || len(snapshot.CapacityPromised) != 1 || snapshot.CapacityPromised[0].Peak != (CapacityVector{6, 1}) {
					t.Fatal("death lost original logical/capacity reservation")
				}
				plan, err := q.Manifest(t.Context(), snapshot.Tickets[0].Token, owner)
				must(t, err)
				if !reflect.DeepEqual(plan, request(6)) {
					t.Fatal("death lost exact original manifest")
				}
			}
			before := processCutDiskDigests(t, path)
			_, err = q.Reserve(t.Context(), request(6), observations())
			if !errors.Is(err, want) {
				t.Fatalf("death granted a fresh admission: %v", err)
			}
			if after := processCutDiskDigests(t, path); before != after {
				t.Fatal("refused admission changed original Q or replayed publication")
			}
			if cut == "write" {
				view, err := q.InspectRecovery(t.Context())
				must(t, err)
				if !view.ManifestValid[0] {
					t.Fatal("torn publication discarded inspectable original manifest")
				}
				original := view.ManifestReservations[0]
				plan := Request{Workflow: original.Workflow, Operation: original.Operation, Owner: original.Owner, Components: original.Components, Fanout: original.Fanout, Entries: view.ManifestEntries[0]}
				if !reflect.DeepEqual(plan, request(6)) {
					t.Fatal("torn publication lost exact original request or effect entries")
				}
				if original.Components[0].Physical.Bytes != 6 || original.Components[0].Capacity[0].Peak != (CapacityVector{6, 1}) {
					t.Fatal("torn publication discarded inspectable original debt")
				}
				// This first reservation wrote only 32 bytes of tickets.b. The
				// intact original manifest is not a valid new ticket-bank page.
				if view.TicketValid[0] != ([2]bool{true, false}) || view.TicketCandidates[0][0].State != Empty || view.TicketCandidates[0][0].Token != (Token{}) {
					t.Fatal("torn cut changed the original empty bank or accepted the damaged bank")
				}
				if !view.CheckpointValid[0] || !view.CheckpointValid[1] {
					t.Fatal("ticket cut damaged original checkpoints")
				}
				revision := view.CheckpointGenerations[0]
				if view.CheckpointGenerations[1] > revision {
					revision = view.CheckpointGenerations[1]
				}
				if original.State != Reserved || original.Token.Slot != 0 || original.Token.ID == (ID{}) || original.Token.Sequence != 1 || original.Token.Generation != 1 || original.PolicyRevision != revision || original.Manifest != (Digest{}) || original.ManifestLength != 0 || original.Evidence != (Digest{}) || len(original.Changes) != 0 {
					t.Fatal("torn cut lost the exact first reserved manifest identity/state")
				}
			}
			t.Log("actual PID death and original flock drain; publication errors injected, not actual errno or power loss")
		})
	}
}

func processCutDiskDigests(t *testing.T, path string) [5]Digest {
	t.Helper()
	var out [5]Digest
	for i, name := range []string{"checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests"} {
		raw, err := os.ReadFile(filepath.Join(path, name))
		must(t, err)
		out[i] = sha256.Sum256(raw)
	}
	return out
}

func processCutSIGKILL(signaled bool, signal syscall.Signal) bool {
	return signaled && signal == syscall.SIGKILL
}
