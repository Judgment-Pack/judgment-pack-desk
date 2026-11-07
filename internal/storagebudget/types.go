// Package storagebudget coordinates cooperative storage promises. It does not
// grant action permission, execute effects, enforce an opaque child's writes,
// discover roots, delete data, or promise physical allocation. Original owners
// must enroll every writer and enforce each admitted byte and namespace bound.
package storagebudget

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"os"
	"time"
)

const (
	Categories             = 9
	MaxRoots               = 4096
	MaxWorkspaces          = 1024
	MaxFilesystems         = 128
	MaxCapacityConstraints = 512
	MaxCapacityRefs        = 4
	Slots                  = 64
	MaxComponents          = 16
	MaxEntries             = 4096
	CheckpointBytes        = 4 << 20
	TicketBytes            = 16 << 10
	ManifestBytes          = 512 << 10
	CoordinatorBytes       = 42 << 20
	MetadataBudget         = 64 << 20
	maxBytes               = uint64(1 << 60)
	maxCount               = uint64(1 << 40)
	pageHeader             = 128
	rootOffset             = 32 << 10
	workspaceOffset        = rootOffset + MaxRoots*768
	filesystemOffset       = workspaceOffset + MaxWorkspaces*512
	appliedOffset          = filesystemOffset + MaxFilesystems*256
	capacityOffset         = appliedOffset + Slots*128
)

// Categories are fixed format dimensions, not independently additive grants.
const (
	History uint8 = iota
	Retained
	Attachments
	Memory
	Control
	Disposable
	Binaries
	Transient
	Settlement
)

var (
	ErrUnknown     = errors.New("storage measurement or durable outcome unknown")
	ErrBusy        = errors.New("storage coordinator busy")
	ErrLimit       = errors.New("storage quota exceeded")
	ErrHeadroom    = errors.New("storage physical headroom insufficient")
	ErrBounds      = errors.New("storage format or finite capacity exceeded")
	ErrStale       = errors.New("storage generation changed")
	ErrUnsupported = errors.New("storage platform primitive unavailable")
	ErrAuthority   = errors.New("storage original owner binding invalid")
)

type ID [16]byte
type Digest [32]byte

func NewID() (id ID, err error) { _, err = rand.Read(id[:]); return }
func (id ID) zero() bool        { return id == ID{} }

// Vector counts logical file lengths and namespace objects. Inodes are checked
// separately against Files+Directories. Zero-byte files still consume capacity.
type Vector struct{ Bytes, Files, Directories uint64 }

func (v Vector) valid() bool {
	return v.Bytes <= maxBytes && v.Files <= maxCount && v.Directories <= maxCount
}
func (v Vector) zero() bool { return v == Vector{} }
func (v Vector) le(w Vector) bool {
	return v.Bytes <= w.Bytes && v.Files <= w.Files && v.Directories <= w.Directories
}
func add(a, b Vector) (Vector, error) {
	if !a.valid() || !b.valid() || a.Bytes > maxBytes-b.Bytes || a.Files > maxCount-b.Files || a.Directories > maxCount-b.Directories {
		return Vector{}, ErrBounds
	}
	return Vector{a.Bytes + b.Bytes, a.Files + b.Files, a.Directories + b.Directories}, nil
}
func sub(a, b Vector) (Vector, error) {
	if !b.le(a) {
		return Vector{}, ErrUnknown
	}
	return Vector{a.Bytes - b.Bytes, a.Files - b.Files, a.Directories - b.Directories}, nil
}
func sum(v [Categories]Vector) (out Vector, err error) {
	for _, x := range v {
		out, err = add(out, x)
		if err != nil {
			return
		}
	}
	return
}
func next(n uint64) (uint64, error) {
	if n == math.MaxUint64 {
		return 0, ErrBounds
	}
	return n + 1, nil
}

type Limits struct {
	Total    Vector
	Category [Categories]Vector
}

func (l Limits) valid() bool {
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

type Policy struct {
	Installation, Workspace Limits
	ObservationMaxAge       time.Duration
}

func (p Policy) valid() bool {
	return p.Installation.valid() && p.Workspace.valid() && p.ObservationMaxAge > 0 && p.ObservationMaxAge <= time.Hour &&
		(Vector{MetadataBudget, 16, 1}).le(p.Installation.Total) && (Vector{MetadataBudget, 16, 1}).le(p.Installation.Category[Control])
}

// HeldAnchor is supplied by original custody, never by a browser path. Root and
// LockWitness are borrowed. OpenHeld opens an independent lock description and
// verifies the witness names that same private stable inode. Binding identifies
// original installation custody; it is not an owner marker minted here.
//
// AdvisoryLimits makes installation and workspace limits advisory: Reserve
// still charges every promise and Snapshot still reports usage against the
// limits, but a promise that reaches or exceeds a limit is admitted rather
// than refused with ErrLimit. Capacity headroom (ErrHeadroom), bounds,
// authority and every other refusal are unchanged.
type HeldAnchor struct {
	Root           *os.Root
	LockWitness    *os.File
	Installation   ID
	Binding        Digest
	AdvisoryLimits bool
}

type Root struct {
	ID                    ID
	Owner, Identity       Digest
	Filesystem, Workspace ID
	Generation            uint64
	Locator               string // bounded opaque owner recovery locator; never followed here
	Known                 bool
	Usage                 [Categories]Vector
	CapacityCount         uint8
	CapacityRefs          [MaxCapacityRefs]CapacityRef
	CapacityBinding       Digest
}
type Workspace struct {
	ID         ID
	Generation uint64
	Known      bool
	Usage      [Categories]Vector
	Limits     Limits
}
type Filesystem struct {
	ID                              ID
	Identity                        Digest
	Generation                      uint64
	InodeModel                      uint8
	CapabilityEvidence              Digest
	ByteConstraint, InodeConstraint ID
}

const (
	CapacityBytes  uint8 = 1
	CapacityInodes uint8 = 2
	InodesUnknown  uint8 = 0
	InodesFinite   uint8 = 1
	InodesDynamic  uint8 = 2
)

// CapacityVector is durable physical-policy demand, never retained logical
// usage or a universal allocation guarantee. Its numeric axes may be authored
// conservative estimates under a versioned installation policy. A documented
// dynamic native inode model omits only an independent native pool; finite
// policy inode units, known object quotas and Desk namespace budgets still apply.
type CapacityVector struct{ Bytes, Inodes uint64 }
type CapacityRef struct {
	Constraint ID
	Generation uint64
}
type CapacityPromise struct {
	Constraint ID
	Generation uint64
	Peak       CapacityVector
}
type CapacityConstraint struct {
	ID         ID
	Identity   Digest
	Generation uint64
	Resources  uint8
	Headroom   CapacityVector
}

// CapacityObservation carries observed/policy counters. KnownResources means
// exactly the declared numeric axes are known, not native quota completeness.
type CapacityObservation struct {
	Constraint     ID
	Generation     uint64
	Available      CapacityVector
	At             time.Time
	KnownResources uint8
}
type Registration struct {
	Roots               []Root
	Workspaces          []Workspace
	Filesystems         []Filesystem
	CapacityConstraints []CapacityConstraint
}

// Observation is the legacy statistics-only ObserveHeld result. Reserve accepts
// CapacityObservation, not this caller-labeled sample. Known does not classify a
// filesystem's finite/dynamic inode capability or prove original pool custody.
type Observation struct {
	Filesystem                      ID
	Generation                      uint64
	AvailableBytes, AvailableInodes uint64
	At                              time.Time
	Known                           bool
}

type Component struct {
	Filesystem           ID
	FilesystemGeneration uint64
	Root                 ID
	RootGeneration       uint64
	Owner                Digest // original custody proof for this registered root
	Workspace            ID
	WorkspaceGeneration  uint64
	Category             uint8
	// Physical is charged once to root/installation logical usage. Attribution
	// is charged to the referencing workspace; Capacity independently promises
	// each declared policy/known native restriction without duplicating logical
	// usage. Versioned authored estimates do not certify native allocation.
	Physical, Attribution                     Vector
	SettlementPhysical, SettlementAttribution Vector
	CapacityCount                             uint8
	Capacity                                  [MaxCapacityRefs]CapacityPromise
	CapacityBinding                           Digest
}

func (c Component) total() (Vector, Vector, error) {
	p, e := add(c.Physical, c.SettlementPhysical)
	if e != nil {
		return p, Vector{}, e
	}
	w, e := add(c.Attribution, c.SettlementAttribution)
	return p, w, e
}

// ObjectEntry is the only manifest entry kind in format version 1.
const ObjectEntry uint8 = 1

type Entry struct {
	Kind                      uint8
	Component                 uint8
	Target, Stage             string
	OldIdentity, NewIdentity  Digest
	Bytes, Files, Directories uint64
}

// WorkspaceReference is an exact member of one admitted registration snapshot.
// Its position is local to this immutable manifest, never a checkpoint row ID.
type WorkspaceReference struct {
	ID         ID
	Generation uint64
}

// WorkspaceMembership uses little-endian bits within each byte. A group charges
// its Attribution and SettlementAttribution to every selected dictionary member.
type WorkspaceMembership [MaxWorkspaces / 8]byte

// WorkspaceFanout is an explicit alternative to flat Component.Workspace fields.
// Groups remain <=MaxComponents; physical/capacity promises are counted once per
// group, with independent exact attribution for every selected workspace. All
// dictionary rows must be used, sorted uniquely by ID and have live generations.
type WorkspaceFanout struct {
	Workspaces  []WorkspaceReference
	Memberships []WorkspaceMembership
}

type Request struct {
	Workflow, Operation string
	Owner               Digest
	Components          []Component
	Fanout              *WorkspaceFanout `json:",omitempty"` // nil preserves flat bytes and JSON lineage
	Entries             []Entry
}

// ReserveComparison is a detached exact pre-admission observation. It confers
// no permission and reserves no capacity. The original host keeps its source
// and journal exclusion while deriving its future representation closure.
// ReserveCompared reloads under Q and refuses stale operands before any write.
type ReserveComparison struct {
	Original PolicyCommitment
	Request  Digest
	Slot     uint8
}
type State uint8

const (
	Empty State = iota
	Reserved
	Staging
	Dispatched
	Uncertain
	Terminal
)

type Token struct {
	Slot                 uint8
	ID                   ID
	Sequence, Generation uint64
}
type Ticket struct {
	Token               Token
	State               State
	PolicyRevision      uint64
	Workflow, Operation string
	Owner               Digest
	Manifest            Digest
	ManifestLength      uint32
	Components          []Component
	// Fanout is nil for flat tickets. Live fanout tickets carry their exact
	// immutable membership. A persisted-applied terminal has only an empty
	// nonnil marker: its retired manifest must not be read after slot reuse.
	// ValidateAppliedRequest verifies a separately retained complete request.
	Fanout  *WorkspaceFanout `json:",omitempty"`
	Changes []Change
	// Evidence binds domain-authored stage/dispatch or observed settlement facts.
	Evidence Digest
}
type Change struct{ PhysicalAdd, PhysicalRemove, AttributionAdd, AttributionRemove Vector }
type SettlementFact struct {
	Token           Token
	Owner, Evidence Digest
	Changes         []Change
}
type Snapshot struct {
	Revision, Epoch  uint64
	Maintenance      bool
	Installation     [Categories]Vector
	Promised         [Categories]Vector
	Workspaces       []Workspace
	Roots            []Root
	Tickets          []Ticket
	CapacityPromised []CapacityPromise
}

// Configuration is a detached, bounded view of one validated durable revision.
// Maintenance is explicit; it does not assert that a census is complete.
type Configuration struct {
	Revision, Epoch     uint64
	Maintenance         bool
	Policy              Policy
	Filesystems         []Filesystem
	CapacityConstraints []CapacityConstraint
}

// StatusPromises separates ordinary additional peak from settlement obligations.
// Both are logical promises, not stored bytes or physical allocation. Their
// checked sum has the same meaning as Snapshot.Promised.
type StatusPromises struct {
	Peak, Settlement [Categories]Vector
}

type StatusRoot struct {
	Root
	Promises StatusPromises
}

type StatusWorkspace struct {
	Workspace
	Promises StatusPromises
}

// StatusTicketCounts counts current pages. Active is Reserved+Staging+Dispatched;
// Uncertain is separate. Terminal incorporation compares the exact persisted
// applied tuple, even when load has already replayed its delta in memory.
type StatusTicketCounts struct {
	Nonempty, Active, Reserved, Staging, Dispatched, Uncertain uint64
	TerminalUnapplied, TerminalApplied                         uint64
}

// StatusSnapshot is an internal, detached view from one validated load under Q.
// Known flags and Maintenance are preserved; usage is private charged accounting
// even when unknown. There are no measured physical, reclaimability or freshness
// values here. Original-owner definitions must not be exposed directly over HTTP.
type StatusSnapshot struct {
	Revision, Epoch     uint64
	Maintenance         bool
	Policy              Policy
	Installation        [Categories]Vector
	Promises            StatusPromises
	Roots               []StatusRoot
	Workspaces          []StatusWorkspace
	Filesystems         []Filesystem
	CapacityConstraints []CapacityConstraint
	CapacityPromised    []CapacityPromise
	Tickets             StatusTicketCounts
}

type RootMeasurement struct {
	ID         ID
	Generation uint64
	Usage      [Categories]Vector
}
type WorkspaceMeasurement struct {
	ID         ID
	Generation uint64
	Usage      [Categories]Vector
}
type Census struct {
	Roots      []RootMeasurement
	Workspaces []WorkspaceMeasurement
	Evidence   Digest
}

func textValid(s string, n int, empty bool) bool {
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
func relative(s string) bool {
	if !textValid(s, 1024, true) || len(s) > 0 && s[0] == '/' {
		return false
	}
	// Backslash is rejected so manifests have one interpretation on all hosts.
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			return false
		}
	}
	if s == ".." || s == "." {
		return false
	}
	for _, p := range split(s) {
		if p == ".." || p == "." || p == "" && s != "" {
			return false
		}
	}
	return true
}
func split(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
func authority(d Digest) bool            { return d != (Digest{}) }
func refuse(e error, where string) error { return fmt.Errorf("%s: %w", where, e) }

// PolicyTransitionBytes is the fixed V6 host inverse operand, not a Q page or
// permission. The host must durably publish typed Prepared under its original
// complete exclusion before calling BeginPolicyTransition.
const PolicyTransitionBytes = 2048

type PolicyPhase uint8

const (
	PolicyQ0 PolicyPhase = iota
	PolicyQ1
	PolicyQ2
	PolicyQ3
	PolicyAborted
)

// PolicyCommitment binds the selected original checkpoint and both complete
// ticket banks. Manifests binds the ordered live-manifest vector validated by
// the same strict load. The arbitrary original checkpoint peer is not retained.
// Recovery uses remaining checkpoint/ticket/manifest bytes, never hash inversion.
type PolicyCommitment struct {
	Installation    ID
	Binding         Digest
	Revision, Epoch uint64
	CheckpointBank  uint8
	Checkpoint      Digest
	Tickets         [2]Digest
	Manifests       Digest
}

// PolicyTransition changes one current effective workspace at most. A default
// change never changes existing workspaces. Before values are current effective
// values, including any earlier sticky override, not source admission baselines.
// All fields are comparable values; no mutable caller buffer is retained.
type PolicyTransition struct {
	Original                  PolicyCommitment
	Before, After             Policy
	Workspace                 ID
	WorkspaceGeneration       uint64
	BeforeLimits, AfterLimits Limits
}

func (p PolicyTransition) Validate() error {
	b := p.Original
	if b.Installation.zero() || b.Binding == (Digest{}) || b.Revision == 0 || b.Epoch == 0 || b.Revision > math.MaxUint64-3 || b.Epoch == math.MaxUint64 || b.CheckpointBank > 1 || b.Checkpoint == (Digest{}) || b.Tickets[0] == (Digest{}) || b.Tickets[1] == (Digest{}) || b.Manifests == (Digest{}) || !p.Before.valid() || !p.After.valid() || p.Before.ObservationMaxAge != p.After.ObservationMaxAge {
		return ErrBounds
	}
	if p.Workspace.zero() {
		if p.WorkspaceGeneration != 0 || p.BeforeLimits != (Limits{}) || p.AfterLimits != (Limits{}) || p.Before == p.After {
			return ErrBounds
		}
	} else if p.WorkspaceGeneration == 0 || !p.BeforeLimits.valid() || !p.AfterLimits.valid() || p.BeforeLimits == p.AfterLimits {
		return ErrBounds
	}
	return nil
}
