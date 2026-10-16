import {readFailure} from '../assistant/failures'
import {LoadingState} from '../ui/LoadingState'
import {ResearchExecutionDetails} from './ResearchExecution'
import {ResearchBudgetHistory} from './ResearchBudget'
import { Spinner } from '../ui/Spinner'
import {ResearchRetryCountdown} from './ResearchRetries'
import {Fragment,useEffect,useRef,useState,type RefObject,type ReactNode} from 'react'
import {useQuery} from '@tanstack/react-query'
import {msg,formatDate} from '../i18n'
import {Button} from '../ui/Button'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Disclosure} from '../ui/Disclosure'
import {Notice} from '../ui/Notice'
import {ExternalLink} from '../ui/ExternalLink'
import {CodeBlock} from '../ui/CodeBlock'
import {RunStatus} from '../ui/RunStatus'
import {IconCheck,IconClose} from '../shell/icons'
import {WebAccessDialog} from '../connections/WebAccessSettings'
import {useWebAccess} from '../connections/webAccess'
import {dialectAPI} from './client'
import type {ResearchActivity,ResearchStep,ResearchRun,ResearchSummary,ResearchTask} from './research'
import styles from './DialectsView.module.css'

export function researchOutcomeLabel(run:ResearchSummary){
 const c=run.outcomes
 if(!c)return msg('{{count}} saved this run',{count:run.recordCount})
 const labels:string[]=[]
 if(c.added)labels.push(msg('{{count}} added',{count:c.added}))
 if(c.updated)labels.push(msg('{{count}} updated',{count:c.updated}))
 if(c.unchanged)labels.push(msg('{{count}} unchanged',{count:c.unchanged}))
 if(c.needsCorrection)labels.push(msg('{{count}} need correction',{count:c.needsCorrection}))
 if(c.pending)labels.push(msg('{{count}} awaiting save',{count:c.pending}))
 return labels.join(' · ')||msg('No records processed')
}
export function researchRunLabel(run:ResearchSummary){
 if(!run.reprocess)return researchStateLabel(run.state)
 if(run.state==='running')return msg('Reprocessing…')
 if(run.state==='queued')return msg('Reprocessing queued')
 if(run.state==='completed')return msg('Reprocessing complete')
 if(run.state==='completed-with-gaps'||run.state==='partial')return msg('Reprocessing finished with gaps')
 if(run.state==='stopped')return msg('Reprocessing stopped')
 if(run.state==='blocked')return msg('Reprocessing blocked')
 if(run.state==='interrupted')return msg('Reprocessing interrupted')
 return msg('Reprocessing needs review')
}
export function researchStateLabel(state:ResearchRun['state']) {
 switch(state){
  case 'running':return msg('Researching…')
  case 'queued':return msg('Research queued')
  case 'paused':return msg('Research paused — approval needed')
  case 'completed':return msg('Research complete')
  case 'completed-with-gaps':return msg('Research finished with gaps')
  case 'partial':return msg('Research incomplete')
  case 'stopped':return msg('Research stopped')
  case 'interrupted':return msg('Research interrupted')
  case 'blocked':return msg('Research blocked')
  default:return msg('Research needs review')
 }
}
export function researchOperationLabel(kind:ResearchStep['kind'],running=false){
 return kind==='model'?(running?msg('Waiting for AI response…'):msg('AI request')):kind==='search'?(running?msg('Searching sources…'):msg('Search sources')):(running?msg('Reading a website…'):msg('Read a website'))
}
function recordedTime(at?:string){const date=new Date(at??'');return Number.isFinite(date.getTime())?formatDate(date,{dateStyle:'medium',timeStyle:'medium'}):msg('Not recorded')}
export function ResearchElapsed({start,end,running=false}:{start:string;end?:string;running?:boolean}){
 const [now,setNow]=useState(Date.now())
 useEffect(()=>{if(!running)return;setNow(Date.now());const timer=setInterval(()=>setNow(Date.now()),1000);return()=>clearInterval(timer)},[running,start])
 const elapsed=Math.floor(((end?Date.parse(end):running?now:NaN)-Date.parse(start))/1000)
 if(!Number.isFinite(elapsed)||elapsed<0)return null
 const text=elapsed<60?msg('{{seconds}}s',{seconds:elapsed}):msg('{{minutes}}m {{seconds}}s',{minutes:Math.floor(elapsed/60),seconds:elapsed%60})
 return <span className={styles.stepTime} aria-live="off" aria-label={msg('Elapsed time: {{time}}',{time:text})}>{text}</span>
}

/** Current gateway capability is checked independently of the saved refusal. */
export function ResearchWebRecovery({compact=false}:{compact?:boolean}){
 const query=useWebAccess(),[open,setOpen]=useState(false),opener=useRef<HTMLButtonElement>(null),data=query.data
 const editable=!!data?.local&&data.supported&&!data.managed
 return <div className={styles.researchRecovery}>
  {!compact&&<span className={styles.problem}>{msg('Website read was blocked.')}</span>}
  {query.isPending?(!compact&&<LoadingState inline>{msg('Checking Web access settings…')}</LoadingState>):query.isError?<Button onClick={()=>void query.refetch()}>{msg('Retry settings check')}</Button>:editable?<Button ref={opener} onClick={()=>setOpen(true)}>{msg('Review web access')}</Button>:!compact&&<span className={styles.note}>{data?.managed?msg('Web access is managed by your organization. Ask your administrator to review the blocked read.'):!data?.local?msg('Configure web access on your external gateway.'):msg('Update the local gateway to configure web access.')}</span>}
  {open&&<WebAccessDialog onClose={()=>setOpen(false)} openerRef={opener} retryContext="research"/>}
 </div>
}
function stepResult(result?:string){
 switch(result){
  case 'portable-answer':return msg('AI proposed typed research fields for contract verification.')
  case 'portable-search':return msg('AI requested a source search.')
  case 'portable-page':return msg('AI requested a website read.')
  case 'portable-read_context':return msg('AI requested a retained source section.')
  case 'search':return msg('AI requested a source search.')
  case 'read':return msg('AI requested a website read.')
  case 'delegate':return msg('AI assigned independent research tasks.')
  case 'candidates':return msg('AI queued candidate pages for research.')
  case 'records':return msg('AI proposed records for preparation checks.')
  case 'finish':return msg('AI requested the end of research.')
  case 'source-saved':return msg('The source response was saved.')
  default:return msg('No result summary was recorded.')
 }
}
export function ResearchActivityDialog({preparationId,runId,opener,onClose}:{preparationId:string;runId:string;opener:RefObject<HTMLButtonElement|null>;onClose:()=>void}){
 const query=useQuery({queryKey:['input-research-activity',preparationId,runId],queryFn:()=>dialectAPI<ResearchActivity>('/'+preparationId+'/research/'+runId+'/activity'),retry:false,staleTime:0,refetchOnMount:'always',refetchInterval:query=>query.state.data?.state==='running'||query.state.data?.state==='queued'||query.state.data?.state==='paused'?1500:false})
 const run=query.data,live=run?.state==='running'||run?.state==='queued'
 return <Dialog open onOpenChange={open=>{if(!open)onClose()}} title={msg('Research activity')} size="wide" openerRef={opener} footer={<DialogActions><Button onClick={onClose}>{msg('Close')}</Button></DialogActions>}>
  <div className={styles.stack}>
   {query.isPending&&<LoadingState>{msg('Loading research steps…')}</LoadingState>}
   {query.isError&&<Notice tone="warning" actions={<Button onClick={()=>void query.refetch()}>{msg('Retry')}</Button>}>{msg('Research steps could not be refreshed. Previously loaded steps may be out of date.')}</Notice>}
   {run&&<>
    <RunStatus running={live}>{researchRunLabel(run)} · {researchOutcomeLabel(run)}{!!run.unavailableSources&&<> · <span className={styles.problem}>{msg('{{count}} sources unavailable',{count:run.unavailableSources})}</span></>}</RunStatus>
    <p className={styles.note}>{msg('Recipe version {{version}}',{version:run.recipeVersion??'—'})} · {recordedTime(run.createdAt)}</p>
    {run.sourceCount!==undefined&&<p className={styles.note}>{msg('{{count}} retained sources · Record count is for this run.',{count:run.sourceCount})}</p>}
    {run.problem&&(run.recovery==='google-grounding'?<ResearchWebRecovery/>:<p className={styles.note}>{run.problem}</p>)}
    {run.explanation&&!run.problem?.includes(run.explanation)&&<p className={styles.note}>{msg('AI explanation: {{explanation}}',{explanation:run.explanation})}</p>}
    {live&&!run.stopRequested&&run.pendingRetry&&<ResearchRetryCountdown pending={run.pendingRetry}/>}
    {run.approval&&<Disclosure title={msg('Workflow approval')}><div className={styles.stack}><p className={styles.note}>{msg('One research run was allowed in chat.')} {recordedTime(run.approval.approvedAt)}</p>{run.approval.usedAt&&<p className={styles.note}>{msg('Allowance used {{time}}',{time:recordedTime(run.approval.usedAt)})}</p>}<p className={styles.note}>{msg('Up to {{records}} records, {{searches}} searches, {{pages}} page reads and {{models}} model requests.',{records:run.approval.limits.records,searches:run.approval.limits.searches,pages:run.approval.limits.pages,models:run.approval.limits.modelCalls})}</p></div></Disclosure>}
    {run.execution?.approach==='delegated'&&<dl className={styles.detail}><ResearchExecutionDetails execution={run.execution}/></dl>}
    <ResearchBudgetHistory requests={run.budgetRequests}/>
    <ol className={styles.researchSteps} aria-label={msg('Research steps')}>
     <ResearchTaskSteps steps={run.steps} tasks={run.tasks??[]} live={live}/>
    </ol>
    {!run.steps.length&&<p className={styles.note}>{live?msg('Waiting for the first research step.'):msg('No research requests were recorded for this run.')}</p>}
    <p className={styles.note}>{msg('Saved operation history. Source accuracy and decision outcomes are not verified here.')}</p>
   </>}
  </div>
 </Dialog>
}
export function ResearchTaskSteps({steps,tasks,live}:{steps:ResearchStep[];tasks:ResearchTask[];live:boolean}){
 const shown=new Set<string>(),items:ReactNode[]=[]
 const addTask=(task:ResearchTask)=>{shown.add(task.id);items.push(<li key={task.id}><ResearchTaskDetails task={task} steps={steps.filter(step=>step.taskId===task.id)} live={live}/></li>)}
 for(const step of steps){const task=tasks.find(task=>task.id===step.taskId);if(task){if(!shown.has(task.id))addTask(task)}else items.push(<li key={step.id}><ResearchStepDetails step={step} live={live}/></li>)}
 for(const task of tasks)if(!shown.has(task.id))addTask(task)
 return <>{items}</>
}
function ResearchTaskDetails({task,steps,live}:{task:ResearchTask;steps:ResearchStep[];live:boolean}){
 const active=live&&task.state==='running'
 const status=active?msg('Running'):task.state==='queued'?(live?msg('Queued'):msg('Not started')):task.state==='complete'?msg('Completed'):task.state==='failed'?msg('Needs attention'):msg('Interrupted')
 return <Disclosure className={styles.researchStep} indicator={active?<Spinner/>:undefined} title={<span className={`${styles.stepHeading} ${styles.taskHeading}`}><span className={styles.stepLabel}>{task.title}{task.startedAt&&<ResearchElapsed start={task.startedAt} end={task.finishedAt} running={active}/>}</span><span className={styles.stepState}>{status}</span></span>}>
  <div className={styles.stack}><p className={styles.note}>{task.objective}</p><p className={styles.note}>{msg('{{count}} AI requests',{count:task.modelRequests})} · {msg('{{count}} source requests',{count:task.sourceRequests})}</p>{task.problem&&<p className={styles.note}>{task.problem}</p>}{task.result&&<p className={styles.note}>{task.result}</p>}<ol className={styles.researchSteps}>{steps.map(step=><li key={step.id}><ResearchStepDetails step={step} live={live}/></li>)}</ol></div>
 </Disclosure>
}
function ResearchStepDetails({step,live}:{step:ResearchStep;live:boolean}){
 const waiting=step.state==='waiting',notStarted=step.state==='not-started'||(!live&&waiting),running=live&&(step.state==='started'||waiting),uncertain=step.state==='uncertain'||(!live&&step.state==='started')
 const state=running?(waiting?msg('Waiting'):msg('Running')):notStarted?msg('Not started'):step.recovered?msg('Recovered'):step.actionProblem?msg('Needs correction'):step.state==='completed'?msg('Completed'):step.failure?msg('Interrupted'):step.continued||step.problemCode?sourceFailureLabel(step.problemCode):msg('Outcome uncertain')
 return <Disclosure className={styles.researchStep} indicator={running?<Spinner/>:undefined} title={<span className={styles.stepHeading} data-attention={uncertain||!!step.actionProblem&&!step.recovered||undefined}>
  <span aria-hidden="true" className={styles.stepIcon}>{running?null:step.state==='completed'&&(!step.actionProblem||step.recovered)?<IconCheck/>:<IconClose/>}</span>
  <span className={styles.stepLabel}><span>{researchOperationLabel(step.kind)}{(step.number??1)>1&&<> · {msg('Attempt {{number}}',{number:step.number!})}</>}</span><ResearchElapsed start={step.startedAt} end={step.finishedAt} running={running}/></span>
  <span className={styles.stepState}>{state}</span>
 </span>}>
  <div className={styles.stepDetails}>
   <dl className={styles.detail}><dt>{msg('Started')}</dt><dd>{recordedTime(step.startedAt)}</dd><dt>{msg('Finished')}</dt><dd>{running?msg('In progress'):recordedTime(step.finishedAt)}</dd></dl>
   {step.url&&<div className={styles.stepSource}><span className={styles.note}>{msg('Requested URL')}</span><ExternalLink href={step.url}>{step.url}</ExternalLink></div>}
   {step.query&&<div className={styles.stepSource}><span className={styles.note}>{msg('Search query')}</span><p>{step.query}</p></div>}
   {running&&waiting&&<p className={styles.note}>{msg('Waiting for the AI connection to become available. This request has not been sent.')}</p>}
   {step.actionProblem?<p className={styles.problem}>{step.actionProblem}</p>:step.continued?<p className={styles.note}>{msg('This source was left out of the research. Saved sources and records were kept; this request was not repeated after this step.')}</p>:step.problem?<p className={styles.problem}>{step.problem}</p>:step.state==='completed'?<p>{stepResult(step.result)}</p>:null}
   {!!step.recordResults?.length&&<Disclosure title={msg('Record results')}><dl className={styles.detail}>{step.recordResults.map((record,index)=><Fragment key={record.key+index}><dt>{record.name}</dt><dd>{record.recoveredBy?msg('Recovered'):record.state==='added'?msg('Added'):record.state==='updated'?msg('Updated'):record.state==='unchanged'?msg('Unchanged'):record.state==='pending'?msg('Awaiting save'):msg('Needs correction')}{record.problem&&<p className={styles.note}>{record.problem}</p>}</dd></Fragment>)}</dl></Disclosure>}
   {step.recovered&&<p className={styles.note}>{msg('The affected records were corrected in a later step. The original issue is kept in history.')}</p>}
   {step.retryAt&&<p className={styles.note}>{msg('Retry scheduled for {{time}}',{time:recordedTime(step.retryAt)})}</p>}
   {step.retryNote&&<p className={styles.note}>{step.retryNote}</p>}
   {step.explanation&&<p className={styles.note}>{msg('AI explanation: {{explanation}}',{explanation:step.explanation})}</p>}
   <Disclosure title={msg('Technical details')}><CodeBlock label={msg('Operation details')} text={JSON.stringify({failure:readFailure(step.failure),id:step.id,kind:step.kind,state:step.state,startedAt:step.startedAt,finishedAt:step.finishedAt,problemCode:step.problemCode,requestDigest:step.digest,continued:step.continued,problem:step.problem,actionProblem:step.actionProblem,attempt:step.number,retryOf:step.retryOf,retriedBy:step.retriedBy},null,2)}/></Disclosure>
  </div>
 </Disclosure>
}

function sourceFailureLabel(code?:string){
 switch(code){
  case 'source-response-too-large':case 'web-over-limit':return msg('Too large')
  case 'web-unsupported-type':return msg('Unsupported format')
  case 'search-timeout':return msg('Timed out')
  case 'search-not-grounded':case 'search-invalid-response':return msg('No usable sources')
  default:return msg('Unavailable')
 }
}
