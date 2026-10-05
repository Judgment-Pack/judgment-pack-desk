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
it('keeps the status of a refusal whose body is not JSON, as an earlier Runner answers a route it does not serve', async () => {
  mocks.fetch.mockResolvedValue(new Response('404 page not found\n', { status: 404, headers: { 'Content-Type': 'application/json' } }))
  const refusal = await jobsAPI(`jobs/job_${'a'.repeat(32)}/events?after=0`).catch(e => e)
  expect(refusal).toBeInstanceOf(JobsRequestError)
  expect(refusal).toMatchObject({ status: 404, message: 'The local runner could not complete this request.' })
  // An answer that is not JSON is still no answer when the status says it is one.
  mocks.fetch.mockResolvedValue(new Response('not json', { status: 200 }))
  expect(await jobsAPI('jobs').catch(e => e)).toBeInstanceOf(SyntaxError)
})
it('carries no code for a Desk answer that has none', async () => {
  mocks.fetch.mockResolvedValue(answer(503, { code: 'bad-request', error: 'The local runner is unavailable.' }))
  const refusal = await jobsAPI('jobs').catch(e => e)
  expect(refusal).toBeInstanceOf(JobsRequestError)
  expect(refusal).toMatchObject({ status: 503, code: undefined })
})
