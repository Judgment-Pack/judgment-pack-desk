/**
 * The Desk-only cases of the harness session tests, against the Desk's own host.
 *
 * The rest of this file moved with the harness to `@judgment-pack/harness`
 * (`session.test.ts`), where it runs against the package's test host. The
 * cases kept here are the ones whose subject is a value only the Desk supplies
 * through `configureHarness` (`assistant/harnessHost.ts`): the session the
 * page holds (`mcp/session`), the error a page without one throws, and the
 * page's language (`i18n/locales`). So they hold the wiring, not only the code.
 */
import { afterEach, describe, expect, it, vi } from 'vitest'
import { assistantTransport, bindModelCall, runAssistantSession } from '@judgment-pack/harness/session'
import type { AssistantSession, Engine } from '@judgment-pack/harness/engine'
import { normalize } from '@judgment-pack/harness/thinking'
import { NoSession, giveThisPageASessionForTesting } from '../mcp/session'
import { rememberLanguage } from '../i18n/locales'

afterEach(() => {
  vi.unstubAllGlobals()
  window.sessionStorage.clear()
})

describe('the model capability the desk binds', () => {
  /**
   * Everything an adversarial engine can reach from one value, as strings.
   *
   * Own and inherited properties, whatever each of them stringifies to, the
   * text of any function it can reach, and the headers if there are any. It is
   * a sweep and not a proof — a getter that only answers on the third read
   * would evade it — but it is the shape the finding asked for, and it fails
   * on the defect it was written against.
   */
  function everythingReachable(value: unknown, depth = 0): string {
    if (depth > 3 || value === null || value === undefined) return String(value)
    const parts: string[] = []
    try {
      parts.push(String(value))
    } catch {
      /* a value that refuses to be a string tells us nothing */
    }
    if (typeof value === 'function') {
      try {
        parts.push(Function.prototype.toString.call(value))
      } catch {
        /* likewise */
      }
      return parts.join(' ')
    }
    if (typeof value !== 'object') return parts.join(' ')
    if (value instanceof Headers) {
      value.forEach((header, name) => parts.push(`${name}: ${header}`))
    }
    const seen = new Set<string>()
    for (
      let level: object | null = value;
      level !== null && level !== Object.prototype;
      level = Object.getPrototypeOf(level) as object | null
    ) {
      for (const key of Object.getOwnPropertyNames(level)) {
        if (seen.has(key)) continue
        seen.add(key)
        if (key === 'constructor') continue
        let read: unknown
        try {
          read = (value as Record<string, unknown>)[key]
        } catch {
          continue
        }
        parts.push(key, everythingReachable(read, depth + 1))
        // A stream is an object like any other: whatever somebody decorated it
        // with is reachable through whoever holds it.
        if (read instanceof ReadableStream) {
          for (const own of Object.getOwnPropertyNames(read)) {
            try {
              parts.push(own, String((read as unknown as Record<string, unknown>)[own]))
            } catch {
              /* a property that refuses to be read tells us nothing */
            }
          }
        }
      }
    }
    return parts.join(' ')
  }

  it('returns a facade an engine cannot read the relay address off', async () => {
    // A browser Response carries the requested URL on `.url`. That URL used to
    // be the relay address with this chassis' session token in it, so returning
    // the one fetch produced handed the engine everything it needed to open
    // /ws?token=… on a connection no gate is on. It carries no credential now,
    // and the facade is kept as defence in depth over this desk's own routing
    // — but the session id must still not be reachable through what comes
    // back, which is what this asserts.
    giveThisPageASessionForTesting('a-secret-session-id')
    vi.stubGlobal('fetch', async (url: unknown) => {
      // A response that knows where it came from, exactly as a browser's does.
      const real = new Response('{"ok":true}', {
        status: 200,
        headers: { 'content-type': 'application/json', 'x-echo': String(url), 'x-request-id':'req_fixture', 'retry-after':'2', 'request-id':'https://private.example' }
      })
      Object.defineProperty(real, 'url', { value: String(url), configurable: true })
      return real
    })
    const answered = await bindModelCall('openai-compatible')('chat/completions', { body: '{}' })
    const reachable = everythingReachable(answered)
    expect(answered.url).toBe('')
    expect(reachable).not.toContain('a-secret-session-id')
    expect(reachable).not.toContain('/api/assistant/relay')
    expect(reachable).not.toContain('token=')
    // What a loop does need is still there.
    expect(answered.status).toBe(200)
    expect(answered.headers.get('content-type')).toBe('application/json')
    expect(await answered.text()).toBe('{"ok":true}')
    // And the header the endpoint used to smuggle it back is not.
    expect(answered.headers.get('x-echo')).toBeNull()
    expect(answered.headers.get('x-request-id')).toBe('req_fixture')
    expect(answered.headers.get('retry-after')).toBe('2')
    expect(answered.headers.get('request-id')).toBeNull()
  })

  it('gives back a body stream of its own, not the one the answer arrived on', async () => {
    // A Response built from a ReadableStream keeps that very object as its
    // body, so a stream somebody decorated — a captured fetch, a patched
    // prototype — was reachable through the facade as `facade.body.leak`.
    giveThisPageASessionForTesting('a-secret-session-id')
    let upstream: ReadableStream<Uint8Array> | null = null
    vi.stubGlobal('fetch', async (url: unknown) => {
      const real = new Response('{"ok":true}', {
        status: 200,
        headers: { 'content-type': 'application/json' }
      })
      upstream = real.body
      // The decoration, on the stream itself.
      Object.defineProperty(real.body, 'leak', {
        value: `${String(url)}`,
        enumerable: true,
        configurable: true
      })
      return real
    })
    const answered = await bindModelCall('openai-compatible')('chat/completions', { body: '{}' })
    expect(answered.body).not.toBe(upstream)
    expect((answered.body as unknown as Record<string, unknown>).leak).toBeUndefined()
    expect(everythingReachable(answered)).not.toContain('a-secret-session-id')
    // And it is still the answer: the bytes come through the fresh stream.
    expect(await answered.text()).toBe('{"ok":true}')
  })

  it('carries no relay address through clone(), a reader, or an async iterator', async () => {
    // The walk reads `clone` as a function and never called it, so a clone's
    // own `url` and its own body were never looked at. Every door out of the
    // facade is opened here.
    giveThisPageASessionForTesting('a-secret-session-id')
    vi.stubGlobal('fetch', async (url: unknown) => {
      const real = new Response('{"ok":true}', {
        status: 200,
        headers: { 'content-type': 'application/json' }
      })
      Object.defineProperty(real, 'url', { value: String(url), configurable: true })
      Object.defineProperty(real.body, 'leak', {
        value: String(url),
        enumerable: true,
        configurable: true
      })
      return real
    })
    const answered = await bindModelCall('openai-compatible')('chat/completions', { body: '{}' })

    // A clone is a second Response built from the same facade: it must be as
    // empty of an address as the one it came from.
    const copy = answered.clone()
    expect(copy.url).toBe('')
    expect(everythingReachable(copy)).not.toContain('a-secret-session-id')
    expect(everythingReachable(copy)).not.toContain('/api/assistant/relay')

    // The reader, and the locked-state transition around it.
    expect(answered.body!.locked).toBe(false)
    const reader = answered.body!.getReader()
    expect(answered.body!.locked).toBe(true)
    expect(everythingReachable(reader)).not.toContain('a-secret-session-id')
    expect(everythingReachable(reader)).not.toContain('/api/assistant/relay')
    // A stream has no async iterator in this runtime unless one is defined;
    // whatever is there is walked rather than assumed absent.
    const iterator = (answered.body as unknown as Record<symbol, unknown>)[Symbol.asyncIterator]
    expect(everythingReachable(iterator)).not.toContain('a-secret-session-id')
    reader.releaseLock()

    // And the clone's own body, which is a different stream again.
    expect(copy.body).not.toBe(answered.body)
    expect((copy.body as unknown as Record<string, unknown>).leak).toBeUndefined()
    expect(await copy.text()).toBe('{"ok":true}')
  })

  it('reports an abort as a fresh AbortError, with no message or cause of its own', async () => {
    // A rejection named AbortError can carry the request URL in its message and
    // again in its cause. Rethrowing it whole handed the engine the address by
    // another door; the classification is all that travels.
    giveThisPageASessionForTesting('a-secret-session-id')
    vi.stubGlobal('fetch', async (url: unknown) => {
      const inner = new Error(`aborted while fetching ${String(url)}`)
      inner.name = 'AbortError'
      const outer = new Error(`aborted while fetching ${String(url)}`, { cause: inner })
      outer.name = 'AbortError'
      throw outer
    })
    const failure = (await bindModelCall('openai-compatible')('chat/completions', { body: '{}' }).catch(
      (error: unknown) => error
    )) as Error & { cause?: unknown }
    expect(failure.name).toBe('AbortError')
    expect(failure.cause).toBeUndefined()
    expect(everythingReachable(failure)).not.toContain('a-secret-session-id')
    expect(everythingReachable(failure)).not.toContain('/api/assistant/relay')
  })

  it('replaces the error a failed call throws, because a TypeError quotes the URL', async () => {
    giveThisPageASessionForTesting('a-secret-session-id')
    vi.stubGlobal('fetch', async (url: unknown) => {
      throw new TypeError(`Failed to fetch ${String(url)}`)
    })
    const failure = await bindModelCall('openai-compatible')('chat/completions', { body: '{}' }).catch(
      (error: unknown) => error
    )
    const reachable = everythingReachable(failure)
    expect(reachable).not.toContain('a-secret-session-id')
    expect(reachable).not.toContain('/api/assistant/relay')
    expect(failure).toMatchObject({failure:{code:'connection-lost',outcome:'unknown'}})
  })
})

describe('the assistant’s own connection', () => {
  it('refuses to build a transport for a page with no session', () => {
    // The offer is the only place a browser lets a page put a credential on a
    // handshake, so a transport built with no id would offer nothing and be
    // refused by the chassis with a status the browser withholds. Refusing
    // here says what is actually wrong.
    expect(() => assistantTransport('')).toThrow(NoSession)
  })
})

describe('session response language', () => {
  it('captures the language once and preserves an explicit session choice', async () => {
    const seen: AssistantSession[] = []
    const engine: Engine = {
      id: 'vercel',
      async *start(input) {
        if ('agent' in input) throw new Error('Unexpected agent session')
        seen.push(input)
        rememberLanguage('de')
        yield { type: 'end' }
      }
    }
    const input: AssistantSession = {
      prompt: 'source guidance', testPrompt: '', tools: [], hostTools: [],
      model: { family: 'gemini', model: 'model-id', call: async () => new Response('{}') },
      callTool: async () => ({ content: [] }),
      thinking: normalize('off', 'gemini'), signal: new AbortController().signal
    }
    try {
      rememberLanguage('fr')
      await runAssistantSession(engine, input, () => {})
      expect(seen[0]?.replyLanguage).toBe('fr')
      expect(input.replyLanguage).toBeUndefined()
      expect(seen[0]?.model).toBe(input.model)
      expect(seen[0]?.tools).toBe(input.tools)
      await runAssistantSession(engine, { ...input, replyLanguage: 'ja' }, () => {})
      expect(seen[1]?.replyLanguage).toBe('ja')
    } finally { rememberLanguage('en') }
  })
})
