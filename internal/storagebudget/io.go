package storagebudget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"os"
	"time"
)

type Coordinator struct {
	anchor  HeldAnchor
	lock    *os.File
	gate    chan struct{}
	closed  bool
	decoded [2]decodedBank
	// A deterministic package-test seam, not a production callback under Q.
	fault *ioFault
}
type ioFault struct {
	Name, Phase string
	Offset      int64
	Short       bool
	Remaining   int
}

func (q *Coordinator) fail(name, phase string, offset int64) (bool, error) {
	f := q.fault
	if f != nil && f.Name == name && f.Phase == phase && (f.Offset < 0 || f.Offset == offset) {
		if f.Remaining > 0 {
			f.Remaining--
			return false, nil
		}
		q.fault = nil
		return f.Short, io.ErrUnexpectedEOF
	}
	return false, nil
}

func OpenHeld(a HeldAnchor) (*Coordinator, error) {
	if a.Root == nil || a.LockWitness == nil || a.Installation.zero() || !authority(a.Binding) {
		return nil, ErrAuthority
	}
	info, e := a.Root.Stat(".")
	if e != nil || !privateInfo(info, true) {
		return nil, ErrAuthority
	}
	witness, e := a.LockWitness.Stat()
	if e != nil || !privateInfo(witness, false) || witness.Size() != 0 {
		return nil, ErrAuthority
	}
	before, e := a.Root.Lstat("lock")
	if e != nil || !privateInfo(before, false) || !os.SameFile(before, witness) {
		return nil, ErrAuthority
	}
	f, e := a.Root.OpenFile("lock", os.O_RDWR|noFollow|nonBlocking, 0)
	if e != nil {
		return nil, ErrAuthority
	}
	opened, e := f.Stat()
	if e != nil || !privateInfo(opened, false) || !os.SameFile(opened, witness) {
		f.Close()
		return nil, ErrAuthority
	}
	q := &Coordinator{anchor: a, lock: f, gate: make(chan struct{}, 1)}
	q.gate <- struct{}{}
	return q, nil
}
func (q *Coordinator) Close() error {
	<-q.gate
	defer func() { q.gate <- struct{}{} }()
	if q.closed {
		return nil
	}
	q.closed = true
	q.decoded = [2]decodedBank{}
	return q.lock.Close()
}
func (q *Coordinator) locked(ctx context.Context, run func() error) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case <-ctx.Done():
		return refuse(ErrBusy, "lock wait")
	case <-q.gate:
	}
	defer func() { q.gate <- struct{}{} }()
	if q.closed {
		return os.ErrClosed
	}
	if e := q.lockIdentity(); e != nil {
		return e
	}
	delay := 5 * time.Millisecond
	for {
		e := flock(q.lock, true)
		if e == nil {
			break
		}
		if !lockBusy(e) {
			return refuse(ErrUnknown, "flock unavailable")
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ErrBusy
		case <-timer.C:
		}
		if delay < 100*time.Millisecond {
			delay *= 2
		}
	}
	defer func() { _ = unflock(q.lock) }()
	if e := q.lockIdentity(); e != nil {
		return e
	}
	if ctx.Err() != nil {
		return ErrBusy
	}
	return run()
}
func (q *Coordinator) lockIdentity() error {
	named, e := q.anchor.Root.Lstat("lock")
	if e != nil {
		return ErrAuthority
	}
	held, e := q.lock.Stat()
	if e != nil || !privateInfo(named, false) || !privateInfo(held, false) || !os.SameFile(named, held) || held.Size() != 0 {
		return ErrAuthority
	}
	root, e := q.anchor.Root.Stat(".")
	if e != nil || !privateInfo(root, true) {
		return ErrAuthority
	}
	return nil
}
func expectedSize(name string) int64 {
	switch name {
	case "checkpoint.a", "checkpoint.b":
		return CheckpointBytes
	case "tickets.a", "tickets.b":
		return Slots * TicketBytes
	case "manifests":
		return Slots * ManifestBytes
	}
	return -1
}
func (q *Coordinator) openFile(name string, write bool) (*os.File, error) {
	size := expectedSize(name)
	if size < 0 {
		return nil, ErrBounds
	}
	before, e := q.anchor.Root.Lstat(name)
	if e != nil || !privateInfo(before, false) || before.Size() != size {
		return nil, refuse(ErrUnknown, "fixed file shape")
	}
	mode := os.O_RDONLY
	if write {
		mode = os.O_RDWR
	}
	f, e := q.anchor.Root.OpenFile(name, mode|noFollow|nonBlocking, 0)
	if e != nil {
		return nil, refuse(ErrUnknown, "fixed file open")
	}
	held, e := f.Stat()
	if e != nil || !privateInfo(held, false) || !os.SameFile(before, held) || held.Size() != size {
		f.Close()
		return nil, refuse(ErrUnknown, "fixed file custody")
	}
	return f, nil
}
func (q *Coordinator) readAt(name string, offset int64, size int) ([]byte, error) {
	f, e := q.openFile(name, false)
	if e != nil {
		return nil, e
	}
	b := make([]byte, size)
	_, e = f.ReadAt(b, offset)
	ce := f.Close()
	if e != nil || ce != nil {
		return nil, refuse(ErrUnknown, "fixed file read/close")
	}
	return b, nil
}
func (q *Coordinator) writeAt(name string, offset int64, b []byte) error {
	if offset < 0 || int64(len(b)) > expectedSize(name)-offset {
		return ErrBounds
	}
	f, e := q.openFile(name, true)
	if e != nil {
		return e
	}
	short, fe := q.fail(name, "write", offset)
	if fe != nil {
		if short {
			_, _ = f.WriteAt(b[:32], offset)
		}
		f.Close()
		return refuse(ErrUnknown, "write outcome")
	}
	n, e := f.WriteAt(b, offset)
	if e != nil || n != len(b) {
		f.Close()
		return refuse(ErrUnknown, "short/write outcome")
	}
	_, fe = q.fail(name, "sync", offset)
	if fe != nil {
		f.Close()
		return refuse(ErrUnknown, "sync outcome")
	}
	if e = syncFile(f); e != nil {
		f.Close()
		return refuse(ErrUnknown, "sync outcome")
	}
	ce := f.Close()
	_, fe = q.fail(name, "close", offset)
	if ce != nil || fe != nil {
		return refuse(ErrUnknown, "close outcome")
	}
	_, fe = q.fail(name, "readback", offset)
	if fe != nil {
		return refuse(ErrUnknown, "readback outcome")
	}
	read, e := q.readAt(name, offset, len(b))
	if e != nil || !bytes.Equal(read, b) {
		return refuse(ErrUnknown, "readback outcome")
	}
	return nil
}

// InitializeHeld is explicit and one-shot. The original owner has already
// created/validated the private anchor and stable lock. Partial initialization
// is never overwritten by InitializeHeld; original-owner Repair must handle it.
func (q *Coordinator) InitializeHeld(ctx context.Context, p Policy) error {
	if !p.valid() {
		return ErrBounds
	}
	return q.locked(ctx, func() error {
		dir, e := q.anchor.Root.Open(".")
		if e != nil {
			return e
		}
		names, e := dir.ReadDir(8)
		ce := dir.Close()
		if e != nil || ce != nil {
			return ErrUnknown
		}
		if len(names) != 1 || names[0].Name() != "lock" {
			return ErrUnknown
		}
		c := &checkpoint{Generation: 1, Epoch: 1, Maintenance: true, Policy: p}
		for i := range c.Applied {
			c.Applied[i].NextSequence = 1
		}
		cp, e := encodeCheckpoint(c, q.anchor)
		if e != nil {
			return e
		}
		for _, name := range []string{"checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests"} {
			if _, e = q.fail(name, "initialize-create", 0); e != nil {
				return ErrUnknown
			}
			f, e := q.anchor.Root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|noFollow|nonBlocking, 0600)
			if e != nil {
				return ErrUnknown
			}
			var offset int64
			write := func(b []byte) error {
				short, fault := q.fail(name, "initialize-write", offset)
				if fault != nil {
					if short {
						_, _ = f.Write(b[:min(32, len(b))])
					}
					return fault
				}
				n, e := f.Write(b)
				offset += int64(n)
				if e != nil {
					return e
				}
				if n != len(b) {
					return io.ErrShortWrite
				}
				return nil
			}
			switch name {
			case "checkpoint.a", "checkpoint.b":
				e = write(cp)
			case "tickets.a", "tickets.b":
				for i := 0; i < Slots && e == nil; i++ {
					var b []byte
					b, e = encodeTicket(Ticket{Token: Token{Slot: uint8(i)}}, q.anchor)
					if e == nil {
						e = write(b)
					}
				}
			case "manifests":
				zero := make([]byte, ManifestBytes)
				for i := 0; i < Slots && e == nil; i++ {
					e = write(zero)
				}
			}
			if e == nil {
				_, e = q.fail(name, "initialize-sync", 0)
				if e == nil {
					e = syncFile(f)
				}
			}
			ce := f.Close()
			_, closeFault := q.fail(name, "initialize-close", 0)
			if e != nil || ce != nil || closeFault != nil {
				return ErrUnknown
			}
		}
		d, e := q.anchor.Root.Open(".")
		if e != nil {
			return ErrUnknown
		}
		_, e = q.fail(".", "initialize-sync", 0)
		if e == nil {
			e = syncFile(d)
		}
		ce = d.Close()
		_, closeFault := q.fail(".", "initialize-close", 0)
		if e != nil || ce != nil || closeFault != nil {
			return ErrUnknown
		}
		if _, e = q.fail(".", "initialize-readback", 0); e != nil {
			return ErrUnknown
		}
		_, e = q.load()
		return e
	})
}

type loaded struct {
	cp           *checkpoint
	tickets      [Slots]Ticket
	ticketBank   [Slots]int
	cpBank       int
	diskApplied  [Slots]applied
	censusDigest Digest
}

func (q *Coordinator) load() (*loaded, error) { return q.loadForCensus(false) }

// The optional fingerprint uses the original strict parser and its exact raw
// selected pages. Ordinary inspection/replay does not hash or change behavior.
func (q *Coordinator) loadForCensus(census bool) (*loaded, error) {
	return q.loadWithEnrollment(census, nil)
}

// Optional detached enrollment bytes are taken from this exact strict load.
// Existing callers retain neither raw banks nor a new recovered capability.
func (q *Coordinator) loadWithEnrollment(census bool, enrollment *EnrollmentInspection) (*loaded, error) {
	// Include inactive/retired regions before a caller can fence or overwrite.
	if e := q.checkLegacyFormat(); e != nil {
		return nil, e
	}
	l := new(loaded)
	var raw [2][]byte
	var cp [2]*checkpoint
	for i, n := range []string{"checkpoint.a", "checkpoint.b"} {
		b, e := q.readAt(n, 0, CheckpointBytes)
		if e != nil {
			return nil, e
		}
		raw[i] = b
		// Both retained decodes are only compared here; the selected one is
		// copied below, so the other bank's model is never copied per load.
		cp[i], e = q.checkpointBank(i, b)
		if e != nil {
			return nil, refuse(ErrUnknown, "checkpoint page")
		}
	}
	high := 0
	if cp[1].Generation > cp[0].Generation {
		high = 1
	}
	low := 1 - high
	if cp[high].Generation-cp[low].Generation > 1 || cp[high].Generation == cp[low].Generation && !bytes.Equal(raw[0], raw[1]) {
		return nil, refuse(ErrUnknown, "checkpoint generations")
	}
	selected := *cp[high]
	l.cp = &selected
	l.cpBank = high
	l.diskApplied = l.cp.Applied
	var censusHash = sha256.New()
	if census {
		censusHash.Write([]byte("desk-exclusive-startup-census-selected-q-v1\x00"))
		censusHash.Write([]byte{2, 0})
		censusHash.Write(q.anchor.Installation[:])
		censusHash.Write(q.anchor.Binding[:])
		v := sha256.Sum256(raw[high])
		censusHash.Write(v[:])
	}
	var banks [2][]byte
	for i, n := range []string{"tickets.a", "tickets.b"} {
		b, e := q.readAt(n, 0, Slots*TicketBytes)
		if e != nil {
			return nil, e
		}
		banks[i] = b
	}
	for i := 0; i < Slots; i++ {
		var ts [2]Ticket
		for j := range ts {
			t, e := q.decodeTicketBank(j, i, banks[j][i*TicketBytes:(i+1)*TicketBytes])
			if e != nil {
				return nil, refuse(ErrUnknown, "ticket page")
			}
			ts[j] = t
		}
		high = 0
		if ts[1].Token.Generation > ts[0].Token.Generation {
			high = 1
		}
		low = 1 - high
		t, o := ts[high], ts[low]
		if t.Token.Generation-o.Token.Generation > 1 {
			return nil, refuse(ErrUnknown, "ticket generations")
		}
		if t.Token.Generation == o.Token.Generation {
			if !bytes.Equal(banks[0][i*TicketBytes:(i+1)*TicketBytes], banks[1][i*TicketBytes:(i+1)*TicketBytes]) {
				return nil, ErrUnknown
			}
		} else if e := legalTransition(o, t, l.diskApplied[i]); e != nil {
			return nil, e
		}
		l.tickets[i] = t
		l.ticketBank[i] = high
		if census {
			v := sha256.Sum256(banks[high][i*TicketBytes : (i+1)*TicketBytes])
			censusHash.Write([]byte{byte(i)})
			censusHash.Write(v[:])
		}
		// Exact applied identity retires this manifest. Never read an overwritten
		// manifest through the old terminal page, even if that page is still valid.
		if t.State == Empty || retired(t, l.diskApplied[i]) {
			continue
		}
		if t.Manifest == (Digest{}) {
			return nil, ErrUnknown
		}
		b, e := q.readAt("manifests", int64(i*ManifestBytes), ManifestBytes)
		if e != nil {
			return nil, e
		}
		if sha256.Sum256(b) != t.Manifest {
			return nil, refuse(ErrUnknown, "manifest digest")
		}
		initial, _, e := decodeManifest(b, t, q.anchor)
		if e != nil || !sameComponents(initial.Components, t.Components) {
			return nil, refuse(ErrUnknown, "manifest binding")
		}
		t.Fanout = cloneFanout(initial.Fanout)
		if e := validateTicket(t); e != nil {
			return nil, e
		}
		if e := registered(l.cp, t, false); e != nil {
			return nil, e
		}
		l.tickets[i] = t
	}
	if census {
		for i, t := range l.tickets {
			if t.State != Empty && !retired(t, l.diskApplied[i]) {
				return nil, ErrBusy
			}
		}
		copy(l.censusDigest[:], censusHash.Sum(nil))
	}
	if enrollment != nil {
		enrollment.Checkpoints = raw
		enrollment.Tickets = banks
		enrollment.CheckpointBank = uint8(l.cpBank)
		for i, bank := range l.ticketBank {
			enrollment.TicketBanks[i] = uint8(bank)
		}
	}
	for _, t := range l.tickets {
		if e := terminalApply(l.cp, t); e != nil {
			return nil, e
		}
	}
	return l, nil
}

func legacyFormat(name string, b []byte) bool {
	legacy := func(offset int) bool {
		return offset >= 0 && offset <= len(b)-8 && string(b[offset:offset+8]) == "JPSTB001"
	}
	switch name {
	case "checkpoint.a", "checkpoint.b":
		return legacy(0)
	case "tickets.a", "tickets.b":
		for i := 0; i < Slots; i++ {
			if legacy(i * TicketBytes) {
				return true
			}
		}
	case "manifests":
		for i := 0; i < Slots; i++ {
			if legacy(i*ManifestBytes) || legacy(i*ManifestBytes+pageHeader) {
				return true
			}
		}
	}
	return false
}

// This reads only fixed metadata headers, never host journal paths or callbacks.
func (q *Coordinator) checkLegacyFormat() error {
	for _, name := range []string{"checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests"} {
		f, e := q.openFile(name, false)
		if e != nil {
			return e
		}
		var offsets []int64
		switch name {
		case "checkpoint.a", "checkpoint.b":
			offsets = []int64{0}
		case "tickets.a", "tickets.b":
			for i := 0; i < Slots; i++ {
				offsets = append(offsets, int64(i*TicketBytes))
			}
		case "manifests":
			for i := 0; i < Slots; i++ {
				offsets = append(offsets, int64(i*ManifestBytes), int64(i*ManifestBytes+pageHeader))
			}
		}
		var header [8]byte
		for _, offset := range offsets {
			if _, e = f.ReadAt(header[:], offset); e != nil {
				_ = f.Close()
				return ErrUnknown
			}
			if string(header[:]) == "JPSTB001" {
				_ = f.Close()
				return refuse(ErrUnsupported, "prior storage format preserved")
			}
		}
		if e = f.Close(); e != nil {
			return ErrUnknown
		}
	}
	return nil
}
func legalTransition(old, new Ticket, a applied) error {
	if new.Token.Generation != old.Token.Generation+1 {
		return ErrUnknown
	}
	if old.State == Empty || retired(old, a) {
		if new.State != Reserved || new.Token.Sequence != a.NextSequence || new.Token.ID.zero() {
			return ErrUnknown
		}
		return nil
	}
	if old.Token.ID != new.Token.ID || old.Token.Sequence != new.Token.Sequence || old.Owner != new.Owner || old.Workflow != new.Workflow || old.Operation != new.Operation || old.PolicyRevision != new.PolicyRevision || old.ManifestLength != new.ManifestLength || old.Manifest != new.Manifest || !sameComponents(old.Components, new.Components) || (old.Fanout != nil) != (new.Fanout != nil) || old.State == Terminal {
		return ErrUnknown
	}
	if new.State == Terminal || new.State == Uncertain {
		return nil
	}
	if old.State == Reserved && new.State == Staging || old.State == Staging && new.State == Dispatched {
		return nil
	}
	return ErrUnknown
}
func sameComponents(a, b []Component) bool {
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
func (q *Coordinator) saveTicket(l *loaded, t Ticket) error {
	b, e := encodeTicket(t, q.anchor)
	if e != nil {
		return e
	}
	name := "tickets.a"
	if l.ticketBank[t.Token.Slot] == 0 {
		name = "tickets.b"
	}
	return q.writeAt(name, int64(t.Token.Slot)*TicketBytes, b)
}
func (q *Coordinator) saveCheckpoint(l *loaded) error {
	g, e := next(l.cp.Generation)
	if e != nil {
		return e
	}
	l.cp.Generation = g
	b, e := encodeCheckpoint(l.cp, q.anchor)
	if e != nil {
		return e
	}
	name := "checkpoint.a"
	if l.cpBank == 0 {
		name = "checkpoint.b"
	}
	if e = q.writeAt(name, 0, b); e != nil {
		return e
	}
	l.cpBank = 1 - l.cpBank
	l.diskApplied = l.cp.Applied
	return nil
}

// Pure page operands owned by the original Coordinator and accessed only under
// its gate. Every load still reads actual files and performs the complete joins.
// Returned values never alias these retained decodes.
type decodedBank struct {
	installation  ID
	binding       Digest
	checkpointRaw []byte
	checkpoint    *checkpoint
	ticketRaw     [Slots][]byte
	tickets       [Slots]Ticket
}

func (q *Coordinator) decodedBank(bank int) *decodedBank {
	b := &q.decoded[bank]
	if b.installation != q.anchor.Installation || b.binding != q.anchor.Binding {
		*b = decodedBank{installation: q.anchor.Installation, binding: q.anchor.Binding}
	}
	return b
}
func (q *Coordinator) decodeCheckpointBank(bank int, raw []byte) (*checkpoint, error) {
	value, err := q.checkpointBank(bank, raw)
	if err != nil {
		return nil, err
	}
	copy := *value
	return &copy, nil
}

// checkpointBank returns the retained strict decode of exactly raw. It is
// read-only: callers copy whatever they keep and never mutate or retain it.
func (q *Coordinator) checkpointBank(bank int, raw []byte) (*checkpoint, error) {
	b := q.decodedBank(bank)
	if b.checkpoint != nil && bytes.Equal(raw, b.checkpointRaw) {
		return b.checkpoint, nil
	}
	// The strict unsealer temporarily changes its input's checksum. The fresh
	// read is private here; retain an independent snapshot only after success.
	value, err := decodeCheckpoint(raw, q.anchor)
	if err != nil {
		return nil, err
	}
	b.checkpointRaw = append([]byte(nil), raw...)
	b.checkpoint = value
	return value, nil
}
func (q *Coordinator) decodeTicketBank(bank, slot int, raw []byte) (Ticket, error) {
	b := q.decodedBank(bank)
	if b.ticketRaw[slot] != nil && bytes.Equal(raw, b.ticketRaw[slot]) {
		return cloneTicket(b.tickets[slot]), nil
	}
	value, err := decodeTicket(raw, slot, q.anchor)
	if err != nil {
		return Ticket{}, err
	}
	b.ticketRaw[slot] = append([]byte(nil), raw...)
	b.tickets[slot] = cloneTicket(value)
	return value, nil
}
