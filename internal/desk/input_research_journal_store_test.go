package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Test-only workspace backend. No file roots, provider calls or retry loop.
type memoryResearchJournals struct {
	mu      sync.Mutex
	entries map[string]*memoryResearchEntry
}
type memoryResearchEntry struct {
	gate chan struct{}
	data []byte
}
type memoryResearchJournal struct {
	entry *memoryResearchEntry
	id    string
}

func newMemoryResearchJournals() *memoryResearchJournals {
	return &memoryResearchJournals{entries: map[string]*memoryResearchEntry{}}
}
func (r *memoryResearchJournals) Run(id string) researchJournal {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.entries[id]
	if entry == nil {
		entry = &memoryResearchEntry{gate: make(chan struct{}, 1)}
		r.entries[id] = entry
	}
	return memoryResearchJournal{entry, id}
}
func (j memoryResearchJournal) locked(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !inputResearchID.MatchString(j.id) {
		return errors.New("Invalid research ID.")
	}
	select {
	case j.entry.gate <- struct{}{}:
		defer func() { <-j.entry.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
}
func (j memoryResearchJournal) Read(ctx context.Context) (inputResearchRun, error) {
	var run inputResearchRun
	err := j.locked(ctx, func() error {
		raw := j.entry.data
		if raw == nil {
			return os.ErrNotExist
		}
		var err error
		run, err = decodeResearchJournal(raw, j.id)
		return err
	})
	return run, err
}
func (j memoryResearchJournal) Create(ctx context.Context, run inputResearchRun) error {
	raw, err := encodeResearchJournal(run, j.id)
	if err != nil {
		return err
	}
	return j.locked(ctx, func() error {
		if j.entry.data != nil {
			return os.ErrExist
		}
		j.entry.data = append([]byte(nil), raw...)
		return nil
	})
}
func (j memoryResearchJournal) Update(ctx context.Context, change func(*inputResearchRun) error) (inputResearchRun, error) {
	var run inputResearchRun
	err := j.locked(ctx, func() error {
		raw := j.entry.data
		if raw == nil {
			return os.ErrNotExist
		}
		next, err := changeResearchJournal(raw, j.id, change)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		j.entry.data = append([]byte(nil), next...)
		run, err = decodeResearchJournal(next, j.id)
		return err
	})
	return run, err
}
func journalFixture(t *testing.T, backend string) (researchJournal, inputResearchRun, *os.Root) {
	t.Helper()
	root, run := researchRunFixture(t)
	if err := root.Remove("record.json"); err != nil {
		t.Fatal(err)
	}
	var journal researchJournal = fileResearchJournal{root, run.ID}
	if backend == "memory" {
		journal = newMemoryResearchJournals().Run(run.ID)
	}
	return journal, run, root
}
func TestResearchJournalContract(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			j, run, root := journalFixture(t, backend)
			ctx := t.Context()
			if _, err := j.Read(ctx); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("missing", err)
			}
			if _, err := j.Update(ctx, func(*inputResearchRun) error { t.Fatal("called on missing journal"); return nil }); !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if err := j.Create(ctx, run); err != nil {
				t.Fatal(err)
			}
			if err := j.Create(ctx, run); !errors.Is(err, os.ErrExist) {
				t.Fatal("replaced journal", err)
			}
			snapshot, err := j.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			snapshot.Seeds[0] = "changed"
			snapshot.Recipe.Fields[0].Name = "changed"
			saved, err := j.Read(ctx)
			if err != nil || saved.Seeds[0] != run.Seeds[0] || saved.Recipe.Fields[0].Name != run.Recipe.Fields[0].Name {
				t.Fatal("aliased read", err)
			}
			var calls atomic.Int32
			var wg sync.WaitGroup
			for i := 0; i < 12; i++ {
				wg.Go(func() {
					if _, err := j.Update(ctx, func(r *inputResearchRun) error { calls.Add(1); r.Step++; return nil }); err != nil {
						t.Error(err)
					}
				})
			}
			wg.Wait()
			saved, err = j.Read(ctx)
			if err != nil || saved.Step != 12 || calls.Load() != 12 {
				t.Fatal("lost/repeated callback", saved.Step, calls.Load(), err)
			}
			stop := errors.New("refused mutation")
			failed, err := j.Update(ctx, func(r *inputResearchRun) error { r.Step = 0; return stop })
			if !errors.Is(err, stop) || failed.ID != "" {
				t.Fatal("acknowledged failed write", err)
			}
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := j.Read(cancelled); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if _, err := j.Update(cancelled, func(*inputResearchRun) error { t.Fatal("cancelled callback"); return nil }); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			cancelled, cancel = context.WithCancel(ctx)
			if _, err := j.Update(cancelled, func(r *inputResearchRun) error { r.Step = 0; cancel(); return nil }); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			saved, err = j.Read(ctx)
			if err != nil || saved.Step != 12 {
				t.Fatal("failed update changed data", saved.Step, err)
			}
			for _, mutate := range []func(*inputResearchRun){func(r *inputResearchRun) { r.ID = "research_" + strings.Repeat("f", 32) }, func(r *inputResearchRun) { r.Recipe.Limits.Records = 1000 }} {
				if _, err := j.Update(ctx, func(r *inputResearchRun) error { mutate(r); return nil }); err == nil {
					t.Fatal("accepted invalid replacement")
				}
			}
			if backend == "memory" {
				if _, err := root.Stat("record.json"); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("file fallback", err)
				}
			}
		})
	}
}
func TestResearchJournalRetainsOperationIdentity(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			j, run, _ := journalFixture(t, backend)
			ctx := t.Context()
			a, _, err := beginResearchAttempt(&run, "model", json.RawMessage(`{"step":0}`))
			if err != nil {
				t.Fatal(err)
			}
			result := json.RawMessage(`{"zero":0,"false":false,"null":null,"number":24,"string":"24"}`)
			if err := finishResearchAttempt(&run, a.ID, result, ""); err != nil {
				t.Fatal(err)
			}
			if err := j.Create(ctx, run); err != nil {
				t.Fatal(err)
			}
			for _, mutate := range []func(*inputResearchRun){
				func(r *inputResearchRun) { r.Attempts = nil }, func(r *inputResearchRun) { r.Attempts[0].ID = "replacement" }, func(r *inputResearchRun) { r.Attempts[0].Digest = "other" }, func(r *inputResearchRun) { r.Attempts[0].State = "started" }, func(r *inputResearchRun) { r.Attempts[0].Result = json.RawMessage(`{}`) },
			} {
				if _, err := j.Update(ctx, func(r *inputResearchRun) error { mutate(r); return nil }); err == nil {
					t.Fatal("rewrote retained operation")
				}
			}
			saved, err := j.Update(ctx, func(r *inputResearchRun) error { r.Attempts[0].ActionProblem = "Needs correction"; return nil })
			if err != nil || !bytes.Equal(saved.Attempts[0].Result, result) {
				t.Fatal("lost typed result", err)
			}
			saved.Attempts[0].Result[0] = '['
			reread, err := j.Read(ctx)
			if err != nil || !bytes.Equal(reread.Attempts[0].Result, result) {
				t.Fatal("aliased update result", err)
			}
		})
	}
}
func TestResearchFileJournalUncertainWriteIsNotReplayed(t *testing.T) {
	j, run, _ := journalFixture(t, "file")
	if err := j.Create(t.Context(), run); err != nil {
		t.Fatal(err)
	}
	count, calls := 0, 0
	testHookPrivateRead = func(name string) error {
		if name == "record.json" {
			count++
			if count == 2 {
				return errors.New("read-back unavailable")
			}
		}
		return nil
	}
	t.Cleanup(func() { testHookPrivateRead = nil })
	store := researchRunStore{journal: j}
	args := json.RawMessage(`{"step":0}`)
	invoke := func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{}`), nil
	}
	if _, err := researchAttempt(t.Context(), store, "model", args, invoke); err == nil || calls != 0 {
		t.Fatal("dispatched after uncertain intent", err, calls)
	}
	testHookPrivateRead = nil
	if _, err := researchAttempt(t.Context(), store, "model", args, invoke); err == nil || calls != 0 {
		t.Fatal("replayed uncertain intent", err, calls)
	}
	saved, err := j.Read(t.Context())
	if err != nil || len(saved.Attempts) != 1 || saved.Attempts[0].State != "started" {
		t.Fatal("lost intent", err)
	}
}
func TestResearchFileJournalCancelledLockWaitAndLegacyRead(t *testing.T) {
	j, run, root := journalFixture(t, "file")
	run.Recipe.Target = ""
	if err := j.Create(t.Context(), run); err != nil {
		t.Fatal(err)
	}
	before, err := readPrivateData(root, "record.json", inputResearchRecordLimit)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := j.Read(t.Context())
	if err != nil || saved.Recipe.Target != "records" {
		t.Fatal("legacy default", err)
	}
	after, _ := readPrivateData(root, "record.json", inputResearchRecordLimit)
	if !bytes.Equal(before, after) {
		t.Fatal("read migrated legacy journal")
	}
	lock, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := lockPrivateData(lock, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	if _, err := j.Update(ctx, func(*inputResearchRun) error { t.Fatal("entered locked callback"); return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
func useMemoryResearchFixture(t *testing.T, s *Server) *memoryResearchJournals {
	t.Helper()
	repo := newMemoryResearchJournals()
	root, err := s.inputResearchRoot(false)
	if errors.Is(err, os.ErrNotExist) {
		s.researchJournals = repo
		return repo
	}
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !inputResearchID.MatchString(e.Name()) {
			continue
		}
		rr, err := openInputResearch(root, e.Name(), false)
		if err != nil {
			t.Fatal(err)
		}
		run, err := s.researchJournal(rr, e.Name()).Read(t.Context())
		if err == nil {
			err = repo.Run(e.Name()).Create(t.Context(), run)
		}
		if err == nil {
			err = rr.Remove("record.json")
		}
		rr.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	s.researchJournals = repo
	return repo
}
func assertNoResearchJournalFiles(t *testing.T, s *Server) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(s.projectDir, inputResearchDirectory, "research_*", "record.json"))
	if err != nil || len(paths) != 0 {
		t.Fatal("journal file fallback", paths, err)
	}
}

func TestResearchJournalScopeAndUnreadableHistory(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			j, run, root := journalFixture(t, backend)
			other, _, _ := journalFixture(t, backend)
			if err := j.Create(t.Context(), run); err != nil {
				t.Fatal(err)
			}
			if _, err := other.Read(t.Context()); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("cross-workspace read", err)
			}
			wrong := run
			wrong.ID = "research_" + strings.Repeat("e", 32)
			if err := other.Create(t.Context(), wrong); err == nil {
				t.Fatal("wrong run identity accepted")
			}
			if backend == "file" {
				if err := writePrivateData(root, "record.json", []byte(`{}`)); err != nil {
					t.Fatal(err)
				}
			} else {
				j.(memoryResearchJournal).entry.data = []byte(`{}`)
			}
			if _, err := j.Read(t.Context()); err == nil {
				t.Fatal("corruption hidden")
			}
			if _, err := j.Update(t.Context(), func(*inputResearchRun) error { t.Fatal("updated unreadable journal"); return nil }); err == nil {
				t.Fatal("corruption overwritten")
			}
			if err := j.Create(t.Context(), run); !errors.Is(err, os.ErrExist) {
				t.Fatal("unreadable journal treated as missing", err)
			}
		})
	}
}

type uncertainCompletionJournal struct {
	researchJournal
	failed bool
}

func (j *uncertainCompletionJournal) Update(ctx context.Context, change func(*inputResearchRun) error) (inputResearchRun, error) {
	saved, err := j.researchJournal.Update(ctx, change)
	if err == nil && !j.failed && len(saved.Attempts) > 0 && saved.Attempts[len(saved.Attempts)-1].State == "completed" {
		j.failed = true
		return inputResearchRun{}, errors.New("completion acknowledgement unavailable")
	}
	return saved, err
}
func TestResearchJournalUncertainCompletionRetainsResultWithoutProviderReplay(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			j, run, _ := journalFixture(t, backend)
			if err := j.Create(t.Context(), run); err != nil {
				t.Fatal(err)
			}
			store := researchRunStore{journal: &uncertainCompletionJournal{researchJournal: j}}
			calls := 0
			result := json.RawMessage(`{"retained":true}`)
			invoke := func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
				calls++
				return result, nil
			}
			args := json.RawMessage(`{"step":0}`)
			if _, err := researchAttempt(t.Context(), store, "model", args, invoke); err == nil || calls != 1 {
				t.Fatal("uncertain completion acknowledged", err, calls)
			}
			recovered, err := researchAttempt(t.Context(), store, "model", args, invoke)
			if err != nil || calls != 1 || !bytes.Equal(recovered, result) {
				t.Fatal("lost result or repeated provider", calls, err)
			}
		})
	}
}
