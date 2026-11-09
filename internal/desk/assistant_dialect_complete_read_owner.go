package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// Only this original actual dispatched read invocation can create the owner.
// It owns its captured roots, executor/work FDs and exact source observation;
// no decoded selector or provider callback can construct publication authority.
type assistantDialectCompleteReadOwner struct {
	self                                         *assistantDialectCompleteReadOwner
	enrollment                                   dialectArtifactOwnerEnrollment
	short                                        *assistantDialectShortOriginal
	registration                                 *retainedArtifactProviderRegistration
	injected                                     *boundInjectedArtifactRepository
	captured                                     func() error
	execution                                    *assistantRunExecution
	writer                                       *assistantRunWriter
	lease                                        *assistantRunLease
	binding                                      assistantRunBinding
	runID, host                                  string
	generation                                   uint64
	root                                         assistantHostIntent
	intentRaw                                    []byte
	objects                                      guardedRetainedArtifactRepository
	inspection                                   retainedArtifactRepository
	raw                                          []byte
	snapshot                                     runcontext.DialectSnapshot
	read                                         assistantDialectRead
	result                                       runcontext.ToolResult
	held                                         assistantDialectCompleteReadCustody
	reserved, published, inspected, acknowledged bool
	mu                                           sync.Mutex
}

func newAssistantDialectCompleteReadOwner(ctx context.Context, e *assistantRunExecution, host string) (*assistantDialectCompleteReadOwner, error) {
	if e == nil || e.writer == nil || e.writer.lease == nil || e.service == nil || e.service.server == nil || e.ctx == nil || e.request == nil || e.source.Start.Purpose != "chat" {
		return nil, errAssistantRunScope
	}
	g, ok := ctx.Value(assistantInvocationGenerationKey{}).(assistantInvocationGeneration)
	physical, dispatched := ctx.Value(assistantExternalPhysicalKey{}).(assistantExternalPhysical)
	root, _ := ctx.Value(assistantHostRootKey{}).(bool)
	if !ok || g.owner != e || g.number == 0 || g.finalizer || !root || !dispatched || physical.ID != host || physical.Name != "read_dialect" || runcontext.CallIDOf(ctx) != physical.Intent.CallID {
		return nil, errAssistantRunScope
	}
	if err := e.Authorize(ctx); err != nil {
		return nil, err
	}
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return nil, err
	}
	opValue, in, err := assistantDialectCompleteReadRoot(run, host)
	_ = opValue
	op := assistantDialectOperation(run, host)
	if err != nil || run.DialectArtifactOwner == "" && (len(run.DialectDrafts) != 0 || len(run.DialectReads) != 0) || op == nil || op.State != "dispatched" || !reflect.DeepEqual(in, physical.Intent) || !assistantDialectCompleteReadLiveRun(run) || assistantDialectCompleteReadOf(&run, host) != nil {
		return nil, errAssistantRunScope
	}
	// Source capture has no raw effect. Exact shared-object exclusion is checked
	// under original reservation/publication custody after its reference is known.
	// Copy the original request facts before enrollment. The lexical enrollment
	// callback only captures storage identity; it cannot publish an object.
	var binding assistantRunBinding
	var copied assistantHostIntent
	if json.Unmarshal(assistantChatJSON(run.Binding), &binding) != nil || readAssistantHostReceipt(assistantChatJSON(in), &copied) != nil {
		return nil, errAssistantRunInvalid
	}
	s := e.service.server
	o := &assistantDialectCompleteReadOwner{execution: e, writer: e.writer, lease: e.writer.lease, binding: binding, runID: run.ID, host: host, generation: g.number, root: copied, intentRaw: bytes.Clone(op.Intent)}
	o.self = o
	registration, err := s.injectedArtifactRegistration()
	if err != nil {
		return nil, err
	}
	o.short, err = captureAssistantDialectShortOriginal(ctx, e, o.writer, o.lease)
	if err != nil {
		return nil, err
	}
	if registration == nil {
		o.enrollment, err = e.withDialectArtifactCompleteReadOwner(ctx, host, nil, o.short)
	} else {
		selection, selectErr := newRetainedArtifactInjectedSelection(nil, false)
		if selectErr != nil {
			return nil, selectErr
		}
		var provisional *boundInjectedArtifactRepository
		o.enrollment, err = e.withDialectArtifactCompleteReadOwner(ctx, host, &dialectArtifactInjectedUse{selection: selection, use: func(v *retainedArtifactInjectedView) error {
			var bindErr error
			provisional, bindErr = v.boundDialectCompleteReadRepository(ctx, o)
			return bindErr
		}}, o.short)
		// Accept nothing until all lexical producer cleanup has succeeded.
		if err == nil && (provisional == nil || provisional.registration != registration || provisional.binding.Owner != o.enrollment.Owner || provisional.binding.Namespace != o.enrollment.Namespace) {
			err = errRetainedArtifactAuthorityUnavailable
		}
		if err == nil {
			o.injected = provisional
		}
	}
	if err != nil {
		return nil, err
	}
	if err = dialectArtifactOwnerStep(ctx, "factory-after-owner-enrollment"); err != nil {
		return nil, err
	}
	o.registration = registration
	if !assistantRunDigest(o.enrollment.Owner) || o.enrollment.Namespace != binding.Project || run.DialectArtifactOwner != "" && run.DialectArtifactOwner != o.enrollment.Owner {
		return nil, errAssistantRunScope
	}
	seal, err := o.short.acquire(ctx, assistantDialectShortSeal)
	if err != nil {
		return nil, err
	}
	defer seal.Close()
	if e.generation != g.number || e.lifecycle != "running" || e.stopRequested || e.observation != nil {
		return nil, errAssistantRunScope
	}
	w, l := o.writer, o.lease
	if e.writer != w || w.lease != l || w != o.writer || l != o.lease || w.failure != nil || l.file == nil || l.work == nil || l.recoveryErr != nil || w.revision != run.Revision || w.id != run.ID || !reflect.DeepEqual(w.binding, run.Binding) {
		return nil, errAssistantRunScope
	}
	project, assistant, physicalRoot := o.short.project, o.short.assistant, o.short.root
	work, executor, store := l.work, l.file, l.store
	canonical, canonicalBinding := e.canonicalStore, e.canonicalBinding
	workInfo, err := work.Stat()
	if err != nil {
		return nil, err
	}
	executorInfo, err := executor.Stat()
	if err != nil {
		return nil, err
	}
	workRoot, err := assistant.root.Stat(".")
	if err != nil {
		return nil, err
	}
	projectRoot, err := physicalRoot.Stat(".")
	if err != nil {
		return nil, err
	}
	enrolled := o.enrollment
	sealedBinding, sealedRoot := bytes.Clone(assistantChatJSON(binding)), bytes.Clone(assistantChatJSON(copied))
	sealedIntent := bytes.Clone(op.Intent)
	// Join the acknowledged producer facts to the actual captured roots/FD,
	// rather than accepting a replacement observed after enrollment.
	if identityKey(projectRoot) != enrolled.ProjectIdentity || identityKey(workRoot) != enrolled.WorkIdentity || dialectArtifactWorkDomainID(identityKey(workRoot), identityKey(workInfo)) != enrolled.WorkDomain {
		return nil, errAssistantRunScope
	}
	o.captured = func() error {
		if o.enrollment != enrolled || o.execution != e || o.writer != w || o.lease != l || o.runID != run.ID || o.host != host || o.registration != registration || !bytes.Equal(assistantChatJSON(o.binding), sealedBinding) || !bytes.Equal(assistantChatJSON(o.root), sealedRoot) || !bytes.Equal(o.intentRaw, sealedIntent) {
			return errAssistantRunScope
		}
		if s.project != project || s.assistant != assistant || s.root != physicalRoot || physicalRoot != project.own.root || !sameDirectory(s.projectDir, project.info) || !sameDirectory(s.configDir, workRoot) ||
			l.work != work || l.file != executor || l.store.server != s || l.store.project != store.project || l.store.name != store.name || l.store.problem != nil ||
			e.canonicalStore != canonical || e.canonicalBinding != canonicalBinding || store.project != o.enrollment.Namespace {
			return errAssistantRunScope
		}
		if canonicalBinding != nil && (canonicalBinding.Writer == nil || canonical == nil || canonicalBinding.Writer.Store != canonical || canonical.writer != w || canonical.id != canonicalBinding.Writer.ID) || canonicalBinding == nil && canonical != nil {
			return errAssistantRunScope
		}
		current, err := s.injectedArtifactRegistration()
		if err != nil || current != registration {
			return errors.Join(errRetainedArtifactAuthorityUnavailable, err)
		}
		if registration != nil {
			if err := registration.current(); err != nil {
				return err
			}
		}
		actualProject, err := physicalRoot.Stat(".")
		if err != nil || !os.SameFile(actualProject, projectRoot) || identityKey(actualProject) != enrolled.ProjectIdentity {
			return errors.Join(errAssistantRunScope, err)
		}
		actualRoot, err := assistant.root.Stat(".")
		if err != nil || !os.SameFile(actualRoot, workRoot) {
			return errors.Join(errAssistantRunScope, err)
		}
		for _, pin := range []struct {
			file *os.File
			info os.FileInfo
			name string
		}{{work, workInfo, ".data-work.lock"}, {executor, executorInfo, "assistant-run-executor-" + store.project + ".lock"}} {
			held, statErr := pin.file.Stat()
			entry, entryErr := assistant.root.Lstat(pin.name)
			if statErr != nil || entryErr != nil {
				return errors.Join(errAssistantRunScope, statErr, entryErr)
			}
			if !os.SameFile(held, pin.info) || !os.SameFile(entry, pin.info) || ownerOnlyFile(pin.name, held.Mode()) != nil || ownedByUs(pin.name, held) != nil || ownerOnlyFile(pin.name, entry.Mode()) != nil || ownedByUs(pin.name, entry) != nil {
				observeAssistantDialectCompleteRead(ctx, assistantDialectCompleteReadObservedPin, errAssistantRunScope)
				return errAssistantRunScope
			}
		}
		return nil
	}
	if err = o.captured(); err != nil {
		return nil, err
	}
	if registration == nil {
		o.objects = s.guardedArtifactRepository()
		o.inspection = s.artifactRepositoryWithAuthority(e.authorizeAssistantDialect)
	} else {
		o.objects, o.inspection = o.injected, o.injected
	}
	return o, nil
}

func (o *assistantDialectCompleteReadOwner) original() bool {
	return o != nil && o.self == o && o.execution != nil && o.writer == o.execution.writer && o.lease == o.writer.lease && o.captured != nil && o.objects != nil
}
func (o *assistantDialectCompleteReadOwner) capture(ctx context.Context, a assistantDialectArguments) (err error) {
	if !o.original() || a.ID == "" || a.PackKey == "" {
		return errAssistantRunScope
	}
	s := o.execution.service.server
	view, err := s.preparationRepository().TryRead(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, view.Close()) }()
	source, err := s.acquireAssistantDialectCompleteInventory(ctx, view, a.ID)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	project, err := s.acquireAssistantDialectCompleteProject(ctx, a.PackKey)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, project.Close(ctx)) }()
	snapshot, err := source.snapshot(ctx, project)
	if err != nil {
		return err
	}
	raw, err := source.encode(ctx, snapshot)
	if err != nil {
		return err
	}
	evidence, err := readAssistantDialectCompleteRead(ctx, raw)
	if err != nil {
		return err
	}
	result, err := evidence.result(ctx)
	if err != nil {
		return err
	}
	metadata := assistantDialectCompleteReadMetadata(snapshot)
	o.raw, o.snapshot, o.result = bytes.Clone(raw), snapshot, result
	o.read = assistantDialectRead{ID: snapshot.ID, Name: snapshot.Name, Revision: snapshot.Revision, RecipeVersion: source.saved.Document.RecipeVersion, PackKey: snapshot.PackKey, PackPath: project.pack.path, Declaration: project.declaration, View: metadata}
	// The decoded evidence owns an independent index, separate from the
	// source snapshot and callbacks. Retain that proof once with its root.
	o.held = assistantDialectCompleteReadCustody{ResearchIndex: evidence.Snapshot.ResearchIndex, Version: 1, ReadOperationID: o.host, ResourceDigest: assistantDialectResource(snapshot.ID, snapshot.PackKey), InputID: snapshot.ID, Revision: snapshot.Revision, PackKey: snapshot.PackKey, PackPath: o.read.PackPath, Declaration: o.read.Declaration, PackDigest: "sha256:" + digestOf([]byte(snapshot.PackJSON)), MetadataSHA256: digestOf(metadata), Intent: assistantDialectCompleteReadIntent{Reference: artifactReference(raw), ResultSHA256: digestOf(assistantDialectCompleteResultBytes(result))}}
	o.held.Intent.Receipt = assistantDialectCompleteReadReceipt(assistantRunRecord{ID: o.runID, Binding: o.binding, Operations: []assistantRunOperation{{ID: o.host, Intent: bytes.Clone(o.intentRaw)}}}, o.held, o.root)
	o.read.Complete = &assistantDialectCompleteReadSelector{Version: 1, ReadOperationID: o.host, Reference: o.held.Intent.Reference, Receipt: o.held.Intent.Receipt, PackDigest: o.held.PackDigest, ResultSHA256: o.held.Intent.ResultSHA256}
	if !validAssistantDialectCompleteReadSelector(o.read) {
		return errAssistantRunInvalid
	}
	return errors.Join(source.Check(ctx), project.Check(ctx))
}

type assistantDialectCompleteReadGuardKind uint8

const (
	assistantDialectCompleteReserve assistantDialectCompleteReadGuardKind = iota + 1
	assistantDialectCompletePublish
	assistantDialectCompleteInspect
	assistantDialectCompleteAcknowledge
)

type assistantDialectCompleteReadGuard struct {
	self      *assistantDialectCompleteReadGuard
	owner     *assistantDialectCompleteReadOwner
	kind      assistantDialectCompleteReadGuardKind
	run       assistantRunRecord
	ledger    assistantRunLedger
	data      *chatDataStore
	private   *privateDataShortLock
	custody   *assistantDialectShortCustody
	view      inputPreparationRead
	source    *assistantDialectCompleteSourceHold
	project   *assistantDialectCompleteProject
	admission assistantDialectArtifactOwnerAdmission
	closed    bool
	closeErr  error
}

func (o *assistantDialectCompleteReadOwner) acquire(ctx context.Context, kind assistantDialectCompleteReadGuardKind) (_ *assistantDialectCompleteReadGuard, err error) {
	if !o.original() || kind < assistantDialectCompleteReserve || kind > assistantDialectCompleteAcknowledge {
		return nil, errAssistantRunScope
	}
	e, s := o.execution, o.execution.service.server
	g := &assistantDialectCompleteReadGuard{owner: o, kind: kind}
	g.self = g
	defer func() {
		if err != nil {
			err = errors.Join(err, g.Close())
		}
	}()
	g.custody, err = o.short.acquire(ctx, assistantDialectShortTransaction)
	if err != nil {
		return nil, err
	}
	g.private = g.custody.private
	if err = g.checkPhysical(ctx); err != nil {
		return nil, err
	}
	if err = s.authorizeAssistantRun(e.request.WithContext(ctx), assistantRunRecord{Binding: o.binding}); err != nil {
		return nil, err
	}
	if err = o.short.checkPrivate(g.custody); err != nil {
		observeAssistantDialectCompleteRead(ctx, assistantDialectCompleteReadObservedPrivate, err)
		return nil, err
	}
	g.data, err = s.openChatData()
	if err != nil {
		return nil, err
	}
	current := s.assistantRunsLocked(g.data.root)
	if current.problem != nil || current.project != o.lease.store.project || current.name != o.lease.store.name {
		return nil, errors.Join(errAssistantRunScope, current.problem)
	}
	if err = o.lease.store.checkScope(g.data.root); err != nil {
		return nil, err
	}
	if err = checkPrivateDataPath(g.data); err != nil {
		return nil, err
	}
	g.ledger, err = o.lease.store.readLocked(ctx, g.data)
	if err != nil {
		return nil, err
	}
	run, err := assistantRunBound(&g.ledger, o.binding, o.runID)
	if err != nil {
		return nil, err
	}
	g.run = *run
	identity, _, err := s.assistantRunIdentity(e.request.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	fresh, err := s.resolveAssistantRun(e.request.WithContext(ctx), e.source.Start, identity, o.binding.Project)
	if err != nil {
		return nil, err
	}
	fresh.Binding.ExecutionKey, fresh.Binding.Unit = e.source.Binding.ExecutionKey, e.source.Binding.Unit
	if !assistantRunFreshSourceEqual(e.source, fresh) || fresh.Binding.Phase != o.binding.Phase {
		return nil, errAssistantRunScope
	}
	if err = assistantDialectCompleteReadFence(g.ledger, g.run, o.host, o.enrollment.Owner, o.held.Intent.Reference); err != nil {
		return nil, err
	}
	if err = g.checkRun(); err != nil {
		return nil, err
	}
	// All source exclusion is forward-acquired before the artifact/provider queue.
	g.view, err = s.preparationRepository().TryRead(ctx)
	if err != nil {
		return nil, err
	}
	g.source, err = s.acquireAssistantDialectCompleteInventory(ctx, g.view, o.read.ID)
	if err != nil {
		return nil, err
	}
	g.project, err = s.acquireAssistantDialectCompleteProject(ctx, o.read.PackKey)
	if err != nil {
		return nil, err
	}
	snapshot, err := g.source.snapshot(ctx, g.project)
	if err != nil {
		return nil, err
	}
	raw, err := g.source.encode(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(raw, o.raw) || !reflect.DeepEqual(snapshot, o.snapshot) || g.project.pack.path != o.read.PackPath || g.project.declaration != o.read.Declaration {
		return nil, errAssistantRunScope
	}
	// Copy the exact expected admitted row; no caller aliases enter transaction.
	if json.Unmarshal(assistantChatJSON(o.held), &g.admission.read) != nil || json.Unmarshal(assistantChatJSON(o.binding), &g.admission.binding) != nil {
		return nil, errAssistantRunInvalid
	}
	g.admission.owner, g.admission.runID, g.admission.host = o.enrollment.Owner, o.runID, o.host
	if kind == assistantDialectCompleteReserve {
		g.admission.readStage = "intent"
	} else if kind == assistantDialectCompleteAcknowledge {
		g.admission.readStage = "ack"
		receipt := g.admission.read.Intent
		g.admission.read.Receipt = &receipt
	}
	if err = g.Check(ctx); err != nil {
		return nil, err
	}
	return g, nil
}
func (g *assistantDialectCompleteReadGuard) checkPhysical(ctx context.Context) error {
	if g == nil || g.self != g || g.closed || !g.owner.original() {
		return errAssistantRunScope
	}
	o, e := g.owner, g.owner.execution
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.ctx.Err(); err != nil {
		return err
	}
	physical, ok := ctx.Value(assistantExternalPhysicalKey{}).(assistantExternalPhysical)
	root, _ := ctx.Value(assistantHostRootKey{}).(bool)
	generation, live := ctx.Value(assistantInvocationGenerationKey{}).(assistantInvocationGeneration)
	if !ok || !live || !root || physical.ID != o.host || physical.Name != "read_dialect" || !reflect.DeepEqual(physical.Intent, o.root) || runcontext.CallIDOf(ctx) != o.root.CallID || generation.owner != e || generation.number != o.generation || generation.finalizer || e.generation != o.generation || e.lifecycle != "running" || e.stopRequested || e.observation != nil || o.writer.failure != nil || o.lease.file == nil || o.lease.work == nil || o.lease.recoveryErr != nil {
		return errAssistantRunScope
	}
	return o.captured()
}
func (g *assistantDialectCompleteReadGuard) checkRun() error {
	o, run := g.owner, g.run
	if !assistantDialectCompleteReadLiveRun(run) || run.ID != o.runID || run.Revision != o.writer.revision || !reflect.DeepEqual(run.Binding, o.binding) || !assistantDialectDraftOwnerMatches(run, o.enrollment.Owner) {
		return errAssistantRunScope
	}
	op, in, err := assistantDialectCompleteReadRoot(run, o.host)
	if err != nil || op.State != "dispatched" || !reflect.DeepEqual(in, o.root) || !bytes.Equal(op.Intent, o.intentRaw) {
		return errAssistantRunScope
	}
	held := assistantDialectCompleteReadOf(&run, o.host)
	switch g.kind {
	case assistantDialectCompleteReserve:
		if o.reserved || held != nil {
			return errAssistantRunDispatch
		}
	case assistantDialectCompletePublish, assistantDialectCompleteInspect, assistantDialectCompleteAcknowledge:
		if !o.reserved || held == nil || !reflect.DeepEqual(held.Intent, o.held.Intent) || held.Receipt != nil || held.Uncertainty != "" || run.DialectArtifactOwner != o.enrollment.Owner {
			return errAssistantRunDispatch
		}
	default:
		return errAssistantRunScope
	}
	return nil
}
func (g *assistantDialectCompleteReadGuard) Check(ctx context.Context) error {
	if err := g.checkPhysical(ctx); err != nil {
		return err
	}
	if err := g.checkRun(); err != nil {
		return err
	}
	if g.source == nil || g.project == nil || g.view == nil || g.private == nil || g.data == nil {
		return errAssistantRunScope
	}
	if err := g.source.Check(ctx); err != nil {
		return err
	}
	if err := g.project.Check(ctx); err != nil {
		return err
	}
	return g.owner.execution.service.server.authorizeAssistantRun(g.owner.execution.request.WithContext(ctx), assistantRunRecord{Binding: g.owner.binding})
}
func (g *assistantDialectCompleteReadGuard) closeData() error {
	var err error
	if g.project != nil {
		err = errors.Join(err, g.project.Close(context.Background()))
		g.project = nil
	}
	if g.source != nil {
		err = errors.Join(err, g.source.Close())
		g.source = nil
	}
	if g.view != nil {
		err = errors.Join(err, g.view.Close())
		g.view = nil
	}
	if g.data != nil {
		err = errors.Join(err, closeDialectArtifactOwner(context.Background(), "complete-read-data", g.data.root.Close))
		g.data = nil
	}
	if g.private != nil {
		err = errors.Join(err, g.private.Close())
		g.private = nil
	}
	return err
}
func (g *assistantDialectCompleteReadGuard) Close() error {
	if g == nil || g.self != g {
		return errAssistantRunScope
	}
	if g.closed {
		return g.closeErr
	}
	g.closed = true
	g.closeErr = errors.Join(g.closeErr, g.closeData())
	if g.custody != nil {
		g.closeErr = errors.Join(g.closeErr, g.custody.Close())
	}
	return g.closeErr
}
func (g *assistantDialectCompleteReadGuard) bindBaseline(ctx context.Context, ledger assistantRunLedger) error {
	if err := g.Check(ctx); err != nil {
		return err
	}
	run, err := assistantRunBound(&ledger, g.owner.binding, g.owner.runID)
	if err != nil || !reflect.DeepEqual(*run, g.run) {
		return errors.Join(errAssistantRunConflict, err)
	}
	return nil
}
func (g *assistantDialectCompleteReadGuard) adoptSaved(ledger assistantRunLedger) error {
	run, err := assistantRunBound(&ledger, g.owner.binding, g.owner.runID)
	if err != nil {
		return err
	}
	g.run, g.ledger = *run, ledger
	return nil
}
func (o *assistantDialectCompleteReadOwner) change(ctx context.Context, kind assistantDialectCompleteReadGuardKind) (err error) {
	g, err := o.acquire(ctx, kind)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, g.closeData())
		if err != nil {
			o.writer.fail(err)
		}
		err = errors.Join(err, g.Close())
	}()
	var saved assistantRunLedger
	err = o.writer.changeLocked(ctx, false, nil, func(change func(*assistantRunLedger) (bool, error)) (assistantRunLedger, error) {
		var e error
		saved, e = o.lease.store.transactionDialectReadLocked(ctx, g, change)
		return saved, e
	}, func(run *assistantRunRecord) (bool, error) {
		switch kind {
		case assistantDialectCompleteReserve:
			if assistantDialectCompleteReadOf(run, o.host) != nil {
				return false, errAssistantRunDispatch
			}
			run.DialectReads = append(run.DialectReads, *g.admission.read)
			if run.DialectArtifactOwner == "" {
				run.DialectArtifactOwner = o.enrollment.Owner
			}
			// Preflight both intent and acknowledged serialized states before raw Put.
			next := g.ledger
			next.Runs = append([]assistantRunRecord(nil), g.ledger.Runs...)
			for i := range next.Runs {
				if next.Runs[i].ID == run.ID {
					var preflight assistantRunRecord
					if json.Unmarshal(assistantChatJSON(run), &preflight) != nil {
						return false, errAssistantRunInvalid
					}
					next.Runs[i] = preflight
					next.Runs[i].Revision++
					if _, _, e := prepareAssistantRunStorage(o.lease.store.name, next); e != nil {
						return false, e
					}
					next.Runs[i].DialectReads = append([]assistantDialectCompleteReadCustody(nil), run.DialectReads...)
					slot := assistantDialectCompleteReadOf(&next.Runs[i], o.host)
					receipt := slot.Intent
					slot.Receipt = &receipt
					next.Runs[i].Revision++
					if _, _, e := prepareAssistantRunStorage(o.lease.store.name, next); e != nil {
						return false, e
					}
					completedOp := assistantDialectOperation(next.Runs[i], o.host)
					if completedOp == nil {
						return false, errAssistantRunScope
					}
					completedOp.State = "completed"
					completedOp.Result = assistantChatJSON(assistantHostResult{Result: assistantDialectCompleteResultBytes(o.result), DialectRead: &o.read})
					next.Runs[i].Revision++
					if _, _, e := prepareAssistantRunStorage(o.lease.store.name, next); e != nil {
						return false, e
					}

				}
			}
		case assistantDialectCompleteAcknowledge:
			slot := assistantDialectCompleteReadOf(run, o.host)
			if slot == nil || slot.Receipt != nil {
				return false, errAssistantRunDispatch
			}
			receipt := slot.Intent
			slot.Receipt = &receipt
		default:
			return false, errAssistantRunScope
		}
		return true, nil
	})
	if err == nil {
		err = g.adoptSaved(saved)
	}
	// The ordinary final Check expects a pre-mutation state; saved graph is
	// independently validated by transaction. Final physical/source checks remain.
	if err == nil {
		err = errors.Join(g.checkPhysical(ctx), g.source.Check(ctx), g.project.Check(ctx))
	}
	return err
}
func (o *assistantDialectCompleteReadOwner) publish(ctx context.Context) (err error) {
	if !o.original() || !o.mu.TryLock() {
		return errAssistantRunScope
	}
	if o.reserved {
		o.mu.Unlock()
		return errAssistantRunDispatch
	}
	o.mu.Unlock()
	if err = o.change(ctx, assistantDialectCompleteReserve); err != nil {
		return err
	}
	// Only this known-successful original reservation arms the one effect.
	o.reserved = true
	ref, err := o.objects.PutGuarded(ctx, o.raw, func(ctx context.Context) (retainedArtifactGuard, error) {
		return o.acquire(ctx, assistantDialectCompletePublish)
	})
	if err != nil || ref != o.held.Intent.Reference {
		return errors.Join(errRetainedArtifactChanged, err)
	}
	o.published = true
	var raw []byte
	if o.injected != nil {
		raw, err = o.injected.Get(ctx, ref)
	} else {
		var g *assistantDialectCompleteReadGuard
		g, err = o.acquire(ctx, assistantDialectCompleteInspect)
		if err == nil {
			raw, err = o.execution.service.server.readAssistantDialectCompleteNativeRaw(ctx, ref)
			err = errors.Join(err, g.Check(ctx), g.Close())
		}
	}
	if err != nil || !bytes.Equal(raw, o.raw) {
		return errors.Join(errRetainedArtifactChanged, err)
	}
	o.inspected = true
	if err = o.change(ctx, assistantDialectCompleteAcknowledge); err != nil {
		return err
	}
	return nil
}
func (e *assistantRunExecution) readAssistantDialectComplete(ctx context.Context, a assistantDialectArguments, paging *assistantDialectCompleteReadPaging) (assistantDialectRead, runcontext.ToolResult, error) {
	physical, ok := ctx.Value(assistantExternalPhysicalKey{}).(assistantExternalPhysical)
	if !ok || physical.Name != "read_dialect" {
		return assistantDialectRead{}, runcontext.ToolResult{}, errAssistantRunScope
	}
	owner, err := newAssistantDialectCompleteReadOwner(ctx, e, physical.ID)
	if err != nil {
		observeAssistantDialectCompleteRead(ctx, assistantDialectCompleteReadObservedReturn, err)
		return assistantDialectRead{}, runcontext.ToolResult{}, err
	}
	if err = owner.capture(ctx, a); err == nil {
		err = owner.publish(ctx)
	}
	if err != nil {
		observeAssistantDialectCompleteRead(ctx, assistantDialectCompleteReadObservedReturn, err)
		return assistantDialectRead{}, runcontext.ToolResult{}, err
	}
	owner.acknowledged = true
	if err = paging.bind(owner); err != nil {
		observeAssistantDialectCompleteRead(ctx, assistantDialectCompleteReadObservedReturn, err)
		return assistantDialectRead{}, runcontext.ToolResult{}, err
	}
	observeAssistantDialectCompleteRead(ctx, assistantDialectCompleteReadObservedReturn, nil)
	return owner.read, owner.result, nil
}

func (s assistantRunStore) transactionDialectReadLocked(ctx context.Context, g *assistantDialectCompleteReadGuard, change func(*assistantRunLedger) (bool, error)) (assistantRunLedger, error) {
	if g == nil || g.self != g || g.owner.lease.store != s || g.data == nil || g.private == nil || (g.kind != assistantDialectCompleteReserve && g.kind != assistantDialectCompleteAcknowledge) {
		return assistantRunLedger{}, errAssistantRunScope
	}
	if err := g.Check(ctx); err != nil {
		return assistantRunLedger{}, err
	}
	return s.transactionLocked(ctx, g.data, nil, g, change)
}

// Overlay-only closed observation. Nil in production, with no authority or return
// value. Original guards and errors are unchanged. Test callbacks only send to
// fixed buffered channels or update atomics; they never wait under custody.
type assistantDialectCompleteReadObservation uint8

const (
	assistantDialectCompleteReadObservedPrivate assistantDialectCompleteReadObservation = iota + 1
	assistantDialectCompleteReadObservedPin
	assistantDialectCompleteReadObservedReturn
)

type assistantDialectCompleteReadObservationClass uint8

const (
	assistantDialectCompleteReadObservedSuccess assistantDialectCompleteReadObservationClass = iota
	assistantDialectCompleteReadObservedFailure
	assistantDialectCompleteReadObservedTurnBusy
)

var testHookAssistantDialectCompleteReadObserved func(context.Context, assistantDialectCompleteReadObservation, assistantDialectCompleteReadObservationClass)

func observeAssistantDialectCompleteRead(ctx context.Context, stage assistantDialectCompleteReadObservation, err error) {
	if testHookAssistantDialectCompleteReadObserved == nil {
		return
	}
	class := assistantDialectCompleteReadObservedSuccess
	if err != nil {
		class = assistantDialectCompleteReadObservedFailure
		if stage == assistantDialectCompleteReadObservedPrivate && errors.Is(err, errPrivateDataTurnBusy) {
			class = assistantDialectCompleteReadObservedTurnBusy
		}
	}
	testHookAssistantDialectCompleteReadObserved(ctx, stage, class)
}
