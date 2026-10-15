package desk

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResearchActivityRetainsLiveCoordinatesAndTypedRecovery(t *testing.T) {
	root, run := researchRunFixture(t)
	args := json.RawMessage(`{"url":"https://vertexaisearch.cloud.google.com/grounding-api-redirect/example"}`)
	_, e := researchAttempt(context.Background(), root, "page", args, func(_ context.Context, current inputResearchRun, _ inputResearchAttempt) (json.RawMessage, error) {
		activity := researchActivity(current)
		if len(activity.Steps) != 1 || activity.Steps[0].URL == "" || activity.Steps[0].State != "started" {
			t.Fatal("live request not retained")
		}
		return nil, researchSourceFailure(400, []byte(`{"error":"source failed: web-redirect-origin {\"redirect\":{\"policy\":\"google-grounding\",\"permitted\":false,\"from\":\"https://vertexaisearch.cloud.google.com\",\"to\":\"https://example.com\"}}"}`))
	})
	if e == nil {
		t.Fatal("refusal lost")
	}
	run, e = readInputResearch(root)
	if e != nil {
		t.Fatal(e)
	}
	run.State = "interrupted"
	run.Problem = run.Attempts[0].Problem
	summary := researchSummary(run)
	if summary.Recovery != "google-grounding" || run.Attempts[0].ProblemCode != "web-redirect-origin" {
		t.Fatalf("missing recovery %+v", summary)
	}
	if summary.CurrentStep != nil {
		t.Fatal("interrupted attempt shown as active")
	}
	// The worker conservatively retains uncertainty, not a made-up failed charge.
	if researchActivity(run).Steps[0].State != "uncertain" {
		t.Fatal("uncertainty overwritten")
	}
}
func TestResearchActivityLegacyCoordinatesRequireMatchingRequest(t *testing.T) {
	_, run := researchRunFixture(t)
	run.Seeds = []string{"https://vertexaisearch.cloud.google.com/grounding-api-redirect/example"}
	action := json.RawMessage(`{"tool":"read","arguments":{"url":"https://vertexaisearch.cloud.google.com/grounding-api-redirect/example"}}`)
	run.Attempts = []inputResearchAttempt{{Kind: "model", State: "completed", Result: action}}
	decoded, _ := decodeResearchAction(action)
	kind, args, e := researchSourceArguments(run, decoded)
	if e != nil {
		t.Fatal(e)
	}
	run.Attempts = append(run.Attempts, inputResearchAttempt{Kind: kind, State: "uncertain", Digest: digestOf(append([]byte(kind+"\n"), args...)), Problem: researchSourceFailure(400, []byte(`{"error":"web-redirect-origin"}`)).Error()})
	step := researchStep(run, 1)
	if step.URL != run.Seeds[0] || step.Recovery != "google-grounding" {
		t.Fatalf("legacy display lost %+v", step)
	}
	run.Attempts[1].Digest = "different"
	if step = researchStep(run, 1); step.URL != "" || step.Recovery != "" {
		t.Fatal("inferred from unrelated action")
	}
}
func TestResearchActivityEndpointIsScopedAndDoesNotReturnPayloads(t *testing.T) {
	s, _ := newTestServer(t, false)
	_, run := researchRunFixture(t)
	run.Intent = "private-prompt"
	run.Sources = []inputResearchSource{{Text: "private-page-body"}}
	run.Attempts = []inputResearchAttempt{{ID: "one", Kind: "model", State: "completed", Result: json.RawMessage(`{"tool":"records","arguments":{"records":[{"name":"private-record"}]}}`)}}
	root, e := s.inputResearchRoot(true)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	rr, e := openInputResearch(root, run.ID, true)
	if e != nil {
		t.Fatal(e)
	}
	defer rr.Close()
	if e = writeInputResearch(rr, run); e != nil {
		t.Fatal(e)
	}
	path := "/api/input-preparations/" + run.PreparationID + "/research/" + run.ID + "/activity"
	call := func(path string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if auth {
			r.Header.Set("Authorization", "Bearer "+testToken)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	reply := call(path, true)
	if reply.Code != 200 || !strings.Contains(reply.Body.String(), `"result":"records"`) {
		t.Fatal(reply.Code, reply.Body.String())
	}
	for _, private := range []string{"private-prompt", "private-page-body", "private-record", "arguments", "signer"} {
		if strings.Contains(reply.Body.String(), private) {
			t.Fatal("payload escaped activity projection", private)
		}
	}
	if reply.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private activity can be cached")
	}
	if call(path, false).Code == 200 {
		t.Fatal("unauthenticated activity allowed")
	}
	if call(strings.Replace(path, run.PreparationID, "prep_"+strings.Repeat("c", 32), 1), true).Code != 404 {
		t.Fatal("activity leaked into another input")
	}
}
func TestResearchRedirectRecoveryRequiresCompleteRefusal(t *testing.T) {
	valid := `web-redirect-origin {"redirect":{"policy":"google-grounding","permitted":false,"from":"https://vertexaisearch.cloud.google.com","to":"https://example.com"}}`
	if researchRedirectRecovery(valid) != "google-grounding" {
		t.Fatal("valid refusal not recognized")
	}
	for _, value := range []string{valid + " secret", strings.Replace(valid, "false", "true", 1), strings.Replace(valid, "example.com", "example.com/path", 1), strings.Replace(valid, "example.com", "user:password@example.com", 1), strings.Replace(valid, "https://vertexaisearch.cloud.google.com", "https://other.example", 1), `web-redirect-origin`, strings.Repeat("x", 201)} {
		if researchRedirectRecovery(value) != "" {
			t.Fatal("unrelated recovery allowed")
		}
	}
}

func TestResearchActivityKeepsEarlierParallelReadsVisible(t *testing.T) {
	_, run := researchRunFixture(t)
	run.Attempts = []inputResearchAttempt{{ID: "first", Kind: "page", State: "started"}, {ID: "second", Kind: "page", State: "started"}, {ID: "last", Kind: "page", State: "completed"}}
	summary := researchSummary(run)
	if summary.ActivePageReads != 2 || summary.CurrentStep == nil || summary.CurrentStep.ID != "first" {
		t.Fatal("out-of-order completion hid active reads", summary)
	}
	run.State = "interrupted"
	summary = researchSummary(run)
	if summary.ActivePageReads != 0 || summary.CurrentStep != nil {
		t.Fatal("terminal run shows active reads")
	}
}

func TestResearchEmptyOutcomeSurvivesBudgetDecisionProjection(t *testing.T) {
	run := inputResearchRun{State: "completed-with-gaps", Problem: "Research finished with the saved results. The additional allowance was declined.", Sources: []inputResearchSource{{ID: "source"}}, Attempts: []inputResearchAttempt{{Kind: "model", State: "completed", Result: recoveryAction("finish", map[string]string{"reason": "The retained notices are closed; no supported open opportunity was found."})}}}
	summary := researchSummary(run)
	if summary.RecordCount != 0 || summary.SourceCount != 1 || summary.Problem != "Research ended. Saved records: 0. Retained sources: 1. No additional requests were authorized." || summary.Explanation != "The retained notices are closed; no supported open opportunity was found." {
		t.Fatalf("lost outcome: %+v", summary)
	}
	if researchActivity(run).Steps[0].Explanation != summary.Explanation {
		t.Fatal("finish step lost explanation")
	}
	if run.Problem != "Research finished with the saved results. The additional allowance was declined." {
		t.Fatal("projection rewrote journal")
	}
	run.State = "running"
	if researchSummary(run).Explanation != "" {
		t.Fatal("old assessment presented as live outcome")
	}
	run.State = "completed-with-gaps"
	run.Attempts = append(run.Attempts, inputResearchAttempt{Kind: "model", State: "completed", Result: recoveryAction("records", map[string]any{"records": []any{}})})
	if researchSummary(run).Explanation != "" {
		t.Fatal("older assessment presented after later work")
	}
}
func TestResearchExplanationRejectsInvalidAndUnfinishedActions(t *testing.T) {
	good := inputResearchAttempt{Kind: "model", State: "completed", Result: recoveryAction("finish", map[string]string{"reason": "No supported records."})}
	for _, change := range []func(*inputResearchAttempt){
		func(a *inputResearchAttempt) { a.State = "uncertain" },
		func(a *inputResearchAttempt) { a.ActionProblem = "Invalid action" },
		func(a *inputResearchAttempt) { a.Kind = "page" },
		func(a *inputResearchAttempt) {
			a.Result = recoveryAction("finish", map[string]string{"reason": strings.Repeat("x", 2001)})
		},
	} {
		a := good
		change(&a)
		if researchAttemptExplanation(a) != "" {
			t.Fatal("invalid finish displayed")
		}
	}
	run := inputResearchRun{State: "completed", Attempts: []inputResearchAttempt{good}}
	child := good
	child.TaskID = "child"
	child.Result = recoveryAction("finish", map[string]string{"reason": "Child only"})
	run.Attempts = append(run.Attempts, child)
	if researchSummary(run).Explanation != "No supported records." {
		t.Fatal("child assessment replaced coordinator")
	}
}
