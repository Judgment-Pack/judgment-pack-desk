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

// A live owner is minted only inside the original dispatched root callback,
// before its first reservation. Nothing in the journal can recreate this owner.
// The actual complete-read owner remains a required, separately enrolled port.
type assistantDialectDraftPutOwner struct {
	self           *assistantDialectDraftPutOwner
	factory        *assistantDialectFactoryStore
	enrollment     dialectArtifactOwnerEnrollment
	registration   *retainedArtifactProviderRegistration
	injected       *boundInjectedArtifactRepository
	captured       func() error
	execution      *assistantRunExecution
	writer         *assistantRunWriter
	lease          *assistantRunLease
	binding        assistantRunBinding
	runID, host    string
	generation     uint64
	action         assistantDialectActionMetadata
	root           assistantHostIntent
	evidence       assistantDialectFactoryOwnedReadEvidence
	objects        guardedRetainedArtifactRepository
	inspection     retainedArtifactRepository
	mu             sync.Mutex
	reservations   map[string]*assistantDialectDraftReservation
	invalidAttempt bool
}

type assistantDialectDraftReservation struct {
	revision uint64
	held     assistantDialectDraftCustody
	intent   assistantDialectDraftIntent
	consumed bool
}

func newAssistantDialectFactoryStore(ctx context.Context, e *assistantRunExecution, host string, evidence assistantDialectFactoryReadEvidence) (*assistantDialectFactoryStore, error) {
	owner, err := newAssistantDialectDraftPutOwner(ctx, e, host, evidence)
	if err != nil {
		return nil, err
	}
	factory := &assistantDialectFactoryStore{execution: e, host: host, objects: owner.inspection, evidence: evidence, put: owner}
	owner.factory = factory
	return factory, nil
}

func newAssistantDialectDraftPutOwner(ctx context.Context, e *assistantRunExecution, host string, evidence assistantDialectFactoryReadEvidence) (*assistantDialectDraftPutOwner, error) {
	owned, ok := evidence.(assistantDialectFactoryOwnedReadEvidence)
	if !ok || e == nil || e.writer == nil || e.writer.lease == nil || e.service == nil || e.service.server == nil || e.ctx == nil || e.request == nil || e.source.Start.Purpose != "chat" {
		return nil, errAssistantRunScope
	}
	g, ok := ctx.Value(assistantInvocationGenerationKey{}).(assistantInvocationGeneration)
	meta, prepared := ctx.Value(assistantDialectPreparedKey{}).(*assistantDialectActionMetadata)
	physical, dispatched := ctx.Value(assistantExternalPhysicalKey{}).(assistantExternalPhysical)
	root, _ := ctx.Value(assistantHostRootKey{}).(bool)
	if !ok || g.owner != e || g.number == 0 || g.finalizer || !root || !prepared || meta == nil || !dispatched || physical.ID != host || physical.Name != "propose_dialect" || runcontext.CallIDOf(ctx) != physical.Intent.CallID {
		return nil, errAssistantRunScope
	}
	if err := e.Authorize(ctx); err != nil {
		return nil, err
	}
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return nil, err
	}
	in, _, err := assistantDialectDraftRoot(run, host)
	op := assistantDialectOperation(run, host)
	if err != nil || run.DialectArtifactOwner == "" && len(run.DialectDrafts) != 0 || op == nil || op.State != "dispatched" || !reflect.DeepEqual(in, physical.Intent) || !reflect.DeepEqual(in.DialectAction, meta) || !assistantDialectDraftLiveRun(run) || assistantDialectDraftOf(&run, host) != nil {
		return nil, errAssistantRunScope
	}
	// Reject all earlier unknown roots/slots, including another session's run.
	ledger, err := e.writer.lease.store.readSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	if err = assistantDialectDraftFence(ledger, run, host); err != nil {
		return nil, err
	}
	if _, err = assistantDialectFactoryBoundSnapshot(ctx, evidence, run, host); err != nil {
		return nil, err
	}
	// Copy the original request facts before enrollment. The lexical enrollment
	// callback only captures storage identity; it cannot publish an object.
	var binding assistantRunBinding
	var copied assistantHostIntent
	if json.Unmarshal(assistantChatJSON(run.Binding), &binding) != nil || readAssistantHostReceipt(assistantChatJSON(in), &copied) != nil {
		return nil, errAssistantRunInvalid
	}
	s := e.service.server
	o := &assistantDialectDraftPutOwner{execution: e, writer: e.writer, lease: e.writer.lease, binding: binding, runID: run.ID, host: host, generation: g.number, action: *copied.DialectAction, root: copied, evidence: owned, reservations: map[string]*assistantDialectDraftReservation{}}
	o.self = o
	registration, err := s.injectedArtifactRegistration()
	if err != nil {
		return nil, err
	}
	if registration == nil {
		o.enrollment, err = e.enrollDialectArtifactOwner(ctx)
	} else {
		selection, selectErr := newRetainedArtifactInjectedSelection(nil, false)
		if selectErr != nil {
			return nil, selectErr
		}
		var provisional *boundInjectedArtifactRepository
		o.enrollment, err = e.withDialectArtifactOwner(ctx, &dialectArtifactInjectedUse{selection: selection, use: func(v *retainedArtifactInjectedView) error {
			var bindErr error
			provisional, bindErr = v.boundDialectFactoryRepository(ctx, o)
			return bindErr
		}})
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
	if !e.canonicalMu.TryLock() {
		return nil, errRetainedArtifactBusy
	}
	defer e.canonicalMu.Unlock()
	if e.generation != g.number || e.lifecycle != "running" || e.stopRequested || e.observation != nil {
		return nil, errAssistantRunScope
	}
	w := e.writer
	if !w.mu.TryLock() {
		return nil, errRetainedArtifactBusy
	}
	defer w.mu.Unlock()
	l := w.lease
	if !l.mu.TryLock() {
		return nil, errRetainedArtifactBusy
	}
	defer l.mu.Unlock()
	if w != o.writer || l != o.lease || w.failure != nil || l.file == nil || l.work == nil || l.recoveryErr != nil || w.revision != run.Revision || w.id != run.ID || !reflect.DeepEqual(w.binding, run.Binding) {
		return nil, errAssistantRunScope
	}
	project, assistant, physicalRoot := s.project, s.assistant, s.root
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
	// Join the acknowledged producer facts to the actual captured roots/FD,
	// rather than accepting a replacement observed after enrollment.
	if identityKey(projectRoot) != enrolled.ProjectIdentity || identityKey(workRoot) != enrolled.WorkIdentity || dialectArtifactWorkDomainID(identityKey(workRoot), identityKey(workInfo)) != enrolled.WorkDomain {
		return nil, errAssistantRunScope
	}
	o.captured = func() error {
		if o.enrollment != enrolled || o.execution != e || o.writer != w || o.lease != l || o.runID != run.ID || o.host != host || o.registration != registration || !bytes.Equal(assistantChatJSON(o.binding), sealedBinding) || !bytes.Equal(assistantChatJSON(o.root), sealedRoot) || !reflect.DeepEqual(o.action, *copied.DialectAction) {
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

func assistantDialectDraftLiveRun(run assistantRunRecord) bool {
	return !run.Historical && run.State == "running" && run.Binding.Owner.Kind == "chat" && run.Binding.Phase == "work" && run.Checkpoint != nil && run.Checkpoint.Status == "running" && run.Observation == nil
}

// Called only after retain's own successful fresh reservation and independently
// validated exact committed revision readback. Never called on duplicate/pending
// delivery or an uncertain write. Binding performs no durable mutation.
func (o *assistantDialectDraftPutOwner) bindReservation(run assistantRunRecord, stage string, revision uint64, expected assistantDialectDraftIntent) error {
	if o == nil || !o.mu.TryLock() {
		return errAssistantRunScope
	}
	defer o.mu.Unlock()
	if !o.original() || run.DialectArtifactOwner != o.enrollment.Owner || o.invalidAttempt || o.reservations[stage] != nil || run.ID != o.runID || run.Revision != revision || !reflect.DeepEqual(run.Binding, o.binding) || !assistantDialectDraftLiveRun(run) {
		return errAssistantRunDispatch
	}
	held := assistantDialectDraftOf(&run, o.host)
	if held == nil || held.Uncertainty != "" || held.Reconciled {
		return errAssistantRunDispatch
	}
	slot := assistantDialectDraftSlotOf(held, stage)
	if slot == nil || slot.Receipt != nil || !reflect.DeepEqual(slot.Intent, &expected) {
		return errAssistantRunDispatch
	}
	var copy assistantDialectDraftCustody
	if json.Unmarshal(assistantChatJSON(held), &copy) != nil {
		return errAssistantRunInvalid
	}
	intent := *assistantDialectDraftSlotOf(&copy, stage).Intent
	o.reservations[stage] = &assistantDialectDraftReservation{revision: revision, held: copy, intent: intent}
	return nil
}

type assistantDialectDraftPublication struct {
	stage                string
	version              int
	candidateFingerprint string
	raw                  []byte
	ref                  retainedArtifactRef
	candidate            runcontext.DialectCandidate
	checkpoint           *runcontext.DialectDraftCheckpoint
	proposal             *runcontext.DialectReviewProposal
	digest               string
	previous             *string
}

func readAssistantDialectDraftPublication(raw []byte) (assistantDialectDraftPublication, error) {
	if len(raw) == 0 || len(raw) > assistantDialectDraftByteLimit {
		return assistantDialectDraftPublication{}, errAssistantRunInvalid
	}
	raw = bytes.Clone(raw)
	if err := assistantDialectFactoryRawGuard(raw, assistantDialectDraftByteLimit); err != nil {
		return assistantDialectDraftPublication{}, err
	}
	p := assistantDialectDraftPublication{raw: raw, ref: artifactReference(raw)}
	fields, err := assistantTestObject(raw)
	if err != nil {
		return p, err
	}
	if _, draft := fields["stage"]; draft {
		cp, fingerprint, err := readAssistantDialectFactoryDraft(raw)
		if err != nil {
			return p, err
		}
		p.version, p.candidateFingerprint = cp.Version, fingerprint
		p.stage, p.checkpoint, p.candidate, p.digest, p.previous = cp.Stage, &cp, cp.Candidate, cp.Digest, cp.PreviousReceipt
	} else {
		proposal, err := readAssistantDialectFactoryFinal(raw)
		if err != nil {
			return p, err
		}
		p.version, p.candidateFingerprint = proposal.Version, proposal.Fingerprint
		p.stage, p.proposal, p.candidate, p.digest = "final", &proposal, proposal.DialectCandidate, proposal.Fingerprint
		p.previous = &proposal.Draft.CheckedReceipt
	}
	return p, nil
}

func (o *assistantDialectDraftPutOwner) putDialectDraft(ctx context.Context, expected retainedArtifactRef, raw []byte) (retainedArtifactRef, error) {
	var empty retainedArtifactRef
	p, err := readAssistantDialectDraftPublication(raw)
	if err != nil || p.version != 2 {
		if err == nil {
			err = errAssistantRunScope
		}
		// An invalid call still cannot be converted into a fresh invocation later.
		if o != nil && o.mu.TryLock() {
			o.invalidAttempt = true
			o.mu.Unlock()
		}
		return empty, err
	}
	if o == nil || !o.mu.TryLock() {
		return empty, errAssistantRunDispatch
	}
	r := o.reservations[p.stage]
	if o.invalidAttempt || r == nil || r.consumed {
		o.mu.Unlock()
		return empty, errAssistantRunDispatch
	}
	// Consume before any queue, read or admission. A refused/uncertain attempt
	// remains consumed even when no payload effect happened.
	r.consumed = true
	reservation := *r
	o.mu.Unlock()
	if p.ref != expected || expected != reservation.intent.Reference || p.digest != reservation.intent.PackageDigest || !reflect.DeepEqual(p.previous, reservation.intent.PreviousReceipt) {
		return empty, errAssistantRunScope
	}
	if err = o.validatePublication(ctx, reservation.held, p); err != nil {
		return empty, err
	}
	ref, err := o.objects.PutGuarded(ctx, p.raw, func(ctx context.Context) (retainedArtifactGuard, error) { return o.acquireGuard(ctx, reservation, p) })
	if err == nil && ref != expected {
		err = errRetainedArtifactChanged
	}
	if err != nil {
		return empty, err
	}
	return ref, nil
}

// Predecessor objects are inspected before entering any custody/queue. Their
// exact immutable references and receipts are compared again under journal
// custody; arbitrary injected Get implementations are never entered there.
func (o *assistantDialectDraftPutOwner) validatePublication(ctx context.Context, held assistantDialectDraftCustody, p assistantDialectDraftPublication) error {
	a, err := readAssistantDialectArguments("propose_dialect", o.root.Arguments)
	if err != nil || a.ID != p.candidate.ID || a.Revision != p.candidate.Revision || a.PackKey != p.candidate.PackKey || a.PackDigest != p.candidate.PackDigest || a.Summary != p.candidate.Summary || !assistantHostArgumentsMatch(assistantChatJSON(a.Facts), assistantChatJSON(p.candidate.Facts)) || !assistantHostArgumentsMatch(assistantChatJSON(a.Evidence), assistantChatJSON(p.candidate.Evidence)) {
		return errAssistantRunScope
	}
	if p.checkpoint != nil {
		if !assistantDialectFactoryDraftMatches(held, *p.checkpoint, p.candidateFingerprint) {
			return errAssistantRunScope
		}
	}
	if p.proposal != nil && (held.Candidate.Receipt == nil || held.Checked.Receipt == nil) {
		return errAssistantRunScope
	}
	if p.proposal != nil && p.proposal.Draft != (runcontext.DialectDraftLineage{AttemptID: held.AttemptID, Fingerprint: held.DraftFingerprint, CandidateReceipt: held.Candidate.Receipt.Receipt, CheckedReceipt: held.Checked.Receipt.Receipt, CheckedDigest: held.Checked.Receipt.PackageDigest}) {
		return errAssistantRunScope
	}
	for _, priorStage := range []string{"candidate", "checked"} {
		if p.stage == "candidate" || p.stage == "checked" && priorStage == "checked" {
			continue
		}
		prior := assistantDialectDraftSlotOf(&held, priorStage)
		if prior.Intent == nil || prior.Receipt == nil || !reflect.DeepEqual(prior.Intent, prior.Receipt) {
			return errAssistantRunDispatch
		}
		raw, err := o.inspection.Get(ctx, prior.Receipt.Reference)
		if err != nil {
			return err
		}
		if verifyRetainedArtifact(prior.Receipt.Reference, raw) != nil {
			return errRetainedArtifactChanged
		}
		cp, fingerprint, err := readAssistantDialectFactoryDraft(raw)
		if err != nil || cp.Version != p.version || cp.Stage != priorStage || cp.Digest != prior.Receipt.PackageDigest || !reflect.DeepEqual(cp.PreviousReceipt, prior.Receipt.PreviousReceipt) || !assistantDialectFactoryDraftMatches(held, cp, fingerprint) {
			return errAssistantRunScope
		}
		candidate := p.candidate
		if p.stage == "final" {
			candidate.DocumentJSON = cp.Candidate.DocumentJSON
		}
		if !reflect.DeepEqual(candidate, cp.Candidate) {
			return errAssistantRunScope
		}
	}
	return nil
}

// This hold owns the real forward host custody and opened private data. It
// carries no publication or inspection permission without a concrete guard.
type assistantDialectDraftHostHold struct {
	self      *assistantDialectDraftHostHold
	owner     *assistantDialectDraftPutOwner
	run       assistantRunRecord
	ledger    assistantRunLedger
	data      *chatDataStore
	private   *privateDataShortLock
	releases  []func()
	closed    bool
	closeOnce sync.Once
	closeErr  error
	captured  func() error
}

type assistantDialectDraftGuard struct {
	*assistantDialectDraftHostHold
	self            *assistantDialectDraftGuard
	reservation     assistantDialectDraftReservation
	publication     assistantDialectDraftPublication
	view            inputPreparationRead
	completeSource  *assistantDialectCompleteSourceHold
	completeProject *assistantDialectCompleteProject
}

type assistantDialectDraftInspectionGuard struct {
	*assistantDialectDraftHostHold
	self            *assistantDialectDraftInspectionGuard
	ref             retainedArtifactRef
	view            inputPreparationRead
	completeSource  *assistantDialectCompleteSourceHold
	completeProject *assistantDialectCompleteProject
}

type assistantDialectDraftReservationGuard struct {
	*assistantDialectDraftHostHold
	self      *assistantDialectDraftReservationGuard
	stage     string
	expected  assistantDialectDraftIntent
	admission assistantDialectArtifactOwnerAdmission
}

func (o *assistantDialectDraftPutOwner) original() bool {
	return o != nil && o.self == o && o.factory != nil && o.factory.put == o && o.factory.execution == o.execution && o.factory.host == o.host && o.captured != nil
}

func (o *assistantDialectDraftPutOwner) acquireHostHold(ctx context.Context) (_ *assistantDialectDraftHostHold, err error) {
	if !o.original() {
		return nil, errAssistantRunScope
	}
	e := o.execution
	h := &assistantDialectDraftHostHold{owner: o, captured: o.captured}
	h.self = h
	defer func() {
		if err != nil {
			err = errors.Join(err, h.Close())
		}
	}()
	s := e.service.server
	if !s.writes.TryLock() {
		return nil, errRetainedArtifactBusy
	}
	h.releases = append(h.releases, s.writes.Unlock)
	if !e.canonicalMu.TryLock() {
		return nil, errRetainedArtifactBusy
	}
	h.releases = append(h.releases, e.canonicalMu.Unlock)
	if !o.writer.mu.TryLock() {
		return nil, errRetainedArtifactBusy
	}
	h.releases = append(h.releases, o.writer.mu.Unlock)
	if !o.lease.mu.TryLock() {
		return nil, errRetainedArtifactBusy
	}
	h.releases = append(h.releases, o.lease.mu.Unlock)
	if err = h.checkPinned(ctx); err != nil {
		return nil, err
	}
	h.private, err = s.tryPrivateDataLockContext(ctx, true)
	if err != nil {
		return nil, err
	}
	h.data, err = s.openChatData()
	if err != nil {
		return nil, err
	}
	current := s.assistantRunsLocked(h.data.root)
	if current.problem != nil || current.project != o.lease.store.project || current.name != o.lease.store.name {
		return nil, errors.Join(errAssistantRunScope, current.problem)
	}
	if err = o.lease.store.checkScope(h.data.root); err != nil {
		return nil, err
	}
	if err = checkPrivateDataPath(h.data); err != nil {
		return nil, err
	}
	h.ledger, err = o.lease.store.readLocked(ctx, h.data)
	if err != nil {
		return nil, err
	}
	run, err := assistantRunBound(&h.ledger, o.binding, o.runID)
	if err != nil {
		return nil, err
	}
	h.run = *run
	if err = h.checkRun(); err != nil {
		return nil, err
	}
	if err = assistantDialectDraftFence(h.ledger, *run, o.host); err != nil {
		return nil, err
	}
	identity, _, err := s.assistantRunIdentity(e.request.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	fresh, err := s.resolveAssistantRun(e.request.WithContext(ctx), e.source.Start, identity, o.binding.Project)
	if err != nil {
		return nil, err
	}
	fresh.Binding.ExecutionKey, fresh.Binding.Unit = e.source.Binding.ExecutionKey, e.source.Binding.Unit
	if fresh.Binding.Phase != "work" || !assistantRunFreshSourceEqual(e.source, fresh) {
		return nil, errAssistantRunScope
	}
	return h, nil
}

func (h *assistantDialectDraftHostHold) checkRun() error {
	o, run := h.owner, h.run
	if !assistantDialectDraftOwnerMatches(run, o.enrollment.Owner) || !assistantDialectDraftLiveRun(run) || run.ID != o.runID || !reflect.DeepEqual(run.Binding, o.binding) || run.Revision != o.writer.revision {
		return errAssistantRunScope
	}
	in, _, err := assistantDialectDraftRoot(run, o.host)
	op := assistantDialectOperation(run, o.host)
	if err != nil || op == nil || op.State != "dispatched" || !reflect.DeepEqual(in, o.root) || !reflect.DeepEqual(in.DialectAction, &o.action) {
		return errAssistantRunScope
	}
	return nil
}

func (h *assistantDialectDraftHostHold) effectView(ctx context.Context) (view inputPreparationRead, snapshot runcontext.DialectSnapshot, source *assistantDialectCompleteSourceHold, project *assistantDialectCompleteProject, err error) {
	o := h.owner
	view, err = o.execution.service.server.preparationRepository().TryRead(ctx)
	if err != nil {
		return nil, snapshot, nil, nil, err
	}
	defer func() {
		if err != nil {
			if project != nil {
				err = errors.Join(err, project.Close(ctx))
				project = nil
			}
			if source != nil {
				err = errors.Join(err, source.Close())
				source = nil
			}
			err = errors.Join(err, view.Close())
			view = nil
		}
	}()
	in, read, err := assistantDialectDraftRoot(h.run, o.host)
	if err != nil {
		return view, snapshot, source, project, err
	}
	readOp := assistantDialectOperation(h.run, in.DialectAction.ReadOperationID)
	var current runcontext.DialectSnapshot
	if production, ok := o.evidence.(*assistantDialectCompletedReadEvidence); ok {
		if production.execution != o.execution {
			return view, snapshot, nil, nil, errAssistantRunScope
		}
		source, project, snapshot, current, err = production.acquireCurrent(ctx, h.run, *readOp, read, assistantDialectBorrowedPreparation{view})
	} else {
		snapshot, current, err = o.evidence.completeDialectSnapshotUsingView(ctx, h.run, *readOp, read, assistantDialectBorrowedPreparation{view})
	}
	if err == nil && (!reflect.DeepEqual(snapshot, current) || !assistantDialectFactorySnapshotBound(snapshot, read)) {
		err = errAssistantRunScope
	}
	return view, snapshot, source, project, err
}

func (o *assistantDialectDraftPutOwner) acquireGuard(ctx context.Context, reservation assistantDialectDraftReservation, p assistantDialectDraftPublication) (_ retainedArtifactGuard, err error) {
	h, err := o.acquireHostHold(ctx)
	if err != nil {
		return nil, err
	}
	g := &assistantDialectDraftGuard{assistantDialectDraftHostHold: h, reservation: reservation, publication: p}
	g.self = g
	defer func() {
		if err != nil {
			err = errors.Join(err, g.Close())
		}
	}()
	if err = g.checkDurable(); err != nil {
		return nil, err
	}
	var snapshot runcontext.DialectSnapshot
	g.view, snapshot, g.completeSource, g.completeProject, err = h.effectView(ctx)
	if err != nil {
		return nil, err
	}
	if !assistantDialectFactorySnapshotMatches(snapshot, p.candidate) {
		return nil, errAssistantRunScope
	}
	if err = g.Check(ctx); err != nil {
		return nil, err
	}
	return g, nil
}

func (o *assistantDialectDraftPutOwner) acquireInspectionGuard(ctx context.Context, ref retainedArtifactRef) (_ *assistantDialectDraftInspectionGuard, err error) {
	h, err := o.acquireHostHold(ctx)
	if err != nil {
		return nil, err
	}
	g := &assistantDialectDraftInspectionGuard{assistantDialectDraftHostHold: h, ref: ref}
	g.self = g
	defer func() {
		if err != nil {
			err = errors.Join(err, g.Close())
		}
	}()
	if err = g.checkDurable(); err != nil {
		return nil, err
	}
	g.view, _, g.completeSource, g.completeProject, err = h.effectView(ctx)
	if err != nil {
		return nil, err
	}
	if err = g.Check(ctx); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *assistantDialectDraftInspectionGuard) checkDurable() error {
	if err := g.checkRun(); err != nil {
		return err
	}
	o := g.owner
	held := assistantDialectDraftOf(&g.run, o.host)
	if held == nil || held.Uncertainty != "" || held.Reconciled {
		return errAssistantRunDispatch
	}
	for _, stage := range []string{"candidate", "checked", "final"} {
		slot := assistantDialectDraftSlotOf(held, stage)
		if slot == nil || slot.Intent == nil || slot.Intent.Reference != g.ref {
			continue
		}
		if slot.Intent.Receipt != assistantDialectDraftReceipt(g.run, *held, stage, *slot.Intent) {
			return errAssistantRunDispatch
		}
		if slot.Receipt != nil && reflect.DeepEqual(slot.Intent, slot.Receipt) {
			return nil
		}
		// Pending bytes may be inspected only by this original consumed attempt.
		if !o.mu.TryLock() {
			return errRetainedArtifactBusy
		}
		r := o.reservations[stage]
		ok := r != nil && r.consumed && reflect.DeepEqual(r.intent, *slot.Intent)
		o.mu.Unlock()
		if ok {
			return nil
		}
	}
	return errAssistantRunDispatch
}

// Borrow only Get. Do not leak the held view's Close method to the evidence
// owner, including through a dynamic interface assertion.
type assistantDialectBorrowedPreparation struct{ view inputPreparationReader }

func (b assistantDialectBorrowedPreparation) Get(id string) (inputPreparationReply, error) {
	return b.view.Get(id)
}

func (g *assistantDialectDraftHostHold) checkPhysical(ctx context.Context) error {
	if g == nil || g.self != g || g.closed || !g.owner.original() {
		return errAssistantRunScope
	}
	o, e := g.owner, g.owner.execution
	physical, physicalOK := ctx.Value(assistantExternalPhysicalKey{}).(assistantExternalPhysical)
	meta, prepared := ctx.Value(assistantDialectPreparedKey{}).(*assistantDialectActionMetadata)
	root, _ := ctx.Value(assistantHostRootKey{}).(bool)
	if !root || !physicalOK || physical.ID != o.host || physical.Name != "propose_dialect" || !reflect.DeepEqual(physical.Intent, o.root) || !prepared || meta == nil || !reflect.DeepEqual(*meta, o.action) || runcontext.CallIDOf(ctx) != o.root.CallID {
		return errAssistantRunScope
	}
	invocation, ok := ctx.Value(assistantInvocationGenerationKey{}).(assistantInvocationGeneration)
	if !ok || invocation.owner != e || invocation.number != o.generation || invocation.finalizer || e.generation != o.generation || e.lifecycle != "running" || e.stopRequested || e.observation != nil || e.writer != o.writer || o.writer.lease != o.lease || o.writer.failure != nil || o.writer.id != o.runID || !reflect.DeepEqual(o.writer.binding, o.binding) || o.lease.file == nil || o.lease.work == nil || o.lease.recoveryErr != nil {
		return errAssistantRunScope
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.ctx.Err(); err != nil {
		return err
	}
	if g.captured == nil {
		return errAssistantRunScope
	}
	return g.captured()
}

func (g *assistantDialectDraftHostHold) checkPinned(ctx context.Context) error {
	if err := g.checkPhysical(ctx); err != nil {
		return err
	}
	o := g.owner
	return o.execution.service.server.authorizeAssistantRun(o.execution.request.WithContext(ctx), assistantRunRecord{Binding: o.binding})
}

func (g *assistantDialectDraftGuard) checkDurable() error {
	o, r, run := g.owner, g.reservation, g.run
	if run.DialectArtifactOwner != o.enrollment.Owner || !assistantDialectDraftLiveRun(run) || run.ID != o.runID || !reflect.DeepEqual(run.Binding, o.binding) || run.Revision != r.revision || run.Revision != o.writer.revision {
		return errAssistantRunScope
	}
	in, _, err := assistantDialectDraftRoot(run, o.host)
	op := assistantDialectOperation(run, o.host)
	if err != nil || op == nil || op.State != "dispatched" || !reflect.DeepEqual(in, o.root) || !reflect.DeepEqual(in.DialectAction, &o.action) {
		return errAssistantRunScope
	}
	held := assistantDialectDraftOf(&run, o.host)
	if held == nil || !reflect.DeepEqual(*held, r.held) || held.Uncertainty != "" || held.Reconciled {
		return errAssistantRunDispatch
	}
	slot := assistantDialectDraftSlotOf(held, g.publication.stage)
	if slot == nil || slot.Receipt != nil || !reflect.DeepEqual(slot.Intent, &r.intent) || r.intent.Reference != g.publication.ref || r.intent.Receipt != assistantDialectDraftReceipt(run, *held, g.publication.stage, r.intent) {
		return errAssistantRunDispatch
	}
	return nil
}

func (g *assistantDialectDraftGuard) checkPhysical(ctx context.Context) error {
	if g == nil || g.self != g || g.view == nil || g.private == nil || g.data == nil {
		return errAssistantRunScope
	}
	if err := g.assistantDialectDraftHostHold.checkPhysical(ctx); err != nil {
		return err
	}
	if g.completeSource != nil {
		if err := g.completeSource.Check(ctx); err != nil {
			return err
		}
		if g.completeProject == nil {
			return errAssistantRunScope
		}
		if err := g.completeProject.Check(ctx); err != nil {
			return err
		}
	}
	return g.checkDurable()
}
func (g *assistantDialectDraftGuard) Check(ctx context.Context) error {
	if err := g.checkPhysical(ctx); err != nil {
		return err
	}
	return g.checkPinned(ctx)
}
func (g *assistantDialectDraftInspectionGuard) checkPhysical(ctx context.Context) error {
	if g == nil || g.self != g || g.view == nil || g.private == nil || g.data == nil {
		return errAssistantRunScope
	}
	if err := g.assistantDialectDraftHostHold.checkPhysical(ctx); err != nil {
		return err
	}
	if g.completeSource != nil {
		if err := g.completeSource.Check(ctx); err != nil {
			return err
		}
		if g.completeProject == nil {
			return errAssistantRunScope
		}
		if err := g.completeProject.Check(ctx); err != nil {
			return err
		}
	}
	return g.checkDurable()
}
func (g *assistantDialectDraftInspectionGuard) Check(ctx context.Context) error {
	if err := g.checkPhysical(ctx); err != nil {
		return err
	}
	return g.checkPinned(ctx)
}
func (g *assistantDialectDraftGuard) holdInjectedAuthority(ctx context.Context, registration *retainedArtifactProviderRegistration, binding retainedArtifactInjectedBinding, operation *retainedArtifactInjectedOperation) (*dialectArtifactInjectedAuthorityHold, error) {
	if g == nil || g.self != g {
		return nil, errAssistantRunScope
	}
	return g.owner.holdInjectedAuthority(ctx, registration, binding, operation, func() error { return g.checkPhysical(ctx) })
}
func (g *assistantDialectDraftInspectionGuard) holdInjectedAuthority(ctx context.Context, registration *retainedArtifactProviderRegistration, binding retainedArtifactInjectedBinding, operation *retainedArtifactInjectedOperation) (*dialectArtifactInjectedAuthorityHold, error) {
	if g == nil || g.self != g {
		return nil, errAssistantRunScope
	}
	return g.owner.holdInjectedAuthority(ctx, registration, binding, operation, func() error { return g.checkPhysical(ctx) })
}
func (o *assistantDialectDraftPutOwner) holdInjectedAuthority(ctx context.Context, registration *retainedArtifactProviderRegistration, binding retainedArtifactInjectedBinding, operation *retainedArtifactInjectedOperation, physical func() error) (*dialectArtifactInjectedAuthorityHold, error) {
	if !o.original() || o.injected == nil || o.registration != registration || o.injected.binding != binding || operation == nil || operation.provider != registration.provider || operation.finished.Load() {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	var authority *dialectArtifactInjectedAuthorityHold
	check := func() error {
		if operation.finished.Load() || authority != nil && authority.operation != operation {
			return errRetainedArtifactAuthorityUnavailable
		}
		return physical()
	}
	g, err := holdDialectArtifactInjectedAuthority(o.execution.service.server, o.execution.request.WithContext(ctx), o.binding, registration, operation, check)
	if err != nil {
		return nil, err
	}
	g.binding = binding
	authority = g
	return g, nil
}
func (g *assistantDialectDraftGuard) Close() error {
	if g == nil || g.self != g {
		return errAssistantRunScope
	}
	if g.completeProject != nil {
		g.closeErr = errors.Join(g.closeErr, g.completeProject.Close(context.Background()))
		g.completeProject = nil
	}
	if g.completeSource != nil {
		g.closeErr = errors.Join(g.closeErr, g.completeSource.Close())
		g.completeSource = nil
	}
	if g.view != nil {
		g.closeErr = errors.Join(g.closeErr, g.view.Close())
		g.view = nil
	}
	return g.assistantDialectDraftHostHold.Close()
}
func (g *assistantDialectDraftInspectionGuard) Close() error {
	if g == nil || g.self != g {
		return errAssistantRunScope
	}
	if g.completeProject != nil {
		g.closeErr = errors.Join(g.closeErr, g.completeProject.Close(context.Background()))
		g.completeProject = nil
	}
	if g.completeSource != nil {
		g.closeErr = errors.Join(g.closeErr, g.completeSource.Close())
		g.completeSource = nil
	}
	if g.view != nil {
		g.closeErr = errors.Join(g.closeErr, g.view.Close())
		g.view = nil
	}
	return g.assistantDialectDraftHostHold.Close()
}
func (g *assistantDialectDraftHostHold) Close() error {
	if g == nil || g.self != g {
		return errAssistantRunScope
	}
	g.closeOnce.Do(func() {
		g.closed = true
		g.closeErr = errors.Join(g.closeErr, g.closeData())
		// Data/private cleanup precedes release of actual host mutexes.
		for i := len(g.releases) - 1; i >= 0; i-- {
			g.releases[i]()
		}
	})
	return g.closeErr
}

func (o *assistantDialectDraftPutOwner) acquireReservationGuard(ctx context.Context, stage string, expected assistantDialectDraftIntent) (_ *assistantDialectDraftReservationGuard, err error) {
	if !contains([]string{"candidate", "checked", "final"}, stage) || !expected.Reference.valid() || !assistantDialectPackageDigest(expected.PackageDigest) {
		return nil, errAssistantRunScope
	}
	h, err := o.acquireHostHold(ctx)
	if err != nil {
		return nil, err
	}
	g := &assistantDialectDraftReservationGuard{assistantDialectDraftHostHold: h, stage: stage}
	g.self = g
	defer func() {
		if err != nil {
			err = errors.Join(err, g.Close())
		}
	}()
	// Independently copy every joined fact before the callback. The expected
	// previous-receipt pointer and binding slices cannot remain caller aliases.
	if json.Unmarshal(assistantChatJSON(expected), &g.expected) != nil || json.Unmarshal(assistantChatJSON(o.binding), &g.admission.binding) != nil {
		return nil, errAssistantRunInvalid
	}
	g.admission.owner, g.admission.runID, g.admission.host, g.admission.stage = o.enrollment.Owner, o.runID, o.host, stage
	if json.Unmarshal(assistantChatJSON(expected), &g.admission.expected) != nil {
		return nil, errAssistantRunInvalid
	}
	if err = g.Check(ctx); err != nil {
		return nil, err
	}
	return g, nil
}
func (g *assistantDialectDraftReservationGuard) Check(ctx context.Context) error {
	if g == nil || g.self != g || g.private == nil || g.data == nil || g.stage != g.admission.stage || !reflect.DeepEqual(g.expected, g.admission.expected) {
		return errAssistantRunScope
	}
	if err := g.checkPinned(ctx); err != nil {
		return err
	}
	if err := g.checkRun(); err != nil {
		return err
	}
	o, a := g.owner, g.admission
	if a.owner != o.enrollment.Owner || a.runID != o.runID || a.host != o.host || !reflect.DeepEqual(a.binding, o.binding) {
		return errAssistantRunScope
	}
	if !assistantDialectDraftOwnerMatches(g.run, a.owner) {
		return errAssistantRunScope
	}
	return assistantDialectDraftFence(g.ledger, g.run, o.host)
}
func (g *assistantDialectDraftReservationGuard) bindBaseline(ctx context.Context, ledger assistantRunLedger) error {
	if err := g.Check(ctx); err != nil {
		return err
	}
	run, err := assistantRunBound(&ledger, g.admission.binding, g.admission.runID)
	if err != nil || !reflect.DeepEqual(*run, g.run) {
		return errors.Join(errAssistantRunConflict, err)
	}
	return nil
}
func (g *assistantDialectDraftReservationGuard) adoptSaved(ledger assistantRunLedger) error {
	// Only the shared leaf's independently prepared saved graph reaches here,
	// after a known successful write (or its freshly decoded false baseline).
	run, err := assistantRunBound(&ledger, g.admission.binding, g.admission.runID)
	if err != nil {
		return err
	}
	g.ledger, g.run = ledger, *run
	return nil
}
func (g *assistantDialectDraftReservationGuard) Close() error {
	if g == nil || g.self != g {
		return errAssistantRunScope
	}
	return g.assistantDialectDraftHostHold.Close()
}
func (g *assistantDialectDraftHostHold) closeData() error {
	var err error
	if g.data != nil {
		err = closeDialectArtifactOwner(context.Background(), "factory-data", g.data.root.Close)
		g.data = nil
	}
	if g.private != nil {
		err = errors.Join(err, g.private.Close())
		g.private = nil
	}
	return err
}
