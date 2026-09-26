import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Tooltip } from 'radix-ui'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { CreateJobContent } from './JobsView'
import { jobsAPI } from './client'
import { readReleaseTests } from './releaseTests'
vi.mock('./client', () => ({ jobsAPI: vi.fn() }))
vi.mock('./releaseTests', () => ({ readReleaseTests: vi.fn() }))
const { pack, packs } = vi.hoisted(() => ({
 pack: { data: { raw: '{"version":"1"}', document: { title: 'Example pack', version: '1' } }, refetch: vi.fn() },
 packs: { data: { packs: [{ id: 'pack', matrixPath: 'cases.json' }] }, refetch: vi.fn() },
}))
vi.mock('../mcp/queries', () => ({ usePacks: () => packs, usePack: () => pack }))
const source = { packKey: 'pack', suiteRevision: 1, caseNames: { clean: 'Clean case' }, exploratoryCount: 0 }
const saved = { project: 'project', matrix: '{"matrixVersion":"3","cases":[{"id":"clean"}]}', testSource: source }
const release = { id: 'release', pack: '{"version":"1"}', packVersion: '1', tests: 'passed', preview: { disposition: { kind: 'outcome', outcomeId: 'accept', reasons: [], handoff: { state: 'none' } } } }
beforeEach(() => {
 pack.refetch.mockResolvedValue(pack); packs.refetch.mockResolvedValue(packs)
 vi.mocked(readReleaseTests).mockResolvedValue(saved)
 vi.mocked(jobsAPI).mockResolvedValue(release)
})
afterEach(() => { cleanup(); vi.resetAllMocks() })
function renderCreate() { return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}><Tooltip.Provider><MemoryRouter initialEntries={['/jobs/new?pack=pack']}><CreateJobContent /></MemoryRouter></Tooltip.Provider></QueryClientProvider>) }
async function check() {
 fireEvent.change(screen.getByLabelText('Job name'), { target: { value: 'Intake' } })
 fireEvent.click(screen.getByRole('button', { name: 'Check release' }))
 await screen.findByText('Review this release')
}
const button = () => screen.getByRole('button', { name: 'Create job' }) as HTMLButtonElement
const review = () => screen.getByLabelText('I reviewed this release, its test status and sample result.') as HTMLInputElement
it('runs saved expectations against the current pack and requires explicit review, invalidating it when sample inputs change', async () => {
 renderCreate(); expect(button().disabled).toBe(true)
 await check()
 expect(jobsAPI).toHaveBeenCalledWith('previews', { pack: pack.data.raw, input: { facts: {} }, matrix: saved.matrix, testSource: source })
 expect(readReleaseTests).toHaveBeenCalledWith('pack', 'cases.json')
 expect(button().disabled).toBe(true)
 fireEvent.click(review()); expect(button().disabled).toBe(false)
 fireEvent.change(screen.getByLabelText('Facts (JSON)'), { target: { value: '{"changed":true}' } })
 expect(button().disabled).toBe(true); expect(screen.queryByText('Review this release')).toBeNull()
 expect(jobsAPI).toHaveBeenCalledTimes(1)
})
it('rejects malformed sample input before invoking the runner and preserves omitted evidence', async () => {
 renderCreate()
 fireEvent.change(screen.getByLabelText('Facts (JSON)'), { target: { value: '{' } })
 fireEvent.click(screen.getByRole('button', { name: 'Check release' }))
 await screen.findByRole('alert'); expect(jobsAPI).not.toHaveBeenCalled()
 fireEvent.change(screen.getByLabelText('Facts (JSON)'), { target: { value: '{"active":false}' } })
 fireEvent.click(screen.getByRole('button', { name: 'Check release' }))
 await waitFor(() => expect(jobsAPI).toHaveBeenCalledWith('previews', expect.objectContaining({ input: { facts: { active: false } } })))
})
it.each(['failed','error'])('blocks creating a job after a %s check', async tests => {
 vi.mocked(jobsAPI).mockResolvedValue({ ...release, tests })
 renderCreate(); await check()
 expect(review().disabled).toBe(true); expect(button().disabled).toBe(true)
 expect(screen.getByText('Correct the pack or saved cases, then check a new release before creating a job.')).toBeTruthy()
})
it('allows an explicitly reviewed untested release without calling it passed', async () => {
 vi.mocked(readReleaseTests).mockResolvedValue({ ...saved, matrix: undefined })
 vi.mocked(jobsAPI).mockResolvedValue({ ...release, tests: 'not-run' })
 renderCreate(); await check()
 expect(screen.getByText('Not run')).toBeTruthy(); expect(screen.queryByText('Passed')).toBeNull()
 expect(jobsAPI).toHaveBeenCalledWith('previews', { pack: pack.data.raw, input: { facts: {} } })
 expect(button().disabled).toBe(true); fireEvent.click(review()); expect(button().disabled).toBe(false)
})
it.each(['tests','pack','project'])('rechecks %s before creating and refuses a stale review', async changed => {
 renderCreate(); await check(); fireEvent.click(review())
 if (changed === 'pack') pack.refetch.mockResolvedValue({ data: { ...pack.data, raw: '{"version":"2"}' } })
 else vi.mocked(readReleaseTests).mockResolvedValue({ ...saved, ...(changed === 'tests' ? { matrix: 'changed' } : { project: 'other' }) })
 fireEvent.click(button())
 await screen.findByText('The pack or saved tests changed. Check the release again before creating the job.')
 expect(jobsAPI).toHaveBeenCalledTimes(1); expect(button().disabled).toBe(true)
})
it('does not silently release untested when reading saved tests fails', async () => {
 vi.mocked(readReleaseTests).mockRejectedValue(new Error('Cannot read saved tests'))
 renderCreate(); fireEvent.click(screen.getByRole('button', { name: 'Check release' }))
 await screen.findByText('Cannot read saved tests'); expect(jobsAPI).not.toHaveBeenCalled(); expect(button().disabled).toBe(true)
})
it('creates the immutable reviewed release after checking freshness again', async () => {
 renderCreate(); await check(); fireEvent.click(review())
 vi.mocked(jobsAPI).mockResolvedValue({ id: 'job-id' })
 fireEvent.click(button())
 await waitFor(() => expect(jobsAPI).toHaveBeenCalledWith('jobs', { name: 'Intake', releaseId: 'release', reviewed: true }))
 expect(readReleaseTests).toHaveBeenCalledTimes(2)
})
