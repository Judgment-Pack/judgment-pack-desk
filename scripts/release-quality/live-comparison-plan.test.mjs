import {test} from 'node:test'
import assert from 'node:assert/strict'
import {digest} from './replay-report.mjs'
import {preparationPlan,validatePreparationPlan,validateObservedComparison,parseClosed,fixedInputs} from './live-comparison-plan.mjs'
const hash='a'.repeat(64)
const identity=role=>({sourceCommit:role==='baseline'?'ef378c406f8b96e2c0c8854bc5e4dff40ac841e2':'b'.repeat(40),sourceTree:role==='baseline'?'0034c3623b5b6e9bb8741da900392fd1c29e1508':'c'.repeat(40),sourceInventory:hash,applicationExecutable:hash,assets:hash,cliVersion:'codex-cli supplied-version',cliExecutable:hash,qualification:role==='baseline'?{kind:'published-source-with-accounting-only-overlay',originalSourceInventory:hash,accountingPatch:hash,overlayReview:hash}:{kind:'candidate-source-build',review:hash}})
const selectedPlan=()=>{
 const p=preparationPlan();p.selection={model:'explicit-model',effort:'medium'}
 p.identities={baseline:identity('baseline'),candidate:identity('candidate')}
 for(const d of Object.values(p.decisions)){d.state='recorded';d.reference=hash}
 return p
}
const observed=(p=selectedPlan())=>({kind:'live-comparison-observed-report',version:1,provenance:'normal-application-live',state:'complete-for-human-review',planSha256:digest(p),selection:structuredClone(p.selection),identities:structuredClone(p.identities),applications:Object.fromEntries(['baseline','candidate'].map(role=>[role,fixedInputs.map((f,i)=>({
 provenance:'normal-application-live',input:{caseId:f.id,prompt:f.prompt,corpusSha256:f.corpusSha256,sliceSha256:hash,toolResult:structuredClone(f.toolResult)},runId:`run_${role}_${i}`,chatId:`chat_${role}_${i}`,
 config:{...p.selection,critic:'off',research:'provided',mode:'draft',phase:'work',phaseInput:{state:'absent',value:null}},
 answers:[{id:`message_${role}_${i}`,text:'First saved answer.',renderedText:'First saved answer.',reloadedText:'First saved answer.'},{id:`second_${role}_${i}`,text:'Second saved answer.',renderedText:'Second saved answer.',reloadedText:'Second saved answer.'}],answerCount:2,
 counts:{applicationStarts:1,managerInvocations:1,callbacks:i,schemaCalls:i,browserErrors:0,unexpectedRequests:0,reloadInvocations:0},
 callbacks:i?[{callId:`call_${role}`,name:role==='baseline'?'desk_runtime_0':'get_schema',args:{},text:role==='baseline'?'Exact native framed result.':'{"schema":"recorded"}',structuredState:role==='baseline'?'unavailable-native-contract':'value',structured:role==='baseline'?null:{schema:'recorded'},isError:false,runtimeResult:structuredClone(f.toolResult)}]:[],
 canonicalReceipts:{state:role==='baseline'?'unavailable-baseline-contract':'observed',evidenceSha256:role==='baseline'?null:hash},terminal:{observed:true,status:'completed',evidenceSha256:hash},lifecycle:{normalOwnerShutdown:true,ownedChildReaped:true,allRecordedPidsAbsent:true,evidenceSha256:hash},accounting:{state:'unavailable',dropped:null,reports:null},evidenceSha256:hash
 }))])),monetary:{basis:'unavailable-subscription',value:null},answerChangeDecision:'pending'})
const value=(state='value',value=0)=>({state,value})
const breakdown=()=>({input_tokens:value('value',2),cached_input_tokens:value(),output_tokens:value('value',3),reasoning_output_tokens:value('value',1),total_tokens:value('value',5),cache_write_input_tokens:value('absent')})
const sink=()=>({state:'observed',dropped:0,reports:[{invocation:7,requestedModel:'explicit-model',requestedEffort:{state:'value',value:'medium'},acknowledgedModel:'explicit-model',acknowledgedEffort:{state:'value',value:'medium'},admitted:true,reaped:true,success:true,terminalObserved:true,terminalStatus:'completed',usagePresent:true,usageValid:true,usage:{observation:1,last_scope:'provider_last_breakdown',total_scope:'provider_thread_total',last:breakdown(),total:breakdown(),model_context_window:value('absent')},finality:'unproven',incomplete:false,fixedModelIneligible:false,rerouteCount:0}]})
function rejects(changes){const p=selectedPlan();for(const change of changes){const r=observed(p);change(r);assert.throws(()=>validateObservedComparison(r,p))}}
test('pending preparation is disabled and carries no implicit model, identity or approval',()=>{
 const p=preparationPlan();assert.equal(validatePreparationPlan(p),p);assert.equal(p.selection,null);assert.equal(p.identities.baseline,null)
 for(const patch of [{execution:'enabled'},{approved:true},{answerChangeDecision:'accepted'},{selection:{model:'explicit-model'}},{selection:{model:'',effort:'medium'}}])assert.throws(()=>validatePreparationPlan({...p,...patch}))
 assert.throws(()=>validateObservedComparison(observed(),p))
 const pending=selectedPlan();pending.decisions.bounds={state:'pending',reference:null};assert.throws(()=>validateObservedComparison(observed(pending),pending))
})
test('completion criteria cannot be renamed hard caps or unqualified deadlines enforced',()=>{
 for(const change of [p=>p.limits.completionCriteria.invocationsPerCase=2,p=>p.limits.hardTokenCeiling=0,p=>p.limits.hardCurrencyCeiling=0,p=>p.limits.proposedDeadlines.qualification='enforced',p=>p.limits.productCeilings.ordinaryCallbacks=1,p=>p.limits.maxCallbacks=1]){const p=preparationPlan();change(p);assert.throws(()=>validatePreparationPlan(p))}
})
test('handwritten complete observation preserves full answer arrays and order for human review',()=>{
 const p=selectedPlan(),r=observed(p);assert.equal(validateObservedComparison(r,p),r)
 assert.deepEqual(r.applications.baseline[0].answers.map(a=>a.text),['First saved answer.','Second saved answer.'])
 assert.equal(r.answerChangeDecision,'pending');assert.equal(r.monetary.value,null)
 // Changed answers are evidence for the later human gate, not a semantic score.
 r.applications.candidate[0].answers[0]={id:'changed',text:'Changed.',renderedText:'Changed.',reloadedText:'Changed.'};validateObservedComparison(r,p)
})
test('offline, standalone CLI and accepted claims are refused',()=>rejects([
 r=>r.provenance='offline-test',r=>r.provenance='standalone-cli',r=>r.applications.baseline[0].provenance='offline-test',r=>r.state='accepted',r=>r.answerChangeDecision='accepted',r=>r.monetary.value=0,r=>r.semanticQualityScore=1
]))
test('selected model, fixed exact inputs and source/build qualification are required',()=>rejects([
 r=>r.selection.model='substitute',r=>r.applications.candidate[0].config.effort='low',r=>r.applications.candidate[0].input.prompt='changed',r=>r.applications.candidate.reverse(),r=>r.applications.candidate[0].input.sliceSha256='b'.repeat(64),r=>r.identities.baseline.qualification.kind='unchanged-published-binary',r=>delete r.identities.candidate.assets,r=>r.planSha256='b'.repeat(64)
]))
test('missing lifecycle, terminal or complete saved/reloaded answers cannot pass',()=>rejects([
 r=>delete r.applications.candidate[0].lifecycle,r=>r.applications.baseline[0].lifecycle.ownedChildReaped=false,r=>r.applications.candidate[0].terminal.status='interrupted',r=>r.applications.baseline[0].answers=[],r=>r.applications.baseline[0].answers[0].reloadedText=null,r=>r.applications.candidate[0].answers.pop(),r=>r.applications.candidate[0].counts.managerInvocations=2,r=>r.applications.baseline[1].callbacks=[],r=>r.applications.baseline[0].counts.reloadInvocations=1
]))
test('unavailable baseline canonical receipts and structured native fields stay unavailable',()=>rejects([
 r=>r.applications.baseline[0].canonicalReceipts={state:'observed',evidenceSha256:hash},r=>r.applications.baseline[1].callbacks[0].structuredState='value',r=>r.applications.baseline[1].callbacks[0].runtimeResult.text='different',r=>r.applications.candidate[0].canonicalReceipts.evidenceSha256=null
]))
test('optional accounting retains observed zero separately from absent counters and unproven finality',()=>{
 const p=selectedPlan(),r=observed(p),a=sink();r.applications.baseline[0].accounting=a;validateObservedComparison(r,p)
 assert.equal(a.reports[0].usage.last.cached_input_tokens.value,0);assert.equal(a.reports[0].usage.last.cache_write_input_tokens.state,'absent');assert.equal(a.reports[0].invocation,7)
 for(const change of [a=>a.dropped=null,a=>a.reports[0].finality='proven',a=>a.reports[0].rerouteCount=1,a=>a.reports[0].requestedEffort={state:'absent',value:'medium'},a=>delete a.reports[0].usage.last.cached_input_tokens,a=>a.reports[0].usage.total_scope='aggregate-inference',a=>a.reports[0].reaped=false]){const bad=structuredClone(r);change(bad.applications.baseline[0].accounting);assert.throws(()=>validateObservedComparison(bad,p))}
 const missing=observed(p);missing.applications.baseline[0].accounting.dropped=0;assert.throws(()=>validateObservedComparison(missing,p))
})
test('missing usage stays absent rather than inferred zeros or subscription price',()=>{
 const p=selectedPlan(),r=observed(p),a=sink();a.reports[0].usagePresent=false;a.reports[0].usageValid=false;a.reports[0].usage.observation=0
 for(const group of ['last','total'])for(const k of Object.keys(a.reports[0].usage[group]))a.reports[0].usage[group][k]=value('absent')
 r.applications.baseline[0].accounting=a;validateObservedComparison(r,p)
 a.reports[0].usage.last.input_tokens=value();assert.throws(()=>validateObservedComparison(r,p))
})
test('existing bounded closed parser rejects duplicate, unknown and oversized payloads',()=>{
 for(const s of ['{"a":1,"a":2}','{} {}','['.repeat(18)+'0'+']'.repeat(18),'"'+'x'.repeat(65536)+'"'])assert.throws(()=>parseClosed(s))
 assert.throws(()=>validatePreparationPlan({...preparationPlan(),live:true}))
})
