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
import { ASSISTANT_TOOLS } from '../../../config/deskConfig'
import { loadEngine } from '../index'
import { vercel } from './index'
import { REHEARSAL_HOOK, SUPPRESSED_REJECTION } from './loop'
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
function turn(step: { text?: string; tool?: { name: string; args: unknown } }): string {
  const frame = (choices: unknown[]) =>
    `data: ${JSON.stringify({ id: 'c', object: 'chat.completion.chunk', model: 'm', choices })}\n\n`
  const lines = [frame([{ index: 0, delta: { role: 'assistant', content: null }, finish_reason: null }])]
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

describe('the registry', () => {
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

describe('the unhandled rejection the SDK’s refusal path leaks', () => {
  /** One `unhandledrejection` event, as a page would deliver it. */
  function reject(name: string): Event {
    const event = new Event('unhandledrejection', { cancelable: true })
    ;(event as { reason?: unknown }).reason = { name }
    globalThis.dispatchEvent(event)
    return event
  }

  it('is suppressed while a run is open, and only that one error name', async () => {
    const { call } = scriptedCall([turn({ text: PROPOSAL_TEXT })])
    const iterator = vercel.start(session(call))[Symbol.asyncIterator]()
    // One event in: the run is open.
    await iterator.next()
    expect(reject(SUPPRESSED_REJECTION).defaultPrevented).toBe(true)
    expect(reject('TypeError').defaultPrevented).toBe(false)
    for (;;) {
      const step = await iterator.next()
      if (step.done === true || step.value.type === 'end') break
    }
  })

  it('is not suppressed once the run has ended: the guard is removed', async () => {
    const { call } = scriptedCall([turn({ text: PROPOSAL_TEXT })])
    await drain(vercel.start(session(call)))
    // A listener that outlived the session would swallow the same error class
    // for a page that is no longer running an assistant at all.
    expect(reject(SUPPRESSED_REJECTION).defaultPrevented).toBe(false)
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
