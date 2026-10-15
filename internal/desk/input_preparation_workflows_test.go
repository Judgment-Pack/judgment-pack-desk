package desk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertNoCollectionFiles(t *testing.T, s *Server) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(s.projectDir, inputPreparationDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("workflow fell back to collection files", err)
	}
}

// These fixtures use disposable projects. Move their saved snapshots to the
// test adapter and remove record files so a hidden bypass cannot pass the test.
func useMemoryPreparationFixture(t *testing.T, s *Server) *memoryPreparationRepository {
	t.Helper()
	repo := s.preparationRepository()
	items, err := repo.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	memory := &memoryPreparationRepository{data: map[string][]byte{}}
	for _, item := range items {
		saved, err := repo.Get(t.Context(), item.ID)
		if err != nil {
			t.Fatal(err)
		}
		copy, err := memory.CompareAndSwap(t.Context(), saved.Document, "")
		if err != nil || copy.SHA256 != saved.SHA256 {
			t.Fatal("fixture changed revision", err)
		}
	}
	s.inputPreparations = memory
	if err = os.RemoveAll(filepath.Join(s.projectDir, inputPreparationDir)); err != nil {
		t.Fatal(err)
	}
	return memory
}

func TestResearchRepositoryPublicationRecovery(t *testing.T) {
	for _, failure := range []string{"conflict", "uncertain"} {
		t.Run(failure, func(t *testing.T) {
			s, rr, run := researchV2Fixture(t)
			repo := useMemoryPreparationFixture(t, s)
			run = v2Apply(t, rr, v2Record(run, "Example"))
			writes := 0
			s.inputPreparations = interceptPreparationWrite{repo, func(ctx context.Context, doc inputPreparation, revision string) (inputPreparationReply, error) {
				writes++
				if failure == "conflict" {
					current, err := repo.Get(ctx, doc.ID)
					if err != nil {
						return current, err
					}
					current.Document.Name = "Concurrent edit"
					if _, err = repo.CompareAndSwap(ctx, current.Document, current.SHA256); err != nil {
						return inputPreparationReply{}, err
					}
					return repo.CompareAndSwap(ctx, doc, revision)
				}
				if _, err := repo.CompareAndSwap(ctx, doc, revision); err != nil {
					return inputPreparationReply{}, err
				}
				return inputPreparationReply{}, errors.New("commit verification unavailable")
			}}
			if err := s.publishResearchRecords(run); err == nil || writes != 1 {
				t.Fatal("publication retried or acknowledged uncertainty", writes, err)
			}
			pending, err := readInputResearch(rr)
			if err != nil || !pending.PublicationPending || pending.Outcomes[0].State != "pending" {
				t.Fatal("uncommitted publication acknowledged", pending.Outcomes, err)
			}
			before, err := repo.Get(t.Context(), run.PreparationID)
			if err != nil {
				t.Fatal(err)
			}
			if failure == "conflict" && (before.Document.Name != "Concurrent edit" || len(before.Document.Records) != 0) {
				t.Fatal("concurrent edit was overwritten")
			}
			if failure == "uncertain" && len(before.Document.Records) != 1 {
				t.Fatal("saved work was lost")
			}
			// Explicit recovery uses retained results only; it must not redo acquisition.
			s.inputPreparations = repo
			if err = s.publishResearchRecords(pending); err != nil {
				t.Fatal(err)
			}
			after, err := repo.Get(t.Context(), run.PreparationID)
			if err != nil || len(after.Document.Records) != 1 {
				t.Fatal("duplicate or missing publication", err)
			}
			if failure == "conflict" && after.Document.Name != "Concurrent edit" {
				t.Fatal("recovery erased a newer edit")
			}
			if failure == "uncertain" && after.SHA256 != before.SHA256 {
				t.Fatal("uncertain write was repeated")
			}
			acknowledged, err := readInputResearch(rr)
			if err != nil || acknowledged.PublicationPending || countResearchOutcomes(acknowledged).Added != 1 {
				t.Fatal("recovery not acknowledged", err)
			}
			assertNoCollectionFiles(t, s)
		})
	}
}

func TestResearchRepositoryAdmission(t *testing.T) {
	for _, mode := range []string{"manual", "auto", "stale"} {
		t.Run(mode, func(t *testing.T) {
			s, rr, run, _ := approvalFixture(t)
			repo := useMemoryPreparationFixture(t, s)
			if mode == "auto" {
				authorizeFixture(t, s, run)
				status := approvalHTTP(s, "chat-1", "", "")
				if status.Code != 200 || !strings.Contains(status.Body.String(), `"mode":"auto"`) {
					t.Fatal(status.Code, status.Body.String())
				}
				result := requestApprovalRun(s, run, "chat-1", "turn-1")
				if result.Code != 200 || !strings.Contains(result.Body.String(), `"status":"started"`) {
					t.Fatal(result.Code, result.Body.String())
				}
			} else {
				expected := 200
				if mode == "stale" {
					current, err := repo.Get(t.Context(), run.PreparationID)
					if err != nil {
						t.Fatal(err)
					}
					current.Document.Name = "Changed after review"
					if _, err = repo.CompareAndSwap(t.Context(), current.Document, current.SHA256); err != nil {
						t.Fatal(err)
					}
					expected = 409
				}
				result := budgetHTTP(s, run, "/confirm", "", `{}`)
				if result.Code != expected {
					t.Fatal(result.Code, result.Body.String())
				}
			}
			saved, err := readInputResearch(rr)
			if err != nil {
				t.Fatal(err)
			}
			expected := "queued"
			if mode == "stale" {
				expected = "review"
			}
			if saved.State != expected || len(saved.Attempts) != 0 {
				t.Fatal("admission bypass or acquisition during review", saved.State)
			}
			assertNoCollectionFiles(t, s)
		})
	}
}

type unavailablePreparationRead struct{ inputPreparationRepository }

func (r unavailablePreparationRead) Get(context.Context, string) (inputPreparationReply, error) {
	return inputPreparationReply{}, errors.New("storage unavailable")
}

func TestDialectDraftRepositoryRecovery(t *testing.T) {
	s, _ := newTestServer(t, false)
	repo := &memoryPreparationRepository{data: map[string][]byte{}}
	s.inputPreparations = repo
	saved := draftResult(t, draftCall(s, "POST", "/api/dialect-drafts", dialectDraftRequest{Key: "memory", Name: "Records", Recipe: inputRecipe{Intent: "Import rows"}}, ""))
	path := "/api/dialect-drafts/" + saved.Document.ID
	s.inputPreparations = unavailablePreparationRead{repo}
	edit := draftCall(s, "PUT", path, map[string]any{"name": "Unchecked edit", "recipe": inputRecipe{}}, saved.SHA256)
	if edit.Code != 409 {
		t.Fatal("edit ignored a failed publication check", edit.Code, edit.Body.String())
	}
	current := draftResult(t, draftCall(s, "GET", path, nil, ""))
	if current.SHA256 != saved.SHA256 {
		t.Fatal("draft changed on storage failure")
	}
	s.inputPreparations = repo
	id, err := s.publishDialectDraft(t.Context(), saved.Document)
	if err != nil {
		t.Fatal(err)
	}
	recovered := draftResult(t, draftCall(s, "POST", path+"/publish", map[string]any{}, saved.SHA256))
	if recovered.Document.SavedInputID != id {
		t.Fatal("lost publication receipt duplicated collection")
	}
	first, err := repo.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	first.Document.Name = "Edited saved collection"
	latest, err := repo.CompareAndSwap(t.Context(), first.Document, first.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.publishDialectDraft(t.Context(), saved.Document); err != nil {
		t.Fatal(err)
	}
	again, err := repo.Get(t.Context(), id)
	if err != nil || again.SHA256 != latest.SHA256 {
		t.Fatal("replay overwrote newer collection")
	}
	assertNoCollectionFiles(t, s)
}

func TestResearchRepositoryBudgetReview(t *testing.T) {
	s, rr, run, revision := budgetHTTPFixture(t)
	useMemoryPreparationFixture(t, s)
	w := budgetHTTP(s, run, "/budget-review", revision, `{}`)
	var review inputResearchBudgetReview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &review) != nil || review.Request.ID == "" || review.Revision != revision {
		t.Fatal(w.Code, w.Body.String())
	}
	repeated := budgetHTTP(s, run, "/budget-review", revision, `{}`)
	if repeated.Code != 200 || repeated.Body.String() != w.Body.String() {
		t.Fatal("review duplicated a request", repeated.Code, repeated.Body.String())
	}
	paused, err := readInputResearch(rr)
	if err != nil || paused.State != "paused" || len(paused.Attempts) != len(run.Attempts) {
		t.Fatal("budget review started acquisition", err)
	}
	assertNoCollectionFiles(t, s)
}
