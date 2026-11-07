package desk

import (
	"errors"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// **A passive pin keeps the owner open without blocking enrollment.** The
// dialect read-recovery writer holds the owner for the Server's life. Counted
// as an operation, that hold refused every later enrollment step ("storage
// coordinator busy"): after any restart with saved chats, a move preview, a
// move and a first dialect draft. A pin does not stop an enrollment step;
// an operation retain still does; and Close still waits for the last pin.
func TestStorageOwnerPinIsNotAnOperation(t *testing.T) {
	s := ownerActualNew(t, ownerActualConfig(t))
	c, o := s.storageConstruction, s.storageConstruction.owner
	if s.recoveryStorage != nil {
		storageTestMust(t, s.recoveryStorage.Close())
		s.recoveryStorage = nil
	}
	storageTestMust(t, o.pin())
	p, err := c.beginEnrollmentEpoch(t.Context())
	if err != nil {
		storageTestMust(t, o.unpin())
		t.Fatalf("a pinned owner refused an enrollment step: %v", err)
	}
	if err = o.retain(); !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("an operation entered an enrollment step", err)
	}
	storageTestMust(t, p.finish(nil))
	storageTestMust(t, o.retain())
	if _, err = c.beginEnrollmentEpoch(t.Context()); !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("an enrollment step began under an operation", err)
	}
	storageTestMust(t, o.drop())
	storageTestMust(t, o.Close())
	o.mu.Lock()
	closed := o.closed
	o.mu.Unlock()
	if closed {
		t.Fatal("the owner closed under a pin")
	}
	storageTestMust(t, o.unpin())
	o.mu.Lock()
	closed = o.closed
	o.mu.Unlock()
	if !closed {
		t.Fatal("the owner stayed open after its last pin")
	}
}
