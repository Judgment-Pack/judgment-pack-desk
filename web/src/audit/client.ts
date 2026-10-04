/**
 * The decision-record panel (ADR-0010, sections 4 and 6), as the page calls it.
 *
 * `GET /api/audit/verify` answers what the runtime's own `jpack audit verify`
 * finds in this desk's trail, run with no key, no held checkpoint and no
 * stamping roots: the report, with the runtime's member names; the runtime's
 * refusal to make one; that the runtime has no audit commands; or that the
 * project keeps no trail. It runs only when asked. Nothing here judges the
 * trail: the runtime's report is the answer.
 */
import { deskFetch } from '../files/client'
import { msg } from '../i18n'

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
  /** The runtime's own sentences, in English, as it wrote them. */
  establishes: string[]
  doesNotEstablish: string[]
}
export type AuditDiagnostic = { code: string; message: string }
export type AuditRecord =
  | { state: 'report'; runtime?: string; report: AuditReport }
  | { state: 'unverified'; runtime?: string; diagnostics: AuditDiagnostic[] }
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
    && list(value.establishes, text) && list(value.doesNotEstablish, text)
    && value.segments.length <= value.segmentsTotal && value.discontinuities.length <= value.discontinuitiesTotal
    && value.findings.length <= value.findingsTotal && (value.status === 'invalid') === (value.findingsTotal > 0)
}

export function isAuditRecord(value: unknown): value is AuditRecord {
  if (!object(value) || !optional(value.runtime, text)) return false
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

/** The runtime's check of this desk's trail, run now. */
export async function readAuditRecord(signal?: AbortSignal): Promise<AuditRecord> {
  const response = await deskFetch('/api/audit/verify', { signal })
  if (!response.ok) throw await refusal(response)
  const value: unknown = await response.json()
  if (!isAuditRecord(value)) throw new Error(msg('The decision record could not be loaded. Please try again.'))
  return value
}
