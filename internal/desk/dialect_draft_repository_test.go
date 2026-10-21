package desk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
)

// A second adapter proves that the HTTP and recovery flows use logical storage.
// It is deliberately test-only, not a selectable cloud destination.
type memoryDialectDraftRepository struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (m *memoryDialectDraftRepository) documents() ([]dialectDraftDocument, error) {
	docs := []dialectDraftDocument{}
	for id, raw := range m.data {
		saved, err := decodeDialectDraft(id, raw)
		if err != nil {
			return nil, err
		}
		docs = append(docs, saved.Document)
	}
	return docs, nil
}
func (m *memoryDialectDraftRepository) List(ctx context.Context) ([]dialectDraftSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	docs, err := m.documents()
	if err != nil {
		return nil, err
	}
	return draftSummaries(docs), nil
}
func (m *memoryDialectDraftRepository) Get(ctx context.Context, id string) (dialectDraftReply, error) {
	if err := ctx.Err(); err != nil {
		return dialectDraftReply{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return dialectDraftReply{}, err
	}
	if !dialectDraftID.MatchString(id) {
		return dialectDraftReply{}, errors.New("invalid identity")
	}
	raw, ok := m.data[id]
	if !ok {
		return dialectDraftReply{}, os.ErrNotExist
	}
	return decodeDialectDraft(id, raw)
}
func (m *memoryDialectDraftRepository) CompareAndSwap(ctx context.Context, d dialectDraftDocument, revision string) (dialectDraftReply, error) {
	if err := ctx.Err(); err != nil {
		return dialectDraftReply{}, err
	}
	raw, next, err := encodeDialectDraft(d)
	if err != nil {
		return dialectDraftReply{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return dialectDraftReply{}, err
	}
	before, ok := m.data[d.ID]
	if ok && (revision == "" || digestOf(before) != revision) || !ok && revision != "" {
		return dialectDraftReply{}, errDialectDraftConflict
	}
	if ok {
		saved, err := decodeDialectDraft(d.ID, before)
		if err != nil {
			return dialectDraftReply{}, err
		}
		if err = validateDialectDraftTransition(saved.Document, next.Document); err != nil {
			return dialectDraftReply{}, err
		}
	} else {
		docs, err := m.documents()
		if err != nil {
			return dialectDraftReply{}, err
		}
		if err = draftCapacity(docs); err != nil {
			return dialectDraftReply{}, err
		}
	}
	m.data[d.ID] = raw
	return next, nil
}
func draftRepositoryFixture(t *testing.T, backend string) (*Server, dialectDraftRepository) {
	t.Helper()
	s, _ := newTestServer(t, false)
	if backend == "memory" {
		s.dialectDrafts = &memoryDialectDraftRepository{data: map[string][]byte{}}
	}
	return s, s.draftRepository()
}
func repositoryDraft(n int) dialectDraftDocument {
	return dialectDraftDocument{Version: 1, ID: dialectDraftIdentity(fmt.Sprint(n), nil), Name: "Draft", Recipe: inputRecipe{Intent: "Retain typed facts", Research: &inputResearchRecipe{Version: 1, Target: "records", Fields: []inputResearchField{{Name: "count", Type: "number"}}}}, CreatedAt: "2026-10-15T00:00:00Z", UpdatedAt: "2026-10-15T00:00:00Z"}
}
func TestDialectDraftStorageContract(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			_, repo := draftRepositoryFixture(t, backend)
			ctx := t.Context()
			d := repositoryDraft(1)
			items, err := repo.List(ctx)
			if err != nil || items == nil || len(items) != 0 {
				t.Fatal(items, err)
			}
			if _, err = repo.Get(ctx, d.ID); !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			saved, err := repo.CompareAndSwap(ctx, d, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = repo.CompareAndSwap(ctx, d, ""); !errors.Is(err, errDialectDraftConflict) {
				t.Fatal("create overwrote", err)
			}
			d.Name = "Updated"
			next, err := repo.CompareAndSwap(ctx, d, saved.SHA256)
			if err != nil || next.SHA256 == saved.SHA256 {
				t.Fatal(next, err)
			}
			if _, err = repo.CompareAndSwap(ctx, d, saved.SHA256); !errors.Is(err, errDialectDraftConflict) {
				t.Fatal("stale write", err)
			}
			next.Document.Recipe.Research.Fields[0].Type = "boolean"
			got, err := repo.Get(ctx, d.ID)
			if err != nil || got.Document.Recipe.Research.Fields[0].Type != "number" {
				t.Fatal("snapshot alias", got, err)
			}
			d.CreatedAt = "changed"
			if _, err = repo.CompareAndSwap(ctx, d, got.SHA256); err == nil {
				t.Fatal("changed creation identity")
			}
			d.CreatedAt = got.Document.CreatedAt
			d.Discarded = true
			d.Name = "Another edit"
			if _, err = repo.CompareAndSwap(ctx, d, got.SHA256); err == nil {
				t.Fatal("completion changed reviewed content")
			}
			d.Name = got.Document.Name
			terminal, err := repo.CompareAndSwap(ctx, d, got.SHA256)
			if err != nil {
				t.Fatal(err)
			}
			d.Discarded = false
			if _, err = repo.CompareAndSwap(ctx, d, terminal.SHA256); err == nil {
				t.Fatal("resurrected")
			}
			items, err = repo.List(ctx)
			if err != nil || len(items) != 0 {
				t.Fatal("terminal draft listed", items, err)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err = repo.Get(canceled, d.ID); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if _, err = repo.List(canceled); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if _, err = repo.CompareAndSwap(canceled, repositoryDraft(2), ""); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}
func TestDialectDraftStorageConcurrentEdits(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			_, repo := draftRepositoryFixture(t, backend)
			d := repositoryDraft(1)
			saved, err := repo.CompareAndSwap(t.Context(), d, "")
			if err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, name := range []string{"Left", "Right"} {
				go func(name string) {
					<-start
					candidate := d
					candidate.Name = name
					_, err := repo.CompareAndSwap(t.Context(), candidate, saved.SHA256)
					results <- err
				}(name)
			}
			close(start)
			wins, conflicts := 0, 0
			for range 2 {
				err := <-results
				if err == nil {
					wins++
				} else if errors.Is(err, errDialectDraftConflict) {
					conflicts++
				} else {
					t.Fatal(err)
				}
			}
			if wins != 1 || conflicts != 1 {
				t.Fatal(wins, conflicts)
			}
		})
	}
}
func TestDialectDraftStorageCapacity(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			_, repo := draftRepositoryFixture(t, backend)
			for n := range dialectDraftActiveLimit {
				if _, err := repo.CompareAndSwap(t.Context(), repositoryDraft(n), ""); err != nil {
					t.Fatal(n, err)
				}
			}
			if _, err := repo.CompareAndSwap(t.Context(), repositoryDraft(999), ""); !errors.Is(err, errDialectDraftCapacity) {
				t.Fatal("capacity", err)
			}
			first, err := repo.Get(t.Context(), repositoryDraft(0).ID)
			if err != nil {
				t.Fatal(err)
			}
			first.Document.Discarded = true
			if _, err = repo.CompareAndSwap(t.Context(), first.Document, first.SHA256); err != nil {
				t.Fatal("capacity prevented completion", err)
			}
			if _, err = repo.CompareAndSwap(t.Context(), repositoryDraft(999), ""); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestDialectDraftFileReadDoesNotCreateOrHideCorruption(t *testing.T) {
	s, repo := draftRepositoryFixture(t, "file")
	if _, err := repo.List(t.Context()); err != nil {
		t.Fatal(err)
	}
	if root, err := s.openDialectDrafts(false); !errors.Is(err, os.ErrNotExist) {
		if root != nil {
			root.Close()
		}
		t.Fatal("listing created storage", err)
	}
	d := repositoryDraft(1)
	if _, err := repo.CompareAndSwap(t.Context(), d, ""); err != nil {
		t.Fatal(err)
	}
	root, err := s.openDialectDrafts(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = writePrivateData(root, d.ID+".json", []byte(`{"version":1}`)); err != nil {
		t.Fatal(err)
	}
	if items, err := repo.List(t.Context()); err == nil || items != nil {
		t.Fatal("false empty listing", items, err)
	}
	if _, err := repo.Get(t.Context(), d.ID); err == nil {
		t.Fatal("corrupt draft accepted")
	}
}

type uncertainDialectDraftWrite struct {
	dialectDraftRepository
	writes int
}

func (u *uncertainDialectDraftWrite) CompareAndSwap(ctx context.Context, d dialectDraftDocument, revision string) (dialectDraftReply, error) {
	u.writes++
	if _, err := u.dialectDraftRepository.CompareAndSwap(ctx, d, revision); err != nil {
		return dialectDraftReply{}, err
	}
	return dialectDraftReply{}, errors.New("acknowledgement unavailable")
}
func TestDialectDraftPublicationReceiptUncertainty(t *testing.T) {
	s, repo := draftRepositoryFixture(t, "memory")
	s.inputPreparations = &memoryPreparationRepository{data: map[string][]byte{}}
	saved := draftResult(t, draftCall(s, "POST", "/api/dialect-drafts", dialectDraftRequest{Key: "receipt", Name: "Records", Recipe: inputRecipe{Intent: "Import records"}}, ""))
	path := "/api/dialect-drafts/" + saved.Document.ID
	uncertain := &uncertainDialectDraftWrite{dialectDraftRepository: repo}
	s.dialectDrafts = uncertain
	if w := draftCall(s, "POST", path+"/publish", map[string]any{}, saved.SHA256); w.Code == 200 {
		t.Fatal("uncertain acknowledgement reported success")
	}
	if uncertain.writes != 1 {
		t.Fatal("automatic replay", uncertain.writes)
	}
	recovered := draftResult(t, draftCall(s, "POST", path+"/publish", map[string]any{}, saved.SHA256))
	if recovered.Document.SavedInputID == "" || uncertain.writes != 1 {
		t.Fatal("completed publication repeated", recovered, uncertain.writes)
	}
	items, err := s.preparationRepository().List(t.Context())
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
}
func TestDialectDraftReadDoesNotTakeWorkflowLease(t *testing.T) {
	s, repo := draftRepositoryFixture(t, "file")
	d := repositoryDraft(1)
	if _, err := repo.CompareAndSwap(t.Context(), d, ""); err != nil {
		t.Fatal(err)
	}
	unlock, err := s.lockDialectDraftWorkflow(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if w := draftCall(s, "GET", "/api/dialect-drafts/"+d.ID, nil, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

// Injected adapters expose the same current-authority fence as the owner's file
// binding. A post-operation refusal returns no successful acknowledgement; a
// completed write is retained for inspection rather than silently retried.
type countedDialectDraftRepository struct {
	dialectDraftRepository
	calls int
}

func (r *countedDialectDraftRepository) List(ctx context.Context) ([]dialectDraftSummary, error) {
	r.calls++
	return r.dialectDraftRepository.List(ctx)
}
func (r *countedDialectDraftRepository) Get(ctx context.Context, id string) (dialectDraftReply, error) {
	r.calls++
	return r.dialectDraftRepository.Get(ctx, id)
}
func (r *countedDialectDraftRepository) CompareAndSwap(ctx context.Context, d dialectDraftDocument, revision string) (dialectDraftReply, error) {
	r.calls++
	return r.dialectDraftRepository.CompareAndSwap(ctx, d, revision)
}
func TestDialectDraftRepositoryCurrentAuthority(t *testing.T) {
	for _, operation := range []string{"list", "get", "create"} {
		for _, withdrawAt := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/%d", operation, withdrawAt), func(t *testing.T) {
				s, underlying := draftRepositoryFixture(t, "memory")
				existing := repositoryDraft(1)
				_, err := underlying.CompareAndSwap(t.Context(), existing, "")
				assistantRunMust(t, err)
				counted := &countedDialectDraftRepository{dialectDraftRepository: underlying}
				s.dialectDrafts = counted
				if s.draftRepository() != counted {
					t.Fatal("ordinary repository binding changed")
				}
				checks := 0
				repository := s.draftRepositoryWithAuthority(func(context.Context) error {
					checks++
					if checks >= withdrawAt {
						return errAssistantRunScope
					}
					return nil
				})
				switch operation {
				case "list":
					var items []dialectDraftSummary
					items, err = repository.List(t.Context())
					if items != nil {
						t.Fatal("withdrawn read exposed a list")
					}
				case "get":
					var saved dialectDraftReply
					saved, err = repository.Get(t.Context(), existing.ID)
					if saved.SHA256 != "" || saved.Document.ID != "" {
						t.Fatal("withdrawn read exposed a draft")
					}
				case "create":
					var saved dialectDraftReply
					saved, err = repository.CompareAndSwap(t.Context(), repositoryDraft(2), "")
					if saved.SHA256 != "" || saved.Document.ID != "" {
						t.Fatal("withdrawn write exposed success")
					}
				}
				if !errors.Is(err, errAssistantRunScope) || checks != withdrawAt || counted.calls != withdrawAt-1 {
					t.Fatal("owning repository omitted current authority", err, checks, counted.calls)
				}
				if operation == "create" {
					saved, err := underlying.Get(t.Context(), repositoryDraft(2).ID)
					if withdrawAt == 1 && !errors.Is(err, os.ErrNotExist) {
						t.Fatal("pre-operation refusal wrote")
					}
					if withdrawAt == 2 && (err != nil || saved.Document.ID != repositoryDraft(2).ID) {
						t.Fatal("post-operation uncertainty lost completed effect", err)
					}
				}
			})
		}
	}
}
