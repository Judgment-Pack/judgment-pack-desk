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
	return s.artifactRepositoryWithAuthority(nil)
}

// Bind only current authority, never a source lookup that reenters private data.
// Owning adapters enforce this callback after storage waits and before writes.
func (s *Server) artifactRepositoryWithAuthority(authorize func(context.Context) error) retainedArtifactRepository {
	if s.retainedArtifacts != nil {
		if authorize == nil {
			return s.retainedArtifacts
		}
		if owner, ok := s.retainedArtifacts.(authorityRetainedArtifactRepository); ok {
			return owner.WithAuthority(authorize)
		}
		return unavailableAuthorityArtifactRepository{}
	}
	return fileRetainedArtifactRepository{open: s.openRetainedArtifacts, authorize: authorize}
}

// A custom owner must bind authority inside its own queued mutation boundary.
// Wrapping Put before/after is insufficient when that implementation can wait.
type authorityRetainedArtifactRepository interface {
	WithAuthority(func(context.Context) error) retainedArtifactRepository
}
type unavailableAuthorityArtifactRepository struct{}

func (unavailableAuthorityArtifactRepository) Get(context.Context, retainedArtifactRef) ([]byte, error) {
	return nil, errRetainedArtifactChanged
}
func (unavailableAuthorityArtifactRepository) Put(context.Context, []byte) (retainedArtifactRef, error) {
	return retainedArtifactRef{}, errRetainedArtifactChanged
}

// retainedArtifactGuard pins the caller's full effect authority for one exact
// publication. Check inspects already-owned state and bounded leaf authority;
// it must not enter another repository, acquire host custody or journal effects.
// Close releases custody exactly once and reports any cleanup uncertainty.
type retainedArtifactGuard interface {
	Check(context.Context) error
	Close() error
}

// This optional owner capability drains its actual storage queue before calling
// acquire. It releases that exclusion, acquires fresh host custody, then tries
// storage exclusion once without waiting. It never retries admission or Put.
// acquire must bind the immutable raw identity and exact invocation intent;
// candidate content and historical metadata cannot provide authority.
type guardedRetainedArtifactRepository interface {
	retainedArtifactRepository
	PutGuarded(context.Context, []byte, func(context.Context) (retainedArtifactGuard, error)) (retainedArtifactRef, error)
}

var errRetainedArtifactAuthorityUnavailable = errors.New("guarded retained artifact authority is unavailable")
var errRetainedArtifactBusy = errors.New("retained artifact publication is busy")

// A wrapper must explicitly delegate guarded publication through its own actual
// queue; an older WithAuthority implementation alone cannot provide this scope.
func (s *Server) guardedArtifactRepository() guardedRetainedArtifactRepository {
	if s.retainedArtifacts != nil {
		if owner, ok := s.retainedArtifacts.(guardedRetainedArtifactRepository); ok {
			return owner
		}
		return unavailableGuardedArtifactRepository{}
	}
	return fileRetainedArtifactRepository{open: s.openRetainedArtifacts}
}

type unavailableGuardedArtifactRepository struct {
	unavailableAuthorityArtifactRepository
}

func (unavailableGuardedArtifactRepository) PutGuarded(context.Context, []byte, func(context.Context) (retainedArtifactGuard, error)) (retainedArtifactRef, error) {
	return retainedArtifactRef{}, errRetainedArtifactAuthorityUnavailable
}

// D5 selection is constructor sealed, distinct from generic Get/Put and the
// optional guarded capability. Type assertion avoids comparing arbitrary
// repository interface values, which can contain non-comparable wrappers.
func (s *Server) injectedArtifactRegistration() (*retainedArtifactProviderRegistration, error) {
	if s == nil || s.retainedArtifacts == nil {
		return nil, nil
	}
	p, ok := s.retainedArtifacts.(*persistentInjectedArtifactProvider)
	if !ok || p == nil || p.self != p || p.revoked.Load() {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	owner := s.deskOwner()
	if owner == nil || owner.retainedArtifactProviders == nil {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	r := owner.retainedArtifactProviders.byID[p.id]
	if r == nil || r.provider != p || r.registry != owner.retainedArtifactProviders {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	return r, nil
}
func (s *Server) prepareInjectedArtifactEnrollment(ctx context.Context) (r *retainedArtifactProviderRegistration, op *retainedArtifactInjectedOperation, err error) {
	r, err = s.injectedArtifactRegistration()
	if err != nil || r == nil {
		return r, nil, err
	}
	op, err = r.provider.pin()
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, op.finish())
			r, op = nil, nil
		}
	}()
	if err = r.drainPinned(ctx, op); err != nil {
		return nil, op, err
	}
	current, err := s.injectedArtifactRegistration()
	if err != nil || current != r {
		return nil, op, errors.Join(errRetainedArtifactAuthorityUnavailable, err)
	}
	return r, op, nil
}
