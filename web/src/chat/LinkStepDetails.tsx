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
  {link&&<div><h3 className={styles.label}>{msg('Requested URL')}</h3><p className={styles.query}>{link.url}</p></div>}
  <p className={styles.note}>{row.status==='working'?msg('Working…'):row.status==='failed'?msg('Failed'):row.status==='interrupted'?msg('Interrupted'):msg('Done')}</p>
  {row.status!=='working'&&<p className={styles.note}>{link?msg('No retained page is available for this step.'):msg('Details were not recorded for this step.')}</p>}
 </div></ReadingDetails>
}
