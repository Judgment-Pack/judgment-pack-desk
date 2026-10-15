package desk

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInputConversionsPreserveUnknownAndExplicitValues(t *testing.T) {
	cases := []struct {
		raw  string
		c    inputConversion
		want string
		bad  bool
	}{
		{"0", inputConversion{Kind: "decimal"}, `"0"`, false},
		{"9007199254740993.123", inputConversion{Kind: "decimal"}, `"9007199254740993.123"`, false},
		{"1e2", inputConversion{Kind: "decimal"}, "", true},
		{"50 employees", inputConversion{Kind: "decimal"}, "", true},
		{"", inputConversion{Kind: "decimal"}, `null`, false},
		{"00123", inputConversion{Kind: "text"}, `"00123"`, false},
		{"false", inputConversion{Kind: "boolean", TrueValue: "true", FalseValue: "false"}, `false`, false},
		{"", inputConversion{Kind: "number"}, `null`, false}, {"  ", inputConversion{Kind: "boolean", TrueValue: "yes", FalseValue: "no"}, `null`, false},
		{"0", inputConversion{Kind: "number"}, `0`, false}, {"0.1", inputConversion{Kind: "number"}, `0.1`, false}, {"1e2", inputConversion{Kind: "number"}, `1e2`, false},
		{"00123", inputConversion{Kind: "number"}, "", true}, {"1,000", inputConversion{Kind: "number"}, "", true}, {"1.2K", inputConversion{Kind: "number"}, "", true}, {"9007199254740993", inputConversion{Kind: "number"}, "", true}, {"1.00000000000000001", inputConversion{Kind: "number"}, "", true}, {"0e999999999", inputConversion{Kind: "number"}, "", true}, {"1e-500", inputConversion{Kind: "number"}, "", true}, {"YES", inputConversion{Kind: "boolean", TrueValue: "yes", FalseValue: "no"}, "", true},
	}
	for _, c := range cases {
		t.Run(c.raw+c.c.Kind, func(t *testing.T) {
			v, e := convertInput(c.raw, c.c)
			if (e != nil) != c.bad {
				t.Fatalf("error %v", e)
			}
			if !c.bad {
				raw, _ := json.Marshal(v)
				if string(raw) != c.want {
					t.Fatalf("got %s", raw)
				}
			}
		})
	}
	q := qualificationFixture()
	q.Conversions = map[string]inputConversion{"/missing": {Kind: "number"}}
	if validateInputQualification(q) == nil {
		t.Fatal("orphan conversion accepted")
	}
	q.Conversions = map[string]inputConversion{"/company/country": {Kind: "boolean", TrueValue: "yes", FalseValue: "yes"}}
	if validateInputQualification(q) == nil {
		t.Fatal("ambiguous boolean accepted")
	}
}
func TestPreparedInputsKeepOriginalsAndPerTargetConversion(t *testing.T) {
	q := qualificationFixture()
	q.Mapping.Case.Facts = []inputDialectFact{{"/company/country", "/values/Count"}, {"/count", "/values/Count"}, {"/blank", "/values/Blank"}}
	q.Conversions = map[string]inputConversion{"/count": {Kind: "number"}}
	r := preparedInputRecord{ID: "row", Class: "asserted", SourceID: "source", Values: map[string]string{"Count": "123", "Blank": "", "License": "present"}}
	doc := inputPreparation{ID: "prep", Recipe: inputRecipe{Qualification: q}, RecipeVersion: 2, Records: []preparedInputRecord{r}, Imports: []inputImport{{ID: "source", RecipeVersion: 1}}}
	row, e := prepareInputRecord(doc, r)
	if e != nil || len(row.Issues) > 0 {
		t.Fatal(e, row.Issues)
	}
	if row.Prepared["/company/country"] != "123" || row.Prepared["/count"] != json.Number("123") || len(row.Prepared) != 2 || r.Values["Count"] != "123" || q.Mapping.Case.Facts[0].Source != "/values/Count" {
		t.Fatal("originals or mapping changed", row)
	}
	if !strings.Contains(string(row.Input), `"originalValues"`) || !strings.Contains(string(row.Input), `"importRecipeVersion":1`) {
		t.Fatal("lineage lost")
	}
	if _, e = prepareInputSelection(doc, []string{"row", "row"}); e == nil {
		t.Fatal("duplicate selection accepted")
	}
	r.Class = "generated"
	if _, e = prepareInputRecord(doc, r); e == nil {
		t.Fatal("generated values admitted as assertions")
	}
}
func TestInputEvaluationRealRunnerDurability(t *testing.T) {
	testInputEvaluationRealRunnerDurability(t, false)
}
func TestDeskHomeMigrationRealRunnerDurability(t *testing.T) {
	testInputEvaluationRealRunnerDurability(t, true)
}
func testInputEvaluationRealRunnerDurability(t *testing.T, moveHome bool) {
	if moveHome {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
	}
	bin, runtime := os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bin == "" || runtime == "" {
		t.Skip("requires released Runner and Runtime")
	}
	if moveHome {
		bin = os.Getenv("JPACK_RUNNER_HOME_TEST_BIN")
		if bin == "" {
			t.Skip("set JPACK_RUNNER_HOME_TEST_BIN for the workspace relocation contract")
		}
	}
	project, config := t.TempDir(), t.TempDir()
	os.Chmod(config, 0700)
	if moveHome {
		// Exercise a durable companion that moves with platform storage.
		raw, err := os.ReadFile(bin)
		if err != nil {
			t.Fatal(err)
		}
		bin = filepath.Join(config, "bundled-companions", "jpack-runner")
		if err := os.MkdirAll(filepath.Dir(bin), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bin, raw, 0700); err != nil {
			t.Fatal(err)
		}
	}
	pack, e := os.ReadFile("testdata/graphs/sanctions-screening-0.1.0.pack.json")
	if e != nil {
		t.Fatal(e)
	}
	pack = []byte(strings.ReplaceAll(strings.ReplaceAll(string(pack), `"value": "0"`, `"value": 0`), `"value": "1"`, `"value": 2`))
	pack = []byte(strings.ReplaceAll(string(pack), `"operator": "greater-than-or-equal"`, `"operator": "equals"`))
	writeProject(t, project, map[string]string{"pack.json": string(pack), "jpack.json": `{"configVersion":"2","packs":{"screening":{"path":"pack.json"}}}`})
	cfg := Config{RunnerBin: bin, JpackBin: runtime, ProjectDir: project, DeskConfigDir: config, Token: testToken, RunnerAllowUntestedReleases: true}
	if moveHome {
		cfg.HomeMoveShutdown = func() {}
	}
	s, ts := startDesk(t, cfg)
	defer func() { ts.Close(); s.Close() }()
	// Control the worker to force the exact accepted-but-response-lost window.
	s.inputEvaluationCancel()
	<-s.inputEvaluationDone
	call := func(method, path string, body any, revision string) (int, []byte) {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("If-Match", revision)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	var saved inputPreparationReply
	q := &inputQualification{Kind: "pack", Key: "screening", Pack: string(pack), Digest: sha256Digest(pack), Mapping: inputRecordDialect{Version: 2, Case: &inputCaseDialect{Facts: []inputDialectFact{{"/screening/matches", "/values/Matches"}}, Evidence: []inputDialectEvidence{{"screening-record", "/values/Evidence"}}}, Sources: []json.RawMessage{}}, Conversions: map[string]inputConversion{"/screening/matches": {Kind: "number"}}}
	code, raw := call("POST", "/api/input-preparations", map[string]any{"name": "Screening inputs", "recipe": inputRecipe{Qualification: q}}, "")
	if code != 201 {
		t.Fatal(code, string(raw))
	}
	json.Unmarshal(raw, &saved)
	path := "/api/input-preparations/" + saved.Document.ID
	code, raw = call("POST", path+"/imports", map[string]any{"kind": "csv", "name": "inputs.csv", "content": "Name,Matches,Evidence\nClear,0,present\nHit,2,present\nUnknown,,unknown\nInvalid,many,present\n"}, saved.SHA256)
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	json.Unmarshal(raw, &saved)
	ids := []string{}
	for _, r := range saved.Document.Records {
		ids = append(ids, r.ID)
	}
	code, raw = call("POST", path+"/evaluations", map[string]any{"recordIds": ids}, saved.SHA256)
	if code != 422 {
		t.Fatal("issues did not prevent evaluation", code, string(raw))
	}
	review := func() inputEvaluation {
		t.Helper()
		code, raw = call("POST", path+"/evaluations", map[string]any{"recordIds": ids[:3]}, saved.SHA256)
		if code != 201 {
			t.Fatal("review", code, string(raw))
		}
		var b inputEvaluation
		if json.Unmarshal(raw, &b) != nil {
			t.Fatal("bad review")
		}
		return b
	}
	b := review()
	// Changing an unrelated project setting must withdraw the confirmation too.
	os.WriteFile(filepath.Join(project, "jpack.json"), []byte(`{"configVersion":"2","packs":{"screening":{"path":"pack.json"}},"audit":{"dir":"audit"}}`), 0600)
	code, raw = call("POST", path+"/evaluations/"+b.ID+"/confirm", map[string]any{"reviewed": true}, saved.SHA256)
	if code != 409 {
		t.Fatal("stale project accepted", code, string(raw))
	}
	b = review()
	code, raw = call("POST", path+"/evaluations/"+b.ID+"/confirm", map[string]any{"reviewed": true}, saved.SHA256)
	if code != 202 {
		t.Fatal("confirm", code, string(raw))
	}
	json.Unmarshal(raw, &b)
	// Runner accepted the job and first row. Desk's ledger deliberately has not
	// seen either response, modelling a process death at both commit boundaries.
	jobRaw, e := s.inputRunner(context.Background(), "jobs", map[string]any{"name": b.Name, "releaseId": b.ReleaseID, "reviewed": true}, "")
	if e != nil {
		t.Fatal(e)
	}
	var job struct {
		ID string `json:"id"`
	}
	json.Unmarshal(jobRaw, &job)
	firstRaw, e := s.inputRunner(context.Background(), "jobs/"+job.ID+"/runs", b.Rows[0].Input, b.ID+":"+b.Rows[0].RecordID)
	if e != nil {
		t.Fatal(e)
	}
	var first struct {
		ID string `json:"id"`
	}
	json.Unmarshal(firstRaw, &first)
	// Let the accepted decision finish; only its response and ledger write
	// are lost here. A genuinely interrupted decision must stay interrupted.
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		raw, e := s.inputRunner(context.Background(), "runs/"+first.ID, nil, "")
		if e != nil {
			t.Fatal(e)
		}
		var held struct {
			State string `json:"state"`
		}
		json.Unmarshal(raw, &held)
		if held.State == "completed" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	var migration deskHomeTransaction
	if moveHome {
		plan, err := s.planDeskHome(filepath.Join(t.TempDir(), "jpack-desk"))
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Blockers) > 0 {
			t.Fatal(plan.Blockers)
		}
		if err = s.queueHomeMove(context.Background(), projectMoveRequest{Destination: plan.Destination, Token: plan.Token}); err != nil {
			t.Fatal(err)
		}
		held, err := readHomeTransaction(s.homeMove.journal)
		if err != nil || held == nil {
			t.Fatal("confirmation did not retain its migration", err)
		}
		migration = *held
		cfg.ProjectDir = filepath.Join(plan.Destination, plan.Registry.Desks[0].Folder)
		project = cfg.ProjectDir
		cfg.DeskConfigDir = filepath.Join(plan.Destination, deskHomePlatform)
		cfg.RunnerBin = migration.relocate(bin)
	}
	ts.Close()
	s.Close()
	if moveHome {
		journal := homeJournalPath(migration.Config)
		if err := applyHomeTransaction(journal, &migration); err != nil {
			t.Fatal(err)
		}
	}
	s, ts = startDesk(t, cfg)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		code, raw = call("GET", path+"/evaluations", nil, "")
		var result struct {
			Items []inputEvaluation `json:"items"`
		}
		json.Unmarshal(raw, &result)
		if len(result.Items) == 1 && (result.Items[0].State == "completed" || result.Items[0].State == "partial") {
			b = result.Items[0]
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if b.State != "completed" || b.JobID != job.ID || b.Rows[0].RunID != first.ID {
		t.Fatalf("did not reconcile: %s %s", b, raw)
	}
	runs, e := s.inputRunner(context.Background(), "jobs/"+job.ID+"/runs", nil, "")
	if e != nil {
		t.Fatal(e)
	}
	var listed struct {
		Items []json.RawMessage `json:"items"`
	}
	json.Unmarshal(runs, &listed)
	if len(listed.Items) != 3 {
		t.Fatalf("duplicated run: %s", runs)
	}
	if !strings.Contains(string(b.Rows[0].Result), `"clear"`) || !strings.Contains(string(b.Rows[1].Result), `"hit"`) || !strings.Contains(string(b.Rows[2].Result), `"unresolved"`) {
		t.Fatalf("wrong decisions: %+v", b.Rows)
	}
	// The durable completed state can become visible just before the worker
	// releases its preparation lock. Replay the same confirmation after that
	// specific temporary refusal; never mask another stale/bad-request failure.
	deadline = time.Now().Add(5 * time.Second)
	for {
		code, raw = call("POST", path+"/evaluations/"+b.ID+"/confirm", map[string]any{"reviewed": true}, saved.SHA256)
		if code == 200 {
			break
		}
		if code != 409 || !strings.Contains(string(raw), "This evaluation is being updated. Try again.") || !time.Now().Before(deadline) {
			t.Fatal("repeat confirmation failed", code, string(raw))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, e = s.readRunReferences(b.ReleaseID); e != nil {
		t.Fatal("independent references missing", e)
	}
	// New jobs must obey the actual installation policy after restart.
	ts.Close()
	s.Close()
	cfg.RunnerAllowUntestedReleases = false
	s, ts = startDesk(t, cfg)
	untested := review()
	code, raw = call("POST", path+"/evaluations/"+untested.ID+"/confirm", map[string]any{"reviewed": true}, saved.SHA256)
	if code != 409 {
		t.Fatal("untested release admitted", code, string(raw))
	}
	writeProject(t, project, map[string]string{"jpack.json": `{"configVersion":"2","packs":{"screening":{"path":"pack.json","matrix":"cases.json"}}}`, "cases.json": `{"matrixVersion":"3","cases":[{"id":"clear","facts":{"screening":{"matches":0}},"evidenceAvailability":{"screening-record":"present"},"expectedDisposition":{"kind":"outcome","outcomeId":"clear","reasons":[],"handoff":{"state":"none"}}}]}`})
	tested := review()
	var release struct {
		Tests string `json:"tests"`
	}
	json.Unmarshal(tested.Release, &release)
	if release.Tests != "passed" {
		t.Fatalf("saved tests did not run: %s", tested.Release)
	}
	code, raw = call("POST", path+"/evaluations/"+tested.ID+"/confirm", map[string]any{"reviewed": true}, saved.SHA256)
	if code != 202 {
		t.Fatal("tested release refused", code, string(raw))
	}
	// Recipe edits keep old results tied to their exact version.

	code, raw = call("PUT", path, map[string]any{"name": "Screening inputs", "recipe": inputRecipe{Qualification: q, Intent: "Changed"}}, saved.SHA256)
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	var changed inputPreparationReply
	json.Unmarshal(raw, &changed)
	if changed.Document.Records[0].Values["Matches"] != "0" || b.RecipeVersion == changed.Document.RecipeVersion {
		t.Fatal("history rewritten")
	}
}

func TestInputBatchSeparatesSigningAndNeverRepeatsInterruptedExecution(t *testing.T) {
	jobID := "job_" + strings.Repeat("a", 32)
	run1 := "run_" + strings.Repeat("b", 32)
	run2 := "run_" + strings.Repeat("c", 32)
	calls := 0
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" {
			t.Error("an accepted run was resubmitted")
		}
		if r.URL.Path == "/v1/runs/"+run1 {
			writeJSON(w, 200, map[string]any{"id": run1, "jobId": jobID, "state": "completed", "result": map[string]any{"disposition": map[string]any{"kind": "outcome", "outcomeId": "qualified"}, "trace": strings.Repeat("x", 1<<20)}, "signing": map[string]any{"status": "failed", "reason": "expected key unavailable"}})
		} else {
			writeJSON(w, 200, map[string]any{"id": run2, "jobId": jobID, "state": "interrupted", "problem": "Stopped during evaluation."})
		}
	}))
	defer companion.Close()
	s := &Server{jobs: &jobsCompanion{url: companion.URL, token: "private", done: make(chan struct{})}, log: log.New(io.Discard, "", 0)}
	root, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	b := inputEvaluation{Version: 1, ID: "eval_" + strings.Repeat("d", 32), PreparationID: "prep_" + strings.Repeat("e", 32), JobID: jobID, ConfirmedAt: "now", Rows: []inputEvaluationRow{{RecordID: "one", RunID: run1, State: "running"}, {RecordID: "two", RunID: run2, State: "running"}}}
	s.advanceInputEvaluation(context.Background(), root, &b)
	if b.State != "partial" || b.Rows[0].State != "completed" || !strings.Contains(string(b.Rows[0].Signing), `"failed"`) || !strings.Contains(string(b.Rows[0].Result), `"qualified"`) || len(b.Rows[0].Result) > 1024 || b.Rows[1].State != "interrupted" {
		t.Fatalf("result state lost: %+v", b)
	}
	s.advanceInputEvaluation(context.Background(), root, &b)
	if calls != 2 {
		t.Fatal("terminal runs were polled or repeated", calls)
	}
}

func TestInputEvaluationRequiresSessionAndSameOrigin(t *testing.T) {
	s, _ := newTestServer(t, false)
	base := "/api/input-preparations/prep_" + strings.Repeat("a", 32)
	for _, path := range []string{base + "/prepare", base + "/conversion-preview", base + "/evaluations", base + "/evaluations/eval_" + strings.Repeat("b", 32) + "/confirm", base + "/evaluations/eval_" + strings.Repeat("b", 32) + "/resume"} {
		for _, authorized := range []bool{false, true} {
			r := httptest.NewRequest("POST", path, strings.NewReader(`{"reviewed":true}`))
			want := 401
			if authorized {
				r.Header.Set("Authorization", "Bearer "+testToken)
				r.Header.Set("Origin", "https://elsewhere.example")
				want = 403
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != want {
				t.Fatalf("%s %d want %d", path, w.Code, want)
			}
		}
	}
}
