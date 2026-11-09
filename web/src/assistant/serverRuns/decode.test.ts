import approvalRecords from './fixtures/approval-records.json'
import approval from './fixtures/approval.json'
import {approvalShownArguments,approvalShownDigest,readRunEvent} from '@judgment-pack/harness/contract'
import {decodeEvent} from './decode'
import { describe, expect, it } from 'vitest'
import { readExecution } from '@judgment-pack/harness/execution/state'
import { AIFailureError } from '@judgment-pack/harness/failures'
import pending from './fixtures/pending.json'
import answer from './fixtures/continuation-answer.json'
import terminal from './fixtures/terminal.json'
import checkpoint from './fixtures/checkpoint.json'
import { decodeDiscovery, decodeSnapshot, parseRunJSON, reference } from './decode'

const binding = {owner:{kind:'chat' as const,id:'chat-one',revision:'a'.repeat(64)},requestId:'message-one'}
const retainedAnswerSnapshot=(question:Record<string,unknown>=structuredClone(answer.events[0].event))=>({
 ...answer,id:'arun_'+'2'.repeat(32),admissionId:'answer-successor',events:[],cursor:`${answer.revision}:0`,continuations:undefined,
 answerHistory:[{parentRunId:answer.id,admissionId:answer.admissionId,actionId:answer.continuations[0].id,question,answer:structuredClone(answer.continuations[0].answer)}]
})
describe('Desk run envelope',()=>{
  it('reads handwritten pending, completed bytes, canonical checkpoint and historical pause',()=>{
    expect(decodeSnapshot(pending,binding).operations).toEqual([])
    expect(decodeSnapshot(terminal,binding).operations[0].result).toBe('eyJudWxsIjpudWxsfQ==')
    expect(decodeSnapshot({...pending,checkpoint},binding).checkpoint?.requestKey).toBe('b'.repeat(64))
    expect(decodeSnapshot({...pending,state:'awaiting-input',historical:true},binding).historical).toBe(true)
    expect(reference(decodeSnapshot(terminal,binding))).toEqual({...binding,id:terminal.id})
  })
  it.each(Object.keys(pending))('rejects omission of required %s',key=>{
    const value: Record<string,unknown> = {...pending}; delete value[key]
    expect(()=>decodeSnapshot(value,binding)).toThrow(AIFailureError)
  })
  it.each([
    {...pending,id:'arun_'+'2'.repeat(32)}, {...pending,requestId:'other'}, {...pending,owner:{...pending.owner,id:'other'}},
    {...pending,owner:{...pending.owner,revision:'b'.repeat(64)}}, {...pending,cursor:'1:1'}, {...pending,revision:2},
    {...pending,events:[{id:'unknown-event',event:{type:'invented'}}],cursor:'1:1'}, {...pending,checkpoint:{}}, {...pending,checkpoint:null}, {...pending,state:'invented'}, {...pending,operations:[{}]},
    {...terminal,events:[terminal.events[0],terminal.events[0]]}, {...pending,events:[{id:'event-one',event:{type:'execution_checkpoint'}}]},
    {...pending,revision:9007199254740992,cursor:'9007199254740992:0'}, {...pending,unexpected:true}
  ])('refuses wrong binding, shape, identity and precision',value=>{
    expect(()=>decodeSnapshot(value,{...binding,id:pending.id})).toThrow(AIFailureError)
  })
  it.each([
    ['array run state', {...pending,state:['running']}],
    ['nested array run state', {...pending,state:[['running']]}],
    ['array operation kind', {...terminal,operations:[{...terminal.operations[0],kind:['model']}]}],
    ['nested array operation kind', {...terminal,operations:[{...terminal.operations[0],kind:[['model']]}]}],
    ['array operation state', {...terminal,state:'running',operations:[{...terminal.operations[0],state:['dispatched'],result:null}]}],
    ['nested array operation state', {...terminal,state:'running',operations:[{...terminal.operations[0],state:[['dispatched']],result:null}]}],
    ['array projection issue', {...pending,projectionIssue:['pending-question']}],
    ['nested array projection issue', {...pending,projectionIssue:[['pending-question']]}]
  ])('rejects non-string envelope enum: %s',(_name,value)=>{
    expect(()=>decodeSnapshot(value,binding)).toThrow(AIFailureError)
  })
  it('rejects duplicate JSON keys, malformed JSON and lone surrogate',()=>{
    for (const raw of ['{"id":1,"id":2}','{','{"text":"\\ud800"}']) expect(()=>parseRunJSON(raw)).toThrow(AIFailureError)
  })
})

it('reads a closed server-authored approval reference and refuses missing, changed or stale associations',async()=>{
 const {default:approval}=await import('./fixtures/approval.json')
 expect(decodeSnapshot(approval,binding).approvalReference?.callId).toBe('call-one')
 for(const approvalReference of [undefined,null,{},[],{...approval.approvalReference,callId:'other'},{...approval.approvalReference,argsDigest:'bad'},{...approval.approvalReference,grant:true}])expect(()=>decodeSnapshot({...approval,approvalReference},binding)).toThrow()
 for(const state of ['running','complete','awaiting-client'])expect(()=>decodeSnapshot({...approval,state},binding)).toThrow()
 expect(()=>decodeSnapshot({...approval,checkpoint:{...approval.checkpoint,approval:{...approval.checkpoint.approval,state:'approved'}}},binding)).toThrow()
 for(const admissionId of [undefined,'','a'.repeat(129),['admission-one']])expect(()=>decodeSnapshot({...pending,admissionId},binding)).toThrow()
})
it('reads latest completed pack checks as bounded closed evidence, including invalid findings',async()=>{
 const {default:vector}=await import('./fixtures/pack-checks.json')
 expect(decodeSnapshot(vector,binding).packChecks?.[0].valid).toBe(false)
 for(const packChecks of [null,[],[{}],[{...vector.packChecks[0],authority:true}],[...vector.packChecks,...vector.packChecks],Array.from({length:65},()=>vector.packChecks[0])])expect(()=>decodeSnapshot({...vector,packChecks},binding)).toThrow()
 for(const key of Object.keys(vector.packChecks[0])){const check:Record<string,unknown>={...vector.packChecks[0]};delete check[key];expect(()=>decodeSnapshot({...vector,packChecks:[check]},binding)).toThrow()}
})

it('reads the handwritten Plan transition and rejects missing, ambiguous, stale or non-executable references',async()=>{
 const {default:plan}=await import('./fixtures/plan-decision.json')
 expect(decodeSnapshot(plan,binding).planDecisionReference).toEqual(plan.planDecisionReference)
 for(const planDecisionReference of [undefined,null,{},[],{...plan.planDecisionReference,CallId:'call'},{...plan.planDecisionReference,callId:'other'},{...plan.planDecisionReference,planRevision:'sha256:'+'b'.repeat(64)},{...plan.planDecisionReference,candidateRevision:'sha256:'+'c'.repeat(64)},{...plan.planDecisionReference,candidateRevision:'c'.repeat(64)},{...plan.planDecisionReference,argsDigest:'a'.repeat(64)}])expect(()=>decodeSnapshot({...plan,planDecisionReference},binding)).toThrow()
 for(const state of ['running','complete','paused-budget','awaiting-client'])expect(()=>decodeSnapshot({...plan,state},binding)).toThrow()
 expect(()=>decodeSnapshot({...plan,historical:true},binding)).toThrow()
 expect(decodeSnapshot({...plan,historical:true,planDecisionReference:undefined},binding).historical).toBe(true)
 expect(()=>decodeSnapshot({...plan,approvalReference:{callId:'other',argsDigest:'a'.repeat(64)}},binding)).toThrow()
 expect(()=>decodeSnapshot({...plan,checkpoint:{...plan.checkpoint,status:'running'}},binding)).toThrow()
})

// Keep these mutations paired with TestAssistantRunPlanDecisionSharedFixture.
it('validates the shared handwritten Plan checkpoint with the published reader and refuses corrupt continuity',async()=>{
 const {default:plan}=await import('./fixtures/plan-decision.json')
 expect(readExecution(plan.checkpoint)?.planState?.pending?.callId).toBe(plan.planDecisionReference.callId)
 const mutations=[
  (cp:typeof plan.checkpoint)=>{cp.version=1},
  (cp:typeof plan.checkpoint)=>{cp.planState.accepted.revision='sha256:'+'a'.repeat(64)},
  (cp:typeof plan.checkpoint)=>{cp.planState.pending.revision='sha256:'+'a'.repeat(64)},
  (cp:typeof plan.checkpoint)=>{cp.planState.pending.candidate.planState.history[0].previous='sha256:'+'a'.repeat(64)},
  (cp:typeof plan.checkpoint)=>{cp.planState.pending.candidate.planState.departuresDiscarded=1},
  (cp:typeof plan.checkpoint)=>{cp.planState.pending.candidate.planState.nextId=4},
  (cp:typeof plan.checkpoint)=>{Object.assign(cp.planState.pending,{unknown:true})},
 ]
 for(const mutate of mutations){const cp=structuredClone(plan.checkpoint);mutate(cp);expect(()=>readExecution(cp)).toThrow();expect(()=>decodeSnapshot({...plan,checkpoint:cp},binding)).toThrow()}
})

it('binds a finite physical control to exact source mapping, request and host operation',async()=>{
 const {sha256}=await import('@judgment-pack/harness/records')
 const args={query:'public policy'},sourceBinding={ownTool:'search_sources',wireName:'desk-search__search_sources'}
 const encode=(v:unknown)=>btoa(JSON.stringify(v))
 const control={version:1,kind:'research-choice',hostOperationId:'host-one',sourceBindingDigest:sha256(JSON.stringify(sourceBinding)),requestId:binding.requestId,query:args.query,reason:'choice-required'}
 const op={id:'host-one',callId:'host-one',kind:'host',name:'search_sources',state:'refused',intent:encode({callId:'source-call',root:false,arguments:encode(args),sourceBinding}),result:encode({result:encode({isError:true}),externalControl:control,externalFailure:encode({version:1,code:'permission',source:'desk',outcome:'not-started'})})}
 const call={callId:'source-call',hostOperationId:op.id,ownTool:'search_sources',wireName:sourceBinding.wireName,outcome:'not-started',control}
 const vector={...pending,state:'stopped',operations:[op],externalCalls:[call]}
 expect(decodeSnapshot(vector,binding).externalCalls?.[0].control?.kind).toBe('research-choice')
 for(const row of [{...call,callId:'foreign'},{...call,hostOperationId:'foreign'},{...call,ownTool:'read_link'},{...call,ownTool:['search_sources']},{...call,wireName:'forged__search_sources'},{...call,outcome:'unknown'},{...call,outcome:['not-started']},{...call,control:{...control,requestId:'foreign'}},{...call,control:{...control,sourceBindingDigest:'b'.repeat(64)}},{...call,control:{...control,query:'different'}},{...call,control:{...control,receipt:'fake'}},{...call,extra:true}])expect(()=>decodeSnapshot({...vector,externalCalls:[row]},binding)).toThrow()
 for(const externalCalls of [null,[],[call,call]])expect(()=>decodeSnapshot({...vector,externalCalls},binding)).toThrow()
 const unknown={...call,outcome:'unknown',control:undefined}
 expect(decodeSnapshot({...vector,operations:[{...op,result:encode({result:encode({isError:true}),externalFailure:encode({version:1,code:'storage',source:'tool',outcome:'unknown'})})}],externalCalls:[unknown]},binding).externalCalls?.[0].outcome).toBe('unknown')
 expect(()=>decodeSnapshot({...vector,operations:[{...op,state:'completed'}]},binding)).toThrow()
})

// The principal and request inventories stay closed by the canonical readers.
it.each(approvalRecords)('reads checkpoint $checkpoint.version and event 3 with their exact kept text',({checkpoint,event})=>{
 const snapshot={...approval,checkpoint,approvalReference:{callId:checkpoint.approval.callId,argsDigest:approvalShownDigest(checkpoint.approval)}}
 expect(decodeSnapshot(snapshot,binding).checkpoint?.approval?.request).toBe('{"id":9007199254740993}')
 expect(approvalShownArguments(decodeSnapshot(snapshot,binding).checkpoint!.approval!)).toBe(event.request)
 expect(decodeEvent({id:'kept-event',event}).event.type).toBe('approval_requested')
 expect(approvalShownArguments(event)).toBe(event.request)
 for(const version of [3,4,7])expect(()=>decodeSnapshot({...snapshot,checkpoint:{...checkpoint,version}},binding)).toThrow()
 const withoutRequest={...checkpoint.approval};delete (withoutRequest as {request?:string}).request
 expect(()=>decodeSnapshot({...snapshot,checkpoint:{...checkpoint,approval:withoutRequest}},binding)).toThrow()
 expect(()=>decodeEvent({id:'kept-event',event:{...event,version:2}})).toThrow()
 expect(()=>decodeSnapshot({...snapshot,approvalReference:{...snapshot.approvalReference,argsDigest:'a'.repeat(64)}},binding)).toThrow()
})

it('reads exact retained answer question version 1 without changing its metadata',()=>{
 const successor=retainedAnswerSnapshot({...answer.events[0].event,version:1}),before=structuredClone(successor)
 const snapshot=decodeSnapshot(successor,binding)
 expect(snapshot.answerHistory?.[0].question).toEqual(successor.answerHistory[0].question)
 expect(snapshot.answerHistory?.[0].question.version).toBe(1)
 const parent={...answer,continuations:answer.continuations.map(receipt=>({...receipt,successorRunId:successor.id}))}
 expect(decodeDiscovery({runs:[parent,successor]},binding)[1].answerHistory).toEqual(snapshot.answerHistory)
 expect(successor).toEqual(before)
})
it('reads historical retained answer question without adding a version',()=>{
 const successor=retainedAnswerSnapshot(),before=structuredClone(successor)
 const snapshot=decodeSnapshot(successor,binding)
 expect(snapshot.answerHistory?.[0].question).toEqual(successor.answerHistory[0].question)
 expect(Object.hasOwn(snapshot.answerHistory![0].question,'version')).toBe(false)
 expect(successor).toEqual(before)
})
it('refuses malformed or unsupported retained question versions',()=>{
 for(const version of [undefined,null,0,2,3,99,'1',true,[],{}]){
  expect(()=>decodeSnapshot(retainedAnswerSnapshot({...answer.events[0].event,version}),binding)).toThrow()
 }
})
it('refuses unknown retained question members, wrong event type and changed answer evidence',()=>{
 for(const question of [
  {...answer.events[0].event,authority:true},
  {...answer.events[0].event,version:1,authority:true},
  {...answer.events[0].event,type:'message'},
  {...answer.events[0].event,version:1,question:[]},
 ])expect(()=>decodeSnapshot(retainedAnswerSnapshot(question),binding)).toThrow()
 for(const edit of [
  (v:ReturnType<typeof retainedAnswerSnapshot>)=>{Object.assign(v.answerHistory[0],{authority:true})},
  (v:ReturnType<typeof retainedAnswerSnapshot>)=>{v.answerHistory[0].parentRunId=v.id},
  (v:ReturnType<typeof retainedAnswerSnapshot>)=>{v.answerHistory[0].admissionId=''},
  (v:ReturnType<typeof retainedAnswerSnapshot>)=>{v.answerHistory[0].actionId=''},
  (v:ReturnType<typeof retainedAnswerSnapshot>)=>{v.answerHistory[0].answer.questionDigest='a'.repeat(64)},
  (v:ReturnType<typeof retainedAnswerSnapshot>)=>{v.answerHistory[0].answer.selected=['missing']},
 ]){const successor=retainedAnswerSnapshot({...answer.events[0].event,version:1});edit(successor);expect(()=>decodeSnapshot(successor,binding)).toThrow()}
})

// A type the canonical reader accepts is still not a semantic event: progress
// and checkpoint frames never reach the retained event list.
it('refuses canonical events that are not semantic run events',()=>{
 const progress={type:'message_progress',text:'partial answer'}
 expect(()=>readRunEvent(progress)).not.toThrow()
 expect(()=>decodeEvent({id:'progress-event',event:progress})).toThrow()
 expect(decodeEvent({id:'message-event',event:{type:'message',text:'partial answer'}}).event.type).toBe('message')
})
