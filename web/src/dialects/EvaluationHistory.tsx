import {Link} from 'react-router-dom'
import {msg} from '../i18n'
import {Button} from '../ui/Button'
import {Notice} from '../ui/Notice'
import type {DialectEvaluation,DialectEvaluationRow} from './client'
import styles from './DialectsView.module.css'

export function evaluationState(state:DialectEvaluationRow['state']){
 switch(state){case 'superseded':return msg('Superseded');case 'expired':return msg('Expired');case 'skipped':return msg('Skipped');case 'cancelled':return msg('Cancelled');case 'needs-attention':return msg('Needs attention');case 'accepted':case 'preparing':case 'waiting':case 'submitted':return msg('Queued');case 'pending':return msg('Pending');case 'queued':return msg('Queued');case 'running':return msg('Running');case 'completed':return msg('Completed');case 'failed':return msg('Failed');case 'interrupted':return msg('Interrupted')}
}
export function latestRecordEvaluation(batches:DialectEvaluation[],recordId:string){
 for(const batch of batches){const row=[...batch.rows].reverse().find(r=>r.recordId===recordId);if(row)return {batch,row}}
}
export function EvaluationResult({result,recipeVersion}:{result?:ReturnType<typeof latestRecordEvaluation>;recipeVersion:number}){
 if(!result)return <>{msg('Not run')}</>
 const {batch,row}=result,content=<>{evaluationState(row.state)}{batch.recipeVersion!==recipeVersion&&<span className={styles.cellMeta}>{msg('Earlier recipe')}</span>}{row.signing?.status==='failed'&&<span className={`${styles.cellMeta} ${styles.problem}`}>{msg('Signing failed')}</span>}</>
 return row.runId&&batch.jobId?<Link className={styles.resultLink} to={'/jobs/'+batch.jobId+'/runs/'+row.runId}>{content}</Link>:<span>{content}</span>
}
export function EvaluationSummary({batch,onRetry,busy}:{batch:DialectEvaluation;onRetry:()=>void;busy:boolean}){
 const completed=batch.rows.filter(r=>r.state==='completed').length,failed=batch.rows.filter(r=>r.state==='failed'||r.state==='interrupted').length,pending=batch.rows.length-completed-failed
 return <div className={styles.stack}><div className={styles.reviewSummary}><p className={styles.note} role="status">{msg('{{completed}} completed · {{failed}} failed · {{pending}} pending',{completed,failed,pending})}</p>{batch.jobId&&<Link className={styles.itemLink} to={'/jobs/'+batch.jobId}>{msg('View job')}</Link>}</div>{batch.problem&&<Notice tone="warning" actions={batch.state==='blocked'?<Button disabled={busy} onClick={onRetry}>{msg('Retry pending submissions')}</Button>:undefined}>{batch.problem}</Notice>}</div>
}
