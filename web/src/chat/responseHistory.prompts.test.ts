import { expect, it } from 'vitest'
import { readExecution } from '@judgment-pack/harness/execution/state'
import { checkpoint } from '../assistant/reportFixtures'
import { readResponseHistory, summarizeWork } from './responseHistory'

it.each(['paused', 'stopped', 'complete'] as const)('retains exact prompt rows across %s history save and reload without aliases', (status) => {
  const prompts = [{request: 401, sha256: 'a'.repeat(64)}, {request: 403, sha256: 'b'.repeat(64)}]
  const execution = readExecution(checkpoint({status, used: 1, limit: 1, unit: 'provider-managed', prompts}))!
  const original = [{id: 'response-one', afterTurnId: 'message-one', documents: [], websites: [], sourceIds: [], work: summarizeWork([], false, {execution})}]
  const stored = JSON.parse(JSON.stringify(original))
  const restored = readResponseHistory(stored)
  expect(restored[0]!.work.report!.prompts).toEqual(prompts)
  restored[0]!.work.report!.prompts![0]!.sha256 = 'c'.repeat(64)
  expect(stored[0].work.report.prompts).toEqual(prompts)
  expect(execution.prompts).toEqual(prompts)
  expect(original[0]!.work.report!.prompts).toEqual(prompts)
})

it('reads old history without inventing rows and refuses malformed rows with fixed prose', () => {
  const row = {id: 'response-one', documents: [], websites: [], sourceIds: [], work: {items: [], notices: [], report: {requests: 1, unit: 'model-requests'}}}
  expect(readResponseHistory([row])[0]!.work.report!.prompts).toBeUndefined()
  expect(() => readResponseHistory([{...row, work: {...row.work, report: {...row.work.report, prompts: [{request: 1, sha256: 'private prompt text'}]}}}])).toThrow('Saved message details are invalid. History was left untouched.')
})

it.each(['\n', '\r', '\u2028', '\u2029'])('refuses a saved digest with a trailing line separator without modifying the input', (separator) => {
  const row = {id: 'response-one', documents: [], websites: [], sourceIds: [], work: {items: [], notices: [], report: {requests: 1, unit: 'model-requests', prompts: [{request: 41, sha256: 'a'.repeat(64) + separator}]}}}
  const before = JSON.stringify(row)
  expect(() => readResponseHistory([row])).toThrow('Saved message details are invalid. History was left untouched.')
  expect(JSON.stringify(row)).toBe(before)
})
