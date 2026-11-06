import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {giveThisPageASessionForTesting} from '../../mcp/session'
import {decodeSnapshot,pendingQuestion} from './decode'
import {presentRun} from './presentation'
import {INITIAL_STATE} from '../../research/initialState'
import {SavedChatRun} from './controller'
import {serverRunClient} from './client'
import type {Chat} from '../../chat/store'
import parent from './fixtures/answer-parent.json'
import successor from './fixtures/answer-successor.json'
const binding={owner:{...parent.owner,kind:'chat' as const},requestId:parent.requestId}
const json=(value:unknown)=>new Response(JSON.stringify(value),{headers:{'Content-Type':'application/json'}})
const chat:Chat={id:'chat-one',title:'Conversation',pinned:false,archived:false,updatedAt:'2026-10-20T00:00:00Z',composer:'',model:'model-one',mode:'draft',view:'chat',serverRequest:{requestId:parent.requestId,admissionId:parent.admissionId}}
beforeEach(()=>giveThisPageASessionForTesting('successor-session'))
afterEach(()=>{vi.restoreAllMocks();vi.unstubAllGlobals()})
it('reads linked history as user data beside the fresh execution allowance',()=>{
 const p=decodeSnapshot(parent,binding),s=decodeSnapshot(successor,binding)
 expect(p.continuations?.[0]).toMatchObject({successorRunId:s.id})
 expect(pendingQuestion(s)).toBeUndefined()
 const display=presentRun(INITIAL_STATE,s)
 expect(display.questions?.[0].resolution).toMatchObject({kind:'answered',continued:true})
 expect(display.turns.find(t=>t.role==='user')?.text).toBe('Two\nExact answer <&> 한글\nnext line')
 expect(display.execution).toMatchObject({id:'execution-two',used:1,limit:20,grants:[]})
})
for(const key of Object.keys(successor.answerHistory[0]))it(`refuses omitted history ${key}`,()=>{
 const value=structuredClone(successor) as unknown as {answerHistory:Record<string,unknown>[]}
 delete value.answerHistory[0][key];expect(()=>decodeSnapshot(value,binding)).toThrow()
})
it('refuses missing or malformed retained question discriminators',()=>{
 for(const type of [undefined,null,42,'','message']){
  const value=structuredClone(successor) as unknown as {answerHistory:{question:Record<string,unknown>}[]}
  if(type===undefined)delete value.answerHistory[0].question.type
  else value.answerHistory[0].question.type=type
  expect(()=>decodeSnapshot(value,binding)).toThrow()
 }
})
it('refuses invalid, duplicated or authority-bearing history and successor links',()=>{
 for(const mutate of [
  (v:typeof successor)=>{v.answerHistory[0].parentRunId=v.id},
  (v:typeof successor)=>{v.answerHistory[0].answer.questionDigest='a'.repeat(64)},
  (v:typeof successor)=>{v.answerHistory.push(v.answerHistory[0])},
  (v:typeof successor)=>{Object.assign(v.answerHistory[0],{approved:true})},
  (v:typeof successor)=>{Object.assign(v.answerHistory[0].answer,{grant:{from:20,to:30}})},
 ]){const v=structuredClone(successor);mutate(v);expect(()=>decodeSnapshot(v,binding)).toThrow()}
 const p=structuredClone(parent);p.continuations[0].successorRunId='other';expect(()=>decodeSnapshot(p,binding)).toThrow()
})
it('reload follows only the saved successor link without POST or new preparation',async()=>{
 const prepare=vi.fn(),reconcile=vi.fn(async()=>chat),pin=vi.fn()
 const run=new SavedChatRun(chat.id,chat,{project:'/project',client:serverRunClient,prepare,reconcile,pin,problem:vi.fn()})
 const fetch=vi.fn().mockResolvedValueOnce(json({runs:[parent,successor]})).mockResolvedValueOnce(json(successor));vi.stubGlobal('fetch',fetch)
 await run.reconnect()
 expect(run.getSnapshot().execution?.id).toBe('execution-two')
 expect(run.getSnapshot().questions?.[0].resolution?.kind).toBe('answered')
 expect(prepare).not.toHaveBeenCalled();expect(fetch).toHaveBeenCalledTimes(2)
 expect(fetch.mock.calls[1][0]).toBe(`/api/assistant-runs/${successor.id}`)
 expect(fetch.mock.calls.every(([,options])=>options.method!=='POST')).toBe(true)
})
it('reload refuses a linked child with a different retained parent answer',async()=>{
 const prepare=vi.fn(),run=new SavedChatRun(chat.id,chat,{project:'/project',client:serverRunClient,prepare,reconcile:vi.fn(async()=>chat),pin:vi.fn(),problem:vi.fn()})
 const changed=structuredClone(successor);changed.answerHistory[0].answer.text='A different answer'
 const fetch=vi.fn().mockResolvedValueOnce(json({runs:[parent,changed]})).mockResolvedValueOnce(json(changed));vi.stubGlobal('fetch',fetch)
 await run.reconnect();expect(run.getSnapshot().status).toBe('failed');expect(prepare).not.toHaveBeenCalled();expect(fetch).toHaveBeenCalledTimes(2)
})
