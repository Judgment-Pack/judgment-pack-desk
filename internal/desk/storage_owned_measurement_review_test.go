//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageOwnedRequireCurrentProof(t *testing.T) {
	t.Helper()
	records, err := storageEnrollmentNativeRecords(storageEnrollmentInstalledNativeRecords)
	storageTestMust(t, err)
	if len(records) == 0 {
		t.Skip("requires genuine exact-source installed Record3; missing/skipped is not qualification")
	}
}
func storageOwnedRelease(release chan struct{}) {
	select {
	case <-release:
	default:
		close(release)
	}
}

// Fixed channel waits have no polling/sleep or authority. Defers release every
// entered boundary and join the original method even when an assertion fails.
func storageOwnedWait(t *testing.T, entered <-chan struct{}, done <-chan error, joined *bool) {
	t.Helper()
	select {
	case <-entered:
	case err := <-done:
		*joined = true
		t.Fatalf("original invocation returned before required boundary: %v", err)
	case <-time.After(2 * storageCensusTime):
		t.Fatal("original invocation did not reach required boundary")
	}
}
func storageOwnedJoin(t *testing.T, done <-chan error, joined *bool) error {
	t.Helper()
	select {
	case err := <-done:
		*joined = true
		return err
	case <-time.After(2 * storageCensusTime):
		t.Fatal("original invocation failed to drain")
		return nil
	}
}

// Exercises activateStorageBudgetStartup itself, including a late original
// identity epoch after loadOrEnrollOwner, forServer, and the outer cleanup.
func TestStorageOwnedMeasurementQualifiedOuterStartup(t *testing.T) {
	storageOwnedRequireCurrentProof(t)
	for _, name := range []string{"late-identity-success", "late-setup-refusal", "for-server-original-authority-close", "caller-canceled-after-capture"} {
		t.Run(name, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			storageTestMust(t, os.Chmod(cfg.ProjectDir, 0700))
			storageTestMust(t, os.Mkdir(filepath.Join(cfg.ProjectDir, startupIdentityDir), 0700))
			if _, err := os.Lstat(filepath.Join(cfg.ProjectDir, startupIdentityDir, startupIdentityName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("fixture is not original positive identity absence", err)
			}
			s, unused, closeOriginal := ownerCrashConstructionStart(t, cfg, func(err error) {
				if err != nil && !errors.Is(err, errStorageInfrastructure) && !errors.Is(err, storagebudget.ErrAuthority) && !errors.Is(err, context.Canceled) {
					t.Error("original cleanup", err)
				}
			}, false)
			if unused != nil || s.storageConstruction != nil {
				t.Fatal("fixture prepared construction before actual activation")
			}
			loaded, releaseLoaded, completed, releaseCompleted := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			s.storageInfrastructure.fault = &storageInfrastructureFault{name: "storage-startup", phase: "owner-loaded", entered: loaded, release: releaseLoaded, pauseOnly: true}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			joined := false
			defer func() {
				storageOwnedRelease(releaseLoaded)
				storageOwnedRelease(releaseCompleted)
				if !joined {
					_ = storageOwnedJoin(t, done, &joined)
				}
				closeOriginal()
			}()
			go func() { done <- s.activateStorageBudgetStartup(ctx) }()
			storageOwnedWait(t, loaded, done, &joined)
			c := s.storageConstruction
			if c == nil || c.startupIdentityCompleted {
				t.Fatal("did not force real post-load identity completion")
			}
			c.owner.mu.Lock()
			first := c.owner.measurement.startup
			published := c.owner.measurement.last
			c.owner.mu.Unlock()
			if first == nil || !first.collected || published != nil {
				t.Fatal("inner census was published before activation's late work")
			}
			firstAttempt := first.value.Attempt
			// The original step took its fault before signaling; the channel handoff
			// orders this next closed step without racing a production fault read.
			s.storageInfrastructure.fault = &storageInfrastructureFault{name: "storage-startup", phase: "late-completion", entered: completed, release: releaseCompleted, pauseOnly: name != "late-setup-refusal"}
			storageOwnedRelease(releaseLoaded)
			storageOwnedWait(t, completed, done, &joined)
			if !c.startupIdentityCompleted {
				t.Fatal("real later identity/local branch did not complete")
			}
			c.owner.mu.Lock()
			last := c.owner.measurement.startup
			published = c.owner.measurement.last
			c.owner.mu.Unlock()
			if last == nil || !last.collected || last.value.Attempt <= firstAttempt || published != nil || last.value.Epoch == 0 {
				t.Fatal("latest actual identity capture was lost or finalized early")
			}
			completedAt, epoch := last.value.Completed, last.value.Epoch
			switch name {
			case "for-server-original-authority-close":
				storageTestMust(t, c.mutation.Close())
			case "caller-canceled-after-capture":
				cancel()
			}
			storageOwnedRelease(releaseCompleted)
			err := storageOwnedJoin(t, done, &joined)
			switch name {
			case "late-identity-success":
				if err != nil || s.storageEnrollment == nil {
					t.Fatal("actual startup did not finish", err)
				}
			case "late-setup-refusal":
				if !errors.Is(err, errStorageInfrastructure) {
					t.Fatal("original injected setup error changed", err)
				}
			case "for-server-original-authority-close":
				if !errors.Is(err, storagebudget.ErrAuthority) {
					t.Fatal("original forServer refusal changed", err)
				}
			// The method's original return/cancellation behavior is preserved; the
			// canceled caller must never publish diagnostic success even if it returned nil.
			case "caller-canceled-after-capture":
			}
			measured, _ := c.owner.ownedMeasurement()
			c.owner.mu.Lock()
			pending := c.owner.measurement.startup
			c.owner.mu.Unlock()
			if pending != nil {
				t.Fatal("outer activation did not consume pending result")
			}
			if name == "late-identity-success" {
				if measured == nil || measured.Completed != completedAt || measured.Epoch != epoch {
					t.Fatal("outer success did not retain actual latest time/epoch")
				}
			} else if measured != nil {
				t.Fatal("later refusal/cancellation published an inner success", name)
			}
		})
	}
}

func storageOwnedForceStatusRefresh(t *testing.T, s *Server) storageStatusResponse {
	t.Helper()
	cache, err := s.storageConstruction.owner.storageStatusCache()
	storageTestMust(t, err)
	cache.mu.Lock()
	cache.attemptedAt = time.Time{}
	cache.mu.Unlock() // test-only rate clock; no measurement time rewrite
	return s.readStorageStatus(t.Context())
}

// Original writer persists J Prepared before its unique reserve-before seam.
// Q is genuinely refreshed while that writer is active, and after its injected
// pre-Reserve refusal has drained without a Q ticket/revision change.
func TestStorageOwnedMeasurementQualifiedWriterBeforeReserve(t *testing.T) {
	storageOwnedRequireCurrentProof(t)
	s := ownerActualNew(t, ownerActualConfig(t))
	storageTestMust(t, s.prepareStoragePolicy(t.Context()))
	o := s.storageConstruction.owner
	before := storageOwnedForceStatusRefresh(t, s)
	if before.Snapshot == nil || before.Snapshot.Measurement.Freshness != "fresh" {
		t.Fatal("fixture lacks genuine idle exact enrollment boundary", before.Availability)
	}
	originalAt, originalGeneration := *before.Snapshot.Measurement.At, before.Snapshot.Measurement.Generation
	var root *storageRootBinding
	for _, candidate := range o.orderedRoots() {
		if candidate.configControl {
			root = candidate
		}
	}
	if root == nil {
		t.Fatal("original config binding absent")
	}
	entered, release := make(chan struct{}), make(chan struct{})
	o.fault = &storageBudgetFault{Name: "reserve", Phase: "before", Entered: entered, Release: release}
	done := make(chan error, 1)
	joined := false
	defer func() {
		storageOwnedRelease(release)
		if !joined {
			_ = storageOwnedJoin(t, done, &joined)
		}
	}()
	plan := storageSingleFilePlan{Root: root, Target: "measurement-before-reserve.json", Stage: ".measurement-before-reserve.stage", Workflow: "measurement-admission", Operation: "original-before-reserve", ExpectedAbsent: true, Bytes: []byte("unchanged original finite request"), ObjectLimit: 4096}
	go func() {
		writer, err := o.admitSingleFile(t.Context(), plan)
		if writer != nil {
			err = errors.Join(err, writer.Close())
		}
		done <- err
	}()
	storageOwnedWait(t, entered, done, &joined)
	// The fixed journal already changed, but Q has no Reserve ticket. Do not
	// inspect/lock the journal under this held original withControl invocation.
	during := storageOwnedForceStatusRefresh(t, s)
	assertSameQ := func(out storageStatusResponse) {
		t.Helper()
		if out.Snapshot == nil || out.Snapshot.PolicyRevision != before.Snapshot.PolicyRevision || out.Snapshot.Epoch != before.Snapshot.Epoch || out.Snapshot.Recovery.ActiveTicketCount+out.Snapshot.Recovery.UncertainTicketCount+out.Snapshot.Recovery.TerminalAwaitingCheckpointCount != 0 {
			t.Fatal("required matching-Q/no-ticket pre-Reserve scenario was not observed", out.Availability)
		}
		if out.Snapshot.Measurement.Freshness != "stale" || out.Snapshot.Measurement.At == nil || *out.Snapshot.Measurement.At != originalAt || out.Snapshot.Measurement.Generation != originalGeneration {
			t.Fatal("known possible write refreshed/relabelled original measurement", out.Snapshot.Measurement)
		}
	}
	assertSameQ(during)
	if during.Snapshot.ObservationSequence == before.Snapshot.ObservationSequence {
		t.Fatal("paused writer check reused Q cache")
	}
	storageOwnedRelease(release)
	if err := storageOwnedJoin(t, done, &joined); !errors.Is(err, errStorageOwnerRecovery) {
		t.Fatal("pre-Reserve injected original refusal changed", err)
	}
	after := storageOwnedForceStatusRefresh(t, s)
	assertSameQ(after)
	if after.Snapshot.ObservationSequence == during.Snapshot.ObservationSequence {
		t.Fatal("completed activity check reused Q cache")
	}
}
