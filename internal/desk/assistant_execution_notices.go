package desk

import (
	"context"
	"errors"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Only the retained Gate owns Emit. Provider/adapter callbacks own an immutable
// invocation instead. Replacing this slot is legal only after the entire old
// adapter (including delegation, callbacks and Boundary defers) has returned.
// A zero publication count alone is NOT that proof: Gate can refuse before it
// reaches the physical transport or before it enters Emit.
type assistantGateNoticeRelay struct {
	mu      sync.Mutex
	current *assistantExecutionInvocation
	retired *assistantExecutionInvocation
	active  int
	failure error
}

func (r *assistantGateNoticeRelay) emit(event contract.RunEvent) {
	r.mu.Lock()
	s := r.current
	if s == nil || r.failure != nil {
		r.failure = errAssistantRunScope
		r.mu.Unlock()
		return
	}
	r.active++
	r.mu.Unlock()
	s.hostEvent(event)
	r.mu.Lock()
	r.active--
	r.mu.Unlock()
}

func (r *assistantGateNoticeRelay) install(s *assistantExecutionInvocation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s == nil || r.current != nil || r.active != 0 || r.failure != nil || r.retired != nil && (r.retired.owner != s.owner || r.retired.generation+1 != s.generation || !r.retired.quiescent()) {
		return errAssistantRunScope
	}
	if r.retired != nil {
		old := r.retired
		old.mu.Lock()
		defer old.mu.Unlock()
		if len(old.acceptedNativeClaims) != 0 {
			if old.store == nil || old.store != s.store || old.binding == nil || s.binding == nil || old.binding.Writer == nil || s.binding.Writer == nil || old.binding.Writer.Store != old.store || s.binding.Writer.Store != s.store || old.binding.Writer.ID == "" || old.binding.Writer.ID != s.binding.Writer.ID {
				return errAssistantRunScope
			}
			// Evidence crosses only this same-owner consecutive-generation
			// reopen. It never supplies admission, dispatch or a new receipt.
			s.priorNativeClaims = append([]string(nil), old.acceptedNativeClaims...)
		}
		old.priorNativeClaims, old.acceptedNativeClaims = nil, nil
	}
	r.current = s
	return nil
}

func (r *assistantGateNoticeRelay) retire(s *assistantExecutionInvocation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current != s || r.active != 0 || r.failure != nil || !s.quiescent() {
		return errAssistantRunScope
	}
	r.current, r.retired = nil, s
	return nil
}

func (r *assistantGateNoticeRelay) parked(generation uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current == nil && r.active == 0 && r.failure == nil && r.retired != nil && r.retired.generation == generation && r.retired.quiescent()
}

// All members selecting authority are fixed at construction. In particular an
// old Emit never calls publishCanonicalHostEvent to select a newer binding.
type assistantExecutionInvocation struct {
	owner      *assistantRunExecution
	generation uint64
	binding    *runcontext.RunBinding
	store      *assistantCanonicalRunStore
	ctx        context.Context
	mu         sync.Mutex
	started    bool
	returned   bool
	closed     bool
	ended      bool
	active     int
	failure    error
	models     []*assistantModelDispatch
	natives    []*assistantNativeSettlement
	// Immutable claimed-row bytes, bounded by the canonical dispatch journal.
	// Accepted evidence is published only after this invocation settles.
	priorNativeClaims, acceptedNativeClaims []string
}

func newAssistantExecutionInvocation(ctx context.Context, e *assistantRunExecution) (*assistantExecutionInvocation, error) {
	e.canonicalMu.Lock()
	defer e.canonicalMu.Unlock()
	if g, ok := ctx.Value(assistantInvocationGenerationKey{}).(assistantInvocationGeneration); ok && (g.owner != e || g.number != e.generation) {
		return nil, errAssistantRunScope
	}
	if e.canonicalBinding == nil || e.canonicalStore == nil || e.stopRequested || e.lifecycle != "running" {
		return nil, errAssistantRunScope
	}
	return &assistantExecutionInvocation{owner: e, generation: e.generation, binding: e.canonicalBinding, store: e.canonicalStore, ctx: e.generationContext(ctx, e.generation)}, nil
}

func (s *assistantExecutionInvocation) check(ctx context.Context) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	g, tagged := ctx.Value(assistantInvocationGenerationKey{}).(assistantInvocationGeneration)
	if closed || tagged && (g.owner != s.owner || g.number != s.generation) {
		return errAssistantRunScope
	}
	e := s.owner
	e.canonicalMu.Lock()
	current := e.generation == s.generation && e.canonicalBinding == s.binding && e.canonicalStore == s.store && !e.stopRequested && e.lifecycle == "running"
	e.canonicalMu.Unlock()
	if !current {
		return errAssistantRunScope
	}
	return e.Authorize(e.generationContext(ctx, s.generation))
}

func (s *assistantExecutionInvocation) fail(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	if s.failure == nil {
		s.failure = err
	}
	s.mu.Unlock()
}

func (s *assistantExecutionInvocation) enterEvent() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.active++
	return true
}

func (s *assistantExecutionInvocation) leaveEvent() {
	s.mu.Lock()
	s.active--
	s.mu.Unlock()
}

func (s *assistantExecutionInvocation) deliver(event contract.RunEvent) {
	if !s.enterEvent() {
		return // A retired callback has no authority, including over its successor.
	}
	defer s.leaveEvent()
	if event.Error != nil {
		s.fail(errAssistantRunDispatch)
	}
	if err := s.check(s.ctx); err != nil {
		s.fail(err)
		return
	}
	err := s.owner.deliverCanonicalEvent(s.ctx, event)
	s.fail(err)
	if err == nil && event.End != nil {
		// Both released adapters put End through Boundary.Event, which drains
		// queued notices before persistence/delivery. Delegation forwards its
		// final End after joining children. This is necessary but never sufficient
		// by itself: outer unwind and physical release are checked separately.
		s.mu.Lock()
		s.ended = true
		s.mu.Unlock()
	}
}

func (s *assistantExecutionInvocation) hostEvent(event contract.RunEvent) {
	if !s.enterEvent() {
		s.fail(errAssistantRunScope)
		return
	}
	defer s.leaveEvent()
	if err := s.check(s.ctx); err != nil {
		s.fail(err)
		return
	}
	_, err := s.binding.Writer.Event(s.ctx, *s.binding, event)
	if err == nil {
		err = s.owner.deliverCanonicalEvent(s.ctx, event)
	}
	s.fail(err)
	if err != nil {
		s.owner.changed(err)
	}
}

func (s *assistantExecutionInvocation) adapter(inner adapter.Adapter) adapter.Adapter {
	return adapter.Func(func(ctx context.Context, run *adapter.Run) (err error) {
		if err = s.check(ctx); err != nil {
			return err
		}
		s.mu.Lock()
		if s.started || s.closed {
			s.mu.Unlock()
			return errAssistantRunScope
		}
		s.started = true
		s.mu.Unlock()
		s.owner.contextInvocationStarted()
		defer func() {
			s.mu.Lock()
			s.returned, s.closed = true, true
			if err != nil && s.failure == nil {
				s.failure = err
			}
			s.mu.Unlock()
			s.owner.contextInvocationReturned(s)
		}()
		return inner.Run(s.owner.generationContext(ctx, s.generation), run)
	})
}

func (s *assistantExecutionInvocation) quiescent() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started && s.returned && s.closed && s.active == 0
}

func (s *assistantExecutionInvocation) model(d *assistantModelDispatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.closed {
		return errAssistantRunScope
	}
	s.models = append(s.models, d)
	return nil
}

func (s *assistantExecutionInvocation) native(n *assistantNativeSettlement) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.closed || n.generation != s.generation || n.binding != s.binding {
		return errAssistantRunScope
	}
	s.natives = append(s.natives, n)
	return nil
}

// A nil adapter return/end event is not release evidence. Require every real
// HTTP body and native callback/model receipt, plus the canonical durable state.
func (s *assistantExecutionInvocation) proof() (assistantInvocationSettlement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || !s.returned || !s.closed || !s.ended || s.active != 0 || s.failure != nil || len(s.models)+len(s.natives) == 0 {
		return assistantInvocationSettlement{}, errAssistantRunDispatch
	}
	for _, d := range s.models {
		if d.uncertain.Load() || !d.handlerReturned.Load() || !d.consumerClosed.Load() || !d.receiptAcknowledged.Load() || d.consumerCloseFailed.Load() {
			return assistantInvocationSettlement{}, errAssistantRunDispatch
		}
		// A fully acknowledged pre-admission refusal owns no provider body.
		if d.admitted.Load() && (!d.physicalEOF.Load() || !d.physicalClosed.Load() || d.physicalCloseFailed.Load()) {
			return assistantInvocationSettlement{}, errAssistantRunDispatch
		}
	}
	nativePause := false
	claims := append([]string(nil), s.priorNativeClaims...)
	for _, n := range s.natives {
		settled, approval := n.invocationSettled()
		if !settled {
			return assistantInvocationSettlement{}, errAssistantRunDispatch
		}
		nativePause = nativePause || approval
		if approval {
			n.mu.Lock()
			claim := n.approvalClaim
			n.mu.Unlock()
			if claim == "" {
				return assistantInvocationSettlement{}, errAssistantRunDispatch
			}
			claims = append(claims, claim)
		}
	}
	return assistantInvocationSettlement{PauseAcknowledged: true, ReceiptsAcknowledged: true, CallbacksJoined: true, ProviderReleased: true, NoPendingWork: true, NativePauseCancellation: nativePause, nativeOwner: s.owner, nativeGeneration: s.generation, nativeBinding: s.binding, nativeClaims: claims}, nil
}

// execute owns one complete outer invocation. The same completion function is
// retained for an explicit decision, so chat projection and workflow completion
// are not lost when control returns through the service's owner loop.
func (x *assistantExecution) execute(ctx context.Context, complete func(context.Context) error) error {
	if x.invocation != nil {
		ctx = x.invocation.owner.generationContext(ctx, x.invocation.generation)
	}
	err := x.Adapter.Run(ctx, x.Run)
	if x.invocation == nil {
		if err == nil && complete != nil {
			return complete(ctx)
		}
		return err
	}
	s := x.invocation
	if retireErr := x.notices.retire(s); retireErr != nil {
		return errors.Join(err, retireErr)
	}
	if err != nil {
		return err
	}
	run, readErr := s.owner.writer.lease.store.Read(ctx, s.owner.record.Binding, s.owner.record.ID)
	if readErr != nil {
		return readErr
	}
	if assistantRunApprovalPause(run) && run.Checkpoint.Approval.Runtime != nil {
		proof, proofErr := s.proof()
		if proofErr != nil || validateAssistantCanonicalProjection(run) != nil || !sameAssistantRunCheckpoint(s.binding.Writer.Checkpoint(), run.Checkpoint) || s.binding.Writer.Failed() {
			return errAssistantRunDispatch
		}
		resume := func(ctx context.Context, next *runcontext.Input) error {
			held, openErr := s.owner.reopenAssistantExecution(ctx, x.prompt, x, next)
			if openErr != nil {
				return openErr
			}
			defer held.Close()
			return held.execute(ctx, complete)
		}
		if err := s.owner.settleRuntimeInvocation(ctx, proof, resume, x.runtime.Close); err != nil {
			return err
		}
		// The service checks exact persisted receipts/dispatches and owner identity
		// before parking. Close transfers resource cleanup only to that owner.
		s.mu.Lock()
		s.acceptedNativeClaims = append([]string(nil), proof.nativeClaims...)
		s.mu.Unlock()
		x.retained = true
		return nil
	}
	if complete != nil {
		return complete(ctx)
	}
	return nil
}
