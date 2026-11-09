package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// These transaction tests supplement the actual API/native producers. They do
// not qualify hand-authored checkpoint transitions as actual provider evidence.
func TestAssistantAnswerSuccessorAdmissionFailureKeepsQuestionPending(t *testing.T) {
	for _, failure := range []string{"promotion", "durable-preparation", "attempted-write"} {
		t.Run(failure, func(t *testing.T) {
			s, token, paused, executor := assistantContinuationFixture(t, "answer", nil)
			request := httptest.NewRequest("POST", "/api/assistant-runs", nil)
			request.Header.Set("Authorization", "Bearer "+token)
			identity, _, err := s.assistantRunIdentity(request)
			assistantRunMust(t, err)
			parent, err := s.assistantRuns().lookup(t.Context(), identity, paused.ID, "", false)
			assistantRunMust(t, err)
			lease, err := s.assistantRuns().Acquire(t.Context())
			assistantRunMust(t, err)
			defer lease.Close()
			start := assistantRunStart{Purpose: "chat", OwnerID: parent.Binding.Owner.ID, Revision: parent.Binding.Owner.Revision, RequestID: parent.Binding.RequestID, AdmissionID: "failed-successor"}
			source, err := s.readAssistantRunSource(request, start, identity, parent.Binding.Project)
			assistantRunMust(t, err)
			cp, err := executor.Prepare(t.Context(), source)
			assistantRunMust(t, err)
			source.Binding.ExecutionKey = cp.RequestKey
			source.Binding.Unit = cp.Unit
			action := assistantContinuationAction(paused, "answer")
			answer := assistantRunAnswer{QuestionID: action.QuestionID, QuestionDigest: action.QuestionDigest, Selected: action.Answer.Selected, Text: *action.Answer.Text, At: "2026-10-23T00:00:00Z"}
			q, err := assistantRunQuestion(parent, answer.QuestionID)
			assistantRunMust(t, err)
			admission := &assistantRunAnswerAdmission{parent: parent, action: action, origin: assistantRunAnswerOrigin{ParentRunID: parent.ID, AdmissionID: parent.AdmissionID, ActionID: action.ID, Question: *q, Answer: answer}, checkpoint: cp, canonical: true}
			if failure == "promotion" {
				admission.promote = func(assistantRunRecord) error { return errAssistantRunScope }
			} else {
				admission.prepare = func(candidate assistantRunRecord) error {
					writer := &assistantRunWriter{lease: lease, binding: candidate.Binding, id: candidate.ID, revision: candidate.Revision}
					store := &assistantCanonicalRunStore{writer: writer, id: candidate.Canonical.Head.RunID, prepared: &candidate}
					before, _ := json.Marshal(candidate)
					if failure == "attempted-write" {
						_, err := store.Commit(t.Context(), runcontext.RunCommit{RunID: store.id, ExpectedRevision: candidate.Canonical.Head.Revision, CommitID: "provisional-write", Checkpoint: candidate.Canonical.Head.Checkpoint, Events: [][]byte{}})
						after, _ := json.Marshal(candidate)
						if !errors.Is(err, errAssistantRunDispatch) || string(before) != string(after) {
							t.Fatal("provisional view accepted mutation", err)
						}
						return err
					}
					// Exercise the actual published durable-preparation refusal with the
					// retained complete head. The store must never reopen it or mutate it.
					retained := parent
					raw, err := json.Marshal(parent.Checkpoint)
					if err != nil {
						return err
					}
					retained.Canonical = &assistantCanonicalJournal{Version: 1, Active: true, Head: assistantCanonicalHead{RunID: parent.Checkpoint.ID, Revision: "prepared-revision", CommitID: "prepared-commit", Checkpoint: raw}}
					store.id = parent.Checkpoint.ID
					store.prepared = &retained
					_, err = runcontext.PrepareDurableInput(t.Context(), &runcontext.Input{Execution: parent.Checkpoint, RunStore: &runcontext.DurableRunInput{Store: store, ID: store.id, Resume: true}})
					var complete *runcontext.RunComplete
					if !errors.As(err, &complete) {
						t.Fatal("published completed-head refusal absent", err)
					}
					return err
				}
			}
			_, created, err := lease.Admit(t.Context(), source.Binding, start.AdmissionID, assistantRunActionDigest(action), func() error { return nil }, admission)
			if err == nil || created {
				t.Fatal("failed preparation admitted successor", err)
			}
			after, err := lease.store.Read(t.Context(), parent.Binding, parent.ID)
			assistantRunMust(t, err)
			if !reflect.DeepEqual(parent, after) {
				t.Fatal("failed admission resolved or rewrote parent")
			}
			pending, err := pendingAssistantRunQuestion(after)
			assistantRunMust(t, err)
			if pending == nil || executor.calls.Load() != 1 {
				t.Fatal("question resolved or provider dispatched")
			}
			ledger, err := lease.store.readSnapshot(t.Context())
			assistantRunMust(t, err)
			if len(ledger.Runs) != 1 {
				t.Fatal("partially admitted successor discoverable")
			}
		})
	}
}

func TestAssistantAnswerSuccessorFreshAllowanceAtParentCeiling(t *testing.T) {
	s, token, paused, executor := assistantContinuationFixture(t, "answer", func(run *assistantRunRecord) { run.Checkpoint.Used = run.Checkpoint.Limit })
	answer := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", assistantContinuationAction(paused, "answer")), 200)
	next := assistantContinuationSuccessor(t, s, token, answer)
	complete := assistantProtocolWait(t, s, token, next.ID, "complete")
	if complete.ID == paused.ID || complete.Checkpoint.Used != 0 || complete.Checkpoint.Limit != 20 || len(complete.Checkpoint.Grants) != 0 || executor.calls.Load() != 2 {
		t.Fatal("fresh default allowance missing", complete)
	}
}

func TestAssistantAnswerSuccessorRetainsOriginalChatAndFailedPrepare(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-chat", true: "failed-prepare"}[fail], func(t *testing.T) {
			s, token, start, _ := assistantProtocolFixture(t, nil)
			entered := make(chan *assistantRunExecution, 2)
			executor := &assistantContextOwnershipExecutor{}
			executor.execute = func(ctx context.Context, e *assistantRunExecution) error {
				entered <- e
				if len(e.record.AnswerHistory) == 0 {
					if _, err := e.canonicalBinding.Writer.Event(ctx, *e.canonicalBinding, assistantContinuationQuestion()); err != nil {
						return err
					}
				}
				cp := *e.canonicalBinding.Writer.Checkpoint()
				cp.Status = "complete"
				return e.canonicalBinding.Writer.Save(ctx, *e.canonicalBinding, cp)
			}
			s.assistantRunExecutor = executor
			start = assistantContextStartFixture(t, s, token, start)
			initial := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
			original := <-entered
			assistantContextDone(t, original)
			paused := assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+initial.ID, nil), 200)
			parent := canonicalActivationRecord(t, s, paused.ID)
			chat := original.contextHandle.Chat()
			if fail {
				executor.prepare = func(context.Context, assistantRunSource) (contract.Checkpoint, error) {
					return contract.Checkpoint{}, errAssistantRunUnavailable
				}
			}
			action := assistantContinuationAction(paused, "answer")
			reply := policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", action)
			if fail {
				if reply.Code < 400 {
					t.Fatal("failed preparation admitted")
				}
				after := canonicalActivationRecord(t, s, paused.ID)
				if !reflect.DeepEqual(parent, after) || chat.IsDisposed() {
					t.Fatal("failed preparation settled original question/context")
				}
				return
			}
			admitted := assistantProtocolView(t, reply, 200)
			successor := <-entered
			if successor.record.ID == original.record.ID || successor.contextHandle.Chat() != chat || successor.canonicalInput.Chat != chat || successor.canonicalStore.prepared != nil || successor.canonicalStore.writer != successor.writer || successor.canonicalInput.Persistence != successor.canonicalBinding {
				t.Fatal("prepared successor changed admitted Chat or writer identity")
			}
			assistantContextDone(t, successor)
			next := assistantContinuationSuccessor(t, s, token, admitted)
			if next.Checkpoint.Used != 0 || next.Checkpoint.Limit != 20 || len(next.AnswerHistory) != 1 {
				t.Fatal("successor lost fresh allowance/history")
			}
			after := canonicalActivationRecord(t, s, paused.ID)
			if !reflect.DeepEqual(parent.Checkpoint, after.Checkpoint) || !reflect.DeepEqual(parent.Canonical, after.Canonical) || !reflect.DeepEqual(parent.Operations, after.Operations) || !reflect.DeepEqual(parent.Events, after.Events) {
				t.Fatal("completed parent changed")
			}
			s.assistantRunService().mu.Lock()
			oldHeld := s.assistantRunService().contextOwners[paused.ID] != nil
			s.assistantRunService().mu.Unlock()
			if oldHeld {
				t.Fatal("settled parent kept duplicate context owner")
			}
		})
	}
}

func TestAssistantAnswerSuccessorSharedHistoryVectors(t *testing.T) {
	parentRaw, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "assistant", "serverRuns", "fixtures", "answer-parent.json"))
	assistantRunMust(t, err)
	var parent assistantRunView
	assistantRunMust(t, json.Unmarshal(parentRaw, &parent))
	var rawParent struct {
		Continuations []json.RawMessage `json:"continuations"`
	}
	assistantRunMust(t, json.Unmarshal(parentRaw, &rawParent))
	receipt, err := readAssistantRunContinuation(rawParent.Continuations[0])
	assistantRunMust(t, err)
	successorRaw, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "assistant", "serverRuns", "fixtures", "answer-successor.json"))
	assistantRunMust(t, err)
	var child assistantRunView
	assistantRunMust(t, json.Unmarshal(successorRaw, &child))
	var rawChild struct {
		AnswerHistory json.RawMessage `json:"answerHistory"`
	}
	assistantRunMust(t, json.Unmarshal(successorRaw, &rawChild))
	assistantRunMust(t, readAssistantRunAnswerHistory(rawChild.AnswerHistory))
	if receipt.SuccessorRunID != child.ID || len(child.AnswerHistory) != 1 || child.AnswerHistory[0].ParentRunID != parent.ID || !reflect.DeepEqual(child.AnswerHistory[0].Answer, *receipt.Answer) {
		t.Fatal("shared successor/history identity lost")
	}
	for _, key := range []string{"parentRunId", "admissionId", "actionId", "question", "answer"} {
		var fields []map[string]json.RawMessage
		assistantRunMust(t, json.Unmarshal(rawChild.AnswerHistory, &fields))
		delete(fields[0], key)
		raw, err := json.Marshal(fields)
		assistantRunMust(t, err)
		if readAssistantRunAnswerHistory(raw) == nil {
			t.Fatal("omitted retained history member accepted", key)
		}
	}
	origin := child.AnswerHistory[0]
	view := assistantRunHistoryProjection(assistantRunRecord{ID: child.ID, AnswerHistory: []assistantRunAnswerOrigin{origin, {ParentRunID: "arun_33333333333333333333333333333333", AdmissionID: "second", ActionID: origin.ActionID, Question: origin.Question, Answer: origin.Answer}}})
	if view.Events[0].Event.UserQuestion.ID == view.Events[1].Event.UserQuestion.ID {
		t.Fatal("separate run-local question IDs merged")
	}
	for _, action := range view.Actions {
		assistantRunMust(t, validateAssistantRunAnswer(view, *action.Answer))
	}
}
