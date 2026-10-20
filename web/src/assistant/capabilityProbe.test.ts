import { describe, expect, it } from 'vitest'
import type { ModelCall } from '@judgment-pack/harness/engine'
import { probeBody, probeCapabilities, PROBE_TOOL, readChatAnswer, readListing } from './capabilityProbe'

const chat = (message: Record<string, unknown>) => ({ choices: [{ message }] })
const call2 = (id: string, value: number) => ({ id, type: 'function', function: { name: PROBE_TOOL, arguments: JSON.stringify({ value }) } })

/** A relay that answers the chat request and the listing as the test says. */
function relay(answers: { chat: () => Response; listing?: () => Response }): { call: ModelCall; sent: { suffix: string; method: string; body: string }[] } {
  const sent: { suffix: string; method: string; body: string }[] = []
  return {
    sent,
    call: async (suffix, request) => {
      sent.push({ suffix, method: request.method ?? 'POST', body: request.body ?? '' })
      if (suffix === 'chat/completions') return answers.chat()
      return answers.listing ? answers.listing() : new Response('{}', { status: 404 })
    }
  }
}

describe('what the chat answer shows', () => {
  it('finds native and parallel tool calls where the reply carries two calls of the tool', () => {
    expect(readChatAnswer(chat({ tool_calls: [call2('a', 1), call2('b', 2)] }))).toEqual({ nativeToolCalls: true, parallelToolCalls: true })
  })

  it('finds native tool calls and no parallel ones where the reply carries one', () => {
    expect(readChatAnswer(chat({ tool_calls: [call2('a', 1)] }))).toEqual({ nativeToolCalls: true, parallelToolCalls: false })
  })

  it('finds none where the reply is prose, or calls a tool it was not offered', () => {
    expect(readChatAnswer(chat({ content: '{"name":"probe_echo","arguments":{"value":1}}' }))).toEqual({ nativeToolCalls: false, parallelToolCalls: false })
    expect(readChatAnswer(chat({ tool_calls: [{ id: 'x', type: 'function', function: { name: 'something_else', arguments: '{}' } }] }))).toEqual({ nativeToolCalls: false, parallelToolCalls: false })
  })

  it('finds the member the reasoning arrives in', () => {
    expect(readChatAnswer(chat({ reasoning_content: 'thinking…', tool_calls: [call2('a', 1)] }))).toMatchObject({ reasoningField: 'reasoning_content' })
    expect(readChatAnswer(chat({ reasoning: 'thinking…' }))).toMatchObject({ reasoningField: 'reasoning' })
    expect(readChatAnswer(chat({ content: 'x' }))).not.toHaveProperty('reasoningField')
  })

  it('reads no answer that is not a chat completion', () => {
    for (const answer of [null, [], 'text', {}, { choices: [] }, { choices: [{}] }, { choices: [{ message: 'x' }] }]) expect(readChatAnswer(answer), JSON.stringify(answer)).toBeUndefined()
  })
})

describe('what the listing shows', () => {
  it('reads the window and the output cap the common servers name, for the model asked about', () => {
    expect(readListing({ data: [{ id: 'other', max_model_len: 4096 }, { id: 'a-model', max_model_len: 32768, max_output_tokens: 4096 }] }, 'a-model')).toEqual({ contextWindow: 32768, outputReserve: 4096 })
    expect(readListing({ data: [{ id: 'a-model', context_length: 131072 }] }, 'a-model')).toEqual({ contextWindow: 131072 })
    expect(readListing({ data: [{ id: 'a-model', n_ctx: 8192 }] }, 'a-model')).toEqual({ contextWindow: 8192 })
  })

  it('holds the output cap to half the window, as the harness does', () => {
    expect(readListing({ data: [{ id: 'a-model', context_length: 8192, max_completion_tokens: 8192 }] }, 'a-model')).toEqual({ contextWindow: 8192, outputReserve: 4096 })
  })

  it('reads no number that is not a whole one in the harness\'s range, nor a cap with no window', () => {
    expect(readListing({ data: [{ id: 'a-model', max_model_len: 100 }] }, 'a-model')).toEqual({})
    expect(readListing({ data: [{ id: 'a-model', max_model_len: '32768' }] }, 'a-model')).toEqual({})
    expect(readListing({ data: [{ id: 'a-model', max_model_len: 5000000 }] }, 'a-model')).toEqual({})
    expect(readListing({ data: [{ id: 'a-model', max_output_tokens: 4096 }] }, 'a-model')).toEqual({})
    expect(readListing({ data: [{ id: 'another', max_model_len: 32768 }] }, 'a-model')).toEqual({})
    expect(readListing(null, 'a-model')).toEqual({})
  })
})

describe('the probe', () => {
  const NOW = () => new Date('2026-10-19T12:00:00.000Z')

  it('sends one small chat request offering one tool, and reads the listing once, and fills what it finds', async () => {
    const r = relay({
      chat: () => Response.json(chat({ tool_calls: [call2('a', 1), call2('b', 2)], reasoning_content: 'hmm' })),
      listing: () => Response.json({ data: [{ id: 'a-model', max_model_len: 32768 }] })
    })
    const outcome = await probeCapabilities(r.call, 'a-model', undefined, NOW)
    expect(outcome).toEqual({ ok: true, findings: { at: '2026-10-19T12:00:00.000Z', facts: { nativeToolCalls: true, parallelToolCalls: true, reasoningField: 'reasoning_content', contextWindow: 32768 }, unknown: ['vision', 'reasoning', 'tool-schemas', 'turn order'] } })
    expect(r.sent.map((request) => [request.suffix, request.method])).toEqual([['chat/completions', 'POST'], ['models', 'GET']])
    const body = JSON.parse(r.sent[0]!.body) as { model: string; stream: boolean; max_tokens: number; tools: unknown[]; messages: unknown[] }
    expect(body).toMatchObject({ model: 'a-model', stream: false, max_tokens: 128 })
    expect(body.tools).toHaveLength(1)
    expect(body.messages).toHaveLength(1)
    expect(r.sent[0]!.body).toBe(probeBody('a-model'))
  })

  it('finds an endpoint that refuses tools to have none natively, and says the window is unknown where the listing gives none', async () => {
    const r = relay({ chat: () => new Response('{"error":"tools are not supported"}', { status: 400 }) })
    const outcome = await probeCapabilities(r.call, 'a-model', undefined, NOW)
    expect(outcome).toMatchObject({ ok: true, findings: { facts: { nativeToolCalls: false, parallelToolCalls: false }, unknown: ['context window', 'vision', 'reasoning', 'tool-schemas', 'turn order'] } })
  })

  it('reports an endpoint that refuses for another reason, one that cannot be reached and an answer it cannot read, without quoting any of them', async () => {
    expect(await probeCapabilities(relay({ chat: () => new Response('{"error":"bad key sk-secret"}', { status: 401 }) }).call, 'a-model')).toEqual({ ok: false, problem: 'refused', status: 401 })
    const down: ModelCall = async () => { throw new Error('connect ECONNREFUSED 127.0.0.1:1') }
    expect(await probeCapabilities(down, 'a-model')).toEqual({ ok: false, problem: 'unreachable' })
    expect(await probeCapabilities(relay({ chat: () => new Response('<html>', { status: 200 }) }).call, 'a-model')).toEqual({ ok: false, problem: 'unreadable', status: 200 })
  })

  it('keeps what the chat request found where the listing cannot be read', async () => {
    const r = relay({ chat: () => Response.json(chat({ tool_calls: [call2('a', 1)] })), listing: () => new Response('nope', { status: 502 }) })
    const outcome = await probeCapabilities(r.call, 'a-model', undefined, NOW)
    expect(outcome).toMatchObject({ ok: true, findings: { facts: { nativeToolCalls: true, parallelToolCalls: false } } })
    expect((outcome as { findings: { facts: object } }).findings.facts).not.toHaveProperty('contextWindow')
  })
})
