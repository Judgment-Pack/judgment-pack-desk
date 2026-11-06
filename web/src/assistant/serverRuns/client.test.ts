import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { forgetSession, giveThisPageASessionForTesting } from '../../mcp/session'
import { RunRequestError, serverRunClient } from './client'
import { decodeSnapshot } from './decode'
import { RunObservations } from './stream'
import pending from './fixtures/pending.json'
import terminal from './fixtures/terminal.json'
import plan from './fixtures/plan-decision.json'
import approval from './fixtures/approval.json'
import type { RunAction, RunSnapshot } from './types'

const binding={owner:{...pending.owner,kind:'chat' as const},requestId:pending.requestId}
const ref={...binding,id:pending.id}
const start={admissionId:'admission-one',purpose:'chat' as const,ownerId:binding.owner.id,revision:binding.owner.revision,requestId:binding.requestId}
const snapshot=()=>decodeSnapshot(pending,binding)
const json=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{'Content-Type':'application/json'}})
beforeEach(()=>giveThisPageASessionForTesting('test-session'))
afterEach(()=>vi.unstubAllGlobals())
describe('authenticated explicit durable run client',()=>{
 it('uses exact closed bodies, fixed paths and authenticated desk transport',async()=>{
  const fetch=vi.fn().mockImplementation(()=>Promise.resolve(json(pending)));vi.stubGlobal('fetch',fetch)
  const client=await serverRunClient()
  await client.start(start)
  expect(fetch.mock.calls[0][0]).toBe('/api/assistant-runs')
  expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual(start)
  expect(fetch.mock.calls[0][1]).toMatchObject({credentials:'omit',redirect:'error',headers:{Authorization:'Bearer test-session'}})
  await client.action(snapshot(),{id:'cancel-one',kind:'cancel',revision:1})
  expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual({id:'cancel-one',kind:'cancel',revision:1})
  await client.action(decodeSnapshot(approval,binding),{id:'approval-one',kind:'approval',revision:1,...approval.approvalReference,approved:false,note:''})
  expect(JSON.parse(fetch.mock.calls[2][1].body)).toEqual({id:'approval-one',kind:'approval',revision:1,...approval.approvalReference,approved:false,note:''})
  await client.action(snapshot(),{id:'abandon-one',kind:'abandon',revision:1})
  expect(JSON.parse(fetch.mock.calls[3][1].body)).toEqual({id:'abandon-one',kind:'abandon',revision:1})
 })
 it('refuses caller authority, stale local revision and historical action without sending',async()=>{
  const fetch=vi.fn();vi.stubGlobal('fetch',fetch);const client=await serverRunClient()
  await expect(client.start({...start,prompt:'hidden'} as typeof start)).rejects.toMatchObject({delivery:'not-sent'})
  expect(()=>client.action(snapshot(),{id:'cancel',kind:'cancel',revision:2})).toThrow(RunRequestError)
  expect(()=>client.action({...snapshot(),historical:true},{id:'abandon',kind:'abandon',revision:1})).toThrow(RunRequestError)
  expect(()=>client.action(snapshot(),{id:'cancel',kind:'cancel',revision:1,approved:true} as never)).toThrow(RunRequestError)
  expect(fetch).not.toHaveBeenCalled()
 })
 it('retains server stale refusal and does not retry or expose raw prose',async()=>{
  const fetch=vi.fn().mockResolvedValue(json({error:'secret-provider-url',code:'stale'},409));vi.stubGlobal('fetch',fetch)
  const client=await serverRunClient()
  await expect(client.action(snapshot(),{id:'cancel',kind:'cancel',revision:1})).rejects.toMatchObject({delivery:'refused',status:409,failure:{outcome:'refused'}})
  expect(fetch).toHaveBeenCalledTimes(1)
 })
 it('lost POST receipt is uncertain; only explicit discovery/read reconciles and never chooses another run',async()=>{
  const fetch=vi.fn().mockRejectedValueOnce(Error('raw upstream secret')).mockResolvedValueOnce(json({runs:[pending]})).mockResolvedValueOnce(json(pending));vi.stubGlobal('fetch',fetch)
  const client=await serverRunClient()
  await expect(client.start(start)).rejects.toMatchObject({delivery:'uncertain',failure:{outcome:'unknown'}})
  expect(fetch).toHaveBeenCalledTimes(1)
  expect(await client.discover(binding)).toHaveLength(1)
  expect((await client.read(ref)).id).toBe(ref.id)
  expect(fetch.mock.calls.map(([path])=>path)).toEqual(['/api/assistant-runs','/api/assistant-runs/owners/chat-one/requests/message-one',`/api/assistant-runs/${ref.id}`])
  fetch.mockResolvedValueOnce(json({...pending,id:'arun_'+'2'.repeat(32)}))
  await expect(client.read(ref)).rejects.toMatchObject({failure:{code:'invalid-response'}})
 })
 it('malformed mutation success is uncertain, server 503 never grants retry permission',async()=>{
  const fetch=vi.fn().mockResolvedValueOnce(json({})).mockResolvedValueOnce(json({error:'secret'},503));vi.stubGlobal('fetch',fetch)
  const client=await serverRunClient()
  await expect(client.start(start)).rejects.toMatchObject({delivery:'uncertain',failure:{code:'invalid-response'}})
  await expect(client.start({...start,admissionId:'explicit-new'})).rejects.toMatchObject({delivery:'uncertain',status:503})
  expect(fetch).toHaveBeenCalledTimes(2)
 })
 it('sends cursor only as a header and never continues a paused/terminal EOF',async()=>{
  const observed=new RunObservations(ref,decodeSnapshot(terminal,binding))
  const wire=`event: snapshot\nid: 3:2\ndata: ${JSON.stringify(terminal)}\n\n`
  const fetch=vi.fn().mockImplementation(()=>Promise.resolve(new Response(wire,{headers:{'Content-Type':'text/event-stream'}})));vi.stubGlobal('fetch',fetch)
  const client=await serverRunClient(), out=[]
  for await(const value of client.events(observed))out.push(value)
  expect(out.map(v=>v.kind)).toEqual(['snapshot']);expect(fetch).toHaveBeenCalledTimes(1)
  expect(fetch.mock.calls[0][0]).toBe(`/api/assistant-runs/${ref.id}/events`)
  expect(fetch.mock.calls[0][1].headers).toMatchObject({'Last-Event-ID':'3:2',Authorization:'Bearer test-session'})
 })
 it('changed or ended session ends in-flight reading and preserves completed observations',async()=>{
  const client=await serverRunClient(), observed=new RunObservations(ref,decodeSnapshot(terminal,binding))
  giveThisPageASessionForTesting('replacement-session')
  await expect(client.read(ref)).rejects.toMatchObject({delivery:'not-sent'})
  giveThisPageASessionForTesting('test-session')
  let started!:()=>void; const ready=new Promise<void>(resolve=>{started=resolve});let cancelled=false
  vi.stubGlobal('fetch',vi.fn().mockImplementation(()=>{started();return Promise.resolve(new Response(new ReadableStream({cancel(){cancelled=true}}),{headers:{'Content-Type':'text/event-stream'}}))}))
  const reading=(async()=>{for await(const _ of client.events(observed)){void _}})()
  await ready; await new Promise(resolve=>setTimeout(resolve,0));forgetSession();await reading
  expect(cancelled).toBe(true);expect(observed.snapshot?.state).toBe('complete')
 })
})
it('releases a lost mutation response on abort and preserves uncertain delivery',async()=>{
 const client=await serverRunClient(),controller=new AbortController();let cancelled=false,started!:()=>void
 const ready=new Promise<void>(resolve=>{started=resolve})
 vi.stubGlobal('fetch',vi.fn().mockImplementation(()=>{started();return Promise.resolve(new Response(new ReadableStream({cancel(){cancelled=true}}),{headers:{'Content-Type':'application/json'}}))}))
 const request=client.start(start,controller.signal)
 const result=expect(request).rejects.toMatchObject({delivery:'uncertain'})
 await ready;await new Promise(resolve=>setTimeout(resolve,0));controller.abort();await result
 expect(cancelled).toBe(true)
})
it('reconciles explicit paused EOF and reports active EOF without continuing',async()=>{
 const client=await serverRunClient(),fetch=vi.fn().mockImplementation(()=>Promise.resolve(new Response(`event: snapshot\nid: 1:0\ndata: ${JSON.stringify({...pending,state:'awaiting-input'})}\n\n`,{headers:{'Content-Type':'text/event-stream'}})))
 vi.stubGlobal('fetch',fetch)
 for await(const _ of client.events(new RunObservations(ref))){void _}
 fetch.mockImplementation(()=>Promise.resolve(new Response(`event: snapshot\nid: 1:0\ndata: ${JSON.stringify(pending)}\n\n`,{headers:{'Content-Type':'text/event-stream'}})))
 const observed=new RunObservations(ref)
 await expect((async()=>{for await(const _ of client.events(observed)){void _}})()).rejects.toMatchObject({failure:{code:'connection-lost'}})
 expect(observed.snapshot?.state).toBe('admitted');expect(fetch).toHaveBeenCalledTimes(2)
})
it('discovers an unchanged saved request after journal revision changes, retaining original and historical binding',async()=>{
 const current={owner:{...binding.owner,revision:'c'.repeat(64)},requestId:binding.requestId}
 const fetch=vi.fn().mockImplementation(()=>Promise.resolve(json({runs:[{...pending,historical:true}]})));vi.stubGlobal('fetch',fetch)
 const client=await serverRunClient(),runs=await client.discover(current)
 expect(runs[0].owner.revision).toBe(binding.owner.revision);expect(runs[0].historical).toBe(true)
 await expect(client.discover({owner:{kind:'chat',id:'other-chat'},requestId:binding.requestId})).rejects.toMatchObject({failure:{code:'invalid-response'}})
 await expect(client.discover({owner:current.owner,requestId:'other-request'})).rejects.toMatchObject({failure:{code:'invalid-response'}})
 await expect(client.read({...ref,owner:current.owner})).rejects.toMatchObject({failure:{code:'invalid-response'}})
 const original=ref.owner.revision
 fetch.mockImplementation(()=>Promise.resolve(json(pending)))
 expect((await client.read({...ref,owner:runs[0].owner})).owner.revision).toBe(original)
})

it.each([
 ['array admitted state',['admitted']],
 ['nested array admitted state',[['admitted']]],
 ['array running state',['running']],
 ['nested array running state',[['running']]]
])('refuses malformed active SSE snapshot before EOF: %s',async(_name,state)=>{
 const fetch=vi.fn().mockImplementation(()=>Promise.resolve(new Response(`event: snapshot\nid: 1:0\ndata: ${JSON.stringify({...pending,state})}\n\n`,{headers:{'Content-Type':'text/event-stream'}})))
 vi.stubGlobal('fetch',fetch)
 const observed=new RunObservations(ref),client=await serverRunClient(),out:unknown[]=[]
 await expect((async()=>{for await(const value of client.events(observed))out.push(value)})()).rejects.toMatchObject({failure:{code:'invalid-response'}})
 expect(out).toEqual([]);expect(observed.snapshot).toBeUndefined();expect(observed.cursor).toBeUndefined();expect(fetch).toHaveBeenCalledTimes(1)
})

it('explicit saved-pack recheck uses references only, binds returned findings and never retries',async()=>{
 const {digestOf}=await import('../../research/checkCandidate')
 const check={documentDigest:'a'.repeat(64),suiteKey:'[]',at:'2026-10-23T00:00:00Z',valid:true,diagnostics:[],totalCases:0}
 const request={id:'check-one',documentDigest:check.documentDigest,suiteDigest:await digestOf(check.suiteKey)}
 const fetch=vi.fn().mockResolvedValueOnce(json(check)).mockResolvedValueOnce(json({...check,documentDigest:'b'.repeat(64)})).mockRejectedValueOnce(Error('private transport prose'))
 vi.stubGlobal('fetch',fetch);const client=await serverRunClient()
 expect(await client.recheckPack(ref,request)).toEqual(check)
 expect(fetch.mock.calls[0][0]).toBe(`/api/assistant-runs/${ref.id}/pack-checks`)
 expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual(request)
 await expect(client.recheckPack(ref,{...request,id:'check-two'})).rejects.toMatchObject({delivery:'uncertain',failure:{code:'invalid-response'}})
 await expect(client.recheckPack(ref,{...request,id:'check-three'})).rejects.toMatchObject({delivery:'uncertain',failure:{code:'connection-lost'}})
 await expect(client.recheckPack(ref,{...request,path:'foreign.json'} as never)).rejects.toMatchObject({delivery:'not-sent'})
 expect(fetch).toHaveBeenCalledTimes(3)
})
it('does not confuse another admission acknowledgement with this start',async()=>{
 const fetch=vi.fn().mockResolvedValue(new Response(JSON.stringify({...pending,admissionId:'another-admission'}),{headers:{'Content-Type':'application/json'}}));vi.stubGlobal('fetch',fetch)
 const client=await serverRunClient();await expect(client.start(start)).rejects.toMatchObject({delivery:'uncertain',failure:{code:'invalid-response'}})
 expect(fetch).toHaveBeenCalledOnce()
})

const planSnapshot=()=>decodeSnapshot(structuredClone(plan),binding)
const planAction=(approved=true):RunAction=>({id:'plan-one',kind:'plan-decision',revision:plan.revision,...plan.planDecisionReference,approved})
it.each([true,false])('sends one exact Plan decision with optional note (%s)',async approved=>{
 const fetch=vi.fn().mockResolvedValue(json(plan));vi.stubGlobal('fetch',fetch)
 const client=await serverRunClient(),action={...planAction(approved),note:''}
 await client.action(planSnapshot(),action)
 expect(fetch).toHaveBeenCalledOnce()
 expect(fetch.mock.calls[0][0]).toBe(`/api/assistant-runs/${plan.id}/actions`)
 expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual(action)
 expect(fetch.mock.calls[0][1].headers.Authorization).toBe('Bearer test-session')
})
it.each([
 ['missing call',{callId:undefined}],['empty call',{callId:''}],['oversized call',{callId:'x'.repeat(257)}],['invalid call',{callId:'bad\ncall'}],['stale call',{callId:'other-call'}],
 ['missing Plan revision',{planRevision:undefined}],['unprefixed Plan revision',{planRevision:'a'.repeat(64)}],['stale Plan revision',{planRevision:'sha256:'+'a'.repeat(64)}],
 ['missing candidate revision',{candidateRevision:undefined}],['uppercase candidate revision',{candidateRevision:'sha256:'+'A'.repeat(64)}],['stale candidate revision',{candidateRevision:'sha256:'+'a'.repeat(64)}],
 ['missing boolean',{approved:undefined}],['null boolean',{approved:null}],['numeric boolean',{approved:1}],
 ['oversized note',{note:'x'.repeat(2001)}],['null note',{note:null}],['NUL note',{note:'a\0b'}],['invalid Unicode note',{note:'\ud800'}],
 ['approval digest',{argsDigest:'a'.repeat(64)}],['extra authority',{ownerId:'other-chat'}],['stale run revision',{revision:2}]
])('refuses malformed or stale Plan action without sending: %s',async(_name,change)=>{
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch);const client=await serverRunClient()
 expect(()=>client.action(planSnapshot(),{...planAction(),...change} as RunAction)).toThrow(RunRequestError)
 expect(fetch).not.toHaveBeenCalled()
})
it.each([
 ['historical',(s:RunSnapshot)=>({...s,historical:true})],
 ['terminal',(s:RunSnapshot)=>({...s,state:'stopped'})],
 ['budget',(s:RunSnapshot)=>({...s,state:'paused-budget'})],
 ['missing saved reference',(s:RunSnapshot)=>({...s,planDecisionReference:undefined})],
 ['stale saved reference',(s:RunSnapshot)=>({...s,planDecisionReference:{...s.planDecisionReference!,callId:'other-call'}})],
 ['unknown saved reference field',(s:RunSnapshot)=>({...s,planDecisionReference:{...s.planDecisionReference!,argsDigest:'a'.repeat(64)}})],
 ['missing checkpoint',(s:RunSnapshot)=>({...s,checkpoint:undefined})],
 ['running checkpoint',(s:RunSnapshot)=>({...s,checkpoint:{...s.checkpoint!,status:'running'}})],
 ['Plan phase',(s:RunSnapshot)=>({...s,checkpoint:{...s.checkpoint!,phase:'plan'}})],
 ['asked approval',(s:RunSnapshot)=>({...s,checkpoint:{...s.checkpoint!,approval:{state:'asked',callId:'other-call',name:'tool',args:{}}}})],
 ['ambiguous approval reference',(s:RunSnapshot)=>({...s,approvalReference:{callId:'other-call',argsDigest:'a'.repeat(64)}})],
 ['malformed retained Plan',(s:RunSnapshot)=>({...s,checkpoint:{...s.checkpoint!,planState:{...s.checkpoint!.planState!,revision:'sha256:'+'a'.repeat(64)}}})]
])('refuses unrunnable saved Plan state without sending: %s',async(_name,change)=>{
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch);const client=await serverRunClient()
 expect(()=>client.action(change(planSnapshot()) as RunSnapshot,planAction())).toThrow(RunRequestError)
 expect(fetch).not.toHaveBeenCalled()
})
it('refuses generic budget, answer and approval bypass of a pending Plan without sending',async()=>{
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch);const client=await serverRunClient()
 const actions:RunAction[]=[{id:'budget',kind:'extend-budget',revision:1},{id:'answer',kind:'answer',revision:1,questionId:'q',questionDigest:'a'.repeat(64),answer:{selected:[],text:'Continue'}},{id:'approval',kind:'approval',revision:1,callId:'other',argsDigest:'a'.repeat(64),approved:true}]
 for(const action of actions)expect(()=>client.action(planSnapshot(),action)).toThrow(RunRequestError)
 expect(fetch).not.toHaveBeenCalled()
})

it('requires the exact decoded current asked approval before sending a decision',async()=>{
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch);const client=await serverRunClient(),saved=decodeSnapshot(approval,binding)
 const action:RunAction={id:'explicit-approval',kind:'approval',revision:saved.revision,...approval.approvalReference,shownDigest:approval.approvalReference.argsDigest,approved:true}
 const attempts:[RunSnapshot,RunAction][]=[
  [saved,{...action,callId:'forged-call'}],
  [saved,{...action,argsDigest:'a'.repeat(64)}],
  [saved,{...action,revision:2}],
  [{...saved,historical:true},action],
  [{...saved,state:'complete'},action],
  [{...saved,approvalReference:undefined},action],
  [{...saved,checkpoint:{...saved.checkpoint!,approval:undefined}},action],
  [{...saved,checkpoint:{...saved.checkpoint!,approval:{...saved.checkpoint!.approval!,state:'approved'}}},action],
 ]
 for(const [current,decision] of attempts)expect(()=>client.action(current,decision)).toThrow(RunRequestError)
 expect(fetch).not.toHaveBeenCalled()
})

it('fails closed on an old pending reference or missing render proof, then accepts one explicit refreshed decision',async()=>{
 const {default:approval}=await import('./fixtures/approval.json'),saved=decodeSnapshot(approval,binding)
 const fetch=vi.fn().mockResolvedValue(json({...pending,state:'stopped'}));vi.stubGlobal('fetch',fetch)
 const client=await serverRunClient(),action:RunAction={id:'refreshed-card',kind:'approval',revision:saved.revision,...approval.approvalReference,shownDigest:approval.approvalReference.argsDigest,approved:true}
 for(const stale of [{...action,argsDigest:action.argsDigest.slice(7)},{...action,shownDigest:undefined},{...action,shownDigest:'sha256:'+'a'.repeat(64)}])expect(()=>client.action(saved,stale)).toThrow(RunRequestError)
 expect(fetch).not.toHaveBeenCalled()
 await client.action(saved,action)
 expect(fetch).toHaveBeenCalledOnce()
 expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual(action)
})
