package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func retryFixture(t *testing.T, limit int) *os.Root {
	t.Helper()
	root, run := researchRunFixture(t)
	run.Recipe.Search = &inputResearchSearch{Connection: "search-test", Revision: "saved"}
	run.Recipe.Limits.Searches = limit
	run.Recipe.Retries = &inputResearchRetryPolicy{Mode: "temporary", MaxRetries: 2}
	if e := writeInputResearch(root, run); e != nil {
		t.Fatal(e)
	}
	return root
}
func retryFailure(code string) error {
	raw, _ := json.Marshal(map[string]string{"error": code})
	err := researchSourceFailure(400, raw)
	var refusal *researchSourceError
	if errors.As(err, &refusal) {
		refusal.acquisitionRefusal = true
	}
	return err
}
func noRetryWait(context.Context, time.Time) error { return nil }

func TestResearchRetryPolicyValidationAndLegacy(t *testing.T) {
	for _, p := range []*inputResearchRetryPolicy{nil, {Mode: "off"}, {Mode: "off", MaxRetries: 2}, {Mode: "temporary", MaxRetries: 2}, {Mode: "temporary", MaxRetries: 3, SearchTimeouts: true}} {
		r := researchRecipeFixture()
		r.Retries = p
		if e := validateInputResearch(&r); e != nil {
			t.Fatal(p, e)
		}
	}
	for _, p := range []*inputResearchRetryPolicy{{Mode: "all", MaxRetries: 2}, {Mode: "temporary", MaxRetries: 0}, {Mode: "temporary", MaxRetries: 4}, {Mode: "off", SearchTimeouts: true}, {Mode: "off", MaxRetries: -1}} {
		r := researchRecipeFixture()
		r.Retries = p
		if validateInputResearch(&r) == nil {
			t.Fatal("invalid policy accepted", p)
		}
	}
	r := researchRecipeFixture()
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), `"retries"`) {
		t.Fatal("legacy recipe acquired a default policy")
	}
}

func TestResearchRetriesPersistExactAttemptsAndReuseOnlySuccess(t *testing.T) {
	root := retryFixture(t, 5)
	args := json.RawMessage(`{"query":"one"}`)
	// Preserve a previously successful model operation; source retries don't replay it.
	_, err := researchAttempt(context.Background(), root, "model", json.RawMessage(`{"step":0}`), func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		return json.RawMessage(`{"tool":"search","arguments":{"query":"one"}}`), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	calls, waits := 0, 0
	call := func(ctx context.Context, run inputResearchRun, a inputResearchAttempt) (json.RawMessage, error) {
		calls++
		saved, e := readInputResearch(root)
		if e != nil || saved.Attempts[len(saved.Attempts)-1].ID != a.ID || a.State != "started" || saved.PendingRetry != nil {
			t.Fatal("request sent before intent saved", e)
		}
		if a.Number != calls || a.Query != "one" {
			t.Fatal(a)
		}
		if calls < 3 {
			return nil, retryFailure("rate-limited")
		}
		return json.RawMessage(`{"text":"saved","bytes":0}`), nil
	}
	wait := func(ctx context.Context, at time.Time) error {
		waits++
		r, e := readInputResearch(root)
		if e != nil {
			t.Fatal(e)
		}
		if r.PendingRetry == nil || r.PendingRetry.Number != waits+1 || len(r.Attempts) != waits+1 {
			t.Fatal("wait not journaled", r)
		}
		delay := time.Until(at)
		base := time.Duration(5*(1<<(waits-1))) * time.Second
		if delay < base-time.Second || delay > base+base/5 {
			t.Fatal("unbounded backoff", delay)
		}
		if researchSummary(r).PendingRetry == nil {
			t.Fatal("missing progress")
		}
		return nil
	}
	raw, e := researchAttemptWithWait(context.Background(), root, "search", args, call, wait)
	if e != nil || calls != 3 || waits != 2 {
		t.Fatal(calls, waits, e)
	}
	cached, e := researchAttempt(context.Background(), root, "search", args, call)
	if e != nil || calls != 3 || string(raw) != string(cached) {
		t.Fatal("completed retry replayed", e)
	}
	run, e := readInputResearch(root)
	if e != nil {
		t.Fatal(e)
	}
	if len(run.Attempts) != 4 || run.Attempts[0].Kind != "model" || run.PendingRetry != nil {
		t.Fatal(run)
	}
	for i := 1; i < 3; i++ {
		a, b := run.Attempts[i], run.Attempts[i+1]
		if a.ID == b.ID || a.Digest != b.Digest || a.RetriedBy != b.ID || b.RetryOf != a.ID || strings.Contains(a.Problem, "No automatic retry") {
			t.Fatal("incorrect retry lineage", a, b)
		}
	}
	activity := researchActivity(run)
	if len(activity.Steps) != 4 || activity.Steps[3].Number != 3 || activity.Steps[3].MaxAttempts != 3 {
		t.Fatal(activity)
	}
}

func TestResearchRetryLimitsAndOptIn(t *testing.T) {
	for _, tc := range []struct {
		name, kind, code string
		policy           *inputResearchRetryPolicy
		budget, calls    int
		note             string
	}{
		{"temporary server", "search", "search-temporary", &inputResearchRetryPolicy{Mode: "temporary", MaxRetries: 2}, 8, 3, "retry limit"},
		{"temporary off", "search", "search-temporary", &inputResearchRetryPolicy{Mode: "off"}, 8, 1, "No automatic retry"},
		{"temporary budget", "search", "search-temporary", &inputResearchRetryPolicy{Mode: "temporary", MaxRetries: 2}, 1, 1, "request limit"},
		{"exhausted", "search", "rate-limited", &inputResearchRetryPolicy{Mode: "temporary", MaxRetries: 2}, 8, 3, "retry limit"},
		{"budget", "search", "rate-limited", &inputResearchRetryPolicy{Mode: "temporary", MaxRetries: 2}, 1, 1, "request limit"},
		{"legacy", "search", "rate-limited", nil, 8, 1, "No automatic retry"},
		{"off", "search", "rate-limited", &inputResearchRetryPolicy{Mode: "off"}, 8, 1, "No automatic retry"},
		{"timeout off", "search", "search-timeout", &inputResearchRetryPolicy{Mode: "temporary", MaxRetries: 2}, 8, 1, "No automatic retry"},
		{"timeout on", "search", "search-timeout", &inputResearchRetryPolicy{Mode: "temporary", MaxRetries: 2, SearchTimeouts: true}, 8, 3, "retry limit"},
		{"model", "model", "rate-limited", &inputResearchRetryPolicy{Mode: "temporary", MaxRetries: 2}, 8, 1, "No automatic retry"},
		{"website", "page", "web-unavailable", &inputResearchRetryPolicy{Mode: "temporary", MaxRetries: 2}, 8, 1, "No automatic retry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := retryFixture(t, tc.budget)
			_, e := changeInputResearch(root, func(r *inputResearchRun) error { r.Recipe.Retries = tc.policy; return nil })
			if e != nil {
				t.Fatal(e)
			}
			calls := 0
			call := func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
				calls++
				return nil, retryFailure(tc.code)
			}
			_, e = researchAttemptWithWait(context.Background(), root, tc.kind, json.RawMessage(`{}`), call, noRetryWait)
			if e == nil || !strings.Contains(e.Error(), tc.note) || calls != tc.calls {
				t.Fatal(calls, e)
			}
			if tc.calls > 1 && strings.Contains(e.Error(), "No automatic retry") {
				t.Fatal("misleading final message", e)
			}
			run, err := readInputResearch(root)
			if err != nil || len(run.Attempts) != tc.calls || run.PendingRetry != nil {
				t.Fatal(run, err)
			}
			_, _ = researchAttemptWithWait(context.Background(), root, tc.kind, json.RawMessage(`{}`), call, noRetryWait)
			if calls != tc.calls {
				t.Fatal("failed group replayed outside live invocation")
			}
		})
	}
}

func TestResearchRetriesRefuseAmbiguousPermanentAndPostAcquireFailures(t *testing.T) {
	for _, code := range []string{"search-invalid-request", "search-invalid-response", "provider-unavailable", "web-unavailable", "web-redirect-origin", "credentials-required", "invalid-request", "blocked-by-policy", "search-budget-exhausted", "source-changed", "search-not-grounded", "connect-required", "setup-required", "response lost", "verification failed", "seal-rate-limited"} {
		t.Run(code, func(t *testing.T) {
			root := retryFixture(t, 5)
			calls := 0
			_, e := researchAttemptWithWait(context.Background(), root, "search", json.RawMessage(`{}`), func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
				calls++
				if code == "seal-rate-limited" {
					return nil, researchSourceFailure(429, []byte(`{"error":"rate-limited"}`))
				}
				if code == "response lost" || code == "verification failed" {
					return nil, errors.New(code)
				}
				return nil, retryFailure(code)
			}, func(context.Context, time.Time) error { t.Fatal("ineligible retry scheduled"); return nil })
			if e == nil || calls != 1 {
				t.Fatal(calls, e)
			}
		})
	}
}

func TestResearchRetryStopsOnCancellationRestartAndSaveFailure(t *testing.T) {
	for _, mode := range []string{"cancel", "stop", "restart", "journal lost"} {
		t.Run(mode, func(t *testing.T) {
			root := retryFixture(t, 5)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			_, e := researchAttemptWithWait(ctx, root, "search", json.RawMessage(`{"query":"test"}`), func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
				calls++
				return nil, retryFailure("rate-limited")
			}, func(ctx context.Context, at time.Time) error {
				switch mode {
				case "cancel":
					cancel()
					return waitResearchRetry(ctx, at)
				case "stop":
					_, e := changeInputResearch(root, func(r *inputResearchRun) error { r.StopRequested = true; return nil })
					return e
				case "restart":
					_, e := changeInputResearch(root, func(r *inputResearchRun) error { recoverInputResearch(r); return nil })
					return e
				default:
					return root.Remove("record.json")
				}
			})
			if e == nil || calls != 1 {
				t.Fatal("retry sent after stop/lost journal", calls, e)
			}
			if mode == "restart" || mode == "cancel" {
				r, e := readInputResearch(root)
				if e != nil || r.PendingRetry != nil {
					t.Fatal("pending retry survived", e)
				}
			}
		})
	}
}

func TestResearchRetryDoesNotRepeatUnretainedResponses(t *testing.T) {
	root := retryFixture(t, 5)
	calls := 0
	_, e := researchAttemptWithWait(context.Background(), root, "search", json.RawMessage(`{}`), func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		calls++
		_ = root.Remove("record.json")
		return nil, retryFailure("rate-limited")
	}, func(context.Context, time.Time) error { t.Fatal("unretained error retried"); return nil })
	if e == nil || calls != 1 || !strings.Contains(e.Error(), "could not be saved") {
		t.Fatal(calls, e)
	}
}

func TestResearchRetryAdmissionUsesAcquisitionStage(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"source failed: rate-limited"}`))
	}))
	defer gateway.Close()
	s, ts, _ := assistantServer(t)
	defer ts.Close()
	writeDeskConfig(t, s, researchDeskFile(gateway.URL))
	for _, path := range []string{"acquire", "seal"} {
		_, err := s.researchSourceCall(context.Background(), inputResearchPin{Authority: "gateway:test", Public: testSignerPublic}, path, map[string]any{})
		var refusal *researchSourceError
		if !errors.As(err, &refusal) || refusal.acquisitionRefusal != (path == "acquire") {
			t.Fatal(path, err)
		}
	}
}

func TestResearchRetryFailureAfterEarlierRetryHasAccurateHistory(t *testing.T) {
	root := retryFixture(t, 5)
	calls := 0
	_, err := researchAttemptWithWait(context.Background(), root, "search", json.RawMessage(`{}`), func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		calls++
		if calls == 1 {
			return nil, retryFailure("rate-limited")
		}
		return nil, retryFailure("invalid-request")
	}, noRetryWait)
	if err == nil || calls != 2 || strings.Contains(err.Error(), "No automatic retry") || !strings.Contains(err.Error(), "not eligible") {
		t.Fatal(calls, err)
	}
	run, _ := readInputResearch(root)
	if strings.Contains(run.Attempts[1].Problem, "No automatic retry") || run.Attempts[1].RetryNote == "" {
		t.Fatal(run.Attempts[1])
	}
}

// Optional browser exercise writes only to a disposable Desk and mocks research
// progress; it never connects to AI or a source provider.
func TestResearchRetryBrowser(t *testing.T) {
	script := os.Getenv("JPS_RESEARCH_RETRY_BROWSER")
	if script == "" {
		t.Skip("set JPS_RESEARCH_RETRY_BROWSER for the local browser exercise")
	}
	project, config := t.TempDir(), t.TempDir()
	_ = os.Chmod(config, 0700)
	s, ts := startDesk(t, Config{Static: os.DirFS("../../web/dist"), LocalAccess: true, ProjectDir: project, DeskConfigDir: config, Token: testToken})
	defer ts.Close()
	defer s.Close()
	saved, _ := reviewedFixture(t, s, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/home/onword/.nvm/versions/node/v22.23.1/bin/node", script, ts.URL, saved.Document.ID)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %s %v", out, err)
	} else {
		t.Log(string(out))
	}
	root, err := s.openInputPreparations(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	changed, err := readInputPreparation(root, saved.Document.ID)
	if err != nil {
		t.Fatal(err)
	}
	p := changed.Document.Recipe.Research.Retries
	if p == nil || p.Mode != "temporary" || p.MaxRetries != 2 || !p.SearchTimeouts {
		t.Fatal("browser changes did not persist", p)
	}
}

func TestResearchTemporaryRetryRecoversWithoutReplayingSuccess(t *testing.T) {
	root := retryFixture(t, 4)
	calls := 0
	args := json.RawMessage(`{"query":"public companies"}`)
	call := func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		calls++
		if calls == 1 {
			return nil, retryFailure("search-temporary")
		}
		return json.RawMessage(`{"text":"retained source","bytes":0}`), nil
	}
	if _, err := researchAttemptWithWait(context.Background(), root, "search", args, call, noRetryWait); err != nil {
		t.Fatal(err)
	}
	if _, err := researchAttemptWithWait(context.Background(), root, "search", args, call, noRetryWait); err != nil {
		t.Fatal(err)
	}
	saved, err := readInputResearch(root)
	if err != nil || calls != 2 || len(saved.Attempts) != 2 || saved.Attempts[1].State != "completed" || saved.Attempts[0].RetriedBy != saved.Attempts[1].ID {
		t.Fatal("recovery did not reuse the saved success", err, calls, saved.Attempts)
	}
	// A temporary failure from sealing cannot replay the acquisition.
	_, err = researchAttemptWithWait(context.Background(), root, "search", json.RawMessage(`{"query":"seal failure"}`), func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		return nil, researchSourceFailure(503, []byte(`{"error":"search-temporary"}`))
	}, func(context.Context, time.Time) error { t.Fatal("seal was retried"); return nil })
	if err == nil {
		t.Fatal("seal failure lost")
	}
}
