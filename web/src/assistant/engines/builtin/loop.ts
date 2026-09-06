/**
 * The built-in engine's loop — the whole of it.
 *
 * No framework: an explicit `while` over an explicit messages array. That is
 * what the bake-off measured as the control, and it is what makes every
 * promise this engine keeps a line somebody can point at. ADR-0001 ships it as
 * the keyless fallback: it adds nothing to the supply chain, and its guardrails
 * live one level below it in the desk's own ToolGate, so a bug written here
 * cannot reach the runtime with a tool nobody granted.
 *
 * What is deliberately **not** here, and lands later: the thinking tier
 * (chunk 4 — a tier other than `off` is reported unavailable and the session
 * continues), the refutation pass, and any writing at all. The proposal is the
 * only sink; the desk renders it and a person accepts it.
 */
import {
  MAX_TURNS,
  SYSTEM,
  eventIterator,
  extractProposal,
  guardedCallTool,
  isCancelled,
  onceOnly,
  textOf,
  thinkingUnavailable,
  withAbort
} from '../contract'
import { anthropic } from './providers/anthropic'
import { openai } from './providers/openai'
import { ModelHttpError } from './providers/types'
import type { Proposal } from '../contract'
import type { CallTool } from '../../engine'
import type { Provider, ToolCall } from './providers/types'
import type { AssistantEvent, AssistantSession } from '../../engine'

// The contract's own vocabulary — the turn bound, the desk's sentence to the
// model, and the one reading of a proposal — is `engines/contract.ts`, shared
// with every other adapter. Re-exported because this engine's own suite and the
// mutation matrix name them here, and because a reader of this loop should not
// have to go looking for the two constants it is bounded by.
export { MAX_TURNS, SYSTEM, extractProposal }
export type { Proposal } from '../contract'

/**
 * One tool call, and an answer whatever happens.
 *
 * A refused call has to come back as a **result** rather than as nothing: a
 * model that is told nothing re-emits the same call, and the loop spends its
 * turns on a tool it will never get. The ToolGate's rejection and the
 * runtime's own in-band `isError` both land here as one shape.
 */
async function callSafely(
  callTool: CallTool,
  call: ToolCall
): Promise<{ text: string; isError: boolean; structured?: unknown }> {
  try {
    const result = await callTool(call.name, call.args)
    return {
      text: textOf(result),
      isError: Boolean(result.isError),
      structured: result.structuredContent
    }
  } catch (cause) {
    // **A cancelled run is not a refused tool.** Turning it into a result the
    // model is told about would carry on a session nobody is listening to.
    if (isCancelled(cause)) throw cause
    return {
      text:
        `refused: ${(cause as Error).message}. This assistant proposes; it never ` +
        `writes a file and never calls a tool it was not offered.`,
      isError: true
    }
  }
}

function providerFor(family: AssistantSession['model']['family']): Provider {
  return family === 'anthropic' ? anthropic : openai
}

/**
 * Run one session, as a stream of events.
 *
 * **The outer shape is an iterator and not this generator**, for the reason
 * `engines/contract.ts` gives at length: an async generator serves `next()` and
 * `return()` from one queue, so a `return()` arriving while a `next()` waits on
 * a model request is not run until that request settles — and the abort that
 * would have settled it was inside the `return()`. Measured on this engine as
 * well as on the SDK-backed one: both promises hung for ever.
 *
 * So the abort is this engine's own, chained to the session's, and it is what
 * the iterator cancels with. What the provider is given is `stop`'s signal, not
 * the session's, so a consumer that walks away ends the request in flight.
 *
 * `end` is emitted exactly once and **not from a `finally`**: a consumer that
 * stops listening closes this generator, and closing it is what it means for
 * that consumer to be owed no terminal event.
 */
export function runBuiltin(session: AssistantSession): AsyncIterable<AssistantEvent> {
  const stop = new AbortController()
  // **One cancellation, reached four ways.** The consumer's `return()`, its
  // `throw()`, the session's own signal and the run's natural end all run these
  // statements and no others, once, synchronously, before anything is awaited —
  // because what is waiting is exactly what this releases.
  const cancel = onceOnly(() => {
    session.signal.removeEventListener('abort', onAbort)
    stop.abort()
  })
  function onAbort(): void {
    cancel()
  }
  if (session.signal.aborted) stop.abort()
  else session.signal.addEventListener('abort', onAbort, { once: true })
  return eventIterator({
    open: () => builtinEvents(session, stop.signal),
    cancel
  })
}

async function* builtinEvents(
  session: AssistantSession,
  signal: AbortSignal
): AsyncGenerator<AssistantEvent> {
  try {
    if (session.thinking.tier !== 'off') {
      // Reported and then carried on with, which is what ADR-0001 means by
      // degrading visibly: the tier is real configuration, this engine does
      // not implement it yet, and a session that silently ran at `off` would
      // be this desk answering a question nobody asked it.
      yield {
        type: 'thinking_unavailable',
        detail: thinkingUnavailable(session.thinking.tier, 'built-in')
      }
    }

    // The runtime, reachable only while the run is: the signal is read before
    // a call is dispatched, so nothing reaches `jpack mcp` after the consumer
    // has left, and the wait is bounded, so a call in flight cannot hold the
    // cleanup that is ending it.
    const callTool = guardedCallTool(session, signal)
    const provider = providerFor(session.model.family)
    const tools = provider.tools(session.tools)
    const messages: unknown[] = provider.initialMessages(SYSTEM, session.prompt)
    let proposal: Proposal | null = null
    let turns = 0

    for (let turn = 1; turn <= MAX_TURNS; turn += 1) {
      turns = turn
      // Bounded by the run's signal and not only by the request's: a capability
      // that never settles must not be able to hold this loop open.
      const reply = await withAbort(
        provider.send({
          call: session.model.call,
          model: session.model.model,
          system: SYSTEM,
          messages,
          tools,
          stream: true,
          signal
        }),
        signal
      )

      if (reply.calls.length === 0) {
        proposal = extractProposal(reply.text)
        break
      }

      const results: { call: ToolCall; text: string; isError: boolean }[] = []
      for (const call of reply.calls) {
        yield { type: 'tool_call', name: call.name, args: call.args }
        const outcome = await callSafely(callTool, call)
        yield {
          type: 'tool_result',
          name: call.name,
          isError: outcome.isError,
          text: outcome.text,
          ...(outcome.structured === undefined ? {} : { structured: outcome.structured })
        }
        results.push({ call, text: outcome.text, isError: outcome.isError })
      }

      provider.appendTurn(messages, reply, results)
    }

    if (proposal === null) {
      yield {
        type: 'error',
        message:
          `the session reached its bound of ${MAX_TURNS} model turns (it used ${turns}) ` +
          `without a final message carrying a proposal; nothing was written`
      }
    } else {
      yield { type: 'proposal', document: proposal.document, unknowns: proposal.unknowns }
    }
  } catch (cause) {
    // **A cancelled run says nothing more at all — not even `end`.** It is the
    // unwinding of a session somebody ended, not a run that finished, and the
    // terminal event belongs to a run that finished. Both engines answer a
    // cancellation the same way, and the page's own terminal accounting is the
    // run hook's (`useAssistantRun`), which writes one whatever an engine does.
    if (isCancelled(cause)) return
    yield { type: 'error', message: describe(cause) }
  }
  // **After the `try`, not inside a `finally`.** A `finally` that yields makes
  // a consumer's first `return()` resolve `{ value: end, done: false }` with
  // this generator still suspended, and `for await`'s own closing discards that
  // value — so the terminal event is delivered to nobody. Here it is reached on
  // every path the run itself takes, and skipped on the one path where it
  // should be: a consumer that asked to stop.
  yield { type: 'end' }
}

/**
 * One sentence for a failure, with the endpoint's own where there is one.
 *
 * A relay refusal arrives in the chassis' `{error, code}` envelope and a
 * status; both are worth saying, because "this desk has no key stored" and
 * "the endpoint refused the request" are different things to go and fix.
 */
function describe(cause: unknown): string {
  if (cause instanceof ModelHttpError) {
    return `${cause.route} answered ${cause.status}: ${cause.endpointMessage}`
  }
  const error = cause as Error
  if (error?.name === 'AbortError') return 'the session was stopped'
  return `${error?.name ?? 'Error'}: ${error?.message ?? String(cause)}`
}
