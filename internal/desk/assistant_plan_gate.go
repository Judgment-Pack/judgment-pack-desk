package desk

import (
	"context"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
)

// Only handles created by this host can acknowledge final contextual metadata.
// Execution names, model names and restored session fields select no authority.
type assistantGateBindings struct {
	mu         sync.RWMutex
	gates      map[*gate.Calls]*gate.Gate
	invocation *assistantExecutionInvocation
}

func (b *assistantGateBindings) add(g *gate.Gate) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.gates == nil {
		b.gates = make(map[*gate.Calls]*gate.Gate)
	}
	b.gates[g.Calls()] = g
}

func (b *assistantGateBindings) bind(run *adapter.Run) error {
	if run == nil {
		return errAssistantRunScope
	}
	b.mu.RLock()
	g := b.gates[run.Gate]
	b.mu.RUnlock()
	if g == nil {
		return errAssistantRunScope
	}
	run.EnrollContext = func(ctx context.Context, offer adapter.ContextOffer) error {
		if b.invocation != nil {
			if err := b.invocation.check(ctx); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if g.Phase().Kind != offer.Mode {
			return errAssistantRunScope
		}
		return g.EnrollContext(offer.ID, offer.HostTools)
	}
	return nil
}

func (b *assistantGateBindings) adapter(inner adapter.Adapter) adapter.Adapter {
	return adapter.Func(func(ctx context.Context, run *adapter.Run) error {
		if b.invocation != nil {
			if err := b.invocation.check(ctx); err != nil {
				return err
			}
		}
		// Delegate copies Runs. Replace even a copied callback using this invocation's
		// exact handle before either certified adapter constructs its context.
		if err := b.bind(run); err != nil {
			return err
		}
		return inner.Run(ctx, run)
	})
}

// A child starts with Runtime metadata only. Its final contextual host metadata
// must still fit the exact parent's phase and the delegated profile.
func (b *assistantGateBindings) child(parent *gate.Gate, profile contract.DelegatedTaskProfile, options gate.Options) (*gate.Calls, []contract.ToolOfferRuntimeTool) {
	child := gate.New(options)
	parentOffer, parentPhase := parent.Offer(), parent.Phase()
	phase := gate.ChildPhase(profile, parentOffer, parentPhase)
	offer := phase.Offer(parentOffer)
	offer.HostTools = nil
	child.SetOffer(offer)
	child.Enter(phase)
	b.add(child)
	return child.Calls(), offer.Tools
}
