package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestResearchBudgetPauseApprovalResumesWithoutRepeatingSources(t *testing.T) {
	root, run := researchRunFixture(t)
	run.Recipe.Limits = inputResearchLimits{Records: 2, Pages: 2, ModelCalls: 3}
	run.Seeds = []string{"https://one.example/", "https://two.example/"}
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
			return recoveryAction("records", map[string]any{"records": []any{hardeningRecord(run.Seeds[0], sourceID)}}), nil
		case 3:
			return recoveryAction("finish", map[string]string{"reason": "One company remains; source discovery is closed by the AI limit."}), nil
		case 4:
			if !strings.Contains(prompt, `"approvedLimits":{"records":2,"searches":0,"pages":2,"modelCalls":9}`) || !strings.Contains(prompt, run.Seeds[0]) {
				t.Fatal("approved budget or saved progress missing")
			}
			return recoveryAction("read", map[string]string{"url": run.Seeds[1]}), nil
		case 5:
			return recoveryAction("records", map[string]any{"records": []any{hardeningRecord(run.Seeds[1], sourceID)}}), nil
		default:
			t.Fatal("unexpected model request", models)
			return nil, nil
		}
	}
	ops.source = func(_ context.Context, _ *os.Root, _ inputResearchRun, a inputResearchAttempt, _ json.RawMessage) (inputResearchSource, error) {
		pages++
		if a.URL != run.Seeds[pages-1] {
			t.Fatal("source repeated", a.URL)
		}
		sourceID = a.ID
		return inputResearchSource{ID: a.ID, Kind: "page", URL: a.URL, URLs: run.Seeds, Text: "Canada", Verification: "verified"}, nil
	}
	ops.publish = func(r inputResearchRun) error { published = len(r.Records); return nil }
	executeInputResearch(context.Background(), root, run, ops)
	paused, err := readInputResearch(root)
	if err != nil || paused.State != "paused" || models != 3 || pages != 1 || published != 1 || len(paused.BudgetRequests) != 1 {
		t.Fatal("not safely paused", err, paused.State, models, pages)
	}
	first, _ := json.Marshal(paused.Records[0])
	attempts := len(paused.Attempts)
	if researchApprovedLimits(paused) != run.Recipe.Limits || researchRequestBudget(&paused, "model") == nil {
		t.Fatal("pending approval granted allowance")
	}
	// A restart keeps the pause and the same decision ID.
	recoverInputResearch(&paused)
	if paused.State != "paused" || pendingResearchBudget(paused) == nil {
		t.Fatal("restart lost pause")
	}
	_, err = changeInputResearch(root, func(current *inputResearchRun) error {
		return decideResearchBudget(current, current.BudgetRequests[0].ID, "approve", inputReviewActor{Kind: "local-access"})
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, _ := readInputResearch(root)
	if approved.State != "queued" || approved.Recipe.Limits != run.Recipe.Limits || len(approved.Attempts) != attempts {
		t.Fatal("approval reset history or recipe")
	}
	approved.State = "running"
	writeInputResearch(root, approved)
	executeInputResearch(context.Background(), root, approved, ops)
	done, err := readInputResearch(root)
	if err != nil || done.State != "completed" || len(done.Records) != 2 || published != 2 || models != 5 || pages != 2 {
		t.Fatal("resume failed", err, done.State, models, pages)
	}
	retained, _ := json.Marshal(done.Records[0])
	if string(first) != string(retained) || done.BudgetRequests[0].State != "approved" {
		t.Fatal("saved records or approval lost")
	}
}

func budgetRequestFixture(t *testing.T) inputResearchRun {
	t.Helper()
	_, run := researchRunFixture(t)
	run.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	run.Intent = "Find companies"
	run.State = "completed-with-gaps"
	run.Phase = "finalize"
	run.Step = 5
	run.Recipe.Limits = inputResearchLimits{Records: 10, Pages: 1, ModelCalls: 6}
	for i := 0; i < 5; i++ {
		run.Attempts = append(run.Attempts, inputResearchAttempt{ID: fmt.Sprint("model", i), Kind: "model", State: "completed", Result: recoveryAction("finish", map[string]string{"reason": "More sources are needed."})})
	}
	return run
}
func TestResearchBudgetDeclineCapsAndApprovalIntegrity(t *testing.T) {
	run := budgetRequestFixture(t)
	if !pauseResearchForBudget(&run) {
		t.Fatal("no request")
	}
	id := run.BudgetRequests[0].ID
	if err := decideResearchBudget(&run, id, "decline", inputReviewActor{Kind: "local-access"}); err != nil {
		t.Fatal(err)
	}
	if run.State != "completed-with-gaps" || researchBudgetProposal(run) != nil || researchApprovedLimits(run) != run.Recipe.Limits {
		t.Fatal("decline escalated or asked again")
	}
	if decideResearchBudget(&run, id, "approve", inputReviewActor{}) == nil {
		t.Fatal("old approval replayed")
	}
	for _, change := range []func(*inputResearchRun){
		func(r *inputResearchRun) { r.BudgetRequests[0].Proposed.Records++ },
		func(r *inputResearchRun) { r.BudgetRequests[0].Proposed.ModelCalls = 61 },
		func(r *inputResearchRun) { r.BudgetRequests[0].Proposed.Searches = 1 },
		func(r *inputResearchRun) { r.BudgetRequests[0].Proposed.Pages = 101 },
		func(r *inputResearchRun) { r.BudgetRequests[0].Current.ModelCalls++ },
		func(r *inputResearchRun) { r.ContinuationOf = "research_" + strings.Repeat("a", 32) },
	} {
		candidate := budgetRequestFixture(t)
		pauseResearchForBudget(&candidate)
		change(&candidate)
		if validateResearchBudgets(candidate) == nil {
			t.Fatal("invalid escalation accepted")
		}
	}
	capped := budgetRequestFixture(t)
	capped.Recipe.Limits.ModelCalls = 60
	capped.Attempts = nil
	for i := 0; i < 60; i++ {
		capped.Attempts = append(capped.Attempts, inputResearchAttempt{Kind: "model", State: "completed"})
	}
	if researchBudgetProposal(capped) != nil {
		t.Fatal("asked for unusable allowance at the hard model ceiling")
	}
	interrupted := budgetRequestFixture(t)
	interrupted.State = "interrupted"
	if researchBudgetProposal(interrupted) != nil {
		t.Fatal("budget increase bypasses a provider interruption")
	}
	invalid := budgetRequestFixture(t)
	invalid.Attempts[len(invalid.Attempts)-1].ActionProblem = "invalid quotation"
	if researchBudgetProposal(invalid) != nil {
		t.Fatal("budget increase bypasses invalid evidence")
	}
}

func budgetHTTPFixture(t *testing.T) (*Server, *os.Root, inputResearchRun, string) {
	t.Helper()
	s, _ := newTestServer(t, false)
	if s.inputResearchCancel != nil {
		s.inputResearchCancel()
		<-s.inputResearchDone
	}
	run := budgetRequestFixture(t)
	pr, err := s.openInputPreparations(true)
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	doc := inputPreparation{Version: 1, ID: run.PreparationID, Name: "Test", RecipeVersion: 1, Recipe: inputRecipe{Intent: run.Intent, Research: &run.Recipe}}
	if _, err = writeInputPreparation(pr, doc); err != nil {
		t.Fatal(err)
	}
	saved, err := readInputPreparation(pr, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.inputResearchRoot(true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	rr, err := openInputResearch(root, run.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rr.Close() })
	if err = writeInputResearch(rr, run); err != nil {
		t.Fatal(err)
	}
	return s, rr, run, saved.SHA256
}
func budgetHTTP(s *Server, run inputResearchRun, suffix, revision, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/input-preparations/"+run.PreparationID+"/research/"+run.ID+suffix, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("If-Match", revision)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestResearchBudgetHTTPReviewIsDurableAndDeclineDoesNotNeedProvider(t *testing.T) {
	s, rr, run, revision := budgetHTTPFixture(t)
	w := budgetHTTP(s, run, "/budget-review", revision, `{}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var review inputResearchBudgetReview
	json.Unmarshal(w.Body.Bytes(), &review)
	again := budgetHTTP(s, run, "/budget-review", revision, `{}`)
	var same inputResearchBudgetReview
	json.Unmarshal(again.Body.Bytes(), &same)
	if same.Request.ID != review.Request.ID {
		t.Fatal("refresh created a duplicate decision")
	}
	paused, err := readInputResearch(rr)
	if err != nil || paused.State != "paused" || len(paused.Attempts) != 5 {
		t.Fatal("review started a provider operation", err)
	}
	if w = budgetHTTP(s, run, "/budget-decision", "stale", fmt.Sprintf(`{"requestId":%q,"decision":"approve"}`, review.Request.ID)); w.Code != 409 {
		t.Fatal("stale review approved", w.Code)
	}
	if w = budgetHTTP(s, run, "/budget-decision", revision, fmt.Sprintf(`{"requestId":%q,"decision":"approve","limits":{"modelCalls":100}}`, review.Request.ID)); w.Code != 400 {
		t.Fatal("client supplied its own budget")
	}
	if w = budgetHTTP(s, run, "/budget-decision", revision, `{"requestId":"wrong","decision":"approve"}`); w.Code != 409 {
		t.Fatal("wrong approval accepted")
	}
	w = budgetHTTP(s, run, "/budget-decision", "", fmt.Sprintf(`{"requestId":%q,"decision":"decline"}`, review.Request.ID))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	declined, err := readInputResearch(rr)
	if err != nil || declined.State != "completed-with-gaps" || declined.BudgetRequests[0].Reviewer.Kind != "local-access" {
		t.Fatal("decline not retained", err)
	}
	if budgetHTTP(s, run, "/budget-review", revision, `{}`).Code != 409 {
		t.Fatal("declined request offered again")
	}
}
func TestResearchBudgetConcurrentDecisionsApplyOnlyOnce(t *testing.T) {
	s, rr, run, revision := budgetHTTPFixture(t)
	w := budgetHTTP(s, run, "/budget-review", revision, `{}`)
	var review inputResearchBudgetReview
	json.Unmarshal(w.Body.Bytes(), &review)
	var wg sync.WaitGroup
	responses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			responses <- budgetHTTP(s, run, "/budget-decision", "", fmt.Sprintf(`{"requestId":%q,"decision":"decline"}`, review.Request.ID)).Code
		}()
	}
	wg.Wait()
	close(responses)
	success := 0
	for code := range responses {
		if code == 200 {
			success++
		} else if code != 409 {
			t.Fatal(code)
		}
	}
	saved, err := readInputResearch(rr)
	if err != nil || success != 1 || len(saved.BudgetRequests) != 1 {
		t.Fatal("duplicate decision", success, err)
	}
}

func TestResearchBudgetHTTPApprovesExactRunAndChecksChangedContext(t *testing.T) {
	bundle := os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	if bundle == "" {
		t.Skip("requires pinned local verifier")
	}
	for _, scenario := range []string{"approve", "recipe", "connection", "gateway", "proof", "newer-run"} {
		t.Run(scenario, func(t *testing.T) {
			s, rr, run, _ := budgetHTTPFixture(t)
			s.cfg.LocalGatewayBundle = bundle
			source, proof, pin := handoffProofFixture(t, bundle+"/gateway")
			source.ID = "attempt_" + strings.Repeat("1", 64)
			config := strings.Replace(researchDeskFile("http://127.0.0.1:1"), testSignerPublic, pin.Public, 1)
			var shared map[string]any
			json.Unmarshal([]byte(config), &shared)
			shared["assistant"] = map[string]any{"engine": "codex", "agent": map[string]any{"provider": "openai", "authMethod": "subscription", "model": "test-model", "tools": []string{}}}
			raw, _ := json.Marshal(shared)
			writeDeskConfig(t, s, string(raw))
			registry, err := s.readAIRegistry()
			if err != nil || len(registry.Connections) != 1 {
				t.Fatal("connection fixture", err, len(registry.Connections))
			}
			run.Recipe.AI.Revision = registry.Connections[0].Revision
			run.Gateway = pin
			run.Sources = []inputResearchSource{source}
			if err = writePrivateData(rr, source.ID+".proof.json", proof); err != nil {
				t.Fatal(err)
			}
			if err = writeInputResearch(rr, run); err != nil {
				t.Fatal(err)
			}
			pr, err := s.openInputPreparations(false)
			if err != nil {
				t.Fatal(err)
			}
			defer pr.Close()
			doc, err := readInputPreparation(pr, run.PreparationID)
			if err != nil {
				t.Fatal(err)
			}
			doc.Document.Recipe.Research = &run.Recipe
			if _, err = writeInputPreparation(pr, doc.Document); err != nil {
				t.Fatal(err)
			}
			doc, err = readInputPreparation(pr, run.PreparationID)
			if err != nil {
				t.Fatal(err)
			}
			w := budgetHTTP(s, run, "/budget-review", doc.SHA256, `{}`)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var review inputResearchBudgetReview
			json.Unmarshal(w.Body.Bytes(), &review)
			// Polling/recovery must not dispatch a paused run.
			s.scanInputResearch(context.Background())
			paused, err := readInputResearch(rr)
			if err != nil || paused.State != "paused" || len(paused.Attempts) != 5 {
				t.Fatal("paused run dispatched", err)
			}
			switch scenario {
			case "recipe":
				doc.Document.RecipeVersion++
				writeInputPreparation(pr, doc.Document)
			case "connection":
				writeDeskConfig(t, s, strings.Replace(string(raw), "test-model", "other-model", 1))
			case "gateway":
				writeDeskConfig(t, s, strings.Replace(string(raw), "gateway:test", "gateway:changed", 1))
			case "proof":
				writePrivateData(rr, source.ID+".proof.json", []byte(`{}`))
			case "newer-run":
				root, err := s.inputResearchRoot(false)
				if err != nil {
					t.Fatal(err)
				}
				defer root.Close()
				newer := run
				newer.ID = "research_" + strings.Repeat("f", 32)
				newer.CreatedAt = time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano)
				other, err := openInputResearch(root, newer.ID, true)
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				writeInputResearch(other, newer)
			}
			w = budgetHTTP(s, run, "/budget-decision", doc.SHA256, fmt.Sprintf(`{"requestId":%q,"decision":"approve"}`, review.Request.ID))
			got, err := readInputResearch(rr)
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "approve" {
				if w.Code != 409 || got.State != "paused" || got.BudgetRequests[0].State != "pending" || researchApprovedLimits(got) != run.Recipe.Limits {
					t.Fatal("changed context granted allowance", scenario, w.Code, w.Body.String())
				}
				// The user can still keep results without repairing unrelated settings.
				w = budgetHTTP(s, run, "/budget-decision", "", fmt.Sprintf(`{"requestId":%q,"decision":"decline"}`, review.Request.ID))
				if w.Code != 200 {
					t.Fatal("decline trapped", w.Code, w.Body.String())
				}
				return
			}
			if w.Code != 200 || got.State != "queued" || got.Recipe.Limits != run.Recipe.Limits || researchApprovedLimits(got) != review.Request.Proposed || len(got.Attempts) != 5 || len(got.Sources) != 1 {
				t.Fatal("exact approval failed", w.Code, w.Body.String())
			}
			if budgetHTTP(s, run, "/budget-decision", doc.SHA256, fmt.Sprintf(`{"requestId":%q,"decision":"approve"}`, review.Request.ID)).Code != 409 {
				t.Fatal("approval replayed")
			}
		})
	}
}
func TestResearchBudgetDispatchAndRetriesRespectApprovedLimits(t *testing.T) {
	run := budgetRequestFixture(t)
	pauseResearchForBudget(&run)
	for _, kind := range []string{"model", "page", "search"} {
		if _, _, err := beginResearchAttempt(&run, kind, json.RawMessage(`{"fresh":true}`)); err == nil {
			t.Fatal("paused dispatch", kind)
		}
	}
	if err := decideResearchBudget(&run, run.BudgetRequests[0].ID, "approve", inputReviewActor{Kind: "local-access"}); err != nil {
		t.Fatal(err)
	}
	run.State = "running"
	limit := researchApprovedLimits(run).Pages
	for i := 0; i < limit; i++ {
		a, _, err := beginResearchAttempt(&run, "page", json.RawMessage(fmt.Sprintf(`{"url":"https://example.com/%d"}`, i)))
		if err != nil {
			t.Fatal(err)
		}
		if err = finishResearchAttempt(&run, a.ID, json.RawMessage(`{}`), ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := beginResearchAttempt(&run, "page", json.RawMessage(`{"url":"https://example.com/extra"}`)); err == nil {
		t.Fatal("approved page ceiling bypassed")
	}
	// Failed search attempts still consume their allowance after an increase.
	run.Recipe.Retries = &inputResearchRetryPolicy{Mode: "temporary", MaxRetries: 2}
	run.Recipe.Limits.Searches = 1
	run.BudgetRequests[0].Current.Searches = 1
	run.BudgetRequests[0].Proposed.Searches = 2
	args := json.RawMessage(`{"query":"new"}`)
	a, _, err := beginResearchAttempt(&run, "search", args)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 2; attempt <= 3; attempt++ {
		last := &run.Attempts[len(run.Attempts)-1]
		last.State = "uncertain"
		last.AcquisitionRefusal = true
		last.ProblemCode = "search-temporary"
		run.PendingRetry = &inputResearchPendingRetry{AfterAttempt: a.ID, Number: attempt, MaxAttempts: 3}
		next, err := beginResearchRetry(&run, "search", args, a.ID)
		if attempt == 2 && err != nil {
			t.Fatal(err)
		}
		if attempt == 3 && err == nil {
			t.Fatal("retry exceeded approved search budget")
		}
		a = next
	}
}
