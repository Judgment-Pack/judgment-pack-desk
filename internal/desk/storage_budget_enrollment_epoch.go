package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

var errStorageEnrollmentUnresolved = errors.New("storage enrollment unresolved; original-owner recovery required")

// The original construction owns exactly one enrollment invocation. Its one
// owner retain is the existing Close/drain lifetime, not a second operation
// registry. No ordinary/startup/handoff authority can be cast to this role.
// Only the synchronous executor owns descriptions and signals final drain.
type storageEnrollmentEpoch struct {
	measurement       *storageOwnedCapture
	measurementCaller context.Context // original caller; internal finish cancels p.ctx
	policy            *storagePolicyOperation

	transfer             *chatTransferPreparation
	transferLock         *storageNamedFile
	transferHoldOrder    []int
	transferDomainOwners []*storageDomainHold
	transferExtraDomain  *os.File
	requester            *Server // actual live requester; selected before prefixes
	// Borrowed original registry/project descriptor for startup recovery. The
	// source dictionary owns its lifetime; this is no Server or new authority.
	startupProject *storageOriginalProject

	self             *storageEnrollmentEpoch
	c                *storageBudgetConstruction
	generation       uint64
	ctx              context.Context
	cancel           context.CancelFunc
	stopCancel       func() bool
	done             chan struct{}
	mu               sync.Mutex
	running          bool
	revoked          bool
	entered          bool
	transferNative   *chatTransferCompletion
	finishing        bool
	err              error
	use              *storageMaintenanceUse
	project          *os.File
	signing          *signingDir
	signingFile      *os.File
	signingNative    storageOwnerPersistentObject
	identityPrefixes bool
	signingAbsent    bool
	work             *storageNamedFile
	holds            []*storageDomainHold
	scope            *storageEnrollmentPeakScope
	qualified        *storageEnrollmentQualification
	targets          [8]storageEnrollmentNativeTarget
	parents          [2]*os.File
	// A live reservation is admitted/reconstructed from original Q/capacity
	// facts before the first schedule effect. Neither persisted Scope nor this
	// bit is independently accepted as authority by a native effect method.
	reservation         storageEnrollmentReservation
	planCursor          int
	evidence            *storageEnrollmentPeakEvidence
	reconciled          bool
	startup             bool
	sourceHoldsComplete bool
	// Actual pre-binding startup descriptions only. They confer no native
	// permission and remain owned by this invocation through alias borrowing.
	sourceFiles []storageNamedFile
	sourceEdges []storageNamedRoot
	sourceRoots []*os.Root
	passGroups  []storageEnrollmentPeakGroup
	// These are actual observations made by this invocation under its original
	// holds. A caller-provided comparison package cannot fill these members.
	original                *storageEnrollmentPeakOriginalComparison
	initialFence            []byte
	fenced                  bool
	originalRoots           []*storageRootBinding
	candidate               *storageOwnerBindingImage
	candidateObserver       *storageAdvisoryObserver
	factualCensus           *storagebudget.Census
	completedEvidence       *storageEnrollmentPeakEvidence
	initialInspection       *storagebudget.EnrollmentInspection
	initialPacket           *storageEnrollmentPeakEvidence
	previousEvidence        *storageEnrollmentPeakEvidence
	anchoredStaged          *storageEnrollmentPeakEvidence
	anchoredReference       *storageEnrollmentPeakEnvelope
	creation                *storageCreationLive
	identityLaunch          *storageIdentityLaunch
	identityPlan            *storageOwnerV5LineagePlan
	identityInitial         storageCompositionInitial
	identityPrivate         *storageRootBinding
	identityApplications    map[byte][]storagebudget.Component
	identityOriginal        *os.File
	identityStamp           *storageNamedRoot
	identity                *storageIdentityLive
	move                    *storageMoveOperation
	identityDone            bool
	identityRecoveryPending bool
	compositionEntry        *storageCompositionFirstBasePlan
	operands                storageEnrollmentOperands
	compositionCut          byte
	compositionStarted      bool
}

// Filled only by the original admission/reconstruction step. The accepted
// Scope already contains the complete fixed reservation union; this member
// neither tracks cumulative spending nor grants an application reservation.
type storageEnrollmentReservation struct {
	phase *storageEnrollmentEpoch
	scope storageEnrollmentPeakScope
}

// A deterministic fixed boundary seam, never a callback or native capability.
// It can delay an actual owner invocation to verify cancellation/drain ordering.
type storageEnrollmentFault struct {
	point   string
	entered chan struct{}
	release <-chan struct{}
	err     error
	once    sync.Once
	next    *storageEnrollmentFault
}

func (p *storageEnrollmentEpoch) boundary(point string) error {
	for f := p.c.enrollmentFault; f != nil; f = f.next {
		if f.point != point {
			continue
		}
		f.once.Do(func() {
			if f.entered != nil {
				close(f.entered)
			}
			if f.release != nil {
				<-f.release
			}
		})
		return f.err
	}
	return nil
}

func (c *storageBudgetConstruction) beginEnrollmentEpoch(ctx context.Context) (*storageEnrollmentEpoch, error) {
	if c == nil || c.self != c || ctx == nil || ctx.Err() != nil || c.owner == nil || !c.owner.valid() || c.ref == nil || !c.ref.valid() || c.ref.cell.startup != c {
		return nil, errors.Join(storagebudget.ErrAuthority, storageExclusiveContextErr(ctx))
	}
	o := c.owner
	o.mu.Lock()
	g := &o.admission
	g.mu.Lock()
	c.mu.Lock()
	// Pin before the first out-of-lock currentness/native operation. Every
	// ordinary/observer entry uses these same short locks or the owner retain.
	if c.closing || c.closed || c.home == nil || c.home.exclusive || c.server == nil || c.server.storageConstruction != c || o.closing || o.closed || o.transitionRequired || o.active != 0 || o.currentCensus != nil || g.phase != storageAdmissionOpen || g.shared != 0 || g.prefixes != 0 || g.preparation != nil || g.exclusiveCensus != nil || g.enrollment != nil || g.generation == ^uint64(0) || c.startupUse != nil || c.startupWork != nil || len(c.startupHolds) != 0 {
		failed := g.phase == storageAdmissionFailed
		c.mu.Unlock()
		g.mu.Unlock()
		o.mu.Unlock()
		return nil, storageAdmissionBusy(failed)
	}
	g.generation++
	callCtx, cancel := context.WithCancel(ctx)
	p := &storageEnrollmentEpoch{measurementCaller: ctx, c: c, generation: g.generation, ctx: callCtx, cancel: cancel, running: true, done: make(chan struct{})}
	p.self = p
	o.active++
	g.phase, g.enrollment = storageAdmissionEnrollment, p
	c.mu.Unlock()
	g.mu.Unlock()
	o.mu.Unlock()
	// Registration does no I/O and owns no independent retain. Cancellation
	// revokes permission, never the actual descriptions of an entered call.
	p.stopCancel = context.AfterFunc(callCtx, p.revoke)
	return p, nil
}

func (p *storageEnrollmentEpoch) revoke() {
	p.mu.Lock()
	p.revoked = true
	p.mu.Unlock()
}

func (p *storageEnrollmentEpoch) permit(ctx context.Context) error {
	if p == nil || p.self != p || p.c == nil || ctx == nil {
		return storagebudget.ErrAuthority
	}
	// Only the already-registered original completion may use its finite P
	// continuation during cancellation/Closing. This does not accept a flag or
	// alter ordinary enrollment permission.
	if completion := p.originalTransferCompletion(); completion != nil {
		return completion.current(ctx)
	}
	if err := errors.Join(ctx.Err(), p.ctx.Err()); err != nil {
		return err
	}
	o := p.c.owner
	o.mu.Lock()
	g := &o.admission
	g.mu.Lock()
	p.c.mu.Lock()
	p.mu.Lock()
	defer o.mu.Unlock()
	defer g.mu.Unlock()
	defer p.c.mu.Unlock()
	defer p.mu.Unlock()
	if !p.running || p.revoked || o.closing || o.closed || p.c.closing || p.c.closed || g.phase == storageAdmissionClosing {
		return errors.Join(os.ErrClosed, p.err)
	}
	if o.transitionRequired || g.phase != storageAdmissionEnrollment || g.enrollment != p || g.generation != p.generation || o.active != func() uint64 {
		if p.startup {
			return 2
		}
		return 1
	}() || g.shared != 0 || g.prefixes != 0 || g.preparation != nil || g.exclusiveCensus != nil {
		return storagebudget.ErrAuthority
	}
	return p.err
}

func (p *storageEnrollmentEpoch) current(ctx context.Context) error {
	if p != nil && p.self == p && p.transfer != nil {
		if op := p.c.originalTransferOperation(); op != nil && op.preparation.epoch == p {
			return op.originalReadCurrent(ctx)
		}
	}
	if err := p.permit(ctx); err != nil {
		return err
	}
	if err := p.c.current(ctx, p.startup); err != nil {
		return err
	}
	if p.startupProject != nil {
		if err := p.startupProjectCurrent(ctx); err != nil {
			return err
		}
	}
	if p.requester != nil && p.requester != p.c.server {
		if err := p.c.historyRequesterCurrent(ctx, p.requester); err != nil {
			return err
		}
	}
	if p.c.home.exclusive != p.startup {
		return storagebudget.ErrAuthority
	}
	if p.use != nil {
		if err := p.use.Current(); err != nil {
			return err
		}
	}
	if p.project != nil {
		a, err := p.project.Stat()
		b, be := p.originalProjectRoot().Stat(".")
		expected := b
		if p.startupProject != nil {
			expected = p.startupProject.named.identity
		} else {
			original := p.originalServer().project
			b, be = original.own.dirFile.Stat()
			expected = original.info
		}
		if err != nil || be != nil || !os.SameFile(a, b) || !os.SameFile(a, expected) {
			return errors.Join(errStorageCustody, err, be)
		}
	}
	if p.identityPrefixes {
		if p.signingAbsent {
			if _, err := p.c.assistant.secrets.Lstat(signingDirName); !errors.Is(err, os.ErrNotExist) {
				return errors.Join(errStorageCustody, err)
			}
		} else {
			if p.signing == nil || p.signingFile == nil {
				return storagebudget.ErrAuthority
			}
			if err := p.signing.namesHeld(); err != nil {
				return err
			}
			a, err := p.signingFile.Stat()
			b, e := p.signing.root.Stat(".")
			if err != nil || e != nil || !os.SameFile(a, b) {
				return errors.Join(errStorageCustody, err, e)
			}
			native, err := storageAdvisoryPersistentHeld(ctx, p.signingFile)
			if err != nil || native != p.signingNative {
				return errors.Join(errStorageCustody, err)
			}
		}
	}
	if p.transferLock != nil {
		if p.transfer == nil || p.transfer.epoch != p {
			return storagebudget.ErrAuthority
		}
		if err := p.transferLock.current(); err != nil {
			return err
		}
	}
	if p.work != nil {
		if err := p.work.current(); err != nil {
			return err
		}
	}
	for _, edge := range p.sourceEdges {
		if err := edge.current(); err != nil {
			return err
		}
	}
	for _, n := range p.sourceFiles {
		if n.file == nil {
			return storagebudget.ErrAuthority
		}
		held, e := n.file.Stat()
		named, f := n.root.Lstat(n.name)
		if e != nil || f != nil || !os.SameFile(held, n.identity) || !os.SameFile(named, n.identity) || named.Mode()&os.ModeSymlink != 0 {
			return errors.Join(errStorageCustody, e, f)
		}
		if !held.IsDir() {
			if err := n.current(); err != nil {
				return err
			}
		}
	}
	// Every domain belongs to this exact retained maintenance use. This
	// read-only frame already checked that use above; inspect each original
	// physical binding/description without recursively repeating the complete
	// installation check twice per domain. Recheck the installation below before
	// returning. No result or authority is retained beyond this call.
	for _, h := range p.holds {
		if err := p.currentDomainBinding(ctx, h); err != nil {
			return err
		}
	}
	if p.use != nil {
		if err := p.use.Current(); err != nil {
			return err
		}
	}
	return p.permit(ctx)
}

// Local physical predicates inside current's read-only installation bracket.
// Alias descriptions must belong to the exact same use and retain their own
// root, prefix and file witnesses. This returns no reusable proof or permit.
func (p *storageEnrollmentEpoch) currentDomainBinding(ctx context.Context, h *storageDomainHold) error {
	if h == nil || h.self != h || h.closed || h.root == nil || h.owner != p.c.owner || h.root.owner != h.owner || p.use == nil || h.lifetime != p.use {
		return storagebudget.ErrAuthority
	}
	if err := h.root.bindingCurrent(ctx); err != nil {
		return err
	}
	if !h.root.qControl && h.prefix == nil && h.borrowed != nil {
		return p.currentDomainBinding(ctx, h.borrowed)
	}
	return h.descriptionCurrent(ctx)
}

func (p *storageEnrollmentEpoch) acquire(ctx context.Context) error {
	return p.acquirePrefixes(ctx, false)
}
func (p *storageEnrollmentEpoch) acquireIdentity(ctx context.Context) error {
	return p.acquirePrefixes(ctx, true)
}
func (p *storageEnrollmentEpoch) acquirePrefixes(ctx context.Context, identity bool) error {
	if err := p.current(ctx); err != nil {
		return err
	}
	if p.use != nil || p.project != nil || p.work != nil || len(p.holds) != 0 {
		return storagebudget.ErrAuthority
	}
	var err error
	// The original long-lived home SH remains untouched. These independent
	// descriptions are taken try-only, in use/project/work/domain order: the
	// project and transfer locks EX. The work lock is EX only for a chat
	// transfer (backup, restore, move), which refuses while work runs; any
	// other enrollment takes it SH, as running work does (ADR 0003, ADR 0014),
	// so it excludes a transfer or move and not the work a write runs inside.
	// In-process writers are ordered by admission, not by this lock.
	p.use, err = p.c.owner.maintenanceUse()
	if err != nil {
		return err
	}
	p.project, err = p.originalProjectRoot().Open(".")
	if err != nil {
		return err
	}
	if err = lockProjectFile(p.project); err != nil {
		return storageLockBusy(err)
	}
	if identity {
		if err = p.acquireIdentitySigning(ctx); err != nil {
			return err
		}
	}
	if p.transfer != nil {
		if p.transfer.epoch != p {
			return storagebudget.ErrAuthority
		}
		if err = p.transfer.current(); err != nil {
			return err
		}
		transfer, e := storageOpenOriginalControl(p.c.ref.cell, ".data-transfer.lock")
		if e != nil {
			return e
		}
		p.transferLock = &transfer
		if e = lockPrivateData(transfer.file, true); e != nil {
			return storageLockBusy(e)
		}
	}
	w, err := storageOpenOriginalControl(p.c.ref.cell, ".data-work.lock")
	if err != nil {
		return err
	}
	p.work = &w
	if err = lockPrivateData(w.file, p.transfer != nil); err != nil {
		return storageLockBusy(err)
	}
	p.originalRoots = p.c.owner.orderedRoots()
	if p.transfer != nil {
		// The lexical transfer owner takes writes/FIFO/private next. It then
		// installs that exact EX as a domain alias before acquiring any peer.
		return p.current(ctx)
	}
	// The separately held original project EX may be the actual domain inode
	// of an already enrolled restricted parent. Reuse its owned description in
	// maintenanceUse's existing alias map; every returned hold borrows it and
	// finish remains the sole owner of its Close.
	for _, description := range []*os.File{p.project, p.signingFile} {
		if description == nil {
			continue
		}
		info, e := description.Stat()
		if e != nil {
			return e
		}
		for _, r := range p.originalRoots {
			if !r.domainDirectory || r.domain.file == nil || !os.SameFile(info, r.domain.identity) {
				continue
			}
			id, e := storageFileIdentity(r.domain.identity)
			if e != nil {
				return e
			}
			if p.use.domains == nil {
				p.use.domains = map[storageDirectoryIdentity]*storageDomainHold{}
			}
			if p.use.domains[id] == nil {
				h := &storageDomainHold{owner: p.c.owner, root: r, lifetime: p.use, file: description}
				h.self = h
				p.use.domains[id] = h
			}
		}
	}
	for _, r := range p.originalRoots {
		// maintenanceUse's existing domain map coalesces actual inode aliases
		// while retaining each original root/category/source relationship.
		h, err := p.c.owner.domain(ctx, r, p.use)
		if err != nil {
			return err
		}
		p.holds = append(p.holds, h)
	}
	p.sourceHoldsComplete = true
	return p.current(ctx)
}

// storageAdmissionBusy is the refusal of an admission that cannot enter.
// After an enrollment that could not be reconciled (a census refusal, a
// refused move), admission stays failed until the original owner recovers it
// at a restart: still ErrBusy for callers, and also the recovery cause, so
// the refusal is worded instead of an internal error.
func storageAdmissionBusy(failed bool) error {
	if failed {
		return errors.Join(storagebudget.ErrBusy, errStorageEnrollmentUnresolved)
	}
	return storagebudget.ErrBusy
}

// storageLockBusy is ErrBusy for a try-only storage lock not taken. One held
// elsewhere is also worded as the refusal chat storage gave before the
// budget (409, CodeStale), instead of an internal error.
func storageLockBusy(err error) error {
	if !lockHeld(err) {
		return errors.Join(storagebudget.ErrBusy, err)
	}
	return errors.Join(storagebudget.ErrBusy, withCode(CodeStale, fmt.Errorf("chat storage is busy or unavailable; finish active work and retry: %w", err)))
}

func (p *storageEnrollmentEpoch) held(ctx context.Context) error {
	if p == nil || p.self != p || p.c == nil || p.use == nil || p.project == nil || p.work == nil || len(p.holds) != len(p.originalRoots) {
		return storagebudget.ErrAuthority
	}
	for i, r := range p.originalRoots {
		if p.holds[i] == nil || p.holds[i].root != r {
			return storagebudget.ErrAuthority
		}
	}
	return p.current(ctx)
}

// Close only revokes and requests cancellation. The owning invocation must
// return from every native call and owned Close before finish releases anything.
func (p *storageEnrollmentEpoch) Close() error {
	if p == nil || p.self != p {
		return storagebudget.ErrAuthority
	}
	p.mu.Lock()
	p.revoked = true
	running, err := p.running, p.err
	p.mu.Unlock()
	p.cancel()
	if running {
		return errors.Join(err, storagebudget.ErrBusy)
	}
	return err
}

func (p *storageEnrollmentEpoch) finish(err error) error {
	if p == nil || p.self != p {
		return storagebudget.ErrAuthority
	}
	p.mu.Lock()
	if !p.running || p.entered || p.finishing {
		prior, busy := p.err, p.entered || p.finishing
		p.mu.Unlock()
		if busy {
			return errors.Join(err, prior, storagebudget.ErrBusy)
		}
		// A completed epoch owns no further cleanup. Preserve a later caller's
		// error (notably the fresh successor epoch) through an outer finalizer.
		return errors.Join(err, prior)
	}
	policyUnrevoked := !p.revoked
	p.finishing = true
	p.revoked = true
	defer func() { p.operands = storageEnrollmentOperands{} }()
	p.mu.Unlock()
	if p.stopCancel != nil {
		p.stopCancel()
	}
	p.cancel()
	// Description Close is mandatory even after cancellation. Only this
	// synchronous owner closes them, after its last actual native operation.
	var cleanup error
	if p.creation != nil {
		cleanup = errors.Join(cleanup, p.creation.Close())
	}
	if p.identity != nil {
		cleanup = errors.Join(cleanup, p.identity.Close())
	}
	if p.identityStamp != nil {
		cleanup = errors.Join(cleanup, p.identityStamp.current(), p.identityStamp.root.Close())
		p.identityStamp = nil
	}
	if p.identityOriginal != nil {
		cleanup = errors.Join(cleanup, p.identityOriginal.Close())
		p.identityOriginal = nil
	}
	if p.candidate != nil {
		cleanup = errors.Join(cleanup, p.candidate.Close())
	}
	for i := len(p.targets) - 1; i >= 0; i-- {
		if p.targets[i].named.file != nil {
			cleanup = errors.Join(cleanup, p.targets[i].named.file.Close())
			p.targets[i].named.file = nil
		}
	}
	for i := len(p.parents) - 1; i >= 0; i-- {
		if p.parents[i] != nil {
			cleanup = errors.Join(cleanup, p.parents[i].Close())
			p.parents[i] = nil
		}
	}
	if p.transfer != nil {
		cleanup = errors.Join(cleanup, p.closeOriginalTransferDomains())
	} else {
		for i := len(p.holds) - 1; i >= 0; i-- {
			if p.holds[i] != nil {
				cleanup = errors.Join(cleanup, p.holds[i].Close())
			}
		}
	}
	p.holds = nil
	for i := len(p.sourceFiles) - 1; i >= 0; i-- {
		cleanup = errors.Join(cleanup, p.sourceFiles[i].file.Close())
	}
	p.sourceFiles = nil
	for i := len(p.sourceRoots) - 1; i >= 0; i-- {
		cleanup = errors.Join(cleanup, p.sourceRoots[i].Close())
	}
	p.sourceRoots = nil
	p.sourceEdges = nil
	if p.work != nil {
		if p.move != nil {
			cleanup = errors.Join(cleanup, closeDialectArtifactOwner(p.measurementCaller, "move-work", p.work.file.Close))
		} else {
			cleanup = errors.Join(cleanup, p.work.file.Close())
		}
		p.work = nil
	}
	if p.transferLock != nil {
		cleanup = errors.Join(cleanup, p.transferLock.file.Close())
		p.transferLock = nil
	}
	if p.signingFile != nil {
		cleanup = errors.Join(cleanup, p.signingFile.Close())
		p.signingFile = nil
	}
	if p.signing != nil {
		cleanup = errors.Join(cleanup, p.signing.root.Close())
		p.signing = nil
	}
	if p.project != nil {
		if p.move != nil && p.move.lease != nil {
			cleanup = errors.Join(cleanup, p.move.lease.Close())
		} else {
			cleanup = errors.Join(cleanup, p.project.Close())
		}
		p.project = nil
	}
	if p.use != nil {
		cleanup = errors.Join(cleanup, p.use.Close())
		p.use = nil
	}
	o := p.c.owner
	o.mu.Lock()
	g := &o.admission
	g.mu.Lock()
	p.mu.Lock()
	p.err = errors.Join(p.err, err, cleanup)
	p.running = false
	// This detached result belongs only to this synchronous policy invocation.
	// Record it before releasing its original admission exclusion, after all
	// owned descriptions returned from Close. It grants no effect permission.
	policyNoEffect := p.policy != nil && !p.policy.started && p.policy.caller != nil && p.policy.caller.Err() == nil && policyUnrevoked && !p.startup && p.transfer == nil && p.scope == nil && p.creation == nil && p.identity == nil && !p.identityRecoveryPending && !p.compositionStarted && !o.closing && !o.closed && g.phase == storageAdmissionEnrollment && g.enrollment == p && g.generation == p.generation && o.active == 1 && g.shared == 0 && g.prefixes == 0 && g.preparation == nil && g.exclusiveCensus == nil
	if g.enrollment == p {
		g.enrollment = nil
		if g.phase != storageAdmissionClosing {
			// A no-effect acquisition/refusal can restore Open after actual
			// cleanup. A persisted scope needs exact final reconciliation.
			transferSafe := (p.transfer == nil || !p.transfer.admitted || p.transfer.completed) && (p.policy == nil || !p.policy.started || p.policy.finished)
			if cleanup == nil && transferSafe && (p.scope == nil && (p.creation == nil || !p.creation.started || p.creation.finished) && (p.identity == nil || !p.identity.started || p.identity.finished) && (!p.identityRecoveryPending || p.identityDone) && (!p.compositionStarted || p.compositionCut == 4 && p.err == nil && (p.identityPlan == nil || p.identityDone)) || p.reconciled && p.err == nil) && !o.transitionRequired {
				if p.startup {
					g.phase = storageAdmissionStartup
				} else {
					g.phase = storageAdmissionOpen
					if p.policy != nil {
						p.policy.noEffect = storagePolicyNoEffect(policyNoEffect)
					}
				}
			} else {
				g.phase = storageAdmissionFailed
			}
		}
	}
	p.mu.Unlock()
	g.mu.Unlock()
	o.mu.Unlock()
	dropErr := o.drop()
	p.mu.Lock()
	p.err = errors.Join(p.err, dropErr)
	if p.policy != nil && (dropErr != nil || p.policy.caller == nil || p.policy.caller.Err() != nil) {
		p.policy.noEffect = false
	}
	p.finishing = false
	result := p.err
	close(p.done)
	p.mu.Unlock()
	if p.transfer == nil && p.reconciled {
		if p.startup {
			o.stageStartupMeasurement(p.measurement, p.c, p.measurementCaller, result)
		} else {
			o.finishOwnedMeasurement(p.measurement, p.measurementCaller, result)
		}
	}
	return result
}

// Existing fault seam with a closed source-family suffix. This comparison
// label has no schedule, permit, native record or application control surface.
func (p *storageEnrollmentEpoch) successorBoundary(point string) error {
	if p.initialPacket != nil {
		family := storageEnrollmentSuccessorFamily(p.initialPacket.Board.SourceKind, p.initialPacket.Board.Source)
		if family != 0 {
			if err := p.boundary(point + "-family" + string([]byte{'0' + family})); err != nil {
				return err
			}
		}
	}
	return p.boundary(point)
}

func (p *storageEnrollmentEpoch) originalServer() *Server {
	if p.startupProject != nil {
		return nil
	}
	if p.requester != nil {
		return p.requester
	}
	return p.c.server
}
func (p *storageEnrollmentEpoch) originalProjectRoot() *os.Root {
	if p.startupProject != nil {
		return p.startupProject.named.root
	}
	return p.originalServer().project.own.root
}
func (p *storageEnrollmentEpoch) originalProjectPath() string {
	if p.startupProject != nil {
		return filepath.Join(p.c.server.home.Path, p.startupProject.entry.Folder)
	}
	return p.originalServer().projectDir
}
func (p *storageEnrollmentEpoch) originalHomeEntry() *deskHomeEntry {
	if p.startupProject != nil {
		return &p.startupProject.entry
	}
	return p.originalServer().homeEntry()
}
func (p *storageEnrollmentEpoch) originalHistoryIdentity(ctx context.Context) (string, error) {
	if entry := p.originalHomeEntry(); entry != nil {
		return entry.HistoryIdentity, nil
	}
	if p.originalServer() == p.c.server {
		return p.c.originalStorageHistory(ctx)
	}
	return p.originalServer().conversationIdentity(), nil
}
func (p *storageEnrollmentEpoch) originalFolderKey() string {
	if p.startupProject != nil {
		return identityKey(p.startupProject.named.identity)
	}
	return p.originalServer().folderKey()
}
func (p *storageEnrollmentEpoch) startupProjectCurrent(ctx context.Context) error {
	if !p.startup || p.requester != nil || p.c.sources == nil || p.c.server.home == nil || p.startupProject == nil {
		return storagebudget.ErrAuthority
	}
	if err := p.c.sources.currentProjects(ctx); err != nil {
		return err
	}
	original, ok := p.c.sources.projects[p.startupProject.entry.ID]
	if !ok || original.entry != p.startupProject.entry || original.named.root != p.startupProject.named.root || original.named.parent != p.startupProject.named.parent {
		return errStorageCustody
	}
	return p.startupProject.named.current()
}
func (p *storageEnrollmentEpoch) selectOriginalRequester(ctx context.Context, s *Server) error {
	if p == nil || p.self != p || s == nil || p.requester != nil || p.startupProject != nil || p.project != nil || p.use != nil {
		return storagebudget.ErrAuthority
	}
	if s != p.c.server || !p.startup {
		if err := p.c.historyRequesterCurrent(ctx, s); err != nil {
			return err
		}
	}
	if s.project == nil || s.project.own == nil || s.project.own.root != s.root {
		return storagebudget.ErrAuthority
	}
	p.requester = s
	return nil
}

// These are detached comparison values, not a request-selected source or a
// native permit. The caller separately owns original entry/use/source custody.
type storagePolicySnapshot struct {
	Installation              storagebudget.ID
	Sequence, Revision, Epoch uint64
	Workspace                 storagebudget.RegisteredWorkspace
	Policy                    storagebudget.Policy
}

type storagePolicyEdit struct {
	Expected                         storagePolicySnapshot
	Installation, Default, Workspace *storagebudget.Limits
}

type storagePolicyOriginal struct {
	Moves        []storageMoveCurrent
	Actual       [2][]byte
	Selected     byte
	Catalog      storageOwnerCatalogV2
	Evidence     storageEnrollmentPeakEvidence
	Lineage      storageCompositionLineage
	Effective    storageOwnerDefinitions
	Policy       storageOwnerPolicy
	Overrides    []byte
	Source       []byte
	Packed       *storageOwnerV6PackedCatalog
	Native       bool
	Boundary     storagePolicyQBoundary
	RecoveryOnly bool
}

// Compare selectors only AFTER the original strict pair decode. Identical
// duplicated physical images retain one semantic original bank in their closed
// publication/scope relation. The header tie-break is only a read bootstrap;
// it cannot override that relation. No distinct-image disagreement qualifies.
// This pure comparison does not decode, establish currentness or grant custody.
func storagePolicyOriginalPairBank(actual [2][]byte, bootstrap, semantic byte, duplicated bool) bool {
	return len(actual[0]) == storageOwnerCatalogBytes && len(actual[1]) == storageOwnerCatalogBytes && bootstrap <= 1 && semantic <= 1 && (bootstrap == semantic || duplicated && bytes.Equal(actual[0], actual[1]))
}

// The original fixed descriptions supply these bytes. Every decoded source is
// joined to the complete actual evidence, its armed peer and original Ready.
// A recognized V6 frame never falls through to an old reader.
func (o *storageBudgetOwner) readOriginalPolicy(ctx context.Context) (out storagePolicyOriginal, err error) {
	if !o.valid() || o.ref.cell.ownerRecord == nil || o.ref.cell.startup == nil {
		return out, storagebudget.ErrAuthority
	}
	c := o.ref.cell.startup
	if err = c.current(ctx, c.home.exclusive); err != nil {
		return out, err
	}
	record, claim := *o.ref.cell.ownerRecord, o.ref.cell.ownerClaim
	for i, name := range []string{"storage-owner.a", "storage-owner.b"} {
		n, e := storageOpenOriginalControl(o.ref.cell, name)
		if e != nil {
			return out, e
		}
		raw := make([]byte, storageOwnerCatalogBytes)
		count, e := n.file.ReadAt(raw, 0)
		e = errors.Join(e, n.current(), n.file.Close())
		if e != nil || count != len(raw) {
			return out, errors.Join(errStorageCustody, e)
		}
		out.Actual[i] = raw
	}
	if storageEnrollmentPeakU64(out.Actual[1][16:24]) > storageEnrollmentPeakU64(out.Actual[0][16:24]) {
		out.Selected = 1
	}
	selected := out.Actual[out.Selected]
	var core []byte
	var packed *storageOwnerV6PackedCatalog
	if bytes.Equal(selected[:8], storageOwnerV6Magic[:]) {
		payload, sequence, e := storageOwnerV6Payload(selected)
		if e != nil {
			return out, e
		}
		if bytes.HasPrefix(payload, storageOwnerV6NativeMagic[:]) {
			// Decode comparison fields first. The full pair (including a still
			// packed predecessor) is joined only after its real E1 evidence and
			// Ready have been read below; no partial decoder grants currentness.
			v, e := storageOwnerV6NativeDecode(selected, record, claim)
			if e != nil {
				return out, e
			}
			core, out.Lineage, out.Policy, out.Overrides = v.Stage.Core, v.Stage.Lineage, v.Policy, append([]byte{}, v.Overrides...)
			out.Moves = v.Stage.Moves
			out.Native = true
			out.Boundary = v.Boundary
		} else {
			if !storageOwnerV6PackedPreflight(payload) {
				return out, storagebudget.ErrBounds
			}
			var v storageOwnerV6PackedCatalog
			if json.Unmarshal(payload, &v) != nil || v.Sequence != sequence {
				return out, storagebudget.ErrBounds
			}
			source, lineage, e := storageOwnerDecodePackedSource(v.Source)
			if e != nil {
				return out, e
			}
			core = make([]byte, storageOwnerCatalogBytes)
			copy(core, source.Core)
			out.Lineage, out.Policy, out.Overrides = lineage, v.EffectivePolicy, append([]byte{}, v.Overrides...)
			packed = &v
		}
	} else if string(selected[:8]) == "DSKONL1\x00" {
		pair, e := storageCompositionReadPair(out.Actual[0], out.Actual[1], record, claim)
		if e != nil || pair.RecoveryOnly || !storagePolicyOriginalPairBank(out.Actual, out.Selected, pair.Selected, pair.Duplicated) {
			return out, errors.Join(errStorageEnrollmentUnresolved, e)
		}
		// Equal physical headers have no primary-bank meaning. The strict
		// completed publication/scope relation selects its original bank.
		out.Selected = pair.Selected
		selected = out.Actual[out.Selected]
		core, out.Lineage = pair.Banks[out.Selected].Core, pair.Banks[out.Selected].Lineage
		out.Moves = pair.Banks[out.Selected].Moves
	} else {
		if !bytes.Equal(out.Actual[0], out.Actual[1]) {
			return out, errStorageEnrollmentUnresolved
		}
		core = selected
		out.Lineage = storageCompositionEmptyLineage()
	}
	outer, e := storageEnrollmentPeakDecodeEnvelopeVariant(core, record, claim, true, true)
	if e != nil || outer.Kind != 2 || outer.Marker == nil || outer.Marker.Phase != 3 || outer.Scope.requireCapacityEstimate() != nil {
		return out, errors.Join(errStorageEnrollmentUnresolved, e)
	}
	var evidence [2]storageEnrollmentPeakEvidence
	for i := byte(0); i < 2; i++ {
		n, e := storageOpenFile(c.original.config, storageEnrollmentNativeName(1+i), int64(outer.Scope.final(i)))
		if e != nil {
			return out, e
		}
		native, e := storageOwnerPersistentFile(ctx, n)
		if e != nil || native != outer.Scope.target(1+i) {
			return out, errors.Join(errStorageCustody, e, n.file.Close())
		}
		raw := make([]byte, n.size)
		count, e := n.file.ReadAt(raw, 0)
		e = errors.Join(e, n.current(), n.file.Close())
		if e != nil || count != len(raw) {
			return out, errors.Join(errStorageCustody, e)
		}
		v, e := storageEnrollmentPeakDecodeEvidenceWithLineage(raw, &out.Lineage, record, claim)
		if e != nil {
			return out, e
		}
		evidence[i] = v
	}
	out.Evidence = evidence[outer.Marker.Bank]
	if !storageEnrollmentPeakJoinArmed(out.Evidence, evidence[1-outer.Marker.Bank]) {
		return out, errStorageCustody
	}
	ready, n, e := storageOwnerReadFixed(c.original.config, storageReadyName, 32)
	if e != nil {
		return out, e
	}
	e = errors.Join(n.current(), n.file.Close())
	if e != nil || storageEnrollmentPeakJoinMarker(outer, out.Evidence, sha256.Sum256(ready)) != nil || !bytes.Equal(ready, record.QBinding[:]) {
		return out, errors.Join(errStorageCustody, e)
	}
	out.Catalog = out.Evidence.Inner.Catalog
	if out.Lineage.validateGraph(out.Catalog, record) != nil {
		return out, errStorageCustody
	}
	if packed != nil {
		v, e := storageOwnerV6PackedDecode(selected, out.Evidence.Raw, record, claim)
		if e != nil {
			return out, e
		}
		_, source, lineage, e := v.validate(out.Evidence.Raw, record, claim)
		if e != nil || !reflect.DeepEqual(source, out.Catalog) || !reflect.DeepEqual(lineage, out.Lineage) {
			return out, errors.Join(errStorageCustody, e)
		}
		out.Packed = &v
		endpoint, e := storageOwnerV6NativeReadEndpoint(selected, out.Evidence.Raw, record, claim)
		if e != nil {
			return out, e
		}
		out.Moves = endpoint.Stage.Moves
		out.Source = append([]byte{}, v.Source...)
		if v.Phase == "prepared" {
			peer := out.Actual[1-out.Selected]
			if v.Sequence != storageEnrollmentPeakU64(peer[16:24])+1 || !bytes.Equal(v.PreviousCatalogDigest[:], peer[32:64]) {
				return out, errStorageEnrollmentUnresolved
			}
			if bytes.Equal(peer[:8], storageOwnerV6Magic[:]) {
				old, e := storageOwnerV6PackedDecode(peer, out.Evidence.Raw, record, claim)
				if e == nil {
					if old.Phase != "active" && old.Phase != "aborted" && old.Phase != "aborted-unfenced" || !bytes.Equal(old.Source, v.Source) || old.Transition == v.Transition {
						return out, errStorageCustody
					}
					_, defs, e := source.definitions()
					if e != nil {
						return out, e
					}
					rows, e := storageOwnerV6DecodeSparse(old.Overrides, defs)
					if e != nil {
						return out, e
					}
					before, e := storageOwnerV6Expand(defs, old.EffectivePolicy, rows)
					if e != nil {
						return out, e
					}
					digest, e := storageOwnerV6EffectiveDigest(before)
					if e != nil || digest != v.BeforeEffectiveDigest {
						return out, errors.Join(errStorageCustody, e)
					}
				} else {
					_, before, _, _, e := storageOwnerV6PackedSourceForPolicy(v, out.Evidence.Raw, record, claim)
					if e != nil || !bytes.Equal(before[1-out.Selected], peer) {
						return out, errors.Join(errStorageCustody, e)
					}
				}
			} else {
				_, before, _, _, e := storageOwnerV6PackedSourceForPolicy(v, out.Evidence.Raw, record, claim)
				if e != nil || !bytes.Equal(before[1-out.Selected], peer) {
					return out, errors.Join(errStorageCustody, e)
				}
			}
		} else {
			projection, e := storagePolicyBeforeProjection(source, source, selected)
			if e != nil || storagePolicyRetainProjectionPeer(&projection, out.Actual, out.Selected, out.Evidence.Raw, record, claim) != nil {
				return out, errors.Join(errStorageCustody, e)
			}
		}
	} else if out.Native {
		pair, e := storageOwnerV6NativeReadPair(out.Actual[0], out.Actual[1], &out.Catalog, out.Evidence.Raw, record, claim)
		if e != nil || !storagePolicyOriginalPairBank(out.Actual, out.Selected, pair.Selected, pair.Duplicated) || pair.RecoveryOnly && !storagePolicyNativePreparedLineage(pair) && !storageMovePendingPair(pair) {
			return out, errors.Join(errStorageEnrollmentUnresolved, e)
		}
		out.Selected = pair.Selected
		selected = out.Actual[out.Selected]
		out.RecoveryOnly = pair.RecoveryOnly
		v, e := storageOwnerV6NativeDecode(selected, record, claim)
		if e != nil {
			return out, e
		}
		if _, e = v.validateEvidence(out.Evidence.Raw, record, claim); e != nil {
			return out, e
		}
		if !out.RecoveryOnly {
			out.Source, e = storageOwnerV6NativePolicySource(out.Actual, out.Selected, out.Evidence.Raw, out.Catalog, record, claim)
			if e != nil {
				return out, e
			}
		}
	} else {
		out.Source, e = storageOwnerPackedSourceOriginal(out.Actual, out.Evidence.Raw, record, claim)
		if e != nil {
			return out, e
		}
		out.Policy = out.Catalog.Policy
		out.Overrides = []byte{0, 0}
	}
	if out.Packed != nil && out.Packed.Phase != "prepared" {
		if out.Packed.TransitionKind == storageOwnerV6PolicyTransition {
			out.Boundary, e = storagePolicyOutcomeBoundary(*out.Packed)
			if e != nil {
				return out, e
			}
		} else {
			out.Boundary = storagePolicyQBoundary{out.Evidence.AfterEpoch, out.Evidence.AfterRevision}
		}
	} else if !out.Native && out.Packed == nil {
		out.Boundary = storagePolicyQBoundary{out.Evidence.AfterEpoch, out.Evidence.AfterRevision}
	}
	_, defs, e := out.Catalog.definitions()
	if e != nil {
		return out, e
	}
	rows, e := storageOwnerV6DecodeSparse(out.Overrides, defs)
	if e != nil {
		return out, e
	}
	out.Effective, e = storageOwnerV6Expand(defs, out.Policy, rows)
	if e != nil {
		return out, e
	}
	c.ref.cell.mu.Lock()
	endpoint, catalog := c.ref.cell.enrollmentEndpoint, c.ref.cell.ownerCatalog
	c.ref.cell.mu.Unlock()
	if endpoint != nil && !bytes.Equal(endpoint.Raw, out.Evidence.Raw) || catalog.Installation != (storagebudget.ID{}) && !reflect.DeepEqual(catalog, out.Catalog) {
		return out, errStorageOwnerTransitionRequired
	}
	return out, c.current(ctx, c.home.exclusive)
}

// Read-only: unlike explicit prepare, this never enrolls or recovers. All
// revisions come from this held original load, never the status cache.
func (s *Server) readStoragePolicySnapshot(ctx context.Context) (out storagePolicySnapshot, err error) {
	e, o, use, err := s.storageStatusLifetime(ctx)
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, use.Close(), o.drop()) }()
	state, err := o.readOriginalPolicy(ctx)
	if err != nil {
		return out, err
	}
	if state.Packed != nil && state.Packed.Phase == "prepared" {
		return out, errStorageEnrollmentUnresolved
	}
	q, err := use.cell.q.InspectAction(ctx)
	if err != nil {
		return out, err
	}
	if err = storagePolicyCompletedQ(state, q); err != nil {
		return out, err
	}
	workspace, err := e.construction.storageStatusWorkspace(s)
	if err != nil {
		return out, err
	}
	for _, row := range state.Effective.Workspaces {
		if row.ID == workspace {
			if out.Workspace.ID != (storagebudget.ID{}) {
				return out, storagebudget.ErrAuthority
			}
			out.Workspace = row
		}
	}
	if out.Workspace.ID == (storagebudget.ID{}) {
		return out, storagebudget.ErrAuthority
	}
	out.Installation = use.cell.record.Installation
	out.Sequence = storageEnrollmentPeakU64(state.Actual[state.Selected][16:24])
	out.Revision, out.Epoch = q.View.Configuration.Revision, q.View.Configuration.Epoch
	out.Policy = q.View.Configuration.Policy
	return out, errors.Join(use.Current(), e.statusCurrent(ctx, s, o))
}

// The policy operation is a closed member of the existing original epoch, not
// another authority/registry. Only that invocation fills it, consumes its five
// groups and performs native calls; finish owns every actual Close and drain.
// Private invocation-local response provenance, never an admission permit.
// Only finish may set true after a no-effect invocation drains successfully.
type storagePolicyNoEffect bool

type storagePolicyOperation struct {
	caller                              context.Context
	noEffect                            storagePolicyNoEffect
	phase                               *storageEnrollmentEpoch
	original                            storagePolicyOriginal
	transition                          storagebudget.PolicyTransition
	prepared, active, aborted, unfenced []byte
	bank                                byte
	consumed                            [5]bool
	spent                               storagebudget.CapacityVector
	started, finished                   bool
}

func (m *storagePolicyOperation) nativeCurrent(ctx context.Context) error {
	if m == nil || m.phase == nil || m.phase.self != m.phase || m.phase.policy != m || m.phase.scope != nil || m.phase.qualified == nil || m.finished {
		return storagebudget.ErrAuthority
	}
	if err := m.phase.held(ctx); err != nil {
		return err
	}
	if err := m.phase.qualified.current(ctx, m.phase); err != nil {
		return err
	}
	return m.phase.permit(ctx)
}

// Fresh observations preserve the original enrolled physical headroom and all
// live Q capacity promises. Logical policy edits cannot change these operands.
// The same whole remaining estimate is charged on every possibly shared view.
func storagePolicyPhysicalRoom(status storagebudget.StatusSnapshot, observations []storagebudget.CapacityObservation, demand storagebudget.CapacityVector, now time.Time) error {
	if now.IsZero() || demand.Bytes > 1<<60 || demand.Inodes > 1<<40 || len(observations) != len(status.CapacityConstraints) {
		return storagebudget.ErrBounds
	}
	for _, row := range status.CapacityConstraints {
		var observed *storagebudget.CapacityObservation
		for i := range observations {
			if observations[i].Constraint == row.ID {
				if observed != nil {
					return storagebudget.ErrBounds
				}
				observed = &observations[i]
			}
		}
		if observed == nil || observed.Generation != row.Generation || observed.KnownResources != row.Resources || observed.At.IsZero() || observed.At.After(now) || now.Sub(observed.At) > status.Policy.ObservationMaxAge {
			return storagebudget.ErrUnknown
		}
		promised := storagebudget.CapacityVector{}
		for _, p := range status.CapacityPromised {
			if p.Constraint == row.ID {
				if p.Generation != row.Generation {
					return storagebudget.ErrStale
				}
				promised = p.Peak
			}
		}
		for _, axis := range []struct {
			enabled                          bool
			available, headroom, peak, extra uint64
		}{
			{row.Resources&storagebudget.CapacityBytes != 0, observed.Available.Bytes, row.Headroom.Bytes, promised.Bytes, demand.Bytes},
			{row.Resources&storagebudget.CapacityInodes != 0, observed.Available.Inodes, row.Headroom.Inodes, promised.Inodes, demand.Inodes},
		} {
			if axis.enabled && (axis.available < axis.headroom || axis.peak > axis.available-axis.headroom || axis.extra > axis.available-axis.headroom-axis.peak) {
				return storagebudget.ErrHeadroom
			}
		}
	}
	return nil
}

var storagePolicyEffects = [5]storageOwnerBootstrapEffect{
	{8388608, 0, 0, 1}, {4194304, 0, 0, 1}, {4194304, 0, 0, 1}, {8388608, 0, 0, 1}, {4194304, 0, 0, 1},
}

func (m *storagePolicyOperation) room(ctx context.Context, demand storagebudget.CapacityVector) error {
	if err := m.nativeCurrent(ctx); err != nil {
		return err
	}
	status, err := m.phase.c.ref.cell.q.StatusSnapshot(ctx)
	if err != nil {
		return err
	}
	ids := make([]storagebudget.ID, 0, len(status.CapacityConstraints))
	for _, row := range status.CapacityConstraints {
		ids = append(ids, row.ID)
	}
	observations, err := m.phase.originalEnrollmentObservations(ctx, ids)
	if err != nil {
		return err
	}
	if err = storagePolicyPhysicalRoom(status, observations, demand, time.Now()); err != nil {
		return err
	}
	return m.phase.held(ctx)
}

func (m *storagePolicyOperation) take(ctx context.Context, group int) error {
	if group < 0 || group >= len(m.consumed) || m.consumed[group] {
		return storagebudget.ErrAuthority
	}
	if err := m.nativeCurrent(ctx); err != nil {
		return err
	}
	e := storagePolicyEffects[group]
	demand, err := storageAdvisoryEstimate(e.bytes, e.files, e.directories, e.metadata)
	if err != nil {
		return err
	}
	if m.spent.Bytes > 1<<60-demand.Bytes || m.spent.Inodes > 1<<40-demand.Inodes {
		return storagebudget.ErrBounds
	}
	total := storagebudget.CapacityVector{Bytes: m.spent.Bytes + demand.Bytes, Inodes: m.spent.Inodes + demand.Inodes}
	if err = m.room(ctx, total); err != nil {
		return err
	}
	// No failed or unknown effect may reuse this group during this lifetime.
	m.consumed[group] = true
	m.spent = total
	m.started = true
	return m.phase.enterNative(ctx)
}

func (m *storagePolicyOperation) qStep(ctx context.Context, group int) error {
	if group != 1 && group != 2 && group != 4 {
		return storagebudget.ErrAuthority
	}
	if group == 1 && !m.consumed[0] || group == 2 && !m.consumed[1] || group == 4 && !m.consumed[3] {
		return storagebudget.ErrAuthority
	}
	if err := m.phase.boundary([]string{"policy-unused", "policy-begin-before", "policy-apply-before", "policy-unused", "policy-finish-before"}[group]); err != nil {
		return err
	}
	if err := m.take(ctx, group); err != nil {
		return err
	}
	var err error
	switch group {
	case 1:
		err = m.phase.c.ref.cell.q.BeginPolicyTransition(ctx, m.transition)
	case 2:
		err = m.phase.c.ref.cell.q.ApplyPolicyTransition(ctx, m.transition)
	case 4:
		err = m.phase.c.ref.cell.q.FinishPolicyTransition(ctx, m.transition)
	}
	return m.phase.leaveNative(err)
}

func (s *Server) prepareStoragePolicy(ctx context.Context) error {
	e := s.storageEnrollment
	if e == nil || e.construction == nil || e.construction.owner == nil {
		return storagebudget.ErrAuthority
	}
	if err := e.statusCurrent(ctx, s, e.construction.owner); err != nil {
		return err
	}
	return e.construction.prepareOriginalPolicyFor(ctx, s)
}

// Original requester/currentness and all mutable operands are re-read while the
// existing epoch owns use/project/work/domain and bootstrap EX. Browser values
// select logical coordinates only; they cannot supply source or effect rights.
func (s *Server) updateStoragePolicy(ctx context.Context, edit storagePolicyEdit) (storagePolicySnapshot, error) {
	out, _, err := s.updateStoragePolicyInvocation(ctx, edit)
	return out, err
}

func (s *Server) updateStoragePolicyInvocation(ctx context.Context, edit storagePolicyEdit) (out storagePolicySnapshot, noEffect storagePolicyNoEffect, err error) {
	caller := ctx
	e := s.storageEnrollment
	if e == nil || e.construction == nil || e.construction.owner == nil {
		return out, false, storagebudget.ErrAuthority
	}
	if err = e.statusCurrent(ctx, s, e.construction.owner); err != nil {
		return out, false, err
	}
	p, err := e.construction.beginEnrollmentEpoch(ctx)
	if err != nil {
		return out, false, err
	}
	defer func() {
		err = p.finish(err)
		p.mu.Lock()
		if p.policy != nil {
			noEffect = p.policy.noEffect
		}
		p.mu.Unlock()
	}()
	ctx = p.ctx
	if err = p.selectOriginalRequester(ctx, s); err != nil {
		return out, false, err
	}
	if err = p.acquire(ctx); err != nil {
		return out, false, err
	}
	m := &storagePolicyOperation{phase: p, caller: caller}
	p.policy = m
	if err = m.acquireControl(ctx); err != nil {
		return out, false, err
	}
	m.original, err = p.c.owner.readOriginalPolicy(ctx)
	if err != nil {
		return out, false, err
	}
	if m.original.Packed != nil && m.original.Packed.Phase == "prepared" {
		return out, false, errStorageEnrollmentUnresolved
	}
	if err = p.qualifyOriginalNative(ctx); err != nil {
		return out, false, err
	}
	if err = m.build(ctx, s, edit); err != nil {
		return out, false, err
	}
	if err = m.publish(ctx, 0, m.prepared); err != nil {
		return out, false, err
	}
	if err = m.qStep(ctx, 1); err != nil {
		return out, false, err
	}
	if err = m.qStep(ctx, 2); err != nil {
		return out, false, err
	}
	if err = m.publish(ctx, 3, m.active); err != nil {
		return out, false, err
	}
	if err = m.qStep(ctx, 4); err != nil {
		return out, false, err
	}
	if err = m.install(ctx, false); err != nil {
		return out, false, err
	}
	m.finished = true
	p.reconciled = true
	out = edit.Expected
	out.Sequence = storageEnrollmentPeakU64(m.active[16:24])
	out.Revision = m.transition.Original.Revision + 3
	out.Epoch = m.transition.Original.Epoch + 1
	out.Policy = m.transition.After
	for _, row := range m.original.Effective.Workspaces {
		if row.ID == out.Workspace.ID {
			out.Workspace = row
		}
	}
	if m.transition.Workspace == out.Workspace.ID {
		out.Workspace.Limits = m.transition.AfterLimits
	}
	return out, false, p.held(ctx)
}

func (m *storagePolicyOperation) acquireControl(ctx context.Context) error {
	if m == nil || m.phase == nil || m.phase.policy != m {
		return storagebudget.ErrAuthority
	}
	if err := m.phase.held(ctx); err != nil {
		return err
	}
	n, err := storageOpenOriginalControl(m.phase.c.ref.cell, storageBootstrapName)
	if err != nil {
		return err
	}
	// The epoch owns this actual Close immediately, including lock refusal.
	m.phase.sourceFiles = append(m.phase.sourceFiles, n)
	if err = lockPrivateData(n.file, true); err != nil {
		return errors.Join(storagebudget.ErrBusy, err)
	}
	return errors.Join(n.current(), m.phase.held(ctx))
}

func (m *storagePolicyOperation) build(ctx context.Context, s *Server, edit storagePolicyEdit) error {
	p := m.phase
	o := p.c.owner
	state := m.original
	q, err := o.ref.cell.q.InspectAction(ctx)
	if err != nil {
		return err
	}
	if err = storagePolicyCompletedQ(state, q); err != nil {
		return err
	}
	workspace, err := p.c.storageStatusWorkspace(s)
	if err != nil {
		return err
	}
	var member storagebudget.RegisteredWorkspace
	for _, row := range state.Effective.Workspaces {
		if row.ID == workspace {
			member = row
		}
	}
	if member.ID == (storagebudget.ID{}) || edit.Expected.Installation != o.ref.cell.record.Installation || edit.Expected.Workspace.ID != member.ID || edit.Expected.Workspace.Generation != member.Generation {
		return storagebudget.ErrAuthority
	}
	if edit.Expected.Sequence != storageEnrollmentPeakU64(state.Actual[state.Selected][16:24]) || edit.Expected.Revision != q.View.Configuration.Revision || edit.Expected.Epoch != q.View.Configuration.Epoch || edit.Expected.Policy != q.View.Configuration.Policy || edit.Expected.Workspace.Limits != member.Limits {
		return storagebudget.ErrStale
	}
	if err = m.bindingCurrent(state.Effective); err != nil {
		return err
	}
	after := q.View.Configuration.Policy
	if edit.Installation != nil {
		after.Installation = *edit.Installation
	}
	if edit.Default != nil {
		after.Workspace = *edit.Default
	}
	if err = storageTransferBootstrapPolicyRoom(after); err != nil {
		return err
	}
	id := storagebudget.ID{}
	generation := uint64(0)
	limits := storagebudget.Limits{}
	if edit.Workspace != nil && *edit.Workspace != member.Limits {
		id, generation, limits = member.ID, member.Generation, *edit.Workspace
	}
	m.transition, err = o.ref.cell.q.CapturePolicyTransition(ctx, edit.Expected.Revision, after, id, generation, limits)
	if err != nil {
		return err
	}
	// This second strict load is joined byte-for-byte to the capture before it
	// supplies closure operands. CheckPolicyTransition/Begin recheck the full
	// commitment under Q; no outside callback or native work runs under Q.
	q, err = o.ref.cell.q.InspectAction(ctx)
	if err != nil {
		return err
	}
	original := m.transition.Original
	if q.CheckpointBank != original.CheckpointBank || storageEnrollmentPeakHash("desk-policy-v6/selected-checkpoint", q.Checkpoints[q.CheckpointBank]) != original.Checkpoint || storageEnrollmentPeakHash("desk-policy-v6/tickets-a", q.Tickets[0]) != original.Tickets[0] || storageEnrollmentPeakHash("desk-policy-v6/tickets-b", q.Tickets[1]) != original.Tickets[1] {
		return storagebudget.ErrBusy
	}
	_, defs, err := state.Catalog.definitions()
	if err != nil {
		return err
	}
	rows, err := storageOwnerV6DecodeSparse(state.Overrides, defs)
	if err != nil {
		return err
	}
	if id != (storagebudget.ID{}) {
		rows, err = storageOwnerV6CanonicalSet(defs, rows, id, generation, limits)
		if err != nil {
			return err
		}
	}
	encoded, err := storageOwnerV6EncodeSparse(rows, defs)
	if err != nil {
		return err
	}
	effective, err := storageOwnerV6Expand(defs, storageOwnerV6HostPolicy(after), rows)
	if err != nil {
		return err
	}
	inverse, err := storagebudget.EncodePolicyTransition(m.transition)
	if err != nil {
		return err
	}
	transition, err := storagebudget.NewID()
	if err != nil {
		return err
	}
	sequence := storageEnrollmentPeakU64(state.Actual[state.Selected][16:24])
	if sequence > ^uint64(0)-2 {
		return storagebudget.ErrBounds
	}
	prepared := storageOwnerV6PackedCatalog{Version: 6, Encoding: "packed-binary-1", Sequence: sequence + 1, TransitionKind: storageOwnerV6PolicyTransition, Phase: "prepared", Transition: transition, Source: append([]byte{}, state.Source...), EffectivePolicy: effective.Policy, Overrides: encoded, PolicyTransition: inverse}
	copy(prepared.PreviousCatalogDigest[:], state.Actual[state.Selected][32:64])
	prepared.BeforeEffectiveDigest, err = storageOwnerV6EffectiveDigest(state.Effective)
	if err != nil {
		return err
	}
	prepared.AfterEffectiveDigest, err = storageOwnerV6EffectiveDigest(effective)
	if err != nil {
		return err
	}
	record, claim := *o.ref.cell.ownerRecord, o.ref.cell.ownerClaim
	m.prepared, err = storageOwnerV6PackedEncode(prepared, state.Evidence.Raw, record, claim)
	if err != nil {
		return err
	}
	final := prepared
	final.Sequence++
	final.Phase = "active"
	copy(final.PreviousCatalogDigest[:], m.prepared[32:64])
	m.active, err = storageOwnerV6PackedEncode(final, state.Evidence.Raw, record, claim)
	if err != nil {
		return err
	}
	aborted := final
	aborted.Phase = "aborted"
	aborted.EffectivePolicy = state.Policy
	aborted.Overrides = append([]byte{}, state.Overrides...)
	m.aborted, err = storageOwnerV6PackedEncode(aborted, state.Evidence.Raw, record, claim)
	if err != nil {
		return err
	}
	unfenced := aborted
	unfenced.Phase = "aborted-unfenced"
	m.unfenced, err = storageOwnerV6PackedEncode(unfenced, state.Evidence.Raw, record, claim)
	if err != nil {
		return err
	}
	for _, v := range []storageOwnerV6PackedCatalog{prepared, final, aborted, unfenced} {
		if _, err = storageOwnerV6PackedClosureSize(v, state.Catalog, state.Lineage); err != nil {
			return err
		}
	}
	if err = o.withOriginalIntentTable(ctx, func(ctx context.Context, table *storageIntentTable) error {
		input, err := o.originalPolicyClosureInput(ctx, q, table, storagebudget.ID{}, storagebudget.Request{}, nil)
		if err != nil {
			return err
		}
		input.Sets = append(input.Sets, storagePolicyEffectiveSet{Policy: effective.Policy, Workspaces: effective.Workspaces})
		_, err = storagePolicyObligationClosure(input)
		return err
	}); err != nil {
		return err
	}
	m.bank = 1 - state.Selected
	total := storagebudget.CapacityVector{}
	for _, effect := range storagePolicyEffects {
		d, err := storageAdvisoryEstimate(effect.bytes, effect.files, effect.directories, effect.metadata)
		if err != nil {
			return err
		}
		total.Bytes += d.Bytes
		total.Inodes += d.Inodes
	}
	if err = m.room(ctx, total); err != nil {
		return err
	}
	return o.ref.cell.q.CheckPolicyTransition(ctx, m.transition, storagebudget.PolicyQ0)
}

func (m *storagePolicyOperation) bindingCurrent(before storageOwnerDefinitions) error {
	o := m.phase.c.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closing || o.closed || o.transitionRequired {
		return os.ErrClosed
	}
	if len(o.workspaces) != len(before.Workspaces) {
		return errStorageOwnerTransitionRequired
	}
	for _, row := range before.Workspaces {
		old, ok := o.workspaces[row.ID]
		if !ok || old.ID != row.ID || old.Generation != row.Generation || old.Limits != row.Limits {
			return errStorageOwnerTransitionRequired
		}
	}
	return nil
}

// Only the registered original policy invocation can replace effective limits.
// Existing usage/known bits and every root/filesystem/constraint remain exact;
// ordinary binding-image installation retains its stricter unchanged rule.
func (m *storagePolicyOperation) install(ctx context.Context, abort bool) error {
	if err := m.nativeCurrent(ctx); err != nil {
		return err
	}
	phase := storagebudget.PolicyQ3
	if abort {
		phase = storagebudget.PolicyAborted
	}
	if err := m.phase.c.ref.cell.q.CheckPolicyTransition(ctx, m.transition, phase); err != nil {
		return err
	}
	o := m.phase.c.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	g := &o.admission
	g.mu.Lock()
	defer g.mu.Unlock()
	if o.closing || o.closed || g.enrollment != m.phase || g.generation != m.phase.generation || g.phase != storageAdmissionEnrollment {
		return os.ErrClosed
	}
	if m.transition.Workspace != (storagebudget.ID{}) && !abort {
		old, ok := o.workspaces[m.transition.Workspace]
		if !ok || old.Generation != m.transition.WorkspaceGeneration || old.Limits != m.transition.BeforeLimits && (!m.phase.startup || old.Limits != m.transition.AfterLimits) {
			return errStorageOwnerTransitionRequired
		}
		old.Limits = m.transition.AfterLimits
		o.workspaces[old.ID] = old
	}
	return nil
}

// Expansion changes logical admission values only. Existing source rows remain
// byte-exact, while a new row captures the currently effective future default.
// The caller's existing catalog/source relation still validates physical rows.
func storagePolicyEffectiveCarry(before, after storageOwnerCatalogV2, policy storageOwnerPolicy, sparse []byte) (storageOwnerDefinitions, error) {
	if before.Installation != after.Installation || before.QBinding != after.QBinding || before.Policy != after.Policy {
		return storageOwnerDefinitions{}, storagebudget.ErrAuthority
	}
	_, old, err := before.definitions()
	if err != nil {
		return storageOwnerDefinitions{}, err
	}
	_, next, err := after.definitions()
	if err != nil {
		return storageOwnerDefinitions{}, err
	}
	rows, err := storageOwnerV6DecodeSparse(sparse, old)
	if err != nil {
		return storageOwnerDefinitions{}, err
	}
	retained := map[storagebudget.ID]bool{}
	for _, w := range before.Workspaces {
		found := false
		for _, v := range after.Workspaces {
			if v.Definition.ID == w.Definition.ID {
				if found || !reflect.DeepEqual(w, v) {
					return storageOwnerDefinitions{}, storagebudget.ErrAuthority
				}
				found = true
			}
		}
		if !found {
			return storageOwnerDefinitions{}, storagebudget.ErrAuthority
		}
		retained[w.Definition.ID] = true
	}
	for _, w := range after.Workspaces {
		if !retained[w.Definition.ID] && w.Definition.Limits != policy.Workspace {
			return storageOwnerDefinitions{}, storagebudget.ErrBounds
		}
	}
	return storageOwnerV6Expand(next, policy, rows)
}

func (c *storageBudgetConstruction) effectiveOwnerDefinitions(ctx context.Context, cat storageOwnerCatalogV2) (storageOwnerDefinitions, error) {
	_, defs, err := cat.definitions()
	if err != nil {
		return defs, err
	}
	if c == nil || c.owner == nil || c.ref == nil {
		return defs, storagebudget.ErrAuthority
	}
	active, err := c.owner.policyReserveClosureActive(ctx)
	if err != nil {
		return defs, err
	}
	if !active {
		return defs, nil
	}
	c.owner.admission.mu.Lock()
	epoch := c.owner.admission.enrollment
	c.owner.admission.mu.Unlock()
	if epoch != nil && epoch.self == epoch && epoch.scope != nil && epoch.original != nil && storagePolicyActualPair(epoch.original.Catalogs) {
		if err = epoch.held(ctx); err != nil {
			return defs, err
		}
		prior := epoch.previousEvidence
		if prior == nil {
			// During restart the current packet independently retains both actual
			// before banks and its own historical effective set. Its Raw is the
			// successor's evidence, never a substitute for lost prior Raw bytes.
			packet := epoch.initialPacket
			if packet == nil || packet.Version != 3 {
				return defs, errStorageEnrollmentUnresolved
			}
			actual, _, e := storagePolicyEvidenceNativeBefore(*packet, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim)
			if e != nil || !bytes.Equal(actual[0], epoch.original.Catalogs[0]) || !bytes.Equal(actual[1], epoch.original.Catalogs[1]) {
				return defs, errors.Join(errStorageCustody, e)
			}
			for _, projection := range packet.Projections {
				if storageEnrollmentPeakProjectionFullDigest(projection) != packet.Before {
					continue
				}
				set := storagePolicyProjectionSet(projection)
				_, source, e := projection.Catalog.definitions()
				if e != nil || len(set.Workspaces) != len(source.Workspaces) {
					return defs, errors.Join(errStorageCustody, e)
				}
				rows := []storageOwnerV6Override{}
				for _, row := range set.Workspaces {
					rows, e = storageOwnerV6CanonicalSet(source, rows, row.ID, row.Generation, row.Limits)
					if e != nil {
						return defs, e
					}
				}
				sparse, e := storageOwnerV6EncodeSparse(rows, source)
				if e != nil {
					return defs, e
				}
				return storagePolicyEffectiveCarry(projection.Catalog, cat, set.Policy, sparse)
			}
			return defs, errStorageEnrollmentUnresolved
		}
		before := prior.Inner.Catalog
		selected := byte(0)
		if storageEnrollmentPeakU64(epoch.original.Catalogs[1][16:24]) > storageEnrollmentPeakU64(epoch.original.Catalogs[0][16:24]) {
			selected = 1
		}
		_, _, _, policy, sparse, e := storageOwnerV6CaptureBefore(epoch.original.Catalogs, selected, prior.Raw, before, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim)
		if e != nil {
			return defs, e
		}
		return storagePolicyEffectiveCarry(before, cat, policy, sparse)
	}
	state, err := c.owner.readOriginalPolicy(ctx)
	if err != nil {
		return defs, err
	}
	policy, sparse := state.Policy, state.Overrides
	if state.Packed != nil && state.Packed.Phase == "prepared" {
		c.owner.admission.mu.Lock()
		p := c.owner.admission.enrollment
		c.owner.admission.mu.Unlock()
		if p == nil || p.c != c || p.policy == nil || p.policy.phase != p {
			return defs, errStorageEnrollmentUnresolved
		}
		m := p.policy
		recorded, err := storagebudget.DecodePolicyTransition(state.Packed.PolicyTransition)
		if err != nil || recorded != m.transition {
			return defs, errors.Join(errStorageCustody, err)
		}
		phase, err := m.currentPhase(ctx)
		if err != nil {
			return defs, err
		}
		if phase == storagebudget.PolicyQ0 || phase == storagebudget.PolicyQ1 || phase == storagebudget.PolicyAborted {
			policy = storageOwnerV6HostPolicy(recorded.Before)
			_, source, err := state.Catalog.definitions()
			if err != nil {
				return defs, err
			}
			rows, err := storageOwnerV6DecodeSparse(sparse, source)
			if err != nil {
				return defs, err
			}
			if recorded.Workspace != (storagebudget.ID{}) {
				rows, err = storageOwnerV6CanonicalSet(source, rows, recorded.Workspace, recorded.WorkspaceGeneration, recorded.BeforeLimits)
				if err != nil {
					return defs, err
				}
			}
			sparse, err = storageOwnerV6EncodeSparse(rows, source)
			if err != nil {
				return defs, err
			}
		}
	}
	return storagePolicyEffectiveCarry(state.Catalog, cat, policy, sparse)
}

func (m *storagePolicyOperation) currentPhase(ctx context.Context) (storagebudget.PolicyPhase, error) {
	if m == nil || m.phase == nil || m.phase.policy != m {
		return 0, storagebudget.ErrAuthority
	}
	if err := m.phase.permit(ctx); err != nil {
		return 0, err
	}
	for _, phase := range []storagebudget.PolicyPhase{storagebudget.PolicyQ0, storagebudget.PolicyQ1, storagebudget.PolicyQ2, storagebudget.PolicyQ3, storagebudget.PolicyAborted} {
		err := m.phase.c.ref.cell.q.CheckPolicyTransition(ctx, m.transition, phase)
		if err == nil {
			return phase, nil
		}
		// No joined/persistence/cancellation/unknown result is a phase mismatch.
		if err != storagebudget.ErrStale {
			return 0, err
		}
	}
	return 0, errStorageEnrollmentUnresolved
}

// Reconstruct the one retained policy operation from its actual strict pair.
// This restores comparison bytes, not spent permissions: the original startup
// epoch, held sources and fresh qualification separately authorize each suffix.
func (m *storagePolicyOperation) reconstruct(ctx context.Context) error {
	state := m.original
	if state.Packed == nil || state.Packed.TransitionKind != storageOwnerV6PolicyTransition {
		return errStorageEnrollmentUnresolved
	}
	prepared := *state.Packed
	m.bank = state.Selected
	if prepared.Phase != "prepared" {
		m.bank = 1 - state.Selected
		var err error
		prepared, err = storageOwnerV6PackedDecode(state.Actual[m.bank], state.Evidence.Raw, *m.phase.c.ref.cell.ownerRecord, m.phase.c.ref.cell.ownerClaim)
		if err != nil || prepared.Phase != "prepared" {
			return errors.Join(errStorageEnrollmentUnresolved, err)
		}
	}
	var err error
	m.transition, err = storagebudget.DecodePolicyTransition(prepared.PolicyTransition)
	if err != nil {
		return err
	}
	m.prepared = append([]byte{}, state.Actual[m.bank]...)
	_, defs, err := state.Catalog.definitions()
	if err != nil {
		return err
	}
	rows, err := storageOwnerV6DecodeSparse(prepared.Overrides, defs)
	if err != nil {
		return err
	}
	if m.transition.Workspace != (storagebudget.ID{}) {
		rows, err = storageOwnerV6CanonicalSet(defs, rows, m.transition.Workspace, m.transition.WorkspaceGeneration, m.transition.BeforeLimits)
		if err != nil {
			return err
		}
	}
	m.original.Policy = storageOwnerV6HostPolicy(m.transition.Before)
	m.original.Overrides, err = storageOwnerV6EncodeSparse(rows, defs)
	if err != nil {
		return err
	}
	m.original.Effective, err = storageOwnerV6Expand(defs, m.original.Policy, rows)
	if err != nil {
		return err
	}
	final := prepared
	final.Sequence++
	final.Phase = "active"
	copy(final.PreviousCatalogDigest[:], m.prepared[32:64])
	record, claim := *m.phase.c.ref.cell.ownerRecord, m.phase.c.ref.cell.ownerClaim
	m.active, err = storageOwnerV6PackedEncode(final, state.Evidence.Raw, record, claim)
	if err != nil {
		return err
	}
	final.Phase = "aborted"
	final.EffectivePolicy = m.original.Policy
	final.Overrides = append([]byte{}, m.original.Overrides...)
	m.aborted, err = storageOwnerV6PackedEncode(final, state.Evidence.Raw, record, claim)
	if err != nil {
		return err
	}
	final.Phase = "aborted-unfenced"
	m.unfenced, err = storageOwnerV6PackedEncode(final, state.Evidence.Raw, record, claim)
	if err != nil {
		return err
	}
	m.started = true
	return m.phase.held(ctx)
}

// A stale healthy before may be metadata-aborted without a Q effect. Keep the
// original epoch and exact old complete definitions. Replayed terminals and a
// changed maintenance epoch cannot masquerade as this no-fence case.
func (m *storagePolicyOperation) noFenceBefore(ctx context.Context) error {
	q, err := m.phase.c.ref.cell.q.InspectAction(ctx)
	if err != nil {
		return err
	}
	if q.View.Configuration.Maintenance || q.View.Configuration.Epoch != m.transition.Original.Epoch || q.View.Configuration.Revision < m.transition.Original.Revision || !reflect.DeepEqual(storageOwnerDefinitionSnapshot(q.View), m.original.Effective) {
		return errStorageEnrollmentUnresolved
	}
	for _, row := range q.View.Slots {
		if row.Ticket.State == storagebudget.Terminal && row.Ticket.Token != row.PersistedApplied {
			return errStorageEnrollmentUnresolved
		}
	}
	return m.phase.held(ctx)
}

// Select the observed suffix once. A failed/unknown dispatch returns directly;
// it is never used as a reason to select another phase or replay that effect.
func (m *storagePolicyOperation) recover(ctx context.Context, abort bool) error {
	state := m.original
	if err := m.reconstruct(ctx); err != nil {
		return err
	}
	phase, err := m.currentPhase(ctx)
	if err == errStorageEnrollmentUnresolved && state.Packed.Phase != "prepared" {
		q, e := m.phase.c.ref.cell.q.InspectAction(ctx)
		if e != nil {
			return e
		}
		if e = storagePolicyCompletedQ(state, q); e != nil {
			return e
		}
		m.consumed[0], m.consumed[1], m.consumed[2] = true, true, true
		if e = m.persistCatalog(ctx, 3, state.Actual[state.Selected]); e != nil {
			return e
		}
		// Keep the selected final state for the full effective/current epoch join.
		m.original = state
		if e = m.persistCompletedQ(ctx, q); e != nil {
			return e
		}
		m.finished, m.phase.reconciled = true, true
		return nil
	}
	if err != nil {
		if err != errStorageEnrollmentUnresolved || state.Packed.Phase == "active" || state.Packed.Phase == "aborted" {
			return err
		}
		if err = m.noFenceBefore(ctx); err != nil {
			return err
		}
		m.consumed[0] = true // actual Prepared was strictly decoded, not reissued
		if state.Packed.Phase == "prepared" {
			if err = m.publish(ctx, 3, m.unfenced); err != nil {
				return err
			}
		} else {
			if err = m.persistCatalog(ctx, 3, m.unfenced); err != nil {
				return err
			}
		}
		if err = m.noFenceBefore(ctx); err != nil {
			return err
		}
		m.finished = true
		m.phase.reconciled = true
		return nil
	}
	if state.Packed.Phase == "aborted-unfenced" {
		if phase != storagebudget.PolicyQ0 {
			return errStorageEnrollmentUnresolved
		}
		if err = m.noFenceBefore(ctx); err != nil {
			return err
		}
		m.consumed[0] = true
		if err = m.persistCatalog(ctx, 3, m.unfenced); err != nil {
			return err
		}
		m.finished, m.phase.reconciled = true, true
		return nil
	}
	if state.Packed.Phase == "aborted" {
		if phase != storagebudget.PolicyQ1 && phase != storagebudget.PolicyAborted {
			return errStorageEnrollmentUnresolved
		}
		abort = true
	} else if state.Packed.Phase == "active" {
		if phase != storagebudget.PolicyQ2 && phase != storagebudget.PolicyQ3 {
			return errStorageEnrollmentUnresolved
		}
		abort = false
	} else if phase == storagebudget.PolicyQ3 || phase == storagebudget.PolicyAborted {
		return errStorageEnrollmentUnresolved
	}
	if phase == storagebudget.PolicyQ2 || phase == storagebudget.PolicyQ3 {
		abort = false
	}
	// Exact durable Q1/Q2/Q3 implies the earlier original Prepared dispatch;
	// persistence of a surviving page finishes its group without WriteAt replay.
	if state.Packed.Phase == "prepared" {
		if err = m.persistCatalog(ctx, 0, m.prepared); err != nil {
			return err
		}
	} else {
		m.consumed[0] = true
	}
	switch phase {
	case storagebudget.PolicyQ0:
		if abort {
			if state.Packed.Phase == "prepared" {
				if err = m.publish(ctx, 3, m.unfenced); err != nil {
					return err
				}
			} else {
				if err = m.persistCatalog(ctx, 3, m.unfenced); err != nil {
					return err
				}
			}
			if err = m.phase.c.ref.cell.q.CheckPolicyTransition(ctx, m.transition, storagebudget.PolicyQ0); err != nil {
				return err
			}
			m.finished = true
			m.phase.reconciled = true
			return nil
		}
		if err = m.qStep(ctx, 1); err != nil {
			return err
		}
		if err = m.qStep(ctx, 2); err != nil {
			return err
		}
	case storagebudget.PolicyQ1:
		if err = m.persistQ(ctx, 1, phase); err != nil {
			return err
		}
		if !abort {
			if err = m.qStep(ctx, 2); err != nil {
				return err
			}
		}
	case storagebudget.PolicyQ2:
		m.consumed[1] = true
		if err = m.persistQ(ctx, 2, phase); err != nil {
			return err
		}
	case storagebudget.PolicyQ3, storagebudget.PolicyAborted:
		m.consumed[1], m.consumed[2], m.consumed[3] = true, true, true
		if err = m.persistQ(ctx, 4, phase); err != nil {
			return err
		}
		if err = m.install(ctx, phase == storagebudget.PolicyAborted); err != nil {
			return err
		}
		m.finished = true
		m.phase.reconciled = true
		return nil
	}
	final := m.active
	if abort {
		final = m.aborted
	}
	if state.Packed.Phase == "prepared" {
		err = m.publish(ctx, 3, final)
	} else {
		err = m.persistCatalog(ctx, 3, final)
	}
	if err != nil {
		return err
	}
	if abort {
		if err = m.take(ctx, 4); err != nil {
			return err
		}
		err = m.phase.c.ref.cell.q.AbortPolicyTransition(ctx, m.transition)
		if err = m.phase.leaveNative(err); err != nil {
			return err
		}
	} else if err = m.qStep(ctx, 4); err != nil {
		return err
	}
	if err = m.install(ctx, abort); err != nil {
		return err
	}
	m.finished = true
	m.phase.reconciled = true
	return nil
}

// A completed original source fixes the one explained maintenance epoch. Later
// ordinary reservations may advance revision and reuse ticket slots, but neither
// generic maintenance nor Repair can be hidden by a subsequent policy edit.
// The unfenced final distinguishes the otherwise byte-identical healthy aborts.
func storagePolicyCompletedQ(state storagePolicyOriginal, q storagebudget.EnrollmentInspection) error {
	if state.RecoveryOnly {
		return errStorageEnrollmentUnresolved
	}
	return storagePolicyQAtBoundary(state.Effective, state.Boundary, q)
}

func storagePolicyQAtBoundary(effective storageOwnerDefinitions, boundary storagePolicyQBoundary, q storagebudget.EnrollmentInspection) error {
	v := q.View.Configuration
	if v.Maintenance || !reflect.DeepEqual(storageOwnerDefinitionSnapshot(q.View), effective) {
		return errStorageOwnerTransitionRequired
	}
	for _, slot := range q.View.Slots {
		if slot.Ticket.State == storagebudget.Terminal && slot.Ticket.Token != slot.PersistedApplied {
			return errStorageEnrollmentUnresolved
		}
	}
	if !boundary.valid() || v.Epoch != boundary.Epoch || v.Revision < boundary.MinimumRevision {
		return errStorageEnrollmentUnresolved
	}
	return nil
}

func storagePolicyActualPair(raw [2][]byte) bool {
	return len(raw[0]) == storageOwnerCatalogBytes && bytes.Equal(raw[0][:8], storageOwnerV6Magic[:]) || len(raw[1]) == storageOwnerCatalogBytes && bytes.Equal(raw[1][:8], storageOwnerV6Magic[:])
}

// This returns only strict comparison operands from the epoch's already captured
// ACTUAL originals and the immutable current packet. Physical currentness and
// every native publication remain with the original epoch and actual files.
func (p *storageEnrollmentEpoch) originalCorePair(raw [2][]byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([2][]byte, int, error) {
	if !storagePolicyActualPair(raw) {
		return storageEnrollmentOriginalCorePair(raw, record, claim)
	}
	if p == nil || p.self != p || p.c == nil || p.original == nil || len(raw[0]) != storageOwnerCatalogBytes || len(raw[1]) != storageOwnerCatalogBytes {
		return [2][]byte{}, 0, storagebudget.ErrAuthority
	}
	p.c.ref.cell.mu.Lock()
	endpoint, catalog := p.c.ref.cell.enrollmentEndpoint, p.c.ref.cell.ownerCatalog
	p.c.ref.cell.mu.Unlock()
	if endpoint == nil {
		return [2][]byte{}, 0, errStorageEnrollmentUnresolved
	}
	pair, err := storageOwnerV6NativeReadPair(raw[0], raw[1], &catalog, endpoint.Raw, record, claim)
	if err == nil {
		return [2][]byte{pair.Banks[0].Stage.Core, pair.Banks[1].Stage.Core}, int(pair.Selected), nil
	}
	selected := byte(0)
	if storageEnrollmentPeakU64(raw[1][16:24]) > storageEnrollmentPeakU64(raw[0][16:24]) {
		selected = 1
	}
	_, core, _, _, _, e := storageOwnerV6CaptureBefore(raw, selected, endpoint.Raw, catalog, record, claim)
	if e != nil {
		return [2][]byte{}, 0, errors.Join(err, e)
	}
	return [2][]byte{core, append([]byte{}, core...)}, int(selected), nil
}

// Completed policy/native endpoints keep the original startup retain, home EX,
// use/project/work/domain ordering and one registered epoch. Discovery starts
// from exact immutable admission baselines; current effective rows are joined
// separately. No GET, New-time enrollment or synthetic project is introduced.
func (c *storageBudgetConstruction) resumeOriginalPolicyStartup(ctx context.Context, record storageOwnerRecordV2, claim storageOwnerClaimV2) (err error) {
	if c.owner != nil || c.sources != nil || !c.home.exclusive || !storagePolicyActualPair(c.startupPhysical) {
		return storagebudget.ErrAuthority
	}
	callerCtx := ctx
	selected := byte(0)
	if storageEnrollmentPeakU64(c.startupPhysical[1][16:24]) > storageEnrollmentPeakU64(c.startupPhysical[0][16:24]) {
		selected = 1
	}
	payload, _, err := storageOwnerV6Payload(c.startupPhysical[selected])
	if err != nil {
		return err
	}
	if bytes.HasPrefix(payload, storageOwnerV6NativeMagic[:]) {
		v, e := storageOwnerV6NativeDecode(c.startupPhysical[selected], record, claim)
		if e != nil {
			return e
		}
		outer, e := storageEnrollmentPeakDecodeEnvelopeNative(v.Stage.Core, record, claim, true, true, true)
		if e != nil {
			return e
		}
		if v.Stage.Transition == storageCompositionEnrollment && (outer.Marker == nil || outer.Marker.Phase != 3) {
			return c.resumeOriginalNativeSourceCut(ctx, v, outer, record, claim)
		}
	}
	owner, err := newStorageBudgetOwner(c.ref, &storageAdvisoryObserver{}, storageBudgetOptions{})
	if err != nil {
		return err
	}
	if err = c.installAdmissionOwner(ctx, owner); err != nil {
		return errors.Join(err, owner.Close())
	}
	p, err := c.beginStartupEnrollment(ctx)
	if err != nil {
		return err
	}
	defer func() { err = p.finish(err) }()
	ctx = p.ctx
	if err = p.acquire(ctx); err != nil {
		return err
	}
	p.sourceHoldsComplete = false
	state, err := owner.readOriginalPolicy(ctx)
	if err != nil {
		return err
	}
	if !bytes.Equal(state.Actual[0], c.startupPhysical[0]) || !bytes.Equal(state.Actual[1], c.startupPhysical[1]) {
		return errStorageCustody
	}
	// The strict selected lineage of these exact banks names each enrolled
	// project's accounting key, as the native enrollment resume records it. A
	// moved project's identity file carries a newer ID; without the lineage
	// the start would key discovery by that ID and refuse the scope row.
	lineage := state.Lineage
	c.startupLineage = &lineage
	m := &storagePolicyOperation{phase: p, original: state}
	p.policy = m
	if state.Packed != nil {
		if err = m.reconstruct(ctx); err != nil {
			return err
		}
	}
	// Only immutable native-bound source rows provide discovery baselines.
	c.ref.cell.mu.Lock()
	c.ref.cell.ownerCatalog, c.ref.cell.enrollmentEndpoint = state.Catalog, &state.Evidence
	c.ref.cell.mu.Unlock()
	if err = c.selectMoveHistory(ctx, state); err != nil {
		return err
	}
	x, err := c.inspectOriginalSources(ctx, record.Installation, nil)
	if err != nil {
		return err
	}
	c.sources = x
	if err = x.buildObjects(ctx); err != nil {
		return err
	}
	if err = x.observeCatalogLegacyHistoryControl(ctx, state.Catalog); err != nil {
		return err
	}
	if err = p.acquireStartupSourceDescriptions(ctx, state.Catalog); err != nil {
		return err
	}
	if err = p.compareOriginalObjectDescriptions(ctx, state.Catalog); err != nil {
		return err
	}
	if err = x.restoreCreationDictionary(state.Catalog, nil); err != nil {
		return err
	}
	if err = c.attachOwnerBindings(ctx, state.Catalog, x); err != nil {
		return err
	}
	if err = p.completeStartupSourceHolds(ctx); err != nil {
		return err
	}
	a := &storageAdvisoryObserver{owner: owner, catalog: state.Catalog, roots: map[storagebudget.ID]storageOwnerRoot{}, filesystems: map[storagebudget.ID]storageOwnerFilesystem{}, constraints: map[storagebudget.ID]storageOwnerConstraint{}, maxAge: time.Duration(state.Catalog.Policy.ObservationMaxAgeNanos)}
	for _, row := range state.Catalog.Roots {
		a.roots[row.Definition.ID] = row
	}
	for _, row := range state.Catalog.Filesystems {
		a.filesystems[row.Definition.ID] = row
	}
	for _, row := range state.Catalog.Constraints {
		a.constraints[row.Definition.ID] = row
	}
	for _, root := range owner.orderedRoots() {
		if err = root.current(ctx); err != nil {
			return err
		}
		sample, e := a.query(ctx, root, root.root)
		if e != nil {
			return e
		}
		if sample.Filesystem != owner.filesystems[root.row.Filesystem] || sample.RootGeneration != root.row.Generation || sample.CapacityCount != root.row.CapacityCount || sample.CapacityRefs != root.row.CapacityRefs {
			return errStorageOwnerTransitionRequired
		}
		for _, row := range sample.Constraints {
			if row != owner.constraints[row.ID] {
				return errStorageOwnerTransitionRequired
			}
		}
	}
	owner.observer = a
	if err = p.qualifyOriginalNative(ctx); err != nil {
		return err
	}
	if err = m.acquireControl(ctx); err != nil {
		return err
	}
	if state.Packed != nil {
		if err = m.recover(ctx, true); err != nil {
			return err
		}
		// Recovery may publish the original final outcome. Reload its actual
		// bytes; a Prepared operand has neither its completed values nor tuple.
		state, err = owner.readOriginalPolicy(ctx)
		if err != nil {
			return err
		}
	} else {
		q, e := c.ref.cell.q.InspectAction(ctx)
		if e != nil {
			return e
		}
		if err = c.moveStartupQ(ctx, state, q); err != nil {
			return err
		}
		p.reconciled = true
	}
	q, err := c.ref.cell.q.InspectAction(ctx)
	if err != nil {
		return err
	}
	if err = c.moveStartupQ(ctx, state, q); err != nil {
		return err
	}
	owner.mu.Lock()
	owner.censusEpoch = q.View.Configuration.Epoch
	owner.mu.Unlock()
	if err = p.finish(nil); err != nil {
		return err
	}
	ctx = callerCtx // finish cancelled only the completed epoch, not its caller.
	// The common outer startup owns typed move recovery after this epoch has
	// released every borrowed source hold. Defer ordinary identity demand only
	// for the exact already-durable typed intent; no second recovery call runs.
	moveIntent, moveFound, moveErr := readProjectMove(c.original.config)
	if moveErr != nil {
		return moveErr
	}
	if moveFound && moveIntent.Storage != nil {
		return c.current(ctx, true)
	}
	// Mandatory original P/J/native recovery precedes readiness. A Prepared
	// native source is not a completed policy snapshot or an ordinary permit.
	demand, err := c.originalIdentityDemand(ctx)
	if err != nil {
		return err
	}
	if demand {
		if err = c.completeOriginalIdentity(ctx); err != nil {
			return err
		}
	}
	// Decode the exact held policy pair before the short control window. The
	// bootstrap exclusion then revalidates both physical images around J/Q
	// validation; each slot still proves its own binding and currentness.
	if err = owner.withStartupMoveControl(ctx, func(ctx context.Context, table *storageIntentTable, original *storagePolicyOriginal) error {
		q, e := c.ref.cell.q.InspectHostState(ctx)
		if e != nil {
			return e
		}
		return owner.validateIntentMappingsOriginal(ctx, table, q, original)
	}); err != nil {
		return err
	}
	return c.current(ctx, true)
}

// Recovery-only native lineage admits only the original already-registered
// identity prefix, actual original pair and exact healthy boundary. It cannot
// activate policy snapshots, ordinary admission or a caller-selected source.
func (p *storageEnrollmentEpoch) originalPolicyCurrentQ(ctx context.Context, state storagePolicyOriginal, q storagebudget.EnrollmentInspection) error {
	if !state.RecoveryOnly {
		return storagePolicyCompletedQ(state, q)
	}
	if p == nil || p.self != p || !p.identityPrefixes || p.original == nil || !bytes.Equal(state.Actual[0], p.original.Catalogs[0]) || !bytes.Equal(state.Actual[1], p.original.Catalogs[1]) {
		return errStorageEnrollmentUnresolved
	}
	pair, err := storageOwnerV6NativeReadPair(state.Actual[0], state.Actual[1], &state.Catalog, state.Evidence.Raw, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if err == nil && storageMovePendingPair(pair) && p.move != nil && p.move.phase == p && p.move.self == p.move {
		return p.move.currentQ(ctx, state, q)
	}
	if err != nil || !storagePolicyNativePreparedLineage(pair) {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	if err = p.held(ctx); err != nil {
		return err
	}
	return storagePolicyQAtBoundary(state.Effective, state.Boundary, q)
}

func (p *storageEnrollmentEpoch) acquireIdentitySigning(ctx context.Context) error {
	if p.identityPrefixes || p.project == nil || p.use == nil {
		return storagebudget.ErrAuthority
	}
	var err error
	p.signing, err = p.c.assistant.openSigning(false)
	if errors.Is(err, errNoSigningDir) {
		p.signing = nil
		p.signingAbsent = true
		err = nil
	} else if err != nil {
		return err
	}
	if p.signing != nil {
		p.signingFile, err = p.signing.root.Open(".")
		if err != nil {
			return err
		}
		if err = lockSigningFile(p.signingFile); err != nil {
			return errors.Join(storagebudget.ErrBusy, err)
		}
		p.signingNative, err = storageAdvisoryPersistentHeld(ctx, p.signingFile)
		if err != nil {
			return err
		}
	}
	p.identityPrefixes = true
	return p.current(ctx)
}
