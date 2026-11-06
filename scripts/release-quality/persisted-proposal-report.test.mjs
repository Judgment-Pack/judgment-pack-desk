import test from 'node:test'
import assert from 'node:assert/strict'
import {execFileSync,spawnSync} from 'node:child_process'
import {mkdtempSync,writeFileSync,readFileSync} from 'node:fs'
import {join} from 'node:path'
import {control,report,original,candidate,prompt,digest,diagnostic,stages,exceptionObservation} from './persisted-proposal-report.mjs'
const base=()=>({kind:'offline-persisted-proposal-browser',caseId:'api-save',state:'incomplete',phase:'bootstrap',errorName:null,runId:null,chatId:null,proposal:null,operations:null,freshChecks:null,settings:null,file:null,counts:null,ui:null,elapsedMs:1,diagnostic:null})
const check=()=>({documentDigest:digest(candidate),suiteKey:'authored-suite',at:'2026-10-25T00:00:00Z',valid:true,diagnostics:[],totalCases:1,report:{status:'passed',summary:{total:1,passed:1},packs:[{rows:[{id:'existing',expected:'proceed',actual:'proceed',status:'passed'}],coverage:[]}]}})
const complete=()=>({...base(),state:'complete',phase:'complete',runId:'arun_123abc',chatId:'chat-1',proposal:{text:candidate,digest:digest(candidate),basis:{packId:'one',path:'one.json',text:original},check:check()},operations:[{id:'host-1',callId:'call-1',kind:'host',name:'propose_saved_pack',state:'completed',intentSha256:'a'.repeat(64),resultSha256:'b'.repeat(64)}],freshChecks:[check()],settings:{model:'gpt-6-sol',effort:null,thinking:'off',critic:'off',research:'provided',mode:'draft',phase:'work',phaseInput:{state:'absent',value:null},criticInput:{state:'value',value:'off'}},file:{before:original,afterAction:candidate,final:candidate,reloaded:candidate,suiteBefore:'authored',suiteAfter:'authored'},counts:{starts:1,models:2,writes:1,checks:1,browserErrors:0,unexpected:0},ui:{applied:false,dirtyRefused:false,duplicateBlocked:true,reloadStable:true}})
test('closed control fixes corpus and refuses remote/auth-bearing origin or extra members',()=>{
 const c={kind:'offline-persisted-proposal-control',caseId:'api-save',origin:'http://127.0.0.1:3210',setupCode:'a'.repeat(24),browserBin:'/fixture/chrome',reportPath:'/fixture/report',suitePath:'/fixture/suite',original,candidate,prompt};assert.equal(control(c),c)
 for(const changed of [{origin:'https://example.com'},{origin:'http://user:pass@127.0.0.1:3210'},{prompt:'other'},{setupCode:'short'},{raw:'secret'}])assert.throws(()=>control({...c,...changed}))
})
test('complete main Save requires authored bytes, fresh checks, one write and no reload replay',()=>{
 assert.equal(report(complete()).state,'complete')
 for(const mutate of [r=>r.file.final=original,r=>r.file.suiteAfter='changed',r=>r.counts.writes=2,r=>r.counts.starts=2,r=>r.freshChecks=[],r=>r.ui.reloadStable=false,r=>r.proposal.check.valid=false,r=>r.operations[0].state='uncertain']){const r=complete();mutate(r);assert.throws(()=>report(r))}
})
test('native Apply records original disk before Save and supports separate dirty veto',()=>{
 const r=complete();r.caseId='native-apply-save';r.settings.effort='medium';r.settings.thinking=null;r.file.afterAction=original;r.counts.models=1;r.counts.checks=0;r.freshChecks=[];r.ui.applied=true;assert.equal(report(r).state,'complete')
 r.file.afterAction=candidate;assert.throws(()=>report(r));r.file.afterAction=original;r.caseId='native-dirty-refusal';r.file.final=original;r.file.reloaded=original;r.counts.writes=0;r.ui.applied=false;r.ui.dirtyRefused=true;assert.equal(report(r).state,'complete');r.counts.writes=1;assert.throws(()=>report(r))
})
test('incomplete report retains finite diagnostic only and never promotes missing evidence',()=>{
 const r={...base(),errorName:'TimeoutError',phase:'proposal'};assert.equal(report(r).state,'incomplete');assert.throws(()=>report({...r,state:'complete',phase:'complete'}));assert.throws(()=>report({...r,message:'raw browser prose'}));assert.throws(()=>report({...r,errorName:'raw URL'}))
})
test('authored candidate changes version alone with no ambiguous numeric literal',()=>{assert.equal(candidate,original.replace('"version":"0.1.0"','"version":"0.1.1"'));assert.deepEqual({...JSON.parse(candidate),version:'0.1.0'},JSON.parse(original))})
function fixture(){const root=mkdtempSync(join(process.env.TMPDIR,'persisted-proposal-fixture-'));const trace=join(root,'trace');const proposal=join(root,'candidate.json');writeFileSync(trace,'',{mode:0o600});writeFileSync(proposal,candidate,{mode:0o600});writeFileSync(join(root,'one.json'),original);return{root,trace,proposal}}
const peer=new URL('./persisted-proposal-peer.py',import.meta.url).pathname
function call(f,frames){return spawnSync('/usr/bin/python3',[peer,'runtime',f.root,f.trace,f.proposal,'mcp'],{input:frames.map(f=>JSON.stringify(f)).join('\n')+'\n',encoding:'utf8'})}
test('finite Runtime accepts exact authored matrix and rejects changed expectations or unknown calls',()=>{
 const f=fixture();const request={jsonrpc:'2.0',id:1,method:'tools/call',params:{name:'experimental_test_cases',arguments:{pack:candidate,matrix:JSON.stringify({matrixVersion:'3',cases:[{id:'existing',facts:{n:1},expectedDisposition:'proceed',focus:'Original requirement: proceed without changing policy.',origin:'manual'}]})}}};const ok=call(f,[request]);assert.equal(ok.status,0);assert.equal(JSON.parse(ok.stdout).result.structuredContent.summary.passed,1)
 const wrong=structuredClone(request);wrong.params.arguments.matrix=wrong.params.arguments.matrix.replace('proceed','hold');assert.equal(call(f,[wrong]).status,65);assert.equal(call(f,[{...request,params:{name:'unknown',arguments:{}}}]).status,65)
})
test('finite Runtime serves actual current file bytes and digest rather than a fixed candidate',()=>{const f=fixture();const q={jsonrpc:'2.0',id:2,method:'tools/call',params:{name:'get_pack',arguments:{pack_id:'one'}}};let got=call(f,[q]);assert.equal(got.status,0);assert.equal(JSON.parse(got.stdout).result.content[0].text,original);writeFileSync(join(f.root,'one.json'),candidate);got=call(f,[q]);assert.equal(JSON.parse(got.stdout).result.structuredContent.sha256,digest(candidate))})

test('incomplete model count stays unknown and complete count remains mandatory',()=>{
 const r={...base(),counts:{starts:1,models:null,writes:0,checks:0,browserErrors:0,unexpected:0}};assert.equal(report(r).counts.models,null)
 for(const value of ['unknown',-1,65])assert.throws(()=>report({...r,counts:{...r.counts,models:value}}))
 const done=complete();done.counts.models=null;assert.throws(()=>report(done));assert.throws(()=>report({...r,counts:{...r.counts,raw:'prose'}}))
})
test('finite Runtime closes matrix keys and retains allowlisted refusal identity only',()=>{
 const f=fixture();const exact={matrixVersion:'3',cases:[{id:'existing',facts:{n:1},expectedDisposition:'proceed',focus:'Original requirement: proceed without changing policy.',origin:'manual'}]}
 for(const mutate of [m=>delete m.cases[0].focus,m=>delete m.cases[0].origin,m=>m.matrixVersion='4',m=>m.extra='raw',m=>m.cases[0].extra='raw',m=>m.cases[0].facts.n=2]){const m=structuredClone(exact);mutate(m);assert.equal(call(f,[{id:1,method:'tools/call',params:{name:'experimental_test_cases',arguments:{pack:candidate,matrix:JSON.stringify(m)}}}]).status,65)}
 const rows=readFileSync(f.trace,'utf8').trim().split('\n').map(JSON.parse).filter(r=>r.kind==='refused');assert.equal(rows.length,6)
 for(const row of rows)assert.deepEqual(JSON.parse(row.tag),{method:'tools/call',name:'experimental_test_cases',reason:'matrix'})
 assert.equal(call(f,[{id:2,method:'private raw method',params:{name:'raw private name',arguments:{secret:'never retain'}}}]).status,65)
 const last=JSON.parse(readFileSync(f.trace,'utf8').trim().split('\n').at(-1));assert.deepEqual(JSON.parse(last.tag),{method:'other',name:'other',reason:'request'});assert(!readFileSync(f.trace,'utf8').includes('private'))
})

test('closed Save diagnostics admit authored finite observations and reject prose or unbounded values',()=>{
 const d={stages:Object.fromEntries(stages.map(k=>[k,null])),click:'returned',button:'enabled',messages:['generic'],requests:[{category:'file-read',status:200,count:3}],readOnly:'none',exceptions:[],stop:'none'};d.stages['click-return']=123;assert.equal(diagnostic(d),d)
 for(const mutate of [d=>d.raw='secret',d=>d.messages=['raw prose'],d=>d.readOnly='raw path',d=>d.click='force',d=>d.stages.action=-1,d=>d.requests[0].category='raw URL',d=>d.requests[0].status=0,d=>d.requests[0].count=4097,d=>d.requests[0].body='secret']){const bad=structuredClone(d);mutate(bad);assert.throws(()=>diagnostic(bad))}
 assert.equal(report({...base(),diagnostic:d}).diagnostic,d)
})

test('closed exception observation permits only fixed sources, classes and boolean guard facts',()=>{
 const v={atMs:21000,reason:'exception',exceptionClass:'RunRequestError',frames:[{script:'web/dist/assets/index-0KiWpcLq.js',function:'checkProposal',line:102,column:4300}],projection:'complete',guards:{snapshotExists:true,snapshotHistorical:false,ended:false,busy:false,stateIsRunning:false,packReview:true}};assert.equal(exceptionObservation(v),v)
 for(const mutate of [v=>v.description='raw exception',v=>v.exceptionClass='raw class',v=>v.frames[0].script='private URL',v=>v.frames[0].function='arbitrary',v=>v.frames[0].locals={},v=>v.guards.snapshotExists='raw',v=>v.guards.token='secret',v=>v.projection='none',v=>v.frames[0].column=-1]){const bad=structuredClone(v);mutate(bad);assert.throws(()=>exceptionObservation(bad))}
 const no={...v,guards:null,projection:'timed-out'};assert.equal(exceptionObservation(no),no)
})

test('intentional diagnostic stops stay distinct from acceptance and boolean facts stay primitive',()=>{
 const d={stages:Object.fromEntries(stages.map(k=>[k,null])),click:'returned',button:'enabled',messages:['generic'],requests:[{category:'discover',status:200,count:1}],readOnly:'none',exceptions:[],stop:'guard-observed'};assert.equal(diagnostic(d),d);assert.equal(report({...base(),phase:'action',diagnostic:d}).state,'incomplete')
 assert.throws(()=>diagnostic({...d,stop:'passed'}));const done=complete();done.diagnostic=d;assert.throws(()=>report(done))
 const v={atMs:21000,reason:'promiseRejection',exceptionClass:'Other',frames:[{script:'web/dist/assets/index-0KiWpcLq.js',function:'checkProposal',line:102,column:4347}],projection:'complete',guards:{snapshotExists:false,snapshotHistorical:false,ended:false,busy:false,stateIsRunning:false,packReview:true}};assert.equal(exceptionObservation(v),v)
 for(const mutate of [v=>v.guards.busy=null,v=>v.guards.stateIsRunning=0,v=>v.guards.packReview={},v=>v.guards.snapshotHistorical='false',v=>v.guards.candidateMatch=true]){const bad=structuredClone(v);mutate(bad);assert.throws(()=>exceptionObservation(bad))}
})

 test('finite native model list exposes the exact hidden catalog and selected visible model',()=>{
 const f=fixture();const frames=[{id:1,method:'model/list',params:{limit:100,includeHidden:true}},{id:2,method:'model/list',params:{}}]
 const got=spawnSync('/usr/bin/python3',[peer,'native',f.root,f.trace,f.proposal,'app-server','--stdio','--strict-config'],{input:frames.map(f=>JSON.stringify(f)).join('\n')+'\n',encoding:'utf8'});assert.equal(got.status,0)
 const rows=got.stdout.trim().split('\n').map(JSON.parse);assert.deepEqual(rows.map(r=>r.id),[1,2])
 const expected=names=>({data:names.map(model=>({model,displayName:model,defaultReasoningEffort:'medium',supportedReasoningEfforts:[{reasoningEffort:'low'},{reasoningEffort:'medium'},{reasoningEffort:'high'}]}))})
 assert.deepEqual(rows[0].result,expected(['gpt-6-astra','gpt-6-sol','gpt-6-luna','gpt-5.6-sol','gpt-5.6-terra','gpt-5.6-luna','gpt-5.5']))
 assert.deepEqual(rows[1].result,expected(['gpt-6-sol']))
})

test('finite native handshake and single proposal callback match the pinned wire contract',()=>{
 const f=fixture();const invoke=frames=>spawnSync('/usr/bin/python3',[peer,'native',f.root,f.trace,f.proposal,'app-server','--stdio','--strict-config'],{cwd:f.root,input:frames.map(f=>JSON.stringify(f)).join('\n')+'\n',encoding:'utf8'})
 const frames=[
  {id:1,method:'initialize',params:{clientInfo:{name:'jps_desk',version:'0.1.0'},capabilities:{experimentalApi:true}}},
  {method:'initialized'},
  {id:2,method:'account/read',params:{refreshToken:false}},
  {id:3,method:'model/list',params:{limit:100,includeHidden:false}},
  {id:4,method:'thread/start',params:{model:'gpt-6-sol',allowProviderModelFallback:false,dynamicTools:[{name:'saved_proposal',description:'propose_saved_pack: Propose a checked update.'}]}},
  {id:5,method:'turn/start',params:{threadId:'proposal-thread',input:[{type:'text',text:prompt}]}},
  {id:'proposal-callback',result:{success:true,contentItems:[{type:'inputText',text:'Checked proposal.'}]}}
 ]
 const got=invoke(frames);assert.equal(got.status,0);assert.equal(got.stderr,'')
 assert.deepEqual(got.stdout.trim().split('\n').map(JSON.parse),[
  {jsonrpc:'2.0',id:1,result:{userAgent:'jps_desk/0.157.1 (Linux)'}},
  {jsonrpc:'2.0',id:2,result:{requiresOpenaiAuth:true,account:{type:'chatgpt',planType:'plus'}}},
  {jsonrpc:'2.0',id:3,result:{data:[{model:'gpt-6-sol',displayName:'gpt-6-sol',defaultReasoningEffort:'medium',supportedReasoningEfforts:[{reasoningEffort:'low'},{reasoningEffort:'medium'},{reasoningEffort:'high'}]}]}},
  {jsonrpc:'2.0',id:4,result:{thread:{id:'proposal-thread'},model:'gpt-6-sol',modelProvider:'openai',approvalPolicy:'never',cwd:f.root,reasoningEffort:'medium'}},
  {id:5,result:{turn:{id:'proposal-turn'}}},
  {id:'proposal-callback',method:'item/tool/call',params:{threadId:'proposal-thread',turnId:'proposal-turn',callId:'proposal-call',tool:'saved_proposal',arguments:{document:candidate}}},
  {method:'item/completed',params:{threadId:'proposal-thread',turnId:'proposal-turn',item:{id:'proposal-message',type:'agentMessage',phase:'final_answer',text:'Review the checked proposal.'}}},
  {method:'turn/completed',params:{threadId:'proposal-thread',turn:{id:'proposal-turn',status:'completed'}}}
 ])
 const rows=readFileSync(f.trace,'utf8').trim().split('\n').map(JSON.parse);assert.equal(rows.filter(r=>r.kind==='native-turn').length,1);assert.equal(rows.filter(r=>r.kind==='callback').length,1);assert.equal(rows.filter(r=>r.kind==='refused').length,0)
 for(const extra of [frames[5],frames[6],{id:6,method:'unknown private method',params:{secret:'never retain'}}])assert.equal(invoke([...frames,extra]).status,65)
 const refused=JSON.parse(readFileSync(f.trace,'utf8').trim().split('\n').at(-1));assert.deepEqual(JSON.parse(refused.tag),{method:'other',name:'other',reason:'request'});assert(!readFileSync(f.trace,'utf8').includes('never retain'))
})
