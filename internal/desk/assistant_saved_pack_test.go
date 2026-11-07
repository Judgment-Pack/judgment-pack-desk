package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const assistantSavedPackCandidate = `{ "id":"one", "version":"1.0.1", "title":"Amount <record> 中文", "threshold":"51", "fixtureNumber":1.20 }`
const assistantSavedPackCorrected = `{ "id":"one", "version":"1.0.2", "title":"Amount <record> 中文", "threshold":"50", "fixtureNumber":1.20 }`
const assistantSavedPackEditor = `{ "id":"one", "version":"1.0.0", "title":"Unsaved editor", "threshold":"50", "fixtureNumber":1.20 }`

// This peer checks actual durable candidate custody before each physical check,
// and exact retained findings before a subsequent candidate can be checked.
const assistantSavedPackRuntimeBranch = `
 elif method=='tools/call' and name in ['validate','experimental_test_cases']:
  mode=open('saved-pack-mode').read() if os.path.exists('saved-pack-mode') else ''
  text=p['arguments'].get('document',p['arguments'].get('pack',''))
  proposals=[o for run in ledger['runs'] for o in (run.get('domainOutputs') or []) if o.get('packProposal',{}).get('text')==text]
  if len(proposals)!=1: sys.exit(61)
  if json.loads(text)['version']=='1.0.2':
   earlier=[o for run in ledger['runs'] for o in (run.get('domainOutputs') or []) if o.get('packProposal',{}).get('text')!=text]
   if not earlier or earlier[0]['packProposal'].get('check',{}).get('report',{}).get('status')!='mismatch': sys.exit(62)
  if name=='validate': report={'status':'valid','diagnostics':[]}
  else:
   rows=json.loads(p['arguments']['matrix'])['cases']
   if '1.20' not in p['arguments']['matrix']: sys.exit(63)
   ok=json.loads(text)['threshold']=='50'
   report={'status':'passed' if ok else 'mismatch','summary':{'total':len(rows),'passed':len(rows) if ok else 0},'packs':[{'rows':[{'id':r['id'],'expected':'independently authored','actual':'observed','status':'passed' if ok else 'mismatch'} for r in rows],'coverage':[{'probe':'boundary','status':'missing','detail':'Review feasible policy boundary.'}]}]}
   if mode=='incomplete': report['packs'][0]['rows'].pop()
   if mode=='foreign-id': report['packs'][0]['rows'][0]['id']='foreign'
   if mode=='wrong-summary': report['summary']['passed']=999
   if mode=='runtime-failure':
    print(json.dumps({'jsonrpc':'2.0','id':q['id'],'result':{'isError':True,'content':[{'type':'text','text':'internal error details must not escape'}]}}),flush=True);continue
   if mode=='changed-suite':
    with open('saved-suite-path') as f: suite_path=f.read()
    with open(suite_path) as f: suites=json.load(f)
    suites['suites']['one']['cases'][0]['rationale']='Changed while Runtime was running'
    with open(suite_path,'w') as f: json.dump(suites,f)
  result={'content':[],'structuredContent':report}
`

func assistantSavedPackOpen(t *testing.T, s *Server, e *assistantRunExecution) *assistantExecution {
	t.Helper()
	assistantExecutionRuntimeFixture(t, s, e)
	peer := strings.Replace(assistantExecutionPeer, " elif method=='prompts/list':", assistantSavedPackRuntimeBranch+"\n elif method=='prompts/list':", 1)
	assistantRunMust(t, os.WriteFile(s.cfg.JpackBin, []byte(peer), 0700))
	x, err := e.openAssistantExecution(t.Context(), assistantExecutionPrompt{Name: "served"})
	assistantRunMust(t, err)
	t.Cleanup(x.Close)
	return x
}
func assistantSavedPackFinish(t *testing.T, s *Server, e *assistantRunExecution) {
	t.Helper()
	assistantRunMust(t, e.writer.change(t.Context(), false, func(run *assistantRunRecord) (bool, error) { run.State = "complete"; return true, nil }))
	assistantRunMust(t, e.ProjectChat(t.Context()))
}
func TestAssistantSavedPackActualAPIAndReload(t *testing.T) {
	var step atomic.Int32
	var e *assistantRunExecution
	var u *upstream
	u = newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := step.Add(1)
		if n == 1 && !bytes.Contains(u.arrivals()[0].body, []byte("Original requirement")) {
			t.Error("first author request omitted saved rationale")
		}
		if n == 2 {
			held := assistantExecutionRecord(t, e)
			if len(held.DomainOutputs) != 1 || held.DomainOutputs[0].PackProposal.Check == nil {
				t.Error("correction requested before retained findings")
			}
			if !bytes.Contains(u.arrivals()[1].body, []byte("needs-correction")) {
				t.Error("correction did not receive exact findings")
			}
		}
		switch n {
		case 1:
			assistantPolicySSE(w, assistantSavedPackTool, map[string]string{"document": assistantSavedPackCandidate}, "")
		case 2:
			assistantPolicySSE(w, assistantSavedPackTool, map[string]string{"document": assistantSavedPackCorrected}, "")
		default:
			assistantPolicySSE(w, "", nil, "Review the checked proposal.")
		}
	})
	capture := &assistantPackBasis{"one", "one.json", assistantSavedPackEditor}
	s, token, execution, _ := assistantTestHostFixtureCapture(t, u.server.URL, "work", false, false, capture)
	e = execution
	before := assistantTestHostSuite(t, s, token, "one")
	x := assistantSavedPackOpen(t, s, e)
	if x.Run.Context.ArtifactProposals == nil || *x.Run.Context.ArtifactProposals {
		t.Fatal("generic linked artifact route remained enabled")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	record := assistantExecutionRecord(t, e)
	if step.Load() != 3 || len(record.DomainOutputs) != 2 {
		t.Fatal("same-turn correction failed", step.Load(), record.DomainOutputs)
	}
	if record.SavedPack.Basis.Text != assistantSavedPackEditor || record.Binding.ExecutionKey != record.Checkpoint.RequestKey || record.Checkpoint.Used != 3 {
		t.Fatal("basis or shared allowance changed")
	}
	for _, out := range record.DomainOutputs {
		if out.PackProposal.Check == nil || out.PackProposal.Check.DocumentDigest != digestOf([]byte(out.PackProposal.Text)) {
			t.Fatal("exact findings missing")
		}
	}
	assistantRunMust(t, validateAssistantDomainOutputs(record))
	assistantSavedPackFinish(t, s, e)
	calls := len(assistantExecutionRuntimeLog(t, s))
	assistantRunMust(t, e.ProjectChat(t.Context()))
	if len(assistantExecutionRuntimeLog(t, s)) != calls || step.Load() != 3 {
		t.Fatal("reload replayed Runtime or model")
	}
	record = assistantExecutionRecord(t, e)
	assistantRunClientJSON(t, "saved-pack-snapshot.json", assistantRunPublic(record))
	raw, _ := json.Marshal(assistantRunLedger{Version: 1, Project: record.Binding.Project, Runs: []assistantRunRecord{record}})
	_, err := decodeAssistantRunStorage(e.writer.lease.store.name, raw)
	assistantRunMust(t, err)
	suite := assistantTestHostSuite(t, s, token, "one")
	if !bytes.Equal(before["cases"], suite["cases"]) {
		t.Fatal("saved expectations changed")
	}
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	if !bytes.Contains(saved.Content, []byte(`"packReview"`)) || !bytes.Contains(saved.Content, []byte(`"suiteKey"`)) {
		t.Fatal("saved proposal reader shape missing")
	}
	if path := os.Getenv("DESK_SAVED_PACK_READER_OUTPUT"); path != "" {
		assistantRunMust(t, os.WriteFile(path, saved.Content, 0600))
	}
	if path := os.Getenv("DESK_SAVED_PACK_SUITE_OUTPUT"); path != "" {
		saved := policyWorkspaceRead(t, s, token, "/api/pack-tests")
		assistantRunMust(t, os.WriteFile(path, saved.Content, 0600))
	}
	pack, err := os.ReadFile(filepath.Join(s.cfg.ProjectDir, "one.json"))
	assistantRunMust(t, err)
	if string(pack) != assistantTestPack {
		t.Fatal("proposal wrote the file")
	}
}
func TestAssistantSavedPackActualNative(t *testing.T) {
	s, _, e, trace := assistantTestHostFixtureCapture(t, "https://example.invalid", "work", false, true, &assistantPackBasis{"one", "one.json", assistantSavedPackEditor})
	args, _ := json.Marshal(map[string]string{"document": assistantSavedPackCandidate})
	peer := strings.Replace(assistantExecutionCodexPeer, "if mode=='delegate' and index==1:\n   tool(offered('delegate_tasks'),{'tasks':[{'title':'Inspect schema','objective':'Inspect the schema only.','profile':'review'}]})", `if mode=='delegate' and index==1: tool(offered('propose_saved_pack'),json.loads(`+fmt.Sprintf("%q", string(args))+`),call='same-provider-call')`, 1)
	assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
	x := assistantSavedPackOpen(t, s, e)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	record := assistantExecutionRecord(t, e)
	if len(record.DomainOutputs) != 1 || record.DomainOutputs[0].PackProposal.Check == nil {
		t.Fatal("native proposal not checked", e.writer.Err())
	}
	assistantRunMust(t, validateAssistantDomainOutputs(record))
	assistantSavedPackFinish(t, s, e)
}
func TestAssistantSavedPackRepeatAndModelFreeCheck(t *testing.T) {
	s, _, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", false, false)
	x := assistantSavedPackOpen(t, s, e)
	first := assistantTestHostInvoke(t, e, x, "first", assistantSavedPackTool, map[string]string{"document": assistantSavedPackCandidate})
	if first.Err() {
		t.Fatal(first.Value().Text())
	}
	calls := len(assistantExecutionRuntimeLog(t, s))
	second := assistantTestHostInvoke(t, e, x, "repeat", assistantSavedPackTool, map[string]string{"document": assistantSavedPackCandidate})
	if second.Err() || !strings.Contains(second.Value().Text(), "stalled") || len(assistantExecutionRuntimeLog(t, s)) != calls {
		t.Fatal("repeat replayed checks or lost original results")
	}
	record := assistantExecutionRecord(t, e)
	held := record.SavedPack
	var retained []bool
	check, err := e.checkAssistantSavedPack(t.Context(), x.runtime, held, assistantSavedPackCandidate, func(_ context.Context, c *assistantPackCheck) error {
		retained = append(retained, c != nil)
		return nil
	})
	assistantRunMust(t, err)
	if check.Report == nil || len(retained) != 2 || retained[0] || !retained[1] {
		t.Fatal("model-free recheck did not retain candidate then findings")
	}
	for _, op := range assistantExecutionRecord(t, e).Operations {
		if op.Kind == "model" {
			t.Fatal("model-free proposal/recheck invoked a model")
		}
	}
}
func TestAssistantSavedPackRefusalsRetainCandidate(t *testing.T) {
	for _, mode := range []string{"incomplete", "foreign-id", "wrong-summary", "runtime-failure", "changed-suite"} {
		t.Run(mode, func(t *testing.T) {
			s, _, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", false, false)
			x := assistantSavedPackOpen(t, s, e)
			assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "saved-pack-mode"), []byte(mode), 0600))
			if mode == "changed-suite" {
				store, err := s.openChatData()
				assistantRunMust(t, err)
				name, err := resolveConversationName(store.root, s.conversationIdentity())
				assistantRunMust(t, err)
				path := filepath.Join(store.root.Name(), strings.Replace(name, "conversations-", "pack-tests-", 1))
				store.root.Close()
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "saved-suite-path"), []byte(path), 0600))
			}
			result := assistantTestHostInvoke(t, e, x, "proposal", assistantSavedPackTool, map[string]string{"document": assistantSavedPackCandidate})
			record := assistantExecutionRecord(t, e)
			if !result.Err() || len(record.DomainOutputs) != 1 || record.DomainOutputs[0].PackProposal.Text != assistantSavedPackCandidate || record.DomainOutputs[0].PackProposal.Check != nil {
				t.Fatal("incomplete check lost candidate or claimed readiness", result.Value().Text())
			}
			if strings.Contains(result.Value().Text(), "internal error details") {
				t.Fatal("Runtime internal prose escaped")
			}
			assistantRunMust(t, validateAssistantDomainOutputs(record))
		})
	}
}
func TestAssistantSavedPackTypedMatrix(t *testing.T) {
	extra := strings.Replace(assistantTestCase, `"expectedDisposition":`+assistantCriticDisposition, `"expectedErrorClass":"type-error","expectedErrorPhase":"evaluation","supportedExtensions":["example"]`, 1)
	extra = strings.Replace(extra, `"existing"`, `"error"`, -1)
	unasserted := strings.Replace(assistantTestCase, `,"expectedDisposition":`+assistantCriticDisposition, "", 1)
	unasserted = strings.Replace(unasserted, `"existing"`, `"unasserted"`, -1)
	suite := `{"revision":1,"cases":[` + assistantTestCase + `,` + extra + `,` + unasserted + `],"runs":[],"recovered":[]}`
	raw, ids, err := assistantSavedPackMatrix([]byte(suite))
	assistantRunMust(t, err)
	if len(ids) != 2 || ids[0] != "existing" || ids[1] != "error" {
		t.Fatal("executable matrix omitted expected error or included unasserted row")
	}
	for _, exact := range []string{`1.20`, `"amount":"4"`, `"flag":false`, `"zero":0`, `"null":null`, `"expectedErrorClass":"type-error"`, `"expectedErrorPhase":"evaluation"`, `"supportedExtensions":["example"]`} {
		if !bytes.Contains(raw, []byte(exact)) {
			t.Fatal("typed saved row changed", exact, string(raw))
		}
	}
	if bytes.Contains(raw, []byte(`"missing"`)) {
		t.Fatal("missing value fabricated")
	}
}
func TestAssistantSavedPackPlanAndMalformed(t *testing.T) {
	for _, raw := range []string{`{}`, `{"document":null}`, `{"document":"[]"}`, `{"document":"{}","path":"other.json"}`, `{"Document":"{}"}`, `{"document":"{}","document":"{}"}`} {
		if _, err := readAssistantSavedPackArguments([]byte(raw)); err == nil {
			t.Fatal("malformed proposal accepted", raw)
		}
	}
	s, _, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "plan", false, false)
	x := assistantSavedPackOpen(t, s, e)
	for _, tool := range x.Run.HostTools {
		if tool.Name == assistantSavedPackTool {
			t.Fatal("Plan offered proposal")
		}
	}
}

// Standalone rechecks use the same snapshot tool contract, while this peer
// independently observes the durable user-owned check receipt before answering.
func assistantSavedPackRecheckPeer(t *testing.T, s *Server, e *assistantRunExecution) string {
	t.Helper()
	store, err := s.openChatData()
	assistantRunMust(t, err)
	ledger := filepath.Join(store.root.Name(), e.writer.lease.store.name)
	store.root.Close()
	bin := filepath.Join(t.TempDir(), "snapshot-runtime")
	peer := `#!/usr/bin/env python3
import json,sys
ledger_path=` + fmt.Sprintf("%q", ledger) + `
for line in sys.stdin:
 q=json.loads(line)
 if 'id' not in q: continue
 if q['method']=='initialize': result={'protocolVersion':'2025-06-18','capabilities':{},'serverInfo':{'name':'snapshot','version':'1'}}
 else:
  with open(ledger_path) as f: ledger=json.load(f)
  checks=[c for run in ledger['runs'] for c in run.get('packChecks',[]) if c.get('check') is None]
  if not checks: sys.exit(71)
  p=q['params'];name=p['name']
  if name=='validate': report={'status':'valid','diagnostics':[]}
  else:
   if not checks[-1].get('validation'): sys.exit(72)
   rows=json.loads(p['arguments']['matrix'])['cases']
   report={'status':'mismatch','summary':{'total':len(rows),'passed':0},'packs':[{'rows':[{'id':r['id'],'expected':'authored','actual':'observed','status':'mismatch'} for r in rows]}]}
  result={'content':[],'structuredContent':report}
 print(json.dumps({'jsonrpc':'2.0','id':q['id'],'result':result}),flush=True)
`
	assistantRunMust(t, os.WriteFile(bin, []byte(peer), 0700))
	return bin
}
func TestAssistantSavedPackExplicitRecheckHTTP(t *testing.T) {
	s, token, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", false, false)
	x := assistantSavedPackOpen(t, s, e)
	result := assistantTestHostInvoke(t, e, x, "proposal", assistantSavedPackTool, map[string]string{"document": assistantSavedPackCandidate})
	if result.Err() {
		t.Fatal(result.Value().Text())
	}
	assistantSavedPackFinish(t, s, e)
	before := assistantExecutionRecord(t, e)
	original, _ := json.Marshal(before.Checkpoint)
	s.cfg.JpackBin = assistantSavedPackRecheckPeer(t, s, e)
	suite, _ := assistantTestSuite(before.SavedPack.Read.Suite)
	request := assistantPackRecheckRequest{ID: "explicit-check", DocumentDigest: digestOf([]byte(assistantSavedPackCandidate)), SuiteDigest: digestOf([]byte(assistantTestIdentity(suite["cases"])))}
	path := "/api/assistant-runs/" + e.record.ID + "/pack-checks"
	reply := policyCall(s, token, "POST", path, request)
	if reply.Code != 200 {
		t.Fatal(reply.Code, reply.Body.String())
	}
	var checked assistantPackCheck
	assistantRunMust(t, json.Unmarshal(reply.Body.Bytes(), &checked))
	if checked.DocumentDigest != request.DocumentDigest || checked.TotalCases != 1 || checked.Report == nil {
		t.Fatal("explicit findings missing")
	}
	after := assistantExecutionRecord(t, e)
	checkpoint, _ := json.Marshal(after.Checkpoint)
	if !bytes.Equal(original, checkpoint) || len(before.Operations) != len(after.Operations) || len(after.PackChecks) != 1 || after.PackChecks[0].Check == nil {
		t.Fatal("recheck took execution ownership or failed retention")
	}
	assistantRunMust(t, validateAssistantDomainOutputs(after))
	// Identical delivery only observes the retained result. A broken binary
	// proves it cannot spawn either Runtime or model work on redelivery.
	s.cfg.JpackBin = filepath.Join(t.TempDir(), "must-not-run")
	replay := policyCall(s, token, "POST", path, request)
	if replay.Code != 200 || !bytes.Equal(reply.Body.Bytes(), replay.Body.Bytes()) {
		t.Fatal("recheck receipt replayed or changed", replay.Body.String())
	}
	// Background check results do not rewrite response recovery in the chat.
	current := policyWorkspaceRead(t, s, token, "/api/conversations")
	if !bytes.Contains(current.Content, []byte(`"packReview"`)) {
		t.Fatal("proposal disappeared")
	}
	if path := os.Getenv("DESK_SAVED_PACK_RECHECK_OUTPUT"); path != "" {
		assistantRunMust(t, os.WriteFile(path, reply.Body.Bytes(), 0600))
	}
	if path := os.Getenv("DESK_SAVED_PACK_RECHECK_REFERENCE"); path != "" {
		reference := map[string]any{"id": before.ID, "owner": before.Binding.Owner, "requestId": before.Binding.RequestID, "request": request}
		assistantRunMust(t, os.WriteFile(path, assistantChatJSON(reference), 0600))
	}
	for _, change := range []string{"candidate", "suite", "owner", "source", "session"} {
		t.Run(change, func(t *testing.T) {
			// Each refusal begins from exact observed saved state, preserving the
			// parent fixture's original proposal and identity after this bounded probe.
			var doc conversationDocument
			assistantRunMust(t, json.Unmarshal(current.Content, &doc))
			originalChat := bytes.Clone(doc.Chats[0])
			chat, _ := assistantTestObject(originalChat)
			changedRequest := request
			changedRequest.ID = "refuse-" + change
			originalPack, err := os.ReadFile(filepath.Join(s.cfg.ProjectDir, "one.json"))
			assistantRunMust(t, err)
			tests := policyWorkspaceRead(t, s, token, "/api/pack-tests")
			switch change {
			case "candidate":
				cp, _ := assistantTestObject(chat["checkpoint"])
				state, _ := assistantTestObject(cp["state"])
				candidates, _ := assistantChatArray(state["candidates"])
				candidate, _ := assistantTestObject(candidates[len(candidates)-1])
				candidate["text"] = assistantChatJSON(assistantSavedPackCorrected)
				candidates[len(candidates)-1] = assistantChatEncodeObject(candidate)
				state["candidates"] = assistantChatEncodeArray(candidates)
				cp["state"] = assistantChatEncodeObject(state)
				chat["checkpoint"] = assistantChatEncodeObject(cp)
			case "owner":
				chat["pack"] = assistantChatJSON(map[string]string{"id": "other", "path": "one.json"})
			case "suite":
				var store map[string]json.RawMessage
				assistantRunMust(t, json.Unmarshal(tests.Content, &store))
				suites, _ := assistantTestObject(store["suites"])
				suite, _ := assistantTestSuite(suites["one"])
				cases, _ := assistantChatArray(suite["cases"])
				c, _ := assistantTestObject(cases[0])
				c["rationale"] = assistantChatJSON("Changed rationale")
				cases[0] = assistantChatEncodeObject(c)
				suite["cases"] = assistantChatEncodeArray(cases)
				suites["one"] = assistantChatEncodeObject(suite)
				store["suites"] = assistantChatEncodeObject(suites)
				policyWorkspaceSave(t, s, token, "/api/pack-tests", json.RawMessage(assistantChatEncodeObject(store)))
			case "source":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte(assistantSavedPackEditor), 0600))
			}
			if change == "candidate" || change == "owner" {
				doc.Chats[0] = assistantChatEncodeObject(chat)
				policyWorkspaceSave(t, s, token, "/api/conversations", doc)
			}
			auth := token
			if change == "session" {
				auth = "not-the-signed-in-session"
			}
			refused := policyCall(s, auth, "POST", path, changedRequest)
			if refused.Code == 200 {
				t.Fatal("changed identity rechecked", change)
			}
			assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), originalPack, 0600))
			policyWorkspaceSave(t, s, token, "/api/pack-tests", json.RawMessage(tests.Content))
			doc.Chats[0] = originalChat
			policyWorkspaceSave(t, s, token, "/api/conversations", doc)
		})
	}
}
func TestAssistantSavedPackExplanation(t *testing.T) {
	var calls atomic.Int32
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assistantPolicySSE(w, "", nil, "The current saved expectation needs policy review.")
	})
	s, _, e, _ := assistantTestHostFixture(t, u.server.URL, "work", false, false)
	x := assistantSavedPackOpen(t, s, e)
	assistantRunMust(t, x.Adapter.Run(t.Context(), x.Run))
	record := assistantExecutionRecord(t, e)
	if calls.Load() != 1 || len(record.DomainOutputs) != 0 {
		t.Fatal("explanation fabricated a proposal")
	}
	for _, op := range record.Operations {
		if op.Name == "tools/call" {
			var frame struct {
				Params struct {
					Name string `json:"name"`
				} `json:"params"`
			}
			json.Unmarshal(op.Intent, &frame)
			if frame.Params.Name == "validate" || frame.Params.Name == "experimental_test_cases" {
				t.Fatal("explanation ran proposal checks")
			}
		}
	}
}
func TestAssistantSavedPackOriginalBasisOnReconstruction(t *testing.T) {
	s, _, e, _ := assistantTestHostFixtureCapture(t, "http://127.0.0.1:1", "work", false, false, &assistantPackBasis{"one", "one.json", assistantSavedPackEditor})
	x := assistantSavedPackOpen(t, s, e)
	result := assistantTestHostInvoke(t, e, x, "proposal", assistantSavedPackTool, map[string]string{"document": assistantSavedPackCandidate})
	if result.Err() {
		t.Fatal(result.Value().Text())
	}
	before := assistantExecutionRecord(t, e)
	// The same admitted execution can rebuild context after a question/budget
	// continuation; capture remains the original request, never the candidate.
	chat, err := e.prepareAssistantChatInput(t.Context())
	assistantRunMust(t, err)
	if len(chat.Documents) == 0 || assistantExecutionRecord(t, e).SavedPack.Basis != before.SavedPack.Basis {
		t.Fatal("reconstruction changed original basis")
	}
	raw, _ := json.Marshal(assistantRunLedger{Version: 1, Project: before.Binding.Project, Runs: []assistantRunRecord{before}})
	decoded, err := decodeAssistantRunStorage(e.writer.lease.store.name, raw)
	assistantRunMust(t, err)
	if decoded.Runs[0].SavedPack.Basis.Text != assistantSavedPackEditor || decoded.Runs[0].DomainOutputs[0].PackProposal.Check == nil {
		t.Fatal("reload lost original basis or findings")
	}
	// A changed source cannot be silently captured as a new basis on resume.
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte(assistantSavedPackCorrected), 0600))
	if _, err = e.prepareAssistantChatInput(t.Context()); err == nil {
		t.Fatal("resume recaptured changed source")
	}
}

func TestAssistantSavedPackActualBudgetContinuation(t *testing.T) {
	var calls atomic.Int32
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			assistantPolicySSE(w, assistantSavedPackTool, map[string]string{"document": assistantSavedPackCandidate}, "")
		case 2:
			// The canonical final request closes dispatch. A provider that still
			// requests a tool pauses without replaying the completed proposal.
			assistantPolicySSE(w, "read_pack_tests", map[string]string{}, "")
		case 3:
			assistantPolicySSE(w, assistantSavedPackTool, map[string]string{"document": assistantSavedPackCorrected}, "")
		default:
			assistantPolicySSE(w, "", nil, "Review the corrected proposal.")
		}
	})
	proceed := make(chan struct{})
	s, token, e, _ := assistantTestHostFixtureExecuteCapture(t, u.server.URL, "work", false, false, &assistantPackBasis{"one", "one.json", assistantSavedPackEditor}, func(ctx context.Context, e *assistantRunExecution) error {
		<-proceed
		x := assistantSavedPackOpen(t, e.service.server, e)
		defer x.Close()
		if len(e.record.Actions) == 0 {
			cp := *e.record.Checkpoint
			cp.Used = cp.Limit - 2
			assistantRunMust(t, e.Checkpoint(ctx, cp))
			x.Run.Context.Execution = &cp
		}
		return x.Adapter.Run(ctx, x.Run)
	})
	close(proceed)
	paused := assistantProtocolWait(t, s, token, e.record.ID, "paused-budget")
	before := assistantExecutionRecord(t, e)
	if calls.Load() != 2 || len(before.DomainOutputs) != 1 || before.DomainOutputs[0].PackProposal.Check == nil {
		t.Fatal("budget pause lost completed candidate/check", calls.Load(), before.DomainOutputs)
	}
	action := assistantContinuationAction(paused, "extend-budget")
	assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", action), 200)
	complete := assistantProtocolWait(t, s, token, e.record.ID, "complete")
	after := assistantExecutionRecord(t, e)
	if calls.Load() != 4 || complete.Checkpoint.Used != paused.Checkpoint.Used+2 || after.SavedPack.Basis != before.SavedPack.Basis || len(after.DomainOutputs) != 2 {
		t.Fatal("continuation reset basis, allowance or findings", calls.Load())
	}
	for _, held := range before.Operations {
		found := false
		for _, current := range after.Operations {
			if held.ID == current.ID {
				found = bytes.Equal(held.Intent, current.Intent) && bytes.Equal(held.Result, current.Result) && held.State == current.State
			}
		}
		if !found {
			t.Fatal("continuation replayed or changed receipt", held.ID)
		}
	}
	assistantRunMust(t, validateAssistantDomainOutputs(after))
}
func TestAssistantSavedPackCancellationRetainsFindings(t *testing.T) {
	s, _, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", false, false)
	x := assistantSavedPackOpen(t, s, e)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	testHookAssistantSavedPackRetained = cancel
	t.Cleanup(func() { testHookAssistantSavedPackRetained = nil })
	raw := assistantChatJSON(map[string]string{"document": assistantSavedPackCandidate})
	assistantPolicyEvent(t, e, "cancel-after-check", assistantSavedPackTool, raw)
	ctx = context.WithValue(runcontext.WithCallID(ctx, "cancel-after-check"), assistantHostRootKey{}, true)
	result, err := assistantGraphFind(t, x.Run.HostTools, assistantSavedPackTool).Execute(ctx, raw)
	assistantRunMust(t, err)
	record := assistantExecutionRecord(t, e)
	if !result.Err() || len(record.DomainOutputs) != 1 || record.DomainOutputs[0].PackProposal.Check == nil {
		t.Fatal("cancelled return lost completed findings")
	}
	assistantRunMust(t, validateAssistantDomainOutputs(record))
}

// Hand-authored pack and expectations for the pinned released Runtime. This
// checks the actual snapshot contract, independently of the scripted host peer.
const assistantSavedPackReleasedDocument = `{"specVersion":"0.2.0-draft","id":"https://example.com/pack/saved-proposal","version":"1.0.0","title":"Saved proposal test","decision":{"intent":"Select the literal accept rule.","question":"Does the literal rule accept?"},"outcomes":[{"id":"accept","label":"Accept"},{"id":"reject","label":"Reject"}],"rules":[{"id":"literal","description":"Always accept.","when":{"op":"literal","value":true},"outcome":"accept","onUnknown":"ignore"}]}`

func TestAssistantSavedPackReleasedRuntime(t *testing.T) {
	bin := os.Getenv("DESK_DIALECT_MAPPING_RUNTIME")
	if bin == "" {
		t.Skip("pinned released Runtime fixture not supplied")
	}
	s := &Server{cfg: Config{JpackBin: bin}}
	row := `{"id":"typed","facts":{"decimal":{"value":"1.20"},"number":{"value":1.20},"zero":{"value":0},"false":{"value":false},"null":{"value":null}},"expectedDisposition":{"kind":"outcome","outcomeId":"accept","reasons":[],"handoff":{"state":"none"}}}`
	// The saved error expectation is independent review data. This literal
	// policy does not inspect facts; released Runtime produces accept even for
	// this scalar carrier. The host must retain that mismatch, never edit it.
	negative := `{"id":"error","facts":42,"expectedErrorClass":"malformed-input"}`
	cases := []json.RawMessage{}
	for _, raw := range []string{row, negative} {
		fields, _ := assistantTestObject([]byte(raw))
		cases = append(cases, assistantChatJSON(map[string]any{"id": json.RawMessage(fields["id"]), "name": "Independently authored", "revision": 1, "row": json.RawMessage(raw), "sources": []any{}, "sourceMappings": map[string]any{}, "origin": "manual", "rationale": "Literal rule accepts valid facts; malformed input is an expected error."}))
	}
	read := assistantTestRead{Suite: assistantChatJSON(map[string]any{"revision": 1, "cases": cases, "runs": []any{}, "recovered": []any{}})}
	retained := 0
	check, err := checkAssistantSavedPack(t.Context(), read, assistantSavedPackReleasedDocument, assistantSavedPackCheckPorts{Fresh: func(context.Context) error { return nil }, Retain: func(_ context.Context, c *assistantPackCheck) error { retained++; return nil }, Call: s.callAssistantPackSnapshot})
	assistantRunMust(t, err)
	if !check.Valid || assistantSavedPackPassed(check) || check.TotalCases != 2 || retained != 2 {
		t.Fatal("released Runtime expected-error mismatch lost", string(check.Report))
	}
	report, _ := assistantTestObject(check.Report)
	packs, _ := assistantChatArray(report["packs"])
	pack, _ := assistantTestObject(packs[0])
	rows, _ := assistantChatArray(pack["rows"])
	typed, _ := assistantTestObject(rows[0])
	errorRow, _ := assistantTestObject(rows[1])
	if string(typed["status"]) != `"passed"` || string(errorRow["status"]) != `"mismatch"` || string(errorRow["expectedErrorClass"]) != `"malformed-input"` {
		t.Fatal("released Runtime report or saved expectation changed", string(check.Report))
	}
	// A separate independently authored passing suite exercises readiness,
	// without dropping or repairing the error expectation in the original suite.
	passingRead := read
	passingRead.Suite = assistantChatJSON(map[string]any{"revision": 1, "cases": cases[:1], "runs": []any{}, "recovered": []any{}})
	passed, err := checkAssistantSavedPack(t.Context(), passingRead, assistantSavedPackReleasedDocument, assistantSavedPackCheckPorts{Fresh: func(context.Context) error { return nil }, Retain: func(context.Context, *assistantPackCheck) error { return nil }, Call: s.callAssistantPackSnapshot})
	assistantRunMust(t, err)
	if !assistantSavedPackPassed(passed) || passed.TotalCases != 1 {
		t.Fatal("released Runtime known passing typed row did not pass", string(passed.Report))
	}
}

func TestAssistantSavedPackStoredCustodyMutations(t *testing.T) {
	s, _, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", false, false)
	x := assistantSavedPackOpen(t, s, e)
	result := assistantTestHostInvoke(t, e, x, "proposal", assistantSavedPackTool, map[string]string{"document": assistantSavedPackCandidate})
	if result.Err() {
		t.Fatal(result.Value().Text())
	}
	original := assistantExecutionRecord(t, e)
	mutations := map[string]func(*assistantRunRecord){
		"source-digest":        func(r *assistantRunRecord) { r.SavedPack.SourceDigest = strings.Repeat("f", 64) },
		"request-digest":       func(r *assistantRunRecord) { r.SavedPack.RequestDigest = strings.Repeat("f", 64) },
		"basis-pack":           func(r *assistantRunRecord) { r.SavedPack.Basis.PackID = "other" },
		"basis-path":           func(r *assistantRunRecord) { r.SavedPack.Basis.Path = "other.json" },
		"candidate-bytes":      func(r *assistantRunRecord) { r.DomainOutputs[0].PackProposal.Text = assistantSavedPackCorrected },
		"validation-operation": func(r *assistantRunRecord) { r.DomainOutputs[0].PackProposal.ValidationID = "foreign" },
		"test-operation":       func(r *assistantRunRecord) { r.DomainOutputs[0].PackProposal.TestID = "foreign" },
		"check-digest": func(r *assistantRunRecord) {
			r.DomainOutputs[0].PackProposal.Check.DocumentDigest = strings.Repeat("f", 64)
		},
		"check-suite":    func(r *assistantRunRecord) { r.DomainOutputs[0].PackProposal.Check.SuiteKey = "[]" },
		"check-date":     func(r *assistantRunRecord) { r.DomainOutputs[0].PackProposal.Check.At = "not-a-date" },
		"check-validity": func(r *assistantRunRecord) { r.DomainOutputs[0].PackProposal.Check.Valid = false },
		"check-diagnostics": func(r *assistantRunRecord) {
			r.DomainOutputs[0].PackProposal.Check.Diagnostics = []json.RawMessage{json.RawMessage(`{"code":"invented"}`)}
		},
		"check-total":  func(r *assistantRunRecord) { r.DomainOutputs[0].PackProposal.Check.TotalCases++ },
		"check-report": func(r *assistantRunRecord) { r.DomainOutputs[0].PackProposal.Check.Report = nil },
		"kind":         func(r *assistantRunRecord) { r.DomainOutputs[0].Kind = "graph" },
		"root-only": func(r *assistantRunRecord) {
			for i := range r.Operations {
				if r.Operations[i].ID == r.DomainOutputs[0].OperationID {
					var in assistantHostIntent
					json.Unmarshal(r.Operations[i].Intent, &in)
					in.Root = false
					r.Operations[i].Intent = assistantChatJSON(in)
				}
			}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(original)
			var changed assistantRunRecord
			assistantRunMust(t, json.Unmarshal(raw, &changed))
			mutate(&changed)
			if validateAssistantDomainOutputs(changed) == nil {
				t.Fatal("mutated saved-pack custody accepted", name)
			}
		})
	}
	// Required member omissions and explicit nulls refuse in the raw reader;
	// optional pending checks remain supported for interrupted operations.
	for _, shape := range []any{assistantSavedPackBasis{}, assistantPackBasis{}, assistantPackCheck{}, assistantSavedPackProposal{}, assistantPackRecheck{}} {
		var raw []byte
		switch shape.(type) {
		case assistantSavedPackBasis:
			raw = assistantChatJSON(original.SavedPack)
		case assistantPackBasis:
			raw = assistantChatJSON(original.SavedPack.Basis)
		case assistantPackCheck:
			raw = assistantChatJSON(original.DomainOutputs[0].PackProposal.Check)
		case assistantSavedPackProposal:
			raw = assistantChatJSON(original.DomainOutputs[0].PackProposal)
		case assistantPackRecheck:
			raw = assistantChatJSON(assistantPackRecheck{ID: "held", OperationID: original.DomainOutputs[0].OperationID, DocumentDigest: digestOf([]byte(assistantSavedPackCandidate)), Read: original.SavedPack.Read})
		}
		fields, err := assistantTestObject(raw)
		assistantRunMust(t, err)
		typed := reflect.TypeOf(shape)
		for key, value := range fields {
			fields[key] = []byte("null")
			if readAssistantDialectCustody(assistantChatEncodeObject(fields), typed) == nil {
				t.Fatal("null custody member accepted", typed.Name(), key)
			}
			fields[key] = value
		}
		for i := 0; i < typed.NumField(); i++ {
			field := typed.Field(i)
			key, opts, _ := strings.Cut(field.Tag.Get("json"), ",")
			if strings.Contains(opts, "omit") {
				continue
			}
			value := fields[key]
			delete(fields, key)
			if readAssistantDialectCustody(assistantChatEncodeObject(fields), typed) == nil {
				t.Fatal("missing required custody member accepted", typed.Name(), key)
			}
			fields[key] = value
		}
	}
}

func TestAssistantSavedPackCaptureAliasAndMismatch(t *testing.T) {
	read := assistantTestRead{Owner: "saved-alias", Path: "packs/current.json", Pack: []byte(assistantSavedPackReleasedDocument)}
	source := assistantRunSource{Original: []byte(`{"pack":{"id":"saved-alias","path":"packs/current.json"}}`), Request: []byte(`{"packContext":{"packId":"different-alias","path":"other.json","text":"{\"id\":\"untrusted-target\"}"}}`)}
	basis, err := assistantSavedPackCapture(source, read)
	assistantRunMust(t, err)
	if basis.PackID != read.Owner || basis.Path != read.Path || basis.Text != string(read.Pack) {
		t.Fatal("document URI or mismatched browser capture selected target")
	}
	source.Request = assistantChatJSON(map[string]any{"packContext": assistantPackBasis{PackID: read.Owner, Path: read.Path, Text: assistantSavedPackEditor}})
	basis, err = assistantSavedPackCapture(source, read)
	assistantRunMust(t, err)
	if basis.Text != assistantSavedPackEditor || basis.PackID != read.Owner || basis.Path != read.Path {
		t.Fatal("matching editor capture lost exact comparison basis")
	}
}
