package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

const assistantRunActiveLimit = 8
const assistantRunSubscriberLimit = 32

type assistantRunService struct {
	mu             sync.Mutex
	server         *Server
	closed         bool
	lease          *assistantRunLease
	active         map[string]*assistantRunExecution
	subscribers    map[uint64]assistantRunSubscriber
	nextSubscriber uint64
}
type assistantRunSubscriber struct {
	runID string
	wake  chan struct{}
}
type assistantRunExecution struct {
	service  *assistantRunService
	source   assistantRunSource
	request  *http.Request
	record   assistantRunRecord
	writer   *assistantRunWriter
	decision *contract.CheckpointApprovalDecision
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	progress assistantRunProgressState
	input    *assistantChatCustody
}

func (s *Server) assistantRunService() *assistantRunService {
	s.assistantRunServiceMu.Lock()
	defer s.assistantRunServiceMu.Unlock()
	if s.assistantRunHost == nil {
		s.assistantRunHost = &assistantRunService{server: s, active: map[string]*assistantRunExecution{}, subscribers: map[uint64]assistantRunSubscriber{}}
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
	lease, err := h.server.assistantRuns().Acquire(ctx)
	if err != nil {
		return err
	}
	h.lease = lease
	return nil
}
func (h *assistantRunService) releaseIdle() {
	if len(h.active) == 0 && h.lease != nil {
		_ = h.lease.Close()
		h.lease = nil
	}
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
	if err = writer.Checkpoint(r.Context(), cp); err != nil {
		return record, true, err
	}
	record, err = h.lease.store.Read(r.Context(), record.Binding, record.ID)
	if err != nil {
		return record, true, err
	}
	if err = h.launch(r, record, source, writer, nil); err != nil {
		_ = writer.Stop(context.Background())
		return record, true, err
	}
	return record, true, nil
}
func (h *assistantRunService) launch(r *http.Request, record assistantRunRecord, source assistantRunSource, writer *assistantRunWriter, decision *contract.CheckpointApprovalDecision) error {
	_, session, err := h.server.assistantRunIdentity(r)
	if err != nil {
		return err
	}
	epoch, err := newAssistantRunProgressEpoch()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopSession := context.AfterFunc(session, cancel)
	stopPolicy := func() bool { return true }
	if h.server.signIn != nil {
		h.server.signIn.mu.Lock()
		epoch := h.server.signIn.epoch
		h.server.signIn.mu.Unlock()
		stopPolicy = context.AfterFunc(epoch, cancel)
		if epoch.Err() != nil {
			cancel()
		}
	}
	if session.Err() != nil {
		cancel()
	}
	execution := &assistantRunExecution{service: h, source: source, request: r.Clone(ctx), record: record, writer: writer, decision: decision, ctx: ctx, cancel: cancel, done: make(chan struct{}), input: &assistantChatCustody{}}
	execution.progress.epoch = epoch
	execution.progress.retained = map[string]assistantRunPreview{}
	for _, p := range record.DisplayPreview {
		key := ""
		if p.TaskID != nil {
			key = *p.TaskID
		}
		execution.progress.retained[key] = p
	}
	h.active[record.ID] = execution
	go func() {
		defer close(execution.done)
		defer stopSession()
		defer stopPolicy()
		defer cancel()
		monitorDone := make(chan struct{})
		go func() {
			defer close(monitorDone)
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if h.server.authorizeAssistantRun(execution.request, record) != nil {
						cancel()
						return
					}
				}
			}
		}()
		err := execution.Authorize(ctx)
		if err == nil {
			err = h.executor(source.Start.Purpose).Execute(ctx, execution)
		}
		canceled := ctx.Err() != nil
		cancel()
		<-monitorDone
		bounded, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		_ = execution.saveProgressPreview(bounded, false)
		latest, readErr := writer.lease.store.Read(bounded, record.Binding, record.ID)
		if readErr == nil && !assistantRunTerminal(latest.State) {
			// A valid pause survives stream disconnection and process reacquisition.
			// Unreported or canceled work closes dispatch and preserves uncertainty.
			paused := err == nil && !canceled && latest.Checkpoint != nil
			// cancel above only stops the monitor; distinguish executor outcome using
			// its persisted checkpoint, not a canceled HTTP reader.
			if question, questionErr := pendingAssistantRunQuestion(latest); paused && questionErr == nil && question != nil && latest.Checkpoint.Status == "complete" {
				_ = writer.State(bounded, "awaiting-input")
			} else if paused && latest.Checkpoint.Approval != nil && latest.Checkpoint.Approval.State == "asked" && latest.Checkpoint.Status == "stopped" {
				_ = writer.State(bounded, "awaiting-input")
			} else if paused && latest.State == "awaiting-client" && latest.Checkpoint.Status == "stopped" {
				// A client-required pause has already closed its adapter context.
			} else if err == nil && !canceled && latest.Checkpoint != nil && latest.Checkpoint.Status == "paused" {
				_ = writer.State(bounded, "paused-budget")
			} else if err == nil && !canceled && latest.Checkpoint != nil && latest.Checkpoint.Status == "complete" {
				_ = writer.State(bounded, "complete")
			} else {
				_ = writer.Stop(bounded)
			}
		}
		h.mu.Lock()
		delete(h.active, record.ID)
		h.notifyLocked(record.ID)
		h.releaseIdle()
		h.mu.Unlock()
	}()
	return nil
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
	if !assistantRunText(action.ID, 128) || !contains([]string{"cancel", "approval", "abandon", "answer", "extend-budget"}, action.Kind) || action.Revision == 0 {
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
	if action.Kind == "cancel" && (action.CallID != "" || action.ArgsDigest != "" || action.Approved != nil || action.Note != nil) {
		return record, errAssistantRunInvalid
	}
	if action.Kind == "approval" && (h.server.assistantRunExecutor == nil || h.active[id] != nil || len(h.active) >= assistantRunActiveLimit) {
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
	writer, err := h.lease.Writer(r.Context(), record.Binding, id)
	if err != nil {
		return record, err
	}
	var decision *contract.CheckpointApprovalDecision
	var source assistantRunSource
	if action.Kind == "approval" {
		cp := record.Checkpoint
		if record.State != "awaiting-input" || cp == nil || cp.Status != "stopped" || cp.Approval == nil || cp.Approval.State != "asked" || cp.Approval.CallID != action.CallID || action.Approved == nil || assistantRunArgsDigest(cp.Approval.Args) != action.ArgsDigest || action.Note != nil && runcontext.U16Length(*action.Note) > 2000 {
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
		if source.Binding.Phase != "work" || !assistantRunFreshSourceEqual(source, assistantRunSource{Binding: record.Binding, Original: source.Original, Request: source.Request}) {
			return record, errAssistantRunScope
		}
		decision = &contract.CheckpointApprovalDecision{CallID: action.CallID, Approved: *action.Approved, Note: action.Note}
	}
	err = writer.change(r.Context(), false, func(run *assistantRunRecord) (bool, error) {
		if run.Revision != action.Revision {
			return false, errAssistantRunConflict
		}
		run.Actions = append(run.Actions, assistantRunActionReceipt{ID: action.ID, Digest: digest, Kind: action.Kind, Decision: decision})
		if action.Kind == "cancel" {
			if active := h.active[id]; active != nil {
				run.DisplayPreview = active.progressPreviewLocked()
			}
			stopAssistantRun(run)
		} else {
			run.State = "admitted"
		}
		return true, nil
	})
	if err != nil {
		if active := h.active[id]; active != nil {
			active.cancel()
		}
		return record, err
	}
	if action.Kind == "cancel" {
		if active := h.active[id]; active != nil {
			active.cancel()
		}
	} else {
		latest, e := h.lease.store.Read(r.Context(), record.Binding, id)
		if e != nil {
			return record, e
		}
		if err = h.launch(r, latest, source, writer, decision); err != nil {
			_ = writer.Stop(context.Background())
			return record, err
		}
	}
	h.notifyLocked(id)
	return h.lease.store.Read(r.Context(), record.Binding, id)
}

// abandon is deliberately separate from original-session cancel/approval. The
// exclusive process lease plus the active registry prove that no executor can
// still own this paused record; a persisted state string alone cannot prove it.
// h.mu stays held through admission and persistence, closing the launch race.
func (h *assistantRunService) abandon(r *http.Request, record assistantRunRecord, action assistantRunAction) (assistantRunRecord, error) {
	if action.CallID != "" || action.ArgsDigest != "" || action.Approved != nil || action.Note != nil {
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
	if record.Historical || !assistantRunPaused(record.State) || record.Revision != action.Revision || h.active[record.ID] != nil {
		return record, errAssistantRunConflict
	}
	if err = h.acquire(r.Context()); err != nil {
		return record, err
	}
	defer h.releaseIdle()
	if record.Binding.Project != h.lease.store.project {
		return record, errAssistantRunScope
	}
	writer, err := h.lease.Writer(r.Context(), record.Binding, record.ID)
	if err != nil {
		return record, err
	}
	err = writer.change(r.Context(), false, func(run *assistantRunRecord) (bool, error) {
		current, _, err := h.server.assistantRunIdentity(r)
		if err != nil {
			return false, err
		}
		if !assistantRunIdentityMatches(current, identity) || current.Principal != run.Binding.Principal {
			return false, errAssistantRunScope
		}
		if run.Revision != action.Revision || !assistantRunPaused(run.State) {
			return false, errAssistantRunConflict
		}
		run.Actions = append(run.Actions, assistantRunActionReceipt{ID: action.ID, Digest: digest, Kind: action.Kind, AuthorizingSession: current.Session, AuthorizingPolicy: current.Policy})
		stopAssistantRun(run)
		return true, nil
	})
	if err != nil {
		return record, err
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

// Only the fixed diagnostic activates here; general chat factory activation is separate.
func (h *assistantRunService) executor(purpose string) assistantRunExecutor {
	if purpose == assistantDiagnosticPurpose {
		return &assistantDiagnosticExecutor{}
	}
	return h.server.assistantRunExecutor
}
