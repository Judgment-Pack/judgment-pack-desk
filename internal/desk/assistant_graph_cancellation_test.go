package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// Hand-authored API response; the real Eino adapter creates the canonical call,
// and the concrete Graph producer supplies the observed draft and result.
func assistantGraphCancellationResponse(w http.ResponseWriter) {
	args, _ := json.Marshal(map[string]string{"id": "new-flow", "content": assistantGraphCandidate})
	delta := map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "provider-call", "type": "function", "function": map[string]any{"name": "propose_graph", "arguments": string(args)}}}}
	raw, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, "data: "+string(raw)+"\n\ndata: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
}
func TestAssistantGraphCancellationRetainsObservedOutput(t *testing.T) {
	for _, mode := range []string{"tool-context", "stop-action"} {
		t.Run(mode, func(t *testing.T) {
			var requests, producers atomic.Int32
			u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); assistantGraphCancellationResponse(w) })
			s, token, e := assistantExecutionFixture(t, u.server.URL, "work")
			x := assistantGraphSetup(t, s, e)
			off := false
			x.Run.Review = &off
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			type observation struct {
				draft     graphDraft
				result    []byte
				err       error
				uncertain bool
			}
			seen := make(chan observation, 1)
			testHookAssistantGraphObserved = func(result runcontext.ToolResult, draft graphDraft) {
				if producers.Add(1) != 1 {
					return
				}
				held := observation{draft: draft, result: bytes.Clone(result.Value().JSON())}
				if mode == "tool-context" {
					cancel()
				} else {
					record, err := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
					if err != nil {
						held.err = err
					} else {
						response := policyCall(s, token, "POST", "/api/assistant-runs/"+e.record.ID+"/actions", assistantRunAction{ID: "stop-after-observation", Kind: "cancel", Revision: record.Revision})
						if response.Code != 200 {
							held.err = fmt.Errorf("stop action status %d: %s", response.Code, response.Body.String())
						} else {
							stopped, err := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
							held.err = err
							for _, op := range stopped.Operations {
								if op.Kind == "host" && op.Name == "propose_graph" && op.State == "uncertain" {
									held.uncertain = true
								}
							}
						}
					}
				}
				seen <- held
			}
			defer func() { testHookAssistantGraphObserved = nil }()
			// Await the actual Execute return, including its persistence, even if the
			// canonical deadline/cancel path stops waiting for the tool first.
			finished := make(chan struct{})
			for i := range x.Run.HostTools {
				if x.Run.HostTools[i].Name == "propose_graph" {
					inner := x.Run.HostTools[i].Execute
					x.Run.HostTools[i].Execute = func(ctx context.Context, args json.RawMessage) (runcontext.ToolResult, error) {
						defer close(finished)
						return inner(ctx, args)
					}
				}
			}
			_ = x.Adapter.Run(ctx, x.Run)
			select {
			case <-finished:
			case <-time.After(8 * time.Second):
				t.Fatal("observed producer persistence did not finish")
			}
			var observed observation
			select {
			case observed = <-seen:
			default:
				t.Fatal("actual producer did not return a draft before cancellation")
			}
			assistantRunMust(t, observed.err)
			if mode == "stop-action" && !observed.uncertain {
				t.Fatal("actual Stop did not mark the dispatched operation uncertain")
			}
			retained := assistantExecutionRecord(t, e)
			if len(retained.DomainOutputs) != 1 {
				t.Fatalf("completed Graph work lost on cancellation: outputs=%d writer=%v", len(retained.DomainOutputs), e.writer.Err())
			}
			output := retained.DomainOutputs[0]
			want, _ := json.Marshal(observed.draft)
			got, _ := json.Marshal(output.Draft)
			if !bytes.Equal(got, want) || output.Draft.Content != assistantGraphCandidate {
				t.Fatal("cancellation changed exact observed draft/identity")
			}
			var producer assistantRunOperation
			for _, op := range retained.Operations {
				if op.ID == output.OperationID {
					producer = op
				}
			}
			var receipt assistantHostResult
			assistantRunMust(t, readAssistantHostReceipt(producer.Result, &receipt))
			if producer.State != "completed" || !bytes.Equal(receipt.Result, observed.result) {
				t.Fatal("exact observed result was not retained atomically with output")
			}
			e.cancel()
			select {
			case <-e.done:
			case <-time.After(3 * time.Second):
				t.Fatal("canceled execution did not drain")
			}
			// Reacquisition performs real durable recovery. It cannot revive dispatch.
			lease, err := s.assistantRuns().Acquire(t.Context())
			assistantRunMust(t, err)
			defer lease.Close()
			record, err := lease.store.Read(t.Context(), retained.Binding, retained.ID)
			assistantRunMust(t, err)
			if !contains([]string{"stopped", "interrupted"}, record.State) {
				t.Fatal("recovery revived the interrupted run", record.State)
			}
			state := record.State
			writer, err := lease.Writer(t.Context(), record.Binding, record.ID)
			assistantRunMust(t, err)
			resumedCtx, resumedCancel := context.WithCancel(t.Context())
			defer resumedCancel()
			resumed := &assistantRunExecution{service: e.service, source: e.source, request: e.request.Clone(resumedCtx), record: record, writer: writer, ctx: resumedCtx, cancel: resumedCancel}
			registry, err := resumed.assistantGraphTools(nil)
			assistantRunMust(t, err)
			var intent assistantHostIntent
			assistantRunMust(t, readAssistantHostReceipt(producer.Intent, &intent))
			replayCtx := context.WithValue(runcontext.WithCallID(t.Context(), intent.CallID), assistantHostRootKey{}, true)
			replay, err := assistantGraphFind(t, registry.tools, "propose_graph").Execute(replayCtx, intent.Arguments)
			assistantRunMust(t, err)
			after, err := lease.store.Read(t.Context(), record.Binding, record.ID)
			assistantRunMust(t, err)
			if !bytes.Equal(replay.Value().JSON(), observed.result) || len(after.Operations) != len(record.Operations) || after.Revision != record.Revision || producers.Load() != 1 || requests.Load() != 1 {
				t.Fatal("reconstructed redelivery changed the result or replayed producer/model work")
			}
			if err = writer.Dispatch(t.Context(), producer.ID); err == nil {
				t.Fatal("observed completion revived dispatch")
			}
			assistantRunMust(t, s.reconcileAssistantChatProjection(e.request.Clone(t.Context()), record.ID))
			assistantRunMust(t, s.reconcileAssistantChatProjection(e.request.Clone(t.Context()), record.ID))
			projected, err := lease.store.Read(t.Context(), record.Binding, record.ID)
			assistantRunMust(t, err)
			if projected.State != state || projected.ChatProjection == nil || !projected.ChatProjection.Acknowledged || producers.Load() != 1 || requests.Load() != 1 {
				t.Fatal("terminal projection recovery revived work or lost its acknowledgement")
			}
			saved := policyWorkspaceRead(t, s, token, "/api/conversations")
			var doc struct {
				Chats []struct {
					ID          string       `json:"id"`
					GraphDrafts []graphDraft `json:"graphDrafts"`
				} `json:"chats"`
			}
			assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
			found := false
			for _, chat := range doc.Chats {
				if chat.ID == record.Binding.Owner.ID && len(chat.GraphDrafts) == 1 {
					draft, _ := json.Marshal(chat.GraphDrafts[0])
					found = bytes.Equal(draft, want)
				}
			}
			if !found {
				t.Fatal("stopped run projection lost exact observed draft")
			}
		})
	}
}
