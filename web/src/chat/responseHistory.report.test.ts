import { expect, it } from 'vitest'
import { readRunReport } from '@judgment-pack/harness/runReport'
import { readResponseHistory } from './responseHistory'
import { REPORT1, REPORT2, REPORT_API } from '../assistant/reportFixtures'

const history = (canonical?: unknown) => [{id: 'response-one', afterTurnId: 'turn-one', documents: [], websites: [], sourceIds: [], work: {items: [], notices: [], report: {requests: 0, unit: 'provider-managed', ...(canonical === undefined ? {} : {canonical})}}}]

it.each([REPORT1, REPORT2, REPORT_API])('retains complete report version $version through the actual saved response reader', canonical => {
 const input = JSON.parse(JSON.stringify(history(canonical)))
 const saved = readResponseHistory(input)
 expect(saved[0]?.work.report?.canonical).toEqual(readRunReport(JSON.stringify(canonical)))
 expect(saved[0]?.work.report?.canonical).toEqual(canonical)
 expect(saved[0]?.work.report?.canonical).not.toBe(input[0].work.report.canonical)
})

it('accepts unchanged summary-only history and refuses invalid canonical facts', () => {
 expect(readResponseHistory(history())[0]?.work.report?.canonical).toBeUndefined()
 for (const canonical of [{...REPORT1, version: 9}, {...REPORT1, approval: {}}, {...REPORT1, completions: [{request: 1, phase: 'work', result: {finishReason: 'invented'}}]}]) {
  const input = history(canonical), before = JSON.stringify(input)
  expect(() => readResponseHistory(input)).toThrow()
  expect(JSON.stringify(input)).toBe(before)
 }
})
