// Offline shared-reader probe. Inputs are hand-authored by the Go fixtures;
// expected display behavior is asserted here, never recorded from a reader run.
import assert from 'node:assert/strict'
import { readFile, readdir } from 'node:fs/promises'
import { join } from 'node:path'
import { readExecution } from '../web/node_modules/@judgment-pack/harness/dist/execution/state.js'
import { decodeCheckpoint } from '../web/src/chat/checkpoint.ts'
import { readResponseHistory } from '../web/src/chat/responseHistory.ts'

const directory = process.argv[2]
assert(directory, 'projection fixture output directory required')
const names = (await readdir(directory)).filter(name => name.endsWith('.json'))
assert.equal(names.length, 6)
const prompts = [{request: 41, sha256: 'a'.repeat(64)}, {request: 43, sha256: '0123456789abcdef'.repeat(4)}]
const originalCandidate = '{ "Existing": false, "Missing":null, "zero":0, "literal":"50" }'
for (const name of names) {
  const document = JSON.parse(await readFile(join(directory, name), 'utf8'))
  const chat = document.chats.find(chat => chat.id === 'chat-one')
  const saved = decodeCheckpoint(chat.checkpoint)
  assert.equal(saved.state.candidates[0].text, originalCandidate, name)
  assert.equal(saved.state.turns[0].input, '{ "Fact":null, "enabled":false, "zero":0, "literal":"50" }', name)
  const responses = readResponseHistory(chat.checkpoint.state.responses)
  if (responses.length > 0) {
    assert.deepEqual(saved.state.execution.prompts, prompts, name)
    assert.deepEqual(responses[0].work.report.prompts, prompts, name)
  }
  if (name === 'completed.json') {
    assert.equal(saved.state.execution.used, 2)
    assert.equal(responses[0].work.report.requests, 2)
    assert.equal(saved.state.turns.at(-1).text, 'Exact completed answer.')
    assert.equal(responses.length, 1)
    assert.equal(responses[0].messageId, saved.state.turns.at(-1).id)
  } else if (name === 'failure-before-prose.json') {
    assert.equal(saved.state.turns.length, 1)
    assert.equal(saved.state.failure.code, 'timeout')
    assert.equal(saved.state.checkRun, 'complete')
    assert.equal(responses[0].afterTurnId, 'message-one')
    assert.equal(responses[0].work.critique, 'Reviewed the retained cases.')
    assert.deepEqual(responses[0].work.critic, {compared:0, disagreed:0, incomplete:['case-one'], figures:{model:'critic-fixture',unit:'model-requests',requests:1}, runModel:'shared-model'})
    assert.deepEqual(responses[0].work.items.map(item => [item.id, item.status]), [['same:1', 'complete'], ['same:2', 'failed']])
  } else if (name === 'create.json') {
    assert.equal(saved.state.candidates.at(-1).text, '{"Count":0,"Enabled":false,"Fact":null,"Literal":"50"}')
    assert.equal(saved.state.candidates.at(-1).responseId, responses[0].id)
  } else {
    assert.deepEqual(readExecution(document.pendingExecution).prompts, prompts, name)
    assert.equal(responses.length, 0, 'a pending continuation must retain the pinned source chat')
    assert.equal(saved.state.turns.length, 1)
  }
}
console.log(`Shared chat readers: ${names.length} completed, work/failure, create and pinned-pause fixtures passed.`)
