import {Fragment,useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {msg,useLocale} from '../i18n'
import {CollectionToolbar} from '../ui/CollectionToolbar'
import {CollectionTable} from '../ui/SelectionTable'
import {Button} from '../ui/Button'
import {LoadingState} from '../ui/LoadingState'
import {Disclosure} from '../ui/Disclosure'
import {QueryStatusNotice} from '../shell/QueryStatusNotice'
import {readPolicyRevision,type PolicyActor,type PolicyDocument} from './client'
import styles from './PolicyPage.module.css'

const actorName=(actor:PolicyActor)=>actor.subject||actor.kind
export function PolicyHistory({document}:{document:PolicyDocument}){
 const locale=useLocale(),[search,setSearch]=useState(''),[kind,setKind]=useState('all'),[selected,setSelected]=useState<string>()
 const events=[
  ...document.revisions.map(revision=>({id:revision.id,sequence:revision.sequence,kind:'draft',label:msg('Draft saved'),at:revision.at,actor:revision.actor,version:revision.artifactVersion,digest:revision.digest,revisionId:revision.id,comment:undefined as string|undefined})),
  ...document.reviews.map(review=>({id:review.id,sequence:review.sequence,kind:'review',label:review.decision==='approve'?msg('Approved'):msg('Changes requested'),at:review.at,actor:review.actor,version:document.revisions.find(revision=>revision.id===review.candidateRevisionId)?.artifactVersion??'',digest:review.candidateDigest,revisionId:review.candidateRevisionId,comment:review.comment})),
  ...document.publications.map(publication=>({id:publication.id,sequence:publication.sequence,kind:'publication',label:msg('Published'),at:publication.at,actor:publication.actor,version:publication.artifactVersion,digest:publication.candidateDigest,revisionId:publication.candidateRevisionId,comment:undefined})),
 ].filter(event=>(kind==='all'||event.kind===kind)&&`${event.label} ${event.version} ${event.actor.subject??''} ${event.comment??''}`.toLocaleLowerCase().includes(search.toLocaleLowerCase())).sort((a,b)=>b.sequence-a.sequence)
 return <div className={styles.history}>
  <CollectionToolbar label={msg('Policy history controls')} searchLabel={msg('Search policy history')} search={search} onSearch={setSearch} filters={[{label:msg('Event type'),value:kind,onValueChange:setKind,options:[{value:'all',label:msg('All events')},{value:'draft',label:msg('Drafts')},{value:'review',label:msg('Reviews')},{value:'publication',label:msg('Publications')}]}]} filtered={!!search||kind!=='all'} onReset={()=>{setSearch('');setKind('all')}}/>
  <div className={styles.tableScroll}><CollectionTable className={styles.table} aria-label={msg('Policy history')}><thead><tr><th scope="col">{msg('Event')}</th><th scope="col">{msg('Version')}</th><th scope="col">{msg('Actor')}</th><th scope="col">{msg('Time')}</th><th scope="col">{msg('Details')}</th></tr></thead><tbody>{events.map(event=><Fragment key={event.id}>
   <tr><th scope="row">{event.label}</th><td>{event.version}</td><td className={styles.actor}>{actorName(event.actor)}</td><td><time dateTime={event.at}>{new Date(event.at).toLocaleString(locale)}</time></td><td><Button size="small" variant="quiet" aria-expanded={selected===event.id} aria-label={msg('Inspect {{event}}',{event:event.label})} onClick={()=>setSelected(selected===event.id?undefined:event.id)}>{msg('Inspect')}</Button></td></tr>
   {selected===event.id&&<tr><td colSpan={5}><div className={styles.historyDetail}><dl className={styles.facts}><dt>{msg('Candidate digest')}</dt><dd className={styles.digest}>{event.digest}</dd><dt>{msg('Actor')}</dt><dd>{event.actor.kind} · {event.actor.subject}{event.actor.issuer?' · '+event.actor.issuer:''}</dd>{event.comment&&<><dt>{msg('Review comment')}</dt><dd>{event.comment}</dd></>}</dl><RevisionSnapshot policyId={document.id} revisionId={event.revisionId}/></div></td></tr>}
  </Fragment>)}</tbody></CollectionTable></div>
  {!events.length&&<p className={styles.note}>{msg('No matching history events.')}</p>}
 </div>
}
function RevisionSnapshot({policyId,revisionId}:{policyId:string;revisionId:string}){
 const query=useQuery({queryKey:['policies',policyId,'revision',revisionId],queryFn:({signal})=>readPolicyRevision(policyId,revisionId,signal),retry:false,staleTime:Infinity})
 return <><QueryStatusNotice id={'policy-history:'+policyId+':'+revisionId} label={msg('Policy revision unavailable')} error={query.error} retrying={query.isFetching} onRetry={()=>query.refetch()}/>{query.isPending?<LoadingState>{msg('Loading saved revision…')}</LoadingState>:query.data?<><dl className={styles.facts}><dt>{msg('Title')}</dt><dd>{query.data.candidate.title}</dd><dt>{msg('Artifact version')}</dt><dd>{query.data.candidate.artifactVersion}</dd><dt>{msg('Review workflow')}</dt><dd>{query.data.workflow.independentReviewRequired?msg('Independent review required'):msg('Local administrator review')}</dd></dl><Disclosure title={msg('Saved policy text')} open><pre className={styles.snapshot}>{query.data.candidate.narrative}</pre></Disclosure></>:<p className={styles.note}>{msg('Policy revision unavailable')}</p>}</>
}
