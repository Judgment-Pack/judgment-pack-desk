/**
 * The desk's conformance session: the bake-off's scenario, in the repository,
 * in CI.
 *
 * ADR-0001 makes certification the price of shipping an engine — "an engine
 * ships only if it passes the desk's conformance session in CI, which is the
 * bake-off scenario carried into the repository" — so this is not a test of
 * the built-in engine. It is a test of **every engine in the registry**, run
 * over the same scenario, and an adapter is certified by adding its id to
 * `CERTIFIED_ENGINES`.
 *
 * Keyless, deterministic, offline: a scripted model as a `fetch` stub, a
 * recorded runtime on an in-memory transport pair, no binary and no network.
 * The whole path is exercised — the ToolGate on a real transport, a real SDK
 * `Client`, the engine's own wire code — so what is measured is what the page
 * does and not a rehearsal of it.
 *
 * Four legs: OpenAI-compatible and Anthropic, each answered as a stream and as
 * one whole object. The second of each pair is not decoration: an endpoint may
 * ignore `stream`, and an engine that parsed by what it *asked for* would read
 * a JSON object as an event stream.
 *
 * The checks are the experiment's own, by their own names:
 *
 * - **K1** no credential in any request, and only the relay's base is called.
 * - **K2** the tools offered to the model are the five, taken from
 *   `tools/list`, with the runtime's own schemas — and no engine source
 *   carries a schema literal (asserted in `assistant/enforcement.test.ts`).
 * - **K3a** `experimental_evaluate` is rewritten to carry `rehearsal: true`,
 *   measured at the scripted server rather than at the page.
 * - **K3b** `write_file` never reaches the runtime.
 * - **K3c** the proposal's document is DRAFT_V2, byte for byte.
 * - **T8** the unknowns arrive as the scenario wrote them.
 * - the event stream's shape and order, and `end` exactly once.
 */
import { afterEach, describe, expect, it, vi } from 'vitest'
import { CERTIFIED_ENGINES, loadEngine } from '../engines'
import { bindModelCall, openAssistantConnection, runAssistantSession } from '../session'
import scenario from './scenario.json'
import { scriptedModel, type RecordedRequest } from './scriptedModel'
import { RECORDED_TOOLS, scriptedRuntime, type ServerObservation } from './scriptedServer'
import { normalize } from '../thinking'
import runtime from './runtime.json'
import type { AssistantEvent, Engine } from '../engine'

const FIVE = scenario.scenarioTools

/** The stand-in for the runtime's `test_pack` prompt. See `runLeg`. */
const TEST_PROMPT = 'THE RUNTIME’S TEST_PACK GUIDANCE, AS PROMPTS/GET SERVED IT'
const DRAFT_V2 = scenario.documents.DRAFT_V2 as unknown

/**
 * The network globals an engine must never touch, and what happens if it does.
 *
 * ADR-0001's contract sketch handed the engine a `baseUrl`, and a `baseUrl` for
 * this relay carries **this chassis' session token**: an adapter could read it
 * and open `/ws?token=…` itself with `globalThis.WebSocket`, driving a third
 * MCP connection the ToolGate is not on. The contract changed to a capability
 * (`assistant/engine.ts`), and this is what holds the change — structurally,
 * rather than by scanning the source for spellings somebody thought of.
 *
 * For the duration of the engine's run every one of these is a sentinel that
 * throws. The desk's own capability captured `fetch` when the session was
 * bound, so it still works; an engine that reaches for a global does not.
 */
const NETWORK_GLOBALS = ['fetch', 'WebSocket', 'XMLHttpRequest', 'EventSource'] as const

class EngineTouchedANetworkGlobal extends Error {
  // Assigned, because a subclass does not get one: every assertion below is by
  // this name, and round 3 pointed out it was `Error` on all of them.
  constructor(message: string) {
    super(message)
    this.name = 'EngineTouchedANetworkGlobal'
  }
}

/** One sealed interval: the sentinels, and what they caught. */
interface Seal {
  /** Every reach, in order. Deferred ones land here too, where nothing catches. */
  violations: string[]
  lift(): void
}

/**
 * Replace every network global with a throwing sentinel.
 *
 * **It records as well as throwing.** A reach from inside a `setTimeout` throws
 * into nobody's `catch`, so the exception alone proves nothing: the leg would
 * pass with the violation swallowed. The list is what the assertions read.
 */
function sealNetwork(): Seal {
  const scope = globalThis as unknown as Record<string, unknown>
  const before = new Map<string, unknown>()
  const violations: string[] = []
  for (const name of NETWORK_GLOBALS) {
    before.set(name, scope[name])
    const sentinel = function sealed(...args: unknown[]): never {
      // The address it asked for is recorded beside the reach, so a leg can say
      // **which** of an engine's schedules got through rather than only that
      // one did. A barrier that catches three of four catches none of the one
      // that matters.
      const asked = args.length > 0 ? ` for ${String(args[0])}` : ''
      const reach =
        `the engine reached for globalThis.${name}${asked}; a session's only reach to a model ` +
        `is session.model.call, and its only reach to the runtime is session.callTool`
      violations.push(reach)
      throw new EngineTouchedANetworkGlobal(reach)
    }
    // Both call shapes: `fetch(...)` and `new WebSocket(...)`.
    scope[name] = sentinel
  }
  return {
    violations,
    lift() {
      for (const [name, value] of before) scope[name] = value
    }
  }
}

/**
 * One deferred callback an engine scheduled while the seal was up.
 *
 * `fire` runs it now; `cancel` marks it as the engine's own cancellation, which
 * is not the same thing at all — a timer an engine cleared is a timer that will
 * never run, and firing it on the engine's behalf would fail a leg for
 * something the engine did not do.
 */
interface Tracked {
  kind: string
  label: string
  /**
   * When this schedule is due, on the drain's own clock.
   *
   * The drain fires what an engine left behind **in the order a browser would
   * have run it**, advancing its clock to each entry's due time. Firing
   * everything at once instead let a sixty-second idle callback run before the
   * one-second timer that was going to cancel it, and report a timeout that
   * had not happened.
   */
  dueAt: number
  /** True until it has run, or the engine cancelled it. */
  pending: boolean
  /** True where the engine itself called clearTimeout/clearInterval. */
  cancelled: boolean
  /** An interval: pending for ever until the engine clears it. */
  repeating: boolean
  fire(): void
  cancel(): void
  /** Stop the real handle. The harness's own hygiene, never a verdict. */
  stop(): void
}

/**
 * Track every handle an engine creates, instead of waiting for one to fire.
 *
 * The barrier this replaces was a fixed wait, and a fixed wait is a delay an
 * engine can out-wait. So the timer functions are wrapped for the sealed
 * window: each call is recorded **and** scheduled for real, so an engine that
 * legitimately depends on a timer still makes progress, and the drain
 * afterwards runs whatever has not run yet.
 *
 * **`clearTimeout` and `clearInterval` are wrapped too.** Without them a handle
 * the engine itself cancelled stayed "pending" in the bookkeeping and was force
 * run by the drain — a certification failure for something the engine had
 * already decided not to do.
 *
 * `queueMicrotask` is wrapped and forwarded rather than deferred: promise
 * machinery runs on it, and holding one back would deadlock the very run this
 * is measuring. `Promise.prototype.then` is **not** wrapped — see
 * `drainDeferredWork` for what that bounds.
 */
function trackDeferredWork(): {
  pending(): Tracked[]
  due(): Tracked[]
  advanceTo(moment: number): void
  liveIntervals(): Tracked[]
  restore(): void
} {
  const scope = globalThis as unknown as Record<string, unknown>
  const before = new Map<string, unknown>()
  /** Names this harness *added*, which are deleted again rather than restored. */
  const added = new Set<string>()
  const tracked: Tracked[] = []
  const byHandle = new Map<unknown, Tracked>()
  const keep = (name: string) => before.set(name, scope[name])

  const realSetTimeout = globalThis.setTimeout
  const realClearTimeout = globalThis.clearTimeout
  const realSetInterval = globalThis.setInterval
  const realClearInterval = globalThis.clearInterval
  const realQueueMicrotask = globalThis.queueMicrotask

  /**
   * The drain's clock: real time, or the point the drain has advanced to.
   *
   * Never goes backwards, and is what a callback reads to know whether its own
   * deadline has been reached.
   */
  let advancedTo = 0
  const now = () => Math.max(Date.now(), advancedTo)

  function record(
    kind: string,
    label: string,
    run: () => void,
    schedule: (wrapped: () => void) => unknown,
    clear: (handle: unknown) => void,
    repeating: boolean,
    delay = 0
  ): unknown {
    const entry: Tracked = {
      kind,
      label,
      dueAt: now() + Math.max(0, delay),
      pending: true,
      cancelled: false,
      repeating,
      fire() {
        if (!entry.pending || entry.cancelled) return
        entry.pending = false
        clear(handle)
        run()
      },
      cancel() {
        entry.cancelled = true
        entry.pending = false
      },
      stop() {
        entry.pending = false
        clear(handle)
      }
    }
    const handle = schedule(() => {
      if (entry.cancelled) return
      if (!repeating) entry.pending = false
      run()
    })
    tracked.push(entry)
    // **A handle of `undefined` is not a handle.** `queueMicrotask` returns
    // nothing, so every microtask an engine queued used to be filed under the
    // one key `undefined` — and `clearTimeout(undefined)`, the defensive line
    // every library carries and the AI SDK reaches seven times a leg, cancelled
    // the most recent of them. The drain then refused to run it "because the
    // engine cancelled it", and a reach on that microtask was never recorded.
    // A schedule with no handle can only be cancelled by the engine holding a
    // handle it was never given, so it is filed under none.
    if (handle !== undefined && handle !== null) byHandle.set(handle, entry)
    return handle
  }

  /** The engine's own cancellation: the handle is done, and never fired here. */
  const forget = (handle: unknown, clear: (h: unknown) => void) => {
    byHandle.get(handle)?.cancel()
    clear(handle)
  }

  keep('setTimeout')
  scope.setTimeout = (fn: unknown, ms?: number, ...args: unknown[]) => {
    if (typeof fn !== 'function') return realSetTimeout(fn as never, ms)
    return record(
      'setTimeout',
      `setTimeout(${String(ms ?? 0)}ms)`,
      () => (fn as (...rest: unknown[]) => void)(...args),
      (wrapped) => realSetTimeout(wrapped, ms),
      (handle) => realClearTimeout(handle as never),
      false,
      ms ?? 0
    )
  }
  keep('clearTimeout')
  scope.clearTimeout = (handle: unknown) =>
    forget(handle, (inner) => realClearTimeout(inner as never))

  keep('setInterval')
  scope.setInterval = (fn: unknown, ms?: number, ...args: unknown[]) => {
    if (typeof fn !== 'function') return realSetInterval(fn as never, ms)
    return record(
      'setInterval',
      `setInterval(${String(ms ?? 0)}ms)`,
      () => (fn as (...rest: unknown[]) => void)(...args),
      (wrapped) => realSetInterval(wrapped, ms),
      (handle) => realClearInterval(handle as never),
      true,
      ms ?? 0
    )
  }
  keep('clearInterval')
  scope.clearInterval = (handle: unknown) =>
    forget(handle, (inner) => realClearInterval(inner as never))

  keep('queueMicrotask')
  scope.queueMicrotask = (fn: () => void) =>
    record(
      'queueMicrotask',
      'queueMicrotask',
      fn,
      (wrapped) => {
        realQueueMicrotask(wrapped)
        return undefined
      },
      () => {},
      false
    )
  if (typeof scope.setImmediate === 'function') {
    const realSetImmediate = scope.setImmediate as (fn: () => void) => unknown
    const realClearImmediate = scope.clearImmediate as ((handle: unknown) => void) | undefined
    keep('setImmediate')
    scope.setImmediate = (fn: () => void, ...args: unknown[]) =>
      record(
        'setImmediate',
        'setImmediate',
        () => fn(...(args as [])),
        (wrapped) => realSetImmediate(wrapped),
        (handle) => realClearImmediate?.(handle),
        false
      )
    // **The canceller too.** `setImmediate` was wrapped and `clearImmediate`
    // was not, so a handle the engine itself cancelled stayed pending in the
    // bookkeeping and was force-run by the drain — a reach attributed to an
    // engine that had already decided not to make it.
    if (typeof realClearImmediate === 'function') {
      keep('clearImmediate')
      scope.clearImmediate = (handle: unknown) => forget(handle, (inner) => realClearImmediate(inner))
    }
  }
  if (typeof scope.requestAnimationFrame === 'function') {
    const realRaf = scope.requestAnimationFrame as (fn: (t: number) => void) => unknown
    const realCancelRaf = scope.cancelAnimationFrame as ((handle: unknown) => void) | undefined
    keep('requestAnimationFrame')
    scope.requestAnimationFrame = (fn: (t: number) => void) =>
      record(
        'requestAnimationFrame',
        'requestAnimationFrame',
        () => fn(0),
        (wrapped) => realRaf(() => wrapped()),
        (handle) => realCancelRaf?.(handle),
        false
      )
    if (typeof realCancelRaf === 'function') {
      keep('cancelAnimationFrame')
      scope.cancelAnimationFrame = (handle: unknown) => forget(handle, (inner) => realCancelRaf(inner))
    }
  }
  /**
   * **The idle callback the seal has to bring with it.**
   *
   * jsdom has no `requestIdleCallback`, so an engine that wrote
   * `globalThis.requestIdleCallback?.(() => fetch(…))` did nothing at all during
   * certification and reached the network in Chrome, after the seal would have
   * lifted. A primitive the *browser* has and the *harness* does not is a hole
   * in a guard whose whole claim is "everything this engine scheduled has run".
   *
   * So the harness installs a realistic one where the environment has none —
   * backed by a timeout, handing the callback the deadline object the API
   * defines — tracked and cancellable like every other schedule, and **removed**
   * again by `restore` rather than left behind for the next test file.
   *
   * **Realistic means the deadline, not only the callback.** A shim that always
   * answered `timeRemaining() === 0` certified nothing about the ordinary idle
   * pattern — `requestIdleCallback(d => { if (d.timeRemaining() > 0) work() })`
   * did nothing here and did its work in a browser, after the seal lifted — and
   * a shim that always answered `didTimeout: false` certified nothing about code
   * that waits for its own timeout. So the deadline carries a positive,
   * decreasing budget measured from when the callback *starts* (the browser's
   * own rule), and `didTimeout` says what actually ran it.
   */
  {
    const realIdle = scope.requestIdleCallback as
      | ((fn: unknown, options?: IdleRequestOptions) => unknown)
      | undefined
    const realCancelIdle = scope.cancelIdleCallback as ((handle: unknown) => void) | undefined
    if (realIdle === undefined) added.add('requestIdleCallback')
    if (realCancelIdle === undefined) added.add('cancelIdleCallback')
    keep('requestIdleCallback')
    keep('cancelIdleCallback')
    scope.requestIdleCallback = (
      fn: (deadline: IdleDeadline) => void,
      options?: IdleRequestOptions
    ) => {
      const timeout = options?.timeout
      // **A sealed leg never goes idle**, so a callback that asked for a
      // timeout is run because of it — and it is run **at that timeout and not
      // before**. Firing every positive timeout after a millisecond and calling
      // it a timeout was the same fiction one step further on: an engine that
      // would have cancelled a sixty-second callback long before its deadline
      // was credited with work a browser would never have let it do.
      const label =
        timeout === undefined
          ? 'requestIdleCallback'
          : `requestIdleCallback(${String(timeout)}ms timeout)`
      // **Computed when the callback runs, not when it was asked for.** A
      // deadline that has not been reached is not a timeout, and saying it was
      // is how an engine got credited with work a browser would never have let
      // it do.
      const deadlineAt = timeout === undefined ? undefined : now() + timeout
      const run = () => {
        const didTimeout = deadlineAt !== undefined && now() >= deadlineAt
        // The budget a browser gives a callback, measured from the moment it
        // starts running rather than from when it was asked for: positive at
        // the first read and decreasing, and never negative.
        const startedAt = Date.now()
        fn({
          didTimeout,
          timeRemaining: () => Math.max(0, IDLE_BUDGET_MS - (Date.now() - startedAt))
        })
      }
      const schedule = (wrapped: () => void): unknown =>
        realIdle !== undefined
          ? realIdle(wrapped, options)
          : // The requested deadline, honoured. An idle slot with no timeout is
            // modelled as the next turn, which is the soonest a browser could
            // have offered one.
            realSetTimeout(wrapped, timeout ?? 1)
      const clear = (handle: unknown) =>
        realCancelIdle !== undefined ? realCancelIdle(handle) : realClearTimeout(handle as never)
      // Due at its own deadline — an idle slot on the next turn where none was
      // asked for — so the drain runs it where a browser would have.
      return record('requestIdleCallback', label, run, schedule, clear, false, timeout ?? 1)
    }
    scope.cancelIdleCallback = (handle: unknown) =>
      forget(handle, (inner) =>
        realCancelIdle !== undefined ? realCancelIdle(inner) : realClearTimeout(inner as never)
      )
  }

  return {
    /** The pending, non-repeating entries, soonest first. */
    due: () =>
      tracked
        .filter((entry) => entry.pending && !entry.repeating && !entry.cancelled)
        .sort((left, right) => left.dueAt - right.dueAt),
    /** Advance the drain's clock to a point, never backwards. */
    advanceTo: (moment: number) => {
      advancedTo = Math.max(advancedTo, moment)
    },
    // **An interval is never work the drain does.** Running a few ticks and
    // calling it drained let an engine hide a reach behind a later one, so an
    // interval is not something this fires on the engine's behalf at all — it
    // is reported, below, and the engine fails for having left it. One rule,
    // one place: a second guard inside `fire` would make this one unobservable.
    pending: () => tracked.filter((entry) => entry.pending && !entry.repeating),
    // **The interval rule.** A certified engine leaves no live interval when its
    // iterator ends: an interval nobody cleared runs for ever, and an engine
    // that reaches on its ninth tick is an engine no bounded drain can catch.
    liveIntervals: () => tracked.filter((entry) => entry.repeating && !entry.cancelled),
    restore() {
      for (const [name, value] of before) {
        // A primitive this harness *added* is deleted rather than restored to
        // `undefined`: a page that feature-detects it would otherwise find the
        // property present and useless for every test that runs after.
        if (added.has(name)) delete scope[name]
        else scope[name] = value
      }
      // Whatever the engine left behind stops here, so a leg cannot leak a
      // ticking timer into the next one. This is hygiene and never a verdict:
      // `liveIntervals` was read before it, and an interval stopped here has
      // already been reported.
      for (const entry of tracked) entry.stop()
    }
  }
}

/** How many times the drain will look for more work before giving up. */
const DRAIN_ROUNDS = 64

/**
 * The idle budget the harness's own `requestIdleCallback` hands a callback.
 *
 * A browser gives an idle callback whatever is left of the frame, up to 50ms.
 * The number matters less than that it is **positive and decreasing**: an
 * engine that guards its work on `deadline.timeRemaining() > 0` must actually
 * do that work here, or certification says nothing about the ordinary idle
 * pattern.
 */
const IDLE_BUDGET_MS = 50

/**
 * Run everything an engine left behind, under the seal, until nothing is left.
 *
 * Repeatedly, because a timer may schedule another one and a microtask may
 * queue another — the chained shapes a fixed wait could not see. Bounded,
 * because an engine that schedules for ever must end the drain rather than the
 * drain ending the suite; **what is left over when the bound is reached is a
 * failure, not a pass**, and the count is what the leg asserts.
 *
 * Promise reactions are flushed rather than tracked: `await Promise.resolve()`
 * between rounds runs whatever the microtask queue holds, which is how a
 * `.then` chain an engine left behind is caught while the seal is still up.
 * What that does not cover is a reaction chained off something that resolves
 * *after* the drain — a fetch to a real host, a socket, a `MessageChannel` —
 * and that bound is stated in the README rather than papered over.
 *
 * **Two more bounds, found when an engine built on a framework first ran these
 * legs**, and stated here for the same reason:
 *
 * - **An open stream is not pending work.** An engine that stopped reading a
 *   `ReadableStream` half way leaves a reader attached and nothing scheduled:
 *   no handle, no microtask, nothing this drain can see. What the seal still
 *   holds is that whatever that stream eventually does cannot reach a network
 *   global — but it holds it only while the seal is up, and the seal comes down
 *   when the drain finishes.
 * - **An unhandled rejection is invisible here.** The one defect ADR-0001
 *   records against the default engine is a rejection the caller cannot claim
 *   reaching the *page*. These legs run in jsdom under Node, where such a
 *   rejection goes to Node's own handler and never becomes a `window` event, so
 *   neither the seal nor the drain can observe it. The guard the ADR asks for is
 *   exercised in `engines/vercel/engine.test.ts` instead, by dispatching the
 *   event a browser would.
 */
async function drainDeferredWork(tracker: {
  pending(): Tracked[]
  due(): Tracked[]
  advanceTo(moment: number): void
}): Promise<number> {
  for (let round = 0; round < DRAIN_ROUNDS; round += 1) {
    // Microtasks first: a `.then` chain needs no timer at all.
    for (let tick = 0; tick < 8; tick += 1) await Promise.resolve()
    let next = tracker.due()[0]
    if (next === undefined) {
      // One more flush, in case the last timer queued a reaction.
      for (let tick = 0; tick < 8; tick += 1) await Promise.resolve()
      next = tracker.due()[0]
      if (next === undefined) break
    }
    // **One at a time, soonest first, with the clock advanced to its due
    // time.** Firing every pending entry at once ran them in the order they
    // were *scheduled* rather than the order they were *due*, so a
    // sixty-second idle callback ran before the one-second timer that was
    // going to cancel it — and, being run, reported a timeout that had not
    // happened. Re-read each round, because firing one schedules others.
    tracker.advanceTo(next.dueAt)
    next.fire()
  }
  return tracker.pending().length
}

interface Leg {
  api: 'openai-compatible' | 'anthropic'
  answerAs: 'stream' | 'whole'
}

const LEGS: Leg[] = [
  { api: 'openai-compatible', answerAs: 'stream' },
  { api: 'openai-compatible', answerAs: 'whole' },
  { api: 'anthropic', answerAs: 'stream' },
  { api: 'anthropic', answerAs: 'whole' }
]

interface Run {
  events: AssistantEvent[]
  requests: RecordedRequest[]
  seen: ServerObservation[]
  /** Every network global the engine reached for, deferred reaches included. */
  violations: string[]
  /** Handles still pending when the drain gave up. Zero, or the seal is a wait. */
  leftPending: number
  /** Intervals the engine never cleared. Each one is a certification failure. */
  liveIntervals: string[]
  /** What a deferred callback threw during the drain, where one did. */
  drainThrew: string
}

/**
 * One whole session, driven exactly as the page drives it.
 *
 * The desk's half — the socket, the gate, the client, the model capability — is
 * bound first, with the real globals in place. Then the network is sealed, and
 * **everything the engine touches happens inside that seal**: its chunk is
 * imported under it, it runs under it, and deferred work is drained under it
 * before the globals come back. The two halves used to be the other way round
 * in both directions — the `import()` happened before the seal and the seal was
 * lifted the instant the run resolved — so a module could reach for `fetch` on
 * load and a timer could reach for it a moment after `end`.
 */
async function runLeg(
  load: () => Promise<Engine>,
  leg: Leg
): Promise<Run> {
  const model = scriptedModel({ api: leg.api, answerAs: leg.answerAs })
  vi.stubGlobal('fetch', model.fetch)
  const runtime = await scriptedRuntime()
  const events: AssistantEvent[] = []
  const connection = openAssistantConnection({
    allowed: FIVE,
    onEvent: (event) => events.push(event),
    transport: runtime.transport
  })
  // **The desk's half is bound outside the seal, and must be**: the model
  // capability captures `fetch` when it is bound, which is the property that
  // lets an engine be sealed off from every network global while its own calls
  // still work. Binding it under the sentinel would capture the sentinel.
  let seal: Seal | null = null
  let tracker: ReturnType<typeof trackDeferredWork> | null = null
  let leftPending = 0
  let liveIntervals: string[] = []
  let drainThrew = ''
  try {
    const ready = await connection.ready
    const call = bindModelCall()
    // Sealed from here: the engine's chunk is imported under it, and every
    // handle it creates from here is tracked rather than waited on.
    seal = sealNetwork()
    tracker = trackDeferredWork()
    const engine = await load()
    await runAssistantSession(
      engine,
      {
        // The prompt the desk fetched over prompts/get. Its text is the
        // runtime's; what matters here is that the engine sends it and adds
        // no authoring instructions of its own.
        prompt: `${scenario.policy}`,
        // The runtime's own `test_pack` guidance is what the critic works from
        // on the page. This session carries no recorded `prompts/get`, so it
        // hands over a stand-in and the legs assert the stand-in travelled —
        // which measures the engine and models the runtime's text.
        testPrompt: TEST_PROMPT,
        tools: ready.tools,
        callTool: ready.callTool,
        model: { family: leg.api, model: 'scripted-model', call },
        thinking: normalize('off', leg.api),
        signal: new AbortController().signal
      },
      (event) => events.push(event)
    )
  } catch (cause) {
    // A load that threw or a run that failed is the leg's result, not the
    // harness's problem: it lands on the stream like any other failure.
    events.push({ type: 'error', message: `${(cause as Error).name}: ${(cause as Error).message}` })
  } finally {
    // **Nested, so every one of these runs whatever throws.** A deferred
    // callback that threw during the drain used to leave this block before the
    // sentinels and the timer wrappers came off, and a leg that failed could
    // poison every leg after it with globals that were never restored.
    try {
      if (tracker !== null) {
        leftPending = await drainDeferredWork(tracker)
        liveIntervals = tracker.liveIntervals().map((entry) => entry.label)
      }
    } catch (cause) {
      drainThrew = `${(cause as Error).name}: ${(cause as Error).message}`
    } finally {
      try {
        tracker?.restore()
      } finally {
        try {
          seal?.lift()
        } finally {
          try {
            await connection.close()
          } finally {
            await runtime.close()
          }
        }
      }
    }
  }
  return {
    events,
    requests: model.requests,
    seen: runtime.seen,
    violations: seal?.violations ?? [],
    leftPending,
    liveIntervals,
    drainThrew
  }
}

/** The registry's own loader, for the engines this build certifies. */
const fromRegistry = (id: (typeof CERTIFIED_ENGINES)[number]) => () => loadEngine(id)

/**
 * What the engine sent, request by request, for a reviewer to read.
 *
 * The conformance session's assertions say what must be true of every engine;
 * this says what **this** one actually put on the wire — the step it was
 * answering, how many results its own messages carried back, and the body's own
 * top-level members. It is attached to the leg as a test annotation rather than
 * asserted, because its value is that a reviewer can read it and notice
 * something nobody wrote a rule about yet.
 */
function requestLog(requests: RecordedRequest[]): string {
  return requests
    .map(
      (request) =>
        `${request.step} results=${request.results} ${new URL(request.url, 'http://desk.invalid').pathname} ` +
        `{${request.bodyMembers.join(', ')}} tools=[${request.toolNames.join(', ')}] ` +
        `headers=[${request.headerNames.join(', ')}]`
    )
    .join('\n')
}

/**
 * The members each wire format defines, which every engine must send.
 *
 * **Not the union of what the two engines send.** The built-in engine puts
 * `stream_options` on an OpenAI-compatible request and the SDK-backed one puts
 * a `tool_choice`; neither is required by the protocol and neither is the
 * other's business. What is asserted is what the format itself defines, so a
 * third adapter is held to the same list.
 */
const WIRE_MEMBERS: Record<Leg['api'], string[]> = {
  'openai-compatible': ['messages', 'model', 'stream', 'tools'],
  anthropic: ['max_tokens', 'messages', 'model', 'stream', 'system', 'tools']
}

const toolCalls = (events: AssistantEvent[]) =>
  events.filter((event): event is Extract<AssistantEvent, { type: 'tool_call' }> => event.type === 'tool_call')
const guardrails = (events: AssistantEvent[]) =>
  events.filter(
    (event): event is Extract<AssistantEvent, { type: 'guardrail' }> => event.type === 'guardrail'
  )
const proposals = (events: AssistantEvent[]) =>
  events.filter(
    (event): event is Extract<AssistantEvent, { type: 'proposal' }> => event.type === 'proposal'
  )

afterEach(() => vi.unstubAllGlobals())

describe('the scenario this session carries', () => {
  it('is the bake-off’s, with its provenance and its eight steps', () => {
    expect(scenario.$comment).toContain('2026-09-05-assistant-engine-bakeoff')
    expect(scenario.generatedAt).toContain('2026-09-05')
    // The generator lives in the experiment's record, not here. An earlier
    // version of that line named a path in this repository that does not
    // exist, which is a provenance claim nobody could check.
    expect(scenario.$comment).toContain('NOT in this repository')
    expect(scenario.steps.map((step) => step.id)).toEqual([
      'T1',
      'T2',
      'T3',
      'T4',
      'T5',
      'T6',
      'T7',
      'T8'
    ])
    expect(scenario.steps.find((step) => step.id === 'T7')!.tool).toBe('write_file')
    // T6 is written with no rehearsal member. That is the whole point of it:
    // the gate has to put one there.
    const evaluate = scenario.steps.find((step) => step.id === 'T6')!
    expect(Object.keys(evaluate.arguments!).sort()).toEqual(['facts', 'pack'])
  })

  it('names the binary it was recorded from, and how to re-record it', () => {
    // **Provenance that can be checked, not asserted.** The fixture used to
    // carry a tag and a short commit in prose, with no recorder in the
    // repository and nothing that could tell a hand-edited answer from a
    // recorded one. `web/scripts/record-conformance.mjs` regenerates this file
    // byte for byte from a named binary and project, and its `verify` mode
    // byte-compares — so what this test holds is that the members the
    // verification needs are all here.
    expect(runtime.recorder).toBe('web/scripts/record-conformance.mjs')
    expect(runtime.runtime.binarySha256).toMatch(/^[0-9a-f]{64}$/)
    expect(runtime.runtime.sourceCommit).toMatch(/^[0-9a-f]{40}$/)
    expect(runtime.runtime.protocolVersion).toBeTruthy()
    expect(runtime.runtime.serverInfo.name).toBeTruthy()
    // The one member here that is a claim rather than a measurement says so.
    expect(runtime.runtime.note).toContain('a claim rather than a measurement')
    expect(runtime.$comment).toContain('conformance:verify')
  })

  it('carries one recorded answer per step T1–T6 and none for the write', () => {
    expect(runtime.calls.map((call) => call.step)).toEqual(['T1', 'T2', 'T3', 'T4', 'T5', 'T6'])
    expect(runtime.calls.map((call) => call.tool)).not.toContain('write_file')
    // Recorded with the arguments the gate lets out of the page.
    const evaluate = runtime.calls.find((call) => call.tool === 'experimental_evaluate')!
    expect((evaluate.arguments as { rehearsal?: unknown }).rehearsal).toBe(true)
  })

  it('offers the five tools the runtime actually served, schemas included', () => {
    expect(RECORDED_TOOLS.map((tool) => tool.name)).toEqual(FIVE)
    for (const tool of RECORDED_TOOLS) {
      expect(tool.inputSchema, `${tool.name} was recorded without its schema`).toBeDefined()
      expect(tool.description, `${tool.name} was recorded without its description`).toBeTruthy()
    }
  })
})

describe.each(CERTIFIED_ENGINES)('engine %s', (engineId) => {
  describe.each(LEGS)('leg $api answered as $answerAs', (leg) => {
    it('runs the whole scenario and ends with a proposal', async () => {
      const { events, seen } = await runLeg(fromRegistry(engineId), leg)

      // Every recorded step ran, in the scenario's order, and the two the
      // scenario exists to provoke did not reach the runtime at all.
      expect(toolCalls(events).map((event) => event.name)).toEqual([
        'get_schema',
        'list_examples',
        'get_example',
        'validate',
        'validate',
        'experimental_evaluate',
        'write_file'
      ])
      expect(seen.filter((call) => call.refusal !== '')).toEqual([])
      expect(seen.map((call) => call.name)).toEqual([
        'get_schema',
        'list_examples',
        'get_example',
        'validate',
        'validate',
        'experimental_evaluate'
      ])

      // The runtime's own answers reached the loop: T4 invalid with the
      // diagnostic the fixture names, T5 valid, T6 a rehearsal.
      const results = events.filter(
        (
          event
        ): event is Extract<AssistantEvent, { type: 'tool_result' }> => event.type === 'tool_result'
      )
      const validates = results.filter((result) => result.name === 'validate')
      expect(JSON.parse(validates[0]!.text).status).toBe(scenario.expectations.T4.status)
      expect(JSON.parse(validates[0]!.text).diagnostics[0].code).toBe(
        scenario.expectations.T4.diagnosticCode
      )
      expect(JSON.parse(validates[1]!.text).status).toBe(scenario.expectations.T5.status)
      const evaluated = JSON.parse(
        results.find((result) => result.name === 'experimental_evaluate')!.text
      )
      expect(evaluated.rehearsal).toBe(scenario.expectations.T6.rehearsal)
      expect(evaluated.disposition.kind).toBe(scenario.expectations.T6.disposition.kind)
      expect(evaluated.disposition.outcomeId).toBe(scenario.expectations.T6.disposition.outcomeId)
    })

    it('K1a — the engine touches no network global, at load, during or after', async () => {
      // The sentinels are up from before the engine's chunk is imported until
      // after deferred work has been drained, and they **record** as well as
      // throwing — a reach from a timer throws into nobody's catch, so the
      // list is what says it happened.
      const { events, violations, leftPending, liveIntervals, drainThrew } = await runLeg(
        fromRegistry(engineId),
        leg
      )
      expect(violations).toEqual([])
      // Nothing was still waiting when the seal came down: the barrier is a
      // drain of tracked handles, not a delay somebody chose. And no interval
      // was left ticking — a certified engine clears what it starts, because an
      // interval nobody clears is one no bounded drain can exhaust.
      expect(leftPending).toBe(0)
      expect(liveIntervals).toEqual([])
      expect(drainThrew).toBe('')
      const errors = events.filter(
        (event): event is Extract<AssistantEvent, { type: 'error' }> => event.type === 'error'
      )
      expect(errors.map((error) => error.message)).toEqual([])
      expect(events.some((event) => event.type === 'proposal')).toBe(true)
    })

    it('K1 — sends no credential, and calls nothing but the relay', async () => {
      const { requests } = await runLeg(fromRegistry(engineId), leg)
      expect(requests.length).toBeGreaterThan(0)
      for (const request of requests) {
        for (const forbidden of [
          'authorization',
          'x-api-key',
          'api-key',
          'cookie',
          'proxy-authorization',
          'x-goog-api-key'
        ]) {
          expect(request.headerNames, `a request carried ${forbidden}`).not.toContain(forbidden)
        }
        // The relay's own base, one path suffix, and the desk's token as the
        // only parameter — which is the only query the relay accepts at all.
        expect(request.url.startsWith('/api/assistant/relay/v1/')).toBe(true)
        const url = new URL(request.url, 'http://desk.invalid')
        expect([...url.searchParams.keys()]).toEqual(['token'])
      }
      const suffix = leg.api === 'anthropic' ? '/v1/messages' : '/chat/completions'
      expect(new URL(requests[0]!.url, 'http://desk.invalid').pathname).toBe(
        `/api/assistant/relay/v1${suffix}`
      )
    })

    it('K2 — offers exactly the five, out of tools/list', async () => {
      const { requests } = await runLeg(fromRegistry(engineId), leg)
      for (const request of requests) {
        expect(request.toolNames).toEqual(FIVE)
      }
    })

    it('K3a — the evaluate reaches the runtime rewritten, and is reported', async () => {
      const { events, seen } = await runLeg(fromRegistry(engineId), leg)
      const evaluate = seen.find((call) => call.name === 'experimental_evaluate')!
      // At the wire. The model asked without it; the server saw it with it.
      expect(evaluate.args.rehearsal).toBe(true)
      const rewrote = guardrails(events).filter((event) => event.action === 'rewrote')
      expect(rewrote).toHaveLength(1)
      expect(rewrote[0]!.tool).toBe('experimental_evaluate')
      expect(rewrote[0]!.detail).toContain('no rehearsal member')
      // And the guardrail is reported between the call and its result.
      const order = events.map((event) => event.type)
      const at = order.lastIndexOf('guardrail')
      expect(order[at - 1]).toBe('tool_call')
      expect(order[at + 1]).toBe('tool_result')
    })

    it('K3b — write_file never reaches the runtime, and the model is told', async () => {
      const { events, seen } = await runLeg(fromRegistry(engineId), leg)
      expect(seen.map((call) => call.name)).not.toContain('write_file')
      const refused = guardrails(events).filter((event) => event.action === 'refused')
      expect(refused).toHaveLength(1)
      expect(refused[0]!.tool).toBe('write_file')
      // A refusal the model is not told about is a turn it spends re-asking.
      const result = events.find(
        (event): event is Extract<AssistantEvent, { type: 'tool_result' }> =>
          event.type === 'tool_result' && event.name === 'write_file'
      )!
      expect(result.isError).toBe(true)
    })

    it('K3c — the proposal is DRAFT_V2, and T8’s unknowns arrive whole', async () => {
      const { events } = await runLeg(fromRegistry(engineId), leg)
      expect(proposals(events)).toHaveLength(1)
      expect(proposals(events)[0]!.document).toEqual(DRAFT_V2)
      expect(proposals(events)[0]!.unknowns).toEqual(scenario.unknowns)
    })

    it('emits the event stream the ADR names, in order, ending once', async () => {
      const { events } = await runLeg(fromRegistry(engineId), leg)
      expect(events.map((event) => event.type)).toEqual([
        'tool_call', // T1 get_schema
        'tool_result',
        'tool_call', // T2 list_examples
        'tool_result',
        'tool_call', // T3 get_example
        'tool_result',
        'tool_call', // T4 validate — invalid by design
        'tool_result',
        'tool_call', // T5 validate — valid
        'tool_result',
        'tool_call', // T6 experimental_evaluate — no rehearsal member
        'guardrail', //    …rewritten before the frame left the page
        'tool_result',
        'tool_call', // T7 write_file
        'guardrail', //    …refused; the frame never left the page
        'tool_result',
        'proposal', // T8
        'end'
      ])
      expect(events.filter((event) => event.type === 'end')).toHaveLength(1)
      expect(events.some((event) => event.type === 'error')).toBe(false)
      // The tier is off on every leg, so nothing degrades and nothing is said.
      expect(events.some((event) => event.type === 'thinking_unavailable')).toBe(false)
    })

    it('sends what the wire format defines, and records the rest for a reviewer', async ({
      annotate
    }) => {
      const { requests } = await runLeg(fromRegistry(engineId), leg)
      for (const request of requests) {
        expect(
          request.bodyMembers,
          `${engineId} ${leg.api} ${request.step} sent {${request.bodyMembers.join(', ')}}`
        ).toEqual(expect.arrayContaining(WIRE_MEMBERS[leg.api]))
      }
      await annotate(`${engineId} · ${leg.api} · ${leg.answerAs}\n${requestLog(requests)}`, 'notice')
    })

    it('carries every result back, so the script never repeats a step', async () => {
      const { requests } = await runLeg(fromRegistry(engineId), leg)
      // The scripted model reads n off the request's own messages. A run whose
      // steps are 1..8 with no repeat is a run whose message array carried
      // every tool result back in the shape the endpoint expects.
      expect(requests.map((request) => request.step)).toEqual([
        'T1',
        'T2',
        'T3',
        'T4',
        'T5',
        'T6',
        'T7',
        'T8'
      ])
      expect(requests.map((request) => request.results)).toEqual([0, 1, 2, 3, 4, 5, 6, 7])
    })
  })
})

/**
 * **The guard, shown to fail.**
 *
 * A conformance session that only ever runs conformant engines proves nothing
 * about the session. These two fixtures are engines the desk would never
 * certify — one reaches for a network global while its module loads, the other
 * schedules the reach for after its run has ended cleanly — and each one is
 * the exact hole round 2 found: the seal used to go up after the `import()` and
 * come down the instant the run resolved.
 *
 * They are loaded through the same `runLeg` every certified engine goes
 * through, so what is being shown is the harness and not a rehearsal of it.
 */
/**
 * The certification fixtures, reachable **through the registry's own loader**.
 *
 * Round 3's point: certified engines go through `loadEngine`, and a fixture
 * imported directly proves the seal on a route nothing ships. The loader takes
 * its table as a parameter for exactly this — the fixtures are never in the
 * build's own table, so nothing a `desk.json` can name reaches them, and the
 * path they travel is the one `builtin` travels.
 */
const CERTIFICATION_LOADERS = {
  'touches-on-load': async () =>
    (await import('./certification/touchesOnLoad')).touchesOnLoad,
  'touches-after-run': async () =>
    (await import('./certification/touchesAfterRun')).touchesAfterRun,
  'clears-its-interval': async () =>
    (await import('./certification/clearsItsInterval')).clearsItsInterval,
  'throws-while-draining': async () =>
    (await import('./certification/throwsWhileDraining')).throwsWhileDraining
}

const fromCertification = (id: keyof typeof CERTIFICATION_LOADERS) => () =>
  loadEngine(id, CERTIFICATION_LOADERS)

/**
 * **The guard, shown to fail.**
 *
 * A conformance session that only ever runs conformant engines proves nothing
 * about the session. These two are engines the desk would never certify — one
 * reaches for a network global while its module loads, the other schedules four
 * reaches for after its run has ended cleanly, at four shapes of schedule the
 * old fixed wait could not have seen — and each is the exact hole a round
 * found: the seal used to go up after the `import()`, and to come down after a
 * delay an engine could out-wait.
 *
 * They are loaded through the same `runLeg` and the same `loadEngine` every
 * certified engine goes through, so what is shown is the harness and not a
 * rehearsal of it.
 */
describe('the idle callback this harness brings with it', () => {
  /**
   * The shim on its own, under controlled time.
   *
   * `trackDeferredWork` is the whole of the barrier and it is not exported, so
   * this installs it exactly as a leg does — with fake timers already in place,
   * so what it captures is the clock this test advances — and takes it off
   * again afterwards.
   */
  function installed(work: (scope: Record<string, unknown>) => void): void {
    vi.useFakeTimers()
    const scope = globalThis as unknown as Record<string, unknown>
    const tracker = trackDeferredWork()
    try {
      work(scope)
    } finally {
      tracker.restore()
      vi.useRealTimers()
    }
  }

  it('does not run a timeout callback before its deadline, and says so when it does', () => {
    installed((scope) => {
      const seen: { didTimeout: boolean; budget: number }[] = []
      const request = scope.requestIdleCallback as typeof requestIdleCallback
      request((deadline) => seen.push({ didTimeout: deadline.didTimeout, budget: deadline.timeRemaining() }), {
        timeout: 60_000
      })
      // A minute is a minute. Firing it after a millisecond and calling that a
      // timeout is what let an engine be credited with work it would have
      // cancelled first.
      vi.advanceTimersByTime(1)
      expect(seen, 'invoked before its deadline').toEqual([])
      vi.advanceTimersByTime(59_998)
      expect(seen, 'invoked before its deadline').toEqual([])
      vi.advanceTimersByTime(1)
      expect(seen).toHaveLength(1)
      expect(seen[0]!.didTimeout, 'it ran because the deadline was reached').toBe(true)
      expect(seen[0]!.budget, 'a positive budget at the first read').toBeGreaterThan(0)
    })
  })

  it('never runs one the engine cancelled before its deadline', () => {
    installed((scope) => {
      let ran = 0
      const request = scope.requestIdleCallback as typeof requestIdleCallback
      const cancel = scope.cancelIdleCallback as typeof cancelIdleCallback
      const handle = request(() => (ran += 1), { timeout: 60_000 })
      vi.advanceTimersByTime(1_000)
      cancel(handle)
      vi.advanceTimersByTime(120_000)
      expect(ran).toBe(0)
    })
  })

  it('offers an idle slot with no timeout on the next turn, and says it did not time out', () => {
    installed((scope) => {
      const seen: boolean[] = []
      const request = scope.requestIdleCallback as typeof requestIdleCallback
      request((deadline) => seen.push(deadline.didTimeout))
      vi.advanceTimersByTime(1)
      expect(seen).toEqual([false])
    })
  })
})

describe('the seal, shown to fail', () => {
  const leg: Leg = { api: 'openai-compatible', answerAs: 'stream' }

  it('catches an engine that touches the network as its module loads', async () => {
    const { violations, events } = await runLeg(fromCertification('touches-on-load'), leg)
    expect(violations).toHaveLength(1)
    expect(violations[0]).toContain('globalThis.fetch')
    // And the leg fails: the import itself threw, so no session ran — by the
    // sentinel's own name, which is what says the seal is why.
    const errors = events.filter(
      (event): event is Extract<AssistantEvent, { type: 'error' }> => event.type === 'error'
    )
    expect(errors).toHaveLength(1)
    expect(errors[0]!.message).toContain('EngineTouchedANetworkGlobal')
    expect(events.some((event) => event.type === 'proposal')).toBe(false)
  })

  /**
   * Which schedules a leg's recorded reaches came from.
   *
   * The hyphen is in the character class deliberately: a marker read as a
   * prefix of a longer one — `idle` out of `idle-timeout` — makes two distinct
   * reaches indistinguishable, and one of them then looks recorded when only
   * the other was.
   */
  const markers = (violations: string[]) =>
    violations
      .map((violation) => /from=([a-z-]+)/.exec(violation)?.[1])
      .filter((marker): marker is string => marker !== undefined)

  it('catches the reaches an engine scheduled for after its run', async () => {
    const { violations, events, leftPending } = await runLeg(
      fromCertification('touches-after-run'),
      leg
    )
    // The run itself is clean — it ends, and nothing throws into the harness.
    expect(events.map((event) => event.type)).toEqual(['end'])
    for (const violation of violations) expect(violation).toContain('globalThis.fetch')
    // **Each schedule by name.** A count would be satisfied by one of them
    // firing repeatedly while the ones that actually defeat a fixed wait went
    // uncaught, which is the defect this fixture exists for.
    const from = new Set(markers(violations))
    expect(from.has('soon'), 'the 10ms reach').toBe(true)
    expect(from.has('far'), 'the five-minute reach').toBe(true)
    expect(from.has('chained'), 'the reach behind another timer').toBe(true)
    expect(from.has('promise'), 'the reach on a promise chain, with no timer').toBe(true)
    // **The one the environment does not have.** jsdom has no
    // `requestIdleCallback`, so this reach did nothing during certification and
    // would have run in Chrome after the seal lifted. The harness installs one —
    // and both of these are written the way idle work is actually written, one
    // guarded on the deadline's budget and one on its `didTimeout`, so a shim
    // that answered zero and false to everything would run them, watch them
    // decline to do anything, and certify a clean leg.
    expect(from.has('idle'), 'the reach guarded on the idle budget').toBe(true)
    expect(from.has('idle-timeout'), 'the reach guarded on didTimeout').toBe(true)
    // **And the one a browser would never have run.** A sixty-second idle
    // callback with a one-second cancellation beside it: the drain advances to
    // each schedule's own due time, so the cancellation happens first and the
    // idle work never does. Firing everything at once ran it — and told it a
    // deadline it had not reached had been reached.
    expect(from.has('idle-long'), 'a reach on an idle callback cancelled long before').toBe(false)
    // And the drain finished with nothing left waiting.
    expect(leftPending).toBe(0)
  })

  it('fails an engine that leaves an interval ticking, whatever it reaches on', async () => {
    // **The interval rule.** Running an interval a few times and clearing it on
    // the engine's behalf let a reach hide behind the fourth tick and report a
    // clean drain. An interval nobody cleared is a certification failure on its
    // own terms, reported by name — and this fixture's interval reach is never
    // run by the harness, so the failure is the interval and not the reach.
    const { liveIntervals, violations } = await runLeg(fromCertification('touches-after-run'), leg)
    expect(liveIntervals).toEqual(['setInterval(30000ms)'])
    expect(markers(violations)).not.toContain('interval')
  })

  it('lets an engine that clears its own interval past that rule, and still fails it', async () => {
    // A rule that fails everything proves as little as one that fails nothing.
    // This engine clears the interval it started — nothing to report — and
    // still reaches from a timeout, which is what fails the leg.
    const { liveIntervals, violations, leftPending } = await runLeg(
      fromCertification('clears-its-interval'),
      leg
    )
    expect(liveIntervals).toEqual([])
    expect(leftPending).toBe(0)
    const from = new Set(markers(violations))
    expect(from.has('kept'), 'the timeout it meant').toBe(true)
    // **A schedule with no handle is not cancelled by a handle nobody was
    // given.** `queueMicrotask` returns nothing, so every microtask used to be
    // filed under the one key `undefined` — and the `clearTimeout(undefined)`
    // this fixture writes next to it cancelled the most recent of them. The
    // drain then skipped it, and its reach was never recorded.
    expect(from.has('microtask'), 'the reach on a microtask it never cancelled').toBe(true)
    // **And nothing it cancelled.** A drain that fired cancelled handles would
    // report a reach this engine never made.
    expect(from.has('cancelled'), 'a reach it had already cancelled').toBe(false)
    expect(from.has('interval'), 'a reach on an interval it cleared').toBe(false)
    // The two whose cancellers the harness did not wrap, and the idle one it
    // now brings with it: each was scheduled and cancelled, and none may be
    // fired on the engine's behalf.
    expect(from.has('immediate'), 'a reach on an immediate it cleared').toBe(false)
    expect(from.has('raf'), 'a reach on an animation frame it cancelled').toBe(false)
    expect(from.has('idle'), 'a reach on an idle callback it cancelled').toBe(false)
  })

  it('restores every global when a deferred callback throws', async () => {
    // The cleanup used to be one `finally` with the drain first in it, so a
    // callback that threw left the block before the sentinels and the timer
    // wrappers came off — and every leg after it ran against globals nobody
    // restored.
    const timers = ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'queueMicrotask']
    const scope = globalThis as unknown as Record<string, unknown>
    const before = timers.map((name) => scope[name])
    const { drainThrew } = await runLeg(fromCertification('throws-while-draining'), leg)
    expect(drainThrew).toContain('nobody was awaiting')
    // The timer functions are the harness's only business and come back by
    // identity.
    expect(timers.map((name) => scope[name])).toEqual(before)
    // The network globals are `fetch`-stubbed by the leg itself, so what is
    // asserted of them is that none is still a sentinel.
    for (const name of NETWORK_GLOBALS) {
      const current = scope[name] as { name?: string } | undefined
      expect(current?.name ?? '', `${name} is still sealed`).not.toBe('sealed')
    }
  })

  it('leaves no sentinel behind when the leg is over', async () => {
    // A seal that did not lift would break every test that runs after it; one
    // that lifted early is the defect above. `fetch` is the harness's own
    // scripted stub by then rather than the browser's, so what is asserted is
    // that none of the four is still a sentinel.
    const sealed = (name: string) =>
      ((globalThis as unknown as Record<string, { name?: string }>)[name]?.name ?? '') === 'sealed'
    await runLeg(fromRegistry('builtin'), leg)
    for (const name of NETWORK_GLOBALS) {
      expect(sealed(name), `${name} is still sealed`).toBe(false)
    }
    // And WebSocket, which the harness never stubs here, is the real one.
    expect(typeof globalThis.WebSocket).toBe('function')
  })

  it('leaves the timer functions exactly as it found them', async () => {
    const names = [
      'setTimeout',
      'clearTimeout',
      'setInterval',
      'clearInterval',
      'queueMicrotask',
      'setImmediate',
      'clearImmediate',
      'requestAnimationFrame',
      'cancelAnimationFrame'
    ]
    const scope = globalThis as unknown as Record<string, unknown>
    const before = names.map((name) => scope[name])
    // And the one the harness **adds**: it must be gone again, not left as a
    // present-but-useless property for every test that runs after this one.
    expect('requestIdleCallback' in scope, 'jsdom has no idle callback').toBe(false)
    await runLeg(fromRegistry('builtin'), leg)
    expect(names.map((name) => scope[name])).toEqual(before)
    expect('requestIdleCallback' in scope, 'the harness left its own behind').toBe(false)
    expect('cancelIdleCallback' in scope, 'the harness left its own behind').toBe(false)
  })
})
