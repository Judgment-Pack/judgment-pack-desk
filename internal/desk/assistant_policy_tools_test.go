package desk

import (
	"bytes"
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
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

const assistantPolicyNarrative = "Preserve missing, false, zero, 1.20 and \"50\" exactly.\r\n中文 <record>."

// Hand-authored private factory fixture, with the real HTTP/service/journal
// boundaries before admission. No request/model field fabricates authority.
func assistantPolicyFixture(t *testing.T, url, phase string, native bool, prepare func(*Server, string, policyReply) policyReply) (*Server, string, *assistantRunExecution, policyReply, string) {
	t.Helper()
	entered := make(chan *assistantRunExecution, 1)
	s, token, start, executor := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		entered <- e
		<-ctx.Done()
		return ctx.Err()
	})
	p := policyCreate(t, s, token)
	if prepare != nil {
		p = prepare(s, token, p)
	}
	connection := aiTestConnection(testAIAlpha, "Policy fixture", url)
	connection.Assistant.Endpoint.Tools = []string{"validate", "get_schema", "experimental_evaluate"}
	if native {
		model := "shared-model"
		connection.Assistant.Engine = "codex"
		connection.Assistant.Endpoint = nil
		connection.Assistant.Agent = &AssistantAgentConfig{Model: &model, Provider: "openai", AuthMethod: "subscription", Models: []string{"shared-model", "child-model"}, Tools: []string{"get_schema"}, Effort: "medium", ChildModel: "child-model", CriticModel: "child-model"}
		s.assistantRunExecutor = &assistantCodexExecutor{executor}
	}
	raw, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{connection}})
	assistantRunMust(t, err)
	assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
	profile := `{"profileVersion":1,"api":{"inherit":true}}`
	if native {
		profile = `{"profileVersion":1,"codex":{"inherit":true}}`
	}
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, assistantProfilePath), []byte(profile), 0600))
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	var chat map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal(doc.Chats[0], &chat))
	chat["policy"] = assistantChatJSON(map[string]string{"id": p.Document.ID})
	chat["policyRequest"] = assistantChatJSON(assistantPolicyBasis{Revision: p.Revision, Candidate: assistantPolicyFields{p.Document.Candidate.Title, p.Document.Candidate.Narrative, p.Document.Candidate.ArtifactVersion}})
	chat["interaction"] = assistantChatJSON(phase)
	for key, value := range map[string]any{"title": "Policy review", "composer": "", "pinned": false, "archived": false, "updatedAt": "2026-10-20T00:00:00Z", "mode": "draft", "view": "chat"} {
		chat[key] = assistantChatJSON(value)
	}
	doc.Chats[0] = assistantChatJSON(chat)
	if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved = policyWorkspaceRead(t, s, token, "/api/conversations")
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	start.Revision = digestOf(doc.Chats[0])
	trace := ""
	if native {
		dir := t.TempDir()
		trace = filepath.Join(dir, "trace.jsonl")
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, "peer.py"), []byte(assistantExecutionCodexPeer), 0600))
		bin := filepath.Join(dir, "codex-peer")
		wrapper := fmt.Sprintf("#!/bin/sh\nexec python3 '%s' 'delegate' '%s' \"$(cat '%s')\"\n", filepath.Join(dir, "peer.py"), trace, filepath.Join(dir, "ledger-path"))
		assistantRunMust(t, os.WriteFile(bin, []byte(wrapper), 0700))
		manager, err := codexbridge.NewManager(codexbridge.Options{Binary: bin, ProfileDir: filepath.Join(dir, "account"), ProjectDir: s.cfg.ProjectDir})
		assistantRunMust(t, err)
		t.Cleanup(manager.Close)
		s.codex = manager
		s.aiAccounts.values = map[string]providerAccountManager{testAIAlpha: manager}
	} else {
		origin, _ := endpointOrigin(url)
		assistantRunMust(t, s.assistant.storeKeyNamed(connectionKeyName(&connection), storedKey{present: true, key: "host-fixture-key", origin: origin, kind: connection.Assistant.Endpoint.Kind}))
	}
	assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	var e *assistantRunExecution
	select {
	case e = <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("Policy execution did not start")
	}
	if native {
		store, err := s.openChatData()
		assistantRunMust(t, err)
		assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "ledger-path"), []byte(filepath.Join(store.root.Name(), e.writer.lease.store.name)), 0600))
		assistantRunMust(t, store.root.Close())
	}
	t.Cleanup(func() {
		e.cancel()
		select {
		case <-e.done:
		case <-time.After(3 * time.Second):
			t.Error("Policy execution did not drain")
		}
	})
	return s, token, e, p, trace
}
func assistantPolicyOpen(t *testing.T, s *Server, e *assistantRunExecution) *assistantExecution {
	t.Helper()
	assistantExecutionRuntimeFixture(t, s, e)
	x, err := e.openAssistantExecution(t.Context(), assistantExecutionPrompt{Name: "served"})
	assistantRunMust(t, err)
	t.Cleanup(x.Close)
	off := false
	x.Run.Review = &off
	return x
}
func assistantPolicyInvoke(t *testing.T, e *assistantRunExecution, x *assistantExecution, id, name string, args any) runcontext.ToolResult {
	t.Helper()
	raw, _ := json.Marshal(args)
	assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &id, Name: name, Args: raw}}))
	ctx := context.WithValue(runcontext.WithCallID(t.Context(), id), assistantHostRootKey{}, true)
	result, err := assistantGraphFind(t, x.Run.HostTools, name).Execute(ctx, raw)
	assistantRunMust(t, err)
	return result
}
func assistantPolicySSE(w http.ResponseWriter, name string, args any, message string) {
	finish := "tool_calls"
	delta := map[string]any{"role": "assistant"}
	if name != "" {
		raw, _ := json.Marshal(args)
		delta["tool_calls"] = []any{map[string]any{"index": 0, "id": "same-provider-id", "type": "function", "function": map[string]any{"name": name, "arguments": string(raw)}}}
	} else {
		finish = "stop"
		delta["content"] = message
	}
	raw, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, "data: "+string(raw)+"\n\ndata: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\""+finish+"\"}]}\n\ndata: [DONE]\n\n")
}
func TestAssistantPolicyActualAPIProducerAndProjection(t *testing.T) {
	var step atomic.Int32
	var p policyReply
	var coverage policyCoverageReply
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := int(step.Add(1))
		sequence := []struct {
			name string
			args any
		}{
			{"read_policy", map[string]any{}},
			{"read_policy_comparison", map[string]any{"before": map[string]string{"kind": "revision", "revisionId": p.Document.Candidate.RevisionID}, "after": map[string]string{"kind": "publication", "publicationId": p.Document.Publications[0].ID}}},
			{"read_policy_coverage", map[string]string{"coverageId": coverage.Document.ID, "candidateId": coverage.Document.Candidate.ID}},
			{"propose_policy_update", map[string]string{"revision": p.Revision, "title": " Exact title ", "narrative": assistantPolicyNarrative, "artifactVersion": "1.1.0"}},
			{"propose_policy_pack_interpretation", map[string]string{"publicationId": p.Document.Publications[0].ID}},
			{"propose_policy_graph_interpretation", map[string]string{"revisionId": p.Document.Candidate.RevisionID}},
		}
		if n <= len(sequence) {
			assistantPolicySSE(w, sequence[n-1].name, sequence[n-1].args, "")
		} else {
			assistantPolicySSE(w, "", nil, "Review the Policy cards.\n\n```json\n{\"proposal\":{\"kind\":\"create\",\"document\":"+reviewPack+",\"unknowns\":[]}}\n```")
		}
	})
	s, token, e, saved, _ := assistantPolicyFixture(t, u.server.URL, "work", false, func(s *Server, token string, p policyReply) policyReply {
		p = policyApprove(t, s, token, p)
		p = policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+p.Document.ID+"/publish", policyPublishBody(t, p))).Policy
		pack := coveragePublishFixture(t, s, []byte(reviewPack))
		service, actor := coverageServiceFixture(s, token)
		out, err := service.coverageCommand(t.Context(), p.Document.ID, actor, coverageSaveBody(t, p, pack))
		assistantRunMust(t, err)
		coverage = *out.Coverage
		return policyRead(t, s, token, p.Document.ID)
	})
	p = saved
	before := policyRead(t, s, token, p.Document.ID)
	x := assistantPolicyOpen(t, s, e)
	if x.Run.Context.ArtifactProposals == nil || *x.Run.Context.ArtifactProposals {
		t.Fatal("Policy artifact boundary missing")
	}
	for _, tool := range x.Run.HostTools {
		if assistantGraphTool(tool.Name) {
			t.Fatal("Policy chat gained Graph producer", tool.Name)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	record := assistantExecutionRecord(t, e)
	if len(record.DomainOutputs) != 3 || record.Checkpoint.Status != "complete" {
		t.Fatalf("actual Policy producers incomplete: outputs=%d checkpoint=%s writer=%v", len(record.DomainOutputs), record.Checkpoint.Status, e.writer.Err())
	}
	counts := map[string]int{}
	guard := false
	for _, event := range record.Events {
		if event.Event.Proposal != nil {
			t.Fatal("ordinary artifact escaped Policy boundary")
		}
		if g := event.Event.Guardrail; g != nil && g.Tool == "proposal" {
			guard = true
		}
	}
	if !guard {
		t.Fatal("actual malicious artifact did not reach canonical refusal")
	}
	for _, out := range record.DomainOutputs {
		counts[out.Kind]++
		if out.PolicyUpdate != nil && out.PolicyUpdate.Candidate.Narrative != assistantPolicyNarrative {
			t.Fatal("Policy candidate changed")
		}
	}
	if counts["policy-update"] != 1 || counts["policy-interpretation"] != 2 {
		t.Fatal(counts)
	}
	after := policyRead(t, s, token, p.Document.ID)
	if !sameDraftValue(before, after) {
		t.Fatal("proposal mutated Policy")
	}
	assistantRunMust(t, validateAssistantDomainOutputs(record))
	// Reconstruction/redelivery uses the original receipt, not a new producer,
	// service write or model request. A new call still reads mutable state afresh.
	scope, err := readAssistantPolicyScope(e.source)
	assistantRunMust(t, err)
	registry, err := e.assistantPolicyTools(scope)
	assistantRunMust(t, err)
	for _, op := range record.Operations {
		if op.Kind == "host" && op.Name == "propose_policy_update" {
			var in assistantHostIntent
			assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
			ctx := context.WithValue(runcontext.WithCallID(t.Context(), in.CallID), assistantHostRootKey{}, true)
			again, err := assistantGraphFind(t, registry.tools, op.Name).Execute(ctx, in.Arguments)
			assistantRunMust(t, err)
			var receipt assistantHostResult
			assistantRunMust(t, readAssistantHostReceipt(op.Result, &receipt))
			if !bytes.Equal(again.Value().JSON(), receipt.Result) {
				t.Fatal("reconstruction replay changed result")
			}
		}
	}
	if step.Load() != 7 || len(assistantExecutionRecord(t, e).DomainOutputs) != 3 {
		t.Fatal("replay repeated producer/model work")
	}
	assistantRunMust(t, e.writer.State(t.Context(), "complete"))
	assistantRunMust(t, s.prepareAssistantChatProjection(e.request.Clone(t.Context()), record.ID))
	assistantRunMust(t, s.writeAssistantChatProjection(e.request.Clone(t.Context()), record.ID))
	prepared := assistantExecutionRecord(t, e)
	encoded, _ := json.Marshal(assistantRunLedger{Version: 1, Project: prepared.Binding.Project, Runs: []assistantRunRecord{prepared}})
	_, err = decodeAssistantRunStorage(e.writer.lease.store.name, encoded)
	assistantRunMust(t, err)
	assistantRunMust(t, e.ProjectChat(t.Context()))
	assistantRunMust(t, e.ProjectChat(t.Context()))
	if step.Load() != 7 || !assistantExecutionRecord(t, e).ChatProjection.Acknowledged {
		t.Fatal("terminal recovery repeated work")
	}
	projected := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc struct {
		Chats []struct {
			PolicyProposals       []assistantPolicyProposal      `json:"policyProposals"`
			PolicyInterpretations []policyInterpretationProposal `json:"policyInterpretations"`
		} `json:"chats"`
	}
	assistantRunMust(t, json.Unmarshal(projected.Content, &doc))
	if len(doc.Chats[0].PolicyProposals) != 1 || len(doc.Chats[0].PolicyInterpretations) != 2 || doc.Chats[0].PolicyProposals[0].Before.Narrative != before.Document.Candidate.Narrative {
		t.Fatal("Policy cards lost original basis")
	}
	if output := os.Getenv("DESK_POLICY_READER_OUTPUT"); output != "" {
		assistantRunMust(t, os.WriteFile(output, projected.Content, 0600))
	}
}

func TestAssistantPolicyPlanChildAndStaleBasis(t *testing.T) {
	for _, phase := range []string{"plan", "work"} {
		t.Run(phase, func(t *testing.T) {
			s, token, e, p, _ := assistantPolicyFixture(t, "http://127.0.0.1:1", phase, false, nil)
			x := assistantPolicyOpen(t, s, e)
			if len(x.Run.HostTools) == 0 {
				t.Fatal("read tools missing")
			}
			for _, tool := range x.Run.HostTools {
				if phase == "plan" && assistantHostProposal(tool.Name) {
					t.Fatal("proposal offered in Plan")
				}
			}
			if phase == "plan" {
				read := assistantPolicyInvoke(t, e, x, "plan-read", "read_policy", map[string]any{})
				if read.Err() {
					t.Fatal("Plan read refused")
				}
				registry, err := e.assistantPolicyTools(mustAssistantPolicyScope(t, e))
				assistantRunMust(t, err)
				raw, _ := json.Marshal(map[string]string{"revision": p.Revision, "title": "Changed", "narrative": "No write", "artifactVersion": "1.1.0"})
				ctx := context.WithValue(runcontext.WithCallID(t.Context(), "plan-proposal"), assistantHostRootKey{}, true)
				no, err := assistantGraphFind(t, registry.tools, "propose_policy_update").Execute(ctx, raw)
				assistantRunMust(t, err)
				if !no.Err() {
					t.Fatal("Plan direct proposal admitted")
				}
				return
			}
			args := map[string]string{"revision": p.Revision, "title": "Changed", "narrative": assistantPolicyNarrative, "artifactVersion": "1.1.0"}
			child := assistantGraphFind(t, x.Run.HostTools, "propose_policy_update")
			raw, _ := json.Marshal(args)
			no, err := child.Execute(runcontext.WithCallID(t.Context(), "child"), raw)
			assistantRunMust(t, err)
			if !no.Err() {
				t.Fatal("child proposal admitted")
			}
			edit := policyEditBody(t, p)
			edit.Narrative = "A concurrent saved edit"
			current := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+p.Document.ID, edit)).Policy
			fresh := assistantPolicyInvoke(t, e, x, "fresh-read", "read_policy", map[string]any{})
			if fresh.Err() || !strings.Contains(fresh.Value().Text(), current.Revision) {
				t.Fatal("mutable read was stale")
			}
			stale := assistantPolicyInvoke(t, e, x, "stale", "propose_policy_update", args)
			record := assistantExecutionRecord(t, e)
			if !stale.Err() || len(record.DomainOutputs) != 1 || record.DomainOutputs[0].PolicyUpdate.Check.Status != "stale" {
				t.Fatal("stale proposal lost or became savable")
			}
			assistantRunMust(t, validateAssistantDomainOutputs(record))
			cp := *record.Checkpoint
			cp.Status = "complete"
			assistantRunMust(t, e.Checkpoint(t.Context(), cp))
			assistantRunMust(t, e.writer.State(t.Context(), "complete"))
			assistantRunMust(t, e.ProjectChat(t.Context()))
			saved := policyWorkspaceRead(t, s, token, "/api/conversations")
			if !bytes.Contains(saved.Content, []byte(`"status":"stale"`)) {
				t.Fatal("stale card not retained")
			}
			if output := os.Getenv("DESK_POLICY_STALE_READER_OUTPUT"); output != "" {
				assistantRunMust(t, os.WriteFile(output, saved.Content, 0600))
			}
		})
	}
}

func assistantPolicyEvent(t *testing.T, e *assistantRunExecution, id, name string, raw json.RawMessage) {
	t.Helper()
	assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &id, Name: name, Args: raw}}))
}
