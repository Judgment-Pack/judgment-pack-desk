package desk

import (
	"sync"
	"testing"
)

// The fixture owns exactly these reservations, not every token that happens to
// be present later. A relay can finish asynchronously after the caller returns;
// a length check followed by receive must never drain that relay's admission.
func assistantFixtureRelayReservations(slots chan struct{}) func() {
	for range maxRelayInFlight {
		slots <- struct{}{}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			for range maxRelayInFlight {
				<-slots
			}
		})
	}
}

func TestAssistantQueueFixtureCleanupPreservesAdmission(t *testing.T) {
	s := &Server{relaySlots: make(chan struct{}, maxRelayInFlight), relayQueue: make(chan struct{}, maxRelayQueued)}
	release := assistantFixtureRelayReservations(s.relaySlots)
	if len(s.relaySlots) != maxRelayInFlight {
		t.Fatal("fixture did not reserve its exact slots")
	}
	release()
	if len(s.relaySlots) != 0 {
		t.Fatal("fixture reservations were not released")
	}
	// A real later relay admission stays owned until its own release. Calling
	// fixture cleanup again must be a no-op even while that token is present.
	admission, refusal := s.admitRelay(t.Context(), t.Name())
	if refusal != nil {
		t.Fatal("later relay admission refused", refusal)
	}
	release()
	if got := len(s.relaySlots); got != 1 {
		// Restore a stolen token only to let the real admission clean up on this
		// failing path. The witness fails on theft, never a teardown timeout.
		if got == 0 {
			s.relaySlots <- struct{}{}
		}
		admission.release()
		t.Fatalf("fixture cleanup consumed later admission: got %d tokens, want 1", got)
	}
	admission.release()
	release()
	if len(s.relaySlots) != 0 {
		t.Fatal("cleanup acquired or left an extra token")
	}
}
