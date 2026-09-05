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
 * **The session carries `callTool` and never a client.** The type is a
 * function with no other member, so an engine cannot reach the transport the
 * ToolGate sits on, cannot open a second one, and cannot call a tool by any
 * route the gate does not see. `assistant/enforcement.test.ts` asserts the
 * member set whole, because the guarantee is "there is nothing else here"
 * rather than "the obvious escape hatch is absent".
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

export interface AssistantSession {
  /** The runtime's prompt text, from `prompts/get`. */
  prompt: string
  /** The allow-listed tools, exactly as `tools/list` served them. */
  tools: McpTool[]
  /** Bound through the ToolGate. */
  callTool: CallTool
  /** `baseUrl` is the chassis relay, never a vendor. */
  model: { family: EndpointKind; baseUrl: string; model: string }
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
