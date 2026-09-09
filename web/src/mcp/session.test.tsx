/**
 * The bootstrap: **one exchange per page load, and nothing else touches the id.**
 *
 * This is the file the design's one rule lives in. PR #40 was set aside after
 * five review rounds because the page had several actors that could each read
 * or replace the session id during its life — the bootstrap, a relaunch marker,
 * renewal, refusal handling in three transports, eviction on the chassis — and
 * every pair of them was a race the next round found. So the properties held
 * here are not "the bootstrap works": they are **how many times it happens**,
 * **who else writes the id** (nobody), and **that a refusal is terminal**.
 *
 * Every test drives the real `deskFetch`, the real `bindModelCall` and the real
 * `bootstrap` against a stubbed `fetch`, so what is counted is what would go on
 * the wire.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { McpProvider, removeTheLaunchHash, socketProtocols, socketURL } from './McpProvider'
import { NoSession, bootstrap, forgetSession, resetSessionForTesting, sessionStorageKey } from './session'
import { deskFetch, listFiles } from '../files/client'
import { bindModelCall } from '../assistant/session'

/** One request as the stub saw it. */
interface Seen {
  url: string
  method: string
  credentials?: RequestCredentials
  authorization?: string
}

const MINTED = 'aaaaaaaabbbbbbbbccccccccddddddddeeeeeeeeffffffff'
const STORED = '11111111222222223333333344444444555555556666666'

/**
 * A `fetch` that records everything and answers what the case asks for.
 *
 * `answer` is given the request so a case can answer the exchange differently
 * from everything else — which is the whole subject here.
 */
function record(answer: (seen: Seen) => Promise<Response> | Response) {
  const seen: Seen[] = []
  vi.stubGlobal('fetch', async (input: unknown, init?: RequestInit) => {
    const headers = (init?.headers ?? {}) as Record<string, string>
    const call: Seen = {
      url: String(input),
      method: init?.method ?? 'GET',
      credentials: init?.credentials,
      authorization: headers.Authorization
    }
    seen.push(call)
    return answer(call)
  })
  return seen
}

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } })

/** The exchange's own answer, and a plain success for everything else. */
const mints = (seen: Seen) =>
  seen.url === '/api/session' && seen.method === 'POST'
    ? json({ id: MINTED, subject: 'local user', issuer: null })
    : json({ root: '/project', files: [] })

const exchanges = (seen: Seen[]) => seen.filter((c) => c.url === '/api/session' && c.method === 'POST')

beforeEach(() => {
  // **This runs after `testing/setup.ts`'s hook**, which hands every other
  // suite a session so that none of them asserts the bootstrap by accident.
  // Clearing here is what makes this suite the one that does.
  resetSessionForTesting()
  window.sessionStorage.clear()
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  resetSessionForTesting()
  window.sessionStorage.clear()
})

describe('the one exchange', () => {
  it('makes exactly one POST however many callers await it', async () => {
    const seen = record(mints)
    const answers = await Promise.all(Array.from({ length: 10 }, () => bootstrap()))
    expect(exchanges(seen)).toHaveLength(1)
    expect(answers).toEqual(Array.from({ length: 10 }, () => MINTED))
  })

  it('makes no second POST for a caller that arrives after it settled', async () => {
    const seen = record(mints)
    expect(await bootstrap()).toBe(MINTED)
    expect(await bootstrap()).toBe(MINTED)
    await listFiles()
    expect(exchanges(seen)).toHaveLength(1)
  })

  it('sends the handoff cookie on the exchange and on nothing else', async () => {
    const seen = record(mints)
    await listFiles()
    const exchange = exchanges(seen)[0]!
    // `same-origin` is what carries the one cookie this desk has.
    expect(exchange.credentials).toBe('same-origin')
    expect(exchange.authorization).toBeUndefined()
    for (const call of seen.filter((c) => c !== exchange)) {
      expect(call.credentials, call.url).toBe('omit')
    }
  })

  it('stores the minted id under a key that names this origin, port included', async () => {
    record(mints)
    await bootstrap()
    expect(sessionStorageKey()).toBe(`jpack-desk-session:${window.location.host}`)
    expect(sessionStorageKey()).toContain(window.location.port)
    expect(window.sessionStorage.getItem(sessionStorageKey())).toBe(MINTED)
  })

  it('replaces a stored id with the one the chassis just minted', async () => {
    // The id the chassis minted is the id the chassis knows; a stored one from
    // an earlier process is not.
    window.sessionStorage.setItem(sessionStorageKey(), STORED)
    record(mints)
    expect(await bootstrap()).toBe(MINTED)
    expect(window.sessionStorage.getItem(sessionStorageKey())).toBe(MINTED)
  })

  it('keeps the stored id when the exchange is refused', async () => {
    // A reload of a tab that bootstrapped earlier: the handoff is long spent,
    // and the id this tab holds may well still be live. Throwing it away over
    // a 401 here would make a reload a sign-out.
    window.sessionStorage.setItem(sessionStorageKey(), STORED)
    const seen = record((call) =>
      call.url === '/api/session' ? json({ error: 'no unspent launch', code: 'unauthorized' }, 401) : json({})
    )
    expect(await bootstrap()).toBe(STORED)
    expect(window.sessionStorage.getItem(sessionStorageKey())).toBe(STORED)
    expect(exchanges(seen)).toHaveLength(1)
  })

  it('answers null when the exchange is refused and this tab holds nothing', async () => {
    record((call) => (call.url === '/api/session' ? json({ code: 'unauthorized' }, 401) : json({})))
    expect(await bootstrap()).toBeNull()
  })

  it('answers the stored id when the chassis does not answer at all', async () => {
    // Not answering is a different thing from refusing, and the requests that
    // follow will fail on their own terms and say so.
    window.sessionStorage.setItem(sessionStorageKey(), STORED)
    record(() => {
      throw new TypeError('Failed to fetch')
    })
    expect(await bootstrap()).toBe(STORED)
  })
})

describe('every chassis call awaits it', () => {
  it('sends nothing until the exchange has answered', async () => {
    let settle: (() => void) | undefined
    const held = new Promise<void>((resolve) => {
      settle = resolve
    })
    const seen = record(async (call) => {
      if (call.url === '/api/session') {
        await held
        return json({ id: MINTED })
      }
      return json({ root: '/project', files: [] })
    })

    const listing = listFiles()
    // The exchange is out; the call that is waiting on it is not.
    await Promise.resolve()
    expect(exchanges(seen)).toHaveLength(1)
    expect(seen.filter((c) => c.url !== '/api/session')).toHaveLength(0)

    settle!()
    await listing
    const files = seen.filter((c) => c.url !== '/api/session')
    expect(files).toHaveLength(1)
    expect(files[0]!.authorization).toBe(`Bearer ${MINTED}`)
  })

  it('puts the id on the header and never on the address', async () => {
    const seen = record(mints)
    await listFiles()
    for (const call of seen.filter((c) => c.url !== '/api/session')) {
      expect(call.url).not.toContain(MINTED)
      expect(call.url).not.toContain('token=')
      expect(call.authorization).toBe(`Bearer ${MINTED}`)
    }
  })

  it('is awaited by the assistant’s relay transport too', async () => {
    const seen = record((call) =>
      call.url === '/api/session' ? json({ id: MINTED }) : json({ ok: true })
    )
    await bindModelCall('openai-compatible')('chat/completions', { body: '{}' })
    const relayed = seen.filter((c) => c.url.startsWith('/api/assistant/relay'))
    expect(relayed).toHaveLength(1)
    expect(relayed[0]!.authorization).toBe(`Bearer ${MINTED}`)
    expect(relayed[0]!.credentials).toBe('omit')
    expect(exchanges(seen)).toHaveLength(1)
  })
})

describe('a refusal after the bootstrap is terminal', () => {
  it('forgets the id, and the next call sends nothing at all', async () => {
    const seen = record((call) =>
      call.url === '/api/session'
        ? json({ id: MINTED })
        : json({ error: 'no session', code: 'unauthorized' }, 401)
    )

    await expect(listFiles()).rejects.toThrow(NoSession)
    expect(window.sessionStorage.getItem(sessionStorageKey())).toBeNull()
    const after = seen.length

    // **The second call issues no POST and no request.** This is the property
    // the shape exists for: there is no actor that can start a second
    // exchange, so the page stays in the state it is in until it is loaded
    // again.
    await expect(listFiles()).rejects.toThrow(NoSession)
    expect(seen).toHaveLength(after)
    expect(exchanges(seen)).toHaveLength(1)
  })

  it('ends the assistant’s relay too, and it re-sends nothing', async () => {
    const seen = record((call) =>
      call.url === '/api/session'
        ? json({ id: MINTED })
        : json({ error: 'no session', code: 'unauthorized' }, 401)
    )
    const call = bindModelCall('openai-compatible')
    await expect(call('chat/completions', { body: '{}' })).rejects.toThrow(NoSession)
    const after = seen.length
    await expect(call('chat/completions', { body: '{}' })).rejects.toThrow(NoSession)
    expect(seen).toHaveLength(after)
  })

  it('does not end the session over the model endpoint’s own 401', async () => {
    // The relay forwards the endpoint's status verbatim, so a 401 there is a
    // key the endpoint does not accept — not a session this desk refused.
    // Logging somebody out of their desk because their model key expired would
    // be this desk reading one refusal as another.
    const seen = record((call) =>
      call.url === '/api/session'
        ? json({ id: MINTED })
        : json({ error: { message: 'invalid api key' } }, 401)
    )
    const call = bindModelCall('openai-compatible')
    const answered = await call('chat/completions', { body: '{}' })
    expect(answered.status).toBe(401)
    expect(window.sessionStorage.getItem(sessionStorageKey())).toBe(MINTED)
    // And the next call still goes out, carrying the same live session.
    await call('chat/completions', { body: '{}' })
    const relayed = seen.filter((c) => c.url.startsWith('/api/assistant/relay'))
    expect(relayed).toHaveLength(2)
    expect(relayed[1]!.authorization).toBe(`Bearer ${MINTED}`)
  })

  it('is terminal even for a caller that never saw the refusal', async () => {
    record(mints)
    await bootstrap()
    forgetSession()
    await expect(deskFetch('/api/desk-config')).rejects.toThrow(NoSession)
  })

  it('leaves the id in place for a call that answers anything but 401', async () => {
    const seen = record((call) =>
      call.url === '/api/session' ? json({ id: MINTED }) : json({ error: 'nope' }, 403)
    )
    await expect(listFiles()).rejects.toThrow()
    expect(window.sessionStorage.getItem(sessionStorageKey())).toBe(MINTED)
    await expect(listFiles()).rejects.toThrow()
    expect(seen.filter((c) => c.url !== '/api/session')).toHaveLength(2)
  })
})

describe('the page holds the id and nothing else does', () => {
  it('is the only module state a refusal changes', async () => {
    // A browser with storage refused still gets a working desk for the life of
    // the page: the id lives in the memoised exchange, not in storage.
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('storage is disabled')
    })
    try {
      const seen = record(mints)
      expect(await bootstrap()).toBe(MINTED)
      await listFiles()
      const files = seen.filter((c) => c.url !== '/api/session')
      expect(files[0]!.authorization).toBe(`Bearer ${MINTED}`)
    } finally {
      setItem.mockRestore()
    }
  })
})

describe('the desk’s own MCP connection', () => {
  /** A `WebSocket` that records the handshake and never opens. */
  function recordingSockets() {
    const dialled: { url: string; protocols?: string | string[] }[] = []
    class Silent {
      onopen?: () => void
      onerror?: () => void
      onclose?: () => void
      onmessage?: () => void
      readyState = 0
      constructor(url: string, protocols?: string | string[]) {
        dialled.push({ url, protocols })
      }
      send() {}
      close() {}
    }
    vi.stubGlobal('WebSocket', Silent)
    return dialled
  }

  it('bootstraps first, then offers the id as a subprotocol and never on the URL', async () => {
    const seen = record(mints)
    const dialled = recordingSockets()
    render(
      <QueryClientProvider client={new QueryClient()}>
        <McpProvider>
          <div />
        </McpProvider>
      </QueryClientProvider>
    )
    await waitFor(() => expect(dialled.length).toBeGreaterThan(0))
    // **The exchange came first**, which is the ordering the whole design
    // rests on: the upgrade has to carry the id, and there is one place it
    // comes from.
    expect(exchanges(seen)).toHaveLength(1)
    expect(dialled[0]!.url).toMatch(/\/ws$/)
    expect(dialled[0]!.url).not.toContain('?')
    expect(dialled[0]!.url).not.toContain(MINTED)
    expect(dialled[0]!.protocols).toEqual(['jpack-desk', `jpack-desk-session.${MINTED}`])
  })

  it('opens no socket at all for a page with no session', async () => {
    record((call) => (call.url === '/api/session' ? json({ code: 'unauthorized' }, 401) : json({})))
    const dialled = recordingSockets()
    render(
      <QueryClientProvider client={new QueryClient()}>
        <McpProvider>
          <div />
        </McpProvider>
      </QueryClientProvider>
    )
    // Given time to do the wrong thing, and it does not.
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(dialled).toEqual([])
  })
})

describe('the address the launch leaves behind', () => {
  it('spells the upgrade without a credential, and the offer with one', () => {
    expect(socketURL()).toBe(`ws://${window.location.host}/ws`)
    expect(socketProtocols(MINTED)).toEqual(['jpack-desk', `jpack-desk-session.${MINTED}`])
  })

  it('takes the bare # off, and leaves an ordinary address alone', () => {
    // `location.hash` is empty for a URL ending in a bare `#`, so this reads
    // `href`: the launch redirects to `/#` precisely so the request's own
    // fragment cannot be inherited (RFC 9110 §10.2.2). Driven through real
    // history entries rather than a stubbed `location`, because the property
    // is about what a browser actually leaves in the address bar.
    const was = window.location.href

    window.history.replaceState(null, '', '/#')
    expect(window.location.href.endsWith('#')).toBe(true)
    removeTheLaunchHash()
    expect(window.location.href.endsWith('#')).toBe(false)
    expect(window.location.pathname).toBe('/')

    // An ordinary address is left exactly as it is — including a real
    // fragment, which is a client-side route's own and not the launch's.
    window.history.replaceState(null, '', '/packs/x?edit=1#members')
    removeTheLaunchHash()
    expect(window.location.href).toContain('/packs/x?edit=1#members')

    window.history.replaceState(null, '', was)
  })
})
