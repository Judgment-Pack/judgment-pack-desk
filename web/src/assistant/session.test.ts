/**
 * The session's own connection, and what it is allowed to be.
 *
 * The conformance suite drives this module end to end; what is here is the
 * three properties that suite cannot see because it injects a transport: that
 * the session opens **its own**, that closing the session closes it, and that
 * the relay base the engine is handed carries the desk's token and nothing
 * else.
 */
import type { Transport } from '@modelcontextprotocol/sdk/shared/transport.js'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { assistantTransport, bindModelCall, openAssistantConnection, suffixProblem } from './session'
import { scriptedRuntime } from './conformance/scriptedServer'
import type { AssistantEvent } from './engine'

afterEach(() => {
  vi.unstubAllGlobals()
  window.sessionStorage.clear()
})

describe('the model capability the desk binds', () => {
  /** Install a recording fetch and return what it was called with. */
  function recordingFetch(): { calls: { url: string; init: RequestInit }[] } {
    const calls: { url: string; init: RequestInit }[] = []
    vi.stubGlobal('fetch', async (url: unknown, init: RequestInit) => {
      calls.push({ url: String(url), init })
      return new Response('{}', { status: 200, headers: { 'content-type': 'application/json' } })
    })
    return { calls }
  }

  it('builds the address itself, with the desk’s token and no other parameter', async () => {
    window.sessionStorage.setItem('jpack-desk-token', 'a-token')
    const { calls } = recordingFetch()
    await bindModelCall()('chat/completions', { body: '{}' })
    const url = new URL(calls[0]!.url, 'http://desk.invalid')
    expect(url.pathname).toBe('/api/assistant/relay/v1/chat/completions')
    // The relay refuses a query carrying anything but `token`, outright.
    expect([...url.searchParams.entries()]).toEqual([['token', 'a-token']])
    expect(calls[0]!.init.method).toBe('POST')
  })

  it('puts the Anthropic suffix after the same mount point', async () => {
    window.sessionStorage.setItem('jpack-desk-token', 'a-token')
    const { calls } = recordingFetch()
    await bindModelCall()('v1/messages', { body: '{}' })
    expect(new URL(calls[0]!.url, 'http://desk.invalid').pathname).toBe(
      '/api/assistant/relay/v1/v1/messages'
    )
  })

  it('carries the protocol headers and drops everything else', async () => {
    // An allow-list, mirrored from the chassis' own. A credential header this
    // desk has never heard of does not travel, because it is not on the list.
    window.sessionStorage.setItem('jpack-desk-token', 'a-token')
    const { calls } = recordingFetch()
    await bindModelCall()('chat/completions', {
      body: '{}',
      headers: {
        'content-type': 'application/json',
        'anthropic-version': '2023-06-01',
        authorization: 'Bearer smuggled',
        'x-api-key': 'smuggled',
        cookie: 'smuggled',
        'x-auth-token': 'smuggled',
        'ocp-apim-subscription-key': 'smuggled'
      }
    })
    expect(calls[0]!.init.headers).toEqual({
      'content-type': 'application/json',
      'anthropic-version': '2023-06-01'
    })
  })

  it('refuses a suffix outside the relay’s own segment rule, before anything is sent', async () => {
    window.sessionStorage.setItem('jpack-desk-token', 'a-token')
    const { calls } = recordingFetch()
    const call = bindModelCall()
    for (const bad of [
      '',
      '/',
      'chat//completions',
      '../admin',
      'chat/../../admin',
      'chat/completions?stream=true',
      'chat%2Fcompletions',
      'chat\\completions',
      'chat completions',
      '.',
      '..',
      'a'.repeat(257)
    ]) {
      await expect(call(bad, { body: '{}' }), bad).rejects.toThrow()
    }
    expect(calls).toEqual([])
  })

  it('states the rule as a function, so a refusal can be read without a socket', () => {
    expect(suffixProblem('chat/completions')).toBe('')
    expect(suffixProblem('v1/messages')).toBe('')
    expect(suffixProblem('chat/completions?x=1')).not.toBe('')
    expect(suffixProblem('%2e%2e/admin')).not.toBe('')
  })

  it('captures fetch when it is bound, not when it is called', async () => {
    // This is what lets the conformance session seal every network global for
    // the duration of an engine's run: the desk's capability still works, and
    // an engine that reaches for a global does not.
    window.sessionStorage.setItem('jpack-desk-token', 'a-token')
    const { calls } = recordingFetch()
    const call = bindModelCall()
    vi.stubGlobal('fetch', () => {
      throw new Error('the engine reached for globalThis.fetch')
    })
    await call('chat/completions', { body: '{}' })
    expect(calls).toHaveLength(1)
  })
})

describe('the assistant’s own connection', () => {
  it('opens a new transport every time, never a shared one', () => {
    // A memoised transport would be one `jpack mcp` and one gate shared between
    // sessions, and closing either would take the other's connection with it.
    // Sharing the *desk's* would put the gate in front of the page's own calls.
    window.sessionStorage.setItem('jpack-desk-token', 'a-token')
    const first = assistantTransport()
    const second = assistantTransport()
    expect(first).not.toBe(second)
  })

  it('does not open a socket merely by being built', () => {
    // The transport opens on `start()`, which `connect` calls. A constructor
    // that dialled would mean a tab that has never run a session still holds a
    // runtime subprocess.
    window.sessionStorage.setItem('jpack-desk-token', 'a-token')
    const opened: string[] = []
    class Spy {
      constructor(url: string) {
        opened.push(url)
      }
    }
    vi.stubGlobal('WebSocket', Spy)
    assistantTransport()
    expect(opened).toEqual([])
  })

  it('serves only the allow-listed tools, out of the runtime’s own listing', async () => {
    const runtime = await scriptedRuntime()
    const connection = openAssistantConnection({
      allowed: ['get_schema', 'validate'],
      onEvent: () => {},
      transport: runtime.transport
    })
    const ready = await connection.ready
    expect(ready.tools.map((tool) => tool.name)).toEqual(['get_schema', 'validate'])
    // The definitions are the runtime's own, schema included.
    expect(ready.tools[0]!.inputSchema).toBeDefined()
    await connection.close()
    await runtime.close()
  })

  it('closes the connection it opened', async () => {
    const runtime = await scriptedRuntime()
    let closed = 0
    const watched: Transport = {
      start: () => runtime.transport.start(),
      close: () => {
        closed += 1
        return runtime.transport.close()
      },
      send: (message) => runtime.transport.send(message),
      set onmessage(handler: Transport['onmessage']) {
        runtime.transport.onmessage = handler
      },
      set onclose(handler: Transport['onclose']) {
        runtime.transport.onclose = handler
      },
      set onerror(handler: Transport['onerror']) {
        runtime.transport.onerror = handler
      }
    }
    const connection = openAssistantConnection({
      allowed: ['validate'],
      onEvent: () => {},
      transport: watched
    })
    await connection.ready
    await connection.close()
    expect(closed).toBe(1)
    await runtime.close()
  })

  /**
   * A transport whose answers a test decides, one method at a time.
   *
   * The three stages a setup can be stuck at — the socket, `initialize`,
   * `tools/list` — are separately controllable, because the finding was that
   * each of them left a connection nobody could close.
   */
  function stagedTransport(behaviour: {
    initialize?: 'answer' | 'hang'
    listTools?: 'answer' | 'hang' | 'reject'
  }): { transport: Transport; closed: () => number } {
    let closed = 0
    const transport: Transport = {
      async start() {},
      async close() {
        closed += 1
        // What a real transport does, and what makes a hung `initialize`
        // reject rather than hang on after the connection is closed.
        transport.onclose?.()
      },
      async send(message) {
        const frame = message as unknown as { id?: number; method?: string }
        if (frame.method === undefined) return
        const reply = (result: unknown) =>
          queueMicrotask(() =>
            transport.onmessage?.({ jsonrpc: '2.0', id: frame.id, result } as never)
          )
        if (frame.method === 'initialize') {
          if (behaviour.initialize === 'hang') return
          reply({
            protocolVersion: '2025-06-18',
            capabilities: { tools: {} },
            serverInfo: { name: 'staged', version: '0' }
          })
          return
        }
        if (frame.method === 'tools/list') {
          if (behaviour.listTools === 'hang') return
          if (behaviour.listTools === 'reject') {
            queueMicrotask(() =>
              transport.onmessage?.({
                jsonrpc: '2.0',
                id: frame.id,
                error: { code: -32603, message: 'the runtime would not list its tools' }
              } as never)
            )
            return
          }
          reply({ tools: [] })
        }
      }
    }
    return { transport, closed: () => closed }
  }

  it('closes the transport when tools/list refuses', async () => {
    // The finding, exactly: setup threw past a connected client that nothing
    // held a reference to, so the socket — and the jpack mcp behind it —
    // stayed up for the life of the tab.
    const staged = stagedTransport({ listTools: 'reject' })
    const connection = openAssistantConnection({
      allowed: ['validate'],
      onEvent: () => {},
      transport: staged.transport
    })
    await expect(connection.ready).rejects.toThrow(/would not list its tools/)
    expect(staged.closed()).toBeGreaterThan(0)
  })

  it.each([
    ['initialize', { initialize: 'hang' as const }],
    ['tools/list', { listTools: 'hang' as const }]
  ])('closes the transport when %s hangs and the caller stops', async (_stage, behaviour) => {
    // Stop, and unmount, and a navigation are all this: the run aborts, and
    // the connection has to go with it even though its setup never finished.
    const staged = stagedTransport(behaviour)
    const controller = new AbortController()
    const connection = openAssistantConnection({
      allowed: ['validate'],
      onEvent: () => {},
      transport: staged.transport,
      signal: controller.signal
    })
    const settled = connection.ready.then(
      () => 'resolved',
      (error: Error) => error.name
    )
    controller.abort()
    await expect(settled).resolves.toBe('AbortError')
    expect(staged.closed()).toBeGreaterThan(0)
  })

  it.each([
    ['initialize', { initialize: 'hang' as const }],
    ['tools/list', { listTools: 'hang' as const }]
  ])('closes the transport when %s hangs and close() is called directly', async (_stage, behaviour) => {
    const staged = stagedTransport(behaviour)
    const connection = openAssistantConnection({
      allowed: ['validate'],
      onEvent: () => {},
      transport: staged.transport
    })
    // The handle exists before its setup finishes, which is the whole reason
    // the shape is synchronous.
    await connection.close()
    expect(staged.closed()).toBe(1)
  })

  it('closes nothing twice, however many times it is closed', async () => {
    const staged = stagedTransport({ listTools: 'hang' })
    const connection = openAssistantConnection({
      allowed: ['validate'],
      onEvent: () => {},
      transport: staged.transport
    })
    await Promise.all([connection.close(), connection.close(), connection.close()])
    expect(staged.closed()).toBe(1)
  })

  it('refuses a connection whose signal was already aborted, and opens nothing', async () => {
    const staged = stagedTransport({})
    const controller = new AbortController()
    controller.abort()
    const connection = openAssistantConnection({
      allowed: ['validate'],
      onEvent: () => {},
      transport: staged.transport,
      signal: controller.signal
    })
    await expect(connection.ready).rejects.toThrow()
    expect(staged.closed()).toBeGreaterThan(0)
  })

  it('reports a refusal as a guardrail event and lets nothing out of the page', async () => {
    const runtime = await scriptedRuntime()
    const events: AssistantEvent[] = []
    const connection = openAssistantConnection({
      allowed: ['validate'],
      onEvent: (event) => events.push(event),
      transport: runtime.transport
    })
    const ready = await connection.ready
    await expect(ready.callTool('write_file', { path: 'p' })).rejects.toThrow(/write_file/)
    expect(runtime.seen).toEqual([])
    expect(events).toEqual([
      {
        type: 'guardrail',
        tool: 'write_file',
        action: 'refused',
        detail: expect.stringContaining('write_file') as unknown as string
      }
    ])
    await connection.close()
    await runtime.close()
  })
})
