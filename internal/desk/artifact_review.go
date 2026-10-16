package desk

// A retained review records exact bytes and human compatibility judgment. It
// grants no permission: each writer must recheck its current base and admission.
import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
)

const artifactReviewLimit = 2 << 20

type artifactImpact struct {
	Classification string `json:"classification"`
	Rationale      string `json:"rationale"`
}

func (i artifactImpact) valid() bool {
	return contains([]string{"initial", "patch", "minor", "major", "unknown"}, i.Classification) && len(i.Rationale) <= 4000 && strings.TrimSpace(i.Rationale) != ""
}

type artifactReview struct {
	Version         int                   `json:"version"`
	Target          string                `json:"target"`
	BasePublication string                `json:"basePublication"`
	StorageRevision string                `json:"storageRevision"`
	Candidate       retainedArtifactRef   `json:"candidate"`
	ProposedVersion string                `json:"proposedVersion"`
	Impact          artifactImpact        `json:"impact"`
	Dependencies    []retainedArtifactRef `json:"dependencies"`
	// The host's checked preview is retained for display and recovery, never used
	// instead of fresh writer admission. JSON stays opaque to this shared store.
	Preview json.RawMessage `json:"preview"`
}
type artifactReviewReply struct {
	Reference retainedArtifactRef `json:"reference"`
	Review    artifactReview      `json:"review"`
}

func decodeArtifactReview(raw []byte) (artifactReview, error) {
	var review artifactReview
	if len(raw) > artifactReviewLimit || decodeDataJSON(raw, &review) != nil || review.Version != 1 || len(review.Target) == 0 || len(review.Target) > 1024 || review.BasePublication != "" && !retainedArtifactDigest.MatchString(review.BasePublication) || !retainedArtifactDigest.MatchString(review.StorageRevision) || !review.Candidate.valid() || len(review.ProposedVersion) == 0 || len(review.ProposedVersion) > 256 || !review.Impact.valid() || len(review.Dependencies) > 32 || !json.Valid(review.Preview) {
		return artifactReview{}, errors.New("The retained artifact review is unreadable.")
	}
	for _, ref := range review.Dependencies {
		if !ref.valid() {
			return artifactReview{}, errRetainedArtifactChanged
		}
	}
	return review, nil
}
func (s *Server) artifactReview(ctx context.Context, ref retainedArtifactRef) (artifactReviewReply, error) {
	if !ref.valid() || ref.Bytes > artifactReviewLimit {
		return artifactReviewReply{}, errRetainedArtifactChanged
	}
	raw, err := s.artifactRepository().Get(ctx, ref)
	if err != nil {
		return artifactReviewReply{}, err
	}
	review, err := decodeArtifactReview(raw)
	return artifactReviewReply{ref, review}, err
}
func (s *Server) latestArtifactReview(ctx context.Context, target string) (*artifactReviewReply, error) {
	root, _, err := openOwnFolder(s.root, []string{".desk-private", "artifact-reviews"}, false, "artifact reviews")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	raw, err := readPrivateData(root, digestOf([]byte(target))+".json", 1024)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ref retainedArtifactRef
	if decodeDataJSON(raw, &ref) != nil {
		return nil, errRetainedArtifactChanged
	}
	reply, err := s.artifactReview(ctx, ref)
	if err != nil {
		return nil, err
	}
	if reply.Review.Target != target {
		return nil, errRetainedArtifactChanged
	}
	return &reply, nil
}
func (s *Server) retainArtifactReview(ctx context.Context, review artifactReview) (artifactReviewReply, error) {
	raw, err := json.Marshal(review)
	if err != nil {
		return artifactReviewReply{}, err
	}
	if _, err = decodeArtifactReview(raw); err != nil {
		return artifactReviewReply{}, err
	}
	// Candidate and dependencies must already be retained. Refuse dangling checks.
	for _, ref := range append([]retainedArtifactRef{review.Candidate}, review.Dependencies...) {
		if _, err = s.artifactRepository().Get(ctx, ref); err != nil {
			return artifactReviewReply{}, err
		}
	}
	ref, err := s.artifactRepository().Put(ctx, raw)
	if err != nil {
		return artifactReviewReply{}, err
	}
	root, made, err := openOwnFolder(s.root, []string{".desk-private", "artifact-reviews"}, true, "artifact reviews")
	if err != nil {
		return artifactReviewReply{}, err
	}
	defer root.Close()
	if made {
		if err = writePrivateData(root, ".gitignore", []byte("*\n")); err != nil {
			return artifactReviewReply{}, err
		}
	}
	lock, err := root.Open(".")
	if err != nil {
		return artifactReviewReply{}, err
	}
	defer lock.Close()
	if err = waitPrivateDataLock(ctx, lock, true); err != nil {
		return artifactReviewReply{}, err
	}
	entries, err := lock.ReadDir(-1)
	if err != nil {
		return artifactReviewReply{}, err
	}
	name := digestOf([]byte(review.Target)) + ".json"
	if len(entries) >= 4096 {
		if _, err = root.Lstat(name); err != nil {
			return artifactReviewReply{}, errors.New("Artifact review storage is full.")
		}
	}
	if err = ctx.Err(); err != nil {
		return artifactReviewReply{}, err
	}
	pointer, _ := json.Marshal(ref)
	if err = writePrivateData(root, name, pointer); err != nil {
		return artifactReviewReply{}, err
	}
	return artifactReviewReply{ref, review}, nil
}
