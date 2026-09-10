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
 * What the page says when the handoff its launch set was **spent by something
 * else**.
 *
 * The residual, happening. Reopening the printed URL cannot help: the launch
 * secret is reusable, so a script that took one handoff takes the next one too.
 * What ends it is a new process, whose secret and whose printed URL are new.
 */
export const HANDOFF_SPENT_MESSAGE =
  'The launch link was used by something else. Restart jpack-desk and open the new URL it prints.'

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
 * The one line a page shows when this desk is serving anybody but this tab.
 *
 * **Derived, never remembered.** The version this replaces stored the count it
 * last saw and said something when the number grew — which meant the *storing*
 * had to happen before the *saying*, and a reload in between silenced it for
 * ever. So nothing is stored: every bootstrap asks how many sessions this desk
 * has and subtracts its own, and a reload recomputes the same answer. There is
 * no state left for a reload to lose.
 *
 * It is a notice and not a refusal — this tab's session is fine — and it says
 * "if that is not you" because it counts **every** other session: the person's
 * own second tab, a script's, and a thief's, which this desk cannot tell apart
 * and will not pretend to.
 *
 * Under 140 characters at every plausible count, because the shell's narration
 * bound applies to it.
 */
export function otherSessionsMessage(others: number): string {
  return others === 1
    ? 'This desk has 1 other session. Restart jpack-desk if that is not you.'
    : `This desk has ${others} other sessions. Restart jpack-desk if that is not you.`
}

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
 * Where **this tab's own session sequence** is kept — keyed by the same origin,
 * for the same reason.
 *
 * **A sibling key rather than a member of the first, and that is deliberate.**
 * The id's key holds an id and exactly an id: the containment gate reads it and
 * asserts 48 characters, and the live drive does the same. Making it a record
 * would change a contract two measurements depend on, for no gain over a second
 * key that dies with the tab exactly as the first does.
 *
 * **And it is the only other thing stored.** The sequence is not a credential
 * and nothing is looked up by it; it exists for one comparison — a spent
 * handoff carries the sequence of the session it bought, and a page whose own
 * sequence matches is looking at the echo of its own link rather than at
 * somebody else's theft.
 */
export function sequenceStorageKey(): string {
  return `jpack-desk-sequence:${window.location.host}`
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
 * What this page has learned that it should say, or `null`.
 *
 * Not a refusal and not a state a reload can lose: it is **derived** by the one
 * bootstrap from what the desk reports, and a reload derives it again. See
 * `otherSessionsMessage`.
 */
let noticed: string | null = null

/** The one line the shell should show beside the desk, or `null`. */
export function sessionNotice(): string | null {
  return noticed
}

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
  // A page that has ended has nothing to say about how many other sessions
  // there are; the sentence it does have is the one below.
  noticed = null
  // **Once, whoever calls it.** Two transports can meet the same refusal in the
  // same tick, and a page that announced the end twice would tear down twice
  // and render the notice twice.
  if (forgotten !== null) return
  forgotten = reason
  try {
    window.sessionStorage.removeItem(sessionStorageKey())
    window.sessionStorage.removeItem(sequenceStorageKey())
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
  let body: { id?: unknown; sessions?: unknown }
  try {
    body = (await answered.json()) as { id?: unknown; sessions?: unknown }
  } catch {
    return stored
  }
  const id = body.id
  if (typeof id !== 'string' || id === '') return stored
  hold(id)
  const counted = countsIn(body.sessions)
  // **This tab's own sequence, and that is the whole of what is kept.** It is
  // not a credential; it exists so that a spent handoff carrying the sequence
  // it bought can be told from somebody else's.
  holdSequence(counted?.yours ?? null)
  // And the line, derived here and remembered nowhere.
  noteOtherSessions(counted)
  return id
}

/** The pair every answer about a session carries, or `null`. */
interface Counts {
  minted: number
  yours: number
}

/**
 * `{minted, yours}` out of an answer that carries them.
 *
 * `null` where the answer says nothing — an older chassis, or a proxy — which
 * reads as "nothing to say" rather than as zero.
 */
function countsIn(sessions: unknown): Counts | null {
  if (sessions === null || typeof sessions !== 'object') return null
  const { minted, yours } = sessions as { minted?: unknown; yours?: unknown }
  if (typeof minted !== 'number' || !Number.isFinite(minted)) return null
  if (typeof yours !== 'number' || !Number.isFinite(yours)) return null
  return { minted, yours }
}

/**
 * How many sessions this desk is serving that are not this tab's, said once.
 *
 * `minted - 1`, and the subtraction is the point: a count of *others* is a fact
 * about the desk right now, computed from what the desk just said, so a reload
 * computes it again. The version this replaces compared against a stored
 * number, which meant a reload between the store and the paint silenced it for
 * ever.
 */
function noteOtherSessions(counted: Counts | null): void {
  if (counted === null) return
  const others = counted.minted - 1
  noticed = others > 0 ? otherSessionsMessage(others) : null
}

/**
 * Ask the desk about its sessions, for a tab that kept the id it already had.
 *
 * **One request, on the one path where the numbers are not already in hand.** A
 * tab that just minted its own session got them with the id. This is for the
 * ordinary case — a reload, `no-handoff`, an id kept — and it is the path where
 * a theft that happened hours ago is otherwise invisible.
 *
 * It runs inside `bootstrap()` rather than beside it, so that everything this
 * page stores has the same single writer.
 */
async function askAboutSessions(id: string): Promise<void> {
  let answered: Response
  try {
    answered = await fetch('/api/session', {
      credentials: 'omit',
      headers: { Authorization: `Bearer ${id}` }
    })
  } catch {
    return
  }
  if (!answered.ok) {
    // A refused read is not this function's business: the id is dead, and the
    // call that meets it next says so. The body is let go of either way.
    await discardBody(answered)
    return
  }
  let counted: Counts | null = null
  try {
    counted = countsIn(((await answered.json()) as { sessions?: unknown }).sessions)
  } catch {
    return
  }
  // The sequence is refreshed too: a tab whose storage was cleared, or which
  // met an older chassis, learns its own place here.
  holdSequence(counted?.yours ?? null)
  noteOtherSessions(counted)
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
 * One code for the first and the last of those was a review's HIGH: the page
 * kept its session either way, so a theft was invisible to the person it
 * happened to.
 */
async function refusedExchange(answered: Response, stored: string | null): Promise<string | null> {
  const code = refusalCode(answered)
  if (answered.status === 503 && code === 'sessions-full') {
    forgetSession(await sentenceOf(answered))
    return null
  }
  if (code === 'handoff-spent') {
    // **Whose spend was it?** The clear reaches `Path=/` and nothing else, so a
    // copy of this tab's own genuine handoff planted at a longer path survives
    // its own exchange and is presented again on the next load. The refusal
    // carries the sequence of the session that handoff bought: this tab's own
    // means the echo of its own link, which is exactly a reload; anything else
    // is somebody who used the link, and the page stops.
    //
    // A produced sequence of zero is evidence of nobody — the exchange spent
    // the handoff and then minted nothing — and reads as a reload too.
    const produced = await producedSequenceOf(answered)
    const mine = storedSequence()
    const echo =
      produced === 0 || (produced !== null && mine !== null && produced === mine)
    if (echo) {
      if (stored !== null) await askAboutSessions(stored)
      return stored
    }
    forgetSession(HANDOFF_SPENT_MESSAGE)
    return null
  }
  await discardBody(answered)
  if (code === 'handoff-expired') {
    forgetSession(HANDOFF_EXPIRED_MESSAGE)
    return null
  }
  // `no-handoff`, or a refusal this page does not recognise: a reload. The id
  // is kept — and **this is the path where a theft is otherwise invisible**, so
  // it is the path that asks the desk how many sessions it is serving.
  if (stored !== null) await askAboutSessions(stored)
  return stored
}

/**
 * The sequence a spent handoff produced, out of the chassis' own refusal.
 *
 * Read whole and unbounded, and that is safe where it was not on the relay: the
 * mark has already said this body is the chassis' own, and it is one small JSON
 * object.
 */
async function producedSequenceOf(answered: Response): Promise<number | null> {
  try {
    const produced = ((await answered.json()) as { producedSeq?: unknown }).producedSeq
    return typeof produced === 'number' && Number.isFinite(produced) ? produced : null
  } catch {
    return null
  }
}

/**
 * The sentence out of a refusal **this chassis wrote**.
 *
 * Read whole, and that is safe here where it was not on the relay: the header
 * has already said this body is the chassis' own, and the chassis' refusal is
 * one small JSON object. Falls back to a sentence of this page's own rather
 * than to nothing, because a person meeting a capacity refusal needs to be told
 * something.
 */
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

/** This tab's own session sequence, or `null` where it has none. */
function storedSequence(): number | null {
  try {
    const held = window.sessionStorage.getItem(sequenceStorageKey())
    if (held === null) return null
    const seq = Number(held)
    return Number.isFinite(seq) && seq > 0 ? seq : null
  } catch {
    return null
  }
}

function holdSequence(seq: number | null): void {
  if (seq === null) return
  try {
    window.sessionStorage.setItem(sequenceStorageKey(), String(seq))
  } catch {
    // A browser that refuses storage cannot tell its own echo from a theft, so
    // it reads a spent handoff as a theft — the cautious answer, and the one a
    // tab that has never held a session gets too.
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
  noticed = null
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
