/**
 * The decision-record panel (ADR-0010, sections 1, 4 and 6), as the page calls
 * it.
 *
 * `GET /api/audit/verify` answers what the runtime's own `jpack audit verify`
 * finds in this desk's trail, run with the public keys Desk keeps for the desk
 * (none on the startup desk), no held checkpoint and no stamping roots: the
 * report, with the runtime's member names; the runtime's refusal to make one;
 * that the runtime has no audit commands; or that the project keeps no trail.
 * Beside a report or a refusal: the keys Desk keeps and passed, `packs
 * validate`'s word on whether the key named signs, and whether the owner can
 * rotate the desk's key now, with the token that confirms it. It runs only
 * when asked. Nothing here judges the trail: the runtime's report is the
 * answer.
 *
 * `POST /api/audit/key/rotate` sends that token back: the desk rotates its key
 * as the panel showed it, or changes nothing (ADR-0010, section 1, "Rotating
 * it").
 */
import { deskFetch } from '../files/client'
import { msg } from '../i18n'
import { sourceMessage } from '../i18n/source'

/** One protection's reach, as runtime 0.26.0 reports it. */
export type AuditCoverageState = { status: string; through?: number; detail?: string }
export type AuditCoverage = {
  legacyPrefix: number
  chained: number
  unchained: number
  uncovered: number
  damaged: number
  signed: AuditCoverageState
  signedRecords: number
  unsignedRecords: number
  checkpointed: AuditCoverageState
  witnessed: number
  unwitnessed: number
  stamped: AuditCoverageState
}
export type AuditSegment = { firstLine: number; lastLine: number }
export type AuditDiscontinuity = { line: number; reason: string; damagedLine: number; bytes: number; digest: string }
export type AuditFinding = { name: string; line: number; detail: string }
/** The signature sidecar as the runtime read it, given where a key was passed. */
export type AuditSignatures = { lines: number; unreadable: number; rotations: number; keysSupplied: number; revocations: number; firstKey: string; keyInForce: string }
export type AuditReport = {
  /** The runtime's: `valid`, `segmented` or `invalid`. */
  status: string
  lines: number
  bytes: number
  snapshotBetweenWrites: boolean
  coverage: AuditCoverage
  segments: AuditSegment[]
  segmentsTotal: number
  discontinuities: AuditDiscontinuity[]
  discontinuitiesTotal: number
  findings: AuditFinding[]
  findingsTotal: number
  signatures?: AuditSignatures
  /** The runtime's own sentences, in English, as it wrote them. */
  establishes: string[]
  doesNotEstablish: string[]
}
export type AuditDiagnostic = { code: string; message: string }
/** One public key Desk keeps for this desk: the key, the runtime's keyId for it, and the record it signs after. */
export type DeskPublicKey = { publicKey: string; keyId: string; at: number }
/**
 * The keys Desk keeps for this desk: `kept`, each passed to the check in this
 * order; `none`, a desk Desk made with no key; `startup`, the project Desk was
 * started on, which keeps none in this version; `unread`, Desk could not read
 * or pass them, and passed none.
 */
export type AuditKeys =
  | { state: 'kept'; public: DeskPublicKey[] }
  | { state: 'none' | 'startup' }
  | { state: 'unread'; problem: string }
/**
 * `packs validate`'s word on the key named for this project: its
 * `audit-signing-key` check, with the runtime's status and sentence; none,
 * where no key is named; or why it did not say.
 */
export type AuditSigning =
  | { state: 'check'; status: 'passed' | 'failed' | 'skipped'; detail?: string }
  | { state: 'no-key' }
  | { state: 'unread'; diagnostics?: AuditDiagnostic[]; problem?: string }
/**
 * Whether the owner can rotate this desk's signing key now: `available`, with
 * the token that confirms it; `unavailable`, and why; or `unfinished`, a
 * rotation that did not finish, and what Desk does with it or why it cannot
 * tell. The reason is Desk's own sentence.
 */
export type AuditRotation =
  | { state: 'available'; token: string }
  | { state: 'unavailable' | 'unfinished'; reason: string }
/** A rotation made: the key that signed until `at`, and the key that signs the records after it. */
export type RotationResult = { state: 'rotated'; at: number; from: DeskPublicKey; next: DeskPublicKey }
/** The keys changed after the panel showed them: nothing was rotated. */
export class StaleRotation extends Error {}

/**
 * The chassis's own sentences about rotation, as it says them, so that the
 * page can show each in the owner's language (`systemMessage`). A reason that
 * carries the runtime's words or a file's state keeps them as they were said.
 */
export const ROTATION_REASONS = [
  sourceMessage('This is the project Desk was started on. Desk keeps no signing key for it in this version, so it has none to rotate.'),
  sourceMessage('Desk keeps no signing key for this desk, so it has none to rotate.'),
  sourceMessage('Desk rotates only a key it can read, with a list of public keys that agrees with it.'),
  sourceMessage('This desk\'s key took over after record {{record}}, and no record has been signed since: a rotation now would take over after the same record. Make a deciding run first.'),
  sourceMessage('This desk already has {{count}} signing keys, the most Desk keeps for one desk, so it rotates no further key.'),
  sourceMessage('Desk reads the trail\'s signature sidecar to tell whether a rotation was written, and it could not be read: {{reason}}. So Desk does not rotate the key now.'),
  sourceMessage('Nothing of it is left to do but remove its marker, which Desk does when it next starts.'),
  sourceMessage('The runtime wrote the rotation, and Desk did not finish it: Desk finishes it when it next starts. Until then, records are written unsigned.'),
  sourceMessage('The runtime did not write the rotation: Desk removes the next key when it next starts. The current key still signs.'),
  sourceMessage('Its marker was removed while Desk looked.'),
  sourceMessage('Desk cannot tell whether the runtime wrote the rotation, so it changes nothing: {{reason}}.')
]

/** The runtime's files a download can hand over, by the name the download takes. */
export const TRAIL_FILES = { evaluations: 'evaluations.jsonl', signatures: 'signatures.jsonl', stamps: 'stamps.jsonl' } as const
export type TrailFile = keyof typeof TRAIL_FILES
export type AuditRecord =
  | { state: 'report'; runtime?: string; report: AuditReport; files?: TrailFile[]; keys?: AuditKeys; signing?: AuditSigning; rotation?: AuditRotation }
  | { state: 'unverified'; runtime?: string; diagnostics: AuditDiagnostic[]; files?: TrailFile[]; keys?: AuditKeys; signing?: AuditSigning; rotation?: AuditRotation }
  | { state: 'older-runtime'; runtime?: string; floor: string }
  | { state: 'no-trail' }

export const AUDIT_KEY = ['desk-audit-record'] as const

/** Desk does not check this desk's record here, and says why. */
export class AuditUnavailable extends Error {}

const text = (value: unknown): value is string => typeof value === 'string'
const count = (value: unknown): value is number => typeof value === 'number' && Number.isInteger(value) && value >= 0
const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
function list<T>(value: unknown, item: (value: unknown) => value is T): value is T[] { return Array.isArray(value) && value.every(item) }
const optional = (value: unknown, check: (value: unknown) => boolean) => value === undefined || check(value)

const named = (value: unknown): value is string => text(value) && value !== ''
/** A protection's reach: a status, and the record it reaches through, which "through" must name. */
const isState = (value: unknown): value is AuditCoverageState => object(value) && named(value.status) && optional(value.through, count) && optional(value.detail, text)
  && (value.status !== 'through' || count(value.through) && value.through > 0)
const isCoverage = (value: unknown): value is AuditCoverage => object(value)
  && ['legacyPrefix', 'chained', 'unchained', 'uncovered', 'damaged', 'signedRecords', 'unsignedRecords', 'witnessed', 'unwitnessed'].every(name => count(value[name]))
  && ['signed', 'checkpointed', 'stamped'].every(name => isState(value[name]))
const isSegment = (value: unknown): value is AuditSegment => object(value) && count(value.firstLine) && count(value.lastLine)
const isDiscontinuity = (value: unknown): value is AuditDiscontinuity => object(value) && count(value.line) && named(value.reason) && count(value.damagedLine) && count(value.bytes) && named(value.digest)
const isFinding = (value: unknown): value is AuditFinding => object(value) && named(value.name) && count(value.line) && text(value.detail)
const isDiagnostic = (value: unknown): value is AuditDiagnostic => object(value) && named(value.code) && named(value.message)
const isTrailFile = (value: unknown): value is TrailFile => text(value) && Object.hasOwn(TRAIL_FILES, value)
const hex = (length: number) => (value: unknown): value is string => text(value) && new RegExp(`^[0-9a-f]{${length}}$`).test(value)
const isSignatures = (value: unknown): value is AuditSignatures => object(value)
  && ['lines', 'unreadable', 'rotations', 'keysSupplied', 'revocations'].every(name => count(value[name])) && named(value.firstKey) && named(value.keyInForce)
const isPublicKey = (value: unknown): value is DeskPublicKey => object(value) && hex(64)(value.publicKey) && hex(32)(value.keyId) && count(value.at)

/** The keys Desk keeps, as the chassis lists them: a kept list is at least one key, in order of the record each signs after. */
export function isAuditKeys(value: unknown): value is AuditKeys {
  if (!object(value)) return false
  switch (value.state) {
    case 'kept': return list(value.public, isPublicKey) && value.public.length > 0 && value.public.every((key, index, keys) => index === 0 ? key.at === 0 : key.at > keys[index - 1]!.at)
    case 'none': case 'startup': return true
    case 'unread': return named(value.problem)
  }
  return false
}

export function isAuditSigning(value: unknown): value is AuditSigning {
  if (!object(value)) return false
  switch (value.state) {
    case 'check': return ['passed', 'failed', 'skipped'].includes(value.status as string) && optional(value.detail, text)
    case 'no-key': return true
    case 'unread': return optional(value.diagnostics, item => list(item, isDiagnostic)) && optional(value.problem, text)
      && (value.diagnostics !== undefined && (value.diagnostics as unknown[]).length > 0 || named(value.problem))
  }
  return false
}

/** The panel's word on rotation: a token of 64 hexadecimal characters with `available`, a sentence with the others, and nothing else. */
export function isAuditRotation(value: unknown): value is AuditRotation {
  if (!object(value)) return false
  switch (value.state) {
    case 'available': return hex(64)(value.token) && value.reason === undefined
    case 'unavailable': case 'unfinished': return named(value.reason) && value.token === undefined
  }
  return false
}

/**
 * A report with every member the runtime gives one, as the chassis checks it:
 * each count present and not negative, no list longer than its total, and
 * findings exactly where the status says a check failed.
 */
export function isAuditReport(value: unknown): value is AuditReport {
  return object(value) && text(value.status) && count(value.lines) && count(value.bytes) && typeof value.snapshotBetweenWrites === 'boolean'
    && isCoverage(value.coverage) && list(value.segments, isSegment) && count(value.segmentsTotal)
    && list(value.discontinuities, isDiscontinuity) && count(value.discontinuitiesTotal)
    && list(value.findings, isFinding) && count(value.findingsTotal)
    && list(value.establishes, text) && list(value.doesNotEstablish, text) && optional(value.signatures, isSignatures)
    && value.segments.length <= value.segmentsTotal && value.discontinuities.length <= value.discontinuitiesTotal
    && value.findings.length <= value.findingsTotal && (value.status === 'invalid') === (value.findingsTotal > 0)
}

export function isAuditRecord(value: unknown): value is AuditRecord {
  if (!object(value) || !optional(value.runtime, text) || !optional(value.files, item => list(item, isTrailFile))
    || !optional(value.keys, isAuditKeys) || !optional(value.signing, isAuditSigning) || !optional(value.rotation, isAuditRotation)) return false
  switch (value.state) {
    case 'report': return isAuditReport(value.report)
    case 'unverified': return list(value.diagnostics, isDiagnostic) && value.diagnostics.length > 0
    case 'older-runtime': return text(value.floor)
    case 'no-trail': return true
  }
  return false
}

async function refusal(response: Response): Promise<Error> {
  let body: { error?: unknown } = {}
  try { body = await response.json() as typeof body } catch { /* The status is still an answer. */ }
  const message = text(body.error) ? body.error : msg('The decision record could not be loaded. Please try again.')
  return response.status === 409 ? new AuditUnavailable(message) : new Error(message)
}

/**
 * One of the runtime's files, as Desk served it: the bytes on disk between two
 * writes, read as a Blob and never as text, so nothing decodes or encodes them.
 */
export async function downloadTrailFile(which: TrailFile): Promise<Blob> {
  const response = await deskFetch(`/api/audit/trail?file=${which}`)
  if (!response.ok) {
    let body: { error?: unknown } = {}
    try { body = await response.json() as typeof body } catch { /* The status is still an answer. */ }
    throw new Error(text(body.error) ? body.error : msg('The file could not be downloaded. Please try again.'))
  }
  return response.blob()
}

/** The runtime's check of this desk's trail, run now. */
export async function readAuditRecord(signal?: AbortSignal): Promise<AuditRecord> {
  const response = await deskFetch('/api/audit/verify', { signal })
  if (!response.ok) throw await refusal(response)
  const value: unknown = await response.json()
  if (!isAuditRecord(value)) throw new Error(msg('The decision record could not be loaded. Please try again.'))
  return value
}

/**
 * Rotate this desk's signing key, as the panel the token names showed it. A
 * refusal says why in Desk's words; `StaleRotation` where the keys changed
 * after the panel showed them.
 */
export async function rotateSigningKey(token: string): Promise<RotationResult> {
  const response = await deskFetch('/api/audit/key/rotate', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token }) })
  if (!response.ok) {
    let body: { error?: unknown; code?: unknown } = {}
    try { body = await response.json() as typeof body } catch { /* The status is still an answer. */ }
    const message = text(body.error) ? body.error : msg('The key could not be rotated. Check the decision record again.')
    throw body.code === 'stale' ? new StaleRotation(message) : new Error(message)
  }
  const value: unknown = await response.json()
  if (!object(value) || value.state !== 'rotated' || !count(value.at) || value.at < 1 || !isPublicKey(value.from) || !isPublicKey(value.next)
    || value.next.at !== value.at || value.next.publicKey === value.from.publicKey) {
    throw new Error(msg('The key could not be rotated. Check the decision record again.'))
  }
  return value as RotationResult
}
