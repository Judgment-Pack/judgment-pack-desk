package desk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSubscriptionEnableHoldsRetainedRelease(t *testing.T) {
	for _, scenario := range []string{"changed-release", "wrong-job", "upgrade-after-read"} {
		t.Run(scenario, func(t *testing.T) {
			job := "job_" + strings.Repeat("a", 32)
			b := inputEvaluation{Version: 1, ID: "eval_" + strings.Repeat("b", 32), PreparationID: "prep_" + strings.Repeat("c", 32), JobID: job, ReleaseID: "release_original", ConfirmedAt: "retained", Rows: []inputEvaluationRow{}, Subscription: &inputSubscription{Mode: "new", Enabled: true, StatePending: true, Initialized: true, TriggerID: "trg_original"}}
			calls := []string{}
			companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				switch r.URL.Path {
				case "/v1/jobs/" + job + "/triggers":
					writeJSON(w, 200, map[string]any{"items": []any{map[string]any{"id": b.Subscription.TriggerID, "revision": 7, "paused": true, "config": subscriptionTrigger()}}})
				case "/v1/jobs/" + job:
					id, release := job, b.ReleaseID
					if scenario == "changed-release" {
						release = "release_new"
					}
					if scenario == "wrong-job" {
						id = "job_other"
					}
					writeJSON(w, 200, map[string]any{"job": map[string]any{"id": id, "releaseId": release}})
				case "/v1/triggers/trg_original/state":
					var body struct {
						Revision int
						Paused   bool
					}
					json.NewDecoder(r.Body).Decode(&body)
					if scenario != "upgrade-after-read" || body.Revision != 7 || body.Paused {
						t.Errorf("unchecked or changed enable: %s %+v", scenario, body)
					}
					// Runner upgraded after the job read and now owns trigger revision 8.
					writeJSON(w, 409, map[string]any{"error": map[string]string{"message": "Trigger revision changed"}})
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					http.Error(w, "unexpected", 500)
				}
			}))
			defer companion.Close()
			s := &Server{jobs: &jobsCompanion{url: companion.URL, token: "private", done: make(chan struct{})}}
			root, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			journal := s.evaluationJournal(root, b.ID)
			saved, err := journal.CompareAndSwap(t.Context(), b, "")
			if err != nil {
				t.Fatal(err)
			}
			checkpoint := evaluationCheckpoint{journal: journal, revision: saved.SHA256}
			s.advanceInputSubscription(context.Background(), &checkpoint, &b)
			want := []string{"GET /v1/jobs/" + job + "/triggers", "GET /v1/jobs/" + job}
			if scenario == "upgrade-after-read" {
				want = append(want, "POST /v1/triggers/trg_original/state")
			}
			if !sameEvaluationValue(calls, want) || b.State != "blocked" || !b.Subscription.StatePending || b.ReleaseID != "release_original" || len(b.Rows) != 0 {
				t.Fatal(calls, b)
			}
		})
	}
}
