/**
 * The `vercel` engine's loop: the Vercel AI SDK v7, behind the desk's contract.
 *
 * ADR-0001 makes this the default engine and says what the slot buys: the
 * desk's promises — propose-only, rehearsal-only, the tool allow-list, key
 * custody — are held **below** whatever runs the loop, so this adapter is a
 * translation and never a second opinion. What it translates is
 * `streamText`'s `stream` into the contract's events, in the same order the
 * built-in engine emits them, and `session`'s two capabilities into the SDK's
 * two extension points: a `fetch` for the model, a tool set for the runtime.
 *
 * Four defects the ADR records against this SDK are guarded here, and the
 * fifth is chunk 4's:
 *
 * 1. **`experimental_refineToolInput` is experimental.** Written as a plain
 *    literal, an upstream rename produces zero errors under this desk's
 *    TypeScript and the rehearsal rewrite fails **open** — measured, in the
 *    bake-off, at 0 errors. `REHEARSAL_HOOK` is written as a key of the SDK's
 *    own options type, so the day `ai` stops declaring it this file stops
 *    compiling.
 * 2. **The refusal path leaks unhandled rejections.** `AI_NoOutputGeneratedError`
 *    is raised inside the SDK's own recorded-stream flush and delivered to
 *    promises the caller cannot claim, so claiming the members this loop reads
 *    does not close it. A guard is installed for the duration of the run and
 *    removed after — the page's console is not this engine's to fill with an
 *    error nobody can catch.
 * 3. **The SDK reads the answer it asked for.** An endpoint that answers whole
 *    to a request that asked to stream ends the run with no output at all. That
 *    is closed one layer down, in `relay.ts`.
 * 4. **A thinking signature split across two stream events is truncated**
 *    (`vercel/ai#19663`, reproduced on the shipped release). Nothing here reads
 *    or writes a thinking block: the tier is reported unavailable and the
 *    session continues, and chunk 4 is where that defect has to be answered.
 *
 * What is deliberately **not** here: the thinking tier, the refutation pass,
 * `@ai-sdk/mcp` (the desk keeps its own MCP client, and its gate is on that
 * client's transport), and any writing at all.
 */
import { createAnthropic } from '@ai-sdk/anthropic'
import { createOpenAICompatible } from '@ai-sdk/openai-compatible'
import { dynamicTool, jsonSchema, stepCountIs, streamText } from 'ai'
import { MAX_TURNS, SYSTEM, extractProposal, textOf, thinkingUnavailable } from '../contract'
import { eventChannel } from './channel'
import { placeholderBase, relayFetch } from './relay'
import type { LanguageModel, ToolSet } from 'ai'
import type { AssistantEvent, AssistantSession, McpTool, McpToolResult } from '../../engine'

/**
 * The tool whose arguments the desk rehearses, named once.
 *
 * This is the one tool name in the adapter, and it is here because the SDK's
 * refinement hook is keyed by tool name. No schema is written anywhere in this
 * file: `session.tools` carries the runtime's own, and `jsonSchema()` passes
 * each through untouched, so the model is shown the contract the runtime
 * enforces or it is shown nothing.
 */
const REHEARSAL_TOOL = 'experimental_evaluate'

/**
 * **The single most important line in this adapter.**
 *
 * `streamText`'s options are declared with a `...settings` rest, so an options
 * object carrying a misspelled `experimental_refineToolInput` is accepted in
 * silence and the rehearsal rewrite is simply never applied. Naming the hook
 * once, as a key of the SDK's own options type, and using that constant as a
 * computed key makes the compiler the tripwire: the day `ai` renames or drops
 * the option, this line is an error rather than a guard that fails open.
 *
 * The ToolGate below it is why that is survivable rather than fatal — it is the
 * desk's own layer, on the wire, and `engines/vercel/engine.test.ts` proves the
 * rehearsal still reaches the runtime with this hook removed entirely.
 */
export const REHEARSAL_HOOK = 'experimental_refineToolInput' satisfies keyof Parameters<
  typeof streamText
>[0]

/** One model, built for the family the desk configured and nothing else. */
function modelFor(session: AssistantSession, signal: AbortSignal): LanguageModel {
  const fetch = relayFetch({ family: session.model.family, call: session.model.call, signal })
  const baseURL = placeholderBase(session.model.family)
  if (session.model.family === 'anthropic') {
    return createAnthropic({
      baseURL,
      // `createAnthropic` reaches `loadApiKey`, which **throws** in a browser
      // with no key rather than omitting the header. The placeholder never
      // leaves `relayFetch`: `x-api-key` is not on its protocol allow-list, the
      // desk's capability drops it again, and the chassis strips whatever it is
      // sent before injecting the configured key.
      apiKey: 'placeholder-the-desk-relay-injects-the-key',
      fetch
    })(session.model.model)
  }
  // `createOpenAICompatible` simply omits `Authorization` when it has no key.
  // That is a convenience: the allow-list in `relay.ts` is the guard.
  return createOpenAICompatible({ name: 'desk-endpoint', baseURL, fetch }).chatModel(
    session.model.model
  )
}

/** The runtime's tools, as the SDK's, with the runtime's own schemas. */
function toolsFor(
  tools: McpTool[],
  execute: (name: string, input: unknown) => Promise<McpToolResult>
): ToolSet {
  const set: ToolSet = {}
  for (const tool of tools) {
    set[tool.name] = dynamicTool({
      description: tool.description ?? '',
      // The served schema, as served. Nothing is re-typed through a schema
      // library, so the model sees the runtime's contract rather than this
      // adapter's reading of it.
      inputSchema: jsonSchema<unknown>(
        (tool.inputSchema ?? { type: 'object', properties: {} }) as Parameters<
          typeof jsonSchema
        >[0]
      ),
      execute: (input: unknown) => execute(tool.name, input)
    })
  }
  return set
}

/**
 * Whether a stream part is a call to a tool this session never offered.
 *
 * The SDK refuses such a call itself — a tool it was not handed has no
 * `execute` to reach — which is a second layer and a welcome one. It is not the
 * desk's layer: the guardrail line a person reads comes from the ToolGate, and
 * a gate that never saw the call has nothing to report. So the attempt is put
 * through `session.callTool` as the model made it, the gate refuses it on the
 * wire, and the refusal is reported by the layer that actually holds.
 */
function unofferedTool(part: { type: string }, offered: ReadonlySet<string>): string | undefined {
  if (part.type !== 'tool-error') return undefined
  const name = (part as { toolName?: unknown }).toolName
  if (typeof name !== 'string' || offered.has(name)) return undefined
  return name
}

/** What one tool call produced, in the shape the contract's event carries. */
function outcome(result: McpToolResult): { text: string; isError: boolean; structured?: unknown } {
  return {
    text: textOf(result),
    isError: Boolean(result.isError),
    structured: result.structuredContent
  }
}

/**
 * The `unhandledrejection` guard, installed for one run.
 *
 * ADR-0001: "the refusal path leaks unhandled `AI_NoOutputGeneratedError`
 * rejections the caller cannot claim; the page needs an `unhandledrejection`
 * guard". It is scoped to the run and **removed** after it, because a listener
 * that outlived the session would swallow the same error class for a page that
 * is no longer running an assistant at all. Only that one error name is
 * suppressed; every other rejection reaches the console as a real page error.
 */
export const SUPPRESSED_REJECTION = 'AI_NoOutputGeneratedError'

function guardRejections(): () => void {
  const scope = globalThis as unknown as {
    addEventListener?: (name: string, fn: (event: Event) => void) => void
    removeEventListener?: (name: string, fn: (event: Event) => void) => void
  }
  if (typeof scope.addEventListener !== 'function') return () => {}
  const guard = (event: Event) => {
    const reason = (event as { reason?: { name?: unknown } }).reason
    if (reason?.name === SUPPRESSED_REJECTION) event.preventDefault()
  }
  scope.addEventListener('unhandledrejection', guard)
  return () => scope.removeEventListener?.('unhandledrejection', guard)
}

/**
 * One sentence for a failure, with no address in it.
 *
 * The status and the endpoint's own sentence, where the SDK's error carries
 * them: "this desk has no key stored", "the endpoint refused the request" and
 * "the endpoint is broken" are different things to go and fix, and they are
 * indistinguishable once the body is thrown away. The **address is not** in it:
 * the SDK's error carries the placeholder URL, which says nothing useful and
 * reads as a real host.
 */
function describe(cause: unknown): string {
  const error = cause as {
    name?: string
    message?: string
    statusCode?: unknown
    responseBody?: unknown
  }
  if (typeof error?.statusCode === 'number') {
    const said = endpointSentence(error.responseBody)
    return `the model endpoint answered ${error.statusCode}${said === '' ? '' : `: ${said}`}`
  }
  return `${error?.name ?? 'Error'}: ${error?.message ?? String(cause)}`
}

/** `error.message`, `message` or a bare `error` string, out of an answer body. */
function endpointSentence(body: unknown): string {
  if (typeof body !== 'string') return ''
  try {
    const parsed = JSON.parse(body) as { error?: { message?: string } | string; message?: string }
    const said =
      typeof parsed?.error === 'string'
        ? parsed.error
        : (parsed?.error?.message ?? parsed?.message)
    if (typeof said === 'string') return said
  } catch {
    /* not JSON — the raw text is the best there is */
  }
  return body.slice(0, 200)
}

/**
 * Run one session, as a stream of events.
 *
 * `end` is emitted exactly once, from the `finally`, whatever happened above
 * it — including an abort, which ends the session and says nothing else,
 * because a viewer who pressed Stop has not been told about a failure.
 */
export async function* runVercel(session: AssistantSession): AsyncGenerator<AssistantEvent> {
  // The run's own controller, chained to the session's: the SDK is given this
  // one so a consumer that walks away ends the run, and the session's abort
  // reaches it the moment the viewer presses Stop.
  const stop = new AbortController()
  // **The channel aborts the run as it releases what it was holding**, because
  // the producer resumes on a microtask and would otherwise carry on into a
  // tool call for a session nobody is listening to any more.
  const channel = eventChannel({ onAbandon: () => stop.abort() })
  const onAbort = () => stop.abort()
  if (session.signal.aborted) stop.abort()
  else session.signal.addEventListener('abort', onAbort, { once: true })
  const release = guardRejections()
  const offered = new Set(session.tools.map((tool) => tool.name))
  // What the model asked for, before the SDK's refinement touched it.
  //
  // One queue and not a map, because the refinement hook below is registered for
  // exactly one tool: every entry here is a call to it, and they are taken in
  // the order the SDK refines and then executes them, so a step carrying two
  // evaluates still pairs each call with its own arguments.
  const asked: unknown[] = []

  const drive = async (): Promise<void> => {
    if (session.thinking.tier !== 'off') {
      await channel.push({
        type: 'thinking_unavailable',
        detail: thinkingUnavailable(session.thinking.tier, 'vercel')
      })
    }

    const tools = toolsFor(session.tools, async (name, input) => {
      // **The desk's gate is handed the call the model made.** The hook below
      // has already rewritten what the SDK carries; what leaves the page is
      // rewritten by the ToolGate, which is the layer that reports it. A gate
      // handed a call somebody else already fixed reports nothing, and the
      // guardrail line in the tab is the only place a person learns that the
      // rehearsal flag was forced.
      const args = (name === REHEARSAL_TOOL && asked.length > 0 ? asked.shift() : input) as Record<
        string,
        unknown
      >
      await channel.push({ type: 'tool_call', name, args })
      if (stop.signal.aborted) {
        // The run ended while this call was waiting to be reported — the viewer
        // pressed Stop, or the consumer stopped listening. A session nobody is
        // watching asks the runtime nothing more.
        const text = 'the session was stopped before this call was made; nothing was written'
        return { content: [{ type: 'text', text }], isError: true }
      }
      let answer: McpToolResult
      try {
        answer = await session.callTool(name, args)
      } catch (cause) {
        const text =
          `refused: ${(cause as Error).message}. This assistant proposes; it never ` +
          `writes a file and never calls a tool it was not offered.`
        await channel.push({ type: 'tool_result', name, isError: true, text })
        return { content: [{ type: 'text', text }], isError: true }
      }
      const said = outcome(answer)
      await channel.push({
        type: 'tool_result',
        name,
        isError: said.isError,
        text: said.text,
        ...(said.structured === undefined ? {} : { structured: said.structured })
      })
      return answer
    })

    let streamed: unknown = null
    const result = streamText({
      model: modelFor(session, stop.signal),
      instructions: SYSTEM,
      tools,
      messages: [{ role: 'user', content: session.prompt }],
      stopWhen: stepCountIs(MAX_TURNS),
      abortSignal: stop.signal,
      // **No retries, deliberately.** The SDK's default is two, with an
      // exponential backoff, and its retryable set includes 409 — which is the
      // status the *desk's own relay* answers with when no key is stored on
      // this machine. So a refusal a person has to go and fix became three
      // requests and six seconds of a spinner. The built-in engine makes one
      // request per turn and reports what came back; two engines that answer a
      // refusal differently is exactly what the slot exists to prevent.
      maxRetries: 0,
      // `streamText` does not throw: a refused request surfaces here and again
      // as an `error` part on the stream. Both are read, and whichever arrives
      // first is the one reported.
      onError: (event: { error: unknown }) => {
        streamed ??= event.error
      },
      // See REHEARSAL_HOOK. The key is the constant, never a literal.
      [REHEARSAL_HOOK]: {
        [REHEARSAL_TOOL]: (input: unknown) => {
          asked.push(input)
          const already = (input as { rehearsal?: unknown } | null)?.rehearsal === true
          return already ? input : { ...(input as object), rehearsal: true }
        }
      }
    })

    // Each of these rejects when the call fails, and a member left unclaimed is
    // a rejection nobody can catch. Claiming them does not close the leak — the
    // SDK mints derived promises of its own — which is what `guardRejections`
    // is for; it does close the ones this loop actually reads.
    const text = result.text
    void Promise.resolve(text).catch(() => undefined)

    // The **last** step's text, not every step's: a session that reasoned aloud
    // before calling a tool would otherwise have that prose concatenated onto
    // the message the proposal is read out of.
    let final = ''
    for await (const part of result.stream) {
      if (part.type === 'start-step') {
        final = ''
        continue
      }
      if (part.type === 'error') {
        streamed ??= (part as { error: unknown }).error
        continue
      }
      const unoffered = unofferedTool(part, offered)
      if (unoffered !== undefined) {
        // The SDK refused it; the desk's gate must be the one to say so.
        const input = (part as { input?: unknown }).input
        await channel.push({ type: 'tool_call', name: unoffered, args: input ?? {} })
        let refusal = 'the call did not leave the page and nothing was written'
        try {
          await session.callTool(unoffered, (input ?? {}) as Record<string, unknown>)
        } catch (cause) {
          refusal = (cause as Error).message
        }
        await channel.push({
          type: 'tool_result',
          name: unoffered,
          isError: true,
          text:
            `refused: ${refusal}. This assistant proposes; it never writes a file and ` +
            `never calls a tool it was not offered.`
        })
        continue
      }
      if (part.type === 'text-delta') final += (part as { text?: string }).text ?? ''
    }
    if (streamed !== null) throw streamed

    const proposal = extractProposal(final || (await text))
    await channel.push({
      type: 'proposal',
      document: proposal.document,
      unknowns: proposal.unknowns
    })
  }

  const loop = drive().catch(async (cause: unknown) => {
    // An abort is the viewer stopping the session, and it ends it: there is no
    // failure to report and nothing more to say than `end`.
    if (stop.signal.aborted || (cause as Error)?.name === 'AbortError') return
    await channel.push({ type: 'error', message: describe(cause) })
  })
  // Nothing else awaits this; a rejection out of the catch above would be
  // unhandled. It cannot reject, and this is what says so.
  const settled = loop.then(
    () => channel.close(),
    () => channel.close()
  )

  try {
    yield* channel.drain()
    await settled
  } finally {
    // A consumer that stopped listening releases whatever the framework's
    // pipeline is waiting to deliver, and the run is aborted rather than left
    // running behind a pane nobody is watching. The abort goes first, so the
    // loop that is about to be released finds a run already over.
    session.signal.removeEventListener('abort', onAbort)
    // `abandon` aborts the run before it releases anything; calling it again
    // where the drain already did is harmless and keeps this path honest.
    channel.abandon()
    stop.abort()
    // **Not awaited here.** On the ordinary path `settled` was awaited above,
    // where waiting is what it means. On this one the consumer has stopped
    // listening, and waiting for a loop that is itself waiting on a tool call
    // would make `return()` on this iterator as slow as the slowest thing the
    // session was doing.
    void settled
    release()
    yield { type: 'end' }
  }
}
