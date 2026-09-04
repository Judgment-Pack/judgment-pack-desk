import { QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { connected, stubClient, testQueryClient, type ToolHandler } from '../testing/harness'
import { McpContext, type McpConnection } from './McpProvider'
import { usePacks, useGraphDocument, useValidate } from './queries'
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

describe('usePacks', () => {
  it('does not call the tool again when another route mounts after a refusal', async () => {
    // `retry: false` is the client's default and was doing its job; the
    // re-call came from `retryOnMount`, which re-runs an errored query with no
    // data the moment a second observer subscribes. The rail holds one
    // observer for the life of the desk and every route mounts a second, so a
    // listing the runtime had refused was called once per navigation for as
    // long as it kept failing.
    const { wrapper, calls } = harness({
      list_packs: () => {
        throw new Error('the runtime refused')
      }
    })
    const first = renderHook(() => usePacks(), { wrapper })
    await waitFor(() => expect(first.result.current.isError).toBe(true))
    expect(calls.filter((call) => call.name === 'list_packs')).toHaveLength(1)

    // A second observer — what a route change is, with the rail's still there.
    const second = renderHook(() => usePacks(), { wrapper })
    await waitFor(() => expect(second.result.current.isError).toBe(true))
    expect(calls.filter((call) => call.name === 'list_packs')).toHaveLength(1)
  })

  it('still answers a later observer from the cache when the listing succeeded', async () => {
    const { wrapper, calls } = harness({
      list_packs: () => ({ text: JSON.stringify({ packs: [{ id: 'intake-triage' }] }) })
    })
    const first = renderHook(() => usePacks(), { wrapper })
    await waitFor(() => expect(first.result.current.isSuccess).toBe(true))
    const second = renderHook(() => usePacks(), { wrapper })
    await waitFor(() => expect(second.result.current.data!.packs).toHaveLength(1))
    expect(calls.filter((call) => call.name === 'list_packs')).toHaveLength(1)
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


/** The `validate` answer jpack 0.19.0 gives a clean document. */
const CLEAN = JSON.stringify({
  outputVersion: '2',
  status: 'valid',
  specVersion: '0.2.0-draft',
  layers: [
    { name: 'carrier', status: 'passed' },
    { name: 'structural', status: 'passed' },
    { name: 'semantic', status: 'passed' }
  ],
  diagnostics: [],
  diagnosticsTruncated: false
})

describe('useValidate', () => {
  it('sends the document and nothing else', async () => {
    // `through` is omitted deliberately: the runtime's own default is
    // `semantic`, so omitting it runs the whole ladder. Sending a value would
    // be the desk deciding how far to check.
    const { wrapper, calls } = harness(
      { validate: () => ({ text: CLEAN }) },
      { validateSupported: true }
    )
    const { result } = renderHook(() => useValidate('{"a":1}'), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(calls).toEqual([{ name: 'validate', args: { document: '{"a":1}' } }])
    expect(result.current.data!.report.status).toBe('valid')
  })

  it('carries the exact bytes the report is about', async () => {
    // The caller anchors these diagnostics onto a rendered document, and the
    // two are different artifacts: the check runs over the file on disk where
    // it loaded and over the served document where it did not. A report is only
    // safe to anchor if these bytes *are* the bytes on screen, and the only way
    // to know that is to have them both.
    const { wrapper } = harness(
      { validate: () => ({ text: CLEAN }) },
      { validateSupported: true }
    )
    const { result } = renderHook(() => useValidate('{"a":1}'), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.checkedBytes).toBe('{"a":1}')
  })

  it('keys the check by the bytes and by the connection', async () => {
    // Identical bytes answer `unsupported` on a runtime bundling different
    // specification artifacts, so a report cached across a reconnect would be
    // a different binary's opinion of the same file.
    const { wrapper, calls, reconnect } = harness(
      { validate: () => ({ text: CLEAN }) },
      { validateSupported: true }
    )
    const first = renderHook(() => useValidate('{"a":1}'), { wrapper })
    await waitFor(() => expect(first.result.current.isSuccess).toBe(true))
    expect(calls).toHaveLength(1)

    // Same bytes, same client, next connection.
    reconnect({ connectionEpoch: 2, validateSupported: true })
    first.rerender()
    await waitFor(() => expect(calls).toHaveLength(2))

    // And different bytes over one connection are a different question.
    renderHook(() => useValidate('{"a":2}'), { wrapper })
    await waitFor(() => expect(calls).toHaveLength(3))
  })

  it('asks nothing of a runtime that does not advertise validate', async () => {
    const { wrapper, calls } = harness(
      { validate: () => ({ text: CLEAN }) },
      { validateSupported: false }
    )
    const { result } = renderHook(() => useValidate('{"a":1}'), { wrapper })
    await waitFor(() => expect(result.current.fetchStatus).toBe('idle'))
    expect(calls).toHaveLength(0)
  })

  it('sends no empty document, which the runtime refuses by name', async () => {
    const { wrapper, calls } = harness(
      { validate: () => ({ text: CLEAN }) },
      { validateSupported: true }
    )
    const nothing = renderHook(() => useValidate(undefined), { wrapper })
    await waitFor(() => expect(nothing.result.current.fetchStatus).toBe('idle'))
    // And the empty string is not "nothing to say yet" either — it is a
    // document the runtime refuses by name, so asking would put a refusal on
    // screen in place of the fact that there is nothing to check.
    const empty = renderHook(() => useValidate(''), { wrapper })
    await waitFor(() => expect(empty.result.current.fetchStatus).toBe('idle'))
    expect(calls).toHaveLength(0)
  })
})
