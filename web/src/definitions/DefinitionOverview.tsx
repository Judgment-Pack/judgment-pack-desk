import {LoadingState} from '../ui/LoadingState'
import type { ReactNode } from 'react'
import { msg, useLocale } from '../i18n'
import { Disclosure } from '../ui/Disclosure'
import { ExpandableText } from '../ui/ExpandableText'
import styles from './DefinitionOverview.module.css'

export interface OverviewDetail { label: string; value: ReactNode }
/** One reading structure for saved definitions and drafts. Values describe declarations, never results of a run. */
export function DefinitionOverview({ label, purpose, description, context, resultLabel, result, details, action, loading, unavailable }: {
  label: string; purpose?: string; description?: string; context?: string[]; resultLabel: string;
  result?: ReactNode; details?: OverviewDetail[]; action?: ReactNode; loading?: boolean; unavailable?: ReactNode
}) {
  useLocale()
  return <section className={styles.overview} aria-label={label} aria-busy={loading || undefined}>
    {loading ? <LoadingState>{msg('Loading overview…')}</LoadingState> : unavailable ? <div role="status" className={styles.muted}>{unavailable}</div> : <>
      <div className={styles.purpose}>
        <div className={styles.lead}><ExpandableText text={purpose || msg('No purpose has been described yet.')} label={msg('purpose')}/></div>
        {description && <div className={styles.description}><ExpandableText text={description} label={msg('description')}/></div>}
      </div>
      <section aria-label={resultLabel} className={styles.result}>
        <div className={styles.heading}><h2>{resultLabel}</h2>{action}</div>
        {result}
      </section>
      {(details?.length || context?.length) ? <Disclosure title={msg('Details')}>
        {context?.map((paragraph, index) => <p className={styles.context} key={index}>{paragraph}</p>)}
        {!!details?.length && <dl className={styles.details}>{details.map(detail => <div key={detail.label}><dt>{detail.label}</dt><dd>{detail.value}</dd></div>)}</dl>}
      </Disclosure> : null}
    </>}
  </section>
}

/** Preserve authored text; omit only exact repetitions. */
export function overviewText(...values: unknown[]) {
  return [...new Set(values.filter((value): value is string => typeof value === 'string').map(value => value.trim()).filter(Boolean))]
}
