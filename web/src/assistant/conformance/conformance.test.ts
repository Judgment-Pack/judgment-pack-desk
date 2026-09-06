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
import runtime from './runtime.json'
import type { AssistantEvent, Engine } from '../engine'

const FIVE = scenario.scenarioTools
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
 * `fire` runs it now; `cancel` stops the real handle. An interval is pending
 * until it is cancelled, so it is fired a bounded number of times and then
 * cleared — an engine that reaches on a tick reaches on the first one.
 */
interface Tracked {
  kind: string
  pending: boolean
  runs: number
  fire(): void
  cancel(): void
}

const INTERVAL_RUNS = 3

/**
 * Track every handle an engine creates, instead of waiting for one to fire.
 *
 * The barrier this replaces was a fixed wait — 200ms — and a fixed wait is a
 * delay an engine can out-wait: 201ms, an interval, or a timer that schedules
 * another timer. Round 3 named all three. So the timer functions are wrapped
 * for the sealed window: each call is recorded **and** scheduled for real, so
 * an engine that legitimately depends on a timer still makes progress; and the
 * drain afterwards fires whatever has not fired yet, repeatedly, so a chain is
 * followed to its end.
 *
 * `queueMicrotask` is wrapped and forwarded rather than deferred: promise
 * machinery runs on it, and holding one back would deadlock the very run this
 * is measuring.
 */
function trackDeferredWork(): {
  pending(): Tracked[]
  restore(): void
} {
  const scope = globalThis as unknown as Record<string, unknown>
  const before = new Map<string, unknown>()
  const tracked: Tracked[] = []
  const keep = (name: string) => before.set(name, scope[name])

  const realSetTimeout = globalThis.setTimeout
  const realClearTimeout = globalThis.clearTimeout
  const realSetInterval = globalThis.setInterval
  const realClearInterval = globalThis.clearInterval
  const realQueueMicrotask = globalThis.queueMicrotask

  /** One entry, and the handle the caller gets back. */
  function record(kind: string, run: () => void, schedule: (wrapped: () => void) => unknown,
                  clear: (handle: unknown) => void, repeating: boolean): unknown {
    const entry: Tracked = {
      kind,
      pending: true,
      runs: 0,
      fire() {
        if (!entry.pending) return
        entry.runs += 1
        if (!repeating) {
          entry.pending = false
          clear(handle)
        } else if (entry.runs >= INTERVAL_RUNS) {
          // Bounded, then cleared: an interval is pending for ever otherwise.
          entry.pending = false
          clear(handle)
        }
        run()
      },
      cancel() {
        entry.pending = false
        clear(handle)
      }
    }
    const handle = schedule(() => {
      if (!repeating) entry.pending = false
      entry.runs += 1
      run()
    })
    tracked.push(entry)
    return handle
  }

  keep('setTimeout')
  scope.setTimeout = (fn: unknown, ms?: number, ...args: unknown[]) => {
    if (typeof fn !== 'function') return realSetTimeout(fn as never, ms)
    return record(
      'setTimeout',
      () => (fn as (...rest: unknown[]) => void)(...args),
      (wrapped) => realSetTimeout(wrapped, ms),
      (handle) => realClearTimeout(handle as never),
      false
    )
  }
  keep('setInterval')
  scope.setInterval = (fn: unknown, ms?: number, ...args: unknown[]) => {
    if (typeof fn !== 'function') return realSetInterval(fn as never, ms)
    return record(
      'setInterval',
      () => (fn as (...rest: unknown[]) => void)(...args),
      (wrapped) => realSetInterval(wrapped, ms),
      (handle) => realClearInterval(handle as never),
      true
    )
  }
  keep('queueMicrotask')
  scope.queueMicrotask = (fn: () => void) =>
    record(
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
        () => fn(...(args as [])),
        (wrapped) => realSetImmediate(wrapped),
        (handle) => realClearImmediate?.(handle),
        false
      )
  }
  if (typeof scope.requestAnimationFrame === 'function') {
    const realRaf = scope.requestAnimationFrame as (fn: (t: number) => void) => unknown
    const realCancelRaf = scope.cancelAnimationFrame as ((handle: unknown) => void) | undefined
    keep('requestAnimationFrame')
    scope.requestAnimationFrame = (fn: (t: number) => void) =>
      record(
        'requestAnimationFrame',
        () => fn(0),
        (wrapped) => realRaf(() => wrapped()),
        (handle) => realCancelRaf?.(handle),
        false
      )
  }

  return {
    pending: () => tracked.filter((entry) => entry.pending),
    restore() {
      for (const [name, value] of before) scope[name] = value
    }
  }
}

/**
 * Run everything an engine left behind, under the seal, until nothing is left.
 *
 * Repeatedly, because a timer may schedule another one — the chained shape the
 * fixed wait could not see. Bounded, because an engine that schedules for ever
 * must end the drain rather than the drain ending the suite; what is left over
 * is reported and asserted rather than waited on.
 */
async function drainDeferredWork(tracker: { pending(): Tracked[] }): Promise<number> {
  for (let round = 0; round < 32; round += 1) {
    const due = tracker.pending()
    if (due.length === 0) break
    for (const entry of due) entry.fire()
    for (let tick = 0; tick < 5; tick += 1) await Promise.resolve()
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
        tools: ready.tools,
        callTool: ready.callTool,
        model: { family: leg.api, model: 'scripted-model', call },
        thinking: { tier: 'off' },
        signal: new AbortController().signal
      },
      (event) => events.push(event)
    )
  } catch (cause) {
    // A load that threw or a run that failed is the leg's result, not the
    // harness's problem: it lands on the stream like any other failure.
    events.push({ type: 'error', message: `${(cause as Error).name}: ${(cause as Error).message}` })
  } finally {
    if (tracker !== null) {
      leftPending = await drainDeferredWork(tracker)
      tracker.restore()
    }
    seal?.lift()
    await connection.close()
    await runtime.close()
  }
  return {
    events,
    requests: model.requests,
    seen: runtime.seen,
    violations: seal?.violations ?? [],
    leftPending
  }
}

/** The registry's own loader, for the engines this build certifies. */
const fromRegistry = (id: (typeof CERTIFIED_ENGINES)[number]) => () => loadEngine(id)

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
      const { events, violations, leftPending } = await runLeg(fromRegistry(engineId), leg)
      expect(violations).toEqual([])
      // Nothing was still waiting when the seal came down: the barrier is a
      // drain of tracked handles, not a delay somebody chose.
      expect(leftPending).toBe(0)
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
    (await import('./certification/touchesAfterRun')).touchesAfterRun
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

  it('catches four reaches an engine scheduled for after its run', async () => {
    const { violations, events, leftPending } = await runLeg(
      fromCertification('touches-after-run'),
      leg
    )
    // The run itself is clean — it ends, and nothing throws into the harness.
    // Four schedules, none of which a fixed wait would have caught: soon, five
    // minutes out, chained behind another timer, and on an interval.
    expect(events.map((event) => event.type)).toEqual(['end'])
    for (const violation of violations) expect(violation).toContain('globalThis.fetch')
    // **Each schedule by name.** A count would be satisfied by an interval
    // ticking four times while the three that actually defeat a fixed wait went
    // uncaught, which is the defect this fixture exists for.
    const from = (marker: string) =>
      violations.some((violation) => violation.includes(`from=${marker}`))
    expect(from('soon'), 'the 10ms reach').toBe(true)
    expect(from('far'), 'the five-minute reach').toBe(true)
    expect(from('chained'), 'the reach behind another timer').toBe(true)
    expect(from('interval'), 'the reach on an interval').toBe(true)
    // And the drain finished: nothing was left waiting when the seal lifted.
    expect(leftPending).toBe(0)
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
    const before = ['setTimeout', 'setInterval', 'queueMicrotask'].map(
      (name) => (globalThis as unknown as Record<string, unknown>)[name]
    )
    await runLeg(fromRegistry('builtin'), leg)
    const after = ['setTimeout', 'setInterval', 'queueMicrotask'].map(
      (name) => (globalThis as unknown as Record<string, unknown>)[name]
    )
    expect(after).toEqual(before)
  })
})
