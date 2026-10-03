/**
 * The upgrade offer for an existing project (ADR-0009, section 4), as the
 * page calls it.
 *
 * `GET /api/upgrade` answers one reading of the project: what turning the
 * gates on would write, for the owner's choice about `requireComparableFacts`,
 * with the first lock's review over the configuration as it would be written,
 * and a token that names this desk and exactly those changes. It writes
 * nothing. `POST /api/upgrade` sends the token and the choice back: the desk
 * writes exactly what it showed and locks it, or puts every file back as it
 * was.
 */
import { deskFetch } from '../../files/client'
import { msg } from '../../i18n'
import { isReview, type Review } from '../review/client'

export type Upgrade = {
  /** `offer` writes something; `unchanged` has nothing to write for this choice; `unavailable` says why in `reason`. */
  state: 'offer' | 'unchanged' | 'unavailable'
  reason?: string
  runtime?: string
  reads?: string[]
  /** The project already holds deciding runs to a reviewed set, and records them. */
  gated: boolean
  comparableFacts?: 'on' | 'off' | 'unavailable'
  /** Whether this offer sets `requireComparableFacts`. */
  requireComparableFacts: boolean
  from?: string
  to?: string
  /** The members of jpack.json the upgrade adds or changes. */
  changes: string[]
  configBefore?: string
  configAfter?: string
  gitignore?: 'add' | 'create' | 'ignored' | 'outside'
  audit?: { state: 'create' | 'exists' | 'kept'; dir?: string }
  /** The project already keeps a lock, which the new configuration drifts from. */
  locked: boolean
  /** The first lock's review, over the upgraded configuration. */
  review?: Review
  token?: string
}
export type Upgraded = {
  files: number
  configVersion: string
  requireComparableFacts: boolean
  copies: 'stored' | 'not-stored'
  copiesProblem?: string
}

export const UPGRADE_KEY = ['desk-upgrade'] as const

/** The project changed after the offer it confirms: every file was left, or put back, as it was. */
export class StaleUpgrade extends Error {}

const text = (value: unknown): value is string => typeof value === 'string'
const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
const optional = (value: unknown, check: (value: unknown) => boolean) => value === undefined || check(value)

function isUpgrade(value: unknown): value is Upgrade {
  if (!object(value) || !['offer', 'unchanged', 'unavailable'].includes(value.state as string)) return false
  if (typeof value.gated !== 'boolean' || typeof value.requireComparableFacts !== 'boolean' || typeof value.locked !== 'boolean') return false
  if (!Array.isArray(value.changes) || !value.changes.every(text)) return false
  if (!optional(value.comparableFacts, item => ['on', 'off', 'unavailable'].includes(item as string))) return false
  if (!optional(value.gitignore, item => ['add', 'create', 'ignored', 'outside'].includes(item as string))) return false
  if (!optional(value.audit, item => object(item) && ['create', 'exists', 'kept'].includes(item.state as string) && optional(item.dir, text))) return false
  if (!optional(value.reads, item => Array.isArray(item) && item.every(text))) return false
  if (!optional(value.review, isReview)) return false
  return ['reason', 'runtime', 'from', 'to', 'configBefore', 'configAfter', 'token'].every(name => optional(value[name], text))
}

async function refusal(response: Response): Promise<Error> {
  let body: { error?: unknown; code?: unknown } = {}
  try { body = await response.json() as typeof body } catch { /* The status is still an answer. */ }
  const message = text(body.error) ? body.error : msg('The upgrade could not be loaded. Please try again.')
  return body.code === 'stale' ? new StaleUpgrade(message) : new Error(message)
}

/** The offer, for the owner's choice about requireComparableFacts. */
export async function readUpgrade(requireComparableFacts: boolean, signal?: AbortSignal): Promise<Upgrade> {
  const response = await deskFetch(requireComparableFacts ? '/api/upgrade' : '/api/upgrade?requireComparableFacts=false', { signal })
  if (!response.ok) throw await refusal(response)
  const value: unknown = await response.json()
  if (!isUpgrade(value)) throw new Error(msg('The upgrade could not be loaded. Please try again.'))
  return value
}

/** Write exactly what the offer the token names showed, and lock it. */
export async function confirmUpgrade(token: string, requireComparableFacts: boolean): Promise<Upgraded> {
  const response = await deskFetch('/api/upgrade', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token, requireComparableFacts }) })
  if (!response.ok) throw await refusal(response)
  const value: unknown = await response.json()
  if (!object(value) || typeof value.files !== 'number' || !text(value.configVersion) || typeof value.requireComparableFacts !== 'boolean' || (value.copies !== 'stored' && value.copies !== 'not-stored') || !optional(value.copiesProblem, text)) throw new Error(msg('The upgrade could not be loaded. Please try again.'))
  return value as Upgraded
}
