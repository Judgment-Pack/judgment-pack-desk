import {LoadingState} from '../ui/LoadingState'
import {ResearchExecutionDetails} from './ResearchExecution'
import {useEffectiveConfig} from '../config/DeskConfigProvider'
import {useSearchConnections} from '../search/connections'
import {Field} from '../ui/Field'
import {Select} from '../ui/Select'
import {ResearchBudgetActions} from './ResearchBudget'
import {useDialectRecipeEditing} from './recipeEditing'
import {ResearchRetryCountdown,ResearchRetryReview} from './ResearchRetries'
import {RunStatus} from '../ui/RunStatus'
import {ResearchWebRecovery,ResearchElapsed,researchRunLabel,researchOutcomeLabel,researchOperationLabel} from './ResearchActivity'
import {useEffect,useRef,useState,type RefObject} from 'react'
import {msg} from '../i18n'
import {Button} from '../ui/Button'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Disclosure} from '../ui/Disclosure'
import {Notice} from '../ui/Notice'
import {dialectAPI,type PreparationReply} from './client'
import type {ResearchRun,ResearchSummary} from './research'
import styles from './DialectsView.module.css'
const failure=(e:unknown)=>e instanceof Error?e.message:msg('The request could not be completed.')
export function ResearchReview({saved,opener,onClose,onStarted,recordIds,authorization}:{authorization?:{chatId:string;onAuthorized:()=>void};recordIds?:string[];saved:PreparationReply;opener:RefObject<HTMLButtonElement|null>;onClose:()=>void;onStarted:(run:ResearchRun)=>void}){
 const [run,setRun]=useState<ResearchRun>(),[error,setError]=useState(''),[busy,setBusy]=useState(false),started=useRef(false),inFlight=useRef(false),[minutes,setMinutes]=useState('60')
 const editing=useDialectRecipeEditing(saved.document.id)
 useEffect(()=>{if(started.current)return;started.current=true;void dialectAPI<ResearchRun>('/'+saved.document.id+'/research','POST',recordIds?{recordIds}:{},saved.sha256).then(setRun).catch(e=>setError(failure(e)))},[saved,recordIds])
 const confirm=async()=>{if(!run||inFlight.current||editing)return;inFlight.current=true;setBusy(true);setError('');try{
  if(authorization){await dialectAPI('/'+saved.document.id+'/research/'+run.id+'/authorize','POST',{chatId:authorization.chatId,minutes:Number(minutes)},saved.sha256);authorization.onAuthorized()}
  else{const accepted=await dialectAPI<ResearchRun>('/'+saved.document.id+'/research/'+run.id+'/confirm','POST',{});onStarted(accepted)}
  onClose()
 }catch(e){setError(failure(e))}finally{inFlight.current=false;setBusy(false)}}
 return <Dialog open onOpenChange={next=>{if(!next&&!busy)onClose()}} title={authorization?msg('Allow automatic research'):recordIds?msg('Reprocess {{count}} records',{count:recordIds.length}):msg('Review research')} openerRef={opener} footer={<DialogActions><Button disabled={busy} onClick={onClose}>{msg('Cancel')}</Button><Button variant="primary" disabled={!run||busy||editing||!!error} onClick={()=>void confirm()}>{busy?authorization?msg('Allowing…'):msg('Starting research…'):authorization?msg('Allow one run'):recordIds?msg('Reprocess {{count}} records',{count:recordIds.length}):msg('Start research')}</Button></DialogActions>}><div className={`${styles.stack} ${authorization||recordIds?styles.approvalReview:''}`}>{!run&&!error&&<LoadingState>{msg('Loading…')}</LoadingState>}{run&&<>{authorization&&<><p>{saved.document.name} · {msg('Recipe version {{version}}',{version:run.recipeVersion})}</p><p className={styles.note}>{msg('Let this chat start one research run using the saved recipe below. Enabling Auto does not start research.')}</p></>}{!authorization&&<p>{recordIds?msg('Update only the selected records using their saved evidence. No new searches or website reads.'):msg('Find records using the saved recipe and selected connection.')}</p>}{recordIds&&<><Disclosure title={msg('Selected records · {{count}}',{count:run.selection?.length??recordIds.length})}><ul>{run.selection?.map(row=><li key={row.recordId}>{row.entity.name}</li>)}</ul></Disclosure><p className={styles.note}>{msg('Current saved recipe · Version {{version}}',{version:run.recipeVersion})}</p><p className={styles.note}>{msg('Changed records will need review again. Saved evidence keeps its original collection date.')}</p></>}<dl className={`${styles.detail} ${recordIds?styles.reprocessDetails:''}`}>{!recordIds&&<><dt>{msg('Find')}</dt><dd>{run.recipe.target==='records'?msg('Records'):run.recipe.target==='companies'?msg('Companies'):run.recipe.target==='people'?msg('People'):msg('People at selected companies')}</dd></>}{!authorization&&!recordIds&&<><dt>{msg('Instructions')}</dt><dd>{run.intent}</dd></>}<ResearchConnectionDetails run={run}/><ResearchExecutionDetails execution={run.execution}/><dt>{msg('Model')}</dt><dd>{run.recipe.ai.model}</dd><dt>{recordIds?msg('Maximum AI requests'):msg('Limits')}</dt><dd>{recordIds?run.recipe.limits.modelCalls:msg('Up to {{records}} records, {{searches}} searches, {{pages}} page reads and {{models}} model requests.',{records:run.recipe.limits.records,searches:run.recipe.limits.searches,pages:run.recipe.limits.pages,models:run.recipe.limits.modelCalls})}</dd>{authorization&&<><dt>{msg('Automatic retries')}</dt><dd>{run.recipe.retries?.mode==='temporary'?msg('Temporary failures · Retry limit: {{count}}',{count:run.recipe.retries.maxRetries}):msg('Off')}</dd></>}</dl>{authorization&&<Disclosure title={msg('Instructions and fields')}><p className={styles.note}>{run.intent}</p><p className={styles.note}>{run.recipe.fields.map(field=>field.name).join(' · ')}</p><ResearchRetryReview policy={run.recipe.retries}/></Disclosure>}{!authorization&&!recordIds&&<p className={styles.note}>{msg('The saved limits take precedence over counts and request budgets in the instructions. Fewer records may be saved when evidence or request budgets run out.')}</p>}{!recordIds&&!authorization&&<ResearchRetryReview policy={run.recipe.retries}/>}{!authorization&&!recordIds&&<p className={styles.note}>{msg('If an individual source cannot be used, research may continue with other evidence within these limits. Missing information stays explicit.')}</p>}<p className={styles.note}>{msg('Provider usage may incur charges under your existing accounts.')}</p>{!authorization&&!recordIds&&<p className={styles.note}>{msg('Qualification is separate. Research does not evaluate records or change decision rules.')}</p>}</>}{authorization&&<><Field label={msg('Must start within')}>{props=><Select {...props} disabled={busy} value={minutes} onValueChange={setMinutes} options={[{value:'15',label:msg('15 minutes')},{value:'60',label:msg('1 hour')}]}/>}</Field><p className={styles.note}>{msg('This allowance applies to this chat in this signed-in window. It ends after one start, expiry, sign-out or a Desk restart. Once started, the run keeps its approved limits.')}</p><p className={styles.note}>{msg('Recipe changes, evaluation and additional usage still require review.')}</p></>}{editing&&<Notice tone="warning">{msg('Save or discard Recipe edits first.')}</Notice>}{error&&<Notice tone="error">{error}</Notice>}</div></Dialog>
}
export function ResearchProgress({run,onStop,busy,onSteps,preparationId,editsPending=false}:{preparationId?:string;editsPending?:boolean;run?:ResearchSummary;onStop:()=>void;busy:boolean;onSteps?:(opener:HTMLButtonElement)=>void}){
 if(!run)return null
 const live=run.state==='running'||run.state==='queued'
 const pending=live&&!run.stopRequested?run.pendingRetry:undefined
 const status=live&&run.stopRequested?msg('Stopping research…'):live&&(run.activePageReads??0)>1?msg('Reading {{count}} websites…',{count:run.activePageReads!}):live&&run.activePageReads===1?researchOperationLabel('page',true):live&&run.currentStep?.state!=='waiting'&&(run.phase==='save'||run.phase==='finalize')?msg('Saving research records…'):run.currentStep&&live?run.currentStep.state==='waiting'?msg('Waiting for AI connection…'):researchOperationLabel(run.currentStep.kind,true):researchRunLabel(run)
 const attention=run.state==='interrupted'||run.state==='blocked'||run.state==='partial'||run.state==='completed-with-gaps'||run.state==='paused'
 const label=run.state==='paused'?msg('Approval needed'):status
 return <div className={styles.researchProgress} data-attention={attention||undefined}>
  <RunStatus running={live} className={styles.researchState}><span>{pending?(pending.problemCode==='search-timeout'?msg('Search timed out'):pending.problemCode==='search-temporary'?msg('Search temporarily unavailable'):msg('Search rate limited')):label} · {researchOutcomeLabel(run)}</span></RunStatus>
  <div className={styles.researchControls}>
   {pending&&<ResearchRetryCountdown pending={pending}/>}
   {!pending&&live&&run.currentStep?.number&&run.currentStep.number>1&&<span className={styles.note}>{msg('Attempt {{number}} of {{total}}',{number:run.currentStep.number,total:run.currentStep.maxAttempts??run.currentStep.number})}</span>}
   {live&&run.currentStep?.startedAt&&<ResearchElapsed start={run.currentStep.startedAt} running/>}
   {onSteps&&<Button variant="quiet" onClick={event=>onSteps(event.currentTarget)}>{msg('View steps')}</Button>}
   {live&&<Button variant="quiet" disabled={busy||run.stopRequested} onClick={onStop}>{busy||run.stopRequested?msg('Stopping…'):msg('Stop')}</Button>}
   {!live&&run.canIncrease&&preparationId&&<ResearchBudgetActions preparationId={preparationId} run={run} disabled={editsPending||busy}/>}
   {run.recovery==='google-grounding'&&<ResearchWebRecovery compact={!!onSteps}/>}
  </div>
  {!onSteps&&run.problem&&run.recovery!=='google-grounding'&&<p className={`${styles.note} ${styles.researchFailure}`}>{run.problem}</p>}
 </div>
}

export {ResearchRecordDetails} from './ResearchRecordDetails'

function ResearchConnectionDetails({run}:{run:ResearchRun}){
 const effective=useEffectiveConfig(),search=useSearchConnections()
 const ai=effective.aiConnections?.data?.connections.find(item=>item.id===run.recipe.ai.connection)
 const source=search.data?.connections.find(item=>item.id===run.recipe.search?.connection)
 return <><dt>{msg('AI connection')}</dt><dd>{ai?.name??run.recipe.ai.connection}</dd>{run.recipe.search&&<><dt>{msg('Web search')}</dt><dd>{source?.name??run.recipe.search.connection}</dd></>}</>
}
