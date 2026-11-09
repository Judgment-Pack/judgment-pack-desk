package storagebudget

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reserveComparisonFiles(t *testing.T, path string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, name := range []string{"checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests"} {
		b, err := os.ReadFile(filepath.Join(path, name))
		must(t, err)
		out[name] = b
	}
	return out
}
func reserveComparisonUnchanged(t *testing.T, path string, before map[string][]byte) {
	t.Helper()
	for name, b := range reserveComparisonFiles(t, path) {
		if !bytes.Equal(before[name], b) {
			t.Fatalf("pre-write refusal changed %s", name)
		}
	}
}

func TestReserveCompared(t *testing.T) {
	for _, name := range []string{"long-workflow", "empty-operation", "invalid-operation", "long-target", "invalid-stage", "wrong-entry-kind", "foreign-component", "fanout-entries"} {
		t.Run(name, func(t *testing.T) {
			q, path := fixture(t)
			r := request(1)
			switch name {
			case "long-workflow":
				r.Workflow = strings.Repeat("x", 129)
			case "empty-operation":
				r.Operation = ""
			case "invalid-operation":
				r.Operation = "bad\xff"
			case "long-target":
				r.Entries[0].Target = strings.Repeat("a", 1025)
			case "invalid-stage":
				r.Entries[0].Stage = "../outside"
			case "wrong-entry-kind":
				r.Entries[0].Kind = 255
			case "foreign-component":
				r.Entries[0].Component = 1
			case "fanout-entries":
				r.Fanout = &WorkspaceFanout{Memberships: make([]WorkspaceMembership, 1)}
			}
			before := reserveComparisonFiles(t, path)
			comparison, raw, err := q.CaptureReserve(t.Context(), r)
			if err != ErrBounds || comparison != (ReserveComparison{}) || raw.Checkpoints[0] != nil {
				t.Fatalf("invalid request captured: %v", err)
			}
			reserveComparisonUnchanged(t, path, before)
		})
	}
	t.Run("actual-reserve-stage-dispatch-settle-checkpoint", func(t *testing.T) {
		q, _ := fixture(t)
		r := request(1)
		guard, inspected, err := q.CaptureReserve(t.Context(), r)
		must(t, err)
		if guard.Slot != 0 || inspected.View.Configuration.Maintenance {
			t.Fatal("wrong original slot/state")
		}
		token, err := q.ReserveCompared(t.Context(), r, observations(), guard)
		must(t, err)
		if token.Slot != guard.Slot {
			t.Fatal("slot changed after comparison")
		}
		token, err = q.Advance(t.Context(), token, owner, Staging, digest(31))
		must(t, err)
		token, err = q.Advance(t.Context(), token, owner, Dispatched, digest(32))
		must(t, err)
		fact := SettlementFact{Token: token, Owner: owner, Evidence: digest(33), Changes: []Change{{PhysicalAdd: Vector{1, 1, 0}, AttributionAdd: Vector{1, 1, 0}}}}
		terminal, err := q.Settle(t.Context(), fact)
		must(t, err)
		again, err := q.Settle(t.Context(), fact)
		must(t, err)
		if again != terminal {
			t.Fatal("exact persistence retry changed terminal")
		}
		must(t, q.Checkpoint(t.Context()))
		host, err := q.InspectHostState(t.Context())
		must(t, err)
		if host.Slots[terminal.Slot].PersistedApplied != terminal {
			t.Fatal("terminal not durably applied")
		}
	})
	for _, name := range []string{"different-request", "different-slot", "selected-checkpoint", "ticket-a", "ticket-b", "manifest-binding"} {
		t.Run(name, func(t *testing.T) {
			q, path := fixture(t)
			r := request(1)
			guard, _, err := q.CaptureReserve(t.Context(), r)
			must(t, err)
			switch name {
			case "different-request":
				r.Entries[0].Stage = "different-stage"
			case "different-slot":
				guard.Slot++
			case "selected-checkpoint":
				guard.Original.Checkpoint[0] ^= 1
			case "ticket-a":
				guard.Original.Tickets[0][0] ^= 1
			case "ticket-b":
				guard.Original.Tickets[1][0] ^= 1
			case "manifest-binding":
				guard.Original.Manifests[0] ^= 1
			}
			before := reserveComparisonFiles(t, path)
			token, err := q.ReserveCompared(t.Context(), r, observations(), guard)
			if err != ErrBusy || token != (Token{}) {
				t.Fatalf("want exact pre-write ErrBusy, got %v", err)
			}
			reserveComparisonUnchanged(t, path, before)
		})
	}
	t.Run("intervening-shared-writer-refuses-without-write", func(t *testing.T) {
		q, path := fixture(t)
		r := request(1)
		guard, _, err := q.CaptureReserve(t.Context(), r)
		must(t, err)
		other, root, lock := openFixture(t, path)
		defer root.Close()
		defer lock.Close()
		defer other.Close()
		_, err = other.Reserve(t.Context(), request(1), observations())
		must(t, err)
		before := reserveComparisonFiles(t, path)
		_, err = q.ReserveCompared(t.Context(), r, observations(), guard)
		if err != ErrBusy {
			t.Fatalf("stale original was not exact ErrBusy: %v", err)
		}
		reserveComparisonUnchanged(t, path, before)
	})
	t.Run("terminal-replay-keeps-ordinary-reserve-semantics", func(t *testing.T) {
		q, _ := fixture(t)
		token, err := q.Reserve(t.Context(), request(1), observations())
		must(t, err)
		_, err = q.Settle(t.Context(), SettlementFact{Token: token, Owner: owner, Evidence: digest(40), Changes: []Change{{}}})
		must(t, err)
		// Deliberately do not checkpoint. Policy capture must refuse this state;
		// ordinary Reserve must still see its replayed charge and choose another slot.
		guard, _, err := q.CaptureReserve(t.Context(), request(1))
		must(t, err)
		if guard.Slot == token.Slot {
			t.Fatal("reused unapplied terminal")
		}
		_, err = q.ReserveCompared(t.Context(), request(1), observations(), guard)
		must(t, err)
	})
	t.Run("last-available-slot-race-is-exact-busy", func(t *testing.T) {
		q, path := fixture(t)
		config, err := q.ConfigurationSnapshot(t.Context())
		must(t, err)
		policy := config.Policy
		policy.Installation.Total.Bytes = MetadataBudget + 1024
		policy.Installation.Category[History] = Vector{1024, 1024, 1024}
		policy.Workspace.Category[History] = Vector{1024, 1024, 1024}
		must(t, q.UpdatePolicy(t.Context(), config.Revision, policy))
		for i := 0; i < Slots-1; i++ {
			_, err = q.Reserve(t.Context(), request(1), observations())
			must(t, err)
		}
		guard, _, err := q.CaptureReserve(t.Context(), request(1))
		must(t, err)
		if guard.Slot != Slots-1 {
			t.Fatal("did not capture last available slot")
		}
		_, err = q.Reserve(t.Context(), request(1), observations())
		must(t, err)
		before := reserveComparisonFiles(t, path)
		_, err = q.ReserveCompared(t.Context(), request(1), observations(), guard)
		if err != ErrBusy {
			t.Fatalf("last-slot stale comparison changed error priority: %v", err)
		}
		reserveComparisonUnchanged(t, path, before)
	})
	t.Run("corrupt-manifest-is-not-proven-busy", func(t *testing.T) {
		q, path := fixture(t)
		_, err := q.Reserve(t.Context(), request(1), observations())
		must(t, err)
		guard, _, err := q.CaptureReserve(t.Context(), request(1))
		must(t, err)
		f, err := os.OpenFile(filepath.Join(path, "manifests"), os.O_WRONLY, 0)
		must(t, err)
		_, err = f.WriteAt([]byte{0xff}, 0)
		must(t, err)
		must(t, f.Close())
		before := reserveComparisonFiles(t, path)
		_, err = q.ReserveCompared(t.Context(), request(1), observations(), guard)
		if !errors.Is(err, ErrUnknown) || err == ErrBusy {
			t.Fatalf("corruption classified as definite comparison refusal: %v", err)
		}
		reserveComparisonUnchanged(t, path, before)
	})
}
