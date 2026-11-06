package storagebudget

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"time"
	"unicode/utf8"
)

type applied struct {
	ID                                 ID
	Sequence, Generation, NextSequence uint64
}
type checkpoint struct {
	Generation          uint64
	Epoch               uint64
	Maintenance         bool
	Policy              Policy
	Installation        [Categories]Vector
	Roots               [MaxRoots]Root
	Workspaces          [MaxWorkspaces]Workspace
	Filesystems         [MaxFilesystems]Filesystem
	CapacityConstraints [MaxCapacityConstraints]CapacityConstraint
	Applied             [Slots]applied
}
type writer struct {
	b   []byte
	n   int
	err error
}

func (w *writer) put(b []byte) {
	if w.err != nil {
		return
	}
	if len(b) > len(w.b)-w.n {
		w.err = ErrBounds
		return
	}
	copy(w.b[w.n:], b)
	w.n += len(b)
}
func (w *writer) u8(v uint8)      { w.put([]byte{v}) }
func (w *writer) u16(v uint16)    { var b [2]byte; binary.LittleEndian.PutUint16(b[:], v); w.put(b[:]) }
func (w *writer) u32(v uint32)    { var b [4]byte; binary.LittleEndian.PutUint32(b[:], v); w.put(b[:]) }
func (w *writer) u64(v uint64)    { var b [8]byte; binary.LittleEndian.PutUint64(b[:], v); w.put(b[:]) }
func (w *writer) id(v ID)         { w.put(v[:]) }
func (w *writer) digest(v Digest) { w.put(v[:]) }
func (w *writer) vector(v Vector) { w.u64(v.Bytes); w.u64(v.Files); w.u64(v.Directories) }
func (w *writer) usage(v [Categories]Vector) {
	for _, a := range v {
		w.vector(a)
	}
}
func (w *writer) limits(v Limits) { w.vector(v.Total); w.usage(v.Category) }
func (w *writer) text(s string, max int) {
	if !utf8.ValidString(s) || len(s) > max {
		w.err = ErrBounds
		return
	}
	w.u16(uint16(len(s)))
	p := make([]byte, max)
	copy(p, s)
	w.put(p)
}
func (w *writer) variable(s string, max int) {
	if !utf8.ValidString(s) || len(s) > max {
		w.err = ErrBounds
		return
	}
	w.u16(uint16(len(s)))
	w.put([]byte(s))
}

type reader struct {
	b   []byte
	n   int
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return make([]byte, n)
	}
	if n < 0 || n > len(r.b)-r.n {
		r.err = ErrUnknown
		return make([]byte, n)
	}
	v := r.b[r.n : r.n+n]
	r.n += n
	return v
}
func (r *reader) u8() uint8          { return r.take(1)[0] }
func (r *reader) u16() uint16        { return binary.LittleEndian.Uint16(r.take(2)) }
func (r *reader) u32() uint32        { return binary.LittleEndian.Uint32(r.take(4)) }
func (r *reader) u64() uint64        { return binary.LittleEndian.Uint64(r.take(8)) }
func (r *reader) id() (v ID)         { copy(v[:], r.take(16)); return }
func (r *reader) digest() (v Digest) { copy(v[:], r.take(32)); return }
func (r *reader) vector() Vector     { return Vector{r.u64(), r.u64(), r.u64()} }
func (r *reader) usage() (v [Categories]Vector) {
	for i := range v {
		v[i] = r.vector()
	}
	return
}
func (r *reader) limits() Limits { return Limits{r.vector(), r.usage()} }
func (r *reader) text(max int) string {
	n := int(r.u16())
	p := r.take(max)
	if n > max {
		r.err = ErrUnknown
		return ""
	}
	if !allZero(p[n:]) || !utf8.Valid(p[:n]) {
		r.err = ErrUnknown
	}
	return string(p[:n])
}
func (r *reader) variable(max int) string {
	n := int(r.u16())
	if n > max {
		r.err = ErrUnknown
		return ""
	}
	p := r.take(n)
	if !utf8.Valid(p) {
		r.err = ErrUnknown
	}
	return string(p)
}
func (r *reader) end() error {
	if !allZero(r.b[r.n:]) {
		r.err = ErrUnknown
	}
	return r.err
}
func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

func seal(b []byte, kind byte, index uint16, generation uint64, a HeldAnchor) {
	copy(b[:8], "JPSTB002")
	b[8] = kind
	binary.LittleEndian.PutUint16(b[10:12], index)
	binary.LittleEndian.PutUint64(b[16:24], generation)
	copy(b[24:40], a.Installation[:])
	copy(b[40:72], a.Binding[:])
	clear(b[72:104])
	d := sha256.Sum256(b)
	copy(b[72:104], d[:])
}
func unseal(b []byte, kind byte, index uint16, a HeldAnchor) (uint64, error) {
	return unsealVariant(b, kind, index, a, false)
}

// Only ticket/manifest parsers opt into the exact-fanout variant. Checkpoints
// and old flat bytes keep their original header and strict reserved fields.
func unsealVariant(b []byte, kind byte, index uint16, a HeldAnchor, fanout bool) (uint64, error) {
	if len(b) < pageHeader || string(b[:8]) != "JPSTB002" || b[8] != kind || b[9] != 0 && (!fanout || b[9] != 1) || binary.LittleEndian.Uint16(b[10:12]) != index || !allZero(b[12:16]) || !allZero(b[104:128]) || !bytes.Equal(b[24:40], a.Installation[:]) || !bytes.Equal(b[40:72], a.Binding[:]) {
		return 0, ErrUnknown
	}
	var old [32]byte
	copy(old[:], b[72:104])
	clear(b[72:104])
	d := sha256.Sum256(b)
	copy(b[72:104], old[:])
	if d != old {
		return 0, ErrUnknown
	}
	return binary.LittleEndian.Uint64(b[16:24]), nil
}
func encodeCheckpoint(c *checkpoint, a HeldAnchor) ([]byte, error) {
	if c == nil || validateCheckpoint(c) != nil {
		return nil, ErrBounds
	}
	b := make([]byte, CheckpointBytes)
	w := writer{b: b[pageHeader : 16<<10]}
	w.u64(c.Epoch)
	if c.Maintenance {
		w.u8(1)
	} else {
		w.u8(0)
	}
	w.usage(c.Installation)
	p := writer{b: b[16<<10 : rootOffset]}
	p.limits(c.Policy.Installation)
	p.limits(c.Policy.Workspace)
	p.u64(uint64(c.Policy.ObservationMaxAge))
	if w.err != nil || p.err != nil {
		return nil, ErrBounds
	}
	for i, v := range c.Roots {
		if v.ID.zero() {
			continue
		}
		w = writer{b: b[rootOffset+i*768 : rootOffset+(i+1)*768]}
		w.id(v.ID)
		w.digest(v.Owner)
		w.digest(v.Identity)
		w.id(v.Filesystem)
		w.id(v.Workspace)
		w.u64(v.Generation)
		if v.Known {
			w.u8(1)
		} else {
			w.u8(0)
		}
		w.text(v.Locator, 256)
		w.usage(v.Usage)
		w.u8(v.CapacityCount)
		for _, ref := range v.CapacityRefs {
			w.id(ref.Constraint)
			w.u64(ref.Generation)
		}
		w.digest(v.CapacityBinding)
		if w.err != nil {
			return nil, w.err
		}
	}
	for i, v := range c.Workspaces {
		if v.ID.zero() {
			continue
		}
		w = writer{b: b[workspaceOffset+i*512 : workspaceOffset+(i+1)*512]}
		w.id(v.ID)
		w.u64(v.Generation)
		if v.Known {
			w.u8(1)
		} else {
			w.u8(0)
		}
		w.usage(v.Usage)
		w.limits(v.Limits)
		if w.err != nil {
			return nil, w.err
		}
	}
	for i, v := range c.Filesystems {
		if v.ID.zero() {
			continue
		}
		w = writer{b: b[filesystemOffset+i*256 : filesystemOffset+(i+1)*256]}
		w.id(v.ID)
		w.digest(v.Identity)
		w.u64(v.Generation)
		w.u8(v.InodeModel)
		w.digest(v.CapabilityEvidence)
		w.id(v.ByteConstraint)
		w.id(v.InodeConstraint)
		if w.err != nil {
			return nil, w.err
		}
	}
	for i, v := range c.Applied {
		w = writer{b: b[appliedOffset+i*128 : appliedOffset+(i+1)*128]}
		w.id(v.ID)
		w.u64(v.Sequence)
		w.u64(v.Generation)
		w.u64(v.NextSequence)
	}
	for i, v := range c.CapacityConstraints {
		if v.ID.zero() {
			continue
		}
		w = writer{b: b[capacityOffset+i*128 : capacityOffset+(i+1)*128]}
		w.id(v.ID)
		w.digest(v.Identity)
		w.u64(v.Generation)
		w.u8(v.Resources)
		w.u64(v.Headroom.Bytes)
		w.u64(v.Headroom.Inodes)
		if w.err != nil {
			return nil, w.err
		}
	}
	seal(b, 1, 0, c.Generation, a)
	return b, nil
}
func decodeCheckpoint(b []byte, a HeldAnchor) (*checkpoint, error) {
	if len(b) != CheckpointBytes {
		return nil, ErrUnknown
	}
	g, e := unseal(b, 1, 0, a)
	if e != nil {
		return nil, e
	}
	c := new(checkpoint)
	c.Generation = g
	r := reader{b: b[pageHeader : 16<<10]}
	c.Epoch = r.u64()
	m := r.u8()
	c.Maintenance = m == 1
	c.Installation = r.usage()
	if m > 1 || r.end() != nil {
		return nil, ErrUnknown
	}
	r = reader{b: b[16<<10 : rootOffset]}
	c.Policy.Installation = r.limits()
	c.Policy.Workspace = r.limits()
	age := r.u64()
	if age > uint64(1<<63-1) {
		return nil, ErrUnknown
	}
	c.Policy.ObservationMaxAge = time.Duration(age)
	if r.end() != nil || !c.Policy.valid() {
		return nil, ErrUnknown
	}
	for i := range c.Roots {
		row := b[rootOffset+i*768 : rootOffset+(i+1)*768]
		if allZero(row) {
			continue
		}
		r = reader{b: row}
		v := Root{ID: r.id(), Owner: r.digest(), Identity: r.digest(), Filesystem: r.id(), Workspace: r.id(), Generation: r.u64()}
		m = r.u8()
		v.Known = m == 1
		v.Locator = r.text(256)
		v.Usage = r.usage()
		v.CapacityCount = r.u8()
		for j := range v.CapacityRefs {
			v.CapacityRefs[j] = CapacityRef{r.id(), r.u64()}
		}
		v.CapacityBinding = r.digest()
		if m > 1 || r.end() != nil {
			return nil, ErrUnknown
		}
		c.Roots[i] = v
	}
	for i := range c.Workspaces {
		row := b[workspaceOffset+i*512 : workspaceOffset+(i+1)*512]
		if allZero(row) {
			continue
		}
		r = reader{b: row}
		v := Workspace{ID: r.id(), Generation: r.u64()}
		m = r.u8()
		v.Known = m == 1
		v.Usage = r.usage()
		v.Limits = r.limits()
		if m > 1 || r.end() != nil {
			return nil, ErrUnknown
		}
		c.Workspaces[i] = v
	}
	for i := range c.Filesystems {
		row := b[filesystemOffset+i*256 : filesystemOffset+(i+1)*256]
		if allZero(row) {
			continue
		}
		r = reader{b: row}
		v := Filesystem{ID: r.id(), Identity: r.digest(), Generation: r.u64(), InodeModel: r.u8(), CapabilityEvidence: r.digest(), ByteConstraint: r.id(), InodeConstraint: r.id()}
		if r.end() != nil {
			return nil, ErrUnknown
		}
		c.Filesystems[i] = v
	}
	for i := range c.Applied {
		r = reader{b: b[appliedOffset+i*128 : appliedOffset+(i+1)*128]}
		c.Applied[i] = applied{ID: r.id(), Sequence: r.u64(), Generation: r.u64(), NextSequence: r.u64()}
		if r.end() != nil {
			return nil, ErrUnknown
		}
	}
	for i := range c.CapacityConstraints {
		row := b[capacityOffset+i*128 : capacityOffset+(i+1)*128]
		if allZero(row) {
			continue
		}
		r = reader{b: row}
		c.CapacityConstraints[i] = CapacityConstraint{ID: r.id(), Identity: r.digest(), Generation: r.u64(), Resources: r.u8(), Headroom: CapacityVector{r.u64(), r.u64()}}
		if r.end() != nil {
			return nil, ErrUnknown
		}
	}
	if !allZero(b[capacityOffset+MaxCapacityConstraints*128:]) || validateCheckpoint(c) != nil {
		return nil, ErrUnknown
	}
	return c, nil
}

func encodeTicket(t Ticket, a HeldAnchor) ([]byte, error) {
	if len(t.Components) > MaxComponents || len(t.Changes) > MaxComponents {
		return nil, ErrBounds
	}
	if e := validateTicket(t); e != nil {
		return nil, e
	}
	b := make([]byte, TicketBytes)
	w := writer{b: b[pageHeader:1024]}
	w.id(t.Token.ID)
	w.u64(t.Token.Sequence)
	w.u8(uint8(t.State))
	w.u64(t.PolicyRevision)
	w.text(t.Workflow, 128)
	w.text(t.Operation, 128)
	w.digest(t.Owner)
	w.digest(t.Manifest)
	w.u32(t.ManifestLength)
	w.digest(t.Evidence)
	w.u8(uint8(len(t.Components)))
	w.u8(uint8(len(t.Changes)))
	if w.err != nil {
		return nil, w.err
	}
	for i, c := range t.Components {
		w = writer{b: b[1024+i*512 : 1024+(i+1)*512]}
		w.id(c.Root)
		w.u64(c.RootGeneration)
		w.digest(c.Owner)
		w.id(c.Filesystem)
		w.u64(c.FilesystemGeneration)
		w.id(c.Workspace)
		w.u64(c.WorkspaceGeneration)
		w.u8(c.Category)
		w.vector(c.Physical)
		w.vector(c.Attribution)
		w.vector(c.SettlementPhysical)
		w.vector(c.SettlementAttribution)
		if i < len(t.Changes) {
			x := t.Changes[i]
			w.vector(x.PhysicalAdd)
			w.vector(x.PhysicalRemove)
			w.vector(x.AttributionAdd)
			w.vector(x.AttributionRemove)
		} else {
			w.put(make([]byte, 96))
		}
		// Change bytes are reserved even on nonterminal pages.
		w.u8(c.CapacityCount)
		for _, ref := range c.Capacity {
			w.id(ref.Constraint)
			w.u64(ref.Generation)
			w.u64(ref.Peak.Bytes)
			w.u64(ref.Peak.Inodes)
		}
		w.digest(c.CapacityBinding)
		if w.err != nil {
			return nil, w.err
		}
	}
	if t.Fanout != nil {
		b[9] = 1
	}
	seal(b, 2, uint16(t.Token.Slot), t.Token.Generation, a)
	return b, nil
}
func decodeTicket(b []byte, slot int, a HeldAnchor) (Ticket, error) {
	var t Ticket
	if len(b) != TicketBytes {
		return t, ErrUnknown
	}
	g, e := unsealVariant(b, 2, uint16(slot), a, true)
	if e != nil {
		return t, e
	}
	r := reader{b: b[pageHeader:1024]}
	if b[9] == 1 {
		t.Fanout = &WorkspaceFanout{}
	}
	t.Token = Token{Slot: uint8(slot), ID: r.id(), Sequence: r.u64(), Generation: g}
	t.State = State(r.u8())
	t.PolicyRevision = r.u64()
	t.Workflow = r.text(128)
	t.Operation = r.text(128)
	t.Owner = r.digest()
	t.Manifest = r.digest()
	t.ManifestLength = r.u32()
	t.Evidence = r.digest()
	n, k := int(r.u8()), int(r.u8())
	if n > MaxComponents || k > n || r.end() != nil {
		return t, ErrUnknown
	}
	for i := 0; i < n; i++ {
		r = reader{b: b[1024+i*512 : 1024+(i+1)*512]}
		c := Component{Root: r.id(), RootGeneration: r.u64(), Owner: r.digest(), Filesystem: r.id(), FilesystemGeneration: r.u64(), Workspace: r.id(), WorkspaceGeneration: r.u64(), Category: r.u8(), Physical: r.vector(), Attribution: r.vector(), SettlementPhysical: r.vector(), SettlementAttribution: r.vector()}
		if i < k {
			t.Changes = append(t.Changes, Change{r.vector(), r.vector(), r.vector(), r.vector()})
		}
		if i >= k && !allZero(r.take(96)) {
			return t, ErrUnknown
		}
		c.CapacityCount = r.u8()
		for j := range c.Capacity {
			c.Capacity[j] = CapacityPromise{Constraint: r.id(), Generation: r.u64(), Peak: CapacityVector{r.u64(), r.u64()}}
		}
		c.CapacityBinding = r.digest()
		t.Components = append(t.Components, c)
		if r.end() != nil {
			return t, ErrUnknown
		}
	}
	if !allZero(b[1024+n*512:]) || validateTicketPage(t) != nil {
		return t, ErrUnknown
	}
	return t, nil
}
func encodeManifest(t Ticket, entries []Entry, a HeldAnchor) ([]byte, error) {
	if len(entries) > MaxEntries {
		return nil, ErrBounds
	}
	t.Manifest = Digest{}
	t.ManifestLength = 0
	t.Changes = nil
	t.State = Reserved
	t.Evidence = Digest{}
	initial, e := encodeTicket(t, a)
	if e != nil {
		return nil, e
	}
	b := make([]byte, ManifestBytes)
	w := writer{b: b[pageHeader:]}
	w.put(initial)
	if t.Fanout != nil {
		// The complete host plan is independently bound by Workflow/Operation.
		// This variant has no Q object-entry lane or payload/name side table.
		if len(entries) != 0 {
			return nil, ErrBounds
		}
		w.u16(uint16(len(t.Fanout.Workspaces)))
		w.u8(uint8(len(t.Components)))
		w.u8(1)
		w.u16(0)
		w.u16(MaxWorkspaces / 8)
		w.u64(0)
		for _, ref := range t.Fanout.Workspaces {
			w.id(ref.ID)
			w.u64(ref.Generation)
		}
		for i := 0; i < MaxComponents; i++ {
			var m WorkspaceMembership
			if i < len(t.Fanout.Memberships) {
				m = t.Fanout.Memberships[i]
			}
			w.put(m[:])
		}
		if w.err != nil {
			return nil, w.err
		}
		b[9] = 1
		seal(b, 3, uint16(t.Token.Slot), t.Token.Sequence, a)
		return b, nil
	}
	w.u16(uint16(len(entries)))
	for _, v := range entries {
		if v.Kind != ObjectEntry || int(v.Component) >= len(t.Components) || !relative(v.Target) || !relative(v.Stage) || !(Vector{v.Bytes, v.Files, v.Directories}).valid() {
			return nil, ErrBounds
		}
		w.u8(v.Kind)
		w.u8(v.Component)
		w.variable(v.Target, 1024)
		w.variable(v.Stage, 1024)
		w.digest(v.OldIdentity)
		w.digest(v.NewIdentity)
		w.u64(v.Bytes)
		w.u64(v.Files)
		w.u64(v.Directories)
	}
	if w.err != nil {
		return nil, w.err
	}
	seal(b, 3, uint16(t.Token.Slot), t.Token.Sequence, a)
	return b, nil
}
func decodeManifest(b []byte, t Ticket, a HeldAnchor) (Ticket, []Entry, error) {
	if len(b) != ManifestBytes {
		return Ticket{}, nil, ErrUnknown
	}
	g, e := unsealVariant(b, 3, uint16(t.Token.Slot), a, true)
	if e != nil || g != t.Token.Sequence {
		return Ticket{}, nil, ErrUnknown
	}
	initial, e := decodeTicket(b[pageHeader:pageHeader+TicketBytes], int(t.Token.Slot), a)
	if e != nil || initial.Token.ID != t.Token.ID || initial.Token.Sequence != t.Token.Sequence || initial.Owner != t.Owner || initial.Workflow != t.Workflow || initial.Operation != t.Operation || initial.PolicyRevision != t.PolicyRevision || initial.Token.Generation > t.Token.Generation || initial.Manifest != (Digest{}) || initial.ManifestLength != 0 || initial.State != Reserved || (initial.Fanout != nil) != (t.Fanout != nil) || (initial.Fanout != nil) != (b[9] == 1) {
		return Ticket{}, nil, ErrUnknown
	}
	r := reader{b: b[pageHeader+TicketBytes:]}
	if initial.Fanout != nil {
		n, groups, version := int(r.u16()), int(r.u8()), r.u8()
		entries, width, reserved := r.u16(), r.u16(), r.u64()
		if n > MaxWorkspaces || groups != len(initial.Components) || version != 1 || entries != 0 || width != MaxWorkspaces/8 || reserved != 0 {
			return Ticket{}, nil, ErrUnknown
		}
		f := &WorkspaceFanout{Workspaces: make([]WorkspaceReference, n), Memberships: make([]WorkspaceMembership, groups)}
		for i := range f.Workspaces {
			f.Workspaces[i] = WorkspaceReference{r.id(), r.u64()}
		}
		for i := 0; i < MaxComponents; i++ {
			m := r.take(MaxWorkspaces / 8)
			if i < groups {
				copy(f.Memberships[i][:], m)
			} else if !allZero(m) {
				return Ticket{}, nil, ErrUnknown
			}
		}
		initial.Fanout = f
		if r.err != nil || r.end() != nil || validateTicket(initial) != nil {
			return Ticket{}, nil, ErrUnknown
		}
		return initial, nil, nil
	}
	n := int(r.u16())
	if n > MaxEntries {
		return Ticket{}, nil, ErrUnknown
	}
	out := make([]Entry, 0, n)
	for i := 0; i < n; i++ {
		v := Entry{Kind: r.u8(), Component: r.u8(), Target: r.variable(1024), Stage: r.variable(1024), OldIdentity: r.digest(), NewIdentity: r.digest(), Bytes: r.u64(), Files: r.u64(), Directories: r.u64()}
		if v.Kind != ObjectEntry || int(v.Component) >= len(initial.Components) || !relative(v.Target) || !relative(v.Stage) || !(Vector{v.Bytes, v.Files, v.Directories}).valid() {
			return Ticket{}, nil, ErrUnknown
		}
		out = append(out, v)
	}
	if r.end() != nil {
		return Ticket{}, nil, ErrUnknown
	}
	return initial, out, nil
}
