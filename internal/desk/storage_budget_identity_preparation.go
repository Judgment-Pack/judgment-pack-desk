package desk

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Unselected no-effect refinement of the original startup session. This cannot
// grant an action, choose a format, issue an effect, or enter a Q workflow.
// Future callers must acquire project/signing before work/domain holds; M3-1
// deliberately leaves the existing census/hold sequence untouched.
type storageIdentityPreparation struct {
	self             *storageIdentityPreparation
	mu               sync.Mutex
	construction     *storageBudgetConstruction
	authority        *storageMutationAuthority
	generation       uint64
	project, signing *storagePrefixLease
	closing, closed  bool
	closeErr         error
}

func (c *storageBudgetConstruction) beginStorageIdentityPreparation(ctx context.Context) (*storageIdentityPreparation, error) {
	if c == nil || c.owner == nil || c.startupUse != nil || c.startupWork != nil || len(c.startupHolds) != 0 {
		return nil, storagebudget.ErrAuthority
	}
	if err := c.mutation.current(ctx, true); err != nil {
		return nil, err
	}
	if err := c.owner.retain(); err != nil {
		return nil, err
	}
	g := &c.owner.admission
	g.mu.Lock()
	if g.phase != storageAdmissionStartup || g.startup != c.mutation || g.preparation != nil || g.shared != 0 {
		g.mu.Unlock()
		return nil, errors.Join(storagebudget.ErrBusy, c.owner.drop())
	}
	p := &storageIdentityPreparation{construction: c, authority: c.mutation, generation: g.generation}
	p.self = p
	g.preparation = p
	g.mu.Unlock()
	return p, nil
}

func (p *storageIdentityPreparation) current(ctx context.Context) error {
	if p == nil || p.self != p || p.construction == nil || p.authority == nil {
		return storagebudget.ErrAuthority
	}
	p.mu.Lock()
	closed, prior := p.closing || p.closed, p.closeErr
	p.mu.Unlock()
	if prior != nil {
		return prior
	}
	if closed {
		return os.ErrClosed
	}
	if err := p.authority.current(ctx, true); err != nil {
		p.mu.Lock()
		p.closeErr = errors.Join(p.closeErr, err)
		p.mu.Unlock()
		return err
	}
	g := &p.construction.owner.admission
	g.mu.Lock()
	ok := g.preparation == p && g.phase == storageAdmissionStartup && g.startup == p.authority && g.generation == p.generation
	g.mu.Unlock()
	if !ok {
		return storagebudget.ErrAuthority
	}
	return nil
}

func (p *storageIdentityPreparation) Close() error {
	if p == nil || p.self != p {
		return storagebudget.ErrAuthority
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return p.closeErr
	}
	p.closing = true
	// No new borrower can enter after closing. Busy retains both preparation
	// and underlying prefix descriptions until a subsequent final drain.
	for _, l := range []*storagePrefixLease{p.signing, p.project} {
		if l != nil {
			err := l.closeWithPreparationHeld()
			if errors.Is(err, storagebudget.ErrBusy) {
				// Earlier prefixes stay held until this later borrower drains.
				return errors.Join(err, p.closeErr)
			}
			p.closeErr = errors.Join(p.closeErr, err)
		}
	}
	p.closed = true
	o := p.construction.owner
	g := &o.admission
	g.mu.Lock()
	if g.preparation == p {
		g.preparation = nil
	}
	g.mu.Unlock()
	// A Close error invalidates the startup session; it cannot yield a ticket.
	if p.closeErr != nil {
		p.closeErr = errors.Join(p.closeErr, p.authority.Close())
	}
	p.closeErr = errors.Join(p.closeErr, o.drop())
	return p.closeErr
}

// The exact locked description is owned once by this sealed lease. Borrowers
// never open another description to prove aliasing and never close this one.
type storagePrefixLease struct {
	self            *storagePrefixLease
	mu              sync.Mutex
	preparation     *storageIdentityPreparation
	owner           *storageBudgetOwner
	generation      uint64
	root            *os.Root
	file            *os.File
	info            os.FileInfo
	persistent      storageOwnerPersistentObject
	signing         *signingDir // nonnil only when opened by exact original assistant
	borrowers       uint64
	borrows         map[*storagePrefixBorrow]bool
	closing, closed bool
	closeErr        error
}

type storagePrefixBorrow struct {
	self   *storagePrefixBorrow
	lease  *storagePrefixLease
	root   *storageRootBinding
	closed bool // lease.mu
}

func (p *storageIdentityPreparation) acquirePrefix(ctx context.Context, signing bool) (_ *storagePrefixLease, err error) {
	if err = p.current(ctx); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	defer func() {
		if err != nil {
			p.closeErr = errors.Join(p.closeErr, err)
		}
	}()
	if p.closing || p.closed || !signing && p.project != nil || signing && (p.project == nil || p.signing != nil) {
		return nil, storagebudget.ErrAuthority
	}
	c := p.construction
	if c.startupUse != nil || c.startupWork != nil || len(c.startupHolds) != 0 {
		return nil, storagebudget.ErrAuthority
	}
	l := &storagePrefixLease{preparation: p, owner: c.owner, generation: p.generation, root: c.server.root}
	l.self = l
	if signing {
		l.signing, err = c.assistant.openSigning(false)
		if err != nil {
			return nil, err
		}
		l.root = l.signing.root
	}
	defer func() {
		if err != nil {
			if l.file != nil {
				err = errors.Join(err, l.file.Close())
			}
			if l.signing != nil {
				err = errors.Join(err, l.root.Close())
			}
		}
	}()
	l.file, err = l.root.Open(".")
	if err != nil {
		return nil, err
	}
	if signing {
		err = lockSigningFile(l.file)
	} else {
		err = lockProjectFile(l.file)
	}
	if err != nil {
		return nil, err
	}
	l.info, err = l.file.Stat()
	if err != nil {
		return nil, err
	}
	l.persistent, err = storageAdvisoryPersistentHeld(ctx, l.file)
	if err != nil {
		return nil, err
	}
	// Validate ancestry/held/native together before installing the only owner.
	if err = l.originalCurrent(ctx); err != nil {
		return nil, err
	}
	g := &c.owner.admission
	g.mu.Lock()
	if g.phase != storageAdmissionStartup || g.preparation != p || g.generation != l.generation || g.prefixes == ^uint64(0) {
		g.mu.Unlock()
		return nil, storagebudget.ErrAuthority
	}
	g.prefixes++
	g.mu.Unlock()
	if signing {
		p.signing = l
	} else {
		p.project = l
	}
	return l, nil
}

func (l *storagePrefixLease) originalCurrent(ctx context.Context) error {
	c := l.preparation.construction
	if err := l.preparation.authority.current(ctx, true); err != nil {
		return err
	}
	if l.owner != c.owner || l.generation != l.preparation.generation {
		return storagebudget.ErrAuthority
	}
	if l.signing == nil {
		if l.root != c.server.root {
			return storagebudget.ErrAuthority
		}
	} else {
		// Join the held signing root to both original retained parents and names.
		secrets, err := c.assistant.root.Lstat(secretsDirName)
		heldSecrets, he := c.assistant.secrets.Stat(".")
		named, ne := c.assistant.secrets.Lstat(signingDirName)
		if err != nil || he != nil || ne != nil || !os.SameFile(secrets, heldSecrets) || !os.SameFile(named, l.info) || l.root != l.signing.root {
			return errors.Join(errStorageCustody, err, he, ne)
		}
		if err := l.signing.namesHeld(); err != nil {
			return err
		}
	}
	held, err := l.file.Stat()
	named, ne := l.root.Stat(".")
	if err != nil || ne != nil || !os.SameFile(held, l.info) || !os.SameFile(held, named) || held.Mode() != l.info.Mode() || named.Mode() != l.info.Mode() {
		return errors.Join(errStorageCustody, err, ne)
	}
	native, err := storageAdvisoryPersistentHeld(ctx, l.file)
	if err != nil || native != l.persistent {
		return errors.Join(errStorageCustody, err)
	}
	return nil
}

func (l *storagePrefixLease) current(ctx context.Context) error {
	if l == nil || l.self != l || l.preparation == nil {
		return storagebudget.ErrAuthority
	}
	if err := l.preparation.current(ctx); err != nil {
		return err
	}
	p := l.preparation
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.project != l && p.signing != l {
		return storagebudget.ErrAuthority
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closing || l.closed {
		return os.ErrClosed
	}
	if l.closeErr != nil {
		return l.closeErr
	}
	if err := l.originalCurrent(ctx); err != nil {
		l.closeErr = errors.Join(l.closeErr, err)
		return err
	}
	return nil
}

func (l *storagePrefixLease) borrow(ctx context.Context, r *storageRootBinding) (*storagePrefixBorrow, error) {
	if err := l.current(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.owner != l.owner || l.owner.roots[r.row.ID] != r || !r.domainDirectory {
		return nil, storagebudget.ErrAuthority
	}
	if err := r.current(ctx); err != nil {
		return nil, err
	}
	p := l.preparation
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing || p.closed || p.project != l && p.signing != l {
		return nil, storagebudget.ErrAuthority
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closing || l.closed || l.borrowers == ^uint64(0) {
		return nil, storagebudget.ErrBusy
	}
	if err := l.aliasCurrent(ctx, r); err != nil {
		return nil, err
	}
	b := &storagePrefixBorrow{lease: l, root: r}
	b.self = b
	if l.borrows == nil {
		l.borrows = map[*storagePrefixBorrow]bool{}
	}
	l.borrows[b] = true
	l.borrowers++
	return b, nil
}

func (b *storagePrefixBorrow) current(ctx context.Context) error {
	if b == nil || b.self != b || b.lease == nil || b.root == nil {
		return storagebudget.ErrAuthority
	}
	l := b.lease
	if err := l.current(ctx); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if b.closed || !l.borrows[b] || l.closed || l.closing {
		return os.ErrClosed
	}
	if err := b.root.current(ctx); err != nil {
		l.closeErr = errors.Join(l.closeErr, err)
		return err
	}
	if err := l.aliasCurrent(ctx, b.root); err != nil {
		l.closeErr = errors.Join(l.closeErr, err)
		return err
	}
	if err := l.originalCurrent(ctx); err != nil {
		l.closeErr = errors.Join(l.closeErr, err)
		return err
	}
	return nil
}

func (l *storagePrefixLease) aliasCurrent(ctx context.Context, r *storageRootBinding) error {
	if r == nil || r.owner != l.owner || l.owner.roots[r.row.ID] != r || !r.domainDirectory {
		return storagebudget.ErrAuthority
	}
	native, err := storageAdvisoryPersistentHeld(ctx, r.domain.file)
	held, he := r.domain.file.Stat()
	if err != nil || he != nil || native != l.persistent || !os.SameFile(held, l.info) {
		return errors.Join(storagebudget.ErrAuthority, err, he)
	}
	return nil
}

func (b *storagePrefixBorrow) Close() error {
	if b == nil || b.self != b || b.lease == nil {
		return storagebudget.ErrAuthority
	}
	l := b.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	if b.closed {
		return nil
	}
	if l.borrowers == 0 || !l.borrows[b] {
		return storagebudget.ErrAuthority
	}
	b.closed = true
	delete(l.borrows, b)
	l.borrowers--
	return nil
}

func (l *storagePrefixLease) Close() error {
	if l == nil || l.self != l || l.preparation == nil {
		return storagebudget.ErrAuthority
	}
	p := l.preparation
	p.mu.Lock()
	defer p.mu.Unlock()
	return l.closeWithPreparationHeld()
}

func (l *storagePrefixLease) closeWithPreparationHeld() error {
	if l == nil || l.self != l || l.owner == nil || l.preparation == nil || l.preparation.project != l && l.preparation.signing != l {
		return storagebudget.ErrAuthority
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return l.closeErr
	}
	l.closing = true
	if l.borrowers != 0 {
		return storagebudget.ErrBusy
	}
	l.closed = true
	l.closeErr = errors.Join(l.closeErr, l.file.Close())
	if l.signing != nil {
		l.closeErr = errors.Join(l.closeErr, l.root.Close())
	}
	g := &l.owner.admission
	g.mu.Lock()
	if g.prefixes == 0 {
		l.closeErr = errors.Join(l.closeErr, storagebudget.ErrAuthority)
	} else {
		g.prefixes--
	}
	g.mu.Unlock()
	return l.closeErr
}

// Unselected domain refinement; existing census/domain acquisition order does
// not call this helper. Preserve maintenance's prior-description dedup first.
func (p *storageIdentityPreparation) domain(ctx context.Context, r *storageRootBinding, u *storageMaintenanceUse) (*storageDomainHold, error) {
	if err := p.current(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.owner != p.construction.owner || p.construction.owner.roots[r.row.ID] != r || u == nil || u != p.construction.startupUse || u.storageCell() != p.construction.ref.cell {
		return nil, storagebudget.ErrAuthority
	}
	if err := u.Current(); err != nil {
		return nil, err
	}
	if r.qControl {
		return p.construction.owner.domain(ctx, r, u)
	}
	id, err := storageFileIdentity(r.domain.identity)
	if err != nil {
		return nil, err
	}
	if u.domains != nil && u.domains[id] != nil {
		d := &storageDomainHold{owner: p.construction.owner, root: r, lifetime: u, borrowed: u.domains[id]}
		d.self = d
		return d, d.current(ctx)
	}
	for _, l := range []*storagePrefixLease{p.project, p.signing} {
		if l == nil || !r.domainDirectory {
			continue
		}
		native, err := storageAdvisoryPersistentHeld(ctx, r.domain.file)
		if err != nil {
			return nil, err
		}
		if native != l.persistent {
			continue
		}
		b, err := l.borrow(ctx, r)
		if err != nil {
			return nil, err
		}
		d := &storageDomainHold{owner: p.construction.owner, root: r, lifetime: u, prefix: b}
		d.self = d
		if u.domains == nil {
			u.domains = map[storageDirectoryIdentity]*storageDomainHold{}
		}
		u.domains[id] = d
		return d, nil
	}
	return p.construction.owner.domain(ctx, r, u)
}
