// Closed preparation/evidence readers only. No admission authority or executor.
import {cases, digest, keys, parseClosed, readPrivate} from './replay-report.mjs'
export {parseClosed}
const fail=()=>{throw Error('invalid-live-comparison-record')}
const exact=(a,b)=>JSON.stringify(a)===JSON.stringify(b)
const uint=v=>Number.isSafeInteger(v)&&v>=0
const sha=v=>typeof v==='string'&&/^[a-f0-9]{64}$/.test(v)
const text=v=>typeof v==='string'&&v.length>0&&v.length<=512
const id=v=>typeof v==='string'&&/^[A-Za-z0-9_-]{1,128}$/.test(v)
const bounded=v=>{if(Buffer.byteLength(JSON.stringify(v))>65536)fail()}
const freeze=v=>{if(v&&typeof v==='object'){Object.values(v).forEach(freeze);Object.freeze(v)}return v}
export const fixedInputs=freeze(cases.map((c,i)=>({...c,expectedCallbacks:i,toolResult:i?{name:'get_schema',args:{},text:'{"schema":"recorded"}',structuredContent:{schema:'recorded'},isError:false}:null})))
export const limits=freeze({
 productCeilings:{ordinaryCallbacks:20,criticCallbacks:4,frames:10000,nativeFrameBytes:4194304,promptBytes:200000,instructionsBytes:200000,toolDescriptionBytes:8192,toolSchemaBytes:65536,totalToolBytes:1048576,eventTextBytes:2097152,callbackArgumentBytes:262144,callbackResultTextBytes:1048576},
 completionCriteria:{invocationsPerCase:1,callbacksPerCase:[0,1]},
 proposedDeadlines:{caseMs:120000,aggregateMs:480000,qualification:'unqualified'},
 hardTokenCeiling:null,hardCurrencyCeiling:null
})
function decision(v){keys(v,['state','reference']);if(!['pending','recorded'].includes(v.state)||(v.state==='pending'?v.reference!==null:!sha(v.reference)))fail()}
function selection(v){keys(v,['model','effort']);if(!text(v.model)||v.model.length>128||/[\r\n\x00]/.test(v.model)||!['none','minimal','low','medium','high','xhigh','max'].includes(v.effort))fail()}
function identity(v,role){
 keys(v,['sourceCommit','sourceTree','sourceInventory','applicationExecutable','assets','cliVersion','cliExecutable','qualification'])
 if(!/^[a-f0-9]{40}$/.test(v.sourceCommit)||!/^[a-f0-9]{40}$/.test(v.sourceTree)||['sourceInventory','applicationExecutable','assets','cliExecutable'].some(k=>!sha(v[k]))||!text(v.cliVersion))fail()
 if(role==='baseline'){
  if(v.sourceCommit!=='ef378c406f8b96e2c0c8854bc5e4dff40ac841e2'||v.sourceTree!=='0034c3623b5b6e9bb8741da900392fd1c29e1508')fail()
  keys(v.qualification,['kind','originalSourceInventory','accountingPatch','overlayReview'])
  if(v.qualification.kind!=='published-source-with-accounting-only-overlay'||['originalSourceInventory','accountingPatch','overlayReview'].some(k=>!sha(v.qualification[k])))fail()
 }else{keys(v.qualification,['kind','review']);if(v.qualification.kind!=='candidate-source-build'||!sha(v.qualification.review))fail()}
}
export function preparationPlan(){
 return {kind:'live-comparison-preparation-plan',version:1,execution:'disabled',inputs:structuredClone(fixedInputs),selection:null,identities:{baseline:null,candidate:null},decisions:{model:{state:'pending',reference:null},bounds:{state:'pending',reference:null},subscriptionCostBasis:{state:'pending',reference:null}},limits:structuredClone(limits),answerChangeDecision:'pending'}
}
export function validatePreparationPlan(p){
 bounded(p);keys(p,['kind','version','execution','inputs','selection','identities','decisions','limits','answerChangeDecision'])
 if(p.kind!=='live-comparison-preparation-plan'||p.version!==1||p.execution!=='disabled'||!exact(p.inputs,fixedInputs)||!exact(p.limits,limits)||p.answerChangeDecision!=='pending')fail()
 if(p.selection!==null)selection(p.selection)
 keys(p.identities,['baseline','candidate']);for(const role of ['baseline','candidate'])if(p.identities[role]!==null)identity(p.identities[role],role)
 keys(p.decisions,['model','bounds','subscriptionCostBasis']);Object.values(p.decisions).forEach(decision)
 if(p.identities.baseline&&p.identities.candidate&&(p.identities.baseline.cliVersion!==p.identities.candidate.cliVersion||p.identities.baseline.cliExecutable!==p.identities.candidate.cliExecutable))fail()
 if(p.decisions.model.state==='recorded'&&p.selection===null)fail()
 return p
}
function observation(v){keys(v,['state','value']);if(!['absent','null','invalid','value'].includes(v.state)||!uint(v.value))fail()}
function usage(v){
 keys(v,['observation','last_scope','total_scope','last','total','model_context_window'])
 if(!uint(v.observation)||v.last_scope!=='provider_last_breakdown'||v.total_scope!=='provider_thread_total')fail()
 observation(v.model_context_window)
 for(const field of ['last','total']){keys(v[field],['input_tokens','cached_input_tokens','output_tokens','reasoning_output_tokens','total_tokens','cache_write_input_tokens']);Object.values(v[field]).forEach(observation)}
}
function accounting(v,selected){
 keys(v,['state','dropped','reports'])
 if(v.state==='unavailable'){if(v.dropped!==null||v.reports!==null)fail();return}
 if(v.state!=='observed'||v.dropped!==0||!Array.isArray(v.reports)||v.reports.length!==1)fail()
 const a=v.reports[0]
 keys(a,['invocation','requestedModel','requestedEffort','acknowledgedModel','acknowledgedEffort','admitted','reaped','success','terminalObserved','terminalStatus','usagePresent','usageValid','usage','finality','incomplete','fixedModelIneligible','rerouteCount'])
 if(!uint(a.invocation)||a.invocation<1||a.requestedModel!==selected.model||a.acknowledgedModel!==selected.model||a.finality!=='unproven'||a.terminalStatus!=='completed'||a.rerouteCount!==0||a.incomplete!==false||a.fixedModelIneligible!==false)fail()
 for(const k of ['requestedEffort','acknowledgedEffort']){keys(a[k],['state','value']);if(a[k].state!=='value'||a[k].value!==selected.effort)fail()}
 for(const k of ['admitted','reaped','success','terminalObserved'])if(a[k]!==true)fail()
 if(typeof a.usagePresent!=='boolean'||typeof a.usageValid!=='boolean')fail()
 usage(a.usage)
 if(a.usagePresent!==a.usageValid||a.usagePresent!==(a.usage.observation>0))fail()
 if(!a.usagePresent&&Object.values(a.usage.last).concat(Object.values(a.usage.total),[a.usage.model_context_window]).some(o=>o.state!=='absent'))fail()
}
function input(v,fixed){keys(v,['caseId','prompt','corpusSha256','sliceSha256','toolResult']);if(v.caseId!==fixed.id||v.prompt!==fixed.prompt||v.corpusSha256!==fixed.corpusSha256||!sha(v.sliceSha256)||!exact(v.toolResult,fixed.toolResult))fail()}
function config(v,selected){keys(v,['model','effort','critic','research','mode','phase','phaseInput']);if(v.model!==selected.model||v.effort!==selected.effort||v.critic!=='off'||v.research!=='provided'||v.mode!=='draft'||v.phase!=='work')fail();keys(v.phaseInput,['state','value']);if(!['absent','value'].includes(v.phaseInput.state)||v.phaseInput.value!==(v.phaseInput.state==='absent'?null:'work'))fail()}
function answers(v){
 if(!Array.isArray(v)||v.length<1||v.length>8||new Set(v.map(a=>a.id)).size!==v.length)fail()
 for(const a of v){keys(a,['id','text','renderedText','reloadedText']);if(!id(a.id)||typeof a.text!=='string'||!a.text||typeof a.renderedText!=='string'||!a.renderedText||a.reloadedText!==a.renderedText)fail()}
}
function callbacks(v,want,role){
 if(!Array.isArray(v)||v.length!==want)fail()
 for(const c of v){
  keys(c,['callId','name','args','text','structuredState','structured','isError','runtimeResult'])
  if(!id(c.callId)||c.name!==(role==='baseline'?'desk_runtime_0':'get_schema')||!exact(c.args,{})||!text(c.text)||role==='candidate'&&c.text!=='{"schema":"recorded"}'||c.isError!==false)fail()
  if(role==='baseline'){if(c.structuredState!=='unavailable-native-contract'||c.structured!==null)fail()}
  else if(c.structuredState==='absent'){if(c.structured!==null)fail()}
  else if(c.structuredState!=='value'||!exact(c.structured,{schema:'recorded'}))fail()
  if(!exact(c.runtimeResult,{name:'get_schema',args:{},text:'{"schema":"recorded"}',structuredContent:{schema:'recorded'},isError:false}))fail()
 }
}
function caseReport(v,fixed,role,selected){
 keys(v,['provenance','input','runId','chatId','config','answers','answerCount','counts','callbacks','canonicalReceipts','terminal','lifecycle','accounting','evidenceSha256'])
 if(v.provenance!=='normal-application-live'||!id(v.runId)||!id(v.chatId)||!sha(v.evidenceSha256))fail()
 input(v.input,fixed);config(v.config,selected);answers(v.answers)
 if(v.answerCount!==v.answers.length)fail()
 keys(v.counts,['applicationStarts','managerInvocations','callbacks','schemaCalls','browserErrors','unexpectedRequests','reloadInvocations'])
 if(!exact(v.counts,{applicationStarts:1,managerInvocations:1,callbacks:fixed.expectedCallbacks,schemaCalls:fixed.expectedCallbacks,browserErrors:0,unexpectedRequests:0,reloadInvocations:0}))fail()
 callbacks(v.callbacks,fixed.expectedCallbacks,role)
 keys(v.canonicalReceipts,['state','evidenceSha256'])
 if(role==='baseline'){if(v.canonicalReceipts.state!=='unavailable-baseline-contract'||v.canonicalReceipts.evidenceSha256!==null)fail()}
 else if(v.canonicalReceipts.state!=='observed'||!sha(v.canonicalReceipts.evidenceSha256))fail()
 keys(v.terminal,['observed','status','evidenceSha256']);if(v.terminal.observed!==true||v.terminal.status!=='completed'||!sha(v.terminal.evidenceSha256))fail()
 keys(v.lifecycle,['normalOwnerShutdown','ownedChildReaped','allRecordedPidsAbsent','evidenceSha256']);if(v.lifecycle.normalOwnerShutdown!==true||v.lifecycle.ownedChildReaped!==true||v.lifecycle.allRecordedPidsAbsent!==true||!sha(v.lifecycle.evidenceSha256))fail()
 accounting(v.accounting,selected)
}
export function validateObservedComparison(r,p){
 validatePreparationPlan(p);bounded(r)
 // Recorded references are prerequisites for interpreting evidence, never grants.
 if(p.selection===null||Object.values(p.identities).some(v=>v===null)||Object.values(p.decisions).some(d=>d.state!=='recorded'))fail()
 keys(r,['kind','version','provenance','state','planSha256','selection','identities','applications','monetary','answerChangeDecision'])
 if(r.kind!=='live-comparison-observed-report'||r.version!==1||r.provenance!=='normal-application-live'||r.state!=='complete-for-human-review'||r.planSha256!==digest(p)||!exact(r.selection,p.selection)||!exact(r.identities,p.identities)||r.answerChangeDecision!=='pending')fail()
 keys(r.monetary,['basis','value']);if(r.monetary.basis!=='unavailable-subscription'||r.monetary.value!==null)fail()
 keys(r.applications,['baseline','candidate'])
 for(const role of ['baseline','candidate']){
  const rows=r.applications[role];if(!Array.isArray(rows)||rows.length!==fixedInputs.length)fail()
  rows.forEach((v,i)=>caseReport(v,fixedInputs[i],role,p.selection))
 }
 for(let i=0;i<fixedInputs.length;i++)if(!exact(r.applications.baseline[i].input,r.applications.candidate[i].input)||!exact(r.applications.baseline[i].config,r.applications.candidate[i].config))fail()
 return r
}
export const readPreparationPlan=path=>validatePreparationPlan(readPrivate(path))
export const readObservedComparison=(path,plan)=>validateObservedComparison(readPrivate(path),plan)
