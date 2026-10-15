package desk

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInputSubscriptionRevisionEligibility(t *testing.T) {
	r := preparedInputRecord{ID: "row", Class: "asserted", Values: map[string]string{"size": "0"}}
	doc := inputPreparation{ID: "collection", RecipeVersion: 4, Records: []preparedInputRecord{r}}
	b := inputEvaluation{PreparationID: doc.ID, RecipeVersion: 4, ReleaseID: "release", Subscription: &inputSubscription{Mode: "new", SourceClass: "asserted"}}
	initializeSubscription(b.Subscription, doc)
	if subscriptionEligible(b, doc, r) {
		t.Fatal("existing baseline processed")
	}
	changed := r
	changed.Values = map[string]string{"size": "1"}
	if subscriptionEligible(b, doc, changed) {
		t.Fatal("new-only processed changed record")
	}
	b.Subscription.Mode = "changed"
	if !subscriptionEligible(b, doc, changed) {
		t.Fatal("changed revision ignored")
	}
	id := subscriptionIdentity(b, changed)
	b.Rows = []inputEvaluationRow{{RecordID: r.ID, EventID: id}}
	if subscriptionEligible(b, doc, changed) {
		t.Fatal("same revision processed twice")
	}
	initializeSubscription(b.Subscription, inputPreparation{})
	if len(b.Subscription.Baseline) != 1 {
		t.Fatal("resume replaced baseline")
	}
	b.Subscription = &inputSubscription{Mode: "new", SourceClass: "asserted", IncludeExisting: true}
	b.Rows = nil
	initializeSubscription(b.Subscription, doc)
	if !subscriptionEligible(b, doc, r) {
		t.Fatal("include-existing omitted record")
	}
	generated := r
	generated.Class = "generated"
	if subscriptionEligible(b, doc, generated) {
		t.Fatal("mixed origin admitted")
	}
	b.Subscription.SourceClass = "generated"
	if subscriptionEligible(b, doc, generated) {
		t.Fatal("unreviewed research admitted")
	}
	b.ReleaseID = "another-release"
	if id == subscriptionIdentity(b, changed) {
		t.Fatal("release binding omitted")
	}
}

func TestInputSubscriptionRealRunnerOutbox(t *testing.T) {
	bin, runtime := os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bin == "" || runtime == "" {
		t.Skip("requires Runner and Runtime")
	}
	project, config := t.TempDir(), t.TempDir()
	os.Chmod(config, 0700)
	pack, e := os.ReadFile("testdata/graphs/sanctions-screening-0.1.0.pack.json")
	if e != nil {
		t.Fatal(e)
	}
	writeProject(t, project, map[string]string{"pack.json": string(pack), "jpack.json": `{"configVersion":"2","packs":{"screening":{"path":"pack.json"}}}`})
	s, ts := startDesk(t, Config{RunnerBin: bin, JpackBin: runtime, ProjectDir: project, DeskConfigDir: config, Token: testToken, RunnerAllowUntestedReleases: true})
	defer func() { ts.Close(); s.Close() }()
	s.inputEvaluationCancel()
	<-s.inputEvaluationDone
	if s.inputResearchCancel != nil {
		s.inputResearchCancel()
		<-s.inputResearchDone
	}
	call := func(path string, body any, revision string) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		for attempt := 0; ; attempt++ {
			r := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
			r.Header.Set("Authorization", "Bearer "+testToken)
			r.Header.Set("If-Match", revision)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code == 409 && attempt < 20 && (strings.Contains(w.Body.String(), "busy") || strings.Contains(w.Body.String(), "being saved")) {
				time.Sleep(25 * time.Millisecond)
				continue
			}
			if w.Code >= 300 {
				t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
			}
			return w.Body.Bytes()
		}
	}

	q := &inputQualification{Kind: "pack", Key: "screening", Pack: string(pack), Digest: sha256Digest(pack), Mapping: inputRecordDialect{Version: 2, Case: &inputCaseDialect{Facts: []inputDialectFact{{"/screening/matches", "/values/Matches"}}, Evidence: []inputDialectEvidence{{"screening-record", "/values/Evidence"}}}, Sources: []json.RawMessage{}}, Conversions: map[string]inputConversion{"/screening/matches": {Kind: "decimal"}}}
	var saved inputPreparationReply
	json.Unmarshal(call("/api/input-preparations", map[string]any{"name": "Incremental", "recipe": inputRecipe{Qualification: q}}, ""), &saved)
	path := "/api/input-preparations/" + saved.Document.ID
	json.Unmarshal(call(path+"/imports", map[string]any{"kind": "csv", "name": "baseline.csv", "content": "Name,Matches,Evidence\nBaseline,0,present\n"}, saved.SHA256), &saved)
	var b inputEvaluation
	json.Unmarshal(call(path+"/evaluations", map[string]any{"recordIds": []string{saved.Document.Records[0].ID}}, saved.SHA256), &b)
	json.Unmarshal(call(path+"/evaluations/"+b.ID+"/confirm", map[string]any{"reviewed": true, "subscription": map[string]any{"mode": "changed", "includeExisting": false}}, saved.SHA256), &b)
	root, e := s.inputEvaluationRoot(false)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	br, e := openInputEvaluation(root, b.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	defer br.Close()
	tick := func() {
		t.Helper()
		s.resumeInputEvaluations(context.Background())
		var err error
		b, err = readInputEvaluation(br)
		if err != nil || b.State == "blocked" {
			t.Fatal(b.State, b.Problem, err)
		}
	}
	tick()
	if b.JobID == "" || b.State != "paused" || len(b.Rows) != 0 {
		t.Fatal("save paused started processing", b)
	}
	call(path+"/evaluations/"+b.ID+"/subscription-state", map[string]any{"reviewed": true, "enabled": true}, "")
	tick()
	if len(b.Rows) != 0 {
		t.Fatal("baseline was processed")
	}
	json.Unmarshal(call(path+"/imports", map[string]any{"kind": "csv", "name": "new.csv", "content": "Name,Matches,Evidence\nNew,1,present\n"}, saved.SHA256), &saved)
	tick()
	if len(b.Rows) != 1 || b.Rows[0].State != "pending" {
		t.Fatal("missing durable outbox", b)
	}
	row := b.Rows[0]
	reply, e := s.inputRunner(context.Background(), "triggers/"+b.Subscription.TriggerID+"/deliveries", map[string]any{"releaseId": b.ReleaseID, "event": map[string]any{"id": row.EventID, "occurredAt": row.EventAt, "input": row.Input}}, "")
	if e != nil {
		t.Fatal(e)
	}
	var accepted struct {
		Occurrence struct {
			ID string `json:"id"`
		} `json:"occurrence"`
	}
	json.Unmarshal(reply, &accepted)
	// Discard that acknowledgement; the on-disk outbox is still pending.
	tick()
	if b.Rows[0].OccurrenceID != accepted.Occurrence.ID {
		t.Fatal("lost acknowledgement created another occurrence", b.Rows)
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline) && b.Rows[0].State != "completed"; {
		time.Sleep(25 * time.Millisecond)
		tick()
	}
	if b.Rows[0].State != "completed" || b.Rows[0].RunID == "" {
		t.Fatal("no retained decision", b.Rows)
	}
	tick()
	if len(b.Rows) != 1 {
		t.Fatal("same record revision repeated")
	}
	call(path+"/evaluations/"+b.ID+"/subscription-state", map[string]any{"reviewed": true, "enabled": false}, "")
	tick()
	json.Unmarshal(call(path+"/imports", map[string]any{"kind": "csv", "name": "paused.csv", "content": "Name,Matches,Evidence\nPaused,0,present\n"}, saved.SHA256), &saved)
	tick()
	if len(b.Rows) != 1 {
		t.Fatal("paused subscription published")
	}
	call(path+"/evaluations/"+b.ID+"/subscription-state", map[string]any{"reviewed": true, "enabled": true}, "")
	tick()
	if len(b.Rows) != 2 {
		t.Fatal("resume replaced the baseline")
	}
	// Recipe changes withdraw eligibility instead of silently applying new mapping.
	saved.Document.RecipeVersion++
	pr, e := s.openInputPreparations(false)
	if e != nil {
		t.Fatal(e)
	}
	_, e = writeInputPreparation(pr, saved.Document)
	pr.Close()
	if e != nil {
		t.Fatal(e)
	}
	s.resumeInputEvaluations(context.Background())
	b, e = readInputEvaluation(br)
	if e != nil || b.State != "blocked" || b.Rows[1].OccurrenceID != "" {
		t.Fatal("stale recipe delivered", b, e)
	}
}
