/**
 * A job's Activity tab (#213): one table, newest first, of what Runner
 * recorded about the job, built only from the records it already serves.
 *
 * Each row is a record: a run, a trigger occurrence, or a source preparation
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
import { useState } from 'react'
import { useInfiniteQuery, useQueries, type UseQueryResult } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { language, msg, useLocale } from '../i18n'
import { useDetailsPortal, useDetailsSlot } from '../shell/DetailsSlot'
import { Button } from '../ui/Button'
import { Disclosure } from '../ui/Disclosure'
import { Select } from '../ui/Select'
import { jobsAPI, type Page, type Release, type Run } from './client'
import { evidenceOf, firstLine, keeps, mergeActivity, MANUAL, statesFor, streamsFor, type ActivityFilter, type ActivityRow, type KindFilter } from './activity'
import { decisionText, RecordJSON, ReleaseName, requesterText, RunDisclosures, RunFields, RunTechnicalDetails, stampLabel, useJobTriggers, when } from './RunRecord'
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
  const pending = (plan.runs && runs.isPending) || (plan.occurrences && occurrences.isPending)
  const merged = mergeActivity({
    runs: plan.runs && runs.data ? { records: runs.data.pages.flatMap(page => page.items), more: runs.hasNextPage } : undefined,
    occurrences: plan.occurrences && occurrences.data ? { records: occurrences.data.pages.flatMap(page => page.items), more: occurrences.hasNextPage } : undefined
  }, keeps(filter))
  const rows = pending ? [] : merged.rows
  const runRows = rows.filter(row => row.run)
  const records = useQueries({ queries: runRows.map(row => ({ queryKey: ['job-activity-run', row.run!.id, row.run!.state], queryFn: () => jobsAPI<Run>(`runs/${row.run!.id}`), staleTime: Infinity })) })
  const record = new Map(runRows.map((row, index) => [row.key, records[index]]))
  const chosen = rows.find(row => row.key === selected)
  const pane = chosen ? <ActivityDetails key={chosen.key} row={chosen} record={record.get(chosen.key)} jobId={jobId} release={release} triggers={triggerItems} /> : null
  const portal = useDetailsPortal(pane)
  const more = (plan.runs && runs.hasNextPage) || (plan.occurrences && occurrences.hasNextPage)
  const fetching = runs.isFetchingNextPage || occurrences.isFetchingNextPage
  const error = (plan.runs && runs.error) || (plan.occurrences && occurrences.error)
  const filtered = filter.kind !== 'all' || filter.state !== 'all' || filter.trigger !== 'all'
  // The next page of the list that holds rows back; the boundary moves only when it is read.
  function loadMore() {
    const lists = { runs: plan.runs && runs.hasNextPage ? runs : undefined, occurrences: plan.occurrences && occurrences.hasNextPage ? occurrences : undefined }
    const next = merged.limiting.filter(name => lists[name])
    for (const name of next.length ? next : (['runs', 'occurrences'] as const)) void lists[name]?.fetchNextPage()
  }
  function choose(kind: KindFilter) { setFilter(current => ({ ...current, kind, state: statesFor(kind).includes(current.state) ? current.state : 'all' })) }
  return <section className={styles.stack} aria-label={msg('Activity')}>
    <p className={styles.note}>{msg('What the local runner recorded about this job, newest first by when it first recorded each item. Every time shown is one the runner stored, named for what it records. Evidence says only whether the runner holds an item; nothing here checks it.')}</p>
    <div className={own.filters}>
      <Select id="activity-kind" aria-label={msg('Kind')} value={filter.kind} options={[{ value: 'all', label: msg('All kinds') }, { value: 'run', label: msg('Runs') }, { value: 'occurrence', label: msg('Trigger occurrences') }, { value: 'preparation', label: msg('Source preparations') }]} onValueChange={value => choose(value as KindFilter)} />
      <Select id="activity-state" aria-label={msg('State')} value={filter.state} options={[{ value: 'all', label: msg('All states') }, ...statesFor(filter.kind).map(value => ({ value, label: stateName(value) }))]} onValueChange={state => setFilter(current => ({ ...current, state }))} />
      <Select id="activity-trigger" aria-label={msg('Trigger')} value={filter.trigger} options={[{ value: 'all', label: msg('All triggers') }, { value: MANUAL, label: msg('Manual / API') }, ...(triggerItems ?? []).map(trigger => ({ value: trigger.id, label: trigger.config.name }))]} onValueChange={trigger => setFilter(current => ({ ...current, trigger }))} />
    </div>
    {error && <p role="alert" className={styles.problem}>{error instanceof Error ? error.message : msg('The local runner could not complete this request.')}</p>}
    {pending && <p role="status">{msg('Loading…')}</p>}
    {!pending && !error && rows.length === 0 && !more && <p className="quiet">{filtered ? msg('Nothing the runner recorded matches these filters.') : msg('The runner has recorded nothing for this job yet.')}</p>}
    {rows.length > 0 && <div className={styles.tableWrap}><table className={styles.table}>
      <thead><tr><th>{msg('When')}</th><th>{msg('What')}</th><th>{msg('By')}</th><th>{msg('Release')}</th><th>{msg('Evidence')}</th></tr></thead>
      <tbody>{rows.map(row => {
        const detail = whatDetail(row), runId = row.run?.id ?? row.occurrence?.runId
        return <tr key={row.key} data-kind={row.kind} className={row.key === selected ? own.selected : undefined}>
          <td><Stamps row={row} /></td>
          <td>
            <button type="button" className={own.select} aria-pressed={row.key === selected} onClick={() => { setSelected(row.key); details.reveal() }}>{whatText(row)}</button>
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
