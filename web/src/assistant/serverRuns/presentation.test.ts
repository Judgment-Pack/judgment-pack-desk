import { expect, it } from 'vitest'
import { INITIAL_STATE } from '../../research/initialState'
import { decodeSnapshot } from './decode'
import { presentRun } from './presentation'
import pending from './fixtures/pending.json'
it('retains parent and child public previews as display-only data outside durable turns and replay cursor',()=>{
 const task={id:'child-one',parentRunId:'parent-one',title:'Child review',objective:'Review the supplied document.',profile:'review',state:'running',createdAt:'2026-10-23T00:00:00Z',modelRequests:1,toolCalls:0,attempts:1,steps:[]}
 const snapshot=decodeSnapshot({...pending,state:'stopped',events:[{id:'task-event',event:{type:'delegated_task',task}}],cursor:'1:1',displayPreview:[{text:'Parent written so far'},{taskId:task.id,text:'Child written so far'}]},{owner:{...pending.owner,kind:'chat'},requestId:pending.requestId})
 const view=presentRun(INITIAL_STATE,snapshot)
 expect(view.streaming).toBe('Parent written so far');expect(view.turns).toEqual([])
 expect(view.responses?.[0].work.items[0]).toMatchObject({id:task.id,status:'interrupted',progress:'Child written so far'})
 expect(snapshot.cursor).toBe('1:1');expect(snapshot.events).toHaveLength(1)
})

it('presents an explicit Plan pause as needs-input without budget continuation',async()=>{
 const {default:plan}=await import('./fixtures/plan-decision.json')
 const snapshot=decodeSnapshot(plan,{owner:{...plan.owner,kind:'chat'},requestId:plan.requestId})
 expect(presentRun(INITIAL_STATE,snapshot).status).toBe('needs-input')
 expect(presentRun(INITIAL_STATE,snapshot).execution?.plan).toEqual(plan.checkpoint.plan)
})

it('projects physical choice controls fresh and after reload without forging canonical tool results',async()=>{
 const {sha256}=await import('@judgment-pack/harness/records')
 const {readResponseHistory}=await import('../../chat/responseHistory')
 const sourceBinding={ownTool:'search_sources',wireName:'desk-search__search_sources'},encode=(v:unknown)=>btoa(JSON.stringify(v))
 const control={version:1,kind:'research-choice',hostOperationId:'host-one',sourceBindingDigest:sha256(JSON.stringify(sourceBinding)),requestId:pending.requestId,query:'public policy',reason:'choice-required'}
 const externalCall={callId:'source-call',hostOperationId:'host-one',ownTool:'search_sources',wireName:sourceBinding.wireName,outcome:'not-started',control}
 const event={type:'tool_call',callId:'source-call',name:sourceBinding.wireName,args:{query:'public policy'}}
 const op={id:'host-one',callId:'host-one',kind:'host',name:'search_sources',state:'refused',intent:encode({callId:'source-call',root:false,arguments:encode(event.args),sourceBinding}),result:encode({result:encode({isError:true}),externalControl:control,externalFailure:encode({version:1,code:'permission',source:'desk',outcome:'not-started'})})}
 const vector={...pending,state:'stopped',operations:[op],externalCalls:[externalCall],events:[{id:'source-event',event}],cursor:'1:1'}
 const binding={owner:{...pending.owner,kind:'chat' as const},requestId:pending.requestId}
 const snapshot=decodeSnapshot(vector,binding)
 const presented=presentRun(INITIAL_STATE,snapshot),work=presented.responses![0].work
 expect(work.items[0]).toMatchObject({name:'search_sources',status:'failed',researchSuggestion:true,externalControl:control,search:{query:'public policy'}})
 expect(work.notices).toContain('Choose research to search external sources. No research started for this operation.')
 expect(snapshot.events).toEqual(vector.events)
 expect(presented.events).toEqual([event])
 expect(presented.events.filter(e=>e.type==='tool_result')).toEqual([])
 const saved=readResponseHistory(JSON.parse(JSON.stringify(presented.responses)))
 expect(saved[0].work.items[0].externalControl).toEqual(control)
 expect(presentRun(INITIAL_STATE,decodeSnapshot(JSON.parse(JSON.stringify(vector)),binding)).responses![0].work).toEqual(work)
 const unknown=presentRun(INITIAL_STATE,decodeSnapshot({...vector,operations:[{...op,result:encode({result:encode({isError:true}),externalFailure:encode({version:1,code:'storage',source:'tool',outcome:'unknown'})})}],externalCalls:[{...externalCall,outcome:'unknown',control:undefined}]},binding)).responses![0].work
 expect(unknown.items[0].researchSuggestion).toBeUndefined()
 expect(unknown.notices).toContain('External source outcome is unknown. Review retained work before starting another request.')
 expect(unknown.notices.some(n=>n.includes('No research started'))).toBe(false)
 // Arbitrary wire-like prose without an authenticated mapping stays unchanged.
 const noMapping=presentRun(INITIAL_STATE,decodeSnapshot({...vector,externalCalls:undefined},binding))
 expect(noMapping.responses![0].work.items[0].name).toBe(sourceBinding.wireName)
})
