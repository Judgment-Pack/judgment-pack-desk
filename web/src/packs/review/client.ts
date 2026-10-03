/**
 * Review and lock (ADR-0009, section 2), as the page calls it.
 *
 * `GET /api/review` answers one reading of the project: every file a lock
 * would cover, and what only the lock names, with the runtime's `packs
 * verify` findings over exactly those bytes, passed through. Where every file
 * can be shown it gives a token that names this desk and that reading.
 * `POST /api/review/lock` sends the token back: the desk locks exactly that
 * reading, or nothing. Nothing here judges a file; the runtime's findings are
 * the answer.
 */
import { deskFetch } from '../../files/client'
import { msg } from '../../i18n'

/** One side of a file's comparison. Its text is in the review's `contents`, by `digest`. */
export type ReviewSide = { state: 'text' | 'no-copy' | 'absent' | 'not-shown'; digest?: string }
export type ReviewFinding = { name: string; kind?: string; id?: string; path?: string; detail?: string }
export type ReviewFile = {
  kind: 'config' | 'pack' | 'graph' | string
  id?: string
  path: string
  digest?: string
  /** How the current lock stands to this file. */
  lock: 'same' | 'other' | 'none' | 'removed'
  now: ReviewSide
  /** The bytes the lock names, where it names other bytes or only the lock names this file. */
  earlier?: ReviewSide
}
export type Review = {
  status: string
  locked: boolean
  findings: ReviewFinding[]
  diagnostics: { code: string; message: string }[]
  files: ReviewFile[]
  /** Each text the review shows, once, by its digest. */
  contents: Record<string, string>
  token?: string
  blocked?: string
}
export type Locked = { files: number; copies: 'stored' | 'not-stored'; copiesProblem?: string }

export const REVIEW_KEY = ['desk-review'] as const

/** The project changed after the review it confirms: nothing was locked. */
export class StaleReview extends Error {}

const text = (value: unknown): value is string => typeof value === 'string'
const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
const side = (value: unknown): boolean => value === undefined || object(value) && ['text', 'no-copy', 'absent', 'not-shown'].includes(value.state as string) && (value.digest === undefined || text(value.digest))

function isReview(value: unknown): value is Review {
  if (!object(value) || !text(value.status) || typeof value.locked !== 'boolean' || !Array.isArray(value.findings) || !Array.isArray(value.diagnostics) || !Array.isArray(value.files) || !object(value.contents) || !Object.values(value.contents).every(text)) return false
  if (!value.findings.every(item => object(item) && text(item.name))) return false
  if (!value.files.every(file => object(file) && text(file.kind) && text(file.path) && ['same', 'other', 'none', 'removed'].includes(file.lock as string) && object(file.now) && side(file.now) && side(file.earlier))) return false
  return (value.token === undefined || text(value.token)) && (value.blocked === undefined || text(value.blocked))
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

/** Lock exactly the reading the review's token names. */
export async function confirmLock(token: string): Promise<Locked> {
  const response = await deskFetch('/api/review/lock', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token }) })
  if (!response.ok) throw await refusal(response)
  const value: unknown = await response.json()
  if (!object(value) || typeof value.files !== 'number' || (value.copies !== 'stored' && value.copies !== 'not-stored') || (value.copiesProblem !== undefined && !text(value.copiesProblem))) throw new Error(msg('The review could not be loaded. Please try again.'))
  return value as Locked
}

/** The text a side of the review shows, if it shows one. */
export function textOf(review: Review, side: ReviewSide | undefined): string | undefined {
  return side?.state === 'text' && side.digest !== undefined ? review.contents[side.digest] : undefined
}
