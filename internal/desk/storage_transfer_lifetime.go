package desk

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// storage_transfer_lifetime.go — distinct closed transfer role.
// One nonblocking process permit bounds retained restore input across all
// Servers and machine owners. There is no queue of archive buffers.
var chatTransferProcessPermit = make(chan struct{}, 1)

type chatTransferLifetimes struct {
	mu      sync.Mutex
	owner   *storageBudgetOwner
	current *chatTransferPreparation
	closing bool
	err     error
}

type chatTransferPreparation struct {
	self                                           *chatTransferPreparation
	registry                                       *chatTransferLifetimes
	server                                         *Server
	request                                        *http.Request
	mode                                           storageTransferMode
	targetPath                                     string // canonical handler data; early capture grants no mutation
	ctx                                            context.Context
	cancel                                         context.CancelFunc
	stopCancel                                     func() bool
	cancelDone                                     chan struct{}
	done                                           chan struct{}
	mu                                             sync.Mutex
	revoked, finishing, finished, permit, retained bool
	admitted, completed                            bool // exact original executor; epoch cannot reopen across debt
	err                                            error
	body                                           io.ReadCloser
	bodyOnce                                       sync.Once
	bodyErr                                        error
	epoch                                          *storageEnrollmentEpoch
	operation                                      *chatTransferOperation
}

// Every mutable step/serving entry uses this operation's barrier. Its lexical
// owner alone runs completion and finalization. Close only revokes and joins;
// it never closes a live borrower's descriptions or supplies another executor.
type chatTransferOperation struct {
	self                                      *chatTransferOperation
	preparation                               *chatTransferPreparation
	mu                                        sync.Mutex
	record                                    storageTransferRecord
	returned                                  storageTransferRecord // original returned observations; never overwrite persisted body early
	created                                   []storageNamedRoot
	stage                                     *storageNamedFile
	oldPointer                                *storageNamedFile
	oldPointerClosed                          bool
	oldPointerErr                             error
	input                                     []byte
	source                                    *chatDataStore
	response                                  *chatTransferExportResponse
	retiredNamespace                          storagebudget.ID
	retiredNamespacePage                      storagebudget.Digest
	retiredNamespaceSource                    []byte
	tailAdmitted, tailConsumed, tailCompleted bool
	destinationEnrolled                       bool
	appliedProof                              *storagebudget.HostState
	board                                     []storageTransferOrdinal
	effectFiles                               map[int]storageNamedFile
	// Captured and detached once under the original epoch, before its first
	// control write. These are the exact operands used by every later leaf.
	catalog                       storageOwnerCatalogV2
	request                       storagebudget.Request
	fullBanks                     [2][]byte
	bankNative                    [2]storageOwnerPersistentObject
	host                          storageIntentSlot
	controlOriginal               *storageEnrollmentPeakOriginalComparison
	controlConsumed               [storageTransferControlGroups]bool
	reserveReturned               bool
	ticketReads                   [3]bool
	reserveError                  error
	rejectedProof                 *storagebudget.HostState
	namespaceDependency           *storageEnrollmentPeakSource
	entered, completing, finished bool
	enteredDone                   chan struct{}
	next                          int
	results                       []byte
	private                       *chatTransferPrivateBorrow
	completion                    *chatTransferCompletion
	completionContext             context.Context
	capacityAt                    time.Time
	capacityChecks                []bool
	observation                   *storageTransferCapacityPass
	path                          *storageTransferPathWitness
	settings                      *os.Root // actual original owner callback; never a caller-supplied FD
	err                           error
}

func (s *Server) prepareOriginalChatTransfer(r *http.Request, mode storageTransferMode) (_ *chatTransferPreparation, err error) {
	if s == nil || r == nil || r.Context().Err() != nil || s.storageEnrollment == nil || mode < storageTransferMove || mode > storageTransferExport {
		return nil, storagebudget.ErrAuthority
	}
	e := s.storageEnrollment
	e.mu.Lock()
	c := e.construction
	valid := e.self == e && !e.closed && e.server == s && c != nil && c.owner != nil
	e.mu.Unlock()
	if !valid {
		return nil, storagebudget.ErrAuthority
	}
	o := c.owner
	if err = o.retain(); err != nil {
		return nil, err
	}
	p := &chatTransferPreparation{server: s, request: r, mode: mode, registry: o.transfers, done: make(chan struct{}), cancelDone: make(chan struct{}), retained: true}
	p.self = p
	if p.registry == nil {
		return nil, errors.Join(storagebudget.ErrAuthority, o.drop())
	}
	p.ctx, p.cancel = context.WithCancel(r.Context())
	l := p.registry
	l.mu.Lock()
	if l.owner != o || l.closing || s.storageTransfersClosing || l.current != nil {
		l.mu.Unlock()
		p.cancel()
		return nil, errors.Join(storagebudget.ErrBusy, o.drop())
	}
	select {
	case chatTransferProcessPermit <- struct{}{}:
		p.permit = true
	default:
		l.mu.Unlock()
		p.cancel()
		return nil, errors.Join(storagebudget.ErrBusy, o.drop())
	}
	l.current = p
	l.mu.Unlock()
	p.stopCancel = context.AfterFunc(p.ctx, func() { defer close(p.cancelDone); p.revoke() })
	if err = p.ctx.Err(); err != nil {
		return nil, p.finish(err)
	}
	return p, nil
}

func (p *chatTransferPreparation) closeBody() error {
	p.bodyOnce.Do(func() {
		p.mu.Lock()
		body := p.body
		p.mu.Unlock()
		if body != nil {
			err := body.Close()
			p.mu.Lock()
			p.bodyErr = err
			p.mu.Unlock()
		}
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.bodyErr
}

func (p *chatTransferPreparation) revoke() {
	if p == nil || p.self != p {
		return
	}
	p.mu.Lock()
	p.revoked = true
	body := p.body
	p.mu.Unlock()
	if body != nil {
		_ = p.closeBody()
	}
}

// Attach only the original request body, before retention and before native
// prefixes. Cancellation closes this same reader; caller data cannot supply a
// replacement body, an input stage or a borrowed native description.
func (p *chatTransferPreparation) attachRestoreBody() error {
	if p == nil || p.self != p || p.mode != storageTransferRestore {
		return storagebudget.ErrAuthority
	}
	p.mu.Lock()
	if p.finishing || p.finished || p.body != nil || p.epoch != nil || p.request == nil || p.request.Body == nil {
		p.mu.Unlock()
		return storagebudget.ErrAuthority
	}
	p.body = p.request.Body
	revoked := p.revoked
	p.mu.Unlock()
	// Cancellation may have fired before attachment. Register this exact body
	// first, then consume its checked Close without entering a network read.
	if err := p.ctx.Err(); err != nil || revoked {
		return errors.Join(os.ErrClosed, err, p.closeBody())
	}
	if err := p.current(); err != nil {
		return errors.Join(err, p.closeBody())
	}
	return nil
}

func (p *chatTransferPreparation) current() error {
	if p == nil || p.self != p || p.registry == nil {
		return storagebudget.ErrAuthority
	}
	p.registry.mu.Lock()
	registered := p.registry.current == p && !p.registry.closing && !p.server.storageTransfersClosing
	p.registry.mu.Unlock()
	p.mu.Lock()
	live := !p.revoked && !p.finishing && !p.finished && p.permit
	p.mu.Unlock()
	if !registered || !live {
		return os.ErrClosed
	}
	return p.ctx.Err()
}

// Transfer this preparation's original retain directly into the existing
// enrollment epoch. There is no zero-retain handoff, SH-to-EX upgrade under
// private/work, fresh requester, or replacement owner. Generic enrollment's
// active==0 constructor is unchanged. This route accepts exactly active==1,
// and that one retain must belong to this still-registered preparation.
func (p *chatTransferPreparation) beginOriginalEnrollment() (*storageEnrollmentEpoch, error) {
	if err := p.current(); err != nil {
		return nil, err
	}
	l := p.registry
	o := l.owner
	c := p.server.storageEnrollment.construction
	o.mu.Lock()
	g := &o.admission
	g.mu.Lock()
	c.mu.Lock()
	l.mu.Lock()
	p.mu.Lock()
	valid := l.current == p && !l.closing && !p.server.storageTransfersClosing && p.retained && !p.revoked && !p.finishing && !p.finished && p.ctx.Err() == nil && p.epoch == nil && p.operation == nil &&
		!o.closing && !o.closed && !o.transitionRequired && o.active == 1 && o.currentCensus == nil && !c.closing && !c.closed && c.owner == o && c.home != nil && !c.home.exclusive &&
		g.phase == storageAdmissionOpen && g.shared == 0 && g.prefixes == 0 && g.preparation == nil && g.exclusiveCensus == nil && g.enrollment == nil && g.generation != ^uint64(0) && c.startupUse == nil && c.startupWork == nil && len(c.startupHolds) == 0
	if !valid {
		p.mu.Unlock()
		l.mu.Unlock()
		c.mu.Unlock()
		g.mu.Unlock()
		o.mu.Unlock()
		return nil, storagebudget.ErrBusy
	}
	g.generation++
	ctx, cancel := context.WithCancel(p.ctx)
	epoch := &storageEnrollmentEpoch{measurementCaller: p.request.Context(), c: c, generation: g.generation, ctx: ctx, cancel: cancel, running: true, done: make(chan struct{}), transfer: p}
	epoch.self = epoch
	g.phase, g.enrollment = storageAdmissionEnrollment, epoch
	p.epoch, p.retained = epoch, false
	p.mu.Unlock()
	l.mu.Unlock()
	c.mu.Unlock()
	g.mu.Unlock()
	o.mu.Unlock()
	epoch.stopCancel = context.AfterFunc(ctx, epoch.revoke)
	if err := epoch.selectOriginalRequester(ctx, p.server); err != nil {
		return nil, err
	}
	return epoch, nil
}

// This function is the writes/private lexical owner. The callback gets only
// its sealed operation; it cannot manufacture writes ownership with a flag,
// expose a raw borrowed handle or return a live borrower. Responses belong to
// the canonical handlers after this function joins the original finalization.
func (s *Server) withOriginalChatTransfer(p *chatTransferPreparation, run func(*chatTransferOperation) error) (err error) {
	if p == nil || p.self != p || p.server != s || run == nil {
		return storagebudget.ErrAuthority
	}
	defer func() { err = p.finish(err) }()
	epoch, err := p.beginOriginalEnrollment()
	if err != nil {
		return err
	}
	if err = epoch.acquire(p.ctx); err != nil {
		return err
	}
	s.writes.Lock()
	// The owning invocation unlocks after actual private/domain Close. Neither
	// the revoker nor the Server Close join ever needs this mutex.
	defer s.writes.Unlock()
	lock, err := p.originalPrivateTurn()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	defer func() { err = errors.Join(err, epoch.closeOriginalTransferDomains()) }()
	op := &chatTransferOperation{preparation: p}
	op.self = op
	registered := false
	defer func() {
		if !registered {
			err = errors.Join(err, op.closeOriginalTransferData())
		}
	}()
	if _, err = op.borrowOriginalPrivate(lock); err != nil {
		return err
	}
	if p.targetPath != "" {
		op.path, err = op.capturePreparedTransferPath(p.targetPath)
		if err != nil {
			return err
		}
	}
	if err = epoch.borrowOriginalTransferDomains(lock, op.path); err != nil {
		return err
	}
	p.mu.Lock()
	if p.revoked || p.finishing || p.finished || p.operation != nil || p.ctx.Err() != nil {
		p.mu.Unlock()
		return os.ErrClosed
	}
	p.operation = op
	registered = true
	p.mu.Unlock()
	// Every callback exit, including cancellation, owns exactly this original
	// operation's finite completion. An external Close never calls it.
	defer func() {
		op.mu.Lock()
		if op.entered {
			done := op.enteredDone
			op.mu.Unlock()
			if done == nil {
				err = errors.Join(err, storagebudget.ErrAuthority)
				return
			}
			<-done
			op.mu.Lock()
		}
		op.mu.Unlock()
		callbackErr := err
		err = errors.Join(callbackErr, op.completeObserved(callbackErr), op.closeOriginalTransferData())
		op.mu.Lock()
		if p.admitted && !p.completed {
			err = errors.Join(err, errStorageEnrollmentUnresolved)
		}
		op.finished = true
		op.err = errors.Join(op.err, err)
		err = op.err
		op.mu.Unlock()
		p.mu.Lock()
		if p.operation == op {
			p.operation = nil
		} else {
			err = errors.Join(err, storagebudget.ErrAuthority)
		}
		p.mu.Unlock()
	}()
	if err = epoch.captureExistingOriginal(p.ctx); err != nil {
		return err
	}
	if err = epoch.qualifyOriginalNative(p.ctx); err != nil {
		return err
	}
	return run(op)
}

func (p *chatTransferPreparation) finish(err error) error {
	if p == nil || p.self != p {
		return errors.Join(storagebudget.ErrAuthority, err)
	}
	p.mu.Lock()
	if p.finished {
		result := errors.Join(p.err, err)
		p.mu.Unlock()
		return result
	}
	if p.finishing || p.operation != nil {
		p.mu.Unlock()
		return errors.Join(storagebudget.ErrBusy, err)
	}
	p.finishing, p.revoked = true, true
	p.mu.Unlock()
	// Finish has no live native/response borrower. Join the cancellation body
	// closer before publication of done and before releasing the process permit.
	p.cancel()
	if p.stopCancel != nil {
		if p.stopCancel() {
			close(p.cancelDone)
		}
		<-p.cancelDone
	}
	err = errors.Join(err, p.closeBody())
	if p.epoch != nil {
		err = p.epoch.finish(err)
	}
	if p.retained {
		err = errors.Join(err, p.registry.owner.drop())
		p.retained = false
	}
	l := p.registry
	l.mu.Lock()
	if l.current != p {
		err = errors.Join(err, storagebudget.ErrAuthority)
	} else {
		l.current = nil
	}
	if err != nil {
		p.server.storageTransferCloseErr = errors.Join(p.server.storageTransferCloseErr, err)
		l.err = errors.Join(l.err, err)
	}
	p.mu.Lock()
	p.err = errors.Join(p.err, err)
	p.finished = true
	p.finishing = false
	result := p.err
	p.mu.Unlock()
	p.mu.Lock()
	permit := p.permit
	p.permit = false
	p.mu.Unlock()
	if permit {
		<-chatTransferProcessPermit
	}
	close(p.done)
	l.mu.Unlock()
	if p.epoch != nil && p.epoch.reconciled {
		p.registry.owner.finishOwnedMeasurement(p.epoch.measurement, p.epoch.measurementCaller, result)
	}
	return result
}

func (l *chatTransferLifetimes) stop(s *Server, all bool) {
	if l == nil {
		return
	}
	l.mu.Lock()
	if all {
		l.closing = true
	}
	if s != nil {
		s.storageTransfersClosing = true
	}
	p := l.current
	if p != nil && !all && p.server != s {
		p = nil
	}
	l.mu.Unlock()
	if p != nil {
		p.cancel()
		p.revoke()
	}
}

func (s *Server) stopOriginalChatTransfers() {
	if s == nil || s.storageEnrollment == nil || s.storageEnrollment.construction == nil || s.storageEnrollment.construction.owner == nil {
		return
	}
	s.storageEnrollment.construction.owner.transfers.stop(s, s.cfg.parent == nil)
}

func (s *Server) drainOriginalChatTransfers() error {
	if s == nil || s.storageEnrollment == nil || s.storageEnrollment.construction == nil || s.storageEnrollment.construction.owner == nil {
		return nil
	}
	l := s.storageEnrollment.construction.owner.transfers
	l.mu.Lock()
	p := l.current
	if p != nil && s.cfg.parent != nil && p.server != s {
		p = nil
	}
	l.mu.Unlock()
	if p != nil {
		<-p.done
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if s.cfg.parent == nil {
		return errors.Join(l.err, s.storageTransferCloseErr)
	}
	return s.storageTransferCloseErr
}

// storage_transfer_borrow.go — distinct closed transfer role.
// The lexical storage owner alone constructs this borrower after taking its
// actual writes mutex and original private EX. It cannot Close, release, queue
// or reacquire the private turn. Equal inodes without the constructor/FIFO
// witness never confer exclusive custody.
type chatTransferPrivateBorrow struct {
	self      *chatTransferPrivateBorrow
	operation *chatTransferOperation
	server    *Server
	lock      *privateDataShortLock
	file      *os.File
	turn      *privateDataTurnWitness
	info      os.FileInfo
}

func (op *chatTransferOperation) borrowOriginalPrivate(lock *privateDataShortLock) (*chatTransferPrivateBorrow, error) {
	if op == nil || op.self != op || op.preparation == nil || lock == nil || lock.self != lock || lock.File == nil || lock.turn == nil {
		return nil, storagebudget.ErrAuthority
	}
	s := op.preparation.server
	lock.mu.Lock()
	valid := !lock.closed && lock.exclusive && lock.turn.queue == &s.privateDataTurns
	lock.mu.Unlock()
	if !valid || !lock.turn.current() {
		return nil, storagebudget.ErrAuthority
	}
	info, err := lock.File.Stat()
	if err != nil {
		return nil, err
	}
	b := &chatTransferPrivateBorrow{operation: op, server: s, lock: lock, file: lock.File, turn: lock.turn, info: info}
	b.self = b
	if err = b.current(); err != nil {
		return nil, err
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	if op.finished || op.private != nil {
		return nil, storagebudget.ErrAuthority
	}
	op.private = b
	return b, nil
}

func (b *chatTransferPrivateBorrow) current() error {
	if b == nil || b.self != b || b.operation == nil || b.operation.self != b.operation || b.operation.preparation == nil || b.operation.preparation.server != b.server || b.lock == nil || b.lock.self != b.lock || b.file == nil || b.turn == nil || b.lock.File != b.file || b.lock.turn != b.turn || b.turn.queue != &b.server.privateDataTurns {
		return storagebudget.ErrAuthority
	}
	b.lock.mu.Lock()
	valid := !b.lock.closed && b.lock.exclusive
	b.lock.mu.Unlock()
	if !valid || !b.turn.current() {
		return storagebudget.ErrAuthority
	}
	held, err := b.file.Stat()
	if err != nil {
		return err
	}
	named, e := b.server.assistant.root.Lstat(".data.lock")
	if e != nil || !storagePrivate(held, false) || held.Size() != 0 || !storagePrivate(named, false) || !os.SameFile(held, b.info) || !os.SameFile(named, b.info) {
		return errors.Join(storagebudget.ErrAuthority, e)
	}
	return nil
}

func (b *chatTransferPrivateBorrow) matches(r *storageRootBinding) bool {
	return b != nil && b.self == b && r != nil && r.self == r && r.owner == b.operation.preparation.registry.owner && r.owner.roots[r.row.ID] == r && !r.domainDirectory && r.domainName == ".data.lock" && r.domain.identity != nil && os.SameFile(r.domain.identity, b.info)
}

// This is an actual FIFO/OS constructor, used only inside the original
// transfer owner. Its already-held enrollment EX proves lazy demand finished;
// it opens the retained existing inode and never calls the ordinary lazy
// enrollment guard or O_CREATE below work/writes/private.
func (p *chatTransferPreparation) originalPrivateTurn() (_ *privateDataShortLock, err error) {
	if p == nil || p.self != p || p.epoch == nil || p.epoch.transfer != p || p.epoch.use == nil || p.epoch.work == nil || p.epoch.transferLock == nil {
		return nil, storagebudget.ErrAuthority
	}
	if err = p.current(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(p.ctx, privateDataLockWait)
	defer cancel()
	release, turn, err := p.server.privateDataTurns.acquireOriginal(ctx)
	if err != nil {
		return nil, err
	}
	n, err := storageOpenOriginalControl(p.epoch.c.ref.cell, ".data.lock")
	if err != nil {
		release()
		return nil, err
	}
	if err = waitPrivateDataLock(ctx, n.file, true); err != nil {
		release()
		return nil, errors.Join(err, n.file.Close())
	}
	lock := newPrivateDataShortLock(n.file, release, turn, true)
	if err = errors.Join(ctx.Err(), n.current()); err != nil {
		return nil, errors.Join(err, lock.Close())
	}
	return lock, nil
}

func (p *storageEnrollmentEpoch) borrowOriginalTransferDomains(lock *privateDataShortLock, extra *storageTransferPathWitness) error {
	if p == nil || p.self != p || p.transfer == nil || p.transfer.epoch != p || p.use == nil || p.project == nil || p.work == nil || lock == nil || lock.self != lock || len(p.holds) != 0 {
		return storagebudget.ErrAuthority
	}
	if err := p.transfer.current(); err != nil {
		return err
	}
	o := p.c.owner
	p.use.domains = map[storageDirectoryIdentity]*storageDomainHold{}
	// The lexical owner remains the sole closer of private/project. The domain
	// map lends aliases only; each returned peer has no owned File to Close.
	for _, file := range []*os.File{lock.File, p.project} {
		info, err := file.Stat()
		if err != nil {
			return err
		}
		for _, r := range p.originalRoots {
			if r.domain.file == nil || !os.SameFile(info, r.domain.identity) {
				continue
			}
			if file == lock.File && (r.domainDirectory || r.domainName != ".data.lock") || file == p.project && !r.domainDirectory {
				continue
			}
			id, err := storageFileIdentity(r.domain.identity)
			if err != nil {
				return err
			}
			if p.use.domains[id] == nil {
				h := &storageDomainHold{owner: o, root: r, lifetime: p.use, file: file}
				h.self = h
				p.use.domains[id] = h
			}
		}
	}
	type ordered struct {
		index  int
		native storageOwnerPersistentObject
	}
	order := make([]ordered, 0, len(p.originalRoots)+1)
	for i, r := range p.originalRoots {
		if r.qControl {
			order = append(order, ordered{index: i})
			continue
		}
		var native storageOwnerPersistentObject
		var err error
		if r.domainDirectory {
			native, err = storageOwnerPersistentRoot(p.ctx, r.domain.root)
		} else {
			native, err = storageOwnerPersistentFile(p.ctx, r.domain)
		}
		if err != nil {
			return err
		}
		order = append(order, ordered{i, native})
	}
	if extra != nil {
		if extra.self != extra || extra.closed || extra.domain == nil || !extra.native.valid(1) {
			return storagebudget.ErrAuthority
		}
		order = append(order, ordered{index: -1, native: extra.native})
	}
	sort.SliceStable(order, func(i, j int) bool { return storageOwnerPOCompare(order[i].native, order[j].native) < 0 })
	p.holds = make([]*storageDomainHold, len(p.originalRoots))
	for _, entry := range order {
		if entry.index < 0 {
			info, e := extra.domain.Stat()
			if e != nil {
				return e
			}
			id, e := storageFileIdentity(info)
			if e != nil {
				return e
			}
			if owner := p.use.domains[id]; owner != nil {
				extra.domainOwner = owner.file
			} else {
				if e = lockPrivateData(extra.domain, true); e != nil {
					return errors.Join(storagebudget.ErrBusy, e)
				}
				p.transferExtraDomain = extra.domain
				extra.domainOwner = extra.domain
				extra.domain = nil
			}
			continue
		}
		r := p.originalRoots[entry.index]
		h, err := o.domain(p.ctx, r, p.use)
		if err != nil {
			return err
		}
		p.holds[entry.index] = h
		if h.file != nil && h.borrowed == nil {
			p.transferDomainOwners = append(p.transferDomainOwners, h)
		}
		p.transferHoldOrder = append(p.transferHoldOrder, entry.index)
	}
	p.sourceHoldsComplete = true
	return p.held(p.ctx)
}

func (p *storageEnrollmentEpoch) closeOriginalTransferDomains() error {
	if p == nil || p.self != p || p.transfer == nil || p.transfer.epoch != p {
		return storagebudget.ErrAuthority
	}
	var err error
	for i := len(p.holds) - 1; i >= 0; i-- {
		hold := p.holds[i]
		if hold == nil {
			continue
		}
		owned := false
		for _, owner := range p.transferDomainOwners {
			owned = owned || hold == owner
		}
		if !owned {
			err = errors.Join(err, hold.Close())
		}
		p.holds[i] = nil
	}
	for i := len(p.transferDomainOwners) - 1; i >= 0; i-- {
		err = errors.Join(err, p.transferDomainOwners[i].Close())
	}
	p.transferDomainOwners = nil
	if p.transferExtraDomain != nil {
		err = errors.Join(err, p.transferExtraDomain.Close())
		p.transferExtraDomain = nil
	}
	p.transferHoldOrder = nil
	p.sourceHoldsComplete = false
	return err
}

// storage_transfer_current.go — distinct closed transfer role.
// This is a registered continuation of one positive Reserve, never a token in
// a context or a fresh operation after cancellation. Its owner keeps original
// home/use/private/domain descriptions until finalization has returned.
type chatTransferCompletion struct {
	self       *chatTransferCompletion
	operation  *chatTransferOperation
	generation uint64
	plan       storagebudget.Digest
	initial    storagebudget.Token
	consumed   [storageTransferControlGroups]bool
	finished   bool
}

func (completion *chatTransferCompletion) current(ctx context.Context) error {
	if completion == nil || completion.self != completion || completion.operation == nil || ctx == nil {
		return storagebudget.ErrAuthority
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	op := completion.operation
	p := op.preparation
	op.mu.Lock()
	valid := op.self == op && op.completion == completion && op.completing && !op.finished && !completion.finished && p != nil && p.admitted && op.record.Plan == completion.plan && op.record.InitialToken == completion.initial
	op.mu.Unlock()
	if !valid || p.self != p || p.epoch == nil || p.epoch.transfer != p || completion.generation != p.epoch.generation {
		return storagebudget.ErrAuthority
	}
	l := p.registry
	l.mu.Lock()
	valid = l.current == p && l.owner == p.epoch.c.owner
	l.mu.Unlock()
	if !valid {
		return storagebudget.ErrAuthority
	}
	o := l.owner
	o.mu.Lock()
	g := &o.admission
	g.mu.Lock()
	p.epoch.c.mu.Lock()
	p.epoch.mu.Lock()
	valid = !o.closed && !p.epoch.c.closed && p.epoch.running && !p.epoch.finishing && g.enrollment == p.epoch && g.generation == completion.generation && (g.phase == storageAdmissionEnrollment || g.phase == storageAdmissionClosing) && o.active == 1 && g.shared == 0 && g.prefixes == 0 && g.preparation == nil && g.exclusiveCensus == nil
	p.epoch.mu.Unlock()
	p.epoch.c.mu.Unlock()
	g.mu.Unlock()
	o.mu.Unlock()
	if !valid {
		return storagebudget.ErrAuthority
	}
	return op.originalTransferCustody(ctx)
}

// The original lexical operation chooses one of these checks by phase. Neither
// form changes ordinary construction, mutation, root or enrollment permission.
func (op *chatTransferOperation) currentBeforeEffect(ctx context.Context) error {
	if op == nil || op.self != op || op.preparation == nil || ctx == nil {
		return storagebudget.ErrAuthority
	}
	if err := op.preparation.current(); err != nil {
		return err
	}
	op.mu.Lock()
	valid := !op.completing && !op.finished
	op.mu.Unlock()
	if !valid {
		return os.ErrClosed
	}
	if err := op.preparation.epoch.permit(ctx); err != nil {
		return err
	}
	return op.originalTransferCustody(ctx)
}

func (op *chatTransferOperation) originalTransferCustody(ctx context.Context) error {
	if op == nil || op.self != op || op.preparation == nil || op.preparation.operation != op || ctx == nil {
		return storagebudget.ErrAuthority
	}
	p := op.preparation
	epoch := p.epoch
	if epoch == nil || epoch.self != epoch || epoch.transfer != p || epoch.use == nil || epoch.project == nil || epoch.work == nil || epoch.transferLock == nil || op.private == nil {
		return storagebudget.ErrAuthority
	}
	c := epoch.c
	if err := c.originPhysicalCurrent(ctx); err != nil {
		return err
	}
	if err := c.sources.currentProjects(ctx); err != nil {
		return err
	}
	cell := epoch.use.cell
	cell.mu.Lock()
	var err error
	if epoch.use.self != epoch.use || epoch.use.closed || cell.closed || cell != c.ref.cell || cell.startup != c {
		err = storagebudget.ErrAuthority
	} else {
		err = cell.ownerV2PhysicalCurrent()
		if err == nil {
			err = cell.useGateCurrent(epoch.use.gate)
		}
	}
	cell.mu.Unlock()
	if err != nil {
		return err
	}
	project, err := epoch.project.Stat()
	named, e := epoch.originalProjectRoot().Stat(".")
	if err != nil || e != nil || !os.SameFile(project, named) {
		return errors.Join(errStorageCustody, err, e)
	}
	if err = errors.Join(epoch.work.current(), epoch.transferLock.current(), op.private.current()); err != nil {
		return err
	}
	if len(epoch.holds) != len(epoch.originalRoots) {
		return storagebudget.ErrAuthority
	}
	for i, root := range epoch.originalRoots {
		if err = root.currentForOriginalTransfer(ctx, op); err != nil {
			return err
		}
		hold := epoch.holds[i]
		if hold == nil || hold.self != hold || hold.closed || hold.root != root || hold.owner != p.registry.owner || hold.lifetime != epoch.use {
			return storagebudget.ErrAuthority
		}
		if root.qControl {
			if hold.file != nil || hold.borrowed != nil {
				return storagebudget.ErrAuthority
			}
			continue
		}
		original := hold
		if hold.borrowed != nil {
			original = hold.borrowed
			key, e := storageFileIdentity(root.domain.identity)
			if e != nil || epoch.use.domains[key] != original || original.self != original || original.closed || original.owner != hold.owner || original.lifetime != epoch.use || original.borrowed != nil {
				return errors.Join(storagebudget.ErrAuthority, e)
			}
		}
		if original.file == nil {
			return storagebudget.ErrAuthority
		}
		held, e := original.file.Stat()
		if e != nil || !os.SameFile(held, root.domain.identity) {
			return errors.Join(errStorageCustody, e)
		}
	}
	if op.path != nil {
		if err = op.path.current(ctx); err != nil {
			return err
		}
		if op.path.domainOwner == nil {
			return storagebudget.ErrAuthority
		}
		held, e := op.path.domainOwner.Stat()
		named, ne := op.path.root.Stat(".")
		if e != nil || ne != nil || !os.SameFile(held, named) {
			return errors.Join(errStorageCustody, e, ne)
		}
		native, e := storageAdvisoryPersistentHeld(ctx, op.path.domainOwner)
		if e != nil || native != op.path.native {
			return errors.Join(errStorageCustody, e)
		}
	}
	if epoch.qualified != nil {
		q := epoch.qualified
		if q.self != q || q.phase != epoch || q.generation != epoch.generation {
			return storagebudget.ErrAuthority
		}
		system, e := storageEnrollmentNativeSystemFacts(ctx)
		if e != nil {
			return e
		}
		system.GOOS, system.GOARCH, system.Toolchain = runtime.GOOS, runtime.GOARCH, runtime.Version()
		facts := storageEnrollmentNativeFacts{System: system}
		for i, parent := range []*os.Root{c.original.config, c.anchor} {
			f, e := parent.Open(".")
			if e != nil {
				return e
			}
			facts.Parents[i], e = storageEnrollmentNativeParentObservation(ctx, f)
			e = errors.Join(e, f.Close())
			if e != nil {
				return e
			}
		}
		if !storageEnrollmentNativeInvocationMatches(q.record, q.capturedFacts, facts) {
			return errStorageEnrollmentQualification
		}
	}
	return ctx.Err()
}

func (r *storageRootBinding) currentForOriginalTransfer(ctx context.Context, op *chatTransferOperation) error {
	if r == nil || r.self != r || op == nil || op.self != op || op.preparation == nil || r.owner != op.preparation.registry.owner || r.owner.roots[r.row.ID] != r || r.source == nil {
		return storagebudget.ErrAuthority
	}
	epoch := op.preparation.epoch
	found := false
	for _, original := range epoch.originalRoots {
		if original == r {
			found = true
			break
		}
	}
	if !found {
		return storagebudget.ErrAuthority
	}
	if err := r.source.currentForOriginalTransfer(ctx, op); err != nil {
		return err
	}
	if r.transferInactive {
		if err := r.transferInactiveCurrent(ctx); err != nil {
			return err
		}
	}
	for _, edge := range r.witnesses {
		if err := edge.current(); err != nil {
			return err
		}
	}
	directory, err := r.source.directoryIdentity()
	if err != nil || directory != r.directory || r.source.root != r.root {
		return errors.Join(errStorageCustody, err)
	}
	if r.qControl {
		if r.domain.file != nil || r.scopeParent != (storagebudget.ID{}) {
			return storagebudget.ErrAuthority
		}
		return ctx.Err()
	}
	if r.domain.file == nil {
		return storagebudget.ErrAuthority
	}
	if !r.domainDirectory {
		return r.domain.current()
	}
	held, err := r.domain.file.Stat()
	named, e := r.domain.root.Stat(".")
	if err != nil || e != nil || !held.IsDir() || !named.IsDir() || !os.SameFile(held, named) || !os.SameFile(held, r.domain.identity) {
		return errors.Join(errStorageCustody, err, e)
	}
	if r.scopeParent != (storagebudget.ID{}) {
		parent := r.owner.roots[r.scopeParent]
		if parent == nil || parent == r || parent.domain.file != r.domain.file || r.source.parent != parent.source {
			return storagebudget.ErrAuthority
		}
	}
	if r.restrictedNamespace == "" && (!storagePrivate(held, true) || !storagePrivate(named, true)) {
		return errStorageCustody
	}
	return ctx.Err()
}

func (s *storageOriginalSourceRoot) currentForOriginalTransfer(ctx context.Context, op *chatTransferOperation) error {
	if s == nil || op == nil || op.self != op || op.preparation == nil || ctx == nil {
		return storagebudget.ErrAuthority
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c := op.preparation.epoch.c
	if s.parent != nil {
		if err := s.parent.currentForOriginalTransfer(ctx, op); err != nil {
			return err
		}
	}
	for _, edge := range s.witnesses {
		if err := edge.current(); err != nil {
			return err
		}
	}
	if s.marker != nil {
		if err := s.marker.current(); err != nil {
			return err
		}
	}
	if s.scope != nil {
		scope := s.scope
		if scope.Default != nil {
			if scope.Default.Workflow == 4 || scope.Default.Workflow == 6 {
				if err := s.transferScopeCurrent(ctx, c); err != nil {
					return err
				}
			} else if s.role != 8 || s.parent != nil || scope.Default.Workflow != 2 || s.path != string(scope.Default.SelectedPath) || s.relative != string(scope.Default.Next) || !c.sources.historyAncestorSelected(ctx, *scope) {
				return storagebudget.ErrAuthority
			}
		} else if s.role != 7 || s.parent != nil || s.relative != ".desk-private" || !bytes.Equal(scope.Namespace, []byte(".desk-private")) {
			return storagebudget.ErrAuthority
		}
	}
	if s.role == 4 && len(s.witnesses) == 0 {
		fresh, err := openPrivateDataRoot(s.path, false)
		if err != nil {
			return err
		}
		held, e := s.root.Stat(".")
		named, f := fresh.Stat(".")
		closed := fresh.Close()
		if e != nil || f != nil || closed != nil || !os.SameFile(held, named) {
			return errors.Join(errStorageCustody, e, f, closed)
		}
	}
	native, err := storageOwnerPersistentRoot(ctx, s.root)
	if err != nil || native != s.persistent {
		return errors.Join(errStorageCustody, err)
	}
	if s.role == 5 {
		if c.providers == nil || c.providers.byID[s.key] == nil || c.providers.byID[s.key].root != s.root || c.server.storageStartupProviders == nil || c.server.storageStartupProviders.byID[s.key] == nil {
			return storagebudget.ErrAuthority
		}
		return c.server.storageStartupProviders.byID[s.key].check(ctx)
	}
	for _, selection := range c.sources.selections {
		if selection.settings != s.root && selection.location.Path != s.path && selection.location.Previous != s.path {
			continue
		}
		if err = op.originalTransferSelectionCurrent(ctx, selection); err != nil {
			return err
		}
	}
	return nil
}

func (op *chatTransferOperation) originalTransferSelectionCurrent(ctx context.Context, selection storageOriginalHistorySelection) error {
	if selection.pending != nil {
		return selection.pending.pendingPointerCurrent(ctx, selection)
	}
	// The exact operation settings description identifies the one permitted
	// selection. No remembered successful publication can revert to old state.
	if op.settings != nil && selection.settings == op.settings {
		raw, err := readPrivateData(selection.settings, dataLocationName, storageTransferPointerBytes)
		pointerState := max(op.record.PointerState, op.returned.PointerState)
		if pointerState < 2 {
			if op.record.OldAbsent && errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err == nil && bytes.Equal(raw, op.record.OldPointer) {
				return nil
			}
		}
		if pointerState > 0 && err == nil && bytes.Equal(raw, op.record.NewPointer) {
			return nil
		}
		// Attempted/unknown is inspectable charge, never a permission to settle.
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	raw, err := readPrivateData(selection.settings, dataLocationName, 16384)
	if !selection.explicit {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errors.Join(errStorageOwnerTransitionRequired, err)
	}
	if err != nil {
		return err
	}
	location, err := storageTransferResolvedLocation(raw, selection.path)
	if err != nil || location != selection.location {
		return errors.Join(errStorageOwnerTransitionRequired, err)
	}
	return nil
}

// storage_transfer_path.go — distinct closed transfer role.
// This invocation owns the complete original held-name chain. The generic
// ancestry witnesses do not claim that every ancestor filesystem is ext4/APFS.
// Only the exact effect parent supplies native capacity/profile facts. These
// actual descriptions remain open through the last borrower and checked Close;
// a digest or reopened pathname cannot replace the live chain.
type storageTransferPathWitness struct {
	self                *storageTransferPathWitness
	path                string
	names               []string
	base, root          *os.Root
	edges               []storageNamedRoot
	native              storageOwnerPersistentObject
	facts               storageEnrollmentNativeParentFacts
	domain, domainOwner *os.File // one original sorted EX owner or exact borrowed alias
	missing             int
	closed              bool
}

func storageTransferPathParts(path string) ([]string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.VolumeName(path) != "" || strings.ContainsAny(path, "\x00\r\n") || len(path) > storageTransferPointerBytes {
		return nil, storagebudget.ErrBounds
	}
	parts := strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
	if path == string(filepath.Separator) {
		return nil, storagebudget.ErrBounds
	}
	if len(parts) >= storageTransferDescendantSlots {
		return nil, storagebudget.ErrBounds
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, storagebudget.ErrBounds
		}
	}
	return parts, nil
}

// Preparation must have registered the original owner retain before this read.
// Positive absence stops at the first missing child. No create, marker, bank
// publication or lookup below that absent name happens here. FD exhaustion is
// an ordinary pre-effect error; it never narrows a path or drops witnesses.
func (op *chatTransferOperation) captureOriginalTransferPath(path string) (out *storageTransferPathWitness, err error) {
	if op == nil || op.self != op || op.preparation == nil || op.preparation.operation != op || op.preparation.admitted {
		return nil, storagebudget.ErrAuthority
	}
	p := op.preparation
	if err = p.current(); err != nil {
		return nil, err
	}
	if err = p.epoch.held(p.ctx); err != nil {
		return nil, err
	}
	names, err := storageTransferPathParts(path)
	if err != nil {
		return nil, err
	}
	w := &storageTransferPathWitness{path: path, names: append([]string{}, names...)}
	w.self = w
	w.base, err = os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	w.root = w.base
	defer func() {
		if err != nil {
			err = errors.Join(err, w.Close())
		}
	}()
	for _, name := range names {
		if err = p.current(); err != nil {
			return nil, err
		}
		before, e := w.root.Lstat(name)
		if errors.Is(e, os.ErrNotExist) {
			break
		}
		if e != nil {
			return nil, e
		}
		if e = safeDirectory("transfer ancestor", before, false); e != nil {
			return nil, e
		}
		edge, e := storagePinChild(w.root, name, false)
		if e != nil {
			return nil, e
		}
		w.edges = append(w.edges, edge)
		w.root = edge.root
	}
	w.missing = len(w.edges)
	f, err := w.root.Open(".")
	if err != nil {
		return nil, err
	}
	w.native, err = storageAdvisoryPersistentHeld(p.ctx, f)
	if err == nil {
		w.facts, err = storageEnrollmentNativeParentObservation(p.ctx, f)
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return nil, err
	}
	if err = w.current(p.ctx); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *storageTransferPathWitness) current(ctx context.Context) (err error) {
	if w == nil || w.self != w || w.closed || w.base == nil || w.root == nil || ctx == nil || w.missing != len(w.edges) || w.missing > len(w.names) || !w.native.valid(1) {
		return storagebudget.ErrAuthority
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	parent := w.base
	for i, edge := range w.edges {
		if edge.parent != parent || edge.name != w.names[i] {
			return storagebudget.ErrAuthority
		}
		if err = edge.current(); err != nil {
			return err
		}
		info, e := edge.root.Stat(".")
		if e != nil {
			return e
		}
		if e = safeDirectory("transfer ancestor", info, false); e != nil {
			return e
		}
		parent = edge.root
	}
	if parent != w.root {
		return storagebudget.ErrAuthority
	}
	f, err := w.root.Open(".")
	if err != nil {
		return err
	}
	native, err := storageAdvisoryPersistentHeld(ctx, f)
	facts := storageEnrollmentNativeParentFacts{}
	if err == nil {
		facts, err = storageEnrollmentNativeParentObservation(ctx, f)
	}
	err = errors.Join(err, f.Close())
	if err != nil || native != w.native || facts != w.facts {
		return errors.Join(errStorageCustody, err)
	}
	// Recheck names after the native observation, including initialized owners.
	for _, edge := range w.edges {
		if err = edge.current(); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (w *storageTransferPathWitness) Close() (err error) {
	if w == nil || w.self != w {
		return storagebudget.ErrAuthority
	}
	if w.closed {
		return nil
	}
	w.closed = true
	for i := len(w.edges) - 1; i >= 0; i-- {
		err = errors.Join(err, w.edges[i].root.Close())
	}
	if w.domain != nil {
		err = errors.Join(err, w.domain.Close())
		w.domain = nil
	}
	if w.base != nil {
		err = errors.Join(err, w.base.Close())
	}
	return err
}

// This lookup supplies a read-only original-custody continuation. It does not
// authorize a syscall, enrollment Scope, new root or operation. Admission and
// every native effect retain their separate typed permission checks.
func (c *storageBudgetConstruction) originalTransferOperation() *chatTransferOperation {
	if c == nil || c.self != c || c.owner == nil {
		return nil
	}
	gate := &c.owner.admission
	gate.mu.Lock()
	epoch := gate.enrollment
	phase := gate.phase
	gate.mu.Unlock()
	if epoch == nil || epoch.self != epoch || epoch.c != c || epoch.transfer == nil || phase != storageAdmissionEnrollment && phase != storageAdmissionClosing {
		return nil
	}
	preparation := epoch.transfer
	preparation.mu.Lock()
	op := preparation.operation
	valid := preparation.self == preparation && preparation.epoch == epoch && !preparation.finished
	preparation.mu.Unlock()
	if !valid || op == nil || op.self != op || op.preparation != preparation {
		return nil
	}
	return op
}

func (op *chatTransferOperation) originalReadCurrent(ctx context.Context) error {
	if op == nil || op.self != op || op.preparation == nil {
		return storagebudget.ErrAuthority
	}
	op.mu.Lock()
	completion := op.completion
	completing := op.completing
	finished := op.finished
	op.mu.Unlock()
	if finished {
		return os.ErrClosed
	}
	if completing {
		if completion == nil {
			return storagebudget.ErrAuthority
		}
		return completion.current(ctx)
	}
	return op.currentBeforeEffect(ctx)
}

func (s *storageOriginalSourceRoot) transferScopeCurrent(ctx context.Context, c *storageBudgetConstruction) error {
	if s == nil || s.scope == nil || s.scope.Default == nil || c == nil || c.sources == nil || c.ref == nil || c.ref.cell.ownerRecord == nil || ctx == nil {
		return storagebudget.ErrAuthority
	}
	scope := s.scope
	d := scope.Default
	if !storageEnrollmentDefaultSourceNamespace(*scope) || scope.Installation != c.ref.cell.ownerRecord.Installation || scope.QBinding != c.ref.cell.binding || d.Selected != s.persistent || string(d.SelectedPath) != s.path || s.parent != nil || len(s.witnesses) == 0 && !(d.Workflow == 4 && string(d.SelectedPath) == "/") {
		return storagebudget.ErrAuthority
	}
	if d.Workflow == 4 {
		if s.role != 8 || s.relative != string(d.Next) {
			return storagebudget.ErrAuthority
		}
	} else if d.Workflow == 6 {
		if s.role != 4 || s.relative != "" {
			return storagebudget.ErrAuthority
		}
	} else {
		return storagebudget.ErrAuthority
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, edge := range s.witnesses {
		if err := edge.current(); err != nil {
			return err
		}
	}
	native, err := storageOwnerPersistentRoot(ctx, s.root)
	if err != nil || native != s.persistent {
		return errors.Join(errStorageCustody, err)
	}
	settingsFound := false
	for _, selection := range c.sources.selections {
		settings, err := storageOwnerPersistentRoot(ctx, selection.settings)
		if err != nil {
			return err
		}
		if settings == d.Settings {
			if settingsFound {
				return storagebudget.ErrAuthority
			}
			settingsFound = true
		}
	}
	if !settingsFound {
		return storagebudget.ErrAuthority
	}
	if s.marker != nil {
		if err := s.marker.current(); err != nil {
			return err
		}
		native, err := storageOwnerPersistentFile(ctx, *s.marker)
		if err != nil || d.Marker != (storageOwnerPersistentObject{}) && native != d.Marker {
			return errors.Join(errStorageCustody, err)
		}
	} else if d.Marker != (storageOwnerPersistentObject{}) {
		return errStorageCustody
	}
	for _, edge := range s.witnesses {
		if err := edge.current(); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// A closer never becomes the owning executor. The preparation invocation must
// finish its body/owner scope and signal done after all actual borrowers return.
func (p *chatTransferPreparation) Close() error {
	if p == nil || p.self != p || p.cancel == nil || p.done == nil {
		return storagebudget.ErrAuthority
	}
	p.cancel()
	p.revoke()
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// Both live and completion dispatch serialize their final entry decision with
// revocation under p.mu. No mutex stays held across the native/Q call. A Close
// which loses entry waits for the exact returned borrower, never its deadline.
func (op *chatTransferOperation) enterOriginalTransferCall(ctx context.Context, completion *chatTransferCompletion) error {
	if completion == nil {
		if err := op.currentBeforeEffect(ctx); err != nil {
			return err
		}
	} else {
		if completion.operation != op {
			return storagebudget.ErrAuthority
		}
		if err := completion.current(ctx); err != nil {
			return err
		}
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	p := op.preparation
	p.mu.Lock()
	defer p.mu.Unlock()
	if op.entered || op.finished || p.finished || p.finishing || p.operation != op || ctx.Err() != nil {
		return storagebudget.ErrAuthority
	}
	if completion == nil && (op.completing || p.revoked || p.ctx.Err() != nil) {
		return os.ErrClosed
	}
	if completion != nil && (op.completion != completion || !op.completing || completion.finished) {
		return storagebudget.ErrAuthority
	}
	op.entered = true
	op.enteredDone = make(chan struct{})
	return nil
}
func (op *chatTransferOperation) leaveOriginalTransferCall(err error) error {
	if op == nil || op.self != op {
		return errors.Join(storagebudget.ErrAuthority, err)
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	if !op.entered || op.enteredDone == nil {
		return errors.Join(storagebudget.ErrAuthority, err)
	}
	op.entered = false
	close(op.enteredDone)
	op.enteredDone = nil
	return err
}

func (op *chatTransferOperation) capturePreparedTransferPath(path string) (_ *storageTransferPathWitness, err error) {
	if op == nil || op.self != op || op.preparation == nil || op.preparation.operation != nil || op.private == nil || op.preparation.targetPath != path || op.preparation.epoch == nil || len(op.preparation.epoch.holds) != 0 {
		return nil, storagebudget.ErrAuthority
	}
	p := op.preparation
	if err = p.current(); err != nil {
		return nil, err
	}
	if err = op.private.current(); err != nil {
		return nil, err
	}
	if err = errors.Join(p.epoch.work.current(), p.epoch.transferLock.current()); err != nil {
		return nil, err
	}
	names, err := storageTransferPathParts(path)
	if err != nil {
		return nil, err
	}
	w := &storageTransferPathWitness{path: path, names: append([]string{}, names...)}
	w.self = w
	w.base, err = os.OpenRoot("/")
	if err != nil {
		return nil, err
	}
	w.root = w.base
	defer func() {
		if err != nil {
			err = errors.Join(err, w.Close())
		}
	}()
	for _, name := range names {
		if err = p.current(); err != nil {
			return nil, err
		}
		info, e := w.root.Lstat(name)
		if errors.Is(e, os.ErrNotExist) {
			break
		}
		if e != nil {
			return nil, e
		}
		if e = safeDirectory("transfer ancestor", info, false); e != nil {
			return nil, e
		}
		edge, e := storagePinChild(w.root, name, false)
		if e != nil {
			return nil, e
		}
		w.edges = append(w.edges, edge)
		w.root = edge.root
	}
	w.missing = len(w.edges)
	w.domain, err = w.root.Open(".")
	if err != nil {
		return nil, err
	}
	w.native, err = storageAdvisoryPersistentHeld(p.ctx, w.domain)
	if err == nil {
		w.facts, err = storageEnrollmentNativeParentObservation(p.ctx, w.domain)
	}
	if err != nil {
		return nil, err
	}
	if err = op.private.current(); err != nil {
		return nil, err
	}
	if err = w.current(p.ctx); err != nil {
		return nil, err
	}
	return w, nil
}

// Resolve only the original registered continuation. Validation is performed
// outside epoch/owner locks by the consuming boundary; a copied struct, later
// operation or context value can never select this branch.
func (p *storageEnrollmentEpoch) originalTransferCompletion() *chatTransferCompletion {
	if p == nil || p.self != p || p.transfer == nil || p.transfer.self != p.transfer || p.transfer.epoch != p || p.transfer.operation == nil {
		return nil
	}
	op := p.transfer.operation
	op.mu.Lock()
	defer op.mu.Unlock()
	completion := op.completion
	if op.self != op || op.preparation != p.transfer || !op.completing || op.finished || completion == nil || completion.self != completion || completion.operation != op || completion.finished {
		return nil
	}
	return completion
}
