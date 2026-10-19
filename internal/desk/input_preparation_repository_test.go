package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// A second, test-only implementation exercises the repository contract and HTTP
// consumers without a filesystem. It is not a selectable persistence backend.
type memoryPreparationRepository struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (m *memoryPreparationRepository) List(ctx context.Context) ([]inputPreparationSummary, error) {
	if err := m.acquire(ctx, true); err != nil {
		return nil, err
	}
	defer m.mu.Unlock()
	items := []inputPreparationSummary{}
	for id, raw := range m.data {
		saved, err := decodeInputPreparation(id, raw)
		if err != nil {
			return nil, err
		}
		d := saved.Document
		items = append(items, inputPreparationSummary{d.ID, d.Name, len(d.Records), d.UpdatedAt})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt == items[j].UpdatedAt {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt > items[j].UpdatedAt
	})
	return items, nil
}
func (m *memoryPreparationRepository) Get(ctx context.Context, id string) (inputPreparationReply, error) {
	if err := m.acquire(ctx, true); err != nil {
		return inputPreparationReply{}, err
	}
	defer m.mu.Unlock()
	raw, ok := m.data[id]
	if !ok {
		return inputPreparationReply{}, os.ErrNotExist
	}
	return decodeInputPreparation(id, raw)
}
func (m *memoryPreparationRepository) CompareAndSwap(ctx context.Context, doc inputPreparation, revision string) (inputPreparationReply, error) {
	if err := m.acquire(ctx, true); err != nil {
		return inputPreparationReply{}, err
	}
	defer m.mu.Unlock()
	prior, exists := m.data[doc.ID]
	if exists && (revision == "" || digestOf(prior) != revision) || !exists && revision != "" {
		return inputPreparationReply{}, errInputPreparationConflict
	}
	if !exists && len(m.data) >= inputPreparationCountLimit {
		return inputPreparationReply{}, errInputPreparationCapacity
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return inputPreparationReply{}, err
	}
	saved, err := decodeInputPreparation(doc.ID, raw)
	if err == nil {
		m.data[doc.ID] = raw
	}
	return saved, err
}

func repositoryDocument(id int) inputPreparation {
	return inputPreparation{Version: 1, ID: fmt.Sprintf("prep_%032x", id), Name: "Collection", Recipe: inputRecipe{Intent: "Retain original values"}, RecipeVersion: 1, CreatedAt: "2026-10-15T00:00:00Z", UpdatedAt: "2026-10-15T00:00:00Z", Records: []preparedInputRecord{{ID: "row-1", Name: "Example", Values: map[string]string{"count": "00123", "flag": "false", "empty": ""}, Class: "asserted", Status: "provided"}}, Imports: []inputImport{}, Activity: []inputActivity{}}
}

func TestInputPreparationRepositoryContract(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			var repo inputPreparationRepository = &memoryPreparationRepository{data: map[string][]byte{}}
			if backend == "file" {
				s, _ := newTestServer(t, false)
				repo = s.preparationRepository()
			}
			ctx := context.Background()
			items, err := repo.List(ctx)
			if err != nil || items == nil || len(items) != 0 {
				t.Fatal("empty listing", items, err)
			}
			doc := repositoryDocument(1)
			if _, err = repo.Get(ctx, doc.ID); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("missing collection", err)
			}
			created, err := repo.CompareAndSwap(ctx, doc, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = repo.CompareAndSwap(ctx, doc, ""); !errors.Is(err, errInputPreparationConflict) {
				t.Fatal("create replaced an existing collection", err)
			}
			doc.Name = "Updated"
			updated, err := repo.CompareAndSwap(ctx, doc, created.SHA256)
			if err != nil || updated.SHA256 == created.SHA256 {
				t.Fatal("update", updated, err)
			}
			doc.Name = "Stale"
			if _, err = repo.CompareAndSwap(ctx, doc, created.SHA256); !errors.Is(err, errInputPreparationConflict) {
				t.Fatal("stale update accepted", err)
			}
			updated.Document.Records[0].Values["count"] = "MUTATED"
			current, err := repo.Get(ctx, doc.ID)
			if err != nil || current.Document.Name != "Updated" || current.Document.Records[0].Values["count"] != "00123" || current.Document.Records[0].Values["flag"] != "false" {
				t.Fatal("snapshot alias or data loss", current, err)
			}
			if _, err = repo.CompareAndSwap(ctx, repositoryDocument(2), current.SHA256); !errors.Is(err, errInputPreparationConflict) {
				t.Fatal("update created a missing collection", err)
			}
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err = repo.CompareAndSwap(cancelled, doc, current.SHA256); !errors.Is(err, context.Canceled) {
				t.Fatal("cancelled write", err)
			}
			if _, err = repo.Get(cancelled, doc.ID); !errors.Is(err, context.Canceled) {
				t.Fatal("cancelled read", err)
			}
			if _, err = repo.List(cancelled); !errors.Is(err, context.Canceled) {
				t.Fatal("cancelled listing", err)
			}
			codes := make(chan error, 2)
			for i := 0; i < 2; i++ {
				go func(i int) {
					candidate := repositoryDocument(1)
					candidate.Name = fmt.Sprint(i)
					_, err := repo.CompareAndSwap(ctx, candidate, current.SHA256)
					codes <- err
				}(i)
			}
			a, b := <-codes, <-codes
			if !(a == nil && errors.Is(b, errInputPreparationConflict) || b == nil && errors.Is(a, errInputPreparationConflict)) {
				t.Fatal("lost update", a, b)
			}
		})
	}
}

func TestInputPreparationRepositoryPreservesBytesAndRejectsInvalidReplacement(t *testing.T) {
	s, _ := newTestServer(t, false)
	repo := s.preparationRepository()
	ctx := context.Background()
	doc := repositoryDocument(1)
	if _, err := repo.CompareAndSwap(ctx, doc, ""); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.MarshalIndent(doc, "", "  ")
	raw = append(raw, '\n')
	path := filepath.Join(s.projectDir, inputPreparationDir, doc.ID+".json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	saved, err := repo.Get(ctx, doc.ID)
	if err != nil || saved.SHA256 != digestOf(raw) {
		t.Fatal("re-encoded saved snapshot", err)
	}
	if _, err = repo.List(ctx); err != nil {
		t.Fatal(err)
	}
	doc.Version = 999
	if _, err = repo.CompareAndSwap(ctx, doc, saved.SHA256); err == nil {
		t.Fatal("invalid replacement accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(raw) {
		t.Fatal("read or refused write changed saved bytes", err)
	}
}

func TestInputPreparationRepositoryCapacityIsAtomic(t *testing.T) {
	s, _ := newTestServer(t, false)
	repo := s.preparationRepository()
	ctx := context.Background()
	for i := 0; i < inputPreparationCountLimit-1; i++ {
		if _, err := repo.CompareAndSwap(ctx, repositoryDocument(i), ""); err != nil {
			t.Fatal(err)
		}
	}
	results := make(chan error, 2)
	for i := 1000; i < 1002; i++ {
		go func(i int) { _, err := repo.CompareAndSwap(ctx, repositoryDocument(i), ""); results <- err }(i)
	}
	a, b := <-results, <-results
	if !(a == nil && errors.Is(b, errInputPreparationCapacity) || b == nil && errors.Is(a, errInputPreparationCapacity)) {
		t.Fatal("capacity check raced", a, b)
	}
	items, err := repo.List(ctx)
	if err != nil || len(items) != inputPreparationCountLimit {
		t.Fatal(len(items), err)
	}
}

func TestInputPreparationHTTPUsesRepositoryWithoutFiles(t *testing.T) {
	s, _ := newTestServer(t, false)
	s.inputPreparations = &memoryPreparationRepository{data: map[string][]byte{}}
	call := func(method, path, body, revision string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("If-Match", revision)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := call("POST", "/api/input-preparations", `{"name":"Stored independently","recipe":{"intent":"Prepare records"}}`, "")
	var saved inputPreparationReply
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &saved) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	path := "/api/input-preparations/" + saved.Document.ID
	w = call("POST", path+"/imports", `{"kind":"csv","name":"source.csv","content":"name,count\nExample,00123\n"}`, saved.SHA256)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &saved) != nil || saved.Document.Records[0].Values["count"] != "00123" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call("PUT", path, `{"name":"Edited","recipe":{"intent":"New purpose"}}`, saved.SHA256)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &saved) != nil || saved.Document.RecipeVersion != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("GET", path, "", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("GET", "/api/input-preparations", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"records":1`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.projectDir, inputPreparationDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("HTTP consumer still opened filesystem storage", err)
	}
}

type interceptPreparationWrite struct {
	inputPreparationRepository
	commit func(context.Context, inputPreparation, string) (inputPreparationReply, error)
}

func (i interceptPreparationWrite) CompareAndSwap(ctx context.Context, doc inputPreparation, revision string) (inputPreparationReply, error) {
	return i.commit(ctx, doc, revision)
}

func TestInputPreparationConcurrentChatSaves(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint("different-settings=", changed), func(t *testing.T) {
			s, _ := newTestServer(t, false)
			repo := s.preparationRepository()
			arrived := make(chan struct{}, 2)
			proceed := make(chan struct{})
			release := sync.OnceFunc(func() { close(proceed) })
			defer release()
			s.inputPreparations = interceptPreparationWrite{repo, func(ctx context.Context, doc inputPreparation, revision string) (inputPreparationReply, error) {
				arrived <- struct{}{}
				<-proceed
				return repo.CompareAndSwap(ctx, doc, revision)
			}}
			results := make(chan *httptest.ResponseRecorder, 2)
			for i := 0; i < 2; i++ {
				go func(i int) {
					name := "Collection"
					if changed && i == 1 {
						name = "Different collection"
					}
					body := fmt.Sprintf(`{"name":%q,"recipe":{"intent":"Prepare records"},"chatOrigin":{"chatId":"chat-1","draftId":"draft-1"}}`, name)
					r := httptest.NewRequest("POST", "/api/input-preparations", strings.NewReader(body))
					r.Header.Set("Authorization", "Bearer "+testToken)
					w := httptest.NewRecorder()
					s.ServeHTTP(w, r)
					results <- w
				}(i)
			}
			// Both requests have observed absence before either can commit.
			for range 2 {
				select {
				case <-arrived:
				case w := <-results:
					t.Fatal("request completed before commit", w.Code, w.Body.String())
				case <-time.After(5 * time.Second):
					t.Fatal("request did not reach commit")
				}
			}
			release()
			a, b := <-results, <-results
			other := 200
			if changed {
				other = 409
			}
			if !(a.Code == 201 && b.Code == other || b.Code == 201 && a.Code == other) {
				t.Fatal(a.Code, a.Body.String(), b.Code, b.Body.String())
			}
			if !changed && a.Body.String() != b.Body.String() {
				t.Fatal("concurrent replay returned a different snapshot")
			}
			items, err := repo.List(context.Background())
			if err != nil || len(items) != 1 {
				t.Fatal("duplicate collection", items, err)
			}
		})
	}
}

func TestInputPreparationUncertainSaveIsNotReplayed(t *testing.T) {
	s, _ := newTestServer(t, false)
	repo := s.preparationRepository()
	writes := 0
	s.inputPreparations = interceptPreparationWrite{repo, func(ctx context.Context, doc inputPreparation, revision string) (inputPreparationReply, error) {
		writes++
		if _, err := repo.CompareAndSwap(ctx, doc, revision); err != nil {
			return inputPreparationReply{}, err
		}
		return inputPreparationReply{}, errors.New("verification unavailable after commit")
	}}
	body := `{"name":"Collection","recipe":{"intent":"Prepare records"},"chatOrigin":{"chatId":"chat-1","draftId":"draft-1"}}`
	r := httptest.NewRequest("POST", "/api/input-preparations", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if writes != 1 || w.Code != 409 {
		t.Fatal("uncertain operation was replayed or reported successful", writes, w.Code, w.Body.String())
	}
	items, err := repo.List(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatal("uncertain completion was discarded", items, err)
	}
}

func TestInputPreparationListDoesNotHideReadFailure(t *testing.T) {
	s, _ := newTestServer(t, false)
	repo := s.preparationRepository()
	ctx := context.Background()
	if _, err := repo.CompareAndSwap(ctx, repositoryDocument(1), ""); err != nil {
		t.Fatal(err)
	}
	prior := testHookPrivateRead
	t.Cleanup(func() { testHookPrivateRead = prior })
	testHookPrivateRead = func(string) error { return os.ErrNotExist }
	if items, err := repo.List(ctx); err == nil || items != nil {
		t.Fatal("failed read was reported as an empty collection", items, err)
	}
}

func (m *memoryPreparationRepository) acquire(ctx context.Context, wait bool) error {
	ctx, cancel := context.WithTimeout(ctx, privateDataLockWait)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if m.mu.TryLock() {
			if err := ctx.Err(); err != nil {
				m.mu.Unlock()
				return err
			}
			return nil
		}
		if !wait {
			return errInputPreparationBusy
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func (m *memoryPreparationRepository) Read(ctx context.Context) (inputPreparationRead, error) {
	if err := m.acquire(ctx, true); err != nil {
		return nil, err
	}
	return &memoryPreparationRead{owner: m, ctx: ctx}, nil
}
func (m *memoryPreparationRepository) TryRead(ctx context.Context) (inputPreparationRead, error) {
	if err := m.acquire(ctx, false); err != nil {
		return nil, err
	}
	return &memoryPreparationRead{owner: m, ctx: ctx}, nil
}

type memoryPreparationRead struct {
	mu     sync.Mutex
	owner  *memoryPreparationRepository
	ctx    context.Context
	closed bool
}

func (r *memoryPreparationRead) Get(id string) (inputPreparationReply, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return inputPreparationReply{}, errInputPreparationReadClosed
	}
	if err := r.ctx.Err(); err != nil {
		return inputPreparationReply{}, err
	}
	raw, ok := r.owner.data[id]
	if !ok {
		return inputPreparationReply{}, os.ErrNotExist
	}
	return decodeInputPreparation(id, raw)
}
func (r *memoryPreparationRead) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.closed = true
		r.owner.mu.Unlock()
	}
	return nil
}

type preparationReaderFunc func(string) (inputPreparationReply, error)

func (f preparationReaderFunc) Get(id string) (inputPreparationReply, error) { return f(id) }

func TestInputPreparationReadGuardsAdmission(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			var repo inputPreparationRepository = &memoryPreparationRepository{data: map[string][]byte{}}
			if backend == "file" {
				s, _ := newTestServer(t, false)
				repo = s.preparationRepository()
			}
			ctx := context.Background()
			first, err := repo.CompareAndSwap(ctx, repositoryDocument(1), "")
			if err != nil {
				t.Fatal(err)
			}
			second, err := repo.CompareAndSwap(ctx, repositoryDocument(2), "")
			if err != nil {
				t.Fatal(err)
			}
			view, err := repo.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			for _, want := range []inputPreparationReply{first, second} {
				got, err := view.Get(want.Document.ID)
				if err != nil || got.SHA256 != want.SHA256 {
					t.Fatal("inconsistent view", got, err)
				}
			}
			deadline, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
			defer cancel()
			candidate := first.Document
			candidate.Name = "Changed during admission"
			if _, err = repo.CompareAndSwap(deadline, candidate, first.SHA256); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("admitted snapshot was replaceable", err)
			}
			got, err := view.Get(first.Document.ID)
			if err != nil || got.SHA256 != first.SHA256 {
				t.Fatal("view changed", got, err)
			}
			if err = view.Close(); err != nil {
				t.Fatal(err)
			}
			if err = view.Close(); err != nil {
				t.Fatal("double close", err)
			}
			if _, err = view.Get(first.Document.ID); !errors.Is(err, errInputPreparationReadClosed) {
				t.Fatal("used released admission guard", err)
			}
			if _, err = repo.CompareAndSwap(ctx, candidate, first.SHA256); err != nil {
				t.Fatal("read guard was not released", err)
			}
		})
	}
}
