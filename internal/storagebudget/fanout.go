package storagebudget

import "bytes"

func cloneFanout(f *WorkspaceFanout) *WorkspaceFanout {
	if f == nil {
		return nil
	}
	return &WorkspaceFanout{Workspaces: append([]WorkspaceReference(nil), f.Workspaces...), Memberships: append([]WorkspaceMembership(nil), f.Memberships...)}
}

func sameFanout(a, b *WorkspaceFanout) bool {
	if a == nil || b == nil {
		return a == b
	}
	if len(a.Workspaces) != len(b.Workspaces) || len(a.Memberships) != len(b.Memberships) {
		return false
	}
	for i := range a.Workspaces {
		if a.Workspaces[i] != b.Workspaces[i] {
			return false
		}
	}
	for i := range a.Memberships {
		if a.Memberships[i] != b.Memberships[i] {
			return false
		}
	}
	return true
}

func cloneTicket(t Ticket) Ticket {
	t.Components = append([]Component(nil), t.Components...)
	t.Changes = append([]Change(nil), t.Changes...)
	t.Fanout = cloneFanout(t.Fanout)
	return t
}

func member(m WorkspaceMembership, i int) bool {
	return m[i/8]&(1<<uint(i%8)) != 0
}

// Only a complete immutable manifest can supply membership. Ticket-page parsing
// deliberately cannot call this: retired manifests may already be overwritten.
func validateFanout(t Ticket) error {
	f := t.Fanout
	if f == nil {
		return nil
	}
	if len(f.Workspaces) > MaxWorkspaces || len(f.Memberships) != len(t.Components) {
		return ErrBounds
	}
	for i, w := range f.Workspaces {
		if w.ID.zero() || w.Generation == 0 || i > 0 && bytes.Compare(f.Workspaces[i-1].ID[:], w.ID[:]) >= 0 {
			return ErrAuthority
		}
	}
	var used WorkspaceMembership
	for g, m := range f.Memberships {
		v := t.Components[g]
		if !v.Workspace.zero() || v.WorkspaceGeneration != 0 {
			return ErrBounds
		}
		selected := false
		for i := 0; i < MaxWorkspaces; i++ {
			if !member(m, i) {
				continue
			}
			if i >= len(f.Workspaces) {
				return ErrBounds
			}
			selected = true
			used[i/8] |= 1 << uint(i%8)
		}
		_, attribution, err := v.total()
		if err != nil {
			return err
		}
		if selected == attribution.zero() {
			return ErrBounds
		}
		if t.State == Terminal && !selected && (!t.Changes[g].AttributionAdd.zero() || !t.Changes[g].AttributionRemove.zero()) {
			return ErrBounds
		}
	}
	for i := range f.Workspaces {
		if !member(used, i) {
			return ErrBounds
		}
	}
	// Exact expanded keys must remain unique even when groups share a root.
	type key struct {
		root, workspace ID
		category        uint8
	}
	seen := make(map[key]bool)
	return walkComponents(t, func(_ int, c Component, _ bool) error {
		k := key{c.Root, c.Workspace, c.Category}
		if seen[k] {
			return ErrBounds
		}
		seen[k] = true
		return nil
	})
}

// Walk without materializing up to16*1024 flat components. The first member
// carries physical/capacity quantities; all other members retain exact custody
// and capacity binding with zero physical promises. A no-member group is one
// ordinary unattributed physical component. Callers validate membership first.
func walkComponents(t Ticket, visit func(int, Component, bool) error) error {
	if t.Fanout == nil {
		for i, c := range t.Components {
			if err := visit(i, c, true); err != nil {
				return err
			}
		}
		return nil
	}
	if len(t.Fanout.Memberships) != len(t.Components) {
		return ErrUnknown
	}
	for g, c := range t.Components {
		first := true
		for i, w := range t.Fanout.Workspaces {
			if !member(t.Fanout.Memberships[g], i) {
				continue
			}
			v := c
			v.Workspace, v.WorkspaceGeneration = w.ID, w.Generation
			if !first {
				v.Physical, v.SettlementPhysical = Vector{}, Vector{}
				for j := range v.Capacity {
					v.Capacity[j].Peak = CapacityVector{}
				}
			}
			if err := visit(g, v, first); err != nil {
				return err
			}
			first = false
		}
		if first {
			if err := visit(g, c, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func componentEnvelopeCovers(a, b Component) bool {
	if !componentKey(a, b) || a.CapacityCount != b.CapacityCount || a.CapacityBinding != b.CapacityBinding {
		return false
	}
	ap, aw, err := a.total()
	if err != nil {
		return false
	}
	bp, bw, err := b.total()
	if err != nil || !ap.le(bp) || !aw.le(bw) {
		return false
	}
	for j := 0; j < int(a.CapacityCount); j++ {
		x, y := a.Capacity[j], b.Capacity[j]
		if x.Constraint != y.Constraint || x.Generation != y.Generation || !x.Peak.le(y.Peak) {
			return false
		}
	}
	return true
}
