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
import {
  ANOTHER_SESSION_MESSAGE,
  HANDOFF_EXPIRED_MESSAGE,
  HANDOFF_SPENT_MESSAGE,
  NO_SESSION_MESSAGE,
  NoSession,
  REFUSAL_HEADER,
  bootstrap,
  forgetSession,
  resetSessionForTesting,
  sessionBearer,
  mintedStorageKey,
  sessionEnded,
  sessionNotice,
  sessionStorageKey,
  whenSessionEnds
} from './session'
import { deskFetch, listFiles } from '../files/client'
import { bindModelCall, openAssistantConnection } from '../assistant/session'
import { BlockedNotice, ConnectionNotices, useBlockingError } from '../shell/ConnectionNotices'

/**
 * The shell's own notice, so that "the terminal UI is rendered" is a statement
 * about what a person sees rather than about a state member.
 */
function Notice() {
  const error = useBlockingError()
  return error === null ? <div data-connected="yes" /> : <BlockedNotice error={error} />
}

/** One request as the stub saw it. */
interface Seen {
  url: string
  method: string
  credentials?: RequestCredentials
  redirect?: RequestRedirect
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
      redirect: init?.redirect,
      authorization: headers.Authorization
    }
    seen.push(call)
    return answer(call)
  })
  return seen
}

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } })

/**
 * A refusal **this chassis authored**, marked as the chassis marks one.
 *
 * The mark is the whole discriminator on the relay route, so a test that
 * answered a bare 401 would be testing the wrong wire.
 */
const refused = (code: string, body: unknown, status = 401) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json', [REFUSAL_HEADER]: code }
  })

/** The exchange's own answer, and a plain success for everything else. */
const mints = (seen: Seen) =>
  seen.url === '/api/session' && seen.method === 'POST'
    ? json({ id: MINTED, subject: 'local user', issuer: null, sessions: { minted: 1 } })
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

  it('keeps the stored id when the exchange finds no handoff', async () => {
    // A reload of a tab that bootstrapped earlier: the handoff is long spent,
    // and the id this tab holds may well still be live. Throwing it away over
    // a 401 here would make a reload a sign-out.
    window.sessionStorage.setItem(sessionStorageKey(), STORED)
    const seen = record((call) =>
      call.url === '/api/session' ? refused('no-handoff', { error: 'no launch is in progress', code: 'no-handoff' }) : json({})
    )
    expect(await bootstrap()).toBe(STORED)
    expect(window.sessionStorage.getItem(sessionStorageKey())).toBe(STORED)
    expect(exchanges(seen)).toHaveLength(1)
  })

  it('lets go of a refused answer’s body, so no request stays in flight', async () => {
    // **A browser keeps a request in flight until its body stream is consumed
    // or cancelled.** A refusal this page reads the status of and nothing else
    // therefore sits there for ever — which is not visible in using the desk,
    // and is exactly what the containment gate caught: a page that never
    // reaches `networkidle` because one `POST /api/session` that answered 401
    // was still open.
    window.sessionStorage.setItem(sessionStorageKey(), STORED)
    let refusal: Response | undefined
    vi.stubGlobal('fetch', async (input: unknown) => {
      if (String(input) === '/api/session') {
        refusal = refused('no-handoff', { code: 'no-handoff' })
        return refusal
      }
      return json({})
    })
    expect(await bootstrap()).toBe(STORED)
    expect(refusal?.bodyUsed).toBe(true)
  })

  it('answers null when there is no handoff and this tab holds nothing', async () => {
    record((call) => (call.url === '/api/session' ? refused('no-handoff', { code: 'no-handoff' }) : json({})))
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

describe('a reload and a theft are told apart', () => {
  it('keeps the stored id on `no-handoff`, which is what a reload is', async () => {
    // The exchange clears the handoff it spends, so a reload inside the window
    // presents nothing at all. That tab holds an id that is very likely still
    // live, and a page that ended its session here would make every reload a
    // sign-out.
    window.sessionStorage.setItem(sessionStorageKey(), STORED)
    record((call) =>
      call.url === '/api/session'
        ? refused('no-handoff', { error: 'no launch is in progress', code: 'no-handoff' })
        : json({})
    )
    expect(await bootstrap()).toBe(STORED)
    expect(sessionEnded()).toBeNull()
    expect(await sessionBearer()).toBe(STORED)
  })

  it('forgets the id on `handoff-spent`, and says the link was used', async () => {
    // **The residual, happening, to an authenticated tab.** The person reopens
    // the printed URL; a script takes the handoff first; the page's own
    // exchange is refused with the code that says which failure it was.
    window.sessionStorage.setItem(sessionStorageKey(), STORED)
    record((call) =>
      call.url === '/api/session'
        ? refused('handoff-spent', {
            error: 'this launch link was already used',
            code: 'handoff-spent'
          })
        : json({})
    )
    expect(await bootstrap()).toBeNull()
    expect(window.sessionStorage.getItem(sessionStorageKey())).toBeNull()
    expect(sessionEnded()).toBe(HANDOFF_SPENT_MESSAGE)
    await expect(sessionBearer()).rejects.toThrow(HANDOFF_SPENT_MESSAGE)
    // Reopening the printed URL cannot fix it, so the sentence does not say to.
    expect(HANDOFF_SPENT_MESSAGE).toContain('Restart jpack-desk')
    expect(HANDOFF_SPENT_MESSAGE).not.toContain('open the URL that jpack-desk printed')
  })

  it('sends nothing at all after a spent handoff, on any transport', async () => {
    const seen = record((call) =>
      call.url === '/api/session'
        ? refused('handoff-spent', { code: 'handoff-spent' })
        : json({})
    )
    await expect(listFiles()).rejects.toThrow(HANDOFF_SPENT_MESSAGE)
    await expect(
      bindModelCall('openai-compatible')('chat/completions', { body: '{}' })
    ).rejects.toThrow(HANDOFF_SPENT_MESSAGE)
    expect(seen.filter((c) => c.url !== '/api/session')).toHaveLength(0)
    expect(exchanges(seen)).toHaveLength(1)
  })

  it('shows the chassis’ own sentence when the desk is at its capacity', async () => {
    // "Open the printed URL" is advice that cannot work here: a fresh tab
    // reopening it meets the same 503. So the chassis' own line is shown
    // verbatim, rather than a sentence this page composed.
    const said = 'this desk holds its maximum of sessions; restart it'
    record((call) =>
      call.url === '/api/session'
        ? refused('sessions-full', { error: said, code: 'sessions-full' }, 503)
        : json({})
    )
    expect(await bootstrap()).toBeNull()
    expect(sessionEnded()).toBe(said)
    await expect(sessionBearer()).rejects.toThrow(said)
  })

  it('treats an unmarked refusal as a reload rather than an ending', async () => {
    // A refusal this chassis did not author — a proxy, a captive portal —
    // carries no mark, and the safe reading of it is "try the id I hold".
    window.sessionStorage.setItem(sessionStorageKey(), STORED)
    record((call) => (call.url === '/api/session' ? json({ nope: true }, 502) : json({})))
    expect(await bootstrap()).toBe(STORED)
    expect(sessionEnded()).toBeNull()
  })
})

describe('the count that outlives the handoff', () => {
  it('stores the count the exchange reported beside the id', async () => {
    record(mints)
    await bootstrap()
    expect(window.sessionStorage.getItem(mintedStorageKey())).toBe('1')
    expect(sessionNotice()).toBeNull()
  })

  it('says so when the desk has minted more since this tab looked', async () => {
    // **This is the signal that survives the handoff.** Everything about a
    // stolen handoff is over inside sixty seconds; a tab reloading hours later
    // has nothing to read but this.
    window.sessionStorage.setItem(sessionStorageKey(), STORED)
    window.sessionStorage.setItem(mintedStorageKey(), '3')
    const seen = record((call) =>
      call.url === '/api/session' && call.method === 'POST'
        ? refused('no-handoff', { code: 'no-handoff' })
        : json({ subject: 'local user', issuer: null, sessions: { minted: 5 } })
    )
    expect(await bootstrap()).toBe(STORED)
    expect(sessionNotice()).toBe(ANOTHER_SESSION_MESSAGE)
    // The new count is stored, so the next reload does not say it again about
    // the same two sessions.
    expect(window.sessionStorage.getItem(mintedStorageKey())).toBe('5')
    // One read, and one only.
    expect(seen.filter((c) => c.url === '/api/session' && c.method === 'GET')).toHaveLength(1)
    // And it is a notice, not an ending: this tab's session still works.
    expect(sessionEnded()).toBeNull()
    expect(await sessionBearer()).toBe(STORED)
  })

  it('says nothing where the count has not grown', async () => {
    window.sessionStorage.setItem(sessionStorageKey(), STORED)
    window.sessionStorage.setItem(mintedStorageKey(), '5')
    record((call) =>
      call.url === '/api/session' && call.method === 'POST'
        ? refused('no-handoff', { code: 'no-handoff' })
        : json({ subject: 'local user', issuer: null, sessions: { minted: 5 } })
    )
    expect(await bootstrap()).toBe(STORED)
    expect(sessionNotice()).toBeNull()
  })

  it('says nothing on a first load, having nothing to compare', async () => {
    window.sessionStorage.setItem(sessionStorageKey(), STORED)
    record((call) =>
      call.url === '/api/session' && call.method === 'POST'
        ? refused('no-handoff', { code: 'no-handoff' })
        : json({ subject: 'local user', issuer: null, sessions: { minted: 9 } })
    )
    expect(await bootstrap()).toBe(STORED)
    expect(sessionNotice()).toBeNull()
    expect(window.sessionStorage.getItem(mintedStorageKey())).toBe('9')
  })

  it('asks for no count on the paths where it would mean nothing', async () => {
    // A tab that just minted its own session knows the count; a tab that is
    // ending has a better sentence. Neither spends a request on this.
    const seen = record(mints)
    await bootstrap()
    expect(seen.filter((c) => c.url === '/api/session' && c.method === 'GET')).toHaveLength(0)
  })

  it('shows the line once, in the shell’s own notice area', async () => {
    window.sessionStorage.setItem(sessionStorageKey(), STORED)
    window.sessionStorage.setItem(mintedStorageKey(), '1')
    record((call) =>
      call.url === '/api/session' && call.method === 'POST'
        ? refused('no-handoff', { code: 'no-handoff' })
        : json({ subject: 'local user', issuer: null, sessions: { minted: 2 } })
    )
    await bootstrap()
    render(
      <QueryClientProvider client={new QueryClient()}>
        <ConnectionNotices />
      </QueryClientProvider>
    )
    const said = document.body.textContent ?? ''
    expect(said).toContain(ANOTHER_SESSION_MESSAGE)
    expect(said.split(ANOTHER_SESSION_MESSAGE).length - 1).toBe(1)
    // The narration bound the shell holds every other sentence to.
    expect(ANOTHER_SESSION_MESSAGE.length).toBeLessThanOrEqual(140)
  })
})

describe('a lapsed link is not a stolen one', () => {
  it('says the link expired, and to reopen rather than restart', async () => {
    window.sessionStorage.setItem(sessionStorageKey(), STORED)
    record((call) =>
      call.url === '/api/session'
        ? refused('handoff-expired', { code: 'handoff-expired' })
        : json({})
    )
    expect(await bootstrap()).toBeNull()
    expect(sessionEnded()).toBe(HANDOFF_EXPIRED_MESSAGE)
    expect(window.sessionStorage.getItem(sessionStorageKey())).toBeNull()
    // An expiry is nobody's fault and the secret still works.
    expect(HANDOFF_EXPIRED_MESSAGE).toContain('Open the URL jpack-desk printed')
    expect(HANDOFF_EXPIRED_MESSAGE).not.toContain('Restart')
  })
})

describe('a reload inside the refusal’s delivery window', () => {
  it('still ends terminal, because the cookie was not cleared', async () => {
    // **The HIGH.** The clearing header used to travel in the same response as
    // the refusal, so a tab that reloaded after the browser stored the first
    // and before the page handled the second presented nothing and read
    // `no-handoff` — keeping its old session with the theft invisible. The
    // chassis clears only on success now, so the reload presents the same
    // cookie and reads the same verdict. This is the page's half: the second
    // load, with the handling of the first deferred, still ends terminal.
    window.sessionStorage.setItem(sessionStorageKey(), STORED)
    window.sessionStorage.setItem(mintedStorageKey(), '1')
    record((call) =>
      call.url === '/api/session' && call.method === 'POST'
        ? refused('handoff-spent', { code: 'handoff-spent' })
        : json({ subject: 'local user', issuer: null, sessions: { minted: 2 } })
    )

    // The first load's refusal, deliberately not awaited: the reload happens
    // while it is still in flight.
    const first = bootstrap()

    // The reload — a fresh page, so the module state starts again.
    resetSessionForTesting()
    expect(await bootstrap()).toBeNull()
    expect(sessionEnded()).toBe(HANDOFF_SPENT_MESSAGE)
    expect(window.sessionStorage.getItem(sessionStorageKey())).toBeNull()
    await first
  })

  it('would have shown the count in any case', async () => {
    // And if the cookie had gone — the ring forgetting it, say — the count is
    // still higher than the one this tab stored, so the reload says so.
    window.sessionStorage.setItem(sessionStorageKey(), STORED)
    window.sessionStorage.setItem(mintedStorageKey(), '1')
    record((call) =>
      call.url === '/api/session' && call.method === 'POST'
        ? refused('no-handoff', { code: 'no-handoff' })
        : json({ subject: 'local user', issuer: null, sessions: { minted: 2 } })
    )
    expect(await bootstrap()).toBe(STORED)
    expect(sessionNotice()).toBe(ANOTHER_SESSION_MESSAGE)
  })
})

describe('the one subscription a session’s end is published on', () => {
  it('tells every listener exactly once, however many callers forget', async () => {
    record(mints)
    await bootstrap()
    let told = 0
    const stop = whenSessionEnds(() => {
      told += 1
    })
    forgetSession()
    forgetSession(HANDOFF_SPENT_MESSAGE)
    expect(told).toBe(1)
    // And the first reason is the one that stands: the session ended once.
    expect(sessionEnded()).toBe(NO_SESSION_MESSAGE)
    stop()
  })

  it('stops telling a listener that unsubscribed', async () => {
    record(mints)
    await bootstrap()
    let told = 0
    whenSessionEnds(() => {
      told += 1
    })()
    forgetSession()
    expect(told).toBe(0)
  })

  it('carries on telling the others when one listener throws', async () => {
    record(mints)
    await bootstrap()
    let told = 0
    const stopFirst = whenSessionEnds(() => {
      throw new Error('a subscriber came apart')
    })
    const stopSecond = whenSessionEnds(() => {
      told += 1
    })
    forgetSession()
    expect(told).toBe(1)
    stopFirst()
    stopSecond()
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
        : refused('unauthorized', { error: 'no session', code: 'unauthorized' })
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

  it('lets go of the 401 it refused on, on a chassis call too', async () => {
    const refusals: Response[] = []
    vi.stubGlobal('fetch', async (input: unknown) => {
      if (String(input) === '/api/session') return json({ id: MINTED })
      const refusal = refused('unauthorized', { error: 'no session', code: 'unauthorized' })
      refusals.push(refusal)
      return refusal
    })
    await expect(listFiles()).rejects.toThrow(NoSession)
    expect(refusals).toHaveLength(1)
    expect(refusals[0]!.bodyUsed).toBe(true)
  })

  it('ends the assistant’s relay too, and it re-sends nothing', async () => {
    const seen = record((call) =>
      call.url === '/api/session'
        ? json({ id: MINTED })
        : refused('unauthorized', { error: 'no session', code: 'unauthorized' })
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
    // be this desk reading one refusal as another. **The endpoint's answer
    // carries no mark**, because the chassis strips it from every upstream
    // answer in every casing and from trailers.
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

  it('delivers a chunked oversized endpoint 401 to the engine, promptly', async () => {
    // **The classifier this replaced would have deadlocked here.** It read a
    // `clone()` up to four kilobytes, and a clone has tee semantics: an answer
    // larger than the bound, arriving in chunks, leaves the reader waiting for
    // a stream nobody is draining. Nothing reads a relayed body now — the mark
    // is a header — so this simply travels.
    const chunk = 2 << 10
    const chunks = 16 // 32 KiB
    record((call) => {
      if (call.url === '/api/session') return json({ id: MINTED })
      let sent = 0
      const body = new ReadableStream<Uint8Array>({
        pull(controller) {
          if (sent === chunks) {
            controller.close()
            return
          }
          sent += 1
          controller.enqueue(new TextEncoder().encode('x'.repeat(chunk)))
        }
      })
      // No mark: this is the endpoint's own refusal, and the chassis strips the
      // mark from every upstream answer.
      return new Response(body, { status: 401, headers: { 'content-type': 'application/json' } })
    })

    const began = Date.now()
    const answered = await bindModelCall('openai-compatible')('chat/completions', { body: '{}' })
    expect(answered.status).toBe(401)
    const said = await answered.text()
    const took = Date.now() - began
    expect(said).toHaveLength(chunk * chunks)
    // Promptly: a reader that was waiting on a tee would not have returned at
    // all. Five seconds is far past this and far short of any real timeout.
    expect(took).toBeLessThan(5000)
    // And the session is untouched, because the answer was the endpoint's.
    expect(sessionEnded()).toBeNull()
    expect(window.sessionStorage.getItem(sessionStorageKey())).toBe(MINTED)
  })

  it('ends the session on a marked 401 from the relay, whatever the body', async () => {
    // The positive control for the row above: the same route, the same status,
    // the mark present — and the mark is the whole of the difference.
    record((call) =>
      call.url === '/api/session'
        ? json({ id: MINTED })
        : refused('unauthorized', { error: 'no session', code: 'unauthorized' })
    )
    await expect(
      bindModelCall('openai-compatible')('chat/completions', { body: '{}' })
    ).rejects.toThrow(NoSession)
    expect(sessionEnded()).toBe(NO_SESSION_MESSAGE)
  })

  it('ends the session only on `unauthorized`, and on no other marked code', async () => {
    // **A `307 Location: /api/session` made this very fetch repeat itself**
    // against the exchange — method, body and this desk's bearer included —
    // and the exchange's marked `no-handoff` was then read as "this desk
    // refused my session". The chassis strips `Location`; this is the other
    // half: only the code that actually means "your session is not one" ends
    // it. The exchange's own codes are answers to a question this route never
    // asks.
    for (const code of ['no-handoff', 'handoff-spent', 'handoff-expired', 'sessions-full', 'bad-request']) {
      resetSessionForTesting()
      window.sessionStorage.clear()
      record((call) =>
        call.url === '/api/session'
          ? json({ id: MINTED, sessions: { minted: 1 } })
          : refused(code, { code })
      )
      const answered = await bindModelCall('openai-compatible')('chat/completions', { body: '{}' })
      expect(answered.status, code).toBe(401)
      expect(sessionEnded(), code).toBeNull()
      expect(window.sessionStorage.getItem(sessionStorageKey()), code).toBe(MINTED)
    }

    // The control: `unauthorized` does end it.
    resetSessionForTesting()
    window.sessionStorage.clear()
    record((call) =>
      call.url === '/api/session'
        ? json({ id: MINTED, sessions: { minted: 1 } })
        : refused('unauthorized', { code: 'unauthorized' })
    )
    await expect(
      bindModelCall('openai-compatible')('chat/completions', { body: '{}' })
    ).rejects.toThrow(NoSession)
  })

  it('reports an opaque redirect as an upstream failure, not as an answer', async () => {
    // `redirect: 'manual'` answers a `Response` of type `opaqueredirect` with
    // status 0 and no body. An engine handed that would be handed nothing it
    // could read, so it is reported as what it is.
    record((call) => {
      if (call.url === '/api/session') return json({ id: MINTED, sessions: { minted: 1 } })
      const opaque = new Response(null, { status: 0 })
      Object.defineProperty(opaque, 'type', { value: 'opaqueredirect' })
      return opaque
    })
    await expect(
      bindModelCall('openai-compatible')('chat/completions', { body: '{}' })
    ).rejects.toThrow(/could not be made/)
    expect(sessionEnded()).toBeNull()
  })

  it('asks the browser not to follow a redirect at all', async () => {
    const seen = record((call) =>
      call.url === '/api/session' ? json({ id: MINTED, sessions: { minted: 1 } }) : json({ ok: true })
    )
    await bindModelCall('openai-compatible')('chat/completions', { body: '{}' })
    const relayed = seen.filter((c) => c.url.startsWith('/api/assistant/relay'))
    expect(relayed[0]!.redirect).toBe('manual')
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
          <Notice />
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

  it('opens no socket once a refusal has ended this page’s session', async () => {
    // **The terminal state reaches the socket too.** The bootstrap's promise
    // goes on answering the id it minted, because that is what it minted; a
    // provider that read it directly would reconnect for ever with an id the
    // chassis has already rejected.
    record(mints)
    await bootstrap()
    forgetSession()
    const dialled = recordingSockets()
    render(
      <QueryClientProvider client={new QueryClient()}>
        <McpProvider>
          <Notice />
        </McpProvider>
      </QueryClientProvider>
    )
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(dialled).toEqual([])
  })

  it('opens no socket at all for a page with no session', async () => {
    record((call) => (call.url === '/api/session' ? refused('no-handoff', { code: 'no-handoff' }) : json({})))
    const dialled = recordingSockets()
    render(
      <QueryClientProvider client={new QueryClient()}>
        <McpProvider>
          <Notice />
        </McpProvider>
      </QueryClientProvider>
    )
    // Given time to do the wrong thing, and it does not.
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(dialled).toEqual([])
  })
})

describe('the terminal state reaches the sockets, not only the requests', () => {
  /**
   * A `WebSocket` that opens, answers an MCP `initialize`, and **records every
   * frame the page sends**.
   *
   * The property is that frames stop, so counting them is the measurement. A
   * stub that never opened would make every assertion below vacuous, which is
   * why the socket completes its handshake first.
   */
  function speakingSockets() {
    const sockets: {
      sent: string[]
      closed: boolean
      protocols?: string | string[]
    }[] = []
    class Speaking {
      // The transport asks `readyState !== WebSocket.OPEN` before it sends, and
      // `WebSocket` is this class once it is stubbed — so the constants have to
      // be here or every send is refused for the wrong reason.
      static readonly CONNECTING = 0
      static readonly OPEN = 1
      static readonly CLOSING = 2
      static readonly CLOSED = 3
      onopen?: () => void
      onerror?: () => void
      onclose?: () => void
      onmessage?: (event: { data: string }) => void
      readyState = 1
      private readonly record: { sent: string[]; closed: boolean; protocols?: string | string[] }
      constructor(_url: string, protocols?: string | string[]) {
        this.record = { sent: [], closed: false, protocols }
        sockets.push(this.record)
        setTimeout(() => this.onopen?.(), 0)
      }
      send(raw: string) {
        this.record.sent.push(raw)
        const message = JSON.parse(raw) as { id?: number; method?: string }
        if (message.id === undefined) return
        const answer =
          message.method === 'initialize'
            ? {
                protocolVersion: '2025-06-18',
                capabilities: {},
                serverInfo: { name: 'stub', version: '0' }
              }
            : { tools: [] }
        setTimeout(
          () => this.onmessage?.({ data: JSON.stringify({ jsonrpc: '2.0', id: message.id, result: answer }) }),
          0
        )
      }
      close() {
        this.record.closed = true
        this.readyState = 3
        this.onclose?.()
      }
    }
    vi.stubGlobal('WebSocket', Speaking)
    return sockets
  }

  it('closes both connections, refuses further calls, and says so once', async () => {
    const seen = record((call) => {
      if (call.url === '/api/session' && call.method === 'POST') return json({ id: MINTED })
      if (call.url === '/api/files') return refused('unauthorized', { code: 'unauthorized' })
      return json({})
    })
    const sockets = speakingSockets()

    // **Both connections up first.** The desk's own, through the provider, and
    // the assistant's, through the same code path a run uses.
    const { rerender } = render(
      <QueryClientProvider client={new QueryClient()}>
        <McpProvider>
          <Notice />
        </McpProvider>
      </QueryClientProvider>
    )
    // StrictMode double-mount, modelled by rendering the same tree again: the
    // effect is set up twice, so the subscription must be torn down once per
    // set-up or the ending is announced twice.
    rerender(
      <QueryClientProvider client={new QueryClient()}>
        <McpProvider>
          <Notice />
        </McpProvider>
      </QueryClientProvider>
    )
    await waitFor(() => expect(sockets.length).toBeGreaterThan(0))

    const assistant = openAssistantConnection({
      allowed: ['get_schema'],
      onEvent: () => {},
      sessionId: MINTED
    })
    const gate = await assistant.ready
    await waitFor(() => expect(sockets.length).toBeGreaterThan(1))
    const before = sockets.map((socket) => socket.sent.length)
    expect(before.every((n) => n > 0)).toBe(true)

    // **The refusal, through an ordinary chassis call.** Nothing tells the
    // sockets directly; the one subscription does.
    await expect(listFiles()).rejects.toThrow(NoSession)

    await waitFor(() => expect(sockets.every((socket) => socket.closed)).toBe(true))
    const after = sockets.map((socket) => socket.sent.length)
    expect(after, 'a frame was sent after the session ended').toEqual(before)

    // The gated connection delivers nothing, and says why rather than
    // reporting a closed transport.
    await expect(gate.callTool('get_schema', {})).rejects.toThrow(NoSession)
    // And a run started now opens nothing at all.
    const late = openAssistantConnection({
      allowed: ['get_schema'],
      onEvent: () => {},
      sessionId: MINTED
    })
    await expect(late.ready).rejects.toThrow(NoSession)
    expect(sockets).toHaveLength(after.length)

    // Queries refuse to send: no request of any kind followed the refusal.
    const settled = seen.length
    await expect(listFiles()).rejects.toThrow(NoSession)
    expect(seen).toHaveLength(settled)

    // The notice is rendered, and **once**: the sentence is in the document one
    // time however many times the effect was set up.
    await waitFor(() =>
      expect(document.body.textContent).toContain('open the URL that jpack-desk printed')
    )
    const said = document.body.textContent ?? ''
    const times = said.split('open the URL that jpack-desk printed').length - 1
    expect(times, said.slice(0, 400)).toBe(1)
  })
})

describe('nothing is delivered after the session ends', () => {
  /** A transport whose answers this test hands over when it chooses. */
  function heldTransport() {
    const waiting: (() => void)[] = []
    let onmessage: ((message: unknown) => void) | undefined
    const transport = {
      start: () => Promise.resolve(),
      close: () => Promise.resolve(),
      send: (message: { id?: number; method?: string }) => {
        if (message.id === undefined) return Promise.resolve()
        const answer =
          message.method === 'initialize'
            ? {
                protocolVersion: '2025-06-18',
                capabilities: {},
                serverInfo: { name: 'held', version: '0' }
              }
            : message.method === 'tools/list'
              ? { tools: [{ name: 'get_schema', inputSchema: { type: 'object' } }] }
              : { content: [{ type: 'text', text: 'a tool answered' }] }
        const deliver = () =>
          onmessage?.({ jsonrpc: '2.0', id: message.id, result: answer })
        // The setup's own messages settle at once; a `tools/call` is held so
        // that the test can end the session while it is in flight.
        if (message.method === 'tools/call') waiting.push(deliver)
        else setTimeout(deliver, 0)
        return Promise.resolve()
      },
      set onmessage(handler: (message: unknown) => void) {
        onmessage = handler
      },
      get onmessage() {
        return onmessage as (message: unknown) => void
      },
      onclose: undefined,
      onerror: undefined
    }
    return { transport, release: () => waiting.splice(0).forEach((deliver) => deliver()) }
  }

  it('drops a tool result that was already in flight', async () => {
    record(mints)
    await bootstrap()
    const held = heldTransport()
    const connection = openAssistantConnection({
      allowed: ['get_schema'],
      onEvent: () => {},
      transport: held.transport as never
    })
    const gate = await connection.ready

    const calling = gate.callTool('get_schema', {})
    // The session ends while the call is in flight, and the answer then
    // arrives. Delivering it would be this desk handing an engine the answer to
    // a question asked on a session the chassis had refused.
    forgetSession()
    held.release()
    await expect(calling).rejects.toThrow(NoSession)
  })

  it('refuses a call queued after the end, before it sends anything', async () => {
    record(mints)
    await bootstrap()
    const held = heldTransport()
    const connection = openAssistantConnection({
      allowed: ['get_schema'],
      onEvent: () => {},
      transport: held.transport as never
    })
    const gate = await connection.ready
    forgetSession()
    await expect(gate.callTool('get_schema', {})).rejects.toThrow(NoSession)
  })

  it('opens nothing for a run started between the refusal and the close', async () => {
    record(mints)
    await bootstrap()
    forgetSession()
    const dialled: string[] = []
    class Spy {
      constructor(url: string) {
        dialled.push(url)
      }
      close() {}
    }
    vi.stubGlobal('WebSocket', Spy)
    const late = openAssistantConnection({
      allowed: ['get_schema'],
      onEvent: () => {},
      sessionId: MINTED
    })
    await expect(late.ready).rejects.toThrow(NoSession)
    expect(dialled).toEqual([])
  })
})

describe('what the page says when it has none', () => {
  it('is one sentence, and it names the way back', () => {
    // The README quotes this and `ConnectionNotices` renders it: the page's
    // whole recovery flow is a person reopening the printed URL, so the
    // sentence has to say that rather than "not connected".
    expect(new NoSession().message).toBe(NO_SESSION_MESSAGE)
    expect(NO_SESSION_MESSAGE).toContain('open the URL that jpack-desk printed at startup')
    expect(NO_SESSION_MESSAGE.split('\n')).toHaveLength(1)
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
