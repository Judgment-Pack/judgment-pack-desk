package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type subscriptionCapacityHarness struct {
	t                      *testing.T
	journal                evaluationJournal
	b                      inputEvaluation
	server                 func() *Server
	repo                   inputPreparationRepository
	mu                     sync.Mutex
	full                   int
	successesBeforeRefusal int
	receipt                bool
	receiptState           string
	status                 int
	code                   string
	refuseAt               string
	paused                 bool
	reads                  int
	bodies                 [][]byte
	admitted               map[string]int
}

// Start from two retained outbox intents. No Runtime, model, source acquisition,
// background workers or user project is needed to exercise delivery recovery.
func newSubscriptionCapacityHarness(t *testing.T, backend string) *subscriptionCapacityHarness {
	t.Helper()
	journal, b, _ := evaluationJournalFixture(t, backend)
	project := t.TempDir()
	writeProject(t, project, map[string]string{"pack.json": qualificationTestPack, "jpack.json": `{"configVersion":"2","packs":{"example":{"path":"pack.json"}}}`})
	root, err := os.OpenRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	doc := repositoryDocument(1)
	doc.ID = b.PreparationID
	doc.Recipe.Qualification = qualificationFixture()
	doc.Records = append(doc.Records, preparedInputRecord{ID: "row-2", Name: "Next", Class: "asserted", Status: "provided", Values: map[string]string{"count": "00234"}})
	repo := &memoryPreparationRepository{data: map[string][]byte{}}
	if _, err = repo.CompareAndSwap(t.Context(), doc, ""); err != nil {
		t.Fatal(err)
	}
	b.JobID = "job_" + strings.Repeat("d", 32)
	b.ConfirmedAt = "2026-10-15T12:01:00Z"
	b.State = "watching"
	b.Subscription = &inputSubscription{Mode: "changed", SourceClass: "asserted", IncludeExisting: true, Enabled: true, Initialized: true, TriggerID: "trg_" + strings.Repeat("e", 32)}
	b.Rows = nil
	for i, record := range doc.Records {
		b.Rows = append(b.Rows, inputEvaluationRow{RecordID: record.ID, RecordDigest: inputRecordDigest(record), EventID: subscriptionIdentity(b, record), EventAt: "2026-10-15T12:01:01Z", State: "pending", Input: json.RawMessage([]string{`{"facts":{"number":24,"decimal":"24","zero":0,"false":false,"null":null}}`, `{"facts":{"number":25,"decimal":"25","zero":0,"false":false,"null":null}}`}[i])})
	}
	h := &subscriptionCapacityHarness{t: t, journal: journal, b: b, repo: repo, full: 12, status: 429, code: "queue_full", admitted: map[string]int{}}
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.full > 0 && h.refuseAt != "" && h.refuseAt == r.Method+" "+r.URL.Path {
			h.full--
			writeJSON(w, h.status, map[string]any{"error": map[string]string{"code": h.code, "message": "Runner refused this request"}})
			return
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/jobs/"+b.JobID+"/triggers":
			writeJSON(w, 200, map[string]any{"items": []any{map[string]any{"id": b.Subscription.TriggerID, "revision": 1, "paused": h.paused, "config": subscriptionTrigger()}}})
		case r.Method == "POST" && r.URL.Path == "/v1/triggers/"+b.Subscription.TriggerID+"/deliveries":
			raw, _ := io.ReadAll(r.Body)
			h.bodies = append(h.bodies, raw)
			if h.full > 0 && h.refuseAt == "" && h.successesBeforeRefusal == 0 {
				h.full--
				writeJSON(w, h.status, map[string]any{"error": map[string]string{"code": h.code, "message": "Runner refused this request"}})
				return
			}
			var request struct{ Event struct{ ID string } }
			if err := json.Unmarshal(raw, &request); err != nil {
				t.Error(err)
			}
			if h.successesBeforeRefusal > 0 {
				h.successesBeforeRefusal--
			}
			h.admitted[request.Event.ID]++
			writeJSON(w, 200, map[string]any{"occurrence": map[string]string{"id": "occ_" + request.Event.ID, "state": "completed"}})
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/triggers/"+b.Subscription.TriggerID+"/deliveries/"):
			h.reads++
			if h.receipt {
				state := h.receiptState
				if state == "" {
					state = "skipped"
				}
				writeJSON(w, 200, map[string]any{"occurrence": map[string]string{"id": "occ_recovered", "state": state, "jobId": b.JobID, "releaseId": b.ReleaseID, "triggerId": b.Subscription.TriggerID, "eventId": strings.TrimPrefix(r.URL.Path, "/v1/triggers/"+b.Subscription.TriggerID+"/deliveries/")}})
				return
			}
			writeJSON(w, 404, map[string]any{"error": map[string]string{"code": "not_found", "message": "No delivery admitted"}})
		case r.Method == "POST" && r.URL.Path == "/v1/triggers/"+b.Subscription.TriggerID+"/state":
			var request struct{ Paused bool }
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			h.paused = request.Paused
			writeJSON(w, 200, map[string]any{})
		default:
			t.Errorf("unexpected Runner call: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	t.Cleanup(companion.Close)
	h.server = func() *Server {
		return &Server{root: root, cfg: Config{ProjectDir: project, RunnerAllowUntestedReleases: true}, inputPreparations: repo, evaluationJournals: evaluationProviderFunc(func(string) evaluationJournal { return journal }), jobs: &jobsCompanion{url: companion.URL, token: "private", done: make(chan struct{})}}
	}
	// The local admission lease remains a filesystem concern for every journal.
	evaluations, err := h.server().inputEvaluationRoot(true)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := openInputEvaluation(evaluations, b.ID, true)
	evaluations.Close()
	if err != nil {
		t.Fatal(err)
	}
	batch.Close()
	if _, err = journal.CompareAndSwap(t.Context(), b, ""); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *subscriptionCapacityHarness) tick(expire bool) {
	h.t.Helper()
	saved, err := h.journal.Read(h.t.Context())
	if err != nil {
		h.t.Fatal(err)
	}
	if expire {
		saved.Document.Subscription.RetryAt = ""
		if saved.Document.Subscription.CapacityRetryAt != "" {
			saved.Document.Subscription.CapacityRetryAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
		}
		saved, err = h.journal.CompareAndSwap(h.t.Context(), saved.Document, saved.SHA256)
		if err != nil {
			h.t.Fatal(err)
		}
	}
	h.b = saved.Document
	checkpoint := evaluationCheckpoint{journal: h.journal, revision: saved.SHA256}
	h.server().advanceInputSubscription(context.Background(), &checkpoint, &h.b)
	if checkpoint.failure != nil {
		h.t.Fatal(checkpoint.failure)
	}
	retained, err := h.journal.Read(h.t.Context())
	if err != nil {
		h.t.Fatal(err)
	}
	h.b = retained.Document
}

func TestInputSubscriptionCapacityWait(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			h := newSubscriptionCapacityHarness(t, backend)
			original := h.b.Rows[0]
			for attempt := 0; attempt < 12; attempt++ {
				h.tick(true)
				if h.b.State == "blocked" || !h.b.Subscription.Enabled || h.b.Rows[0].State != "pending" {
					t.Fatalf("full queue blocked retained delivery on refusal %d: state=%s failures=%d", attempt+1, h.b.State, h.b.Subscription.Failures)
				}
				if h.b.Rows[0].EventID != original.EventID || h.b.Rows[0].EventAt != original.EventAt || !bytes.Equal(h.b.Rows[0].Input, original.Input) {
					t.Fatal("capacity retry changed retained intent")
				}
				wantDelay := 2 << min(attempt, 7)
				at, err := time.Parse(time.RFC3339Nano, h.b.Subscription.CapacityRetryAt)
				if err != nil || time.Until(at) <= time.Duration(wantDelay-1)*time.Second || time.Until(at) > time.Duration(wantDelay)*time.Second || h.b.Subscription.CapacityBackoff != wantDelay || h.b.Subscription.Failures != 0 || h.b.Problem != "" {
					t.Fatal("capacity wait not bounded and persisted separately", h.b.Subscription, h.b.Problem, err)
				}
				// A fresh coordinator must retain the wait without submitting again.
				h.tick(false)
				h.mu.Lock()
				calls := len(h.bodies)
				h.mu.Unlock()
				if calls != attempt+1 {
					t.Fatal("restart bypassed persisted capacity wait", calls)
				}
			}
			h.tick(true)
			h.mu.Lock()
			defer h.mu.Unlock()
			if len(h.admitted) != 2 || h.admitted[original.EventID] != 1 || h.admitted[h.b.Rows[1].EventID] != 1 {
				t.Fatal("delivery lost or admitted twice", h.admitted)
			}
			for _, raw := range h.bodies[:13] {
				if !bytes.Equal(raw, h.bodies[0]) {
					t.Fatal("redelivery changed exact envelope")
				}
			}
			if h.b.State != "watching" || h.b.Subscription.Failures != 0 || len(h.b.Rows[0].Input) != 0 || h.b.Subscription.CapacityBackoff != 0 || h.b.Subscription.CapacityRetryAt != "" {
				t.Fatal("successful delivery not retained", h.b)
			}
		})
	}
}

func TestInputSubscriptionCapacityWaitPause(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		for _, control := range []string{"Desk", "Runner"} {
			t.Run(backend+"/"+control, func(t *testing.T) {
				h := newSubscriptionCapacityHarness(t, backend)
				h.tick(false)
				original := h.b.Rows[0]
				if control == "Desk" {
					s := h.server()
					view, err := h.repo.Read(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					saved, err := view.Get(h.b.PreparationID)
					if err != nil {
						t.Fatal(err)
					}
					request := httptest.NewRequest("POST", "/api/input-preparations/"+h.b.PreparationID+"/evaluations/"+h.b.ID+"/subscription-state", strings.NewReader(`{"reviewed":true,"enabled":false}`))
					response := httptest.NewRecorder()
					s.confirmInputEvaluation(response, request, view, saved, h.b.ID)
					view.Close()
					if response.Code != 200 {
						t.Fatal("pause refused", response.Code, response.Body.String())
					}
				} else {
					h.mu.Lock()
					h.paused = true
					h.mu.Unlock()
				}
				h.tick(false)
				h.tick(false)
				h.mu.Lock()
				defer h.mu.Unlock()
				if h.b.State != "paused" || h.b.Subscription.Enabled || h.b.Subscription.CapacityRetryAt != "" || h.b.Subscription.CapacityBackoff != 0 || len(h.bodies) != 1 || h.reads == 0 {
					t.Fatal("pause did not close admission and allow read-only recovery", h.b, len(h.bodies), h.reads)
				}
				if !bytes.Equal(original.Input, h.b.Rows[0].Input) || original.EventID != h.b.Rows[0].EventID || original.EventAt != h.b.Rows[0].EventAt {
					t.Fatal("pause discarded pending intent")
				}
			})
		}
	}
}

func TestInputSubscriptionCapacityDoesNotExcuseOtherRefusals(t *testing.T) {
	for _, scenario := range []struct {
		name          string
		status        int
		code          string
		otherEndpoint bool
	}{
		{"other429", 429, "rate_limited", false}, {"uncoded429", 429, "", false}, {"server503", 503, "queue_full", false}, {"triggerRead", 429, "queue_full", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			h := newSubscriptionCapacityHarness(t, "file")
			h.tick(false) // First establish a capacity wait to check that errors clear it.
			h.mu.Lock()
			h.full, h.status, h.code = 5, scenario.status, scenario.code
			if scenario.otherEndpoint {
				h.refuseAt = "GET /v1/jobs/" + h.b.JobID + "/triggers"
			}
			h.mu.Unlock()
			for attempt := 1; attempt <= 5; attempt++ {
				h.tick(true)
				if h.b.Subscription.Failures != attempt || h.b.Subscription.CapacityBackoff != 0 || h.b.Subscription.CapacityRetryAt != "" || h.b.Problem == "" {
					t.Fatal("ordinary refusal counted as capacity", h.b)
				}
				if attempt < 5 && (h.b.State != "queued" || h.b.Subscription.RetryAt == "") {
					t.Fatal("ordinary retry changed", h.b)
				}
			}
			if h.b.State != "blocked" || h.b.Subscription.RetryAt != "" {
				t.Fatal("ordinary refusals no longer block", h.b)
			}
		})
	}
}

func TestInputSubscriptionCapacityWaitJournalGuards(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			h := newSubscriptionCapacityHarness(t, backend)
			h.tick(false)
			for _, mutate := range []func(*inputEvaluation){
				func(b *inputEvaluation) { b.Subscription.CapacityBackoff = -2 },
				func(b *inputEvaluation) { b.Subscription.CapacityBackoff = 1 },
				func(b *inputEvaluation) { b.Subscription.CapacityBackoff = 3 },
				func(b *inputEvaluation) { b.Subscription.CapacityBackoff = 512 },
				func(b *inputEvaluation) { b.Subscription.CapacityBackoff = 0 },
				func(b *inputEvaluation) { b.Subscription.CapacityRetryAt = "" },
				func(b *inputEvaluation) { b.Subscription.CapacityRetryAt = "invalid" },
				func(b *inputEvaluation) { b.Subscription.RetryAt = b.Subscription.CapacityRetryAt },
				func(b *inputEvaluation) { b.Subscription.Enabled = false },
				func(b *inputEvaluation) { b.Subscription.StatePending = true },
				func(b *inputEvaluation) { b.State = "blocked" },
				func(b *inputEvaluation) { b.Problem = "failed" },
				func(b *inputEvaluation) {
					for i := range b.Rows {
						b.Rows[i].State = "superseded"
						b.Rows[i].Input = nil
					}
				},
			} {
				saved, err := h.journal.Read(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				mutate(&saved.Document)
				if _, err = h.journal.CompareAndSwap(t.Context(), saved.Document, saved.SHA256); err == nil {
					t.Fatal("invalid capacity state persisted")
				}
				if raw, err := json.Marshal(saved.Document); err != nil {
					t.Fatal(err)
				} else if _, err = decodeEvaluationSnapshot(raw, h.b.ID); err == nil {
					t.Fatal("invalid capacity state restored")
				}
			}
			// Scheduling metadata is mutable, but it still requires the latest CAS.
			before, err := h.journal.Read(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			before.Document.Subscription.CapacityBackoff = 256
			before.Document.Subscription.CapacityRetryAt = time.Now().Add(256 * time.Second).UTC().Format(time.RFC3339Nano)
			if _, err = h.journal.CompareAndSwap(t.Context(), before.Document, before.SHA256); err != nil {
				t.Fatal("bounded capacity update refused", err)
			}
			before.Document.Subscription.clearCapacityWait()
			if _, err = h.journal.CompareAndSwap(t.Context(), before.Document, before.SHA256); !errors.Is(err, errEvaluationConflict) {
				t.Fatal("stale coordinator changed wait", err)
			}
		})
	}
}

func TestInputSubscriptionCapacityKeepsOrdinaryFailureCount(t *testing.T) {
	h := newSubscriptionCapacityHarness(t, "file")
	saved, err := h.journal.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	saved.Document.Subscription.Failures = 4
	if _, err = h.journal.CompareAndSwap(t.Context(), saved.Document, saved.SHA256); err != nil {
		t.Fatal(err)
	}
	for range 6 {
		h.tick(true)
		if h.b.Subscription.Failures != 4 || h.b.State == "blocked" {
			t.Fatal("capacity changed ordinary failure count", h.b)
		}
	}
	h.mu.Lock()
	h.status, h.code = 503, "unavailable"
	h.mu.Unlock()
	h.tick(true)
	if h.b.State != "blocked" || h.b.Subscription.Failures != 5 || h.b.Subscription.CapacityRetryAt != "" {
		t.Fatal("ordinary failure budget was reset by capacity waits", h.b)
	}
}

func TestInputSubscriptionCapacityWaitRechecksRevocation(t *testing.T) {
	h := newSubscriptionCapacityHarness(t, "file")
	h.tick(false)
	original := h.b.Rows[0]
	saved, err := h.repo.Get(t.Context(), h.b.PreparationID)
	if err != nil {
		t.Fatal(err)
	}
	saved.Document.Records = saved.Document.Records[1:]
	if _, err = h.repo.CompareAndSwap(t.Context(), saved.Document, saved.SHA256); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	h.full = 0
	h.mu.Unlock()
	// Its retry time is still in the future. Revocation must reconcile the
	// exact receipt now, supersede the unadmitted row, then deliver the next row.
	h.tick(false)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.b.Rows[0].State != "superseded" || len(h.b.Rows[0].Input) != 0 || h.b.Rows[0].EventID != original.EventID || h.b.Rows[0].EventAt != original.EventAt || h.reads != 1 || len(h.bodies) != 2 || h.admitted[original.EventID] != 0 || h.admitted[h.b.Rows[1].EventID] != 1 {
		t.Fatal("capacity wait bypassed revocation or lost next delivery", h.b, h.reads, len(h.bodies), h.admitted)
	}
	if h.b.State != "watching" || h.b.Subscription.CapacityBackoff != 0 || h.b.Subscription.CapacityRetryAt != "" {
		t.Fatal("supersession retained stale capacity wait", h.b)
	}
}

// The normal review/enable workflow prepares both records with Runtime. Only
// the full-queue refusals are simulated; successful deliveries execute in Runner.
func TestInputSubscriptionRealRunnerCapacityWait(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			h := newSubscriptionUpgradeHarness(t, backend)
			b := h.subscription()
			h.ok(h.path+"/evaluations/"+b.ID+"/subscription-state", map[string]any{"reviewed": true, "enabled": true}, "", nil)
			h.s.resumeInputEvaluations(t.Context())
			h.importRecord("First")
			h.importRecord("Second")
			h.s.resumeInputEvaluations(t.Context())
			b = h.read(b.ID)
			if len(b.Rows) != 1 || b.Rows[0].State != "pending" {
				t.Fatal("missing prepared outbox", b)
			}
			originalRow := b.Rows[0]
			trigger := b.Subscription.TriggerID
			original := h.s.jobs
			endpoint, _, err := original.endpoint()
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var bodies [][]byte
			admitted := map[string]int{}
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				delivery := r.Method == "POST" && r.URL.Path == "/v1/triggers/"+trigger+"/deliveries"
				if delivery {
					mu.Lock()
					bodies = append(bodies, raw)
					full := len(bodies) <= 12
					mu.Unlock()
					if full {
						writeJSON(w, 429, map[string]any{"error": map[string]string{"code": "queue_full", "message": "Runner queue is full"}})
						return
					}
				}
				request, err := http.NewRequestWithContext(r.Context(), r.Method, endpoint+r.URL.RequestURI(), bytes.NewReader(raw))
				if err != nil {
					t.Error(err)
					return
				}
				request.Header = r.Header.Clone()
				response, err := http.DefaultClient.Do(request)
				if err != nil {
					t.Error(err)
					return
				}
				defer response.Body.Close()
				if delivery && response.StatusCode >= 200 && response.StatusCode < 300 {
					var event struct{ Event struct{ ID string } }
					if err := json.Unmarshal(raw, &event); err != nil {
						t.Error(err)
					}
					mu.Lock()
					admitted[event.Event.ID]++
					mu.Unlock()
				}
				w.WriteHeader(response.StatusCode)
				io.Copy(w, response.Body)
			}))
			defer proxy.Close()
			h.s.jobs = &jobsCompanion{url: proxy.URL, token: original.token, done: make(chan struct{})}
			defer func() { h.s.jobs = original }()
			journal := h.journal(b.ID)
			for attempt := 0; attempt < 12; attempt++ {
				if attempt > 0 {
					saved, err := journal.Read(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					saved.Document.Subscription.CapacityRetryAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
					if _, err = journal.CompareAndSwap(t.Context(), saved.Document, saved.SHA256); err != nil {
						t.Fatal(err)
					}
				}
				h.s.resumeInputEvaluations(t.Context())
				b = h.read(b.ID)
				if b.State == "blocked" || b.Problem != "" || b.Subscription.Failures != 0 || b.Subscription.CapacityRetryAt == "" || !sameEvaluationValue(b.Rows[0], originalRow) {
					t.Fatal("capacity refusal changed pending revision", attempt, b)
				}
			}
			saved, err := journal.Read(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			saved.Document.Subscription.CapacityRetryAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
			if _, err = journal.CompareAndSwap(t.Context(), saved.Document, saved.SHA256); err != nil {
				t.Fatal(err)
			}
			for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
				h.s.resumeInputEvaluations(t.Context())
				b = h.read(b.ID)
				if b.State == "blocked" {
					t.Fatal(b.Problem)
				}
				if len(b.Rows) == 2 && b.Rows[0].State == "completed" && b.Rows[1].State == "completed" {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(b.Rows) != 2 || b.Rows[0].State != "completed" || b.Rows[1].State != "completed" || len(admitted) != 2 || admitted[b.Rows[0].EventID] != 1 || admitted[b.Rows[1].EventID] != 1 || b.Subscription.CapacityRetryAt != "" || b.Subscription.CapacityBackoff != 0 {
				t.Fatal("full queue lost or duplicated a revision", b, admitted)
			}
			if len(bodies) != 14 {
				t.Fatal("unexpected delivery attempts", len(bodies))
			}
			for _, raw := range bodies[:13] {
				if !bytes.Equal(raw, bodies[0]) {
					t.Fatal("capacity retry changed envelope bytes")
				}
			}
			raw, err := h.s.inputRunner(t.Context(), "jobs/"+b.JobID+"/runs", nil, "")
			if err != nil {
				t.Fatal(err)
			}
			var runs struct{ Items []json.RawMessage }
			if err = json.Unmarshal(raw, &runs); err != nil || len(runs.Items) != 2 {
				t.Fatal("Runner created extra or missing runs", len(runs.Items), err)
			}
		})
	}
}

// Stop each pass on the next refusal, before the end-of-pass reset. This
// models an outbox that keeps growing without depending on Runtime backfill.
func TestInputSubscriptionSuccessResetsConsecutiveRefusals(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			h := newSubscriptionCapacityHarness(t, backend)
			h.status, h.code, h.full = 503, "unavailable", 1
			h.tick(false)
			template := h.b.Rows[1]
			for attempt := range 6 {
				h.update(func(b *inputEvaluation) {
					b.Subscription.RetryAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
					if attempt > 0 {
						row := template
						row.EventID = fmt.Sprintf("input-%064x", attempt)
						b.Rows = append(b.Rows, row)
					}
				})
				original := h.b.Rows[len(h.b.Rows)-2]
				h.mu.Lock()
				h.full, h.successesBeforeRefusal = 1, 1
				h.mu.Unlock()
				h.tick(false)
				if h.b.State != "queued" || h.b.Subscription.Failures != 1 || h.b.Subscription.RetryAt == "" {
					t.Fatal("successful admission did not restart consecutive refusal count", h.b)
				}
				delivered := h.b.Rows[len(h.b.Rows)-2]
				pending := h.b.Rows[len(h.b.Rows)-1]
				if delivered.EventID != original.EventID || delivered.EventAt != original.EventAt || delivered.OccurrenceID == "" || len(delivered.Input) != 0 || !bytes.Equal(pending.Input, template.Input) {
					t.Fatal("admission changed retained identity or pending input", h.b.Rows)
				}
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			for id, count := range h.admitted {
				if count != 1 {
					t.Fatal("duplicate delivery", id, count)
				}
			}
		})
	}
}

func (h *subscriptionCapacityHarness) update(change func(*inputEvaluation)) {
	h.t.Helper()
	saved, err := h.journal.Read(h.t.Context())
	if err != nil {
		h.t.Fatal(err)
	}
	change(&saved.Document)
	saved, err = h.journal.CompareAndSwap(h.t.Context(), saved.Document, saved.SHA256)
	if err != nil {
		h.t.Fatal(err)
	}
	h.b = saved.Document
}

func TestInputSubscriptionReceiptResetsRefusals(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		for _, mode := range []string{"revoked", "paused"} {
			t.Run(backend+"/"+mode, func(t *testing.T) {
				h := newSubscriptionCapacityHarness(t, backend)
				original := h.b.Rows[0]
				pending := h.b.Rows[1]
				h.update(func(b *inputEvaluation) {
					b.Subscription.Failures = 4
					b.Subscription.RetryAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
					if mode == "paused" {
						b.Subscription.Enabled = false
						b.State = "paused"
					}
				})
				if mode == "revoked" {
					saved, err := h.repo.Get(t.Context(), h.b.PreparationID)
					if err != nil {
						t.Fatal(err)
					}
					saved.Document.Records = saved.Document.Records[1:]
					if _, err = h.repo.CompareAndSwap(t.Context(), saved.Document, saved.SHA256); err != nil {
						t.Fatal(err)
					}
				}
				h.receipt, h.full, h.status, h.code = true, 1, 503, "unavailable"
				if mode == "paused" {
					h.paused = true
					h.refuseAt = "GET /v1/triggers/" + h.b.Subscription.TriggerID + "/deliveries/" + h.b.Rows[1].EventID
				}
				h.tick(false)
				if h.b.State != "queued" || h.b.Subscription.Failures != 1 || h.b.Subscription.RetryAt == "" || h.b.Rows[0].EventID != original.EventID || h.b.Rows[0].EventAt != original.EventAt || len(h.b.Rows[0].Input) != 0 || h.b.Rows[0].OccurrenceID != "occ_recovered" || len(h.admitted) != 0 {
					t.Fatal("receipt reconciliation retained refusal count or changed delivery identity", h.b)
				}
				if !sameEvaluationValue(h.b.Rows[1], pending) {
					t.Fatal("receipt reconciliation changed pending delivery", h.b.Rows[1])
				}
				if mode == "paused" && len(h.bodies) != 0 {
					t.Fatal("paused receipt recovery submitted a delivery")
				}
			})
		}
	}
}

func TestInputSubscriptionResumeResetsRefusals(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			h := newSubscriptionCapacityHarness(t, backend)
			h.update(func(b *inputEvaluation) {
				b.State, b.Problem = "blocked", "Runner unavailable"
				b.Subscription.Failures, b.Subscription.RetryAt = 5, time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
			})
			original := h.b.Rows
			view, err := h.repo.Read(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			saved, err := view.Get(h.b.PreparationID)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("POST", "/evaluations/"+h.b.ID+"/resume", strings.NewReader(`{"reviewed":true}`))
			response := httptest.NewRecorder()
			h.server().confirmInputEvaluation(response, request, view, saved, h.b.ID)
			if response.Code != 200 {
				t.Fatal(response.Code, response.Body.String())
			}
			retained, err := h.journal.Read(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			b := retained.Document
			if b.State != "queued" || b.Problem != "" || b.Subscription.Failures != 0 || b.Subscription.RetryAt != "" || b.Subscription.CapacityBackoff != 0 || b.Subscription.CapacityRetryAt != "" || !sameEvaluationValue(b.Rows, original) || len(h.bodies) != 0 {
				t.Fatal("resume did not reset waits while retaining exact outbox", b)
			}
		})
	}
}

func TestInputSubscriptionAgedOrdinaryRetryThenCapacityWait(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			h := newSubscriptionCapacityHarness(t, backend)
			original := h.b.Rows
			h.status, h.code, h.full = 503, "unavailable", 1
			h.tick(false)
			h.update(func(b *inputEvaluation) {
				b.Subscription.RetryAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
			})
			h.mu.Lock()
			h.status, h.code, h.full = 429, "queue_full", 2
			h.mu.Unlock()
			h.tick(false)
			if h.b.Subscription.RetryAt != "" || h.b.Subscription.CapacityBackoff != 2 || h.b.Subscription.CapacityRetryAt == "" || h.b.Subscription.Failures != 1 || !sameEvaluationValue(h.b.Rows, original) {
				t.Fatal("aged retry did not persist separate capacity wait", h.b)
			}
			h.tick(false)
			h.mu.Lock()
			defer h.mu.Unlock()
			if len(h.bodies) != 2 || len(h.admitted) != 0 {
				t.Fatal("delivery repeated before capacity retry due", len(h.bodies), h.admitted)
			}
		})
	}
}

func TestInputSubscriptionKnownOccurrencePreservesPendingWait(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			h := newSubscriptionCapacityHarness(t, backend)
			pending := h.b.Rows[1]
			retryAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
			h.update(func(b *inputEvaluation) {
				b.Rows[0].OccurrenceID, b.Rows[0].State, b.Rows[0].Input = "occ_recovered", "accepted", nil
				b.State = "queued"
				b.Subscription.Failures = 3
				b.Subscription.CapacityBackoff, b.Subscription.CapacityRetryAt = 2, retryAt
			})
			h.receipt, h.receiptState = true, "accepted"
			for range 2 {
				h.tick(false)
				if h.b.Subscription.Failures != 3 || h.b.Subscription.CapacityBackoff != 2 || h.b.Subscription.CapacityRetryAt != retryAt || h.b.Subscription.RetryAt != "" || !sameEvaluationValue(h.b.Rows[1], pending) {
					t.Fatal("known occurrence poll changed pending retry state", h.b)
				}
			}
			if h.reads != 2 || len(h.bodies) != 0 || len(h.admitted) != 0 {
				t.Fatal("known occurrence poll bypassed pending capacity wait", h.reads, len(h.bodies), h.admitted)
			}
		})
	}
}
