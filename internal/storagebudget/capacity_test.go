package storagebudget

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// These are synthetic original-owner metadata facts, not platform certification.
// Pool 60 is shared; restrictions 61/62 independently limit two volumes.
func capacityState(t *testing.T, model uint8) *checkpoint {
	t.Helper()
	r := fixtureRegistry()
	r.CapacityConstraints = []CapacityConstraint{
		{ID: ident(60), Identity: digest(60), Generation: 1, Resources: CapacityBytes},
		{ID: ident(61), Identity: digest(61), Generation: 1, Resources: CapacityBytes | CapacityInodes},
		{ID: ident(62), Identity: digest(62), Generation: 1, Resources: CapacityBytes | CapacityInodes},
	}
	for i := range r.Filesystems {
		f := &r.Filesystems[i]
		f.InodeModel = model
		f.ByteConstraint = ident(60)
		f.InodeConstraint = ident(byte(61 + i))
		if model == InodesDynamic {
			f.InodeConstraint = ID{}
		}
	}
	for i := range r.Roots {
		root := &r.Roots[i]
		f := r.Filesystems[0]
		if root.Filesystem == ident(21) {
			f = r.Filesystems[1]
		}
		root.Known = true
		root.CapacityCount = 1
		root.CapacityRefs = [MaxCapacityRefs]CapacityRef{{ident(60), 1}}
		if model == InodesFinite {
			root.CapacityCount = 2
			root.CapacityRefs[1] = CapacityRef{f.InodeConstraint, 1}
		}
		var e error
		root.CapacityBinding, e = CapacityBindingFor(*root, f, r.CapacityConstraints)
		must(t, e)
	}
	c := &checkpoint{Generation: 1, Epoch: 1, Policy: fixturePolicy()}
	c.Policy.Installation.Total = Vector{maxBytes, maxCount, maxCount}
	c.Policy.Workspace.Total = Vector{maxBytes, maxCount, maxCount}
	for i := 0; i < Categories; i++ {
		c.Policy.Installation.Category[i] = Vector{maxBytes, maxCount, maxCount}
		c.Policy.Workspace.Category[i] = Vector{maxBytes, maxCount, maxCount}
	}
	for i, v := range r.Roots {
		c.Roots[i] = v
	}
	for i, v := range r.Workspaces {
		v.Known = true
		v.Limits = c.Policy.Workspace
		c.Workspaces[i] = v
	}
	for i, v := range r.Filesystems {
		c.Filesystems[i] = v
	}
	for i, v := range r.CapacityConstraints {
		c.CapacityConstraints[i] = v
	}
	for i := range c.Applied {
		c.Applied[i].NextSequence = 1
	}
	must(t, validateCheckpoint(c))
	return c
}

func capacityRequest(c *checkpoint, rootIndex int, n uint64) Request {
	r := c.Roots[rootIndex]
	f := c.Filesystems[filesystemIndex(c, r.Filesystem)]
	x := Component{Root: r.ID, RootGeneration: r.Generation, Owner: r.Owner, Filesystem: f.ID, FilesystemGeneration: f.Generation, Category: History, Physical: Vector{Bytes: n, Files: 1}, CapacityCount: r.CapacityCount, CapacityBinding: r.CapacityBinding}
	for i := 0; i < int(r.CapacityCount); i++ {
		ref := r.CapacityRefs[i]
		row := c.CapacityConstraints[constraintIndex(c, ref.Constraint)]
		peak := CapacityVector{}
		if row.Resources&CapacityBytes != 0 {
			peak.Bytes = max(n, 1)
		}
		if row.Resources&CapacityInodes != 0 {
			peak.Inodes = 1
		}
		x.Capacity[i] = CapacityPromise{ref.Constraint, ref.Generation, peak}
	}
	return Request{Owner: owner, Workflow: "capacity", Operation: "two-volumes", Components: []Component{x}}
}
func capacityTicket(r Request, slot uint8) Ticket {
	return Ticket{Token: Token{slot, ident(100 + slot), 1, 1}, State: Reserved, PolicyRevision: 1, Owner: r.Owner, Workflow: r.Workflow, Operation: r.Operation, Components: r.Components}
}
func capacityObservations() []CapacityObservation {
	at := time.Unix(1000, 0)
	return []CapacityObservation{
		{Constraint: ident(60), Generation: 1, Available: CapacityVector{Bytes: 100}, At: at, KnownResources: CapacityBytes},
		{Constraint: ident(61), Generation: 1, Available: CapacityVector{10, 100}, At: at, KnownResources: CapacityBytes | CapacityInodes},
		{Constraint: ident(62), Generation: 1, Available: CapacityVector{90, 100}, At: at, KnownResources: CapacityBytes | CapacityInodes},
	}
}

func TestCapacityOverlapLogicalOnce(t *testing.T) {
	c := capacityState(t, InodesFinite)
	a, b := capacityRequest(c, 1, 8), capacityRequest(c, 2, 80)
	var tickets [Slots]Ticket
	tickets[0] = capacityTicket(a, 0)
	tickets[1] = capacityTicket(b, 1)
	must(t, admit(c, tickets, capacityObservations(), time.Unix(1000, 0)))
	logical, _, physical, e := promises(c, tickets)
	must(t, e)
	if logical[History] != (Vector{88, 2, 0}) || physical[0] != (CapacityVector{88, 0}) || physical[1] != (CapacityVector{8, 1}) || physical[2] != (CapacityVector{80, 1}) {
		t.Fatal("overlapping promises multiplied usage or merged independent restrictions")
	}
	clone := tickets[0].Components[0]
	clone.Workspace = ident(10)
	clone.WorkspaceGeneration = 1
	clone.Attribution = clone.Physical
	clone.Physical = Vector{}
	for i := range clone.Capacity {
		clone.Capacity[i].Peak = CapacityVector{}
	}
	tickets[0].Components = append(tickets[0].Components, clone)
	must(t, admit(c, tickets, capacityObservations(), time.Unix(1000, 0)))
	logical, _, physical, e = promises(c, tickets)
	must(t, e)
	if logical[History].Bytes != 88 || physical[0].Bytes != 88 {
		t.Fatal("workspace attribution duplicated physical promises")
	}
	for _, n := range []uint64{12, 21} {
		tickets[0] = capacityTicket(capacityRequest(c, 1, n), 0)
		if e = admit(c, tickets, capacityObservations(), time.Unix(1000, 0)); !errors.Is(e, ErrHeadroom) {
			t.Fatal("independent quota or shared limit bypassed", e)
		}
	}
}

func TestCapacityHandwrittenVectors(t *testing.T) {
	var vectors []struct {
		Name                          string
		Resources                     uint8
		Available, Headroom, Promised CapacityVector
		Allowed                       bool
	}
	b, e := os.ReadFile("testdata/capacity.json")
	must(t, e)
	must(t, json.Unmarshal(b, &vectors))
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			c := capacityState(t, InodesDynamic)
			if v.Resources == 0 || v.Resources & ^uint8(CapacityBytes|CapacityInodes) != 0 {
				c.CapacityConstraints[0].Resources = v.Resources
				if validateCheckpoint(c) == nil || v.Allowed {
					t.Fatal("unknown resource registry accepted")
				}
				return
			}
			row := 0
			if v.Resources == CapacityInodes {
				row = 1
				c.Roots[1].CapacityCount = 2
				c.Roots[1].CapacityRefs[1] = CapacityRef{ident(61), 1}
			}
			c.CapacityConstraints[row].Resources = v.Resources
			c.CapacityConstraints[row].Headroom = v.Headroom
			var rows []CapacityConstraint
			for _, r := range c.CapacityConstraints {
				if !r.ID.zero() {
					rows = append(rows, r)
				}
			}
			if !validConstraint(c.CapacityConstraints[row]) {
				if v.Allowed {
					t.Fatal("valid fixture constraint rejected")
				}
				return
			}
			for i := 0; i < 3; i++ {
				f := c.Filesystems[filesystemIndex(c, c.Roots[i].Filesystem)]
				binding, err := CapacityBindingFor(c.Roots[i], f, rows)
				must(t, err)
				c.Roots[i].CapacityBinding = binding
			}
			r := capacityRequest(c, 1, 1)
			x := &r.Components[0]
			if v.Resources&CapacityBytes != 0 {
				x.Physical.Bytes = v.Promised.Bytes
				x.Physical.Files = 0
			} else {
				x.Physical.Files = v.Promised.Inodes
			}
			if v.Resources&CapacityInodes != 0 {
				x.Physical.Files = v.Promised.Inodes
			}
			for i := 0; i < int(x.CapacityCount); i++ {
				if x.Capacity[i].Constraint == c.CapacityConstraints[row].ID {
					x.Capacity[i].Peak = v.Promised
				}
			}
			o := []CapacityObservation{{Constraint: ident(60), Generation: 1, Available: CapacityVector{Bytes: 100}, At: time.Unix(1000, 0), KnownResources: CapacityBytes}}
			observation := CapacityObservation{Constraint: c.CapacityConstraints[row].ID, Generation: 1, Available: v.Available, At: time.Unix(1000, 0), KnownResources: v.Resources}
			if row == 0 {
				o[0] = observation
			} else {
				o = append(o, observation)
			}
			var tickets [Slots]Ticket
			tickets[0] = capacityTicket(r, 0)
			err := admit(c, tickets, o, time.Unix(1000, 0))
			if (err == nil) != v.Allowed {
				t.Fatalf("handwritten capacity outcome mismatch: %v", err)
			}
		})
	}
	if _, e = capacityAdd(CapacityVector{maxBytes, 0}, CapacityVector{1, 0}); !errors.Is(e, ErrBounds) {
		t.Fatal("byte sum wrapped")
	}
	if _, e = capacityAdd(CapacityVector{0, maxCount}, CapacityVector{0, 1}); !errors.Is(e, ErrBounds) {
		t.Fatal("inode sum wrapped")
	}
}

func TestCapacityFiniteDynamicUnknown(t *testing.T) {
	c := capacityState(t, InodesDynamic)
	var tickets [Slots]Ticket
	tickets[0] = capacityTicket(capacityRequest(c, 1, 8), 0)
	o := capacityObservations()[:1]
	must(t, admit(c, tickets, o, time.Unix(1000, 0)))
	c.Policy.Installation.Category[History].Files = 1
	tickets[1] = capacityTicket(capacityRequest(c, 2, 1), 1)
	if e := admit(c, tickets, o, time.Unix(1000, 0)); !errors.Is(e, ErrLimit) {
		t.Fatal("dynamic inodes bypassed finite Desk object budget", e)
	}
	tickets[1] = Ticket{}
	// A documented dynamic backing pool still has an actual finite object quota.
	c.CapacityConstraints[1].Resources = CapacityInodes
	r := &c.Roots[1]
	r.CapacityCount = 2
	r.CapacityRefs[1] = CapacityRef{ident(61), 1}
	var rows []CapacityConstraint
	for _, v := range c.CapacityConstraints {
		if !v.ID.zero() {
			rows = append(rows, v)
		}
	}
	var e error
	r.CapacityBinding, e = CapacityBindingFor(*r, c.Filesystems[0], rows)
	must(t, e)
	tickets[0] = capacityTicket(capacityRequest(c, 1, 8), 0)
	o = append(o, CapacityObservation{Constraint: ident(61), Generation: 1, Available: CapacityVector{}, At: time.Unix(1000, 0), KnownResources: CapacityInodes})
	if e = admit(c, tickets, o, time.Unix(1000, 0)); !errors.Is(e, ErrHeadroom) {
		t.Fatal("dynamic model erased actual finite object quota", e)
	}
	c.Filesystems[0].InodeModel = InodesUnknown
	if validateCheckpoint(c) == nil {
		t.Fatal("unknown became documented dynamic capability")
	}
}

func TestCapacityCompleteDomainAndEnvelope(t *testing.T) {
	for _, name := range []string{"missing", "extra", "generation", "binding", "order", "unused", "bytes", "inodes", "metadata", "attribution"} {
		t.Run(name, func(t *testing.T) {
			c := capacityState(t, InodesFinite)
			r := capacityRequest(c, 1, 8)
			x := &r.Components[0]
			switch name {
			case "missing":
				x.CapacityCount = 1
				x.Capacity[1] = CapacityPromise{}
			case "extra":
				x.CapacityCount = 3
				x.Capacity[2] = CapacityPromise{ident(62), 1, CapacityVector{8, 1}}
			case "generation":
				x.Capacity[1].Generation = 2
			case "binding":
				x.CapacityBinding = digest(99)
			case "order":
				x.Capacity[0], x.Capacity[1] = x.Capacity[1], x.Capacity[0]
			case "unused":
				x.Capacity[3] = CapacityPromise{ident(63), 1, CapacityVector{1, 1}}
			case "bytes":
				x.Capacity[1].Peak.Bytes = 7
			case "inodes":
				x.Capacity[1].Peak.Inodes = 0
			case "metadata":
				x.Physical.Bytes = 0
				x.Capacity[0].Peak.Bytes = 0
			case "attribution":
				x.Workspace = ident(10)
				x.WorkspaceGeneration = 1
				x.Attribution = x.Physical
				x.Physical = Vector{}
			}
			ticket := capacityTicket(r, 0)
			if validateTicket(ticket) == nil && registered(c, ticket, true) == nil {
				t.Fatal("incomplete or underpromised domain accepted")
			}
		})
	}
}

func TestCapacityObservationCoverageAndMask(t *testing.T) {
	for _, name := range []string{"missing-old-pool", "missing-quota", "duplicate", "unregistered", "generation", "unknown", "mask", "wrong-resource", "stale", "future", "zero-time", "bounds", "zero-inodes"} {
		t.Run(name, func(t *testing.T) {
			c := capacityState(t, InodesFinite)
			var tickets [Slots]Ticket
			tickets[0] = capacityTicket(capacityRequest(c, 1, 1), 0)
			tickets[1] = capacityTicket(capacityRequest(c, 2, 1), 1)
			o := capacityObservations()
			switch name {
			case "missing-old-pool":
				o = o[1:]
			case "missing-quota":
				o = o[:2]
			case "duplicate":
				o = append(o, o[0])
			case "unregistered":
				o[0].Constraint = ident(99)
			case "generation":
				o[0].Generation++
			case "unknown":
				o[0].KnownResources = 0
			case "mask":
				o[0].KnownResources = 4
			case "wrong-resource":
				o[0].Available.Inodes = 1
			case "stale":
				o[0].At = time.Unix(900, 0)
			case "future":
				o[0].At = time.Unix(1001, 0)
			case "zero-time":
				o[0].At = time.Time{}
			case "bounds":
				o[0].Available.Bytes = maxBytes + 1
			case "zero-inodes":
				o[1].Available.Inodes = 0
			}
			if admit(c, tickets, o, time.Unix(1000, 0)) == nil {
				t.Fatal("invalid/missing outstanding restriction sample granted admission")
			}
		})
	}
}

func TestCapacityConstraintAndCapabilityValidation(t *testing.T) {
	for _, name := range []string{"duplicate-id", "duplicate-identity", "zero-identity", "resource-zero", "resource-extra", "irrelevant-headroom", "headroom-overflow", "unknown-inodes", "missing-evidence", "missing-byte", "missing-inodes", "dynamic-primary", "root-primary-missing", "root-binding"} {
		t.Run(name, func(t *testing.T) {
			c := capacityState(t, InodesFinite)
			switch name {
			case "duplicate-id":
				c.CapacityConstraints[1].ID = c.CapacityConstraints[0].ID
			case "duplicate-identity":
				c.CapacityConstraints[1].Identity = c.CapacityConstraints[0].Identity
			case "zero-identity":
				c.CapacityConstraints[0].Identity = Digest{}
			case "resource-zero":
				c.CapacityConstraints[0].Resources = 0
			case "resource-extra":
				c.CapacityConstraints[0].Resources = 4
			case "irrelevant-headroom":
				c.CapacityConstraints[0].Headroom.Inodes = 1
			case "headroom-overflow":
				c.CapacityConstraints[0].Headroom.Bytes = maxBytes + 1
			case "unknown-inodes":
				c.Filesystems[0].InodeModel = InodesUnknown
			case "missing-evidence":
				c.Filesystems[0].CapabilityEvidence = Digest{}
			case "missing-byte":
				c.Filesystems[0].ByteConstraint = ID{}
			case "missing-inodes":
				c.Filesystems[0].InodeConstraint = ID{}
			case "dynamic-primary":
				c.Filesystems[0].InodeModel = InodesDynamic
			case "root-primary-missing":
				c.Roots[1].CapacityRefs[0] = CapacityRef{ident(62), 1}
			case "root-binding":
				c.Roots[1].CapacityBinding = digest(99)
			}
			if validateCheckpoint(c) == nil {
				t.Fatal("invalid capacity registry accepted")
			}
		})
	}
}

func TestCapacityBindingMetadataAndPrimaryCoverage(t *testing.T) {
	c := capacityState(t, InodesFinite)
	root, f := c.Roots[1], c.Filesystems[0]
	rows := []CapacityConstraint{c.CapacityConstraints[0], c.CapacityConstraints[1], c.CapacityConstraints[2]}
	base, e := CapacityBindingFor(root, f, rows)
	must(t, e)
	root.Known = false
	root.Usage[History] = Vector{99, 1, 0}
	rows[0].Headroom.Bytes = 9
	same, e := CapacityBindingFor(root, f, rows)
	must(t, e)
	if same != base {
		t.Fatal("mutable usage/known/headroom changed immutable relation")
	}
	root.CapacityCount = 1
	root.CapacityRefs[1] = CapacityRef{}
	if _, e = CapacityBindingFor(root, f, rows); !errors.Is(e, ErrAuthority) {
		t.Fatal("missing finite primary inode ref accepted", e)
	}
	root = c.Roots[1]
	root.CapacityRefs[0] = CapacityRef{ident(61), 1}
	root.CapacityRefs[1] = CapacityRef{ident(62), 1}
	if _, e = CapacityBindingFor(root, f, rows); !errors.Is(e, ErrAuthority) {
		t.Fatal("missing primary byte ref accepted", e)
	}
	for _, field := range []string{"root-id", "owner", "identity", "generation", "filesystem-identity", "filesystem-generation", "evidence", "inode-model", "constraint-identity", "constraint-resource"} {
		r, g := c.Roots[1], c.Filesystems[0]
		v := append([]CapacityConstraint(nil), rows...)
		switch field {
		case "root-id":
			r.ID = ident(99)
		case "owner":
			r.Owner = digest(99)
		case "identity":
			r.Identity = digest(99)
		case "generation":
			r.Generation++
		case "filesystem-identity":
			g.Identity = digest(99)
		case "filesystem-generation":
			g.Generation++
		case "evidence":
			g.CapabilityEvidence = digest(99)
		case "inode-model":
			g.InodeModel = InodesDynamic
			g.InodeConstraint = ID{}
		case "constraint-identity":
			v[1].Identity = digest(99)
		case "constraint-resource":
			v[1].Resources = CapacityInodes
		}
		next, err := CapacityBindingFor(r, g, v)
		must(t, err)
		if next == base {
			t.Fatal("immutable metadata omitted from binding", field)
		}
	}
}

func TestCapacityRegistrationBoundsAtomic(t *testing.T) {
	q, path := fixture(t)
	s, e := q.ConfigurationSnapshot(t.Context())
	must(t, e)
	_, e = q.BeginMaintenance(t.Context(), s.Revision)
	must(t, e)
	s, e = q.ConfigurationSnapshot(t.Context())
	must(t, e)
	r := Registration{}
	for i := 0; i < 510; i++ {
		r.CapacityConstraints = append(r.CapacityConstraints, CapacityConstraint{ID: indexedID(1000 + i), Identity: indexedDigest(1000 + i), Generation: 1, Resources: CapacityBytes})
	}
	must(t, q.Register(t.Context(), s.Revision, r))
	full, e := q.ConfigurationSnapshot(t.Context())
	must(t, e)
	if len(full.CapacityConstraints) != 512 || full.CapacityConstraints[511] != r.CapacityConstraints[509] {
		t.Fatal("capacity registry truncated")
	}
	before := configurationTestHashes(t, path)
	extra := CapacityConstraint{ID: indexedID(2000), Identity: indexedDigest(2000), Generation: 1, Resources: CapacityBytes}
	if e = q.Register(t.Context(), full.Revision, Registration{CapacityConstraints: []CapacityConstraint{extra}}); !errors.Is(e, ErrBounds) {
		t.Fatal("513th capacity row accepted", e)
	}
	if e = q.Register(t.Context(), full.Revision, Registration{CapacityConstraints: make([]CapacityConstraint, 513)}); !errors.Is(e, ErrBounds) {
		t.Fatal("oversized registry input accepted", e)
	}
	root := fixtureRegistry().Roots[1]
	root.CapacityCount = 5
	if _, e = CapacityBindingFor(root, fixtureRegistry().Filesystems[0], fixtureRegistry().CapacityConstraints); !errors.Is(e, ErrBounds) {
		t.Fatal("fifth complete ref did not refuse", e)
	}
	after, e := q.ConfigurationSnapshot(t.Context())
	must(t, e)
	if !reflect.DeepEqual(full, after) || !reflect.DeepEqual(before, configurationTestHashes(t, path)) {
		t.Fatal("bounded refusal mutated original registration")
	}
}

func TestCapacityV2LayoutAndPadding(t *testing.T) {
	c := capacityState(t, InodesFinite)
	a := HeldAnchor{Installation: ident(30), Binding: digest(30)}
	b, e := encodeCheckpoint(c, a)
	must(t, e)
	if string(b[:8]) != "JPSTB002" || capacityOffset != 3743744 || capacityOffset+512*128 != 3809280 || len(b)-3809280 != 385024 {
		t.Fatal("v2 fixed checkpoint arithmetic changed")
	}
	if b[rootOffset+595] != 2 || b[filesystemOffset+56] != 1 || b[capacityOffset+56] != 1 || !allZero(b[rootOffset+724:rootOffset+768]) || !allZero(b[filesystemOffset+121:filesystemOffset+256]) || !allZero(b[capacityOffset+73:capacityOffset+128]) {
		t.Fatal("v2 payload ends/fields wrong")
	}
	for _, offset := range []int{rootOffset + 724, filesystemOffset + 121, capacityOffset + 73, 3809280} {
		x := append([]byte(nil), b...)
		x[offset] = 1
		seal(x, 1, 0, c.Generation, a)
		if _, e = decodeCheckpoint(x, a); e == nil {
			t.Fatal("nonzero v2 padding accepted", offset)
		}
	}
	ticket := capacityTicket(capacityRequest(c, 1, 8), 0)
	b, e = encodeTicket(ticket, a)
	must(t, e)
	if len(b) != 16384 || b[1024+297] != 2 || !allZero(b[1024+490:1024+512]) || !allZero(b[9216:]) {
		t.Fatal("ticket v2 fixed component arithmetic changed")
	}
	for _, offset := range []int{1024 + 490, 9216} {
		x := append([]byte(nil), b...)
		x[offset] = 1
		seal(x, 2, 0, 1, a)
		if _, e = decodeTicket(x, 0, a); e == nil {
			t.Fatal("ticket padding accepted")
		}
	}
	x := append([]byte(nil), b...)
	x[1024+297] = 5
	seal(x, 2, 0, 1, a)
	if _, e = decodeTicket(x, 0, a); e == nil {
		t.Fatal("fifth capacity ref accepted")
	}
	decoded, e := decodeTicket(b, 0, a)
	must(t, e)
	if !sameComponents(decoded.Components, ticket.Components) {
		t.Fatal("capacity fields lost in ticket")
	}
	for _, n := range []int{237, 238} {
		entries := make([]Entry, n)
		for i := range entries {
			entries[i] = Entry{Kind: ObjectEntry, Target: string(bytes.Repeat([]byte{'a'}, 1024)), Stage: string(bytes.Repeat([]byte{'b'}, 1024))}
		}
		_, e = encodeManifest(ticket, entries, a)
		if (e == nil) != (n == 237) {
			t.Fatal("manifest entry bound changed", n, e)
		}
	}
}

func TestCapacityImmutableTransitionAndRepairCoverage(t *testing.T) {
	c := capacityState(t, InodesFinite)
	r := capacityRequest(c, 1, 8)
	old := capacityTicket(r, 0)
	for _, field := range []string{"count", "constraint", "generation", "peak", "binding"} {
		next := old
		next.Components = append([]Component(nil), old.Components...)
		next.Token.Generation = 2
		next.State = Staging
		x := &next.Components[0]
		switch field {
		case "count":
			x.CapacityCount = 1
		case "constraint":
			x.Capacity[1].Constraint = ident(62)
		case "generation":
			x.Capacity[1].Generation++
		case "peak":
			x.Capacity[1].Peak.Bytes++
		case "binding":
			x.CapacityBinding = digest(99)
		}
		if legalTransition(old, next, applied{NextSequence: 1}) == nil {
			t.Fatal("transition changed immutable capacity", field)
		}
	}
	next := r
	next.Components = append([]Component(nil), r.Components...)
	next.Components[0].Capacity[0].Peak.Bytes = 100
	next.Components[0].Capacity[1].Peak.Bytes = 7
	if envelopeCovers(next, old) {
		t.Fatal("larger container covered smaller quota promise")
	}
	next.Components[0].Capacity[1].Peak.Bytes = 8
	if !envelopeCovers(next, old) {
		t.Fatal("independent equal/larger peaks were not conserved")
	}
}

func TestCapacitySnapshotAndTerminalConservation(t *testing.T) {
	q, path := fixture(t)
	token, e := q.Reserve(t.Context(), request(6), observations())
	must(t, e)
	s, e := q.Snapshot(t.Context())
	must(t, e)
	if s.Promised[History].Bytes != 6 || len(s.CapacityPromised) != 1 || s.CapacityPromised[0] != (CapacityPromise{ident(60), 1, CapacityVector{6, 1}}) {
		t.Fatal("logical/capacity snapshot mixed")
	}
	s.CapacityPromised[0].Peak.Bytes = 999
	s.Tickets[0].Components[0].Capacity[0].Peak.Bytes = 999
	config, e := q.ConfigurationSnapshot(t.Context())
	must(t, e)
	config.CapacityConstraints[0].Identity = digest(99)
	other, r, w := openFixture(t, path)
	defer other.Close()
	defer r.Close()
	defer w.Close()
	fresh, e := other.Snapshot(t.Context())
	must(t, e)
	if fresh.CapacityPromised[0].Peak.Bytes != 6 {
		t.Fatal("snapshot data aliased original")
	}
	config, e = other.ConfigurationSnapshot(t.Context())
	must(t, e)
	if config.CapacityConstraints[0] != fixtureRegistry().CapacityConstraints[0] {
		t.Fatal("configuration aliases registry")
	}
	token, e = other.Advance(t.Context(), token, owner, Uncertain, digest(84))
	must(t, e)
	fresh, e = other.Snapshot(t.Context())
	must(t, e)
	if fresh.CapacityPromised[0].Peak.Bytes != 6 {
		t.Fatal("uncertain state released peak")
	}
	_, e = other.Settle(t.Context(), SettlementFact{Token: token, Owner: owner, Evidence: digest(85), Changes: []Change{{PhysicalAdd: Vector{6, 1, 0}, AttributionAdd: Vector{6, 1, 0}}}})
	must(t, e)
	fresh, e = other.Snapshot(t.Context())
	must(t, e)
	if len(fresh.CapacityPromised) != 0 || fresh.Installation[History].Bytes != 6 {
		t.Fatal("terminal did not release whole peak/apply usage once")
	}
	must(t, other.Checkpoint(t.Context()))
	fresh, e = q.Snapshot(t.Context())
	must(t, e)
	if fresh.Installation[History].Bytes != 6 || len(fresh.CapacityPromised) != 0 {
		t.Fatal("checkpoint duplicated logical/capacity accounting")
	}
}

func TestCapacityRepairPolicyAndFormatEvidence(t *testing.T) {
	q, path := fixture(t)
	_, e := q.Reserve(t.Context(), request(6), observations())
	must(t, e)
	v, e := q.InspectRecovery(t.Context())
	must(t, e)
	f := RepairFact{Fingerprint: v.Fingerprint, Evidence: digest(90), FormatEvidence: RepairFormatEvidence(q.anchor, v.Fingerprint), Policy: fixturePolicy(), Registry: fixtureRegistry(), Census: fixtureCensus()}
	for i := range f.Slots {
		f.Slots[i].Disposition = digest(91)
	}
	f.Slots[0] = RecoverySlot{Request: request(6), Entries: request(6).Entries}
	before := configurationTestHashes(t, path)
	for _, field := range []string{"missing-format", "foreign-format", "identity", "headroom", "generation", "resources"} {
		x := f
		x.Registry = fixtureRegistry()
		switch field {
		case "missing-format":
			x.FormatEvidence = Digest{}
		case "foreign-format":
			x.FormatEvidence = RepairFormatEvidence(HeldAnchor{Installation: ident(31), Binding: digest(30)}, v.Fingerprint)
		case "identity":
			x.Registry.CapacityConstraints[0].Identity = digest(99)
		case "headroom":
			x.Registry.CapacityConstraints[0].Headroom.Bytes = 0
		case "generation":
			x.Registry.CapacityConstraints[0].Generation++
		case "resources":
			x.Registry.CapacityConstraints[0].Resources = CapacityBytes
		}
		if _, e = q.Repair(t.Context(), x); !errors.Is(e, ErrAuthority) {
			t.Fatal("repair changed original format/capacity relation", field, e)
		}
	}
	if !reflect.DeepEqual(before, configurationTestHashes(t, path)) {
		t.Fatal("rejected repair wrote state")
	}
	_, e = q.Repair(t.Context(), f)
	must(t, e)
	s, e := q.Snapshot(t.Context())
	must(t, e)
	if len(s.CapacityPromised) != 1 || s.CapacityPromised[0].Peak != (CapacityVector{6, 1}) {
		t.Fatal("repair lost original capacity peak")
	}
}

func TestCapacityRecognizedLegacyPreservedBeforeWrite(t *testing.T) {
	for _, row := range []struct {
		name   string
		offset int64
	}{
		{"checkpoint.a", 0}, {"checkpoint.b", 0}, {"tickets.a", 63 * TicketBytes}, {"tickets.b", 63 * TicketBytes}, {"manifests", 63 * ManifestBytes}, {"manifests", 63*ManifestBytes + 128},
	} {
		t.Run(row.name+"-"+time.Duration(row.offset).String(), func(t *testing.T) {
			q, path := fixture(t)
			f, e := os.OpenFile(filepath.Join(path, row.name), os.O_WRONLY, 0)
			must(t, e)
			_, e = f.WriteAt([]byte("JPSTB001"), row.offset)
			must(t, e)
			must(t, f.Close())
			before := configurationTestHashes(t, path)
			_, e = q.Reserve(t.Context(), request(1), observations())
			if !errors.Is(e, ErrUnsupported) {
				t.Fatal("legacy reserve did not refuse", e)
			}
			_, e = q.BeginMaintenance(t.Context(), 3)
			if !errors.Is(e, ErrUnsupported) {
				t.Fatal("legacy fence did not refuse", e)
			}
			_, e = q.InspectRecovery(t.Context())
			if !errors.Is(e, ErrUnsupported) {
				t.Fatal("legacy inspection did not refuse", e)
			}
			_, e = q.Repair(t.Context(), RepairFact{Fingerprint: digest(1), FormatEvidence: RepairFormatEvidence(q.anchor, digest(1)), Evidence: digest(2), Policy: fixturePolicy(), Registry: fixtureRegistry(), Census: fixtureCensus()})
			if !errors.Is(e, ErrUnsupported) {
				t.Fatal("raw repair reinterpreted prior format", e)
			}
			if e = q.InitializeHeld(t.Context(), fixturePolicy()); e == nil {
				t.Fatal("prior format initialized over existing state")
			}
			if !reflect.DeepEqual(before, configurationTestHashes(t, path)) {
				t.Fatal("prior/mixed format bytes changed")
			}
		})
	}
}

// Build synthetic multi-volume state through the actual durable public API.
func capacityRegistry(c *checkpoint) Registration {
	var r Registration
	for _, v := range c.Roots {
		if !v.ID.zero() {
			v.Known = false
			v.Usage = [Categories]Vector{}
			r.Roots = append(r.Roots, v)
		}
	}
	for _, v := range c.Workspaces {
		if !v.ID.zero() {
			v.Known = false
			v.Usage = [Categories]Vector{}
			r.Workspaces = append(r.Workspaces, v)
		}
	}
	for _, v := range c.Filesystems {
		if !v.ID.zero() {
			r.Filesystems = append(r.Filesystems, v)
		}
	}
	for _, v := range c.CapacityConstraints {
		if !v.ID.zero() {
			r.CapacityConstraints = append(r.CapacityConstraints, v)
		}
	}
	return r
}
func capacityDurableFixture(t *testing.T, c *checkpoint) (*Coordinator, string) {
	t.Helper()
	path := t.TempDir()
	must(t, os.Chmod(path, 0700))
	root, e := os.OpenRoot(path)
	must(t, e)
	witness, e := root.OpenFile("lock", os.O_CREATE|os.O_EXCL|os.O_RDWR|noFollow, 0600)
	must(t, e)
	q, e := OpenHeld(HeldAnchor{root, witness, ident(30), digest(30), false})
	must(t, e)
	t.Cleanup(func() { q.Close(); witness.Close(); root.Close() })
	must(t, q.InitializeHeld(t.Context(), c.Policy))
	must(t, q.Register(t.Context(), 1, capacityRegistry(c)))
	must(t, q.PublishCensus(t.Context(), 1, fixtureCensus()))
	return q, path
}
func capacityNowObservations(shared uint64) []CapacityObservation {
	at := time.Now()
	return []CapacityObservation{
		{Constraint: ident(60), Generation: 1, Available: CapacityVector{Bytes: shared}, At: at, KnownResources: CapacityBytes},
		{Constraint: ident(61), Generation: 1, Available: CapacityVector{100, 100}, At: at, KnownResources: CapacityBytes | CapacityInodes},
		{Constraint: ident(62), Generation: 1, Available: CapacityVector{100, 100}, At: at, KnownResources: CapacityBytes | CapacityInodes},
	}
}
func capacityRepairFact(t *testing.T, q *Coordinator, c *checkpoint) RepairFact {
	t.Helper()
	v, e := q.InspectRecovery(t.Context())
	must(t, e)
	f := RepairFact{Fingerprint: v.Fingerprint, FormatEvidence: RepairFormatEvidence(q.anchor, v.Fingerprint), Evidence: digest(90), Policy: c.Policy, Registry: capacityRegistry(c), Census: fixtureCensus()}
	for i := range f.Slots {
		f.Slots[i].Disposition = digest(91)
	}
	return f
}
func TestCapacityRepairAggregateFiniteBeforeWrite(t *testing.T) {
	for _, dimension := range []string{"bytes-over", "inodes-over", "exact-maxima"} {
		t.Run(dimension, func(t *testing.T) {
			c := capacityState(t, InodesFinite)
			c.CapacityConstraints[0].Resources = CapacityBytes | CapacityInodes
			registry := capacityRegistry(c)
			for i := 0; i < 3; i++ {
				r := &c.Roots[i]
				f := c.Filesystems[filesystemIndex(c, r.Filesystem)]
				var e error
				r.CapacityBinding, e = CapacityBindingFor(*r, f, registry.CapacityConstraints)
				must(t, e)
			}
			q, path := capacityDurableFixture(t, c)
			originals := []Request{capacityRequest(c, 1, 1), capacityRequest(c, 2, 1)}
			var originalTokens [2]Token
			obs := capacityNowObservations(100)
			obs[0].KnownResources = CapacityBytes | CapacityInodes
			obs[0].Available.Inodes = 100
			for i, r := range originals {
				var e error
				originalTokens[i], e = q.Reserve(t.Context(), r, obs)
				must(t, e)
			}
			a, b := capacityRequest(c, 1, 1), capacityRequest(c, 2, 1)
			// Independent peaks are larger than logical usage and valid individually.
			a.Components[0].Capacity[0].Peak = CapacityVector{maxBytes - 1, maxCount - 1}
			b.Components[0].Capacity[0].Peak = CapacityVector{1, 1}
			if dimension == "bytes-over" {
				a.Components[0].Capacity[0].Peak.Bytes = maxBytes
			}
			if dimension == "inodes-over" {
				a.Components[0].Capacity[0].Peak.Inodes = maxCount
			}
			f := capacityRepairFact(t, q, c)
			f.Slots[0] = RecoverySlot{Request: a, Reconstruction: digest(92)}
			f.Slots[1] = RecoverySlot{Request: b, Reconstruction: digest(93)}
			before := configurationTestHashes(t, path)
			_, e := q.Repair(t.Context(), f)
			if dimension != "exact-maxima" {
				if !errors.Is(e, ErrBounds) {
					t.Fatalf("aggregate overflow accepted: %v", e)
				}
				if !reflect.DeepEqual(before, configurationTestHashes(t, path)) {
					t.Fatal("overflow refusal wrote original bytes")
				}
				for i, token := range originalTokens {
					r, e := q.Manifest(t.Context(), token, owner)
					must(t, e)
					if !sameComponents(r.Components, originals[i].Components) {
						t.Fatal("overflow lost original recoverable ticket envelope")
					}
				}
				return
			}
			must(t, e)
			reopened, root, witness := openFixture(t, path)
			defer reopened.Close()
			defer root.Close()
			defer witness.Close()
			s, e := reopened.Snapshot(t.Context())
			must(t, e)
			want := []CapacityPromise{{ident(60), 1, CapacityVector{maxBytes, maxCount}}, {ident(61), 1, CapacityVector{1, 1}}, {ident(62), 1, CapacityVector{1, 1}}}
			if s.Promised[History] != (Vector{2, 2, 0}) || !reflect.DeepEqual(s.CapacityPromised, want) || len(s.Tickets) != 2 {
				t.Fatal("exact finite independent maxima did not survive Repair/reopen")
			}
		})
	}
}
func TestCapacityMultiConstraintCrashRepairAndTerminal(t *testing.T) {
	c := capacityState(t, InodesFinite)
	q, path := capacityDurableFixture(t, c)
	a, b := capacityRequest(c, 1, 8), capacityRequest(c, 2, 80)
	_, e := q.Reserve(t.Context(), a, capacityNowObservations(100))
	must(t, e)
	q.fault = &ioFault{Name: "tickets.b", Phase: "write", Offset: TicketBytes, Short: true}
	if _, e = q.Reserve(t.Context(), b, capacityNowObservations(100)); !errors.Is(e, ErrUnknown) {
		t.Fatal("torn second ticket did not preserve uncertainty", e)
	}
	f := capacityRepairFact(t, q, c)
	f.Slots[0] = RecoverySlot{Request: a}
	f.Slots[1] = RecoverySlot{Request: b}
	tokens, e := q.Repair(t.Context(), f)
	must(t, e)
	reopened, root, witness := openFixture(t, path)
	defer reopened.Close()
	defer root.Close()
	defer witness.Close()
	s, e := reopened.Snapshot(t.Context())
	must(t, e)
	want := []CapacityPromise{{ident(60), 1, CapacityVector{88, 0}}, {ident(61), 1, CapacityVector{8, 1}}, {ident(62), 1, CapacityVector{80, 1}}}
	if s.Promised[History] != (Vector{88, 2, 0}) || !reflect.DeepEqual(s.CapacityPromised, want) || len(s.Tickets) != 2 {
		t.Fatal("actual torn-page repair lost one overlapping restriction")
	}
	for i, wantRequest := range []Request{a, b} {
		ticket := s.Tickets[i]
		if ticket.State != Uncertain || !sameComponents(ticket.Components, wantRequest.Components) {
			t.Fatal("repaired immutable peak/binding changed")
		}
	}
	_, e = reopened.Settle(t.Context(), SettlementFact{Token: tokens[0], Owner: owner, Evidence: digest(94), Changes: []Change{{PhysicalAdd: Vector{8, 1, 0}}}})
	must(t, e)
	remaining, e := reopened.Snapshot(t.Context())
	must(t, e)
	if !reflect.DeepEqual(remaining.CapacityPromised, []CapacityPromise{{ident(60), 1, CapacityVector{80, 0}}, {ident(62), 1, CapacityVector{80, 1}}}) {
		t.Fatal("terminal released a partial constraint or another ticket's peak")
	}
	_, e = reopened.Settle(t.Context(), SettlementFact{Token: tokens[1], Owner: owner, Evidence: digest(95), Changes: []Change{{PhysicalAdd: Vector{80, 1, 0}}}})
	must(t, e)
	must(t, reopened.Checkpoint(t.Context()))
	final, e := q.Snapshot(t.Context())
	must(t, e)
	if len(final.CapacityPromised) != 0 || final.Installation[History] != (Vector{88, 2, 0}) {
		t.Fatal("terminal/checkpoint did not conserve logical-once and whole-ticket release")
	}
}

func TestCapacityProcessAdmissionHelper(t *testing.T) {
	if os.Getenv("JP_CAPACITY_HELPER") != "1" {
		return
	}
	q, root, witness := openFixture(t, os.Getenv("JP_CAPACITY_ROOT"))
	defer q.Close()
	defer root.Close()
	defer witness.Close()
	c := capacityState(t, InodesFinite)
	index := 1
	if os.Getenv("JP_CAPACITY_VOLUME") == "b" {
		index = 2
	}
	r := capacityProcessRequest(c, index, os.Getenv("JP_CAPACITY_AXIS"))
	fmt.Println("ready")
	line, e := bufio.NewReader(os.Stdin).ReadString('\n')
	must(t, e)
	if line != "go\n" {
		t.Fatal("barrier")
	}
	available := uint64(10)
	if os.Getenv("JP_CAPACITY_AXIS") != "" {
		available = 100
	}
	_, e = q.Reserve(t.Context(), r, capacityNowObservations(available))
	switch {
	case e == nil:
		fmt.Println("admitted")
	case errors.Is(e, ErrHeadroom):
		fmt.Println("headroom")
	case errors.Is(e, ErrLimit):
		fmt.Println("limit")
	default:
		t.Fatal("shared original budgets must decide admission", e)
	}
}
func TestCapacityDistinctFilesystemProcessesShareHeadroom(t *testing.T) {
	testCapacityProcessReservations(t, "")
}

func TestCapacityProcessesLogicalTemporarySettlementReservations(t *testing.T) {
	for _, axis := range []string{"logical", "temporary", "settlement"} {
		t.Run(axis, func(t *testing.T) { testCapacityProcessReservations(t, axis) })
	}
}

func capacityProcessRequest(c *checkpoint, index int, axis string) Request {
	r := capacityRequest(c, index, 6)
	if axis == "temporary" {
		r.Components[0].Category = Transient
	}
	if axis == "settlement" {
		r = capacityRequest(c, index, 7)
		r.Components[0].Physical.Bytes = 1
		r.Components[0].SettlementPhysical.Bytes = 6
	}
	return r
}

func testCapacityProcessReservations(t *testing.T, axis string) {
	t.Helper()
	c := capacityState(t, InodesFinite)
	if axis != "" {
		category := History
		if axis == "temporary" {
			category = Transient
		}
		c.Policy.Installation.Category[category].Bytes = 10
	}
	q, path := capacityDurableFixture(t, c)
	exe, e := os.Executable()
	must(t, e)
	var commands []*exec.Cmd
	var inputs []ioWriteCloser
	var readers []*bufio.Reader
	t.Cleanup(func() {
		for _, cmd := range commands {
			if cmd.ProcessState == nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		}
	})
	for _, volume := range []string{"a", "b"} {
		cmd := exec.Command(exe, "-test.run=^TestCapacityProcessAdmissionHelper$", "-test.count=1")
		cmd.Env = append(os.Environ(), "JP_CAPACITY_HELPER=1", "JP_CAPACITY_ROOT="+path, "JP_CAPACITY_VOLUME="+volume, "JP_CAPACITY_AXIS="+axis)
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
	for _, reader := range readers {
		line, e := reader.ReadString('\n')
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
	admitted, refused := 0, 0
	for i, reader := range readers {
		line, e := reader.ReadString('\n')
		must(t, e)
		switch line {
		case "admitted\n":
			admitted++
		case "headroom\n":
			if axis != "" {
				t.Fatal("capacity unexpectedly decided a logical-axis fixture")
			}
			refused++
		case "limit\n":
			if axis == "" {
				t.Fatal("logical limit unexpectedly decided capacity fixture")
			}
			refused++
		default:
			t.Fatal(line)
		}
		must(t, commands[i].Wait())
	}
	if admitted != 1 || refused != 1 {
		t.Fatal("distinct filesystem processes spent the same shared bytes twice")
	}
	s, e := q.Snapshot(t.Context())
	must(t, e)
	category, promised := History, uint64(6)
	if axis == "temporary" {
		category = Transient
	}
	if axis == "settlement" {
		promised = 7
	}
	if len(s.Tickets) != 1 || s.Promised[category] != (Vector{promised, 1, 0}) || len(s.CapacityPromised) != 2 || s.CapacityPromised[0] != (CapacityPromise{ident(60), 1, CapacityVector{promised, 0}}) {
		t.Fatal("process winner not durably charged on unchanged independent axes")
	}
	if axis == "settlement" && s.Tickets[0].Components[0].SettlementPhysical.Bytes != 6 {
		t.Fatal("winner lost its original settlement obligation")
	}
}
