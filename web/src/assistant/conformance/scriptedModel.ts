/**
 * The bake-off's scripted model, in TypeScript, as a `fetch` stub.
 *
 * A port of `fixture/scripted_model.py` from
 * `docs/experiments/2026-09-05-assistant-engine-bakeoff`: the same step logic
 * (`count_openai`, `count_anthropic`, `choose_step`), the same two wire
 * formats, the same SSE grammars, the same repeat detector — and, from
 * `fixture/THINKING-SPEC.md`, the thinking gates per wire format, the
 * deterministic reasoning text and signatures, the `-nothink` degrade, the
 * split-signature probe and the critic script. What is dropped is the run-id
 * bookkeeping, which existed so several candidates could share one HTTP server.
 *
 * **Server mode is not the same as client request.** With thinking on, a
 * request that carries no thinking parameter is answered exactly as it would be
 * with thinking off, and the row records `thinkingRequested: false`. That
 * asymmetry *is* the measurement: an engine that cannot configure the parameter
 * produces a run where every row says `false`.
 *
 * **It decides the next step from the conversation, not from a counter it
 * keeps.** n is the number of scenario tool results already present in the
 * request's own messages; the answer is step n+1. So a session that dropped a
 * result, replayed a turn, or rebuilt its message array wrongly gets a
 * different script — which is what makes this a test of the engine's message
 * handling and not only of its parsing.
 *
 * It also **records every request**: the header names, the tool names offered,
 * the message count and the URL, which is what `requests.jsonl` carried in the
 * experiment and what K1 and K2 are read off here.
 */
import scenario from './scenario.json'

export interface ScenarioStep {
  id: string
  kind: string
  tool?: string
  arguments?: Record<string, unknown>
  text?: string
}

const STEPS = scenario.steps as ScenarioStep[]
const COUNTED = new Set(scenario.countedTools)

/** The literal the desk's critic carries. `assistant/refutation.ts` owns it. */
const MARKER = 'REFUTATION PASS'

/**
 * The thinking modes this endpoint can be stood up in.
 *
 * - `off` — phase A, byte-identical: no gate, no reasoning, nothing recorded
 *   but `thinkingRequested: false`.
 * - `on` — the gate, and deterministic reasoning behind it.
 * - `split` — `on`, with the Anthropic signature sent as two `signature_delta`
 *   events. Real Anthropic sends one; a re-chunking proxy is the case.
 * - `enabled-only` — refuses `thinking: {"type":"adaptive"}` with a 400 and
 *   accepts the token-budget spelling, which is the dialect fallback's leg.
 * - `nothink` — 400 `Unsupported parameter` to any request carrying a thinking
 *   parameter at all, which is the degrade's leg.
 */
export type ThinkingMode = 'off' | 'on' | 'split' | 'enabled-only' | 'nothink'

/** The reasoning one step carries, a pure function of its id. */
export function thinkText(step: { id: string; tool?: string }): string {
  return (
    `[scripted reasoning ${step.id}] The policy has three branches. ` +
    `Next I will call ${step.tool ?? 'no tool'} to make progress; ` +
    'I must not state a verdict of my own.'
  )
}

/**
 * The signature one step carries, a pure function of its id.
 *
 * Recomputed by the checker rather than compared against a value this endpoint
 * merely logged, so what a leg asserts is the signature that *should* have come
 * back and not the one this fixture happened to remember.
 */
export function thinkSignature(step: { id: string }): string {
  return btoa(`scripted-thinking-signature:${step.id}`)
}

/** The critic's three steps: a check, a rehearsal, and a sentence about them. */
function criticSteps(refuted: boolean): ScenarioStep[] {
  const validate = STEPS.find((step) => step.id === (refuted ? 'T4' : 'T5'))!
  const evaluate = STEPS.find((step) => step.id === 'T6')!
  return [
    // The document the desk proposed, or — on the refuted leg — the draft the
    // runtime already refused once. Both are recorded calls, so the runtime's
    // own answer comes back either way.
    { id: 'R1', kind: 'tool_call', tool: 'validate', arguments: validate.arguments },
    // **Written with no rehearsal member**, exactly as T6 is: the critic runs
    // inside the same ToolGate, so this arrives at the runtime rewritten or it
    // does not arrive at all.
    { id: 'R2', kind: 'tool_call', tool: 'experimental_evaluate', arguments: evaluate.arguments },
    {
      id: 'R3',
      kind: 'final_message',
      // **The prose disagrees with the runtime, on purpose, on both legs.** A
      // verdict read out of this sentence is the wrong verdict either way.
      text: refuted
        ? 'REFUTATION: none found. Everything checks out and the pack is ready.'
        : 'REFUTATION: I am fairly sure this pack is refuted and should not be used.'
    }
  ]
}

/** One request the page made, as the checker reads it. */
export interface RecordedRequest {
  url: string
  /** Lower-cased header names only. A value is never recorded, not even a length. */
  headerNames: string[]
  api: 'openai-compatible' | 'anthropic'
  streamRequested: boolean
  toolNames: string[]
  messageCount: number
  /** The step this answer carried. */
  step: string
  /** How many scenario tool results the request's own messages carried. */
  results: number
  /**
   * The body's own top-level members, sorted.
   *
   * Recorded so a leg can say **what the engine actually sent** rather than
   * only that the session worked: the two engines differ here — one puts
   * `stream_options` on an OpenAI-compatible request and the other a
   * `tool_choice` — and the conformance session holds them to what the two wire
   * formats define rather than to each other's spelling.
   */
  bodyMembers: string[]
  /** THINKING-SPEC §5. The request carried a parameter this endpoint reads as "on". */
  thinkingRequested: boolean
  /** The first match, with its path, verbatim. */
  thinkingParam: { path: string; value: unknown } | null
  /** True where this request is the desk's critic rather than its main loop. */
  refutation: boolean
  /** Anthropic only: every distinct signature carried back, in order. */
  signaturesCarried: string[]
  /** What this endpoint has emitted so far, recomputed from the step ids. */
  signaturesExpected: string[]
  /** Expected minus carried: the T-b gating row. */
  signaturesMissing: string[]
  /** Carried values that are a strict fragment of an expected one (#19663). */
  signaturesTruncated: string[]
  /** Signatures on a block with no thinking text at all (langchainjs#10744). */
  signaturesMalformed: string[]
  /** OpenAI only: the reasoning member names carried back, per assistant message. */
  reasoningIn: string[]
}

/** One thinking parameter this endpoint recognises, with where it was found. */
type ThinkingParam = { path: string; value: unknown } | null

const OFF_EFFORTS = new Set(['none', 'off', 'disabled'])

/** THINKING-SPEC §3.1, deliberately tolerant: five spellings, one slot tier. */
function openAiThinkingParam(body: Record<string, unknown>): ThinkingParam {
  const look = (holder: Record<string, unknown>, prefix: string): ThinkingParam => {
    const effort = holder.reasoning_effort
    if (typeof effort === 'string' && !OFF_EFFORTS.has(effort)) {
      return { path: `${prefix}reasoning_effort`, value: effort }
    }
    const reasoning = holder.reasoning
    if (reasoning !== undefined && reasoning !== null && reasoning !== false && reasoning !== 'none') {
      return { path: `${prefix}reasoning`, value: reasoning }
    }
    const thinking = holder.thinking as { type?: unknown } | undefined
    if (thinking !== undefined && thinking !== null && typeof thinking === 'object') {
      if (thinking.type === 'enabled' || thinking.type === 'adaptive') {
        return { path: `${prefix}thinking`, value: thinking }
      }
    }
    const template = holder.chat_template_kwargs as { enable_thinking?: unknown } | undefined
    if (template !== undefined && template !== null && Boolean(template.enable_thinking)) {
      return { path: `${prefix}chat_template_kwargs.enable_thinking`, value: template.enable_thinking }
    }
    return null
  }
  const top = look(body, '')
  if (top !== null) return top
  const extra = body.extra_body
  if (extra !== null && typeof extra === 'object') {
    return look(extra as Record<string, unknown>, 'extra_body.')
  }
  return null
}

/** THINKING-SPEC §2.1: a dict whose `type` asks for thinking, and nothing else. */
function anthropicThinkingParam(body: Record<string, unknown>): ThinkingParam {
  const thinking = body.thinking as { type?: unknown } | undefined
  if (thinking === undefined || thinking === null || typeof thinking !== 'object') return null
  if (thinking.type !== 'enabled' && thinking.type !== 'adaptive') return null
  return { path: 'thinking', value: thinking }
}

interface OpenAiMessage {
  role?: string
  tool_call_id?: string
  name?: string
  tool_calls?: { id?: string; function?: { name?: string } }[]
}

/**
 * Count results for counted tools in an OpenAI message list.
 *
 * A result is a `role: "tool"` message, and its tool is resolved through the
 * `tool_call_id` an earlier assistant message announced. A result whose call
 * cannot be resolved is counted anyway: nothing else in this scenario makes
 * tool results.
 */
export function countOpenAi(messages: unknown[]): number {
  const namesById = new Map<string, string>()
  let n = 0
  for (const raw of messages ?? []) {
    const message = raw as OpenAiMessage
    if (message?.role === 'assistant') {
      for (const call of message.tool_calls ?? []) {
        const id = call.id
        const name = call.function?.name
        if (id && name) namesById.set(id, name)
      }
    } else if (message?.role === 'tool' || message?.role === 'function') {
      const name = namesById.get(String(message.tool_call_id)) ?? message.name
      if (name === undefined || COUNTED.has(name)) n += 1
    }
  }
  return n
}

interface AnthropicBlock {
  type?: string
  id?: string
  name?: string
  tool_use_id?: string
}

/** Count `tool_result` blocks for counted tools in an Anthropic message list. */
export function countAnthropic(messages: unknown[]): number {
  const namesById = new Map<string, string>()
  let n = 0
  for (const raw of messages ?? []) {
    const content = (raw as { content?: unknown }).content
    if (!Array.isArray(content)) continue
    for (const item of content) {
      const block = item as AnthropicBlock
      if (block?.type === 'tool_use') {
        if (block.id && block.name) namesById.set(block.id, block.name)
      } else if (block?.type === 'tool_result') {
        const name = namesById.get(String(block.tool_use_id))
        if (name === undefined || COUNTED.has(name)) n += 1
      }
    }
  }
  return n
}

/**
 * The step at result-count n, with the experiment's repeat detector.
 *
 * A harness that dropped a refused call without returning any result would
 * re-trigger the same step for ever; after three identical repeats the script
 * advances, so a run that stalls ends rather than hanging the suite.
 */
export function chooseStep(state: { lastBase?: number; repeats: number }, n: number): ScenarioStep {
  const base = Math.min(n + 1, STEPS.length)
  if (state.lastBase === base) state.repeats += 1
  else {
    state.lastBase = base
    state.repeats = 1
  }
  const offset = state.repeats > 3 ? state.repeats - 3 : 0
  return STEPS[Math.min(base + offset, STEPS.length) - 1]!
}

/**
 * What a request carried back about thinking, from its own messages.
 *
 * The three lists are THINKING-SPEC §5's, and each names a real defect shape:
 * a signature the client dropped, one it truncated (`vercel/ai#19663`), and one
 * it kept beside a block whose text it lost (`langchainjs#10744`).
 */
function anthropicCarried(
  messages: unknown[],
  emitted: readonly string[]
): { carried: string[]; truncated: string[]; malformed: string[] } {
  const carried: string[] = []
  const truncated: string[] = []
  const malformed: string[] = []
  for (const message of messages ?? []) {
    const content = (message as { content?: unknown }).content
    if (!Array.isArray(content)) continue
    for (const item of content) {
      const block = item as { type?: string; thinking?: unknown; signature?: unknown }
      if (typeof block?.signature !== 'string' || block.signature === '') continue
      const signature = block.signature
      if (!carried.includes(signature)) carried.push(signature)
      if (typeof block.thinking !== 'string' || block.thinking === '') malformed.push(signature)
      const whole = emitted.find(
        (expected) =>
          expected !== signature &&
          expected.length > signature.length &&
          (expected.startsWith(signature) || expected.endsWith(signature))
      )
      if (whole !== undefined) truncated.push(signature)
    }
  }
  return { carried, truncated, malformed }
}

/** Which reasoning member names an OpenAI-compatible request carried back. */
function openAiCarried(messages: unknown[]): string[] {
  const names: string[] = []
  for (const message of messages ?? []) {
    const held = message as Record<string, unknown>
    if (held?.role !== 'assistant') continue
    for (const name of ['reasoning_content', 'reasoning']) {
      const said = held[name]
      if (typeof said === 'string' && said !== '' && !names.includes(name)) names.push(name)
    }
  }
  return names
}

function openAiAnswer(step: ScenarioStep, reasoning: boolean) {
  const message =
    step.kind === 'tool_call'
      ? {
          role: 'assistant',
          content: null,
          tool_calls: [
            {
              id: `call_${step.id}`,
              type: 'function',
              function: { name: step.tool, arguments: JSON.stringify(step.arguments ?? {}) }
            }
          ]
        }
      : { role: 'assistant', content: step.text }
  // **Both vendor names, identical text.** DeepSeek and vLLM < 0.18 send
  // `reasoning_content`; vLLM >= 0.18 and OpenRouter send `reasoning`. A
  // fixture that emitted one would score an engine on the fixture's choice.
  const thinking = reasoning
    ? { reasoning_content: thinkText(step), reasoning: thinkText(step) }
    : {}
  return {
    id: `chatcmpl-${step.id}`,
    object: 'chat.completion',
    model: 'scripted-model',
    choices: [
      {
        index: 0,
        message: { ...message, ...thinking },
        finish_reason: step.kind === 'tool_call' ? 'tool_calls' : 'stop',
        logprobs: null
      }
    ],
    usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 }
  }
}

function anthropicAnswer(step: ScenarioStep, reasoning: boolean) {
  const content: unknown[] =
    step.kind === 'tool_call'
      ? [{ type: 'tool_use', id: `toolu_${step.id}`, name: step.tool, input: step.arguments ?? {} }]
      : [{ type: 'text', text: step.text }]
  // The thinking block is **first**, before the tool use and before the text,
  // which is where the protocol puts it.
  if (reasoning) {
    content.unshift({
      type: 'thinking',
      thinking: thinkText(step),
      signature: thinkSignature(step)
    })
  }
  return {
    id: `msg_${step.id}`,
    type: 'message',
    role: 'assistant',
    model: 'scripted-model',
    content,
    stop_reason: step.kind === 'tool_call' ? 'tool_use' : 'end_turn',
    stop_sequence: null,
    usage: { input_tokens: 1, output_tokens: 1 }
  }
}

function chunks(text: string, size: number): string[] {
  if (!text) return ['']
  const out: string[] = []
  for (let index = 0; index < text.length; index += size) out.push(text.slice(index, index + size))
  return out
}

function openAiStream(step: ScenarioStep, reasoning: boolean): string {
  const id = `chatcmpl-${step.id}`
  const lines: string[] = []
  const frame = (choices: unknown[]) =>
    lines.push(
      `data: ${JSON.stringify({ id, object: 'chat.completion.chunk', model: 'scripted-model', choices })}\n\n`
    )
  frame([{ index: 0, delta: { role: 'assistant', content: null }, finish_reason: null }])
  // After the role frame and before the tool-call frames, under both names.
  if (reasoning) {
    for (const piece of chunks(thinkText(step), 48)) {
      frame([
        { index: 0, delta: { reasoning_content: piece, reasoning: piece }, finish_reason: null }
      ])
    }
  }
  if (step.kind === 'tool_call') {
    const args = JSON.stringify(step.arguments ?? {})
    frame([
      {
        index: 0,
        delta: {
          tool_calls: [
            {
              index: 0,
              id: `call_${step.id}`,
              type: 'function',
              function: { name: step.tool, arguments: '' }
            }
          ]
        },
        finish_reason: null
      }
    ])
    // Split across frames, exactly as an endpoint does: an engine that read
    // only the first frame's arguments would send an empty document.
    for (const piece of chunks(args, 256)) {
      frame([
        { index: 0, delta: { tool_calls: [{ index: 0, function: { arguments: piece } }] }, finish_reason: null }
      ])
    }
    frame([{ index: 0, delta: {}, finish_reason: 'tool_calls' }])
  } else {
    for (const piece of chunks(step.text ?? '', 96)) {
      frame([{ index: 0, delta: { content: piece }, finish_reason: null }])
    }
    frame([{ index: 0, delta: {}, finish_reason: 'stop' }])
  }
  lines.push('data: [DONE]\n\n')
  return lines.join('')
}

/**
 * **`message_delta` carries `usage`, because the protocol says it does.**
 *
 * The fixture used to send `{"type":"message_delta","delta":{"stop_reason":…}}`
 * and nothing else, which no Anthropic endpoint sends: the documented event
 * carries the message's running output-token count, and `stop_sequence` beside
 * the stop reason. The built-in engine never noticed, because it reads only
 * `content_block_*` and `message_stop` — so the gap was invisible until an
 * engine that validates the whole event grammar ran the same leg and refused
 * it. That is the fixture being wrong about the wire, not an engine being
 * fussy, and it is fixed here rather than branched around.
 */
function anthropicStream(step: ScenarioStep, reasoning: boolean, split: boolean): string {
  const lines: string[] = []
  const event = (name: string, object: unknown) =>
    lines.push(`event: ${name}\ndata: ${JSON.stringify(object)}\n\n`)
  event('message_start', {
    type: 'message_start',
    message: {
      id: `msg_${step.id}`,
      type: 'message',
      role: 'assistant',
      model: 'scripted-model',
      content: [],
      stop_reason: null,
      usage: { input_tokens: 1, output_tokens: 0 }
    }
  })
  event('ping', { type: 'ping' })
  // The thinking block is inserted at index 0 and everything else shifts, which
  // is the shape THINKING-SPEC §2.4 writes.
  let at = 0
  if (reasoning) {
    event('content_block_start', {
      type: 'content_block_start',
      index: at,
      content_block: { type: 'thinking', thinking: '' }
    })
    for (const piece of chunks(thinkText(step), 48)) {
      event('content_block_delta', {
        type: 'content_block_delta',
        index: at,
        delta: { type: 'thinking_delta', thinking: piece }
      })
    }
    const signature = thinkSignature(step)
    // **One event is the default**, because that is what Anthropic sends and
    // `vercel/ai#19663` was closed on exactly that argument. `split` is the
    // re-chunking proxy, and it is a separate leg rather than a gating row.
    const fragments = split
      ? [signature.slice(0, Math.floor(signature.length / 2)), signature.slice(Math.floor(signature.length / 2))]
      : [signature]
    for (const fragment of fragments) {
      event('content_block_delta', {
        type: 'content_block_delta',
        index: at,
        delta: { type: 'signature_delta', signature: fragment }
      })
    }
    event('content_block_stop', { type: 'content_block_stop', index: at })
    at += 1
  }
  if (step.kind === 'tool_call') {
    event('content_block_start', {
      type: 'content_block_start',
      index: at,
      content_block: { type: 'tool_use', id: `toolu_${step.id}`, name: step.tool, input: {} }
    })
    for (const piece of chunks(JSON.stringify(step.arguments ?? {}), 256)) {
      event('content_block_delta', {
        type: 'content_block_delta',
        index: at,
        delta: { type: 'input_json_delta', partial_json: piece }
      })
    }
    event('content_block_stop', { type: 'content_block_stop', index: at })
    event('message_delta', {
      type: 'message_delta',
      delta: { stop_reason: 'tool_use', stop_sequence: null },
      usage: { output_tokens: 1 }
    })
  } else {
    event('content_block_start', {
      type: 'content_block_start',
      index: at,
      content_block: { type: 'text', text: '' }
    })
    for (const piece of chunks(step.text ?? '', 96)) {
      event('content_block_delta', {
        type: 'content_block_delta',
        index: at,
        delta: { type: 'text_delta', text: piece }
      })
    }
    event('content_block_stop', { type: 'content_block_stop', index: at })
    event('message_delta', {
      type: 'message_delta',
      delta: { stop_reason: 'end_turn', stop_sequence: null },
      usage: { output_tokens: 1 }
    })
  }
  event('message_stop', { type: 'message_stop' })
  return lines.join('')
}

export interface ScriptedModel {
  /** Install as `globalThis.fetch`. */
  fetch: typeof fetch
  requests: RecordedRequest[]
}

/**
 * One scripted endpoint, as a `fetch` stub.
 *
 * `answerAs` decides what comes back over the wire: `stream` writes SSE and
 * `whole` writes one JSON object, whatever the request asked for. That is the
 * fourth leg of the conformance matrix — an endpoint that ignores `stream` is
 * a gateway that buffers, and the engine has to read the answer it got rather
 * than the answer it asked for.
 */
export function scriptedModel(options: {
  api: 'openai-compatible' | 'anthropic'
  answerAs: 'stream' | 'whole'
  /** THINKING-SPEC's mode for this endpoint. `off` is phase A, unchanged. */
  thinking?: ThinkingMode
  /**
   * Whether the critic's `validate` is asked about the draft the runtime
   * already refused.
   *
   * This is the **`refuted: true` branch** ADR-0001 records as never executed
   * outside a verifier mutation: the critic causes a real `validate` over
   * DRAFT_V1, the runtime says `invalid`, and the desk renders a refutation.
   */
  refuted?: boolean
}): ScriptedModel {
  const requests: RecordedRequest[] = []
  const state = { lastBase: undefined as number | undefined, repeats: 0 }
  const mode: ThinkingMode = options.thinking ?? 'off'
  const critic = criticSteps(options.refuted === true)
  /** The step ids this endpoint has emitted reasoning for, in order. */
  const emitted: string[] = []

  /** The 400 an endpoint with no thinking answers, in each protocol's shape. */
  const refuse = (message: string): Response =>
    new Response(
      JSON.stringify(
        options.api === 'anthropic'
          ? { type: 'error', error: { type: 'invalid_request_error', message } }
          : { error: { message, type: 'invalid_request_error', param: null, code: null } }
      ),
      { status: 400, headers: { 'content-type': 'application/json' } }
    )

  const stub = async (input: unknown, init?: RequestInit): Promise<Response> => {
    const url = String(input)
    const headers = (init?.headers ?? {}) as Record<string, string>
    const raw = String(init?.body ?? '{}')
    const body = JSON.parse(raw) as {
      messages?: unknown[]
      tools?: { name?: string; function?: { name?: string } }[]
      stream?: boolean
      [member: string]: unknown
    }
    const messages = body.messages ?? []
    const param =
      options.api === 'anthropic' ? anthropicThinkingParam(body) : openAiThinkingParam(body)

    // **The refutation pass is a second conversation**, and the marker is how
    // this endpoint tells the two apart. THINKING-SPEC §9.2.
    const refutation = raw.includes(MARKER)
    const n = options.api === 'anthropic' ? countAnthropic(messages) : countOpenAi(messages)
    const step = refutation
      ? critic[Math.min(n, critic.length - 1)]!
      : chooseStep(state, n)

    const carried =
      options.api === 'anthropic'
        ? anthropicCarried(messages, emitted.map((id) => thinkSignature({ id })))
        : { carried: [], truncated: [], malformed: [] }
    const expected = emitted.map((id) => thinkSignature({ id }))
    requests.push({
      url,
      headerNames: Object.keys(headers).map((name) => name.toLowerCase()),
      api: options.api,
      streamRequested: Boolean(body.stream),
      toolNames: (body.tools ?? []).map((tool) => String(tool.function?.name ?? tool.name ?? '')),
      messageCount: messages.length,
      step: step.id,
      results: n,
      bodyMembers: Object.keys(body as Record<string, unknown>).sort(),
      thinkingRequested: param !== null,
      thinkingParam: param,
      refutation,
      signaturesCarried: carried.carried,
      signaturesExpected: expected,
      signaturesMissing: expected.filter((signature) => !carried.carried.includes(signature)),
      signaturesTruncated: carried.truncated,
      signaturesMalformed: carried.malformed,
      reasoningIn: options.api === 'anthropic' ? [] : openAiCarried(messages)
    })

    // **The two refusals, before anything is answered.** `nothink` is the
    // degrade's leg — an endpoint that has no thinking at all — and
    // `enabled-only` is the dialect fallback's, an endpoint that has thinking
    // under the other spelling.
    if (mode === 'nothink' && param !== null) {
      return refuse(
        options.api === 'anthropic'
          ? 'thinking: Extra inputs are not permitted'
          : 'Unsupported parameter: reasoning_effort'
      )
    }
    if (
      mode === 'enabled-only' &&
      (param?.value as { type?: unknown } | undefined)?.type === 'adaptive'
    ) {
      return refuse('Adaptive thinking is not supported by this model')
    }

    // Thinking output must not change the step logic: neither a thinking block
    // nor a reasoning member is a tool result, so `chooseStep` is untouched.
    const reasoning = mode !== 'off' && mode !== 'nothink' && param !== null
    if (reasoning && !emitted.includes(step.id)) emitted.push(step.id)

    if (options.answerAs === 'whole') {
      const answer =
        options.api === 'anthropic'
          ? anthropicAnswer(step, reasoning)
          : openAiAnswer(step, reasoning)
      return new Response(JSON.stringify(answer), {
        status: 200,
        headers: { 'content-type': 'application/json' }
      })
    }
    const text =
      options.api === 'anthropic'
        ? anthropicStream(step, reasoning, mode === 'split')
        : openAiStream(step, reasoning)
    return new Response(text, {
      status: 200,
      headers: { 'content-type': 'text/event-stream' }
    })
  }

  return { fetch: stub as unknown as typeof fetch, requests }
}
