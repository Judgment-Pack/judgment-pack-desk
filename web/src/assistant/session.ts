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
export function suffixProblem(suffix: string): string {
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
 * A model call this session may make, bound by the desk.
 *
 * Three things happen here that an engine must not be trusted to do:
 *
 * - **the address is built here**, out of the mount point and a suffix this
 *   function validated, with the session token attached by `chassisUrl`. The
 *   engine never sees a URL and never sees the token, so an adapter cannot
 *   read this chassis' credential out of its own configuration and open a
 *   second, ungated socket with it;
 * - **the headers are an allow-list**, so nothing resembling a credential
 *   travels even as far as this desk's own route;
 * - **`fetch` is captured when the session is bound**, not read at call time,
 *   so the conformance session can replace every network global with a
 *   throwing sentinel for the duration of an engine's run. An engine that
 *   reaches for one fails the leg; this call still works.
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
    return send(chassisUrl(`${RELAY_PREFIX}/${suffix}`), {
      method: 'POST',
      headers,
      body: request.body,
      signal: request.signal
    })
  }
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

export interface AssistantConnection {
  /** The allow-listed tools, exactly as `tools/list` served them. */
  tools: McpTool[]
  /** Bound through the gate. The only thing an engine is handed. */
  callTool: CallTool
  close(): Promise<void>
}

/**
 * Open the assistant's connection, gated.
 *
 * `transport` is injectable so a test — the conformance session included — can
 * drive the whole path, gate and client and all, without a socket. Nothing in
 * the page passes it.
 */
export async function openAssistantConnection(options: {
  allowed: readonly string[]
  onEvent: (event: AssistantEvent) => void
  transport?: Transport
}): Promise<AssistantConnection> {
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
  await client.connect(gated)
  const listed = (await client.listTools()).tools as McpTool[]
  // The tools the model is offered are the ones the runtime served, filtered
  // to what this desk's file granted — never a definition written here.
  const tools = listed.filter((tool) => allowed.has(tool.name))
  return {
    tools,
    // The SDK's own result type is wider than the contract's — it carries the
    // task and meta members this desk never reads — so it is narrowed here,
    // once, rather than at each engine.
    callTool: async (name, args) =>
      (await client.callTool({ name, arguments: args })) as McpToolResult,
    close: () => client.close()
  }
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
