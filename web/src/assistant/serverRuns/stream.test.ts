import { describe, expect, it } from 'vitest'
import { AIFailureError } from '@judgment-pack/harness/failures'
import pending from './fixtures/pending.json'
import terminal from './fixtures/terminal.json'
import { decodeSnapshot, MAX_RUN_BYTES } from './decode'
import { readFrames, RunObservations } from './stream'
const reference = {...pending,owner:{...pending.owner,kind:'chat' as const}}
export const frame = (kind: string,cursor: string,data:unknown) => `event: ${kind}\r\nid: ${cursor}\r\ndata: ${JSON.stringify(data)}\r\n\r\n`
function chunks(raw: string, width=7) {
 const bytes=new TextEncoder().encode(raw); let at=0
 return new ReadableStream<Uint8Array>({pull(c){if(at===bytes.length){c.close();return} c.enqueue(bytes.slice(at,at+width));at+=Math.min(width,bytes.length-at)}})
}
async function read(raw: string, width=7) { const out=[]; for await(const f of readFrames(chunks(raw,width))) out.push(f); return out }
describe('durable fetch SSE',()=>{
 it('handles fragmented UTF8, split CRLF, comments, duplicate events and a snapshot sharing the cursor',async()=>{
  const observed=new RunObservations(reference,decodeSnapshot(pending,reference))
  const raw=': keepalive\r\n\r\n'+frame('event','3:1',terminal.events[0])+frame('event','3:1',terminal.events[0])+frame('event','3:2',terminal.events[1])+frame('snapshot','3:2',terminal)
  const out=[];for await(const f of readFrames(chunks(raw,1))){const value=observed.accept(f);if(value)out.push(value)}
  expect(out.map(v=>v.kind)).toEqual(['event','event','snapshot'])
  expect(observed.snapshot?.events[0].event.text).toBe('Ready 한글')
  expect(observed.cursor).toBe('3:2')
  expect(observed.accept((await read(frame('snapshot','3:2',terminal)))[0])?.kind).toBe('snapshot')
 })
 it.each(['data: {}\n\n','event: bad\nid: 1:0\ndata: {}\n\n','event: snapshot\nid: 1:0\ndata: {\n\n',frame('snapshot','1:0',pending).slice(0,-2),'event: snapshot\nid: 1:0\nid: 1:0\ndata: {}\n\n'])('fails malformed or truncated frame safely',async raw=>{await expect(read(raw)).rejects.toBeInstanceOf(AIFailureError)})
 it('bounds frame bytes and rejects malformed UTF8',async()=>{
  await expect(read('x'.repeat(MAX_RUN_BYTES+1),65536)).rejects.toBeInstanceOf(AIFailureError)
  const body=new ReadableStream<Uint8Array>({start(c){c.enqueue(new Uint8Array([0xff]));c.close()}})
  await expect((async()=>{for await(const _ of readFrames(body)){ void _ }})()).rejects.toBeInstanceOf(AIFailureError)
 })
 it('rejects changed event identity and stale snapshots',()=>{
  const observed=new RunObservations(reference,decodeSnapshot(terminal,reference))
  expect(()=>observed.accept({kind:'event',cursor:'3:1',data:{...terminal.events[0],event:{type:'message',text:'changed'}}})).toThrow(AIFailureError)
  expect(()=>observed.accept({kind:'snapshot',cursor:'1:0',data:pending})).toThrow(AIFailureError)
 })
 it('abort cancels and releases reader while retaining completed observations',async()=>{
  let cancelled=false
  const body=new ReadableStream<Uint8Array>({cancel(){cancelled=true}}), controller=new AbortController()
  const observed=new RunObservations(reference,decodeSnapshot(terminal,reference))
  const reading=(async()=>{for await(const f of readFrames(body,controller.signal))observed.accept(f)})()
  controller.abort(); await reading
  expect(cancelled).toBe(true);expect(body.locked).toBe(false);expect(observed.snapshot?.state).toBe('complete')
 })
})
it('does not erase an event observed before its snapshot or mutate a completed receipt',()=>{
 const observed=new RunObservations(reference,decodeSnapshot(pending,reference))
 observed.accept({kind:'event',cursor:'3:1',data:terminal.events[0]})
 expect(()=>observed.accept({kind:'snapshot',cursor:'3:0',data:{...pending,revision:3,cursor:'3:0'}})).toThrow(AIFailureError)
 expect(observed.snapshot?.revision).toBe(1)
 const complete=new RunObservations(reference,decodeSnapshot(terminal,reference))
 expect(()=>complete.accept({kind:'snapshot',cursor:'4:2',data:{...terminal,revision:4,cursor:'4:2',operations:[{...terminal.operations[0],result:'e30='}]}})).toThrow(AIFailureError)
 expect(complete.snapshot?.operations[0].result).toBe(terminal.operations[0].result)
})
