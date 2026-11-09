package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	agentadapter "github.com/Judgment-Pack/judgment-pack-agent/go/adapter/agent"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/delegate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/certification"
)

// Handwritten native protocol peer. Each AgentScript play is input data; all
// admission, account ownership, process reaping, tool validation and callbacks
// remain in the actual private Manager and Desk assistantAgent binding.
const assistantBindingCertificationPeer = `#!/usr/bin/env python3
import base64,json,os,sys
mode,trace,ledger=sys.argv[1:4]
pidfile=trace+'.pid'
if os.path.exists(pidfile):
 with open(pidfile) as f: prior=int(f.read())
 try: os.kill(prior,0)
 except ProcessLookupError: pass
 else: sys.exit(71)
with open(pidfile,'w') as f: f.write(str(os.getpid()))
request={}; steps=[]; at=0; pending=set()
def write(v): print(json.dumps(v,separators=(',',':')),flush=True)
def note(method,p): write({'method':method,'params':p})
def tool(step,item):
 name=step['Tool']
 for offered in request['dynamicTools']:
  if offered['description'].startswith(name+':'): name=offered['name']; break
 rpc='rpc-'+item; pending.add(rpc)
 write({'id':rpc,'method':'item/tool/call','params':{'threadId':'thread','turnId':'turn','callId':'call-'+item,'tool':name,'arguments':step.get('Arguments') or {}}})
def advance():
 global at
 while at<len(steps):
  step=steps[at]; at+=1; item=str(at)
  if step.get('Parallel'):
   for i,call in enumerate(step['Parallel']): tool(call,item+'-'+str(i))
   return
  if step.get('Tool'):
   tool(step,item); return
  text=step.get('Commentary') or step.get('Say')
  if text:
   phase='commentary' if step.get('Commentary') else 'final_answer'
   note('item/completed',{'threadId':'thread','turnId':'turn','item':{'id':'item-'+item,'type':'agentMessage','phase':phase,'text':text}})
 if play.get('Fail'):
  note('error',{'threadId':'thread','turnId':'turn','willRetry':False,'error':{'message':'scripted provider failure'}})
 else: note('turn/completed',{'threadId':'thread','turn':{'id':'turn','status':'completed'}})
for line in sys.stdin:
 q=json.loads(line); method=q.get('method',''); p=q.get('params',{})
 def reply(v): write({'id':q['id'],'result':v})
 if method=='initialize': reply({'userAgent':'jps_desk/0.157.1 (Linux)'})
 elif method=='initialized': pass
 elif method=='account/read': reply({'requiresOpenaiAuth':True,'account':{'type':'chatgpt','planType':'plus'}})
 elif method=='model/list':
  if p.get('includeHidden'):
   with open(os.path.join(os.environ['CODEX_HOME'],'model-catalog.json')) as f: catalog=json.load(f)
   reply({'data':[{'model':m['slug']} for m in catalog['models']]})
  else: reply({'data':[{'model':m,'displayName':m,'defaultReasoningEffort':'medium','supportedReasoningEfforts':[{'reasoningEffort':e} for e in ['low','medium','high']]} for m in ['agent-model','agent-small','agent-critic']]})
 elif method=='thread/start':
  request=p
  if p['approvalPolicy']!='never' or p['allowProviderModelFallback']!=False or p['permissions']!='jps' or p['environments']!=[] or p['runtimeWorkspaceRoots']!=[]: sys.exit(72)
  reply({'thread':{'id':'thread'},'model':p['model'],'modelProvider':'openai','approvalPolicy':'never','cwd':os.getcwd()})
 elif method=='turn/start':
  with open(ledger) as f: runs=json.load(f)['runs']
  prompt=p['input'][0]['text']
  ops=[op for r in runs for op in (r.get('operations') or []) if op['kind']=='model' and json.loads(base64.b64decode(op['intent']))['prompt']==prompt]
  if not ops or ops[-1]['state']!='dispatched': sys.exit(73)
  with open(trace+'.play') as f: play=json.load(f)
  steps=play.get('Steps') or []
  reply({'turn':{'id':'turn'}})
  if not play.get('Hold'): advance()
 elif not method:
  pending.discard(q.get('id'))
  if not pending: advance()
 else: sys.exit(74)
`

func assistantBindingCertificationNativeHost(t *testing.T) certification.AgentHost {
	t.Helper()
	return certification.AgentHost{Name: "Desk assistantAgent with its private codexbridge.Manager", Bind: func(script certification.AgentScript) adapter.AgentRun {
		_, _, e, _, trace := assistantCodexSourceFixture(t, "certification", "work", testAIAlpha, `{"profileVersion":2,"inherit":true}`, func(s *Server, token string, start assistantRunStart) assistantRunStart {
			model := "agent-model"
			connection := aiTestConnection(testAIAlpha, "Native certification fixture", "https://example.invalid")
			connection.Assistant.Engine, connection.Assistant.Endpoint = "codex", nil
			connection.Assistant.Agent = &AssistantAgentConfig{Model: &model, Provider: "openai", AuthMethod: "subscription", Models: []string{model, "agent-small", "agent-critic"}, Tools: []string{"get_schema", "validate", "experimental_evaluate"}, Effort: "medium", ChildModel: "agent-small", CriticModel: "agent-critic"}
			raw, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{connection}})
			assistantRunMust(t, err)
			assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
			saved := policyWorkspaceRead(t, s, token, "/api/conversations")
			var doc conversationDocument
			assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
			doc.Chats[0] = bytes.ReplaceAll(doc.Chats[0], []byte("shared-model"), []byte(model))
			if response := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); response.Code != 200 {
				t.Fatal(response.Body.String())
			}
			saved = policyWorkspaceRead(t, s, token, "/api/conversations")
			assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
			start.Revision = digestOf(doc.Chats[0])
			return start
		})
		assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(assistantBindingCertificationPeer), 0600))
		bound, err := e.assistantAgent()
		assistantRunMust(t, err)
		number := 0
		return func(ctx context.Context, request adapter.AgentRequest, callbacks adapter.AgentCallbacks) error {
			number++
			play := script(request, number)
			if play.Busy != 0 || play.Fail != nil {
				return fmt.Errorf("this bounded native contract probe does not implement Busy or Fail scripts")
			}
			raw, err := json.Marshal(play)
			if err != nil {
				return err
			}
			if err = os.WriteFile(trace+".play", raw, 0600); err != nil {
				return err
			}
			return bound.Run(ctx, request, callbacks)
		}
	}}
}

// This is an explicit opt-in diagnostic until the released oracle and native
// unoffered-call boundary have a contract disposition. It must fail honestly;
// no stand-in, filtered finding or expected-failure assertion certifies Desk.
func TestAssistantBindingCertificationNativeContract(t *testing.T) {
	if os.Getenv("DESK_ASSISTANT_NATIVE_CERTIFICATION_PROBE") != "1" {
		t.Skip("native certification contract disposition is required; explicit diagnostic only")
	}
	host := assistantBindingCertificationNativeHost(t)
	with := func(inner certification.Adapter) certification.Adapter {
		return delegate.With(inner, delegate.Options{})
	}
	var report bytes.Buffer
	passed, failed := certification.CertifyAgent(t.Context(), &report, nil, host, with, assistantCanonicalAgent(agentadapter.New()), "project")
	t.Log(report.String())
	t.Logf("actual native binding: %d passed, %d failed; scope: released project check only", passed, failed)
	if failed != 0 {
		t.Fail()
	}
}
