import {afterEach, expect, it, vi} from 'vitest'
import {decodeStoragePolicySnapshot, editStorageBudgetPolicy, prepareStorageBudgetPolicy, readStorageBudgetPolicy, STORAGE_POLICY_MAX_BYTES} from './storageBudgetPolicy'
import {storagePolicyFixture} from './storageBudgetPolicy.fixture'
const fetch = vi.hoisted(() => vi.fn())
vi.mock('../files/client', () => ({deskFetch: fetch}))
afterEach(() => fetch.mockReset())
it('preserves full snapshot decimal values without float conversion', () => {
  const v = storagePolicyFixture()
  expect(decodeStoragePolicySnapshot(v)).toEqual(v)
  expect(v.installationLimits.total.bytes).toBe('9007199254740993')
})
it('rejects malformed closed snapshot and nested limits', () => {
  const changes: ((v: Record<string, unknown>) => void)[] = [
    v => {v.path = '/foreign'}, v => {v.epoch = '01'}, v => {v.sequence = 9007199254740993}, v => {v.revision = '18446744073709551616'},
    v => {v.workspace = null}, v => {v.defaultLimits = {total: {bytes: '1', files: '1', directories: '1'}, categories: []}},
    v => {v.installation = '00000000000000000000000000000000'}, v => {v.observationMaxAgeNanos = '9223372036854775808'},
  ]
  for (const change of changes) {const v = JSON.parse(JSON.stringify(storagePolicyFixture())); change(v); expect(() => decodeStoragePolicySnapshot(v)).toThrow()}
})
it('sends exact frozen expected snapshot and explicit one-time workspace values', async () => {
  const expected = storagePolicyFixture(), limits = expected.defaultLimits
  fetch.mockResolvedValue(Response.json({...expected, workspace: {...expected.workspace, limits}}))
  await editStorageBudgetPolicy(expected, 'workspace', limits)
  expect(fetch).toHaveBeenCalledTimes(1)
  const [path, init] = fetch.mock.calls[0]
  expect(path).toBe('/api/storage/policy'); expect(init.method).toBe('POST')
  expect(JSON.parse(init.body)).toEqual({expected, scope: 'workspace', limits})
  expect(new TextEncoder().encode(init.body).byteLength).toBeLessThanOrEqual(STORAGE_POLICY_MAX_BYTES)
})
it('refuses invalid local input before sending a mutation', async () => {
  const expected = storagePolicyFixture(), limits = structuredClone(expected.workspace.limits)
  limits.total.bytes = '1e3'
  await expect(editStorageBudgetPolicy(expected, 'workspace', limits)).rejects.toMatchObject({kind: 'invalid'})
  expect(fetch).not.toHaveBeenCalled()
})
it('bounds replies and never retries a conflict or unknown transport outcome', async () => {
  fetch.mockResolvedValueOnce(new Response(' '.repeat(STORAGE_POLICY_MAX_BYTES + 1)))
  await expect(readStorageBudgetPolicy()).rejects.toMatchObject({kind: 'uncertain'})
  fetch.mockResolvedValueOnce(new Response(null, {status: 409}))
  const expected = storagePolicyFixture()
  await expect(editStorageBudgetPolicy(expected, 'workspace', expected.defaultLimits)).rejects.toMatchObject({kind: 'conflict'})
  fetch.mockRejectedValueOnce(new Error('private diagnostic'))
  await expect(editStorageBudgetPolicy(expected, 'workspace', expected.defaultLimits)).rejects.toThrow()
  expect(fetch).toHaveBeenCalledTimes(3)
})
it('prepares only through an explicit POST and does not infer a read from uncertain preparation', async () => {
  fetch.mockResolvedValueOnce(new Response(null, {status: 204}))
  await prepareStorageBudgetPolicy()
  expect(fetch.mock.calls[0]).toEqual(['/api/storage/policy/prepare', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: '{}'}])
  fetch.mockResolvedValueOnce(new Response(null, {status: 503}))
  await expect(prepareStorageBudgetPolicy()).rejects.toMatchObject({kind: 'uncertain'})
  expect(fetch).toHaveBeenCalledTimes(2)
})

it('does not adopt a foreign response as the next frozen snapshot', async () => {
  const expected = storagePolicyFixture(), foreign = storagePolicyFixture(); foreign.workspace.id = foreign.installation
  fetch.mockResolvedValueOnce(Response.json(foreign))
  await expect(editStorageBudgetPolicy(expected, 'workspace', expected.defaultLimits)).rejects.toMatchObject({kind: 'uncertain'})
  expect(fetch).toHaveBeenCalledTimes(1)
})
