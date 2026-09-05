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

/** The one method this gate has an opinion about. */
export const TOOLS_CALL = 'tools/call'

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
  /**
   * The frame was rebuilt. `notice` is null exactly where the caller had
   * already written an own `rehearsal: true` — the frame is still rebuilt,
   * because "own, and last" is what travels, but there is nothing to report.
   */
  | { verdict: 'rewrote'; notice: GuardrailNotice | null; frame: JSONRPCMessage }

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

/** A plain object, and nothing else: not an array, not null, not a primitive. */
function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/** The refusal every malformed frame gets, in one place. */
function refuse(tool: string, detail: string): FrameVerdict {
  return {
    verdict: 'refused',
    notice: { tool, action: 'refused', detail },
    violation: new GateViolation(tool, `refused on the wire: ${detail}`)
  }
}

/**
 * The own, enumerable properties of an argument object, read **once** each.
 *
 * Once, because a getter or a proxy may answer differently on a second read:
 * a gate that checked one value and serialized another is a gate that can be
 * told two different things about the same call. What this returns is the
 * object that will be sent, and it is the object the check was made against.
 */
function ownArguments(args: Record<string, unknown>): Record<string, unknown> {
  const copy: Record<string, unknown> = {}
  for (const key of Object.keys(args)) copy[key] = args[key]
  return copy
}

/**
 * What must happen to one outbound frame before it may leave the page.
 *
 * **It fails closed.** A frame that is not a well-formed single JSON-RPC
 * request, notification or response is refused rather than waved through as
 * traffic this gate has no opinion about. That rule is here because the old one
 * — "anything without `method` equal to `tools/call` passes" — let a **batch**
 * out: a JSON-RPC array has no `method`, so a batch carrying an allowed call
 * beside a `write_file` reached the socket whole. The SDK's type says one
 * message; the type is not what runs, and the whole point of a gate at the wire
 * is that it covers callers the type never described.
 *
 * A near-spelling of the guarded method is refused too — `Tools/Call`,
 * ` tools/call `, `tools/call\u200b`. Some server somewhere folds case or trims,
 * and a frame two readers disagree about is one this desk will not send. That
 * is the chassis' own ruling about its query, applied here.
 *
 * What still passes untouched: `initialize`, `tools/list`, `prompts/get`, a
 * notification, and a response to a request the server made. Reading what the
 * runtime serves is not one of the things the assistant *does*.
 */
export function inspectOutboundFrame(
  message: JSONRPCMessage,
  allowed: ReadonlySet<string>
): FrameVerdict {
  const unreadable = '(a frame this gate could not read)'
  if (!isRecord(message)) {
    return refuse(
      unreadable,
      Array.isArray(message)
        ? 'a JSON-RPC batch is not a frame this gate can check one call at a time, so it is ' +
            'refused whole; nothing left the page'
        : 'an outbound frame must be a single JSON-RPC object; this one is not, and nothing ' +
            'left the page'
    )
  }

  const method = (message as { method?: unknown }).method
  if (method === undefined) {
    // A response to a request the server made: an id, and one of result/error.
    const hasId = 'id' in message && message.id !== null && message.id !== undefined
    const answers = 'result' in message || 'error' in message
    if (hasId && answers) return { verdict: 'pass' }
    return refuse(
      unreadable,
      'an outbound frame with no method must be a response carrying an id and a result or an ' +
        'error; this one is neither a request nor a response'
    )
  }
  if (typeof method !== 'string') {
    return refuse(unreadable, 'an outbound frame\'s method must be a string')
  }
  if (method !== TOOLS_CALL) {
    // Fail closed on a spelling that is `tools/call` to some other reader.
    if (method.trim().toLowerCase() === TOOLS_CALL) {
      return refuse(
        unreadable,
        `${JSON.stringify(method)} is a spelling of ${TOOLS_CALL} this desk will not send: a ` +
          `frame two readers disagree about is one it cannot check`
      )
    }
    return { verdict: 'pass' }
  }

  const params = (message as { params?: unknown }).params
  if (!isRecord(params)) {
    return refuse(unreadable, `a ${TOOLS_CALL} frame must carry a params object`)
  }
  const name = params.name
  if (typeof name !== 'string' || !allowed.has(name)) {
    const spelled = typeof name === 'string' ? name : String(name)
    return refuse(
      spelled,
      `${spelled} is not one of the tools this assistant may call ` +
        `(${[...allowed].join(', ') || 'none'}); the call did not leave the page and ` +
        `nothing was written`
    )
  }
  const supplied = params.arguments
  if (supplied !== undefined && !isRecord(supplied)) {
    return refuse(name, `a ${TOOLS_CALL} frame's arguments must be an object where it has any`)
  }

  if (name !== REHEARSAL_TOOL) return { verdict: 'pass' }

  /**
   * **The evaluate frame is never passed through as it arrived.**
   *
   * It is rebuilt from the own, enumerable properties of what the caller gave,
   * with an own `rehearsal: true` written last. An inherited `rehearsal: true`
   * — `Object.create({ rehearsal: true })` — reads as `true` to every check and
   * is dropped by `JSON.stringify`, so a gate that inspected and forwarded
   * would have sent an unrehearsed evaluation and the runtime would have
   * appended an audit record. There is no branch here that forwards the
   * original object, which is why there is no shape that can slip past it.
   */
  const args = ownArguments(supplied ?? {})
  const already = Object.hasOwn(args, REHEARSAL_MEMBER) && args[REHEARSAL_MEMBER] === true
  args[REHEARSAL_MEMBER] = true
  const rebuilt = {
    ...(message as object),
    params: { ...params, arguments: args }
  } as unknown as JSONRPCMessage
  if (already) {
    // Nothing to report: the caller asked for exactly what it got. The frame
    // is still the rebuilt one, because "own and last" is what travels.
    return { verdict: 'rewrote', notice: null, frame: rebuilt }
  }
  // What was there, and only that. The pack and the facts travel in the same
  // object and neither is ever named here.
  const had = Object.hasOwn(supplied ?? {}, REHEARSAL_MEMBER)
    ? `the call carried ${REHEARSAL_MEMBER}: ${JSON.stringify((supplied ?? {})[REHEARSAL_MEMBER])}`
    : REHEARSAL_MEMBER in (supplied ?? {})
      ? `the call carried an inherited ${REHEARSAL_MEMBER}, which no serializer sends`
      : `the call carried no ${REHEARSAL_MEMBER} member`
  return {
    verdict: 'rewrote',
    notice: {
      tool: name,
      action: 'rewrote',
      detail:
        `${had}; it was rewritten to an own ${REHEARSAL_MEMBER}: true before the frame left ` +
        `the page, so the runtime appends no audit record (runtime ADR-0018, ADR-0028)`
    },
    frame: rebuilt
  }
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
      if (decided.notice !== null) options.onGuardrail?.(decided.notice)
      return send(decided.frame, sendOptions)
    }
    return send(message, sendOptions)
  }
  return inner
}
