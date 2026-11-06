package desk

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

const storageHostWorkflow = "storage-single-file-v1"

var errStorageOwnerRecovery = errors.New("storage original owner reconciliation required")

// A filesystem observation comes from an original descriptor source. This
// foundation has a private catalog-v3 concrete adapter for the original-owner
// lifecycle. Actual New installs that adapter; nil still refuses. Numeric policy axes and
// discovery status are distinct: unknown quota/container details never prove
// disabled enforcement, an unlimited pool, or complete physical health.
type storageFilesystemSample struct {
	PhysicalStatus   storagePhysicalStatus
	NativeAvailable  storagebudget.CapacityVector
	NativeTotalBytes uint64
	Filesystem       storagebudget.Filesystem
	RootGeneration   uint64
	CapacityCount    uint8
	CapacityRefs     [storagebudget.MaxCapacityRefs]storagebudget.CapacityRef
	Constraints      []storagebudget.CapacityConstraint
	Observations     []storagebudget.CapacityObservation
}

// Demand contains no payload. Workflows enforce finite logical effects and
// bounded persistence groups/metadata operations; the policy authors finite
// allocation estimates including settlement. An estimate is neither native
// allocation proof nor permission for extra operations, bytes or retries.
type storageCapacityDemand struct {
	Physical, Settlement storagebudget.Vector
	MetadataOps          uint64
}
type storageFilesystemObserver interface {
	observeStorageFilesystem(context.Context, *os.Root) (storageFilesystemSample, error)
	storageCapacityEnvelope(context.Context, *os.Root, storageCapacityDemand) ([storagebudget.MaxCapacityRefs]storagebudget.CapacityVector, error)
}
type storageBudgetOptions struct{ HeadroomBytes, HeadroomInodes uint64 }
type storageBudgetOwner struct {
	transfers          *chatTransferLifetimes
	admission          storageAdmissionGate
	self               *storageBudgetOwner
	mu                 sync.Mutex
	controlMu          sync.Mutex
	censusMu           sync.Mutex
	ref                *storageInstallationRef
	observer           storageFilesystemObserver
	options            storageBudgetOptions
	roots              map[storagebudget.ID]*storageRootBinding
	workspaces         map[storagebudget.ID]storagebudget.Workspace
	filesystems        map[storagebudget.ID]storagebudget.Filesystem
	constraints        map[storagebudget.ID]storagebudget.CapacityConstraint
	openedRoots        []*os.Root
	openedFiles        []*os.File
	active             uint64
	closing, closed    bool
	closeErr           error
	censusEpoch        uint64
	currentCensus      *storageInitialCensusAuthority // mu; installed only by initialCensus
	transitionRequired bool
	fault              *storageBudgetFault
	closedDone         chan struct{}
	closingStarted     chan struct{}
	measurement        storageOwnedMeasurementSlot // mu; diagnostic only, never effect authority
	statusCache        *storageStatusCache         // shared by the original installation and its children
	statusPressure     storageStatusPressureSlot
}
type storageRootBinding struct {
	self                    *storageRootBinding
	owner                   *storageBudgetOwner
	root                    *os.Root
	witnesses               []storageNamedRoot
	row                     storagebudget.Root
	directory               storageDirectoryIdentity
	category                uint8
	references              []storagebudget.ID
	legacyReferences        []storagebudget.ID
	configControl, qControl bool
	domainName              string
	domain                  storageNamedFile
	domainDirectory         bool
	source                  *storageOriginalSourceRoot
	restrictedNamespace     string           // only the closed original Role7 .desk-private scope
	transferInactive        bool             // exact completed Workflow4 scalar4; never fresh-write authority
	scopeParent             storagebudget.ID // exact Kind8 parent relation, otherwise zero
}
type storageBudgetFault struct {
	Name, Phase string
	Short       bool
	Remaining   int
	Entered     chan struct{}
	Release     <-chan struct{}
}

func (o *storageBudgetOwner) fail(name, phase string) error {
	f := o.fault
	if f != nil && f.Name == name && f.Phase == phase {
		if f.Remaining > 0 {
			f.Remaining--
			return nil
		}
		o.fault = nil
		if f.Entered != nil {
			close(f.Entered)
		}
		if f.Release != nil {
			<-f.Release
		}
		return errStorageOwnerRecovery
	}
	return nil
}
func storageMetadataDigest(purpose string, ids ...storageDirectoryIdentity) storagebudget.Digest {
	h := sha256.New()
	h.Write([]byte(purpose))
	var b [16]byte
	for _, id := range ids {
		binary.LittleEndian.PutUint64(b[:8], id.Device)
		binary.LittleEndian.PutUint64(b[8:], id.Inode)
		h.Write(b[:])
	}
	var d storagebudget.Digest
	copy(d[:], h.Sum(nil))
	return d
}
func storageDerivedID(purpose string, installation storagebudget.ID, body []byte) storagebudget.ID {
	h := sha256.New()
	h.Write([]byte(purpose))
	h.Write(installation[:])
	h.Write(body)
	var id storagebudget.ID
	copy(id[:], h.Sum(nil))
	return id
}
func newStorageBudgetOwner(ref *storageInstallationRef, observer storageFilesystemObserver, opt storageBudgetOptions) (*storageBudgetOwner, error) {
	if !ref.valid() || !ref.owner || observer == nil || opt.HeadroomBytes > 1<<60 || opt.HeadroomInodes > 1<<40 {
		return nil, storagebudget.ErrAuthority
	}
	borrowed, e := ref.borrow()
	if e != nil {
		return nil, e
	}
	o := &storageBudgetOwner{closedDone: make(chan struct{}), closingStarted: make(chan struct{}), ref: borrowed, observer: observer, options: opt, roots: map[storagebudget.ID]*storageRootBinding{}, workspaces: map[storagebudget.ID]storagebudget.Workspace{}, filesystems: map[storagebudget.ID]storagebudget.Filesystem{}, constraints: map[storagebudget.ID]storagebudget.CapacityConstraint{}}
	o.self = o
	o.transfers = &chatTransferLifetimes{owner: o}
	o.initializeAdmission()
	return o, nil
}
func (o *storageBudgetOwner) valid() bool { return o != nil && o.self == o && o.ref.valid() }
func (o *storageBudgetOwner) retain() error {
	if !o.valid() {
		return errStorageCustody
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closing || o.closed {
		return os.ErrClosed
	}
	if o.transitionRequired {
		return errStorageOwnerTransitionRequired
	}
	o.admission.mu.Lock()
	enrollment := o.admission.enrollment != nil
	o.admission.mu.Unlock()
	if enrollment {
		return storagebudget.ErrBusy
	}
	if o.active == ^uint64(0) {
		return storagebudget.ErrBounds
	}
	o.active++
	return nil
}
func (o *storageBudgetOwner) drop() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.active == 0 {
		return storagebudget.ErrAuthority
	}
	o.active--
	return o.closeLocked()
}
func (o *storageBudgetOwner) closeLocked() (e error) {
	if !o.closing || o.active != 0 {
		return nil
	}
	if o.closed {
		return o.closeErr
	}
	o.closed = true
	if o.closedDone != nil {
		defer close(o.closedDone)
	}
	for i := len(o.openedFiles) - 1; i >= 0; i-- {
		e = errors.Join(e, o.openedFiles[i].Close())
	}
	for i := len(o.openedRoots) - 1; i >= 0; i-- {
		e = errors.Join(e, o.openedRoots[i].Close())
	}
	o.closeErr = errors.Join(e, o.ref.Close())
	return o.closeErr
}
func (o *storageBudgetOwner) Close() error {
	if !o.valid() {
		return errStorageCustody
	}
	admissionErr := o.closeAdmission()
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closing {
		o.closing = true
		o.measurement = storageOwnedMeasurementSlot{}
		o.statusPressure.retire()
		if o.statusCache != nil {
			o.statusCache.retire()
		}
		if o.closingStarted != nil {
			close(o.closingStarted)
		}
	}
	return errors.Join(admissionErr, o.closeLocked())
}

// Only the original machine owner constructs these rows from its validated
// original workspace identity. They are neither serialized write capabilities
// nor replacements for existing registry/signing/Runner identities.
func (o *storageBudgetOwner) addWorkspace(original string, generation uint64, limits storagebudget.Limits) (storagebudget.ID, error) {
	if !o.valid() || original == "" || len(original) > 256 || strings.ContainsAny(original, "\x00\r\n") || generation == 0 {
		return storagebudget.ID{}, storagebudget.ErrAuthority
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closing || o.censusEpoch != 0 || o.active != 0 {
		return storagebudget.ID{}, storagebudget.ErrBusy
	}
	id := storageDerivedID("desk-storage-workspace-v1", o.ref.cell.record.Installation, []byte(original))
	row := storagebudget.Workspace{ID: id, Generation: generation, Limits: limits}
	if prior, ok := o.workspaces[id]; ok && prior != row {
		return id, storagebudget.ErrAuthority
	}
	o.workspaces[id] = row
	return id, nil
}
func (o *storageBudgetOwner) configRoot(ctx context.Context, domainName string) (*storageRootBinding, error) {
	if !o.valid() {
		return nil, storagebudget.ErrAuthority
	}
	return o.bindOriginalRoot(ctx, o.ref.cell.original.config, nil, storagebudget.Control, nil, domainName, true, false)
}
func (o *storageBudgetOwner) anchorRoot(ctx context.Context) (*storageRootBinding, error) {
	if !o.valid() {
		return nil, storagebudget.ErrAuthority
	}
	return o.bindOriginalRoot(ctx, o.ref.cell.anchor, nil, storagebudget.Control, nil, "", false, true)
}
func (o *storageBudgetOwner) childRoot(ctx context.Context, parent *storageRootBinding, name string, category uint8, references []storagebudget.ID, domainName string) (*storageRootBinding, error) {
	if !o.valid() || parent == nil || parent.self != parent || parent.owner != o || name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00\r\n") {
		return nil, storagebudget.ErrAuthority
	}
	if e := parent.current(ctx); e != nil {
		return nil, e
	}
	named, e := storagePinChild(parent.root, name, true)
	if e != nil {
		return nil, e
	}
	binding, e := o.bindOriginalRoot(ctx, named.root, append(append([]storageNamedRoot(nil), parent.witnesses...), named), category, references, domainName, false, false)
	if e != nil {
		return nil, errors.Join(e, named.root.Close())
	}
	o.mu.Lock()
	o.openedRoots = append(o.openedRoots, named.root)
	o.mu.Unlock()
	return binding, nil
}

// bindOriginalRoot is private to the original namespace constructors above.
// External project/history roots need their original storage claim/migration;
// accepting an arbitrary path, held root or copied marker here is forbidden.
func (o *storageBudgetOwner) bindOriginalRoot(ctx context.Context, root *os.Root, witnesses []storageNamedRoot, category uint8, references []storagebudget.ID, domainName string, config, qcontrol bool) (*storageRootBinding, error) {
	if !o.valid() || root == nil || category >= storagebudget.Categories || !qcontrol && domainName == "" {
		return nil, storagebudget.ErrAuthority
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if e := o.ref.cell.current(); e != nil {
		return nil, e
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closing || o.censusEpoch != 0 || o.active != 0 {
		return nil, storagebudget.ErrBusy
	}
	directory, e := storageDirectoryID(root)
	if e != nil {
		return nil, e
	}
	for _, id := range references {
		if _, ok := o.workspaces[id]; !ok {
			return nil, storagebudget.ErrAuthority
		}
	}
	var sample storageFilesystemSample
	var original *storageOwnerRoot
	if a, ok := o.observer.(*storageAdvisoryObserver); ok {
		selected, se := a.originalRoot(ctx, root)
		if se != nil {
			return nil, se
		}
		original = &selected
		provisional := &storageRootBinding{owner: o, row: storagebudget.Root{ID: selected.Definition.ID}}
		sample, e = a.query(ctx, provisional, root)
	} else {
		sample, e = o.observer.observeStorageFilesystem(ctx, root)
	}
	if e != nil {
		return nil, e
	}
	if e = o.validateStorageSample(sample); e != nil {
		return nil, e
	}
	fs := sample.Filesystem
	fsid := fs.ID
	if prior, ok := o.filesystems[fsid]; ok && prior != fs {
		return nil, storagebudget.ErrAuthority
	}
	for _, row := range sample.Constraints {
		if prior, ok := o.constraints[row.ID]; ok && prior != row {
			return nil, storagebudget.ErrAuthority
		}
		for _, prior := range o.constraints {
			if prior.ID != row.ID && prior.Identity == row.Identity {
				return nil, storagebudget.ErrAuthority
			}
		}
	}
	physical := storageMetadataDigest("desk-storage-root-directory-v1", directory)
	id := storageDerivedID("desk-storage-root-id-v1", o.ref.cell.record.Installation, physical[:])
	if original != nil {
		id = original.Definition.ID
	}
	if prior, ok := o.roots[id]; ok {
		if prior.category != category || prior.row.Filesystem != fsid || prior.row.Generation != sample.RootGeneration || prior.row.CapacityCount != sample.CapacityCount || prior.row.CapacityRefs != sample.CapacityRefs || prior.domainName != domainName || prior.configControl != config || prior.qControl != qcontrol {
			return nil, storagebudget.ErrAuthority
		}
		prior.witnesses = append(prior.witnesses, witnesses...)
		prior.references = storageUniqueIDs(append(prior.references, references...))
		prior.row.Workspace = storagebudget.ID{}
		if len(prior.references) == 1 {
			prior.row.Workspace = prior.references[0]
		}
		return prior, nil
	}
	r := &storageRootBinding{owner: o, root: root, witnesses: witnesses, directory: directory, category: category, references: storageUniqueIDs(references), configControl: config, qControl: qcontrol, domainName: domainName}
	r.self = r
	identities := []storageDirectoryIdentity{directory}
	if domainName != "" {
		if strings.ContainsAny(domainName, "/\\\x00\r\n") || domainName == "." || domainName == ".." {
			return nil, storagebudget.ErrAuthority
		}
		r.domain, e = storageOpenFile(root, domainName, 0)
		if e != nil {
			return nil, e
		}
		di, e := storageFileIdentity(r.domain.identity)
		if e != nil {
			return nil, errors.Join(e, r.domain.file.Close())
		}
		identities = append(identities, di)
	}
	// Persist original control-inode identity in the config boundary identity.
	// A separately reopened replacement control/ledger lock cannot inherit it.
	if config {
		for _, n := range o.ref.cell.original.files {
			identity, e := storageFileIdentity(n.identity)
			if e != nil {
				if r.domain.file != nil {
					r.domain.file.Close()
				}
				return nil, e
			}
			identities = append(identities, identity)
		}
	}
	identity := storageMetadataDigest("desk-storage-root-owner-v1", identities...)
	r.row = storagebudget.Root{ID: id, Owner: o.ref.cell.binding, Identity: identity, Filesystem: fsid, Generation: sample.RootGeneration, CapacityCount: sample.CapacityCount, CapacityRefs: sample.CapacityRefs, Locator: "original:" + storageHexID(id)}
	if len(r.references) == 1 {
		r.row.Workspace = r.references[0]
	}
	r.row.CapacityBinding, e = storagebudget.CapacityBindingFor(r.row, fs, sample.Constraints)
	if e != nil {
		if r.domain.file != nil {
			e = errors.Join(e, r.domain.file.Close())
		}
		return nil, e
	}
	if original != nil {
		// Original lifecycle has reattached the catalog's selected controls. Keep
		// persisted identifiers/identity/refs/binding on reopen, never regenerate them
		// from a live device number or reinterpret an older owner as v3.
		d := original.Definition
		if original.Category != category || d.Owner != o.ref.cell.binding || d.Filesystem != fsid || d.Generation != sample.RootGeneration || d.CapacityRefs != sample.CapacityRefs || d.CapacityCount != sample.CapacityCount {
			if r.domain.file != nil {
				r.domain.file.Close()
			}
			return nil, storagebudget.ErrAuthority
		}
		r.row.Identity = d.Identity
		r.row.Locator = d.Locator
		// The binding formula includes immutable original identity; recompute only
		// to verify the exact persisted operand, never to replace it.
		binding, be := storagebudget.CapacityBindingFor(r.row, fs, sample.Constraints)
		if be != nil || binding != d.CapacityBinding {
			if r.domain.file != nil {
				r.domain.file.Close()
			}
			return nil, storagebudget.ErrAuthority
		}
		r.row.CapacityBinding = d.CapacityBinding
	}
	for _, row := range sample.Constraints {
		o.constraints[row.ID] = row
	}
	if r.domain.file != nil {
		o.openedFiles = append(o.openedFiles, r.domain.file)
	}
	o.filesystems[fsid] = fs
	o.roots[id] = r
	return r, nil
}
func storageUniqueIDs(in []storagebudget.ID) []storagebudget.ID {
	out := append([]storagebudget.ID(nil), in...)
	sort.Slice(out, func(i, j int) bool { return strings.Compare(string(out[i][:]), string(out[j][:])) < 0 })
	n := 0
	for _, id := range out {
		if n == 0 || out[n-1] != id {
			out[n] = id
			n++
		}
	}
	return out[:n]
}
func storageHexID(id storagebudget.ID) string {
	const digits = "0123456789abcdef"
	b := make([]byte, 32)
	for i, v := range id {
		b[2*i] = digits[v>>4]
		b[2*i+1] = digits[v&15]
	}
	return string(b)
}
func (r *storageRootBinding) current(ctx context.Context) error {
	if r == nil || r.self != r || !r.owner.valid() {
		return storagebudget.ErrAuthority
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := r.owner.ref.cell.current(); e != nil {
		return e
	}
	return r.bindingCurrent(ctx)
}

// Read-only binding predicate only. It neither validates installation custody
// nor grants authority. The ordinary current wrapper checks the installation;
// an original enrollment current frame brackets all its bindings with fresh
// validation of the same retained maintenance use.
func (r *storageRootBinding) bindingCurrent(ctx context.Context) error {
	if r == nil || r.self != r || !r.owner.valid() {
		return storagebudget.ErrAuthority
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.source != nil {
		if e := r.source.current(ctx, r.owner.ref.cell.startup); e != nil {
			return e
		}
	}
	if r.transferInactive {
		if err := r.transferInactiveCurrent(ctx); err != nil {
			return err
		}
	}

	for _, n := range r.witnesses {
		if e := n.current(); e != nil {
			return e
		}
	}
	var directory storageDirectoryIdentity
	var e error
	if r.source != nil {
		if r.source.root != r.root {
			return errStorageCustody
		}
		directory, e = r.source.directoryIdentity()
	} else {
		directory, e = storageDirectoryID(r.root)
	}
	if e != nil || directory != r.directory {
		return errStorageCustody
	}
	if r.domain.file != nil {
		return r.domainCurrent()
	}
	if r.scopeParent != (storagebudget.ID{}) {
		return errStorageCustody
	}
	return nil
}
func (r *storageRootBinding) domainCurrent() error {
	if r.scopeParent != (storagebudget.ID{}) {
		parent := r.owner.roots[r.scopeParent]
		if parent == nil || parent == r || parent.scopeParent != (storagebudget.ID{}) || parent.source == nil || parent.source.role != 7 || r.source == nil || r.source.parent != parent.source || r.domain.file == nil || r.domain.file != parent.domain.file || r.domain.root != parent.root || !r.domainDirectory {
			return errStorageCustody
		}
		if err := parent.source.current(context.Background(), r.owner.ref.cell.startup); err != nil {
			return err
		}
		return parent.domainCurrent()
	}
	if !r.domainDirectory {
		return r.domain.current()
	}
	held, err := r.domain.file.Stat()
	if err != nil {
		return err
	}
	named, err := r.domain.root.Stat(".")
	if err != nil {
		return err
	}
	private := storagePrivate(held, true) && storagePrivate(named, true)
	if r.restrictedNamespace != "" && r.source != nil && (r.source.role == 7 || r.source.role == 8) {
		// The selected original project may be non-private. Its adopted source
		// owns that directory; the restricted scope never authorizes its other
		// contents. Original identity/name currentness was checked by r.current.
		private = held.IsDir() && named.IsDir()
	}
	if !private || !os.SameFile(held, named) || !os.SameFile(held, r.domain.identity) {
		return errStorageCustody
	}
	return nil
}
func (r *storageRootBinding) categoryOf(relative string) (uint8, []storagebudget.ID) {
	category := r.category
	references := r.references
	if r.configControl {
		category = storagebudget.Control
		references = nil
		if !strings.Contains(relative, "/") && (conversationFileName.MatchString(relative) || assistantRunFileName.MatchString(relative) || assistantSourceFileName.MatchString(relative)) {
			category = storagebudget.History
			references = r.legacyReferences
		}
		if assistantDialectReadRecoveryFileName.MatchString(relative) {
			references = r.legacyReferences
		}
	} else if assistantDialectReadRecoveryFileName.MatchString(relative) {
		category = storagebudget.Control
	}
	// One owner decision serves census and every ordinary writer. Retained
	// provider roots retain their category; only flat chat namespaces split.
	if !strings.Contains(relative, "/") && (r.category == storagebudget.History || r.configControl) {
		if memoryFileName.MatchString(relative) {
			category = storagebudget.Memory
			if r.configControl {
				references = r.legacyReferences
			}
		}
		if attachmentFileName.MatchString(relative) {
			category = storagebudget.Attachments
			if r.configControl {
				references = r.legacyReferences
			}
		}
	}
	return category, references
}
func (o *storageBudgetOwner) legacyHistory(r *storageRootBinding, workspaces []storagebudget.ID) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.censusEpoch != 0 || o.active != 0 || r == nil || r.owner != o || !r.configControl {
		return storagebudget.ErrAuthority
	}
	for _, id := range workspaces {
		if _, ok := o.workspaces[id]; !ok {
			return storagebudget.ErrAuthority
		}
	}
	r.legacyReferences = storageUniqueIDs(workspaces)
	return nil
}

type storageLifetime interface {
	Current() error
	storageCell() *storageInstallation
}

func (u *storageInstallationUse) storageCell() *storageInstallation {
	if u == nil || u.self != u {
		return nil
	}
	return u.cell
}

type storageMaintenanceUse struct {
	self    *storageMaintenanceUse
	cell    *storageInstallation
	gate    *os.File
	closed  bool
	domains map[storageDirectoryIdentity]*storageDomainHold
}

func (u *storageMaintenanceUse) storageCell() *storageInstallation {
	if u == nil || u.self != u {
		return nil
	}
	return u.cell
}
func (u *storageMaintenanceUse) Current() error {
	if u == nil || u.self != u || u.cell == nil {
		return errStorageCustody
	}
	c := u.cell
	c.mu.Lock()
	defer c.mu.Unlock()
	if u.closed || c.closed {
		return os.ErrClosed
	}
	if e := c.current(); e != nil {
		return e
	}
	return c.useGateCurrent(u.gate)
}
func (u *storageMaintenanceUse) Close() error {
	if u == nil || u.self != u || u.cell == nil {
		return errStorageCustody
	}
	c := u.cell
	c.mu.Lock()
	defer c.mu.Unlock()
	if u.closed {
		return c.closeErr
	}
	u.closed = true
	return errors.Join(u.gate.Close(), c.dropLocked())
}
func (o *storageBudgetOwner) maintenanceUse() (*storageMaintenanceUse, error) {
	c := o.ref.cell
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing || c.closed {
		return nil, os.ErrClosed
	}
	if e := c.current(); e != nil {
		return nil, e
	}
	gate, e := storageOpenOriginalControl(c, storageUseName)
	if e != nil {
		return nil, e
	}
	if e = lockPrivateData(gate.file, true); e != nil {
		return nil, errors.Join(storagebudget.ErrBusy, gate.file.Close())
	}
	if e = c.useGateCurrent(gate.file); e != nil {
		return nil, errors.Join(e, gate.file.Close())
	}
	if c.refs == ^uint64(0) {
		return nil, errors.Join(storagebudget.ErrBounds, gate.file.Close())
	}
	c.refs++
	u := &storageMaintenanceUse{cell: c, gate: gate.file}
	u.self = u
	return u, nil
}
func storageOpenOriginalControl(c *storageInstallation, name string) (storageNamedFile, error) {
	for _, original := range c.original.files {
		if original.root != c.original.config || original.name != name {
			continue
		}
		if e := original.current(); e != nil {
			return storageNamedFile{}, e
		}
		fresh, e := storageOpenFile(original.root, name, original.size)
		if e != nil {
			return fresh, e
		}
		held, e := fresh.file.Stat()
		witness, we := original.file.Stat()
		if e != nil || we != nil || !os.SameFile(held, witness) || original.current() != nil {
			return storageNamedFile{}, errors.Join(errStorageCustody, fresh.file.Close())
		}
		return fresh, nil
	}
	return storageNamedFile{}, errStorageCustody
}

type storageDomainHold struct {
	self     *storageDomainHold
	owner    *storageBudgetOwner
	root     *storageRootBinding
	lifetime storageLifetime
	file     *os.File
	closed   bool
	borrowed *storageDomainHold
	prefix   *storagePrefixBorrow
}

func (o *storageBudgetOwner) domain(ctx context.Context, r *storageRootBinding, lifetime storageLifetime) (*storageDomainHold, error) {
	if !o.valid() || r == nil || r.owner != o || lifetime == nil || lifetime.storageCell() != o.ref.cell {
		return nil, storagebudget.ErrAuthority
	}
	if e := lifetime.Current(); e != nil {
		return nil, e
	}
	if e := r.current(ctx); e != nil {
		return nil, e
	}
	d := &storageDomainHold{owner: o, root: r, lifetime: lifetime}
	d.self = d
	if r.qControl {
		if _, ok := lifetime.(*storageMaintenanceUse); !ok {
			return nil, storagebudget.ErrAuthority
		}
		return d, nil
	}
	if m, ok := lifetime.(*storageMaintenanceUse); ok {
		if m.domains == nil {
			m.domains = map[storageDirectoryIdentity]*storageDomainHold{}
		}
		id, e := storageFileIdentity(r.domain.identity)
		if e != nil {
			return nil, e
		}
		if prior := m.domains[id]; prior != nil {
			d.borrowed = prior
			if e = d.current(ctx); e != nil {
				return nil, e
			}
			return d, nil
		}
	}
	var n storageNamedFile
	var e error
	if r.domainDirectory {
		f, err := r.domain.root.Open(".")
		if err != nil {
			return nil, err
		}
		info, err := f.Stat()
		if err != nil {
			return nil, errors.Join(err, f.Close())
		}
		n = storageNamedFile{root: r.domain.root, name: ".", file: f, identity: info}
	} else {
		n, e = storageOpenFile(r.domain.root, r.domainName, 0)
		if e != nil {
			return nil, e
		}
	}

	held, e := n.file.Stat()
	witness, we := r.domain.file.Stat()
	if e != nil || we != nil || !os.SameFile(held, witness) {
		return nil, errors.Join(errStorageCustody, n.file.Close())
	}
	if e = lockPrivateData(n.file, true); e != nil {
		return nil, errors.Join(storagebudget.ErrBusy, n.file.Close())
	}
	d.file = n.file
	if m, ok := lifetime.(*storageMaintenanceUse); ok {
		id, e := storageFileIdentity(r.domain.identity)
		if e != nil {
			return nil, errors.Join(e, n.file.Close())
		}
		m.domains[id] = d
	}
	if e = d.current(ctx); e != nil {
		return nil, errors.Join(e, n.file.Close())
	}
	return d, nil
}
func (d *storageDomainHold) current(ctx context.Context) error {
	if d == nil || d.self != d || d.closed || d.root == nil || d.root.owner != d.owner {
		return storagebudget.ErrAuthority
	}
	if e := d.lifetime.Current(); e != nil {
		return e
	}
	if e := d.root.current(ctx); e != nil {
		return e
	}
	return d.descriptionCurrent(ctx)
}

// Description predicate only, shared by ordinary domain validation and the
// original enrollment's read-only frame. The caller separately checks the
// root binding and its exact live installation lifetime.
func (d *storageDomainHold) descriptionCurrent(ctx context.Context) error {
	if d == nil || d.self != d || d.closed || d.root == nil || d.root.owner != d.owner {
		return storagebudget.ErrAuthority
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.root.qControl {
		return nil
	}
	if d.prefix != nil {
		return d.prefix.current(ctx)
	}
	if d.borrowed != nil {
		return d.borrowed.current(ctx)
	}
	held, e := d.file.Stat()
	original, oe := d.root.domain.file.Stat()
	if e != nil || oe != nil || !os.SameFile(held, original) {
		return errStorageCustody
	}
	return d.root.domainCurrent()
}
func (d *storageDomainHold) Close() error {
	if d == nil || d.self != d {
		return storagebudget.ErrAuthority
	}
	if d.closed {
		return nil
	}
	d.closed = true
	if d.prefix != nil {
		return d.prefix.Close()
	}
	if d.file != nil {
		return d.file.Close()
	}
	return nil
}

// validateStorageSample checks structure and explicit initial headroom policy;
// it does not manufacture platform authority from a digest or an available count.
func (o *storageBudgetOwner) validateStorageSample(s storageFilesystemSample) error {
	if len(s.Constraints) < 1 || len(s.Constraints) > storagebudget.MaxCapacityRefs || int(s.CapacityCount) != len(s.Constraints) {
		return storagebudget.ErrBounds
	}
	f := s.Filesystem
	r := storagebudget.Root{ID: f.ID, Owner: f.Identity, Identity: f.Identity, Filesystem: f.ID, Generation: s.RootGeneration, CapacityCount: s.CapacityCount, CapacityRefs: s.CapacityRefs}
	if _, e := storagebudget.CapacityBindingFor(r, f, s.Constraints); e != nil {
		return e
	}
	for i, row := range s.Constraints {
		if row.ID != s.CapacityRefs[i].Constraint || row.Generation != s.CapacityRefs[i].Generation {
			return storagebudget.ErrAuthority
		}
		if a, ok := o.observer.(*storageAdvisoryObserver); ok && row == storageOwnerAdvisoryConstraint(a.catalog.Installation).Definition {
			continue
		}
		if row.ID == f.ByteConstraint && row.Headroom.Bytes < o.options.HeadroomBytes {
			return storagebudget.ErrAuthority
		}
		if row.ID == f.InodeConstraint && row.Headroom.Inodes < o.options.HeadroomInodes {
			return storagebudget.ErrAuthority
		}
	}
	return nil
}
func (o *storageBudgetOwner) rootStorageSample(ctx context.Context, r *storageRootBinding, root *os.Root) (storageFilesystemSample, error) {
	if r == nil || r.owner != o {
		return storageFilesystemSample{}, storagebudget.ErrAuthority
	}
	if _, ok := o.observer.(*storageAdvisoryObserver); ok {
		if _, present := ctx.Value(storageCapacityPassKey{}).(*storageCapacityPass); !present {
			p, e := o.beginStorageCapacityPass(ctx, []storageCapacityEffectParent{{binding: r, root: root}})
			if e != nil {
				return storageFilesystemSample{}, e
			}
			defer p.Close()
			ctx = p.context(ctx)
		}
	}
	s, e := o.observer.observeStorageFilesystem(ctx, root)
	if e != nil {
		return storageFilesystemSample{}, e
	}
	if e = o.validateStorageSample(s); e != nil {
		return storageFilesystemSample{}, e
	}
	if s.Filesystem != o.filesystems[r.row.Filesystem] || s.RootGeneration != r.row.Generation || s.CapacityCount != r.row.CapacityCount || s.CapacityRefs != r.row.CapacityRefs {
		if _, ok := o.observer.(*storageAdvisoryObserver); ok {
			return storageFilesystemSample{}, o.fenceStorageTransition(ctx)
		}
		return storageFilesystemSample{}, storagebudget.ErrUnknown
	}
	for _, row := range s.Constraints {
		if row != o.constraints[row.ID] {
			if _, ok := o.observer.(*storageAdvisoryObserver); ok {
				return storageFilesystemSample{}, o.fenceStorageTransition(ctx)
			}
			return storageFilesystemSample{}, storagebudget.ErrUnknown
		}
	}
	binding, e := storagebudget.CapacityBindingFor(r.row, s.Filesystem, s.Constraints)
	if e != nil || binding != r.row.CapacityBinding {
		if _, ok := o.observer.(*storageAdvisoryObserver); ok {
			return storageFilesystemSample{}, o.fenceStorageTransition(ctx)
		}
		return storageFilesystemSample{}, storagebudget.ErrUnknown
	}
	return s, nil
}

// The zero values deliberately mean unknown/unavailable, never Healthy.
type storagePhysicalMode uint8
type storagePhysicalCounterStatus uint8
type storagePhysicalDiscovery uint8
type storagePhysicalReason uint8

const (
	storagePhysicalModeUnknown storagePhysicalMode = iota
	storagePhysicalAdvisory
)
const (
	storagePhysicalCounterUnknown storagePhysicalCounterStatus = iota
	storagePhysicalCounterMeasured
	storagePhysicalCounterDynamic // documented absence of an independent inode pool
)
const (
	storagePhysicalDiscoveryUnknown storagePhysicalDiscovery = iota
	storagePhysicalDiscoveryNotApplicable
	storagePhysicalDiscoveryDisabled
	storagePhysicalDiscoveryAccounting
	storagePhysicalDiscoveryEnforced
)
const (
	storagePhysicalReasonUnknown storagePhysicalReason = iota
	storagePhysicalReasonDetailsUnknown
	storagePhysicalReasonObserved
	storagePhysicalReasonCountersUnavailable
	storagePhysicalReasonStale
	storagePhysicalReasonTransitionRequired
)

type storagePhysicalStatus struct {
	PolicyID                            string
	PolicyVersion                       uint16
	Mode                                storagePhysicalMode
	Bytes, Inodes                       storagePhysicalCounterStatus
	UserQuota, GroupQuota, ProjectQuota storagePhysicalDiscovery
	APFSContainer                       storagePhysicalDiscovery
	Oldest                              time.Time
	Reason                              storagePhysicalReason
}

// This checks only the typed advisory declaration, never freshness, custody,
// installed code or admission. Numeric observations retain their own masks.
func (s storagePhysicalStatus) validAdvisory() bool {
	if s.PolicyID != "desk-advisory-allocation-v2" || s.PolicyVersion != 2 || s.Mode != storagePhysicalAdvisory || s.Bytes > storagePhysicalCounterMeasured || s.Inodes > storagePhysicalCounterDynamic || s.Reason > storagePhysicalReasonTransitionRequired {
		return false
	}
	for _, d := range []storagePhysicalDiscovery{s.UserQuota, s.GroupQuota, s.ProjectQuota, s.APFSContainer} {
		if d > storagePhysicalDiscoveryEnforced {
			return false
		}
	}
	return !s.Oldest.IsZero()
}

// Complete capacity-observer dependency collocated under the existing root
// source pin; no qualification-critical executable remains in the shim.

var errStorageOwnerTransitionRequired = errors.New("storage original owner constraint transition required")

const (
	storageCapacityPassRoots       = 4096
	storageCapacityPassParents     = 32
	storageCapacityPassConstraints = 512
)

// Effects are sealed by an original binding and its complete named child chain.
// The observer never accepts a caller path as an observation capability.
type storageCapacityEffectParent struct {
	binding   *storageRootBinding
	root      *os.Root
	witnesses []storageNamedRoot
}

type storageCapacityPassKey struct{}
type storageCapacityPass struct {
	self               *storageCapacityPass
	mu                 sync.Mutex
	owner              *storageBudgetOwner
	use                *storageInstallationUse
	startup            *storageBudgetConstruction
	census             *storageInitialCensusAuthority
	exclusive          *storageExclusiveStartupCensus
	startupUse         *storageMaintenanceUse
	startupHolds       []*storageDomainHold
	roots              []*storageRootBinding
	effects            []storageCapacityEffectParent
	samples            map[*os.Root]storageFilesystemSample
	started, completed time.Time
	maxAge             time.Duration
	epoch              uint64
	groups             uint16
	closed             bool
	mutation           *storageMutationAuthority
}

// beginStorageCapacityPass holds original SH custody and a registry retain until
// Close. Enrollment/maintenance must acquire EX and cannot mutate this set. The
// query deadline bounds acceptance, not the duration of an uninterruptible call.
func (o *storageBudgetOwner) beginStorageCapacityPass(ctx context.Context, effects []storageCapacityEffectParent) (p *storageCapacityPass, err error) {
	return o.beginStorageCapacityPassWithStartup(ctx, effects, nil)
}

// Only the actual unfinished startup can lend its existing EX custody. The pass
// owns its registry retain, but never closes or converts the borrowed holds.
func (o *storageBudgetOwner) beginStorageCapacityPassWithStartup(ctx context.Context, effects []storageCapacityEffectParent, startup *storageBudgetConstruction) (*storageCapacityPass, error) {
	return o.beginStorageCapacityPassWithAuthority(ctx, effects, startup, nil)
}

func (o *storageBudgetOwner) beginStorageCapacityPassWithCensus(ctx context.Context, effects []storageCapacityEffectParent, census *storageInitialCensusAuthority) (*storageCapacityPass, error) {
	if census == nil {
		return nil, storagebudget.ErrAuthority
	}
	return o.beginStorageCapacityPassWithAuthority(ctx, effects, nil, census)
}

func (o *storageBudgetOwner) beginStorageCapacityPassWithAuthority(ctx context.Context, effects []storageCapacityEffectParent, startup *storageBudgetConstruction, census *storageInitialCensusAuthority) (*storageCapacityPass, error) {
	return o.beginStorageCapacityPassAll(ctx, effects, startup, census, nil)
}
func (o *storageBudgetOwner) beginStorageCapacityPassWithExclusive(ctx context.Context, effects []storageCapacityEffectParent, exclusive *storageExclusiveStartupCensus) (*storageCapacityPass, error) {
	return o.beginStorageCapacityPassAll(ctx, effects, nil, nil, exclusive)
}
func (o *storageBudgetOwner) beginStorageCapacityPassAll(ctx context.Context, effects []storageCapacityEffectParent, startup *storageBudgetConstruction, census *storageInitialCensusAuthority, exclusive *storageExclusiveStartupCensus) (p *storageCapacityPass, err error) {
	if ctx == nil || !o.valid() || len(effects) > storageCapacityPassParents {
		return nil, storagebudget.ErrBounds
	}
	transferred := false
	if exclusive != nil {
		if exclusive != o.exclusiveCensusPhase() || startup != nil || census != nil {
			return nil, storagebudget.ErrAuthority
		}
		if err = exclusive.borrow(ctx); err != nil {
			return nil, err
		}
	} else {
		// Register the existing pass retain before any currentness/native work.
		// The same short owner/admission transition excludes phase installation.
		if err = o.retainCapacityPassOutsideExclusive(); err != nil {
			return nil, err
		}
	}
	defer func() {
		if err != nil && !transferred {
			if exclusive != nil {
				err = errors.Join(err, exclusive.dropBorrow())
			} else {
				err = errors.Join(err, o.drop())
			}
		}
	}()

	if startup != nil && startup.self == startup {
		if err = startup.exclusiveBoundary("capacity-before-current"); err != nil {
			return nil, err
		}
	}

	if census != nil {
		if startup != nil {
			return nil, storagebudget.ErrAuthority
		}
		if err = census.current(ctx, o); err != nil {
			return nil, err
		}
	}
	if startup != nil && (startup.self != startup || startup != o.ref.cell.startup || startup.owner != o || startup.closing || startup.startupUse == nil) {
		return nil, storagebudget.ErrAuthority
	}
	mutation := storageMutationIn(ctx, o)
	if exclusive != nil {
		mutation = exclusive.mutation
	}
	if startup != nil {
		mutation = startup.mutation
	} else if census != nil {
		mutation = census.mutation
	}
	if mutation != nil {
		if err = mutation.current(ctx, false); err != nil {
			return nil, err
		}
		ctx = storageMutationContext(ctx, mutation)
	} else if !o.admissionOpen() {
		return nil, storagebudget.ErrAuthority
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	p = &storageCapacityPass{owner: o, mutation: mutation, exclusive: exclusive, started: time.Now(), samples: map[*os.Root]storageFilesystemSample{}, maxAge: time.Duration(storageAdvisoryInitialMaxAgeNS)}
	p.self = p
	transferred = true
	// Return assignments can clear p before this runs. Retain the exact acquired
	// pass for cleanup, including failures before use acquisition completes.
	defer func(acquired *storageCapacityPass) {
		if err != nil {
			// Q reports canceled lock waits as busy; preserve caller cancellation
			// alongside that refusal and any original-custody cleanup error.
			err = errors.Join(err, ctx.Err(), acquired.Close())
			p = nil
		}
	}(p)
	if exclusive != nil {
		if err = exclusive.held(ctx); err != nil {
			return nil, err
		}
	} else if census != nil {
		p.census = census
		if err = census.current(ctx, o); err != nil {
			return nil, err
		}
	} else if startup == nil {
		p.use, err = o.ref.acquire()
		if err != nil {
			return nil, err
		}
	} else {
		p.startup, p.startupUse = startup, startup.startupUse
		p.startupHolds = append([]*storageDomainHold(nil), startup.startupHolds...)
		if err = p.startupCurrent(ctx); err != nil {
			return nil, err
		}
	}
	view, qe := o.ref.cell.q.InspectHost(ctx)
	if qe != nil {
		return nil, qe
	}
	p.epoch = view.Configuration.Epoch
	if exclusive != nil && (view.Configuration.Maintenance || p.epoch != exclusive.epoch) {
		return nil, storagebudget.ErrStale
	}
	if (startup != nil || census != nil) && !view.Configuration.Maintenance {
		return nil, storagebudget.ErrAuthority
	}
	if census != nil && p.epoch != census.epoch {
		return nil, storagebudget.ErrStale
	}
	o.mu.Lock()
	if o.transitionRequired {
		err = errStorageOwnerTransitionRequired
	}
	if len(o.roots) > storageCapacityPassRoots || len(o.constraints) > storageCapacityPassConstraints+1 {
		err = storagebudget.ErrBounds
	}
	if err == nil {
		for id, r := range o.roots {
			if r == nil || r.owner != o || r.self != r || r.row.ID != id {
				err = storagebudget.ErrAuthority
				break
			}
			p.roots = append(p.roots, r)
		}
	}
	o.mu.Unlock()
	if err != nil {
		return nil, err
	}
	sort.Slice(p.roots, func(i, j int) bool { return string(p.roots[i].row.ID[:]) < string(p.roots[j].row.ID[:]) })
	if len(p.roots) == 0 {
		return nil, storagebudget.ErrUnknown
	}
	if startup != nil {
		if len(p.startupHolds) != len(p.roots) {
			return nil, storagebudget.ErrAuthority
		}
		for i, r := range p.roots {
			if p.startupHolds[i] == nil || p.startupHolds[i].root != r || p.startupHolds[i].lifetime != p.startupUse {
				return nil, storagebudget.ErrAuthority
			}
		}
	}
	if a, ok := o.observer.(*storageAdvisoryObserver); ok && a.maxAge < p.maxAge {
		p.maxAge = a.maxAge
	}
	for _, effect := range effects {
		if effect.binding == nil || effect.binding.owner != o || effect.root == nil {
			return nil, storagebudget.ErrAuthority
		}
		found := false
		for _, r := range p.roots {
			found = found || r == effect.binding
		}
		if !found {
			return nil, storagebudget.ErrAuthority
		}
		parent := effect.binding.root
		for _, n := range effect.witnesses {
			if n.parent != parent {
				return nil, storagebudget.ErrAuthority
			}
			parent = n.root
		}
		if parent != effect.root {
			return nil, storagebudget.ErrAuthority
		}
		// A borrowed parent must be the binding itself or the last exact named child.
		if effect.root != effect.binding.root && (len(effect.witnesses) == 0 || effect.witnesses[len(effect.witnesses)-1].root != effect.root) {
			return nil, storagebudget.ErrAuthority
		}
		p.effects = append(p.effects, storageCapacityEffectParent{effect.binding, effect.root, append([]storageNamedRoot(nil), effect.witnesses...)})
	}
	for _, r := range p.roots {
		if err = p.query(ctx, r, r.root, nil); err != nil {
			return nil, err
		}
	}
	for _, e := range p.effects {
		if err = p.query(ctx, e.binding, e.root, e.witnesses); err != nil {
			return nil, err
		}
	}
	if a, ok := o.observer.(*storageAdvisoryObserver); ok {
		if err = p.derive(a); err != nil {
			return nil, err
		}
	}
	p.completed = time.Now()
	if err = p.current(ctx); err != nil {
		return nil, err
	}
	p.completed = time.Now()
	if p.completed.Sub(p.started) > time.Duration(storageAdvisoryPassDeadlineNS) {
		return nil, storagebudget.ErrStale
	}
	return p, nil
}
func (p *storageCapacityPass) query(ctx context.Context, r *storageRootBinding, root *os.Root, w []storageNamedRoot) error {
	if p.mutation != nil {
		if err := p.mutation.current(ctx, false); err != nil {
			return err
		}
		ctx = storageMutationContext(ctx, p.mutation)
	}
	if p.exclusive != nil {
		if err := p.exclusive.current(ctx); err != nil {
			return err
		}
	}
	if p.census != nil {
		if err := p.census.current(ctx, p.owner); err != nil {
			return err
		}
	}
	if err := p.checkTime(ctx, true); err != nil {
		return err
	}
	if err := r.current(ctx); err != nil {
		return err
	}
	for _, n := range w {
		if err := n.current(); err != nil {
			return err
		}
	}
	if _, ok := p.samples[root]; ok {
		return p.checkTime(ctx, true)
	}
	var s storageFilesystemSample
	var err error
	if a, ok := p.owner.observer.(*storageAdvisoryObserver); ok {
		s, err = a.query(ctx, r, root)
	} else {
		s, err = p.owner.observer.observeStorageFilesystem(ctx, root)
	}
	if err != nil {
		if errors.Is(err, errStorageOwnerTransitionRequired) {
			return p.owner.fenceStorageTransition(ctx)
		}
		return err
	}
	if err = r.current(ctx); err != nil {
		return err
	}
	for _, n := range w {
		if err = n.current(); err != nil {
			return err
		}
	}
	if p.census != nil {
		if err = p.census.current(ctx, p.owner); err != nil {
			return err
		}
	}
	p.samples[root] = s
	return p.checkTime(ctx, true)
}
func (p *storageCapacityPass) checkTime(ctx context.Context, querying bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := time.Now()
	if querying && now.Sub(p.started) > time.Duration(storageAdvisoryPassDeadlineNS) {
		return storagebudget.ErrStale
	}
	if !p.completed.IsZero() && (p.completed.Sub(p.started) > time.Duration(storageAdvisoryPassDeadlineNS) || now.Sub(p.started) > p.maxAge) {
		return storagebudget.ErrStale
	}
	return nil
}
func (p *storageCapacityPass) current(ctx context.Context) error {
	if p == nil || p.self != p {
		return storagebudget.ErrAuthority
	}
	if p.mutation != nil {
		if err := p.mutation.current(ctx, false); err != nil {
			return err
		}
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return os.ErrClosed
	}
	if err := p.checkTime(ctx, false); err != nil {
		return err
	}
	if p.exclusive != nil {
		if err := p.exclusive.held(ctx); err != nil {
			return err
		}
	} else if p.census != nil {
		if err := p.census.current(ctx, p.owner); err != nil {
			return err
		}
	} else if p.startup != nil {
		if err := p.startupCurrent(ctx); err != nil {
			return err
		}
	} else {
		if err := p.use.Current(); err != nil {
			return err
		}
	}
	p.owner.mu.Lock()
	changed := p.owner.transitionRequired || len(p.roots) != len(p.owner.roots)
	for _, r := range p.roots {
		if p.owner.roots[r.row.ID] != r {
			changed = true
		}
	}
	p.owner.mu.Unlock()
	if changed {
		return errStorageOwnerTransitionRequired
	}
	view, err := p.owner.ref.cell.q.InspectHost(ctx)
	if err != nil {
		return err
	}
	if p.exclusive != nil && view.Configuration.Maintenance {
		return storagebudget.ErrStale
	}
	if view.Configuration.Epoch != p.epoch {
		return storagebudget.ErrStale
	}
	if (p.startup != nil || p.census != nil) && !view.Configuration.Maintenance {
		return storagebudget.ErrStale
	}
	for _, r := range p.roots {
		if err := r.current(ctx); err != nil {
			return err
		}
	}
	for _, e := range p.effects {
		for _, n := range e.witnesses {
			if err := n.current(); err != nil {
				return err
			}
		}
	}
	if p.exclusive != nil {
		return p.exclusive.current(ctx)
	}
	if p.census != nil {
		return p.census.current(ctx, p.owner)
	}
	return nil
}

func (p *storageCapacityPass) startupCurrent(ctx context.Context) error {
	c := p.startup
	if c == nil || c.self != c || c != p.owner.ref.cell.startup || c.owner != p.owner || c.closing || c.startupUse != p.startupUse || p.startupUse == nil || p.startupUse.storageCell() != p.owner.ref.cell || len(c.startupHolds) != len(p.startupHolds) {
		return storagebudget.ErrAuthority
	}
	if err := c.current(ctx, true); err != nil {
		return err
	}
	if err := c.home.requireFixedZero(ctx); err != nil {
		return err
	}
	if err := p.startupUse.Current(); err != nil {
		return err
	}
	for i, h := range p.startupHolds {
		if h == nil || h != c.startupHolds[i] || h.owner != p.owner || h.lifetime != p.startupUse {
			return storagebudget.ErrAuthority
		}
		if err := h.current(ctx); err != nil {
			return err
		}
	}
	return nil
}
func (p *storageCapacityPass) Close() error {
	if p == nil || p.self != p {
		return storagebudget.ErrAuthority
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	var err error
	if p.use != nil {
		err = p.use.Close()
	}
	if p.exclusive != nil {
		return errors.Join(err, p.exclusive.dropBorrow())
	}
	return errors.Join(err, p.owner.drop())
}
func (p *storageCapacityPass) context(ctx context.Context) context.Context {
	if p.mutation != nil {
		ctx = storageMutationContext(ctx, p.mutation)
	}
	return context.WithValue(ctx, storageCapacityPassKey{}, p)
}
func storagePassFrom(ctx context.Context, o *storageBudgetOwner) (*storageCapacityPass, error) {
	p, _ := ctx.Value(storageCapacityPassKey{}).(*storageCapacityPass)
	if p == nil || p.owner != o {
		return nil, storagebudget.ErrAuthority
	}
	return p, p.current(ctx)
}

// A positive immutable relation discovery invalidates every local pass first.
// Existing Q maintenance performs only its normal terminal incorporation; no
// definition/Register/Repair/rebind or promise rewrite is permitted here.
func (o *storageBudgetOwner) fenceStorageTransition(ctx context.Context) error {
	o.mu.Lock()
	o.transitionRequired = true
	o.mu.Unlock()
	if err := o.requireFenceMutation(ctx); err != nil {
		return errors.Join(errStorageOwnerTransitionRequired, errStorageOwnerRecovery, err)
	}
	view, err := o.ref.cell.q.InspectHost(ctx)
	if err != nil {
		return errors.Join(errStorageOwnerTransitionRequired, errStorageOwnerRecovery, err)
	}
	if !view.Configuration.Maintenance {
		if err = o.requireFenceMutation(ctx); err == nil {
			_, err = o.ref.cell.q.BeginMaintenance(ctx, view.Configuration.Revision)
		}
	}
	if err != nil {
		return errors.Join(errStorageOwnerTransitionRequired, errStorageOwnerRecovery, err)
	}
	return errStorageOwnerTransitionRequired
}

// Construction is private to original owner reattachment/enrollment. The catalog
// is already validated against original owner/claim custody by that lifecycle;
// detached profile evidence alone cannot select a backend or invent a root.
// New/startup wiring and original catalog construction belong to the next batch.
type storageAdvisoryObserver struct {
	owner       *storageBudgetOwner
	catalog     storageOwnerCatalogV2
	roots       map[storagebudget.ID]storageOwnerRoot
	filesystems map[storagebudget.ID]storageOwnerFilesystem
	constraints map[storagebudget.ID]storageOwnerConstraint
	maxAge      time.Duration
}

func (o *storageBudgetOwner) installStorageAdvisoryObserver(c storageOwnerCatalogV2, record storageOwnerRecordV2, claim storageOwnerClaimV2) error {
	if !o.valid() || c.Version != 3 || c.Installation != o.ref.cell.record.Installation {
		return storagebudget.ErrAuthority
	}
	if record.QBinding != o.ref.cell.binding {
		return storagebudget.ErrAuthority
	}
	if err := c.validateVersion(record, claim, 3); err != nil {
		return err
	}
	view, err := o.ref.cell.q.InspectHost(context.Background())
	if err != nil {
		return err
	}
	if c.Phase == "active" && view.Configuration.Maintenance {
		return errStorageOwnerRecovery
	}
	if err := storageAdvisoryPlatformProfiles(c.Profiles); err != nil {
		return err
	}
	a := &storageAdvisoryObserver{owner: o, catalog: c, roots: map[storagebudget.ID]storageOwnerRoot{}, filesystems: map[storagebudget.ID]storageOwnerFilesystem{}, constraints: map[storagebudget.ID]storageOwnerConstraint{}, maxAge: time.Duration(c.Policy.ObservationMaxAgeNanos)}
	a.catalog.Roots = append([]storageOwnerRoot(nil), c.Roots...)
	a.catalog.Objects = append([]storageOwnerObject(nil), c.Objects...)
	for _, r := range c.Roots {
		a.roots[r.Definition.ID] = r
	}
	for _, f := range c.Filesystems {
		a.filesystems[f.Definition.ID] = f
	}
	for _, v := range c.Constraints {
		a.constraints[v.Definition.ID] = v
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.active != 0 || o.censusEpoch != 0 || o.closing || len(o.roots) != 0 {
		return storagebudget.ErrBusy
	}
	o.observer = a
	return nil
}
func (a *storageAdvisoryObserver) observeStorageFilesystem(ctx context.Context, root *os.Root) (storageFilesystemSample, error) {
	p, err := storagePassFrom(ctx, a.owner)
	if err != nil {
		return storageFilesystemSample{}, err
	}
	if phase := a.owner.exclusiveCensusPhase(); phase != nil && p.exclusive != phase {
		return storageFilesystemSample{}, storagebudget.ErrAuthority
	}
	ctx = p.context(ctx)
	s, ok := p.samples[root]
	if !ok {
		return s, storagebudget.ErrAuthority
	}
	var binding *storageRootBinding
	for _, r := range p.roots {
		if r.root == root {
			binding = r
			break
		}
	}
	if binding == nil {
		for _, e := range p.effects {
			if e.root == root {
				binding = e.binding
				break
			}
		}
	}
	if binding == nil {
		return s, storagebudget.ErrAuthority
	}
	// Cached numbers cannot cache mutable custody, mapping or applicability.
	if _, err = a.query(ctx, binding, root); err != nil {
		if errors.Is(err, errStorageOwnerTransitionRequired) {
			err = a.owner.fenceStorageTransition(ctx)
		}
		return storageFilesystemSample{}, err
	}
	s.Constraints = append([]storagebudget.CapacityConstraint(nil), s.Constraints...)
	s.Observations = append([]storagebudget.CapacityObservation(nil), s.Observations...)
	return s, p.current(ctx)
}
func (a *storageAdvisoryObserver) storageCapacityEnvelope(ctx context.Context, root *os.Root, d storageCapacityDemand) (out [storagebudget.MaxCapacityRefs]storagebudget.CapacityVector, err error) {
	s, err := a.observeStorageFilesystem(ctx, root)
	if err != nil {
		return out, err
	}
	p, e := storagePassFrom(ctx, a.owner)
	if e != nil {
		return out, e
	}
	if e = p.claimGroup(d); e != nil {
		return out, e
	}
	b, f, n := d.Physical.Bytes, d.Physical.Files, d.Physical.Directories
	for _, pair := range [][2]uint64{{b, d.Settlement.Bytes}, {f, d.Settlement.Files}, {n, d.Settlement.Directories}} {
		if pair[0] > ^uint64(0)-pair[1] {
			return out, storagebudget.ErrBounds
		}
	}
	peak, err := storageAdvisoryEstimate(b+d.Settlement.Bytes, f+d.Settlement.Files, n+d.Settlement.Directories, d.MetadataOps)
	if err != nil || peak.Bytes > 1<<60 || peak.Inodes > 1<<40 {
		return out, errors.Join(err, storagebudget.ErrBounds)
	}
	for i, row := range s.Constraints {
		if row.Resources&storagebudget.CapacityBytes != 0 {
			out[i].Bytes = peak.Bytes
		}
		if row.Resources&storagebudget.CapacityInodes != 0 {
			out[i].Inodes = peak.Inodes
		}
	}
	return out, nil
}
func (a *storageAdvisoryObserver) exactCatalogBinding(r *storageRootBinding) (storageOwnerRoot, bool, error) {
	if a == nil || r == nil {
		return storageOwnerRoot{}, false, storagebudget.ErrAuthority
	}
	original, ok := a.roots[r.row.ID]
	if !ok {
		return storageOwnerRoot{}, false, storagebudget.ErrAuthority
	}
	// Explicit-ID observations retain the exact catalog row. Reverse lookup by
	// physical native would conflate an owner with disjoint nonowning views.
	provisional := r.self == nil && r.root == nil && r.source == nil && r.row == (storagebudget.Root{ID: original.Definition.ID})
	if !provisional {
		if r.self != r || r.owner != a.owner || storageOwnerRootValue(original.Definition) != r.row || len(original.References) != len(r.references) || int(original.DirectoryObject) >= len(a.catalog.Objects) || r.source != nil && r.source.persistent != a.catalog.Objects[original.DirectoryObject].Persistent {
			return storageOwnerRoot{}, false, storagebudget.ErrAuthority
		}
		for i, id := range original.References {
			if r.references[i] != id {
				return storageOwnerRoot{}, false, storagebudget.ErrAuthority
			}
		}
		if storageTransferCatalogView(original) {
			if _, err := storageTransferNamespaceRelations(a.catalog); err != nil {
				return storageOwnerRoot{}, false, err
			}
			if !storageTransferNamespaceBinding(r) || r.transferInactive != (original.Source.ControlSelection == 4) {
				return storageOwnerRoot{}, false, storagebudget.ErrAuthority
			}
			raw, err := storageEnrollmentPeakEncodeSource(*r.source.scope)
			if err != nil || string(raw) != string(original.Source.Relative) {
				return storageOwnerRoot{}, false, errors.Join(storagebudget.ErrAuthority, err)
			}
		}
	} else if storageTransferCatalogView(original) {
		return storageOwnerRoot{}, false, storagebudget.ErrAuthority
	}
	return original, provisional, nil
}
func (a *storageAdvisoryObserver) query(ctx context.Context, r *storageRootBinding, root *os.Root) (storageFilesystemSample, error) {
	original, provisional, joinErr := a.exactCatalogBinding(r)
	if joinErr != nil {
		return storageFilesystemSample{}, joinErr
	}
	fs := a.filesystems[original.Definition.Filesystem]
	s := storageFilesystemSample{Filesystem: fs.Definition, RootGeneration: original.Definition.Generation, CapacityCount: original.Definition.CapacityCount, CapacityRefs: original.Definition.CapacityRefs}
	for i := 0; i < int(s.CapacityCount); i++ {
		v, ok := a.constraints[s.CapacityRefs[i].Constraint]
		if !ok {
			return s, storagebudget.ErrUnknown
		}
		s.Constraints = append(s.Constraints, v.Definition)
	}
	// Open a normal held description relative to the sealed original root. Native
	// leaves borrow that exact description; no pathname reopening or O_PATH query.
	held, err := root.Open(".")
	if err != nil {
		return s, err
	}
	if provisional || root == r.root {
		native, e := storageAdvisoryPersistentHeld(ctx, held)
		if e != nil || int(original.DirectoryObject) >= len(a.catalog.Objects) || native != a.catalog.Objects[original.DirectoryObject].Persistent {
			return s, errors.Join(errStorageCustody, e, held.Close())
		}
	}
	facts, err := storageAdvisoryNativeView(ctx, held, fs, original.Membership, a.constraints)
	err = errors.Join(err, held.Close())
	if err != nil {
		return s, err
	}
	s.PhysicalStatus = facts.status
	s.NativeAvailable = facts.available
	s.NativeTotalBytes = facts.totalBytes
	s.Observations = facts.exact
	return s, nil
}

type storageAdvisoryNativeFacts struct {
	status     storagePhysicalStatus
	available  storagebudget.CapacityVector
	totalBytes uint64
	exact      []storagebudget.CapacityObservation
}

func storageAdvisoryStatus(at time.Time) storagePhysicalStatus {
	return storagePhysicalStatus{PolicyID: "desk-advisory-allocation-v2", PolicyVersion: 2, Mode: storagePhysicalAdvisory, Oldest: at, Reason: storagePhysicalReasonDetailsUnknown}
}
func (p *storageCapacityPass) derive(a *storageAdvisoryObserver) error {
	available := storagebudget.CapacityVector{Bytes: 1 << 60, Inodes: storageAdvisoryInodePolicyCap}
	var oldest time.Time
	for _, s := range p.samples {
		if !s.PhysicalStatus.validAdvisory() || s.PhysicalStatus.Bytes != storagePhysicalCounterMeasured {
			return storagebudget.ErrUnknown
		}
		if oldest.IsZero() || s.PhysicalStatus.Oldest.Before(oldest) {
			oldest = s.PhysicalStatus.Oldest
		}
		h := max(storageAdvisoryHeadroomMinBytes, min(storageAdvisoryHeadroomMaxBytes, s.NativeTotalBytes/storageAdvisoryHeadroomDivisor))
		h = max(h, p.owner.options.HeadroomBytes)
		if s.NativeAvailable.Bytes < h {
			return storagebudget.ErrHeadroom
		}
		available.Bytes = min(available.Bytes, s.NativeAvailable.Bytes-h)
		switch s.PhysicalStatus.Inodes {
		case storagePhysicalCounterMeasured:
			h = max(storageAdvisoryInodeHeadroomFloor, p.owner.options.HeadroomInodes)
			v := uint64(0)
			if s.NativeAvailable.Inodes > h {
				v = s.NativeAvailable.Inodes - h
			}
			available.Inodes = min(available.Inodes, v)
		case storagePhysicalCounterDynamic:
		default:
			return storagebudget.ErrUnknown
		}
	}
	if oldest.IsZero() || time.Since(oldest) > p.maxAge || oldest.After(time.Now()) {
		return storagebudget.ErrStale
	}
	policy := storageOwnerAdvisoryConstraint(a.catalog.Installation).Definition
	obs := storagebudget.CapacityObservation{Constraint: policy.ID, Generation: policy.Generation, KnownResources: policy.Resources, Available: available, At: oldest}
	for root, s := range p.samples {
		s.Observations = append(s.Observations, obs)
		sort.Slice(s.Observations, func(i, j int) bool {
			return string(s.Observations[i].Constraint[:]) < string(s.Observations[j].Constraint[:])
		})
		s.PhysicalStatus.Oldest = oldest
		p.samples[root] = s
	}
	return nil
}

// No caller can create a concrete owner from an arbitrary root path. This
// constructor requires the retained actual original installation and exact
// catalog/record/claim join; it does not install anything in New.
func newStorageAdvisoryBudgetOwner(ref *storageInstallationRef, c storageOwnerCatalogV2, record storageOwnerRecordV2, claim storageOwnerClaimV2, opt storageBudgetOptions) (*storageBudgetOwner, error) {
	o, err := newStorageBudgetOwner(ref, &storageAdvisoryObserver{}, opt)
	if err != nil {
		return nil, err
	}
	if err = o.installStorageAdvisoryObserver(c, record, claim); err != nil {
		return nil, errors.Join(err, o.Close())
	}
	return o, nil
}

func (a *storageAdvisoryObserver) originalRoot(ctx context.Context, root *os.Root) (storageOwnerRoot, error) {
	held, err := root.Open(".")
	if err != nil {
		return storageOwnerRoot{}, err
	}
	object, err := storageAdvisoryPersistentHeld(ctx, held)
	err = errors.Join(err, held.Close())
	if err != nil {
		return storageOwnerRoot{}, err
	}
	return a.originalOwningNative(object)
}

func (a *storageAdvisoryObserver) originalOwningNative(object storageOwnerPersistentObject) (storageOwnerRoot, error) {
	var found storageOwnerRoot
	if _, err := storageTransferNamespaceRelations(a.catalog); err != nil {
		return storageOwnerRoot{}, err
	}
	for _, r := range a.catalog.Roots {
		// This constructor is for ordinary owning roots. A view's exact ID may
		// only be supplied by the existing catalog-derived binding constructor.
		if storageTransferCatalogView(r) {
			continue
		}
		if int(r.DirectoryObject) >= len(a.catalog.Objects) {
			return storageOwnerRoot{}, storagebudget.ErrAuthority
		}
		if a.catalog.Objects[r.DirectoryObject].Persistent == object {
			if found.Definition.ID != (storagebudget.ID{}) {
				return storageOwnerRoot{}, storagebudget.ErrAuthority
			}
			found = r
		}
	}
	if found.Definition.ID == (storagebudget.ID{}) {
		return found, errStorageCustody
	}
	return found, nil
}

func (p *storageCapacityPass) claimGroup(d storageCapacityDemand) error {
	if d.Physical == (storagebudget.Vector{}) && d.Settlement == (storagebudget.Vector{}) && d.MetadataOps == 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return os.ErrClosed
	}
	if p.groups >= 2*storagebudget.MaxComponents {
		return storagebudget.ErrBounds
	}
	p.groups++
	return nil
}

// This is a subject-selection input, not evidence of enabled quotas, qualified
// namespace mapping or unlimited resources. Existing metadata effects and
// prospective creation have distinct charging subjects on the same descriptor.
type storageAdvisoryObjectUse uint8

const (
	storageAdvisoryProspectiveChild storageAdvisoryObjectUse = iota + 1
	storageAdvisoryExistingObject
)
const storageAdvisoryExt4ProjectInherit uint32 = 0x00000200 // FS_XFLAG_PROJINHERIT

func storageAdvisoryExt4QuotaSubjects(object storageNativeLinuxObjectRaw, ext storageNativeExt4Raw, euid, egid uint32, use storageAdvisoryObjectUse) ([3]uint32, error) {
	switch use {
	case storageAdvisoryProspectiveChild:
		ids := [3]uint32{euid, egid, 0}
		if object.Mode&02000 != 0 {
			ids[1] = object.GID
		}
		// With no inheritance the new inode uses ext4's default project 0. That is
		// an exact query subject here, never an invented disabled/unlimited row.
		if ext.ParentXFlags&storageAdvisoryExt4ProjectInherit != 0 {
			ids[2] = ext.ParentProjectID
		}
		return ids, nil
	case storageAdvisoryExistingObject:
		return [3]uint32{object.UID, object.GID, ext.ParentProjectID}, nil
	default:
		return [3]uint32{}, storagebudget.ErrUnknown
	}
}

// Compare known enrolled subject mappings before any availability/discovery
// query can short-circuit. Removal of PROJINHERIT can change the creation
// project even if a later status getter is denied or reports accounting off.
func storageAdvisoryValidateQuotaSubjects(m storageOwnerMembership, ids [3]uint32) error {
	for i, s := range []storageOwnerSubject{m.User, m.Group, m.Project} {
		if s.State >= 1 && s.State <= 3 && s.Subject != ids[i] {
			return errStorageOwnerTransitionRequired
		}
	}
	return nil
}

// A newly positive volume restriction requires an original-owner transition
// even while its remaining/pending schema is unavailable. Already enrolled
// exact rows keep refusing their unreadable counters without deleting the row.
func storageAdvisoryAPFSRestriction(v storageNativeDarwinVolumeRaw, m storageOwnerMembership, rows map[storagebudget.ID]storageOwnerConstraint, enrolled bool) error {
	positive := v.Quota != 0 || v.Reserve != 0
	if !enrolled {
		if positive {
			return storagebudget.ErrUnknown
		}
		return nil
	}
	knownVolume, knownContainer := false, false
	for _, row := range rows {
		knownVolume = knownVolume || (row.Kind == 6 && row.VolumeUUID == m.VolumeUUID && row.ContainerUUID == m.ContainerUUID)
		knownContainer = knownContainer || (row.Kind == 5 && row.ContainerUUID == m.ContainerUUID)
	}
	if positive && !knownVolume {
		return errStorageOwnerTransitionRequired
	}
	if knownVolume || knownContainer {
		return storagebudget.ErrUnknown
	}
	return nil
}

func storageAdvisoryConfirmQuotaSubjects(m storageOwnerMembership, before, after [3]uint32) error {
	if err := storageAdvisoryValidateQuotaSubjects(m, after); err != nil {
		return err
	}
	if before != after {
		return storagebudget.ErrUnknown
	}
	return nil
}

func storageAdvisoryLinuxTargetJoin(parent, target storageNativeLinuxObjectRaw, parentUUID, targetUUID [16]byte) error {
	if parentUUID == ([16]byte{}) || targetUUID != parentUUID || parent.MountID == 0 || target.MountID != parent.MountID || parent.MountKind != target.MountKind || parent.DeviceMajor != target.DeviceMajor || parent.DeviceMinor != target.DeviceMinor {
		return errStorageCustody
	}
	if parent.MountKind != storageNativeMountOrdinary && parent.MountKind != storageNativeMountUnique {
		return storagebudget.ErrUnknown
	}
	return nil
}
func storageAdvisoryDarwinTargetJoin(parent, target storageNativeDarwinTypedObjectRaw) error {
	if parent.Kind != storageNativeObjectDirectory || target.Kind != storageNativeObjectRegular || parent.RealFSID == ([2]int32{}) || parent.RealFSID != target.RealFSID || parent.RealDevice != target.RealDevice {
		return errStorageCustody
	}
	return nil
}

func storageAdvisoryLaunchQuotaRefusal(raw storageNativeQuotaDiskRaw) error {
	r, err := storageNativeQuotaExt4Remaining(raw)
	if err != nil {
		if raw.HardBlocks != 0 || raw.SoftBlocks != 0 || raw.HardInodes != 0 || raw.SoftInodes != 0 {
			return storagebudget.ErrUnknown
		}
		// Unqualified extra discovery remains Unknown. This grants no disabled or
		// unlimited claim and constructs no enrolled row.
		return nil
	}
	if r.Bytes.State == storageNativeQuotaFinite || r.Inodes.State == storageNativeQuotaFinite {
		return storagebudget.ErrUnknown
	}
	return nil
}
