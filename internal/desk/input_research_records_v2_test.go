package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func researchV2Fixture(t *testing.T) (*Server, *os.Root, inputResearchRun) {
	t.Helper()
	s, _ := newTestServer(t, false)
	_, run := researchRunFixture(t)
	run.ContractVersion = 2
	run.IdentityVersion = 1
	run.CreatedAt = "2026-10-13T12:00:00Z"
	run.Phase = "finalize"
	run.Recipe.Limits.Records = 3
	run.Recipe.Limits.Searches = 0
	run.Recipe.Limits.Pages = 0
	run.Recipe.Limits.ModelCalls = 5
	run.Sources = []inputResearchSource{{ID: "attempt_" + strings.Repeat("1", 64), Kind: "page", URL: "https://example.com/", URLs: []string{"https://example.com/"}, Text: "Example operates in Canada. Another operates in Canada.", Verification: "verified"}}
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
	pr, err := s.openInputPreparations(true)
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	_, err = writeInputPreparation(pr, inputPreparation{Version: 1, ID: run.PreparationID, Name: "Recovery", RecipeVersion: 1, Recipe: inputRecipe{Intent: "Find companies", Research: &run.Recipe}})
	if err != nil {
		t.Fatal(err)
	}
	return s, rr, run
}
func v2Record(run inputResearchRun, name string) map[string]any {
	return map[string]any{"kind": "company", "name": name, "urlRef": "url_1", "fields": map[string]any{"country": map[string]any{"status": "supported", "value": "Canada", "sources": []any{map[string]any{"sourceId": run.Sources[0].ID, "quote": "operates in Canada."}}}}}
}
func v2Apply(t *testing.T, root *os.Root, records ...map[string]any) inputResearchRun {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"records": records})
	run, err := changeInputResearch(root, func(run *inputResearchRun) error {
		run.Attempts = append(run.Attempts, inputResearchAttempt{ID: "model_" + string(rune('a'+len(run.Attempts))), Kind: "model", State: "completed", Result: recoveryAction("records", map[string]any{"records": records})})
		if err := applyResearchRecords(run, raw); err != nil {
			return err
		}
		recordResearchResults(run)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return run
}
func v2Rows(t *testing.T, s *Server, id string) inputPreparationReply {
	t.Helper()
	root, err := s.openInputPreparations(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	saved, err := readInputPreparation(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}
func TestResearchV2PartialSaveCorrectionAndIdempotentPublication(t *testing.T) {
	s, rr, run := researchV2Fixture(t)
	bad := v2Record(run, "Another")
	bad["urlRef"] = "invented"
	run = v2Apply(t, rr, v2Record(run, "Example"), bad)
	if len(run.Records) != 1 || !researchNeedsCorrection(run) || countResearchOutcomes(run).Pending != 1 {
		t.Fatalf("valid sibling lost: %+v", run.Outcomes)
	}
	if err := s.publishResearchRecords(run); err != nil {
		t.Fatal(err)
	}
	saved := v2Rows(t, s, run.PreparationID)
	if len(saved.Document.Records) != 1 {
		t.Fatal("valid record not published")
	}
	// Stale delivery and a crash between publishing rows and acknowledging the run
	// must both reconcile to the same row and history event.
	if err := s.publishResearchRecords(run); err != nil {
		t.Fatal(err)
	}
	repeated := v2Rows(t, s, run.PreparationID)
	if repeated.SHA256 != saved.SHA256 {
		t.Fatal("replayed publication modified history")
	}
	fixed := v2Record(run, "Another")
	fixed["correctionOf"] = run.Outcomes[1].Key
	run = v2Apply(t, rr, fixed)
	if err := s.publishResearchRecords(run); err != nil {
		t.Fatal(err)
	}
	run, _ = readInputResearch(rr)
	if len(v2Rows(t, s, run.PreparationID).Document.Records) != 2 || countResearchOutcomes(run).Added != 2 {
		t.Fatalf("corrected sibling not counted once: %+v", run.Outcomes)
	}
	if !researchStep(run, 0).Recovered || researchNeedsCorrection(run) {
		t.Fatal("correction not linked to original failure")
	}
	// Same evidence URL may support different companies, never collapse them.
	if run.Records[0].ID == run.Records[1].ID {
		t.Fatal("directory URL merged companies")
	}
}
func TestResearchV2RedirectTitleNeverBecomesWebsite(t *testing.T) {
	_, rr, run := researchV2Fixture(t)
	run.Sources = append(run.Sources, inputResearchSource{ID: "search", Kind: "search", URLs: []string{"https://vertexaisearch.cloud.google.com/grounding-api-redirect/abc"}, Text: `{"title":"coastclaims.com"}`})
	writeInputResearch(rr, run)
	row := v2Record(run, "Coast Claims")
	row["websiteRef"] = "url_2"
	run = v2Apply(t, rr, row)
	if len(run.Records) != 0 || !strings.Contains(run.Outcomes[0].Problem, "not a retained page") {
		t.Fatal("search redirect promoted to website")
	}
	delete(row, "websiteRef")
	run = v2Apply(t, rr, row)
	if len(run.Records) != 1 || run.Records[0].Website != "" || run.Records[0].URL != "https://example.com/" {
		t.Fatal("evidence link was treated as official website")
	}
}
func TestResearchV2SelectedScopeStaleWritesAndUnchangedReview(t *testing.T) {
	s, rr, run := researchV2Fixture(t)
	run = v2Apply(t, rr, v2Record(run, "Example"), v2Record(run, "Another"))
	if err := s.publishResearchRecords(run); err != nil {
		t.Fatal(err)
	}
	saved := v2Rows(t, s, run.PreparationID)
	saved = acceptFixture(t, s, saved)
	original := saved.Document.Records[0]
	other := saved.Document.Records[1]
	selected := researchRecordSnapshots(saved.Document)[0]
	selected.SourceIDs = []string{run.Sources[0].ID}
	run.ID = "research_" + strings.Repeat("c", 32)
	run.Reprocess = true
	run.SourceRuns = map[string]string{run.Sources[0].ID: selected.RunID}
	run.Selection = []researchRecordSnapshot{selected}
	run.Existing = run.Selection
	run.Records = nil
	run.Outcomes = nil
	run.Attempts = nil
	run.Recipe.Limits.Records = 1
	root, _ := s.inputResearchRoot(false)
	defer root.Close()
	rr2, _ := openInputResearch(root, run.ID, true)
	defer rr2.Close()
	writeInputResearch(rr2, run)
	row := v2Record(run, "Example")
	row["recordId"] = other.ID
	rejected := v2Apply(t, rr2, row)
	if len(rejected.Records) != 0 {
		t.Fatal("unselected write accepted")
	}
	// Reset only fixture operation outcomes for the separate valid scenario.
	writeInputResearch(rr2, run)
	row["recordId"] = original.ID
	run = v2Apply(t, rr2, row)
	if err := s.publishResearchRecords(run); err != nil {
		t.Fatal(err)
	}
	current := v2Rows(t, s, run.PreparationID)
	if current.SHA256 != saved.SHA256 || current.Document.Records[0].Review == nil {
		t.Fatal("unchanged research erased review or changed history")
	}
	run, _ = readInputResearch(rr2)
	if countResearchOutcomes(run).Unchanged != 1 {
		t.Fatal("unchanged not counted")
	}
	// Concurrent edits after approval cannot be overwritten or create another row.
	row["fields"].(map[string]any)["country"].(map[string]any)["value"] = "CA"
	run = v2Apply(t, rr2, row)
	pr, _ := s.openInputPreparations(false)
	defer pr.Close()
	doc := current.Document
	doc.Records[0].Name = "Edited in another window"
	doc.Records[0].Review = nil
	writeInputPreparation(pr, doc)
	if err := s.publishResearchRecords(run); err == nil {
		t.Fatal("stale selection overwrote edited record")
	}
	after := v2Rows(t, s, run.PreparationID)
	if after.Document.Records[0].Name != "Edited in another window" || len(after.Document.Records) != 2 {
		t.Fatal("stale save mutated rows")
	}
}
func TestResearchV2DurableLoopCorrectsOnlyFailedRecord(t *testing.T) {
	s, rr, run := researchV2Fixture(t)
	ops := recoveryOperations()
	ops.publish = s.publishResearchRecords
	calls := 0
	ops.source = func(context.Context, inputResearchRun, inputResearchAttempt, json.RawMessage) (inputResearchSource, error) {
		t.Fatal("finalize requested a source")
		return inputResearchSource{}, nil
	}
	ops.model = func(_ context.Context, _ inputResearchAI, prompt string, _ func(bool) error) (json.RawMessage, error) {
		calls++
		if calls == 1 {
			bad := v2Record(run, "Another")
			bad["urlRef"] = "invented"
			return recoveryAction("records", map[string]any{"records": []any{v2Record(run, "Example"), bad}}), nil
		}
		if calls == 2 {
			if len(v2Rows(t, s, run.PreparationID).Document.Records) != 1 || !strings.Contains(prompt, "needs-correction") {
				t.Fatal("partial results not retained before correction")
			}
			current, _ := readInputResearch(rr)
			fixed := v2Record(run, "Another")
			fixed["correctionOf"] = current.Outcomes[1].Key
			return recoveryAction("records", map[string]any{"records": []any{fixed}}), nil
		}
		return recoveryAction("finish", map[string]any{"reason": "Only two candidates are supported."}), nil
	}
	executeInputResearch(context.Background(), fileResearchStore(rr), run, ops)
	run, _ = readInputResearch(rr)
	if calls != 3 || len(run.Records) != 2 || researchNeedsCorrection(run) || !researchStep(run, 0).Recovered {
		t.Fatalf("loop did not recover: %d %+v", calls, run.Outcomes)
	}
}
func TestResearchV2CompanyMatchRequiresNameAndEvidenceHost(t *testing.T) {
	a := inputResearchEntity{Kind: "company", Name: "Shopify Inc.", URL: "https://www.shopify.com/company"}
	b := inputResearchEntity{Kind: "company", Name: "Shopify", URL: "https://shopify.com/"}
	if !researchSameIdentity(a, b) {
		t.Fatal("legal suffix prevented identity match")
	}
	b.Name = "Shopify Subsidiary"
	if researchSameIdentity(a, b) {
		t.Fatal("shared host merged subsidiary")
	}
	b.Name = "Shopify"
	b.URL = "https://directory.example/shops"
	if researchSameIdentity(a, b) {
		t.Fatal("name alone merged ambiguous evidence")
	}
}

func TestResearchV2SelectionCollectsOnlyAssociatedEvidenceAcrossRuns(t *testing.T) {
	bundle := os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	if bundle == "" {
		t.Skip("requires pinned local verifier")
	}
	s, _, run := researchV2Fixture(t)
	s.cfg.LocalGatewayBundle = bundle
	source, proof, pin := handoffProofFixture(t, bundle+"/gateway")
	root, err := s.inputResearchRoot(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	doc := v2Rows(t, s, run.PreparationID).Document
	for i, digit := range []string{"c", "d"} {
		parent := run
		parent.ContractVersion = 0
		parent.ID = "research_" + strings.Repeat(digit, 32)
		parent.State = "completed"
		parent.Gateway = pin
		page := source
		page.ID = "attempt_" + strings.Repeat("9", 64)
		if i == 1 {
			page.At = "2026-10-12T12:00:00Z"
		}
		unrelated := page
		unrelated.ID = "attempt_" + strings.Repeat("e", 64)
		unrelated.URL = "https://unrelated.example/"
		unrelated.URLs = []string{unrelated.URL}
		parent.Sources = []inputResearchSource{page, unrelated}
		rr, _ := openInputResearch(root, parent.ID, true)
		writePrivateData(rr, page.ID+".proof.json", proof)
		writeInputResearch(rr, parent)
		rr.Close()
		entity := inputResearchEntity{ID: "entity-" + digit, Kind: "company", Name: "Example " + digit, URL: source.URL, Fields: map[string]inputResearchValue{"country": {Status: "supported", Value: json.RawMessage(`"CA"`), Sources: []inputResearchCitation{{SourceID: page.ID, Quote: "Example operates in CA."}}}}}
		doc.Records = append(doc.Records, preparedInputRecord{ID: "row-" + digit, Name: entity.Name, Class: "generated", Status: "researched", Research: &inputResearchRecord{RunID: parent.ID, RecipeVersion: 1, Entity: entity}})
		_ = i
	}
	run.ID = "research_" + strings.Repeat("f", 32)
	run.Gateway = pin
	run.Existing = researchRecordSnapshots(doc)
	run.RecipeVersion = 2
	doc.RecipeVersion = 2
	if err = s.prepareResearchReprocessing(context.Background(), root, &run, doc, []string{"row-c", "row-d"}); err != nil {
		t.Fatal(err)
	}
	if len(run.Selection) != 2 || len(run.Sources) != 2 || run.Recipe.Limits.Records != 2 || run.Recipe.Limits.Searches != 0 || run.Recipe.Limits.Pages != 0 || run.Recipe.Search != nil {
		t.Fatal("selection widened evidence or source access")
	}
	if len(run.Selection[1].SourceAliases) != 1 || run.Selection[0].SourceIDs[0] == run.Selection[1].SourceIDs[0] {
		t.Fatal("colliding source observations were combined")
	}
	contextRows := researchSelectedContext(run)
	unchanged := doc.Records[1]
	researchCopy := *unchanged.Research
	researchCopy.RecipeVersion = run.RecipeVersion
	unchanged.Research = &researchCopy
	if !researchUnchangedContent(run, unchanged, contextRows[1].Entity) {
		t.Fatal("rebinding source references invalidated an unchanged record")
	}
	if err = s.writeResearchReview(t.Context(), root, run, nil); err != nil {
		t.Fatal(err)
	}
	child, _ := openInputResearch(root, run.ID, false)
	defer child.Close()
	if err = s.verifyResearchContinuation(context.Background(), child, run); err != nil {
		t.Fatal("copied proof verification", err)
	}
	invalid := run
	invalid.Selection = nil
	if s.prepareResearchReprocessing(context.Background(), root, &invalid, doc, []string{"row-c", "missing"}) == nil {
		t.Fatal("silently skipped ineligible row")
	}
	if s.prepareResearchReprocessing(context.Background(), root, &invalid, doc, []string{"row-c", "row-c"}) == nil {
		t.Fatal("duplicate selection accepted")
	}
}

func TestResearchV2NoRecoveryClaimBeforePublicationAndDistinctNamesakes(t *testing.T) {
	s, rr, run := researchV2Fixture(t)
	run.Seeds = append(run.Seeds, "https://different.example/")
	writeInputResearch(rr, run)
	first := v2Record(run, "Example")
	first["websiteRef"] = "not-retained"
	run = v2Apply(t, rr, first)
	delete(first, "websiteRef")
	run = v2Apply(t, rr, first)
	if researchStep(run, 0).Recovered {
		t.Fatal("claimed recovery before publication")
	}
	if err := s.publishResearchRecords(run); err != nil {
		t.Fatal(err)
	}
	second := v2Record(run, "Example")
	second["urlRef"] = "url_2"
	run = v2Apply(t, rr, second)
	if err := s.publishResearchRecords(run); err != nil {
		t.Fatal(err)
	}
	run, _ = readInputResearch(rr)
	if len(v2Rows(t, s, run.PreparationID).Document.Records) != 2 || countResearchOutcomes(run).Added != 2 {
		t.Fatalf("different companies with the same name were collapsed: %+v", run.Outcomes)
	}
}
func TestResearchV2SelectionCannotAcquireSourcesOrReceiveAnAllowance(t *testing.T) {
	_, _, run := researchV2Fixture(t)
	run.Reprocess = true
	run.Selection = []researchRecordSnapshot{{RecordID: "row", Digest: "snapshot", RunID: run.ID, SourceIDs: []string{run.Sources[0].ID}, Entity: inputResearchEntity{ID: "entity"}}}
	run.Existing = run.Selection
	run.SourceRuns = map[string]string{run.Sources[0].ID: run.ID}
	run.Recipe.Limits.Records = 1
	run.Recipe.Search = nil
	if err := validateResearchRecordScope(run); err != nil {
		t.Fatal(err)
	}
	if researchBudgetProposal(run) != nil {
		t.Fatal("reprocessing offered a source budget increase")
	}
	for _, mutate := range []func(*inputResearchRun){func(r *inputResearchRun) { r.Recipe.Limits.Pages = 1 }, func(r *inputResearchRun) { r.Recipe.Limits.Searches = 1 }, func(r *inputResearchRun) { r.Phase = "save" }, func(r *inputResearchRun) { r.Approval = &inputResearchApproval{} }, func(r *inputResearchRun) { r.Selection = nil }, func(r *inputResearchRun) {
		r.Outcomes = []researchRecordOutcome{{State: "pending", RecordID: "outside"}}
	}} {
		changed := run
		mutate(&changed)
		if validateResearchRecordScope(changed) == nil {
			t.Fatal("scope escalation admitted")
		}
	}
}
func TestResearchV2InstructionsPreserveLegacyRuns(t *testing.T) {
	if researchModelInstructions(`{"contractVersion":2}`) != inputResearchInstructions+researchContextInstructions || researchModelInstructions(`{}`) != legacyResearchInstructions+researchContextInstructions {
		t.Fatal("research contract changed under a legacy run")
	}
}

func TestResearchV2EmptySelectionCannotFallBackToDiscovery(t *testing.T) {
	s, _, run := researchV2Fixture(t)
	for _, body := range []string{`{"recordIds":null}`, `{"recordIds":[]}`, `{"recordIds":"all"}`, `{"continueFrom":"` + run.ID + `"}`} {
		request := httptest.NewRequest("POST", "/api/input-preparations/"+run.PreparationID+"/research", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+testToken)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		s.ServeHTTP(response, request)
		if response.Code != 409 {
			t.Fatalf("invalid scope admitted: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestResearchV2CorrectedRecordRetainsEvaluationHandoff(t *testing.T) {
	bundle := os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	if bundle == "" {
		t.Skip("requires pinned local verifier")
	}
	s, rr, run := researchV2Fixture(t)
	s.cfg.LocalGatewayBundle = bundle
	source, proof, pin := handoffProofFixture(t, bundle+"/gateway")
	run.Sources = []inputResearchSource{source}
	run.Gateway = pin
	run.State = "completed-with-gaps"
	writeInputResearch(rr, run)
	writePrivateData(rr, source.ID+".proof.json", proof)
	row := map[string]any{"name": "Example", "kind": "company", "urlRef": "invalid", "fields": map[string]any{"country": map[string]any{"value": "CA", "status": "supported", "sources": []any{map[string]any{"sourceId": source.ID, "quote": "Example operates in CA."}}}}}
	run = v2Apply(t, rr, row)
	row["correctionOf"] = run.Outcomes[0].Key
	row["urlRef"] = "url_1"
	run = v2Apply(t, rr, row)
	if err := s.publishResearchRecords(run); err != nil {
		t.Fatal(err)
	}
	pr, _ := s.openInputPreparations(false)
	defer pr.Close()
	saved := v2Rows(t, s, run.PreparationID)
	saved.Document.Recipe.Qualification = qualificationFixture()
	writeInputPreparation(pr, saved.Document)
	saved = v2Rows(t, s, run.PreparationID)
	saved = acceptFixture(t, s, saved)
	rows, err := prepareInputSelection(saved.Document, []string{saved.Document.Records[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.reviewedInputSnapshot(context.Background(), saved.Document, saved.Document.Records[0], rows[0]); err != nil {
		t.Fatal("corrected record cannot be evaluated:", err)
	}
	// A locally edited proposal must fail the same handoff.
	run, _ = readInputResearch(rr)
	run.Attempts[1].Result = recoveryAction("finish", map[string]any{})
	writeInputResearch(rr, run)
	if _, err = s.reviewedInputSnapshot(context.Background(), saved.Document, saved.Document.Records[0], rows[0]); err == nil {
		t.Fatal("unbound corrected result admitted")
	}
}

func TestResearchV2LocalPublicationRecoveryDoesNotRepeatAI(t *testing.T) {
	s, rr, run := researchV2Fixture(t)
	ops := recoveryOperations()
	calls := 0
	ops.model = func(context.Context, inputResearchAI, string, func(bool) error) (json.RawMessage, error) {
		calls++
		return recoveryAction("records", map[string]any{"records": []any{v2Record(run, "Example")}}), nil
	}
	ops.publish = func(inputResearchRun) error { return errors.New("temporary local write failure") }
	executeInputResearch(context.Background(), fileResearchStore(rr), run, ops)
	saved, err := readInputResearch(rr)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.PublicationPending || saved.PublicationResumeState != "partial" || saved.State != "blocked" || len(saved.Records) != 1 {
		t.Fatal("retained publication was lost")
	}
	if len(v2Rows(t, s, run.PreparationID).Document.Records) != 0 || countResearchOutcomes(saved).Added != 0 {
		t.Fatal("unpublished record counted as saved")
	}
	if err = s.publishResearchRecords(saved); err != nil {
		t.Fatal(err)
	}
	recovered, _ := readInputResearch(rr)
	if recovered.PublicationPending || recovered.PublicationResumeState != "" || recovered.State != "partial" || countResearchOutcomes(recovered).Added != 1 || calls != 1 {
		t.Fatal("local recovery repeated work or left stale error")
	}
}

func TestResearchV2HTTPReviewAndConfirmationKeepExactSelection(t *testing.T) {
	bundle := os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	if bundle == "" {
		t.Skip("requires pinned local verifier")
	}
	s, rr, parent, _ := approvalFixture(t)
	s.cfg.LocalGatewayBundle = bundle
	source, proof, pin := handoffProofFixture(t, bundle+"/gateway")
	source.ID = "attempt_" + strings.Repeat("1", 64)
	var config map[string]any
	json.Unmarshal([]byte(strings.ReplaceAll(researchDeskFile("http://127.0.0.1:1"), testSignerPublic, pin.Public)), &config)
	config["assistant"] = map[string]any{"engine": "codex", "agent": map[string]any{"provider": "openai", "authMethod": "subscription", "model": "test-model", "tools": []string{}}}
	raw, _ := json.Marshal(config)
	writeDeskConfig(t, s, string(raw))
	registry, err := s.readAIRegistry()
	if err != nil {
		t.Fatal(err)
	}
	parent.Gateway = pin
	parent.State = "completed"
	parent.Sources = []inputResearchSource{source}
	parent.Recipe.AI.Revision = registry.Connections[0].Revision
	writePrivateData(rr, source.ID+".proof.json", proof)
	writeInputResearch(rr, parent)
	pr, _ := s.openInputPreparations(false)
	defer pr.Close()
	saved := v2Rows(t, s, parent.PreparationID)
	saved.Document.Recipe.Research = &parent.Recipe
	for _, name := range []string{"One", "Two"} {
		entity := inputResearchEntity{ID: "entity-" + name, Kind: "company", Name: name, URL: source.URL, Fields: map[string]inputResearchValue{"country": {Status: "missing"}}}
		saved.Document.Records = append(saved.Document.Records, preparedInputRecord{ID: "row-" + name, Name: name, Class: "generated", Status: "researched", Research: &inputResearchRecord{RunID: parent.ID, RecipeVersion: parent.RecipeVersion, Entity: entity}})
	}
	writeInputPreparation(pr, saved.Document)
	saved = v2Rows(t, s, parent.PreparationID)
	request := httptest.NewRequest("POST", "/api/input-preparations/"+parent.PreparationID+"/research", strings.NewReader(`{"recordIds":["row-Two"]}`))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", saved.SHA256)
	response := httptest.NewRecorder()
	s.ServeHTTP(response, request)
	if response.Code != 201 {
		t.Fatalf("review: %d %s", response.Code, response.Body.String())
	}
	var reviewed inputResearchRun
	if err = json.Unmarshal(response.Body.Bytes(), &reviewed); err != nil {
		t.Fatal(err)
	}
	if reviewed.State != "review" || !reviewed.Reprocess || len(reviewed.Selection) != 1 || reviewed.Selection[0].RecordID != "row-Two" || len(reviewed.Attempts) != 0 || reviewed.Recipe.Limits.Pages != 0 {
		t.Fatal("review scope changed or started work")
	}
	denied := budgetHTTP(s, reviewed, "/authorize", reviewed.Revision, `{"chatId":"chat-1","minutes":60}`)
	if denied.Code != 409 {
		t.Fatal("selection became an automatic allowance")
	}
	accepted := budgetHTTP(s, reviewed, "/confirm", reviewed.Revision, `{}`)
	if accepted.Code != 200 {
		t.Fatalf("confirmation: %d %s", accepted.Code, accepted.Body.String())
	}
	if err = json.Unmarshal(accepted.Body.Bytes(), &reviewed); err != nil || reviewed.State != "queued" || len(reviewed.Attempts) != 0 {
		t.Fatal("confirmation dispatched an unreviewed operation")
	}
}
