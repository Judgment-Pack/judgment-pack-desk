package desk

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"time"
)

// This closed classification is emitted only by the two TryLock sites below.
// Semantic, private-lock, ownership and cleanup errors never enter this class.
type assistantDialectShortBusy uint8

const (
	assistantDialectShortNotBusy assistantDialectShortBusy = iota
	assistantDialectShortCanonicalBusy
	assistantDialectShortWriterBusy
)

type assistantDialectShortKind uint8

const (
	assistantDialectShortSeal assistantDialectShortKind = iota + 1
	assistantDialectShortEnrollment
	assistantDialectShortTransaction
)

// Original anchors are captured under the original service and outer custody,
// before the first wait. They are observations, never authority or new leases.
type assistantDialectShortOriginal struct {
	execution                             *assistantRunExecution
	request                               *http.Request
	ownerContext                          context.Context
	runID                                 string
	service                               *assistantRunService
	server                                *Server
	writer                                *assistantRunWriter
	lease                                 *assistantRunLease
	store                                 assistantRunStore
	work, executor                        *os.File
	project                               *ProjectRoot
	assistant                             *assistantStore
	root, workRoot                        *os.Root
	projectInfo, workInfo                 os.FileInfo
	projectDir, configDir                 string
	owner                                 *Server
	home, ownerHome                       *deskHome
	homeLease                             *os.File
	homeInfo, homeLeaseInfo, registryInfo os.FileInfo
	registryRaw                           []byte
	homeEntry                             deskHomeEntry
	homeRecord                            dialectArtifactHomeAnchorRecord
}

func captureAssistantDialectShortOriginal(ctx context.Context, e *assistantRunExecution, w *assistantRunWriter, l *assistantRunLease) (_ *assistantDialectShortOriginal, err error) {
	if ctx == nil || e == nil || e.service == nil || w == nil || l == nil {
		return nil, errAssistantRunScope
	}
	h := e.service
	if !h.mu.TryLock() {
		return nil, errDialectArtifactOwner
	}
	defer h.mu.Unlock()
	s := h.server
	if s == nil || h.closed || e.finalizing || h.active[e.record.ID] != e || e.writer != w || w.lease != l || s.project == nil || s.assistant == nil || s.root == nil || s.writes == nil {
		return nil, errDialectArtifactOwner
	}
	outer, err := s.acquireDialectArtifactOwnerOuter(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, outer.Close()) }()
	if !l.mu.TryLock() {
		return nil, errDialectArtifactOwner
	}
	defer l.mu.Unlock()
	a := &assistantDialectShortOriginal{execution: e, service: h, server: s, writer: w, lease: l, project: s.project, assistant: s.assistant, root: s.root, workRoot: s.assistant.root, projectDir: s.projectDir, configDir: s.configDir, owner: outer.owner, home: s.home, ownerHome: outer.owner.home, homeLease: outer.owner.homeLease}
	a.request, a.ownerContext, a.runID = e.request, e.ctx, e.record.ID
	a.store, a.work, a.executor = l.store, l.work, l.file
	a.projectInfo, err = a.root.Stat(".")
	if err != nil {
		return nil, err
	}
	a.workInfo, err = a.workRoot.Stat(".")
	if err != nil {
		return nil, err
	}
	if outer.home != nil {
		a.homeInfo, a.homeLeaseInfo, a.registryInfo = outer.home.homeInfo, outer.home.leaseInfo, outer.home.registryInfo
		a.registryRaw = bytes.Clone(outer.home.registryRaw)
		a.homeEntry, a.homeRecord = outer.home.entry, outer.home.record
	}
	if err = a.checkOuter(outer); err != nil {
		return nil, err
	}
	return a, nil
}

// Called only after the complete prefix, while service and outer custody are
// still held. No newer home, root, registry or owner can replace these anchors.
func (a *assistantDialectShortOriginal) checkOuter(g *dialectArtifactOwnerOuterHold) error {
	s, h := a.server, a.service
	if g == nil || g.closed || g.s != s || g.owner != a.owner || h.server != s || h.closed || a.execution.finalizing || h.active[a.runID] != a.execution || a.execution.service != h || a.execution.request != a.request || a.execution.ctx != a.ownerContext || a.execution.record.ID != a.runID || a.execution.writer != a.writer || a.writer.lease != a.lease ||
		s.project != a.project || s.assistant != a.assistant || s.root != a.root || s.assistant.root != a.workRoot || s.projectDir != a.projectDir || s.configDir != a.configDir || s.home != a.home || g.owner.home != a.ownerHome || g.owner.homeLease != a.homeLease {
		return errAssistantRunScope
	}
	if err := a.checkRoots(); err != nil {
		return err
	}
	if (g.home == nil) != (a.homeInfo == nil) {
		return errAssistantRunScope
	}
	if g.home != nil && (!os.SameFile(g.home.homeInfo, a.homeInfo) || !os.SameFile(g.home.leaseInfo, a.homeLeaseInfo) || !os.SameFile(g.home.registryInfo, a.registryInfo) || !bytes.Equal(g.home.registryRaw, a.registryRaw) || !sameDialectArtifactHomeBinding(g.home.entry, a.homeEntry) || g.home.record.Version != a.homeRecord.Version || g.home.record.Kind != a.homeRecord.Kind || g.home.record.ID != a.homeRecord.ID || !bytes.Equal(g.home.record.PathBytes, a.homeRecord.PathBytes) || g.home.record.FolderIdentity != a.homeRecord.FolderIdentity) {
		return errAssistantRunScope
	}
	return nil
}
func (a *assistantDialectShortOriginal) checkRoots() error {
	s := a.server
	if s.project != a.project || s.assistant != a.assistant || s.root != a.root || s.assistant.root != a.workRoot || s.projectDir != a.projectDir || s.configDir != a.configDir || !sameDirectory(a.projectDir, a.projectInfo) || !sameDirectory(a.configDir, a.workInfo) {
		return errAssistantRunScope
	}
	project, err := a.root.Stat(".")
	if err != nil || !os.SameFile(project, a.projectInfo) {
		return errors.Join(errAssistantRunScope, err)
	}
	work, err := a.workRoot.Stat(".")
	if err != nil || !os.SameFile(work, a.workInfo) {
		return errors.Join(errAssistantRunScope, err)
	}
	return nil
}

type assistantDialectShortCustody struct {
	private       *privateDataShortLock
	outer         *dialectArtifactOwnerOuterHold
	releases      []func()
	serviceUnlock func()
	closed        bool
	closeErr      error
}

func (g *assistantDialectShortCustody) Close() error {
	if g == nil {
		return nil
	}
	if g.closed {
		return g.closeErr
	}
	g.closed = true
	if g.private != nil {
		g.closeErr = errors.Join(g.closeErr, g.private.Close())
	}
	for i := len(g.releases) - 1; i >= 0; i-- {
		g.releases[i]()
	}
	g.releases = nil
	if g.outer != nil {
		g.closeErr = errors.Join(g.closeErr, g.outer.Close())
	}
	if g.serviceUnlock != nil {
		g.serviceUnlock()
		g.serviceUnlock = nil
	}
	return g.closeErr
}
func (g *assistantDialectShortCustody) withdraw(busy assistantDialectShortBusy, acquisitionErr error) (bool, error) {
	closeErr := g.Close()
	if acquisitionErr != nil || closeErr != nil {
		return false, errors.Join(acquisitionErr, closeErr)
	}
	if busy != assistantDialectShortCanonicalBusy && busy != assistantDialectShortWriterBusy {
		return false, errAssistantRunScope
	}
	return true, nil
}

// checkPrivate runs after admission, under the complete original custody. The
// actual staged descriptor must match this same original server/root entry.
func (a *assistantDialectShortOriginal) checkPrivate(g *assistantDialectShortCustody) error {
	if g == nil || g.closed || g.private == nil || a.lease.store.problem != nil || a.store.problem != nil || a.lease.store.server != a.store.server || a.lease.store.project != a.store.project || a.lease.store.name != a.store.name || a.store.server != a.server || a.lease.work != a.work || a.lease.file != a.executor || a.execution.writer != a.writer || a.writer.lease != a.lease {
		return errAssistantRunScope
	}
	if err := a.checkRoots(); err != nil {
		return err
	}
	held, err := g.private.Stat()
	if err != nil {
		return err
	}
	entry, err := a.workRoot.Lstat(".data.lock")
	if err != nil {
		return err
	}
	if !os.SameFile(held, entry) || ownerOnlyFile("private data lock", held.Mode()) != nil || ownedByUs("private data lock", held) != nil || ownerOnlyFile("private data lock", entry.Mode()) != nil || ownedByUs("private data lock", entry) != nil {
		return errAssistantRunScope
	}
	return nil
}

// tryLocks has no authorization, source/run validation, provider or mutation
// callback. The only repeated semantic checks are the existing physical home
// acquisition checks inside acquireDialectArtifactOwnerOuter. All stay terminal.
func (a *assistantDialectShortOriginal) tryLocks(ctx context.Context, g *assistantDialectShortCustody, kind assistantDialectShortKind) (assistantDialectShortBusy, error) {
	s, e, w, l := a.server, a.execution, a.writer, a.lease
	refusal := errRetainedArtifactBusy
	if kind == assistantDialectShortEnrollment {
		refusal = errDialectArtifactOwner
	}
	if kind == assistantDialectShortEnrollment {
		if !a.service.mu.TryLock() {
			return assistantDialectShortNotBusy, errDialectArtifactOwner
		}
		// Service must outlive outer; release it separately after outer.Close.
		g.serviceUnlock = a.service.mu.Unlock
		var err error
		g.outer, err = s.acquireDialectArtifactOwnerOuter(ctx)
		if err != nil {
			return assistantDialectShortNotBusy, err
		}
	}
	if kind != assistantDialectShortSeal {
		if !s.writes.TryLock() {
			return assistantDialectShortNotBusy, refusal
		}
		g.releases = append(g.releases, s.writes.Unlock)
	}
	if !e.canonicalMu.TryLock() {
		return assistantDialectShortCanonicalBusy, nil
	}
	g.releases = append(g.releases, e.canonicalMu.Unlock)
	if !w.mu.TryLock() {
		return assistantDialectShortWriterBusy, nil
	}
	g.releases = append(g.releases, w.mu.Unlock)
	if !l.mu.TryLock() {
		return assistantDialectShortNotBusy, refusal
	}
	g.releases = append(g.releases, l.mu.Unlock)
	return assistantDialectShortNotBusy, nil
}

// No operation is invoked here. A busy prefix releases every real hold before
// its cancellable delay and a new FIFO claim. A single deadline covers all
// attempts, the queue, OS custody and delays. Normal guards keep original ctx.
func (a *assistantDialectShortOriginal) acquire(ctx context.Context, kind assistantDialectShortKind) (*assistantDialectShortCustody, error) {
	if a == nil || ctx == nil || kind < assistantDialectShortSeal || kind > assistantDialectShortTransaction {
		return nil, errAssistantRunScope
	}
	admission, cancel := context.WithTimeout(ctx, privateDataLockWait)
	defer cancel()
	delay := 5 * time.Millisecond
	for {
		if err := admission.Err(); err != nil {
			return nil, err
		}
		g := &assistantDialectShortCustody{}
		if kind != assistantDialectShortSeal {
			var err error
			g.private, err = a.server.privateDataLockContext(admission, true)
			if err != nil {
				return nil, err
			}
		}
		busy, err := a.tryLocks(ctx, g, kind)
		if err == nil && busy == assistantDialectShortNotBusy {
			err = admission.Err()
		}
		if err == nil && busy == assistantDialectShortNotBusy {
			return g, nil
		}
		retry, err := g.withdraw(busy, err)
		if err != nil {
			return nil, err
		}
		if !retry {
			return nil, errAssistantRunScope
		}
		timer := time.NewTimer(delay)
		select {
		case <-admission.Done():
			timer.Stop()
			return nil, admission.Err()
		case <-timer.C:
		}
		if delay < 100*time.Millisecond {
			delay *= 2
			if delay > 100*time.Millisecond {
				delay = 100 * time.Millisecond
			}
		}
	}
}
