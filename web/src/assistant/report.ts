import { validCriticRecord, type CriticRecord } from '@judgment-pack/harness/criticSwitch'
import { MAX_STEPS, MAX_TOOLS_PER_STEP, timingTotals, type RunTiming } from './runMeter'
import { MAX_PROMPT_DIGESTS, validDelegationFigures, type DelegationFigures, type ExecutionCheckpoint, type TokenFigures, type PromptDigest } from '@judgment-pack/harness/execution/state'

/**
 * What a finished (or paused) response reports about itself: how many model
 * requests it made, the provider's own token counts where it returned them,
 * how many requests were retried and why, and, where the response delegated,
 * each child's figures beside the parent's. It is built from the execution
 * checkpoint the harness saves, so it **survives a reload** with the response
 * history, and it states only what the checkpoint holds: nothing here is an
 * estimate (the byte count divided by three the old display used), and a count
 * the provider did not return is left out rather than said as zero.
 */
export interface RunReport {
  critic?: CriticRecord
  /** The model requests the response made (or, on an agent capability, the agent runs). */
  requests: number
  unit: 'model-requests' | 'provider-managed'
  /** Requests the provider refused and the harness sent again, by the failure code of each refusal. */
  retries?: { count: number; reasons: string[] }
  /** Retained request ordinals and prompt digests, exactly as the checkpoint holds them. */
  prompts?: PromptDigest[]
  /** The provider's token counts for the parent's own requests, summed; a member the provider did not return is absent. */
  tokens?: TokenFigures
  /** Each delegated child's figures, each batch's total and the parent's own, as the checkpoint carries them. */
  delegation?: DelegationFigures
  /**
   * What the Desk measured on the connection (`RunMeter`): per request, its model, bytes, time to first byte,
   * time to its end and status, and the tools the model called after it with each one's time. The requests of
   * delegated children and of the critic go through the same connection and are among them.
   */
  timing?: RunTiming
}

const TOKEN_MEMBERS = ['inputTokens', 'outputTokens', 'reasoningTokens', 'cacheReadTokens', 'cacheWriteTokens'] as const

/** The sum of the token counts the provider returned: a member no request returned stays absent. */
export function sumTokens(rows: readonly Partial<Record<(typeof TOKEN_MEMBERS)[number], number>>[]): TokenFigures | undefined {
  const sum: TokenFigures = {}
  for (const member of TOKEN_MEMBERS) {
    const held = rows.map((row) => row[member]).filter((value): value is number => typeof value === 'number')
    if (held.length > 0) sum[member] = held.reduce((total, value) => total + value, 0)
  }
  return Object.keys(sum).length === 0 ? undefined : sum
}

/**
 * The report a response's figures give: the harness's execution checkpoint, and what the Desk measured on the
 * connection. Nothing where neither holds a request.
 */
export function reportFromExecution(execution: ExecutionCheckpoint | undefined, timing?: RunTiming): RunReport | undefined {
  const measured = timing !== undefined && timingTotals(timing).requests > 0
  if ((execution === undefined || (execution.used === 0 && execution.delegation === undefined && execution.critic === undefined)) && !measured) return undefined
  const rows = execution?.completions ?? []
  // The parent's own tokens: the checkpoint's rows are the parent's requests (a child's are in its figures).
  const tokens = sumTokens(rows.filter((row) => row.phase !== 'review').map((row) => row.result))
  const refused = rows.filter((row) => row.failure !== undefined)
  const reasons = [...new Set(refused.map((row) => row.failure!.code))]
  return {
    requests: execution?.used ?? timingTotals(timing!).requests,
    unit: execution?.unit ?? 'model-requests',
    ...(refused.length === 0 ? {} : { retries: { count: refused.length, reasons } }),
    ...(execution?.prompts === undefined ? {} : { prompts: structuredClone(execution.prompts) }),
    ...(tokens === undefined ? {} : { tokens }),
    ...(execution?.critic === undefined ? {} : { critic: structuredClone(execution.critic) }),
    ...(execution?.delegation === undefined ? {} : { delegation: structuredClone(execution.delegation) }),
    ...(measured ? { timing: structuredClone(timing!) } : {})
  }
}

const object = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value)
const count = (value: unknown, max = Number.MAX_SAFE_INTEGER): value is number => Number.isSafeInteger(value) && Number(value) >= 0 && Number(value) <= max
const validTokens = (value: unknown): value is TokenFigures => object(value) && Object.keys(value).every((key) => (TOKEN_MEMBERS as readonly string[]).includes(key)) && TOKEN_MEMBERS.every((key) => value[key] === undefined || count(value[key]))

const DAY = 86_400_000

/** A saved timing, read closed. */
export function validTiming(value: unknown): value is RunTiming {
  const step = (item: unknown) =>
    object(item) && Object.keys(item).every((key) => ['model', 'requestBytes', 'firstByteMs', 'durationMs', 'status', 'outcome', 'tools', 'queuedMs', 'queueDepth'].includes(key)) &&
    typeof item.model === 'string' && item.model.length > 0 && item.model.length <= 200 && count(item.requestBytes, 1 << 30) &&
    (item.firstByteMs === undefined || count(item.firstByteMs, DAY)) && (item.durationMs === undefined || count(item.durationMs, DAY)) &&
    (item.status === undefined || (Number.isInteger(item.status) && Number(item.status) >= 100 && Number(item.status) <= 599)) &&
    ['complete', 'failed', 'aborted'].includes(String(item.outcome)) && (item.queuedMs === undefined || count(item.queuedMs, DAY)) && (item.queueDepth === undefined || count(item.queueDepth, 1000)) &&
    (item.tools === undefined || (Array.isArray(item.tools) && item.tools.length <= MAX_TOOLS_PER_STEP && item.tools.every((tool) =>
      object(tool) && Object.keys(tool).every((key) => ['name', 'ms', 'failed'].includes(key)) && typeof tool.name === 'string' && tool.name.length > 0 && tool.name.length <= 150 && count(tool.ms, DAY) && (tool.failed === undefined || tool.failed === true))))
  return object(value) && Object.keys(value).every((key) => key === 'steps' || key === 'omitted') && Array.isArray(value.steps) && value.steps.length <= MAX_STEPS && value.steps.every(step) && count(value.omitted, 1_000_000)
}

/** A saved report, read closed: only the members above, each as the checkpoint or the meter states it. */
export function validRunReport(value: unknown): value is RunReport {
  if (!object(value) || !Object.keys(value).every((key) => ['requests', 'unit', 'retries', 'tokens', 'delegation', 'timing', 'critic', 'prompts'].includes(key))) return false
  if (!count(value.requests, 1_000_000) || (value.unit !== 'model-requests' && value.unit !== 'provider-managed')) return false
  if (value.retries !== undefined && !(object(value.retries) && Object.keys(value.retries).every((key) => key === 'count' || key === 'reasons') && count(value.retries.count, 1000) && Array.isArray(value.retries.reasons) && value.retries.reasons.length <= 32 && value.retries.reasons.every((reason) => typeof reason === 'string' && reason.length <= 64))) return false
  if (value.critic !== undefined && !validCriticRecord(value.critic)) return false
  if (value.prompts !== undefined && !validPrompts(value.prompts)) return false
  if (value.tokens !== undefined && !validTokens(value.tokens)) return false
  if (value.timing !== undefined && !validTiming(value.timing)) return false
  return value.delegation === undefined || validDelegationFigures(value.delegation)
}

/** Match the canonical checkpoint's bounded, ordered digest rows, without relating ordinals to counters. */
function validPrompts(value: unknown): value is PromptDigest[] {
  return Array.isArray(value) && value.length <= MAX_PROMPT_DIGESTS && value.every((row, at) =>
    object(row) && Object.keys(row).every((key) => key === 'request' || key === 'sha256') &&
    count(row.request) && row.request > 0 && (at === 0 || row.request > value[at - 1].request) &&
    typeof row.sha256 === 'string' && row.sha256.length === 64 && /^[a-f0-9]{64}$/.test(row.sha256))
}
