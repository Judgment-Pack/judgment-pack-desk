package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func researchRunFixture(t *testing.T) (*os.Root, inputResearchRun) {
	t.Helper()
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { root.Close() })
	run := inputResearchRun{Version: 1, ID: "research_" + strings.Repeat("a", 32), PreparationID: "prep_" + strings.Repeat("b", 32), RecipeVersion: 1, Recipe: researchRecipeFixture(), State: "running", Seeds: []string{"https://example.com/"}, Records: []inputResearchEntity{}, Sources: []inputResearchSource{}, Attempts: []inputResearchAttempt{}}
	if e = writeInputResearch(root, run); e != nil {
		t.Fatal(e)
	}
	return root, run
}
func TestResearchWorkerCommitsBeforeCallCachesAndKeepsUncertainOutcome(t *testing.T) {
	root, _ := researchRunFixture(t)
	calls := 0
	args := json.RawMessage(`{"step":0}`)
	call := func(ctx context.Context, run inputResearchRun, a inputResearchAttempt) (json.RawMessage, error) {
		calls++
		saved, e := readInputResearch(root)
		if e != nil || len(saved.Attempts) != 1 || saved.Attempts[0].State != "started" {
			t.Fatal("provider called before durable intent", e)
		}
		return json.RawMessage(`{"tool":"finish","arguments":{}}`), nil
	}
	first, e := researchAttempt(context.Background(), fileResearchStore(root), "model", args, call)
	if e != nil {
		t.Fatal(e)
	}
	second, e := researchAttempt(context.Background(), fileResearchStore(root), "model", args, call)
	if e != nil || string(first) != string(second) || calls != 1 {
		t.Fatal("completed request repeated", e, calls)
	}
	fail := func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		calls++
		return nil, errors.New("response lost")
	}
	_, e = researchAttempt(context.Background(), fileResearchStore(root), "model", json.RawMessage(`{"step":1}`), fail)
	if e == nil {
		t.Fatal("lost response accepted")
	}
	_, e = researchAttempt(context.Background(), fileResearchStore(root), "model", json.RawMessage(`{"step":1}`), fail)
	if e == nil || calls != 2 {
		t.Fatal("uncertain request replayed")
	}
}
func TestResearchWorkerCannotReadInventedURLsOrCommitInvalidBatch(t *testing.T) {
	_, run := researchRunFixture(t)
	if _, _, e := researchSourceArguments(run, inputResearchAction{Tool: "read", Arguments: json.RawMessage(`{"url":"http://localhost/private"}`)}); e == nil {
		t.Fatal("invented fetch admitted")
	}
	if kind, _, e := researchSourceArguments(run, inputResearchAction{Tool: "read", Arguments: json.RawMessage(`{"url":"https://example.com"}`)}); e != nil || kind != "page" {
		t.Fatal(e)
	}
	valid := `{"kind":"company","name":"Example","url":"https://example.com/","fields":{"country":{"status":"missing","sources":[]}}}`
	if e := applyResearchRecords(&run, json.RawMessage(`{"records":[`+valid+`]}`)); e != nil || len(run.Records) != 1 {
		t.Fatal(e)
	}
	before, _ := json.Marshal(run.Records)
	invalid := strings.Replace(valid, `"status":"missing"`, `"status":"supported"`, 1)
	if e := applyResearchRecords(&run, json.RawMessage(`{"records":[`+valid+`,`+invalid+`]}`)); e == nil {
		t.Fatal("unsupported extraction accepted")
	}
	after, _ := json.Marshal(run.Records)
	if string(before) != string(after) {
		t.Fatal("invalid batch partially committed")
	}
}
func TestResearchRecordsExistBeforeQualificationAndRemainGenerated(t *testing.T) {
	s, _ := newTestServer(t, false)
	root, e := s.openInputPreparations(true)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	_, run := researchRunFixture(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	doc := inputPreparation{Version: 1, ID: run.PreparationID, Name: "Independent research", Recipe: inputRecipe{Intent: "Find suppliers", Research: &run.Recipe}, RecipeVersion: 1, CreatedAt: now, UpdatedAt: now, Records: []preparedInputRecord{}, Imports: []inputImport{}, Activity: []inputActivity{}}
	if _, e = writeInputPreparation(root, doc); e != nil {
		t.Fatal(e)
	}
	run.Records = []inputResearchEntity{{ID: "entity-supplier", Kind: "company", Name: "Example", URL: "https://example.com/", Fields: map[string]inputResearchValue{"country": {Value: json.RawMessage(`"Canada"`), Status: "interpretation"}}}}
	if e = s.publishResearchRecords(run); e != nil {
		t.Fatal(e)
	}
	saved, e := readInputPreparation(root, doc.ID)
	if e != nil {
		t.Fatal(e)
	}
	if saved.Document.Recipe.Qualification != nil || len(saved.Document.Records) != 1 || saved.Document.Records[0].Class != "generated" || saved.Document.Records[0].Research.Entity.ID != "entity-supplier" {
		t.Fatal("research acquired qualification or lost its generated provenance")
	}
	if e = s.publishResearchRecords(run); e != nil {
		t.Fatal(e)
	}
	repeated, e := readInputPreparation(root, doc.ID)
	if e != nil || repeated.SHA256 != saved.SHA256 {
		t.Fatal("repeat publication changed history", e)
	}
}

func TestResearchUsesSavedRecordGoalWhenInstructionsContainOldCount(t *testing.T) {
	_, run := researchRunFixture(t)
	run.Intent = "Find up to 3 companies with at least 50 employees. Use 3 searches."
	run.Recipe.Limits.Records = 10
	run.Recipe.Limits.Searches = 10
	run.Records = []inputResearchEntity{{ID: "saved-one"}}
	run.Attempts = []inputResearchAttempt{{Kind: "search"}, {Kind: "search"}}
	prompt, err := researchPrompt(run)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Instructions string         `json:"instructions"`
		Authority    string         `json:"limitsAuthority"`
		Goal         map[string]int `json:"recordGoal"`
		Remaining    map[string]int `json:"requestsRemaining"`
	}
	if err = json.Unmarshal([]byte(prompt), &got); err != nil {
		t.Fatal(err)
	}
	if got.Goal["maximum"] != 10 || got.Goal["saved"] != 1 || got.Goal["remaining"] != 9 || got.Remaining["search"] != 8 {
		t.Fatal("saved limits were replaced by stale prose", got)
	}
	if got.Instructions != run.Intent || !strings.Contains(got.Authority, "even when instructions contain older numbers") || !strings.Contains(inputResearchInstructions, "override conflicting totals") {
		t.Fatal("lost criteria or limit precedence", got)
	}
	// The host still enforces the cap independently of whether a model follows it.
	records := []any{}
	run.Records = nil
	run.Seeds = nil
	for i := 0; i < 11; i++ {
		url := "https://company" + fmt.Sprint(i) + ".example/"
		run.Seeds = append(run.Seeds, url)
		records = append(records, map[string]any{"kind": "company", "name": "Company " + fmt.Sprint(i), "url": url, "fields": map[string]any{"country": map[string]any{"status": "missing", "sources": []any{}}}})
	}
	raw, _ := json.Marshal(map[string]any{"records": records})
	if applyResearchRecords(&run, raw) == nil || len(run.Records) != 0 {
		t.Fatal("oversized batch was saved")
	}
	raw, _ = json.Marshal(map[string]any{"records": records[:10]})
	if e := applyResearchRecords(&run, raw); e != nil || len(run.Records) != 10 {
		t.Fatal("old prose limited the saved batch", e, len(run.Records))
	}
}
