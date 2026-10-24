package desk

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

type assistantAuthoringTestExecutor struct {
	assistantAuthoringExecutor
	calls   atomic.Int32
	execute func(context.Context, *assistantRunExecution) error
}

func (x *assistantAuthoringTestExecutor) Execute(ctx context.Context, e *assistantRunExecution) error {
	x.calls.Add(1)
	return x.execute(ctx, e)
}
func assistantAuthoringRequestFixture(key, purpose string) assistantAuthoringRequest {
	a := assistantAuthoringRequest{RequestID: key, Purpose: purpose, Prompt: "author_pack", Arguments: map[string]string{"policy": "Decide exactly; unknown policy is a question."}, Model: "shared-model", Thinking: "off", Phase: "work"}
	if purpose == "author-pack" {
		draft := "{\n \"title\":\"UNSAVED-DRAFT\",\"value\":1.20,\"missing\":null,\"flag\":false\n}"
		a.Draft = &draft
		a.Path = "one.pack.json"
	}
	if purpose == "author-graph" {
		a.Prompt = "author_graph"
		a.Arguments = map[string]string{"relationship": "The selected pack supplies the decision."}
		a.Packs = []string{"one"}
		a.GraphID = "flow"
		a.GraphDigest = digestOf([]byte(assistantGraphOriginal))
	}
	return a
}
func assistantAuthoringFixture(t *testing.T, upstream string) (*Server, string) {
	t.Helper()
	s, token := assistantBriefFixture(t, upstream)
	writeProjectFile(t, s.projectDir, "jpack.json", `{"configVersion":"5","packs":{"one":{"path":"one.pack.json"}},"graphs":{"flow":{"path":"flow.graph.json"}}}`)
	writeProjectFile(t, s.projectDir, "one.pack.json", reviewPack)
	writeProjectFile(t, s.projectDir, "flow.graph.json", assistantGraphOriginal)
	writeProjectFile(t, s.projectDir, assistantProfilePath, `{"profileVersion":1,"api":{"inherit":true},"critic":"off"}`)
	store := s.assistantRuns()
	data, err := s.openChatData()
	assistantRunMust(t, err)
	ledgerPath := filepath.Join(data.root.Name(), store.name)
	assistantRunMust(t, data.root.Close())
	peer := strings.Replace(assistantExecutionPeer, "{'name':'served','arguments':[]}", "{'name':'author_pack','arguments':[{'name':'policy','required':True}]},{'name':'fix_pack','arguments':[{'name':'diagnostics','required':True}]},{'name':'author_graph','arguments':[{'name':'relationship','required':True},{'name':'packs','required':True}]}", 1)
	peer = strings.Replace(peer, "'text':'SERVED-RUNTIME-PROMPT'", "'text':'CANONICAL AUTHOR GUIDANCE '+json.dumps(p.get('arguments',{}))", 1)
	s.cfg.JpackBin = filepath.Join(t.TempDir(), "authoring-runtime")
	assistantRunMust(t, os.WriteFile(s.cfg.JpackBin, []byte(peer), 0700))
	config, _ := json.Marshal(map[string]string{"ledger": ledgerPath})
	writeProjectFile(t, s.projectDir, "execution-fixture.json", string(config))
	return s, token
}
func assistantAuthoringCaptureFixture(t *testing.T, s *Server, token string, a assistantAuthoringRequest) assistantAuthoringReply {
	t.Helper()
	w := assistantBriefCall(s, token, "POST", "/api/assistant-authoring/captures", a)
	if w.Code != 200 {
		t.Fatal("capture", w.Code, w.Body.String())
	}
	var reply assistantAuthoringReply
	assistantRunMust(t, json.Unmarshal(w.Body.Bytes(), &reply))
	if reply.Start == nil {
		t.Fatal("missing host reference")
	}
	return reply
}
func assistantAuthoringLedger(t *testing.T, s *Server) assistantRunLedger {
	t.Helper()
	store := s.assistantRuns()
	ledger, err := store.transaction(t.Context(), func(*assistantRunLedger) (bool, error) { return false, nil })
	assistantRunMust(t, err)
	return ledger
}
func assistantAuthoringComplete(ctx context.Context, e *assistantRunExecution) error {
	document := map[string]json.RawMessage{"title": json.RawMessage(`"Proposed"`)}
	if err := e.Event(ctx, "proposal", contract.RunEvent{Proposal: &contract.RunEventProposal{Type: "proposal", Document: document, Unknowns: []string{}}}); err != nil {
		return err
	}
	cp := *e.record.Checkpoint
	cp.Status = "complete"
	return e.Checkpoint(ctx, cp)
}
func TestAssistantAuthoringCaptureOwnerAndCancellation(t *testing.T) {
	s, token := assistantAuthoringFixture(t, "https://example.invalid")
	a := assistantAuthoringRequestFixture("capture-one", "author-pack")
	reply := assistantAuthoringCaptureFixture(t, s, token, a)
	ledger := assistantAuthoringLedger(t, s)
	c := ledger.AuthoringCaptures[0]
	raw, _ := json.Marshal(a)
	if !bytes.Equal(c.Request, raw) || c.ID == a.RequestID || c.AdmissionID == a.RequestID || len(ledger.Runs) != 0 {
		t.Fatal("capture became client authority or model admission")
	}
	var snapshot assistantAuthoringSnapshot
	assistantRunMust(t, json.Unmarshal(c.Snapshot, &snapshot))
	if snapshot.Pack == nil || string(snapshot.Pack.Pack) != reviewPack || string(snapshot.Pack.Pack) == *a.Draft {
		t.Fatal("untrusted draft replaced saved source")
	}
	before := assistantRunRaw(t, s.assistantRuns())
	for _, call := range []struct {
		method, path string
		body         any
	}{{"GET", "/api/assistant-authoring/captures/capture-one", nil}, {"POST", "/api/assistant-authoring/captures", a}} {
		w := assistantBriefCall(s, token, call.method, call.path, call.body)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var again assistantAuthoringReply
		assistantRunMust(t, json.Unmarshal(w.Body.Bytes(), &again))
		if *again.Start != *reply.Start {
			t.Fatal("reference changed on reconciliation")
		}
	}
	if !bytes.Equal(before, assistantRunRaw(t, s.assistantRuns())) {
		t.Fatal("read or duplicate capture wrote ledger")
	}
	changed := a
	changed.Arguments = map[string]string{"policy": "changed"}
	if w := assistantBriefCall(s, token, "POST", "/api/assistant-authoring/captures", changed); w.Code != 409 {
		t.Fatal("key reused with changed data", w.Code)
	}
	bad := map[string]string{"ownerId": reply.Start.OwnerID, "revision": strings.Repeat("0", 64)}
	if w := assistantBriefCall(s, token, "POST", "/api/assistant-authoring/captures/capture-one/cancel", bad); w.Code < 400 {
		t.Fatal("cancel accepted wrong host reference", w.Code)
	}
	if !bytes.Equal(before, assistantRunRaw(t, s.assistantRuns())) {
		t.Fatal("refusal changed ledger")
	}
	bad["revision"] = reply.Start.Revision
	if w := assistantBriefCall(s, token, "POST", "/api/assistant-authoring/captures/capture-one/cancel", bad); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := assistantBriefCall(s, token, "POST", "/api/assistant-runs", reply.Start); w.Code < 400 {
		t.Fatal("late admission survived Stop", w.Code)
	}
	if len(assistantAuthoringLedger(t, s).Runs) != 0 {
		t.Fatal("canceled request allocated run")
	}
}
func TestAssistantAuthoringAdmissionAndInterruptedNoReplay(t *testing.T) {
	s, token := assistantAuthoringFixture(t, "https://example.invalid")
	entered := make(chan *assistantRunExecution, 1)
	executor := &assistantAuthoringTestExecutor{execute: func(ctx context.Context, e *assistantRunExecution) error {
		entered <- e
		<-ctx.Done()
		return ctx.Err()
	}}
	s.assistantRunExecutor = executor
	ref := assistantAuthoringCaptureFixture(t, s, token, assistantAuthoringRequestFixture("same-request", "describe-pack"))
	first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", ref.Start), 201)
	e := <-entered
	if repeat := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", ref.Start), 200); repeat.ID != first.ID {
		t.Fatal("lost ack duplicated run")
	}
	alternate := *ref.Start
	alternate.AdmissionID = "client-chosen-other-admission"
	if w := assistantBriefCall(s, token, "POST", "/api/assistant-runs", alternate); w.Code < 400 {
		t.Fatal("same capture admitted twice")
	}
	request := assistantBriefCall(s, token, "GET", "/api/assistant-authoring/captures/same-request", nil)
	var found assistantAuthoringReply
	assistantRunMust(t, json.Unmarshal(request.Body.Bytes(), &found))
	if found.Run == nil || found.Run.ID != first.ID {
		t.Fatal("lost ack could not reconcile")
	}
	// Process/service replacement interrupts retained execution. No GET or restart
	// reconstructs provider work, even when its admission acknowledgement was lost.
	s.assistantRunService().close()
	<-e.done
	s.assistantRunServiceMu.Lock()
	s.assistantRunHost = nil
	s.assistantRunServiceMu.Unlock()
	again := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", ref.Start), 200)
	if again.ID != first.ID || again.State == "complete" || executor.calls.Load() != 1 {
		t.Fatal("restart replayed", again.State, executor.calls.Load())
	}
}
func TestAssistantAuthoringCurrentSourceAndSelection(t *testing.T) {
	for _, mutation := range []string{"saved-bytes", "declaration", "selected-pack", "graph", "profile", "model", "connection", "session"} {
		t.Run(mutation, func(t *testing.T) {
			s, token := assistantAuthoringFixture(t, "https://example.invalid")
			a := assistantAuthoringRequestFixture("stale-one", "author-pack")
			if mutation == "selected-pack" || mutation == "graph" {
				a = assistantAuthoringRequestFixture("stale-one", "author-graph")
			}
			ref := assistantAuthoringCaptureFixture(t, s, token, a)
			switch mutation {
			case "saved-bytes", "selected-pack":
				writeProjectFile(t, s.projectDir, "one.pack.json", reviewPack+"\n")
			case "declaration":
				writeProjectFile(t, s.projectDir, "jpack.json", `{"packs":{"one":{"path":"one.pack.json","description":"changed"}}}`)
			case "graph":
				writeProjectFile(t, s.projectDir, "flow.graph.json", assistantGraphOriginal+"\n")
			case "profile":
				writeProjectFile(t, s.projectDir, assistantProfilePath, `{"profileVersion":1,"api":{"inherit":true},"critic":"on"}`)
			case "model", "connection":
				registry, err := s.readAIRegistry()
				assistantRunMust(t, err)
				if mutation == "model" {
					registry.Connections[0].Assistant.Endpoint.Models = []string{"other"}
					registry.Connections[0].Assistant.Endpoint.Model = nil
				} else {
					registry.Connections[0].Assistant.Endpoint.URL = "https://changed.invalid"
				}
				raw, err := encodeAIRegistry(registry.aiRegistry)
				assistantRunMust(t, err)
				assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
			case "session":
				s.sessions.revoke(token)
			}
			if w := assistantBriefCall(s, token, "POST", "/api/assistant-runs", ref.Start); w.Code < 400 {
				t.Fatal("stale capture started", w.Code)
			}
			if len(assistantAuthoringLedger(t, s).Runs) != 0 {
				t.Fatal("stale source allocated model run")
			}
		})
	}
}
func TestAssistantAuthoringMalformedAndCapacity(t *testing.T) {
	s, token := assistantAuthoringFixture(t, "https://example.invalid")
	a := assistantAuthoringRequestFixture("valid-one", "describe-pack")
	assistantAuthoringCaptureFixture(t, s, token, a)
	before := assistantRunRaw(t, s.assistantRuns())
	raw, _ := json.Marshal(a)
	invalid := []string{strings.Replace(string(raw), `"purpose":`, `"Purpose":`, 1), strings.Replace(string(raw), `"phase":"work"`, `"phase":"work","phase":"plan"`, 1), strings.Replace(string(raw), `"arguments":`, `"tools":[],"arguments":`, 1), strings.Replace(string(raw), `"author_pack"`, `"arbitrary_prompt"`, 1), strings.Replace(string(raw), `"policy":`, `"Policy":`, 1)}
	for _, body := range invalid {
		r := httptest.NewRequest("POST", "/api/assistant-authoring/captures", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code < 400 {
			t.Fatal("malformed authoring accepted", body)
		}
	}
	if !bytes.Equal(before, assistantRunRaw(t, s.assistantRuns())) {
		t.Fatal("malformed request wrote ledger")
	}
	var ledger assistantRunLedger
	assistantRunMust(t, json.Unmarshal(before, &ledger))
	for _, mutation := range []string{"alias", "duplicate", "null", "binding"} {
		broken := bytes.Clone(before)
		switch mutation {
		case "alias":
			broken = bytes.Replace(broken, []byte(`"scope":`), []byte(`"Scope":`), 1)
		case "duplicate":
			broken = bytes.Replace(broken, []byte(`"cancelled":`), []byte(`"cancelled":true,"cancelled":`), 1)
			if bytes.Equal(broken, before) {
				broken = bytes.Replace(broken, []byte(`"revision":`), []byte(`"revision":"x","revision":`), 1)
			}
		case "null":
			broken = bytes.Replace(broken, []byte(`"scope":{`), []byte(`"scope":null,"other":{`), 1)
		case "binding":
			ledger.AuthoringCaptures[0].Revision = strings.Repeat("a", 64)
			broken, _ = json.Marshal(ledger)
		}
		if _, err := decodeAssistantRunStorage(s.assistantRuns().name, broken); err == nil {
			t.Fatal("strict ledger accepted", mutation)
		}
	}
	// Reuse the same finite ledger budget. Completed/canceled captures are never
	// silently evicted; a full ledger refuses the next capture before a write.
	_, err := s.assistantRuns().transaction(t.Context(), func(ledger *assistantRunLedger) (bool, error) {
		base := ledger.AuthoringCaptures[0]
		for len(ledger.AuthoringCaptures) < assistantRunCountLimit {
			c := base
			c.RequestID = fmt.Sprintf("retained-%d", len(ledger.AuthoringCaptures))
			request, _ := readAssistantAuthoringRequest(c.Request)
			request.RequestID = c.RequestID
			c.Request, _ = json.Marshal(request)
			c.ID = fmt.Sprintf("author_%032x", len(ledger.AuthoringCaptures))
			c.AdmissionID = fmt.Sprintf("author-admit-%032x", len(ledger.AuthoringCaptures))
			c.Revision = assistantAuthoringRevision(c)
			ledger.AuthoringCaptures = append(ledger.AuthoringCaptures, c)
		}
		return true, nil
	})
	assistantRunMust(t, err)
	before = assistantRunRaw(t, s.assistantRuns())
	a.RequestID = "over-capacity"
	if w := assistantBriefCall(s, token, "POST", "/api/assistant-authoring/captures", a); w.Code < 400 {
		t.Fatal("capture capacity grew")
	}
	if !bytes.Equal(before, assistantRunRaw(t, s.assistantRuns())) {
		t.Fatal("capacity refusal wrote ledger")
	}
}
func TestAssistantAuthoringActualAPI(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), "CANONICAL AUTHOR GUIDANCE") {
			t.Error("Runtime guidance absent")
		}
		document := json.RawMessage(reviewPack)
		if strings.Contains(string(raw), "graph authoring assistant") {
			graph, _ := json.Marshal(map[string]any{"graph": json.RawMessage(assistantGraphCandidate), "id": "flow", "description": "Proposed graph"})
			document = graph
		}
		assistantBriefSSE(w, assistantAuthoringProposalFixture(document), "stop")
	}))
	defer provider.Close()
	for _, purpose := range []string{"author-pack", "describe-pack", "author-graph"} {
		t.Run(purpose, func(t *testing.T) {
			s, token := assistantAuthoringFixture(t, provider.URL)
			a := assistantAuthoringRequestFixture("actual-"+purpose, purpose)
			ref := assistantAuthoringCaptureFixture(t, s, token, a)
			before := calls.Load()
			first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", ref.Start), 201)
			complete := assistantProtocolWait(t, s, token, first.ID, "complete")
			if calls.Load() != before+1 || complete.Checkpoint == nil || complete.Checkpoint.Used != 1 || len(complete.Checkpoint.Prompts) == 0 {
				t.Fatal("canonical request accounting missing", complete.State, calls.Load()-before)
			}
			if repeat := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", ref.Start), 200); repeat.ID != first.ID || calls.Load() != before+1 {
				t.Fatal("terminal admission replayed")
			}
			assistantAuthoringProbe(t, s, token, "api-"+purpose, a, ref, complete)
			if string(s.readProjectFile("one.pack.json").data) != reviewPack || string(s.readProjectFile("flow.graph.json").data) != assistantGraphOriginal {
				t.Fatal("generation saved an artifact")
			}
		})
	}
}

func TestAssistantAuthoringPrepareIsModelFree(t *testing.T) {
	s, token := assistantAuthoringFixture(t, "https://example.invalid")
	a := assistantAuthoringRequestFixture("prepare", "describe-pack")
	ref := assistantAuthoringCaptureFixture(t, s, token, a)
	r := httptest.NewRequest("POST", "/api/assistant-runs", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	identity, _, err := s.assistantRunIdentity(r)
	assistantRunMust(t, err)
	source, err := s.readAssistantRunSource(r, *ref.Start, identity, s.assistantRuns().project)
	assistantRunMust(t, err)
	cp, err := s.assistantRunExecutor.Prepare(t.Context(), source)
	if err != nil {
		t.Fatalf("canonical prepare: %T %v", err, err)
	}
	t.Logf("checkpoint %s %s %s", cp.Status, cp.Phase, cp.Unit)
	if len(assistantAuthoringLedger(t, s).Runs) != 0 {
		t.Fatal("Prepare admitted provider work")
	}
}

func TestAssistantAuthoringSavedCasesAndClosedTools(t *testing.T) {
	s, token := assistantAuthoringFixture(t, "https://example.invalid")
	// Save through the authoritative workspace API. Intentional types and current
	// expected errors are never reconstructed from the unsaved editor buffer.
	tests := json.RawMessage(`{"version":1,"suites":{"one":` + assistantTestSuiteFixture + `}}`)
	if w := policyWorkspaceCall(s, token, "PUT", "/api/pack-tests", "absent", tests); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	entered := make(chan *assistantRunExecution, 1)
	s.assistantRunExecutor = &assistantAuthoringTestExecutor{execute: func(ctx context.Context, e *assistantRunExecution) error {
		entered <- e
		<-ctx.Done()
		return ctx.Err()
	}}
	ref := assistantAuthoringCaptureFixture(t, s, token, assistantAuthoringRequestFixture("case-input", "author-pack"))
	assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", ref.Start), 201)
	e := <-entered
	input, err := e.prepareAssistantInput(t.Context())
	assistantRunMust(t, err)
	if !strings.Contains(input.Request.Input, `UNSAVED-DRAFT`) || !strings.Contains(input.Request.Input, `savedPack`) {
		t.Fatal("saved basis/draft distinction missing")
	}
	held, err := e.readAssistantAuthoringCriticSnapshot(t.Context(), false)
	assistantRunMust(t, err)
	if len(held.cases) != 1 {
		t.Fatal("saved critic cases lost", len(held.cases))
	}
	for _, literal := range []string{`"amount":"4"`, `"n":1.2`, `"flag":false`, `"zero":0`, `"null":null`} {
		if !bytes.Contains(held.cases[0], []byte(literal)) {
			t.Fatal("critic typed fact changed", literal, string(held.cases[0]))
		}
	}
	tools, err := e.assistantAuthoringTools()
	assistantRunMust(t, err)
	if len(tools.tools) != 1 || tools.tools[0].Name != "propose_pack" || tools.tools[0].Delegation != "" || tools.tools[0].Effect != "proposal" {
		t.Fatal("authoring host tool offer expanded")
	}
	prompt, err := assistantAuthoringPrompt(e.source)
	assistantRunMust(t, err)
	prompt.Arguments["policy"] = "changed"
	if x, err := e.openAssistantExecution(t.Context(), prompt); err == nil {
		x.Close()
		t.Fatal("arbitrary prompt argument replaced captured request")
	}
	s.assistantRunService().close()
	<-e.done
}

func TestAssistantAuthoringActualNative(t *testing.T) {
	s, token := assistantAuthoringFixture(t, "https://example.invalid")
	registry, err := s.readAIRegistry()
	assistantRunMust(t, err)
	model := "shared-model"
	registry.Connections[0].Assistant.Engine = "codex"
	registry.Connections[0].Assistant.Endpoint = nil
	registry.Connections[0].Assistant.Agent = &AssistantAgentConfig{Provider: "openai", AuthMethod: "subscription", Model: &model, Models: []string{model}, Effort: "medium", Tools: []string{"get_schema"}}
	raw, err := encodeAIRegistry(registry.aiRegistry)
	assistantRunMust(t, err)
	assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
	writeProjectFile(t, s.projectDir, assistantProfilePath, `{"profileVersion":1,"codex":{"inherit":true},"critic":"off"}`)
	a := assistantAuthoringRequestFixture("native-author", "author-pack")
	a.Effort = "medium"
	ref := assistantAuthoringCaptureFixture(t, s, token, a)
	fixture := t.TempDir()
	binary, trace := filepath.Join(fixture, "codex-peer"), filepath.Join(fixture, "trace.jsonl")
	peer := strings.Replace(assistantExecutionCodexPeer, "text='Fixture answer.'", "text="+fmt.Sprintf("%q", assistantAuthoringProposalFixture(json.RawMessage(reviewPack))), 1)
	peer = strings.Replace(peer, "else: tool(offered('get_schema'),{})", "elif mode=='brief': finish()\n  else: tool(offered('get_schema'),{})", 1)
	assistantRunMust(t, os.WriteFile(filepath.Join(fixture, "peer.py"), []byte(peer), 0600))
	data, err := s.openChatData()
	assistantRunMust(t, err)
	ledgerPath := filepath.Join(data.root.Name(), s.assistantRuns().name)
	assistantRunMust(t, data.root.Close())
	wrapper := fmt.Sprintf("#!/bin/sh\nexec python3 '%s' brief '%s' '%s'\n", filepath.Join(fixture, "peer.py"), trace, ledgerPath)
	assistantRunMust(t, os.WriteFile(binary, []byte(wrapper), 0700))
	manager, err := codexbridge.NewManager(codexbridge.Options{Binary: binary, ProfileDir: filepath.Join(fixture, "account"), ProjectDir: s.cfg.ProjectDir})
	assistantRunMust(t, err)
	t.Cleanup(manager.Close)
	s.codex = manager
	s.aiAccounts.values = map[string]providerAccountManager{testAIAlpha: manager}
	first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", ref.Start), 201)
	complete := assistantProtocolWait(t, s, token, first.ID, "complete")
	if complete.Checkpoint == nil || complete.Checkpoint.Unit != "provider-managed" {
		t.Fatal("native canonical accounting changed")
	}
	generations := 0
	for _, row := range assistantCodexLog(t, trace) {
		if string(row["type"]) == `"generation"` {
			generations++
			if !strings.Contains(string(row["prompt"]), "UNSAVED-DRAFT") || !strings.Contains(string(row["prompt"]), "CANONICAL AUTHOR GUIDANCE") {
				t.Fatal("native authored source/guidance missing")
			}
		}
	}
	if generations != 1 {
		t.Fatal("native request repeated", generations)
	}
	assistantAuthoringProbe(t, s, token, "native-author-pack", a, ref, complete)
}

func TestAssistantAuthoringStorageMoveBackupAndHistoricalRestore(t *testing.T) {
	s, token := assistantAuthoringFixture(t, "https://example.invalid")
	ref := assistantAuthoringCaptureFixture(t, s, token, assistantAuthoringRequestFixture("storage-author", "author-pack"))
	store := s.assistantRuns()
	original := assistantRunRaw(t, store)
	server := httptest.NewServer(s)
	defer server.Close()
	backup := backupBytes(t, server)
	archive, err := zip.NewReader(bytes.NewReader(backup), int64(len(backup)))
	assistantRunMust(t, err)
	found := false
	for _, entry := range archive.File {
		if entry.Name == store.name {
			reader, err := entry.Open()
			assistantRunMust(t, err)
			body, err := io.ReadAll(reader)
			assistantRunMust(t, err)
			assistantRunMust(t, reader.Close())
			if !bytes.Equal(body, original) {
				t.Fatal("backup lost capture exact request/source bytes")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("capture-only ledger omitted from backup")
	}
	before := storageGet(t, server)
	target := filepath.Join(t.TempDir(), "moved-data")
	if code, body := assistantRunStorageMove(t, server, moveChatData{Path: target, Revision: before.Revision}); code != 200 {
		t.Fatal("capture data move", code, body)
	}
	if !bytes.Equal(original, assistantRunRaw(t, s.assistantRuns())) {
		t.Fatal("data move rewrote capture")
	}
	// Exercise the real restore route as well as its strict reader. Imported
	// captures retain their exact bytes and reference, but lose admission authority.
	beforeRestore := storageGet(t, server)
	if code, body := restoreBackup(t, server, moveChatData{Path: filepath.Join(t.TempDir(), "restored-data"), Revision: beforeRestore.Revision}, backup); code != 200 {
		t.Fatal("capture restore", code, body)
	}
	restored := assistantRunRaw(t, s.assistantRuns())
	ledger, err := decodeAssistantRunStorage(store.name, restored)
	assistantRunMust(t, err)
	var initial assistantRunLedger
	assistantRunMust(t, json.Unmarshal(original, &initial))
	c := ledger.AuthoringCaptures[0]
	if !c.Cancelled || !bytes.Equal(c.Request, initial.AuthoringCaptures[0].Request) || !bytes.Equal(c.Snapshot, initial.AuthoringCaptures[0].Snapshot) || *assistantAuthoringStart(c) != *ref.Start {
		t.Fatal("restore changed retained bytes or restored admission authority")
	}

	if w := assistantBriefCall(s, token, "POST", "/api/assistant-runs", ref.Start); w.Code < 400 {
		t.Fatal("restored capture became executable authority")
	}
}

func TestAssistantAuthoringStopActiveProviderAndLateAdmission(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer provider.Close()
	s, token := assistantAuthoringFixture(t, provider.URL)
	ref := assistantAuthoringCaptureFixture(t, s, token, assistantAuthoringRequestFixture("stop-active", "describe-pack"))
	first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", ref.Start), 201)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("actual provider was not reached")
	}
	cancel := map[string]string{"ownerId": ref.Start.OwnerID, "revision": ref.Start.Revision}
	response := assistantBriefCall(s, token, "POST", "/api/assistant-authoring/captures/stop-active/cancel", cancel)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		provider.CloseClientConnections()
		t.Fatal("explicit capture Stop did not cancel actual provider")
	}
	terminal := assistantProtocolWait(t, s, token, first.ID, "stopped")
	if terminal.Checkpoint.Status != "stopped" || len(terminal.Operations) == 0 {
		t.Fatal("cancel lost canonical operation evidence")
	}
	repeat := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", ref.Start), 200)
	if repeat.ID != first.ID || repeat.State != "stopped" || calls.Load() != 1 {
		t.Fatal("late start replayed canceled request")
	}
	again := assistantBriefCall(s, token, "POST", "/api/assistant-authoring/captures/stop-active/cancel", cancel)
	if again.Code != 200 {
		t.Fatal("idempotent exact cancellation failed", again.Code)
	}
}

func TestAssistantAuthoringTerminalCaptureCannotReadmit(t *testing.T) {
	s, token := assistantAuthoringFixture(t, "https://example.invalid")
	executor := &assistantAuthoringTestExecutor{execute: assistantAuthoringComplete}
	s.assistantRunExecutor = executor
	ref := assistantAuthoringCaptureFixture(t, s, token, assistantAuthoringRequestFixture("terminal-owner", "describe-pack"))
	first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", ref.Start), 201)
	assistantProtocolWait(t, s, token, first.ID, "complete")
	alternate := *ref.Start
	alternate.AdmissionID = "author-admit-" + strings.Repeat("0", 32)
	if w := assistantBriefCall(s, token, "POST", "/api/assistant-runs", alternate); w.Code < 400 {
		t.Fatal("same terminal capture admitted another model run")
	}
	if executor.calls.Load() != 1 || len(assistantAuthoringLedger(t, s).Runs) != 1 {
		t.Fatal("terminal owner executed twice")
	}
}

// Temporary observations connect the actual guarded producer with the canonical
// TypeScript readers. They are never recorded goldens or application authority.
func assistantAuthoringProbe(t *testing.T, s *Server, token, label string, request assistantAuthoringRequest, captured assistantAuthoringReply, run assistantRunView) {
	t.Helper()
	dir := os.Getenv("JPS_AUTHORING_OUTPUTS")
	if dir == "" {
		return
	}
	reference := assistantBriefCall(s, token, "GET", "/api/assistant-authoring/captures/"+request.RequestID, nil)
	if reference.Code != 200 {
		t.Fatal("authoring reference probe", reference.Code, reference.Body.String())
	}
	before := assistantRunRaw(t, s.assistantRuns())
	stream := assistantBriefCall(s, token, "GET", "/api/assistant-runs/"+run.ID+"/events", nil)
	if stream.Code != 200 || !strings.HasPrefix(stream.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatal("authoring stream probe", stream.Code, stream.Body.String())
	}
	if !bytes.Equal(before, assistantRunRaw(t, s.assistantRuns())) {
		t.Fatal("reader probe wrote ledger")
	}
	ledger := assistantAuthoringLedger(t, s)
	capture := ledger.AuthoringCaptures[0]
	cancel := assistantBriefCall(s, token, "POST", "/api/assistant-authoring/captures/"+request.RequestID+"/cancel", map[string]string{"ownerId": captured.Start.OwnerID, "revision": captured.Start.Revision})
	if cancel.Code != 200 {
		t.Fatal("authoring cancellation probe", cancel.Code, cancel.Body.String())
	}
	raw, err := json.Marshal(map[string]any{"request": request, "captured": captured, "reference": json.RawMessage(reference.Body.Bytes()), "cancelled": json.RawMessage(cancel.Body.Bytes()), "capture": capture, "run": run})
	assistantRunMust(t, err)
	assistantRunMust(t, os.WriteFile(filepath.Join(dir, label+".json"), raw, 0600))
	assistantRunMust(t, os.WriteFile(filepath.Join(dir, label+".sse"), stream.Body.Bytes(), 0600))
}

func assistantAuthoringProposalFixture(document json.RawMessage) string {
	raw, _ := json.Marshal(map[string]any{"proposal": map[string]any{"kind": "create", "document": document, "unknowns": []string{}}})
	return "```json\n" + string(raw) + "\n```"
}

// Testing guidance is a host admission barrier. Browser prompt previews neither
// authorize a request nor replace a refused or still-pending Runtime response.
func TestAssistantAuthoringCriticGuidanceAdmission(t *testing.T) {
	for _, mode := range []string{"refused", "pending"} {
		t.Run(mode, func(t *testing.T) {
			s, token := assistantAuthoringFixture(t, "https://example.invalid")
			writeProjectFile(t, s.projectDir, assistantProfilePath, `{"profileVersion":1,"api":{"inherit":true},"critic":"on"}`)
			assistantCriticAPISetup(t, s)
			entered := make(chan *assistantRunExecution, 1)
			s.assistantRunExecutor = &assistantAuthoringTestExecutor{execute: func(ctx context.Context, e *assistantRunExecution) error {
				entered <- e
				<-ctx.Done()
				return ctx.Err()
			}}
			peer, err := os.ReadFile(s.cfg.JpackBin)
			assistantRunMust(t, err)
			text := strings.Replace(string(peer), "{'name':'author_pack','arguments'", "{'name':'test_pack','arguments':[]},{'name':'author_pack','arguments'", 1)
			hook := ` if method=='prompts/get' and name=='test_pack':
  with open('critic-guidance.started','w') as f: f.write('started')
`
			if mode == "refused" {
				hook += `  print(json.dumps({'jsonrpc':'2.0','id':q['id'],'error':{'code':-32603,'message':'testing guidance refused'}}),flush=True);continue
`
			} else {
				hook += `  while not os.path.exists('critic-guidance.release'): time.sleep(.005)
`
			}
			text = strings.Replace(text, " if method=='initialize':", hook+" if method=='initialize':", 1)
			assistantRunMust(t, os.WriteFile(s.cfg.JpackBin, []byte(text), 0700))
			ref := assistantAuthoringCaptureFixture(t, s, token, assistantAuthoringRequestFixture("critic-guidance-"+mode, "author-pack"))
			assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", ref.Start), 201)
			e := <-entered
			t.Cleanup(func() { s.assistantRunService().close(); <-e.done })
			ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
			defer cancel()
			type opened struct {
				x   *assistantExecution
				err error
			}
			result := make(chan opened, 1)
			prompt, err := assistantAuthoringPrompt(e.source)
			assistantRunMust(t, err)
			go func() { x, err := e.openAssistantExecution(ctx, prompt); result <- opened{x, err} }()
			if mode == "refused" {
				select {
				case answer := <-result:
					if answer.x != nil {
						answer.x.Close()
					}
					if answer.err == nil {
						t.Fatal("refused testing guidance admitted execution")
					}
				case <-time.After(3 * time.Second):
					cancel()
					answer := <-result
					if answer.x != nil {
						answer.x.Close()
					}
					t.Fatal("refused testing guidance remained pending")
				}
			} else {
				deadline := time.After(4 * time.Second)
			waiting:
				for {
					if _, err := os.Stat(filepath.Join(s.projectDir, "critic-guidance.started")); err == nil {
						break waiting
					}
					select {
					case answer := <-result:
						if answer.x != nil {
							answer.x.Close()
						}
						t.Fatal("execution opened before testing guidance arrived", answer.err)
					case <-deadline:
						cancel()
						answer := <-result
						if answer.x != nil {
							answer.x.Close()
						}
						t.Fatal("testing guidance request never arrived")
					case <-time.After(5 * time.Millisecond):
					}
				}
				select {
				case answer := <-result:
					if answer.x != nil {
						answer.x.Close()
					}
					t.Fatal("execution opened while testing guidance was pending", answer.err)
				case <-time.After(50 * time.Millisecond):
				}
				if len(assistantExecutionRecord(t, e).Operations) == 0 {
					t.Fatal("testing guidance lacked retained Runtime operation")
				}
				writeProjectFile(t, s.projectDir, "critic-guidance.release", "release")
				answer := <-result
				assistantRunMust(t, answer.err)
				if answer.x.Run.TestPrompt == "" {
					answer.x.Close()
					t.Fatal("served testing guidance omitted")
				}
				answer.x.Close()
			}
			for _, op := range assistantExecutionRecord(t, e).Operations {
				if op.Kind == "model" {
					t.Fatal("guidance check dispatched a model")
				}
			}
		})
	}
}
