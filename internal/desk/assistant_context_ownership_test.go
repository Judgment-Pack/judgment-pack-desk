package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func assistantContextStartFixture(t *testing.T, s *Server, token string, start assistantRunStart) assistantRunStart {
	t.Helper()
	document := assistantContextDocumentFixture(t, s, token, "fixture-document")
	selector := assistantRunContextSelector{Document: document.Document}
	if assistantAuthoringPurpose(start.Purpose) {
		pane := assistantContextPaneFixture(t, s, token, document.Document, "fixture-pane-"+start.Purpose, start.Purpose)
		selector.Pane = pane.Pane
	}
	start.Context = &selector
	return start
}

// Service/host ownership fixture: real shared production Prepare, source and
// canonical Store; synthetic execution owns no provider or Runtime. Actual
// provider quiescence and two-turn integration are separate production gates.
type assistantContextOwnershipExecutor struct {
	prepare  func(context.Context, assistantRunSource) (contract.Checkpoint, error)
	execute  func(context.Context, *assistantRunExecution) error
	prepares atomic.Int32
}

func (*assistantContextOwnershipExecutor) assistantRetainedChat()  {}
func (*assistantContextOwnershipExecutor) assistantCanonicalRuns() {}
func (x *assistantContextOwnershipExecutor) Prepare(ctx context.Context, source assistantRunSource) (contract.Checkpoint, error) {
	x.prepares.Add(1)
	if x.prepare != nil {
		return x.prepare(ctx, source)
	}
	return (&assistantCreationExecutor{}).Prepare(ctx, source)
}
func (x *assistantContextOwnershipExecutor) Execute(ctx context.Context, e *assistantRunExecution) error {
	if x.execute != nil {
		return x.execute(ctx, e)
	}
	cp := *e.canonicalBinding.Writer.Checkpoint()
	cp.Status = "complete"
	return e.canonicalBinding.Writer.Save(ctx, *e.canonicalBinding, cp)
}
func assistantContextDone(t *testing.T, e *assistantRunExecution) {
	t.Helper()
	select {
	case <-e.done:
	case <-time.After(5 * time.Second):
		t.Fatal("owner did not finish")
	}
}

// A positively settled approval pause keeps its original owner and lease alive.
// The completion wait deliberately requires the opposite lifetime, so wait for
// the service's explicit parked notification and assert both channels here.
func assistantRetainedWaitParked(t *testing.T, s *Server, token string, e *assistantRunExecution) assistantRunView {
	t.Helper()
	h := s.assistantRunService()
	wake, unsubscribe, err := h.subscribe(e.record.ID)
	assistantRunMust(t, err)
	defer unsubscribe()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for {
		e.canonicalMu.Lock()
		state, invocationDone := e.lifecycle, e.invocationDone
		settled := e.settlement.safe(nil) && e.resumeInvocation != nil
		e.canonicalMu.Unlock()
		if state == "parked" {
			if !settled {
				t.Fatal("parked owner lacks positive invocation settlement")
			}
			select {
			case <-invocationDone:
			default:
				t.Fatal("parked invocation did not join")
			}
			break
		}
		select {
		case <-wake:
		case <-e.done:
			t.Fatal("owner completed before parking", state)
		case <-deadline.C:
			t.Fatal("owner did not park", state)
		}
	}
	h.mu.Lock()
	retained := h.active[e.record.ID] == e && h.lease == e.writer.lease
	h.mu.Unlock()
	if !retained || e.ctx.Err() != nil {
		t.Fatal("parked owner lost its original execution or lease")
	}
	select {
	case <-e.done:
		t.Fatal("owner completed while parked")
	default:
	}
	view := assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+e.record.ID, nil), 200)
	if view.State != "awaiting-input" || view.Checkpoint == nil || (view.Checkpoint.Status != "paused" && view.Checkpoint.Status != "stopped") || view.Checkpoint.Approval == nil || view.Checkpoint.Approval.State != "asked" {
		t.Fatal("parked approval misclassified", view.State)
	}
	return view
}

func TestAssistantContextReservationPromotion(t *testing.T) {
	c := newAssistantChatContexts()
	b := assistantContextTestBinding()
	d := assistantContextTestDocument(t, c, b, "one")
	h := assistantContextTestRetain(t, c, b, assistantRunContextSelector{Document: d.Document}, "chat", b.Owner.ID)
	assistantRunMust(t, c.reserveRun(h))
	assistantRunMust(t, c.retireDocument(b, d.Document))
	if h.Chat().IsDisposed() {
		t.Fatal("pending preparation lost its context")
	}
	final := b
	final.ExecutionKey = strings.Repeat("a", 64)
	final.Unit = "requests"
	assistantRunMust(t, c.promoteRun(h, "run-one", final))
	restored, err := c.retainRun("run-one", final)
	assistantRunMust(t, err)
	if restored.Chat() != h.Chat() {
		t.Fatal("promotion replaced Chat")
	}
	wrong := final
	wrong.RequestID = "another"
	if _, err := c.retainRun("run-one", wrong); err == nil {
		t.Fatal("retained run accepted changed source")
	}
	h.Release()
	c.settleRun("run-one")
	if h.Chat().IsDisposed() {
		t.Fatal("live invocation disposed before release")
	}
	restored.Release()
	if !h.Chat().IsDisposed() {
		t.Fatal("retired settled context not disposed")
	}
	if c.reserved != 0 || len(c.runs) != 0 || len(c.entries) != 0 {
		t.Fatal("reservation leaked")
	}
}
func TestAssistantContextPendingCapacityAndBinding(t *testing.T) {
	c := newAssistantChatContexts()
	defer c.close()
	b := assistantContextTestBinding()
	d := assistantContextTestDocument(t, c, b, "one")
	handles := make([]*assistantChatContextHandle, 0, assistantRunCountLimit)
	for i := 0; i < assistantRunCountLimit; i++ {
		h := assistantContextTestRetain(t, c, b, assistantRunContextSelector{Document: d.Document}, "chat", b.Owner.ID)
		assistantRunMust(t, c.reserveRun(h))
		handles = append(handles, h)
	}
	excess := assistantContextTestRetain(t, c, b, assistantRunContextSelector{Document: d.Document}, "chat", b.Owner.ID)
	if err := c.reserveRun(excess); err != errAssistantRunUnavailable {
		t.Fatal("pending capacity not reserved", err)
	}
	if err := c.bindRun(excess, "extra-run"); err != errAssistantRunUnavailable {
		t.Fatal("direct binding bypassed reservation limit", err)
	}
	final := b
	final.ExecutionKey = strings.Repeat("a", 64)
	final.Unit = "requests"
	final.RequestID = "forged"
	if err := c.promoteRun(handles[0], "forged-run", final); err == nil {
		t.Fatal("promotion changed an immutable binding fact")
	}
	for _, h := range handles {
		h.Release()
		h.Release()
	}
	excess.Release()
	if c.reserved != 0 || len(c.runs) != 0 {
		t.Fatal("failed promotion/release leaked pending slots")
	}
}
func TestAssistantContextRequiredBeforePrepare(t *testing.T) {
	s, token, start, _ := assistantProtocolFixture(t, nil)
	x := &assistantContextOwnershipExecutor{}
	s.assistantRunExecutor = x
	if w := policyCall(s, token, "POST", "/api/assistant-runs", start); w.Code < 400 {
		t.Fatal("missing context admitted")
	}
	start.Context = &assistantRunContextSelector{Document: strings.Repeat("a", 64)}
	if w := policyCall(s, token, "POST", "/api/assistant-runs", start); w.Code < 400 {
		t.Fatal("unknown context admitted")
	}
	start = assistantContextStartFixture(t, s, token, start)
	other := policyTestSession(t, s)
	if w := policyCall(s, other, "POST", "/api/assistant-runs", start); w.Code < 400 {
		t.Fatal("foreign session reused context")
	}
	if w := policyCall(s, token, "POST", "/api/assistant-contexts/documents/release", assistantContextDocumentRelease{start.Context.Document}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := policyCall(s, token, "POST", "/api/assistant-runs", start); w.Code < 400 {
		t.Fatal("retired context admitted")
	}
	if x.prepares.Load() != 0 {
		t.Fatal("refused context reached Prepare")
	}
}
func TestAssistantContextPrepareRetirementTransfer(t *testing.T) {
	s, token, start, _ := assistantProtocolFixture(t, nil)
	prepared := make(chan *runcontext.ChatContext, 1)
	proceed := make(chan struct{})
	entered := make(chan *assistantRunExecution, 1)
	complete := make(chan struct{})
	x := &assistantContextOwnershipExecutor{}
	x.prepare = func(ctx context.Context, source assistantRunSource) (contract.Checkpoint, error) {
		cp, err := (&assistantCreationExecutor{}).Prepare(ctx, source)
		if err != nil {
			return cp, err
		}
		if source.chat == nil || cp.Context.Scope != source.chat.Scope() {
			return cp, errors.New("Prepare omitted original Chat scope")
		}
		prepared <- source.chat
		select {
		case <-proceed:
		case <-ctx.Done():
			return cp, ctx.Err()
		}
		return cp, nil
	}
	x.execute = func(ctx context.Context, e *assistantRunExecution) error {
		entered <- e
		select {
		case <-complete:
		case <-ctx.Done():
			return ctx.Err()
		}
		cp := *e.canonicalBinding.Writer.Checkpoint()
		cp.Status = "complete"
		return e.canonicalBinding.Writer.Save(ctx, *e.canonicalBinding, cp)
	}
	s.assistantRunExecutor = x
	start = assistantContextStartFixture(t, s, token, start)
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- policyCall(s, token, "POST", "/api/assistant-runs", start) }()
	var chat *runcontext.ChatContext
	select {
	case chat = <-prepared:
	case <-time.After(3 * time.Second):
		t.Fatal("Prepare did not receive context")
	}
	w := policyCall(s, token, "POST", "/api/assistant-contexts/documents/release", assistantContextDocumentRelease{start.Context.Document})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if chat.IsDisposed() {
		t.Fatal("release disposed pending preparation")
	}
	close(proceed)
	view := assistantProtocolView(t, <-result, 201)
	var e *assistantRunExecution
	select {
	case e = <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("promoted owner did not launch")
	}
	if e.source.chat != chat || e.contextHandle.Chat() != chat || e.canonicalInput.Chat != chat || e.record.Checkpoint.Context.Scope != chat.Scope() {
		t.Fatal("Prepare/durable input/Execute changed Chat")
	}
	substitute := runcontext.NewChatContext("")
	defer substitute.Dispose()
	if err := e.bindContextInput(&runcontext.Input{Chat: substitute}); err == nil {
		t.Fatal("substituted initial context accepted")
	}
	close(complete)
	assistantContextDone(t, e)
	if got := assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+view.ID, nil), 200); got.State != "complete" {
		t.Fatal(got.State)
	}
	if !chat.IsDisposed() {
		t.Fatal("retired context remained after proven no-adapter owner completion")
	}
	// Existing admission lookup remains authoritative even though scope retired.
	if retry := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 200); retry.ID != view.ID || x.prepares.Load() != 1 {
		t.Fatal("idempotency allocated or prepared replacement")
	}
}
func TestAssistantContextPreparationFailureDoesNotQuarantine(t *testing.T) {
	s, token, start, _ := assistantProtocolFixture(t, nil)
	var chat *runcontext.ChatContext
	first := true
	x := &assistantContextOwnershipExecutor{}
	x.prepare = func(ctx context.Context, source assistantRunSource) (contract.Checkpoint, error) {
		cp, err := (&assistantCreationExecutor{}).Prepare(ctx, source)
		if err != nil {
			return cp, err
		}
		if chat == nil {
			chat = source.chat
		} else if chat != source.chat {
			return cp, errors.New("explicit retry changed same document Chat")
		}
		if first {
			first = false
			return cp, errors.New("positive preparation-only refusal")
		}
		return cp, nil
	}
	s.assistantRunExecutor = x
	start = assistantContextStartFixture(t, s, token, start)
	if w := policyCall(s, token, "POST", "/api/assistant-runs", start); w.Code < 400 {
		t.Fatal("fixture refusal admitted")
	}
	h := s.assistantRunService()
	h.mu.Lock()
	registry := h.contexts.registry
	owners := len(h.contextOwners)
	h.mu.Unlock()
	registry.mu.Lock()
	reserved, runs := registry.reserved, len(registry.runs)
	registry.mu.Unlock()
	if reserved != 0 || runs != 0 || owners != 0 || chat == nil || chat.IsDisposed() {
		t.Fatal("preparation-only failure leaked/poisoned scope")
	}
	admitted := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	assistantProtocolWait(t, s, token, admitted.ID, "complete")
	if x.prepares.Load() != 2 {
		t.Fatal("explicit retry did not prepare exactly once")
	}
}
func TestAssistantContextUnknownInvocationQuarantines(t *testing.T) {
	s, token, start, _ := assistantProtocolFixture(t, nil)
	entered := make(chan *assistantRunExecution, 1)
	x := &assistantContextOwnershipExecutor{execute: func(_ context.Context, e *assistantRunExecution) error {
		entered <- e
		e.contextInvocationStarted()
		// Explicit unknown-usage fixture: outer return is present; callback/provider
		// settlement evidence is absent. This is not a provider qualification claim.
		e.contextInvocationReturned(&assistantExecutionInvocation{owner: e, started: true, returned: true, closed: true})
		return errors.New("unknown provider release")
	}}
	s.assistantRunExecutor = x
	start = assistantContextStartFixture(t, s, token, start)
	view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	e := <-entered
	assistantContextDone(t, e)
	chat := e.contextHandle.Chat()
	h := s.assistantRunService()
	h.mu.Lock()
	owner := h.contextOwners[view.ID]
	quarantined := owner != nil && owner.quarantined
	h.mu.Unlock()
	if !quarantined || chat.IsDisposed() {
		t.Fatal("unknown callback lifetime was treated as settled")
	}
	if _, err := h.contexts.registry.retain(e.record.Binding, *start.Context, "chat", start.OwnerID); err == nil {
		t.Fatal("unknown entry granted future reuse")
	}
	w := policyCall(s, token, "POST", "/api/assistant-contexts/documents/release", assistantContextDocumentRelease{start.Context.Document})
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	h.close()
	if chat.IsDisposed() {
		t.Fatal("shutdown falsely disposed uncertain callback access")
	}
}

func TestAssistantContextPausedOwnerRetainsOriginalChat(t *testing.T) {
	for _, finish := range []string{"continue", "cancel", "authority"} {
		t.Run(finish, func(t *testing.T) {
			s, token, start, _ := assistantProtocolFixture(t, nil)
			entered := make(chan *assistantRunExecution, 2)
			x := &assistantContextOwnershipExecutor{execute: func(ctx context.Context, e *assistantRunExecution) error {
				entered <- e
				cp := *e.canonicalBinding.Writer.Checkpoint()
				if len(e.record.Actions) == 0 {
					cp.Status = "paused"
					cp.Used = cp.Limit
				} else {
					cp.Status = "complete"
				}
				return e.canonicalBinding.Writer.Save(ctx, *e.canonicalBinding, cp)
			}}
			s.assistantRunExecutor = x
			start = assistantContextStartFixture(t, s, token, start)
			view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
			original := <-entered
			assistantContextDone(t, original)
			paused := assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+view.ID, nil), 200)
			if paused.State != "paused-budget" {
				t.Fatal(paused.State)
			}
			chat := original.contextHandle.Chat()
			w := policyCall(s, token, "POST", "/api/assistant-contexts/documents/release", assistantContextDocumentRelease{start.Context.Document})
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
			if chat.IsDisposed() {
				t.Fatal("document release settled paused run")
			}
			switch finish {
			case "continue":
				a := assistantContinuationAction(paused, "extend-budget")
				assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+view.ID+"/actions", a), 200)
				resumed := <-entered
				if resumed.source.chat != chat || resumed.canonicalInput.Chat != chat || resumed.record.ID != original.record.ID {
					t.Fatal("continuation replaced original Chat/run")
				}
				assistantContextDone(t, resumed)
			case "cancel":
				assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+view.ID+"/actions", assistantRunAction{ID: "cancel-paused", Kind: "cancel", Revision: paused.Revision}), 200)
			case "authority":
				s.sessions.revoke(token)
				deadline := time.Now().Add(3 * time.Second)
				for !chat.IsDisposed() && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
			}
			if !chat.IsDisposed() {
				t.Fatal("settled retired owner leaked Chat", finish)
			}
		})
	}
}

func TestAssistantContextAuthoritativeSavedDeletion(t *testing.T) {
	s, token, _, _ := assistantProtocolFixture(t, nil)
	doc := assistantContextDocumentFixture(t, s, token, "delete-document")
	r := httptest.NewRequest("GET", "/api/assistant-runs", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	binding, _, err := s.assistantRunIdentity(r)
	assistantRunMust(t, err)
	binding.Project = s.assistantRuns().project
	registry := s.assistantRunService().contexts.registry
	held := assistantContextTestRetain(t, registry, binding, assistantRunContextSelector{Document: doc.Document}, "chat", "chat-one")
	chat := held.Chat()
	held.Release()
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	empty := map[string]any{"version": 1, "chats": []any{}}
	if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", "stale", empty); w.Code < 400 {
		t.Fatal("stale deletion committed")
	}
	if chat.IsDisposed() {
		t.Fatal("failed CAS retired Chat")
	}
	if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, empty); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !chat.IsDisposed() {
		t.Fatal("authoritative deletion did not retire removed owner")
	}
	if got := removedSavedContextOwners([]byte(`{"version":1,"chats":[{"id":"one"},{"id":"two"}]}`), []byte(`{"version":1,"chats":[{"id":"two"},{"id":"three"}]}`)); fmt.Sprint(got) != "[one]" {
		t.Fatal("removed owner set", got)
	}
}

func TestAssistantContextAdmittedPromotionFailureStops(t *testing.T) {
	s, token, start, _ := assistantProtocolFixture(t, nil)
	var chat *runcontext.ChatContext
	var executes atomic.Int32
	x := &assistantContextOwnershipExecutor{}
	x.prepare = func(ctx context.Context, source assistantRunSource) (contract.Checkpoint, error) {
		cp, err := (&assistantCreationExecutor{}).Prepare(ctx, source)
		chat = source.chat
		// A definitive registry shutdown is injected after preparation, while the
		// original pending reference is still held. Admission must remain durable.
		s.assistantRunHost.contexts.registry.close()
		return cp, err
	}
	x.execute = func(context.Context, *assistantRunExecution) error { executes.Add(1); return nil }
	s.assistantRunExecutor = x
	start = assistantContextStartFixture(t, s, token, start)
	if w := policyCall(s, token, "POST", "/api/assistant-runs", start); w.Code < 400 {
		t.Fatal("failed ownership transfer acknowledged launch")
	}
	ledger := assistantAuthoringLedger(t, s)
	if len(ledger.Runs) != 1 || ledger.Runs[0].State != "stopped" || executes.Load() != 0 || chat == nil || !chat.IsDisposed() {
		t.Fatal("admitted failure lost durable stop or retained proven-unused context")
	}
	retry := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 200)
	if retry.ID != ledger.Runs[0].ID || retry.State != "stopped" || x.prepares.Load() != 1 {
		t.Fatal("retry reconstructed failed admitted owner")
	}
}

func TestAssistantContextParkedOriginalStoreContinuation(t *testing.T) {
	s, token, start, _ := assistantProtocolFixture(t, nil)
	entered := make(chan *assistantRunExecution, 1)
	var original *runcontext.RunBinding
	var chat *runcontext.ChatContext
	var cleanup, resumed atomic.Int32
	x := &assistantContextOwnershipExecutor{}
	x.execute = func(ctx context.Context, e *assistantRunExecution) error {
		original = e.canonicalBinding
		chat = e.contextHandle.Chat()
		cp := assistantProtocolAsked(*original.Writer.Checkpoint())
		cp.Status = "paused"
		if err := original.Writer.Save(ctx, *original, cp); err != nil {
			return err
		}
		// Explicit synthetic settlement qualifies only this no-provider fixture.
		proof := assistantInvocationSettlement{PauseAcknowledged: true, ReceiptsAcknowledged: true, CallbacksJoined: true, ProviderReleased: true, NoPendingWork: true}
		if err := e.settleRuntimeInvocation(ctx, proof, func(ctx context.Context, next *runcontext.Input) error {
			resumed.Add(1)
			if next.Chat != chat || next.Persistence == original || next.Persistence.Writer.Store != original.Writer.Store || next.Persistence.Writer.ID != original.Writer.ID || next.Approval == nil || !next.Approval.Approved {
				return errors.New("retained context/store changed")
			}
			cp := *next.Persistence.Writer.Checkpoint()
			cp.Status = "running"
			cp.Approval.State = "approved"
			at := "2026-10-28T23:00:00Z"
			cp.Approval.DecidedAt = &at
			if err := next.Persistence.Writer.Save(ctx, *next.Persistence, cp); err != nil {
				return err
			}
			cp.Status = "complete"
			cp.Approval.State = "answered"
			if err := json.Unmarshal([]byte(`{"content":[{"type":"text","text":"owned fixture result"}]}`), &cp.Approval.Result); err != nil {
				return err
			}
			return next.Persistence.Writer.Save(ctx, *next.Persistence, cp)
		}, func() { cleanup.Add(1) }); err != nil {
			return err
		}
		entered <- e
		return nil
	}
	s.assistantRunExecutor = x
	start = assistantContextStartFixture(t, s, token, start)
	view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	var e *assistantRunExecution
	select {
	case e = <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("fixture failed before settlement")
	}
	paused := assistantRetainedWaitParked(t, s, token, e)
	if chat == nil || chat.IsDisposed() || e.canonicalInput.Chat != chat || e.canonicalBinding != original || cleanup.Load() != 0 || resumed.Load() != 0 {
		t.Fatal("park replaced or disposed Chat, binding or owner")
	}
	if w := policyCall(s, token, "POST", "/api/assistant-contexts/documents/release", assistantContextDocumentRelease{start.Context.Document}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	approved := true
	a := assistantRunAction{ID: "context-approve", Kind: "approval", Revision: paused.Revision, CallID: paused.Checkpoint.Approval.CallID, ArgsDigest: paused.ApprovalReference.ArgsDigest, ShownDigest: &paused.ApprovalReference.ArgsDigest, Approved: &approved}
	assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+view.ID+"/actions", a), 200)
	assistantContextDone(t, e)
	complete := assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+view.ID, nil), 200)
	if complete.State != "complete" || resumed.Load() != 1 || cleanup.Load() != 1 || !chat.IsDisposed() {
		t.Fatal("original owner continuation did not settle exactly once")
	}
}
