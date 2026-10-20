package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

type chatProjectionFixture struct {
	s      *Server
	token  string
	store  assistantRunStore
	lease  *assistantRunLease
	source []byte
}

func newChatProjectionFixture(t *testing.T) *chatProjectionFixture {
	t.Helper()
	s, token, _, _ := assistantProtocolFixture(t, assistantProtocolComplete)
	raw, err := os.ReadFile("testdata/assistant-chat-projection-source.json")
	assistantRunMust(t, err)
	data, err := s.openChatData()
	assistantRunMust(t, err)
	defer data.root.Close()
	name, err := resolveConversationName(data.root, s.conversationIdentity())
	assistantRunMust(t, err)
	assistantRunMust(t, writePrivateData(data.root, name, raw))
	store := s.assistantRuns()
	lease, err := store.Acquire(t.Context())
	assistantRunMust(t, err)
	t.Cleanup(func() { lease.Close() })
	return &chatProjectionFixture{s: s, token: token, store: store, lease: lease, source: raw}
}
func (f *chatProjectionFixture) request() *http.Request {
	r := httptest.NewRequest("GET", "/api/assistant-runs", nil)
	r.Header.Set("Authorization", "Bearer "+f.token)
	return r
}
func (f *chatProjectionFixture) read(t *testing.T) []byte {
	t.Helper()
	data, err := f.s.openChatData()
	assistantRunMust(t, err)
	defer data.root.Close()
	name, err := resolveConversationName(data.root, f.s.conversationIdentity())
	assistantRunMust(t, err)
	raw, err := readPrivateData(data.root, name, maxConversationBytes)
	assistantRunMust(t, err)
	return raw
}
func (f *chatProjectionFixture) save(t *testing.T, raw []byte) {
	t.Helper()
	data, err := f.s.openChatData()
	assistantRunMust(t, err)
	defer data.root.Close()
	name, err := resolveConversationName(data.root, f.s.conversationIdentity())
	assistantRunMust(t, err)
	assistantRunMust(t, writePrivateData(data.root, name, raw))
}
func (f *chatProjectionFixture) admit(t *testing.T, owner, request, admission, state string, events ...contract.RunEvent) assistantRunRecord {
	t.Helper()
	_, rows, err := assistantChatRows(f.read(t))
	assistantRunMust(t, err)
	index, err := assistantChatSelected(rows, owner)
	assistantRunMust(t, err)
	r := f.request()
	identity, _, err := f.s.assistantRunIdentity(r)
	assistantRunMust(t, err)
	start := assistantRunStart{Purpose: "chat", OwnerID: owner, RequestID: request, AdmissionID: admission, Revision: digestOf(rows[index])}
	source, err := f.s.readAssistantRunSource(r, start, identity, f.store.project)
	assistantRunMust(t, err)
	executor := &assistantProtocolExecutor{}
	cp, err := executor.Prepare(t.Context(), source)
	assistantRunMust(t, err)
	source.Binding.Unit = cp.Unit
	source.Binding.ExecutionKey = cp.RequestKey
	run, _, err := f.lease.Admit(t.Context(), source.Binding, admission, digestOf([]byte(admission)), func() error { return nil })
	assistantRunMust(t, err)
	writer, err := f.lease.Writer(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	assistantRunMust(t, writer.Checkpoint(t.Context(), cp))
	// A real completed operation is retained as exact bytes; projection never
	// reconstructs or executes it from the presentation-only work summary.
	op := assistantRunOp("typed-original")
	op.Intent = []byte(` {"missing":null,"flag":false,"zero":0,"literal":"50"} `)
	assistantRunMust(t, writer.Intent(t.Context(), op))
	assistantRunMust(t, writer.Dispatch(t.Context(), op.ID))
	assistantRunMust(t, writer.Result(t.Context(), op.ID, "completed", []byte(` {"result":false,"missing":null,"number":1.0,"literal":"50"} `)))
	for i, event := range events {
		assistantRunMust(t, writer.Event(t.Context(), "event-"+string(rune('a'+i)), event))
	}
	cp.Used = 2
	switch state {
	case "complete":
		cp.Status = "complete"
	case "paused-budget":
		cp.Status = "paused"
	default:
		cp.Status = "stopped"
	}
	if state == "awaiting-input" {
		cp = assistantProtocolAsked(cp)
		for _, event := range events {
			if q := event.UserQuestion; q != nil {
				raw, err := json.Marshal(q.Question)
				assistantRunMust(t, err)
				var args map[string]json.RawMessage
				assistantRunMust(t, json.Unmarshal(raw, &args))
				cp.Approval.CallID = q.ID
				cp.Approval.Name, cp.Approval.Tool = "ask_user", "ask_user"
				cp.Approval.Args = args
			}
		}
	}
	assistantRunMust(t, writer.Checkpoint(t.Context(), cp))
	assistantRunMust(t, writer.State(t.Context(), state))
	run, err = f.store.Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	return run
}
func projectionMessage(text string) contract.RunEvent {
	return contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: text}}
}
func projectionToolEvents() []contract.RunEvent {
	first, second := "same:1", "same:2"
	return []contract.RunEvent{
		{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &first, Name: "read_fixture", Args: json.RawMessage(`{"zero":0,"flag":false,"missing":null}`)}},
		{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &second, Name: "read_fixture", Args: json.RawMessage(`{}`)}},
		{ToolResult: &contract.ToolResult{Type: "tool_result", CallID: &second, Name: "read_fixture", Text: "second", IsError: true}},
		{ToolResult: &contract.ToolResult{Type: "tool_result", CallID: &first, Name: "read_fixture", Text: "first"}},
		{Critique: &contract.RunEventCritique{Type: "critique", Text: "Reviewed the retained cases.", Checks: []contract.RunEventCritiqueChecksItem{}, Incomplete: []string{"case-one"}, Figures: &contract.ProvenanceCriticFigures{Model: "critic-fixture", Unit: "model-requests", Requests: 1}}},
		{Error: &contract.RunEventError{Type: "error", Message: "The model timed out.", Failure: &contract.Failure{Version: 1, Code: "timeout", Source: "model", Outcome: "unknown"}}},
	}
}
func TestAssistantChatProjectionPrepareWriteReadbackAck(t *testing.T) {
	f := newChatProjectionFixture(t)
	run := f.admit(t, "chat-one", "message-one", "first", "complete", projectionMessage("Exact completed answer."))
	before := f.read(t)
	_, oldRows, _ := assistantChatRows(before)
	assistantRunMust(t, f.s.prepareAssistantChatProjection(f.request(), run.ID))
	if !bytes.Equal(before, f.read(t)) {
		t.Fatal("prepare changed source chat")
	}
	prepared, err := f.store.Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	if prepared.ChatProjection == nil || prepared.ChatProjection.Acknowledged {
		t.Fatal("prepare not persisted")
	}
	// Reacquired writer/session object: a crash after prepare requires no execution.
	f.lease.Close()
	f.lease, err = f.store.Acquire(t.Context())
	assistantRunMust(t, err)
	t.Cleanup(func() { f.lease.Close() })
	assistantRunMust(t, f.s.writeAssistantChatProjection(f.request(), run.ID))
	written := f.read(t)
	_, newRows, _ := assistantChatRows(written)
	if !bytes.Equal(oldRows[1], newRows[1]) {
		t.Fatal("unrelated raw chat was compacted")
	}
	if !bytes.Contains(newRows[0], []byte(`"previousNumeric":1.0`)) || !bytes.Contains(newRows[0], []byte(`{ \"Existing\": false, \"Missing\":null, \"zero\":0, \"literal\":\"50\" }`)) {
		t.Fatal("retained original strings or typed numbers changed")
	}
	// Lost acknowledgement: retry exact saved bytes, then acknowledge once.
	assistantRunMust(t, f.s.reconcileAssistantChatProjection(f.request(), run.ID))
	assistantRunMust(t, f.s.reconcileAssistantChatProjection(f.request(), run.ID))
	if !bytes.Equal(written, f.read(t)) {
		t.Fatal("duplicate completion changed output")
	}
	saved, err := f.store.Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	if len(saved.Operations) != 1 || !bytes.Equal(saved.Operations[0].Intent, run.Operations[0].Intent) || !bytes.Equal(saved.Operations[0].Result, run.Operations[0].Result) {
		t.Fatal("exact typed operation originals changed during recovery")
	}
	if saved.Checkpoint.Used != 2 {
		t.Fatal("projection reset charged usage")
	}
	if !saved.ChatProjection.Acknowledged || !bytes.Equal(saved.ChatProjection.Output, newRows[0]) {
		t.Fatal("ack missing exact prepared output")
	}
	rows, err := f.s.discoverAssistantChatRuns(f.request(), "chat-one", "message-one")
	assistantRunMust(t, err)
	if len(rows) != 1 || rows[0].ID != run.ID || rows[0].ChatProjection == nil || !rows[0].ChatProjection.Acknowledged {
		t.Fatal("projected request not discoverable")
	}
	exportChatProjection(t, "completed", written)
}
func TestAssistantChatProjectionWorkFailureAndCreate(t *testing.T) {
	t.Run("failure-before-prose", func(t *testing.T) {
		f := newChatProjectionFixture(t)
		run := f.admit(t, "chat-one", "message-one", "failure", "failed", projectionToolEvents()...)
		assistantRunMust(t, f.s.reconcileAssistantChatProjection(f.request(), run.ID))
		raw := f.read(t)
		if !bytes.Contains(raw, []byte(`"afterTurnId":"message-one"`)) || !bytes.Contains(raw, []byte(`"failure":{"version":1,"code":"timeout"`)) {
			t.Fatal("failure association lost", string(raw))
		}
		if !bytes.Contains(raw, []byte(`"id":"same:1","name":"read_fixture","status":"complete"`)) || !bytes.Contains(raw, []byte(`"id":"same:2","name":"read_fixture","status":"failed"`)) {
			t.Fatal("parallel tool identity lost")
		}
		if !bytes.Contains(raw, []byte(`"checkRun":"complete"`)) {
			t.Fatal("artifact check overwrote response failure")
		}
		exportChatProjection(t, "failure-before-prose", raw)
	})
	t.Run("normal-create", func(t *testing.T) {
		f := newChatProjectionFixture(t)
		event := contract.RunEvent{Proposal: &contract.RunEventProposal{Type: "proposal", Document: map[string]json.RawMessage{"Enabled": []byte(`false`), "Fact": []byte(`null`), "Count": []byte(`0`), "Literal": []byte(`"50"`)}, Unknowns: []string{}}}
		run := f.admit(t, "chat-one", "message-one", "create", "complete", event, projectionMessage("Review the proposal."))
		assistantRunMust(t, f.s.reconcileAssistantChatProjection(f.request(), run.ID))
		raw := f.read(t)
		if !bytes.Contains(raw, []byte(`{\"Count\":0,\"Enabled\":false,\"Fact\":null,\"Literal\":\"50\"}`)) {
			t.Fatal("new sorted-map proposal not retained")
		}
		exportChatProjection(t, "create", raw)
	})
	t.Run("saved-update-limitation", func(t *testing.T) {
		f := newChatProjectionFixture(t)
		raw := bytes.Replace(f.read(t), []byte(`"id":"chat-one"`), []byte(`"id":"chat-one","pack":{"id":"saved","path":"saved.json","digest":"prior"}`), 1)
		f.save(t, raw)
		run := f.admit(t, "chat-one", "message-one", "update", "complete", contract.RunEvent{Proposal: &contract.RunEventProposal{Type: "proposal", Document: map[string]json.RawMessage{"Enabled": []byte(`false`)}, Unknowns: []string{}}})
		err := f.s.reconcileAssistantChatProjection(f.request(), run.ID)
		if !errors.Is(err, errAssistantChatProjectionUnsupported) || !bytes.Equal(raw, f.read(t)) {
			t.Fatal("unsupported proposal was silently projected", err)
		}
		saved, e := f.store.Read(t.Context(), run.Binding, run.ID)
		assistantRunMust(t, e)
		if saved.ProjectionIssue != "proposal-target" || len(saved.Events) != 1 {
			t.Fatal("unsupported proposal not retained")
		}
	})
}
func TestAssistantChatProjectionPausesStayPinned(t *testing.T) {
	for _, state := range []string{"awaiting-input", "paused-budget", "awaiting-client"} {
		t.Run(state, func(t *testing.T) {
			f := newChatProjectionFixture(t)
			before := f.read(t)
			events := []contract.RunEvent{projectionMessage("Retained while paused")}
			if state == "awaiting-input" {
				events = append(events, contract.RunEvent{UserQuestion: &contract.RunEventUserQuestion{Type: "user_question", ID: "question-one", Question: contract.Question{Title: "Choose", Question: "Which input should be used?", Selection: "single", Options: []contract.QuestionOption{{ID: "a", Label: "First", Description: "Use the first input."}, {ID: "b", Label: "Second", Description: "Use the second input."}}}}})
			}
			run := f.admit(t, "chat-one", "message-one", state, state, events...)
			if err := f.s.reconcileAssistantChatProjection(f.request(), run.ID); err == nil {
				t.Fatal("pending continuation changed chat")
			}
			if !bytes.Equal(before, f.read(t)) {
				t.Fatal("pause source changed")
			}
			rows, err := f.s.discoverAssistantChatRuns(f.request(), "chat-one", "message-one")
			assistantRunMust(t, err)
			if len(rows) != 1 || rows[0].State != state || len(rows[0].Events) != len(events) {
				t.Fatal("pause not discoverable")
			}
			if state == "awaiting-input" && (rows[0].Checkpoint.Approval == nil || rows[0].Checkpoint.Approval.CallID != "question-one" || rows[0].Checkpoint.Approval.Name != "ask_user") {
				t.Fatal("pending canonical question lost its bound checkpoint")
			}
			exportChatProjection(t, state, before)
		})
	}
}
func TestAssistantChatProjectionLostReadbackAndCancellation(t *testing.T) {
	f := newChatProjectionFixture(t)
	run := f.admit(t, "chat-one", "message-one", "lost-readback", "complete", projectionMessage("Already saved; never rerun"))
	assistantRunMust(t, f.s.prepareAssistantChatProjection(f.request(), run.ID))
	reads := 0
	t.Cleanup(func() { testHookPrivateRead = nil })
	testHookPrivateRead = func(name string) error {
		if strings.HasPrefix(name, "conversations-") {
			reads++
			if reads == 2 {
				return errors.New("fixture lost write acknowledgement")
			}
		}
		return nil
	}
	err := f.s.writeAssistantChatProjection(f.request(), run.ID)
	testHookPrivateRead = nil
	if err == nil {
		t.Fatal("lost readback reported success")
	}
	saved, e := f.store.Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, e)
	if saved.ChatProjection.Acknowledged {
		t.Fatal("readback failure acknowledged")
	}
	_, rows, e := assistantChatRows(f.read(t))
	assistantRunMust(t, e)
	if !bytes.Equal(rows[0], saved.ChatProjection.Output) {
		t.Fatal("fixture did not reach post-write crash point")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = f.s.reconcileAssistantChatProjection(f.request().Clone(ctx), run.ID); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled recovery proceeded", err)
	}
	assistantRunMust(t, f.s.reconcileAssistantChatProjection(f.request(), run.ID))
}
func TestAssistantChatProjectionNamespaceChangeRefuses(t *testing.T) {
	f := newChatProjectionFixture(t)
	run := f.admit(t, "chat-one", "message-one", "namespace", "complete", projectionMessage("Original namespace"))
	assistantRunMust(t, f.s.prepareAssistantChatProjection(f.request(), run.ID))
	original := f.s.projectDir
	f.s.projectDir = filepath.Join(t.TempDir(), "another-project")
	if err := f.s.reconcileAssistantChatProjection(f.request(), run.ID); err == nil {
		t.Fatal("projection crossed namespace")
	}
	f.s.projectDir = original
	assistantRunMust(t, f.s.reconcileAssistantChatProjection(f.request(), run.ID))
}

func TestAssistantChatProjectionCASAndAuthorization(t *testing.T) {
	for _, kind := range []string{"changed-owner", "deleted-owner", "revoked", "foreign-session", "historical", "changed-prepared-output", "changed-events", "wrong-project"} {
		t.Run(kind, func(t *testing.T) {
			f := newChatProjectionFixture(t)
			run := f.admit(t, "chat-one", "message-one", kind, "complete", projectionMessage("Prepared answer"))
			assistantRunMust(t, f.s.prepareAssistantChatProjection(f.request(), run.ID))
			before := f.read(t)
			request := f.request()
			switch kind {
			case "changed-owner":
				f.save(t, bytes.Replace(before, []byte(`"title":"Projection fixture"`), []byte(`"title":"Changed owner"`), 1))
			case "deleted-owner":
				fields, rows, _ := assistantChatRows(before)
				fields["chats"] = assistantChatEncodeArray(rows[1:])
				f.save(t, assistantChatEncodeObject(fields))
			case "revoked":
				f.s.sessions.revoke(f.token)
			case "foreign-session":
				request.Header.Set("Authorization", "Bearer "+policyTestSession(t, f.s))
			case "historical":
				_, err := f.store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) {
					l.Runs[0].Historical = true
					l.Runs[0].Revision++
					return true, nil
				})
				assistantRunMust(t, err)
			case "changed-prepared-output":
				_, err := f.store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) {
					l.Runs[0].ChatProjection.Output = []byte(`{}`)
					return true, nil
				})
				if err == nil {
					t.Fatal("contradictory prepared output admitted")
				}
			case "changed-events":
				_, err := f.store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) {
					l.Runs[0].Events[0].Event.Message.Text = "Contradictory completion"
					return true, nil
				})
				if err == nil {
					t.Fatal("prepared semantic output changed")
				}
				return
			case "wrong-project":
				run.Binding.Project = strings.Repeat("a", 64)
				if _, err := f.store.Read(t.Context(), run.Binding, run.ID); err == nil {
					t.Fatal("foreign project binding read")
				}
				return
			}
			current := f.read(t)
			if kind != "changed-prepared-output" {
				if err := f.s.writeAssistantChatProjection(request, run.ID); err == nil {
					t.Fatal("stale or unauthorized projection accepted")
				}
			}
			if !bytes.Equal(current, f.read(t)) {
				t.Fatal("refusal overwrote saved chat")
			}
		})
	}
}
func TestAssistantChatProjectionConcurrentChatsAndDiscovery(t *testing.T) {
	f := newChatProjectionFixture(t)
	a := f.admit(t, "chat-one", "message-one", "one", "complete", projectionMessage("First chat"))
	b := f.admit(t, "chat-two", "message-two", "two", "complete", projectionMessage("Second chat"))
	assistantRunMust(t, f.s.prepareAssistantChatProjection(f.request(), a.ID))
	assistantRunMust(t, f.s.prepareAssistantChatProjection(f.request(), b.ID))
	assistantRunMust(t, f.s.reconcileAssistantChatProjection(f.request(), b.ID))
	_, before, _ := assistantChatRows(f.read(t))
	assistantRunMust(t, f.s.reconcileAssistantChatProjection(f.request(), a.ID))
	_, after, _ := assistantChatRows(f.read(t))
	if !bytes.Equal(before[1], after[1]) {
		t.Fatal("another run's acknowledged chat changed")
	}
	retry := f.admit(t, "chat-one", "message-one", "explicit-retry", "stopped")
	rows, err := f.s.discoverAssistantChatRuns(f.request(), "chat-one", "message-one")
	assistantRunMust(t, err)
	if len(rows) != 2 || rows[0].ID > rows[1].ID {
		t.Fatal("retry discovery ordering")
	}
	newSession := f.request()
	newSession.Header.Set("Authorization", "Bearer "+policyTestSession(t, f.s))
	rows, err = f.s.discoverAssistantChatRuns(newSession, "chat-one", "message-one")
	assistantRunMust(t, err)
	if len(rows) != 2 || !rows[0].Historical || !rows[1].Historical {
		t.Fatal("new session gained live continuation")
	}
	if err = f.s.reconcileAssistantChatProjection(newSession, retry.ID); err == nil {
		t.Fatal("historical discovery authorized write")
	}
	other, err := f.s.sessions.createExpiring("another principal", nil, "", "")
	assistantRunMust(t, err)
	foreign := f.request()
	foreign.Header.Set("Authorization", "Bearer "+other)
	rows, err = f.s.discoverAssistantChatRuns(foreign, "chat-one", "message-one")
	assistantRunMust(t, err)
	if len(rows) != 0 {
		t.Fatal("another principal's runs leaked")
	}
	w := policyCall(f.s, f.token, "GET", "/api/assistant-runs/owners/chat-one/requests/message-one", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := policyCall(f.s, "", "GET", "/api/assistant-runs/owners/chat-one/requests/message-one", nil); w.Code == 200 {
		t.Fatal("unauthenticated discovery")
	}
}
func exportChatProjection(t *testing.T, name string, raw []byte) {
	t.Helper()
	if dir := os.Getenv("JPS_TEST_CHAT_PROJECTION_OUTPUTS"); dir != "" {
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, name+".json"), raw, 0600))
	}
}

func TestAssistantChatProjectionServiceBoundary(t *testing.T) {
	s, token, start, executor := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		if err := e.Event(ctx, "final-prose", projectionMessage("Service-owned terminal response")); err != nil {
			return err
		}
		cp := *e.record.Checkpoint
		cp.Status = "complete"
		if err := e.Checkpoint(ctx, cp); err != nil {
			return err
		}
		if err := e.writer.State(ctx, "complete"); err != nil {
			return err
		}
		return e.ProjectChat(ctx)
	})
	view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	final := assistantProtocolWait(t, s, token, view.ID, "complete")
	if final.ChatProjection == nil || !final.ChatProjection.Acknowledged || executor.calls.Load() != 1 {
		t.Fatal("terminal service projection missing", final)
	}
	r := httptest.NewRequest("GET", "/api/assistant-runs", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	assistantRunMust(t, s.reconcileAssistantChatProjection(r, view.ID))
	if executor.calls.Load() != 1 {
		t.Fatal("recovery replayed executor")
	}
}

func TestAssistantChatProjectionPrematureAckAndMalformedAssociations(t *testing.T) {
	f := newChatProjectionFixture(t)
	run := f.admit(t, "chat-one", "message-one", "ack", "complete", projectionMessage("Prepared only"))
	assistantRunMust(t, f.s.prepareAssistantChatProjection(f.request(), run.ID))
	if err := f.s.acknowledgeAssistantChatProjection(f.request(), run.ID); err == nil {
		t.Fatal("unwritten output acknowledged")
	}
	_, rows, err := assistantChatRows(f.read(t))
	assistantRunMust(t, err)
	first, second := "same:1", "same:2"
	for _, events := range [][]contract.RunEvent{
		{{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &first, Name: "read"}}, {ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &first, Name: "read"}}},
		{{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &first, Name: "read"}}, {ToolResult: &contract.ToolResult{Type: "tool_result", CallID: &second, Name: "read"}}},
		{{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &first, Name: "read"}}, {ToolResult: &contract.ToolResult{Type: "tool_result", CallID: &first, Name: "other"}}},
	} {
		bad := run
		bad.Events = nil
		for i, event := range events {
			bad.Events = append(bad.Events, assistantRunEvent{ID: string(rune('a' + i)), Event: event})
		}
		if _, err := compileAssistantChatProjection(rows[0], bad, "2026-10-20T00:00:00Z"); err == nil {
			t.Fatal("ambiguous tool association accepted")
		}
	}
	for _, raw := range [][]byte{
		bytes.Replace(rows[0], []byte(`"id":"message-one"`), []byte(`"id":"bad_id"`), 1),
		bytes.Replace(rows[0], []byte(`"title":"Projection fixture"`), []byte(`"title":"one","title":"two"`), 1),
	} {
		if _, err := compileAssistantChatProjection(raw, run, "2026-10-20T00:00:00Z"); err == nil {
			t.Fatal("invalid chat identity accepted")
		}
	}
}

func TestAssistantChatProjectionOwnerGuards(t *testing.T) {
	f := newChatProjectionFixture(t)
	run := f.admit(t, "chat-one", "message-one", "owner-guards", "complete", projectionMessage("Bound"))
	for _, kind := range []string{"session", "policy", "principal", "historical", "project", "kind", "pending", "request"} {
		t.Run(kind, func(t *testing.T) {
			bad := run
			switch kind {
			case "session":
				bad.Binding.Session = strings.Repeat("a", 64)
			case "policy":
				bad.Binding.Policy = strings.Repeat("b", 64)
			case "principal":
				bad.Binding.Principal = strings.Repeat("c", 64)
			case "historical":
				bad.Historical = true
			case "project":
				bad.Binding.Project = strings.Repeat("d", 64)
			case "kind":
				bad.Binding.Owner.Kind = "pack"
			case "pending":
				bad.State = "awaiting-input"
			case "request":
				bad.Binding.RequestDigest = strings.Repeat("e", 64)
			}
			_, err := f.store.transaction(t.Context(), func(*assistantRunLedger) (bool, error) {
				root, _, _, _, err := f.s.assistantChatProjectionOwner(f.request(), f.store, &bad)
				if root != nil {
					root.Close()
				}
				if err == nil {
					t.Fatal("current owner guard accepted", kind)
				}
				return false, nil
			})
			assistantRunMust(t, err)
		})
	}
}
func TestAssistantChatProjectionDiscoveryBindsExactRequest(t *testing.T) {
	f := newChatProjectionFixture(t)
	f.admit(t, "chat-one", "message-one", "request-scope", "complete", projectionMessage("Retained"))
	f.save(t, bytes.Replace(f.read(t), []byte(`"text":"Keep exact typed originals"`), []byte(`"text":"Changed request under same ID"`), 1))
	rows, err := f.s.discoverAssistantChatRuns(f.request(), "chat-one", "message-one")
	assistantRunMust(t, err)
	if len(rows) != 0 {
		t.Fatal("changed request inherited retained run")
	}
}

func TestAssistantChatProjectionRejectsUnreadableDomainPayloads(t *testing.T) {
	f := newChatProjectionFixture(t)
	run := f.admit(t, "chat-one", "message-one", "domain-payloads", "complete", projectionMessage("Retained"))
	_, rows, err := assistantChatRows(f.read(t))
	assistantRunMust(t, err)
	for _, event := range []contract.RunEvent{
		{Error: &contract.RunEventError{Type: "error", Message: "Invalid typed failure", Failure: &contract.Failure{Version: 1, Code: "invented", Source: "model", Outcome: "unknown"}}},
		{Critique: &contract.RunEventCritique{Type: "critique", Text: "Oversized incomplete cases", Incomplete: make([]string, 65)}},
		{Critique: &contract.RunEventCritique{Type: "critique", Text: "Invalid figures", Figures: &contract.ProvenanceCriticFigures{Model: "fixture", Unit: "invented", Requests: 1}}},
	} {
		bad := run
		bad.Events = []assistantRunEvent{{ID: "bad", Event: event}}
		if _, err := compileAssistantChatProjection(rows[0], bad, "2026-10-20T00:00:00Z"); err == nil {
			t.Fatal("unreadable domain payload projected")
		}
	}
}

func TestAssistantChatProjectionRecoveryNormalizesCheckpointDisplay(t *testing.T) {
	f := newChatProjectionFixture(t)
	run := f.admit(t, "chat-one", "message-one", "checkpoint-recovery", "failed", projectionMessage("Interrupted after work"))
	_, err := f.store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) {
		l.Runs[0].Checkpoint.Operations = append(l.Runs[0].Checkpoint.Operations, contract.CheckpointOperation{ID: "3f71c7b0-7df4-4eb6-96f3-f3a1390dc3a0", Name: "read_exact", Args: json.RawMessage(`{"Fact":false,"missing":null}`), Status: "running"})
		l.Runs[0].Revision++
		return true, nil
	})
	assistantRunMust(t, err)
	assistantRunMust(t, f.s.prepareAssistantChatProjection(f.request(), run.ID))
	prepared, err := f.store.Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	if prepared.Checkpoint.Operations[0].Status != "running" {
		t.Fatal("projection mutated original checkpoint")
	}
	if !bytes.Contains(prepared.ChatProjection.Output, []byte(`"status":"interrupted"`)) {
		t.Fatal("terminal display presents unfinished operation as running")
	}
	fields, rows, err := assistantChatRows(f.read(t))
	assistantRunMust(t, err)
	rows[0] = prepared.ChatProjection.Output
	fields["chats"] = assistantChatEncodeArray(rows)
	if err := validateConversations(assistantChatEncodeObject(fields)); err != nil {
		t.Fatal("prepared conversation validation", err)
	}
	f.lease.Close()
	f.lease, err = f.store.Acquire(t.Context())
	assistantRunMust(t, err)
	t.Cleanup(func() { f.lease.Close() })
	latest, err := f.store.Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	if latest.Checkpoint.Operations[0].Status != "interrupted" || !bytes.Equal(latest.ChatProjection.Output, prepared.ChatProjection.Output) {
		t.Fatal("recovery rewrote prepared output")
	}
	if err := f.s.prepareAssistantChatProjection(f.request(), run.ID); err != nil {
		t.Fatal("recovery prepare", err)
	}
	if err := f.s.writeAssistantChatProjection(f.request(), run.ID); err != nil {
		t.Fatal("recovery write", err)
	}
	if err := f.s.acknowledgeAssistantChatProjection(f.request(), run.ID); err != nil {
		t.Fatal("recovery ack", err)
	}
	ledger, err := f.store.transaction(t.Context(), func(*assistantRunLedger) (bool, error) { return false, nil })
	assistantRunMust(t, err)
	raw, err := json.Marshal(ledger)
	assistantRunMust(t, err)
	historical, err := historicalAssistantRunStorage(f.store.name, raw)
	assistantRunMust(t, err)
	decoded, err := decodeAssistantRunStorage(f.store.name, historical)
	assistantRunMust(t, err)
	if !decoded.Runs[0].Historical || !bytes.Equal(decoded.Runs[0].ChatProjection.Output, prepared.ChatProjection.Output) {
		t.Fatal("historical import lost exact projection")
	}
}

func TestAssistantChatProjectionRetainsUnrepresentableCheckpointOriginals(t *testing.T) {
	f := newChatProjectionFixture(t)
	run := f.admit(t, "chat-one", "message-one", "invalid-plan-original", "failed", projectionMessage("Retained"))
	// A concurrent unrelated legacy entry must not make the resulting whole
	// conversation invalid. The selected admitted source remains unchanged.
	f.save(t, bytes.Replace(f.read(t), []byte(`"title":"Other pinned chat"`), []byte(`"title":"Other pinned chat","plans":[{}]`), 1))
	before := f.read(t)
	if err := f.s.prepareAssistantChatProjection(f.request(), run.ID); !errors.Is(err, errAssistantChatProjectionUnsupported) {
		t.Fatal("saved-chat validation bypassed", err)
	}
	saved, err := f.store.Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	if saved.ProjectionIssue != "saved-chat-validation" || saved.ChatProjection != nil || !bytes.Equal(before, f.read(t)) {
		t.Fatal("unrepresentable source changed")
	}
}
