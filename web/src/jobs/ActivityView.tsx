/**
 * A job's Activity tab (#213): one table, newest first, of what Runner
 * recorded about the job: the entries of Runner's journal of job activity,
 * where Runner serves it (#218), and the records it serves.
 *
 * A journal row is an entry, as Runner wrote it: its kind, the time Runner
 * wrote it, and who Runner says initiated it (`journal.ts`). Desk derives no
 * entry from records it observed. Where Runner answers the journal's route
 * with 404, the tab shows the records alone and says so.
 *
 * A record row is a run, a trigger occurrence, or a source preparation
 * (see `activity.ts` for how rows are made, ordered and paged). It says when
 * Runner recorded it, with each time named; what it is; whether this
 * installation or a trigger initiated it; which fixed release it belongs to;
 * and, for a run, what the record holds, each item present or absent. Runner's
 * lists leave out a run's inputs and record bytes, so a run row reads its own
 * record for that column.
 *
 * Nothing in this tab checks anything, and it says no more than that the
 * runner holds an item. No brief is in it.
 */
import { useState, type ReactNode } from 'react'
import { useInfiniteQuery, useQueries, useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { language, msg, useLocale } from '../i18n'
import { useDetailsPortal, useDetailsSlot } from '../shell/DetailsSlot'
import { Button } from '../ui/Button'
import { Disclosure } from '../ui/Disclosure'
import { Select } from '../ui/Select'
import { jobsAPI, type Page, type Release, type Run } from './client'
import { evidenceOf, firstLine, keeps, mergeActivity, MANUAL, stamp, statesFor, streamsFor, type ActivityFilter, type ActivityRow, type JournalRow, type KindFilter } from './activity'
import { beforeTheJournal, JOURNAL_REFRESH_MS, kindText, readJobJournal, type JournalEntry, type JournalReading } from './journal'
import { decisionText, RecordJSON, ReleaseName, requesterText, RunDisclosures, RunFields, RunTechnicalDetails, stampLabel, useJobRecord, useJobTriggers, when } from './RunRecord'
import { occurrenceState } from './SourcePreparations'
import { reason } from './TriggersView'
import type { Occurrence, Trigger } from './triggerTypes'
import styles from './JobsView.module.css'
import own from './ActivityView.module.css'

function stateName(state: string): string {
  return ({
    queued: msg('Queued'), running: msg('Running'), completed: msg('Completed'), failed: msg('Failed'), interrupted: msg('Interrupted'),
    received: msg('Received'), skipped: msg('Skipped'), expired: msg('Expired'), cancelled: msg('Canceled'),
    waiting: msg('Waiting for sources'), 'needs-attention': msg('Needs attention')
  } as Record<string, string>)[state] ?? state
}

/** What the record is, in the row's own words. */
export function whatText(row: ActivityRow): string {
  if (row.run) {
    return row.state === 'queued' ? msg('Run queued')
      : row.state === 'running' ? msg('Run started')
        : row.state === 'completed' ? msg('Run completed: {{decision}}', { decision: decisionText(row.run.result) })
          : row.state === 'failed' ? msg('Run failed')
            : msg('Run interrupted')
  }
  if (row.kind === 'preparation') return row.state === 'waiting' ? msg('Source preparation waiting for sources') : msg('Source preparation needs attention')
  return row.state === 'skipped' ? msg('Occurrence skipped')
    : row.state === 'expired' ? msg('Occurrence expired')
      : row.state === 'failed' ? msg('Occurrence failed')
        : row.state === 'cancelled' ? msg('Occurrence canceled')
          : msg('Occurrence received')
}

/** The line under it: a failed run's problem, an occurrence's reason or progress. */
function whatDetail(row: ActivityRow): string | undefined {
  if (row.run) return row.run.problem ? firstLine(row.run.problem) : undefined
  const o = row.occurrence!
  if (row.kind === 'occurrence' && row.state === 'received') return occurrenceState(o.state)
  return o.reason ? firstLine(reason(o.reason)) : undefined
}

/** When Runner wrote an entry; "Not recorded" where it wrote no time Desk can read, as the run page says (#223). */
function Recorded({ at }: { at: unknown }) {
  const stored = typeof at === 'string' ? stamp('recorded', at) : undefined
  return stored ? <time dateTime={stored.at}>{when(stored.at)}</time> : <>{msg('Not recorded')}</>
}

/** Who Runner says initiated an entry (`JournalActor`): never a person. */
export function actorText(by: unknown, triggers?: Trigger[]): string {
  if (!by || typeof by !== 'object') return msg('Not recorded')
  const actor = by as Record<string, unknown>
  const named = (id: string) => triggers?.find(trigger => trigger.id === id)?.config.name ?? msg('Trigger {{id}}', { id: id.slice(-8) })
  switch (actor.kind) {
    case 'installation': return msg('This installation')
    case 'trigger':
      return typeof actor.trigger === 'string' ? requesterText({ kind: 'trigger', triggerId: actor.trigger, revision: typeof actor.revision === 'number' ? actor.revision : undefined }, triggers) : msg('Not recorded')
    case 'trigger-credential':
      if (typeof actor.trigger !== 'string') return msg('Not recorded')
      // keyRevision is the trigger revision the key was issued at, null for a key issued before the journal began.
      return typeof actor.keyRevision === 'number' ? msg('Event token of {{name}}, issued at trigger revision {{revision}}', { name: named(actor.trigger), revision: actor.keyRevision })
        : actor.keyRevision === null ? msg('Event token of {{name}}, issued before the journal began', { name: named(actor.trigger) }) : msg('Not recorded')
    case 'cloud-connection': return typeof actor.connection === 'string' ? msg('Cloud connection {{connection}}', { connection: actor.connection }) : msg('Not recorded')
    case 'runner': return msg('The runner itself')
    // A kind of initiator a later Runner names: shown as Runner wrote it.
    default: return typeof actor.kind === 'string' ? actor.kind : msg('Not recorded')
  }
}

/** The lines under an entry's kind: the trigger's revision, a refusal's answer, the reason Runner recorded. */
function entryDetails(entry: JournalEntry, triggers?: Trigger[]): ReactNode[] {
  const lines: ReactNode[] = []
  const concerns = entry.concerns && typeof entry.concerns === 'object' ? entry.concerns : {}
  if (typeof entry.kind === 'string' && entry.kind.startsWith('trigger.') && typeof concerns.trigger === 'string') {
    lines.push(requesterText({ kind: 'trigger', triggerId: concerns.trigger, revision: typeof entry.revision === 'number' ? entry.revision : undefined }, triggers))
  }
  const answered = [entry.request, entry.status, entry.code].filter(value => typeof value === 'string' || typeof value === 'number')
  if (entry.kind === 'admission.refused' && answered.length > 0) lines.push(<code>{answered.join(' · ')}</code>)
  if (typeof entry.reason === 'string' && entry.reason) lines.push(firstLine(reason(entry.reason)))
  return lines
}
const runOf = (entry: JournalEntry) => typeof entry.concerns?.run === 'string' && entry.concerns.run.startsWith('run_') ? entry.concerns.run : undefined
const releaseOf = (entry: JournalEntry) => typeof entry.concerns?.release === 'string' ? entry.concerns.release : undefined

function Stamps({ row }: { row: ActivityRow }) {
  return <div className={own.stamps}>{[row.when, ...row.also].map(stamp => <div key={stamp.name}><span className={own.stampName}>{stampLabel(stamp.name)}</span> <time dateTime={stamp.at}>{when(stamp.at)}</time></div>)}</div>
}

const listed = (items: string[]) => new Intl.ListFormat(language(), { type: 'unit', style: 'short' }).format(items)
function EvidenceCell({ record }: { record?: UseQueryResult<Run> }) {
  if (!record || record.isPending) return <span className="quiet">{msg('Loading…')}</span>
  if (!record.data) return <span className={styles.problem}>{msg('The runner did not return this run.')}</span>
  const held = evidenceOf(record.data)
  const items: [string, boolean][] = [[msg('Retained inputs'), held.inputs], [msg('Exact record bytes'), held.recordBytes], [msg('Signature sidecar'), held.sidecar], [msg('Acquisition receipts'), held.receipts]]
  const present = items.filter(([, has]) => has).map(([name]) => name), absent = items.filter(([, has]) => !has).map(([name]) => name)
  return <div className={own.evidence}>
    {present.length > 0 && <span>{msg('Present: {{items}}', { items: listed(present) })}</span>}
    {absent.length > 0 && <span className={own.absent}>{msg('Absent: {{items}}', { items: listed(absent) })}</span>}
  </div>
}

function OccurrenceDetails({ row, jobId, release, triggers }: { row: ActivityRow; jobId: string; release: Release; triggers?: Trigger[] }) {
  const o = row.occurrence!
  return <>
    <dl className={styles.properties}>
      <div><dt>{msg('Status')}</dt><dd>{occurrenceState(o.state)}</dd></div>
      <div><dt>{msg('Trigger')}</dt><dd>{requesterText(row.by, triggers)}</dd></div>
      {[row.when, ...row.also].map(stamp => <div key={stamp.name}><dt>{stampLabel(stamp.name)}</dt><dd>{when(stamp.at)}</dd></div>)}
      {o.preparation && <div><dt>{msg('Source deadline')}</dt><dd>{when(o.preparation.deadline)}</dd></div>}
      <div><dt>{msg('Release')}</dt><dd><ReleaseName jobId={jobId} releaseId={o.releaseId} release={release} /></dd></div>
      {o.runId && <div><dt>{msg('Run')}</dt><dd><Link to={`/jobs/${jobId}/runs/${o.runId}`}>{msg('Run {{id}}', { id: o.runId.slice(-8) })}</Link></dd></div>}
    </dl>
    {o.reason && <p className={styles.note}>{reason(o.reason)}</p>}
    {o.preparation && <Disclosure title={msg('Sources · {{count}}', { count: o.preparation.tasks.length })}><ul className={styles.sourceSteps}>{o.preparation.tasks.map(task => <li key={task.id}><span>{task.name}</span><span className="quiet">{occurrenceState(task.state)}</span></li>)}</ul></Disclosure>}
    <Disclosure title={msg('Technical details')}>
      <dl className={styles.properties}>
        <div><dt>{msg('Occurrence ID')}</dt><dd><code>{o.id}</code></dd></div>
        <div><dt>{msg('Trigger ID')}</dt><dd><code>{o.triggerId}</code></dd></div>
      </dl>
      <RecordJSON title={msg('Record (JSON)')} value={o} />
    </Disclosure>
  </>
}

function JournalDetails({ row, jobId, release, triggers }: { row: JournalRow; jobId: string; release: Release; triggers?: Trigger[] }) {
  const entry = row.entry, title = kindText(entry.kind), run = runOf(entry), releaseId = releaseOf(entry)
  return <section className={own.pane} aria-label={title}>
    <h2>{title}</h2>
    <dl className={styles.properties}>
      <div><dt>{msg('Source')}</dt><dd>{msg('Journal entry')}</dd></div>
      <div><dt>{stampLabel('recorded')}</dt><dd><Recorded at={entry.at} /></dd></div>
      <div><dt>{msg('By')}</dt><dd>{actorText(entry.by, triggers)}</dd></div>
      {releaseId && <div><dt>{msg('Release')}</dt><dd><ReleaseName jobId={jobId} releaseId={releaseId} release={release} /></dd></div>}
      {run && <div><dt>{msg('Run')}</dt><dd><Link to={`/jobs/${jobId}/runs/${run}`}>{msg('Run {{id}}', { id: run.slice(-8) })}</Link></dd></div>}
    </dl>
    {entryDetails(entry, triggers).map((line, index) => <p key={index} className={styles.note}>{line}</p>)}
    <RecordJSON title={msg('Journal entry (JSON)')} value={entry} />
  </section>
}

function ActivityDetails({ row, record, jobId, release, triggers }: { row: ActivityRow; record?: UseQueryResult<Run>; jobId: string; release: Release; triggers?: Trigger[] }) {
  const title = whatText(row)
  return <section className={own.pane} aria-label={title}>
    <h2>{title}</h2>
    {row.run ? <>
      <div><Link to={`/jobs/${jobId}/runs/${row.run.id}`}>{msg('View run')}</Link></div>
      {!record || record.isPending ? <p role="status">{msg('Loading run…')}</p>
        : record.data ? <><RunFields run={record.data} /><RunDisclosures run={record.data} /><RunTechnicalDetails run={record.data} /></>
          : <p className={styles.problem}>{msg('The runner did not return this run.')}</p>}
    </> : <OccurrenceDetails row={row} jobId={jobId} release={release} triggers={triggers} />}
  </section>
}

export function ActivityView({ jobId, release }: { jobId: string; release: Release }) {
  useLocale()
  const [filter, setFilter] = useState<ActivityFilter>({ kind: 'all', state: 'all', trigger: 'all' })
  const [selected, setSelected] = useState<string>()
  const details = useDetailsSlot()
  const triggers = useJobTriggers(jobId), triggerItems = triggers.data?.items
  const plan = streamsFor(filter)
  const filtered = filter.kind !== 'all' || filter.state !== 'all' || filter.trigger !== 'all'
  const job = useJobRecord(jobId)
  // Runner's journal, read forward from where the last reading ended, never
  // from 0 again; once caught up, asked again no faster than the lists are.
  const client = useQueryClient(), journalKey = ['job-journal', jobId]
  const journal = useQuery({
    queryKey: journalKey,
    queryFn: ({ signal }) => { const prior = client.getQueryData<JournalReading>(journalKey); return readJobJournal(jobId, prior?.served ? prior.journal : undefined, signal) },
    refetchInterval: query => query.state.data?.served === false ? false : JOURNAL_REFRESH_MS,
    structuralSharing: false
  })
  const served = journal.data?.served ? journal.data.journal : undefined
  const runs = useInfiniteQuery({
    queryKey: ['jobs-pages', `jobs/${jobId}/runs`, 'activity', plan.runState ?? 'all'], enabled: plan.runs, initialPageParam: 0,
    queryFn: ({ pageParam }) => jobsAPI<Page<Run>>(`jobs/${jobId}/runs?${plan.runState ? `state=${plan.runState}&` : ''}after=${pageParam}`),
    getNextPageParam: page => page.next || undefined, refetchInterval: 5000
  })
  const occurrences = useInfiniteQuery({
    queryKey: ['job-occurrences', jobId, 'activity'], enabled: plan.occurrences, initialPageParam: 0,
    queryFn: ({ pageParam }) => jobsAPI<Page<Occurrence>>(`jobs/${jobId}/occurrences?after=${pageParam}`),
    getNextPageParam: page => page.next || undefined, refetchInterval: 5000
  })
  // A list being read for the first time could hold a newer row than any
  // shown, so nothing is shown until every list the filter reads has answered.
  const pending = (plan.runs && runs.isPending) || (plan.occurrences && occurrences.isPending) || journal.isPending
  const merged = mergeActivity({
    runs: plan.runs && runs.data ? { records: runs.data.pages.flatMap(page => page.items), more: runs.hasNextPage } : undefined,
    occurrences: plan.occurrences && occurrences.data ? { records: occurrences.data.pages.flatMap(page => page.items), more: occurrences.hasNextPage } : undefined
  }, keeps(filter), served && { entries: served.entries, show: !filtered })
  const rows = pending ? [] : merged.rows, shown = pending ? [] : merged.shown
  const runRows = rows.filter(row => row.run)
  const records = useQueries({ queries: runRows.map(row => ({ queryKey: ['job-activity-run', row.run!.id, row.run!.state], queryFn: () => jobsAPI<Run>(`runs/${row.run!.id}`), staleTime: Infinity })) })
  const record = new Map(runRows.map((row, index) => [row.key, records[index]]))
  const chosen = shown.find(row => row.key === selected)
  const pane = !chosen ? null : chosen.kind === 'journal' ? <JournalDetails key={chosen.key} row={chosen} jobId={jobId} release={release} triggers={triggerItems} />
    : <ActivityDetails key={chosen.key} row={chosen} record={record.get(chosen.key)} jobId={jobId} release={release} triggers={triggerItems} />
  const portal = useDetailsPortal(pane)
  const more = (plan.runs && runs.hasNextPage) || (plan.occurrences && occurrences.hasNextPage)
  const fetching = runs.isFetchingNextPage || occurrences.isFetchingNextPage
  const error = (plan.runs && runs.error) || (plan.occurrences && occurrences.error) || journal.error
  // The next page of the list that holds rows back; the boundary moves only when it is read.
  function loadMore() {
    const lists = { runs: plan.runs && runs.hasNextPage ? runs : undefined, occurrences: plan.occurrences && occurrences.hasNextPage ? occurrences : undefined }
    const next = merged.limiting.filter(name => lists[name])
    for (const name of next.length ? next : (['runs', 'occurrences'] as const)) void lists[name]?.fetchNextPage()
  }
  function choose(kind: KindFilter) { setFilter(current => ({ ...current, kind, state: statesFor(kind).includes(current.state) ? current.state : 'all' })) }
  return <section className={styles.stack} aria-label={msg('Activity')}>
    <p className={styles.note}>{msg('What the local runner recorded about this job, newest first: the entries of its journal of job activity in the order the runner wrote them, and its records, each where the entry that created it stands, or by when the runner first recorded it where the journal has no such entry. Every time shown is one the runner stored, named for what it records. The journal is the runner’s own log, not chained and not signed. Evidence says only whether the runner holds an item; nothing here checks it.')}</p>
    <div className={own.filters}>
      <Select id="activity-kind" aria-label={msg('Kind')} value={filter.kind} options={[{ value: 'all', label: msg('All kinds') }, { value: 'run', label: msg('Runs') }, { value: 'occurrence', label: msg('Trigger occurrences') }, { value: 'preparation', label: msg('Source preparations') }]} onValueChange={value => choose(value as KindFilter)} />
      <Select id="activity-state" aria-label={msg('State')} value={filter.state} options={[{ value: 'all', label: msg('All states') }, ...statesFor(filter.kind).map(value => ({ value, label: stateName(value) }))]} onValueChange={state => setFilter(current => ({ ...current, state }))} />
      <Select id="activity-trigger" aria-label={msg('Trigger')} value={filter.trigger} options={[{ value: 'all', label: msg('All triggers') }, { value: MANUAL, label: msg('Manual / API') }, ...(triggerItems ?? []).map(trigger => ({ value: trigger.id, label: trigger.config.name }))]} onValueChange={trigger => setFilter(current => ({ ...current, trigger }))} />
    </div>
    {error && <p role="alert" className={styles.problem}>{error instanceof Error ? error.message : msg('The local runner could not complete this request.')}</p>}
    {pending && <p role="status">{msg('Loading…')}</p>}
    {journal.data?.served === false && <p className="quiet">{msg('The local runner serves no journal of job activity for this job, so only its records are shown.')}</p>}
    {served && filtered && <p className="quiet">{msg('The runner’s journal is shown when no filter is set.')}</p>}
    {served && !pending && beforeTheJournal(served.began, job.data?.job.createdAt) && <p className={styles.note}>{typeof served.began === 'string' && stamp('recorded', served.began)
      ? msg('No journal before {{time}}: the runner’s journal of job activity began then. Nothing earlier has an entry, and nothing is filled in from its records.', { time: when(served.began) })
      : msg('The runner did not say when its journal began in a time this Desk can read. Nothing before it has an entry.')}</p>}
    {!pending && !error && shown.length === 0 && !more && <p className="quiet">{filtered ? msg('Nothing the runner recorded matches these filters.') : msg('The runner has recorded nothing for this job yet.')}</p>}
    {shown.length > 0 && <div className={styles.tableWrap}><table className={styles.table}>
      <thead><tr><th>{msg('When')}</th><th>{msg('Source')}</th><th>{msg('What')}</th><th>{msg('By')}</th><th>{msg('Release')}</th><th>{msg('Evidence')}</th></tr></thead>
      <tbody>{shown.map(row => {
        const choose = () => { setSelected(row.key); details.reveal() }
        if (row.kind === 'journal') {
          const entry = row.entry, run = runOf(entry), releaseId = releaseOf(entry)
          return <tr key={row.key} data-kind="journal" className={row.key === selected ? own.selected : undefined}>
            <td><div className={own.stamps}><div><span className={own.stampName}>{stampLabel('recorded')}</span> <Recorded at={entry.at} /></div></div></td>
            <td>{msg('Journal entry')}</td>
            <td>
              <button type="button" className={own.select} aria-pressed={row.key === selected} onClick={choose}>{kindText(entry.kind)}</button>
              {entryDetails(entry, triggerItems).map((line, index) => <span key={index} className={styles.cellMeta}>{line}</span>)}
              {run && <Link className={styles.cellMeta} to={`/jobs/${jobId}/runs/${run}`}>{msg('Run {{id}}', { id: run.slice(-8) })}</Link>}
            </td>
            <td>{actorText(entry.by, triggerItems)}</td>
            <td>{releaseId ? <ReleaseName jobId={jobId} releaseId={releaseId} release={release} short /> : '—'}</td>
            <td>—</td>
          </tr>
        }
        const detail = whatDetail(row), runId = row.run?.id ?? row.occurrence?.runId
        return <tr key={row.key} data-kind={row.kind} className={row.key === selected ? own.selected : undefined}>
          <td><Stamps row={row} /></td>
          <td>{msg('Record')}</td>
          <td>
            <button type="button" className={own.select} aria-pressed={row.key === selected} onClick={choose}>{whatText(row)}</button>
            {detail && <span className={styles.cellMeta}>{detail}</span>}
            {runId && <Link className={styles.cellMeta} to={`/jobs/${jobId}/runs/${runId}`}>{msg('Run {{id}}', { id: runId.slice(-8) })}</Link>}
          </td>
          <td>{requesterText(row.by, triggerItems)}</td>
          <td><ReleaseName jobId={jobId} releaseId={row.releaseId} release={release} short /></td>
          <td>{row.run ? <EvidenceCell record={record.get(row.key)} /> : '—'}</td>
        </tr>
      })}</tbody>
    </table></div>}
    {more && <div><Button disabled={fetching} onClick={loadMore}>{msg('Load more')}</Button></div>}
    {!portal && pane}
    {portal}
  </section>
}
