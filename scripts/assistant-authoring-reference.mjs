// Handwritten expectations; observations are temporary actual Go API/native output.
// This checks browser protocol exports, not reachability of an application route.
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import {join} from 'node:path'
import {decodeSnapshot,parseRunJSON} from '../web/src/assistant/serverRuns/decode.ts'
import {readFrames,RunObservations} from '../web/src/assistant/serverRuns/stream.ts'
import {serverRunClient} from '../web/src/assistant/serverRuns/client.ts'
import {giveThisPageASessionForTesting} from '../web/src/mcp/session.ts'
import {canonicalProposal} from '../web/src/assistant/proposalValue.ts'
import {reportFromExecution} from '../web/src/assistant/report.ts'

const dir=process.argv[2]
assert(dir,'actual authoring producer output is required')
giveThisPageASessionForTesting('synthetic-author-reader-session')
const client=await serverRunClient()
const purposes=['author-pack','describe-pack','author-graph']
const cases=[...purposes.map(purpose=>['api-'+purpose,purpose]),['native-author-pack','author-pack']]
const json=value=>new Response(JSON.stringify(value),{headers:{'Content-Type':'application/json'}})
const decodeBytes=value=>Buffer.from(value,'base64').toString('utf8')
for(const[name,purpose]of cases){
 const output=parseRunJSON(await readFile(join(dir,name+'.json'),'utf8'))
 const {request,captured,reference,cancelled,capture,run:raw}=output
 const start=captured.start
 assert.equal(start.purpose,purpose);assert.equal(start.requestId,request.requestId)
 assert.match(start.ownerId,/^author_[a-f0-9]{32}$/);assert.match(start.admissionId,/^author-admit-[a-f0-9]{32}$/)
 assert.match(start.revision,/^[a-f0-9]{64}$/)
 assert.deepEqual(Object.keys(capture).sort(),['admissionId','id','request','requestId','revision','scope','snapshot'])
 assert.deepEqual(Object.keys(capture.scope).sort(),['connectionId','connectionRevision','policy','principal','profileRevision','project','session'])
 assert.equal(capture.id,start.ownerId);assert.equal(capture.admissionId,start.admissionId);assert.equal(capture.revision,start.revision)
 assert.deepEqual(parseRunJSON(decodeBytes(capture.request)),request)
 const source=parseRunJSON(decodeBytes(capture.snapshot))
 if(purpose==='author-pack'){
  assert(request.draft.includes('UNSAVED-DRAFT'));assert(!decodeBytes(source.pack.pack).includes('UNSAVED-DRAFT'))
  assert.notEqual(decodeBytes(source.pack.pack),request.draft)
 }else if(purpose==='describe-pack')assert.deepEqual(source,{})
 else {assert.equal(source.graph.id,'flow');assert.equal(source.packs[0].id,'one');assert(decodeBytes(source.packs[0].bytes).length>0)}
 const binding={owner:{kind:purpose,id:start.ownerId,revision:start.revision},requestId:start.requestId}
 const run=decodeSnapshot(raw,binding)
 assert.equal(run.state,'complete');assert.equal(run.checkpoint.status,'complete')
 const native=name.startsWith('native-')
 assert.equal(run.checkpoint.used,native?0:1,'provider-managed native work must not invent model-request usage')
 assert.equal(run.checkpoint.unit,name.startsWith('native-')?'provider-managed':'model-requests')
 assert.equal(run.operations.filter(op=>op.kind==='model').length,1)
 const proposals=run.events.filter(({event})=>event.type==='proposal')
 assert.equal(proposals.length,1)
 const proposal=canonicalProposal(proposals[0].event)
 assert.equal(proposal.type,'proposal');assert(Object.isFrozen(proposal.document));assert(Object.isFrozen(proposal))
 const report=reportFromExecution(run.checkpoint)
 if(native){assert.equal(report?.requests??0,0);if(report)assert.equal(report.unit,'provider-managed')}
 else {assert.equal(report.requests,1);assert.equal(report.unit,'model-requests');assert(report.prompts.length>0)}
 for(const bad of [
  {...binding,owner:{...binding.owner,id:'author_'+'f'.repeat(32)}},
  {...binding,owner:{...binding.owner,revision:'f'.repeat(64)}},
  {...binding,owner:{...binding.owner,kind:'chat'}},
  {...binding,requestId:'foreign-request'}
 ])assert.throws(()=>decodeSnapshot(raw,bad))
 const bytes=new Uint8Array(await readFile(join(dir,name+'.sse')))
 let at=0
 const stream=new ReadableStream({pull(controller){if(at===bytes.length){controller.close();return}controller.enqueue(bytes.slice(at,++at))}})
 const observations=new RunObservations(binding,run)
 for await(const frame of readFrames(stream))observations.accept(frame)
 assert.deepEqual(observations.snapshot,run,'actual UTF8 event replay converges on complete checkpoint')
 const calls=[]
 let response
 globalThis.fetch=async(path,init)=>{calls.push({path,...init});return json(response)}
 const target={connectionId:capture.scope.connectionId,connectionRevision:capture.scope.connectionRevision}
 response=captured
 assert.deepEqual((await client.captureAuthoring(request,target)).start,start)
 assert.equal(calls.at(-1).method,'POST');assert.deepEqual(JSON.parse(calls.at(-1).body),request)
 assert.equal(calls.at(-1).headers['X-Assistant-Connection'],target.connectionId)
 assert.equal(calls.at(-1).headers['X-Assistant-Revision'],target.connectionRevision)
 response=reference
 assert.equal((await client.authoringReference(request.requestId,purpose)).run.id,run.id)
 assert.equal(calls.at(-1).method,'GET');assert.equal(calls.at(-1).body,undefined)
 response=raw
 assert.equal((await client.start(start,undefined,target)).id,run.id)
 assert.equal((await client.read(run)).id,run.id)
 response=cancelled
 const stopped=await client.cancelAuthoring(start)
 assert.equal(stopped.cancelled,true);assert.equal(stopped.run.id,run.id)
 assert.deepEqual(JSON.parse(calls.at(-1).body),{ownerId:start.ownerId,revision:start.revision})
 assert.equal(calls.at(-1).path,`/api/assistant-authoring/captures/${request.requestId}/cancel`)
 assert(calls.every(call=>call.headers.Authorization==='Bearer synthetic-author-reader-session'))
 for(const patch of [{purpose:'chat'},{purpose:'invented-authoring'},{ownerId:request.requestId},{admissionId:request.requestId},{requestId:'foreign-request'},{revision:'invalid'}]){
  response={...reference,start:{...start,...patch}}
  await assert.rejects(()=>client.authoringReference(request.requestId,purpose))
 }
 response={...reference,unexpectedAuthority:true}
 await assert.rejects(()=>client.authoringReference(request.requestId,purpose))
 response={...reference,run:{...raw,owner:{...raw.owner,id:'author_'+'f'.repeat(32)}}}
 await assert.rejects(()=>client.authoringReference(request.requestId,purpose))
}
console.log('Authoring readers: actual API all three purposes and native capture/reference/cancel, exact identity/source, canonical proposals/reports, closed reader negatives and UTF8 SSE passed.')
