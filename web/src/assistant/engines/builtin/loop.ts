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
import { anthropic } from './providers/anthropic'
import { openai } from './providers/openai'
import { ModelHttpError } from './providers/types'
import type { Provider, ToolCall } from './providers/types'
import type { AssistantEvent, AssistantSession, McpToolResult } from '../../engine'

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
 * message this engine cannot choose between and should not guess at.
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

/** The text half of one tool answer, joined in the runtime's own order. */
function textOf(result: McpToolResult): string {
  return (result.content ?? [])
    .filter((block) => block.type === 'text')
    .map((block) => block.text ?? '')
    .join('\n')
}

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
        detail:
          `this desk is configured for thinking "${session.thinking.tier}", and the built-in ` +
          `engine does not run a thinking tier yet; the session ran with the model's own ` +
          `default reasoning and no tier parameter was sent`
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
        base: session.model.baseUrl,
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
