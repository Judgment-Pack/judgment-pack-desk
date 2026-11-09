//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// Helper-only real storage and ownership descriptors; no run, operation,
// receipt, provider or publication authority is synthesized by these tests.
// The separate actual API regression supplies the real dispatched root.
func assistantDialectShortFixture(t *testing.T, home bool) *assistantDialectShortOriginal {
	t.Helper()
	var s *Server
	if home {
		_, _, s, _ = dialectArtifactHomeAnchorFixture(t, "short-admission")
	} else {
		s, _ = dialectArtifactOwnerSetup(t)
	}
	store := s.assistantRuns()
	l, err := store.Acquire(t.Context())
	assistantRunMust(t, err)
	t.Cleanup(func() { assistantRunMust(t, l.Close()) })
	w := &assistantRunWriter{lease: l}
	h := &assistantRunService{server: s, lease: l, active: map[string]*assistantRunExecution{}}
	e := &assistantRunExecution{service: h, writer: w, ctx: t.Context(), record: assistantRunRecord{ID: "short-admission-helper"}}
	h.active[e.record.ID] = e
	a, err := captureAssistantDialectShortOriginal(t.Context(), e, w, l)
	assistantRunMust(t, err)
	return a
}

func assistantDialectShortReleased(t *testing.T, a *assistantDialectShortOriginal) {
	t.Helper()
	for _, m := range []*sync.Mutex{a.server.writes, &a.execution.canonicalMu, &a.writer.mu, &a.lease.mu} {
		if !m.TryLock() {
			t.Fatal("short acquisition retained a host hold")
		}
		m.Unlock()
	}
	probe, err := a.server.privateDataFileLock(".data.lock", true)
	assistantRunMust(t, err)
	assistantRunMust(t, probe.Close())
	if len(a.server.privateDataTurns.waiters) != 0 {
		t.Fatal("short acquisition retained a local turn")
	}
}

func TestAssistantDialectShortAdmissionMutexContention(t *testing.T) {
	for _, which := range []string{"canonical", "writer"} {
		t.Run(which, func(t *testing.T) {
			a := assistantDialectShortFixture(t, false)
			synctest.Test(t, func(t *testing.T) {
				target := &a.execution.canonicalMu
				if which == "writer" {
					target = &a.writer.mu
				}
				target.Lock()
				locked := true
				defer func() {
					if locked {
						target.Unlock()
					}
				}()
				type outcome struct {
					g   *assistantDialectShortCustody
					err error
				}
				result := make(chan outcome, 1)
				go func() { g, err := a.acquire(t.Context(), assistantDialectShortTransaction); result <- outcome{g, err} }()
				synctest.Wait() // Admission has withdrawn and is in its first fake timer.
				if !a.server.writes.TryLock() {
					t.Fatal("backoff retained writes")
				}
				a.server.writes.Unlock()
				if !a.lease.mu.TryLock() {
					t.Fatal("backoff retained executor custody")
				}
				a.lease.mu.Unlock()
				probe, err := a.server.privateDataFileLock(".data.lock", true)
				assistantRunMust(t, err)
				assistantRunMust(t, probe.Close())
				if len(a.server.privateDataTurns.waiters) != 0 {
					t.Fatal("backoff retained queue claim")
				}
				// Another real transaction owns the turn before the original requeues.
				other, err := a.server.privateDataLockContext(t.Context(), true)
				assistantRunMust(t, err)
				defer other.Close()
				target.Unlock()
				locked = false
				time.Sleep(5 * time.Millisecond) // Fake time only; never a scheduling sleep.
				synctest.Wait()
				if len(a.server.privateDataTurns.waiters) != 2 {
					t.Fatal("original admission did not queue behind the earlier transaction")
				}
				select {
				case got := <-result:
					if got.g != nil {
						got.g.Close()
					}
					t.Fatal("admission overtook existing transaction", got.err)
				default:
				}
				assistantRunMust(t, other.Close())
				got := <-result
				assistantRunMust(t, got.err)
				assistantRunMust(t, a.checkPrivate(got.g))
				assistantRunMust(t, got.g.Close())
				assistantRunMust(t, got.g.Close())
				assistantDialectShortReleased(t, a)
			})
		})
	}
}

func TestAssistantDialectShortAdmissionCancellation(t *testing.T) {
	for _, where := range []string{"before", "queued", "mutex-backoff"} {
		t.Run(where, func(t *testing.T) {
			a := assistantDialectShortFixture(t, false)
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var head *privateDataShortLock
				if where == "before" {
					cancel()
				}
				if where == "queued" {
					var err error
					head, err = a.server.privateDataLockContext(t.Context(), true)
					assistantRunMust(t, err)
					defer head.Close()
				}
				if where == "mutex-backoff" {
					a.writer.mu.Lock()
					defer a.writer.mu.Unlock()
				}
				result := make(chan error, 1)
				go func() {
					g, err := a.acquire(ctx, assistantDialectShortTransaction)
					if g != nil {
						err = errors.Join(err, g.Close())
					}
					result <- err
				}()
				synctest.Wait()
				cancel()
				if err := <-result; !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation admitted custody or lost cancellation", err)
				}
				if head != nil {
					if len(a.server.privateDataTurns.waiters) != 1 {
						t.Fatal("canceled waiter did not withdraw")
					}
					assistantRunMust(t, head.Close())
				}
				if len(a.server.privateDataTurns.waiters) != 0 {
					t.Fatal("cancellation retained a turn")
				}
				if !a.server.writes.TryLock() {
					t.Fatal("cancellation retained writes")
				}
				a.server.writes.Unlock()
				probe, err := a.server.privateDataFileLock(".data.lock", true)
				assistantRunMust(t, err)
				assistantRunMust(t, probe.Close())
			})
		})
	}
}

func TestAssistantDialectShortAdmissionSealHasNoPrivateWait(t *testing.T) {
	a := assistantDialectShortFixture(t, false)
	synctest.Test(t, func(t *testing.T) {
		head, err := a.server.privateDataLockContext(t.Context(), true)
		assistantRunMust(t, err)
		defer head.Close()
		a.writer.mu.Lock()
		result := make(chan *assistantDialectShortCustody, 1)
		failed := make(chan error, 1)
		go func() {
			g, err := a.acquire(t.Context(), assistantDialectShortSeal)
			if err != nil {
				failed <- err
			}
			result <- g
		}()
		synctest.Wait()
		if len(a.server.privateDataTurns.waiters) != 1 {
			t.Fatal("constructor seal queued an unrelated private transaction")
		}
		if !a.execution.canonicalMu.TryLock() {
			t.Fatal("seal backoff retained canonical custody")
		}
		a.execution.canonicalMu.Unlock()
		a.writer.mu.Unlock()
		g := <-result
		if g == nil {
			t.Fatal("seal did not coordinate writer contention", <-failed)
		}
		if g.private != nil || len(a.server.privateDataTurns.waiters) != 1 {
			g.Close()
			t.Fatal("seal acquired private custody")
		}
		assistantRunMust(t, g.Close())
		assistantRunMust(t, head.Close())
		assistantDialectShortReleased(t, a)
	})
}

func TestAssistantDialectShortAdmissionOneOverallDeadline(t *testing.T) {
	a := assistantDialectShortFixture(t, false)
	synctest.Test(t, func(t *testing.T) {
		head, err := a.server.privateDataTurns.acquire(t.Context())
		assistantRunMust(t, err)
		defer head()
		foreign, err := a.server.privateDataFileLock(".data.lock", true)
		assistantRunMust(t, err)
		defer foreign.Close()
		a.execution.canonicalMu.Lock()
		defer a.execution.canonicalMu.Unlock()
		started := time.Now()
		result := make(chan error, 1)
		go func() {
			g, err := a.acquire(t.Context(), assistantDialectShortTransaction)
			if g != nil {
				err = errors.Join(err, g.Close())
			}
			result <- err
		}()
		synctest.Wait()
		time.Sleep(4 * time.Second)
		head()
		synctest.Wait()
		time.Sleep(500 * time.Millisecond)
		assistantRunMust(t, foreign.Close())
		if err := <-result; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("overall deadline did not refuse", err)
		}
		if elapsed := time.Since(started); elapsed != privateDataLockWait {
			t.Fatal("queue, OS and mutex waits did not share one bound", elapsed)
		}
		if len(a.server.privateDataTurns.waiters) != 0 {
			t.Fatal("expired admission retained turn")
		}
		if !a.server.writes.TryLock() {
			t.Fatal("expired admission retained writes")
		}
		a.server.writes.Unlock()
	})
}

func TestAssistantDialectShortAdmissionOwnershipStaysNonblocking(t *testing.T) {
	for _, which := range []string{"writes", "executor", "service", "outer"} {
		t.Run(which, func(t *testing.T) {
			a := assistantDialectShortFixture(t, false)
			synctest.Test(t, func(t *testing.T) {
				kind := assistantDialectShortTransaction
				target := a.server.writes
				switch which {
				case "executor":
					target = &a.lease.mu
				case "service":
					target = &a.service.mu
					kind = assistantDialectShortEnrollment
				case "outer":
					target = &a.owner.desksMu
					kind = assistantDialectShortEnrollment
				}
				target.Lock()
				started := time.Now()
				g, err := a.acquire(t.Context(), kind)
				target.Unlock()
				if g != nil {
					g.Close()
					t.Fatal("ownership conflict admitted")
				}
				if err == nil || time.Since(started) != 0 {
					t.Fatal("ownership boundary waited or did not refuse", err)
				}
				assistantDialectShortReleased(t, a)
			})
		})
	}
}

func TestAssistantDialectShortAdmissionFailureClassAndCleanup(t *testing.T) {
	a := assistantDialectShortFixture(t, false)
	for _, which := range []string{"semantic", "cleanup", "untyped"} {
		t.Run(which, func(t *testing.T) {
			held, err := a.server.privateDataLockContext(t.Context(), true)
			assistantRunMust(t, err)
			var operationErr error
			busy := assistantDialectShortCanonicalBusy
			switch which {
			case "semantic":
				operationErr = errAssistantRunScope
			case "cleanup":
				assistantRunMust(t, held.File.Close())
			case "untyped":
				busy = assistantDialectShortNotBusy
			}
			a.server.writes.Lock()
			g := &assistantDialectShortCustody{private: held, releases: []func(){a.server.writes.Unlock}}
			retry, err := g.withdraw(busy, operationErr)
			if retry || err == nil {
				t.Fatal("semantic, Close or untyped failure was retryable")
			}
			if which == "semantic" && !errors.Is(err, errAssistantRunScope) {
				t.Fatal("semantic refusal was lost", err)
			}
			if which == "cleanup" && !errors.Is(err, os.ErrClosed) {
				t.Fatal("actual descriptor Close failure was lost", err)
			}
			assistantDialectShortReleased(t, a)
		})
	}
}

func TestAssistantDialectShortAdmissionHomeCloseFailureStopsRequeue(t *testing.T) {
	a := assistantDialectShortFixture(t, true)
	if testHookDialectArtifactOwnerStep != nil {
		t.Fatal("home Close regression needs an unused seam")
	}
	injected := errors.New("handwritten home close failure")
	closes := 0
	testHookDialectArtifactOwnerStep = func(stage string) error {
		if stage == "home-registry.close" {
			closes++
			return injected
		}
		return nil
	}
	defer func() { testHookDialectArtifactOwnerStep = nil }()
	a.execution.canonicalMu.Lock()
	g, err := a.acquire(t.Context(), assistantDialectShortEnrollment)
	a.execution.canonicalMu.Unlock()
	if g != nil {
		g.Close()
		t.Fatal("failed home Close admitted custody")
	}
	if !errors.Is(err, injected) || closes != 1 {
		t.Fatal("home Close failure was retried or lost", closes, err)
	}
	assistantDialectShortReleased(t, a)
	if !a.service.mu.TryLock() {
		t.Fatal("home Close failure retained service")
	}
	a.service.mu.Unlock()
	if !a.owner.desksMu.TryLock() {
		t.Fatal("home Close failure retained outer")
	}
	a.owner.desksMu.Unlock()
}

func TestAssistantDialectShortAdmissionChangedAnchorsDuringWait(t *testing.T) {
	for _, which := range []string{"project-root", "work-root", "owner", "location", "home-registry"} {
		t.Run(which, func(t *testing.T) {
			a := assistantDialectShortFixture(t, which == "home-registry")
			synctest.Test(t, func(t *testing.T) {
				head, err := a.server.privateDataLockContext(t.Context(), true)
				assistantRunMust(t, err)
				defer head.Close()
				result := make(chan error, 1)
				go func() {
					g, err := a.acquire(t.Context(), assistantDialectShortEnrollment)
					if err == nil {
						err = a.checkOuter(g.outer)
					}
					if err == nil {
						err = a.checkPrivate(g)
					}
					if g != nil {
						err = errors.Join(err, g.Close())
					}
					result <- err
				}()
				synctest.Wait()
				if len(a.server.privateDataTurns.waiters) != 2 {
					t.Fatal("original enrollment did not actually queue")
				}
				// The original descriptors/anchors remain retained. Only a scratch entry
				// or protected registry/owner field changes; no newer identity is adopted.
				switch which {
				case "project-root", "work-root":
					path := a.projectDir
					if which == "work-root" {
						path = a.configDir
					}
					backup := path + "-original"
					assistantRunMust(t, os.Rename(path, backup))
					assistantRunMust(t, os.Mkdir(path, 0700))
					defer func() { assistantRunMust(t, os.Remove(path)); assistantRunMust(t, os.Rename(backup, path)) }()
				case "owner":
					a.service.mu.Lock()
					a.owner.desksMu.Lock()
					a.server.cfg.parent = &Server{assistant: a.assistant, configDir: a.configDir}
					a.owner.desksMu.Unlock()
					a.service.mu.Unlock()
					defer func() { a.server.cfg.parent = nil }()
				case "location":
					a.server.location.mu.Lock()
					a.server.location.moved = "changed-scratch-location"
					a.server.location.mu.Unlock()
					defer func() { a.server.location.mu.Lock(); a.server.location.moved = ""; a.server.location.mu.Unlock() }()
				case "home-registry":
					a.owner.desksMu.Lock()
					path := filepath.Join(a.configDir, deskHomeRegistryName)
					replacement, err := os.CreateTemp(a.configDir, "short-registry-replacement-")
					assistantRunMust(t, err)
					_, err = replacement.Write(a.registryRaw)
					assistantRunMust(t, err)
					assistantRunMust(t, replacement.Close())
					assistantRunMust(t, os.Rename(replacement.Name(), path))
					a.owner.desksMu.Unlock()
				}
				assistantRunMust(t, head.Close())
				if err := <-result; err == nil {
					t.Fatal("changed original anchor was adopted")
				}
				if len(a.server.privateDataTurns.waiters) != 0 {
					t.Fatal("anchor refusal retained turn")
				}
				if !a.service.mu.TryLock() {
					t.Fatal("anchor refusal retained service")
				}
				a.service.mu.Unlock()
				if !a.owner.desksMu.TryLock() {
					t.Fatal("anchor refusal retained outer custody")
				}
				a.owner.desksMu.Unlock()
			})
		})
	}
}

func TestAssistantDialectShortAdmissionTransferUsesOriginalContext(t *testing.T) {
	a := assistantDialectShortFixture(t, true)
	type key struct{}
	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), key{}, a), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	g, err := a.acquire(ctx, assistantDialectShortEnrollment)
	assistantRunMust(t, err)
	defer g.Close()
	assistantRunMust(t, a.checkOuter(g.outer))
	assistantRunMust(t, a.checkPrivate(g))
	if ctx.Err() != nil || ctx.Value(key{}) != a {
		t.Fatal("admission canceled or replaced original execution context")
	}
	if got, _ := ctx.Deadline(); got != deadline {
		t.Fatal("admission changed original execution deadline")
	}
	if g.outer == nil || g.outer.home == nil {
		t.Fatal("context-transfer regression requires the real original home hold")
	}
	if g.outer.home.ctx != ctx {
		t.Fatal("home retained acquisition deadline instead of original context")
	}
	assistantRunMust(t, g.outer.home.check())
}
