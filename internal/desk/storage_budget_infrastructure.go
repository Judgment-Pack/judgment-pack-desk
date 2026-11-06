package desk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// This provision is a finite original-launch policy, not a Q token, physical
// allocation certificate, or permission for a subsequent launch to replay work.
const (
	storageInfrastructureMaxGroups = 12
	storageInfrastructureMaxBytes  = 3932160
	storageInfrastructureMaxInodes = 29
)

var errStorageInfrastructure = errors.New("Desk infrastructure changed or requires original-owner storage reconciliation")

// A safe ancestry can exist while custody of the assistant directory cannot.
// This preserves the pack-reading startup fallback, before any lease effect.
type storageInfrastructureAssistantError struct{ err error }

func (e *storageInfrastructureAssistantError) Error() string { return e.err.Error() }
func (e *storageInfrastructureAssistantError) Unwrap() error { return e.err }
func storageAssistantRefusal(err error) error                { return &storageInfrastructureAssistantError{err} }

type storageInfrastructureDirectory struct {
	parent      *os.Root
	name, label string
	root        *os.Root
	held        *os.File // normal descriptor, separately owned from root
	identity    os.FileInfo
	ours        bool
}

func storageInspectDirectory(parent *os.Root, name, label string, ours bool) (*storageInfrastructureDirectory, error) {
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if err = safeDirectory(label, before, ours); err != nil {
		return nil, storageAssistantRefusal(err)
	}
	afterCustodyCheck(label)
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	held, err := root.OpenFile(".", os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	d := &storageInfrastructureDirectory{parent: parent, name: name, label: label, root: root, held: held, identity: before, ours: ours}
	if err = d.current(false); err != nil {
		return nil, errors.Join(err, d.close())
	}
	return d, nil
}
func (d *storageInfrastructureDirectory) current(exact bool) error {
	before, err := d.parent.Lstat(d.name)
	if err != nil {
		return errors.Join(errStorageInfrastructure, err)
	}
	if err = safeDirectory(d.label, before, d.ours); err != nil {
		return storageAssistantRefusal(err)
	}
	held, err := d.held.Stat()
	if err != nil {
		return err
	}
	root, err := d.root.Stat(".")
	if err != nil {
		return err
	}
	after, err := d.parent.Lstat(d.name)
	if err != nil {
		return err
	}
	if !os.SameFile(d.identity, before) || !os.SameFile(before, held) || !os.SameFile(held, root) || !os.SameFile(held, after) || held.Mode() != after.Mode() {
		return errStorageInfrastructure
	}
	if err = safeDirectory(d.label, held, d.ours); err != nil {
		return storageAssistantRefusal(err)
	}
	if exact && needsNarrowing(held.Mode()) {
		return errStorageInfrastructure
	}
	return nil
}
func (d *storageInfrastructureDirectory) close() error {
	return errors.Join(d.held.Close(), d.root.Close())
}

// These objects are minted only by the actual adopted original Server.New.
// self, server, project and the server's own pointer are checked on each effect.
// Paths label launch selection; operations use retained parents and literals.
// Minted inline only at the actual original New callsite after project adoption
// and trusted config/home resolution. inspect consumes it exactly once.
type storageInfrastructureLaunch struct {
	self      *storageInfrastructureLaunch
	server    *Server
	project   *ProjectRoot
	configDir string
	consumed  bool
}

type storageInfrastructureStart struct {
	self                                    *storageInfrastructureStart
	origin                                  *storageInfrastructureLaunch
	server                                  *Server
	project                                 *ProjectRoot
	base                                    *os.Root
	ancestry                                []*storageInfrastructureDirectory
	parent                                  *os.Root
	configName                              string
	config, secrets                         *storageInfrastructureDirectory
	children                                []*storageInfrastructureDirectory
	gate                                    *os.File
	home                                    *storageStartupHome
	preBudget                               bool
	groups, dirs, files, modes, completions uint64
	spent                                   storagebudget.CapacityVector
	promoted                                bool
	completed                               map[*storageInfrastructureDirectory]bool
	fault                                   *storageInfrastructureFault
	failed                                  bool
}
type storageInfrastructureFault struct {
	name, phase string
	entered     chan struct{}
	release     <-chan struct{}
	pauseOnly   bool // package tests only; legacy matching faults still refuse
}

func (p *storageInfrastructureStart) step(name, phase string) error {
	if p.fault != nil && p.fault.name == name && p.fault.phase == phase {
		f := p.fault
		p.fault = nil
		if f.entered != nil {
			close(f.entered)
		}
		if f.release != nil {
			<-f.release
		}
		if f.pauseOnly {
			return nil
		}
		return errStorageInfrastructure
	}
	return nil
}

type storageStartupHome struct {
	self      *storageStartupHome
	start     *storageInfrastructureStart
	lease     *os.File
	identity  os.FileInfo
	exclusive bool
	// fixedZeroEligible is v2 eligibility only; nonempty legacy lock bytes stay untouched.
	fixedZeroEligible bool
}

func (s *Server) inspectInfrastructure(ctx context.Context) (_ *storageInfrastructureStart, err error) {
	origin := s.storageInfrastructureLaunch
	if ctx == nil || s.cfg.parent != nil || origin == nil || origin.self != origin || origin.server != s || origin.project != s.project || origin.configDir != s.configDir || origin.consumed || s.storageInfrastructure != nil || s.project == nil || s.root != s.project.own.root {
		return nil, errStorageInfrastructure
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	origin.consumed = true
	if !custodyChecked || s.configDir == "" || !filepath.IsAbs(s.configDir) || filepath.Clean(s.configDir) != s.configDir || len(s.configDir) > 4096 {
		return nil, storageAssistantRefusal(errNoConfigDir)
	}
	parts := splitPath(s.configDir)
	if len(parts) < 2 || len(parts) > 65 {
		return nil, storageAssistantRefusal(errStorageInfrastructure)
	}
	p := &storageInfrastructureStart{server: s, project: s.project, origin: origin, configName: parts[len(parts)-1]}
	p.self = p
	p.completed = make(map[*storageInfrastructureDirectory]bool)
	s.storageInfrastructure = p
	defer func() {
		if err != nil {
			err = errors.Join(err, p.close())
			s.storageInfrastructure = nil
		}
	}()
	p.base, err = os.OpenRoot(parts[0])
	if err != nil {
		return nil, err
	}
	p.parent = p.base
	label := parts[0]
	for _, name := range parts[1 : len(parts)-1] {
		label = filepath.Join(label, name)
		d, e := storageInspectDirectory(p.parent, name, label, false)
		if e != nil {
			return nil, storageAssistantRefusal(e)
		}
		p.ancestry = append(p.ancestry, d)
		p.parent = d.root
	}
	p.config, err = storageInspectDirectory(p.parent, p.configName, s.configDir, true)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
		p.preBudget = true
		return p, nil
	}
	if err != nil {
		return nil, storageAssistantRefusal(err)
	}
	p.preBudget, err = p.storageAbsent()
	if err != nil {
		return nil, err
	}
	p.secrets, err = storageInspectDirectory(p.config.root, secretsDirName, filepath.Join(s.configDir, secretsDirName), true)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return nil, storageAssistantRefusal(err)
	}
	return p, nil
}

func (p *storageInfrastructureStart) authority(ctx context.Context) error {
	if ctx == nil || p == nil || p.self != p || p.failed || p.server == nil || p.origin == nil || p.origin.self != p.origin || p.origin.server != p.server || p.origin.project != p.project || p.origin.configDir != p.server.configDir || !p.origin.consumed || p.server.storageInfrastructureLaunch != p.origin || p.server.storageInfrastructure != p || p.server.cfg.parent != nil || p.server.project != p.project || p.server.root != p.project.own.root {
		return errStorageInfrastructure
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, d := range p.ancestry {
		if err := d.current(false); err != nil {
			return err
		}
	}
	if p.config != nil {
		return p.config.current(false)
	}
	return nil
}
func (p *storageInfrastructureStart) storageAbsent() (bool, error) {
	if p.config == nil {
		return true, nil
	}
	absent := true
	// Presence of any name, even a foreign or partial object, forbids pre-Q.
	for _, name := range []string{storageClaimName, storageRecordName, storageReadyName, storageBootstrapName, storageUseName, storageIntentName, storageAnchorName, "storage-owner.a", "storage-owner.b", ".data-transfer.lock", storageTransferBankA, storageTransferBankB} {
		_, err := p.config.root.Lstat(name)
		if err == nil {
			absent = false
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return absent, nil
}
func (p *storageInfrastructureStart) acquireParentGate(ctx context.Context) (err error) {
	defer func() {
		if err != nil && p != nil {
			p.failed = true
		}
	}()
	if err := p.authority(ctx); err != nil {
		return err
	}
	if p.gate != nil {
		return errStorageInfrastructure
	}
	f, err := p.parent.OpenFile(".", os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return err
	}
	a, err := f.Stat()
	b, berr := p.parent.Stat(".")
	if err != nil || berr != nil || !os.SameFile(a, b) {
		return errors.Join(errStorageInfrastructure, err, berr, f.Close())
	}
	if err = lockPrivateData(f, true); err != nil {
		return errors.Join(err, f.Close())
	}
	p.gate = f
	if err = p.authority(ctx); err != nil {
		return err
	}
	if p.config == nil {
		if _, err = p.parent.Lstat(p.configName); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(errStorageInfrastructure, err)
		}
	} else {
		absent, e := p.storageAbsent()
		if e != nil || absent != p.preBudget {
			return errors.Join(errStorageInfrastructure, e)
		}
		if p.secrets != nil {
			if err = p.secrets.current(false); err != nil {
				return err
			}
		} else if _, err = p.config.root.Lstat(secretsDirName); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(errStorageInfrastructure, err)
		}
	}
	tx, err := readHomeTransaction(homeJournalPath(p.server.configDir))
	if err != nil {
		return err
	}
	if tx != nil && !tx.Complete {
		return errors.New("Restart Desk to recover its pending home migration.")
	}
	return nil
}

// reserve consumes the logical and estimated-policy allowance BEFORE the first
// effect. It never refunds an uncertain syscall. Each exact directory domain
// is sampled afresh; counters are advisory and external ENOSPC remains possible.
func (p *storageInfrastructureStart) reserve(ctx context.Context, parent *os.Root, target *storageInfrastructureDirectory, dirs, files, modes, completion uint64) error {
	if ctx == nil {
		return os.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(storageAdvisoryPassDeadlineNS))
	defer cancel()
	if err := p.authority(ctx); err != nil {
		return err
	}
	if parent != p.parent && (p.config == nil || parent != p.config.root) {
		return errStorageInfrastructure
	}
	if target != nil && target != p.config && target != p.secrets {
		found := false
		for _, d := range p.children {
			if d == target {
				found = true
			}
		}
		if !found {
			return errStorageInfrastructure
		}
	}
	if p.home != nil {
		if !p.home.exclusive {
			return errStorageInfrastructure
		}
		if err := p.home.current(ctx); err != nil {
			return err
		}
	}
	if err := p.authority(ctx); err != nil {
		return err
	}
	if !p.preBudget || (p.gate == nil && (p.home == nil || !p.home.exclusive)) {
		return errStorageInfrastructure
	}
	absent, err := p.storageAbsent()
	if err != nil || !absent {
		return errors.Join(errStorageInfrastructure, err)
	}
	if target != nil {
		if err = target.current(false); err != nil {
			return err
		}
	}
	if p.groups >= storageInfrastructureMaxGroups || p.dirs > 4 || p.files > 1 || p.modes > 2 || p.completions > 5 || dirs > 4-p.dirs || files > 1-p.files || modes > 2-p.modes || completion > 5-p.completions || dirs+files+modes+completion != 1 {
		return storagebudget.ErrBounds
	}
	estimate, err := storageAdvisoryEstimate(0, files, dirs, modes+completion)
	if err != nil {
		return err
	}
	if estimate.Bytes > storageInfrastructureMaxBytes-p.spent.Bytes || estimate.Inodes > storageInfrastructureMaxInodes-p.spent.Inodes {
		return storagebudget.ErrBounds
	}
	// Open a separate normal observation description. Never close a borrowed FD.
	f, err := parent.OpenFile(".", os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return err
	}
	a, err := f.Stat()
	b, berr := parent.Stat(".")
	if err != nil || berr != nil || !os.SameFile(a, b) {
		return errors.Join(errStorageInfrastructure, err, berr, f.Close())
	}
	// The parent itself receives metadata/persistence effects in every group.
	// A prospective child is an additional subject only for actual creation.
	facts, observeErr := storageAdvisoryReadExistingHeld(ctx, f)
	if observeErr == nil {
		observeErr = p.checkAvailability(facts, estimate)
	}
	if observeErr == nil && dirs+files > 0 {
		facts, observeErr = storageAdvisoryReadHeld(ctx, f)
		if observeErr == nil {
			observeErr = p.checkAvailability(facts, estimate)
		}
	}
	after, afterErr := f.Stat()
	named, namedErr := parent.Stat(".")
	if afterErr == nil && namedErr == nil && (!os.SameFile(a, after) || !os.SameFile(a, named)) {
		afterErr = errStorageInfrastructure
	}
	err = errors.Join(observeErr, afterErr, namedErr, f.Close())
	if err != nil {
		return err
	}
	if target != nil {
		facts, err = storageAdvisoryReadExistingHeld(ctx, target.held)
		if err != nil {
			return err
		}
		if err = p.checkAvailability(facts, estimate); err != nil {
			return err
		}
	}
	if completion == 1 && target == nil {
		if p.config == nil || parent != p.config.root || p.home == nil {
			return errStorageInfrastructure
		}
		if err = p.observeLeasePersistence(ctx, estimate); err != nil {
			return err
		}
	}
	if err = p.authority(ctx); err != nil {
		return err
	}
	if target != nil {
		if err = target.current(false); err != nil {
			return err
		}
	}
	absent, err = p.storageAbsent()
	if err != nil || !absent {
		return errors.Join(errStorageInfrastructure, err)
	}
	p.groups++
	p.dirs += dirs
	p.files += files
	p.modes += modes
	p.completions += completion
	p.spent.Bytes += estimate.Bytes
	p.spent.Inodes += estimate.Inodes
	return nil
}
func (p *storageInfrastructureStart) checkAvailability(f storageAdvisoryNativeFacts, demand storagebudget.CapacityVector) error {
	if f.status.Oldest.IsZero() || time.Since(f.status.Oldest) > time.Duration(storageAdvisoryInitialMaxAgeNS) || time.Since(f.status.Oldest) < 0 {
		return storagebudget.ErrUnknown
	}
	headroom := f.totalBytes / storageAdvisoryHeadroomDivisor
	if headroom < storageAdvisoryHeadroomMinBytes {
		headroom = storageAdvisoryHeadroomMinBytes
	}
	if headroom > storageAdvisoryHeadroomMaxBytes {
		headroom = storageAdvisoryHeadroomMaxBytes
	}
	if f.available.Bytes < headroom || p.spent.Bytes > f.available.Bytes-headroom || demand.Bytes > f.available.Bytes-headroom-p.spent.Bytes {
		return storagebudget.ErrHeadroom
	}
	available := storageAdvisoryInodePolicyCap
	if f.status.Inodes == storagePhysicalCounterMeasured {
		if f.available.Inodes < storageAdvisoryInodeHeadroomFloor {
			return storagebudget.ErrHeadroom
		}
		if n := f.available.Inodes - storageAdvisoryInodeHeadroomFloor; n < available {
			available = n
		}
	} else if f.status.Inodes != storagePhysicalCounterDynamic {
		return storagebudget.ErrUnknown
	}
	if p.spent.Inodes > available || demand.Inodes > available-p.spent.Inodes {
		return storagebudget.ErrHeadroom
	}
	return nil
}

func (p *storageInfrastructureStart) directory(ctx context.Context, parent *os.Root, name, label string, existing *storageInfrastructureDirectory, narrow bool) (*storageInfrastructureDirectory, error) {
	if err := p.authority(ctx); err != nil {
		return nil, err
	}
	if parent == p.parent {
		if name != p.configName || existing != p.config {
			return nil, errStorageInfrastructure
		}
	} else if p.config == nil || parent != p.config.root || (name != secretsDirName && name != "accounts" && name != "services") {
		return nil, errStorageInfrastructure
	}
	d := existing
	changed := false
	if d == nil {
		if err := p.reserve(ctx, parent, nil, 1, 0, 0, 0); err != nil {
			return nil, err
		}
		if _, err := parent.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Join(errStorageInfrastructure, err)
		}
		if err := parent.Mkdir(name, custodyDirMode); err != nil {
			return nil, err
		}
		var err error
		d, err = storageInspectDirectory(parent, name, label, true)
		if err != nil {
			return nil, err
		}
		// Adopt immediately, including on any later error. No remove/rollback.
		if name == p.configName && parent == p.parent {
			p.config = d
		} else if name == secretsDirName {
			p.secrets = d
		} else {
			p.children = append(p.children, d)
		}
		changed = true
		if err := p.step(name, "created"); err != nil {
			return nil, err
		}
	}
	if err := d.current(false); err != nil {
		return nil, err
	}
	info, err := d.held.Stat()
	if err != nil {
		return nil, err
	}
	if needsNarrowing(info.Mode()) {
		if !narrow {
			return nil, errStorageInfrastructure
		}
		if err = p.reserve(ctx, parent, d, 0, 0, 1, 0); err != nil {
			return nil, err
		}
		if err = d.held.Chmod(custodyDirMode); err != nil {
			return nil, err
		}
		changed = true
		if err := p.step(name, "narrowed"); err != nil {
			return nil, err
		}
	}
	if err = d.current(true); err != nil {
		return nil, err
	}
	if changed {
		if err = p.persistDirectory(ctx, d); err != nil {
			return nil, err
		}
	} else if p.preBudget && !p.completed[d] {
		if err = p.completeCurrentInfrastructurePersistence(ctx, d); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// FP1 completes current exact metadata; it neither repeats mkdir/chmod nor
// certifies an earlier launch's calls. Sync and Close each have an owner.
func (p *storageInfrastructureStart) completeCurrentInfrastructurePersistence(ctx context.Context, d *storageInfrastructureDirectory) error {
	if err := p.reserve(ctx, d.parent, d, 0, 0, 0, 1); err != nil {
		return err
	}
	return p.persistDirectory(ctx, d)
}
func (p *storageInfrastructureStart) persistDirectory(ctx context.Context, d *storageInfrastructureDirectory) error {
	if err := p.authority(ctx); err != nil {
		return err
	}
	if err := d.current(true); err != nil {
		return err
	}
	if err := p.step(d.name, "before-persistence"); err != nil {
		return err
	}
	if err := storageSyncInfrastructureDescription(ctx, d.root, ".", d.identity); err != nil {
		return err
	}
	parent, err := d.parent.Stat(".")
	if err != nil {
		return err
	}
	if err = storageSyncInfrastructureDescription(ctx, d.parent, ".", parent); err != nil {
		return err
	}
	err = errors.Join(ctx.Err(), d.current(true), p.authority(ctx), p.step(d.name, "after-persistence"))
	if err == nil {
		p.completed[d] = true
	}
	return err
}
func storageSyncInfrastructureDescription(ctx context.Context, root *os.Root, name string, identity os.FileInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := root.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return err
	}
	before, err := f.Stat()
	if err != nil || !os.SameFile(identity, before) {
		return errors.Join(errStorageInfrastructure, err, f.Close())
	}
	syncErr := f.Sync()
	after, statErr := f.Stat()
	current, nameErr := root.Lstat(name)
	if statErr == nil && nameErr == nil && (!os.SameFile(identity, after) || !os.SameFile(identity, current)) {
		statErr = errStorageInfrastructure
	}
	return errors.Join(syncErr, statErr, nameErr, f.Close(), ctx.Err())
}

func (p *storageInfrastructureStart) provisionConfigAndAcquireHome(ctx context.Context) (_ *storageStartupHome, err error) {
	defer func() {
		if err != nil && p != nil {
			p.failed = true
		}
	}()
	if p.gate == nil || p.home != nil {
		return nil, errStorageInfrastructure
	}
	if err := p.authority(ctx); err != nil {
		return nil, err
	}
	if p.config == nil {
		if _, err := p.directory(ctx, p.parent, p.configName, p.server.configDir, nil, true); err != nil {
			return nil, err
		}
	}
	lease, err := openHomeLeaseExistingRoot(p.config.root, true)
	if errors.Is(err, os.ErrNotExist) {
		if !p.preBudget {
			return nil, errStorageInfrastructure
		}
		// Existing config completion/narrowing precedes lease-dependent growth.
		if _, err = p.directory(ctx, p.parent, p.configName, p.server.configDir, p.config, true); err != nil {
			return nil, err
		}
		if err = p.reserve(ctx, p.config.root, nil, 0, 1, 0, 0); err != nil {
			return nil, err
		}
		lease, err = p.config.root.OpenFile(".home-use.lock", os.O_CREATE|os.O_EXCL|os.O_RDWR|openNoFollow|openNonBlocking, custodyFileMode)
		if err != nil {
			return nil, err
		}
		info, e := lease.Stat()
		if e != nil || !storagePrivate(info, false) || info.Size() != 0 {
			return nil, errors.Join(errStorageInfrastructure, e, lease.Close())
		}
		if err = lockPrivateData(lease, true); err != nil {
			return nil, errors.Join(err, lease.Close())
		}
		err = p.adoptHome(lease, info)
		if err != nil {
			return nil, errors.Join(err, lease.Close())
		}
		if err = p.persistLease(ctx); err != nil {
			return nil, err
		}
	} else if err == nil {
		info, e := lease.Stat()
		if e != nil {
			return nil, errors.Join(e, lease.Close())
		}
		if err = p.adoptHome(lease, info); err != nil {
			return nil, errors.Join(err, lease.Close())
		}
		if _, err = p.directory(ctx, p.parent, p.configName, p.server.configDir, p.config, true); err != nil {
			return nil, err
		}
		// Legacy-only witnesses, including empty hardlinked leases, are not
		// completion targets. Keep their original bytes/names for explicit v2
		// eligibility refusal; size alone cannot issue FP1 completion authority.
		if p.preBudget && p.home.fixedZeroEligible {
			if err = p.home.requireFixedZero(ctx); err != nil {
				return nil, err
			}
			if err = p.reserve(ctx, p.config.root, nil, 0, 0, 0, 1); err != nil {
				return nil, err
			}
			if err = p.persistLease(ctx); err != nil {
				return nil, err
			}
		}
	} else {
		return nil, err
	}
	if err = p.home.current(ctx); err != nil {
		return nil, err
	}
	gate := p.gate
	p.gate = nil
	if err = gate.Close(); err != nil {
		return nil, err
	}
	return p.home, nil
}
func (p *storageInfrastructureStart) adoptHome(lease *os.File, info os.FileInfo) error {
	h := &storageStartupHome{start: p, lease: lease, identity: info, exclusive: true, fixedZeroEligible: info.Size() == 0 && storagePrivate(info, false)}
	h.self = h
	if p.home != nil || p.server.homeLease != nil {
		return errStorageInfrastructure
	}
	p.home = h
	p.server.homeLease = lease
	if err := h.current(context.Background()); err != nil {
		// Adoption did not complete: the caller still owns and closes this File.
		p.home = nil
		p.server.homeLease = nil
		return err
	}
	return nil
}
func (h *storageStartupHome) current(ctx context.Context) error {
	if h == nil || h.self != h || h.start == nil || h.start.home != h || h.start.server.homeLease != h.lease {
		return errStorageInfrastructure
	}
	if err := h.start.authority(ctx); err != nil {
		return err
	}
	return validateHomeLease(h.start.config.root, h.lease, h.identity)
}

// requireFixedZero is also the explicit downstream v2 eligibility gate.
// Legacy home-lock custody can remain valid while this stricter witness fails.
func (h *storageStartupHome) requireFixedZero(ctx context.Context) error {
	if err := h.current(ctx); err != nil {
		return err
	}
	if !h.fixedZeroEligible {
		return errStorageInfrastructure
	}
	before, err := h.start.config.root.Lstat(".home-use.lock")
	if err != nil {
		return err
	}
	held, err := h.lease.Stat()
	if err != nil {
		return err
	}
	after, err := h.start.config.root.Lstat(".home-use.lock")
	if err != nil {
		return err
	}
	for _, info := range []os.FileInfo{before, held, after} {
		if info.Size() != 0 || !storagePrivate(info, false) || !os.SameFile(h.identity, info) {
			return errStorageInfrastructure
		}
	}
	return nil
}

// Borrow only this sealed home's actual lease and exact config directory.
// Directory counters supply availability; the separate regular-file metadata
// supplies its existing quota subjects and must join that exact native scope.
func (p *storageInfrastructureStart) observeLeasePersistence(ctx context.Context, demand storagebudget.CapacityVector) error {
	if err := p.home.requireFixedZero(ctx); err != nil {
		return err
	}
	facts, err := storageAdvisoryReadExistingLeaseHeld(ctx, p.config.held, p.home.lease)
	if err != nil {
		return err
	}
	if err = p.home.requireFixedZero(ctx); err != nil {
		return err
	}
	return p.checkAvailability(facts, demand)
}
func (p *storageInfrastructureStart) persistLease(ctx context.Context) error {
	if ctx == nil {
		return os.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(storageAdvisoryPassDeadlineNS))
	defer cancel()
	// The group's allowance is already spent; refresh actual lease mapping for
	// persistence without issuing/refunding another completion group.
	if err := p.observeLeasePersistence(ctx, storagebudget.CapacityVector{}); err != nil {
		return err
	}
	if err := p.home.requireFixedZero(ctx); err != nil {
		return err
	}
	if err := storageSyncInfrastructureDescription(ctx, p.config.root, ".home-use.lock", p.home.identity); err != nil {
		return err
	}
	if err := p.home.requireFixedZero(ctx); err != nil {
		return err
	}
	if err := storageSyncInfrastructureDescription(ctx, p.config.root, ".", p.config.identity); err != nil {
		return err
	}
	return p.home.requireFixedZero(ctx)
}
func (p *storageInfrastructureStart) provisionAssistantStore(ctx context.Context, h *storageStartupHome) (_ *assistantStore, err error) {
	defer func() {
		if err != nil && p != nil {
			p.failed = true
		}
	}()
	if h != p.home || !h.exclusive || p.promoted {
		return nil, errStorageInfrastructure
	}
	if err := h.current(ctx); err != nil {
		return nil, err
	}
	if _, err := p.directory(ctx, p.config.root, secretsDirName, filepath.Join(p.server.configDir, secretsDirName), p.secrets, true); err != nil {
		return nil, err
	}
	// Duplicate independent Root ownership for the store. The launch retains its
	// own witnesses for currentness until Server.Close, and closes only its own.
	root, err := p.config.root.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	secrets, err := p.secrets.root.OpenRoot(".")
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	if err = h.current(ctx); err != nil {
		return nil, errors.Join(err, root.Close(), secrets.Close())
	}
	p.promoted = true
	return &assistantStore{dir: p.server.configDir, root: root, secrets: secrets}, nil
}
func (p *storageInfrastructureStart) provisionHomeServices(ctx context.Context, h *storageStartupHome) (err error) {
	defer func() {
		if err != nil && p != nil {
			p.failed = true
		}
	}()
	if h != p.home || !h.exclusive || !p.promoted {
		return errStorageInfrastructure
	}
	if err := h.current(ctx); err != nil {
		return err
	}
	for _, name := range []string{"accounts", "services"} {
		d, err := storageInspectDirectory(p.config.root, name, filepath.Join(p.server.configDir, name), true)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if d != nil {
			p.children = append(p.children, d)
		}
		if _, err = p.directory(ctx, p.config.root, name, filepath.Join(p.server.configDir, name), d, false); err != nil {
			return err
		}
	}
	return nil
}

// Phase4's original-owner/Q enrollment runs before this exact same-FD transfer.
// No SH/EX release/reacquire gap is introduced by this startup split.
func (s *Server) finishInfrastructureStartup(ctx context.Context) error {
	p := s.storageInfrastructure
	if p == nil || p.home == nil {
		return nil
	}
	if err := p.home.current(ctx); err != nil {
		return err
	}
	if !p.home.exclusive {
		return errStorageInfrastructure
	}
	if err := lockPrivateData(p.home.lease, false); err != nil {
		return err
	}
	p.home.exclusive = false
	return p.home.current(ctx)
}
func (p *storageInfrastructureStart) close() (err error) {
	if p == nil {
		return nil
	}
	if p.gate != nil {
		err = errors.Join(err, p.gate.Close())
		p.gate = nil
	}
	for i := len(p.children) - 1; i >= 0; i-- {
		err = errors.Join(err, p.children[i].close())
	}
	if p.secrets != nil {
		err = errors.Join(err, p.secrets.close())
	}
	if p.config != nil {
		err = errors.Join(err, p.config.close())
	}
	for i := len(p.ancestry) - 1; i >= 0; i-- {
		err = errors.Join(err, p.ancestry[i].close())
	}
	if p.base != nil {
		err = errors.Join(err, p.base.Close())
	}
	p.children = nil
	p.secrets = nil
	p.config = nil
	p.ancestry = nil
	p.base = nil
	return err
}

// A child borrows completed original infrastructure; it may obtain independent
// read-only handles but cannot issue a fresh launch provision or lease create.
func (s *Server) borrowInfrastructure() (*assistantStore, error) {
	parent := s.cfg.parent
	if parent == nil || parent.assistant == nil {
		return nil, errStorageInfrastructure
	}
	if !parent.assistant.usable() {
		return &assistantStore{dir: s.configDir, problem: parent.assistant.problem}, nil
	}
	if s.configDir != parent.configDir || parent.storageInfrastructure == nil || parent.storageInfrastructure.home == nil || parent.storageInfrastructure.home.exclusive {
		return nil, errStorageInfrastructure
	}
	if err := parent.storageInfrastructure.home.current(context.Background()); err != nil {
		return nil, err
	}
	root, err := parent.assistant.root.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	secrets, err := parent.assistant.secrets.OpenRoot(".")
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	return &assistantStore{dir: s.configDir, root: root, secrets: secrets}, nil
}

func storageInfrastructureProblem(err error) (*assistantStore, bool) {
	var refusal *storageInfrastructureAssistantError
	if !errors.As(err, &refusal) {
		return nil, false
	}
	return &assistantStore{problem: fmt.Errorf("assistant custody: %w", err)}, true
}
