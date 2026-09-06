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

class EngineTouchedANetworkGlobal extends Error {}

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
    const sentinel = function sealed(): never {
      const reach =
        `the engine reached for globalThis.${name}; a session's only reach to a model is ` +
        `session.model.call, and its only reach to the runtime is session.callTool`
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
 * Let deferred work run while the seal is still up.
 *
 * A timer an engine set before it finished is exactly the reach the seal used
 * to miss, so the barrier is real time rather than a microtask flush: fake
 * timers fight the SDK's own in-memory transport, which settles on
 * microtasks, and a run that hangs is worse than a bound that is stated. The
 * length is the interval the fixture schedules inside, several times over.
 */
const DRAIN_MS = 200
async function drainDeferredWork(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, DRAIN_MS))
  // And a few microtask ticks, for anything the timers queued on their way out.
  for (let tick = 0; tick < 5; tick += 1) await Promise.resolve()
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
  try {
    const ready = await connection.ready
    const call = bindModelCall()
    // Sealed from here: the engine's chunk is imported under it.
    seal = sealNetwork()
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
    await drainDeferredWork()
    seal?.lift()
    await connection.close()
    await runtime.close()
  }
  return {
    events,
    requests: model.requests,
    seen: runtime.seen,
    violations: seal?.violations ?? []
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
      const { events, violations } = await runLeg(fromRegistry(engineId), leg)
      expect(violations).toEqual([])
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
describe('the seal, shown to fail', () => {
  const leg: Leg = { api: 'openai-compatible', answerAs: 'stream' }

  it('catches an engine that touches the network as its module loads', async () => {
    const { violations, events } = await runLeg(
      async () => (await import('./certification/touchesOnLoad')).touchesOnLoad,
      leg
    )
    expect(violations).toHaveLength(1)
    expect(violations[0]).toContain('globalThis.fetch')
    // And the leg fails: the import itself threw, so no session ran.
    expect(events.some((event) => event.type === 'error')).toBe(true)
    expect(events.some((event) => event.type === 'proposal')).toBe(false)
  })

  it('catches an engine that schedules its reach for after the run', async () => {
    const { violations, events } = await runLeg(
      async () => (await import('./certification/touchesAfterRun')).touchesAfterRun,
      leg
    )
    // The run itself is clean — it ends, and nothing throws into the harness.
    // The barrier is what sees it: the timer fires while the seal is still up.
    expect(violations).toHaveLength(1)
    expect(violations[0]).toContain('globalThis.fetch')
    expect(events.map((event) => event.type)).toEqual(['end'])
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
})
