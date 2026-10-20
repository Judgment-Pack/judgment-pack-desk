// Expectations are hand-authored; inputs come from actual projection/restore.
import assert from 'node:assert/strict'
import {readFile, readdir} from 'node:fs/promises'
import {join} from 'node:path'
import {decodeCheckpoint} from '../web/src/chat/checkpoint.ts'
import {readResponseHistory} from '../web/src/chat/responseHistory.ts'

const directory = process.argv[2]
assert(directory)
const names = (await readdir(directory)).filter(name => name.endsWith('.json')).sort()
assert.deepEqual(names, ['issue-original.json', 'issue-replacement.json', 'projected.json', 'restored.json'])
const expected = {Fact:false, fact:null, zero:0, number:50, literal:'50', rows:[{ID:0, id:false}]}
for (const name of names) {
  const document = JSON.parse(await readFile(join(directory, name), 'utf8'))
  const cp = document.chats.find(chat => chat.id === 'chat-one').checkpoint
  const saved = decodeCheckpoint(cp)
  if (name.startsWith('issue-')) {
    const issue = saved.state.expectationIssues[0]
    assert.equal(issue.id, 'retained-issue', name)
    assert.equal(issue.message, 'review', name)
    const row = name === 'issue-original.json' ? issue.original : issue.resolved.replacement
    assert.deepEqual(row.facts, expected, name)
    assert.equal(row.id, 'issue-case', name)
    assert.equal(row.expectationSource, 'retained', name)
    assert.equal(row.rationale, 'case rationale', name)
    if (issue.resolved) {
      assert.equal(issue.resolved.approvedAt, '2026-10-20T00:00:00Z', name)
      assert.equal(issue.resolved.rationale, 'resolution rationale', name)
      assert.deepEqual(issue.original.facts, {}, name)
    }
    continue
  }
  assert(saved.state.execution, name)
  const op = saved.state.execution.operations[0]
  assert.equal(op.status, 'interrupted', name)
  assert.deepEqual(op.args, expected, name)
  assert.deepEqual(op.result.structuredContent, expected, name)
  assert.equal(saved.state.turns.at(-1).text, 'Retained typed data', name)
  assert.equal(readResponseHistory(cp.state.responses).length, 1, name)
}
console.log(`Case-sensitive history: ${names.length} actual reader observations passed.`)
