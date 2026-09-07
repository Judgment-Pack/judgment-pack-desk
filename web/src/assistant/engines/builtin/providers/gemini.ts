/**
 * The native Gemini wire, by hand.
 *
 * The same loop, a third wire shape, and it differs from the other two in more
 * than spelling:
 *
 * - **the address carries the model and the streaming choice**, as
 *   `v1beta/models/<model>:streamGenerateContent?alt=sse` against
 *   `:generateContent`, rather than a fixed path and two body members. That is
 *   why `Provider.path` is a function, and it is why the chassis relay has a
 *   colon exception and a one-pair query exception at all;
 * - **a turn is a list of `parts`**, not a list of typed blocks: a thought
 *   summary is a `text` part with `thought: true` beside it, and a signature is
 *   a `thoughtSignature` member on whichever part carried it;
 * - **the system prompt is `systemInstruction`**, a `Content` of its own rather
 *   than a member or a message.
 *
 * **The assistant turn is kept as received**, which is the property the other
 * two providers keep and the reason thinking works here by construction: the
 * `parts` array that arrived is the `parts` array that goes back, so a
 * `thoughtSignature` travels because the part it is on travels, and a part
 * shape this desk has never heard of survives because nothing filters by kind.
 * Google's own rule is that thought blocks are resent exactly as they were
 * received; this provider has no code that could do otherwise.
 *
 * **The one accumulation, and its limit.** A streamed answer arrives as several
 * `GenerateContentResponse` chunks whose `parts` are the continuation of one
 * another — a thought summary comes in pieces and its signature arrives on the
 * last of them — so consecutive text parts of the same kind are joined into
 * one, which is what the wire means and what makes a signature land on a part
 * that still has its text. Nothing else is joined: a `functionCall` part is
 * never merged, a thought part is never merged into an answer part, and no
 * value is rewritten.
 *
 * The reference this was written against is in the README, with the date it was
 * read.
 */
import { servedSchemaFor, withAbort } from '../../contract'
import { isEventStream, sseEvents } from './sse'
import { ModelHttpError, protocolHeaders } from './types'
import type { McpTool } from '../../../engine'
import type { ModelTurn, Provider, SendOptions, ToolCall } from './types'

/**
 * One `Part`, as far as this provider needs to know — which is deliberately
 * not much.
 *
 * The index signature is the point: what arrives is kept whole, so a member
 * this desk does not read is still on the object that goes back.
 */
interface Part {
  text?: string
  thought?: boolean
  thoughtSignature?: string
  functionCall?: { id?: string; name?: string; args?: unknown }
  [member: string]: unknown
}

interface Candidate {
  content?: { role?: string; parts?: Part[] }
  finishReason?: string
}

interface GenerateContentResponse {
  candidates?: Candidate[]
}

/** The version segment this desk addresses, which the endpoint's base omits. */
const VERSION = 'v1beta'

/** Whether a part is a text part rather than a call or something else. */
function isText(part: Part | undefined): boolean {
  return part !== undefined && typeof part.text === 'string' && part.functionCall === undefined
}

/**
 * Add one arriving part to the turn being assembled.
 *
 * Consecutive text parts of the same kind — both a thought summary, or both an
 * answer — are one part, because that is what a streamed continuation is. A
 * signature arriving on a later piece belongs to the part it continues, so it
 * is carried onto the joined part rather than left on a fragment with no text
 * of its own, which is a block a continuation may be refused for.
 */
function absorb(parts: Part[], arriving: Part): void {
  const last = parts[parts.length - 1]
  if (
    isText(arriving) &&
    isText(last) &&
    (last!.thought === true) === (arriving.thought === true)
  ) {
    last!.text = (last!.text ?? '') + (arriving.text ?? '')
    if (typeof arriving.thoughtSignature === 'string' && arriving.thoughtSignature !== '') {
      last!.thoughtSignature = arriving.thoughtSignature
    }
    return
  }
  // A copy, so the object this provider goes on to write a signature onto is
  // never the one the JSON parser handed a caller elsewhere.
  parts.push({ ...arriving })
}

/** The thought summaries in one turn, in the endpoint's own order. */
function reasoningOf(parts: Part[]): string[] {
  return parts
    .filter((part) => part.thought === true && typeof part.text === 'string' && part.text !== '')
    .map((part) => part.text!)
}

/** Every signature the turn carried, whole and in order. */
function signaturesOf(parts: Part[]): string[] {
  return parts
    .map((part) => (typeof part.thoughtSignature === 'string' ? part.thoughtSignature : ''))
    .filter((signature) => signature !== '')
}

/** The answer text: the parts that are not thought summaries. */
function textOfParts(parts: Part[]): string {
  return parts
    .filter((part) => part.thought !== true && typeof part.text === 'string')
    .map((part) => part.text!)
    .join('')
}

function callsOf(parts: Part[]): ToolCall[] {
  const calls: ToolCall[] = []
  for (const part of parts) {
    const call = part.functionCall
    if (call === undefined || call.name === undefined) continue
    const args = (call.args ?? {}) as Record<string, unknown>
    calls.push({
      // The wire's own id where the endpoint sent one, and **nothing invented
      // where it did not**: the response part this desk sends back carries an
      // id only if the call did, because an id the endpoint never issued is a
      // reference to a call it cannot resolve.
      id: typeof call.id === 'string' ? call.id : '',
      name: call.name,
      args: args !== null && typeof args === 'object' ? args : {},
      argsText: JSON.stringify(call.args ?? {})
    })
  }
  return calls
}

function turnOf(parts: Part[]): ModelTurn {
  return {
    text: textOfParts(parts),
    calls: callsOf(parts),
    // The array that arrived, kept: no map, no filter, no rebuild.
    assistant: { role: 'model', parts },
    reasoning: reasoningOf(parts),
    signatures: signaturesOf(parts)
  }
}

export const gemini: Provider = {
  family: 'gemini',

  path({ model, stream }) {
    // The pair is the relay's one closed query exception and the colon its one
    // closed path exception; both are checked again by the desk's capability
    // before anything leaves the page, so a model name outside the address
    // class is refused there rather than escaped here.
    return stream
      ? `${VERSION}/models/${model}:streamGenerateContent?alt=sse`
      : `${VERSION}/models/${model}:generateContent`
  },

  tools(defs: McpTool[]) {
    // The runtime's own schema, minus the closed removal list this wire's
    // OpenAPI subset requires — `assistant/geminiSchema.ts` owns that list and
    // says what it costs. Nothing is written here.
    return [
      {
        functionDeclarations: defs.map((def) => ({
          name: def.name,
          description: def.description ?? '',
          parameters: servedSchemaFor('gemini', def)
        }))
      }
    ]
  },

  initialMessages(_system: string, user: string) {
    // The system prompt is not a message on this wire: it is
    // `systemInstruction`, and `send` puts it there.
    return [{ role: 'user', parts: [{ text: user }] }]
  },

  async send(options: SendOptions): Promise<ModelTurn> {
    const suffix = gemini.path({ model: options.model, stream: options.stream })
    const body: Record<string, unknown> = {
      contents: options.messages,
      systemInstruction: { parts: [{ text: options.system }] },
      tools: options.tools,
      // **The desk's table's members, merged — and this is where they are
      // placed.** The table names `thinkingConfig`; this wire wants it under
      // `generationConfig`, and putting it there is the provider's business
      // rather than the table's.
      generationConfig: { ...(options.thinking ?? {}) }
    }

    // **Bounded by the run's signal**, and a thunk: a closed run makes no
    // request at all, and a capability that never settled could not hold this
    // loop open.
    const response = await withAbort(
      () =>
        options.call(suffix, {
          headers: protocolHeaders(),
          body: JSON.stringify(body),
          signal: options.signal
        }),
      options.signal
    )
    if (!response.ok) {
      throw new ModelHttpError(
        response.status,
        await withAbort(() => response.text(), options.signal),
        suffix
      )
    }

    const parts: Part[] = []
    if (!isEventStream(response)) {
      const payload = (await withAbort(
        () => response.json(),
        options.signal
      )) as GenerateContentResponse
      for (const part of payload.candidates?.[0]?.content?.parts ?? []) absorb(parts, part)
      return turnOf(parts)
    }

    for await (const data of sseEvents(response, options.signal)) {
      // The stream carries one whole `GenerateContentResponse` per event, and
      // no terminal sentinel: it ends when the body does.
      const chunk = JSON.parse(data) as GenerateContentResponse
      for (const part of chunk.candidates?.[0]?.content?.parts ?? []) absorb(parts, part)
    }
    return turnOf(parts)
  },

  appendTurn(messages, turn, results) {
    // The model's turn goes back exactly as it came.
    messages.push(
      turn.assistant ?? {
        role: 'model',
        parts: turn.calls.map((call) => ({
          functionCall: { name: call.name, args: call.args }
        }))
      }
    )
    messages.push({
      role: 'user',
      parts: results.map((result) => ({
        functionResponse: {
          ...(result.call.id === '' ? {} : { id: result.call.id }),
          name: result.call.name,
          // The wire takes an object, so the runtime's text goes in one. An
          // error is the runtime's own words as well: this provider does not
          // write a second sentence about a call it did not make.
          response: { name: result.call.name, content: result.text }
        }
      }))
    })
  }
}
