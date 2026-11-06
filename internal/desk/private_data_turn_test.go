//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"
)

func TestPrivateDataShortLockQueuedReaderBeforeReacquire(t *testing.T) {
	s, _, _ := assistantServer(t)
	synctest.Test(t, func(t *testing.T) {
		held, err := s.privateDataLock(true)
		if err != nil {
			t.Fatal(err)
		}
		defer held.Close()
		order := make(chan string, 4)
		readerClose := make(chan struct{})
		go func() {
			read, err := s.privateDataLock(false)
			if err != nil {
				t.Error(err)
				return
			}
			order <- "reader"
			<-readerClose
			read.Close()
		}()
		synctest.Wait() // Reader is queued behind the current transaction.
		go func() {
			for range 3 {
				write, err := s.privateDataLock(true)
				if err != nil {
					t.Error(err)
					return
				}
				order <- "writer"
				write.Close()
			}
		}()
		synctest.Wait()
		held.Close()
		if got := <-order; got != "reader" {
			t.Errorf("first acquisition = %s", got)
		}
		synctest.Wait()
		select {
		case got := <-order:
			t.Errorf("writer acquired before reader closed: %s", got)
		default:
		}
		close(readerClose)
		for range 3 {
			if got := <-order; got != "writer" {
				t.Errorf("next = %s", got)
			}
		}
	})
}

func TestPrivateDataTurnCancellationWithdraws(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var q privateDataTurnQueue
		release, err := q.acquire(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		canceled := make(chan error, 1)
		go func() {
			turn, err := q.acquire(ctx)
			if turn != nil {
				turn()
			}
			canceled <- err
		}()
		synctest.Wait()
		done := make(chan struct{})
		go func() {
			turn, err := q.acquire(t.Context())
			if err != nil {
				t.Error(err)
			} else {
				turn()
			}
			close(done)
		}()
		synctest.Wait()
		cancel()
		if err := <-canceled; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation = %v", err)
		}
		release()
		release() // Must not release the next waiter's turn a second time.
		<-done
		if len(q.waiters) != 0 {
			t.Fatal("queue retained a canceled or completed turn")
		}
		if turn, err := q.acquire(ctx); !errors.Is(err, context.Canceled) || turn != nil {
			t.Fatal("expired caller gained custody", err)
		}
		if len(q.waiters) != 0 {
			t.Fatal("expired head wedged queue")
		}
	})
}

func TestPrivateDataShortLockFailureAndIdempotentClose(t *testing.T) {
	s, _, _ := assistantServer(t)
	path := filepath.Join(s.configDir, ".data.lock")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if lock, err := s.privateDataLock(true); err == nil {
		lock.Close()
		t.Fatal("unsafe lock admitted")
	}
	if len(s.privateDataTurns.waiters) != 0 {
		t.Fatal("validation failure retained turn")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	first, err := s.privateDataLock(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := s.privateDataLock(true)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := first.Close(); err != nil {
		t.Fatal("repeated close", err)
	}
	if len(s.privateDataTurns.waiters) != 1 {
		t.Fatal("repeated close released another turn")
	}
	if raw, err := s.privateDataFileLock(".data.lock", true); err == nil {
		raw.Close()
		t.Fatal("OS custody lost on repeated close")
	}
}

func TestPrivateDataShortLockOverallDeadlineAndRawLeases(t *testing.T) {
	s, _, _ := assistantServer(t)
	if err := s.ensureDefaultHistoryEnrollment(t.Context()); err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		release, err := s.privateDataTurns.acquire(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		// Raw ownership/admission locks bypass local admission and remain nonblocking.
		foreign, err := s.privateDataFileLock(".data.lock", true)
		if err != nil {
			t.Fatal(err)
		}
		defer foreign.Close()
		if second, err := s.privateDataFileLock(".data.lock", true); err == nil {
			second.Close()
			t.Fatal("raw lease did not exclude")
		}
		result := make(chan error, 1)
		start := time.Now()
		go func() {
			lock, err := s.privateDataLock(true)
			if lock != nil {
				lock.Close()
			}
			result <- err
		}()
		synctest.Wait()
		time.Sleep(4 * time.Second) // Advance the fake clock while the local holder owns its turn.
		release()
		if err := <-result; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("foreign OS lock did not refuse", err)
		}
		if elapsed := time.Since(start); elapsed != privateDataLockWait {
			t.Fatalf("overall wait = %s", elapsed)
		}
		if len(s.privateDataTurns.waiters) != 0 {
			t.Fatal("failed OS acquisition retained turn")
		}
		foreign.Close()
		lock, err := s.privateDataLock(true)
		if err != nil {
			t.Fatal("failed acquisition wedged queue", err)
		}
		lock.Close()
	})
}

func TestPrivateDataShortLockOpenFailureReleasesTurn(t *testing.T) {
	s, _, _ := assistantServer(t)
	if err := s.assistant.root.Close(); err != nil {
		t.Fatal(err)
	}
	if lock, err := s.privateDataLock(true); err == nil {
		lock.Close()
		t.Fatal("closed storage root admitted a lock")
	}
	if len(s.privateDataTurns.waiters) != 0 {
		t.Fatal("failed open retained a local turn")
	}
}

func TestPrivateDataTryTurnDoesNotOvertakeWaiter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var q privateDataTurnQueue
		head, err := q.acquire(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		acquired, finish, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		go func() {
			defer close(done)
			release, err := q.acquire(t.Context())
			if err != nil {
				t.Error(err)
				return
			}
			close(acquired)
			<-finish
			release()
		}()
		synctest.Wait()
		if release, err := q.tryAcquire(t.Context()); release != nil || !errors.Is(err, errPrivateDataTurnBusy) || len(q.waiters) != 2 {
			t.Fatal("try overtook queued waiter", err)
		}
		head()
		<-acquired
		if release, err := q.tryAcquire(t.Context()); release != nil || !errors.Is(err, errPrivateDataTurnBusy) || len(q.waiters) != 1 {
			t.Fatal("try overtook admitted waiter", err)
		}
		close(finish)
		<-done
		release, err := q.tryAcquire(t.Context())
		if err != nil {
			t.Fatal("queue did not drain", err)
		}
		release()
		next, err := q.acquire(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		release() // Idempotent try release cannot remove a later transaction.
		if len(q.waiters) != 1 {
			t.Fatal("try released a later turn")
		}
		next()
	})
}

func TestPrivateDataTryTurnMutexAndCancellationAreNonblocking(t *testing.T) {
	var q privateDataTurnQueue
	q.mu.Lock()
	release, err := q.tryAcquire(t.Context())
	q.mu.Unlock()
	if release != nil || !errors.Is(err, errPrivateDataTurnBusy) {
		t.Fatal("queue mutex contention did not refuse", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if release, err := q.tryAcquire(ctx); release != nil || !errors.Is(err, context.Canceled) || len(q.waiters) != 0 {
		t.Fatal("canceled try retained custody", err)
	}
}

func TestPrivateDataTryOSContentionReleasesTurnForWaiter(t *testing.T) {
	s, _, _ := assistantServer(t)
	if err := s.ensureDefaultHistoryEnrollment(t.Context()); err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		foreign, err := s.privateDataFileLock(".data.lock", true)
		if err != nil {
			t.Fatal(err)
		}
		defer foreign.Close()
		if lock, err := s.tryPrivateDataLockContext(t.Context(), true); lock != nil || err == nil || !lockHeld(err) {
			t.Fatal("OS contention admitted or failed to refuse", err)
		}
		if len(s.privateDataTurns.waiters) != 0 {
			t.Fatal("failed OS try retained turn")
		}
		result := make(chan *privateDataShortLock, 1)
		go func() {
			lock, err := s.privateDataLockContext(t.Context(), true)
			if err != nil {
				t.Error(err)
			}
			result <- lock
		}()
		synctest.Wait()
		if len(s.privateDataTurns.waiters) != 1 {
			t.Fatal("normal waiter did not receive released turn")
		}
		if lock, err := s.tryPrivateDataLockContext(t.Context(), true); lock != nil || !errors.Is(err, errPrivateDataTurnBusy) {
			t.Fatal("try bypassed existing OS waiter", err)
		}
		foreign.Close()
		lock := <-result
		if lock == nil {
			t.Fatal("next waiter failed to progress")
		}
		lock.Close()
	})
}

// This context cancels when the real OS lock becomes owned, without relying on
// sleeps or the number of internal context checks. Err still obeys Context's
// contract: it is nonnil only after the embedded cancellation has completed.
type privateDataCancelOnCustodyContext struct {
	context.Context
	owned  func() bool
	cancel context.CancelFunc
}

func (ctx privateDataCancelOnCustodyContext) Err() error {
	if ctx.owned() {
		ctx.cancel()
	}
	return ctx.Context.Err()
}

func TestPrivateDataTryCancellationAfterOSAdmissionAndIdempotentClose(t *testing.T) {
	s, _, _ := assistantServer(t)
	base, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := privateDataCancelOnCustodyContext{Context: base, cancel: cancel, owned: func() bool {
		probe, err := s.privateDataFileLock(".data.lock", true)
		if err == nil {
			probe.Close()
			return false
		}
		if !lockHeld(err) {
			t.Fatal("custody probe failed", err)
		}
		return true
	}}
	if lock, err := s.tryPrivateDataLockContext(ctx, true); lock != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation after OS admission lost", err)
	}
	if len(s.privateDataTurns.waiters) != 0 {
		t.Fatal("canceled OS try retained queue turn")
	}
	first, err := s.tryPrivateDataLockContext(t.Context(), true)
	if err != nil {
		t.Fatal("canceled OS try retained descriptor", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := s.tryPrivateDataLockContext(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := first.Close(); err != nil || len(s.privateDataTurns.waiters) != 1 {
		t.Fatal("repeat try close released later transaction", err)
	}
	if probe, err := s.privateDataFileLock(".data.lock", true); err == nil {
		probe.Close()
		t.Fatal("repeat close lost later OS custody")
	}
}

func TestPrivateDataTryOpenAndValidationFailureReleaseTurn(t *testing.T) {
	for _, mode := range []string{"permissions", "closed-root", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _ := assistantServer(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch mode {
			case "permissions":
				path := filepath.Join(s.configDir, ".data.lock")
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "closed-root":
				s.assistant.root.Close()
			case "canceled":
				cancel()
			}
			if lock, err := s.tryPrivateDataLockContext(ctx, true); lock != nil || err == nil {
				t.Fatal("unavailable storage admitted try", err)
			}
			if len(s.privateDataTurns.waiters) != 0 {
				t.Fatal("failed try retained turn")
			}
		})
	}
}
