import { QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { parseConnectionCatalog, useConnections } from './catalog'
import { connectionCatalogFixture } from '../testing/connectionCatalog'
import { testQueryClient } from '../testing/harness'
const fetch = vi.hoisted(() => vi.fn())
vi.mock('../files/client', async original => ({ ...await original<typeof import('../files/client')>(), deskFetch: fetch }))
afterEach(() => { cleanup(); vi.resetAllMocks() })
const fixture = () => structuredClone(connectionCatalogFixture)

it('requires the supported protocol and complete operations without inventing missing providers', () => {
 const wire = fixture()
 expect(parseConnectionCatalog(wire).map(item => item.id)).toEqual(['google-drive', 'gmail', 'notion', 'obsidian'])
 wire.providers[0]!.selection = 'remote-script'
 wire.providers[1]!.operations = ['status']
 wire.providers.push({ ...wire.providers[2]!, id: 'future-provider' })
 expect(parseConnectionCatalog(wire).map(item => item.id)).toEqual(['notion', 'obsidian'])
 expect(parseConnectionCatalog({ version: 1, providers: [] })).toEqual([])
})

it.each([null, {}, { version: 2, providers: [] }, { version: 1, providers: null }, { version: 1, providers: Array(33).fill({}) }, { version: 1, providers: [connectionCatalogFixture.providers[0], connectionCatalogFixture.providers[0]] }, { version: 1, providers: [{ ...connectionCatalogFixture.providers[0], operations: ['status', 'status'] }] }, { version: 1, providers: [{ ...connectionCatalogFixture.providers[0], queryRequired: undefined }] }])('refuses malformed discovery: %j', raw => {
 expect(() => parseConnectionCatalog(raw)).toThrow()
})

it('requests status only for advertised, compatible providers and drops cached actions on failure', async () => {
 let broken = false
 fetch.mockImplementation(async (url: string) => {
  if (url.endsWith('/catalog')) return Response.json(broken ? { version: 2, providers: [] } : { version: 1, providers: [connectionCatalogFixture.providers[3]] })
  return Response.json({ version: 1, provider: 'obsidian', state: 'connected', maxFileBytes: 4 << 20, maxFiles: 4 })
 })
 const client = testQueryClient()
 const { result, rerender } = renderHook(({ enabled }) => useConnections(enabled), { initialProps: { enabled: true }, wrapper: ({ children }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> })
 await waitFor(() => expect(result.current.entries[0]?.status.data?.state).toBe('connected'))
 expect(fetch.mock.calls.map(call => call[0])).toEqual(['/api/connections/catalog', '/api/connections/obsidian/status'])
 rerender({ enabled: false }); expect(result.current.entries).toEqual([])
 rerender({ enabled: true }); expect(result.current.entries).toHaveLength(1)
 broken = true
 await act(async () => { await result.current.refetch() })
 await waitFor(() => expect(result.current.isError).toBe(true)); expect(result.current.entries).toEqual([])
})

it('does not start a discovery request with local processing disabled', () => {
 const client = testQueryClient()
 const { result } = renderHook(() => useConnections(false), { wrapper: ({ children }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> })
 expect(result.current.entries).toEqual([]); expect(result.current.loading).toBe(false); expect(fetch).not.toHaveBeenCalled()
})

it('removing a provider cancels its pending status and cannot restore it through a late response', async () => {
 let providers = connectionCatalogFixture.providers.slice(3), release!: (response: Response) => void, signal!: AbortSignal
 fetch.mockImplementation((url: string, options: RequestInit) => {
  if (url.endsWith('/catalog')) return Promise.resolve(Response.json({ version: 1, providers }))
  signal = options.signal as AbortSignal
  return new Promise<Response>(resolve => { release = resolve })
 })
 const client = testQueryClient()
 const { result } = renderHook(() => useConnections(true), { wrapper: ({ children }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> })
 await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2))
 providers = []
 await act(async () => { await result.current.refetch() })
 await waitFor(() => expect(result.current.entries).toEqual([]))
 expect(signal.aborted).toBe(true)
 await act(async () => release(Response.json({ state: 'connected' })))
 expect(result.current.entries).toEqual([])
})
