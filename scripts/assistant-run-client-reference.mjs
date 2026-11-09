// Expected behavior is handwritten here; inputs are ephemeral actual Go output.
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { decodeDiscovery, decodeSnapshot, parseRunJSON, SEMANTIC_EVENT_TYPES } from '../web/src/assistant/serverRuns/decode.ts'
import { readFrames, RunObservations } from '../web/src/assistant/serverRuns/stream.ts'
const dir=process.argv[2]
assert(dir,'actual Go producer output required')
const read=async name=>parseRunJSON(await readFile(join(dir,name),'utf8'))
const pending=await read('pending.json'),terminal=await read('terminal.json')
const binding={owner:pending.owner,requestId:pending.requestId,id:pending.id}
const observed=new RunObservations(binding,decodeSnapshot(pending,binding))
assert.equal(observed.snapshot.checkpoint.status,'running')
const completed=decodeSnapshot(terminal,binding)
assert.equal(completed.state,'complete');assert.equal(completed.checkpoint.status,'complete')
assert.equal(completed.events[0].event.text,'Ready 한글')
const discovery=await read('discovery.json')
assert.equal(discovery.runs.length,1);assert.equal(decodeSnapshot(discovery.runs[0],binding).id,pending.id)
const current=await read('current-binding.json')
assert.notEqual(current.owner.revision,binding.owner.revision,'authorized journal changed source revision')
const retained=decodeDiscovery(await read('changed-journal-discovery.json'),current)
assert.equal(retained.length,1);assert.equal(retained[0].id,pending.id)
assert.equal(retained[0].owner.revision,binding.owner.revision,'discover must preserve original source revision')
assert.throws(()=>decodeSnapshot(retained[0],{...current,id:pending.id}),'ordinary read still binds exact original revision')
assert.equal(decodeDiscovery(await read('historical-discovery.json'),current)[0].historical,true)
assert.deepEqual(decodeDiscovery(await read('wrong-request-discovery.json'),{owner:current.owner,requestId:'other-request'}),[])
assert.throws(()=>decodeDiscovery(discovery,{owner:{kind:'chat',id:'other-chat'},requestId:binding.requestId}),'wrong owner refused')
assert.throws(()=>decodeDiscovery(discovery,{owner:current.owner,requestId:'other-request'}),'wrong request refused')

const wire=new Uint8Array(await readFile(join(dir,'events.sse')))
let at=0
const stream=new ReadableStream({pull(c){if(at===wire.length){c.close();return}c.enqueue(wire.slice(at,++at))}})
const seen=[]
for await(const frame of readFrames(stream)){const observation=observed.accept(frame);if(observation)seen.push(observation)}
assert.deepEqual(seen.map(v=>v.kind),['event','event','snapshot'])
assert.equal(seen[1].cursor,seen[2].cursor,'semantic end and authoritative snapshot share a cursor')
assert.deepEqual(observed.snapshot,completed)
const reconnect=new Response(await readFile(join(dir,'reconnect.sse')))
let reconciled=0
for await(const frame of readFrames(reconnect.body)){const observation=observed.accept(frame);assert.equal(observation.kind,'snapshot');reconciled++}
assert.equal(reconciled,1)
assert.deepEqual(Object.keys(SEMANTIC_EVENT_TYPES).sort(),await read('event-types.json'),'released browser and Go durable event union parity')
console.log('Run client readers: actual pending/terminal checkpoints, changed-journal/historical discovery with original binding and wrong-owner/request refusal, UTF8 SSE suffix, shared cursor, reconnect and 31 Go/browser discriminators passed.')
