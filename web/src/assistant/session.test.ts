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
import { assistantTransport, openAssistantConnection, relayBaseUrl } from './session'
import { scriptedRuntime } from './conformance/scriptedServer'
import type { AssistantEvent } from './engine'

afterEach(() => {
  vi.unstubAllGlobals()
  window.sessionStorage.clear()
})

describe('the relay base handed to the engine', () => {
  it('is the relay mount point with the desk’s token and no other parameter', () => {
    window.sessionStorage.setItem('jpack-desk-token', 'a-token')
    const base = relayBaseUrl()
    const url = new URL(base, 'http://desk.invalid')
    expect(url.pathname).toBe('/api/assistant/relay/v1')
    // The relay refuses a query carrying anything but `token`, outright. A
    // second parameter here would be every request refused.
    expect([...url.searchParams.entries()]).toEqual([['token', 'a-token']])
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
    const connection = await openAssistantConnection({
      allowed: ['get_schema', 'validate'],
      onEvent: () => {},
      transport: runtime.transport
    })
    expect(connection.tools.map((tool) => tool.name)).toEqual(['get_schema', 'validate'])
    // The definitions are the runtime's own, schema included.
    expect(connection.tools[0]!.inputSchema).toBeDefined()
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
    const connection = await openAssistantConnection({
      allowed: ['validate'],
      onEvent: () => {},
      transport: watched
    })
    await connection.close()
    expect(closed).toBe(1)
    await runtime.close()
  })

  it('reports a refusal as a guardrail event and lets nothing out of the page', async () => {
    const runtime = await scriptedRuntime()
    const events: AssistantEvent[] = []
    const connection = await openAssistantConnection({
      allowed: ['validate'],
      onEvent: (event) => events.push(event),
      transport: runtime.transport
    })
    await expect(connection.callTool('write_file', { path: 'p' })).rejects.toThrow(/write_file/)
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
