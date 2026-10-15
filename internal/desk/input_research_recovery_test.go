package desk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func recoveryOperations() inputResearchOperations {
	return inputResearchOperations{
		validateSearch: func(context.Context, inputResearchRecipe) error { return nil },
		validateAI:     func(context.Context, inputResearchAI) error { return nil },
		publish:        func(inputResearchRun) error { return nil },
	}
}
func recoveryRefusal(code string) error {
	return &researchSourceError{code: code, message: "The source could not be used. No automatic retry was made.", acquisitionRefusal: true}
}
func recoveryAction(tool string, args any) json.RawMessage {
	raw, _ := json.Marshal(inputResearchAction{Tool: tool, Arguments: func() json.RawMessage { b, _ := json.Marshal(args); return b }()})
	return raw
}

func TestResearchRecoversLateSourceFailureWithoutRepeatingWork(t *testing.T) {
	root, run := researchRunFixture(t)
	run.Recipe.Limits.ModelCalls = 12
	run.Seeds = []string{"https://example.com/", "https://example.com/large.pdf", "https://other.example/"}
	if err := writeInputResearch(root, run); err != nil {
		t.Fatal(err)
	}
	ops := recoveryOperations()
	models, pages := 0, 0
	publishedBeforeFailure := false
	var savedSource string
	var seenPrompts []string
	ops.model = func(_ context.Context, _ inputResearchAI, prompt string, _ func(bool) error) (json.RawMessage, error) {
		seenPrompts = append(seenPrompts, prompt)
		n := models
		models++
		switch n {
		case 0:
			return recoveryAction("read", map[string]string{"url": run.Seeds[0]}), nil
		case 1:
			return recoveryAction("records", map[string]any{"records": []any{map[string]any{"kind": "company", "name": "Example", "url": run.Seeds[0], "fields": map[string]any{"country": map[string]any{"value": "Canada", "status": "supported", "sources": []any{map[string]string{"sourceId": savedSource, "quote": "Canada"}}}}}}}), nil
		case 2, 3:
			return recoveryAction("read", map[string]string{"url": run.Seeds[1]}), nil
		case 4:
			return recoveryAction("read", map[string]string{"url": run.Seeds[2]}), nil
		case 5:
			return recoveryAction("records", map[string]any{"records": []any{map[string]any{"kind": "company", "name": "Other", "url": run.Seeds[2], "fields": map[string]any{"country": map[string]any{"status": "missing", "sources": []any{}}}}}}), nil
		default:
			return recoveryAction("finish", map[string]any{}), nil
		}
	}
	ops.source = func(_ context.Context, _ inputResearchRun, a inputResearchAttempt, _ json.RawMessage) (inputResearchSource, error) {
		pages++
		if a.URL == run.Seeds[1] {
			if !publishedBeforeFailure {
				t.Error("earlier records were not published before late failure")
			}
			return inputResearchSource{}, recoveryRefusal("web-over-limit")
		}
		if savedSource == "" {
			savedSource = a.ID
		}
		return inputResearchSource{ID: a.ID, Kind: "page", URL: a.URL, Text: "Canada", Verification: "verified"}, nil
	}
	ops.publish = func(current inputResearchRun) error {
		if len(current.Records) > 0 {
			publishedBeforeFailure = true
		}
		return nil
	}
	executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
	saved, err := readInputResearch(root)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != "completed-with-gaps" || len(saved.Records) != 2 || len(saved.Sources) != 2 || pages != 3 || models != 7 {
		t.Fatalf("state=%s records=%d sources=%d pages=%d models=%d", saved.State, len(saved.Records), len(saved.Sources), pages, models)
	}
	if saved.Records[1].Fields["country"].Status != "missing" {
		t.Fatal("missing information lost")
	}
	if researchSummary(saved).UnavailableSources != 1 {
		t.Fatal("duplicate request counted as another source gap")
	}
	var prompt struct {
		Unavailable []inputResearchSourceFailure `json:"unavailableSources"`
		Sources     []inputResearchSource        `json:"retainedSources"`
		Records     []inputResearchEntity        `json:"savedRecords"`
		Remaining   map[string]int               `json:"requestsRemaining"`
	}
	if err = json.Unmarshal([]byte(seenPrompts[3]), &prompt); err != nil {
		t.Fatal(err)
	}
	if len(prompt.Unavailable) != 1 || prompt.Unavailable[0].URL != run.Seeds[1] || len(prompt.Sources) != 1 || len(prompt.Records) != 1 || prompt.Remaining["page"] != run.Recipe.Limits.Pages-2 {
		t.Fatal("next AI step lost saved evidence, failure or budget")
	}
	for _, a := range saved.Attempts {
		if a.Continued && (a.State != "uncertain" || len(a.Result) != 0) {
			t.Fatal("failed source was promoted to evidence")
		}
	}
	// A process loss after any saved gap cannot authorize a restart/replay.
	saved.State = "running"
	recoverInputResearch(&saved)
	if saved.State != "interrupted" || len(saved.Records) != 2 || len(saved.Sources) != 2 {
		t.Fatal("process recovery lost saved progress")
	}
}

func TestResearchSourceRecoveryBoundary(t *testing.T) {
	for _, tt := range []struct {
		name, kind, code string
		refusal, allow   bool
	}{
		{"large page", "page", "web-over-limit", true, true},
		{"large response", "page", "source-response-too-large", false, true},
		{"unsupported", "page", "web-unsupported-type", true, true},
		{"processing", "page", "web-processing-failed", true, true},
		{"redirect loop", "page", "web-redirect-limit", true, true},
		{"search empty", "search", "search-not-grounded", true, true},
		{"search timeout", "search", "search-timeout", true, true},
		{"search temporary", "search", "search-temporary", true, true},
		{"bad search response", "search", "search-invalid-response", true, true},
		{"invalid response after acquisition", "search", "search-invalid-response", false, false},
		{"rejected search", "search", "search-invalid-request", true, false},
		{"authentication", "search", "credentials-required", true, false},
		{"configuration", "search", "source-changed", true, false},
		{"policy", "page", "web-redirect-origin", true, false},
		{"private address", "page", "web-public-only", true, false},
		{"ambiguous website", "page", "web-unavailable", true, false},
		{"ambiguous provider", "search", "provider-unavailable", true, false},
		{"rate limit", "search", "rate-limited", true, false},
		{"daily budget", "search", "search-budget-exhausted", true, false},
		{"invalid request", "search", "invalid-request", true, false},
		{"seal failed", "page", "web-over-limit", false, false},
		{"model", "model", "web-over-limit", true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := inputResearchAttempt{Kind: tt.kind, State: "uncertain", ProblemCode: tt.code, AcquisitionRefusal: tt.refusal}
			if researchContinuableAttempt(a) != tt.allow {
				t.Fatal("wrong recovery classification")
			}
		})
	}
}

func TestResearchFatalFailuresStopBeforeNextModelRequest(t *testing.T) {
	for _, failure := range []error{recoveryRefusal("web-redirect-origin"), recoveryRefusal("web-unavailable"), recoveryRefusal("credentials-required"), errors.New("The retained source proof could not be verified."), errors.New("The source could not be saved."), &researchSourceError{code: "web-over-limit", message: "Seal failed"}} {
		t.Run(failure.Error(), func(t *testing.T) {
			root, run := researchRunFixture(t)
			ops := recoveryOperations()
			models, pages := 0, 0
			ops.model = func(context.Context, inputResearchAI, string, func(bool) error) (json.RawMessage, error) {
				models++
				return recoveryAction("read", map[string]string{"url": run.Seeds[0]}), nil
			}
			ops.source = func(context.Context, inputResearchRun, inputResearchAttempt, json.RawMessage) (inputResearchSource, error) {
				pages++
				return inputResearchSource{}, failure
			}
			executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
			saved, err := readInputResearch(root)
			if err != nil || saved.State != "interrupted" || models != 1 || pages != 1 || researchSummary(saved).UnavailableSources != 0 {
				t.Fatal("fatal error was ignored", err, saved.State, models, pages)
			}
		})
	}
}

func TestResearchGapNeedsSavedHistoryAndRespectsStop(t *testing.T) {
	for _, mode := range []string{"stop", "cancel", "journal lost"} {
		t.Run(mode, func(t *testing.T) {
			root, run := researchRunFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			args := json.RawMessage(`{"url":"https://example.com/"}`)
			_, failure := researchAttempt(ctx, fileResearchStore(root), "page", args, func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
				if mode == "journal lost" {
					if err := root.Rename("record.json", "before.json"); err != nil {
						t.Fatal(err)
					}
					if err := root.Mkdir("record.json", 0700); err != nil {
						t.Fatal(err)
					}
				}
				return nil, recoveryRefusal("web-over-limit")
			})
			if mode == "stop" {
				_, err := changeInputResearch(root, func(r *inputResearchRun) error { r.StopRequested = true; return nil })
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "cancel" {
				cancel()
			}
			if continued, _ := continueResearchSource(ctx, fileResearchStore(root), failure); continued {
				t.Fatal("continued without usable live history", run.ID)
			}
		})
	}
}

func TestResearchTimeoutExhaustsSavedRetriesBeforeContinuing(t *testing.T) {
	root, run := researchRunFixture(t)
	run.Recipe.Search = &inputResearchSearch{Connection: "search-test", Revision: "reviewed"}
	run.Recipe.Limits.Searches = 4
	run.Recipe.Retries = &inputResearchRetryPolicy{Mode: "temporary", MaxRetries: 2, SearchTimeouts: true}
	if err := writeInputResearch(root, run); err != nil {
		t.Fatal(err)
	}
	calls := 0
	args := json.RawMessage(`{"query":"example"}`)
	call := func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		calls++
		return nil, recoveryRefusal("search-timeout")
	}
	_, err := researchAttemptWithWait(context.Background(), fileResearchStore(root), "search", args, call, func(context.Context, time.Time) error { return nil })
	if ok, e := continueResearchSource(context.Background(), fileResearchStore(root), err); !ok || e != nil {
		t.Fatal("exhausted operation did not become a gap", e)
	}
	_, err = researchAttempt(context.Background(), fileResearchStore(root), "search", args, call)
	if ok, e := continueResearchSource(context.Background(), fileResearchStore(root), err); !ok || e != nil {
		t.Fatal("repeat could not reuse gap", e)
	}
	saved, _ := readInputResearch(root)
	if calls != 3 || len(saved.Attempts) != 3 || researchSummary(saved).UnavailableSources != 1 || saved.PendingRetry != nil || !strings.Contains(saved.Attempts[2].RetryNote, "limit") {
		t.Fatal("retry history or budget changed", calls)
	}
}

func TestResearchRecoveryDoesNotExceedBudgets(t *testing.T) {
	for _, mode := range []string{"model", "page"} {
		t.Run(mode, func(t *testing.T) {
			root, run := researchRunFixture(t)
			ops := recoveryOperations()
			models, pages := 0, 0
			run.Recipe.Limits.ModelCalls = 3
			if mode == "page" {
				run.Recipe.Limits.Pages = 1
			}
			run.Seeds = []string{"https://example.com/", "https://other.example/"}
			if err := writeInputResearch(root, run); err != nil {
				t.Fatal(err)
			}
			ops.model = func(context.Context, inputResearchAI, string, func(bool) error) (json.RawMessage, error) {
				models++
				url := run.Seeds[0]
				if mode == "page" && models > 1 {
					url = run.Seeds[1]
				}
				return recoveryAction("read", map[string]string{"url": url}), nil
			}
			ops.source = func(context.Context, inputResearchRun, inputResearchAttempt, json.RawMessage) (inputResearchSource, error) {
				pages++
				return inputResearchSource{}, recoveryRefusal("web-over-limit")
			}
			executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
			saved, err := readInputResearch(root)
			if err != nil || pages != 1 || models > 3 || saved.State != "partial" || researchSummary(saved).UnavailableSources != 1 {
				t.Fatal("limit bypassed", err, pages, models, saved.State)
			}
		})
	}
}

func TestResearchSavesAndEnrichesPartialRecordAtRecordLimit(t *testing.T) {
	root, run := researchRunFixture(t)
	run.Recipe.Limits.Records = 1
	if err := writeInputResearch(root, run); err != nil {
		t.Fatal(err)
	}
	ops := recoveryOperations()
	models := 0
	publishedMissing := false
	sourceID := ""
	ops.model = func(context.Context, inputResearchAI, string, func(bool) error) (json.RawMessage, error) {
		models++
		if models == 2 {
			return recoveryAction("read", map[string]string{"url": run.Seeds[0]}), nil
		}
		field := map[string]any{"status": "missing", "sources": []any{}}
		if models == 3 {
			field = map[string]any{"status": "supported", "value": "Canada", "sources": []any{map[string]string{"sourceId": sourceID, "quote": "Canada"}}}
		}
		return recoveryAction("records", map[string]any{"records": []any{map[string]any{"kind": "company", "name": "Example", "url": run.Seeds[0], "fields": map[string]any{"country": field}}}}), nil
	}
	ops.source = func(_ context.Context, _ inputResearchRun, a inputResearchAttempt, _ json.RawMessage) (inputResearchSource, error) {
		if !publishedMissing {
			t.Error("partial record not published early")
		}
		sourceID = a.ID
		return inputResearchSource{ID: a.ID, Kind: "page", URL: a.URL, Text: "Canada", Verification: "verified"}, nil
	}
	ops.publish = func(r inputResearchRun) error {
		if len(r.Records) == 1 && r.Records[0].Fields["country"].Status == "missing" {
			publishedMissing = true
		}
		return nil
	}
	executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
	saved, err := readInputResearch(root)
	if err != nil || saved.State != "completed" || models != 3 || len(saved.Records) != 1 || saved.Records[0].Fields["country"].Status != "supported" {
		t.Fatal("partial record blocked later enrichment", err, saved.State, models)
	}
}

func TestResearchRecoveryStopsOnChangedConnection(t *testing.T) {
	root, run := researchRunFixture(t)
	ops := recoveryOperations()
	models := 0
	changed := false
	ops.model = func(context.Context, inputResearchAI, string, func(bool) error) (json.RawMessage, error) {
		models++
		return recoveryAction("read", map[string]string{"url": run.Seeds[0]}), nil
	}
	ops.source = func(context.Context, inputResearchRun, inputResearchAttempt, json.RawMessage) (inputResearchSource, error) {
		changed = true
		return inputResearchSource{}, recoveryRefusal("web-over-limit")
	}
	ops.validateSearch = func(context.Context, inputResearchRecipe) error {
		if changed {
			return errResearchSearchChanged
		}
		return nil
	}
	executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
	saved, err := readInputResearch(root)
	if err != nil || saved.State != "blocked" || models != 1 || saved.Problem != errResearchSearchChanged.Error() {
		t.Fatal("changed search settings ignored after recovery", err, saved.State, models)
	}
}

func TestResearchExhaustedTemporarySearchContinuesWithSavedSources(t *testing.T) {
	root := retryFixture(t, 5)
	attempts := 0
	_, err := researchAttemptWithWait(context.Background(), fileResearchStore(root), "search", json.RawMessage(`{"query":"unavailable query"}`), func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		attempts++
		return nil, retryFailure("search-temporary")
	}, noRetryWait)
	if ok, e := continueResearchSource(context.Background(), fileResearchStore(root), err); !ok || e != nil {
		t.Fatal("exhausted search should be a retained gap", e)
	}
	run, _ := readInputResearch(root)
	ops := recoveryOperations()
	models, pages := 0, 0
	ops.model = func(_ context.Context, _ inputResearchAI, prompt string, _ func(bool) error) (json.RawMessage, error) {
		models++
		if !strings.Contains(prompt, `"code":"search-temporary"`) {
			t.Fatal("failure omitted from next model request")
		}
		if models == 1 {
			return recoveryAction("read", map[string]string{"url": run.Seeds[0]}), nil
		}
		if models == 2 {
			return recoveryAction("records", map[string]any{"records": []any{map[string]any{"kind": "company", "name": "Retained company", "url": run.Seeds[0], "fields": map[string]any{"country": map[string]any{"status": "missing", "sources": []any{}}}}}}), nil
		}
		return recoveryAction("finish", map[string]any{}), nil
	}
	ops.source = func(_ context.Context, _ inputResearchRun, a inputResearchAttempt, _ json.RawMessage) (inputResearchSource, error) {
		pages++
		return inputResearchSource{ID: a.ID, Kind: "page", URL: a.URL, Text: "Company", Verification: "verified"}, nil
	}
	executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
	saved, e := readInputResearch(root)
	if e != nil || attempts != 3 || pages != 1 || len(saved.Records) != 1 || saved.State != "completed-with-gaps" {
		t.Fatal("remaining evidence not processed", e, saved.State, attempts, pages)
	}
}
