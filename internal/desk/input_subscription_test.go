package desk

import (
	"context"
	"encoding/json"
	"errors"
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
	for _, backend := range []string{"file", "memory"} {
		for _, phase := range []string{"before", "after"} {
			for _, state := range []string{"enabled", "paused"} {
				t.Run(backend+"/"+phase+"/"+state, func(t *testing.T) { testInputSubscriptionRealRunnerOutbox(t, backend, phase, state == "paused") })
			}
		}
	}
}
func testInputSubscriptionRealRunnerOutbox(t *testing.T, backend, phase string, paused bool) {
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
	if backend == "memory" {
		s.inputPreparations = &memoryPreparationRepository{data: map[string][]byte{}}
		s.evaluationJournals = newMemoryEvaluationJournals()
	}
	call := func(path string, body any, revision string) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		for attempt := 0; ; attempt++ {
			r := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
			r.Header.Set("Authorization", "Bearer "+policyTestSession(t, s))
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
	// One branch starts named, the other starts legacy. Both must keep their
	// reviewed executable mapping when newer publications appear later.
	if phase == "after" {
		checkAndPublishDialect(t, s, saved, "asserted", "1.0.0")
	}
	var b inputEvaluation
	json.Unmarshal(call(path+"/evaluations", map[string]any{"recordIds": []string{saved.Document.Records[0].ID}}, saved.SHA256), &b)
	json.Unmarshal(call(path+"/evaluations/"+b.ID+"/confirm", map[string]any{"reviewed": true, "subscription": map[string]any{"mode": "changed", "includeExisting": false}}, saved.SHA256), &b)
	if phase == "after" {
		checkAndPublishDialect(t, s, saved, "asserted", "1.0.1")
	} else {
		checkAndPublishDialect(t, s, saved, "asserted", "1.0.0")
	}
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
		snapshot, readErr := s.evaluationJournal(br, b.ID).Read(t.Context())
		b, err = snapshot.Document, readErr
		if err != nil || b.State == "blocked" {
			t.Fatal(b.State, b.Problem, err)
		}
	}
	// Runner accepts job creation, but Desk loses the acknowledgement. Its
	// frozen release/configuration must resolve to that exact job on recovery.
	acceptedJobRaw, err := s.inputRunner(t.Context(), "jobs", map[string]any{"name": b.Name, "releaseId": b.ReleaseID, "reviewed": true, "trigger": subscriptionTrigger()}, "")
	if err != nil {
		t.Fatal(err)
	}
	var acceptedJob struct {
		ID        string `json:"id"`
		TriggerID string `json:"initialTriggerId"`
	}
	if err = json.Unmarshal(acceptedJobRaw, &acceptedJob); err != nil || acceptedJob.ID == "" || acceptedJob.TriggerID == "" {
		t.Fatal("invalid accepted job", err)
	}
	tick()
	if b.JobID != acceptedJob.ID || b.Subscription.TriggerID != acceptedJob.TriggerID || b.State != "paused" || len(b.Rows) != 0 {
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
	// Let the accepted run finish before recovering its acknowledgement.
	// Runner retains a submitted occurrence separately from the completed run.
	completed := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		raw, err := s.inputRunner(t.Context(), "triggers/"+b.Subscription.TriggerID+"/deliveries/"+row.EventID, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		var delivery struct {
			Occurrence struct {
				RunID string `json:"runId"`
			} `json:"occurrence"`
		}
		if err = json.Unmarshal(raw, &delivery); err != nil {
			t.Fatal(err)
		}
		if delivery.Occurrence.RunID != "" {
			raw, err = s.inputRunner(t.Context(), "runs/"+delivery.Occurrence.RunID, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			var run struct {
				State string `json:"state"`
			}
			if err = json.Unmarshal(raw, &run); err != nil {
				t.Fatal(err)
			}
			if run.State == "completed" {
				completed = true
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !completed {
		t.Fatal("accepted run did not complete")
	}
	if paused {
		call(path+"/evaluations/"+b.ID+"/subscription-state", map[string]any{"reviewed": true, "enabled": false}, "")
	}
	// Discard that acknowledgement; the journaled outbox is still pending.
	// Then lose the storage acknowledgement too, on either side of commit.
	provider := s.evaluationJournals
	journal := s.evaluationJournal(br, b.ID)
	writes := 0
	s.evaluationJournals = evaluationProviderFunc(func(id string) evaluationJournal {
		if id != b.ID {
			t.Fatal("unexpected batch")
		}
		return interceptedEvaluationJournal{evaluationJournal: journal, write: func(ctx context.Context, candidate inputEvaluation, revision string) (evaluationSnapshot, error) {
			// The confirmed pause is reconciled before receipt-only recovery.
			if paused && len(candidate.Rows) == 1 && candidate.Rows[0].OccurrenceID == "" {
				return journal.CompareAndSwap(ctx, candidate, revision)
			}
			writes++
			if len(candidate.Rows) != 1 || candidate.Rows[0].OccurrenceID != accepted.Occurrence.ID {
				t.Fatal("unexpected recovery write")
			}
			if phase == "after" {
				if _, err := journal.CompareAndSwap(ctx, candidate, revision); err != nil {
					return evaluationSnapshot{}, err
				}
			}
			return evaluationSnapshot{}, errors.New("delivery acknowledgement persistence unavailable")
		}}
	})
	tick()
	s.evaluationJournals = provider
	if writes != 1 || b.Rows[0].EventID != row.EventID || b.Rows[0].EventAt != row.EventAt {
		t.Fatal("uncertain delivery was replayed or changed", writes, b.Rows)
	}
	if phase == "before" && (b.Rows[0].OccurrenceID != "" || !sameEvaluationValue(b.Rows[0].Input, row.Input)) {
		t.Fatal("uncommitted acknowledgement changed outbox")
	}
	tick()
	if b.Rows[0].OccurrenceID != accepted.Occurrence.ID {
		t.Fatal("lost acknowledgement created another occurrence", b.Rows)
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline) && (b.Rows[0].State != "completed"); {
		time.Sleep(25 * time.Millisecond)
		tick()
	}
	if b.Rows[0].State != "completed" || b.Rows[0].RunID == "" || len(b.Rows[0].Result) == 0 {
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
	if _, e = s.preparationRepository().CompareAndSwap(t.Context(), saved.Document, saved.SHA256); e != nil {
		t.Fatal(e)
	}
	if backend == "memory" {
		assertNoCollectionFiles(t, s)
		assertNoEvaluationFiles(t, s)
	}

	s.resumeInputEvaluations(context.Background())
	snapshot, readErr := s.evaluationJournal(br, b.ID).Read(t.Context())
	b, e = snapshot.Document, readErr
	if e != nil || b.State != "blocked" || b.Rows[1].OccurrenceID != "" {
		t.Fatal("stale recipe delivered", b, e)
	}
}

func TestPausedSubscriptionReceiptDoesNotDiscardUnknownIntent(t *testing.T) {
	original := inputEvaluationRow{RecordID: "row", RecordDigest: "record-digest", EventID: "event", EventAt: "2026-10-16T10:00:00Z", State: "pending", Input: json.RawMessage(`{"facts":{"zero":0,"missing":null},"signature":"retained"}`)}
	b := inputEvaluation{JobID: "job_original", ReleaseID: "release_original", Subscription: &inputSubscription{TriggerID: "trigger_original"}}
	valid := map[string]any{"id": "occ_original", "jobId": b.JobID, "releaseId": b.ReleaseID, "triggerId": b.Subscription.TriggerID, "eventId": original.EventID, "state": "submitted", "runId": "run_original"}
	for _, scenario := range []string{"absent", "unavailable", "wrong-job", "wrong-release", "wrong-trigger", "wrong-event", "accepted"} {
		t.Run(scenario, func(t *testing.T) {
			row := original
			receipt := map[string]any{}
			for key, value := range valid {
				receipt[key] = value
			}
			switch scenario {
			case "wrong-job":
				receipt["jobId"] = "job_other"
			case "wrong-release":
				receipt["releaseId"] = "release_other"
			case "wrong-trigger":
				receipt["triggerId"] = "trigger_other"
			case "wrong-event":
				receipt["eventId"] = "event_other"
			}
			calls := 0
			found, err := recoverPausedSubscriptionDelivery(t.Context(), b, &row, func(_ context.Context, path string) (json.RawMessage, error) {
				calls++
				if path != "triggers/trigger_original/deliveries/event" {
					t.Fatal("wrong receipt", path)
				}
				if scenario == "absent" {
					return nil, &inputRunnerError{Status: 404, Message: "absent"}
				}
				if scenario == "unavailable" {
					return nil, &inputRunnerError{Status: 503, Message: "unavailable"}
				}
				raw, _ := json.Marshal(map[string]any{"occurrence": receipt})
				return raw, nil
			})
			if calls != 1 {
				t.Fatal("receipt lookup repeated", calls)
			}
			if scenario == "accepted" {
				if err != nil || !found || row.OccurrenceID != "occ_original" || row.RunID != "run_original" || len(row.Input) != 0 || row.EventID != original.EventID || row.EventAt != original.EventAt {
					t.Fatal(found, err, row)
				}
			} else {
				if found || !sameEvaluationValue(row, original) {
					t.Fatal("unknown intent was changed", found, err, row)
				}
				if (err == nil) != (scenario == "absent") {
					t.Fatal("wrong recovery outcome", err)
				}
			}
		})
	}
}
