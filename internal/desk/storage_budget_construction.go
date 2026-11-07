package desk

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Only actual parent New installs this address-sealed origin after adopting its
// ProjectRoot and original infrastructure home EX. It is not a path/FD factory.
type storageBudgetConstruction struct {
	self                 *storageBudgetConstruction
	server               *Server
	project              *ProjectRoot
	assistant            *assistantStore
	home                 *storageStartupHome
	providers            *retainedArtifactProviderRegistry
	selected             retainedArtifactRepository
	original             *storageOriginal
	anchor               *os.Root
	ref                  *storageInstallationRef
	owner                *storageBudgetOwner
	bootstrap            *storageConstructionBootstrap
	bootstrapTransferred bool
	mutation, handoff    *storageMutationAuthority
	closeDone            chan struct{}
	closeErr             error
	sources              *storageOriginalSources
	mu                   sync.Mutex
	closed               bool
	closing              bool
	grants               map[string]bool
	spent                storagebudget.CapacityVector
	identityLaunch       *storageIdentityLaunch // original once-total launch accounting; never reset on epoch cleanup
	// Read-only startup comparison. Actual physical files remain original; these
	// detached cores never authorize publication, Q, sources or ordinary writes.
	startupPhysical          [2][]byte
	startupCores             [2][]byte
	startupLineage           *storageCompositionLineage
	startupComposed          bool
	startupFirstBase         bool
	startupLocal             bool
	startupIdentityCompleted bool
	// Enables fresh read-only native/current-row joins, never a cached history grant.
	moveHistory             bool
	moveStartup             *storagePolicyOriginal // strict detached early-cut operands; both raw banks rechecked before comparison
	fault                   *storageBootstrapFault
	startupUse              *storageMaintenanceUse
	startupWork             *storageNamedFile
	startupHolds            []*storageDomainHold
	startupCensusEpoch      uint64
	exclusiveCensusEligible bool
	exclusiveCensus         *storageExclusiveStartupCensus
	exclusiveCleanupDone    chan struct{}
	exclusiveCleanupErr     error
	enrollmentFault         *storageEnrollmentFault      // deterministic package-test boundary, nil in production
	exclusiveFault          *storageExclusiveCensusFault // deterministic package-test boundary seam; nil in production
}

func (c *storageBudgetConstruction) failOwner(key, phase string) error {
	return c.fault.fail(key, phase)
}

func (s *Server) prepareStorageBudgetConstruction(ctx context.Context) (_ *storageBudgetConstruction, err error) {
	if ctx == nil || s == nil || s.cfg.parent != nil || s.deskOwner() != s || s.project == nil || s.project.own == nil || s.project.own.root != s.root || !s.assistant.usable() || s.storageConstruction != nil {
		return nil, storagebudget.ErrAuthority
	}
	p := s.storageInfrastructure
	if p == nil || p.server != s || p.home == nil || !p.home.exclusive || p.config == nil {
		return nil, storagebudget.ErrAuthority
	}
	if err = p.home.requireFixedZero(ctx); err != nil {
		return nil, err
	}
	a, err := s.assistant.root.Stat(".")
	if err != nil {
		return nil, err
	}
	b, err := p.config.root.Stat(".")
	if err != nil || !os.SameFile(a, b) {
		return nil, errors.Join(errStorageCustody, err)
	}
	o, err := p.pinConstructionOriginal(ctx)
	if err != nil {
		return nil, err
	}
	b, err = o.config.Stat(".")
	if err != nil || !os.SameFile(a, b) {
		return nil, errors.Join(errStorageCustody, err, o.close())
	}
	c := &storageBudgetConstruction{server: s, project: s.project, assistant: s.assistant, home: p.home, providers: s.retainedArtifactProviders, selected: s.retainedArtifacts, original: o, grants: map[string]bool{}}
	c.self = c
	c.closeDone = make(chan struct{})
	c.bootstrap = &storageConstructionBootstrap{construction: c}
	c.bootstrap.self = c.bootstrap
	s.storageConstruction = c
	if err = c.current(ctx, true); err != nil {
		return nil, errors.Join(err, c.Close())
	}
	return c, nil
}

// Construction inherits the actual launch's retained ancestry, whose safe
// ancestor rules permit a non-private parent. The standalone path-only helper
// keeps its stricter private-parent prerequisite. Every independently owned
// handle here must still name the launch's original object; the existing home
// descriptor alone retains EX and performs the eventual same-FD SH handoff.
func (p *storageInfrastructureStart) pinConstructionOriginal(ctx context.Context) (_ *storageOriginal, err error) {
	if p == nil || p.home == nil || p.home.start != p || !p.home.exclusive || p.config == nil {
		return nil, storagebudget.ErrAuthority
	}
	if err = p.authority(ctx); err != nil {
		return nil, err
	}
	if err = p.home.requireFixedZero(ctx); err != nil {
		return nil, err
	}
	base, err := p.base.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	o := &storageOriginal{}
	defer func() {
		if err != nil {
			if len(o.roots) == 0 {
				err = errors.Join(err, base.Close())
			} else {
				err = errors.Join(err, o.close())
			}
		}
	}()
	parent := base
	chain := append(append([]*storageInfrastructureDirectory(nil), p.ancestry...), p.config)
	for _, original := range chain {
		n, e := storagePinChild(parent, original.name, original == p.config)
		if e != nil {
			return nil, e
		}
		o.roots = append(o.roots, n)
		if !os.SameFile(n.identity, original.identity) {
			return nil, errStorageCustody
		}
		parent = n.root
	}
	o.config = parent
	lease, err := storageOpenFile(o.config, ".home-use.lock", 0)
	if err != nil {
		return nil, err
	}
	o.files = append(o.files, lease)
	o.lease = lease.file
	if !os.SameFile(lease.identity, p.home.identity) {
		return nil, errStorageCustody
	}
	if err = p.home.requireFixedZero(ctx); err != nil {
		return nil, err
	}
	if err = o.current(); err != nil {
		return nil, err
	}
	return o, nil
}

func (c *storageBudgetConstruction) current(ctx context.Context, exclusive bool) error {
	if err := c.originCurrent(ctx, exclusive); err != nil {
		return err
	}
	if c.sources != nil {
		return c.sources.current(ctx)
	}
	return nil
}

// originCurrent is admission/current-construction validation. Closing always
// refuses here; a retained use has its own sealed physical-custody check below.
func (c *storageBudgetConstruction) originCurrent(ctx context.Context, exclusive bool) error {
	if c == nil || c.self != c || ctx == nil {
		return storagebudget.ErrAuthority
	}
	c.mu.Lock()
	closed := c.closed || c.closing
	c.mu.Unlock()
	if closed {
		return os.ErrClosed
	}
	if exclusive && (c.home == nil || !c.home.exclusive) {
		return storagebudget.ErrAuthority
	}
	return c.originPhysicalCurrent(ctx)
}

// originPhysicalCurrent checks original bindings and descriptions, never grants
// a use or a mutation. The only draining caller is a sealed, unreleased use
// validated by originalCurrentLocked while its installation mutex is held.
func (c *storageBudgetConstruction) originPhysicalCurrent(ctx context.Context) error {
	if c == nil || c.self != c || ctx == nil || c.server == nil || c.server.storageConstruction != c || c.server.cfg.parent != nil || c.server.deskOwner() != c.server || c.project == nil || c.project.own == nil || c.server.project != c.project || c.project.own.root != c.server.root || c.server.assistant != c.assistant || c.server.retainedArtifactProviders != c.providers || !storageSameSelectedProvider(c.server.retainedArtifacts, c.selected) || c.server.storageInfrastructure == nil || c.home != c.server.storageInfrastructure.home {
		return storagebudget.ErrAuthority
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.home.current(ctx); err != nil {
		return err
	}
	rootInfo, err := c.server.root.Stat(".")
	if err != nil {
		return err
	}
	normalInfo, err := c.project.own.dirFile.Stat()
	if err != nil || !os.SameFile(rootInfo, normalInfo) || !os.SameFile(rootInfo, c.project.info) {
		return errors.Join(errStorageCustody, err)
	}
	c.project.own.mu.Lock()
	adopted := c.project.own.adopted
	c.project.own.mu.Unlock()
	if !adopted {
		return storagebudget.ErrAuthority
	}
	if err := c.original.current(); err != nil {
		return err
	}
	a, err := c.original.config.Stat(".")
	if err != nil {
		return err
	}
	b, err := c.assistant.root.Stat(".")
	if err != nil || !os.SameFile(a, b) {
		return errors.Join(errStorageCustody, err)
	}
	if p, ok := c.selected.(*persistentInjectedArtifactProvider); ok {
		if c.providers == nil || c.providers.byID[p.id] == nil || c.providers.byID[p.id].provider != p {
			return storagebudget.ErrAuthority
		}
		if err = c.providers.byID[p.id].current(); err != nil {
			return err
		}
		pins := c.server.storageStartupProviders
		var pin *dialectArtifactSelectedProviderPin
		if pins != nil {
			pin = pins.byID[p.id]
		}
		if pin == nil || pin.operation == nil || pin.operation.provider != p || pin.operation.finished.Load() {
			return storagebudget.ErrAuthority
		}
	}
	return nil
}

func storageSameSelectedProvider(a, b retainedArtifactRepository) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ap, aok := a.(*persistentInjectedArtifactProvider)
	bp, bok := b.(*persistentInjectedArtifactProvider)
	return aok && bok && ap == bp
}

func (c *storageBudgetConstruction) Close() error {
	if c == nil {
		return nil
	}
	if c.self != c {
		return storagebudget.ErrAuthority
	}
	c.mu.Lock()
	if c.closing {
		done := c.closeDone
		c.mu.Unlock()
		if done != nil {
			<-done
		}
		c.mu.Lock()
		err := c.closeErr
		c.mu.Unlock()
		return err
	}
	c.closing = true
	if c.closeDone == nil {
		c.closeDone = make(chan struct{})
	}
	done, owner := c.closeDone, c.owner
	c.mu.Unlock()
	// The final operation needs construction/admission/owner locks to drain.
	// Neither cancellation nor this join runs under any of those locks.
	var err error
	if owner != nil {
		err = errors.Join(err, owner.Close())
		c.mu.Lock()
		census, cleanupDone := c.exclusiveCensus, c.exclusiveCleanupDone
		c.mu.Unlock()
		if census != nil {
			// Owner Close has revoked permission, but the executor owns every
			// native result/Close. Do not wait owner.closedDone before enabling
			// the once-only terminal continuation after this invocation drains.
			<-census.done
			// The original startup finalizer owns outer hold cleanup. It can
			// proceed only after running=false, and terminal cleanup joins it.
			if cleanupDone != nil {
				<-cleanupDone
			}
			c.mu.Lock()
			err = errors.Join(err, c.exclusiveCleanupErr)
			c.mu.Unlock()
			err = errors.Join(err, owner.Close())
		}
		if owner.closedDone != nil {
			<-owner.closedDone
		}
		owner.mu.Lock()
		err = errors.Join(err, owner.closeErr)
		owner.mu.Unlock()
	}
	if c.sources != nil {
		err = errors.Join(err, c.sources.Close())
	}
	if c.ref != nil {
		err = errors.Join(err, c.ref.Close())
	} else if c.original != nil {
		err = errors.Join(err, c.original.close())
	}
	c.mu.Lock()
	c.closed, c.closeErr = true, err
	close(done)
	c.mu.Unlock()
	return err
}
