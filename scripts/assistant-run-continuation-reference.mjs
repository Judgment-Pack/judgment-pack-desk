// Handwritten assertions over ephemeral actual Go output; no recorded goldens.
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { decodeSnapshot, parseRunJSON, pendingQuestion } from '../web/src/assistant/serverRuns/decode.ts'
import { readSavedQuestions } from '../web/node_modules/@judgment-pack/harness/dist/questions.js'
const dir=process.argv[2]
for(const name of ['continuation-answer','continuation-extend-budget','actual-continuation-answer','actual-continuation-extend-budget','actual-continuation-subscription']){
 const raw=parseRunJSON(await readFile(join(dir,name+'.json'),'utf8'))
 const value=decodeSnapshot(raw,{owner:raw.owner,requestId:raw.requestId,id:raw.id})
 assert.equal(value.state,'complete');assert.equal(value.continuations.length,1)
 assert.equal(value.checkpoint.status,'complete');assert.equal(pendingQuestion(value),undefined)
 const receipt=value.continuations[0]
 if(receipt.kind==='answer'){
  assert.deepEqual(receipt.answer.selected,['two']);assert.equal(receipt.answer.text,'Exact answer <&> 한글\nnext line')
  assert.equal(value.checkpoint.grants.length,0)
 }else{assert.deepEqual(receipt.grant,value.checkpoint.grants.at(-1));assert.equal(receipt.grant.to-receipt.grant.from,10)}
 if(name.endsWith('subscription')){assert.equal(value.checkpoint.unit,'provider-managed');assert.equal(value.checkpoint.used,0);assert.equal(value.checkpoint.limit,1)}
}
const chat=parseRunJSON(await readFile(join(dir,'actual-continuation-projected-chat.json'),'utf8'))
const saved=readSavedQuestions(chat.checkpoint.state.questions)
assert.equal(saved.length,1);assert.equal(saved[0].resolution.kind,'answered');assert.equal(saved[0].resolution.continued,true)
assert.deepEqual(saved[0].draft,{selected:['two'],text:'Exact answer <&> 한글\nnext line'})
assert.equal(chat.checkpoint.state.turns.find(t=>t.id===saved[0].resolution.turnId).role,'user')
console.log('Run continuation readers: actual API/native answers, canonical budget receipt, completed checkpoints and saved-question reload passed.')
