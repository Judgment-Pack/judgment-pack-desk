// Closed offline-only readers. No live authorization or billing inference.
import {constants,openSync,closeSync,fstatSync,readFileSync,writeFileSync} from 'node:fs'
import {isAbsolute} from 'node:path'
import {createHash} from 'node:crypto'
export const cases=[
 {id:'model-policy-refusal',prompt:'Explain briefly why the host must keep the declared policy boundary in force. Answer in one sentence without calling tools.',corpusSha256:'24d8250a39d1cfd4181b090facfb62a5a37d9eec178634c0f781b6d8dddd6357'},
 {id:'model-recorded-tool-result',prompt:'Read the schema with get_schema, then state what the recorded schema says in one sentence. Do not call any other tool.',corpusSha256:'4493e788bcd0ae0f9a31215bd6d8e68111e7dc0bb1a60a67fd30612f42fae3a7'}
]
const fail=()=>{throw Error('invalid-offline-replay-record')}
const obj=v=>v!==null&&typeof v==='object'&&!Array.isArray(v)
export function keys(v,required,optional=[]){if(!obj(v)||required.some(k=>!Object.hasOwn(v,k))||Object.keys(v).some(k=>!required.includes(k)&&!optional.includes(k)))fail()}
const integer=(v,max=Number.MAX_SAFE_INTEGER)=>Number.isSafeInteger(v)&&v>=0&&v<=max
const identifier=v=>typeof v==='string'&&/^[A-Za-z0-9_-]{1,128}$/.test(v)
const sha=v=>typeof v==='string'&&/^[a-f0-9]{64}$/.test(v)
export const digest=v=>createHash('sha256').update(typeof v==='string'?v:JSON.stringify(v)).digest('hex')
// Application snapshots have a separate transport domain; report/control limits stay fixed.
export function parseApplicationSnapshot(text){return parseBounded(text,4*1024*1024)}
export function parseClosed(text){return parseBounded(text,65536)}
function parseBounded(text,maxBytes){
 if(typeof text!=='string'||Buffer.byteLength(text)>maxBytes)fail()
 let i=0,budget=8192
 const ws=()=>{while(/\s/.test(text[i]??'')&&i<text.length)i++}
 const string=()=>{const start=i++;while(i<text.length){const c=text[i++];if(c==='"'){try{return JSON.parse(text.slice(start,i))}catch{fail()}}if(c==='\\')i++}fail()}
 const value=depth=>{
  if(depth>16||--budget<0)fail();ws();const c=text[i]
  if(c==='"'){string();return}
  if(c==='{'||c==='['){i++;ws();const object=c==='{',end=object?'}':']',seen=new Set();if(text[i]===end){i++;return}
   while(true){if(object){if(text[i]!=='"')fail();const key=string();if(seen.has(key))fail();seen.add(key);ws();if(text[i++]!==':')fail()}value(depth+1);ws();if(text[i]===end){i++;return}if(text[i++]!==',')fail();ws()}
  }
  const token=/^(?:true|false|null|-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?)/.exec(text.slice(i));if(!token)fail();i+=token[0].length
 }
 value(0);ws();if(i!==text.length)fail();try{return JSON.parse(text)}catch{fail()}
}
export function readPrivate(path,max=65536){
 if(!isAbsolute(path))fail()
 const fd=openSync(path,constants.O_RDONLY|constants.O_NOFOLLOW)
 try{const s=fstatSync(fd);if(!s.isFile()||s.uid!==process.getuid()||(s.mode&0o077)!==0||s.size>max)fail();return parseClosed(readFileSync(fd,'utf8'))}finally{closeSync(fd)}
}
export function writePrivate(path,value){
 const fd=openSync(path,constants.O_WRONLY|constants.O_TRUNC|constants.O_NOFOLLOW)
 try{const s=fstatSync(fd);if(!s.isFile()||s.uid!==process.getuid()||(s.mode&0o077)!==0)fail();const text=JSON.stringify(value);if(Buffer.byteLength(text)>65536)fail();writeFileSync(fd,text+'\n')}finally{closeSync(fd)}
}
export function validateControl(c){
 keys(c,['kind','provenance','caseId','prompt','model','effort','caseDeadlineMs','aggregateDeadlineMs','origin','setupCode','browserBin','reportPath'])
 const fixed=cases.find(row=>row.id===c.caseId)
 if(c.kind!=='offline-mounted-replay-control'||c.provenance!=='offline-test'||!fixed||c.prompt!==fixed.prompt||c.model!=='gpt-6-sol'||c.effort!=='medium'||!integer(c.caseDeadlineMs,60000)||c.caseDeadlineMs<1000||!integer(c.aggregateDeadlineMs,180000)||c.aggregateDeadlineMs<c.caseDeadlineMs||!isAbsolute(c.browserBin)||!isAbsolute(c.reportPath)||typeof c.setupCode!=='string'||!/^[A-Za-z0-9_-]{20,128}$/.test(c.setupCode))fail()
 const u=new URL(c.origin);if(u.protocol!=='http:'||u.hostname!=='127.0.0.1'||!u.port||u.username||u.password||u.pathname!=='/'||u.search||u.hash||u.origin!==c.origin)fail()
 return c
}
export function validateBrowserReport(r){
 if(Buffer.byteLength(JSON.stringify(r))>65536)fail()
 keys(r,['kind','provenance','caseId','state','reason','runId','chatId','answers','tools','operations','counts','settings','elapsedMs'],['bootstrapFailure','captureStage'])
 if(r.kind!=='offline-mounted-browser-result'||r.provenance!=='offline-test'||!cases.some(c=>c.id===r.caseId)||!['incomplete','complete'].includes(r.state)||!['pending','complete','bootstrap','settings','dispatch','terminal','capture','deadline','unexpected-request','browser-error','cleanup'].includes(r.reason)||!integer(r.elapsedMs,180000))fail()
 if(Object.hasOwn(r,'captureStage')&&!['settings','answers','reload','events','operations','validation'].includes(r.captureStage))fail()
 if(Object.hasOwn(r,'bootstrapFailure')){keys(r.bootstrapFailure,['subphase','errorName']);if(r.state!=='incomplete'||!['launch','context','page','route','navigation','owner-setup','owner-navigation'].includes(r.bootstrapFailure.subphase)||!['Error','TimeoutError','TypeError','AssertionError','RangeError','Other'].includes(r.bootstrapFailure.errorName))fail()}
 if(r.runId!==null&&!identifier(r.runId)||r.chatId!==null&&!identifier(r.chatId))fail()
 for(const name of ['answers','tools','operations'])if(r[name]!==null&&(!Array.isArray(r[name])||r[name].length>64))fail()
 for(const field of ['answers','tools','operations'])if(r[field]!==null&&new Set(r[field].map(row=>row?.[field==='tools'?'callId':'id'])).size!==r[field].length)fail()
 for(const a of r.answers??[]){keys(a,['id','text','renderedText','reloadedText']);if(!identifier(a.id)||typeof a.text!=='string'||typeof a.renderedText!=='string'||a.reloadedText!==null&&typeof a.reloadedText!=='string'||Buffer.byteLength(a.text)>200000||Buffer.byteLength(a.renderedText)>200000||a.reloadedText!==null&&Buffer.byteLength(a.reloadedText)>200000)fail()}
 for(const t of r.tools??[]){keys(t,['callId','callEventId','resultEventId','name','args','text','structured','structuredState','isError']);if(!identifier(t.callId)||!identifier(t.callEventId)||!identifier(t.resultEventId)||t.name!=='get_schema'||JSON.stringify(t.args)!=='{}'||t.text!=='{"schema":"recorded"}'||!(['absent','value'].includes(t.structuredState)&&(t.structuredState==='absent'?t.structured===null:JSON.stringify(t.structured)==='{"schema":"recorded"}'))||t.isError!==false)fail()}
 for(const o of r.operations??[]){keys(o,['id','callId','kind','name','state','intentSha256','result']);if(!identifier(o.id)||!identifier(o.callId)||typeof o.name!=='string'||o.name.length>200||!sha(o.intentSha256)||typeof o.result!=='string'||o.result.length>16000||!/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(o.result)||!['runtime','model','callback'].includes(o.kind)||o.state!=='completed')fail()}
 if(r.counts!==null){keys(r.counts,['starts','models','callbacks','browserErrors','unexpectedRequests']);if(Object.values(r.counts).some(v=>!integer(v,64)))fail()}
 if(r.settings!==null){
  keys(r.settings,['model','effort','critic','research','mode','phase','phaseInput'])
  for(const [key,want] of Object.entries(fixedSettings))if(r.settings[key]!==null&&r.settings[key]!==want)fail()
  const input=r.settings.phaseInput
  if(input!==null){keys(input,['state','value']);if(!['absent','value'].includes(input.state)||input.value!==(input.state==='absent'?null:'work'))fail()}
 }
 if(r.state==='complete'){
  if(r.answers?.some(a=>!a.text||!a.renderedText||!a.reloadedText)||!r.settings?.phaseInput||Object.keys(fixedSettings).some(k=>r.settings[k]!==fixedSettings[k]))fail()
  const want=r.caseId===cases[0].id?0:1
  if(r.reason!=='complete'||!r.runId||!r.chatId||!r.answers?.length||r.answers.length>8||r.tools?.length!==want||!r.operations?.length||!r.settings||r.counts?.starts!==1||r.counts.models!==1||r.counts.callbacks!==want||r.counts.browserErrors!==0||r.counts.unexpectedRequests!==0)fail()
 }
 return r
}
export const fixedSettings={model:'gpt-6-sol',effort:'medium',critic:'off',research:'provided',mode:'draft',phase:'work'}
export function captureSettings(chat,checkpoint){
 if(checkpoint?.phase!=='work')fail()
 if(Object.hasOwn(chat,'interaction')&&chat.interaction!=='work')fail()
 return {model:chat.model??null,effort:chat.reasoning?.effort??null,critic:chat.critic??(chat.adversarialReview===false?'off':null),research:chat.researchMode??null,mode:chat.mode??null,phase:'work',phaseInput:Object.hasOwn(chat,'interaction')?{state:'value',value:chat.interaction}:{state:'absent',value:null}}
}
export function captureTools(events){
 const calls=events.filter(e=>e.event.type==='tool_call'),results=events.filter(e=>e.event.type==='tool_result')
 if(calls.length!==results.length||new Set(calls.map(e=>e.event.callId)).size!==calls.length||new Set(results.map(e=>e.event.callId)).size!==results.length)fail()
 return results.map(({id:resultEventId,event:t})=>{
  const matching=calls.filter(({event:c})=>c.callId===t.callId&&c.name===t.name)
  if(matching.length!==1||!identifier(t.callId))fail()
  return {callId:t.callId,callEventId:matching[0].id,resultEventId,name:t.name,args:matching[0].event.args,text:t.text,structured:t.structured??null,structuredState:Object.hasOwn(t,'structured')?'value':'absent',isError:t.isError}
 })
}
export function retainBrowserObservations(r){
 const safe={kind:'offline-mounted-browser-result',provenance:'offline-test',caseId:r.caseId,state:'incomplete',reason:r.reason,runId:null,chatId:null,answers:null,tools:null,operations:null,counts:null,settings:null,elapsedMs:r.elapsedMs}
 for(const key of ['runId','chatId','answers','tools','operations','counts','bootstrapFailure','captureStage']){
  if(!Object.hasOwn(r,key))continue
  try{validateBrowserReport({...safe,[key]:r[key]});safe[key]=r[key]}catch{}
 }
 if(r.settings!==null){
  safe.settings={...Object.fromEntries(Object.keys(fixedSettings).map(k=>[k,null])),phaseInput:null}
  for(const key of Object.keys(safe.settings))try{const settings={...safe.settings,[key]:r.settings[key]};validateBrowserReport({...safe,settings});safe.settings=settings}catch{}
 }
 validateBrowserReport(safe);return safe
}
export function validateMountedReport(r){
 keys(r,['kind','provenance','qualification','state','reason','input','bounds','identities','browser','accounting','dropped','trace'])
 if(r.kind!=='offline-mounted-case-report'||r.provenance!=='offline-test'||r.qualification!=='provisional'||!['incomplete','complete'].includes(r.state)||!['pending','complete','browser-incomplete','accounting-incomplete','trace-incomplete','shutdown-incomplete'].includes(r.reason))fail()
 keys(r.input,['caseId','prompt','corpusSha256','sliceSha256']);const c=cases.find(c=>c.id===r.input.caseId);if(!c||r.input.prompt!==c.prompt||r.input.corpusSha256!==c.corpusSha256||!sha(r.input.sliceSha256))fail()
 keys(r.bounds,['caseMs','aggregateMs','maxNativeTurns','maxCallbacks']);if(!integer(r.bounds.caseMs,60000)||r.bounds.caseMs<1000||!integer(r.bounds.aggregateMs,180000)||r.bounds.aggregateMs<r.bounds.caseMs||r.bounds.maxNativeTurns!==1||r.bounds.maxCallbacks!==(c===cases[0]?0:1))fail()
 keys(r.identities,['launcher','driver','reader','peer','applicationExecutable','browserExecutable','assets','sourceInventory']);if(Object.values(r.identities).some(v=>!sha(v)))fail()
 if(r.browser!==null)validateBrowserReport(r.browser)
 if(r.dropped!==null&&!integer(r.dropped))fail()
 if(r.trace!==null){keys(r.trace,['runtimeStarts','nativeStarts','nativeTurns','callbacks','schemaCalls','metadataCalls','refusals','allPidsAbsent']);if(Object.entries(r.trace).some(([k,v])=>k==='allPidsAbsent'?typeof v!=='boolean':!integer(v,1000)))fail()}
 if(r.accounting!==null){if(!Array.isArray(r.accounting)||r.accounting.length>16)fail();for(const a of r.accounting){
  keys(a,['invocation','requestedModel','requestedEffort','acknowledgedModel','acknowledgedEffort','admitted','reaped','success','terminalObserved','terminalStatus','usagePresent','usageValid','usage','finality','incomplete','fixedModelIneligible','rerouteCount'])
  if(!integer(a.invocation)||a.finality!=='unproven'||!integer(a.rerouteCount,8)||[a.requestedModel,a.acknowledgedModel].some(v=>v!==''&&v!=='gpt-6-sol'))fail()
  for(const field of ['admitted','reaped','success','terminalObserved','usagePresent','usageValid','incomplete','fixedModelIneligible'])if(typeof a[field]!=='boolean')fail()
  for(const field of ['requestedEffort','acknowledgedEffort']){keys(a[field],['state'],['value']);if(!['absent','null','invalid','value'].includes(a[field].state)||a[field].state==='value'&&a[field].value!=='medium')fail()}
  if(!['','completed','failed','interrupted','inProgress'].includes(a.terminalStatus))fail()
  keys(a.usage,['observation','last_scope','total_scope','last','total','model_context_window'])
  if(!integer(a.usage.observation,10000)||a.usage.last_scope!=='provider_last_breakdown'||a.usage.total_scope!=='provider_thread_total')fail()
  const value=v=>{keys(v,['state','value']);if(!['absent','null','invalid','value'].includes(v.state)||!integer(v.value))fail()}
  value(a.usage.model_context_window)
  for(const field of ['last','total']){keys(a.usage[field],['input_tokens','cached_input_tokens','output_tokens','reasoning_output_tokens','total_tokens','cache_write_input_tokens']);Object.values(a.usage[field]).forEach(value)}
 }}
 if(r.state==='complete'){
  const a=r.accounting?.[0],t=r.trace
  if(r.reason!=='complete'||r.browser?.state!=='complete'||r.browser.caseId!==c.id||r.accounting?.length!==1||!a.admitted||!a.reaped||!a.success||!a.terminalObserved||a.terminalStatus!=='completed'||a.incomplete||a.fixedModelIneligible||a.rerouteCount!==0||a.invocation!==1||r.dropped!==0||a.requestedModel!=='gpt-6-sol'||a.acknowledgedModel!=='gpt-6-sol'||a.requestedEffort.value!=='medium'||a.acknowledgedEffort.value!=='medium'||!t?.allPidsAbsent||t.nativeTurns!==1||t.callbacks!==r.bounds.maxCallbacks||t.schemaCalls!==r.bounds.maxCallbacks||t.refusals!==0)fail()
 }
 return r
}
if(process.argv[1]?.endsWith('/replay-report.mjs')&&process.argv[2]==='validate'){
 try{validateMountedReport(readPrivate(process.argv[3]));process.stdout.write('offline report valid\n')}catch{process.stderr.write('invalid offline report\n');process.exitCode=1}
}
