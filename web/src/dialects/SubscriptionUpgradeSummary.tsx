import {msg,formatDate} from '../i18n'
import type {DialectEvaluation} from './client'
import styles from './DialectsView.module.css'

export function SubscriptionUpgradeSummary({evaluation}:{evaluation:DialectEvaluation}){
 const review=evaluation.subscriptionUpgrade
 if(!review)return null
 return <>
  <dl className={styles.detail}>
   <dt>{msg('Current release')}</dt><dd><code>{review.baseReleaseId}</code></dd>
   <dt>{msg('Proposed release')}</dt><dd><code>{evaluation.releaseId}</code></dd>
   <dt>{msg('Reviewed')}</dt><dd>{review.reviewedAt?<time dateTime={review.reviewedAt}>{formatDate(new Date(review.reviewedAt),{dateStyle:'medium',timeStyle:'long'})}</time>:'—'}</dd>
   <dt>{msg('Records to process')}</dt><dd>{review.mode==='changed'?msg('New and changed records'):msg('New records only')}</dd>
   <dt>{msg('Record origin')}</dt><dd>{review.sourceClass==='generated'?msg('AI research'):msg('Supplied')}</dd>
  </dl>
  <p className={styles.note}>{msg('Keeps prior results and replaces this subscription with a paused revision. Records present at the saved review time are skipped; future eligible records use the new release after you enable it.')}</p>
 </>
}
