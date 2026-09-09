/**
 * The session this page holds, and the **one** bootstrap that gets it.
 *
 * # One actor, and why that is the whole design
 *
 * The page held a credential once as `?token=` on every URL, which put it in
 * the address bar, in `Referer`, in proxy logs and in `Response.url`. Then it
 * held none and the browser did, as a cookie — and a cookie is ambient: it has
 * no port, so every other service on `127.0.0.1` received it, and a script that
 * captured one could replay it. What this holds is a **bearer id**: nothing
 * sends it except code that means to, it is never on a URL, and it is scoped by
 * this module to the origin it came from.
 *
 * The shape:
 *
 *  1. `jpack-desk` prints `/launch?secret=…`. Opening it sets a one-shot,
 *     sixty-second handoff cookie and redirects to `/`.
 *  2. This module calls `POST /api/session` **exactly once per page load**,
 *     which spends that handoff and answers `{id, subject, issuer}`.
 *  3. The id goes in `sessionStorage` under a key naming this origin, and on
 *     every later request as `Authorization: Bearer <id>` — or, on the
 *     WebSocket upgrade, as a subprotocol offer, because a browser's
 *     `WebSocket` constructor has no header parameter and the id must not go on
 *     the URL.
 *
 * **`bootstrap()` is the only thing that reads or writes the stored id**, and
 * it runs once: the promise is memoised at module scope and never reset. That
 * is not an optimisation, it is the design. The shape this replaces had several
 * actors that could each read or replace the id during the page's life — the
 * bootstrap, a relaunch marker, renewal, refusal handling in three transports,
 * eviction on the chassis — and **every pair of them was a race**: five review
 * rounds closed instances and the next round found the next pair. One actor has
 * no pairs.
 *
 * # What a refusal means, and why nothing retries
 *
 * A `401` after the bootstrap means the id names nothing: the chassis restarted,
 * or the handoff was taken by something else before this page spent it. Only the
 * printed URL mints another, so the page **forgets the id and stops**
 * (`forgetSession`). That state is terminal until the next page load, which runs
 * `bootstrap()` again — and a handoff present then is spent, which is the whole
 * recovery flow: reopen the URL the desk printed.
 *
 * `sessionStorage` rather than `localStorage`: per tab, and cleared when the tab
 * is. The key names `host:port`, so two desks on two ports never read each
 * other's — and unlike a cookie, nothing on any other port ever receives this.
 */

/** What the page says when the chassis has no session for it. */
export const NO_SESSION_MESSAGE =
  'No session — open the URL that jpack-desk printed at startup.'

/**
 * Thrown where this page has no session the chassis will accept. It is not
 * retryable: no handoff appears on its own, and the fix is a person opening the
 * printed URL again.
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
 * The one exchange, memoised for the life of the page.
 *
 * Null until the first caller asks, and **never set back to null**. Ten
 * components mounting at once share one `POST`; a caller arriving after it has
 * settled gets the settled answer without a request. There is no path that
 * starts a second exchange, because a second exchange is a second actor.
 */
let bootstrapping: Promise<string | null> | null = null

/**
 * Whether a chassis call has refused the id this page holds.
 *
 * Once true it stays true for the life of the page: see the module comment.
 * It is separate from the memoised promise on purpose — the promise records
 * what the bootstrap *answered*, which does not change, and this records
 * whether that answer is still usable.
 */
let forgotten = false

/**
 * Begin this page's session, or answer the one it already has.
 *
 * Resolves the session id, or `null` where this page has none — it never
 * rejects, because "there is no session" is an answer every caller has to
 * handle and not a failure any of them can retry past.
 */
export function bootstrap(): Promise<string | null> {
  bootstrapping ??= beginSession()
  return bootstrapping
}

/**
 * The session id a request may carry, or `NoSession`.
 *
 * **Every chassis call goes through this**, which is what makes "each of them
 * awaits the bootstrap" a property of the code rather than of a comment: a call
 * issued before the exchange has answered sends nothing until it has, and a
 * call issued after a `401` sends nothing at all.
 */
export async function sessionBearer(): Promise<string> {
  const id = await bootstrap()
  if (forgotten || id === null || id === '') throw new NoSession()
  return id
}

/**
 * Forget the id this page holds, because the chassis refused it.
 *
 * **This is the end of the road, not a step towards a new one.** Called from
 * the three chassis transports and from the upgrade classifier, and it leaves
 * the page in the no-session state until it is loaded again.
 */
export function forgetSession(): void {
  forgotten = true
  try {
    window.sessionStorage.removeItem(sessionStorageKey())
  } catch {
    // A browser with storage disabled has nothing to remove, and a desk that
    // failed over that would be a worse desk than one that skips this.
  }
}

/**
 * The exchange itself: one `POST /api/session`, and what to do with each answer.
 *
 * - **200** — the handoff was spent for a fresh id. It replaces whatever was in
 *   storage, because the id the chassis just minted is the one the chassis
 *   knows and a stored one from an earlier process is not.
 * - **401** — no unspent handoff: the printed URL was not opened, or the
 *   handoff expired, or something else spent it. A stored id may still be live
 *   (this is a reload of a tab that bootstrapped earlier), so that is the
 *   answer; otherwise there is no session.
 * - **a network failure** — the chassis is not answering at all, which is not
 *   the same as refusing. The stored id is the answer for the same reason, and
 *   the requests that follow will fail on their own terms and say so.
 *
 * `credentials: 'same-origin'` is the **only** place this page sends a cookie.
 * Every other request omits them: the handoff is worth one call to this route
 * and belongs on no other.
 */
async function beginSession(): Promise<string | null> {
  const stored = storedSessionID()
  let answered: Response
  try {
    answered = await fetch('/api/session', { method: 'POST', credentials: 'same-origin' })
  } catch {
    return stored
  }
  if (!answered.ok) return stored
  let id: unknown
  try {
    id = ((await answered.json()) as { id?: unknown }).id
  } catch {
    return stored
  }
  if (typeof id !== 'string' || id === '') return stored
  hold(id)
  return id
}

/** The id in this tab's storage, or `null`. */
function storedSessionID(): string | null {
  try {
    const held = window.sessionStorage.getItem(sessionStorageKey())
    return held === null || held === '' ? null : held
  } catch {
    return null
  }
}

/**
 * Put the minted id in storage.
 *
 * A browser that refuses storage still gets a working desk for the life of the
 * page: the id the bootstrap resolved is what every caller awaits, and that
 * lives in the memoised promise rather than in storage. What such a browser
 * loses is the id surviving a reload.
 */
function hold(id: string): void {
  try {
    window.sessionStorage.setItem(sessionStorageKey(), id)
  } catch {
    // See above: the promise holds it either way.
  }
}

/**
 * Put this page in the state it is in on a fresh load, for a test.
 *
 * The memoised promise and the terminal flag are module state that outlives a
 * `beforeEach`, so a suite testing the bootstrap has to be able to clear them.
 * Nothing in the page calls this.
 */
export function resetSessionForTesting(): void {
  bootstrapping = null
  forgotten = false
}

/**
 * Hand this page a session without an exchange, for a test that is about
 * something else.
 *
 * The twenty-seven suites that stub `fetch` to answer the file API or the relay
 * are not about the bootstrap, and without this each would have to answer a
 * `POST /api/session` as well — so each would be asserting the bootstrap by
 * accident, and a change to it would break them all for a reason none of them
 * is about. `src/testing/setup.ts` calls this before every test; the bootstrap's
 * own suite calls `resetSessionForTesting` instead.
 */
export function giveThisPageASessionForTesting(id: string): void {
  bootstrapping = Promise.resolve(id)
  forgotten = false
}
