package desk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Handwritten admission fixtures use the actual guarded file repository and
// immutable spool. Fake preparation proves source/effect order, never actual B3
// or generated producer authority. No production route registers this alias.
const retainedReviewInitialAlias = "/initial-selection-fixture"

func retainedReviewInitialRegister(s *Server) {
	s.mux.HandleFunc("/api/input-preparations/{preparation}"+retainedReviewInitialAlias, s.handleRetainedPreparedSelection)
}
func retainedReviewInitialRequest(id string, source retainedSelectionWireSource, raw string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/input-preparations/"+source.PreparationID+retainedReviewInitialAlias, strings.NewReader(raw))
	r.SetPathValue("preparation", source.PreparationID)
	r.Header.Set("Authorization", "Bearer "+id)
	r.Header.Set("Accept", retainedSelectionMediaType)
	r.Header.Set("If-Match", source.Revision)
	r.Header.Set("Content-Type", "application/json")
	return r
}
func retainedReviewInitialSelected(source retainedSelectionWireSource) retainedReviewSelection {
	return retainedReviewSelection{source.PreparationID, source.Revision, append([]string(nil), source.RecordIDs...), source.RequestBytes}
}

type retainedReviewInitialRepository struct {
	inputPreparationRepository
	server                          *Server
	reads, gets, directGets, closes atomic.Int64
	onRead                          func(context.Context) error
	mutate                          func(*inputPreparationReply)
}
type retainedReviewInitialRead struct {
	inputPreparationRead
	owner *retainedReviewInitialRepository
}

func (r *retainedReviewInitialRepository) Read(ctx context.Context) (inputPreparationRead, error) {
	r.reads.Add(1)
	if r.server != nil {
		r.server.retainedReviews.mu.Lock()
		reserved := r.server.retainedReviews.builder != nil
		r.server.retainedReviews.mu.Unlock()
		if !reserved {
			return nil, errors.New("repository read before reservation")
		}
	}
	if r.onRead != nil {
		if err := r.onRead(ctx); err != nil {
			return nil, err
		}
	}
	held, err := r.inputPreparationRepository.Read(ctx)
	if err != nil {
		return nil, err
	}
	return &retainedReviewInitialRead{held, r}, nil
}
func (r *retainedReviewInitialRepository) Get(ctx context.Context, id string) (inputPreparationReply, error) {
	r.directGets.Add(1)
	return r.inputPreparationRepository.Get(ctx, id)
}
func (r *retainedReviewInitialRead) Get(id string) (inputPreparationReply, error) {
	r.owner.gets.Add(1)
	saved, err := r.inputPreparationRead.Get(id)
	if err == nil && r.owner.mutate != nil {
		r.owner.mutate(&saved)
	}
	return saved, err
}
func (r *retainedReviewInitialRead) Close() error {
	r.owner.closes.Add(1)
	return r.inputPreparationRead.Close()
}
func retainedReviewInitialTracked(s *Server) *retainedReviewInitialRepository {
	repo := &retainedReviewInitialRepository{inputPreparationRepository: s.preparationRepository(), server: s}
	s.inputPreparations = repo
	return repo
}

// I1: real request guards and exact initial header/body shape, before reserve.
func TestRetainedReviewInitialShapeAndExactHeaders(t *testing.T) {
	s, id, source := retainedReviewFixture(t)
	retainedReviewInitialRegister(s)
	repo := retainedReviewInitialTracked(s)
	// Any attempt to reserve would return 410 instead of the expected shape
	// status. Thus these refusals cannot allocate a file or invoke preparation.
	s.retainedReviews.mu.Lock()
	s.retainedReviews.closed = true
	s.retainedReviews.mu.Unlock()
	type shape struct {
		name   string
		status int
		raw    string
		edit   func(*http.Request)
	}
	cases := []shape{
		{"missing Accept", 406, "", func(r *http.Request) { r.Header.Del("Accept") }},
		{"multiple Accept", 406, "", func(r *http.Request) { r.Header.Add("Accept", retainedSelectionMediaType) }},
		{"second Accept", 406, "", func(r *http.Request) { r.Header.Add("Accept", "application/json") }},
		{"Accept list", 406, "", func(r *http.Request) { r.Header.Set("Accept", retainedSelectionMediaType+", application/json") }},
		{"Accept wildcard", 406, "", func(r *http.Request) { r.Header.Set("Accept", "*/*") }},
		{"Accept q", 406, "", func(r *http.Request) { r.Header.Set("Accept", retainedSelectionMediaType+";q=1") }},
		{"missing revision", 400, "", func(r *http.Request) { r.Header.Del("If-Match") }},
		{"second revision", 400, "", func(r *http.Request) { r.Header.Add("If-Match", strings.Repeat("0", 64)) }},
		{"identical second revision", 400, "", func(r *http.Request) { r.Header.Add("If-Match", source.Revision) }},
		{"weak revision", 400, "", func(r *http.Request) { r.Header.Set("If-Match", `W/"`+source.Revision+`"`) }},
		{"quoted revision", 400, "", func(r *http.Request) { r.Header.Set("If-Match", `"`+source.Revision+`"`) }},
		{"star revision", 400, "", func(r *http.Request) { r.Header.Set("If-Match", "*") }},
		{"revision list", 400, "", func(r *http.Request) { r.Header.Set("If-Match", source.Revision+","+source.Revision) }},
		{"uppercase revision", 400, "", func(r *http.Request) { r.Header.Set("If-Match", strings.Repeat("A", 64)) }},
		{"wrong method", 400, "", func(r *http.Request) { r.Method = http.MethodGet }},
		{"invalid path", 400, "", func(r *http.Request) { r.URL.Path = "/api/input-preparations/invalid" + retainedReviewInitialAlias }},
		{"query", 400, "", func(r *http.Request) { r.URL.RawQuery = "x=1" }},
		{"empty query", 400, "", func(r *http.Request) { r.URL.ForceQuery = true }},
		{"Range", 400, "", func(r *http.Request) { r.Header.Set("Range", "bytes=0-1") }},
		{"empty Range", 400, "", func(r *http.Request) { r.Header.Set("Range", "") }},
		{"Content-Range", 400, "", func(r *http.Request) { r.Header.Set("Content-Range", "") }},
		{"missing MIME", 415, "", func(r *http.Request) { r.Header.Del("Content-Type") }},
		{"malformed MIME", 415, "", func(r *http.Request) { r.Header.Set("Content-Type", "application/json; =broken") }},
		{"wrong MIME", 415, "", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
		{"multiple MIME", 415, "", func(r *http.Request) { r.Header.Add("Content-Type", "application/json") }},
		{"MIME list", 415, "", func(r *http.Request) { r.Header.Set("Content-Type", "application/json, application/json") }},
		{"encoded", 415, "", func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }},
		{"identity list", 415, "", func(r *http.Request) { r.Header.Set("Content-Encoding", "identity, identity") }},
		{"empty encoding", 415, "", func(r *http.Request) { r.Header.Set("Content-Encoding", "") }},
		{"multiple encoding", 415, "", func(r *http.Request) {
			r.Header.Add("Content-Encoding", "identity")
			r.Header.Add("Content-Encoding", "identity")
		}},
	}
	for i, raw := range []string{
		`null`, `{}`, `{"recordIds":null}`, `{"recordIds":[]}`, `{"recordIds":[null]}`, `{"recordIds":[""]}`, `{"recordIds":[1]}`, `{"recordIds":[true]}`,
		`{"recordIds":["row-1","row-\u0031"]}`, `{"recordIds":["row-1"],"RecordIds":["row-1"]}`,
		`{"recordIds":["row-1"],"record\u0049ds":["row-1"]}`, `{"recordIds":["row-1"],"subscriptionId":"secret"}`,
		`{"recordIds":["row-1"],"packDigest":"secret"}`, `{"recordIds":["row-1"],"origin":"asserted"}`,
		`{"recordIds":["row-1"],"conversionContract":{}}`, `{"recordIds":["row-1"],"requestBytes":1}`,
		`{"recordIds":["\ud800"]}`, "{\"recordIds\":[\"\xff\"]}", `{"recordIds":"row-1"}`, `{"recordIds":["row-1"]} null`,
	} {
		cases = append(cases, shape{fmt.Sprintf("JSON %d", i), 400, raw, nil})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := tc.raw
			if raw == "" {
				raw = `{"recordIds":["row-1"]}`
			}
			r := retainedReviewInitialRequest(id, source, raw)
			if tc.edit != nil {
				tc.edit(r)
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"`+CodeBadRequest+`"`) || strings.Contains(w.Body.String(), "secret") {
				t.Fatal("wrong fixed request refusal", w.Code, w.Body.String())
			}
		})
	}
	if repo.reads.Load() != 0 || repo.gets.Load() != 0 || repo.directGets.Load() != 0 {
		t.Fatal("shape refusal read source")
	}
	retainedReviewAssertNoReservations(t, s)
}

// I2: raw bytes, including arbitrary splits, final n>0/EOF, whitespace and
// escapes. Content-Length never determines the admitted request accounting.
type retainedReviewInitialBody struct {
	raw                   string
	offset, reads, closes int
}

func (b *retainedReviewInitialBody) Read(p []byte) (int, error) {
	b.reads++
	if b.offset == len(b.raw) {
		return 0, io.EOF
	}
	n := min(len(p), 1+b.reads%7, len(b.raw)-b.offset)
	copy(p, b.raw[b.offset:b.offset+n])
	b.offset += n
	if b.offset == len(b.raw) {
		return n, io.EOF
	}
	return n, nil
}
func (b *retainedReviewInitialBody) Close() error { b.closes++; return io.ErrClosedPipe }
func TestRetainedReviewInitialActualRequestBytes(t *testing.T) {
	s, _, source := retainedReviewFixture(t)
	saved, err := s.preparationRepository().Get(t.Context(), source.PreparationID)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"recordIds":["row-1"]}`, " \n { \"record\\u0049ds\" : [ \"row-\\u0031\" ] } \t\r\n", `{"recordIds":["ré😀"]}`, `{"recordIds":["r\u00e9\ud83d\ude00"]}`} {
		for _, declared := range []int64{-1, 1, 9999} {
			r := retainedReviewInitialRequest("", source, raw)
			body := &retainedReviewInitialBody{raw: raw}
			r.Body = body
			r.ContentLength = declared
			r.Header.Set("Content-Type", `application/json; charset=UTF-8`)
			r.Header.Set("Content-Encoding", "identity")
			w := httptest.NewRecorder()
			selection, ok := retainedReviewInitialSelection(w, r)
			if !ok || selection.RequestBytes != int64(len(raw)) || body.offset != len(raw) {
				t.Fatal("raw request accounting changed", ok, selection, w.Body.String())
			}
			if selection.RecordIDs[0] == "row-1" {
				derived, err := retainedReviewSourceFromSaved(saved, selection)
				if err != nil || derived.RequestBytes != int64(len(raw)) {
					t.Fatal("derived source lost exact request bytes", err)
				}
			} else if selection.RecordIDs[0] != "ré😀" {
				t.Fatal("Unicode scalar decoding changed", selection.RecordIDs)
			}
		}
	}
	for _, size := range []int{retainedSelectionRequestBytes, retainedSelectionRequestBytes + 1} {
		raw := `{"recordIds":["row-1"]}`
		raw += strings.Repeat(" ", size-len(raw))
		r := retainedReviewInitialRequest("", source, raw)
		r.ContentLength = 1
		w := httptest.NewRecorder()
		selection, ok := retainedReviewInitialSelection(w, r)
		if size == retainedSelectionRequestBytes {
			if !ok || selection.RequestBytes != int64(size) {
				t.Fatal("legal parser boundary refused", w.Body.String())
			}
		} else if ok || w.Code != 413 || !strings.Contains(w.Body.String(), `"`+CodeTooLarge+`"`) {
			t.Fatal("actual oversized request admitted", ok, w.Code, w.Body.String())
		}
	}
	body := &retainedReviewInitialBody{raw: "x"}
	counted := &retainedReviewCountedBody{ReadCloser: body}
	if raw, err := io.ReadAll(counted); err != nil || string(raw) != "x" || counted.bytes != 1 {
		t.Fatal("n plus EOF lost", err)
	}
	if err := counted.Close(); !errors.Is(err, io.ErrClosedPipe) || body.closes != 1 {
		t.Fatal("Close did not delegate", err)
	}
}

// I3: the one held saved source supplies all authority metadata and request
// order. A compatibility expectation can refuse but cannot supply metadata.
func TestRetainedReviewInitialSameSavedSource(t *testing.T) {
	s, id, expected := retainedReviewFixtureRows(t, true)
	expected.RecordIDs = []string{"row-2", "row-1"}
	selection := retainedReviewInitialSelected(expected)
	repo := retainedReviewInitialTracked(s)
	calls := 0
	d, err := s.buildRetainedReviewCore(t.Context(), retainedReviewFixtureOwner(t, s, id), selection, nil, func(ctx context.Context, saved inputPreparationReply, record string) (retainedPreparedInput, error) {
		if repo.closes.Load() != 0 || saved.SHA256 != expected.Revision || saved.Document.ID != expected.PreparationID || saved.Document.Recipe.Qualification.Digest != expected.PackDigest || record != expected.RecordIDs[calls] {
			return retainedPreparedInput{}, errors.New("preparation did not use the one held saved source")
		}
		calls++
		return retainedReviewFixturePrepare(ctx, saved, record)
	}, newAnonymousRetainedReviewFile)
	if err != nil {
		t.Fatal(err)
	}
	defer d.abort()
	if !reflect.DeepEqual(d.view.source, expected) || calls != 2 || repo.reads.Load() != 1 || repo.gets.Load() != 1 || repo.directGets.Load() != 0 || repo.closes.Load() != 1 {
		t.Fatal("source was caller-derived or reread", d.view.source, repo.reads.Load(), repo.gets.Load(), repo.closes.Load())
	}
	w := httptest.NewRecorder()
	if err := d.deliver(w); err != nil {
		t.Fatal(err)
	}
	header, rows, err := readRetainedSelectionWire(strings.NewReader(w.Body.String()), expected, int64(w.Body.Len()), func(_ retainedSelectionWireDescriptor, raw []byte) error {
		if string(raw) != string(retainedReviewTestInput) {
			return errors.New("changed Input")
		}
		return nil
	})
	if err != nil || header.PackDigest != expected.PackDigest || header.ConversionContract != expected.ConversionContract || header.Origin != "asserted" || len(rows) != 2 || rows[0].ID != "row-2" {
		t.Fatal("saved metadata or selection order lost", err)
	}
	for _, field := range []string{"pack", "origin"} {
		wrong := expected
		if field == "pack" {
			wrong.PackDigest = "sha256:" + strings.Repeat("0", 64)
		} else {
			wrong.Origin = "generated"
		}
		prepared, created := 0, 0
		next, err := s.buildRetainedReviewUsing(t.Context(), id, wrong, func(context.Context, inputPreparationReply, string) (retainedPreparedInput, error) {
			prepared++
			return retainedPreparedInput{}, nil
		}, func(*os.Root) (retainedReviewFile, error) { created++; return nil, os.ErrPermission })
		if next != nil {
			next.abort()
			t.Fatal("fixture expectation supplied authority")
		}
		if err == nil || prepared != 0 || created != 0 {
			t.Fatal("fixture expectation not asserted before effects", err)
		}
	}
}

func retainedReviewInitialGeneratedFixture(t *testing.T) (*Server, string, retainedSelectionWireSource) {
	t.Helper()
	s, _ := newTestServer(t, false)
	doc := repositoryDocument(72)
	first := doc.Records[0]
	first.Class = "generated"
	first.Research = &inputResearchRecord{}
	second := first
	second.ID = "row-2"
	doc.Records = []preparedInputRecord{first, second}
	repo := s.preparationRepository()
	saved, err := repo.CompareAndSwap(t.Context(), doc, "")
	if err != nil {
		t.Fatal(err)
	}
	// Establish human review from the trusted raw-cell snapshot, with retained
	// matching events. This is structural readiness, not producer authority.
	doc = saved.Document
	doc.Recipe.Qualification = retainedB4Qualification(t)
	for i, record := range doc.Records {
		review := inputRecordReview{RecordID: record.ID, RecordDigest: inputRecordDigest(record), RecipeVersion: doc.RecipeVersion, Decision: "accepted", At: "2026-10-15T00:00:00Z", Reviewer: inputReviewActor{Kind: "local-access"}}
		doc.Reviews = append(doc.Reviews, inputRecordReviewEvent{Review: review, Record: record})
		doc.Records[i].Review = &review
	}
	saved, err = repo.CompareAndSwap(t.Context(), doc, saved.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.sessions.create("initial generated fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, id, retainedSelectionWireSource{PreparationID: doc.ID, Revision: saved.SHA256, PackDigest: doc.Recipe.Qualification.Digest, ConversionContract: retainedSelectionInstalledContract(), Origin: "generated", RecordIDs: []string{"row-1", "row-2"}, RequestBytes: 64}
}

// I4: every selected row must pass source/cell/origin/current-human-review
// admission before the first anonymous file or preparation callback.
func TestRetainedReviewInitialEntireSelectionBeforeEffects(t *testing.T) {
	for _, failure := range []string{"later missing", "later duplicate saved", "later changed custody", "mixed origin", "ordinary", "wrong exact tuple", "generated later missing review", "generated later stale review", "generated later missing human event", "generated valid review"} {
		t.Run(failure, func(t *testing.T) {
			var s *Server
			var id string
			var source retainedSelectionWireSource
			generated := strings.HasPrefix(failure, "generated")
			if generated {
				s, id, source = retainedReviewInitialGeneratedFixture(t)
			} else {
				s, id, source = retainedReviewFixtureRows(t, true)
			}
			saved, err := s.preparationRepository().Get(t.Context(), source.PreparationID)
			if err != nil {
				t.Fatal(err)
			}
			persist := false
			switch failure {
			case "later missing":
				source.RecordIDs = []string{"row-1", "missing"}
			case "later duplicate saved":
				saved.Document.Records = append(saved.Document.Records, saved.Document.Records[1])
				persist = true
			case "mixed origin":
				saved.Document.Records[1].Class = "generated"
				saved.Document.Records[1].Research = &inputResearchRecord{}
				persist = true
			case "ordinary":
				saved.Document.Recipe.Qualification = nil
				persist = true
			case "generated later missing review":
				saved.Document.Records[1].Review = nil
				persist = true
			case "generated later stale review":
				saved.Document.RecipeVersion++
				persist = true
			case "generated later missing human event":
				saved.Document.Reviews = saved.Document.Reviews[:1]
				persist = true
			}
			if persist {
				saved, err = s.preparationRepository().CompareAndSwap(t.Context(), saved.Document, saved.SHA256)
				if err != nil {
					t.Fatal(err)
				}
				source.Revision = saved.SHA256
			}
			repo := retainedReviewInitialTracked(s)
			if failure == "later changed custody" {
				repo.mutate = func(reply *inputPreparationReply) { reply.Document.Records[1].Values["count"] = "changed" }
			}
			if failure == "wrong exact tuple" {
				repo.mutate = func(reply *inputPreparationReply) {
					contract := *reply.Document.Recipe.Qualification.ConversionContract
					contract.Profile = "uninstalled"
					reply.Document.Recipe.Qualification.ConversionContract = &contract
				}
			}
			prepared, created := 0, 0
			d, err := s.buildRetainedReviewCore(t.Context(), retainedReviewFixtureOwner(t, s, id), retainedReviewInitialSelected(source), nil, func(ctx context.Context, reply inputPreparationReply, record string) (retainedPreparedInput, error) {
				prepared++
				return retainedReviewFixturePrepare(ctx, reply, record)
			}, func(root *os.Root) (retainedReviewFile, error) {
				created++
				return newAnonymousRetainedReviewFile(root)
			})
			if failure == "generated valid review" {
				if err != nil || d == nil || prepared != 2 || created != 1 {
					t.Fatal("structurally current generated review refused", err, prepared, created)
				}
				d.abort()
			} else {
				if d != nil {
					d.abort()
					t.Fatal("incomplete selection admitted")
				}
				if err == nil || prepared != 0 || created != 0 {
					t.Fatal("selection preflight crossed effects", err, prepared, created)
				}
				w := httptest.NewRecorder()
				retainedReviewInitialRefusal(w, err)
				if w.Code != 422 {
					t.Fatal("preflight status changed", w.Code, w.Body.String())
				}
			}
			if repo.reads.Load() != 1 || repo.gets.Load() != 1 || repo.directGets.Load() != 0 || repo.closes.Load() != 1 {
				t.Fatal("preflight guard not acquired/closed exactly once")
			}
			retainedReviewAssertNoReservations(t, s)
		})
	}
}

// I5: new core reservation ordering and busy admission. The unchanged
// TestRetainedReviewBuildHoldsSourceAndSealsExactBytes exercises this same core
// via the compatibility adapter with real CAS, descriptor seal and blocked final
// network byte, proving source exclusion through seal and release before I/O.
func TestRetainedReviewInitialReserveBeforeSourceAndBusy(t *testing.T) {
	s, id, source := retainedReviewFixture(t)
	repo := retainedReviewInitialTracked(s)
	owner := retainedReviewFixtureOwner(t, s, id)
	lane, err := s.retainedReviews.reserve(owner, t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var release sync.Once
	releaseLane := func() { release.Do(func() { s.retainedReviews.abort(lane); s.retainedReviews.release(lane) }) }
	defer releaseLane()
	d, err := s.buildRetainedReviewCore(t.Context(), owner, retainedReviewInitialSelected(source), nil, retainedReviewFixturePrepare, newAnonymousRetainedReviewFile)
	if d != nil {
		d.abort()
		t.Fatal("busy core returned delivery")
	}
	if !errors.Is(err, errRetainedReviewBusy) || repo.reads.Load() != 0 {
		t.Fatal("busy reserve touched source", err)
	}
	releaseLane()
	realRepo, ok := repo.inputPreparationRepository.(fileInputPreparationRepository)
	if !ok {
		t.Fatal("expected actual file repository")
	}
	assertHeld := func() {
		t.Helper()
		root, err := realRepo.open(false)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		lock, err := root.Open(".")
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		if err := lockPrivateData(lock, true); !lockHeld(err) {
			t.Fatal("source guard not held through preparation/seal", err)
		}
	}
	reads := 0
	d, err = s.buildRetainedReviewCore(t.Context(), owner, retainedReviewInitialSelected(source), nil, func(ctx context.Context, reply inputPreparationReply, record string) (retainedPreparedInput, error) {
		assertHeld()
		return retainedReviewFixturePrepare(ctx, reply, record)
	}, func(root *os.Root) (retainedReviewFile, error) {
		file, err := newAnonymousRetainedReviewFile(root)
		if err != nil {
			return nil, err
		}
		return &retainedReviewFaultFile{retainedReviewFile: file, onSync: assertHeld, onRead: func() { reads++; assertHeld() }}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.abort()
	if repo.reads.Load() != 1 || repo.gets.Load() != 1 || repo.closes.Load() != 1 || reads < 2 {
		t.Fatal("reservation/held seal sequence changed")
	}
	// The file instrumentation is finished before network delivery: its guard
	// assertion applies to seal, while delivery deliberately has no source lock.
	file := d.view.file.(*retainedReviewFaultFile)
	file.onRead = nil
	w := &retainedReviewBlockingResponse{ResponseRecorder: httptest.NewRecorder(), lastByte: true, entered: make(chan struct{}), release: make(chan struct{})}
	var network sync.Once
	defer network.Do(func() { close(w.release) })
	result := make(chan error, 1)
	go func() { result <- d.deliver(w) }()
	retainedReviewMoveWait(t, w.entered, "new core delivery did not reach final byte")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	current, err := realRepo.Get(ctx, source.PreparationID)
	if err != nil {
		t.Fatal("network held source read guard", err)
	}
	current.Document.Name = "Changed during initial delivery"
	updated, err := realRepo.CompareAndSwap(ctx, current.Document, current.SHA256)
	if err != nil || updated.SHA256 == source.Revision {
		t.Fatal("network delivery retained source exclusion", err)
	}
	if d.view.source.Revision != source.Revision {
		t.Fatal("frozen source changed after seal")
	}
	network.Do(func() { close(w.release) })
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("initial delivery did not finish")
	}
}

// I6: lifecycle hooks added at the new parse -> reserve and reserve -> read
// boundaries. Existing session/move/delivery tests continue through the same
// core; these fixtures add no timing sleeps or alternate production owners.
// Home confirmation refuses during actual HTTP parsing; its successful queue
// follows request quiescence. Direct core read/seal seams permit real queueing.
type retainedReviewInitialEndingBody struct {
	io.ReadCloser
	end  func()
	once sync.Once
}

func (b *retainedReviewInitialEndingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.once.Do(b.end)
	}
	return n, err
}

func retainedReviewInitialHomeParseLifecycle(t *testing.T, s *Server, id string, source retainedSelectionWireSource) {
	t.Helper()
	owner := retainedReviewFixtureOwner(t, s, id)
	prior := retainedReviewMoveReadable(t, s, id, source)
	file := prior.view.file
	plan, err := s.planDeskHome(filepath.Join(t.TempDir(), "moved-initial-home"))
	if err != nil || len(plan.Blockers) != 0 {
		t.Fatal("home move fixture unavailable", err, plan.Blockers)
	}
	request := projectMoveRequest{Destination: plan.Destination, Token: plan.Token}
	repo := retainedReviewInitialTracked(s)
	retainedReviewInitialRegister(s)
	r := retainedReviewInitialRequest(id, source, `{"recordIds":["row-1"]}`)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	r = r.WithContext(ctx)
	confirmed := false
	r.Body = &retainedReviewInitialEndingBody{ReadCloser: r.Body, end: func() {
		confirmed = true
		// serveProject owns location.use through the complete HTTP request.
		// Confirmation must refuse here without retiring any retained view.
		err := s.queueHomeMove(t.Context(), request)
		if err == nil || err.Error() != "Finish active work in every Desk before moving Desk home." {
			t.Fatal("active parsed request did not exclude home confirmation", err)
		}
		if s.homeMove.queued.Load() || s.projectHasMoved() || !owner.live() {
			t.Fatal("busy home confirmation retired request owner")
		}
		if _, err := os.Stat(homeJournalPath(s.configDir)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("busy home confirmation wrote move journal", err)
		}
		s.retainedReviews.mu.Lock()
		closed := s.retainedReviews.closed
		s.retainedReviews.mu.Unlock()
		if closed {
			t.Fatal("busy home confirmation closed installation admission")
		}
		// Cancellation, separately from the refused home move, ends this valid
		// initial request before reserve/source/B3 and lets HTTP custody drain.
		cancel()
	}}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if !confirmed || !errors.Is(ctx.Err(), context.Canceled) || repo.reads.Load() != 0 || repo.gets.Load() != 0 || repo.directGets.Load() != 0 || w.Header().Get("Content-Type") == retainedSelectionMediaType {
		t.Fatal("canceled parsed request crossed source/effect boundary", confirmed, ctx.Err(), w.Code, w.Body.String())
	}
	// The refused confirmation and canceled initial request leave the already
	// readable view usable. No canceled request is mistaken for home retirement.
	lease := retainedReviewMoveRead(t, s, id, prior)
	lease.close(true)
	if _, err := file.Stat(); err != nil {
		t.Fatal("busy home confirmation closed existing descriptor", err)
	}
	// ServeHTTP has returned and released location.use. Only a real successful
	// confirmation may now save the authoritative journal and retire the view.
	if err := s.queueHomeMove(t.Context(), request); err != nil {
		t.Fatal("quiescent home confirmation was not queued", err)
	}
	if !s.homeMove.queued.Load() || !s.projectHasMoved() || owner.live() {
		t.Fatal("confirmed home queue did not retire initial owner")
	}
	s.retainedReviews.mu.Lock()
	closed := s.retainedReviews.closed
	s.retainedReviews.mu.Unlock()
	if !closed {
		t.Fatal("confirmed home queue left installation admission open")
	}
	tx, err := readHomeTransaction(homeJournalPath(s.configDir))
	if err != nil || tx == nil || tx.Complete || tx.Destination != plan.Destination {
		t.Fatal("confirmed home queue lacks pending authoritative journal", err)
	}
	if _, err := os.Stat(plan.Destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("home queue moved files before shutdown", err)
	}
	retainedReviewAssertUnavailable(t, s, id, prior.view)
	retainedReviewMoveWait(t, prior.view.done, "confirmed home queue leaked prior view")
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("confirmed home queue leaked anonymous descriptor", err)
	}
	d, err := s.buildRetainedReviewCore(t.Context(), owner, retainedReviewInitialSelected(source), nil, retainedReviewFixturePrepare, newAnonymousRetainedReviewFile)
	if d != nil {
		d.abort()
		t.Fatal("confirmed home queue resurrected initial delivery")
	}
	if !errors.Is(err, errRetainedReviewUnavailable) || repo.reads.Load() != 0 || repo.gets.Load() != 0 || repo.directGets.Load() != 0 {
		t.Fatal("confirmed home queue admitted new source work", err)
	}
	retainedReviewAssertNoReservations(t, s)
}

func TestRetainedReviewInitialLifecycleAdmissionAndCleanup(t *testing.T) {
	for _, ending := range []string{"session", "project", "home"} {
		for _, boundary := range []string{"parse", "read", "seal"} {
			t.Run(ending+"/"+boundary, func(t *testing.T) {
				var s *Server
				var id string
				var source retainedSelectionWireSource
				if ending == "home" {
					s, _ = retainedReviewMoveHome(t)
					id, source = retainedReviewMoveSource(t, s)
					if boundary == "parse" {
						retainedReviewInitialHomeParseLifecycle(t, s, id, source)
						return
					}
				} else {
					s, id, source = retainedReviewFixture(t)
				}
				owner := retainedReviewFixtureOwner(t, s, id)
				var retire func()
				switch ending {
				case "session":
					retire = func() { s.sessions.revoke(id) }
				case "project":
					retire = func() { s.markProjectMoved(filepath.Join(t.TempDir(), "moved-initial-project")) }
				case "home":
					plan, err := s.planDeskHome(filepath.Join(t.TempDir(), "moved-initial-home"))
					if err != nil || len(plan.Blockers) != 0 {
						t.Fatal("home move fixture unavailable", err, plan.Blockers)
					}
					retire = func() {
						if err := s.queueHomeMove(t.Context(), projectMoveRequest{Destination: plan.Destination, Token: plan.Token}); err != nil {
							t.Fatal(err)
						}
					}
				}
				repo := retainedReviewInitialTracked(s)
				if boundary == "parse" {
					retainedReviewInitialRegister(s)
					r := retainedReviewInitialRequest(id, source, `{"recordIds":["row-1"]}`)
					r.Body = &retainedReviewInitialEndingBody{ReadCloser: r.Body, end: retire}
					w := httptest.NewRecorder()
					s.ServeHTTP(w, r)
					// A revoked session can cancel the real request before a status
					// is writable. No status claim substitutes for zero source work.
					if repo.reads.Load() != 0 || repo.gets.Load() != 0 || w.Header().Get("Content-Type") == retainedSelectionMediaType {
						t.Fatal("retired parsed request crossed source/effect boundary", w.Code, w.Body.String())
					}
				} else {
					if boundary == "read" {
						repo.onRead = func(context.Context) error { retire(); return nil }
					}
					prepared, created := 0, 0
					var file retainedReviewFile
					d, err := s.buildRetainedReviewCore(t.Context(), owner, retainedReviewInitialSelected(source), nil, func(ctx context.Context, saved inputPreparationReply, record string) (retainedPreparedInput, error) {
						prepared++
						return retainedReviewFixturePrepare(ctx, saved, record)
					}, func(root *os.Root) (retainedReviewFile, error) {
						created++
						actual, err := newAnonymousRetainedReviewFile(root)
						if err != nil {
							return nil, err
						}
						file = actual
						return &retainedReviewFaultFile{retainedReviewFile: actual, onSync: retire}, nil
					})
					if d != nil {
						d.abort()
						t.Fatal("retired builder resurrected delivery")
					}
					if err == nil {
						t.Fatal("retired builder returned success")
					}
					if boundary == "read" && (prepared != 0 || created != 0) {
						t.Fatal("retired read crossed preparation/file effects", prepared, created)
					}
					if boundary == "seal" && (prepared != 1 || created != 1) {
						t.Fatal("seal fixture missed retirement boundary", prepared, created)
					}
					if file != nil {
						if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
							t.Fatal("retirement leaked anonymous descriptor", err)
						}
					}
				}
				retainedReviewAssertNoReservations(t, s)
			})
		}
	}
}
func TestRetainedReviewInitialOwnerBindingAndGuards(t *testing.T) {
	s, id, source := retainedReviewFixture(t)
	retainedReviewInitialRegister(s)
	repo := retainedReviewInitialTracked(s)
	for _, credential := range []string{"", testToken} {
		r := retainedReviewInitialRequest(credential, source, `{"recordIds":["row-1"]}`)
		if credential == "" {
			r.Header.Del("Authorization")
		}
		body := &retainedReviewInitialBody{raw: `{"recordIds":["row-1"]}`}
		r.Body = body
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 401 || body.reads != 0 {
			t.Fatal("non-session request read body", w.Code, body.reads)
		}
	}
	r := retainedReviewInitialRequest(id, source, `{"recordIds":["row-1"]}`)
	r.Header.Set("Origin", "https://foreign.invalid")
	body := &retainedReviewInitialBody{raw: `{"recordIds":["row-1"]}`}
	r.Body = body
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 || body.reads != 0 {
		t.Fatal("foreign-origin request read body", w.Code, body.reads)
	}
	foreign, foreignID, _ := retainedReviewFixture(t)
	owner := retainedReviewFixtureOwner(t, s, id)
	other := retainedReviewFixtureOwner(t, foreign, foreignID)
	for _, field := range []string{"server", "project", "sessions", "MAC", "lifetime", "whole foreign owner"} {
		bad := owner
		switch field {
		case "server":
			bad.server = other.server
		case "project":
			bad.project = other.project
		case "sessions":
			bad.sessions = other.sessions
		case "MAC":
			bad.mac = ""
		case "lifetime":
			bad.lifetime = nil
		case "whole foreign owner":
			bad = other
		}
		d, err := s.buildRetainedReviewCore(t.Context(), bad, retainedReviewInitialSelected(source), nil, retainedReviewFixturePrepare, newAnonymousRetainedReviewFile)
		if d != nil {
			d.abort()
			t.Fatal("foreign owner admitted", field)
		}
		if !errors.Is(err, errRetainedReviewUnavailable) {
			t.Fatal("owner binding refused with wrong category", field, err)
		}
	}
	if repo.reads.Load() != 0 || repo.directGets.Load() != 0 {
		t.Fatal("foreign request or owner read source")
	}
	retainedReviewAssertNoReservations(t, s)
}

func TestRetainedReviewInitialFixedWrappedRefusals(t *testing.T) {
	private := errors.New("sensitive provider prose /private/path https://secret.invalid stack")
	for _, tc := range []struct {
		name   string
		cause  error
		status int
		code   string
	}{
		{"manager busy", errRetainedReviewBusy, 503, "review-busy"},
		{"repository busy", errInputPreparationBusy, 503, "review-busy"},
		{"stale", errRetainedReviewStale, 409, CodeStale},
		{"unsupported", retainedContractFailure("capability-unsupported"), 422, "capability-unsupported"},
		{"malformed", retainedContractFailure("input-malformed"), 422, "input-malformed"},
		{"over bound", retainedContractFailure("input-over-bound"), 422, "input-over-bound"},
		{"preflight", errRetainedReviewPreflight, 422, CodeBadRequest},
		{"resource", errRetainedReviewResource, 503, "review-resource"},
		{"close after contract refusal", errors.Join(errRetainedReviewResource, retainedContractFailure("input-malformed")), 503, "review-resource"},
		{"expired", errRetainedReviewUnavailable, 410, "review-unavailable"},
		{"unknown", private, 503, "review-unavailable"},
		{"unknown contract", retainedContractFailure("unrecognized-private-code"), 503, "review-unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := fmt.Errorf("private wrapper: %w", errors.Join(tc.cause, private))
			w := httptest.NewRecorder()
			retainedReviewInitialRefusal(w, err)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"`+tc.code+`"`) {
				t.Fatal("fixed initial refusal changed", w.Code, w.Body.String())
			}
			for _, secret := range []string{"sensitive", "private", "secret.invalid", "unrecognized-private-code"} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatal("raw failure escaped", w.Body.String())
				}
			}
			if code := retainedContractCode(tc.cause); code != "" && retainedContractCode(err) != code {
				t.Fatal("typed contract cause erased")
			}
		})
	}
}

// I7: selected Accept never activates the occupied real public /prepare route.
// An ordinary unqualified recipe retains its existing JSON refusal through
// that same mux; selected Accept cannot change its admission or transport.
func TestRetainedReviewInitialOrdinaryPublicGuard(t *testing.T) {
	s, id, source := retainedReviewFixture(t)
	s.cfg.JpackBin = filepath.Join(t.TempDir(), "unavailable-test-runtime")
	for _, selected := range []bool{true, false} {
		if !selected {
			saved, err := s.preparationRepository().Get(t.Context(), source.PreparationID)
			if err != nil {
				t.Fatal(err)
			}
			saved.Document.Recipe.Qualification = nil
			saved, err = s.preparationRepository().CompareAndSwap(t.Context(), saved.Document, saved.SHA256)
			if err != nil {
				t.Fatal(err)
			}
			source.Revision = saved.SHA256
		}
		r := retainedReviewInitialRequest(id, source, `{"recordIds":["row-1"]}`)
		r.URL.Path = "/api/input-preparations/" + source.PreparationID + "/prepare"
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if selected {
			if w.Code != 422 || !strings.Contains(w.Body.String(), `"`+CodeBadRequest+`"`) {
				t.Fatal("selected recipe entered ordinary public preparation", w.Code, w.Body.String())
			}
		} else if w.Code != 422 || !strings.Contains(w.Body.String(), "Choose a supplied record and a saved pack dialect.") {
			t.Fatal("ordinary public preparation changed", w.Code, w.Body.String())
		}
		if w.Header().Get("Content-Type") != "application/json" || strings.HasPrefix(w.Body.String(), retainedSelectionMagic) {
			t.Fatal("Accept opted public handler into stream", w.Header(), w.Body.String())
		}
		retainedReviewAssertNoReservations(t, s)
	}
}

// Well-formed initial requests reach only the private builder's own held source
// admission. These refusals need no fake preparation hook on the HTTP handler.
func TestRetainedReviewInitialHTTPSourceRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		code   string
	}{
		{"stale", 409, CodeStale}, {"ordinary", 422, "capability-unsupported"},
		{"missing row", 422, CodeBadRequest}, {"missing collection", 410, "review-unavailable"},
		{"repository busy", 503, "review-busy"}, {"repository unknown", 503, "review-unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, id, source := retainedReviewFixture(t)
			retainedReviewInitialRegister(s)
			if tc.name == "ordinary" {
				saved, err := s.preparationRepository().Get(t.Context(), source.PreparationID)
				if err != nil {
					t.Fatal(err)
				}
				saved.Document.Recipe.Qualification = nil
				saved, err = s.preparationRepository().CompareAndSwap(t.Context(), saved.Document, saved.SHA256)
				if err != nil {
					t.Fatal(err)
				}
				source.Revision = saved.SHA256
			}
			repo := retainedReviewInitialTracked(s)
			if tc.name == "repository busy" {
				repo.onRead = func(context.Context) error {
					return errors.Join(errInputPreparationBusy, errors.New("sensitive /private/path"))
				}
			}
			if tc.name == "repository unknown" {
				repo.onRead = func(context.Context) error { return errors.New("sensitive provider /private/path") }
			}
			if tc.name == "stale" {
				source.Revision = strings.Repeat("0", 64)
			}
			if tc.name == "missing collection" {
				source.PreparationID = "prep_" + strings.Repeat("0", 32)
			}
			raw := `{"recordIds":["row-1"]}`
			if tc.name == "missing row" {
				raw = `{"recordIds":["missing"]}`
			}
			r := retainedReviewInitialRequest(id, source, raw)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"`+tc.code+`"`) || strings.Contains(w.Body.String(), "sensitive") || strings.Contains(w.Body.String(), "private") {
				t.Fatal("initial source refusal changed or leaked cause", w.Code, w.Body.String())
			}
			if repo.reads.Load() != 1 || repo.directGets.Load() != 0 {
				t.Fatal("initial source did not use one reserved Read")
			}
			if tc.name != "repository busy" && tc.name != "repository unknown" && (repo.gets.Load() != 1 || repo.closes.Load() != 1) {
				t.Fatal("initial held source not closed exactly once")
			}
			retainedReviewAssertNoReservations(t, s)
		})
	}
}
