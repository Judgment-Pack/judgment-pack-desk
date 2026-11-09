import test from 'node:test'
import assert from 'node:assert/strict'
import {spawnSync} from 'node:child_process'
import {mkdtempSync,writeFileSync,readFileSync} from 'node:fs'
import {tmpdir} from 'node:os'
import {join} from 'node:path'
import {fileURLToPath} from 'node:url'
import {fixedInputs} from './live-comparison-plan.mjs'
import {inventory,setupObserver,signInAndOpenConnections} from './offline-owner-browser-qualification.mjs'

const peer=fileURLToPath(new URL('./offline-owner-browser-peer.py',import.meta.url))
const request=(method,params={})=>({jsonrpc:'2.0',id:1,method,params})
function run(role,frames){
 const root=mkdtempSync(join(tmpdir(),'offline-owner-peer-')),trace=join(root,'trace.jsonl');writeFileSync(trace,'',{mode:0o600})
 const result=spawnSync('/usr/bin/python3',[peer,role,trace,...(role==='native'?['app-server','--stdio','--strict-config']:['mcp'])],{input:frames.map(v=>JSON.stringify(v)+'\n').join(''),encoding:'utf8',timeout:3000,maxBuffer:16384})
 return {...result,rows:result.stdout.trim()?result.stdout.trim().split('\n').map(JSON.parse):[],trace:readFileSync(trace,'utf8').trim().split('\n').map(JSON.parse)}
}
const thread=request('thread/start',{model:'gpt-6-sol',allowProviderModelFallback:false,dynamicTools:[{name:'desk_runtime_0',description:'get_schema: supplied recorded input'}]})
test('two literal public prompts use separate supplied native invocations and fixed answers',()=>{
 for(const [index,input] of fixedInputs.entries()){
  const frames=[request('initialize'),request('account/read'),request('model/list'),thread,request('turn/start',{input:[{type:'text',text:input.prompt}],effort:'medium'})]
  if(index)frames.push({id:'offline-callback',result:{success:true,contentItems:[{type:'inputText',text:'{"schema":"recorded"}'}]}})
  const result=run('native',frames);assert.equal(result.status,0);assert.equal(result.stderr,'')
  assert.equal(result.rows.filter(v=>v.method==='item/tool/call').length,index)
  assert.equal(result.rows.find(v=>v.method==='item/completed').params.item.text,'Offline fixture answer.')
  assert.equal(result.rows.find(v=>v.method==='turn/completed').params.turn.status,'completed')
  assert.equal(result.trace.filter(v=>v.kind==='native-turn').length,1)
 }
})
test('Runtime returns exact supplied schema fixture and no second callback',()=>{
 const call=request('tools/call',{name:'get_schema',arguments:{}}),result=run('runtime',[request('initialize'),request('tools/list'),call])
 assert.equal(result.status,0);assert.deepEqual(result.rows[2].result,{content:[{type:'text',text:'{"schema":"recorded"}'}],structuredContent:{schema:'recorded'},isError:false})
 assert.equal(run('runtime',[call,call]).status,64)
})
test('Runtime metadata keeps the exact tool offer and accepts only empty author_pack arguments',()=>{
 const result=run('runtime',[request('initialize'),request('tools/list'),request('prompts/list'),request('prompts/get',{name:'author_pack'}),request('prompts/get',{name:'author_pack',arguments:{}})])
 assert.equal(result.status,0)
 assert.deepEqual(result.rows[1].result,{tools:[{name:'get_schema',description:'Public recorded schema fixture',inputSchema:{type:'object',properties:{},additionalProperties:false}}]})
 assert.deepEqual(result.rows[2].result,{prompts:[{name:'author_pack',arguments:[]}]})
 const prompt={messages:[{role:'user',content:{type:'text',text:"Answer the person's exact question."}}]}
 assert.deepEqual(result.rows[3].result,prompt);assert.deepEqual(result.rows[4].result,prompt)
 assert.deepEqual(result.trace.filter(v=>v.kind==='metadata').map(v=>v.case),['initialize','tools/list','prompts/list','prompts/get','prompts/get'])
 for(const params of [{},{name:'other'},{name:'author_pack',arguments:null},{name:'author_pack',arguments:[]},{name:'author_pack',arguments:{policy:'unlisted'}},{name:'author_pack',extra:true},{name:'author_pack',arguments:{},extra:true}]){
  const refused=run('runtime',[request('prompts/get',params)])
  assert.equal(refused.status,64);assert.deepEqual(refused.rows,[]);assert.equal(refused.trace.at(-1).kind,'refused')
 }
})
test('unlisted prompt, model substitution, login and unrelated tools refuse',()=>{
 assert.equal(run('native',[thread,request('turn/start',{input:[{text:'unlisted'}],effort:'medium'})]).status,66)
 assert.equal(run('native',[request('thread/start',{model:'other',allowProviderModelFallback:false})]).status,65)
 assert.equal(run('native',[request('account/login/start')]).status,69)
 assert.equal(run('runtime',[request('tools/call',{name:'other',arguments:{}})]).status,64)
})
test('source custody inventories sort literal paths and hash actual bytes',()=>{
 const root=mkdtempSync(join(tmpdir(),'offline-owner-inventory-'));writeFileSync(join(root,'b'),'B');writeFileSync(join(root,'a'),'A')
 assert.deepEqual(inventory(root).rows,[['a','559aead08264d5795d3909718cdd05abd49572e84fe55590eef31a88a08fdffd'],['b','df7e70e5021544f4834bbee64a9e3789febc4be81470df629cad6ddb03320a5c']])
})

// Handwritten delayed sign-in: independently release normal reload, session ack and shell.
function delayedSignIn() {
 const calls=[],pending={}
 const wait=name=>new Promise((resolve,reject)=>{calls.push(name);pending[name]={resolve,reject}})
 const frame={url:()=> 'http://fixture/'},page={
  mainFrame:()=>frame,
  waitForEvent:(name,options)=>{assert.equal(name,'framenavigated');assert.equal(options.timeout,15000);return wait('reload').then(()=>assert(options.predicate(frame)))},
  waitForResponse:optionsPredicate=>wait('session').then(()=>{assert(!optionsPredicate({url:()=> 'http://fixture/api/session',ok:()=>false}));assert(optionsPredicate({url:()=> 'http://fixture/api/session',ok:()=>true}))}),
  getByRole:(role,options)=>{if(role==='button'){assert.equal(options.name,'Sign in');return {click:async()=>{calls.push('click')}}}assert.equal(role,'link');assert.equal(options.name,'Admin');return {waitFor:()=>wait('shell')}},
  goto:async url=>{assert.equal(url,'http://fixture/admin#connections-ai');calls.push('navigate')}
 }
 return {page,calls,pending}
}
const tick=()=>new Promise(resolve=>setImmediate(resolve))
test('normal delayed sign-in waits for reload, session acknowledgement and authenticated UI before navigation',{timeout:1000},async()=>{
 const fixture=delayedSignIn(),records=[],running=signInAndOpenConnections(fixture.page,'http://fixture',setupObserver(value=>records.push(value)))
 assert.deepEqual(fixture.calls,['reload','session','click'])
 fixture.pending.reload.resolve();await tick();assert(!fixture.calls.includes('navigate'));assert(!fixture.calls.includes('shell'))
 fixture.pending.session.resolve();await tick();assert(!fixture.calls.includes('navigate'));assert(fixture.calls.includes('shell'))
 fixture.pending.shell.resolve();await running;assert.equal(fixture.calls.at(-1),'navigate')
 assert.deepEqual(records.filter(v=>v.state==='ready').map(v=>v.step),['sign-in-submit','sign-in-reload','sign-in-session','authenticated-shell','connections-navigation'])
 assert(records.every(v=>Object.keys(v).join(',')==='kind,provenance,step,category,state'&&v.provenance==='offline-qualification'))
})
test('failed normal session readiness never navigates or records caught prose',{timeout:1000},async()=>{
 const fixture=delayedSignIn(),records=[],running=signInAndOpenConnections(fixture.page,'http://fixture',setupObserver(value=>records.push(value)))
 const refused=assert.rejects(running,/private failure/)
 fixture.pending.reload.resolve();fixture.pending.session.reject(new Error('private failure'));await refused
 assert(!fixture.calls.includes('navigate'));assert(!fixture.calls.includes('shell'))
 assert.deepEqual(records.at(-1),{kind:'offline-setup-observation',provenance:'offline-qualification',step:'sign-in-session',category:'readiness',state:'failed'})
 assert(!JSON.stringify(records).includes('private failure'))
})
