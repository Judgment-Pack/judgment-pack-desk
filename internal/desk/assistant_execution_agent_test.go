package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// Hand-authored protocol peer, launched by the actual private Manager with its
// real profile, process group, discovery, admission, callback and reap paths.
// Every new process refuses to overlap the prior process (including a zombie).
const assistantExecutionCodexPeer = `#!/usr/bin/env python3
import base64,json,os,sys,time
mode,trace,ledger=sys.argv[1:4]
request={}; index=0
pidfile=trace+'.pid'
if os.path.exists(pidfile):
 with open(pidfile) as f: prior=int(f.read())
 try: os.kill(prior,0)
 except ProcessLookupError: pass
 else: sys.exit(71)
with open(pidfile,'w') as f: f.write(str(os.getpid()))
def write(v): print(json.dumps(v,separators=(',',':')),flush=True)
def log(v):
 with open(trace,'a') as f: f.write(json.dumps(v)+'\n')
def note(method,p): write({'method':method,'params':p})
def finish():
 text='Fixture answer.'
 if mode=='delegate' and index==2:
  text='Child findings.\n\n'+'` + "```" + `json\n{"report":{"summary":"Child findings.","findings":[],"citations":[],"openQuestions":[],"confidence":"high","notDone":[]}}\n'+'` + "```" + `'
 note('item/completed',{'threadId':'thread','turnId':'turn','item':{'id':'message','type':'agentMessage','phase':'final_answer','text':text}})
 note('turn/completed',{'threadId':'thread','turn':{'id':'turn','status':'completed'}})
def tool(name,args,call='call',rpc='rpc'):
 write({'id':rpc,'method':'item/tool/call','params':{'threadId':'thread','turnId':'turn','callId':call,'tool':name,'arguments':args}})
def offered(name):
 return next((t['name'] for t in request['dynamicTools'] if t['description'].startswith(name+':')),'unoffered')
for line in sys.stdin:
 q=json.loads(line); method=q.get('method',''); p=q.get('params',{})
 def reply(v): write({'id':q['id'],'result':v})
 if method=='initialize': reply({'userAgent':'jps_desk/0.157.1 (Linux)'})
 elif method=='initialized': pass
 elif method=='account/read':
  reply({'requiresOpenaiAuth':True,'account':{'type':'chatgpt','planType':'plus'}})
 elif method=='model/list':
  if p.get('includeHidden'):
   with open(os.path.join(os.environ['CODEX_HOME'],'model-catalog.json')) as f: catalog=json.load(f)
   reply({'data':[{'model':m['slug']} for m in catalog['models']]})
  else:
   if mode=='discovery-wait':
    with open(trace+'.waiting','w') as f: f.write('ready')
    while not os.path.exists(trace+'.release'): time.sleep(.005)
   reply({'data':[{'model':m,'displayName':m,'defaultReasoningEffort':'high','supportedReasoningEfforts':[{'reasoningEffort':e} for e in ['low','medium','high']]} for m in ['shared-model','child-model']]})
 elif method=='thread/start':
  request=p
  if p['approvalPolicy']!='never' or p['allowProviderModelFallback']!=False or p['permissions']!='jps' or p['environments']!=[] or p['runtimeWorkspaceRoots']!=[]: sys.exit(72)
  log({'type':'thread','request':p})
  reply({'thread':{'id':'thread'},'model':p['model'],'modelProvider':'openai','approvalPolicy':'never','cwd':os.getcwd()})
 elif method=='turn/start':
  with open(ledger) as f: runs=json.load(f)['runs']
  prompt=p['input'][0]['text']
  ops=[op for r in runs for op in (r.get('operations') or []) if op['kind']=='model' and json.loads(base64.b64decode(op['intent']))['prompt']==prompt]
  if not ops or ops[-1]['state']!='dispatched': sys.exit(73)
  old=[]
  if os.path.exists(trace):
   with open(trace) as f: old=[json.loads(x) for x in f]
  index=1+len([x for x in old if x['type']=='generation'])
  log({'type':'generation','request':request,'turn':p,'prompt':prompt,'operation':ops[-1]['id']})
  reply({'turn':{'id':'turn'}})
  if mode in ['hang','discovery-wait']: continue
  if mode=='callback-wait':
   with open(trace+'.waiting','w') as f: f.write('ready')
   while not os.path.exists(trace+'.release'): time.sleep(.005)
  if mode=='delegate' and index==1:
   tool(offered('delegate_tasks'),{'tasks':[{'title':'Inspect schema','objective':'Inspect the schema only.','profile':'review'}]})
  elif mode=='budget': tool(offered('request_budget_increase'),{'reason':'More work','nextStep':'Read the schema'})
  elif mode=='continuation-question' and index>1:
   history=prompt.split("PERSON'S RETAINED QUESTION ANSWER (user history; preserve the original task and constraints; no tool approval or compute grant)\n",1)[1]
   answer=json.JSONDecoder().raw_decode(history)[0]['answer']
   if answer['selected']!=['two'] or answer['text']!='Exact answer <&> 한글\nnext line' or 'Review exact input' not in prompt: sys.exit(75)
   finish()
  elif mode in ['question','continuation-question']: tool(offered('ask_user'),{'title':'Scope','question':'Which scope?','selection':'single','options':[{'label':'One','id':'one'},{'label':'Two','id':'two'}]})
  elif mode=='unoffered': tool('desk_unoffered',{})
  elif mode=='malformed': tool(offered('get_schema'),[])
  elif mode=='duplicate': tool(offered('get_schema'),{})
  elif mode=='delegate' and index in [2,3]: finish()
  else: tool(offered('get_schema'),{})
 elif not method:
  log({'type':'answer','answer':q})
  if mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')
  else: finish()
 else: sys.exit(74)
`

type assistantCodexExecutor struct{ *assistantProtocolExecutor }

func (x *assistantCodexExecutor) Prepare(ctx context.Context, source assistantRunSource) (contract.Checkpoint, error) {
	return assistantFixtureCheckpoint(ctx, source, x.review)
}

func assistantCodexFixture(t *testing.T, mode, phase string) (*Server, string, *assistantRunExecution, *codexbridge.Manager, string) {
	t.Helper()
	return assistantCodexProfileFixture(t, mode, phase, testAIAlpha, `{"profileVersion":1,"codex":{"inherit":true},"projectInstructions":"HOST-INSTRUCTIONS-ONCE","context":{"version":1,"projectBrief":"execution-brief.md"}}`)
}

func assistantCodexProfileFixture(t *testing.T, mode, phase, connectionID, profile string) (*Server, string, *assistantRunExecution, *codexbridge.Manager, string) {
	return assistantCodexSourceFixture(t, mode, phase, connectionID, profile, nil)
}
func assistantCodexSourceFixture(t *testing.T, mode, phase, connectionID, profile string, prepare func(*Server, string, assistantRunStart) assistantRunStart) (*Server, string, *assistantRunExecution, *codexbridge.Manager, string) {
	return assistantCodexSourceFixtureExecute(t, mode, phase, connectionID, profile, prepare, nil)
}
func assistantCodexSourceFixtureExecute(t *testing.T, mode, phase, connectionID, profile string, prepare func(*Server, string, assistantRunStart) assistantRunStart, execute func(context.Context, *assistantRunExecution) error) (*Server, string, *assistantRunExecution, *codexbridge.Manager, string) {
	t.Helper()
	entered := make(chan *assistantRunExecution, 1)
	s, token, start, executor := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		entered <- e
		if execute != nil {
			return execute(ctx, e)
		}
		<-ctx.Done()
		return ctx.Err()
	})
	s.assistantRunExecutor = &assistantCodexExecutor{executor}
	connection := aiTestConnection(connectionID, "Subscription fixture", "https://example.invalid")
	connection.Assistant.Engine, connection.Assistant.Endpoint = "codex", nil
	model := "shared-model"
	connection.Assistant.Agent = &AssistantAgentConfig{Model: &model, Provider: "openai", AuthMethod: "subscription", Models: []string{"shared-model", "child-model"}, Tools: []string{"validate", "get_schema", "experimental_evaluate"}, Effort: "medium", ChildModel: "child-model", CriticModel: "child-model"}
	raw, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: connectionID, Connections: []aiConnection{connection}})
	assistantRunMust(t, err)
	assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, assistantProfilePath), []byte(profile), 0600))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "execution-brief.md"), []byte("HOST-BRIEF-ONCE"), 0600))
	if phase == "plan" || connectionID != testAIAlpha {
		saved := policyWorkspaceRead(t, s, token, "/api/conversations")
		var doc conversationDocument
		assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
		if phase == "plan" {
			doc.Chats[0] = bytes.Replace(doc.Chats[0], []byte(`"work"`), []byte(`"plan"`), 1)
		}
		doc.Chats[0] = bytes.ReplaceAll(doc.Chats[0], []byte(testAIAlpha), []byte(connectionID))
		if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		saved = policyWorkspaceRead(t, s, token, "/api/conversations")
		assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
		start.Revision = digestOf(doc.Chats[0])
	}
	if prepare != nil {
		start = prepare(s, token, start)
	}
	// Construct a fresh local bridge profile before the authenticated start; the
	// exact selected-account map resolves this Manager in the private binding.
	fixture := t.TempDir()
	binary, trace := filepath.Join(fixture, "codex-peer"), filepath.Join(fixture, "trace.jsonl")
	// Ledger pathname is resolved after admission, but before the first launch.
	peer, peerErr := os.Create(filepath.Join(fixture, "peer.py"))
	assistantRunMust(t, peerErr)
	_, err = peer.WriteString(assistantExecutionCodexPeer)
	assistantRunMust(t, err)
	assistantRunMust(t, peer.Close())
	wrapper := fmt.Sprintf("#!/bin/sh\nexec python3 '%s' '%s' '%s' \"$(cat '%s')\"\n", filepath.Join(fixture, "peer.py"), mode, trace, filepath.Join(fixture, "ledger-path"))
	assistantRunMust(t, os.WriteFile(binary, []byte(wrapper), 0700))
	m, err := codexbridge.NewManager(codexbridge.Options{Binary: binary, ProfileDir: filepath.Join(fixture, "account"), ProjectDir: s.cfg.ProjectDir})
	assistantRunMust(t, err)
	t.Cleanup(m.Close)
	s.codex = m
	s.aiAccounts.values = map[string]providerAccountManager{connectionID: m}
	assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	var e *assistantRunExecution
	select {
	case e = <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("subscription execution did not start")
	}
	store, err := s.openChatData()
	assistantRunMust(t, err)
	assistantRunMust(t, os.WriteFile(filepath.Join(fixture, "ledger-path"), []byte(filepath.Join(store.root.Name(), e.writer.lease.store.name)), 0600))
	assistantRunMust(t, store.root.Close())
	t.Cleanup(func() {
		e.cancel()
		select {
		case <-e.done:
		case <-time.After(3 * time.Second):
			t.Error("subscription execution did not drain")
		}
	})
	return s, token, e, m, trace
}

func assistantCodexLog(t *testing.T, trace string) []map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(trace)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	assistantRunMust(t, err)
	var out []map[string]json.RawMessage
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var row map[string]json.RawMessage
		assistantRunMust(t, json.Unmarshal(line, &row))
		out = append(out, row)
	}
	return out
}
func assistantCodexWait(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("private bridge did not reach fixture boundary", path)
}
func assistantCodexRequest() adapter.AgentRequest {
	return adapter.AgentRequest{Model: "shared-model", Prompt: "EXACT-AGENT-PROMPT", Instructions: "EXACT-AGENT-INSTRUCTIONS", Phase: "author", Effort: "medium", Tools: []adapter.AgentTool{{Name: "desk_runtime_0", Description: "get_schema: fixture", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
}
func assistantCodexCallbacks(calls *atomic.Int32) adapter.AgentCallbacks {
	return adapter.AgentCallbacks{Event: func(context.Context, adapter.AgentEvent) error { return nil }, Tool: func(context.Context, adapter.AgentToolCall) (adapter.AgentToolAnswer, error) {
		calls.Add(1)
		return adapter.AgentToolAnswer{Text: `{"exact":false,"missing":null,"decimal":1.20}`}, nil
	}}
}

func TestAssistantExecutionCodexProjectEffort(t *testing.T) {
	for _, tc := range []struct {
		name, preferences, effort string
		legacy                    bool
	}{
		{name: "v2-override", preferences: `{"inherit":false,"models":["shared-model","child-model"],"model":"shared-model","effort":"low"}`, effort: "low"},
		{name: "legacy-override", legacy: true, preferences: `{"inherit":false,"models":["shared-model","child-model"],"model":"shared-model","effort":"high"}`, effort: "high"},
		{name: "v2-inherited", preferences: `{"inherit":true}`, effort: "medium"},
		{name: "legacy-inherited", legacy: true, preferences: `{"inherit":true}`, effort: "medium"},
		{name: "v2-no-preference", preferences: "absent", effort: "medium"},
		{name: "v2-global-inherited", effort: "medium"},
		{name: "v2-model-default-omitted", preferences: `{"inherit":false,"models":["shared-model","child-model"],"model":"shared-model"}`},
		{name: "legacy-model-default-omitted", legacy: true, preferences: `{"inherit":false,"models":["shared-model","child-model"],"model":"shared-model"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			connectionID := testAIAlpha
			profile := `{"profileVersion":2,"inherit":true}`
			if tc.legacy {
				connectionID = "legacy-codex"
				profile = fmt.Sprintf(`{"profileVersion":1,"codex":%s}`, tc.preferences)
			} else if tc.preferences != "" {
				models := `{}`
				if tc.preferences != "absent" {
					models = fmt.Sprintf(`{%q:%s}`, connectionID, tc.preferences)
				}
				profile = fmt.Sprintf(`{"profileVersion":2,"inherit":false,"connections":[%q],"defaultConnection":%q,"models":%s}`, connectionID, connectionID, models)
			}
			s, _, e, _, trace := assistantCodexSourceFixture(t, "delegate", "work", connectionID, profile, func(s *Server, _ string, start assistantRunStart) assistantRunStart {
				assistantFixtureReviewOff(s)
				return start
			})
			x := assistantExecutionOpen(t, s, e)
			if x.Run.Agent.Effort != tc.effort {
				t.Fatalf("effective saved effort = %q, want %q", x.Run.Agent.Effort, tc.effort)
			}
			review := false
			x.Run.Review = &review
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
			wireEffort := tc.effort
			if wireEffort == "" {
				// The actual Manager resolves omitted effort from each discovered
				// model's default before turn/start; it must not use shared config.
				wireEffort = "high"
			}
			generations := 0
			for _, row := range assistantCodexLog(t, trace) {
				if string(row["type"]) != `"generation"` {
					continue
				}
				var thread struct{ Model string }
				var turn struct{ Effort string }
				assistantRunMust(t, json.Unmarshal(row["request"], &thread))
				assistantRunMust(t, json.Unmarshal(row["turn"], &turn))
				if generations >= 3 || thread.Model != []string{"shared-model", "child-model", "shared-model"}[generations] || turn.Effort != wireEffort {
					t.Fatalf("effective effort lost at parent/child generation %d: model=%q effort=%q want=%q", generations, thread.Model, turn.Effort, wireEffort)
				}
				generations++
			}
			if generations != 3 {
				t.Fatalf("parent/child/parent generations = %d, want 3", generations)
			}
			for _, op := range assistantExecutionRecord(t, e).Operations {
				if op.Kind != "model" {
					continue
				}
				var request adapter.AgentRequest
				assistantRunMust(t, json.Unmarshal(op.Intent, &request))
				if request.Effort != tc.effort {
					t.Fatalf("retained invocation effort = %q, want %q", request.Effort, tc.effort)
				}
			}
		})
	}
}

func TestAssistantExecutionCodexFactoryDelegation(t *testing.T) {
	s, _, e, _, trace := assistantCodexFixture(t, "delegate", "work")
	x := assistantExecutionOpen(t, s, e)
	observer := &assistantExecutionPromptObserver{Host: x.Run.Host}
	x.Run.Host = observer
	if x.Run.Agent == nil || x.Run.Model != nil {
		t.Fatal("subscription factory did not bind exactly one capability")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	record := assistantExecutionRecord(t, e)
	if e.ctx.Err() != nil || record.Checkpoint.Status != "complete" || record.Checkpoint.Unit != "provider-managed" || record.Checkpoint.Used != 0 || record.Checkpoint.ID != e.record.Checkpoint.ID {
		t.Fatalf("subscription composition failed: ctx=%v cp=%+v events=%+v", e.ctx.Err(), record.Checkpoint, record.Events)
	}
	var generations []map[string]json.RawMessage
	for _, row := range assistantCodexLog(t, trace) {
		if string(row["type"]) == `"generation"` {
			generations = append(generations, row)
		}
	}
	if len(generations) != 3 {
		t.Fatalf("parent/child/parent did not complete: %d events=%+v", len(generations), record.Events)
	}
	for i, expected := range []string{"shared-model", "child-model", "shared-model"} {
		var request struct {
			Model            string
			BaseInstructions string
			DynamicTools     []codexbridge.Tool
		}
		assistantRunMust(t, json.Unmarshal(generations[i]["request"], &request))
		if request.Model != expected {
			t.Fatal("delegated model preference changed", request.Model)
		}
		if i == 0 {
			for _, part := range []string{"HOST-INSTRUCTIONS-ONCE", "HOST-BRIEF-ONCE"} {
				if strings.Count(request.BaseInstructions, part) != 1 {
					t.Fatal("canonical instructions prepared twice or omitted", part)
				}
			}
			if strings.Count(string(generations[i]["prompt"]), "SERVED-RUNTIME-PROMPT") != 1 {
				t.Fatal("served prompt changed")
			}
		}
		for _, tool := range request.DynamicTools {
			if strings.HasPrefix(tool.Description, "experimental_test_cases:") || i == 1 && strings.HasPrefix(tool.Description, "delegate_tasks:") {
				t.Fatal("child or served tool ceiling widened")
			}
		}
	}
	if observer.brief != 3 || observer.instructions != 3 || observer.memory != 0 {
		t.Fatal("canonical Host did not assemble once", observer)
	}
	if record.Checkpoint.Delegation == nil || record.Checkpoint.Handoff == nil || len(record.Checkpoint.Handoff.Tasks) != 1 || record.Checkpoint.Handoff.Tasks[0].State != "complete" {
		t.Fatalf("child result/figures missing: %+v", record.Checkpoint.Delegation)
	}
	if record.Checkpoint.Delegation.Parent.AgentRuns == nil || *record.Checkpoint.Delegation.Parent.AgentRuns != 2 || record.Checkpoint.Delegation.Parent.ModelRequests != nil {
		t.Fatal("provider-managed parent figures changed")
	}
	if len(record.Checkpoint.Delegation.Batches) != 1 || len(record.Checkpoint.Delegation.Batches[0].Children) != 1 || record.Checkpoint.Delegation.Batches[0].Children[0].AgentRuns == nil || *record.Checkpoint.Delegation.Batches[0].Children[0].AgentRuns != 1 || record.Checkpoint.Delegation.Batches[0].Children[0].ModelRequests != nil {
		t.Fatal("provider-managed child figures changed")
	}
	knownHandoff := false
	for _, op := range record.Operations {
		if op.Kind == "model" {
			var outcome assistantAgentOutcome
			assistantRunMust(t, json.Unmarshal(op.Result, &outcome))
			if outcome.Stop == "delegation" {
				knownHandoff = true
				if outcome.Failure != nil {
					t.Fatal("canonical handoff retained unknown work")
				}
			}
		}
	}
	if !knownHandoff {
		t.Fatal("canonical handoff acknowledgement missing")
	}
	for _, op := range record.Operations {
		if op.Kind == "model" && op.State != "completed" {
			t.Fatal("released invocation outcome not retained", op)
		}
	}
}

func TestAssistantExecutionCodexExactReceiptsAndRefusals(t *testing.T) {
	for _, mode := range []string{"normal", "duplicate", "unoffered", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			_, _, e, _, trace := assistantCodexFixture(t, mode, "work")
			agent, err := e.assistantAgent()
			assistantRunMust(t, err)
			var calls atomic.Int32
			callbacks := assistantCodexCallbacks(&calls)
			inner := callbacks.Tool
			callbacks.Tool = func(ctx context.Context, call adapter.AgentToolCall) (adapter.AgentToolAnswer, error) {
				r := assistantExecutionRecord(t, e)
				if len(r.Operations) < 2 || r.Operations[len(r.Operations)-1].Kind != "callback" || r.Operations[len(r.Operations)-1].State != "dispatched" {
					t.Error("callback preceded acknowledged tool dispatch")
				}
				return inner(ctx, call)
			}
			err = agent.Run(context.Background(), assistantCodexRequest(), callbacks)
			if mode == "normal" && err != nil || mode != "normal" && err == nil {
				t.Fatalf("malformed callback handling %q: %v", mode, err)
			}
			if calls.Load() > 1 || (mode == "unoffered" || mode == "malformed") && calls.Load() != 0 {
				t.Fatal("unpaired callback executed", calls.Load())
			}
			record := assistantExecutionRecord(t, e)
			if len(record.Operations) == 0 || record.Operations[0].Kind != "model" || !bytes.Equal(record.Operations[0].Intent, mustAssistantCodexJSON(assistantCodexRequest())) {
				t.Fatal("exact invocation identity missing", record.Operations)
			}
			if mode == "normal" {
				if len(record.Operations) != 2 || !strings.Contains(string(record.Operations[1].Result), `1.20`) || !strings.Contains(string(record.Operations[1].Intent), record.Operations[0].ID) {
					t.Fatal("tool pairing lost exact typed answer", record.Operations)
				}
				if err = e.Dispatch(context.Background(), record.Operations[0].ID); err == nil {
					t.Fatal("old receipt resumed generation")
				}
			}
			if len(assistantCodexLog(t, trace)) == 0 {
				t.Fatal("actual process fixture was bypassed")
			}
		})
	}
}
func mustAssistantCodexJSON(value any) []byte { body, _ := json.Marshal(value); return body }

func TestAssistantExecutionCodexGenerationRechecks(t *testing.T) {
	for _, mode := range []string{"source", "connection", "profile", "session", "policy", "persistence"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e, _, trace := assistantCodexFixture(t, "discovery-wait", "work")
			agent, err := e.assistantAgent()
			assistantRunMust(t, err)
			var calls atomic.Int32
			done := make(chan error, 1)
			go func() {
				done <- agent.Run(context.Background(), assistantCodexRequest(), assistantCodexCallbacks(&calls))
			}()
			assistantCodexWait(t, trace+".waiting")
			switch mode {
			case "source":
				saved := policyWorkspaceRead(t, s, token, "/api/conversations")
				var doc conversationDocument
				assistantRunMust(t, json.Unmarshal(bytes.ReplaceAll(saved.Content, []byte("Review exact input"), []byte("Changed exact input")), &doc))
				if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); w.Code != 200 {
					t.Fatal(w.Body.String())
				}
			case "connection":
				connection := e.source.Connection
				connection.Assistant.Agent.Effort = "high"
				raw, encodeErr := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{connection}})
				assistantRunMust(t, encodeErr)
				assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
			case "profile":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, assistantProfilePath), []byte(`{"profileVersion":1,"codex":{"inherit":false,"models":["child-model"]}}`), 0600))
			case "session":
				s.sessions.revoke(token)
			case "policy":
				s.signIn.mu.Lock()
				s.signIn.cancelEpoch()
				s.signIn.epoch, s.signIn.cancelEpoch = context.WithCancel(context.Background())
				s.signIn.generation++
				s.signIn.mu.Unlock()
			case "persistence":
				store, openErr := s.openChatData()
				assistantRunMust(t, openErr)
				ledger := e.writer.lease.store.name
				assistantRunMust(t, store.root.Rename(ledger, ledger+".fixture-held"))
				assistantRunMust(t, store.root.Mkdir(ledger, 0700))
				assistantRunMust(t, store.root.Close())
			}
			assistantRunMust(t, os.WriteFile(trace+".release", []byte("release"), 0600))
			select {
			case err = <-done:
				if err == nil || adapter.IsBusy(err) {
					t.Fatal("changed admission was retried or succeeded", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("generation ignored changed authority")
			}
			for _, row := range assistantCodexLog(t, trace) {
				if string(row["type"]) == `"generation"` {
					t.Fatal("generation crossed changed authority")
				}
			}
			if calls.Load() != 0 {
				t.Fatal("callback followed revoked generation")
			}
		})
	}
}

func TestAssistantExecutionCodexBusyAndCallbackOutcomes(t *testing.T) {
	t.Run("bounded-published-retry", func(t *testing.T) {
		s, _, e, _, trace := assistantCodexFixture(t, "normal", "work")
		x := assistantExecutionOpen(t, s, e)
		agent := x.Run.Agent
		entered, release, holderDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		defer func() {
			select {
			case <-release:
			default:
				close(release)
			}
		}()
		var calls atomic.Int32
		callbacks := assistantCodexCallbacks(&calls)
		callbacks.Tool = func(ctx context.Context, _ adapter.AgentToolCall) (adapter.AgentToolAnswer, error) {
			close(entered)
			select {
			case <-release:
				return adapter.AgentToolAnswer{Text: "released"}, nil
			case <-ctx.Done():
				return adapter.AgentToolAnswer{}, ctx.Err()
			}
		}
		go func() { holderDone <- agent.Run(context.Background(), assistantCodexRequest(), callbacks) }()
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("holder never acquired account")
		}
		// Observe real, durably retained refusals instead of timing how quickly
		// the adapter can assemble its prompt and persist receipts under race.
		busy := make(chan struct{}, 3)
		retryAgent := *agent
		retryAgent.Run = func(ctx context.Context, request adapter.AgentRequest, callbacks adapter.AgentCallbacks) error {
			err := agent.Run(ctx, request, callbacks)
			if err == adapter.ErrBusy {
				busy <- struct{}{}
			}
			return err
		}
		x.Run.Agent = &retryAgent
		done := make(chan error, 1)
		go func() { done <- x.Adapter.Run(context.Background(), x.Run) }()
		for i := 0; i < 2; i++ {
			select {
			case <-busy:
			case err := <-done:
				t.Fatalf("published retry stopped before two busy refusals: %v", err)
			case <-time.After(15 * time.Second):
				t.Fatal("published retry did not return two busy refusals")
			}
		}
		close(release)
		assistantRunMust(t, <-holderDone)
		select {
		case err := <-done:
			assistantRunMust(t, err)
		case <-time.After(4 * time.Second):
			t.Fatal("published bounded retry did not finish")
		}
		r := assistantExecutionRecord(t, e)
		refused, completed := 0, 0
		for _, op := range r.Operations {
			if op.Kind == "model" {
				if op.State == "refused" {
					refused++
				}
				if op.State == "completed" {
					completed++
				}
			}
		}
		if refused != 2 || completed != 2 || r.Checkpoint.Used != 0 || e.ctx.Err() != nil {
			t.Fatalf("busy changed accounting or admission: refused=%d completed=%d cp=%+v ctx=%v", refused, completed, r.Checkpoint, e.ctx.Err())
		}
		generations := 0
		for _, row := range assistantCodexLog(t, trace) {
			if string(row["type"]) == `"generation"` {
				generations++
			}
		}
		if generations != 2 {
			t.Fatal("busy replayed admitted work", generations)
		}
	})
	t.Run("callback-busy-is-admitted", func(t *testing.T) {
		_, _, e, _, trace := assistantCodexFixture(t, "normal", "work")
		a, err := e.assistantAgent()
		assistantRunMust(t, err)
		var calls atomic.Int32
		callbacks := assistantCodexCallbacks(&calls)
		callbacks.Tool = func(context.Context, adapter.AgentToolCall) (adapter.AgentToolAnswer, error) {
			calls.Add(1)
			return adapter.AgentToolAnswer{}, codexbridge.ErrBusy
		}
		err = a.Run(context.Background(), assistantCodexRequest(), callbacks)
		if err == nil || adapter.IsBusy(err) || calls.Load() != 1 {
			t.Fatal("admitted callback error became busy admission", err, calls.Load())
		}
		r := assistantExecutionRecord(t, e)
		var result assistantAgentOutcome
		assistantRunMust(t, json.Unmarshal(r.Operations[0].Result, &result))
		if r.Operations[0].State != "completed" || result.Failure == nil || result.Failure.Outcome != "unknown" || e.ctx.Err() == nil {
			t.Fatal("admitted uncertainty was lost", r.Operations[0])
		}
		if len(assistantCodexLog(t, trace)) != 2 {
			t.Fatal("callback failure replayed process")
		}
	})
}

func TestAssistantExecutionCodexCancellationReleasesAccount(t *testing.T) {
	_, _, e, m, trace := assistantCodexFixture(t, "normal", "work")
	a, err := e.assistantAgent()
	assistantRunMust(t, err)
	entered := make(chan struct{})
	callbackDone := make(chan struct{})
	var calls atomic.Int32
	callbacks := assistantCodexCallbacks(&calls)
	callbacks.Tool = func(ctx context.Context, _ adapter.AgentToolCall) (adapter.AgentToolAnswer, error) {
		close(entered)
		<-ctx.Done()
		close(callbackDone)
		return adapter.AgentToolAnswer{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx, assistantCodexRequest(), callbacks) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("callback did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("canceled invocation did not return")
	}
	select {
	case <-callbackDone:
	default:
		t.Fatal("invocation returned with live callback")
	}
	if e.ctx.Err() == nil {
		t.Fatal("arbitrary invocation cancellation left root admission open")
	}
	if err := a.Run(context.Background(), assistantCodexRequest(), assistantCodexCallbacks(&calls)); err == nil {
		t.Fatal("canceled unknown work was dispatched again")
	}
	// Models needs the same real account lock and process. Its new process also
	// proves the previous provider process was reaped, rather than just unlocked.
	models, err := m.Models(context.Background())
	assistantRunMust(t, err)
	if len(models) != 2 || len(assistantCodexLog(t, trace)) != 2 {
		t.Fatal("account did not release cleanly")
	}
}

func TestAssistantExecutionCodexPlanModelCeiling(t *testing.T) {
	s, _, e, _, _ := assistantCodexFixture(t, "normal", "plan")
	x := assistantExecutionOpen(t, s, e)
	for _, tool := range x.Run.Tools {
		if tool.Name == "experimental_evaluate" || tool.Name == "experimental_test_cases" {
			t.Fatal("Plan widened Runtime offer")
		}
	}
	if err := x.Run.Agent.Enabled(context.Background(), "foreign-model"); err == nil {
		t.Fatal("unconfigured model admitted")
	}
}

func TestAssistantExecutionCodexQuestionAndAllowancePolicy(t *testing.T) {
	for _, mode := range []string{"question", "budget"} {
		t.Run(mode, func(t *testing.T) {
			s, _, e, m, trace := assistantCodexFixture(t, mode, "work")
			x := assistantExecutionOpen(t, s, e)
			if mode == "question" {
				// A fixture-owned interaction supplies no production domain tool.
				interaction := "question"
				offer, err := x.gate.Open(context.Background(), []contract.ToolOfferHostTool{{Name: "ask_user", Interaction: &interaction}})
				assistantRunMust(t, err)
				x.gate.Enter(gate.LoopPhase(offer))
				x.Run.HostTools = []runcontext.HostTool{{Name: "ask_user", Description: "Ask a scope question.", Interaction: interaction, InputSchema: json.RawMessage(`{"type":"object"}`)}}
			}
			if mode == "budget" {
				effect := "plan"
				offer, err := x.gate.Open(context.Background(), []contract.ToolOfferHostTool{{Name: "request_budget_increase", Effect: &effect}})
				assistantRunMust(t, err)
				x.gate.Enter(gate.LoopPhase(offer))
			}
			assistantRunMust(t, x.Adapter.Run(context.Background(), x.Run))
			r := assistantExecutionRecord(t, e)
			if e.ctx.Err() != nil || r.Checkpoint.Used != 0 || r.Checkpoint.Limit != 1 || len(r.Checkpoint.Grants) != 0 {
				t.Fatal("pause changed root allowance", r.Checkpoint, e.ctx.Err())
			}
			if mode == "question" {
				found := false
				for _, event := range r.Events {
					if event.Event.UserQuestion != nil {
						found = true
					}
				}
				if !found {
					t.Fatalf("question did not persist: %+v", r.Events)
				}
			} else {
				unavailable := false
				for _, op := range r.Operations {
					if op.Kind == "callback" && strings.Contains(string(op.Result), "unavailable") {
						unavailable = true
					}
				}
				if !unavailable || r.Checkpoint.Status != "complete" {
					t.Fatalf("provider-managed budget policy changed: %+v", r.Checkpoint)
				}
			}
			if _, err := m.Models(context.Background()); err != nil {
				t.Fatal("paused invocation retained account/process", err)
			}
			generations := 0
			for _, row := range assistantCodexLog(t, trace) {
				if string(row["type"]) == `"generation"` {
					generations++
				}
			}
			if generations != 1 {
				t.Fatal("pause replayed generation", generations)
			}
		})
	}
}

func TestAssistantExecutionCodexReconstructionRetainsIdentityAndTests(t *testing.T) {
	s, _, e, _, trace := assistantCodexFixture(t, "normal", "work")
	x := assistantExecutionOpen(t, s, e)
	var count atomic.Int32
	assistantRunMust(t, x.Run.Agent.Run(context.Background(), assistantCodexRequest(), assistantCodexCallbacks(&count)))
	before := assistantExecutionRecord(t, e)
	child, _, err := x.Run.Children(context.Background(), contract.DelegatedTaskProfile{ID: "fixture"}, func(contract.RunEvent) {})
	assistantRunMust(t, err)
	result, err := child.CallTool(context.Background(), "experimental_test_cases", json.RawMessage(`{"pack":"{}","matrix":"{}"}`))
	assistantRunMust(t, err)
	if !result.IsError || x.gate.TestCalls() != 1 {
		t.Fatal("served test ceiling widened")
	}
	// The canonical save hook carries the shared count. A reconstructed private
	// capability restores this persisted root rather than resetting its IDs.
	x.Run.Emit(contract.RunEvent{ExecutionCheckpoint: &contract.RunEventExecutionCheckpoint{Type: "execution_checkpoint", Checkpoint: *before.Checkpoint}})
	retained := assistantExecutionRecord(t, e)
	e.record.Checkpoint = retained.Checkpoint
	x.Close()
	x2, err := e.openAssistantExecution(context.Background(), assistantExecutionPrompt{Name: "served"})
	assistantRunMust(t, err)
	defer x2.Close()
	if x2.gate.TestCalls() != 1 || x2.Run.Context.Execution.ID != before.Checkpoint.ID {
		t.Fatal("reconstruction reset root/shared tests")
	}
	assistantRunMust(t, x2.Run.Agent.Run(context.Background(), assistantCodexRequest(), assistantCodexCallbacks(&count)))
	after := assistantExecutionRecord(t, e)
	ids := map[string]bool{}
	for _, op := range after.Operations {
		if ids[op.ID] {
			t.Fatal("reconstructed identity reused", op.ID)
		}
		ids[op.ID] = true
	}
	if count.Load() != 2 || len(assistantCodexLog(t, trace)) != 6 {
		t.Fatal("reconstruction resumed an old invocation", count.Load())
	}
}

func TestAssistantExecutionCodexCallbackAuthority(t *testing.T) {
	s, _, e, _, trace := assistantCodexFixture(t, "callback-wait", "work")
	a, err := e.assistantAgent()
	assistantRunMust(t, err)
	var calls atomic.Int32
	done := make(chan error, 1)
	go func() { done <- a.Run(context.Background(), assistantCodexRequest(), assistantCodexCallbacks(&calls)) }()
	assistantCodexWait(t, trace+".waiting")
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, assistantProfilePath), []byte(`{"profileVersion":1,"codex":{"inherit":false,"models":["child-model"]}}`), 0600))
	assistantRunMust(t, os.WriteFile(trace+".release", []byte("release"), 0600))
	select {
	case err = <-done:
		if err == nil || adapter.IsBusy(err) {
			t.Fatal("stale callback became retryable/successful", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("callback revocation did not stop run")
	}
	if calls.Load() != 0 {
		t.Fatal("callback ran under stale model/profile")
	}
	r := assistantExecutionRecord(t, e)
	if len(r.Operations) != 1 || r.Operations[0].State != "completed" {
		t.Fatal("revoked callback obtained operation authority", r.Operations)
	}
}
