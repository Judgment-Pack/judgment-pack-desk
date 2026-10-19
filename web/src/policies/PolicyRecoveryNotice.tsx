import {msg,useLocale} from '../i18n'
import {Notice} from '../ui/Notice'
import type {PolicyRecoveryInfo} from './client'
import styles from './PolicyPage.module.css'

export function PolicyRecoveryNotice({recovery}:{recovery?:PolicyRecoveryInfo}){
 const locale=useLocale()
 if(!recovery)return null
 return <Notice className={styles.recoveryNotice} tone="warning" title={msg('Restored Policy history · Currentness unknown')} detail={<dl className={styles.facts}><dt>{msg('Recovery snapshot')}</dt><dd className={styles.digest}>{recovery.snapshotId}</dd><dt>{msg('Restored')}</dt><dd>{new Date(recovery.restoredAt).toLocaleString(locale)}</dd></dl>}>
  <p>{msg('Archived approvals, publications and source assertions were integrity checked on import. They are historical records, not authenticated current approval or permission to execute.')}</p>
  {recovery.freshReviewRequired&&<p>{msg('Review the saved candidate again, then publish a new local version before activating a release.')}</p>}
  {recovery.sourceConsentRequired&&<p>{msg('Archived source authorization is inactive. Choose the source again and review new consent before any provider access.')}</p>}
 </Notice>
}
