package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const assistantCreationCasesDocument = `{"cases":[{"id":"typed","facts":{"zero":0,"flag":false,"nil":null,"numeric":"1"},"expectedDisposition":` + creationDisposition + `,"expectedHandoffTarget":null,"expectationSource":"you-1","rationale":"The person explicitly requires accept for these facts."},{"id":"invalid","facts":{},"expectedDisposition":{"kind":"outcome","outcomeId":"accept","reasons":"invalid","handoff":{"state":"none"}},"expectedHandoffTarget":null,"expectationSource":"you-1","rationale":"The person explicitly requires accept with no handoff."}]}`
const assistantCreationCorrectionDocument = `{"expectedDisposition":` + creationDisposition + `,"rationale":"The independent person statement establishes accept with no handoff."}`

type assistantCreationObservedExecutor struct {
	assistantCreationExecutor
	t *testing.T
}

func (x *assistantCreationObservedExecutor) Execute(ctx context.Context, e *assistantRunExecution) error {
	err := x.assistantCreationExecutor.Execute(ctx, e)
	if err != nil {
		x.t.Logf("creation execution: %v", err)
		for _, run := range assistantAuthoringLedger(x.t, e.service.server).Runs {
			if run.ID == e.record.ID {
				for _, op := range run.Operations {
					x.t.Logf("operation %s %s %s", op.ID, op.Name, op.State)
				}
				if run.Creation != nil {
					x.t.Logf("creation action %s cases=%d issues=%d held=%d check=%t", run.Creation.Action, len(run.Creation.Cases), len(run.Creation.Issues), len(run.Creation.Held), run.Creation.Check != nil)
					if run.Creation.Proposal != nil {
						x.t.Logf("pending expected disposition %s", run.Creation.Proposal.ExpectedDisposition)
					}
					for _, op := range run.Operations {
						if op.Kind == "runtime" && op.State == "completed" {
							var result struct{ Result json.RawMessage }
							if json.Unmarshal(op.Result, &result) == nil {
								if payload, err := assistantSavedPackPayload(result.Result); err == nil && payload["results"] != nil {
									x.t.Logf("retained expectation report %s", assistantChatEncodeObject(payload))
								}
							}
						}
					}
				}
			}
		}
	}
	return err
}

func assistantCreationActualFixture(t *testing.T, endpoint string) (*Server, string) {
	t.Helper()
	runtime := os.Getenv("DESK_DIALECT_MAPPING_RUNTIME")
	if runtime == "" {
		t.Skip("explicit companion Runtime fixture required")
	}
	s, token := assistantBriefFixture(t, endpoint)
	s.cfg.JpackBin = runtime
	s.assistantRunExecutor = &assistantCreationObservedExecutor{t: t}
	writeProjectFile(t, s.projectDir, "jpack.json", `{"configVersion":"5","packs":{"one":{"path":"one.pack.json"}}}`)
	writeProjectFile(t, s.projectDir, "one.pack.json", reviewPack)
	writeProjectFile(t, s.projectDir, assistantProfilePath, `{"profileVersion":1,"api":{"inherit":true},"critic":"off"}`)
	state := map[string]any{"phase": "review", "status": "needs-input", "detail": "", "brief": "Always accept.", "seedUrls": []any{}, "turns": []any{
		map[string]any{"id": "person-one", "role": "user", "kind": "brief", "text": "Always accept, including missing facts, zero, false and null. A handoff is never requested.", "at": "2026-10-23T00:00:00Z"},
		map[string]any{"id": "assistant-one", "role": "assistant", "kind": "message", "text": assistantAuthoringProposalFixture(json.RawMessage(reviewPack)), "at": "2026-10-23T00:00:01Z"},
	}, "questions": []any{}, "events": []any{}, "candidates": []any{}, "cases": []any{}, "droppedCases": []any{}, "heldProposal": nil, "expectationIssues": []any{}, "unknowns": []any{}, "citations": []any{}, "verdicts": map[string]any{}, "registries": map[string]any{}, "revisionsUsed": 0, "sessions": []any{}, "responses": []any{}, "readiness": ""}
	chat := map[string]any{"id": "chat-one", "title": "Creation", "pinned": false, "archived": false, "composer": "", "updatedAt": "2026-10-23T00:00:01Z", "model": "shared-model", "aiConnection": testAIAlpha, "mode": "draft", "view": "chat", "interaction": "work", "checkpoint": map[string]any{"state": state, "sources": []any{}}}
	w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", "absent", map[string]any{"version": 1, "chats": []any{chat}})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	return s, token
}

func assistantCreationActualStart(t *testing.T, s *Server, token string, action assistantCreationAction, n int) assistantRunStart {
	t.Helper()
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	fields, rows, err := assistantChatRows(saved.Content)
	assistantRunMust(t, err)
	chat, err := assistantTestObject(rows[0])
	assistantRunMust(t, err)
	cp, err := assistantTestObject(chat["checkpoint"])
	assistantRunMust(t, err)
	state, err := assistantTestObject(cp["state"])
	assistantRunMust(t, err)
	turns, err := assistantChatArray(state["turns"])
	assistantRunMust(t, err)
	request := fmt.Sprintf("creation-%d", n)
	turn := map[string]any{"id": request, "role": "user", "kind": "message", "text": "Explicit creation control: " + action.Kind, "creation": action, "at": "2026-10-23T00:01:00Z"}
	if action.Kind == "research-choice" {
		turn["researchChoice"] = action.Choice
	}
	turns = append(turns, assistantChatJSON(turn))
	state["turns"] = assistantChatEncodeArray(turns)
	cp["state"] = assistantChatEncodeObject(state)
	chat["checkpoint"] = assistantChatEncodeObject(cp)
	chat["serverRequest"] = assistantChatJSON(map[string]string{"requestId": request, "admissionId": request})
	rows[0] = assistantChatEncodeObject(chat)
	fields["chats"] = assistantChatEncodeArray(rows)
	w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, json.RawMessage(assistantChatEncodeObject(fields)))
	if w.Code != 200 {
		t.Fatal("save action", w.Code, w.Body.String())
	}
	retained := policyWorkspaceRead(t, s, token, "/api/conversations")
	_, rows, err = assistantChatRows(retained.Content)
	assistantRunMust(t, err)
	return assistantRunStart{Purpose: "chat", OwnerID: "chat-one", RequestID: request, AdmissionID: request, Revision: digestOf(rows[0])}
}

func assistantCreationActualRun(t *testing.T, s *Server, token string, action assistantCreationAction, n int) assistantRunView {
	t.Helper()
	start := assistantCreationActualStart(t, s, token, action, n)
	first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 201)
	complete := assistantProtocolWait(t, s, token, first.ID, "complete")
	if complete.ChatProjection == nil || !complete.ChatProjection.Acknowledged {
		t.Fatal("terminal projection not acknowledged", complete.ProjectionIssue)
	}
	if again := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 200); again.ID != first.ID {
		t.Fatal("admission identity changed")
	}
	if dir := os.Getenv("JPS_CREATION_OUTPUTS"); dir != "" {
		engine := "api"
		registry, err := s.readAIRegistry()
		assistantRunMust(t, err)
		if registry.Connections[0].Assistant.Engine == "codex" {
			engine = "native"
		}
		if strings.Contains(t.Name(), "Interrupted") {
			engine += "-interruption"
		}
		chat := policyWorkspaceRead(t, s, token, "/api/conversations")
		raw, err := json.Marshal(map[string]any{"run": complete, "chat": json.RawMessage(chat.Content)})
		assistantRunMust(t, err)
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%02d-%s.json", engine, n, action.Kind)), raw, 0600))
		stream := assistantBriefCall(s, token, "GET", "/api/assistant-runs/"+first.ID+"/events", nil)
		if stream.Code != 200 {
			t.Fatal("events", stream.Code)
		}
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%02d-%s.sse", engine, n, action.Kind)), stream.Body.Bytes(), 0600))
	}
	return complete
}

// Test setup uses the same public lifecycle stores as the mounted browser.
// It copies retained display data only; the next action still needs fresh host
// source resolution and Runtime checks. The browser's own merge is tested in TS.
func assistantCreationRetainDraft(t *testing.T, s *Server, token string) {
	t.Helper()
	conversations := policyWorkspaceRead(t, s, token, "/api/conversations")
	fields, chats, err := assistantChatRows(conversations.Content)
	assistantRunMust(t, err)
	chat, err := assistantTestObject(chats[0])
	assistantRunMust(t, err)
	saved := policyWorkspaceRead(t, s, token, "/api/draft-packs")
	var document struct {
		Version int               `json:"version"`
		Drafts  []json.RawMessage `json:"drafts"`
		Deleted []string          `json:"deleted"`
	}
	assistantRunMust(t, json.Unmarshal(saved.Content, &document))
	generation := 1
	if len(document.Drafts) > 0 {
		draft, err := assistantTestObject(document.Drafts[0])
		assistantRunMust(t, err)
		assistantRunMust(t, json.Unmarshal(draft["generation"], &generation))
		generation++
	}
	draft := map[string]any{"id": "draft-creation", "generation": generation, "title": "Creation fixture", "folderId": "home", "mode": "draft", "createdAt": "2026-10-23T00:00:00Z", "updatedAt": "2026-10-23T00:01:00Z", "documents": []any{}, "checkpoint": json.RawMessage(chat["checkpoint"])}
	w := policyWorkspaceCall(s, token, "PUT", "/api/draft-packs", saved.SHA256, map[string]any{"version": 1, "drafts": []any{draft}, "deleted": []any{}})
	if w.Code != 200 {
		t.Fatal("retain artifact", w.Code, w.Body.String())
	}
	chat["draftId"] = assistantChatJSON("draft-creation")
	chat["draftGeneration"] = assistantChatJSON(generation)
	chats[0] = assistantChatEncodeObject(chat)
	fields["chats"] = assistantChatEncodeArray(chats)
	w = policyWorkspaceCall(s, token, "PUT", "/api/conversations", conversations.SHA256, json.RawMessage(assistantChatEncodeObject(fields)))
	if w.Code != 200 {
		t.Fatal("retain artifact link", w.Code, w.Body.String())
	}
}

func TestAssistantCreationActualAPI(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		for _, private := range []string{"literal-rule", "A true literal condition.", "Creation review retained."} {
			if index > 2 {
				break
			}
			if strings.Contains(string(raw), private) {
				t.Error("independent reviewer received draft rules or assistant history", private)
			}
		}
		switch index {
		case 1:
			assistantBriefSSE(w, assistantAuthoringProposalFixture(json.RawMessage(assistantCreationCasesDocument)), "stop")
		case 2:
			assistantBriefSSE(w, assistantAuthoringProposalFixture(json.RawMessage(assistantCreationCorrectionDocument)), "stop")
		case 3:
			assistantBriefSSE(w, "I will use the provided material. No action or artifact was produced.", "stop")
		default:
			t.Error("unexpected model request", index)
			assistantBriefSSE(w, "No hidden action.", "stop")
		}
	}))
	defer provider.Close()
	s, token := assistantCreationActualFixture(t, provider.URL)
	assistantCreationActualJourney(t, s, token, calls.Load, func(string) {})
	choice := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "research-choice", Choice: "provided"}, 6)
	if calls.Load() != 3 || choice.Creation != nil || len(choice.Events) == 0 {
		t.Fatal("explicit read-only choice did not use a fresh canonical request")
	}
}

func TestAssistantCreationLegacyRecovery(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		t.Error("legacy message recovery dispatched a model")
		assistantBriefSSE(w, "Unexpected model request.", "stop")
	}))
	defer provider.Close()
	s, token := assistantCreationActualFixture(t, provider.URL)
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	fields, chats, err := assistantChatRows(saved.Content)
	assistantRunMust(t, err)
	chat, err := assistantTestObject(chats[0])
	assistantRunMust(t, err)
	cp, err := assistantTestObject(chat["checkpoint"])
	assistantRunMust(t, err)
	state, err := assistantTestObject(cp["state"])
	assistantRunMust(t, err)
	turns, err := assistantChatArray(state["turns"])
	assistantRunMust(t, err)
	message, err := assistantTestObject(turns[1])
	assistantRunMust(t, err)
	delete(message, "id")
	turns[1] = assistantChatEncodeObject(message)
	state["turns"] = assistantChatEncodeArray(turns)
	cp["state"] = assistantChatEncodeObject(state)
	chat["checkpoint"] = assistantChatEncodeObject(cp)
	chats[0] = assistantChatEncodeObject(chat)
	fields["chats"] = assistantChatEncodeArray(chats)
	w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, json.RawMessage(assistantChatEncodeObject(fields)))
	if w.Code != 200 {
		t.Fatal("retain legacy message", w.Code, w.Body.String())
	}
	recovered := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "recover", MessageID: "legacy-turn-1", MessageDigest: digestOf([]byte(assistantAuthoringProposalFixture(json.RawMessage(reviewPack))))}, 1)
	if calls.Load() != 0 || recovered.Creation == nil || recovered.Creation.Check == nil || !recovered.Creation.Check.Valid {
		t.Fatal("exact legacy message recovery lost current validation")
	}
}

func assistantCreationActualJourney(t *testing.T, s *Server, token string, calls func() int32, next func(string)) {
	t.Helper()
	digest := digestOf([]byte(strings.TrimSpace(reviewPack)))
	recovered := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "recover", MessageID: "assistant-one", MessageDigest: digestOf([]byte(assistantAuthoringProposalFixture(json.RawMessage(reviewPack))))}, 1)
	if recovered.Creation == nil || recovered.Creation.Check == nil || !recovered.Creation.Check.Valid {
		t.Fatal("recover did not validate exact retained proposal")
	}
	digest = recovered.Creation.CandidateDigest
	assistantCreationRetainDraft(t, s, token)
	checked := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "check", CandidateDigest: digest}, 2)
	if checked.Creation.Check.TotalCases != 0 || calls() != 0 {
		t.Fatal("model-free check acquired cases")
	}
	next(assistantCreationCasesDocument)
	established := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "establish-cases", CandidateDigest: digest}, 3)
	if established.Creation == nil || !strings.Contains(established.Creation.Cases, `"zero":0`) || !strings.Contains(established.Creation.Issues, `"admitted":true`) || calls() != 1 {
		t.Fatal("independent cases/findings missing", established.Creation, calls())
	}
	assistantCreationRetainDraft(t, s, token)
	next(assistantCreationCorrectionDocument)
	proposal := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "propose-correction", CandidateDigest: digest, IssueID: "invalid"}, 4)
	if proposal.Creation.Proposal == nil || calls() != 2 {
		t.Fatal("current correction missing")
	}
	approved := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "approve-correction", CandidateDigest: digest, IssueID: "invalid", Token: proposal.Creation.Proposal.Token}, 5)
	if approved.Creation.Check == nil || approved.Creation.Check.TotalCases != 2 || !strings.Contains(string(approved.Creation.Check.Report), `"status":"passed"`) || approved.Creation.Issues != "[]" || calls() != 2 {
		t.Fatal("approval did not retest complete unchanged suite", approved.Creation)
	}
	assistantCreationRetainDraft(t, s, token)
}

func TestAssistantCreationActualNative(t *testing.T) {
	s, token := assistantCreationActualFixture(t, "https://example.invalid")
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
	fixture := t.TempDir()
	binary, trace := filepath.Join(fixture, "codex-peer"), filepath.Join(fixture, "trace.jsonl")
	peer := strings.Replace(assistantExecutionCodexPeer, "text='Fixture answer.'", "text=open(trace+'.reply').read()", 1)
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
	calls := func() int32 {
		if _, err := os.Stat(trace); os.IsNotExist(err) {
			return 0
		}
		var n int32
		for _, row := range assistantCodexLog(t, trace) {
			if string(row["type"]) == `"generation"` {
				n++
				if n <= 2 && strings.Contains(string(row["prompt"]), "literal-rule") {
					t.Error("native reviewer received draft rules")
				}
			}
		}
		return n
	}
	assistantCreationActualJourney(t, s, token, calls, func(document string) {
		assistantRunMust(t, os.WriteFile(trace+".reply", []byte(assistantAuthoringProposalFixture(json.RawMessage(document))), 0600))
	})
	assistantRunMust(t, os.WriteFile(trace+".reply", []byte("I will use only the provided material."), 0600))
	choice := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "research-choice", Choice: "provided"}, 6)
	if calls() != 3 || choice.Creation != nil {
		t.Fatal("native explicit research choice did not remain read-only")
	}
}

// This deliberately incomplete peer supplies no case or matrix acceptance.
// It stops delivery at a known finite call; retry/retest then uses the actual
// released Runtime above, preserving all independently proposed rows.
func assistantCreationBlockedRuntime(t *testing.T, s *Server, blocked string) string {
	t.Helper()
	data, err := s.openChatData()
	assistantRunMust(t, err)
	ledgerPath := filepath.Join(data.root.Name(), s.assistantRuns().name)
	assistantRunMust(t, data.root.Close())
	writeProjectFile(t, s.projectDir, "execution-fixture.json", string(assistantChatJSON(map[string]string{"ledger": ledgerPath})))
	marker := filepath.Join(s.projectDir, "creation-finite-call-entered")
	branch := fmt.Sprintf(`
 elif method=='tools/call' and name==%q:
  with open('creation-finite-call-entered','w') as f: f.write('entered')
  time.sleep(30)
  sys.exit(0)
 elif method=='tools/call' and name=='validate': result={'content':[],'structuredContent':{'status':'valid','diagnostics':[]}}
 elif method=='tools/call' and name=='experimental_validate_expectations': result={'content':[],'structuredContent':{'specVersion':'0.2.0-draft','status':'valid','results':[{'index':i,'status':'valid','canonical':%q} for i,_ in enumerate(p['arguments']['expectations'])]}}
	`, blocked, `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"accept","reasons":[]}`)
	branch = strings.TrimRight(branch, "\t\r\n ") + "\n"
	peer := strings.Replace(assistantExecutionPeer, " else:\n  mode=", branch+" else:\n  mode=", 1)
	peer = strings.Replace(peer, "for run in ledger['runs'] for op", "for run in ledger['runs'] if run['state']=='running' for op", 1)
	peer = strings.Replace(peer, "{'name':'served','arguments':[]}", "{'name':'test_pack','arguments':[]}", 1)
	bin := filepath.Join(t.TempDir(), "creation-incomplete-runtime")
	assistantRunMust(t, os.WriteFile(bin, []byte(peer), 0700))
	s.cfg.JpackBin = bin
	return marker
}

func assistantCreationStopAt(t *testing.T, s *Server, token string, action assistantCreationAction, n int, marker string) assistantRunView {
	t.Helper()
	_ = os.Remove(marker)
	start := assistantCreationActualStart(t, s, token, action, n)
	first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 201)
	deadline := time.Now().Add(12 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("finite call did not enter")
		}
		time.Sleep(10 * time.Millisecond)
	}
	current := assistantProtocolView(t, assistantBriefCall(s, token, "GET", "/api/assistant-runs/"+first.ID, nil), 200)
	assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs/"+first.ID+"/actions", assistantRunAction{ID: fmt.Sprintf("stop-%d", n), Kind: "cancel", Revision: current.Revision}), 200)
	stopped := assistantProtocolWait(t, s, token, first.ID, "stopped")
	if stopped.ChatProjection == nil || !stopped.ChatProjection.Acknowledged {
		t.Fatal("stopped original was not projected")
	}
	return stopped
}

func TestAssistantCreationInterruptedValidationAndRetest(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			assistantBriefSSE(w, assistantAuthoringProposalFixture(json.RawMessage(assistantCreationCasesDocument)), "stop")
		case 2:
			assistantBriefSSE(w, assistantAuthoringProposalFixture(json.RawMessage(assistantCreationCorrectionDocument)), "stop")
		default:
			t.Error("unexpected reviewer replay")
			assistantBriefSSE(w, "No replay.", "stop")
		}
	}))
	defer provider.Close()
	s, token := assistantCreationActualFixture(t, provider.URL)
	recovered := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "recover", MessageID: "assistant-one", MessageDigest: digestOf([]byte(assistantAuthoringProposalFixture(json.RawMessage(reviewPack))))}, 1)
	digest := recovered.Creation.CandidateDigest
	assistantCreationRetainDraft(t, s, token)
	actual := s.cfg.JpackBin
	marker := assistantCreationBlockedRuntime(t, s, "experimental_validate_expectations")
	stopped := assistantCreationStopAt(t, s, token, assistantCreationAction{Kind: "establish-cases", CandidateDigest: digest}, 2, marker)
	if stopped.Creation == nil || stopped.Creation.Held == "[]" || stopped.Creation.Cases != "[]" || calls.Load() != 1 {
		t.Fatal("interrupted validator lost the exact independent proposal")
	}
	assistantCreationRetainDraft(t, s, token)
	s.cfg.JpackBin = actual
	retried := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "retry-validation", CandidateDigest: digest}, 3)
	if calls.Load() != 1 || !strings.Contains(retried.Creation.Cases, `"zero":0`) || !strings.Contains(retried.Creation.Issues, `"id":"invalid"`) {
		t.Fatal("retry changed or regenerated held cases")
	}
	assistantCreationRetainDraft(t, s, token)
	proposal := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "propose-correction", CandidateDigest: digest, IssueID: "invalid"}, 4)
	approve := assistantCreationAction{Kind: "approve-correction", CandidateDigest: digest, IssueID: "invalid", Token: proposal.Creation.Proposal.Token}
	marker = assistantCreationBlockedRuntime(t, s, "experimental_test_cases")
	rollback := assistantCreationStopAt(t, s, token, approve, 5, marker)
	if rollback.Creation == nil || rollback.Creation.Check != nil || rollback.Creation.Proposal == nil || !strings.Contains(rollback.Creation.Issues, `"id":"invalid"`) || rollback.Creation.Cases != proposal.Creation.Cases {
		t.Fatal("interrupted retest resolved or changed an issue")
	}
	s.cfg.JpackBin = actual
	approved := assistantCreationActualRun(t, s, token, approve, 6)
	if approved.Creation.Check == nil || approved.Creation.Check.TotalCases != 2 || approved.Creation.Issues != "[]" || calls.Load() != 2 {
		t.Fatal("explicit approval retry omitted a case or repeated the reviewer")
	}
}

func TestAssistantCreationOriginalCaseOrder(t *testing.T) {
	t.Setenv("JPS_CREATION_OUTPUTS", "")
	fields, err := assistantTestObject([]byte(assistantCreationCasesDocument))
	assistantRunMust(t, err)
	rows, err := assistantChatArray(fields["cases"])
	assistantRunMust(t, err)
	proposed := assistantChatJSON(map[string]any{"cases": []json.RawMessage{rows[1], rows[0]}})
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			assistantBriefSSE(w, assistantAuthoringProposalFixture(json.RawMessage(proposed)), "stop")
		} else {
			assistantBriefSSE(w, assistantAuthoringProposalFixture(json.RawMessage(assistantCreationCorrectionDocument)), "stop")
		}
	}))
	defer provider.Close()
	s, token := assistantCreationActualFixture(t, provider.URL)
	recovered := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "recover", MessageID: "assistant-one", MessageDigest: digestOf([]byte(assistantAuthoringProposalFixture(json.RawMessage(reviewPack))))}, 1)
	digest := recovered.Creation.CandidateDigest
	assistantCreationRetainDraft(t, s, token)
	established := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "establish-cases", CandidateDigest: digest}, 2)
	for _, run := range assistantAuthoringLedger(t, s).Runs {
		if run.ID == established.ID {
			if run.Creation.Issues[0].Position == nil || *run.Creation.Issues[0].Position != 0 {
				t.Fatal("new issue lost its first position")
			}
			run.Creation.Issues[0].Position = nil
			assistantRunMust(t, validateAssistantCreationReceipt(run)) // Earlier receipts remain readable.
		}
	}
	assistantCreationRetainDraft(t, s, token)
	proposal := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "propose-correction", CandidateDigest: digest, IssueID: "invalid"}, 3)
	approved := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "approve-correction", CandidateDigest: digest, IssueID: "invalid", Token: proposal.Creation.Proposal.Token}, 4)
	var cases []assistantCreationCase
	assistantRunMust(t, json.Unmarshal([]byte(approved.Creation.Cases), &cases))
	if len(cases) != 2 || cases[0].ID != "invalid" || cases[1].ID != "typed" {
		t.Fatalf("independent proposal order [invalid typed] changed on approval: %s", approved.Creation.Cases)
	}
	// A syntactically complete mismatch is still inadmissible as a retained
	// completed approval even when its exact Runtime result agrees with the receipt.
	for _, run := range assistantAuthoringLedger(t, s).Runs {
		if run.ID == approved.ID {
			assistantRunMust(t, validateAssistantCreationReceipt(run))
			report, err := assistantTestObject(run.Creation.Check.Report)
			assistantRunMust(t, err)
			report["status"] = []byte(`"mismatch"`)
			report["summary"] = []byte(`{"total":2,"passed":1}`)
			packs, err := assistantChatArray(report["packs"])
			assistantRunMust(t, err)
			pack, err := assistantTestObject(packs[0])
			assistantRunMust(t, err)
			results, err := assistantChatArray(pack["rows"])
			assistantRunMust(t, err)
			result, err := assistantTestObject(results[0])
			assistantRunMust(t, err)
			result["status"] = []byte(`"mismatch"`)
			result["actual"] = assistantChatJSON(`{}`)
			results[0] = assistantChatEncodeObject(result)
			pack["rows"] = assistantChatEncodeArray(results)
			packs[0] = assistantChatEncodeObject(pack)
			report["packs"] = assistantChatEncodeArray(packs)
			run.Creation.Check.Report = assistantChatEncodeObject(report)
			for i, op := range run.Operations {
				if op.ID == run.Creation.TestID {
					reply, err := assistantTestObject(op.Result)
					assistantRunMust(t, err)
					body, err := assistantTestObject(reply["result"])
					assistantRunMust(t, err)
					body["structuredContent"] = run.Creation.Check.Report
					reply["result"] = assistantChatEncodeObject(body)
					run.Operations[i].Result = assistantChatEncodeObject(reply)
				}
			}
			if validateAssistantCreationReceipt(run) == nil {
				t.Fatal("retained completed mismatch approval admitted")
			}
		}
	}

}

func TestAssistantCreationFailedRetestRetainsIssue(t *testing.T) {
	t.Setenv("JPS_CREATION_OUTPUTS", "")
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			assistantBriefSSE(w, assistantAuthoringProposalFixture(json.RawMessage(assistantCreationCasesDocument)), "stop")
		} else {
			assistantBriefSSE(w, assistantAuthoringProposalFixture(json.RawMessage(strings.ReplaceAll(assistantCreationCorrectionDocument, `"accept"`, `"reject"`))), "stop")
		}
	}))
	defer provider.Close()
	s, token := assistantCreationActualFixture(t, provider.URL)
	recovered := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "recover", MessageID: "assistant-one", MessageDigest: digestOf([]byte(assistantAuthoringProposalFixture(json.RawMessage(reviewPack))))}, 1)
	digest := recovered.Creation.CandidateDigest
	assistantCreationRetainDraft(t, s, token)
	established := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "establish-cases", CandidateDigest: digest}, 2)
	assistantCreationRetainDraft(t, s, token)
	proposal := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "propose-correction", CandidateDigest: digest, IssueID: "invalid"}, 3)
	action := assistantCreationAction{Kind: "approve-correction", CandidateDigest: digest, IssueID: "invalid", Token: proposal.Creation.Proposal.Token}
	start := assistantCreationActualStart(t, s, token, action, 4)
	first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 201)
	failed := assistantProtocolWait(t, s, token, first.ID, "stopped")
	if failed.Creation == nil || failed.Creation.Check != nil || failed.Creation.Proposal == nil || failed.Creation.Cases != established.Creation.Cases || failed.Creation.Issues != established.Creation.Issues || calls.Load() != 2 {
		t.Fatal("mismatching retest published or lost staged correction", failed.Creation)
	}
	for _, run := range assistantAuthoringLedger(t, s).Runs {
		if run.ID == first.ID {
			if run.Creation.Approved != nil || run.Creation.Proposal == nil {
				t.Fatal("failed retest resolved original issue")
			}
			retained := false
			for _, op := range run.Operations {
				if op.State == "completed" && strings.Contains(string(op.Intent), `experimental_test_cases`) && strings.Contains(string(op.Intent), `reject`) {
					retained = true
				}
			}
			if !retained {
				t.Fatal("failed retest lost exact staged matrix operation")
			}
			assistantRunMust(t, validateAssistantCreationReceipt(run))
		}
	}
}
