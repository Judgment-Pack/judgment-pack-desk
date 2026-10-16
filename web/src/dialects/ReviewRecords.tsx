import {LoadingState} from '../ui/LoadingState'
import {SubscriptionUpgradeSummary} from './SubscriptionUpgradeSummary'
import {useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {Link} from 'react-router-dom'
import {msg,formatDate} from '../i18n'
import {ResearchRecordDetails} from './ResearchRun'
import {reviewDecisionLabel} from './recordReview'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Disclosure} from '../ui/Disclosure'
import {humanId} from '../packs/logicModel'
import {Field} from '../ui/Field'
import {Select} from '../ui/Select'
import {Notice} from '../ui/Notice'
import {Button} from '../ui/Button'
import {InputLineage} from '../jobs/DialectReview'
import {ReleaseReadiness} from '../jobs/ReleaseReadiness'
import {ReleaseStanding} from '../jobs/ReleaseStanding'
import {jobsAPI,type InputPreview} from '../jobs/client'
import {factFields} from '../packs/test-workspace/model'
import {boundPack} from './qualification'
import {dialectAPI,prepareRecords,type PreparationReply,type DialectRecord,type DialectEvaluation} from './client'
import styles from './DialectsView.module.css'

export function ReviewRecords({saved:current,records,opener,onClose,onEvaluated,subscriptionOnly=false,upgradeSource,onUpgradeInterrupted}:{saved:PreparationReply;records:DialectRecord[];opener:React.RefObject<HTMLButtonElement|null>;onClose:()=>void;onEvaluated?:(evaluation:DialectEvaluation)=>void;subscriptionOnly?:boolean;upgradeSource?:DialectEvaluation;onUpgradeInterrupted?:(evaluation:DialectEvaluation)=>void}) {
 // A refetch must not silently replace what the reader is confirming.
 const [saved]=useState(current),[selection]=useState(records)
 const [selected,setSelected]=useState(selection[0]?.id??''),record=selection.find(r=>r.id===selected),binding=saved.document.recipe.qualification
 const [automatic,setAutomatic]=useState(subscriptionOnly),[mode,setMode]=useState('new'),[includeExisting,setIncludeExisting]=useState(false)
 const [evaluation,setEvaluation]=useState<DialectEvaluation>(),[reviewed,setReviewed]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const stale=current.sha256!==saved.sha256||records.map(r=>r.id).join('|')!==selection.map(r=>r.id).join('|')
 const prepared=useQuery({queryKey:['prepared-records',saved.document.id,saved.sha256,selection.map(r=>r.id)],queryFn:()=>prepareRecords(saved,selection),retry:false,refetchOnWindowFocus:false})
 const row=prepared.data?.rows.find(r=>r.id===selected),issues=prepared.data?.rows.filter(r=>r.issues.length)??[]
 const preview=useQuery({queryKey:['prepared-input-preview',saved.document.id,saved.sha256,selected],enabled:!!row&&!row.issues.length,queryFn:({signal})=>jobsAPI<InputPreview>('inputs/preview',row!.input,undefined,signal),retry:false,refetchOnWindowFocus:false})
 const pack=binding?boundPack(binding):undefined,fields=pack?factFields(pack):[]
 async function check(){setBusy(true);setError('');try{setEvaluation(await dialectAPI<DialectEvaluation>('/'+saved.document.id+'/evaluations','POST',{recordIds:selection.map(r=>r.id),...(upgradeSource?{subscriptionId:upgradeSource.id}:{})},saved.sha256));setReviewed(false)}catch(e){setError((e as Error).message)}finally{setBusy(false)}}
 async function evaluate(){if(!evaluation||!reviewed||stale)return;setBusy(true);setError('');try{const result=await dialectAPI<DialectEvaluation>('/'+saved.document.id+'/evaluations/'+evaluation.id+(upgradeSource?'/subscription-upgrade':'/confirm'),'POST',upgradeSource?{reviewed:true,action:'apply'}:{reviewed:true,...(automatic?{subscription:{mode,includeExisting}}:{})},saved.sha256);onEvaluated?.(result);onClose()}catch(e){setError((e as Error).message);if(upgradeSource)onUpgradeInterrupted?.(evaluation)}finally{setBusy(false)}}
 const canEvaluate=evaluation&&(evaluation.release.tests==='passed'||!evaluation.requireTested&&evaluation.release.tests==='not-run')
 const title=upgradeSource?msg('Review subscription upgrade'):evaluation&&automatic?msg('Create Dialects subscription'):evaluation?msg('Evaluate {{count}} records',{count:selection.length}):msg('Review records')
 return <Dialog open onOpenChange={open=>{if(!open&&!busy)onClose()}} title={title} openerRef={opener} size={evaluation?'default':'wide'} footer={<DialogActions><Button disabled={busy} onClick={()=>{if(evaluation){setEvaluation(undefined);setError('')}else onClose()}}>{evaluation?msg('Back'):msg('Close')}</Button>{evaluation?<Button variant="primary" disabled={busy||stale||!reviewed||!canEvaluate} onClick={()=>void evaluate()}>{busy?msg('Saving…'):upgradeSource?msg('Apply upgrade'):automatic?msg('Save paused'):msg('Evaluate {{count}} records',{count:selection.length})}</Button>:<Button variant="primary" disabled={busy||stale||!prepared.data||!!prepared.error||issues.length>0||!preview.data||!!preview.error} onClick={()=>void check()}>{busy?<LoadingState inline>{msg('Checking release…')}</LoadingState>:msg('Review evaluation')}</Button>}</DialogActions>}><div className={styles.stack}>
  {stale&&<Notice tone="warning">{msg('Dialects changed. Close this dialog and review the selection again.')}</Notice>}
  {error&&<Notice tone="error">{error}</Notice>}
  {evaluation?<>
   <dl className={styles.detail}><dt>{msg('Pack')}</dt><dd>{evaluation.release.title} · {evaluation.release.packVersion}</dd><dt>{msg('Dialects')}</dt><dd>{msg('Recipe {{version}} · {{count}} selected records',{version:evaluation.recipeVersion,count:selection.length})}</dd><dt>{msg('Saved tests')}</dt><dd>{evaluation.release.tests==='passed'?msg('Passed'):evaluation.release.tests==='not-run'?msg('Not run'):msg('Needs attention')}</dd><dt>{msg('Project reviewed-set requirement')}</dt><dd>{evaluation.projectReviewed?msg('On'):msg('Off')}</dd></dl>
   {!subscriptionOnly&&!upgradeSource&&<label className="checkbox"><input type="checkbox" checked={automatic} disabled={busy} onChange={e=>{setAutomatic(e.target.checked);setReviewed(false)}}/>{msg('Connect these Dialects to an incremental job')}</label>}
   {upgradeSource?<SubscriptionUpgradeSummary evaluation={evaluation}/>:automatic?<>
    <Field label={msg('Records to process')}>{props=><Select {...props} disabled={busy} value={mode} onValueChange={value=>{setMode(value);setReviewed(false)}} options={[{value:'new',label:msg('New records only')},{value:'changed',label:msg('New and changed records')}]}/>}</Field>
    <label className="checkbox"><input type="checkbox" checked={includeExisting} disabled={busy} onChange={e=>{setIncludeExisting(e.target.checked);setReviewed(false)}}/>{msg('Include existing eligible records when enabled')}</label>
    <p className={styles.note}>{msg('Uses this collection, saved dialect and record origin. AI research requires a current accepted review. Save paused, then review and enable from Jobs.')}</p>
   </>:<p className={styles.note}>{msg('Creates one job with one run per record. Results and input history are saved in Jobs.')}</p>}
   {evaluation.requireTested&&evaluation.release.tests==='not-run'&&<Notice tone="warning">{msg('Saved tests are required before evaluation.')} <Link to={'/packs/'+encodeURIComponent(binding!.key)+'/matrix'}>{msg('Open tests')}</Link></Notice>}
   <Disclosure title={msg('Review details')}><div className={styles.stack}><ReleaseReadiness release={evaluation.release}/><ReleaseStanding release={evaluation.release} packId={binding!.key}/><p className={styles.note}>{msg('Sample decision · Rehearsal only')}: {evaluation.release.preview.disposition.outcomeId??evaluation.release.preview.disposition.kind}</p></div></Disclosure>
   <label className="checkbox"><input type="checkbox" checked={reviewed} disabled={busy||stale||!canEvaluate} onChange={e=>setReviewed(e.target.checked)}/>{msg('I reviewed the selected inputs and this release’s test status.')}</label>
  </>:<>
   <div className={styles.reviewSummary}><p className={styles.note}>{prepared.isPending?<LoadingState inline>{msg('Checking inputs…')}</LoadingState>:issues.length?msg('{{count}} selected · {{issues}} conversion issues',{count:selection.length,issues:issues.length}):msg('{{count}} selected · {{ready}} ready',{count:selection.length,ready:prepared.data?.rows.length??0})}</p>{issues.length>0&&<Button variant="quiet" onClick={()=>setSelected(issues[0].id)}>{msg('Review issues')}</Button>}</div>
   <Field label={msg('Record')}>{props=><Select {...props} disabled={busy} value={selected} options={selection.map(r=>({value:r.id,label:r.name}))} onValueChange={setSelected}/>}</Field>
   {binding&&pack&&<p className={styles.note}>{msg('Saved pack snapshot')}: {pack.title??binding.key} · {pack.version}</p>}
   <p className={styles.note}>{msg('Original values are kept. Unknown inputs may lead to an unresolved decision. No operational run has started.')}</p>
   {prepared.isPending?<LoadingState>{msg('Checking inputs…')}</LoadingState>:prepared.error?<Notice tone="error" actions={<Button onClick={()=>void prepared.refetch()}>{msg('Retry')}</Button>}>{prepared.error.message}</Notice>:row&&<>
    {row.issues.length>0&&<Notice tone="warning">{msg('Resolve conversion issues in Recipe or explicitly change your selection. No records are skipped automatically.')}<ul>{row.issues.filter(issue=>!fields.some(f=>f.path===issue.target)).map(issue=><li key={issue.target}>{humanId(issue.target,issue.target)}: {issue.message}</li>)}</ul></Notice>}
    <div className={`${styles.tableWrap} ${styles.reviewTable}`} tabIndex={0} role="region" aria-label={msg('Original and prepared values')}><table className={styles.table}><thead><tr><th>{msg('Dialect')}</th><th>{msg('Original')}</th><th>{msg('Used for evaluation')}</th><th>{msg('Origin')}</th><th>{msg('State')}</th></tr></thead><tbody>{fields.map(field=>{
     const mapping=binding?.mapping.case?.facts?.find(f=>f.target===field.path),column=mapping?.source.slice('/values/'.length).replace(/~1/g,'/').replace(/~0/g,'~'),original=column===undefined?undefined:row.original[column],value=row.prepared[field.path],issue=row.issues.find(i=>i.target===field.path),type=Array.isArray(value)?'array':typeof value
     const state=issue?issue.message:value===undefined?msg('Unknown'):field.type!=='json'&&field.type!==type?msg('Type mismatch'):msg('Ready')
     return <tr key={field.path}><td>{field.label}</td><td>{original===undefined?'—':JSON.stringify(original)}</td><td>{value===undefined?'—':JSON.stringify(value)}</td><td>{column===undefined?'—':row.origin==='generated'?msg('AI research'):msg('Supplied')}</td><td className={issue||state===msg('Type mismatch')?styles.problem:undefined}>{state}</td></tr>
    })}</tbody></table></div>
    {!row.issues.length&&(preview.isPending?<LoadingState>{msg('Checking inputs…')}</LoadingState>:preview.error?<Notice tone="error" actions={<Button onClick={()=>void preview.refetch()}>{msg('Retry')}</Button>}>{preview.error.message}</Notice>:preview.data&&<>
     {Boolean(pack?.evidenceRequirements?.length)&&<Disclosure title={msg('Evidence availability')}><div className={styles.tableWrap}><table className={styles.table}><thead><tr><th>{msg('Evidence')}</th><th>{msg('State')}</th></tr></thead><tbody>{pack!.evidenceRequirements!.map(e=><tr key={e.id}><td>{humanId(e.id,e.id)}</td><td>{preview.data!.input.evidence?.[e.id]==='present'?msg('Present'):preview.data!.input.evidence?.[e.id]==='absent'?msg('Absent'):msg('Unknown')}</td></tr>)}</tbody></table></div></Disclosure>}
     <Disclosure title={msg('Sources and review history')}>{record?.class==='generated'?<><ResearchRecordDetails record={record} preparationId={saved.document.id}/>{(saved.document.reviews??[]).filter(event=>event.review.recordId===record.id).slice().reverse().map((event,i)=><div key={event.review.at+i}><p>{reviewDecisionLabel(event.review.decision)}</p><p className={styles.note}>{event.review.reviewer.name||event.review.reviewer.subject||msg('Local access')} · {formatDate(new Date(event.review.at),{dateStyle:'medium',timeStyle:'short'})} · {msg('Recipe version {{version}}',{version:event.review.recipeVersion})}</p>{event.review.note&&<p className={styles.reviewNote}>{event.review.note}</p>}</div>)}</>:<p className={styles.note}>{msg('Source: {{source}} · Row {{row}}',{source:saved.document.imports.find(s=>s.id===record?.sourceId)?.name??msg('Supplied URLs'),row:record?.row??0})}</p>}{preview.data.input.preparation&&<InputLineage preparation={preview.data.input.preparation}/>}</Disclosure>
    </>)}
   </>}
  </>}
 </div></Dialog>
}
