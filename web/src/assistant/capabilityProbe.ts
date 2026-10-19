import type { ModelCall } from '@judgment-pack/harness/engine'
import type { CapabilityFacts } from '../config/deskConfig'
import { LISTING_SUFFIX } from './modelListing'

/**
 * What one small probe of an OpenAI-compatible endpoint finds, to fill the
 * capability record the Desk answers the harness with (`@judgment-pack/harness`,
 * `endpointCapabilities`, v0.1.0 and after).
 *
 * **One chat request and one read of the endpoint's model listing, both through
 * the relay**, so the endpoint is reached by the connection's own saved
 * address and key and by nothing the page names (the capability `bindModelCall`
 * gives an engine). The chat request offers one tool and asks for two calls of
 * it in one reply, and the answer shows:
 * - **native tool calls**: the reply carries `tool_calls`; an endpoint that
 *   refuses the `tools` member (a 400 or 422) or answers in prose has none, and
 *   the harness then describes the tools in the instructions and reads one JSON
 *   call out of the answer (its text-path fallback);
 * - **parallel tool calls**: it carried two;
 * - **the member its reasoning arrives in**: `reasoning_content` or `reasoning`
 *   on the message.
 * The listing may carry the model's context window and output cap under the
 * names the common servers use (`max_model_len`, `context_length`, …), and when
 * it does the window and the reserve are filled from it.
 *
 * **What it does not find, it leaves out** rather than guessing: whether the
 * model is shown images, which reasoning dialect the endpoint takes, whether it
 * narrows tool schemas, and whether its chat template keeps a strict turn order
 * are not visible in one request, and stay the family's default until their
 * owner says otherwise in the connection form.
 *
 * **Only counts, names and numbers are read out of the answers**, never their
 * text: the page keeps and shows nothing an endpoint wrote.
 */
export const PROBE_TOOL = 'probe_echo'

export interface ProbeFindings {
  /** The time the probe ran, as an RFC 3339 timestamp. */
  at: string
  facts: CapabilityFacts
  /** What the probe asked about and could not tell, in the order asked. */
  unknown: string[]
}

export type ProbeOutcome =
  | { ok: true; findings: ProbeFindings }
  | { ok: false; problem: 'refused' | 'unreachable' | 'unreadable'; status?: number }

/** The one request the probe sends. Small: one message, one tool, a reply bounded at 128 tokens. */
export function probeBody(model: string): string {
  return JSON.stringify({
    model,
    stream: false,
    max_tokens: 128,
    tool_choice: 'auto',
    messages: [{ role: 'user', content: `Call the function ${PROBE_TOOL} twice in one reply: once with value 1 and once with value 2. Reply with the calls only.` }],
    tools: [{ type: 'function', function: { name: PROBE_TOOL, description: 'Echo a number.', parameters: { type: 'object', properties: { value: { type: 'integer' } }, required: ['value'], additionalProperties: false } } }]
  })
}

const object = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value)
const whole = (value: unknown, low: number, high: number): value is number => Number.isInteger(value) && Number(value) >= low && Number(value) <= high

/** The names the common servers give a model's window and output cap in their listing. */
const WINDOW_NAMES = ['max_model_len', 'context_length', 'context_window', 'max_context_length', 'n_ctx', 'max_input_tokens'] as const
const OUTPUT_NAMES = ['max_output_tokens', 'max_completion_tokens'] as const

/** What the chat answer shows, as facts; `undefined` for a member it cannot show. */
export function readChatAnswer(answer: unknown): Pick<CapabilityFacts, 'nativeToolCalls' | 'parallelToolCalls' | 'reasoningField'> | undefined {
  if (!object(answer) || !Array.isArray(answer.choices)) return undefined
  const first = answer.choices[0]
  if (!object(first) || !object(first.message)) return undefined
  const message = first.message
  const calls = Array.isArray(message.tool_calls) ? message.tool_calls.filter((call) => object(call) && object(call.function) && call.function.name === PROBE_TOOL).length : 0
  const field = typeof message.reasoning_content === 'string' ? 'reasoning_content' : typeof message.reasoning === 'string' ? 'reasoning' : undefined
  return { nativeToolCalls: calls > 0, parallelToolCalls: calls > 1, ...(field === undefined ? {} : { reasoningField: field }) }
}

/** The window and output cap a listing states for one model, where it states them. */
export function readListing(listing: unknown, model: string): Pick<CapabilityFacts, 'contextWindow' | 'outputReserve'> {
  const rows = object(listing) && Array.isArray(listing.data) ? listing.data : Array.isArray(listing) ? listing : []
  const row = rows.find((item) => object(item) && item.id === model)
  if (!object(row)) return {}
  const named = (names: readonly string[], low: number, high: number) => {
    for (const name of names) if (whole(row[name], low, high)) return row[name] as number
    return undefined
  }
  const contextWindow = named(WINDOW_NAMES, 4096, 2_000_000)
  const output = named(OUTPUT_NAMES, 1024, 1_000_000)
  return {
    ...(contextWindow === undefined ? {} : { contextWindow }),
    // The reserve is at most half the window, the harness's own bound, so a larger cap is held to it.
    ...(contextWindow === undefined || output === undefined ? {} : { outputReserve: Math.min(output, Math.floor(contextWindow / 2)) })
  }
}

/**
 * Probe the endpoint behind a model call. `call` is the connection's own model
 * call (`bindModelCall`), which carries its saved address and key.
 */
export async function probeCapabilities(call: ModelCall, model: string, signal?: AbortSignal, now: () => Date = () => new Date()): Promise<ProbeOutcome> {
  let facts: CapabilityFacts = {}
  const unknown: string[] = ['vision', 'reasoning', 'tool-schemas', 'turn order']
  let answered: Response
  try {
    answered = await call('chat/completions', { method: 'POST', body: probeBody(model), headers: { 'Content-Type': 'application/json' }, ...(signal ? { signal } : {}) })
  } catch {
    return { ok: false, problem: 'unreachable' }
  }
  if (answered.status === 400 || answered.status === 422) {
    // The endpoint refused a request that offered a tool: it does not take tool calls natively.
    facts = { nativeToolCalls: false, parallelToolCalls: false }
    void answered.body?.cancel().catch(() => undefined)
  } else if (!answered.ok) {
    void answered.body?.cancel().catch(() => undefined)
    return { ok: false, problem: 'refused', status: answered.status }
  } else {
    let read: ReturnType<typeof readChatAnswer>
    try {
      read = readChatAnswer(await answered.json())
    } catch {
      read = undefined
    }
    if (read === undefined) return { ok: false, problem: 'unreadable', status: answered.status }
    facts = { ...read }
  }
  // The listing is a second small read, and its absence costs the probe only the window.
  try {
    const listed = await call(LISTING_SUFFIX['openai-compatible'], { method: 'GET', body: '', ...(signal ? { signal } : {}) })
    if (listed.ok) facts = { ...facts, ...readListing(await listed.json(), model) }
    else void listed.body?.cancel().catch(() => undefined)
  } catch {
    /* the window stays unknown */
  }
  if (facts.contextWindow === undefined) unknown.unshift('context window')
  return { ok: true, findings: { at: now().toISOString(), facts, unknown } }
}
