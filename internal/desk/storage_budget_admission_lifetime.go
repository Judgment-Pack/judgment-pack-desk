package desk

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Admission exclusion is separate from action permission and capacity. Only the
// original owner creates these address-sealed lifetimes; context carries an
// existing reference and cannot manufacture one.
type storageAdmissionPhase uint8

const (
	storageAdmissionStartup storageAdmissionPhase = iota
	storageAdmissionHandoff
	storageAdmissionOpen
	storageAdmissionEnrollment
	storageAdmissionFailed
	storageAdmissionClosing
)

type storageAdmissionGate struct {
	mu                  sync.Mutex
	phase               storageAdmissionPhase
	generation          uint64
	measurementActivity uint64 // mu; saturating diagnostic counter, never authority
	shared              uint64
	prefixes            uint64
	leases              map[*storageMutationAuthority]bool
	startup, handoff    *storageMutationAuthority
	preparation         *storageIdentityPreparation
	exclusiveCensus     *storageExclusiveStartupCensus
	enrollment          *storageEnrollmentEpoch
}

type storageMutationKind uint8

const (
	storageMutationOrdinary storageMutationKind = iota + 1
	storageMutationStartup
	storageMutationHandoff // read-only; never a Q or host-save authority
)

type storageMutationAuthority struct {
	self             *storageMutationAuthority
	owner            *storageBudgetOwner
	construction     *storageBudgetConstruction
	kind             storageMutationKind
	generation       uint64
	closed, retained bool                 // owner's admission.mu
	validatedPass    *storageCapacityPass // handoff only; installed by actual revalidation
}

type storageMutationContextKey struct{}

func storageMutationContext(ctx context.Context, a *storageMutationAuthority) context.Context {
	return context.WithValue(ctx, storageMutationContextKey{}, a)
}

func storageMutationIn(ctx context.Context, o *storageBudgetOwner) *storageMutationAuthority {
	if ctx == nil {
		return nil
	}
	a, _ := ctx.Value(storageMutationContextKey{}).(*storageMutationAuthority)
	if a == nil || a.self != a || a.owner != o {
		return nil
	}
	return a
}

func (o *storageBudgetOwner) initializeAdmission() {
	o.admission.generation = 1
	o.admission.leases = map[*storageMutationAuthority]bool{}
	o.admission.phase = storageAdmissionOpen
	if o.ref.cell.startup != nil {
		// A construction candidate is blocked before its first publication.
		o.admission.phase = storageAdmissionStartup
	}
}

func (o *storageBudgetOwner) beginAdmission(ctx context.Context) (*storageMutationAuthority, error) {
	if ctx == nil {
		return nil, storagebudget.ErrAuthority
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := o.retain(); err != nil {
		return nil, err
	}
	g := &o.admission
	g.mu.Lock()
	var err error
	if g.phase != storageAdmissionOpen {
		err = storagebudget.ErrBusy
	} else if g.generation == 0 || g.shared == ^uint64(0) {
		err = storagebudget.ErrBounds
	}
	if err != nil {
		g.mu.Unlock()
		return nil, errors.Join(err, o.drop())
	}
	a := &storageMutationAuthority{owner: o, kind: storageMutationOrdinary, generation: g.generation, retained: true}
	a.self = a
	g.shared++
	if g.measurementActivity != ^uint64(0) {
		g.measurementActivity++
	}
	g.leases[a] = true
	g.mu.Unlock()
	return a, nil
}

func (a *storageMutationAuthority) current(ctx context.Context, write bool) error {
	return a.currentExclusion(ctx, write, false)
}

// Only the already-existing restriction fence may validate exclusion after
// positive invalidity was latched. General mutation authority remains refused.
func (a *storageMutationAuthority) currentForFence(ctx context.Context) error {
	return a.currentExclusion(ctx, true, true)
}

func (a *storageMutationAuthority) currentExclusion(ctx context.Context, write, fence bool) error {
	if a == nil || a.self != a || a.owner == nil || ctx == nil {
		return storagebudget.ErrAuthority
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	o := a.owner
	if !o.valid() {
		return storagebudget.ErrAuthority
	}
	o.mu.Lock()
	closing, changed := o.closing || o.closed, o.transitionRequired
	o.mu.Unlock()
	if closing {
		return os.ErrClosed
	}
	if changed && !fence {
		return errStorageOwnerTransitionRequired
	}
	g := &o.admission
	g.mu.Lock()
	ok := !a.closed && a.retained && a.generation == g.generation
	switch a.kind {
	case storageMutationOrdinary:
		ok = ok && g.phase == storageAdmissionOpen && g.leases[a] && g.shared > 0
	case storageMutationStartup:
		ok = ok && g.phase == storageAdmissionStartup && g.startup == a
	case storageMutationHandoff:
		ok = ok && !write && g.phase == storageAdmissionHandoff && g.handoff == a
	default:
		ok = false
	}
	if write && !fence && g.exclusiveCensus != nil {
		ok = false
	}
	g.mu.Unlock()
	if !ok {
		return storagebudget.ErrAuthority
	}
	if a.kind != storageMutationOrdinary {
		c := a.construction
		if c == nil || c.self != c || c.owner != o || c.ref == nil || c.ref.cell != o.ref.cell {
			return storagebudget.ErrAuthority
		}
		return c.current(ctx, a.kind == storageMutationStartup)
	}
	return nil
}

func (o *storageBudgetOwner) requireMutation(ctx context.Context) error {
	return storageMutationIn(ctx, o).current(ctx, true)
}

func (o *storageBudgetOwner) requireFenceMutation(ctx context.Context) error {
	return storageMutationIn(ctx, o).currentForFence(ctx)
}

func (a *storageMutationAuthority) Close() error {
	if a == nil || a.self != a || a.owner == nil {
		return storagebudget.ErrAuthority
	}
	o := a.owner
	g := &o.admission
	g.mu.Lock()
	if a.closed || !a.retained {
		g.mu.Unlock()
		return nil
	}
	a.closed, a.retained = true, false
	if a.kind == storageMutationOrdinary {
		if !g.leases[a] || g.shared == 0 {
			g.mu.Unlock()
			return storagebudget.ErrAuthority
		}
		delete(g.leases, a)
		g.shared--
	} else {
		if g.startup == a {
			g.startup = nil
		}
		if g.handoff == a {
			g.handoff = nil
		}
		if g.phase != storageAdmissionClosing {
			g.phase = storageAdmissionFailed
		}
	}
	g.mu.Unlock()
	return o.drop()
}

func (o *storageBudgetOwner) closeAdmission() error {
	o.transfers.stop(nil, true)
	g := &o.admission
	g.mu.Lock()
	g.phase = storageAdmissionClosing
	startup, handoff, preparation, census, enrollment := g.startup, g.handoff, g.preparation, g.exclusiveCensus, g.enrollment
	g.mu.Unlock()
	var err error
	if enrollment != nil {
		if transfer := enrollment.transfer; transfer != nil && transfer.self == transfer && transfer.epoch == enrollment && transfer.registry == o.transfers {
			// Its lexical owner joins actual completion/Close through the registered
			// transfer drain. Revocation cannot report Busy as a completed drain.
			enrollment.revoke()
			enrollment.cancel()
		} else {
			err = errors.Join(err, enrollment.Close())
		}
	}
	if census != nil {
		e := census.Close()
		if errors.Is(e, storagebudget.ErrBusy) {
			return e
		}
		err = errors.Join(err, e)
	}
	if startup != nil && startup.construction != nil {
		c := startup.construction
		c.mu.Lock()
		done := c.exclusiveCleanupDone
		c.mu.Unlock()
		if done != nil {
			select {
			case <-done:
			default:
				return errors.Join(err, storagebudget.ErrBusy)
			}
		}
	}
	if preparation != nil {
		err = errors.Join(err, preparation.Close())
	}
	if startup != nil {
		err = errors.Join(err, startup.Close())
	}
	if handoff != nil {
		err = errors.Join(err, handoff.Close())
	}
	return err
}

func (o *storageBudgetOwner) admissionOpen() bool {
	g := &o.admission
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.phase == storageAdmissionOpen
}

// This proof exists only on actual unpublished New construction. It is consumed
// once before the owner is installed; it never pretends that InitializeHeld had
// an owner admission lease before that owner existed.
type storageConstructionBootstrap struct {
	self         *storageConstructionBootstrap
	construction *storageBudgetConstruction
}

func (b *storageConstructionBootstrap) current(ctx context.Context) error {
	if b == nil || b.self != b || b.construction == nil {
		return storagebudget.ErrAuthority
	}
	c := b.construction
	c.mu.Lock()
	ok := c.bootstrap == b && !c.bootstrapTransferred && c.owner == nil && !c.closing && !c.closed
	c.mu.Unlock()
	if !ok {
		return storagebudget.ErrAuthority
	}
	return c.current(ctx, true)
}

func (c *storageBudgetConstruction) constructionMutationContext(ctx context.Context) (context.Context, error) {
	if c == nil || c.self != c {
		return ctx, storagebudget.ErrAuthority
	}
	c.mu.Lock()
	a, b := c.mutation, c.bootstrap
	owner := c.owner
	c.mu.Unlock()
	if owner == nil {
		return ctx, b.current(ctx)
	}
	if err := a.current(ctx, true); err != nil {
		return ctx, err
	}
	return storageMutationContext(ctx, a), nil
}

func (c *storageBudgetConstruction) requireConstructionMutation(ctx context.Context) error {
	if c == nil || c.self != c {
		return storagebudget.ErrAuthority
	}
	c.mu.Lock()
	a, b, owner := c.mutation, c.bootstrap, c.owner
	c.mu.Unlock()
	if owner == nil {
		return b.current(ctx)
	}
	if storageMutationIn(ctx, owner) != a {
		return storagebudget.ErrAuthority
	}
	return a.current(ctx, true)
}

func (c *storageBudgetConstruction) installAdmissionOwner(ctx context.Context, o *storageBudgetOwner) error {
	if c == nil || c.self != c || c.ref == nil || o == nil || !o.valid() || o.ref.cell != c.ref.cell || o.ref.cell.startup != c {
		return storagebudget.ErrAuthority
	}
	if err := c.bootstrap.current(ctx); err != nil {
		return err
	}
	if err := o.retain(); err != nil {
		return err
	}
	g := &o.admission
	g.mu.Lock()
	c.mu.Lock()
	if c.closing || c.closed || c.bootstrapTransferred || c.owner != nil || g.phase != storageAdmissionStartup || g.shared != 0 || g.startup != nil || g.generation == ^uint64(0) {
		c.mu.Unlock()
		g.mu.Unlock()
		return errors.Join(storagebudget.ErrAuthority, o.drop())
	}
	g.generation++
	a := &storageMutationAuthority{owner: o, construction: c, kind: storageMutationStartup, generation: g.generation, retained: true}
	a.self = a
	g.startup = a
	c.owner, c.mutation, c.bootstrapTransferred = o, a, true
	c.mu.Unlock()
	g.mu.Unlock()
	return nil
}

func (c *storageBudgetConstruction) beginAdmissionHandoff(ctx context.Context) error {
	if c == nil || c.owner == nil {
		return storagebudget.ErrAuthority
	}
	if err := c.mutation.current(ctx, true); err != nil {
		return err
	}
	// loadOrEnrollOwner's original defer owns and completes these releases.
	if c.startupUse != nil || c.startupWork != nil || len(c.startupHolds) != 0 {
		return storagebudget.ErrBusy
	}
	c.owner.mu.Lock()
	if c.owner.active != 1 || c.owner.closing || c.owner.closed || c.owner.transitionRequired {
		c.owner.mu.Unlock()
		return storagebudget.ErrBusy
	}
	g := &c.owner.admission
	g.mu.Lock()
	defer g.mu.Unlock()
	defer c.owner.mu.Unlock()
	a := c.mutation
	if g.phase != storageAdmissionStartup || g.startup != a || a.closed || !a.retained || g.shared != 0 || g.prefixes != 0 || g.preparation != nil || g.generation == ^uint64(0) {
		return storagebudget.ErrAuthority
	}
	g.generation++
	h := &storageMutationAuthority{owner: c.owner, construction: c, kind: storageMutationHandoff, generation: g.generation, retained: true}
	h.self = h
	// Move the single owner retain; revoked startup authority cannot issue again.
	a.closed, a.retained = true, false
	g.startup, g.handoff, g.phase = nil, h, storageAdmissionHandoff
	c.handoff = h
	return nil
}

func (c *storageBudgetConstruction) openAdmissionHandoff(ctx context.Context, h *storageMutationAuthority) error {
	if h == nil || h != c.handoff || h.kind != storageMutationHandoff {
		return storagebudget.ErrAuthority
	}
	if err := h.current(ctx, false); err != nil {
		return err
	}
	if c.home.exclusive || c.startupUse != nil || c.startupWork != nil || len(c.startupHolds) != 0 {
		return storagebudget.ErrAuthority
	}
	g := &c.owner.admission
	g.mu.Lock()
	pass := h.validatedPass
	g.mu.Unlock()
	if pass == nil || pass.self != pass || pass.owner != c.owner || pass.mutation != h || pass.completed.IsZero() {
		return storagebudget.ErrAuthority
	}
	pass.mu.Lock()
	closed := pass.closed
	pass.mu.Unlock()
	if !closed {
		return storagebudget.ErrBusy
	}
	if err := pass.checkTime(ctx, false); err != nil {
		return err
	}
	o := c.owner
	o.mu.Lock()
	invalid := o.transitionRequired || o.closing || o.closed
	// Only the handoff retain may remain after the validation pass has closed.
	busy := o.active != 1
	if invalid || busy || ctx.Err() != nil {
		o.mu.Unlock()
		return errors.Join(storagebudget.ErrAuthority, ctx.Err())
	}
	g = &o.admission
	g.mu.Lock()
	c.mu.Lock()
	if c.closing || c.closed || h.validatedPass != pass || g.phase != storageAdmissionHandoff || g.handoff != h || h.closed || !h.retained || g.generation != h.generation || g.shared != 0 || g.prefixes != 0 || g.preparation != nil || g.generation == ^uint64(0) {
		c.mu.Unlock()
		g.mu.Unlock()
		o.mu.Unlock()
		return storagebudget.ErrAuthority
	}
	g.generation++
	h.closed, h.retained = true, false
	g.handoff, g.phase = nil, storageAdmissionOpen
	c.mu.Unlock()
	g.mu.Unlock()
	o.mu.Unlock()
	return o.drop()
}

func (s *Server) prepareStorageAdmissionHandoff(ctx context.Context) error {
	if s.cfg.parent != nil || s.storageEnrollment == nil {
		return nil
	}
	c := s.storageEnrollment.construction
	if c == nil || c.server != s {
		return storagebudget.ErrAuthority
	}
	if err := c.beginAdmissionHandoff(ctx); err != nil {
		return errors.Join(err, c.mutation.Close())
	}
	return nil
}

// The handoff inspector borrows the ticket, never obtains ordinary admission.
// It uses the normal use-SH path only after the original startup EX was closed.
func (o *storageBudgetOwner) beginStorageCapacityPassForHandoff(ctx context.Context, h *storageMutationAuthority) (*storageCapacityPass, error) {
	if h == nil || h.owner != o || h.kind != storageMutationHandoff || h.construction == nil || h.construction.home.exclusive || h.construction.startupUse != nil {
		return nil, storagebudget.ErrAuthority
	}
	if err := h.current(ctx, false); err != nil {
		return nil, err
	}
	return o.beginStorageCapacityPass(storageMutationContext(ctx, h), nil)
}

// Observation only. Owner->gate is the existing lock order; no Q/native call is
// made while these locks are held. Reads/Close do not advance this counter.
type storageOwnedActivity struct {
	Generation, Ordinary uint64
	Idle                 bool
}

func (o *storageBudgetOwner) ownedMeasurementActivity() storageOwnedActivity {
	o.mu.Lock()
	defer o.mu.Unlock()
	g := &o.admission
	g.mu.Lock()
	defer g.mu.Unlock()
	return storageOwnedActivity{Generation: g.generation, Ordinary: g.measurementActivity, Idle: o.self == o && !o.closing && !o.closed && !o.transitionRequired && o.currentCensus == nil && g.phase == storageAdmissionOpen && g.shared == 0 && g.prefixes == 0 && len(g.leases) == 0 && g.startup == nil && g.handoff == nil && g.preparation == nil && g.exclusiveCensus == nil && g.enrollment == nil}
}
func (v *storageOwnedMeasurement) activityContinuous(before, after storageOwnedActivity) bool {
	return v != nil && before == after && after.Idle && after.Generation != 0 && after.Ordinary != ^uint64(0) && v.AdmissionGeneration == after.Generation && v.OrdinaryActivity == after.Ordinary
}
