package desk

import (
	"context"
	"errors"
	"regexp"
)

// References are workspace-relative content identities, never paths or storage
// credentials. Construction binds the repository to an authorized workspace.
// Get verifies both digest and size. Put preserves exact bytes and is immutable:
// an existing identity must match; corruption is not repaired by overwriting it.
// Errors may represent uncertain persistence. Neither method retries a write.
type retainedArtifactRepository interface {
	Get(context.Context, retainedArtifactRef) ([]byte, error)
	Put(context.Context, []byte) (retainedArtifactRef, error)
}

type retainedArtifactRef struct {
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

const retainedArtifactByteLimit = 32 << 20

var retainedArtifactDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var errRetainedArtifactChanged = errors.New("retained artifact does not match its content identity")

func artifactReference(raw []byte) retainedArtifactRef {
	return retainedArtifactRef{SHA256: digestOf(raw), Bytes: len(raw)}
}
func (r retainedArtifactRef) valid() bool {
	return retainedArtifactDigest.MatchString(r.SHA256) && r.Bytes >= 0 && r.Bytes <= retainedArtifactByteLimit
}
func verifyRetainedArtifact(ref retainedArtifactRef, raw []byte) error {
	if !ref.valid() || ref != artifactReference(raw) {
		return errRetainedArtifactChanged
	}
	return nil
}
func (s *Server) artifactRepository() retainedArtifactRepository {
	if s.retainedArtifacts != nil {
		return s.retainedArtifacts
	}
	return fileRetainedArtifactRepository{open: s.openRetainedArtifacts}
}
