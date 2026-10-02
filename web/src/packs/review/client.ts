/**
 * Review and lock (ADR-0009, section 2), as the page calls it.
 *
 * `GET /api/review` answers the runtime's `packs verify` findings, passed
 * through, and the set of files a lock would cover, each by digest.
 * `POST /api/review/lock` sends that set back: the desk locks exactly it, or
 * nothing. Nothing here judges a file; the runtime's findings are the answer.
 */
import { deskFetch } from '../../files/client'
import { msg } from '../../i18n'

export type ReviewSide = { state: 'text' | 'no-copy' | 'unlocked' | 'absent' | 'too-large'; text?: string }
export type ReviewFinding = {
  name: string
  kind?: string
  id?: string
  path?: string
  detail?: string
  earlier: ReviewSide
  now: ReviewSide
}
export type ReviewEntry = { kind: string; id: string; path: string; digest: string }
export type ReviewSet = { config: string; entries: ReviewEntry[] }
export type Review = {
  status: string
  locked: boolean
  findings: ReviewFinding[]
  diagnostics: { code: string; message: string }[]
  set: ReviewSet | null
  unreadable?: string
}
export type Locked = { files: number; copies: 'stored' | 'not-stored' }

export const REVIEW_KEY = ['desk-review'] as const

/** The project changed after the review it confirms: nothing was locked. */
export class StaleReview extends Error {}

const text = (value: unknown): value is string => typeof value === 'string'
const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
const side = (value: unknown): boolean => object(value) && ['text', 'no-copy', 'unlocked', 'absent', 'too-large'].includes(value.state as string) && (value.text === undefined || text(value.text))

function isReview(value: unknown): value is Review {
  if (!object(value) || !text(value.status) || typeof value.locked !== 'boolean' || !Array.isArray(value.findings) || !Array.isArray(value.diagnostics)) return false
  if (!value.findings.every(item => object(item) && text(item.name) && side(item.earlier) && side(item.now))) return false
  if (value.set !== null && !(object(value.set) && text(value.set.config) && Array.isArray(value.set.entries) && value.set.entries.every(entry => object(entry) && text(entry.kind) && text(entry.id) && text(entry.path) && text(entry.digest)))) return false
  return true
}

async function refusal(response: Response): Promise<Error> {
  let body: { error?: unknown; code?: unknown } = {}
  try { body = await response.json() as typeof body } catch { /* The status is still an answer. */ }
  const message = text(body.error) ? body.error : msg('The review could not be loaded. Please try again.')
  return body.code === 'stale' ? new StaleReview(message) : new Error(message)
}

export async function readReview(signal?: AbortSignal): Promise<Review> {
  const response = await deskFetch('/api/review', { signal })
  if (!response.ok) throw await refusal(response)
  const value: unknown = await response.json()
  if (!isReview(value)) throw new Error(msg('The review could not be loaded. Please try again.'))
  return value
}

/** Lock exactly the set the review showed. */
export async function confirmLock(set: ReviewSet): Promise<Locked> {
  const response = await deskFetch('/api/review/lock', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ set }) })
  if (!response.ok) throw await refusal(response)
  const value: unknown = await response.json()
  if (!object(value) || typeof value.files !== 'number' || (value.copies !== 'stored' && value.copies !== 'not-stored')) throw new Error(msg('The review could not be loaded. Please try again.'))
  return value as Locked
}
