package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func TestAssistantDialectResearchActualAPIProducer(t *testing.T) {
	for _, grant := range []bool{false, true} {
		t.Run(map[bool]string{false: "review", true: "started"}[grant], func(t *testing.T) {
			var saved inputPreparationReply
			var steps atomic.Int32
			names := []string{"read_dialect_recipe", assistantResearchTool, "read_dialect_activity"}
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				n := int(steps.Add(1))
				finish := "tool_calls"
				var delta map[string]any
				if n <= len(names) {
					args := string(assistantChatJSON(map[string]string{"id": saved.Document.ID}))
					if n == 2 {
						args = assistantResearchArgs(saved)
					}
					delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "same-provider-call", "type": "function", "function": map[string]any{"name": names[n-1], "arguments": args}}}}
				} else {
					delta = map[string]any{"role": "assistant", "content": "Report the actual admission status. Queued research is not complete."}
					finish = "stop"
				}
				raw, _ := json.Marshal(map[string]any{"id": "research-fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: "+string(raw)+"\n\ndata: {\"id\":\"research-fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\""+finish+"\"}]}\n\ndata: [DONE]\n\n")
			})
			s, token, e := assistantRecipeBrowserFixture(t, u.server.URL)
			var rrRun inputResearchRun
			seeded, rr, run := assistantResearchSeed(t, s, token, e, grant)
			saved = seeded
			rrRun = run
			x := assistantExecutionOpen(t, s, e)
			off := false
			x.Run.Review = &off
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
			record := assistantMappingObserve(t, s, e, 1)
			if steps.Load() != 4 {
				t.Fatal("actual research flow requested extra model operations", steps.Load())
			}
			p := record.DomainOutputs[0].DialectAction
			if (p.Action.Applied != nil) != grant {
				t.Fatal("actual admission status lost")
			}
			for _, op := range record.Operations {
				if op.Kind == "host" && op.State != "completed" {
					t.Fatal("actual producer refused", op.Name, string(op.Result))
				}
			}
			if grant {
				assistantResearchUsed(t, s, rr, rrRun)
			}
			before, err := s.researchJournal(rr, run.ID).Read(t.Context())
			assistantRunMust(t, err)
			assistantRunMust(t, e.writer.State(t.Context(), "complete"))
			assistantRunMust(t, e.ProjectChat(t.Context()))
			assistantRunMust(t, e.ProjectChat(t.Context()))
			after, err := s.researchJournal(rr, run.ID).Read(t.Context())
			assistantRunMust(t, err)
			if steps.Load() != 4 || !bytesEqualResearch(before, after) {
				t.Fatal("projection repeated workflow/model")
			}
			key := "DESK_DIALECT_RESEARCH_READER_REVIEW"
			if grant {
				key = "DESK_DIALECT_RESEARCH_READER_STARTED"
			}
			if path := os.Getenv(key); path != "" {
				assistantRunMust(t, os.WriteFile(path, policyWorkspaceRead(t, s, token, "/api/conversations").Content, 0600))
			}
			if !grant {
				if path := os.Getenv("DESK_DIALECT_RESEARCH_READER_REVIEW_SOURCE"); path != "" {
					assistantRunMust(t, os.WriteFile(path, assistantChatJSON(saved), 0600))
				}
			}
			if grant {
				if path := os.Getenv("DESK_DIALECT_RESEARCH_READER_SOURCE"); path != "" {
					assistantRunMust(t, os.WriteFile(path, assistantChatJSON(saved), 0600))
				}
				for _, op := range record.Operations {
					if op.Kind == "host" && op.Name == "read_dialect_activity" {
						var receipt assistantHostResult
						assistantRunMust(t, readAssistantHostReceipt(op.Result, &receipt))
						v, err := runcontext.ParseJSON(receipt.Result)
						assistantRunMust(t, err)
						result := runcontext.ReadToolResult(v)
						if path := os.Getenv("DESK_DIALECT_RESEARCH_READER_ACTIVITY"); path != "" {
							assistantRunMust(t, os.WriteFile(path, []byte(result.Content[0].Text()), 0600))
						}
						if !strings.Contains(result.Content[0].Text(), `"state":"queued"`) {
							t.Fatal("queued research absent from real activity")
						}
					}
				}
			}
		})
	}
}
func bytesEqualResearch(a, b inputResearchRun) bool {
	return string(assistantChatJSON(a)) == string(assistantChatJSON(b))
}
func TestAssistantDialectResearchActualNativeParentChild(t *testing.T) {
	s, token, e, _, trace := assistantCodexFixture(t, "delegate", "work")
	saved, rr, run := assistantResearchSeed(t, s, token, e, true)
	id := string(assistantChatJSON(map[string]string{"id": saved.Document.ID}))
	peer := strings.Replace(assistantExecutionCodexPeer, "if mode=='delegate' and index==1:\n   tool(offered('delegate_tasks'),{'tasks':[{'title':'Inspect schema','objective':'Inspect the schema only.','profile':'review'}]})", `if mode=='delegate' and index==1: tool(offered('read_dialect_recipe'),json.loads(`+fmt.Sprintf("%q", id)+`),call='same-provider-call')
  elif mode=='delegate' and index==2: tool(offered('read_dialect_activity'),json.loads(`+fmt.Sprintf("%q", id)+`),call='same-provider-call')
  elif mode=='delegate' and index==3: tool(offered('request_dialect_research'),json.loads(`+fmt.Sprintf("%q", assistantResearchArgs(saved))+`),call='same-provider-call')`, 1)
	peer = strings.Replace(peer, "if mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')", `if mode=='delegate' and index==1: tool(offered('delegate_tasks'),{'tasks':[{'title':'Read activity','objective':'Read explicit activity only.','profile':'review'}]},call='delegate',rpc='rpc-delegate')
  elif mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')`, 1)
	assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
	x := assistantExecutionOpen(t, s, e)
	off := false
	x.Run.Review = &off
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	record := assistantMappingObserve(t, s, e, 1)
	assistantResearchUsed(t, s, rr, run)
	roots, children := 0, 0
	callbacks := map[string]bool{}
	for _, op := range record.Operations {
		if op.Kind == "host" {
			var in assistantHostIntent
			assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
			if in.CallbackID == "" || callbacks[in.CallbackID] {
				t.Fatal("native callback custody lost")
			}
			callbacks[in.CallbackID] = true
			if in.Root {
				roots++
			} else {
				children++
			}
			if op.State != "completed" {
				t.Fatal("actual native host refusal", op.Name, string(op.Result))
			}
		}
	}
	if roots != 2 || children != 1 {
		t.Fatal("native parent-child-parent custody lost", roots, children)
	}
}
