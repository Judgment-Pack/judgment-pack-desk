package storagebudget

import "context"

// HostSlotState distinguishes the current bounded ticket page from the tuple
// actually incorporated by the durable checkpoint. A terminal can be replayed
// into the loaded in-memory checkpoint without being PersistedApplied yet.
// Empty applied identities have zero ID/sequence/generation; Slot still names
// their bounded row. No current/retired ticket is omitted from this view.
type HostSlotState struct {
	Ticket                Ticket
	PersistedApplied      Token
	PersistedNextSequence uint64
}

// HostState is one bounded, detached read under Q. This is metadata for the
// original host journal, not a recovered write capability or action permission.
// Inspecting it does not checkpoint, repair, advance, or retire anything.
type HostState struct {
	Revision, Epoch uint64
	Maintenance     bool
	Slots           [Slots]HostSlotState
}

// InspectHostState returns complete current ticket metadata and actual persisted
// applied tuples at one validated revision. It returns a zero result on error.
// Callers must compare full token identities and original epoch/custody facts;
// a matching slot number or replayed in-memory terminal is no retirement proof.
func (q *Coordinator) InspectHostState(ctx context.Context) (HostState, error) {
	var out HostState
	err := q.locked(ctx, func() error {
		l, err := q.load()
		if err != nil {
			return err
		}
		out.Revision = l.cp.Generation
		out.Epoch = l.cp.Epoch
		out.Maintenance = l.cp.Maintenance
		for i, ticket := range l.tickets {
			ticket = cloneTicket(ticket)
			a := l.diskApplied[i]
			out.Slots[i] = HostSlotState{Ticket: ticket, PersistedApplied: Token{Slot: uint8(i), ID: a.ID, Sequence: a.Sequence, Generation: a.Generation}, PersistedNextSequence: a.NextSequence}
		}
		return nil
	})
	if err != nil {
		return HostState{}, err
	}
	return out, nil
}

// RegisteredRoot contains the complete immutable v2 definition. Known/Usage
// are deliberately absent: inspection neither measures nor grants authority.
type RegisteredRoot struct {
	ID                    ID
	Owner, Identity       Digest
	Filesystem, Workspace ID
	Generation            uint64
	Locator               string
	CapacityCount         uint8
	CapacityRefs          [MaxCapacityRefs]CapacityRef
	CapacityBinding       Digest
}
type RegisteredWorkspace struct {
	ID         ID
	Generation uint64
	Limits     Limits
}
type HostInspection struct {
	Configuration Configuration
	Roots         []RegisteredRoot
	Workspaces    []RegisteredWorkspace
	Slots         [Slots]HostSlotState
}

// InspectHost projects exactly one validated load. Persisted tuples are from
// diskApplied, never replayed checkpoint state. Every dynamic slice is detached.
// This DTO is observation metadata, not custody or recovered write permission.
func (q *Coordinator) InspectHost(ctx context.Context) (HostInspection, error) {
	var out HostInspection
	err := q.locked(ctx, func() error {
		l, err := q.load()
		if err != nil {
			return err
		}
		c := l.cp
		out.Configuration = Configuration{Revision: c.Generation, Epoch: c.Epoch, Maintenance: c.Maintenance, Policy: c.Policy}
		for _, f := range c.Filesystems {
			if !f.ID.zero() {
				out.Configuration.Filesystems = append(out.Configuration.Filesystems, f)
			}
		}
		for _, r := range c.CapacityConstraints {
			if !r.ID.zero() {
				out.Configuration.CapacityConstraints = append(out.Configuration.CapacityConstraints, r)
			}
		}
		for _, r := range c.Roots {
			if !r.ID.zero() {
				out.Roots = append(out.Roots, RegisteredRoot{ID: r.ID, Owner: r.Owner, Identity: r.Identity, Filesystem: r.Filesystem, Workspace: r.Workspace, Generation: r.Generation, Locator: r.Locator, CapacityCount: r.CapacityCount, CapacityRefs: r.CapacityRefs, CapacityBinding: r.CapacityBinding})
			}
		}
		for _, w := range c.Workspaces {
			if !w.ID.zero() {
				out.Workspaces = append(out.Workspaces, RegisteredWorkspace{ID: w.ID, Generation: w.Generation, Limits: w.Limits})
			}
		}
		for i, t := range l.tickets {
			t = cloneTicket(t)
			a := l.diskApplied[i]
			out.Slots[i] = HostSlotState{Ticket: t, PersistedApplied: Token{Slot: uint8(i), ID: a.ID, Sequence: a.Sequence, Generation: a.Generation}, PersistedNextSequence: a.NextSequence}
		}
		return nil
	})
	if err != nil {
		return HostInspection{}, err
	}
	return out, nil
}
