// Hand-authored Unicode expectations for the actual saved checkpoint readers.
import assert from 'node:assert/strict'
import {readFile, readdir} from 'node:fs/promises'
import {join} from 'node:path'
import {decodeCheckpoint} from '../web/src/chat/checkpoint.ts'
import {readResponseHistory} from '../web/src/chat/responseHistory.ts'

const [directory, mode = 'positive'] = process.argv.slice(2)
assert(directory)
const names = (await readdir(directory)).filter(name => name.endsWith('.json'))
assert.equal(names.length, mode === 'omission' ? 5 : 6)
const existing = String.raw` { "Existing":"café 🙂", "pair":"\ud83d\ude42", "literal":"\\ud800" } `
for (const name of names) {
 const doc = JSON.parse(await readFile(join(directory, name), 'utf8'))
 const cp = doc.chats.find(chat => chat.id === 'chat-one').checkpoint
 if (mode === 'omission') {
  if (name.startsWith('proposal-')) {
   assert.throws(() => decodeCheckpoint(cp), /unpaired surrogate escape/, name)
  } else {
   const saved = decodeCheckpoint(cp)
   const op = saved.state.execution.operations[0]
   const value = name.startsWith('checkpoint-args') ? op.args.payload.value : op.result.structuredContent.values[0]
   assert.equal(value.charCodeAt(0), name.startsWith('checkpoint-args') ? 0xd800 : 0xdc00, name)
   // The next server admission's existing scalar validator rejects these raw
   // escapes even though this browser reader accepts the resulting code unit.
  }
 } else {
  const saved = decodeCheckpoint(cp)
  const responses = readResponseHistory(cp.state.responses)
  if (name === 'valid-unicode.json') {
   assert.equal(responses.length, 1)
   assert.equal(saved.state.candidates[1].text, existing)
   const proposal = JSON.parse(saved.state.candidates.at(-1).text).Payload.nested
   assert.deepEqual(proposal, {'🙂':'café', value:'🙂', literal:String.raw`\ud800`, replacement:'�'})
   for (const value of [saved.state.execution.operations[0].args, saved.state.execution.operations[0].result.structuredContent]) {
    assert.deepEqual(value.nested, proposal)
   }
   assert.equal(saved.state.turns.at(-1).text, 'café 🙂 �')
  } else {
   assert.equal(responses.length, 0, name)
   assert.equal(saved.state.turns.length, 1, name)
   assert.equal(saved.state.candidates.length, 1, name)
  }
 }
}
console.log(`Unicode ${mode}: ${names.length} actual reader observations passed.`)
