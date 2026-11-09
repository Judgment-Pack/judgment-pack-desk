package desk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRetainedB4PublicPreparationAndPreviewRemainClosed(t *testing.T) {
	h := retainedRuntimeHarness(t)
	s, ts := newTestServer(t, false)
	s.cfg.JpackBin = h.peer
	q := retainedB4Qualification(t)
	doc := repositoryDocument(20)
	doc.Records = []preparedInputRecord{}
	doc.Recipe.Qualification = q
	saved, err := s.preparationRepository().CompareAndSwap(t.Context(), doc, "")
	if err != nil {
		t.Fatal(err)
	}
	// The private preparation route rejects launch and absent credentials;
	// the session-backed requests below must reach the selected-recipe gate.
	for _, credential := range []string{"", testToken} {
		r := httptest.NewRequest(http.MethodPost, "/api/input-preparations/"+doc.ID+"/prepare", strings.NewReader(`{"recordIds":["row-1"]}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", saved.SHA256)
		if credential != "" {
			r.Header.Set("Authorization", "Bearer "+credential)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized || len(h.calls(t)) != 0 {
			t.Fatal("non-session credential admitted private preparation", w.Code)
		}
	}
	for _, route := range []string{"prepare", "conversion-preview", "evaluations"} {
		body := any(map[string]any{"recordIds": []string{"row-1"}})
		if route == "conversion-preview" {
			body = map[string]any{"qualification": q}
		}
		w := retainedB4Request(t, s, ts, "POST", "/api/input-preparations/"+doc.ID+"/"+route, body, saved.SHA256)
		if w.Code != 422 {
			t.Fatalf("selected %s was not closed: %d %s", route, w.Code, w.Body.Bytes())
		}
	}
	ordinary := repositoryDocument(21)
	old, err := s.preparationRepository().CompareAndSwap(t.Context(), ordinary, "")
	if err != nil {
		t.Fatal(err)
	}
	w := retainedB4Request(t, s, ts, "POST", "/api/input-preparations/"+ordinary.ID+"/conversion-preview", map[string]any{"qualification": q}, old.SHA256)
	if w.Code != 422 || len(h.calls(t)) != 0 {
		t.Fatal("proposed selection entered ordinary preview or Runtime", w.Code)
	}
	if _, err = os.Stat(filepath.Join(s.projectDir, inputEvaluationsDir)); !os.IsNotExist(err) {
		t.Fatal("selected request created evaluation history", err)
	}
	after, err := s.preparationRepository().Get(t.Context(), doc.ID)
	if err != nil || after.SHA256 != saved.SHA256 {
		t.Fatal("closed operation changed the recipe", err)
	}
}

func TestRetainedB4OrdinaryOwnersRejectSelectedBeforeDispatch(t *testing.T) {
	var calls atomic.Int64
	s := retainedD4aHTTP(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) })
	// An unavailable Runtime must never be consulted by these ordinary owners.
	s.cfg.JpackBin = filepath.Join(t.TempDir(), "unavailable-runtime")
	doc := repositoryDocument(22)
	doc.Recipe.Qualification = retainedB4Qualification(t)
	row := inputPreparedRow{ID: "row-1", Origin: "asserted", Input: json.RawMessage(`{"source":{}}`)}
	checks := []struct {
		name string
		call func() error
	}{
		{"record", func() error { _, err := prepareInputRecord(doc, doc.Records[0]); return err }},
		{"selection", func() error { _, err := prepareInputSelection(doc, []string{"row-1"}); return err }},
		{"mapping check", func() error { return s.checkPreparedMappings(t.Context(), doc, []inputPreparedRow{row}) }},
		{"empty handoff", func() error { return s.prepareReviewedInputs(t.Context(), doc, nil) }},
		{"handoff", func() error { return s.prepareReviewedInputs(t.Context(), doc, []inputPreparedRow{row}) }},
		{"retained release", func() error {
			return s.prepareReviewedInputsForRelease(t.Context(), doc, []inputPreparedRow{row}, json.RawMessage(`{}`))
		}},
		{"executable", func() error { _, err := executableDialect(doc, "asserted", "1.0.0"); return err }},
		{"publication", func() error { _, err := s.publishedExecutable(t.Context(), doc, "asserted"); return err }},
		{"frozen executable", func() error { _, err := s.frozenExecutable(t.Context(), doc, reviewedInputRelease{}); return err }},
		{"draft", func() error { return validateDialectDraft("Selected", doc.Recipe) }},
		{"draft create", func() error {
			_, err := s.ensureDialectDraft(t.Context(), dialectDraftRequest{Key: "selected", Recipe: doc.Recipe})
			return err
		}},
		{"draft promote", func() error {
			_, err := s.publishDialectDraft(t.Context(), dialectDraftDocument{ID: "draft_" + strings.Repeat("a", 32), Name: "Selected", Recipe: doc.Recipe})
			return err
		}},
		{"assistant replacement", func() error {
			_, err := assistantDialectQualification(doc, assistantDialectArguments{}, assistantDialectRead{})
			return err
		}},
		{"job", func() error {
			return s.ensureEvaluationJob(t.Context(), &evaluationCheckpoint{}, &inputEvaluation{Recipe: doc.Recipe})
		}},
		{"subscription document", func() error { _, err := s.subscriptionDocument(nil, inputEvaluation{Recipe: doc.Recipe}); return err }},
		{"subscription upgrade", func() error {
			return s.reviewSubscriptionUpgrade(t.Context(), inputPreparationReply{Document: doc}, &inputEvaluation{}, "")
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); retainedContractCode(err) != "capability-unsupported" {
				t.Fatal("selected input reached ordinary owner", err)
			}
		})
	}
	before := inputEvaluation{Recipe: doc.Recipe, State: "queued", Subscription: &inputSubscription{Mode: "new"}}
	after := before
	s.advanceInputSubscription(t.Context(), &evaluationCheckpoint{}, &after)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("selected subscription worker mutated a new intent")
	}
	before.Subscription = nil
	after = before
	s.advanceInputEvaluation(t.Context(), &evaluationCheckpoint{}, &after)
	if !reflect.DeepEqual(before, after) || calls.Load() != 0 {
		t.Fatal("selected worker dispatched or mutated work", calls.Load())
	}
}

func TestRetainedB4OrdinaryBatchUsesItsFrozenRecipe(t *testing.T) {
	var calls atomic.Int64
	job := "job_" + strings.Repeat("a", 32)
	release := "release_" + strings.Repeat("b", 32)
	s := retainedD4aHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/v1/jobs" {
			t.Error("unexpected ordinary job route", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		writeJSON(w, 200, map[string]any{"id": job, "name": "Frozen ordinary", "releaseId": release})
	})
	// The source collection can explicitly change later. It is not the source of
	// the frozen batch's contract. The unchanged job owner must still use b.Recipe.
	current := repositoryDocument(23)
	current.Records = []preparedInputRecord{}
	current.Recipe.Qualification = retainedB4Qualification(t)
	repository := &memoryPreparationRepository{data: map[string][]byte{}}
	if _, err := repository.CompareAndSwap(t.Context(), current, ""); err != nil {
		t.Fatal(err)
	}
	s.inputPreparations = repository
	b := inputEvaluation{Version: 1, ID: "eval_" + strings.Repeat("c", 32), PreparationID: current.ID, Name: "Frozen ordinary", Recipe: inputRecipe{Qualification: qualificationFixture()}, ReleaseID: release, State: "queued", Rows: []inputEvaluationRow{}}
	journals := newMemoryEvaluationJournals()
	checkpoint := evaluationCheckpoint{journal: journals.Batch(b.ID)}
	if err := s.ensureEvaluationJob(t.Context(), &checkpoint, &b); err != nil || b.JobID != job || calls.Load() != 1 {
		t.Fatal("current selected recipe disabled ordinary frozen batch", err, calls.Load())
	}
}

func TestRetainedB4SelectedConfirmationGateAndCompletedReadback(t *testing.T) {
	s, ts := newTestServer(t, false)
	current := repositoryDocument(24)
	current.Records = []preparedInputRecord{}
	current.Recipe.Qualification = retainedB4Qualification(t)
	saved, err := s.preparationRepository().CompareAndSwap(t.Context(), current, "")
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.inputEvaluationRoot(true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, confirmed := range []bool{false, true} {
		suffix := "d"
		if confirmed {
			suffix = "e"
		}
		b := inputEvaluation{Version: 1, ID: "eval_" + strings.Repeat(suffix, 32), PreparationID: current.ID, Recipe: current.Recipe, Revision: saved.SHA256, State: "review", Rows: []inputEvaluationRow{}}
		if confirmed {
			b.ConfirmedAt = "2026-10-29T00:00:00Z"
			b.State = "completed"
		}
		if err = s.createEvaluationReview(t.Context(), root, b); err != nil {
			t.Fatal(err)
		}
		path := "/api/input-preparations/" + current.ID + "/evaluations/" + b.ID + "/confirm"
		w := retainedB4Request(t, s, ts, "POST", path, map[string]any{"reviewed": true}, saved.SHA256)
		if !confirmed && w.Code != 409 {
			t.Fatal("new selected confirmation was not refused", w.Code)
		}
		if confirmed && w.Code != 200 {
			t.Fatal("completed confirmation became unavailable", w.Code, w.Body.String())
		}
		br, err := openInputEvaluation(root, b.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		after, err := s.evaluationJournal(br, b.ID).Read(context.Background())
		br.Close()
		if err != nil || !reflect.DeepEqual(after.Document, b) {
			t.Fatal("confirmation gate rewrote durable state", err)
		}
	}
}
