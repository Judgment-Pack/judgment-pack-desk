import { beforeEach, expect, it, vi } from 'vitest'
import { readReleaseTests } from './releaseTests'
import { readTests } from '../packs/test-workspace/store'
import { readFile } from '../files/client'
import { emptySuite, newCase, type TestSuite } from '../packs/test-workspace/model'
vi.mock('../packs/test-workspace/store', () => ({ readTests: vi.fn() }))
vi.mock('../files/client', () => ({ readFile: vi.fn() }))
const expected = { kind: 'outcome', outcomeId: 'accept', reasons: [], handoff: { state: 'none' } }
const one = { ...newCase(), id: 'one', row: { id: 'one', facts: { active: false }, expectedDisposition: expected } }
let suite: TestSuite
beforeEach(() => { vi.resetAllMocks(); suite = { ...emptySuite(), cases: [one] }; vi.mocked(readTests).mockImplementation(async () => ({ project: 'project', sha256: 'hash', content: { version: 1, suites: { pack: suite } } })) })
it('freezes current saved expectations, excluding exploratory cases, proposals and prior reports', async () => {
 suite.cases.push({ ...newCase(), id: 'exploratory' })
 const held = await readReleaseTests('pack')
 expect(JSON.parse(held.matrix!).cases).toEqual([expect.objectContaining({ id: 'one', facts: { active: false } })])
 expect(held.testSource.exploratoryCount).toBe(1)
 expect(suite.cases).toHaveLength(2)
})
it('reads the registered matrix before its first Tests visit and respects saved edits and deletions', async () => {
 suite.deleted = ['deleted']
 vi.mocked(readFile).mockResolvedValue({ content: JSON.stringify({ matrixVersion: '3', cases: [
  { ...one.row, facts: { stale: true } }, { ...one.row, id: 'two' }, { ...one.row, id: 'deleted' },
 ] }), sha256: 'hash' } as Awaited<ReturnType<typeof readFile>>)
 const held = await readReleaseTests('pack', 'matrix.json')
 const rows = JSON.parse(held.matrix!).cases
 expect(rows.map((r: { id: string }) => r.id)).toEqual(['one','two'])
 expect(rows[0].facts).toEqual({ active: false }); expect(suite.cases).toHaveLength(1)
})
it('uses the authoring store after import instead of resurrecting removed file cases', async () => {
 suite.recovered.push('matrix:matrix.json')
 await readReleaseTests('pack', 'matrix.json'); expect(readFile).not.toHaveBeenCalled()
})
it('reports no matrix for zero saved expectations', async () => {
 suite.cases = [newCase()]
 expect((await readReleaseTests('pack')).matrix).toBeUndefined()
})
it('fails on unreadable or invalid registered cases rather than silently omitting tests', async () => {
 vi.mocked(readFile).mockRejectedValueOnce(Error('Unreadable'))
 await expect(readReleaseTests('pack','matrix.json')).rejects.toThrow('Unreadable')
 vi.mocked(readFile).mockResolvedValueOnce({ content: '{' } as Awaited<ReturnType<typeof readFile>>)
 await expect(readReleaseTests('pack','matrix.json')).rejects.toThrow()
})
