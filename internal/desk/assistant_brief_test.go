package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	agentadapter "github.com/Judgment-Pack/judgment-pack-agent/go/adapter/agent"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/eino"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

var assistantBriefFixtureText = BriefText{Context: "Frozen context <&> 한글", Findings: "Historical findings.", Uncertainty: "Evidence remains unverified.", NextAction: "Review the saved sources."}

type assistantBriefTestExecutor struct {
	assistantBriefExecutor
	execute func(context.Context, *assistantRunExecution) error
	calls   atomic.Int32
}

func (x *assistantBriefTestExecutor) Execute(ctx context.Context, e *assistantRunExecution) error {
	x.calls.Add(1)
	return x.execute(ctx, e)
}

func assistantBriefTestComplete(ctx context.Context, e *assistantRunExecution) error {
	raw, _ := json.Marshal(assistantBriefFixtureText)
	var document map[string]json.RawMessage
	_ = json.Unmarshal(raw, &document)
	if err := e.Event(ctx, "fixture-brief-proposal", contract.RunEvent{Proposal: &contract.RunEventProposal{Type: "proposal", Document: document, Unknowns: []string{}}}); err != nil {
		return err
	}
	cp := *e.record.Checkpoint
	cp.Status = "complete"
	return e.Checkpoint(ctx, cp)
}

func assistantBriefCall(s *Server, token, method, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	if body == nil {
		raw = nil
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set(aiConnectionHeader, testAIAlpha)
	registry, _ := s.readAIRegistry()
	if len(registry.Connections) != 0 {
		r.Header.Set(aiRevisionHeader, registry.Connections[0].Revision)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func assistantBriefFixture(t *testing.T, upstream string) (*Server, string) {
	t.Helper()
	s, token := policyTestServer(t)
	connection := aiTestConnection(testAIAlpha, "Brief fixture", upstream)
	// Configured broad tools and subordinate models must confer no brief powers.
	connection.Assistant.Endpoint.Tools = []string{"validate", "get_schema", "experimental_evaluate"}
	connection.Assistant.Endpoint.Models = append(connection.Assistant.Endpoint.Models, "child-model")
	connection.Assistant.Endpoint.CriticModel, connection.Assistant.Endpoint.ChildModel = "child-model", "child-model"
	raw, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{connection}})
	assistantRunMust(t, err)
	assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
	origin, _ := endpointOrigin(upstream)
	assistantRunMust(t, s.assistant.storeKeyNamed(connectionKeyName(&connection), storedKey{present: true, key: "fixture-key", origin: origin, kind: connection.Assistant.Endpoint.Kind}))
	writeProjectFile(t, s.projectDir, "jpack.json", `{"packs":{"one":{"path":"one.pack.json"}},"graphs":{"flow":{"path":"flow.graph.json"}}}`)
	writeProjectFile(t, s.projectDir, "one.pack.json", `{"title":"Frozen <&> 한글","version":"1","evidenceRequirements":[{"id":"evidence","required":true}]}`)
	writeProjectFile(t, s.projectDir, "flow.graph.json", `{"nodes":{"node":{"pack":"one"}},"edges":[],"result":"node"}`)
	// A brief must never launch the configured Runtime executable.
	s.cfg.JpackBin = filepath.Join(t.TempDir(), "runtime-must-not-start")
	if _, ok := s.assistantRunExecutor.(*assistantProductionExecutor); !ok {
		t.Fatal("normal Server lacks the production executor")
	}
	return s, token
}

func assistantBriefBegin(t *testing.T, s *Server, token, subject string) assistantRunStart {
	t.Helper()
	var snapshot json.RawMessage
	if definitionBriefSubject(subject) {
		reply := policyWorkspaceRead(t, s, token, "/api/briefs?subject="+subject)
		snapshot = reply.Snapshot
	} else {
		lock, err := s.privateDataLockContext(t.Context(), true)
		assistantRunMust(t, err)
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		reply, err := s.assistantBriefRead(req, subject)
		_ = lock.Close()
		assistantRunMust(t, err)
		snapshot = reply.Snapshot
	}
	command := BriefCommand{Action: "begin", Subject: subject, Snapshot: snapshot, Model: "shared-model"}
	if strings.HasPrefix(subject, "case:") {
		parts := strings.Split(subject, ":")
		command.Owner, command.CaseID = parts[1], parts[2]
	}
	var w *httptest.ResponseRecorder
	if assistantBriefJob.MatchString(subject) {
		w = assistantBriefCall(s, token, "POST", "/api/operations/"+assistantBriefRunnerPath(subject), command)
	} else {
		w = policyWorkspaceCall(s, token, "POST", "/api/briefs", "", command)
	}
	if w.Code != 200 {
		t.Fatal("brief begin", w.Code, w.Body.String())
	}
	var reply assistantBriefReply
	assistantRunMust(t, json.Unmarshal(w.Body.Bytes(), &reply))
	pending := reply.Content.Subjects[subject].Pending
	if pending == nil {
		t.Fatal("missing reservation")
	}
	w = assistantBriefCall(s, token, "POST", "/api/assistant-briefs/reference", assistantBriefReferenceRequest{Subject: subject, Token: pending.Token})
	if w.Code != 200 {
		t.Fatal("brief reference", w.Code, w.Body.String())
	}
	var reference assistantBriefReferenceReply
	assistantRunMust(t, json.Unmarshal(w.Body.Bytes(), &reference))
	if reference.Run != nil || reference.Start.Revision != digestOf(pending.Snapshot) || reference.Start.AdmissionID != "brief-"+pending.Token {
		t.Fatal("reference did not freeze exact server bytes")
	}
	return reference.Start
}

func assistantBriefSSE(w http.ResponseWriter, text, finish string) {
	w.Header().Set("Content-Type", "text/event-stream")
	first, _ := json.Marshal(map[string]any{"id": "brief", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": text}, "finish_reason": nil}}})
	last, _ := json.Marshal(map[string]any{"id": "brief", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}})
	_, _ = fmt.Fprintf(w, "data: %s\n\ndata: %s\n\ndata: [DONE]\n\n", first, last)
}

func assistantBriefProposal(text BriefText) string {
	raw, _ := json.Marshal(map[string]any{"proposal": map[string]any{"document": text, "unknowns": []string{}}})
	return "```json\n" + string(raw) + "\n```"
}

func assistantBriefProbe(t *testing.T, s *Server, token, label string, start assistantRunStart, run assistantRunView, receipt assistantBriefReply) {
	t.Helper()
	if dir := os.Getenv("JPS_BRIEF_OUTPUTS"); dir != "" {
		raw, err := json.Marshal(map[string]any{"start": start, "run": run, "receipt": receipt})
		assistantRunMust(t, err)
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, label+".json"), raw, 0600))
		stream := assistantBriefCall(s, token, "GET", "/api/assistant-runs/"+run.ID+"/events", nil)
		if stream.Code != 200 || !strings.HasPrefix(stream.Header().Get("Content-Type"), "text/event-stream") {
			t.Fatal("brief producer stream", stream.Code, stream.Body.String())
		}
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, label+".sse"), stream.Body.Bytes(), 0600))
	}
}

func TestAssistantBriefOutputContract(t *testing.T) {
	raw, _ := json.Marshal(assistantBriefFixtureText)
	var document map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal(raw, &document))
	valid := func() assistantRunRecord {
		return assistantRunRecord{Binding: assistantRunBinding{Owner: assistantRunOwner{Kind: "brief"}}, Checkpoint: &contract.Checkpoint{Status: "complete"}, Events: []assistantRunEvent{{Event: contract.RunEvent{Proposal: &contract.RunEventProposal{Type: "proposal", Document: document, Unknowns: []string{}}}}}}
	}
	if got, err := assistantBriefOutput(valid()); err != nil || got != assistantBriefFixtureText {
		t.Fatal(got, err)
	}
	for _, name := range []string{"empty", "truncated", "extra", "wrong-type", "oversized", "duplicate", "error", "foreign-owner"} {
		t.Run(name, func(t *testing.T) {
			run := valid()
			run.Events = append([]assistantRunEvent(nil), run.Events...)
			proposal := *run.Events[0].Event.Proposal
			proposal.Document = map[string]json.RawMessage{}
			for k, v := range document {
				proposal.Document[k] = v
			}
			run.Events[0].Event.Proposal = &proposal
			switch name {
			case "empty":
				proposal.Document["context"] = json.RawMessage(`" "`)
			case "truncated":
				run.Checkpoint.Status = "stopped"
			case "extra":
				proposal.Document["approval"] = json.RawMessage(`true`)
			case "wrong-type":
				proposal.Document["context"] = json.RawMessage(`{}`)
			case "oversized":
				proposal.Document["findings"], _ = json.Marshal(strings.Repeat("x", 3001))
			case "duplicate":
				run.Events = append(run.Events, run.Events[0])
			case "error":
				run.Events = append(run.Events, assistantRunEvent{Event: contract.RunEvent{Error: &contract.RunEventError{Type: "error", Message: "Stopped"}}})
			case "foreign-owner":
				run.Binding.Owner.Kind = "chat"
			}
			if _, err := assistantBriefOutput(run); err == nil {
				t.Fatal("invalid output became commit authority")
			}
		})
	}
}

func TestAssistantBriefSubjectBounds(t *testing.T) {
	d := strings.Repeat("a", 64)
	base := assistantRunBinding{Principal: d, Session: d, Policy: d, Project: d, Owner: assistantRunOwner{Kind: "brief", Revision: d}, RequestID: strings.Repeat("1", 32), RequestDigest: d, ConnectionID: testAIAlpha, ConnectionRevision: d, ProfileRevision: d, ExecutionKey: d, Model: "shared-model", Phase: "work", Unit: "model-requests"}
	for _, length := range []int{200, 201, 1024, 1025} {
		subject := "pack:" + strings.Repeat("x", length-5)
		if assistantBriefSubject(subject) != (length <= 1024) {
			t.Fatal("brief subject byte boundary", length)
		}
		for _, kind := range []string{"brief", "chat", "pane"} {
			binding := base
			binding.Owner.Kind, binding.Owner.ID = kind, subject
			accepted := validateAssistantRunBinding(binding, d) == nil
			want := length <= 200 || kind == "brief" && length <= 1024
			if accepted != want {
				t.Fatal("owner byte boundary", kind, length, accepted)
			}
		}
	}
	unicode := "pack:" + strings.Repeat("한", 339) + "aa"
	if len(unicode) != 1024 || !assistantBriefSubject(unicode) || assistantBriefSubject(unicode+"a") {
		t.Fatal("Unicode subject used code units instead of UTF8 bytes")
	}
	request := httptest.NewRequest("POST", "/", strings.NewReader(`{"subject":`+strconv.QuoteToASCII(unicode)+`,"token":"`+strings.Repeat("1", 32)+`"}`))
	var decoded assistantBriefReferenceRequest
	if assistantRunRequest(request, &decoded) != nil || decoded.Subject != unicode || !assistantBriefSubject(decoded.Subject) {
		t.Fatal("escaped Unicode changed subject bound")
	}
	for _, subject := range []string{"pack:", "case::", "/private/path", "pack:one\nother"} {
		if assistantBriefSubject(subject) {
			t.Fatal("invalid subject grammar", subject)
		}
	}
}

func TestAssistantBriefVerifiedSourceProjection(t *testing.T) {
	ref := assistantDocumentReference{ID: "11111111-1111-1111-1111-111111111111", Digest: "sha256:" + strings.Repeat("a", 64), Pages: []int{1, 3}, AllowPartial: true}
	raw, _ := json.Marshal(map[string]any{"kind": "case", "record": map[string]any{"case": map[string]any{"sources": []any{map[string]any{"id": ref.ID, "text": "", "document": ref}}}}})
	source := assistantRunSource{Original: raw}
	if !assistantBriefDocumentReferenced(raw, ref) {
		t.Fatal("selected saved reference lost")
	}
	changed := ref
	changed.AllowPartial = false
	if assistantBriefDocumentReferenced(raw, changed) {
		t.Fatal("partial consent was substituted")
	}
	changed = ref
	changed.Pages = []int{2}
	if assistantBriefDocumentReferenced(raw, changed) {
		t.Fatal("unselected page was substituted")
	}
	held := assistantVerifiedDocument{Name: "Retained image extraction", reference: ref, Partial: true, record: assistantDocumentRecord{pages: make([]assistantDocumentRecordPage, 3)}, Pages: []assistantDocumentPage{{Page: 1, Citation: "exact-citation/1", Text: "Exact image text <&> 한글"}, {Page: 3, Citation: "exact-citation/3", Text: "Exact selected page"}}}
	prompt := assistantBriefVerifiedPrompt(source, []assistantVerifiedDocument{held})
	if !strings.Contains(prompt, `"partial":true`) || !strings.Contains(prompt, `"totalPages":3`) || !strings.Contains(prompt, "exact-citation/1") || !strings.Contains(prompt, "한글") || strings.Contains(prompt, `"page":2`) {
		t.Fatal("verified source selection or evidence gaps changed", prompt)
	}
	var snapshot map[string]any
	_ = json.Unmarshal(raw, &snapshot)
	sources := snapshot["record"].(map[string]any)["case"].(map[string]any)["sources"].([]any)
	sources[0].(map[string]any)["text"] = "unverified unselected prose"
	forged, _ := json.Marshal(snapshot)
	if assistantBriefDocumentReferenced(forged, ref) {
		t.Fatal("browser-extracted prose became verified document context")
	}
}

// Published adapter/context parity without a listener or a storage fixture.
// The physical Desk transports are exercised separately by the actual probes.
func TestAssistantBriefPublishedAdapter(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprint(native), func(t *testing.T) {
			connection := aiTestConnection(testAIAlpha, "Brief fixture", "https://example.invalid")
			if native {
				model := "shared-model"
				connection.Assistant.Engine = "codex"
				connection.Assistant.Endpoint = nil
				connection.Assistant.Agent = &AssistantAgentConfig{Model: &model, Models: []string{model}, Effort: "medium"}
			}
			source := assistantRunSource{Start: assistantRunStart{Purpose: "brief", Revision: strings.Repeat("a", 64)}, Binding: assistantRunBinding{Owner: assistantRunOwner{Kind: "brief"}, ConnectionID: testAIAlpha, Model: "shared-model", Phase: "work"}, Original: json.RawMessage(`{"kind":"pack","title":"Frozen","pack":"{\"version\":\"1\"}","record":{}}`), Text: assistantBriefInstruction, Connection: connection, Profile: assistantProfile{Version: 1}}
			cp, err := (&assistantBriefExecutor{}).Prepare(t.Context(), source)
			assistantRunMust(t, err)
			input, err := assistantBriefInput(source)
			assistantRunMust(t, err)
			input.Execution = &cp
			input.Checkpoint = func(_ context.Context, latest contract.Checkpoint) error { cp = latest; return nil }
			var events []assistantRunEvent
			calls := 0
			run := &adapter.Run{Purpose: "brief", ModelName: "shared-model", Context: input, Prompt: assistantBriefPrompt(source), Critic: "off", Emit: func(event contract.RunEvent) {
				if event.ExecutionCheckpoint != nil {
					cp = event.ExecutionCheckpoint.Checkpoint
				}
				events = append(events, assistantRunEvent{Event: event})
			}}
			var executor adapter.Adapter = eino.New()
			if native {
				run.Agent = &adapter.Agent{Model: "shared-model", Effort: "medium", Run: func(ctx context.Context, request adapter.AgentRequest, callbacks adapter.AgentCallbacks) error {
					calls++
					if request.Model != "shared-model" {
						t.Error("reserved native model changed", request.Model)
					}
					if !strings.Contains(request.Prompt, "Frozen") {
						t.Error("frozen source absent")
					}
					return callbacks.Event(ctx, adapter.AgentEvent{Type: "message", ID: "brief-message", Text: assistantBriefProposal(assistantBriefFixtureText), Phase: "final"})
				}}
				executor = agentadapter.New()
			} else {
				run.Wire = "openai-compatible"
				run.Thinking = "off"
				run.Model = func(_ context.Context, _ string, request relay.Request) (*http.Response, error) {
					calls++
					var body struct {
						Model string `json:"model"`
					}
					_ = json.Unmarshal(request.Body, &body)
					if body.Model != "shared-model" {
						t.Error("reserved API model changed", body.Model)
					}
					if !strings.Contains(string(request.Body), "Frozen") {
						t.Error("frozen source absent")
					}
					w := httptest.NewRecorder()
					assistantBriefSSE(w, assistantBriefProposal(assistantBriefFixtureText), "stop")
					return w.Result(), nil
				}
			}
			assistantRunMust(t, executor.Run(t.Context(), run))
			result := assistantRunRecord{Binding: source.Binding, Checkpoint: &cp, Events: events}
			if text, err := assistantBriefOutput(result); err != nil || text != assistantBriefFixtureText {
				var failures []string
				for _, saved := range events {
					if saved.Event.Error != nil {
						failures = append(failures, saved.Event.Error.Message)
					}
				}
				t.Fatal("published brief output", err, "checkpoint", cp.Status, "failures", failures)
			}
			if calls != 1 {
				t.Fatal("unexpected fixture dispatches", calls)
			}
			if native && cp.Unit != "provider-managed" || !native && cp.Unit != "model-requests" {
				t.Fatal("purpose changed accounting", cp.Unit)
			}
		})
	}
}

func TestAssistantBriefActualAPI(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		var request struct {
			Model string `json:"model"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		_ = json.Unmarshal(raw, &request)
		if request.Model != "shared-model" {
			t.Error("reserved API model changed", request.Model)
		}
		if !strings.Contains(string(raw), "Frozen") || !strings.Contains(string(raw), "historical") {
			t.Error("frozen source or brief method absent", string(raw))
		}
		for _, tool := range request.Tools {
			if contains([]string{"experimental_evaluate", "get_schema", "delegate_tasks", "ask_user", "propose_plan"}, tool.Function.Name) {
				t.Error("forbidden brief tool", tool.Function.Name)
			}
		}
		assistantBriefSSE(w, assistantBriefProposal(assistantBriefFixtureText), "stop")
	}))
	defer provider.Close()
	for _, subject := range []string{"pack:one", "graph:flow", "pack-draft:draft-one", "graph-draft:draft-one", "case:one:case-one"} {
		t.Run(subject, func(t *testing.T) {
			s, token := assistantBriefFixture(t, provider.URL)
			if subject == "pack-draft:draft-one" {
				var drafts any
				_ = json.Unmarshal([]byte(`{"version":1,"drafts":[{"id":"draft-one","title":"Frozen draft","checkpoint":{"state":{"candidates":[{"text":"{\"title\":\"Frozen draft\",\"version\":\"1\"}","revision":1}],"unknowns":[],"cases":[]}}}],"deleted":[]}`), &drafts)
				if w := policyWorkspaceCall(s, token, "PUT", "/api/draft-packs", "absent", drafts); w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			if subject == "graph-draft:draft-one" {
				drafts := graphDraftDocument{Version: 1, Drafts: []graphDraft{{DraftID: "draft-one", ID: "draft-flow", OriginChatID: "origin", Path: "draft.graph.json", Content: `{"nodes":{},"edges":[]}`, Description: "Frozen graph draft", CreatedAt: "2026-10-23T00:00:00Z"}}, Deleted: []string{}}
				if w := policyWorkspaceCall(s, token, "PUT", "/api/draft-graphs", "absent", drafts); w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			if strings.HasPrefix(subject, "case:") {
				w := policyWorkspaceCall(s, token, "PUT", "/api/pack-tests", "absent", map[string]any{"version": 1, "suites": map[string]any{"one": map[string]any{"revision": 1, "cases": []any{map[string]any{"id": "case-one", "name": "Saved case", "revision": 1, "row": map[string]any{"facts": map[string]any{}}, "sources": []any{}, "sourceMappings": map[string]any{}, "origin": "manual", "rationale": ""}}, "runs": []any{}, "recovered": []any{}}}})
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			start := assistantBriefBegin(t, s, token, subject)
			before := calls.Load()
			first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 201)
			complete := assistantProtocolWait(t, s, token, first.ID, "complete")
			if w := assistantBriefCall(s, token, "GET", "/api/assistant-runs/"+first.ID+"/brief", nil); w.Code != 409 {
				t.Fatal("unsaved receipt became a write", w.Code)
			}
			if complete.Owner.Kind != "brief" || complete.Owner.ID != subject || calls.Load() != before+1 {
				t.Fatal("wrong owner or duplicate dispatch", complete.Owner, calls.Load()-before)
			}
			for _, op := range complete.Operations {
				if op.Kind != "model" {
					t.Fatal("brief obtained domain capability", op)
				}
			}
			if w := assistantBriefCall(s, token, "POST", "/api/assistant-runs/"+first.ID+"/brief", map[string]any{"token": start.RequestID, "text": assistantBriefFixtureText}); w.Code != 400 {
				t.Fatal("client authored sections accepted", w.Code)
			}
			commit := func() assistantBriefReply {
				w := assistantBriefCall(s, token, "POST", "/api/assistant-runs/"+first.ID+"/brief", map[string]string{"token": start.RequestID})
				if w.Code != 200 {
					t.Fatal("commit", w.Code, w.Body.String())
				}
				var reply assistantBriefReply
				assistantRunMust(t, json.Unmarshal(w.Body.Bytes(), &reply))
				return reply
			}
			saved := commit()
			assistantBriefProbe(t, s, token, "api-"+strings.ReplaceAll(subject, ":", "-"), start, complete, saved)
			if w := assistantBriefCall(s, token, "GET", "/api/assistant-runs/"+first.ID+"/brief", nil); w.Code != 200 {
				t.Fatal("exact saved receipt unavailable", w.Code, w.Body.String())
			}
			if revisions := saved.Content.Subjects[subject].Revisions; len(revisions) != 1 || revisions[0].Text != assistantBriefFixtureText {
				t.Fatal("wrong saved output", revisions)
			}
			// Lose the save response, change source, then reconcile the same saved
			// revision. Neither another provider request nor another revision occurs.
			writeProjectFile(t, s.projectDir, "one.pack.json", `{"version":"changed"}`)
			if len(commit().Content.Subjects[subject].Revisions) != 1 {
				t.Fatal("lost ack duplicated revision")
			}
			retry := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 200)
			if retry.ID != first.ID || calls.Load() != before+1 {
				t.Fatal("lost start ack replayed generation")
			}
		})
	}
}

func TestAssistantBriefActualNative(t *testing.T) {
	s, token := assistantBriefFixture(t, "https://example.invalid")
	registry, err := s.readAIRegistry()
	assistantRunMust(t, err)
	model := "shared-model"
	registry.Connections[0].Assistant.Engine = "codex"
	registry.Connections[0].Assistant.Endpoint = nil
	registry.Connections[0].Assistant.Agent = &AssistantAgentConfig{Provider: "openai", AuthMethod: "subscription", Model: &model, Models: []string{model}, Effort: "medium", Tools: []string{"get_schema", "experimental_evaluate"}}
	raw, err := encodeAIRegistry(registry.aiRegistry)
	assistantRunMust(t, err)
	assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
	start := assistantBriefBegin(t, s, token, "pack:one")
	fixture := t.TempDir()
	binary, trace := filepath.Join(fixture, "codex-peer"), filepath.Join(fixture, "trace.jsonl")
	peer := strings.Replace(assistantExecutionCodexPeer, "text='Fixture answer.'", "text="+fmt.Sprintf("%q", assistantBriefProposal(assistantBriefFixtureText)), 1)
	peer = strings.Replace(peer, "else: tool(offered('get_schema'),{})", "elif mode=='brief': finish()\n  else: tool(offered('get_schema'),{})", 1)
	assistantRunMust(t, os.WriteFile(filepath.Join(fixture, "peer.py"), []byte(peer), 0600))
	ledger := s.assistantRuns()
	data, err := s.openChatData()
	assistantRunMust(t, err)
	ledgerPath := filepath.Join(data.root.Name(), ledger.name)
	assistantRunMust(t, data.root.Close())
	wrapper := fmt.Sprintf("#!/bin/sh\nexec python3 '%s' brief '%s' '%s'\n", filepath.Join(fixture, "peer.py"), trace, ledgerPath)
	assistantRunMust(t, os.WriteFile(binary, []byte(wrapper), 0700))
	manager, err := codexbridge.NewManager(codexbridge.Options{Binary: binary, ProfileDir: filepath.Join(fixture, "account"), ProjectDir: s.cfg.ProjectDir})
	assistantRunMust(t, err)
	t.Cleanup(manager.Close)
	s.codex = manager
	s.aiAccounts.values = map[string]providerAccountManager{testAIAlpha: manager}
	first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 201)
	complete := assistantProtocolWait(t, s, token, first.ID, "complete")
	if complete.Checkpoint.Unit != "provider-managed" {
		t.Fatal("native accounting changed")
	}
	var generations int
	for _, row := range assistantCodexLog(t, trace) {
		if string(row["type"]) == `"generation"` {
			generations++
			if !strings.Contains(string(row["prompt"]), "Frozen") || strings.Contains(string(row["request"]), "experimental_evaluate") {
				t.Fatal("wrong native source or tool offer", string(row["request"]))
			}
		}
	}
	if generations != 1 {
		t.Fatal("native brief repeated", generations)
	}
	if w := assistantBriefCall(s, token, "POST", "/api/assistant-runs/"+first.ID+"/brief", map[string]string{"token": start.RequestID}); w.Code != 200 {
		t.Fatal("native commit", w.Code, w.Body.String())
	} else {
		var receipt assistantBriefReply
		assistantRunMust(t, json.Unmarshal(w.Body.Bytes(), &receipt))
		assistantBriefProbe(t, s, token, "native-pack-one", start, complete, receipt)
	}
}

func TestAssistantBriefActualRunnerSubjects(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assistantBriefSSE(w, assistantBriefProposal(assistantBriefFixtureText), "stop")
	}))
	defer provider.Close()
	for _, kind := range []string{"job", "run"} {
		t.Run(kind, func(t *testing.T) {
			s, token := assistantBriefFixture(t, provider.URL)
			subject := kind + ":" + kind + "_" + strings.Repeat("1", 32)
			snapshot, _ := json.Marshal(map[string]any{"kind": kind, "title": "Frozen operational record", "pack": `{"version":"historical"}`, "record": map[string]any{"releaseId": "retained-release", "sample": map[string]any{"evidence": map[string]string{"evidence": "unknown"}}}})
			var mu sync.Mutex
			stored := BriefStore{Version: 1, Subjects: map[string]BriefHistory{}}
			companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.URL.Path != "/v1/"+assistantBriefRunnerPath(subject) || r.Header.Get("Authorization") != "Bearer fixture-private" {
					t.Error("unscoped Runner request", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				if r.Method == "POST" {
					var command BriefCommand
					if json.NewDecoder(r.Body).Decode(&command) != nil || command.Subject != subject {
						w.WriteHeader(400)
						return
					}
					if command.Action == "begin" && !sameBriefJSON(command.Snapshot, snapshot) {
						w.WriteHeader(409)
						return
					}
					command.Snapshot = snapshot
					if err := applyBrief(&stored, command, time.Now()); err != nil {
						w.WriteHeader(409)
						return
					}
				}
				writeJSON(w, 200, assistantBriefReply{Content: stored, Snapshot: snapshot})
			}))
			defer companion.Close()
			done := make(chan struct{})
			s.jobs = &jobsCompanion{url: companion.URL, token: "fixture-private", done: done, stop: make(chan struct{})}
			t.Cleanup(func() { close(done) })
			start := assistantBriefBegin(t, s, token, subject)
			first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 201)
			complete := assistantProtocolWait(t, s, token, first.ID, "complete")
			w := assistantBriefCall(s, token, "POST", "/api/assistant-runs/"+first.ID+"/brief", map[string]string{"token": start.RequestID})
			if w.Code != 200 {
				t.Fatal("Runner brief commit", w.Code, w.Body.String())
			}
			var reply assistantBriefReply
			assistantRunMust(t, json.Unmarshal(w.Body.Bytes(), &reply))
			if len(reply.Content.Subjects[subject].Revisions) != 1 || reply.Content.Subjects[subject].Revisions[0].Text != assistantBriefFixtureText {
				t.Fatal("wrong Runner revision")
			}
			assistantBriefProbe(t, s, token, "runner-"+kind, start, complete, reply)
		})
	}
}

func TestAssistantBriefActualInvalidOutput(t *testing.T) {
	for _, name := range []string{"malformed", "extra-section", "oversized", "truncated"} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				text, finish := assistantBriefProposal(assistantBriefFixtureText), "stop"
				switch name {
				case "malformed":
					text = "```json\n{\"proposal\":\n```"
				case "extra-section":
					text = strings.Replace(text, `"document":{`, `"document":{"approval":"unauthorized",`, 1)
				case "oversized":
					value := assistantBriefFixtureText
					value.Context = strings.Repeat("x", 3001)
					text = assistantBriefProposal(value)
				case "truncated":
					finish = "length"
				}
				assistantBriefSSE(w, text, finish)
			}))
			defer provider.Close()
			s, token := assistantBriefFixture(t, provider.URL)
			start := assistantBriefBegin(t, s, token, "pack:one")
			first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 201)
			assistantProtocolWait(t, s, token, first.ID, "stopped")
			before := calls.Load()
			if w := assistantBriefCall(s, token, "POST", "/api/assistant-runs/"+first.ID+"/brief", map[string]string{"token": start.RequestID}); w.Code == 200 {
				t.Fatal("invalid provider output committed", name)
			}
			retained := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 200)
			if retained.ID != first.ID || calls.Load() != before {
				t.Fatal("invalid output automatically replayed generation", name)
			}
		})
	}
}

func TestAssistantBriefReservationRefusals(t *testing.T) {
	s, token := assistantBriefFixture(t, "https://example.invalid")
	start := assistantBriefBegin(t, s, token, "pack:one")
	for _, name := range []string{"token", "digest", "admission", "connection", "source", "expired", "session"} {
		t.Run(name, func(t *testing.T) {
			candidate := start
			session := token
			switch name {
			case "token":
				candidate.RequestID = strings.Repeat("f", 32)
			case "digest":
				candidate.Revision = strings.Repeat("f", 64)
			case "admission":
				candidate.AdmissionID = "another-generation"
			case "connection":
				candidate.OwnerID = "pack:missing"
			case "source":
				writeProjectFile(t, s.projectDir, "one.pack.json", `{"version":"changed"}`)
				defer writeProjectFile(t, s.projectDir, "one.pack.json", `{"title":"Frozen <&> 한글","version":"1","evidenceRequirements":[{"id":"evidence","required":true}]}`)
			case "expired":
				lock, err := s.privateDataLockContext(t.Context(), true)
				assistantRunMust(t, err)
				data, err := s.openChatData()
				assistantRunMust(t, err)
				name, err := resolveConversationName(data.root, s.conversationIdentity())
				assistantRunMust(t, err)
				name = strings.Replace(name, "conversations-", "briefs-", 1)
				saved, err := s.readWorkspaceRecord(data.root, name, false)
				assistantRunMust(t, err)
				var d BriefStore
				assistantRunMust(t, json.Unmarshal(saved.Content, &d))
				pending := d.Subjects[start.OwnerID].Pending
				expires := pending.Expires
				pending.Expires = time.Now().Add(-time.Minute).Format(time.RFC3339)
				raw, _ := json.Marshal(d)
				assistantRunMust(t, writePrivateData(data.root, name, raw))
				assistantRunMust(t, data.root.Close())
				assistantRunMust(t, lock.Close())
				defer func() {
					lock, _ := s.privateDataLockContext(context.Background(), true)
					defer lock.Close()
					data, _ := s.openChatData()
					defer data.root.Close()
					pending.Expires = expires
					raw, _ := json.Marshal(d)
					assistantRunMust(t, writePrivateData(data.root, name, raw))
				}()
			case "session":
				session = "invalid-session"
			}
			w := assistantBriefCall(s, session, "POST", "/api/assistant-runs", candidate)
			if w.Code == 201 || w.Code == 200 {
				t.Fatal("invalid brief reservation dispatched", name)
			}
		})
	}
}

func TestAssistantBriefCommitRechecksAndCancellation(t *testing.T) {
	for _, name := range []string{"source", "profile", "connection", "session", "project", "cancel", "legacy-text"} {
		t.Run(name, func(t *testing.T) {
			s, token := assistantBriefFixture(t, "https://example.invalid")
			executor := &assistantBriefTestExecutor{execute: assistantBriefTestComplete}
			s.assistantRunExecutor = executor
			start := assistantBriefBegin(t, s, token, "pack:one")
			first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 201)
			assistantProtocolWait(t, s, token, first.ID, "complete")
			session := token
			switch name {
			case "source":
				writeProjectFile(t, s.projectDir, "one.pack.json", `{"version":"changed"}`)
			case "profile":
				writeProjectFile(t, s.projectDir, assistantProfilePath, `{"profileVersion":1,"api":{"inherit":true},"projectInstructions":"Changed instructions"}`)
			case "connection":
				registry, _ := s.readAIRegistry()
				registry.Connections[0].Enabled = false
				raw, _ := encodeAIRegistry(registry.aiRegistry)
				assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
			case "session":
				session = policyTestSession(t, s)
			case "project":
				s.markProjectMoved(filepath.Join(t.TempDir(), "moved"))
			case "cancel":
				w := policyWorkspaceCall(s, token, "POST", "/api/briefs", "", BriefCommand{Action: "cancel", Subject: start.OwnerID, Token: start.RequestID})
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			case "legacy-text":
				text := assistantBriefFixtureText
				text.Context = "Different browser-authored content"
				if w := policyWorkspaceCall(s, token, "POST", "/api/briefs", "", BriefCommand{Action: "complete", Subject: start.OwnerID, Token: start.RequestID, Text: text}); w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				if w := assistantBriefCall(s, token, "GET", "/api/assistant-runs/"+first.ID+"/brief", nil); w.Code == 200 {
					t.Fatal("matching token substituted for exact saved output")
				}
			}
			w := assistantBriefCall(s, session, "POST", "/api/assistant-runs/"+first.ID+"/brief", map[string]string{"token": start.RequestID})
			if w.Code == 200 {
				t.Fatal("stale authority committed", name)
			}
			if executor.calls.Load() != 1 {
				t.Fatal("refusal restarted generation")
			}
		})
	}
}

func TestAssistantBriefStopAndInterruptedNoReplay(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(fmt.Sprint(interrupted), func(t *testing.T) {
			s, token := assistantBriefFixture(t, "https://example.invalid")
			entered := make(chan struct{})
			executor := &assistantBriefTestExecutor{execute: func(ctx context.Context, e *assistantRunExecution) error {
				if err := e.Event(ctx, "retained-prefix", contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: "Retained interrupted output"}}); err != nil {
					return err
				}
				close(entered)
				if interrupted {
					return errors.New("fixture transport uncertain")
				}
				<-ctx.Done()
				return ctx.Err()
			}}
			s.assistantRunExecutor = executor
			start := assistantBriefBegin(t, s, token, "pack:one")
			first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 201)
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("executor not admitted")
			}
			if !interrupted {
				latest := assistantProtocolView(t, assistantBriefCall(s, token, "GET", "/api/assistant-runs/"+first.ID, nil), 200)
				assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs/"+first.ID+"/actions", assistantRunAction{ID: "stop-brief", Kind: "cancel", Revision: latest.Revision}), 200)
			}
			stopped := assistantProtocolWait(t, s, token, first.ID, "stopped")
			if len(stopped.Events) == 0 || stopped.Events[0].Event.Message == nil {
				t.Fatal("interrupted output erased")
			}
			if w := assistantBriefCall(s, token, "POST", "/api/assistant-runs/"+first.ID+"/brief", map[string]string{"token": start.RequestID}); w.Code == 200 {
				t.Fatal("interrupted output committed")
			}
			retry := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 200)
			if retry.ID != first.ID || executor.calls.Load() != 1 {
				t.Fatal("read/retry replayed interrupted generation")
			}
		})
	}
}

func TestAssistantBriefBrowser(t *testing.T) {
	script := os.Getenv("JPS_BRIEF_BROWSER")
	if script == "" {
		t.Skip("set JPS_BRIEF_BROWSER after building web/dist")
	}
	runtime := os.Getenv("DESK_DIALECT_MAPPING_RUNTIME")
	if !filepath.IsAbs(runtime) {
		t.Fatal("browser requires the explicit pinned Runtime test fixture")
	}
	var calls atomic.Int32
	providerCanceled := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 3 {
			// net/http observes a disconnected client after the request body
			// reaches EOF. Drain this synthetic POST before awaiting Stop.
			if _, err := io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, assistantRunRecordLimit)); err != nil {
				t.Errorf("read synthetic provider request: %v", err)
				return
			}
			<-r.Context().Done()
			close(providerCanceled)
			return
		}
		if n == 1 {
			select {
			case <-time.After(700 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
		assistantBriefSSE(w, assistantBriefProposal(assistantBriefFixtureText), "stop")
	}))
	defer provider.Close()
	s, _ := assistantBriefFixture(t, provider.URL)
	s.cfg.JpackBin = runtime
	writeProjectFile(t, s.projectDir, "jpack.json", `{"configVersion":"5","packs":{"one":{"path":"one.pack.json"}}}`)
	writeProjectFile(t, s.projectDir, "one.pack.json", strings.Replace(reviewPack, "Minimal literal decision", "Frozen browser brief", 1))
	s.cfg.Static = os.DirFS("../../web/dist")
	s.static = http.FileServer(http.FS(s.cfg.Static))
	s.mux.HandleFunc("GET /brief-probe/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]int32{"calls": calls.Load()})
	})
	server := httptest.NewServer(s)
	defer server.Close()
	s.cfg.Port = server.Listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", script, server.URL, testToken)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("brief browser: %v\n%s", err, output)
	} else {
		t.Log(string(output))
	}
	select {
	case <-providerCanceled:
	case <-time.After(5 * time.Second):
		provider.CloseClientConnections()
		t.Fatal("Stop did not cancel the active provider request")
	}
	if calls.Load() != 3 {
		t.Fatal("browser repeated or missed generation", calls.Load())
	}
}
