package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Copy only identical closed DTO shapes, preserving every field and element.
// It is used between the private pure original-Q copy and original public DTOs.
func storageEnrollmentPeakCopy(dst, src reflect.Value) bool {
	if dst.Kind() != src.Kind() {
		return false
	}
	switch dst.Kind() {
	case reflect.Struct:
		if dst.NumField() != src.NumField() {
			return false
		}
		for i := 0; i < dst.NumField(); i++ {
			if dst.Type().Field(i).Name != src.Type().Field(i).Name || !storageEnrollmentPeakCopy(dst.Field(i), src.Field(i)) {
				return false
			}
		}
	case reflect.Array:
		if dst.Len() != src.Len() {
			return false
		}
		for i := 0; i < dst.Len(); i++ {
			if !storageEnrollmentPeakCopy(dst.Index(i), src.Index(i)) {
				return false
			}
		}
	case reflect.Pointer:
		if src.IsNil() {
			dst.SetZero()
			return true
		}
		dst.Set(reflect.New(dst.Type().Elem()))
		return storageEnrollmentPeakCopy(dst.Elem(), src.Elem())
	case reflect.Slice:
		if src.IsNil() {
			dst.SetZero()
			return true
		}
		dst.Set(reflect.MakeSlice(dst.Type(), src.Len(), src.Len()))
		for i := 0; i < src.Len(); i++ {
			if !storageEnrollmentPeakCopy(dst.Index(i), src.Index(i)) {
				return false
			}
		}
	default:
		if !src.Type().ConvertibleTo(dst.Type()) {
			return false
		}
		dst.Set(src.Convert(dst.Type()))
	}
	return true
}
func storageEnrollmentPeakQTicketValue(t storagebudget.Ticket) storageEnrollmentPeakQTicket {
	var q storageEnrollmentPeakQTicket
	if !storageEnrollmentPeakCopy(reflect.ValueOf(&q).Elem(), reflect.ValueOf(t)) {
		// A failed closed-shape conversion must never become a valid Empty row.
		q = storageEnrollmentPeakQTicket{State: storageEnrollmentPeakQState(255)}
	}
	return q
}
func storageEnrollmentPeakReadTicket(raw []byte) (storagebudget.Ticket, error) {
	var t storagebudget.Ticket
	r := storageEnrollmentPeakReader{raw: raw}
	storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&t.Token).Elem())
	t.State = storagebudget.State(r.u8())
	t.PolicyRevision = r.u64()
	t.Workflow = string(r.data(128))
	t.Operation = string(r.data(128))
	t.Owner = r.digest()
	t.Manifest = r.digest()
	t.ManifestLength = r.u32()
	t.Evidence = r.digest()
	rawCount, nx := r.u8(), int(r.u8())
	nc := int(rawCount & 127)
	if rawCount&128 != 0 {
		t.Fanout = &storagebudget.WorkspaceFanout{}
	}
	if nc > 16 || nx > 16 || t.Fanout != nil && t.State != storagebudget.Terminal {
		return t, storagebudget.ErrBounds
	}
	// Zero wire counts use the same nil slices as the original Q decoder and
	// public ticket clones. Nil and empty encode identically; canonical DTOs
	// keep exact retained-view equality independent of dictionary interning.
	if nc > 0 {
		t.Components = make([]storagebudget.Component, nc)
	}
	if nx > 0 {
		t.Changes = make([]storagebudget.Change, nx)
	}
	for i := range t.Components {
		storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&t.Components[i]).Elem())
	}
	for i := range t.Changes {
		storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&t.Changes[i]).Elem())
	}
	if !r.done() || storageEnrollmentPeakQValidateTicketPage(storageEnrollmentPeakQTicketValue(t)) != nil {
		return storagebudget.Ticket{}, storagebudget.ErrBounds
	}
	return t, nil
}
func storageEnrollmentPeakWriteTicket(t storagebudget.Ticket) ([]byte, error) {
	if storageEnrollmentPeakQValidateTicketPage(storageEnrollmentPeakQTicketValue(t)) != nil || len(t.Components) > 16 || len(t.Changes) > 16 {
		return nil, storagebudget.ErrBounds
	}
	w := storageEnrollmentPeakWriter{max: 8244}
	storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(t.Token))
	w.u8(byte(t.State))
	w.u64(t.PolicyRevision)
	w.data([]byte(t.Workflow), 128)
	w.data([]byte(t.Operation), 128)
	w.put(t.Owner[:])
	w.put(t.Manifest[:])
	w.u32(t.ManifestLength)
	w.put(t.Evidence[:])
	count := byte(len(t.Components))
	if t.Fanout != nil {
		if t.State != storagebudget.Terminal || len(t.Fanout.Workspaces) != 0 || len(t.Fanout.Memberships) != 0 {
			return nil, storagebudget.ErrBounds
		}
		count |= 128
	}
	w.u8(count)
	w.u8(byte(len(t.Changes)))
	for _, v := range t.Components {
		storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(v))
	}
	for _, v := range t.Changes {
		storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(v))
	}
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	if _, err := storageEnrollmentPeakReadTicket(w.raw); err != nil {
		return nil, err
	}
	return w.raw, nil
}
func storageEnrollmentPeakReadTickets(raw []byte, count uint32) ([]storagebudget.Ticket, error) {
	if count > 4224 || len(raw) > 35143680 {
		return nil, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw}
	out := make([]storagebudget.Ticket, 0, count)
	var previous []byte
	for i := uint32(0); i < count; i++ {
		start := r.pos
		id := r.u16()
		n := int(r.u16())
		digest := r.digest()
		if id != uint16(i) || n > 8244 {
			return nil, storagebudget.ErrBounds
		}
		body := r.take(n)
		if r.bad || i > 0 && bytes.Compare(previous, body) >= 0 || storageEnrollmentPeakHash("desk-enrollment-ticket-e1", body) != digest {
			return nil, storagebudget.ErrBounds
		}
		t, err := storageEnrollmentPeakReadTicket(body)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
		previous = body
		padding := (8 - (r.pos-start)%8) % 8
		if !storageEnrollmentPeakZero(r.take(padding)) {
			return nil, storagebudget.ErrBounds
		}
	}
	if !r.done() {
		return nil, storagebudget.ErrBounds
	}
	return out, nil
}

type storageEnrollmentPeakView struct {
	Revision, Epoch uint64
	Maintenance     bool
	Policy          storageOwnerPolicy
	DefinitionRefs  [4][]uint16
	Slots           [64]storagebudget.HostSlotState
	Raw             []byte
	// Present only in the closed DSKEPP3 branch; legacy readers never infer it
	// from current policy. Owned full set values survive dictionary reindexing.
	Effective *storagePolicyEffectiveSet
}

func storagePolicyApplyViewSet(q *storageEnrollmentPeakQCheckpoint, v storageEnrollmentPeakView, c storageOwnerCatalogV2) error {
	if v.Effective == nil {
		return nil
	}
	s := *v.Effective
	if s.Policy != v.Policy || len(s.Workspaces) != len(v.DefinitionRefs[3]) {
		return storagebudget.ErrBounds
	}
	if _, err := storagePolicyEffectiveKey(s, c); err != nil {
		return err
	}
	rows := map[storagebudget.ID]storagebudget.RegisteredWorkspace{}
	for _, row := range s.Workspaces {
		rows[row.ID] = row
	}
	for _, ref := range v.DefinitionRefs[3] {
		if int(ref) >= len(c.Workspaces) {
			return storagebudget.ErrBounds
		}
		d := c.Workspaces[ref].Definition
		row, ok := rows[d.ID]
		if !ok || row.Generation != d.Generation {
			return storagebudget.ErrBounds
		}
	}
	used := 0
	for i, row := range q.Workspaces {
		if row.ID == (storageEnrollmentPeakQID{}) {
			continue
		}
		e, ok := rows[storagebudget.ID(row.ID)]
		if !ok || e.Generation != row.Generation {
			return storagebudget.ErrBounds
		}
		if !storageEnrollmentPeakCopy(reflect.ValueOf(&q.Workspaces[i].Limits).Elem(), reflect.ValueOf(e.Limits)) {
			return storagebudget.ErrBounds
		}
		used++
	}
	if used != len(rows) {
		return storagebudget.ErrBounds
	}
	return nil
}

func storageEnrollmentPeakViewCheckpoint(v storageEnrollmentPeakView, c storageOwnerCatalogV2) (*storageEnrollmentPeakQCheckpoint, error) {
	q := &storageEnrollmentPeakQCheckpoint{Generation: v.Revision, Epoch: v.Epoch, Maintenance: v.Maintenance}
	storageEnrollmentPeakCopy(reflect.ValueOf(&q.Policy.Installation).Elem(), reflect.ValueOf(v.Policy.Installation))
	storageEnrollmentPeakCopy(reflect.ValueOf(&q.Policy.Workspace).Elem(), reflect.ValueOf(v.Policy.Workspace))
	q.Policy.ObservationMaxAge = time.Duration(v.Policy.ObservationMaxAgeNanos)
	for k, list := range v.DefinitionRefs {
		for i, index := range list {
			switch k {
			case 0:
				if int(index) >= len(c.Filesystems) {
					return nil, storagebudget.ErrBounds
				}
				storageEnrollmentPeakCopy(reflect.ValueOf(&q.Filesystems[i]).Elem(), reflect.ValueOf(c.Filesystems[index].Definition))
			case 1:
				if int(index) >= len(c.Constraints) {
					return nil, storagebudget.ErrBounds
				}
				storageEnrollmentPeakCopy(reflect.ValueOf(&q.CapacityConstraints[i]).Elem(), reflect.ValueOf(c.Constraints[index].Definition))
			case 2:
				if int(index) >= len(c.Roots) {
					return nil, storagebudget.ErrBounds
				}
				r := c.Roots[index].Definition
				root := storagebudget.Root{ID: r.ID, Owner: r.Owner, Identity: r.Identity, Filesystem: r.Filesystem, Workspace: r.Workspace, Generation: r.Generation, Locator: r.Locator, CapacityCount: r.CapacityCount, CapacityRefs: r.CapacityRefs, CapacityBinding: r.CapacityBinding}
				storageEnrollmentPeakCopy(reflect.ValueOf(&q.Roots[i]).Elem(), reflect.ValueOf(root))
			case 3:
				if int(index) >= len(c.Workspaces) {
					return nil, storagebudget.ErrBounds
				}
				w := c.Workspaces[index].Definition
				workspace := storagebudget.Workspace{ID: w.ID, Generation: w.Generation, Limits: w.Limits}
				storageEnrollmentPeakCopy(reflect.ValueOf(&q.Workspaces[i]).Elem(), reflect.ValueOf(workspace))
			}
		}
	}
	for i, s := range v.Slots {
		q.Applied[i] = storageEnrollmentPeakQApplied{ID: storageEnrollmentPeakQID(s.PersistedApplied.ID), Sequence: s.PersistedApplied.Sequence, Generation: s.PersistedApplied.Generation, NextSequence: s.PersistedNextSequence}
	}
	if storagePolicyApplyViewSet(q, v, c) != nil || storageEnrollmentPeakQValidateCheckpoint(q) != nil {
		return nil, storagebudget.ErrBounds
	}
	for _, s := range v.Slots {
		t := storageEnrollmentPeakQTicketValue(s.Ticket)
		if t.State != storageEnrollmentPeakQEmpty && storageEnrollmentPeakQRegistered(q, t, false) != nil {
			return nil, storagebudget.ErrBounds
		}
	}
	return q, nil
}
func storageEnrollmentPeakReadViews(raw []byte, count uint32, c storageOwnerCatalogV2, tickets []storagebudget.Ticket) ([]storageEnrollmentPeakView, error) {
	return storageEnrollmentPeakReadViewsVariant(raw, count, c, tickets, nil)
}

func storageEnrollmentPeakReadViewsVariant(raw []byte, count uint32, c storageOwnerCatalogV2, tickets []storagebudget.Ticket, sets []storagePolicyEffectiveSet) ([]storageEnrollmentPeakView, error) {
	if count == 0 || count > 66 || len(raw) > 1081344 {
		return nil, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw}
	out := make([]storageEnrollmentPeakView, 0, count)
	limits := [4]int{128, 512, 4096, 1024}
	for k := uint32(0); k < count; k++ {
		start := r.pos
		v := storageEnrollmentPeakView{Revision: r.u64(), Epoch: r.u64()}
		m := r.u8()
		v.Maintenance = m == 1
		storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&v.Policy).Elem())
		if m > 1 || !v.Policy.valid() {
			return nil, storagebudget.ErrBounds
		}
		for j := range v.DefinitionRefs {
			n := int(r.u16())
			if n > limits[j] {
				return nil, storagebudget.ErrBounds
			}
			v.DefinitionRefs[j] = make([]uint16, n)
			seen := map[uint16]bool{}
			for i := range v.DefinitionRefs[j] {
				id := r.u16()
				if seen[id] {
					return nil, storagebudget.ErrBounds
				}
				seen[id] = true
				v.DefinitionRefs[j][i] = id
			}
		}
		for i := range v.Slots {
			index := int(r.u16())
			if index >= len(tickets) {
				return nil, storagebudget.ErrBounds
			}
			s := storagebudget.HostSlotState{Ticket: tickets[index]}
			storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&s.PersistedApplied).Elem())
			s.PersistedNextSequence = r.u64()
			if s.Ticket.Token.Slot != uint8(i) || s.PersistedApplied.Slot != uint8(i) {
				return nil, storagebudget.ErrBounds
			}
			v.Slots[i] = s
		}
		if sets != nil {
			index := int(r.u16())
			if index >= len(sets) {
				return nil, storagebudget.ErrBounds
			}
			owned := sets[index]
			owned.Workspaces = append([]storagebudget.RegisteredWorkspace{}, owned.Workspaces...)
			v.Effective = &owned
		}
		if r.bad || r.pos-start > 16384 {
			return nil, storagebudget.ErrBounds
		}
		if _, err := storageEnrollmentPeakViewCheckpoint(v, c); err != nil {
			return nil, err
		}
		v.Raw = append([]byte{}, raw[start:r.pos]...)
		out = append(out, v)
	}
	if !r.done() {
		return nil, storagebudget.ErrBounds
	}
	return out, nil
}

type storageEnrollmentPeakCheckpointProjection struct {
	Q                                                       *storageEnrollmentPeakQCheckpoint
	Bank                                                    uint8
	Checkpoints                                             [2]storagebudget.Digest
	Tickets                                                 [64][2]storagebudget.Digest
	TicketBanks                                             [64]uint8
	View                                                    uint16
	Raw, Checkpoint, PeerCheckpoint                         []byte
	RootRefs, WorkspaceRefs, FilesystemRefs, ConstraintRefs []uint16
}

func storageEnrollmentPeakReadCheckpointV1(raw []byte, c storageOwnerCatalogV2, views []storageEnrollmentPeakView, s storageEnrollmentPeakScope) (storageEnrollmentPeakCheckpointProjection, error) {
	var p storageEnrollmentPeakCheckpointProjection
	if len(raw) > 2097152 {
		return p, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw}
	q := &storageEnrollmentPeakQCheckpoint{Generation: r.u64(), Epoch: r.u64()}
	m := r.u8()
	q.Maintenance = m == 1
	policy := storageOwnerPolicy{}
	storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&policy).Elem())
	storageEnrollmentPeakCopy(reflect.ValueOf(&q.Policy.Installation).Elem(), reflect.ValueOf(policy.Installation))
	storageEnrollmentPeakCopy(reflect.ValueOf(&q.Policy.Workspace).Elem(), reflect.ValueOf(policy.Workspace))
	q.Policy.ObservationMaxAge = time.Duration(policy.ObservationMaxAgeNanos)
	storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&q.Installation).Elem())
	if m > 1 || !policy.valid() {
		return p, storagebudget.ErrBounds
	}
	limits := [4]int{4096, 1024, 128, 512}
	for kind, limit := range limits {
		n := int(r.u16())
		if n > limit {
			return p, storagebudget.ErrBounds
		}
		previous := -1
		for i := 0; i < n; i++ {
			physical, index := int(r.u16()), int(r.u16())
			if physical <= previous || physical >= limit {
				return p, storagebudget.ErrBounds
			}
			previous = physical
			switch kind {
			case 0:
				if index >= len(c.Roots) {
					return p, storagebudget.ErrBounds
				}
				d := c.Roots[index].Definition
				v := storagebudget.Root{ID: d.ID, Owner: d.Owner, Identity: d.Identity, Filesystem: d.Filesystem, Workspace: d.Workspace, Generation: d.Generation, Locator: d.Locator, CapacityCount: d.CapacityCount, CapacityRefs: d.CapacityRefs, CapacityBinding: d.CapacityBinding}
				known := r.u8()
				v.Known = known == 1
				storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&v.Usage).Elem())
				if known > 1 {
					return p, storagebudget.ErrBounds
				}
				storageEnrollmentPeakCopy(reflect.ValueOf(&q.Roots[physical]).Elem(), reflect.ValueOf(v))
				p.RootRefs = append(p.RootRefs, uint16(index))
			case 1:
				if index >= len(c.Workspaces) {
					return p, storagebudget.ErrBounds
				}
				d := c.Workspaces[index].Definition
				v := storagebudget.Workspace{ID: d.ID, Generation: d.Generation, Limits: d.Limits}
				known := r.u8()
				v.Known = known == 1
				storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&v.Usage).Elem())
				if known > 1 {
					return p, storagebudget.ErrBounds
				}
				storageEnrollmentPeakCopy(reflect.ValueOf(&q.Workspaces[physical]).Elem(), reflect.ValueOf(v))
				p.WorkspaceRefs = append(p.WorkspaceRefs, uint16(index))
			case 2:
				if index >= len(c.Filesystems) {
					return p, storagebudget.ErrBounds
				}
				storageEnrollmentPeakCopy(reflect.ValueOf(&q.Filesystems[physical]).Elem(), reflect.ValueOf(c.Filesystems[index].Definition))
				p.FilesystemRefs = append(p.FilesystemRefs, uint16(index))
			case 3:
				if index >= len(c.Constraints) {
					return p, storagebudget.ErrBounds
				}
				storageEnrollmentPeakCopy(reflect.ValueOf(&q.CapacityConstraints[physical]).Elem(), reflect.ValueOf(c.Constraints[index].Definition))
				p.ConstraintRefs = append(p.ConstraintRefs, uint16(index))
			}
		}
	}
	for i := range q.Applied {
		q.Applied[i] = storageEnrollmentPeakQApplied{ID: storageEnrollmentPeakQID(r.id()), Sequence: r.u64(), Generation: r.u64(), NextSequence: r.u64()}
	}
	p.Bank = r.u8()
	for i := range p.Checkpoints {
		p.Checkpoints[i] = r.digest()
		if p.Checkpoints[i] == (storagebudget.Digest{}) {
			return p, storagebudget.ErrBounds
		}
	}
	for i := range p.Tickets {
		for j := range p.Tickets[i] {
			p.Tickets[i][j] = r.digest()
			if p.Tickets[i][j] == (storagebudget.Digest{}) {
				return p, storagebudget.ErrBounds
			}
		}
	}
	for i := range p.TicketBanks {
		p.TicketBanks[i] = r.u8()
		if p.TicketBanks[i] > 1 {
			return p, storagebudget.ErrBounds
		}
	}
	p.View = r.u16()
	if !r.done() || p.Bank > 1 || p.Bank != s[325] || int(p.View) >= len(views) {
		return p, storagebudget.ErrBounds
	}
	v := views[p.View]
	if storagePolicyApplyViewSet(q, v, c) != nil || storageEnrollmentPeakQValidateCheckpoint(q) != nil {
		return p, storagebudget.ErrBounds
	}
	if q.Generation != v.Revision || q.Epoch != v.Epoch || q.Maintenance != v.Maintenance || policy != v.Policy || !reflect.DeepEqual(p.FilesystemRefs, v.DefinitionRefs[0]) || !reflect.DeepEqual(p.ConstraintRefs, v.DefinitionRefs[1]) || !reflect.DeepEqual(p.RootRefs, v.DefinitionRefs[2]) || !reflect.DeepEqual(p.WorkspaceRefs, v.DefinitionRefs[3]) {
		return p, storagebudget.ErrBounds
	}
	for i, slot := range v.Slots {
		a := q.Applied[i]
		if a.ID != storageEnrollmentPeakQID(slot.PersistedApplied.ID) || a.Sequence != slot.PersistedApplied.Sequence || a.Generation != slot.PersistedApplied.Generation || a.NextSequence != slot.PersistedNextSequence {
			return p, storagebudget.ErrBounds
		}
		qt := storageEnrollmentPeakQTicketValue(slot.Ticket)
		if qt.State != storageEnrollmentPeakQEmpty && (!storageEnrollmentPeakQRetired(qt, a) || slot.Ticket.Token != slot.PersistedApplied || slot.PersistedNextSequence <= slot.Ticket.Token.Sequence) {
			return p, storagebudget.ErrBounds
		}
		encoded, err := storageEnrollmentPeakQEncodeTicket(qt, storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(c.Installation), Binding: storageEnrollmentPeakQDigest(c.QBinding)})
		if err != nil || sha256.Sum256(encoded) != p.Tickets[i][p.TicketBanks[i]] {
			return p, storagebudget.ErrBounds
		}
	}
	cp, err := storageEnrollmentPeakQEncodeCheckpoint(q, storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(c.Installation), Binding: storageEnrollmentPeakQDigest(c.QBinding)})
	if err != nil || sha256.Sum256(cp) != p.Checkpoints[p.Bank] || !bytes.Equal(p.Checkpoints[p.Bank][:], s[120:152]) {
		return p, storagebudget.ErrBounds
	}
	p.Q = q
	p.Checkpoint = cp
	p.Raw = append([]byte{}, raw...)
	return p, nil
}

func storageEnrollmentPeakReadCensus(raw []byte, q *storageEnrollmentPeakQCheckpoint, phase byte) (storageEnrollmentPeakQCensus, error) {
	var f storageEnrollmentPeakQCensus
	if len(raw) > 2097152 || phase > 2 {
		return f, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw}
	f.Evidence = storageEnrollmentPeakQDigest(r.digest())
	n := int(r.u16())
	if n > 4096 {
		return f, storagebudget.ErrBounds
	}
	f.Roots = make([]storageEnrollmentPeakQRootMeasurement, n)
	for i := range f.Roots {
		storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&f.Roots[i]).Elem())
	}
	n = int(r.u16())
	if n > 1024 {
		return f, storagebudget.ErrBounds
	}
	f.Workspaces = make([]storageEnrollmentPeakQWorkspaceMeasurement, n)
	for i := range f.Workspaces {
		storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&f.Workspaces[i]).Elem())
	}
	if !r.done() {
		return f, storagebudget.ErrBounds
	}
	ri, wi := 0, 0
	for _, v := range q.Roots {
		if v.ID == (storageEnrollmentPeakQID{}) {
			continue
		}
		if ri >= len(f.Roots) || f.Roots[ri].ID != v.ID || f.Roots[ri].Generation != v.Generation {
			return f, storagebudget.ErrBounds
		}
		if phase == 0 && f.Roots[ri].Usage != ([9]storageEnrollmentPeakQVector{}) {
			return f, storagebudget.ErrBounds
		}
		ri++
	}
	for _, v := range q.Workspaces {
		if v.ID == (storageEnrollmentPeakQID{}) {
			continue
		}
		if wi >= len(f.Workspaces) || f.Workspaces[wi].ID != v.ID || f.Workspaces[wi].Generation != v.Generation {
			return f, storagebudget.ErrBounds
		}
		if phase == 0 && f.Workspaces[wi].Usage != ([9]storageEnrollmentPeakQVector{}) {
			return f, storagebudget.ErrBounds
		}
		wi++
	}
	if ri != len(f.Roots) || wi != len(f.Workspaces) || phase == 0 && f.Evidence != (storageEnrollmentPeakQDigest{}) || phase != 0 && f.Evidence == (storageEnrollmentPeakQDigest{}) {
		return f, storagebudget.ErrBounds
	}
	return f, nil
}

// R is derived from exact dictionaries; incoming data is never normalized to
// repair malformed values. Its four arrays always exist, including empties.
func storageEnrollmentPeakRegistration(c storageOwnerCatalogV2, refs [4][]uint16, q0 *storageEnrollmentPeakQCheckpoint) (storagebudget.Registration, error) {
	r := storagebudget.Registration{Roots: []storagebudget.Root{}, Workspaces: []storagebudget.Workspace{}, Filesystems: []storagebudget.Filesystem{}, CapacityConstraints: []storagebudget.CapacityConstraint{}}
	for _, i := range refs[0] {
		if int(i) >= len(c.Roots) {
			return r, storagebudget.ErrBounds
		}
		d := c.Roots[i].Definition
		r.Roots = append(r.Roots, storagebudget.Root{ID: d.ID, Owner: d.Owner, Identity: d.Identity, Filesystem: d.Filesystem, Workspace: d.Workspace, Generation: d.Generation, Locator: d.Locator, Known: false, CapacityCount: d.CapacityCount, CapacityRefs: d.CapacityRefs, CapacityBinding: d.CapacityBinding})
	}
	for _, i := range refs[1] {
		if int(i) >= len(c.Workspaces) {
			return r, storagebudget.ErrBounds
		}
		d := c.Workspaces[i].Definition
		limits := d.Limits
		if limits == (storagebudget.Limits{}) {
			storageEnrollmentPeakCopy(reflect.ValueOf(&limits).Elem(), reflect.ValueOf(q0.Policy.Workspace))
		}
		r.Workspaces = append(r.Workspaces, storagebudget.Workspace{ID: d.ID, Generation: d.Generation, Known: false, Limits: limits})
	}
	for _, i := range refs[2] {
		if int(i) >= len(c.Filesystems) {
			return r, storagebudget.ErrBounds
		}
		r.Filesystems = append(r.Filesystems, c.Filesystems[i].Definition)
	}
	for _, i := range refs[3] {
		if int(i) >= len(c.Constraints) {
			return r, storagebudget.ErrBounds
		}
		r.CapacityConstraints = append(r.CapacityConstraints, c.Constraints[i].Definition)
	}
	return r, nil
}
func storageEnrollmentPeakRegistrationDigest(r storagebudget.Registration) (storagebudget.Digest, error) {
	if r.Roots == nil || r.Workspaces == nil || r.Filesystems == nil || r.CapacityConstraints == nil {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return storagebudget.Digest{}, err
	}
	return sha256.Sum256(raw), nil
}

func storageEnrollmentPeakPredict(q0 *storageEnrollmentPeakQCheckpoint, r storagebudget.Registration) ([3]*storageEnrollmentPeakQCheckpoint, error) {
	var out [3]*storageEnrollmentPeakQCheckpoint
	if q0 == nil || q0.Maintenance || q0.Generation > ^uint64(0)-3 || q0.Epoch == ^uint64(0) {
		return out, storagebudget.ErrBounds
	}
	a := *q0
	a.Generation++
	a.Epoch++
	a.Maintenance = true
	out[0] = &a
	b := a
	b.Generation++
	insert := func(array reflect.Value, src reflect.Value) bool {
		for j := 0; j < src.Len(); j++ {
			value := src.Index(j)
			id := value.FieldByName("ID")
			for i := 0; i < array.Len(); i++ {
				if reflect.DeepEqual(array.Index(i).FieldByName("ID").Interface(), id.Convert(array.Index(i).FieldByName("ID").Type()).Interface()) {
					return false
				}
			}
			placed := false
			for i := 0; i < array.Len(); i++ {
				if array.Index(i).FieldByName("ID").IsZero() {
					if !storageEnrollmentPeakCopy(array.Index(i), value) {
						return false
					}
					placed = true
					break
				}
			}
			if !placed {
				return false
			}
		}
		return true
	}
	if !insert(reflect.ValueOf(&b.Workspaces).Elem(), reflect.ValueOf(r.Workspaces)) || !insert(reflect.ValueOf(&b.CapacityConstraints).Elem(), reflect.ValueOf(r.CapacityConstraints)) || !insert(reflect.ValueOf(&b.Filesystems).Elem(), reflect.ValueOf(r.Filesystems)) || !insert(reflect.ValueOf(&b.Roots).Elem(), reflect.ValueOf(r.Roots)) || storageEnrollmentPeakQValidateCheckpoint(&b) != nil {
		return out, storagebudget.ErrBounds
	}
	out[1] = &b
	return out, nil
}

// Exact Q0 occupancy is the complete before definition set; physical order is
// preserved separately in the checkpoint/view, never inferred from sorted IDs.
func storageEnrollmentPeakBeforeDefinitions(c storageOwnerCatalogV2, p storageEnrollmentPeakCheckpointProjection) bool {
	refs := [][]uint16{p.RootRefs, p.WorkspaceRefs, p.FilesystemRefs, p.ConstraintRefs}
	ids := [][]storagebudget.ID{c.BeforeDefinitionIDs.Roots, c.BeforeDefinitionIDs.Workspaces, c.BeforeDefinitionIDs.Filesystems, c.BeforeDefinitionIDs.Constraints}
	for kind, list := range refs {
		if len(list) != len(ids[kind]) {
			return false
		}
		seen := map[storagebudget.ID]bool{}
		for _, index := range list {
			var id storagebudget.ID
			switch kind {
			case 0:
				id = c.Roots[index].Definition.ID
			case 1:
				id = c.Workspaces[index].Definition.ID
			case 2:
				id = c.Filesystems[index].Definition.ID
			case 3:
				id = c.Constraints[index].Definition.ID
			}
			if seen[id] {
				return false
			}
			seen[id] = true
		}
		for _, id := range ids[kind] {
			if !seen[id] {
				return false
			}
		}
	}
	return true
}
