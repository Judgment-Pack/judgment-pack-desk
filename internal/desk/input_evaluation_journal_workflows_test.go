package desk

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type interceptedEvaluationJournal struct {
	evaluationJournal
	write func(context.Context, inputEvaluation, string) (evaluationSnapshot, error)
}

func (j interceptedEvaluationJournal) CompareAndSwap(ctx context.Context, b inputEvaluation, revision string) (evaluationSnapshot, error) {
	return j.write(ctx, b, revision)
}

type evaluationProviderFunc func(string) evaluationJournal

func (f evaluationProviderFunc) Batch(id string) evaluationJournal { return f(id) }

func TestEvaluationJournalUncertainJobAcknowledgementStopsDispatch(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		for _, phase := range []string{"before", "after"} {
			t.Run(backend+"/"+phase, func(t *testing.T) {
				journal, b, _ := evaluationJournalFixture(t, backend)
				b.ConfirmedAt = "confirmed"
				b.State = "queued"
				saved, err := journal.CompareAndSwap(t.Context(), b, "")
				if err != nil {
					t.Fatal(err)
				}
				writes := 0
				fault := interceptedEvaluationJournal{evaluationJournal: journal, write: func(ctx context.Context, b inputEvaluation, revision string) (evaluationSnapshot, error) {
					writes++
					if phase == "after" {
						if _, err := journal.CompareAndSwap(ctx, b, revision); err != nil {
							return evaluationSnapshot{}, err
						}
					}
					return evaluationSnapshot{}, errors.New("job acknowledgement persistence unavailable")
				}}
				jobs, runs := 0, 0
				jobID := "job_" + strings.Repeat("d", 32)
				runID := "run_" + strings.Repeat("e", 32)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/v1/jobs":
						jobs++
						writeJSON(w, 201, map[string]any{"id": jobID, "name": b.Name, "releaseId": b.ReleaseID})
					case "/v1/jobs/" + jobID + "/runs":
						runs++
						if r.Header.Get("Idempotency-Key") != b.ID+":"+b.Rows[0].RecordID {
							t.Error("changed submission identity")
						}
						writeJSON(w, 200, map[string]any{"id": runID, "jobId": jobID, "state": "completed", "result": map[string]any{"disposition": map[string]any{"kind": "outcome", "outcomeId": "keep"}}, "signing": map[string]any{"status": "failed"}})
					default:
						t.Errorf("unexpected dispatch: %s", r.URL.Path)
					}
				}))
				defer server.Close()
				s := &Server{jobs: &jobsCompanion{url: server.URL, token: "private", done: make(chan struct{})}, log: log.New(io.Discard, "", 0)}
				checkpoint := evaluationCheckpoint{journal: fault, revision: saved.SHA256}
				s.advanceInputEvaluation(t.Context(), &checkpoint, &b)
				s.advanceInputEvaluation(t.Context(), &checkpoint, &b) // A poisoned checkpoint cannot be reused.
				if writes != 1 || jobs != 1 || runs != 0 {
					t.Fatal("work continued after uncertain save", writes, jobs, runs)
				}
				recovered, err := journal.Read(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				b = recovered.Document
				checkpoint = evaluationCheckpoint{journal: journal, revision: recovered.SHA256}
				s.advanceInputEvaluation(t.Context(), &checkpoint, &b)
				s.advanceInputEvaluation(t.Context(), &checkpoint, &b)
				expectedJobs := 1
				if phase == "before" {
					expectedJobs = 2
				}
				if jobs != expectedJobs || runs != 1 || b.State != "completed" {
					t.Fatal("recovery repeated execution", jobs, runs, b.State)
				}
				after, err := journal.Read(t.Context())
				if err != nil || after.Document.Rows[0].RunID != runID || !strings.Contains(string(after.Document.Rows[0].Signing), "failed") {
					t.Fatal("retained receipt missing", err)
				}
			})
		}
	}
}
func TestEvaluationJournalRefusesWrongJobBinding(t *testing.T) {
	for _, field := range []string{"releaseId", "name", "initialTriggerId"} {
		t.Run(field, func(t *testing.T) {
			journal, b, _ := evaluationJournalFixture(t, "memory")
			b.ConfirmedAt = "confirmed"
			b.State = "queued"
			saved, err := journal.CompareAndSwap(t.Context(), b, "")
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/jobs" {
					t.Error("row dispatched for unbound job")
				}
				reply := map[string]any{"id": "job_" + strings.Repeat("d", 32), "name": b.Name, "releaseId": b.ReleaseID}
				reply[field] = "different"
				writeJSON(w, 200, reply)
			}))
			defer server.Close()
			s := &Server{jobs: &jobsCompanion{url: server.URL, token: "private", done: make(chan struct{})}}
			checkpoint := evaluationCheckpoint{journal: journal, revision: saved.SHA256}
			if err := s.ensureEvaluationJob(t.Context(), &checkpoint, &b); err == nil || b.JobID != "" {
				t.Fatal("adopted unrelated job", err)
			}
			after, err := journal.Read(t.Context())
			if err != nil || after.SHA256 != saved.SHA256 {
				t.Fatal("changed journal", err)
			}
		})
	}
}
func TestEvaluationJournalHTTPUnreadableSubscriptionsRemainUnavailable(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			s, _ := newTestServer(t, false)
			if backend == "memory" {
				s.evaluationJournals = newMemoryEvaluationJournals()
			}
			root, err := s.inputEvaluationRoot(true)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			_, b, _ := evaluationJournalFixture(t, backend)
			b.Rows = nil
			b.ConfirmedAt = "confirmed"
			b.Subscription = &inputSubscription{Mode: "changed", SourceClass: "asserted"}
			if err = s.createEvaluationReview(t.Context(), root, b); err != nil {
				t.Fatal(err)
			}
			request := func(path string) *httptest.ResponseRecorder {
				req := httptest.NewRequest("GET", path, nil)
				req.Header.Set("Authorization", "Bearer "+policyTestSession(t, s))
				w := httptest.NewRecorder()
				s.ServeHTTP(w, req)
				return w
			}
			if w := request("/api/input-subscriptions"); w.Code != 200 || !strings.Contains(w.Body.String(), b.ID) {
				t.Fatal("saved subscription missing", w.Code, w.Body.String())
			}
			if backend == "memory" {
				j := s.evaluationJournals.Batch(b.ID).(memoryEvaluationJournal)
				j.entry.raw = []byte(`{}`)
			} else {
				br, err := openInputEvaluation(root, b.ID, false)
				if err != nil {
					t.Fatal(err)
				}
				defer br.Close()
				if err = writePrivateData(br, "record.json", []byte(`{}`)); err != nil {
					t.Fatal(err)
				}
			}
			for _, path := range []string{"/api/input-subscriptions", "/api/input-preparations/" + b.PreparationID + "/evaluations"} {
				if w := request(path); w.Code != 409 || strings.Contains(w.Body.String(), `"items":[]`) {
					t.Fatal("unavailable reported as empty", w.Code, w.Body.String())
				}
			}
			if backend == "memory" {
				assertNoEvaluationFiles(t, s)
			}
		})
	}
}
func TestEvaluationJournalUncertainReviewKeepsStagingHidden(t *testing.T) {
	s, _ := newTestServer(t, false)
	repo := newMemoryEvaluationJournals()
	calls := 0
	s.evaluationJournals = evaluationProviderFunc(func(id string) evaluationJournal {
		j := repo.Batch(id)
		return interceptedEvaluationJournal{evaluationJournal: j, write: func(ctx context.Context, b inputEvaluation, revision string) (evaluationSnapshot, error) {
			calls++
			if _, err := j.CompareAndSwap(ctx, b, revision); err != nil {
				return evaluationSnapshot{}, err
			}
			return evaluationSnapshot{}, errors.New("acknowledgement unavailable")
		}}
	})
	root, err := s.inputEvaluationRoot(true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	_, b, _ := evaluationJournalFixture(t, "memory")
	if err = s.createEvaluationReview(t.Context(), root, b); err == nil || calls != 1 {
		t.Fatal("uncertain create acknowledged/repeated", err, calls)
	}
	if _, err := root.Stat(".review-" + b.ID); err != nil {
		t.Fatal("lost uncertain staging", err)
	}
	if _, err := root.Stat(b.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("uncertain review published", err)
	}
	if err = s.createEvaluationReview(t.Context(), root, b); !errors.Is(err, os.ErrExist) || calls != 1 {
		t.Fatal("staged review replaced", err, calls)
	}
	req := httptest.NewRequest("GET", "/api/input-preparations/"+b.PreparationID+"/evaluations", nil)
	req.Header.Set("Authorization", "Bearer "+policyTestSession(t, s))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 || strings.Contains(w.Body.String(), b.ID) {
		t.Fatal("hidden review became visible", w.Code, w.Body.String())
	}
	assertNoEvaluationFiles(t, s)
}
