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
 * **The thinking tier is the desk's, and this loop only carries it.** The
 * members on each request come from `assistant/thinking.ts`'s table, the one
 * fallback between the two Anthropic spellings is that module's, and so are
 * the two states a tier cannot express. What is here is the three places an
 * engine has to put them: the members on the body, the passage events, and the
 * one retry a refusal earns.
 *
 * What is deliberately **not** here: any writing at all. The proposal is the
 * only sink; the desk renders it and a person accepts it.
 */
import {
  MAX_TURNS,
  SYSTEM,
  eventIterator,
  extractProposal,
  guardedCallTool,
  isCancelled,
  openRun,
  textOf,
  withAbort
} from '../contract'
import { openThinking } from '../../thinking'
import {
  CRITIC_SYSTEM,
  MAX_CRITIC_TURNS,
  criticCannotRun,
  criticMessage,
  critiqueEvent,
  critiqueOnProposal,
  openCritique
} from '../../refutation'
import { anthropic } from './providers/anthropic'
import { openai } from './providers/openai'
import { ModelHttpError } from './providers/types'
import type { Proposal } from '../contract'
import type { Critique, CritiqueRecorder } from '../../refutation'
import type { ThinkingSlot } from '../../thinking'
import type { CallTool } from '../../engine'
import type { ModelTurn, Provider, ToolCall } from './providers/types'
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
/**
 * What one tool call produced: an answer from the runtime, or a refusal by the
 * desk's own gate.
 *
 * **Two shapes, and only one of them can reach a recorder.** The distinction
 * matters to exactly one caller — the refutation pass — because a refusal is
 * not a thing the runtime said, and a critique counting one would report a
 * verdict about a call that never left the page. It was a boolean an engine
 * set, which is a flag somebody can set wrongly; it is a **type** now. The
 * refusal arm has no `report` member at all, so the code that would hand a
 * guardrail's own words to the recorder does not compile.
 */
type ToolOutcome =
  | {
      kind: 'answered'
      text: string
      isError: boolean
      structured?: unknown
      /** The runtime answered, so this answer may be a check. */
      report(recorder: CritiqueRecorder, name: string): void
    }
  | { kind: 'refused'; text: string; isError: true; structured?: undefined }

async function callSafely(callTool: CallTool, call: ToolCall): Promise<ToolOutcome> {
  try {
    const result = await callTool(call.name, call.args)
    const text = textOf(result)
    return {
      kind: 'answered',
      text,
      isError: Boolean(result.isError),
      structured: result.structuredContent,
      report: (recorder, name) => recorder.saw(name, text)
    }
  } catch (cause) {
    // **A cancelled run is not a refused tool.** Turning it into a result the
    // model is told about would carry on a session nobody is listening to.
    if (isCancelled(cause)) throw cause
    return {
      kind: 'refused',
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
  // **One gate, reached four ways.** The consumer's `return()`, its `throw()`,
  // the session's own signal and the run's natural end all close the same gate,
  // once, synchronously — and a session already aborted when this is called
  // closes it before a provider is built or a request is made.
  const gate = openRun(session, () => {})
  return eventIterator({ gate, open: () => builtinEvents(session, gate.signal) })
}

async function* builtinEvents(
  session: AssistantSession,
  signal: AbortSignal
): AsyncGenerator<AssistantEvent> {
  try {
    // The tier, held by the desk. This loop asks it for members and yields
    // whatever notice it hands back; it never decides what a refusal meant.
    const slot = openThinking(session)

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
      // that never settles must not be able to hold this loop open. A thunk,
      // so a closed run makes no request at all.
      //
      // **At most three attempts, and each one is the desk's decision.** The
      // tier as configured; the other Anthropic spelling where the first was
      // refused; and the plain request once the slot has degraded. A refusal
      // that is not about the tier is rethrown on the first attempt, and once
      // the slot carries no members `refused` says `other` — so this cannot
      // spin.
      let reply: ModelTurn | null = null
      for (let attempt = 1; attempt <= 3 && reply === null; attempt += 1) {
        try {
          reply = await withAbort(
            () =>
              provider.send({
                call: session.model.call,
                model: session.model.model,
                system: SYSTEM,
                messages,
                tools,
                stream: true,
                signal,
                thinking: slot.members()
              }),
            signal
          )
        } catch (cause) {
          if (!(cause instanceof ModelHttpError)) throw cause
          const refusal = slot.refused(cause.status, cause.endpointMessage)
          if (refusal.kind === 'other') throw cause
          // One line, once, whatever brought it about. A dialect fallback says
          // nothing: the desk asked in the other spelling and the session still
          // thinks.
          if (refusal.kind === 'degrade' && refusal.event !== null) yield refusal.event
        }
      }
      if (reply === null) {
        throw new Error('the model request was refused at every thinking spelling this desk knows')
      }

      // **The passage, not the deltas.** This loop reads a whole turn before it
      // yields anything, so what it has is the passage the contract's `done`
      // marks — and reasoning text goes to the tab and nowhere near the
      // runtime.
      for (const passage of reply.reasoning) {
        yield { type: 'reasoning', text: passage, done: true }
      }
      // The **turn** is the unit of evidence about an endpoint, and whether it
      // produced an answer of its own is half of it: a turn that only called a
      // tool says nothing about whether an endpoint will reason.
      if (reply.reasoning.length > 0) slot.sawReasoning()
      const noticed = slot.turnEnded(reply.text !== '')
      if (noticed !== null) yield noticed

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
      // **The refutation pass, after the proposal exists and before it is
      // shown.** Gated on the tier, on the same gate, over the same five tools,
      // and every call it makes is a read. What it produces is information: a
      // refuted proposal is still a proposal, and the desk shows it with the
      // runtime's own words beside it.
      const critique = slot.runsRefutation()
        ? yield* refute({ session, provider, tools, callTool, slot, signal, document: proposal.document })
        : null
      yield {
        type: 'proposal',
        document: proposal.document,
        unknowns: proposal.unknowns,
        ...critiqueOnProposal(critique)
      }
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
 * The critic: a second loop, a fresh conversation, and the same everything else.
 *
 * **Not a second engine and not a second gate.** It is handed the run's own
 * `callTool` — bound through the ToolGate and bounded by the run's signal — and
 * the runtime's own five tools, so its `experimental_evaluate` is rewritten to
 * a rehearsal on the wire exactly as the main loop's is, and a `write_file` it
 * asked for would be refused by the same layer. The cancellation seam holds for
 * the same reason: every await here goes through `withAbort` on the run's
 * signal, so a viewer who presses Stop during the pass ends it where they would
 * have ended the loop above.
 *
 * The verdict is `assistant/refutation.ts`'s, computed from the answers this
 * function saw come back. Nothing about it is read out of the critic's prose.
 */
async function* refute(options: {
  session: AssistantSession
  provider: Provider
  tools: unknown[]
  callTool: CallTool
  slot: ThinkingSlot
  signal: AbortSignal
  document: unknown
}): AsyncGenerator<AssistantEvent, Critique> {
  // **No runtime prompt, no critic.** The instructions are the runtime's; this
  // desk adds one sentence and has none of its own to fall back on.
  const cannot = criticCannotRun(options.session.testPrompt)
  if (cannot !== null) {
    yield critiqueEvent(cannot)
    return cannot
  }
  const recorder = openCritique()
  const messages = options.provider.initialMessages(
    CRITIC_SYSTEM,
    criticMessage(options.session.testPrompt, options.document)
  )
  let modelText = ''
  for (let turn = 1; turn <= MAX_CRITIC_TURNS; turn += 1) {
    const reply = await withAbort(
      () =>
        options.provider.send({
          call: options.session.model.call,
          model: options.session.model.model,
          system: CRITIC_SYSTEM,
          messages,
          tools: options.tools,
          stream: true,
          signal: options.signal,
          // The same tier the main loop settled on, degrade included.
          thinking: options.slot.members()
        }),
      options.signal
    )
    for (const passage of reply.reasoning) {
      yield { type: 'reasoning', text: passage, done: true }
    }
    // The critic's turns are this session's turns, on this session's endpoint.
    if (reply.reasoning.length > 0) options.slot.sawReasoning()
    const noticed = options.slot.turnEnded(reply.text !== '')
    if (noticed !== null) yield noticed
    if (reply.calls.length === 0) {
      modelText = reply.text
      break
    }
    const results: { call: ToolCall; text: string; isError: boolean }[] = []
    for (const call of reply.calls) {
      yield { type: 'tool_call', name: call.name, args: call.args }
      const outcome = await callSafely(options.callTool, call)
      yield {
        type: 'tool_result',
        name: call.name,
        isError: outcome.isError,
        text: outcome.text,
        ...(outcome.structured === undefined ? {} : { structured: outcome.structured })
      }
      // **Only what the runtime answered**, and the type is what says so: the
      // refusal arm has no `report`, so a call the gate refused cannot reach
      // the recorder from here at all. Which of the answers is a check is then
      // the desk's decision and not this loop's.
      if (outcome.kind === 'answered') outcome.report(recorder, call.name)
      results.push({ call, text: outcome.text, isError: outcome.isError })
    }
    // Echoed as received here too, so the pass's own thinking blocks and their
    // signatures survive its tool turns exactly as the main loop's do.
    options.provider.appendTurn(messages, reply, results)
  }
  const critique = recorder.critique(modelText)
  yield critiqueEvent(critique)
  return critique
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
