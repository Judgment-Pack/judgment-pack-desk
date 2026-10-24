import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { sha256 } from '@judgment-pack/harness/records'
import { INITIAL_STATE } from '../../research/initialState'
import { giveThisPageASessionForTesting, forgetSession } from '../../mcp/session'
import type { RunState } from '../../research/run'
import type { Chat } from '../../chat/store'
import { SavedChatRun } from './controller'
import { serverRunClient } from './client'
import pending from './fixtures/pending.json'
import answer from './fixtures/continuation-answer.json'
import budget from './fixtures/continuation-budget.json'

const chat:Chat={id:'chat-one',title:'Conversation',pinned:false,archived:false,updatedAt:'2026-10-20T00:00:00Z',composer:'',model:'model-one',mode:'draft',view:'chat'}
const json=(v:unknown)=>new Response(JSON.stringify(v),{headers:{'Content-Type':'application/json'}})
const snapshot=(requestId='00000000-0000-4000-8000-000000000001',patch:Record<string,unknown>={})=>({...pending,admissionId:requestId,requestId,...patch})
function setup(saved:Chat=chat,draftScope?:()=>Promise<string>){
 const prepare=vi.fn(async(_state:RunState,_requestId:string,_admissionId:string,_signal:AbortSignal)=> 'a'.repeat(64)),reconcile=vi.fn(async():Promise<Chat|undefined>=>saved),pin=vi.fn(),problem=vi.fn(),notSent=vi.fn()
 const run=new SavedChatRun(saved.id,saved,{project:'/project',draftScope,client:serverRunClient,prepare,reconcile,pin,problem,notSent})
 return {run,prepare,reconcile,pin,notSent}
}
beforeEach(()=>giveThisPageASessionForTesting('controller-session'))
afterEach(()=>{vi.restoreAllMocks();vi.unstubAllGlobals();sessionStorage.clear()})
it('persists original turn, settings operands and stable admission before POST; a lost ack discovers that admission once',async()=>{
 vi.spyOn(crypto,'randomUUID').mockReturnValue('00000000-0000-4000-8000-000000000001')
 const h=setup(),fetch=vi.fn().mockRejectedValueOnce(Error('lost POST ack')).mockResolvedValueOnce(json({runs:[snapshot('00000000-0000-4000-8000-000000000001',{state:'failed'})]}));vi.stubGlobal('fetch',fetch)
 const files=[{id:'selected',name:'source.txt',text:'Original attachment'}],basis={packId:'one',path:'one.json',text:'{ "missing":null,"enabled":false }'}
 h.run.send('Prepared input','Original message',false,files,'Original statement','provided',basis)
 await vi.waitFor(()=>expect(h.run.getSnapshot().status).toBe('failed'))
 expect(h.prepare).toHaveBeenCalledOnce();expect(h.prepare.mock.calls[0]).toMatchObject([{turns:[{id:'00000000-0000-4000-8000-000000000001',text:'Original message',input:'Prepared input',statement:'Original statement',attachments:files,researchChoice:'provided',packContext:basis}]},'00000000-0000-4000-8000-000000000001','00000000-0000-4000-8000-000000000001',expect.any(AbortSignal)])
 expect(fetch).toHaveBeenCalledTimes(2);expect(fetch.mock.calls[0][0]).toBe('/api/assistant-runs')
 expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({admissionId:'00000000-0000-4000-8000-000000000001',purpose:'chat',ownerId:chat.id,revision:'a'.repeat(64),requestId:'00000000-0000-4000-8000-000000000001'})
 expect(fetch.mock.calls[1][0]).toContain('/owners/chat-one/requests/00000000-0000-4000-8000-000000000001');expect(h.run.canRetryResponse).toBe(false)
 h.run.retryResponse();expect(fetch).toHaveBeenCalledTimes(2)
})
it('never adopts another admission or ambiguous discovery and never starts on reconnect',async()=>{
 const h=setup({...chat,serverRequest:{requestId:'message-one',admissionId:'expected'}})
 const fetch=vi.fn().mockResolvedValue(json({runs:[{...pending,admissionId:'other'}]}));vi.stubGlobal('fetch',fetch)
 await h.run.reconnect();expect(h.run.getSnapshot().status).toBe('failed');expect(h.run.canRetryResponse).toBe(false)
 expect(fetch).toHaveBeenCalledOnce();expect(fetch.mock.calls[0][1].method).not.toBe('POST')
 fetch.mockResolvedValue(json({runs:[{...pending,admissionId:'expected'},{...pending,id:'arun_'+'2'.repeat(32),admissionId:'expected'}]}))
 await h.run.reconnect();expect(h.run.getSnapshot().status).toBe('failed');expect(fetch).toHaveBeenCalledTimes(2)
})
it('route disconnect aborts reads without cancelling; late acknowledgement cannot update the disposed owner',async()=>{
 let resolve!:(response:Response)=>void
 const fetch=vi.fn().mockImplementation(()=>new Promise<Response>(r=>{resolve=r}));vi.stubGlobal('fetch',fetch)
 const h=setup({...chat,serverRequest:{requestId:pending.requestId,admissionId:pending.admissionId}}),before=h.run.getSnapshot()
 const read=h.run.reconnect();await vi.waitFor(()=>expect(fetch).toHaveBeenCalledOnce())
 h.run.disconnect();resolve(json({runs:[{...pending,state:'failed'}]}));await read
 expect(h.run.getSnapshot()).toBe(before);expect(fetch).toHaveBeenCalledOnce()
})
it('explicit Stop during a lost start acknowledgement cancels the discovered original once',async()=>{
 vi.spyOn(crypto,'randomUUID').mockReturnValueOnce('00000000-0000-4000-8000-000000000001').mockReturnValue('00000000-0000-4000-8000-000000000002')
 let release!:()=>void
 const fetch=vi.fn().mockImplementationOnce(()=>new Promise((_r,reject)=>{release=()=>reject(Error('lost ack'))})).mockResolvedValueOnce(json({runs:[snapshot()]})).mockResolvedValueOnce(json(snapshot('00000000-0000-4000-8000-000000000001',{state:'stopped',revision:2,cursor:'2:0'})))
 vi.stubGlobal('fetch',fetch);const h=setup();h.run.send('original')
 await vi.waitFor(()=>expect(fetch).toHaveBeenCalledOnce());h.run.stop();h.run.stop();release()
 await vi.waitFor(()=>expect(h.run.getSnapshot().status).toBe('stopped'))
 expect(fetch).toHaveBeenCalledTimes(3);const action=JSON.parse(fetch.mock.calls[2][1].body)
 expect(action).toEqual({id:'00000000-0000-4000-8000-000000000002',kind:'cancel',revision:1});expect(fetch.mock.calls.filter(([url])=>url==='/api/assistant-runs')).toHaveLength(1)
})
it('submits exact current question digest and answer without browser checkpointing or a new start',async()=>{
 const paused={...answer,state:'awaiting-input',continuations:undefined},h=setup({...chat,serverRequest:{requestId:answer.requestId,admissionId:answer.admissionId}})
 const fetch=vi.fn().mockResolvedValueOnce(json({runs:[paused]})).mockResolvedValueOnce(json(answer));vi.stubGlobal('fetch',fetch)
 await h.run.reconnect();const question=h.run.getSnapshot().questions![0]
 h.run.updateQuestionDraft(question.id,{selected:['one'],text:'Retained answer'});expect(h.run.prepareQuestionAnswer(question.id,'answered')).toBe(true)
 expect(h.run.continueQuestion(question.id)).toBe(true)
 await vi.waitFor(()=>expect(fetch).toHaveBeenCalledTimes(2))
 expect(JSON.parse(fetch.mock.calls[1][1].body)).toMatchObject({kind:'answer',revision:answer.revision,questionId:question.id,answer:{selected:['one'],text:'Retained answer'}})
 expect(h.prepare).not.toHaveBeenCalled();expect(fetch.mock.calls.every(([url])=>url!=='/api/assistant-runs')).toBe(true)
})
it('budget continuation uses explicit current action; terminal ack uncertainty never permits implicit retry',async()=>{
 const paused={...budget,state:'paused-budget'},h=setup({...chat,serverRequest:{requestId:budget.requestId,admissionId:budget.admissionId}})
 const fetch=vi.fn().mockResolvedValueOnce(json({runs:[paused]})).mockResolvedValueOnce(json({...budget,state:'complete'}));vi.stubGlobal('fetch',fetch)
 await h.run.reconnect();expect(h.run.prepareBudgetContinuation()).toBe(true);h.run.continueExecution()
 await vi.waitFor(()=>expect(fetch).toHaveBeenCalledTimes(2));expect(JSON.parse(fetch.mock.calls[1][1].body)).toMatchObject({kind:'extend-budget',revision:budget.revision})
 await vi.waitFor(()=>expect(h.run.getSnapshot().status).toBe('complete'));expect(h.run.canRetryResponse).toBe(false);expect(h.prepare).not.toHaveBeenCalled()
})
it('a historical constructor performs no read or model/tool dispatch',()=>{
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch);const h=setup({...chat,checkpoint:{state:{...INITIAL_STATE,turns:[{id:'old',role:'assistant',kind:'message',text:'Historical answer',at:chat.updatedAt}]},sources:[]}})
 expect(h.run.getSnapshot().turns[0].text).toBe('Historical answer');expect(fetch).not.toHaveBeenCalled()
})
const produced=process.env.DESK_RUN_CLIENT_OUTPUTS
it.skipIf(!produced)('consumes actual Go API/native continuations through authenticated controller readers without new admission',async()=>{
 for(const name of ['actual-continuation-answer.json','actual-continuation-extend-budget.json','actual-continuation-subscription.json']){
  const observed=JSON.parse(readFileSync(`${produced}/${name}`,'utf8'))
  const h=setup({...chat,serverRequest:{requestId:observed.requestId,admissionId:observed.admissionId}})
  const fetch=vi.fn().mockResolvedValue(json({runs:[observed]}));vi.stubGlobal('fetch',fetch)
  await h.run.reconnect();expect(h.run.getSnapshot().execution?.used).toBe(observed.checkpoint.used)
  expect(h.run.getSnapshot().turns.some(t=>t.role==='assistant')).toBe(true);expect(h.prepare).not.toHaveBeenCalled();expect(fetch).toHaveBeenCalledOnce()
 }
})
it.skipIf(!produced)('consumes actual Go snapshots/SSE and isolates child progress; session end clears display and prevents late access',async()=>{
 const observed=JSON.parse(readFileSync(`${produced}/pending.json`,'utf8')),wire=readFileSync(`${produced}/events.sse`,'utf8')
 const h=setup({...chat,serverRequest:{requestId:observed.requestId,admissionId:observed.admissionId}})
 const fetch=vi.fn().mockResolvedValueOnce(json({runs:[observed]})).mockResolvedValueOnce(new Response(wire,{headers:{'Content-Type':'text/event-stream'}}));vi.stubGlobal('fetch',fetch)
 await h.run.reconnect();expect(h.run.getSnapshot().turns.some(t=>t.text==='Ready 한글')).toBe(true)
 const {whenSessionEnds}=await import('../../mcp/session'),end=whenSessionEnds(h.run.endSession)
 forgetSession();expect(h.run.getSnapshot().turns).toEqual([])
 await h.run.reconnect();expect(fetch).toHaveBeenCalledTimes(2);end()
})
it('accepts a terminal projection only after authenticated acknowledgement and preserves history on read failure',async()=>{
 const responseId=`response-${sha256(pending.id).slice(0,32)}`,h=setup({...chat,serverRequest:{requestId:pending.requestId,admissionId:pending.admissionId}})
 const terminal={...pending,state:'complete',chatProjection:{responseId,digest:'b'.repeat(64),acknowledged:true}}
 const fetch=vi.fn().mockResolvedValue(json({runs:[terminal]}));vi.stubGlobal('fetch',fetch)
 h.reconcile.mockRejectedValueOnce(Error('failed private read'));await h.run.reconnect()
 expect(h.run.canRetryResponse).toBe(false);expect(h.run.getSnapshot().status).toBe('failed');expect(h.prepare).not.toHaveBeenCalled()
})
it('uses the retained server approval reference with the current revision and never reconstructs arguments',async()=>{
 const {default:approval}=await import('./fixtures/approval.json'),h=setup({...chat,serverRequest:{requestId:approval.requestId,admissionId:approval.admissionId}})
 const fetch=vi.fn().mockResolvedValueOnce(json({runs:[approval]})).mockResolvedValueOnce(json({...pending,state:'stopped'}));vi.stubGlobal('fetch',fetch)
 await h.run.reconnect();expect(h.run.pendingApproval).toMatchObject(approval.approvalReference)
 h.run.decideApproval(false)
 await vi.waitFor(()=>expect(fetch).toHaveBeenCalledTimes(2))
 expect(JSON.parse(fetch.mock.calls[1][1].body)).toMatchObject({kind:'approval',revision:approval.revision,...approval.approvalReference,approved:false})
 expect(h.prepare).not.toHaveBeenCalled()
})
it.skipIf(!produced)('refresh retains the latest mismatch and pass for the exact candidate without granting Save or mutating its basis',async()=>{
 const snapshot=JSON.parse(readFileSync(`${produced}/saved-pack-snapshot.json`,'utf8')),document=JSON.parse(readFileSync(`${produced}/saved-pack-chat.json`,'utf8'))
 const {decodeCheckpoint}=await import('../../chat/checkpoint'),saved={...document.chats[0],checkpoint:decodeCheckpoint(document.chats[0].checkpoint),serverRequest:{requestId:snapshot.requestId,admissionId:snapshot.admissionId}}
 const h=setup(saved),fetch=vi.fn();vi.stubGlobal('fetch',fetch)
 const original=saved.checkpoint.state.candidates.at(-1)!,basis=original.packReview!.basis,check=original.packReview!.check!
 fetch.mockResolvedValue(json({runs:[{...snapshot,packChecks:[{...check,valid:false,diagnostics:['Latest failed check']}]}]}))
 await h.run.reconnect();expect(h.run.getSnapshot().candidates.at(-1)?.packReview).toMatchObject({basis,check:{valid:false,diagnostics:['Latest failed check']}})
 fetch.mockResolvedValue(json({runs:[{...snapshot,packChecks:[check]}]}));await h.run.reconnect()
 expect(h.run.getSnapshot().candidates.at(-1)?.packReview).toMatchObject({basis,check:{valid:true}})
 fetch.mockResolvedValue(json({runs:[{...snapshot,packChecks:[{...check,documentDigest:'f'.repeat(64)}]}]}));await h.run.reconnect()
 expect(h.run.getSnapshot().status).toBe('failed');expect(h.prepare).not.toHaveBeenCalled()
})
it('Stop before POST releases only the proven unadmitted marker and keeps the original turn for an explicit fresh retry',async()=>{
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch);const h=setup()
 let release!:(revision:string)=>void;h.prepare.mockImplementationOnce(()=>new Promise(r=>{release=r}))
 h.run.send('Original prepared input');await vi.waitFor(()=>expect(h.prepare).toHaveBeenCalledOnce());h.run.stop();release('a'.repeat(64))
 await vi.waitFor(()=>expect(h.run.getSnapshot().status).toBe('stopped'))
 expect(fetch).not.toHaveBeenCalled();expect(h.notSent).toHaveBeenCalledOnce();expect(h.run.getSnapshot().turns[0].input).toBe('Original prepared input');expect(h.run.canRetryResponse).toBe(true)
})
it('a failed save before POST retains the turn and permits only a fresh explicit retry',async()=>{
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch);const h=setup();h.prepare.mockRejectedValueOnce(Error('CAS save failed'))
 h.run.send('Original request');await vi.waitFor(()=>expect(h.run.getSnapshot().status).toBe('failed'))
 expect(fetch).not.toHaveBeenCalled();expect(h.notSent).toHaveBeenCalledOnce();expect(h.run.canRetryResponse).toBe(true)
 h.prepare.mockImplementationOnce(()=>new Promise(()=>{}));h.run.retryResponse()
 await vi.waitFor(()=>expect(h.prepare).toHaveBeenCalledTimes(2))
 const before=h.prepare.mock.calls[0][1],next=h.prepare.mock.calls[1][1]
 expect(next).not.toBe(before);expect(h.run.getSnapshot().turns).toHaveLength(2);h.run.disconnect()
})
it('a refused observation after admission cannot be reclassified as an unsent request',async()=>{
 vi.spyOn(crypto,'randomUUID').mockReturnValue('00000000-0000-4000-8000-000000000001')
 const fetch=vi.fn().mockResolvedValueOnce(json(snapshot('00000000-0000-4000-8000-000000000001'))).mockResolvedValueOnce(new Response('{}',{status:403}));vi.stubGlobal('fetch',fetch)
 const h=setup();h.run.send('Original request');await vi.waitFor(()=>expect(h.run.getSnapshot().status).toBe('failed'))
 expect(h.notSent).not.toHaveBeenCalled();expect(h.run.canRetryResponse).toBe(false);expect(fetch).toHaveBeenCalledTimes(2)
})
it('retains typed question drafts across refresh only for the exact authenticated session and question digest',async()=>{
 const saved={...chat,serverRequest:{requestId:answer.requestId,admissionId:answer.admissionId}},paused={...answer,state:'awaiting-input',continuations:undefined}
 const fetch=vi.fn().mockImplementation(()=>Promise.resolve(json({runs:[paused]})));vi.stubGlobal('fetch',fetch)
 const first=setup(saved,async()=> 'session-project-owner-a');await first.run.reconnect();const id=first.run.getSnapshot().questions![0].id
 first.run.updateQuestionDraft(id,{selected:['one'],text:'Keep this unfinished answer'});first.run.disconnect()
 const refresh=setup(saved,async()=> 'session-project-owner-a');await refresh.run.reconnect()
 expect(refresh.run.getSnapshot().questions![0].draft.text).toBe('Keep this unfinished answer')
 const replacement=setup(saved,async()=> 'session-project-owner-b');await replacement.run.reconnect()
 expect(replacement.run.getSnapshot().questions![0].draft.text).toBe('')
 const changed=structuredClone(paused);(changed.events[0].event.question as {question:string}).question='Changed exact question?'
 fetch.mockImplementation(()=>Promise.resolve(json({runs:[changed]})));const newer=setup(saved,async()=> 'session-project-owner-a');await newer.run.reconnect()
 expect(newer.run.getSnapshot().questions![0].draft.text).toBe('')
 refresh.run.endSession();expect(sessionStorage.length).toBe(0);expect(refresh.run.getSnapshot().turns).toEqual([])
})

const retainedChat=()=>({...chat,serverRequest:{requestId:pending.requestId,admissionId:pending.admissionId},checkpoint:{state:{...INITIAL_STATE,turns:[{id:pending.requestId,role:'user' as const,kind:'message' as const,text:'Original message',at:chat.updatedAt}]},sources:[]}})
const acknowledged=()=>({...pending,state:'complete',chatProjection:{responseId:`response-${sha256(pending.id).slice(0,32)}`,digest:'b'.repeat(64),acknowledged:true}})
const deferred=<T,>()=>{let resolve!:(value:T)=>void,reject!:(cause:unknown)=>void;const promise=new Promise<T>((r,j)=>{resolve=r;reject=j});return {promise,resolve,reject}}
function expectFreshWorkBlocked(h:ReturnType<typeof setup>){
 const before=h.run.getSnapshot()
 expect(h.run.canRetryResponse).toBe(false)
 h.run.send('Fresh request');h.run.retryResponse()
 expect(()=>h.run.appendConversation([])).toThrow()
 expect(h.run.getSnapshot()).toBe(before);expect(h.prepare).not.toHaveBeenCalled()
}
it('blocks restored-marker work before discovery and preserves the original admission throughout deferred adoption',async()=>{
 const h=setup(retainedChat()),read=deferred<Chat|undefined>()
 const fetch=vi.fn().mockImplementation(async()=>json({runs:[acknowledged()]}));vi.stubGlobal('fetch',fetch)
 expectFreshWorkBlocked(h);expect(h.run.needsReconnect).toBe(true)
 h.reconcile.mockReturnValueOnce(read.promise);const reconnect=h.run.reconnect()
 await vi.waitFor(()=>expect(h.reconcile).toHaveBeenCalledOnce());expectFreshWorkBlocked(h)
 expect(h.run.needsReconnect).toBe(true)
 read.resolve(retainedChat());await reconnect
 expect(h.run.canRetryResponse).toBe(true);expect(h.run.needsReconnect).toBe(false)
 h.prepare.mockImplementation(()=>new Promise(()=>{}));h.run.retryResponse()
 await vi.waitFor(()=>expect(h.prepare).toHaveBeenCalledOnce());h.run.disconnect()
 expect(fetch).toHaveBeenCalledOnce()
})
it.each(['undefined','rejected'] as const)('retains original Reconnect identity after %s reconciliation and blocked fresh work',async outcome=>{
 const h=setup(retainedChat()),fetch=vi.fn().mockImplementation(async()=>json({runs:[acknowledged()]}));vi.stubGlobal('fetch',fetch)
 if(outcome==='undefined')h.reconcile.mockResolvedValueOnce(undefined)
 else h.reconcile.mockRejectedValueOnce(Error('private read failed'))
 await h.run.reconnect();expectFreshWorkBlocked(h);expect(h.run.needsReconnect).toBe(true)
 await h.run.reconnect();expect(fetch).toHaveBeenCalledTimes(2)
 expect(fetch.mock.calls.every(([url])=>String(url).endsWith(`/requests/${pending.requestId}`))).toBe(true)
 expect(h.run.canRetryResponse).toBe(true)
 h.prepare.mockImplementation(()=>new Promise(()=>{}));h.run.send('Fresh after reconciliation')
 await vi.waitFor(()=>expect(h.prepare).toHaveBeenCalledOnce());h.run.disconnect()
})
it.each(['disconnect','replacement','logout'] as const)('does not unlock fresh work from a stale terminal reconciliation after %s',async transition=>{
 const h=setup(retainedChat()),old=deferred<Chat|undefined>(),current=deferred<Chat|undefined>()
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async()=>json({runs:[acknowledged()]})))
 h.reconcile.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
 const first=h.run.reconnect();await vi.waitFor(()=>expect(h.reconcile).toHaveBeenCalledOnce())
 let second:Promise<void>|undefined
 if(transition==='replacement'){second=h.run.reconnect();await vi.waitFor(()=>expect(h.reconcile).toHaveBeenCalledTimes(2))}
 else if(transition==='logout'){h.run.endSession();expect(h.run.needsReconnect).toBe(false)}
 else h.run.disconnect()
 const before=h.run.getSnapshot();old.resolve(retainedChat());await first
 expectFreshWorkBlocked(h);expect(h.run.getSnapshot()).toBe(before)
 if(transition==='logout')expect(h.run.needsReconnect).toBe(false)
 if(second){current.resolve(retainedChat());await second;expect(h.run.canRetryResponse).toBe(true)}
})
it.each(['success','undefined','rejected'] as const)('keeps observed terminal acknowledgement gated through %s saved projection reconciliation',async outcome=>{
 const h=setup(retainedChat()),read=deferred<Chat|undefined>()
 const terminal=acknowledged(),wire=`event: snapshot\nid: ${terminal.cursor}\ndata: ${JSON.stringify(terminal)}\n\n`
 const fetch=vi.fn().mockResolvedValueOnce(json({runs:[pending]})).mockResolvedValueOnce(new Response(wire,{headers:{'Content-Type':'text/event-stream'}}));vi.stubGlobal('fetch',fetch)
 h.reconcile.mockReturnValueOnce(read.promise);const reconnect=h.run.reconnect()
 await vi.waitFor(()=>expect(h.reconcile).toHaveBeenCalledOnce());expectFreshWorkBlocked(h)
 if(outcome==='rejected')read.reject(Error('private read failed'))
 else read.resolve(outcome==='undefined'?undefined:retainedChat())
 await reconnect
 if(outcome==='success')expect(h.run.canRetryResponse).toBe(true)
 else {
  expectFreshWorkBlocked(h);expect(h.run.needsReconnect).toBe(true)
  fetch.mockResolvedValueOnce(json({runs:[terminal]}));await h.run.reconnect()
  expect(h.run.canRetryResponse).toBe(true);expect(fetch.mock.calls[2][0]).toContain(`/requests/${pending.requestId}`)
 }
})

it('gates each observed terminal reconciliation after an earlier successful projection',async()=>{
 const h=setup(retainedChat()),first=deferred<Chat|undefined>(),second=deferred<Chat|undefined>(),terminal=acknowledged()
 const frame=`event: snapshot\nid: ${terminal.cursor}\ndata: ${JSON.stringify(terminal)}\n\n`
 vi.stubGlobal('fetch',vi.fn().mockResolvedValueOnce(json({runs:[pending]})).mockResolvedValueOnce(new Response(frame+frame,{headers:{'Content-Type':'text/event-stream'}})))
 h.reconcile.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
 const reconnect=h.run.reconnect();await vi.waitFor(()=>expect(h.reconcile).toHaveBeenCalledOnce())
 first.resolve(retainedChat());await vi.waitFor(()=>expect(h.reconcile).toHaveBeenCalledTimes(2));expectFreshWorkBlocked(h)
 second.resolve(retainedChat());await reconnect;expect(h.run.canRetryResponse).toBe(true)
})
