import { afterEach, expect, it, vi } from 'vitest'
import { canonicalJobsRequest, jobsAPI, JobsRequestError } from './client'
import { canonicalize } from '../dialect/canon'
import { deskMappings } from '../dialect/__fixtures__/desk-mappings'
import { jobsRequest, RetainedJSON } from './wire'
const mocks = vi.hoisted(() => ({ fetch: vi.fn() }))
vi.mock('../files/client', async original => ({ ...await original<typeof import('../files/client')>(), deskFetch: mocks.fetch }))
afterEach(() => { vi.resetAllMocks(); vi.unstubAllGlobals() })
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

it('canonicalizes nested preview and constant-trigger mappings while preserving signed numbers and case data', () => {
  const mapping = deskMappings['case-score'], canonical = canonicalize(mapping)
  const raw = '{"result":{"n":9007199254740993,"decimal":1.0000,"negative":-0},"receipt":{}}'
  const source = {mapping,case:{mapping:{version:2,tag:'case data'}},sources:{records:{response:new RetainedJSON(raw)}}}
  const input = canonicalJobsRequest({input:{source}})
  expect(input).toContain('"mapping":' + canonical)
  expect(input).toContain(raw)
  expect(JSON.parse(input).input.source.case).toEqual(source.case)
  const constant = jobsRequest({source})
  for (const body of [
    {input:{kind:'constant',value:constant}},
    {config:{input:{kind:'constant',value:constant}}},
    {trigger:{input:{kind:'constant',value:constant}}},
  ]) {
    const output = JSON.parse(canonicalJobsRequest(body))
    const value = (output.input ?? output.config?.input ?? output.trigger.input).value
    expect(value).toContain('"mapping":' + canonical)
    expect(value).toContain(raw)
  }
})

it('leaves version-1 mappings and non-mapping requests byte for byte as before', () => {
  for (const provider of ['local-file','google-drive']) {
    const body = {source:{mapping:{version:1,provider,facts:[],evidence:[]},snapshot:{}}}
    expect(canonicalJobsRequest(body)).toBe(jobsRequest(body))
  }
  const input = {facts:{mapping:{version:2,z:1,a:2}}}
  expect(canonicalJobsRequest(input)).toBe(jobsRequest(input))
})

it('refuses a constant mapping with an exponent before fetch, while retaining non-mapping number literals', async () => {
  await expect(jobsAPI('jobs/job/triggers', {input:{kind:'constant',value:'{"source":{"mapping":{"version":2,"maxAge":1e2}}}'}})).rejects.toThrow(/integer_literal/)
  expect(mocks.fetch).not.toHaveBeenCalled()
  expect(JSON.parse(canonicalJobsRequest({input:{kind:'constant',value:'{"facts":{"n":1.0}}'}})).input.value).toBe('{"facts":{"n":1.0}}')
})

it('splices only the mapping bytes of a constant trigger with escaped member names and retained whitespace', () => {
  const before = ' { "other": [ { "s": "}\\\"" } ], "sour\\u0063e" : { "case": {"zero": -0}, "mapping" : '
  const after = ', "sources" : { "records": { "response": { "result": 1.000, "text": "\\u0026" } } } } } '
  const mapping = '{ "version": 2, "case": {"facts":[],"evidence":[]} }'
  const body = {config:{input:{kind:'constant',value:before+mapping+after}}}
  expect(JSON.parse(canonicalJobsRequest(body)).config.input.value).toBe(before+canonicalize(JSON.parse(mapping))+after)
  const untouched = {input:{kind:'constant',value:' { "facts": { "n": 1.0 } } '}}
  expect(canonicalJobsRequest(untouched)).toBe(jobsRequest(untouched))
})

it('canonicalizes named mappings without re-encoding signed source payloads',async()=>{
 const mapping={formatVersion:'3.0.0',id:'urn:test',version:'1.0.0',case:{facts:[],evidence:[]}}
 const raw='{"source":{"mapping":'+JSON.stringify(mapping)+',"case":{},"sources":{"a":{"response":{"result":{"exact":1.000},"receipt":{}}}}}}'
 const {canonicalJobsRequest}=await import('./client')
 const encoded=canonicalJobsRequest({input:{kind:'constant',value:raw}})
 const inner=JSON.parse(encoded).input.value
 expect(inner).toContain('"exact":1.000')
 expect(inner).toContain('"formatVersion":"3.0.0"')
 expect(inner).toContain('"version":"1.0.0"')
 expect(inner).not.toContain('"provider":""')
})
it('shows a refused binding to a set-aside profile as Desk\'s message, naming the profile and Runner\'s reason only', async () => {
  const reason = 'the input profile old-pages: an endpointScope entry is not an endpoint prefix in the constrained page form, nor the wildcard https://* (input-malformed)'
  mocks.fetch.mockResolvedValue(answer(422, { error: { code: 'input_profile_set_aside', message: 'Forwarded wording.', retryable: false, profile: 'old-pages', reason } }))
  const refusal = await jobsAPI('inputs/next', { source: {} }).catch(e => e)
  expect(refusal).toBeInstanceOf(JobsRequestError)
  expect(refusal).toMatchObject({ status: 422, code: 'input_profile_set_aside', message: 'The input profile old-pages was set aside by Runner and cannot be bound. Runner’s reason: an endpointScope entry is not an endpoint prefix in the constrained page form, nor the wildcard https://*. Choose another profile for this source, or upgrade the job to a release that does not name it.' })
  // The profile is named once; Runner's code is not shown.
  expect((refusal as Error).message.match(/\bold-pages\b/g)).toHaveLength(1)
  expect((refusal as Error).message).not.toContain('(input-malformed)')
  mocks.fetch.mockResolvedValue(answer(422, { error: { code: 'input_profile_set_aside', message: 'Forwarded wording.', retryable: false, profile: 'old-pages' } }))
  expect(await jobsAPI('jobs', {}).catch(e => e)).toMatchObject({ message: 'The input profile old-pages was set aside by Runner and cannot be bound. Choose another profile for this source, or upgrade the job to a release that does not name it.' })
  // Where Desk could name no profile, its own message is shown as it sent it.
  mocks.fetch.mockResolvedValue(answer(422, { error: { code: 'input_profile_set_aside', message: 'An input profile this binding names was set aside by Runner and cannot be bound.', retryable: false } }))
  expect(await jobsAPI('jobs', {}).catch(e => e)).toMatchObject({ message: 'An input profile this binding names was set aside by Runner and cannot be bound.' })
})
