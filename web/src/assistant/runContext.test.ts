import { afterEach, expect, it, vi } from 'vitest'
import type { CallTool } from '@judgment-pack/harness/engine'
import { memoryInUse, runtimeIdentityOf } from './runContext'

const request = vi.hoisted(() => ({ memory: vi.fn() }))
vi.mock('@judgment-pack/harness/memory/client', () => ({ MEMORY_KEY: ['project-memory'], memoryRequest: request.memory }))
afterEach(() => vi.clearAllMocks())

const described = (version: string): CallTool => vi.fn(async () => ({ content: [{ type: 'text', text: JSON.stringify({ tool: { name: 'jpack', version }, specVersions: ['0.2.0-draft'] }) }] }))

it('names the Runtime by the digest of what it describes and its version, read once for a connection', async () => {
  const connection = {}
  const call = described('0.35.0')
  const first = await runtimeIdentityOf(connection, call)
  expect(first).toEqual({ identity: expect.stringMatching(/^[a-f0-9]{64}$/), version: '0.35.0' })
  expect(await runtimeIdentityOf(connection, call)).toEqual(first)
  expect(call).toHaveBeenCalledTimes(1)
  // Another runtime is another identity.
  const other = await runtimeIdentityOf({}, described('0.36.0'))
  expect(other!.identity).not.toBe(first!.identity)
})

it('names no identity for a build without a release version, or one that cannot be described', async () => {
  for (const version of ['0.0.0-dev', 'dev', 'unknown', '0.0.0']) expect(await runtimeIdentityOf({}, described(version)), version).toBeUndefined()
  const broken: CallTool = vi.fn(async () => { throw new Error('refused') })
  const connection = {}
  expect(await runtimeIdentityOf(connection, broken)).toBeUndefined()
  // A failure is not remembered: the next turn asks again.
  expect(await runtimeIdentityOf(connection, described('0.35.0'))).toMatchObject({ version: '0.35.0' })
})

it('reads the project\'s memory use setting, and says nothing where it cannot be read', async () => {
  request.memory.mockResolvedValueOnce({ content: { settings: { use: true, suggest: false } } })
  expect(await memoryInUse()).toBe(true)
  request.memory.mockResolvedValueOnce({ content: { settings: { use: false, suggest: true } } })
  expect(await memoryInUse()).toBe(false)
  request.memory.mockRejectedValueOnce(new Error('offline'))
  expect(await memoryInUse()).toBeUndefined()
  request.memory.mockResolvedValueOnce({ content: {} })
  expect(await memoryInUse()).toBeUndefined()
})
