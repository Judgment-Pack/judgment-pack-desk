import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {runInNewContext} from 'node:vm'
import {test} from 'node:test'
import {projectionReady,diagnostic,stages} from './persisted-proposal-report.mjs'

const browser=readFileSync(new URL('./persisted-proposal-browser.mjs',import.meta.url),'utf8')
// Exercise the driver's actual deadline and polling functions with a virtual clock.
// Expectations below come from the documented complete-before-projection ordering.
function polling(){
 let now=0
 const clock=browser.match(/^const started=.*$/m)?.[0]
 const wait=browser.match(/^const wait=async f=>.*$/m)?.[0]
 assert(clock&&wait)
 const context={assert,Date:{now:()=>now},setTimeout:resolve=>{now+=100;resolve()},observeUI:async()=>{},errors:0,unexpected:0}
 const poll=runInNewContext(clock+';'+wait+';wait',context)
 return {poll,now:()=>now}
}
function diagnostics(terminal){return {stages:Object.fromEntries(stages.map(k=>[k,null])),click:'not-started',button:'unknown',messages:[],requests:[],readOnly:'none',exceptions:[],stop:'none',terminal}}

test('complete without acknowledgement waits for the observed acknowledged snapshot',async()=>{
 const {poll,now}=polling(),observations=[]
 const snapshots=[{state:'running'},{state:'complete'},{state:'complete',chatProjection:{acknowledged:false}},{state:'complete',chatProjection:{acknowledged:true}}]
 let reads=0,run
 await poll(async()=>{run=snapshots[reads++];return projectionReady(run,observations,now())})
 assert.equal(reads,4)
 assert.equal(run.state,'complete');assert.equal(run.chatProjection.acknowledged,true)
 assert.deepEqual(observations,[{state:'complete',acknowledged:null,atMs:100,count:1},{state:'complete',acknowledged:false,atMs:200,count:1},{state:'complete',acknowledged:true,atMs:300,count:1}])
 diagnostic(diagnostics(observations))
})

test('failed, interrupted, stopped and paused terminal snapshots refuse immediately',async()=>{
 for(const state of ['failed','interrupted','stopped','paused-budget','awaiting-input','awaiting-client']){
  const {poll,now}=polling(),observations=[];let reads=0
  await assert.rejects(poll(async()=>{reads++;return projectionReady({state,chatProjection:{acknowledged:true}},observations,now())}),{name:'AssertionError'})
  assert.equal(reads,1);assert.equal(now(),0)
  assert.deepEqual(observations,[{state,acknowledged:true,atMs:0,count:1}]);diagnostic(diagnostics(observations))
 }
})

test('unacknowledged completion expires at the original shared sixty second deadline',async()=>{
 const {poll,now}=polling(),observations=[];let reads=0
 // Earlier work consumes the same budget; terminal polling cannot reset it.
 await poll(async()=>now()>=59000)
 await assert.rejects(poll(async()=>{reads++;return projectionReady({state:'complete',chatProjection:{acknowledged:false}},observations,now())}),{name:'AssertionError'})
 assert.equal(now(),60000);assert.equal(reads,10)
 assert.deepEqual(observations,[{state:'complete',acknowledged:false,atMs:59000,count:10}]);diagnostic(diagnostics(observations))
})

test('observation categories and counts remain bounded and omit unrecognized values',()=>{
 const observations=[]
 for(let i=0;i<4100;i++)assert.equal(projectionReady({state:'complete',chatProjection:{acknowledged:false},message:'not retained'},observations,10),false)
 assert.deepEqual(observations,[{state:'complete',acknowledged:false,atMs:10,count:4096}])
 assert.equal(projectionReady({state:'unknown',chatProjection:{acknowledged:true}},observations,20),false)
 diagnostic(diagnostics(observations))
 assert.throws(()=>diagnostic(diagnostics([{state:'unknown',acknowledged:false,atMs:0,count:1}])),{name:'AssertionError'})
 assert.throws(()=>diagnostic(diagnostics([...observations,...observations])),{name:'AssertionError'})
 // Existing diagnostic producers remain readable without the new optional evidence.
 const legacy=diagnostics([]);delete legacy.terminal;diagnostic(legacy)
})
