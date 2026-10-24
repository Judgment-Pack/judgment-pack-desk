package codexbridge

import (
	"errors"
	"syscall"
)

// causedFailure retains local error identity without exposing paths or native prose.
type causedFailure struct{ kind, cause error }

func (e *causedFailure) Error() string   { return e.kind.Error() }
func (e *causedFailure) Unwrap() []error { return []error{e.kind, e.cause} }
func failureCause(kind, cause error) error {
	if cause == nil {
		return kind
	}
	return &causedFailure{kind: kind, cause: cause}
}

// DiskFull recognizes a retained filesystem cause at account and run boundaries.
func DiskFull(err error) bool { return errors.Is(err, syscall.ENOSPC) }
