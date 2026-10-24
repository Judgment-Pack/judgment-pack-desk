// Expectations are handwritten. Inputs are temporary actual API/native output.
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import {join} from 'node:path'
import {decodeSnapshot,parseRunJSON} from '../web/src/assistant/serverRuns/decode.ts'
import {readFrames,RunObservations} from '../web/src/assistant/serverRuns/stream.ts'
import {savedBrief} from '../web/src/briefs/serverGeneration.ts'
const dir=process.argv[2]
assert(dir,'actual brief producer output is required')
const text={context:'Frozen context <&> 한글',findings:'Historical findings.',uncertainty:'Evidence remains unverified.',nextAction:'Review the saved sources.'}
const cases=[['api-pack-one','pack:one','pack'],['api-graph-flow','graph:flow','graph'],['api-pack-draft-draft-one','pack-draft:draft-one','pack'],['api-graph-draft-draft-one','graph-draft:draft-one','graph'],['api-case-one-case-one','case:one:case-one','case'],['native-pack-one','pack:one','pack'],['runner-job',`job:job_${'1'.repeat(32)}`,'job'],['runner-run',`run:run_${'1'.repeat(32)}`,'run']]
for(const [name,subject,kind] of cases){
 const {start,run:raw,receipt}=parseRunJSON(await readFile(join(dir,name+'.json'),'utf8'))
 assert.equal(start.purpose,'brief');assert.equal(start.ownerId,subject)
 assert.match(start.requestId,/^[a-f0-9]{32}$/);assert.match(start.revision,/^[a-f0-9]{64}$/)
 assert.equal(start.admissionId,`brief-${start.requestId}`)
 const binding={owner:{kind:'brief',id:subject,revision:start.revision},requestId:start.requestId}
 const run=decodeSnapshot(raw,binding)
 assert.equal(run.state,'complete');assert.equal(run.checkpoint.status,'complete')
 assert.equal(run.checkpoint.unit,name.startsWith('native-')?'provider-managed':'model-requests')
 assert.equal(run.operations.length,1);assert(run.operations.every(op=>op.kind==='model'&&op.state==='completed'))
 const proposal=run.events.filter(event=>event.event.type==='proposal')
 assert.equal(proposal.length,1);assert.deepEqual(proposal[0].event.document,text)
 assert(!run.events.some(event=>['tool_call','user_question','approval_requested','delegated_task','critique'].includes(event.event.type)))
 const saved=savedBrief(receipt,run).content.subjects[subject].revisions
 assert.equal(saved.length,1);assert.equal(saved[0].id,start.requestId)
 assert.equal(saved[0].snapshot.kind,kind);assert.equal(saved[0].model,'shared-model');assert.deepEqual(saved[0].text,text)
 assert.throws(()=>decodeSnapshot(raw,{...binding,owner:{...binding.owner,revision:'f'.repeat(64)}}))
 assert.throws(()=>decodeSnapshot(raw,{...binding,owner:{...binding.owner,kind:'chat'}}))
 const observed=new RunObservations(run)
 const response=new Response(await readFile(join(dir,name+'.sse')))
 for await(const frame of readFrames(response.body))observed.accept(frame)
 assert.deepEqual(observed.snapshot,run,'actual server event replay must converge on the same complete run')
}
console.log('Brief readers: eight actual API/native/Runner subject observations, exact reservation/source metadata, four sections, saved receipts and UTF8 SSE passed.')
