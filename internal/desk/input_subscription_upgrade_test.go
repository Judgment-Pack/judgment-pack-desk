package desk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type subscriptionUpgradeHarness struct {
	t     *testing.T
	s     *Server
	saved inputPreparationReply
	path  string
}

func newSubscriptionUpgradeHarness(t *testing.T, backend string) *subscriptionUpgradeHarness {
	t.Helper()
	bin, runtime := os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bin == "" || runtime == "" {
		t.Skip("requires Runner and Runtime")
	}
	project, config := t.TempDir(), t.TempDir()
	os.Chmod(config, 0700)
	writeProject(t, project, map[string]string{"pack.json": qualificationTestPack, "jpack.json": `{"configVersion":"2","packs":{"example":{"path":"pack.json"}}}`})
	s, ts := startDesk(t, Config{RunnerBin: bin, JpackBin: runtime, ProjectDir: project, DeskConfigDir: config, Token: testToken, RunnerAllowUntestedReleases: true})
	t.Cleanup(func() { ts.Close(); s.Close() })
	s.inputEvaluationCancel()
	<-s.inputEvaluationDone
	if s.inputResearchCancel != nil {
		s.inputResearchCancel()
		<-s.inputResearchDone
	}
	if backend == "memory" {
		s.evaluationJournals = newMemoryEvaluationJournals()
		s.inputPreparations = &memoryPreparationRepository{data: map[string][]byte{}}
	}
	h := &subscriptionUpgradeHarness{t: t, s: s}
	h.ok("/api/input-preparations", map[string]any{"name": "Governance", "recipe": inputRecipe{Qualification: qualificationFixture()}}, "", &h.saved)
	h.path = "/api/input-preparations/" + h.saved.Document.ID
	h.importRecord("Baseline")
	return h
}
func (h *subscriptionUpgradeHarness) call(path string, body any, revision string) (int, []byte) {
	h.t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("If-Match", revision)
	w := httptest.NewRecorder()
	h.s.ServeHTTP(w, r)
	return w.Code, w.Body.Bytes()
}
func (h *subscriptionUpgradeHarness) ok(path string, body any, revision string, result any) {
	h.t.Helper()
	code, raw := h.call(path, body, revision)
	if code >= 300 {
		h.t.Fatalf("%s: %d %s", path, code, raw)
	}
	if result != nil {
		if err := json.Unmarshal(raw, result); err != nil {
			h.t.Fatal(err)
		}
	}
}
func (h *subscriptionUpgradeHarness) importRecord(name string) {
	h.t.Helper()
	h.ok(h.path+"/imports", map[string]any{"kind": "csv", "name": name + ".csv", "content": "Name,Country,License\n" + name + ",CA,present\n"}, h.saved.SHA256, &h.saved)
}
func (h *subscriptionUpgradeHarness) review(source string) inputEvaluation {
	h.t.Helper()
	var b inputEvaluation
	h.ok(h.path+"/evaluations", map[string]any{"recordIds": []string{h.saved.Document.Records[0].ID}, "subscriptionId": source}, h.saved.SHA256, &b)
	return b
}
func (h *subscriptionUpgradeHarness) journal(id string) evaluationJournal {
	h.t.Helper()
	root, e := h.s.inputEvaluationRoot(false)
	if e != nil {
		h.t.Fatal(e)
	}
	defer root.Close()
	br, e := openInputEvaluation(root, id, false)
	if e != nil {
		h.t.Fatal(e)
	}
	h.t.Cleanup(func() { br.Close() })
	return h.s.evaluationJournal(br, id)
}
func (h *subscriptionUpgradeHarness) read(id string) inputEvaluation {
	h.t.Helper()
	b, e := h.journal(id).Read(h.t.Context())
	if e != nil {
		h.t.Fatal(e)
	}
	return b.Document
}
func (h *subscriptionUpgradeHarness) subscription() inputEvaluation {
	h.t.Helper()
	b := h.review("")
	h.ok(h.path+"/evaluations/"+b.ID+"/confirm", map[string]any{"reviewed": true, "subscription": map[string]any{"mode": "changed", "includeExisting": false}}, h.saved.SHA256, nil)
	h.s.resumeInputEvaluations(h.t.Context())
	b = h.read(b.ID)
	if b.State != "paused" || b.Subscription.TriggerID == "" {
		h.t.Fatal(b.State, b.Problem)
	}
	return b
}
func (h *subscriptionUpgradeHarness) action(b inputEvaluation, action string) (int, []byte) {
	return h.call(h.path+"/evaluations/"+b.ID+"/subscription-upgrade", map[string]any{"reviewed": true, "action": action}, b.Revision)
}
func (h *subscriptionUpgradeHarness) accepted(b inputEvaluation, action string) inputEvaluation {
	h.t.Helper()
	code, raw := h.action(b, action)
	if code != 200 {
		h.t.Fatalf("%s: %d %s", action, code, raw)
	}
	var next inputEvaluation
	json.Unmarshal(raw, &next)
	return next
}

func TestSubscriptionUpgradeRevisions(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			h := newSubscriptionUpgradeHarness(t, backend)
			source := h.subscription()
			// Give the first revision real completed history before replacing it.
			h.ok(h.path+"/evaluations/"+source.ID+"/subscription-state", map[string]any{"reviewed": true, "enabled": true}, "", nil)
			h.s.resumeInputEvaluations(t.Context())
			h.importRecord("Historical")
			for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
				h.s.resumeInputEvaluations(t.Context())
				source = h.read(source.ID)
				if len(source.Rows) == 1 && evaluationRowTerminal(source.Rows[0].State) {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if len(source.Rows) != 1 || !evaluationRowTerminal(source.Rows[0].State) {
				t.Fatal("history not completed", source.State, source.Problem)
			}
			h.ok(h.path+"/evaluations/"+source.ID+"/subscription-state", map[string]any{"reviewed": true, "enabled": false}, "", nil)
			h.s.resumeInputEvaluations(t.Context())
			source = h.read(source.ID)
			candidate := h.review(source.ID)
			next := h.accepted(candidate, "apply")
			old := h.read(source.ID)
			if old.State != "superseded" || old.ReleaseID != source.ReleaseID || !sameEvaluationValue(old.Rows, source.Rows) {
				t.Fatal("history replaced")
			}
			if next.Subscription.Enabled || next.State != "paused" || len(next.Rows) != 0 || next.JobID != source.JobID || next.Subscription.TriggerID != source.Subscription.TriggerID || !next.Subscription.Initialized {
				t.Fatal("invalid replacement", next)
			}
			// The baseline belongs to this new review, not first-ever enable or old revision.
			h.importRecord("After review")
			if !subscriptionEligible(next, h.saved.Document, h.saved.Document.Records[len(h.saved.Document.Records)-1]) {
				t.Fatal("post-review record skipped")
			}
			for _, r := range h.saved.Document.Records[:len(h.saved.Document.Records)-1] {
				if subscriptionEligible(next, h.saved.Document, r) {
					t.Fatal("old record replayed")
				}
			}
			code, _ := h.call(h.path+"/evaluations/"+source.ID+"/subscription-state", map[string]any{"reviewed": true, "enabled": true}, "")
			if code != 409 {
				t.Fatal("superseded source enabled", code)
			}
			// A replacement can itself be replaced, without moving any historical row.
			third := h.accepted(h.review(next.ID), "apply")
			if third.JobID != source.JobID || third.Subscription.TriggerID != source.Subscription.TriggerID {
				t.Fatal("created another job/trigger")
			}
			h.ok(h.path+"/evaluations/"+third.ID+"/subscription-state", map[string]any{"reviewed": true, "enabled": true}, "", nil)
			h.s.resumeInputEvaluations(t.Context())
			third = h.read(third.ID)
			if third.State != "watching" || len(third.Rows) != 0 {
				t.Fatal("replacement enable failed", third.State, third.Problem)
			}
		})
	}
}

func TestSubscriptionUpgradeEveryCheckpointRecovery(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		for point := 1; point <= 6; point++ {
			for _, after := range []bool{false, true} {
				name := backend + "/" + string(rune('0'+point))
				if after {
					name += "/after"
				} else {
					name += "/before"
				}
				t.Run(name, func(t *testing.T) {
					h := newSubscriptionUpgradeHarness(t, backend)
					source := h.subscription()
					candidate := h.review(source.ID)
					provider := h.s.evaluationJournals
					sourceJournal, candidateJournal := h.journal(source.ID), h.journal(candidate.ID)
					writes := 0
					h.s.evaluationJournals = evaluationProviderFunc(func(id string) evaluationJournal {
						j := candidateJournal
						if id == source.ID {
							j = sourceJournal
						} else if id != candidate.ID {
							t.Fatal("unexpected journal", id)
						}
						return interceptedEvaluationJournal{evaluationJournal: j, write: func(ctx context.Context, b inputEvaluation, revision string) (evaluationSnapshot, error) {
							writes++
							if writes != point {
								return j.CompareAndSwap(ctx, b, revision)
							}
							if after {
								if _, err := j.CompareAndSwap(ctx, b, revision); err != nil {
									return evaluationSnapshot{}, err
								}
							}
							return evaluationSnapshot{}, errors.New("checkpoint acknowledgement lost")
						}}
					})
					code, _ := h.action(candidate, "apply")
					if code != 409 || writes != point {
						t.Fatal("fault not exercised", code, writes, point)
					}
					h.s.evaluationJournals = provider
					retained := h.read(candidate.ID)
					if point >= 4 {
						// A changed preparation must not prevent receipt-only reconciliation.
						h.importRecord("Later")
						retained = h.accepted(retained, "recover")
					} else if retained.Upgrade != nil && retained.Upgrade.Phase == "submitted" {
						code, _ = h.action(retained, "recover")
						if code != 409 {
							t.Fatal("unknown recovery dispatched", code)
						}
						old := h.read(source.ID)
						if !subscriptionReserved(old) {
							t.Fatal("unknown result released source")
						}
						retained = h.accepted(retained, "retry")
						if len(retained.Upgrade.RetryAt) != 1 {
							t.Fatal("retry authorization missing")
						}
					} else {
						retained = h.accepted(retained, "apply")
					}
					if retained.State != "paused" || retained.Subscription == nil || len(retained.Rows) != 0 || h.read(source.ID).State != "superseded" {
						t.Fatal("handoff incomplete")
					}
					duplicate := h.accepted(retained, "recover")
					if !sameEvaluationValue(retained, duplicate) {
						t.Fatal("recovery changed revision")
					}
					raw, err := h.s.inputRunner(t.Context(), "jobs/"+source.JobID, nil, "")
					var current struct {
						Job struct {
							Revision  int
							ReleaseID string
						}
					}
					json.Unmarshal(raw, &current)
					if err != nil || current.Job.Revision != 2 || current.Job.ReleaseID != candidate.ReleaseID {
						t.Fatal("upgrade repeated", err, string(raw))
					}
					raw, err = h.s.inputRunner(t.Context(), "runs", nil, "")
					if err != nil || !strings.Contains(string(raw), `"items":[]`) {
						t.Fatal("upgrade started work", err, string(raw))
					}
				})
			}
		}
	}
}

func TestSubscriptionUpgradeConflictingReceiptReleasesReservation(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			h := newSubscriptionUpgradeHarness(t, backend)
			source := h.subscription()
			candidate := h.review(source.ID)
			// Simulate a crash immediately after durable submit intent, before dispatch.
			cj, sj := h.journal(candidate.ID), h.journal(source.ID)
			cs, _ := cj.Read(t.Context())
			candidate.Upgrade = &inputUpgradeReview{JobID: source.JobID, BaseRevision: 1, BaseReleaseID: source.ReleaseID, Phase: "prepared"}
			cs, e := cj.CompareAndSwap(t.Context(), candidate, cs.SHA256)
			if e != nil {
				t.Fatal(e)
			}
			ss, _ := sj.Read(t.Context())
			source.Successors = []subscriptionSuccessor{{CandidateID: candidate.ID, State: "pending"}}
			if _, e = sj.CompareAndSwap(t.Context(), source, ss.SHA256); e != nil {
				t.Fatal(e)
			}
			candidate.Upgrade.Phase = "submitted"
			if _, e = cj.CompareAndSwap(t.Context(), candidate, cs.SHA256); e != nil {
				t.Fatal(e)
			}
			competing := h.review("")
			if _, e = h.s.inputRunner(t.Context(), "jobs/"+source.JobID+"/upgrade", map[string]any{"reviewed": true, "releaseId": competing.ReleaseID, "baseReleaseId": source.ReleaseID, "baseRevision": 1}, ""); e != nil {
				t.Fatal(e)
			}
			refused := h.accepted(candidate, "recover")
			if refused.Upgrade.Phase != "refused" || subscriptionReserved(h.read(source.ID)) {
				t.Fatal("immutable competing receipt left source reserved")
			}
			fresh := h.review(source.ID)
			if fresh.SubscriptionUpgrade.BaseRevision != 2 {
				t.Fatal("fresh review lost current base")
			}
			// The source history and old attempt remain retained while a new review is admitted.
			if len(h.read(source.ID).Successors) != 1 {
				t.Fatal("refusal history erased")
			}
		})
	}
}

func retainSubmittedSubscriptionUpgrade(t *testing.T, h *subscriptionUpgradeHarness, source, candidate inputEvaluation) inputEvaluation {
	t.Helper()
	cj, sj := h.journal(candidate.ID), h.journal(source.ID)
	cs, _ := cj.Read(t.Context())
	candidate.Upgrade = &inputUpgradeReview{JobID: source.JobID, BaseRevision: candidate.SubscriptionUpgrade.BaseRevision, BaseReleaseID: candidate.SubscriptionUpgrade.BaseReleaseID, Phase: "prepared"}
	cs, e := cj.CompareAndSwap(t.Context(), candidate, cs.SHA256)
	if e != nil {
		t.Fatal(e)
	}
	ss, _ := sj.Read(t.Context())
	source.Successors = append(source.Successors, subscriptionSuccessor{CandidateID: candidate.ID, State: "pending"})
	if _, e = sj.CompareAndSwap(t.Context(), source, ss.SHA256); e != nil {
		t.Fatal(e)
	}
	candidate.Upgrade.Phase = "submitted"
	if _, e = cj.CompareAndSwap(t.Context(), candidate, cs.SHA256); e != nil {
		t.Fatal(e)
	}
	return candidate
}
func TestSubscriptionUpgradeRetryFailureKeepsReservation(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			h := newSubscriptionUpgradeHarness(t, backend)
			source := h.subscription()
			candidate := retainSubmittedSubscriptionUpgrade(t, h, source, h.review(source.ID))
			original := h.s.jobs
			endpoint, _, e := original.endpoint()
			if e != nil {
				t.Fatal(e)
			}
			// An older request commits while this explicit retry sees an auth failure.
			posts := 0
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				req, e := http.NewRequestWithContext(r.Context(), r.Method, endpoint+r.URL.RequestURI(), r.Body)
				if e != nil {
					t.Error(e)
					return
				}
				req.Header = r.Header.Clone()
				response, e := http.DefaultClient.Do(req)
				if e != nil {
					t.Error(e)
					return
				}
				defer response.Body.Close()
				raw, _ := io.ReadAll(response.Body)
				if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/upgrade") {
					posts++
					if response.StatusCode != 200 {
						t.Error("real Runner did not accept", response.StatusCode, string(raw))
					}
					writeJSON(w, 403, map[string]any{"error": map[string]string{"code": "forbidden", "message": "Retry authorization expired"}})
					return
				}
				w.WriteHeader(response.StatusCode)
				w.Write(raw)
			}))
			defer proxy.Close()
			h.s.jobs = &jobsCompanion{url: proxy.URL, token: original.token, done: make(chan struct{})}
			defer func() { h.s.jobs = original }()
			code, _ := h.action(candidate, "retry")
			if code != 409 || posts != 1 {
				t.Fatal(code, posts)
			}
			pending := h.read(candidate.ID)
			if pending.Upgrade.Phase != "submitted" || !subscriptionReserved(h.read(source.ID)) {
				t.Fatal("retry failure released uncertain operation")
			}
			h.s.jobs = original
			// Another upgrade is allowed in Runner; recovery still belongs to its historical receipt.
			later := h.review("")
			if _, e = h.s.inputRunner(t.Context(), "jobs/"+source.JobID+"/upgrade", map[string]any{"reviewed": true, "releaseId": later.ReleaseID, "baseReleaseId": candidate.ReleaseID, "baseRevision": 2}, ""); e != nil {
				t.Fatal(e)
			}
			next := h.accepted(pending, "recover")
			if next.ReleaseID != candidate.ReleaseID || next.State != "paused" {
				t.Fatal("historical receipt replaced")
			}
			h.ok(h.path+"/evaluations/"+next.ID+"/subscription-state", map[string]any{"reviewed": true, "enabled": true}, "", nil)
			h.s.resumeInputEvaluations(t.Context())
			next = h.read(next.ID)
			if next.State != "blocked" || len(next.Rows) != 0 {
				t.Fatal("outdated revision enabled", next.State, next.Problem)
			}
		})
	}
}
func TestSubscriptionUpgradeCompetingCandidates(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			h := newSubscriptionUpgradeHarness(t, backend)
			source := h.subscription()
			a, b := h.review(source.ID), h.review(source.ID)
			results := make(chan int, 2)
			for _, candidate := range []inputEvaluation{a, b} {
				go func(c inputEvaluation) { code, _ := h.action(c, "apply"); results <- code }(candidate)
			}
			x, y := <-results, <-results
			if !(x == 200 && y == 409 || x == 409 && y == 200) {
				t.Fatal("competing handoffs", x, y)
			}
			retained := h.read(source.ID)
			if len(retained.Successors) != 1 || retained.Successors[0].State != "complete" {
				t.Fatal("multiple successors")
			}
		})
	}
}
