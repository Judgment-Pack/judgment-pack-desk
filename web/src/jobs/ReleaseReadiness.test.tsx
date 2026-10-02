import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { DeskConfigProvider, useDeskConfigRead } from '../config/DeskConfigProvider'
import { testQueryClient } from '../testing/harness'
import { ReleaseReadiness } from './ReleaseReadiness'
import type { Release } from './client'
afterEach(() => { cleanup(); vi.unstubAllGlobals() })
const report = { status: 'passed', summary: { total: 1, passed: 1, mismatched: 0 }, packs: [{ id: 'target', status: 'passed', summary: { total: 1, passed: 1, mismatched: 0 }, rows: [], coverage: [{ probe: 'reason:unknown', status: 'missing', detail: 'No expected unknown result.' }] }] }
const release: Release = { id: 'release', title: 'Intake', packId: 'intake', packVersion: '1', packDigest: 'pack-hash', runtimeDigest: 'runtime-hash', createdAt: '2026-09-25T12:00:00Z', pack: '{}', sample: { facts: {} }, preview: { disposition: { kind: 'outcome', outcomeId: 'accept', reasons: [], handoff: { state: 'none' } } }, tests: 'passed', testEvidence: { status: 'passed', matrix: '{}', matrixDigest: 'hash', packDigest: 'pack-hash', runtimeDigest: 'runtime-hash', checkedAt: '2026-09-25T12:00:00Z', report } }
it('keeps test outcomes and advisory coverage separate, with details collapsed', () => {
 render(<ReleaseReadiness release={release} />)
 expect(screen.getByText('Passed')).toBeTruthy()
 expect(screen.getByText('Saved cases: 1 · 1 passed · 0 failed')).toBeTruthy()
 const disclosure = screen.getByText('Coverage gaps: 1 · advisory').closest('details')!
 expect(disclosure.open).toBe(false)
 fireEvent.click(screen.getByText('Coverage gaps: 1 · advisory'))
 expect(disclosure.open).toBe(true)
 expect(screen.getByText('No expected unknown result.')).toBeTruthy()
})
it('shows incomplete checks without publishing partial counts as a completed result', () => {
 render(<ReleaseReadiness release={{ ...release, tests: 'error', testEvidence: { ...release.testEvidence!, status: 'error', problem: 'The check did not complete.' } }} />)
 expect(screen.getByText('Check incomplete')).toBeTruthy()
 expect(screen.queryByText('Saved cases: 1 · 1 passed · 0 failed')).toBeNull()
 expect(screen.getByText('Runtime report')).toBeTruthy()
})
it('keeps old releases explicitly untested', () => {
 render(<ReleaseReadiness release={{ ...release, tests: 'not-run', testEvidence: undefined }} />)
 expect(screen.getByText('Not run')).toBeTruthy()
 expect(screen.queryByText('Passed')).toBeNull()
 expect(screen.queryByText('Test results')).toBeNull()
})

/** Shown once the configuration query has answered, so a note can be read after it. */
function Answered() { return useDeskConfigRead() ? <span>configuration read</span> : null }
/** The page learns the policy from Desk's own desk-config answer, through the real provider. */
async function renderWithPolicy(jobs: unknown, props: { job?: boolean } = {}) {
 vi.stubGlobal('fetch', async (url: string) => String(url).includes('/api/desk-config')
  ? { ok: true, status: 200, statusText: '', text: async () => JSON.stringify({ path: '/desk.json', present: false, sha256: '', project: { dir: '/p', file: '/p/jpack-desk.json' }, runtime: { bin: 'jpack' }, ...(jobs === undefined ? {} : { jobs }) }) }
  : { ok: false, status: 404, statusText: '', text: async () => JSON.stringify({ error: 'no such file' }) })
 render(<QueryClientProvider client={testQueryClient()}><DeskConfigProvider><ReleaseReadiness release={{ ...release, tests: 'not-run', testEvidence: undefined }} {...props} /><Answered /></DeskConfigProvider></QueryClientProvider>)
 await screen.findByText('configuration read')
}
const unknown = 'No saved tests were run for this release. Review it as untested before creating a job.'
const refuses = 'No saved tests were run for this release, and this installation refuses to create a job from an untested release. Save tests for this pack, then check a new release. To turn this policy off, restart Desk with --runner-require-tested-releases=false.'
const allows = 'No saved tests were run for this release. This installation allows a job from an untested release, because Desk was started with --runner-require-tested-releases=false. Review it as untested before creating a job.'
const keepsRunning = 'No saved tests were run for this release. This job was created before this installation refused untested releases, and it keeps running. A new job needs a release whose saved tests ran and passed.'
it('says this installation refuses a job from an untested release, and how to turn that off', async () => {
 await renderWithPolicy({ requireTestedReleases: true })
 expect(screen.getByText(refuses)).toBeTruthy()
 expect(screen.queryByText(unknown)).toBeNull()
})
it('says the policy is off where Desk was started with it off', async () => {
 await renderWithPolicy({ requireTestedReleases: false })
 expect(screen.getByText(allows)).toBeTruthy()
 expect(screen.queryByText(refuses)).toBeNull()
})
it.each([['no policy', undefined], ['a policy that is not a boolean', { requireTestedReleases: 'true' }]])('claims no policy where Desk stated %s', async (_, jobs) => {
 await renderWithPolicy(jobs)
 expect(screen.getByText(unknown)).toBeTruthy()
 expect(screen.queryByText(refuses)).toBeNull(); expect(screen.queryByText(allows)).toBeNull()
})
it('tells an existing job on an untested release that it keeps running', async () => {
 await renderWithPolicy({ requireTestedReleases: true }, { job: true })
 expect(screen.getByText(keepsRunning)).toBeTruthy()
 expect(screen.queryByText(refuses)).toBeNull()
})
it('does not tell an existing job it was made before the policy where the policy is off', async () => {
 await renderWithPolicy({ requireTestedReleases: false }, { job: true })
 expect(screen.getByText(unknown)).toBeTruthy()
 expect(screen.queryByText(keepsRunning)).toBeNull(); expect(screen.queryByText(allows)).toBeNull()
})
