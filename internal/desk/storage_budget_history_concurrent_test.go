package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageHistoryConcurrentServer(t *testing.T) *Server {
	t.Helper()
	cfg := ownerActualConfig(t)
	cfg.JpackBin = standInRuntime(t)
	cfg.Logger = log.New(io.Discard, "", 0)
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("actual startup refused: class=%s", storageFirstUseErrorClass(err))
	}
	t.Cleanup(func() { storageTestMust(t, s.Close()) })
	if s.storageReadOnly || s.storageConstruction == nil || s.storageEnrollment == nil {
		t.Fatal("actual supported startup did not enroll")
	}
	return s
}

func storageHistoryConcurrentRequest(ctx context.Context, s *Server, session string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/conversations", nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+session)
	response := httptest.NewRecorder()
	s.ServeHTTP(response, request)
	return response
}

// Match the actual signed-in overlap: two real conversation GETs demand the
// unselected default history. The follower must inspect the completed original
// selection itself; canceling another queued caller cannot revoke the creator.
func TestStorageHistoryConcurrentFirstUseJoinsOriginalTurn(t *testing.T) {
	requireStorageLine(t)
	s := storageHistoryConcurrentServer(t)
	c, owner := s.storageConstruction, s.storageConstruction.owner
	session := policyTestSession(t, s)
	entered, release := make(chan struct{}), make(chan struct{})
	c.enrollmentFault = &storageEnrollmentFault{point: "initial-control-create", entered: entered, release: release}
	type waiterKey struct{}
	canceledQueued, followerQueued := make(chan struct{}, 1), make(chan struct{}, 1)
	previous := testHookPrivateDataTurnQueued
	testHookPrivateDataTurnQueued = func(ctx context.Context) {
		var signal chan struct{}
		switch ctx.Value(waiterKey{}) {
		case "canceled":
			signal = canceledQueued
		case "follower":
			signal = followerQueued
		}
		if signal != nil {
			select {
			case signal <- struct{}{}:
			default:
			}
		}
	}
	creatorCtx, cancelCreator := context.WithCancel(t.Context())
	canceledCtx, cancelWaiter := context.WithCancel(context.WithValue(t.Context(), waiterKey{}, "canceled"))
	creatorResponse := make(chan *httptest.ResponseRecorder, 1)
	var creatorCompletedAt, followerCompletedAt time.Time
	creatorFinished := make(chan struct{})
	var followerFinished, canceledFinished chan struct{}
	var unblock sync.Once
	defer func() {
		cancelWaiter()
		unblock.Do(func() { close(release) })
		cancelCreator()
		<-creatorFinished
		if followerFinished != nil {
			<-followerFinished
		}
		if canceledFinished != nil {
			<-canceledFinished
		}
		testHookPrivateDataTurnQueued = previous
	}()
	go func() {
		defer close(creatorFinished)
		response := storageHistoryConcurrentRequest(creatorCtx, s, session)
		creatorCompletedAt = time.Now()
		creatorResponse <- response
	}()
	select {
	case <-entered:
	case response := <-creatorResponse:
		t.Fatalf("creator missed actual enrollment boundary: http=%d", response.Code)
	case <-time.After(10 * time.Second):
		t.Fatal("creator did not reach actual enrollment boundary")
	}
	before := storageTestTree(t, s.configDir)
	owner.mu.Lock()
	active := owner.active
	owner.admission.mu.Lock()
	epoch := owner.admission.enrollment
	owner.admission.mu.Unlock()
	owner.mu.Unlock()
	if epoch == nil || active != 1 {
		t.Fatal("creator did not retain the exclusive original epoch")
	}
	canceled := make(chan error, 1)
	canceledFinished = make(chan struct{})
	go func() { defer close(canceledFinished); canceled <- s.ensureDefaultHistoryEnrollment(canceledCtx) }()
	select {
	case <-canceledQueued:
	case err := <-canceled:
		t.Fatalf("canceled waiter bypassed original FIFO: class=%s", storageFirstUseErrorClass(err))
	case <-time.After(time.Second):
		t.Fatal("cancelable caller did not queue")
	}
	cancelWaiter()
	select {
	case err := <-canceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued cancellation: class=%s", storageFirstUseErrorClass(err))
		}
	case <-time.After(time.Second):
		t.Fatal("queued cancellation did not withdraw")
	}
	followerResponse := make(chan *httptest.ResponseRecorder, 1)
	followerFinished = make(chan struct{})
	followerCtx := context.WithValue(t.Context(), waiterKey{}, "follower")
	go func() {
		defer close(followerFinished)
		response := storageHistoryConcurrentRequest(followerCtx, s, session)
		followerCompletedAt = time.Now()
		followerResponse <- response
	}()
	select {
	case <-followerQueued:
	case response := <-followerResponse:
		t.Fatalf("follower bypassed original FIFO: http=%d", response.Code)
	case <-time.After(time.Second):
		t.Fatal("second actual conversation request did not queue")
	}
	owner.mu.Lock()
	owner.admission.mu.Lock()
	same := owner.active == active && owner.admission.enrollment == epoch && owner.admission.phase == storageAdmissionEnrollment
	owner.admission.mu.Unlock()
	owner.mu.Unlock()
	if !same || !reflect.DeepEqual(before, storageTestTree(t, s.configDir)) {
		t.Fatal("queued callers changed creator authority or persisted state")
	}
	barrierReleasedAt := time.Now()
	unblock.Do(func() { close(release) })
	var first, second conversationReply
	for i, channel := range []chan *httptest.ResponseRecorder{creatorResponse, followerResponse} {
		role := []string{"creator", "follower"}[i]
		var response *httptest.ResponseRecorder
		select {
		case response = <-channel:
		case <-time.After(15 * time.Second):
			t.Fatalf("actual concurrent first use did not complete: response=%d role=%s elapsed=%s", i, role, time.Since(barrierReleasedAt))
		}
		// Read only the matching completion stamp after its channel receive.
		// The follower may finish while this loop still waits for the creator.
		completedAt := creatorCompletedAt
		if i == 1 {
			completedAt = followerCompletedAt
		}
		code, class := "none", "none"
		if response.Code != http.StatusOK {
			code, class = "unknown", "unknown"
			var envelope struct {
				Code  string `json:"code"`
				Error string `json:"error"`
			}
			if response.Body.Len() <= 4096 && json.Unmarshal(response.Body.Bytes(), &envelope) == nil {
				if envelope.Code == CodeInternal {
					code = CodeInternal
				}
				// Exact known envelope matches only: never print response prose,
				// infer a wrapped error type, or emit an unknown code.
				for _, known := range []error{context.Canceled, context.DeadlineExceeded, errStorageCustody, errStorageOwnerTransitionRequired, storagebudget.ErrAuthority, storagebudget.ErrBusy, storagebudget.ErrStale, storagebudget.ErrBounds, storagebudget.ErrUnsupported} {
					if envelope.Error == "Chat storage: "+known.Error() {
						class = storageFirstUseErrorClass(known)
						break
					}
				}
			}
		}
		t.Logf("concurrent conversation response=%d role=%s http=%d code=%s exact-envelope-class=%s elapsed=%s", i, role, response.Code, code, class, completedAt.Sub(barrierReleasedAt))
		target := &first
		if i == 1 {
			target = &second
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), target) != nil || target.SHA256 == "" {
			t.Fatalf("actual concurrent conversation read refused: http=%d", response.Code)
		}
	}
	if first.SHA256 != second.SHA256 || !bytes.Equal(first.Content, second.Content) {
		t.Fatal("concurrent readers did not observe the same saved history revision")
	}
	owner.mu.Lock()
	completedEpoch := owner.censusEpoch
	owner.mu.Unlock()
	ordinary := storageHistoryConcurrentRequest(t.Context(), s, session)
	owner.mu.Lock()
	unchangedEpoch := owner.censusEpoch == completedEpoch
	active = owner.active
	owner.mu.Unlock()
	owner.admission.mu.Lock()
	drained := owner.admission.enrollment == nil && owner.admission.shared == 0 && owner.admission.phase == storageAdmissionOpen
	owner.admission.mu.Unlock()
	s.privateDataTurns.mu.Lock()
	queueDrained := len(s.privateDataTurns.waiters) == 0
	s.privateDataTurns.mu.Unlock()
	if ordinary.Code != http.StatusOK || !unchangedEpoch || active != 0 || !drained || !queueDrained {
		t.Fatal("completed first use repeated enrollment or retained admission/queue state")
	}
}

func TestStorageHistoryFirstUseWaitIsBounded(t *testing.T) {
	requireStorageLine(t)
	s := storageHistoryConcurrentServer(t)
	before := storageTestTree(t, s.configDir)
	synctest.Test(t, func(t *testing.T) {
		release, _, err := s.privateDataTurns.acquireOriginal(t.Context())
		storageTestMust(t, err)
		defer release()
		start := time.Now()
		err = s.ensureDefaultHistoryEnrollment(t.Context())
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != privateDataLockWait {
			t.Fatal("first-use queue wait did not honor the existing transaction bound")
		}
		s.privateDataTurns.mu.Lock()
		waiters := len(s.privateDataTurns.waiters)
		s.privateDataTurns.mu.Unlock()
		if waiters != 1 {
			t.Fatal("timed-out caller retained its queue entry")
		}
	})
	if !reflect.DeepEqual(before, storageTestTree(t, s.configDir)) {
		t.Fatal("timed-out first use changed persisted state")
	}
}

func TestStorageHistoryFirstUseKeepsConflictingAdmissionTryOnly(t *testing.T) {
	requireStorageLine(t)
	s := storageHistoryConcurrentServer(t)
	owner := s.storageConstruction.owner
	before := storageTestTree(t, s.configDir)
	admission, err := owner.beginAdmission(t.Context())
	storageTestMust(t, err)
	err = s.ensureDefaultHistoryEnrollment(t.Context())
	closeErr := admission.Close()
	storageTestMust(t, closeErr)
	if !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("first use bypassed actual ordinary admission")
	}
	_, heldOwner, use, err := s.storageStatusLifetime(t.Context())
	storageTestMust(t, err)
	err = s.ensureDefaultHistoryEnrollment(t.Context())
	storageTestMust(t, use.Close())
	storageTestMust(t, heldOwner.drop())
	if !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("first use bypassed actual diagnostic use")
	}
	release, _, err := s.privateDataTurns.acquireOriginal(t.Context())
	storageTestMust(t, err)
	// A custody-owning try caller must refuse immediately, without entering the
	// bounded waiting path that ordinary concurrent readers use.
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	lock, err := s.tryPrivateDataLockContext(ctx, true)
	release()
	if lock != nil {
		_ = lock.Close()
		t.Fatal("try caller bypassed the existing transaction")
	}
	if !errors.Is(err, errPrivateDataTurnBusy) || codeOf(err) != CodeStale || ctx.Err() != nil {
		t.Fatal("custody-owning first use waited or changed its local queue refusal")
	}
	if !reflect.DeepEqual(before, storageTestTree(t, s.configDir)) {
		t.Fatal("refused first use changed persisted state")
	}
}
