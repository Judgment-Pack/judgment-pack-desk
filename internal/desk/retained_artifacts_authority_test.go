//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
)

type artifactAuthorityTestGuard struct {
	check func(context.Context) error
	close func() error
}

func (g *artifactAuthorityTestGuard) Check(ctx context.Context) error {
	if g.check != nil {
		return g.check(ctx)
	}
	return ctx.Err()
}
func (g *artifactAuthorityTestGuard) Close() error { return g.close() }

// Explicit delegation is required; embedding the old repository interface does
// not accidentally grant the new owner capability to an injected wrapper.
type artifactAuthorityTestAdapter struct {
	retainedArtifactRepository
	owner guardedRetainedArtifactRepository
	calls int
}

func (a *artifactAuthorityTestAdapter) PutGuarded(ctx context.Context, raw []byte, acquire func(context.Context) (retainedArtifactGuard, error)) (retainedArtifactRef, error) {
	a.calls++
	return a.owner.PutGuarded(ctx, raw, acquire)
}

type artifactLegacyAuthorityTestAdapter struct{ retainedArtifactRepository }

func (a artifactLegacyAuthorityTestAdapter) WithAuthority(func(context.Context) error) retainedArtifactRepository {
	return a
}

func artifactAuthorityTestRoot(t *testing.T, s *Server) *os.Root {
	t.Helper()
	root, err := s.openRetainedArtifacts(true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}
func artifactAuthorityTestLock(t *testing.T, root *os.Root) *os.File {
	t.Helper()
	file, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	if err = lockPrivateData(file, true); err != nil {
		file.Close()
		t.Fatal(err)
	}
	return file
}
func assertGuardedArtifactAbsent(t *testing.T, root *os.Root, raw []byte) {
	t.Helper()
	if _, err := root.Lstat(artifactReference(raw).SHA256); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused publication left an object", err)
	}
}

func TestGuardedArtifactSetupFreshGuardAndImmutableCopy(t *testing.T) {
	s, _ := newTestServer(t, false)
	original := []byte("exact guarded bytes")
	want := bytes.Clone(original)
	active, acquired, closed := false, 0, 0
	owner := fileRetainedArtifactRepository{open: func(create bool) (*os.Root, error) {
		if create && !active {
			t.Fatal("bookkeeping created outside guard")
		}
		return s.openRetainedArtifacts(create)
	}}
	acquire := func(ctx context.Context) (retainedArtifactGuard, error) {
		if active || acquired != closed {
			t.Fatal("guard reacquired before release")
		}
		acquired++
		active = true
		if acquired == 1 {
			original[0] = 'X' // The owner must already have copied raw.
		} else {
			root := artifactAuthorityTestRoot(t, s)
			// Fresh admission runs after drainage has released actual exclusion.
			lock := artifactAuthorityTestLock(t, root)
			lock.Close()
		}
		return &artifactAuthorityTestGuard{check: func(context.Context) error {
			if !active {
				t.Fatal("check after release")
			}
			return nil
		}, close: func() error {
			if !active {
				t.Fatal("guard closed twice")
			}
			active = false
			closed++
			// Artifact exclusion must be gone before guard release.
			root := artifactAuthorityTestRoot(t, s)
			lock := artifactAuthorityTestLock(t, root)
			lock.Close()
			return nil
		}}, nil
	}
	ref, err := owner.PutGuarded(t.Context(), original, acquire)
	if err != nil || ref != artifactReference(want) || active || acquired != 2 || closed != 2 {
		t.Fatal("setup/effect lifecycle", ref, err, acquired, closed, active)
	}
	got, err := s.artifactRepository().Get(t.Context(), ref)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("publication did not retain immutable input", err)
	}
}

func TestGuardedArtifactQueuedAuthorityChange(t *testing.T) {
	// These names describe independent guard facts. Concrete Desk source/phase,
	// generation and lease binding is tested by the full D5 guard owner.
	for _, fact := range []string{"phase", "request", "deletion", "session", "policy", "generation", "lease"} {
		for _, injected := range []bool{false, true} {
			name := fact + "/file"
			if injected {
				name = fact + "/injected"
			}
			t.Run(name, func(t *testing.T) {
				s, _ := newTestServer(t, false)
				root := artifactAuthorityTestRoot(t, s)
				if injected {
					fileOwner := s.guardedArtifactRepository()
					s.retainedArtifacts = &artifactAuthorityTestAdapter{retainedArtifactRepository: fileOwner, owner: fileOwner}
				}
				synctest.Test(t, func(t *testing.T) {
					lock := artifactAuthorityTestLock(t, root)
					defer lock.Close()
					var host sync.Mutex
					valid, calls := true, 0
					refused := errors.New("current " + fact + " changed")
					raw := []byte("queued guarded object")
					result := make(chan error, 1)
					go func() {
						ref, err := s.guardedArtifactRepository().PutGuarded(t.Context(), raw, func(context.Context) (retainedArtifactGuard, error) {
							calls++
							if !host.TryLock() {
								return nil, errors.New("host busy")
							}
							if !valid {
								host.Unlock()
								return nil, refused
							}
							return &artifactAuthorityTestGuard{close: func() error { host.Unlock(); return nil }}, nil
						})
						if ref != (retainedArtifactRef{}) {
							t.Error("stale authority acknowledged")
						}
						result <- err
					}()
					synctest.Wait()
					if calls != 0 || !host.TryLock() {
						t.Fatal("host guard retained while queued", calls)
					}
					valid = false // This change completes before queue release.
					host.Unlock()
					lock.Close()
					if err := <-result; !errors.Is(err, refused) || calls != 1 {
						t.Fatal("queued currentness was not reread exactly once", err, calls)
					}
					assertGuardedArtifactAbsent(t, root, raw)
				})
			})
		}
	}
}

func TestGuardedArtifactFinalContentionDoesNotWaitOrRetry(t *testing.T) {
	s, _ := newTestServer(t, false)
	root := artifactAuthorityTestRoot(t, s)
	var competitor *os.File
	defer func() {
		if competitor != nil {
			competitor.Close()
		}
	}()
	calls, closed := 0, 0
	raw := []byte("loses the drainage gap")
	ref, err := s.guardedArtifactRepository().PutGuarded(t.Context(), raw, func(context.Context) (retainedArtifactGuard, error) {
		calls++
		// No queue exclusion is held; another publisher wins this exact gap.
		competitor = artifactAuthorityTestLock(t, root)
		return &artifactAuthorityTestGuard{close: func() error { closed++; return nil }}, nil
	})
	if !errors.Is(err, errRetainedArtifactBusy) || ref != (retainedArtifactRef{}) || calls != 1 || closed != 1 {
		t.Fatal("final contention waited, retried or leaked guard", ref, err, calls, closed)
	}
	assertGuardedArtifactAbsent(t, root, raw)
}

func TestGuardedArtifactRefusesUnsupportedAndLegacyOwners(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		s, _ := newTestServer(t, false)
		memory := newMemoryArtifacts()
		s.retainedArtifacts = memory
		if legacy {
			s.retainedArtifacts = artifactLegacyAuthorityTestAdapter{memory}
		}
		calls := 0
		ref, err := s.guardedArtifactRepository().PutGuarded(t.Context(), []byte("unavailable"), func(context.Context) (retainedArtifactGuard, error) {
			calls++
			return nil, nil
		})
		if !errors.Is(err, errRetainedArtifactAuthorityUnavailable) || ref != (retainedArtifactRef{}) || calls != 0 || len(memory.data) != 0 {
			t.Fatal("unsupported owner provided guarded effect", ref, err, calls)
		}
		assertNoArtifactFiles(t, s)
	}
}

func TestGuardedArtifactGuardAcquisitionFailureAndCancellation(t *testing.T) {
	for _, mode := range []string{"nil", "error-with-guard", "canceled-in-acquire", "check-refused"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := newTestServer(t, false)
			root := artifactAuthorityTestRoot(t, s)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			failure := errors.New("guard unavailable")
			calls, closed := 0, 0
			raw := []byte("not admitted")
			ref, err := s.guardedArtifactRepository().PutGuarded(ctx, raw, func(context.Context) (retainedArtifactGuard, error) {
				calls++
				guard := &artifactAuthorityTestGuard{close: func() error { closed++; return nil }}
				switch mode {
				case "nil":
					return nil, nil
				case "error-with-guard":
					return guard, failure
				case "canceled-in-acquire":
					cancel()
				case "check-refused":
					guard.check = func(context.Context) error { return failure }
				}
				return guard, nil
			})
			wantClosed := 1
			if mode == "nil" {
				wantClosed = 0
			}
			if err == nil || ref != (retainedArtifactRef{}) || calls != 1 || closed != wantClosed {
				t.Fatal("guard acquisition retried, acknowledged or leaked", ref, err, calls, closed)
			}
			assertGuardedArtifactAbsent(t, root, raw)
			lock := artifactAuthorityTestLock(t, root)
			lock.Close()
		})
	}
}

func TestGuardedArtifactUnknownReadbackCancellationAndClose(t *testing.T) {
	for _, mode := range []string{"readback", "cancel-after-publication", "guard-close"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := newTestServer(t, false)
			root := artifactAuthorityTestRoot(t, s)
			raw := []byte("exact object despite uncertain acknowledgment")
			expected := artifactReference(raw)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			failure := errors.New("uncertain owner completion")
			reads, calls, closed := 0, 0, 0
			testHookPrivateRead = func(name string) error {
				if name == expected.SHA256 {
					reads++
					if mode == "readback" {
						return failure
					}
					if mode == "cancel-after-publication" {
						cancel()
					}
				}
				return nil
			}
			t.Cleanup(func() { testHookPrivateRead = nil })
			ref, err := s.guardedArtifactRepository().PutGuarded(ctx, raw, func(context.Context) (retainedArtifactGuard, error) {
				calls++
				return &artifactAuthorityTestGuard{close: func() error {
					closed++
					lock := artifactAuthorityTestLock(t, root)
					lock.Close()
					if mode == "guard-close" {
						return failure
					}
					return nil
				}}, nil
			})
			if err == nil || ref != (retainedArtifactRef{}) || calls != 1 || closed != 1 || reads != 1 {
				t.Fatal("uncertain effect acknowledged/retried or leaked", ref, err, calls, closed, reads)
			}
			testHookPrivateRead = nil
			got, readErr := s.artifactRepository().Get(t.Context(), expected)
			if readErr != nil || !bytes.Equal(got, raw) {
				t.Fatal("uncertainty destroyed exact published evidence", readErr)
			}
		})
	}
}

func TestGuardedArtifactExistingContentAndOldBehavior(t *testing.T) {
	s, _ := newTestServer(t, false)
	raw := []byte("existing exact content")
	ref, err := s.artifactRepository().Put(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.projectDir, retainedArtifactDirectory, ref.SHA256)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	calls, closed := 0, 0
	acquire := func(context.Context) (retainedArtifactGuard, error) {
		calls++
		return &artifactAuthorityTestGuard{close: func() error { closed++; return nil }}, nil
	}
	if got, err := s.guardedArtifactRepository().PutGuarded(t.Context(), raw, acquire); err != nil || got != ref {
		t.Fatal("exact repeated content refused", got, err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || calls != 1 || closed != 1 {
		t.Fatal("repeated content replaced or guard leaked", err, calls, closed)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.guardedArtifactRepository().PutGuarded(t.Context(), raw, acquire); err == nil || got != (retainedArtifactRef{}) {
		t.Fatal("guarded put repaired corruption", got, err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "corrupt" {
		t.Fatal("corruption overwritten", err)
	}
	// Old authority binding still enforces its existing callback path.
	refused := errors.New("old callback refused")
	old := s.artifactRepositoryWithAuthority(func(context.Context) error { return refused })
	if _, err := old.Put(t.Context(), []byte("old call")); !errors.Is(err, refused) {
		t.Fatal("old authority behavior changed", err)
	}
	if _, err := old.(guardedRetainedArtifactRepository).PutGuarded(t.Context(), raw, acquire); !errors.Is(err, errRetainedArtifactAuthorityUnavailable) {
		t.Fatal("guarded path bypassed an older bound authority", err)
	}
}

func TestGuardedArtifactCanceledQueueAndInputRefusalDoNotAcquire(t *testing.T) {
	t.Run("queued cancellation", func(t *testing.T) {
		s, _ := newTestServer(t, false)
		root := artifactAuthorityTestRoot(t, s)
		synctest.Test(t, func(t *testing.T) {
			lock := artifactAuthorityTestLock(t, root)
			defer lock.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			raw := []byte("cancel while queued")
			result := make(chan error, 1)
			go func() {
				_, err := s.guardedArtifactRepository().PutGuarded(ctx, raw, func(context.Context) (retainedArtifactGuard, error) {
					calls++
					return nil, nil
				})
				result <- err
			}()
			synctest.Wait()
			cancel()
			if err := <-result; !errors.Is(err, context.Canceled) || calls != 0 {
				t.Fatal("queued cancellation acquired host custody", err, calls)
			}
			assertGuardedArtifactAbsent(t, root, raw)
		})
	})
	for _, mode := range []string{"canceled", "oversized", "missing-acquire"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := newTestServer(t, false)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			acquire := func(context.Context) (retainedArtifactGuard, error) { calls++; return nil, nil }
			raw := []byte("refused input")
			switch mode {
			case "canceled":
				cancel()
			case "oversized":
				raw = make([]byte, retainedArtifactByteLimit+1)
			case "missing-acquire":
				acquire = nil
			}
			if ref, err := s.guardedArtifactRepository().PutGuarded(ctx, raw, acquire); err == nil || ref != (retainedArtifactRef{}) || calls != 0 {
				t.Fatal("invalid call entered setup/admission", ref, err, calls)
			}
			assertNoArtifactFiles(t, s)
		})
	}
}

func TestGuardedArtifactRechecksBeforeRenameAndExistingAcknowledgment(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "before rename", true: "existing acknowledgment"}[existing], func(t *testing.T) {
			s, _ := newTestServer(t, false)
			root := artifactAuthorityTestRoot(t, s)
			raw := []byte("guarded final currentness")
			if existing {
				if _, err := s.artifactRepository().Put(t.Context(), raw); err != nil {
					t.Fatal(err)
				}
			}
			refused := errors.New("authority revoked before publication or acknowledgment")
			checkedRead, closed := false, 0
			testHookPrivateRead = func(name string) error {
				if name == artifactReference(raw).SHA256 {
					checkedRead = true
				}
				return nil
			}
			t.Cleanup(func() { testHookPrivateRead = nil })
			ref, err := s.guardedArtifactRepository().PutGuarded(t.Context(), raw, func(context.Context) (retainedArtifactGuard, error) {
				return &artifactAuthorityTestGuard{check: func(context.Context) error {
					if existing && checkedRead {
						return refused
					}
					// Observe actual completed staging rather than callback counts.
					dir, err := root.Open(".")
					if err != nil {
						return err
					}
					entries, err := dir.ReadDir(-1)
					closeErr := dir.Close()
					if err != nil || closeErr != nil {
						return errors.Join(err, closeErr)
					}
					for _, entry := range entries {
						if len(entry.Name()) > len(".data-") && entry.Name()[:len(".data-")] == ".data-" {
							info, err := root.Stat(entry.Name())
							if err != nil {
								return err
							}
							if info.Size() == int64(len(raw)) {
								return refused
							}
						}
					}
					return nil
				}, close: func() error { closed++; return nil }}, nil
			})
			if !errors.Is(err, refused) || ref != (retainedArtifactRef{}) || closed != 1 {
				t.Fatal("late authority change acknowledged", ref, err, closed)
			}
			if !existing {
				assertGuardedArtifactAbsent(t, root, raw)
				entries, err := os.ReadDir(filepath.Join(s.projectDir, retainedArtifactDirectory))
				if err != nil || len(entries) != 1 || entries[0].Name() != ".gitignore" {
					t.Fatal("refused staged write left temporary content", entries, err)
				}
			}
		})
	}
}
