package storagebudget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"sort"
	"time"
)

func (q *Coordinator) Register(ctx context.Context, revision uint64, r Registration) error {
	if len(r.Roots) > MaxRoots || len(r.Workspaces) > MaxWorkspaces || len(r.Filesystems) > MaxFilesystems || len(r.CapacityConstraints) > MaxCapacityConstraints {
		return ErrBounds
	}
	return q.locked(ctx, func() error {
		l, e := q.load()
		if e != nil {
			return e
		}
		c := l.cp
		if c.Generation != revision {
			return ErrStale
		}
		if !c.Maintenance {
			return ErrBusy
		}
		for _, v := range r.Workspaces {
			if v.ID.zero() || v.Generation == 0 || workspaceIndex(c, v.ID) >= 0 {
				return ErrAuthority
			}
			v.Known = false
			v.Usage = [Categories]Vector{}
			if v.Limits == (Limits{}) {
				v.Limits = c.Policy.Workspace
			} else if !v.Limits.valid() {
				return ErrBounds
			}
			placed := false
			for i, x := range c.Workspaces {
				if x.ID.zero() {
					c.Workspaces[i] = v
					placed = true
					break
				}
			}
			if !placed {
				return ErrBounds
			}
		}
		for _, v := range r.CapacityConstraints {
			if !validConstraint(v) || constraintIndex(c, v.ID) >= 0 {
				return ErrAuthority
			}
			placed := false
			for i, x := range c.CapacityConstraints {
				if x.ID.zero() {
					c.CapacityConstraints[i] = v
					placed = true
					break
				}
			}
			if !placed {
				return ErrBounds
			}
		}
		for _, v := range r.Filesystems {
			if v.ID.zero() || filesystemIndex(c, v.ID) >= 0 {
				return ErrAuthority
			}
			placed := false
			for i, x := range c.Filesystems {
				if x.ID.zero() {
					c.Filesystems[i] = v
					placed = true
					break
				}
			}
			if !placed {
				return ErrBounds
			}
		}
		for _, v := range r.Roots {
			if v.CapacityCount > MaxCapacityRefs {
				return ErrBounds
			}
			if v.ID.zero() || rootIndex(c, v.ID) >= 0 {
				return ErrAuthority
			}
			v.Known = false
			v.Usage = [Categories]Vector{}
			placed := false
			for i, x := range c.Roots {
				if x.ID.zero() {
					c.Roots[i] = v
					placed = true
					break
				}
			}
			if !placed {
				return ErrBounds
			}
		}
		if e = validateCheckpoint(c); e != nil {
			return e
		}
		return q.saveCheckpoint(l)
	})
}

// BeginMaintenance fences all fresh admission before the owner takes any domain
// lock/census outside Q. Existing exact settlement remains possible.
func (q *Coordinator) BeginMaintenance(ctx context.Context, revision uint64) (uint64, error) {
	var epoch uint64
	e := q.locked(ctx, func() error {
		l, e := q.load()
		if e != nil {
			return e
		}
		if l.cp.Generation != revision {
			return ErrStale
		}
		epoch, e = next(l.cp.Epoch)
		if e != nil {
			return e
		}
		l.cp.Epoch = epoch
		l.cp.Maintenance = true
		return q.saveCheckpoint(l)
	})
	return epoch, e
}

// PublishCensus is the simple no-active-operation bootstrap/reconciliation path.
// Live/uncertain tickets require Repair's complete original-owner reconciliation;
// an active writer must not race an unfenced whole-root measurement.
func (q *Coordinator) PublishCensus(ctx context.Context, epoch uint64, f Census) error {
	return q.locked(ctx, func() error {
		l, e := q.load()
		if e != nil {
			return e
		}
		if !l.cp.Maintenance || l.cp.Epoch != epoch {
			return ErrStale
		}
		for i, t := range l.tickets {
			if t.State != Empty && !retired(t, l.diskApplied[i]) {
				return ErrBusy
			}
		}
		if e = publishCensus(l.cp, f); e != nil {
			return e
		}
		if !(Vector{MetadataBudget, 16, 1}).le(l.cp.Installation[Control]) {
			return ErrBounds
		}
		l.cp.Maintenance = false
		return q.saveCheckpoint(l)
	})
}
func (q *Coordinator) UpdatePolicy(ctx context.Context, revision uint64, p Policy) error {
	if !p.valid() {
		return ErrBounds
	}
	return q.locked(ctx, func() error {
		l, e := q.load()
		if e != nil {
			return e
		}
		if l.cp.Generation != revision {
			return ErrStale
		}
		old := l.cp.Policy.Workspace
		for i, w := range l.cp.Workspaces {
			if !w.ID.zero() && w.Limits == old {
				l.cp.Workspaces[i].Limits = p.Workspace
			}
		}
		l.cp.Policy = p
		return q.saveCheckpoint(l)
	})
}

func (q *Coordinator) Reserve(ctx context.Context, r Request, observations []CapacityObservation) (Token, error) {
	return q.reserve(ctx, r, observations, nil)
}

// ReserveCompared adds only the exact pre-write comparison. It uses ordinary
// Reserve loading/replay semantics, including unapplied terminals. A mismatch
// is precisely ErrBusy: no manifest/ticket write has occurred. Joined IO/Close
// errors retain their original uncertain meaning in the caller.
func (q *Coordinator) ReserveCompared(ctx context.Context, r Request, observations []CapacityObservation, expected ReserveComparison) (Token, error) {
	return q.reserve(ctx, r, observations, &expected)
}

func (q *Coordinator) reserve(ctx context.Context, r Request, observations []CapacityObservation, expected *ReserveComparison) (Token, error) {
	var token Token
	if r.Fanout != nil && (len(r.Entries) != 0 || len(r.Fanout.Workspaces) > MaxWorkspaces || len(r.Fanout.Memberships) != len(r.Components)) {
		return token, ErrBounds
	}
	if !textValid(r.Workflow, 128, false) || !textValid(r.Operation, 128, false) || !authority(r.Owner) || len(r.Components) == 0 || len(r.Components) > MaxComponents || len(r.Entries) > MaxEntries {
		return token, ErrBounds
	}
	e := q.locked(ctx, func() error {
		var raw EnrollmentInspection
		var l *loaded
		var e error
		if expected == nil {
			l, e = q.load()
		} else {
			l, e = q.loadWithEnrollment(false, &raw)
		}
		if e != nil {
			return e
		}
		if expected != nil {
			request, err := reserveRequestDigest(r)
			if err != nil {
				return err
			}
			// A shared writer taking the last free slot is still precisely a stale
			// comparison, not the ordinary no-slot ErrBounds refusal.
			if expected.Request != request || expected.Original != q.policyCommitment(l, raw) {
				return ErrBusy
			}
		}
		slot := -1
		for i, t := range l.tickets {
			if t.State == Empty || retired(t, l.diskApplied[i]) {
				slot = i
				break
			}
		}
		if slot < 0 {
			return ErrBounds
		}
		if expected != nil {
			if expected.Slot != uint8(slot) {
				return ErrBusy
			}
		}
		id, e := NewID()
		if e != nil {
			return e
		}
		g, e := next(l.tickets[slot].Token.Generation)
		if e != nil {
			return e
		}
		t := Ticket{Token: Token{uint8(slot), id, l.diskApplied[slot].NextSequence, g}, State: Reserved, PolicyRevision: l.cp.Generation, Workflow: r.Workflow, Operation: r.Operation, Owner: r.Owner, Components: append([]Component(nil), r.Components...), Fanout: cloneFanout(r.Fanout)}
		if e = validateTicket(t); e != nil {
			return e
		}
		if e = registered(l.cp, t, true); e != nil {
			return e
		}
		// The test candidate includes all components under the same Q lock; no root
		// can consume capacity when another component/filesystem would refuse.
		candidate := l.tickets
		candidate[slot] = t
		if e = admit(l.cp, candidate, observations, time.Now()); e != nil {
			return e
		}
		manifest, e := encodeManifest(t, r.Entries, q.anchor)
		if e != nil {
			return e
		}
		t.Manifest = sha256.Sum256(manifest)
		t.ManifestLength = ManifestBytes
		if e = q.writeAt("manifests", int64(slot)*ManifestBytes, manifest); e != nil {
			return e
		}
		if e = q.saveTicket(l, t); e != nil {
			return e
		}
		token = t.Token
		return nil
	})
	return token, e
}

func reserveRequestDigest(r Request) (Digest, error) {
	if !textValid(r.Workflow, 128, false) || !textValid(r.Operation, 128, false) || !authority(r.Owner) || len(r.Components) == 0 || len(r.Components) > MaxComponents || len(r.Entries) > MaxEntries || r.Fanout != nil && (len(r.Entries) != 0 || len(r.Fanout.Workspaces) > MaxWorkspaces || len(r.Fanout.Memberships) != len(r.Components)) {
		return Digest{}, ErrBounds
	}
	// Apply the existing manifest name/shape and aggregate frame bounds before
	// JSON allocates any variable-length input. No new accepted request shape.
	manifestBytes := pageHeader + TicketBytes + 2
	for _, entry := range r.Entries {
		if entry.Kind != ObjectEntry || int(entry.Component) >= len(r.Components) || !relative(entry.Target) || !relative(entry.Stage) || !(Vector{entry.Bytes, entry.Files, entry.Directories}).valid() {
			return Digest{}, ErrBounds
		}
		manifestBytes += 94 + len(entry.Target) + len(entry.Stage)
		if manifestBytes > ManifestBytes {
			return Digest{}, ErrBounds
		}
	}
	if err := validateFanout(Ticket{State: Reserved, Components: r.Components, Fanout: r.Fanout}); err != nil {
		return Digest{}, err
	}
	// Request is a closed finite DTO. JSON includes every Entry, fanout member,
	// vector and identity; it is a comparison commitment, never a manifest or
	// a permission. The durable manifest still uses its unchanged strict codec.
	b, err := json.Marshal(r)
	if err != nil {
		return Digest{}, err
	}
	return sha256.Sum256(b), nil
}
func current(l *loaded, token Token, owner Digest) (Ticket, error) {
	if int(token.Slot) >= Slots {
		return Ticket{}, ErrBounds
	}
	t := l.tickets[token.Slot]
	if t.Token != token {
		return Ticket{}, ErrStale
	}
	if t.State == Empty || t.Owner != owner || !authority(owner) {
		return Ticket{}, ErrAuthority
	}
	return t, nil
}

// Advance binds the owner's durable stage/dispatch intent. The owner must also
// persist this token in its own workflow before dispatch; B1 never dispatches.
func (q *Coordinator) Advance(ctx context.Context, token Token, owner Digest, state State, evidence Digest) (Token, error) {
	var out Token
	if !authority(evidence) {
		return out, ErrAuthority
	}
	e := q.locked(ctx, func() error {
		l, e := q.load()
		if e != nil {
			return e
		}
		t, e := current(l, token, owner)
		if e != nil {
			return e
		}
		if state != Uncertain && !(t.State == Reserved && state == Staging) && !(t.State == Staging && state == Dispatched) || t.State == Terminal {
			return ErrBounds
		}
		t.Token.Generation, e = next(t.Token.Generation)
		if e != nil {
			return e
		}
		t.State = state
		t.Evidence = evidence
		if e = q.saveTicket(l, t); e != nil {
			return e
		}
		out = t.Token
		return nil
	})
	return out, e
}

// Settle is idempotent for the exact same fact/token: a caller may repeat only
// this persistence request, never the underlying operation. New facts require
// the current token. Unproven residual data must stay charged or nonterminal.
func (q *Coordinator) Settle(ctx context.Context, f SettlementFact) (Token, error) {
	var out Token
	if !authority(f.Evidence) {
		return out, ErrAuthority
	}
	e := q.locked(ctx, func() error {
		l, e := q.load()
		if e != nil {
			return e
		}
		if int(f.Token.Slot) >= Slots {
			return ErrBounds
		}
		t := l.tickets[f.Token.Slot]
		if t.State == Terminal && t.Token.ID == f.Token.ID && t.Token.Sequence == f.Token.Sequence && t.Owner == f.Owner && t.Evidence == f.Evidence && changesEqual(t.Changes, f.Changes) && (t.Token.Generation == f.Token.Generation || t.Token.Generation == f.Token.Generation+1) {
			out = t.Token
			return nil
		}
		t, e = current(l, f.Token, f.Owner)
		if e != nil {
			return e
		}
		if t.State == Terminal || len(f.Changes) != len(t.Components) {
			return ErrBounds
		}
		t.Token.Generation, e = next(t.Token.Generation)
		if e != nil {
			return e
		}
		t.State = Terminal
		t.Evidence = f.Evidence
		t.Changes = append([]Change(nil), f.Changes...)
		if e = validateTicket(t); e != nil {
			return e
		}
		// This settlement may remove data added by an earlier terminal ticket in
		// a higher slot. Make those prior deltas the durable baseline before
		// writing this terminal, so reload never depends on numeric slot order.
		if l.cp.Applied != l.diskApplied {
			if e = q.saveCheckpoint(l); e != nil {
				return e
			}
		}
		if e = terminalApply(l.cp, t); e != nil {
			return e
		}
		if e = q.saveTicket(l, t); e != nil {
			return e
		}
		out = t.Token
		return nil
	})
	return out, e
}
func changesEqual(a, b []Change) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func (q *Coordinator) Checkpoint(ctx context.Context) error {
	return q.locked(ctx, func() error {
		l, e := q.load()
		if e != nil {
			return e
		}
		return q.saveCheckpoint(l)
	})
}
func (q *Coordinator) Snapshot(ctx context.Context) (Snapshot, error) {
	var out Snapshot
	e := q.locked(ctx, func() error {
		l, e := q.load()
		if e != nil {
			return e
		}
		c := l.cp
		out.Revision = c.Generation
		out.Epoch = c.Epoch
		out.Maintenance = c.Maintenance
		out.Installation = c.Installation
		logical, _, capacity, e := promises(c, l.tickets)
		out.Promised = logical
		if e == nil {
			for i, p := range capacity {
				if !p.zero() {
					row := c.CapacityConstraints[i]
					out.CapacityPromised = append(out.CapacityPromised, CapacityPromise{row.ID, row.Generation, p})
				}
			}
			sort.Slice(out.CapacityPromised, func(i, j int) bool {
				return bytes.Compare(out.CapacityPromised[i].Constraint[:], out.CapacityPromised[j].Constraint[:]) < 0
			})
		}
		if e != nil {
			return e
		}
		for _, r := range c.Roots {
			if !r.ID.zero() {
				out.Roots = append(out.Roots, r)
			}
		}
		for _, w := range c.Workspaces {
			if !w.ID.zero() {
				out.Workspaces = append(out.Workspaces, w)
			}
		}
		for _, t := range l.tickets {
			if t.State != Empty && !retired(t, l.diskApplied[t.Token.Slot]) {
				out.Tickets = append(out.Tickets, t)
			}
		}
		return nil
	})
	return out, e
}

// Manifest returns only the exact live ticket's bounded immutable plan. Applied
// terminal manifests are retired and must never be dereferenced after reuse.
func (q *Coordinator) Manifest(ctx context.Context, token Token, owner Digest) (Request, error) {
	var out Request
	e := q.locked(ctx, func() error {
		l, e := q.load()
		if e != nil {
			return e
		}
		t, e := current(l, token, owner)
		if e != nil {
			return e
		}
		if retired(t, l.diskApplied[token.Slot]) {
			return ErrStale
		}
		b, e := q.readAt("manifests", int64(token.Slot)*ManifestBytes, ManifestBytes)
		if e != nil {
			return e
		}
		initial, entries, e := decodeManifest(b, t, q.anchor)
		if e != nil {
			return e
		}
		out = Request{Workflow: initial.Workflow, Operation: initial.Operation, Owner: initial.Owner, Components: initial.Components, Fanout: cloneFanout(initial.Fanout), Entries: entries}
		return nil
	})
	return out, e
}

// ConfigurationSnapshot reads complete policy/filesystem configuration at one
// revision under Q. It performs no checkpoint, repair, registration or callback.
// A corrupt or unknown durable state yields a zero result and its refusal.
func (q *Coordinator) ConfigurationSnapshot(ctx context.Context) (Configuration, error) {
	var out Configuration
	err := q.locked(ctx, func() error {
		l, err := q.load()
		if err != nil {
			return err
		}
		c := l.cp
		out = Configuration{Revision: c.Generation, Epoch: c.Epoch, Maintenance: c.Maintenance, Policy: c.Policy}
		for _, f := range c.Filesystems {
			if !f.ID.zero() {
				out.Filesystems = append(out.Filesystems, f)
			}
		}
		for _, row := range c.CapacityConstraints {
			if !row.ID.zero() {
				out.CapacityConstraints = append(out.CapacityConstraints, row)
			}
		}
		return nil
	})
	if err != nil {
		return Configuration{}, err
	}
	return out, nil
}

// StatusSnapshot projects policy, charged accounting and promises from exactly
// one load. It never persists replayed terminals or reads domain journals/trees.
func (q *Coordinator) StatusSnapshot(ctx context.Context) (StatusSnapshot, error) {
	var out StatusSnapshot
	err := q.locked(ctx, func() error {
		l, err := q.load()
		if err != nil {
			return err
		}
		out, err = projectStatus(l)
		return err
	})
	if err != nil {
		return StatusSnapshot{}, err
	}
	return out, nil
}

func statusPromiseAdd(dst *StatusPromises, category uint8, peak, settlement Vector) error {
	if category >= Categories {
		return ErrBounds
	}
	p, err := add(dst.Peak[category], peak)
	if err != nil {
		return err
	}
	s, err := add(dst.Settlement[category], settlement)
	if err != nil {
		return err
	}
	if _, err = add(p, s); err != nil {
		return err
	}
	dst.Peak[category], dst.Settlement[category] = p, s
	return nil
}

func projectStatus(l *loaded) (StatusSnapshot, error) {
	c := l.cp
	// Keep the admission algorithm authoritative for component registration,
	// combined logical bounds and capacity demand (Peak already includes it all).
	_, _, capacity, err := promises(c, l.tickets)
	if err != nil {
		return StatusSnapshot{}, err
	}
	out := StatusSnapshot{Revision: c.Generation, Epoch: c.Epoch, Maintenance: c.Maintenance, Policy: c.Policy, Installation: c.Installation}
	roots, workspaces := make(map[ID]int), make(map[ID]int)
	for _, r := range c.Roots {
		if !r.ID.zero() {
			roots[r.ID] = len(out.Roots)
			out.Roots = append(out.Roots, StatusRoot{Root: r})
		}
	}
	for _, w := range c.Workspaces {
		if !w.ID.zero() {
			workspaces[w.ID] = len(out.Workspaces)
			out.Workspaces = append(out.Workspaces, StatusWorkspace{Workspace: w})
		}
	}
	for _, f := range c.Filesystems {
		if !f.ID.zero() {
			out.Filesystems = append(out.Filesystems, f)
		}
	}
	for i, row := range c.CapacityConstraints {
		if !row.ID.zero() {
			out.CapacityConstraints = append(out.CapacityConstraints, row)
			if !capacity[i].zero() {
				out.CapacityPromised = append(out.CapacityPromised, CapacityPromise{row.ID, row.Generation, capacity[i]})
			}
		}
	}
	sort.Slice(out.CapacityPromised, func(i, j int) bool {
		return bytes.Compare(out.CapacityPromised[i].Constraint[:], out.CapacityPromised[j].Constraint[:]) < 0
	})
	for i, t := range l.tickets {
		switch t.State {
		case Empty:
			continue
		case Reserved:
			out.Tickets.Reserved++
			out.Tickets.Active++
		case Staging:
			out.Tickets.Staging++
			out.Tickets.Active++
		case Dispatched:
			out.Tickets.Dispatched++
			out.Tickets.Active++
		case Uncertain:
			out.Tickets.Uncertain++
		case Terminal:
			if retired(t, l.diskApplied[i]) {
				out.Tickets.TerminalApplied++
			} else {
				out.Tickets.TerminalUnapplied++
			}
		default:
			return StatusSnapshot{}, ErrUnknown
		}
		out.Tickets.Nonempty++
		if t.State == Terminal {
			continue
		}
		if err = walkComponents(t, func(_ int, v Component, _ bool) error {
			if err = statusPromiseAdd(&out.Promises, v.Category, v.Physical, v.SettlementPhysical); err != nil {
				return err
			}
			r, ok := roots[v.Root]
			if !ok {
				return ErrAuthority
			}
			if err = statusPromiseAdd(&out.Roots[r].Promises, v.Category, v.Physical, v.SettlementPhysical); err != nil {
				return err
			}
			if !v.Workspace.zero() {
				w, ok := workspaces[v.Workspace]
				if !ok {
					return ErrAuthority
				}
				if err = statusPromiseAdd(&out.Workspaces[w].Promises, v.Category, v.Attribution, v.SettlementAttribution); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return StatusSnapshot{}, err
		}
	}
	return out, nil
}

// CheckPolicyTransition is detached exact prefix observation. It does not adopt
// an unexplained maintenance prefix or authorize recovery. The original owner
// must first validate its durable typed V6 Prepared/Active/aborted relation.
func (q *Coordinator) CheckPolicyTransition(ctx context.Context, p PolicyTransition, phase PolicyPhase) error {
	return q.locked(ctx, func() error {
		l, raw, err := q.policyLoad()
		if err != nil {
			return err
		}
		if err = q.policyCompare(l, raw, p, phase); err != nil {
			return err
		}
		return ctx.Err()
	})
}

// policyStep never retries or falls back to generic maintenance. Every effect
// compares the entire retained Q frame under the very same original Q lock.
func (q *Coordinator) policyStep(ctx context.Context, p PolicyTransition, from, to PolicyPhase) error {
	if !(from == PolicyQ0 && to == PolicyQ1 || from == PolicyQ1 && to == PolicyQ2 || from == PolicyQ2 && to == PolicyQ3 || from == PolicyQ1 && to == PolicyAborted) {
		return ErrBounds
	}
	return q.locked(ctx, func() error {
		l, raw, err := q.policyLoad()
		if err != nil {
			return err
		}
		if err = q.policyCompare(l, raw, p, from); err != nil {
			return err
		}
		target, err := policyPhaseCheckpoint(*l.cp, p, to)
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		// saveCheckpoint alone advances the revision and rotates the bank. Keep
		// that original checked write/sync/close/readback path and error intact.
		target.Generation = l.cp.Generation
		*l.cp = target
		return q.saveCheckpoint(l)
	})
}

// BeginPolicyTransition requires the host's checked Prepared to already be
// durable. These Q primitives do not issue host control/native/effect grants.
func (q *Coordinator) BeginPolicyTransition(ctx context.Context, p PolicyTransition) error {
	return q.policyStep(ctx, p, PolicyQ0, PolicyQ1)
}

// ApplyPolicyTransition atomically updates Policy and at most one exact current
// workspace. Unlike legacy UpdatePolicy, default equality never propagates.
// Existing usage, applied tuples and already-admitted ticket promises remain.
func (q *Coordinator) ApplyPolicyTransition(ctx context.Context, p PolicyTransition) error {
	return q.policyStep(ctx, p, PolicyQ1, PolicyQ2)
}

// FinishPolicyTransition is used only after typed committed Active is durable.
func (q *Coordinator) FinishPolicyTransition(ctx context.Context, p PolicyTransition) error {
	return q.policyStep(ctx, p, PolicyQ2, PolicyQ3)
}

// AbortPolicyTransition clears only the exact still-unapplied Q1 prefix after
// the owner's typed aborted publication. Q2 cannot roll back through this API.
func (q *Coordinator) AbortPolicyTransition(ctx context.Context, p PolicyTransition) error {
	return q.policyStep(ctx, p, PolicyQ1, PolicyAborted)
}
