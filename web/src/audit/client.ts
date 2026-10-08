/**
 * The decision-record panel (ADR-0010, sections 1, 4 and 6), as the page calls
 * it.
 *
 * `GET /api/audit/verify` answers what the runtime's own `jpack audit verify`
 * finds in this desk's trail, run with the public keys Desk keeps for the desk
 * (on the startup desk, those its upgrade made, if any), no held checkpoint
 * and no stamping roots: the
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
 *
 * Where the report names the finding `incomplete-last-line`, and only there,
 * the answer says whether Desk offers `jpack audit repair`: on a chained
 * trail it could read, with the line it names and a token; otherwise not, and
 * why. `POST /api/audit/repair` sends the token back, and the desk runs the
 * repair once, where the trail is still as Desk read it, or runs nothing
 * (ADR-0010, section 4, "Repair").
 *
 * The hand-over (ADR-0010, section 2): `GET /api/audit/holders` lists the
 * holders the owner added and what Desk recorded as handed over to each,
 * against the trail as the runtime gives it now; `POST /api/audit/holders`
 * adds one; `GET /api/audit/checkpoints?holder=<id>` answers the checkpoints
 * after that holder's cursor, as the exact bytes the runtime printed, with
 * headers that name them; and `POST /api/audit/holders/<id>/confirm` is the
 * owner's word that the file went to the holder. Desk's record of it proves
 * nothing to anyone: only the holder's own copy counts. With it, the check
 * above is also given the checkpoints Desk handed over, and says how many.
 * Each holder may hold Runner's chain of runs too (ADR-0010, section 5), with
 * a cursor of its own: the same routes, with `chain=jobs` on a download and
 * `"chain":"jobs"` in a confirmation, read a fresh copy of the chain.
 *
 * Stamping (ADR-0010, section 3; question 5: no authority by default): the
 * decision record says what Desk keeps of this desk's time-stamping
 * authority, whether its roots were given to the check, how many records are
 * pending a stamp, and the last stamp run since Desk started.
 * `POST /api/audit/stamping/check` holds a proposal to Desk's rules and
 * answers what Desk would keep, with a token; `POST /api/audit/stamping`
 * keeps it on that token, once; `POST /api/audit/stamping/remove` removes the
 * settings the decision record showed; and `POST /api/audit/stamping/stamp`
 * runs one stamp on the owner's request.
 *
 * The Jobs record (ADR-0010, section 4, "A Jobs record panel"): `GET
 * /api/audit/jobs-verify` answers the runtime's `audit verify --trail` over a
 * fresh copy of Runner's chain of runs, with the checkpoints of it handed
 * over and nothing else held, with Runner's key as Desk reports it; or that
 * this desk has no Runner, or Runner is not running.
 */
import type { QueryClient } from '@tanstack/react-query'
import { deskFetch } from '../files/client'
import { msg } from '../i18n'
import { sourceMessage } from '../i18n/source'
import { runnerKeyOf, type RunnerKey } from '../jobs/runnerKey'
import { ON_REQUEST_ONLY } from '../mcp/projectChange'

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
/**
 * The runtime's lag between each covered record's `at` and the time the first
 * stamp covering it attests, as it reports it: over `records` records, the
 * longest and its record, the shortest and its record.
 */
export type AuditStampLag = { records: number; maxSeconds: number; maxSequence?: number; minSeconds: number; minSequence?: number; atAfterStamp: boolean; atUnreadable: number }
/**
 * The stamps file as the runtime read it, given where time-stamping roots were
 * passed: its lines, those it could not read, those that hold under the roots
 * (`trusted`, the runtime's word), how many of those had revocation checked,
 * the time the stamped records existed by, and the lag.
 */
export type AuditStamps = { lines: number; unreadable: number; trusted: number; revocationChecked: number; revocationNotChecked: number; coveredBy?: string; lag: AuditStampLag }
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
  stamps?: AuditStamps
  /** The trail's identity as the runtime read it; none where no line is chained. */
  trail?: string
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
 * started on, for which Desk keeps none until its upgrade makes one; `unread`,
 * Desk could not read or pass them, and passed none.
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
  sourceMessage('Desk keeps no signing key for this desk, so it has none to rotate.'),
  sourceMessage('Desk rotates only a key it can read, with a list of public keys that agrees with it.'),
  sourceMessage('This desk\'s key took over after record {{record}}, and no record has been signed since: a rotation now would take over after the same record. Make a deciding run first.'),
  sourceMessage('This desk already has {{count}} signing keys, the most Desk keeps for one desk, so it rotates no further key.'),
  sourceMessage('Desk reads the trail\'s signature sidecar to tell whether a rotation was written, and it could not be read: {{reason}}. So Desk does not rotate the key now.'),
  sourceMessage('Nothing of it is left to do but remove its marker, which Desk does when it next starts.'),
  sourceMessage('The runtime wrote the rotation, and Desk did not finish it: Desk finishes it when it next starts. Until then, records are written unsigned.'),
  sourceMessage('The runtime did not write the rotation: Desk removes the next key when it next starts, and keeps the current key.'),
  sourceMessage('Its marker was removed while Desk looked.'),
  sourceMessage('Desk cannot tell whether the runtime wrote the rotation, so it changes nothing: {{reason}}.'),
  sourceMessage('Desk keeps no signing key for the project it was started on, so it has none to rotate.'),
  sourceMessage('JPACK_SIGNING_KEY is set where Desk was started, and the runtime signs this project\'s records with the key it names, not with the key Desk keeps, so Desk rotates no key here.'),
  sourceMessage('Desk rotates the signing key of the project it was started on only where its jpack.json names the key Desk keeps: {{reason}}. A rotation now would hand signing over in the trail while jpack.json named a key that signs nothing more.')
]

/** The runtime's files a download can hand over, by the name the download takes. */
export const TRAIL_FILES = { evaluations: 'evaluations.jsonl', signatures: 'signatures.jsonl', stamps: 'stamps.jsonl' } as const
export type TrailFile = keyof typeof TRAIL_FILES
/**
 * Beside a report or a refusal: how many holders' files of checkpoints Desk
 * passed to the check as `--expect`; the labels of the holders whose file it
 * passed over, because it could not be read now or is not what Desk recorded
 * as handed over; and, where Desk could not read its record of hand-overs at
 * all, or tell which trail is current, why it passed none.
 */
type HeldInputs = { expected?: number; expectUnread?: string[]; handoverProblem?: string }
/**
 * For a holder whose checkpoints were passed to the check: the last record
 * handed over to it, of the trail the report is of, and what follows it, as
 * the report says it. `records` where the report says how many chained
 * records follow; `lines` in its place where it does not, since a line a
 * repair names as damaged is not a record.
 */
export type HandedSince = { holder: string; trail: string; through: number; records?: number; lines?: number }
export type AuditRecord =
  | ({ state: 'report'; runtime?: string; report: AuditReport; files?: TrailFile[]; keys?: AuditKeys; signing?: AuditSigning; rotation?: AuditRotation; identity?: AuditIdentity; repair?: AuditRepair; stamping?: AuditStamping; since?: HandedSince[] } & HeldInputs)
  | ({ state: 'unverified'; runtime?: string; diagnostics: AuditDiagnostic[]; files?: TrailFile[]; keys?: AuditKeys; signing?: AuditSigning; rotation?: AuditRotation; identity?: AuditIdentity; stamping?: AuditStamping } & HeldInputs)
  | { state: 'older-runtime'; runtime?: string; floor: string; identity?: AuditIdentity }
  | { state: 'no-trail'; identity?: AuditIdentity }

export const AUDIT_KEY = ['desk-audit-record'] as const

/** Desk does not check this desk's record here, and says why. */
export class AuditUnavailable extends Error {}

/**
 * The chassis's question on this project's identity, given beside its
 * refusal to check the decision record as with every answer (review round 1
 * of #315), carried by the error the refusal is read as.
 */
export function identityOf(error: unknown): AuditIdentity | undefined {
  return error instanceof Error && 'identity' in error && isAuditIdentity(error.identity) ? error.identity : undefined
}

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
const number = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value)
const isLag = (value: unknown): value is AuditStampLag => object(value) && count(value.records) && number(value.maxSeconds) && number(value.minSeconds)
  && typeof value.atAfterStamp === 'boolean' && count(value.atUnreadable)
  && (value.records === 0 || count(value.maxSequence) && value.maxSequence > 0 && count(value.minSequence) && value.minSequence > 0)
const isStamps = (value: unknown): value is AuditStamps => object(value)
  && ['lines', 'unreadable', 'trusted', 'revocationChecked', 'revocationNotChecked'].every(name => count(value[name])) && optional(value.coveredBy, named) && isLag(value.lag)

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
    && list(value.establishes, text) && list(value.doesNotEstablish, text) && optional(value.signatures, isSignatures) && optional(value.stamps, isStamps)
    && optional(value.trail, hex(32))
    && value.segments.length <= value.segmentsTotal && value.discontinuities.length <= value.discontinuitiesTotal
    && value.findings.length <= value.findingsTotal && (value.status === 'invalid') === (value.findingsTotal > 0)
}

export function isAuditRecord(value: unknown): value is AuditRecord {
  if (!object(value) || !optional(value.runtime, text) || !optional(value.files, item => list(item, isTrailFile))
    || !optional(value.keys, isAuditKeys) || !optional(value.signing, isAuditSigning) || !optional(value.rotation, isAuditRotation) || !optional(value.identity, isAuditIdentity)
    || !optional(value.expected, count) || !optional(value.expectUnread, item => list(item, named)) || !optional(value.handoverProblem, named)) return false
  if (!optional(value.stamping, isAuditStamping)) return false
  switch (value.state) {
    case 'report': return isAuditReport(value.report) && optional(value.repair, isAuditRepair) && optional(value.since, item => list(item, isHandedSince))
    case 'unverified': return list(value.diagnostics, isDiagnostic) && value.diagnostics.length > 0 && value.repair === undefined && value.since === undefined
    case 'older-runtime': return text(value.floor) && value.stamping === undefined
    case 'no-trail': return value.stamping === undefined
  }
  return false
}

/** A holder's count after the last record handed over: of records, or of lines, never both. */
function isHandedSince(value: unknown): value is HandedSince {
  return object(value) && hex(16)(value.holder) && hex(32)(value.trail) && count(value.through) && value.through > 0
    && (count(value.records) && value.lines === undefined || count(value.lines) && value.records === undefined)
}

async function refusal(response: Response): Promise<Error> {
  let body: { error?: unknown; identity?: unknown } = {}
  try { body = await response.json() as typeof body } catch { /* The status is still an answer. */ }
  const message = text(body.error) ? body.error : msg('The decision record could not be loaded. Please try again.')
  const error = response.status === 409 ? new AuditUnavailable(message) : new Error(message)
  return isAuditIdentity(body.identity) ? Object.assign(error, { identity: body.identity }) : error
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

/**
 * Check the trail again, as after the upgrade that makes the project's
 * signing key, which changes the keys the check is given. The panel's query
 * is disabled, so this fetches into it; a failure is the query's to show. As
 * `checkJobsRecordAgain` does: a check already in flight, asked before the
 * key was made, is cancelled, never joined, and the fetch is marked to run
 * only on request.
 */
export async function checkDecisionRecordAgain(client: QueryClient): Promise<void> {
  await client.cancelQueries({ queryKey: AUDIT_KEY })
  await client.fetchQuery({ queryKey: AUDIT_KEY, queryFn: ({ signal }) => readAuditRecord(signal), meta: ON_REQUEST_ONLY, staleTime: 0, retry: false }).catch(() => undefined)
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

/* The project's identity ---------------------------------------------------- */

/**
 * Where this project's identity was written in another folder, which no
 * longer holds it, and Desk cannot tell whether this folder is that one,
 * moved here, or a copy of it (issue #309): the two answers the owner may
 * give, each with the token that confirms it. Desk makes, rotates and
 * recovers no signing key under that identity until the owner answers.
 */
export type AuditIdentity = { state: 'unresolved'; moved: string; copy: string }
/** The owner's answer: this folder was moved here, or it is a copy. */
export type IdentityChoice = 'moved' | 'copy'

export function isAuditIdentity(value: unknown): value is AuditIdentity {
  return object(value) && value.state === 'unresolved' && hex(64)(value.moved) && hex(64)(value.copy) && value.moved !== value.copy
}

/**
 * Answer which this folder is, with the token the decision record gave for
 * that answer. A refusal says why in Desk's words.
 */
export async function resolveIdentity(choice: IdentityChoice, token: string): Promise<IdentityChoice> {
  const response = await deskFetch('/api/project/identity', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ choice, token }) })
  if (!response.ok) {
    let body: { error?: unknown } = {}
    try { body = await response.json() as typeof body } catch { /* The status is still an answer. */ }
    throw new Error(text(body.error) ? body.error : msg('This project’s identity could not be changed. Check the decision record again.'))
  }
  const value: unknown = await response.json()
  if (!object(value) || value.state !== choice) throw new Error(msg('This project’s identity could not be changed. Check the decision record again.'))
  return choice
}

/* The repair ---------------------------------------------------------------- */

/**
 * The decision record's word on `jpack audit repair`, given only where the
 * runtime's report names an incomplete last line: the line it names; with
 * `available`, the token that confirms one repair of the trail as Desk read
 * it; with `unavailable`, why Desk offers none, in its own words.
 */
export type AuditRepair = { state: 'available'; line: number; token: string } | { state: 'unavailable'; line: number; reason: string }
/** A repair made: the discontinuity record the runtime reports it wrote, by its own member names. */
export type RepairResult = { state: 'repaired'; discontinuity: AuditDiscontinuity }
/**
 * Nothing was repaired, or Desk cannot say whether it was: Desk's sentence,
 * and the runtime's own words, where it gave them, each as it said them.
 */
export class RepairRefused extends Error {
  constructor(message: string, readonly diagnostics: AuditDiagnostic[] = []) { super(message); this.name = 'RepairRefused' }
}

/**
 * The chassis's own sentences about a repair, as it says them, so that the
 * page can show each in the owner's language (`systemMessage`). The runtime's
 * words beside them are shown as it said them.
 */
export const REPAIR_REASONS = [
  sourceMessage('The trail changed after the decision record showed it, so nothing was repaired. Check the decision record again.'),
  sourceMessage('Nothing was repaired: this project\'s jpack.json declares no audit directory, so it keeps no trail.'),
  sourceMessage('Nothing was repaired: the runtime did not check the trail again, so Desk could not tell that it is the trail the decision record showed. It said:'),
  sourceMessage('The runtime did not report a repair. It said:'),
  sourceMessage('The runtime\'s audit repair did not answer as documented, so Desk cannot say whether it repaired the trail. Check the decision record again: it shows what the trail holds now.'),
  sourceMessage('The runtime\'s audit repair did not finish as asked: {{reason}}. Desk cannot say whether it repaired the trail. Check the decision record again: it shows what the trail holds now.'),
  sourceMessage('Nothing was repaired: the runtime this Desk runs (jpack {{version}}) does not read configVersion 6 and has no audit repair. A runtime of {{floor}} or later has it.'),
  sourceMessage('Nothing was repaired: {{reason}}.'),
  sourceMessage('A cross-site request cannot repair this desk\'s trail.'),
  sourceMessage('Confirm the repair with the token the decision record gave.'),
  sourceMessage('This project\'s jpack.json says audit.chain false, and the runtime repairs only a chained trail, so Desk offers no repair.'),
  sourceMessage('Nothing was repaired: this project\'s jpack.json says audit.chain false, and the runtime repairs only a chained trail.'),
  sourceMessage('Desk could not read this project\'s jpack.json to tell whether its trail is chained, so it offers no repair.'),
  sourceMessage('Desk binds a repair to the trail\'s bytes as it reads them, and offers none now: {{reason}}.'),
  sourceMessage('Nothing was repaired: Desk could not read the trail again: {{reason}}.'),
  sourceMessage('This confirmation was used already, so nothing was repaired. Check the decision record again: it offers a fresh one where a repair is still needed.'),
  sourceMessage('The runtime repaired a damaged last line other than the one the decision record showed, so this is not the repair you confirmed: check the decision record again.')
]

/** The panel's word on a repair: a line the report names, and a token of 96 hexadecimal characters with `available`, or a sentence with `unavailable`. */
export function isAuditRepair(value: unknown): value is AuditRepair {
  if (!object(value) || !count(value.line) || value.line < 1) return false
  switch (value.state) {
    case 'available': return hex(96)(value.token) && value.reason === undefined
    case 'unavailable': return named(value.reason) && value.token === undefined
  }
  return false
}

/**
 * Repair this desk's trail, as the decision record the token names showed it.
 * A refusal says why in Desk's words, with the runtime's beside them where it
 * gave any.
 */
export async function repairTrail(token: string): Promise<RepairResult> {
  const unread = msg('Desk could not read what the repair answered. The decision record, checked again, shows what the trail holds now.')
  const response = await deskFetch('/api/audit/repair', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token }) })
  if (!response.ok) {
    let body: { error?: unknown; diagnostics?: unknown } = {}
    try { body = await response.json() as typeof body } catch { /* The status is still an answer. */ }
    throw new RepairRefused(text(body.error) ? body.error : unread, list(body.diagnostics, isDiagnostic) ? body.diagnostics : [])
  }
  const value: unknown = await response.json()
  if (!object(value) || value.state !== 'repaired' || !isDiscontinuity(value.discontinuity) || value.discontinuity.line <= value.discontinuity.damagedLine) {
    throw new RepairRefused(unread)
  }
  return value as RepairResult
}

/* The hand-over ------------------------------------------------------------- */

/**
 * What Desk recorded of one trail handed over to one holder; `linesSince`, for
 * the current trail only, the lines after the last record handed over through
 * the last chained one: lines, never records, since a line a repair names as
 * damaged is one of them.
 */
export type HolderTrail = { through: number; confirmedAt: number; digest: string; linesSince?: number }
/**
 * One holder: the owner's label and channel, and Desk's record of each trail
 * handed over to it; under `jobs`, of each identity of Runner's chain of runs,
 * apart from the trail's.
 */
export type Holder = { id: string; label: string; channel: string; addedAt: number; trails: Record<string, HolderTrail>; otherTrail?: boolean
  jobs?: Record<string, HolderTrail>; otherJobsChain?: boolean }
/** The trail as the runtime gives it now: its identity and the sequence of its last chained record. */
export type HandoverTrail = { identity: string; sequence: number }
/** Which chain a hand-over is of: the desk's own trail, or Runner's chain of runs. */
export type HandoverChain = 'trail' | 'jobs'
/**
 * Runner's chain of runs, as the hand-over shows it beside each holder: none
 * where this desk has no Runner, or Runner is not running; no run chained
 * yet; its identity and last sequence; or why Desk could not tell, in its
 * words or the runtime's.
 */
export type JobsChain =
  | { state: 'no-runner' | 'not-running' | 'empty' }
  | { state: 'chain'; chain: HandoverTrail }
  | { state: 'unread'; diagnostics?: AuditDiagnostic[]; problem?: string }
/**
 * The holders, and the trail: none where it has no chained record yet, or the
 * runtime gives no checkpoint of it, and then `diagnostics` say why in the
 * runtime's words. `jobs` is Runner's chain of runs, given where Desk keeps a
 * holder.
 */
export type Holders = { holders: Holder[]; trail: HandoverTrail | null; diagnostics?: AuditDiagnostic[]; jobs?: JobsChain }
/** A download of checkpoints: the bytes as served, the name to save them under, what the headers named, and which chain it is of. */
export type Checkpoints = { blob: Blob; name: string; trail: string; from: number; through: number; digest: string; more: boolean; chain: HandoverChain }

export const HOLDERS_KEY = ['desk-audit-holders'] as const

/** The file is not what the trail gives now: nothing was recorded. */
export class StaleHandover extends Error {}

/**
 * The chassis's own sentences about the hand-over, as it says them, so that
 * the page can show each in the owner's language (`systemMessage`). A reason
 * that carries the runtime's words keeps them as they were said.
 */
export const HANDOVER_REASONS = [
  sourceMessage("A holder's label is 1 to 120 characters and its channel 1 to 200, each with no control characters."),
  sourceMessage('Desk keeps no holder by that id.'),
  sourceMessage('The trail has no chained record yet, so there is nothing to hand over.'),
  sourceMessage('Desk keeps at most {{count}} holders for one desk, so it adds no more.'),
  sourceMessage('The runtime gives no checkpoint of this trail now: {{reason}}'),
  sourceMessage('Desk recorded checkpoints through record {{cursor}} as handed over to this holder, and the trail\'s last chained record is now record {{sequence}}: the trail is shorter than what was handed over, so Desk hands nothing over.'),
  sourceMessage("With this holder, Desk's list of holders would be larger than the {{limit}} bytes Desk reads of it, so it adds no more. Shorter labels and channels take less room."),
  sourceMessage('Desk could not read its record of hand-overs, so it passed none of the checkpoints it handed over to the check: {{reason}}.'),
  sourceMessage('Desk could not tell which trail the checkpoints it handed over belong to, so it passed none of them to the check: {{reason}}')
]

const trailIdentity = hex(32)
const sha256 = (value: unknown): value is string => text(value) && /^sha256:[0-9a-f]{64}$/.test(value)
const holderId = hex(16)
const isHolderTrail = (value: unknown): value is HolderTrail => object(value) && count(value.through) && value.through > 0 && count(value.confirmedAt)
  && sha256(value.digest) && optional(value.linesSince, count)

/** One holder, as the chassis shows it: an id of its form, the owner's words, and a record keyed by trail identity. */
export function isHolder(value: unknown): value is Holder {
  return object(value) && holderId(value.id) && named(value.label) && named(value.channel) && count(value.addedAt)
    && object(value.trails) && Object.entries(value.trails).every(([trail, entry]) => trailIdentity(trail) && isHolderTrail(entry))
    && optional(value.otherTrail, item => typeof item === 'boolean')
    && optional(value.jobs, item => object(item) && Object.entries(item).every(([chain, entry]) => trailIdentity(chain) && isHolderTrail(entry)))
    && optional(value.otherJobsChain, item => typeof item === 'boolean')
}

/** Runner's chain of runs, as the chassis says it: a chain with its identity and a last record, or why not, and nothing else. */
export function isJobsChain(value: unknown): value is JobsChain {
  if (!object(value)) return false
  switch (value.state) {
    case 'no-runner': case 'not-running': case 'empty': return value.chain === undefined && value.diagnostics === undefined && value.problem === undefined
    case 'chain': return object(value.chain) && trailIdentity(value.chain.identity) && count(value.chain.sequence) && value.chain.sequence > 0
    case 'unread': return value.chain === undefined && optional(value.diagnostics, item => list(item, isDiagnostic) && item.length > 0) && optional(value.problem, named)
      && (value.diagnostics !== undefined || value.problem !== undefined)
  }
  return false
}

/** The holders and the trail: a trail with no runtime refusal beside it, or none and perhaps the runtime's words. */
export function isHolders(value: unknown): value is Holders {
  if (!object(value) || !list(value.holders, isHolder) || !optional(value.diagnostics, item => list(item, isDiagnostic) && item.length > 0) || !optional(value.jobs, isJobsChain)) return false
  if (value.trail === null) return true
  return object(value.trail) && trailIdentity(value.trail.identity) && count(value.trail.sequence) && value.trail.sequence > 0 && value.diagnostics === undefined
}

async function handoverRefusal(response: Response, fallback: string): Promise<Error> {
  let body: { error?: unknown; reason?: unknown } = {}
  try { body = await response.json() as typeof body } catch { /* The status is still an answer. */ }
  if (response.status === 409 && body.reason === 'stale') return new StaleHandover(msg('What you downloaded is not what the trail gives now. Download it again and hand over that file.'))
  return new Error(text(body.error) ? body.error : fallback)
}

/** The holders Desk keeps, and what it recorded of each. */
export async function readHolders(signal?: AbortSignal): Promise<Holders> {
  const response = await deskFetch('/api/audit/holders', { signal })
  if (!response.ok) throw await handoverRefusal(response, msg('The holders could not be loaded. Please try again.'))
  const value: unknown = await response.json()
  if (!isHolders(value)) throw new Error(msg('The holders could not be loaded. Please try again.'))
  return value
}

/** Add a holder by the owner's label and channel. */
export async function addHolder(label: string, channel: string): Promise<Holder> {
  const response = await deskFetch('/api/audit/holders', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ label, channel }) })
  if (!response.ok) throw await handoverRefusal(response, msg('The holder could not be added. Please try again.'))
  const value: unknown = await response.json()
  if (!isHolder(value)) throw new Error(msg('The holder could not be added. Please try again.'))
  return value
}

/**
 * The checkpoints after a holder's cursor, as Desk served them: the bytes read
 * as a Blob and never as text, so nothing decodes or encodes them, and the
 * headers that name them. `null` where there is nothing new to hand over.
 * With `jobs`, those of Runner's chain of runs, after the holder's cursor for
 * the chain, saved under a name that says so.
 */
export async function downloadCheckpoints(holder: string, chain: HandoverChain = 'trail'): Promise<Checkpoints | null> {
  const failed = msg('The checkpoints could not be downloaded. Please try again.')
  const response = await deskFetch(`/api/audit/checkpoints?holder=${encodeURIComponent(holder)}${chain === 'jobs' ? '&chain=jobs' : ''}`)
  if (response.status === 204) return null
  if (!response.ok) throw await handoverRefusal(response, failed)
  const header = (name: string) => response.headers.get(name) ?? ''
  const trail = header('Desk-Checkpoints-Trail'), digest = header('Desk-Checkpoints-Digest'), more = header('Desk-Checkpoints-More')
  const from = /^\d+$/.test(header('Desk-Checkpoints-From')) ? Number(header('Desk-Checkpoints-From')) : -1
  const through = /^\d+$/.test(header('Desk-Checkpoints-Through')) ? Number(header('Desk-Checkpoints-Through')) : -1
  const name = /^attachment; filename="((?:jobs-)?checkpoints-[0-9a-f]{32}-\d+-\d+\.jsonl)"$/.exec(header('Content-Disposition'))?.[1]
  const prefix = chain === 'jobs' ? 'jobs-' : ''
  if (!trailIdentity(trail) || !sha256(digest) || !['true', 'false'].includes(more) || !count(from) || !count(through) || through <= from
    || name !== `${prefix}checkpoints-${trail}-${from + 1}-${through}.jsonl`) throw new Error(failed)
  return { blob: await response.blob(), name, trail, from, through, digest, more: more === 'true', chain }
}

/**
 * The owner's word that a download went to the holder. Desk records it only
 * where the trail still gives those bytes from the holder's cursor;
 * `StaleHandover` where it does not. A download of Runner's chain of runs is
 * confirmed as one, against the holder's cursor for the chain.
 */
export async function confirmHandover(holder: string, checkpoints: Pick<Checkpoints, 'trail' | 'from' | 'through' | 'digest' | 'chain'>): Promise<Holder> {
  const { trail, from, through, digest, chain } = checkpoints
  const body = chain === 'jobs' ? { chain, trail, from, through, digest } : { trail, from, through, digest }
  const response = await deskFetch(`/api/audit/holders/${encodeURIComponent(holder)}/confirm`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  if (!response.ok) throw await handoverRefusal(response, msg('The hand-over could not be recorded. Please try again.'))
  const value: unknown = await response.json()
  if (!isHolder(value) || value.id !== holder) throw new Error(msg('The hand-over could not be recorded. Please try again.'))
  return value
}

/* The Jobs record ----------------------------------------------------------- */

/**
 * The Jobs record: the runtime's report on Desk's copy of Runner's chain of
 * runs, or its refusal to make one, each with the checkpoints Desk passed,
 * Runner's key as Desk reports it (`GET /api/runner-key`), and the copy's
 * line count; the decision record's older-runtime state; or that Runner is
 * not running, or that this desk has none.
 */
export type JobsRecord =
  | ({ state: 'report'; runtime?: string; report: AuditReport; runnerKey?: RunnerKey; chainLines: number } & HeldInputs)
  | ({ state: 'unverified'; runtime?: string; diagnostics: AuditDiagnostic[]; runnerKey?: RunnerKey; chainLines: number } & HeldInputs)
  | { state: 'older-runtime'; runtime?: string; floor: string }
  | { state: 'not-running'; runnerKey?: RunnerKey }
  | { state: 'no-runner' }

export const JOBS_RECORD_KEY = ['desk-jobs-record'] as const

/**
 * The chassis's own sentences about the chain of runs, as it says them, so
 * that the page can show each in the owner's language (`systemMessage`).
 */
export const JOBS_REASONS = [
  sourceMessage('This desk has no Runner, so it has no chain of runs to hand over.'),
  sourceMessage('Runner is not running on this desk now, so Desk could not read its chain of runs.'),
  sourceMessage('No run is chained yet, so there is nothing to hand over.'),
  sourceMessage('This desk holds no project folder for Desk to run the runtime in.')
]

/**
 * A Jobs record as the chassis answers one: a report or a refusal with the
 * copy's line count, the held inputs and Runner's key each of their shapes;
 * and nothing of a report's where there is none.
 */
export function isJobsRecord(value: unknown): value is JobsRecord {
  if (!object(value) || !optional(value.runtime, text) || !optional(value.expected, count) || !optional(value.expectUnread, item => list(item, named))
    || !optional(value.handoverProblem, named) || !optional(value.chainLines, count) || value.runnerKey !== undefined && runnerKeyOf(value.runnerKey) === undefined) return false
  switch (value.state) {
    case 'report': return isAuditReport(value.report) && count(value.chainLines)
    case 'unverified': return count(value.chainLines) && list(value.diagnostics, isDiagnostic) && value.diagnostics.length > 0
    case 'older-runtime': return text(value.floor) && value.report === undefined
    case 'not-running': case 'no-runner': return value.report === undefined && value.chainLines === undefined
  }
  return false
}

/** The runtime's check of a fresh copy of Runner's chain of runs, run now. */
export async function readJobsRecord(signal?: AbortSignal): Promise<JobsRecord> {
  const failed = msg('The chain of runs could not be checked. Please try again.')
  const response = await deskFetch('/api/audit/jobs-verify', { signal })
  if (!response.ok) {
    let body: { error?: unknown } = {}
    try { body = await response.json() as typeof body } catch { /* The status is still an answer. */ }
    throw new Error(text(body.error) ? body.error : failed)
  }
  const value: unknown = await response.json()
  if (!isJobsRecord(value)) throw new Error(failed)
  if ('runnerKey' in value && value.runnerKey !== undefined) return { ...value, runnerKey: runnerKeyOf(value.runnerKey) }
  return value
}

/**
 * Check the chain of runs again, as after a hand-over of it is confirmed. The
 * Jobs record's query is disabled, so this fetches into it; a failure is the
 * query's to show.
 *
 * **A check already in flight is cancelled, never joined** (review round 1).
 * It was asked before the confirmation, so it reads the chain without the
 * checkpoints just handed over; joined, it would stand as the check after
 * them. **And the fetch is marked to run only on request**, as the panel's own
 * query is: `fetchQuery` sets the query's options, and without the mark a
 * change to the project would cancel it, with nothing to run it again.
 */
export async function checkJobsRecordAgain(client: QueryClient): Promise<void> {
  await client.cancelQueries({ queryKey: JOBS_RECORD_KEY })
  await client.fetchQuery({ queryKey: JOBS_RECORD_KEY, queryFn: ({ signal }) => readJobsRecord(signal), meta: ON_REQUEST_ONLY, staleTime: 0, retry: false }).catch(() => undefined)
}

/**
 * Runner's whole chain of runs, as Desk passed it on (`GET
 * /api/operations/run-chain`): read as a Blob and never as text, so nothing
 * decodes or encodes it, or a refusal in Desk's words.
 */
export async function downloadRunChain(): Promise<Blob> {
  const response = await deskFetch('/api/operations/run-chain')
  if (!response.ok) {
    let body: { error?: unknown } = {}
    try { body = await response.json() as typeof body } catch { /* The status is still an answer. */ }
    throw new Error(text(body.error) ? body.error : msg('The chain of runs could not be downloaded. Please try again.'))
  }
  return response.blob()
}

/* Stamping ------------------------------------------------------------------ */

/** One root certificate Desk keeps: its subject, and the SHA-256 of its DER bytes. */
export type StampingRoot = { subject: string; sha256: string }
/** One revocation-list file Desk keeps: the SHA-256 of its bytes, and how many lists it holds. */
export type StampingList = { sha256: string; lists: number }
/** Stamping settings, as Desk keeps them or would keep them: `setAt` by Desk's clock, once kept. */
export type StampingSettings = { authority: string; intervalMinutes: number; policies: string[]; roots: StampingRoot[]; crls: StampingList[]; setAt?: number }
/**
 * One stamp run's outcome, by Desk's clock (`at`, in seconds): the runtime's
 * answer, a stamp or a checkpoint stamped already, with the checkpoint it
 * named (its trail, sequence and record digest) and, for a stamp, the
 * authority's time and policy as the runtime printed them; the runtime's
 * refusal, in its words; or Desk's.
 */
export type StampRun = {
  at: number; requested?: boolean
  status: 'stamped' | 'already-stamped' | 'refused' | 'problem'
  trail?: string; sequence?: number; digest?: string; stampedAt?: string; existedBy?: string; policy?: string
  diagnostics?: AuditDiagnostic[]; problem?: string
}
/**
 * The decision record's word on stamping: no authority; one set, its
 * settings, and whether their roots were given to the check (and why not);
 * settings Desk could not read now, and why; or none kept here, and why.
 * Beside it, where the runtime checked the stamps, the records pending a
 * stamp, or, where its report does not say how many records, the lines
 * (`pendingLines`); the last stamp run since Desk started, whether one is
 * running, and the token that confirms a removal of the settings shown.
 */
export type AuditStamping = {
  state: 'none' | 'set' | 'unread' | 'unavailable'
  settings?: StampingSettings; problem?: string; removeToken?: string
  passed?: boolean; passProblem?: string; pending?: number; pendingLines?: number; last?: StampRun; running?: boolean
  /**
   * Whether a stamp the runtime checked reaches the very checkpoint the last
   * run named, its trail, sequence and record (issue #312), as the chassis
   * decides it; where not, why, in Desk's words. Given where the last run
   * named a checkpoint.
   */
  lastChecked?: LastRunChecked
}
/** The chassis's word on the last run's checkpoint: checked, or why not. */
export type LastRunChecked = { checked: true } | { checked: false; reason: string }
/** What the owner proposes: the roots as PEM text, each revocation list's bytes in base64. */
export type StampingProposal = { authority: string; intervalMinutes?: number; roots: string; policies: string[]; crls: string[] }
/** What a check answers: what Desk would keep, and the token that confirms it. */
export type StampingChecked = { token: string; shown: StampingSettings }

/**
 * The chassis's own sentences about stamping, as it says them, so that the
 * page can show each in the owner's language (`systemMessage`). A reason that
 * carries the runtime's or custody's words keeps them as they were said.
 */
export const STAMPING_REASONS = [
  sourceMessage("Send the stamping settings as JSON: the authority's address, the interval in minutes, the root certificates, and any policy OIDs and revocation lists."),
  sourceMessage("Give the authority's address as an http or https URL of at most 2048 characters, with a host, and with no user name, password, fragment, space or control character."),
  sourceMessage('Give the interval as a whole number of minutes from 5 to 1440.'),
  sourceMessage('Give the root certificates you trust for this authority as PEM, at most 262144 bytes of CERTIFICATE blocks and nothing else, at least one, each of which can be read.'),
  sourceMessage('Give at most 8 policy OIDs, each once, each a dotted object identifier such as 1.2.3.4 of at most 64 characters.'),
  sourceMessage('Give at most 4 revocation lists, each once, each of at most 262144 bytes: X509 CRL blocks in PEM and nothing else, or one list in DER, each of which can be read.'),
  sourceMessage('Confirm the stamping settings with the token Desk gave when it showed them.'),
  sourceMessage('Remove the time-stamping authority with the token the decision record gave.'),
  sourceMessage('The stamping settings changed after Desk showed them, so nothing was changed. Check the decision record again.'),
  sourceMessage('This confirmation was used already, so nothing was changed. Check the decision record again for a fresh one.'),
  sourceMessage('No time-stamping authority is set for this desk, so Desk stamps nothing.'),
  sourceMessage('A stamp run for this desk is in progress, so Desk starts no other. The decision record shows its outcome once it ends.'),
  sourceMessage('Desk is stopping, so it starts no stamp run.'),
  sourceMessage("The runtime's audit stamp did not answer as documented, so Desk cannot say whether it stamped. The decision record, checked again, shows the stamps the runtime accepts."),
  sourceMessage('A stamp request needs no settings: send {} as JSON.'),
  sourceMessage('Desk could not read the stamping settings it keeps for this desk, so it uses none of them now: {{reason}}.'),
  sourceMessage('Desk could not keep these stamping settings, and the decision record shows the settings it reads now: {{reason}}.'),
  sourceMessage('Desk keeps no stamping settings here: {{reason}}.'),
  sourceMessage('The stamp run did not finish: {{reason}}.'),
  sourceMessage('Desk could not tell where the trail ends now, so it asked for no stamp: {{reason}}.'),
  sourceMessage('This runtime (jpack {{version}}) has no audit stamp. Stamping needs jpack {{floor}} or later.'),
  sourceMessage('Desk could not hand the runtime the roots it keeps for this desk, so no stamp was checked: {{reason}}.'),
  sourceMessage("A cross-site request cannot change this desk's stamping."),
  sourceMessage('The stamping settings changed while the stamp run made its checks, so it asked for no stamp. The next run uses the settings as they are now.'),
  sourceMessage("the runtime was given no roots to check the stamps with"),
  sourceMessage("the runtime did not check the stamps"),
  sourceMessage("no stamp the runtime checked covers a record of this trail"),
  sourceMessage("the stamps the runtime checked are of another trail than the one the run named"),
  sourceMessage("the stamps the runtime checked reach record {{record}}, before the checkpoint the run named"),
  sourceMessage("this trail holds no chained record at that sequence now"),
  sourceMessage("Desk could not ask the runtime for the checkpoint at that sequence now"),
  sourceMessage("the record at that sequence now is not the record the run named, as a trail put back to an earlier point and written since would not be")
]

const sha256Form = (value: unknown): value is string => text(value) && /^sha256:[0-9a-f]{64}$/.test(value)
const isRoot = (value: unknown): value is StampingRoot => object(value) && text(value.subject) && sha256Form(value.sha256)
const isList = (value: unknown): value is StampingList => object(value) && sha256Form(value.sha256) && count(value.lists) && value.lists > 0
const instant = (value: unknown): value is string => named(value) && !Number.isNaN(Date.parse(value))

/** Settings as the chassis shows them: an address, an interval within its bounds, at least one root, and lists of their shapes. */
export function isStampingSettings(value: unknown): value is StampingSettings {
  return object(value) && named(value.authority) && /^https?:\/\//i.test(value.authority) && count(value.intervalMinutes) && value.intervalMinutes >= 5 && value.intervalMinutes <= 1440
    && list(value.policies, named) && list(value.roots, isRoot) && value.roots.length > 0 && list(value.crls, isList) && optional(value.setAt, count)
}

/** A stamp run as the chassis says it: each outcome with what it carries, and nothing else. */
export function isStampRun(value: unknown): value is StampRun {
  if (!object(value) || !count(value.at) || !optional(value.requested, item => typeof item === 'boolean')) return false
  const checkpoint = hex(32)(value.trail) && count(value.sequence) && value.sequence > 0 && optional(value.digest, sha256Form)
  const stamp = ['stampedAt', 'existedBy', 'policy'].map(name => value[name])
  switch (value.status) {
    case 'stamped': return checkpoint && instant(value.stampedAt) && instant(value.existedBy) && named(value.policy) && value.diagnostics === undefined && value.problem === undefined
    case 'already-stamped': return checkpoint && stamp.every(item => item === undefined) && value.diagnostics === undefined && value.problem === undefined
    case 'refused': return list(value.diagnostics, isDiagnostic) && value.diagnostics.length > 0 && value.problem === undefined && value.trail === undefined && value.digest === undefined
    case 'problem': return named(value.problem) && value.diagnostics === undefined && value.trail === undefined && value.digest === undefined
  }
  return false
}

/** The decision record's word on stamping: each state with what it carries, a removal token of 96 hexadecimal characters, and a count pending, of records or of lines. */
/** The last run's word as the chassis gives it: checked with no reason, or not with one. */
export function isLastRunChecked(value: unknown): value is LastRunChecked {
  return object(value) && (value.checked === true && value.reason === undefined || value.checked === false && named(value.reason))
}

export function isAuditStamping(value: unknown): value is AuditStamping {
  if (!object(value) || !optional(value.removeToken, hex(96)) || !optional(value.passed, item => typeof item === 'boolean') || !optional(value.passProblem, named)
    || !optional(value.pending, count) || !optional(value.pendingLines, count) || value.pending !== undefined && value.pendingLines !== undefined
    || !optional(value.last, isStampRun) || !optional(value.running, item => typeof item === 'boolean') || !optional(value.lastChecked, isLastRunChecked)) return false
  switch (value.state) {
    case 'none': return value.settings === undefined && value.removeToken === undefined && !value.passed && value.problem === undefined
    case 'set': return isStampingSettings(value.settings) && value.problem === undefined && typeof value.removeToken === 'string'
    case 'unread': return value.settings === undefined && named(value.problem) && !value.passed
    case 'unavailable': return value.settings === undefined && named(value.problem) && value.removeToken === undefined && !value.passed
  }
  return false
}

async function stampingRefusal(response: Response, fallback: string): Promise<Error> {
  let body: { error?: unknown } = {}
  try { body = await response.json() as typeof body } catch { /* The status is still an answer. */ }
  const message = text(body.error) ? body.error : fallback
  return new Error(message)
}

const postJSON = (url: string, body: unknown) => deskFetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })

/** Hold a proposal to Desk's rules: what Desk would keep, and the token that confirms it, or why not, in Desk's words. */
export async function checkStamping(proposal: StampingProposal): Promise<StampingChecked> {
  const failed = msg('The stamping settings could not be checked. Please try again.')
  const response = await postJSON('/api/audit/stamping/check', proposal)
  if (!response.ok) throw await stampingRefusal(response, failed)
  const value: unknown = await response.json()
  if (!object(value) || !hex(96)(value.token) || !isStampingSettings(value.shown)) throw new Error(failed)
  return value as StampingChecked
}

/** Keep the proposal the token confirms, or say why not in Desk's words: the settings or the proposal changed since, or the token was used. */
export async function setStamping(proposal: StampingProposal, token: string): Promise<void> {
  const response = await postJSON('/api/audit/stamping', { ...proposal, token })
  if (!response.ok) throw await stampingRefusal(response, msg('The stamping settings could not be kept. Check the decision record again.'))
}

/** Remove the settings the decision record showed, or say why not in Desk's words: they changed since, or the token was used. */
export async function removeStamping(token: string): Promise<void> {
  const response = await postJSON('/api/audit/stamping/remove', { token })
  if (!response.ok) throw await stampingRefusal(response, msg('The time-stamping authority could not be removed. Check the decision record again.'))
}

/** One stamp run, on the owner's request: its outcome, or why none ran, in Desk's words. */
export async function stampNow(): Promise<StampRun> {
  const failed = msg('Desk could not read what the stamp run answered. The decision record, checked again, shows the stamps the runtime accepts.')
  const response = await postJSON('/api/audit/stamping/stamp', {})
  if (!response.ok) throw await stampingRefusal(response, failed)
  const value: unknown = await response.json()
  if (!object(value) || !isStampRun(value.run)) throw new Error(failed)
  return value.run
}
