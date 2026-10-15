import {privateQuery} from '../data/privateQuery'
import {dialectListQuery} from './queries'
import {useRef,useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {Link} from 'react-router-dom'
import {msg} from '../i18n'
import {answer,deskFetch} from '../files/client'
import {Button} from '../ui/Button'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Field} from '../ui/Field'
import {Select} from '../ui/Select'
import {Notice} from '../ui/Notice'
import {DeskStatusNotice} from '../shell/DeskNotices'
import {QueryStatusNotice} from '../shell/QueryStatusNotice'
import {dialectAPI,type DialectEvaluation,type PreparationReply} from './client'
import {ReviewRecords} from './ReviewRecords'
import {currentRecordReview} from './recordReview'
import styles from './DialectsView.module.css'
export const subscriptionsKey=['input-subscriptions']
export function useDialectSubscriptions(enabled=true){return useQuery({enabled,queryKey:subscriptionsKey,queryFn:async()=>answer<{items:DialectEvaluation[]}>(await deskFetch('/api/input-subscriptions')),refetchInterval:4000})}
export function DialectSubscriptions({preparationId,jobId,evaluations}:{preparationId?:string;jobId?:string;evaluations?:DialectEvaluation[]}){
 const query=useDialectSubscriptions(evaluations===undefined),[review,setReview]=useState<DialectEvaluation>(),[checked,setChecked]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState(''),opener=useRef<HTMLButtonElement|null>(null)
 const rows=(evaluations??query.data?.items??[]).filter(b=>(!preparationId||b.preparationId===preparationId)&&(!jobId||b.jobId===jobId))
 async function change(b:DialectEvaluation,enabled:boolean){setBusy(true);setError('');try{await dialectAPI('/'+b.preparationId+'/evaluations/'+b.id+'/subscription-state','POST',{reviewed:true,enabled});setReview(undefined);await query.refetch()}catch(e){setError((e as Error).message)}finally{setBusy(false)}}
 return <><QueryStatusNotice id={'input-subscriptions-'+(jobId??preparationId??'all')} label={msg('Dialects subscriptions unavailable')} error={query.error} retrying={query.isFetching} onRetry={()=>query.refetch()}/>
 {rows.map(b=><div key={b.id} className={styles.reviewSummary}><div><strong>{msg('Dialects subscription')}</strong> · {b.name}<p className={styles.note}>{b.state==='blocked'?msg('Needs attention'):b.subscription?.enabled?msg('Watching reviewed records'):msg('Paused')} · {b.subscription?.mode==='changed'?msg('New and changed records'):msg('New records only')} · {b.subscription?.sourceClass==='generated'?msg('AI research'):msg('Supplied')}</p>{b.problem&&<DeskStatusNotice id={'subscription-'+b.id} tone="warning" label={msg('Dialects subscription needs attention')}>{b.problem}</DeskStatusNotice>}</div><div>{b.jobId&&!jobId&&<Link to={'/jobs/'+b.jobId+'?tab=triggers'}>{msg('Open job')}</Link>} {b.subscription?.enabled&&b.state==='blocked'&&<Button disabled={busy} onClick={()=>void change(b,false)}>{msg('Pause')}</Button>}<Button disabled={busy} onClick={event=>{opener.current=event.currentTarget;setError('');if(b.subscription?.enabled&&b.state!=='blocked')void change(b,false);else{setChecked(false);setReview(b)}}}>{b.subscription?.enabled&&b.state!=='blocked'?msg('Pause'):msg('Review and enable')}</Button></div></div>)}
 {error&&!review&&<Notice tone="error">{error}</Notice>}
 <Dialog open={!!review} onOpenChange={open=>{if(!open&&!busy)setReview(undefined)}} title={msg('Enable Dialects subscription')} openerRef={opener} footer={<DialogActions><Button disabled={busy} onClick={()=>setReview(undefined)}>{msg('Cancel')}</Button><Button variant="primary" disabled={busy||!checked} onClick={()=>review&&void change(review,true)}>{msg('Enable')}</Button></DialogActions>}>
 {review&&<div className={styles.stack}><p>{review.name} · {msg('Recipe version {{version}}',{version:review.recipeVersion})}</p><p>{review.subscription?.mode==='changed'?msg('New records and changed, re-reviewed records will receive decisions.'):msg('Each new record will receive one decision. Later changes will not trigger another run.')}</p><p>{review.subscription?.initialized?msg('The saved baseline is kept when resuming.'):review.subscription?.includeExisting?msg('Existing eligible records will be included.'):msg('Records present when you enable will become the baseline and will be skipped.')}</p><p className={styles.note}>{msg('Uses the reviewed fixed release. Keep Desk running; the browser may close. Pausing stops new deliveries; accepted work continues.')}</p>{error&&<Notice tone="error">{error}</Notice>}<label className="checkbox"><input type="checkbox" checked={checked} disabled={busy} onChange={e=>setChecked(e.target.checked)}/>{msg('I reviewed the source, dialect, eligibility and automatic execution policy.')}</label></div>}
 </Dialog></>
}
export function ConnectDialectDialog({opener,onClose,onSaved}:{opener:React.RefObject<HTMLButtonElement|null>;onClose:()=>void;onSaved:()=>void}){
 const [id,setId]=useState(''),[sourceClass,setSourceClass]=useState('generated'),[review,setReview]=useState(false)
 const list=useQuery(dialectListQuery())
 const query=useQuery({...privateQuery,queryKey:['input-job-source',id],enabled:!!id,queryFn:()=>dialectAPI<PreparationReply>('/'+id)})
 const doc=query.data?.document,record=doc?.records.find(r=>r.class===sourceClass&&(r.class==='asserted'||currentRecordReview(r,doc.recipeVersion)?.decision==='accepted'))
 if(review&&query.data&&record)return <ReviewRecords saved={query.data} records={[record]} opener={opener} subscriptionOnly onClose={()=>setReview(false)} onEvaluated={()=>{onSaved();onClose()}}/>
 return <Dialog open title={msg('Connect Dialects to a job')} openerRef={opener} onOpenChange={open=>{if(!open)onClose()}} footer={<DialogActions><Button onClick={onClose}>{msg('Cancel')}</Button><Button variant="primary" disabled={!doc?.recipe.qualification||!record} onClick={()=>setReview(true)}>{msg('Review records')}</Button></DialogActions>}><div className={styles.stack}>
 <Field label={msg('Dialects')}>{props=><Select {...props} value={id} options={(list.data?.items??[]).map(r=>({value:r.id,label:r.name}))} onValueChange={setId}/>}</Field>
 <Field label={msg('Record origin')}>{props=><Select {...props} value={sourceClass} options={[{value:'generated',label:msg('AI research')},{value:'asserted',label:msg('Supplied')}]} onValueChange={setSourceClass}/>}</Field>
 <p className={styles.note}>{msg('The first eligible record is used to review the dialect and fixed release. Save paused before enabling automatic delivery.')}</p>
 {doc&&(!doc.recipe.qualification||!record)&&<Notice tone="warning">{msg('Save a pack dialect and prepare at least one eligible record first.')} <Link to={'/dialects/'+doc.id+'?tab=recipe'}>{msg('Open recipe')}</Link></Notice>}
 {(list.error||query.error)&&<Notice tone="error">{(list.error??query.error)?.message}</Notice>}
 </div></Dialog>
}
