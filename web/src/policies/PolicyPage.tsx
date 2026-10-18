import {useRef,useState} from 'react'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {useNavigate,useParams} from 'react-router-dom'
import {msg,systemMessage,useLocale} from '../i18n'
import {Button} from '../ui/Button'
import {Field,FieldGroup} from '../ui/Field'
import {Input} from '../ui/Input'
import {TextArea} from '../ui/TextArea'
import {PageBody,PageHeader} from '../ui/PageLayout'
import {SettingsSection,SettingsStack} from '../ui/SettingsSection'
import {Tabs} from '../ui/Tabs'
import {Dialog,DialogActions} from '../ui/Dialog'
import {LoadingState} from '../ui/LoadingState'
import {QueryStatusNotice} from '../shell/QueryStatusNotice'
import {useConfirmDiscard,useRegisteredChanges} from '../shell/UnsavedChanges'
import {PolicyHistory} from './PolicyHistory'
import {POLICIES_KEY,PolicyRequestError,policyHref,policyQuery,readPolicy,recoverPolicyOperation,writePolicy,type PolicyReply,type PolicyWrite,type PolicyMutationReply} from './client'
import styles from './PolicyPage.module.css'

type Buffer={title:string;narrative:string;artifactVersion:string}
const blank:Buffer={title:'',narrative:'',artifactVersion:'0.1.0'}
const fields=(reply:PolicyReply):Buffer=>({title:reply.document.candidate.title,narrative:reply.document.candidate.narrative,artifactVersion:reply.document.candidate.artifactVersion})
const equal=(a:Buffer,b:Buffer)=>a.title===b.title&&a.narrative===b.narrative&&a.artifactVersion===b.artifactVersion
export function reviewStateLabel(state:string){return state==='approved'?msg('Approved'):state==='changes-requested'?msg('Changes requested'):msg('Unreviewed')}
export function PolicyPage(){
 const {policyId}=useParams()
 return <article className="detail" data-layout="page" data-measure="full">{policyId?<ExistingPolicy key={policyId} id={policyId}/>:<PolicyEditor key="new"/>}</article>
}
function ExistingPolicy({id}:{id:string}){
 const query=useQuery(policyQuery(id))
 return <><QueryStatusNotice id={'policy-read:'+id} label={msg('Policy unavailable')} error={query.error} retrying={query.isFetching} onRetry={()=>query.refetch()}/>{query.data?<PolicyEditor initial={query.data} observed={query.data}/>:<><PageHeader title={msg('Library')} titleHref="/library?type=policy" context={msg('Policy')}/><PageBody loading={query.isPending?msg('Loading policy…'):undefined}>{query.error&&<p>{msg('Policy unavailable')}</p>}</PageBody></>}</>
}
/** Freeze the editor base; privateQuery refreshes never replace a local buffer. */
export function PolicyEditor({initial,observed}:{initial?:PolicyReply;observed?:PolicyReply}){
 useLocale()
 const [saved,setSaved]=useState(initial),[value,setValue]=useState<Buffer>(()=>initial?fields(initial):blank)
 const [busy,setBusy]=useState(false),[error,setError]=useState<PolicyRequestError>(),[pending,setPending]=useState<{operation:PolicyWrite;submitted:Buffer;result?:PolicyMutationReply}>()
 const [latest,setLatest]=useState<PolicyReply>(),[comment,setComment]=useState(''),[publishOpen,setPublishOpen]=useState(false),[tab,setTab]=useState('draft'),[notice,setNotice]=useState('')
 const active=useRef(false),cache=useQueryClient(),navigate=useNavigate(),confirm=useConfirmDiscard()
 const dirty=!equal(value,saved?fields(saved):blank)
 const observedLatest=latest??(observed&&saved&&observed.revision!==saved.revision?observed:undefined)
 const held=!!pending,disabled=busy||held
 const save=async()=>{
  if(active.current||held)throw new PolicyRequestError('delivery-unknown')
  const operation:PolicyWrite={path:saved?'/'+encodeURIComponent(saved.document.id):'',method:saved?'PUT':'POST',body:{operationId:crypto.randomUUID(),...value,workflow:saved?.document.workflow??{independentReviewRequired:false},...(saved?{baseRevision:saved.revision}:{})}}
  await execute({operation,submitted:{...value}})
 }
 const clear=useRegisteredChanges(dirty||held||!!comment,msg('Leaving will discard unsaved policy changes.'),{label:msg('Policy'),name:value.title,busy,saveFailed:!!error,saveDraft:!held&&!comment&&!observedLatest?save:undefined})
 const updateCache=(reply:PolicyReply)=>{cache.setQueryData([...POLICIES_KEY,reply.document.id],reply);void cache.invalidateQueries({queryKey:POLICIES_KEY,exact:true})}
 async function execute(work:{operation:PolicyWrite;submitted:Buffer;result?:PolicyMutationReply},recover=false){
  if(active.current)return
  active.current=true;setBusy(true);setError(undefined);setNotice('')
  let result=work.result
  try{
   if(!result)result=recover?await recoverPolicyOperation(String(work.operation.body.operationId)):await writePolicy(work.operation)
   // Keep the exact response available if its follow-up read fails. Recovery
   // must not treat this retained outcome as the current mutable head.
   setPending({...work,result})
   const current=await readPolicy(result.policy.document.id)
   updateCache(current)
   const changedSinceResult=current.revision!==result.policy.revision
   setSaved(changedSinceResult?result.policy:current);setLatest(changedSinceResult?current:undefined);setPending(undefined)
   if(work.operation.path.endsWith('/review'))setComment(heldComment=>heldComment===work.operation.body.comment?'':heldComment)
   if(!changedSinceResult){setValue(fields(current));setNotice(result.delivery==='recovered'?msg('Operation recovered; current policy loaded.'):work.operation.path.endsWith('/publish')?msg('Published'):work.operation.path.endsWith('/review')?reviewStateLabel(current.document.reviewState):msg('Draft saved'))}
   else {setValue(work.submitted);setNotice(msg('The operation completed, then the saved policy changed. Your local text is preserved. Review it before saving again.'))}
   if(!initial&&!changedSinceResult){clear();navigate(policyHref(current.document.id),{replace:true})}
  }catch(cause){
   const failure=cause instanceof PolicyRequestError?cause:new PolicyRequestError('unavailable')
   setError(recover&&!result&&failure.code==='not-found'?new PolicyRequestError('operation-unresolved'):failure)
   if(result||failure.uncertain||recover)setPending({...work,result})
   else if(failure.code==='revision-conflict'&&saved){
    try{const current=await readPolicy(saved.document.id);updateCache(current);setLatest(current)}catch{/* Preserve the buffer and its original revision. */}
   }
   throw failure
  }finally{active.current=false;setBusy(false)}
 }
 const action=async(decision:'approve'|'request-changes'|'publish')=>{
  if(!saved||dirty||disabled||active.current||observedLatest)return
  const document=saved.document
  const operation:PolicyWrite={path:'/'+encodeURIComponent(document.id)+(decision==='publish'?'/publish':'/review'),method:'POST',body:{operationId:crypto.randomUUID(),baseRevision:saved.revision,candidateDigest:document.candidate.digest,workflowDigest:document.workflowDigest,...(decision==='publish'?{reviewId:document.currentReviewId}:{decision,comment})}}
  setPublishOpen(false)
  await execute({operation,submitted:{...value}})
 }
 async function loadLatest(){
  if(!saved||disabled)return
  setBusy(true);setError(undefined)
  try{const current=await readPolicy(saved.document.id);updateCache(current);setLatest(current)}catch(cause){setError(cause instanceof PolicyRequestError?cause:new PolicyRequestError('unavailable'))}finally{setBusy(false)}
 }
 const rebase=()=>{if(!observedLatest)return;setSaved(observedLatest);setLatest(undefined);setNotice(msg('Your text is preserved against the latest saved revision. Save draft to create a new candidate.'))}
 const reload=async()=>{if(!observedLatest||disabled||dirty&&!await confirm(msg('Load the latest saved policy and discard your unsaved changes?'),{name:value.title}))return;setSaved(observedLatest);setValue(fields(observedLatest));setLatest(undefined);setError(undefined);setNotice('')}
 const document=saved?.document,currentPublished=document?.publications.some(publication=>publication.candidateRevisionId===document.candidate.revisionId&&publication.workflowDigest===document.workflowDigest),versionPublished=document?.publications.some(publication=>publication.artifactVersion===document.candidate.artifactVersion)
 const reviewDisabled=disabled||dirty||!!observedLatest||!saved
 const approveDisabled=reviewDisabled||!!document?.workflow.independentReviewRequired||!document?.candidate.title.trim()||!document?.candidate.narrative.trim()||!document?.candidate.artifactVersion.trim()
 const reviewHint=held?msg('Recover the pending operation before continuing.'):observedLatest?msg('Review the latest saved revision before continuing.'):dirty?msg('Save draft before reviewing or publishing.'):!saved?msg('Save this draft to begin review.'):document?.workflow.independentReviewRequired?msg('This workflow requires a different reviewer. Independent review is unavailable in this local installation.'):undefined
 return <>
  <PageHeader variant="title" title={msg('Library')} titleHref="/library?type=policy" context={value.title||msg('New policy')} meta={msg('Policy')} actions={<Button variant="primary" disabled={disabled||!dirty&&!!saved||!!observedLatest} onClick={()=>void save().catch(()=>{})}>{busy?msg('Working…'):msg('Save draft')}</Button>}/>
  <Tabs scrollable variant="page" label={msg('Policy')} value={tab} onValueChange={setTab} fillPanel={tab} tabs={[
   {value:'draft',label:msg('Draft'),panel:<div className={styles.panel}><PageBody width="wide"><SettingsStack>
    <SettingsSection level={2} variant="plain" title={msg('Policy draft')} description={msg('Write or paste policy text in Markdown. Saved policies stay in Library independently of chats.')}>
     <FieldGroup><Field label={msg('Title')}>{wiring=><Input {...wiring} maxLength={500} value={value.title} disabled={disabled} onChange={event=>setValue({...value,title:event.target.value})}/>}</Field>
      <Field label={msg('Artifact version')} hint={<>{msg('Use a version such as 1.0.0. Published versions cannot be overwritten.')} {msg('Publish with a version greater than the latest publication.')}</>}>{wiring=><Input {...wiring} maxLength={256} value={value.artifactVersion} disabled={disabled} onChange={event=>setValue({...value,artifactVersion:event.target.value})}/>}</Field>
      <Field label={msg('Policy text')} hint={msg('Markdown is stored as written. Review the complete saved text before approving.')}>{wiring=><TextArea {...wiring} className={styles.narrative} value={value.narrative} disabled={disabled} onChange={event=>setValue({...value,narrative:event.target.value})}/>}</Field>
     </FieldGroup>
    </SettingsSection>
    {observedLatest&&<SettingsSection level={2} variant="plain" title={msg('Saved policy changed')} description={msg('Your editor has kept its original text. Compare the latest saved draft and choose how to continue.')}>
     <dl className={styles.facts}><dt>{msg('Title')}</dt><dd>{observedLatest.document.candidate.title}</dd><dt>{msg('Artifact version')}</dt><dd>{observedLatest.document.candidate.artifactVersion}</dd></dl><pre className={styles.snapshot}>{observedLatest.document.candidate.narrative}</pre>
     <div className={styles.actions}><Button disabled={disabled} onClick={rebase}>{msg('Keep my text on latest revision')}</Button><Button variant="quiet" disabled={disabled} onClick={()=>void reload()}>{msg('Load latest saved draft')}</Button></div>
    </SettingsSection>}
    <SettingsSection level={2} variant="plain" title={msg('Review')} description={document?.workflow.independentReviewRequired?msg('This workflow requires a different reviewer. Independent review is unavailable in this local installation.'):msg('In this local installation, the same authenticated administrator may author, review and publish. Reviews record that actor and the exact saved candidate.')}>
     <p className={styles.note}>{reviewStateLabel(document?.reviewState??'unreviewed')}</p>
     <Field label={msg('Review comment')} hint={msg('Optional context for your decision.')}>{wiring=><TextArea {...wiring} rows={3} maxLength={8192} value={comment} disabled={disabled||!saved} onChange={event=>setComment(event.target.value)}/>}</Field>
     <div className={styles.actions}><Button disabled={approveDisabled} onClick={()=>void action('approve').catch(()=>{})}>{msg('Approve candidate')}</Button><Button disabled={reviewDisabled} onClick={()=>void action('request-changes').catch(()=>{})}>{msg('Request changes')}</Button></div>
     {reviewHint&&<p className={styles.note}>{reviewHint}</p>}
    </SettingsSection>
    <SettingsSection level={2} variant="plain" title={msg('Publication')} description={msg('Publication retains the exact approved candidate as an immutable local version.')}>
     <p className={styles.note}>{currentPublished?msg('Current candidate published'):msg('Current candidate unpublished')}</p>
     <div className={styles.actions}><Button disabled={approveDisabled||document?.reviewState!=='approved'||versionPublished} onClick={()=>setPublishOpen(true)}>{msg('Publish version')}</Button></div>
     {versionPublished&&!currentPublished&&<p className={styles.note}>{msg('This policy version is already published. Save a new artifact version before publishing again.')}</p>}
     {document&&document.reviewState!=='approved'&&!reviewHint&&<p className={styles.note}>{msg('Approve the exact saved candidate before publishing.')}</p>}
    </SettingsSection>
   </SettingsStack></PageBody></div>},
   {value:'history',label:msg('History'),panel:<div className={styles.panel}><PageBody width="wide">{document?<PolicyHistory document={document}/>:<p>{msg('Save a draft to start its history.')}</p>}</PageBody></div>}
  ]}/>
  <footer className={styles.footer}><span role="status">{busy?<LoadingState inline>{msg('Working…')}</LoadingState>:held?msg('Operation recovery required'):dirty?msg('Unsaved changes'):saved?msg('Draft saved'):msg('New policy')}</span><div className={styles.feedback}>{error&&<p role="alert">{systemMessage(error.message)}</p>}{notice&&<p role="status">{notice}</p>}{held?<Button size="small" disabled={busy} onClick={()=>void execute(pending!,true).catch(()=>{})}>{pending?.result?msg('Reload current policy'):msg('Check operation result')}</Button>:error?.code==='revision-conflict'&&!observedLatest?<Button size="small" disabled={busy} onClick={()=>void loadLatest()}>{msg('Compare latest saved draft')}</Button>:null}</div></footer>
  <Dialog open={publishOpen} onOpenChange={setPublishOpen} title={msg('Publish policy version?')} description={msg('Confirm publication of this exact saved and approved candidate.')} footer={<DialogActions><Button onClick={()=>setPublishOpen(false)}>{msg('Cancel')}</Button><Button variant="primary" disabled={approveDisabled||document?.reviewState!=='approved'} onClick={()=>void action('publish').catch(()=>{})}>{msg('Publish version')}</Button></DialogActions>}>
   <dl className={styles.facts}><dt>{msg('Title')}</dt><dd>{document?.candidate.title}</dd><dt>{msg('Artifact version')}</dt><dd>{document?.candidate.artifactVersion}</dd><dt>{msg('Candidate digest')}</dt><dd className={styles.digest}>{document?.candidate.digest}</dd><dt>{msg('Review')}</dt><dd>{document?.currentReviewId}</dd></dl>
  </Dialog>
 </>
}
