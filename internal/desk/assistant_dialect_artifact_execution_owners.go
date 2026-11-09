package desk

import (
	"context"
	"errors"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"os"
	"reflect"
)

// Inactive provenance enrollment for the original registered execution. No
// constructor or effect path calls this entry. It confers no action authority.
// The original service SH-work/EX-executor lease is borrowed only while l.mu
// is owned; it is never reopened, upgraded, relocked or closed here.
func (e *assistantRunExecution) enrollDialectArtifactOwner(ctx context.Context) (dialectArtifactOwnerEnrollment, error) {
	return e.withDialectArtifactOwner(ctx, nil)
}
func (e *assistantRunExecution) withDialectArtifactOwner(ctx context.Context, use *dialectArtifactInjectedUse) (dialectArtifactOwnerEnrollment, error) {
	return e.withDialectArtifactEnrollment(ctx, use, nil, nil)
}
func (e *assistantRunExecution) withDialectArtifactCompleteReadOwner(ctx context.Context, host string, use *dialectArtifactInjectedUse, short *assistantDialectShortOriginal) (dialectArtifactOwnerEnrollment, error) {
	if e == nil || e.writer == nil || e.writer.lease == nil {
		return dialectArtifactOwnerEnrollment{}, errAssistantRunScope
	}
	physical, ok := ctx.Value(assistantExternalPhysicalKey{}).(assistantExternalPhysical)
	generation, live := ctx.Value(assistantInvocationGenerationKey{}).(assistantInvocationGeneration)
	root, _ := ctx.Value(assistantHostRootKey{}).(bool)
	if !ok || !live || generation.owner != e || generation.finalizer || generation.number == 0 || !root || physical.ID != host || physical.Name != "read_dialect" || runcontext.CallIDOf(ctx) != physical.Intent.CallID {
		return dialectArtifactOwnerEnrollment{}, errAssistantRunScope
	}
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return dialectArtifactOwnerEnrollment{}, err
	}
	op, in, err := assistantDialectCompleteReadRoot(run, host)
	if err != nil || op.State != "dispatched" || !reflect.DeepEqual(in, physical.Intent) {
		return dialectArtifactOwnerEnrollment{}, errAssistantRunScope
	}
	if short == nil || short.execution != e || short.writer != e.writer || short.lease != e.writer.lease {
		return dialectArtifactOwnerEnrollment{}, errAssistantRunScope
	}
	return e.withDialectArtifactEnrollment(ctx, use, &in, short)
}
func (e *assistantRunExecution) withDialectArtifactEnrollment(ctx context.Context, use *dialectArtifactInjectedUse, readRoot *assistantHostIntent, short *assistantDialectShortOriginal) (out dialectArtifactOwnerEnrollment, err error) {
	defer func() {
		if err != nil {
			out = dialectArtifactOwnerEnrollment{}
		}
	}()
	if ctx == nil || e == nil || e.service == nil || e.service.server == nil {
		return out, errDialectArtifactOwner
	}
	h := e.service
	s := h.server
	registration, operation, err := s.prepareInjectedArtifactEnrollment(ctx)
	if err != nil {
		return out, err
	}
	if use != nil && registration == nil {
		return out, errRetainedArtifactAuthorityUnavailable
	}
	if operation != nil {
		defer func() { err = errors.Join(err, operation.finish()) }()
	}
	var custody *assistantDialectShortCustody
	if short != nil {
		custody, err = short.acquire(ctx, assistantDialectShortEnrollment)
		if err != nil {
			return out, err
		}
		defer func() { err = errors.Join(err, custody.Close()) }()
		if err = short.checkOuter(custody.outer); err != nil {
			return out, err
		}
		if err = short.checkPrivate(custody); err != nil {
			return out, err
		}
	} else {
		if !h.mu.TryLock() {
			return out, errDialectArtifactOwner
		}
		defer h.mu.Unlock()
	}
	if s == nil || h.closed || e.finalizing || h.active[e.record.ID] != e ||
		e.ctx == nil || e.request == nil ||
		s.project == nil || s.project.own == nil || s.root == nil || s.root != s.project.own.root ||
		s.assistant == nil || !s.assistant.usable() || s.writes == nil || s.sessions == nil {
		return out, errDialectArtifactOwner
	}
	request := e.request
	project, assistant, root := s.project, s.assistant, s.root
	assistantRoot := assistant.root
	checkPhysical := func() error {
		if ctx.Err() != nil || e.ctx.Err() != nil {
			return errors.Join(ctx.Err(), e.ctx.Err())
		}
		if e.request != request || e.service != h || h.server != s || h.closed || e.finalizing || h.active[e.record.ID] != e ||
			s.project != project || s.assistant != assistant || s.root != root || assistant.root != assistantRoot ||
			s.root != project.own.root || !sameDirectory(s.projectDir, project.info) {
			return errDialectArtifactOwner
		}
		return nil
	}
	check := func() error {
		if err := checkPhysical(); err != nil {
			return err
		}
		return s.authorizeAssistantRunStorage(request.WithContext(ctx))
	}
	if err = check(); err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, check()) }()
	useOuter := func(owner *Server, home *dialectArtifactHomeHold, outerCheck, outerLeaf func() error) (out dialectArtifactOwnerEnrollment, err error) {
		var w *assistantRunWriter
		var l *assistantRunLease
		if short == nil {
			if !s.writes.TryLock() {
				return out, errDialectArtifactOwner
			}
			defer s.writes.Unlock()
			if !e.canonicalMu.TryLock() {
				return out, errDialectArtifactOwner
			}
			defer e.canonicalMu.Unlock()
			w = e.writer
			if w == nil || !w.mu.TryLock() {
				return out, errDialectArtifactOwner
			}
			defer w.mu.Unlock()
			l = w.lease
			if l == nil || !l.mu.TryLock() {
				return out, errDialectArtifactOwner
			}
			defer l.mu.Unlock()
		} else {
			w, l = short.writer, short.lease
		}
		binding, canonical, generation := e.canonicalBinding, e.canonicalStore, e.generation
		store := l.store
		work, executor := l.work, l.file
		if work == nil || executor == nil {
			return out, errDialectArtifactOwner
		}
		workInfo, err := work.Stat()
		if err != nil {
			return out, err
		}
		executorInfo, err := executor.Stat()
		if err != nil {
			return out, err
		}
		workRoot, err := assistant.root.Stat(".")
		if err != nil {
			return out, err
		}
		checkInnerPhysical := func() error {
			if e.writer != w || w.lease != l || h.lease != l || l.store.server != s ||
				l.store.project != store.project || l.store.name != store.name || l.store.problem != nil || store.problem != nil || !assistantRunDigest(store.project) ||
				!assistantRunFileName.MatchString(store.name) || store.name != "assistant-runs-"+store.project+".json" ||
				l.work != work || l.file != executor || l.recoveryErr != nil || w.failure != nil ||
				w.id != e.record.ID || w.binding.Project != store.project ||
				!reflect.DeepEqual(w.binding, e.record.Binding) || !reflect.DeepEqual(w.binding, e.source.Binding) ||
				e.source.Start.Purpose != "chat" || e.lifecycle != "running" || e.stopRequested ||
				e.observation != nil || generation == 0 || e.generation != generation ||
				e.canonicalBinding != binding || e.canonicalStore != canonical {
				return errDialectArtifactOwner
			}
			if g, ok := ctx.Value(assistantInvocationGenerationKey{}).(assistantInvocationGeneration); ok &&
				(g.owner != e || g.number != generation || g.finalizer) {
				return errDialectArtifactOwner
			}
			if binding != nil && (binding.Writer == nil || canonical == nil || binding.Writer.Store != canonical ||
				canonical.writer != w || canonical.id != binding.Writer.ID) || binding == nil && canonical != nil {
				return errDialectArtifactOwner
			}
			if !sameDirectory(s.configDir, workRoot) {
				return errDialectArtifactOwner
			}
			actualRoot, err := assistant.root.Stat(".")
			if err != nil || !os.SameFile(actualRoot, workRoot) {
				return errors.Join(errDialectArtifactOwner, err)
			}
			for _, pin := range []struct {
				file *os.File
				info os.FileInfo
				name string
			}{{work, workInfo, ".data-work.lock"}, {executor, executorInfo, "assistant-run-executor-" + store.project + ".lock"}} {
				held, statErr := pin.file.Stat()
				entry, entryErr := assistant.root.Lstat(pin.name)
				if statErr != nil || entryErr != nil {
					return errors.Join(errDialectArtifactOwner, statErr, entryErr)
				}
				if !os.SameFile(held, pin.info) || !os.SameFile(entry, pin.info) ||
					ownerOnlyFile(pin.name, held.Mode()) != nil || ownedByUs(pin.name, held) != nil ||
					ownerOnlyFile(pin.name, entry.Mode()) != nil || ownedByUs(pin.name, entry) != nil {
					return errDialectArtifactOwner
				}
			}
			return nil
		}
		leafPins := func() error {
			if err := outerLeaf(); err != nil {
				return err
			}
			return checkInnerPhysical()
		}
		checkPins := func() error {
			if err := outerCheck(); err != nil {
				return err
			}
			if err := checkInnerPhysical(); err != nil {
				return err
			}
			return s.authorizeAssistantRun(request.WithContext(ctx), assistantRunRecord{Binding: w.binding})
		}
		if err = checkPins(); err != nil {
			return out, err
		}
		// Registered before private: all borrowed Stat and pointer checks finish
		// under canonical/writer/lease custody, after leaf and private cleanup.
		defer func() { err = errors.Join(err, checkPins()) }()
		var private *privateDataShortLock
		if short == nil {
			private, err = s.tryPrivateDataLockContext(ctx, true)
			if err != nil {
				return out, err
			}
		} else {
			private = custody.private
		}
		defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "private", private.Close)) }()
		validateData := func(data *chatDataStore) error {
			if err := checkPins(); err != nil {
				return err
			}
			current := s.assistantRunsLocked(data.root)
			if current.problem != nil || current.project != store.project || current.name != store.name {
				return errors.Join(errDialectArtifactOwner, current.problem)
			}
			if err := store.checkScope(data.root); err != nil {
				return err
			}
			if err := checkPrivateDataPath(data); err != nil {
				return err
			}
			ledger, err := store.readLocked(ctx, data)
			if err != nil {
				return err
			}
			run, err := assistantRunBound(&ledger, w.binding, w.id)
			if err != nil {
				return err
			}
			if run.ID != e.record.ID || run.Revision != w.revision || !(readRoot == nil && assistantDialectDraftLiveRun(*run) || readRoot != nil && assistantDialectCompleteReadLiveRun(*run)) {
				return errDialectArtifactOwner
			}
			if readRoot != nil {
				physical, ok := ctx.Value(assistantExternalPhysicalKey{}).(assistantExternalPhysical)
				op, in, e := assistantDialectCompleteReadRoot(*run, physical.ID)
				if !ok || e != nil || op.State != "dispatched" || !reflect.DeepEqual(in, *readRoot) || !reflect.DeepEqual(in, physical.Intent) {
					return errAssistantRunScope
				}
			}
			if run.CanonicalRequired || assistantCanonicalActive(*run) {
				if !assistantCanonicalActive(*run) || binding == nil || canonical == nil ||
					canonical.id != run.Canonical.Head.RunID || canonical.writer != w ||
					binding.Writer.Store != canonical || binding.Writer.ID != canonical.id {
					return errDialectArtifactOwner
				}
			} else if binding != nil || canonical != nil {
				return errDialectArtifactOwner
			}
			identity, _, err := s.assistantRunIdentity(request.WithContext(ctx))
			if err != nil {
				return err
			}
			fresh, err := s.resolveAssistantRun(request.WithContext(ctx), e.source.Start, identity, store.project)
			if err != nil {
				return err
			}
			fresh.Binding.ExecutionKey, fresh.Binding.Unit = e.source.Binding.ExecutionKey, e.source.Binding.Unit
			if (readRoot == nil && fresh.Binding.Phase != "work" || readRoot != nil && fresh.Binding.Phase != e.source.Binding.Phase) || !assistantRunFreshSourceEqual(e.source, fresh) {
				return errDialectArtifactOwner
			}
			return nil
		}
		return s.enrollDialectArtifactOwnerLocked(ctx, owner, home, work, checkPins, leafPins, validateData, request.WithContext(ctx), registration, operation, use)
	}
	if short != nil {
		return custody.outer.use(check, checkPhysical, useOuter)
	}
	return s.withDialectArtifactOwnerOuter(ctx, check, checkPhysical, useOuter)
}
