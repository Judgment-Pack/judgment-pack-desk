/**
 * The page holds no credential, and this is what says so.
 *
 * The chassis prints `/launch?secret=…`, trades the secret once for the
 * `jpack-desk-session` cookie and redirects to `/`. From then on the browser
 * attaches the cookie to every same-origin request by itself — so an address
 * this page builds carries nothing, and there is nothing in `sessionStorage`
 * for anything to read.
 *
 * These are the page's half of that. The chassis' half is
 * `internal/desk/session_test.go`, which is where a claim about *what is
 * accepted* belongs; nothing here can prove the chassis refuses a query, and
 * nothing there can prove this page stopped sending one.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { chassisUrl, deskFetch } from '../files/client'
import { testQueryClient } from '../testing/harness'
import {
  McpProvider,
  NO_SESSION_MESSAGE,
  forgetStaleSessionToken,
  removeTheLaunchHash,
  socketURL,
  useMcp
} from './McpProvider'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  window.sessionStorage.clear()
})

describe('the addresses this page builds', () => {
  it('opens the relay at /ws, with no query at all', () => {
    // `socketURL` took the token as an argument and wrote `?token=`. It takes
    // nothing now: a function with a credential parameter is a function some
    // caller has to find a credential for.
    expect(socketURL()).toBe(`ws://${window.location.host}/ws`)
    expect(socketURL()).not.toContain('?')
    expect(socketURL.length).toBe(0)
  })

  it('builds a chassis URL out of the caller’s own parameters and no others', () => {
    expect(chassisUrl('/api/files')).toBe('/api/files')
    expect(chassisUrl('/api/desk-config')).toBe('/api/desk-config')
    expect(chassisUrl('/api/file', { path: 'packs/a.pack.json' })).toBe(
      '/api/file?path=packs%2Fa.pack.json'
    )
    // The pair the gemini wire has nowhere else to put, and it is the whole
    // query — there is no credential in front of it any more.
    expect(chassisUrl('/api/assistant/relay/v1/v1beta/models/m:x', { alt: 'sse' })).toBe(
      '/api/assistant/relay/v1/v1beta/models/m:x?alt=sse'
    )
  })

  it('puts no token on any address, under either spelling', () => {
    for (const built of [
      chassisUrl('/api/files'),
      chassisUrl('/api/file', { path: 'a.json' }),
      chassisUrl('/api/assistant/key'),
      socketURL()
    ]) {
      expect(built).not.toContain('token')
      expect(built).not.toContain('secret')
    }
  })

  it('emits no dangling ? where the caller had no parameters', () => {
    // A leftover of the token always being there. Harmless on the wire and
    // wrong in every log line and every test that reads an address.
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

describe('the bare # the launch exchange redirects to', () => {
  /** jsdom's history is real; this is the address bar the page woke up on. */
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
    expect(window.location.pathname).toBe('/packs')
    expect(window.location.search).toBe('?edit=1')
  })

  it('leaves a fragment that names something alone', () => {
    // An in-page anchor is somebody's link, not the exchange's leftovers.
    at('/help#security')
    removeTheLaunchHash()
    expect(window.location.hash).toBe('#security')
  })

  it('does nothing where there is no fragment at all', () => {
    at('/packs')
    removeTheLaunchHash()
    expect(window.location.href.endsWith('#')).toBe(false)
    expect(window.location.pathname).toBe('/packs')
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

describe('every chassis request', () => {
  it('states credentials: same-origin rather than relying on the default', async () => {
    const seen: RequestInit[] = []
    vi.stubGlobal('fetch', async (_url: unknown, init: RequestInit) => {
      seen.push(init)
      return new Response('{}', { status: 200 })
    })
    await deskFetch('/api/files')
    await deskFetch('/api/file', { method: 'PUT', body: '{}' })
    expect(seen).toHaveLength(2)
    for (const init of seen) expect(init.credentials).toBe('same-origin')
    // And the caller's own members survive the wrapper.
    expect(seen[1]!.method).toBe('PUT')
    expect(seen[1]!.body).toBe('{}')
  })
})

/** A page that renders whatever the connection is saying. */
function Says() {
  const { status, error } = useMcp()
  return <p>{`${status}: ${error?.message ?? ''}`}</p>
}

/** A WebSocket that never opens, which is every failed handshake. */
function refusingSocket(): typeof WebSocket {
  class Refuses {
    onopen: (() => void) | null = null
    onerror: (() => void) | null = null
    onclose: (() => void) | null = null
    onmessage: (() => void) | null = null
    constructor() {
      queueMicrotask(() => this.onerror?.())
    }
    close() {}
    send() {}
  }
  return Refuses as unknown as typeof WebSocket
}

describe('what the page says when there is no session', () => {
  it('names the launch URL, on a 401, and stops trying', async () => {
    // **A failed handshake tells the page nothing**: the browser withholds the
    // status of a rejected upgrade, so "the chassis is not running" and "this
    // browser has no session" arrive as the same empty error. Only a channel
    // that reports a status can tell them apart.
    vi.stubGlobal('WebSocket', refusingSocket())
    const asked: string[] = []
    vi.stubGlobal('fetch', async (url: unknown) => {
      asked.push(String(url))
      return new Response('{"code":"unauthorized"}', { status: 401 })
    })
    render(
      <QueryClientProvider client={testQueryClient()}>
        <McpProvider>
          <Says />
        </McpProvider>
      </QueryClientProvider>
    )
    await waitFor(() =>
      expect(screen.getByText(`failed: ${NO_SESSION_MESSAGE}`)).toBeTruthy()
    )
    expect(asked).toContain('/api/session')
    // `failed` and not `reconnecting`: no cookie appears on its own, and a
    // page that retried forever would bury the one instruction that fixes it.
    expect(screen.queryByText(/^reconnecting/)).toBeNull()
  })

  it('says nothing about a token, because there is none to check', () => {
    expect(NO_SESSION_MESSAGE).not.toContain('token')
    expect(NO_SESSION_MESSAGE).toBe(
      'No session — open the URL that jpack-desk printed at startup.'
    )
  })

  it('keeps reconnecting where the chassis simply is not answering', async () => {
    // The other half: a 200 from `/api/session` would mean the session is
    // fine, and anything that is not a 401 — including a fetch that throws
    // because nothing is listening — is retryable.
    vi.stubGlobal('WebSocket', refusingSocket())
    vi.stubGlobal('fetch', async () => {
      throw new TypeError('Failed to fetch')
    })
    render(
      <QueryClientProvider client={testQueryClient()}>
        <McpProvider>
          <Says />
        </McpProvider>
      </QueryClientProvider>
    )
    await waitFor(() => expect(screen.getByText(/^reconnecting/)).toBeTruthy())
  })
})
