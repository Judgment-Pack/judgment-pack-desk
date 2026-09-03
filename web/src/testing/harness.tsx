/**
 * Standing one connected desk up without a socket.
 *
 * The provider owns a real WebSocket and a real SDK Client, which a component
 * test has no business starting. What a test does need is everything on the
 * other side of that: a connection whose capabilities, epoch and status it can
 * set, and a client whose tool answers it writes. So the context the provider
 * fills is filled here directly, and the queries, the routes and the views run
 * exactly as they do in the page.
 *
 * The answers are shaped like the wire's own — a text block, optional
 * structured content, and `isError` for a refusal reported in band — because
 * the code under test reads them that way, and a fixture shaped like the
 * client's internals would test the fixture.
 */
import type { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render } from '@testing-library/react'
import { useState, type ReactElement } from 'react'
import { Link, RouterProvider, createMemoryRouter } from 'react-router-dom'
import { UNKNOWN_CAPABILITIES } from '../mcp/capabilities'
import { McpContext, type McpConnection } from '../mcp/McpProvider'

/** One tool answer, as `tools/call` carries it. */
export interface ToolAnswer {
  /** The text half. Omit for a call answering with no text block at all. */
  text?: string
  structured?: Record<string, unknown>
  /** True for a refusal the runtime reported in band. */
  isError?: boolean
}

export type ToolHandler = (
  args: Record<string, unknown>
) => ToolAnswer | Promise<ToolAnswer>

/**
 * Optional extras a stub may answer.
 *
 * Additive and default-off, so every existing caller is unaffected: a client
 * with no `prompts` answers an empty listing, which is what a runtime without
 * prompts looks like from here.
 */
export interface StubExtras {
  /** One entry per advertised prompt: its name, and the text it carries. */
  prompts?: Record<string, { description?: string; text: string }>
}

/** A client that answers from handlers, and remembers what it was asked. */
export function stubClient(
  handlers: Record<string, ToolHandler>,
  extras: StubExtras = {}
): {
  client: Client
  calls: { name: string; args: Record<string, unknown> }[]
} {
  const calls: { name: string; args: Record<string, unknown> }[] = []
  const client = {
    async callTool(request: { name: string; arguments?: Record<string, unknown> }) {
      const args = request.arguments ?? {}
      calls.push({ name: request.name, args })
      const handler = handlers[request.name]
      if (!handler) throw new Error(`no stub answers ${request.name}`)
      const answer = await handler(args)
      return {
        content: answer.text === undefined ? [] : [{ type: 'text', text: answer.text }],
        structuredContent: answer.structured,
        isError: answer.isError ?? false
      }
    },
    async listPrompts() {
      return { prompts: Object.keys(extras.prompts ?? {}).map((name) => ({ name })) }
    },
    async getPrompt(params: { name: string }) {
      const prompt = (extras.prompts ?? {})[params.name]
      if (!prompt) throw new Error(`no stub answers the prompt ${params.name}`)
      return {
        description: prompt.description,
        messages: [{ role: 'user', content: { type: 'text', text: prompt.text } }]
      }
    }
  }
  return { client: client as unknown as Client, calls }
}

/**
 * One connected desk. Every member has the value a live connection would have
 * once initialize and the tool listing have both answered; a test overrides the
 * ones its case is about.
 */
export function connected(overrides: Partial<McpConnection> = {}): McpConnection {
  return {
    client: null,
    status: 'ready',
    error: null,
    server: { name: 'jpack', version: 'test' },
    ...UNKNOWN_CAPABILITIES,
    known: true,
    connectionEpoch: 1,
    capabilitiesError: null,
    attempt: 0,
    everConnected: true,
    retryNow: () => {},
    ...overrides
  }
}

/** The page's own query defaults: the runtime is local, and the chassis watches it. */
export function testQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { refetchOnWindowFocus: false, staleTime: Infinity, retry: false }
    }
  })
}

/**
 * Render one view inside a connection a test controls.
 *
 * `setConnection` re-renders with a new connection value, which is how a
 * reconnect, a capability the runtime no longer advertises, and a listing that
 * failed are all driven: each is a new context value over the same query cache,
 * exactly as the provider produces one.
 */
export function renderConnected(
  ui: ReactElement,
  connectionValue: McpConnection,
  options: {
    path?: string
    queryClient?: QueryClient
    /**
     * Add an in-app link, so a test can drive same-document navigation — the
     * exit `beforeunload` never sees and a router blocker has to cover.
     */
    nav?: boolean
  } = {}
) {
  const queryClient = options.queryClient ?? testQueryClient()
  const setConnectionRef: { current?: (next: McpConnection) => void } = {}
  // A data router, because the application uses one: `useBlocker` is only
  // available there, and a harness on the older router would let a view that
  // depends on it pass here and throw in the page.
  //
  // **One router for the life of the render.** Production builds its router
  // once; a harness that built a new one on every `setConnection` would reset
  // history and remount the tree on a capability change, which is neither what
  // the page does nor a state any test means to be in. Only the context value
  // changes, which is exactly what the provider changes.
  const connection = { current: connectionValue }
  const Shell = () => {
    const [value, setValue] = useState(connection.current)
    setConnectionRef.current = (next: McpConnection) => {
      connection.current = next
      setValue(next)
    }
    return (
      <McpContext.Provider value={value}>
        {options.nav && <Link to="/elsewhere">go elsewhere</Link>}
        {ui}
      </McpContext.Provider>
    )
  }
  const router = createMemoryRouter([{ path: '*', element: <Shell /> }], {
    initialEntries: [options.path ?? '/']
  })
  const tree = (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  const result = render(tree)
  return {
    ...result,
    queryClient,
    setConnection: (next: McpConnection) => {
      act(() => setConnectionRef.current?.(next))
    }
  }
}
