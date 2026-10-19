import {LoadingState} from '../ui/LoadingState'
import { useCollectionSelection } from '../ui/useCollectionSelection'
import { SelectionTable, SelectionHeader, SelectionCell, SelectionActions } from '../ui/SelectionTable'
import { exportSelectedRows } from '../ui/selectionExport'
import { CollectionRow } from '../ui/CollectionRow'
import { useId } from 'react'
import { VisuallyHidden } from 'radix-ui'
import { Link } from 'react-router-dom'
import { msg, useLocale } from '../i18n'
import { CollectionLayout } from '../ui/CollectionLayout'
import { Pagination } from '../ui/Pagination'
import { usePagination } from '../ui/usePagination'
import type { DialectEvaluation, Preparation } from './client'
import {reviewDecisionLabel} from './recordReview'
import type {ResearchSummary} from './research'
import {researchStateLabel} from './ResearchActivity'
import styles from './DialectsView.module.css'

export function DialectActivity({ document, batches, loading = false, error, research = [], onResearch }: {
  research?: ResearchSummary[]
  onResearch?: (id:string,opener:HTMLButtonElement)=>void
  document: Preparation
  batches: DialectEvaluation[]
  loading?: boolean
  error?: Error | null
}) {
  const locale = useLocale(), descriptionId = useId()
  const events = [
    ...document.activity.map((event, index) => ({
      id: 'preparation:' + index,
      at: event.at,
      recipeVersion: event.recipeVersion,
      label: event.kind === 'created' ? msg('Input created') : event.kind === 'recipe-updated'
        ? msg('Recipe updated') : event.kind === 'researched' ? msg('Research records saved: {{count}}', {count:event.count??0}) : msg('Records imported: {{count}}', { count: event.count ?? 0 }),
      researchId: undefined as string | undefined,
      jobId: undefined as string | undefined
    })).reverse(),
    ...(document.reviews??[]).map((event,index)=>({id:'review:'+index,at:event.review.at,recipeVersion:event.review.recipeVersion,label:event.record.name+' · '+reviewDecisionLabel(event.review.decision),researchId:undefined as string|undefined,jobId:undefined as string|undefined,review:event.review,record:event.record})),
    ...batches.filter(batch => batch.confirmedAt).map(batch => ({
      id: 'evaluation:' + batch.id,
      at: batch.confirmedAt!,
      recipeVersion: batch.recipeVersion,
      label: msg('Evaluation · {{count}} records', { count: batch.rows.length }),
      researchId: undefined as string | undefined,
      jobId: batch.jobId
    })),
    ...research.filter(run=>run.state!=='review').map(run=>({id:'research:'+run.id,at:run.createdAt??run.updatedAt,recipeVersion:run.recipeVersion,label:researchStateLabel(run.state),researchId:run.id,jobId:undefined as string|undefined}))
  ].sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
  const selection = useCollectionSelection(events, event => event.id, document.id)
  const pagination = usePagination(events.length, document.id)
  return <CollectionLayout label={msg('Activity')} scrollKey={JSON.stringify([pagination.page,pagination.size,document.id])} footer={<Pagination pagination={pagination} complete={!loading && !error}/>} toolbar={<>
      <div className={`${styles.toolbar} ${styles.activityToolbar}`}>
      <p id={descriptionId} className={styles.note}>{msg('Local preparation activity. This is not a signed audit trail.')}</p><SelectionActions selection={selection} onExport={()=>exportSelectedRows('input-activity',selection.selectedRows)}/></div>
      {loading && <LoadingState>{msg('Loading…')}</LoadingState>}
    </>}>
      <SelectionTable selection={selection} className={styles.table} aria-label={msg('Activity')} aria-describedby={descriptionId}>
        <thead><tr><SelectionHeader selection={selection} complete={!loading&&!error}/><th scope="col">{msg('Event')}</th><th scope="col">{msg('Recorded')}</th><th scope="col">{msg('Recipe version')}</th><th scope="col"><VisuallyHidden.Root>{msg('Actions')}</VisuallyHidden.Root></th></tr></thead>
        <tbody>{events.slice(pagination.start, pagination.end).map(event => <CollectionRow key={event.id} actionable={Boolean(event.jobId||event.researchId)} data-selection-selected={selection.has(event.id)||undefined}><SelectionCell selection={selection} id={event.id} name={event.label}/>
          <td>{event.researchId&&onResearch?<button data-row-primary className={styles.record} onClick={e=>onResearch(event.researchId!,e.currentTarget)}>{event.label}</button>:event.label}</td>
          <td><time dateTime={event.at}>{new Date(event.at).toLocaleString(locale)}</time></td>
          <td>{event.recipeVersion??'—'}</td>
          <td className={styles.activityAction}>{event.jobId && <Link data-row-primary className={styles.itemLink} to={'/jobs/' + event.jobId}>{msg('View job')}</Link>}</td>
        </CollectionRow>)}</tbody>
      </SelectionTable>
  </CollectionLayout>
}
