import {LoadingState} from '../ui/LoadingState'
import {useRef,useState,type RefObject} from 'react'
import {useQueryClient} from '@tanstack/react-query'
import {msg,formatDate} from '../i18n'
import {Button} from '../ui/Button'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Notice} from '../ui/Notice'
import {Disclosure} from '../ui/Disclosure'
import {dialectAPI,type PreparationReply} from './client'
import {useDialectRecipeEditing} from './recipeEditing'
import type {ResearchBudgetRequest,ResearchBudgetReview,ResearchRun,ResearchSummary} from './research'
import styles from './DialectsView.module.css'

const failure=(error:unknown)=>error instanceof Error?error.message:msg('The request could not be completed.')

export function ResearchBudgetActions({preparationId,run,disabled=false}:{preparationId:string;run:ResearchSummary;disabled?:boolean}){
 const cache=useQueryClient(),editing=useDialectRecipeEditing(preparationId)
 const opener=useRef<HTMLButtonElement>(null),inFlight=useRef(false)
 const [review,setReview]=useState<ResearchBudgetReview>(),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const path='/'+preparationId+'/research/'+run.id
 const refresh=async()=>{await Promise.all([
  cache.invalidateQueries({queryKey:['input-research',preparationId]}),
  cache.invalidateQueries({queryKey:['input-research-activity',preparationId,run.id]}),
  cache.invalidateQueries({queryKey:['input-preparations',preparationId]})
 ])}
 const load=async()=>{
  if(inFlight.current||disabled||editing)return
  inFlight.current=true;setBusy(true);setError('')
  try{
   const saved=await dialectAPI<PreparationReply>('/'+preparationId)
   setReview(await dialectAPI<ResearchBudgetReview>(path+'/budget-review','POST',{},saved.sha256))
   await refresh()
  }catch(error){setError(failure(error))}finally{inFlight.current=false;setBusy(false)}
 }
 const decide=async(decision:'approve'|'decline')=>{
  const request=decision==='approve'?review?.request:run.budgetRequest
  if(!request||inFlight.current||decision==='approve'&&(disabled||editing))return
  inFlight.current=true;setBusy(true);setError('')
  try{
   const updated=await dialectAPI<ResearchRun>(path+'/budget-decision','POST',{requestId:request.id,decision},decision==='approve'?review?.revision:undefined)
   cache.setQueryData<{items:ResearchSummary[]}>(['input-research',preparationId],current=>current?{items:current.items.map(item=>item.id===run.id?{...item,state:updated.state,problem:updated.problem,updatedAt:updated.updatedAt,phase:updated.phase,budgetRequest:undefined,canIncrease:false}:item)}:current)
   setReview(undefined);await refresh()
  }catch(error){setError(failure(error));void refresh()}finally{inFlight.current=false;setBusy(false)}
 }
 return <>
  {run.budgetRequest&&<Button variant="quiet" disabled={busy} onClick={()=>void decide('decline')}>{run.recordCount?msg('Keep {{count}} records',{count:run.recordCount}):msg('Finish without records')}</Button>}
  <Button ref={opener} disabled={busy||disabled||editing} onClick={()=>void load()}>{busy&&!review?<LoadingState inline>{msg('Loading…')}</LoadingState>:disabled||editing?msg('Save recipe before continuing'):msg('Review increase')}</Button>
  {error&&!review&&<Notice tone="warning">{error}</Notice>}
  {review&&<ResearchBudgetDialog review={review} opener={opener} busy={busy} blocked={disabled||editing} error={error} onClose={()=>{if(!inFlight.current){setReview(undefined);setError('')}}} onApprove={()=>void decide('approve')} onReload={()=>void load()}/>}
 </>
}
function ResearchBudgetDialog({review,opener,busy,blocked,error,onClose,onApprove,onReload}:{review:ResearchBudgetReview;opener:RefObject<HTMLButtonElement|null>;busy:boolean;blocked:boolean;error:string;onClose:()=>void;onApprove:()=>void;onReload:()=>void}){
 return <Dialog open title={msg('Review research increase')} openerRef={opener} onOpenChange={open=>{if(!open&&!busy)onClose()}} footer={<DialogActions><Button disabled={busy} onClick={onClose}>{msg('Cancel')}</Button><Button variant="primary" disabled={busy||blocked||!!error} onClick={onApprove}>{busy?msg('Continuing…'):msg('Approve and continue')}</Button></DialogActions>}>
  <div className={styles.stack}>
   <p>{msg('{{saved}} of {{maximum}} records saved',{saved:review.recordCount,maximum:review.request.current.records})}</p>
   <p className={styles.note}>{review.request.reason}</p>
   <ResearchBudgetTable request={review.request}/>
   <p className={styles.note}>{msg('Applies only to this run. The record target, recipe, connections and retry policy stay unchanged.')}</p>
   <p className={styles.note}>{msg('Continue from the saved records and sources. Completed requests are retained; failed requests are not automatically repeated.')}</p>
   <p className={styles.note}>{msg('Additional requests may incur provider usage. More records are not guaranteed.')}</p>
   {blocked&&<Notice tone="warning">{msg('Save or discard Recipe edits before approving this increase.')}</Notice>}
   {error&&<Notice tone="warning" actions={<Button disabled={busy||blocked} onClick={onReload}>{msg('Reload review')}</Button>}>{error}</Notice>}
  </div>
 </Dialog>
}
export function ResearchBudgetTable({request}:{request:ResearchBudgetRequest}){
 return <table className={[styles.table,styles.budgetTable].join(' ')} aria-label={msg('Research request allowance')}>
  <thead><tr><th scope="col">{msg('Allowance')}</th><th scope="col">{msg('Used')}</th><th scope="col">{msg('Current')}</th><th scope="col">{msg('Proposed')}</th></tr></thead>
  <tbody>{([{key:'searches',label:msg('Searches')},{key:'pages',label:msg('Page reads')},{key:'modelCalls',label:msg('Model requests')}] as const).map(({key,label})=><tr key={key}><th scope="row">{label}</th><td>{request.used[key]}</td><td>{request.current[key]}</td><td>{request.proposed[key]}{request.proposed[key]>request.current[key]&&<span className={styles.cellMeta}>+{request.proposed[key]-request.current[key]}</span>}</td></tr>)}</tbody>
 </table>
}
export function ResearchBudgetHistory({requests}:{requests?:ResearchBudgetRequest[]}){
 if(!requests?.length)return null
 return <Disclosure title={msg('Budget decisions')}><div className={styles.stack}>{requests.map(request=><section className={styles.stack} key={request.id}>
  <p className={styles.note}>{request.state==='approved'?msg('Approved'):request.state==='declined'?msg('Declined'):msg('Awaiting your approval')} · {formatDate(new Date(request.resolvedAt??request.createdAt),{dateStyle:'medium',timeStyle:'short'})}{request.reviewer?.name&&<> · {request.reviewer.name}</>}</p>
  <ResearchBudgetTable request={request}/>
 </section>)}</div></Disclosure>
}
