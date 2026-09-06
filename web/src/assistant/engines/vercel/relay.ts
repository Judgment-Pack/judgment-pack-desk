/**
 * The one seam between the AI SDK and the desk: a `fetch` the SDK is given and
 * the desk's capability behind it.
 *
 * The SDK's providers are built with a **placeholder origin the adapter never
 * resolves** and this function as their `fetch`. So the SDK composes an
 * absolute URL, as it always does, and what it composes is reduced here to the
 * path suffix `session.model.call` admits — the desk builds the address, adds
 * this chassis' session token, and the relay attaches the model credential on
 * the far side. Nothing in this adapter holds a URL with the desk's origin in
 * it, and no engine code calls `globalThis.fetch`: the conformance session
 * seals it for the duration of every run.
 *
 * Three things happen here that the SDK must not be trusted to do:
 *
 * - **the address is checked**, twice over. Anything that is not a path under
 *   the placeholder origin is refused, and so is a query of any kind. The
 *   desk's capability refuses both again — a suffix rule mirrored from the
 *   chassis — and both layers are tested, because "the SDK would never do
 *   that" is a premise and not a rule;
 * - **the credential is stripped.** `createAnthropic` throws `LoadAPIKeyError`
 *   in a browser with no key (`loadApiKey`, reached from its `getHeaders`), so
 *   the provider is handed a placeholder that never leaves this function. Every
 *   header outside the protocol's own is dropped here, and the desk's
 *   capability drops everything outside its own allow-list again;
 * - **the answer is presented in the framing the SDK asked for.** See below.
 */
import { isEventStream } from '../contract'
import type { EndpointKind } from '../../../config/deskConfig'
import type { ModelCall } from '../../engine'

/**
 * The origin the SDK composes against, and which nothing ever resolves.
 *
 * `.invalid` is reserved by RFC 2606 precisely so a placeholder cannot reach
 * anything: if this ever escaped to a real `fetch` it would fail rather than
 * arrive somewhere. The per-family base below is chosen so that the path the
 * SDK composes **is** the suffix the desk's own providers use — the
 * OpenAI-compatible model posts to `<base>/chat/completions` and the Anthropic
 * model to `<base>/messages` — so both engines address the relay identically
 * and the chassis sees one route.
 */
export const PLACEHOLDER_ORIGIN = 'https://relay.invalid'

/** The base each family's provider is constructed with. */
export function placeholderBase(family: EndpointKind): string {
  // `v1/messages` after the relay's mount point, which is where the chassis'
  // own probe sends an Anthropic request; `chat/completions` for the other.
  return family === 'anthropic' ? `${PLACEHOLDER_ORIGIN}/v1` : PLACEHOLDER_ORIGIN
}

/** The refusal every address this wrapper will not send carries. */
export const ADDRESS_REFUSED =
  'the assistant engine addressed something other than the desk’s relay; the request was not made'

/**
 * The headers a model request may carry out of this adapter.
 *
 * An allow-list rather than a list of credential names to strip, for the reason
 * the chassis gives at length: `X-Auth-Token`, `X-Amz-Security-Token`,
 * `Ocp-Apim-Subscription-Key` and whatever a gateway invents next walk straight
 * through a denylist. `x-api-key` — the placeholder `createAnthropic` insists
 * on — is absent rather than deleted, so there is no second rule to keep in
 * step with this one. `authorization` likewise: `createOpenAICompatible` omits
 * it when it has no key, and this list is why that is a convenience and not the
 * guard.
 */
const PROTOCOL_HEADERS: readonly string[] = [
  'accept',
  'content-type',
  'anthropic-version',
  'anthropic-beta',
  'openai-beta'
]

/**
 * The path suffix, or nothing where this is not an address this desk will send
 * to.
 *
 * **The suffix is what follows the placeholder origin, and the family's base is
 * a second check on top of it.** So the address the desk composes is the same
 * one the built-in engine composes — `chat/completions`, `v1/messages` — and a
 * provider that reached for some other path under the same origin, or for a
 * different origin altogether, is refused rather than sent.
 */
export function suffixOf(url: string, base: string): string | undefined {
  let parsed: URL
  try {
    parsed = new URL(url)
  } catch {
    return undefined
  }
  // A query is refused rather than dropped: the relay's own rule is that the
  // desk's token is the only parameter it will accept, so a provider that
  // appended one (`queryParams`, an api-version member) must fail here rather
  // than have this desk quietly decide what it meant.
  if (parsed.search !== '' || parsed.hash !== '') return undefined
  if (!url.startsWith(`${base}/`)) return undefined
  const suffix = url.slice(PLACEHOLDER_ORIGIN.length + 1)
  return suffix === '' ? undefined : suffix
}

/** Header names and values as a plain record, whatever shape they arrived in. */
function headerRecord(headers: HeadersInit | undefined): Record<string, string> {
  const record: Record<string, string> = {}
  if (headers === undefined) return record
  if (typeof Headers !== 'undefined' && headers instanceof Headers) {
    headers.forEach((value, name) => {
      record[name] = value
    })
    return record
  }
  if (Array.isArray(headers)) {
    for (const [name, value] of headers) record[String(name)] = String(value)
    return record
  }
  for (const [name, value] of Object.entries(headers)) record[name] = String(value)
  return record
}

/**
 * A whole JSON answer, presented in the streaming framing the same protocol
 * defines.
 *
 * **The defect this closes is the SDK's, and it is a real one.** The desk
 * requires an engine to read the answer it *got* rather than the answer it
 * asked for — an endpoint may ignore `stream`, and a gateway that buffers is an
 * ordinary deployment. `streamText` chooses its response handler when it
 * chooses to stream: the answer is fed to an event-source parser whatever came
 * back, so a JSON object yields no events at all and the run ends with
 * `AI_InvalidResponseDataError` on the OpenAI-compatible path and
 * `AI_NoOutputGeneratedError` on the Anthropic one. Both were measured on the
 * shipped release; the desk's conformance session has a leg for each.
 *
 * So the adapter closes it where it can be closed without a second request: the
 * answer is re-framed into the events the same protocol would have sent. This
 * is the only wire knowledge in the adapter, it is confined to answers the
 * endpoint did not stream, and it is exercised by two of the four conformance
 * legs.
 */
export function reframe(family: EndpointKind, payload: unknown): string {
  return family === 'anthropic' ? anthropicEvents(payload) : openAiChunks(payload)
}

interface OpenAiWhole {
  id?: string
  created?: number
  model?: string
  choices?: {
    index?: number
    message?: {
      role?: string
      content?: string | null
      tool_calls?: { id?: string; type?: string; function?: { name?: string; arguments?: string } }[]
    }
    finish_reason?: string | null
  }[]
  usage?: unknown
}

function openAiChunks(payload: unknown): string {
  const whole = (payload ?? {}) as OpenAiWhole
  const head = { id: whole.id, created: whole.created, model: whole.model }
  const lines: string[] = []
  const frame = (choices: unknown[], usage?: unknown) =>
    lines.push(
      `data: ${JSON.stringify({
        ...head,
        object: 'chat.completion.chunk',
        choices,
        ...(usage === undefined ? {} : { usage })
      })}\n\n`
    )
  const choices = whole.choices ?? []
  for (const choice of choices) {
    const message = choice.message ?? {}
    const delta: Record<string, unknown> = {}
    if (message.role !== undefined) delta.role = message.role
    if (message.content !== undefined) delta.content = message.content
    if (message.tool_calls !== undefined) {
      // The one member a chunk carries and a whole message does not: the index
      // that lets deltas be reassembled. It is the position in this array.
      delta.tool_calls = message.tool_calls.map((call, index) => ({ index, ...call }))
    }
    frame([{ index: choice.index ?? 0, delta, finish_reason: null }])
    frame([{ index: choice.index ?? 0, delta: {}, finish_reason: choice.finish_reason ?? 'stop' }])
  }
  if (choices.length === 0) frame([{ index: 0, delta: {}, finish_reason: 'stop' }])
  if (whole.usage !== undefined) frame([], whole.usage)
  lines.push('data: [DONE]\n\n')
  return lines.join('')
}

interface AnthropicWhole {
  content?: { type?: string; text?: string; id?: string; name?: string; input?: unknown }[]
  stop_reason?: string | null
  stop_sequence?: string | null
  usage?: { output_tokens?: number }
}

function anthropicEvents(payload: unknown): string {
  const whole = (payload ?? {}) as AnthropicWhole
  const { content, ...message } = whole as Record<string, unknown> & AnthropicWhole
  const blocks = Array.isArray(content) ? content : []
  const lines: string[] = []
  const event = (name: string, object: Record<string, unknown>) =>
    lines.push(`event: ${name}\ndata: ${JSON.stringify({ type: name, ...object })}\n\n`)

  event('message_start', { message: { ...message, content: [] } })
  blocks.forEach((block, index) => {
    if (block.type === 'tool_use') {
      event('content_block_start', {
        index,
        content_block: { type: 'tool_use', id: block.id, name: block.name, input: {} }
      })
      event('content_block_delta', {
        index,
        delta: { type: 'input_json_delta', partial_json: JSON.stringify(block.input ?? {}) }
      })
    } else {
      event('content_block_start', { index, content_block: { type: 'text', text: '' } })
      event('content_block_delta', {
        index,
        delta: { type: 'text_delta', text: block.text ?? '' }
      })
    }
    event('content_block_stop', { index })
  })
  event('message_delta', {
    delta: { stop_reason: whole.stop_reason ?? null, stop_sequence: whole.stop_sequence ?? null },
    // Required by the protocol on this event, and by the SDK's own schema.
    usage: { output_tokens: whole.usage?.output_tokens ?? 0 }
  })
  event('message_stop', {})
  return lines.join('')
}

/**
 * The `fetch` the SDK's providers are given, and the whole of an engine's reach
 * to a model.
 *
 * `call` is captured when the adapter is built rather than read at call time,
 * for the same reason the desk's own capability captures `fetch`: the
 * conformance session replaces every network global with a throwing sentinel
 * for the duration of a run, and this must still work while an engine that
 * reached for one does not.
 */
export function relayFetch(options: {
  family: EndpointKind
  call: ModelCall
  signal?: AbortSignal
}): typeof fetch {
  const base = placeholderBase(options.family)
  const send = async (input: unknown, init?: RequestInit): Promise<Response> => {
    const suffix = typeof input === 'string' ? suffixOf(input, base) : undefined
    if (suffix === undefined) throw new Error(ADDRESS_REFUSED)
    const headers: Record<string, string> = {}
    for (const [name, value] of Object.entries(headerRecord(init?.headers))) {
      if (PROTOCOL_HEADERS.includes(name.toLowerCase())) headers[name] = value
    }
    const body = init?.body
    if (typeof body !== 'string') {
      throw new Error('a model request body must be the JSON text the SDK composed')
    }
    const answered = await options.call(suffix, {
      headers,
      body,
      // The run's signal, not the SDK's: an abort has to reach the request the
      // desk actually made, and the SDK's own is passed through where it set one.
      signal: (init?.signal ?? options.signal) ?? undefined
    })
    // The SDK asked to stream and the endpoint answered whole. See `reframe`.
    if (!answered.ok || isEventStream(answered) || !asksToStream(body)) return answered
    const payload: unknown = await answered.json().catch(() => undefined)
    if (payload === undefined) return answered
    return new Response(reframe(options.family, payload), {
      status: answered.status,
      statusText: answered.statusText,
      headers: { 'content-type': 'text/event-stream' }
    })
  }
  return send as unknown as typeof fetch
}

/** Whether the request the SDK composed asked the endpoint to stream. */
function asksToStream(body: string): boolean {
  try {
    return (JSON.parse(body) as { stream?: unknown }).stream === true
  } catch {
    return false
  }
}
