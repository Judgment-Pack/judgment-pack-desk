package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
)

const assistantGraphConfig = `{"configVersion":"5","packs":{"one":{"path":"one.json","description":"A pack"}},"graphs":{"flow":{"path":"flow.graph.json","description":"A graph"}}}`
const assistantGraphOriginal = "{\n \"formatVersion\":\"1\", \"nodes\":{\"first\":{\"pack\":\"one\"}}, \"edges\":[]\n}"
const assistantGraphCandidate = "{\n \"formatVersion\":\"1\", \"nodes\":{\"first\":{\"pack\":\"one\"}}, \"edges\":[], \"fixture\":{\"missing\":null,\"flag\":false,\"zero\":0,\"literal\":\"50\",\"number\":1.20}\n}"
const assistantGraphCLI = `
if sys.argv[1:3]==['experimental','graph']:
 command=sys.argv[3]; text=sys.stdin.read()
 with open('graph-cli.jsonl','a') as f: f.write(json.dumps({'command':command,'args':sys.argv[4:],'input':text})+'\n')
 if command=='list': print('{"outputVersion":"2","command":"experimental graph list","status":"resolved","graphs":[{"id":"flow","path":"flow.graph.json"}]}')
 elif command=='evaluate': print('{"outputVersion":"2","command":"experimental graph evaluate","status":"evaluated","rehearsal":true,"value":{"number":1.20,"literal":"50","missing":null,"flag":false,"zero":0}}')
 else: print('{ "outputVersion":"2", "command":"experimental graph '+command+'", "status":"valid", "number":1.20, "literal":"50", "missing":null, "flag":false, "zero":0 }')
 sys.exit(0)
`

func assistantGraphSetup(t *testing.T, s *Server, e *assistantRunExecution) *assistantExecution {
	t.Helper()
	for name, content := range map[string]string{"jpack.json": assistantGraphConfig, "one.json": "{ \"pack\":\"one\", \"number\":1.20, \"flag\":false }", "flow.graph.json": assistantGraphOriginal} {
		assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, name), []byte(content), 0600))
	}
	assistantExecutionRuntimeFixture(t, s, e)
	peer := strings.Replace(assistantExecutionPeer, "with open('execution-fixture.json')", assistantGraphCLI+"\nwith open('execution-fixture.json')", 1)
	peer = strings.Replace(peer, "{'name':'served','arguments':[]}", "{'name':'served','arguments':[]},{'name':'author_graph','arguments':[{'name':'relationship','required':True},{'name':'packs','required':True}]}", 1)
	peer = strings.Replace(peer, "'text':'SERVED-RUNTIME-PROMPT'", "'text':('RUNTIME GRAPH CONTRACT '+p['arguments']['packs']) if name=='author_graph' else 'SERVED-RUNTIME-PROMPT'", 1)
	assistantRunMust(t, os.WriteFile(s.cfg.JpackBin, []byte(peer), 0700))
	x, err := e.openAssistantExecution(t.Context(), assistantExecutionPrompt{Name: "served"})
	assistantRunMust(t, err)
	t.Cleanup(x.Close)
	return x
}
func assistantGraphFind(t *testing.T, tools []runcontext.HostTool, name string) runcontext.HostTool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatal("missing tool", name)
	return runcontext.HostTool{}
}
func assistantGraphInvoke(t *testing.T, e *assistantRunExecution, x *assistantExecution, call, name string, args any) runcontext.ToolResult {
	t.Helper()
	raw, _ := json.Marshal(args)
	assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &call, Name: name, Args: raw}}))
	ctx := context.WithValue(runcontext.WithCallID(t.Context(), call), assistantHostRootKey{}, true)
	result, err := assistantGraphFind(t, x.Run.HostTools, name).Execute(ctx, raw)
	assistantRunMust(t, err)
	assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolResult: &contract.ToolResult{Type: "tool_result", CallID: &call, Name: name, Text: result.Content[0].Text(), IsError: result.Err()}}))
	return result
}
func TestAssistantGraphProducerDurableReadAndProjection(t *testing.T) {
	s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	x := assistantGraphSetup(t, s, e)
	// The real producer refuses a revision with no root read.
	no := assistantGraphInvoke(t, e, x, "no-read", "propose_graph", map[string]string{"id": "flow", "content": assistantGraphCandidate})
	if !no.Err() {
		t.Fatal("revision without read succeeded")
	}
	read := assistantGraphInvoke(t, e, x, "read", "read_decision", map[string]string{"type": "graph", "id": "flow"})
	if read.Err() || !strings.Contains(read.Content[0].Text(), "1.20") {
		t.Fatal(read.Value().Text())
	}
	// Use the actual canonical checkpoint writer, storage decoder and a newly
	// constructed registry; read authority is never recaptured from narrative.
	cp := *assistantExecutionRecord(t, e).Checkpoint
	assistantRunMust(t, e.Checkpoint(t.Context(), cp))
	record := assistantExecutionRecord(t, e)
	raw, _ := json.Marshal(assistantRunLedger{Version: 1, Project: record.Binding.Project, Runs: []assistantRunRecord{record}})
	_, err := decodeAssistantRunStorage(e.writer.lease.store.name, raw)
	assistantRunMust(t, err)
	registry, err := e.assistantGraphTools(x.runtime)
	assistantRunMust(t, err)
	x.Run.HostTools = registry.tools
	proposed := assistantGraphInvoke(t, e, x, "proposal", "propose_graph", map[string]string{"id": "flow", "content": assistantGraphCandidate, "description": "Retained exact graph"})
	if proposed.Err() {
		t.Fatal(proposed.Value().Text())
	}
	record = assistantExecutionRecord(t, e)
	if len(record.DomainOutputs) != 1 {
		t.Fatal("producer payload absent")
	}
	out := record.DomainOutputs[0]
	if out.Draft.Content != assistantGraphCandidate || out.Draft.BaseSHA256 != digestOf([]byte(assistantGraphOriginal)) || out.Draft.Path != "flow.graph.json" || out.CallID != "proposal" {
		t.Fatalf("candidate changed: %+v", out)
	}
	// Exact replay returns the original draft/result and never adds another output.
	args, _ := json.Marshal(map[string]string{"id": "flow", "content": assistantGraphCandidate, "description": "Retained exact graph"})
	ctx := context.WithValue(runcontext.WithCallID(t.Context(), "proposal"), assistantHostRootKey{}, true)
	again, err := assistantGraphFind(t, registry.tools, "propose_graph").Execute(ctx, args)
	assistantRunMust(t, err)
	if again.Value().Text() != proposed.Value().Text() || len(assistantExecutionRecord(t, e).DomainOutputs) != 1 {
		t.Fatal("producer replay generated a new result")
	}
	cp = *record.Checkpoint
	cp.Status = "complete"
	assistantRunMust(t, e.Checkpoint(t.Context(), cp))
	assistantRunMust(t, e.writer.State(t.Context(), "complete"))
	assistantRunMust(t, e.ProjectChat(t.Context()))
	assistantRunMust(t, e.ProjectChat(t.Context()))
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc struct {
		Chats []struct {
			ID          string       `json:"id"`
			GraphDrafts []graphDraft `json:"graphDrafts"`
		} `json:"chats"`
	}
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	if len(doc.Chats[0].GraphDrafts) != 1 || doc.Chats[0].GraphDrafts[0].Content != assistantGraphCandidate {
		t.Fatal("terminal projection lost graph")
	}
	if output := os.Getenv("DESK_GRAPH_READER_OUTPUT"); output != "" {
		assistantRunMust(t, os.WriteFile(output, saved.Content, 0600))
	}
	// The existing detached-draft migration runs as part of projection/deletion.
	var deleted conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &deleted))
	deleted.Chats = []json.RawMessage{}
	w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, deleted)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	detached := policyWorkspaceRead(t, s, token, "/api/draft-graphs")
	if output := os.Getenv("DESK_GRAPH_DETACHED_OUTPUT"); output != "" {
		assistantRunMust(t, os.WriteFile(output, detached.Content, 0600))
	}
	if !bytes.Contains(detached.Content, []byte(out.Draft.DraftID)) || !bytes.Contains(detached.Content, []byte(`"originChatId"`)) {
		t.Fatal("deletion lost detached graph", string(detached.Content))
	}
}
func TestAssistantGraphToolsRealServicesAndOffers(t *testing.T) {
	for _, phase := range []string{"work", "plan"} {
		t.Run(phase, func(t *testing.T) {
			s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", phase)
			x := assistantGraphSetup(t, s, e)
			names := []string{}
			for _, tool := range x.Run.HostTools {
				names = append(names, tool.Name)
				if tool.Name == "propose_graph" {
					if tool.Effect != "propose" || tool.Delegation != "" {
						t.Fatal("proposal grants")
					}
				} else if tool.Effect != "read" || tool.Delegation != "read" {
					t.Fatal("read classification")
				}
			}
			if contains(names, "propose_graph") != (phase == "work") {
				t.Fatal("Plan proposal offer", names)
			}
			profile := contract.DelegatedTaskProfile{ID: "reader", Capabilities: []string{"read"}}
			_, _, err := x.Run.Children(t.Context(), profile, func(contract.RunEvent) {})
			assistantRunMust(t, err)
			offered := x.gate.Offer()
			child := gate.ChildPhase(profile, offered, gate.PlanPhase(offered))
			if contains(child.Host, "propose_graph") || !contains(child.Host, "read_decision") {
				t.Fatal("child offer", child.Host)
			}
			for _, row := range []struct {
				name string
				args any
				want string
			}{
				{"list_decisions", map[string]any{}, `"id":"flow"`},
				{"read_decision", map[string]string{"type": "pack", "id": "one"}, "1.20"},
				{"get_graph_authoring_instructions", map[string]any{"relationship": "first then next", "packs": []string{"one"}}, "RUNTIME GRAPH CONTRACT [{ \"pack"},
				{"graph_validate", map[string]string{"content": assistantGraphCandidate}, `"number":1.20`},
				{"graph_explain", map[string]string{"content": assistantGraphCandidate}, `"number":1.20`},
				{"graph_rehearse", map[string]string{"id": "flow", "inputs": `{"first":{"facts":{"null":null,"flag":false,"zero":0,"literal":"50","number":1.20},"evidence":{}}}`}, `"rehearsal":true`},
			} {
				result := assistantGraphInvoke(t, e, x, phase+row.name, row.name, row.args)
				if result.Err() || !strings.Contains(result.Content[0].Text(), row.want) {
					t.Fatalf("%s: %s", row.name, result.Value().Text())
				}
			}
			cli, err := os.ReadFile(filepath.Join(s.cfg.ProjectDir, "graph-cli.jsonl"))
			assistantRunMust(t, err)
			if !bytes.Contains(cli, []byte(`--rehearsal`)) || !bytes.Contains(cli, []byte(`1.20`)) {
				t.Fatal("typed rehearsal input changed", string(cli))
			}
		})
	}
}
func TestAssistantGraphActualEinoProducer(t *testing.T) {
	var step atomic.Int32
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := step.Add(1)
		var delta map[string]any
		finish := "tool_calls"
		sequence := []struct {
			name string
			args any
		}{
			{"list_decisions", map[string]any{}},
			{"read_decision", map[string]string{"type": "graph", "id": "flow"}},
			{"get_graph_authoring_instructions", map[string]any{"relationship": "first then next", "packs": []string{"one"}}},
			{"graph_validate", map[string]string{"content": assistantGraphCandidate}},
			{"graph_explain", map[string]string{"content": assistantGraphCandidate}},
			{"graph_rehearse", map[string]string{"id": "flow", "inputs": `{"first":{"facts":{"null":null,"flag":false,"zero":0,"literal":"50","number":1.20}}}`}},
			{"propose_graph", map[string]string{"id": "flow", "content": assistantGraphCandidate}},
		}
		if int(n) <= len(sequence) {
			call := sequence[n-1]
			args, _ := json.Marshal(call.args)
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "provider-call", "type": "function", "function": map[string]any{"name": call.name, "arguments": string(args)}}}}
		} else {
			delta = map[string]any{"role": "assistant", "content": "Retained graph."}
			finish = "stop"
		}

		raw, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+string(raw)+"\n\ndata: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\""+finish+"\"}]}\n\ndata: [DONE]\n\n")
	})
	s, _, e := assistantExecutionFixture(t, u.server.URL, "work")
	x := assistantGraphSetup(t, s, e)
	off := false
	x.Run.Review = &off
	// Seven serial host tools and their durable model/checkpoint steps need
	// headroom under the race detector; this is only the scripted fixture bound.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	arrivals := u.arrivals()
	if len(arrivals) == 0 || !bytes.Contains(arrivals[0].body, []byte("propose_graph")) || !bytes.Contains(arrivals[0].body, []byte("read_decision")) {
		t.Fatal("actual API offer omitted Graph tools")
	}
	record := assistantExecutionRecord(t, e)
	if len(record.DomainOutputs) != 1 || record.DomainOutputs[0].Draft.Content != assistantGraphCandidate {
		t.Fatalf("actual API producer failed: outputs=%+v events=%+v writer=%v", record.DomainOutputs, record.Events, e.writer.Err())
	}
	if record.Checkpoint.Status != "complete" {
		t.Fatal("actual API checkpoint did not finish")
	}
	assistantRunMust(t, e.writer.State(t.Context(), "complete"))
	assistantRunMust(t, s.prepareAssistantChatProjection(e.request.Clone(t.Context()), record.ID))
	assistantRunMust(t, s.writeAssistantChatProjection(e.request.Clone(t.Context()), record.ID))
	prepared := assistantExecutionRecord(t, e)
	if prepared.ChatProjection == nil || prepared.ChatProjection.Acknowledged {
		t.Fatal("write/ack fixture did not retain the unacknowledged prepared projection")
	}
	corrupt := prepared
	corrupt.DomainOutputs = append([]assistantDomainOutput(nil), prepared.DomainOutputs...)
	corrupt.DomainOutputs[0].Draft.CreatedAt = "2020-01-01T00:00:00Z"
	if validateAssistantChatProjection(corrupt) == nil {
		t.Fatal("prepared projection did not bind the exact Graph output")
	}
	// Recovery decodes the actual stored terminal receipt and performs only its
	// saved write/readback/ack; no host operation or model invocation is repeated.
	data, _ := json.Marshal(assistantRunLedger{Version: 1, Project: prepared.Binding.Project, Runs: []assistantRunRecord{prepared}})
	_, err := decodeAssistantRunStorage(e.writer.lease.store.name, data)
	assistantRunMust(t, err)
	count := step.Load()
	assistantRunMust(t, e.ProjectChat(t.Context()))
	assistantRunMust(t, e.ProjectChat(t.Context()))
	if step.Load() != count || !assistantExecutionRecord(t, e).ChatProjection.Acknowledged {
		t.Fatal("terminal ack recovery dispatched work")
	}

}
