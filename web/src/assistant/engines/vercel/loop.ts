import { assistantLanguageInstructions } from '../../../i18n/assistantLanguage'
/**
 * The `vercel` engine's loop: the Vercel AI SDK v7, behind the desk's contract.
 *
 * ADR-0001 makes this the default engine and says what the slot buys: the
 * desk's promises — propose-only, rehearsal-only, the tool allow-list, key
 * custody — are held **below** whatever runs the loop, so this adapter is a
 * translation and never a second opinion. What it translates is
 * `streamText`'s `stream` into the contract's events, in the same order the
 * contract declares them, and `session`'s two capabilities into the SDK's
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
 *    reaches the page from a promise the result exposed and nobody claimed. It
 *    is handled by claiming result promises and disabling the unused browser
 *    telemetry path that leaves its completion promise unclaimed. Nothing on
 *    this page is suppressed — see `claimPromises`.
 * 3. **The SDK reads the answer it asked for.** An endpoint that answers whole
 *    to a request that asked to stream ends the run with no output at all. That
 *    is closed one layer down, in `relay.ts`.
 * 4. **A thinking signature split across two stream events is truncated**
 *    (`vercel/ai#19663`, reproduced on the shipped release). The desk cannot
 *    make the SDK reassemble one, so it **detects** the truncation instead: the
 *    fragments are ledgered as they arrive, the outgoing body is compared with
 *    them, a block whose signature came back as a fragment is removed rather
 *    than sent, and the session degrades once with the reason. See
 *    `relay.ts`'s `withoutTruncatedThinking`.
 * 5. **The tier is a call setting, applied per step.** `prepareStep` supplies
 *    it on every request with no agent to rebuild — but *what* it supplies is
 *    `assistant/thinking.ts`'s table, translated once by `sdkThinking` below,
 *    so the two engines put the same members on the wire.
 *
 * What is deliberately **not** here: `@ai-sdk/mcp` (the desk keeps its own MCP
 * client, and its gate is on that client's transport), and any writing at all.
 */
import { createAnthropic } from '@ai-sdk/anthropic'
import { createGoogleGenerativeAI } from '@ai-sdk/google'
import { createOpenAICompatible } from '@ai-sdk/openai-compatible'
import { dynamicTool, jsonSchema, stepCountIs, streamText } from 'ai'
import {
  MAX_TURNS,
  SYSTEM,
  CONVERSATION_SYSTEM,
  TEST_DESIGN_SYSTEM,
  BRIEF_SYSTEM,
  streamingProse,
  hasProposalFence,
  eventIterator,
  extractProposal,
  proseOf,
  guardedCallTool,
  isCancelled,
  narrowingEvents,
  openRun,
  schemasShown,
  servedSchemaFor,
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
import { refusedSchemaKeyword, refusedSchemaSentence } from '../../geminiSchema'
import { eventChannel } from './channel'
import { placeholderBase, relayFetch, signatureLedger, signatureOf } from './relay'
import type { LanguageModel, ToolSet } from 'ai'
import type { AssistantEngine, EndpointKind } from '../../../config/deskConfig'
import type { AssistantEvent, AssistantSession, McpTool, McpToolResult, HostTool } from '../../engine'
import type { CritiqueRecorder } from '../../refutation'
import type { ThinkingSlot } from '../../thinking'
import type { SignatureLedger } from './relay'

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

/**
 * The name the OpenAI-compatible provider is built under.
 *
 * It is also the key its `providerOptions` are read from — the SDK takes the
 * part of the provider id before the dot — so the two are one constant rather
 * than two strings somebody has to keep in step.
 */
export const ENDPOINT_NAME = 'desk-endpoint'

/**
 * The key the Google provider reads its options from.
 *
 * Not the desk's to choose: the provider takes the part of its own id before
 * the dot, and its id is `google.generative-ai` whatever base URL it is built
 * with. Written once so that the translation below and the engine's own suite
 * name the same string.
 */
export const GOOGLE_OPTIONS = 'google'

/**
 * The desk's wire members, in the SDK's own vocabulary.
 *
 * **A translation, and never a second table.** `assistant/thinking.ts` decides
 * what `on` means for a family and which Anthropic spelling is in force; this
 * says how to ask *this SDK* to put exactly those members on the request. The
 * mapping is asserted against the table in the engine's own suite, so a
 * spelling that stopped producing the desk's members is a red test rather than
 * a session that quietly thought at some other depth.
 *
 * `{}` where the slot carries nothing, so a run at `off` — or after a degrade —
 * is byte-identical to one from before this chunk.
 */
export function sdkThinking(
  family: EndpointKind,
  members: Record<string, unknown> | null
): Record<string, unknown> {
  if (members === null) return {}
  if (family === 'anthropic') {
    const thinking = members.thinking as { type?: string; budget_tokens?: number } | undefined
    const effort = (members.output_config as { effort?: string } | undefined)?.effort
    const anthropic: Record<string, unknown> = {}
    if (thinking?.type === 'adaptive') anthropic.thinking = { type: 'adaptive' }
    else if (thinking?.type === 'enabled') {
      anthropic.thinking = { type: 'enabled', budgetTokens: thinking.budget_tokens }
    }
    // **The maximum travels with the budget.** Anthropic spends the thinking
    // budget out of `max_tokens`, so the two are one decision and the table
    // makes it; this is the SDK's spelling of the number the table chose.
    const maximum = members.max_tokens
    if (typeof maximum === 'number') {
      return { providerOptions: { anthropic }, maxOutputTokens: maximum }
    }
    // The depth is a sibling on the wire and a sibling here: the provider puts
    // `effort` into `output_config`, which is the member the desk's table names.
    if (effort !== undefined) anthropic.effort = effort
    return { providerOptions: { anthropic } }
  }
  if (family === 'gemini') {
    // **The table already named it `thinkingConfig`, which is the SDK's own
    // spelling as well as the wire's.** So this is the shortest of the three
    // translations: the members go under the provider key the Google provider
    // reads its options from, and the SDK puts them in `generationConfig`
    // exactly where each protocol's own wire puts them.
    return { providerOptions: { [GOOGLE_OPTIONS]: { thinkingConfig: members.thinkingConfig } } }
  }
  return { providerOptions: { [ENDPOINT_NAME]: { reasoningEffort: members.reasoning_effort } } }
}

/** One model, built for the family the desk configured and nothing else. */
function modelFor(
  session: AssistantSession,
  signal: AbortSignal,
  ledger: SignatureLedger,
  onTruncated: (reason: string) => void,
  slot: ThinkingSlot
): LanguageModel {
  const fetch = relayFetch({
    family: session.model.family,
    call: session.model.call,
    signal,
    ledger,
    onTruncated,
    // Read after `onTruncated` has told the slot, so the rebuilt request
    // carries what the desk asks for now: after a truncation, nothing.
    membersNow: () => slot.members()
  })
  const baseURL = placeholderBase(session.model.family)
  if (session.model.family === 'gemini') {
    return createGoogleGenerativeAI({
      baseURL,
      // `createGoogleGenerativeAI` reaches `loadApiKey` the way
      // `createAnthropic` does and **throws** in a browser with no key rather
      // than omitting the header. The placeholder never leaves `relayFetch`:
      // `x-goog-api-key` is not on its protocol allow-list, the desk's
      // capability drops it again, and the chassis strips whatever it is sent
      // before injecting the configured key.
      apiKey: 'placeholder-the-desk-relay-injects-the-key',
      fetch
    })(session.model.model)
  }
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
  return createOpenAICompatible({ name: ENDPOINT_NAME, baseURL, fetch }).chatModel(
    session.model.model
  )
}

/**
 * The runtime's tools, as the SDK's, with the runtime's own schemas — and the
 * desk's host tools beside them, each executed by the desk's own function
 * rather than dispatched over the gate.
 */
function toolsFor(
  family: EndpointKind,
  tools: McpTool[],
  execute: (name: string, input: unknown) => Promise<McpToolResult>,
  hostTools: HostTool[] = [],
  executeHost: (tool: HostTool, input: unknown) => Promise<McpToolResult> = async () => ({
    content: [],
    isError: true
  })
): ToolSet {
  const set: ToolSet = {}
  for (const tool of hostTools) {
    set[tool.name] = dynamicTool({
      description: tool.description,
      inputSchema: jsonSchema<unknown>(
        servedSchemaFor(family, tool) as Parameters<typeof jsonSchema>[0]
      ),
      execute: (input: unknown) => executeHost(tool, input)
    })
  }
  for (const tool of tools) {
    set[tool.name] = dynamicTool({
      description: tool.description ?? '',
      // The served schema, as served — minus, on the one family whose wire
      // takes an OpenAPI subset, the closed removal list
      // `assistant/geminiSchema.ts` documents. Nothing is re-typed through a
      // schema library and nothing is written here where the runtime served
      // none — `servedSchema` refuses that, and the session ends rather than
      // showing the model a contract this desk invented.
      inputSchema: jsonSchema<unknown>(
        servedSchemaFor(family, tool) as Parameters<typeof jsonSchema>[0]
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
 * Claim the SDK result's promise-valued members before consuming its stream.
 * Reading a result getter creates a promise that can reject on a failed call.
 * Enumerate own properties and prototype getters so new SDK result fields do
 * not silently become unclaimed. Stream getters are excluded: reading those
 * creates unused tee/transform branches rather than promises.
 *
 * A separate browser-only leak in SDK 7.0.93 was traced to telemetry:
 * streamText passes `_totalUsage.promise.then(...)` into
 * openTelemetryChannelSpanContext, whose non-Node return leaves it unclaimed.
 * Both calls below explicitly disable that unused telemetry path. This fixes
 * Stop and endpoint-refusal errors at their source; no page-wide rejection
 * listener suppresses errors. The real-browser response check covers both.
 */
export function claimPromises(result: object): number {
  const names = new Set<string>()
  for (
    let held: object | null = result;
    held !== null;
    held = Object.getPrototypeOf(held) as object | null
  ) {
    for (const name of Object.getOwnPropertyNames(held)) names.add(name)
  }
  let claimed = 0
  for (const name of names) {
    // Stream getters create a new tee/transform, not a promise. Opening an
    // unconsumed stream just to inspect it adds a branch with no consumer.
    if (name === 'constructor' || name === 'stream' || name.endsWith('Stream')) continue
    let value: unknown
    try {
      value = (result as Record<string, unknown>)[name]
    } catch {
      // A member that throws on being read is not a promise to claim.
      continue
    }
    if (value === null || typeof value !== 'object') continue
    if (typeof (value as { then?: unknown }).then !== 'function') continue
    claimed += 1
    void Promise.resolve(value as PromiseLike<unknown>).catch(() => undefined)
  }
  return claimed
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
 * `end` is emitted exactly once, on the channel like every other event and
 * never from a `finally`, whatever happened above it — including an abort,
 * which ends the session and says nothing else, because a viewer who pressed
 * Stop has not been told about a failure. A consumer that stops listening is
 * owed no terminal event and gets none: its `return()` closes the generator.
 */
export function runVercel(
  session: AssistantSession,
  /** The id the registry loaded this engine under. See `runBuiltin`. */
  id: AssistantEngine
): AsyncIterable<AssistantEvent> {
  // The run's own controller, chained to the session's: the SDK is given this
  // one so a consumer that walks away ends the run, and the session's abort
  // reaches it the moment the viewer presses Stop.
  /**
   * One gate, reached four ways.
   *
   * The consumer's `return()`, its `throw()`, the session's own signal and the
   * run's natural end all close it — once, synchronously, marking the run closed
   * *before* it aborts or releases anything, because what a released loop
   * produces next must already be nobody's. A session already aborted when this
   * is called closes it before a provider is built or a request is made.
   */
  /**
   * The channel and the gate, in the only order that works on the one path
   * this is all for.
   *
   * A session already aborted closes the gate the moment it is opened, and
   * closing it abandons the channel — so the channel has to exist first. But
   * abandoning calls back into the gate, which does not exist yet. Both are
   * true, and neither is a problem: during that first synchronous close the
   * hook below is still the no-op, which is exactly right, because the thing it
   * would have called is already closing.
   */
  let closeRun = () => {}
  const channel = eventChannel({ onAbandon: () => closeRun() })
  const gate = openRun(session, () => channel.abandon())
  closeRun = gate.close
  // The runtime, reachable only while the run is: read before dispatch, so
  // nothing reaches `jpack mcp` after the consumer has left, and bounded, so a
  // call in flight cannot hold the cleanup that is ending it.
  const callTool = guardedCallTool(session, gate.signal)
  // The tier, held by the desk. This adapter asks it for members and pushes
  // whatever notice it hands back; it never decides what a refusal meant.
  const slot = openThinking(session)
  const ledger = signatureLedger()
  /**
   * The truncation notice, **delivered at the transition rather than owed**.
   *
   * The slot goes unavailable the instant this is called and the request that
   * leaves is already the degraded one, so a notice held until the next stream
   * part arrived left the tab saying `thinking on` about a request that carried
   * none — for as long as that request took, or for ever if it hung. The relay
   * awaits this, so the line is on the stream before the request goes.
   */
  const onTruncated = async (reason: string): Promise<void> => {
    const said = slot.truncated(reason)
    if (said !== null) await channel.push(said)
  }
  // Both the runtime's and the desk's: an SDK refusal of a host tool's input
  // is the host tool's to report, never a call for the gate. A host tool
  // named like a runtime tool would shadow it in the SDK's set, so a
  // collision ends the run before the model is shown either.
  const offered = new Set([...session.tools, ...session.hostTools].map((tool) => tool.name))
  const collision = session.hostTools.find((tool) => session.tools.some((served) => served.name === tool.name))
  // What the model asked for, before the SDK's refinement touched it.
  //
  // One queue and not a map, because the refinement hook below is registered for
  // exactly one tool: every entry here is a call to it, and they are taken in
  // the order the SDK refines and then executes them, so a step carrying two
  // evaluates still pairs each call with its own arguments.
  const asked: unknown[] = []

  /**
   * One whole attempt at the session.
   *
   * `produced` counts what this attempt already put on the stream, because the
   * only refusal this desk retries is one that arrived **before anything was
   * delivered** — a 400 on the very first request. A tier refusal after a tool
   * call has run is not a session to start again; it is reported like any other
   * failure.
   */
  const runOnce = async (produced: { count: number }): Promise<void> => {
    /**
     * One event on the stream, counted.
     *
     * The count is what makes the retry below safe: an attempt that delivered
     * nothing can be started again, and one that delivered anything cannot.
     */
    const deliver = async (event: AssistantEvent): Promise<void> => {
      if (event.type !== 'message_progress' || event.text !== '') produced.count += 1
      await channel.push(event)
    }
    /**
     * The critic's recorder, while the critic is running.
     *
     * The tool dispatch below is the **same** one the main loop uses — same
     * gate, same events, same rehearsal hook — so the pass does not get a
     * dispatch of its own to be outside anything with. What changes for the
     * length of the pass is that the answers are also shown to the recorder.
     */
    let recording: CritiqueRecorder | null = null
    // A fresh attempt takes nothing from the one before it.
    asked.length = 0
    const tools = toolsFor(session.model.family, session.tools, async (name, input) => {
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
      const correlation = session.interactive ? { callId: crypto.randomUUID() } : {}
      await deliver({ type: 'tool_call', ...correlation, name, args })
      let answer: McpToolResult
      try {
        answer = await callTool(name, args)
      } catch (cause) {
        // A run that ended while this was waiting — or before it was
        // dispatched — is the session unwinding, not a tool refusing. The
        // runtime was not asked and the model is told nothing more.
        if (isCancelled(cause)) {
          const text = 'the session was stopped before this call was made; nothing was written'
          return { content: [{ type: 'text', text }], isError: true }
        }
        const text =
          `refused: ${(cause as Error).message}. This assistant proposes; it never ` +
          `writes a file and never calls a tool it was not offered.`
        await deliver({ type: 'tool_result', ...correlation, name, isError: true, text })
        return { content: [{ type: 'text', text }], isError: true }
      }
      const said = outcome(answer)
      await deliver({
        type: 'tool_result',
        ...correlation,
        name,
        isError: said.isError,
        text: said.text,
        ...(said.structured === undefined ? {} : { structured: said.structured })
      })
      // **Only what the runtime answered**, and this is the only place it is
      // reached: the refusal path above returns before it, so a call the gate
      // refused is a `guardrail` line and never a check.
      recording?.saw(name, said.text)
      return answer
    }, session.hostTools, async (tool, input) => {
      // A host tool runs on the page, under the run's own signal: a stopped run
      // stops it, and its answer is reported in the same two events a runtime
      // answer earns. It is never a check the critic counts — the runtime's
      // verdicts are the runtime's — and it never reaches the recorder.
      const args = (input ?? {}) as Record<string, unknown>
      const correlation = session.interactive ? { callId: crypto.randomUUID() } : {}
      await deliver({ type: 'tool_call', ...correlation, name: tool.name, args })
      let answer: McpToolResult
      try {
        answer = await withAbort(() => tool.execute(args, gate.signal), gate.signal)
      } catch (cause) {
        if (isCancelled(cause)) {
          const text = 'the session was stopped before this call was made; nothing was written'
          return { content: [{ type: 'text', text }], isError: true }
        }
        const text = `refused: ${(cause as Error).message}`
        await deliver({ type: 'tool_result', ...correlation, name: tool.name, isError: true, text })
        return { content: [{ type: 'text', text }], isError: true }
      }
      const said = outcome(answer)
      await deliver({
        type: 'tool_result',
        ...correlation,
        name: tool.name,
        isError: said.isError,
        text: said.text,
        ...(said.structured === undefined ? {} : { structured: said.structured })
      })
      return answer
    })

    // One model and one refinement hook, shared by the loop and the critic:
    // the pass runs on the same everything, which is what makes "inside the
    // same ToolGate" structural rather than a habit.
    const model = modelFor(session, gate.signal, ledger, onTruncated, slot)
    // See REHEARSAL_HOOK. The key is the constant, never a literal.
    const refine = {
      [REHEARSAL_TOOL]: (input: unknown) => {
        asked.push(input)
        const already = (input as { rehearsal?: unknown } | null)?.rehearsal === true
        return already ? input : { ...(input as object), rehearsal: true }
      }
    }

    let streamed: unknown = null
    const result = streamText({
      // See claimPromises: the unused browser tracing path leaks on failure.
      telemetry: { isEnabled: false },
      model,
      instructions: (session.purpose === 'brief' ? BRIEF_SYSTEM : session.purpose === 'test-design' ? TEST_DESIGN_SYSTEM : session.allowConversation ? CONVERSATION_SYSTEM : SYSTEM) + assistantLanguageInstructions(session.replyLanguage),
      tools,
      messages: [{ role: 'user', content: session.prompt }],
      stopWhen: stepCountIs(MAX_TURNS),
      abortSignal: gate.signal,
      // **The tier, per step, with no agent to rebuild.** ADR-0001 credits this
      // SDK with exactly that, and the members are the desk's table's — read
      // fresh on every step, so a degrade or a dialect fallback takes effect on
      // the next request rather than at the next session.
      prepareStep: () => sdkThinking(session.model.family, slot.members()),
      // **No retries, deliberately.** The SDK's default is two, with an
      // exponential backoff, and its retryable set includes 409 — which is the
      // status the *desk's own relay* answers with when no key is stored on
      // this machine. So a refusal a person has to go and fix became three
      // requests and six seconds of a spinner. This adapter makes one
      // request per turn and reports what came back; two engines that answer a
      // refusal differently is exactly what the slot exists to prevent.
      maxRetries: 0,
      // `streamText` does not throw: a refused request surfaces here and again
      // as an `error` part on the stream. Both are read, and whichever arrives
      // first is the one reported.
      onError: (event: { error: unknown }) => {
        streamed ??= event.error
      },
      [REHEARSAL_HOOK]: refine
    })

    // **Before anything is awaited, and the only claim there is.** Every
    // promise-valued member of the result is claimed the moment the result
    // exists, so a call that fails produces no rejection the page cannot catch.
    // Nothing below reads one of those members without awaiting it, which is
    // the other half of the same rule. See `claimPromises`.
    claimPromises(result)

    // The **last** step's text, not every step's: a session that reasoned aloud
    // before calling a tool would otherwise have that prose concatenated onto
    // the message the proposal is read out of.
    let final = ''
    /** How many steps have started, so a turn boundary can be recognised. */
    let steps = 0
    /** One reasoning passage, accumulated so `done` can carry the whole of it. */
    let reasoning = ''
    // **Read through the run's signal, not only the SDK's.** `abortSignal`
    // makes the SDK end its own stream; this makes the *read* end whatever the
    // SDK decides to do, which is the same rule every other await in this loop
    // is held to.
    const parts = result.stream[Symbol.asyncIterator]()
    for (;;) {
      const step = await withAbort(() => parts.next(), gate.signal)
      if (step.done === true) break
      const part = step.value
      if (part.type === 'start-step') {
        // A step boundary is the end of the turn before it. ADR-0001's second
        // unmeasurable state — "no thinking block after the first turn" — is
        // decided here, by the desk, and said once.
        if (steps > 0) {
          // `final` still holds the step that just ended: whether it produced
          // an answer of its own is half the evidence a turn carries.
          const noticed = slot.turnEnded(final !== '')
          if (noticed !== null) await deliver(noticed)
        }
        // A turn boundary is where a reasoning block's signature is finished.
        // See `signatureLedger`.
        ledger.boundary()
        steps += 1
        final = ''
        if (session.interactive) await deliver({ type: 'message_progress', text: '' })
        continue
      }
      if (part.type === 'error') {
        streamed ??= (part as { error: unknown }).error
        continue
      }
      // **Reasoning is reported whatever the tier is.** The tier is what this
      // desk *asks* for, and this chunk asks for nothing; but a model that
      // always thinks reasons anyway, and an endpoint that returns
      // `reasoning_content` on its own default behaviour is the ordinary case
      // rather than the exotic one. An adapter that dropped those parts would
      // be deciding, on the desk's behalf, that what the model said about its
      // own reasoning is not worth showing.
      if (part.type === 'reasoning-delta') {
        const text = (part as { text?: string }).text ?? ''
        reasoning += text
        // Every signature fragment, as it arrives, wherever this family's
        // provider puts one. See `signatureLedger` and `signatureOf`.
        const carried = signatureOf(session.model.family, part)
        if (carried !== undefined) {
          ledger.fragment(String((part as { id?: unknown }).id ?? ''), carried)
        }
        if (text !== '') await deliver({ type: 'reasoning', text, done: false })
        continue
      }
      if (part.type === 'reasoning-end') {
        // The whole of it, once, so a reader has the passage rather than the
        // pieces — which is the shape the contract's `done` marks.
        await deliver({ type: 'reasoning', text: reasoning, done: true })
        reasoning = ''
        slot.sawReasoning()
        continue
      }
      // **A signed function call is ledgered too.** On the Gemini wire the
      // signature that matters most rides on the first `functionCall` part of a
      // turn rather than on a summary, and the SDK surfaces it in the same
      // provider metadata. Read off `tool-call` and not off `tool-input-*`,
      // which repeat the same value three times before it.
      if (part.type === 'tool-call') {
        const onCall = signatureOf(session.model.family, part)
        if (onCall !== undefined) {
          ledger.fragment(String((part as { toolCallId?: unknown }).toolCallId ?? ''), onCall)
        }
      }
      const unoffered = unofferedTool(part, offered)
      if (unoffered !== undefined) {
        // The SDK refused it; the desk's gate must be the one to say so.
        const input = (part as { input?: unknown }).input
        await deliver({ type: 'tool_call', name: unoffered, args: input ?? {} })
        let refusal = 'the call did not leave the page and nothing was written'
        try {
          await callTool(unoffered, (input ?? {}) as Record<string, unknown>)
        } catch (cause) {
          // A cancelled run stops here rather than reporting a refusal that
          // never happened.
          if (isCancelled(cause)) throw cause
          refusal = (cause as Error).message
        }
        await deliver({
          type: 'tool_result',
          name: unoffered,
          isError: true,
          text:
            `refused: ${refusal}. This assistant proposes; it never writes a file and ` +
            `never calls a tool it was not offered.`
        })
        continue
      }
      if (part.type === 'text-delta') {
        final += (part as { text?: string }).text ?? ''
        if (session.interactive) await deliver({ type: 'message_progress', text: streamingProse(final) })
      }
    }
    if (streamed !== null) throw streamed
    // The last turn's own accounting.
    const noticed = slot.turnEnded(final !== '')
    if (noticed !== null) await deliver(noticed)

    // `result.text` mints a fresh promise on every read, so it is read here,
    // where it is awaited, and nowhere it would be left standing.
    const finalText = final !== '' ? final : await withAbort(() => Promise.resolve(result.text), gate.signal)
    if (session.allowConversation && !hasProposalFence(finalText)) {
      if (!finalText.trim()) throw new Error('The assistant returned an empty reply')
      await deliver({ type: 'message', text: proseOf(finalText) })
      return
    }
    const proposal = extractProposal(finalText)
    // What the model said beside the document, as it said it.
    const prose = proseOf(finalText)
    if (prose !== '') await deliver({ type: 'message', text: prose })

    /**
     * The refutation pass: a second `streamText`, which is this SDK's own
     * subagent shape, on the same model, the same tool set and the same
     * refinement hook.
     *
     * It runs after the proposal exists and **before** the proposal event is
     * delivered, and every call it makes is a read.
     */
    let critique = null as ReturnType<CritiqueRecorder['critique']> | null
    // **No runtime prompt, no critic.** The instructions are the runtime's; this
    // desk adds one sentence and has none of its own to fall back on.
    const review = session.adversarialReview ?? slot.runsRefutation()
    const cannot = review ? criticCannotRun(session.testPrompt) : null
    if (cannot !== null) {
      critique = cannot
      await deliver(critiqueEvent(cannot))
    } else if (review) {
      const recorder = openCritique()
      recording = recorder
      // A fresh conversation: its history carries none of the loop's blocks.
      ledger.conversation()
      let criticText = ''
      let criticReasoning = ''
      try {
        const critic = streamText({
          telemetry: { isEnabled: false },
          model,
          instructions: CRITIC_SYSTEM + assistantLanguageInstructions(session.replyLanguage),
          tools,
          messages: [
            { role: 'user', content: criticMessage(session.testPrompt, proposal.document) }
          ],
          stopWhen: stepCountIs(MAX_CRITIC_TURNS),
          abortSignal: gate.signal,
          maxRetries: 0,
          onError: (event: { error: unknown }) => {
            streamed ??= event.error
          },
          prepareStep: () => sdkThinking(session.model.family, slot.members()),
          [REHEARSAL_HOOK]: refine
        })
        claimPromises(critic)
        const criticParts = critic.stream[Symbol.asyncIterator]()
        for (;;) {
          const step = await withAbort(() => criticParts.next(), gate.signal)
          if (step.done === true) break
          const part = step.value
          if (part.type === 'start-step') {
            ledger.boundary()
            // The critic's turns are this session's turns.
            const said = slot.turnEnded(criticText !== '')
            if (said !== null) await deliver(said)
            criticText = ''
            continue
          }
          if (part.type === 'error') {
            streamed ??= (part as { error: unknown }).error
            continue
          }
          if (part.type === 'reasoning-delta') {
            const text = (part as { text?: string }).text ?? ''
            criticReasoning += text
            const carried = signatureOf(session.model.family, part)
            if (carried !== undefined) {
              ledger.fragment(String((part as { id?: unknown }).id ?? ''), carried)
            }
            if (text !== '') await deliver({ type: 'reasoning', text, done: false })
            continue
          }
          if (part.type === 'reasoning-end') {
            await deliver({ type: 'reasoning', text: criticReasoning, done: true })
            criticReasoning = ''
            slot.sawReasoning()
            continue
          }
          if (part.type === 'tool-call') {
            const onCall = signatureOf(session.model.family, part)
            if (onCall !== undefined) {
              ledger.fragment(String((part as { toolCallId?: unknown }).toolCallId ?? ''), onCall)
            }
            continue
          }
          if (part.type === 'text-delta') criticText += (part as { text?: string }).text ?? ''
        }
      } finally {
        // Whatever happened, the main loop's dispatch stops feeding a recorder
        // nobody is reading.
        recording = null
      }
      const ended = slot.turnEnded(criticText !== '')
      if (ended !== null) await deliver(ended)
      if (streamed !== null) throw streamed
      critique = recorder.critique(criticText)
      await deliver(critiqueEvent(critique))
    }

    await deliver({
      type: 'proposal',
      document: proposal.document,
      unknowns: proposal.unknowns,
      ...critiqueOnProposal(critique)
    })
  }

  /**
   * The status and the endpoint's own sentence out of an SDK error, or nothing.
   *
   * The desk's thinking slot needs both to decide whether a refusal was about
   * the tier it sent. Neither is quoted past that: the sentence a person reads
   * is `describe`'s, and the endpoint's body reaches the closed list of
   * patterns in `assistant/thinking.ts` and goes no further.
   */
  const refusalOf = (cause: unknown): { status: number; message: string } | null => {
    const error = cause as { statusCode?: unknown; responseBody?: unknown; cause?: unknown }
    const status =
      typeof error?.statusCode === 'number'
        ? error.statusCode
        : typeof (error?.cause as { statusCode?: unknown } | undefined)?.statusCode === 'number'
          ? ((error.cause as { statusCode: number }).statusCode)
          : undefined
    if (status === undefined) return null
    const body = error.responseBody ?? (error.cause as { responseBody?: unknown } | undefined)?.responseBody
    return { status, message: endpointSentence(body) }
  }

  /**
   * The refusal a served schema earned, or nothing.
   *
   * Two conditions and both required, exactly as the tier's classifier has: a
   * 400 whose message names a keyword this desk **actually sent**. Anything
   * else is the failure it already was, reported unchanged.
   */
  const schemaRefusal = (cause: unknown): Error | null => {
    // **Only on the family whose wire takes a schema subset.** The classifier
    // matches a substring, so on the other two families a 400 saying
    // "unsupported response type" names `type` and the
    // real failure was being rewritten into a sentence about a removal list that
    // has nothing to do with it.
    if (session.model.family !== 'gemini') return null
    const refusal = refusalOf(cause)
    if (refusal === null) return null
    const keyword = refusedSchemaKeyword(
      refusal.status,
      refusal.message,
      schemasShown(session.model.family, session.tools)
    )
    return keyword === '' ? null : new Error(refusedSchemaSentence(keyword))
  }

  /**
   * The session, with the one retry a tier refusal earns.
   *
   * **Only before anything has been delivered.** An endpoint that has no
   * thinking answers 400 to the first request, and starting that request again
   * costs one call and no work; an endpoint that refused the tier half way
   * through a session is not one whose session can be replayed, and it is
   * reported like any other failure. Three attempts at most — the tier as
   * configured, the other Anthropic spelling, and the plain request — and the
   * slot itself says `other` once it carries no members, so this cannot spin.
   */
  const drive = async (): Promise<void> => {
    // A host tool named like a runtime tool would shadow it in the SDK's
    // set: reported as this run's error, before the model is shown either.
    if (collision !== undefined) throw new Error(`host tool ${collision.name} is named like a tool the runtime serves; nothing was run`)
    // **Said before the model is asked anything, and pushed rather than
    // delivered.** These are a property of the session and not of an attempt:
    // counting them as delivered would spend the one retry a tier refusal
    // earns, on every run that narrows a schema at all — and pushing them here
    // rather than from `open` keeps them in order with everything after them.
    for (const notice of narrowingEvents(id, session.model.family, [...session.tools, ...session.hostTools])) {
      await channel.push(notice)
    }
    for (let attempt = 1; ; attempt += 1) {
      const produced = { count: 0 }
      try {
        await runOnce(produced)
        return
      } catch (cause) {
        const refusal = refusalOf(cause)
        if (refusal === null || produced.count > 0 || attempt >= 3) throw schemaRefusal(cause) ?? cause
        const said = slot.refused(refusal.status, refusal.message)
        // **A schema keyword the removal list does not name is reported, not
        // stripped.** `assistant/geminiSchema.ts` holds the ruling; this is the
        // one line that carries it on this engine — a 400 naming a keyword this
        // desk actually sent becomes an error a person can act on, rather than
        // the desk widening its idea of the runtime's contract on being
        // refused.
        if (said.kind === 'other') throw schemaRefusal(cause) ?? cause
        if (said.kind === 'degrade' && said.event !== null) await channel.push(said.event)
      }
    }
  }

  /**
   * The contract's terminal event, on the same ordered stream as the rest.
   *
   * **Not delivered by the iterator's `return()` and not yielded from any
   * `finally`.** Both are the same mistake wearing different hats: a consumer
   * that has stopped listening is owed no terminal event, and an answer of
   * `{ done: false }` to a `return()` hands it a value `for await` discards. So
   * `end` travels the way every other event does — pushed, delivered in order,
   * and once — and where the consumer left early the channel is already
   * abandoned, this push resolves at once, and the event is dropped.
   */
  const finish = async (): Promise<void> => {
    await channel.push({ type: 'end' })
    channel.close()
  }
  /**
   * The run itself starts on the consumer's first `next()`.
   *
   * A consumer that opens a session and leaves without asking for an event has
   * asked the model nothing, and this is what makes that true.
   */
  const open = (): AsyncGenerator<AssistantEvent> => {
    const loop = drive().catch(async (cause: unknown) => {
      // A cancelled run is the viewer stopping the session, or a consumer
      // walking away: it ends the run and there is no failure to report.
      if (isCancelled(cause) || gate.signal.aborted) return
      await channel.push({ type: 'error', message: describe(cause) })
    })
    // Nothing else awaits this; a rejection out of the catch above would be
    // unhandled. It cannot reject, and this is what says so.
    void loop.then(finish, finish)
    return channel.drain()
  }

  return eventIterator({ gate, open })
}
