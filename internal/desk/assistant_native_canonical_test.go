package desk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	agentadapter "github.com/Judgment-Pack/judgment-pack-agent/go/adapter/agent"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/delegate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/turn"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

func TestAssistantNativeCanonicalProduction(t *testing.T) {
	for _, phase := range []string{"work", "plan"} {
		t.Run(phase, func(t *testing.T) {
			s, token, start, trace := assistantProductionFixture(t, "https://example.invalid", phase, "read")
			initial := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
			view := assistantProtocolWaitWithin(t, s, token, initial.ID, "complete", 60*time.Second)
			var record assistantRunRecord
			for _, row := range assistantAuthoringLedger(t, s).Runs {
				if row.ID == view.ID {
					record = row
				}
			}
			if !assistantCanonicalActive(record) || record.Checkpoint == nil || record.Checkpoint.Durable == nil || len(record.Canonical.Events) == 0 || len(record.Canonical.Commits) < 2 {
				t.Fatal("actual native canonical head/log missing")
			}
			assistantRunMust(t, validateAssistantCanonicalProjection(record))
			if record.Checkpoint.Phase != phase || record.Checkpoint.Used != 0 || record.Checkpoint.RequestCap != nil {
				t.Fatal("native phase/accounting changed")
			}
			messages := 0
			for _, row := range record.Canonical.Events {
				var env runcontext.StoredRunEnvelope
				assistantRunMust(t, json.Unmarshal(row.Bytes, &env))
				if env.Event.Message != nil {
					messages++
				}
			}
			if messages != 1 {
				t.Fatalf("canonical message duplicated or omitted: %d", messages)
			}
			before, err := os.ReadFile(trace)
			assistantRunMust(t, err)
			again := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 200)
			after, err := os.ReadFile(trace)
			assistantRunMust(t, err)
			if again.ID != view.ID || string(before) != string(after) || strings.Count(string(after), `"type": "generation"`) != 1 {
				t.Fatal("actual native admission replayed")
			}
		})
	}
}

// Actual account contention proves non-admission without releasing the other owner.
func TestAssistantNativeStrictBusyChildNeverAdmitted(t *testing.T) {
	_, _, e, m, trace := assistantCodexFixture(t, "delegate", "work")
	native, err := e.assistantAgent()
	assistantRunMust(t, err)
	owner := codexbridge.Owner{ID: "other-live-owner", Session: context.Background(), Policy: context.Background()}
	blockerCtx, stopBlocker := context.WithCancel(context.Background())
	defer stopBlocker()
	blockerEntered, blockerDone := make(chan struct{}), make(chan error, 1)
	var calls, children, acknowledgements, refusals atomic.Int32
	provider := native.Run
	native.Run = func(ctx context.Context, request adapter.AgentRequest, cb adapter.AgentCallbacks) error {
		n := calls.Add(1)
		if strings.Contains(request.Prompt, turn.ChildRules) {
			children.Add(1)
		}
		notAdmitted := cb.ProviderRelease.NotAdmitted
		cb.ProviderRelease.NotAdmitted = func(token *adapter.ProviderReleaseInvocation) bool {
			refusals.Add(1)
			return notAdmitted(token)
		}
		ack := cb.ProviderRelease.Acknowledge
		cb.ProviderRelease.Acknowledge = func(token *adapter.ProviderReleaseInvocation) bool {
			acknowledgements.Add(1)
			accepted := ack(token)
			if n == 1 {
				go func() {
					request := codexbridge.RunRequest{Model: "shared-model", Prompt: "Other admitted owner", Effort: "medium", BeforeGeneration: func(ctx context.Context) error { close(blockerEntered); <-ctx.Done(); return ctx.Err() }}
					blockerDone <- m.Run(blockerCtx, owner, request, func(codexbridge.RunEvent) error { return nil }, func(context.Context, codexbridge.ToolCall) (codexbridge.ToolAnswer, error) {
						return codexbridge.ToolAnswer{}, errors.New("unexpected blocker tool")
					})
				}()
				select {
				case <-blockerEntered:
				case <-time.After(5 * time.Second):
					t.Error("other owner did not acquire released account")
				}
			}
			return accepted
		}
		return provider(ctx, request, cb)
	}
	store := runcontext.NewMemoryRunStore(runcontext.MemoryRunStoreOptions{})
	events := []contract.RunEvent{}
	execute := func(resume bool) {
		g := gate.New(gate.Options{})
		g.SetOffer(contract.ToolOffer{})
		bindings := &assistantGateBindings{}
		bindings.add(g)
		run := &adapter.Run{Gate: g.Calls(), ModelName: "shared-model", Prompt: "Inspect the schema only.", Critic: "off", AllowConversation: true, Interactive: true, Context: &runcontext.Input{CurrentRequest: &runcontext.CurrentRequest{Message: "Inspect the schema only.", Input: "Inspect the schema only."}, RunStore: &runcontext.DurableRunInput{Store: store, ID: "busy-child-release", Resume: resume}}, Emit: func(event contract.RunEvent) { events = append(events, event) }, Children: func(_ context.Context, profile contract.DelegatedTaskProfile, _ func(contract.RunEvent)) (*gate.Calls, []contract.ToolOfferRuntimeTool, error) {
			calls, tools := bindings.child(g, profile, gate.Options{Turn: g})
			return calls, tools, nil
		}, Agent: native}
		assistantRunMust(t, delegate.With(bindings.adapter(assistantCanonicalAgent(agentadapter.New())), delegate.Options{}).Run(context.Background(), run))
	}
	execute(false)
	if children.Load() != 3 || acknowledgements.Load() != 1 || refusals.Load() != calls.Load()-1 {
		t.Fatalf("expected released parent and bounded never-admitted attempts: calls=%d children=%d ack=%d refused=%d", calls.Load(), children.Load(), acknowledgements.Load(), refusals.Load())
	}
	head, err := store.Load(t.Context(), "busy-child-release")
	assistantRunMust(t, err)
	if head == nil || strings.Contains(string(head.Checkpoint), "ProviderReleaseUnknown:") {
		t.Fatal("known-not-started child lost its disposition")
	}
	select {
	case err := <-blockerDone:
		t.Fatalf("busy child closed another owner: %v", err)
	default:
	}
	beforeChildren := children.Load()
	execute(true)
	if children.Load() != beforeChildren {
		t.Fatal("restore replayed the child or forged release")
	}
	stopBlocker()
	select {
	case <-blockerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("other owner did not release after its own cancellation")
	}
	generations := 0
	for _, row := range assistantCodexLog(t, trace) {
		if string(row["type"]) == `"generation"` {
			generations++
		}
	}
	if generations != 1 {
		t.Fatalf("busy child or blocker generated work: %d", generations)
	}
}

func TestAssistantNativeCanonicalReleasedHandoff(t *testing.T) {
	s, token, start, trace := assistantProductionFixture(t, "https://example.invalid", "work", "delegate")
	initial := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	view := assistantProtocolWaitWithin(t, s, token, initial.ID, "complete", 60*time.Second)
	var record assistantRunRecord
	for _, row := range assistantAuthoringLedger(t, s).Runs {
		if row.ID == view.ID {
			record = row
		}
	}
	if !assistantCanonicalActive(record) || record.Checkpoint.Durable == nil || record.Checkpoint.Handoff == nil || record.Checkpoint.Handoff.State != "continued" {
		t.Fatalf("strict actual handoff did not continue: %+v", record.Checkpoint)
	}
	assistantRunMust(t, validateAssistantCanonicalProjection(record))
	generations := 0
	for _, row := range assistantCodexLog(t, trace) {
		if string(row["type"]) == `"generation"` {
			generations++
		}
	}
	if generations != 3 {
		t.Fatalf("strict parent/child/parent generations %d", generations)
	}
	for _, row := range record.Canonical.Events {
		if strings.Contains(string(row.Bytes), "ProviderReleaseUnknown:") {
			t.Fatal("reaped live handoff lacked exact release proof")
		}
	}
}
