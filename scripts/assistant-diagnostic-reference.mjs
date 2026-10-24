import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import {join} from 'node:path'
import {decodeSnapshot} from '../web/src/assistant/serverRuns/decode.ts'
import vector from '../web/src/assistant/serverRuns/fixtures/diagnostic.json'
assert.equal(decodeSnapshot(vector,{owner:vector.owner,requestId:vector.requestId}).owner.kind,'connection-diagnostic')
const read=async name=>{const raw=JSON.parse(await readFile(join(process.argv[2],name),'utf8'));return decodeSnapshot(raw,{id:raw.id,owner:raw.owner,requestId:raw.requestId})}
const run=await read('diagnostic-actual.json')
assert.equal(run.owner.kind,'connection-diagnostic');assert.equal(run.state,'complete')
assert.equal(run.checkpoint.unit,'provider-managed');assert.equal(run.checkpoint.used,0);assert.equal(run.checkpoint.limit,1)
assert.equal(run.events.filter(e=>e.event.type==='tool_result'&&e.event.name==='get_schema'&&!e.event.isError).length,1)
assert.equal(run.events.some(e=>['proposal','user_question','approval_requested','delegation_requested','error'].includes(e.event.type)),false)
const restored=await read('diagnostic-restored.json')
assert.equal(restored.historical,true);assert.deepEqual(restored.checkpoint,run.checkpoint);assert.deepEqual(restored.operations,run.operations)
for(const mode of ['prose','unoffered','question','budget','malformed','duplicate']){
 const refused=await read(`diagnostic-refused-${mode}.json`)
 assert.equal(refused.state,'stopped');assert.equal(refused.events.some(e=>['proposal','user_question','approval_requested','delegation_requested'].includes(e.event.type)),false)
}
console.log('Native diagnostic readers: real bridge/schema/checkpoint, provider-managed 0/limit 1, strict owner, historical restore and six refusal producers passed.')
