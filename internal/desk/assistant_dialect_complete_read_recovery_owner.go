package desk

import (
	"context"
	"errors"
	"net/http"
	"os"
	"reflect"
)

// The actual service lends its original descriptors; a saved run, PID or
// serialized generation cannot construct this lifetime. External acquisition
// opens the existing named descriptors without creating or recovering anything.
type assistantDialectReadRecoveryPin struct {
	self                                              *assistantDialectReadRecoveryPin
	host                                              *assistantRunService
	lease                                             *assistantRunLease
	run                                               assistantRunRecord
	storage                                           *assistantDialectReadRecoveryStorageOperation
	borrowed                                          bool
	closed                                            bool
	closeErr                                          error
	projectInfo, workRootInfo, workInfo, executorInfo os.FileInfo
}

func (h *assistantRunService) pinDialectReadRecovery(ctx context.Context, run assistantRunRecord) (*assistantDialectReadRecoveryPin, error) {
	return h.pinDialectReadRecoveryPrepared(ctx, run, nil)
}
func (h *assistantRunService) pinDialectReadRecoveryPrepared(ctx context.Context, run assistantRunRecord, prepared *assistantDialectReadRecoveryStorageOperation) (_ *assistantDialectReadRecoveryPin, err error) {
	var store assistantRunStore
	// Namespace reads may wait for FIFO/private custody. The prepared writer
	// path performs them before acquiring any service/lease/host mutex.
	if prepared != nil {
		if prepared.writer.server != h.server {
			return nil, errAssistantRunScope
		}
		if e := prepared.current(ctx); e != nil {
			return nil, e
		}
		store = h.server.assistantRunsContext(ctx)
		if store.problem != nil {
			return nil, store.problem
		}
	}

	if !h.mu.TryLock() {
		return nil, errRetainedArtifactBusy
	}
	defer h.mu.Unlock()
	if h.closed || h.server == nil || !assistantRunTerminal(run.State) || h.active[run.ID] != nil {
		return nil, errAssistantRunConflict
	}
	s := h.server
	if prepared == nil {
		store = s.assistantRuns()
	}
	if store.problem != nil || store.project != run.Binding.Project || s.root == nil || s.assistant == nil {
		return nil, errAssistantRunScope
	}
	p := &assistantDialectReadRecoveryPin{host: h, run: run}
	p.self = p
	if h.lease != nil {
		p.lease, p.borrowed = h.lease, true
	} else {
		if h.dialectReadRecoveryPins != 0 {
			return nil, errRetainedArtifactBusy
		}
		// External recovery must exclude the original writer even if the named
		// executor inode was replaced. Its original native work-domain inode is
		// verified before raw observation; every original writer holds SH there.
		// Borrowed same-service custody above keeps its original SH lease.
		work, e := openAssistantDialectRecoveryLock(ctx, s.assistant.root, ".data-work.lock", true)
		if e != nil {
			return nil, e
		}
		file, e := openAssistantDialectRecoveryLock(ctx, s.assistant.root, "assistant-run-executor-"+store.project+".lock", true)
		if e != nil {
			return nil, errors.Join(e, work.Close())
		}
		p.lease = &assistantRunLease{store: store, work: work, file: file, recoveryHome: s.originalDialectRecoveryHome()}
		recoveryStorageStep("external-work-executor-acquired")
	}
	defer func() {
		if err != nil && !p.borrowed {
			err = errors.Join(err, p.lease.Close())
		}
	}()
	if !p.lease.mu.TryLock() {
		return nil, errRetainedArtifactBusy
	}
	defer p.lease.mu.Unlock()
	if p.lease.store.server != s || p.lease.store.project != store.project || p.lease.store.name != store.name || p.lease.file == nil || p.lease.work == nil || p.lease.recoveryErr != nil {
		return nil, errAssistantRunScope
	}
	if p.projectInfo, err = s.root.Stat("."); err != nil {
		return nil, err
	}
	if p.workRootInfo, err = s.assistant.root.Stat("."); err != nil {
		return nil, err
	}
	if p.workInfo, err = p.lease.work.Stat(); err != nil {
		return nil, err
	}
	if p.executorInfo, err = p.lease.file.Stat(); err != nil {
		return nil, err
	}
	if err = p.checkLocked(ctx); err != nil {
		return nil, err
	}
	if prepared != nil {
		if err = prepared.bindPin(p); err != nil {
			return nil, err
		}
		if p.borrowed {
			recoveryStorageStep("active-work-executor-borrowed")
		}
	}
	h.dialectReadRecoveryPins++
	return p, nil
}

func openAssistantDialectRecoveryLock(ctx context.Context, root *os.Root, name string, exclusive bool) (_ *os.File, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if ownerOnlyFile(name, before.Mode()) != nil || ownedByUs(name, before) != nil {
		return nil, errAssistantRunScope
	}
	file, err := root.OpenFile(name, os.O_RDWR|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, file.Close())
		}
	}()
	held, err := file.Stat()
	if err != nil || !os.SameFile(before, held) {
		return nil, errors.Join(errAssistantRunScope, err)
	}
	if err = lockPrivateData(file, exclusive); err != nil {
		return nil, errors.Join(errRetainedArtifactBusy, err)
	}
	after, err := root.Lstat(name)
	if err != nil || !os.SameFile(held, after) {
		return nil, errors.Join(errAssistantRunScope, err)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return file, nil
}

// Caller owns service and lease mutexes. Original files stay pinned until Close.
func (p *assistantDialectReadRecoveryPin) checkLocked(ctx context.Context) error {
	if p == nil || p.self != p || p.closed || p.host.closed || p.host.active[p.run.ID] != nil || p.lease.file == nil || p.lease.work == nil || p.lease.recoveryErr != nil {
		return errAssistantRunScope
	}
	if p.borrowed && p.host.lease != p.lease || !p.borrowed && p.host.lease != nil {
		return errAssistantRunScope
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s := p.host.server
	project, e1 := s.root.Stat(".")
	settings, e2 := s.assistant.root.Stat(".")
	if e1 != nil || e2 != nil || !os.SameFile(project, p.projectInfo) || !os.SameFile(settings, p.workRootInfo) || !sameDirectory(s.projectDir, project) || !sameDirectory(s.configDir, settings) {
		return errors.Join(errAssistantRunScope, e1, e2)
	}
	for _, v := range []struct {
		name string
		file *os.File
		info os.FileInfo
	}{{".data-work.lock", p.lease.work, p.workInfo}, {"assistant-run-executor-" + p.lease.store.project + ".lock", p.lease.file, p.executorInfo}} {
		held, e1 := v.file.Stat()
		entry, e2 := s.assistant.root.Lstat(v.name)
		if e1 != nil || e2 != nil || !os.SameFile(held, v.info) || !os.SameFile(entry, v.info) || ownerOnlyFile(v.name, entry.Mode()) != nil || ownedByUs(v.name, entry) != nil {
			return errors.Join(errAssistantRunScope, e1, e2)
		}
	}
	return nil
}

func (p *assistantDialectReadRecoveryPin) Close() error {
	if p == nil {
		return nil
	}
	if p.self != p {
		return errAssistantRunScope
	}
	h := p.host
	h.mu.Lock()
	defer h.mu.Unlock()
	if p.closed {
		return p.closeErr
	}
	p.closed = true
	if h.dialectReadRecoveryPins <= 0 {
		p.closeErr = errAssistantRunScope
		return p.closeErr
	}
	h.dialectReadRecoveryPins--
	if !p.borrowed {
		p.closeErr = p.lease.Close()
		recoveryStorageStep("external-work-executor-closed")
		return p.closeErr
	}
	recoveryStorageStep("active-pin-released")
	if h.dialectReadRecoveryPins == 0 && len(h.active) == 0 && h.lease == p.lease {
		h.lease = nil
		p.closeErr = p.lease.Close()
		return p.closeErr
	}
	return nil
}

type assistantDialectReadRecoveryGuard struct {
	self                                       *assistantDialectReadRecoveryGuard
	pin                                        *assistantDialectReadRecoveryPin
	request                                    *http.Request
	identity                                   assistantRunBinding
	private                                    *privateDataShortLock
	privateBorrow                              *assistantDialectRecoveryPrivateHold
	outer                                      *dialectArtifactOwnerOuterHold
	data                                       *chatDataStore
	run                                        assistantRunRecord
	serviceHeld, writesHeld, leaseHeld, closed bool
	closeErr                                   error
}

func (p *assistantDialectReadRecoveryPin) acquire(r *http.Request) (_ *assistantDialectReadRecoveryGuard, err error) {
	ctx, s := r.Context(), p.host.server
	g := &assistantDialectReadRecoveryGuard{pin: p, request: r}
	g.self = g
	defer func() {
		if err != nil {
			err = errors.Join(err, g.Close())
		}
	}()
	// FIFO admission precedes every TryLock. No held prefix waits or retries a
	// semantic refusal; a busy caller makes a separately admitted observation.
	if p.storage != nil && p.storage.private != nil {
		err = p.storage.private.borrow(ctx, g)
	} else {
		g.private, err = s.privateDataLockContext(ctx, true)
		if err == nil {
			recoveryStorageStep("private-acquired")
		}
	}
	if err != nil {
		return nil, err
	}
	if !p.host.mu.TryLock() {
		return nil, errRetainedArtifactBusy
	}
	g.serviceHeld = true
	g.outer, err = s.acquireDialectArtifactOwnerOuter(ctx)
	if err != nil {
		return nil, err
	}
	if !s.writes.TryLock() {
		return nil, errRetainedArtifactBusy
	}
	g.writesHeld = true
	if !p.lease.mu.TryLock() {
		return nil, errRetainedArtifactBusy
	}
	g.leaseHeld = true
	if err = p.checkLocked(ctx); err != nil {
		return nil, err
	}
	g.identity, _, err = s.assistantRunIdentity(r)
	if err != nil {
		return nil, err
	}
	if err = s.authorizeAssistantRunStorage(r); err != nil {
		return nil, err
	}
	if g.identity.Principal != p.run.Binding.Principal {
		return nil, errAssistantRunScope
	}
	g.data, err = s.openChatData()
	if err != nil {
		return nil, err
	}
	if err = p.lease.store.checkScope(g.data.root); err != nil {
		return nil, err
	}
	ledger, err := p.lease.store.readLocked(ctx, g.data)
	if err != nil {
		return nil, err
	}
	bound, err := assistantRunBound(&ledger, p.run.Binding, p.run.ID)
	if err != nil || !reflect.DeepEqual(*bound, p.run) || !assistantRunTerminal(bound.State) {
		return nil, errors.Join(errAssistantRunConflict, err)
	}
	g.run = *bound
	if err = g.Check(ctx); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *assistantDialectReadRecoveryGuard) Check(ctx context.Context) error {
	if g == nil || g.self != g || g.closed || !g.serviceHeld || !g.writesHeld || !g.leaseHeld || g.private == nil || g.data == nil {
		return errAssistantRunScope
	}
	if g.privateBorrow != nil {
		if e := g.privateBorrow.checkBorrower(ctx, g); e != nil {
			return e
		}
	}
	if err := g.pin.checkLocked(ctx); err != nil {
		return err
	}
	if err := g.outer.check(); err != nil {
		return err
	}
	s := g.pin.host.server
	held, heldErr := g.private.Stat()
	entry, entryErr := s.assistant.root.Lstat(".data.lock")
	if heldErr != nil || entryErr != nil || !os.SameFile(held, entry) || ownerOnlyFile(".data.lock", held.Mode()) != nil || ownerOnlyFile(".data.lock", entry.Mode()) != nil || ownedByUs(".data.lock", held) != nil || ownedByUs(".data.lock", entry) != nil {
		return errors.Join(errAssistantRunScope, heldErr, entryErr)
	}
	current := s.assistantRunsLocked(g.data.root)
	if current.problem != nil || current.project != g.pin.lease.store.project || current.name != g.pin.lease.store.name {
		return errors.Join(errAssistantRunScope, current.problem)
	}
	identity, _, err := s.assistantRunIdentity(g.request.WithContext(ctx))
	if err != nil || !assistantRunIdentityMatches(identity, g.identity) || identity.Principal != g.run.Binding.Principal {
		return errors.Join(errAssistantRunScope, err)
	}
	if err = checkPrivateDataPath(g.data); err != nil {
		return err
	}
	return s.authorizeAssistantRunStorage(g.request.WithContext(ctx))
}

func (g *assistantDialectReadRecoveryGuard) Close() error {
	if g == nil {
		return nil
	}
	if g.closed {
		return g.closeErr
	}
	if g.data != nil {
		g.closeErr = errors.Join(g.closeErr, closeDialectArtifactOwner(context.Background(), "read-recovery-data", g.data.root.Close))
		g.data = nil
	}
	if g.private != nil {
		if g.privateBorrow != nil {
			g.closeErr = errors.Join(g.closeErr, g.privateBorrow.release(g))
			g.privateBorrow = nil
		} else {
			g.closeErr = errors.Join(g.closeErr, g.private.Close())
		}
		g.private = nil
	}
	if g.leaseHeld {
		g.pin.lease.mu.Unlock()
		g.leaseHeld = false
	}
	if g.writesHeld {
		g.pin.host.server.writes.Unlock()
		g.writesHeld = false
	}
	if g.outer != nil {
		g.closeErr = errors.Join(g.closeErr, g.outer.Close())
		g.outer = nil
	}
	if g.serviceHeld {
		g.pin.host.mu.Unlock()
		g.serviceHeld = false
	}
	g.closed = true
	return g.closeErr
}

// Observes only original native raw. No mutable source capture/result() call,
// Put, factory, model request or original acknowledgment exists on this path.
func (g *assistantDialectReadRecoveryGuard) observeNative(ctx context.Context, host string) (state, reason, provenance string, err error) {
	if err = g.Check(ctx); err != nil {
		return "unavailable", "ownership-mismatch", "", err
	}
	s := g.pin.host.server
	if s.retainedArtifacts != nil {
		return "unavailable", "provider-unavailable", "", errRetainedArtifactAuthorityUnavailable
	}
	if g.run.Historical {
		return "unavailable", "owner-unavailable", "", errRetainedArtifactAuthorityUnavailable
	}
	p := &assistantDialectCompleteProjectionRead{server: s, runID: g.run.ID, binding: g.run.Binding, work: g.pin.lease.work, folderInfo: g.pin.workRootInfo, workInfo: g.pin.workInfo}
	p.self = p
	evidence := &assistantDialectCompletedReadEvidence{projection: p}
	if err = evidence.nativeProvenance(ctx, g.run); err != nil {
		return "unavailable", "ownership-mismatch", "", err
	}
	provenance = digestOf(assistantChatJSON([]any{g.run.DialectArtifactOwner, g.run.Binding.Project, identityKey(g.pin.projectInfo), identityKey(g.pin.workRootInfo), identityKey(g.pin.workInfo)}))
	// Even a never-reserved read requires original native work provenance before
	// a definitive absence. A missing owner or substituted work name is not
	// physical absence evidence, and cannot borrow a new named inode's EX.
	held := assistantDialectCompleteReadOf(&g.run, host)
	if held == nil {
		return "absent", "", provenance, nil
	}
	raw, readErr := s.readAssistantDialectCompleteNativeRaw(ctx, held.Intent.Reference)
	finalErr := errors.Join(evidence.nativeProvenance(ctx, g.run), g.Check(ctx))
	if finalErr != nil {
		return "unavailable", "ownership-mismatch", provenance, errors.Join(readErr, finalErr)
	}
	if readErr != nil {
		if injectedArtifactOnlyNotExist(readErr) {
			return "absent", "", provenance, nil
		}
		if assistantDialectReadRecoveryOnlyChanged(readErr) {
			return "corrupt", "", provenance, nil
		}
		return "unavailable", "storage-io", provenance, readErr
	}
	decoded, decodeErr := readAssistantDialectCompleteRead(ctx, raw)
	if decodeErr != nil || decoded.Snapshot.ID != held.InputID || decoded.Snapshot.Revision != held.Revision || decoded.Snapshot.PackKey != held.PackKey || "sha256:"+digestOf([]byte(decoded.Snapshot.PackJSON)) != held.PackDigest || digestOf(assistantDialectCompleteReadMetadata(decoded.Snapshot)) != held.MetadataSHA256 || !reflect.DeepEqual(decoded.Snapshot.ResearchIndex, held.ResearchIndex) {
		return "corrupt", "", provenance, nil
	}
	return "present-valid", "", provenance, nil
}

// A content mismatch is definitive only when every error leaf proves that
// mismatch. Joined Close/I/O uncertainty must survive, just like joined absence.
func assistantDialectReadRecoveryOnlyChanged(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		all := joined.Unwrap()
		if len(all) == 0 {
			return false
		}
		for _, child := range all {
			if !assistantDialectReadRecoveryOnlyChanged(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return assistantDialectReadRecoveryOnlyChanged(wrapped.Unwrap())
	}
	return err == errRetainedArtifactChanged
}
