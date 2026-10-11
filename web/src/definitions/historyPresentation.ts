import { msg } from '../i18n'
import type { TestRun } from '../packs/test-workspace/model'

export interface HistoryEntry {
  id: string
  kind: 'changes' | 'checks' | 'briefs' | 'decisions'
  title: string
  detail: string
  at?: string
  raw: string
  digest?: string
  note?: string
  facts?: {label: string; value: string}[]
  sections?: {title: string; text: string}[]
}

/** Describe the saved report. Never infer a pass from missing or unknown results. */
export function testHistoryEntry(run: TestRun): HistoryEntry {
  let detail = msg('No result recorded')
  if (run.error) detail = msg('Could not complete')
  else if (run.report?.status === 'passed') detail = msg('Expected outcomes matched')
  else if (run.report?.status === 'mismatch') detail = msg('Results differed from expectations')
  else if (run.report?.status === 'skipped') detail = msg('Tests were skipped')
  else if (run.report) detail = msg('See saved test results')
  else if (run.trial) detail = msg('Trial run · no expectation comparison')
  const facts: NonNullable<HistoryEntry['facts']> = []
  if (run.packVersion) facts.push({label:msg('Pack version'),value:run.packVersion})
  if (run.cases.length) facts.push({label:msg('Saved test cases'),value:String(run.cases.length)})
  return {
    id: 'test:' + run.id, kind: 'checks', title: msg('Test run recorded'), detail,
    at: run.at, raw: JSON.stringify(run,null,2), digest: run.packDigest, facts,
    note: msg('This result applies to the pack and inputs saved at that time. See Review for the current status.')
  }
}
