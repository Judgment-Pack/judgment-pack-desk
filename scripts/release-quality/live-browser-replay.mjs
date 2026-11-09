// Concrete two-chat inference capture. The caller retains the already signed-in
// page/context from normal Desk owner and named-connection setup. Importing this
// module never launches a browser, signs in, or grants execution authority.
import assert from 'node:assert/strict'
import {constants, openSync, closeSync, writeFileSync, renameSync} from 'node:fs'
import {join, isAbsolute} from 'node:path'
import {pathToFileURL} from 'node:url'
import {readPrivate, writePrivate, parseClosed, parseApplicationSnapshot, keys, digest, captureTools} from './replay-report.mjs'
import {privateCanonicalRetainer,validateCanonicalEvidence,canonicalReference} from './live-canonical-evidence.mjs'
import {fixedInputs} from './live-comparison-plan.mjs'

const refuse = code => { throw Object.assign(new Error(code), {code}) }
const same = (a,b) => assert.deepEqual(a,b)
const id = value => typeof value==='string' && /^[A-Za-z0-9_.:/-]{1,128}$/.test(value)
const sha = value => typeof value==='string' && /^[a-f0-9]{64}$/.test(value)
const SLICE = '7728e5f53f9539c0343a86d6b4aa9351b1c8aad662acf35cd495dca36c91fe24'
const sleep = () => new Promise(resolve=>setTimeout(resolve,25))
const errors = new Set(['control','readiness','settings','fresh-chat','dispatch','terminal','answers','reload','accounting','owner','deadline','unexpected-request','browser-error','evidence','cleanup'])

export function validateLiveControl(c) {
 parseClosed(JSON.stringify(c))
 keys(c,['kind','role','model','effort','connectionId','connectionName','projectRoot','controlDir','evidenceDir','ownerConfigurationSha256','source','inputs','baselineReader'])
 if(c.kind!=='live-two-chat-capture-input'||!['baseline','candidate'].includes(c.role)||!id(c.model)||!['none','minimal','low','medium','high','xhigh','max'].includes(c.effort)||!id(c.connectionId)||typeof c.connectionName!=='string'||!c.connectionName||c.connectionName.length>128||!isAbsolute(c.projectRoot)||!isAbsolute(c.controlDir)||!sha(c.ownerConfigurationSha256))refuse('control')
 if(!isAbsolute(c.evidenceDir)||c.evidenceDir===c.controlDir)refuse('control')
 keys(c.source,['commit','tree','inventory','application','assets','helper','browser','cli','cliVersion','driver','reader','runtimeInput'])
 if(!/^[a-f0-9]{40}$/.test(c.source.commit)||!/^[a-f0-9]{40}$/.test(c.source.tree)||Object.entries(c.source).some(([k,v])=>!['commit','tree','cliVersion'].includes(k)&&!sha(v))||typeof c.source.cliVersion!=='string'||!c.source.cliVersion||c.source.cliVersion.length>128)refuse('control')
 if(c.role==='baseline'&&(c.source.commit!=='ef378c406f8b96e2c0c8854bc5e4dff40ac841e2'||c.source.tree!=='0034c3623b5b6e9bb8741da900392fd1c29e1508'))refuse('control')
 same(c.inputs,fixedInputs.map(input=>({...input,sliceSha256:SLICE})))
 if(c.role==='baseline'){keys(c.baselineReader,['path','sha256']);if(!isAbsolute(c.baselineReader.path)||!sha(c.baselineReader.sha256)||c.baselineReader.sha256!==c.source.reader)refuse('control')}
 else if(c.baselineReader!==null)refuse('control')
 return c
}

export function validateDescriptor(v,c) {
 keys(v,['baseUrl','setupToken','configurationSha256','setupTimeoutMs','caseTimeoutMs','aggregateInferenceTimeoutMs'])
 const u=new URL(v.baseUrl)
 if(u.protocol!=='http:'||u.hostname!=='127.0.0.1'||!u.port||u.origin!==v.baseUrl||u.username||u.password||u.search||u.hash||v.configurationSha256!==c.ownerConfigurationSha256||v.caseTimeoutMs!==120000||v.aggregateInferenceTimeoutMs!==480000||!Number.isSafeInteger(v.setupTimeoutMs)||v.setupTimeoutMs<=0||v.setupTimeoutMs>3600000||typeof v.setupToken!=='string'||!v.setupToken)refuse('control')
 // setupToken is deliberately never returned to the capture or its records.
 return {origin:v.baseUrl,caseMs:v.caseTimeoutMs,aggregateMs:v.aggregateInferenceTimeoutMs}
}

// Atomic publication: the Go owner must never see an empty/partial command.
// Only ENOENT means "not yet published"; permissions and malformed files fail.
export function privateOwnerIO(dir) {
 return {
  read(name) {try{return readPrivate(join(dir,name))}catch(e){if(e.code==='ENOENT')return null;throw e}},
  publish(name,value) {
   if(!/^(?:00[1-4]|abort)\.json$/.test(name))refuse('control')
   const text=JSON.stringify(value)+'\n';if(Buffer.byteLength(text)>4096)refuse('control')
   // O_EXCL precreation and a separate pending name avoid writePrivate's missing
   // O_CREAT and prevent overwriting any prior session command.
   const target=join(dir,name),pending=target+'.pending'
   const reservation=openSync(target+'.reservation',constants.O_CREAT|constants.O_EXCL|constants.O_WRONLY|constants.O_NOFOLLOW,0o600)
   closeSync(reservation)
   let existing
   try{existing=openSync(target,constants.O_RDONLY|constants.O_NOFOLLOW)}catch(e){if(e.code!=='ENOENT')throw e}
   if(existing!==undefined){closeSync(existing);refuse('control')}
   const fd=openSync(pending,constants.O_CREAT|constants.O_EXCL|constants.O_WRONLY|constants.O_NOFOLLOW,0o600)
   try{writeFileSync(fd,text)}finally{closeSync(fd)}
   renameSync(pending,target)
  }
 }
}

export function privateRetainer(dir) {
 return value=>{
  const name=value.state==='case-captured'?'case-'+value.cases.length+'.json':value.state==='complete'?'session.json':'incomplete.json'
  const path=join(dir,name)
  // Fresh private evidence only. A prior file refuses instead of truncating it.
  const fd=openSync(path,constants.O_CREAT|constants.O_EXCL|constants.O_WRONLY|constants.O_NOFOLLOW,0o600)
  closeSync(fd)
  writePrivate(path,value)
 }
}

// Canonical completion and its durable acknowledgment are checked separately.
// The candidate projects canonical complete to saved ready; the baseline retains
// its earlier saved complete contract. Neither saved state proves run completion.
export function savedChatComplete(chat,role) {
 if(!['candidate','baseline'].includes(role))refuse('control')
 const state=chat?.checkpoint?.state
 return state?.status===(role==='candidate'?'ready':'complete')&&Array.isArray(state.turns)&&state.turns.some(t=>t.role==='assistant'&&t.kind==='message')
}

export function settingsOf(chat,c) {
 if(!chat||chat.aiConnection!==c.connectionId||chat.aiConnectionName!==c.connectionName||chat.model!==c.model)refuse('settings')
 return effectiveSettingsOf(chat,c)
}

function effectiveSettingsOf(chat,c) {
 if(!chat||chat.reasoning?.model!==c.model||chat.reasoning?.effort!==c.effort||chat.mode!=='draft'||chat.researchMode!=='provided'||Object.hasOwn(chat,'interaction')&&chat.interaction!=='work')refuse('settings')
 const critic=c.role==='baseline'?(chat.adversarialReview===true?'on':'off'):(chat.critic??(chat.adversarialReview===false?'off':null))
 if(critic!=='off')refuse('settings')
 return {model:c.model,effort:c.effort,connectionId:c.connectionId,connectionName:c.connectionName,mode:chat.mode,research:chat.researchMode,critic,phase:'work',phaseInput:Object.hasOwn(chat,'interaction')?{state:'value',value:chat.interaction}:{state:'absent',value:null},criticInput:c.role==='baseline'?(Object.hasOwn(chat,'adversarialReview')?{state:'value',value:chat.adversarialReview}:{state:'absent',value:null}):null}
}

// An unsubmitted home draft can inherit the selected connection/model. Normal
// send pins them in the saved chat; keep that raw draft state separate from the
// effective configuration, and require the actual UI selection before admission.
export function draftSettingsOf(chat,c,selection) {
 if(selection?.checked!==true||selection.value!==JSON.stringify([c.connectionId,c.model]))refuse('settings')
 const raw={}
 for(const [key,expected] of [['aiConnection',c.connectionId],['aiConnectionName',c.connectionName],['model',c.model]]){
  const present=Object.hasOwn(chat,key),value=chat[key]
  if(present&&(typeof value!=='string'||value!==''&&value!==expected))refuse('settings')
  raw[key]=present?{state:'value',value}:{state:'absent',value:null}
 }
 return {settings:effectiveSettingsOf(chat,c),raw}
}

const settingSteps = ['composer','task-menu','task-chat','model-trigger','model-open','model-choices','model-select','model-checked','reasoning-home','reasoning-select','model-close','settings-open','research-menu','research-select','critic-select','tools-check','settings-close','draft-read','draft-settings']
const reasoningCategories = ['None','Minimal','Low','Medium','High','Extra high','Maximum','absent','other']
export function validateSettingsObservation(v) {
 keys(v,['kind','case','step','state'],['count','equal','reasoning','draft'])
 if(v.kind!=='capture-settings-observation'||![1,2].includes(v.case)||!settingSteps.includes(v.step)||!['before','after','failed'].includes(v.state))refuse('settings')
 if(Object.hasOwn(v,'count')&&(!Number.isSafeInteger(v.count)||v.count<0||v.count>128))refuse('settings')
 if(Object.hasOwn(v,'equal')&&typeof v.equal!=='boolean')refuse('settings')
 if(Object.hasOwn(v,'reasoning')&&!reasoningCategories.includes(v.reasoning))refuse('settings')
 if(Object.hasOwn(v,'draft')){
  keys(v.draft,['connectionEqual','connectionNameEqual','modelEqual','reasoningModelEqual','effortEqual','mode','research','critic','interaction'])
  for(const k of ['connectionEqual','connectionNameEqual','modelEqual','reasoningModelEqual','effortEqual'])if(typeof v.draft[k]!=='boolean')refuse('settings')
  for(const [k,allowed] of Object.entries({mode:['draft','other','absent'],research:['provided','other','absent'],critic:['off','on','other','absent'],interaction:['work','plan','other','absent']}))if(!allowed.includes(v.draft[k]))refuse('settings')
 }
 if(Buffer.byteLength(JSON.stringify(v))>1024)refuse('settings')
 return v
}
export function settingsObserver(retain,index) {
 let count=0
 return async(step,run,details=()=>({}))=>{
  const emit=(state,extra={})=>{if(++count>128)refuse('settings');const v=validateSettingsObservation({kind:'capture-settings-observation',case:index,step,state,...extra});retain?.(v)}
  emit('before')
  try{const value=await run();emit('after',details(value));return value}catch(error){emit('failed');throw error}
 }
}
export function draftSettingsObservation(chat,c) {
 const category=(value,allowed)=>value===undefined?'absent':allowed.includes(value)?value:'other'
 const critic=c.role==='baseline'?(chat?.adversarialReview===undefined?undefined:chat.adversarialReview===false?'off':chat.adversarialReview===true?'on':'other'):chat?.critic??(chat?.adversarialReview===false?'off':undefined)
 return {connectionEqual:chat?.aiConnection===c.connectionId,connectionNameEqual:chat?.aiConnectionName===c.connectionName,modelEqual:chat?.model===c.model,reasoningModelEqual:chat?.reasoning?.model===c.model,effortEqual:chat?.reasoning?.effort===c.effort,mode:category(chat?.mode,['draft']),research:category(chat?.researchMode,['provided']),critic:category(critic,['off','on']),interaction:category(chat?.interaction,['work','plan'])}
}
// Private diagnostics only: closed categories and equality facts, no run payload.
const terminalStates=['running','complete','failed','stopped','interrupted','awaiting-input','awaiting-client','paused-budget']
const terminalBranches=['read','terminal-state','terminal-checkpoint','accepted']
const category=(value,allowed)=>value===undefined?'absent':allowed.includes(value)?value:'other'
export function validateTerminalObservation(v) {
 keys(v,['kind','case','branch','caseElapsedMs','terminalElapsedMs','runState','checkpointStatus','phase','runIdEqual','ownerIdEqual','projectionAck'])
 if(v.kind!=='capture-terminal-observation'||![1,2].includes(v.case)||!terminalBranches.includes(v.branch))refuse('terminal')
 for(const k of ['caseElapsedMs','terminalElapsedMs'])if(!Number.isSafeInteger(v[k])||v[k]<0||v[k]>480000)refuse('terminal')
 for(const k of ['runState','checkpointStatus'])if(![...terminalStates,'absent','other'].includes(v[k]))refuse('terminal')
 if(!['work','plan','absent','other'].includes(v.phase)||!['absent','false','true','other'].includes(v.projectionAck)||typeof v.runIdEqual!=='boolean'||typeof v.ownerIdEqual!=='boolean')refuse('terminal')
 if(Buffer.byteLength(JSON.stringify(v))>1024)refuse('terminal')
 return v
}
export function terminalObservation(run,runId,chatId) {
 const ack=run?.chatProjection?.acknowledged
 return {runState:category(run?.state,terminalStates),checkpointStatus:category(run?.checkpoint?.status,terminalStates),phase:category(run?.checkpoint?.phase,['work','plan']),runIdEqual:run?.id===runId,ownerIdEqual:run?.owner?.id===chatId,projectionAck:ack===undefined?'absent':ack===true?'true':ack===false?'false':'other'}
}
export function terminalObserver(retain,index,caseStart,terminalStart) {
 let count=0,last=null,closed=false
 return (branch,run,runId,chatId)=>{
  if(!retain||closed)return
  const facts=terminalObservation(run,runId,chatId),signature=JSON.stringify({branch,...facts})
  if(signature===last)return
  // Keep at most127 transitions plus the actual refusal/acceptance, even for
  // an oscillating peer. Suppression never changes execution or deadlines.
  const final=branch!=='read'
  if(!final&&count>=127)return
  const elapsed=start=>Math.max(0,Math.min(480000,Math.trunc(Date.now()-start)))
  retain(validateTerminalObservation({kind:'capture-terminal-observation',case:index,branch,caseElapsedMs:elapsed(caseStart),terminalElapsedMs:elapsed(terminalStart),...facts}))
  count++;last=signature;if(final)closed=true
 }
}
// Server acknowledgment and the browser's asynchronous reconciliation are separate.
// Wait for the entire saved ordered message set on this exact chat, never a subset.
export function validateRenderedObservation(v) {
 const uint=value=>Number.isSafeInteger(value)&&value>=0
 keys(v,['kind','case','step','caseElapsedMs','expectedCount','actualCount','sameChat','sameIds','sameOrder','accepted'])
 if(v.kind!=='capture-rendered-observation'||![1,2].includes(v.case)||!['answers','reload'].includes(v.step))refuse('answers')
 if(!uint(v.caseElapsedMs)||v.caseElapsedMs>480000||!uint(v.expectedCount)||v.expectedCount<1||v.expectedCount>8||!uint(v.actualCount)||v.actualCount>8193)refuse('answers')
 for(const key of ['sameChat','sameIds','sameOrder','accepted'])if(typeof v[key]!=='boolean')refuse('answers')
 if(v.accepted!==(v.sameChat&&v.sameOrder&&v.actualCount===v.expectedCount)||v.sameOrder&&!v.sameIds)refuse('answers')
 return v
}
export async function waitRenderedAnswers({page,chatId,messages,wait,observe,index,step,caseStart}) {
 const expected=messages.map(m=>m.id)
 assert(expected.length>0&&expected.length<=8&&expected.every(id)&&new Set(expected).size===expected.length)
 let count=0,last=null
 await wait(async()=>{
  // One DOM snapshot keeps ownership and message identity facts together. Text is
  // still captured in full below, with exact IDs rechecked before each read.
  const actual=await page.locator('section[data-chat-id]').evaluateAll(sections=>{
   const section=sections[0]
   return {chatId:section?.getAttribute('data-chat-id')??null,ids:section?[...section.querySelectorAll('article[data-role="assistant"][data-kind="message"]')].map(node=>node.getAttribute('data-message-id')):[]}
  })
  const sameChat=actual.chatId===chatId,sameIds=actual.ids.length===expected.length&&new Set(actual.ids).size===expected.length&&expected.every(id=>actual.ids.includes(id)),sameOrder=sameIds&&expected.every((id,i)=>actual.ids[i]===id)
  const accepted=sameChat&&sameOrder
  const facts={kind:'capture-rendered-observation',case:index,step,expectedCount:expected.length,actualCount:Math.min(actual.ids.length,8193),sameChat,sameIds,sameOrder,accepted}
  const signature=JSON.stringify(facts)
  if(observe&&signature!==last&&(count<127||accepted)){
   observe(validateRenderedObservation({...facts,caseElapsedMs:Math.max(0,Math.min(480000,Math.trunc(Date.now()-caseStart)))}));count++;last=signature
  }
  return accepted
 })
 return page.locator('section[data-chat-id]').first().locator('article[data-role="assistant"][data-kind="message"]')
}
async function configure(page,c,observe) {
 const composer=page.getByPlaceholder('Ask a question or describe a task…');await observe('composer',()=>composer.waitFor())
 await observe('task-menu',()=>page.getByRole('combobox',{name:'Task tools',exact:true}).click())
 await observe('task-chat',()=>page.getByRole('option',{name:'Chat',exact:true}).click())
 const trigger=page.getByRole('button',{name:/^(?:Model: |Choose AI connection$)/})
 const triggers=await observe('model-trigger',()=>trigger.count(),count=>({count}));assert.equal(triggers,1)
 await observe('model-open',()=>trigger.click())
 // Exact saved connection/model binding remains mandatory.
 const choices=page.getByRole('radio',{name:c.model,exact:true})
 let chosen=null
 await observe('model-choices',async()=>{
  const count=await choices.count();if(count>128)refuse('settings')
  for(let i=0;i<count;i++)if(await choices.nth(i).getAttribute('value')===JSON.stringify([c.connectionId,c.model]))chosen=choices.nth(i)
  return count
 },count=>({count,equal:chosen!==null}))
 if(!chosen)refuse('settings');await observe('model-select',()=>chosen.click())
 const selection=await observe('model-checked',async()=>{
  const value=await chosen.getAttribute('value'),checked=await chosen.isChecked()
  if(value!==JSON.stringify([c.connectionId,c.model])||checked!==true)refuse('settings')
  return {value,checked}
 },value=>({equal:value.checked}))
 const slider=page.getByRole('slider',{name:'Reasoning',exact:true})
 const label={none:'None',minimal:'Minimal',low:'Low',medium:'Medium',high:'High',xhigh:'Extra high',max:'Maximum'}[c.effort]
 await observe('reasoning-home',()=>slider.press('Home'))
 let matched=false
 await observe('reasoning-select',async()=>{
  let actual=null
  for(let n=0;n<7;n++){actual=await slider.getAttribute('aria-valuetext');if(actual===label){matched=true;break}await slider.press('ArrowRight')}
  return actual===null?'absent':reasoningCategories.slice(0,7).includes(actual)?actual:'other'
 },reasoning=>({reasoning,equal:matched}))
 if(!matched)refuse('settings')
 await observe('model-close',()=>page.keyboard.press('Escape'))
 await observe('settings-open',()=>page.getByRole('button',{name:'Assistant settings',exact:true}).click())
 await observe('research-menu',()=>page.getByRole('combobox',{name:'Research',exact:true}).click())
 await observe('research-select',()=>page.getByRole('option',{name:'Provided sources only',exact:true}).click())
 await observe('critic-select',async()=>{
  if(c.role==='baseline'){const box=page.getByRole('checkbox',{name:'Adversarial review',exact:true});if(await box.isChecked())await box.uncheck();assert.equal(await box.isChecked(),false)}
  else await page.getByRole('combobox',{name:'Adversarial review',exact:true}).selectOption('off')
 })
 const tools=await observe('tools-check',()=>page.getByRole('region',{name:'Allowed tools',exact:true}).locator('li').allTextContents(),value=>({count:value.length,equal:JSON.stringify(value)===JSON.stringify(['Read pack schema'])}))
 same(tools,['Read pack schema'])
 await observe('settings-close',()=>page.keyboard.press('Escape'))
 return {composer,selection}
}

export function checkAccounting(a,index,c) {
 keys(a,['schema_version','invocation','sequence_exhausted','requested_model','requested_effort','selected_effort','acknowledged_model','acknowledged_provider','acknowledged_effort','thread_id','turn_id','admitted','owned_client_reaped','application_success','exit_reason','notifications','usage_updates','usage_present','usage_schema_valid','last_observed','terminal_observed','terminal_status','terminal_usage_finality','malformed','scope_mismatch','overflow','routing_disagreement','reroute_observed','reroute_count','reroutes','incomplete','fixed_model_ineligible'])
 if(!a||a.schema_version!==1||a.invocation!==index||a.requested_model!==c.model||a.acknowledged_model!==c.model||a.acknowledged_provider!=='openai'||a.selected_effort!==c.effort||a.requested_effort?.state!=='value'||a.requested_effort.value!==c.effort||!['absent','null','value'].includes(a.acknowledged_effort?.state)||a.acknowledged_effort.state==='value'&&a.acknowledged_effort.value!==c.effort||!id(a.thread_id)||!id(a.turn_id))refuse('accounting')
 for(const k of ['admitted','owned_client_reaped','application_success','terminal_observed','usage_present','usage_schema_valid'])if(a[k]!==true)refuse('accounting')
 for(const k of ['sequence_exhausted','malformed','scope_mismatch','overflow','routing_disagreement','reroute_observed','incomplete','fixed_model_ineligible'])if(a[k]!==false)refuse('accounting')
 if(a.terminal_status!=='completed'||a.exit_reason!=='success'||a.terminal_usage_finality!=='unproven'||a.reroute_count!==0)refuse('accounting')
 const uint=v=>Number.isSafeInteger(v)&&v>=0
 const value=v=>{keys(v,['state','value']);if(!['absent','null','invalid','value'].includes(v.state)||!uint(v.value))refuse('accounting')}
 keys(a.last_observed,['observation','last_scope','total_scope','last','total','model_context_window'])
 if(!uint(a.last_observed.observation)||a.last_observed.observation<1||a.last_observed.last_scope!=='provider_last_breakdown'||a.last_observed.total_scope!=='provider_thread_total')refuse('accounting')
 for(const name of ['last','total']){keys(a.last_observed[name],['input_tokens','cached_input_tokens','output_tokens','reasoning_output_tokens','total_tokens','cache_write_input_tokens']);Object.values(a.last_observed[name]).forEach(value)}
 value(a.last_observed.model_context_window)
 if(!Array.isArray(a.reroutes)||a.reroutes.length!==8)refuse('accounting')
 for(const r of a.reroutes){keys(r,['thread_id','turn_id','from_model','to_model','reason']);if(Object.values(r).some(v=>v!==''))refuse('accounting')}
 // Retain original counters and all absence/null states, never add or price them.
 return structuredClone(a)
}

// page is the same page/context retained from normal interactive setup. ownerIO
// is injectable only for handwritten control tests; production uses private files.
// This entrypoint is deliberately not a CLI or an authentication launcher.
export async function captureLiveSession({page,control,ownerIO=privateOwnerIO(control.controlDir),baselineModule=null,retain,retainCanonical,observeSettings,observeTerminal,observeRendered}) {
 let c,descriptor,phase='control',active=null,bad=null,bearer='',serial=0,runId=null,capture=null,starts=0,browserErrors=0,unexpected=0,deadline=Infinity,aggregate=Infinity
 const records=[];let lifecycle=null,contextCloseStarted=false,dispatchDiagnostic=null,responseCount=0,observeResponses=true
 const pendingResponses=new Map()
 const rejectDispatch=(category,status)=>{if(observeResponses){dispatchDiagnostic??={category,status};bad='dispatch'}}
 const finishDispatchObservation=async()=>{
  // Do not let an unavailable response body delay abort indefinitely. A still
  // pending read is distinct from a rejected read; neither supplies response bytes.
  if(pendingResponses.size){let timer;try{await Promise.race([Promise.all([...pendingResponses.keys()]),new Promise(resolve=>{timer=setTimeout(resolve,1500)})])}finally{clearTimeout(timer)}}
  if(!dispatchDiagnostic&&pendingResponses.size)dispatchDiagnostic={category:'body-unavailable',status:pendingResponses.values().next().value,bodyPending:true}
  observeResponses=false
  return dispatchDiagnostic?{...dispatchDiagnostic,requestCount:Math.min(starts,3),responseCount:Math.min(responseCount,3),countsCapped:starts>3||responseCount>3}:null
 }
 const save=async value=>{parseClosed(JSON.stringify(value));await retain(structuredClone(value))}
 const left=()=>{const ms=Math.min(deadline,aggregate)-Date.now();if(ms<=0)refuse('deadline');return Math.min(ms,15000)}
 const healthy=()=>{left();if(bad)refuse(bad);if(unexpected)refuse('unexpected-request');if(browserErrors)refuse('browser-error')}
 const wait=async fn=>{for(;;){healthy();const value=await fn();if(value)return value;const ended=ownerIO.read('lifecycle.json');if(ended)refuse('owner');await sleep()}}
 const command=async(n,v)=>{ownerIO.publish(String(n).padStart(3,'0')+'.json',v);same(await wait(()=>ownerIO.read(String(n).padStart(3,'0')+'-ack.json')),v)}
 const api=async path=>{
  healthy();if(!bearer||!/^\/api\/(?:conversations|assistant-runs\/arun_[a-f0-9]+)$/.test(path))refuse('readiness')
  const response=await page.request.get(descriptor.origin+path,{headers:{Authorization:bearer},timeout:left()})
  assert(response.ok());return parseApplicationSnapshot(await response.text())
 }
 const inspectRun=(run,want,chatId)=>{
  if(run.id!==runId||run.owner?.id!==chatId||!Array.isArray(run.operations)||!Array.isArray(run.events))refuse('evidence')
  const calls=run.events.filter(e=>e.event?.type==='tool_call')
  if(calls.length>want||calls.some(e=>e.event.name!=='get_schema'||JSON.stringify(e.event.args)!=='{}')||run.operations.filter(o=>o.kind==='model').length>1||run.events.some(e=>['error','guardrail','budget_paused'].includes(e.event?.type)))refuse('evidence')
 }
 const onError=()=>{browserErrors++}
 const onRequest=request=>{try{
  const u=new URL(request.url());if(u.origin!==descriptor.origin){unexpected++;return}
  if(/\/api\/model-providers\/.*\/(?:login|install|logout)/.test(u.pathname)){unexpected++;return}
  if(u.pathname==='/api/conversations')void request.headerValue('authorization').then(v=>{bearer=v??bearer}).catch(()=>{bad='readiness'})
  if(request.method()==='POST'&&u.pathname==='/api/assistant-runs'){
   if(c.role!=='candidate'||!active||++starts!==active)refuse('unexpected-request')
   const body=parseClosed(request.postData()??'null');if(body.purpose!=='chat'||body.ownerId!==records[active-1].chatId)refuse('unexpected-request')
   void request.headerValue('authorization').then(v=>{bearer=v??bearer}).catch(()=>{bad='readiness'})
  }
  if(/^\/api\/(?:codex\/run|assistant\/relay\/)/.test(u.pathname)||c.role==='candidate'&&u.pathname==='/api/agent/run')unexpected++
 }catch{bad='unexpected-request'}}
 const onResponse=response=>{if(response.request().method()==='POST'&&new URL(response.url()).pathname==='/api/assistant-runs'){
  responseCount++
  let value;try{value=response.status()}catch{rejectDispatch('status',null);return}
  const status=Number.isInteger(value)&&value>=100&&value<=599?value:null
  const task=(async()=>{
   let raw;try{raw=await response.text()}catch{rejectDispatch('body-unavailable',status);return}
   if(!observeResponses)return
   let v;try{v=parseClosed(raw)}catch{rejectDispatch('json-invalid',status);return}
   if(value!==201){rejectDispatch('status',status);return}
   if(typeof v?.id!=='string'||!/^arun_[a-f0-9]+$/.test(v.id)){rejectDispatch('invalid-run-id',status);return}
   if(runId){rejectDispatch('duplicate-response',status);return}
   runId=v.id
  })()
  pendingResponses.set(task,status);void task.finally(()=>pendingResponses.delete(task))
 }}
 const onSocket=ws=>{
  const u=new URL(ws.url()),socket=++serial
  if(u.origin.replace('ws:','http:')!==descriptor.origin||!['/api/agent/run','/ws'].includes(u.pathname)){bad='unexpected-request';return}
  if(c.role==='candidate'){if(u.pathname==='/api/agent/run')bad='unexpected-request';return}
  for(const [event,direction] of [['framesent','sent'],['framereceived','received']])ws.on(event,frame=>{try{
   const raw=frame.payload;if(typeof raw!=='string')refuse('evidence')
   if(!capture){
    if(u.pathname==='/api/agent/run')refuse('unexpected-request')
    const v=parseClosed(raw)
    if(direction==='sent'&&v.method==='tools/call'&&!['list_packs','describe_runtime'].includes(v.params?.name))refuse('unexpected-request')
    return
   }
   if(u.pathname==='/api/agent/run'&&direction==='sent'&&raw.startsWith('{"type":"start",')){if(!active)refuse('unexpected-request');starts++;capture.native(direction,{type:'start'});return}
   const v=parseClosed(raw)
   if(u.pathname==='/api/agent/run')capture.native(direction,v);else capture.runtime(direction,socket,v)
  }catch{bad='evidence'}})
 }
 try {
  c=validateLiveControl(control);descriptor=validateDescriptor(ownerIO.read('owner.json'),c)
  if(retain===undefined)retain=privateRetainer(c.evidenceDir)
  if(typeof retain!=='function')refuse('control')
  if(retainCanonical===undefined)retainCanonical=privateCanonicalRetainer(c.evidenceDir)
  if(typeof retainCanonical!=='function')refuse('control')
  if(c.role==='baseline'){
   if(!baselineModule){const {readFileSync}=await import('node:fs');if(digest(readFileSync(c.baselineReader.path,'utf8'))!==c.baselineReader.sha256)refuse('control');baselineModule=await import(pathToFileURL(c.baselineReader.path).href)}
   if(typeof baselineModule.Capture!=='function')refuse('control')
  }
  phase='readiness';deadline=Date.now()+descriptor.caseMs
  if(new URL(page.url()).origin!==descriptor.origin)refuse('readiness')
  page.setDefaultTimeout(15000)
  // The host must finish normal owner/password and named subscription connection
  // sign-in in this context first. This function does not use owner.setupToken.
  page.on('pageerror',onError);page.on('request',onRequest);page.on('response',onResponse);page.on('websocket',onSocket)
  // Reopen home in the same authenticated context after attaching observers:
  // Playwright cannot retroactively observe the existing Runtime websocket.
  await page.goto(descriptor.origin,{timeout:left()})
  for(let index=1;index<=2;index++){
   const caseStart=Date.now();active=null;capture=null;runId=null;deadline=Math.min(Date.now()+descriptor.caseMs,aggregate)
   if(index===2){phase='fresh-chat';await page.getByRole('button',{name:'Chat history',exact:true}).click();await page.getByRole('button',{name:'New chat',exact:true}).click()}
   phase='settings';const observe=settingsObserver(observeSettings,index);const {composer,selection}=await configure(page,c,observe)
   const pane=page.locator('section[data-chat-id]').first(),chatId=await pane.getAttribute('data-chat-id')
   if(!id(chatId)||records.some(r=>r.chatId===chatId)||await pane.locator('article[data-role]').count())refuse('fresh-chat')
   // Read only this explicit project's ordinary draft, never enumerate storage.
   const draft=await observe('draft-read',()=>page.evaluate(key=>sessionStorage.getItem(key),'jpack.home-draft:'+c.projectRoot))
   const fresh=parseClosed(draft);if(fresh.id!==chatId||fresh.checkpoint||fresh.composer||fresh.attachments?.length)refuse('fresh-chat')
   await observe('draft-settings',async()=>draftSettingsObservation(fresh,c),draft=>({draft}))
   const {settings,raw:draftSelection}=draftSettingsOf(fresh,c,selection)
   const record={input:c.inputs[index-1],chatId,settings,draftSelection,runId:null,answers:null,evidence:null,accounting:null,counts:null,normalOwnerShutdown:null}
   records.push(record);capture=c.role==='baseline'?new baselineModule.Capture():null
   phase='owner';await command(index*2-1,{action:'begin',case:index,chatId})
   if(index===1)aggregate=Date.now()+descriptor.aggregateMs
   deadline=Math.min(Date.now()+descriptor.caseMs,aggregate);active=index
   phase='dispatch';const before=starts;await composer.fill(record.input.prompt);await composer.press('Enter')
   phase='terminal';let run=null,chat=null
   const terminal=terminalObserver(observeTerminal,index,caseStart,Date.now())
   await wait(async()=>{
    if(c.role==='candidate'){
     if(!runId)return false;run=await api('/api/assistant-runs/'+runId)
     terminal('read',run,runId,chatId)
     inspectRun(run,index-1,chatId)
     if(['failed','stopped','interrupted','awaiting-input','awaiting-client','paused-budget'].includes(run.state)){terminal('terminal-state',run,runId,chatId);refuse('terminal')}
     if(run.state!=='complete')return false
     if(run.checkpoint?.status!=='complete'||run.checkpoint?.phase!=='work'||run.owner?.id!==chatId){terminal('terminal-checkpoint',run,runId,chatId);refuse('terminal')}
     // State complete precedes ProjectChat's separate durable acknowledgment.
     // Keep observing this same run within its existing owner/case deadline.
     if(run.chatProjection?.acknowledged!==true)return false
     terminal('accepted',run,runId,chatId)
    }else if(!capture.terminal)return false
    const saved=await api('/api/conversations');chat=saved.content.chats.find(v=>v.id===chatId)
    return savedChatComplete(chat,c.role)
   })
   record.runId=c.role==='candidate'?runId:capture.runId
   same(settingsOf(chat,c),settings)
   record.savedChat={status:chat.checkpoint.state.status,phase:Object.hasOwn(chat.checkpoint.state,'phase')?{state:'value',value:chat.checkpoint.state.phase}:{state:'absent',value:null}}
   const turns=chat.checkpoint.state.turns;const user=turns.filter(t=>t.role==='user');assert.equal(user.length,1);assert.equal(user[0].text,record.input.prompt)
   const messages=turns.filter(t=>t.role==='assistant'&&t.kind==='message');assert(messages.length>0&&messages.length<=8);assert.equal(new Set(messages.map(m=>m.id)).size,messages.length)
   phase='answers';const articles=await waitRenderedAnswers({page,chatId,messages,wait,observe:observeRendered,index,step:phase,caseStart})
   record.answers=[]
   for(let i=0;i<messages.length;i++){assert.equal(await articles.nth(i).getAttribute('data-message-id'),messages[i].id);assert(id(messages[i].id)&&typeof messages[i].text==='string'&&messages[i].text);record.answers.push({id:messages[i].id,text:messages[i].text,renderedText:await articles.nth(i).locator('[class*="markdown"]').first().innerText(),reloadedText:null})}
   phase='reload';await page.goto(descriptor.origin+'/chats/'+chatId,{timeout:left()})
   const reloaded=await waitRenderedAnswers({page,chatId,messages,wait,observe:observeRendered,index,step:phase,caseStart})
   const restored=(await api('/api/conversations')).content.chats.find(v=>v.id===chatId);same(restored.checkpoint.state.turns,turns);same(settingsOf(restored,c),settings)
   if(!savedChatComplete(restored,c.role))refuse('reload')
   same(Object.hasOwn(restored.checkpoint.state,'phase')?{state:'value',value:restored.checkpoint.state.phase}:{state:'absent',value:null},record.savedChat.phase)
   for(let i=0;i<messages.length;i++){assert.equal(await reloaded.nth(i).getAttribute('data-message-id'),messages[i].id);record.answers[i].reloadedText=await reloaded.nth(i).locator('[class*="markdown"]').first().innerText();assert(record.answers[i].renderedText);assert.equal(record.answers[i].reloadedText,record.answers[i].renderedText)}
   healthy();assert.equal(starts-before,1)
   phase='evidence'
   if(c.role==='candidate'){
    run=await api('/api/assistant-runs/'+runId)
    inspectRun(run,index-1,chatId)
    const tools=captureTools(run.events??[]);assert.equal(tools.length,index-1)
    assert(!(run.events??[]).some(e=>['error','guardrail','budget_paused'].includes(e.event.type)))
    assert.equal(run.operations.filter(o=>o.kind==='model').length,1)
    for(const tool of tools){same(tool.args,{});assert.equal(tool.name,'get_schema');assert.equal(tool.text,'{"schema":"recorded"}');assert.equal(tool.isError,false);if(tool.structuredState==='value')same(tool.structured,{schema:'recorded'})}
    const canonical=validateCanonicalEvidence({kind:'capture-canonical-evidence',binding:{role:c.role,case:index,caseId:record.input.id,chatId,runId},events:run.events,operations:run.operations},{role:c.role,case:index,caseId:record.input.id,chatId,runId})
    const reference=await retainCanonical(structuredClone(canonical));same(reference,canonicalReference(canonical))
    record.evidence={canonical:reference,tools}
   }else record.evidence={canonical:'unavailable-baseline-contract',...capture.finish(index-1)}
   phase='accounting';record.accounting=await wait(()=>ownerIO.read('case-'+index+'-accounting.json'));checkAccounting(record.accounting,index,c)
   record.counts={starts:starts-before,callbacks:index-1,browserErrors,unexpectedRequests:unexpected,reloadInvocations:0}
   phase='evidence';await save({state:'case-captured',role:c.role,source:c.source,cases:records,lifecycle:null,answerChangeDecision:'pending',usageFinality:'unproven',monetaryCost:null})
   phase='owner';await command(index*2,{action:'complete',case:index,chatId,threadId:record.accounting.thread_id,turnId:record.accounting.turn_id})
   active=null
  }
  lifecycle=await wait(()=>ownerIO.read('lifecycle.json'))
  keys(lifecycle,['schemaVersion','reason','normalOwnerShutdown','shutdownError','deadlineCallbacksJoined','accountingDropped','cases','invocationCriteria','usageFinality','monetaryCost'],['unexpectedAccounting'])
  if(lifecycle.reason!=='completed'||lifecycle.normalOwnerShutdown!==true||lifecycle.shutdownError!==false||lifecycle.deadlineCallbacksJoined!==true||lifecycle.accountingDropped!==0||lifecycle.unexpectedAccounting||lifecycle.cases?.length!==2)refuse('owner')
  for(let i=0;i<2;i++){const actual=lifecycle.cases[i];if(actual.caseId!==c.inputs[i].id||actual.chatId!==records[i].chatId||actual.completed!==true)refuse('owner');same(actual.accounting,records[i].accounting);records[i].normalOwnerShutdown=true}
  healthy();assert.equal(starts,2)
  const result={state:'complete',role:c.role,source:c.source,cases:records,lifecycle,answerChangeDecision:'pending',usageFinality:'unproven',monetaryCost:null}
  phase='cleanup';contextCloseStarted=true;await page.context().close()
  phase='evidence';await save(result);return result
 }catch(e){
  const reason=errors.has(e.code)?e.code:phase
  try{ownerIO.publish('abort.json',{action:'abort'})}catch{}
  try{const stop=page.getByRole('button',{name:'Stop',exact:true});if(await stop.count())await stop.first().click({timeout:1500})}catch{}
  // Partial validated observations remain private. Never persist caught prose,
  // start bodies, bearer, owner descriptor, URLs or authentication challenges.
  const dispatch=await finishDispatchObservation()
  if(c&&typeof retain==='function')try{await save({state:'incomplete',reason,...(dispatch?{dispatchDiagnostic:dispatch}:{}),role:c.role,source:c.source,cases:records,lifecycle:null,answerChangeDecision:'pending',usageFinality:'unproven',monetaryCost:null})}catch{}
  throw Object.assign(new Error('live-browser-incomplete:'+reason),{code:reason})
 }finally{
  page.off('pageerror',onError);page.off('request',onRequest);page.off('response',onResponse);page.off('websocket',onSocket)
  // The caller retains the browser process; this one setup/inference context is
  // closed only after the shared owner's final lifecycle or whole-session abort.
  if(!contextCloseStarted){contextCloseStarted=true;try{await page.context().close()}catch{refuse('cleanup')}}
 }
}
