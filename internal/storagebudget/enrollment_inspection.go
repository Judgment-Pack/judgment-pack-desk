package storagebudget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"reflect"
	"time"
)

// EnrollmentInspection is detached observation from one original strict Q load.
// The exact original banks preserve physical row order and all unused rows.
// Neither these bytes nor the decoded view grant mutation, native custody,
// reservation, repair, or a publish capability. The original host must retain
// its separately established complete writer exclusion throughout enrollment.
// The raw banks total exactly 10 MiB; no manifest content is exported.
type EnrollmentInspection struct {
	View           HostInspection
	CheckpointBank uint8
	TicketBanks    [Slots]uint8
	Checkpoints    [2][]byte
	Tickets        [2][]byte
}

// CaptureReserve observes the exact ordinary Reserve input under the existing
// Q gate. Unlike InspectEnrollment or policyLoad it permits live reservations
// and terminal replay. No Q or outer-host state is changed. The host must keep
// its original outer holds through ReserveCompared; comparison detects other
// shared writers that changed Q in the intervening computation.
func (q *Coordinator) CaptureReserve(ctx context.Context, request Request) (ReserveComparison, EnrollmentInspection, error) {
	var out ReserveComparison
	var raw EnrollmentInspection
	digest, err := reserveRequestDigest(request)
	if err != nil {
		return out, raw, err
	}
	err = q.locked(ctx, func() error {
		l, err := q.loadWithEnrollment(false, &raw)
		if err != nil {
			return err
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
		out = ReserveComparison{Original: q.policyCommitment(l, raw), Request: digest, Slot: uint8(slot)}
		raw.View = hostInspection(l)
		return ctx.Err()
	})
	if err != nil {
		return ReserveComparison{}, EnrollmentInspection{}, err
	}
	return out, raw, nil
}

// InspectEnrollment returns both original checkpoint and ticket banks and the
// HostInspection derived by that same validated load. All nonempty tickets must
// already be exactly persisted-applied terminals, regardless of workflow. This
// also works during the exact maintenance prefixes of an existing bridge; the
// host separately compares its complete accepted Q0/Q1/Q2/Q3 chain. No state is
// written, replay incorporated, maintenance entered, or damaged peer repaired.
func (q *Coordinator) InspectEnrollment(ctx context.Context) (EnrollmentInspection, error) {
	var out EnrollmentInspection
	err := q.locked(ctx, func() error {
		l, err := q.loadWithEnrollment(true, &out)
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if l.cp.Applied != l.diskApplied {
			return ErrUnknown
		}
		out.View = hostInspection(l)
		return nil
	})
	if err != nil {
		return EnrollmentInspection{}, err
	}
	return out, nil
}

// InspectAction projects the same strict load and original raw banks while an
// owner's already recorded action is pending. Unlike InspectEnrollment it does
// not require retirement and does not produce an enrollment/census input. The
// HostInspection retains disk-applied tuples even when terminal replay affects
// its charged usage. Observation supplies no token, permit, repair or mutation.
func (q *Coordinator) InspectAction(ctx context.Context) (EnrollmentInspection, error) {
	var out EnrollmentInspection
	err := q.locked(ctx, func() error {
		l, err := q.loadWithEnrollment(false, &out)
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		out.View = hostInspection(l)
		return nil
	})
	if err != nil {
		return EnrollmentInspection{}, err
	}
	return out, nil
}

// ValidateAppliedRequest compares a host-retained original request with the
// complete current disk-applied terminal. It reconstructs only the canonical
// manifest commitment; it never reads a retired manifest or returns its payload.
// initial is the original Reserved token, independently retained/derived by the
// owning workflow. Observation grants no write, replay, or receipt authority.
func (q *Coordinator) ValidateAppliedRequest(ctx context.Context, epoch uint64, row HostSlotState, initial Token, request Request) error {
	return q.locked(ctx, func() error {
		l, err := q.load()
		if err != nil {
			return err
		}
		t := row.Ticket
		if int(t.Token.Slot) >= Slots || t.State != Terminal || epoch != l.cp.Epoch || row.PersistedApplied != t.Token || row.PersistedNextSequence <= t.Token.Sequence || !reflect.DeepEqual(t, l.tickets[t.Token.Slot]) {
			return ErrStale
		}
		a := l.diskApplied[t.Token.Slot]
		if row.PersistedApplied != (Token{Slot: t.Token.Slot, ID: a.ID, Sequence: a.Sequence, Generation: a.Generation}) || row.PersistedNextSequence != a.NextSequence {
			return ErrStale
		}
		if initial.Slot != t.Token.Slot || initial.ID != t.Token.ID || initial.Sequence != t.Token.Sequence || initial.Generation == 0 || initial.Generation > t.Token.Generation || request.Owner != t.Owner || request.Workflow != t.Workflow || request.Operation != t.Operation || !reflect.DeepEqual(request.Components, t.Components) || (request.Fanout != nil) != (t.Fanout != nil) {
			return ErrUnknown
		}
		original := t
		// Membership belongs to the separately retained original request. The
		// applied terminal's manifest is retired and may no longer be read.
		original.Fanout = cloneFanout(request.Fanout)
		original.Token = initial
		manifest, err := encodeManifest(original, request.Entries, q.anchor)
		if err != nil {
			return err
		}
		if t.ManifestLength != ManifestBytes || sha256.Sum256(manifest) != t.Manifest {
			return ErrUnknown
		}
		return ctx.Err()
	})
}

// EncodePolicyTransition is a separate fixed host operand. It does not change
// any existing Q codec. Padding is zero and decode requires byte-exact canonical
// re-encoding. All original authority remains with the owning host operation.
func EncodePolicyTransition(p PolicyTransition) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	b := make([]byte, PolicyTransitionBytes)
	w := writer{b: b}
	w.put([]byte("DSKPOL6\x00"))
	x := p.Original
	w.id(x.Installation)
	w.digest(x.Binding)
	w.u64(x.Revision)
	w.u64(x.Epoch)
	w.u8(x.CheckpointBank)
	w.digest(x.Checkpoint)
	w.digest(x.Tickets[0])
	w.digest(x.Tickets[1])
	w.digest(x.Manifests)
	for _, v := range []Policy{p.Before, p.After} {
		w.limits(v.Installation)
		w.limits(v.Workspace)
		w.u64(uint64(v.ObservationMaxAge))
	}
	w.id(p.Workspace)
	w.u64(p.WorkspaceGeneration)
	w.limits(p.BeforeLimits)
	w.limits(p.AfterLimits)
	if w.err != nil {
		return nil, w.err
	}
	return b, nil
}

func DecodePolicyTransition(b []byte) (PolicyTransition, error) {
	var p PolicyTransition
	if len(b) != PolicyTransitionBytes || !bytes.Equal(b[:8], []byte("DSKPOL6\x00")) {
		return p, ErrBounds
	}
	r := reader{b: b, n: 8}
	x := &p.Original
	x.Installation = r.id()
	x.Binding = r.digest()
	x.Revision = r.u64()
	x.Epoch = r.u64()
	x.CheckpointBank = r.u8()
	x.Checkpoint = r.digest()
	x.Tickets[0] = r.digest()
	x.Tickets[1] = r.digest()
	x.Manifests = r.digest()
	for _, v := range []*Policy{&p.Before, &p.After} {
		v.Installation = r.limits()
		v.Workspace = r.limits()
		v.ObservationMaxAge = time.Duration(r.u64())
	}
	p.Workspace = r.id()
	p.WorkspaceGeneration = r.u64()
	p.BeforeLimits = r.limits()
	p.AfterLimits = r.limits()
	if r.end() != nil {
		return PolicyTransition{}, ErrBounds
	}
	canonical, err := EncodePolicyTransition(p)
	if err != nil || !bytes.Equal(b, canonical) {
		return PolicyTransition{}, ErrBounds
	}
	return p, nil
}

// policyLoad refuses any terminal-unapplied row even if terminal replay happens
// to leave some counters unchanged. Other already-admitted promises remain live.
// Raw canonical equality also prevents incorporating replay into a comparison.
func (q *Coordinator) policyLoad() (*loaded, EnrollmentInspection, error) {
	var raw EnrollmentInspection
	l, err := q.loadWithEnrollment(false, &raw)
	if err != nil {
		return nil, raw, err
	}
	for i, t := range l.tickets {
		if t.State == Terminal && !retired(t, l.diskApplied[i]) {
			return nil, raw, ErrBusy
		}
	}
	canonical, err := encodeCheckpoint(l.cp, q.anchor)
	if err != nil {
		return nil, raw, err
	}
	if l.cp.Applied != l.diskApplied || !bytes.Equal(canonical, raw.Checkpoints[l.cpBank]) {
		return nil, raw, ErrUnknown
	}
	return l, raw, nil
}

func policyLiveManifests(l *loaded) Digest {
	h := sha256.New()
	h.Write([]byte("desk-policy-live-manifests-v6\x00"))
	for i, t := range l.tickets {
		var row [42]byte
		row[0] = byte(i)
		if t.State != Empty && !retired(t, l.diskApplied[i]) {
			row[1] = 1
			binary.LittleEndian.PutUint64(row[2:10], uint64(t.ManifestLength))
			copy(row[10:], t.Manifest[:])
		}
		h.Write(row[:])
	}
	var d Digest
	copy(d[:], h.Sum(nil))
	return d
}

func (q *Coordinator) policyCommitment(l *loaded, raw EnrollmentInspection) PolicyCommitment {
	return PolicyCommitment{q.anchor.Installation, q.anchor.Binding, l.cp.Generation, l.cp.Epoch, uint8(l.cpBank), policyBytesDigest("selected-checkpoint", raw.Checkpoints[l.cpBank]), [2]Digest{policyBytesDigest("tickets-a", raw.Tickets[0]), policyBytesDigest("tickets-b", raw.Tickets[1])}, policyLiveManifests(l)}
}

// CapturePolicyTransition reads once under Q's existing lock. It captures no
// authority and performs no maintenance or policy effect. Caller exclusion and
// a later durable Prepared are required; Begin compares the complete state again.
func (q *Coordinator) CapturePolicyTransition(ctx context.Context, revision uint64, after Policy, workspace ID, generation uint64, limits Limits) (PolicyTransition, error) {
	var out PolicyTransition
	err := q.locked(ctx, func() error {
		l, raw, err := q.policyLoad()
		if err != nil {
			return err
		}
		if l.cp.Maintenance || l.cp.Generation != revision {
			return ErrStale
		}
		out = PolicyTransition{Original: q.policyCommitment(l, raw), Before: l.cp.Policy, After: after, Workspace: workspace, WorkspaceGeneration: generation, AfterLimits: limits}
		if !workspace.zero() {
			i := workspaceIndex(l.cp, workspace)
			if i < 0 || l.cp.Workspaces[i].Generation != generation {
				return ErrAuthority
			}
			out.BeforeLimits = l.cp.Workspaces[i].Limits
		}
		if err = out.Validate(); err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil {
		return PolicyTransition{}, err
	}
	return out, nil
}

// policyPhaseCheckpoint derives only a forward policy prefix from an original
// complete checkpoint. No accounting/root/unused/applied field is synthesized.
func policyPhaseCheckpoint(base checkpoint, p PolicyTransition, phase PolicyPhase) (checkpoint, error) {
	if phase > PolicyAborted {
		return checkpoint{}, ErrBounds
	}
	c := base
	c.Generation = p.Original.Revision
	c.Epoch = p.Original.Epoch
	c.Maintenance = false
	c.Policy = p.Before
	if !p.Workspace.zero() {
		i := workspaceIndex(&c, p.Workspace)
		if i < 0 || c.Workspaces[i].Generation != p.WorkspaceGeneration {
			return checkpoint{}, ErrAuthority
		}
		c.Workspaces[i].Limits = p.BeforeLimits
	}
	switch phase {
	case PolicyQ1:
		c.Generation++
		c.Epoch++
		c.Maintenance = true
	case PolicyQ2, PolicyQ3:
		c.Generation += uint64(phase)
		c.Epoch++
		c.Maintenance = phase == PolicyQ2
		c.Policy = p.After
		if !p.Workspace.zero() {
			c.Workspaces[workspaceIndex(&c, p.Workspace)].Limits = p.AfterLimits
		}
	case PolicyAborted:
		c.Generation += 2
		c.Epoch++
	}
	return c, nil
}

// policyCompare uses actual remaining bytes to invert the scalar/one-row delta.
// Both ticket banks and live manifests remain the original bytes. Only Q0's
// arbitrary checkpoint peer is excluded; later peers must be the exact previous
// forward prefix, under the original anchor and strict Q decoder.
func (q *Coordinator) policyCompare(l *loaded, raw EnrollmentInspection, p PolicyTransition, phase PolicyPhase) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if phase > PolicyAborted || p.Original.Installation != q.anchor.Installation || p.Original.Binding != q.anchor.Binding {
		return ErrAuthority
	}
	base, err := policyPhaseCheckpoint(*l.cp, p, PolicyQ0)
	if err != nil {
		return err
	}
	b, err := encodeCheckpoint(&base, q.anchor)
	if err != nil {
		return err
	}
	if policyBytesDigest("selected-checkpoint", b) != p.Original.Checkpoint || policyBytesDigest("tickets-a", raw.Tickets[0]) != p.Original.Tickets[0] || policyBytesDigest("tickets-b", raw.Tickets[1]) != p.Original.Tickets[1] || policyLiveManifests(l) != p.Original.Manifests {
		return ErrStale
	}
	expected, err := policyPhaseCheckpoint(base, p, phase)
	if err != nil {
		return err
	}
	b, err = encodeCheckpoint(&expected, q.anchor)
	if err != nil {
		return err
	}
	bank := int(p.Original.CheckpointBank)
	if phase == PolicyQ1 || phase == PolicyQ3 {
		bank = 1 - bank
	}
	if l.cpBank != bank || !bytes.Equal(b, raw.Checkpoints[bank]) {
		return ErrStale
	}
	if phase != PolicyQ0 {
		previous := phase - 1
		if phase == PolicyAborted {
			previous = PolicyQ1
		}
		peer, err := policyPhaseCheckpoint(base, p, previous)
		if err != nil {
			return err
		}
		b, err = encodeCheckpoint(&peer, q.anchor)
		if err != nil {
			return err
		}
		if !bytes.Equal(b, raw.Checkpoints[1-bank]) {
			return ErrStale
		}
	}
	return nil
}

func policyBytesDigest(kind string, raw []byte) Digest {
	h := sha256.New()
	h.Write([]byte("desk-policy-v6/" + kind + "\x00"))
	h.Write(raw)
	var d Digest
	copy(d[:], h.Sum(nil))
	return d
}
