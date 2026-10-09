/**
 * Records shaped as Runner v0.5.0 serves them, for the Activity tab (#213).
 *
 * `GET runs/{run}` answers the whole run; the lists answer it without `input`,
 * `audit`, `auditBytes` and `auditSignatures` (`internal/runner/list.go`), and
 * `summary` strips them the same way, so a test that read evidence from a
 * list would see none. Occurrences are public records: no input.
 */
import { JobsRequestError, type Release, type Run } from '../client'
import type { JournalPage } from '../journal'
import type { Occurrence, Trigger } from '../triggerTypes'

export const JOB = 'job_' + 'a'.repeat(32)
export const OWNER = 'local-owner:' + 'b'.repeat(64)
export const TRIGGER = 'trg_' + 'c'.repeat(32)
const id = (prefix: string, n: number) => prefix + String(n).padStart(32, '0')
export const at = (minute: number, second = 0) => `2026-10-03T10:${String(minute).padStart(2, '0')}:${String(second).padStart(2, '0')}Z`

/** The record line, and the sidecar line that signs it, as the runtime wrote them. */
export const RECORD_LINE = '{"kind":"decision","record":"one"}'
export const SIDECAR_LINES = '{"record":1,"signature":"c2ln"}\n'
const b64 = (text: string) => btoa(text)

export const release = {
  id: 'rel_' + 'd'.repeat(32), title: 'Intake policy', packId: 'intake', packVersion: '1.2.0',
  packDigest: 'sha256:' + 'e'.repeat(64), runtimeDigest: 'sha256:' + 'f'.repeat(64),
  createdAt: at(0), pack: '{}', sample: { facts: {} }, preview: { disposition: { kind: 'outcome', outcomeId: 'accept', reasons: [], handoff: { state: 'none' } } }, tests: 'passed'
} as Release

export const trigger: Trigger = {
  id: TRIGGER, jobId: JOB, revision: 3, authority: 'local', paused: false, createdAt: at(0), updatedAt: at(0), hasKey: false,
  config: { name: 'Nightly intake', kind: 'schedule', missed: 'skip', overlap: 'skip', queueSeconds: 3600, schedule: { kind: 'daily', timezone: 'UTC', time: '10:00', startAt: at(0) } }
}

const decision = (outcomeId: string) => ({ disposition: { kind: 'outcome', outcomeId, reasons: ['Policy met'], handoff: { state: 'none' } } })
const base = (n: number, state: Run['state'], minute: number): Run => ({ id: id('run_', n), jobId: JOB, releaseId: release.id, revision: 1, state, createdAt: at(minute), attempt: state === 'queued' ? 0 : 1, requestedBy: OWNER, input: { facts: { amount: n } } })
const origin = { occurrenceId: id('occ_', 1), triggerId: TRIGGER, triggerRevision: 2, kind: 'schedule', scheduledAt: at(29) }

/** Whole records, newest first by Runner's sequence. */
export const runs: Record<string, Run> = {
  queued: { ...base(1, 'queued', 40) },
  running: { ...base(2, 'running', 30), startedAt: at(30, 5), requestedBy: 'trigger:' + TRIGGER, trigger: origin },
  signed: { ...base(3, 'completed', 20), startedAt: at(20, 1), finishedAt: at(20, 9), result: decision('accept'), audit: { kind: 'decision' }, auditBytes: b64(RECORD_LINE), auditSignatures: b64(SIDECAR_LINES) },
  unsigned: { ...base(4, 'completed', 18), startedAt: at(18, 1), finishedAt: at(18, 4), result: decision('refer'), audit: { kind: 'decision' }, auditBytes: b64(RECORD_LINE) },
  // Expired in the queue: Runner finished it without ever starting it.
  failed: { ...base(5, 'failed', 16), finishedAt: at(17), problem: 'The automatic run expired in the queue before evaluation.\nSecond line Runner kept.' },
  // Left running when the runner stopped: finishedAt is when it started again.
  interrupted: { ...base(6, 'interrupted', 14), startedAt: at(14, 2), finishedAt: at(50), problem: 'The runner stopped during evaluation. Retained attempt files may contain an audit record. This run was not automatically repeated.' },
  // Recorded before Runner kept requestedBy.
  legacy: { ...base(7, 'completed', 12), startedAt: at(12, 1), finishedAt: at(12, 3), result: decision('accept'), requestedBy: undefined }
}

/** A run as a list returns it. */
export function summary(run: Run): Run {
  const { input: _input, audit: _audit, auditBytes: _bytes, auditSignatures: _signatures, ...rest } = run
  return rest as Run
}

const occurrence = (n: number, state: Occurrence['state'], minute: number, extra: Partial<Occurrence> = {}): Occurrence => ({ id: id('occ_', n), jobId: JOB, releaseId: release.id, jobRevision: 1, triggerId: TRIGGER, triggerRevision: 3, kind: 'schedule', receivedAt: at(minute), expiresAt: at(59), state, ...extra })
export const occurrences: Record<string, Occurrence> = {
  submitted: occurrence(1, 'submitted', 29, { triggerRevision: 2, scheduledAt: at(29), runId: runs.running.id }),
  skipped: occurrence(2, 'skipped', 27, { scheduledAt: at(27), reason: 'overlap' }),
  expired: occurrence(3, 'expired', 25, { reason: 'queue-expired' }),
  failed: occurrence(4, 'failed', 23, { reason: 'Source acquisition was interrupted. It was not repeated automatically.' }),
  cancelled: occurrence(5, 'cancelled', 21, { reason: 'Cancelled locally. Provider cancellation is best effort; late results will not be evaluated.' }),
  waiting: occurrence(6, 'waiting', 10, { preparation: { startedAt: at(10, 2), deadline: at(59), tasks: [{ name: 'ledger', id: 'task-1', state: 'running', startedAt: at(10, 2) }] } }),
  attention: occurrence(7, 'needs-attention', 8, { reason: 'The acquisition worker stopped before retaining the response. The source was not called again.', preparation: { startedAt: at(8, 1), deadline: at(59), tasks: [{ name: 'ledger', id: 'task-2', state: 'needs-attention', startedAt: at(8, 1) }] } })
}

/**
 * A job's journal as Runner v0.7.0 serves it (`GET /v1/jobs/{job}/events`):
 * the entries after a cursor, by sequence, at most `size` of them; `next` the
 * last one's sequence, or the cursor when there is none; `more` when another
 * page is already there. `absent` answers 404, as a Runner or a Desk that does
 * not serve the route does. By default, an empty journal that began when the
 * job was created.
 */
export interface StandInJournal { began?: string; entries?: { sequence: number }[]; size?: number }
export function journalPage(journal: StandInJournal, after: number): JournalPage {
  const entries = journal.entries ?? [], size = journal.size ?? 50
  const items = entries.filter(entry => entry.sequence > after).slice(0, size)
  const next = items.length ? items[items.length - 1]!.sequence : after
  return { journalBegan: journal.began ?? at(0), items, next, more: entries.some(entry => entry.sequence > next) }
}

/** Runner's lists and records for one job, answering as Desk's `jobsAPI` does. */
export function runner(options: { runs?: Run[]; occurrences?: Occurrence[]; pages?: Record<string, unknown>; journal?: StandInJournal | 'absent' } = {}) {
  const all = options.runs ?? Object.values(runs), occurring = options.occurrences ?? Object.values(occurrences)
  return async (path: string): Promise<unknown> => {
    if (options.pages?.[path]) return options.pages[path]
    const events = /^jobs\/job_a{32}\/events\?after=(\d+)$/.exec(path)
    if (events) {
      if (options.journal === 'absent') throw new JobsRequestError('Unknown Jobs operation.', 404)
      return journalPage(options.journal ?? {}, Number(events[1]))
    }
    if (path === `jobs/${JOB}`) return { job: { id: JOB, name: 'Daily intake', releaseId: release.id, revision: 1, createdAt: at(0) }, release }
    if (path === `jobs/${JOB}/triggers`) return { items: [trigger], localFiles: true }
    const listed = /^jobs\/job_a{32}\/runs\?(?:state=(\w+)&)?after=0$/.exec(path)
    if (listed) return { items: all.filter(run => !listed[1] || run.state === listed[1]).map(summary), next: 0 }
    if (path === `jobs/${JOB}/occurrences?after=0`) return { items: occurring, next: 0 }
    const run = /^runs\/(run_\d{32})$/.exec(path)
    if (run) { const found = all.find(r => r.id === run[1]); if (found) return found }
    throw new Error(`unexpected request: ${path}`)
  }
}
