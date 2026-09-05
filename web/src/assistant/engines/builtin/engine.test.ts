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
import { CERTIFIED_ENGINES, isCertified, loadEngine, resolveEngine } from '../index'
import { builtin } from './index'
import { MAX_TURNS, extractProposal } from './loop'
import { protocolHeaders } from './providers/types'
import type { AssistantEvent, AssistantSession, McpTool, ModelCall } from '../../engine'


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
    tools: TOOLS,
    callTool: async () => ({ content: [{ type: 'text', text: '{"status":"valid"}' }] }),
    model: { family: 'openai-compatible', model: 'a-model', call: async () => new Response('{}') },
    thinking: { tier: 'off' },
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
    expect(events).toEqual([{ type: 'error', message: 'the session was stopped' }, { type: 'end' }])
  })
})

describe('the thinking tier this chunk does not run', () => {
  it.each(['on', 'ultra'] as const)('reports %s unavailable and carries on', async (tier) => {
    const { session: one } = scripted(() => finalMessage(PROPOSAL_TEXT), { thinking: { tier } })
    const events = await drain(builtin.start(one))
    expect(events[0]).toMatchObject({ type: 'thinking_unavailable' })
    expect((events[0] as { detail: string }).detail).toContain(tier)
    expect((events[0] as { detail: string }).detail).toContain('does not run a thinking tier yet')
    // Reported, and then the session completes: degrading is not refusing.
    expect(events.map((event) => event.type)).toEqual(['thinking_unavailable', 'proposal', 'end'])
  })

  it('says nothing at all where the tier is off', async () => {
    const { session: one } = scripted(() => finalMessage(PROPOSAL_TEXT))
    const events = await drain(builtin.start(one))
    expect(events.map((event) => event.type)).not.toContain('thinking_unavailable')
  })
})

describe('the registry', () => {
  it('carries builtin, and loads it as its own chunk', async () => {
    expect([...CERTIFIED_ENGINES]).toEqual(['builtin'])
    const engine = await loadEngine('builtin')
    expect(engine.id).toBe('builtin')
    expect(engine).toBe(builtin)
  })

  it('falls back to builtin for an engine this build does not carry, and says which', () => {
    expect(isCertified('vercel')).toBe(false)
    expect(resolveEngine('vercel')).toEqual({
      id: 'builtin',
      substituted: 'vercel is not certified in this build; running builtin'
    })
  })

  it('substitutes nothing where the configured engine is certified', () => {
    expect(resolveEngine('builtin')).toEqual({ id: 'builtin' })
  })
})
