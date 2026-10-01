import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { HostTool } from '../assistant/engine'
import { useResearchRun } from './useResearchRun'
import type { AuthoringMode } from './mode'

const fake = vi.hoisted(() => ({ session: vi.fn(), tools: vi.fn(), policy: vi.fn(() => 'WEB POLICY FOR THIS MESSAGE'), load: vi.fn(), gateway: { authority: 'test', signer: { public: 'test' } } }))
vi.mock('../documents/client', async (original) => ({ ...(await original<typeof import('../documents/client')>()), loadDocument: fake.load }))
vi.mock('../assistant/target', () => ({ bindExecution: () => ({}), selectedAssistant: () => ({ models: ['test-model'], model: 'test-model', tools: [] }) }))
vi.mock('../assistant/useAssistantSlot', () => ({ assistantReady: () => true, useAssistantSlot: () => ({ state: 'ready', endpoint: 'https://example.invalid', keyStatus: 'stored', keyPresent: true, engine: 'test', thinking: 'off' }) }))
vi.mock('../assistant/pickedModel', () => ({ usePickedModel: () => ({ model: 'test-model', models: ['test-model'] }) }))
vi.mock('../assistant/engines', () => ({ loadEngine: async () => ({}) }))
vi.mock('../assistant/session', () => ({ runAssistantSession: fake.session }))
vi.mock('../files/queries', () => ({ useFileListing: () => ({ data: { root: '/test' } }) }))
vi.mock('../mcp/session', () => ({ sessionBearer: async () => 'test-session' }))
vi.mock('../mcp/McpProvider', () => ({ useMcp: () => ({ status: 'disconnected', client: null }) }))
vi.mock('../mcp/prompts', () => ({ AUTHOR_PACK_PROMPT: 'author_pack', TEST_PACK_PROMPT: 'test_pack', usePromptNames: () => ({ data: [] }), usePromptText: () => ({}) }))
vi.mock('../config/DeskConfigProvider', () => ({ useEffectiveConfig: () => ({ config: { research: { gateway: fake.gateway, sources: { search: null, read: null }, limits: { seconds: 30 } } } }) }))
vi.mock('../shell/consoleLog', () => ({ recordActivity: () => {} }))
afterEach(() => { cleanup(); vi.clearAllMocks() })

it('offers current web tools and policy on the first Research message and its follow-up', async () => {
  const search: HostTool = { name: 'search_sources', description: 'Fixture search', inputSchema: {}, execute: vi.fn(async () => ({ content: [] })) }
  const read: HostTool = { ...search, name: 'read_link' }
  fake.tools.mockReturnValue([search, read])
  fake.session.mockImplementation(async (_engine, request, deliver) => {
    expect(request.hostTools.map((tool: HostTool) => tool.name)).toEqual(['get_authoring_instructions', 'search_sources', 'read_link'])
    expect(request.prompt).toContain('WEB POLICY FOR THIS MESSAGE')
    await request.hostTools.find((tool: HostTool) => tool.name === 'search_sources').execute({ query: 'public policy' }, request.signal)
    deliver({ type: 'message', text: 'Here is the research summary.' })
  })
  const { result, rerender } = renderHook(({ mode }: { mode: AuthoringMode }) => useResearchRun({ mode, draftTools: fake.tools, researchPolicy: fake.policy }), { initialProps: { mode: 'draft' as AuthoringMode } })
  rerender({ mode: 'web-research' })
  expect(result.current.blocked).toBe('')
  act(() => result.current.run!.start('Research public policy', []))
  await waitFor(() => expect(result.current.state.status).toBe('complete'))
  act(() => result.current.run!.send('Find another source'))
  await waitFor(() => expect(fake.session).toHaveBeenCalledTimes(2))
  await waitFor(() => expect(result.current.state.status).toBe('complete'))
  expect(search.execute).toHaveBeenCalledTimes(2)
  expect(result.current.state.candidates).toEqual([])
})

it('keeps supplied-link reading usable without a search provider', async () => {
  fake.tools.mockReturnValue([{ name: 'read_link', execute: vi.fn() }])
  fake.session.mockImplementation(async (_engine, request, deliver) => {
    expect(request.hostTools.map((tool: HostTool) => tool.name)).toEqual(['get_authoring_instructions', 'read_link'])
    deliver({ type: 'message', text: 'Please supply a URL, or configure a search connection.' })
  })
  const { result } = renderHook(() => useResearchRun({ mode: 'web-research', draftTools: fake.tools, researchPolicy: fake.policy }))
  expect(result.current.blocked).toBe('')
  act(() => result.current.run!.start('Research this', []))
  await waitFor(() => expect(result.current.state.status).toBe('complete'))
})

it('asks a chat draft to trace its citations again whenever the documents the chat keeps change', async () => {
  const { AuthoringRun } = await import('./run')
  const changed = vi.spyOn(AuthoringRun.prototype, 'basisChanged')
  const file = (pages: number[]) => [{ id: 'kept', name: 'Kept', text: '', document: { id: 'kept', digest: 'sha256:' + 'a'.repeat(64), pages, allowPartial: false } }]
  const { rerender } = renderHook(({ pages }: { pages: number[] }) => useResearchRun({ mode: 'draft', draftTools: fake.tools, documents: () => file(pages) }), { initialProps: { pages: [1] } })
  const after = changed.mock.calls.length
  rerender({ pages: [1] })
  expect(changed.mock.calls.length).toBe(after)
  rerender({ pages: [1, 2] })
  expect(changed.mock.calls.length).toBe(after + 1)
  changed.mockRestore()
})

it('ends its run\'s traces at rest when its owner unmounts', async () => {
  const { AuthoringRun } = await import('./run')
  const detached = vi.spyOn(AuthoringRun.prototype, 'detach')
  const attached = vi.spyOn(AuthoringRun.prototype, 'attach')
  const { unmount } = renderHook(() => useResearchRun({ mode: 'draft', draftTools: fake.tools }))
  expect(attached).toHaveBeenCalled()
  expect(detached).not.toHaveBeenCalled()
  unmount()
  expect(detached).toHaveBeenCalledOnce()
  detached.mockRestore()
  attached.mockRestore()
})

it('traces a reopened chat draft through its own ports: the configured pin, the kept documents, and a load its unmount ends', async () => {
  const fixture = (await import('../documents/__fixtures__/web-snapshot.json')).default
  const record = structuredClone(fixture) as unknown as import('../documents/record').DocumentRecord
  const quote = 'First fact & second.'
  const reference = { id: '12345678-1234-1234-1234-000000000001', digest: 'sha256:' + '1'.repeat(64), pages: [1], allowPartial: false }
  const kept = [{ id: reference.id, name: 'Example', text: '', document: reference }]
  const pack = { specVersion: '0.2.0-draft', id: 'https://example.org/p', version: '0.1.0', title: 'Kept page', sources: [{ id: 'page', title: 'Page', locator: { kind: 'uri', value: record.provenance.source.url }, citation: { location: `attachment:${reference.id}/${reference.digest}/page/1`, excerpt: quote } }] }
  const { INITIAL_STATE } = await import('./run')
  const saved = { ...INITIAL_STATE, status: 'needs-input' as const, turns: [{ id: 't1', role: 'user' as const, kind: 'brief' as const, text: 'Draft it.', at: '2026-09-30T00:00:00Z' }],
    candidates: [{ revision: 1, producedBy: 'conversation' as const, document: pack, text: JSON.stringify(pack), digest: '' }] }
  let release!: () => void
  const signals: AbortSignal[] = []
  fake.load.mockImplementation((_reference: unknown, _pin: unknown, signal: AbortSignal) => {
    signals.push(signal)
    return new Promise((resolve) => { release = () => resolve({ record, digest: reference.digest, object: { version: 1, original: { name: 'Example', mediaType: 'text/plain', bytes: '', sha256: '' } } }) })
  })
  const { result, rerender, unmount } = renderHook(({ documents }: { documents: typeof kept }) => useResearchRun({ mode: 'draft', draftTools: fake.tools, documents: () => documents }), { initialProps: { documents: kept } })
  await act(() => result.current.run!.restore(saved))
  await waitFor(() => expect(fake.load).toHaveBeenCalledOnce())
  // Verified under the pin the desk is configured with, as it stands.
  expect(fake.load.mock.calls[0]![0]).toEqual(reference)
  expect(fake.load.mock.calls[0]![1]).toEqual({ authority: 'test', signer: { public: 'test' } })
  act(() => release())
  await waitFor(() => expect(result.current.state.citations).toMatchObject([{ sourceId: 'page', traced: true }]))
  // The chat stops keeping the document: traced again, through the hook alone.
  rerender({ documents: [] })
  await waitFor(() => expect(result.current.state.citations).toMatchObject([{ sourceId: 'page', traced: false }]))
  // A load still held when the owner goes away is ended with it.
  rerender({ documents: kept })
  await waitFor(() => expect(fake.load).toHaveBeenCalledTimes(2))
  expect(signals[1]!.aborted).toBe(false)
  unmount()
  expect(signals[1]!.aborted).toBe(true)
})

it('traces a reopened chat draft again under a pin that moves while it loads, and the trace it superseded loads nothing more', async () => {
  const fixture = (await import('../documents/__fixtures__/web-snapshot.json')).default
  const record = structuredClone(fixture) as unknown as import('../documents/record').DocumentRecord
  const quote = 'First fact & second.'
  const reference = (n: number) => ({ id: `12345678-1234-1234-1234-00000000000${n}`, digest: 'sha256:' + String(n).repeat(64), pages: [1], allowPartial: false })
  const kept = [1, 2].map(n => ({ id: reference(n).id, name: `Example ${n}`, text: '', document: reference(n) }))
  const pack = { specVersion: '0.2.0-draft', id: 'https://example.org/p', version: '0.1.0', title: 'Kept pages',
    sources: [1, 2].map(n => ({ id: `page-${n}`, title: 'Page', locator: { kind: 'uri', value: record.provenance.source.url }, citation: { location: `attachment:${reference(n).id}/${reference(n).digest}/page/1`, excerpt: quote } })) }
  const { INITIAL_STATE } = await import('./run')
  const saved = { ...INITIAL_STATE, status: 'needs-input' as const, turns: [{ id: 't1', role: 'user' as const, kind: 'brief' as const, text: 'Draft it.', at: '2026-09-30T00:00:00Z' }],
    candidates: [{ revision: 1, producedBy: 'conversation' as const, document: pack, text: JSON.stringify(pack), digest: '' }] }
  const A = { authority: 'gateway-a', signer: { public: 'a' } }, B = { authority: 'gateway-b', signer: { public: 'b' } }
  fake.gateway = A
  let release!: () => void
  const verified = (ref: { digest: string }) => ({ record, digest: ref.digest, object: { version: 1, original: { name: 'Example', mediaType: 'text/plain', bytes: '', sha256: '' } } })
  fake.load.mockImplementation((ref: { id: string; digest: string }) => ref.id.endsWith('1') && fake.load.mock.calls.length === 1
    ? new Promise(resolve => { release = () => resolve(verified(ref)) }) : Promise.resolve(verified(ref)))
  const { result, rerender } = renderHook(() => useResearchRun({ mode: 'draft', draftTools: fake.tools, documents: () => kept }))
  await act(() => result.current.run!.restore(saved))
  await waitFor(() => expect(fake.load).toHaveBeenCalledOnce())
  // The configured pin moves while the first document loads.
  fake.gateway = B
  rerender()
  act(() => release())
  await waitFor(() => expect(result.current.state.tracing).toBe(false))
  // The move superseded the trace under A, which asked for nothing more once
  // it was; the trace that landed verified both documents under B.
  expect(fake.load.mock.calls.map(call => [(call[0] as { id: string }).id.slice(-1), (call[1] as { authority: string }).authority])).toEqual([['1', 'gateway-a'], ['1', 'gateway-b'], ['2', 'gateway-b']])
  expect(result.current.state.citations.map(citation => citation.traced)).toEqual([true, true])
  expect(result.current.state.tracedBasis).toBe(result.current.run!.basisNow())
  fake.gateway = { authority: 'test', signer: { public: 'test' } }
})
