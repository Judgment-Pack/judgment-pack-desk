package storagebudget

import (
	"bytes"
	"crypto/sha256"
	"time"
)

func (v CapacityVector) valid() bool              { return v.Bytes <= maxBytes && v.Inodes <= maxCount }
func (v CapacityVector) zero() bool               { return v == (CapacityVector{}) }
func (v CapacityVector) le(w CapacityVector) bool { return v.Bytes <= w.Bytes && v.Inodes <= w.Inodes }
func capacityAdd(a, b CapacityVector) (CapacityVector, error) {
	if !a.valid() || !b.valid() || a.Bytes > maxBytes-b.Bytes || a.Inodes > maxCount-b.Inodes {
		return CapacityVector{}, ErrBounds
	}
	return CapacityVector{a.Bytes + b.Bytes, a.Inodes + b.Inodes}, nil
}
func capacityMasked(v CapacityVector, resources uint8) bool {
	return v.valid() && resources != 0 && resources & ^uint8(CapacityBytes|CapacityInodes) == 0 && (resources&CapacityBytes != 0 || v.Bytes == 0) && (resources&CapacityInodes != 0 || v.Inodes == 0)
}
func validConstraint(v CapacityConstraint) bool {
	return !v.ID.zero() && authority(v.Identity) && v.Generation != 0 && capacityMasked(v.Headroom, v.Resources)
}
func constraintIndex(c *checkpoint, id ID) int {
	// Index-only range avoids copying this complete fixed array per lookup.
	for i := range c.CapacityConstraints {
		if !id.zero() && c.CapacityConstraints[i].ID == id {
			return i
		}
	}
	return -1
}
func filesystemCapacityValid(f Filesystem, constraints map[ID]CapacityConstraint) bool {
	b, ok := constraints[f.ByteConstraint]
	if f.ID.zero() || !authority(f.Identity) || f.Generation == 0 || !authority(f.CapabilityEvidence) || !ok || b.Resources&CapacityBytes == 0 {
		return false
	}
	switch f.InodeModel {
	case InodesFinite:
		n, ok := constraints[f.InodeConstraint]
		return ok && n.Resources&CapacityInodes != 0
	case InodesDynamic:
		return f.InodeConstraint.zero()
	default:
		return false
	}
}

// CapacityBindingFor binds the declared immutable physical-policy domain. It is
// a canonical metadata digest, not custody proof, complete quota discovery or an
// allocation guarantee. The original owner verifies the applicable declared
// descriptor/native relations; explicitly unknown discovery stays separate.
func CapacityBindingFor(root Root, f Filesystem, constraints []CapacityConstraint) (Digest, error) {
	if len(constraints) > MaxCapacityConstraints || root.CapacityCount > MaxCapacityRefs {
		return Digest{}, ErrBounds
	}
	if root.ID.zero() || !authority(root.Owner) || !authority(root.Identity) || root.Generation == 0 || root.Filesystem != f.ID || root.CapacityCount == 0 {
		return Digest{}, ErrAuthority
	}
	rows := make(map[ID]CapacityConstraint, len(constraints))
	identities := make(map[Digest]bool, len(constraints))
	for _, v := range constraints {
		if !validConstraint(v) || identities[v.Identity] {
			return Digest{}, ErrAuthority
		}
		if _, exists := rows[v.ID]; exists {
			return Digest{}, ErrAuthority
		}
		rows[v.ID] = v
		identities[v.Identity] = true
	}
	return capacityBinding(root, f, rows)
}

func capacityBinding(root Root, f Filesystem, rows map[ID]CapacityConstraint) (Digest, error) {
	if root.ID.zero() || !authority(root.Owner) || !authority(root.Identity) || root.Generation == 0 || root.Filesystem != f.ID || root.CapacityCount == 0 || root.CapacityCount > MaxCapacityRefs {
		return Digest{}, ErrAuthority
	}
	if !filesystemCapacityValid(f, rows) {
		return Digest{}, ErrAuthority
	}
	w := writer{b: make([]byte, 512)}
	w.put([]byte("JPSTB002/root-capacity\x00"))
	w.id(root.ID)
	w.digest(root.Owner)
	w.digest(root.Identity)
	w.u64(root.Generation)
	w.id(root.Filesystem)
	w.id(f.ID)
	w.digest(f.Identity)
	w.u64(f.Generation)
	w.u8(f.InodeModel)
	w.digest(f.CapabilityEvidence)
	w.id(f.ByteConstraint)
	w.id(f.InodeConstraint)
	w.u8(root.CapacityCount)
	seenByte, seenInode := false, f.InodeModel == InodesDynamic
	for i, ref := range root.CapacityRefs {
		if i >= int(root.CapacityCount) {
			if ref != (CapacityRef{}) {
				return Digest{}, ErrBounds
			}
			continue
		}
		v, ok := rows[ref.Constraint]
		if !ok || ref.Generation != v.Generation || i > 0 && bytes.Compare(root.CapacityRefs[i-1].Constraint[:], ref.Constraint[:]) >= 0 {
			return Digest{}, ErrAuthority
		}
		seenByte = seenByte || ref.Constraint == f.ByteConstraint
		seenInode = seenInode || ref.Constraint == f.InodeConstraint
		w.id(v.ID)
		w.digest(v.Identity)
		w.u64(v.Generation)
		w.u8(v.Resources)
	}
	if !seenByte || !seenInode || w.err != nil {
		return Digest{}, ErrAuthority
	}
	return sha256.Sum256(w.b[:w.n]), nil
}

func validateCheckpoint(c *checkpoint) error {
	if c.Generation == 0 || !c.Policy.valid() {
		return ErrUnknown
	}
	roots := map[ID]bool{}
	workspaces := map[ID]bool{}
	filesystems := map[ID]bool{}
	constraints := map[ID]CapacityConstraint{}
	identities := map[Digest]bool{}
	for _, v := range c.CapacityConstraints {
		if v.ID.zero() {
			if v != (CapacityConstraint{}) {
				return ErrUnknown
			}
			continue
		}
		if !validConstraint(v) || identities[v.Identity] {
			return ErrUnknown
		}
		if _, ok := constraints[v.ID]; ok {
			return ErrUnknown
		}
		constraints[v.ID] = v
		identities[v.Identity] = true
	}
	for _, v := range c.Workspaces {
		if v.ID.zero() {
			continue
		}
		if workspaces[v.ID] || v.Generation == 0 || !v.Limits.valid() {
			return ErrUnknown
		}
		workspaces[v.ID] = true
		if _, e := sum(v.Usage); e != nil {
			return e
		}
	}
	for _, v := range c.Filesystems {
		if v.ID.zero() {
			if v != (Filesystem{}) {
				return ErrUnknown
			}
			continue
		}
		if filesystems[v.ID] || !filesystemCapacityValid(v, constraints) {
			return ErrUnknown
		}
		filesystems[v.ID] = true
	}
	var usage [Categories]Vector
	for _, v := range c.Roots {
		if v.ID.zero() {
			if v != (Root{}) {
				return ErrUnknown
			}
			continue
		}
		if roots[v.ID] || v.Generation == 0 || !authority(v.Owner) || !authority(v.Identity) || !textValid(v.Locator, 256, false) || !filesystems[v.Filesystem] || !v.Workspace.zero() && !workspaces[v.Workspace] {
			return ErrUnknown
		}
		roots[v.ID] = true
		f := c.Filesystems[filesystemIndex(c, v.Filesystem)]
		binding, e := capacityBinding(v, f, constraints)
		if e != nil || binding != v.CapacityBinding {
			return ErrAuthority
		}
		// Physical identity duplication is forbidden. B2 must resolve overlap and
		// hardlinks with original ownership; separate ledgers are not double owners.
		for _, prior := range c.Roots {
			if prior.ID != v.ID && !prior.ID.zero() && prior.Identity == v.Identity {
				return ErrAuthority
			}
		}
		for k, x := range v.Usage {
			var e error
			usage[k], e = add(usage[k], x)
			if e != nil {
				return e
			}
		}
	}
	if usage != c.Installation {
		return ErrUnknown
	}
	for _, a := range c.Applied {
		if a.NextSequence == 0 || a.ID.zero() && (a.Sequence != 0 || a.Generation != 0) || !a.ID.zero() && (a.Sequence == 0 || a.Generation == 0 || a.NextSequence <= a.Sequence) {
			return ErrUnknown
		}
	}
	return nil
}
func validateTicket(t Ticket) error {
	if err := validateTicketPage(t); err != nil {
		return err
	}
	return validateFanout(t)
}

// Page-only validation does not dereference an applied terminal's retired
// manifest. Live accounting additionally requires complete validateFanout.
func validateTicketPage(t Ticket) error {
	if int(t.Token.Slot) >= Slots || t.State > Terminal || len(t.Components) > MaxComponents {
		return ErrUnknown
	}
	if t.State == Empty {
		if t.Fanout != nil || !t.Token.ID.zero() || t.Token.Sequence != 0 || len(t.Components) != 0 || len(t.Changes) != 0 || t.Manifest != (Digest{}) || t.ManifestLength != 0 || t.Owner != (Digest{}) || t.Workflow != "" || t.Operation != "" || t.PolicyRevision != 0 || t.Evidence != (Digest{}) {
			return ErrUnknown
		}
		return nil
	}
	if t.Token.ID.zero() || t.Token.Sequence == 0 || t.Token.Generation == 0 || t.PolicyRevision == 0 || !authority(t.Owner) || !textValid(t.Workflow, 128, false) || !textValid(t.Operation, 128, false) || len(t.Components) == 0 {
		return ErrUnknown
	}
	if t.Manifest == (Digest{}) {
		if t.ManifestLength != 0 {
			return ErrUnknown
		}
	} else if t.ManifestLength != ManifestBytes {
		return ErrUnknown
	}
	if t.State == Terminal {
		if len(t.Changes) != len(t.Components) || !authority(t.Evidence) {
			return ErrUnknown
		}
	} else if len(t.Changes) != 0 {
		return ErrUnknown
	}
	for i, v := range t.Components {
		if v.Root.zero() || v.RootGeneration == 0 || v.Filesystem.zero() || v.FilesystemGeneration == 0 || !authority(v.Owner) || v.Category >= Categories || t.Fanout == nil && v.Workspace.zero() && (!v.Attribution.zero() || !v.SettlementAttribution.zero()) || !v.Workspace.zero() && v.WorkspaceGeneration == 0 || t.Fanout != nil && (!v.Workspace.zero() || v.WorkspaceGeneration != 0) {
			return ErrUnknown
		}
		p, w, e := v.total()
		if e != nil {
			return e
		}
		if p.zero() && w.zero() {
			return ErrBounds
		}
		if v.CapacityCount > MaxCapacityRefs {
			return ErrBounds
		}
		if v.CapacityCount == 0 || !authority(v.CapacityBinding) {
			return ErrAuthority
		}
		for j, ref := range v.Capacity {
			if j >= int(v.CapacityCount) {
				if ref != (CapacityPromise{}) {
					return ErrBounds
				}
				continue
			}
			if ref.Constraint.zero() || ref.Generation == 0 || !ref.Peak.valid() || p.zero() && !ref.Peak.zero() || j > 0 && bytes.Compare(v.Capacity[j-1].Constraint[:], ref.Constraint[:]) >= 0 {
				return ErrBounds
			}
		}
		for j := 0; j < i; j++ {
			q := t.Components[j]
			if t.Fanout == nil && v.Root == q.Root && v.Workspace == q.Workspace && v.Category == q.Category {
				return ErrBounds
			}
		}
		if t.State == Terminal {
			x := t.Changes[i]
			if !x.PhysicalAdd.le(p) || !x.AttributionAdd.le(w) || !x.PhysicalRemove.valid() || !x.AttributionRemove.valid() {
				return ErrBounds
			}
		}
	}
	return nil
}
func rootIndex(c *checkpoint, id ID) int {
	// Index-only range avoids copying this complete fixed array per lookup.
	for i := range c.Roots {
		if !id.zero() && c.Roots[i].ID == id {
			return i
		}
	}
	return -1
}
func workspaceIndex(c *checkpoint, id ID) int {
	// Index-only range avoids copying this complete fixed array per lookup.
	for i := range c.Workspaces {
		if !id.zero() && c.Workspaces[i].ID == id {
			return i
		}
	}
	return -1
}
func filesystemIndex(c *checkpoint, id ID) int {
	// Index-only range avoids copying this complete fixed array per lookup.
	for i := range c.Filesystems {
		if !id.zero() && c.Filesystems[i].ID == id {
			return i
		}
	}
	return -1
}
func registered(c *checkpoint, t Ticket, requireKnown bool) error {
	if err := validateFanout(t); err != nil {
		return err
	}
	return walkComponents(t, func(_ int, v Component, _ bool) error {
		r := rootIndex(c, v.Root)
		if r < 0 {
			return ErrAuthority
		}
		root := c.Roots[r]
		if root.Generation != v.RootGeneration || root.Owner != v.Owner {
			return ErrAuthority
		}
		if requireKnown && !root.Known {
			return ErrUnknown
		}
		if !v.Workspace.zero() {
			w := workspaceIndex(c, v.Workspace)
			if w < 0 || c.Workspaces[w].Generation != v.WorkspaceGeneration {
				return ErrStale
			}
			if requireKnown && !c.Workspaces[w].Known {
				return ErrUnknown
			}
		}
		f := filesystemIndex(c, root.Filesystem)
		if f < 0 || root.Filesystem != v.Filesystem || c.Filesystems[f].Generation != v.FilesystemGeneration {
			return ErrUnknown
		}
		if root.CapacityCount != v.CapacityCount || root.CapacityBinding != v.CapacityBinding {
			return ErrAuthority
		}
		p, _, e := v.total()
		if e != nil {
			return e
		}
		n, e := add(Vector{Files: p.Files}, Vector{Files: p.Directories})
		if e != nil {
			return e
		}
		for j := 0; j < int(v.CapacityCount); j++ {
			ref := v.Capacity[j]
			if root.CapacityRefs[j] != (CapacityRef{ref.Constraint, ref.Generation}) {
				return ErrAuthority
			}
			k := constraintIndex(c, ref.Constraint)
			if k < 0 {
				return ErrAuthority
			}
			row := c.CapacityConstraints[k]
			if ref.Generation != row.Generation || !capacityMasked(ref.Peak, row.Resources) {
				return ErrAuthority
			}
			if row.Resources&CapacityBytes != 0 && (ref.Peak.Bytes < p.Bytes || n.Files > 0 && ref.Peak.Bytes == 0) || row.Resources&CapacityInodes != 0 && ref.Peak.Inodes < n.Files {
				return ErrBounds
			}
		}
		return nil
	})
}
func retired(t Ticket, a applied) bool {
	return t.State == Terminal && t.Token.ID == a.ID && t.Token.Sequence == a.Sequence && t.Token.Generation == a.Generation
}
func terminalApply(c *checkpoint, t Ticket) error {
	if t.State != Terminal {
		return nil
	}
	a := c.Applied[t.Token.Slot]
	if retired(t, a) {
		return nil
	}
	if t.Token.Sequence < a.NextSequence {
		return ErrUnknown
	}
	if e := registered(c, t, false); e != nil {
		return e
	}
	if err := walkComponents(t, func(i int, v Component, physical bool) error {
		x := t.Changes[i]
		if !physical {
			x.PhysicalAdd, x.PhysicalRemove = Vector{}, Vector{}
		}
		r := rootIndex(c, v.Root)
		n, e := add(c.Roots[r].Usage[v.Category], x.PhysicalAdd)
		if e == nil {
			n, e = sub(n, x.PhysicalRemove)
		}
		if e != nil {
			return e
		}
		c.Roots[r].Usage[v.Category] = n
		n, e = add(c.Installation[v.Category], x.PhysicalAdd)
		if e == nil {
			n, e = sub(n, x.PhysicalRemove)
		}
		if e != nil {
			return e
		}
		c.Installation[v.Category] = n
		if !v.Workspace.zero() {
			w := workspaceIndex(c, v.Workspace)
			n, e = add(c.Workspaces[w].Usage[v.Category], x.AttributionAdd)
			if e == nil {
				n, e = sub(n, x.AttributionRemove)
			}
			if e != nil {
				return e
			}
			c.Workspaces[w].Usage[v.Category] = n
		}
		return nil
	}); err != nil {
		return err
	}
	n, e := next(t.Token.Sequence)
	if e != nil {
		return e
	}
	c.Applied[t.Token.Slot] = applied{t.Token.ID, t.Token.Sequence, t.Token.Generation, n}
	return nil
}
func limitsCheck(used, promised [Categories]Vector, l Limits) error {
	for i, u := range used {
		v, e := add(u, promised[i])
		if e != nil {
			return e
		}
		if !v.le(l.Category[i]) {
			return ErrLimit
		}
	}
	u, e := sum(used)
	if e != nil {
		return e
	}
	p, e := sum(promised)
	if e != nil {
		return e
	}
	v, e := add(u, p)
	if e != nil {
		return e
	}
	if !v.le(l.Total) {
		return ErrLimit
	}
	return nil
}
func promises(c *checkpoint, tickets [Slots]Ticket) ([Categories]Vector, [MaxWorkspaces][Categories]Vector, [MaxCapacityConstraints]CapacityVector, error) {
	var all [Categories]Vector
	var work [MaxWorkspaces][Categories]Vector
	var capacity [MaxCapacityConstraints]CapacityVector
	for _, t := range tickets {
		if t.State == Empty || t.State == Terminal {
			continue
		}
		if e := registered(c, t, false); e != nil {
			return all, work, capacity, e
		}
		if err := walkComponents(t, func(_ int, v Component, _ bool) error {
			p, w, e := v.total()
			if e != nil {
				return e
			}
			all[v.Category], e = add(all[v.Category], p)
			if e != nil {
				return e
			}
			for j := 0; j < int(v.CapacityCount); j++ {
				ref := v.Capacity[j]
				k := constraintIndex(c, ref.Constraint)
				capacity[k], e = capacityAdd(capacity[k], ref.Peak)
				if e != nil {
					return e
				}
			}
			if !v.Workspace.zero() {
				j := workspaceIndex(c, v.Workspace)
				work[j][v.Category], e = add(work[j][v.Category], w)
				if e != nil {
					return e
				}
			}
			return nil
		}); err != nil {
			return all, work, capacity, err
		}
	}
	return all, work, capacity, nil
}
func admit(c *checkpoint, tickets [Slots]Ticket, observations []CapacityObservation, now time.Time) error {
	return admitLimits(c, tickets, observations, now, false)
}

// admitLimits checks the candidate tickets. With advisory limits, a promise at
// or over an installation or workspace limit is not refused; the sums are still
// computed, so an overflow still refuses, and capacity headroom is unchanged.
func admitLimits(c *checkpoint, tickets [Slots]Ticket, observations []CapacityObservation, now time.Time, advisory bool) error {
	if c.Maintenance {
		return ErrUnknown
	}
	all, work, capacity, e := promises(c, tickets)
	if e != nil {
		return e
	}
	limited := func(e error) bool { return e != nil && !(advisory && e == ErrLimit) }
	if e = limitsCheck(c.Installation, all, c.Policy.Installation); limited(e) {
		return e
	}
	for i, v := range c.Workspaces {
		if !v.ID.zero() {
			if e = limitsCheck(v.Usage, work[i], v.Limits); limited(e) {
				return e
			}
		}
	}
	if len(observations) > MaxCapacityConstraints {
		return ErrBounds
	}
	seen := map[ID]CapacityObservation{}
	for _, o := range observations {
		if _, ok := seen[o.Constraint]; ok {
			return ErrBounds
		}
		k := constraintIndex(c, o.Constraint)
		if k < 0 {
			return ErrAuthority
		}
		f := c.CapacityConstraints[k]
		if o.Generation != f.Generation || !capacityMasked(o.Available, f.Resources) || o.KnownResources != f.Resources || o.At.IsZero() || o.At.After(now) || now.Sub(o.At) > c.Policy.ObservationMaxAge {
			return ErrUnknown
		}
		seen[o.Constraint] = o
	}
	for i, p := range capacity {
		if p.zero() {
			continue
		}
		f := c.CapacityConstraints[i]
		o, ok := seen[f.ID]
		if !ok || o.KnownResources != f.Resources || o.Generation != f.Generation || o.At.IsZero() || o.At.After(now) || now.Sub(o.At) > c.Policy.ObservationMaxAge {
			return ErrUnknown
		}
		if f.Resources&CapacityBytes != 0 && (o.Available.Bytes < f.Headroom.Bytes || p.Bytes > o.Available.Bytes-f.Headroom.Bytes) {
			return ErrHeadroom
		}
		if f.Resources&CapacityInodes != 0 && (o.Available.Inodes < f.Headroom.Inodes || p.Inodes > o.Available.Inodes-f.Headroom.Inodes) {
			return ErrHeadroom
		}
	}
	return nil
}
func publishCensus(c *checkpoint, f Census) error {
	if !authority(f.Evidence) {
		return ErrAuthority
	}
	nr, nw := 0, 0
	for _, v := range c.Roots {
		if !v.ID.zero() {
			nr++
		}
	}
	for _, v := range c.Workspaces {
		if !v.ID.zero() {
			nw++
		}
	}
	if len(f.Roots) != nr || len(f.Workspaces) != nw {
		return ErrBounds
	}
	seen := map[ID]bool{}
	c.Installation = [Categories]Vector{}
	for _, m := range f.Roots {
		i := rootIndex(c, m.ID)
		if i < 0 || seen[m.ID] || m.Generation != c.Roots[i].Generation {
			return ErrStale
		}
		seen[m.ID] = true
		if _, e := sum(m.Usage); e != nil {
			return e
		}
		c.Roots[i].Usage = m.Usage
		c.Roots[i].Known = true
		for k, v := range m.Usage {
			var e error
			c.Installation[k], e = add(c.Installation[k], v)
			if e != nil {
				return e
			}
		}
	}
	seen = map[ID]bool{}
	for _, m := range f.Workspaces {
		i := workspaceIndex(c, m.ID)
		if i < 0 || seen[m.ID] || m.Generation != c.Workspaces[i].Generation {
			return ErrStale
		}
		seen[m.ID] = true
		if _, e := sum(m.Usage); e != nil {
			return e
		}
		c.Workspaces[i].Usage = m.Usage
		c.Workspaces[i].Known = true
	}
	return nil
}
