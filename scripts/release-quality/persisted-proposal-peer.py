#!/usr/bin/env python3
"""Finite offline peers: exact authored proposal and checks; unknown calls refuse."""
import hashlib,json,os,sys
role,project,trace,candidate_path=sys.argv[1:5]
extra=sys.argv[5:]
if (role=='runtime' and extra!=['mcp']) or (role=='native' and extra!=['app-server','--stdio','--strict-config']):sys.exit(61)
with open(candidate_path) as f:candidate=f.read()
def record(kind,tag=''):
 fd=os.open(trace,os.O_WRONLY|os.O_APPEND|os.O_NOFOLLOW)
 try:os.write(fd,(json.dumps({'kind':kind,'role':role,'pid':os.getpid(),'tag':tag})+'\n').encode())
 finally:os.close(fd)
def refuse(reason='request'):
 allowed_methods=['initialize','initialized','notifications/initialized','tools/list','tools/call','prompts/list','prompts/get','account/read','model/list','thread/start','turn/start']
 allowed_names=['list_packs','describe_runtime','get_pack','validate','experimental_test_cases']
 method_value=method if method in allowed_methods else 'other'
 name_value=p.get('name') if isinstance(p,dict) and p.get('name') in allowed_names else 'other'
 record('refused',json.dumps({'method':method_value,'name':name_value,'reason':reason if reason in ['request','matrix','validation','limit'] else 'request'},separators=(',',':')));sys.exit(65)
def send(v):print(json.dumps(v,separators=(',',':')),flush=True)
def finish():
 send({'method':'item/completed','params':{'threadId':'proposal-thread','turnId':'proposal-turn','item':{'id':'proposal-message','type':'agentMessage','phase':'final_answer','text':'Review the checked proposal.'}}})
 send({'method':'turn/completed','params':{'threadId':'proposal-thread','turn':{'id':'proposal-turn','status':'completed'}}})
record('start');request=None;turns=callbacks=0;method=None;p={}
for index,line in enumerate(sys.stdin):
 if index>=256 or len(line)>4*1024*1024:refuse('limit')
 q=json.loads(line);method=q.get('method');p=q.get('params',{})
 if 'id' not in q:
  if method not in ['initialized','notifications/initialized']:refuse()
  continue
 if role=='runtime':
  if method=='initialize':result={'protocolVersion':'2025-06-18','capabilities':{'tools':{},'prompts':{}},'serverInfo':{'name':'offline-proposal','version':'1'}}
  elif method=='tools/list':result={'tools':[{'name':n,'description':n,'inputSchema':{'type':'object','properties':{},'additionalProperties':True}} for n in ['get_schema','get_pack','list_packs','validate','experimental_test_cases']]}
  elif method=='prompts/list':result={'prompts':[{'name':'author_pack','arguments':[]}]}
  elif method=='prompts/get' and p=={'name':'author_pack','arguments':{}}:result={'messages':[{'role':'user','content':{'type':'text','text':'Preserve policy and saved expectations. Propose a checked update only.'}}]}
  elif method=='tools/call':
   name=p.get('name');a=p.get('arguments',{})
   if name=='list_packs' and a=={}:
    value={'packs':[{'id':'one','packId':'https://example.invalid/judgment-packs/minimal','packVersion':'0.1.0','path':'one.json','description':'A minimal pack'}]};result={'content':[{'type':'text','text':json.dumps(value)}],'structuredContent':value,'isError':False}
   elif name=='describe_runtime' and a=={}:result={'content':[{'type':'text','text':'{"name":"offline-proposal"}'}]}
   elif name=='get_pack' and a=={'pack_id':'one'}:
    text=open(os.path.join(project,'one.json')).read();value=json.loads(text);result={'content':[{'type':'text','text':text}],'structuredContent':{'id':'one','path':'one.json','sha256':hashlib.sha256(text.encode()).hexdigest(),'bytes':len(text.encode()),'packVersion':value['version']},'isError':False}
   elif name=='validate' and set(a)=={'document'}:
    text=a['document'];value=json.loads(text)
    if value.get('id')!='https://example.invalid/judgment-packs/minimal' or value.get('version') not in ['0.1.0','0.1.1']:refuse('validation')
    record('metadata','validate');result={'content':[{'type':'text','text':'{"status":"valid","diagnostics":[]}'}],'structuredContent':{'status':'valid','diagnostics':[]},'isError':False}
   elif name=='experimental_test_cases' and set(a)=={'pack','matrix'} and a['pack']==candidate:
    matrix=json.loads(a['matrix'])
    if matrix!={'matrixVersion':'3','cases':[{'id':'existing','facts':{'n':1},'expectedDisposition':'proceed','focus':'Original requirement: proceed without changing policy.','origin':'manual'}]}:refuse('matrix')
    record('metadata','saved-suite');value={'status':'passed','summary':{'total':1,'passed':1},'packs':[{'rows':[{'id':'existing','expected':'proceed','actual':'proceed','status':'passed'}],'coverage':[]}]};result={'content':[{'type':'text','text':json.dumps(value)}],'structuredContent':value,'isError':False}
   else:refuse()
  else:refuse()
  if method!='tools/call':record('metadata',method)
 elif method=='initialize':result={'userAgent':'jps_desk/0.157.1 (Linux)'}
 elif method=='account/read':result={'requiresOpenaiAuth':True,'account':{'type':'chatgpt','planType':'plus'}}
 elif method=='model/list':
  models=['gpt-6-astra','gpt-6-sol','gpt-6-luna','gpt-5.6-sol','gpt-5.6-terra','gpt-5.6-luna','gpt-5.5'] if p.get('includeHidden') else ['gpt-6-sol']
  result={'data':[{'model':m,'displayName':m,'defaultReasoningEffort':'medium','supportedReasoningEfforts':[{'reasoningEffort':e} for e in ['low','medium','high']]} for m in models]}
 elif method=='thread/start' and request is None:
  request=p
  if p.get('model')!='gpt-6-sol' or p.get('allowProviderModelFallback') is not False:refuse()
  result={'thread':{'id':'proposal-thread'},'model':'gpt-6-sol','modelProvider':'openai','approvalPolicy':'never','cwd':os.getcwd(),'reasoningEffort':'medium'}
 elif method=='turn/start' and turns==0:
  turns+=1;record('native-turn');send({'id':q['id'],'result':{'turn':{'id':'proposal-turn'}}})
  tools=[t for t in request['dynamicTools'] if t['description'].startswith('propose_saved_pack:')]
  if len(tools)!=1:refuse()
  record('callback');send({'id':'proposal-callback','method':'item/tool/call','params':{'threadId':'proposal-thread','turnId':'proposal-turn','callId':'proposal-call','tool':tools[0]['name'],'arguments':{'document':candidate}}});continue
 elif method is None and q['id']=='proposal-callback' and callbacks==0:
  callbacks+=1
  if q.get('result',{}).get('success') is not True:refuse()
  finish();continue
 else:refuse()
 send({'jsonrpc':'2.0','id':q['id'],'result':result})
