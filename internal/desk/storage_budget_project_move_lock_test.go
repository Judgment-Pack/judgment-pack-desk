package desk

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Actual host paths: pure self/tuple checks cannot establish that ordinary
// lazy enrollment survives or that registered custody acquires its real lock.
func TestStorageMovePrivateLockOriginal(t *testing.T) {
	for _, name := range []string{"ordinary-lazy", "registered-original", "stale-confirmation"} {
		t.Run(name, func(t *testing.T) {
			project := t.TempDir()
			storageTestMust(t, os.Mkdir(filepath.Join(project, startupIdentityDir), 0700))
			privateFixture(t, filepath.Join(project, startupIdentityDir, startupIdentityName), identityLine(strings.Repeat("f", 64), project, ""))
			s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: standInRuntime(t), Token: testToken, Logger: log.New(&bytes.Buffer{}, "", 0), DeskConfigDir: t.TempDir()})
			t.Cleanup(func() {
				ts.Close()
				if err := s.Close(); err != nil {
					t.Error(err)
				}
			})
			if name == "ordinary-lazy" {
				c := s.storageConstruction
				c.ref.cell.mu.Lock()
				before := c.ref.cell.enrollmentEndpoint
				c.ref.cell.mu.Unlock()
				if before != nil {
					t.Fatal("fixture already enrolled history")
				}
				lock, err := s.privateDataLockContext(t.Context(), true)
				storageTestMust(t, err)
				storageTestMust(t, lock.Close())
				c.ref.cell.mu.Lock()
				after := c.ref.cell.enrollmentEndpoint
				c.ref.cell.mu.Unlock()
				if after == nil || !c.owner.admissionOpen() {
					t.Fatal("ordinary lock skipped lazy enrollment or failed admission")
				}
				return
			}
			if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
				t.Fatal("initial history", code)
			}
			before, err := s.storageConstruction.ref.cell.q.InspectAction(t.Context())
			storageTestMust(t, err)
			if name == "stale-confirmation" {
				destination := filepath.Join(t.TempDir(), "unmoved")
				err := s.confirmProjectMove(t.Context(), projectMoveRequest{Destination: destination, Token: "invalid"})
				if err == nil {
					t.Fatal("stale confirmation accepted")
				}
				after, err := s.storageConstruction.ref.cell.q.InspectAction(t.Context())
				storageTestMust(t, err)
				if !reflect.DeepEqual(before, after) {
					t.Fatal("stale confirmation published prerequisite Q work")
				}
				if _, err = os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("stale confirmation moved project", err)
				}
				if _, found, err := readProjectMove(s.assistant.root); err != nil || found {
					t.Fatal("stale confirmation published intent", err)
				}
				return
			}
			var original *storageMoveOperation
			err = s.withDialectArtifactProjectMoveHostOwned(t.Context(), func(_ *os.File, _ *dialectArtifactSelectedProviderPins, lease *storageCreatorLease) error {
				if lease == nil || lease.move == nil {
					return storagebudget.ErrAuthority
				}
				m := lease.move
				original = m
				// The ordinary retain is still forbidden during enrollment.
				// Registered acquisition must not invoke that path a second time.
				ordinary, e := s.privateDataLockContext(t.Context(), true)
				if ordinary != nil {
					_ = ordinary.Close()
					t.Fatal("ordinary lock bypassed registered enrollment")
				}
				if !errors.Is(e, storagebudget.ErrBusy) {
					t.Fatal("ordinary retain guard changed", e)
				}
				lock, e := m.privateDataLock(t.Context(), s)
				if e != nil {
					return e
				}
				if lock.turn == nil || lock.turn.firstUse != (privateDataFirstUseKey{}) {
					_ = lock.Close()
					t.Fatal("registered lock is not the original settings turn")
				}
				if e = lock.Close(); e != nil {
					return e
				}
				foreign := &Server{}
				if lock, e = m.privateDataLock(t.Context(), foreign); lock != nil || e != storagebudget.ErrAuthority {
					if lock != nil {
						_ = lock.Close()
					}
					t.Fatal("foreign caller accepted")
				}
				copied := *m
				copied.self = &copied
				if lock, e = copied.privateDataLock(t.Context(), s); lock != nil || e != storagebudget.ErrAuthority {
					if lock != nil {
						_ = lock.Close()
					}
					t.Fatal("unregistered operation accepted")
				}
				e = func() error {
					saved := m.lease
					defer func() { m.lease = saved }()
					m.lease = &storageCreatorLease{}
					held, failure := m.privateDataLock(t.Context(), s)
					if held != nil {
						_ = held.Close()
						return errors.New("mismatched lease acquired custody")
					}
					return failure
				}()
				if e != storagebudget.ErrAuthority {
					t.Fatal("mismatched lease accepted", e)
				}
				return nil
			})
			storageTestMust(t, err)
			if original == nil || !s.storageConstruction.owner.admissionOpen() {
				t.Fatal("original epoch did not drain")
			}
			held, err := original.privateDataLock(t.Context(), s)
			if held != nil {
				_ = held.Close()
				t.Fatal("stale epoch acquired custody")
			}
			if err == nil {
				t.Fatal("stale epoch accepted")
			}
			after, err := s.storageConstruction.ref.cell.q.InspectAction(t.Context())
			storageTestMust(t, err)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("lock acquisition repeated Q work")
			}
		})
	}
}
