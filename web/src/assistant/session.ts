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
  McpToolResult
} from './engine'

/**
 * The relay base the engine is handed.
 *
 * It carries this chassis' session token and nothing else, because the relay
 * refuses a query with any other pair in it — any name, any case, any encoding
 * — and the engine appends a path suffix to it without adding a parameter of
 * its own. The token is the desk's, so it is the desk that puts it here: an
 * engine that had to reach for the session token would be an engine holding a
 * credential.
 */
export function relayBaseUrl(): string {
  return chassisUrl('/api/assistant/relay/v1')
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
