package desk

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Handwritten store fixtures isolate lifecycle from actual B3 qualification.
// Their callback is never configured on the production Server.
var retainedReviewTestInput = []byte(`{"value":9007199254740993,"negativeZero":-0,"fraction":1.0,"exponent":1e3}`)

func retainedReviewFixture(t *testing.T) (*Server, string, retainedSelectionWireSource) {
	t.Helper()
	return retainedReviewFixtureRows(t, false)
}
func retainedReviewFixtureRows(t *testing.T, twoRows bool) (*Server, string, retainedSelectionWireSource) {
	t.Helper()
	s, _ := newTestServer(t, false)
	doc := repositoryDocument(71)
	ids := []string{"row-1"}
	if twoRows {
		second := doc.Records[0]
		second.ID = "row-2"
		doc.Records = append(doc.Records, second)
		ids = append(ids, second.ID)
	}
	repo := s.preparationRepository()
	if _, err := repo.CompareAndSwap(t.Context(), doc, ""); err != nil {
		t.Fatal(err)
	}
	// Attach qualification only to a trusted read carrying original cell
	// custody. A fresh qualified in-memory document must remain unencodable.
	saved, err := repo.Get(t.Context(), doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	doc = saved.Document
	doc.Recipe.Qualification = retainedB4Qualification(t)
	saved, err = repo.CompareAndSwap(t.Context(), doc, saved.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.sessions.create("review fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, id, retainedSelectionWireSource{PreparationID: doc.ID, Revision: saved.SHA256, PackDigest: doc.Recipe.Qualification.Digest, ConversionContract: retainedSelectionInstalledContract(), Origin: "asserted", RecordIDs: ids, RequestBytes: 64}
}
func retainedReviewFixturePrepare(_ context.Context, _ inputPreparationReply, _ string) (retainedPreparedInput, error) {
	return retainedPreparedInput{InputJSON: append([]byte(nil), retainedReviewTestInput...)}, nil
}
func retainedReviewFixtureBuild(t *testing.T, s *Server, id string, source retainedSelectionWireSource) *retainedReviewDelivery {
	t.Helper()
	d, err := s.buildRetainedReviewUsing(t.Context(), id, source, retainedReviewFixturePrepare, newAnonymousRetainedReviewFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.abort)
	return d
}
func retainedReviewFixtureOwner(t *testing.T, s *Server, id string) retainedReviewOwner {
	t.Helper()
	owner, err := s.retainedReviewOwner(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}
func retainedReviewAssertUnavailable(t *testing.T, s *Server, id string, v *retainedReviewView) {
	t.Helper()
	lease, err := s.retainedReviews.read(t.Context(), retainedReviewFixtureOwner(t, s, id), v.source.PreparationID, v.id, v.indexSHA, 0, "row-1")
	if lease != nil {
		lease.close(false)
		t.Fatal("unavailable view returned a reader")
	}
	if !errors.Is(err, errRetainedReviewUnavailable) {
		t.Fatal("wanted unavailable", err)
	}
}

type retainedReviewTrackingRepository struct {
	inputPreparationRepository
	closed    atomic.Bool
	failClose bool
}
type retainedReviewTrackingRead struct {
	inputPreparationRead
	owner *retainedReviewTrackingRepository
}

func (r *retainedReviewTrackingRepository) Read(ctx context.Context) (inputPreparationRead, error) {
	read, err := r.inputPreparationRepository.Read(ctx)
	if err != nil {
		return nil, err
	}
	r.closed.Store(false)
	return &retainedReviewTrackingRead{read, r}, nil
}
func (r *retainedReviewTrackingRead) Close() error {
	err := r.inputPreparationRead.Close()
	r.owner.closed.Store(true)
	if r.owner.failClose {
		err = errors.Join(err, io.ErrClosedPipe)
	}
	return err
}

type retainedReviewFaultFile struct {
	retainedReviewFile
	writeFault, syncFault, statFault, corrupt bool
	closed                                    atomic.Int64
	onSync, onRead                            func()
}

func (f *retainedReviewFaultFile) Write(p []byte) (int, error) {
	if f.writeFault {
		return 0, nil
	} // Successful short write must poison seal.
	return f.retainedReviewFile.Write(p)
}
func (f *retainedReviewFaultFile) Sync() error {
	if f.onSync != nil {
		f.onSync()
	}
	if f.syncFault {
		return os.ErrPermission
	}
	return f.retainedReviewFile.Sync()
}
func (f *retainedReviewFaultFile) Stat() (os.FileInfo, error) {
	if f.statFault {
		return nil, os.ErrPermission
	}
	return f.retainedReviewFile.Stat()
}
func (f *retainedReviewFaultFile) ReadAt(p []byte, off int64) (int, error) {
	if f.onRead != nil {
		f.onRead()
	}
	n, err := f.retainedReviewFile.ReadAt(p, off)
	if f.corrupt && n > 0 {
		p[0] ^= 1
	}
	return n, err
}
func (f *retainedReviewFaultFile) Close() error {
	f.closed.Add(1)
	return f.retainedReviewFile.Close()
}

func TestRetainedReviewAnonymousBeforePayload(t *testing.T) {
	s, _, _ := retainedReviewFixture(t)
	file, err := newAnonymousRetainedReviewFile(s.assistant.root)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	dir, err := s.assistant.root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".data-") {
			t.Fatal("named review stage survived before payload")
		}
	}
	if err := retainedSelectionWrite(file, retainedReviewTestInput); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(retainedReviewTestInput))
	if _, err := file.ReadAt(got, 0); err != nil || string(got) != string(retainedReviewTestInput) {
		t.Fatal("held anonymous descriptor unavailable", err)
	}
	if _, err := newAnonymousRetainedReviewFile(nil); err == nil {
		t.Fatal("missing custody accepted")
	}
}

type retainedReviewAdmissionFile struct {
	retainedReviewFile
	writes, closes, stats int
	afterStat             func() (os.FileInfo, error)
}

func (f *retainedReviewAdmissionFile) Write(p []byte) (int, error) {
	f.writes++
	return f.retainedReviewFile.Write(p)
}
func (f *retainedReviewAdmissionFile) Close() error {
	f.closes++
	return f.retainedReviewFile.Close()
}
func (f *retainedReviewAdmissionFile) Stat() (os.FileInfo, error) {
	f.stats++
	if f.stats == 2 && f.afterStat != nil {
		return f.afterStat()
	}
	return f.retainedReviewFile.Stat()
}

func retainedReviewAssertNoReservations(t *testing.T, s *Server) {
	t.Helper()
	s.retainedReviews.mu.Lock()
	defer s.retainedReviews.mu.Unlock()
	if len(s.retainedReviews.views) != 0 || s.retainedReviews.builder != nil {
		t.Fatal("fault leaked view or builder reservation")
	}
}

func TestRetainedReviewAnonymousAdmissionFaultsBeforePreparation(t *testing.T) {
	for _, fault := range []string{"unlink", "linked identity", "absence", "descriptor identity"} {
		t.Run(fault, func(t *testing.T) {
			s, id, source := retainedReviewFixture(t)
			repository := &retainedReviewTrackingRepository{inputPreparationRepository: s.preparationRepository()}
			s.inputPreparations = repository
			var file *retainedReviewAdmissionFile
			var stageName string
			other, err := os.CreateTemp(t.TempDir(), "other-inode-")
			if err != nil {
				t.Fatal(err)
			}
			otherInfo, statErr := other.Stat()
			closeErr := other.Close()
			if statErr != nil || closeErr != nil {
				t.Fatal(errors.Join(statErr, closeErr))
			}
			stats, removes := 0, 0
			ops := retainedReviewFileOps{
				stage: func(root *os.Root) (retainedReviewFile, string, error) {
					actual, name, err := newDataStage(root)
					if err != nil {
						return nil, "", err
					}
					stageName = name
					file = &retainedReviewAdmissionFile{retainedReviewFile: actual}
					if fault == "descriptor identity" {
						file.afterStat = func() (os.FileInfo, error) { return otherInfo, nil }
					}
					return file, name, nil
				},
				lstat: func(root *os.Root, name string) (os.FileInfo, error) {
					stats++
					if fault == "linked identity" && stats == 1 || fault == "absence" && stats == 2 {
						return otherInfo, nil
					}
					return root.Lstat(name)
				},
				remove: func(root *os.Root, name string) error {
					removes++
					if fault == "unlink" && removes == 1 {
						return os.ErrPermission
					}
					return root.Remove(name)
				},
			}
			calls := 0
			d, err := s.buildRetainedReviewUsing(t.Context(), id, source, func(ctx context.Context, saved inputPreparationReply, record string) (retainedPreparedInput, error) {
				calls++
				return retainedReviewFixturePrepare(ctx, saved, record)
			}, func(root *os.Root) (retainedReviewFile, error) {
				return newAnonymousRetainedReviewFileUsing(root, ops)
			})
			if d != nil {
				d.abort()
				t.Fatal("anonymous admission fault returned delivery")
			}
			if err == nil || calls != 0 || file == nil || file.writes != 0 || file.closes != 1 || !repository.closed.Load() {
				t.Fatal("anonymous failure crossed preparation or leaked descriptor", err, calls, file)
			}
			if _, err := file.retainedReviewFile.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatal("admission descriptor remains open", err)
			}
			if _, err := s.assistant.root.Lstat(stageName); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("owned empty stage was not cleaned", err)
			}
			retainedReviewAssertNoReservations(t, s)
		})
	}
}

// Err's second observation is the actual file repository's post-lock check.
// Pause there after acquisition so the real mutation can complete while network
// delivery is deliberately blocked, independent of scheduler timing.
type retainedReviewCASContext struct {
	context.Context
	checks            atomic.Int64
	acquired, release chan struct{}
}

func (c *retainedReviewCASContext) Err() error {
	err := c.Context.Err()
	if c.checks.Add(1) == 2 {
		close(c.acquired)
		<-c.release
	}
	return err
}

func TestRetainedReviewBuildHoldsSourceAndSealsExactBytes(t *testing.T) {
	s, id, source := retainedReviewFixtureRows(t, true)
	repo, ok := s.preparationRepository().(fileInputPreparationRepository)
	if !ok {
		t.Fatal("fixture must use the actual file repository")
	}
	candidate, err := repo.Get(t.Context(), source.PreparationID)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Document.Name = "Later edit"
	preparing, releasePreparation := make(chan struct{}), make(chan struct{})
	sealing, releaseSeal := make(chan struct{}), make(chan struct{})
	var prepareRelease, sealRelease sync.Once
	defer prepareRelease.Do(func() { close(releasePreparation) })
	defer sealRelease.Do(func() { close(releaseSeal) })
	type buildResult struct {
		delivery *retainedReviewDelivery
		err      error
	}
	built := make(chan buildResult, 1)
	go func() {
		calls := 0
		d, err := s.buildRetainedReviewUsing(t.Context(), id, source, func(ctx context.Context, saved inputPreparationReply, record string) (retainedPreparedInput, error) {
			calls++
			if calls == 1 {
				close(preparing)
				<-releasePreparation
			}
			return retainedReviewFixturePrepare(ctx, saved, record)
		}, func(root *os.Root) (retainedReviewFile, error) {
			actual, err := newAnonymousRetainedReviewFile(root)
			if err != nil {
				return nil, err
			}
			file := &retainedReviewFaultFile{retainedReviewFile: actual}
			reads := 0
			file.onRead = func() {
				reads++
				if reads == 3 { // Full stream and first descriptor already checked.
					close(sealing)
					<-releaseSeal
				}
			}
			return file, nil
		})
		built <- buildResult{d, err}
	}()
	select {
	case <-preparing:
	case <-time.After(5 * time.Second):
		t.Fatal("build did not enter preparation")
	}
	opened := make(chan struct{})
	competing := fileInputPreparationRepository{open: func(create bool) (*os.Root, error) {
		root, err := repo.open(create)
		if create && err == nil {
			close(opened)
		}
		return root, err
	}}
	casCtx := &retainedReviewCASContext{Context: t.Context(), acquired: make(chan struct{}), release: make(chan struct{})}
	var casRelease sync.Once
	defer casRelease.Do(func() { close(casCtx.release) })
	type casResult struct {
		saved inputPreparationReply
		err   error
	}
	changed := make(chan casResult, 1)
	go func() {
		saved, err := competing.CompareAndSwap(casCtx, candidate.Document, candidate.SHA256)
		changed <- casResult{saved, err}
	}()
	select {
	case <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("competing CAS did not enter actual repository")
	}
	assertBlocked := func() {
		t.Helper()
		root, err := repo.open(false)
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
			t.Fatal("source guard did not exclude actual repository CAS lock", err)
		}
		select {
		case <-casCtx.acquired:
			t.Fatal("competing CAS acquired during held source")
		default:
		}
	}
	assertBlocked()
	prepareRelease.Do(func() { close(releasePreparation) })
	select {
	case <-sealing:
	case <-time.After(5 * time.Second):
		t.Fatal("build did not reach final descriptor seal")
	}
	assertBlocked() // The same real guard still excludes CAS during seal readback.
	sealRelease.Do(func() { close(releaseSeal) })
	var d *retainedReviewDelivery
	select {
	case result := <-built:
		if result.err != nil {
			t.Fatal(result.err)
		}
		d = result.delivery
	case <-time.After(5 * time.Second):
		t.Fatal("sealed build did not release source")
	}
	defer d.abort()
	frozen, err := retainedReviewHashSection(t.Context(), d.view.file, 0, d.view.length)
	if err != nil {
		t.Fatal(err)
	}
	w := &retainedReviewBlockingResponse{ResponseRecorder: httptest.NewRecorder(), lastByte: true, entered: make(chan struct{}), release: make(chan struct{})}
	var networkRelease sync.Once
	defer networkRelease.Do(func() { close(w.release) })
	delivered := make(chan error, 1)
	go func() { delivered <- d.deliver(w) }()
	select {
	case <-w.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("delivery did not pause at final byte")
	}
	select {
	case <-casCtx.acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("CAS remained locked during delivery")
	}
	casRelease.Do(func() { close(casCtx.release) })
	select {
	case result := <-changed:
		if result.err != nil || result.saved.SHA256 == source.Revision {
			t.Fatal("competing CAS failed to change current source during paused delivery", result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("competing CAS did not complete during paused delivery")
	}
	current, err := repo.Get(t.Context(), source.PreparationID)
	if err != nil || current.SHA256 == source.Revision || current.Document.Name != "Later edit" {
		t.Fatal("real source mutation did not persist", err)
	}
	after, err := retainedReviewHashSection(t.Context(), d.view.file, 0, d.view.length)
	if err != nil || after != frozen || d.view.source.Revision != source.Revision {
		t.Fatal("sealed bytes or revision changed during competing CAS", err)
	}
	networkRelease.Do(func() { close(w.release) })
	select {
	case err := <-delivered:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("delivery did not finish")
	}
	_, rows, err := readRetainedSelectionWire(strings.NewReader(w.Body.String()), source, int64(w.Body.Len()), func(descriptor retainedSelectionWireDescriptor, raw []byte) error {
		if descriptor != d.view.rows[descriptor.Index] || string(raw) != string(retainedReviewTestInput) {
			t.Fatal("payload or descriptor changed")
		}
		return nil
	})
	if err != nil || len(rows) != 2 || int64(w.Body.Len()) != d.view.length {
		t.Fatal("two-row frozen stream did not survive delivery", err)
	}
	lease, err := s.retainedReviews.read(t.Context(), retainedReviewFixtureOwner(t, s, id), source.PreparationID, d.view.id, d.view.indexSHA, 0, "row-1")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.close(true)
	raw, err := io.ReadAll(io.NewSectionReader(d.view.file, lease.row.Offset, lease.row.InputBytes))
	if err != nil || string(raw) != string(retainedReviewTestInput) {
		t.Fatal("frozen row changed after actual source mutation", err)
	}
}

func TestRetainedReviewPreflightBeforeAnyPreparation(t *testing.T) {
	for _, failure := range []string{"revision", "pack", "origin", "missing record", "later missing record", "mixed origin", "duplicate requested ID", "duplicate saved ID", "ordinary", "missing custody", "generated review"} {
		t.Run(failure, func(t *testing.T) {
			s, id, source := retainedReviewFixture(t)
			saved, err := s.preparationRepository().Get(t.Context(), source.PreparationID)
			if err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "revision":
				source.Revision = strings.Repeat("0", 64)
			case "pack":
				source.PackDigest = "sha256:" + strings.Repeat("0", 64)
			case "origin":
				source.Origin = "generated"
			case "missing record":
				source.RecordIDs = []string{"missing"}
			case "later missing record":
				source.RecordIDs = []string{"row-1", "missing"}
			case "mixed origin":
				second := saved.Document.Records[0]
				second.ID, second.Class, second.Research = "row-2", "generated", &inputResearchRecord{}
				saved.Document.Records = append(saved.Document.Records, second)
				source.RecordIDs = []string{"row-1", "row-2"}
			case "duplicate requested ID":
				source.RecordIDs = []string{"row-1", "row-1"}
			case "duplicate saved ID":
				saved.Document.Records = append(saved.Document.Records, saved.Document.Records[0])
			case "ordinary":
				saved.Document.Recipe.Qualification = nil
			case "missing custody":
				saved.Document.Records[0].Values = nil
			case "generated review":
				saved.Document.Records[0].Class = "generated"
				saved.Document.Records[0].Research = &inputResearchRecord{}
				source.Origin = "generated"
			}
			if failure == "mixed origin" || failure == "duplicate saved ID" || failure == "ordinary" || failure == "missing custody" || failure == "generated review" {
				saved, err = s.preparationRepository().CompareAndSwap(t.Context(), saved.Document, saved.SHA256)
				if err != nil {
					t.Fatal(err)
				}
				source.Revision = saved.SHA256
			}
			calls := 0
			d, err := s.buildRetainedReviewUsing(t.Context(), id, source, func(ctx context.Context, saved inputPreparationReply, record string) (retainedPreparedInput, error) {
				calls++
				return retainedReviewFixturePrepare(ctx, saved, record)
			}, newAnonymousRetainedReviewFile)
			if d != nil {
				d.abort()
				t.Fatal("invalid source constructed delivery")
			}
			if err == nil || calls != 0 {
				t.Fatal("preflight dispatched or accepted", calls, err)
			}
		})
	}
}

func TestRetainedReviewBuildFaultsNeverBecomeDeliverable(t *testing.T) {
	for _, fault := range []string{"create quota", "write", "sync", "stat", "readback", "source close", "later prepare", "later cancel", "seal cancel", "readback cancel", "later descriptor readback"} {
		t.Run(fault, func(t *testing.T) {
			s, id, source := retainedReviewFixtureRows(t, true)
			repository := &retainedReviewTrackingRepository{inputPreparationRepository: s.preparationRepository(), failClose: fault == "source close"}
			s.inputPreparations = repository
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var file *retainedReviewFaultFile
			creates := 0
			create := func(root *os.Root) (retainedReviewFile, error) {
				creates++
				if fault == "create quota" {
					return nil, errors.New("fixture ENOSPC")
				}
				actual, err := newAnonymousRetainedReviewFile(root)
				if err != nil {
					return nil, err
				}
				file = &retainedReviewFaultFile{retainedReviewFile: actual, writeFault: fault == "write", syncFault: fault == "sync", statFault: fault == "stat", corrupt: fault == "readback"}
				if fault == "seal cancel" {
					file.onSync = cancel
				}
				reads := 0
				file.onRead = func() {
					reads++
					if fault == "readback cancel" && reads == 1 {
						cancel()
					}
					// The complete small stream and first row were already
					// verified; corrupt the second row's descriptor readback.
					if fault == "later descriptor readback" && reads == 3 {
						file.corrupt = true
					}
				}
				return file, nil
			}
			calls := 0
			d, err := s.buildRetainedReviewUsing(ctx, id, source, func(ctx context.Context, saved inputPreparationReply, record string) (retainedPreparedInput, error) {
				calls++
				if fault == "later prepare" && record == "row-2" {
					return retainedPreparedInput{}, io.ErrUnexpectedEOF
				}
				if fault == "later cancel" && record == "row-2" {
					cancel()
				}
				return retainedReviewFixturePrepare(ctx, saved, record)
			}, create)
			if d != nil {
				d.abort()
				t.Fatal("fault returned deliverable spool")
			}
			if err == nil || !repository.closed.Load() {
				t.Fatal("fault or source close ignored", err)
			}
			if file != nil && file.closed.Load() != 1 {
				t.Fatal("descriptor not closed exactly once", file.closed.Load())
			}
			if creates != 1 || fault != "create quota" && file == nil {
				t.Fatal("fixture did not reach filesystem fault boundary", creates)
			}
			expectedCalls := 2
			if fault == "create quota" || fault == "write" {
				expectedCalls = 0
			}
			if calls != expectedCalls {
				t.Fatal("fixture did not exercise complete two-row preparation boundary", calls)
			}
			retainedReviewAssertNoReservations(t, s)
		})
	}
}

func TestRetainedReviewSingleBuildLaneAndBoundedViews(t *testing.T) {
	s, id, source := retainedReviewFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	result := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		d, err := s.buildRetainedReviewUsing(ctx, id, source, func(ctx context.Context, saved inputPreparationReply, record string) (retainedPreparedInput, error) {
			close(entered)
			<-release
			return retainedReviewFixturePrepare(ctx, saved, record)
		}, newAnonymousRetainedReviewFile)
		if d != nil {
			d.abort()
		}
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second * 5):
		t.Fatal("build did not enter fixture")
	}
	cancel() // A canceled but still-running builder must retain its lane.
	if d, err := s.buildRetainedReviewUsing(t.Context(), id, source, retainedReviewFixturePrepare, newAnonymousRetainedReviewFile); !errors.Is(err, errRetainedReviewBusy) {
		if d != nil {
			d.abort()
		}
		t.Fatal("second build was not refused", err)
	}
	unblock.Do(func() { close(release) })
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("canceled builder acknowledged")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled builder did not release")
	}
	views := make([]*retainedReviewView, 0, maxSessions)
	for i := 0; i < maxSessions; i++ {
		d := retainedReviewFixtureBuild(t, s, id, source)
		if err := d.deliver(httptest.NewRecorder()); err != nil {
			t.Fatal(err)
		}
		views = append(views, d.view)
	}
	if d, err := s.buildRetainedReviewUsing(t.Context(), id, source, retainedReviewFixturePrepare, newAnonymousRetainedReviewFile); !errors.Is(err, errRetainedReviewBusy) {
		if d != nil {
			d.abort()
		}
		t.Fatal("65th view admitted", err)
	}
	owner := retainedReviewFixtureOwner(t, s, id)
	for _, v := range views {
		s.retainedReviews.closeOwned(owner, source.PreparationID, v.id, v.indexSHA)
	}
	d := retainedReviewFixtureBuild(t, s, id, source)
	d.abort()
}

func TestRetainedReviewReadCloseExpiryAndOwnership(t *testing.T) {
	s, id, source := retainedReviewFixture(t)
	d := retainedReviewFixtureBuild(t, s, id, source)
	if err := d.deliver(httptest.NewRecorder()); err != nil {
		t.Fatal(err)
	}
	owner := retainedReviewFixtureOwner(t, s, id)
	foreignID, err := s.sessions.create("same subject", nil)
	if err != nil {
		t.Fatal(err)
	}
	retainedReviewAssertUnavailable(t, s, foreignID, d.view)
	foreign := owner
	foreign.server = &Server{}
	if lease, err := s.retainedReviews.read(t.Context(), foreign, source.PreparationID, d.view.id, d.view.indexSHA, 0, "row-1"); lease != nil || !errors.Is(err, errRetainedReviewUnavailable) {
		t.Fatal("foreign Server read admitted", err)
	}
	foreign = owner
	foreign.project = &ProjectRoot{}
	if lease, err := s.retainedReviews.read(t.Context(), foreign, source.PreparationID, d.view.id, d.view.indexSHA, 0, "row-1"); lease != nil || !errors.Is(err, errRetainedReviewUnavailable) {
		t.Fatal("foreign project read admitted", err)
	}
	lease, err := s.retainedReviews.read(t.Context(), owner, source.PreparationID, d.view.id, d.view.indexSHA, 0, "row-1")
	if err != nil {
		t.Fatal(err)
	}
	if second, err := s.retainedReviews.read(t.Context(), owner, source.PreparationID, d.view.id, d.view.indexSHA, 0, "row-1"); second != nil || !errors.Is(err, errRetainedReviewBusy) {
		t.Fatal("concurrent row reader admitted", err)
	}
	s.retainedReviews.closeOwned(owner, source.PreparationID, d.view.id, d.view.indexSHA)
	if lease.ctx.Err() == nil {
		t.Fatal("close did not cancel active read")
	}
	// Close excludes new leases but the existing descriptor stays held until
	// that reader releases. A delayed close cannot break an active ReadAt.
	one := make([]byte, 1)
	if _, err := d.view.file.ReadAt(one, lease.row.Offset); err != nil {
		t.Fatal("descriptor closed before reader release", err)
	}
	retainedReviewAssertUnavailable(t, s, id, d.view)
	lease.close(false)
	lease.close(false)
	if d.view.file != nil {
		t.Fatal("closed descriptor retained after release")
	}
	// Expiry uses this view's idle clock; unrelated session activity does not
	// change it. No sleep or dependency on a real thirty-minute timer is needed.
	d = retainedReviewFixtureBuild(t, s, id, source)
	if err := d.deliver(httptest.NewRecorder()); err != nil {
		t.Fatal(err)
	}
	s.retainedReviews.mu.Lock()
	d.view.idleUntil = time.Now().Add(-time.Second)
	s.retainedReviews.mu.Unlock()
	_, _ = s.sessions.lookup(id)
	s.retainedReviews.expire(d.view)
	retainedReviewAssertUnavailable(t, s, id, d.view)
	if d.view.file != nil {
		t.Fatal("expiry leaked descriptor")
	}
}

func TestRetainedReviewSessionAndServerRetireActiveLeases(t *testing.T) {
	for _, ending := range []string{"session", "server", "installation"} {
		t.Run(ending, func(t *testing.T) {
			s, id, source := retainedReviewFixture(t)
			d := retainedReviewFixtureBuild(t, s, id, source)
			if err := d.deliver(httptest.NewRecorder()); err != nil {
				t.Fatal(err)
			}
			lease, err := s.retainedReviews.read(t.Context(), retainedReviewFixtureOwner(t, s, id), source.PreparationID, d.view.id, d.view.indexSHA, 0, "row-1")
			if err != nil {
				t.Fatal(err)
			}
			defer lease.close(false)
			if ending == "session" {
				s.sessions.revoke(id)
			} else {
				s.retainedReviews.closeServer(s, ending == "installation")
			}
			select {
			case <-lease.ctx.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("owner end did not cancel read")
			}
			lease.close(false)
			// Cancellation of the held session context makes admission fail even
			// before its asynchronous cleanup callback acquires the manager lock.
			select {
			case <-d.view.done:
			case <-time.After(5 * time.Second):
				t.Fatal("retirement did not release descriptor")
			}
			if d.view.file != nil {
				t.Fatal("retired view leaked descriptor")
			}
		})
	}
}

func TestRetainedReviewChildSharesAdmissionButCannotOwnParentContent(t *testing.T) {
	s, id, source := retainedReviewFixture(t)
	d := retainedReviewFixtureBuild(t, s, id, source)
	if err := d.deliver(httptest.NewRecorder()); err != nil {
		t.Fatal(err)
	}
	// A distinct internal Server shares the real parent's session and manager.
	// Even the same pinned project does not make it the content owner.
	child := &Server{cfg: Config{parent: s}, project: s.project, assistant: s.assistant, sessions: s.sessions, signIn: s.signIn, retainedReviews: s.retainedReviews}
	childOwner := retainedReviewFixtureOwner(t, child, id)
	if lease, err := child.retainedReviews.read(t.Context(), childOwner, source.PreparationID, d.view.id, d.view.indexSHA, 0, "row-1"); lease != nil || !errors.Is(err, errRetainedReviewUnavailable) {
		t.Fatal("child reached parent content", err)
	}
	reservation, err := child.retainedReviews.reserve(childOwner, t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer child.retainedReviews.release(reservation)
	defer child.retainedReviews.abort(reservation)
	if parent, err := s.retainedReviews.reserve(retainedReviewFixtureOwner(t, s, id), t.Context()); parent != nil || !errors.Is(err, errRetainedReviewBusy) {
		t.Fatal("child did not share build lane", err)
	}
	child.retainedReviews.closeServer(child, false)
	lease, err := s.retainedReviews.read(t.Context(), retainedReviewFixtureOwner(t, s, id), source.PreparationID, d.view.id, d.view.indexSHA, 0, "row-1")
	if err != nil {
		t.Fatal("retiring child retired parent view", err)
	}
	lease.close(true)
	if _, err := child.retainedReviews.reserve(childOwner, t.Context()); !errors.Is(err, errRetainedReviewUnavailable) {
		t.Fatal("retired child admitted a build", err)
	}
}
