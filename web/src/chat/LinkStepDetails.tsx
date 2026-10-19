import { ChatWebAccessRecovery } from '../connections/WebAccessSettings'
import { ExternalLink } from '../ui/ExternalLink'
import { linkFailureMessage } from './linkFailure'
import { Disclosure } from '../ui/Disclosure'
import { msg, useLocale } from '../i18n'
import { normalizeLink } from '../documents/link'
import { SourceReader } from '../documents/SourceReader'
import { ReadingDetails } from './ReadingDetails'
import type { WorkItem } from './responseHistory'
import type { ChatAttachment } from './store'
import styles from '../search/SearchSources.module.css'

export function LinkStepDetails({row,document}:{row:WorkItem;document?:ChatAttachment}){
 useLocale()
 const link=row.link, ref=document?.document
 const parsed=link&&normalizeLink(link.url)
 if(link?.pages&&parsed&& !('refused' in parsed)&&document?.link?.url===parsed.fetchUrl&&ref&&ref.id===link.documentId&&ref.digest===link.digest&&link.pages.every(page=>ref.pages.includes(page))){
  return <SourceReader name={document.name} reference={{...ref,pages:link.pages}} link={document.link} requestedUrl={link.url}/>
 }
 return <ReadingDetails title={msg('Read a link')}><div className={styles.reader}>
  <p className={styles.note}>{row.status==='working'?msg('Working…'):row.status==='failed'?msg('Failed'):row.status==='interrupted'?msg('Interrupted'):msg('Done')}</p>
  {row.status==='failed'&&<p className={styles.query}>{row.linkFailure?linkFailureMessage(row.linkFailure):msg('Details were not recorded for this step.')}</p>}
  {row.status!=='working'&&<p className={styles.note}>{link?msg('No retained page is available for this step.'):row.status!=='failed'?msg('Details were not recorded for this step.'):null}</p>}
  {row.redirectPolicy==='google-grounding'&&<ChatWebAccessRecovery/>}
  {link&&<Disclosure title={msg('Requested URL')}><p className={styles.query}><ExternalLink href={link.url}>{link.url}</ExternalLink></p></Disclosure>}
 </div></ReadingDetails>
}
