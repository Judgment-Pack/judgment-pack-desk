package storagebudget

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// These vectors are independently authored logical operands, not captured
// expected outputs. Original Q fixtures provide only their existing held files.
func policyOperandVector() PolicyTransition {
	p := fixturePolicy()
	after := p
	after.Installation.Category[History].Bytes = 5
	return PolicyTransition{Original: PolicyCommitment{Installation: ident(30), Binding: digest(30), Revision: 3, Epoch: 1, CheckpointBank: 0, Checkpoint: digest(40), Tickets: [2]Digest{digest(41), digest(42)}, Manifests: digest(43)}, Before: p, After: after}
}
func TestPolicyTransitionStrictOperand(t *testing.T) {
	t.Run("round-trip-and-detached", func(t *testing.T) {
		p := policyOperandVector()
		b, e := EncodePolicyTransition(p)
		must(t, e)
		if len(b) != 2048 {
			t.Fatal("inverse frame width")
		}
		out, e := DecodePolicyTransition(b)
		must(t, e)
		if out != p {
			t.Fatal("lost operand")
		}
		b[0] = 0
		if out != p {
			t.Fatal("decode aliased input")
		}
	})
	for _, name := range []string{"magic", "padding", "short", "oversize"} {
		t.Run(name, func(t *testing.T) {
			b, e := EncodePolicyTransition(policyOperandVector())
			must(t, e)
			switch name {
			case "magic":
				b[0] ^= 1
			case "padding":
				b[len(b)-1] = 1
			case "short":
				b = b[:len(b)-1]
			case "oversize":
				b = append(b, 0)
			}
			if _, e = DecodePolicyTransition(b); e == nil {
				t.Fatal("accepted malformed inverse operand")
			}
		})
	}
	for _, name := range []string{"revision-wrap", "epoch-wrap", "immutable-age", "unchanged", "missing-row", "generation", "invalid-limits"} {
		t.Run(name, func(t *testing.T) {
			p := policyOperandVector()
			switch name {
			case "revision-wrap":
				p.Original.Revision = math.MaxUint64 - 2
			case "epoch-wrap":
				p.Original.Epoch = math.MaxUint64
			case "immutable-age":
				p.After.ObservationMaxAge = time.Second
			case "unchanged":
				p.After = p.Before
			case "missing-row":
				p.AfterLimits = p.Before.Workspace
			case "generation":
				p.Workspace = ident(10)
				p.BeforeLimits = p.Before.Workspace
				p.AfterLimits = p.Before.Workspace
				p.AfterLimits.Total.Bytes--
			case "invalid-limits":
				p.After.Workspace.Total.Files = 0
			}
			if _, e := EncodePolicyTransition(p); e == nil {
				t.Fatal("accepted invalid transition")
			}
		})
	}
}

func policyCaptureVector(t *testing.T, q *Coordinator, edited bool) PolicyTransition {
	t.Helper()
	view, e := q.InspectHost(t.Context())
	must(t, e)
	p := view.Configuration.Policy
	p.Installation.Category[History].Bytes = 5
	p.Workspace.Category[History].Bytes = 4
	var id ID
	var generation uint64
	var limits Limits
	if edited {
		id = ident(10)
		generation = 1
		limits = fixturePolicy().Workspace
		limits.Category[History].Bytes = 3
	}
	change, e := q.CapturePolicyTransition(t.Context(), view.Configuration.Revision, p, id, generation, limits)
	must(t, e)
	return change
}
func TestPolicyTransitionPhasesAndPromises(t *testing.T) {
	t.Run("Q0-Q3-live-sticky-restart", func(t *testing.T) {
		q, path := fixture(t)
		token, e := q.Reserve(t.Context(), request(6), observations())
		must(t, e)
		original, e := q.InspectAction(t.Context())
		must(t, e)
		manifest, e := os.ReadFile(filepath.Join(path, "manifests"))
		must(t, e)
		p := policyCaptureVector(t, q, true)
		must(t, q.CheckPolicyTransition(t.Context(), p, PolicyQ0))
		must(t, q.BeginPolicyTransition(t.Context(), p))
		must(t, q.CheckPolicyTransition(t.Context(), p, PolicyQ1))
		other, e := OpenHeld(q.anchor)
		must(t, e)
		defer other.Close()
		must(t, other.CheckPolicyTransition(t.Context(), p, PolicyQ1))
		must(t, other.ApplyPolicyTransition(t.Context(), p))
		must(t, q.CheckPolicyTransition(t.Context(), p, PolicyQ2))
		if e = q.AbortPolicyTransition(t.Context(), p); !errors.Is(e, ErrStale) {
			t.Fatalf("Q2 rolled back: %v", e)
		}
		must(t, q.FinishPolicyTransition(t.Context(), p))
		must(t, other.CheckPolicyTransition(t.Context(), p, PolicyQ3))
		now, e := q.InspectAction(t.Context())
		must(t, e)
		if now.CheckpointBank != 1-original.CheckpointBank || now.View.Configuration.Revision != p.Original.Revision+3 || now.View.Configuration.Epoch != p.Original.Epoch+1 || now.View.Configuration.Maintenance {
			t.Fatal("phase scalar/bank relation")
		}
		for bank := range original.Tickets {
			if !bytes.Equal(original.Tickets[bank], now.Tickets[bank]) {
				t.Fatal("changed full ticket bank")
			}
		}
		if !reflect.DeepEqual(original.View.Slots, now.View.Slots) || now.View.Slots[token.Slot].Ticket.Token != token {
			t.Fatal("changed original tokens/promises")
		}
		currentManifest, e := os.ReadFile(filepath.Join(path, "manifests"))
		must(t, e)
		if !bytes.Equal(manifest, currentManifest) {
			t.Fatal("rewrote original manifests")
		}
		for _, w := range now.View.Workspaces {
			switch w.ID {
			case ident(10):
				if w.Limits.Category[History].Bytes != 3 {
					t.Fatal("missing explicit limit")
				}
			case ident(11):
				if w.Limits != fixturePolicy().Workspace {
					t.Fatal("default propagated to sticky peer")
				}
			}
		}
		snapshot, e := q.Snapshot(t.Context())
		must(t, e)
		if snapshot.Promised[History].Bytes != 6 {
			t.Fatal("lost preexisting promise")
		}
		if _, e = q.Reserve(t.Context(), request(1), observations()); !errors.Is(e, ErrLimit) {
			t.Fatalf("decrease admitted growth: %v", e)
		}
		if e = q.FinishPolicyTransition(t.Context(), p); !errors.Is(e, ErrStale) {
			t.Fatalf("replayed finalization: %v", e)
		}
		staged, e := q.Advance(t.Context(), token, owner, Staging, digest(44))
		must(t, e)
		_, e = q.Settle(t.Context(), SettlementFact{Token: staged, Owner: owner, Evidence: digest(45), Changes: []Change{{PhysicalAdd: Vector{6, 1, 0}, AttributionAdd: Vector{6, 1, 0}}}})
		must(t, e)
		must(t, q.Checkpoint(t.Context()))
		settled, e := q.Snapshot(t.Context())
		must(t, e)
		if settled.Installation[History].Bytes != 6 || settled.Promised[History].Bytes != 0 {
			t.Fatal("lower limit revoked original settlement")
		}
	})
	t.Run("Q1-only-abort-restart", func(t *testing.T) {
		q, _ := fixture(t)
		before, e := q.InspectAction(t.Context())
		must(t, e)
		p := policyCaptureVector(t, q, false)
		must(t, q.BeginPolicyTransition(t.Context(), p))
		other, e := OpenHeld(q.anchor)
		must(t, e)
		defer other.Close()
		must(t, other.AbortPolicyTransition(t.Context(), p))
		must(t, q.CheckPolicyTransition(t.Context(), p, PolicyAborted))
		now, e := q.InspectAction(t.Context())
		must(t, e)
		if now.View.Configuration.Policy != p.Before || now.View.Configuration.Maintenance || now.View.Configuration.Revision != p.Original.Revision+2 || !reflect.DeepEqual(now.View.Workspaces, before.View.Workspaces) {
			t.Fatal("abort changed baseline")
		}
		if e = q.ApplyPolicyTransition(t.Context(), p); e == nil {
			t.Fatal("applied after abort")
		}
	})
	t.Run("ticket-race-with-same-checkpoint", func(t *testing.T) {
		q, _ := fixture(t)
		p := policyCaptureVector(t, q, false)
		_, e := q.Reserve(t.Context(), request(1), observations())
		must(t, e)
		view, e := q.InspectHost(t.Context())
		must(t, e)
		if view.Configuration.Revision != p.Original.Revision {
			t.Fatal("test requires same revision")
		}
		if e = q.BeginPolicyTransition(t.Context(), p); !errors.Is(e, ErrStale) {
			t.Fatalf("missed raw ticket race: %v", e)
		}
	})
	t.Run("terminal-unapplied-needs-original-recovery", func(t *testing.T) {
		q, path := fixture(t)
		token, e := q.Reserve(t.Context(), request(1), observations())
		must(t, e)
		_, e = q.Settle(t.Context(), SettlementFact{Token: token, Owner: owner, Evidence: digest(52), Changes: []Change{{PhysicalAdd: Vector{1, 1, 0}, AttributionAdd: Vector{1, 1, 0}}}})
		must(t, e)
		raw, e := os.ReadFile(filepath.Join(path, "checkpoint.a"))
		must(t, e)
		view, e := q.InspectHost(t.Context())
		must(t, e)
		p := fixturePolicy()
		p.Installation.Category[History].Bytes = 9
		if _, e = q.CapturePolicyTransition(t.Context(), view.Configuration.Revision, p, ID{}, 0, Limits{}); !errors.Is(e, ErrBusy) {
			t.Fatalf("adopted replay: %v", e)
		}
		same, e := os.ReadFile(filepath.Join(path, "checkpoint.a"))
		must(t, e)
		if !bytes.Equal(raw, same) {
			t.Fatal("capture persisted replay")
		}
		must(t, q.Checkpoint(t.Context()))
		_ = policyCaptureVector(t, q, false)
	})
	t.Run("foreign-and-canceled-no-write", func(t *testing.T) {
		q, _ := fixture(t)
		p := policyCaptureVector(t, q, false)
		foreign := p
		foreign.Original.Binding = digest(99)
		if e := q.BeginPolicyTransition(t.Context(), foreign); !errors.Is(e, ErrAuthority) {
			t.Fatalf("foreign original: %v", e)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if e := q.BeginPolicyTransition(ctx, p); e == nil {
			t.Fatal("canceled dispatch")
		}
		must(t, q.CheckPolicyTransition(t.Context(), p, PolicyQ0))
	})
	t.Run("torn-checkpoint-refuses-without-repair", func(t *testing.T) {
		q, _ := fixture(t)
		p := policyCaptureVector(t, q, false)
		must(t, q.BeginPolicyTransition(t.Context(), p))
		name := "checkpoint.a"
		if p.Original.CheckpointBank == 1 {
			name = "checkpoint.b"
		}
		q.fault = &ioFault{Name: name, Phase: "write", Offset: 0, Short: true}
		if e := q.ApplyPolicyTransition(t.Context(), p); e == nil {
			t.Fatal("short write succeeded")
		}
		q.fault = nil
		if e := q.CheckPolicyTransition(t.Context(), p, PolicyQ1); e == nil {
			t.Fatal("torn peer accepted/repaired")
		}
	})
}
