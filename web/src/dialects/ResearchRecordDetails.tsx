import {useQuery} from '@tanstack/react-query'
import {msg,formatDate} from '../i18n'
import {privateQuery} from '../data/privateQuery'
import {QueryStatusNotice} from '../shell/QueryStatusNotice'
import {ExternalLink} from '../ui/ExternalLink'
import {RecordDetailsTable} from '../ui/RecordDetailsTable'
import {dialectAPI,type DialectRecord} from './client'
import type {ResearchRun,ResearchValue} from './research'
import styles from './ResearchRecordDetails.module.css'

export function researchFieldLabel(key:string){const text=key.replace(/[_-]/g,' ').replace(/([a-z])([A-Z])/g,'$1 $2');return text.charAt(0).toUpperCase()+text.slice(1)}
export function ResearchRecordDetails({record,preparationId,mappedFields=[],previous}:{record:DialectRecord;preparationId:string;mappedFields?:string[];previous?:DialectRecord}){
 const ref=record.research!
 const source=useQuery({...privateQuery,queryKey:['input-research-run',preparationId,ref.runId],queryFn:()=>dialectAPI<ResearchRun>('/'+preparationId+'/research/'+ref.runId),retry:false})
 const status=(state:ResearchValue['status'])=>({supported:msg('Source-backed'),interpretation:msg('AI interpretation'),missing:msg('Missing'),conflicting:msg('Conflicting sources'),inaccessible:msg('Source unavailable'),stale:msg('Outdated source')})[state]
 const old=previous?.research?.entity.fields
 const fields=Object.entries(ref.entity.fields).sort(([a],[b])=>Number(mappedFields.includes(b))-Number(mappedFields.includes(a))).map(([key,field])=>{
  const citations=field.sources??[]
  return {key,label:researchFieldLabel(key),value:field.value===undefined?msg('Unknown'):typeof field.value==='object'?JSON.stringify(field.value,null,2):String(field.value),state:status(field.status)+(citations.length?' · '+(citations.length===1?msg('1 source'):msg('{{count}} sources',{count:citations.length})):''),attention:field.status!=='supported',changed:!!old&&JSON.stringify(old[key])!==JSON.stringify(field),details:<>
   {field.note&&<p>{field.note}</p>}
   {citations.length?citations.map((citation,index)=>{const page=source.data?.sources?.find(s=>s.id===citation.sourceId);return <div className={styles.source} key={index}>{page?.url&&<ExternalLink href={page.url}>{page.url}</ExternalLink>}<blockquote>{citation.quote}</blockquote>{page?.at&&<span className={styles.note}>{msg('Retrieved {{date}}',{date:formatDate(new Date(page.at),{dateStyle:'medium',timeStyle:'short'})})}</span>}</div>}):<p className={styles.note}>{msg('No retained quotations for this field.')}</p>}
   <span className={styles.note}>{msg('Field')}: <code>{key}</code>{mappedFields.includes(key)&&<> · {msg('Used by the pack')}</>}</span>
  </>}
 })
 return <div className={styles.root}>
  <QueryStatusNotice id={'review-sources:'+record.id} label={msg('Source details unavailable')} error={source.error} retrying={source.isFetching} onRetry={()=>source.refetch()}>{msg('Saved quotations remain available. Source metadata could not be refreshed.')}</QueryStatusNotice>
  <dl className={styles.links}><dt>{msg('Evidence link')}</dt><dd><ExternalLink href={ref.entity.url}>{ref.entity.url}</ExternalLink></dd><dt>{msg('Website')}</dt><dd>{ref.entity.website?<ExternalLink href={ref.entity.website}>{ref.entity.website}</ExternalLink>:msg('Not confirmed')}</dd>{ref.entity.companyId&&<><dt>{msg('Company')}</dt><dd>{source.data?.companies?.find(c=>c.id===ref.entity.companyId)?.name??msg('Selected company')}</dd></>}</dl>
  <RecordDetailsTable key={record.id+ref.runId} fields={fields} hasPrevious={!!old}/>
 </div>
}
