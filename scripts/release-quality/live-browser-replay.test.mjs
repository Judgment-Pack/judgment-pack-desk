import test from 'node:test'
import assert from 'node:assert/strict'
import {EventEmitter} from 'node:events'
import {mkdtempSync,writeFileSync,readFileSync,chmodSync,symlinkSync} from 'node:fs'
import {join} from 'node:path'
import {tmpdir} from 'node:os'
import {captureLiveSession,validateRenderedObservation,savedChatComplete,validateLiveControl,validateDescriptor,checkAccounting,settingsOf,draftSettingsOf,privateOwnerIO,privateRetainer,validateSettingsObservation,settingsObserver,draftSettingsObservation,validateTerminalObservation,terminalObservation,terminalObserver} from './live-browser-replay.mjs'
import {parseClosed,parseApplicationSnapshot,readPrivate,writePrivate} from './replay-report.mjs'
import {privateCanonicalRetainer,canonicalReference,readCanonicalEvidence} from './live-canonical-evidence.mjs'
import {fixedInputs} from './live-comparison-plan.mjs'

// All pages, event frames, controls, answers and accounting below are handwritten.
// No browser/program/account/provider is launched by this suite.
const H='a'.repeat(64), origin='http://127.0.0.1:8123'
const control=(role='candidate')=>({kind:'live-two-chat-capture-input',role,model:'chosen-model',effort:'medium',connectionId:'connection-one',connectionName:'Measured subscription',projectRoot:'/provided/project',controlDir:'/provided/control',evidenceDir:'/provided/evidence',ownerConfigurationSha256:H,source:{commit:role==='baseline'?'ef378c406f8b96e2c0c8854bc5e4dff40ac841e2':'c'.repeat(40),tree:role==='baseline'?'0034c3623b5b6e9bb8741da900392fd1c29e1508':'d'.repeat(40),inventory:H,application:H,assets:H,helper:H,browser:H,cli:H,cliVersion:'supplied-version',driver:H,reader:H,runtimeInput:H},inputs:fixedInputs.map(v=>({...structuredClone(v),sliceSha256:'7728e5f53f9539c0343a86d6b4aa9351b1c8aad662acf35cd495dca36c91fe24'})),baselineReader:role==='baseline'?{path:'/provided/baseline-reader.mjs',sha256:H}:null})
const descriptor=()=>({baseUrl:origin,setupToken:'PRIVATE-SETUP-DO-NOT-RETAIN',configurationSha256:H,setupTimeoutMs:300000,caseTimeoutMs:120000,aggregateInferenceTimeoutMs:480000})
const counter=(state='absent',value=0)=>({state,value})
const breakdown=()=>Object.fromEntries(['input_tokens','cached_input_tokens','output_tokens','reasoning_output_tokens','total_tokens','cache_write_input_tokens'].map(k=>[k,counter()]))
function accounting(n) {return {schema_version:1,invocation:n,sequence_exhausted:false,requested_model:'chosen-model',requested_effort:{state:'value',value:'medium'},selected_effort:'medium',acknowledged_model:'chosen-model',acknowledged_provider:'openai',acknowledged_effort:{state:'absent'},thread_id:'thread-'+n,turn_id:'turn-'+n,admitted:true,owned_client_reaped:true,application_success:true,exit_reason:'success',notifications:4,usage_updates:1,usage_present:true,usage_schema_valid:true,last_observed:{observation:1,last_scope:'provider_last_breakdown',total_scope:'provider_thread_total',last:breakdown(),total:breakdown(),model_context_window:counter()},terminal_observed:true,terminal_status:'completed',terminal_usage_finality:'unproven',malformed:false,scope_mismatch:false,overflow:false,routing_disagreement:false,reroute_observed:false,reroute_count:0,reroutes:Array.from({length:8},()=>({thread_id:'',turn_id:'',from_model:'',to_model:'',reason:''})),incomplete:false,fixed_model_ineligible:false}}
class Owner {
 constructor(c,log){this.c=c;this.log=log;this.files=new Map([['owner.json',descriptor()]]);this.commands=[];this.admitted=0;this.completed=[];this.mutateAccounting=null}
 read(name){return this.files.get(name)??null}
 publish(name,v){
  assert(!this.files.has(name));this.commands.push([name,structuredClone(v)]);this.log.push(name);this.files.set(name,v)
  if(name==='abort.json'){this.aborted=true;return}
  const n=Number(name.slice(0,3));assert.equal(n,this.commands.length)
  if(v.action==='begin'){assert.equal(v.case,this.admitted+1);this.admitted=v.case;this.chat=v.chatId}
  else{assert.equal(v.case,this.admitted);assert.equal(v.threadId,'thread-'+v.case);assert.equal(v.turnId,'turn-'+v.case);this.completed.push({caseId:this.c.inputs[v.case-1].id,chatId:v.chatId,completed:true,accounting:this.read('case-'+v.case+'-accounting.json')})}
  this.files.set(name.replace('.json','-ack.json'),structuredClone(v))
  if(n===4)this.files.set('lifecycle.json',{schemaVersion:1,reason:'completed',normalOwnerShutdown:true,shutdownError:false,deadlineCallbacksJoined:true,accountingDropped:0,cases:this.completed,invocationCriteria:'one completed observed invocation per case; not a prospective cap',usageFinality:'unproven',monetaryCost:null})
 }
 report(n){const a=accounting(n);this.mutateAccounting?.(a);this.files.set('case-'+n+'-accounting.json',a)}
}
class Locator {
 constructor(page,type,name='',i=0){Object.assign(this,{page,type,name,i})}
 first(){return this}
 nth(i){return new Locator(this.page,this.type,this.name,i)}
 async count(){if(this.type==='article')return this.page.sent?2:0;if(this.name==='Stop')return 0;return 1}
 locator(selector){return new Locator(this.page,selector.includes('article')?'article':selector.includes('markdown')?'markdown':selector==='li'?'tools':'other','',this.i)}
 async evaluateAll(){assert.equal(this.type,'pane');return this.page.renderSnapshot?.()??{chatId:'chat-'+this.page.n,ids:this.page.sent?['answer-'+this.page.n+'-0','answer-'+this.page.n+'-1']:[]}}
 async waitFor(){}
 async allTextContents(){return ['Read pack schema']}
 async getAttribute(name){if(name==='data-chat-id')return 'chat-'+this.page.n;if(name==='data-message-id')return 'answer-'+this.page.n+'-'+this.i;if(name==='value')return JSON.stringify([this.page.c.connectionId,this.page.c.model]);if(name==='aria-valuetext')return 'Medium';return null}
 async innerText(){return 'Rendered '+this.page.n+'/'+this.i}
 async isChecked(){return this.type==='radio'}
 async uncheck(){}
 async selectOption(){}
 async click(){if(this.name==='New chat'){this.page.log.push('fresh-chat');this.page.n++;this.page.sent=false}if(this.name instanceof RegExp)this.page.emit('request',this.page.requestOf('/api/conversations'))}
 async fill(value){this.page.prompt=value}
 async press(value){if(this.type==='composer'&&value==='Enter')this.page.dispatch()}
}
class Page extends EventEmitter {
 constructor(c,owner,log){super();Object.assign(this,{c,owner,log,n:1,sent:false,prompt:'',closed:0});this.keyboard={press:async()=>{}};this.request={get:async url=>({ok:()=>true,text:async()=>JSON.stringify(url.includes('/api/conversations')?{content:{chats:[this.chat()]}}:this.run())})}}
 setDefaultTimeout(value){assert.equal(value,15000)}
 url(){return origin+'/' }
 context(){return {close:async()=>{this.log.push('context-close');this.closed++}}}
 getByPlaceholder(){return new Locator(this,'composer')}
 getByRole(role,options){return new Locator(this,role,options.name)}
 locator(selector){return new Locator(this,selector.includes('article')?'article':'pane')}
 async evaluate(_fn,key){assert.equal(key,'jpack.home-draft:'+this.c.projectRoot);const chat=this.chat(false);delete chat.aiConnection;delete chat.aiConnectionName;chat.model='';this.mutateDraft?.(chat);return JSON.stringify(chat)}
 async goto(url){if(url===origin){this.log.push('observe-home');this.emit('request',this.requestOf('/api/conversations'));return}this.log.push('reload-'+this.n);assert.equal(url,origin+'/chats/chat-'+this.n);if(this.onReload)this.onReload()}
 chat(saved=true){return {id:'chat-'+this.n,aiConnection:this.c.connectionId,aiConnectionName:this.c.connectionName,model:this.c.model,reasoning:{model:this.c.model,effort:this.c.effort},mode:'draft',researchMode:'provided',...(this.c.role==='candidate'?{critic:'off',interaction:'work'}:{}),composer:'',...(saved?{checkpoint:{state:{status:this.c.role==='candidate'?'ready':'complete',...(this.c.role==='candidate'?{phase:'conversation'}:{}),turns:[{id:'user-'+this.n,role:'user',kind:'message',text:this.prompt},{id:'answer-'+this.n+'-0',role:'assistant',kind:'message',text:'Saved '+this.n+'/0'},{id:'answer-'+this.n+'-1',role:'assistant',kind:'message',text:'Saved '+this.n+'/1'}]}}}:{})}}
 run(){return {id:'arun_'+String(this.n).repeat(32),owner:{id:'chat-'+this.n},state:'complete',checkpoint:{status:'complete',phase:'work'},chatProjection:{acknowledged:true},operations:[{id:'operation-'+this.n,kind:'model',state:'completed'}],events:this.n===1?[]:[{id:'call-event',event:{type:'tool_call',callId:'call-two',name:'get_schema',args:{}}},{id:'result-event',event:{type:'tool_result',callId:'call-two',name:'get_schema',text:'{"schema":"recorded"}',structured:{schema:'recorded'},isError:false}}]}}
 requestOf(path,method='GET'){return {url:()=>origin+path,method:()=>method,headerValue:async()=> 'PRIVATE-BEARER-DO-NOT-RETAIN',postData:()=>JSON.stringify({purpose:'chat',ownerId:'chat-'+this.n})}}
 dispatch(){
  assert.equal(this.owner.admitted,this.n);assert.equal(this.owner.chat,'chat-'+this.n);assert.equal(this.prompt,this.c.inputs[this.n-1].prompt);this.log.push('dispatch-'+this.n);this.sent=true
  if(this.c.role==='candidate'){
   const request=this.requestOf('/api/assistant-runs','POST');this.emit('request',request);this.emit('response',{request:()=>request,url:request.url,status:()=>201,text:async()=>JSON.stringify({id:'arun_'+String(this.n).repeat(32)})})
  }else{
   const ws=new EventEmitter();ws.url=()=>origin.replace('http:','ws:')+'/api/agent/run';this.emit('websocket',ws)
   ws.emit('framesent',{payload:'{"type":"start","request":{"secret":"MUST-NOT-PARSE"}}'})
   ws.emit('framereceived',{payload:JSON.stringify({type:'started',runId:String(this.n).repeat(48)})})
   if(this.n===2){const runtime=new EventEmitter();runtime.url=()=>origin.replace('http:','ws:')+'/ws';this.emit('websocket',runtime);runtime.emit('framesent',{payload:'{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"get_schema","arguments":{}}}'});runtime.emit('framereceived',{payload:'{"jsonrpc":"2.0","id":9,"result":{"content":[{"type":"text","text":"recorded-runtime"}],"structuredContent":{"schema":"recorded"},"isError":false}}'})}
   ws.emit('framereceived',{payload:JSON.stringify({type:'end',runId:String(this.n).repeat(48),error:''})})
  }
  this.owner.report(this.n);this.afterDispatch?.()
 }
}
// Deliberately supplied event/control double, not a second implementation of the
// qualified baseline reader. Actual reader parity remains a separate qualification.
class CaptureDouble {
 constructor(){this.tools=[];this.runtimeTools=[];this.terminal=false;this.starts=0}
 native(direction,v){if(v.type==='start')this.starts++;if(v.type==='started')this.runId=v.runId;if(v.type==='end')this.terminal=true}
 runtime(direction,_socket,v){if(direction==='received'){this.tools=[{callId:'native-call',name:'desk_runtime_0',args:{},text:'framed native answer',structuredState:'unavailable-native-contract',isError:false}];this.runtimeTools=[{rpcId:v.id,text:'recorded-runtime',structured:{schema:'recorded'}}]}}
 finish(want){assert.equal(this.starts,1);assert.equal(this.tools.length,want);assert.equal(this.runtimeTools.length,want);return {tools:this.tools,runtimeTools:this.runtimeTools}}
}
function setup(role='candidate'){const c=control(role),log=[],owner=new Owner(c,log),page=new Page(c,owner,log),saved=[];return {c,log,owner,page,saved,args:{control:c,page,ownerIO:owner,baselineModule:{Capture:CaptureDouble},retainCanonical:value=>canonicalReference(value),retain:async value=>{saved.push(structuredClone(value))}}}}

test('closed explicit input requires model, effort, role, exact fixed corpus, identities and owner limits',()=>{
 const c=control();validateLiveControl(c);assert.deepEqual(validateDescriptor(descriptor(),c),{origin,caseMs:120000,aggregateMs:480000})
 for(const mutate of [v=>delete v.model,v=>delete v.effort,v=>v.role='offline',v=>v.inputs.reverse(),v=>v.inputs[0].prompt='changed',v=>v.inputs[0].sliceSha256=H,v=>v.source.cli=null,v=>v.authorized=true]){const v=structuredClone(c);mutate(v);assert.throws(()=>validateLiveControl(v))}
 for(const mutate of [v=>v.configurationSha256='b'.repeat(64),v=>v.caseTimeoutMs=60000,v=>v.baseUrl+='/?token=private',v=>v.execution='granted']){const v=descriptor();mutate(v);assert.throws(()=>validateDescriptor(v,c))}
})
for(const role of ['candidate','baseline'])test(role+' keeps sequential chats, full ordered answers, deltas, sequence2 and final lifecycle separate',async()=>{
 const f=setup(role),result=await captureLiveSession(f.args)
 assert.equal(result.state,'complete');assert.deepEqual(result.cases[0].savedChat,{status:role==='candidate'?'ready':'complete',phase:role==='candidate'?{state:'value',value:'conversation'}:{state:'absent',value:null}});assert.deepEqual(result.cases[0].draftSelection,{aiConnection:{state:'absent',value:null},aiConnectionName:{state:'absent',value:null},model:{state:'value',value:''}});assert.deepEqual(result.cases.map(v=>v.accounting.invocation),[1,2]);assert.deepEqual(result.cases.map(v=>v.chatId),['chat-1','chat-2'])
 assert.deepEqual(result.cases[1].answers,[{id:'answer-2-0',text:'Saved 2/0',renderedText:'Rendered 2/0',reloadedText:'Rendered 2/0'},{id:'answer-2-1',text:'Saved 2/1',renderedText:'Rendered 2/1',reloadedText:'Rendered 2/1'}])
 assert.deepEqual(f.log,['observe-home','001.json','dispatch-1','reload-1','002.json','fresh-chat','003.json','dispatch-2','reload-2','004.json','context-close'])
 assert.equal(f.saved[0].cases[0].normalOwnerShutdown,null);assert.equal(f.saved[0].lifecycle,null);assert.equal(f.saved[1].cases[0].normalOwnerShutdown,null);assert.equal(result.cases[0].normalOwnerShutdown,true)
 assert.deepEqual(result.cases.map(v=>v.counts.callbacks),[0,1]);assert.equal(result.answerChangeDecision,'pending');assert.equal(result.monetaryCost,null)
 assert.equal(result.cases[0].accounting.last_observed.last.input_tokens.state,'absent');assert.equal(result.cases[0].accounting.acknowledged_effort.state,'absent')
 if(role==='baseline'){assert.equal(result.cases[0].settings.phaseInput.state,'absent');assert.equal(result.cases[0].settings.criticInput.state,'absent');assert.equal(result.cases[1].evidence.canonical,'unavailable-baseline-contract');assert.equal(result.cases[1].evidence.tools[0].structuredState,'unavailable-native-contract')}
 const raw=JSON.stringify(f.saved);for(const secret of ['PRIVATE-SETUP','PRIVATE-BEARER','MUST-NOT-PARSE'])assert(!raw.includes(secret))
})
test('accounting anomaly aborts whole session and skips case2, preserving actual report missingness',async()=>{
 const f=setup();f.owner.mutateAccounting=a=>{a.usage_present=false;a.usage_schema_valid=false}
 await assert.rejects(captureLiveSession(f.args),{code:'accounting'})
 assert.equal(f.owner.aborted,true);assert.equal(f.page.n,1);assert.equal(f.page.closed,1);assert(!f.log.includes('002.json'));assert.equal(f.saved.at(-1).state,'incomplete')
})
test('reload starts, callback anomalies, owner refusal and browser errors invalidate case1',async()=>{
 for(const mode of ['reload','callbacks','owner','browser']){
  const f=setup()
  if(mode==='reload')f.page.onReload=()=>f.page.dispatch()
  if(mode==='callbacks'){const run=f.page.run.bind(f.page);f.page.run=()=>({...run(),events:[{id:'extra',event:{type:'tool_call',callId:'extra',name:'get_schema',args:{}}}]})}
  if(mode==='owner')f.owner.report=function(){this.files.set('lifecycle.json',{reason:'case_deadline'})}
  if(mode==='browser')f.page.afterDispatch=()=>f.page.emit('pageerror',new Error('PRIVATE-RAW-ERROR'))
  await assert.rejects(captureLiveSession(f.args));assert(f.owner.aborted);assert.equal(f.page.n,1);assert(!f.log.includes('003.json'));assert(!JSON.stringify(f.saved).includes('PRIVATE-RAW-ERROR'))
 }
})
test('missing explicit selection and unready page never dispatch; settings and saved raw omission remain exact',async()=>{
 const f=setup();delete f.c.effort;await assert.rejects(captureLiveSession(f.args));assert(!f.log.includes('dispatch-1'))
 const g=setup();g.page.url=()=> 'about:blank';await assert.rejects(captureLiveSession(g.args),{code:'readiness'});assert(!g.log.includes('001.json'))
 const c=control('baseline'),chat=new Page(c,null,[]).chat(false);assert.equal(settingsOf(chat,c).phaseInput.state,'absent');assert.throws(()=>settingsOf({...chat,interaction:'plan'},c));assert.throws(()=>settingsOf({...chat,aiConnection:'other'},c))
})
test('accounting selection, terminal, sequence, reaping and finality are completion criteria',()=>{
 const c=control();checkAccounting(accounting(2),2,c)
 for(const mutate of [v=>v.invocation=1,v=>v.owned_client_reaped=false,v=>v.requested_model='substituted',v=>v.reroute_observed=true,v=>v.terminal_usage_finality='proven',v=>v.acknowledged_effort={state:'value',value:'high'},v=>v.secret='private']){const v=accounting(2);mutate(v);assert.throws(()=>checkAccounting(v,2,c))}
})
test('private numbered controls publish atomically once; only absence is pending',()=>{
 const dir=mkdtempSync(join(tmpdir(),'live-browser-capture-')),io=privateOwnerIO(dir)
 assert.equal(io.read('001-ack.json'),null);io.publish('001.json',{action:'begin',case:1,chatId:'chat-one'});assert.deepEqual(io.read('001.json'),{action:'begin',case:1,chatId:'chat-one'});assert.throws(()=>io.publish('001.json',{action:'abort'}));assert.throws(()=>io.publish('005.json',{}))
 const ack=join(dir,'001-ack.json');writeFileSync(ack,'{"action":"begin","action":"abort"}',{mode:0o600});assert.throws(()=>io.read('001-ack.json'))
 writeFileSync(ack,'{}');chmodSync(ack,0o644);assert.throws(()=>io.read('001-ack.json'));assert.equal(readFileSync(join(dir,'001.json'),'utf8').includes('begin'),true)
 const retain=privateRetainer(dir);retain({state:'complete',cases:[]});assert.throws(()=>retain({state:'complete',cases:[]}));assert.throws(()=>retain({state:'incomplete',text:'x'.repeat(65537)}))
})
test('expired case1 or case2 stops the session without later admission',async()=>{
 for(const index of [1,2]){
  const f=setup(),original=Date.now
  f.page.afterDispatch=()=>{if(f.page.n===index){const now=original();Date.now=()=>now+120001}}
  try{await assert.rejects(captureLiveSession(f.args),{code:'deadline'});assert(f.owner.aborted);assert.equal(f.page.n,index);assert.equal(f.owner.admitted,index);assert(!f.log.includes(index===1?'003.json':'004.json'))}finally{Date.now=original}
 }
})
test('complete run waits for its separately committed chat projection acknowledgment',async()=>{
 const f=setup(),run=f.page.run.bind(f.page);let reads=0
 f.page.run=()=>{const value=run();if(++reads===1)value.chatProjection.acknowledged=false;return value}
 const result=await captureLiveSession(f.args)
 assert.equal(result.state,'complete');assert(reads>=5);assert.equal(f.owner.aborted,undefined)
})

test('capture settings observations are closed bounded and finite',async()=>{
 const row={kind:'capture-settings-observation',case:1,step:'model-choices',state:'after',count:2,equal:true}
 validateSettingsObservation(row)
 for(const extra of [{secret:'PRIVATE'},{count:129},{count:-1},{count:1.5},{equal:'yes'},{reasoning:'PRIVATE'},{state:'unknown'},{step:'x'.repeat(2000)},{case:3},{draft:{secret:'PRIVATE'}}])assert.throws(()=>validateSettingsObservation({...row,...extra}))
 const c=control(),chat=new Page(c,null,[]).chat(false)
 const expected={connectionEqual:true,connectionNameEqual:true,modelEqual:true,reasoningModelEqual:true,effortEqual:true,mode:'draft',research:'provided',critic:'off',interaction:'work'}
 assert.deepEqual(draftSettingsObservation(chat,c),expected)
 const draft={kind:'capture-settings-observation',case:1,step:'draft-settings',state:'after',draft:expected}
 validateSettingsObservation(draft)
 for(const extra of [{secret:'PRIVATE'},{mode:'PRIVATE'},{effortEqual:'medium'},{critic:'x'.repeat(2000)}])assert.throws(()=>validateSettingsObservation({...draft,draft:{...expected,...extra}}))
 assert.deepEqual(draftSettingsObservation({mode:'PRIVATE',researchMode:'PRIVATE',critic:'PRIVATE',interaction:'PRIVATE'},c),{connectionEqual:false,connectionNameEqual:false,modelEqual:false,reasoningModelEqual:false,effortEqual:false,mode:'other',research:'other',critic:'other',interaction:'other'})
 const rows=[],observe=settingsObserver(v=>rows.push(v),1)
 await assert.rejects(observe('model-open',async()=>{throw Error('PRIVATE RAW ERROR')}))
 assert.deepEqual(rows,[{kind:'capture-settings-observation',case:1,step:'model-open',state:'before'},{kind:'capture-settings-observation',case:1,step:'model-open',state:'failed'}])
 assert(!JSON.stringify(rows).includes('PRIVATE'))
})
test('capture settings named steps preserve exact checks and identify first failure',async()=>{
 const f=setup(),rows=[];f.args.observeSettings=v=>rows.push(v)
 const result=await captureLiveSession(f.args);assert.equal(result.state,'complete')
 for(const index of [1,2]){
  const after=rows.filter(v=>v.case===index&&v.state==='after')
  assert.deepEqual(after.map(v=>v.step),['composer','task-menu','task-chat','model-trigger','model-open','model-choices','model-select','model-checked','reasoning-home','reasoning-select','model-close','settings-open','research-menu','research-select','critic-select','tools-check','settings-close','draft-read','draft-settings'])
  assert.deepEqual(after.find(v=>v.step==='reasoning-select'),{kind:'capture-settings-observation',case:index,step:'reasoning-select',state:'after',reasoning:'Medium',equal:true})
  assert.equal(after.find(v=>v.step==='model-choices').equal,true);assert.equal(after.find(v=>v.step==='tools-check').equal,true)
 }
 const g=setup(),failed=[];g.args.observeSettings=v=>failed.push(v)
 const original=g.page.getByRole.bind(g.page)
 g.page.getByRole=(role,options)=>{const loc=original(role,options);if(options.name==='Adversarial review')loc.selectOption=async()=>{throw Error('PRIVATE RAW ERROR')};return loc}
 await assert.rejects(captureLiveSession(g.args),{code:'settings'})
 assert.deepEqual(failed.at(-1),{kind:'capture-settings-observation',case:1,step:'critic-select',state:'failed'})
 assert.equal(g.owner.admitted,0);assert(!JSON.stringify(failed).includes('PRIVATE'))
})


test('draft producer seam accepts inherited and explicit selection with raw missingness retained',async()=>{
 for(const explicit of [false,true]){
  const f=setup()
  if(explicit)f.page.mutateDraft=chat=>Object.assign(chat,{aiConnection:f.c.connectionId,aiConnectionName:f.c.connectionName,model:f.c.model})
  const result=await captureLiveSession(f.args)
  assert.equal(result.state,'complete')
  assert.deepEqual(result.cases[0].draftSelection.model,{state:'value',value:explicit?f.c.model:''})
  assert.equal(result.cases[0].settings.connectionId,f.c.connectionId)
 }
 const c=control(),chat=new Page(c,null,[]).chat(false),selection={checked:true,value:JSON.stringify([c.connectionId,c.model])}
 for(const key of ['aiConnection','aiConnectionName','model']){
  const inherited=structuredClone(chat);delete inherited[key]
  assert.deepEqual(draftSettingsOf(inherited,c,selection).raw[key],{state:'absent',value:null})
  inherited[key]='';assert.deepEqual(draftSettingsOf(inherited,c,selection).raw[key],{state:'value',value:''})
 }
})
test('conflicting drafts, unchecked or unverified radio and missing reasoning refuse before admission',async()=>{
 const mutations=[...['aiConnection','aiConnectionName','model'].map(key=>chat=>{chat[key]='conflict'}),chat=>{delete chat.reasoning},chat=>{chat.reasoning.effort='high'},chat=>{chat.reasoning.model='other'},chat=>{chat.interaction='plan'}]
 for(const mutate of mutations){
  const f=setup();f.page.mutateDraft=mutate
  await assert.rejects(captureLiveSession(f.args),{code:'settings'});assert.equal(f.owner.admitted,0)
 }
 for(const mode of ['unchecked','wrong-value']){
  const f=setup(),original=f.page.getByRole.bind(f.page)
  f.page.getByRole=(role,options)=>{const loc=original(role,options);if(role==='radio'){loc.nth=()=>loc;loc.isChecked=async()=>false;if(mode==='wrong-value')loc.getAttribute=async()=>JSON.stringify(['other',f.c.model])}return loc}
  await assert.rejects(captureLiveSession(f.args),{code:'settings'});assert.equal(f.owner.admitted,0)
 }
 const c=control(),chat=new Page(c,null,[]).chat(false)
 for(const selection of [undefined,{checked:false,value:JSON.stringify([c.connectionId,c.model])},{checked:true,value:JSON.stringify(['other',c.model])}])assert.throws(()=>draftSettingsOf(chat,c,selection),{code:'settings'})
})
test('normal submission and reload still require exact explicitly saved connection name and model',async()=>{
 for(const at of ['saved','reload'])for(const key of ['aiConnection','aiConnectionName','model'])for(const value of [undefined,'','conflict']){
  const f=setup(),original=f.page.chat.bind(f.page);let reloaded=false
  f.page.onReload=()=>{reloaded=true}
  f.page.chat=(saved=true)=>{const chat=original(saved);if(saved&&f.page.sent&&(at==='saved'||reloaded)){if(value===undefined)delete chat[key];else chat[key]=value}return chat}
  await assert.rejects(captureLiveSession(f.args),{code:'settings'});assert.equal(f.owner.admitted,1);assert(!f.log.includes('003.json'))
 }
})


test('terminal categories are closed, bounded, and preserve unknown and absent facts',()=>{
 const f=setup(),facts=terminalObservation(f.page.run(),'wrong','wrong')
 const row={kind:'capture-terminal-observation',case:1,branch:'read',caseElapsedMs:0,terminalElapsedMs:480000,...facts}
 validateTerminalObservation(row)
 for(const extra of [{secret:'PRIVATE'},{case:3},{branch:'unknown'},{caseElapsedMs:-1},{terminalElapsedMs:480001},{caseElapsedMs:1.5},{runState:'PRIVATE'},{checkpointStatus:'PRIVATE'},{phase:'PRIVATE'},{runIdEqual:1},{ownerIdEqual:null},{projectionAck:true}])assert.throws(()=>validateTerminalObservation({...row,...extra}))
 assert.deepEqual(terminalObservation({state:'PRIVATE',checkpoint:{status:null,phase:'PRIVATE'},chatProjection:{acknowledged:1}},'id','chat'),{runState:'other',checkpointStatus:'other',phase:'other',runIdEqual:false,ownerIdEqual:false,projectionAck:'other'})
 assert.deepEqual(terminalObservation({},'id','chat'),{runState:'absent',checkpointStatus:'absent',phase:'absent',runIdEqual:false,ownerIdEqual:false,projectionAck:'absent'})
 const rows=[],observe=terminalObserver(v=>rows.push(v),1,Date.now()-999999,Date.now()+1000)
 for(let i=0;i<300;i++)observe('read',{state:i%2?'running':'complete'},'id','chat')
 observe('terminal-checkpoint',{},'id','chat');observe('read',{},'id','chat')
 assert.equal(rows.length,128);assert.equal(rows.at(-1).branch,'terminal-checkpoint');assert.equal(rows[0].caseElapsedMs,480000);assert.equal(rows[0].terminalElapsedMs,0)
})
test('actual terminal refusal branches retain the exact failing read without payloads',async()=>{
 for(const state of ['failed','stopped','interrupted','awaiting-input','awaiting-client','paused-budget']){
  const f=setup(),rows=[],original=f.page.run.bind(f.page);f.args.observeTerminal=v=>rows.push(v)
  f.page.run=()=>({...original(),state,private:'PRIVATE RAW ERROR'})
  await assert.rejects(captureLiveSession(f.args),{code:'terminal'})
  assert.equal(rows.at(-1).branch,'terminal-state');assert.equal(rows.at(-1).runState,state);assert.equal(rows.at(-1).runIdEqual,true);assert.equal(rows.at(-1).ownerIdEqual,true);assert(!JSON.stringify(rows).includes('PRIVATE'))
 }
 for(const checkpoint of [{status:'running',phase:'work'},{status:'complete',phase:'plan'},undefined]){
  const f=setup(),rows=[],original=f.page.run.bind(f.page);f.args.observeTerminal=v=>rows.push(v);f.page.run=()=>({...original(),checkpoint})
  await assert.rejects(captureLiveSession(f.args),{code:'terminal'});assert.equal(rows.at(-1).branch,'terminal-checkpoint');assert.equal(rows.at(-1).checkpointStatus,checkpoint?.status??'absent');assert.equal(rows.at(-1).phase,checkpoint?.phase??'absent')
 }
 // An owner/id mismatch is still refused by the earlier unchanged evidence guard.
 for(const field of ['owner','id']){
  const f=setup(),rows=[],original=f.page.run.bind(f.page);f.args.observeTerminal=v=>rows.push(v);f.page.run=()=>({...original(),[field]:field==='owner'?{id:'other'}:'other'})
  await assert.rejects(captureLiveSession(f.args),{code:'evidence'});assert.equal(rows.at(-1).branch,'read');assert.equal(rows.at(-1)[field==='owner'?'ownerIdEqual':'runIdEqual'],false)
 }
})
test('terminal observer retains waiting states and projection absence/false before acceptance',async()=>{
 const f=setup(),rows=[],original=f.page.run.bind(f.page);let n=0;f.args.observeTerminal=v=>rows.push(v)
 f.page.run=()=>{const run=original();if(++n===1)run.state='running';if(n===2)delete run.chatProjection;if(n===3)run.chatProjection.acknowledged=false;return run}
 assert.equal((await captureLiveSession(f.args)).state,'complete')
 assert.deepEqual(rows.filter(v=>v.case===1).map(v=>[v.branch,v.runState,v.projectionAck]),[['read','running','true'],['read','complete','absent'],['read','complete','false'],['read','complete','true'],['accepted','complete','true']])
 assert.equal(rows.at(-1).branch,'accepted')
})


test('application snapshots reuse duplicate depth and node guards with a separate 4 MiB domain',()=>{
 const text=JSON.stringify({checkpoint:{text:'x'.repeat(70000)}})
 assert.equal(parseApplicationSnapshot(text).checkpoint.text.length,70000);assert.throws(()=>parseClosed(text))
 for(const raw of ['{"a":1,"a":2}',JSON.stringify('x'.repeat(4*1024*1024)), '['.repeat(18)+'0'+']'.repeat(18),JSON.stringify(Array(8193).fill(0))])assert.throws(()=>parseApplicationSnapshot(raw))
 const dir=mkdtempSync(join(tmpdir(),'capture-default-bounds-')),path=join(dir,'record.json');writeFileSync(path,text,{mode:0o600})
 assert.throws(()=>readPrivate(path));assert.throws(()=>writePrivate(path,{text:'x'.repeat(70000)}))
 const c=control();c.connectionName='x'.repeat(70000);assert.throws(()=>validateLiveControl(c))
})
test('canonical private artifacts bind exact untruncated bytes and case identity',()=>{
 const dir=mkdtempSync(join(tmpdir(),'capture-canonical-')),retain=privateCanonicalRetainer(dir)
 const binding={role:'candidate',case:1,caseId:fixedInputs[0].id,chatId:'chat-1',runId:'arun_111'}
 const v={kind:'capture-canonical-evidence',binding,events:[{text:'event'}],operations:[{intent:'x'.repeat(70000)}]},ref=retain(v)
 assert.deepEqual(readCanonicalEvidence(dir,ref,binding),v);assert.equal(ref.bytes,Buffer.byteLength(readFileSync(join(dir,ref.file),'utf8')))
 assert.throws(()=>retain(v));assert.throws(()=>readCanonicalEvidence(dir,{...ref,sha256:H},binding));assert.throws(()=>readCanonicalEvidence(dir,{...ref,bytes:ref.bytes-1},binding));assert.throws(()=>readCanonicalEvidence(dir,ref,{...binding,case:2}));assert.throws(()=>readCanonicalEvidence(dir,ref,{...binding,chatId:'other'}))
 chmodSync(join(dir,ref.file),0o644);assert.throws(()=>readCanonicalEvidence(dir,ref,binding))
 const other=mkdtempSync(join(tmpdir(),'capture-canonical-link-'));symlinkSync(join(dir,ref.file),join(other,ref.file));assert.throws(()=>privateCanonicalRetainer(other)(v));assert.throws(()=>readCanonicalEvidence(other,ref,binding))
})
test('capture producer keeps large canonical receipts outside bounded summary and preserves full answers',async()=>{
 const f=setup(),dir=mkdtempSync(join(tmpdir(),'capture-large-session-')),original=f.page.run.bind(f.page)
 f.page.run=()=>{const run=original();run.operations[0].intent='x'.repeat(100000);return run}
 f.args.retainCanonical=privateCanonicalRetainer(dir)
 f.args.retain=value=>{parseClosed(JSON.stringify(value));f.saved.push(structuredClone(value))}
 const result=await captureLiveSession(f.args);assert.equal(result.state,'complete');assert(Buffer.byteLength(JSON.stringify(result))<65536)
 for(const row of result.cases){const ref=row.evidence.canonical;assert.equal(readCanonicalEvidence(dir,ref,ref.binding).operations[0].intent.length,100000);assert.equal(row.answers.length,2);assert.equal(row.answers[1].text,'Saved '+ref.binding.case+'/1')}
 const g=setup(),chat=g.page.chat.bind(g.page);g.page.chat=(saved=true)=>{const value=chat(saved);if(saved)value.checkpoint.state.turns[1].text='x'.repeat(70000);return value}
 await assert.rejects(captureLiveSession(g.args),{code:'evidence'});assert.equal(g.owner.admitted,1)
})


test('saved completion keeps role-specific schemas and waits for incomplete or missing states',()=>{
 const message={role:'assistant',kind:'message',text:'Handwritten answer'}
 for(const role of ['candidate','baseline']){
  const expected=role==='candidate'?'ready':'complete'
  assert.equal(savedChatComplete({checkpoint:{state:{status:expected,turns:[message]}}},role),true)
  for(const status of ['ready','complete','failed','stopped','running',undefined].filter(s=>s!==expected))assert.equal(savedChatComplete({checkpoint:{state:{status,turns:[message]}}},role),false)
  for(const chat of [undefined,{}, {checkpoint:{}},{checkpoint:{state:{status:expected}}},{checkpoint:{state:{status:expected,turns:[]}}},{checkpoint:{state:{status:expected,turns:[{role:'user',kind:'message'}]}}}])assert.equal(savedChatComplete(chat,role),false)
 }
 assert.throws(()=>savedChatComplete({},'unknown'),{code:'control'})
})
test('saved ready never bypasses canonical completion or acknowledgment and missing chats wait',async()=>{
 const f=setup(),original=f.page.run.bind(f.page),chat=f.page.chat.bind(f.page);let runs=0,savedReads=0
 f.page.run=()=>{const run=original();runs++;if(runs===1)run.state='running';if(runs===2)run.chatProjection.acknowledged=false;return run}
 f.page.chat=(saved=true)=>{const value=chat(saved);if(saved){assert(runs>=3);if(++savedReads===1)return {id:value.id};if(savedReads===2)value.checkpoint.state.status='running'}return value}
 const result=await captureLiveSession(f.args);assert.equal(result.state,'complete');assert(savedReads>=6)
 for(const status of ['complete','failed','stopped',undefined]){
  const g=setup(),get=g.page.chat.bind(g.page);let reads=0
  g.page.chat=(saved=true)=>{const value=get(saved);if(saved){value.checkpoint.state.status=status;if(++reads===1)g.owner.files.set('lifecycle.json',{reason:'case_deadline'})}return value}
  await assert.rejects(captureLiveSession(g.args),{code:'owner'});assert.equal(g.owner.admitted,1);assert(!g.log.includes('reload-1'))
 }
})
test('reload preserves saved completion status and raw phase',async()=>{
 for(const field of ['status','phase']){
  const f=setup(),chat=f.page.chat.bind(f.page);let reloaded=false
  f.page.onReload=()=>{reloaded=true};f.page.chat=(saved=true)=>{const value=chat(saved);if(saved&&reloaded)value.checkpoint.state[field]=field==='status'?'running':'plan';return value}
  await assert.rejects(captureLiveSession(f.args),{code:'reload'});assert.equal(f.owner.admitted,1)
 }
})

test('rendered answers wait through asynchronous transient reconciliation for the complete exact ordered set and chat',async()=>{
 const f=setup(),rows=[];f.args.observeRendered=v=>rows.push(v)
 let reads=0,reloaded=false
 f.page.onReload=()=>{reloaded=true;reads=0};f.page.afterDispatch=()=>{reads=0}
 f.page.renderSnapshot=async()=>{await Promise.resolve();reads++;return {chatId:'chat-'+f.page.n,ids:reads===1?['streaming']:reads===2?['answer-'+f.page.n+'-0']:['answer-'+f.page.n+'-0','answer-'+f.page.n+'-1']}}
 const result=await captureLiveSession(f.args)
 assert(reloaded);assert.equal(result.cases.length,2)
 for(const index of [1,2])for(const step of ['answers','reload']){
  const seen=rows.filter(v=>v.case===index&&v.step===step)
  assert.deepEqual(seen.map(v=>[v.actualCount,v.sameIds,v.sameOrder,v.accepted]),[[1,false,false,false],[2,true,true,true]])
 }
 assert.deepEqual(result.cases[0].answers,[{id:'answer-1-0',text:'Saved 1/0',renderedText:'Rendered 1/0',reloadedText:'Rendered 1/0'},{id:'answer-1-1',text:'Saved 1/1',renderedText:'Rendered 1/1',reloadedText:'Rendered 1/1'}])
})
test('permanent extra missing wrong reordered IDs or owner exhaust original deadline in answers and reload without extra invocation',async()=>{
 for(const step of ['answers','reload'])for(const defect of ['extra','missing','wrong','order','owner']){
  const f=setup(),rows=[],original=Date.now;let reloaded=false
  f.args.observeRendered=v=>rows.push(v);f.page.onReload=()=>{reloaded=true}
  f.page.renderSnapshot=()=>{
   const ids=['answer-1-0','answer-1-1'];let chatId='chat-1'
   if(step==='answers'||reloaded){if(defect==='extra')ids.push('extra');if(defect==='missing')ids.pop();if(defect==='wrong')ids[0]='wrong';if(defect==='order')ids.reverse();if(defect==='owner')chatId='other';const now=original();Date.now=()=>now+120001}
   return {chatId,ids}
  }
  try{await assert.rejects(captureLiveSession(f.args),{code:'deadline'})}finally{Date.now=original}
  assert.equal(f.page.n,1);assert.equal(f.owner.admitted,1);assert(!f.log.includes('003.json'));assert(f.owner.aborted)
  const row=rows.at(-1);assert.equal(row.step,step);assert.equal(row.accepted,false);assert.equal(row.sameChat,defect!=='owner');assert.equal(row.sameIds,['order','owner'].includes(defect));assert.equal(row.sameOrder,defect==='owner')
 }
})
test('render observations refuse unbounded counts categories fields and inconsistent acceptance',()=>{
 const value={kind:'capture-rendered-observation',case:1,step:'answers',caseElapsedMs:0,expectedCount:2,actualCount:1,sameChat:true,sameIds:false,sameOrder:false,accepted:false}
 assert.deepEqual(validateRenderedObservation(value),value)
 for(const mutate of [v=>v.actualCount=8194,v=>v.step='PRIVATE',v=>v.raw='PRIVATE',v=>v.accepted=true,v=>v.sameOrder=true]){const bad={...value};mutate(bad);assert.throws(()=>validateRenderedObservation(bad))}
})

// These response doubles carry private sentinels deliberately; no response bytes
// or caught error prose may become the diagnostic evidence.
function dispatchResponse(page,make) {
 const emit=page.emit.bind(page)
 page.emit=(event,value)=>{
  if(event!=='response')return emit(event,value)
  for(const response of make(value))emit(event,response)
  return true
 }
}
function assertPrivateDispatchEvidence(f,category,status,counts={requestCount:1,responseCount:1}) {
 const evidence=f.saved.at(-1)
 assert.equal(evidence.state,'incomplete')
 assert.deepEqual(evidence.dispatchDiagnostic,{category,status,...counts,countsCapped:false})
 const raw=JSON.stringify(f.saved)
 for(const secret of ['PRIVATE-DISPATCH','PRIVATE-SETUP','PRIVATE-BEARER','password','cookie','checkpoint-secret','https://private.invalid'])assert(!raw.includes(secret))
 assert.equal(f.owner.aborted,true);assert.equal(f.page.n,1);assert.equal(f.page.closed,1)
 assert(!f.log.includes('003.json'))
}
test('dispatch failures retain only categorical response diagnostics and bounded multiplicity',async()=>{
 const secret='PRIVATE-DISPATCH password cookie checkpoint-secret https://private.invalid'
 const valid=JSON.stringify({id:'arun_'+'1'.repeat(32),private:secret})
 for(const [category,make,status] of [
  ['body-unavailable',r=>[{...r,text:async()=>{throw new Error(secret)}}],201],
  ['json-invalid',r=>[{...r,text:async()=>'{"id":"'+secret+'","id":0}'}],201],
  ['status',r=>[{...r,status:()=>200,text:async()=>valid}],200],
  ['invalid-run-id',r=>[{...r,text:async()=>JSON.stringify({id:secret})}],201],
  ['invalid-run-id',r=>[{...r,text:async()=>'{"id":{"toString":null}}'}],201],
  ['duplicate-response',r=>[{...r,text:async()=>valid},{...r,text:async()=>valid}],201]
 ]) {
  const f=setup();dispatchResponse(f.page,make)
  await assert.rejects(captureLiveSession(f.args),{code:'dispatch'})
  assertPrivateDispatchEvidence(f,category,status,{requestCount:1,responseCount:category==='duplicate-response'?2:1})
 }
})
test('abort retains delayed response rejection and separates a still pending body without leaking later bytes',async()=>{
 for(const pending of [false,true]) {
  const f=setup();let resolveBody
  dispatchResponse(f.page,r=>[{...r,text:()=>pending?new Promise(resolve=>{resolveBody=resolve}):new Promise((_resolve,reject)=>setTimeout(()=>reject(new Error('PRIVATE-DISPATCH password')),0))}])
  f.page.afterDispatch=()=>{throw Object.assign(new Error('PRIVATE-DISPATCH'),{code:'terminal'})}
  await assert.rejects(captureLiveSession(f.args),{code:'terminal'})
  const row=f.saved.at(-1).dispatchDiagnostic
  assert.deepEqual(row,{category:'body-unavailable',status:201,...(pending?{bodyPending:true}:{}),requestCount:1,responseCount:1,countsCapped:false})
  if(pending){resolveBody('{"id":"PRIVATE-DISPATCH","password":"checkpoint-secret"}');await new Promise(resolve=>setImmediate(resolve));assert.deepEqual(f.saved.at(-1).dispatchDiagnostic,row)}
  const raw=JSON.stringify(f.saved);assert(!raw.includes('PRIVATE-DISPATCH'));assert(!raw.includes('checkpoint-secret'));assert.equal(f.page.closed,1)
 }
})
test('a duplicate POST is retained separately from duplicate response delivery',async()=>{
 const f=setup(),emit=f.page.emit.bind(f.page)
 f.page.emit=(event,value)=>{if(event==='request'&&value.method()==='POST')emit(event,value);return emit(event,value)}
 dispatchResponse(f.page,r=>[{...r,status:()=>200}])
 await assert.rejects(captureLiveSession(f.args))
 assertPrivateDispatchEvidence(f,'status',200,{requestCount:2,responseCount:1})
})
test('successful evidence has no dispatch diagnostic additions',async()=>{
 const f=setup();await captureLiveSession(f.args)
 assert(f.saved.every(row=>!Object.hasOwn(row,'dispatchDiagnostic')))
})
