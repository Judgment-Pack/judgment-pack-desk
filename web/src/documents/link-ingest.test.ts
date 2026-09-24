import { expect, it, vi } from 'vitest'
import { GatewayError } from '../research/gatewayClient'
import { ingestLink, ingestWeb } from './client'
import { signedDocument } from './__fixtures__/signedDocument'
const mocks = vi.hoisted(() => ({ acquire: vi.fn(), fetch: vi.fn(), seal: vi.fn(), registry: vi.fn() }))
vi.mock('../research/gatewayClient', async original => ({ ...await original<typeof import('../research/gatewayClient')>(), acquire: mocks.acquire, seal: mocks.seal, registry: mocks.registry }))
vi.mock('../files/client', async original => ({ ...await original<typeof import('../files/client')>(), deskFetch: mocks.fetch }))

const signal = new AbortController().signal
const documents = { enabled: true, source: 'documents', maxFileBytes: 4 << 20, maxRequestBytes: 32 << 20, maxResponseBytes: 8 << 20 }

it.each([
  ['the managed local gateway', true, 'local-documents'],
  ['a gateway the desk-level file declares', false, undefined]
])('sends a link to %s with the constraint that gateway takes', async (_which, managedLocal, constraint) => {
  const { object, pin } = await signedDocument('web')
  mocks.acquire.mockResolvedValue({ text: object.proof!.response })
  mocks.fetch.mockResolvedValue(new Response(JSON.stringify({ sha256: 'saved' }), { status: 200 }))
  mocks.seal.mockResolvedValue(undefined)
  mocks.registry.mockResolvedValue('')
  const config = { gateway: pin, documents, sources: { search: null, read: null, web: { source: 'web' as const } }, limits: { searches: 8, reads: 12, bytes: 8_388_608, seconds: 600 }, ...(managedLocal ? { managedLocal: true } : {}) }
  // The registry is empty here, so verification refuses at the end; what the
  // gateway was asked, and how, is settled before that.
  await expect(ingestLink({ url: 'https://example.com/policy' }, config, signal, () => {})).rejects.toThrow()
  expect(mocks.acquire).toHaveBeenCalledTimes(1)
  expect(mocks.acquire.mock.calls[0]![1]).toBe('web')
  expect(mocks.acquire.mock.calls[0]![2]).toEqual({ url: 'https://example.com/policy' })
  expect(mocks.acquire.mock.calls[0]![5]).toBe(constraint)
  expect(mocks.seal.mock.calls[0]![2]).toBe(constraint)
  expect(mocks.registry.mock.calls[0]![1]).toBe(constraint)
  vi.resetAllMocks()
})

it('keeps a selected file pinned to the managed local gateway whatever the pin says', async () => {
  const { object, pin } = await signedDocument('gmail')
  mocks.acquire.mockRejectedValue(new GatewayError(409, 'local document processing is no longer available; nothing was sent', 'research-unconfigured'))
  const config = { gateway: pin, documents, sources: { search: null, read: null, web: null }, limits: { searches: 8, reads: 12, bytes: 8_388_608, seconds: 600 } }
  const { ingestGmail } = await import('./client')
  await expect(ingestGmail({ messageId: 'abc1', grant: 'aa'.repeat(32) }, config, signal, () => {})).rejects.toThrow()
  expect(mocks.acquire.mock.calls[0]![5]).toBe('local-documents')
  expect(object.proof).toBeDefined()
  vi.resetAllMocks()
})

it('keeps the gateway’s failure word for a link read, and the pane’s sentence for Add link', async () => {
  const { pin } = await signedDocument('web')
  const config = { gateway: pin, documents, sources: { search: null, read: null, web: null }, limits: { searches: 8, reads: 12, bytes: 8_388_608, seconds: 600 }, managedLocal: true }
  mocks.acquire.mockRejectedValue(new GatewayError(400, 'source failed: web-over-limit'))
  await expect(ingestLink({ url: 'https://example.com/policy' }, config, signal, () => {})).rejects.toThrow('web-over-limit')
  await expect(ingestWeb({ url: 'https://example.com/policy' }, config, signal, () => {})).rejects.toThrow('Could not read this link')
  expect(mocks.fetch).not.toHaveBeenCalled()
  vi.resetAllMocks()
})
