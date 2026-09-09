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
 * The key the page used to keep the desk's credential under, and does not any
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
  try {
    return window.sessionStorage.getItem(sessionStorageKey()) ?? ''
  } catch {
    return ''
  }
}

function hold(id: string): void {
  try {
    window.sessionStorage.setItem(sessionStorageKey(), id)
  } catch {
    // Storage refused. The session still works for this page's lifetime — the
    // id is in the promise below — it simply will not survive a reload.
  }
}

function drop(): void {
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
 * the code rather than of a comment.
 */
let inFlight: Promise<string> | null = null

/**
 * Begin a session, or return the one this tab already has.
 *
 * A `401` here is the end of the road and says so: the handoff has been spent,
 * has expired, or was never set because the printed URL was not opened.
 */
export async function sessionID(): Promise<string> {
  const held = heldSessionID()
  if (held) return held
  inFlight ??= beginSession()
  try {
    return await inFlight
  } catch (cause) {
    // A failed bootstrap is not cached: the chassis may simply have been down,
    // and a page that never retried would need a reload it does not tell the
    // person to make.
    inFlight = null
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
    // refusing — the caller retries this one.
    throw new Error('the desk chassis is not answering')
  }
  if (!answered.ok) throw new NoSession()
  const body = (await answered.json()) as { id?: unknown }
  if (typeof body.id !== 'string' || body.id === '') throw new NoSession()
  hold(body.id)
  return body.id
}

/**
 * Forget the id this tab holds and begin again.
 *
 * Called where the chassis answers `401` to a request carrying an id: the desk
 * was restarted, or the session was signed out or evicted, and the id names
 * nothing. One retry, and then the failure is the person's to act on.
 */
export async function renewSession(): Promise<string> {
  drop()
  inFlight = null
  return sessionID()
}

/** For a test that wants a clean module between cases. */
export function forgetSessionForTesting(): void {
  drop()
  inFlight = null
}

if (typeof window !== 'undefined') forgetStaleSessionToken()
