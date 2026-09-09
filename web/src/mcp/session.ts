/**
 * The session this page holds, and the bootstrap that gets it.
 *
 * **The page holds a credential again, deliberately, and that is the point.**
 * It held one before as `?token=` on every URL, which put it in the address
 * bar, in `Referer`, in proxy logs and in `Response.url`. Then it held none and
 * the browser did, as a cookie — and a cookie is ambient: it has no port, so
 * every other service on `127.0.0.1` received it, and a script that captured
 * one could replay it. What this holds is a **bearer id**: nothing sends it
 * except code that means to, it is never on a URL, and it is scoped by this
 * module to the origin it came from.
 *
 * The shape:
 *
 *  1. `jpack-desk` prints `/launch?secret=…`. Opening it sets a one-shot,
 *     sixty-second handoff cookie and redirects to `/`.
 *  2. This module calls `POST /api/session` **once**, which spends that handoff
 *     and answers `{id, subject, issuer}`.
 *  3. The id goes in `sessionStorage` under a key naming this origin, and on
 *     every later request as `Authorization: Bearer <id>` — or, on the
 *     WebSocket upgrade, as a subprotocol offer, because a browser's
 *     `WebSocket` constructor has no header parameter and the id must not go on
 *     the URL.
 *
 * `sessionStorage` rather than `localStorage`: per tab, and cleared when the
 * tab is. The key names `host:port`, so two desks on two ports never read each
 * other's — and unlike a cookie, nothing on any other port ever receives this.
 *
 * **There is no renewal.** A chassis that refuses an id has restarted, or the
 * session was signed out or evicted; the only thing that mints another is the
 * printed URL, so the page clears what it holds and says so. Trying again
 * automatically would be a page pretending it can recover from something only a
 * person can.
 */

/** What the page says when the chassis has no session for it. */
export const NO_SESSION_MESSAGE =
  'No session — open the URL that jpack-desk printed at startup.'

/**
 * Thrown where the chassis will not begin a session. It is not retryable: no
 * handoff appears on its own, and the fix is a person opening the printed URL.
 */
export class NoSession extends Error {
  constructor() {
    super(NO_SESSION_MESSAGE)
    this.name = 'NoSession'
  }
}

/**
 * The storage key, **naming the origin**.
 *
 * `window.location.host` is `host:port`, so a desk on 8791 and a desk on 8899
 * keep different ids in the same browser and neither can read the other's. The
 * cookie this replaced could not be scoped that way at all: a cookie's origin
 * has no port.
 */
export function sessionStorageKey(): string {
  return `jpack-desk-session:${window.location.host}`
}

/**
 * The marker the launch sets beside its `HttpOnly` handoff, carrying no secret
 * and saying only that one is waiting.
 *
 * It exists because the handoff is `HttpOnly` — which is what stops page code
 * reading it, and therefore also what stops page code knowing there is one. See
 * `pendingCookiePrefix` in `internal/desk/session.go`.
 */
const MARKER_PREFIX = 'jpack-desk-handoff-pending'

/** Every marker in this browser's jar for this host, by name. */
function markersHere(): string[] {
  try {
    return document.cookie
      .split(';')
      .map((pair) => pair.trim().split('=')[0] ?? '')
      .filter((name) => name.startsWith(`${MARKER_PREFIX}-`))
  } catch {
    return []
  }
}

/**
 * The marker this page may act on, or `''`.
 *
 * **Exact, by port, because a cookie has no port and two desks share a jar.**
 * A page on `127.0.0.1:8791` sees the marker of a desk on `127.0.0.1:8899` as
 * well as its own; acting on the wrong one spends nothing and — worse — used to
 * *clear* the other desk's marker, so that desk's page never spent its handoff.
 * So the name must be this page's own port.
 *
 * **The one exception is a proxy**, and it is stated rather than inferred: under
 * `npm run dev` the page is on 5173 and the chassis on 8791, so no marker can
 * match the page's port. Where there is exactly one marker and none matches,
 * it is unambiguous and it is taken. Where there are several and none matches,
 * nothing is done — a dev server proxying two desks is not a thing this desk
 * supports, and guessing would be worse than waiting.
 */
export function markerForThisPage(): string {
  const markers = markersHere()
  const mine = `${MARKER_PREFIX}-${window.location.port}`
  if (markers.includes(mine)) return mine
  return markers.length === 1 ? (markers[0] as string) : ''
}

/**
 * The key this page used to keep the desk's credential under, and does not any
 * more. A stale one left by an older build is removed rather than left to sit.
 */
const STALE_TOKEN_KEY = 'jpack-desk-token'

export function forgetStaleSessionToken(): void {
  try {
    window.sessionStorage.removeItem(STALE_TOKEN_KEY)
  } catch {
    // A browser with storage disabled has nothing to forget, and a desk that
    // refused to load over it would be a worse desk than one that skips this.
  }
}

/** The id this tab is holding, or the empty string. */
export function heldSessionID(): string {
  if (inMemory !== '') return inMemory
  try {
    return window.sessionStorage.getItem(sessionStorageKey()) ?? ''
  } catch {
    return ''
  }
}

/**
 * The id this tab holds when storage will not keep one.
 *
 * A browser with storage disabled still gets a working desk for the life of the
 * page; what it loses is the id surviving a reload. Both connections read
 * through `sessionID()`, so both see this.
 */
let inMemory = ''

function hold(id: string): void {
  inMemory = id
  try {
    window.sessionStorage.setItem(sessionStorageKey(), id)
  } catch {
    // Storage refused. The id is in memory above, which is what this page
    // needs; it simply will not survive a reload.
  }
}

/**
 * Forget the id this tab holds.
 *
 * **This is the end of the road, not a step towards a new one.** A chassis that
 * refuses an id has restarted, or the session was signed out or evicted, and
 * the only thing that mints another is the printed URL. The page says so; it
 * does not try again.
 */
export function forgetSession(): void {
  inMemory = ''
  try {
    window.sessionStorage.removeItem(sessionStorageKey())
  } catch {
    // Nothing to drop.
  }
}

/**
 * The one exchange in flight, so that a page which mounts eight queries at once
 * spends **one** handoff rather than eight.
 *
 * The handoff is single use: the first `POST` wins and the other seven would be
 * `401`. Memoising the promise is what makes "the page calls this once" true of
 * the code rather than of a comment — and it is cleared when the promise
 * settles either way, so a *later* marker (a relaunch after this one finished)
 * starts a new exchange rather than being answered from a stale promise.
 */
let inFlight: Promise<string> | null = null

/**
 * Begin a session, or return the one this tab already has.
 *
 * **A waiting handoff is always spent, even by a tab that already has an id.**
 * Reopening the printed URL in a tab that had one used to leave the new handoff
 * sitting in the jar for its full sixty seconds — unspent, and worth a session
 * to anything that could capture it. So a relaunch replaces the stored id, and
 * every caller that arrives while that exchange is in flight receives the same
 * new id rather than starting one of its own.
 *
 * A `401` here is the end of the road and says so: the handoff has been spent,
 * has expired, or was never set because the printed URL was not opened.
 */
export async function sessionID(): Promise<string> {
  const held = heldSessionID()
  if (held && markerForThisPage() === '') return held
  if (inFlight === null) {
    inFlight = beginSession().finally(() => {
      inFlight = null
    })
  }
  try {
    return await inFlight
  } catch (cause) {
    // **A relaunch that failed keeps what the tab had.** The handoff may have
    // been spent by something else, and a page that threw away a working
    // session over it would turn that into an outage for the person who is
    // legitimately here.
    if (held) return held
    throw cause
  }
}

async function beginSession(): Promise<string> {
  let answered: Response
  try {
    answered = await fetch('/api/session', {
      method: 'POST',
      // **The one request that sends a cookie**, because the handoff is one.
      // Every other request in this desk sends `credentials: 'omit'`.
      credentials: 'same-origin'
    })
  } catch {
    // The chassis is not answering at all, which is a different thing from
    // refusing — the caller may try again.
    throw new Error('the desk chassis is not answering')
  }
  if (!answered.ok) throw new NoSession()
  const body = (await answered.json()) as { id?: unknown }
  if (typeof body.id !== 'string' || body.id === '') throw new NoSession()
  hold(body.id)
  return body.id
}

/** For a test that wants a clean module between cases. */
export function forgetSessionForTesting(): void {
  forgetSession()
  inFlight = null
}

if (typeof window !== 'undefined') forgetStaleSessionToken()
