/**
 * The ToolGate: the desk's promises about what the assistant may reach, held
 * **on the wire** rather than anywhere an engine could be wrong.
 *
 * ADR-0001 puts this below the engine slot deliberately. Every outbound
 * `tools/call` frame is checked by name against the session's allow-list and
 * refused otherwise; `experimental_evaluate` is rewritten to carry
 * `rehearsal: true` before the frame leaves the page. A loop that asked for a
 * tool it was never offered, an SDK retry, a second caller nobody has written
 * yet — each is stopped at the same place, because the place is the transport
 * and not a branch in whatever is running above it.
 *
 * **This is a `Transport`, not a wrapper around `callTool`.** The bake-off
 * measured both layers and reported that only this one survives the loop above
 * it being wrong: `Client`'s public surface offers no outbound middleware over
 * a WebSocket (`setRequestHandler` serves inbound requests; the fetch
 * middleware is for HTTP transports), so a gate on the call site is a gate one
 * `client.callTool` away from being bypassed. There is exactly one door and it
 * is `send`.
 *
 * **The allow-list is the configured subset, intersected with the five.** The
 * desk's closed list (`ASSISTANT_TOOLS`) is the ceiling and `endpoint.tools`
 * is what this desk's file granted; a name in neither cannot be reached by
 * writing a longer list somewhere else.
 *
 * **Order is allow-list, then rewrite**, and it matters in one case only:
 * `experimental_evaluate` that the file did not grant is *refused*, not
 * rewritten and sent. Rewriting first would turn a tool nobody granted into a
 * tool this desk politely corrected on its way out.
 *
 * `inspectOutboundFrame` is exported on its own so a test can call it with a
 * frame and no socket — the prototype's `assertOutboundFrame`, kept as a pure
 * function for exactly that reason.
 */
import type { Transport } from '@modelcontextprotocol/sdk/shared/transport.js'
import type { JSONRPCMessage } from '@modelcontextprotocol/sdk/types.js'
import { ASSISTANT_TOOLS } from '../config/deskConfig'

/** The one tool whose arguments this gate rewrites, and the member it writes. */
export const REHEARSAL_TOOL = 'experimental_evaluate'
export const REHEARSAL_MEMBER = 'rehearsal'

/**
 * A call the gate would not let out of the page.
 *
 * It carries the tool by name because the caller has to be able to say which
 * call was refused without reading a sentence back apart, and because the
 * engine turns this into a result the model is told about.
 */
export class GateViolation extends Error {
  readonly tool: string

  constructor(tool: string, message: string) {
    super(message)
    this.name = 'GateViolation'
    this.tool = tool
  }
}

/**
 * One thing the gate did, in the ADR's own vocabulary.
 *
 * `detail` says what was there and never what the pack said: a rewrite's
 * detail names the `rehearsal` member's previous value and nothing else, so a
 * guardrail line in the pane cannot become a place a draft leaks into a log.
 */
export interface GuardrailNotice {
  tool: string
  action: 'rewrote' | 'refused'
  detail: string
}

/** What `inspectOutboundFrame` decided about one frame. */
export type FrameVerdict =
  | { verdict: 'pass' }
  | { verdict: 'refused'; notice: GuardrailNotice; violation: GateViolation }
  | { verdict: 'rewrote'; notice: GuardrailNotice; frame: JSONRPCMessage }

interface ToolsCallFrame {
  method?: string
  params?: { name?: string; arguments?: Record<string, unknown> }
}

/**
 * The allow-list for a session: what the file granted, intersected with the
 * five the desk will ever serve.
 *
 * The intersection is not defensive tidying. `decodeDeskConfig` refuses a name
 * outside the five by name, so a file that reaches here cannot carry a sixth —
 * but this function is also called with a list a test wrote, and a gate whose
 * ceiling depends on a decoder somewhere else having run is a gate with a
 * premise instead of a rule.
 */
export function allowedTools(configured: readonly string[]): ReadonlySet<string> {
  const ceiling = new Set<string>(ASSISTANT_TOOLS)
  return new Set(configured.filter((name) => ceiling.has(name)))
}

/**
 * What must happen to one outbound frame before it may leave the page.
 *
 * Anything that is not a `tools/call` passes untouched — `initialize`,
 * `tools/list`, `prompts/get`, a notification, a response — because this gate
 * is about what the assistant may *do*, and reading what the runtime serves is
 * not one of the things it does.
 */
export function inspectOutboundFrame(
  message: JSONRPCMessage,
  allowed: ReadonlySet<string>
): FrameVerdict {
  const frame = message as ToolsCallFrame
  if (frame.method !== 'tools/call') return { verdict: 'pass' }
  const name = frame.params?.name
  if (typeof name !== 'string' || !allowed.has(name)) {
    const spelled = typeof name === 'string' ? name : String(name)
    const detail =
      `${spelled} is not one of the tools this assistant may call ` +
      `(${[...allowed].join(', ') || 'none'}); the call did not leave the page and ` +
      `nothing was written`
    return {
      verdict: 'refused',
      notice: { tool: spelled, action: 'refused', detail },
      violation: new GateViolation(spelled, `refused on the wire: ${detail}`)
    }
  }
  const args = frame.params?.arguments ?? {}
  if (name === REHEARSAL_TOOL && args[REHEARSAL_MEMBER] !== true) {
    // What was there, and only that. The pack and the facts travel in the same
    // object and neither is ever named here.
    const had =
      REHEARSAL_MEMBER in args
        ? `the call carried ${REHEARSAL_MEMBER}: ${JSON.stringify(args[REHEARSAL_MEMBER])}`
        : `the call carried no ${REHEARSAL_MEMBER} member`
    return {
      verdict: 'rewrote',
      notice: {
        tool: name,
        action: 'rewrote',
        detail:
          `${had}; it was rewritten to ${REHEARSAL_MEMBER}: true before the frame left ` +
          `the page, so the runtime appends no audit record (runtime ADR-0018, ADR-0028)`
      },
      frame: {
        ...(message as object),
        params: { ...frame.params, arguments: { ...args, [REHEARSAL_MEMBER]: true } }
      } as unknown as JSONRPCMessage
    }
  }
  return { verdict: 'pass' }
}

/**
 * Wrap a transport so nothing reaches the socket unexamined.
 *
 * `send` is replaced on the object it was handed rather than a proxy being
 * returned around it, because the SDK `Client` keeps its own reference to the
 * transport it was connected with: a decorator the client never sees is a gate
 * on a path nothing uses. The wrapped transport is returned as well, so a
 * caller reads as though it were a wrapper.
 *
 * A refusal **rejects** rather than dropping the call, so the caller learns
 * that it happened; the engine turns that rejection into a result the model
 * gets, which is what keeps a refused turn from being re-emitted for ever.
 */
export function gateTransport(
  inner: Transport,
  options: { allowed: ReadonlySet<string>; onGuardrail?: (notice: GuardrailNotice) => void }
): Transport {
  const send = inner.send.bind(inner) as (
    message: JSONRPCMessage,
    sendOptions?: unknown
  ) => Promise<void>
  inner.send = (message: JSONRPCMessage, sendOptions?: unknown): Promise<void> => {
    const decided = inspectOutboundFrame(message, options.allowed)
    if (decided.verdict === 'refused') {
      options.onGuardrail?.(decided.notice)
      return Promise.reject(decided.violation)
    }
    if (decided.verdict === 'rewrote') {
      options.onGuardrail?.(decided.notice)
      return send(decided.frame, sendOptions)
    }
    return send(message, sendOptions)
  }
  return inner
}
