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
import type { ThinkingTier } from '../../config/deskConfig'
import type { AssistantEvent, McpTool, McpToolResult } from '../engine'

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
  /** Opened on the first `next()`; never, if the consumer leaves before one. */
  open: () => AsyncGenerator<AssistantEvent>
  /** Ends whatever the events come from. Called once, before anything waits. */
  cancel: () => void
}): AsyncIterableIterator<AssistantEvent> {
  const done = { value: undefined, done: true } as const
  let events: AsyncGenerator<AssistantEvent> | null = null
  let closed = false
  let closing: Promise<void> | null = null

  const close = (): Promise<void> =>
    (closing ??= (async () => {
      // **Before anything is awaited.** A pending `next()` is waiting on
      // something only this can end, and the `return()` below is queued behind
      // that `next()`: cancelling first is what lets it settle, and its settling
      // is what lets the queued `return()` run at all.
      options.cancel()
      await events?.return(undefined)
    })())

  const iterator: AsyncIterableIterator<AssistantEvent> = {
    [Symbol.asyncIterator]() {
      return iterator
    },
    async next(): Promise<IteratorResult<AssistantEvent>> {
      // After `return()`, without touching what is underneath: the run is over
      // and there is nothing there to ask.
      if (closed) return done
      events ??= options.open()
      const step = await events.next()
      // **A `return()` that overtook this read owns the closing, and this does
      // not wait on it.** Both would otherwise be waiting on the same promise,
      // and the `return()` — which registered first — would settle first: the
      // consumer would be told the iterator was closed before the `next()` it
      // was still holding had come back at all.
      if (closed) return done
      if (step.done === true) {
        closed = true
        await close()
        return done
      }
      return { value: step.value, done: false }
    },
    async return(): Promise<IteratorResult<AssistantEvent>> {
      closed = true
      await close()
      return done
    },
    async throw(cause?: unknown): Promise<IteratorResult<AssistantEvent>> {
      closed = true
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

/**
 * The sentence an engine that does not run a thinking tier says, once.
 *
 * ADR-0001's "degrade visibly": the tier is real configuration, no engine
 * implements it before chunk 4, and a session that silently ran at `off` would
 * be this desk answering a question nobody asked it. One sentence rather than
 * one per adapter, because two spellings of "this did not happen" is how a
 * reader learns to skip the line.
 */
export function thinkingUnavailable(tier: ThinkingTier, engine: string): string {
  return (
    `this desk is configured for thinking "${tier}", and the ${engine} engine does not run a ` +
    `thinking tier yet; the session ran with the model's own default reasoning and no tier ` +
    `parameter was sent`
  )
}
