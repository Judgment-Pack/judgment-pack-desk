import { describe, expect, it, afterEach, vi } from 'vitest'
import { AIFailureError } from '@judgment-pack/harness/failures'
import { giveThisPageASessionForTesting, forgetSession } from '../../mcp/session'
import { serverRunClient } from './client'
import { decodeProgress, decodeSnapshot, MAX_PROGRESS_BYTES } from './decode'
import { readFrames, RunObservations } from './stream'
import pending from './fixtures/pending.json'
import terminal from './fixtures/terminal.json'
import progress from './fixtures/progress.json'
const ref = {...pending,owner:{...pending.owner,kind:'chat' as const}}
const snapshot = () => decodeSnapshot(pending,ref)
const wire = (p:unknown) => `event: progress\ndata: ${JSON.stringify(p)}\n\n`
const snap = (p:typeof pending|typeof terminal) => `event: snapshot\nid: ${p.cursor}\ndata: ${JSON.stringify(p)}\n\n`
function bytes(raw:string,width=1){const b=new TextEncoder().encode(raw);let at=0;return new ReadableStream<Uint8Array>({pull(c){if(at===b.length)c.close();else{c.enqueue(b.slice(at,at+width));at+=Math.min(width,b.length-at)}}})}
afterEach(()=>vi.unstubAllGlobals())
describe('ephemeral canonical progress',()=>{
 it('reads the Go wire vector in fragmented Unicode; whole prefix, empty clear and task isolation never move replay cursor',async()=>{
  const seen=new RunObservations(ref);seen.beginStream()
  const out=[]
  for await(const f of readFrames(bytes(snap(pending)+wire(progress)+wire({...progress,sequence:2,taskId:'child',text:'Child only'})+wire({...progress,sequence:3,text:''})))){const o=seen.accept(f);if(o)out.push(o)}
  expect(out.map(v=>v.kind)).toEqual(['snapshot','progress','progress','progress'])
  expect(seen.answer).toBe('');expect(seen.children.get('child')?.text).toBe('Child only');expect(seen.cursor).toBe('1:0')
  seen.accept({kind:'progress',data:{...progress,sequence:4,text:'Replacement'}})
  expect(seen.answer).toBe('Replacement')
  seen.accept({kind:'progress',data:{...progress,sequence:4,text:'duplicate'}})
  seen.accept({kind:'progress',data:{...progress,sequence:2,text:'older'}})
  expect(seen.answer).toBe('Replacement')
 })
 it.each(['id','owner','requestId','epoch','sequence','cursor','text'])('requires %s in the new closed envelope',key=>{const p={...progress};delete p[key as keyof typeof p];expect(()=>decodeProgress(p,ref)).toThrow(AIFailureError)})
 it.each([
  {id:'arun_'+'3'.repeat(32)}, {owner:{...pending.owner,id:'other'}}, {owner:{...pending.owner,kind:'project'}}, {owner:{...pending.owner,revision:'b'.repeat(64)}}, {requestId:'other'}, {epoch:''}, {epoch:'A'.repeat(32)}, {sequence:0}, {sequence:1.5}, {sequence:Number.MAX_SAFE_INTEGER+1}, {cursor:'0:0'}, {text:'\uD800'}, {text:'\0'}, {text:'x'.repeat(MAX_PROGRESS_BYTES+1)}, {taskId:''}, {taskId:'x'.repeat(129)}, {taskId:null}, {authority:true},
 ])('refuses malformed, foreign or oversized progress (%j)',patch=>{expect(()=>decodeProgress({...progress,...patch},ref)).toThrow(AIFailureError)})
 it('requires stream reconciliation and exact current epoch/cursor; clears on terminal, reconnect and disconnect',()=>{
  const seen=new RunObservations(ref,snapshot());seen.beginStream()
  expect(()=>seen.accept({kind:'progress',data:progress})).toThrow()
  seen.accept({kind:'snapshot',cursor:'1:0',data:pending});seen.accept({kind:'progress',data:progress})
  expect(seen.answer).toBe(progress.text)
  expect(()=>seen.accept({kind:'progress',data:{...progress,sequence:2,cursor:'2:0'}})).toThrow()
  expect(()=>seen.accept({kind:'progress',data:{...progress,sequence:2,epoch:'3'.repeat(32)}})).toThrow()
  seen.beginStream();expect(seen.answer).toBe('');seen.accept({kind:'snapshot',cursor:'1:0',data:pending});seen.accept({kind:'progress',data:progress})
  seen.accept({kind:'event',cursor:'3:1',data:terminal.events[0]});expect(seen.answer).toBe('')
  expect(()=>seen.accept({kind:'progress',data:{...progress,sequence:2,cursor:'3:1'}})).toThrow()
  seen.accept({kind:'snapshot',cursor:'3:2',data:terminal})
  expect(()=>seen.accept({kind:'progress',data:{...progress,cursor:'3:2'}})).toThrow()
  expect(seen.cursor).toBe('3:2');seen.endStream();expect(seen.answer).toBe('')
 })
 it('bounds views and refuses progress SSE ids, missing payloads and overlong input',async()=>{
  const seen=new RunObservations(ref,snapshot())
  for(let i=1;i<=17;i++)seen.accept({kind:'progress',data:{...progress,sequence:i,taskId:`child-${i}`}})
  expect(()=>seen.accept({kind:'progress',data:{...progress,sequence:18}})).toThrow()
  for(const raw of [`event: progress\nid: 1:0\ndata: ${JSON.stringify(progress)}\n\n`,`event: progress\nid:\ndata: {}\n\n`,'event: progress\n\n',wire({...progress,text:'x'.repeat(6*MAX_PROGRESS_BYTES+2048)})]){
   await expect((async()=>{for await(const f of readFrames(bytes(raw,1024)))void f})()).rejects.toBeInstanceOf(AIFailureError)
  }
 })
 it('bounds aggregate live and saved preview bytes independently of the per-prefix and view caps',()=>{
  const seen=new RunObservations(ref,snapshot()),text='x'.repeat(MAX_PROGRESS_BYTES)
  for(let i=1;i<=15;i++)seen.accept({kind:'progress',data:{...progress,sequence:i,text,taskId:`task-${i}`}})
  expect(()=>seen.accept({kind:'progress',data:{...progress,sequence:16,text,taskId:'task-16'}})).toThrow()
  expect(seen.children.size).toBe(15);expect(seen.cursor).toBe('1:0')
  const rows=Array.from({length:16},(_,i)=>({text,taskId:`task-${i}`}))
  expect(()=>decodeSnapshot({...pending,displayPreview:rows},ref)).toThrow()
 })
 it('reads bounded persisted previews as display data only, independent of live epoch and durable events',()=>{
  const s=decodeSnapshot({...pending,displayPreview:[{text:'Interrupted prose'},{text:'Child prose',taskId:'child'}]},ref)
  const seen=new RunObservations(ref,s);expect(s.displayPreview).toHaveLength(2);expect(seen.answer).toBe('');expect(seen.cursor).toBe('1:0')
  for(const displayPreview of [null,[],[{}],[{text:''}],[{text:'x',taskId:''}],[{text:'x',authority:true}],[{text:'x'},{text:'y'}],[{text:'\ud800'}],[{text:'x'.repeat(MAX_PROGRESS_BYTES+1)}]])expect(()=>decodeSnapshot({...pending,displayPreview},ref)).toThrow()
 })
 it('uses actual authenticated fetch reader before completion and clears on logout without replay/cancel',async()=>{
  giveThisPageASessionForTesting('progress-session')
  let stream:ReadableStreamDefaultController<Uint8Array>|undefined
  const body=new ReadableStream<Uint8Array>({start(c){stream=c}})
  const fetch=vi.fn().mockResolvedValue(new Response(body,{headers:{'Content-Type':'text/event-stream'}}));vi.stubGlobal('fetch',fetch)
  const client=await serverRunClient(),seen=new RunObservations(ref), events=client.events(seen)
  stream!.enqueue(new TextEncoder().encode(snap(pending)+wire(progress)))
  expect((await events.next()).value?.kind).toBe('snapshot')
  expect((await events.next()).value?.kind).toBe('progress');expect(seen.answer).toBe(progress.text)
  forgetSession();await events.next();expect(seen.answer).toBe('');expect(seen.cursor).toBe('1:0');expect(fetch).toHaveBeenCalledTimes(1)
  expect(fetch.mock.calls[0][1].headers.Authorization).toBe('Bearer progress-session')
 })
})
