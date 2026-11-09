// The Graph fixture runs the actual Go producer and existing terminal writer.
// Read its output through the same browser readers used by chat and Graphs.
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import {validGraphSnapshot,decodeGraphDrafts} from '../web/src/graphs/drafts.ts'
import {decodeCheckpoint} from '../web/src/chat/checkpoint.ts'
const [chatPath,detachedPath]=process.argv.slice(2)
assert(chatPath&&detachedPath,'chat and detached Graph fixture outputs required')
const saved=JSON.parse(await readFile(chatPath,'utf8'))
const chat=saved.chats[0]
assert(decodeCheckpoint(chat.checkpoint))
assert.equal(chat.graphDrafts.length,1)
const draft=chat.graphDrafts[0]
assert(validGraphSnapshot(draft))
assert.equal(draft.id,'flow')
assert.equal(draft.path,'flow.graph.json')
assert(draft.baseSha256)
assert.equal(draft.content,'{\n "formatVersion":"1", "nodes":{"first":{"pack":"one"}}, "edges":[], "fixture":{"missing":null,"flag":false,"zero":0,"literal":"50","number":1.20}\n}')
const detached=decodeGraphDrafts(JSON.parse(await readFile(detachedPath,'utf8')))
assert.equal(detached.drafts.length,1)
assert.equal(detached.drafts[0].draftId,draft.draftId)
assert.equal(detached.drafts[0].content,draft.content)
assert.equal(detached.drafts[0].originChatId,chat.id)
console.log('Graph shared readers: exact producer draft and detached deletion retention passed.')
