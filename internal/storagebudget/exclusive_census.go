package storagebudget

import (
	"context"
	"errors"
)

// ExclusiveCensusSnapshot is one original-coordinator observation, never host,
// native, action or allocation authority. The original host must retain complete
// exclusive writer custody across measurement and publication. No callback runs
// under Q; no snapshot can authorize clearing a maintenance fence.
type ExclusiveCensusSnapshot struct {
	q      *Coordinator
	digest Digest
	epoch  uint64
	used   bool // q.gate
}

func (q *Coordinator) CaptureExclusiveCensus(ctx context.Context) (*ExclusiveCensusSnapshot, HostInspection, error) {
	var out HostInspection
	var snap *ExclusiveCensusSnapshot
	err := q.locked(ctx, func() error {
		l, err := q.loadForCensus(true)
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if l.cp.Maintenance || l.cp.Epoch == 0 {
			return ErrStale
		}
		if l.cp.Epoch == ^uint64(0) || l.cp.Generation == ^uint64(0) {
			return ErrBounds
		}
		out = hostInspection(l)
		snap = &ExclusiveCensusSnapshot{q: q, digest: l.censusDigest, epoch: l.cp.Epoch}
		return nil
	})
	if err != nil {
		return nil, HostInspection{}, err
	}
	return snap, out, nil
}

func (q *Coordinator) PublishExclusiveCensus(ctx context.Context, s *ExclusiveCensusSnapshot, f Census) error {
	return q.locked(ctx, func() error {
		if s == nil || s.q != q || s.used {
			return ErrAuthority
		}
		s.used = true
		if err := ctx.Err(); err != nil {
			return err
		}
		l, err := q.loadForCensus(true)
		if err != nil {
			return err
		}
		if l.cp.Maintenance || l.cp.Epoch != s.epoch || l.censusDigest != s.digest {
			return ErrStale
		}
		// Every accepted row is Empty or exactly retired. terminalApply returned
		// before altering any decoded checkpoint field, not merely its revision.
		if l.cp.Applied != l.diskApplied {
			return ErrUnknown
		}
		if err = publishCensus(l.cp, f); err != nil {
			return err
		}
		if !(Vector{MetadataBudget, 16, 1}).le(l.cp.Installation[Control]) {
			return ErrBounds
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = q.saveCheckpoint(l); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return errors.Join(ErrUnknown, err)
		}
		return nil
	})
}

// Detached inspection of the same validated load that supplied the snapshot.
func hostInspection(l *loaded) HostInspection {
	c := l.cp
	out := HostInspection{Configuration: Configuration{Revision: c.Generation, Epoch: c.Epoch, Maintenance: c.Maintenance, Policy: c.Policy}}
	for _, v := range c.Filesystems {
		if !v.ID.zero() {
			out.Configuration.Filesystems = append(out.Configuration.Filesystems, v)
		}
	}
	for _, v := range c.CapacityConstraints {
		if !v.ID.zero() {
			out.Configuration.CapacityConstraints = append(out.Configuration.CapacityConstraints, v)
		}
	}
	for _, v := range c.Roots {
		if !v.ID.zero() {
			out.Roots = append(out.Roots, RegisteredRoot{v.ID, v.Owner, v.Identity, v.Filesystem, v.Workspace, v.Generation, v.Locator, v.CapacityCount, v.CapacityRefs, v.CapacityBinding})
		}
	}
	for _, v := range c.Workspaces {
		if !v.ID.zero() {
			out.Workspaces = append(out.Workspaces, RegisteredWorkspace{v.ID, v.Generation, v.Limits})
		}
	}
	for j, t := range l.tickets {
		t = cloneTicket(t)
		a := l.diskApplied[j]
		out.Slots[j] = HostSlotState{Ticket: t, PersistedApplied: Token{uint8(j), a.ID, a.Sequence, a.Generation}, PersistedNextSequence: a.NextSequence}
	}
	return out
}
