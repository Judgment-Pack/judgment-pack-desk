/**
 * The two wire formats, behind one shape.
 *
 * Ported from the bake-off's `none` prototype (the control candidate ADR-0001
 * measured, and the one this engine is), minus the thinking tier: chunk 2b
 * runs `off` and reports the two other tiers as unavailable, so nothing here
 * reads or writes a reasoning field yet. The `assistant` member below is kept
 * anyway, and that is deliberate — see its comment.
 */

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
   * Nothing in this chunk needs it: with the tier off there are no thinking
   * blocks to preserve. It is here because it is the property the bake-off
   * credited this engine with — Anthropic's rule is that thinking blocks come
   * back "complete and unmodified", and filtering by block type silently drops
   * `redacted_thinking` — and a shape that has to be reintroduced later is a
   * shape that gets reintroduced wrongly. Keeping it costs one member.
   */
  assistant: unknown
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
  /** The chassis relay's base. The engine appends a path suffix and nothing else. */
  base: string
  model: string
  system: string
  messages: unknown[]
  tools: unknown[]
  /** Whether the request asks for a stream. What comes back decides how it is read. */
  stream: boolean
  signal?: AbortSignal
}

export interface Provider {
  readonly family: 'openai-compatible' | 'anthropic'
  /** The path this protocol appends to the relay base. */
  readonly suffix: string
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
 * Where one request goes: the relay's base with one path suffix on it, and
 * **not one parameter of the page's own**.
 *
 * The desk hands the engine a base that already carries this chassis' session
 * token, and the relay refuses a query carrying anything else — any name, any
 * case, any encoding — with `assistant-relay-path`. That rule came out of four
 * review rounds and is structural: no comparison the desk can write is the
 * comparison every parser downstream makes, so the page's query is refused
 * rather than filtered.
 *
 * So the suffix is appended to the base's **path** and the base's query is
 * carried across untouched. An engine that built `${base}/${suffix}` by
 * concatenation would put the suffix after the query and address a route that
 * does not exist; an engine that added `?stream=true` would have every request
 * refused. Both wire formats put `stream` in the body, so neither needs one.
 */
export function relayRequestUrl(base: string, suffix: string): string {
  const url = new URL(base, 'http://desk.invalid')
  url.pathname = `${url.pathname.replace(/\/+$/, '')}/${suffix.replace(/^\/+/, '')}`
  // Relative in, relative out: the page's own origin is where this goes, and
  // writing an absolute URL for a same-origin request invents a host.
  return /^[a-z][a-z0-9+.-]*:/i.test(base) || base.startsWith('//')
    ? url.toString()
    : `${url.pathname}${url.search}`
}

/**
 * The headers one model request carries, and the whole of them.
 *
 * **No credential, of any name.** The page holds none: the chassis relay keeps
 * the key on this machine, strips whatever it is sent, and attaches the
 * configured one on the way out. A header here would be a credential this page
 * had to have obtained, which is the single thing the relay exists to prevent.
 * `web/src/assistant/engines/builtin/engine.test.ts` asserts the outgoing
 * request carries no `authorization`, no `x-api-key` and no `cookie`.
 */
export function relayHeaders(extra: Record<string, string> = {}): Record<string, string> {
  return { 'content-type': 'application/json', ...extra }
}
