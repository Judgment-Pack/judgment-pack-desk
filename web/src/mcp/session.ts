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
 * What the page says when its handoff **lapsed** rather than being taken.
 *
 * A different instruction, because it is a different fact: nobody used the
 * link, sixty seconds simply went by, and the launch secret still works. So the
 * answer is to *reopen* the printed URL rather than to restart.
 */
export const HANDOFF_EXPIRED_MESSAGE =
  'The launch link expired before this page loaded. Open the URL jpack-desk printed at startup.'

/**
 * Thrown where this page has no session the chassis will accept. It is not
 * retryable: no handoff appears on its own, and what fixes it is a person — the
 * message says which thing they should do.
 */
export class NoSession extends Error {
  constructor(message: string = NO_SESSION_MESSAGE) {
    super(message)
    this.name = 'NoSession'
  }
}

/**
 * The header the chassis marks **its own** refusals with, and the code it puts
 * in it.
 *
 * One route on this desk forwards somebody else's answer — the model relay — so
 * a `401` there is either this chassis refusing the session or the configured
 * endpoint refusing the stored key. Ending a person's desk session because
 * their model key expired would be this page reading one refusal as another.
 *
 * **Read off a header rather than out of a body**, and both halves of that
 * matter. A body has to be consumed to be read, and a *cloned* body has tee
 * semantics — an oversized chunked answer deadlocks the reader that is trying
 * to classify it. And an endpoint can write any body it likes, so a body-borne
 * discriminator was forgeable; the chassis strips this header from every
 * upstream answer, in every casing and from trailers, so this one is not.
 */
export const REFUSAL_HEADER = 'X-Jpack-Desk-Refusal'

/** The code this chassis marked an answer with, or `null` where it did not. */
export function refusalCode(answered: Response): string | null {
  return answered.headers.get(REFUSAL_HEADER)
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
let forgotten: string | null = null

/**
 * The one subscription a session's end is published on.
 *
 * **Why an emitter and not a poll.** A `401` on a `fetch` is noticed by the
 * caller that made it; an MCP socket that is already open notices nothing at
 * all, and went on carrying frames for a session the chassis had refused. Both
 * connections subscribe here, and `forgetSession` publishes once — so the
 * terminal state reaches the sockets rather than only the requests.
 *
 * It is not a second actor over the credential: a listener is told that the
 * session ended, and reads and writes nothing.
 */
const ending = new Set<() => void>()

/**
 * Be told when this page's session ends, and stop being told.
 *
 * The returned function unsubscribes, and a caller that mounts twice — React
 * StrictMode does — must call it, which is what keeps the terminal state from
 * being announced twice.
 */
export function whenSessionEnds(listener: () => void): () => void {
  ending.add(listener)
  return () => {
    ending.delete(listener)
  }
}

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
  if (forgotten !== null) throw new NoSession(forgotten)
  if (id === null || id === '') throw new NoSession()
  return id
}

/**
 * The sentence this page's session ended with, or `null` while it has not.
 *
 * Synchronous, for the renderers: what a person is told depends on *which*
 * ending it was — a desk that restarted, a launch link somebody else used, or a
 * desk at its capacity — and each says a different thing to do.
 */
export function sessionEnded(): string | null {
  return forgotten
}

/**
 * Forget the id this page holds, because the chassis refused it.
 *
 * **This is the end of the road, not a step towards a new one.** Called from
 * the three chassis transports and from the upgrade classifier, and it leaves
 * the page in the no-session state until it is loaded again.
 */
export function forgetSession(reason: string = NO_SESSION_MESSAGE): void {
  // **Once, whoever calls it.** Two transports can meet the same refusal in the
  // same tick, and a page that announced the end twice would tear down twice
  // and render the notice twice.
  if (forgotten !== null) return
  forgotten = reason
  try {
    window.sessionStorage.removeItem(sessionStorageKey())
  } catch {
    // A browser with storage disabled has nothing to remove, and a desk that
    // failed over that would be a worse desk than one that skips this.
  }
  // A copy, because a listener may unsubscribe itself while being told.
  for (const listener of [...ending]) {
    try {
      listener()
    } catch {
      // A subscriber that throws on the way down does not stop the others
      // being told; there is nothing left for this page to do about it.
    }
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
  if (!answered.ok) return await refusedExchange(answered, stored)
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

/**
 * What a refused exchange means, which is **three different things**.
 *
 *  - **`handoff-spent`** — a handoff was presented and this desk no longer
 *    holds it: somebody took it inside its sixty seconds, or it expired. This
 *    is the stated residual actually happening, and it ends the page's session:
 *    reopening the printed URL cannot help, because the secret is reusable and
 *    whatever took one handoff takes the next.
 *  - **`sessions-full`** — this desk holds as many sessions as it will. The
 *    chassis' own sentence is shown **verbatim**, because "open the printed
 *    URL" is advice that cannot work here: a fresh tab reopening it gets the
 *    same 503.
 *  - **anything else, `no-handoff` included** — nobody opened the printed URL
 *    for this browser, or far more often this page is simply **reloading** after
 *    its own handoff was spent and cleared. A tab in that state holds an id
 *    that is very likely still live, so it keeps it; if it is not live, the
 *    first chassis call answers a marked `401` and ends the session then.
 *
 * **`handoff-spent` is deliberately not its own case.** The chassis separates
 * it from `no-handoff` so that a spent handoff can be told from an expired one;
 * it cannot tell this tab's own earlier spend from anybody else's, and neither
 * can this page, so acting on it would end live sessions on a guess. Four
 * review rounds tried to turn it into a visible signal and each attempt was a
 * defect on the same seam; the line of work is withdrawn.
 */
async function refusedExchange(answered: Response, stored: string | null): Promise<string | null> {
  const code = refusalCode(answered)
  if (answered.status === 503 && code === 'sessions-full') {
    forgetSession(await sentenceOf(answered))
    return null
  }
  await discardBody(answered)
  if (code === 'handoff-expired') {
    // **The one refusal that says something a person can act on.** Nobody used
    // the link; sixty seconds went by, and the launch secret still works, so
    // the answer is to reopen the printed URL.
    forgetSession(HANDOFF_EXPIRED_MESSAGE)
    return null
  }
  // **Everything else is a reload**, `handoff-spent` included.
  //
  // A cookie this desk no longer holds tells this page nothing it may act on.
  // The desk cannot distinguish a page's own earlier spend — the exchange
  // clears only `Path=/`, so a copy planted at a longer path outlives it — from
  // anybody else's, and neither can this. So a tab holding an id keeps it, and
  // a tab holding none has no session and says so. Four attempts at reading
  // more into a spent handoff were tried and withdrawn; each was a defect on
  // the same seam, and the residual is stated in the README instead.
  return stored
}

async function sentenceOf(answered: Response): Promise<string> {
  try {
    const said = ((await answered.json()) as { error?: unknown }).error
    if (typeof said === 'string' && said !== '') return said
  } catch {
    // Not a body this page can read. The fallback below is still true.
  }
  return 'This desk will not begin another session. Restart jpack-desk.'
}

/**
 * Read nothing from a response, and **let go of it**.
 *
 * A `Response` body is a stream, and a browser keeps the request in flight
 * until that stream is consumed or cancelled. So a refusal this page reads the
 * status of and nothing else leaves a request open **for ever** — which is not
 * a leak anybody would notice by using the desk, and is exactly what the
 * containment gate noticed: `page.goto(…, {waitUntil: 'networkidle'})` never
 * settled, because one `POST /api/session` that answered `401` was still there.
 *
 * Every path in this desk that decides on a status alone goes through this, and
 * the reason is written once here rather than three times at those sites.
 */
export async function discardBody(answered: Response): Promise<void> {
  try {
    await answered.body?.cancel()
  } catch {
    // Already read, already cancelled, or a runtime with no stream on a
    // response. There is nothing left to let go of either way.
  }
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
  forgotten = null
  // **The subscriptions too.** A listener that outlived its own test would be
  // told about the next test's session ending, and would tear down a component
  // that is no longer mounted. `whenSessionEnds` returns an unsubscribe and
  // every caller in the page uses it — `TestUnmountingUnsubscribes`, in this
  // file's own suite, is what holds that; this is the belt beside it.
  ending.clear()
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
  forgotten = null
}
