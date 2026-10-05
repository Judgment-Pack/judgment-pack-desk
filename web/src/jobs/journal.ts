/**
 * Runner's journal of job activity (Runner v0.6.0, `GET /v1/jobs/{job}/events`,
 * its `docs/design/activity-journal.md`), as the Activity tab reads it (#218).
 *
 * **Runner's record, in Runner's order.** Each entry is one change of state
 * Runner made, written in the transaction that made it, or a request to start
 * work it refused. Entries are served oldest first, in the order their changes
 * committed; the tab keeps that order and never orders entries by their time
 * (`at` is Runner's clock: "Order is sequence, never at"). Desk derives no entry
 * from records it observed itself.
 *
 * **Paging.** Pages hold at most 50 entries after a cursor, `after` meaning
 * later than. A reader follows `next` while `more` says another page is
 * already available, and once it has caught up asks again with the same
 * cursor: never from 0 again.
 *
 * **What it is not.** The operator's own log, kept in the store the operator
 * holds: not chained, not signed, and binding nothing against the operator.
 * Nothing from before `journalBegan` has an entry, and nothing is back-filled.
 */
import { msg } from '../i18n'
import { JobsRequestError, jobsAPI } from './client'

/**
 * Every kind Runner writes, as the pinned Runner's `openapi.json` lists them
 * (`JournalEntry.kind`), at the source tag below. A Runner that adds a kind
 * fails a test until the kind is worded here.
 */
export const JOURNAL_SOURCE = 'v0.6.0'
export const JOURNAL_KINDS = [
  'trigger.configured', 'trigger.paused', 'trigger.resumed', 'trigger.key-rotated',
  'occurrence.received', 'occurrence.skipped', 'occurrence.preparing', 'occurrence.ready', 'occurrence.submitted',
  'occurrence.failed', 'occurrence.expired', 'occurrence.needs-attention', 'occurrence.cancelled', 'occurrence.reconciled',
  'run.queued', 'run.started', 'run.expired', 'run.completed', 'run.failed', 'run.interrupted',
  'release.previewed', 'job.created', 'admission.refused',
  'journal.began', 'runner.started', 'runner.stopped'
] as const
export type JournalKind = typeof JOURNAL_KINDS[number]

/** Each kind in plain words: what the entry's kind says, and nothing more. */
const KIND_WORDS: Record<JournalKind, () => string> = {
  'trigger.configured': () => msg('Trigger configured'),
  'trigger.paused': () => msg('Trigger paused'),
  'trigger.resumed': () => msg('Trigger resumed'),
  'trigger.key-rotated': () => msg('Trigger key rotated'),
  'occurrence.received': () => msg('Occurrence received'),
  'occurrence.skipped': () => msg('Occurrence skipped'),
  'occurrence.preparing': () => msg('Occurrence preparing'),
  'occurrence.ready': () => msg('Occurrence ready'),
  'occurrence.submitted': () => msg('Occurrence submitted'),
  'occurrence.failed': () => msg('Occurrence failed'),
  'occurrence.expired': () => msg('Occurrence expired'),
  'occurrence.needs-attention': () => msg('Occurrence needs attention'),
  'occurrence.cancelled': () => msg('Occurrence canceled'),
  'occurrence.reconciled': () => msg('Occurrence reconciled'),
  'run.queued': () => msg('Run queued'),
  'run.started': () => msg('Run started'),
  'run.expired': () => msg('Run expired in the queue'),
  'run.completed': () => msg('Run completed'),
  'run.failed': () => msg('Run failed'),
  'run.interrupted': () => msg('Run interrupted'),
  'release.previewed': () => msg('Release previewed'),
  'job.created': () => msg('Job created'),
  'admission.refused': () => msg('Request to start work refused'),
  'journal.began': () => msg('Journal began'),
  'runner.started': () => msg('Runner started'),
  'runner.stopped': () => msg('Runner stopped')
}
/** The kinds this Desk words, for the test that holds them to Runner's list. */
export const WORDED_KINDS = Object.keys(KIND_WORDS)

/** An entry's kind in words; a kind this Desk does not know is named, never dropped. */
export function kindText(kind: unknown): string {
  if (typeof kind === 'string' && Object.hasOwn(KIND_WORDS, kind)) return KIND_WORDS[kind as JournalKind]()
  return msg('An entry of a kind this Desk does not know: {{kind}}', { kind: typeof kind === 'string' ? kind : JSON.stringify(kind ?? null) })
}

/** Who Runner can say initiated a change (`JournalActor`). Never a person. */
export type JournalActor =
  | { kind: 'installation'; owner: string }
  | { kind: 'trigger'; trigger: string; revision: number }
  | { kind: 'trigger-credential'; trigger: string; keyRevision: number | null }
  | { kind: 'cloud-connection'; connection: string }
  | { kind: 'runner' }

/**
 * One entry as Runner serves it. Every member is read as possibly absent or of
 * another type: an entry Desk cannot read is still shown, with what it lacks
 * said to be not recorded.
 */
export interface JournalEntry {
  sequence?: unknown
  entryVersion?: unknown
  kind?: unknown
  at?: unknown
  by?: unknown
  concerns?: { job?: string; release?: string; trigger?: string; occurrence?: string; run?: string }
  revision?: unknown
  from?: unknown
  to?: unknown
  reason?: unknown
  previousRevision?: unknown
  request?: unknown
  status?: unknown
  code?: unknown
  [member: string]: unknown
}
export interface JournalPage { journalBegan: string; items: unknown[]; next: number; more: boolean }

/** What the tab has read of a job's journal: its entries in Runner's order, and the cursor to ask again with. */
export interface Journal { entries: JournalEntry[]; next: number; began: unknown }
/** A Runner, or a Desk, that answers the job's journal route with 404 serves no journal for it. */
export type JournalReading = { served: true; journal: Journal } | { served: false }

/** An item as an entry: a value that is not an object is an entry with nothing recorded. */
export function entryOf(item: unknown): JournalEntry {
  return item !== null && typeof item === 'object' && !Array.isArray(item) ? item as JournalEntry : {}
}

/**
 * Reads the pages after what is already read, following `next` while Runner
 * says another page is available, and returns everything read so far. It never
 * asks from 0 again once it has a cursor, and a page that moves the cursor
 * nowhere ends the reading, whatever it says of `more`.
 */
export async function readJournal(page: (after: number) => Promise<JournalPage>, prior?: Journal): Promise<Journal> {
  const entries = prior ? [...prior.entries] : []
  let next = prior?.next ?? 0, began = prior?.began
  for (;;) {
    const answer = await page(next)
    for (const item of answer.items ?? []) entries.push(entryOf(item))
    began = answer.journalBegan
    const moved = Number.isSafeInteger(answer.next) && answer.next > next
    if (moved) next = answer.next
    if (!answer.more || !moved) return { entries, next, began }
  }
}

/** The journal of a job through Desk's Jobs route, or that the route is not served. */
export async function readJobJournal(jobId: string, prior: Journal | undefined, signal?: AbortSignal): Promise<JournalReading> {
  try {
    return { served: true, journal: await readJournal(after => jobsAPI<JournalPage>(`jobs/${jobId}/events?after=${after}`, undefined, undefined, signal), prior) }
  } catch (error) {
    if (error instanceof JobsRequestError && error.status === 404) return { served: false }
    throw error
  }
}

/**
 * Whether the job may hold activity from before its journal began: its record
 * was created before `journalBegan`, or either time does not read as one, so
 * that it cannot be told. Both are Runner's own times.
 */
export function beforeTheJournal(began: unknown, jobCreatedAt: unknown): boolean {
  const start = typeof began === 'string' && began ? Date.parse(began) : NaN
  const created = typeof jobCreatedAt === 'string' && jobCreatedAt ? Date.parse(jobCreatedAt) : NaN
  return Number.isNaN(start) || Number.isNaN(created) || created < start
}

/** How often the tab asks again once it has caught up: never faster than the record lists. */
export const JOURNAL_REFRESH_MS = 5000

/**
 * Where the record each entry created stands in the journal: a run at its
 * `run.queued`, an occurrence at its admission (an `occurrence.*` entry from
 * null). Keyed as the record rows are, `run:<id>` and `occurrence:<id>`.
 */
export function creations(entries: JournalEntry[]): Map<string, number> {
  const places = new Map<string, number>()
  entries.forEach((entry, place) => {
    const concerns = entry.concerns && typeof entry.concerns === 'object' ? entry.concerns : {}
    let key: string | undefined
    if (entry.kind === 'run.queued' && typeof concerns.run === 'string') key = `run:${concerns.run}`
    else if (typeof entry.kind === 'string' && entry.kind.startsWith('occurrence.') && entry.from === null && typeof concerns.occurrence === 'string') key = `occurrence:${concerns.occurrence}`
    if (key && !places.has(key)) places.set(key, place)
  })
  return places
}
