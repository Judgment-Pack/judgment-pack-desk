package desk

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// This injected owner keeps authority inside its real file adapter; it does not
// replace server-wide repositories or rely on a callback outside the queue.
type assistantMappingArtifactOwner struct {
	base      retainedArtifactRepository
	authorize func(context.Context) error
	entered   chan<- struct{}
	fail      bool
}

func (r assistantMappingArtifactOwner) WithAuthority(authorize func(context.Context) error) retainedArtifactRepository {
	r.authorize = authorize
	if owner, ok := r.base.(authorityRetainedArtifactRepository); ok {
		r.base = owner.WithAuthority(authorize)
	} else {
		r.base = unavailableAuthorityArtifactRepository{}
	}
	return r
}
func (r assistantMappingArtifactOwner) Get(ctx context.Context, ref retainedArtifactRef) ([]byte, error) {
	return r.base.Get(ctx, ref)
}
func (r assistantMappingArtifactOwner) Put(ctx context.Context, raw []byte) (retainedArtifactRef, error) {
	ref, err := r.base.Put(ctx, raw)
	if r.entered != nil {
		select {
		case r.entered <- struct{}{}:
		default:
		}
	}
	if err == nil && r.fail {
		return retainedArtifactRef{}, errors.New("fixture lost acknowledgement")
	}
	return ref, err
}
func assistantMappingAwait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("owner boundary not reached")
	}
}
func assistantMappingBlocked(t *testing.T, ch <-chan error) {
	t.Helper()
	select {
	case err := <-ch:
		t.Fatal("owning queue bypassed", err)
	case <-time.After(30 * time.Millisecond):
	}
}
func assistantMappingDrained(t *testing.T, ch <-chan error) {
	t.Helper()
	select {
	case err := <-ch:
		if err == nil {
			t.Fatal("revoked queued mutation acknowledged")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued owner did not drain")
	}
}
func TestAssistantDialectMappingOwnerQueuedAuthority(t *testing.T) {
	for _, injected := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "injected"}[injected], func(t *testing.T) {
			s, _ := newTestServer(t, false)
			root, err := s.openRetainedArtifacts(true)
			assistantRunMust(t, err)
			defer root.Close()
			lock, err := root.Open(".")
			assistantRunMust(t, err)
			defer lock.Close()
			assistantRunMust(t, waitPrivateDataLock(t.Context(), lock, true))
			var revoked atomic.Bool
			entered := make(chan struct{}, 1)
			auth := func(ctx context.Context) error {
				if revoked.Load() {
					return errAssistantRunScope
				}
				return ctx.Err()
			}
			base := fileRetainedArtifactRepository{open: func(create bool) (*os.Root, error) {
				select {
				case entered <- struct{}{}:
				default:
				}
				return s.openRetainedArtifacts(create)
			}}
			var repo retainedArtifactRepository
			if injected {
				s.retainedArtifacts = assistantMappingArtifactOwner{base: base}
				repo = s.artifactRepositoryWithAuthority(auth)
			} else {
				repo = base.WithAuthority(auth)
			}
			raw := []byte("queued immutable mapping artifact")
			done := make(chan error, 1)
			go func() { _, err := repo.Put(t.Context(), raw); done <- err }()
			assistantMappingAwait(t, entered)
			assistantMappingBlocked(t, done)
			revoked.Store(true)
			assistantRunMust(t, lock.Close())
			assistantMappingDrained(t, done)
			if _, err = root.Lstat(artifactReference(raw).SHA256); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("revoked artifact was retained", err)
			}
		})
	}
	t.Run("review-pointer", func(t *testing.T) {
		s, _ := newTestServer(t, false)
		ref, err := s.artifactRepository().Put(t.Context(), []byte("candidate"))
		assistantRunMust(t, err)
		old := artifactReview{Version: 1, Target: "mapping-test", StorageRevision: digestOf([]byte("source")), Candidate: ref, ProposedVersion: "1.0.0", Impact: artifactImpact{"initial", "Review"}, Dependencies: []retainedArtifactRef{}, Preview: []byte(`{}`)}
		first, err := s.retainArtifactReview(t.Context(), old)
		assistantRunMust(t, err)
		root, _, err := openOwnFolder(s.root, []string{".desk-private", "artifact-reviews"}, false, "artifact reviews")
		assistantRunMust(t, err)
		defer root.Close()
		lock, err := root.Open(".")
		assistantRunMust(t, err)
		defer lock.Close()
		assistantRunMust(t, waitPrivateDataLock(t.Context(), lock, true))
		var revoked atomic.Bool
		entered := make(chan struct{}, 1)
		s.retainedArtifacts = assistantMappingArtifactOwner{base: fileRetainedArtifactRepository{open: s.openRetainedArtifacts}, entered: entered}
		auth := func(context.Context) error {
			if revoked.Load() {
				return errAssistantRunScope
			}
			return nil
		}
		next := old
		next.ProposedVersion = "1.0.1"
		done := make(chan error, 1)
		go func() { _, err := s.retainArtifactReviewWithAuthority(t.Context(), next, auth); done <- err }()
		assistantMappingAwait(t, entered)
		assistantMappingBlocked(t, done)
		revoked.Store(true)
		assistantRunMust(t, lock.Close())
		assistantMappingDrained(t, done)
		latest, err := s.latestArtifactReview(t.Context(), old.Target)
		assistantRunMust(t, err)
		if latest == nil || latest.Reference != first.Reference {
			t.Fatal("revoked review replaced pointer")
		}
	})
	t.Run("publication-receipt", func(t *testing.T) {
		s, _ := newTestServer(t, false)
		s.cfg.JpackBin = assistantMappingBinary(t)
		saved := assistantMappingSeed(t, s)
		answer := checkAndPublishDialect(t, s, saved, "asserted", "1.0.0")
		tx, err := s.openPublicationTransaction(t.Context())
		assistantRunMust(t, err)
		defer tx.close()
		var revoked atomic.Bool
		entered := make(chan struct{}, 1)
		auth := func(context.Context) error {
			select {
			case entered <- struct{}{}:
			default:
			}
			if revoked.Load() {
				return errAssistantRunScope
			}
			return nil
		}
		request := digestOf([]byte("queued-publication-receipt"))
		done := make(chan error, 1)
		go func() {
			done <- s.retainPublicationReceiptWithAuthority(t.Context(), request, answer.Publication.ID, auth)
		}()
		assistantMappingAwait(t, entered)
		assistantMappingBlocked(t, done)
		revoked.Store(true)
		tx.close()
		assistantMappingDrained(t, done)
		ledger, err := s.readDialectPublicationLedger()
		assistantRunMust(t, err)
		if _, ok := ledger.Receipts[request]; ok {
			t.Fatal("revoked receipt was retained")
		}
	})
}
