/**
 * OpenAI-compatible chat completions, by hand.
 *
 * The whole provider is a base URL and a body: there is no SDK between this
 * object and the wire, so nothing can drop a member, rewrite a served schema,
 * or attach a credential the page was never given. Ported from the bake-off's
 * `none` prototype, minus the reasoning fields (chunk 4).
 */
import { isEventStream, sseEvents } from './sse'
import { ModelHttpError, relayHeaders, relayRequestUrl } from './types'
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
  return { text, calls: callsOf(message.tool_calls ?? []), assistant: message }
}

export const openai: Provider = {
  family: 'openai-compatible',
  suffix: 'chat/completions',

  tools(defs: McpTool[]) {
    // The schema is the runtime's own `inputSchema`, passed through untouched.
    // Nothing here writes a schema of its own: the model is shown the contract
    // the runtime actually enforces, or it is shown nothing.
    return defs.map((def) => ({
      type: 'function',
      function: {
        name: def.name,
        description: def.description ?? '',
        parameters: def.inputSchema ?? { type: 'object', properties: {} }
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
      stream: options.stream
    }
    if (options.stream) body.stream_options = { include_usage: true }

    const response = await fetch(relayRequestUrl(options.base, openai.suffix), {
      method: 'POST',
      headers: relayHeaders(),
      body: JSON.stringify(body),
      signal: options.signal
    })
    if (!response.ok) {
      throw new ModelHttpError(response.status, await response.text(), openai.suffix)
    }

    if (!isEventStream(response)) {
      const payload = (await response.json()) as { choices?: { message?: OpenAiMessage }[] }
      return turnOf(payload.choices?.[0]?.message ?? {})
    }

    let text = ''
    const slots = new Map<number, { id: string; name: string; argsText: string }>()
    for await (const data of sseEvents(response)) {
      if (data === '[DONE]') break
      const chunk = JSON.parse(data) as {
        choices?: { delta?: { content?: string; tool_calls?: OpenAiToolCall[] } }[]
      }
      for (const choice of chunk.choices ?? []) {
        const delta = choice.delta ?? {}
        if (delta.content) text += delta.content
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
    // on the next turn is the same object either way.
    const assistant: Record<string, unknown> = { role: 'assistant', content: text || null }
    if (calls.length) {
      assistant.tool_calls = calls.map((call) => ({
        id: call.id,
        type: 'function',
        function: { name: call.name, arguments: call.argsText }
      }))
    }
    return { text, calls, assistant }
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
