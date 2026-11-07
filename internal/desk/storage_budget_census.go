package desk

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

const (
	storageCensusEntries = 65536
	storageCensusDepth   = 64
	storageCensusName    = 1024
	storageCensusTime    = 30 * time.Second
)

func storageVectorAdd(a, b storagebudget.Vector) (storagebudget.Vector, error) {
	if a.Bytes > 1<<60 || b.Bytes > 1<<60-a.Bytes || a.Files > 1<<40 || b.Files > 1<<40-a.Files || a.Directories > 1<<40 || b.Directories > 1<<40-a.Directories {
		return storagebudget.Vector{}, storagebudget.ErrBounds
	}
	return storagebudget.Vector{Bytes: a.Bytes + b.Bytes, Files: a.Files + b.Files, Directories: a.Directories + b.Directories}, nil
}
func storageVectorLE(a, b storagebudget.Vector) bool {
	return a.Bytes <= b.Bytes && a.Files <= b.Files && a.Directories <= b.Directories
}
func storageCensusPrivateFile(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode() & ^os.FileMode(0777) != 0 || info.Mode().Perm()&0077 != 0 || info.Size() < 0 {
		return false
	}
	uid, ok := ownerOf(info)
	links, known := linkCount(info)
	return ok && known && uid == effectiveUser() && links > 0
}

// The census rule is custody's rule for what Desk reads (readConfigNamed): a
// regular file or a directory, owned by the user running Desk, that neither
// group nor others may write. Group and other read are not refused, so a file
// at the mode an editor, a checkout or the person's umask leaves (0644, 0755)
// is measured and its bytes counted, wherever it is under a registered root.
// Another user's entry, a link where a file or directory is expected, a special
// file, special mode bits, and anything group or others may write are refused,
// and the census refuses the installation over it (storage starts read-only).
// storageCensusPrivateFile stays the stricter rule for the files Desk writes
// and reopens itself.
func storageCensusMeasuredFile(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode() & ^os.FileMode(0777) != 0 || info.Mode().Perm()&worldMode != 0 || info.Size() < 0 {
		return false
	}
	uid, ok := ownerOf(info)
	links, known := linkCount(info)
	return ok && known && uid == effectiveUser() && links > 0
}

// storageCensusMeasuredDirectory is the same rule for a directory, which the
// owner must also be able to list and enter.
func storageCensusMeasuredDirectory(info os.FileInfo) bool {
	if !custodyChecked || info == nil || !info.IsDir() || info.Mode() & ^(os.ModeDir|os.FileMode(0777)) != 0 || info.Mode().Perm()&0700 != 0700 || info.Mode().Perm()&worldMode != 0 {
		return false
	}
	uid, ok := ownerOf(info)
	return ok && uid == effectiveUser()
}
func storageUnretired(slot storagebudget.HostSlotState) bool {
	return slot.Ticket.State != storagebudget.Empty && !(slot.Ticket.State == storagebudget.Terminal && slot.Ticket.Token == slot.PersistedApplied)
}
func (o *storageBudgetOwner) orderedRoots() []*storageRootBinding {
	roots := make([]*storageRootBinding, 0, len(o.roots))
	for _, r := range o.roots {
		roots = append(roots, r)
	}
	sort.Slice(roots, func(i, j int) bool {
		return strings.Compare(string(roots[i].row.ID[:]), string(roots[j].row.ID[:])) < 0
	})
	return roots
}
func storageRegisteredRoot(r storagebudget.Root) storagebudget.RegisteredRoot {
	return storagebudget.RegisteredRoot{ID: r.ID, Owner: r.Owner, Identity: r.Identity, Filesystem: r.Filesystem, Workspace: r.Workspace, Generation: r.Generation, Locator: r.Locator, CapacityCount: r.CapacityCount, CapacityRefs: r.CapacityRefs, CapacityBinding: r.CapacityBinding}
}
func storageHostState(v storagebudget.HostInspection) storagebudget.HostState {
	return storagebudget.HostState{Revision: v.Configuration.Revision, Epoch: v.Configuration.Epoch, Maintenance: v.Configuration.Maintenance, Slots: v.Slots}
}
func storageRegistrationCount(r storagebudget.Registration) int {
	return len(r.Roots) + len(r.Workspaces) + len(r.Filesystems) + len(r.CapacityConstraints)
}

// Pure four-table comparison from one validated revision. No persisted usage or
// Known bit is compared with an original definition, and no unknown row is lost.
func (o *storageBudgetOwner) registrationMissingFrom(v storagebudget.HostInspection) (storagebudget.Registration, error) {
	var add storagebudget.Registration
	fs := map[storagebudget.ID]bool{}
	constraints := map[storagebudget.ID]bool{}
	work := map[storagebudget.ID]bool{}
	roots := map[storagebudget.ID]bool{}
	for _, row := range v.Configuration.Filesystems {
		ours, ok := o.filesystems[row.ID]
		if !ok || ours != row || fs[row.ID] {
			return add, storagebudget.ErrUnknown
		}
		fs[row.ID] = true
	}
	for _, row := range v.Configuration.CapacityConstraints {
		ours, ok := o.constraints[row.ID]
		if !ok || ours != row || constraints[row.ID] {
			return add, storagebudget.ErrUnknown
		}
		constraints[row.ID] = true
	}
	for _, row := range v.Workspaces {
		ours, ok := o.workspaces[row.ID]
		if ours.Limits == (storagebudget.Limits{}) {
			ours.Limits = v.Configuration.Policy.Workspace
		}
		if !ok || ours.Generation != row.Generation || ours.Limits != row.Limits || work[row.ID] {
			return add, storagebudget.ErrUnknown
		}
		work[row.ID] = true
	}
	for _, row := range v.Roots {
		ours, ok := o.roots[row.ID]
		if !ok || storageRegisteredRoot(ours.row) != row || roots[row.ID] {
			return add, storagebudget.ErrUnknown
		}
		roots[row.ID] = true
	}
	for id, row := range o.filesystems {
		if !fs[id] {
			add.Filesystems = append(add.Filesystems, row)
		}
	}
	for id, row := range o.constraints {
		if !constraints[id] {
			add.CapacityConstraints = append(add.CapacityConstraints, row)
		}
	}
	for id, row := range o.workspaces {
		if row.Limits == (storagebudget.Limits{}) {
			row.Limits = v.Configuration.Policy.Workspace
		}
		if !work[id] {
			add.Workspaces = append(add.Workspaces, row)
		}
	}
	for _, r := range o.orderedRoots() {
		if !roots[r.row.ID] {
			add.Roots = append(add.Roots, r.row)
		}
	}
	if len(o.roots) > storagebudget.MaxRoots || len(o.workspaces) > storagebudget.MaxWorkspaces || len(o.filesystems) > storagebudget.MaxFilesystems || len(o.constraints) > storagebudget.MaxCapacityConstraints {
		return storagebudget.Registration{}, storagebudget.ErrBounds
	}
	sort.Slice(add.Filesystems, func(i, j int) bool { return string(add.Filesystems[i].ID[:]) < string(add.Filesystems[j].ID[:]) })
	sort.Slice(add.Workspaces, func(i, j int) bool { return string(add.Workspaces[i].ID[:]) < string(add.Workspaces[j].ID[:]) })
	sort.Slice(add.CapacityConstraints, func(i, j int) bool {
		return string(add.CapacityConstraints[i].ID[:]) < string(add.CapacityConstraints[j].ID[:])
	})
	return add, nil
}
func (o *storageBudgetOwner) registrationMissing(ctx context.Context) (storagebudget.Registration, uint64, error) {
	v, e := o.ref.cell.q.InspectHost(ctx)
	if e != nil {
		return storagebudget.Registration{}, 0, e
	}
	add, e := o.registrationMissingFrom(v)
	return add, v.Configuration.Revision, e
}

// Every phase reopens the original journal through withControl and loads Q once.
// Outer home/use/domain custody remains with InitialCensus across all phases.
// The shared categoryOf policy may reclassify recognized Memory/Attachments
// only after this original-home EX + complete-census route. Every unresolved
// old-category Q promise and nonreusable host record refuses before measurement
// can replace usage; no per-category subtraction or forgiveness occurs here.
func (o *storageBudgetOwner) inspectInitialCensusPhase(ctx context.Context, t *storageIntentTable, epoch uint64, holds []*storageDomainHold) (storagebudget.HostInspection, storagebudget.Registration, error) {
	var zero storagebudget.HostInspection
	var missing storagebudget.Registration
	v, e := o.ref.cell.q.InspectHost(ctx)
	if e != nil {
		return zero, missing, e
	}
	if !v.Configuration.Maintenance || v.Configuration.Epoch != epoch {
		return zero, missing, storagebudget.ErrStale
	}
	missing, e = o.registrationMissingFrom(v)
	if e != nil {
		return zero, storagebudget.Registration{}, e
	}
	if e = o.validateIntentMappings(ctx, t, storageHostState(v)); e != nil {
		return zero, storagebudget.Registration{}, e
	}
	for _, slot := range v.Slots {
		if storageUnretired(slot) {
			return zero, storagebudget.Registration{}, storagebudget.ErrBusy
		}
	}
	for _, slot := range t.slots {
		if !slot.record.reusable() {
			return zero, storagebudget.Registration{}, errStorageOwnerRecovery
		}
	}
	roots := o.orderedRoots()
	if len(holds) != len(roots) {
		return zero, storagebudget.Registration{}, storagebudget.ErrAuthority
	}
	for i, h := range holds {
		if h == nil || h.root != roots[i] || h.owner != o {
			return zero, storagebudget.Registration{}, storagebudget.ErrAuthority
		}
		if e = h.current(ctx); e != nil {
			return zero, storagebudget.Registration{}, e
		}
	}
	if e = o.ref.cell.current(); e != nil {
		return zero, storagebudget.Registration{}, e
	}
	return v, missing, nil
}
func (o *storageBudgetOwner) registerInitialCensus(ctx context.Context, epoch uint64, holds []*storageDomainHold) error {
	return o.withControl(ctx, func(ctx context.Context, t *storageIntentTable) error {
		v, add, e := o.inspectInitialCensusPhase(ctx, t, epoch, holds)
		if e != nil {
			return e
		}
		if storageRegistrationCount(add) == 0 {
			return nil
		}
		if o.ref.cell.ownerRecord != nil {
			return errStorageOwnerRecovery
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if e = o.requireMutation(ctx); e != nil {
			return e
		}
		return o.ref.cell.q.Register(ctx, v.Configuration.Revision, add)
	})
}
func storageCensusMatches(v storagebudget.HostInspection, c storagebudget.Census) bool {
	if len(c.Roots) != len(v.Roots) || len(c.Workspaces) != len(v.Workspaces) {
		return false
	}
	roots := map[storagebudget.ID]uint64{}
	work := map[storagebudget.ID]uint64{}
	for _, r := range v.Roots {
		roots[r.ID] = r.Generation
	}
	for _, w := range v.Workspaces {
		work[w.ID] = w.Generation
	}
	for _, r := range c.Roots {
		if roots[r.ID] != r.Generation || r.Generation == 0 {
			return false
		}
		delete(roots, r.ID)
	}
	for _, w := range c.Workspaces {
		if work[w.ID] != w.Generation || w.Generation == 0 {
			return false
		}
		delete(work, w.ID)
	}
	return len(roots) == 0 && len(work) == 0
}
func (o *storageBudgetOwner) publishInitialCensus(ctx context.Context, epoch uint64, holds []*storageDomainHold, c storagebudget.Census) error {
	return o.withControl(ctx, func(ctx context.Context, t *storageIntentTable) error {
		v, add, e := o.inspectInitialCensusPhase(ctx, t, epoch, holds)
		if e != nil {
			return e
		}
		if storageRegistrationCount(add) != 0 || !storageCensusMatches(v, c) {
			return storagebudget.ErrUnknown
		}
		if e = o.fail("census", "before-publish"); e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if o.ref.cell.startup != nil {
			if e = o.ref.cell.startup.reserveOwnerEffect(ctx, o.ref.cell.anchor, nil, "anchor/census"); e != nil {
				return e
			}
		}
		if e = o.requireMutation(ctx); e != nil {
			return e
		}
		return o.ref.cell.q.PublishCensus(ctx, epoch, c)
	})
}

// This token is minted only by the actual censusMu-held generic invocation,
// after its original EX prefix and phase validation. Its fields are immutable;
// mu serializes currentness with revocation before the invocation closes holds.
// Lock order is token.mu -> owner.mu (released before cell/Q checks). No caller
// takes token.mu while holding owner.mu, controlMu or the installation mutex.
type storageInitialCensusAuthority struct {
	self     *storageInitialCensusAuthority
	mu       sync.Mutex
	active   bool
	owner    *storageBudgetOwner
	mutation *storageMutationAuthority
	ref      *storageInstallationRef
	cell     *storageInstallation
	home     storageNamedFile
	use      *storageMaintenanceUse
	roots    []*storageRootBinding
	holds    []*storageDomainHold
	epoch    uint64
}

func (c *storageInitialCensusAuthority) current(ctx context.Context, o *storageBudgetOwner) error {
	if c == nil || c.self != c || o == nil || c.owner != o {
		return storagebudget.ErrAuthority
	}
	if err := c.mutation.current(ctx, true); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active || !o.valid() || o.ref != c.ref || o.ref.cell != c.cell || c.use == nil || c.use.storageCell() != c.cell || c.epoch == 0 {
		return storagebudget.ErrAuthority
	}
	o.mu.Lock()
	changed := o.currentCensus != c || o.transitionRequired || len(o.roots) != len(c.roots) || len(c.holds) != len(c.roots)
	for _, r := range c.roots {
		if r == nil || r.owner != o || r.self != r || o.roots[r.row.ID] != r {
			changed = true
		}
	}
	o.mu.Unlock()
	if changed {
		return storagebudget.ErrAuthority
	}
	if c.home.root != c.cell.original.config || c.home.name != ".home-use.lock" || c.home.file == nil {
		return storagebudget.ErrAuthority
	}
	if err := c.home.current(); err != nil {
		return err
	}
	if err := c.use.Current(); err != nil {
		return err
	}
	for i, h := range c.holds {
		if h == nil || h.owner != o || h.root != c.roots[i] || h.lifetime != c.use {
			return storagebudget.ErrAuthority
		}
		if err := h.current(ctx); err != nil {
			return err
		}
	}
	view, err := c.cell.q.InspectHost(ctx)
	if err != nil {
		return err
	}
	if !view.Configuration.Maintenance || view.Configuration.Epoch != c.epoch {
		return storagebudget.ErrStale
	}
	return nil
}

func (c *storageInitialCensusAuthority) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active = false
	c.owner.mu.Lock()
	if c.owner.currentCensus == c {
		c.owner.currentCensus = nil
	}
	c.owner.mu.Unlock()
}

// InitialCensus is only the complete no-unretired-ticket path. It never drops a
// registered root/workspace, refunds a ticket, recovers a host intent or replaces
// unknown measurement with zero. Dynamic/live/missing-root recovery is separate.
func (o *storageBudgetOwner) InitialCensus(ctx context.Context) error {
	return o.initialCensus(ctx, nil)
}

func (o *storageBudgetOwner) initialCensusFromStartup(ctx context.Context, home *storageStartupHome) error {
	if !o.valid() || o.ref.cell.startup == nil || home != o.ref.cell.startup.home || !home.exclusive {
		return storagebudget.ErrAuthority
	}
	if err := home.requireFixedZero(ctx); err != nil {
		return err
	}
	return o.initialCensus(ctx, home)
}

func (o *storageBudgetOwner) initialCensus(ctx context.Context, startup *storageStartupHome) (err error) {
	caller := ctx
	var observation *storageOwnedCapture
	defer func() {
		if observation == nil {
			return
		}
		if startup != nil {
			o.stageStartupMeasurement(observation, o.ref.cell.startup, caller, err)
		} else {
			o.finishOwnedMeasurement(observation, caller, err)
		}
	}()
	if !o.valid() {
		return storagebudget.ErrAuthority
	}
	if !o.censusMu.TryLock() {
		return storagebudget.ErrBusy
	}
	defer o.censusMu.Unlock()
	if e := o.retain(); e != nil {
		return e
	}
	defer func() { err = errors.Join(err, o.drop()) }()
	var mutation *storageMutationAuthority
	if startup != nil {
		mutation = o.ref.cell.startup.mutation
	} else {
		mutation = storageMutationIn(ctx, o)
		if mutation == nil {
			var e error
			mutation, e = o.beginAdmission(ctx)
			if e != nil {
				return e
			}
			defer func() { err = errors.Join(err, mutation.Close()) }()
		}
	}
	if e := mutation.current(ctx, true); e != nil {
		return e
	}
	ctx = storageMutationContext(ctx, mutation)
	ctx, cancel := context.WithTimeout(ctx, storageCensusTime)
	defer cancel()
	// This initial route precedes the original server shared home lease. It does
	// not wait to upgrade an already-held shared ownership lease against itself.
	var censusHome storageNamedFile
	if startup == nil {
		home, e := storageOpenOriginalControl(o.ref.cell, ".home-use.lock")
		if e != nil {
			return e
		}
		censusHome = home
		defer func() { err = errors.Join(err, home.file.Close()) }()
		if e = lockPrivateData(home.file, true); e != nil {
			return storagebudget.ErrBusy
		}
	} else if e := startup.requireFixedZero(ctx); e != nil {
		return e
	}
	var e error
	// CT2: recognize legacy in every bank/inactive slot before the first Q
	// mutation. This adds one bounded journal read, without moving same-format
	// idle/use/domain refusal ahead of the maintenance fence.
	if e = o.withControl(ctx, func(context.Context, *storageIntentTable) error { return nil }); e != nil {
		return e
	}
	view, e := o.ref.cell.q.InspectHost(ctx)
	if e != nil {
		return e
	}
	if _, e = o.registrationMissingFrom(view); e != nil {
		return e
	}
	if e = o.ref.cell.current(); e != nil {
		return e
	}
	var epoch uint64
	if startup != nil && view.Configuration.Maintenance {
		if o.ref.cell.startup.startupCensusEpoch == view.Configuration.Epoch && view.Configuration.Epoch != 0 {
			epoch = view.Configuration.Epoch
		} else {
			c := o.ref.cell
			cat := c.ownerCatalog
			// Exact first-enrollment fence only. A later/unknown maintenance cause,
			// including a discovered restriction, must remain blocked on restart.
			if cat.Sequence != 2 || cat.Phase != "active" || view.Configuration.Epoch != 1 || cat.ExpectedQRevision == ^uint64(0) || view.Configuration.Revision != cat.ExpectedQRevision+1 || len(cat.BeforeDefinitionIDs.Roots)+len(cat.BeforeDefinitionIDs.Workspaces)+len(cat.BeforeDefinitionIDs.Filesystems)+len(cat.BeforeDefinitionIDs.Constraints) != 0 {
				return errStorageOwnerRecovery
			}
			epoch = view.Configuration.Epoch
		}
	} else {
		if startup != nil {
			if e = o.ref.cell.startup.reserveOwnerEffect(ctx, o.ref.cell.anchor, nil, "anchor/maintenance"); e != nil {
				return e
			}
		}
		if e = o.requireMutation(ctx); e != nil {
			return e
		}
		epoch, e = o.ref.cell.q.BeginMaintenance(ctx, view.Configuration.Revision)
		if e != nil {
			return e
		}
	}
	roots := o.orderedRoots()
	haveConfig, haveQ := false, false
	var holds []*storageDomainHold
	var censusUse *storageMaintenanceUse
	if startup != nil && o.ref.cell.startup.startupUse != nil {
		holds = o.ref.cell.startup.startupHolds
		if len(holds) != len(roots) {
			return storagebudget.ErrAuthority
		}
		for i, r := range roots {
			if holds[i].root != r {
				return storagebudget.ErrAuthority
			}
			haveConfig = haveConfig || r.configControl
			haveQ = haveQ || r.qControl
		}
	} else {
		use, e := o.maintenanceUse()
		if e != nil {
			return e
		}
		censusUse = use
		defer func() { err = errors.Join(err, use.Close()) }()
		defer func() {
			for i := len(holds) - 1; i >= 0; i-- {
				err = errors.Join(err, holds[i].Close())
			}
		}()
		for _, r := range roots {
			haveConfig = haveConfig || r.configControl
			haveQ = haveQ || r.qControl
			h, e := o.domain(ctx, r, use)
			if e != nil {
				return e
			}
			holds = append(holds, h)
		}
	}
	if !haveConfig || !haveQ {
		return storagebudget.ErrAuthority
	}
	if e = o.withControl(ctx, func(ctx context.Context, t *storageIntentTable) error {
		_, _, e := o.inspectInitialCensusPhase(ctx, t, epoch, holds)
		return e
	}); e != nil {
		return e
	}
	var census storagebudget.Census
	if startup != nil {
		census, e = o.measureCensusWithStartup(ctx, roots, o.ref.cell.startup, &observation)
	} else {
		// Install only after genuine acquisition and phase validation above. This
		// defer runs before domain/use/home cleanup, including every error exit.
		seal := &storageInitialCensusAuthority{active: true, owner: o, mutation: mutation, ref: o.ref, cell: o.ref.cell, home: censusHome, use: censusUse, roots: append([]*storageRootBinding(nil), roots...), holds: append([]*storageDomainHold(nil), holds...), epoch: epoch}
		seal.self = seal
		o.mu.Lock()
		if o.currentCensus != nil {
			o.mu.Unlock()
			return storagebudget.ErrAuthority
		}
		o.currentCensus = seal
		o.mu.Unlock()
		defer seal.invalidate()
		if e = seal.current(ctx, o); e != nil {
			return e
		}
		census, e = o.measureCensusWithAuthority(ctx, roots, nil, seal, &observation)
	}
	if e != nil {
		return e
	}
	for _, h := range holds {
		if e = h.current(ctx); e != nil {
			return e
		}
	}
	if e = o.registerInitialCensus(ctx, epoch, holds); e != nil {
		return e
	}
	if e = o.fail("census", "after-register"); e != nil {
		return e
	}
	if e = o.publishInitialCensus(ctx, epoch, holds, census); e != nil {
		return e
	}
	o.mu.Lock()
	o.censusEpoch = epoch
	o.mu.Unlock()
	if observation != nil {
		observation.value.Epoch = epoch
	}
	return nil
}

type storageCensusSeen struct{ filesystem storagebudget.Digest }
type storageCensusAttribution struct {
	workspace storagebudget.ID
	identity  storageDirectoryIdentity
}
type storageCensusWalk struct {
	enrollment *storageEnrollmentEpoch
	transfer   *chatTransferOperation
	owner      *storageBudgetOwner
	ctx        context.Context
	entries    int
	roots      map[storageDirectoryIdentity]*storageRootBinding
	// Only catalog-derived Workflow4 OwnSelf0 views live here. Owning roots
	// retain their unique native index; views carve exact immediate names.
	namespaces    map[storageDirectoryIdentity]map[string]*storageRootBinding
	transferEdges map[storageTransferCensusEdge]bool
	seen          map[storageDirectoryIdentity]storageCensusSeen
	directories   map[storageDirectoryIdentity]bool
	attributed    map[storageCensusAttribution]bool
	workspace     map[storagebudget.ID][storagebudget.Categories]storagebudget.Vector
	observation   *storageOwnedCapture
	readyMeasured map[*storageRootBinding]bool
	startup       *storageBudgetConstruction
	census        *storageInitialCensusAuthority
	exclusive     *storageExclusiveStartupCensus
}

func (o *storageBudgetOwner) measureCensus(ctx context.Context, roots []*storageRootBinding) (storagebudget.Census, error) {
	return o.measureCensusWithStartup(ctx, roots, nil)
}

func (o *storageBudgetOwner) measureCensusWithStartup(ctx context.Context, roots []*storageRootBinding, startup *storageBudgetConstruction, observation ...**storageOwnedCapture) (storagebudget.Census, error) {
	return o.measureCensusWithAuthority(ctx, roots, startup, nil, observation...)
}

func (o *storageBudgetOwner) measureCensusWithAuthority(ctx context.Context, roots []*storageRootBinding, startup *storageBudgetConstruction, census *storageInitialCensusAuthority, observation ...**storageOwnedCapture) (storagebudget.Census, error) {
	return o.measureCensusAll(ctx, roots, startup, census, nil, observation...)
}
func (o *storageBudgetOwner) measureCensusWithExclusive(ctx context.Context, roots []*storageRootBinding, exclusive *storageExclusiveStartupCensus, observation ...**storageOwnedCapture) (storagebudget.Census, error) {
	return o.measureCensusAll(ctx, roots, nil, nil, exclusive, observation...)
}
func (o *storageBudgetOwner) measureCensusAll(ctx context.Context, roots []*storageRootBinding, startup *storageBudgetConstruction, census *storageInitialCensusAuthority, exclusive *storageExclusiveStartupCensus, observation ...**storageOwnedCapture) (storagebudget.Census, error) {
	return o.measureCensusAllTransfer(ctx, roots, startup, census, exclusive, nil, observation...)
}
func (o *storageBudgetOwner) measureCensusAllTransfer(ctx context.Context, roots []*storageRootBinding, startup *storageBudgetConstruction, census *storageInitialCensusAuthority, exclusive *storageExclusiveStartupCensus, transfer *chatTransferOperation, observation ...**storageOwnedCapture) (storagebudget.Census, error) {
	out := storagebudget.Census{}
	if census != nil {
		if startup != nil {
			return out, storagebudget.ErrAuthority
		}
		if err := census.current(ctx, o); err != nil {
			return out, err
		}
	}
	walk := storageCensusWalk{owner: o, ctx: ctx, roots: map[storageDirectoryIdentity]*storageRootBinding{}, seen: map[storageDirectoryIdentity]storageCensusSeen{}, directories: map[storageDirectoryIdentity]bool{}, attributed: map[storageCensusAttribution]bool{}, workspace: map[storagebudget.ID][storagebudget.Categories]storagebudget.Vector{}, readyMeasured: map[*storageRootBinding]bool{}}
	walk.startup, walk.census, walk.exclusive = startup, census, exclusive
	walk.transfer = transfer
	if transfer != nil {
		if startup != nil || census != nil || exclusive != nil || transfer.preparation.registry.owner != o || len(roots) != len(transfer.preparation.epoch.originalRoots) {
			return out, storagebudget.ErrAuthority
		}
		for i, root := range roots {
			if root != transfer.preparation.epoch.originalRoots[i] {
				return out, storagebudget.ErrAuthority
			}
		}
		if err := transfer.originalReadCurrent(ctx); err != nil {
			return out, err
		}
	}
	if len(observation) == 1 && observation[0] != nil {
		walk.observation = o.beginOwnedMeasurement(roots, o.workspaces, o.filesystems)
		*observation[0] = walk.observation
	}
	walk.namespaces = map[storageDirectoryIdentity]map[string]*storageRootBinding{}
	for _, r := range roots {
		if storageTransferNamespaceBinding(r) {
			retired, e := walk.originalTransferNamespaceRetired(r)
			if e != nil {
				return out, e
			}
			if retired {
				continue
			}
			if walk.namespaces[r.directory] == nil {
				walk.namespaces[r.directory] = map[string]*storageRootBinding{}
			}
			if walk.namespaces[r.directory][r.restrictedNamespace] != nil {
				return out, storagebudget.ErrAuthority
			}
			walk.namespaces[r.directory][r.restrictedNamespace] = r
		} else {
			if walk.roots[r.directory] != nil {
				return out, storagebudget.ErrAuthority
			}
			walk.roots[r.directory] = r
		}
	}
	if e := walk.captureOriginalTransferChains(roots); e != nil {
		return out, e
	}
	for id := range o.workspaces {
		walk.workspace[id] = [storagebudget.Categories]storagebudget.Vector{}
	}
	for _, r := range roots {
		if e := r.current(ctx); e != nil {
			return out, e
		}
		parent, e := r.censusControlParent()
		if e != nil {
			return storagebudget.Census{}, e
		}
		var usage [storagebudget.Categories]storagebudget.Vector
		count := 0
		retired, e := walk.originalTransferNamespaceRetired(r)
		if e != nil {
			return storagebudget.Census{}, e
		}
		if !retired {
			if e := walk.directory(r, r.root, nil, "", 0, &count, &usage); e != nil {
				return storagebudget.Census{}, e
			}
		}
		if e := walk.carried(r, &count, &usage); e != nil {
			return storagebudget.Census{}, e
		}
		v, e := storageVectorAdd(usage[storagebudget.Control], parent.provision(walk.readyMeasured[r]))
		if e != nil {
			return storagebudget.Census{}, e
		}
		usage[storagebudget.Control] = v
		out.Roots = append(out.Roots, storagebudget.RootMeasurement{ID: r.row.ID, Generation: r.row.Generation, Usage: usage})
	}
	for id, usage := range walk.workspace {
		out.Workspaces = append(out.Workspaces, storagebudget.WorkspaceMeasurement{ID: id, Generation: o.workspaces[id].Generation, Usage: usage})
	}
	sort.Slice(out.Workspaces, func(i, j int) bool {
		return strings.Compare(string(out.Workspaces[i].ID[:]), string(out.Workspaces[j].ID[:])) < 0
	})
	metadata, e := json.Marshal(struct {
		Roots      []storagebudget.RootMeasurement
		Workspaces []storagebudget.WorkspaceMeasurement
	}{out.Roots, out.Workspaces})
	if e != nil {
		return storagebudget.Census{}, storagebudget.ErrBounds
	}
	out.Evidence = sha256.Sum256(append([]byte("desk-storage-complete-metadata-census-v1\x00"), metadata...))
	walk.observation.complete()
	return out, nil
}
func (r *storageRootBinding) fixedControl(relative string) bool {
	if strings.Contains(relative, "/") {
		return false
	}
	if r.qControl {
		switch relative {
		case "lock", "checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests":
			return true
		}
	}
	if r.configControl {
		switch relative {
		case ".home-use.lock", storageBootstrapName, storageUseName, storageClaimName, storageRecordName, storageIntentName, storageReadyName:
			return true
		}
		return relative == r.domainName
	}
	return false
}
func (w *storageCensusWalk) add(r *storageRootBinding, relative string, identity storageDirectoryIdentity, size uint64, directory bool, usage *[storagebudget.Categories]storagebudget.Vector) error {
	category, references := r.categoryOf(relative)
	return w.addAs(r, category, references, identity, size, directory, usage)
}
func (w *storageCensusWalk) addAs(r *storageRootBinding, category uint8, references []storagebudget.ID, identity storageDirectoryIdentity, size uint64, directory bool, usage *[storagebudget.Categories]storagebudget.Vector) error {
	v := storagebudget.Vector{}
	if directory {
		v.Directories = 1
	} else {
		v.Files = 1
		fs := w.owner.filesystems[r.row.Filesystem]
		if prior, ok := w.seen[identity]; ok {
			if prior.filesystem != fs.Identity {
				return storagebudget.ErrUnknown
			}
		} else {
			w.seen[identity] = storageCensusSeen{fs.Identity}
			v.Bytes = size
		}
	}
	next, e := storageVectorAdd(usage[category], v)
	if e != nil {
		return e
	}
	usage[category] = next
	for _, id := range references {
		attribution := v
		if !directory {
			key := storageCensusAttribution{id, identity}
			if !w.attributed[key] {
				attribution.Bytes = size
				w.attributed[key] = true
			} else {
				attribution.Bytes = 0
			}
		}
		prior, ok := w.workspace[id]
		if !ok {
			return storagebudget.ErrAuthority
		}
		next, e = storageVectorAdd(prior[category], attribution)
		if e != nil {
			return e
		}
		prior[category] = next
		w.workspace[id] = prior
	}
	return nil
}
func (w *storageCensusWalk) directory(r *storageRootBinding, root *os.Root, witnesses []storageNamedRoot, relative string, depth int, count *int, usage *[storagebudget.Categories]storagebudget.Vector) (err error) {
	if relative == "" && r.restrictedNamespace != "" {
		if r.source != nil && r.source.role == 8 {
			return w.historyAncestorScope(r, root, count, usage)
		}
		return w.projectPrivateScope(r, root, count, usage)
	}
	if w.exclusive != nil {
		if e := w.exclusive.permit(w.ctx); e != nil {
			return e
		}
	}
	if depth > storageCensusDepth || len(relative) > storageCensusName {
		return storagebudget.ErrBounds
	}
	if e := w.ctx.Err(); e != nil {
		return e
	}
	before, e := root.Stat(".")
	if e != nil || !storageCensusMeasuredDirectory(before) {
		return storagebudget.ErrUnknown
	}
	identity, e := storageFileIdentity(before)
	if e != nil {
		return e
	}
	if w.directories[identity] {
		return storagebudget.ErrUnknown
	}
	w.directories[identity] = true
	if relative != "" || !r.qControl && !r.configControl {
		if e = w.add(r, relative, identity, 0, true, usage); e != nil {
			return e
		}
	}
	if e = w.sampleDirectory(r, root, witnesses); e != nil {
		return e
	}
	file, e := root.Open(".")
	if e != nil {
		return e
	}
	if w.exclusive != nil {
		e = w.exclusive.boundary("walk-read-dir")
		if e == nil {
			e = w.exclusive.current(w.ctx)
		}
		if e != nil {
			return errors.Join(e, file.Close())
		}
	}
	entries, re := file.ReadDir(storageCensusEntries - w.entries + 1)
	var boundaryErr error
	if w.exclusive != nil {
		boundaryErr = w.exclusive.boundary("walk-dir-close")
	}
	ce := errors.Join(boundaryErr, file.Close())
	if re != nil && !errors.Is(re, io.EOF) || ce != nil {
		return errors.Join(storagebudget.ErrUnknown, re, ce)
	}
	if len(entries) > storageCensusEntries-w.entries {
		return storagebudget.ErrBounds
	}
	w.entries += len(entries)
	*count += len(entries)
	if (r.category == storagebudget.History || len(r.legacyReferences) > 0) && *count > maxStorageFiles {
		return storagebudget.ErrBounds
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if w.exclusive != nil {
			if e = w.exclusive.permit(w.ctx); e != nil {
				return e
			}
		}
		if e = w.ctx.Err(); e != nil {
			return e
		}
		name := entry.Name()
		childName := name
		if relative != "" {
			childName = relative + "/" + name
		}
		if len(childName) > storageCensusName {
			return storagebudget.ErrBounds
		}
		info, e := root.Lstat(name)
		if e != nil {
			return storagebudget.ErrUnknown
		}
		if w.exclusive != nil {
			if e = w.exclusive.permit(w.ctx); e != nil {
				return e
			}
		}
		id, e := storageFileIdentity(info)
		if e != nil {
			return e
		}
		if r.configControl && childName == storageReadyName {
			if w.readyMeasured[r] {
				return storagebudget.ErrUnknown
			}
			if e = storageCensusReady(root, w.owner.ref.cell.binding, info, func(id storageDirectoryIdentity) error { return w.add(r, childName, id, 32, false, usage) }, func(before, after os.FileInfo) { walk := w.observation; walk.observe(r, childName, before, after) }); e != nil {
				return e
			}
			w.readyMeasured[r] = true
			continue
		}
		if info.IsDir() {
			if view := w.transferNamespaceClaim(identity, name); view != nil && view != r {
				if e = view.current(w.ctx); e != nil {
					return e
				}
				continue
			}
			if bound, ok := w.roots[id]; ok && bound != r {
				if e = bound.current(w.ctx); e != nil {
					return e
				}
				continue
			}
			// The walk holds the child to the census rule when it enters it.
			child, e := storagePinChild(root, name, false)
			if e != nil {
				return e
			}
			chain := append(append([]storageNamedRoot(nil), witnesses...), child)
			e = w.directory(r, child.root, chain, childName, depth+1, count, usage)
			if e == nil {
				e = child.current()
			}
			var boundaryErr error
			if w.exclusive != nil {
				boundaryErr = w.exclusive.boundary("child-root-close")
			}
			ce := errors.Join(boundaryErr, child.root.Close())
			if e != nil || ce != nil {
				return errors.Join(e, ce)
			}
		} else {
			if !storageCensusMeasuredFile(info) {
				return storagebudget.ErrUnknown
			}
			covered, e := r.censusCovered(w.ctx, childName, info)
			if e != nil {
				return e
			}
			if !covered {
				if e = w.add(r, childName, id, uint64(info.Size()), false, usage); e != nil {
					return e
				}
			}
			after, e := root.Lstat(name)
			if e != nil || !os.SameFile(info, after) || after.Size() != info.Size() || after.Mode() != info.Mode() || !after.ModTime().Equal(info.ModTime()) {
				return storagebudget.ErrUnknown
			}
			w.observation.observe(r, childName, info, after)
		}
	}
	if w.exclusive != nil {
		if e = w.exclusive.permit(w.ctx); e != nil {
			return e
		}
	}
	after, e := root.Stat(".")
	if e != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) {
		return storagebudget.ErrUnknown
	}
	w.observation.observe(r, relative, before, after)
	return r.current(w.ctx)
}

// carried measures, under the installation's config root r, each project's
// private folder that no registered root measures: one Desk made outside the
// identity producer before the project had a registered private folder or
// project scope (storage-budget-startup.md, "Custody rule for v0.14.0"). The
// config root carries its bytes as Control, attributed to the project's
// workspace, so installation and workspace limits count them. Its content is
// held to the census rule like any other. A folder its own root, a project
// scope or a transfer view measures is left to that root. No capacity sample
// is taken for it: no registered filesystem row describes where it is.
func (w *storageCensusWalk) carried(r *storageRootBinding, count *int, usage *[storagebudget.Categories]storagebudget.Vector) error {
	if r == nil || !r.configControl {
		return nil
	}
	var x *storageOriginalSources
	if w.enrollment != nil {
		x = w.enrollment.c.sources
	} else if w.owner != nil && w.owner.ref != nil && w.owner.ref.cell != nil && w.owner.ref.cell.startup != nil {
		x = w.owner.ref.cell.startup.sources
	}
	if x == nil {
		return nil
	}
	for _, c := range x.carried {
		if e := w.ctx.Err(); e != nil {
			return e
		}
		project, e := c.project.Stat(".")
		if e != nil {
			return e
		}
		projectID, e := storageFileIdentity(project)
		if e != nil {
			return e
		}
		if scope := w.roots[projectID]; scope != nil && scope.restrictedNamespace == startupIdentityDir || w.transferNamespaceClaim(projectID, startupIdentityDir) != nil {
			continue
		}
		info, e := c.project.Lstat(startupIdentityDir)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return storagebudget.ErrUnknown
		}
		id, e := storageFileIdentity(info)
		if e != nil {
			return e
		}
		if w.roots[id] != nil {
			continue
		}
		child, e := storagePinChild(c.project, startupIdentityDir, false)
		if e != nil {
			return e
		}
		e = w.carriedDirectory(r, c.workspace, child.root, startupIdentityDir, 1, count, usage)
		if e = errors.Join(e, child.current(), child.root.Close()); e != nil {
			return e
		}
	}
	return r.current(w.ctx)
}

// carriedDirectory is directory's walk for a carried private folder: the same
// bounds, rule and before/after identity checks, with every entry charged to r
// as Control, or Retained in its retained-artifacts folder, and attributed to
// workspace when it is registered in this walk.
func (w *storageCensusWalk) carriedDirectory(r *storageRootBinding, workspace storagebudget.ID, root *os.Root, relative string, depth int, count *int, usage *[storagebudget.Categories]storagebudget.Vector) error {
	var references []storagebudget.ID
	if _, ok := w.workspace[workspace]; ok {
		references = []storagebudget.ID{workspace}
	}
	if w.exclusive != nil {
		if e := w.exclusive.permit(w.ctx); e != nil {
			return e
		}
	}
	if depth > storageCensusDepth || len(relative) > storageCensusName {
		return storagebudget.ErrBounds
	}
	if e := w.ctx.Err(); e != nil {
		return e
	}
	before, e := root.Stat(".")
	if e != nil || !storageCensusMeasuredDirectory(before) {
		return storagebudget.ErrUnknown
	}
	identity, e := storageFileIdentity(before)
	if e != nil {
		return e
	}
	if w.directories[identity] {
		return storagebudget.ErrUnknown
	}
	w.directories[identity] = true
	category := uint8(storagebudget.Control)
	if storageRetainedArtifactsPath(relative, startupIdentityDir+"/") {
		category = storagebudget.Retained
	}
	if e = w.addAs(r, category, references, identity, 0, true, usage); e != nil {
		return e
	}
	file, e := root.Open(".")
	if e != nil {
		return e
	}
	if w.exclusive != nil {
		if e = w.exclusive.current(w.ctx); e != nil {
			return errors.Join(e, file.Close())
		}
	}
	entries, re := file.ReadDir(storageCensusEntries - w.entries + 1)
	ce := file.Close()
	if re != nil && !errors.Is(re, io.EOF) || ce != nil {
		return errors.Join(storagebudget.ErrUnknown, re, ce)
	}
	if len(entries) > storageCensusEntries-w.entries {
		return storagebudget.ErrBounds
	}
	w.entries += len(entries)
	*count += len(entries)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if w.exclusive != nil {
			if e = w.exclusive.permit(w.ctx); e != nil {
				return e
			}
		}
		if e = w.ctx.Err(); e != nil {
			return e
		}
		name := entry.Name()
		childName := relative + "/" + name
		if len(childName) > storageCensusName {
			return storagebudget.ErrBounds
		}
		info, e := root.Lstat(name)
		if e != nil {
			return storagebudget.ErrUnknown
		}
		id, e := storageFileIdentity(info)
		if e != nil {
			return e
		}
		if info.IsDir() {
			bound := w.roots[id]
			if view := w.transferNamespaceClaim(identity, name); view != nil {
				bound = view
			}
			if bound != nil {
				if e = bound.current(w.ctx); e != nil {
					return e
				}
				continue
			}
			child, e := storagePinChild(root, name, false)
			if e != nil {
				return e
			}
			e = w.carriedDirectory(r, workspace, child.root, childName, depth+1, count, usage)
			if e = errors.Join(e, child.current(), child.root.Close()); e != nil {
				return e
			}
			continue
		}
		if !storageCensusMeasuredFile(info) {
			return storagebudget.ErrUnknown
		}
		fileCategory := uint8(storagebudget.Control)
		if storageRetainedArtifactsPath(childName, startupIdentityDir+"/") {
			fileCategory = storagebudget.Retained
		}
		if e = w.addAs(r, fileCategory, references, id, uint64(info.Size()), false, usage); e != nil {
			return e
		}
		after, e := root.Lstat(name)
		if e != nil || !os.SameFile(info, after) || after.Size() != info.Size() || after.Mode() != info.Mode() || !after.ModTime().Equal(info.ModTime()) {
			return storagebudget.ErrUnknown
		}
		w.observation.observe(r, childName, info, after)
	}
	after, e := root.Stat(".")
	if e != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) {
		return storagebudget.ErrUnknown
	}
	w.observation.observe(r, relative, before, after)
	return nil
}

// Each census child retains its exact original named chain until observation
// and pass cleanup finish. Initial census borrows EX without a second SH.
func (w *storageCensusWalk) sampleDirectory(r *storageRootBinding, root *os.Root, witnesses []storageNamedRoot) error {
	if w.transfer != nil {
		return w.sampleOriginalTransferDirectory(r, root, witnesses)
	}
	if w.enrollment != nil {
		p := w.enrollment
		if p.candidate == nil || p.candidate.roots[r.row.ID] != r || p.candidateObserver == nil {
			return storagebudget.ErrAuthority
		}
		if err := p.nativePermit(w.ctx); err != nil {
			return err
		}
		for _, witness := range witnesses {
			if err := witness.current(); err != nil {
				return err
			}
		}
		if _, err := p.candidateObserver.query(w.ctx, r, root); err != nil {
			return err
		}
		for _, witness := range witnesses {
			if err := witness.current(); err != nil {
				return err
			}
		}
		return p.nativePermit(w.ctx)
	}
	if w.exclusive != nil {
		p, err := w.owner.beginStorageCapacityPassWithExclusive(w.ctx, []storageCapacityEffectParent{{binding: r, root: root, witnesses: witnesses}}, w.exclusive)
		if err != nil {
			return err
		}
		_, err = w.owner.rootStorageSample(p.context(w.ctx), r, root)
		return errors.Join(err, p.Close())
	}

	_, concrete := w.owner.observer.(*storageAdvisoryObserver)
	if w.startup == nil && (w.census == nil || !concrete) {
		_, err := w.owner.rootStorageSample(w.ctx, r, root)
		return err
	}
	if !concrete {
		return storagebudget.ErrAuthority
	}
	p, err := w.owner.beginStorageCapacityPassWithAuthority(w.ctx, []storageCapacityEffectParent{{binding: r, root: root, witnesses: witnesses}}, w.startup, w.census)
	if err != nil {
		return err
	}
	_, err = w.owner.rootStorageSample(p.context(w.ctx), r, root)
	return errors.Join(err, p.Close())
}

// Detached, volatile metadata observations. These members are never consulted by
// admission, native permission, Q settlement, recovery or deletion.
const storageOwnedObjectLimit = storageCensusEntries + 2*storagebudget.MaxRoots

type storageOwnedMeasurementSlot struct {
	attempt uint64
	failed  bool
	last    *storageOwnedMeasurement
	startup *storageOwnedCapture // original startup only; cleared by its outer finalizer
}
type storageOwnedMeasurement struct {
	Attempt                                     uint64
	Started, Completed                          time.Time
	Epoch, Revision                             uint64
	BoundaryKnown                               bool
	AdmissionGeneration, OrdinaryActivity       uint64
	Scope                                       storagebudget.Digest
	Logical, Allocation, Shared                 storagebudget.Vector
	LogicalKnown, AllocationKnown, SharedKnown  bool
	Roots                                       map[storagebudget.ID]storagebudget.RegisteredRoot
	WorkspaceGenerations, FilesystemGenerations map[storagebudget.ID]uint64
	Filesystems                                 map[storagebudget.ID]storageOwnedFilesystem
}
type storageOwnedFilesystem struct {
	Logical, Allocation           storagebudget.Vector
	LogicalKnown, AllocationKnown bool
}
type storageOwnedObject struct {
	filesystem      storagebudget.ID
	identity        storagebudget.Digest
	logical         storagebudget.Vector
	allocated       uint64
	allocationKnown bool
	firstWorkspace  storagebudget.ID
	shared          bool
}
type storageOwnedCapture struct {
	value                storageOwnedMeasurement
	seen                 map[storageDirectoryIdentity]*storageOwnedObject
	filesystems          map[storagebudget.ID]storagebudget.Filesystem
	workspaces           map[storagebudget.ID]storagebudget.Workspace
	startup              *storageBudgetConstruction
	caller               context.Context
	collected, innerDone bool
}

// Source correspondence includes reference/category/scope semantics, not usage
// or editable limits. Bounds are the existing registered definition limits.
func storageOwnedScope(roots []*storageRootBinding, workspaces map[storagebudget.ID]storagebudget.Workspace, filesystems map[storagebudget.ID]storagebudget.Filesystem) storagebudget.Digest {
	if len(roots) > storagebudget.MaxRoots || len(workspaces) > storagebudget.MaxWorkspaces || len(filesystems) > storagebudget.MaxFilesystems {
		return storagebudget.Digest{}
	}
	type root struct {
		Row                storagebudget.RegisteredRoot
		Directory          storageDirectoryIdentity
		Category           uint8
		References, Legacy []storagebudget.ID
		Namespace          string
		Inactive           bool
		Parent             storagebudget.ID
		Source             *storageEnrollmentPeakSource
	}
	type member struct {
		ID         storagebudget.ID
		Generation uint64
	}
	rs := make([]root, 0, len(roots))
	ws := make([]member, 0, len(workspaces))
	fs := make([]storagebudget.Filesystem, 0, len(filesystems))
	for _, r := range roots {
		if r == nil || len(r.references) > storagebudget.MaxWorkspaces || len(r.legacyReferences) > storagebudget.MaxWorkspaces {
			return storagebudget.Digest{}
		}
		row := root{Row: storageRegisteredRoot(r.row), Directory: r.directory, Category: r.category, References: r.references, Legacy: r.legacyReferences, Namespace: r.restrictedNamespace, Inactive: r.transferInactive, Parent: r.scopeParent}
		if r.source != nil {
			row.Source = r.source.scope
		}
		rs = append(rs, row)
	}
	for id, w := range workspaces {
		ws = append(ws, member{id, w.Generation})
	}
	for _, f := range filesystems {
		fs = append(fs, f)
	}
	sort.Slice(rs, func(i, j int) bool { return string(rs[i].Row.ID[:]) < string(rs[j].Row.ID[:]) })
	sort.Slice(ws, func(i, j int) bool { return string(ws[i].ID[:]) < string(ws[j].ID[:]) })
	sort.Slice(fs, func(i, j int) bool { return string(fs[i].ID[:]) < string(fs[j].ID[:]) })
	raw, e := json.Marshal(struct {
		Roots       []root
		Workspaces  []member
		Filesystems []storagebudget.Filesystem
	}{rs, ws, fs})
	if e != nil {
		return storagebudget.Digest{}
	}
	return sha256.Sum256(raw)
}
func (o *storageBudgetOwner) beginOwnedMeasurement(roots []*storageRootBinding, workspaces map[storagebudget.ID]storagebudget.Workspace, filesystems map[storagebudget.ID]storagebudget.Filesystem) *storageOwnedCapture {
	c := &storageOwnedCapture{seen: map[storageDirectoryIdentity]*storageOwnedObject{}, filesystems: map[storagebudget.ID]storagebudget.Filesystem{}, workspaces: map[storagebudget.ID]storagebudget.Workspace{}}
	c.value = storageOwnedMeasurement{Started: time.Now(), Scope: storageOwnedScope(roots, workspaces, filesystems), LogicalKnown: true, AllocationKnown: true, SharedKnown: true, Filesystems: map[storagebudget.ID]storageOwnedFilesystem{}}
	c.value.Roots = map[storagebudget.ID]storagebudget.RegisteredRoot{}
	c.value.WorkspaceGenerations = map[storagebudget.ID]uint64{}
	c.value.FilesystemGenerations = map[storagebudget.ID]uint64{}
	for _, r := range roots {
		c.value.Roots[r.row.ID] = storageRegisteredRoot(r.row)
	}
	for id, f := range filesystems {
		c.value.FilesystemGenerations[id] = f.Generation
		c.filesystems[id] = f
		c.value.Filesystems[id] = storageOwnedFilesystem{LogicalKnown: true, AllocationKnown: true}
	}
	for id, w := range workspaces {
		c.workspaces[id] = w
		c.value.WorkspaceGenerations[id] = w.Generation
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	// An exhausted diagnostic counter cannot change the operation's result.
	o.measurement.failed = true
	if o.self != o || o.measurement.attempt == ^uint64(0) || o.closing || o.closed || o.transitionRequired {
		return nil
	}
	o.measurement.attempt++
	c.value.Attempt = o.measurement.attempt
	g := &o.admission
	g.mu.Lock()
	c.value.AdmissionGeneration, c.value.OrdinaryActivity = g.generation, g.measurementActivity
	g.mu.Unlock()
	return c
}
func storageOwnedAdd(a, b storagebudget.Vector) (storagebudget.Vector, bool) {
	if b.Bytes > ^uint64(0)-a.Bytes || b.Files > ^uint64(0)-a.Files || b.Directories > ^uint64(0)-a.Directories {
		return storagebudget.Vector{}, false
	}
	return storagebudget.Vector{Bytes: a.Bytes + b.Bytes, Files: a.Files + b.Files, Directories: a.Directories + b.Directories}, true
}
func (c *storageOwnedCapture) observe(r *storageRootBinding, relative string, before, after os.FileInfo) {
	if c == nil {
		return
	}
	if r == nil {
		c.unknownObject(storagebudget.ID{})
		for filesystem := range c.value.Filesystems {
			c.unknownObject(filesystem)
		}
		return
	}
	if before == nil || after == nil {
		c.unknownObject(r.row.Filesystem)
		return
	}
	id, e := storageFileIdentity(before)
	fs, ok := c.filesystems[r.row.Filesystem]
	if e != nil || !ok {
		c.unknownObject(r.row.Filesystem)
		return
	}
	logical := storagebudget.Vector{}
	if before.IsDir() {
		logical.Directories = 1
	} else if before.Mode().IsRegular() && before.Size() >= 0 {
		logical.Bytes = uint64(before.Size())
		logical.Files = 1
	} else {
		c.unknownObject(r.row.Filesystem)
		return
	}
	allocation, known := storageReportedAllocation(before)
	afterAllocation, afterKnown := storageReportedAllocation(after)
	known = known && afterKnown && allocation == afterAllocation
	row := c.value.Filesystems[r.row.Filesystem]
	if !known {
		row.AllocationKnown = false
		c.value.AllocationKnown = false
	}
	prior := c.seen[id]
	if prior == nil {
		if len(c.seen) >= storageOwnedObjectLimit {
			c.unknownObject(r.row.Filesystem)
			return
		}
		prior = &storageOwnedObject{filesystem: r.row.Filesystem, identity: fs.Identity, logical: logical, allocated: allocation, allocationKnown: known}
		c.seen[id] = prior
		var good bool
		c.value.Logical, good = storageOwnedAdd(c.value.Logical, logical)
		c.value.LogicalKnown = c.value.LogicalKnown && good
		row.Logical, good = storageOwnedAdd(row.Logical, logical)
		row.LogicalKnown = row.LogicalKnown && good
		if known {
			allocated := logical
			allocated.Bytes = allocation
			c.value.Allocation, good = storageOwnedAdd(c.value.Allocation, allocated)
			c.value.AllocationKnown = c.value.AllocationKnown && good
			row.Allocation, good = storageOwnedAdd(row.Allocation, allocated)
			row.AllocationKnown = row.AllocationKnown && good
		}
	} else if prior.identity != fs.Identity || prior.filesystem != r.row.Filesystem || prior.logical != logical {
		c.unknownObject(prior.filesystem)
		c.unknownObject(r.row.Filesystem)
		row = c.value.Filesystems[r.row.Filesystem]
	} else if !prior.allocationKnown || !known || prior.allocated != allocation {
		c.value.AllocationKnown = false
		row.AllocationKnown = false
	}
	_, references := r.categoryOf(relative)
	for _, workspace := range references {
		if _, ok := c.workspaces[workspace]; !ok {
			c.value.SharedKnown = false
			continue
		}
		if workspace == (storagebudget.ID{}) {
			c.value.SharedKnown = false
			continue
		}
		if prior.firstWorkspace == (storagebudget.ID{}) {
			prior.firstWorkspace = workspace
		} else if prior.firstWorkspace != workspace && !prior.shared {
			prior.shared = true
			var good bool
			c.value.Shared, good = storageOwnedAdd(c.value.Shared, logical)
			c.value.SharedKnown = c.value.SharedKnown && good
		}
	}
	c.value.Filesystems[r.row.Filesystem] = row
}
func (c *storageOwnedCapture) complete() {
	if c == nil {
		return
	}
	c.value.Completed = time.Now()
	c.collected = true
	c.seen = nil
	c.filesystems = nil
	c.workspaces = nil
}

// Called only by an existing original finalizer, after its owned cleanup. A
// newer attempt invalidates this result, even if the newer attempt failed.
func (o *storageBudgetOwner) finishOwnedMeasurement(c *storageOwnedCapture, ctx context.Context, err error) {
	if c == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.finishOwnedMeasurementLocked(c, ctx, err)
}
func (o *storageBudgetOwner) finishOwnedMeasurementLocked(c *storageOwnedCapture, ctx context.Context, err error) {
	if o.self != o || c == nil || c.value.Attempt != o.measurement.attempt || err != nil || ctx == nil || ctx.Err() != nil || !c.collected || c.value.Epoch == 0 || o.closing || o.closed || o.transitionRequired {
		return
	}
	if c.value.Scope == (storagebudget.Digest{}) || storageOwnedScope(o.orderedRoots(), o.workspaces, o.filesystems) != c.value.Scope {
		return
	}
	value := c.value
	o.measurement.last = &value
	o.measurement.failed = false
}
func (o *storageBudgetOwner) stageStartupMeasurement(c *storageOwnedCapture, startup *storageBudgetConstruction, ctx context.Context, err error) {
	if c == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if c.value.Attempt != o.measurement.attempt || err != nil || ctx == nil || ctx.Err() != nil || !c.collected {
		return
	}
	c.startup = startup
	c.caller = ctx
	c.innerDone = true
	o.measurement.startup = c
}
func (o *storageBudgetOwner) finishStartupMeasurement(startup *storageBudgetConstruction, ctx context.Context, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	c := o.measurement.startup
	if c == nil || c.startup != startup {
		return
	}
	o.measurement.startup = nil
	if c.innerDone && c.caller != nil && c.caller.Err() == nil {
		o.finishOwnedMeasurementLocked(c, ctx, err)
	}
}

// No live metadata or mutable buffers escape the original owner. Scope hashing
// runs only over bounded registered rows, never over a filesystem tree.
func (o *storageBudgetOwner) ownedMeasurement() (*storageOwnedMeasurement, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	v := o.measurement.last
	if v == nil || o.closing || o.closed || o.transitionRequired || v.Scope != storageOwnedScope(o.orderedRoots(), o.workspaces, o.filesystems) {
		return nil, true
	}
	return v, o.measurement.failed
}

func (c *storageOwnedCapture) unknownObject(filesystem storagebudget.ID) {
	c.value.LogicalKnown = false
	c.value.AllocationKnown = false
	c.value.SharedKnown = false
	if row, ok := c.value.Filesystems[filesystem]; ok {
		row.LogicalKnown = false
		row.AllocationKnown = false
		c.value.Filesystems[filesystem] = row
	}
}
