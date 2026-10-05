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
 * **Reading forward, bounded.** Runner pages the journal oldest first and has
 * no way to read it from its newest entries, so the tab reads it from the
 * start, one page after another, never two at once, giving the browser a turn
 * between pages. Until it reaches the end it says how far it has read and
 * shows no entry, since a journal read in part would be shown as the whole of
 * it. It keeps at most `JOURNAL_LIMITS.keep` entries, dropping the oldest and
 * counting them, and the page renders the newest `window`, in steps. A reading
 * that fails stops where it is, and continues from that cursor when asked.
 *
 * **What it is not.** The operator's own log, kept in the store the operator
 * holds: not chained, not signed, and binding nothing against the operator.
 * Nothing from before `journalBegan` has an entry, and nothing is back-filled.
 */
import { useEffect, useRef, useState } from 'react'
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

/**
 * How much of the journal the page holds and renders. `keep`: the most entries
 * held in memory; the oldest beyond it are dropped and counted. `window`: the
 * newest entries rendered at first; `step`: how many more each "show earlier"
 * adds. Entries are small, but 20,000 of them as objects are already tens of
 * megabytes in a browser tab.
 */
export interface JournalLimits { keep: number; window: number; step: number }
export const JOURNAL_LIMITS: JournalLimits = { keep: 20000, window: 500, step: 500 }

/**
 * What has been read of a job's journal: the entries kept, oldest first, in
 * Runner's order; how many earlier entries were read and dropped, which is the
 * place of the first kept entry; the cursor to ask again with; and when the
 * journal began.
 */
export interface Journal { entries: JournalEntry[]; dropped: number; next: number; began: unknown }
export const emptyJournal = (): Journal => ({ entries: [], dropped: 0, next: 0, began: undefined })
/** How many entries have been read, kept or not. */
export const readCount = (journal: Journal) => journal.dropped + journal.entries.length

/** An item as an entry: a value that is not an object is an entry with nothing recorded. */
export function entryOf(item: unknown): JournalEntry {
  return item !== null && typeof item === 'object' && !Array.isArray(item) ? item as JournalEntry : {}
}

/**
 * Reads the pages after what `journal` holds, one after another, into it:
 * following `next` while Runner says another page is available, keeping at
 * most `keep` entries, and calling `progress` and then `pause` between pages.
 * It never asks from 0 again once it has a cursor, and a page that moves the
 * cursor nowhere ends the reading, whatever it says of `more`. A page that
 * fails leaves `journal` as it was before that page, so that the reading can
 * continue from its cursor; one answered after `signal` aborted is not read.
 */
export async function catchUp(journal: Journal, page: (after: number) => Promise<JournalPage>, keep: number, progress: () => void, pause: () => Promise<void>, signal?: AbortSignal): Promise<void> {
  for (;;) {
    const answer = await page(journal.next)
    if (signal?.aborted) throw signal.reason
    const items = Array.isArray(answer.items) ? answer.items : []
    for (const item of items) journal.entries.push(entryOf(item))
    const excess = journal.entries.length - keep
    if (excess > 0) { journal.entries.splice(0, excess); journal.dropped += excess }
    journal.began = answer.journalBegan
    const moved = Number.isSafeInteger(answer.next) && answer.next > journal.next
    if (moved) journal.next = answer.next
    if (!answer.more || !moved) return
    progress()
    await pause()
  }
}

/** A turn for the browser: the next page is asked for from a new task, after rendering and input. */
export function yieldToBrowser(): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, 0))
}

/** What the page shows: the journal as it was when last read to its end. Never changed once made. */
export interface JournalSnapshot { entries: JournalEntry[]; dropped: number; began: unknown }
export type JournalState =
  /** Before Runner first answered. */
  | { phase: 'starting' }
  /** The route answered 404 to the first request: this runner serves no journal for the job. */
  | { phase: 'absent' }
  /** Catching up, read to the end, or stopped by a failure; `read` and `next` are the reading's own. */
  | { phase: 'reading' | 'caught-up' | 'stopped'; read: number; next: number; snapshot?: JournalSnapshot; reason?: string }

/**
 * Reads a job's journal for the page: from the start once, then from its
 * cursor every `JOURNAL_REFRESH_MS` once caught up. `resume` continues a
 * reading that stopped, from its cursor.
 */
export function useJournalReader(jobId: string, keep: number): { state: JournalState; resume: () => void } {
  const [state, setState] = useState<JournalState>({ phase: 'starting' })
  const [attempt, setAttempt] = useState(0)
  const held = useRef<{ jobId: string; journal: Journal; snapshot?: JournalSnapshot }>(undefined)
  if (held.current?.jobId !== jobId) held.current = { jobId, journal: emptyJournal() }
  useEffect(() => {
    const reading = held.current!, journal = reading.journal, controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    const status = (phase: 'reading' | 'caught-up' | 'stopped', reason?: string) => setState({ phase, read: readCount(journal), next: journal.next, snapshot: reading.snapshot, ...(reason === undefined ? {} : { reason }) })
    const page = (after: number) => jobsAPI<JournalPage>(`jobs/${jobId}/events?after=${after}`, undefined, undefined, controller.signal)
    async function read() {
      for (;;) {
        try {
          await catchUp(journal, page, keep, () => status('reading'), yieldToBrowser, controller.signal)
        } catch (error) {
          if (controller.signal.aborted) return
          if (error instanceof JobsRequestError && error.status === 404 && readCount(journal) === 0 && journal.next === 0) { setState({ phase: 'absent' }); return }
          status('stopped', error instanceof Error ? error.message : msg('The local runner could not complete this request.'))
          return
        }
        if (controller.signal.aborted) return
        // A new snapshot only where the reading added to what is shown.
        const last = reading.snapshot
        if (!last || last.dropped + last.entries.length !== readCount(journal)) reading.snapshot = { entries: journal.entries.slice(), dropped: journal.dropped, began: journal.began }
        else if (last.began !== journal.began) reading.snapshot = { ...last, began: journal.began }
        status('caught-up')
        await new Promise<void>(resolve => { timer = setTimeout(resolve, JOURNAL_REFRESH_MS) })
        if (controller.signal.aborted) return
      }
    }
    void read()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [jobId, keep, attempt])
  return {
    state,
    resume: () => { const reading = held.current!; setState({ phase: 'reading', read: readCount(reading.journal), next: reading.journal.next, snapshot: reading.snapshot }); setAttempt(n => n + 1) }
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
  const known = found.get(entries)
  if (known) return known
  const places = new Map<string, number>()
  entries.forEach((entry, place) => {
    const concerns = entry.concerns && typeof entry.concerns === 'object' ? entry.concerns : {}
    let key: string | undefined
    if (entry.kind === 'run.queued' && typeof concerns.run === 'string') key = `run:${concerns.run}`
    else if (typeof entry.kind === 'string' && entry.kind.startsWith('occurrence.') && entry.from === null && typeof concerns.occurrence === 'string') key = `occurrence:${concerns.occurrence}`
    if (key && !places.has(key)) places.set(key, place)
  })
  found.set(entries, places)
  return places
}
/** A snapshot's entries never change, so where their records stand is found once per snapshot. */
const found = new WeakMap<JournalEntry[], Map<string, number>>()
