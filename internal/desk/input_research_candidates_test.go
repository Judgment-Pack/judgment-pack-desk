package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func candidateFixture(t *testing.T, n int) (*os.Root, inputResearchRun) {
	t.Helper()
	root, run := researchRunFixture(t)
	run.Seeds = nil
	candidates := []map[string]string{}
	for i := 0; i < n; i++ {
		run.Seeds = append(run.Seeds, fmt.Sprintf("https://company%d.example/", i))
		candidates = append(candidates, map[string]string{"name": fmt.Sprintf("Company %d", i), "urlRef": fmt.Sprintf("url_%d", i+1)})
	}
	raw, _ := json.Marshal(map[string]any{"candidates": candidates})
	if err := queueResearchCandidates(&run, raw); err != nil {
		t.Fatal(err)
	}
	if err := writeInputResearch(root, run); err != nil {
		t.Fatal(err)
	}
	return root, run
}
func testCandidateSource(a inputResearchAttempt) inputResearchSource {
	return inputResearchSource{ID: a.ID, Kind: "page", URL: a.URL, Text: "Canada", Verification: "verified", Bytes: 123}
}

func TestResearchCandidateQueueValidatesAtomicallyAndDoesNotReplay(t *testing.T) {
	_, run := candidateFixture(t, 2)
	before, _ := json.Marshal(run.Candidates)
	for _, raw := range []string{`{"candidates":[{"name":"Duplicate","urlRef":"url_1"}]}`, `{"candidates":[{"name":"New","urlRef":"url_3"},{"name":"Invented","urlRef":"bad"}]}`} {
		if queueResearchCandidates(&run, json.RawMessage(raw)) == nil {
			t.Fatal("invalid/no-op queue accepted")
		}
		after, _ := json.Marshal(run.Candidates)
		if string(before) != string(after) {
			t.Fatal("queue partially changed")
		}
	}
	run.Candidates = nil
	args := json.RawMessage(`{"url":"https://company0.example/"}`)
	a, _, err := beginResearchAttempt(&run, "page", args)
	if err != nil {
		t.Fatal(err)
	}
	if err = queueResearchCandidates(&run, json.RawMessage(`{"candidates":[{"name":"Alias","urlRef":"url_1"}]}`)); err == nil {
		t.Fatal("uncertain read requeued")
	}
	if err = finishResearchAttempt(&run, a.ID, json.RawMessage(`{}`), ""); err != nil {
		t.Fatal(err)
	}
	if queueResearchCandidates(&run, json.RawMessage(`{"candidates":[{"name":"Alias","urlRef":"url_1"}]}`)) == nil {
		t.Fatal("completed read requeued")
	}
}

func TestResearchCandidatePagesOverlapAndCompleteByID(t *testing.T) {
	root, _ := candidateFixture(t, 5)
	ops := recoveryOperations()
	entered := make(chan inputResearchAttempt, 3)
	release := map[string]chan struct{}{}
	for i := 0; i < 3; i++ {
		release[fmt.Sprintf("https://company%d.example/", i)] = make(chan struct{})
	}
	var active, peak atomic.Int32
	ops.source = func(ctx context.Context, _ *os.Root, _ inputResearchRun, a inputResearchAttempt, _ json.RawMessage) (inputResearchSource, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- a
		select {
		case <-release[a.URL]:
		case <-ctx.Done():
			return inputResearchSource{}, ctx.Err()
		}
		if a.URL == "https://company1.example/" {
			return inputResearchSource{}, recoveryRefusal("web-over-limit")
		}
		return testCandidateSource(a), nil
	}
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { done <- readResearchCandidateBatch(ctx, root, ops) }()
	attempts := []inputResearchAttempt{}
	for range 3 {
		select {
		case a := <-entered:
			attempts = append(attempts, a)
		case <-ctx.Done():
			t.Fatal("page reads did not overlap")
		}
	}
	saved, err := readInputResearch(root)
	if err != nil || len(saved.Attempts) != 3 || researchReservedBytes(saved) == 0 {
		t.Fatal("requests were not reserved before calls", err)
	}
	// Finish the first admitted attempt last. The failure must stay on its own ID.
	for i := len(attempts) - 1; i >= 0; i-- {
		close(release[attempts[i].URL])
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	saved, err = readInputResearch(root)
	if err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 3 || len(saved.Sources) != 2 || researchReservedBytes(saved) != 0 || len(researchSourceFailures(saved)) != 1 || saved.Phase != "save" {
		t.Fatal("concurrent batch lost progress", peak.Load(), len(saved.Sources), saved.Phase)
	}
	size := 0
	for _, a := range saved.Attempts {
		if a.URL == "https://company1.example/" {
			if a.State != "uncertain" || !a.Continued || a.ProblemCode != "web-over-limit" {
				t.Fatal("failure attached to wrong request", a)
			}
		} else {
			if a.State != "completed" || a.Problem != "" {
				t.Fatal("completion attached to wrong request", a)
			}
			size += len(a.Result) + 123
		}
	}
	if saved.RetrievedBytes != size {
		t.Fatal("byte accounting lost an update", saved.RetrievedBytes, size)
	}
	if saved.Candidates[3].State != "queued" || saved.Candidates[4].State != "queued" {
		t.Fatal("more than one batch dispatched")
	}
	if err = readResearchCandidateBatch(ctx, root, ops); err != nil {
		t.Fatal(err)
	}
	again, _ := readInputResearch(root)
	if len(again.Attempts) != 3 {
		t.Fatal("skipped record-save checkpoint")
	}
}

func TestResearchCandidateSchedulerSavesTenIncrementally(t *testing.T) {
	root, run := candidateFixture(t, 12)
	run.Candidates = nil
	run.Recipe.Limits.Records = 10
	run.Recipe.Limits.ModelCalls = 12
	if err := writeInputResearch(root, run); err != nil {
		t.Fatal(err)
	}
	ops := recoveryOperations()
	models := 0
	var pages atomic.Int32
	published := []int{}
	ops.source = func(_ context.Context, _ *os.Root, _ inputResearchRun, a inputResearchAttempt, _ json.RawMessage) (inputResearchSource, error) {
		pages.Add(1)
		return testCandidateSource(a), nil
	}
	ops.model = func(_ context.Context, _ inputResearchAI, prompt string, _ func(bool) error) (json.RawMessage, error) {
		models++
		if models == 1 {
			cs := []map[string]string{}
			for i := 0; i < 12; i++ {
				cs = append(cs, map[string]string{"name": fmt.Sprintf("Company %d", i), "urlRef": fmt.Sprintf("url_%d", i+1)})
			}
			return recoveryAction("candidates", map[string]any{"candidates": cs}), nil
		}
		var payload struct {
			Phase   string                `json:"phase"`
			Queue   []map[string]string   `json:"candidateQueue"`
			Sources []inputResearchSource `json:"retainedSources"`
		}
		if err := json.Unmarshal([]byte(prompt), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Phase != "save" {
			t.Fatal("model called before save checkpoint", payload.Phase)
		}
		cs := []any{}
		for _, c := range payload.Queue {
			if c["state"] == "ready" {
				i := 0
				fmt.Sscanf(c["name"], "Company %d", &i)
				cs = append(cs, map[string]any{"kind": "company", "name": c["name"], "url": run.Seeds[i], "fields": map[string]any{"country": map[string]any{"value": "Canada", "status": "supported", "sources": []map[string]string{{"sourceId": c["sourceId"], "quote": "Canada"}}}}})
			}
		}
		if len(cs) == 0 || len(cs) > 3 {
			t.Fatal("wrong batch size", len(cs))
		}
		return recoveryAction("records", map[string]any{"records": cs}), nil
	}
	ops.publish = func(r inputResearchRun) error {
		if len(published) == 0 || published[len(published)-1] != len(r.Records) {
			published = append(published, len(r.Records))
		}
		return nil
	}
	executeInputResearch(context.Background(), root, run, ops)
	saved, err := readInputResearch(root)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != "completed" || len(saved.Records) != 10 || pages.Load() != 10 || models != 5 || fmt.Sprint(published) != "[3 6 9 10]" {
		t.Fatal("candidate scheduling did not reach goal incrementally", saved.State, len(saved.Records), pages.Load(), models, published, saved.Problem)
	}
}

func TestResearchCandidateBudgetStopAndRecovery(t *testing.T) {
	for _, mode := range []string{"page-limit", "storage-limit", "stop", "fatal", "restart"} {
		t.Run(mode, func(t *testing.T) {
			root, run := candidateFixture(t, 5)
			ops := recoveryOperations()
			var calls atomic.Int32
			if mode == "page-limit" {
				run.Recipe.Limits.Pages = 1
			}
			if mode == "storage-limit" {
				run.RetrievedBytes = inputResearchRetainedByteLimit - (inputResearchSourceProofLimit + (2 << 20))
			}
			if mode == "stop" {
				run.StopRequested = true
			}
			if mode == "restart" {
				run.Candidates[0].State = "reading"
				recoverInputResearch(&run)
			}
			if err := writeInputResearch(root, run); err != nil {
				t.Fatal(err)
			}
			started := make(chan struct{}, 3)
			ops.source = func(ctx context.Context, _ *os.Root, _ inputResearchRun, a inputResearchAttempt, _ json.RawMessage) (inputResearchSource, error) {
				calls.Add(1)
				if mode == "fatal" {
					started <- struct{}{}
					if a.URL == run.Seeds[0] {
						for range 3 {
							<-started
						}
						return inputResearchSource{}, recoveryRefusal("web-redirect-origin")
					}
					<-ctx.Done()
					return inputResearchSource{}, ctx.Err()
				}
				return testCandidateSource(a), nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := readResearchCandidateBatch(ctx, root, ops)
			saved, e := readInputResearch(root)
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "page-limit", "storage-limit":
				if err != nil || calls.Load() != 1 || len(saved.Sources) != 1 {
					t.Fatal("shared budget not enforced", err, calls.Load())
				}
			case "stop", "restart":
				if err == nil || calls.Load() != 0 {
					t.Fatal("stop/restart dispatched a request", err, calls.Load())
				}
			case "fatal":
				if err == nil || !strings.Contains(err.Error(), "source could not be used") || calls.Load() != 3 || len(researchSourceFailures(saved)) != 0 {
					t.Fatal("policy failure continued", err, calls.Load())
				}
			}
			if researchReservedBytes(saved) != 0 {
				t.Fatal("reservation leaked")
			}
		})
	}
}

func TestResearchUnusableSearchAfterTimeoutContinues(t *testing.T) {
	root := retryFixture(t, 5)
	if _, err := changeInputResearch(root, func(r *inputResearchRun) error { r.Recipe.Retries.SearchTimeouts = true; return nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	_, err := researchAttemptWithWait(context.Background(), root, "search", json.RawMessage(`{"query":"candidate leads"}`), func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		calls++
		if calls == 1 {
			return nil, retryFailure("search-timeout")
		}
		return nil, retryFailure("search-invalid-response")
	}, noRetryWait)
	if ok, e := continueResearchSource(context.Background(), root, err); !ok || e != nil {
		t.Fatal("invalid response interrupted recovery", e)
	}
	run, e := readInputResearch(root)
	if e != nil {
		t.Fatal(e)
	}
	if calls != 2 || run.PendingRetry != nil || len(run.Sources) != 0 || len(researchSourceFailures(run)) != 1 {
		t.Fatal("invalid response retried or treated as evidence", calls)
	}
	// Acquisition classification cannot swallow proof/sealing errors.
	if ok, _ := continueResearchSource(context.Background(), root, errors.New("proof invalid")); ok {
		t.Fatal("proof failure continued")
	}
}

func TestResearchCandidateRecoveryRetainsCompletedPageWithoutReplay(t *testing.T) {
	_, run := candidateFixture(t, 2)
	args, _ := json.Marshal(map[string]string{"url": run.Candidates[0].URL})
	a, _, err := beginResearchAttempt(&run, "page", args)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(testCandidateSource(a))
	if err = finishResearchAttempt(&run, a.ID, raw, ""); err != nil {
		t.Fatal(err)
	}
	run.Candidates[0].State = "reading"
	args, _ = json.Marshal(map[string]string{"url": run.Candidates[1].URL})
	if _, _, err = beginResearchAttempt(&run, "page", args); err != nil {
		t.Fatal(err)
	}
	run.Candidates[1].State = "reading"
	recoverInputResearch(&run)
	if run.State != "interrupted" || len(run.Sources) != 1 || run.Candidates[0].State != "ready" || run.Candidates[1].State != "uncertain" || run.Attempts[1].State != "uncertain" || researchReservedBytes(run) != 0 {
		t.Fatal("restart lost completed evidence or replayed uncertain read", run)
	}
}

func TestResearchCandidateStopCancelsOutstandingAndKeepsCompleted(t *testing.T) {
	root, _ := candidateFixture(t, 4)
	ops := recoveryOperations()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan inputResearchAttempt, 3)
	release := make(chan struct{})
	ops.source = func(call context.Context, _ *os.Root, _ inputResearchRun, a inputResearchAttempt, _ json.RawMessage) (inputResearchSource, error) {
		entered <- a
		if a.URL == "https://company0.example/" {
			<-release
			return testCandidateSource(a), nil
		}
		<-call.Done()
		return inputResearchSource{}, call.Err()
	}
	done := make(chan error, 1)
	go func() { done <- readResearchCandidateBatch(ctx, root, ops) }()
	for range 3 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("reads did not start")
		}
	}
	close(release)
	// Wait only for the durable first completion, then exercise the actual Stop flag.
	deadline := time.Now().Add(5 * time.Second)
	for {
		saved, err := readInputResearch(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(saved.Sources) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("completed page not retained incrementally")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := changeInputResearch(root, func(r *inputResearchRun) error { r.StopRequested = true; return nil }); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancel lost")
	}
	saved, err := readInputResearch(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Sources) != 1 || len(saved.Attempts) != 3 || saved.Candidates[3].State != "queued" || researchReservedBytes(saved) != 0 {
		t.Fatal("stop lost saved page or dispatched more work")
	}
}

func TestResearchCandidateChecksAIConnectionBeforeReadingQueue(t *testing.T) {
	root, run := candidateFixture(t, 2)
	ops := recoveryOperations()
	calls := 0
	ops.validateAI = func(context.Context, inputResearchAI) error { return errors.New("AI settings changed") }
	ops.source = func(context.Context, *os.Root, inputResearchRun, inputResearchAttempt, json.RawMessage) (inputResearchSource, error) {
		calls++
		return inputResearchSource{}, nil
	}
	executeInputResearch(context.Background(), root, run, ops)
	saved, err := readInputResearch(root)
	if err != nil || calls != 0 || saved.State != "blocked" || saved.Problem != "AI settings changed" {
		t.Fatal("source dispatched with unusable AI connection", err, calls, saved.State)
	}
}
