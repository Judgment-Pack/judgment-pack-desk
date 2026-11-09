package desk

import (
	"bytes"
	"crypto/sha256"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageEnrollmentPeakQCloneFanout(f *storageEnrollmentPeakQWorkspaceFanout) *storageEnrollmentPeakQWorkspaceFanout {
	if f == nil {
		return nil
	}
	return &storageEnrollmentPeakQWorkspaceFanout{Workspaces: append([]storageEnrollmentPeakQWorkspaceReference(nil), f.Workspaces...), Memberships: append([]storageEnrollmentPeakQWorkspaceMembership(nil), f.Memberships...)}
}

func storageEnrollmentPeakQSameFanout(a, b *storageEnrollmentPeakQWorkspaceFanout) bool {
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

func storageEnrollmentPeakQCloneTicket(t storageEnrollmentPeakQTicket) storageEnrollmentPeakQTicket {
	t.Components = append([]storageEnrollmentPeakQComponent(nil), t.Components...)
	t.Changes = append([]storageEnrollmentPeakQChange(nil), t.Changes...)
	t.Fanout = storageEnrollmentPeakQCloneFanout(t.Fanout)
	return t
}

func storageEnrollmentPeakQMember(m storageEnrollmentPeakQWorkspaceMembership, i int) bool {
	return m[i/8]&(1<<uint(i%8)) != 0
}

// Only a complete immutable manifest can supply membership. Ticket-page parsing
// deliberately cannot call this: retired manifests may already be overwritten.
func storageEnrollmentPeakQValidateFanout(t storageEnrollmentPeakQTicket) error {
	f := t.Fanout
	if f == nil {
		return nil
	}
	if len(f.Workspaces) > storageEnrollmentPeakQMaxWorkspaces || len(f.Memberships) != len(t.Components) {
		return storagebudget.ErrBounds
	}
	for i, w := range f.Workspaces {
		if w.ID.zero() || w.Generation == 0 || i > 0 && bytes.Compare(f.Workspaces[i-1].ID[:], w.ID[:]) >= 0 {
			return storagebudget.ErrAuthority
		}
	}
	var used storageEnrollmentPeakQWorkspaceMembership
	for g, m := range f.Memberships {
		v := t.Components[g]
		if !v.Workspace.zero() || v.WorkspaceGeneration != 0 {
			return storagebudget.ErrBounds
		}
		selected := false
		for i := 0; i < storageEnrollmentPeakQMaxWorkspaces; i++ {
			if !storageEnrollmentPeakQMember(m, i) {
				continue
			}
			if i >= len(f.Workspaces) {
				return storagebudget.ErrBounds
			}
			selected = true
			used[i/8] |= 1 << uint(i%8)
		}
		_, attribution, err := v.total()
		if err != nil {
			return err
		}
		if selected == attribution.zero() {
			return storagebudget.ErrBounds
		}
		if t.State == storageEnrollmentPeakQTerminal && !selected && (!t.Changes[g].AttributionAdd.zero() || !t.Changes[g].AttributionRemove.zero()) {
			return storagebudget.ErrBounds
		}
	}
	for i := range f.Workspaces {
		if !storageEnrollmentPeakQMember(used, i) {
			return storagebudget.ErrBounds
		}
	}
	// Exact expanded keys must remain unique even when groups share a root.
	type key struct {
		root, workspace storageEnrollmentPeakQID
		category        uint8
	}
	seen := make(map[key]bool)
	return storageEnrollmentPeakQWalkComponents(t, func(_ int, c storageEnrollmentPeakQComponent, _ bool) error {
		k := key{c.Root, c.Workspace, c.Category}
		if seen[k] {
			return storagebudget.ErrBounds
		}
		seen[k] = true
		return nil
	})
}

// Walk without materializing up to16*1024 flat components. The first member
// carries physical/capacity quantities; all other members retain exact custody
// and capacity binding with zero physical promises. A no-member group is one
// ordinary unattributed physical component. Callers validate membership first.
func storageEnrollmentPeakQWalkComponents(t storageEnrollmentPeakQTicket, visit func(int, storageEnrollmentPeakQComponent, bool) error) error {
	if t.Fanout == nil {
		for i, c := range t.Components {
			if err := visit(i, c, true); err != nil {
				return err
			}
		}
		return nil
	}
	if len(t.Fanout.Memberships) != len(t.Components) {
		return storagebudget.ErrUnknown
	}
	for g, c := range t.Components {
		first := true
		for i, w := range t.Fanout.Workspaces {
			if !storageEnrollmentPeakQMember(t.Fanout.Memberships[g], i) {
				continue
			}
			v := c
			v.Workspace, v.WorkspaceGeneration = w.ID, w.Generation
			if !first {
				v.Physical, v.SettlementPhysical = storageEnrollmentPeakQVector{}, storageEnrollmentPeakQVector{}
				for j := range v.Capacity {
					v.Capacity[j].Peak = storageEnrollmentPeakQCapacityVector{}
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

// Pure canonical manifest reconstruction for an exact retained fanout request.
// It opens no retired manifest and issues no admission or native evidence.
func storageEnrollmentPeakQEncodeFanoutManifest(t storageEnrollmentPeakQTicket, a storageEnrollmentPeakQHeldAnchor) ([]byte, error) {
	if t.Fanout == nil {
		return nil, storagebudget.ErrBounds
	}
	t.Manifest, t.Evidence = storageEnrollmentPeakQDigest{}, storageEnrollmentPeakQDigest{}
	t.ManifestLength, t.State, t.Changes = 0, storageEnrollmentPeakQReserved, nil
	if err := storageEnrollmentPeakQValidateTicket(t); err != nil {
		return nil, err
	}
	initial, err := storageEnrollmentPeakQEncodeTicket(t, a)
	if err != nil {
		return nil, err
	}
	b := make([]byte, storageEnrollmentPeakQManifestBytes)
	w := storageEnrollmentPeakQWriter{b: b[storageEnrollmentPeakQPageHeader:]}
	w.put(initial)
	w.u16(uint16(len(t.Fanout.Workspaces)))
	w.u8(uint8(len(t.Components)))
	w.u8(1)
	w.u16(0)
	w.u16(storageEnrollmentPeakQMaxWorkspaces / 8)
	w.u64(0)
	for _, ref := range t.Fanout.Workspaces {
		w.id(ref.ID)
		w.u64(ref.Generation)
	}
	for i := 0; i < storageEnrollmentPeakQMaxComponents; i++ {
		var m storageEnrollmentPeakQWorkspaceMembership
		if i < len(t.Fanout.Memberships) {
			m = t.Fanout.Memberships[i]
		}
		w.put(m[:])
	}
	if w.err != nil {
		return nil, w.err
	}
	b[9] = 1
	storageEnrollmentPeakQSeal(b, 3, uint16(t.Token.Slot), t.Token.Sequence, a)
	return b, nil
}

// A page marker is insufficient for live accounting: the complete immutable
// dictionary must validate against this exact ticket before it can be used.
func storageEnrollmentPeakQDecodeFanoutManifest(b []byte, t storageEnrollmentPeakQTicket, a storageEnrollmentPeakQHeldAnchor) (storageEnrollmentPeakQTicket, error) {
	if len(b) != storageEnrollmentPeakQManifestBytes || b[9] != 1 || t.Fanout == nil || t.ManifestLength != storageEnrollmentPeakQManifestBytes || sha256.Sum256(b) != t.Manifest {
		return storageEnrollmentPeakQTicket{}, storagebudget.ErrUnknown
	}
	g, err := storageEnrollmentPeakQUnsealVariant(b, 3, uint16(t.Token.Slot), a, true)
	if err != nil || g != t.Token.Sequence {
		return storageEnrollmentPeakQTicket{}, storagebudget.ErrUnknown
	}
	initial, err := storageEnrollmentPeakQDecodeTicket(b[storageEnrollmentPeakQPageHeader:storageEnrollmentPeakQPageHeader+storageEnrollmentPeakQTicketBytes], int(t.Token.Slot), a)
	if err != nil || initial.Fanout == nil || initial.Token.ID != t.Token.ID || initial.Token.Sequence != t.Token.Sequence || initial.Owner != t.Owner || initial.Workflow != t.Workflow || initial.Operation != t.Operation || initial.PolicyRevision != t.PolicyRevision || initial.Token.Generation > t.Token.Generation || initial.Manifest != (storageEnrollmentPeakQDigest{}) || initial.ManifestLength != 0 || initial.State != storageEnrollmentPeakQReserved {
		return storageEnrollmentPeakQTicket{}, storagebudget.ErrUnknown
	}
	if len(initial.Components) != len(t.Components) {
		return storageEnrollmentPeakQTicket{}, storagebudget.ErrUnknown
	}
	for i, component := range initial.Components {
		if component != t.Components[i] {
			return storageEnrollmentPeakQTicket{}, storagebudget.ErrUnknown
		}
	}
	r := storageEnrollmentPeakQReader{b: b[storageEnrollmentPeakQPageHeader+storageEnrollmentPeakQTicketBytes:]}
	n, groups, version := int(r.u16()), int(r.u8()), r.u8()
	entries, width, reserved := r.u16(), r.u16(), r.u64()
	if n > storageEnrollmentPeakQMaxWorkspaces || groups != len(initial.Components) || version != 1 || entries != 0 || width != storageEnrollmentPeakQMaxWorkspaces/8 || reserved != 0 {
		return storageEnrollmentPeakQTicket{}, storagebudget.ErrUnknown
	}
	f := &storageEnrollmentPeakQWorkspaceFanout{Workspaces: make([]storageEnrollmentPeakQWorkspaceReference, n), Memberships: make([]storageEnrollmentPeakQWorkspaceMembership, groups)}
	for i := range f.Workspaces {
		f.Workspaces[i] = storageEnrollmentPeakQWorkspaceReference{r.id(), r.u64()}
	}
	for i := 0; i < storageEnrollmentPeakQMaxComponents; i++ {
		m := r.take(storageEnrollmentPeakQMaxWorkspaces / 8)
		if i < groups {
			copy(f.Memberships[i][:], m)
		} else if !storageEnrollmentPeakQAllZero(m) {
			return storageEnrollmentPeakQTicket{}, storagebudget.ErrUnknown
		}
	}
	initial.Fanout = f
	if r.end() != nil || storageEnrollmentPeakQValidateTicket(initial) != nil || len(t.Fanout.Memberships) != 0 && !storageEnrollmentPeakQSameFanout(initial.Fanout, t.Fanout) {
		return storageEnrollmentPeakQTicket{}, storagebudget.ErrUnknown
	}
	return initial, nil
}
