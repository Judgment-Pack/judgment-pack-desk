package desk

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// These handwritten fixtures exercise actual move lifecycle functions. The
// existing private prepare callback isolates them from production B3 execution.
func retainedReviewMoveSource(t *testing.T, s *Server) (string, retainedSelectionWireSource) {
	t.Helper()
	doc := repositoryDocument(71)
	repo := s.preparationRepository()
	if _, err := repo.CompareAndSwap(t.Context(), doc, ""); err != nil {
		t.Fatal(err)
	}
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
	id, err := s.sessions.create("move review fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	return id, retainedSelectionWireSource{PreparationID: doc.ID, Revision: saved.SHA256, PackDigest: doc.Recipe.Qualification.Digest, ConversionContract: retainedSelectionInstalledContract(), Origin: "asserted", RecordIDs: []string{"row-1"}, RequestBytes: 64}
}

func retainedReviewMoveReadable(t *testing.T, s *Server, id string, source retainedSelectionWireSource) *retainedReviewDelivery {
	t.Helper()
	d := retainedReviewFixtureBuild(t, s, id, source)
	if err := d.deliver(httptest.NewRecorder()); err != nil {
		t.Fatal(err)
	}
	return d
}

func retainedReviewMoveRead(t *testing.T, s *Server, id string, d *retainedReviewDelivery) *retainedReviewRead {
	t.Helper()
	v := d.view
	lease, err := s.retainedReviews.read(t.Context(), retainedReviewFixtureOwner(t, s, id), v.source.PreparationID, v.id, v.indexSHA, 0, "row-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lease.close(false) })
	return lease
}

func retainedReviewMoveWait(t *testing.T, done <-chan struct{}, problem string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal(problem)
	}
}

func TestRetainedReviewProjectMoveRetiresBuildAndReadOnlyForMovedServer(t *testing.T) {
	s, id, source := retainedReviewFixture(t)
	sibling, siblingID, siblingSource := retainedReviewFixture(t)
	// Distinct real Server/project/session owners share installation admission.
	sibling.retainedReviews = s.retainedReviews
	d := retainedReviewMoveReadable(t, s, id, source)
	lease := retainedReviewMoveRead(t, s, id, d)
	siblingDelivery := retainedReviewMoveReadable(t, sibling, siblingID, siblingSource)

	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	result := make(chan error, 1)
	var buildContext context.Context
	var buildFile retainedReviewFile
	go func() {
		delivery, err := s.buildRetainedReviewUsing(t.Context(), id, source, func(ctx context.Context, saved inputPreparationReply, record string) (retainedPreparedInput, error) {
			buildContext = ctx
			close(entered)
			<-release
			// A callback returning after retirement must not resurrect delivery.
			return retainedReviewFixturePrepare(ctx, saved, record)
		}, func(root *os.Root) (retainedReviewFile, error) {
			file, err := newAnonymousRetainedReviewFile(root)
			buildFile = file
			return file, err
		})
		if delivery != nil {
			delivery.abort()
		}
		result <- err
	}()
	retainedReviewMoveWait(t, entered, "build did not enter fixture")
	s.markProjectMoved(filepath.Join(t.TempDir(), "moved-project"))
	if lease.ctx.Err() == nil {
		t.Fatal("project retirement did not cancel active read")
	}
	retainedReviewMoveWait(t, buildContext.Done(), "project retirement did not cancel active build")
	if _, err := buildFile.Stat(); err != nil {
		t.Fatal("retirement closed descriptor before builder release", err)
	}
	if _, err := d.view.file.Stat(); err != nil {
		t.Fatal("retirement closed descriptor before reader release", err)
	}
	retainedReviewAssertUnavailable(t, s, id, d.view)
	if delivery, err := s.buildRetainedReviewUsing(t.Context(), id, source, retainedReviewFixturePrepare, newAnonymousRetainedReviewFile); delivery != nil || !errors.Is(err, errRetainedReviewUnavailable) {
		if delivery != nil {
			delivery.abort()
		}
		t.Fatal("moved Server admitted another build", err)
	}
	lease.close(true)
	retainedReviewMoveWait(t, d.view.done, "retired reader did not release descriptor")
	unblock.Do(func() { close(release) })
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("late builder resurrected a delivery")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("retired builder did not finish")
	}
	if _, err := buildFile.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("retired builder leaked descriptor", err)
	}
	siblingLease := retainedReviewMoveRead(t, sibling, siblingID, siblingDelivery)
	siblingLease.close(true)
	// Retiring a project leaves the shared manager and other Desks admitted.
	next := retainedReviewMoveReadable(t, sibling, siblingID, siblingSource)
	sibling.retainedReviews.closeOwned(retainedReviewFixtureOwner(t, sibling, siblingID), siblingSource.PreparationID, next.view.id, next.view.indexSHA)
}

func TestRetainedReviewProjectMoveReleasesLocationBeforeRetirement(t *testing.T) {
	s, id, source := retainedReviewFixture(t)
	d := retainedReviewMoveReadable(t, s, id, source)
	manager := s.retainedReviews
	manager.mu.Lock()
	var unlock sync.Once
	defer unlock.Do(manager.mu.Unlock)
	moved := make(chan struct{})
	go func() {
		s.markProjectMoved(filepath.Join(s.projectDir, "retired"))
		close(moved)
	}()
	// Hold the manager in its established manager->location order. The move
	// must publish its location and release location.mu before waiting for us.
	// Yielding plus a deadline bounds observation without timing sleeps.
	observed := make(chan struct{})
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			if s.projectHasMoved() {
				close(observed)
				return
			}
			select {
			case <-stop:
				return
			default:
				runtime.Gosched()
			}
		}
	}()
	retainedReviewMoveWait(t, observed, "move held location.mu while waiting for the review manager")
	select {
	case <-moved:
		t.Fatal("move completed without waiting for review retirement")
	default:
	}
	unlock.Do(manager.mu.Unlock)
	retainedReviewMoveWait(t, moved, "move did not finish after manager release")
	retainedReviewMoveWait(t, d.view.done, "move did not retire readable view")
}

func retainedReviewMoveHome(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg, _ := homeFixture(t)
	// Always supply a test-owned program, including on an integration host.
	cfg.JpackBin = standInRuntime(t)
	cfg.HomeMoveShutdown = func() {}
	s, ts := startDesk(t, cfg)
	t.Cleanup(func() { ts.Close(); s.Close() })
	return s, ts
}

func TestRetainedReviewHomeMoveFailedConfirmationsPreserveViewsAndAdmission(t *testing.T) {
	for _, failure := range []string{"canceled", "stale token", "missing lease"} {
		t.Run(failure, func(t *testing.T) {
			s, _ := retainedReviewMoveHome(t)
			id, source := retainedReviewMoveSource(t, s)
			d := retainedReviewMoveReadable(t, s, id, source)
			destination := filepath.Join(t.TempDir(), "desk-home")
			if _, err := s.planDeskHome("relative-destination"); err == nil {
				t.Fatal("invalid preview accepted")
			}
			failedPreviewLease := retainedReviewMoveRead(t, s, id, d)
			failedPreviewLease.close(true)
			plan, err := s.planDeskHome(destination)
			if err != nil || len(plan.Blockers) != 0 {
				t.Fatal("preview failed", plan.Blockers, err)
			}
			// Preview itself does not retire the current view.
			previewLease := retainedReviewMoveRead(t, s, id, d)
			previewLease.close(true)
			request := projectMoveRequest{Destination: destination, Token: plan.Token}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch failure {
			case "canceled":
				cancel()
			case "stale token":
				request.Token = "unreviewed"
			case "missing lease":
				lease := s.homeLease
				s.homeLease = nil
				defer func() { s.homeLease = lease }()
			}
			if err := s.queueHomeMove(ctx, request); err == nil {
				t.Fatal("failed confirmation accepted")
			}
			if s.homeMove.queued.Load() || s.projectHasMoved() {
				t.Fatal("failed confirmation retired installation")
			}
			if _, err := os.Stat(homeJournalPath(s.configDir)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed confirmation wrote move journal", err)
			}
			lease := retainedReviewMoveRead(t, s, id, d)
			lease.close(true)
			next := retainedReviewMoveReadable(t, s, id, source)
			s.retainedReviews.closeOwned(retainedReviewFixtureOwner(t, s, id), source.PreparationID, next.view.id, next.view.indexSHA)
		})
	}
}

func TestRetainedReviewQueuedHomeMoveRetiresParentAndChildren(t *testing.T) {
	s, ts := retainedReviewMoveHome(t)
	row := createTestDesk(t, ts, "Move review child")
	child := s.desks[row.ID]
	parentID, parentSource := retainedReviewMoveSource(t, s)
	childID, childSource := retainedReviewMoveSource(t, child)
	parentDelivery := retainedReviewMoveReadable(t, s, parentID, parentSource)
	childDelivery := retainedReviewMoveReadable(t, child, childID, childSource)
	parentLease := retainedReviewMoveRead(t, s, parentID, parentDelivery)
	childLease := retainedReviewMoveRead(t, child, childID, childDelivery)
	plan, err := s.planDeskHome(filepath.Join(t.TempDir(), "desk-home"))
	if err != nil || len(plan.Blockers) != 0 {
		t.Fatal("preview failed", plan.Blockers, err)
	}
	if err := s.queueHomeMove(t.Context(), projectMoveRequest{Destination: plan.Destination, Token: plan.Token}); err != nil {
		t.Fatal("confirmed move was not queued", err)
	}
	if !s.homeMove.queued.Load() || !s.projectHasMoved() || !child.projectHasMoved() {
		t.Fatal("successful queue did not mark every Desk moved")
	}
	tx, err := readHomeTransaction(homeJournalPath(s.configDir))
	if err != nil || tx == nil || tx.Complete || tx.Destination != plan.Destination {
		t.Fatal("queued move has no pending authoritative journal", err)
	}
	if _, err := os.Stat(plan.Destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("queue moved files before shutdown", err)
	}
	for _, item := range []struct {
		server   *Server
		id       string
		source   retainedSelectionWireSource
		delivery *retainedReviewDelivery
		lease    *retainedReviewRead
	}{{s, parentID, parentSource, parentDelivery, parentLease}, {child, childID, childSource, childDelivery, childLease}} {
		if item.lease.ctx.Err() == nil {
			t.Fatal("queued move did not cancel active read")
		}
		retainedReviewAssertUnavailable(t, item.server, item.id, item.delivery.view)
		item.lease.close(true)
		retainedReviewMoveWait(t, item.delivery.view.done, "queued move leaked descriptor")
		if delivery, err := item.server.buildRetainedReviewUsing(t.Context(), item.id, item.source, retainedReviewFixturePrepare, newAnonymousRetainedReviewFile); delivery != nil || !errors.Is(err, errRetainedReviewUnavailable) {
			if delivery != nil {
				delivery.abort()
			}
			t.Fatal("queued move admitted a new build", err)
		}
	}
	// Installation retirement is permanent even for an owner outside the
	// currently open Desk list; admission cannot resurrect through that owner.
	s.retainedReviews.mu.Lock()
	closed := s.retainedReviews.closed
	s.retainedReviews.mu.Unlock()
	if !closed {
		t.Fatal("home move left installation admission open")
	}
}
