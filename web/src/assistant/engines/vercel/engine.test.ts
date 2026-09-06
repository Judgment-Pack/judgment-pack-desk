/**
 * The `vercel` engine's own promises, each measured where it is kept.
 *
 * The whole scenario — eight steps, both wire formats, both answer shapes, the
 * real runtime's tool definitions and its recorded answers, every network
 * global sealed — is `assistant/conformance/`, and it is what certifies this
 * adapter. What is here is the handful of properties that suite would only ever
 * exercise incidentally, and the ones that are about **this SDK** rather than
 * about the contract: the address discipline on the `fetch` the providers are
 * given, the credential that never leaves it, the two layers that put
 * `rehearsal: true` on an evaluate, the unhandled-rejection guard the ADR names,
 * and `end` exactly once on the paths the scenario never takes.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { ASSISTANT_ENGINES, ASSISTANT_TOOLS } from '../../../config/deskConfig'
import { CERTIFICATION_IS_TOTAL, CERTIFIED_ENGINES, loadEngine } from '../index'
import { ChannelHasOneConsumer, eventChannel } from './channel'
import { vercel } from './index'
import { REHEARSAL_HOOK, claimPromises, sdkThinking } from './loop'
import {
  ADDRESS_REFUSED,
  PLACEHOLDER_ORIGIN,
  placeholderBase,
  reframe,
  relayFetch,
  signatureLedger,
  suffixOf,
  withoutTruncatedThinking
} from './relay'
import { normalize, wireFor } from '../../thinking'
import { REFUTATION_MARKER } from '../../refutation'
import type { streamText } from 'ai'
import type { AssistantEvent, AssistantSession, McpTool, ModelCall, McpToolResult } from '../../engine'

const TOOLS: McpTool[] = [
  { name: 'validate', description: 'check a document', inputSchema: { type: 'object' } },
  {
    name: 'experimental_evaluate',
    description: 'rehearse an evaluation',
    inputSchema: { type: 'object' }
  }
]

const PROPOSAL_TEXT =
  'Here it is.\n\n```json\n' +
  JSON.stringify({ proposal: { kind: 'create', document: { id: 'p' }, unknowns: ['who signs'] } }) +
  '\n```\n'

/** One assistant turn, as the OpenAI-compatible wire streams it. */
function turn(step: {
  text?: string
  tool?: { name: string; args: unknown }
  /** What an endpoint that reasons on its own default behaviour sends. */
  reasoning?: string[]
}): string {
  const frame = (choices: unknown[]) =>
    `data: ${JSON.stringify({ id: 'c', object: 'chat.completion.chunk', model: 'm', choices })}\n\n`
  const lines = [frame([{ index: 0, delta: { role: 'assistant', content: null }, finish_reason: null }])]
  for (const piece of step.reasoning ?? []) {
    lines.push(frame([{ index: 0, delta: { reasoning_content: piece }, finish_reason: null }]))
  }
  // A turn may answer **and** call a tool, which is what an endpoint does when
  // it says what it is about to do — and it is the shape the "a tool-only turn
  // neither counts nor resets" rule needs to be measured against.
  if (step.tool && step.text !== undefined) {
    lines.push(frame([{ index: 0, delta: { content: step.text }, finish_reason: null }]))
  }
  if (step.tool) {
    lines.push(
      frame([
        {
          index: 0,
          delta: {
            tool_calls: [
              {
                index: 0,
                id: `call_${step.tool.name}`,
                type: 'function',
                function: { name: step.tool.name, arguments: JSON.stringify(step.tool.args) }
              }
            ]
          },
          finish_reason: null
        }
      ]),
      frame([{ index: 0, delta: {}, finish_reason: 'tool_calls' }])
    )
  } else {
    lines.push(
      frame([{ index: 0, delta: { content: step.text ?? '' }, finish_reason: null }]),
      frame([{ index: 0, delta: {}, finish_reason: 'stop' }])
    )
  }
  lines.push('data: [DONE]\n\n')
  return lines.join('')
}

/** One Anthropic SSE turn: a thinking block, then a tool call or the text. */
function anthropicTurn(step: {
  reasoning?: string
  signature?: string
  splitSignature?: boolean
  text?: string
  tool?: { name: string; args: unknown }
}): string {
  const lines: string[] = []
  const event = (name: string, object: Record<string, unknown>) =>
    lines.push(`event: ${name}\ndata: ${JSON.stringify({ type: name, ...object })}\n\n`)
  event('message_start', {
    message: { id: 'm', type: 'message', role: 'assistant', model: 'm', content: [], stop_reason: null, usage: { input_tokens: 1, output_tokens: 0 } }
  })
  let index = 0
  if (step.reasoning !== undefined) {
    event('content_block_start', { index, content_block: { type: 'thinking', thinking: '' } })
    event('content_block_delta', { index, delta: { type: 'thinking_delta', thinking: step.reasoning } })
    const signature = step.signature ?? 'c2ln'
    if (step.splitSignature === true) {
      const half = Math.floor(signature.length / 2)
      event('content_block_delta', { index, delta: { type: 'signature_delta', signature: signature.slice(0, half) } })
      event('content_block_delta', { index, delta: { type: 'signature_delta', signature: signature.slice(half) } })
    } else {
      event('content_block_delta', { index, delta: { type: 'signature_delta', signature } })
    }
    event('content_block_stop', { index })
    index += 1
  }
  if (step.tool !== undefined) {
    event('content_block_start', {
      index,
      content_block: { type: 'tool_use', id: `toolu_${step.tool.name}`, name: step.tool.name, input: {} }
    })
    event('content_block_delta', {
      index,
      delta: { type: 'input_json_delta', partial_json: JSON.stringify(step.tool.args) }
    })
  } else {
    event('content_block_start', { index, content_block: { type: 'text', text: '' } })
    event('content_block_delta', { index, delta: { type: 'text_delta', text: step.text ?? '' } })
  }
  event('content_block_stop', { index })
  event('message_delta', {
    delta: { stop_reason: step.tool === undefined ? 'end_turn' : 'tool_use', stop_sequence: null },
    usage: { output_tokens: 1 }
  })
  event('message_stop', {})
  return lines.join('')
}

interface Recorded {
  suffix: string
  headerNames: string[]
  body: Record<string, unknown>
}

/** A model capability that answers turn by turn and remembers what it was sent. */
function scriptedCall(turns: string[]): { call: ModelCall; seen: Recorded[] } {
  const seen: Recorded[] = []
  const call: ModelCall = async (suffix, request) => {
    const body = JSON.parse(request.body) as Record<string, unknown>
    seen.push({
      suffix,
      headerNames: Object.keys(request.headers ?? {}).map((name) => name.toLowerCase()),
      body
    })
    const answer = turns[Math.min(seen.length - 1, turns.length - 1)]!
    return new Response(answer, { status: 200, headers: { 'content-type': 'text/event-stream' } })
  }
  return { call, seen }
}

function session(
  call: ModelCall,
  overrides: Partial<AssistantSession> = {},
  callTool?: AssistantSession['callTool']
): AssistantSession {
  return {
    prompt: 'the runtime’s prompt',
    testPrompt: 'the runtime’s test_pack guidance',
    tools: TOOLS,
    callTool:
      callTool ??
      (async (): Promise<McpToolResult> => ({ content: [{ type: 'text', text: '{"status":"ok"}' }] })),
    model: { family: 'openai-compatible', model: 'a-model', call },
    thinking: normalize('off', 'openai-compatible'),
    signal: new AbortController().signal,
    ...overrides
  }
}

async function drain(events: AsyncIterable<AssistantEvent>): Promise<AssistantEvent[]> {
  const seen: AssistantEvent[] = []
  for await (const event of events) seen.push(event)
  return seen
}

/** A promise, or a marker where it did not settle inside the bound. */
async function within(ms: number, work: Promise<unknown>): Promise<string> {
  return Promise.race([
    work.then(() => 'settled'),
    new Promise<string>((resolve) => setTimeout(() => resolve('STILL WAITING'), ms))
  ])
}

describe('the ordered channel, when the consumer stops listening', () => {
  it('settles the delivery it was yielding when the drain is returned', async () => {
    // The defect: `drain` takes an entry off the queue **before** yielding it,
    // so a consumer that stops at exactly that event leaves a delivery nothing
    // is holding — not the queue, and not the loop that never resumes. The
    // producer waited on it for ever.
    const channel = eventChannel()
    const first = channel.push({ type: 'end' })
    const second = channel.push({ type: 'end' })
    const drain = channel.drain()
    expect((await drain.next()).value).toEqual({ type: 'end' })
    await drain.return(undefined)
    expect(await within(500, first), 'the event that was in flight').toBe('settled')
    expect(await within(500, second), 'the events still queued behind it').toBe('settled')
  })

  it('refuses a second consumer by name, and takes nothing from the first', async () => {
    // One channel, one consumer. The in-flight slot is a single slot because
    // there is a single reader: two drains taking concurrently would overwrite
    // each other's, and the overwritten entry would be in neither the queue nor
    // the slot — a delivery nothing could ever settle.
    const channel = eventChannel()
    const first = channel.push({ type: 'end' })
    const second = channel.push({ type: 'end' })
    const one = channel.drain()
    const two = channel.drain()
    expect((await one.next()).value).toEqual({ type: 'end' })
    const refused = await two.next().then(
      () => undefined,
      (cause: unknown) => cause as Error
    )
    expect(refused?.name).toBe('ChannelHasOneConsumer')
    expect(refused).toBeInstanceOf(ChannelHasOneConsumer)
    expect(refused?.message).toContain('already has a consumer')
    // And the refusal left the first reader and both deliveries untouched.
    await one.return(undefined)
    expect(await within(500, first), 'the event the first reader held').toBe('settled')
    expect(await within(500, second), 'the event still queued behind it').toBe('settled')
  })

  it('settles a delivery abandoned before the drain ever ran', async () => {
    const channel = eventChannel()
    const waiting = channel.push({ type: 'end' })
    channel.abandon()
    expect(await within(500, waiting)).toBe('settled')
  })
})

describe('a session that was already over before the run began', () => {
  it.each(['off', 'ultra'] as const)('emits nothing and asks nothing, at tier %s', async (tier) => {
    // An aborted signal is a run that is over. It used to abort the SDK's
    // controller and nothing else, so the engine still said
    // `thinking_unavailable`, still built a provider, and still delivered
    // `end` — an engine reporting on a session that never happened.
    const controller = new AbortController()
    controller.abort()
    let requests = 0
    let tools = 0
    const call: ModelCall = async () => {
      requests += 1
      return new Response(turn({ text: PROPOSAL_TEXT }), {
        status: 200,
        headers: { 'content-type': 'text/event-stream' }
      })
    }
    const events = await drain(
      vercel.start(
        session(call, { signal: controller.signal, thinking: normalize(tier, 'openai-compatible') }, async () => {
          tools += 1
          return { content: [] }
        })
      )
    )
    expect(events).toEqual([])
    expect(requests, 'a request was made for a run that was already over').toBe(0)
    expect(tools, 'the runtime was asked by a run that was already over').toBe(0)
  })
})

describe('when the thing being awaited wins its race with the abort', () => {
  it('delivers nothing after the run closed, and no error and no end', async () => {
    // `withAbort` settles with the **value** when the work wins by a
    // microtask; the abort then runs while the loop is still holding it. What
    // stops the loop delivering is not the abort — it is that the run is
    // already marked closed and the delivery path reads that.
    const controller = new AbortController()
    const { call } = scriptedCall([
      turn({ tool: { name: 'validate', args: { document: {} } } }),
      turn({ text: PROPOSAL_TEXT })
    ])
    const events: AssistantEvent[] = []
    const iterator = vercel.start(
      session(call, { signal: controller.signal }, async () => {
        // The answer arrives, and the session is aborted in the same turn: the
        // value wins, and the abort lands a microtask later.
        const answer: McpToolResult = { content: [{ type: 'text', text: '{"status":"ok"}' }] }
        controller.abort()
        return answer
      })
    )[Symbol.asyncIterator]()
    const first = await iterator.next()
    expect(first.value).toMatchObject({ type: 'tool_call' })
    for (;;) {
      const step = await iterator.next()
      if (step.done === true) break
      events.push(step.value)
    }
    expect(events, 'an event was delivered after the run closed').toEqual([])
  })

  it('delivers no error where the awaited thing failed first', async () => {
    // The other half: the underlying promise **rejected** just before the
    // abort, so the loop's catch sees the original failure rather than a
    // cancellation — and would have reported an `error` and an `end` for a run
    // that was already over.
    const controller = new AbortController()
    const { call } = scriptedCall([
      turn({ tool: { name: 'validate', args: { document: {} } } }),
      turn({ text: PROPOSAL_TEXT })
    ])
    const events: AssistantEvent[] = []
    const iterator = vercel.start(
      session(call, { signal: controller.signal }, async () => {
        controller.abort()
        throw new Error('the socket went away')
      })
    )[Symbol.asyncIterator]()
    expect((await iterator.next()).value).toMatchObject({ type: 'tool_call' })
    for (;;) {
      const step = await iterator.next()
      if (step.done === true) break
      events.push(step.value)
    }
    expect(events).toEqual([])
  })
})

describe('the registry', () => {
  it('cannot make an engine loadable without certifying it', () => {
    // The loader table and the certified list used to be independent: adding a
    // third engine and its loader while forgetting the list compiled, let a
    // `desk.json` select it, and left `describe.each` never certifying it.
    expect(CERTIFICATION_IS_TOTAL).toBe(true)
    const total: typeof CERTIFICATION_IS_TOTAL = true
    expect(total).toBe(true)
    // @ts-expect-error — the assertion is a type equality, not a boolean: it
    // stops compiling the day the two sets differ, which is the whole guard.
    const wrong: typeof CERTIFICATION_IS_TOTAL = false
    expect(wrong).toBe(false)
    // And at runtime: every id the registry can load is one the suite runs.
    for (const id of ASSISTANT_ENGINES) {
      expect(CERTIFIED_ENGINES, `${id} is loadable but not certified`).toContain(id)
    }
    expect([...CERTIFIED_ENGINES].sort()).toEqual([...ASSISTANT_ENGINES].sort())
  })

  it('loads this adapter for the id a desk.json names, and not another', async () => {
    // A `vercel` entry pointing at `builtin` would pass every conformance leg
    // twice over and certify nothing — which is exactly what the fallback this
    // build removed used to do on purpose.
    const engine = await loadEngine('vercel')
    expect(engine.id).toBe('vercel')
    expect(engine).toBe(vercel)
  })
})

describe('the address the SDK composes, and what this desk will send', () => {
  it('reduces the SDK’s absolute URL to the suffix the built-in engine uses', () => {
    expect(suffixOf(`${PLACEHOLDER_ORIGIN}/chat/completions`, placeholderBase('openai-compatible')))
      .toBe('chat/completions')
    expect(suffixOf(`${PLACEHOLDER_ORIGIN}/v1/messages`, placeholderBase('anthropic'))).toBe(
      'v1/messages'
    )
  })

  it('refuses another origin, a query, a fragment and an empty path', () => {
    const base = placeholderBase('openai-compatible')
    for (const url of [
      'https://api.openai.com/v1/chat/completions',
      'http://127.0.0.1:8791/api/assistant/relay/v1/chat/completions',
      `${PLACEHOLDER_ORIGIN}/chat/completions?api-version=2024-02-01`,
      `${PLACEHOLDER_ORIGIN}/chat/completions#x`,
      `${PLACEHOLDER_ORIGIN}/`,
      'not a url'
    ]) {
      expect(suffixOf(url, base), url).toBeUndefined()
    }
    // And the Anthropic base is a real second check: a path under the same
    // origin that is not under the family's base does not travel either.
    expect(suffixOf(`${PLACEHOLDER_ORIGIN}/chat/completions`, placeholderBase('anthropic')))
      .toBeUndefined()
  })

  it('sends nothing at all where the address is not the relay’s', async () => {
    let called = 0
    const call: ModelCall = async () => {
      called += 1
      return new Response('{}')
    }
    const fetch = relayFetch({ family: 'openai-compatible', call, signal: new AbortController().signal })
    await expect(fetch('https://api.openai.com/v1/chat/completions', { body: '{}' })).rejects.toThrow(
      ADDRESS_REFUSED
    )
    expect(called).toBe(0)
  })

  it('strips every header outside the protocol’s own, the placeholder key included', async () => {
    const seen: string[][] = []
    const call: ModelCall = async (_suffix, request) => {
      seen.push(Object.keys(request.headers ?? {}).map((name) => name.toLowerCase()))
      return new Response('{}', { headers: { 'content-type': 'application/json' } })
    }
    const fetch = relayFetch({ family: 'anthropic', call, signal: new AbortController().signal })
    await fetch(`${PLACEHOLDER_ORIGIN}/v1/messages`, {
      body: '{}',
      headers: {
        'content-type': 'application/json',
        'anthropic-version': '2023-06-01',
        'x-api-key': 'placeholder-the-desk-relay-injects-the-key',
        authorization: 'Bearer sk-nope',
        'user-agent': 'ai-sdk/anthropic'
      }
    })
    expect(seen).toEqual([['content-type', 'anthropic-version']])
  })
})

describe('an endpoint that ignored `stream`', () => {
  // The SDK reads the answer it *asked for*: a JSON object fed to its
  // event-source parser yields no events at all. The desk requires the opposite,
  // so a whole answer is presented in the framing the same protocol defines.
  it('is presented to the SDK as the chunks the OpenAI-compatible wire defines', () => {
    const framed = reframe('openai-compatible', {
      id: 'c',
      choices: [
        {
          index: 0,
          message: {
            role: 'assistant',
            content: null,
            tool_calls: [{ id: 'call_1', type: 'function', function: { name: 'validate', arguments: '{}' } }]
          },
          finish_reason: 'tool_calls'
        }
      ]
    })
    const events = framed
      .split('\n\n')
      .filter((line) => line.startsWith('data: ') && !line.includes('[DONE]'))
      .map((line) => JSON.parse(line.slice(6)) as { choices: { delta?: Record<string, unknown> }[] })
    expect(framed).toContain('data: [DONE]')
    // The one member a chunk carries that a whole message does not.
    expect((events[0]!.choices[0]!.delta!.tool_calls as { index: number }[])[0]!.index).toBe(0)
  })

  it('is presented to the SDK as the events the Anthropic wire defines', () => {
    const framed = reframe('anthropic', {
      id: 'm',
      role: 'assistant',
      content: [{ type: 'text', text: 'hello' }],
      stop_reason: 'end_turn',
      usage: { output_tokens: 3 }
    })
    const names = [...framed.matchAll(/^event: (.+)$/gm)].map((match) => match[1])
    expect(names).toEqual([
      'message_start',
      'content_block_start',
      'content_block_delta',
      'content_block_stop',
      'message_delta',
      'message_stop'
    ])
    // `usage` is on `message_delta` because the protocol puts it there — and
    // because the SDK's own schema refuses the event without it.
    expect(framed).toContain('"usage":{"output_tokens":3}')
  })
})

describe('rehearsal, on two layers', () => {
  it('is a compile error the day the SDK renames the hook', () => {
    // The hook is `experimental_`, and `streamText`'s options carry a rest
    // parameter, so a misspelling is accepted in silence and the rewrite is
    // simply never applied. `REHEARSAL_HOOK` is written as a key of the SDK's
    // own options type; this is the same claim, asserted where a reader is.
    expect(REHEARSAL_HOOK).toBe('experimental_refineToolInput')
    const named: keyof Parameters<typeof streamText>[0] = REHEARSAL_HOOK
    expect(named).toBe('experimental_refineToolInput')
    // @ts-expect-error — a misspelling is not a key of the SDK's options, which
    // is the whole of the guard: delete the `satisfies` in `loop.ts` and an
    // upstream rename produces no error at all.
    const renamed: keyof Parameters<typeof streamText>[0] = 'experimental_refineToolInputRENAMED'
    expect(renamed).toBeTruthy()
  })

  it('hands the desk’s gate the call the model made, and the SDK the rewritten one', async () => {
    // Two layers, and they do different jobs. The hook rewrites what the SDK
    // carries — which is what the model is shown on its next turn — and the
    // ToolGate rewrites what leaves the page. `callTool` must receive the call
    // **as the model made it**, or the gate has nothing to report and the
    // guardrail line in the tab disappears.
    const asked: Record<string, unknown>[] = []
    const { call, seen } = scriptedCall([
      turn({ tool: { name: 'experimental_evaluate', args: { pack: {}, facts: {} } } }),
      turn({ text: PROPOSAL_TEXT })
    ])
    await drain(
      vercel.start(
        session(call, {}, async (name, args) => {
          asked.push({ name, ...args })
          return { content: [{ type: 'text', text: '{"status":"ok"}' }] }
        })
      )
    )
    // What the gate was handed: no rehearsal member, exactly as the model wrote
    // it. The gate is what puts one there, and what says so.
    expect(asked).toEqual([{ name: 'experimental_evaluate', pack: {}, facts: {} }])
    expect(Object.hasOwn(asked[0]!, 'rehearsal')).toBe(false)
    // And what the SDK carried into the next request: the rewritten input.
    const second = seen[1]!.body.messages as { role: string; tool_calls?: unknown[] }[]
    const echoed = second.find((message) => Array.isArray(message.tool_calls))!
    const carried = (echoed.tool_calls as { function: { arguments: string } }[])[0]!
    expect(JSON.parse(carried.function.arguments).rehearsal).toBe(true)
  })
})

describe('the tools this engine offers, and the schemas it writes', () => {
  it('offers exactly what the session handed it, with the runtime’s own schema', async () => {
    // K2, measured on the wire: the served schema arrives as served, member for
    // member, rather than re-typed through a schema library into this adapter's
    // reading of it.
    const served = {
      type: 'object',
      properties: { pack: { type: 'object' }, facts: { type: 'object' } },
      required: ['pack'],
      additionalProperties: false
    }
    const { call, seen } = scriptedCall([turn({ text: PROPOSAL_TEXT })])
    await drain(
      vercel.start(
        session(call, { tools: [{ name: 'validate', description: 'd', inputSchema: served }] })
      )
    )
    const tools = seen[0]!.body.tools as { function: { name: string; parameters: unknown } }[]
    expect(tools.map((tool) => tool.function.name)).toEqual(['validate'])
    expect(tools[0]!.function.parameters).toEqual(served)
  })

  it('refuses a tool the runtime served without a schema, rather than inventing one', async () => {
    // The desk never writes a contract the runtime does not enforce. A
    // permissive `{"type":"object"}` written here would tell the model that
    // anything is acceptable for a tool whose real contract this desk does not
    // know — so the session ends instead, naming the tool.
    const { call, seen } = scriptedCall([turn({ text: PROPOSAL_TEXT })])
    const events = await drain(
      vercel.start(session(call, { tools: [{ name: 'a_new_tool', description: 'd' }] }))
    )
    expect(events.map((event) => event.type)).toEqual(['error', 'end'])
    expect((events[0] as { message: string }).message).toContain('a_new_tool')
    expect((events[0] as { message: string }).message).toContain('without an input schema')
    // And nothing was asked of the model at all: the refusal is before the loop.
    expect(seen).toHaveLength(0)
  })

  it('names one tool in its whole source: the one the ADR names', () => {
    // The model is shown the contract the runtime enforces or it is shown
    // nothing, so the adapter has nothing to say about any particular tool. The
    // one exception is the rehearsal tool, and it is there because the SDK's
    // refinement hook is keyed by tool name.
    const source = readFileSync(join(import.meta.dirname, 'loop.ts'), 'utf8')
    for (const tool of ASSISTANT_TOOLS) {
      if (tool === 'experimental_evaluate') continue
      expect(source, `the adapter names the tool ${tool}`).not.toContain(`'${tool}'`)
    }
  })
})

describe('exactly one end, on every path', () => {
  it('delivers the terminal event through the channel, in order, once', async () => {
    // Not from a `finally`: `end` is pushed and delivered like every other
    // event, so a direct consumer — `runAssistantSession`, and anything else
    // driving the contract — actually receives it.
    const { call } = scriptedCall([
      turn({ tool: { name: 'validate', args: { document: {} } } }),
      turn({ text: PROPOSAL_TEXT })
    ])
    const events = await drain(vercel.start(session(call)))
    expect(events.map((event) => event.type)).toEqual([
      'tool_call',
      'tool_result',
      'proposal',
      'end'
    ])
    expect(events.filter((event) => event.type === 'end')).toHaveLength(1)
  })

  it('reports a refusal once and ends', async () => {
    const call: ModelCall = async () =>
      new Response(JSON.stringify({ error: 'no key stored', code: 'assistant-no-key' }), {
        status: 409,
        headers: { 'content-type': 'application/json' }
      })
    const events = await drain(vercel.start(session(call)))
    const errors = events.filter((event) => event.type === 'error')
    expect(errors).toHaveLength(1)
    expect((errors[0] as { message: string }).message).toContain('409')
    expect((errors[0] as { message: string }).message).toContain('no key stored')
    // No address in it, not even the placeholder the SDK's own error carries.
    expect((errors[0] as { message: string }).message).not.toContain('relay.invalid')
    expect(events.filter((event) => event.type === 'end')).toHaveLength(1)
    expect(events[events.length - 1]!.type).toBe('end')
  })

  it('makes one request per turn: the SDK’s own retries are off', async () => {
    // The SDK retries twice by default, with a backoff, and its retryable set
    // includes 409 — the status the desk's own relay answers with when no key
    // is stored. Three requests and six seconds for a refusal a person has to
    // go and fix.
    let requests = 0
    const call: ModelCall = async () => {
      requests += 1
      return new Response('{"error":"no key stored"}', {
        status: 409,
        headers: { 'content-type': 'application/json' }
      })
    }
    await drain(vercel.start(session(call)))
    expect(requests).toBe(1)
  })

  it('says nothing at all, and ends, when the session is aborted', async () => {
    // The desk's own capability rejects an aborted call with a fresh
    // `AbortError` carrying a fixed sentence and no address; this stands in for
    // it, because a stub that ignored the signal would be testing a capability
    // the page does not have.
    const controller = new AbortController()
    const call: ModelCall = (_suffix, request) => {
      controller.abort()
      const stopped = () => new DOMException('the model request was aborted', 'AbortError')
      if (request.signal?.aborted === true) return Promise.reject(stopped())
      return new Promise<Response>((_resolve, reject) => {
        request.signal?.addEventListener('abort', () => reject(stopped()))
      })
    }
    const events = await drain(vercel.start(session(call, { signal: controller.signal })))
    // Nothing at all: a cancelled run is a session somebody ended rather than
    // a run that finished, and the terminal event belongs to the one that
    // finished. The stream ends; the run hook writes the page's own `end`.
    expect(events).toEqual([])
  })

  it('ends once where the final message carries no fenced block', async () => {
    const { call } = scriptedCall([turn({ text: 'I could not write one.' })])
    const events = await drain(vercel.start(session(call)))
    expect(events.map((event) => event.type)).toEqual(['error', 'end'])
    expect((events[0] as { message: string }).message).toContain('exactly one fenced JSON block')
  })
})

describe('a consumer that stops in the middle of a run', () => {
  it('closes the generator on the first return, and produces nothing after it', async () => {
    // Yielding the terminal `end` from a `finally` made the first `return()`
    // resolve `{ value: end, done: false }` with the generator still suspended,
    // and `for await`'s own closing discards that value — so a direct consumer
    // was never handed the terminal event at all, and the page only worked
    // because the run hook writes one of its own. A consumer that asks to stop
    // is owed no terminal event; what it is owed is a closed iterator.
    const { call } = scriptedCall([
      turn({ tool: { name: 'validate', args: { document: {} } } }),
      turn({ text: PROPOSAL_TEXT })
    ])
    const iterator = vercel.start(session(call))[Symbol.asyncIterator]()
    expect((await iterator.next()).value).toMatchObject({ type: 'tool_call' })
    expect(await iterator.return!(undefined)).toEqual({ value: undefined, done: true })
    expect(await iterator.next()).toEqual({ value: undefined, done: true })
  })

  it('closes on a return before the first next, having run nothing', async () => {
    const { call, seen } = scriptedCall([turn({ text: PROPOSAL_TEXT })])
    const iterator = vercel.start(session(call))[Symbol.asyncIterator]()
    expect(await within(2000, iterator.return!(undefined) as Promise<unknown>)).toBe('settled')
    expect(await iterator.next()).toEqual({ value: undefined, done: true })
    expect(seen, 'the model was never asked anything').toHaveLength(0)
  })

  it('stops a run whose model request only ends when it is aborted', async () => {
    // The sequence the outer shape exists for. An async generator serves
    // `next()` and `return()` from one queue, so the `return()` carrying the
    // abort was queued behind the very `next()` the abort would have released,
    // and both hung for ever. Measured on this engine before the iterator.
    let sawAbort = false
    let arrived = () => {}
    const entered = new Promise<void>((resolve) => {
      arrived = resolve
    })
    const call: ModelCall = (_suffix, request) =>
      new Promise<Response>((_resolve, reject) => {
        arrived()
        const stopped = () => {
          sawAbort = true
          reject(new DOMException('the model request was aborted', 'AbortError'))
        }
        if (request.signal?.aborted === true) stopped()
        else request.signal?.addEventListener('abort', stopped)
      })

    const settledInOrder: string[] = []
    const iterator = vercel.start(session(call))[Symbol.asyncIterator]()
    const pending = iterator.next().then((step) => {
      settledInOrder.push('next')
      return step
    })
    await entered
    const returned = iterator.return!(undefined).then((step) => {
      settledInOrder.push('return')
      return step
    })

    expect(await within(2000, pending), 'the pending next').toBe('settled')
    expect(await within(2000, returned), 'the return').toBe('settled')
    expect(await pending).toEqual({ value: undefined, done: true })
    expect(await returned).toEqual({ value: undefined, done: true })
    expect(settledInOrder).toEqual(['next', 'return'])
    expect(sawAbort, 'the request in flight observed the abort').toBe(true)
    // And it is closed: nothing more is produced, and nothing underneath is
    // asked again.
    expect(await iterator.next()).toEqual({ value: undefined, done: true })
    expect(await iterator.return!(undefined)).toEqual({ value: undefined, done: true })
  })

  /**
   * A run stopped while it is waiting on the **runtime**, three ways.
   *
   * The seam every review round found an interleaving in: something outside the
   * engine is being awaited, and the cleanup that would end it is queued behind
   * that await. Every one of these is bounded, so a regression fails rather
   * than hanging the suite.
   */
  describe.each([
    ['return()', (i: AsyncIterator<AssistantEvent>) => i.return!(undefined)],
    ['throw()', (i: AsyncIterator<AssistantEvent>) => i.throw!(new Error('gave up')).catch(() => undefined)]
  ] as const)('while a tool call is pending, %s', (_name, stop) => {
    it('settles the pending next first, then itself, and asks the runtime nothing more', async () => {
      let arrived = () => {}
      const entered = new Promise<void>((resolve) => {
        arrived = resolve
      })
      let calls = 0
      const { call } = scriptedCall([
        turn({ tool: { name: 'validate', args: { document: {} } } }),
        turn({ text: PROPOSAL_TEXT })
      ])
      const iterator = vercel.start(
        session(call, {}, async () => {
          calls += 1
          arrived()
          // The capability that never settles: an MCP frame on a socket nobody
          // is answering. Nothing about it honours a signal.
          return new Promise<McpToolResult>(() => {})
        })
      )[Symbol.asyncIterator]()

      expect((await iterator.next()).value).toMatchObject({ type: 'tool_call' })
      const pending = iterator.next()
      await entered
      const settledInOrder: string[] = []
      const held = pending.then(() => settledInOrder.push('next'))
      const ended = Promise.resolve(stop(iterator)).then(() => settledInOrder.push('stop'))

      expect(await within(2000, held), 'the pending next').toBe('settled')
      expect(await within(2000, ended), 'the stop').toBe('settled')
      expect(await pending).toEqual({ value: undefined, done: true })
      expect(settledInOrder).toEqual(['next', 'stop'])
      expect(calls, 'the runtime was asked exactly once, before the stop').toBe(1)
    })
  })

  it('settles a pending next when the session itself is aborted mid tool call', async () => {
    // The session's own signal reaches the same cancellation the consumer's
    // `return()` does — it used to abort the SDK and leave the channel alone,
    // so a `next()` waiting on a tool call that never settled waited for ever.
    const controller = new AbortController()
    let arrived = () => {}
    const entered = new Promise<void>((resolve) => {
      arrived = resolve
    })
    const { call } = scriptedCall([
      turn({ tool: { name: 'validate', args: { document: {} } } }),
      turn({ text: PROPOSAL_TEXT })
    ])
    const iterator = vercel.start(
      session(call, { signal: controller.signal }, async () => {
        arrived()
        return new Promise<McpToolResult>(() => {})
      })
    )[Symbol.asyncIterator]()
    expect((await iterator.next()).value).toMatchObject({ type: 'tool_call' })
    const pending = iterator.next()
    await entered
    controller.abort()
    expect(await within(2000, pending), 'the pending next').toBe('settled')
    expect(await pending).toEqual({ value: undefined, done: true })
  })

  it('lets no tools/call reach the runtime after the consumer has closed the run', async () => {
    // Two queued reads and a model answer that arrives late carrying a tool
    // call: the guard is read **before** the call is dispatched, so a closed
    // run has nowhere to send it.
    let asked: string[] = []
    let answer = () => {}
    const held = new Promise<void>((resolve) => {
      answer = resolve
    })
    let turnNumber = 0
    const call: ModelCall = async () => {
      turnNumber += 1
      if (turnNumber === 1) await held
      return new Response(
        turnNumber === 1
          ? turn({ tool: { name: 'validate', args: { document: {} } } })
          : turn({ text: PROPOSAL_TEXT }),
        { status: 200, headers: { 'content-type': 'text/event-stream' } }
      )
    }
    const iterator = vercel.start(
      session(call, {}, async (name) => {
        asked.push(name)
        return { content: [{ type: 'text', text: '{}' }] }
      })
    )[Symbol.asyncIterator]()
    const first = iterator.next()
    const second = iterator.next()
    void first.catch(() => undefined)
    void second.catch(() => undefined)
    expect(await within(2000, iterator.return!(undefined) as Promise<unknown>)).toBe('settled')
    // Now the model answers, with a tool call, into a run that is closed.
    answer()
    await new Promise((resolve) => setTimeout(resolve, 100))
    expect(asked, 'a tools/call reached the runtime after the run closed').toEqual([])
    expect(await first).toEqual({ value: undefined, done: true })
    expect(await second).toEqual({ value: undefined, done: true })
  })

  it('returns promptly, and asks the runtime nothing more', async () => {
    // The whole path: the model asks for a tool, the adapter's `execute` waits
    // for the `tool_call` event to reach the consumer, and the consumer stops
    // there. The deadlock itself is proved on the channel above — this is the
    // shape it happens in, and the two things a viewer would notice: `return()`
    // settles, and a session nobody is watching asks the runtime nothing more.
    let asked = 0
    const { call, seen } = scriptedCall([
      turn({ tool: { name: 'validate', args: { document: {} } } }),
      turn({ text: PROPOSAL_TEXT })
    ])
    const iterator = vercel.start(
      session(call, {}, async () => {
        asked += 1
        return { content: [{ type: 'text', text: '{"status":"ok"}' }] }
      })
    )[Symbol.asyncIterator]()
    const first = await iterator.next()
    expect(first.value).toMatchObject({ type: 'tool_call', name: 'validate' })
    expect(asked, 'the tool has not been called yet').toBe(0)
    expect(await within(2000, iterator.return!(undefined))).toBe('settled')
    // Whatever the framework's pipeline does as it unwinds, it does not reach
    // the runtime, and it does not take another turn.
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(asked, 'the runtime was asked after the consumer stopped').toBe(0)
    expect(seen).toHaveLength(1)
  })
})

describe('the unhandled rejection the SDK’s refusal path leaks', () => {
  /**
   * Every rejection Node saw with no handler while `run` was in flight.
   *
   * `process`, not `window`: jsdom does not turn a Node-level unhandled
   * rejection into an `unhandledrejection` event, so a test that dispatched one
   * itself would be measuring its own dispatch. This is the real thing.
   */
  async function unhandled(run: () => Promise<void>): Promise<string[]> {
    const seen: string[] = []
    const watch = (reason: unknown) =>
      seen.push(String((reason as { name?: string } | undefined)?.name ?? reason))
    const others = process.listeners('unhandledRejection')
    process.removeAllListeners('unhandledRejection')
    process.on('unhandledRejection', watch)
    try {
      await run()
      // Node reports an unclaimed rejection a turn or two later, never in the
      // same tick, so the window has to outlast the run.
      for (let tick = 0; tick < 6; tick += 1) await new Promise((r) => setTimeout(r, 10))
    } finally {
      process.off('unhandledRejection', watch)
      for (const listener of others) process.on('unhandledRejection', listener as never)
    }
    return seen
  }

  /** The desk's own refusal: the relay answers 409 when no key is stored. */
  const refuses: ModelCall = async () =>
    new Response(JSON.stringify({ error: 'no key stored', code: 'assistant-no-key' }), {
      status: 409,
      headers: { 'content-type': 'application/json' }
    })

  it('is not leaked at all: the result’s promises are claimed as it is made', async () => {
    // Measured, not asserted: reading `result.text` and leaving it unclaimed
    // produces exactly one `AI_NoOutputGeneratedError` on this path. Claiming
    // every promise-valued member of the result produces none, and nothing is
    // suppressed anywhere to achieve it.
    const events: AssistantEvent[] = []
    const leaked = await unhandled(async () => {
      for await (const event of vercel.start(session(refuses))) events.push(event)
    })
    expect(leaked).toEqual([])
    expect(events.map((event) => event.type)).toEqual(['error', 'end'])
  })

  it('installs no page listener, so an unrelated rejection stays observable', async () => {
    // The guard this replaces suppressed **every** page rejection whose reason
    // merely carried that error name — an unrelated operation elsewhere in the
    // page, during a run, was hidden from the browser's own diagnostics.
    const iterator = vercel.start(session(scriptedCall([turn({ text: PROPOSAL_TEXT })]).call))[
      Symbol.asyncIterator
    ]()
    await iterator.next()
    const event = new Event('unhandledrejection', { cancelable: true })
    ;(event as { reason?: unknown }).reason = { name: 'AI_NoOutputGeneratedError' }
    globalThis.dispatchEvent(event)
    expect(event.defaultPrevented, 'something on this page suppressed it').toBe(false)
    await iterator.return!(undefined)
  })

  it('claims the promise-valued members the SDK exposes, and steps over the rest', () => {
    // The enumeration itself, on a stand-in: own properties and prototype
    // getters alike, a member that throws on being read stepped over, and
    // nothing claimed that is not a promise.
    let read = 0
    const stand = Object.create({
      get inherited() {
        read += 1
        return Promise.reject(new Error('claimed'))
      },
      get throws(): unknown {
        throw new Error('a member that will not be read')
      }
    }) as Record<string, unknown>
    stand.own = Promise.reject(new Error('claimed'))
    stand.plain = 'not a promise'
    expect(claimPromises(stand)).toBe(2)
    expect(read).toBe(1)
  })
})

describe('what the model said about its own reasoning', () => {
  it('reaches the contract as reasoning events, whatever the tier is', async () => {
    // The tier is what this desk *asks* for, and this chunk asks for nothing.
    // A model that always thinks reasons anyway, and the SDK surfaces that as
    // its own `reasoning-start` / `-delta` / `-end` part types.
    const { call } = scriptedCall([
      turn({ reasoning: ['I check the schema ', 'before I propose.'], text: PROPOSAL_TEXT })
    ])
    const events = await drain(vercel.start(session(call)))
    expect(events.map((event) => event.type)).toEqual([
      'reasoning',
      'reasoning',
      'reasoning',
      'proposal',
      'end'
    ])
    expect(events.slice(0, 3)).toEqual([
      { type: 'reasoning', text: 'I check the schema ', done: false },
      { type: 'reasoning', text: 'before I propose.', done: false },
      // The whole passage on `done`, so a reader has it rather than the pieces.
      { type: 'reasoning', text: 'I check the schema before I propose.', done: true }
    ])
    // **And no capability is claimed from one turn.** One unsolicited passage
    // is a turn, not a model that always thinks.
    expect(events.some((event) => event.type === 'thinking_unavailable')).toBe(false)
  })

  it('reports a model that always thinks after two answers of it', async () => {
    // A turn that only called a tool neither counts nor resets, in either
    // tier, so the two that count are the two that answered — with a tool-only
    // turn between them, which must not wipe the count.
    const { call } = scriptedCall([
      turn({
        reasoning: ['I look first.'],
        text: 'Let me look.',
        tool: { name: 'validate', args: { document: '{}' } }
      }),
      turn({ tool: { name: 'validate', args: { document: '{}' } } }),
      turn({ reasoning: ['And then I propose.'], text: PROPOSAL_TEXT })
    ])
    const events = await drain(vercel.start(session(call)))
    const notices = events.filter(
      (event): event is Extract<AssistantEvent, { type: 'thinking_unavailable' }> =>
        event.type === 'thinking_unavailable'
    )
    expect(notices).toHaveLength(1)
    expect(notices[0]!.detail).toContain('always thinks')
  })

  it('says nothing where the endpoint reasoned about nothing', async () => {
    const { call } = scriptedCall([turn({ text: PROPOSAL_TEXT })])
    const events = await drain(vercel.start(session(call)))
    expect(events.some((event) => event.type === 'reasoning')).toBe(false)
  })
})

describe('the thinking tier, through the SDK’s own call settings', () => {
  /**
   * **The translation, held to the desk's table.**
   *
   * `sdkThinking` is the one place this adapter turns the desk's wire members
   * into the SDK's vocabulary, and the assertion below is that what comes out
   * the other end of the SDK is what the table asked for — measured on the
   * body, not on the option object.
   */
  it.each([
    ['openai-compatible', 'on'],
    ['openai-compatible', 'ultra'],
    ['anthropic', 'on'],
    ['anthropic', 'ultra']
  ] as const)('puts the desk’s own members on a %s request at %s', async (family, tier) => {
    const answer =
      family === 'anthropic' ? anthropicTurn({ text: PROPOSAL_TEXT }) : turn({ text: PROPOSAL_TEXT })
    const { call, seen } = scriptedCall([answer])
    await drain(
      vercel.start(
        session(call, {
          model: { family, model: 'a-model', call },
          thinking: normalize(tier, family)
        })
      )
    )
    const table = normalize(tier, family).wire!.members
    for (const [member, value] of Object.entries(table)) {
      expect(seen[0]!.body[member], `${family} ${tier} ${member}`).toEqual(value)
    }
    // The fallback dialect's pair, through the SDK's own call settings.
    const fallback = wireFor(tier, 'anthropic-enabled')!.members as { max_tokens: number }
    if (family === 'anthropic') {
      expect(sdkThinking(family, wireFor(tier, 'anthropic-enabled')!.members)).toMatchObject({
        maxOutputTokens: fallback.max_tokens
      })
    }
  })

  it('sends no tier member at all where the tier is off', async () => {
    const { call, seen } = scriptedCall([turn({ text: PROPOSAL_TEXT })])
    await drain(vercel.start(session(call)))
    expect(Object.keys(seen[0]!.body)).not.toContain('reasoning_effort')
    expect(Object.keys(seen[0]!.body)).not.toContain('thinking')
  })

  it('translates each of the desk’s three dialects and nothing else', () => {
    expect(sdkThinking('openai-compatible', null)).toEqual({})
    expect(sdkThinking('openai-compatible', { reasoning_effort: 'xhigh' })).toEqual({
      providerOptions: { 'desk-endpoint': { reasoningEffort: 'xhigh' } }
    })
    expect(
      sdkThinking('anthropic', { thinking: { type: 'adaptive' }, output_config: { effort: 'high' } })
    ).toEqual({ providerOptions: { anthropic: { thinking: { type: 'adaptive' }, effort: 'high' } } })
    expect(
      sdkThinking('anthropic', {
        thinking: { type: 'enabled', budget_tokens: 16000 },
        max_tokens: 20096
      })
    ).toEqual({
      providerOptions: { anthropic: { thinking: { type: 'enabled', budgetTokens: 16000 } } },
      // The SDK's spelling of the number the desk's table chose beside the
      // budget: Anthropic spends the budget out of the request's maximum.
      maxOutputTokens: 20096
    })
  })

  it('degrades once on a 400 that names the member, and completes', async () => {
    const seen: Record<string, unknown>[] = []
    const call: ModelCall = async (_suffix, request) => {
      // The critic is a second conversation and is not what this measures.
      if (request.body.includes(REFUTATION_MARKER)) {
        return new Response(turn({ text: 'REFUTATION: nothing to report.' }), {
          status: 200,
          headers: { 'content-type': 'text/event-stream' }
        })
      }
      seen.push(JSON.parse(request.body) as Record<string, unknown>)
      if (seen.length === 1) {
        return new Response(
          JSON.stringify({ error: { message: 'Unsupported parameter: reasoning_effort' } }),
          { status: 400, headers: { 'content-type': 'application/json' } }
        )
      }
      return new Response(turn({ reasoning: ['x'], text: PROPOSAL_TEXT }), {
        status: 200,
        headers: { 'content-type': 'text/event-stream' }
      })
    }
    const events = await drain(
      vercel.start(session(call, { thinking: normalize('on', 'openai-compatible') }))
    )
    expect(seen).toHaveLength(2)
    expect(seen[0]!.reasoning_effort).toBe('high')
    // The retry is the identical request without the member.
    expect(Object.keys(seen[1]!)).not.toContain('reasoning_effort')
    const notices = events.filter((event) => event.type === 'thinking_unavailable')
    expect(notices).toHaveLength(1)
    expect(events.some((event) => event.type === 'proposal')).toBe(true)
    expect(events[events.length - 1]!.type).toBe('end')
  })

  it('falls back once to the other Anthropic spelling, and says nothing about it', async () => {
    const seen: Record<string, unknown>[] = []
    const call: ModelCall = async (_suffix, request) => {
      if (request.body.includes(REFUTATION_MARKER)) {
        return new Response(anthropicTurn({ text: 'REFUTATION: nothing to report.' }), {
          status: 200,
          headers: { 'content-type': 'text/event-stream' }
        })
      }
      seen.push(JSON.parse(request.body) as Record<string, unknown>)
      if (seen.length === 1) {
        return new Response(
          JSON.stringify({ error: { message: 'Adaptive thinking is not supported by this model' } }),
          { status: 400, headers: { 'content-type': 'application/json' } }
        )
      }
      return new Response(anthropicTurn({ reasoning: 'I read it.', text: PROPOSAL_TEXT }), {
        status: 200,
        headers: { 'content-type': 'text/event-stream' }
      })
    }
    const events = await drain(
      vercel.start(
        session(call, {
          model: { family: 'anthropic', model: 'a-model', call },
          thinking: normalize('on', 'anthropic')
        })
      )
    )
    expect(seen).toHaveLength(2)
    expect(seen[0]!.thinking).toEqual({ type: 'adaptive' })
    expect(seen[1]!.thinking).toEqual({ type: 'enabled', budget_tokens: 8000 })
    expect(events.some((event) => event.type === 'thinking_unavailable')).toBe(false)
  })

  it('reports unavailable after two answers with no reasoning, and not after one', async () => {
    // The critic's own turn is this session's second answer, so the second
    // observation lands there on a session that proposes at once.
    const { call, seen } = scriptedCall([turn({ text: PROPOSAL_TEXT })])
    const events = await drain(
      vercel.start(session(call, { thinking: normalize('on', 'openai-compatible') }))
    )
    const notices = events.filter((event) => event.type === 'thinking_unavailable')
    expect(notices).toHaveLength(1)
    expect((notices[0] as { detail: string }).detail).toContain('no reasoning block')
    expect(events.some((event) => event.type === 'proposal')).toBe(true)
    // The first request still carried the tier: nothing was concluded from one
    // turn.
    expect(seen[0]!.body.reasoning_effort).toBe('high')
  })

  it('does not conclude anything from a turn that only called a tool', async () => {
    const { call } = scriptedCall([
      turn({ tool: { name: 'validate', args: { document: '{}' } } }),
      turn({ reasoning: ['I did think.'], text: PROPOSAL_TEXT })
    ])
    const events = await drain(
      vercel.start(session(call, { thinking: normalize('on', 'openai-compatible') }))
    )
    expect(events.some((event) => event.type === 'thinking_unavailable')).toBe(false)
  })
})

describe('the split signature this SDK truncates (vercel/ai#19663)', () => {
  it('reassembles the fragments the desk saw, and refuses the fragment', () => {
    const ledger = signatureLedger()
    ledger.fragment('0', 'c2lnbmF0')
    ledger.fragment('0', 'dXJlLVQx')
    expect(ledger.wholes()).toEqual(['c2lnbmF0dXJlLVQx'])
    const body = JSON.stringify({
      messages: [
        {
          role: 'assistant',
          content: [
            { type: 'thinking', thinking: 'I read it.', signature: 'dXJlLVQx' },
            { type: 'text', text: 'hello' }
          ]
        }
      ]
    })
    const filtered = withoutTruncatedThinking(body, ledger)
    expect(filtered.truncated).toContain('19663')
    const sent = JSON.parse(filtered.body) as { messages: { content: { type: string }[] }[] }
    expect(sent.messages[0]!.content.map((block) => block.type)).toEqual(['text'])
  })

  it('rebuilds the request from the slot rather than filtering the composed one', () => {
    const ledger = signatureLedger()
    ledger.fragment('0', 'c2lnbmF0dXJlLVQx')
    const body = JSON.stringify({
      model: 'm',
      max_tokens: 12096,
      thinking: { type: 'enabled', budget_tokens: 8000 },
      output_config: { effort: 'high' },
      messages: [
        {
          role: 'assistant',
          content: [
            { type: 'thinking', thinking: 'I read it.', signature: 'dXJlLVQx' },
            { type: 'text', text: 'hello' }
          ]
        }
      ]
    })
    // The slot has degraded by the time this is read, so it asks for nothing.
    const rebuilt = withoutTruncatedThinking(body, ledger, () => null)
    const sent = JSON.parse(rebuilt.body) as Record<string, unknown>
    expect(rebuilt.truncated).toContain('19663')
    expect(Object.keys(sent)).not.toContain('thinking')
    expect(Object.keys(sent)).not.toContain('output_config')
    // `max_tokens` stays: the protocol requires one on every request, and one
    // larger than a degraded session needs is legal.
    expect(sent.max_tokens).toBe(12096)
    expect(sent.model).toBe('m')
  })

  it('leaves a whole signature exactly where it was', () => {
    const ledger = signatureLedger()
    ledger.fragment('0', 'c2lnbmF0dXJlLVQx')
    const body = JSON.stringify({
      messages: [
        { role: 'assistant', content: [{ type: 'thinking', thinking: 'x', signature: 'c2lnbmF0dXJlLVQx' }] }
      ]
    })
    const filtered = withoutTruncatedThinking(body, ledger)
    expect(filtered.truncated).toBe('')
    expect(filtered.body).toBe(body)
  })

  it('starts a new signature at each turn, because the block ids repeat', () => {
    // **The turn boundary, which is not decoration.** On the Anthropic wire a
    // reasoning part is keyed by its index in the message, so every turn starts
    // again at `0`. A ledger with no boundary concatenates one turn's signature
    // onto the next and then reports the next turn's *whole* signature as a
    // fragment of the pair — measured, on a session's third turn.
    const ledger = signatureLedger()
    ledger.fragment('0', 'c2lnLVQx')
    ledger.boundary()
    ledger.fragment('0', 'c2lnLVQy')
    expect(ledger.wholes()).toEqual(['c2lnLVQx', 'c2lnLVQy'])
  })

  it('lets a later block whose own signature is shorter survive', () => {
    // **Block identity, by position.** The ledger holds one signature per
    // signed block in the order they were sent, and the history carries the
    // same blocks in the same order. A global membership test threw the second
    // block away because its signature happened to be a prefix of the first's.
    const ledger = signatureLedger()
    ledger.fragment('0', 'abcdef')
    ledger.boundary()
    ledger.fragment('0', 'abc')
    const body = JSON.stringify({
      messages: [
        { role: 'assistant', content: [{ type: 'thinking', thinking: 'one', signature: 'abcdef' }] },
        { role: 'user', content: [{ type: 'text', text: 'and?' }] },
        { role: 'assistant', content: [{ type: 'thinking', thinking: 'two', signature: 'abc' }] }
      ]
    })
    const looked = withoutTruncatedThinking(body, ledger, () => null)
    expect(looked.truncated).toBe('')
    expect(looked.body).toBe(body)
  })

  it('still catches the block that did come back as a fragment', () => {
    const ledger = signatureLedger()
    ledger.fragment('0', 'abcdef')
    ledger.boundary()
    ledger.fragment('0', 'abc')
    // The FIRST block came back halved; the second is its own whole signature.
    const body = JSON.stringify({
      messages: [
        { role: 'assistant', content: [{ type: 'thinking', thinking: 'one', signature: 'abc' }] },
        { role: 'assistant', content: [{ type: 'thinking', thinking: 'two', signature: 'abc' }] }
      ]
    })
    const looked = withoutTruncatedThinking(body, ledger, () => null)
    expect(looked.truncated).toContain('19663')
    const sent = JSON.parse(looked.body) as { messages: { content: { type: string }[] }[] }
    expect(sent.messages[0]!.content).toEqual([])
    expect(sent.messages[1]!.content).toHaveLength(1)
  })

  it('does not double a signature the SDK repeated whole', () => {
    // The SDK re-emits the same value where the endpoint sent one event, and a
    // ledger that appended blindly would invent a truncation nobody caused.
    const ledger = signatureLedger()
    ledger.fragment('0', 'c2ln')
    ledger.fragment('0', 'c2ln')
    expect(ledger.wholes()).toEqual(['c2ln'])
  })

  /**
   * **The measurement, end to end — and it is written to fail if the SDK is
   * ever fixed in silence.**
   *
   * The fixture splits one signature across two `signature_delta` events, which
   * is what a re-chunking proxy does. If this SDK ever reassembles them, the
   * desk detects no truncation, no notice is emitted, and this case goes red —
   * which is the point: the guard would then be dead code, and the desk should
   * find out from its own suite rather than from an endpoint.
   */
  it('detects the truncation on a real session and degrades once', async () => {
    const seen: Record<string, unknown>[] = []
    const call: ModelCall = async (_suffix, request) => {
      if (request.body.includes(REFUTATION_MARKER)) {
        return new Response(anthropicTurn({ text: 'REFUTATION: nothing to report.' }), {
          status: 200,
          headers: { 'content-type': 'text/event-stream' }
        })
      }
      seen.push(JSON.parse(request.body) as Record<string, unknown>)
      if (seen.length === 1) {
        return new Response(
          anthropicTurn({
            reasoning: 'I will check it.',
            signature: 'c2lnbmF0dXJlLVQx',
            splitSignature: true,
            tool: { name: 'validate', args: { document: '{}' } }
          }),
          { status: 200, headers: { 'content-type': 'text/event-stream' } }
        )
      }
      return new Response(anthropicTurn({ reasoning: 'Done.', text: PROPOSAL_TEXT }), {
        status: 200,
        headers: { 'content-type': 'text/event-stream' }
      })
    }
    const events = await drain(
      vercel.start(
        session(call, {
          model: { family: 'anthropic', model: 'a-model', call },
          thinking: normalize('on', 'anthropic')
        })
      )
    )
    const notices = events.filter(
      (event): event is Extract<AssistantEvent, { type: 'thinking_unavailable' }> =>
        event.type === 'thinking_unavailable'
    )
    expect(
      notices.map((notice) => notice.detail),
      'the SDK reassembled the split signature — the guard is now dead code'
    ).toHaveLength(1)
    expect(notices[0]!.detail).toContain('19663')
    // And no malformed block left the page: the second request carries no
    // thinking block at all rather than one with half a signature.
    const carried = (seen[1]!.messages as { role: string; content: unknown }[])
      .flatMap((message) => (Array.isArray(message.content) ? message.content : []))
      .filter((block) => (block as { type?: string }).type === 'thinking')
    expect(carried).toEqual([])
    // **And it does not ask for thinking either.** A request that both asks for
    // thinking and has dropped a block the endpoint signed is a continuation a
    // real endpoint may refuse: the body was composed before the slot degraded,
    // so it is rebuilt from what the slot says afterwards rather than filtered.
    expect(Object.keys(seen[1]!)).not.toContain('thinking')
    expect(Object.keys(seen[1]!)).not.toContain('output_config')
    // The session still completes: a degrade is not a refusal.
    expect(events[events.length - 1]!.type).toBe('end')
    expect(events.some((event) => event.type === 'proposal')).toBe(true)
  })
})

describe('what the author is told when the endpoint refuses', () => {
  /**
   * **The half of the SDK's refusal path this desk owns.**
   *
   * One `AI_NoOutputGeneratedError` still reaches a browser's console from
   * inside the SDK's own transform flush, and it is not reachable from the
   * result's object graph at any depth — measured on the live drive, three
   * ways (see `claimPromises`). What this holds is that the console is not
   * where a person finds out: the run says what happened, on its own stream,
   * with the status and the endpoint's own sentence in it.
   */
  it('reports the status and the endpoint’s own sentence, and ends once', async () => {
    const call: ModelCall = async () =>
      new Response(JSON.stringify({ error: { message: 'this endpoint refuses everything' } }), {
        status: 400,
        headers: { 'content-type': 'application/json' }
      })
    const events = await drain(vercel.start(session(call)))
    expect(events.map((event) => event.type)).toEqual(['error', 'end'])
    const said = (events[0] as { message: string }).message
    expect(said).toContain('400')
    expect(said).toContain('this endpoint refuses everything')
    // …and no address in it: the SDK's error carries the placeholder origin,
    // which says nothing useful and reads as a real host.
    expect(said).not.toContain('relay.invalid')
    expect(said).not.toContain('http')
  })
})

describe('the refutation pass, on this SDK’s second streamText', () => {
  const VALID = JSON.stringify({ status: 'valid', diagnostics: [] })
  const INVALID = JSON.stringify({
    status: 'invalid',
    diagnostics: [{ code: 'JPS-SEMANTIC-UNRESOLVED-OUTCOME' }]
  })

  /** A session whose main loop proposes at once and whose critic runs a script. */
  function criticised(options: {
    tier: 'off' | 'on' | 'ultra'
    criticCalls?: { name: string; args: Record<string, unknown> }[]
    criticSays: string
    answer: string
  }): { session: AssistantSession; asked: { name: string; args: unknown }[]; critic: string[] } {
    const asked: { name: string; args: unknown }[] = []
    const critic: string[] = []
    let criticTurn = 0
    const call: ModelCall = async (_suffix, request) => {
      const stream = (text: string) =>
        new Response(text, { status: 200, headers: { 'content-type': 'text/event-stream' } })
      if (request.body.includes(REFUTATION_MARKER)) {
        critic.push(request.body)
        criticTurn += 1
        const next = (options.criticCalls ?? [])[criticTurn - 1]
        return stream(
          next === undefined ? turn({ text: options.criticSays }) : turn({ tool: next })
        )
      }
      return stream(turn({ text: PROPOSAL_TEXT }))
    }
    return {
      session: session(
        call,
        { thinking: normalize(options.tier, 'openai-compatible') },
        async (name, args) => {
          asked.push({ name, args })
          return { content: [{ type: 'text', text: options.answer }] }
        }
      ),
      asked,
      critic
    }
  }

  it('does not run at all where the tier is off', async () => {
    const one = criticised({ tier: 'off', criticSays: 'x', answer: VALID })
    const events = await drain(vercel.start(one.session))
    expect(one.critic).toEqual([])
    expect(events.some((event) => event.type === 'critique')).toBe(false)
  })

  it('runs after the proposal exists and before it is shown', async () => {
    const one = criticised({
      tier: 'on',
      criticCalls: [{ name: 'validate', args: { document: '{}' } }],
      criticSays: 'REFUTATION: none found.',
      answer: VALID
    })
    const events = await drain(vercel.start(one.session))
    const kinds = events.map((event) => event.type)
    expect(kinds).toContain('critique')
    expect(kinds.indexOf('critique')).toBeLessThan(kinds.indexOf('proposal'))
    // The critic's own call travelled the session's `callTool` — the same
    // capability, through the same gate, as the main loop's.
    expect(one.asked).toEqual([{ name: 'validate', args: { document: '{}' } }])
    expect(kinds.filter((kind) => kind === 'tool_call')).toHaveLength(1)
  })

  it('takes the verdict from the runtime and not from the critic’s prose', async () => {
    const one = criticised({
      tier: 'on',
      criticCalls: [{ name: 'validate', args: { document: '{}' } }],
      criticSays: 'REFUTATION: this pack is broken and must not be used.',
      answer: VALID
    })
    const events = await drain(vercel.start(one.session))
    const critique = events.find(
      (event): event is Extract<AssistantEvent, { type: 'critique' }> => event.type === 'critique'
    )!
    expect(critique.refuted).toBe(false)
    expect(critique.checks).toEqual([{ tool: 'validate', status: 'valid' }])
  })

  it('reports refuted where the runtime refused, whatever the critic said', async () => {
    const one = criticised({
      tier: 'on',
      criticCalls: [{ name: 'validate', args: { document: '{}' } }],
      criticSays: 'REFUTATION: none found. Everything checks out.',
      answer: INVALID
    })
    const events = await drain(vercel.start(one.session))
    const critique = events.find(
      (event): event is Extract<AssistantEvent, { type: 'critique' }> => event.type === 'critique'
    )!
    expect(critique.refuted).toBe(true)
    const proposal = events.find(
      (event): event is Extract<AssistantEvent, { type: 'proposal' }> => event.type === 'proposal'
    )!
    expect(proposal.critique).toEqual({ refuted: true })
    expect(proposal.document).toBeDefined()
  })

  it('says the critic ran no check, and puts no line on the proposal', async () => {
    const one = criticised({ tier: 'on', criticSays: 'I had a look.', answer: VALID })
    const events = await drain(vercel.start(one.session))
    const critique = events.find(
      (event): event is Extract<AssistantEvent, { type: 'critique' }> => event.type === 'critique'
    )!
    expect(critique.checks).toEqual([])
    expect(critique.text).toContain('no runtime check')
    const proposal = events.find(
      (event): event is Extract<AssistantEvent, { type: 'proposal' }> => event.type === 'proposal'
    )!
    expect(proposal.critique).toBeUndefined()
  })

  it('rehearses the critic’s evaluate through the same hook as the main loop’s', async () => {
    // The critic gets the **same** tool set and the same refinement hook, so an
    // evaluate it asks for without a rehearsal member is rewritten before it is
    // executed — and the desk's gate below rewrites it again on the wire.
    const one = criticised({
      tier: 'on',
      criticCalls: [{ name: 'experimental_evaluate', args: { pack: '{}', facts: '{}' } }],
      criticSays: 'REFUTATION: none found.',
      answer: JSON.stringify({ status: 'evaluated', rehearsal: true })
    })
    const events = await drain(vercel.start(one.session))
    expect(one.asked).toEqual([
      { name: 'experimental_evaluate', args: { pack: '{}', facts: '{}' } }
    ])
    const critique = events.find(
      (event): event is Extract<AssistantEvent, { type: 'critique' }> => event.type === 'critique'
    )!
    expect(critique.checks).toEqual([{ tool: 'experimental_evaluate', status: 'evaluated' }])
    // **`evaluated` is not `valid`, and it does not refute.** One word over
    // both tools would report every session ever run as refuted.
    expect(critique.refuted).toBe(false)
  })

  it('hands the critic the runtime’s testing prompt and the proposed document', async () => {
    const one = criticised({ tier: 'on', criticSays: 'done', answer: VALID })
    await drain(vercel.start(one.session))
    expect(one.critic[0]).toContain('test_pack guidance')
    expect(one.critic[0]).toContain(REFUTATION_MARKER)
    expect(one.critic[0]).toContain('A pack')
  })

  it('ends where the run does, and says nothing after it', async () => {
    const controller = new AbortController()
    const call: ModelCall = async (_suffix, request) => {
      if (request.body.includes(REFUTATION_MARKER)) {
        controller.abort()
        return new Promise<Response>(() => {})
      }
      return new Response(turn({ text: PROPOSAL_TEXT }), {
        status: 200,
        headers: { 'content-type': 'text/event-stream' }
      })
    }
    const events = await drain(
      vercel.start(
        session(call, {
          signal: controller.signal,
          thinking: normalize('on', 'openai-compatible')
        })
      )
    )
    expect(events.map((event) => event.type)).not.toContain('critique')
    expect(events.map((event) => event.type)).not.toContain('proposal')
    expect(events.map((event) => event.type)).not.toContain('end')
  })
})
