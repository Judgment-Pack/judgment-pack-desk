import { afterEach, expect, it, vi } from 'vitest'
import { jobsAPI, JobsRequestError } from './client'
const mocks = vi.hoisted(() => ({ fetch: vi.fn() }))
vi.mock('../files/client', async original => ({ ...await original<typeof import('../files/client')>(), deskFetch: mocks.fetch }))
afterEach(() => { vi.resetAllMocks() })
const answer = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

it('keeps the runner refusal code beside its own words', async () => {
  const message = 'This installation creates jobs only from releases whose saved tests ran and passed. Save tests for this pack, then check a new release.'
  mocks.fetch.mockResolvedValue(answer(409, { error: { code: 'release_untested', message, retryable: false } }))
  const refusal = await jobsAPI('jobs', { name: 'Intake', releaseId: 'release', reviewed: true }).catch(e => e)
  expect(refusal).toBeInstanceOf(JobsRequestError)
  expect(refusal).toMatchObject({ message, status: 409, code: 'release_untested' })
})
it('carries no code for a Desk answer that has none', async () => {
  mocks.fetch.mockResolvedValue(answer(503, { code: 'bad-request', error: 'The local runner is unavailable.' }))
  const refusal = await jobsAPI('jobs').catch(e => e)
  expect(refusal).toBeInstanceOf(JobsRequestError)
  expect(refusal).toMatchObject({ status: 503, code: undefined })
})
