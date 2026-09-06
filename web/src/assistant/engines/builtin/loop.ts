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
import { MAX_TURNS, SYSTEM, extractProposal, textOf, thinkingUnavailable } from '../contract'
import { anthropic } from './providers/anthropic'
import { openai } from './providers/openai'
import { ModelHttpError } from './providers/types'
import type { Proposal } from '../contract'
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
  session: AssistantSession,
  call: ToolCall
): Promise<{ text: string; isError: boolean; structured?: unknown }> {
  try {
    const result = await session.callTool(call.name, call.args)
    return {
      text: textOf(result),
      isError: Boolean(result.isError),
      structured: result.structuredContent
    }
  } catch (cause) {
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
 * `end` is emitted exactly once, from the `finally`, whatever happened above
 * it — including an abort. A pane that renders "running" until it sees `end`
 * would otherwise spin for ever on the one path nobody tests.
 */
export async function* runBuiltin(session: AssistantSession): AsyncGenerator<AssistantEvent> {
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

    const provider = providerFor(session.model.family)
    const tools = provider.tools(session.tools)
    const messages: unknown[] = provider.initialMessages(SYSTEM, session.prompt)
    let proposal: Proposal | null = null
    let turns = 0

    for (let turn = 1; turn <= MAX_TURNS; turn += 1) {
      turns = turn
      const reply = await provider.send({
        call: session.model.call,
        model: session.model.model,
        system: SYSTEM,
        messages,
        tools,
        stream: true,
        signal: session.signal
      })

      if (reply.calls.length === 0) {
        proposal = extractProposal(reply.text)
        break
      }

      const results: { call: ToolCall; text: string; isError: boolean }[] = []
      for (const call of reply.calls) {
        yield { type: 'tool_call', name: call.name, args: call.args }
        const outcome = await callSafely(session, call)
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
    yield { type: 'error', message: describe(cause) }
  } finally {
    yield { type: 'end' }
  }
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
