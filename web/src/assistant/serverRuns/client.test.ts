import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { forgetSession, giveThisPageASessionForTesting } from '../../mcp/session'
import { RunRequestError, serverRunClient } from './client'
import { decodeSnapshot } from './decode'
import { RunObservations } from './stream'
import pending from './fixtures/pending.json'
import terminal from './fixtures/terminal.json'

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
  await client.action(snapshot(),{id:'approval-one',kind:'approval',revision:1,callId:'call-one',argsDigest:'b'.repeat(64),approved:false,note:''})
  expect(JSON.parse(fetch.mock.calls[2][1].body)).toEqual({id:'approval-one',kind:'approval',revision:1,callId:'call-one',argsDigest:'b'.repeat(64),approved:false,note:''})
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
