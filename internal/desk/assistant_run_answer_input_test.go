package desk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// These handwritten host-boundary cases supplement the actual API/native
// successor producers; they do not claim that the synthetic executor is a model.
func assistantAnswerInputFixture(t *testing.T, setup func(*Server, string, map[string]json.RawMessage)) (*Server, string, assistantRunView, *assistantProtocolExecutor) {
	t.Helper()
	s, token, start, executor := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		if len(e.record.AnswerHistory) == 0 {
			if err := e.Event(ctx, "question-event", assistantContinuationQuestion()); err != nil {
				return err
			}
		}
		return e.writer.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
			cp := *run.Checkpoint
			cp.Status = "complete"
			run.Checkpoint = &cp
			return true, nil
		})
	})
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	chat, err := assistantTestObject(doc.Chats[0])
	assistantRunMust(t, err)
	if setup != nil {
		setup(s, token, chat)
	}
	doc.Chats[0] = assistantChatEncodeObject(chat)
	policyWorkspaceSave(t, s, token, "/api/conversations", doc)
	saved = policyWorkspaceRead(t, s, token, "/api/conversations")
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	start.Revision = digestOf(doc.Chats[0])
	view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	return s, token, assistantProtocolWait(t, s, token, view.ID, "awaiting-input"), executor
}

func assistantAnswerInputPack(t *testing.T, s *Server, token string, chat map[string]json.RawMessage) {
	t.Helper()
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "jpack.json"), []byte(assistantGraphConfig), 0600))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte(assistantTestPack), 0600))
	chat["pack"] = assistantChatJSON(map[string]string{"id": "one", "path": "one.json", "digest": digestOf([]byte(assistantTestPack))})
	policyWorkspaceSave(t, s, token, "/api/pack-tests", json.RawMessage(`{"version":1,"suites":{"one":`+assistantTestSuiteFixture+`}}`))
}
func assistantAnswerInputPolicy(chat map[string]json.RawMessage, p policyReply) {
	chat["policy"] = assistantChatJSON(map[string]string{"id": p.Document.ID})
	chat["policyRequest"] = assistantChatJSON(assistantPolicyBasis{Revision: p.Revision, Candidate: assistantPolicyFields{p.Document.Candidate.Title, p.Document.Candidate.Narrative, p.Document.Candidate.ArtifactVersion}})
}
func assistantAnswerInputDocument(t *testing.T, chat map[string]json.RawMessage, ref assistantDocumentReference) {
	t.Helper()
	cp, err := assistantTestObject(chat["checkpoint"])
	assistantRunMust(t, err)
	state, err := assistantTestObject(cp["state"])
	assistantRunMust(t, err)
	turns, err := assistantChatArray(state["turns"])
	assistantRunMust(t, err)
	turn, err := assistantTestObject(turns[0])
	assistantRunMust(t, err)
	turn["attachments"] = assistantChatJSON([]any{map[string]any{"id": ref.ID, "name": "report.txt", "text": "", "document": ref}})
	turns[0] = assistantChatEncodeObject(turn)
	state["turns"] = assistantChatEncodeArray(turns)
	cp["state"] = assistantChatEncodeObject(state)
	chat["checkpoint"] = assistantChatEncodeObject(cp)
}

func assistantAnswerInputUnchanged(t *testing.T, s *Server, parent assistantRunRecord, executor *assistantProtocolExecutor) {
	t.Helper()
	store := s.assistantRuns()
	after, err := store.Read(t.Context(), parent.Binding, parent.ID)
	assistantRunMust(t, err)
	pending, err := pendingAssistantRunQuestion(after)
	assistantRunMust(t, err)
	ledger, err := store.readSnapshot(t.Context())
	assistantRunMust(t, err)
	if !reflect.DeepEqual(parent, after) || pending == nil || len(ledger.Runs) != 1 || executor.calls.Load() != 1 {
		t.Fatal("preparation/admission failure consumed question, changed parent or dispatched successor")
	}
}

func TestAssistantAnswerSuccessorInitialInputFailureKeepsPending(t *testing.T) {
	for _, kind := range []string{"saved-pack", "policy", "policy-storage", "document", "critic-invalid-deleted"} {
		t.Run(kind, func(t *testing.T) {
			var policy policyReply
			s, token, paused, executor := assistantAnswerInputFixture(t, func(s *Server, token string, chat map[string]json.RawMessage) {
				switch kind {
				case "saved-pack", "critic-invalid-deleted":
					assistantAnswerInputPack(t, s, token, chat)
					if kind == "critic-invalid-deleted" {
						chat["adversarialReview"] = assistantChatJSON(true)
						// Saved before the parent starts: these unchanged bytes pass
						// ordinary suite validation, but the critic refuses them.
						suite := strings.Replace(assistantTestSuiteFixture, `"revision":1,`, `"revision":1,"deleted":42,`, 1)
						policyWorkspaceSave(t, s, token, "/api/pack-tests", json.RawMessage(`{"version":1,"suites":{"one":`+suite+`}}`))
					}
				case "policy", "policy-storage":
					policy = policyCreate(t, s, token)
					assistantAnswerInputPolicy(chat, policy)
				case "document":
					_, pin, ref := assistantDocumentFixture(t, "web", "http", nil)
					writeDeskConfig(t, s, strings.Replace(researchDeskFile("http://127.0.0.1:9"), testSignerPublic, pin.Public, 1))
					assistantAnswerInputDocument(t, chat, ref)
				}
			})
			parent := canonicalActivationRecord(t, s, paused.ID)
			switch kind {
			case "saved-pack":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte(`{"broken"`), 0600))
			case "policy":
				edit := policyEditBody(t, policy)
				edit.Title = "Changed after request"
				policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+policy.Document.ID, edit))
			case "policy-storage":
				testHookPrivateRead = func(name string) error {
					if name == "ledger.json" {
						return errors.New("fixture initial Policy read failure")
					}
					return nil
				}
				t.Cleanup(func() { testHookPrivateRead = nil })
			}
			// No Runtime owner or model execution may precede input preparation.
			// The missing attachment refuses before invoking its verifier, too.
			marker := filepath.Join(t.TempDir(), "runtime-called")
			bin := filepath.Join(t.TempDir(), "runtime-refusal-fixture")
			assistantRunMust(t, os.WriteFile(bin, []byte("#!/bin/sh\n: > '"+marker+"'\nexit 90\n"), 0700))
			s.cfg.JpackBin = bin
			reply := policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", assistantContinuationAction(paused, "answer"))
			if reply.Code < 400 {
				t.Fatal("initial input refusal admitted successor", reply.Code)
			}
			testHookPrivateRead = nil
			assistantAnswerInputUnchanged(t, s, parent, executor)
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("Runtime executed before admission", err)
			}
		})
	}
}

func TestAssistantAnswerSuccessorPreparedInputStaleCASKeepsPending(t *testing.T) {
	for _, kind := range []string{"pack", "suite", "policy-revision", "policy-publication", "session", "critic-deleted"} {
		t.Run(kind, func(t *testing.T) {
			var policy policyReply
			s, token, paused, executor := assistantAnswerInputFixture(t, func(s *Server, token string, chat map[string]json.RawMessage) {
				if kind == "pack" || kind == "suite" || kind == "critic-deleted" {
					assistantAnswerInputPack(t, s, token, chat)
					if kind == "critic-deleted" {
						chat["adversarialReview"] = assistantChatJSON(true)
					}
				} else {
					policy = policyCreate(t, s, token)
					if kind == "policy-publication" {
						policy = policyApprove(t, s, token, policy)
					}
					assistantAnswerInputPolicy(chat, policy)
				}
			})
			request := assistantDocumentRequest(token)
			identity, _, err := s.assistantRunIdentity(request)
			assistantRunMust(t, err)
			parent := canonicalActivationRecord(t, s, paused.ID)
			lease, err := s.assistantRuns().Acquire(t.Context())
			assistantRunMust(t, err)
			defer lease.Close()
			start := assistantRunStart{Purpose: "chat", OwnerID: parent.Binding.Owner.ID, Revision: parent.Binding.Owner.Revision, RequestID: parent.Binding.RequestID, AdmissionID: "stale-input-answer"}
			source, err := s.readAssistantRunSource(request, start, identity, parent.Binding.Project)
			assistantRunMust(t, err)
			cp, err := executor.Prepare(t.Context(), source)
			assistantRunMust(t, err)
			source.Binding.ExecutionKey, source.Binding.Unit = cp.RequestKey, cp.Unit
			action := assistantContinuationAction(paused, "answer")
			answer := assistantRunAnswer{QuestionID: action.QuestionID, QuestionDigest: action.QuestionDigest, Selected: action.Answer.Selected, Text: *action.Answer.Text, At: "2026-11-04T00:00:00Z"}
			q, err := assistantRunQuestion(parent, answer.QuestionID)
			assistantRunMust(t, err)
			origin := assistantRunAnswerOrigin{ParentRunID: parent.ID, AdmissionID: parent.AdmissionID, ActionID: action.ID, Question: *q, Answer: answer}
			prepared, err := s.prepareAssistantAnswerInput(request, source, append(append([]assistantRunAnswerOrigin{}, parent.AnswerHistory...), origin))
			assistantRunMust(t, err)
			switch kind {
			case "pack":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte(assistantSavedPackCorrected), 0600))
			case "suite":
				policyWorkspaceSave(t, s, token, "/api/pack-tests", json.RawMessage(`{"version":1,"suites":{"one":{"revision":2,"cases":[],"runs":[],"recovered":[]}}}`))
			case "critic-deleted":
				suite := strings.Replace(assistantTestSuiteFixture, `"revision":1,`, `"revision":1,"deleted":["existing"],`, 1)
				policyWorkspaceSave(t, s, token, "/api/pack-tests", json.RawMessage(`{"version":1,"suites":{"one":`+suite+`}}`))
				current, err := s.readAssistantTestTarget(t.Context(), source, "one")
				assistantRunMust(t, err)
				if prepared.custody.critic == nil || !assistantTestSame(prepared.savedPack.Read, current) {
					t.Fatal("fixture did not isolate changed critic custody from the saved-pack basis")
				}
			case "policy-revision":
				edit := policyEditBody(t, policy)
				edit.Title = "Changed after preparation"
				policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+policy.Document.ID, edit))
			case "policy-publication":
				policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+policy.Document.ID+"/publish", policyPublishBody(t, policy)))
			case "session":
				s.sessions.revoke(token)
			}
			admission := &assistantRunAnswerAdmission{parent: parent, action: action, origin: origin, checkpoint: cp, savedPack: prepared.savedPack, critic: prepared.custody.critic}
			_, created, err := lease.Admit(t.Context(), source.Binding, start.AdmissionID, assistantRunActionDigest(action), func() error {
				current, _, err := s.assistantRunIdentity(request)
				if err != nil || !assistantRunIdentityMatches(current, identity) {
					return errAssistantRunScope
				}
				fresh, err := s.resolveAssistantRun(request, start, current, parent.Binding.Project)
				if err != nil {
					return err
				}
				fresh.Binding.ExecutionKey, fresh.Binding.Unit = source.Binding.ExecutionKey, source.Binding.Unit
				if !assistantRunFreshSourceEqual(source, fresh) {
					return errAssistantRunConflict
				}
				return s.recheckAssistantAnswerInputLocked(request, source, prepared)
			}, admission)
			if err == nil || created {
				t.Fatal("stale prepared input admitted successor")
			}
			assistantAnswerInputUnchanged(t, s, parent, executor)
		})
	}
}

func TestAssistantAnswerSuccessorInitialSavedPackIsAtomic(t *testing.T) {
	s, token, paused, _ := assistantAnswerInputFixture(t, func(s *Server, token string, chat map[string]json.RawMessage) {
		assistantAnswerInputPack(t, s, token, chat)
		chat["adversarialReview"] = assistantChatJSON(true)
	})
	parent := canonicalActivationRecord(t, s, paused.ID)
	executor := s.assistantRunExecutor.(*assistantProtocolExecutor)
	seen := make(chan *assistantRunExecution, 1)
	executor.execute = func(ctx context.Context, e *assistantRunExecution) error {
		seen <- e
		before, prepared := e.writer.revision, e.input.critic
		held, err := e.holdAssistantCritic(ctx)
		if err != nil {
			return err
		}
		if prepared == nil || held != prepared || e.writer.revision != before {
			return errors.New("initial critic custody was reread or separately persisted after admission")
		}
		return assistantProtocolComplete(ctx, e)
	}
	response := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", assistantContinuationAction(paused, "answer")), 200)
	next := assistantContinuationSuccessor(t, s, token, response)
	complete := assistantProtocolWait(t, s, token, next.ID, "complete")
	e := <-seen
	stored := canonicalActivationRecord(t, s, complete.ID)
	if e.initialInput == nil || !e.input.ready || stored.SavedPack == nil || !reflect.DeepEqual(stored.SavedPack, e.record.SavedPack) || stored.SavedPack.Read.PackDigest != digestOf([]byte(assistantTestPack)) || e.initialInput.Request.Message != "Review exact input" || len(stored.AnswerHistory) != 1 {
		t.Fatal("admitted execution did not receive the exact initialized pack/input/custody")
	}
	if e.input.critic == nil || !validAssistantCriticCustody(stored.CriticCustody) || stored.CriticCustody == nil || stored.CriticCustody.Fingerprint != assistantCriticFingerprint(stored.ID, stored.Binding, e.input.critic) || stored.CriticCustody.Fingerprint == assistantCriticFingerprint(parent.ID, parent.Binding, e.input.critic) || !reflect.DeepEqual(stored.CriticCustody, e.record.CriticCustody) || e.input.critic.fingerprint != stored.CriticCustody.Fingerprint {
		t.Fatal("fresh run-specific critic custody was not initialized atomically with the exact prepared snapshot")
	}
	if stored.Checkpoint.ID == parent.Checkpoint.ID || stored.Checkpoint.Used != 0 || len(stored.Checkpoint.Grants) != 0 || len(stored.Operations) != 0 {
		t.Fatal("prepared source data inherited execution authority")
	}
	if !reflect.DeepEqual(parent.Checkpoint, canonicalActivationRecord(t, s, parent.ID).Checkpoint) || executor.calls.Load() != 2 {
		t.Fatal("answer changed completed checkpoint or dispatched twice")
	}
}

func TestAssistantAnswerSuccessorPreparedDocumentStaleCASKeepsPending(t *testing.T) {
	program := assistantDocumentProgram(t)
	bundle := os.Getenv("JPS_TEST_DOCUMENT_BUNDLE")
	if bundle == "" {
		bundle = os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	}
	if bundle == "" {
		t.Skip("set JPS_TEST_DOCUMENT_BUNDLE to the pinned Gateway bundle")
	}
	if filepath.Join(bundle, executableName("gateway")) != program {
		t.Fatal("tests must share the same pinned executable")
	}
	raw, pin, ref := assistantDocumentFixture(t, "web", "http", nil)
	s, token, paused, executor := assistantAnswerInputFixture(t, func(s *Server, token string, chat map[string]json.RawMessage) {
		s.cfg.LocalGatewayBundle = bundle
		writeDeskConfig(t, s, strings.Replace(researchDeskFile("http://127.0.0.1:9"), testSignerPublic, pin.Public, 1))
		assistantAnswerInputDocument(t, chat, ref)
	})
	store, err := s.openChatData()
	assistantRunMust(t, err)
	defer store.root.Close()
	conversation, err := resolveConversationName(store.root, s.conversationIdentity())
	assistantRunMust(t, err)
	name := attachmentProjectPrefix(conversation) + ref.ID + ".json"
	assistantRunMust(t, writePrivateData(store.root, name, raw))
	request := assistantDocumentRequest(token)
	identity, _, err := s.assistantRunIdentity(request)
	assistantRunMust(t, err)
	parent := canonicalActivationRecord(t, s, paused.ID)
	lease, err := s.assistantRuns().Acquire(t.Context())
	assistantRunMust(t, err)
	defer lease.Close()
	start := assistantRunStart{Purpose: "chat", OwnerID: parent.Binding.Owner.ID, Revision: parent.Binding.Owner.Revision, RequestID: parent.Binding.RequestID, AdmissionID: "stale-document-answer"}
	source, err := s.readAssistantRunSource(request, start, identity, parent.Binding.Project)
	assistantRunMust(t, err)
	cp, err := executor.Prepare(t.Context(), source)
	assistantRunMust(t, err)
	source.Binding.ExecutionKey, source.Binding.Unit = cp.RequestKey, cp.Unit
	action := assistantContinuationAction(paused, "answer")
	answer := assistantRunAnswer{QuestionID: action.QuestionID, QuestionDigest: action.QuestionDigest, Selected: action.Answer.Selected, Text: *action.Answer.Text, At: "2026-11-04T00:00:00Z"}
	q, err := assistantRunQuestion(parent, answer.QuestionID)
	assistantRunMust(t, err)
	origin := assistantRunAnswerOrigin{ParentRunID: parent.ID, AdmissionID: parent.AdmissionID, ActionID: action.ID, Question: *q, Answer: answer}
	prepared, err := s.prepareAssistantAnswerInput(request, source, []assistantRunAnswerOrigin{origin})
	assistantRunMust(t, err)
	if len(prepared.custody.documents) != 1 || prepared.custody.documents[0].Pages[0].Text != "Selected first page." {
		t.Fatal("actual verified document preparation missing")
	}
	assistantRunMust(t, writePrivateData(store.root, name, append(raw, ' ')))
	admission := &assistantRunAnswerAdmission{parent: parent, action: action, origin: origin, checkpoint: cp}
	_, created, err := lease.Admit(t.Context(), source.Binding, start.AdmissionID, assistantRunActionDigest(action), func() error {
		return s.recheckAssistantAnswerInputLocked(request, source, prepared)
	}, admission)
	if err == nil || created {
		t.Fatal("changed exact attachment bytes admitted successor")
	}
	assistantAnswerInputUnchanged(t, s, parent, executor)
}

func TestAssistantAnswerSuccessorJournaledRuntimeFailureKeepsAdmission(t *testing.T) {
	s, token, paused, executor := assistantAnswerInputFixture(t, func(s *Server, token string, chat map[string]json.RawMessage) {
		assistantAnswerInputPack(t, s, token, chat)
	})
	parent := canonicalActivationRecord(t, s, paused.ID)
	entered := make(chan *assistantRunExecution, 1)
	release := make(chan struct{})
	executor.execute = func(ctx context.Context, e *assistantRunExecution) error {
		entered <- e
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		x, err := e.openAssistantExecution(ctx, assistantExecutionPrompt{Name: "served"})
		if x != nil {
			x.Close()
		}
		return err
	}
	action := assistantContinuationAction(paused, "answer")
	response := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", action), 200)
	next := assistantContinuationSuccessor(t, s, token, response)
	e := <-entered
	assistantExecutionRuntimeFixture(t, s, e)
	peer := `#!/usr/bin/env python3
import json,sys
for line in sys.stdin:
 q=json.loads(line)
 with open('execution-fixture.json') as f: config=json.load(f)
 with open(config['ledger']) as f: ledger=json.load(f)
 ops=[op for run in ledger['runs'] for op in (run.get('operations') or []) if op['id']==q.get('id')]
 if len(ops)!=1 or ops[0]['state']!='dispatched': sys.exit(31)
 with open('answer-runtime-frame.json','w') as f: f.write(line)
 sys.exit(72)
`
	assistantRunMust(t, os.WriteFile(s.cfg.JpackBin, []byte(peer), 0700))
	close(release)
	stopped := assistantProtocolWait(t, s, token, next.ID, "stopped")
	if _, err := os.Stat(filepath.Join(s.cfg.ProjectDir, "answer-runtime-frame.json")); err != nil {
		t.Fatal("new run did not journal and dispatch Runtime", err)
	}
	// A dispatched request without a response has unknown effects. The shared
	// Runtime boundary stops the run and retains an uncertain receipt.
	stored := canonicalActivationRecord(t, s, stopped.ID)
	if stored.State != "stopped" || stored.Checkpoint.Status != "stopped" || len(stored.Operations) != 1 || stored.Operations[0].Kind != "runtime" || stored.Operations[0].State != "uncertain" || stored.Operations[0].Result != nil || e.ctx.Err() == nil || e.writer.Err() != nil {
		t.Fatal("Runtime uncertainty lost its stopped run or exact unfinished receipt")
	}
	if stored.AdmissionID == "" || stored.AdmissionID == parent.AdmissionID || len(stored.AnswerHistory) != 1 || stored.AnswerHistory[0].ParentRunID != parent.ID || stored.AnswerHistory[0].ActionID != action.ID {
		t.Fatal("Runtime uncertainty lost successor admission or answer origin")
	}
	after := canonicalActivationRecord(t, s, paused.ID)
	pending, err := pendingAssistantRunQuestion(after)
	assistantRunMust(t, err)
	if pending != nil || after.State != "complete" || len(after.Actions) != 1 || after.Actions[0].SuccessorRunID != stopped.ID || !reflect.DeepEqual(parent.Checkpoint, after.Checkpoint) || e.initialInput != nil {
		t.Fatal("journaled execution failure rolled back answer or lost prepared input")
	}
	assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", action), 200)
	if executor.calls.Load() != 2 || !reflect.DeepEqual(stored, canonicalActivationRecord(t, s, stopped.ID)) || !reflect.DeepEqual(after, canonicalActivationRecord(t, s, paused.ID)) {
		t.Fatal("duplicate answer replayed uncertainty or changed its retained admission")
	}
}
