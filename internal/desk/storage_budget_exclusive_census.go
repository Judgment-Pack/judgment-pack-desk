package desk

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

type storageExclusiveCensusStage uint8

const (
	storageExclusiveCapturing storageExclusiveCensusStage = iota
	storageExclusiveMeasuring
	storageExclusiveReady
	storageExclusivePublishing
	storageExclusiveCompleted
	storageExclusiveFailed
	storageExclusiveClosing
)

// This exact original invocation owns the phase retain, including native work
// before/between/after capacity passes. Close revokes permission immediately;
// only the executor can signal actual drain. Neither a DTO nor censusMu is a
// cleanup lifetime. No action, identity producer or recovery route is enabled.
type storageExclusiveStartupCensus struct {
	self             *storageExclusiveStartupCensus
	mu               sync.Mutex
	c                *storageBudgetConstruction
	mutation         *storageMutationAuthority
	generation       uint64
	running, revoked bool
	stage            storageExclusiveCensusStage
	borrowers        uint64
	helper           *storageExclusiveCensusControl
	done             chan struct{}
	err              error
	snapshot         *storagebudget.ExclusiveCensusSnapshot
	view             storagebudget.HostInspection
	journal          [32]byte
	epoch            uint64
}

// A single package-test boundary can hold an actual invocation/owned handle.
// No callback executes, especially under Q; ordinary cancellation is not a
// promise that an in-flight native call or Close can be interrupted.
type storageExclusiveCensusFault struct {
	point   string
	entered chan struct{}
	release <-chan struct{}
	err     error
	once    sync.Once
	next    *storageExclusiveCensusFault
}

func (p *storageExclusiveStartupCensus) boundary(point string) error {
	return p.c.exclusiveBoundary(point)
}
func (c *storageBudgetConstruction) exclusiveBoundary(point string) error {
	for f := c.exclusiveFault; f != nil; f = f.next {
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

type storageExclusiveCensusControl struct {
	self               *storageExclusiveCensusControl
	phase              *storageExclusiveStartupCensus
	retained, qEntered bool // phase.mu
	err                error
}

func (o *storageBudgetOwner) exclusiveCensusPhase() *storageExclusiveStartupCensus {
	o.admission.mu.Lock()
	defer o.admission.mu.Unlock()
	return o.admission.exclusiveCensus
}

// Reuse the existing capacity-pass retain, but install it before its first
// out-of-lock original currentness/native observation. No observer can be in
// the pre-retain gap while a startup phase is minted at exact active1.
func (o *storageBudgetOwner) retainCapacityPassOutsideExclusive() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	g := &o.admission
	g.mu.Lock()
	defer g.mu.Unlock()
	if o.closing || o.closed {
		return os.ErrClosed
	}
	if o.transitionRequired {
		return errStorageOwnerTransitionRequired
	}
	if g.phase == storageAdmissionClosing {
		return os.ErrClosed
	}
	if g.exclusiveCensus != nil || g.enrollment != nil {
		return storagebudget.ErrAuthority
	}
	if g.phase == storageAdmissionStartup {
		a := g.startup
		if a == nil || a.construction == nil {
			return storagebudget.ErrAuthority
		}
		c := a.construction
		c.mu.Lock()
		// Generic Startup passes serve the original Maintenance census only.
		// Before holds publication there is nothing they may borrow. The
		// exclusive eligibility latch is set before that publication, so an
		// old pass cannot start currentness/owned cleanup on the healthy route.
		allowed := c.self == c && c.owner == o && c.mutation == a && !c.closing && !c.closed && !c.exclusiveCensusEligible && c.startupUse != nil
		c.mu.Unlock()
		if !allowed {
			return storagebudget.ErrAuthority
		}
	}
	if o.active == ^uint64(0) {
		return storagebudget.ErrBounds
	}
	o.active++
	return nil
}

// Select only the already-proved active/after-definitions/Maintenance=false
// route, before publishing any borrowed startup EX description. It shares the
// owner's short lock order with generic pass entry; it does no I/O or waiting.
func (c *storageBudgetConstruction) selectExclusiveCensus() error {
	o := c.owner
	g := &o.admission
	o.mu.Lock()
	g.mu.Lock()
	c.mu.Lock()
	defer o.mu.Unlock()
	defer g.mu.Unlock()
	defer c.mu.Unlock()
	if c.self != c || c.closing || c.closed || c.startupUse != nil || c.startupWork != nil || len(c.startupHolds) != 0 || c.exclusiveCensusEligible || c.exclusiveCensus != nil || o.closing || o.closed || o.transitionRequired || o.active != 1 || o.currentCensus != nil || g.phase != storageAdmissionStartup || g.startup != c.mutation || g.shared != 0 || g.preparation != nil || g.prefixes != 0 {
		return storagebudget.ErrAuthority
	}
	c.exclusiveCensusEligible = true
	return nil
}

func (c *storageBudgetConstruction) beginExclusiveCensus(ctx context.Context) (*storageExclusiveStartupCensus, error) {
	if c == nil || ctx == nil || ctx.Err() != nil {
		return nil, errors.Join(storagebudget.ErrAuthority, storageExclusiveContextErr(ctx))
	}
	o := c.owner
	if o == nil {
		return nil, storagebudget.ErrAuthority
	}
	// No native checks before the invocation pin is atomically installed.
	o.mu.Lock()
	g := &o.admission
	g.mu.Lock()
	c.mu.Lock()
	defer o.mu.Unlock()
	defer g.mu.Unlock()
	defer c.mu.Unlock()
	if c.self != c || c.closing || c.closed || !c.exclusiveCensusEligible || c.exclusiveCensus != nil || c.grants["anchor/census"] || o.currentCensus != nil || o.closing || o.closed || o.transitionRequired || o.active != 1 || g.phase != storageAdmissionStartup || g.startup != c.mutation || g.exclusiveCensus != nil || g.shared != 0 || g.preparation != nil || g.prefixes != 0 || c.mutation == nil || c.mutation.closed || !c.mutation.retained {
		return nil, storagebudget.ErrAuthority
	}
	p := &storageExclusiveStartupCensus{c: c, mutation: c.mutation, generation: g.generation, running: true, done: make(chan struct{})}
	p.self = p
	o.active++
	g.exclusiveCensus = p
	c.exclusiveCensus = p
	c.exclusiveCleanupDone = make(chan struct{})
	return p, nil
}

func storageExclusiveContextErr(ctx context.Context) error {
	if ctx == nil {
		return storagebudget.ErrAuthority
	}
	return ctx.Err()
}

// Local permission is separate from the retain. It performs no native work.
func (p *storageExclusiveStartupCensus) permit(ctx context.Context) error {
	if p == nil || p.self != p || p.c == nil {
		return storagebudget.ErrAuthority
	}
	if err := storageExclusiveContextErr(ctx); err != nil {
		return err
	}
	c := p.c
	o := c.owner
	g := &o.admission
	o.mu.Lock()
	g.mu.Lock()
	c.mu.Lock()
	p.mu.Lock()
	defer o.mu.Unlock()
	defer g.mu.Unlock()
	defer c.mu.Unlock()
	defer p.mu.Unlock()
	if !p.running || p.revoked || c.closing || o.closing || o.closed || g.phase == storageAdmissionClosing {
		return errors.Join(os.ErrClosed, p.err)
	}
	if o.transitionRequired {
		return errStorageOwnerTransitionRequired
	}
	if g.exclusiveCensus != p || g.phase != storageAdmissionStartup || g.startup != p.mutation || g.generation != p.generation {
		return storagebudget.ErrAuthority
	}
	return nil
}
func (p *storageExclusiveStartupCensus) current(ctx context.Context) error {
	if err := p.permit(ctx); err != nil {
		return err
	}
	if err := p.mutation.current(ctx, false); err != nil {
		return err
	}
	return p.permit(ctx)
}

func (p *storageExclusiveStartupCensus) held(ctx context.Context) error {
	if err := p.current(ctx); err != nil {
		return err
	}
	c := p.c
	if err := c.home.requireFixedZero(ctx); err != nil {
		return err
	}
	if c.startupUse == nil || c.startupWork == nil || len(c.startupHolds) != len(c.owner.orderedRoots()) {
		return storagebudget.ErrAuthority
	}
	if err := c.startupUse.Current(); err != nil {
		return err
	}
	if err := c.startupWork.current(); err != nil {
		return err
	}
	for _, h := range c.startupHolds {
		if err := h.current(ctx); err != nil {
			return err
		}
	}
	return p.current(ctx)
}

func (p *storageExclusiveStartupCensus) Close() error {
	if p == nil || p.self != p {
		return storagebudget.ErrAuthority
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.revoked = true
	p.stage = storageExclusiveClosing
	p.err = errors.Join(p.err, os.ErrClosed)
	if p.running || p.borrowers != 0 || p.helper != nil {
		return errors.Join(storagebudget.ErrBusy, p.err)
	}
	return p.err
}

func (p *storageExclusiveStartupCensus) finish(err error) error {
	o := p.c.owner
	g := &o.admission
	o.mu.Lock()
	g.mu.Lock()
	p.c.mu.Lock()
	p.mu.Lock()
	if !p.running {
		prior := p.err
		p.mu.Unlock()
		p.c.mu.Unlock()
		g.mu.Unlock()
		o.mu.Unlock()
		return prior
	}
	if p.borrowers != 0 || p.helper != nil || o.active != 2 {
		p.mu.Unlock()
		p.c.mu.Unlock()
		g.mu.Unlock()
		o.mu.Unlock()
		return errors.Join(err, storagebudget.ErrBusy)
	}
	if p.c.closing || o.closing || g.phase == storageAdmissionClosing {
		p.err = errors.Join(p.err, os.ErrClosed)
	}
	p.err = errors.Join(p.err, err)
	p.running = false
	if p.err == nil {
		p.stage = storageExclusiveCompleted
	} else {
		p.stage = storageExclusiveFailed
	}
	if g.exclusiveCensus == p {
		g.exclusiveCensus = nil
	}
	p.mu.Unlock()
	p.c.mu.Unlock()
	g.mu.Unlock()
	o.mu.Unlock()
	result := o.drop()
	p.mu.Lock()
	p.err = errors.Join(p.err, result)
	result = p.err
	close(p.done)
	p.mu.Unlock()
	return result
}

// Each borrowed observation owns its actual retain before its first native
// currentness/query and drops only after the owning pass's final cleanup.
func (p *storageExclusiveStartupCensus) borrow(ctx context.Context) error {
	if err := storageExclusiveContextErr(ctx); err != nil {
		return err
	}
	o := p.c.owner
	g := &o.admission
	o.mu.Lock()
	g.mu.Lock()
	p.c.mu.Lock()
	p.mu.Lock()
	defer o.mu.Unlock()
	defer g.mu.Unlock()
	defer p.c.mu.Unlock()
	defer p.mu.Unlock()
	if !p.running || p.revoked || p.stage != storageExclusiveMeasuring || p.helper != nil || p.c.closing || o.closing || o.transitionRequired || g.exclusiveCensus != p || g.phase != storageAdmissionStartup || g.generation != p.generation || o.active == ^uint64(0) || p.borrowers == ^uint64(0) {
		return storagebudget.ErrAuthority
	}
	p.borrowers++
	o.active++
	return nil
}
func (p *storageExclusiveStartupCensus) dropBorrow() error {
	p.mu.Lock()
	if p.borrowers == 0 {
		p.mu.Unlock()
		return storagebudget.ErrAuthority
	}
	p.borrowers--
	p.mu.Unlock()
	return p.c.owner.drop()
}

func storageExclusiveJournal(t *storageIntentTable) [32]byte {
	h := sha256.New()
	h.Write([]byte("desk-exclusive-census-selected-journal-v1\x00"))
	for i, s := range t.slots {
		h.Write([]byte{byte(i)})
		if len(s.page) == 0 {
			h.Write([]byte{0})
		} else {
			h.Write([]byte{1})
			v := sha256.Sum256(s.page)
			h.Write(v[:])
		}
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Conservative retained representation credits, not Go heap/OS allocation
// evidence. Temporary strict-loader buffers are unchanged and not retained.
func storageExclusiveViewBound(v storagebudget.HostInspection) error {
	if len(v.Roots) > storagebudget.MaxRoots || len(v.Workspaces) > storagebudget.MaxWorkspaces || len(v.Configuration.Filesystems) > storagebudget.MaxFilesystems || len(v.Configuration.CapacityConstraints) > storagebudget.MaxCapacityConstraints {
		return storagebudget.ErrBounds
	}
	credits := uint64(1024 + len(v.Roots)*1024 + len(v.Workspaces)*512 + len(v.Configuration.Filesystems)*512 + len(v.Configuration.CapacityConstraints)*256 + storagebudget.Slots*512)
	for _, slot := range v.Slots {
		t := slot.Ticket
		if len(t.Components) > storagebudget.MaxComponents || len(t.Changes) > storagebudget.MaxComponents || len(t.Workflow) > 128 || len(t.Operation) > 128 {
			return storagebudget.ErrBounds
		}
		credits += uint64(len(t.Components)*512 + len(t.Changes)*128 + len(t.Workflow) + len(t.Operation))
	}
	if credits > 8<<20 {
		return storagebudget.ErrBounds
	}
	return nil
}

func (p *storageExclusiveStartupCensus) validateTable(ctx context.Context, t *storageIntentTable, v storagebudget.HostInspection) error {
	if v.Configuration.Maintenance || v.Configuration.Epoch == 0 {
		return storagebudget.ErrStale
	}
	missing, err := p.c.owner.registrationMissingFrom(v)
	if err != nil || storageRegistrationCount(missing) != 0 {
		return errors.Join(storagebudget.ErrStale, err)
	}
	for _, s := range v.Slots {
		if storageUnretired(s) {
			return storagebudget.ErrBusy
		}
	}
	for _, s := range t.slots {
		if !s.record.reusable() {
			return errStorageOwnerRecovery
		}
	}
	return p.c.owner.validateIntentMappings(ctx, t, storageHostState(v))
}

func (c *storageBudgetConstruction) runExclusiveCensus(ctx context.Context) (err error) {
	caller := ctx
	o := c.owner
	if !o.censusMu.TryLock() {
		return storagebudget.ErrBusy
	}
	defer o.censusMu.Unlock()
	if err = c.exclusiveBoundary("before-phase"); err != nil {
		return err
	}
	p, err := c.beginExclusiveCensus(ctx)
	if err != nil {
		return err
	}
	var observation *storageOwnedCapture
	defer func() { err = p.finish(err); o.stageStartupMeasurement(observation, c, caller, err) }() // all invocation-owned cleanup has returned
	ctx, cancel := context.WithTimeout(ctx, storageCensusTime)
	defer cancel()
	ctx = storageMutationContext(ctx, c.mutation)
	if err = p.held(ctx); err != nil {
		return err
	}
	err = o.withControl(ctx, func(ctx context.Context, t *storageIntentTable) error {
		if e := p.permit(ctx); e != nil {
			return e
		}
		if e := p.boundary("capture"); e != nil {
			return e
		}
		if e := p.current(ctx); e != nil {
			return e
		}
		var e error
		p.snapshot, p.view, e = c.ref.cell.q.CaptureExclusiveCensus(ctx)
		if e != nil {
			return e
		}
		if e = storageExclusiveViewBound(p.view); e != nil {
			return e
		}
		if e = p.validateTable(ctx, t, p.view); e != nil {
			return e
		}
		p.epoch = p.view.Configuration.Epoch
		p.journal = storageExclusiveJournal(t)
		return p.current(ctx)
	})
	if err != nil {
		return err
	}
	if err = p.advance(ctx, storageExclusiveCapturing, storageExclusiveMeasuring); err != nil {
		return err
	}
	census, err := o.measureCensusWithExclusive(ctx, o.orderedRoots(), p, &observation)
	if err != nil {
		return err
	}
	if err = p.held(ctx); err != nil {
		return err
	}
	if err = p.advance(ctx, storageExclusiveMeasuring, storageExclusiveReady); err != nil {
		return err
	}
	h, err := p.beginControl(ctx)
	if err != nil {
		return err
	}
	// Finish only after withControl has returned from its deferred gate Close.
	err = o.withControl(ctx, func(ctx context.Context, t *storageIntentTable) error {
		if e := h.current(ctx); e != nil {
			return e
		}
		if storageExclusiveJournal(t) != p.journal {
			return storagebudget.ErrStale
		}
		if e := p.validateTable(ctx, t, p.view); e != nil {
			return e
		}
		if !storageCensusMatches(p.view, census) {
			return storagebudget.ErrStale
		}
		if e := c.reserveOwnerEffectForCensus(ctx, c.anchor, nil, "anchor/census", h); e != nil {
			return e
		}
		if e := p.boundary("post-spend"); e != nil {
			return e
		}
		if e := h.enterQ(ctx); e != nil {
			return e
		}
		if e := c.ref.cell.q.PublishExclusiveCensus(ctx, p.snapshot, census); e != nil {
			return e
		}
		return errors.Join(h.current(ctx), p.boundary("control-return"))
	})
	err = h.finish(err)
	if err != nil {
		return err
	}
	if err = p.boundary("helper-drained"); err != nil {
		return err
	}
	if err = p.current(ctx); err != nil {
		return err
	}
	if err = c.ref.cell.current(); err != nil {
		return err
	}
	if err = p.permit(ctx); err != nil {
		return err
	}
	o.mu.Lock()
	o.censusEpoch = p.epoch
	o.mu.Unlock()
	if observation != nil {
		observation.value.Epoch = p.epoch
	}
	return nil
}

func (p *storageExclusiveStartupCensus) advance(ctx context.Context, from, to storageExclusiveCensusStage) error {
	if err := storageExclusiveContextErr(ctx); err != nil {
		return err
	}
	c := p.c
	o := c.owner
	g := &o.admission
	o.mu.Lock()
	g.mu.Lock()
	c.mu.Lock()
	p.mu.Lock()
	defer o.mu.Unlock()
	defer g.mu.Unlock()
	defer c.mu.Unlock()
	defer p.mu.Unlock()
	if !p.running || p.revoked || p.stage != from || p.borrowers != 0 || p.helper != nil || o.active != 2 || o.closing || o.transitionRequired || c.closing || g.phase != storageAdmissionStartup || g.exclusiveCensus != p || g.generation != p.generation {
		return storagebudget.ErrAuthority
	}
	p.stage = to
	return nil
}

func (p *storageExclusiveStartupCensus) beginControl(ctx context.Context) (*storageExclusiveCensusControl, error) {
	if err := storageExclusiveContextErr(ctx); err != nil {
		return nil, err
	}
	o := p.c.owner
	g := &o.admission
	o.mu.Lock()
	g.mu.Lock()
	p.c.mu.Lock()
	p.mu.Lock()
	defer o.mu.Unlock()
	defer g.mu.Unlock()
	defer p.c.mu.Unlock()
	defer p.mu.Unlock()
	if !p.running || p.revoked || p.stage != storageExclusiveReady || p.helper != nil || p.borrowers != 0 || o.active != 2 || o.closing || o.transitionRequired || p.c.closing || g.phase != storageAdmissionStartup || g.exclusiveCensus != p || g.generation != p.generation {
		return nil, storagebudget.ErrAuthority
	}
	h := &storageExclusiveCensusControl{phase: p, retained: true}
	h.self = h
	p.helper = h
	o.active++
	return h, nil
}
func (h *storageExclusiveCensusControl) current(ctx context.Context) error {
	if h == nil || h.self != h || h.phase == nil {
		return storagebudget.ErrAuthority
	}
	p := h.phase
	p.mu.Lock()
	ok := p.helper == h && h.retained && (p.stage == storageExclusiveReady || p.stage == storageExclusivePublishing)
	p.mu.Unlock()
	if !ok {
		return storagebudget.ErrAuthority
	}
	return p.current(ctx)
}
func (h *storageExclusiveCensusControl) transition(ctx context.Context, commit *storagebudget.CapacityVector, enter bool) error {
	if err := storageExclusiveContextErr(ctx); err != nil {
		return err
	}
	p := h.phase
	c := p.c
	o := c.owner
	g := &o.admission
	o.mu.Lock()
	g.mu.Lock()
	c.mu.Lock()
	p.mu.Lock()
	defer o.mu.Unlock()
	defer g.mu.Unlock()
	defer c.mu.Unlock()
	defer p.mu.Unlock()
	if !p.running || p.revoked || p.helper != h || !h.retained || p.borrowers != 0 || o.active != 3 || o.closing || o.transitionRequired || c.closing || g.phase != storageAdmissionStartup || g.exclusiveCensus != p || g.generation != p.generation {
		return storagebudget.ErrAuthority
	}
	if commit != nil {
		if p.stage != storageExclusiveReady || c.grants["anchor/census"] || h.qEntered {
			return storagebudget.ErrBounds
		}
		c.grants["anchor/census"] = true
		c.spent.Bytes += commit.Bytes
		c.spent.Inodes += commit.Inodes
	} else if enter {
		if p.stage != storageExclusiveReady || !c.grants["anchor/census"] || h.qEntered {
			return storagebudget.ErrAuthority
		}
		h.qEntered = true
		p.stage = storageExclusivePublishing
	}
	return nil
}
func (h *storageExclusiveCensusControl) enterQ(ctx context.Context) error {
	return h.transition(ctx, nil, true)
}
func (h *storageExclusiveCensusControl) finish(err error) error {
	p := h.phase
	p.mu.Lock()
	if !h.retained {
		prior := h.err
		p.mu.Unlock()
		return prior
	}
	h.err = errors.Join(h.err, err)
	h.retained = false
	p.helper = nil
	p.mu.Unlock()
	result := errors.Join(h.err, p.c.owner.drop())
	p.mu.Lock()
	h.err = result
	p.mu.Unlock()
	return result
}
