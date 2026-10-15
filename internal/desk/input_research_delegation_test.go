package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/delegation"
)

func delegatedFixture(t *testing.T, concurrency int) (*Server, *os.Root, inputResearchRun) {
	t.Helper()
	s, root, r := researchV2Fixture(t)
	r.Phase = ""
	r.Recipe.Limits = inputResearchLimits{Records: 3, Searches: 10, Pages: 10, ModelCalls: 20}
	r.Recipe.Search = &inputResearchSearch{Connection: "search-test", Revision: strings.Repeat("a", 64)}
	r.Recipe.Execution = &delegation.Policy{Approach: "delegated", Concurrency: concurrency}
	execution := delegation.Resolve(r.Recipe.Execution, 2)
	r.Execution = &execution
	r.Sources = nil
	r.Seeds = []string{"https://a.example/", "https://b.example/", "https://c.example/"}
	for i, url := range r.Seeds[:2] {
		r.Candidates = append(r.Candidates, inputResearchCandidate{ID: "candidate_" + digestOf([]byte(url)), Name: fmt.Sprintf("Company %d", i+1), URL: url, State: "queued"})
	}
	if e := writeInputResearch(root, r); e != nil {
		t.Fatal(e)
	}
	pr, e := s.openInputPreparations(false)
	if e != nil {
		t.Fatal(e)
	}
	defer pr.Close()
	saved, e := readInputPreparation(pr, r.PreparationID)
	if e != nil {
		t.Fatal(e)
	}
	saved.Document.Recipe.Research = &r.Recipe
	if _, e = writeInputPreparation(pr, saved.Document); e != nil {
		t.Fatal(e)
	}
	return s, root, r
}

type taskPromptFixture struct {
	Task struct {
		ID, Profile, Name string
		CandidateID       string `json:"candidateId"`
		RecordID          string `json:"recordId"`
	} `json:"delegatedTask"`
	URLs     []inputResearchURLReference `json:"availableURLs"`
	Sources  []inputResearchSource       `json:"retainedSources"`
	Feedback string                      `json:"actionFeedback"`
}

func delegatedResult(p taskPromptFixture) json.RawMessage {
	ref := ""
	if len(p.URLs) > 0 {
		ref = p.URLs[0].Ref
	}
	record := map[string]any{"kind": "company", "name": p.Task.Name, "candidateId": p.Task.CandidateID, "urlRef": ref, "fields": map[string]any{"country": map[string]any{"status": "supported", "value": "Canada", "sources": []any{map[string]any{"sourceId": p.Sources[0].ID, "quote": "operates in Canada."}}}}}
	if p.Task.RecordID != "" {
		record["recordId"] = p.Task.RecordID
		delete(record, "candidateId")
	}
	return recoveryAction("records", map[string]any{"records": []any{record}})
}
func delegatedOps(s *Server) inputResearchOperations {
	ops := recoveryOperations()
	ops.publish = s.publishResearchRecords
	ops.source = func(_ context.Context, _ inputResearchRun, a inputResearchAttempt, args json.RawMessage) (inputResearchSource, error) {
		url, _ := researchRequestCoordinates(a.Kind, args)
		return inputResearchSource{ID: a.ID, Kind: a.Kind, URL: url, URLs: []string{url}, Verification: "verified", Text: "This company operates in Canada."}, nil
	}
	return ops
}
func TestResearchDelegationParallelCompletionsRemainBoundToTheirTasks(t *testing.T) {
	s, root, r := delegatedFixture(t, 2)
	ops := delegatedOps(s)
	var active, peak atomic.Int32
	arrived := make(chan struct{}, 2)
	ops.model = func(ctx context.Context, _ inputResearchAI, prompt string, available func(bool) error) (json.RawMessage, error) {
		var p taskPromptFixture
		if e := json.Unmarshal([]byte(prompt), &p); e != nil {
			return nil, e
		}
		if p.Task.ID == "" {
			return recoveryAction("finish", map[string]any{"reason": "The two candidates were prepared."}), nil
		}
		if len(p.Sources) != 1 || strings.Contains(prompt, "full conversation history") {
			return nil, errors.New("task context not isolated")
		}
		n := active.Add(1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		defer active.Add(-1)
		arrived <- struct{}{}
		deadline := time.After(time.Second)
		for peak.Load() < 2 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-deadline:
				return nil, errors.New("did not overlap")
			default:
				time.Sleep(time.Millisecond)
			}
		}
		if e := available(true); e != nil {
			return nil, e
		}
		if e := available(false); e != nil {
			return nil, e
		}
		if p.Task.Name == "Company 1" {
			time.Sleep(50 * time.Millisecond)
		}
		return delegatedResult(p), nil
	}
	executeInputResearch(context.Background(), fileResearchStore(root), r, ops)
	saved, e := readInputResearch(root)
	if e != nil {
		t.Fatal(e)
	}
	if peak.Load() != 2 || len(saved.Records) != 2 || len(saved.Tasks) != 2 {
		t.Fatalf("peak %d records %d tasks %+v problem %s", peak.Load(), len(saved.Records), saved.Tasks, saved.Problem)
	}
	if len(v2Rows(t, s, r.PreparationID).Document.Records) != 2 {
		t.Fatal("findings not published")
	}
	for _, task := range saved.Tasks {
		if task.State != "complete" {
			t.Fatalf("%+v", task)
		}
		found := false
		for _, a := range saved.Attempts {
			if a.TaskID == task.ID && len(a.RecordResults) > 0 {
				found = true
				if a.RecordResults[0].Name != task.Title {
					t.Fatal("completion attached to sibling")
				}
			}
		}
		if !found {
			t.Fatal("no bound record outcome")
		}
	}
	// Durable operation IDs and completed tasks survive reload; no re-dispatch.
	before := len(saved.Attempts)
	saved.State = "running"
	recoverInputResearch(&saved)
	if len(saved.Attempts) != before || saved.Tasks[0].State != "complete" {
		t.Fatal("completed work was reset")
	}
}
func TestResearchDelegationFailedSiblingDoesNotDiscardRecords(t *testing.T) {
	s, root, r := delegatedFixture(t, 2)
	ops := delegatedOps(s)
	ops.model = func(_ context.Context, _ inputResearchAI, prompt string, _ func(bool) error) (json.RawMessage, error) {
		var p taskPromptFixture
		_ = json.Unmarshal([]byte(prompt), &p)
		if p.Task.ID == "" {
			return recoveryAction("finish", map[string]any{"reason": "One task failed."}), nil
		}
		if p.Task.Name == "Company 1" {
			return nil, errors.New("connection ended after admission")
		}
		return delegatedResult(p), nil
	}
	executeInputResearch(context.Background(), fileResearchStore(root), r, ops)
	saved, e := readInputResearch(root)
	if e != nil {
		t.Fatal(e)
	}
	if len(saved.Records) != 1 || len(v2Rows(t, s, r.PreparationID).Document.Records) != 1 {
		t.Fatalf("valid sibling lost: %s %+v", saved.Problem, saved.Tasks)
	}
	var failed int
	for _, task := range saved.Tasks {
		if task.State == "failed" {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("%+v", saved.Tasks)
	}
}
func TestResearchDelegationBudgetIsReservedAcrossWaitingRequests(t *testing.T) {
	_, root, r := delegatedFixture(t, 2)
	r.Recipe.Limits.ModelCalls = 3
	for i := range r.Candidates {
		c := &r.Candidates[i]
		task := newResearchTask(r, "candidate", c.ID, c.Name, "Prepare this company.")
		task.CandidateID = c.ID
		task.State = "running"
		c.TaskID = task.ID
		r.Tasks = append(r.Tasks, task)
	}
	if e := writeInputResearch(root, r); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiting := make(chan struct{})
	done := make(chan error, 1)
	first := context.WithValue(ctx, researchTaskContextKey{}, r.Tasks[0].ID)
	go func() {
		_, e := researchAttempt(first, fileResearchStore(root), "model", json.RawMessage(`{"task":1}`), func(call context.Context, _ inputResearchRun, a inputResearchAttempt) (json.RawMessage, error) {
			_, e := changeInputResearch(root, func(r *inputResearchRun) error { return researchModelAvailability(r, a.ID, true) })
			if e != nil {
				return nil, e
			}
			close(waiting)
			<-call.Done()
			return nil, call.Err()
		})
		done <- e
	}()
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("no waiting request")
	}
	calls := 0
	second := context.WithValue(ctx, researchTaskContextKey{}, r.Tasks[1].ID)
	_, e := researchAttempt(second, fileResearchStore(root), "model", json.RawMessage(`{"task":2}`), func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		calls++
		return nil, nil
	})
	if !errors.Is(e, errResearchRequestLimit) || calls != 0 {
		t.Fatalf("overspent reserved budget: %v %d", e, calls)
	}
	cancel()
	<-done
}
func TestResearchDelegationRejectsSiblingSourcesAndNestedDelegation(t *testing.T) {
	s, root, r := delegatedFixture(t, 1)
	ops := delegatedOps(s)
	r.Sources = []inputResearchSource{{ID: "allowed", Kind: "page", URL: r.Seeds[0], Text: "operates in Canada.", Verification: "verified"}, {ID: "sibling", Kind: "page", URL: r.Seeds[1], Text: "operates in Canada.", Verification: "verified"}}
	task := newResearchTask(r, "candidate", r.Candidates[0].ID, r.Candidates[0].Name, "Prepare company 1.")
	task.CandidateID = r.Candidates[0].ID
	task.SourceIDs = []string{"allowed"}
	task.State = "running"
	r.Candidates[0].TaskID = task.ID
	r.Tasks = []researchTask{task}
	if e := writeInputResearch(root, r); e != nil {
		t.Fatal(e)
	}
	raw := delegatedResult(taskPromptFixture{Sources: []inputResearchSource{r.Sources[1]}})
	var action inputResearchAction
	_ = json.Unmarshal(raw, &action)
	if validateResearchTaskRecords(r, task, action.Arguments) == nil {
		t.Fatal("out-of-scope record admitted")
	}
	if e := applyResearchTaskAction(context.Background(), fileResearchStore(root), ops, task, "", inputResearchAction{Tool: "delegate", Arguments: json.RawMessage(`{}`)}, &sync.Mutex{}); e == nil {
		t.Fatal("nested delegation admitted")
	}
}

func TestResearchDelegationDiscoveryQueuesCandidatesAndCannotSaveDirectly(t *testing.T) {
	s, root, r := delegatedFixture(t, 2)
	r.Candidates = nil
	r.Sources = nil
	if e := writeInputResearch(root, r); e != nil {
		t.Fatal(e)
	}
	ops := delegatedOps(s)
	var mu sync.Mutex
	turns := map[string]int{}
	ops.source = func(_ context.Context, _ inputResearchRun, a inputResearchAttempt, args json.RawMessage) (inputResearchSource, error) {
		_, query := researchRequestCoordinates(a.Kind, args)
		return inputResearchSource{ID: a.ID, Kind: "search", URLs: []string{"https://a.example/"}, Query: query, Text: "A company lead", Verification: "verified"}, nil
	}
	ops.model = func(_ context.Context, _ inputResearchAI, prompt string, _ func(bool) error) (json.RawMessage, error) {
		var p taskPromptFixture
		_ = json.Unmarshal([]byte(prompt), &p)
		mu.Lock()
		turns[p.Task.ID]++
		turn := turns[p.Task.ID]
		mu.Unlock()
		if turn == 1 {
			return recoveryAction("search", map[string]any{"query": "independent sector"}), nil
		}
		return recoveryAction("candidates", map[string]any{"candidates": []any{map[string]any{"name": "Company 1", "urlRef": p.URLs[0].Ref}}}), nil
	}
	args := json.RawMessage(`{"tasks":[{"title":"Sector discovery","objective":"Find firms in the assigned sector."}]}`)
	if e := queueResearchDiscoveryTasks(fileResearchStore(root), args); e != nil {
		t.Fatal(e)
	}
	if e := runQueuedResearchTasks(context.Background(), fileResearchStore(root), ops); e != nil {
		t.Fatal(e)
	}
	saved, e := readInputResearch(root)
	if e != nil {
		t.Fatal(e)
	}
	if len(saved.Candidates) != 1 || len(saved.Records) != 0 || saved.Tasks[0].State != "complete" {
		t.Fatalf("%+v", saved.Tasks)
	}
	if e := queueResearchDiscoveryTasks(fileResearchStore(root), args); e == nil {
		t.Fatal("same task delegated twice")
	}
}
func TestResearchDelegationSelectedRowsKeepTheirScopeAndReviews(t *testing.T) {
	s, root, r := researchV2Fixture(t)
	r = v2Apply(t, root, v2Record(r, "Example"), v2Record(r, "Another"))
	if e := s.publishResearchRecords(r); e != nil {
		t.Fatal(e)
	}
	doc := v2Rows(t, s, r.PreparationID).Document
	snapshots := researchRecordSnapshots(doc)
	for i := range snapshots {
		snapshots[i].SourceIDs = []string{r.Sources[0].ID}
	}
	r.ID = "research_" + strings.Repeat("d", 32)
	r.Recipe.Execution = &delegation.Policy{Approach: "delegated", Concurrency: 2}
	execution := delegation.Resolve(r.Recipe.Execution, 2)
	r.Execution = &execution
	r.Reprocess = true
	r.Selection = snapshots
	r.Existing = snapshots
	r.Recipe.Limits.Records = 2
	r.Recipe.Limits.ModelCalls = 10
	r.Recipe.Search = nil
	r.State = "running"
	r.Step = 0
	r.Attempts = nil
	r.Records = nil
	r.Outcomes = nil
	r.PublicationPending = false
	r.Tasks = nil
	r.SourceRuns = map[string]string{}
	for _, source := range r.Sources {
		r.SourceRuns[source.ID] = "research_" + strings.Repeat("a", 32)
	}
	runs, _ := s.inputResearchRoot(true)
	defer runs.Close()
	next, e := openInputResearch(runs, r.ID, true)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	if e = writeInputResearch(next, r); e != nil {
		t.Fatal(e)
	}
	ops := delegatedOps(s)
	ops.source = func(context.Context, inputResearchRun, inputResearchAttempt, json.RawMessage) (inputResearchSource, error) {
		return inputResearchSource{}, errors.New("selected scope attempted web access")
	}
	ops.model = func(_ context.Context, _ inputResearchAI, prompt string, _ func(bool) error) (json.RawMessage, error) {
		var p taskPromptFixture
		_ = json.Unmarshal([]byte(prompt), &p)
		if p.Task.RecordID == "" || p.Task.Profile != "selected" {
			return nil, errors.New("missing selected task")
		}
		return delegatedResult(p), nil
	}
	executeInputResearch(context.Background(), fileResearchStore(next), r, ops)
	saved, e := readInputResearch(next)
	if e != nil {
		t.Fatal(e)
	}
	if len(saved.Records) != 2 || countResearchOutcomes(saved).Unchanged != 2 {
		t.Fatalf("%s %+v tasks %+v", saved.Problem, saved.Outcomes, saved.Tasks)
	}
	for _, a := range saved.Attempts {
		if a.Kind != "model" {
			t.Fatal("selected scope dispatched source")
		}
	}
	if len(v2Rows(t, s, r.PreparationID).Document.Records) != 2 {
		t.Fatal("selected reprocessing created extra rows")
	}
}
func TestResearchDelegationConcurrentRetriesKeepSeparatePendingState(t *testing.T) {
	root := retryFixture(t, 6)
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for _, query := range []string{"one", "two"} {
		wg.Add(1)
		go func(query string) {
			defer wg.Done()
			calls := 0
			args, _ := json.Marshal(map[string]string{"query": query})
			_, e := researchAttemptWithWait(context.Background(), fileResearchStore(root), "search", args, func(_ context.Context, _ inputResearchRun, a inputResearchAttempt) (json.RawMessage, error) {
				calls++
				if calls == 1 {
					return nil, retryFailure("search-temporary")
				}
				if a.Number != 2 || a.RetryOf == "" {
					return nil, errors.New("retry identity lost")
				}
				return json.RawMessage(`{"text":"saved","bytes":0}`), nil
			}, func(context.Context, time.Time) error { arrived <- struct{}{}; <-release; return nil })
			failures <- e
		}(query)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-time.After(time.Second):
			t.Fatal("no independent pending retry")
		}
	}
	r, e := readInputResearch(root)
	if e != nil || len(researchPendingRetries(&r)) != 2 {
		t.Fatalf("pending retry overwritten: %v %+v", e, r.PendingRetries)
	}
	close(release)
	wg.Wait()
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	r, e = readInputResearch(root)
	if e != nil || len(r.Attempts) != 4 || r.PendingRetry != nil || len(r.PendingRetries) != 0 {
		t.Fatalf("retry bookkeeping failed: %v %+v", e, r.Attempts)
	}
}
func TestResearchDelegationStopCancelsAllActiveTasks(t *testing.T) {
	s, root, r := delegatedFixture(t, 2)
	ops := delegatedOps(s)
	started := make(chan struct{}, 2)
	ops.model = func(ctx context.Context, _ inputResearchAI, _ string, _ func(bool) error) (json.RawMessage, error) {
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	done := make(chan struct{})
	go func() { defer close(done); executeInputResearch(context.Background(), fileResearchStore(root), r, ops) }()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("tasks did not start")
		}
	}
	if _, e := changeInputResearch(root, func(r *inputResearchRun) error { r.StopRequested = true; return nil }); e != nil {
		t.Fatal(e)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not cancel every child")
	}
	saved, e := readInputResearch(root)
	if e != nil {
		t.Fatal(e)
	}
	if saved.State != "stopped" {
		t.Fatal(saved.State)
	}
	for _, task := range saved.Tasks {
		if task.State != "interrupted" {
			t.Fatal(task.State)
		}
	}
}

func TestResearchDelegationCoordinatorResumesAfterDiscoveryAndCandidateTasks(t *testing.T) {
	s, root, r := delegatedFixture(t, 2)
	r.Candidates = nil
	if err := writeInputResearch(root, r); err != nil {
		t.Fatal(err)
	}
	ops := delegatedOps(s)
	var parents atomic.Int32
	ops.model = func(ctx context.Context, _ inputResearchAI, prompt string, available func(bool) error) (json.RawMessage, error) {
		var p taskPromptFixture
		if err := json.Unmarshal([]byte(prompt), &p); err != nil {
			return nil, err
		}
		switch p.Task.Profile {
		case "discovery":
			return recoveryAction("candidates", map[string]any{"candidates": []any{map[string]any{"name": "Company 1", "urlRef": p.URLs[0].Ref}}}), nil
		case "candidate":
			return delegatedResult(p), nil
		default:
			if parents.Add(1) == 1 {
				return recoveryAction("delegate", map[string]any{"tasks": []any{map[string]any{"title": "Discover firms", "objective": "Find firms in the supplied scope."}}}), nil
			}
			return recoveryAction("finish", map[string]any{"reason": "The available candidate was prepared."}), nil
		}
	}
	executeInputResearch(context.Background(), fileResearchStore(root), r, ops)
	saved, err := readInputResearch(root)
	if err != nil {
		t.Fatal(err)
	}
	if parents.Load() != 2 || len(saved.Tasks) != 2 || len(saved.Records) != 1 || saved.State != "completed-with-gaps" {
		t.Fatalf("parents=%d state=%s tasks=%d records=%d problem=%s", parents.Load(), saved.State, len(saved.Tasks), len(saved.Records), saved.Problem)
	}
	models := 0
	for _, a := range saved.Attempts {
		if a.Kind == "model" {
			models++
		}
	}
	if models != 4 || saved.Step != models {
		t.Fatalf("shared model accounting: step=%d requests=%d", saved.Step, models)
	}
	if len(v2Rows(t, s, r.PreparationID).Document.Records) != 1 {
		t.Fatal("child record was not saved")
	}
}
