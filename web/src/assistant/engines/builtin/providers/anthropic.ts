/**
 * Anthropic messages, by hand.
 *
 * The same loop, a different wire shape: flat tools, `tool_use` blocks in, a
 * user turn of `tool_result` blocks back, the system prompt in its own
 * top-level member, and a different path after the relay's mount point.
 *
 * **The assistant turn is kept as received** — the whole `content` array, in
 * order, nothing filtered by block type. That is what makes thinking work here
 * by construction: a `thinking` block goes back with its signature because the
 * block that arrived is the block that is sent, and `redacted_thinking` — which
 * a filter by block type silently drops — survives for the same reason.
 *
 * **A signature split across two `signature_delta` events is one signature.**
 * The protocol's own default is a single event and `vercel/ai#19663` was closed
 * on that argument, but a re-chunking proxy is an ordinary deployment, so the
 * fragments are concatenated here rather than the last one kept. What that
 * costs is one `+=`; what keeping the last one costs is a malformed block on
 * the next request.
 *
 * Ported from the bake-off's `none` prototype, thinking handling included.
 */
import { servedSchemaFor, withAbort } from '../../contract'
import { RESPONSE_TOKENS } from '../../../thinking'
import { isEventStream, sseEvents } from './sse'
import { ModelHttpError, protocolHeaders } from './types'
import type { McpTool } from '../../../engine'
import type { ModelTurn, Provider, SendOptions, ToolCall } from './types'

interface Block {
  type?: string
  text?: string
  id?: string
  name?: string
  input?: unknown
  [key: string]: unknown
}

/**
 * The reasoning passages in one content array, in the endpoint's own order.
 *
 * A `redacted_thinking` block carries no text a person could read, so it is not
 * a passage — but it is still in `content`, and `content` is what goes back.
 */
function reasoningOf(content: Block[]): string[] {
  return content
    .filter((block) => block.type === 'thinking')
    .map((block) => String(block.thinking ?? ''))
    .filter((said) => said !== '')
}

/** Every signature in one content array, whole. */
function signaturesOf(content: Block[]): string[] {
  return content
    .map((block) => (typeof block.signature === 'string' ? block.signature : ''))
    .filter((signature) => signature !== '')
}

function callsOf(content: Block[]): ToolCall[] {
  const calls: ToolCall[] = []
  for (const block of content) {
    if (block.type !== 'tool_use') continue
    calls.push({
      id: String(block.id ?? ''),
      name: String(block.name ?? ''),
      args: (block.input ?? {}) as Record<string, unknown>,
      argsText: JSON.stringify(block.input ?? {})
    })
  }
  return calls
}

/**
 * The one path this protocol posts to, whatever the request asks for.
 *
 * The `v1` before it is the relay's mount point, not this endpoint's: one desk
 * route serves every protocol, and each lands after the configured base exactly
 * where the chassis' own probe sends its request.
 */
const SUFFIX = 'v1/messages'

export const anthropic: Provider = {
  family: 'anthropic',
  path: () => SUFFIX,

  tools(defs: McpTool[]) {
    return defs.map((def) => ({
      name: def.name,
      description: def.description ?? '',
      // The runtime's own, or nothing at all: see `servedSchema`.
      input_schema: servedSchemaFor('anthropic', def)
    }))
  },

  initialMessages(_system: string, user: string) {
    return [{ role: 'user', content: [{ type: 'text', text: user }] }]
  },

  async send(options: SendOptions): Promise<ModelTurn> {
    const body: Record<string, unknown> = {
      model: options.model,
      // The desk's response allowance. The tier's members below may raise it:
      // the enabled thinking dialect's budget is spent out of this number, so
      // the table that chooses the budget chooses the maximum with it.
      max_tokens: RESPONSE_TOKENS,
      system: options.system,
      messages: options.messages,
      tools: options.tools,
      stream: options.stream,
      // The desk's table's members, merged. On this family that is
      // `thinking` beside `output_config`, or `thinking` with a budget in it —
      // and which of the two is `assistant/thinking.ts`'s decision, not this
      // provider's.
      ...(options.thinking ?? {})
    }

    // **Bounded by the run's signal**, and a thunk: a closed run makes no
    // request at all, and a capability that never settled could not hold
    // this loop open.
    const response = await withAbort(
      () =>
        options.call(SUFFIX, {
          // `anthropic-version` is on the relay's outbound allow-list; nothing
          // resembling a credential is, and nothing here is one.
          headers: protocolHeaders({ 'anthropic-version': '2023-06-01' }),
          body: JSON.stringify(body),
          signal: options.signal
        }),
      options.signal
    )
    if (!response.ok) {
      throw new ModelHttpError(response.status, await withAbort(() => response.text(), options.signal), SUFFIX)
    }

    if (!isEventStream(response)) {
      const payload = (await withAbort(() => response.json(), options.signal)) as { content?: Block[] }
      const content = Array.isArray(payload.content) ? payload.content : []
      const text = content
        .filter((block) => block.type === 'text')
        .map((block) => block.text ?? '')
        .join('')
      // `content` is the array the endpoint sent, kept by reference: no map, no
      // filter, no rebuild.
      return {
        text,
        calls: callsOf(content),
        assistant: { role: 'assistant', content },
        reasoning: reasoningOf(content),
        // A thinking block on this wire always carries its text, so a passage
        // and the fact of one are the same thing here.
        reasoned: reasoningOf(content).length > 0,
        signatures: signaturesOf(content)
      }
    }

    let text = ''
    const blocks = new Map<number, Block>()
    const partial = new Map<number, string>()
    for await (const data of sseEvents(response, options.signal)) {
      const event = JSON.parse(data) as {
        type?: string
        index?: number
        content_block?: Block
        delta?: {
          type?: string
          text?: string
          partial_json?: string
          thinking?: string
          signature?: string
        }
      }
      if (event.type === 'content_block_start') {
        // Whatever kind it is, the block starts as the one that arrived.
        blocks.set(event.index ?? 0, { ...(event.content_block ?? {}) })
        if (event.content_block?.type === 'tool_use') partial.set(event.index ?? 0, '')
      } else if (event.type === 'content_block_delta') {
        const block = blocks.get(event.index ?? 0)
        if (!block) continue
        const delta = event.delta ?? {}
        if (delta.type === 'text_delta') {
          block.text = (block.text ?? '') + (delta.text ?? '')
          text += delta.text ?? ''
        } else if (delta.type === 'thinking_delta') {
          block.thinking = String(block.thinking ?? '') + (delta.thinking ?? '')
        } else if (delta.type === 'signature_delta') {
          // **Concatenated, not replaced.** Two events are one signature; the
          // block that goes back must carry the whole of it or the endpoint
          // refuses the next request.
          block.signature = String(block.signature ?? '') + (delta.signature ?? '')
        } else if (delta.type === 'input_json_delta') {
          partial.set(event.index ?? 0, (partial.get(event.index ?? 0) ?? '') + (delta.partial_json ?? ''))
        }
      } else if (event.type === 'message_stop') {
        break
      }
    }
    for (const [index, argsText] of partial) {
      const block = blocks.get(index)
      if (!block) continue
      try {
        block.input = JSON.parse(argsText || '{}')
      } catch {
        block.input = {}
      }
    }
    const content = [...blocks.keys()]
      .sort((left, right) => left - right)
      .map((key) => blocks.get(key)!)
    return {
      text,
      calls: callsOf(content),
      assistant: { role: 'assistant', content },
      reasoning: reasoningOf(content),
      reasoned: reasoningOf(content).length > 0,
      signatures: signaturesOf(content)
    }
  },

  appendTurn(messages, turn, results) {
    // The assistant message goes back exactly as it came.
    messages.push(
      turn.assistant ?? {
        role: 'assistant',
        content: turn.calls.map((call) => ({
          type: 'tool_use',
          id: call.id,
          name: call.name,
          input: call.args
        }))
      }
    )
    messages.push({
      role: 'user',
      content: results.map((result) => ({
        type: 'tool_result',
        tool_use_id: result.call.id,
        content: [{ type: 'text', text: result.text }],
        ...(result.isError ? { is_error: true } : {})
      }))
    })
  }
}
