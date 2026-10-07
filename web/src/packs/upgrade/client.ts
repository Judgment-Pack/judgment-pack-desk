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
 *
 * On the project Desk was started on, the offer carries one more item,
 * "Sign this project's decisions" (ADR-0010, section 1 and question 2): never
 * chosen for the owner, asked for with `signingKey=true`, and sent back as
 * `"signingKey": true` with the token of the offer that named the key.
 */
import { deskFetch } from '../../files/client'
import { msg } from '../../i18n'
import { sourceMessage } from '../../i18n/source'
import type { DeskPublicKey } from '../../audit/client'
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
  /**
   * "Sign this project's decisions", on the project Desk was started on only:
   * `offered`, which the owner may choose; `named`, jpack.json already names a
   * key; `unavailable`, and why, in Desk's words.
   */
  signingKey?: UpgradeSigning
  /** Whether this offer makes the key and names it. */
  sign?: boolean
}
export type UpgradeSigning = { state: 'offered' | 'named' } | { state: 'unavailable'; reason: string }
export type Upgraded = {
  files: number
  configVersion: string
  requireComparableFacts: boolean
  copies: 'stored' | 'not-stored'
  copiesProblem?: string
  /** The key jpack.json now names, where the upgrade made one. */
  signingKey?: DeskPublicKey
}

/**
 * Why the item is not offered, as the chassis says it (`keyNotOffered…` in
 * `internal/desk/startup_key.go`), so that `systemMessage` shows each in the
 * owner's language. A reason Desk's custody gives stays as the chassis wrote it.
 */
export const SIGNING_REASONS = [
  sourceMessage('This project is not offered a signing key: a project names its signing key at configVersion 6, and the runtime this Desk runs (jpack {{version}}) does not read it. A runtime of {{floor}} or later does.'),
  sourceMessage('This project is not offered a signing key, because Desk could not keep one for it: {{reason}}.'),
  sourceMessage('This project is not offered a signing key: JPACK_SIGNING_KEY is set where Desk was started, and the runtime signs this project\'s records with the key it names, not with one jpack.json names.'),
  sourceMessage('This project is not offered a signing key: its audit member turns the chain off, and the runtime signs only a chained trail.'),
  sourceMessage('This project is not offered a signing key: its audit member is not an object Desk can add a key to.')
] as const

export const UPGRADE_KEY = ['desk-upgrade'] as const

/** The project changed after the offer it confirms: every file was left, or put back, as it was. */
export class StaleUpgrade extends Error {}

const text = (value: unknown): value is string => typeof value === 'string'
const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
const optional = (value: unknown, check: (value: unknown) => boolean) => value === undefined || check(value)
const hex = (length: number) => (value: unknown) => text(value) && new RegExp(`^[0-9a-f]{${length}}$`).test(value)

/** The item as the chassis gives it: a reason with `unavailable`, and none with the others. */
function isSigning(value: unknown): value is UpgradeSigning {
  if (!object(value)) return false
  switch (value.state) {
    case 'offered': case 'named': return value.reason === undefined
    case 'unavailable': return text(value.reason) && value.reason !== ''
  }
  return false
}

function isUpgrade(value: unknown): value is Upgrade {
  if (!object(value) || !['offer', 'unchanged', 'unavailable'].includes(value.state as string)) return false
  if (typeof value.gated !== 'boolean' || typeof value.requireComparableFacts !== 'boolean' || typeof value.locked !== 'boolean') return false
  if (!Array.isArray(value.changes) || !value.changes.every(text)) return false
  if (!optional(value.comparableFacts, item => ['on', 'off', 'unavailable'].includes(item as string))) return false
  if (!optional(value.gitignore, item => ['add', 'create', 'ignored', 'outside'].includes(item as string))) return false
  if (!optional(value.audit, item => object(item) && ['create', 'exists', 'kept'].includes(item.state as string) && optional(item.dir, text))) return false
  if (!optional(value.reads, item => Array.isArray(item) && item.every(text))) return false
  if (!optional(value.review, isReview)) return false
  if (!optional(value.signingKey, isSigning) || !optional(value.sign, item => typeof item === 'boolean') || value.sign === true && (value.signingKey as UpgradeSigning | undefined)?.state !== 'offered') return false
  return ['reason', 'runtime', 'from', 'to', 'configBefore', 'configAfter', 'token'].every(name => optional(value[name], text))
}

async function refusal(response: Response): Promise<Error> {
  let body: { error?: unknown; code?: unknown } = {}
  try { body = await response.json() as typeof body } catch { /* The status is still an answer. */ }
  const message = text(body.error) ? body.error : msg('The upgrade could not be loaded. Please try again.')
  return body.code === 'stale' ? new StaleUpgrade(message) : new Error(message)
}

/** The offer, for the owner's choices about requireComparableFacts and the signing key. */
export async function readUpgrade(requireComparableFacts: boolean, signingKey: boolean, signal?: AbortSignal): Promise<Upgrade> {
  const asked = [...(requireComparableFacts ? [] : ['requireComparableFacts=false']), ...(signingKey ? ['signingKey=true'] : [])]
  const response = await deskFetch(asked.length ? `/api/upgrade?${asked.join('&')}` : '/api/upgrade', { signal })
  if (!response.ok) throw await refusal(response)
  const value: unknown = await response.json()
  if (!isUpgrade(value)) throw new Error(msg('The upgrade could not be loaded. Please try again.'))
  return value
}

/** Write exactly what the offer the token names showed, and lock it; with the signing key, make it first. */
export async function confirmUpgrade(token: string, requireComparableFacts: boolean, signingKey = false): Promise<Upgraded> {
  const response = await deskFetch('/api/upgrade', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token, requireComparableFacts, ...(signingKey ? { signingKey } : {}) }) })
  if (!response.ok) throw await refusal(response)
  const value: unknown = await response.json()
  if (!object(value) || typeof value.files !== 'number' || !text(value.configVersion) || typeof value.requireComparableFacts !== 'boolean' || (value.copies !== 'stored' && value.copies !== 'not-stored') || !optional(value.copiesProblem, text)
    || !optional(value.signingKey, key => object(key) && hex(64)(key.publicKey) && hex(32)(key.keyId) && key.at === 0)) throw new Error(msg('The upgrade could not be loaded. Please try again.'))
  return value as Upgraded
}
