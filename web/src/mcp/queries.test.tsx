import { QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { connected, stubClient, testQueryClient, type ToolHandler } from '../testing/harness'
import { McpContext, type McpConnection } from './McpProvider'
import { usePacks, useGraphDocument } from './queries'
import { ToolRefusal } from './refusal'

/** A graph document, valid, as the runtime's text half carries one. */
const DOCUMENT = JSON.stringify({
  formatVersion: '1',
  id: 'onboarding',
  version: '0.1.0',
  nodes: { a: { pack: 'p' }, b: { pack: 'q' } },
  edges: [{ from: 'a', to: 'b', fact: '/x' }],
  result: 'b'
})

function harness(handlers: Record<string, ToolHandler>, overrides: Partial<McpConnection> = {}) {
  const { client, calls } = stubClient(handlers)
  const queryClient = testQueryClient()
  let connection = connected({ client, graphDocumentSupported: true, ...overrides })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <McpContext.Provider value={connection}>{children}</McpContext.Provider>
    </QueryClientProvider>
  )
  return {
    calls,
    wrapper,
    reconnect: (next: Partial<McpConnection>) => {
      connection = connected({ client, graphDocumentSupported: true, ...overrides, ...next })
    }
  }
}

describe('useGraphDocument', () => {
  it('reads a document the runtime reported valid', async () => {
    const { wrapper } = harness({
      experimental_get_graph: () => ({
        text: DOCUMENT,
        structured: { status: 'valid', id: 'onboarding', bytes: DOCUMENT.length }
      })
    })
    const { result } = renderHook(() => useGraphDocument('onboarding'), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.document!.edges).toHaveLength(1)
    expect(result.current.data!.unreadable).toBeUndefined()
  })

  it('does not read one the runtime reported undecodable, however parseable the text is', async () => {
    // The override this exists to stop. The runtime's decode refuses duplicate
    // member names; JSON.parse takes them last-wins. A desk that parsed anyway
    // would draw a graph out of bytes the runtime had already refused, and
    // would report nothing about the refusal.
    const parseable = '{"nodes":{"a":{"pack":"p"}},"nodes":{"b":{"pack":"q"}},"edges":[]}'
    const { wrapper } = harness({
      experimental_get_graph: () => ({
        text: parseable,
        structured: { status: 'undecodable', detail: 'Object member name is duplicated.' }
      })
    })
    const { result } = renderHook(() => useGraphDocument('onboarding'), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.document).toBeUndefined()
    expect(result.current.data!.unreadable).toBe('Object member name is duplicated.')
    // The bytes are still carried: the runtime served them deliberately.
    expect(result.current.data!.raw).toBe(parseable)
  })

  it('names the member that declined a document the runtime did decode', async () => {
    const { wrapper } = harness({
      experimental_get_graph: () => ({
        text: '{"nodes":{"a":{"pack":"p"}}}',
        structured: { status: 'valid' }
      })
    })
    const { result } = renderHook(() => useGraphDocument('onboarding'), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.document).toBeUndefined()
    expect(result.current.data!.unreadable).toContain('`edges`')
  })

  it('carries an empty text block through as the empty document it is', async () => {
    // An empty text block is an answer, not a missing one. What it means is the
    // reader's question, and this reader says the served text is not JSON.
    const { wrapper } = harness({
      experimental_get_graph: () => ({ text: '', structured: { status: 'valid' } })
    })
    const { result } = renderHook(() => useGraphDocument('onboarding'), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.raw).toBe('')
    expect(result.current.data!.unreadable).toContain('not JSON')
  })

  it('rejects a call that answered with no text block at all', async () => {
    const { wrapper } = harness({ experimental_get_graph: () => ({ structured: { status: 'valid' } }) })
    const { result } = renderHook(() => useGraphDocument('onboarding'), { wrapper })
    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error!.message).toContain('returned no text content')
  })

  it('keys the fetch by the connection it was read over', async () => {
    // Two answers joined by node name only describe one graph if one connection
    // read both. A key that survived a reconnect would let a document read
    // before the socket dropped be joined to a matrix run from after it.
    const { wrapper, calls, reconnect } = harness({
      experimental_get_graph: () => ({ text: DOCUMENT, structured: { status: 'valid' } })
    })
    const first = renderHook(() => useGraphDocument('onboarding'), { wrapper })
    await waitFor(() => expect(first.result.current.isSuccess).toBe(true))
    expect(calls).toHaveLength(1)

    // Same client, same graph, next connection: nothing is carried forward.
    reconnect({ connectionEpoch: 2 })
    first.rerender()
    await waitFor(() => expect(calls).toHaveLength(2))
  })

  it('asks for nothing where the runtime does not advertise the tool', async () => {
    const { wrapper, calls } = harness(
      { experimental_get_graph: () => ({ text: DOCUMENT, structured: { status: 'valid' } }) },
      { graphDocumentSupported: false }
    )
    const { result } = renderHook(() => useGraphDocument('onboarding'), { wrapper })
    await waitFor(() => expect(result.current.fetchStatus).toBe('idle'))
    expect(calls).toHaveLength(0)
  })
})

describe('the tool call under every query', () => {
  it("reports a refusal as the runtime refusing, in the runtime's own words", async () => {
    const { wrapper } = harness({
      list_packs: () => ({ text: 'no project is configured here', isError: true })
    })
    const { result } = renderHook(() => usePacks(), { wrapper })
    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error).toBeInstanceOf(ToolRefusal)
    expect(result.current.error!.message).toBe('no project is configured here')
  })

  it('does not report a fetch that never completed as a refusal', async () => {
    const { wrapper } = harness({
      list_packs: () => {
        throw new Error('the desk connection closed')
      }
    })
    const { result } = renderHook(() => usePacks(), { wrapper })
    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error).not.toBeInstanceOf(ToolRefusal)
    expect(result.current.error!.message).toContain('the desk connection closed')
  })

  it('rejects an empty payload at the JSON parse, where JSON was promised', async () => {
    const { wrapper } = harness({ list_packs: () => ({ text: '' }) })
    const { result } = renderHook(() => usePacks(), { wrapper })
    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error!.message).toContain('not JSON')
    expect(result.current.error!.message).not.toContain('no text content')
  })
})
