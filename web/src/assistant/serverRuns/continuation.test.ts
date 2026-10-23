import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import { giveThisPageASessionForTesting } from '../../mcp/session'
import { RunRequestError, serverRunClient } from './client'
import { decodeSnapshot, pendingQuestion } from './decode'
import answer from './fixtures/continuation-answer.json'
import budget from './fixtures/continuation-budget.json'
import actions from './fixtures/continuation-actions.json'
import type { RunAction } from './types'
const binding={owner:{...answer.owner,kind:'chat' as const},requestId:answer.requestId}
const paused=()=>decodeSnapshot({...answer,state:'awaiting-input',continuations:undefined},binding)
beforeEach(()=>giveThisPageASessionForTesting('test-session'))
afterEach(()=>vi.unstubAllGlobals())
it('reads handwritten answer/grant receipts and exact original question identity',()=>{
 const a=decodeSnapshot(answer,binding),b=decodeSnapshot(budget,binding)
 expect(a.continuations?.[0]).toEqual(answer.continuations[0]);expect(b.continuations?.[0]).toEqual(budget.continuations[0])
 expect(pendingQuestion(a)).toBeUndefined();expect(pendingQuestion(paused())?.digest).toBe(actions[0].questionDigest)
})
for(const [name,vector] of [['answer',answer],['budget',budget]] as const){
 for(const key of Object.keys(vector.continuations[0]))it(`refuses omitted ${name} receipt ${key}`,()=>{
  const v=structuredClone(vector) as unknown as {continuations:Record<string,unknown>[]};delete v.continuations[0][key];expect(()=>decodeSnapshot(v,binding)).toThrow()
 })
 const payload=vector.continuations[0].kind==='answer'?'answer':'grant'
 for(const key of Object.keys((vector.continuations[0] as unknown as Record<string,Record<string,unknown>>)[payload]))it(`refuses omitted ${name} ${payload}.${key}`,()=>{
  const v=structuredClone(vector) as unknown as {continuations:Record<string,Record<string,unknown>>[]};delete v.continuations[0][payload][key];expect(()=>decodeSnapshot(v,binding)).toThrow()
 })
}
it('refuses changed choices, repeated questions, unknown receipt fields and forged grant',()=>{
 for(const edit of [
  (v:typeof answer)=>{v.continuations[0].answer.selected=['missing']},
  (v:typeof answer)=>{v.continuations[0].answer.selected=['one','two']},
  (v:typeof answer)=>{v.continuations[0].answer.questionDigest='a'.repeat(64)},
  (v:typeof answer)=>{v.events.push({...v.events[0],id:'second'});v.cursor='5:2'},
  (v:typeof answer)=>{Object.assign(v.continuations[0],{approved:true})},
 ]){const v=structuredClone(answer);edit(v);expect(()=>decodeSnapshot(v,binding)).toThrow()}
 const v=structuredClone(budget);v.continuations[0].grant.to=60;expect(()=>decodeSnapshot(v,binding)).toThrow()
})
it('sends exact closed answer and budget actions; lost answer response remains uncertain without retry',async()=>{
 const fetch=vi.fn().mockResolvedValue(new Response(JSON.stringify(answer),{headers:{'Content-Type':'application/json'}}));vi.stubGlobal('fetch',fetch)
 const client=await serverRunClient()
 await client.action(paused(),actions[0] as RunAction)
 expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual(actions[0])
 const b=decodeSnapshot({...budget,state:'paused-budget'},binding)
 fetch.mockResolvedValueOnce(new Response(JSON.stringify(budget),{headers:{'Content-Type':'application/json'}}))
 await client.action(b,actions[1] as RunAction)
 expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual(actions[1])
 fetch.mockRejectedValueOnce(Error('private transport prose'))
 await expect(client.action(paused(),actions[0] as RunAction)).rejects.toMatchObject({delivery:'uncertain'})
 expect(fetch).toHaveBeenCalledTimes(3)
 expect(()=>client.action(paused(),{...actions[0],answer:{selected:['missing'],text:''}} as RunAction)).toThrow(RunRequestError)
 expect(()=>client.action(paused(),{...actions[0],approved:true} as never)).toThrow(RunRequestError)
 expect(fetch).toHaveBeenCalledTimes(3)
})

for(const action of actions)for(const key of Object.keys(action))it(`refuses omitted ${action.kind} action ${key} before POST`,async()=>{
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch);const client=await serverRunClient()
 const missing={...action} as Record<string,unknown>;delete missing[key]
 const state=action.kind==='answer'?paused():decodeSnapshot({...budget,state:'paused-budget'},binding)
 expect(()=>client.action(state,missing as RunAction)).toThrow(RunRequestError);expect(fetch).not.toHaveBeenCalled()
})
it('refuses malformed answer scalars as not-sent without changing text',async()=>{
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch);const client=await serverRunClient()
 for(const text of ['\ud800','\udc00','\ud800x','\0']){
  expect(()=>client.action(paused(),{...actions[0],answer:{selected:[],text}} as RunAction)).toThrow(RunRequestError)
 }
 expect(fetch).not.toHaveBeenCalled()
})
