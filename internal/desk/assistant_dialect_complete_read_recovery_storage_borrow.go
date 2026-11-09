package desk

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Captured only by New immediately after its original home lease acquisition,
// before a service/work lease exists. Saved paths and later HTTP pins cannot
// construct this earlier lifetime. Server remains the sole descriptor owner.
type assistantDialectRecoveryHomeWitness struct {
	self   *assistantDialectRecoveryHomeWitness
	server *Server
	file   *os.File
	info   os.FileInfo
}

func (s *Server) captureOriginalDialectRecoveryHome() error {
	if s == nil || s.homeLease == nil || s.recoveryStorageHome != nil {
		return errAssistantRunScope
	}
	info, e := s.homeLease.Stat()
	if e != nil {
		return e
	}
	w := &assistantDialectRecoveryHomeWitness{server: s, file: s.homeLease, info: info}
	w.self = w
	s.recoveryStorageHome = w
	if e := w.current(); e != nil {
		s.recoveryStorageHome = nil
		return e
	}
	return nil
}
func (s *Server) originalDialectRecoveryHome() *assistantDialectRecoveryHomeWitness {
	if s == nil {
		return nil
	}
	owner := s.deskOwner()
	if owner == nil {
		return nil
	}
	return owner.recoveryStorageHome
}
func (w *assistantDialectRecoveryHomeWitness) current() error {
	if w == nil || w.self != w || w.server == nil || w.server.recoveryStorageHome != w || w.server.homeLease != w.file || w.server.assistant == nil || !w.server.assistant.usable() {
		return errAssistantRunScope
	}
	held, e := w.file.Stat()
	if e != nil {
		return e
	}
	named, e := w.server.assistant.root.Lstat(".home-use.lock")
	if e != nil || !os.SameFile(held, w.info) || !os.SameFile(named, w.info) || !storagePrivate(held, false) || held.Size() != 0 {
		return errors.Join(errAssistantRunScope, e)
	}
	return nil
}

// One actual private EX and its original FIFO turn. Ownership is transferred
// from the first guard; the second guard gets one lexical borrow, never a new
// queue entry/flock or authority inferred from a raw file description.
type assistantDialectRecoveryPrivateHold struct {
	self                      *assistantDialectRecoveryPrivateHold
	mu                        sync.Mutex
	operation                 *assistantDialectReadRecoveryStorageOperation
	config                    *storageRootBinding
	lock                      *privateDataShortLock
	info                      os.FileInfo
	borrower                  *assistantDialectReadRecoveryGuard
	borrowed, closing, closed bool
	closeErr                  error
}

func (h *assistantDialectRecoveryPrivateHold) matchesRoot(r *storageRootBinding) bool {
	// An enrolled child may share the installation's exact .data.lock domain.
	// Compare its original binding/inode, never its history root's pointer or
	// pathname. A distinct enrolled domain still needs its own independent EX.
	return h != nil && h.self == h && h.config != nil && r != nil && r.self == r && r.owner == h.config.owner && r.owner.roots[r.row.ID] == r && r.domainName == ".data.lock" && !r.domainDirectory && r.domain.identity != nil && os.SameFile(r.domain.identity, h.info)
}
func (h *assistantDialectRecoveryPrivateHold) current(ctx context.Context) error {
	if h == nil || h.self != h || !h.mu.TryLock() {
		return storagebudget.ErrAuthority
	}
	defer h.mu.Unlock()
	return h.currentLocked(ctx)
}
func (h *assistantDialectRecoveryPrivateHold) currentLocked(ctx context.Context) error {
	if h.closed || h.closing || h.lock == nil || h.operation == nil || h.operation.revoked.Load() {
		return storagebudget.ErrAuthority
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := h.config.current(ctx); e != nil {
		return e
	}
	held, e := h.lock.Stat()
	if e != nil {
		return e
	}
	named, e := h.config.root.Lstat(".data.lock")
	if e != nil || !os.SameFile(held, h.info) || !os.SameFile(named, h.info) || !os.SameFile(h.config.domain.identity, h.info) || !storagePrivate(held, false) || held.Size() != 0 {
		return errors.Join(storagebudget.ErrAuthority, e)
	}
	return nil
}
func (h *assistantDialectRecoveryPrivateHold) borrow(ctx context.Context, g *assistantDialectReadRecoveryGuard) error {
	if h == nil || h.self != h || !h.mu.TryLock() {
		return storagebudget.ErrBusy
	}
	defer h.mu.Unlock()
	if e := h.currentLocked(ctx); e != nil {
		return e
	}
	if h.borrowed || h.borrower != nil || g == nil || g.self != g || g.pin != h.operation.pin {
		return storagebudget.ErrAuthority
	}
	h.borrowed = true
	h.borrower = g
	g.private = h.lock
	g.privateBorrow = h
	recoveryStorageStep("private-borrowed")
	return nil
}
func (h *assistantDialectRecoveryPrivateHold) release(g *assistantDialectReadRecoveryGuard) error {
	if h == nil || h.self != h {
		return storagebudget.ErrAuthority
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.borrower != g {
		return storagebudget.ErrAuthority
	}
	h.borrower = nil
	recoveryStorageStep("private-borrow-released")
	if h.closing {
		return h.closeLocked()
	}
	return nil
}
func (h *assistantDialectRecoveryPrivateHold) closeLocked() error {
	if h.closed {
		return h.closeErr
	}
	h.closed = true
	h.closeErr = h.lock.Close()
	recoveryStorageStep("private-closed")
	return h.closeErr
}
func (h *assistantDialectRecoveryPrivateHold) Close() error {
	if h == nil {
		return nil
	}
	if h.self != h {
		return storagebudget.ErrAuthority
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closing = true
	if h.borrower != nil {
		return storagebudget.ErrBusy
	}
	return h.closeLocked()
}

// Observation only: no return value, no authority or timing substitution.
var testHookDialectRecoveryStorageStep func(string)

func recoveryStorageStep(step string) {
	if testHookDialectRecoveryStorageStep != nil {
		testHookDialectRecoveryStorageStep(step)
	}
}

func (h *assistantDialectRecoveryPrivateHold) checkBorrower(ctx context.Context, g *assistantDialectReadRecoveryGuard) error {
	if h == nil || h.self != h || !h.mu.TryLock() {
		return storagebudget.ErrBusy
	}
	defer h.mu.Unlock()
	if h.borrower != g {
		return storagebudget.ErrAuthority
	}
	return h.currentLocked(ctx)
}
