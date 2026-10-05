/**
 * The key this desk's Runner signs its runs with, or why it signs none
 * (ADR-0010, section 5; `internal/desk/runner_key.go`).
 *
 * It changes while the page is open: Runner starts, answers, refuses its key,
 * stops. So it is not part of the configuration query, which is read once and
 * kept: it has a query of its own, on `GET /api/runner-key`, asked again on a
 * short interval for as long as something shows it (Help & About), and again
 * when a Jobs action completes (`refreshRunnerKey`).
 */
import { useQuery, type QueryClient } from '@tanstack/react-query'
import { deskFetch } from '../files/client'

/** Why a desk's Runner was started without its signing key, as Desk names it. */
export const RUNNER_KEY_REASONS = ['custody', 'not-made', 'unfinished', 'lost', 'not-read-now', 'in-use', 'not-used', 'runtime-refused', 'runner-refused'] as const
export type RunnerKeyReason = typeof RUNNER_KEY_REASONS[number]

/**
 * What Desk reported of the key: Runner started with it, and Desk gives its
 * public half as the runtime read it; Runner started without it, and why, in
 * the words of the check, the runtime or Runner; Runner is starting; or
 * Runner did not start, and why. Never a path.
 */
export type RunnerKey =
  | { state: 'signed'; publicKey: string; keyId: string }
  | { state: 'unsigned'; reason: RunnerKeyReason; detail?: string }
  | { state: 'starting' }
  | { state: 'not-running'; detail?: string }

/**
 * The Runner key Desk reported, or undefined where it is not one of its four
 * shapes: a public key of 64 and a keyId of 32 lowercase hexadecimal
 * characters; a reason the page has words for, with words that are text;
 * starting; or not running, with words that are text.
 */
export function runnerKeyOf(value: unknown): RunnerKey | undefined {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined
  const key = value as Record<string, unknown>
  const words = key.detail === undefined || typeof key.detail === 'string'
  const detail = typeof key.detail === 'string' && key.detail !== '' ? { detail: key.detail } : {}
  if (key.state === 'signed' && typeof key.publicKey === 'string' && /^[0-9a-f]{64}$/.test(key.publicKey) && typeof key.keyId === 'string' && /^[0-9a-f]{32}$/.test(key.keyId)) {
    return { state: 'signed', publicKey: key.publicKey, keyId: key.keyId }
  }
  if (key.state === 'unsigned' && (RUNNER_KEY_REASONS as readonly unknown[]).includes(key.reason) && words) {
    return { state: 'unsigned', reason: key.reason as RunnerKeyReason, ...detail }
  }
  if (key.state === 'starting') return { state: 'starting' }
  if (key.state === 'not-running' && words) return { state: 'not-running', ...detail }
  return undefined
}

export const RUNNER_KEY_QUERY_KEY = ['runner-key'] as const

/** How often the key is asked for again while something shows it. */
export const RUNNER_KEY_REFRESH_MS = 3000

/**
 * Desk's answer: the key, or null where this desk has no Runner, or where the
 * answer is not one of the shapes the key has. A refusal is an error.
 */
export async function loadRunnerKey(signal?: AbortSignal): Promise<RunnerKey | null> {
  const response = await deskFetch('/api/runner-key', { signal })
  if (!response.ok) throw new Error(`Desk did not say what Runner signs with (${response.status}).`)
  const answer: unknown = JSON.parse(await response.text())
  if (!answer || typeof answer !== 'object') return null
  return runnerKeyOf((answer as { runnerKey?: unknown }).runnerKey) ?? null
}

/** The key, asked again every RUNNER_KEY_REFRESH_MS while it is shown. */
export function useRunnerKey() {
  return useQuery({
    queryKey: RUNNER_KEY_QUERY_KEY,
    queryFn: ({ signal }) => loadRunnerKey(signal),
    staleTime: 0,
    refetchInterval: RUNNER_KEY_REFRESH_MS,
    refetchIntervalInBackground: false
  })
}

/** Asks for the key again, as a Jobs action that may have started Runner completes. */
export function refreshRunnerKey(client: QueryClient): Promise<void> {
  return client.invalidateQueries({ queryKey: RUNNER_KEY_QUERY_KEY })
}
