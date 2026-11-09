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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Registration exists only in tests. The production mux keeps selected
// preparation and these private row/close routes unregistered.
func retainedReviewRegisterFixture(s *Server) {
	s.mux.HandleFunc("POST /api/input-preparations/{preparation}/prepared-row", s.handleRetainedPreparedRow)
	s.mux.HandleFunc("POST /api/input-preparations/{preparation}/prepared-view-close", s.handleRetainedPreparedViewClose)
}
func retainedReviewHTTPRequest(t *testing.T, s *Server, credential, preparation, route, raw, digest string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/input-preparations/"+preparation+"/"+route, strings.NewReader(raw))
	if credential != "" {
		r.Header.Set("Authorization", "Bearer "+credential)
	}
	r.Header.Set("If-Match", digest)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func retainedReviewRowBody(v *retainedReviewView) string {
	raw, _ := json.Marshal(map[string]any{"viewId": v.id, "indexSha256": v.indexSHA, "index": 0, "id": "row-1"})
	return string(raw)
}
func retainedReviewCloseBody(v *retainedReviewView) string {
	raw, _ := json.Marshal(map[string]any{"viewId": v.id, "indexSha256": v.indexSHA})
	return string(raw)
}

type retainedReviewBlockingResponse struct {
	*httptest.ResponseRecorder
	lastByte   bool
	entered    chan struct{}
	release    chan struct{}
	once       sync.Once
	flushError error
}

func (w *retainedReviewBlockingResponse) Write(p []byte) (int, error) {
	if !w.lastByte {
		return w.ResponseRecorder.Write(p)
	}
	// Deliberately withhold the last byte of the one-chunk handwritten stream.
	// The callback signals after every earlier byte has reached the recorder.
	n, err := w.ResponseRecorder.Write(p[:len(p)-1])
	if err != nil {
		return n, err
	}
	w.once.Do(func() { close(w.entered) })
	<-w.release
	last, err := w.ResponseRecorder.Write(p[len(p)-1:])
	return n + last, err
}
func (w *retainedReviewBlockingResponse) FlushError() error {
	if !w.lastByte {
		w.once.Do(func() { close(w.entered) })
		<-w.release
	}
	if w.flushError != nil {
		return w.flushError
	}
	w.ResponseRecorder.Flush()
	return nil
}

func TestRetainedReviewHTTPNoRowsBeforeLastByteAndFlush(t *testing.T) {
	for _, boundary := range []string{"last byte", "final flush"} {
		t.Run(boundary, func(t *testing.T) {
			s, id, source := retainedReviewFixture(t)
			retainedReviewRegisterFixture(s)
			d := retainedReviewFixtureBuild(t, s, id, source)
			w := &retainedReviewBlockingResponse{ResponseRecorder: httptest.NewRecorder(), lastByte: boundary == "last byte", entered: make(chan struct{}), release: make(chan struct{})}
			var release sync.Once
			defer release.Do(func() { close(w.release) })
			result := make(chan error, 1)
			go func() { result <- d.deliver(w) }()
			select {
			case <-w.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("delivery did not reach handwritten boundary")
			}
			early := retainedReviewHTTPRequest(t, s, id, source.PreparationID, "prepared-row", retainedReviewRowBody(d.view), d.view.indexSHA)
			if early.Code != http.StatusConflict || !strings.Contains(early.Body.String(), `"review-not-ready"`) || strings.Contains(early.Body.String(), "9007199254740993") {
				t.Fatal("row payload escaped before full delivery", early.Code, early.Body.String())
			}
			if boundary == "last byte" && int64(w.Body.Len()) != d.view.length-1 {
				t.Fatal("fixture did not withhold final byte")
			}
			if boundary == "final flush" && int64(w.Body.Len()) != d.view.length {
				t.Fatal("fixture blocked before full body")
			}
			release.Do(func() { close(w.release) })
			if err := <-result; err != nil {
				t.Fatal(err)
			}
			ready := retainedReviewHTTPRequest(t, s, id, source.PreparationID, "prepared-row", retainedReviewRowBody(d.view), d.view.indexSHA)
			if ready.Code != http.StatusOK || ready.Body.String() != string(retainedReviewTestInput) {
				t.Fatal("exact row unavailable after final delivery", ready.Code, ready.Body.String())
			}
			if ready.Header().Get("Content-Length") != "74" || ready.Header().Get("Content-Type") != "application/json" || ready.Header().Get("Cache-Control") != "no-store, no-transform" || ready.Header().Get("X-Content-Type-Options") != "nosniff" || ready.Header().Get("Accept-Ranges") != "none" || ready.Header().Get("Content-Encoding") != "" {
				t.Fatal("row transport headers changed", ready.Header())
			}
		})
	}
}

type retainedReviewFailedResponse struct {
	*httptest.ResponseRecorder
	short bool
}

func (w retainedReviewFailedResponse) Write(p []byte) (int, error) {
	if w.short {
		return len(p) - 1, nil
	}
	return 0, io.ErrClosedPipe
}

func TestRetainedReviewHTTPFailedDeliveryRetiresView(t *testing.T) {
	for _, fault := range []string{"write", "short write", "flush", "request cancel", "session revoke", "close during flush"} {
		t.Run(fault, func(t *testing.T) {
			s, id, source := retainedReviewFixture(t)
			retainedReviewRegisterFixture(s)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			d, err := s.buildRetainedReviewUsing(ctx, id, source, retainedReviewFixturePrepare, newAnonymousRetainedReviewFile)
			if err != nil {
				t.Fatal(err)
			}
			defer d.abort()
			if fault == "write" || fault == "short write" {
				err = d.deliver(retainedReviewFailedResponse{httptest.NewRecorder(), fault == "short write"})
			} else {
				w := &retainedReviewBlockingResponse{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), release: make(chan struct{})}
				var release sync.Once
				defer release.Do(func() { close(w.release) })
				if fault == "flush" {
					w.flushError = io.ErrClosedPipe
				}
				result := make(chan error, 1)
				go func() { result <- d.deliver(w) }()
				select {
				case <-w.entered:
				case <-time.After(5 * time.Second):
					t.Fatal("fixture did not reach final flush")
				}
				switch fault {
				case "request cancel":
					cancel()
				case "session revoke":
					s.sessions.revoke(id)
				case "close during flush":
					close := retainedReviewHTTPRequest(t, s, id, source.PreparationID, "prepared-view-close", retainedReviewCloseBody(d.view), d.view.indexSHA)
					if close.Code != http.StatusNoContent {
						t.Fatal("close during flush failed", close.Code)
					}
				}
				release.Do(func() { close(w.release) })
				err = <-result
			}
			if err == nil {
				t.Fatal("incomplete delivery acknowledged")
			}
			select {
			case <-d.view.done:
			case <-time.After(5 * time.Second):
				t.Fatal("incomplete delivery leaked descriptor")
			}
			if d.view.file != nil {
				t.Fatal("incomplete view retained file")
			}
			if fault != "session revoke" {
				retainedReviewAssertUnavailable(t, s, id, d.view)
			}
		})
	}
}

func TestRetainedReviewHTTPGuardAndClosedReferences(t *testing.T) {
	s, id, source := retainedReviewFixture(t)
	retainedReviewRegisterFixture(s)
	d := retainedReviewFixtureBuild(t, s, id, source)
	if err := d.deliver(httptest.NewRecorder()); err != nil {
		t.Fatal(err)
	}
	row, closeBody := retainedReviewRowBody(d.view), retainedReviewCloseBody(d.view)
	for _, credential := range []string{"", testToken} {
		w := retainedReviewHTTPRequest(t, s, credential, source.PreparationID, "prepared-row", row, d.view.indexSHA)
		if w.Code != http.StatusUnauthorized {
			t.Fatal("row admitted non-session credential", w.Code)
		}
		w = retainedReviewHTTPRequest(t, s, credential, source.PreparationID, "prepared-view-close", closeBody, d.view.indexSHA)
		if w.Code != http.StatusUnauthorized {
			t.Fatal("close admitted non-session credential", w.Code)
		}
	}
	foreign, err := s.sessions.create("foreign", nil)
	if err != nil {
		t.Fatal(err)
	}
	w := retainedReviewHTTPRequest(t, s, foreign, source.PreparationID, "prepared-row", row, d.view.indexSHA)
	if w.Code != http.StatusGone || strings.Contains(w.Body.String(), "9007199254740993") {
		t.Fatal("foreign row reference leaked", w.Code)
	}
	w = retainedReviewHTTPRequest(t, s, foreign, source.PreparationID, "prepared-view-close", closeBody, d.view.indexSHA)
	if w.Code != http.StatusNoContent {
		t.Fatal("foreign close disclosed existence", w.Code)
	}
	w = retainedReviewHTTPRequest(t, s, id, source.PreparationID, "prepared-row", row, d.view.indexSHA)
	if w.Code != http.StatusOK {
		t.Fatal("foreign close deleted owned view", w.Code)
	}
	for _, raw := range []string{
		`{}`, `null`, strings.Replace(row, `"index":0`, `"index":null`, 1), strings.Replace(row, `"index":0`, `"index":1.5`, 1),
		strings.Replace(row, `"index":0`, `"index":-1`, 1), row[:len(row)-1] + `,"path":"elsewhere"}`, row[:len(row)-1] + `,"index":0}`,
	} {
		w := retainedReviewHTTPRequest(t, s, id, source.PreparationID, "prepared-row", raw, d.view.indexSHA)
		if w.Code != http.StatusBadRequest {
			t.Fatal("open or malformed row request accepted", raw, w.Code)
		}
	}
	for _, edit := range []string{"digest", "id", "index", "preparation"} {
		body, preparation, digest := row, source.PreparationID, d.view.indexSHA
		switch edit {
		case "digest":
			digest = strings.Repeat("0", 64)
		case "id":
			body = strings.Replace(row, "row-1", "other", 1)
		case "index":
			body = strings.Replace(row, `"index":0`, `"index":1`, 1)
		case "preparation":
			preparation = "prep_" + strings.Repeat("0", 32)
		}
		w := retainedReviewHTTPRequest(t, s, id, preparation, "prepared-row", body, digest)
		if w.Code != http.StatusGone && w.Code != http.StatusBadRequest {
			t.Fatal("mismatched identity admitted", edit, w.Code)
		}
	}
	for i := 0; i < 2; i++ {
		w := retainedReviewHTTPRequest(t, s, id, source.PreparationID, "prepared-view-close", closeBody, d.view.indexSHA)
		if w.Code != http.StatusNoContent {
			t.Fatal("owned close not idempotent", w.Code)
		}
	}
	w = retainedReviewHTTPRequest(t, s, id, source.PreparationID, "prepared-row", row, d.view.indexSHA)
	if w.Code != http.StatusGone {
		t.Fatal("late read admitted", w.Code)
	}
}

type retainedReviewUnsupportedDeadline struct{ *httptest.ResponseRecorder }

func (w retainedReviewUnsupportedDeadline) SetWriteDeadline(time.Time) error {
	return http.ErrNotSupported
}
func TestRetainedReviewHTTPUnsupportedDeadlineAndCanceledCopy(t *testing.T) {
	s, id, source := retainedReviewFixture(t)
	d := retainedReviewFixtureBuild(t, s, id, source)
	w := retainedReviewUnsupportedDeadline{httptest.NewRecorder()}
	if err := d.deliver(w); err != nil {
		t.Fatal("unsupported deadline refused otherwise complete delivery", err)
	}
	if int64(w.Body.Len()) != d.view.length {
		t.Fatal("unsupported-deadline body incomplete")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	empty := httptest.NewRecorder()
	if err := retainedReviewCopy(ctx, empty, d.view.file, 0, d.view.length); !errors.Is(err, context.Canceled) || empty.Body.Len() != 0 {
		t.Fatal("canceled copy emitted bytes", err)
	}
}

// This writer implements real deadline cancellation for deterministic tests:
// blocked I/O returns only when the immediate cancellation deadline arrives.
// It also records any erroneous future deadline installed after cancellation.
type retainedReviewDeadlineResponse struct {
	*httptest.ResponseRecorder
	mu                                 sync.Mutex
	immediate                          chan struct{}
	entered                            chan struct{}
	once, enterOnce                    sync.Once
	blockWrite                         bool
	sawImmediate, futureAfterImmediate bool
	writes, flushes                    int
}

func (w *retainedReviewDeadlineResponse) SetWriteDeadline(until time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if until.IsZero() {
		return nil
	} // Cleanup reset is joined after cancellation.
	if until.After(time.Now()) {
		if w.sawImmediate {
			w.futureAfterImmediate = true
		}
		return nil
	}
	w.sawImmediate = true
	w.once.Do(func() { close(w.immediate) })
	return nil
}
func (w *retainedReviewDeadlineResponse) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.writes++
	w.mu.Unlock()
	if w.blockWrite {
		w.enterOnce.Do(func() { close(w.entered) })
		<-w.immediate
		return 0, os.ErrDeadlineExceeded
	}
	return w.ResponseRecorder.Write(p)
}
func (w *retainedReviewDeadlineResponse) FlushError() error {
	w.mu.Lock()
	w.flushes++
	w.mu.Unlock()
	w.enterOnce.Do(func() { close(w.entered) })
	<-w.immediate
	return os.ErrDeadlineExceeded
}

// Pause a captured successful ctx.Err observation immediately after ReadAt.
// Cancellation can then install its deadline before that admitted refresh
// resumes. This reproduces the precise stale-observation ordering of RR-1.
type retainedReviewPausedErrContext struct {
	context.Context
	armed            atomic.Bool
	entered, release chan struct{}
}

func (c *retainedReviewPausedErrContext) Err() error {
	err := c.Context.Err()
	if c.armed.Swap(false) {
		close(c.entered)
		<-c.release
	}
	return err
}

type retainedReviewArmRead struct {
	io.ReaderAt
	ctx *retainedReviewPausedErrContext
}

func (r retainedReviewArmRead) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.ReaderAt.ReadAt(p, off)
	r.ctx.armed.Store(true)
	return n, err
}

func TestRetainedReviewHTTPCancellationDeadlineWinsAdmittedRefresh(t *testing.T) {
	base, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := &retainedReviewPausedErrContext{Context: base, entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(ctx.release) })
	w := &retainedReviewDeadlineResponse{ResponseRecorder: httptest.NewRecorder(), immediate: make(chan struct{}), entered: make(chan struct{}), blockWrite: true}
	result := make(chan error, 1)
	go func() {
		result <- retainedReviewCopy(ctx, w, retainedReviewArmRead{strings.NewReader("handwritten"), ctx}, 0, 11)
	}()
	select {
	case <-ctx.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not capture live context")
	}
	cancel()
	select {
	case <-w.immediate:
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not install immediate deadline")
	}
	release.Do(func() { close(ctx.release) })
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("admitted refresh ignored cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled refresh did not return")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.futureAfterImmediate || w.writes != 0 || w.flushes != 0 {
		t.Fatal("future deadline or I/O escaped after cancellation", w.futureAfterImmediate, w.writes, w.flushes)
	}
}

func TestRetainedReviewHTTPDeadlineCancelsBlockedWriteAndFlush(t *testing.T) {
	for _, boundary := range []string{"write", "final flush"} {
		for _, ending := range []string{"request", "session", "view"} {
			t.Run(boundary+"/"+ending, func(t *testing.T) {
				s, id, source := retainedReviewFixture(t)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				d, err := s.buildRetainedReviewUsing(ctx, id, source, retainedReviewFixturePrepare, newAnonymousRetainedReviewFile)
				if err != nil {
					t.Fatal(err)
				}
				defer d.abort()
				w := &retainedReviewDeadlineResponse{ResponseRecorder: httptest.NewRecorder(), immediate: make(chan struct{}), entered: make(chan struct{}), blockWrite: boundary == "write"}
				// Failure cleanup may release the fixture if an assertion stops
				// early; successful test paths rely only on the owner's deadline.
				defer w.SetWriteDeadline(time.Now())
				result := make(chan error, 1)
				go func() { result <- d.deliver(w) }()
				select {
				case <-w.entered:
				case <-time.After(5 * time.Second):
					t.Fatal("delivery did not block at deadline-capable writer")
				}
				switch ending {
				case "request":
					cancel()
				case "session":
					s.sessions.revoke(id)
				case "view":
					s.retainedReviews.closeOwned(retainedReviewFixtureOwner(t, s, id), source.PreparationID, d.view.id, d.view.indexSHA)
				}
				select {
				case err := <-result:
					if !errors.Is(err, os.ErrDeadlineExceeded) {
						t.Fatal("blocked I/O did not return its deadline error", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("owner cancellation did not release blocked I/O")
				}
				select {
				case <-d.view.done:
				case <-time.After(5 * time.Second):
					t.Fatal("canceled delivery retained descriptor")
				}
				w.mu.Lock()
				immediate, future, writes, flushes := w.sawImmediate, w.futureAfterImmediate, w.writes, w.flushes
				w.mu.Unlock()
				if !immediate || future || writes != 1 || boundary == "write" && flushes != 0 || boundary == "final flush" && flushes != 1 {
					t.Fatal("deadline/I/O ordering changed", immediate, future, writes, flushes)
				}
				retainedReviewAssertNoReservations(t, s)
			})
		}
	}
}
