package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResearchJournalMemoryApprovalAndStop(t *testing.T) {
	for _, mode := range []string{"manual", "auto"} {
		t.Run(mode, func(t *testing.T) {
			s, _, run, _ := approvalFixture(t)
			useMemoryPreparationFixture(t, s)
			repo := useMemoryResearchFixture(t, s)
			if mode == "auto" {
				authorizeFixture(t, s, run)
				s.scanInputResearch(t.Context()) // Grant alone cannot dispatch.
				before, err := repo.Run(run.ID).Read(t.Context())
				if err != nil || before.State != "review" || len(before.Attempts) != 0 {
					t.Fatal("grant dispatched work", err)
				}
				status := approvalHTTP(s, "chat-1", "", "")
				if status.Code != 200 || !strings.Contains(status.Body.String(), `"mode":"auto"`) {
					t.Fatal(status.Code, status.Body.String())
				}
				first := requestApprovalRun(s, run, "chat-1", "turn-1")
				if first.Code != 200 || !strings.Contains(first.Body.String(), `"status":"started"`) {
					t.Fatal(first.Code, first.Body.String())
				}
				again := requestApprovalRun(s, run, "chat-1", "turn-1")
				if again.Code != 200 || first.Body.String() != again.Body.String() {
					t.Fatal("duplicated admission")
				}
			} else {
				if w := requestApprovalRun(s, run, "chat-1", "turn-1"); w.Code != 200 || !strings.Contains(w.Body.String(), "review-required") {
					t.Fatal(w.Body.String())
				}
				if w := budgetHTTP(s, run, "/confirm", run.Revision, `{}`); w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			if w := budgetHTTP(s, run, "/stop", "", `{}`); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			saved, err := repo.Run(run.ID).Read(t.Context())
			if err != nil || saved.State != "stopped" || !saved.StopRequested || len(saved.Attempts) != 0 {
				t.Fatal("stop missing", err)
			}
			for _, suffix := range []string{"", "/" + run.ID, "/" + run.ID + "/activity"} {
				req := httptest.NewRequest("GET", "/api/input-preparations/"+run.PreparationID+"/research"+suffix, nil)
				req.Header.Set("Authorization", "Bearer "+testToken)
				w := httptest.NewRecorder()
				s.ServeHTTP(w, req)
				if w.Code != 200 || !strings.Contains(w.Body.String(), run.ID) {
					t.Fatal("history missing", suffix, w.Code, w.Body.String())
				}
			}
			assertNoResearchJournalFiles(t, s)
			assertNoCollectionFiles(t, s)
		})
	}
}
func TestResearchJournalMemoryBudgetDecision(t *testing.T) {
	s, _, run, revision := budgetHTTPFixture(t)
	useMemoryPreparationFixture(t, s)
	repo := useMemoryResearchFixture(t, s)
	w := budgetHTTP(s, run, "/budget-review", revision, `{}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var review inputResearchBudgetReview
	if err := json.Unmarshal(w.Body.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	again := budgetHTTP(s, run, "/budget-review", revision, `{}`)
	var same inputResearchBudgetReview
	if err := json.Unmarshal(again.Body.Bytes(), &same); err != nil || same.Request.ID != review.Request.ID {
		t.Fatal("duplicate review", err)
	}
	if w = budgetHTTP(s, run, "/budget-decision", "", fmt.Sprintf(`{"requestId":%q,"decision":"decline"}`, review.Request.ID)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved, err := repo.Run(run.ID).Read(t.Context())
	if err != nil || saved.State != "completed-with-gaps" || len(saved.BudgetRequests) != 1 || saved.BudgetRequests[0].Reviewer.Kind != "local-access" {
		t.Fatal("decision not retained", err)
	}
	assertNoResearchJournalFiles(t, s)
}
func TestResearchJournalMemoryWorkerRecoversAndPublishesOnce(t *testing.T) {
	s, rr, run := researchV2Fixture(t)
	run = v2Apply(t, rr, v2Record(run, "Example"))
	useMemoryPreparationFixture(t, s)
	repo := useMemoryResearchFixture(t, s)
	if _, err := repo.Run(run.ID).Update(t.Context(), func(r *inputResearchRun) error { r.State = "running"; return nil }); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s.scanInputResearch(t.Context())
	}
	saved, err := repo.Run(run.ID).Read(t.Context())
	if err != nil || saved.State != "interrupted" || len(saved.Attempts) != len(run.Attempts) || len(saved.Records) != 1 || saved.PublicationPending {
		t.Fatal("recovery lost retained work", saved.State, err)
	}
	records, err := s.preparationRepository().Get(t.Context(), run.PreparationID)
	if err != nil || len(records.Document.Records) != 1 {
		t.Fatal("duplicate/missing publication", err)
	}
	assertNoResearchJournalFiles(t, s)
	assertNoCollectionFiles(t, s)
}
func TestResearchJournalStopRemainsAvailableDuringProviderWait(t *testing.T) {
	s, _, run, _ := approvalFixture(t)
	repo := useMemoryResearchFixture(t, s)
	if _, err := repo.Run(run.ID).Update(t.Context(), func(r *inputResearchRun) error { r.State = "running"; return nil }); err != nil {
		t.Fatal(err)
	}
	store := researchRunStore{journal: repo.Run(run.ID)}
	started, done := make(chan struct{}), make(chan struct{})
	ops := recoveryOperations()
	ops.model = func(ctx context.Context, _ inputResearchAI, _ string, _ func(bool) error) (json.RawMessage, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	go func() { defer close(done); executeInputResearch(t.Context(), store, run, ops) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not start")
	}
	if w := budgetHTTP(s, run, "/stop", "", `{}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop waited on provider lock")
	}
	saved, err := repo.Run(run.ID).Read(t.Context())
	if err != nil || saved.State != "stopped" || len(saved.Attempts) != 1 || saved.Attempts[0].State != "uncertain" {
		t.Fatal("cancelled completion lost", saved.State, err)
	}
	assertNoResearchJournalFiles(t, s)
}

type uncertainCreateResearchJournals struct {
	researchJournalProvider
	calls *int
}
type uncertainCreateResearchJournal struct {
	researchJournal
	calls *int
}

func (p uncertainCreateResearchJournals) Run(id string) researchJournal {
	return uncertainCreateResearchJournal{p.researchJournalProvider.Run(id), p.calls}
}
func (j uncertainCreateResearchJournal) Create(ctx context.Context, r inputResearchRun) error {
	*j.calls++
	if err := j.researchJournal.Create(ctx, r); err != nil {
		return err
	}
	return errors.New("commit acknowledgement unavailable")
}
func TestResearchJournalReviewCreationUncertaintyKeepsEvidenceDirectory(t *testing.T) {
	s, _, run, _ := approvalFixture(t)
	repo := useMemoryResearchFixture(t, s)
	root, err := s.inputResearchRoot(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	run.ID = "research_" + strings.Repeat("c", 32)
	calls := 0
	s.researchJournals = uncertainCreateResearchJournals{repo, &calls}
	if err := s.writeResearchReview(t.Context(), root, run, nil); err == nil || calls != 1 {
		t.Fatal("retried/acknowledged uncertain create", calls, err)
	}
	saved, err := repo.Run(run.ID).Read(t.Context())
	if err != nil || saved.State != "review" {
		t.Fatal("lost review", err)
	}
	if _, err := openInputResearch(root, run.ID, false); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("published uncertain review", err)
	}
	dir, _, err := openOwnFolder(root, []string{".review-" + run.ID}, false, "research review")
	if err != nil {
		t.Fatal("deleted uncertain evidence directory", err)
	}
	defer dir.Close()
	if err := writePrivateData(dir, "retained-marker", []byte("retain")); err != nil {
		t.Fatal(err)
	}
	if err := s.writeResearchReview(t.Context(), root, run, nil); !errors.Is(err, os.ErrExist) || calls != 1 {
		t.Fatal("replaced uncertain run", err)
	}
	if data, err := readPrivateData(dir, "retained-marker", 100); err != nil || string(data) != "retain" {
		t.Fatal("lost retained directory", err)
	}
	if err := s.researchInputAvailable(t.Context(), root, run.PreparationID, ""); err != nil {
		t.Fatal("staged failure blocked admission", err)
	}
	req := httptest.NewRequest("GET", "/api/input-preparations/"+run.PreparationID+"/research", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 || strings.Contains(w.Body.String(), run.ID) {
		t.Fatal("unpublished journal leaked into history", w.Code, w.Body.String())
	}
	s.scanInputResearch(t.Context())
	after, err := repo.Run(run.ID).Read(t.Context())
	if err != nil || after.State != "review" || len(after.Attempts) != 0 {
		t.Fatal("staged run was dispatched", err)
	}
	assertNoResearchJournalFiles(t, s)
}
