package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func assistantProtocolPause(state string) func(context.Context, *assistantRunExecution) error {
	return func(ctx context.Context, e *assistantRunExecution) error {
		// Retain one exact completed result and one unknown operation as evidence.
		op := assistantRunOp("completed")
		if err := e.Intent(ctx, op); err != nil {
			return err
		}
		if err := e.Dispatch(ctx, op.ID); err != nil {
			return err
		}
		if err := e.Result(ctx, op.ID, "completed", []byte("{ \"exact\" : null }\n")); err != nil {
			return err
		}
		op = assistantRunOp("unknown")
		if err := e.Intent(ctx, op); err != nil {
			return err
		}
		if err := e.Dispatch(ctx, op.ID); err != nil {
			return err
		}
		// The host observed an unknown outcome; no result bytes are invented.
		if err := e.writer.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
			for i := range run.Operations {
				if run.Operations[i].ID == op.ID {
					run.Operations[i].State = "uncertain"
					return true, nil
				}
			}
			return false, errAssistantRunInvalid
		}); err != nil {
			return err
		}
		cp := *e.record.Checkpoint
		switch state {
		case "awaiting-input":
			cp = assistantProtocolAsked(cp)
		case "paused-budget":
			cp.Status = "paused"
		case "awaiting-client":
			cp.Status = "stopped"
		}
		if err := e.Checkpoint(ctx, cp); err != nil {
			return err
		}
		return e.writer.State(ctx, state)
	}
}

func TestAssistantRunProtocolAbandonPausedOwner(t *testing.T) {
	for _, state := range []string{"awaiting-input", "paused-budget", "awaiting-client"} {
		for _, replacement := range []string{"fresh-session", "session", "policy", "restart"} {
			t.Run(state+"/"+replacement, func(t *testing.T) {
				s, token, start, executor := assistantProtocolFixture(t, assistantProtocolPause(state))
				initial := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
				pause := assistantProtocolWait(t, s, token, initial.ID, state)
				original, err := s.assistantRunService().read(assistantProtocolRequest(token), initial.ID)
				assistantRunMust(t, err)
				switch replacement {
				case "session":
					s.sessions.revoke(token)
				case "policy":
					s.signIn.mu.Lock()
					s.signIn.cancelEpoch()
					s.signIn.epoch, s.signIn.cancelEpoch = context.WithCancel(context.Background())
					s.signIn.generation++
					s.signIn.mu.Unlock()
				case "restart":
					assistantRunMust(t, s.Close())
					s, err = New(s.cfg)
					assistantRunMust(t, err)
					t.Cleanup(func() { _ = s.Close() })
					s.assistantRunExecutor = executor
				}
				current := policyTestSession(t, s)
				path := "/api/assistant-runs/" + initial.ID
				read := assistantProtocolView(t, policyCall(s, current, "GET", path, nil), 200)
				if read.State != state || read.Revision != pause.Revision {
					t.Fatal("read/recovery silently changed pause", read)
				}
				// The recovery reader never inherits ordinary cancel or approval authority.
				for _, kind := range []string{"cancel", "approval"} {
					if w := policyCall(s, current, "POST", path+"/actions", assistantRunAction{ID: "old-decision", Kind: kind, Revision: pause.Revision}); w.Code != 404 {
						t.Fatal("old grant rebound", kind, w.Code)
					}
				}
				action := assistantRunAction{ID: "abandon-once", Kind: "abandon", Revision: pause.Revision}
				stale := action
				stale.Revision--
				if w := policyCall(s, current, "POST", path+"/actions", stale); w.Code != 409 {
					t.Fatal("stale abandonment", w.Code)
				}
				ended := assistantProtocolView(t, policyCall(s, current, "POST", path+"/actions", action), 200)
				again := assistantProtocolView(t, policyCall(s, current, "POST", path+"/actions", action), 200)
				if ended.State != "stopped" || ended.Revision != again.Revision || !reflect.DeepEqual(ended.Operations, pause.Operations) {
					t.Fatal("abandonment changed receipts or repeated", ended, again)
				}
				changed := action
				changed.Revision++
				if w := policyCall(s, current, "POST", path+"/actions", changed); w.Code != 409 {
					t.Fatal("changed duplicate action", w.Code)
				}
				record, err := s.assistantRunService().read(assistantProtocolRequest(current), initial.ID)
				assistantRunMust(t, err)
				identity, _, err := s.assistantRunIdentity(assistantProtocolRequest(current))
				assistantRunMust(t, err)
				if !reflect.DeepEqual(record.Binding, original.Binding) || len(record.Actions) != 1 || record.Actions[0].Digest != assistantRunActionDigest(action) || record.Actions[0].AuthorizingSession != identity.Session || record.Actions[0].AuthorizingPolicy != identity.Policy {
					t.Fatal("lost original binding or authorizing receipt", record.Actions)
				}
				if state == "awaiting-input" && !reflect.DeepEqual(record.Checkpoint.Approval, original.Checkpoint.Approval) {
					t.Fatal("old approval evidence changed")
				}
				if executor.calls.Load() != 1 {
					t.Fatal("abandonment executed work")
				}
				// Even a still-valid original session cannot execute the saved approval now.
				if replacement == "fresh-session" {
					yes := true
					old := assistantRunAction{ID: "approve-after-abandon", Kind: "approval", Revision: record.Revision, CallID: "call-one", Approved: &yes}
					if w := policyCall(s, token, "POST", path+"/actions", old); w.Code < 400 {
						t.Fatal("old approval revived")
					}
				}
				// Save a distinct user request and admit it separately under the new session.
				saved := policyWorkspaceRead(t, s, current, "/api/conversations")
				var doc conversationDocument
				assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
				var chat map[string]any
				assistantRunMust(t, json.Unmarshal(doc.Chats[0], &chat))
				chat["checkpoint"] = map[string]any{"state": map[string]any{"turns": []any{map[string]any{"id": "message-two", "role": "user", "kind": "message", "text": "Fresh explicit request", "at": "2026-10-20T00:00:01Z"}}}}
				w := policyWorkspaceCall(s, current, "PUT", "/api/conversations", saved.SHA256, map[string]any{"version": 1, "chats": []any{chat}})
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				saved = policyWorkspaceRead(t, s, current, "/api/conversations")
				assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
				start.AdmissionID = "fresh-admission"
				start.RequestID = "message-two"
				start.Revision = digestOf(doc.Chats[0])
				fresh := assistantProtocolView(t, policyCall(s, current, "POST", "/api/assistant-runs", start), 201)
				assistantProtocolWait(t, s, current, fresh.ID, state)
				if fresh.ID == initial.ID || executor.calls.Load() != 2 {
					t.Fatal("fresh request not separately admitted")
				}
			})
		}
	}
}

func assistantProtocolRequest(token string) *http.Request {
	r := httptest.NewRequest("GET", "/api/assistant-runs", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func TestAssistantRunProtocolAbandonAuthorityAndLease(t *testing.T) {
	s, token, start, executor := assistantProtocolFixture(t, assistantProtocolPause("awaiting-input"))
	v := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	v = assistantProtocolWait(t, s, token, v.ID, "awaiting-input")
	current := policyTestSession(t, s)
	path := "/api/assistant-runs/" + v.ID
	action := assistantRunAction{ID: "abandon", Kind: "abandon", Revision: v.Revision}
	foreign, err := s.sessions.createExpiring("other principal", nil, "", "")
	assistantRunMust(t, err)
	for _, credential := range []string{"", testToken, foreign} {
		if w := policyCall(s, credential, "GET", path, nil); w.Code < 400 {
			t.Fatal("foreign recovery read", w.Code)
		}
		if w := policyCall(s, credential, "POST", path+"/actions", action); w.Code < 400 {
			t.Fatal("foreign abandon", w.Code)
		}
	}
	other, otherToken := policyTestServer(t)
	if w := policyCall(other, otherToken, "GET", path, nil); w.Code != 404 {
		t.Fatal("cross-project read", w.Code)
	}
	if w := policyCall(other, otherToken, "POST", path+"/actions", action); w.Code != 404 {
		t.Fatal("cross-project abandon", w.Code)
	}
	malformed := action
	malformed.CallID = "call-one"
	if w := policyCall(s, current, "POST", path+"/actions", malformed); w.Code != 400 {
		t.Fatal("abandon accepted decision fields", w.Code)
	}
	// A competing process lease blocks abandonment even though the saved state is paused.
	lease, err := s.assistantRuns().Acquire(context.Background())
	assistantRunMust(t, err)
	if w := policyCall(s, current, "POST", path+"/actions", action); w.Code < 400 {
		t.Fatal("abandon bypassed executor lease")
	}
	assistantRunMust(t, lease.Close())
	// A stale local state cannot prove inactivity while an executor remains active.
	host := s.assistantRunService()
	host.mu.Lock()
	host.active[v.ID] = &assistantRunExecution{}
	host.mu.Unlock()
	w := policyCall(s, current, "POST", path+"/actions", action)
	host.mu.Lock()
	delete(host.active, v.ID)
	host.mu.Unlock()
	if w.Code != 409 {
		t.Fatal("abandon bypassed active execution", w.Code)
	}
	// Revocation after the initial read must still fail at the persistence boundary.
	record, err := host.read(assistantProtocolRequest(current), v.ID)
	assistantRunMust(t, err)
	request := assistantProtocolRequest(current)
	canceled, cancel := context.WithCancel(request.Context())
	cancel()
	host.mu.Lock()
	_, err = host.abandon(request.WithContext(canceled), record, action)
	host.mu.Unlock()
	if err == nil {
		t.Fatal("canceled private-workspace authorization retained")
	}
	// Policy history uses a stricter actor shape than basic workspace authentication.
	badIssuer := ""
	noHistory, err := s.sessions.createExpiring("local user", &badIssuer, "", "")
	assistantRunMust(t, err)
	if w := policyCall(s, noHistory, "POST", path+"/actions", action); w.Code != 401 {
		t.Fatal("missing Policy-history authorization", w.Code)
	}
	assistantProtocolView(t, policyCall(s, current, "POST", path+"/actions", action), 200)
	s.sessions.revoke(current)
	if w := policyCall(s, current, "POST", path+"/actions", action); w.Code != 401 {
		t.Fatal("revoked duplicate authorized", w.Code)
	}
	if executor.calls.Load() != 1 {
		t.Fatal("recovery dispatched")
	}
}

func TestAssistantRunProtocolSSECurrentCursorReconciliation(t *testing.T) {
	for _, state := range []string{"complete", "awaiting-input", "paused-budget", "awaiting-client"} {
		t.Run(state, func(t *testing.T) {
			s, token, start, executor := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
				for _, id := range []string{"first", "second"} {
					if err := e.Event(ctx, id, contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: id}}); err != nil {
						return err
					}
				}
				if state == "complete" {
					return assistantProtocolComplete(ctx, e)
				}
				return assistantProtocolPause(state)(ctx, e)
			})
			v := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
			v = assistantProtocolWait(t, s, token, v.ID, state)
			for _, count := range []int{1, 2} {
				r := assistantProtocolRequest(token)
				r.URL.Path = "/api/assistant-runs/" + v.ID + "/events"
				r.Header.Set("Last-Event-ID", fmt.Sprintf("%d:%d", v.Revision, count))
				w := httptest.NewRecorder()
				s.ServeHTTP(w, r)
				body := w.Body.String()
				wantEvents := 2 - count
				if w.Code != 200 || strings.Count(body, "event: event\n") != wantEvents || strings.Count(body, "event: snapshot\n") != 1 || !strings.HasSuffix(body, "\n\n") {
					t.Fatal("missing event suffix/snapshot", count, w.Code, body)
				}
				if count == 1 && !strings.Contains(strings.Split(body, "event: snapshot")[0], `"text":"second"`) {
					t.Fatal("lost suffix", body)
				}
			}
			if executor.calls.Load() != 1 {
				t.Fatal("SSE replay executed work")
			}
		})
	}
}

type assistantDeadlineWriter struct {
	*httptest.ResponseRecorder
	deadline          time.Time
	renewals, flushes int
}

func (w *assistantDeadlineWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	w.renewals++
	return nil
}
func (w *assistantDeadlineWriter) Write(raw []byte) (int, error) {
	if !w.deadline.After(time.Now()) {
		return 0, errors.New("expired deadline")
	}
	return w.ResponseRecorder.Write(raw)
}
func (w *assistantDeadlineWriter) FlushError() error {
	if !w.deadline.After(time.Now()) {
		return errors.New("expired flush deadline")
	}
	w.flushes++
	return nil
}
func TestAssistantRunProtocolSSEHeartbeatRenewsDeadline(t *testing.T) {
	w := &assistantDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	controller := http.NewResponseController(w)
	assistantRunMust(t, assistantRunSSE(w, controller, "snapshot", "1:0", map[string]any{"state": "running"}))
	w.deadline = time.Now().Add(-time.Second) // First idle heartbeat after the data deadline.
	assistantRunMust(t, assistantRunHeartbeat(w, controller))
	if w.renewals != 2 || w.flushes != 2 || time.Until(w.deadline) < 9*time.Second || !strings.HasSuffix(w.Body.String(), ": keepalive\n\n") {
		t.Fatal("heartbeat did not renew bounded write/flush", w.renewals, w.flushes)
	}
}

func TestAssistantRunProtocolApprovalNoteUTF16Bound(t *testing.T) {
	s, token, start, executor := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		if e.decision != nil {
			return assistantProtocolComplete(ctx, e)
		}
		return e.Checkpoint(ctx, assistantProtocolAsked(*e.record.Checkpoint))
	})
	v := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	v = assistantProtocolWait(t, s, token, v.ID, "awaiting-input")
	exact := strings.Repeat("😀", 1000)
	over := exact + "a"
	yes := true
	action := assistantRunAction{ID: "note-bound", Kind: "approval", Revision: v.Revision, CallID: "call-one", ArgsDigest: assistantRunArgsDigest(v.Checkpoint.Approval.Args), Approved: &yes, Note: &over}
	path := "/api/assistant-runs/" + v.ID + "/actions"
	if w := policyCall(s, token, "POST", path, action); w.Code != 409 {
		t.Fatal("overlong UTF-16 decision accepted", w.Code)
	}
	record, err := s.assistantRunService().read(assistantProtocolRequest(token), v.ID)
	assistantRunMust(t, err)
	if len(record.Actions) != 0 || executor.calls.Load() != 1 {
		t.Fatal("overlong decision persisted or launched")
	}
	action.Note = &exact
	assistantProtocolView(t, policyCall(s, token, "POST", path, action), 200)
	assistantProtocolWait(t, s, token, v.ID, "complete")
	record, err = s.assistantRunService().read(assistantProtocolRequest(token), v.ID)
	assistantRunMust(t, err)
	if len(record.Actions) != 1 || record.Actions[0].Decision.Note == nil || *record.Actions[0].Decision.Note != exact || executor.calls.Load() != 2 {
		t.Fatal("exact-bound note lost")
	}
}
