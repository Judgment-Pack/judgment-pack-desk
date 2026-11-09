import {test} from 'node:test'
import assert from 'node:assert/strict'
import {cases,validateControl,validateBrowserReport,validateMountedReport,parseClosed,captureSettings,captureTools,retainBrowserObservations} from './replay-report.mjs'
const control=()=>({kind:'offline-mounted-replay-control',provenance:'offline-test',caseId:cases[0].id,prompt:cases[0].prompt,model:'gpt-6-sol',effort:'medium',caseDeadlineMs:60000,aggregateDeadlineMs:180000,origin:'http://127.0.0.1:8791',setupCode:'fixture-only-secret-12345678',browserBin:'/supplied/browser',reportPath:'/owned/report.json'})
const browser=()=>({kind:'offline-mounted-browser-result',provenance:'offline-test',caseId:cases[0].id,state:'incomplete',reason:'pending',runId:null,chatId:null,answers:null,tools:null,operations:null,counts:null,settings:null,elapsedMs:0})
const value=(state='value',value=0)=>({state,value})
const breakdown=()=>({input_tokens:value('value',2),cached_input_tokens:value(),output_tokens:value('value',3),reasoning_output_tokens:value('value',1),total_tokens:value('value',5),cache_write_input_tokens:value('absent')})
const accounting=()=>({invocation:1,requestedModel:'gpt-6-sol',requestedEffort:{state:'value',value:'medium'},acknowledgedModel:'gpt-6-sol',acknowledgedEffort:{state:'value',value:'medium'},admitted:true,reaped:true,success:true,terminalObserved:true,terminalStatus:'completed',usagePresent:true,usageValid:true,usage:{observation:1,last_scope:'provider_last_breakdown',total_scope:'provider_thread_total',last:breakdown(),total:breakdown(),model_context_window:value('absent')},finality:'unproven',incomplete:false,fixedModelIneligible:false,rerouteCount:0})
const report=()=>({kind:'offline-mounted-case-report',provenance:'offline-test',qualification:'provisional',state:'incomplete',reason:'pending',input:{caseId:cases[0].id,prompt:cases[0].prompt,corpusSha256:cases[0].corpusSha256,sliceSha256:'a'.repeat(64)},bounds:{caseMs:60000,aggregateMs:180000,maxNativeTurns:1,maxCallbacks:0},identities:Object.fromEntries(['launcher','driver','reader','peer','applicationExecutable','browserExecutable','assets','sourceInventory'].map(k=>[k,'b'.repeat(64)])),browser:null,accounting:null,dropped:null,trace:null})
test('control requires exact offline case, model, effort, bounds and loopback origin',()=>{
 assert.equal(validateControl(control()).provenance,'offline-test')
 for(const patch of [{provenance:'live'},{model:''},{effort:''},{caseDeadlineMs:120000},{origin:'http://127.0.0.1:8791/?secret=x'},{authorized:true},{prompt:'changed'},{browserBin:'chrome'}])assert.throws(()=>validateControl({...control(),...patch}))
})
test('missing observations stay null and cannot become complete',()=>{
 const b=browser(),r=report();validateBrowserReport(b);validateMountedReport(r)
 assert.equal(r.accounting,null);assert.equal(r.dropped,null)
 assert.throws(()=>validateBrowserReport({...b,state:'complete',reason:'complete'}))
 assert.throws(()=>validateMountedReport({...r,state:'complete',reason:'complete'}))
})
test('handwritten completed fixture retains observed zeros and absent optional counters',()=>{
 const r=report(),b=browser();Object.assign(b,{state:'complete',reason:'complete',runId:'arun_fixture',chatId:'chat-fixture',answers:[{id:'message-fixture',text:'Fixture.',renderedText:'Fixture.',reloadedText:'Fixture.'}],tools:[],operations:[{id:'operation-fixture',callId:'call-fixture',kind:'model',name:'fixture',state:'completed',intentSha256:'c'.repeat(64),result:'e30='}],counts:{starts:1,models:1,callbacks:0,browserErrors:0,unexpectedRequests:0},settings:{model:'gpt-6-sol',effort:'medium',critic:'off',research:'provided',mode:'draft',phase:'work',phaseInput:{state:'absent',value:null}},elapsedMs:100})
 Object.assign(r,{state:'complete',reason:'complete',browser:b,accounting:[accounting()],dropped:0,trace:{runtimeStarts:2,nativeStarts:2,nativeTurns:1,callbacks:0,schemaCalls:0,metadataCalls:6,refusals:0,allPidsAbsent:true}})
 validateMountedReport(r)
 assert.equal(r.accounting[0].usage.last.cached_input_tokens.value,0)
 assert.equal(r.accounting[0].usage.last.cache_write_input_tokens.state,'absent')
 for(const change of [r=>r.accounting[0].finality='proven',r=>r.accounting[0].reaped=false,r=>r.accounting[0].rerouteCount=1,r=>r.trace.nativeTurns=2,r=>r.browser.answers=[],r=>r.browser.counts.unexpectedRequests=1,r=>r.accounting[0].rawFrame='secret',r=>r.dropped=1]){const mutant=structuredClone(r);change(mutant);assert.throws(()=>validateMountedReport(mutant))}
 const partial=structuredClone(r);partial.state='incomplete';partial.reason='accounting-incomplete';partial.accounting[0].incomplete=true;partial.accounting[0].usage.total.total_tokens=value('invalid',5);validateMountedReport(partial);assert.equal(partial.accounting[0].usage.total.total_tokens.value,5)
})
test('closed parser refuses duplicate, trailing, deeply nested, malformed and oversized JSON',()=>{
 assert.deepEqual(parseClosed('{"one":[0,null,false]}'),{one:[0,null,false]})
 for(const s of ['{"a":1,"a":2}','{"a":{"b":1,"b":2}}','{} {}','{"a":}', '['.repeat(18)+'0'+']'.repeat(18), '"'+'x'.repeat(65536)+'"'])assert.throws(()=>parseClosed(s))
})

test('bootstrap diagnosis accepts only closed subphase and error names on incomplete reports',()=>{
 const b={...browser(),reason:'bootstrap',bootstrapFailure:{subphase:'launch',errorName:'Error'}}
 validateBrowserReport(b)
 for(const patch of [{subphase:'raw-path',errorName:'Error'},{subphase:'launch',errorName:'raw-error'},{subphase:'launch',errorName:'Error',message:'private'}])assert.throws(()=>validateBrowserReport({...b,bootstrapFailure:patch}))
 assert.throws(()=>validateBrowserReport({...b,state:'complete',reason:'complete'}))
})

const settings=()=>({model:'gpt-6-sol',reasoning:{effort:'medium'},critic:'off',researchMode:'provided',mode:'draft'})
test('phase omission is defaulted by production contract, explicit input stays distinct',()=>{
 const absent=captureSettings(settings(),{phase:'work'})
 assert.deepEqual(absent,{model:'gpt-6-sol',effort:'medium',critic:'off',research:'provided',mode:'draft',phase:'work',phaseInput:{state:'absent',value:null}})
 assert.deepEqual(captureSettings({...settings(),interaction:'work'},{phase:'work'}).phaseInput,{state:'value',value:'work'})
 for(const interaction of [null,'plan','unknown'])assert.throws(()=>captureSettings({...settings(),interaction},{phase:'work'}))
 assert.throws(()=>captureSettings(settings(),{}))
})
test('tool capture pairs exact IDs and names, retaining optional structured missingness',()=>{
 const call={id:'event-call',event:{type:'tool_call',callId:'call-schema',name:'get_schema',args:{}}}
 const result={id:'event-result',event:{type:'tool_result',callId:'call-schema',name:'get_schema',text:'{"schema":"recorded"}',isError:false}}
 const expected={callId:'call-schema',callEventId:'event-call',resultEventId:'event-result',name:'get_schema',args:{},text:'{"schema":"recorded"}',structured:null,structuredState:'absent',isError:false}
 assert.deepEqual(captureTools([call,result]),[expected])
 assert.deepEqual(captureTools([call,{...result,event:{...result.event,structured:{schema:'recorded'}}}]),[{...expected,structured:{schema:'recorded'},structuredState:'value'}])
 for(const events of [[call,{...call,event:{...call.event,callId:'call-two'}},result,result],[call],[result],[call,call,result,result],[call,{...result,event:{...result.event,callId:'other'}}],[call,{...result,event:{...result.event,name:'other'}}]])assert.throws(()=>captureTools(events))
})
test('partial invalid capture preserves only individually validated observations and closed stage',()=>{
 const partial={...browser(),reason:'capture',captureStage:'answers',runId:'arun_observed',chatId:'chat-observed',settings:{...captureSettings(settings(),{phase:'work'}),effort:undefined},answers:[{id:'message-one',text:'First.',renderedText:'First.',reloadedText:null}]}
 const retained=retainBrowserObservations(partial)
 assert.equal(retained.runId,'arun_observed');assert.equal(retained.elapsedMs,0)
 assert.equal(retained.settings.effort,null);assert.equal(retained.settings.phase,'work')
 assert.deepEqual(retained.settings.phaseInput,{state:'absent',value:null})
 assert.deepEqual(retained.answers,partial.answers);
 const ordered={...partial,answers:[...partial.answers,{id:'message-two',text:'Second.',renderedText:'Second.',reloadedText:null}]}
 assert.deepEqual(retainBrowserObservations(ordered).answers.map(a=>a.id),['message-one','message-two'])
 assert.equal(retainBrowserObservations({...partial,answers:[...partial.answers,...partial.answers]}).answers,null)
 assert.equal(retainBrowserObservations({...partial,answers:[{...partial.answers[0],text:'x'.repeat(65536)}]}).answers,null);assert.equal(retained.captureStage,'answers')
 validateBrowserReport(retained)
 assert.throws(()=>validateBrowserReport({...retained,state:'complete',reason:'complete'}))
 assert.equal(retainBrowserObservations({...partial,tools:[{raw:'private'}]}).tools,null)
 assert.throws(()=>validateBrowserReport({...retained,captureStage:'raw failure prose'}))
})
