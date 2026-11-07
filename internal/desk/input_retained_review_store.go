package desk

// Inactive private review storage. Source admission and the B3 preparation
// remain host operations; a view is an immutable snapshot, never execution authority.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
	"sync"
	"time"
	"unicode/utf8"
)

const retainedReviewChunk = 64 << 10

var (
	errRetainedReviewBusy        = errors.New("Retained review resources are busy. Try again explicitly.")
	errRetainedReviewUnavailable = errors.New("This retained review is unavailable. Start another review explicitly.")
	errRetainedReviewStale       = errors.New("Dialects changed. Reload and review the selection again.")
	errRetainedReviewPreflight   = errors.New("The saved selection is not available for this review.")
	errRetainedReviewResource    = errors.New("Retained review storage could not be completed. Start another review explicitly.")
	errRetainedReviewNotReady    = errors.New("This retained review has not completed delivery.")
)

type retainedReviewFile interface {
	io.Writer
	io.ReaderAt
	Stat() (os.FileInfo, error)
	Sync() error
	Close() error
}

type retainedReviewOwner struct {
	server   *Server
	project  *ProjectRoot
	sessions *sessionStore
	mac      string
	lifetime context.Context
}

// The session reference comes from the actual request session store, not its
// subject, a caller-selected identifier, the launch secret or a public view ID.
func (s *Server) retainedReviewOwner(rctx context.Context, id string) (retainedReviewOwner, error) {
	if s.sessions == nil || rctx.Err() != nil {
		return retainedReviewOwner{}, errRetainedReviewUnavailable
	}
	held, ok := s.sessions.lookup(id)
	if !ok || !s.signInSessionAllowed(held) || held.ctx == nil || held.ctx.Err() != nil {
		return retainedReviewOwner{}, errRetainedReviewUnavailable
	}
	return retainedReviewOwner{s, s.project, s.sessions, s.sessions.handle(id), held.ctx}, nil
}
func (o retainedReviewOwner) same(other retainedReviewOwner) bool {
	return o.server == other.server && o.project == other.project && o.sessions == other.sessions && o.mac == other.mac && o.lifetime == other.lifetime
}
func (o retainedReviewOwner) live() bool {
	if o.server == nil || o.server.project != o.project || o.lifetime == nil || o.lifetime.Err() != nil {
		return false
	}
	s := o.server
	return s.assistant != nil && s.assistant.usable() && s.assistant.root != nil && !s.location.moving.Load() && !s.projectHasMoved() && !s.deskOwner().homeMove.queued.Load()
}

type retainedReviewState uint8

const (
	retainedReviewBuilding retainedReviewState = iota
	retainedReviewDelivering
	retainedReviewReadable
	retainedReviewClosed
)

type retainedReviewView struct {
	id                       string
	owner                    retainedReviewOwner
	ctx                      context.Context
	cancel                   context.CancelFunc
	stopSession, stopRequest func() bool
	state                    retainedReviewState
	file                     retainedReviewFile
	source                   retainedSelectionWireSource
	rows                     []retainedSelectionWireDescriptor
	length                   int64
	indexSHA                 string
	refs                     int // The builder/delivery and each active reader own a reference.
	reading                  bool
	readCancel               context.CancelFunc
	idleUntil                time.Time
	timer                    *time.Timer
	done                     chan struct{} // closed once the descriptor and reservation are released
}

// One manager is shared by parent and child desks. Reservations count as views,
// and a canceled build retains its lane and slot until its I/O lease releases.
// This prevents a cancellation storm from opening unbounded files or builders.
type retainedReviewManager struct {
	mu      sync.Mutex
	views   map[string]*retainedReviewView
	builder *retainedReviewView
	closed  bool
}

func newRetainedReviewManager() *retainedReviewManager {
	return &retainedReviewManager{views: make(map[string]*retainedReviewView)}
}
func (m *retainedReviewManager) reserve(owner retainedReviewOwner, request context.Context) (*retainedReviewView, error) {
	if !owner.live() || request.Err() != nil {
		return nil, errRetainedReviewUnavailable
	}
	token, err := NewToken()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || owner.server.retainedReviewRetired || !owner.live() || request.Err() != nil {
		return nil, errRetainedReviewUnavailable
	}
	if m.builder != nil || len(m.views) >= maxSessions {
		return nil, errRetainedReviewBusy
	}
	id := "rv_" + token
	if m.views[id] != nil {
		return nil, errRetainedReviewBusy
	}
	ctx, cancel := context.WithCancel(owner.lifetime)
	v := &retainedReviewView{id: id, owner: owner, ctx: ctx, cancel: cancel, state: retainedReviewBuilding, refs: 1, done: make(chan struct{})}
	m.views[id], m.builder = v, v
	v.stopSession = context.AfterFunc(owner.lifetime, func() { m.abort(v) })
	v.stopRequest = context.AfterFunc(request, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if v.state != retainedReviewReadable {
			m.retireLocked(v)
		}
	})
	return v, nil
}
func (m *retainedReviewManager) retireLocked(v *retainedReviewView) {
	if v.state == retainedReviewClosed {
		return
	}
	v.state = retainedReviewClosed
	v.cancel()
	if v.readCancel != nil {
		v.readCancel()
	}
	if v.stopSession != nil {
		v.stopSession()
	}
	if v.stopRequest != nil {
		v.stopRequest()
	}
	if v.timer != nil {
		v.timer.Stop()
	}
	m.disposeLocked(v)
}
func (m *retainedReviewManager) disposeLocked(v *retainedReviewView) {
	if v.state != retainedReviewClosed || v.refs != 0 || m.views[v.id] != v {
		return
	}
	if v.file != nil {
		_ = v.file.Close()
		v.file = nil
	}
	delete(m.views, v.id)
	close(v.done)
	if m.builder == v {
		m.builder = nil
	}
}
func (m *retainedReviewManager) abort(v *retainedReviewView) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retireLocked(v)
}
func (m *retainedReviewManager) release(v *retainedReviewView) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v.refs--
	m.disposeLocked(v)
}
func (m *retainedReviewManager) closeServer(s *Server, installation bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.retainedReviewRetired = true
	if installation {
		m.closed = true
	}
	for _, v := range m.views {
		if installation || v.owner.server == s {
			m.retireLocked(v)
		}
	}
}
func (m *retainedReviewManager) expire(v *retainedReviewView) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v.state != retainedReviewReadable {
		return
	}
	if remaining := time.Until(v.idleUntil); remaining > 0 {
		v.timer.Reset(remaining)
		return
	}
	m.retireLocked(v)
}
func (m *retainedReviewManager) refreshLocked(v *retainedReviewView) {
	v.idleUntil = time.Now().Add(signInIdleLifetime)
	if v.timer == nil {
		v.timer = time.AfterFunc(signInIdleLifetime, func() { m.expire(v) })
	} else {
		v.timer.Reset(signInIdleLifetime)
	}
}

// No private byte is written before the fresh name is unlinked and descriptor
// identity is checked. Unsupported open/unlink semantics refuse without fallback.
func newAnonymousRetainedReviewFile(root *os.Root) (retainedReviewFile, error) {
	return newAnonymousRetainedReviewFileUsing(root, retainedReviewFileOps{})
}

// Private per-call filesystem seam for admission fault fixtures. The production
// entry always uses these exact os.Root operations, with no mutable hooks.
type retainedReviewFileOps struct {
	stage  func(*os.Root) (retainedReviewFile, string, error)
	lstat  func(*os.Root, string) (os.FileInfo, error)
	remove func(*os.Root, string) error
}

func newAnonymousRetainedReviewFileUsing(root *os.Root, ops retainedReviewFileOps) (retainedReviewFile, error) {
	if root == nil {
		return nil, errRetainedReviewUnavailable
	}
	if ops.stage == nil {
		ops.stage = func(root *os.Root) (retainedReviewFile, string, error) { return newDataStage(root) }
	}
	if ops.lstat == nil {
		ops.lstat = func(root *os.Root, name string) (os.FileInfo, error) { return root.Lstat(name) }
	}
	if ops.remove == nil {
		ops.remove = func(root *os.Root, name string) error { return root.Remove(name) }
	}
	file, name, err := ops.stage(root)
	if err != nil {
		return nil, err
	}
	linked := true
	fail := func(cause error) (retainedReviewFile, error) {
		// Cleanup is limited to this still-empty created inode; never delete a
		// replacement, and never revisit the name after successful unlink.
		var held os.FileInfo
		if linked {
			held, _ = file.Stat()
		}
		closeErr := file.Close()
		var removeErr error
		if linked && held != nil {
			named, nameErr := ops.lstat(root, name)
			if nameErr == nil && os.SameFile(held, named) {
				removeErr = ops.remove(root, name)
			}
		}
		return nil, errors.Join(cause, closeErr, removeErr)
	}
	before, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	if !before.Mode().IsRegular() || before.Size() != 0 || before.Mode().Perm() != custodyFileMode {
		return fail(errRetainedReviewUnavailable)
	}
	named, err := ops.lstat(root, name)
	if err != nil || !os.SameFile(before, named) {
		return fail(errRetainedReviewUnavailable)
	}
	if err := ops.remove(root, name); err != nil {
		return fail(err)
	}
	linked = false
	if _, err := ops.lstat(root, name); !errors.Is(err, os.ErrNotExist) {
		return fail(errRetainedReviewUnavailable)
	}
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || after.Size() != 0 || !os.SameFile(before, after) {
		return fail(errRetainedReviewUnavailable)
	}
	return file, nil
}

type retainedReviewSink struct {
	ctx  context.Context
	file retainedReviewFile
	hash hash.Hash
}

func (w *retainedReviewSink) Write(raw []byte) (int, error) {
	total := 0
	for len(raw) > 0 {
		if err := w.ctx.Err(); err != nil {
			return total, err
		}
		n := min(len(raw), retainedReviewChunk)
		written, err := w.file.Write(raw[:n])
		if written < 0 || written > n {
			return total, errors.Join(errRetainedReviewResource, io.ErrShortWrite)
		}
		if written > 0 {
			_, _ = w.hash.Write(raw[:written])
			total += written
		}
		if err != nil {
			return total, errors.Join(errRetainedReviewResource, err)
		}
		if written != n {
			return total, errors.Join(errRetainedReviewResource, io.ErrShortWrite)
		}
		raw = raw[n:]
	}
	return total, nil
}
func retainedReviewHashSection(ctx context.Context, file io.ReaderAt, offset, length int64) (string, error) {
	h := sha256.New()
	reader := io.NewSectionReader(file, offset, length)
	var buffer [retainedReviewChunk]byte
	for remaining := length; remaining > 0; {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n := int64(len(buffer))
		if remaining < n {
			n = remaining
		}
		if _, err := io.ReadFull(reader, buffer[:int(n)]); err != nil {
			return "", err
		}
		_, _ = h.Write(buffer[:int(n)])
		remaining -= n
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// The request owns selection and byte accounting only. Saved source metadata
// is derived under the one repository read guard after manager reservation.
type retainedReviewSelection struct {
	PreparationID string
	Revision      string
	RecordIDs     []string
	RequestBytes  int64
}

func retainedReviewSelected(selection retainedReviewSelection) (retainedReviewSelection, error) {
	if !inputPreparationID.MatchString(selection.PreparationID) || !retainedSelectionSHA.MatchString(selection.Revision) || len(selection.RecordIDs) < 1 || len(selection.RecordIDs) > inputRecordLimit || selection.RequestBytes < 1 || selection.RequestBytes > retainedSelectionRequestBytes {
		return selection, errRetainedSelectionWire
	}
	seen := make(map[string]bool, len(selection.RecordIDs))
	var total int64
	for _, id := range selection.RecordIDs {
		if id == "" || !utf8.ValidString(id) || seen[id] {
			return selection, errRetainedSelectionWire
		}
		seen[id] = true
		var err error
		total, err = retainedSelectionAdd(total, int64(len(id)))
		if err != nil || total > selection.RequestBytes {
			return selection, errRetainedSelectionWire
		}
	}
	selection.RecordIDs = append([]string(nil), selection.RecordIDs...)
	return selection, nil
}

// Pure full-selection preflight. No callback, repository or external owner can
// replace the held saved source, and no row is prepared until all rows pass.
func retainedReviewSourceFromSaved(saved inputPreparationReply, selection retainedReviewSelection) (retainedSelectionWireSource, error) {
	var source retainedSelectionWireSource
	selection, err := retainedReviewSelected(selection)
	if err != nil {
		return source, err
	}
	if saved.Document.ID != selection.PreparationID {
		return source, errRetainedReviewPreflight
	}
	if saved.SHA256 != selection.Revision {
		return source, errRetainedReviewStale
	}
	q := saved.Document.Recipe.Qualification
	if q == nil || q.ConversionContract == nil || *q.ConversionContract != inputRetainedContract() {
		return source, retainedContractFailure("capability-unsupported")
	}
	origin := ""
	for _, id := range selection.RecordIDs {
		record, err := retainedSavedPreparationRecord(saved, id)
		if err != nil {
			return source, errors.Join(errRetainedReviewPreflight, err)
		}
		if origin == "" {
			origin = record.Class
		} else if origin != record.Class {
			return source, errRetainedReviewPreflight
		}
		if record.Class == "generated" {
			if err := readyInputReview(saved.Document, record); err != nil {
				return source, errors.Join(errRetainedReviewPreflight, err)
			}
		}
	}
	contract := q.ConversionContract
	source = retainedSelectionWireSource{
		PreparationID: saved.Document.ID, Revision: saved.SHA256, PackDigest: q.Digest,
		ConversionContract: retainedSelectionWireContract{contract.Contract, retainedSelectionWireSpec{contract.Spec.Version, contract.Spec.Revision, contract.Spec.SchemaSHA256, contract.Spec.CorpusSHA256}, contract.Profile},
		Origin:             origin, RecordIDs: selection.RecordIDs, RequestBytes: selection.RequestBytes,
	}
	return retainedSelectionSource(source)
}

// The production method uses the existing actual B3 owner. It has no public
// registration and no caller-supplied Input/path/producer-approval parameter.
func (s *Server) buildRetainedReview(ctx context.Context, owner retainedReviewOwner, selection retainedReviewSelection) (*retainedReviewDelivery, error) {
	return s.buildRetainedReviewCore(ctx, owner, selection, nil, func(ctx context.Context, saved inputPreparationReply, id string) (retainedPreparedInput, error) {
		return s.prepareRetainedSavedInput(ctx, saved, id, nil)
	}, newAnonymousRetainedReviewFile)
}

type retainedReviewPrepare func(context.Context, inputPreparationReply, string) (retainedPreparedInput, error)

// Compatibility seam for handwritten lifecycle fixtures. Expected metadata is
// an assertion against independently derived source, never source authority.
func (s *Server) buildRetainedReviewUsing(ctx context.Context, sessionID string, source retainedSelectionWireSource, prepare retainedReviewPrepare, create func(*os.Root) (retainedReviewFile, error)) (*retainedReviewDelivery, error) {
	source, err := retainedSelectionSource(source)
	if err != nil {
		return nil, err
	}
	owner, err := s.retainedReviewOwner(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	selection := retainedReviewSelection{source.PreparationID, source.Revision, source.RecordIDs, source.RequestBytes}
	return s.buildRetainedReviewCore(ctx, owner, selection, &source, prepare, create)
}

// The core owns reserve -> Read/Get -> complete preflight -> B3/spool/seal ->
// Close. The exact source guard survives seal and is released before delivery.
func (s *Server) buildRetainedReviewCore(ctx context.Context, owner retainedReviewOwner, selection retainedReviewSelection, expected *retainedSelectionWireSource, prepare retainedReviewPrepare, create func(*os.Root) (retainedReviewFile, error)) (_ *retainedReviewDelivery, err error) {
	if s.retainedReviews == nil || prepare == nil || create == nil || owner.server != s || owner.project != s.project || owner.sessions != s.sessions || owner.sessions == nil || owner.mac == "" || owner.lifetime == nil || !owner.live() {
		return nil, errRetainedReviewUnavailable
	}
	selection, err = retainedReviewSelected(selection)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil && !owner.live() {
			err = errors.Join(errRetainedReviewUnavailable, err)
		}
	}()
	m := s.retainedReviews
	v, err := m.reserve(owner, ctx) // Reserve before acquiring the source guard.
	if err != nil {
		return nil, err
	}
	owned := true
	defer func() {
		if owned {
			m.abort(v)
			m.release(v)
		}
	}()
	work, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(v.ctx, cancel)
	defer stop()
	defer cancel()
	if v.ctx.Err() != nil {
		cancel()
	}
	held, err := s.preparationRepository().Read(work)
	if err != nil {
		if errors.Is(err, errInputPreparationStoreMissing) || errors.Is(err, os.ErrNotExist) {
			err = errors.Join(errRetainedReviewUnavailable, err)
		}
		return nil, err
	}
	// A failed Close prevents delivery, including on the successful seal path.
	heldOpen := true
	defer func() {
		if heldOpen {
			if closeErr := held.Close(); closeErr != nil {
				err = errors.Join(err, errRetainedReviewResource, closeErr)
			}
		}
	}()
	saved, err := held.Get(selection.PreparationID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = errors.Join(errRetainedReviewUnavailable, err)
		}
		return nil, err
	}
	source, err := retainedReviewSourceFromSaved(saved, selection)
	if err != nil {
		return nil, err
	}
	if expected != nil && (expected.PackDigest != source.PackDigest || expected.ConversionContract != source.ConversionContract || expected.Origin != source.Origin) {
		return nil, errRetainedReviewPreflight
	}
	if err := work.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	live := v.state == retainedReviewBuilding && owner.live() && ctx.Err() == nil && v.ctx.Err() == nil
	m.mu.Unlock()
	if !live {
		return nil, errRetainedReviewUnavailable
	}
	file, err := create(s.assistant.root)
	if err != nil {
		return nil, errors.Join(errRetainedReviewResource, err)
	}
	m.mu.Lock()
	v.file = file
	closed := v.state == retainedReviewClosed || !owner.live() || work.Err() != nil
	m.mu.Unlock()
	if closed {
		return nil, errRetainedReviewUnavailable
	}
	sink := &retainedReviewSink{work, file, sha256.New()}
	writer, err := newRetainedSelectionWireWriter(sink, source, v.id)
	if err != nil {
		return nil, err
	}
	rows := make([]retainedSelectionWireDescriptor, 0, len(source.RecordIDs))
	for _, id := range source.RecordIDs {
		if err := work.Err(); err != nil {
			return nil, err
		}
		if v.ctx.Err() != nil || !owner.live() {
			return nil, errRetainedReviewUnavailable
		}
		// An iteration owns only one complete B3 result. No aggregate payload
		// array or mapping/reply retention survives the iteration.
		result, err := prepare(work, saved, id)
		if err != nil {
			return nil, err
		}
		descriptor, err := writer.Add(id, result.InputJSON)
		result = retainedPreparedInput{}
		if err != nil {
			return nil, err
		}
		rows = append(rows, descriptor)
	}
	length, err := writer.Finish()
	if err != nil {
		return nil, err
	}
	if err := file.Sync(); err != nil {
		return nil, errors.Join(errRetainedReviewResource, err)
	}
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() != length {
		return nil, errors.Join(errRetainedReviewResource, err)
	}
	digest, err := retainedReviewHashSection(work, file, 0, length)
	if err != nil || digest != hex.EncodeToString(sink.hash.Sum(nil)) {
		return nil, errors.Join(errRetainedReviewResource, err)
	}
	var previousEnd int64
	for _, row := range rows {
		end, addErr := retainedSelectionAdd(row.Offset, row.InputBytes)
		if addErr != nil || row.Offset <= previousEnd || end > length {
			return nil, errRetainedReviewResource
		}
		digest, err := retainedReviewHashSection(work, file, row.Offset, row.InputBytes)
		if err != nil || digest != row.InputSHA256 {
			return nil, errors.Join(errRetainedReviewResource, err)
		}
		previousEnd = end
	}
	if err := work.Err(); err != nil {
		return nil, err
	}
	closeErr := held.Close()
	heldOpen = false
	if closeErr != nil {
		return nil, errors.Join(errRetainedReviewResource, closeErr)
	}
	m.mu.Lock()
	if v.state != retainedReviewBuilding || !owner.live() || work.Err() != nil || ctx.Err() != nil {
		m.mu.Unlock()
		return nil, errRetainedReviewUnavailable
	}
	v.source, v.rows, v.length = source, rows, length
	v.indexSHA = hex.EncodeToString(writer.index.Sum(nil))
	v.state = retainedReviewDelivering
	m.builder = nil
	m.mu.Unlock()
	owned = false // The delivery owns the existing reference until completion.
	return &retainedReviewDelivery{manager: m, view: v, request: ctx}, nil
}

type retainedReviewDelivery struct {
	manager *retainedReviewManager
	view    *retainedReviewView
	request context.Context
	once    sync.Once
}

func (d *retainedReviewDelivery) abort() {
	d.once.Do(func() { d.manager.abort(d.view); d.manager.release(d.view) })
}
func (d *retainedReviewDelivery) commit() error {
	m, v := d.manager, d.view
	m.mu.Lock()
	if v.state != retainedReviewDelivering || !v.owner.live() || d.request.Err() != nil || v.ctx.Err() != nil {
		m.mu.Unlock()
		d.abort()
		return errRetainedReviewUnavailable
	}
	v.state = retainedReviewReadable
	if v.stopRequest != nil {
		v.stopRequest()
	}
	m.refreshLocked(v)
	m.mu.Unlock()
	d.once.Do(func() { m.release(v) })
	return nil
}

type retainedReviewRead struct {
	manager *retainedReviewManager
	view    *retainedReviewView
	row     retainedSelectionWireDescriptor
	ctx     context.Context
	cancel  context.CancelFunc
	stop    func() bool
	once    sync.Once
}

func (m *retainedReviewManager) read(ctx context.Context, owner retainedReviewOwner, preparation, viewID, indexSHA string, index int, id string) (*retainedReviewRead, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.views[viewID]
	if v == nil || !v.owner.same(owner) || v.source.PreparationID != preparation || v.indexSHA != indexSHA || v.state == retainedReviewClosed || !owner.live() || ctx.Err() != nil || v.ctx.Err() != nil {
		return nil, errRetainedReviewUnavailable
	}
	if v.state != retainedReviewReadable {
		return nil, errRetainedReviewNotReady
	}
	if !time.Now().Before(v.idleUntil) {
		m.retireLocked(v)
		return nil, errRetainedReviewUnavailable
	}
	if index < 0 || index >= len(v.rows) || v.rows[index].ID != id {
		return nil, errRetainedReviewUnavailable
	}
	if v.reading {
		return nil, errRetainedReviewBusy
	}
	readCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(v.ctx, cancel)
	v.refs++
	v.reading = true
	v.readCancel = cancel
	m.refreshLocked(v) // Only an admitted readable row access refreshes this view.
	return &retainedReviewRead{manager: m, view: v, row: v.rows[index], ctx: readCtx, cancel: cancel, stop: stop}, nil
}
func (r *retainedReviewRead) close(complete bool) {
	r.once.Do(func() {
		r.stop()
		r.cancel()
		m, v := r.manager, r.view
		m.mu.Lock()
		defer m.mu.Unlock()
		v.reading = false
		v.readCancel = nil
		v.refs--
		if complete && v.state == retainedReviewReadable && v.owner.live() {
			m.refreshLocked(v)
		}
		m.disposeLocked(v)
	})
}
func (m *retainedReviewManager) closeOwned(owner retainedReviewOwner, preparation, viewID, indexSHA string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.views[viewID]
	// Identical completion for unknown, foreign, and already-closed references.
	// No tombstones or unbounded post-close inventory is required.
	if v != nil && v.owner.same(owner) && v.source.PreparationID == preparation && v.indexSHA == indexSHA {
		m.retireLocked(v)
	}
}
