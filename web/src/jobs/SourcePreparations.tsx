import {useEffect,useRef,useState} from 'react'
import {useInfiniteQuery,useQueryClient} from '@tanstack/react-query'
import {Link} from 'react-router-dom'
import {msg,formatDate,language} from '../i18n'
import {Button} from '../ui/Button'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Disclosure} from '../ui/Disclosure'
import {TypedConfirmation} from '../shell/UnsavedChanges'
import {jobsAPI,type Page} from './client'
import type {Occurrence} from './triggerTypes'
import styles from './JobsView.module.css'

export function occurrenceState(state:string):string {
 return ({accepted:msg('Queued'),preparing:msg('Preparing inputs'),waiting:msg('Waiting for sources'),'needs-attention':msg('Needs attention'),cancelled:msg('Canceled.'),'cancel-requested':msg('Cancellation requested'),submitted:msg('Submitted'),skipped:msg('Skipped'),failed:msg('Failed'),expired:msg('Expired'),queued:msg('Queued'),running:msg('Running'),completed:msg('Completed')} as Record<string,string>)[state]??msg('Needs attention')
}
const date=(s:string)=>formatDate(new Date(s),{dateStyle:'medium',timeStyle:'short'})
function elapsedSince(startedAt:string){
 const seconds=Math.max(0,Math.floor((Date.now()-new Date(startedAt).getTime())/1000))
 const unit=seconds>=3600?'hour':seconds>=60?'minute':'second'
 return new Intl.RelativeTimeFormat(language(),{numeric:'always'}).format(-Math.floor(seconds/(unit==='hour'?3600:unit==='minute'?60:1)),unit)
}
export function SourceProgress({occurrence:o}:{occurrence:Occurrence}) {
 const client=useQueryClient(),opener=useRef<HTMLElement|null>(null)
 const [confirm,setConfirm]=useState(false),[typed,setTyped]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const p=o.preparation
 if(!p)return null
 async function act(action:'cancel'|'reconcile'){
  setBusy(true);setError('')
  try{await jobsAPI(`occurrences/${o.id}/${action}`,{});setConfirm(false);await client.invalidateQueries({queryKey:['job-occurrences',o.jobId]})}
  catch(e){setError(e instanceof Error?e.message:String(e))}finally{setBusy(false)}
 }
 const canAct=o.state==='waiting'||o.state==='needs-attention'
 return <div className={styles.sourceProgress}>
  <Disclosure title={msg('Sources · {{count}}',{count:p.tasks.length})}>
   <dl className={styles.properties}><div><dt>{msg('Submitted')}</dt><dd>{date(p.startedAt)}{o.state==='waiting'&&<span className={styles.cellMeta}>{elapsedSince(p.startedAt)}</span>}</dd></div><div><dt>{msg('Source deadline')}</dt><dd>{date(p.deadline)}</dd></div></dl>
   <ul className={styles.sourceSteps}>{p.tasks.map(task=><li key={task.id}><span>{task.name}</span><span className="quiet">{occurrenceState(task.state)}</span></li>)}</ul>
   {o.reason&&<p className={styles.note}>{o.reason}</p>}
   {canAct&&<div className={styles.actions}>{o.state==='needs-attention'&&new Date(p.deadline).getTime()>Date.now()&&<Button disabled={busy} onClick={()=>void act('reconcile')}>{msg('Check status')}</Button>}<Button variant="quiet" disabled={busy} onClick={e=>{opener.current=e.currentTarget;setTyped('');setConfirm(true)}}>{msg('Cancel')}</Button></div>}
  </Disclosure>
  {error&&<p role="alert" className={styles.problem}>{error}</p>}
  <Dialog open={confirm} onOpenChange={open=>{if(!busy)setConfirm(open)}} title={msg('Cancel source preparation?')} openerRef={opener} footer={<DialogActions><Button disabled={busy} onClick={()=>setConfirm(false)}>{msg('Back')}</Button><Button variant="primary" disabled={busy||typed!==msg('Yes')} onClick={()=>void act('cancel')}>{msg('Confirm')}</Button></DialogActions>}>
   <p>{msg('No decision will be evaluated. The provider may still finish; late results will not be used.')}</p><TypedConfirmation value={typed} onChange={setTyped} disabled={busy}/>
  </Dialog>
 </div>
}
export function SourcePreparations({jobId,onHasItems}:{jobId:string;onHasItems?:(hasItems:boolean)=>void}){
 const query=useInfiniteQuery({queryKey:['job-occurrences',jobId,'preparations'],initialPageParam:0,queryFn:({pageParam})=>jobsAPI<Page<Occurrence>>(`jobs/${jobId}/occurrences?preparations=1&after=${pageParam}`),getNextPageParam:p=>p.next||undefined,refetchInterval:5000})
 const items=query.data?.pages.flatMap(p=>p.items).filter(o=>o.preparation)??[]
 useEffect(()=>onHasItems?.(items.length>0),[items.length,onHasItems])
 if(query.error)return <p role="alert" className={styles.problem}>{msg('Source preparation could not be loaded.')}</p>
 if(!items.length)return null
 return <section className={styles.stack}><h2>{msg('Source preparation')}</h2><div className={styles.tableWrap}><table className={styles.table}><thead><tr><th>{msg('Received')}</th><th>{msg('Status')}</th><th>{msg('Details')}</th></tr></thead><tbody>{items.map(o=><tr key={o.id}><td>{date(o.receivedAt)}</td><td><span className={o.state==='waiting'?styles.sourceWaiting:undefined}>{occurrenceState(o.state)}</span>{o.reason&&o.state!=='waiting'&&<span className={styles.cellMeta}>{o.reason}</span>}{o.runId&&<Link className={styles.cellMeta} to={`/jobs/${jobId}/runs/${o.runId}`}>{msg('View run')}</Link>}</td><td><SourceProgress occurrence={o}/></td></tr>)}</tbody></table></div>{query.hasNextPage&&<div><Button disabled={query.isFetchingNextPage} onClick={()=>void query.fetchNextPage()}>{msg('Load more')}</Button></div>}</section>
}
