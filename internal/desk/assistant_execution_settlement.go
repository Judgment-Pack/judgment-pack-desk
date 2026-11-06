package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// This capability observes one actual native invocation. It admits no new work
// after Manager returns, and is distinct from both process release and a
// successful result receipt. The outer invocation separately qualifies retention.
type assistantNativeSettlement struct {
	execution  *assistantRunExecution
	generation uint64
	binding    *runcontext.RunBinding
	writer     *assistantRunWriter
	lease      *assistantRunLease
	captured   bool

	mu                                                             sync.Mutex
	closed, aborted, joinedClosed                                  bool
	joined                                                         chan struct{}
	callbacks                                                      map[*assistantNativeCallback]struct{}
	releases                                                       int
	released, receiptFailed                                        bool
	approvalCall, approvalOperation                                string
	approvalClaim                                                  string
	planCall                                                       string
	planOperation                                                  assistantRunOperation
	plan                                                           *assistantPlanDecisionReference
	planHead                                                       assistantCanonicalHead
	modelAcknowledged, modelKnown, modelApproval, modelNotAdmitted bool
}
type assistantNativeCallback struct {
	owner                     *assistantNativeSettlement
	call                      string
	operation                 assistantRunOperation
	plan                      *assistantPlanDecisionReference
	planHead                  assistantCanonicalHead
	admitted, returned, ended bool
}

func newAssistantNativeSettlement(ctx context.Context, e *assistantRunExecution) *assistantNativeSettlement {
	e.canonicalMu.Lock()
	defer e.canonicalMu.Unlock()
	return &assistantNativeSettlement{execution: e, generation: e.generation, binding: e.canonicalBinding, writer: e.writer, lease: e.writer.lease, captured: e.generationMatches(ctx, e.generation), joined: make(chan struct{}), callbacks: map[*assistantNativeCallback]struct{}{}}
}
func (s *assistantNativeSettlement) begin(call string) (*assistantNativeCallback, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errAssistantRunDispatch
	}
	token := &assistantNativeCallback{owner: s, call: call}
	s.callbacks[token] = struct{}{}
	return token, nil
}
func (s *assistantNativeSettlement) admit(token *assistantNativeCallback, op assistantRunOperation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.callbacks[token]; !ok || token.owner != s || token.ended || token.admitted || token.call == "" {
		return errAssistantRunScope
	}
	token.operation, token.admitted = op, true
	return nil
}
func (s *assistantNativeSettlement) end(token *assistantNativeCallback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.callbacks[token]; !ok {
		s.receiptFailed = true
		return
	}
	token.ended = true
	delete(s.callbacks, token)
	s.closeJoinedLocked()
}
func (s *assistantNativeSettlement) closeJoinedLocked() {
	if s.closed && len(s.callbacks) == 0 && !s.joinedClosed {
		s.joinedClosed = true
		close(s.joined)
	}
}
func (s *assistantNativeSettlement) closeAndJoin(owner context.Context) error {
	s.mu.Lock()
	s.closed = true
	s.closeJoinedLocked()
	joined := s.joined
	s.mu.Unlock()
	bounded, cancel := context.WithTimeout(owner, 5*time.Second)
	defer cancel()
	select {
	case <-joined:
		return nil
	case <-bounded.Done():
		s.mu.Lock()
		s.aborted = true
		s.mu.Unlock()
		return bounded.Err()
	}
}
func (s *assistantNativeSettlement) release(acknowledged bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releases++
	s.released = s.releases == 1 && acknowledged
}
func (s *assistantNativeSettlement) eventResult(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	s.receiptFailed = true
	s.mu.Unlock()
}
func (s *assistantNativeSettlement) receipt(token *assistantNativeCallback, approval bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.callbacks[token]
	if !ok || token.owner != s || token.ended || token.returned || s.aborted || err != nil {
		s.receiptFailed = true
		return
	}
	token.returned = true
	if approval {
		if s.approvalCall != "" {
			s.receiptFailed = true
			return
		}
		s.approvalCall, s.approvalOperation = token.call, token.operation.ID
	}
	if token.plan != nil {
		if approval || s.planCall != "" || s.approvalCall != "" {
			s.receiptFailed = true
			return
		}
		copy := *token.plan
		s.planCall, s.planOperation, s.plan, s.planHead = token.call, token.operation, &copy, token.planHead
	}
}

func (s *assistantNativeSettlement) fixedScope(ctx context.Context) error {
	e := s.execution
	if !s.captured || s.binding == nil {
		return errAssistantRunScope
	}
	e.canonicalMu.Lock()
	current := e.generation == s.generation && e.canonicalBinding == s.binding && e.writer == s.writer && e.writer.lease == s.lease && !e.stopRequested && e.lifecycle == "running" && e.ctx.Err() == nil
	e.canonicalMu.Unlock()
	if !current {
		return errAssistantRunScope
	}
	return e.Authorize(e.generationContext(ctx, s.generation))
}
func (s *assistantNativeSettlement) approvalCheckpoint(ctx context.Context, call string) (assistantRunRecord, error) {
	if err := s.fixedScope(ctx); err != nil {
		return assistantRunRecord{}, err
	}
	run, err := s.lease.store.Read(ctx, s.execution.record.Binding, s.execution.record.ID)
	if err != nil {
		return run, err
	}
	cp := run.Checkpoint
	if validateAssistantCanonicalProjection(run) != nil || cp == nil || cp.Status != "paused" || cp.Approval == nil || cp.Approval.State != "asked" || cp.Approval.CallID != call || cp.Durable == nil || cp.Durable.PendingHandoff != nil || !sameAssistantRunCheckpoint(s.binding.Writer.Checkpoint(), cp) {
		return run, errAssistantRunScope
	}
	return run, nil
}

// The private writer already retains observed results with its own bounded
// cancellation handling. This check qualifies only the known approval label;
// it never supplies an uncancelled context for new admission or dispatch.
func (s *assistantNativeSettlement) approvalCallback(token *assistantNativeCallback, stop *assistantAgentStop) error {
	s.mu.Lock()
	_, present := s.callbacks[token]
	eligible := present && token.owner == s && token.admitted && !token.returned && !token.ended && !s.aborted
	op, call := token.operation, token.call
	s.mu.Unlock()
	if !eligible || stop == nil || !stop.approvalFor(call) {
		return errAssistantRunScope
	}
	ctx, cancel := context.WithTimeout(s.execution.generationContext(s.execution.ctx, s.generation), 5*time.Second)
	defer cancel()
	run, err := s.approvalCheckpoint(ctx, call)
	if err != nil {
		return err
	}
	for _, held := range run.Operations {
		if held.ID == op.ID {
			if held.Kind == "callback" && held.State == "dispatched" && held.Name == op.Name && held.CallID == op.CallID && bytes.Equal(held.Intent, op.Intent) {
				return nil
			}
			break
		}
	}
	return errAssistantRunScope
}
func (s *assistantNativeSettlement) approvalSettled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed && s.joinedClosed && !s.aborted && len(s.callbacks) == 0 && s.releases == 1 && s.released && !s.receiptFailed && s.approvalCall != ""
}

// The complete physical callback receipt must agree with the exact acknowledged
// pause, independently of Manager's process release. Unknown results and any
// other unfinished operation cannot qualify an approval stop.
func (s *assistantNativeSettlement) approvalModel(operation assistantRunOperation) error {
	if !s.approvalSettled() {
		return errAssistantRunScope
	}
	s.mu.Lock()
	call, callback := s.approvalCall, s.approvalOperation
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(s.execution.generationContext(s.execution.ctx, s.generation), 5*time.Second)
	defer cancel()
	run, err := s.approvalCheckpoint(ctx, call)
	if err != nil {
		return err
	}
	modelFound, callbackFound := false, false
	for _, held := range run.Operations {
		switch held.ID {
		case operation.ID:
			modelFound = held.Kind == "model" && held.State == "dispatched" && held.Name == operation.Name && held.CallID == operation.CallID && bytes.Equal(held.Intent, operation.Intent)
		case callback:
			var outcome assistantAgentOutcome
			callbackFound = held.Kind == "callback" && held.State == "completed" && json.Unmarshal(held.Result, &outcome) == nil && outcome.Stop == "approval" && outcome.Failure == nil && outcome.Answer != nil
		default:
			if held.State != "completed" && held.State != "refused" {
				return errAssistantRunScope
			}
		}
	}
	if !modelFound || !callbackFound {
		return errAssistantRunScope
	}
	// InvokeNativeRoute has acknowledged this exact read-agent claim before
	// entering the host. Capture it while still claimed: the package records
	// the Tool's waiting error only after this physical model returns.
	claim := ""
	for _, row := range run.Checkpoint.Durable.Dispatches {
		if row.Kind != "agent" || !row.Read || row.State != "claimed" || row.Tool != operation.Name || row.Request != string(operation.Intent) {
			continue
		}
		task := ""
		if row.Scope.Task != nil {
			task = *row.Scope.Task
		}
		if row.Scope.Execution != s.binding.Scope.Execution || row.Scope.Role != s.binding.Scope.Role || task != s.binding.Scope.Task {
			return errAssistantRunScope
		}
		if claim != "" || row.Result != nil || row.Failure != nil || len(row.Artifacts) != 0 || row.RepeatResultJSON != nil || row.PlanPause != nil {
			return errAssistantRunScope
		}
		raw, err := json.Marshal(row)
		if err != nil {
			return err
		}
		claim = string(raw)
	}
	if claim == "" {
		return errAssistantRunScope
	}
	s.mu.Lock()
	s.approvalClaim = claim
	s.mu.Unlock()
	return nil
}

// A Plan pause has its own approval id. It is qualified independently of an
// action approval and cannot supply or consume action authority.
func (s *assistantNativeSettlement) planCheckpoint(ctx context.Context, expected *assistantPlanDecisionReference) (assistantRunRecord, error) {
	if err := s.fixedScope(ctx); err != nil {
		return assistantRunRecord{}, err
	}
	run, err := s.lease.store.Read(ctx, s.execution.record.Binding, s.execution.record.ID)
	if err != nil {
		return run, err
	}
	cp := run.Checkpoint
	if expected == nil || validateAssistantCanonicalProjection(run) != nil || cp == nil || cp.Durable == nil || cp.Durable.PendingHandoff != nil || !reflect.DeepEqual(assistantRunPlanReference(run), expected) || !sameAssistantRunCheckpoint(s.binding.Writer.Checkpoint(), cp) {
		return run, errAssistantRunScope
	}
	return run, nil
}

func (s *assistantNativeSettlement) planCallback(token *assistantNativeCallback, stop *assistantAgentStop) error {
	s.mu.Lock()
	_, present := s.callbacks[token]
	eligible := present && token.owner == s && token.admitted && !token.returned && !token.ended && !s.aborted && token.plan == nil
	op, call := token.operation, token.call
	s.mu.Unlock()
	if !eligible || stop == nil {
		return errAssistantRunScope
	}
	expected := stop.planFor(call, op.Name)
	ctx, cancel := context.WithTimeout(s.execution.generationContext(s.execution.ctx, s.generation), 5*time.Second)
	defer cancel()
	run, err := s.planCheckpoint(ctx, expected)
	if err != nil {
		return err
	}
	for _, held := range run.Operations {
		if held.ID != op.ID {
			continue
		}
		if held.Kind != "callback" || held.State != "dispatched" || held.Name != op.Name || held.CallID != op.CallID || !bytes.Equal(held.Intent, op.Intent) {
			return errAssistantRunScope
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, present := s.callbacks[token]; !present || token.ended || token.returned || s.aborted {
			return errAssistantRunScope
		}
		token.plan, token.planHead = expected, run.Canonical.Head
		return nil
	}
	return errAssistantRunScope
}

// The actual provider release and callback join precede the model receipt. The
// package marks PlanPause only after this host call returns; this proof merely
// keeps the live owner available for that original package operation.
func (s *assistantNativeSettlement) planModel(operation assistantRunOperation) error {
	s.mu.Lock()
	settled := s.closed && s.joinedClosed && !s.aborted && len(s.callbacks) == 0 && s.releases == 1 && s.released && !s.receiptFailed && s.planCall != "" && s.approvalCall == ""
	callback, expected, head := s.planOperation, s.plan, s.planHead
	s.mu.Unlock()
	if !settled {
		return errAssistantRunScope
	}
	ctx, cancel := context.WithTimeout(s.execution.generationContext(s.execution.ctx, s.generation), 5*time.Second)
	defer cancel()
	run, err := s.planCheckpoint(ctx, expected)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(head, run.Canonical.Head) {
		return errAssistantRunScope
	}
	modelFound, callbackFound := false, false
	for _, held := range run.Operations {
		switch held.ID {
		case operation.ID:
			modelFound = held.Kind == "model" && held.State == "dispatched" && held.Name == operation.Name && held.CallID == operation.CallID && bytes.Equal(held.Intent, operation.Intent)
		case callback.ID:
			var outcome assistantAgentOutcome
			callbackFound = held.Kind == "callback" && held.State == "completed" && held.Name == callback.Name && held.CallID == callback.CallID && bytes.Equal(held.Intent, callback.Intent) && json.Unmarshal(held.Result, &outcome) == nil && outcome.Stop == "plan" && outcome.Failure == nil && outcome.Answer != nil
		default:
			if held.State != "completed" && held.State != "refused" {
				return errAssistantRunScope
			}
		}
	}
	if !modelFound || !callbackFound {
		return errAssistantRunScope
	}
	return nil
}

// Called only after the physical model receipt. P3a approval qualification stays
// unchanged; ordinary successful attempts also require exact release and join.
func (s *assistantNativeSettlement) modelReceipt(err error, known, approval bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelAcknowledged = err == nil
	s.modelKnown, s.modelApproval = known, approval
}

func (s *assistantNativeSettlement) invocationSettled() (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	joined := s.closed && s.joinedClosed && !s.aborted && len(s.callbacks) == 0 && !s.receiptFailed && s.modelAcknowledged
	if s.modelNotAdmitted {
		return joined && s.releases == 0 && s.approvalCall == "" && s.planCall == "", false
	}
	return joined && s.releases == 1 && s.released && s.modelKnown, s.modelApproval
}

func (s *assistantNativeSettlement) notAdmitted(acknowledged bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelNotAdmitted = acknowledged
}
