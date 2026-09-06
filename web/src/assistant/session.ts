/**
 * One assistant session: its own connection, its own gate, one event stream.
 *
 * **The assistant gets its own MCP connection, and this is where that is
 * decided.** The desk's one client serves the page's own calls — `list_packs`,
 * `get_pack`, the what-if pane's `experimental_evaluate` — and every one of
 * them is outside the assistant's allow-list, so the gate cannot sit on that
 * transport without breaking the desk. A second `DeskWebSocketTransport` is
 * opened instead, wrapped by the ToolGate, with its own `Client`; the chassis
 * spawns one more `jpack mcp` behind it, and it lives exactly as long as the
 * session. That is the price of a guarantee held at the wire rather than at a
 * call site, and it is in the README.
 *
 * **The guardrail events are the desk's, not an engine's.** The gate reports
 * what it did, and this module turns each notice into the ADR's `guardrail`
 * event on the same ordered sink the engine's events go to — so a refusal
 * lands between the `tool_call` that provoked it and the `tool_result` the
 * engine got back, which is the order a reader needs to make sense of either.
 */
import { Client } from '@modelcontextprotocol/sdk/client/index.js'
import type { Transport } from '@modelcontextprotocol/sdk/shared/transport.js'
import { chassisUrl } from '../files/client'
import { DeskWebSocketTransport } from '../mcp/transport'
import { sessionToken, socketURL } from '../mcp/McpProvider'
import { allowedTools, gateTransport, type GuardrailNotice } from './toolGate'
import type {
  AssistantEvent,
  AssistantSession,
  CallTool,
  Engine,
  McpTool,
  McpToolResult,
  ModelCall,
  ModelRequest
} from './engine'

/**
 * The relay's mount point. The `v1` belongs to this route, not to any endpoint.
 */
const RELAY_PREFIX = '/api/assistant/relay/v1'

/**
 * The headers a model request may carry, mirrored from the chassis' own
 * outbound allow-list (`relayedRequestHeaders` in `internal/desk/modelrelay.go`).
 *
 * **An allow-list, for the reason the chassis gives at length**: "every inbound
 * credential is stripped" cannot be held by a list of names somebody thought of
 * — `X-Auth-Token`, `X-Amz-Security-Token`, `Ocp-Apim-Subscription-Key` and
 * whatever a gateway invents next walk straight through a denylist. This is the
 * same claim one layer earlier, so that an engine's header never reaches even
 * this desk's own route unless the protocol needs it. `Authorization`,
 * `X-Api-Key` and `Cookie` are absent rather than deleted: there is no second
 * rule to keep in step with this one.
 */
const MODEL_REQUEST_HEADERS: readonly string[] = [
  'accept',
  'accept-language',
  'content-type',
  'anthropic-version',
  'anthropic-beta',
  'openai-beta',
  'openai-organization',
  'openai-project'
]

/** Bounds the suffix, as `maxRelaySuffix` does on the chassis side. */
const MAX_SUFFIX = 256

/**
 * The path suffix rule, mirrored from `relaySuffixProblem`.
 *
 * One or more segments of `[A-Za-z0-9._-]`: no dot segment, no empty segment,
 * no percent sign, no backslash, no query. It is checked **here** as well as on
 * the chassis because the point of the capability is that the engine cannot
 * address anything the desk did not agree to — a refusal that only happened on
 * the far side would be a refusal after the request left the page.
 */
export function suffixProblem(suffix: unknown): string {
  // **A primitive string, or nothing.** The signature said `string` and the
  // runtime did not check: a string-like object can answer an innocuous
  // `length` and `split()` while this function is looking and a different
  // `toString()` when the URL is built, which turns this capability into an
  // authenticated POST to another same-origin chassis route. A boxed String, a
  // proxy, a `Symbol.toPrimitive` and a `Request` are all this shape. The type
  // is not what runs.
  if (typeof suffix !== 'string') {
    return `a model call's path must be a string; this one is a ${typeof suffix}`
  }
  if (suffix === '') return `a model call must name at least one path segment after ${RELAY_PREFIX}`
  if (suffix.length > MAX_SUFFIX) {
    return `a model call's path is at most ${MAX_SUFFIX} bytes; this one is ${suffix.length}`
  }
  for (const segment of suffix.split('/')) {
    if (segment === '') return 'a model call\'s path may not carry an empty segment'
    if (segment === '.' || segment === '..') {
      return 'a model call\'s path may not carry a dot segment'
    }
    if (!/^[A-Za-z0-9._-]+$/.test(segment)) {
      return (
        `a model call's path segments are letters, digits, ".", "_" and "-"; ` +
        `${JSON.stringify(segment)} is not one`
      )
    }
  }
  return ''
}

/**
 * The answer headers an engine may read, and the whole of them.
 *
 * Both wire formats need one thing from an answer's headers — whether it is an
 * event stream — and the byte count is worth carrying beside it. Everything
 * else is dropped, on the same allow-list reasoning the request side uses: a
 * header nobody has heard of does not reach the engine because it is not on
 * this list. The chassis already strips credential headers from an answer; this
 * is the same claim one layer in.
 */
const MODEL_ANSWER_HEADERS: readonly string[] = ['content-type', 'content-length']

/**
 * The sentence every failed model call carries, whatever went wrong.
 *
 * Fixed text, because a browser's own `TypeError` for a failed fetch **quotes
 * the URL** — which is the relay address with this chassis' session token in
 * it. An engine that caught the error and read `.message` would have the token.
 */
const CALL_FAILED =
  'the model request could not be made; the desk holds the address and the reason is not the ' +
  "engine's to read"

/**
 * A model call this session may make, bound by the desk.
 *
 * Four things happen here that an engine must not be trusted to do:
 *
 * - **the address is built here**, out of the mount point and a suffix this
 *   function validated, with the session token attached by `chassisUrl`;
 * - **the answer is a facade**, constructed by this desk. A browser `Response`
 *   carries the requested URL on `.url`, so returning the one `fetch` produced
 *   handed the engine the token-bearing relay address and, from it, everything
 *   needed to open `/ws?token=…` on a connection no gate is on. A constructed
 *   `Response` has an empty `url`, no `redirected` history and only the headers
 *   this desk copied onto it. The same reasoning covers the failure path: the
 *   error is replaced, because a fetch `TypeError` quotes the URL;
 * - **the headers are an allow-list**, in both directions, so nothing
 *   resembling a credential travels even as far as this desk's own route and
 *   nothing but the protocol's own comes back;
 * - **`fetch` is captured when the session is bound**, not read at call time,
 *   so the conformance session can replace every network global with a
 *   throwing sentinel for the whole of an engine's leg. An engine that reaches
 *   for one fails; this call still works.
 */
export function bindModelCall(): ModelCall {
  const send = globalThis.fetch.bind(globalThis)
  return async (suffix: string, request: ModelRequest): Promise<Response> => {
    const problem = suffixProblem(suffix)
    if (problem !== '') throw new Error(problem)
    const headers: Record<string, string> = {}
    for (const [name, value] of Object.entries(request.headers ?? {})) {
      if (MODEL_REQUEST_HEADERS.includes(name.toLowerCase())) headers[name] = value
    }
    let answered: Response
    try {
      answered = await send(chassisUrl(`${RELAY_PREFIX}/${suffix}`), {
        method: 'POST',
        headers,
        body: request.body,
        signal: request.signal
      })
    } catch (cause) {
      // An abort is the caller's own signal and is reported as itself, so a
      // loop can tell "stopped" from "failed". It names no URL either.
      if ((cause as Error)?.name === 'AbortError') throw cause
      throw new Error(CALL_FAILED)
    }
    return facade(answered)
  }
}

/**
 * The answer the engine gets: this desk's, built from the endpoint's.
 *
 * A body with no `url`, no `redirected`, and a filtered header copy. The status
 * and the reason phrase travel because a loop has to be able to tell a refusal
 * from an answer.
 */
function facade(answered: Response): Response {
  const carried = new Headers()
  answered.headers.forEach((value, name) => {
    if (MODEL_ANSWER_HEADERS.includes(name.toLowerCase())) carried.set(name, value)
  })
  // A body is not allowed on these statuses, and the constructor throws rather
  // than ignoring one.
  const empty = answered.status === 204 || answered.status === 205 || answered.status === 304
  return new Response(empty ? null : answered.body, {
    status: answered.status,
    statusText: answered.statusText,
    headers: carried
  })
}

/**
 * The transport this session runs on: **a new one, every time**.
 *
 * Not a shared or memoised one. Two sessions sharing a transport would share a
 * `jpack mcp` and a gate, and closing either would take the other's connection
 * with it; sharing the *desk's* would put the gate in front of the page's own
 * calls. The guarantee is one connection per session, and it is held by this
 * function returning a fresh object.
 */
export function assistantTransport(): Transport {
  return new DeskWebSocketTransport(socketURL(sessionToken()))
}

/** What a connection is once it has finished setting itself up. */
export interface AssistantConnectionReady {
  /** The allow-listed tools, exactly as `tools/list` served them. */
  tools: McpTool[]
  /** Bound through the gate. The only thing an engine is handed. */
  callTool: CallTool
}

/**
 * A connection, **owned from the instant it is asked for**.
 *
 * The handle is returned synchronously and `ready` settles later, which is the
 * whole point: `close()` exists before `initialize` has been answered, so a
 * caller can release a connection whose setup is still in flight. The shape
 * this replaces returned a promise, and a caller could not store anything until
 * it resolved — so a `tools/list` that hung or rejected left a socket open, a
 * `jpack mcp` running, and nothing anywhere holding a reference to either.
 */
export interface AssistantConnection {
  ready: Promise<AssistantConnectionReady>
  /** Idempotent, and safe at any point in the setup. */
  close(): Promise<void>
}

/**
 * Open the assistant's connection, gated.
 *
 * `transport` is injectable so a test — the conformance session included — can
 * drive the whole path, gate and client and all, without a socket. Nothing in
 * the page passes it. `signal` is the run's; an abort closes whatever exists
 * and rejects `ready`, rather than leaving a setup running against a session
 * nobody is watching any more.
 */
export function openAssistantConnection(options: {
  allowed: readonly string[]
  onEvent: (event: AssistantEvent) => void
  transport?: Transport
  signal?: AbortSignal
}): AssistantConnection {
  const allowed = allowedTools(options.allowed)
  const raw = options.transport ?? assistantTransport()
  const notify = (notice: GuardrailNotice) =>
    options.onEvent({
      type: 'guardrail',
      tool: notice.tool,
      action: notice.action,
      detail: notice.detail
    })
  const gated = gateTransport(raw, { allowed, onGuardrail: notify })
  const client = new Client({ name: 'judgment-pack-desk-assistant', version: '0.1.0' }, { capabilities: {} })

  let connected = false
  let shutting: Promise<void> | null = null
  /**
   * Close once, and never reject.
   *
   * The client's own `close` reaches the transport it was connected with, so
   * once `connect` has returned that is the whole of it. Before then the client
   * has adopted nothing this can rely on — and a transport that was started and
   * never adopted is exactly the socket this finding was about — so the raw one
   * is closed directly. Either way a real transport's `onclose` reaches the
   * client, which is what makes a hung `initialize` reject rather than hang on.
   *
   * Never rejects: a caller closing a connection has nothing left to do about a
   * failure to close it.
   */
  const close = (): Promise<void> => {
    shutting ??= (async () => {
      try {
        await (connected ? client.close() : raw.close())
      } catch {
        /* the socket is going away regardless */
      }
    })()
    return shutting
  }

  const aborted = () => {
    const error = new Error('the assistant connection was closed before it was ready')
    error.name = 'AbortError'
    return error
  }

  const ready = (async (): Promise<AssistantConnectionReady> => {
    // An abort at any point closes what exists **and settles this promise**.
    // Closing alone is not enough: a transport that answers nothing answers a
    // close with nothing either, and `ready` would hang for the life of the
    // page with the caller still waiting on it.
    let onAbort = () => {}
    const stopped = new Promise<never>((_resolve, reject) => {
      onAbort = () => {
        void close()
        reject(aborted())
      }
    })
    // Nobody awaits `stopped` unless the race below does; without this an abort
    // on a completed setup would be an unhandled rejection.
    void stopped.catch(() => undefined)
    options.signal?.addEventListener('abort', onAbort, { once: true })
    try {
      if (options.signal?.aborted) throw aborted()
      await Promise.race([client.connect(gated), stopped])
      connected = true
      if (options.signal?.aborted) throw aborted()
      const listed = ((await Promise.race([client.listTools(), stopped])).tools ?? []) as McpTool[]
      if (options.signal?.aborted) throw aborted()
      // The tools the model is offered are the ones the runtime served,
      // filtered to what this desk's file granted — never a definition written
      // here.
      const tools = listed.filter((tool) => allowed.has(tool.name))
      return {
        tools,
        // The SDK's own result type is wider than the contract's — it carries
        // the task and meta members this desk never reads — so it is narrowed
        // here, once, rather than at each engine.
        callTool: async (name, args) =>
          (await client.callTool({ name, arguments: args })) as McpToolResult
      }
    } catch (cause) {
      // **Nothing is left open on a failure.** A `tools/list` that answered
      // with a JSON-RPC error used to throw straight out of here, past a
      // connected client nobody held a reference to.
      await close()
      throw cause
    } finally {
      options.signal?.removeEventListener('abort', onAbort)
    }
  })()
  // A `ready` nobody awaits — an abort during setup — must not be an unhandled
  // rejection. The caller still gets the rejection when it awaits.
  void ready.catch(() => undefined)

  return { ready, close }
}

/**
 * Drive one engine, and put its events on the sink in the order they happen.
 *
 * The loop is `for await` and nothing else: an engine that never ends is
 * bounded by its own `maxTurns` and by the session's signal, and this does not
 * second-guess either.
 */
export async function runAssistantSession(
  engine: Engine,
  session: AssistantSession,
  onEvent: (event: AssistantEvent) => void
): Promise<void> {
  for await (const event of engine.start(session)) onEvent(event)
}
