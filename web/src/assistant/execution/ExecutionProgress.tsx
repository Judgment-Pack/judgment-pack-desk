import {msg} from '../../i18n'
import {ChatAction,ChatActions} from '../../chat/ChatActions'
import {Disclosure} from '../../ui/Disclosure'
import {Spinner} from '../../ui/Spinner'
import {IconCheck} from '../../shell/icons'
import {MAX_REQUEST_ALLOWANCE,REQUEST_INCREMENT,type ExecutionCheckpoint} from '@judgment-pack/harness/execution/state'
import {RunReportLines} from '../RunReportLines'
import type {RunReport} from '../report'
import styles from './ExecutionProgress.module.css'
export function ExecutionProgress({execution,report,disabled,onContinue,onStop}:{execution?:ExecutionCheckpoint;/** What the response reports about itself so far: the totals shown under the tasks. */report?:RunReport;disabled?:boolean;onContinue?:()=>void;onStop?:()=>void}){
 if(!execution||!execution.plan.length&&execution.status!=='paused')return null
 const paused=execution.status==='paused'
 return <section className={styles.progress} aria-label={msg('Task progress')}>
 {execution.plan.length>0&&<Disclosure title={msg('Tasks · {{done}} of {{total}} complete',{done:execution.plan.filter(s=>s.status==='completed').length,total:execution.plan.length})}>
  <ol className={styles.steps}>{execution.plan.map((s,i)=><li key={i}><span className={styles.indicator}>{s.status==='completed'?<IconCheck/>:s.status==='in_progress'&&execution.status==='running'?<Spinner/>:i+1}</span><span>{s.step}{s.detail&&<small>{s.detail}</small>}</span></li>)}</ol>
  {execution.explanation&&<p>{execution.explanation}</p>}
  {report&&<RunReportLines report={report}/>}
 </Disclosure>}
 {paused&&execution.plan.length===0&&report&&<RunReportLines report={report}/>}
 {paused&&<div className={styles.pause} role="status"><strong>{msg('Paused · Request limit reached')}</strong><span>{msg('{{used}} of {{limit}} requests used',{used:execution.used,limit:execution.limit})}</span><p>{execution.reason||msg('Completed tool results and task progress are kept. Continue to finish the remaining work.')}</p>
 <ChatActions>{onStop&&<ChatAction variant="quiet" disabled={disabled} onClick={onStop}>{msg('Keep completed work')}</ChatAction>}{onContinue&&execution.limit<MAX_REQUEST_ALLOWANCE&&<ChatAction disabled={disabled} onClick={onContinue}>{msg('Continue · +{{count}} requests',{count:Math.min(REQUEST_INCREMENT,MAX_REQUEST_ALLOWANCE-execution.limit)})}</ChatAction>}</ChatActions></div>}
 </section>
}
