/**
 * The engine contract, as ADR-0001 writes it.
 *
 * An engine is a module the page loads for one session. It receives everything
 * the desk has already decided — the runtime's prompt, the runtime's own tool
 * definitions, a bound `callTool`, the relay's base URL, the tier, an abort
 * signal — and returns a stream of events. Accept and Reject are the desk's
 * actions on the proposal, not engine calls.
 *
 * **Deliberately smaller than any framework's API.** What a framework offers
 * beyond this — its own approval pause, its own memory, its own file tools —
 * stays inside its adapter or is not used. That is the whole point of the slot:
 * the desk's promises are held below the loop, so swapping the loop changes
 * none of them.
 *
 * **`Engine`, not `AssistantEngine`.** The ADR writes the interface as
 * `AssistantEngine`, and this page already spells `AssistantEngine` as the
 * *identifier* a `desk.json` may name (`config/deskConfig.ts`). One name with
 * two meanings in one page is worse than a deviation recorded here: the id
 * keeps the name it has in configuration, and the adapter is `Engine`.
 *
 * **The session carries capabilities and never addresses.** `callTool` is a
 * function with no other member, so an engine cannot reach the transport the
 * ToolGate sits on, cannot open a second one, and cannot call a tool by any
 * route the gate does not see. `model.call` is the same shape for model
 * traffic. The member set is asserted whole in
 * `assistant/enforcement.test.ts`, because the guarantee is "there is nothing
 * else here" rather than "the obvious escape hatch is absent".
 *
 * **`model` is a capability, not a base URL — a deliberate deviation from
 * ADR-0001's contract sketch**, which wrote
 * `model: { family, baseUrl, model }` with `baseUrl` "the chassis relay". The
 * relay authenticates with this chassis' session token in the query, so a
 * `baseUrl` an engine can read is **this desk's credential in the engine's
 * hands** — and an adapter holding it can open `/ws?token=…` itself with
 * `globalThis.WebSocket` and drive a third, ungated MCP connection. Nothing in
 * the contract would have been violated; the guarantee would simply have been
 * gone. So the engine is handed no URL and no token: `model.call(suffix, init)`
 * is a capability the desk binds, which builds the address itself, admits only
 * a validated path suffix, carries no header outside the relay's own
 * allow-list, and is the only way an engine reaches a model at all.
 *
 * The conformance session holds that structurally rather than by inspection:
 * every leg runs with `fetch`, `WebSocket`, `XMLHttpRequest` and `EventSource`
 * replaced by throwing sentinels for the duration of the engine's run, so an
 * engine that reaches for any of them fails the leg. When the `vercel` adapter
 * lands, this capability is what is passed as the provider's `fetch` option,
 * so the shape survives the next chunk.
 */
import type { AssistantEngine, EndpointKind, ThinkingTier } from '../config/deskConfig'

/** One tool exactly as `tools/list` served it. Nothing is re-declared. */
export interface McpTool {
  name: string
  description?: string
  inputSchema?: unknown
}

/** One `tools/call` answer, in the shape the wire carries it. */
export interface McpToolResult {
  content?: { type?: string; text?: string }[]
  isError?: boolean
  structuredContent?: unknown
}

/**
 * The only way an engine reaches the runtime.
 *
 * A function and not an object: an object could grow a member, and the member
 * it would grow is the one that skips the gate.
 */
export type CallTool = (name: string, args: Record<string, unknown>) => Promise<McpToolResult>

/**
 * What an engine may put in one model request, and the whole of it.
 *
 * A body, a signal, and headers the **protocol** needs. There is no URL member
 * because the address is the desk's, no method because every call on both wire
 * formats is a POST, and no credential because the page holds none.
 */
export interface ModelRequest {
  /** Protocol headers only. Anything outside the relay's allow-list is dropped. */
  headers?: Record<string, string>
  body: string
  signal?: AbortSignal
}

/**
 * The only way an engine reaches a model.
 *
 * `suffix` is a path suffix — `chat/completions`, `v1/messages` — held to the
 * relay's own segment rule. It is not a URL and it may not carry a query: the
 * desk builds the address, attaches this chassis' session token, and the relay
 * attaches the model credential on the far side.
 */
export type ModelCall = (suffix: string, request: ModelRequest) => Promise<Response>

export interface AssistantSession {
  /** The runtime's prompt text, from `prompts/get`. */
  prompt: string
  /** The allow-listed tools, exactly as `tools/list` served them. */
  tools: McpTool[]
  /** Bound through the ToolGate. */
  callTool: CallTool
  /** A capability and a name. No address, and no credential. */
  model: { family: EndpointKind; model: string; call: ModelCall }
  thinking: { tier: ThinkingTier }
  signal: AbortSignal
}

/**
 * What an engine may say. The ADR's union, verbatim.
 *
 * `guardrail` is on it because the pane renders one stream; the events
 * themselves are emitted by the **desk's** gate rather than by an engine, and
 * the runner interleaves them. An engine that emitted its own would be
 * reporting on a guard it does not hold.
 */
export type AssistantEvent =
  | { type: 'reasoning'; text: string; done: boolean }
  | { type: 'tool_call'; name: string; args: unknown }
  | { type: 'tool_result'; name: string; isError: boolean; text: string; structured?: unknown }
  | { type: 'guardrail'; tool: string; action: 'rewrote' | 'refused'; detail: string }
  | { type: 'thinking_unavailable'; detail: string }
  | { type: 'critique'; refuted: boolean; checks: { tool: string; status: string }[]; text: string }
  | { type: 'proposal'; document: unknown; unknowns: string[]; critique?: { refuted: boolean } }
  | { type: 'error'; message: string }
  | { type: 'end' }

export interface Engine {
  readonly id: AssistantEngine
  start(session: AssistantSession): AsyncIterable<AssistantEvent>
}
