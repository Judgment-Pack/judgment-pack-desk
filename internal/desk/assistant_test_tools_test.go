package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

const assistantTestPack = `{ "id":"one", "version":"1.0.0", "title":"Amount <record> 中文", "threshold":"50", "fixtureNumber":1.20 }`
const assistantTestCase = `{ "id":"existing","name":"Authored original","revision":1,"row":{"id":"existing","facts":{"amount":"4","Amount":"5","n":1.20,"flag":false,"zero":0,"null":null,"2":2,"10":10,"😀":"face","":"bmp"},"expectedDisposition":` + assistantCriticDisposition + `},"sources":[{"id":"requirement","name":"Requirement","text":"Boundary source <record> 中文."}],"sourceMappings":{},"origin":"manual","rationale":"Original requirement" }`
const assistantTestSuiteFixture = `{"revision":1,"cases":[` + assistantTestCase + `],"runs":[],"recovered":[],"unrelated":{"original":1.20,"Value":false,"value":null}}`
const assistantTestDocument = `{ "matrixVersion":"3", "cases":[
 {"id":"existing","facts":{"amount":"50","Amount":"51","n":1.20,"flag":false,"zero":0,"null":null,"2":2,"10":10,"😀":"face","":"bmp"},"expectedDisposition":` + assistantCriticDisposition + `},
 {"id":"wrong-type","facts":{"amount":0},"expectedErrorClass":"type-error"},
 {"id":"missing","facts":{},"expectedErrorClass":"missing-fact"}],
 "sourceMappings":{"existing":{"/amount":"requirement"}},
 "intentionalFactTypes":[{"caseId":"wrong-type","path":"/amount","reason":"Deliberately test a JSON number instead of a decimal string."}] }`

// This peer implements the existing Desk consumer contract, including fact-type
// fields that released Runtime v0.37.0 does not provide (Desk #582). Positive
// fixtures certify host protocol/custody only, never released Runtime support.
const assistantTestRuntimeBranch = `
 elif method=='tools/call' and name in ['experimental_get_test_matrix_contract','experimental_validate_test_matrix']:
  mode=open('test-host-mode').read() if os.path.exists('test-host-mode') else ''
  report={'contractVersion':'1','specVersion':'0.2.0-draft'}
  if name=='experimental_get_test_matrix_contract': report['contract']={'matrixVersion':'3','rowMembers':['id','facts','expectedDisposition','expectedErrorClass']}
  else:
   matrix=json.loads(p['arguments']['matrix'])
   if 'sourceMappings' in matrix or 'intentionalFactTypes' in matrix: sys.exit(44)
   rows=matrix['cases']
   report.update({'status':'valid','findings':[],'results':[{'index':i,'id':r['id'],'status':'valid','findings':[]} for i,r in enumerate(rows)],'factCheckVersion':'1','factChecks':[{'index':i,'id':r['id'],'status':'complete','findings':([{'code':'FACT-NOT-COMPARABLE','path':'/amount','message':'Ordered comparison requires a decimal string.','operator':'lt','factType':'number','expectedTypes':['decimal-string']}] if r['id']=='wrong-type' else [])} for i,r in enumerate(rows)]})
   if mode=='released-contract': report.pop('factCheckVersion');report.pop('factChecks')
   elif mode=='wrong-id': report['results'][0]['id']='foreign'
   elif mode=='omitted-row': report['results'].pop()
   elif mode=='unordered': report['results'][0]['index']=2
   elif mode=='incomplete': report['results'][0].pop('findings')
   elif mode=='unavailable': report['factChecks'][0]['status']='unavailable';report['factChecks'][0]['message']='No advice available'
  result={'content':[],'structuredContent':report}
`

func assistantTestHostFixture(t *testing.T, url, phase string, draft, native bool, review ...bool) (*Server, string, *assistantRunExecution, string) {
	t.Helper()
	entered := make(chan *assistantRunExecution, 1)
	s, token, start, executor := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		entered <- e
		<-ctx.Done()
		return ctx.Err()
	})
	connection := aiTestConnection(testAIAlpha, "Test host fixture", url)
	connection.Assistant.Endpoint.Tools = []string{"validate", "get_schema"}
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
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "jpack.json"), []byte(assistantGraphConfig), 0600))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte(assistantTestPack), 0600))
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	chat, err := assistantTestObject(doc.Chats[0])
	assistantRunMust(t, err)
	for key, value := range map[string]any{"interaction": phase, "adversarialReview": false, "title": "Test review", "composer": "", "pinned": false, "archived": false, "updatedAt": "2026-10-20T00:00:00Z", "mode": "draft", "view": "chat"} {
		chat[key] = assistantChatJSON(value)
	}
	if len(review) > 0 {
		chat["adversarialReview"] = assistantChatJSON(review[0])
	}
	owner := "one"
	if draft {
		owner = "draft-tests"
		chat["draftId"] = assistantChatJSON(owner)
		cp, _ := assistantTestObject(chat["checkpoint"])
		state, _ := assistantTestObject(cp["state"])
		state["turns"] = []byte(`[]`)
		state["responses"] = []byte(`[]`)
		for _, name := range []string{"seedUrls", "unknowns", "sessions", "cases", "expectationIssues", "questions"} {
			state[name] = []byte(`[]`)
		}
		state["brief"] = assistantChatJSON("Test the retained draft")
		state["revisionsUsed"] = []byte(`0`)
		cp["sources"] = []byte(`[]`)
		state["candidates"] = assistantChatJSON([]any{map[string]string{"text": assistantTestPack}})
		cp["state"] = assistantChatEncodeObject(state)
		draft := map[string]any{"id": owner, "generation": 1, "title": "Real test draft", "folderId": "home", "mode": "draft", "createdAt": "2026-10-20T00:00:00Z", "updatedAt": "2026-10-20T00:00:00Z", "documents": []any{}, "checkpoint": json.RawMessage(assistantChatEncodeObject(cp))}
		policyWorkspaceSave(t, s, token, "/api/draft-packs", map[string]any{"version": 1, "drafts": []any{draft}, "deleted": []string{}})
	} else {
		chat["pack"] = assistantChatJSON(map[string]string{"id": owner, "path": "one.json", "digest": digestOf([]byte(assistantTestPack))})
	}
	doc.Chats[0] = assistantChatEncodeObject(chat)
	policyWorkspaceSave(t, s, token, "/api/conversations", doc)
	policyWorkspaceSave(t, s, token, "/api/pack-tests", json.RawMessage(`{"version":1,"suites":{"`+owner+`":`+assistantTestSuiteFixture+`,"untouched": { "revision":9,"cases":[],"runs":[],"recovered":[],"data":{"Value":false,"value":null,"n":1.20} }}}`))
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
		t.Fatal("Test host execution did not start")
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
			t.Error("Test host execution did not drain")
		}
	})
	return s, token, e, trace
}
func assistantTestHostOpen(t *testing.T, s *Server, e *assistantRunExecution) *assistantExecution {
	t.Helper()
	assistantExecutionRuntimeFixture(t, s, e)
	peer := strings.Replace(assistantExecutionPeer, " elif method=='prompts/list':", assistantTestRuntimeBranch+"\n elif method=='prompts/list':", 1)
	peer = strings.Replace(peer, "{'name':'served','arguments':[]}", "{'name':'served','arguments':[]},{'name':'test_pack','arguments':[{'name':'pack','required':False}]}", 1)
	peer = strings.Replace(peer, "'text':'SERVED-RUNTIME-PROMPT'", "'text':('Runtime test design: retain decimal strings and independent expectations. PACK '+p['arguments'].get('pack','')) if name=='test_pack' else 'SERVED-RUNTIME-PROMPT'", 1)
	assistantRunMust(t, os.WriteFile(s.cfg.JpackBin, []byte(peer), 0700))
	x, err := e.openAssistantExecution(t.Context(), assistantExecutionPrompt{Name: "served"})
	assistantRunMust(t, err)
	t.Cleanup(x.Close)
	return x
}
func assistantTestHostInvoke(t *testing.T, e *assistantRunExecution, x *assistantExecution, id, name string, args any) runcontext.ToolResult {
	t.Helper()
	raw, _ := json.Marshal(args)
	assistantPolicyEvent(t, e, id, name, raw)
	ctx := context.WithValue(runcontext.WithCallID(t.Context(), id), assistantHostRootKey{}, true)
	result, err := assistantGraphFind(t, x.Run.HostTools, name).Execute(ctx, raw)
	assistantRunMust(t, err)
	assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolResult: &contract.ToolResult{Type: "tool_result", CallID: &id, Name: name, IsError: result.Err(), Text: result.Content[0].Text()}}))
	return result
}
func assistantTestHostProposalArgs() map[string]string {
	return map[string]string{"document": assistantTestDocument, "summary": "Boundary and deliberate wrong-type cases <record> 中文"}
}
func assistantTestHostSuite(t *testing.T, s *Server, token, owner string) map[string]json.RawMessage {
	t.Helper()
	lock, err := s.privateDataLockContext(t.Context(), true)
	assistantRunMust(t, err)
	defer lock.Close()
	store, err := s.openChatData()
	assistantRunMust(t, err)
	defer store.root.Close()
	name, err := resolveConversationName(store.root, s.conversationIdentity())
	assistantRunMust(t, err)
	saved, err := s.readWorkspaceRecord(store.root, strings.Replace(name, "conversations-", "pack-tests-", 1), false)
	assistantRunMust(t, err)
	fields, err := assistantTestObject(saved.Content)
	assistantRunMust(t, err)
	suites, err := assistantTestObject(fields["suites"])
	assistantRunMust(t, err)
	suite, err := assistantTestSuite(suites[owner])
	assistantRunMust(t, err)
	return suite
}

func TestAssistantTestHostSavedAndDraftCustody(t *testing.T) {
	for _, draft := range []bool{false, true} {
		t.Run(fmt.Sprint(draft), func(t *testing.T) {
			s, token, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", draft, false)
			x := assistantTestHostOpen(t, s, e)
			owner := "one"
			if draft {
				owner = "draft-tests"
			}
			before := assistantTestHostSuite(t, s, token, owner)
			no := assistantTestHostInvoke(t, e, x, "before-read", "propose_test_cases", assistantTestHostProposalArgs())
			if !no.Err() {
				t.Fatal("proposal without read accepted")
			}
			read := assistantTestHostInvoke(t, e, x, "read", "read_pack_tests", map[string]any{})
			if read.Err() || !strings.Contains(read.Content[0].Text(), "1.20") {
				t.Fatal(read.Value().Text())
			}
			result := assistantTestHostInvoke(t, e, x, "propose", "propose_test_cases", assistantTestHostProposalArgs())
			if result.Err() {
				t.Fatal(result.Value().Text())
			}
			record := assistantExecutionRecord(t, e)
			if len(record.DomainOutputs) != 1 || record.DomainOutputs[0].TestProposal.Read.Draft != draft {
				t.Fatal("missing exact test output")
			}
			assistantRunMust(t, validateAssistantDomainOutputs(record))
			output := record.DomainOutputs[0].TestProposal
			owner = output.Read.Owner
			suite := assistantTestHostSuite(t, s, token, owner)
			if !bytes.Equal(suite["cases"], before["cases"]) || !bytes.Equal(suite["unrelated"], before["unrelated"]) {
				t.Fatal("proposal changed saved cases or unrelated records")
			}
			proposals, err := assistantChatArray(suite["proposals"])
			assistantRunMust(t, err)
			if len(proposals) != 1 || !bytes.Equal(proposals[0], output.Proposal) {
				t.Fatal("suite did not read back exact proposal")
			}
			raw, _ := json.Marshal(assistantTestHostProposalArgs())
			ctx := context.WithValue(runcontext.WithCallID(t.Context(), "propose"), assistantHostRootKey{}, true)
			count := len(assistantExecutionRuntimeLog(t, s))
			again, err := assistantGraphFind(t, x.Run.HostTools, "propose_test_cases").Execute(ctx, raw)
			assistantRunMust(t, err)
			if again.Value().Text() != result.Value().Text() || len(assistantExecutionRuntimeLog(t, s)) != count || len(assistantExecutionRecord(t, e).DomainOutputs) != 1 {
				t.Fatal("completed proposal replayed work")
			}
			if draft {
				cp := *assistantExecutionRecord(t, e).Checkpoint
				cp.Status = "complete"
				assistantRunMust(t, e.Checkpoint(t.Context(), cp))
				assistantRunMust(t, e.writer.State(t.Context(), "complete"))
				assistantRunMust(t, e.ProjectChat(t.Context()))
				for env, path := range map[string]string{"DESK_TEST_HOST_DRAFT_CHAT_OUTPUT": "/api/conversations", "DESK_TEST_HOST_DRAFT_SUITE_OUTPUT": "/api/pack-tests", "DESK_TEST_HOST_DRAFTS_OUTPUT": "/api/draft-packs"} {
					if file := os.Getenv(env); file != "" {
						saved := policyWorkspaceRead(t, s, token, path)
						assistantRunMust(t, os.WriteFile(file, saved.Content, 0600))
					}
				}
			}
		})
	}
}
func TestAssistantTestHostActualAPIAndReaders(t *testing.T) {
	var step atomic.Int32
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := step.Add(1)
		switch n {
		case 1:
			assistantPolicySSE(w, "read_pack_tests", map[string]any{}, "")
		case 2:
			assistantPolicySSE(w, "propose_test_cases", assistantTestHostProposalArgs(), "")
		default:
			assistantPolicySSE(w, "", nil, "Review the proposed test cases.")
		}
	})
	s, token, e, _ := assistantTestHostFixture(t, u.server.URL, "work", false, false)
	x := assistantTestHostOpen(t, s, e)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	if step.Load() != 3 {
		t.Fatal("unexpected model work")
	}
	record := assistantExecutionRecord(t, e)
	if len(record.DomainOutputs) != 1 {
		t.Fatal("actual API producer missing", e.writer.Err())
	}
	assistantRunMust(t, validateAssistantDomainOutputs(record))
	// Retain and decode the real completed checkpoint, then prepare/write but
	// deliberately lose acknowledgement before explicit model-free recovery.
	assistantRunMust(t, e.writer.change(t.Context(), false, func(run *assistantRunRecord) (bool, error) { run.State = "complete"; return true, nil }))
	assistantRunMust(t, s.prepareAssistantChatProjection(e.request.Clone(t.Context()), e.record.ID))
	assistantRunMust(t, s.writeAssistantChatProjection(e.request.Clone(t.Context()), e.record.ID))
	record = assistantExecutionRecord(t, e)
	raw, _ := json.Marshal(assistantRunLedger{Version: 1, Project: record.Binding.Project, Runs: []assistantRunRecord{record}})
	_, err := decodeAssistantRunStorage(e.writer.lease.store.name, raw)
	assistantRunMust(t, err)
	calls := len(assistantExecutionRuntimeLog(t, s))
	assistantRunMust(t, e.ProjectChat(t.Context()))
	assistantRunMust(t, e.ProjectChat(t.Context()))
	if step.Load() != 3 || len(assistantExecutionRuntimeLog(t, s)) != calls {
		t.Fatal("projection recovery replayed work")
	}
	chat := policyWorkspaceRead(t, s, token, "/api/conversations")
	tests := policyWorkspaceRead(t, s, token, "/api/pack-tests")
	if !strings.Contains(string(chat.Content), `"testDesign"`) || !strings.Contains(string(tests.Content), `"caseSnapshots"`) {
		t.Fatal("proposal or review link missing")
	}
	if path := os.Getenv("DESK_TEST_HOST_CHAT_OUTPUT"); path != "" {
		assistantRunMust(t, os.WriteFile(path, chat.Content, 0600))
	}
	if path := os.Getenv("DESK_TEST_HOST_SUITE_OUTPUT"); path != "" {
		assistantRunMust(t, os.WriteFile(path, tests.Content, 0600))
	}
}
func TestAssistantTestHostActualNativeParentChild(t *testing.T) {
	s, token, e, trace := assistantTestHostFixture(t, "https://example.invalid", "work", false, true)
	args, _ := json.Marshal(assistantTestHostProposalArgs())
	peer := strings.Replace(assistantExecutionCodexPeer, "if mode=='delegate' and index==1:\n   tool(offered('delegate_tasks'),{'tasks':[{'title':'Inspect schema','objective':'Inspect the schema only.','profile':'review'}]})", `if mode=='delegate' and index==1: tool(offered('read_pack_tests'),{},call='same-provider-call')
  elif mode=='delegate' and index==2: tool(offered('read_pack_tests'),{},call='same-provider-call')
  elif mode=='delegate' and index==3: tool(offered('propose_test_cases'),json.loads(`+fmt.Sprintf("%q", string(args))+`),call='same-provider-call')`, 1)
	peer = strings.Replace(peer, "if mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')", `if mode=='delegate' and index==1: tool(offered('delegate_tasks'),{'tasks':[{'title':'Read tests','objective':'Read the linked tests only.','profile':'review'}]},call='delegate',rpc='rpc-delegate')
  elif mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')`, 1)
	assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
	x := assistantTestHostOpen(t, s, e)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	record := assistantExecutionRecord(t, e)
	if len(record.DomainOutputs) != 1 {
		t.Fatal("native producer missing", e.writer.Err())
	}
	callbacks := map[string]bool{}
	roots := map[bool]int{}
	for _, op := range record.Operations {
		if op.Kind != "host" || !assistantTestTool(op.Name) {
			continue
		}
		var in assistantHostIntent
		assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
		if in.CallbackID == "" || callbacks[in.CallbackID] {
			t.Fatal("native callbacks collapsed")
		}
		callbacks[in.CallbackID] = true
		roots[in.Root]++
	}
	if roots[true] != 2 || roots[false] != 1 {
		t.Fatal("native root/child custody changed", roots)
	}
	assistantRunMust(t, validateAssistantDomainOutputs(record))
	suite := assistantTestHostSuite(t, s, token, "one")
	proposals, _ := assistantChatArray(suite["proposals"])
	if len(proposals) != 1 {
		t.Fatal("native suite proposal missing")
	}
}
