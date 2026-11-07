package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

const assistantRunActiveLimit = 8
const assistantRunSubscriberLimit = 32

type assistantRunService struct {
	contexts                  *assistantContextHTTPState
	contextOwners             map[string]*assistantContextOwnership
	mu                        sync.Mutex
	server                    *Server
	closed                    bool
	lease                     *assistantRunLease
	dialectReadRecoveryPins   int
	dialectReadRecoveryWriter assistantDialectReadRecoveryWriter
	active                    map[string]*assistantRunExecution
	subscribers               map[uint64]assistantRunSubscriber
	nextSubscriber            uint64
}
type assistantRunSubscriber struct {
	runID string
	wake  chan struct{}
}
type assistantRunExecution struct {
	// Exact transport cancellation is published only after its closure is ready.
	// Stop closes its pipes before waiting on canonical/lease admission locks.
	contextHandle           *assistantChatContextHandle
	contextInvoked          bool // canonicalMu; actual outer adapter entered, not returned
	contextUnknown          bool // canonicalMu; sticky unknown lifetime, never auto-disposed
	runtimeOwner            atomic.Pointer[assistantRuntime]
	retainedRuntime         *assistantRuntime         // canonicalMu; survives process publication clearing
	gateNotices             *assistantGateNoticeRelay // installed once; generation sinks retire before parking
	service                 *assistantRunService
	source                  assistantRunSource
	request                 *http.Request
	record                  assistantRunRecord
	writer                  *assistantRunWriter
	planDecision            *assistantPlanDecisionReference
	decision                *contract.CheckpointApprovalDecision
	ctx                     context.Context
	cancel                  context.CancelFunc
	done                    chan struct{}
	progress                assistantRunProgressState
	input                   *assistantChatCustody
	initialInput            *assistantChatInput // consumed once by first open; approvals prepare fresh input
	canonicalMu             sync.Mutex
	canonicalBinding        *runcontext.RunBinding
	canonicalStore          *assistantCanonicalRunStore
	canonicalInput          *runcontext.Input
	generation              uint64
	lifecycle               string // canonicalMu: running, settling, parked, resuming, closed
	settlement              *assistantInvocationSettlement
	resumeInvocation        assistantRuntimeContinuation
	closeOwner              func()
	continuations           chan *runcontext.Input
	invocationDone          chan struct{}
	observation             *assistantObservationClaim
	observationPhysicalUsed bool
	stopRequested           bool
	finalizing              bool // protected by service.mu
}

func (s *Server) assistantRunService() *assistantRunService {
	s.assistantRunServiceMu.Lock()
	defer s.assistantRunServiceMu.Unlock()
	if s.assistantRunHost == nil {
		s.assistantRunHost = &assistantRunService{server: s, contexts: newAssistantContextHTTPState(), active: map[string]*assistantRunExecution{}, subscribers: map[uint64]assistantRunSubscriber{}}
		s.assistantRunHost.contexts.onRetire = s.assistantRunHost.retireContextAuthority
	}
	return s.assistantRunHost
}
func (h *assistantRunService) acquire(ctx context.Context) error {
	if h.closed {
		return errAssistantRunUnavailable
	}
	if h.lease != nil {
		return nil
	}
	if h.dialectReadRecoveryPins != 0 {
		return errRetainedArtifactBusy
	}
	lease, err := h.server.assistantRuns().Acquire(ctx)
	if err != nil {
		return err
	}
	h.lease = lease
	return nil
}
func (h *assistantRunService) releaseIdle() {
	if len(h.active) == 0 && h.dialectReadRecoveryPins == 0 && h.lease != nil {
		_ = h.lease.Close()
		h.lease = nil
	}
}
func assistantGuardedApprovalRetry(source assistantRunSource, prior assistantRunRecord) bool {
	if assistantUnkeptApproval(prior) == nil {
		return false
	}
	// The framework execution id is fresh, while the admitted principal,
	// original request/chat revision, source and connection scope must match.
	source.Binding.ExecutionKey = prior.Binding.ExecutionKey
	source.Binding.Unit = prior.Binding.Unit
	return assistantRunSourceMatchesRecord(source, prior)
}

func (h *assistantRunService) start(r *http.Request, start assistantRunStart) (assistantRunRecord, bool, error) {
	identity, _, err := h.server.assistantRunIdentity(r)
	if err != nil {
		return assistantRunRecord{}, false, err
	}
	body, _ := json.Marshal(start)
	bodyDigest := digestOf(body)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return assistantRunRecord{}, false, errAssistantRunUnavailable
	}
	store := h.server.assistantRuns()
	existing, err := store.lookup(r.Context(), identity, "", start.AdmissionID, false)
	if err == nil {
		if existing.BodyDigest != bodyDigest {
			return assistantRunRecord{}, false, errAssistantRunConflict
		}
		return existing, false, nil
	}
	if !errors.Is(err, errAssistantRunNotFound) {
		return assistantRunRecord{}, false, err
	}
	if h.executor(start.Purpose) == nil {
		return assistantRunRecord{}, false, errAssistantRunUnavailable
	}
	if len(h.active) >= assistantRunActiveLimit {
		return assistantRunRecord{}, false, errAssistantRunUnavailable
	}
	if err = h.acquire(r.Context()); err != nil {
		return assistantRunRecord{}, false, err
	}
	defer h.releaseIdle()
	source, err := h.server.readAssistantRunSource(r, start, identity, h.lease.store.project)
	if err != nil {
		return assistantRunRecord{}, false, err
	}
	// A fresh admission id is not permission to repeat this exact original
	// request after an approved physical effect lost its canonical result.
	_, err = store.transaction(r.Context(), func(ledger *assistantRunLedger) (bool, error) {
		for _, prior := range ledger.Runs {
			if h.active[prior.ID] == nil && assistantGuardedApprovalRetry(source, prior) {
				return false, errAssistantRunUnavailable
			}
		}
		return false, nil
	})
	if err != nil {
		return assistantRunRecord{}, false, err
	}
	canonical, err := assistantCanonicalAdmission(h.executor(start.Purpose), source)
	if err != nil {
		return assistantRunRecord{}, false, err
	}
	observation, err := assistantObservationAdmission(h.executor(start.Purpose), source)
	if err != nil {
		return assistantRunRecord{}, false, err
	}
	pendingContext, err := h.prepareContext(&source)
	if err != nil {
		return assistantRunRecord{}, false, err
	}
	defer pendingContext.Release()
	cp, err := h.executor(start.Purpose).Prepare(r.Context(), source)
	if err != nil {
		return assistantRunRecord{}, false, err
	}
	source.Binding.ExecutionKey = cp.RequestKey
	source.Binding.Unit = cp.Unit
	if cp.Phase != source.Binding.Phase || cp.Status != "running" {
		return assistantRunRecord{}, false, errAssistantRunScope
	}
	if _, err = readAssistantRunCheckpoint(cp, source.Binding); err != nil {
		return assistantRunRecord{}, false, err
	}
	record, created, err := h.lease.Admit(r.Context(), source.Binding, start.AdmissionID, bodyDigest, func() error {
		current, _, err := h.server.assistantRunIdentity(r)
		if err != nil {
			return err
		}
		if !assistantRunIdentityMatches(current, identity) {
			return errAssistantRunScope
		}
		fresh, err := h.server.resolveAssistantRun(r, start, identity, h.lease.store.project)
		if err != nil {
			return err
		}
		fresh.Binding.ExecutionKey = source.Binding.ExecutionKey
		fresh.Binding.Unit = source.Binding.Unit
		if !assistantRunFreshSourceEqual(source, fresh) {
			return errAssistantRunConflict
		}
		return nil
	})
	if err != nil || !created {
		return record, created, err
	}
	writer, err := h.lease.Writer(r.Context(), record.Binding, record.ID)
	if err != nil {
		return record, true, err
	}
	if observation != nil {
		err = writer.initializeObservation(r.Context(), source, *observation, cp)
	} else if canonical {
		err = writer.initializeCanonical(r.Context(), cp)
	} else {
		err = writer.Checkpoint(r.Context(), cp)
	}
	if err != nil {
		return record, true, err
	}
	record, err = h.lease.store.Read(r.Context(), record.Binding, record.ID)
	if err != nil {
		return record, true, err
	}
	if pendingContext != nil {
		if err = h.contexts.registry.promoteRun(pendingContext, record.ID, record.Binding); err != nil {
			_ = writer.Stop(context.Background())
			return record, true, err
		}
		h.rememberContext(record)
	}
	if err = h.launch(r, record, source, writer, nil); err != nil {
		h.settleContextLocked(record.ID)
		_ = writer.Stop(context.Background())
		return record, true, err
	}
	return record, true, nil
}
func (h *assistantRunService) launch(r *http.Request, record assistantRunRecord, source assistantRunSource, writer *assistantRunWriter, decision *contract.CheckpointApprovalDecision) error {
	prepared, err := h.prepareLaunch(r, record, source, writer, decision, nil)
	if err != nil {
		return err
	}
	prepared.activate()
	return nil
}
func (h *assistantRunService) prepareLaunch(r *http.Request, record assistantRunRecord, source assistantRunSource, writer *assistantRunWriter, decision *contract.CheckpointApprovalDecision, staged *assistantRunRecord) (*assistantPreparedLaunch, error) {
	_, session, err := h.server.assistantRunIdentity(r)
	if err != nil {
		return nil, err
	}
	contextHandle, err := h.launchContext(record, &source)
	if err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			contextHandle.Release()
		}
	}()
	epoch, err := newAssistantRunProgressEpoch()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	execution := &assistantRunExecution{contextHandle: contextHandle, service: h, source: source, request: r.Clone(ctx), record: record, writer: writer, decision: decision, ctx: ctx, done: make(chan struct{}), input: &assistantChatCustody{}, generation: 1, lifecycle: "running", continuations: make(chan *runcontext.Input, 1)}
	if decision != nil {
		for _, receipt := range record.Actions {
			if receipt.Kind == "plan-decision" && receipt.Decision != nil && reflect.DeepEqual(receipt.Decision, decision) {
				execution.planDecision = receipt.PlanDecision
			}
		}
	}
	execution.cancel = func() {
		execution.closeCanonicalDispatch()
		cancel()
	}
	if staged != nil && assistantCanonicalActive(record) {
		raw, err := json.Marshal(staged)
		if err != nil {
			cancel()
			return nil, err
		}
		var frozen assistantRunRecord
		if err = json.Unmarshal(raw, &frozen); err != nil {
			cancel()
			return nil, err
		}
		execution.canonicalStore = &assistantCanonicalRunStore{writer: writer, id: record.Canonical.Head.RunID, prepared: &frozen}
	}
	// Ordinary owners bind before cancellable preflight. Fresh observations let
	// the actual Agent startup open its first writer and attach it before work.
	observation, err := assistantObservationAdmission(h.executor(source.Start.Purpose), source)
	if err == nil && observation != nil {
		err = execution.openObservationClaim(ctx, *observation)
	} else if err == nil {
		err = execution.bindCanonicalInput(execution.generationContext(ctx, 1), &runcontext.Input{Chat: source.chatScope()})
	}
	if err != nil {
		cancel()
		return nil, err
	}
	execution.progress.epoch = epoch
	transferred = true
	return &assistantPreparedLaunch{execution: execution, session: session, cancel: cancel}, nil
}

type assistantPreparedLaunch struct {
	execution *assistantRunExecution
	session   context.Context
	cancel    context.CancelFunc
}

func (p *assistantPreparedLaunch) discard() {
	p.cancel()
	if p.execution.canonicalBinding != nil {
		_ = p.execution.canonicalBinding.Writer.CloseDispatch(*p.execution.canonicalBinding)
	}
	p.execution.contextHandle.Release()
}
func (p *assistantPreparedLaunch) activate() {
	execution, session, cancel := p.execution, p.session, p.cancel
	h, record := execution.service, execution.record
	stopSession := context.AfterFunc(session, execution.cancel)
	stopPolicy := func() bool { return true }
	if h.server.signIn != nil {
		h.server.signIn.mu.Lock()
		epoch := h.server.signIn.epoch
		h.server.signIn.mu.Unlock()
		stopPolicy = context.AfterFunc(epoch, execution.cancel)
		if epoch.Err() != nil {
			execution.cancel()
		}
	}
	if session.Err() != nil {
		execution.cancel()
	}
	execution.progress.retained = map[string]assistantRunPreview{}
	for _, p := range record.DisplayPreview {
		key := ""
		if p.TaskID != nil {
			key = *p.TaskID
		}
		execution.progress.retained[key] = p
	}
	h.active[record.ID] = execution
	if owner := h.contextOwners[record.ID]; owner != nil {
		owner.paused = false
	}
	go execution.runOwner(session, stopSession, stopPolicy, cancel)
}
func (h *assistantRunService) read(r *http.Request, id string) (assistantRunRecord, error) {
	identity, _, err := h.server.assistantRunIdentity(r)
	if err != nil {
		return assistantRunRecord{}, err
	}
	run, err := h.server.assistantRuns().lookup(r.Context(), identity, id, "", true)
	if err == nil && run.Binding.Owner.Kind == assistantDiagnosticPurpose && !assistantRunIdentityMatches(identity, run.Binding) {
		return assistantRunRecord{}, errAssistantRunScope
	}
	return run, err
}
func (h *assistantRunService) action(r *http.Request, id string, action assistantRunAction) (assistantRunRecord, error) {
	if !assistantRunText(action.ID, 128) || !contains([]string{"cancel", "approval", "plan-decision", "abandon", "answer", "extend-budget"}, action.Kind) || action.Revision == 0 {
		return assistantRunRecord{}, errAssistantRunInvalid
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return assistantRunRecord{}, errAssistantRunUnavailable
	}
	record, err := h.read(r, id)
	if err != nil {
		return record, err
	}
	if err = validateAssistantRunActionShape(action); err != nil {
		return record, err
	}
	if action.Kind == "abandon" {
		return h.abandon(r, record, action)
	}
	if err = h.server.authorizeAssistantRun(r, record); err != nil {
		return record, err
	}
	digest := assistantRunActionDigest(action)
	for _, receipt := range record.Actions {
		if receipt.ID == action.ID {
			if receipt.Digest != digest {
				return record, errAssistantRunConflict
			}
			return record, nil
		}
	}
	if record.Binding.Owner.Kind == assistantDiagnosticPurpose && action.Kind != "cancel" {
		return record, errAssistantRunScope
	}
	if action.Kind == "answer" || action.Kind == "extend-budget" {
		return h.continueRun(r, record, action)
	}
	if record.Historical || assistantRunTerminal(record.State) || record.Revision != action.Revision {
		return record, errAssistantRunConflict
	}
	if action.Kind == "cancel" && (action.CallID != "" || action.ArgsDigest != "" || action.ShownDigest != nil || action.Approved != nil || action.Note != nil) {
		return record, errAssistantRunInvalid
	}
	if action.Kind == "approval" {
		if active := h.active[id]; active != nil {
			return h.continueRuntimeApproval(r, record, action, active)
		}
		// A canonical approval cannot reacquire a different process/Gate owner.
		if assistantCanonicalActive(record) {
			return record, errAssistantRunUnavailable
		}
	}
	if (action.Kind == "approval" || action.Kind == "plan-decision") && (h.server.assistantRunExecutor == nil || h.active[id] != nil || len(h.active) >= assistantRunActiveLimit) {
		return record, errAssistantRunUnavailable
	}
	if err = h.acquire(r.Context()); err != nil {
		return record, err
	}
	defer h.releaseIdle()
	// Acquire may recover active work. Never overwrite recovery's revision.
	record, err = h.read(r, id)
	if err != nil {
		return record, err
	}
	if record.Revision != action.Revision {
		return record, errAssistantRunConflict
	}
	active := h.active[id]
	if action.Kind == "cancel" && active != nil && active.finalizing {
		return record, errAssistantRunConflict
	}
	var writer *assistantRunWriter
	if active != nil && action.Kind == "cancel" {
		writer = active.writer
	} else {
		writer, err = h.lease.Writer(r.Context(), record.Binding, id)
		if err != nil {
			return record, err
		}
	}
	var decision *contract.CheckpointApprovalDecision
	var source assistantRunSource
	var planDecision *assistantPlanDecisionReference
	if action.Kind == "approval" || action.Kind == "plan-decision" {
		cp := record.Checkpoint
		if action.Kind == "plan-decision" {
			planDecision = assistantRunPlanReference(record)
			if record.State != "awaiting-input" || planDecision == nil || *planDecision != (assistantPlanDecisionReference{action.CallID, action.PlanRevision, action.CandidateRevision}) {
				return record, errAssistantRunConflict
			}
		} else if record.State != "awaiting-input" || cp == nil || !assistantRunApprovalPause(record) || cp.Approval == nil || cp.Approval.State != "asked" || cp.Approval.CallID != action.CallID || action.Approved == nil || !assistantRunApprovalMatches(cp.Approval, action) || action.Note != nil && runcontext.U16Length(*action.Note) > 2000 {
			return record, errAssistantRunConflict
		}
		identity, _, e := h.server.assistantRunIdentity(r)
		if e != nil {
			return record, e
		}
		start := assistantRunStart{Purpose: "chat", OwnerID: record.Binding.Owner.ID, Revision: record.Binding.Owner.Revision, RequestID: record.Binding.RequestID, AdmissionID: record.AdmissionID}
		source, err = h.server.readAssistantRunSource(r, start, identity, record.Binding.Project)
		if err != nil {
			return record, err
		}
		source.Binding.ExecutionKey = record.Binding.ExecutionKey
		source.Binding.Unit = record.Binding.Unit
		if source.Binding.Phase != "work" || !assistantRunSourceMatchesRecord(source, record) {
			return record, errAssistantRunScope
		}
		contextHandle, contextErr := h.continuationContext(record)
		if contextErr != nil {
			return record, contextErr
		}
		defer contextHandle.Release()
		assistantContextOnSource(&source, contextHandle)
		decision = &contract.CheckpointApprovalDecision{CallID: action.CallID, Approved: *action.Approved, Note: action.Note, ShownDigest: action.ShownDigest}
	}
	err = writer.changeAt(r.Context(), false, &action.Revision, func(run *assistantRunRecord) (bool, error) {
		run.Actions = append(run.Actions, assistantRunActionReceipt{ID: action.ID, Digest: digest, Kind: action.Kind, Decision: decision, PlanDecision: planDecision})
		if action.Kind == "cancel" {
			if active := h.active[id]; active != nil {
				run.DisplayPreview = active.progressPreviewLocked()
			}
			if active == nil {
				stopAssistantRun(run)
			}
		} else {
			run.State = "admitted"
		}
		return true, nil
	})
	if err != nil {
		if active != nil && writer.Err() != nil {
			active.cancel()
		}
		return record, err
	}
	if action.Kind == "cancel" {
		if active == nil {
			h.settleContextLocked(record.ID)
		}
		if active != nil {
			active.cancel()
			// Cleanup and canonical delivery need h.mu; never wait while holding it.
			h.mu.Unlock()
			timer := time.NewTimer(6 * time.Second)
			select {
			case <-active.done:
			case <-timer.C:
				err = context.DeadlineExceeded
			case <-r.Context().Done():
				err = r.Context().Err()
			}
			timer.Stop()
			h.mu.Lock()
			if err != nil {
				return record, err
			}
		}
	} else {
		latest, e := h.lease.store.Read(r.Context(), record.Binding, id)
		if e != nil {
			return record, e
		}
		if err = h.launch(r, latest, source, writer, decision); err != nil {
			h.settleContextLocked(record.ID)
			_ = writer.Stop(context.Background())
			return record, err
		}
	}
	h.notifyLocked(id)
	return writer.lease.store.Read(r.Context(), record.Binding, id)
}

// Abandon admits either a settled retained owner or an inactive paused record.
// The active registry and exclusive process lease establish custody; a persisted
// state string alone cannot. Finalization/reaping never retains service.mu.
func (h *assistantRunService) abandon(r *http.Request, record assistantRunRecord, action assistantRunAction) (assistantRunRecord, error) {
	if action.CallID != "" || action.ArgsDigest != "" || action.ShownDigest != nil || action.Approved != nil || action.Note != nil {
		return record, errAssistantRunInvalid
	}
	identity, _, err := h.server.assistantRunIdentity(r)
	if err != nil {
		return record, err
	}
	if identity.Principal != record.Binding.Principal {
		return record, errAssistantRunScope
	}
	digest := assistantRunActionDigest(action)
	for _, receipt := range record.Actions {
		if receipt.ID == action.ID {
			if receipt.Digest != digest {
				return record, errAssistantRunConflict
			}
			return record, nil
		}
	}
	if record.Historical || !assistantRunPaused(record.State) || record.Revision != action.Revision {
		return record, errAssistantRunConflict
	}
	if err = h.acquire(r.Context()); err != nil {
		return record, err
	}
	defer h.releaseIdle()
	if record.Binding.Project != h.lease.store.project {
		return record, errAssistantRunScope
	}
	expectedRevision := action.Revision
	active := h.active[record.ID]
	var writer *assistantRunWriter
	if active != nil {
		active.canonicalMu.Lock()
		parked := active.lifecycle == "parked"
		active.canonicalMu.Unlock()
		if !parked {
			return record, errAssistantRunConflict
		}
		writer = active.writer
		// Finalize through the current canonical binding before the abandon
		// receipt requires its terminal envelope. The owner still retains its
		// lease until the subsequent cancellation/reap completes.
		h.mu.Unlock()
		active.closeCanonicalDispatch()
		err = active.finalizeCanonicalStop()
		if err == nil {
			var latest assistantRunRecord
			latest, err = writer.lease.store.Read(r.Context(), record.Binding, record.ID)
			if err == nil && !reflect.DeepEqual(latest.Actions, record.Actions) {
				err = errAssistantRunConflict
			}
			expectedRevision = latest.Revision
		}
		h.mu.Lock()
		if err != nil {
			active.cancel()
			return record, err
		}
	} else {
		writer, err = h.lease.Writer(r.Context(), record.Binding, record.ID)
		if err != nil {
			return record, err
		}
	}
	err = writer.change(r.Context(), false, func(run *assistantRunRecord) (bool, error) {
		current, _, err := h.server.assistantRunIdentity(r)
		if err != nil {
			return false, err
		}
		if !assistantRunIdentityMatches(current, identity) || current.Principal != run.Binding.Principal {
			return false, errAssistantRunScope
		}
		if run.Revision != expectedRevision || !assistantRunPaused(run.State) {
			return false, errAssistantRunConflict
		}
		run.Actions = append(run.Actions, assistantRunActionReceipt{ID: action.ID, Digest: digest, Kind: action.Kind, AuthorizingSession: current.Session, AuthorizingPolicy: current.Policy})
		stopAssistantRun(run)
		return true, nil
	})
	if err != nil {
		if active != nil {
			active.cancel()
		}
		return record, err
	}
	if active != nil {
		active.cancel()
	} else {
		h.settleContextLocked(record.ID)
	}
	h.notifyLocked(record.ID)
	return h.lease.store.Read(r.Context(), record.Binding, record.ID)
}
func (h *assistantRunService) notifyLocked(id string) {
	for _, sub := range h.subscribers {
		if sub.runID == id {
			select {
			case sub.wake <- struct{}{}:
			default:
			}
		}
	}
}
func (h *assistantRunService) notify(id string) { h.mu.Lock(); h.notifyLocked(id); h.mu.Unlock() }
func (h *assistantRunService) subscribe(id string) (<-chan struct{}, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.subscribers) >= assistantRunSubscriberLimit {
		return nil, nil, errAssistantRunUnavailable
	}
	h.nextSubscriber++
	key := h.nextSubscriber
	wake := make(chan struct{}, 1)
	h.subscribers[key] = assistantRunSubscriber{runID: id, wake: wake}
	return wake, func() { h.mu.Lock(); delete(h.subscribers, key); h.mu.Unlock() }, nil
}
func (h *assistantRunService) close() {
	h.mu.Lock()
	h.closed = true
	h.settleIdleContextsLocked(nil)
	h.contexts.close()
	done := make([]<-chan struct{}, 0, len(h.active))
	for _, active := range h.active {
		active.cancel()
		done = append(done, active.done)
	}
	h.mu.Unlock()
	timer := time.NewTimer(ShutdownGrace)
	defer timer.Stop()
	for _, ch := range done {
		select {
		case <-ch:
		case <-timer.C:
			return
		}
	}
	h.mu.Lock()
	h.releaseIdle()
	h.mu.Unlock()
}

// These are the only capability boundaries handed to a server runner. Dispatch
// rechecks live session, Policy, saved owner and selected connection each time.
func (e *assistantRunExecution) Authorize(ctx context.Context) error {
	if g, ok := ctx.Value(assistantInvocationGenerationKey{}).(assistantInvocationGeneration); ok {
		e.canonicalMu.Lock()
		current := g.owner == e && g.number == e.generation && !e.stopRequested && e.lifecycle == "running"
		e.canonicalMu.Unlock()
		if !current {
			return errAssistantRunScope
		}
	}
	if err := e.observationError(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := e.ctx.Err(); err != nil {
		return err
	}
	if err := e.writer.Err(); err != nil {
		return err
	}
	identity, _, err := e.service.server.assistantRunIdentity(e.request.WithContext(ctx))
	if err != nil {
		return err
	}
	fresh, err := e.service.server.readAssistantRunSource(e.request.WithContext(ctx), e.source.Start, identity, e.record.Binding.Project)
	if err != nil {
		return err
	}
	fresh.Binding.ExecutionKey = e.source.Binding.ExecutionKey
	fresh.Binding.Unit = e.source.Binding.Unit
	if !assistantRunFreshSourceEqual(e.source, fresh) {
		return errAssistantRunScope
	}
	return nil
}
func (e *assistantRunExecution) Checkpoint(ctx context.Context, cp contract.Checkpoint) error {
	ctx, finish := e.diagnosticPersistenceContext(ctx)
	defer finish()
	if cp.Status != "running" {
		e.clearProgress()
	}
	err := e.writer.Checkpoint(ctx, cp, e.boundaryProgressPreview(false))
	e.changed(err)
	return err
}
func (e *assistantRunExecution) Event(ctx context.Context, id string, event contract.RunEvent) error {
	err := e.writer.Event(ctx, id, event, e.boundaryProgressPreview(event.Message != nil))
	e.changed(err)
	return err
}
func (e *assistantRunExecution) Intent(ctx context.Context, op assistantRunOperation) error {
	if err := e.Authorize(ctx); err != nil {
		e.cancel()
		return err
	}
	err := e.writer.Intent(ctx, op)
	e.changed(err)
	return err
}
func (e *assistantRunExecution) Dispatch(ctx context.Context, id string) error {
	if err := e.Authorize(ctx); err != nil {
		e.cancel()
		return err
	}
	err := e.writer.Dispatch(ctx, id)
	e.changed(err)
	return err
}
func (e *assistantRunExecution) Result(ctx context.Context, id, state string, result []byte) error {
	err := e.writer.Result(ctx, id, state, result)
	e.changed(err)
	return err
}
func (e *assistantRunExecution) changed(err error) {
	if err != nil {
		e.cancel()
	}
	e.service.notify(e.record.ID)
}

// Diagnostic keeps its fixed narrow owner; all other admitted source purposes
// use the closed executor installed by New.
func (h *assistantRunService) executor(purpose string) assistantRunExecutor {
	if purpose == assistantSettingsObservationPurpose {
		return &assistantSettingsObservationExecutor{}
	}
	if purpose == assistantDiagnosticPurpose {
		return &assistantDiagnosticExecutor{}
	}
	return h.server.assistantRunExecutor
}
