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
  it('rejects duplicate JSON keys, malformed JSON and lone surrogate',()=>{
    for (const raw of ['{"id":1,"id":2}','{','{"text":"\\ud800"}']) expect(()=>parseRunJSON(raw)).toThrow(AIFailureError)
  })
})
