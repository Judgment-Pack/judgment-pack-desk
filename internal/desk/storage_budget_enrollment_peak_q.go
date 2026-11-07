package desk

// Private pure copies of the pinned original Q comparison codec/validation.
// Mechanical identifier isolation preserves the original layouts/domains and
// transition arithmetic. No Coordinator, handle, path, lock, randomness or I/O
// API is included; this data cannot grant authority or replay an operation.
import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
	"math"
	"time"
	"unicode/utf8"
)

const (
	storageEnrollmentPeakQCategories                                         = 9
	storageEnrollmentPeakQMaxRoots                                           = 4096
	storageEnrollmentPeakQMaxWorkspaces                                      = 1024
	storageEnrollmentPeakQMaxFilesystems                                     = 128
	storageEnrollmentPeakQMaxCapacityConstraints                             = 512
	storageEnrollmentPeakQMaxCapacityRefs                                    = 4
	storageEnrollmentPeakQSlots                                              = 64
	storageEnrollmentPeakQMaxComponents                                      = 16
	storageEnrollmentPeakQCheckpointBytes                                    = 4 << 20
	storageEnrollmentPeakQTicketBytes                                        = 16 << 10
	storageEnrollmentPeakQManifestBytes                                      = 512 << 10
	storageEnrollmentPeakQMetadataBudget                                     = 64 << 20
	storageEnrollmentPeakQMaxBytes                                           = 1 << 60
	storageEnrollmentPeakQMaxCount                                           = 1 << 40
	storageEnrollmentPeakQPageHeader                                         = 128
	storageEnrollmentPeakQRootOffset                                         = 32 << 10
	storageEnrollmentPeakQWorkspaceOffset                                    = storageEnrollmentPeakQRootOffset + 4096*768
	storageEnrollmentPeakQFilesystemOffset                                   = storageEnrollmentPeakQWorkspaceOffset + 1024*512
	storageEnrollmentPeakQAppliedOffset                                      = storageEnrollmentPeakQFilesystemOffset + 128*256
	storageEnrollmentPeakQCapacityOffset                                     = storageEnrollmentPeakQAppliedOffset + 64*128
	storageEnrollmentPeakQControl                                            = 4
	storageEnrollmentPeakQCapacityBytes                                      = 1
	storageEnrollmentPeakQCapacityInodes                                     = 2
	storageEnrollmentPeakQInodesUnknown                                      = 0
	storageEnrollmentPeakQInodesFinite                                       = 1
	storageEnrollmentPeakQInodesDynamic                                      = 2
	storageEnrollmentPeakQEmpty                  storageEnrollmentPeakQState = 0
	storageEnrollmentPeakQReserved               storageEnrollmentPeakQState = 1
	storageEnrollmentPeakQStaging                storageEnrollmentPeakQState = 2
	storageEnrollmentPeakQDispatched             storageEnrollmentPeakQState = 3
	storageEnrollmentPeakQUncertain              storageEnrollmentPeakQState = 4
	storageEnrollmentPeakQTerminal               storageEnrollmentPeakQState = 5
)

type storageEnrollmentPeakQID [16]byte

type storageEnrollmentPeakQDigest [32]byte

type storageEnrollmentPeakQVector struct{ Bytes, Files, Directories uint64 }

type storageEnrollmentPeakQLimits struct {
	Total    storageEnrollmentPeakQVector
	Category [storageEnrollmentPeakQCategories]storageEnrollmentPeakQVector
}

type storageEnrollmentPeakQPolicy struct {
	Installation, Workspace storageEnrollmentPeakQLimits
	ObservationMaxAge       time.Duration
}

type storageEnrollmentPeakQRoot struct {
	ID                    storageEnrollmentPeakQID
	Owner, Identity       storageEnrollmentPeakQDigest
	Filesystem, Workspace storageEnrollmentPeakQID
	Generation            uint64
	Locator               string // bounded opaque owner recovery locator; never followed here
	Known                 bool
	Usage                 [storageEnrollmentPeakQCategories]storageEnrollmentPeakQVector
	CapacityCount         uint8
	CapacityRefs          [storageEnrollmentPeakQMaxCapacityRefs]storageEnrollmentPeakQCapacityRef
	CapacityBinding       storageEnrollmentPeakQDigest
}

type storageEnrollmentPeakQWorkspace struct {
	ID         storageEnrollmentPeakQID
	Generation uint64
	Known      bool
	Usage      [storageEnrollmentPeakQCategories]storageEnrollmentPeakQVector
	Limits     storageEnrollmentPeakQLimits
}

type storageEnrollmentPeakQFilesystem struct {
	ID                              storageEnrollmentPeakQID
	Identity                        storageEnrollmentPeakQDigest
	Generation                      uint64
	InodeModel                      uint8
	CapabilityEvidence              storageEnrollmentPeakQDigest
	ByteConstraint, InodeConstraint storageEnrollmentPeakQID
}

type storageEnrollmentPeakQCapacityVector struct{ Bytes, Inodes uint64 }

type storageEnrollmentPeakQCapacityRef struct {
	Constraint storageEnrollmentPeakQID
	Generation uint64
}

type storageEnrollmentPeakQCapacityPromise struct {
	Constraint storageEnrollmentPeakQID
	Generation uint64
	Peak       storageEnrollmentPeakQCapacityVector
}

type storageEnrollmentPeakQCapacityConstraint struct {
	ID         storageEnrollmentPeakQID
	Identity   storageEnrollmentPeakQDigest
	Generation uint64
	Resources  uint8
	Headroom   storageEnrollmentPeakQCapacityVector
}

type storageEnrollmentPeakQComponent struct {
	Filesystem           storageEnrollmentPeakQID
	FilesystemGeneration uint64
	Root                 storageEnrollmentPeakQID
	RootGeneration       uint64
	Owner                storageEnrollmentPeakQDigest // original custody proof for this registered root
	Workspace            storageEnrollmentPeakQID
	WorkspaceGeneration  uint64
	Category             uint8
	// Physical is charged once to root/installation logical usage. Attribution
	// is charged to the referencing workspace; Capacity independently promises
	// each declared policy/known native restriction without duplicating logical
	// usage. Versioned authored estimates do not certify native allocation.
	Physical, Attribution                     storageEnrollmentPeakQVector
	SettlementPhysical, SettlementAttribution storageEnrollmentPeakQVector
	CapacityCount                             uint8
	Capacity                                  [storageEnrollmentPeakQMaxCapacityRefs]storageEnrollmentPeakQCapacityPromise
	CapacityBinding                           storageEnrollmentPeakQDigest
}

type storageEnrollmentPeakQToken struct {
	Slot                 uint8
	ID                   storageEnrollmentPeakQID
	Sequence, Generation uint64
}

type storageEnrollmentPeakQState uint8

type storageEnrollmentPeakQWorkspaceReference struct {
	ID         storageEnrollmentPeakQID
	Generation uint64
}
type storageEnrollmentPeakQWorkspaceMembership [storageEnrollmentPeakQMaxWorkspaces / 8]byte
type storageEnrollmentPeakQWorkspaceFanout struct {
	Workspaces  []storageEnrollmentPeakQWorkspaceReference
	Memberships []storageEnrollmentPeakQWorkspaceMembership
}

type storageEnrollmentPeakQTicket struct {
	Token               storageEnrollmentPeakQToken
	State               storageEnrollmentPeakQState
	PolicyRevision      uint64
	Workflow, Operation string
	Owner               storageEnrollmentPeakQDigest
	Manifest            storageEnrollmentPeakQDigest
	ManifestLength      uint32
	Components          []storageEnrollmentPeakQComponent
	Fanout              *storageEnrollmentPeakQWorkspaceFanout
	Changes             []storageEnrollmentPeakQChange
	// Evidence binds domain-authored stage/dispatch or observed settlement facts.
	Evidence storageEnrollmentPeakQDigest
}

type storageEnrollmentPeakQChange struct{ PhysicalAdd, PhysicalRemove, AttributionAdd, AttributionRemove storageEnrollmentPeakQVector }

type storageEnrollmentPeakQCensus struct {
	Roots      []storageEnrollmentPeakQRootMeasurement
	Workspaces []storageEnrollmentPeakQWorkspaceMeasurement
	Evidence   storageEnrollmentPeakQDigest
}

type storageEnrollmentPeakQRootMeasurement struct {
	ID         storageEnrollmentPeakQID
	Generation uint64
	Usage      [storageEnrollmentPeakQCategories]storageEnrollmentPeakQVector
}

type storageEnrollmentPeakQWorkspaceMeasurement struct {
	ID         storageEnrollmentPeakQID
	Generation uint64
	Usage      [storageEnrollmentPeakQCategories]storageEnrollmentPeakQVector
}

func (id storageEnrollmentPeakQID) zero() bool { return id == storageEnrollmentPeakQID{} }

func (v storageEnrollmentPeakQVector) zero() bool { return v == storageEnrollmentPeakQVector{} }

func (v storageEnrollmentPeakQVector) valid() bool {
	return v.Bytes <= storageEnrollmentPeakQMaxBytes && v.Files <= storageEnrollmentPeakQMaxCount && v.Directories <= storageEnrollmentPeakQMaxCount
}

func (l storageEnrollmentPeakQLimits) valid() bool {
	if !l.Total.valid() || l.Total.Bytes == 0 || l.Total.Files == 0 || l.Total.Directories == 0 {
		return false
	}
	for _, v := range l.Category {
		if !v.valid() || v.Bytes == 0 || v.Files == 0 || v.Directories == 0 {
			return false
		}
	}
	return true
}

func (p storageEnrollmentPeakQPolicy) valid() bool {
	return p.Installation.valid() && p.Workspace.valid() && p.ObservationMaxAge > 0 && p.ObservationMaxAge <= time.Hour &&
		(storageEnrollmentPeakQVector{storageEnrollmentPeakQMetadataBudget, 16, 1}).le(p.Installation.Total) && (storageEnrollmentPeakQVector{storageEnrollmentPeakQMetadataBudget, 16, 1}).le(p.Installation.Category[storageEnrollmentPeakQControl])
}

func (v storageEnrollmentPeakQVector) le(w storageEnrollmentPeakQVector) bool {
	return v.Bytes <= w.Bytes && v.Files <= w.Files && v.Directories <= w.Directories
}

func (c storageEnrollmentPeakQComponent) total() (storageEnrollmentPeakQVector, storageEnrollmentPeakQVector, error) {
	p, e := storageEnrollmentPeakQAdd(c.Physical, c.SettlementPhysical)
	if e != nil {
		return p, storageEnrollmentPeakQVector{}, e
	}
	w, e := storageEnrollmentPeakQAdd(c.Attribution, c.SettlementAttribution)
	return p, w, e
}

func storageEnrollmentPeakQAdd(a, b storageEnrollmentPeakQVector) (storageEnrollmentPeakQVector, error) {
	if !a.valid() || !b.valid() || a.Bytes > storageEnrollmentPeakQMaxBytes-b.Bytes || a.Files > storageEnrollmentPeakQMaxCount-b.Files || a.Directories > storageEnrollmentPeakQMaxCount-b.Directories {
		return storageEnrollmentPeakQVector{}, storagebudget.ErrBounds
	}
	return storageEnrollmentPeakQVector{a.Bytes + b.Bytes, a.Files + b.Files, a.Directories + b.Directories}, nil
}

func storageEnrollmentPeakQSub(a, b storageEnrollmentPeakQVector) (storageEnrollmentPeakQVector, error) {
	if !b.le(a) {
		return storageEnrollmentPeakQVector{}, storagebudget.ErrUnknown
	}
	return storageEnrollmentPeakQVector{a.Bytes - b.Bytes, a.Files - b.Files, a.Directories - b.Directories}, nil
}

func storageEnrollmentPeakQSum(v [storageEnrollmentPeakQCategories]storageEnrollmentPeakQVector) (out storageEnrollmentPeakQVector, err error) {
	for _, x := range v {
		out, err = storageEnrollmentPeakQAdd(out, x)
		if err != nil {
			return
		}
	}
	return
}

func storageEnrollmentPeakQNext(n uint64) (uint64, error) {
	if n == math.MaxUint64 {
		return 0, storagebudget.ErrBounds
	}
	return n + 1, nil
}

func storageEnrollmentPeakQTextValid(s string, n int, empty bool) bool {
	if len(s) > n || (!empty && len(s) == 0) {
		return false
	}
	for _, r := range s {
		if r == 0 || r == '\r' || r == '\n' || r == '\ufffd' {
			return false
		}
	}
	return true
}

func storageEnrollmentPeakQAuthority(d storageEnrollmentPeakQDigest) bool {
	return d != (storageEnrollmentPeakQDigest{})
}

type storageEnrollmentPeakQHeldAnchor struct {
	Installation storageEnrollmentPeakQID
	Binding      storageEnrollmentPeakQDigest
}

type storageEnrollmentPeakQApplied struct {
	ID                                 storageEnrollmentPeakQID
	Sequence, Generation, NextSequence uint64
}
type storageEnrollmentPeakQCheckpoint struct {
	Generation          uint64
	Epoch               uint64
	Maintenance         bool
	Policy              storageEnrollmentPeakQPolicy
	Installation        [storageEnrollmentPeakQCategories]storageEnrollmentPeakQVector
	Roots               [storageEnrollmentPeakQMaxRoots]storageEnrollmentPeakQRoot
	Workspaces          [storageEnrollmentPeakQMaxWorkspaces]storageEnrollmentPeakQWorkspace
	Filesystems         [storageEnrollmentPeakQMaxFilesystems]storageEnrollmentPeakQFilesystem
	CapacityConstraints [storageEnrollmentPeakQMaxCapacityConstraints]storageEnrollmentPeakQCapacityConstraint
	Applied             [storageEnrollmentPeakQSlots]storageEnrollmentPeakQApplied
}
type storageEnrollmentPeakQWriter struct {
	b   []byte
	n   int
	err error
}

func (w *storageEnrollmentPeakQWriter) put(b []byte) {
	if w.err != nil {
		return
	}
	if len(b) > len(w.b)-w.n {
		w.err = storagebudget.ErrBounds
		return
	}
	copy(w.b[w.n:], b)
	w.n += len(b)
}
func (w *storageEnrollmentPeakQWriter) u8(v uint8) { w.put([]byte{v}) }
func (w *storageEnrollmentPeakQWriter) u16(v uint16) {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	w.put(b[:])
}
func (w *storageEnrollmentPeakQWriter) u32(v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	w.put(b[:])
}
func (w *storageEnrollmentPeakQWriter) u64(v uint64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	w.put(b[:])
}
func (w *storageEnrollmentPeakQWriter) id(v storageEnrollmentPeakQID)         { w.put(v[:]) }
func (w *storageEnrollmentPeakQWriter) digest(v storageEnrollmentPeakQDigest) { w.put(v[:]) }
func (w *storageEnrollmentPeakQWriter) vector(v storageEnrollmentPeakQVector) {
	w.u64(v.Bytes)
	w.u64(v.Files)
	w.u64(v.Directories)
}
func (w *storageEnrollmentPeakQWriter) usage(v [storageEnrollmentPeakQCategories]storageEnrollmentPeakQVector) {
	for _, a := range v {
		w.vector(a)
	}
}
func (w *storageEnrollmentPeakQWriter) limits(v storageEnrollmentPeakQLimits) {
	w.vector(v.Total)
	w.usage(v.Category)
}
func (w *storageEnrollmentPeakQWriter) text(s string, max int) {
	if !utf8.ValidString(s) || len(s) > max {
		w.err = storagebudget.ErrBounds
		return
	}
	w.u16(uint16(len(s)))
	p := make([]byte, max)
	copy(p, s)
	w.put(p)
}
func (w *storageEnrollmentPeakQWriter) variable(s string, max int) {
	if !utf8.ValidString(s) || len(s) > max {
		w.err = storagebudget.ErrBounds
		return
	}
	w.u16(uint16(len(s)))
	w.put([]byte(s))
}

type storageEnrollmentPeakQReader struct {
	b   []byte
	n   int
	err error
}

func (r *storageEnrollmentPeakQReader) take(n int) []byte {
	if r.err != nil {
		return make([]byte, n)
	}
	if n < 0 || n > len(r.b)-r.n {
		r.err = storagebudget.ErrUnknown
		return make([]byte, n)
	}
	v := r.b[r.n : r.n+n]
	r.n += n
	return v
}
func (r *storageEnrollmentPeakQReader) u8() uint8   { return r.take(1)[0] }
func (r *storageEnrollmentPeakQReader) u16() uint16 { return binary.LittleEndian.Uint16(r.take(2)) }
func (r *storageEnrollmentPeakQReader) u32() uint32 { return binary.LittleEndian.Uint32(r.take(4)) }
func (r *storageEnrollmentPeakQReader) u64() uint64 { return binary.LittleEndian.Uint64(r.take(8)) }
func (r *storageEnrollmentPeakQReader) id() (v storageEnrollmentPeakQID) {
	copy(v[:], r.take(16))
	return
}
func (r *storageEnrollmentPeakQReader) digest() (v storageEnrollmentPeakQDigest) {
	copy(v[:], r.take(32))
	return
}
func (r *storageEnrollmentPeakQReader) vector() storageEnrollmentPeakQVector {
	return storageEnrollmentPeakQVector{r.u64(), r.u64(), r.u64()}
}
func (r *storageEnrollmentPeakQReader) usage() (v [storageEnrollmentPeakQCategories]storageEnrollmentPeakQVector) {
	for i := range v {
		v[i] = r.vector()
	}
	return
}
func (r *storageEnrollmentPeakQReader) limits() storageEnrollmentPeakQLimits {
	return storageEnrollmentPeakQLimits{r.vector(), r.usage()}
}
func (r *storageEnrollmentPeakQReader) text(max int) string {
	n := int(r.u16())
	p := r.take(max)
	if n > max {
		r.err = storagebudget.ErrUnknown
		return ""
	}
	if !storageEnrollmentPeakQAllZero(p[n:]) || !utf8.Valid(p[:n]) {
		r.err = storagebudget.ErrUnknown
	}
	return string(p[:n])
}
func (r *storageEnrollmentPeakQReader) variable(max int) string {
	n := int(r.u16())
	if n > max {
		r.err = storagebudget.ErrUnknown
		return ""
	}
	p := r.take(n)
	if !utf8.Valid(p) {
		r.err = storagebudget.ErrUnknown
	}
	return string(p)
}
func (r *storageEnrollmentPeakQReader) end() error {
	if !storageEnrollmentPeakQAllZero(r.b[r.n:]) {
		r.err = storagebudget.ErrUnknown
	}
	return r.err
}
func storageEnrollmentPeakQAllZero(b []byte) bool {
	return storageAllZero(b)
}

func storageEnrollmentPeakQSeal(b []byte, kind byte, index uint16, generation uint64, a storageEnrollmentPeakQHeldAnchor) {
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
func storageEnrollmentPeakQUnseal(b []byte, kind byte, index uint16, a storageEnrollmentPeakQHeldAnchor) (uint64, error) {
	return storageEnrollmentPeakQUnsealVariant(b, kind, index, a, false)
}
func storageEnrollmentPeakQUnsealVariant(b []byte, kind byte, index uint16, a storageEnrollmentPeakQHeldAnchor, fanout bool) (uint64, error) {
	if len(b) < storageEnrollmentPeakQPageHeader || string(b[:8]) != "JPSTB002" || b[8] != kind || b[9] != 0 && (!fanout || b[9] != 1) || binary.LittleEndian.Uint16(b[10:12]) != index || !storageEnrollmentPeakQAllZero(b[12:16]) || !storageEnrollmentPeakQAllZero(b[104:128]) || !bytes.Equal(b[24:40], a.Installation[:]) || !bytes.Equal(b[40:72], a.Binding[:]) {
		return 0, storagebudget.ErrUnknown
	}
	var old [32]byte
	copy(old[:], b[72:104])
	clear(b[72:104])
	d := sha256.Sum256(b)
	copy(b[72:104], old[:])
	if d != old {
		return 0, storagebudget.ErrUnknown
	}
	return binary.LittleEndian.Uint64(b[16:24]), nil
}
func storageEnrollmentPeakQEncodeCheckpoint(c *storageEnrollmentPeakQCheckpoint, a storageEnrollmentPeakQHeldAnchor) ([]byte, error) {
	if c == nil || storageEnrollmentPeakQValidateCheckpoint(c) != nil {
		return nil, storagebudget.ErrBounds
	}
	b := make([]byte, storageEnrollmentPeakQCheckpointBytes)
	w := storageEnrollmentPeakQWriter{b: b[storageEnrollmentPeakQPageHeader : 16<<10]}
	w.u64(c.Epoch)
	if c.Maintenance {
		w.u8(1)
	} else {
		w.u8(0)
	}
	w.usage(c.Installation)
	p := storageEnrollmentPeakQWriter{b: b[16<<10 : storageEnrollmentPeakQRootOffset]}
	p.limits(c.Policy.Installation)
	p.limits(c.Policy.Workspace)
	p.u64(uint64(c.Policy.ObservationMaxAge))
	if w.err != nil || p.err != nil {
		return nil, storagebudget.ErrBounds
	}
	for i, v := range c.Roots {
		if v.ID.zero() {
			continue
		}
		w = storageEnrollmentPeakQWriter{b: b[storageEnrollmentPeakQRootOffset+i*768 : storageEnrollmentPeakQRootOffset+(i+1)*768]}
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
		w = storageEnrollmentPeakQWriter{b: b[storageEnrollmentPeakQWorkspaceOffset+i*512 : storageEnrollmentPeakQWorkspaceOffset+(i+1)*512]}
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
		w = storageEnrollmentPeakQWriter{b: b[storageEnrollmentPeakQFilesystemOffset+i*256 : storageEnrollmentPeakQFilesystemOffset+(i+1)*256]}
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
		w = storageEnrollmentPeakQWriter{b: b[storageEnrollmentPeakQAppliedOffset+i*128 : storageEnrollmentPeakQAppliedOffset+(i+1)*128]}
		w.id(v.ID)
		w.u64(v.Sequence)
		w.u64(v.Generation)
		w.u64(v.NextSequence)
	}
	for i, v := range c.CapacityConstraints {
		if v.ID.zero() {
			continue
		}
		w = storageEnrollmentPeakQWriter{b: b[storageEnrollmentPeakQCapacityOffset+i*128 : storageEnrollmentPeakQCapacityOffset+(i+1)*128]}
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
	storageEnrollmentPeakQSeal(b, 1, 0, c.Generation, a)
	return b, nil
}
func storageEnrollmentPeakQDecodeCheckpoint(b []byte, a storageEnrollmentPeakQHeldAnchor) (*storageEnrollmentPeakQCheckpoint, error) {
	if len(b) != storageEnrollmentPeakQCheckpointBytes {
		return nil, storagebudget.ErrUnknown
	}
	g, e := storageEnrollmentPeakQUnseal(b, 1, 0, a)
	if e != nil {
		return nil, e
	}
	c := new(storageEnrollmentPeakQCheckpoint)
	c.Generation = g
	r := storageEnrollmentPeakQReader{b: b[storageEnrollmentPeakQPageHeader : 16<<10]}
	c.Epoch = r.u64()
	m := r.u8()
	c.Maintenance = m == 1
	c.Installation = r.usage()
	if m > 1 || r.end() != nil {
		return nil, storagebudget.ErrUnknown
	}
	r = storageEnrollmentPeakQReader{b: b[16<<10 : storageEnrollmentPeakQRootOffset]}
	c.Policy.Installation = r.limits()
	c.Policy.Workspace = r.limits()
	age := r.u64()
	if age > uint64(1<<63-1) {
		return nil, storagebudget.ErrUnknown
	}
	c.Policy.ObservationMaxAge = time.Duration(age)
	if r.end() != nil || !c.Policy.valid() {
		return nil, storagebudget.ErrUnknown
	}
	for i := range c.Roots {
		row := b[storageEnrollmentPeakQRootOffset+i*768 : storageEnrollmentPeakQRootOffset+(i+1)*768]
		if storageEnrollmentPeakQAllZero(row) {
			continue
		}
		r = storageEnrollmentPeakQReader{b: row}
		v := storageEnrollmentPeakQRoot{ID: r.id(), Owner: r.digest(), Identity: r.digest(), Filesystem: r.id(), Workspace: r.id(), Generation: r.u64()}
		m = r.u8()
		v.Known = m == 1
		v.Locator = r.text(256)
		v.Usage = r.usage()
		v.CapacityCount = r.u8()
		for j := range v.CapacityRefs {
			v.CapacityRefs[j] = storageEnrollmentPeakQCapacityRef{r.id(), r.u64()}
		}
		v.CapacityBinding = r.digest()
		if m > 1 || r.end() != nil {
			return nil, storagebudget.ErrUnknown
		}
		c.Roots[i] = v
	}
	for i := range c.Workspaces {
		row := b[storageEnrollmentPeakQWorkspaceOffset+i*512 : storageEnrollmentPeakQWorkspaceOffset+(i+1)*512]
		if storageEnrollmentPeakQAllZero(row) {
			continue
		}
		r = storageEnrollmentPeakQReader{b: row}
		v := storageEnrollmentPeakQWorkspace{ID: r.id(), Generation: r.u64()}
		m = r.u8()
		v.Known = m == 1
		v.Usage = r.usage()
		v.Limits = r.limits()
		if m > 1 || r.end() != nil {
			return nil, storagebudget.ErrUnknown
		}
		c.Workspaces[i] = v
	}
	for i := range c.Filesystems {
		row := b[storageEnrollmentPeakQFilesystemOffset+i*256 : storageEnrollmentPeakQFilesystemOffset+(i+1)*256]
		if storageEnrollmentPeakQAllZero(row) {
			continue
		}
		r = storageEnrollmentPeakQReader{b: row}
		v := storageEnrollmentPeakQFilesystem{ID: r.id(), Identity: r.digest(), Generation: r.u64(), InodeModel: r.u8(), CapabilityEvidence: r.digest(), ByteConstraint: r.id(), InodeConstraint: r.id()}
		if r.end() != nil {
			return nil, storagebudget.ErrUnknown
		}
		c.Filesystems[i] = v
	}
	for i := range c.Applied {
		r = storageEnrollmentPeakQReader{b: b[storageEnrollmentPeakQAppliedOffset+i*128 : storageEnrollmentPeakQAppliedOffset+(i+1)*128]}
		c.Applied[i] = storageEnrollmentPeakQApplied{ID: r.id(), Sequence: r.u64(), Generation: r.u64(), NextSequence: r.u64()}
		if r.end() != nil {
			return nil, storagebudget.ErrUnknown
		}
	}
	for i := range c.CapacityConstraints {
		row := b[storageEnrollmentPeakQCapacityOffset+i*128 : storageEnrollmentPeakQCapacityOffset+(i+1)*128]
		if storageEnrollmentPeakQAllZero(row) {
			continue
		}
		r = storageEnrollmentPeakQReader{b: row}
		c.CapacityConstraints[i] = storageEnrollmentPeakQCapacityConstraint{ID: r.id(), Identity: r.digest(), Generation: r.u64(), Resources: r.u8(), Headroom: storageEnrollmentPeakQCapacityVector{r.u64(), r.u64()}}
		if r.end() != nil {
			return nil, storagebudget.ErrUnknown
		}
	}
	if !storageEnrollmentPeakQAllZero(b[storageEnrollmentPeakQCapacityOffset+storageEnrollmentPeakQMaxCapacityConstraints*128:]) || storageEnrollmentPeakQValidateCheckpoint(c) != nil {
		return nil, storagebudget.ErrUnknown
	}
	return c, nil
}

func storageEnrollmentPeakQEncodeTicket(t storageEnrollmentPeakQTicket, a storageEnrollmentPeakQHeldAnchor) ([]byte, error) {
	if len(t.Components) > storageEnrollmentPeakQMaxComponents || len(t.Changes) > storageEnrollmentPeakQMaxComponents {
		return nil, storagebudget.ErrBounds
	}
	if e := storageEnrollmentPeakQValidateTicketPage(t); e != nil {
		return nil, e
	}
	if t.Fanout != nil && len(t.Fanout.Memberships) != 0 {
		if err := storageEnrollmentPeakQValidateFanout(t); err != nil {
			return nil, err
		}
	}
	b := make([]byte, storageEnrollmentPeakQTicketBytes)
	w := storageEnrollmentPeakQWriter{b: b[storageEnrollmentPeakQPageHeader:1024]}
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
		w = storageEnrollmentPeakQWriter{b: b[1024+i*512 : 1024+(i+1)*512]}
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
	storageEnrollmentPeakQSeal(b, 2, uint16(t.Token.Slot), t.Token.Generation, a)
	return b, nil
}
func storageEnrollmentPeakQDecodeTicket(b []byte, slot int, a storageEnrollmentPeakQHeldAnchor) (storageEnrollmentPeakQTicket, error) {
	var t storageEnrollmentPeakQTicket
	if len(b) != storageEnrollmentPeakQTicketBytes {
		return t, storagebudget.ErrUnknown
	}
	g, e := storageEnrollmentPeakQUnsealVariant(b, 2, uint16(slot), a, true)
	if e != nil {
		return t, e
	}
	r := storageEnrollmentPeakQReader{b: b[storageEnrollmentPeakQPageHeader:1024]}
	if b[9] == 1 {
		t.Fanout = &storageEnrollmentPeakQWorkspaceFanout{}
	}
	t.Token = storageEnrollmentPeakQToken{Slot: uint8(slot), ID: r.id(), Sequence: r.u64(), Generation: g}
	t.State = storageEnrollmentPeakQState(r.u8())
	t.PolicyRevision = r.u64()
	t.Workflow = r.text(128)
	t.Operation = r.text(128)
	t.Owner = r.digest()
	t.Manifest = r.digest()
	t.ManifestLength = r.u32()
	t.Evidence = r.digest()
	n, k := int(r.u8()), int(r.u8())
	if n > storageEnrollmentPeakQMaxComponents || k > n || r.end() != nil {
		return t, storagebudget.ErrUnknown
	}
	for i := 0; i < n; i++ {
		r = storageEnrollmentPeakQReader{b: b[1024+i*512 : 1024+(i+1)*512]}
		c := storageEnrollmentPeakQComponent{Root: r.id(), RootGeneration: r.u64(), Owner: r.digest(), Filesystem: r.id(), FilesystemGeneration: r.u64(), Workspace: r.id(), WorkspaceGeneration: r.u64(), Category: r.u8(), Physical: r.vector(), Attribution: r.vector(), SettlementPhysical: r.vector(), SettlementAttribution: r.vector()}
		if i < k {
			t.Changes = append(t.Changes, storageEnrollmentPeakQChange{r.vector(), r.vector(), r.vector(), r.vector()})
		}
		if i >= k && !storageEnrollmentPeakQAllZero(r.take(96)) {
			return t, storagebudget.ErrUnknown
		}
		c.CapacityCount = r.u8()
		for j := range c.Capacity {
			c.Capacity[j] = storageEnrollmentPeakQCapacityPromise{Constraint: r.id(), Generation: r.u64(), Peak: storageEnrollmentPeakQCapacityVector{r.u64(), r.u64()}}
		}
		c.CapacityBinding = r.digest()
		t.Components = append(t.Components, c)
		if r.end() != nil {
			return t, storagebudget.ErrUnknown
		}
	}
	if !storageEnrollmentPeakQAllZero(b[1024+n*512:]) || storageEnrollmentPeakQValidateTicketPage(t) != nil {
		return t, storagebudget.ErrUnknown
	}
	return t, nil
}

func (v storageEnrollmentPeakQCapacityVector) valid() bool {
	return v.Bytes <= storageEnrollmentPeakQMaxBytes && v.Inodes <= storageEnrollmentPeakQMaxCount
}
func (v storageEnrollmentPeakQCapacityVector) zero() bool {
	return v == (storageEnrollmentPeakQCapacityVector{})
}
func (v storageEnrollmentPeakQCapacityVector) le(w storageEnrollmentPeakQCapacityVector) bool {
	return v.Bytes <= w.Bytes && v.Inodes <= w.Inodes
}
func storageEnrollmentPeakQCapacityAdd(a, b storageEnrollmentPeakQCapacityVector) (storageEnrollmentPeakQCapacityVector, error) {
	if !a.valid() || !b.valid() || a.Bytes > storageEnrollmentPeakQMaxBytes-b.Bytes || a.Inodes > storageEnrollmentPeakQMaxCount-b.Inodes {
		return storageEnrollmentPeakQCapacityVector{}, storagebudget.ErrBounds
	}
	return storageEnrollmentPeakQCapacityVector{a.Bytes + b.Bytes, a.Inodes + b.Inodes}, nil
}
func storageEnrollmentPeakQCapacityMasked(v storageEnrollmentPeakQCapacityVector, resources uint8) bool {
	return v.valid() && resources != 0 && resources & ^uint8(storageEnrollmentPeakQCapacityBytes|storageEnrollmentPeakQCapacityInodes) == 0 && (resources&storageEnrollmentPeakQCapacityBytes != 0 || v.Bytes == 0) && (resources&storageEnrollmentPeakQCapacityInodes != 0 || v.Inodes == 0)
}
func storageEnrollmentPeakQValidConstraint(v storageEnrollmentPeakQCapacityConstraint) bool {
	return !v.ID.zero() && storageEnrollmentPeakQAuthority(v.Identity) && v.Generation != 0 && storageEnrollmentPeakQCapacityMasked(v.Headroom, v.Resources)
}
func storageEnrollmentPeakQConstraintIndex(c *storageEnrollmentPeakQCheckpoint, id storageEnrollmentPeakQID) int {
	// Index-only range avoids copying this complete fixed array per lookup.
	for i := range c.CapacityConstraints {
		if !id.zero() && c.CapacityConstraints[i].ID == id {
			return i
		}
	}
	return -1
}
func storageEnrollmentPeakQFilesystemCapacityValid(f storageEnrollmentPeakQFilesystem, constraints map[storageEnrollmentPeakQID]storageEnrollmentPeakQCapacityConstraint) bool {
	b, ok := constraints[f.ByteConstraint]
	if f.ID.zero() || !storageEnrollmentPeakQAuthority(f.Identity) || f.Generation == 0 || !storageEnrollmentPeakQAuthority(f.CapabilityEvidence) || !ok || b.Resources&storageEnrollmentPeakQCapacityBytes == 0 {
		return false
	}
	switch f.InodeModel {
	case storageEnrollmentPeakQInodesFinite:
		n, ok := constraints[f.InodeConstraint]
		return ok && n.Resources&storageEnrollmentPeakQCapacityInodes != 0
	case storageEnrollmentPeakQInodesDynamic:
		return f.InodeConstraint.zero()
	default:
		return false
	}
}

// CapacityBindingFor binds the declared immutable physical-policy domain. It is
// a canonical metadata digest, not custody proof, complete quota discovery or an
// allocation guarantee. The original owner verifies the applicable declared
// descriptor/native relations; explicitly unknown discovery stays separate.
func storageEnrollmentPeakQCapacityBindingFor(root storageEnrollmentPeakQRoot, f storageEnrollmentPeakQFilesystem, constraints []storageEnrollmentPeakQCapacityConstraint) (storageEnrollmentPeakQDigest, error) {
	if len(constraints) > storageEnrollmentPeakQMaxCapacityConstraints || root.CapacityCount > storageEnrollmentPeakQMaxCapacityRefs {
		return storageEnrollmentPeakQDigest{}, storagebudget.ErrBounds
	}
	if root.ID.zero() || !storageEnrollmentPeakQAuthority(root.Owner) || !storageEnrollmentPeakQAuthority(root.Identity) || root.Generation == 0 || root.Filesystem != f.ID || root.CapacityCount == 0 {
		return storageEnrollmentPeakQDigest{}, storagebudget.ErrAuthority
	}
	rows := make(map[storageEnrollmentPeakQID]storageEnrollmentPeakQCapacityConstraint, len(constraints))
	identities := make(map[storageEnrollmentPeakQDigest]bool, len(constraints))
	for _, v := range constraints {
		if !storageEnrollmentPeakQValidConstraint(v) || identities[v.Identity] {
			return storageEnrollmentPeakQDigest{}, storagebudget.ErrAuthority
		}
		if _, exists := rows[v.ID]; exists {
			return storageEnrollmentPeakQDigest{}, storagebudget.ErrAuthority
		}
		rows[v.ID] = v
		identities[v.Identity] = true
	}
	return storageEnrollmentPeakQCapacityBinding(root, f, rows)
}

func storageEnrollmentPeakQCapacityBinding(root storageEnrollmentPeakQRoot, f storageEnrollmentPeakQFilesystem, rows map[storageEnrollmentPeakQID]storageEnrollmentPeakQCapacityConstraint) (storageEnrollmentPeakQDigest, error) {
	if root.ID.zero() || !storageEnrollmentPeakQAuthority(root.Owner) || !storageEnrollmentPeakQAuthority(root.Identity) || root.Generation == 0 || root.Filesystem != f.ID || root.CapacityCount == 0 || root.CapacityCount > storageEnrollmentPeakQMaxCapacityRefs {
		return storageEnrollmentPeakQDigest{}, storagebudget.ErrAuthority
	}
	if !storageEnrollmentPeakQFilesystemCapacityValid(f, rows) {
		return storageEnrollmentPeakQDigest{}, storagebudget.ErrAuthority
	}
	w := storageEnrollmentPeakQWriter{b: make([]byte, 512)}
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
	seenByte, seenInode := false, f.InodeModel == storageEnrollmentPeakQInodesDynamic
	for i, ref := range root.CapacityRefs {
		if i >= int(root.CapacityCount) {
			if ref != (storageEnrollmentPeakQCapacityRef{}) {
				return storageEnrollmentPeakQDigest{}, storagebudget.ErrBounds
			}
			continue
		}
		v, ok := rows[ref.Constraint]
		if !ok || ref.Generation != v.Generation || i > 0 && bytes.Compare(root.CapacityRefs[i-1].Constraint[:], ref.Constraint[:]) >= 0 {
			return storageEnrollmentPeakQDigest{}, storagebudget.ErrAuthority
		}
		seenByte = seenByte || ref.Constraint == f.ByteConstraint
		seenInode = seenInode || ref.Constraint == f.InodeConstraint
		w.id(v.ID)
		w.digest(v.Identity)
		w.u64(v.Generation)
		w.u8(v.Resources)
	}
	if !seenByte || !seenInode || w.err != nil {
		return storageEnrollmentPeakQDigest{}, storagebudget.ErrAuthority
	}
	return sha256.Sum256(w.b[:w.n]), nil
}

func storageEnrollmentPeakQValidateCheckpoint(c *storageEnrollmentPeakQCheckpoint) error {
	if c.Generation == 0 || !c.Policy.valid() {
		return storagebudget.ErrUnknown
	}
	roots := map[storageEnrollmentPeakQID]bool{}
	workspaces := map[storageEnrollmentPeakQID]bool{}
	filesystems := map[storageEnrollmentPeakQID]bool{}
	constraints := map[storageEnrollmentPeakQID]storageEnrollmentPeakQCapacityConstraint{}
	identities := map[storageEnrollmentPeakQDigest]bool{}
	for _, v := range c.CapacityConstraints {
		if v.ID.zero() {
			if v != (storageEnrollmentPeakQCapacityConstraint{}) {
				return storagebudget.ErrUnknown
			}
			continue
		}
		if !storageEnrollmentPeakQValidConstraint(v) || identities[v.Identity] {
			return storagebudget.ErrUnknown
		}
		if _, ok := constraints[v.ID]; ok {
			return storagebudget.ErrUnknown
		}
		constraints[v.ID] = v
		identities[v.Identity] = true
	}
	for _, v := range c.Workspaces {
		if v.ID.zero() {
			continue
		}
		if workspaces[v.ID] || v.Generation == 0 || !v.Limits.valid() {
			return storagebudget.ErrUnknown
		}
		workspaces[v.ID] = true
		if _, e := storageEnrollmentPeakQSum(v.Usage); e != nil {
			return e
		}
	}
	for _, v := range c.Filesystems {
		if v.ID.zero() {
			if v != (storageEnrollmentPeakQFilesystem{}) {
				return storagebudget.ErrUnknown
			}
			continue
		}
		if filesystems[v.ID] || !storageEnrollmentPeakQFilesystemCapacityValid(v, constraints) {
			return storagebudget.ErrUnknown
		}
		filesystems[v.ID] = true
	}
	var usage [storageEnrollmentPeakQCategories]storageEnrollmentPeakQVector
	for _, v := range c.Roots {
		if v.ID.zero() {
			if v != (storageEnrollmentPeakQRoot{}) {
				return storagebudget.ErrUnknown
			}
			continue
		}
		if roots[v.ID] || v.Generation == 0 || !storageEnrollmentPeakQAuthority(v.Owner) || !storageEnrollmentPeakQAuthority(v.Identity) || !storageEnrollmentPeakQTextValid(v.Locator, 256, false) || !filesystems[v.Filesystem] || !v.Workspace.zero() && !workspaces[v.Workspace] {
			return storagebudget.ErrUnknown
		}
		roots[v.ID] = true
		f := c.Filesystems[storageEnrollmentPeakQFilesystemIndex(c, v.Filesystem)]
		binding, e := storageEnrollmentPeakQCapacityBinding(v, f, constraints)
		if e != nil || binding != v.CapacityBinding {
			return storagebudget.ErrAuthority
		}
		// Physical identity duplication is forbidden. B2 must resolve overlap and
		// hardlinks with original ownership; separate ledgers are not double owners.
		for _, prior := range c.Roots {
			if prior.ID != v.ID && !prior.ID.zero() && prior.Identity == v.Identity {
				return storagebudget.ErrAuthority
			}
		}
		for k, x := range v.Usage {
			var e error
			usage[k], e = storageEnrollmentPeakQAdd(usage[k], x)
			if e != nil {
				return e
			}
		}
	}
	if usage != c.Installation {
		return storagebudget.ErrUnknown
	}
	for _, a := range c.Applied {
		if a.NextSequence == 0 || a.ID.zero() && (a.Sequence != 0 || a.Generation != 0) || !a.ID.zero() && (a.Sequence == 0 || a.Generation == 0 || a.NextSequence <= a.Sequence) {
			return storagebudget.ErrUnknown
		}
	}
	return nil
}
func storageEnrollmentPeakQValidateTicket(t storageEnrollmentPeakQTicket) error {
	if err := storageEnrollmentPeakQValidateTicketPage(t); err != nil {
		return err
	}
	return storageEnrollmentPeakQValidateFanout(t)
}
func storageEnrollmentPeakQValidateTicketPage(t storageEnrollmentPeakQTicket) error {
	if int(t.Token.Slot) >= storageEnrollmentPeakQSlots || t.State > storageEnrollmentPeakQTerminal || len(t.Components) > storageEnrollmentPeakQMaxComponents {
		return storagebudget.ErrUnknown
	}
	if t.State == storageEnrollmentPeakQEmpty {
		if t.Fanout != nil || !t.Token.ID.zero() || t.Token.Sequence != 0 || len(t.Components) != 0 || len(t.Changes) != 0 || t.Manifest != (storageEnrollmentPeakQDigest{}) || t.ManifestLength != 0 || t.Owner != (storageEnrollmentPeakQDigest{}) || t.Workflow != "" || t.Operation != "" || t.PolicyRevision != 0 || t.Evidence != (storageEnrollmentPeakQDigest{}) {
			return storagebudget.ErrUnknown
		}
		return nil
	}
	if t.Token.ID.zero() || t.Token.Sequence == 0 || t.Token.Generation == 0 || t.PolicyRevision == 0 || !storageEnrollmentPeakQAuthority(t.Owner) || !storageEnrollmentPeakQTextValid(t.Workflow, 128, false) || !storageEnrollmentPeakQTextValid(t.Operation, 128, false) || len(t.Components) == 0 {
		return storagebudget.ErrUnknown
	}
	if t.Manifest == (storageEnrollmentPeakQDigest{}) {
		if t.ManifestLength != 0 {
			return storagebudget.ErrUnknown
		}
	} else if t.ManifestLength != storageEnrollmentPeakQManifestBytes {
		return storagebudget.ErrUnknown
	}
	if t.State == storageEnrollmentPeakQTerminal {
		if len(t.Changes) != len(t.Components) || !storageEnrollmentPeakQAuthority(t.Evidence) {
			return storagebudget.ErrUnknown
		}
	} else if len(t.Changes) != 0 {
		return storagebudget.ErrUnknown
	}
	for i, v := range t.Components {
		if v.Root.zero() || v.RootGeneration == 0 || v.Filesystem.zero() || v.FilesystemGeneration == 0 || !storageEnrollmentPeakQAuthority(v.Owner) || v.Category >= storageEnrollmentPeakQCategories || t.Fanout == nil && v.Workspace.zero() && (!v.Attribution.zero() || !v.SettlementAttribution.zero()) || !v.Workspace.zero() && v.WorkspaceGeneration == 0 || t.Fanout != nil && (!v.Workspace.zero() || v.WorkspaceGeneration != 0) {
			return storagebudget.ErrUnknown
		}
		p, w, e := v.total()
		if e != nil {
			return e
		}
		if p.zero() && w.zero() {
			return storagebudget.ErrBounds
		}
		if v.CapacityCount > storageEnrollmentPeakQMaxCapacityRefs {
			return storagebudget.ErrBounds
		}
		if v.CapacityCount == 0 || !storageEnrollmentPeakQAuthority(v.CapacityBinding) {
			return storagebudget.ErrAuthority
		}
		for j, ref := range v.Capacity {
			if j >= int(v.CapacityCount) {
				if ref != (storageEnrollmentPeakQCapacityPromise{}) {
					return storagebudget.ErrBounds
				}
				continue
			}
			if ref.Constraint.zero() || ref.Generation == 0 || !ref.Peak.valid() || p.zero() && !ref.Peak.zero() || j > 0 && bytes.Compare(v.Capacity[j-1].Constraint[:], ref.Constraint[:]) >= 0 {
				return storagebudget.ErrBounds
			}
		}
		for j := 0; j < i; j++ {
			q := t.Components[j]
			if t.Fanout == nil && v.Root == q.Root && v.Workspace == q.Workspace && v.Category == q.Category {
				return storagebudget.ErrBounds
			}
		}
		if t.State == storageEnrollmentPeakQTerminal {
			x := t.Changes[i]
			if !x.PhysicalAdd.le(p) || !x.AttributionAdd.le(w) || !x.PhysicalRemove.valid() || !x.AttributionRemove.valid() {
				return storagebudget.ErrBounds
			}
		}
	}
	return nil
}
func storageEnrollmentPeakQRootIndex(c *storageEnrollmentPeakQCheckpoint, id storageEnrollmentPeakQID) int {
	// Index-only range avoids copying this complete fixed array per lookup.
	for i := range c.Roots {
		if !id.zero() && c.Roots[i].ID == id {
			return i
		}
	}
	return -1
}
func storageEnrollmentPeakQWorkspaceIndex(c *storageEnrollmentPeakQCheckpoint, id storageEnrollmentPeakQID) int {
	// Index-only range avoids copying this complete fixed array per lookup.
	for i := range c.Workspaces {
		if !id.zero() && c.Workspaces[i].ID == id {
			return i
		}
	}
	return -1
}
func storageEnrollmentPeakQFilesystemIndex(c *storageEnrollmentPeakQCheckpoint, id storageEnrollmentPeakQID) int {
	// Index-only range avoids copying this complete fixed array per lookup.
	for i := range c.Filesystems {
		if !id.zero() && c.Filesystems[i].ID == id {
			return i
		}
	}
	return -1
}
func storageEnrollmentPeakQRegistered(c *storageEnrollmentPeakQCheckpoint, t storageEnrollmentPeakQTicket, requireKnown bool) error {
	if t.Fanout != nil && len(t.Fanout.Memberships) == 0 {
		if int(t.Token.Slot) >= len(c.Applied) || !storageEnrollmentPeakQRetired(t, c.Applied[t.Token.Slot]) || storageEnrollmentPeakQValidateTicketPage(t) != nil {
			return storagebudget.ErrBounds
		}
		// Exact-applied pages have no live membership/capacity obligation.
		// Verify their physical definitions without inventing workspace rows.
		t.Fanout = nil
		t.Components = append([]storageEnrollmentPeakQComponent(nil), t.Components...)
		for i := range t.Components {
			t.Components[i].Attribution, t.Components[i].SettlementAttribution = storageEnrollmentPeakQVector{}, storageEnrollmentPeakQVector{}
		}
	}
	if err := storageEnrollmentPeakQValidateFanout(t); err != nil {
		return err
	}
	return storageEnrollmentPeakQWalkComponents(t, func(_ int, v storageEnrollmentPeakQComponent, _ bool) error {
		r := storageEnrollmentPeakQRootIndex(c, v.Root)
		if r < 0 {
			return storagebudget.ErrAuthority
		}
		root := c.Roots[r]
		if root.Generation != v.RootGeneration || root.Owner != v.Owner {
			return storagebudget.ErrAuthority
		}
		if requireKnown && !root.Known {
			return storagebudget.ErrUnknown
		}
		if !v.Workspace.zero() {
			w := storageEnrollmentPeakQWorkspaceIndex(c, v.Workspace)
			if w < 0 || c.Workspaces[w].Generation != v.WorkspaceGeneration {
				return storagebudget.ErrStale
			}
			if requireKnown && !c.Workspaces[w].Known {
				return storagebudget.ErrUnknown
			}
		}
		f := storageEnrollmentPeakQFilesystemIndex(c, root.Filesystem)
		if f < 0 || root.Filesystem != v.Filesystem || c.Filesystems[f].Generation != v.FilesystemGeneration {
			return storagebudget.ErrUnknown
		}
		if root.CapacityCount != v.CapacityCount || root.CapacityBinding != v.CapacityBinding {
			return storagebudget.ErrAuthority
		}
		p, _, e := v.total()
		if e != nil {
			return e
		}
		n, e := storageEnrollmentPeakQAdd(storageEnrollmentPeakQVector{Files: p.Files}, storageEnrollmentPeakQVector{Files: p.Directories})
		if e != nil {
			return e
		}
		for j := 0; j < int(v.CapacityCount); j++ {
			ref := v.Capacity[j]
			if root.CapacityRefs[j] != (storageEnrollmentPeakQCapacityRef{ref.Constraint, ref.Generation}) {
				return storagebudget.ErrAuthority
			}
			k := storageEnrollmentPeakQConstraintIndex(c, ref.Constraint)
			if k < 0 {
				return storagebudget.ErrAuthority
			}
			row := c.CapacityConstraints[k]
			if ref.Generation != row.Generation || !storageEnrollmentPeakQCapacityMasked(ref.Peak, row.Resources) {
				return storagebudget.ErrAuthority
			}
			if row.Resources&storageEnrollmentPeakQCapacityBytes != 0 && (ref.Peak.Bytes < p.Bytes || n.Files > 0 && ref.Peak.Bytes == 0) || row.Resources&storageEnrollmentPeakQCapacityInodes != 0 && ref.Peak.Inodes < n.Files {
				return storagebudget.ErrBounds
			}
		}
		return nil
	})
}
func storageEnrollmentPeakQRetired(t storageEnrollmentPeakQTicket, a storageEnrollmentPeakQApplied) bool {
	return t.State == storageEnrollmentPeakQTerminal && t.Token.ID == a.ID && t.Token.Sequence == a.Sequence && t.Token.Generation == a.Generation
}
func storageEnrollmentPeakQTerminalApply(c *storageEnrollmentPeakQCheckpoint, t storageEnrollmentPeakQTicket) error {
	if t.State != storageEnrollmentPeakQTerminal {
		return nil
	}
	a := c.Applied[t.Token.Slot]
	if storageEnrollmentPeakQRetired(t, a) {
		return nil
	}
	if t.Token.Sequence < a.NextSequence {
		return storagebudget.ErrUnknown
	}
	if e := storageEnrollmentPeakQRegistered(c, t, false); e != nil {
		return e
	}
	if err := storageEnrollmentPeakQWalkComponents(t, func(i int, v storageEnrollmentPeakQComponent, physical bool) error {
		x := t.Changes[i]
		if !physical {
			x.PhysicalAdd, x.PhysicalRemove = storageEnrollmentPeakQVector{}, storageEnrollmentPeakQVector{}
		}
		r := storageEnrollmentPeakQRootIndex(c, v.Root)
		n, e := storageEnrollmentPeakQAdd(c.Roots[r].Usage[v.Category], x.PhysicalAdd)
		if e == nil {
			n, e = storageEnrollmentPeakQSub(n, x.PhysicalRemove)
		}
		if e != nil {
			return e
		}
		c.Roots[r].Usage[v.Category] = n
		n, e = storageEnrollmentPeakQAdd(c.Installation[v.Category], x.PhysicalAdd)
		if e == nil {
			n, e = storageEnrollmentPeakQSub(n, x.PhysicalRemove)
		}
		if e != nil {
			return e
		}
		c.Installation[v.Category] = n
		if !v.Workspace.zero() {
			w := storageEnrollmentPeakQWorkspaceIndex(c, v.Workspace)
			n, e = storageEnrollmentPeakQAdd(c.Workspaces[w].Usage[v.Category], x.AttributionAdd)
			if e == nil {
				n, e = storageEnrollmentPeakQSub(n, x.AttributionRemove)
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
	n, e := storageEnrollmentPeakQNext(t.Token.Sequence)
	if e != nil {
		return e
	}
	c.Applied[t.Token.Slot] = storageEnrollmentPeakQApplied{t.Token.ID, t.Token.Sequence, t.Token.Generation, n}
	return nil
}
func storageEnrollmentPeakQLimitsCheck(used, promised [storageEnrollmentPeakQCategories]storageEnrollmentPeakQVector, l storageEnrollmentPeakQLimits) error {
	for i, u := range used {
		v, e := storageEnrollmentPeakQAdd(u, promised[i])
		if e != nil {
			return e
		}
		if !v.le(l.Category[i]) {
			return storagebudget.ErrLimit
		}
	}
	u, e := storageEnrollmentPeakQSum(used)
	if e != nil {
		return e
	}
	p, e := storageEnrollmentPeakQSum(promised)
	if e != nil {
		return e
	}
	v, e := storageEnrollmentPeakQAdd(u, p)
	if e != nil {
		return e
	}
	if !v.le(l.Total) {
		return storagebudget.ErrLimit
	}
	return nil
}

func storageEnrollmentPeakQPublishCensus(c *storageEnrollmentPeakQCheckpoint, f storageEnrollmentPeakQCensus) error {
	if !storageEnrollmentPeakQAuthority(f.Evidence) {
		return storagebudget.ErrAuthority
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
		return storagebudget.ErrBounds
	}
	seen := map[storageEnrollmentPeakQID]bool{}
	c.Installation = [storageEnrollmentPeakQCategories]storageEnrollmentPeakQVector{}
	for _, m := range f.Roots {
		i := storageEnrollmentPeakQRootIndex(c, m.ID)
		if i < 0 || seen[m.ID] || m.Generation != c.Roots[i].Generation {
			return storagebudget.ErrStale
		}
		seen[m.ID] = true
		if _, e := storageEnrollmentPeakQSum(m.Usage); e != nil {
			return e
		}
		c.Roots[i].Usage = m.Usage
		c.Roots[i].Known = true
		for k, v := range m.Usage {
			var e error
			c.Installation[k], e = storageEnrollmentPeakQAdd(c.Installation[k], v)
			if e != nil {
				return e
			}
		}
	}
	seen = map[storageEnrollmentPeakQID]bool{}
	for _, m := range f.Workspaces {
		i := storageEnrollmentPeakQWorkspaceIndex(c, m.ID)
		if i < 0 || seen[m.ID] || m.Generation != c.Workspaces[i].Generation {
			return storagebudget.ErrStale
		}
		seen[m.ID] = true
		if _, e := storageEnrollmentPeakQSum(m.Usage); e != nil {
			return e
		}
		c.Workspaces[i].Usage = m.Usage
		c.Workspaces[i].Known = true
	}
	return nil
}
