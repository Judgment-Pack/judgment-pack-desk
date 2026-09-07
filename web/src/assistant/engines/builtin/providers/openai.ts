/**
 * OpenAI-compatible chat completions, by hand.
 *
 * The whole provider is a base URL and a body: there is no SDK between this
 * object and the wire, so nothing can drop a member, rewrite a served schema,
 * or attach a credential the page was never given. Ported from the bake-off's
 * `none` prototype, reasoning fields included.
 *
 * **Reasoning is read under both vendor names.** DeepSeek's own API and
 * vLLM < 0.18 send `reasoning_content`; vLLM ≥ 0.18 and OpenRouter send
 * `reasoning`. The bake-off measured this engine as the only candidate that
 * reads both, and it is one line in each of the two readers below.
 *
 * **The turn goes back under the names it came under.** Nothing in the
 * chat-completions protocol requires a client to send reasoning back and
 * several endpoints refuse it, so this desk neither invents a member nor drops
 * one: a whole answer is echoed as the object it arrived as, and a streamed one
 * is reassembled with exactly the member names the deltas used.
 */
import { servedSchemaFor, withAbort } from '../../contract'
import { isEventStream, sseEvents } from './sse'
import { ModelHttpError, protocolHeaders } from './types'
import type { McpTool } from '../../../engine'
import type { ModelTurn, Provider, SendOptions, ToolCall } from './types'

interface OpenAiToolCall {
  id?: string
  index?: number
  function?: { name?: string; arguments?: string }
}

interface OpenAiMessage {
  content?: string | null
  tool_calls?: OpenAiToolCall[]
  /** vLLM < 0.18, DeepSeek. */
  reasoning_content?: string | null
  /** vLLM >= 0.18, OpenRouter. */
  reasoning?: string | null
}

/** The two names one reasoning passage may arrive under, in preference order. */
const REASONING_NAMES = ['reasoning_content', 'reasoning'] as const

/** The passage this message carried, under whichever name it used. */
function reasoningOf(message: OpenAiMessage): string[] {
  for (const name of REASONING_NAMES) {
    const said = message[name]
    if (typeof said === 'string' && said !== '') return [said]
  }
  return []
}

function parseArgs(text: string): Record<string, unknown> {
  try {
    const value: unknown = JSON.parse(text || '{}')
    return value !== null && typeof value === 'object' ? (value as Record<string, unknown>) : {}
  } catch {
    return {}
  }
}

function callsOf(raw: OpenAiToolCall[]): ToolCall[] {
  return raw.map((call) => ({
    id: String(call.id ?? ''),
    name: String(call.function?.name ?? ''),
    argsText: call.function?.arguments ?? '{}',
    args: parseArgs(call.function?.arguments ?? '{}')
  }))
}

function turnOf(message: OpenAiMessage): ModelTurn {
  const text = typeof message.content === 'string' ? message.content : ''
  const reasoning = reasoningOf(message)
  return {
    text,
    calls: callsOf(message.tool_calls ?? []),
    // As received, with whatever reasoning member the endpoint put on it.
    assistant: message,
    reasoning,
    // This protocol carries reasoning as prose and nothing else, so a passage
    // and the fact of one are the same thing here.
    reasoned: reasoning.length > 0,
    // Signatures are Anthropic's; this protocol has none.
    signatures: []
  }
}

/** The one path this protocol posts to, whatever the request asks for. */
const SUFFIX = 'chat/completions'

export const openai: Provider = {
  family: 'openai-compatible',
  path: () => SUFFIX,

  tools(defs: McpTool[]) {
    // The schema is the runtime's own `inputSchema`, passed through untouched.
    // Nothing here writes a schema of its own: the model is shown the contract
    // the runtime actually enforces, or — `servedSchema` refusing — nothing at
    // all, and the session ends rather than showing an invented one.
    return defs.map((def) => ({
      type: 'function',
      function: {
        name: def.name,
        description: def.description ?? '',
        parameters: servedSchemaFor('openai-compatible', def)
      }
    }))
  },

  initialMessages(system: string, user: string) {
    return [
      { role: 'system', content: system },
      { role: 'user', content: user }
    ]
  },

  async send(options: SendOptions): Promise<ModelTurn> {
    const body: Record<string, unknown> = {
      model: options.model,
      messages: options.messages,
      tools: options.tools,
      stream: options.stream,
      // The desk's table's members, merged. Nothing here decides what they are.
      ...(options.thinking ?? {})
    }
    if (options.stream) body.stream_options = { include_usage: true }

    // **Bounded by the run's signal**, and a thunk: a closed run makes no
    // request at all, and a capability that never settled could not hold
    // this loop open.
    const response = await withAbort(
      () =>
        options.call(SUFFIX, {
          headers: protocolHeaders(),
          body: JSON.stringify(body),
          signal: options.signal
        }),
      options.signal
    )
    if (!response.ok) {
      throw new ModelHttpError(response.status, await withAbort(() => response.text(), options.signal), SUFFIX)
    }

    if (!isEventStream(response)) {
      const payload = (await withAbort(() => response.json(), options.signal)) as { choices?: { message?: OpenAiMessage }[] }
      return turnOf(payload.choices?.[0]?.message ?? {})
    }

    let text = ''
    /** The reasoning deltas, per name, so the echo uses the names that arrived. */
    const reasoning = new Map<string, string>()
    const slots = new Map<number, { id: string; name: string; argsText: string }>()
    for await (const data of sseEvents(response, options.signal)) {
      if (data === '[DONE]') break
      const chunk = JSON.parse(data) as {
        choices?: {
          delta?: {
            content?: string
            tool_calls?: OpenAiToolCall[]
            reasoning_content?: string
            reasoning?: string
          }
        }[]
      }
      for (const choice of chunk.choices ?? []) {
        const delta = choice.delta ?? {}
        if (delta.content) text += delta.content
        for (const name of REASONING_NAMES) {
          const piece = delta[name]
          if (typeof piece === 'string' && piece !== '') {
            reasoning.set(name, (reasoning.get(name) ?? '') + piece)
          }
        }
        for (const call of delta.tool_calls ?? []) {
          const index = call.index ?? 0
          const slot = slots.get(index) ?? { id: '', name: '', argsText: '' }
          if (call.id) slot.id = call.id
          if (call.function?.name) slot.name = call.function.name
          if (call.function?.arguments) slot.argsText += call.function.arguments
          slots.set(index, slot)
        }
      }
    }
    const calls: ToolCall[] = [...slots.keys()]
      .sort((left, right) => left - right)
      .map((key) => {
        const slot = slots.get(key)!
        return { id: slot.id, name: slot.name, argsText: slot.argsText, args: parseArgs(slot.argsText) }
      })
    // Reassembled in the shape the endpoint would have sent whole, so the echo
    // on the next turn is the same object either way — **including the
    // reasoning members, under the names the deltas used and no others**.
    const assistant: Record<string, unknown> = { role: 'assistant', content: text || null }
    for (const [name, said] of reasoning) assistant[name] = said
    if (calls.length) {
      assistant.tool_calls = calls.map((call) => ({
        id: call.id,
        type: 'function',
        function: { name: call.name, arguments: call.argsText }
      }))
    }
    const passages: string[] = []
    for (const name of REASONING_NAMES) {
      const said = reasoning.get(name)
      if (said !== undefined && said !== '') {
        passages.push(said)
        break
      }
    }
    return {
      text,
      calls,
      assistant,
      reasoning: passages,
      reasoned: passages.length > 0,
      signatures: []
    }
  },

  appendTurn(messages, turn, results) {
    messages.push(
      turn.assistant ?? {
        role: 'assistant',
        content: null,
        tool_calls: turn.calls.map((call) => ({
          id: call.id,
          type: 'function',
          function: { name: call.name, arguments: call.argsText }
        }))
      }
    )
    for (const result of results) {
      messages.push({
        role: 'tool',
        tool_call_id: result.call.id,
        name: result.call.name,
        content: result.text
      })
    }
  }
}
