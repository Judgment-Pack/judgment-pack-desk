/**
 * The bake-off's scripted model, in TypeScript, as a `fetch` stub.
 *
 * A port of `fixture/scripted_model.py` from
 * `docs/experiments/2026-09-05-assistant-engine-bakeoff`: the same step logic
 * (`count_openai`, `count_anthropic`, `choose_step`), the same two wire
 * formats, the same SSE grammars, the same repeat detector. The thinking and
 * critic halves are dropped — chunk 4 — and so is the run-id bookkeeping, which
 * existed so several candidates could share one HTTP server.
 *
 * **It decides the next step from the conversation, not from a counter it
 * keeps.** n is the number of scenario tool results already present in the
 * request's own messages; the answer is step n+1. So a session that dropped a
 * result, replayed a turn, or rebuilt its message array wrongly gets a
 * different script — which is what makes this a test of the engine's message
 * handling and not only of its parsing.
 *
 * It also **records every request**: the header names, the tool names offered,
 * the message count and the URL, which is what `requests.jsonl` carried in the
 * experiment and what K1 and K2 are read off here.
 */
import scenario from './scenario.json'

export interface ScenarioStep {
  id: string
  kind: string
  tool?: string
  arguments?: Record<string, unknown>
  text?: string
}

const STEPS = scenario.steps as ScenarioStep[]
const COUNTED = new Set(scenario.countedTools)

/** One request the page made, as the checker reads it. */
export interface RecordedRequest {
  url: string
  /** Lower-cased header names only. A value is never recorded, not even a length. */
  headerNames: string[]
  api: 'openai-compatible' | 'anthropic'
  streamRequested: boolean
  toolNames: string[]
  messageCount: number
  /** The step this answer carried. */
  step: string
  /** How many scenario tool results the request's own messages carried. */
  results: number
  /**
   * The body's own top-level members, sorted.
   *
   * Recorded so a leg can say **what the engine actually sent** rather than
   * only that the session worked: the two engines differ here — one puts
   * `stream_options` on an OpenAI-compatible request and the other a
   * `tool_choice` — and the conformance session holds them to what the two wire
   * formats define rather than to each other's spelling.
   */
  bodyMembers: string[]
}

interface OpenAiMessage {
  role?: string
  tool_call_id?: string
  name?: string
  tool_calls?: { id?: string; function?: { name?: string } }[]
}

/**
 * Count results for counted tools in an OpenAI message list.
 *
 * A result is a `role: "tool"` message, and its tool is resolved through the
 * `tool_call_id` an earlier assistant message announced. A result whose call
 * cannot be resolved is counted anyway: nothing else in this scenario makes
 * tool results.
 */
export function countOpenAi(messages: unknown[]): number {
  const namesById = new Map<string, string>()
  let n = 0
  for (const raw of messages ?? []) {
    const message = raw as OpenAiMessage
    if (message?.role === 'assistant') {
      for (const call of message.tool_calls ?? []) {
        const id = call.id
        const name = call.function?.name
        if (id && name) namesById.set(id, name)
      }
    } else if (message?.role === 'tool' || message?.role === 'function') {
      const name = namesById.get(String(message.tool_call_id)) ?? message.name
      if (name === undefined || COUNTED.has(name)) n += 1
    }
  }
  return n
}

interface AnthropicBlock {
  type?: string
  id?: string
  name?: string
  tool_use_id?: string
}

/** Count `tool_result` blocks for counted tools in an Anthropic message list. */
export function countAnthropic(messages: unknown[]): number {
  const namesById = new Map<string, string>()
  let n = 0
  for (const raw of messages ?? []) {
    const content = (raw as { content?: unknown }).content
    if (!Array.isArray(content)) continue
    for (const item of content) {
      const block = item as AnthropicBlock
      if (block?.type === 'tool_use') {
        if (block.id && block.name) namesById.set(block.id, block.name)
      } else if (block?.type === 'tool_result') {
        const name = namesById.get(String(block.tool_use_id))
        if (name === undefined || COUNTED.has(name)) n += 1
      }
    }
  }
  return n
}

/**
 * The step at result-count n, with the experiment's repeat detector.
 *
 * A harness that dropped a refused call without returning any result would
 * re-trigger the same step for ever; after three identical repeats the script
 * advances, so a run that stalls ends rather than hanging the suite.
 */
export function chooseStep(state: { lastBase?: number; repeats: number }, n: number): ScenarioStep {
  const base = Math.min(n + 1, STEPS.length)
  if (state.lastBase === base) state.repeats += 1
  else {
    state.lastBase = base
    state.repeats = 1
  }
  const offset = state.repeats > 3 ? state.repeats - 3 : 0
  return STEPS[Math.min(base + offset, STEPS.length) - 1]!
}

function openAiAnswer(step: ScenarioStep) {
  const message =
    step.kind === 'tool_call'
      ? {
          role: 'assistant',
          content: null,
          tool_calls: [
            {
              id: `call_${step.id}`,
              type: 'function',
              function: { name: step.tool, arguments: JSON.stringify(step.arguments ?? {}) }
            }
          ]
        }
      : { role: 'assistant', content: step.text }
  return {
    id: `chatcmpl-${step.id}`,
    object: 'chat.completion',
    model: 'scripted-model',
    choices: [
      {
        index: 0,
        message,
        finish_reason: step.kind === 'tool_call' ? 'tool_calls' : 'stop',
        logprobs: null
      }
    ],
    usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 }
  }
}

function anthropicAnswer(step: ScenarioStep) {
  const content =
    step.kind === 'tool_call'
      ? [{ type: 'tool_use', id: `toolu_${step.id}`, name: step.tool, input: step.arguments ?? {} }]
      : [{ type: 'text', text: step.text }]
  return {
    id: `msg_${step.id}`,
    type: 'message',
    role: 'assistant',
    model: 'scripted-model',
    content,
    stop_reason: step.kind === 'tool_call' ? 'tool_use' : 'end_turn',
    stop_sequence: null,
    usage: { input_tokens: 1, output_tokens: 1 }
  }
}

function chunks(text: string, size: number): string[] {
  if (!text) return ['']
  const out: string[] = []
  for (let index = 0; index < text.length; index += size) out.push(text.slice(index, index + size))
  return out
}

function openAiStream(step: ScenarioStep): string {
  const id = `chatcmpl-${step.id}`
  const lines: string[] = []
  const frame = (choices: unknown[]) =>
    lines.push(
      `data: ${JSON.stringify({ id, object: 'chat.completion.chunk', model: 'scripted-model', choices })}\n\n`
    )
  frame([{ index: 0, delta: { role: 'assistant', content: null }, finish_reason: null }])
  if (step.kind === 'tool_call') {
    const args = JSON.stringify(step.arguments ?? {})
    frame([
      {
        index: 0,
        delta: {
          tool_calls: [
            {
              index: 0,
              id: `call_${step.id}`,
              type: 'function',
              function: { name: step.tool, arguments: '' }
            }
          ]
        },
        finish_reason: null
      }
    ])
    // Split across frames, exactly as an endpoint does: an engine that read
    // only the first frame's arguments would send an empty document.
    for (const piece of chunks(args, 256)) {
      frame([
        { index: 0, delta: { tool_calls: [{ index: 0, function: { arguments: piece } }] }, finish_reason: null }
      ])
    }
    frame([{ index: 0, delta: {}, finish_reason: 'tool_calls' }])
  } else {
    for (const piece of chunks(step.text ?? '', 96)) {
      frame([{ index: 0, delta: { content: piece }, finish_reason: null }])
    }
    frame([{ index: 0, delta: {}, finish_reason: 'stop' }])
  }
  lines.push('data: [DONE]\n\n')
  return lines.join('')
}

/**
 * **`message_delta` carries `usage`, because the protocol says it does.**
 *
 * The fixture used to send `{"type":"message_delta","delta":{"stop_reason":…}}`
 * and nothing else, which no Anthropic endpoint sends: the documented event
 * carries the message's running output-token count, and `stop_sequence` beside
 * the stop reason. The built-in engine never noticed, because it reads only
 * `content_block_*` and `message_stop` — so the gap was invisible until an
 * engine that validates the whole event grammar ran the same leg and refused
 * it. That is the fixture being wrong about the wire, not an engine being
 * fussy, and it is fixed here rather than branched around.
 */
function anthropicStream(step: ScenarioStep): string {
  const lines: string[] = []
  const event = (name: string, object: unknown) =>
    lines.push(`event: ${name}\ndata: ${JSON.stringify(object)}\n\n`)
  event('message_start', {
    type: 'message_start',
    message: {
      id: `msg_${step.id}`,
      type: 'message',
      role: 'assistant',
      model: 'scripted-model',
      content: [],
      stop_reason: null,
      usage: { input_tokens: 1, output_tokens: 0 }
    }
  })
  event('ping', { type: 'ping' })
  if (step.kind === 'tool_call') {
    event('content_block_start', {
      type: 'content_block_start',
      index: 0,
      content_block: { type: 'tool_use', id: `toolu_${step.id}`, name: step.tool, input: {} }
    })
    for (const piece of chunks(JSON.stringify(step.arguments ?? {}), 256)) {
      event('content_block_delta', {
        type: 'content_block_delta',
        index: 0,
        delta: { type: 'input_json_delta', partial_json: piece }
      })
    }
    event('content_block_stop', { type: 'content_block_stop', index: 0 })
    event('message_delta', {
      type: 'message_delta',
      delta: { stop_reason: 'tool_use', stop_sequence: null },
      usage: { output_tokens: 1 }
    })
  } else {
    event('content_block_start', {
      type: 'content_block_start',
      index: 0,
      content_block: { type: 'text', text: '' }
    })
    for (const piece of chunks(step.text ?? '', 96)) {
      event('content_block_delta', {
        type: 'content_block_delta',
        index: 0,
        delta: { type: 'text_delta', text: piece }
      })
    }
    event('content_block_stop', { type: 'content_block_stop', index: 0 })
    event('message_delta', {
      type: 'message_delta',
      delta: { stop_reason: 'end_turn', stop_sequence: null },
      usage: { output_tokens: 1 }
    })
  }
  event('message_stop', { type: 'message_stop' })
  return lines.join('')
}

export interface ScriptedModel {
  /** Install as `globalThis.fetch`. */
  fetch: typeof fetch
  requests: RecordedRequest[]
}

/**
 * One scripted endpoint, as a `fetch` stub.
 *
 * `answerAs` decides what comes back over the wire: `stream` writes SSE and
 * `whole` writes one JSON object, whatever the request asked for. That is the
 * fourth leg of the conformance matrix — an endpoint that ignores `stream` is
 * a gateway that buffers, and the engine has to read the answer it got rather
 * than the answer it asked for.
 */
export function scriptedModel(options: {
  api: 'openai-compatible' | 'anthropic'
  answerAs: 'stream' | 'whole'
}): ScriptedModel {
  const requests: RecordedRequest[] = []
  const state = { lastBase: undefined as number | undefined, repeats: 0 }

  const stub = async (input: unknown, init?: RequestInit): Promise<Response> => {
    const url = String(input)
    const headers = (init?.headers ?? {}) as Record<string, string>
    const body = JSON.parse(String(init?.body ?? '{}')) as {
      messages?: unknown[]
      tools?: { name?: string; function?: { name?: string } }[]
      stream?: boolean
      [member: string]: unknown
    }
    const messages = body.messages ?? []
    const n = options.api === 'anthropic' ? countAnthropic(messages) : countOpenAi(messages)
    const step = chooseStep(state, n)
    requests.push({
      url,
      headerNames: Object.keys(headers).map((name) => name.toLowerCase()),
      api: options.api,
      streamRequested: Boolean(body.stream),
      toolNames: (body.tools ?? []).map((tool) => String(tool.function?.name ?? tool.name ?? '')),
      messageCount: messages.length,
      step: step.id,
      results: n,
      bodyMembers: Object.keys(body as Record<string, unknown>).sort()
    })

    if (options.answerAs === 'whole') {
      const answer = options.api === 'anthropic' ? anthropicAnswer(step) : openAiAnswer(step)
      return new Response(JSON.stringify(answer), {
        status: 200,
        headers: { 'content-type': 'application/json' }
      })
    }
    const text = options.api === 'anthropic' ? anthropicStream(step) : openAiStream(step)
    return new Response(text, {
      status: 200,
      headers: { 'content-type': 'text/event-stream' }
    })
  }

  return { fetch: stub as unknown as typeof fetch, requests }
}
