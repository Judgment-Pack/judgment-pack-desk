/**
 * The session this page holds, and the bootstrap that gets it.
 *
 * **Nothing ambient authorizes anything.** The chassis prints `/launch?secret=…`;
 * opening it sets a one-shot, sixty-second handoff cookie and redirects to `/`.
 * The page then makes one `POST /api/session`, which spends that handoff and
 * answers a session id — and from then on the page puts the id on every request
 * itself, as `Authorization: Bearer` on a `fetch` and as a subprotocol offer on
 * the upgrade. Never on a URL, never in a cookie.
 *
 * These are the page's half. The chassis' half is
 * `internal/desk/session_test.go`, which is where a claim about *what is
 * accepted* belongs; nothing here can prove the chassis refuses a cookie, and
 * nothing there can prove this page stopped sending one.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { chassisUrl, deskFetch } from '../files/client'
import { testQueryClient } from '../testing/harness'
import { McpProvider, removeTheLaunchHash, socketProtocols, socketURL, useMcp } from './McpProvider'
import {
  NO_SESSION_MESSAGE,
  forgetSessionForTesting,
  renewSession,
  forgetStaleSessionToken,
  heldSessionID,
  sessionID,
  sessionStorageKey
} from './session'

beforeEach(() => {
  window.sessionStorage.clear()
  forgetSessionForTesting()
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  window.sessionStorage.clear()
  forgetSessionForTesting()
  for (const pair of document.cookie.split(';')) {
    const name = pair.trim().split('=')[0]
    if (name) document.cookie = `${name}=; Max-Age=0; Path=/`
  }
})

/** A `fetch` that answers the bootstrap and records everything it was asked. */
function servesTheExchange(id = 'a-session-id', status = 200) {
  const calls: { url: string; init: RequestInit }[] = []
  vi.stubGlobal('fetch', async (url: unknown, init: RequestInit = {}) => {
    calls.push({ url: String(url), init })
    if (String(url) === '/api/session' && init.method === 'POST') {
      return new Response(JSON.stringify({ id, subject: 'local user', issuer: null }), {
        status,
        headers: { 'content-type': 'application/json' }
      })
    }
    return new Response('{}', { status: 200, headers: { 'content-type': 'application/json' } })
  })
  return calls
}

describe('the bootstrap', () => {
  it('spends the handoff once and keeps the id under a key naming this origin', async () => {
    const calls = servesTheExchange('the-id')
    expect(await sessionID()).toBe('the-id')

    const exchanges = calls.filter((c) => c.url === '/api/session')
    expect(exchanges).toHaveLength(1)
    expect(exchanges[0]!.init.method).toBe('POST')
    // **The one request that sends a cookie**, because the handoff is one.
    expect(exchanges[0]!.init.credentials).toBe('same-origin')

    expect(sessionStorageKey()).toBe(`jpack-desk-session:${window.location.host}`)
    expect(sessionStorageKey()).toContain(':')
    expect(window.sessionStorage.getItem(sessionStorageKey())).toBe('the-id')
    expect(heldSessionID()).toBe('the-id')
  })

  it('makes one exchange however many callers ask at once', async () => {
    // The handoff is single use: eight queries mounting together must not make
    // eight `POST`s, seven of which would be 401.
    const calls = servesTheExchange('the-id')
    const asked = await Promise.all(Array.from({ length: 8 }, () => sessionID()))
    expect(new Set(asked)).toEqual(new Set(['the-id']))
    expect(calls.filter((c) => c.url === '/api/session')).toHaveLength(1)
  })

  it('does not ask again where this tab already holds one', async () => {
    window.sessionStorage.setItem(sessionStorageKey(), 'held-already')
    const calls = servesTheExchange('a-fresh-one')
    expect(await sessionID()).toBe('held-already')
    expect(calls.filter((c) => c.url === '/api/session')).toHaveLength(0)
  })

  it('keys by port, so two desks in one browser never read each other’s', () => {
    const first = sessionStorageKey()
    expect(first).toContain(window.location.host)
    expect(window.location.host).toContain(':')

    // A cookie could not be scoped this way at all: a cookie's origin has no
    // port, which is the whole reason the session stopped being one.
    //
    // `vi.stubGlobal` rather than `Object.defineProperty`, because
    // `unstubAllGlobals` puts the real `Location` back — a hand-rolled restore
    // left a plain object behind and `history.replaceState` stopped moving the
    // address for every test after it.
    vi.stubGlobal('location', { ...window.location, host: '127.0.0.1:8899' })
    expect(sessionStorageKey()).not.toBe(first)
    expect(sessionStorageKey()).toContain('8899')
    vi.unstubAllGlobals()
    expect(sessionStorageKey()).toBe(first)
  })

  it('reports a refused exchange as the one thing a person can act on', async () => {
    servesTheExchange('unused', 401)
    await expect(sessionID()).rejects.toThrow(NO_SESSION_MESSAGE)
    expect(window.sessionStorage.getItem(sessionStorageKey())).toBeNull()
  })

  it('says nothing about a token, because there is none to check', () => {
    expect(NO_SESSION_MESSAGE).not.toContain('token')
    expect(NO_SESSION_MESSAGE).toBe(
      'No session — open the URL that jpack-desk printed at startup.'
    )
  })
})

describe('every chassis request', () => {
  it('carries the id on Authorization and omits credentials', async () => {
    const calls = servesTheExchange('the-id')
    await deskFetch('/api/files')
    const read = calls.find((c) => c.url === '/api/files')!
    expect((read.init.headers as Record<string, string>).Authorization).toBe('Bearer the-id')
    // **`omit`, stated.** `same-origin` is the default and would send the
    // launch handoff on every request; that cookie is worth one exchange.
    expect(read.init.credentials).toBe('omit')
  })

  it('keeps the caller’s own members', async () => {
    const calls = servesTheExchange('the-id')
    await deskFetch('/api/file', { method: 'PUT', body: '{}', headers: { 'Content-Type': 'application/json' } })
    const wrote = calls.find((c) => c.url === '/api/file')!
    expect(wrote.init.method).toBe('PUT')
    expect(wrote.init.body).toBe('{}')
    expect((wrote.init.headers as Record<string, string>)['Content-Type']).toBe('application/json')
  })

  it('renews once on a 401 and sends the new id', async () => {
    // A restarted chassis has forgotten every id. A desk that made the person
    // reload rather than re-bootstrapping would be a desk that lies about being
    // live.
    window.sessionStorage.setItem(sessionStorageKey(), 'stale')
    const seen: { url: string; auth: string }[] = []
    vi.stubGlobal('fetch', async (url: unknown, init: RequestInit = {}) => {
      const auth = (init.headers as Record<string, string> | undefined)?.Authorization ?? ''
      seen.push({ url: String(url), auth })
      if (String(url) === '/api/session') {
        return new Response(JSON.stringify({ id: 'fresh' }), {
          status: 200,
          headers: { 'content-type': 'application/json' }
        })
      }
      return new Response('{}', { status: auth === 'Bearer fresh' ? 200 : 401 })
    })
    const answered = await deskFetch('/api/files')
    expect(answered.status).toBe(200)
    expect(seen.map((s) => s.auth)).toEqual(['Bearer stale', '', 'Bearer fresh'])
    expect(window.sessionStorage.getItem(sessionStorageKey())).toBe('fresh')
  })

  it('does not renew for ever', async () => {
    let exchanges = 0
    vi.stubGlobal('fetch', async (url: unknown) => {
      if (String(url) === '/api/session') {
        exchanges += 1
        return new Response(JSON.stringify({ id: `id-${exchanges}` }), {
          status: 200,
          headers: { 'content-type': 'application/json' }
        })
      }
      return new Response('{}', { status: 401 })
    })
    const answered = await deskFetch('/api/files')
    expect(answered.status).toBe(401)
    // One renewal, and then the refusal is the person's to act on.
    expect(exchanges).toBe(2)
  })
})

describe('a relaunch', () => {
  /** The readable marker the launch sets beside its HttpOnly handoff. */
  function markerIsSet(port = '8791') {
    document.cookie = `jpack-desk-handoff-pending-${port}=1; Path=/`
  }

  it('is spent even by a tab that already holds an id, and replaces it', async () => {
    // **The bug this closes.** The handoff is `HttpOnly`, so page code cannot
    // read it and therefore could not tell there was one — and a tab that
    // already had an id left the new handoff sitting in the jar for its full
    // sixty seconds, unspent and worth a session to anything that captured it.
    window.sessionStorage.setItem(sessionStorageKey(), 'the-old-id')
    markerIsSet()
    const calls = servesTheExchange('the-new-id')

    expect(await sessionID()).toBe('the-new-id')
    expect(calls.filter((c) => c.url === '/api/session')).toHaveLength(1)
    expect(window.sessionStorage.getItem(sessionStorageKey())).toBe('the-new-id')
    // And the marker is gone, so a reload does not try to spend a handoff that
    // is already spent.
    expect(document.cookie).not.toContain('jpack-desk-handoff-pending')
  })

  it('keeps the old id where the exchange is refused', async () => {
    // The handoff may have been spent by something else — that is the stated
    // residual — and a page that threw away a working session over it would
    // turn a theft into an outage for the person who is legitimately here.
    window.sessionStorage.setItem(sessionStorageKey(), 'the-old-id')
    markerIsSet()
    servesTheExchange('unused', 401)
    expect(await sessionID()).toBe('the-old-id')
  })

  it('asks for nothing where no handoff is waiting', async () => {
    window.sessionStorage.setItem(sessionStorageKey(), 'the-old-id')
    const calls = servesTheExchange('a-fresh-one')
    expect(await sessionID()).toBe('the-old-id')
    expect(calls.filter((c) => c.url === '/api/session')).toHaveLength(0)
  })
})

describe('renewal', () => {
  it('is shared, and a late refusal does not delete the fresh id', async () => {
    // Two requests in flight with an old id: the first renews, and the second's
    // `401` arrives after the fresh id is already stored. Clearing
    // unconditionally would delete the new session on the strength of an answer
    // about the old one.
    window.sessionStorage.setItem(sessionStorageKey(), 'stale')
    let exchanges = 0
    vi.stubGlobal('fetch', async (url: unknown) => {
      if (String(url) === '/api/session') {
        exchanges += 1
        return new Response(JSON.stringify({ id: 'fresh' }), {
          status: 200,
          headers: { 'content-type': 'application/json' }
        })
      }
      return new Response('{}', { status: 200 })
    })
    const [first, second] = await Promise.all([renewSession('stale'), renewSession('stale')])
    expect(first).toBe('fresh')
    expect(second).toBe('fresh')
    expect(exchanges, 'one renewal, shared').toBe(1)

    // And a third, late refusal about the id that has already been replaced.
    expect(await renewSession('stale')).toBe('fresh')
    expect(exchanges, 'a late 401 about an old id starts nothing').toBe(1)
    expect(window.sessionStorage.getItem(sessionStorageKey())).toBe('fresh')
  })

  it('renews where the id that failed is the one still stored', async () => {
    window.sessionStorage.setItem(sessionStorageKey(), 'stale')
    let exchanges = 0
    vi.stubGlobal('fetch', async (url: unknown) => {
      if (String(url) === '/api/session') {
        exchanges += 1
        return new Response(JSON.stringify({ id: 'fresh' }), { status: 200 })
      }
      return new Response('{}', { status: 200 })
    })
    expect(await renewSession('stale')).toBe('fresh')
    expect(exchanges).toBe(1)
  })
})

describe('the addresses and offers this page builds', () => {
  it('opens the relay at /ws, with no query at all', () => {
    expect(socketURL()).toBe(`ws://${window.location.host}/ws`)
    expect(socketURL()).not.toContain('?')
    expect(socketURL.length).toBe(0)
  })

  it('carries the id in the subprotocol offer and nowhere else', () => {
    const offer = socketProtocols('the-id')
    expect(offer).toEqual(['jpack-desk', 'jpack-desk-session.the-id'])
    // The address is clean; the credential is in the offer, which is the one
    // place a browser lets a page put anything on a handshake.
    expect(socketURL()).not.toContain('the-id')
  })

  it('builds a chassis URL out of the caller’s own parameters and no others', () => {
    expect(chassisUrl('/api/files')).toBe('/api/files')
    expect(chassisUrl('/api/file', { path: 'packs/a.pack.json' })).toBe(
      '/api/file?path=packs%2Fa.pack.json'
    )
    expect(chassisUrl('/api/assistant/relay/v1/v1beta/models/m:x', { alt: 'sse' })).toBe(
      '/api/assistant/relay/v1/v1beta/models/m:x?alt=sse'
    )
  })

  it('puts no credential on any address, under any spelling', () => {
    for (const built of [
      chassisUrl('/api/files'),
      chassisUrl('/api/file', { path: 'a.json' }),
      chassisUrl('/api/assistant/key'),
      socketURL()
    ]) {
      expect(built).not.toContain('token')
      expect(built).not.toContain('secret')
      expect(built).not.toContain('session')
    }
  })

  it('emits no dangling ? where the caller had no parameters', () => {
    expect(chassisUrl('/api/files').endsWith('?')).toBe(false)
  })
})

describe('the credential this page used to keep', () => {
  it('removes the stale sessionStorage key', () => {
    window.sessionStorage.setItem('jpack-desk-token', 'left over from an older build')
    forgetStaleSessionToken()
    expect(window.sessionStorage.getItem('jpack-desk-token')).toBeNull()
  })

  it('leaves everything else in that storage alone', () => {
    window.sessionStorage.setItem('something-else', 'kept')
    forgetStaleSessionToken()
    expect(window.sessionStorage.getItem('something-else')).toBe('kept')
  })

  it('does not throw where the browser refuses storage at all', () => {
    vi.stubGlobal('sessionStorage', {
      removeItem() {
        throw new Error('storage is disabled in this browser')
      }
    })
    expect(() => forgetStaleSessionToken()).not.toThrow()
  })
})

describe('the bare # the launch redirect leaves', () => {
  function at(href: string) {
    window.history.replaceState(null, '', href)
  }

  it('is taken off the address bar, once, on load', () => {
    at('/#')
    expect(window.location.href.endsWith('#')).toBe(true)
    removeTheLaunchHash()
    expect(window.location.href.endsWith('#')).toBe(false)
    expect(window.location.pathname).toBe('/')
    expect(window.location.search).toBe('')
  })

  it('keeps the query, which is the route’s and not the launch’s', () => {
    at('/packs?edit=1#')
    removeTheLaunchHash()
    expect(window.location.href.endsWith('#')).toBe(false)
    expect(window.location.search).toBe('?edit=1')
  })

  it('leaves a fragment that names something alone', () => {
    at('/help#security')
    removeTheLaunchHash()
    expect(window.location.hash).toBe('#security')
  })

  it('does nothing where there is no fragment at all', () => {
    at('/packs')
    removeTheLaunchHash()
    expect(window.location.href.endsWith('#')).toBe(false)
  })

  it('does not throw where the browser refuses history manipulation', () => {
    at('/#')
    vi.stubGlobal('history', {
      replaceState() {
        throw new Error('history is not available here')
      }
    })
    expect(() => removeTheLaunchHash()).not.toThrow()
  })
})

/** A page that renders whatever the connection is saying. */
function Says() {
  const { status, error } = useMcp()
  return <p>{`${status}: ${error?.message ?? ''}`}</p>
}

/** A WebSocket that never opens, which is every failed handshake. */
function refusingSocket(opened: string[][] = []): typeof WebSocket {
  class Refuses {
    onopen: (() => void) | null = null
    onerror: (() => void) | null = null
    onclose: (() => void) | null = null
    onmessage: (() => void) | null = null
    constructor(url: string, protocols?: string[]) {
      opened.push([url, ...(protocols ?? [])])
      queueMicrotask(() => this.onerror?.())
    }
    close() {}
    send() {}
  }
  return Refuses as unknown as typeof WebSocket
}

describe('what the page does on load', () => {
  function draw() {
    return render(
      <QueryClientProvider client={testQueryClient()}>
        <McpProvider>
          <Says />
        </McpProvider>
      </QueryClientProvider>
    )
  }

  it('bootstraps, then offers the id on the upgrade', async () => {
    const opened: string[][] = []
    vi.stubGlobal('WebSocket', refusingSocket(opened))
    servesTheExchange('the-id')
    draw()
    await waitFor(() => expect(opened.length).toBeGreaterThan(0))
    expect(opened[0]![0]).toBe(`ws://${window.location.host}/ws`)
    expect(opened[0]!.slice(1)).toEqual(['jpack-desk', 'jpack-desk-session.the-id'])
    expect(opened[0]![0]).not.toContain('the-id')
  })

  it('names the launch URL where the exchange is refused, and stops trying', async () => {
    vi.stubGlobal('WebSocket', refusingSocket())
    servesTheExchange('unused', 401)
    draw()
    await waitFor(() => expect(screen.getByText(`failed: ${NO_SESSION_MESSAGE}`)).toBeTruthy())
    // `failed` and not `reconnecting`: no handoff appears on its own, and a
    // page that retried for ever would bury the instruction that fixes it.
    expect(screen.queryByText(/^reconnecting/)).toBeNull()
  })

  it('opens no socket after the effect was torn down', async () => {
    // **StrictMode mounts twice**, and a route change unmounts. The bootstrap
    // is awaited before the socket is built, so an effect can be disposed while
    // that promise is pending — and connecting afterwards opens a socket with
    // nothing left to close it.
    const opened: string[][] = []
    vi.stubGlobal('WebSocket', refusingSocket(opened))
    let release: (() => void) | undefined
    const held = new Promise<void>((resolve) => {
      release = resolve
    })
    vi.stubGlobal('fetch', async (url: unknown) => {
      if (String(url) === '/api/session') {
        await held
        return new Response(JSON.stringify({ id: 'the-id' }), { status: 200 })
      }
      return new Response('{}', { status: 200 })
    })
    const drawn = draw()
    // Torn down while the bootstrap is still in flight.
    drawn.unmount()
    release?.()
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(opened, 'a socket was opened after the effect was disposed').toEqual([])
  })

  it('validates a stale id once, renews once, and then stops', async () => {
    // A restarted chassis has forgotten every id. The browser gives the page no
    // status for a refused upgrade, so the id is put to a channel that answers:
    // a `401` from `GET /api/session` means this id names nothing.
    window.sessionStorage.setItem(sessionStorageKey(), 'an-id-from-an-older-desk')
    vi.stubGlobal('WebSocket', refusingSocket())
    const asked: { url: string; method: string }[] = []
    vi.stubGlobal('fetch', async (url: unknown, init: RequestInit = {}) => {
      asked.push({ url: String(url), method: init.method ?? 'GET' })
      // The store is new: the old id is refused, and there is no handoff to
      // exchange for a fresh one.
      return new Response('{}', { status: 401 })
    })
    draw()
    await waitFor(() => expect(screen.getByText(`failed: ${NO_SESSION_MESSAGE}`)).toBeTruthy())
    expect(asked.some((a) => a.url === '/api/session' && a.method === 'GET')).toBe(true)
    expect(asked.some((a) => a.url === '/api/session' && a.method === 'POST')).toBe(true)
    expect(screen.queryByText(/^reconnecting/)).toBeNull()
  })

  it('keeps reconnecting where the chassis simply is not answering', async () => {
    vi.stubGlobal('WebSocket', refusingSocket())
    vi.stubGlobal('fetch', async () => {
      throw new TypeError('Failed to fetch')
    })
    draw()
    await waitFor(() => expect(screen.getByText(/^reconnecting/)).toBeTruthy())
  })
})
