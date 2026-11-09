package desk

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

type privateDataFirstUseTurnResult struct {
	release func()
	witness *privateDataTurnWitness
	err     error
}

func privateDataFirstUseTestKey() privateDataFirstUseKey {
	return privateDataFirstUseKey{construction: &storageBudgetConstruction{}, requester: &Server{}}
}

func privateDataFirstUseTestQueue(ctx context.Context, q *privateDataTurnQueue, key privateDataFirstUseKey, ordinary bool) <-chan privateDataFirstUseTurnResult {
	result := make(chan privateDataFirstUseTurnResult, 1)
	go func() {
		var got privateDataFirstUseTurnResult
		if ordinary {
			got.release, got.witness, got.err = q.acquireOriginal(ctx)
		} else {
			got.release, got.witness, got.err = q.acquireFirstUseOriginal(ctx, key)
		}
		result <- got
	}()
	synctest.Wait()
	return result
}

func privateDataFirstUseTestOwned(t *testing.T, result <-chan privateDataFirstUseTurnResult) privateDataFirstUseTurnResult {
	t.Helper()
	got := <-result
	if got.err != nil || got.release == nil || !got.witness.current() {
		t.Fatal("queued caller did not receive its original turn", got.err)
	}
	return got
}

func privateDataFirstUseTestExpired(t *testing.T, result <-chan privateDataFirstUseTurnResult, want error) {
	t.Helper()
	got := <-result
	if !errors.Is(got.err, want) || got.release != nil || got.witness != nil {
		t.Fatal("expired caller gained a turn or changed its refusal", got.err)
	}
}

func TestPrivateDataFirstUseMatchingJoinKeepsFIFOAndOriginalWitness(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var q privateDataTurnQueue
		key := privateDataFirstUseTestKey()
		release, head, err := q.acquireFirstUseOriginal(t.Context(), key)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		first := privateDataFirstUseTestQueue(t.Context(), &q, key, false)
		second := privateDataFirstUseTestQueue(t.Context(), &q, key, false)
		firstOriginal, secondOriginal := q.waiters[1], q.waiters[2]
		time.Sleep(6 * time.Second)
		if !head.current() || firstOriginal.current() || secondOriginal.current() || len(q.waiters) != 3 {
			t.Fatal("matching join changed head authority or withdrew before 15 seconds")
		}
		if got, _, err := q.tryAcquireOriginal(t.Context()); got != nil || !errors.Is(err, errPrivateDataTurnBusy) {
			t.Fatal("try admission bypassed first-use waiters", err)
		}
		release()
		got := privateDataFirstUseTestOwned(t, first)
		if got.witness != firstOriginal || head.current() || secondOriginal.current() {
			t.Fatal("first join changed FIFO or witness identity")
		}
		copyOfWitness := *got.witness
		if copyOfWitness.current() {
			t.Fatal("copied witness gained original authority")
		}
		release() // Repeated predecessor release must leave the new head intact.
		if !got.witness.current() {
			t.Fatal("repeated release removed the next owner")
		}
		got.release()
		last := privateDataFirstUseTestOwned(t, second)
		if last.witness != secondOriginal || firstOriginal.current() {
			t.Fatal("second join changed original witness identity")
		}
		last.release()
		if len(q.waiters) != 0 || secondOriginal.current() {
			t.Fatal("completed joins retained authority or queue entries")
		}
	})
}

func TestPrivateDataFirstUseMatchingJoinHasAbsoluteBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var q privateDataTurnQueue
		key := privateDataFirstUseTestKey()
		release, head, err := q.acquireFirstUseOriginal(t.Context(), key)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		started := time.Now()
		result := privateDataFirstUseTestQueue(t.Context(), &q, key, false)
		privateDataFirstUseTestExpired(t, result, context.DeadlineExceeded)
		if time.Since(started) != 15*time.Second || len(q.waiters) != 1 || !head.current() {
			t.Fatal("absolute join expiry changed the creator or its bound")
		}
	})
}

func TestPrivateDataFirstUseUnclassifiedAndDifferentKeysKeepShortBound(t *testing.T) {
	for _, kind := range []string{"ordinary", "construction", "requester", "empty-key", "partial-key"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var q privateDataTurnQueue
				key := privateDataFirstUseTestKey()
				var release func()
				var head *privateDataTurnWitness
				var err error
				if kind == "ordinary" {
					release, head, err = q.acquireOriginal(t.Context())
				} else {
					release, head, err = q.acquireFirstUseOriginal(t.Context(), key)
				}
				if err != nil {
					t.Fatal(err)
				}
				defer release()
				switch kind {
				case "construction":
					key.construction = &storageBudgetConstruction{}
				case "requester":
					key.requester = &Server{}
				case "empty-key":
					key = privateDataFirstUseKey{}
				case "partial-key":
					key.requester = nil
				}
				started := time.Now()
				result := privateDataFirstUseTestQueue(t.Context(), &q, key, false)
				privateDataFirstUseTestExpired(t, result, context.DeadlineExceeded)
				if time.Since(started) != privateDataLockWait || len(q.waiters) != 1 || !head.current() {
					t.Fatal("nonmatching join changed the short allowance or creator")
				}
			})
		})
	}
}

func TestPrivateDataFirstUseMixedHeadsKeepCumulativeShortBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var q privateDataTurnQueue
		key := privateDataFirstUseTestKey()
		release, _, err := q.acquireFirstUseOriginal(t.Context(), key)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		ordinary1 := privateDataFirstUseTestQueue(t.Context(), &q, key, true)
		matching := privateDataFirstUseTestQueue(t.Context(), &q, key, false)
		ordinary2 := privateDataFirstUseTestQueue(t.Context(), &q, key, true)
		started := time.Now()
		result := privateDataFirstUseTestQueue(t.Context(), &q, key, false)
		time.Sleep(3 * time.Second)
		release()
		first := privateDataFirstUseTestOwned(t, ordinary1)
		time.Sleep(2 * time.Second)
		first.release()
		middle := privateDataFirstUseTestOwned(t, matching)
		time.Sleep(4 * time.Second)
		middle.release()
		last := privateDataFirstUseTestOwned(t, ordinary2)
		defer last.release()
		privateDataFirstUseTestExpired(t, result, context.DeadlineExceeded)
		if time.Since(started) != 12*time.Second || len(q.waiters) != 1 || !last.witness.current() {
			t.Fatal("head changes reset the cumulative five-second allowance")
		}
	})
}

func TestPrivateDataFirstUseMixedHeadsKeepAbsoluteBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var q privateDataTurnQueue
		key := privateDataFirstUseTestKey()
		release, _, err := q.acquireFirstUseOriginal(t.Context(), key)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		ordinary := privateDataFirstUseTestQueue(t.Context(), &q, key, true)
		matching := privateDataFirstUseTestQueue(t.Context(), &q, key, false)
		started := time.Now()
		result := privateDataFirstUseTestQueue(t.Context(), &q, key, false)
		time.Sleep(6 * time.Second)
		release()
		short := privateDataFirstUseTestOwned(t, ordinary)
		time.Sleep(time.Second)
		short.release()
		long := privateDataFirstUseTestOwned(t, matching)
		defer long.release()
		privateDataFirstUseTestExpired(t, result, context.DeadlineExceeded)
		if time.Since(started) != 15*time.Second || len(q.waiters) != 1 || !long.witness.current() {
			t.Fatal("head changes restarted the absolute join bound")
		}
	})
}

func TestPrivateDataFirstUseAccountsUnobservedHeadChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var q privateDataTurnQueue
		key := privateDataFirstUseTestKey()
		release, _, err := q.acquireFirstUseOriginal(t.Context(), key)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		ordinary1 := privateDataFirstUseTestQueue(t.Context(), &q, key, true)
		matching1 := privateDataFirstUseTestQueue(t.Context(), &q, key, false)
		ordinary2 := privateDataFirstUseTestQueue(t.Context(), &q, key, true)
		matching2 := privateDataFirstUseTestQueue(t.Context(), &q, key, false)
		// Hold this waiter after enqueue so it cannot observe any notifications.
		// Matching heads must not hide the intervening unrelated intervals.
		type blockedKey struct{}
		unblock := make(chan struct{})
		defer func() {
			select {
			case <-unblock:
			default:
				close(unblock)
			}
		}()
		previous := testHookPrivateDataTurnQueued
		testHookPrivateDataTurnQueued = func(ctx context.Context) {
			if ctx.Value(blockedKey{}) != nil {
				<-unblock
			}
		}
		defer func() { testHookPrivateDataTurnQueued = previous }()
		result := privateDataFirstUseTestQueue(context.WithValue(t.Context(), blockedKey{}, true), &q, key, false)
		time.Sleep(time.Second)
		release()
		first := privateDataFirstUseTestOwned(t, ordinary1)
		time.Sleep(2 * time.Second)
		first.release()
		second := privateDataFirstUseTestOwned(t, matching1)
		time.Sleep(time.Second)
		second.release()
		third := privateDataFirstUseTestOwned(t, ordinary2)
		time.Sleep(2 * time.Second)
		third.release()
		fourth := privateDataFirstUseTestOwned(t, matching2)
		defer fourth.release()
		time.Sleep(time.Second)
		q.mu.Lock()
		original := q.waiters[1]
		charged := original.nonmatching
		q.mu.Unlock()
		if charged != 4*time.Second {
			t.Fatalf("unobserved unrelated waiting = %s; want 4s", charged)
		}
		close(unblock)
		synctest.Wait()
		if len(q.waiters) != 2 || !fourth.witness.current() {
			t.Fatal("matching head did not preserve the remaining short allowance")
		}
		fourth.release()
		last := privateDataFirstUseTestOwned(t, result)
		if last.witness != original || last.witness.nonmatching != 4*time.Second {
			t.Fatal("unobserved head changes lost time or changed the original witness")
		}
		last.release()
	})
}

func TestPrivateDataFirstUseEarlierCallerDeadlineAndCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "canceled"}[canceled], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var q privateDataTurnQueue
				key := privateDataFirstUseTestKey()
				release, head, err := q.acquireFirstUseOriginal(t.Context(), key)
				if err != nil {
					t.Fatal(err)
				}
				defer release()
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
				defer cancel()
				started := time.Now()
				result := privateDataFirstUseTestQueue(ctx, &q, key, false)
				next := privateDataFirstUseTestQueue(t.Context(), &q, key, false)
				want, elapsed := error(context.DeadlineExceeded), 2*time.Second
				if canceled {
					time.Sleep(time.Second)
					cancel()
					want, elapsed = context.Canceled, time.Second
				}
				privateDataFirstUseTestExpired(t, result, want)
				if time.Since(started) != elapsed || len(q.waiters) != 2 || !head.current() {
					t.Fatal("caller expiry changed another waiter's entry or the creator")
				}
				release()
				last := privateDataFirstUseTestOwned(t, next)
				last.release()
			})
		})
	}
}

type privateDataFirstUseCancelOnReadyContext struct {
	context.Context
	ready  func() bool
	cancel context.CancelFunc
}

func (ctx privateDataFirstUseCancelOnReadyContext) Err() error {
	if ctx.ready() {
		ctx.cancel()
	}
	return ctx.Context.Err()
}

func TestPrivateDataFirstUseCancellationWinsReady(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var q privateDataTurnQueue
		key := privateDataFirstUseTestKey()
		release, _, err := q.acquireFirstUseOriginal(t.Context(), key)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		base, cancel := context.WithCancel(t.Context())
		defer cancel()
		var original *privateDataTurnWitness
		ctx := privateDataFirstUseCancelOnReadyContext{Context: base, cancel: cancel, ready: func() bool {
			q.mu.Lock()
			defer q.mu.Unlock()
			return original != nil && len(q.waiters) != 0 && q.waiters[0] == original
		}}
		result := privateDataFirstUseTestQueue(ctx, &q, key, false)
		original = q.waiters[1]
		next := privateDataFirstUseTestQueue(t.Context(), &q, key, false)
		release()
		privateDataFirstUseTestExpired(t, result, context.Canceled)
		last := privateDataFirstUseTestOwned(t, next)
		defer last.release()
		if original.current() || len(q.waiters) != 1 {
			t.Fatal("cancellation against readiness retained the canceled original")
		}
	})
}

func TestPrivateDataFirstUseBoundsWinReady(t *testing.T) {
	for _, ordinary := range []bool{false, true} {
		t.Run(map[bool]string{false: "absolute", true: "short"}[ordinary], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var q privateDataTurnQueue
				key := privateDataFirstUseTestKey()
				var release func()
				var err error
				bound := 15 * time.Second
				if ordinary {
					release, _, err = q.acquireOriginal(t.Context())
					bound = privateDataLockWait
				} else {
					release, _, err = q.acquireFirstUseOriginal(t.Context(), key)
				}
				if err != nil {
					t.Fatal(err)
				}
				defer release()
				unblock := make(chan struct{})
				previous := testHookPrivateDataTurnQueued
				testHookPrivateDataTurnQueued = func(context.Context) { <-unblock }
				defer func() { testHookPrivateDataTurnQueued = previous }()
				result := privateDataFirstUseTestQueue(t.Context(), &q, key, false)
				time.Sleep(bound)
				release() // Make ready before the caller can inspect either bound.
				close(unblock)
				privateDataFirstUseTestExpired(t, result, context.DeadlineExceeded)
				if len(q.waiters) != 0 {
					t.Fatal("ready expired caller retained its original entry")
				}
				next, witness, err := q.tryAcquireOriginal(t.Context())
				if err != nil || !witness.current() {
					t.Fatal("ready expiry wedged the next original admission", err)
				}
				next()
			})
		})
	}
}

// The Server can initialize and drain its queue before a caller enters a
// fake-clock bubble. A later first-use waiter must use its new queue lifetime.
func TestPrivateDataFirstUseReusedIdleQueueHasLocalNotification(t *testing.T) {
	var q privateDataTurnQueue
	release, err := q.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
	// Fail before entering synctest if an external notification remains. A
	// select on that channel is not durably blocked in the later bubble.
	q.mu.Lock()
	idle := len(q.waiters) == 0 && q.changed == nil
	q.mu.Unlock()
	if !idle {
		t.Fatal("drained queue retained its prior notification lifetime")
	}
	synctest.Test(t, func(t *testing.T) {
		head, err := q.acquire(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer head()
		start := time.Now()
		result := privateDataFirstUseTestQueue(t.Context(), &q, privateDataFirstUseTestKey(), false)
		privateDataFirstUseTestExpired(t, result, context.DeadlineExceeded)
		if elapsed := time.Since(start); elapsed != privateDataLockWait {
			t.Fatalf("reused queue short wait = %s", elapsed)
		}
		if len(q.waiters) != 1 {
			t.Fatal("expired first-use waiter changed original holder")
		}
		head()
		if len(q.waiters) != 0 || q.changed != nil {
			t.Fatal("second queue lifetime did not drain")
		}
	})
}

// The actual private-lock path carries charged enrollment waiting into the
// remaining queue/OS allowance, while same-first-use waiting keeps its own cap.
func TestPrivateDataFirstUseShortLockCarriesOnlyNonmatchingWait(t *testing.T) {
	requireStorageLine(t)
	for _, matching := range []bool{false, true} {
		name := "unrelated"
		if matching {
			name = "matching"
		}
		t.Run(name, func(t *testing.T) {
			s, _, _ := assistantServer(t)
			if err := s.ensureDefaultHistoryEnrollment(t.Context()); err != nil {
				t.Fatal(err)
			}
			synctest.Test(t, func(t *testing.T) {
				var head func()
				var err error
				if matching {
					key := privateDataFirstUseKey{construction: s.storageEnrollment.construction, requester: s}
					head, _, err = s.privateDataTurns.acquireFirstUseOriginal(t.Context(), key)
				} else {
					head, err = s.privateDataTurns.acquire(t.Context())
				}
				if err != nil {
					t.Fatal(err)
				}
				defer head()
				foreign, err := s.privateDataFileLock(".data.lock", true)
				if err != nil {
					t.Fatal(err)
				}
				defer foreign.Close()
				result := make(chan error, 1)
				start := time.Now()
				go func() {
					lock, err := s.privateDataLockContext(t.Context(), true)
					if lock != nil {
						lock.Close()
					}
					result <- err
				}()
				synctest.Wait()
				wait := 4 * time.Second
				want := privateDataLockWait
				if matching {
					wait = 6 * time.Second
					want += wait
				}
				time.Sleep(wait)
				head()
				if err := <-result; !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("foreign OS lock did not refuse", err)
				}
				if elapsed := time.Since(start); elapsed != want {
					t.Fatalf("combined enrollment/short wait = %s; want %s", elapsed, want)
				}
				if len(s.privateDataTurns.waiters) != 0 {
					t.Fatal("expired short lock retained a turn")
				}
			})
		})
	}
}
