// Actual application recovery with finite local peers, never live inference.
import assert from 'node:assert/strict'
import {isAbsolute} from 'node:path'
import {chromium} from '../../web/node_modules/playwright-core/index.mjs'
import {readPrivate,writePrivate,keys,digest} from './replay-report.mjs'
import {signInAndOpenConnections} from './offline-owner-browser-qualification.mjs'

const c=readPrivate(process.argv[2])
keys(c,['kind','caseId','origin','setupCode','browserBin','reportPath'])
assert.equal(c.kind,'offline-recovery-control');assert(['reload-stop','typed-error'].includes(c.caseId))
const origin=new URL(c.origin)
assert.equal(origin.protocol,'http:');assert.equal(origin.hostname,'127.0.0.1')
assert(origin.port&&origin.origin===c.origin&&!origin.username&&!origin.password&&origin.pathname==='/'&&!origin.search&&!origin.hash)
assert(isAbsolute(c.browserBin)&&isAbsolute(c.reportPath)&&/^[A-Za-z0-9_-]{20,128}$/.test(c.setupCode))
const started=Date.now(),deadline=started+90000,left=()=>{assert(Date.now()<deadline);return deadline-Date.now()}
let browser,page,bearer='',starts=0,actions=0,errors=0,unexpected=0,runId=null,chatId=null
const r={kind:'offline-recovery-browser',provenance:'offline-test',caseId:c.caseId,state:'incomplete',step:'bootstrap',errorName:null,runId:null,admissionId:null,requestId:null,terminalState:null,failureCode:null,completedOperationsSha256:null,terminalOperationsSha256:null,navigation:[],observations:{},counts:null,ui:{normalSignIn:false,activeReconnect:false,explicitStop:false,typedFailure:false,terminalReload:false},elapsedMs:0}
const step=name=>{r.step=name;writePrivate(c.reportPath,r)}
const wait=async f=>{while(true){left();assert.equal(errors,0);assert.equal(unexpected,0);if(await f())return;await new Promise(resolve=>setTimeout(resolve,100))}}
const api=async path=>{
 assert(bearer&&/^\/api\/(?:conversations|assistant-runs\/arun_[a-f0-9]+)$/.test(path))
 const response=await page.request.get(c.origin+path,{headers:{Authorization:bearer},timeout:left()})
 assert(response.ok());const text=await response.text();assert(Buffer.byteLength(text)<=4*1024*1024);return JSON.parse(text)
}
const getRun=()=>api('/api/assistant-runs/'+runId)
// The pack pane is selected by its actual saved owner, not a seeded browser chat.
const ownerPane=()=>page.locator('section[data-placement="pane"][data-chat-id="'+chatId+'"]')
const reload=async stage=>{
 step(stage+'-navigation')
 await page.goto(c.origin+'/packs/one?chat='+chatId,{timeout:left()})
 const response=await page.reload({timeout:left()});assert(response?.ok())
 const type=await page.evaluate(()=>performance.getEntriesByType('navigation')[0]?.type??null)
 assert.equal(type,'reload');assert(r.navigation.length<2);r.navigation.push({stage,type})
 await ownerPane().waitFor();writePrivate(c.reportPath,r)
}
const sameOwner=run=>{assert.equal(run.id,runId);assert.equal(run.owner.id,chatId);assert.equal(run.admissionId,r.admissionId);assert.equal(run.requestId,r.requestId)}
const observeRun=run=>{r.observations[r.step]={state:run.state,checkpointStatus:run.checkpoint?.status??null,projectionAcknowledged:run.chatProjection?.acknowledged??false,projectionIssue:run.projectionIssue??null,operationStates:run.operations.map(o=>({kind:o.kind,state:o.state})),actionKinds:run.actions?.map(a=>a.kind)??null};writePrivate(c.reportPath,r)}
const terminal=async expected=>{
 let run
 await wait(async()=>{run=await getRun();sameOwner(run);observeRun(run);if(['complete','failed','stopped','interrupted','paused-budget','awaiting-input','awaiting-client'].includes(run.state))assert.equal(run.state,expected);return run.state===expected&&run.chatProjection?.acknowledged===true})
 return run
}
const authenticationMessage='The AI connection needs authentication. Check its credentials or sign in again.'
const failureUI=async()=>{
 const recovery=ownerPane().getByRole('article',{name:'Response interrupted',exact:true})
 await recovery.getByRole('alert').filter({hasText:authenticationMessage}).waitFor()
 assert.equal((await recovery.getByRole('alert').innerText()).trim(),authenticationMessage)
 assert.equal(await page.getByText('fixture-provider-prose-must-not-render',{exact:false}).count(),0)
}
const timer=setTimeout(()=>void browser?.close(),90000)
try{
 step('launch')
 browser=await chromium.launch({executablePath:c.browserBin,args:['--no-sandbox'],timeout:left()})
 const context=await browser.newContext({viewport:{width:1600,height:1000},locale:'en-CA'})
 page=await context.newPage();page.setDefaultTimeout(15000)
 await context.route('**/*',async route=>{const u=new URL(route.request().url());if(u.origin!==c.origin||/\/api\/model-providers\/.*\/(?:login|install|logout)/.test(u.pathname)){unexpected++;await route.abort()}else await route.continue()})
 page.on('pageerror',()=>errors++)
 page.on('request',q=>{
  const p=new URL(q.url()).pathname
  if(p==='/api/conversations'||p==='/api/ai-connections')void q.headerValue('authorization').then(v=>{bearer=v??bearer})
  if(p==='/api/assistant-runs'&&q.method()==='POST'){starts++;if(starts>1)unexpected++}
  if(/^\/api\/assistant-runs\/arun_[a-f0-9]+\/actions$/.test(p)&&q.method()==='POST'){
   actions++;try{if(c.caseId!=='reload-stop'||actions>1||p!=='/api/assistant-runs/'+runId+'/actions'||q.postDataJSON().kind!=='cancel')unexpected++}catch{unexpected++}
  }
 })
 step('owner-setup');await page.goto(c.origin,{timeout:left()})
 await page.getByRole('button',{name:'Set up sign-in',exact:true}).click()
 await page.getByLabel('Owner setup code',{exact:true}).fill(c.setupCode)
 await page.getByRole('button',{name:'Continue',exact:true}).click()
 await page.getByRole('button',{name:'Set password',exact:true}).click()
 const password=page.getByRole('dialog',{name:'Set password',exact:true})
 await password.getByLabel('New password',{exact:true}).fill('Fixture7')
 await password.getByLabel('Confirm new password',{exact:true}).fill('Fixture7')
 await password.getByRole('button',{name:'Set password',exact:true}).click()
 step('owner-sign-in');await page.getByLabel('Desk password',{exact:true}).fill('Fixture7')
 await signInAndOpenConnections(page,c.origin,async(_step,_category,action)=>action())
 await wait(()=>!!bearer);r.ui.normalSignIn=true
 step('chat-navigation');await page.goto(c.origin+'/packs/one',{timeout:left()})
 const initial=page.locator('section[data-chat-id][data-placement="pane"]').first()
 const composer=initial.getByPlaceholder('Ask about this pack…');await composer.waitFor()
 step('chat-settings')
 await initial.getByRole('combobox',{name:'Task tools',exact:true}).click();await page.getByRole('option',{name:'Chat',exact:true}).click()
 await initial.getByRole('button',{name:'Assistant settings',exact:true}).click()
 await page.getByRole('combobox',{name:'Research',exact:true}).click();await page.getByRole('option',{name:'Provided sources only',exact:true}).click()
 await page.getByRole('combobox',{name:'Adversarial review',exact:true}).selectOption('off')
 assert.equal(await initial.getByRole('combobox',{name:'Task tools',exact:true}).innerText(),'Chat')
 assert.equal(await page.getByRole('combobox',{name:'Research',exact:true}).innerText(),'Provided sources only')
 assert.equal(await page.getByRole('combobox',{name:'Adversarial review',exact:true}).inputValue(),'off')
 await page.keyboard.press('Escape')
 step('dispatch');await composer.fill('Read the schema once with get_schema, then explain the recorded result. Do not repeat a completed tool call.')
 const [response]=await Promise.all([page.waitForResponse(res=>new URL(res.url()).pathname==='/api/assistant-runs'&&res.request().method()==='POST',{timeout:left()}),composer.press('Enter')])
 assert.equal(response.status(),201);const admission=await response.json();assert(/^arun_[a-f0-9]+$/.test(admission.id));runId=admission.id;r.runId=runId
 step('completed-read')
 let run
 await wait(async()=>{run=await getRun();return run.operations.filter(o=>o.kind==='model').length===2&&run.operations.some(o=>o.kind==='model'&&o.state==='completed')&&run.operations.some(o=>o.kind==='runtime'&&o.name==='tools/call'&&o.state==='completed')})
 chatId=run.owner.id;r.admissionId=run.admissionId;r.requestId=run.requestId
 const completed=run.operations.filter(o=>o.state==='completed');r.completedOperationsSha256=digest(completed)
 const preserveCompleted=current=>{for(const expected of completed)assert.deepEqual(current.operations.find(o=>o.id===expected.id),expected)}
 const saved=await api('/api/conversations'),chat=saved.content.chats.find(v=>v.id===chatId)
 assert(chat);assert.equal(chat.model,'gpt-6-sol');assert.equal(chat.critic,'off');assert.equal(chat.researchMode,'provided')
 if(c.caseId==='reload-stop'){
  assert.equal(run.state,'running')
  await reload('active-reload')
  step('active-reconnected');await ownerPane().getByRole('button',{name:'Stop',exact:true}).waitFor()
  const active=await getRun();sameOwner(active);assert.equal(active.state,'running');preserveCompleted(active)
  assert.equal(starts,1);assert.equal(actions,0);r.ui.activeReconnect=true
  step('explicit-stop-action-response')
  const [stopResponse]=await Promise.all([page.waitForResponse(res=>new URL(res.url()).pathname==='/api/assistant-runs/'+runId+'/actions'&&res.request().method()==='POST',{timeout:left()}),ownerPane().getByRole('button',{name:'Stop',exact:true}).click()])
  r.observations.stopAction={status:stopResponse.status()};writePrivate(c.reportPath,r);assert(stopResponse.ok())
  step('explicit-stop-response-finished');const finishError=await stopResponse.finished();assert.equal(finishError,null)
  step('explicit-stop-host-state');await wait(async()=>{run=await getRun();sameOwner(run);observeRun(run);return run.state==='stopped'});preserveCompleted(run)
  step('explicit-stop-chat-projection');run=await terminal('stopped');preserveCompleted(run);r.ui.explicitStop=true
 }else{
  step('typed-failure');run=await terminal('failed');preserveCompleted(run)
  const failure=run.events.map(row=>row.event).find(event=>event.type==='error'&&event.failure)
  assert(failure);assert.equal(failure.failure.code,'authentication');r.failureCode=failure.failure.code
  await failureUI();r.ui.typedFailure=true
 }
 r.terminalState=run.state;r.terminalOperationsSha256=digest(run.operations)
 await reload('terminal-reload')
 step('terminal-readback');const retained=await getRun();sameOwner(retained)
 assert.equal(retained.state,r.terminalState);assert.equal(retained.chatProjection?.acknowledged,true)
 assert.equal(digest(retained.operations),r.terminalOperationsSha256);preserveCompleted(retained)
 if(c.caseId==='typed-error'){
  assert.equal(retained.events.map(row=>row.event).find(event=>event.type==='error'&&event.failure)?.failure.code,'authentication')
  await failureUI()
 }else{
  await ownerPane().getByRole('button',{name:'Send',exact:true}).waitFor()
  assert.equal(await ownerPane().getByRole('button',{name:'Stop',exact:true}).count(),0)
 }
 assert.equal(starts,1);assert.equal(actions,c.caseId==='reload-stop'?1:0);assert.equal(errors,0);assert.equal(unexpected,0)
 r.ui.terminalReload=true;r.state='complete';r.step='complete'
}catch(error){r.errorName=['Error','TimeoutError','AssertionError','TypeError','RangeError'].includes(error?.name)?error.name:'Other'}
finally{
 clearTimeout(timer)
 try{await browser?.close()}catch{r.state='incomplete';r.step='cleanup';r.errorName='Error'}
 r.counts={starts,actions,browserErrors:errors,unexpectedRequests:unexpected};r.elapsedMs=Date.now()-started;writePrivate(c.reportPath,r)
}
process.stdout.write('offline recovery '+c.caseId+' '+r.state+': '+r.step+'\n')
if(r.state!=='complete')process.exitCode=1
