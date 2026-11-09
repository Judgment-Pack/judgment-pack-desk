package storagebudget

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"os"
)

// RecoveryView is bounded metadata, never credential/file contents. Inspect
// first fences healthy admission; a damaged page already prevents admission.
// The owner then takes original domain leases OUTSIDE Q, inspects exact
// targets/stages/receipts and submits a fingerprint-bound Repair. PID/TTL or
// missing receipts are not disposition evidence. No underlying effect replays.
type RecoveryView struct {
	ManifestEntries       [Slots][]Entry
	Fingerprint           Digest
	CheckpointGenerations [2]uint64
	CheckpointValid       [2]bool
	TicketCandidates      [Slots][2]Ticket
	TicketValid           [Slots][2]bool
	ManifestReservations  [Slots]Ticket
	ManifestValid         [Slots]bool
}
type RecoverySlot struct {
	// Request retains a conservative obligation as Uncertain. Empty components
	// require Disposition: an original-owner proof of exact reconciled outcome.
	Request     Request
	Entries     []Entry
	Disposition Digest
	// Required to reconstruct an unreadable obligation when neither an intact
	// manifest nor a readable unretired reservation establishes its envelope.
	Reconstruction Digest
}
type RepairFact struct {
	Fingerprint, Evidence Digest
	FormatEvidence        Digest
	Policy                Policy
	Registry              Registration
	Census                Census
	Slots                 [Slots]RecoverySlot
}

// RepairFormatEvidence binds the original owner's v2-format assertion to this
// exact installation and inspected state. The owner must retain actual original
// v2 record/construction/reconstruction evidence; computing this hash proves
// neither provenance nor custody. Recognized v1 state always refuses Repair.
func RepairFormatEvidence(a HeldAnchor, fingerprint Digest) Digest {
	if a.Installation.zero() || !authority(a.Binding) || !authority(fingerprint) {
		return Digest{}
	}
	h := sha256.New()
	h.Write([]byte("JPSTB002/original-format\x00"))
	h.Write(a.Installation[:])
	h.Write(a.Binding[:])
	h.Write(fingerprint[:])
	var d Digest
	copy(d[:], h.Sum(nil))
	return d
}

type rawRecovery struct {
	view        RecoveryView
	checkpoints [2]*checkpoint
	manifest    [Slots][]byte
}

func (q *Coordinator) InspectRecovery(ctx context.Context) (RecoveryView, error) {
	var out RecoveryView
	e := q.locked(ctx, func() error {
		if l, e := q.load(); e == nil && !l.cp.Maintenance {
			l.cp.Maintenance = true
			l.cp.Epoch, e = next(l.cp.Epoch)
			if e != nil {
				return e
			}
			if e = q.saveCheckpoint(l); e != nil {
				return e
			}
		}
		r, e := q.inspect()
		if e != nil {
			return e
		}
		out = r.view
		return nil
	})
	return out, e
}
func (q *Coordinator) inspect() (*rawRecovery, error) {
	r := new(rawRecovery)
	h := sha256.New()
	for _, name := range []string{"checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests"} {
		// Fixed maximum reads only. A malformed shape remains recoverable evidence;
		// oversized/foreign/symlink files are not modified by this interface.
		before, e := q.anchor.Root.Lstat(name)
		if e != nil || !privateInfo(before, false) || before.Size() < 0 || before.Size() > expectedSize(name) {
			return nil, ErrUnknown
		}
		f, e := q.anchor.Root.OpenFile(name, os.O_RDONLY|noFollow|nonBlocking, 0)
		if e != nil {
			return nil, ErrUnknown
		}
		held, e := f.Stat()
		if e != nil || !privateInfo(held, false) || !os.SameFile(before, held) {
			f.Close()
			return nil, ErrAuthority
		}
		b, e := io.ReadAll(io.LimitReader(f, expectedSize(name)+1))
		ce := f.Close()
		if e != nil || ce != nil || int64(len(b)) != held.Size() {
			return nil, ErrUnknown
		}
		if legacyFormat(name, b) {
			return nil, refuse(ErrUnsupported, "prior storage format preserved")
		}
		h.Write([]byte(name))
		var length [8]byte
		binary.LittleEndian.PutUint64(length[:], uint64(len(b)))
		h.Write(length[:])
		h.Write(b)
		switch name {
		case "checkpoint.a", "checkpoint.b":
			i := 0
			if name == "checkpoint.b" {
				i = 1
			}
			c, e := decodeCheckpoint(b, q.anchor)
			if e == nil {
				r.checkpoints[i] = c
				r.view.CheckpointValid[i] = true
				r.view.CheckpointGenerations[i] = c.Generation
			}
		case "tickets.a", "tickets.b":
			j := 0
			if name == "tickets.b" {
				j = 1
			}
			for i := 0; i < Slots; i++ {
				if len(b) < (i+1)*TicketBytes {
					break
				}
				t, e := decodeTicket(b[i*TicketBytes:(i+1)*TicketBytes], i, q.anchor)
				if e == nil {
					r.view.TicketCandidates[i][j] = t
					r.view.TicketValid[i][j] = true
				}
			}
		case "manifests":
			for i := 0; i < Slots; i++ {
				if len(b) < (i+1)*ManifestBytes {
					break
				}
				page := b[i*ManifestBytes : (i+1)*ManifestBytes]
				r.manifest[i] = page
				initial, e := decodeTicket(page[pageHeader:pageHeader+TicketBytes], i, q.anchor)
				if e != nil {
					continue
				}
				t, entries, e := decodeManifest(page, initial, q.anchor)
				if e == nil {
					r.view.ManifestReservations[i] = t
					r.view.ManifestEntries[i] = entries
					r.view.ManifestValid[i] = true
				}
			}
		}
	}
	// A readable group page alone cannot reconstruct unknown workspace IDs.
	// Hydrate only from its exact committed immutable manifest; otherwise keep
	// the page candidate visible but incomplete for conservative Repair refusal.
	for i := range r.view.TicketCandidates {
		for j, t := range r.view.TicketCandidates[i] {
			if !r.view.TicketValid[i][j] || t.Fanout == nil || len(r.manifest[i]) != ManifestBytes || sha256.Sum256(r.manifest[i]) != t.Manifest {
				continue
			}
			initial, _, err := decodeManifest(r.manifest[i], t, q.anchor)
			if err == nil && sameComponents(initial.Components, t.Components) {
				t.Fanout = cloneFanout(initial.Fanout)
				if validateTicket(t) == nil {
					r.view.TicketCandidates[i][j] = t
				}
			}
		}
	}
	copy(r.view.Fingerprint[:], h.Sum(nil))
	return r, nil
}
func coveredByCheckpoint(r *rawRecovery, t Ticket) bool {
	for _, c := range r.checkpoints {
		if c == nil || c.Maintenance {
			continue
		}
		a := c.Applied[t.Token.Slot]
		if a.Sequence > t.Token.Sequence || a.Sequence == t.Token.Sequence && a.ID == t.Token.ID && a.Generation >= t.Token.Generation {
			return true
		}
	}
	return false
}
func envelopeCovers(request Request, old Ticket) bool {
	if request.Owner != old.Owner || request.Workflow != old.Workflow || request.Operation != old.Operation {
		return false
	}
	if request.Fanout != nil || old.Fanout != nil {
		// Retaining the same variant and immutable membership is intentional:
		// Repair does not translate or infer an interrupted host's fanout plan.
		// An unreadable membership requires explicit original-owner disposition.
		if request.Fanout == nil || old.Fanout == nil || validateFanout(old) != nil || !sameFanout(request.Fanout, old.Fanout) || len(request.Components) != len(old.Components) {
			return false
		}
		for i, a := range old.Components {
			if !componentEnvelopeCovers(a, request.Components[i]) {
				return false
			}
		}
		return true
	}
	for _, a := range old.Components {
		found := false
		ap, aw, e := a.total()
		if e != nil {
			return false
		}
		for _, b := range request.Components {
			if a.Root == b.Root && a.RootGeneration == b.RootGeneration && a.Owner == b.Owner && a.Filesystem == b.Filesystem && a.FilesystemGeneration == b.FilesystemGeneration && a.Workspace == b.Workspace && a.WorkspaceGeneration == b.WorkspaceGeneration && a.Category == b.Category {
				bp, bw, e := b.total()
				if e != nil {
					return false
				}
				if ap.le(bp) && aw.le(bw) {
					found = a.CapacityCount == b.CapacityCount && a.CapacityBinding == b.CapacityBinding
					for j := 0; found && j < int(a.CapacityCount); j++ {
						x, y := a.Capacity[j], b.Capacity[j]
						found = x.Constraint == y.Constraint && x.Generation == y.Generation && x.Peak.le(y.Peak)
					}
				}
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Repair is an original-owner maintenance transaction, not automatic garbage
// collection. A complete, lease-protected physical/attribution census is required
// even when a valid old checkpoint exists: slot reuse may have retired deltas
// unavailable from the older generation. Retained promises are additional to the
// census (conservative double charging of materialized stages is intentional).
// A nonzero Disposition is an internal owner's explicit exact-outcome assertion;
// the hash itself does not prove ownership or authorize a public caller.
func (q *Coordinator) Repair(ctx context.Context, f RepairFact) ([Slots]Token, error) {
	var out [Slots]Token
	if !authority(f.Evidence) || !authority(f.Census.Evidence) || !f.Policy.valid() || len(f.Registry.Roots) > MaxRoots || len(f.Registry.Workspaces) > MaxWorkspaces || len(f.Registry.Filesystems) > MaxFilesystems || len(f.Registry.CapacityConstraints) > MaxCapacityConstraints {
		return out, ErrBounds
	}
	e := q.locked(ctx, func() error {
		raw, e := q.inspect()
		if e != nil {
			return e
		}
		if raw.view.Fingerprint != f.Fingerprint {
			return ErrStale
		}
		if !authority(f.FormatEvidence) || f.FormatEvidence != RepairFormatEvidence(q.anchor, f.Fingerprint) {
			return ErrAuthority
		}
		// Shape repair is intentionally narrower than custody recovery: do not
		// overwrite oversized/foreign files or erase trailing ambiguous data.
		for _, n := range []string{"checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests"} {
			i, e := q.anchor.Root.Lstat(n)
			if e != nil || i.Size() != expectedSize(n) {
				return ErrUnknown
			}
		}
		c := &checkpoint{Policy: f.Policy, Maintenance: true, Epoch: 1, Generation: 1}
		for _, old := range raw.checkpoints {
			if old != nil {
				if old.Generation >= c.Generation {
					c.Generation = old.Generation
				}
				if old.Epoch >= c.Epoch {
					c.Epoch = old.Epoch
				}
			}
		}
		c.Generation, e = next(c.Generation)
		if e != nil {
			return e
		}
		c.Epoch, e = next(c.Epoch)
		if e != nil {
			return e
		}
		for i, v := range f.Registry.Roots {
			c.Roots[i] = v
		}
		for i, v := range f.Registry.Workspaces {
			c.Workspaces[i] = v
		}
		for i, v := range f.Registry.Filesystems {
			c.Filesystems[i] = v
		}
		for i, v := range f.Registry.CapacityConstraints {
			c.CapacityConstraints[i] = v
		}
		// Never silently drop or rebind a readable registered owner. Missing roots
		// must remain represented/charged until original custody resolves them.
		for _, old := range raw.checkpoints {
			if old == nil {
				continue
			}
			for _, v := range old.Roots {
				if v.ID.zero() {
					continue
				}
				i := rootIndex(c, v.ID)
				if i < 0 || c.Roots[i].Owner != v.Owner || c.Roots[i].Identity != v.Identity || c.Roots[i].Generation < v.Generation || c.Roots[i].CapacityCount != v.CapacityCount || c.Roots[i].CapacityRefs != v.CapacityRefs || c.Roots[i].CapacityBinding != v.CapacityBinding {
					return ErrAuthority
				}
			}
			for _, w := range old.Workspaces {
				if !w.ID.zero() {
					i := workspaceIndex(c, w.ID)
					if i < 0 || c.Workspaces[i].Generation < w.Generation {
						return ErrAuthority
					}
				}
			}
			for _, fs := range old.Filesystems {
				if !fs.ID.zero() {
					i := filesystemIndex(c, fs.ID)
					if i < 0 || c.Filesystems[i] != fs {
						return ErrAuthority
					}
				}
			}
			for _, v := range old.CapacityConstraints {
				if v.ID.zero() {
					continue
				}
				i := constraintIndex(c, v.ID)
				if i < 0 || c.CapacityConstraints[i] != v {
					return ErrAuthority
				}
			}
		}
		if e = publishCensus(c, f.Census); e != nil {
			return e
		}
		if !(Vector{MetadataBudget, 16, 1}).le(c.Installation[Control]) {
			return ErrBounds
		}
		var rebuilt [Slots]Ticket
		var manifests [Slots][]byte
		for i, s := range f.Slots {
			if len(s.Request.Components) > MaxComponents || len(s.Entries) > MaxEntries || s.Request.Fanout != nil && (len(s.Entries) != 0 || len(s.Request.Fanout.Workspaces) > MaxWorkspaces || len(s.Request.Fanout.Memberships) != len(s.Request.Components)) {
				return ErrBounds
			}
			generation, sequence := uint64(0), uint64(0)
			var lastID ID
			for _, old := range raw.checkpoints {
				if old == nil {
					continue
				}
				a := old.Applied[i]
				if a.Sequence >= sequence {
					sequence = a.Sequence
					lastID = a.ID
				}
				if a.Generation > generation {
					generation = a.Generation
				}
			}
			var candidates []Ticket
			for j, t := range raw.view.TicketCandidates[i] {
				if !raw.view.TicketValid[i][j] {
					continue
				}
				if t.Token.Generation > generation {
					generation = t.Token.Generation
				}
				if t.Token.Sequence >= sequence {
					sequence = t.Token.Sequence
					lastID = t.Token.ID
				}
				if t.State != Empty && !coveredByCheckpoint(raw, t) {
					candidates = append(candidates, t)
				}
			}
			if raw.view.ManifestValid[i] {
				t := raw.view.ManifestReservations[i]
				if t.Token.Generation > generation {
					generation = t.Token.Generation
				}
				if t.Token.Sequence >= sequence {
					sequence = t.Token.Sequence
					lastID = t.Token.ID
				}
				if !coveredByCheckpoint(raw, t) {
					candidates = append(candidates, t)
				}
			}
			g, e := next(generation)
			if e != nil {
				return e
			}
			n, e := next(sequence)
			if e != nil {
				return e
			}
			c.Applied[i] = applied{lastID, sequence, generation, n}
			if sequence == 0 {
				c.Applied[i] = applied{NextSequence: 1}
			}
			if len(s.Request.Components) == 0 {
				if s.Request.Fanout != nil {
					return ErrBounds
				}
				if !authority(s.Disposition) {
					return ErrAuthority
				}
				rebuilt[i] = Ticket{Token: Token{Slot: uint8(i), Generation: g}}
				continue
			}
			if !authority(s.Disposition) {
				if !raw.view.ManifestValid[i] && !authority(s.Reconstruction) {
					return ErrAuthority
				}
				if raw.view.ManifestValid[i] && !coveredByCheckpoint(raw, raw.view.ManifestReservations[i]) && !entriesCover(s.Request.Components, s.Entries, raw.view.ManifestReservations[i].Components, raw.view.ManifestEntries[i]) {
					return ErrAuthority
				}
				hasEnvelope := false
				for _, old := range candidates {
					if old.State != Terminal {
						hasEnvelope = true
					}
				}
				if !hasEnvelope && !authority(s.Reconstruction) {
					return ErrAuthority
				}
				for _, old := range candidates {
					if !envelopeCovers(s.Request, old) {
						return ErrAuthority
					}
				}
			}
			id, e := NewID()
			if e != nil {
				return e
			}
			t := Ticket{Token: Token{uint8(i), id, n, g}, State: Uncertain, PolicyRevision: c.Generation, Workflow: s.Request.Workflow, Operation: s.Request.Operation, Owner: s.Request.Owner, Components: append([]Component(nil), s.Request.Components...), Fanout: cloneFanout(s.Request.Fanout), Evidence: f.Evidence}
			if e = validateTicket(t); e != nil {
				return e
			}
			if e = registered(c, t, false); e != nil {
				return e
			}
			m, e := encodeManifest(t, s.Entries, q.anchor)
			if e != nil {
				return e
			}
			t.Manifest = sha256.Sum256(m)
			t.ManifestLength = ManifestBytes
			rebuilt[i] = t
			manifests[i] = m
			out[i] = t.Token
		}
		if e = validateCheckpoint(c); e != nil {
			return e
		}
		// CC1: every rebuilt envelope must fit finite aggregate representation
		// before the first baseline write. Recovery may remain over policy/headroom;
		// it may never persist an unreadable aggregate or shrink a prior promise.
		if _, _, _, e = promises(c, rebuilt); e != nil {
			return e
		}
		// The maintenance baseline retires all old ticket/manifest references only
		// after original-owner census/dispositions above. Old pages may now be
		// replaced; any interrupted mix remains maintenance or explicit unknown.
		baseline, e := encodeCheckpoint(c, q.anchor)
		if e != nil {
			return e
		}
		for _, n := range []string{"checkpoint.a", "checkpoint.b"} {
			if e = q.writeAt(n, 0, baseline); e != nil {
				return e
			}
		}
		for i, t := range rebuilt {
			if manifests[i] != nil {
				if e = q.writeAt("manifests", int64(i)*ManifestBytes, manifests[i]); e != nil {
					return e
				}
			}
			b, e := encodeTicket(t, q.anchor)
			if e != nil {
				return e
			}
			for _, n := range []string{"tickets.a", "tickets.b"} {
				if e = q.writeAt(n, int64(i)*TicketBytes, b); e != nil {
					return e
				}
			}
		}
		c.Generation, e = next(c.Generation)
		if e != nil {
			return e
		}
		c.Maintenance = false
		done, e := encodeCheckpoint(c, q.anchor)
		if e != nil {
			return e
		}
		for _, n := range []string{"checkpoint.a", "checkpoint.b"} {
			if e = q.writeAt(n, 0, done); e != nil {
				return e
			}
		}
		return nil
	})
	return out, e
}

func componentKey(a, b Component) bool {
	return a.Root == b.Root && a.RootGeneration == b.RootGeneration && a.Owner == b.Owner && a.Filesystem == b.Filesystem && a.FilesystemGeneration == b.FilesystemGeneration && a.Workspace == b.Workspace && a.WorkspaceGeneration == b.WorkspaceGeneration && a.Category == b.Category
}
func entriesCover(components []Component, entries []Entry, oldComponents []Component, oldEntries []Entry) bool {
	for _, old := range oldEntries {
		if int(old.Component) >= len(oldComponents) {
			return false
		}
		mapped := -1
		for i, c := range components {
			if componentKey(c, oldComponents[old.Component]) {
				mapped = i
				break
			}
		}
		if mapped < 0 {
			return false
		}
		old.Component = uint8(mapped)
		found := false
		for _, e := range entries {
			if old == e {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
