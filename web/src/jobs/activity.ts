/**
 * The rows of a job's Activity tab (#213): what Runner recorded about the job,
 * read from records it already serves, one row per record, newest first.
 *
 * **Every time shown is one Runner stored, named for what it records.** A run
 * is submitted (`createdAt`), started (`startedAt`) and finished
 * (`finishedAt`); an interrupted run's `finishedAt` is the time Runner
 * recorded the interruption, which for a run left running when the runner
 * stopped is the time it started again (Runner v0.5.0,
 * `internal/runner/store.go:177-184`), not the time evaluation stopped. An
 * occurrence is received (`receivedAt`), and a scheduled one names the slot it
 * was for (`scheduledAt`); a source preparation started at
 * `preparation.startedAt`. A record without the time that orders it is no row:
 * nothing here supplies one.
 *
 * **One row per record, ordered by the time Runner first recorded it.** Runner
 * pages runs and occurrences separately, newest first by its own sequence, and
 * each record's first time (`createdAt`, `receivedAt`) is written when the
 * record is inserted, so it follows that sequence. A record on a page not yet
 * loaded is therefore never newer than the oldest loaded record of its kind,
 * and the merge shows only rows down to that point for each kind with more
 * pages: rows older than it are held until the next page is loaded, so loading
 * more never puts a row above one already shown. A later time of a record (its
 * start or finish) is shown in the row and does not order it, because a
 * record not yet loaded can have a later one.
 *
 * **A preparation is an occurrence's own state.** Runner keeps a source
 * preparation inside its occurrence; an occurrence that is waiting for sources
 * or needs attention reads as a preparation row, and in any other state as an
 * occurrence row.
 */
import type { JobInput, Run } from './client'
import type { Occurrence } from './triggerTypes'

export type ActivityKind = 'run' | 'occurrence' | 'preparation'
export type KindFilter = 'all' | ActivityKind
export const RUN_STATES = ['queued', 'running', 'completed', 'failed', 'interrupted'] as const
export const OCCURRENCE_STATES = ['received', 'skipped', 'expired', 'failed', 'cancelled'] as const
export const PREPARATION_STATES = ['waiting', 'needs-attention'] as const

/** What a stored timestamp records. */
export type StampName = 'submitted' | 'started' | 'finished' | 'interruption' | 'received' | 'scheduled' | 'preparation-started'
export interface Stamp { name: StampName; at: string; time: number }

/** Who initiated a record: this installation's owner, or a trigger. Never a person. */
export type Requester =
  | { kind: 'installation' }
  | { kind: 'trigger'; triggerId: string; revision?: number; triggerKind?: string }
  | { kind: 'unrecorded' }

export interface ActivityRow {
  key: string
  kind: ActivityKind
  /** The record's state, in the vocabulary of the state filter. */
  state: string
  /** The time Runner first recorded the record, which orders the row. */
  when: Stamp
  /** The record's other stored times, each named. */
  also: Stamp[]
  by: Requester
  releaseId: string
  run?: Run
  occurrence?: Occurrence
}

/** A stored time, or nothing: an absent or unreadable value is never replaced. */
export function stamp(name: StampName, at: string | undefined): Stamp | undefined {
  if (!at) return undefined
  const time = Date.parse(at)
  return Number.isNaN(time) ? undefined : { name, at, time }
}
const stored = (stamps: (Stamp | undefined)[]) => stamps.filter((value): value is Stamp => value !== undefined)

/**
 * Runner records `requestedBy` as its configured owner, which Desk sets to this
 * installation, or as `trigger:<id>`. A run recorded before Runner kept it has
 * none, and is said to have none.
 */
export function requesterOf(run: Pick<Run, 'requestedBy' | 'trigger'>): Requester {
  const by = run.requestedBy ?? ''
  if (by.startsWith('trigger:')) {
    const triggerId = by.slice('trigger:'.length)
    const origin = run.trigger?.triggerId === triggerId ? run.trigger : undefined
    return { kind: 'trigger', triggerId, revision: origin?.triggerRevision, triggerKind: origin?.kind }
  }
  return by ? { kind: 'installation' } : { kind: 'unrecorded' }
}

export function runRow(run: Run): ActivityRow | undefined {
  const when = stamp('submitted', run.createdAt)
  if (!when) return undefined
  const end = run.state === 'interrupted' ? stamp('interruption', run.finishedAt)
    : run.state === 'completed' || run.state === 'failed' ? stamp('finished', run.finishedAt)
      : undefined
  return { key: `run:${run.id}`, kind: 'run', state: run.state, when, also: stored([stamp('started', run.startedAt), end]), by: requesterOf(run), releaseId: run.releaseId, run }
}

const ENDED = new Set<string>(['skipped', 'expired', 'failed', 'cancelled'])
export function occurrenceRow(o: Occurrence): ActivityRow | undefined {
  const when = stamp('received', o.receivedAt)
  if (!when) return undefined
  const preparing = Boolean(o.preparation) && (PREPARATION_STATES as readonly string[]).includes(o.state)
  return {
    key: `occurrence:${o.id}`,
    kind: preparing ? 'preparation' : 'occurrence',
    state: preparing || ENDED.has(o.state) ? o.state : 'received',
    when,
    also: stored([stamp('scheduled', o.scheduledAt), stamp('preparation-started', o.preparation?.startedAt)]),
    by: { kind: 'trigger', triggerId: o.triggerId, revision: o.triggerRevision, triggerKind: o.kind },
    releaseId: o.releaseId,
    occurrence: o
  }
}

export interface ActivityFilter { kind: KindFilter; state: string; trigger: string }
export const MANUAL = 'manual'

/** The states the state filter offers for a kind. */
export function statesFor(kind: KindFilter): string[] {
  const states = kind === 'run' ? RUN_STATES : kind === 'occurrence' ? OCCURRENCE_STATES : kind === 'preparation' ? PREPARATION_STATES
    : [...RUN_STATES, ...OCCURRENCE_STATES, ...PREPARATION_STATES]
  return [...new Set<string>(states)]
}

/**
 * Which of Runner's lists a filter reads. Runner filters runs by state before
 * it pages them, as the global Runs list asks it to; occurrences it serves
 * unfiltered. A list no row of the filter can come from is not read, so its
 * pages never hold back the other's rows.
 */
export function streamsFor(filter: ActivityFilter): { runs: boolean; runState?: string; occurrences: boolean } {
  const runState = (RUN_STATES as readonly string[]).includes(filter.state) ? filter.state : undefined
  const runs = (filter.kind === 'all' || filter.kind === 'run') && (filter.state === 'all' || runState !== undefined)
  const occurrenceStates: readonly string[] = filter.kind === 'occurrence' ? OCCURRENCE_STATES : filter.kind === 'preparation' ? PREPARATION_STATES
    : [...OCCURRENCE_STATES, ...PREPARATION_STATES]
  const occurrences = filter.kind !== 'run' && filter.trigger !== MANUAL && (filter.state === 'all' || occurrenceStates.includes(filter.state))
  return { runs, ...(runs && runState ? { runState } : {}), occurrences }
}

export function keeps(filter: ActivityFilter) {
  return (row: ActivityRow) =>
    (filter.kind === 'all' || row.kind === filter.kind)
    && (filter.state === 'all' || row.state === filter.state)
    && (filter.trigger === 'all'
      || (filter.trigger === MANUAL ? row.by.kind !== 'trigger' : row.by.kind === 'trigger' && row.by.triggerId === filter.trigger))
}

export interface Loaded<T> { records: T[]; more: boolean }
export type StreamName = 'runs' | 'occurrences'
export interface Merged {
  rows: ActivityRow[]
  /** Loaded rows older than a record not yet loaded could be. */
  held: number
  /** The lists whose next page moves the boundary. */
  limiting: StreamName[]
}

const newestFirst = (a: ActivityRow, b: ActivityRow) => b.when.time - a.when.time || (a.key < b.key ? -1 : a.key > b.key ? 1 : 0)

export function mergeActivity(streams: { runs?: Loaded<Run>; occurrences?: Loaded<Occurrence> }, keep: (row: ActivityRow) => boolean = () => true): Merged {
  const built: Record<StreamName, ActivityRow[]> = {
    runs: rowsOf(streams.runs?.records.map(runRow)),
    occurrences: rowsOf(streams.occurrences?.records.map(occurrenceRow))
  }
  // A list with more pages holds back every row older than its oldest loaded
  // record. One whose loaded records carry no readable time holds back all.
  const floors: Partial<Record<StreamName, number>> = {}
  let boundary = -Infinity
  for (const name of ['runs', 'occurrences'] as const) {
    if (!streams[name]?.more) continue
    const floor = built[name].reduce((oldest, row) => Math.min(oldest, row.when.time), Infinity)
    floors[name] = floor
    boundary = Math.max(boundary, floor)
  }
  const all = [...built.runs, ...built.occurrences].filter(keep).sort(newestFirst)
  const rows = all.filter(row => row.when.time >= boundary)
  return { rows, held: all.length - rows.length, limiting: (['runs', 'occurrences'] as const).filter(name => floors[name] === boundary) }
}
function rowsOf(rows: (ActivityRow | undefined)[] | undefined): ActivityRow[] {
  return (rows ?? []).filter((row): row is ActivityRow => row !== undefined)
}

/** What the run record holds, each present or absent. Nothing here checks any of it. */
export interface Evidence { inputs: boolean; recordBytes: boolean; sidecar: boolean; receipts: boolean }
function hasInputs(input?: JobInput): boolean {
  return Boolean(input && (input.facts !== undefined || input.evidence !== undefined || input.source !== undefined))
}
export function evidenceOf(run: Run): Evidence {
  return {
    inputs: hasInputs(run.input),
    recordBytes: Boolean(run.auditBytes),
    sidecar: Boolean(run.auditSignatures),
    receipts: (run.input?.preparation?.cites?.length ?? 0) > 0
  }
}

export function firstLine(text: string): string {
  return text.split(/\r?\n/, 1)[0] ?? ''
}

/** The bytes a base64 member carries, as Runner encoded them (Go's standard encoding). */
export function decodeBase64(text: string): Uint8Array {
  const binary = atob(text)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index)
  return bytes
}

/** The SHA-256 of bytes, written as Runner writes a digest. */
export async function sha256(bytes: Uint8Array): Promise<string> {
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', bytes as BufferSource))
  return 'sha256:' + [...digest].map(byte => byte.toString(16).padStart(2, '0')).join('')
}
