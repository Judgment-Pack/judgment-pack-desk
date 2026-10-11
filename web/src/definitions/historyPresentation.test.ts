import { expect, it } from 'vitest'
import { testHistoryEntry } from './historyPresentation'
import type { TestRun } from '../packs/test-workspace/model'
const run:TestRun={id:'run',at:'2026-10-11T10:00:00Z',packText:'{}',packDigest:'fingerprint',packVersion:'1',cases:[],origin:'tests'}
it('distinguishes a recorded mismatch, an exploratory trial and a missing result', () => {
  const mismatch=testHistoryEntry({...run,report:{status:'mismatch',summary:{total:2,passed:1,mismatched:1}}})
  expect(mismatch.detail).toBe('Results differed from expectations')
  expect(mismatch.raw).toContain('"status": "mismatch"')
  expect(testHistoryEntry({...run,trial:{at:run.at,documentDigest:run.packDigest,facts:{},disposition:{kind:'outcome',outcomeId:'approve'}}}).detail).toContain('no expectation comparison')
  expect(testHistoryEntry(run).detail).toBe('No result recorded')
  expect(testHistoryEntry({...run,report:{status:'new-status',summary:{total:1,passed:1,mismatched:0}}}).detail).toBe('See saved test results')
})
it('does not present an incomplete run with a passing report as a successful run', () => {
  expect(testHistoryEntry({...run,error:'Interrupted',report:{status:'passed',summary:{total:1,passed:1,mismatched:0}}}).detail).toBe('Could not complete')
})
