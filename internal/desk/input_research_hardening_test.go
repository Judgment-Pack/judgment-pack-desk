package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

func hardeningRecord(url, source string) map[string]any {
	field := map[string]any{"status": "missing", "sources": []any{}}
	if source != "" {
		field = map[string]any{"status": "supported", "value": "Canada", "sources": []any{map[string]string{"sourceId": source, "quote": "Canada"}}}
	}
	return map[string]any{"kind": "company", "name": "Example", "url": url, "fields": map[string]any{"country": field}}
}
func TestResearchFinalizesAfterPageBudgetAndSavesBeforeFurtherDiscovery(t *testing.T) {
	root, run := researchRunFixture(t)
	run.Recipe.Limits.Pages = 1
	run.Recipe.Limits.ModelCalls = 8
	run.Recipe.Limits.Records = 10
	run.Seeds = append(run.Seeds, "https://example.com/second")
	if err := writeInputResearch(root, run); err != nil {
		t.Fatal(err)
	}
	ops := recoveryOperations()
	models, pages, published := 0, 0, 0
	sourceID := ""
	ops.model = func(_ context.Context, _ inputResearchAI, prompt string, _ func(bool) error) (json.RawMessage, error) {
		models++
		switch models {
		case 1:
			return recoveryAction("read", map[string]string{"url": run.Seeds[0]}), nil
		case 2:
			if !strings.Contains(prompt, `"phase":"finalize"`) {
				t.Fatal("no budget finalization phase")
			}
			return recoveryAction("read", map[string]string{"url": run.Seeds[1]}), nil
		case 3:
			if !strings.Contains(prompt, "reserved for saving") {
				t.Fatal("no correction feedback")
			}
			return recoveryAction("records", map[string]any{"records": []any{hardeningRecord(run.Seeds[0], sourceID)}}), nil
		default:
			return recoveryAction("finish", map[string]string{"reason": "No further candidates were supported by the retained page."}), nil
		}
	}
	ops.source = func(_ context.Context, _ inputResearchRun, a inputResearchAttempt, _ json.RawMessage) (inputResearchSource, error) {
		pages++
		sourceID = a.ID
		return inputResearchSource{ID: a.ID, Kind: "page", URL: run.Seeds[0], URLs: run.Seeds, Text: "Example is based in Canada.", Verification: "verified"}, nil
	}
	ops.publish = func(r inputResearchRun) error { published = len(r.Records); return nil }
	executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
	got, err := readInputResearch(root)
	if err != nil || len(got.Records) != 1 || published != 1 || pages != 1 || models != 4 || got.State != "paused" || got.Problem == "" {
		t.Fatalf("did not finalize: %v %+v calls %d/%d", err, got, models, pages)
	}
}
func TestResearchInvalidBatchCorrectsAtomicallyAndRetainsFeedback(t *testing.T) {
	root, run := researchRunFixture(t)
	run.Recipe.Limits.Records = 2
	run.Seeds = append(run.Seeds, "https://second.example/")
	writeInputResearch(root, run)
	first := hardeningRecord(run.Seeds[0], "")
	second := hardeningRecord(run.Seeds[1], "")
	bad := hardeningRecord(run.Seeds[1], "")
	bad["fields"] = map[string]any{"country": map[string]any{"status": "interpretation", "value": 42, "sources": []any{}}}
	ops := recoveryOperations()
	calls := 0
	partialPublication := false
	ops.model = func(_ context.Context, _ inputResearchAI, prompt string, _ func(bool) error) (json.RawMessage, error) {
		calls++
		if calls == 1 {
			return recoveryAction("records", map[string]any{"records": []any{first, bad}}), nil
		}
		if calls == 2 {
			saved, _ := readInputResearch(root)
			if len(saved.Records) != 0 || !strings.Contains(prompt, "Field country requires a value in its declared string encoding.") {
				t.Fatalf("invalid batch partly committed or no typed correction feedback: records=%d prompt=%s", len(saved.Records), prompt)
			}
			return recoveryAction("records", map[string]any{"records": []any{first, second}}), nil
		}
		return recoveryAction("finish", map[string]string{"reason": "Both candidate identities are saved; country evidence is missing."}), nil
	}
	ops.publish = func(r inputResearchRun) error {
		if len(r.Records) == 1 {
			partialPublication = true
		}
		return nil
	}
	executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
	got, err := readInputResearch(root)
	if err != nil || len(got.Records) != 2 || got.State != "completed-with-gaps" || calls != 3 || partialPublication || got.Attempts[0].ActionProblem == "" {
		t.Fatal("atomic correction failed", err, got.State, calls)
	}
}
func TestResearchEmptyPageContinuesAndDoesNotRepeatRequest(t *testing.T) {
	root, run := researchRunFixture(t)
	run.Seeds = append(run.Seeds, "https://second.example/")
	writeInputResearch(root, run)
	ops := recoveryOperations()
	models, pages := 0, 0
	sourceID := ""
	ops.model = func(_ context.Context, _ inputResearchAI, prompt string, _ func(bool) error) (json.RawMessage, error) {
		models++
		if models <= 2 {
			return recoveryAction("read", map[string]string{"url": run.Seeds[models-1]}), nil
		}
		if models == 3 {
			return recoveryAction("records", map[string]any{"records": []any{hardeningRecord(run.Seeds[1], sourceID)}}), nil
		}
		return recoveryAction("finish", map[string]string{"reason": "One usable candidate; the first source had no text."}), nil
	}
	ops.source = func(_ context.Context, _ inputResearchRun, a inputResearchAttempt, args json.RawMessage) (inputResearchSource, error) {
		pages++
		if pages == 1 {
			return decodeInputResearchSource(inputResearchSource{ID: a.ID, Kind: "page", Verification: "verified"}, args, json.RawMessage(`{"attachmentVersion":"1","content":{"pages":[]},"processing":{"status":"complete"},"provenance":{"source":{"kind":"web","url":"https://example.com/","requestedUrl":"https://example.com/"}}}`))
		}
		sourceID = a.ID
		return inputResearchSource{ID: a.ID, Kind: "page", URL: run.Seeds[1], URLs: run.Seeds, Text: "Canada", Verification: "verified"}, nil
	}
	executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
	got, err := readInputResearch(root)
	if err != nil || pages != 2 || len(got.Records) != 1 || researchSummary(got).UnavailableSources != 1 || got.State != "completed-with-gaps" {
		t.Fatal("empty page stopped work", err, got.State)
	}
}
func TestResearchRepeatedReadAndMalformedActionsAreBounded(t *testing.T) {
	for _, mode := range []string{"repeated", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			root, run := researchRunFixture(t)
			ops := recoveryOperations()
			calls, pages := 0, 0
			ops.model = func(context.Context, inputResearchAI, string, func(bool) error) (json.RawMessage, error) {
				calls++
				if mode == "malformed" {
					return json.RawMessage(`{"tool":"wrong","arguments":{}}`), nil
				}
				return recoveryAction("read", map[string]string{"url": run.Seeds[0]}), nil
			}
			ops.source = func(_ context.Context, _ inputResearchRun, a inputResearchAttempt, _ json.RawMessage) (inputResearchSource, error) {
				pages++
				return inputResearchSource{ID: a.ID, Kind: "page", URLs: run.Seeds, Text: "Canada", Verification: "verified"}, nil
			}
			executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
			got, _ := readInputResearch(root)
			expected := 3
			if mode == "repeated" {
				expected = 4
			}
			if got.State != "partial" || calls != expected || pages > 1 || !strings.Contains(got.Problem, "two corrections") {
				t.Fatal("unbounded loop", got.State, calls, pages)
			}
		})
	}
}
func TestResearchEmptyFinishExplainsShortfall(t *testing.T) {
	root, run := researchRunFixture(t)
	ops := recoveryOperations()
	ops.model = func(context.Context, inputResearchAI, string, func(bool) error) (json.RawMessage, error) {
		return recoveryAction("finish", map[string]any{}), nil
	}
	executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
	got, _ := readInputResearch(root)
	if got.State != "completed-with-gaps" || !strings.Contains(got.Problem, "Saved 0 of up to") {
		t.Fatal("false empty success", got.State, got.Problem)
	}
}
func TestResearchPromptSharesBridgeBoundAndPreservesRetainedText(t *testing.T) {
	_, run := researchRunFixture(t)
	run.Seeds = nil
	for i := 0; i < 80; i++ {
		run.Seeds = append(run.Seeds, fmt.Sprintf("https://example.com/%d/", i)+strings.Repeat("a", 900))
	}
	run.Sources = []inputResearchSource{{ID: "one", Kind: "page", Text: strings.Repeat("<text> ", 20000)}}
	original := run.Sources[0].Text
	prompt, err := researchPrompt(run)
	if err != nil || len(prompt) > codexbridge.MaxPromptBytes || run.Sources[0].Text != original {
		t.Fatal("invalid context", err, len(prompt))
	}
	var context map[string]any
	if json.Unmarshal([]byte(prompt), &context) != nil {
		t.Fatal("invalid prompt JSON")
	}
	if len(context["availableURLs"].([]any)) != 80 {
		t.Fatal("lost source identities")
	}
}
func TestResearchContinuationNeverDispatchesSourcesAndRequiresReview(t *testing.T) {
	root, run := researchRunFixture(t)
	run.Phase = "finalize"
	run.ContinuationOf = "research_" + strings.Repeat("c", 32)
	run.Recipe.Search = nil
	run.Recipe.Limits.Searches = 0
	run.Recipe.Limits.Pages = 0
	run.Sources = []inputResearchSource{{ID: "retained", Kind: "page", Text: "Canada", URL: run.Seeds[0], URLs: run.Seeds, Verification: "verified"}}
	writeInputResearch(root, run)
	ops := recoveryOperations()
	calls := 0
	ops.validateSearch = func(context.Context, inputResearchRecipe) error {
		t.Fatal("continuation depends on search settings")
		return nil
	}
	ops.source = func(context.Context, inputResearchRun, inputResearchAttempt, json.RawMessage) (inputResearchSource, error) {
		t.Fatal("source dispatched in continuation")
		return inputResearchSource{}, nil
	}
	ops.model = func(context.Context, inputResearchAI, string, func(bool) error) (json.RawMessage, error) {
		calls++
		if calls == 1 {
			return recoveryAction("read", map[string]string{"url": run.Seeds[0]}), nil
		}
		if calls == 2 {
			return recoveryAction("records", map[string]any{"records": []any{hardeningRecord(run.Seeds[0], "retained")}}), nil
		}
		return recoveryAction("finish", map[string]string{"reason": "Only one retained candidate."}), nil
	}
	executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
	got, _ := readInputResearch(root)
	if len(got.Records) != 1 || calls != 3 || got.State != "completed-with-gaps" {
		t.Fatal("continuation failed", got.State, calls)
	}
	for _, state := range []string{"review", "queued", "running"} {
		got.State = state
		if researchCanContinue(got) {
			t.Fatal("active run continuable")
		}
	}
}
func TestResearchAdmissionIsExclusiveAndBlocksAcrossInputsWindows(t *testing.T) {
	root, _ := researchRunFixture(t)
	prep := "prep_" + strings.Repeat("d", 32)
	ids := []string{"research_" + strings.Repeat("1", 32), "research_" + strings.Repeat("2", 32)}
	for _, id := range ids {
		rr, _ := openInputResearch(root, id, true)
		writeInputResearch(rr, inputResearchRun{Version: 1, ID: id, PreparationID: prep, State: "review", Recipe: researchRecipeFixture()})
		rr.Close()
	}
	var wg sync.WaitGroup
	admitted := make(chan string, 2)
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			lock, err := root.Open(".")
			if err != nil {
				t.Error(err)
				return
			}
			defer lock.Close()
			if err = lockPrivateData(lock, true); err != nil {
				return
			}
			if (&Server{}).researchInputAvailable(context.Background(), root, prep, id) != nil {
				return
			}
			rr, _ := openInputResearch(root, id, false)
			defer rr.Close()
			if _, err = changeInputResearch(rr, func(r *inputResearchRun) error { r.State = "queued"; return nil }); err != nil {
				t.Error(err)
				return
			}
			admitted <- id
		}(id)
	}
	wg.Wait()
	close(admitted)
	count := 0
	for range admitted {
		count++
	}
	if count != 1 {
		t.Fatal("duplicate admission", count)
	}
}
func TestResearchHTTPRejectsSecondRunBeforeProviderValidation(t *testing.T) {
	s, _ := newTestServer(t, false)
	root, err := s.inputResearchRoot(true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	_, run := researchRunFixture(t)
	rr, _ := openInputResearch(root, run.ID, true)
	defer rr.Close()
	writeInputResearch(rr, run)
	req := httptest.NewRequest("POST", "/api/input-preparations/"+run.PreparationID+"/research", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "already queued or running") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestResearchPublicationKeepsIdentityAndNeverReplaysOlderRun(t *testing.T) {
	s, _ := newTestServer(t, false)
	pr, err := s.openInputPreparations(true)
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	root, _ := s.inputResearchRoot(true)
	defer root.Close()
	_, first := researchRunFixture(t)
	first.IdentityVersion = 1
	first.CreatedAt = "2026-10-13T10:00:00Z"
	first.State = "completed-with-gaps"
	first.Records = []inputResearchEntity{{ID: "entity-first", Kind: "company", Name: "Example", URL: "https://example.com/contact", Fields: map[string]inputResearchValue{"country": {Status: "missing"}}}}
	doc := inputPreparation{Version: 1, ID: first.PreparationID, Name: "test", RecipeVersion: 1, Recipe: inputRecipe{Intent: "Find companies", Research: &first.Recipe}}
	writeInputPreparation(pr, doc)
	persist := func(run inputResearchRun) {
		rr, err := openInputResearch(root, run.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		defer rr.Close()
		if err = writeInputResearch(rr, run); err != nil {
			t.Fatal(err)
		}
	}
	persist(first)
	if err = s.publishResearchRecords(first); err != nil {
		t.Fatal(err)
	}
	old, _ := readInputPreparation(pr, doc.ID)
	id := old.Document.Records[0].ID
	next := first
	next.ID = "research_" + strings.Repeat("e", 32)
	next.CreatedAt = "2026-10-13T11:00:00Z"
	next.Records = append([]inputResearchEntity(nil), first.Records...)
	next.Records[0].URL = "https://www.example.com/about"
	next.Records[0].ID = "entity-second"
	persist(next)
	if err = s.publishResearchRecords(next); err != nil {
		t.Fatal(err)
	}
	if err = s.publishResearchRecords(first); err != nil {
		t.Fatal(err)
	}
	got, _ := readInputPreparation(pr, doc.ID)
	if len(got.Document.Records) != 1 || got.Document.Records[0].ID != id || got.Document.Records[0].Research.RunID != next.ID {
		t.Fatal("duplicate/old record", got.Document.Records)
	}
	personA, personB := first.Records[0], next.Records[0]
	personA.Kind = "person"
	personB.Kind = "person"
	if researchEntityKey(personA) == researchEntityKey(personB) {
		t.Fatal("different person URLs merged")
	}
	different := next.Records[0]
	different.Name = "Example subsidiary"
	if researchEntityKey(different) == researchEntityKey(next.Records[0]) {
		t.Fatal("subsidiary merged by domain")
	}
}
func TestResearchModelInterruptionKeepsSourcesForExplicitContinuation(t *testing.T) {
	root, run := researchRunFixture(t)
	ops := recoveryOperations()
	models := 0
	ops.model = func(context.Context, inputResearchAI, string, func(bool) error) (json.RawMessage, error) {
		models++
		if models == 1 {
			return recoveryAction("read", map[string]string{"url": run.Seeds[0]}), nil
		}
		return nil, errors.New("response lost")
	}
	ops.source = func(_ context.Context, _ inputResearchRun, a inputResearchAttempt, _ json.RawMessage) (inputResearchSource, error) {
		return inputResearchSource{ID: a.ID, Kind: "page", URLs: run.Seeds, Text: "Canada", Verification: "verified"}, nil
	}
	executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
	got, _ := readInputResearch(root)
	if got.State != "interrupted" || models != 2 || !researchSummary(got).CanContinue || len(got.Sources) != 1 {
		t.Fatal("unsafe replay or lost continuation", got.State, models)
	}
}

func TestResearchSixFieldRecipeSavesEarlyAndKeepsOptionalFieldsMissing(t *testing.T) {
	root, run := researchRunFixture(t)
	run.Recipe.Limits = inputResearchLimits{Records: 10, Searches: 10, Pages: 6, ModelCalls: 15}
	run.Recipe.Search = &inputResearchSearch{Connection: "search-test", Revision: "reviewed"}
	run.Recipe.Fields = nil
	for _, name := range []string{"business_description", "fit_signal", "country", "employee_count_evidence", "intake_channels", "routing_complexity_signal"} {
		run.Recipe.Fields = append(run.Recipe.Fields, inputResearchField{Name: name, Description: name, Type: "string"})
	}
	run.Intent = "Find up to three companies in Canada or the United States with at least 50 employees."
	writeInputResearch(root, run)
	ops := recoveryOperations()
	calls, pages := 0, 0
	sourceID := ""
	ops.model = func(_ context.Context, _ inputResearchAI, prompt string, _ func(bool) error) (json.RawMessage, error) {
		calls++
		switch calls {
		case 1:
			return recoveryAction("read", map[string]string{"url": run.Seeds[0]}), nil
		case 2:
			if !strings.Contains(prompt, `"phase":"save"`) || !strings.Contains(prompt, `"maximum":10`) {
				t.Fatal("lost checkpoint or limit authority")
			}
			fields := map[string]any{}
			for _, f := range run.Recipe.Fields {
				fields[f.Name] = map[string]any{"status": "missing", "sources": []any{}}
			}
			fields["country"] = map[string]any{"status": "supported", "value": "Canada", "sources": []any{map[string]string{"sourceId": sourceID, "quote": "Canada"}}}
			return recoveryAction("records", map[string]any{"records": []any{map[string]any{"kind": "company", "name": "Example", "url": run.Seeds[0], "fields": fields}}}), nil
		default:
			return nil, errors.New("AI connection interrupted after candidate save")
		}
	}
	ops.source = func(_ context.Context, _ inputResearchRun, a inputResearchAttempt, _ json.RawMessage) (inputResearchSource, error) {
		pages++
		sourceID = a.ID
		return inputResearchSource{ID: a.ID, Kind: "page", URLs: run.Seeds, Text: "Example operates in Canada.", Verification: "verified"}, nil
	}
	published := 0
	ops.publish = func(r inputResearchRun) error { published = len(r.Records); return nil }
	executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
	got, _ := readInputResearch(root)
	if got.State != "interrupted" || published != 1 || len(got.Records) != 1 || len(got.Records[0].Fields) != 6 || got.Records[0].Fields["employee_count_evidence"].Status != "missing" || pages != 1 {
		t.Fatal("interruption lost saved candidate or invented optional data", got.State, published)
	}
}

func TestResearchContinuationCopiesProofsAndSupportsReviewedPartialEvaluation(t *testing.T) {
	for _, backend := range []string{"legacy", "file", "memory"} {
		t.Run(backend, func(t *testing.T) { testResearchContinuationCopiesProofs(t, backend) })
	}
}
func testResearchContinuationCopiesProofs(t *testing.T, backend string) {
	bundle := os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	if bundle == "" {
		t.Skip("requires pinned local verifier")
	}
	s, _ := newTestServer(t, false)
	s.cfg.LocalGatewayBundle = bundle
	if backend == "memory" {
		s.retainedArtifacts = newMemoryArtifacts()
		s.researchJournals = newMemoryResearchJournals()
	}
	source, proof, pin := handoffProofFixture(t, bundle+"/gateway")
	source.ID = "attempt_" + strings.Repeat("1", 64)
	root, err := s.inputResearchRoot(true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	_, parent := researchRunFixture(t)
	parent.State = "interrupted"
	parent.Gateway = pin
	parent.Sources = []inputResearchSource{source}
	rr, _ := openInputResearch(root, parent.ID, true)
	defer rr.Close()
	if backend == "legacy" {
		if err = writePrivateData(rr, source.ID+".proof.json", proof); err != nil {
			t.Fatal(err)
		}
	} else {
		var retained inputResearchProof
		if err = json.Unmarshal(proof, &retained); err != nil {
			t.Fatal(err)
		}
		size, e := s.saveResearchProof(t.Context(), rr, source.ID, retained, 0)
		if e != nil {
			t.Fatal(e)
		}
		parent.Sources[0].Bytes = size
		snapshot, e := s.loadResearchProof(t.Context(), rr, source.ID)
		if e != nil {
			t.Fatal(e)
		}
		proof = snapshot.Raw
	}
	s.researchJournal(rr, parent.ID).Create(t.Context(), parent)
	if err = s.verifyResearchContinuation(context.Background(), rr, parent); err != nil {
		t.Fatal(err)
	}
	child := parent
	child.ID = "research_" + strings.Repeat("f", 32)
	child.State = "review"
	child.Phase = "finalize"
	child.ContinuationOf = parent.ID
	child.IdentityVersion = 1
	child.CreatedAt = "2026-10-13T12:00:00Z"
	child.Recipe.Limits.Pages = 0
	child.Recipe.Limits.Searches = 0
	if err = s.writeResearchReview(t.Context(), root, child, rr); err != nil {
		t.Fatal(err)
	}
	cr, _ := openInputResearch(root, child.ID, false)
	defer cr.Close()
	copied, _ := readPrivateData(cr, source.ID+".proof.json", inputResearchSourceProofLimit)
	if string(copied) != string(proof) {
		t.Fatal("proof copy changed")
	}
	retained, _ := s.researchJournal(cr, child.ID).Read(t.Context())
	if retained.State != "review" || len(retained.Attempts) != 0 {
		t.Fatal("continuation auto-started")
	}
	child.State = "running"
	s.researchJournal(cr, child.ID).Update(t.Context(), func(r *inputResearchRun) error { r.State = child.State; return nil })
	pr, _ := s.openInputPreparations(true)
	defer pr.Close()
	doc := inputPreparation{Version: 1, ID: child.PreparationID, Name: "Continued", RecipeVersion: 1, Recipe: inputRecipe{Intent: "Find companies", Research: &child.Recipe, Qualification: qualificationFixture()}}
	writeInputPreparation(pr, doc)
	ops := recoveryOperations()
	ops.publish = s.publishResearchRecords
	calls := 0
	ops.model = func(context.Context, inputResearchAI, string, func(bool) error) (json.RawMessage, error) {
		calls++
		if calls == 1 {
			return recoveryAction("records", map[string]any{"records": []any{map[string]any{"kind": "company", "name": "Example", "url": "https://example.com/", "fields": map[string]any{"country": map[string]any{"status": "supported", "value": "CA", "sources": []any{map[string]string{"sourceId": source.ID, "quote": "Example operates in CA."}}}}}}}), nil
		}
		return recoveryAction("finish", map[string]string{"reason": "Only one company was supported."}), nil
	}
	executeInputResearch(context.Background(), s.researchRunStore(cr, child.ID), child, ops)
	saved, _ := readInputPreparation(pr, doc.ID)
	saved = acceptFixture(t, s, saved)
	rows, err := prepareInputSelection(saved.Document, []string{saved.Document.Records[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"completed-with-gaps", "partial"} {
		s.researchJournal(cr, child.ID).Update(t.Context(), func(r *inputResearchRun) error { r.State = state; return nil })
		if _, err = s.reviewedInputSnapshot(context.Background(), saved.Document, saved.Document.Records[0], rows[0]); err != nil {
			t.Fatal("reviewed partial handoff failed", state, err)
		}
	}
	changed := parent
	changed.Sources = append([]inputResearchSource(nil), parent.Sources...)
	changed.Sources[0].Text = "Changed text"
	if s.verifyResearchContinuation(context.Background(), rr, changed) == nil {
		t.Fatal("modified source admitted")
	}
	if err = writePrivateData(rr, source.ID+".proof.json", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if s.verifyResearchContinuation(context.Background(), rr, parent) == nil {
		t.Fatal("modified proof admitted")
	}
	if backend == "memory" {
		assertNoArtifactFiles(t, s)
		assertNoResearchJournalFiles(t, s)
	}
	original, _ := s.researchJournal(rr, parent.ID).Read(t.Context())
	if original.State != "interrupted" || len(original.Records) != 0 {
		t.Fatal("continuation changed original run")
	}
}

func TestResearchEmptySourceProofStillConsumesStorage(t *testing.T) {
	root, _ := researchRunFixture(t)
	proof := []byte(`{"retained":"unusable extraction"}`)
	_, err := researchAttempt(context.Background(), fileResearchStore(root), "page", json.RawMessage(`{"url":"https://example.com/"}`), func(_ context.Context, _ inputResearchRun, a inputResearchAttempt) (json.RawMessage, error) {
		if e := writePrivateData(root, a.ID+".proof.json", proof); e != nil {
			t.Fatal(e)
		}
		return nil, &researchSourceError{code: "source-empty-text", message: "No usable text."}
	})
	if ok, e := continueResearchSource(context.Background(), fileResearchStore(root), err); !ok || e != nil {
		t.Fatal(e)
	}
	run, _ := readInputResearch(root)
	if run.RetrievedBytes != len(proof) {
		t.Fatal("discarded source bytes not counted", run.RetrievedBytes)
	}
}

func TestResearchCompanyIdentityPreservesExistingPeopleRelationships(t *testing.T) {
	_, run := researchRunFixture(t)
	run.IdentityVersion = 1
	record := hardeningRecord("https://example.com/contact", "")
	run.Seeds = []string{"https://example.com/contact", "https://example.com/about"}
	key := researchEntityKey(inputResearchEntity{Kind: "company", Name: "Example", URL: run.Seeds[0]})
	run.EntityIDs = map[string]string{key: "entity-existing-company"}
	run.RowIDs = map[string]string{key: "row-existing-company"}
	raw, _ := json.Marshal(map[string]any{"records": []any{record}})
	if err := applyResearchRecords(&run, raw); err != nil {
		t.Fatal(err)
	}
	record["url"] = run.Seeds[1]
	raw, _ = json.Marshal(map[string]any{"records": []any{record}})
	if err := applyResearchRecords(&run, raw); err != nil {
		t.Fatal(err)
	}
	if len(run.Records) != 1 || run.Records[0].ID != "entity-existing-company" || researchPreparedRecord(run, run.Records[0]).ID != "row-existing-company" {
		t.Fatal("company relationship or row identity changed")
	}
}
