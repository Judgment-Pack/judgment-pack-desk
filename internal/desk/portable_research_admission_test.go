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

func portableAdmissionEntries(t *testing.T, root *os.Root) int {
	t.Helper()
	dir, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func fillPortableAdmissionRoot(t *testing.T, s *Server, count int) *os.Root {
	t.Helper()
	root, err := s.inputResearchRoot(true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	if _, err := root.Stat(".gitignore"); err != nil {
		t.Fatal(err)
	}
	// Include hidden regular files and non-run directories. Admission counts
	// every retained entry, even when it is not a readable research journal.
	before := portableAdmissionEntries(t, root)
	for i := before; i < count; i++ {
		name := fmt.Sprintf(".portable-admission-%03d", i)
		if i%2 == 0 {
			if err := root.Mkdir(name, 0700); err != nil {
				t.Fatal(err)
			}
		} else {
			f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := portableAdmissionEntries(t, root); got != count {
		t.Fatalf("root entries = %d, want %d", got, count)
	}
	return root
}

func portableAdmissionBody(run inputResearchRun) map[string]any {
	frozen := run.Portable.Frozen
	return map[string]any{"input": frozen.Input, "choice": frozen.Choice, "limits": frozen.Limits}
}

func TestPortableResearchAdmissionCountsAllRootEntries(t *testing.T) {
	for _, count := range []int{500, 501, 502} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s, ts, basis := portableResearchFixture(t, func(http.ResponseWriter, *http.Request) {
				t.Error("admission dispatched a model")
			})
			root := fillPortableAdmissionRoot(t, s, count)
			body := portableAdmissionBody(basis)
			code, raw := aiTestRequest(t, ts, "POST", "/api/portable-research", body, nil)
			if count == 500 {
				if code != 201 {
					t.Fatalf("last available slot: %d %s", code, raw)
				}
				var reply struct{ ID, Revision, State string }
				if err := json.Unmarshal(raw, &reply); err != nil || reply.State != "review" || reply.Revision == "" {
					t.Fatalf("invalid saved review: %s (%v)", raw, err)
				}
				rr, err := openInputResearch(root, reply.ID, false)
				if err != nil {
					t.Fatal(err)
				}
				saved, err := s.researchJournal(rr, reply.ID).Read(t.Context())
				rr.Close()
				if err != nil || saved.State != "review" || saved.Portable == nil || saved.Revision != reply.Revision {
					t.Fatalf("saved review unavailable: %+v (%v)", saved, err)
				}
				count++
				code, raw = aiTestRequest(t, ts, "POST", "/api/portable-research", body, nil)
			}
			if code != 409 || !bytes.Contains(raw, []byte("This project has reached its retained research-run limit.")) {
				t.Fatalf("full root: %d %s", code, raw)
			}
			if got := portableAdmissionEntries(t, root); got != count {
				t.Fatalf("rejected request changed root entries: %d, want %d", got, count)
			}
		})
	}
}

type portableAdmissionJournalProvider struct {
	researchJournalProvider
	create func(context.Context, inputResearchRun, researchJournal) error
}

func (p portableAdmissionJournalProvider) Run(id string) researchJournal {
	return portableAdmissionJournal{researchJournal: p.researchJournalProvider.Run(id), create: p.create}
}

type portableAdmissionJournal struct {
	researchJournal
	create func(context.Context, inputResearchRun, researchJournal) error
}

func (j portableAdmissionJournal) Create(ctx context.Context, run inputResearchRun) error {
	return j.create(ctx, run, j.researchJournal)
}

func portableAdmissionRequest(ctx context.Context, ts *httptest.Server, body any) (int, []byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	r, err := http.NewRequestWithContext(ctx, "POST", ts.URL+"/api/portable-research", bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	bearer(r)
	r.Header.Set("Content-Type", "application/json")
	response, err := ts.Client().Do(r)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err = io.ReadAll(response.Body)
	return response.StatusCode, raw, err
}

func TestPortableResearchAdmissionHoldsOriginalRootThroughCreate(t *testing.T) {
	s, ts, basis := portableResearchFixture(t, func(http.ResponseWriter, *http.Request) {
		t.Error("admission dispatched a model")
	})
	root := fillPortableAdmissionRoot(t, s, 500)
	entered, release := make(chan struct{}), make(chan struct{})
	var once, started sync.Once
	releaseCreate := func() { once.Do(func() { close(release) }) }
	defer releaseCreate()
	s.researchJournals = portableAdmissionJournalProvider{researchJournalProvider: newMemoryResearchJournals(), create: func(ctx context.Context, run inputResearchRun, journal researchJournal) error {
		started.Do(func() { close(entered) })
		select {
		case <-release:
			return journal.Create(ctx, run)
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	type result struct {
		code int
		raw  []byte
		err  error
	}
	done := make(chan result, 1)
	body := portableAdmissionBody(basis)
	go func() {
		code, raw, err := portableAdmissionRequest(ctx, ts, body)
		done <- result{code, raw, err}
	}()
	select {
	case <-entered:
	case reply := <-done:
		t.Fatalf("request did not reach Create: %+v", reply)
	case <-ctx.Done():
		t.Fatal("request did not reach Create before deadline")
	}
	probe, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	if err := lockPrivateData(probe, true); !lockHeld(err) {
		t.Fatalf("Create did not retain the original root EX: %v", err)
	}
	code, raw, err := portableAdmissionRequest(ctx, ts, body)
	if err != nil || code != 409 || !bytes.Contains(raw, []byte("Another research change is being saved. Try again after it finishes.")) {
		t.Fatalf("concurrent admission: %d %s (%v)", code, raw, err)
	}
	releaseCreate()
	reply := <-done
	if reply.err != nil || reply.code != 201 {
		t.Fatalf("first admission: %d %s (%v)", reply.code, reply.raw, reply.err)
	}
	if err := lockPrivateData(probe, true); err != nil {
		t.Fatalf("admission remained held after Create: %v", err)
	}
	probe.Close()
	code, raw = aiTestRequest(t, ts, "POST", "/api/portable-research", body, nil)
	if code != 409 || !bytes.Contains(raw, []byte("retained research-run limit")) || portableAdmissionEntries(t, root) != 501 {
		t.Fatalf("final slot was bypassed: %d %s", code, raw)
	}
}

func TestPortableResearchAdmissionReleasesRootOnCreateError(t *testing.T) {
	s, ts, basis := portableResearchFixture(t, func(http.ResponseWriter, *http.Request) {
		t.Error("admission dispatched a model")
	})
	root := fillPortableAdmissionRoot(t, s, 1)
	s.researchJournals = portableAdmissionJournalProvider{researchJournalProvider: newMemoryResearchJournals(), create: func(context.Context, inputResearchRun, researchJournal) error {
		return errors.New("injected create failure")
	}}
	code, raw := aiTestRequest(t, ts, "POST", "/api/portable-research", portableAdmissionBody(basis), nil)
	if code != 409 || !strings.Contains(string(raw), "injected create failure") {
		t.Fatalf("create failure: %d %s", code, raw)
	}
	probe, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	if err := lockPrivateData(probe, true); err != nil {
		t.Fatalf("failed Create retained admission: %v", err)
	}
	// Preserve the current failed-create outcome: its run directory is retained.
	if got := portableAdmissionEntries(t, root); got != 2 {
		t.Fatalf("failed Create root entries = %d, want 2", got)
	}
}
