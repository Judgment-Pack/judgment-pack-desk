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
import { openAssistantConnection, runAssistantSession } from '../session'
import scenario from './scenario.json'
import { scriptedModel, type RecordedRequest } from './scriptedModel'
import { RECORDED_TOOLS, scriptedRuntime, type ServerObservation } from './scriptedServer'
import type { AssistantEvent } from '../engine'

const RELAY_BASE = '/api/assistant/relay/v1?token=conformance-token'
const FIVE = scenario.scenarioTools
const DRAFT_V2 = scenario.documents.DRAFT_V2 as unknown

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
}

/** One whole session, driven exactly as the page drives it. */
async function runLeg(engineId: (typeof CERTIFIED_ENGINES)[number], leg: Leg): Promise<Run> {
  const model = scriptedModel({ api: leg.api, answerAs: leg.answerAs })
  vi.stubGlobal('fetch', model.fetch)
  const runtime = await scriptedRuntime()
  const events: AssistantEvent[] = []
  const connection = await openAssistantConnection({
    allowed: FIVE,
    onEvent: (event) => events.push(event),
    transport: runtime.transport
  })
  try {
    const engine = await loadEngine(engineId)
    await runAssistantSession(
      engine,
      {
        // The prompt the desk fetched over prompts/get. Its text is the
        // runtime's; what matters here is that the engine sends it and adds
        // no authoring instructions of its own.
        prompt: `${scenario.policy}`,
        tools: connection.tools,
        callTool: connection.callTool,
        model: { family: leg.api, baseUrl: RELAY_BASE, model: 'scripted-model' },
        thinking: { tier: 'off' },
        signal: new AbortController().signal
      },
      (event) => events.push(event)
    )
  } finally {
    await connection.close()
    await runtime.close()
  }
  return { events, requests: model.requests, seen: runtime.seen }
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
      const { events, seen } = await runLeg(engineId, leg)

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

    it('K1 — sends no credential, and calls nothing but the relay', async () => {
      const { requests } = await runLeg(engineId, leg)
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
      const { requests } = await runLeg(engineId, leg)
      for (const request of requests) {
        expect(request.toolNames).toEqual(FIVE)
      }
    })

    it('K3a — the evaluate reaches the runtime rewritten, and is reported', async () => {
      const { events, seen } = await runLeg(engineId, leg)
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
      const { events, seen } = await runLeg(engineId, leg)
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
      const { events } = await runLeg(engineId, leg)
      expect(proposals(events)).toHaveLength(1)
      expect(proposals(events)[0]!.document).toEqual(DRAFT_V2)
      expect(proposals(events)[0]!.unknowns).toEqual(scenario.unknowns)
    })

    it('emits the event stream the ADR names, in order, ending once', async () => {
      const { events } = await runLeg(engineId, leg)
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
      const { requests } = await runLeg(engineId, leg)
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
