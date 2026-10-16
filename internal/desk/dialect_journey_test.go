package desk

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real host research scheduling, source verification, generated-data review,
// mapping preparation, pack matrix, release tests, Job and verification export.
// Only the AI and public website content are deterministic fixtures.
func TestDialectCompanyPeopleTestedJobJourney(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) { testDialectResearchTestedJobJourney(t, backend, false) })
	}
}
func TestDialectProcurementTestedJobJourney(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) { testDialectResearchTestedJobJourney(t, backend, true) })
	}
}
func testDialectResearchTestedJobJourney(t *testing.T, backend string, generic bool) {
	rootFact, field, fieldType := "contact", "role", "string"
	var value, other any = "Operations director", "Other role"
	parentName, childName := "Example company", "Alex Example"
	childURL, childText := "https://example.com/team/alex", "Alex Example is Operations director at Example."
	parentKind, childKind := "company", "person"
	if generic {
		rootFact, field, fieldType = "notice", "is_open", "boolean"
		value, other = true, false
		parentName, childName = "Example procurement agency", "Open imaging procurement"
		childURL, childText = "https://example.com/notices/imaging", "The imaging procurement is open for submissions."
		parentKind, childKind = "record", "record"
	}
	target := "/" + rootFact + "/" + field
	bundle, runner, runtime := os.Getenv("JPS_TEST_GATEWAY_BUNDLE"), os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bundle == "" || runner == "" || runtime == "" {
		t.Skip("requires installed Gateway, Runner and Runtime")
	}
	project, config := t.TempDir(), t.TempDir()
	os.Chmod(config, 0700)
	packBytes, err := os.ReadFile("../../web/src/packs/__fixtures__/minimal.pack.json")
	if err != nil {
		t.Fatal(err)
	}
	var pack map[string]any
	json.Unmarshal(packBytes, &pack)
	pack["title"] = "Professional contact shortlist"
	if generic {
		pack["title"] = "Open procurement shortlist"
	}
	pack["rules"] = []any{
		map[string]any{"id": "operations", "description": "Public operations role", "when": map[string]any{"op": "fact", "path": target, "operator": "equals", "value": value}, "outcome": "proceed", "onUnknown": "escalate"},
	}
	pack["fallbackOutcome"] = "hold"
	packBytes, _ = json.Marshal(pack)
	cases := []any{}
	for _, c := range []struct {
		id, outcome string
		value       any
	}{{"matching", "proceed", value}, {"nonmatching", "hold", other}} {
		cases = append(cases, map[string]any{"id": c.id, "facts": map[string]any{rootFact: map[string]any{field: c.value}}, "expectedDisposition": map[string]any{"kind": "outcome", "outcomeId": c.outcome, "reasons": []any{}, "handoff": map[string]string{"state": "none"}}})
	}
	cases = append(cases, map[string]any{"id": "missing-role", "facts": map[string]any{}, "expectedDisposition": map[string]any{"kind": "unresolved", "reasons": []string{"unknown"}, "handoff": map[string]string{"state": "none"}}})
	matrix, _ := json.Marshal(map[string]any{"matrixVersion": "3", "cases": cases})
	writeProject(t, project, map[string]string{"contacts.pack.json": string(packBytes), "contacts.matrix.json": string(matrix), "jpack.json": `{"configVersion":"6","packs":{"contacts":{"path":"contacts.pack.json","matrix":"contacts.matrix.json"}}}`})
	s, ts := startDesk(t, Config{Static: os.DirFS("../../web/dist"), RunnerBin: runner, JpackBin: runtime, LocalGatewayBundle: bundle, ProjectDir: project, DeskConfigDir: config, Token: testToken})
	defer ts.Close()
	defer s.Close()
	s.inputResearchCancel()
	<-s.inputResearchDone
	if backend == "memory" {
		s.inputEvaluationCancel()
		<-s.inputEvaluationDone
		s.inputPreparations = &memoryPreparationRepository{data: map[string][]byte{}}
		s.retainedArtifacts = newMemoryArtifacts()
		s.researchJournals = newMemoryResearchJournals()
		s.evaluationJournals = newMemoryEvaluationJournals()
		s.startInputEvaluations()
	}
	call := func(method, path string, body any, revision string) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		for tries := 0; ; tries++ {
			req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
			req.Header.Set("Authorization", "Bearer "+testToken)
			req.Header.Set("If-Match", revision)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			if w.Code == 409 && tries < 30 && (strings.Contains(w.Body.String(), "busy") || strings.Contains(w.Body.String(), "being saved")) {
				time.Sleep(25 * time.Millisecond)
				continue
			}
			if w.Code >= 300 {
				t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
			}
			return w.Body.Bytes()
		}
	}
	discover := func(name, kind, url, text, field, fieldType string, value any, companies *inputPreparationReply) inputPreparationReply {
		t.Helper()
		recipe := researchRecipeFixture()
		recipe.Target = "companies"
		recipe.Fields = []inputResearchField{{Name: field, Description: field, Type: fieldType}}
		recipe.Limits = inputResearchLimits{Records: 1, Pages: 1, ModelCalls: 6}
		if companies != nil {
			recipe.Target = "people-at-companies"
			recipe.CompanyInputs = []inputResearchCompanyRef{{PreparationID: companies.Document.ID, Revision: companies.SHA256, EntityID: companies.Document.Records[0].Research.Entity.ID}}
		}
		if generic {
			recipe.Target = "records"
			recipe.RequireParents = companies != nil
		}
		var saved inputPreparationReply
		json.Unmarshal(call("POST", "/api/input-preparations", map[string]any{"name": name, "recipe": inputRecipe{Intent: "Fixture public research", Research: &recipe}}, ""), &saved)
		root, e := s.inputResearchRoot(true)
		if e != nil {
			t.Fatal(e)
		}
		defer root.Close()
		_, run := researchRunFixture(t)
		run.ID = "research_" + digestOf([]byte(name))[:32]
		run.PreparationID = saved.Document.ID
		run.RelationshipVersion = 1
		run.ContractVersion = 2
		run.IdentityVersion = 2
		run.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		run.Recipe = recipe
		run.Seeds = []string{url}
		source, proof, pin := handoffProofFixtureFor(t, filepath.Join(bundle, "gateway"), url, text)
		run.Gateway = pin
		pr, e := s.preparationRepository().Read(t.Context())
		if e != nil {
			t.Fatal(e)
		}
		run.Companies, e = selectedResearchCompanies(pr, saved.Document, recipe)
		pr.Close()
		if e != nil {
			t.Fatal(e)
		}
		rr, e := openInputResearch(root, run.ID, true)
		if e != nil {
			t.Fatal(e)
		}
		defer rr.Close()
		if e = s.researchJournal(rr, run.ID).Create(t.Context(), run); e != nil {
			t.Fatal(e)
		}
		ops := recoveryOperations()
		ops.publish = s.publishResearchRecords
		ops.source = func(ctx context.Context, current inputResearchRun, a inputResearchAttempt, _ json.RawMessage) (inputResearchSource, error) {
			var retained inputResearchProof
			if err := json.Unmarshal(proof, &retained); err != nil {
				return inputResearchSource{}, err
			}
			source.ID = a.ID
			size, err := s.saveResearchProof(ctx, rr, a.ID, retained, current.RetrievedBytes)
			source.Bytes = size
			return source, err
		}
		ops.model = func(_ context.Context, _ inputResearchAI, _ string, _ func(bool) error) (json.RawMessage, error) {
			current, e := s.researchJournal(rr, run.ID).Read(t.Context())
			if e != nil {
				return nil, e
			}
			ref := researchURLReferences(current)[0].Ref
			if len(current.Sources) == 0 {
				return recoveryAction("read", map[string]string{"urlRef": ref}), nil
			}
			if len(current.Records) == 0 {
				entity := map[string]any{"kind": kind, "name": name, "urlRef": ref, "fields": map[string]any{field: map[string]any{"status": "supported", "value": value, "sources": []any{map[string]string{"sourceId": source.ID, "quote": text}}}}}
				if companies != nil {
					entity["companyId"] = run.Companies[0].ID
				}
				return recoveryAction("records", map[string]any{"records": []any{entity}}), nil
			}
			return recoveryAction("finish", map[string]string{"reason": "Record goal reached."}), nil
		}
		executeInputResearch(context.Background(), s.researchRunStore(rr, run.ID), run, ops)
		result, e := s.researchJournal(rr, run.ID).Read(t.Context())
		if e != nil || result.State != "completed" || len(result.Records) != 1 {
			t.Fatalf("research: %s %s, records %d, %v", result.State, result.Problem, len(result.Records), e)
		}
		json.Unmarshal(call("GET", "/api/input-preparations/"+saved.Document.ID, nil, ""), &saved)
		if len(saved.Document.Records) != 1 || saved.Document.Records[0].Class != "generated" {
			t.Fatal("research not published as generated")
		}
		// Re-publishing a recovered checkpoint must not duplicate a contact.
		if e = s.publishResearchRecords(result); e != nil {
			t.Fatal(e)
		}
		again, e := s.preparationRepository().Get(t.Context(), saved.Document.ID)
		if e != nil {
			t.Fatal(e)
		}
		if len(again.Document.Records) != 1 {
			t.Fatal("replay duplicated a record")
		}
		return saved
	}
	companies := discover(parentName, parentKind, "https://example.com/", "Example operates in CA.", "country", "text", "CA", nil)
	contacts := discover(childName, childKind, childURL, childText, field, fieldType, value, &companies)
	contact := contacts.Document.Records[0]
	if contact.Research.Entity.CompanyID != companies.Document.Records[0].Research.Entity.ID || contact.Values["company_name"] != parentName {
		t.Fatal("company lineage missing")
	}
	q := &inputQualification{Kind: "pack", Key: "contacts", Pack: string(packBytes), Digest: sha256Digest(packBytes), Mapping: inputRecordDialect{Version: 2, Case: &inputCaseDialect{Facts: []inputDialectFact{{Target: target, Source: "/values/" + field}}, Evidence: []inputDialectEvidence{}}, Sources: []json.RawMessage{}}}
	if generic {
		q.Conversions = map[string]inputConversion{target: {Kind: "research-value"}}
	}
	contacts.Document.Recipe.Qualification = q
	path := "/api/input-preparations/" + contacts.Document.ID
	json.Unmarshal(call("PUT", path, map[string]any{"name": contacts.Document.Name, "recipe": contacts.Document.Recipe}, contacts.SHA256), &contacts)
	contacts = acceptFixture(t, s, contacts)
	publication := checkAndPublishDialect(t, s, contacts, "generated", "1.0.0")
	if script := os.Getenv("JPS_DIALECT_JOB_BROWSER"); script != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "node", script, ts.URL, testToken, contacts.Document.ID)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("browser: %s %v", out, err)
		} else {
			t.Log(string(out))
		}
	}

	rows, e := prepareInputSelection(contacts.Document, []string{contact.ID})
	if e != nil || len(rows) != 1 {
		t.Fatal("conversion", e, rows)
	}
	preparedValue, _ := json.Marshal(rows[0].Prepared[target])
	wantValue, _ := json.Marshal(value)
	if string(preparedValue) != string(wantValue) {
		t.Fatalf("typed value changed: %s want %s", preparedValue, wantValue)
	}
	var batch inputEvaluation
	json.Unmarshal(call("POST", path+"/evaluations", map[string]any{"recordIds": []string{contact.ID}}, contacts.SHA256), &batch)

	if !batch.RequireTested || batch.JobID != "" || batch.ConfirmedAt != "" {
		t.Fatal("test gate missing or review executed work")
	}
	call("POST", path+"/evaluations/"+batch.ID+"/confirm", map[string]bool{"reviewed": true}, contacts.SHA256)
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
		root, e := s.inputEvaluationRoot(false)
		if e != nil {
			t.Fatal(e)
		}
		dir, e := openInputEvaluation(root, batch.ID, false)
		if e != nil {
			t.Fatal(e)
		}
		snapshot, readErr := s.evaluationJournal(dir, batch.ID).Read(t.Context())
		batch, e = snapshot.Document, readErr
		dir.Close()
		root.Close()
		if e != nil {
			t.Fatal(e)
		}
		if batch.State == "completed" || batch.State == "blocked" || batch.State == "partial" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if batch.State != "completed" || len(batch.Rows) != 1 {
		t.Fatalf("Job failed %+v", batch)
	}
	raw, e := s.inputRunner(context.Background(), "runs/"+batch.Rows[0].RunID+"/verification", nil, "")
	if e != nil {
		t.Fatal(e)
	}
	for _, part := range []string{publication.Digest, `"formatVersion":"3.0.0"`, `"class":"generated"`, `"outcomeId":"proceed"`, contact.Research.Entity.CompanyID, `"company_name":"` + parentName + `"`, `"modelResponses"`} {
		if !strings.Contains(string(raw), part) {
			t.Fatal("verification export missing", part)
		}
	}
	checked := s.checkRunSignature(context.Background(), batch.Rows[0].RunID)
	if checked.Status != "verified" || checked.References != "verified" {
		t.Fatal("verification", checked)
	}
	if backend == "memory" {
		assertNoCollectionFiles(t, s)
		assertNoArtifactFiles(t, s)
		assertNoResearchJournalFiles(t, s)
		assertNoEvaluationFiles(t, s)
	}
	t.Log("parent discovery -> related record discovery -> dedup -> pinned parent lineage -> typed mapping -> review -> tested release -> Job -> verified generated-origin export passed")
}
