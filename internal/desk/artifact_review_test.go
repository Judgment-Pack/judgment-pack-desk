package desk

import (
	"context"
	"os"
	"testing"
)

func TestArtifactReviewRetention(t *testing.T) {
	for _, memory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "memory"}[memory], func(t *testing.T) {
			s, _ := newTestServer(t, false)
			if memory {
				s.retainedArtifacts = newMemoryArtifacts()
			}
			ctx := context.Background()
			raw := []byte("{\"exact\":1.000}")
			ref, err := s.artifactRepository().Put(ctx, raw)
			if err != nil {
				t.Fatal(err)
			}
			review := artifactReview{Version: 1, Target: "urn:review:test", StorageRevision: digestOf([]byte("base")), Candidate: ref, ProposedVersion: "1.0.0", Impact: artifactImpact{"initial", "Reviewed"}, Dependencies: []retainedArtifactRef{}, Preview: []byte("{}")}
			first, err := s.retainArtifactReview(ctx, review)
			if err != nil {
				t.Fatal(err)
			}
			review.Impact.Rationale = "Changed judgment"
			second, err := s.retainArtifactReview(ctx, review)
			if err != nil || first.Reference == second.Reference {
				t.Fatal("review changed in place", err)
			}
			prior, err := s.artifactReview(ctx, first.Reference)
			if err != nil || prior.Review.Impact.Rationale != "Reviewed" {
				t.Fatal("old review lost", err)
			}
			latest, err := s.latestArtifactReview(ctx, review.Target)
			if err != nil || latest == nil || latest.Reference != second.Reference {
				t.Fatal("latest review unavailable", err)
			}
			exact, err := s.artifactRepository().Get(ctx, first.Review.Candidate)
			if err != nil || string(exact) != string(raw) {
				t.Fatal("candidate changed", err)
			}
			ignored, err := s.root.ReadFile(".desk-private/artifact-reviews/.gitignore")
			if err != nil || string(ignored) != "*\n" {
				t.Fatal("private review not ignored", err)
			}
			review.Candidate = artifactReference([]byte("missing"))
			if _, err = s.retainArtifactReview(ctx, review); !os.IsNotExist(err) {
				t.Fatal("dangling review retained", err)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err = s.retainArtifactReview(canceled, first.Review); err == nil {
				t.Fatal("canceled write succeeded")
			}
		})
	}
}
