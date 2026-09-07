/**
 * The contract's own vocabulary, shared by every adapter.
 *
 * ADR-0001 makes the engine a slot: the desk states one contract and each
 * framework is an adapter behind it. Two adapters ship, and the handful of
 * things that are the **contract's** rather than either loop's live here, in one
 * module both import, for the reason the ToolGate's allow-list is one list: a
 * rule written twice is a rule two readers can disagree about, and the one that
 * matters here — *the document a person accepts comes out of one fenced block
 * and never out of the prose around it* — is not a rule to keep in step by hand.
 *
 * What is **not** here is anything about a wire, a framework or a turn: those
 * are each adapter's own, and an engine that needed a helper from another
 * engine would be an engine the slot did not really separate.
 */
import { withoutUnsupportedKeywords } from '../geminiSchema'
import type { EndpointKind } from '../../config/deskConfig'
import type { AssistantEvent, AssistantSession, CallTool, McpTool, McpToolResult } from '../engine'

/**
 * The run ended while something was still waiting on it.
 *
 * Not a failure and never an `error` event: a consumer left, a viewer pressed
 * Stop, or the run's own cleanup came round. Every loop treats it as the
 * unwinding it is.
 */
export class RunCancelled extends Error {
  constructor() {
    super('the run was cancelled while this was still waiting on it')
    this.name = 'RunCancelled'
  }
}

/** Whether a failure is a run that ended rather than a run that went wrong. */
export function isCancelled(cause: unknown): boolean {
  const name = (cause as { name?: unknown } | null | undefined)?.name
  return name === 'RunCancelled' || name === 'AbortError'
}

/**
 * One await on something outside the engine, bounded by the run's own signal.
 *
 * **This is the class fix, and it is why it is one function used everywhere.**
 * Four rounds of review found four interleavings of the same shape: a consumer
 * stops a run, the cleanup that would end it is queued behind an `await` on
 * something the cleanup was supposed to end, and both wait for ever. Aborting
 * *the thing being awaited* closes one of those at a time and only where the
 * thing happens to honour a signal — the model request does, a `tools/call`
 * over a socket does not.
 *
 * So no loop in either engine awaits anything external directly. Every one of
 * them awaits **this**, which settles the moment the run's signal does, whatever
 * the thing underneath decides to do. The underlying promise keeps a handler
 * either way, so nothing it does later reaches the page as an unclaimed
 * rejection.
 */
export function withAbort<T>(work: () => Promise<T>, signal: AbortSignal): Promise<T> {
  // **A thunk, so the work is not even started on a closed run.** Taking a
  // promise meant the call had already been made by the time the signal was
  // read: a session aborted before `start` still reached the model, because
  // evaluating the argument *is* the request.
  if (signal.aborted) return Promise.reject(new RunCancelled())
  let started: Promise<T>
  try {
    started = work()
  } catch (cause) {
    return Promise.reject(cause as Error)
  }
  return new Promise<T>((resolve, reject) => {
    const cancelled = () => reject(new RunCancelled())
    signal.addEventListener('abort', cancelled, { once: true })
    started.then(
      (value) => {
        signal.removeEventListener('abort', cancelled)
        resolve(value)
      },
      (cause: unknown) => {
        signal.removeEventListener('abort', cancelled)
        reject(cause)
      }
    )
    // **Read again, after the listener exists.** A run that closed *while*
    // `work()` was running — a capability that aborts the session itself, a
    // consumer that left during a synchronous dispatch — fired its `abort`
    // before this listener was registered, so the listener never ran and the
    // await hung on whatever `work()` returned. The check at the top of this
    // function cannot see that: it happens before the work starts. Rejecting
    // here after the handlers are attached settles the wait and leaves nothing
    // unclaimed, because `started` already has both of them.
    if (signal.aborted) cancelled()
  })
}

/**
 * One run's own gate: closed, then aborted, then released — in that order.
 *
 * **`closed` is the single source of truth, and it is set first.** A signal
 * says "stop soon"; a closed gate says "nothing more from this run reaches
 * anybody", and the difference is a whole class of race. `withAbort` settles
 * with a *value* when the thing underneath wins by a microtask, and the abort
 * that arrived a moment later then runs while the loop is still holding that
 * value — so the loop resumes, yields a `tool_call`, or catches the original
 * failure and reports an `error` and an `end`, all after the run was cancelled.
 * Aborting cannot prevent that; a flag the delivery path reads can, and does.
 *
 * The session's own signal is chained here, so a viewer pressing Stop and a
 * consumer walking away reach the same statements in the same order. A session
 * that is **already** aborted closes the gate before anything is constructed:
 * no event, no provider, no request.
 */
export interface RunGate {
  /** The run's own signal. Aborted the instant the gate closes. */
  readonly signal: AbortSignal
  /** True from the instant the run closes, before anything else happens. */
  isClosed(): boolean
  /** Close it: marks closed, aborts, releases — once, and synchronously. */
  close(): void
}

export function openRun(session: AssistantSession, release: () => void): RunGate {
  const stop = new AbortController()
  let closed = false
  const close = () => {
    if (closed) return
    // **First**, and before anything is awaited or aborted: what follows can
    // resume a loop, and what a resumed loop produces must already be nobody's.
    closed = true
    session.signal.removeEventListener('abort', close)
    stop.abort()
    release()
  }
  if (session.signal.aborted) close()
  else session.signal.addEventListener('abort', close, { once: true })
  return { signal: stop.signal, isClosed: () => closed, close }
}

/**
 * The runtime, reachable only while the run is.
 *
 * Two guards, and the first is the one that matters: **the signal is read
 * before the call is dispatched**, so a `tools/call` cannot reach the runtime
 * after the consumer has left — a late model answer arriving on a closed run
 * has nowhere to send it. The second bounds the wait, so a call already in
 * flight cannot hold the cleanup that is trying to end it.
 */
export function guardedCallTool(session: AssistantSession, signal: AbortSignal): CallTool {
  return async (name, args) => {
    if (signal.aborted) throw new RunCancelled()
    return withAbort(() => session.callTool(name, args), signal)
  }
}

/**
 * A cancellation that happens once, however many ways it is reached.
 *
 * `return()`, `throw()`, the session's own signal and the run's natural end all
 * arrive here, and what they run is the same statements in the same order —
 * synchronously, before anything is awaited, because what is waiting is exactly
 * what this releases.
 */
export function onceOnly(work: () => void): () => void {
  let ran = false
  return () => {
    if (ran) return
    ran = true
    work()
  }
}

/**
 * An engine's events, behind an iterator whose `return()` acts **at once**.
 *
 * **An async generator cannot be the engine's outer shape.** Its `next()`,
 * `return()` and `throw()` are served from one queue: a `return()` that arrives
 * while a `next()` is pending is not run until that `next()` settles. So a run
 * waiting on a model request that only ends when it is aborted could never be
 * stopped by the consumer that owned it — the `return()` carrying the abort was
 * queued behind the very `next()` the abort would have released, and both hung
 * for ever. Reproduced on both engines before this existed.
 *
 * This is the hand-written iterator that fixes it. `return()` **cancels first**,
 * synchronously, before it awaits anything: whatever the pending `next()` is
 * waiting on ends, that `next()` settles `{ done: true }`, and only then does
 * `return()` settle `{ done: true }` itself. The generator underneath is opened
 * on the first `next()` and never at all if the consumer leaves before then, and
 * the cancellation runs exactly once however many times it is asked for.
 *
 * **Nothing is delivered by `return()`.** A consumer that has stopped listening
 * is owed no terminal event, and an iterator that answered one would be the
 * `finally`-that-yields defect wearing a different hat: `{ done: false }` from a
 * `return()` leaves the consumer holding a value `for await` discards.
 */
export function eventIterator(options: {
  /** The run's gate. Closed is closed, whoever closed it. */
  gate: RunGate
  /** Opened on the first `next()`; never, if the run closed before one. */
  open: () => AsyncGenerator<AssistantEvent>
}): AsyncIterableIterator<AssistantEvent> {
  const done = { value: undefined, done: true } as const
  let events: AsyncGenerator<AssistantEvent> | null = null
  let closing: Promise<void> | null = null

  const close = (): Promise<void> =>
    (closing ??= (async () => {
      // **Before anything is awaited.** A pending `next()` is waiting on
      // something only this can end, and the `return()` below is queued behind
      // that `next()`: closing first is what lets it settle, and its settling
      // is what lets the queued `return()` run at all.
      options.gate.close()
      await events?.return(undefined)
    })())

  const iterator: AsyncIterableIterator<AssistantEvent> = {
    [Symbol.asyncIterator]() {
      return iterator
    },
    async next(): Promise<IteratorResult<AssistantEvent>> {
      // **Nothing is opened on a closed run**, so a session aborted before
      // `start` builds no provider and makes no request; and after a `return()`
      // there is nothing left to ask.
      if (options.gate.isClosed()) return done
      events ??= options.open()
      const step = await events.next()
      // **The one place delivery is decided, and it reads the gate.** A loop
      // that resumed after the run closed — because the thing it was awaiting
      // won its race with the abort by a microtask — can yield whatever it
      // likes: a `tool_call`, an `error`, an `end`. None of it is anybody's.
      //
      // And this does **not** wait on the closing: a `return()` that overtook
      // this read owns that, and both waiting on the same promise would settle
      // the `return()` first — telling the consumer the iterator was closed
      // before the `next()` it was holding had come back at all.
      if (options.gate.isClosed()) return done
      if (step.done === true) {
        await close()
        return done
      }
      return { value: step.value, done: false }
    },
    async return(): Promise<IteratorResult<AssistantEvent>> {
      // `close` shuts the gate as its first statement, before it awaits — an
      // async function's body runs synchronously to its first `await`, so this
      // is the synchronous close, and saying it twice would only hide which
      // line is the one that matters.
      await close()
      return done
    },
    async throw(cause?: unknown): Promise<IteratorResult<AssistantEvent>> {
      await close()
      throw cause
    }
  }
  return iterator
}

/**
 * The most model turns one session may take.
 *
 * Eight is the whole scripted scenario; twenty leaves room for a model that
 * asks the runtime more questions than the fixture does, and bounds a loop
 * whose stopping condition is a model's own decision to stop calling tools.
 * A session that reaches it ends with an error rather than quietly.
 */
export const MAX_TURNS = 20

/**
 * What the desk tells the model about itself, above the runtime's own prompt.
 *
 * Short on purpose: the authoring instructions are the runtime's, fetched over
 * `prompts/get`, and a system prompt that restated them would be this desk
 * having a second opinion about how a pack is written. What is here is the two
 * things the runtime's prompt does not know — that this loop proposes rather
 * than writes, and the shape the proposal has to arrive in.
 */
export const SYSTEM =
  'You are the judgment-pack desk’s authoring assistant. You propose; you never ' +
  'write a file and never state a verdict of your own. When you report a check you ' +
  'quote the runtime. End by proposing the pack as a single fenced JSON block ' +
  'shaped {"proposal": {"kind": "create", "document": …, "unknowns": […]}}.'

const FENCE = /```(?:json)?\s*\n([\s\S]*?)\n```/g

export interface Proposal {
  document: unknown
  unknowns: string[]
}

/**
 * The proposal, and **only** out of the fenced block.
 *
 * Never out of the prose around it. The model's sentences are the model's; the
 * document this desk offers a person to accept is the one the model set apart
 * as a document, and reading a JSON object out of an explanation would let a
 * worked example become a proposal. Exactly one block, because two is a
 * message no engine can choose between and none should guess at.
 */
export function extractProposal(text: string): Proposal {
  const blocks = [...(text ?? '').matchAll(FENCE)].map((match) => match[1] ?? '')
  if (blocks.length !== 1) {
    throw new Error(
      `the final message must carry exactly one fenced JSON block holding the proposal; ` +
        `this one carried ${blocks.length}`
    )
  }
  const parsed = JSON.parse(blocks[0]!) as {
    proposal?: { document?: unknown; unknowns?: unknown }
  }
  if (parsed.proposal === undefined) {
    throw new Error('the fenced block in the final message carries no "proposal" member')
  }
  const unknowns = parsed.proposal.unknowns
  return {
    document: parsed.proposal.document,
    unknowns: Array.isArray(unknowns) ? unknowns.map((entry) => String(entry)) : []
  }
}

/**
 * The schema the runtime served for one tool, or a refusal.
 *
 * **The desk never invents a contract the runtime does not enforce.** Every
 * engine passes the served `inputSchema` through untouched, and a tool that
 * arrived without one used to be given a permissive `{"type":"object"}` written
 * here — which is this desk telling the model that anything is acceptable for a
 * tool whose actual contract it does not know. K2 says the model is shown the
 * runtime's contract *or it is shown nothing*, and a session that cannot show
 * it does not run.
 *
 * The five the runtime serves all carry one, so this refuses nothing a real
 * `jpack mcp` offers; what it refuses is a future tool, or another server, that
 * does not.
 */
export function servedSchema(tool: McpTool): unknown {
  if (tool.inputSchema === undefined || tool.inputSchema === null) {
    throw new Error(
      `the runtime served ${tool.name} without an input schema, and this desk will not write ` +
        `one for it: the model is shown the contract the runtime enforces or it is shown ` +
        `nothing. Nothing was written.`
    )
  }
  return tool.inputSchema
}

/**
 * The schema one family is shown, out of the one the runtime served.
 *
 * **Two families are shown the runtime's schema and the third is shown it minus
 * a closed list.** `servedSchema` above is the rule — the contract the runtime
 * enforces, or nothing — and this is the one documented exception to it: the
 * native Gemini wire takes an OpenAPI subset in `parameters` and refuses
 * keywords an ordinary JSON Schema carries, `additionalProperties` among them,
 * which every one of the runtime's five declares.
 *
 * The removal list, what it costs and why an unlisted keyword is an error
 * rather than a strip are all `assistant/geminiSchema.ts`'s, in one place, so
 * that both engines show the model the same thing. Nothing is added or
 * rewritten here, and nothing at all happens on the other two families.
 */
export function servedSchemaFor(family: EndpointKind, tool: McpTool): unknown {
  const schema = servedSchema(tool)
  return family === 'gemini' ? withoutUnsupportedKeywords(schema) : schema
}

/**
 * Every schema this session put in front of the model, and **only** those.
 *
 * Read by the refusal classifier, which asks whether a 400 names a keyword the
 * desk actually sent. The provider's own tool payload is deliberately not what
 * it is given: that carries `parameters`, `functionDeclarations` and the tool
 * names around the schema, and an endpoint's own error path — `Unknown name
 * "properties" at 'tools[0].function_declarations[0].parameters'` — contains
 * those words, so a classifier reading the payload would report the wrapper
 * instead of the keyword.
 */
export function schemasShown(family: EndpointKind, tools: McpTool[]): unknown[] {
  return tools.map((tool) => servedSchemaFor(family, tool))
}

/** The text half of one tool answer, joined in the runtime's own order. */
export function textOf(result: McpToolResult): string {
  return (result.content ?? [])
    .filter((block) => block.type === 'text')
    .map((block) => block.text ?? '')
    .join('\n')
}

/**
 * Whether an answer is a stream, read off the **answer** rather than off the
 * request.
 *
 * A request that asked to stream may be answered whole — a gateway that
 * buffers, an endpoint that ignores the member, an error envelope from the
 * desk's own relay — and an engine that parsed by what it *asked for* would
 * read a JSON object as an event stream and report an empty turn. So the
 * content type decides, on every engine.
 */
export function isEventStream(response: Response): boolean {
  return (response.headers.get('content-type') ?? '').toLowerCase().includes('text/event-stream')
}

// **The "this engine does not run a tier" sentence is gone with the reason for
// it.** Both engines run the tier now, and every sentence the desk says about
// thinking — the degrade, "this model always thinks", the line in the tab — is
// `assistant/thinking.ts`'s, because a state that two modules describe is a
// state two readers can disagree about.
