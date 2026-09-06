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
import { eventChannel } from './channel'
import { vercel } from './index'
import { REHEARSAL_HOOK, claimPromises } from './loop'
import { ADDRESS_REFUSED, PLACEHOLDER_ORIGIN, placeholderBase, reframe, relayFetch, suffixOf } from './relay'
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
    tools: TOOLS,
    callTool:
      callTool ??
      (async (): Promise<McpToolResult> => ({ content: [{ type: 'text', text: '{"status":"ok"}' }] })),
    model: { family: 'openai-compatible', model: 'a-model', call },
    thinking: { tier: 'off' },
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

  it('settles a delivery abandoned before the drain ever ran', async () => {
    const channel = eventChannel()
    const waiting = channel.push({ type: 'end' })
    channel.abandon()
    expect(await within(500, waiting)).toBe('settled')
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
    const fetch = relayFetch({ family: 'openai-compatible', call })
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
    const fetch = relayFetch({ family: 'anthropic', call })
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

  it('ends once and says nothing else when the session is aborted', async () => {
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
    expect(events.map((event) => event.type)).toEqual(['end'])
  })

  it('ends once where the final message carries no fenced block', async () => {
    const { call } = scriptedCall([turn({ text: 'I could not write one.' })])
    const events = await drain(vercel.start(session(call)))
    expect(events.map((event) => event.type)).toEqual(['error', 'end'])
    expect((events[0] as { message: string }).message).toContain('exactly one fenced JSON block')
  })
})

describe('a consumer that stops in the middle of a run', () => {
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
  })

  it('says nothing where the endpoint reasoned about nothing', async () => {
    const { call } = scriptedCall([turn({ text: PROPOSAL_TEXT })])
    const events = await drain(vercel.start(session(call)))
    expect(events.some((event) => event.type === 'reasoning')).toBe(false)
  })
})

describe('the thinking tier this chunk does not run', () => {
  it.each(['on', 'ultra'] as const)('reports %s unavailable and carries on', async (tier) => {
    const { call } = scriptedCall([turn({ text: PROPOSAL_TEXT })])
    const events = await drain(vercel.start(session(call, { thinking: { tier } })))
    expect(events.map((event) => event.type)).toEqual(['thinking_unavailable', 'proposal', 'end'])
    expect((events[0] as { detail: string }).detail).toContain(tier)
    expect((events[0] as { detail: string }).detail).toContain('vercel')
  })
})
