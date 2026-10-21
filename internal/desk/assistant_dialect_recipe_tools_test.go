package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// Hand-authored inputs, never captured from the implementation's output.
const assistantRecipeCandidate = `{"name":"Portable recipe","intent":"Preserve exact fields.\r\n中文 <record>&","fields":[{"name":"title","description":"Original title","type":"text"},{"name":"count","description":"Whole count","type":"integer"},{"name":"amount","description":"Exact amount","type":"decimal"},{"name":"flag","description":"Yes or no","type":"boolean"},{"name":"tags","description":"Labels","type":"text-list"},{"name":"bounds","description":"Interval","type":"range"},{"name":"legacyText","description":"Preserve old type","type":"string"},{"name":"legacyNumber","description":"Preserve old type","type":"number"}],"criteria":[{"id":"relevant","description":"Relevant to the person's request","qualificationTarget":"Review separately"}],"limits":{"records":5,"searches":3,"pages":4,"modelCalls":6},"execution":{"approach":"delegated","concurrency":2}}`

// Complete browser-owned source fields are authored before durable admission.
func assistantRecipeBrowserFixture(t *testing.T, upstreamURL string) (*Server, string, *assistantRunExecution) {
	t.Helper()
	entered := make(chan *assistantRunExecution, 1)
	s, token, start, _ := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		entered <- e
		<-ctx.Done()
		return ctx.Err()
	})
	connection := aiTestConnection(testAIAlpha, "Recipe fixture", upstreamURL)
	connection.Assistant.Endpoint.Tools = []string{"validate", "experimental_evaluate", "get_schema"}
	raw, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{connection}})
	assistantRunMust(t, err)
	assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, assistantProfilePath), []byte(`{"profileVersion":1,"api":{"inherit":true}}`), 0600))
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	var chat map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal(doc.Chats[0], &chat))
	for key, value := range map[string]any{"title": "Recipe review", "composer": "", "pinned": false, "archived": false, "updatedAt": "2026-10-20T00:00:00Z", "mode": "draft", "view": "chat"} {
		chat[key] = assistantChatJSON(value)
	}
	doc.Chats[0] = assistantChatJSON(chat)
	if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved = policyWorkspaceRead(t, s, token, "/api/conversations")
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	start.Revision = digestOf(doc.Chats[0])
	origin, _ := endpointOrigin(upstreamURL)
	assistantRunMust(t, s.assistant.storeKeyNamed(connectionKeyName(&connection), storedKey{present: true, key: "host-fixture-key", origin: origin, kind: connection.Assistant.Endpoint.Kind}))
	view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	select {
	case e := <-entered:
		if e.record.ID != view.ID {
			t.Fatal("wrong execution")
		}
		t.Cleanup(func() {
			e.cancel()
			select {
			case <-e.done:
			case <-time.After(3 * time.Second):
				t.Error("execution did not drain")
			}
		})
		return s, token, e
	case <-time.After(3 * time.Second):
		t.Fatal("execution did not start")
	}
	return nil, "", nil
}

func assistantRecipeSeed(t *testing.T, s *Server) inputPreparationReply {
	t.Helper()
	a, err := readAssistantDialectRecipeArguments("propose_dialect_recipe", []byte(assistantRecipeCandidate))
	assistantRunMust(t, err)
	d := repositoryDocument(1)
	d.Recipe = a.Content.recipe()
	d.Recipe.Research.AI = inputResearchAI{Connection: testAIAlpha, Revision: "HIDDEN-BINDING", Model: "HIDDEN-MODEL"}
	d.Recipe.Template = &inputTemplateOrigin{ID: "portable-template", Name: "Portable template", Digest: "sha256:" + strings.Repeat("a", 64), License: "MIT"}
	d.Records[0].Values["title"] = "ROW-SECRET"
	saved, err := s.preparationRepository().CompareAndSwap(t.Context(), d, "")
	assistantRunMust(t, err)
	return saved
}
func assistantRecipeInvoke(t *testing.T, e *assistantRunExecution, x *assistantExecution, call, name, raw string) runcontext.ToolResult {
	t.Helper()
	return assistantGraphInvoke(t, e, x, call, name, json.RawMessage(raw))
}
func assistantRecipeImport(t *testing.T, e *assistantRunExecution, r assistantRunRecord) error {
	t.Helper()
	raw, err := json.Marshal(assistantRunLedger{Version: 1, Project: r.Binding.Project, Runs: []assistantRunRecord{r}})
	assistantRunMust(t, err)
	_, err = decodeAssistantRunStorage(e.writer.lease.store.name, raw)
	return err
}
func assistantRecipeObserve(t *testing.T, s *Server, e *assistantRunExecution) assistantRunRecord {
	t.Helper()
	r := assistantExecutionRecord(t, e)
	if len(r.DomainOutputs) != 1 || r.DomainOutputs[0].Recipe == nil {
		t.Fatalf("recipe output absent: %+v writer=%v", r.DomainOutputs, e.writer.Err())
	}
	p := r.DomainOutputs[0].Recipe
	a, err := readAssistantDialectRecipeArguments("propose_dialect_recipe", []byte(assistantRecipeCandidate))
	assistantRunMust(t, err)
	if !sameDraftValue(p.Proposal.content(), a.Content) || p.Proposal.AfterTurnID != r.Binding.RequestID || p.Proposal.Attribution.Connection != r.Binding.ConnectionID || p.Proposal.Attribution.Model != r.Binding.Model {
		t.Fatal("original content or metadata changed")
	}
	saved, err := s.draftRepository().Get(t.Context(), p.Retained.ID)
	assistantRunMust(t, err)
	if !validAssistantDialectRecipeRetained(r.Binding, p.Proposal, saved) || saved.SHA256 != p.Retained.SHA256 || saved.Document.Recipe.Research.Limits.Searches != 0 || saved.Document.Recipe.Research.AI.Connection != "" {
		t.Fatal("actual independent draft differs")
	}
	assistantRunMust(t, assistantRecipeImport(t, e, r))
	return r
}
func TestAssistantDialectRecipeActualAPIProducer(t *testing.T) {
	var step atomic.Int32
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := step.Add(1)
		finish := "tool_calls"
		var delta map[string]any
		names := []string{"get_dialect_recipe_instructions", "list_dialects", "read_dialect_recipe", "propose_dialect_recipe"}
		args := []string{`{}`, `{}`, `{"id":"prep_00000000000000000000000000000001"}`, assistantRecipeCandidate}
		if n <= 4 {
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "same-provider-call", "type": "function", "function": map[string]any{"name": names[n-1], "arguments": args[n-1]}}}}
		} else {
			delta = map[string]any{"role": "assistant", "content": "Review the retained recipe; research has not started."}
			finish = "stop"
		}
		raw, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+string(raw)+"\n\ndata: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\""+finish+"\"}]}\n\ndata: [DONE]\n\n")
	})
	s, token, e := assistantRecipeBrowserFixture(t, u.server.URL)
	assistantRecipeSeed(t, s)
	x := assistantExecutionOpen(t, s, e)
	off := false
	x.Run.Review = &off
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	if step.Load() != 5 {
		t.Fatal("actual four-tool loop did not finish", step.Load())
	}
	record := assistantRecipeObserve(t, s, e)
	if record.Checkpoint.Status != "complete" {
		t.Fatal("actual checkpoint unfinished")
	}
	reads := 0
	for _, op := range record.Operations {
		if op.Kind == "host" {
			reads++
			var held assistantHostResult
			assistantRunMust(t, readAssistantHostReceipt(op.Result, &held))
			text := string(held.Result)
			if strings.Contains(text, "ROW-SECRET") || strings.Contains(text, "HIDDEN-BINDING") || strings.Contains(text, "HIDDEN-MODEL") {
				t.Fatal("private data exposed")
			}
		}
	}
	if reads != 4 {
		t.Fatal("tools absent", reads)
	}
	for _, name := range assistantDialectRecipeNames {
		if !bytes.Contains(u.arrivals()[0].body, []byte(name)) {
			t.Fatal("actual offer missing", name)
		}
	}
	assistantRunMust(t, e.writer.State(t.Context(), "complete"))
	assistantRunMust(t, s.prepareAssistantChatProjection(e.request.Clone(t.Context()), record.ID))
	assistantRunMust(t, s.writeAssistantChatProjection(e.request.Clone(t.Context()), record.ID))
	// Lost projection acknowledgement recovers by readback only.
	calls := step.Load()
	assistantRunMust(t, e.ProjectChat(t.Context()))
	assistantRunMust(t, e.ProjectChat(t.Context()))
	if step.Load() != calls || !assistantExecutionRecord(t, e).ChatProjection.Acknowledged {
		t.Fatal("ack recovery repeated model work")
	}
	conversations := policyWorkspaceRead(t, s, token, "/api/conversations")
	if !bytes.Contains(conversations.Content, []byte(`"inputDrafts"`)) || !bytes.Contains(conversations.Content, []byte(`"type":"decimal"`)) {
		t.Fatal("proposal absent from actual saved chat")
	}
	output := record.DomainOutputs[0].Recipe
	reply := policyCall(s, token, "GET", "/api/dialect-drafts/"+output.Retained.ID, nil)
	if reply.Code != 200 {
		t.Fatal(reply.Body.String())
	}
	if path := os.Getenv("DESK_DIALECT_RECIPE_READER_CHAT"); path != "" {
		assistantRunMust(t, os.WriteFile(path, conversations.Content, 0600))
	}
	if path := os.Getenv("DESK_DIALECT_RECIPE_READER_DRAFT"); path != "" {
		assistantRunMust(t, os.WriteFile(path, reply.Body.Bytes(), 0600))
	}
	var empty conversationDocument
	assistantRunMust(t, json.Unmarshal(conversations.Content, &empty))
	empty.Chats = []json.RawMessage{}
	if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", conversations.SHA256, empty); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	after := policyCall(s, token, "GET", "/api/dialect-drafts/"+output.Retained.ID, nil)
	if after.Code != 200 || !bytes.Equal(after.Body.Bytes(), reply.Body.Bytes()) {
		t.Fatal("independent draft lost with chat")
	}
	items, err := s.preparationRepository().List(t.Context())
	assistantRunMust(t, err)
	if len(items) != 1 {
		t.Fatal("proposal saved a preparation")
	}
}
func TestAssistantDialectRecipeActualNativeParentChild(t *testing.T) {
	s, _, e, _, trace := assistantCodexFixture(t, "delegate", "work")
	assistantRecipeSeed(t, s)
	peer := strings.Replace(assistantExecutionCodexPeer, "if mode=='delegate' and index==1:\n   tool(offered('delegate_tasks'),{'tasks':[{'title':'Inspect schema','objective':'Inspect the schema only.','profile':'review'}]})", `if mode=='delegate' and index==1: tool(offered('read_dialect_recipe'),{'id':'prep_00000000000000000000000000000001'},call='same-provider-call')
  elif mode=='delegate' and index==2: tool(offered('read_dialect_recipe'),{'id':'prep_00000000000000000000000000000001'},call='same-provider-call')
  elif mode=='delegate' and index==3: tool(offered('propose_dialect_recipe'),json.loads(`+fmt.Sprintf("%q", assistantRecipeCandidate)+`),call='same-provider-call')`, 1)
	peer = strings.Replace(peer, "if mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')", `if mode=='delegate' and index==1: tool(offered('delegate_tasks'),{'tasks':[{'title':'Read recipe','objective':'Read the declared recipe only.','profile':'review'}]},call='delegate',rpc='rpc-delegate')
  elif mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')`, 1)
	assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
	x := assistantExecutionOpen(t, s, e)
	off := false
	x.Run.Review = &off
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	r := assistantRecipeObserve(t, s, e)
	roots := map[bool]int{}
	callbacks := map[string]bool{}
	for _, op := range r.Operations {
		if op.Kind == "host" {
			var in assistantHostIntent
			assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
			roots[in.Root]++
			if in.CallbackID == "" || callbacks[in.CallbackID] {
				t.Fatal("callback identity collapsed")
			}
			callbacks[in.CallbackID] = true
		}
	}
	if roots[true] != 2 || roots[false] != 1 {
		t.Fatal("native scopes changed", roots)
	}
}

func TestAssistantDialectRecipeActualAPIUncertaintyStops(t *testing.T) {
	var step atomic.Int32
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := step.Add(1)
		finish := "stop"
		delta := map[string]any{"role": "assistant", "content": "No more work."}
		if n == 1 {
			finish = "tool_calls"
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "unknown-write", "type": "function", "function": map[string]any{"name": "propose_dialect_recipe", "arguments": assistantRecipeCandidate}}}}
		}
		raw, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+string(raw)+"\n\ndata: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\""+finish+"\"}]}\n\ndata: [DONE]\n\n")
	})
	s, token, e := assistantExecutionFixture(t, u.server.URL, "work")
	x := assistantExecutionOpen(t, s, e)
	off := false
	x.Run.Review = &off
	actual := s.draftRepository()
	fault := &assistantRecipeDraftFault{dialectDraftRepository: actual, fault: errors.New("fixture create acknowledgement unavailable")}
	s.dialectDrafts = fault
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_ = x.Adapter.Run(ctx, x.Run)
	if step.Load() != 1 || fault.writes.Load() != 1 || e.ctx.Err() == nil {
		t.Fatal("uncertain recipe continued automatic model work", step.Load(), fault.writes.Load(), e.ctx.Err())
	}
	select {
	case <-e.done:
	case <-time.After(3 * time.Second):
		t.Fatal("uncertain execution did not stop")
	}
	r := assistantExecutionRecord(t, e)
	if w := policyCall(s, token, "GET", "/api/assistant-runs/"+r.ID, nil); w.Code != 200 {
		t.Fatal("retained run inspection unavailable", w.Body.String())
	}
	_ = policyWorkspaceRead(t, s, token, "/api/conversations")
	if len(r.DomainOutputs) != 0 {
		t.Fatal("uncertain write exposed completed output")
	}
	drafts, err := actual.List(t.Context())
	assistantRunMust(t, err)
	if len(drafts) != 1 {
		t.Fatal("fixture did not physically retain the uncertain draft")
	}
	found := false
	for _, op := range r.Operations {
		if op.Kind == "host" && op.Name == "propose_dialect_recipe" {
			found = true
			if op.State != "refused" || len(op.Result) == 0 {
				t.Fatal("stop discarded the observed refusal")
			}
			var receipt assistantHostResult
			assistantRunMust(t, readAssistantHostReceipt(op.Result, &receipt))
			value, err := runcontext.ParseJSON(receipt.Result)
			assistantRunMust(t, err)
			held := runcontext.ReadToolResult(value)
			if held.StructuredContent == nil {
				t.Fatal("typed uncertain outcome missing")
			}
			known := held.StructuredContent.Get("failure")
			if known.Get("code").Str() != "storage" || known.Get("source").Str() != "storage" || known.Get("outcome").Str() != "unknown" || !strings.Contains(known.Get("detail").Str(), "outcome is unknown") {
				t.Fatal("uncertain outcome mislabeled", string(receipt.Result))
			}
		}
	}
	if !found {
		t.Fatal("uncertain operation missing")
	}
}
