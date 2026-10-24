package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/wire"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

type assistantProtocolExecutor struct {
	execute func(context.Context, *assistantRunExecution) error
	calls   atomic.Int32
	review  *bool
}

func (x *assistantProtocolExecutor) Prepare(ctx context.Context, source assistantRunSource) (contract.Checkpoint, error) {
	return assistantFixtureCheckpoint(ctx, source, x.review)
}

// Seed the same immutable session inputs that the adapters consume. Policy
// resolution and checkpoint binding remain owned by ContextualSession.
func assistantFixtureCheckpoint(ctx context.Context, source assistantRunSource, review *bool, policy ...runcontext.PolicyLayer) (contract.Checkpoint, error) {
	chat, err := compileAssistantChatInput(source)
	if err != nil {
		return contract.Checkpoint{}, err
	}
	e := &assistantRunExecution{source: source, record: assistantRunRecord{Binding: source.Binding}}
	named, err := e.assistantExecutionModels()
	if err != nil {
		return contract.Checkpoint{}, err
	}
	input := &runcontext.Input{Phase: source.Binding.Phase, Connection: source.Binding.ConnectionID, CurrentRequest: &chat.Request}
	if source.Profile.Context != nil {
		raw, err := json.Marshal(source.Profile.Context)
		if err != nil {
			return contract.Checkpoint{}, err
		}
		value, err := runcontext.ParseJSON(raw)
		if err != nil {
			return contract.Checkpoint{}, err
		}
		input.Configuration, err = runcontext.ReadConfiguration(value)
		if err != nil {
			return contract.Checkpoint{}, err
		}
	}
	r := &adapter.Run{ModelName: source.Binding.Model, Context: input}
	if review == nil {
		review = chat.Review
	}
	session := runcontext.Session{Prompt: source.Text, Purpose: source.Start.Purpose, Context: input, Critic: chat.Critic, CriticProject: chat.CriticProject, CriticReasoning: chat.CriticReasoning, AdversarialReview: review, PolicyLegacyModels: &named}
	if chat.CriticPurpose != "" {
		session.Purpose = chat.CriticPurpose
	}
	config := source.Connection.Assistant
	if config.Engine == "codex" {
		// Prepare precedes persisted execution authority, so it cannot call
		// assistantAgent. Retain its selected default/override effort here.
		effort, model := config.Agent.Effort, config.Agent.Model
		if preferences := assistantChatPreferences(source); preferences != nil && !preferences.Inherit {
			effort, model = preferences.Effort, preferences.Model
		}
		if model == nil || *model != source.Binding.Model {
			effort = ""
		}
		if chat.Reasoning != nil && chat.Reasoning.Get("model").Str() == source.Binding.Model {
			effort = ""
			if choice := chat.Reasoning.Get("effort"); choice.IsString() {
				effort = choice.Str()
			}
		}
		r.Agent = &adapter.Agent{Model: source.Binding.Model, Effort: effort}
		session.Agent = &runcontext.AgentSession{Model: r.Agent.Model, Effort: r.Agent.Effort}
	} else {
		r.Wire = config.Endpoint.Kind
		host := func(wire.EndpointKind, string) *wire.Value {
			if config.Endpoint.Capabilities == nil {
				return wire.Obj()
			}
			raw, _ := json.Marshal(config.Endpoint.Capabilities)
			value, _ := wire.Parse(raw)
			return value
		}
		capabilities, err := wire.CapabilitiesFor(wire.EndpointKind(r.Wire), r.ModelName, host, nil)
		if err != nil {
			return contract.Checkpoint{}, err
		}
		thinking := chat.Thinking
		if thinking == "" {
			thinking = "off"
		}
		session.Model = &runcontext.ModelSession{Family: r.Wire, Model: r.ModelName, Thinking: thinking, ToolSchemas: string(capabilities.ToolSchemas)}
		if capabilities.ContextWindowTokens != nil && capabilities.OutputTokens != nil {
			session.Model.Budget = &runcontext.ModelBudget{ContextWindowTokens: *capabilities.ContextWindowTokens, OutputTokens: *capabilities.OutputTokens}
		}
	}
	if len(policy) > 0 {
		session.Policy = policy[0]
	}
	session.PolicyFamilies = adapter.PolicyFamilies(r, named)
	session.PolicyConnections = append(adapter.PolicyConnectionIDs(r), named.Critic, named.Child)
	run, err := runcontext.ContextualSession(ctx, session)
	if err != nil {
		return contract.Checkpoint{}, err
	}
	defer run.Context.Close()
	return run.Context.Snapshot("")
}

func assistantFixtureReviewOff(s *Server) {
	off := false
	switch x := s.assistantRunExecutor.(type) {
	case *assistantProtocolExecutor:
		x.review = &off
	case *assistantCodexExecutor:
		x.review = &off
	}
}

func (x *assistantProtocolExecutor) Execute(ctx context.Context, e *assistantRunExecution) error {
	x.calls.Add(1)
	return x.execute(ctx, e)
}
func assistantProtocolFixture(t *testing.T, execute func(context.Context, *assistantRunExecution) error) (*Server, string, assistantRunStart, *assistantProtocolExecutor) {
	t.Helper()
	s, token := policyTestServer(t)
	connection := aiTestConnection(testAIAlpha, "Protocol fixture", "https://example.invalid")
	registry := aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{connection}}
	raw, err := encodeAIRegistry(registry)
	assistantRunMust(t, err)
	assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
	chat := map[string]any{"id": "chat-one", "interaction": "work", "aiConnection": testAIAlpha, "model": "shared-model", "checkpoint": map[string]any{"state": map[string]any{"turns": []any{map[string]any{"id": "message-one", "role": "user", "kind": "message", "text": "Review exact input", "at": "2026-10-20T00:00:00Z"}}}}}
	raw, _ = json.Marshal(chat)
	document := map[string]any{"version": 1, "chats": []json.RawMessage{raw}}
	w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", "absent", document)
	if w.Code != 200 {
		t.Fatal("save journal", w.Code, w.Body.String())
	}
	// Revision uses the bytes retained by the real conversation journal.
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc conversationDocument
	_ = json.Unmarshal(saved.Content, &doc)
	start := assistantRunStart{AdmissionID: "admission-one", Purpose: "chat", OwnerID: "chat-one", Revision: digestOf(doc.Chats[0]), RequestID: "message-one"}
	executor := &assistantProtocolExecutor{execute: execute}
	s.assistantRunExecutor = executor
	return s, token, start, executor
}
func assistantProtocolView(t *testing.T, w *httptest.ResponseRecorder, status int) assistantRunView {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d want %d: %s", w.Code, status, w.Body.String())
	}
	var view assistantRunView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	return view
}
func assistantProtocolWait(t *testing.T, s *Server, token, id, state string) assistantRunView {
	t.Helper()
	return assistantProtocolWaitWithin(t, s, token, id, state, 15*time.Second)
}

func assistantProtocolWaitWithin(t *testing.T, s *Server, token, id, state string, timeout time.Duration) assistantRunView {
	t.Helper()
	h := s.assistantRunService()
	h.mu.Lock()
	execution := h.active[id]
	h.mu.Unlock()

	// Admission registers the execution before returning. Its completion channel
	// closes after final persistence and active-run cleanup, so avoid competing
	// with those writes through repeated authenticated reads. Allow the service's
	// five-second finalization window as well as the executor itself to finish.
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	var waitErr error
	if execution != nil {
		select {
		case <-execution.done:
		case <-ctx.Done():
			waitErr = ctx.Err()
		}
	}
	view := assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+id, nil), 200)
	h.mu.Lock()
	active := h.active[id] != nil
	h.mu.Unlock()
	if waitErr != nil || view.State != state || active {
		checkpoint := "missing"
		if cp := view.Checkpoint; cp != nil {
			checkpoint = fmt.Sprintf("status=%s used=%d limit=%d prompts=%d", cp.Status, cp.Used, cp.Limit, len(cp.Prompts))
		}
		var writerErr error
		if execution != nil {
			writerErr = execution.writer.Err()
		}
		t.Fatalf("run did not settle %s: wait_error=%v state=%s active=%t revision=%d checkpoint={%s} operations=%d events=%d writer_error=%v", state, waitErr, view.State, active, view.Revision, checkpoint, len(view.Operations), len(view.Events), writerErr)
	}
	return view
}

func assistantProtocolComplete(ctx context.Context, e *assistantRunExecution) error {
	cp := *e.record.Checkpoint
	cp.Status = "complete"
	return e.Checkpoint(ctx, cp)
}
func TestAssistantRunProtocolAdmissionAndAuthority(t *testing.T) {
	s, token, start, executor := assistantProtocolFixture(t, assistantProtocolComplete)
	if w := policyCall(s, testToken, "POST", "/api/assistant-runs", start); w.Code != 401 {
		t.Fatal("launch secret became an authenticated principal", w.Code)
	}
	if w := policyCall(s, "", "POST", "/api/assistant-runs", start); w.Code != 401 {
		t.Fatal("anonymous start", w.Code)
	}
	bad := start
	bad.Revision = digestOf([]byte("stale"))
	if w := policyCall(s, token, "POST", "/api/assistant-runs", bad); w.Code != 409 {
		t.Fatal("stale revision admitted", w.Code, w.Body.String())
	}
	bad = start
	bad.RequestID = "not-latest"
	if w := policyCall(s, token, "POST", "/api/assistant-runs", bad); w.Code != 404 {
		t.Fatal("foreign request admitted", w.Code)
	}
	first := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	assistantProtocolWait(t, s, token, first.ID, "complete")
	// The journal changes after a completed response: exact lost-response retry
	// must return its old admission before a fresh saved-owner CAS.
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, map[string]any{"version": 1, "chats": []any{}})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	retry := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 200)
	if retry.ID != first.ID || executor.calls.Load() != 1 {
		t.Fatal("lost response caused second execution")
	}
	bad = start
	bad.RequestID = "changed-body"
	if w := policyCall(s, token, "POST", "/api/assistant-runs", bad); w.Code != 409 {
		t.Fatal("conflicting body accepted", w.Code)
	}
	other, err := s.sessions.createExpiring("another principal", nil, "", "")
	assistantRunMust(t, err)
	for _, route := range []string{"/api/assistant-runs/" + first.ID, "/api/assistant-runs/" + first.ID + "/events"} {
		if w := policyCall(s, other, "GET", route, nil); w.Code != 404 {
			t.Fatal("other session read run", route, w.Code)
		}
	}
	if w := policyCall(s, policyTestSession(t, s), "POST", "/api/assistant-runs", start); w.Code != 404 {
		t.Fatal("other session reused admission", w.Code)
	}
	newSession := policyTestSession(t, s)
	if w := policyCall(s, newSession, "GET", "/api/assistant-runs/"+first.ID, nil); w.Code != 200 {
		t.Fatal("current same-principal session could not read stopped result", w.Code)
	}
	if w := policyCall(s, newSession, "POST", "/api/assistant-runs/"+first.ID+"/actions", assistantRunAction{ID: "new-session-stop", Kind: "cancel", Revision: retry.Revision}); w.Code != 404 {
		t.Fatal("new session inherited action authority", w.Code)
	}
	foreign, foreignToken := policyTestServer(t)
	if w := policyCall(foreign, foreignToken, "GET", "/api/assistant-runs/"+first.ID, nil); w.Code != 404 {
		t.Fatal("other project read run", w.Code)
	}
	// Unbound production purposes fail before allocating any admission.
	s.assistantRunExecutor = nil
	bad = start
	bad.AdmissionID = "unbound"
	if w := policyCall(s, token, "POST", "/api/assistant-runs", bad); w.Code != 503 {
		t.Fatal("unbound executor admitted", w.Code)
	}
	for _, body := range []string{`{"admissionId":"a","AdmissionId":"b"}`, `{"admissionId":"a","admissionId":"b"}`, `{"binding":{},"checkpoint":{}}`} {
		r := httptest.NewRequest("POST", "/api/assistant-runs", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("ambiguous/authority body accepted", w.Code)
		}
	}
}
func TestAssistantRunProtocolDisconnectCancelAndRevocation(t *testing.T) {
	for _, mode := range []string{"disconnect", "session", "policy", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			entered := make(chan *assistantRunExecution, 1)
			s, token, start, _ := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
				entered <- e
				<-ctx.Done()
				return ctx.Err()
			})
			requestCtx, cancel := context.WithCancel(context.Background())
			raw, _ := json.Marshal(start)
			r := httptest.NewRequest("POST", "/api/assistant-runs", strings.NewReader(string(raw))).WithContext(requestCtx)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			view := assistantProtocolView(t, w, 201)
			e := <-entered
			switch mode {
			case "disconnect":
				cancel()
				select {
				case <-e.ctx.Done():
					t.Fatal("reader disconnect canceled saved run")
				case <-time.After(20 * time.Millisecond):
				}
				action := assistantRunAction{ID: "stop-once", Kind: "cancel", Revision: view.Revision}
				result := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+view.ID+"/actions", action), 200)
				if result.State != "stopped" {
					t.Fatal(result.State)
				}
				assistantProtocolWait(t, s, token, view.ID, "stopped")
				duplicate := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+view.ID+"/actions", action), 200)
				if duplicate.Revision != result.Revision {
					t.Fatal("cancel repeated delivery")
				}
			case "session":
				s.sessions.revoke(token)
			case "policy":
				s.signIn.mu.Lock()
				s.signIn.cancelEpoch()
				s.signIn.epoch, s.signIn.cancelEpoch = context.WithCancel(context.Background())
				s.signIn.generation++
				s.signIn.mu.Unlock()
			case "shutdown":
				assistantRunMust(t, s.Close())
			}
			cancel()
			select {
			case <-e.done:
			case <-time.After(time.Second):
				t.Fatal("revocation/shutdown did not drain")
			}
			run, err := e.writer.lease.store.Read(context.Background(), e.record.Binding, e.record.ID)
			if mode != "shutdown" {
				assistantRunMust(t, err)
				if run.State != "stopped" {
					t.Fatal("canceled run retained dispatch", run.State)
				}
			}
		})
	}
}
func assistantProtocolAsked(cp contract.Checkpoint) contract.Checkpoint {
	cp.Status = "stopped"
	cp.Approval = &contract.CheckpointApproval{CallID: "call-one", Name: "write_file", Tool: "write_file", Source: contract.ToolOfferSourceRecord{Kind: "custom", ID: "fixture", Trust: "person"}, Args: map[string]json.RawMessage{"path": json.RawMessage(`"reviewed.txt"`), "content": json.RawMessage(`"exact"`)}, State: "asked"}
	return cp
}
func TestAssistantRunProtocolApprovalRestoredAndDeliveredOnce(t *testing.T) {
	deliveries := atomic.Int32{}
	s, token, start, executor := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		cp := *e.record.Checkpoint
		if e.decision == nil {
			return e.Checkpoint(ctx, assistantProtocolAsked(cp))
		}
		if !e.decision.Approved || e.decision.CallID != "call-one" {
			return errors.New("wrong decision")
		}
		// The approved checkpoint precedes the host capability dispatch.
		cp.Status = "running"
		cp.Approval.State = "approved"
		now := time.Now().UTC().Format(time.RFC3339Nano)
		cp.Approval.DecidedAt = &now
		if err := e.Checkpoint(ctx, cp); err != nil {
			return err
		}
		op := assistantRunOperation{ID: "write-one", CallID: "call-one", Kind: "host", Name: "write_file", Intent: []byte(`{"path":"reviewed.txt","content":"exact"}`)}
		if err := e.Intent(ctx, op); err != nil {
			return err
		}
		if err := e.Dispatch(ctx, op.ID); err != nil {
			return err
		}
		deliveries.Add(1)
		if err := e.Result(ctx, op.ID, "completed", []byte(`{"saved":true}`)); err != nil {
			return err
		}
		cp.Status = "complete"
		return e.Checkpoint(ctx, cp)
	})
	view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	paused := assistantProtocolWait(t, s, token, view.ID, "awaiting-input")
	if paused.AdmissionID != start.AdmissionID || paused.ApprovalReference == nil || paused.ApprovalReference.CallID != paused.Checkpoint.Approval.CallID || paused.ApprovalReference.ArgsDigest != assistantRunArgsDigest(paused.Checkpoint.Approval.Args) {
		t.Fatal("approval/admission reference lost")
	}
	assistantRunClientJSON(t, "consumer-approval.json", paused)
	// service releases its executor lease at the pause. Reacquisition reads the
	// persisted canonical approval; no model or tool executes on recovery.
	lease, err := s.assistantRuns().Acquire(context.Background())
	assistantRunMust(t, err)
	assistantRunMust(t, lease.Close())
	raw, _ := json.Marshal(paused.Checkpoint.Approval.Args)
	approved := true
	action := assistantRunAction{ID: "approval-one", Kind: "approval", Revision: paused.Revision, CallID: "call-one", ArgsDigest: digestOf(raw), Approved: &approved}
	for _, mutate := range []func(*assistantRunAction){func(a *assistantRunAction) { a.CallID = "stale" }, func(a *assistantRunAction) { a.ArgsDigest = digestOf([]byte("changed arguments")) }, func(a *assistantRunAction) { a.Revision-- }} {
		bad := action
		mutate(&bad)
		if w := policyCall(s, token, "POST", "/api/assistant-runs/"+view.ID+"/actions", bad); w.Code != 409 {
			t.Fatal("stale decision accepted", w.Code, w.Body.String())
		}
	}
	assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+view.ID+"/actions", action), 200)
	complete := assistantProtocolWait(t, s, token, view.ID, "complete")
	assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+view.ID+"/actions", action), 200)
	if deliveries.Load() != 1 || executor.calls.Load() != 2 || len(complete.Operations) != 1 || complete.Operations[0].State != "completed" {
		t.Fatal("approval delivery repeated/lost", deliveries.Load(), executor.calls.Load(), complete)
	}
	changed := false
	action.Approved = &changed
	if w := policyCall(s, token, "POST", "/api/assistant-runs/"+view.ID+"/actions", action); w.Code != 409 {
		t.Fatal("conflicting decision retry accepted", w.Code)
	}
}
func TestAssistantRunProtocolSSESnapshotAndBoundedSubscribers(t *testing.T) {
	s, token, start, _ := assistantProtocolFixture(t, assistantProtocolComplete)
	view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	view = assistantProtocolWait(t, s, token, view.ID, "complete")
	for _, cursor := range []string{"", "999999:0", "1:99999", "invalid"} {
		r := httptest.NewRequest("GET", "/api/assistant-runs/"+view.ID+"/events", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Last-Event-ID", cursor)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "event: snapshot\n") || !strings.Contains(w.Body.String(), "id: "+view.Cursor) {
			t.Fatal("missing recovery snapshot", cursor, w.Code, w.Body.String())
		}
	}
	h := s.assistantRunService()
	var release []func()
	for range assistantRunSubscriberLimit {
		_, close, err := h.subscribe(view.ID)
		assistantRunMust(t, err)
		release = append(release, close)
	}
	defer func() {
		for _, close := range release {
			close()
		}
	}()
	if _, _, err := h.subscribe(view.ID); err == nil {
		t.Fatal("unbounded subscribers")
	}
	// Slow subscribers get one coalesced signal; semantic events stay durable.
	for range 1000 {
		h.notify(view.ID)
	}
	h.mu.Lock()
	for _, sub := range h.subscribers {
		if len(sub.wake) != 1 {
			t.Fatal("unbounded subscriber queue")
		}
	}
	h.mu.Unlock()
	if w := policyCall(s, token, "GET", "/api/assistant-runs/"+view.ID+"/events?token=secret", nil); w.Code != 400 {
		t.Fatal("URL credential/cursor accepted")
	}
}
func TestAssistantRunProtocolPersistenceFailureBeforeDispatch(t *testing.T) {
	reached := make(chan error, 1)
	s, token, start, executor := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		op := assistantRunOp("one")
		err := e.Intent(ctx, op)
		if err == nil {
			err = e.Dispatch(ctx, op.ID)
		}
		reached <- err
		return err
	})
	// Fail the initial checkpoint write by turning the already-created private
	// ledger file into a directory after admission's verified write.
	reads := 0
	store := s.assistantRuns()
	testHookPrivateRead = func(name string) error {
		if name == store.name {
			reads++
			if reads == 4 {
				return fmt.Errorf("fixture checkpoint readback failure")
			}
		}
		return nil
	}
	w := policyCall(s, token, "POST", "/api/assistant-runs", start)
	testHookPrivateRead = nil
	if w.Code == 201 {
		select {
		case err := <-reached:
			if err == nil {
				t.Fatal("failure allowed dispatch")
			}
		case <-time.After(time.Second):
			t.Fatal("runner not settled")
		}
	} else if executor.calls.Load() != 0 {
		t.Fatal("failed checkpoint dispatched executor")
	}
	// Retained admission remains recoverable without starting it again.
	if w.Code == 201 {
		return
	}
	if w.Code != 409 {
		t.Fatal("unexpected failure status", w.Code, w.Body.String())
	}
}
func TestAssistantRunProtocolOwnerChangeStopsDispatch(t *testing.T) {
	proceed := make(chan struct{})
	defer func() {
		select {
		case <-proceed:
		default:
			close(proceed)
		}
	}()
	entered := make(chan struct{})
	result := make(chan error, 1)
	s, token, start, _ := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		close(entered)
		<-proceed
		err := e.Intent(ctx, assistantRunOp("one"))
		result <- err
		return err
	})
	view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	// Admission returns before the runner's initial Authorize. Enter the
	// executor before revoking scope, so this exercises the next Intent fence
	// rather than skipping the executor and leaving its result unsignaled.
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not enter before owner change")
	}
	assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, assistantProfilePath), []byte(`{"profileVersion":1,"api":{"inherit":false,"models":[],"model":null}}`), 0600))
	close(proceed)
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("disabled model escaped dispatch admission")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not return")
	}
	assistantProtocolWait(t, s, token, view.ID, "stopped")
}
