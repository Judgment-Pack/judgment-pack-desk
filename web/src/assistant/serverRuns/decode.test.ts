import { describe, expect, it } from 'vitest'
import { AIFailureError } from '@judgment-pack/harness/failures'
import pending from './fixtures/pending.json'
import terminal from './fixtures/terminal.json'
import checkpoint from './fixtures/checkpoint.json'
import { decodeSnapshot, parseRunJSON, reference } from './decode'

const binding = {owner:{kind:'chat' as const,id:'chat-one',revision:'a'.repeat(64)},requestId:'message-one'}
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
