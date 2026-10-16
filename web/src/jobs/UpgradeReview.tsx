import {msg} from '../i18n'
import {Disclosure} from '../ui/Disclosure'
import {CodeDiff} from '../ui/CodeDiff'
import {useDialectSubscriptions} from '../dialects/DialectSubscriptions'
import {QueryStatusNotice} from '../shell/QueryStatusNotice'
import {retainedJobMember,type Release} from './client'
import type {JobUpgrade} from './upgrade'
import styles from './JobsView.module.css'

/** Compare with the immutable base, never the editable pack view. */
export function UpgradeReview({base,candidate}:{base:JobUpgrade;candidate:Release}) {
 const subscriptions=useDialectSubscriptions()
 const rows=subscriptions.data?.items.filter(row=>row.jobId===base.job.id&&row.subscription)??[]
 const text=(release:Release,key:string)=>retainedJobMember(release,[key])??'null'
 const comparisons=[
  {label:msg('Pack'),before:base.release.pack,after:candidate.pack},
  {label:msg('Dialect'),before:text(base.release,'inputMapping'),after:text(candidate,'inputMapping')},
  {label:msg('Source profiles'),before:text(base.release,'inputProfiles'),after:text(candidate,'inputProfiles')},
 ]
 return <section className={styles.stack} aria-label={msg('Review job upgrade')}>
  <h2>{msg('Review job upgrade')}</h2>
  <dl className={styles.properties}>
   <div><dt>{msg('Revision')}</dt><dd>{base.job.revision} → {base.job.revision+1}</dd></div>
   <div><dt>{msg('Pack')}</dt><dd>{base.release.packId} · {base.release.packVersion} → {candidate.packVersion}</dd></div>
   <div><dt>{msg('Pack digest')}</dt><dd><code>{base.release.packDigest}</code> → <code>{candidate.packDigest}</code></dd></div>
   <div><dt>{msg('Dialect')}</dt><dd>{base.release.dialect?`${base.release.dialect.id} · ${base.release.dialect.version}`:msg('Not supplied')} → {candidate.dialect?`${candidate.dialect.id} · ${candidate.dialect.version}`:msg('Not supplied')}</dd></div>
   <div><dt>{msg('Published dialect digest')}</dt><dd><code>{base.release.dialect?.digest??'—'}</code> → <code>{candidate.dialect?.digest??'—'}</code></dd></div>
  </dl>
  {comparisons.map(item=><Disclosure key={item.label} title={item.label+' · '+(item.before===item.after?msg('No changes.'):msg('Review proposed changes'))}><CodeDiff before={item.before} after={item.after}/></Disclosure>)}
  <p className={styles.note}>{msg('Existing runs keep their release. Triggers remain paused. Connected Dialects keep their previous release and require a separate subscription review before delivery can resume.')}</p>
  <QueryStatusNotice id={'upgrade-subscriptions-'+base.job.id} label={msg('Dialects subscriptions unavailable')} error={subscriptions.error} retrying={subscriptions.isFetching} onRetry={()=>subscriptions.refetch()}/>
  {rows.map(row=><p key={row.id}>{row.name} · {row.releaseId} · {row.subscription?.enabled?msg('Watching reviewed records'):msg('Paused')}</p>)}
 </section>
}
