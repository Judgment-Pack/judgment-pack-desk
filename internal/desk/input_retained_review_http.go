package desk

// These handlers are intentionally unregistered. Public selected /prepare
// remains closed until source, browser and resource qualification completes.
import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"sync"
	"time"
)

func retainedReviewHeaders(w http.ResponseWriter, media string, length int64) {
	w.Header().Set("Content-Type", media)
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.Header().Set("Cache-Control", "no-store, no-transform")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Accept-Ranges", "none")
	w.Header().Del("Content-Encoding")
	w.Header().Del("Content-Range")
}

// Copy only bounded chunks from the held descriptor. SetWriteDeadline is best
// effort on unsupported writers; cancellation still closes admission and the
// handler releases its lease when the writer returns. No enforced timeout is
// claimed for a writer that does not support deadlines.
func retainedReviewCopy(ctx context.Context, w http.ResponseWriter, file io.ReaderAt, offset, length int64) error {
	controller := http.NewResponseController(w)
	var deadlines sync.Mutex
	extend := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		until := time.Now().Add(relayIdle)
		if deadline, ok := ctx.Deadline(); ok && deadline.Before(until) {
			until = deadline
		}
		deadlines.Lock()
		// Cancellation owns the final nonzero deadline. An earlier admitted
		// refresh must not replace its immediate deadline after waiting here.
		if err := ctx.Err(); err != nil {
			deadlines.Unlock()
			return err
		}
		err := controller.SetWriteDeadline(until)
		deadlines.Unlock()
		if errors.Is(err, http.ErrNotSupported) {
			return nil
		}
		return err
	}
	if err := extend(); err != nil {
		return err
	}
	canceled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		deadlines.Lock()
		_ = controller.SetWriteDeadline(time.Now())
		deadlines.Unlock()
		close(canceled)
	})
	defer func() {
		if !stop() {
			<-canceled
		}
		deadlines.Lock()
		_ = controller.SetWriteDeadline(time.Time{})
		deadlines.Unlock()
	}()
	reader := io.NewSectionReader(file, offset, length)
	var buffer [retainedReviewChunk]byte
	var copied int64
	for copied < length {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := min(int64(len(buffer)), length-copied)
		if _, err := io.ReadFull(reader, buffer[:int(n)]); err != nil {
			return err
		}
		if err := extend(); err != nil {
			return err
		}
		written, err := w.Write(buffer[:int(n)])
		if err != nil {
			return err
		}
		if written != int(n) {
			return io.ErrShortWrite
		}
		copied += n
	}
	if copied != length {
		return io.ErrShortWrite
	}
	if err := extend(); err != nil {
		return err
	}
	if err := controller.Flush(); err != nil {
		return err
	}
	return ctx.Err()
}

// A delivery retains its builder reference until the full copy, final flush
// and live-state recheck succeed. A successful file seal alone never admits a
// row request. Callers must not write any success bytes before this method.
func (d *retainedReviewDelivery) deliver(w http.ResponseWriter) error {
	defer d.abort()
	m, v := d.manager, d.view
	m.mu.Lock()
	ready := v.state == retainedReviewDelivering && v.owner.live() && d.request.Err() == nil && v.ctx.Err() == nil
	m.mu.Unlock()
	if !ready {
		return errRetainedReviewUnavailable
	}
	ctx, cancel := context.WithCancel(d.request)
	stop := context.AfterFunc(v.ctx, cancel)
	defer stop()
	defer cancel()
	if v.ctx.Err() != nil {
		cancel()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	retainedReviewHeaders(w, retainedSelectionMediaType, v.length)
	if err := retainedReviewCopy(ctx, w, v.file, 0, v.length); err != nil {
		return err
	}
	return d.commit()
}

func retainedReviewRefusal(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errRetainedReviewNotReady):
		writeJSONCoded(w, http.StatusConflict, "review-not-ready", errRetainedReviewNotReady.Error())
	case errors.Is(err, errRetainedReviewBusy):
		writeJSONCoded(w, http.StatusServiceUnavailable, "review-busy", errRetainedReviewBusy.Error())
	default:
		writeJSONCoded(w, http.StatusGone, "review-unavailable", errRetainedReviewUnavailable.Error())
	}
}

func (s *Server) retainedReviewRequest(w http.ResponseWriter, r *http.Request) (retainedReviewOwner, bool) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) || !s.guardPrivateWorkspace(w, r) {
		return retainedReviewOwner{}, false
	}
	w.Header().Set("Cache-Control", "no-store, no-transform")
	if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.URL.ForceQuery || r.Header.Get("Range") != "" || !inputPreparationID.MatchString(r.PathValue("preparation")) {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Invalid retained review request.")
		return retainedReviewOwner{}, false
	}
	owner, err := s.retainedReviewOwner(r.Context(), bearerOf(r))
	if err != nil || s.retainedReviews == nil {
		retainedReviewRefusal(w, errRetainedReviewUnavailable)
		return retainedReviewOwner{}, false
	}
	return owner, true
}

// Only the initial selected request owns these strict negotiation rules. Row,
// close and ordinary handlers retain their existing acceptance contracts.
type retainedReviewCountedBody struct {
	io.ReadCloser
	bytes int64
}

func (b *retainedReviewCountedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.bytes += int64(n)
	return n, err
}

func retainedReviewInitialSelection(w http.ResponseWriter, r *http.Request) (retainedReviewSelection, bool) {
	var selection retainedReviewSelection
	bad := func(status int) (retainedReviewSelection, bool) {
		writeJSONCoded(w, status, CodeBadRequest, "Invalid retained selection request.")
		return selection, false
	}
	// Even an empty Range/Content-Range field refuses. The shared guard also
	// checks method, path and query; these presence checks are initial-only.
	if _, present := r.Header["Range"]; present {
		return bad(http.StatusBadRequest)
	}
	if _, present := r.Header["Content-Range"]; present {
		return bad(http.StatusBadRequest)
	}
	accept := r.Header.Values("Accept")
	if len(accept) != 1 || accept[0] != retainedSelectionMediaType {
		return bad(http.StatusNotAcceptable)
	}
	revision := r.Header.Values("If-Match")
	if len(revision) != 1 || !retainedSelectionSHA.MatchString(revision[0]) {
		return bad(http.StatusBadRequest)
	}
	contentType := r.Header.Values("Content-Type")
	if len(contentType) != 1 {
		return bad(http.StatusUnsupportedMediaType)
	}
	media, _, err := mime.ParseMediaType(contentType[0])
	if err != nil || media != "application/json" {
		return bad(http.StatusUnsupportedMediaType)
	}
	encoding := r.Header.Values("Content-Encoding")
	if len(encoding) > 1 || len(encoding) == 1 && encoding[0] != "identity" {
		return bad(http.StatusUnsupportedMediaType)
	}
	var request struct {
		RecordIDs []string `json:"recordIds"`
	}
	counted := &retainedReviewCountedBody{ReadCloser: r.Body}
	copy := *r
	copy.Body = counted
	// inputRequest is the single parser and 2 MiB limit owner. Successful
	// parsing counted every raw byte through EOF, including JSON whitespace.
	if err := inputRequest(&copy, &request); err != nil {
		if r.Context().Err() != nil {
			return selection, false
		}
		var oversized overLimit
		if errors.As(err, &oversized) {
			writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge, "The retained selection request is too large.")
			return selection, false
		}
		return bad(http.StatusBadRequest)
	}
	selection = retainedReviewSelection{r.PathValue("preparation"), revision[0], request.RecordIDs, counted.bytes}
	selection, err = retainedReviewSelected(selection)
	if err != nil {
		return bad(http.StatusBadRequest)
	}
	return selection, true
}

// Initial refusal classification preserves wrapped causes for internal callers
// while emitting only fixed host-authored messages. Unknown component failures
// are unavailable, with no assertion that earlier acquisition had no effects.
func retainedReviewInitialRefusal(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errRetainedReviewResource):
		writeJSONCoded(w, http.StatusServiceUnavailable, "review-resource", errRetainedReviewResource.Error())
	case errors.Is(err, errRetainedReviewBusy), errors.Is(err, errInputPreparationBusy):
		writeJSONCoded(w, http.StatusServiceUnavailable, "review-busy", errRetainedReviewBusy.Error())
	case errors.Is(err, errRetainedReviewStale):
		writeJSONCoded(w, http.StatusConflict, CodeStale, errRetainedReviewStale.Error())
	case retainedContractCode(err) == "capability-unsupported", retainedContractCode(err) == "input-malformed", retainedContractCode(err) == "input-over-bound":
		code := retainedContractCode(err)
		writeJSONCoded(w, http.StatusUnprocessableEntity, code, (&retainedContractError{Code: code}).Error())
	case errors.Is(err, errRetainedReviewPreflight):
		writeJSONCoded(w, http.StatusUnprocessableEntity, CodeBadRequest, errRetainedReviewPreflight.Error())
	case errors.Is(err, errRetainedReviewUnavailable):
		writeJSONCoded(w, http.StatusGone, "review-unavailable", errRetainedReviewUnavailable.Error())
	default:
		writeJSONCoded(w, http.StatusServiceUnavailable, "review-unavailable", errRetainedReviewUnavailable.Error())
	}
}

// Intentionally unregistered. Actual B3/generated authority and public/browser
// resource qualification remain independent prerequisites for activation.
func (s *Server) handleRetainedPreparedSelection(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.retainedReviewRequest(w, r)
	if !ok {
		return
	}
	selection, ok := retainedReviewInitialSelection(w, r)
	if !ok || r.Context().Err() != nil {
		return
	}
	delivery, err := s.buildRetainedReview(r.Context(), owner, selection)
	if err != nil {
		if r.Context().Err() == nil {
			if !owner.live() {
				retainedReviewInitialRefusal(w, errRetainedReviewUnavailable)
			} else {
				retainedReviewInitialRefusal(w, err)
			}
		}
		return
	}
	// Never append JSON or replace status after any part of delivery starts.
	_ = delivery.deliver(w)
}

func (s *Server) handleRetainedPreparedRow(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.retainedReviewRequest(w, r)
	if !ok {
		return
	}
	var request struct {
		ViewID      string `json:"viewId"`
		IndexSHA256 string `json:"indexSha256"`
		Index       *int   `json:"index"`
		ID          string `json:"id"`
	}
	if inputRequest(r, &request) != nil || !retainedSelectionViewID.MatchString(request.ViewID) || !retainedSelectionSHA.MatchString(request.IndexSHA256) || r.Header.Get("If-Match") != request.IndexSHA256 || request.Index == nil || *request.Index < 0 || request.ID == "" {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Invalid retained row reference.")
		return
	}
	lease, err := s.retainedReviews.read(r.Context(), owner, r.PathValue("preparation"), request.ViewID, request.IndexSHA256, *request.Index, request.ID)
	if err != nil {
		retainedReviewRefusal(w, err)
		return
	}
	complete := false
	defer func() { lease.close(complete) }()
	if lease.ctx.Err() != nil {
		return
	}
	retainedReviewHeaders(w, "application/json", lease.row.InputBytes)
	if err := retainedReviewCopy(lease.ctx, w, lease.view.file, lease.row.Offset, lease.row.InputBytes); err != nil {
		return
	}
	// A canceled/closed view cannot refresh its idle clock or be acknowledged
	// as a completed read; the stream itself may already have reached its peer.
	complete = lease.ctx.Err() == nil
}
func (s *Server) handleRetainedPreparedViewClose(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.retainedReviewRequest(w, r)
	if !ok {
		return
	}
	var request struct {
		ViewID      string `json:"viewId"`
		IndexSHA256 string `json:"indexSha256"`
	}
	if inputRequest(r, &request) != nil || !retainedSelectionViewID.MatchString(request.ViewID) || !retainedSelectionSHA.MatchString(request.IndexSHA256) || r.Header.Get("If-Match") != request.IndexSHA256 {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Invalid retained view reference.")
		return
	}
	s.retainedReviews.closeOwned(owner, r.PathValue("preparation"), request.ViewID, request.IndexSHA256)
	w.WriteHeader(http.StatusNoContent)
}
