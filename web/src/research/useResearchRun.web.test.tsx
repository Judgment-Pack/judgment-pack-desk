import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { HostTool } from '../assistant/engine'
import { useResearchRun } from './useResearchRun'
import type { AuthoringMode } from './mode'

const fake = vi.hoisted(() => ({ session: vi.fn(), tools: vi.fn(), policy: vi.fn(() => 'WEB POLICY FOR THIS MESSAGE') }))
vi.mock('../assistant/target', () => ({ bindExecution: () => ({}), selectedAssistant: () => ({ models: ['test-model'], model: 'test-model', tools: [] }) }))
vi.mock('../assistant/useAssistantSlot', () => ({ assistantReady: () => true, useAssistantSlot: () => ({ state: 'ready', endpoint: 'https://example.invalid', keyStatus: 'stored', keyPresent: true, engine: 'test', thinking: 'off' }) }))
vi.mock('../assistant/pickedModel', () => ({ usePickedModel: () => ({ model: 'test-model', models: ['test-model'] }) }))
vi.mock('../assistant/engines', () => ({ loadEngine: async () => ({}) }))
vi.mock('../assistant/session', () => ({ runAssistantSession: fake.session }))
vi.mock('../files/queries', () => ({ useFileListing: () => ({ data: { root: '/test' } }) }))
vi.mock('../mcp/session', () => ({ sessionBearer: async () => 'test-session' }))
vi.mock('../mcp/McpProvider', () => ({ useMcp: () => ({ status: 'disconnected', client: null }) }))
vi.mock('../mcp/prompts', () => ({ AUTHOR_PACK_PROMPT: 'author_pack', TEST_PACK_PROMPT: 'test_pack', usePromptNames: () => ({ data: [] }), usePromptText: () => ({}) }))
vi.mock('../config/DeskConfigProvider', () => ({ useEffectiveConfig: () => ({ config: { research: { gateway: { authority: 'test', signer: { public: 'test' } }, sources: { search: null, read: null }, limits: { seconds: 30 } } } }) }))
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
