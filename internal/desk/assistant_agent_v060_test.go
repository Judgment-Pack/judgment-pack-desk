package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// Agent's release certification holds the actual adapter tool prefix constant.
// This host regression holds the marker through the real durable pause, reload
// and authenticated decision delivery that sit between those adapter requests.
func TestAssistantAgentV060ApprovalMarker(t *testing.T) {
	const marker = "0123456789abcdef0123456789abcdef"
	const description = "A retained tool description."
	original := runcontext.FrameBlock("tool description", description, marker)
	for _, approved := range []bool{false, true} {
		t.Run(fmt.Sprint(approved), func(t *testing.T) {
			s, token, start, executor := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
				cp := *e.record.Checkpoint
				if e.decision == nil {
					cp = assistantProtocolAsked(cp)
					value := marker
					cp.Approval.Marker = &value
					return e.Checkpoint(ctx, cp)
				}
				raw, err := json.Marshal(cp)
				if err != nil {
					return err
				}
				value, err := runcontext.ParseJSON(raw)
				if err != nil {
					return err
				}
				held, err := runcontext.ReadExecution(value)
				if err != nil {
					return err
				}
				if held.Approval == nil || held.Approval.Marker == nil || *held.Approval.Marker != marker || e.decision.Approved != approved || e.decision.CallID != "call-one" {
					return fmt.Errorf("approval continuation lost its exact marker or decision")
				}
				if runcontext.FrameBlock("tool description", description, *held.Approval.Marker) != original {
					return fmt.Errorf("persisted marker changed framed description bytes")
				}
				cp.Status = "complete"
				cp.Approval.State = "refused"
				if approved {
					cp.Approval.State = "approved"
				}
				// The fixture dispatches no tool: retained metadata itself grants
				// nothing, even when a current person approves this synthetic call.
				return e.Checkpoint(ctx, cp)
			})
			view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
			paused := assistantProtocolWait(t, s, token, view.ID, "awaiting-input")
			if paused.Checkpoint.Approval.Marker == nil || *paused.Checkpoint.Approval.Marker != marker {
				t.Fatal("public paused snapshot lost the framing marker")
			}
			lease, err := s.assistantRuns().Acquire(t.Context())
			assistantRunMust(t, err)
			assistantRunMust(t, lease.Close())
			args, err := json.Marshal(paused.Checkpoint.Approval.Args)
			assistantRunMust(t, err)
			action := assistantRunAction{ID: "marker-decision", Kind: "approval", Revision: paused.Revision, CallID: "call-one", ArgsDigest: digestOf(args), Approved: &approved}
			assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+view.ID+"/actions", action), 200)
			complete := assistantProtocolWait(t, s, token, view.ID, "complete")
			before, err := json.Marshal(paused.Checkpoint.Approval.Marker)
			assistantRunMust(t, err)
			after, err := json.Marshal(complete.Checkpoint.Approval.Marker)
			assistantRunMust(t, err)
			if !bytes.Equal(before, after) || executor.calls.Load() != 2 || len(complete.Operations) != 0 {
				t.Fatal("host continuation changed the marker or dispatched work")
			}
		})
	}
}
