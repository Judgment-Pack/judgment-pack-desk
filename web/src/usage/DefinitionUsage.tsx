import { Disclosure } from '../ui/Disclosure'
import { useId, useRef, useState } from 'react'
import { msg, useLocale } from '../i18n'
import { Button, ButtonLink } from '../ui/Button'
import { SegmentedControl } from '../ui/SegmentedControl'
import { Input } from '../ui/Input'
import { Pagination } from '../ui/Pagination'
import { usePagination } from '../ui/usePagination'
import { Tooltip } from '../ui/Tooltip'
import { ErrorBox } from '../components/primitives'
import { IconClose, IconGraph, IconHistory } from '../shell/icons'
import { triggerName } from '../jobs/TriggerForm'
import type { Run } from '../jobs/client'
import { usePackUsage } from './usePackUsage'
import type { UsageRow } from './model'
import styles from './DefinitionUsage.module.css'

export type UsageSubject = { kind: 'pack'; configuredKey?: string; documentId?: string; draft?: boolean } | { kind: 'graph' }

export function DefinitionUsage(props: UsageSubject) {
  useLocale()
  return props.kind === 'graph' ? <section className={styles.unavailable} aria-label={msg('Usage')}>
    <h2>{msg('Jobs')}</h2>
    <p>{msg('Jobs currently run saved pack releases. Running a graph as a job is not supported yet.')}</p>
    <p>{msg('Use Tests to check this graph. Job history and schedules remain in Jobs.')}</p>
    <ButtonLink variant="quiet" to="/jobs">{msg('Browse jobs')}</ButtonLink>
  </section> : <PackUsage key={JSON.stringify([props.configuredKey, props.documentId])} {...props}/>
}

function PackUsage({ configuredKey, documentId, draft }: Extract<UsageSubject, { kind: 'pack' }>) {
  const data = usePackUsage(configuredKey, documentId)
  const [filter, setFilter] = useState('all'), [search, setSearch] = useState(''), [selected, setSelected] = useState<string>()
  const opener = useRef<HTMLButtonElement | null>(null), detailsId = useId()
  const rows = data.rows.filter(row => (filter === 'all' || row.kind === filter) && row.name.toLocaleLowerCase().includes(search.toLocaleLowerCase()))
  const pagination = usePagination(rows.length, JSON.stringify([filter, search, configuredKey, documentId]))
  const visible = rows.slice(pagination.start, pagination.end)
  const detail = visible.find(row => row.id === selected)
  const graphPending = data.status === 'ready' && (data.packs.isPending || (data.supported && (data.inventory.isPending || (data.references.length > 0 && data.inventoryKnown && !data.inventory.error && !data.packs.error && data.graphReads.isPending))))
  const graphsComplete = data.status === 'ready' && !graphPending && data.inventoryKnown && data.supported && !data.packs.error && !data.inventory.error && !data.graphReads.error
    && !data.graphReads.data?.unread && !!data.chats.store && data.chats.ready && !data.chats.error && !data.unreadDrafts
  const jobsPending = !!documentId && data.status === 'ready' && data.jobs.isPending
  const jobsComplete = data.status === 'ready' && !!documentId && !jobsPending && !data.jobs.error && !data.unreadJobs && !data.jobs.hasNextPage
  const complete = (filter === 'job' || graphsComplete) && (filter === 'graph' || jobsComplete)
  const pending = (filter !== 'job' && graphPending) || (filter !== 'graph' && jobsPending)
  const close = () => { setSelected(undefined); opener.current?.focus() }
  return <section className={styles.root} aria-label={msg('Usage')}>
    <div className={styles.toolbar}>
      <SegmentedControl label={msg('Usage type')} value={filter} onValueChange={setFilter} segments={[
        { value: 'all', label: msg('All') }, { value: 'graph', label: msg('Graphs') }, { value: 'job', label: msg('Jobs') }
      ]}/>
      <Input aria-label={msg('Search usage')} placeholder={msg('Search usage…')} value={search} onChange={event => setSearch(event.target.value)}/>
    </div>
    <div className={styles.list}>
      <p className={styles.note}>{draft ? msg('References to saved versions of this pack. Graphs and job releases do not use this draft.') : msg('Graphs reference the saved pack. Jobs keep a frozen release and do not follow edits.')}</p>
      {!complete && !pending && <div role="status">
        <p className={styles.note}>{msg('Usage is incomplete. Only confirmed references are shown.')}</p>
        <Disclosure title={msg('Availability details')}>
      {data.status !== 'ready' && <p role="status" className={styles.note}>{msg('Connect to the runtime to load current usage.')}</p>}
      {filter !== 'job' && <>
        {!data.supported && <p role="status" className={styles.note}>{msg('Graph usage is unavailable with this runtime.')}</p>}
        {data.packs.error && <ErrorBox title={msg('Could not read pack references')} error={data.packs.error}/>}
        {data.supported && data.inventory.data && !data.inventoryKnown && <p role="status" className={styles.note}>{msg('Graph inventory is incomplete. Usage cannot be confirmed.')}</p>}
        {data.inventory.error && <ErrorBox title={msg('Could not list graphs')} error={data.inventory.error}/>}
        {data.graphReads.error && <ErrorBox title={msg('Could not read graph references')} error={data.graphReads.error}/>}
        {!!data.graphReads.data?.unread && <p role="status" className={styles.note}>{msg('{{count}} graphs could not be read. Usage is incomplete.', { count: data.graphReads.data.unread })}</p>}
        {(!data.chats.store || !data.chats.ready || !!data.chats.error || data.unreadDrafts > 0) && <p role="status" className={styles.note}>{msg('Some saved graph drafts could not be read yet. Their references are not included.')}</p>}
        {!graphPending && !data.packs.error && !data.references.length && <p className={styles.note}>{msg('No configured pack reference is linked to this document yet.')}</p>}
      </>}
      {filter !== 'graph' && <>
        {!documentId && <p className={styles.note}>{msg('A pack document ID is needed to find its job releases.')}</p>}
        {data.jobs.error && <><ErrorBox title={msg('Could not read job usage')} error={data.jobs.error}/><Button variant="quiet" onClick={() => void data.jobs.refetch()} disabled={data.jobs.isFetching}>{msg('Retry')}</Button></>}
        {data.unreadJobs > 0 && <p role="status" className={styles.note}>{msg('{{count}} job releases could not be read. Usage is incomplete.', { count: data.unreadJobs })}</p>}

      </>}
        </Disclosure>
      </div>}
      {data.jobs.hasNextPage && <div className={styles.more}><p className={styles.note}>{msg('Matches from {{count}} loaded jobs. More jobs remain to be checked.', { count: data.scannedJobs })}</p><Button disabled={data.jobs.isFetching} onClick={() => void data.jobs.fetchNextPage()}>{data.jobs.isFetchingNextPage ? msg('Loading…') : msg('Load more jobs')}</Button></div>}
      {pending && <p role="status" className={styles.note}>{msg('Loading usage…')}</p>}
      {!pending && !rows.length && <p className={styles.empty} role="status">{complete ? search ? msg('No matching references.') : msg('No usage found.') : msg('No references found in the loaded data.')}</p>}
      {rows.length > 0 && <div className={styles.tableScroll} tabIndex={0} role="region" aria-label={msg('Usage references')}><table className={styles.table}>
        <thead><tr><th scope="col">{msg('Name')}</th><th scope="col">{msg('Pack version')}</th><th scope="col">{msg('Status')}</th></tr></thead>
        <tbody>{visible.map(row => <tr key={row.id} data-selected={selected === row.id || undefined}>
          <td><Button variant="quiet" className={styles.name} aria-expanded={detail?.id === row.id} aria-controls={detailsId} onClick={event => { opener.current = event.currentTarget; setSelected(row.id) }}>{row.kind === 'graph' ? <IconGraph/> : <IconHistory/>}{row.name}</Button>
            <span className={styles.meta}>{row.kind === 'graph' ? row.draft ? msg('Draft graph') : msg('Graph') : triggerLabel(row)}</span></td>
          <td>{row.versions.join(', ') || '—'}<span className={styles.meta}>{row.kind === 'job' ? msg('Frozen release') : msg('Saved pack')}</span></td>
          <td>{row.kind === 'graph' ? row.draft ? msg('Draft reference') : msg('Configured reference') : latestRun(row) ? runState(latestRun(row)!.state) : msg('No runs yet')}</td>
        </tr>)}</tbody>
      </table></div>}
    </div>
    <Pagination pagination={pagination} complete={complete}/>
    <section id={detailsId} hidden={!detail} className={styles.details} aria-label={msg('Usage details')}>
      {detail && <><header><h2>{detail.name}</h2><ButtonLink to={detail.href}>{detail.kind === 'graph' ? msg('Open graph') : msg('Open job')}</ButtonLink>
        <Tooltip content={msg('Close details')}><Button variant="quiet" size="icon" aria-label={msg('Close details')} onClick={close}><IconClose/></Button></Tooltip></header>
        <div className={styles.detailBody}>
          {detail.kind === 'graph' ? <><p className={styles.note}>{detail.draft ? msg('This draft declares a reference to a saved pack. It has not been run here.') : msg('These nodes reference the configured pack. This view does not run the graph.')}</p><dl><dt>{msg('Pack reference')}</dt><dd>{detail.packKeys?.join(', ')}</dd><dt>{msg('Nodes')}</dt><dd>{detail.nodes?.join(', ')}</dd><dt>{msg('Graph version')}</dt><dd>{detail.graphVersion || '—'}</dd></dl></>
          : <><p className={styles.note}>{msg('This job uses its frozen release, not the current draft or unsaved edits. Open the job for schedules and run history.')}</p><dl><dt>{msg('Pack version')}</dt><dd>{detail.release?.packVersion}</dd><dt>{msg('Release')}</dt><dd>{detail.release?.id}</dd><dt>{msg('Pack digest')}</dt><dd><code>{detail.release?.packDigest}</code></dd><dt>{msg('Trigger')}</dt><dd>{triggerLabel(detail)}</dd><dt>{msg('Latest run')}</dt><dd>{latestRun(detail) ? <ButtonLink variant="inline" to={`${detail.href}/runs/${encodeURIComponent(latestRun(detail)!.id)}`}>{runState(latestRun(detail)!.state)} · {new Date(latestRun(detail)!.createdAt).toLocaleString()}</ButtonLink> : msg('No runs yet')}</dd></dl></>}
        </div></>}
    </section>
  </section>
}
function latestRun(row: UsageRow) { return row.job?.recentRuns?.reduce<Pick<Run, 'id' | 'state' | 'createdAt'> | undefined>((latest, run) => !latest || run.createdAt > latest.createdAt ? run : latest, undefined) }
function runState(state: Run['state']) { return { queued: msg('Queued'), running: msg('Running'), completed: msg('Completed'), failed: msg('Failed'), interrupted: msg('Interrupted') }[state] }
function triggerLabel(row: UsageRow) {
  return row.job?.triggers?.length ? row.job.triggers.map(trigger => `${triggerName(trigger.kind)} · ${trigger.paused ? msg('Paused') : trigger.kind === 'schedule' && !trigger.nextAt ? msg('Finished') : msg('Active')}`).join(', ') : msg('Manual / API')
}
