package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func assistantContinuationQuestion() contract.RunEvent {
	return contract.RunEvent{UserQuestion: &contract.RunEventUserQuestion{Type: "user_question", ID: "question-one", Question: contract.Question{Title: "Scope", Question: "Which scope?", Selection: "single", RecommendedOptionID: contract.Nullable[string]{Set: true, Null: true}, Recommendation: contract.Nullable[string]{Set: true, Null: true}, Options: []contract.QuestionOption{{ID: "one", Label: "One", Description: ""}, {ID: "two", Label: "Two", Description: ""}}}}}
}
func assistantContinuationFixture(t *testing.T, kind string, mutate func(*assistantRunRecord)) (*Server, string, assistantRunView, *assistantProtocolExecutor) {
	t.Helper()
	s, token, start, executor := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		if len(e.record.Actions) > 0 {
			return assistantProtocolComplete(ctx, e)
		}
		if kind == "answer" {
			if err := e.Event(ctx, "question-event", assistantContinuationQuestion()); err != nil {
				return err
			}
		}
		return e.writer.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
			cp := *run.Checkpoint
			if kind == "answer" {
				cp.Status = "complete"
			} else {
				cp.Status = "paused"
				cp.Used = cp.Limit
			}
			run.Checkpoint = &cp
			if mutate != nil {
				mutate(run)
			}
			return true, nil
		})
	})
	view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	state := "awaiting-input"
	if kind != "answer" {
		state = "paused-budget"
	}
	return s, token, assistantProtocolWait(t, s, token, view.ID, state), executor
}
func assistantContinuationAction(view assistantRunView, kind string) assistantRunAction {
	a := assistantRunAction{ID: "continue-once", Kind: kind, Revision: view.Revision}
	if kind == "answer" {
		text := "Exact answer <&> 한글\nnext line"
		a.QuestionID = "question-one"
		a.QuestionDigest = assistantRunQuestionDigest(assistantContinuationQuestion().UserQuestion.Question)
		a.Answer = &assistantRunAnswerDraft{Selected: []string{"two"}, Text: &text}
	}
	return a
}
func TestAssistantRunContinuationAnswerAndBudgetOnce(t *testing.T) {
	for _, kind := range []string{"answer", "extend-budget"} {
		t.Run(kind, func(t *testing.T) {
			s, token, paused, executor := assistantContinuationFixture(t, kind, nil)
			a := assistantContinuationAction(paused, kind)
			before, _ := json.Marshal(paused.Checkpoint)
			admitted := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", a), 200)
			if admitted.State != "admitted" || admitted.Checkpoint.Status != "running" || len(admitted.Continuations) != 1 {
				t.Fatal("admission/receipt missing", admitted)
			}
			complete := assistantProtocolWait(t, s, token, paused.ID, "complete")
			duplicate := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", a), 200)
			if duplicate.Revision != complete.Revision || executor.calls.Load() != 2 {
				t.Fatal("duplicate re-launched", executor.calls.Load())
			}
			changed := a
			changed.Revision++
			if w := policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", changed); w.Code != 409 {
				t.Fatal("changed payload accepted", w.Code)
			}
			if complete.Checkpoint.ID != paused.Checkpoint.ID || complete.Checkpoint.RequestKey != paused.Checkpoint.RequestKey || complete.Checkpoint.Used != paused.Checkpoint.Used {
				t.Fatal("original identity/accounting lost")
			}
			if kind == "answer" {
				after, _ := json.Marshal(complete.Checkpoint)
				if string(before) != string(after) {
					t.Fatal("answer changed allowance/checkpoint")
				}
			} else if len(complete.Checkpoint.Grants) != 1 || complete.Checkpoint.Limit != paused.Checkpoint.Limit+int64(runcontext.RequestIncrement) {
				t.Fatal("canonical grant missing")
			}
			assistantRunClientJSON(t, "continuation-"+kind+".json", complete)
		})
	}
}
func TestAssistantRunContinuationRefusals(t *testing.T) {
	for _, mode := range []string{"stale", "question", "digest", "option", "multiple", "empty", "oversize", "uncertain", "not-started", "canonical-interrupted", "ceiling", "approval", "phase", "profile", "session", "policy", "storage", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			mutate := func(run *assistantRunRecord) {
				switch mode {
				case "uncertain", "not-started":
					op := assistantRunOp("unsafe")
					op.State = mode
					run.Operations = append(run.Operations, op)
				case "canonical-interrupted":
					run.Checkpoint.Operations = append(run.Checkpoint.Operations, contract.CheckpointOperation{ID: "interrupted", Name: "validate", Args: json.RawMessage(`{}`), Status: "interrupted"})
				case "ceiling":
					run.Checkpoint.Used = run.Checkpoint.Limit
				case "approval":
					cp := assistantProtocolAsked(*run.Checkpoint)
					cp.Status = "complete"
					run.Checkpoint = &cp
				}
			}
			s, token, paused, executor := assistantContinuationFixture(t, "answer", mutate)
			a := assistantContinuationAction(paused, "answer")
			switch mode {
			case "stale":
				a.Revision--
			case "question":
				a.QuestionID = "other"
			case "digest":
				a.QuestionDigest = strings.Repeat("a", 64)
			case "option":
				a.Answer.Selected = []string{"missing"}
			case "multiple":
				a.Answer.Selected = []string{"one", "two"}
			case "empty":
				text := " "
				a.Answer.Selected = []string{}
				a.Answer.Text = &text
			case "oversize":
				text := strings.Repeat("x", 4001)
				a.Answer.Text = &text
			case "session":
				token = policyTestSession(t, s)
			case "policy":
				s.signIn.mu.Lock()
				s.signIn.generation++
				s.signIn.mu.Unlock()
			case "phase":
				saved := policyWorkspaceRead(t, s, token, "/api/conversations")
				var doc conversationDocument
				assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
				doc.Chats[0] = []byte(strings.Replace(string(doc.Chats[0]), `"work"`, `"plan"`, 1))
				if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); w.Code != 200 {
					t.Fatal(w.Code)
				}
			case "profile":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, assistantProfilePath), []byte(`{"profileVersion":1,"projectInstructions":"changed"}`), 0600))
			case "storage":
				testHookPrivateRead = func(string) error { return errors.New("continuation storage fixture") }
				defer func() { testHookPrivateRead = nil }()
			case "cancel":
				assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", assistantRunAction{ID: "cancel", Kind: "cancel", Revision: paused.Revision}), 200)
			}
			if w := policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", a); w.Code < 400 {
				t.Fatal("unsafe continuation admitted", w.Code)
			}
			if executor.calls.Load() != 1 {
				t.Fatal("refusal dispatched", executor.calls.Load())
			}
		})
	}
}
func TestAssistantRunContinuationMalformedAndRepeatedQuestions(t *testing.T) {
	s, token, paused, executor := assistantContinuationFixture(t, "answer", nil)
	base := `{"id":"answer","kind":"answer","revision":` + jsonNumber(paused.Revision) + `,"questionId":"question-one","questionDigest":"` + assistantContinuationAction(paused, "answer").QuestionDigest + `","answer":`
	for _, tail := range []string{`{}}`, `{"selected":[]}}`, `{"text":"yes"}}`, `{"selected":null,"text":"yes"}}`, `{"selected":[],"text":"yes","grant":true}}`, `{"selected":[],"Text":"yes"}}`} {
		r := httptest.NewRequest("POST", "/api/assistant-runs/"+paused.ID+"/actions", strings.NewReader(base+tail))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("malformed accepted", tail, w.Code, w.Body.String())
		}
	}
	if executor.calls.Load() != 1 {
		t.Fatal("malformed dispatched")
	}
	run := assistantRunRecord{Events: []assistantRunEvent{{ID: "e1", Event: assistantContinuationQuestion()}, {ID: "e2", Event: assistantContinuationQuestion()}}}
	if _, err := pendingAssistantRunQuestion(run); err == nil {
		t.Fatal("repeated question ID accepted")
	}
}
func jsonNumber(value uint64) string { raw, _ := json.Marshal(value); return string(raw) }
func TestAssistantRunContinuationPersistedCrashNeverReplays(t *testing.T) {
	s, token, paused, executor := assistantContinuationFixture(t, "answer", nil)
	lease, err := s.assistantRuns().Acquire(context.Background())
	assistantRunMust(t, err)
	var record assistantRunRecord
	_, err = lease.store.transaction(context.Background(), func(ledger *assistantRunLedger) (bool, error) {
		for i := range ledger.Runs {
			run := &ledger.Runs[i]
			if run.ID == paused.ID {
				a := assistantContinuationAction(paused, "answer")
				cp, err := reopenAssistantRunQuestionCheckpoint(*run)
				if err != nil {
					return false, err
				}
				run.Checkpoint = cp
				run.Actions = append(run.Actions, assistantRunActionReceipt{ID: a.ID, Kind: a.Kind, Digest: assistantRunActionDigest(a), Answer: &assistantRunAnswer{QuestionID: a.QuestionID, QuestionDigest: a.QuestionDigest, Selected: a.Answer.Selected, Text: *a.Answer.Text, At: "2026-10-23T00:00:00Z"}})
				run.State = "admitted"
				run.Revision++
				record = *run
				return true, nil
			}
		}
		return false, errors.New("run missing")
	})
	assistantRunMust(t, err)
	assistantRunMust(t, lease.Close())
	lease, err = s.assistantRuns().Acquire(context.Background())
	assistantRunMust(t, err)
	assistantRunMust(t, lease.Close())
	got := assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+paused.ID, nil), 200)
	if got.State != "interrupted" || len(got.Continuations) != 1 || executor.calls.Load() != 1 {
		t.Fatal("recovery replayed/lost receipt", got.State)
	}
	duplicate := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", assistantContinuationAction(paused, "answer")), 200)
	if duplicate.State != "interrupted" || executor.calls.Load() != 1 {
		t.Fatal("duplicate replayed after crash")
	}
	source := []byte(`{"id":"chat-one","checkpoint":{"state":{"turns":[{"id":"message-one","role":"user","kind":"message","text":"Review exact input","at":"2026-10-20T00:00:00Z"}]}}}`)
	record.State = "stopped"
	projected, err := compileAssistantChatProjection(source, record, "2026-10-23T00:00:00Z")
	assistantRunMust(t, err)
	chat, err := assistantChatObject(projected)
	assistantRunMust(t, err)
	cp, err := assistantChatObject(chat["checkpoint"])
	assistantRunMust(t, err)
	state, err := assistantChatObject(cp["state"])
	assistantRunMust(t, err)
	var questions []contract.QuestionSaved
	assistantRunMust(t, json.Unmarshal(state["questions"], &questions))
	if len(questions) != 1 || questions[0].Resolution == nil || !reflect.DeepEqual(questions[0].Draft.Selected, []string{"two"}) || questions[0].Draft.Text != "Exact answer <&> 한글\nnext line" {
		t.Fatal("resolved projection lost answer", string(projected))
	}
}

func TestAssistantRunContinuationCheckpointCustody(t *testing.T) {
	// Handwritten optional members exercise full-copy custody, including exact typed
	// facts and child/source/reasoning accounting. No provider result is recorded.
	raw := `{"version":1,"id":"execution-one","requestKey":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","phase":"work","unit":"model-requests","used":1,"limit":20,"grants":[],"status":"complete","plan":[],"explanation":"retained","reason":"question","testCalls":3,"operations":[{"id":"read-one","name":"validate","args":{"missing":null,"flag":false,"decimal":1.20},"status":"complete","result":{"content":[{"type":"text","text":"exact"}],"structuredContent":{"flag":false,"missing":null}}}],"context":{"scope":"fixture","entries":[{"id":"fixture:exact","kind":"tool","label":"Retained facts","digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","bytes":40,"revision":"source-v1","value":{"missing":null,"flag":false,"decimal":1.20}}]},"prompts":[{"request":1,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}],"sourceIdentities":[{"source":"fixture","identityDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","withheld":false,"serviceAsked":false}],"sources":[{"source":"fixture","kind":"custom","calls":1,"refused":0,"proposed":0,"bytes":40,"milliseconds":3}],"policy":{"models.author.reasoning":{"value":"medium","layer":"run","source":"layer"}},"compaction":{"mode":"index","summaries":[]},"outputGuard":{"private":1},"draft":{"documents":[]},"usage":{"rows":[{"source":"api","connection":"fixture","role":"author","model":"shared-model","requests":1,"invocations":1,"milliseconds":3,"bytes":40}]},"childUsage":[{"id":"child-one","usage":{"rows":[{"source":"api","connection":"fixture","role":"child","model":"child-model","requests":1,"invocations":1,"milliseconds":2,"bytes":20}]}}],"failover":[{"role":"author","connection":"fixture","history":[],"consumed":0}]}`
	value, err := runcontext.ParseJSON([]byte(raw))
	assistantRunMust(t, err)
	value, err = runcontext.WriteRecord(runcontext.CheckpointSchema, value, runcontext.RecordDefaults, nil, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC), "2099-01-01T00:00:00Z")
	assistantRunMust(t, err)
	cp, err := runcontext.ReadExecution(value)
	assistantRunMust(t, err)
	run := assistantRunRecord{Binding: assistantRunBinding{ExecutionKey: cp.RequestKey, Phase: cp.Phase, Unit: cp.Unit}, Checkpoint: cp}
	reopened, err := reopenAssistantRunQuestionCheckpoint(run)
	assistantRunMust(t, err)
	if reopened.Status != "running" || reopened.Digest == nil || *reopened.Digest == *cp.Digest {
		t.Fatal("explicit running transition/digest missing")
	}
	if cp.Status != "complete" {
		t.Fatal("original checkpoint mutated")
	}
	left, right := *cp, *reopened
	left.Status = right.Status
	left.Digest = right.Digest
	if !reflect.DeepEqual(left, right) {
		t.Fatal("answer altered optional checkpoint member")
	}
	run.Checkpoint = reopened
	run.Checkpoint.Status = "paused"
	// Rehash the fixture's status transition before canonical ExtendExecution.
	encoded, _ := json.Marshal(run.Checkpoint)
	v, _ := runcontext.ParseJSON(encoded)
	digest := runcontext.RecordDigest(v)
	run.Checkpoint.Digest = &digest
	extended, err := runcontext.ExtendExecution(*run.Checkpoint, false, time.Date(2026, 10, 23, 0, 1, 0, 0, time.UTC))
	assistantRunMust(t, err)
	left, right = *run.Checkpoint, *extended
	left.Status = right.Status
	left.Digest = right.Digest
	left.Limit = right.Limit
	left.Grants = right.Grants
	left.Reason = right.Reason
	if !reflect.DeepEqual(left, right) {
		t.Fatal("grant altered optional checkpoint member")
	}
}

func TestAssistantRunContinuationSharedVectors(t *testing.T) {
	for _, name := range []string{"answer", "budget"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "assistant", "serverRuns", "fixtures", "continuation-"+name+".json"))
			assistantRunMust(t, err)
			var view assistantRunView
			assistantRunMust(t, json.Unmarshal(raw, &view))
			var wire struct {
				Continuations []json.RawMessage `json:"continuations"`
			}
			assistantRunMust(t, json.Unmarshal(raw, &wire))
			if len(wire.Continuations) != 1 {
				t.Fatal("vector receipt missing")
			}
			receipt, err := readAssistantRunContinuation(wire.Continuations[0])
			assistantRunMust(t, err)
			run := assistantRunRecord{State: view.State, Events: view.Events, Checkpoint: view.Checkpoint, Actions: []assistantRunActionReceipt{{ID: receipt.ID, Digest: receipt.Digest, Kind: receipt.Kind, Answer: receipt.Answer, Grant: receipt.Grant}}}
			assistantRunMust(t, validateAssistantRunActionReceipts(run))
			fields, err := assistantTestObject(wire.Continuations[0])
			assistantRunMust(t, err)
			for key := range fields {
				copy := map[string]json.RawMessage{}
				for k, v := range fields {
					if k != key {
						copy[k] = v
					}
				}
				encoded, _ := json.Marshal(copy)
				if _, err := readAssistantRunContinuation(encoded); err == nil {
					t.Fatal("receipt omission accepted", key)
				}
			}
			nested := "answer"
			if name == "budget" {
				nested = "grant"
			}
			child, err := assistantTestObject(fields[nested])
			assistantRunMust(t, err)
			for key := range child {
				copy := map[string]json.RawMessage{}
				for k, v := range child {
					if k != key {
						copy[k] = v
					}
				}
				modified, _ := json.Marshal(copy)
				outer := map[string]json.RawMessage{}
				for k, v := range fields {
					outer[k] = v
				}
				outer[nested] = modified
				encoded, _ := json.Marshal(outer)
				if _, err := readAssistantRunContinuation(encoded); err == nil {
					t.Fatal("nested omission accepted", key)
				}
			}
		})
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "assistant", "serverRuns", "fixtures", "continuation-actions.json"))
	assistantRunMust(t, err)
	var actions []json.RawMessage
	assistantRunMust(t, json.Unmarshal(raw, &actions))
	for _, raw := range actions {
		r := httptest.NewRequest("POST", "/", strings.NewReader(string(raw)))
		var a assistantRunAction
		assistantRunMust(t, assistantRunRequest(r, &a))
		assistantRunMust(t, validateAssistantRunActionShape(a))
	}
}

func TestAssistantRunContinuationConcurrentDuplicate(t *testing.T) {
	for _, kind := range []string{"answer", "extend-budget"} {
		t.Run(kind, func(t *testing.T) {
			s, token, paused, executor := assistantContinuationFixture(t, kind, nil)
			release := make(chan struct{})
			entered := make(chan struct{}, 2)
			defer close(release)
			executor.execute = func(ctx context.Context, e *assistantRunExecution) error {
				entered <- struct{}{}
				select {
				case <-release:
					return assistantProtocolComplete(ctx, e)
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			a := assistantContinuationAction(paused, kind)
			ready := make(chan struct{})
			responses := make(chan *httptest.ResponseRecorder, 2)
			for range 2 {
				go func() {
					<-ready
					responses <- policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", a)
				}()
			}
			close(ready)
			first := assistantProtocolView(t, <-responses, 200)
			second := assistantProtocolView(t, <-responses, 200)
			if first.Revision != second.Revision || len(first.Continuations) != 1 || len(second.Continuations) != 1 {
				t.Fatal("concurrent duplicate changed receipt/revision")
			}
			h := s.assistantRunService()
			h.mu.Lock()
			active := h.active[paused.ID] != nil
			h.mu.Unlock()
			if !active {
				t.Fatal("continuation not active")
			}
			// calls increments before Execute's bounded block. Observe through the same
			// active execution boundary instead of timing-dependent request sleeps.
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("continuation did not enter")
			}
			if executor.calls.Load() != 2 {
				t.Fatal("duplicate launched more than one continuation", executor.calls.Load())
			}
			if kind == "extend-budget" && len(first.Checkpoint.Grants) != 1 {
				t.Fatal("duplicate compute grant")
			}
		})
	}
}
func TestAssistantRunContinuationSemanticReceiptGuards(t *testing.T) {
	for _, name := range []string{"answer", "budget"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "assistant", "serverRuns", "fixtures", "continuation-"+name+".json"))
			assistantRunMust(t, err)
			var view assistantRunView
			assistantRunMust(t, json.Unmarshal(raw, &view))
			r := view.Continuations[0]
			run := assistantRunRecord{State: view.State, Checkpoint: view.Checkpoint, Events: view.Events, Actions: []assistantRunActionReceipt{{ID: r.ID, Digest: r.Digest, Kind: r.Kind, Answer: r.Answer, Grant: r.Grant}}}
			if name == "answer" {
				run.Actions[0].Answer.QuestionDigest = strings.Repeat("a", 64)
				if validateAssistantRunActionReceipts(run) == nil {
					t.Fatal("answer digest must match retained question")
				}
			} else {
				run.Actions[0].Grant.To++
				if validateAssistantRunActionReceipts(run) == nil {
					t.Fatal("grant receipt must match canonical checkpoint")
				}
			}
		})
	}
}

func TestAssistantRunContinuationMalformedScalars(t *testing.T) {
	prefix := `{"id":"answer","kind":"answer","revision":1,"questionId":"question-one","questionDigest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","answer":{"selected":[],"text":`
	for _, value := range []string{`"\ud800"`, `"\udc00"`, `"\ud800x"`, `"` + string([]byte{0xff}) + `"`} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(prefix+value+`}}`))
		var action assistantRunAction
		if assistantRunRequest(r, &action) == nil {
			t.Fatal("malformed scalar answer repaired instead of refused")
		}
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(prefix+`"\ud83d\ude42"}}`))
	var action assistantRunAction
	assistantRunMust(t, assistantRunRequest(r, &action))
	if action.Answer == nil || action.Answer.Text == nil || *action.Answer.Text != "🙂" {
		t.Fatal("valid scalar pair changed")
	}
}

func TestAssistantRunContinuationDigestPayloadCompatibility(t *testing.T) {
	for _, continued := range []bool{false, true} {
		for _, outputs := range [][]assistantDomainOutput{nil, {}, {{Kind: "graph", RunID: "run", RequestKey: "request", OperationID: "operation", CallID: "call"}}} {
			run := assistantRunRecord{State: "complete", Events: []assistantRunEvent{{ID: "message", Event: contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: "Retained <&> 한글"}}}}, DomainOutputs: outputs}
			if continued {
				run.Actions = []assistantRunActionReceipt{{ID: "budget", Digest: strings.Repeat("a", 64), Kind: "extend-budget", Grant: &contract.CheckpointGrant{From: 20, To: 30, At: "2026-10-23T00:00:00Z"}}}
			}
			// This is the pre-factoring wire formula, written separately to compare bytes
			// as well as the persisted digest. The production base is built only once.
			var before []byte
			if continued {
				before, _ = json.Marshal(struct {
					State         string
					Checkpoint    any
					Events        []assistantRunEvent
					Outputs       []assistantDomainOutput
					Continuations []assistantRunContinuationReceipt
				}{run.State, assistantChatProjectionCheckpoint(run), run.Events, run.DomainOutputs, assistantRunContinuations(run)})
			} else if len(outputs) == 0 {
				before, _ = json.Marshal(struct {
					State      string
					Checkpoint any
					Events     []assistantRunEvent
				}{run.State, assistantChatProjectionCheckpoint(run), run.Events})
			} else {
				before, _ = json.Marshal(struct {
					State      string
					Checkpoint any
					Events     []assistantRunEvent
					Outputs    []assistantDomainOutput
				}{run.State, assistantChatProjectionCheckpoint(run), run.Events, run.DomainOutputs})
			}
			after := assistantChatEventDigestPayload(run)
			if string(before) != string(after) || assistantChatEventDigest(run) != digestOf(before) {
				t.Fatalf("digest wire changed: before=%s after=%s", before, after)
			}
		}
	}
}
