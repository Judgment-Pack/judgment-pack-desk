/**
 * Runner's journal of job activity (Runner v0.8.0, `GET /v1/jobs/{job}/events`,
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
 * **Reading forward, bounded.** Runner pages the journal oldest first, and
 * before v0.8.0 has no way to read it from its newest entries, so the tab
 * reads it from the start, one page after another, never two at once, giving the browser a turn
 * between pages. Until it reaches the end it says how far it has read and
 * shows no entry, since a journal read in part would be shown as the whole of
 * it. It keeps at most `JOURNAL_LIMITS.keep` entries, dropping the oldest and
 * counting them, and the page renders the newest `window`, in steps. A reading
 * that fails stops where it is, and continues from that cursor when asked.
 *
 * **Reading from the newest entries (Runner v0.8.0, #235).** Where Runner lists
 * `journal-backward` in `GET /v1/status`, the first reading is one request for
 * the newest page (`tail=true`); older pages follow on request, one at a time
 * (`before=<sequence>`), only as far as the viewer asks; and the cursor `next`
 * of the newest page follows the journal forward from there. Pages come in the
 * journal's order. Each is held to what Runner returned for the one before
 * (`journalBegan`, `more`, `before`, `next`, and the sequences of its entries)
 * before it is merged; a page that does not fit is refused, said, and not
 * merged. Where Runner lists no such capability, the forward reading above is
 * unchanged. Nothing from before what has been read is claimed to be absent:
 * the tab says what it shows and what it has not yet read.
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
export const JOURNAL_SOURCE = 'v0.8.0'
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
export interface JournalSnapshot { entries: JournalEntry[]; dropped: number; began: unknown; places?: number[] }
/**
 * Set when the journal is read from its newest entries: whether older entries
 * are not yet read, and whether a request for them is under way.
 */
export interface BackwardReading { more: boolean; loadingOlder: boolean }
export type JournalState =
  /** Before Runner first answered. */
  | { phase: 'starting' }
  /** The route answered 404 to the first request: this runner serves no journal for the job. */
  | { phase: 'absent' }
  /** Catching up, read to the end, or stopped by a failure; `read` and `next` are the reading's own. */
  | { phase: 'reading' | 'caught-up' | 'stopped'; read: number; next: number; snapshot?: JournalSnapshot; reason?: string; backward?: BackwardReading }

/** The capability Runner lists in `GET /v1/status` when its journal routes accept `tail` and `before` (Runner v0.8.0). */
export const JOURNAL_BACKWARD = 'journal-backward'
let detected: Promise<boolean> | undefined
/** Forgets what `GET /v1/status` said, so that the next reading asks again: after a failure, or a Runner that may have started anew. */
export function forgetRunnerCapabilities(): void { detected = undefined }
/**
 * Whether Runner lists `journal-backward`, asked of `GET /v1/status` once and
 * kept until a reading fails (`forgetRunnerCapabilities`). A request that
 * fails is not an answer: it rejects, and is asked again, never read as a
 * Runner without the capability. An answer that lists none, or no list, is a
 * Runner that is read forward, as every Runner can be.
 */
export function runnerReadsJournalBackward(): Promise<boolean> {
  if (!detected) {
    const asked = jobsAPI<{ capabilities?: unknown }>('status').then(answer => Array.isArray(answer?.capabilities) && answer.capabilities.includes(JOURNAL_BACKWARD))
    detected = asked
    asked.catch(() => { if (detected === asked) detected = undefined })
  }
  return detected
}

/**
 * What has been read of a job's journal from its newest entries, as Runner
 * returned it: the entries kept in journal order, each with its sequence
 * (`places`, ascending); when the journal began; whether entries older than the
 * first kept one exist (`older.more`) and the cursor for them (`older.before`,
 * the sequence of the first kept entry); and the cursor to follow the journal
 * forward from (`next`). `read` is whether the newest page has been read.
 */
export interface Held { entries: JournalEntry[]; places: number[]; began: string | undefined; older: { more: boolean; before: number }; next: number; read: boolean }
export const emptyHeld = (): Held => ({ entries: [], places: [], began: undefined, older: { more: false, before: 0 }, next: 0, read: false })

/** A page of Runner's that does not fit what was already read. It is said and never merged. */
export class PageRefused extends Error {
  constructor() { super(msg('The runner’s journal page did not fit the entries already read, so it was not used.')); this.name = 'PageRefused' }
}

/** Which page a request asked for: the newest, the one before `before`, or the one after `after`. */
export type Way = { kind: 'tail' } | { kind: 'older'; before: number } | { kind: 'forward'; after: number }
export interface HeldPage { began: string; items: JournalEntry[]; places: number[]; next: number; more: boolean; before?: number }

const isCursor = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0

/** A page by its form alone: every member Runner promises, entries each with a sequence, ascending. */
function formOf(answer: unknown): HeldPage | undefined {
  if (!answer || typeof answer !== 'object' || Array.isArray(answer)) return undefined
  const page = answer as Record<string, unknown>
  if (typeof page.journalBegan !== 'string' || !Array.isArray(page.items) || !isCursor(page.next) || typeof page.more !== 'boolean') return undefined
  if (page.before !== undefined && !isCursor(page.before)) return undefined
  const items: JournalEntry[] = [], places: number[] = []
  for (const item of page.items) {
    if (!item || typeof item !== 'object' || Array.isArray(item)) return undefined
    const sequence = (item as JournalEntry).sequence
    if (typeof sequence !== 'number' || !Number.isSafeInteger(sequence) || sequence < 1 || (places.length > 0 && sequence <= places[places.length - 1]!)) return undefined
    items.push(item as JournalEntry)
    places.push(sequence)
  }
  return { began: page.journalBegan, items, places, next: page.next, more: page.more, ...(page.before === undefined ? {} : { before: page.before }) }
}

/**
 * Holds a page Runner answered to the record of the request and of what was
 * already read, and returns it only if it fits: the same `journalBegan`; for a
 * backward page, `before` the sequence of its first entry (the cursor asked
 * with, for an empty page), all its entries before the cursor asked with, `next`
 * its last, and `more` only where an older entry can exist; for a forward page,
 * no `before`, all its entries after the cursor asked with, and `next` its last
 * (the cursor asked with, for an empty page). Anything else throws `PageRefused`.
 */
export function holdPage(held: Held, way: Way, answer: unknown): HeldPage {
  const page = formOf(answer)
  if (!page || (held.began !== undefined && page.began !== held.began)) throw new PageRefused()
  const first = page.places[0], last = page.places[page.places.length - 1]
  const entries = page.places.length > 0
  let fits: boolean
  switch (way.kind) {
    case 'tail':
      fits = entries ? page.before === first && page.next === last : page.before === 0 && page.next === 0 && !page.more
      break
    case 'older':
      fits = page.before !== undefined && (entries ? page.before === first && page.next === last && last! < way.before : page.before === way.before && page.next === 0 && !page.more)
      break
    case 'forward':
      fits = page.before === undefined && (entries ? first! > way.after && page.next === last : page.next === way.after && !page.more)
      break
  }
  // The sequence of the journal's first entry is 1: nothing can come before it.
  if (way.kind !== 'forward' && page.more && (!entries || first! <= 1)) fits = false
  if (!fits) throw new PageRefused()
  return page
}

/**
 * Merges a page that `holdPage` returned into what is held: the newest page
 * replaces nothing (it is the first), an older page goes before the entries
 * held, a forward page after them, so that the entries stay in journal order,
 * each once. Beyond `keep` entries the oldest are dropped from a forward
 * arrival, and older ones are said to exist again. An older page asked for
 * before a drop moved the cursor is not merged: `false`.
 */
export function mergePage(held: Held, way: Way, page: HeldPage, keep: number): boolean {
  switch (way.kind) {
    case 'tail':
      held.entries = page.items; held.places = page.places; held.began = page.began
      held.older = { more: page.more, before: page.before! }; held.next = page.next; held.read = true
      return true
    case 'older':
      if (!held.older.more || held.older.before !== way.before) return false
      held.entries = [...page.items, ...held.entries]; held.places = [...page.places, ...held.places]
      held.older = { more: page.more, before: page.before! }
      return true
    case 'forward': {
      held.entries = [...held.entries, ...page.items]; held.places = [...held.places, ...page.places]; held.next = page.next
      const excess = held.entries.length - keep
      if (excess > 0) {
        held.entries = held.entries.slice(excess); held.places = held.places.slice(excess)
        held.older = { more: true, before: held.places[0]! }
      }
      return true
    }
  }
}

interface Reading {
  jobId: string
  mode?: 'forward' | 'backward'
  journal: Journal
  backward: Held
  snapshot?: JournalSnapshot
  loadingOlder: boolean
  catchingUp: boolean
  /** How many entries a viewer asked to have held, at least. */
  target: number
  stopped?: string
}

/** The state the page shows for what a reading holds. */
function describe(reading: Reading, phase?: 'reading' | 'caught-up' | 'stopped', reason?: string): JournalState {
  if (reading.mode === 'backward') {
    const held = reading.backward
    const stoppedFor = reason ?? reading.stopped
    return {
      phase: stoppedFor !== undefined ? 'stopped' : reading.loadingOlder || reading.catchingUp ? 'reading' : 'caught-up',
      read: held.entries.length, next: held.next, snapshot: reading.snapshot,
      backward: { more: held.older.more, loadingOlder: reading.loadingOlder },
      ...(stoppedFor === undefined ? {} : { reason: stoppedFor })
    }
  }
  return { phase: phase!, read: readCount(reading.journal), next: reading.journal.next, snapshot: reading.snapshot, ...(reason === undefined ? {} : { reason }) }
}

/**
 * Reads a job's journal for the page. Where Runner lists `journal-backward`,
 * from its newest entries: the newest page once, older pages as `showOlder`
 * asks for them, and the entries after the newest every `JOURNAL_REFRESH_MS`.
 * Otherwise, and for every Runner that does not list it, from the start once,
 * then from its cursor every `JOURNAL_REFRESH_MS` once caught up. `resume`
 * continues a reading that stopped, from its cursor.
 */
export function useJournalReader(jobId: string, keep: number): { state: JournalState; resume: () => void; showOlder: (count: number) => void } {
  const [state, setState] = useState<JournalState>({ phase: 'starting' })
  const [attempt, setAttempt] = useState(0)
  const held = useRef<Reading>(undefined)
  const older = useRef<((count: number) => void) | undefined>(undefined)
  if (held.current?.jobId !== jobId) held.current = { jobId, journal: emptyJournal(), backward: emptyHeld(), loadingOlder: false, catchingUp: false, target: 0 }
  useEffect(() => {
    const reading = held.current!, controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    const journal = reading.journal
    const status = (phase: 'reading' | 'caught-up' | 'stopped', reason?: string) => setState(describe(reading, phase, reason))
    const page = (after: number) => jobsAPI<JournalPage>(`jobs/${jobId}/events?after=${after}`, undefined, undefined, controller.signal)
    async function readForward() {
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
    async function readBackward() {
      const stop = (error: unknown) => {
        if (controller.signal.aborted) return
        if (error instanceof JobsRequestError && error.status === 404 && !reading.backward.read) { setState({ phase: 'absent' }); controller.abort(); return }
        reading.stopped = error instanceof Error ? error.message : msg('The local runner could not complete this request.')
        reading.loadingOlder = false; reading.catchingUp = false
        setState(describe(reading))
        controller.abort()
      }
      // One page, asked for, held to the record and merged; a snapshot of what changed.
      const take = async (way: Way, path: string): Promise<HeldPage> => {
        const answer = await jobsAPI<unknown>(path, undefined, undefined, controller.signal)
        if (controller.signal.aborted) throw controller.signal.reason
        const checked = holdPage(reading.backward, way, answer)
        if (mergePage(reading.backward, way, checked, keep)) {
          // A snapshot of what changed. The arrays are replaced by every merge, never changed in place.
          reading.snapshot = { entries: reading.backward.entries, dropped: 0, began: reading.backward.began, places: reading.backward.places }
        }
        return checked
      }
      reading.stopped = undefined
      // The newest page was just read: the next request waits its turn, as a caught-up reading does.
      let waits = false
      if (!reading.backward.read) {
        waits = true
        try { await take({ kind: 'tail' }, `jobs/${jobId}/events?tail=true`) } catch (error) { stop(error); return }
        if (controller.signal.aborted) return
        setState(describe(reading))
      }
      const readOlder = async () => {
        if (reading.loadingOlder || controller.signal.aborted) return
        reading.loadingOlder = true
        setState(describe(reading))
        try {
          while (reading.backward.older.more && reading.backward.entries.length < Math.min(reading.target, keep)) {
            const before = reading.backward.older.before
            await take({ kind: 'older', before }, `jobs/${jobId}/events?before=${before}`)
            setState(describe(reading))
            await yieldToBrowser()
            if (controller.signal.aborted) return
          }
        } catch (error) { stop(error); return }
        reading.loadingOlder = false
        if (!controller.signal.aborted) setState(describe(reading))
      }
      older.current = count => { reading.target = Math.max(reading.target, reading.backward.entries.length + count); void readOlder() }
      if (reading.target > reading.backward.entries.length) void readOlder()
      for (;;) {
        if (waits) {
          await new Promise<void>(resolve => { timer = setTimeout(resolve, JOURNAL_REFRESH_MS) })
          if (controller.signal.aborted) return
        }
        waits = true
        try {
          for (;;) {
            const after = reading.backward.next
            const answered = await take({ kind: 'forward', after }, `jobs/${jobId}/events?after=${after}`)
            if (!answered.more) break
            reading.catchingUp = true
            setState(describe(reading))
            await yieldToBrowser()
            if (controller.signal.aborted) return
          }
        } catch (error) { stop(error); return }
        reading.catchingUp = false
        if (controller.signal.aborted) return
        setState(describe(reading))
      }
    }
    async function read() {
      let backward: boolean
      try { backward = await runnerReadsJournalBackward() } catch (error) {
        if (controller.signal.aborted) return
        reading.stopped = error instanceof Error ? error.message : msg('The local runner could not complete this request.')
        setState(describe(reading, 'stopped', reading.stopped))
        return
      }
      if (controller.signal.aborted) return
      const mode = backward ? 'backward' : 'forward'
      if (reading.mode && reading.mode !== mode) {
        // Another Runner answered than the one read before: nothing it read is carried over.
        Object.assign(reading.journal, emptyJournal())
        reading.backward = emptyHeld(); reading.snapshot = undefined; reading.target = 0
      }
      reading.mode = mode
      reading.stopped = undefined
      if (backward) await readBackward(); else await readForward()
    }
    void read()
    return () => { controller.abort(); clearTimeout(timer); older.current = undefined }
  }, [jobId, keep, attempt])
  return {
    state,
    resume: () => {
      const reading = held.current!
      forgetRunnerCapabilities()
      reading.stopped = undefined
      setState(reading.mode ? describe(reading, 'reading') : { phase: 'starting' })
      setAttempt(n => n + 1)
    },
    showOlder: (count: number) => older.current?.(count)
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
