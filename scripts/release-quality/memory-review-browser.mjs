// Actual Desk UI; handwritten provider responses are supplied by the Go launcher.
import assert from 'node:assert/strict'
import {isAbsolute} from 'node:path'
import {chromium} from '../../web/node_modules/playwright-core/index.mjs'
import {readPrivate,writePrivate,keys,digest} from './replay-report.mjs'
import {projectionReady} from './persisted-proposal-report.mjs'
import {signInAndOpenConnections} from './offline-owner-browser-qualification.mjs'

const c=readPrivate(process.argv[2])
keys(c,['kind','origin','setupCode','browserBin','reportPath','scenario'])
assert.equal(c.kind,'offline-memory-review-control')
assert(['accept','edit-forget','decline','clear','stale','lost-ack'].includes(c.scenario))
// Clear completes two responses and reconciliation before its final document reload.
const limits={starts:c.scenario==='clear'?2:1,providerCalls:c.scenario==='clear'?4:2,settingsPosts:2,memoryPosts:c.scenario==='clear'?4:c.scenario==='edit-forget'?3:c.scenario==='stale'?2:1,seconds:c.scenario==='clear'?120:90}
const origin=new URL(c.origin)
assert.equal(origin.protocol,'http:');assert.equal(origin.hostname,'127.0.0.1')
assert(origin.port&&origin.origin===c.origin&&!origin.username&&!origin.password&&origin.pathname==='/'&&!origin.search&&!origin.hash)
assert(isAbsolute(c.browserBin)&&isAbsolute(c.reportPath)&&/^[A-Za-z0-9_-]{20,128}$/.test(c.setupCode))
const started=Date.now(),deadline=started+limits.seconds*1000,left=()=>{assert(Date.now()<deadline);return deadline-Date.now()}
let browser,page,bearer='',admissionBearer='',starts=0,reviews=0,errors=0,unexpected=0,runId=null
const r={scenario:c.scenario,limits,writes:[],observations:[],kind:'offline-memory-review-browser',provenance:'offline-test',state:'incomplete',phase:'bootstrap',step:'bootstrap',settings:{},navigation:[],responseStatus:null,errorName:null,runId:null,proposal:null,accepted:null,memorySha256:null,operationsSha256:null,terminal:[],counts:null,ui:{normalSignIn:false,pendingAfterReload:false,explicitAcceptance:false,acceptedAfterReload:false},elapsedMs:0}
const checkpoint=phase=>{r.phase=phase;r.step=phase;writePrivate(c.reportPath,r)}
const step=name=>{r.step=name;writePrivate(c.reportPath,r)}
const wait=async f=>{while(true){left();assert.equal(errors,0);assert.equal(unexpected,0);if(await f())return;await new Promise(resolve=>setTimeout(resolve,100))}}
const api=async path=>{
 assert(bearer&&/^\/api\/(?:ai-memory|conversations|assistant-runs\/arun_[a-f0-9]+)$/.test(path))
 const requestBearer=bearer,calledAt=Date.now(),category=path.startsWith('/api/assistant-runs/')?'assistant-run':path.slice(5)
 const timing=r.apiTimings??={count:0,totalMs:0,maxMs:0,last:null}
 let response
 try{response=await page.request.get(c.origin+path,{headers:{Authorization:requestBearer},timeout:left()})}
 finally{
  const elapsedMs=Date.now()-calledAt
  timing.count++;timing.totalMs+=elapsedMs;timing.maxMs=Math.max(timing.maxMs,elapsedMs)
  timing.last={category,status:response?.status()??null,elapsedMs,credentialMatchesAdmission:!!admissionBearer&&requestBearer===admissionBearer}
  writePrivate(c.reportPath,r)
 }
 if(!response.ok()){
  let code=null
  const body=await response.body();assert(body.length<=4096)
  try{code=JSON.parse(body.toString('utf8')).code??null}catch{}
  r.apiFailure={category:path.startsWith('/api/assistant-runs/')?'assistant-run':path.slice(5),status:response.status(),code:typeof code==='string'&&/^[a-z0-9_-]{1,80}$/i.test(code)?code:null,elapsedMs:Date.now()-calledAt,credentialMatchesAdmission:!!admissionBearer&&requestBearer===admissionBearer}
  writePrivate(c.reportPath,r)
 }
 assert(response.ok());const text=await response.text();assert(Buffer.byteLength(text)<=4*1024*1024);return JSON.parse(text)
}
const memoryUI=async({reload=false}={})=>{
 const row={phase:r.phase,reload,navigationType:null,expandedBefore:null,expandedAfter:null}
 assert(r.navigation.length<10);r.navigation.push(row)
 step('memory-navigation')
 const target=c.origin+'/admin#assistant'
 if(page.url()!==target)await page.goto(target,{timeout:left()})
 if(reload){
  step('memory-document-reload');const response=await page.reload({timeout:left()});assert(response?.ok())
  row.navigationType=await page.evaluate(()=>performance.getEntriesByType('navigation')[0]?.type??null)
  writePrivate(c.reportPath,r);assert.equal(row.navigationType,'reload')
 }
 step('memory-disclosure')
 const disclosure=page.locator('#assistant-memory').getByRole('button',{name:'Project memory',exact:true})
 await disclosure.waitFor()
 row.expandedBefore=await disclosure.getAttribute('aria-expanded');writePrivate(c.reportPath,r)
 assert(['true','false'].includes(row.expandedBefore))
 if(row.expandedBefore==='false')await disclosure.click({timeout:left()})
 row.expandedAfter=await disclosure.getAttribute('aria-expanded');writePrivate(c.reportPath,r)
 assert.equal(row.expandedAfter,'true')
 step('memory-region');const region=page.getByRole('region',{name:'Project memory',exact:true});await region.waitFor();return region
}
// Controlled settings are committed asynchronously; click does not assert the
// immediate checked state as Playwright check/uncheck do. Observe the normal
// write, durable readback and React state before issuing another change.
const setMemorySetting=async(region,key,name,value)=>{
 const checkbox=region.getByRole('checkbox',{name,exact:true})
 const observe=async()=>{const row={count:await checkbox.count()};if(row.count===1){row.visible=await checkbox.isVisible();row.enabled=await checkbox.isEnabled();row.checked=await checkbox.isChecked()}r.settings[key]=row;writePrivate(c.reportPath,r);return row}
 step('settings-'+key+'-ready')
 await wait(async()=>{const row=await observe();return row.visible&&row.enabled})
 if((await observe()).checked!==value){
  step('settings-'+key+'-write')
  const [response]=await Promise.all([page.waitForResponse(res=>{
   if(new URL(res.url()).pathname!=='/api/ai-memory'||res.request().method()!=='POST')return false
   try{return res.request().postDataJSON().action==='settings'}catch{return false}
  },{timeout:left()}),checkbox.click({timeout:left()})])
  r.responseStatus=response.status();writePrivate(c.reportPath,r);assert(response.ok())
  await response.finished()
 }
 step('settings-'+key+'-readback')
 await wait(async()=>{const m=await api('/api/ai-memory');return m.content.settings[key]===value})
 step('settings-'+key+'-ui')
 await wait(async()=>{const row=await observe();return row.visible&&row.enabled&&row.checked===value})
}
// Observe only normal UI writes; reconciliation is a declared single delivery
// of an already completed receipt, with its original If-Match and intent.
const memoryPost=async(body,revision,expected=200)=>{
 r.writes.push({body,revision,observationOnly:true})
 assert(r.writes.filter(w=>w.body.action!=='settings').length<=limits.memoryPosts)
 const response=await page.request.post(c.origin+'/api/ai-memory',{headers:{Authorization:bearer,'If-Match':revision},data:body,timeout:left()})
 assert.equal(response.status(),expected)
 return response.json()
}
const observe=async label=>{
 const snapshot=await api('/api/ai-memory')
 r.observations.push({label,...snapshot});writePrivate(c.reportPath,r)
 return snapshot
}
const uiWrite=async(target,action,status=200)=>{
 const [response]=await Promise.all([page.waitForResponse(res=>{
  if(new URL(res.url()).pathname!=='/api/ai-memory'||res.request().method()!=='POST')return false
  return res.request().postDataJSON().action===action
 },{timeout:left()}),target.click({timeout:left()})])
 assert.equal(response.status(),status);await response.finished()
 return response
}
const management=async(context,run,proposal,before)=>{
 checkpoint(c.scenario)
 let region=await memoryUI()
 await region.getByRole('button',{name:'Remember',exact:true}).waitFor()
 const edited='Keep outreach evidence explicit.'
 const edit=async()=>{
  step('memory-edit-open');await region.getByRole('button',{name:'Edit',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'Edit memory',exact:true})
  step('memory-edit-draft');await dialog.getByLabel('Memory',{exact:true}).fill(edited)
  return dialog
 }
 let frozen,second
 if(c.scenario==='stale'){
  frozen=await edit()
  // A normal same-origin popup retains its opener and inherits the browser's
  // session. Keep the originating editor mounted at its frozen revision.
  step('same-session-popup-open')
  ;[second]=await Promise.all([
   page.waitForEvent('popup',{timeout:left()}),
   page.evaluate(url=>{window.open(url,'_blank')},c.origin+'/admin#assistant')
  ])
  second.setDefaultTimeout(15000);second.on('pageerror',()=>errors++)
  assert.equal(await second.opener(),page)
  step('same-session-popup-shell')
  await second.getByRole('link',{name:'Back to app',exact:true}).waitFor({state:'visible'})
  r.ui.sameSessionPopupShell=true;writePrivate(c.reportPath,r)
  const original=page;page=second;region=await memoryUI()
  step('same-session-popup-proposal')
  await region.getByText(proposal.memory.text,{exact:true}).waitFor({state:'visible'})
  assert.equal(await region.getByRole('link',{name:'Source chat',exact:true}).getAttribute('href'),'/chats/'+encodeURIComponent(proposal.chatId))
  assert(await region.getByRole('button',{name:'Remember',exact:true}).isEnabled())
  assert(await region.getByRole('button',{name:'Edit',exact:true}).isEnabled())
  const dialog=await edit()
  // Observe the popup's normal UI write and bind it to the originating proposal.
  second.on('request',q=>{if(new URL(q.url()).pathname==='/api/ai-memory'&&q.method()==='POST'){reviews++;r.writes.push({body:q.postDataJSON(),revision:q.headers()['if-match']})}})
  step('same-session-popup-review')
  await uiWrite(dialog.getByRole('button',{name:'Remember',exact:true}),'review')
  const popupIntent=r.writes.filter(w=>w.body.action==='review').at(-1)
  assert.equal(popupIntent.body.review.runId,runId)
  assert.equal(popupIntent.body.review.runRevision,proposal.runRevision)
  assert.equal(popupIntent.body.review.proposalId,proposal.memory.id)
  assert.equal(popupIntent.body.review.proposalDigest,proposal.memory.digest)
  assert.equal(popupIntent.revision,before.sha256)
  r.ui.sameSessionPopupProposal=true;writePrivate(c.reportPath,r)
  page=original
  const current=await observe('other-page-edit')
  assert.notEqual(current.sha256,before.sha256)
  step('frozen-editor-review');await uiWrite(frozen.getByRole('button',{name:'Remember',exact:true}),'review',409)
  assert.equal(await frozen.getByLabel('Memory',{exact:true}).inputValue(),edited)
  await wait(async()=>await frozen.getByText(/changed|conflict|refresh|stale/i).count()>0)
  assert.equal((await observe('frozen-editor-refused')).sha256,current.sha256)
  const refused=r.writes.filter(w=>w.body.action==='review').at(-1)
  assert.equal(refused.revision,before.sha256)
  await frozen.getByRole('button',{name:'Cancel',exact:true}).click()
  await second.close()
 }else if(c.scenario==='lost-ack'){
  let completed=false
  await page.route('**/api/ai-memory',async route=>{
   if(route.request().method()!=='POST'||route.request().postDataJSON().action!=='review')return route.continue()
   assert(!completed)
   const response=await route.fetch({timeout:left()});assert.equal(response.status(),200)
   const actual=await response.json();assert.equal(actual.content.reviews.length,1);assert.equal(actual.content.entries.length,1)
   r.completedBeforeLostAck=actual;completed=true;writePrivate(c.reportPath,r)
   await route.abort('failed')
  })
  await region.getByRole('button',{name:'Remember',exact:true}).click()
  await wait(()=>completed)
  await wait(async()=>await region.getByRole('button',{name:'Forget',exact:true}).count()===1)
  await page.unroute('**/api/ai-memory')
  assert.equal((await observe('lost-ack-readback')).sha256,r.completedBeforeLostAck.sha256)
 }else if(c.scenario==='decline'){
  await uiWrite(region.getByRole('button',{name:'Decline',exact:true}),'review')
 }else{
  const dialog=await edit()
  await uiWrite(dialog.getByRole('button',{name:'Remember',exact:true}),'review')
 }
 let after=await observe('decision')
 assert.equal(after.content.reviews.length,1);assert.equal(after.proposals?.length??0,0)
 const receipt=after.content.reviews[0]
 assert.equal(receipt.runId,runId);assert.equal(receipt.runRevision,proposal.runRevision)
 assert.equal(receipt.proposalId,proposal.memory.id);assert.equal(receipt.proposalDigest,proposal.memory.digest)
 const decision=c.scenario==='decline'?'decline':c.scenario==='lost-ack'?'accept':'edit'
 assert.equal(receipt.decision,decision)
 const completedIntent=r.writes.find(w=>w.body.action==='review'&&w.body.review.id===receipt.id)
 assert(completedIntent)
 if(decision==='decline')assert.equal(after.content.entries.length,0)
 else{
  const entry=after.content.entries[0];assert.equal(after.content.entries.length,1)
  assert.equal(entry.id,receipt.entryId);assert.equal(entry.canonical.id,receipt.memoryId);assert.equal(entry.canonical.digest,receipt.memoryDigest)
  assert.equal(entry.source.chatId,proposal.chatId);assert.equal(entry.source.turnId,proposal.turnId)
  if(decision==='edit'){
   assert.equal(entry.text,edited);assert.notEqual(entry.canonical.id,proposal.memory.id)
   assert.equal(entry.canonical.digest,'sha256:'+ (await import('node:crypto')).createHash('sha256').update(edited).digest('hex'))
   assert.equal(entry.canonical.source.proposedBy,'person')
  }
 }
 if(c.scenario==='edit-forget'){
  await uiWrite(region.getByRole('button',{name:'Forget',exact:true}),'forget')
  after=await observe('forgotten');assert.equal(after.content.entries.length,0)
  assert.deepEqual(after.content.reviews,[receipt])
  const reconciled=await memoryPost(completedIntent.body,completedIntent.revision)
  assert.equal(reconciled.sha256,after.sha256);assert.equal(reconciled.content.entries.length,0)
 }
 if(c.scenario==='clear'){
  // One further real user submission creates a pending proposal beside saved memory.
  await page.goto(c.origin+'/packs/one',{timeout:left()})
  const pane=page.locator('section[data-chat-id][data-placement="pane"]').first()
  const composer=pane.locator('form[data-chat-composer] textarea');await composer.waitFor()
  // Saved history replaces the temporary pack chat after full navigation.
  // Observe its identity before writing the controlled composer once.
  const text='Remember that I prefer concise outreach. Propose this new preference for my review, then finish.'
  const send=pane.getByRole('button',{name:'Send',exact:true})
  const reconnect=pane.getByRole('button',{name:'Reconnect',exact:true})
  r.clearReadiness=[]
  const observeComposer=async phase=>{
   const row={phase,elapsedMs:Date.now()-started,expectedChatId:proposal.chatId,chatId:await pane.getAttribute('data-chat-id'),historyLoading:await pane.getAttribute('data-history-loading'),composerEnabled:await composer.isEnabled(),hasText:!!(await composer.inputValue()).trim(),exactText:(await composer.inputValue())===text,sendCount:await send.count(),stopCount:await pane.getByRole('button',{name:'Stop',exact:true}).count(),reconnectCount:await reconnect.count()}
   if(row.sendCount===1)row.sendEnabled=await send.isEnabled()
   if(row.reconnectCount===1)row.reconnectEnabled=await reconnect.isEnabled()
   r.clearComposer=row
   // Retain at most the first and last observation in each finite phase.
   const previous=r.clearReadiness.at(-1)
   if(previous?.phase===phase&&r.clearReadiness.at(-2)?.phase===phase)r.clearReadiness[r.clearReadiness.length-1]=row
   else r.clearReadiness.push(row)
   writePrivate(c.reportPath,r);return row
  }
  step('clear-history-readiness')
  await wait(async()=>{
   const row=await observeComposer('history')
   return row.chatId===proposal.chatId&&row.historyLoading===null&&row.composerEnabled&&row.stopCount===0
  })
  step('clear-composer-fill')
  await composer.fill(text)
  step('clear-composer-readiness')
  await wait(async()=>{
   const row=await observeComposer('filled')
   assert.equal(row.chatId,proposal.chatId);assert.equal(row.historyLoading,null)
   assert(row.exactText,'Clear composer must retain the single filled message')
   return row.composerEnabled&&row.sendEnabled&&row.stopCount===0
  })
  const ready=await observeComposer('before-dispatch')
  assert.equal(ready.chatId,proposal.chatId);assert.equal(ready.historyLoading,null)
  assert(ready.exactText&&ready.composerEnabled&&ready.sendEnabled&&ready.stopCount===0)
  step('clear-dispatch')
  const [response]=await Promise.all([page.waitForResponse(res=>new URL(res.url()).pathname==='/api/assistant-runs'&&res.request().method()==='POST',{timeout:left()}),send.click({timeout:left()})])
  assert.equal(response.status(),201);admissionBearer=await response.request().headerValue('authorization');r.admissionCredentials??=[];r.admissionCredentials.push({ordinal:starts,capturedMatchesAdmission:bearer===admissionBearer});writePrivate(c.reportPath,r);const admission=await response.json()
  // Terminal stream EOF can precede saved-chat acknowledgement. Observe the
  // settled UI before read-only acknowledgement checks, then use the offered
  // explicit recovery once if the composer still needs reconciliation.
  step('clear-response-reconciliation')
  await wait(async()=>{
   const row=await observeComposer('terminal-ui')
   assert.equal(row.chatId,proposal.chatId)
   return row.historyLoading===null&&row.stopCount===0&&(row.composerEnabled||row.reconnectEnabled)
  })
  step('clear-terminal-readback')
  let pendingRun
  r.clearTerminal=[]
  await wait(async()=>{
   pendingRun=await api('/api/assistant-runs/'+admission.id)
   assert.equal(pendingRun.id,admission.id);assert.equal(pendingRun.owner.kind,'chat');assert.equal(pendingRun.owner.id,proposal.chatId)
   assert.equal(pendingRun.requestId,admission.requestId);assert.equal(pendingRun.admissionId,admission.admissionId)
   const ready=projectionReady(pendingRun,r.clearTerminal,Date.now()-started)
   writePrivate(c.reportPath,r);return ready
  })
  assert.equal(pendingRun.operations.filter(o=>o.kind==='model').length,2)
  assert.equal(starts,2);assert.equal(reviews,1)
  const reconciledUI=await observeComposer('acknowledged-ui')
  assert.equal(reconciledUI.chatId,proposal.chatId);assert.equal(reconciledUI.historyLoading,null);assert.equal(reconciledUI.stopCount,0)
  r.clearReconnect={clicked:false,startsBefore:starts,reviewsBefore:reviews}
  if(!reconciledUI.composerEnabled){
   assert.equal(reconciledUI.reconnectCount,1);assert.equal(reconciledUI.reconnectEnabled,true)
   step('clear-explicit-reconnect')
   r.clearReconnect.clicked=true;writePrivate(c.reportPath,r)
   await reconnect.click({timeout:left()})
  }
  await wait(async()=>{
   const row=await observeComposer('reconciled-ui')
   assert.equal(row.chatId,proposal.chatId);assert.equal(row.historyLoading,null)
   assert.equal(starts,2);assert.equal(reviews,1)
   return row.composerEnabled&&row.stopCount===0
  })
  r.clearReconnect.startsAfter=starts;r.clearReconnect.reviewsAfter=reviews;writePrivate(c.reportPath,r)
  const pendingOps=digest(pendingRun.operations)
  before=await observe('clear-saved-and-pending');assert.equal(before.content.entries.length,1);assert.equal(before.proposals.length,1)
  const pending=before.proposals[0];assert.equal(pending.runId,admission.id)
  region=await memoryUI()
  await region.getByRole('button',{name:'Clear memory',exact:true}).click()
  let dialog=page.getByRole('dialog',{name:'Clear project memory?',exact:true})
  const writesBeforeCancel=r.writes.length
  await dialog.getByRole('button',{name:'Cancel',exact:true}).click()
  assert.equal(r.writes.length,writesBeforeCancel);assert.equal((await observe('clear-cancel')).sha256,before.sha256)
  await region.getByRole('button',{name:'Clear memory',exact:true}).click()
  dialog=page.getByRole('dialog',{name:'Clear project memory?',exact:true})
  await uiWrite(dialog.getByRole('button',{name:'Clear memory',exact:true}),'clear')
  after=await observe('clear-confirm');assert.equal(after.content.entries.length,0);assert.equal(after.proposals?.length??0,0)
  assert(after.content.generationAfter);assert.deepEqual(after.content.reviews,[receipt])
  const reconciled=await memoryPost(completedIntent.body,completedIntent.revision)
  assert.equal(reconciled.sha256,after.sha256);assert.equal(reconciled.content.entries.length,0)
  // A fresh intent for the excluded pending source must refuse at the current revision.
  const blocked={action:'review',review:{id:crypto.randomUUID(),runId:pending.runId,runRevision:pending.runRevision,proposalId:pending.memory.id,proposalDigest:pending.memory.digest,decision:'accept'}}
  const refusal=await page.request.post(c.origin+'/api/ai-memory',{headers:{Authorization:bearer,'If-Match':after.sha256},data:blocked,timeout:left()})
  r.writes.push({body:blocked,revision:after.sha256,expectedRefusal:true})
  assert.equal(refusal.status(),409);r.clearBarrierStatus=refusal.status()
  assert.equal((await api('/api/assistant-runs/'+admission.id)).revision,pendingRun.revision)
  assert.equal(digest((await api('/api/assistant-runs/'+admission.id)).operations),pendingOps)
 }
 region=await memoryUI({reload:true})
 const final=await observe('document-reload')
 assert.equal(final.sha256,after.sha256)
 assert.deepEqual(final.content.reviews,[receipt])
 assert.equal(final.proposals?.length??0,0)
 const expectedEntries=['stale','lost-ack'].includes(c.scenario)?1:0
 assert.equal(final.content.entries.length,expectedEntries)
 await wait(async()=>expectedEntries?await region.getByRole('button',{name:'Forget',exact:true}).count()===1:await region.getByText('No memories match this view.',{exact:true}).count()===1)
 assert.equal(digest((await api('/api/assistant-runs/'+runId)).operations),r.operationsSha256)
 assert.equal((await api('/api/assistant-runs/'+runId)).revision,run.revision)
 assert.equal(starts,limits.starts);assert.equal(errors,0);assert.equal(unexpected,0)
 assert.equal(r.writes.filter(w=>w.body.action!=='settings').length,limits.memoryPosts)
 r.memorySha256=final.sha256
}
const timer=setTimeout(()=>void browser?.close(),limits.seconds*1000)
try{
 await (async()=>{
 checkpoint('launch')
 browser=await chromium.launch({executablePath:c.browserBin,args:['--no-sandbox'],timeout:left()})
 const context=await browser.newContext({viewport:{width:1600,height:1000},locale:'en-CA'})
 page=await context.newPage();page.setDefaultTimeout(15000)
 await context.route('**/*',async route=>{const u=new URL(route.request().url());if(u.origin!==c.origin||/\/api\/model-providers\/.*\/(?:login|install|logout)/.test(u.pathname)){unexpected++;await route.abort()}else await route.continue()})
 page.on('pageerror',()=>errors++)
 page.on('request',q=>{const p=new URL(q.url()).pathname;if(p==='/api/conversations'||p==='/api/ai-connections')void q.headerValue('authorization').then(v=>{bearer=v??bearer});if(p==='/api/assistant-runs'&&q.method()==='POST'){starts++;if(starts>limits.starts)unexpected++}if(p==='/api/ai-memory'&&q.method()==='POST'){try{const body=q.postDataJSON();r.writes.push({body,revision:q.headers()['if-match']??null});if(body.action==='review')reviews++;const decisions=r.writes.filter(w=>w.body.action!=='settings');if(decisions.length>limits.memoryPosts||r.writes.length-decisions.length>limits.settingsPosts)unexpected++}catch{unexpected++}}})
 checkpoint('owner-setup')
 await page.goto(c.origin,{timeout:left()})
 await page.getByRole('button',{name:'Set up sign-in',exact:true}).click()
 await page.getByLabel('Owner setup code',{exact:true}).fill(c.setupCode)
 await page.getByRole('button',{name:'Continue',exact:true}).click()
 await page.getByRole('button',{name:'Set password',exact:true}).click()
 const password=page.getByRole('dialog',{name:'Set password',exact:true})
 await password.getByLabel('New password',{exact:true}).fill('Fixture7')
 await password.getByLabel('Confirm new password',{exact:true}).fill('Fixture7')
 await password.getByRole('button',{name:'Set password',exact:true}).click()
 checkpoint('owner-sign-in')
 await page.getByLabel('Desk password',{exact:true}).fill('Fixture7')
 await signInAndOpenConnections(page,c.origin,async(_step,_category,action)=>action())
 await wait(()=>!!bearer);r.ui.normalSignIn=true
 checkpoint('memory-settings')
 let region=await memoryUI()
 await setMemorySetting(region,'use','Use saved memory',false)
 await setMemorySetting(region,'suggest','Suggest memories',true)
 step('settings-final-readback')
 await wait(async()=>{const m=await api('/api/ai-memory');return m.content.settings.suggest&&!m.content.settings.use})
 step('settings-empty');const empty=await api('/api/ai-memory');assert.equal(empty.content.entries.length,0);assert.equal(empty.proposals?.length??0,0)
 checkpoint('chat-settings')
 step('pack-navigation');await page.goto(c.origin+'/packs/one',{timeout:left()})
 const pane=page.locator('section[data-chat-id][data-placement="pane"]').first()
 step('composer-ready');const composer=pane.getByPlaceholder('Ask about this pack…');await composer.waitFor()
 step('chat-mode');await pane.getByRole('combobox',{name:'Task tools',exact:true}).click();await page.getByRole('option',{name:'Chat',exact:true}).click()
 step('chat-options');await pane.getByRole('button',{name:'Assistant settings',exact:true}).click()
 step('chat-research');await page.getByRole('combobox',{name:'Research',exact:true}).click();await page.getByRole('option',{name:'Provided sources only',exact:true}).click()
 step('chat-review');await page.getByRole('combobox',{name:'Adversarial review',exact:true}).selectOption('off')
 // Unsubmitted composers live in UI draft state until normal submission.
 // Observe the selected controls here; assert saved settings after dispatch.
 step('chat-settings-ui')
 assert.equal(await pane.getByRole('combobox',{name:'Task tools',exact:true}).innerText(),'Chat')
 assert.equal(await page.getByRole('combobox',{name:'Research',exact:true}).innerText(),'Provided sources only')
 assert.equal(await page.getByRole('combobox',{name:'Adversarial review',exact:true}).inputValue(),'off')
 await page.keyboard.press('Escape')
 checkpoint('dispatch')
 await composer.fill('Remember that I prefer concise outreach. Propose the preference for my review, then finish.')
 const [response]=await Promise.all([page.waitForResponse(res=>new URL(res.url()).pathname==='/api/assistant-runs'&&res.request().method()==='POST',{timeout:left()}),composer.press('Enter')])
 assert.equal(response.status(),201);admissionBearer=await response.request().headerValue('authorization');r.admissionCredentials??=[];r.admissionCredentials.push({ordinal:starts,capturedMatchesAdmission:bearer===admissionBearer});writePrivate(c.reportPath,r);const admission=await response.json();assert(/^arun_[a-f0-9]+$/.test(admission.id));runId=admission.id;r.runId=runId
 checkpoint('terminal')
 let run
 await wait(async()=>{run=await api('/api/assistant-runs/'+runId);return projectionReady(run,r.terminal,Date.now()-started)})
 assert.equal(run.operations.filter(o=>o.kind==='model').length,2)
 assert(run.operations.every(o=>o.state==='completed'))
 r.operationsSha256=digest(run.operations)
 const saved=await api('/api/conversations'),chat=saved.content.chats.find(v=>v.id===run.owner.id)
 assert(chat);assert.equal(chat.model,'gpt-6-sol');assert.equal(chat.critic,'off');assert.equal(chat.researchMode,'provided')
 let before=await api('/api/ai-memory')
 assert.equal(before.content.entries.length,0);assert.equal(before.content.reviews?.length??0,0);assert.equal(before.proposals.length,1)
 const proposal=before.proposals[0]
 assert.equal(proposal.runId,runId);assert.equal(proposal.chatId,run.owner.id);assert.equal(proposal.turnId,run.requestId);assert.equal(proposal.reviewable,true)
 assert.equal(proposal.memory.kind,'preference');assert.equal(proposal.memory.text,'Keep outreach concise.')
 r.proposal={id:proposal.memory.id,digest:proposal.memory.digest,runRevision:proposal.runRevision}
 if(c.scenario!=='accept'){await management(context,run,proposal,before);r.state='complete';r.phase='complete';return}
 checkpoint('pending-reload')
 region=await memoryUI({reload:true})
 await region.getByRole('button',{name:'Remember',exact:true}).waitFor()
 assert.equal(await region.getByText('Keep outreach concise.',{exact:true}).count(),1)
 before=await api('/api/ai-memory');assert.equal(before.content.entries.length,0);assert.equal(before.proposals[0].memory.id,r.proposal.id)
 assert.equal(reviews,0);assert.equal(starts,1);assert.equal(digest((await api('/api/assistant-runs/'+runId)).operations),r.operationsSha256)
 r.ui.pendingAfterReload=true
 checkpoint('accept')
 step('accept-write')
 const [reviewResponse]=await Promise.all([page.waitForResponse(res=>new URL(res.url()).pathname==='/api/ai-memory'&&res.request().method()==='POST',{timeout:left()}),region.getByRole('button',{name:'Remember',exact:true}).click()])
 r.responseStatus=reviewResponse.status();writePrivate(c.reportPath,r);assert(reviewResponse.ok());await reviewResponse.finished()
 step('accept-readback')
 let accepted
 await wait(async()=>{accepted=await api('/api/ai-memory');return accepted.content.entries.length===1})
 const entry=accepted.content.entries[0],receipt=accepted.content.reviews[0]
 assert.equal(accepted.proposals?.length??0,0);assert.equal(accepted.content.reviews.length,1)
 assert.equal(entry.text,'Keep outreach concise.');assert.equal(entry.state,'active');assert.equal(entry.canonical.id,r.proposal.id);assert.equal(entry.canonical.digest,r.proposal.digest)
 assert.equal(entry.source.chatId,run.owner.id);assert.equal(entry.source.turnId,run.requestId)
 assert.equal(receipt.decision,'accept');assert.equal(receipt.proposalId,r.proposal.id);assert.equal(receipt.proposalDigest,r.proposal.digest)
 r.accepted={id:entry.id,memoryId:entry.canonical.id,digest:entry.canonical.digest,receiptId:receipt.id};r.memorySha256=accepted.sha256;r.ui.explicitAcceptance=true
 step('accept-ui');await region.getByRole('button',{name:'Forget',exact:true}).waitFor()
 checkpoint('accepted-reload')
 region=await memoryUI({reload:true})
 await region.getByRole('button',{name:'Forget',exact:true}).waitFor()
 assert.equal(await region.getByRole('button',{name:'Remember',exact:true}).count(),0)
 assert.equal(await region.getByText('Keep outreach concise.',{exact:true}).count(),1)
 assert.equal((await api('/api/ai-memory')).sha256,r.memorySha256)
 assert.equal(digest((await api('/api/assistant-runs/'+runId)).operations),r.operationsSha256)
 assert.equal(starts,1);assert.equal(reviews,1);assert.equal(errors,0);assert.equal(unexpected,0)
 r.ui.acceptedAfterReload=true;r.state='complete';r.phase='complete'
 })()
}catch(error){
 // Caught transport/assertion prose may contain request headers or payloads.
 // Persist only a closed classification, never the caught message or stack.
 r.errorName=['Error','TimeoutError','AssertionError','TypeError','RangeError'].includes(error?.name)?error.name:'Other'
 r.errorCategory=r.errorName==='TimeoutError'?'timeout':r.errorName==='AssertionError'?'assertion':'driver-failure'
}
finally{
 clearTimeout(timer)
 try{await browser?.close()}catch{r.state='incomplete';r.phase='cleanup';r.errorName='Error'}
 r.counts={starts,reviews,browserErrors:errors,unexpectedRequests:unexpected};r.elapsedMs=Date.now()-started
 writePrivate(c.reportPath,r)
}
process.stdout.write('offline memory review '+r.state+': '+r.phase+'\n')
if(r.state!=='complete')process.exitCode=1
