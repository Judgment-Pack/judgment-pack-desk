import test from 'node:test'
import assert from 'node:assert/strict'
import {EventEmitter} from 'node:events'
import {createLiveSessionOperator,validateReady} from './live-session-operator.mjs'
const id='ai-'+'a'.repeat(24),H='b'.repeat(64),secret='PRIVATE-CHALLENGE-PASSWORD-BEARER'
function data(){return {registry:{version:1,defaultConnection:id,connections:[{id,name:'Measured',enabled:true,revision:H,assistant:{engine:'codex',endpoint:null,agent:{provider:'openai',authMethod:'subscription',model:'chosen-model',models:['chosen-model'],effort:'medium',tools:['get_schema']}}}]},status:{provider:'openai',authMethod:'subscription',agent:'codex',runtime:'available',account:'connected'},models:{models:[{id:'chosen-model',efforts:['low','medium']}]},input:{connectionId:id,connectionName:'Measured',model:'chosen-model',effort:'medium'},applicationRole:'candidate'}}
class Locator {
 constructor(page,name,type=''){Object.assign(this,{page,name,type})}
 getByRole(type,{name}={}){return new Locator(this.page,name,type)}
 getByLabel(name){return new Locator(this.page,name)}
 filter(){return this}
 nth(i){return new Locator(this.page,i===0?'get_schema':'other-tool','tool')}
 async click(){this.page.log.push('click:'+this.name);this.page.before?.(this.name);if(this.name==='Add connection'&&this.type==='button')this.page.created=true}
 async fill(){this.page.log.push('fill-name')}
 async check(){this.page.log.push('check:'+this.name)}
 async uncheck(){this.page.log.push('uncheck:'+this.name);this.page.before?.('tool-uncheck')}
 async count(){return 2}
 async evaluate(){return this.name}
 async isVisible(){this.page.log.push('visible:'+this.name);this.page.before?.('visible:'+this.name);if(this.name==='Admin'&&this.page.adminWaits++<1)return false;if(this.name==='Connected to ChatGPT'&&this.page.connectedWaits++<1)return false;return true}
}
class Page extends EventEmitter {
 constructor(){super();this.log=[];this.created=false;this.adminWaits=0;this.connectedWaits=0;this.d=data();this.keyboard={press:async()=>this.log.push('escape')};this.request={get:async(url,options)=>{this.log.push('read:'+new URL(url).pathname);assert.equal(options.headers.Authorization,'Bearer '+secret);this.onRead?.(url);const path=new URL(url).pathname;let body;if(path==='/api/session')body={subject:'owner',issuer:null};else if(path==='/api/ai-connections')body=this.created?this.d.registry:{version:1,connections:[]};else{assert.equal(options.headers['X-Assistant-Connection'],id);body=path.endsWith('/status')?this.d.status:this.d.models}return {ok:()=>true,text:async()=>JSON.stringify(body)}}}}
 getByRole(type,{name}={}){return new Locator(this,name,type)}
 getByText(name){return new Locator(this,name)}
 locator(name){return new Locator(this,name)}
 async goto(){this.log.push('admin-navigation');this.emit('request',{url:()=> 'http://127.0.0.1:8123/api/ai-connections',headerValue:async()=> 'Bearer '+secret})}
 async close(){this.log.push('page-close')}
}
function fixture(applicationRole='candidate'){
 const page=new Page(),abort=new AbortController(),signal=abort.signal
 const operator=createLiveSessionOperator({role:applicationRole,humanOwnerSetup:async args=>{assert.equal(args.page,page);assert.equal(args.setupToken,secret);page.log.push('human-owner')},humanDeviceSignIn:async args=>{assert.equal(args.page,page);assert.deepEqual(Object.keys(args).sort(),['page','signal']);page.log.push('human-device')}})
 const input={page,origin:'http://127.0.0.1:8123',setupToken:secret,connectionName:'Measured',model:'chosen-model',effort:'medium',signal}
 const verify={page,control:{role:applicationRole,...page.d.input},signal}
 return {page,abort,operator,input,verify}
}
test('factory requires explicit human handoffs and role',()=>{
 assert.throws(()=>createLiveSessionOperator({role:'candidate'}));assert.throws(()=>createLiveSessionOperator({humanOwnerSetup:()=>{},humanDeviceSignIn:()=>{}}))
})
for(const applicationRole of ['baseline','candidate'])test(applicationRole+' normal UI delayed readiness and independent source-shaped readback',async()=>{
 const f=fixture(applicationRole),result=await f.operator.setupUI(f.input)
 assert.deepEqual(result,{connectionId:id});assert(!JSON.stringify(result).includes(secret));assert.equal(await f.operator.verifyReady(f.verify),true)
 const log=f.page.log
 assert(log.indexOf('human-owner')<log.indexOf('admin-navigation'));assert(log.indexOf('click:Connect ChatGPT')<log.indexOf('human-device'));assert(log.indexOf('human-device')<log.indexOf('click:Save shared settings'));assert.equal(f.page.adminWaits,2);assert.equal(f.page.connectedWaits,2)
 assert(log.includes('click:Device code'));assert(log.includes('click:chosen-model'));assert(log.includes('click:medium'));assert(!log.some(v=>v.includes('Test connection')))
 assert(log.includes('read:/api/model-providers/openai/status'));assert(log.includes('read:/api/model-providers/openai/models'));assert(!log.some(v=>v.includes('/login')||v.includes('/assistant-runs')))
})
test('exact saved identity, provider, model, effort, allowed models/tools and account/model offers refuse mismatches',()=>{
 for(const change of [v=>v.registry.connections[0].name='Other',v=>v.input.connectionId='ai-'+'c'.repeat(24),v=>v.registry.connections[0].assistant.engine='vercel',v=>v.registry.connections[0].assistant.agent.provider='other',v=>v.registry.connections[0].assistant.agent.model='wrong',v=>v.registry.connections[0].assistant.agent.effort='low',v=>v.registry.connections[0].assistant.agent.tools=['get_schema','other'],v=>v.registry.connections[0].assistant.agent.models=['chosen-model','other'],v=>v.status.account='login-pending',v=>v.status.runtime='not-installed',v=>v.models.models[0].efforts=['high'],v=>v.registry.connections.push(structuredClone(v.registry.connections[0])),v=>v.registry.defaultConnection='other',v=>v.status.accountRevision='invalid']){const d=data();change(d);assert.throws(()=>validateReady(d))}
 const d=data();assert.equal(validateReady(d),true);d.status.accountRevision=H;assert.equal(validateReady(d),true);delete d.status.accountRevision;d.applicationRole='baseline';assert.equal(validateReady(d),true);assert(!('accountRevision' in d.status))
})
test('signal abort before Connect closes only supplied page and prevents later writes',async()=>{
 const f=fixture();f.page.before=name=>{if(name==='Device code')f.abort.abort()}
 await assert.rejects(f.operator.setupUI(f.input),/live-session-operator-incomplete/)
 assert(f.page.log.includes('page-close'));assert(!f.page.log.includes('click:Connect ChatGPT'));assert(!f.page.log.includes('click:Save shared settings'))
})
test('signal abort before save prevents save and readback',async()=>{
 const f=fixture();f.page.before=name=>{if(name==='tool-uncheck')f.abort.abort()}
 await assert.rejects(f.operator.setupUI(f.input),/live-session-operator-incomplete/)
 assert(!f.page.log.includes('click:Save shared settings'));assert(!f.page.log.includes('read:/api/model-providers/openai/status'))
})
test('abort before verification or during first read prevents subsequent reads',async()=>{
 const f=fixture();await f.operator.setupUI(f.input);f.abort.abort();const n=f.page.log.length;await assert.rejects(f.operator.verifyReady(f.verify));assert.equal(f.page.log.length,n)
 const g=fixture();await g.operator.setupUI(g.input);g.page.onRead=()=>g.abort.abort();await assert.rejects(g.operator.verifyReady(g.verify));assert(!g.page.log.includes('read:/api/model-providers/openai/status'))
})
test('human failure and returned credential data refuse with no raw prose',async()=>{
 const f=fixture();const operator=createLiveSessionOperator({role:'candidate',humanOwnerSetup:async()=>{throw new Error(secret)},humanDeviceSignIn:async()=>{}})
 await assert.rejects(operator.setupUI(f.input),error=>error.message==='live-session-operator-incomplete'&&!String(error).includes(secret))
 const other=createLiveSessionOperator({role:'candidate',humanOwnerSetup:async()=>({password:secret}),humanDeviceSignIn:async()=>{}});await assert.rejects(other.setupUI(f.input));assert(!f.page.log.includes('click:Connect ChatGPT'))
})
test('exact model/effort inputs required and stale connection cannot pass verify',async()=>{
 for(const key of ['model','effort']){const f=fixture();delete f.input[key];await assert.rejects(f.operator.setupUI(f.input))}
 const f=fixture();await f.operator.setupUI(f.input);f.verify.control.connectionId='ai-'+'d'.repeat(24);await assert.rejects(f.operator.verifyReady(f.verify))
})
