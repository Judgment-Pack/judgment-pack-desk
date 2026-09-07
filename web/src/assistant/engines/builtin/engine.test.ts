/**
 * The built-in engine's own promises, each measured where it is kept.
 *
 * The whole scenario — eight steps, both wire formats, the real runtime's tool
 * definitions and its recorded answers — is `assistant/conformance/`. What is
 * here is the handful of properties that suite would only ever exercise
 * incidentally: the request's headers and URL, the turn bound, `end` exactly
 * once, the proposal's provenance, and the tier this chunk does not implement.
 */
import { afterEach, describe, expect, it, vi } from 'vitest'
import { CERTIFIED_ENGINES, loadEngine } from '../index'
import { builtin } from './index'
import { MAX_TURNS, extractProposal } from './loop'
import { protocolHeaders } from './providers/types'
import { ASSISTANT_ENGINES } from '../../../config/deskConfig'
import { RESPONSE_TOKENS, normalize } from '../../thinking'
import { REFUTATION_MARKER } from '../../refutation'
import { REFUTE_ON_A_DEGRADED_ENDPOINT } from '../../thinking'
import type {
  AssistantEvent,
  AssistantSession,
  McpTool,
  McpToolResult,
  ModelCall
} from '../../engine'


const TOOLS: McpTool[] = [
  { name: 'validate', description: 'check a document', inputSchema: { type: 'object' } }
]

/**
 * One recorded model call, in the shape a checker reads.
 *
 * The **suffix** and not a URL, because that is all the engine gets to choose:
 * the desk's capability builds the address. A test that recorded a URL would be
 * testing the desk's own binding, which `assistant/session.test.ts` holds.
 */
interface Recorded {
  suffix: string
  headerNames: string[]
  body: Record<string, unknown>
}

/** A model capability that answers from a script and remembers the calls. */
function stubModel(
  answers: (body: Record<string, unknown>, turn: number) => unknown
): { call: ModelCall; seen: Recorded[] } {
  const seen: Recorded[] = []
  let turn = 0
  const call: ModelCall = async (suffix, request) => {
    turn += 1
    const body = JSON.parse(request.body) as Record<string, unknown>
    seen.push({
      suffix,
      headerNames: Object.keys(request.headers ?? {}).map((name) => name.toLowerCase()),
      body
    })
    return new Response(JSON.stringify(answers(body, turn)), {
      status: 200,
      headers: { 'content-type': 'application/json' }
    })
  }
  return { call, seen }
}

function session(overrides: Partial<AssistantSession> = {}): AssistantSession {
  return {
    prompt: 'the runtime’s prompt',
    testPrompt: 'the runtime’s test_pack guidance',
    tools: TOOLS,
    callTool: async () => ({ content: [{ type: 'text', text: '{"status":"valid"}' }] }),
    model: { family: 'openai-compatible', model: 'a-model', call: async () => new Response('{}') },
    thinking: normalize('off', 'openai-compatible'),
    signal: new AbortController().signal,
    ...overrides
  }
}

/** A session whose model capability is the scripted one. */
function scripted(
  answers: (body: Record<string, unknown>, turn: number) => unknown,
  overrides: Partial<AssistantSession> = {}
): { session: AssistantSession; seen: Recorded[] } {
  const model = stubModel(answers)
  const base = session(overrides)
  return {
    session: { ...base, model: { ...base.model, call: model.call } },
    seen: model.seen
  }
}

async function drain(iterable: AsyncIterable<AssistantEvent>): Promise<AssistantEvent[]> {
  const events: AssistantEvent[] = []
  for await (const event of iterable) events.push(event)
  return events
}

/** A final message carrying the fenced proposal, in the endpoint's own shape. */
function finalMessage(text: string) {
  return { choices: [{ message: { role: 'assistant', content: text } }] }
}

const PROPOSAL_TEXT =
  'The runtime reports it valid.\n\n```json\n' +
  JSON.stringify({ proposal: { kind: 'create', document: { title: 'A pack' }, unknowns: ['one'] } }) +
  '\n```\n'

afterEach(() => vi.unstubAllGlobals())

describe('the request the engine makes', () => {
  it('carries no credential of any name', async () => {
    const { session: one, seen } = scripted(() => finalMessage(PROPOSAL_TEXT))
    await drain(builtin.start(one))
    expect(seen).toHaveLength(1)
    for (const name of ['authorization', 'x-api-key', 'cookie', 'api-key', 'proxy-authorization']) {
      expect(seen[0]!.headerNames, `the request carried ${name}`).not.toContain(name)
    }
    expect(seen[0]!.headerNames).toEqual(['content-type'])
  })

  it('carries no credential on the Anthropic path either', async () => {
    const { session: one, seen } = scripted(
      () => ({ content: [{ type: 'text', text: PROPOSAL_TEXT }] }),
      { model: { family: 'anthropic', model: 'm', call: async () => new Response('{}') } }
    )
    await drain(builtin.start(one))
    expect(seen[0]!.headerNames.sort()).toEqual(['anthropic-version', 'content-type'])
  })

  it('names a path suffix and never a URL', async () => {
    // The engine chooses the suffix; the desk builds the address. There is no
    // URL here to point somewhere else and no token to read out of one.
    const { session: one, seen } = scripted(() => finalMessage(PROPOSAL_TEXT))
    await drain(builtin.start(one))
    expect(seen[0]!.suffix).toBe('chat/completions')
    expect(seen[0]!.suffix).not.toContain('?')
    expect(seen[0]!.suffix).not.toContain('token')
  })

  it('names the Anthropic suffix on that leg', async () => {
    const { session: one, seen } = scripted(
      () => ({ content: [{ type: 'text', text: PROPOSAL_TEXT }] }),
      { model: { family: 'anthropic', model: 'm', call: async () => new Response('{}') } }
    )
    await drain(builtin.start(one))
    expect(seen[0]!.suffix).toBe('v1/messages')
  })

  it('puts stream in the body, which is why no query is ever needed', async () => {
    const { session: one, seen } = scripted(() => finalMessage(PROPOSAL_TEXT))
    await drain(builtin.start(one))
    expect(seen[0]!.body.stream).toBe(true)
    expect(seen[0]!.suffix).not.toContain('stream')
  })

  it('offers the runtime’s own tool definitions, schema included, unrewritten', async () => {
    const { session: one, seen } = scripted(() => finalMessage(PROPOSAL_TEXT))
    await drain(builtin.start(one))
    expect(seen[0]!.body.tools).toEqual([
      {
        type: 'function',
        function: {
          name: 'validate',
          description: 'check a document',
          parameters: { type: 'object' }
        }
      }
    ])
  })
})

describe('protocolHeaders', () => {
  it('is a content type and whatever the protocol needs, and nothing else', () => {
    expect(protocolHeaders()).toEqual({ 'content-type': 'application/json' })
    expect(protocolHeaders({ 'anthropic-version': '2023-06-01' })).toEqual({
      'content-type': 'application/json',
      'anthropic-version': '2023-06-01'
    })
  })
})

describe('the proposal comes out of the fenced block and nowhere else', () => {
  it('reads the one fenced block', () => {
    expect(extractProposal(PROPOSAL_TEXT)).toEqual({
      document: { title: 'A pack' },
      unknowns: ['one']
    })
  })

  it('ignores a JSON object written in the prose beside it', () => {
    // The shape the model actually produces: a worked example in the
    // explanation, and the document set apart in the block. An engine that
    // read the prose would offer a person the example to accept.
    const text =
      'My first draft was {"proposal": {"kind": "create", "document": {"title": "WRONG"}}} ' +
      'and the runtime refused it.\n\n```json\n' +
      JSON.stringify({ proposal: { kind: 'create', document: { title: 'RIGHT' }, unknowns: [] } }) +
      '\n```\n'
    expect(extractProposal(text).document).toEqual({ title: 'RIGHT' })
  })

  it('refuses a message with no fenced block, and one with two', () => {
    expect(() => extractProposal('no block here')).toThrow(/exactly one fenced JSON block/)
    expect(() => extractProposal(`${PROPOSAL_TEXT}\n${PROPOSAL_TEXT}`)).toThrow(/carried 2/)
  })

  it('refuses a fenced block that carries no proposal member', () => {
    expect(() => extractProposal('```json\n{"document": {}}\n```')).toThrow(/no "proposal" member/)
  })

  it('reports no unknowns as an empty list rather than as missing', () => {
    expect(
      extractProposal(`\`\`\`json\n${JSON.stringify({ proposal: { document: {} } })}\n\`\`\``)
    ).toEqual({ document: {}, unknowns: [] })
  })
})

describe('the event stream', () => {
  it('ends with exactly one end event on the happy path', async () => {
    const { session: one } = scripted(() => finalMessage(PROPOSAL_TEXT))
    const events = await drain(builtin.start(one))
    expect(events.filter((event) => event.type === 'end')).toHaveLength(1)
    expect(events.at(-1)!.type).toBe('end')
    expect(events.map((event) => event.type)).toEqual(['proposal', 'end'])
  })

  it('ends with exactly one end event when the endpoint refuses', async () => {
    const refusing = session({
      model: {
        family: 'openai-compatible',
        model: 'a-model',
        call: async () =>
          new Response(JSON.stringify({ error: 'no key stored', code: 'assistant-no-key' }), {
            status: 409,
            headers: { 'content-type': 'application/json' }
          })
      }
    })
    const events = await drain(builtin.start(refusing))
    expect(events.filter((event) => event.type === 'end')).toHaveLength(1)
    expect(events[0]).toMatchObject({ type: 'error' })
    expect((events[0] as { message: string }).message).toContain('409')
    expect((events[0] as { message: string }).message).toContain('no key stored')
  })

  it('ends with exactly one end event when the final message carries no proposal', async () => {
    const { session: one } = scripted(() => finalMessage('I could not write one.'))
    const events = await drain(builtin.start(one))
    expect(events.map((event) => event.type)).toEqual(['error', 'end'])
  })

  it('reports a tool call, then its result, in that order', async () => {
    let asked = 0
    const { session: one } = scripted((_body, turn) =>
      turn === 1
        ? {
            choices: [
              {
                message: {
                  role: 'assistant',
                  content: null,
                  tool_calls: [
                    {
                      id: 'call_1',
                      type: 'function',
                      function: { name: 'validate', arguments: '{"document":"{}"}' }
                    }
                  ]
                }
              }
            ]
          }
        : finalMessage(PROPOSAL_TEXT),
      {
        callTool: async (name, args) => {
          asked += 1
          expect(name).toBe('validate')
          expect(args).toEqual({ document: '{}' })
          return {
            content: [{ type: 'text', text: '{"status":"valid"}' }],
            structuredContent: { status: 'valid' }
          }
        }
      }
    )
    const events = await drain(builtin.start(one))
    expect(asked).toBe(1)
    expect(events.map((event) => event.type)).toEqual([
      'tool_call',
      'tool_result',
      'proposal',
      'end'
    ])
    expect(events[1]).toMatchObject({
      type: 'tool_result',
      name: 'validate',
      isError: false,
      text: '{"status":"valid"}',
      structured: { status: 'valid' }
    })
  })

  it('turns a gate refusal into a result the model is told about', async () => {
    // A dropped call is a turn the model spends re-asking. The refusal has to
    // come back as a result, which is what lets the session reach its proposal.
    const { session: one } = scripted(
      (_body, turn) =>
        turn === 1
          ? {
              choices: [
                {
                  message: {
                    role: 'assistant',
                    content: null,
                    tool_calls: [
                      {
                        id: 'call_1',
                        type: 'function',
                        function: { name: 'write_file', arguments: '{"path":"p"}' }
                      }
                    ]
                  }
                }
              ]
            }
          : finalMessage(PROPOSAL_TEXT),
      {
        callTool: async () => {
          throw new Error('refused on the wire: write_file is not one of the tools')
        }
      }
    )
    const events = await drain(builtin.start(one))
    expect(events[1]).toMatchObject({ type: 'tool_result', name: 'write_file', isError: true })
    expect((events[1] as { text: string }).text).toContain('never')
    expect(events.map((event) => event.type)).toContain('proposal')
  })

  it('bounds the session and says so where a model never stops calling tools', async () => {
    const { session: one } = scripted(() => ({
      choices: [
        {
          message: {
            role: 'assistant',
            content: null,
            tool_calls: [
              { id: 'c', type: 'function', function: { name: 'validate', arguments: '{}' } }
            ]
          }
        }
      ]
    }))
    const events = await drain(builtin.start(one))
    expect(events.filter((event) => event.type === 'tool_call')).toHaveLength(MAX_TURNS)
    expect(events.at(-2)).toMatchObject({ type: 'error' })
    expect((events.at(-2) as { message: string }).message).toContain(String(MAX_TURNS))
    expect(events.filter((event) => event.type === 'end')).toHaveLength(1)
  })

  it('stops on the session’s signal and still ends once', async () => {
    const controller = new AbortController()
    const stopping = session({
      signal: controller.signal,
      model: {
        family: 'openai-compatible',
        model: 'a-model',
        call: async () => {
          controller.abort()
          const error = new Error('aborted')
          error.name = 'AbortError'
          throw error
        }
      }
    })
    const events = await drain(builtin.start(stopping))
    // **Nothing at all, and the stream ends.** A viewer who pressed Stop has
    // not been told about a failure, and a cancelled run is a session somebody
    // ended rather than a run that finished — the terminal event belongs to the
    // one that finished. Both engines answer a cancellation this way, and the
    // page's terminal accounting is the run hook's, which writes an `end`
    // whatever an engine does.
    expect(events).toEqual([])
  })
})

describe('the thinking tier, on this engine’s own wire', () => {
  /**
   * A model capability that answers a scripted list of Responses in order.
   *
   * `turn` counts only the **main loop's** requests: the refutation pass is a
   * second conversation and is answered by `criticAnswer` below, so a test
   * about the tier is not also a test about the critic.
   */
  function answering(
    answers: (turn: number) => Response,
    criticAnswer: () => Response = () => whole({ choices: [{ message: { role: 'assistant', content: 'REFUTATION: nothing to report.' } }] })
  ): {
    call: ModelCall
    bodies: Record<string, unknown>[]
    critic: Record<string, unknown>[]
  } {
    const bodies: Record<string, unknown>[] = []
    const critic: Record<string, unknown>[] = []
    const call: ModelCall = async (_suffix, request) => {
      const body = JSON.parse(request.body) as Record<string, unknown>
      if (request.body.includes(REFUTATION_MARKER)) {
        critic.push(body)
        return criticAnswer()
      }
      bodies.push(body)
      return answers(bodies.length)
    }
    return { call, bodies, critic }
  }

  const whole = (payload: unknown) =>
    new Response(JSON.stringify(payload), {
      status: 200,
      headers: { 'content-type': 'application/json' }
    })

  const refusal = (message: string) =>
    new Response(JSON.stringify({ error: { message } }), {
      status: 400,
      headers: { 'content-type': 'application/json' }
    })

  const proposalMessage = { choices: [{ message: { role: 'assistant', content: PROPOSAL_TEXT } }] }
  const anthropicProposal = { content: [{ type: 'text', text: PROPOSAL_TEXT }] }
  /** The same answer with a thinking block on it, as a thinking endpoint sends. */
  const anthropicThought = {
    content: [
      { type: 'thinking', thinking: 'I read the schema.', signature: 'c2ln' },
      { type: 'text', text: PROPOSAL_TEXT }
    ]
  }

  const withModel = (
    call: ModelCall,
    family: 'openai-compatible' | 'anthropic',
    tier: 'off' | 'on' | 'ultra'
  ): AssistantSession => ({
    ...session(),
    model: { family, model: 'a-model', call },
    thinking: normalize(tier, family)
  })

  it('puts the desk’s table on every OpenAI-compatible request', async () => {
    for (const [tier, effort] of [
      ['on', 'high'],
      ['ultra', 'xhigh']
    ] as const) {
      const model = answering(() => whole(proposalMessage))
      await drain(builtin.start(withModel(model.call, 'openai-compatible', tier)))
      expect(model.bodies[0]!.reasoning_effort, tier).toBe(effort)
    }
  })

  it('puts the adaptive spelling and its sibling on an Anthropic request', async () => {
    const model = answering(() => whole(anthropicProposal))
    await drain(builtin.start(withModel(model.call, 'anthropic', 'on')))
    expect(model.bodies[0]!.thinking).toEqual({ type: 'adaptive' })
    expect(model.bodies[0]!.output_config).toEqual({ effort: 'high' })
  })

  it('sends no tier member at all where the tier is off', async () => {
    const model = answering(() => whole(proposalMessage))
    const events = await drain(builtin.start(withModel(model.call, 'openai-compatible', 'off')))
    expect(Object.keys(model.bodies[0]!)).not.toContain('reasoning_effort')
    expect(Object.keys(model.bodies[0]!)).not.toContain('thinking')
    expect(events.map((event) => event.type)).not.toContain('thinking_unavailable')
  })

  it('reads a passage back under either vendor name', async () => {
    for (const name of ['reasoning_content', 'reasoning'] as const) {
      const model = answering(() =>
        whole({
          choices: [{ message: { role: 'assistant', content: PROPOSAL_TEXT, [name]: 'I read it.' } }]
        })
      )
      const events = await drain(builtin.start(withModel(model.call, 'openai-compatible', 'on')))
      expect(events[0], name).toEqual({ type: 'reasoning', text: 'I read it.', done: true })
    }
  })

  it('falls back once to the other Anthropic spelling, and says nothing about it', async () => {
    const model = answering((turn) =>
      turn === 1 ? refusal('Adaptive thinking is not supported by this model') : whole(anthropicThought)
    )
    const events = await drain(builtin.start(withModel(model.call, 'anthropic', 'on')))
    expect(model.bodies).toHaveLength(2)
    expect(model.bodies[0]!.thinking).toEqual({ type: 'adaptive' })
    // The other spelling, with the budget in it — and no `output_config`.
    expect(model.bodies[1]!.thinking).toEqual({ type: 'enabled', budget_tokens: 8000 })
    expect(Object.keys(model.bodies[1]!)).not.toContain('output_config')
    // **And the maximum the budget requires.** The budget is spent out of
    // `max_tokens`, so a request carrying 8000 beside the provider's own 4096
    // is one an endpoint on this dialect refuses.
    expect(model.bodies[1]!.max_tokens).toBe(8000 + RESPONSE_TOKENS)
    expect(model.bodies[0]!.max_tokens).toBe(RESPONSE_TOKENS)
    // A fallback is not a degrade: the session still thinks and says nothing.
    expect(events.some((event) => event.type === 'thinking_unavailable')).toBe(false)
    expect(events.map((event) => event.type)).toContain('reasoning')
  })

  it('degrades once, retries plain, and completes the session', async () => {
    const model = answering((turn) =>
      turn === 1 ? refusal('Unsupported parameter: reasoning_effort') : whole(proposalMessage)
    )
    const events = await drain(builtin.start(withModel(model.call, 'openai-compatible', 'on')))
    expect(model.bodies).toHaveLength(2)
    expect(Object.keys(model.bodies[1]!)).not.toContain('reasoning_effort')
    const notices = events.filter((event) => event.type === 'thinking_unavailable')
    // **Once.** A degrade said twice is a reader learning to skip the line.
    expect(notices).toHaveLength(1)
    expect(events[0]!.type).toBe('thinking_unavailable')
    expect(events[events.length - 1]!.type).toBe('end')
    expect(events.some((event) => event.type === 'proposal')).toBe(true)
  })

  it('reports a model that always thinks, after two answers of it and not one', async () => {
    // **One answer is an answer, not a capability** — and the tool call in the
    // middle neither counts nor resets, which is the rule applied in both
    // tiers. The first turn answers and calls a tool; the second only calls a
    // tool; the third answers and proposes. Only then is it said.
    const thinkingTurn = (content: unknown) =>
      whole({ choices: [{ message: { role: 'assistant', reasoning_content: 'I thought.', ...(content as object) } }] })
    const aCall = (id: string) => [
      { id, type: 'function', function: { name: 'validate', arguments: '{"document":"{}"}' } }
    ]
    const model = answering((turn) => {
      if (turn === 1) return thinkingTurn({ content: 'Let me look.', tool_calls: aCall('call_1') })
      if (turn === 2) return whole({ choices: [{ message: { role: 'assistant', content: null, tool_calls: aCall('call_2') } }] })
      return thinkingTurn({ content: PROPOSAL_TEXT })
    })
    const events = await drain(builtin.start(withModel(model.call, 'openai-compatible', 'off')))
    expect(Object.keys(model.bodies[0]!)).not.toContain('reasoning_effort')
    expect(events.map((event) => event.type)).toEqual([
      'reasoning',
      'tool_call',
      'tool_result',
      'tool_call',
      'tool_result',
      'reasoning',
      'thinking_unavailable',
      'proposal',
      'end'
    ])
    expect((events[6] as { detail: string }).detail).toContain('always thinks')
    // …and no critic ran at all, because the tier is off.
    expect(model.critic).toEqual([])
  })

  it('says nothing about one reasoning turn among quiet ones', async () => {
    const model = answering((turn) =>
      turn === 1
        ? whole({
            choices: [
              {
                message: {
                  role: 'assistant',
                  content: 'Let me look.',
                  reasoning_content: 'I thought once.',
                  tool_calls: [
                    {
                      id: 'call_1',
                      type: 'function',
                      function: { name: 'validate', arguments: '{"document":"{}"}' }
                    }
                  ]
                }
              }
            ]
          })
        : whole(proposalMessage)
    )
    const events = await drain(builtin.start(withModel(model.call, 'openai-compatible', 'off')))
    expect(events.some((event) => event.type === 'thinking_unavailable')).toBe(false)
  })

  it('reports unavailable after two answers with no reasoning, and not after one', async () => {
    // The critic's own turn is this session's second answer, so the pass is
    // where the second observation lands on a session that proposes at once.
    const model = answering(() => whole(proposalMessage))
    const events = await drain(builtin.start(withModel(model.call, 'openai-compatible', 'on')))
    const notices = events.filter((event) => event.type === 'thinking_unavailable')
    expect(notices).toHaveLength(1)
    expect((notices[0] as { detail: string }).detail).toContain('no reasoning block')
    // Nothing was said on the first turn: the proposal's own request had
    // already gone out with the tier still on it.
    expect(Object.keys(model.bodies[0]!)).toContain('reasoning_effort')
  })

  it('rethrows a refusal that is not about the tier', async () => {
    const model = answering(() => refusal('messages: at least one message is required'))
    const events = await drain(builtin.start(withModel(model.call, 'openai-compatible', 'on')))
    expect(model.bodies).toHaveLength(1)
    expect(events.map((event) => event.type)).toEqual(['error', 'end'])
    expect((events[0] as { message: string }).message).toContain('at least one message')
  })
})

describe('what an Anthropic thinking turn survives on the way back', () => {
  /** One Anthropic SSE turn, block by block, exactly as the protocol writes it. */
  function anthropicStream(blocks: Record<string, unknown>[]): Response {
    const lines: string[] = []
    const event = (name: string, object: Record<string, unknown>) =>
      lines.push(`event: ${name}\ndata: ${JSON.stringify({ type: name, ...object })}\n\n`)
    event('message_start', {
      message: { id: 'm', role: 'assistant', content: [], usage: { input_tokens: 1, output_tokens: 0 } }
    })
    blocks.forEach((block, index) => {
      if (block.type === 'thinking') {
        event('content_block_start', { index, content_block: { type: 'thinking', thinking: '' } })
        event('content_block_delta', {
          index,
          delta: { type: 'thinking_delta', thinking: String(block.thinking ?? '') }
        })
        // **Split across two events**, which is the shape `vercel/ai#19663` is
        // about. This engine concatenates them; the fixture halves the value.
        const signature = String(block.signature ?? '')
        const half = Math.floor(signature.length / 2)
        event('content_block_delta', {
          index,
          delta: { type: 'signature_delta', signature: signature.slice(0, half) }
        })
        event('content_block_delta', {
          index,
          delta: { type: 'signature_delta', signature: signature.slice(half) }
        })
      } else if (block.type === 'redacted_thinking') {
        event('content_block_start', {
          index,
          content_block: { type: 'redacted_thinking', data: String(block.data ?? '') }
        })
      } else if (block.type === 'tool_use') {
        event('content_block_start', {
          index,
          content_block: { type: 'tool_use', id: String(block.id), name: String(block.name), input: {} }
        })
        event('content_block_delta', {
          index,
          delta: { type: 'input_json_delta', partial_json: JSON.stringify(block.input ?? {}) }
        })
      } else {
        event('content_block_start', { index, content_block: { type: 'text', text: '' } })
        event('content_block_delta', {
          index,
          delta: { type: 'text_delta', text: String(block.text ?? '') }
        })
      }
      event('content_block_stop', { index })
    })
    event('message_delta', { delta: { stop_reason: 'end_turn', stop_sequence: null }, usage: { output_tokens: 1 } })
    event('message_stop', {})
    return new Response(lines.join(''), {
      status: 200,
      headers: { 'content-type': 'text/event-stream' }
    })
  }

  it('carries the whole block back — text, a reassembled signature, and a redacted one', async () => {
    const bodies: Record<string, unknown>[] = []
    const call: ModelCall = async (_suffix, request) => {
      // The critic is a second conversation and is not what this measures.
      if (request.body.includes(REFUTATION_MARKER)) {
        return anthropicStream([{ type: 'text', text: 'REFUTATION: nothing to report.' }])
      }
      bodies.push(JSON.parse(request.body) as Record<string, unknown>)
      if (bodies.length === 1) {
        return anthropicStream([
          { type: 'thinking', thinking: 'I will check the schema.', signature: 'c2lnbmF0dXJlLVQx' },
          { type: 'redacted_thinking', data: 'cmVkYWN0ZWQ6VDE=' },
          { type: 'tool_use', id: 'toolu_1', name: 'validate', input: { document: '{}' } }
        ])
      }
      return anthropicStream([{ type: 'text', text: PROPOSAL_TEXT }])
    }
    const one: AssistantSession = {
      ...session(),
      model: { family: 'anthropic', model: 'a-model', call },
      thinking: normalize('on', 'anthropic')
    }
    const events = await drain(builtin.start(one))
    expect(events.map((event) => event.type)).toEqual([
      'reasoning',
      'tool_call',
      'tool_result',
      // The second turn proposes and carries no reasoning; the critic's own
      // turn is the second answer with none, so the desk reports it there.
      'thinking_unavailable',
      // The refutation pass runs at this tier and reached no runtime check.
      'critique',
      'proposal',
      'end'
    ])
    // The turn that goes back is the turn that arrived: every block, in order.
    const sent = (bodies[1]!.messages as { role: string; content: unknown }[])[1]!
    const carried = sent.content as { type: string; signature?: string; data?: string }[]
    expect(sent.role).toBe('assistant')
    expect(carried.map((block) => block.type)).toEqual([
      'thinking',
      'redacted_thinking',
      'tool_use'
    ])
    // **Reassembled.** Two `signature_delta` events are one signature, and a
    // half signature is a block the endpoint refuses.
    expect(carried[0]!.signature).toBe('c2lnbmF0dXJlLVQx')
    expect(carried[1]!.data).toBe('cmVkYWN0ZWQ6VDE=')
  })

  it('never sends the model’s reasoning text to the runtime', async () => {
    const asked: unknown[] = []
    const call: ModelCall = async (_suffix, request) => {
      if (request.body.includes(REFUTATION_MARKER)) {
        return anthropicStream([{ type: 'text', text: 'REFUTATION: nothing to report.' }])
      }
      const body = JSON.parse(request.body) as { messages?: unknown[] }
      if ((body.messages ?? []).length <= 1) {
        return anthropicStream([
          { type: 'thinking', thinking: 'A SECRET THOUGHT', signature: 'c2ln' },
          { type: 'tool_use', id: 'toolu_1', name: 'validate', input: { document: '{}' } }
        ])
      }
      return anthropicStream([{ type: 'text', text: PROPOSAL_TEXT }])
    }
    const one: AssistantSession = {
      ...session({ callTool: async (_name, args) => {
        asked.push(args)
        return { content: [{ type: 'text', text: '{"status":"valid"}' }] }
      } }),
      model: { family: 'anthropic', model: 'a-model', call },
      thinking: normalize('on', 'anthropic')
    }
    await drain(builtin.start(one))
    expect(asked).toEqual([{ document: '{}' }])
    expect(JSON.stringify(asked)).not.toContain('A SECRET THOUGHT')
  })
})

describe('a tool the runtime served without a schema', () => {
  it('ends the session naming it, rather than inventing a contract for it', async () => {
    const { session: one } = scripted(() => finalMessage(PROPOSAL_TEXT), {
      tools: [{ name: 'a_new_tool', description: 'd' }]
    })
    const events = await drain(builtin.start(one))
    expect(events.map((event) => event.type)).toEqual(['error', 'end'])
    expect((events[0] as { message: string }).message).toContain('a_new_tool')
    expect((events[0] as { message: string }).message).toContain('without an input schema')
  })
})

describe('a consumer that stops in the middle of a run', () => {
  it('stops a run whose model request only ends when it is aborted', async () => {
    // This engine had the same defect the SDK-backed one did, and for the same
    // reason: an async generator serves `next()` and `return()` from one queue,
    // so the `return()` was queued behind the `next()` it would have released.
    // It also had no abort of its own — the session's was the caller's — so
    // there was nothing for a `return()` to cancel with even if it had run.
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
    const bound = (work: Promise<unknown>) =>
      Promise.race([
        work.then(() => 'settled'),
        new Promise<string>((resolve) => setTimeout(() => resolve('STILL WAITING'), 2000))
      ])

    const settledInOrder: string[] = []
    const iterator = builtin.start(session({ model: { family: 'openai-compatible', model: 'a-model', call } }))[
      Symbol.asyncIterator
    ]()
    const pending = iterator.next().then((step) => {
      settledInOrder.push('next')
      return step
    })
    await entered
    const returned = iterator.return!(undefined).then((step) => {
      settledInOrder.push('return')
      return step
    })

    expect(await bound(pending), 'the pending next').toBe('settled')
    expect(await bound(returned), 'the return').toBe('settled')
    expect(await pending).toEqual({ value: undefined, done: true })
    expect(await returned).toEqual({ value: undefined, done: true })
    expect(settledInOrder).toEqual(['next', 'return'])
    expect(sawAbort, 'the request in flight observed the abort').toBe(true)
    expect(await iterator.next()).toEqual({ value: undefined, done: true })
  })
})

describe('a run stopped while it is waiting on the runtime', () => {
  /** A model that calls one tool, then proposes. */
  function callsATool(): ModelCall {
    let turn = 0
    return async () => {
      turn += 1
      return new Response(
        JSON.stringify(
          turn === 1
            ? {
                choices: [
                  {
                    message: {
                      role: 'assistant',
                      content: null,
                      tool_calls: [
                        {
                          id: 'call_1',
                          type: 'function',
                          function: { name: 'validate', arguments: '{}' }
                        }
                      ]
                    },
                    finish_reason: 'tool_calls'
                  }
                ]
              }
            : { choices: [{ message: { role: 'assistant', content: PROPOSAL_TEXT } }] }
        ),
        { status: 200, headers: { 'content-type': 'application/json' } }
      )
    }
  }
  const bound = (work: Promise<unknown>) =>
    Promise.race([
      work.then(
        () => 'settled',
        () => 'settled'
      ),
      new Promise<string>((resolve) => setTimeout(() => resolve('STILL WAITING'), 2000))
    ])

  describe.each([
    ['return()', (i: AsyncIterator<AssistantEvent>) => i.return!(undefined)],
    [
      'throw()',
      (i: AsyncIterator<AssistantEvent>) => i.throw!(new Error('gave up')).catch(() => undefined)
    ]
  ] as const)('%s', (_name, stop) => {
    it('settles the pending next first, then itself, and asks the runtime nothing more', async () => {
      // The interleaving this engine kept: `return()` aborted the provider's
      // signal and then waited on the inner generator, which was queued behind
      // a `next()` waiting on a `tools/call` that honours no signal at all.
      let arrived = () => {}
      const entered = new Promise<void>((resolve) => {
        arrived = resolve
      })
      let calls = 0
      const one = session({
        model: { family: 'openai-compatible', model: 'a-model', call: callsATool() },
        callTool: async () => {
          calls += 1
          arrived()
          return new Promise<McpToolResult>(() => {})
        }
      })
      const iterator = builtin.start(one)[Symbol.asyncIterator]()
      expect((await iterator.next()).value).toMatchObject({ type: 'tool_call' })
      const pending = iterator.next()
      await entered
      const settledInOrder: string[] = []
      const held = pending.then(() => settledInOrder.push('next'))
      const ended = Promise.resolve(stop(iterator)).then(() => settledInOrder.push('stop'))

      expect(await bound(held), 'the pending next').toBe('settled')
      expect(await bound(ended), 'the stop').toBe('settled')
      expect(await pending).toEqual({ value: undefined, done: true })
      expect(settledInOrder).toEqual(['next', 'stop'])
      expect(calls, 'the runtime was asked exactly once, before the stop').toBe(1)
    })
  })

  it('settles a pending next when the session itself is aborted mid tool call', async () => {
    const controller = new AbortController()
    let arrived = () => {}
    const entered = new Promise<void>((resolve) => {
      arrived = resolve
    })
    const one = session({
      signal: controller.signal,
      model: { family: 'openai-compatible', model: 'a-model', call: callsATool() },
      callTool: async () => {
        arrived()
        return new Promise<McpToolResult>(() => {})
      }
    })
    const iterator = builtin.start(one)[Symbol.asyncIterator]()
    expect((await iterator.next()).value).toMatchObject({ type: 'tool_call' })
    const pending = iterator.next()
    await entered
    controller.abort()
    expect(await bound(pending), 'the pending next').toBe('settled')
    expect(await pending).toEqual({ value: undefined, done: true })
  })

  it('lets no tools/call reach the runtime after the consumer has closed the run', async () => {
    // Two queued reads and a late model answer carrying a tool call. This
    // engine had no guard before dispatch at all.
    const asked: string[] = []
    let answer = () => {}
    const held = new Promise<void>((resolve) => {
      answer = resolve
    })
    const inner = callsATool()
    let turn = 0
    const one = session({
      model: {
        family: 'openai-compatible',
        model: 'a-model',
        call: async (suffix, request) => {
          turn += 1
          if (turn === 1) await held
          return inner(suffix, request)
        }
      },
      callTool: async (name) => {
        asked.push(name)
        return { content: [{ type: 'text', text: '{}' }] }
      }
    })
    const iterator = builtin.start(one)[Symbol.asyncIterator]()
    const first = iterator.next()
    const second = iterator.next()
    void first.catch(() => undefined)
    void second.catch(() => undefined)
    expect(await bound(iterator.return!(undefined) as Promise<unknown>)).toBe('settled')
    answer()
    await new Promise((resolve) => setTimeout(resolve, 100))
    expect(asked, 'a tools/call reached the runtime after the run closed').toEqual([])
    expect(await first).toEqual({ value: undefined, done: true })
    expect(await second).toEqual({ value: undefined, done: true })
  })
})

describe('a session that was already over before the run began', () => {
  it.each(['off', 'ultra'] as const)('emits nothing and asks nothing, at tier %s', async (tier) => {
    // An aborted signal is a run that is over. It used to abort this engine's
    // controller and nothing else, so it still said `thinking_unavailable` and
    // still evaluated `provider.send(...)` — a request made for a session that
    // never happened.
    const controller = new AbortController()
    controller.abort()
    let requests = 0
    let tools = 0
    const one = session({
      signal: controller.signal,
      thinking: normalize(tier, 'openai-compatible'),
      model: {
        family: 'openai-compatible',
        model: 'a-model',
        call: async () => {
          requests += 1
          return new Response(JSON.stringify(finalMessage(PROPOSAL_TEXT)), {
            status: 200,
            headers: { 'content-type': 'application/json' }
          })
        }
      },
      callTool: async () => {
        tools += 1
        return { content: [] }
      }
    })
    const events = await drain(builtin.start(one))
    expect(events).toEqual([])
    expect(requests, 'a request was made for a run that was already over').toBe(0)
    expect(tools, 'the runtime was asked by a run that was already over').toBe(0)
  })
})

describe('when the thing being awaited wins its race with the abort', () => {
  /** A model that calls one tool, then proposes. */
  function callsAToolThenProposes(): ModelCall {
    let turn = 0
    return async () => {
      turn += 1
      return new Response(
        JSON.stringify(
          turn === 1
            ? {
                choices: [
                  {
                    message: {
                      role: 'assistant',
                      content: null,
                      tool_calls: [
                        {
                          id: 'call_1',
                          type: 'function',
                          function: { name: 'validate', arguments: '{}' }
                        }
                      ]
                    },
                    finish_reason: 'tool_calls'
                  }
                ]
              }
            : { choices: [{ message: { role: 'assistant', content: PROPOSAL_TEXT } }] }
        ),
        { status: 200, headers: { 'content-type': 'application/json' } }
      )
    }
  }

  it('delivers nothing after the run closed, and no error and no end', async () => {
    // `withAbort` settles with the **value** when the work wins by a
    // microtask; the abort then runs while the loop is still holding it. What
    // stops the loop delivering a `tool_result` is not the abort — it is that
    // the run is already marked closed and the delivery path reads that.
    const controller = new AbortController()
    const one = session({
      signal: controller.signal,
      model: { family: 'openai-compatible', model: 'a-model', call: callsAToolThenProposes() },
      callTool: async () => {
        const answer: McpToolResult = { content: [{ type: 'text', text: '{"status":"ok"}' }] }
        controller.abort()
        return answer
      }
    })
    const iterator = builtin.start(one)[Symbol.asyncIterator]()
    expect((await iterator.next()).value).toMatchObject({ type: 'tool_call' })
    const after: AssistantEvent[] = []
    for (;;) {
      const step = await iterator.next()
      if (step.done === true) break
      after.push(step.value)
    }
    expect(after, 'an event was delivered after the run closed').toEqual([])
  })

  it('delivers no error where the awaited thing failed first', async () => {
    // The other half, and the one this engine got wrong: the underlying promise
    // **rejected** just before the abort, so the catch saw the original failure
    // rather than a cancellation and reported an `error` and an `end` for a run
    // that was already over.
    const controller = new AbortController()
    const one = session({
      signal: controller.signal,
      model: { family: 'openai-compatible', model: 'a-model', call: callsAToolThenProposes() },
      callTool: async () => {
        controller.abort()
        throw new Error('the socket went away')
      }
    })
    const iterator = builtin.start(one)[Symbol.asyncIterator]()
    expect((await iterator.next()).value).toMatchObject({ type: 'tool_call' })
    const after: AssistantEvent[] = []
    for (;;) {
      const step = await iterator.next()
      if (step.done === true) break
      after.push(step.value)
    }
    expect(after).toEqual([])
  })
})

describe('the registry', () => {
  it('carries builtin, and loads it as its own chunk', async () => {
    expect([...CERTIFIED_ENGINES]).toEqual(['builtin', 'vercel'])
    const engine = await loadEngine('builtin')
    expect(engine.id).toBe('builtin')
    expect(engine).toBe(builtin)
  })

  it('certifies every engine a desk.json may name, so nothing is substituted', () => {
    // The registry used to fall back to `builtin` for an id this build carried
    // no adapter for, and the tab said so in one line. Both engines ship now,
    // and `LOADERS` is a total map over `AssistantEngine`: an id added to the
    // decoder's closed list without a chunk is a compile error, which is a
    // stronger statement than a fallback nobody could reach.
    expect([...ASSISTANT_ENGINES].sort()).toEqual([...CERTIFIED_ENGINES].sort())
  })

  it('refuses an id no table registers, by name', async () => {
    // The registry's loader takes its table as a parameter so the conformance
    // session can put its certification fixtures down the path a certified
    // engine travels. That parameter is also the way an id with no chunk can
    // reach it, so the refusal is the loader's own and says which id.
    await expect(loadEngine('not-an-engine')).rejects.toThrow(
      'no engine chunk is registered for not-an-engine'
    )
    await expect(loadEngine('builtin', {})).rejects.toThrow(
      'no engine chunk is registered for builtin'
    )
  })

  it('loads from the table it is given, which is how a fixture is certified', async () => {
    const stub = { id: 'builtin' as const, start: () => [] as never }
    await expect(loadEngine('anything', { anything: async () => stub })).resolves.toBe(stub)
  })

})

describe('the refutation pass, on this engine’s second loop', () => {
  const VALID = JSON.stringify({ status: 'valid', diagnostics: [] })
  const INVALID = JSON.stringify({
    status: 'invalid',
    diagnostics: [{ code: 'JPS-SEMANTIC-UNRESOLVED-OUTCOME' }]
  })

  /**
   * A session whose main loop proposes at once and whose critic runs a script.
   *
   * `criticSays` is the critic's final prose, and `answer` is what the runtime
   * gives it: the point of the pair is that the two can disagree and the
   * verdict follows the runtime.
   */
  function criticised(options: {
    tier: 'off' | 'on' | 'ultra'
    criticCalls?: { name: string; args: Record<string, unknown> }[]
    criticSays: string
    answer: string
    isError?: boolean
  }): { session: AssistantSession; asked: { name: string; args: unknown }[]; critic: string[] } {
    const asked: { name: string; args: unknown }[] = []
    const critic: string[] = []
    let criticTurn = 0
    const call: ModelCall = async (_suffix, request) => {
      const body = JSON.parse(request.body) as { messages?: { content?: unknown }[] }
      if (request.body.includes(REFUTATION_MARKER)) {
        critic.push(JSON.stringify(body.messages))
        criticTurn += 1
        const calls = options.criticCalls ?? []
        const next = calls[criticTurn - 1]
        const message =
          next === undefined
            ? { role: 'assistant', content: options.criticSays }
            : {
                role: 'assistant',
                content: null,
                tool_calls: [
                  {
                    id: `call_c${criticTurn}`,
                    type: 'function',
                    function: { name: next.name, arguments: JSON.stringify(next.args) }
                  }
                ]
              }
        return new Response(JSON.stringify({ choices: [{ message }] }), {
          status: 200,
          headers: { 'content-type': 'application/json' }
        })
      }
      return new Response(
        JSON.stringify({
          choices: [
            {
              message: { role: 'assistant', content: PROPOSAL_TEXT, reasoning_content: 'thought' }
            }
          ]
        }),
        { status: 200, headers: { 'content-type': 'application/json' } }
      )
    }
    return {
      session: {
        ...session({
          callTool: async (name, args) => {
            asked.push({ name, args })
            return {
              content: [{ type: 'text', text: options.answer }],
              ...(options.isError === true ? { isError: true } : {})
            }
          }
        }),
        model: { family: 'openai-compatible', model: 'a-model', call },
        thinking: normalize(options.tier, 'openai-compatible')
      },
      asked,
      critic
    }
  }

  it('does not run at all where the tier is off', async () => {
    const one = criticised({ tier: 'off', criticSays: 'x', answer: VALID })
    const events = await drain(builtin.start(one.session))
    expect(one.critic).toEqual([])
    expect(events.some((event) => event.type === 'critique')).toBe(false)
    const proposal = events.find((event) => event.type === 'proposal')!
    expect((proposal as { critique?: unknown }).critique).toBeUndefined()
  })

  it('runs after the proposal exists and before it is shown', async () => {
    const one = criticised({
      tier: 'on',
      criticCalls: [{ name: 'validate', args: { document: '{}' } }],
      criticSays: 'REFUTATION: none found.',
      answer: VALID
    })
    const events = await drain(builtin.start(one.session))
    const kinds = events.map((event) => event.type)
    // The critic's own call and its answer are on the same stream, then the
    // critique, and only then the proposal.
    expect(kinds).toEqual([
      'reasoning',
      'tool_call',
      'tool_result',
      'critique',
      'proposal',
      'end'
    ])
    expect(kinds.indexOf('critique')).toBeLessThan(kinds.indexOf('proposal'))
    expect(one.asked).toEqual([{ name: 'validate', args: { document: '{}' } }])
  })

  it('takes the verdict from the runtime and not from the critic’s prose', async () => {
    // The runtime says valid; the critic says the pack is broken.
    const one = criticised({
      tier: 'on',
      criticCalls: [{ name: 'validate', args: { document: '{}' } }],
      criticSays: 'REFUTATION: this pack is broken and must not be used.',
      answer: VALID
    })
    const events = await drain(builtin.start(one.session))
    const critique = events.find(
      (event): event is Extract<AssistantEvent, { type: 'critique' }> => event.type === 'critique'
    )!
    expect(critique.refuted).toBe(false)
    expect(critique.checks).toEqual([{ tool: 'validate', status: 'valid' }])
    const proposal = events.find(
      (event): event is Extract<AssistantEvent, { type: 'proposal' }> => event.type === 'proposal'
    )!
    expect(proposal.critique).toEqual({ refuted: false })
  })

  it('reports refuted where the runtime refused, whatever the critic said', async () => {
    const one = criticised({
      tier: 'on',
      criticCalls: [{ name: 'validate', args: { document: '{}' } }],
      criticSays: 'REFUTATION: none found. Everything checks out.',
      answer: INVALID
    })
    const events = await drain(builtin.start(one.session))
    const critique = events.find(
      (event): event is Extract<AssistantEvent, { type: 'critique' }> => event.type === 'critique'
    )!
    expect(critique.refuted).toBe(true)
    expect(critique.text).toContain('1 diagnostic(s)')
    const proposal = events.find(
      (event): event is Extract<AssistantEvent, { type: 'proposal' }> => event.type === 'proposal'
    )!
    // **And the proposal is still shown.** Refutation is information, not a
    // failure: the desk shows the document with the runtime's words beside it.
    expect(proposal.critique).toEqual({ refuted: true })
    expect(proposal.document).toBeDefined()
  })

  it('says the critic ran no check, and puts no line on the proposal', async () => {
    const one = criticised({ tier: 'on', criticSays: 'I had a look.', answer: VALID })
    const events = await drain(builtin.start(one.session))
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

  it('hands the critic the runtime’s testing prompt and the proposed document', async () => {
    const one = criticised({ tier: 'on', criticSays: 'done', answer: VALID })
    await drain(builtin.start(one.session))
    expect(one.critic[0]).toContain('test_pack guidance')
    expect(one.critic[0]).toContain(REFUTATION_MARKER)
    // The document the main loop proposed, and no other.
    expect(one.critic[0]).toContain('A pack')
  })

  it('follows the ruling on a degraded endpoint, whichever way it is set', async () => {
    // **The alternative is one boolean and nothing else.** A maintainer who
    // rules the other way flips `REFUTE_ON_A_DEGRADED_ENDPOINT` and this case
    // follows: it asserts the *behaviour the constant asks for*, not the value
    // this chunk happens to carry.
    let refused = false
    const asked: string[] = []
    const call: ModelCall = async (_suffix, request) => {
      if (request.body.includes(REFUTATION_MARKER)) {
        asked.push('critic')
        return new Response(
          JSON.stringify({ choices: [{ message: { role: 'assistant', content: 'REFUTATION: none.' } }] }),
          { status: 200, headers: { 'content-type': 'application/json' } }
        )
      }
      if (!refused) {
        refused = true
        return new Response(
          JSON.stringify({ error: { message: 'Unsupported parameter: reasoning_effort' } }),
          { status: 400, headers: { 'content-type': 'application/json' } }
        )
      }
      return new Response(
        JSON.stringify({ choices: [{ message: { role: 'assistant', content: PROPOSAL_TEXT } }] }),
        { status: 200, headers: { 'content-type': 'application/json' } }
      )
    }
    const one: AssistantSession = {
      ...session(),
      model: { family: 'openai-compatible', model: 'a-model', call },
      thinking: normalize('on', 'openai-compatible')
    }
    const events = await drain(builtin.start(one))
    expect(events.filter((event) => event.type === 'thinking_unavailable')).toHaveLength(1)
    // The tab's line is the degrade's either way. Whether the pass ran is the
    // ruling's: under the default it does, because its value is the runtime's
    // checks rather than the model's thinking.
    expect(asked).toEqual(REFUTE_ON_A_DEGRADED_ENDPOINT ? ['critic'] : [])
    expect(events.some((event) => event.type === 'critique')).toBe(
      REFUTE_ON_A_DEGRADED_ENDPOINT
    )
    // …and the session completes with its proposal on either ruling.
    expect(events.some((event) => event.type === 'proposal')).toBe(true)
  })

  it('ends a critic tool call that is still in flight when the run is stopped', async () => {
    // **The run gate, on the critic's own dispatch.** The pass runs on
    // `guardedCallTool` — the same bounded capability the main loop uses — so a
    // viewer who presses Stop while the critic is waiting on `jpack mcp` ends
    // the pass. A critic handed `session.callTool` directly waits for ever on a
    // socket that answers nothing, and this session never ends.
    const controller = new AbortController()
    const call: ModelCall = async (_suffix, request) => {
      if (request.body.includes(REFUTATION_MARKER)) {
        return new Response(
          JSON.stringify({
            choices: [
              {
                message: {
                  role: 'assistant',
                  content: null,
                  tool_calls: [
                    {
                      id: 'call_c1',
                      type: 'function',
                      function: { name: 'validate', arguments: '{"document":"{}"}' }
                    }
                  ]
                }
              }
            ]
          }),
          { status: 200, headers: { 'content-type': 'application/json' } }
        )
      }
      return new Response(
        JSON.stringify({ choices: [{ message: { role: 'assistant', content: PROPOSAL_TEXT } }] }),
        { status: 200, headers: { 'content-type': 'application/json' } }
      )
    }
    const one: AssistantSession = {
      ...session({
        signal: controller.signal,
        // The viewer presses Stop while this call is in flight, and it never
        // settles — which is what a socket to a subprocess does.
        callTool: async () => {
          controller.abort()
          return new Promise(() => {})
        }
      }),
      model: { family: 'openai-compatible', model: 'a-model', call },
      thinking: normalize('on', 'openai-compatible')
    }
    const events = await drain(builtin.start(one))
    // It ended, and said nothing after the cancel.
    expect(events.map((event) => event.type)).not.toContain('critique')
    expect(events.map((event) => event.type)).not.toContain('proposal')
    expect(events.map((event) => event.type)).not.toContain('end')
  })

  it('ends where the run does, and says nothing after it', async () => {
    // The cancellation seam holds for the critic exactly as for the first loop.
    const controller = new AbortController()
    const call: ModelCall = async (_suffix, request) => {
      if (request.body.includes(REFUTATION_MARKER)) {
        controller.abort()
        return new Promise<Response>(() => {})
      }
      return new Response(
        JSON.stringify({ choices: [{ message: { role: 'assistant', content: PROPOSAL_TEXT } }] }),
        { status: 200, headers: { 'content-type': 'application/json' } }
      )
    }
    const one: AssistantSession = {
      ...session({ signal: controller.signal }),
      model: { family: 'openai-compatible', model: 'a-model', call },
      thinking: normalize('on', 'openai-compatible')
    }
    const events = await drain(builtin.start(one))
    // Nothing at all after the cancel: no critique, no proposal, no `end`.
    expect(events.map((event) => event.type)).not.toContain('critique')
    expect(events.map((event) => event.type)).not.toContain('proposal')
    expect(events.map((event) => event.type)).not.toContain('end')
  })
})

describe('the native Gemini wire, on this engine’s own provider', () => {
  /** One whole Gemini answer, in the endpoint's own shape. */
  const answer = (parts: unknown[]) => ({
    candidates: [{ content: { role: 'model', parts }, finishReason: 'STOP' }]
  })
  const fenced = (text: string) => answer([{ text }])

  /** The same, streamed: one `GenerateContentResponse` per event. */
  function streamed(frames: unknown[][]): Response {
    const body = frames
      .map((parts) => `data: ${JSON.stringify(answer(parts))}\n\n`)
      .join('')
    return new Response(body, { status: 200, headers: { 'content-type': 'text/event-stream' } })
  }

  const geminiSession = (call: ModelCall, tier: 'off' | 'on' = 'off'): AssistantSession => ({
    ...session(),
    model: { family: 'gemini', model: 'a-model', call },
    thinking: normalize(tier, 'gemini')
  })

  it('addresses the method in the path, and asks for its stream in the one pair', async () => {
    const seen: Recorded[] = []
    const call: ModelCall = async (suffix, request) => {
      seen.push({
        suffix,
        headerNames: Object.keys(request.headers ?? {}).map((name) => name.toLowerCase()),
        body: JSON.parse(request.body) as Record<string, unknown>
      })
      return new Response(JSON.stringify(fenced(PROPOSAL_TEXT)), {
        status: 200,
        headers: { 'content-type': 'application/json' }
      })
    }
    await drain(builtin.start(geminiSession(call)))
    expect(seen[0]!.suffix).toBe('v1beta/models/a-model:streamGenerateContent?alt=sse')
    // No credential of any name, and no header this wire does not need.
    expect(seen[0]!.headerNames).toEqual(['content-type'])
    // The system prompt is not a message on this wire.
    expect(seen[0]!.body.systemInstruction).toEqual({ parts: [{ text: expect.any(String) }] })
    expect(Object.keys(seen[0]!.body).sort()).toEqual([
      'contents',
      'generationConfig',
      'systemInstruction',
      'tools'
    ])
    // The declarations are the runtime's own, in this wire's own wrapper.
    expect(seen[0]!.body.tools).toEqual([
      { functionDeclarations: [{ name: 'validate', description: 'check a document', parameters: { type: 'object' } }] }
    ])
  })

  it('joins a thought summary streamed in pieces, and keeps its signature on it', async () => {
    // **The one accumulation this provider does**, and the reason: a signature
    // arrives on the last piece of the summary it belongs to, and a client that
    // kept the pieces apart would send back a signature on a part with no text
    // — which is a continuation the endpoint refuses.
    const call: ModelCall = async () =>
      streamed([
        [{ text: 'I read ', thought: true }],
        [{ text: 'the policy.', thought: true, thoughtSignature: 'c2ln' }],
        [{ text: PROPOSAL_TEXT }]
      ])
    const events = await drain(builtin.start(geminiSession(call, 'on')))
    const passages = events.filter(
      (event): event is Extract<AssistantEvent, { type: 'reasoning' }> => event.type === 'reasoning'
    )
    // Every passage this session produced — the loop's and the critic's, which
    // runs on the same endpoint — is the joined summary rather than its pieces.
    expect(passages.length).toBeGreaterThan(0)
    for (const passage of passages) {
      expect(passage.text).toBe('I read the policy.')
      expect(passage.done).toBe(true)
    }
    expect(events.some((event) => event.type === 'proposal')).toBe(true)
  })

  it('never joins a thought part into an answer part, or a call into either', async () => {
    const call: ModelCall = async () =>
      streamed([
        [{ text: 'thinking', thought: true, thoughtSignature: 'c2ln' }],
        [{ text: 'The runtime reports it valid.\n\n' }],
        [{ text: '```json\n{"proposal":{"document":{"title":"A pack"},"unknowns":[]}}\n```' }]
      ])
    const events = await drain(builtin.start(geminiSession(call, 'on')))
    const passages = events.filter(
      (event): event is Extract<AssistantEvent, { type: 'reasoning' }> => event.type === 'reasoning'
    )
    // The thought is its own passage and never absorbs the answer beside it,
    // whichever loop produced it; the two answer pieces are joined into the one
    // message the proposal is read out of.
    expect(passages.length).toBeGreaterThan(0)
    for (const passage of passages) expect(passage.text).toBe('thinking')
    const proposal = events.find(
      (event): event is Extract<AssistantEvent, { type: 'proposal' }> => event.type === 'proposal'
    )
    expect(proposal!.document).toEqual({ title: 'A pack' })
  })

  it('sends the turn back exactly as it came, with a functionResponse beside it', async () => {
    const seen: Record<string, unknown>[] = []
    let turn = 0
    const call: ModelCall = async (_suffix, request) => {
      turn += 1
      seen.push(JSON.parse(request.body) as Record<string, unknown>)
      if (turn === 1) {
        return streamed([
          [{ text: 'checking', thought: true, thoughtSignature: 'c2ln' }],
          [{ functionCall: { name: 'validate', args: { document: '{}' } } }]
        ])
      }
      return new Response(JSON.stringify(fenced(PROPOSAL_TEXT)), {
        status: 200,
        headers: { 'content-type': 'application/json' }
      })
    }
    await drain(builtin.start(geminiSession(call, 'on')))
    const contents = seen[1]!.contents as { role: string; parts: Record<string, unknown>[] }[]
    // user, model (as received), user (the result)
    expect(contents.map((content) => content.role)).toEqual(['user', 'model', 'user'])
    // **As received**: the thought part goes back with its text and its
    // signature, on the part that carried them.
    expect(contents[1]!.parts[0]).toEqual({
      text: 'checking',
      thought: true,
      thoughtSignature: 'c2ln'
    })
    expect(contents[1]!.parts[1]).toEqual({
      functionCall: { name: 'validate', args: { document: '{}' } }
    })
    // The result is a functionResponse naming the tool, with **no invented id**:
    // the call carried none, so neither does the response.
    expect(contents[2]!.parts).toEqual([
      {
        functionResponse: {
          name: 'validate',
          response: { name: 'validate', content: '{"status":"valid"}' }
        }
      }
    ])
  })

  it('carries the tier into generationConfig, and nothing there at all when off', async () => {
    const seen: Record<string, unknown>[] = []
    const call: ModelCall = async (_suffix, request) => {
      seen.push(JSON.parse(request.body) as Record<string, unknown>)
      return new Response(JSON.stringify(fenced(PROPOSAL_TEXT)), {
        status: 200,
        headers: { 'content-type': 'application/json' }
      })
    }
    await drain(builtin.start(geminiSession(call, 'on')))
    expect(seen[0]!.generationConfig).toEqual({
      thinkingConfig: normalize('on', 'gemini').wire!.members.thinkingConfig
    })
    seen.length = 0
    await drain(builtin.start(geminiSession(call, 'off')))
    // **Off is a member on this wire**, because omission means thinking here.
    expect(seen[0]!.generationConfig).toEqual({ thinkingConfig: { thinkingBudget: 0 } })
  })

  it('reads this wire’s own error envelope, so a refusal is classified', async () => {
    const call: ModelCall = async () =>
      new Response(
        JSON.stringify({
          error: {
            code: 400,
            message: 'Unknown name "thinkingConfig" at \'generation_config\'',
            status: 'INVALID_ARGUMENT'
          }
        }),
        { status: 400, headers: { 'content-type': 'application/json' } }
      )
    const events = await drain(builtin.start(geminiSession(call, 'on')))
    // Two spellings tried and then the degrade, said once — this endpoint
    // refuses both, so the session reports the endpoint has no thinking.
    const notices = events.filter((event) => event.type === 'thinking_unavailable')
    expect(notices).toHaveLength(1)
  })
})
