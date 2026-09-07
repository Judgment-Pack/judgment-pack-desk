/**
 * The two wire formats, behind one shape.
 *
 * Ported from the bake-off's `none` prototype (the control candidate ADR-0001
 * measured, and the one this engine is), thinking included: each provider
 * merges the **desk's** wire members into its body, reads reasoning back, and
 * echoes the assistant turn as received.
 */

import type { EndpointKind } from '../../../../config/deskConfig'
import type { ModelCall } from '../../../engine'

/** One tool call the model asked for. */
export interface ToolCall {
  id: string
  name: string
  /** Normalised to an object at the adapter boundary, so the loop sees one shape. */
  args: Record<string, unknown>
  /** The exact JSON text the endpoint sent, so the turn can be echoed verbatim. */
  argsText: string
}

export interface ModelTurn {
  text: string
  calls: ToolCall[]
  /**
   * The assistant turn **exactly as the endpoint sent it**, echoed back
   * unchanged on the next request rather than rebuilt from a typed model.
   *
   * This is the property the bake-off credited this engine with, and thinking
   * is what it is for: Anthropic's rule is that thinking blocks come back
   * "complete and unmodified", and filtering by block type silently drops
   * `redacted_thinking`. Nothing below maps, filters or rebuilds a block.
   */
  assistant: unknown
  /**
   * The reasoning passages this turn carried, in the endpoint's own order.
   *
   * One entry per passage rather than per delta: the contract's `done` marks a
   * passage finished, and a reader wants the passage. Empty where the endpoint
   * sent none — which is one of the two signals the desk's thinking slot reads
   * to decide that an endpoint has no thinking at all.
   */
  reasoning: string[]
  /**
   * Whether this turn **reasoned**, which is not the same as whether it produced
   * a passage a person can read.
   *
   * On two wires those are the same thing and this is `reasoning.length > 0`. On
   * the Gemini wire they are not: a thought part may carry a signature and no
   * text — the summary was empty, or was not streamed — and a desk that counted
   * only readable passages would watch a model think through every turn at tier
   * `off` and never conclude that it always thinks. The tier's own rule reads
   * this; the tab reads `reasoning`.
   */
  reasoned: boolean
  /**
   * Every thinking signature this turn carried, whole.
   *
   * Anthropic and Gemini — the two wires that sign a model's reasoning — and
   * reassembled here on both: an Anthropic signature split across two
   * `signature_delta` events is one signature, and a Gemini `thoughtSignature`
   * arrives on the last chunk of the thought part it belongs to. The desk
   * concatenates or carries whole as each wire requires rather than keeping the
   * last fragment. It is recorded so that what goes back out can be compared
   * with what came in.
   */
  signatures: string[]
}

/**
 * A non-2xx from the relay or the endpoint behind it, with the body kept.
 *
 * The status and the body are both carried because "this endpoint has no
 * thinking", "this desk has no key stored" and "this endpoint is broken" are
 * different answers, and they are indistinguishable once the body is thrown
 * away. The desk's own refusals arrive here too, in the chassis' `{error,
 * code}` envelope.
 */
export class ModelHttpError extends Error {
  readonly status: number
  readonly bodyText: string
  readonly route: string

  constructor(status: number, bodyText: string, route: string) {
    super(`${route} answered ${status}: ${bodyText.slice(0, 300)}`)
    this.name = 'ModelHttpError'
    this.status = status
    this.bodyText = bodyText
    this.route = route
  }

  /** `error.message` out of the body, where the answer had the usual shape. */
  get endpointMessage(): string {
    try {
      const parsed = JSON.parse(this.bodyText) as { error?: { message?: string }; message?: string }
      const message = parsed?.error?.message ?? parsed?.message
      if (typeof message === 'string') return message
    } catch {
      /* not JSON — fall through to the raw text */
    }
    return this.bodyText.slice(0, 200)
  }
}

export interface SendOptions {
  /**
   * The desk's model capability, and the engine's only reach to a model.
   *
   * Not a URL: the engine is handed no address and no credential, so it can
   * neither point a request somewhere else nor read this chassis' session
   * token out of its own configuration. See `assistant/engine.ts`.
   */
  call: ModelCall
  model: string
  system: string
  messages: unknown[]
  tools: unknown[]
  /** Whether the request asks for a stream. What comes back decides how it is read. */
  stream: boolean
  /**
   * The tier's wire members, from the desk's own table, or null.
   *
   * **Merged, never chosen.** `assistant/thinking.ts` owns the mapping from
   * `(family, tier)` to members and owns the fallback between the two Anthropic
   * spellings; a provider puts what it is given on the request and has no
   * opinion about it.
   */
  thinking?: Record<string, unknown> | null
  /**
   * The run's own signal, and **required**.
   *
   * Every await below it — the request, the body read, each read of the stream
   * — is bounded by this. An optional signal is an await nobody bounded.
   */
  signal: AbortSignal
}

export interface Provider {
  readonly family: EndpointKind
  /**
   * The path this protocol appends to the relay base, for one request.
   *
   * **A function and no longer a constant, because one wire's address depends
   * on the request.** Two protocols post to a fixed path and carry the model
   * name and the streaming choice in the body; the native Gemini wire puts both
   * in the address — `v1beta/models/<model>:streamGenerateContent?alt=sse`
   * against `:generateContent` — so a constant could not describe it. What is
   * returned is a **suffix**, held to the desk's mirror of the chassis' own
   * rule before anything is sent: a model name outside that class is refused
   * rather than escaped.
   */
  path(request: { model: string; stream: boolean }): string
  tools(defs: { name: string; description?: string; inputSchema?: unknown }[]): unknown[]
  initialMessages(system: string, user: string): unknown[]
  send(options: SendOptions): Promise<ModelTurn>
  /**
   * Push the assistant turn and its results onto the message array. The turn is
   * passed whole rather than only its calls, so the assistant message can be
   * echoed as received.
   */
  appendTurn(
    messages: unknown[],
    turn: ModelTurn,
    results: { call: ToolCall; text: string; isError: boolean }[]
  ): void
}

/**
 * The headers one model request carries, and the whole of them.
 *
 * **No credential, of any name.** The page holds none: the chassis relay keeps
 * the key on this machine, strips whatever it is sent, and attaches the
 * configured one on the way out. A header here would be a credential this page
 * had to have obtained, which is the single thing the relay exists to prevent.
 * The desk's capability drops anything outside its allow-list before the
 * request leaves the engine, so this is a convenience and not the guard.
 */
export function protocolHeaders(extra: Record<string, string> = {}): Record<string, string> {
  return { 'content-type': 'application/json', ...extra }
}
