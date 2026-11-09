import assert from 'node:assert/strict'
import {chromium} from 'playwright-core'

const [origin,setupCode,executablePath]=process.argv.slice(2)
assert(origin&&setupCode&&executablePath,'Use TestStorageBudgetStatusBrowser with explicitly supplied tools')
assert.equal(new URL(origin).hostname,'127.0.0.1','Disposable loopback server only')
const browser=await chromium.launch({executablePath,args:['--no-sandbox']})
const context=await browser.newContext({viewport:{width:1440,height:1000}})
const page=await context.newPage()
page.setDefaultTimeout(15000)
let browserErrors=0,externalAttempts=0,projection='real',projectionMarker,session
let projectionResponses=0,realBrowserResponses=0,locationReads=0
page.on('pageerror',()=>browserErrors++)
page.on('request',request=>{if(new URL(request.url()).pathname==='/api/storage')locationReads++})
// No network/provider fixture. Abort foreign HTTP(S) before dispatch and fail.
await context.route('**/*',async route=>{
 const url=new URL(route.request().url())
 if(['http:','https:'].includes(url.protocol)&&url.origin!==origin){externalAttempts++;await route.abort();return}
 await route.continue()
})
const max='18446744073709551615',shownMax='18,446,744,073,709,551,615'
// Handwritten DISPLAY projection, separate from the server/Q observation.
// No pressure, grants, tickets, frozen state or authority is synthesized here.
function displayFixture(mode,marker){
 const zero={bytes:'0',files:'0',directories:'0'}
 const amounts={known:true,chargedLogical:{bytes:max,files:'1',directories:'0'},promisedPeak:zero,promisedSettlement:zero,limit:{bytes:'9007199254740992',files:'10',directories:'10'},logicalRemaining:zero,effectiveLogicalRemaining:zero,overBudget:true}
 const installation={...amounts,id:'11111111111111111111111111111111',generation:'1'}
 const workspace={...amounts,id:'22222222222222222222222222222222',generation:'1'}
 const at=marker,unknown={value:null,unknown:'not-measured'}
 return {childObservation:{coverage:'selected-child-roots-only',enforcement:'warning-only',state:mode==='stale'?'stale':'partial',observedAt:'2026-10-31T11:59:00Z',lowSpaceThresholdBytes:'1073741824',roots:[{component:'runner',role:'workspace',state:'measured',reason:'none',logicalBytes:'5',files:'1',directories:'1',availableBytes:'0',lowSpace:true},{component:'native',role:'profile',state:'unavailable',reason:'unknown',logicalBytes:null,files:null,directories:null,availableBytes:null,lowSpace:null}]},version:1,availability:mode==='unavailable'?'unavailable':mode==='stale'?'stale':'available',reason:mode==='maximum'?null:'storage-busy',readOnly:mode==='unavailable',servedAt:at,
  pressure:{active:false,codes:[],firstSeenAt:null,lastSeenAt:null,durableState:'unconfirmed'},
  snapshot:mode==='unavailable'?null:{observationSession:'33333333333333333333333333333333',observationSequence:max,observedAt:at,policyRevision:max,epoch:'1',maintenance:false,
   coverage:'registered-scope-only',measurement:{at:null,generation:'1',freshness:'unknown'},installation,currentWorkspace:workspace,
   categories:['history','retained','attachments','memory','control','disposable','binaries','transient','settlement'].map(category=>({...amounts,category})),
   currentWorkspaceCategories:['history','retained','attachments','memory','control','disposable','binaries','transient','settlement'].map(category=>({...amounts,category})),
   workspaces:[workspace],roots:[],filesystems:[],constraints:[],retainedLogical:unknown,physicalAllocated:unknown,sharedAttribution:{state:'not-measured',value:null},reclaimable:{state:'not-measured',eligibleLogical:null,allocatedEstimate:null},recovery:{pendingRecoveryCount:null,pendingRecoveryState:'not-measured',uncertainTicketCount:0,activeTicketCount:0,terminalAwaitingCheckpointCount:0}}}
}
// In real mode every status response is the actual authenticated application
// route. In projection modes route.fetch still runs it, but the substituted
// display body is NEVER counted as Q/status/pressure acceptance evidence.
await page.route(origin+'/api/storage/status',async route=>{
 if(projection==='real'){realBrowserResponses++;await route.continue();return}
 const mode=projection,marker=projectionMarker,response=await route.fetch()
 assert(response.ok(),'Underlying actual diagnostic must stay readable')
 projectionResponses++
 await route.fulfill({response,headers:{...response.headers(),'x-storage-display-fixture':marker},json:displayFixture(mode,marker)})
})
async function api(path,method='GET',body,revision){
 const response=await context.request.fetch(origin+path,{method,headers:{Authorization:'Bearer '+session,Origin:origin,...(revision?{'If-Match':revision}:{}),'Content-Type':'application/json'},...(body===undefined?{}:{data:body}),timeout:15000})
 // Report only fixed request identity, status and an allowlisted Desk code.
 // Never retain response bodies, bearer values or arbitrary error prose.
 const refusal=response.headers()['x-jpack-desk-refusal']
 const code=['unauthorized','forbidden','stale','assistant-unusable-store','internal','outside-root','too-large','storage-busy','storage-unavailable','storage-read-only'].includes(refusal)?refusal:refusal===undefined?'absent':'other'
 assert(response.ok(),`Actual application API refused fixture operation: ${method} ${path}; HTTP ${response.status()}; refusal ${code}`)
 return response.json()
}
async function control(action,method='POST'){
 const response=await context.request.fetch(origin+'/__storage_fixture/'+action,{method,headers:{Authorization:'Bearer '+setupCode},timeout:15000})
 assert(response.ok(),'Disposable fixture control failed');return response.json()
}
async function focusRefresh(marker){
 const response=page.waitForResponse(async response=>new URL(response.url()).pathname==='/api/storage/status'&&(!marker||await response.headerValue('x-storage-display-fixture')===marker))
 await page.evaluate(()=>{
  window.dispatchEvent(new Event('focus'))
  // The pinned query-core focus manager listens to visibilitychange. Keep
  // document visibility real; this event asks for the normal visible refresh.
  document.dispatchEvent(new Event('visibilitychange',{bubbles:true}))
 })
 const completed=await response
 assert(completed.ok(),'Status refresh must remain readable')
 assert.equal(await completed.finished(),null,'Status response body must complete')
 if(marker)assert.equal((await completed.json()).servedAt,marker,'Consume the selected display response body')
}
async function refreshButton(){
 const response=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/storage/status')
 await page.getByRole('button',{name:'Refresh storage status',exact:true}).click()
 const completed=await response
 assert(completed.ok())
 assert.equal(await completed.finished(),null,'Status response body must complete before navigation')
 await completed.body()
}
const counterRows=()=>page.getByText(new RegExp(shownMax.replaceAll(',','[,]')+' bytes'))
const readOnlyProjectionNotice=()=>page.getByText('Storage is read only. Existing data remains visible; status refresh does not resume paused work.',{exact:true})
// Handwritten as-of markers distinguish each available/stale projection render.
// Unavailable supplies a fresh readOnly (false -> true) render marker while
// retaining the prior as-of
// identity and all 20 amount rows (workspace + installation + nine of each).
// Neither these markers nor the fixture response header are backend evidence.
async function consumeProjection(mode,marker,retainedMarker=marker){
 // An unavailable marker must be newly rendered, never inherited from the
 // previous maximum/stale response. This precondition defeats old-row matches.
 if(mode==='unavailable')await readOnlyProjectionNotice().waitFor({state:'hidden'})
 projection=mode;projectionMarker=marker
 await focusRefresh(marker)
 await readOnlyProjectionNotice().waitFor({state:mode==='unavailable'?'visible':'hidden'})
 const observation=page.locator(`time[datetime="${retainedMarker}"]`)
 await observation.waitFor()
 assert.equal(await observation.textContent(),retainedMarker,'Keep the exact prior as-of observation')
 // Query isFetching drives this existing UI button. Body completion by itself
 // cannot satisfy this boundary or the fresh rendered read-only transition.
 await page.waitForFunction(()=>[...document.querySelectorAll('button')].some(button=>button.textContent==='Refresh storage status'&&!button.disabled))
 await counterRows().first().waitFor()
 assert.equal(await counterRows().count(),20,'All prior maximum amount rows must remain rendered')
 assert.equal(await observation.count(),1,'Keep one exact as-of observation')
 await page.getByText('Low filesystem space observed for child storage. Available space is below 1 GiB. Child growth is not prevented.',{exact:true}).waitFor()
 await page.getByText('Warning only. Child storage growth is measured where available and is not prevented. These observations do not reserve capacity or change Desk limits.',{exact:true}).waitFor()
 if(mode==='unavailable')assert.equal(await page.locator(`time[datetime="${marker}"]`).count(),0,'Unavailable must not mint a replacement observation')
}
try{
 await page.goto(origin)
 await page.getByRole('button',{name:'Set up sign-in',exact:true}).click()
 await page.getByLabel('Owner setup code',{exact:true}).fill(setupCode)
 // Sign-in navigates as soon as the page consumes the setup reply. Read
 // the actual body before delivery so navigation cannot discard it first.
 await page.route(origin+'/api/auth/setup',async route=>{
  const response=await route.fetch()
  assert(response.ok(),'Actual owner setup must succeed')
  session=(await response.json()).id
  await route.fulfill({response})
 },{times:1})
 const setup=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/auth/setup'&&response.request().method()==='POST')
 await page.getByRole('button',{name:'Continue',exact:true}).click()
 const setupResponse=await setup;assert(setupResponse.ok())
 assert.equal(typeof session,'string')
 await page.getByRole('button',{name:'Set password',exact:true}).waitFor()
 const initial=await api('/api/conversations')
 const chat={id:'storage-save-failure',title:'Storage save fixture',pinned:false,archived:false,updatedAt:'2026-10-31T12:00:00Z',composer:'saved original',model:'',mode:'draft',view:'chat'}
 await api('/api/conversations','PUT',{version:1,chats:[chat]},initial.sha256)
 // Reload hydration before editing; no fabricated chat checkpoint or typed pause.
 await page.goto(origin+'/admin#storage')
 await page.getByRole('button',{name:'Change location…',exact:true}).waitFor()
 const actual=await api('/api/storage/status')
 assert(actual.snapshot,'Actual supported New must return a Q snapshot')
 assert.equal(actual.version,1);assert.equal(actual.pressure.active,false)
 assert(actual.childObservation,'Actual selected child-root observation must be available independently of Q')
 assert.equal(actual.childObservation.enforcement,'warning-only')
 assert.equal(actual.childObservation.coverage,'selected-child-roots-only')
 assert(actual.childObservation.roots.some(row=>row.component==='runtime'),'Actual Runtime selection must be represented')
 const location=await api('/api/storage')
 assert.equal(Boolean(location.managed),false,'Move journey needs original unmanaged supported setup')
 await page.getByRole('button',{name:'Change location…',exact:true}).click()
 const move=page.getByRole('dialog',{name:'Move chat data',exact:true})
 await move.getByLabel('New folder',{exact:true}).fill('/fixture/move-destination')
 const readsBefore=locationReads
 await consumeProjection('maximum','2026-10-31T12:00:01Z');await counterRows().first().waitFor()
 assert.equal(await page.getByText('Over budget',{exact:true}).first().locator('xpath=following-sibling::dd[1]').innerText(),'Yes')
 await consumeProjection('stale','2026-10-31T12:00:02Z');await page.getByText(/^Stale storage observation/).waitFor()
 await consumeProjection('unavailable','2026-10-31T12:00:03Z','2026-10-31T12:00:02Z');await counterRows().first().waitFor()
 assert.equal(await move.getByLabel('New folder',{exact:true}).inputValue(),'/fixture/move-destination')
 assert.equal(locationReads,readsBefore,'Counter refresh must not scan legacy location')
 assert((await page.title()).startsWith('* '),'Manual storage edit must retain dirty registry')
 // Inspect the REAL UI-produced request, then abort before application mutation.
 // This is request-envelope evidence, not a successful server move/restore.
 let moveEnvelope
 await page.route(origin+'/api/storage/move',async route=>{moveEnvelope=route.request().postDataJSON();await route.abort()})
 const moveRequest=page.waitForRequest(origin+'/api/storage/move')
 await move.getByRole('button',{name:'Move data',exact:true}).click();await moveRequest
 await move.getByRole('button',{name:'Reload settings',exact:true}).waitFor()
 assert.deepEqual(moveEnvelope,{path:'/fixture/move-destination',revision:location.revision})
 assert.equal(await move.getByLabel('New folder',{exact:true}).inputValue(),'/fixture/move-destination')
 await move.getByLabel('New folder',{exact:true}).fill(location.path)
 await move.getByRole('button',{name:'Cancel',exact:true}).click()
 await page.getByRole('button',{name:'Restore backup…',exact:true}).click()
 const restore=page.getByRole('dialog',{name:'Restore chat backup',exact:true})
 const file=restore.getByLabel('Chat backup',{exact:true})
 await file.setInputFiles({name:'handwritten-fixture.zip',mimeType:'application/zip',buffer:Buffer.from('synthetic fixture, never submitted to the server')})
 await file.evaluate(element=>{globalThis.__storageFixtureFile=element.files[0]})
 await restore.getByLabel('Restore into',{exact:true}).fill('/fixture/restore-destination')
 await restore.getByLabel('Type Yes to confirm.',{exact:true}).fill('Yes')
 const restoreLocation=await api('/api/storage')
 await consumeProjection('maximum','2026-10-31T12:00:04Z');await counterRows().first().waitFor()
 await consumeProjection('unavailable','2026-10-31T12:00:05Z','2026-10-31T12:00:04Z');await counterRows().first().waitFor()
 assert.equal(await file.evaluate(element=>element.files[0]===globalThis.__storageFixtureFile),true,'Refresh replaced File object')
 assert.equal(await restore.getByLabel('Restore into',{exact:true}).inputValue(),'/fixture/restore-destination')
 assert.equal(await restore.getByLabel('Type Yes to confirm.',{exact:true}).inputValue(),'Yes')
 assert((await page.title()).startsWith('* '),'Restore setup must retain dirty registry after unavailable consumption')
 let restoreEnvelope
 // Capture the actual multipart request, then abort before server dispatch.
 // The body contains only this fixed synthetic file and handwritten settings.
 await page.route(origin+'/api/storage/restore',async route=>{
  const request=route.request(),contentType=request.headers()['content-type']
  const boundary=/boundary=(?:"([^"]+)"|([^;]+))/.exec(contentType)
  assert(boundary,'UI must send multipart FormData')
  const parts=request.postDataBuffer().toString('utf8').split('--'+(boundary[1]??boundary[2]))
  const settings=parts.find(part=>part.includes('name="settings"'))
  const backup=parts.find(part=>part.includes('name="backup"'))
  assert(settings&&backup,'Restore must retain both selected inputs')
  restoreEnvelope={settings:JSON.parse(settings.split('\r\n\r\n')[1].replace(/\r\n$/,'')),name:/filename="([^"]+)"/.exec(backup)?.[1]}
  await route.abort()
 })
 const restoreRequest=page.waitForRequest(origin+'/api/storage/restore')
 await restore.getByRole('button',{name:'Restore and reload',exact:true}).click();await restoreRequest
 // Aborted fetch has settled when the action becomes enabled again.
 await page.waitForFunction(()=>[...document.querySelectorAll('[role="dialog"] button')].some(button=>button.textContent==='Restore and reload'&&!button.disabled))
 assert.deepEqual(restoreEnvelope,{settings:{path:'/fixture/restore-destination',revision:restoreLocation.revision},name:'handwritten-fixture.zip'})
 assert.equal(await file.evaluate(element=>element.files[0]===globalThis.__storageFixtureFile),true)
 await file.setInputFiles([]);await restore.getByLabel('Restore into',{exact:true}).fill('')
 await restore.getByRole('button',{name:'Cancel',exact:true}).click()
 projection='real';await refreshButton()
 // A home draft is unsent and browser-owned; full reload must preserve it.
 await page.goto(origin)
 // A visible composer also exists on the locked loading-history placeholder.
 // Read only the hydrated main chat, then require the same draft after reload.
 const homePanel=page.locator('section[data-placement="main"][data-chat-id]:not([data-history-loading])')
 const home=homePanel.getByRole('textbox',{name:'Message the assistant',exact:true})
 await home.fill('An unsent browser draft survives diagnostic focus')
 const homeId=await homePanel.getAttribute('data-chat-id')
 assert(homeId&&homeId!=='loading-history','Home edits must belong to a hydrated draft')
 await focusRefresh();assert.equal(await home.inputValue(),'An unsent browser draft survives diagnostic focus')
 await page.reload();await home.waitFor()
 assert.equal(await homePanel.getAttribute('data-chat-id'),homeId,'Reload must restore the same unsent home draft')
 assert.equal(await home.inputValue(),'An unsent browser draft survives diagnostic focus')
 // Clear the home draft before leaving; saved-chat failure stays in memory.
 await home.fill('')
 await page.goto(origin+'/chats/'+chat.id)
 const composer=page.locator(`section[data-placement="main"][data-chat-id="${chat.id}"]:not([data-history-loading])`).getByRole('textbox',{name:'Message the assistant',exact:true})
 await composer.waitFor()
 assert.equal(await composer.inputValue(),'saved original')
 const persisted=await api('/api/conversations')
 await control('arm');await control('journal-fail')
 const failedSave=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/conversations'&&response.request().method()==='PUT')
 await composer.fill('Unsent edits retained after actual save ENOSPC')
 assert.equal((await failedSave).ok(),false,'Actual save must fail before staging')
 await page.locator('.desk-dirty').waitFor()
 assert((await page.title()).startsWith('* '),'Failed autosave must retain dirty registry')
 await focusRefresh()
 const notices=page.locator('[data-desk-notices]')
 await notices.click()
 const panel=page.getByRole('dialog')
 await panel.getByText('Chat changes could not be saved. Keep this window open until saving succeeds.',{exact:true}).waitFor()
 await panel.getByRole('link',{name:'Review storage',exact:true}).waitFor()
 assert.equal(await panel.getByRole('link',{name:'Review storage',exact:true}).count(),1,'Pressure must coalesce')
 assert.equal(await panel.getByRole('link',{name:'Review storage',exact:true}).getAttribute('href'),'/admin#storage')
 await page.keyboard.press('Escape')
 const pressured=await api('/api/storage/status')
 assert(pressured.snapshot);assert.deepEqual(pressured.pressure.codes,['storage-disk-full']);assert(pressured.pressure.active)
 const old=await api('/api/conversations')
 assert.equal(old.sha256,persisted.sha256);assert.deepEqual(old.content,persisted.content)
 await control('freeze')
 for(let i=0;i<4;i++){
  await focusRefresh()
  assert.equal(await composer.inputValue(),'Unsent edits retained after actual save ENOSPC')
  assert((await page.title()).startsWith('* '));assert.equal(await page.locator('.desk-dirty').count(),1)
  const again=await api('/api/storage/status')
  assert(again.snapshot);assert.deepEqual(again.pressure,pressured.pressure)
 }
 await control('verify')
 const trace=await control('trace','GET')
 assert.equal(trace.overflow,false);assert.equal(trace.workspaceRefusals,1);assert.equal(trace.createRefusals,1);assert.equal(trace.updateRefusals,1);assert.equal(trace.forbiddenDispatches,0)
 const diagnostics=trace.rows.filter(row=>row.route==='storage-status')
 assert(diagnostics.length>=8&&diagnostics.length<=64,'Bounded real server diagnostic count')
 assert(diagnostics.every(row=>row.method==='GET'&&row.status===200&&row.bytes>0&&row.bytes<=8388608),'Real server traces must remain bounded')
 assert.equal(browserErrors,0);assert.equal(externalAttempts,0)
 console.log(JSON.stringify({kind:'BROWSER',actualAuthenticatedStatus:true,actualSavePreStageENOSPC:true,actualDefaultJournalCreateAndUpdateENOSPC:true,pressureFromProductionReporter:true,oneStorageHint:true,dirtyMoveAndRestoreRetained:true,frozenRevisionEnvelopes:true,restoreFileIdentity:true,unsentHomeDraftReload:true,failedSaveRetainedAcrossDiagnosticRefreshAndFocus:true,noRetryOrProviderOrEffectDispatch:true,serverTraceRows:trace.rows.length,realBrowserResponses,fixtureOnlyDisplayProjection:{responses:projectionResponses,maximumDecimal:true,overBudget:true,stale:true,unavailableRetainedRows:true,childWarningOnly:true,childUnknownNotZero:true},limitations:['No notification writer exists; no failure survival claim for one','No postcommit failure, real disk exhaustion, typed AI pause or all-writer coverage','Full reload of failed saved-chat changes is destructive and is not attempted']}))
}finally{
 // Closing the disposable browser ends the journey; no UI unload confirmation
 // is used as permission to discard an actual user's failed save.
 await browser.close()
}
