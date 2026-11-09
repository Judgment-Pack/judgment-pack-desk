package desk

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// Drive the production host with the actual cancellation-returning Manager.
// The opaque Tool error is deliberately not recognized by text or checkpoint.
func TestAssistantAgentReturnsExactToolErrorAfterReleaseAndJoin(t *testing.T) {
	for _, mode := range []string{"synchronous", "attempt-cancel", "storage", "writer-failure", "unknown-release", "tool-cancel", "caller-cancel", "release-withheld"} {
		t.Run(mode, func(t *testing.T) {
			f := assistantNativeSettlementSetup(t)
			caller, cancelCaller := context.WithCancel(f.invocation)
			defer cancelCaller()
			stopping := &assistantAgentStop{parent: caller}
			attempt, cancelAttempt := context.WithCancel(context.WithValue(caller, assistantAgentStopKey{}, stopping))
			defer cancelAttempt()
			var toolErr error = errors.New("opaque callback sentinel")
			writeErr := &runcontext.StorageOutcomeUnknown{}
			if mode == "tool-cancel" {
				toolErr = context.Canceled
			}
			if mode == "storage" {
				toolErr = &runcontext.StorageOutcomeUnknown{}
			}
			if mode == "unknown-release" {
				toolErr = &runcontext.ProviderReleaseUnknown{}
			}
			released, callbackDone, resume := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var resumeOnce sync.Once
			allowReturn := func() { resumeOnce.Do(func() { close(resume) }) }
			t.Cleanup(allowReturn)
			release, finish := adapter.ProviderReleaseAttempt()
			acknowledge := release.Acknowledge
			release.Acknowledge = func(id *adapter.ProviderReleaseInvocation) bool {
				ok := false
				if mode != "release-withheld" {
					ok = acknowledge(id)
				}
				close(released)
				return ok
			}
			callbacks := adapter.AgentCallbacks{ProviderRelease: release, Event: func(context.Context, adapter.AgentEvent) error { return nil }, Tool: func(context.Context, adapter.AgentToolCall) (adapter.AgentToolAnswer, error) {
				defer close(callbackDone)
				if mode != "synchronous" {
					if mode == "caller-cancel" {
						cancelCaller()
					}
					cancelAttempt()
					<-released
					<-resume
				}
				if mode == "writer-failure" {
					// The same writer latch survives even if a later observed-result
					// transaction succeeds. It cannot become a pause/cancellation.
					f.execution.writer.mu.Lock()
					f.execution.writer.fail(writeErr)
					f.execution.writer.mu.Unlock()
				}
				return adapter.AgentToolAnswer{}, toolErr
			}}
			request := adapter.AgentRequest{Model: "shared-model", Prompt: "Bounded callback return fixture.", Effort: "medium", Phase: "author", Tools: []adapter.AgentTool{{Name: "desk_fixture_act", Description: "write_fixture: exact callback return", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
			done := make(chan error, 1)
			go func() { done <- f.agent.Run(attempt, request, callbacks) }()
			select {
			case <-released:
			case <-time.After(6 * time.Second):
				t.Fatal("actual provider was not released")
			}
			if mode != "synchronous" {
				select {
				case err := <-done:
					t.Fatal("provider release returned before Tool callback joined", err)
				default:
				}
			}
			allowReturn()
			var got error
			select {
			case got = <-done:
			case <-time.After(6 * time.Second):
				t.Fatal("host did not return after callback join")
			}
			select {
			case <-callbackDone:
			default:
				t.Fatal("host returned before original callback")
			}
			switch mode {
			case "caller-cancel":
				if !errors.Is(got, context.Canceled) || got == toolErr {
					t.Fatal("Tool error displaced the real caller cancellation", got)
				}
			case "writer-failure":
				if got != writeErr || !finish() || f.execution.ctx.Err() == nil {
					t.Fatal("latched receipt writer failure lost its identity", got)
				}
			case "tool-cancel":
				if got != toolErr || !finish() || f.execution.ctx.Err() == nil {
					t.Fatal("actual Tool cancellation retained a live owner", got)
				}
			case "release-withheld":
				if got == toolErr || got == nil || finish() || f.execution.ctx.Err() == nil {
					t.Fatal("unknown provider release became an acknowledged Tool return")
				}
			default:
				if got != toolErr || !finish() {
					t.Fatal("host lost exact Tool error or actual release", got)
				}
				if f.execution.ctx.Err() != nil {
					t.Fatal("provider attempt cancellation canceled the outer owner")
				}
			}
			if mode == "storage" || mode == "unknown-release" {
				_, outcome := assistantNativeSettlementOutcome(t, f.execution, "model")
				if outcome.Stop != "" || outcome.Failure == nil || outcome.Failure.Code != "storage" || outcome.Failure.Outcome != "unknown" {
					t.Fatal("typed unknown/storage Tool failure was suppressed", outcome)
				}
			}
		})
	}
}
