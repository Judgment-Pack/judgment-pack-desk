import {useEffect,useRef,useState} from 'react'
import {useQuery,useQueryClient,type UseQueryResult} from '@tanstack/react-query'
import {msg,systemMessage,useLocale} from '../i18n'
import {useConnectionsPane} from '../connections/ConnectionPaneContext'
import {readDocumentRecord,usablePages,needsPartialConsent} from '../documents/record'
import {Button} from '../ui/Button'
import {Field} from '../ui/Field'
import {Select} from '../ui/Select'
import {TextArea} from '../ui/TextArea'
import {Dialog,DialogActions} from '../ui/Dialog'
import {SettingsSection,SettingsStack} from '../ui/SettingsSection'
import {LoadingState} from '../ui/LoadingState'
import {CollectionTable} from '../ui/SelectionTable'
import {QueryStatusNotice} from '../shell/QueryStatusNotice'
import {useRegisteredChanges} from '../shell/UnsavedChanges'
import {POLICIES_KEY,PolicyRequestError,writePolicy,recoverPolicyOperation,readPolicyObservation,downloadPolicyObservation,type PolicyReply,type PolicySourceSync,type PolicySourceObservation,type PolicyWrite,type PolicyMutationReply} from './client'
import styles from './PolicyPage.module.css'
import {usePolicyPendingJournal} from './pendingOperation'
import {PolicyComparison,type DiscussPolicyComparison} from './PolicyComparison'
import type {PolicyComparisonRequest} from './comparison'

export function previousSourceObservation(observations:PolicySourceObservation[],observation:PolicySourceObservation){
 const index=observations.findIndex(item=>item.id===observation.id)
 if(index<1)return undefined
 return observations.slice(0,index).reverse().find(item=>item.bindingId===observation.bindingId&&item.source.provider===observation.source.provider&&item.source.accountId===observation.source.accountId&&item.source.resourceId===observation.source.resourceId)
}
function sourceResultMatches(result:PolicyMutationReply,operation:PolicyWrite,id:string){const receipt=result.receipt as {operationId?:unknown;kind?:unknown;policyId?:unknown}|null;return result.policy.document.id===id&&!!receipt&&receipt.operationId===operation.body.operationId&&receipt.kind==='source-'+operation.body.action&&receipt.policyId===id&&!!result.sourceSync}
const stateLabel=(state:string)=>state==='active'?msg('Automatic checks active'):state==='binding'?msg('Connecting source…'):state==='paused'?msg('Source checks paused'):state==='revoked'?msg('Source access revoked'):msg('Source authorization ended')
export function PolicyConnectedSource({policy,query,editing,disabled,onPendingChange,onDiscuss,newerPolicyState=false}:{newerPolicyState?:boolean;onDiscuss?:DiscussPolicyComparison;policy:PolicyReply;query:UseQueryResult<PolicySourceSync,Error>;editing:boolean;disabled:boolean;onPendingChange?:(held:boolean)=>void}){
 const locale=useLocale(),cache=useQueryClient(),connections=useConnectionsPane(),active=useRef(false)
 const journal=usePolicyPendingJournal('source-sync',policy.document.id),pending=journal.pending
 const [chosen,setChosen]=useState<{resourceId:string;selectionContext:string;title:string}>(),[frequency,setFrequency]=useState('15'),[busy,setBusy]=useState(false),[error,setError]=useState<PolicyRequestError>(),[selected,setSelected]=useState<string>(),[review,setReview]=useState<{sourceRevision:string;policyRevision:string;observation:PolicySourceObservation;candidate:PolicyReply['document']['candidate'];workflowDigest:string}>(),[comment,setComment]=useState('')
 const [comparison,setComparison]=useState<{request:PolicyComparisonRequest;latestId?:string;candidateId:string}>()
 const data=query.data,consentRequired=data?.recovery?.sourceConsentRequired??policy.recovery?.sourceConsentRequired??false,state=data?.state,id=policy.document.id,latest=state?.observations.at(-1)
 const reviewStale=!!review&&(review.sourceRevision!==data?.revision||review.policyRevision!==policy.revision||review.candidate.digest!==policy.document.candidate.digest||review.workflowDigest!==policy.document.workflowDigest)
 const currentObservation=!!latest&&(latest.bindingId===state?.binding?.bindingId||state?.state==='revoked'||state?.state==='authorization-ended')
 const held=!!pending,locked=disabled||busy||held||!journal.ready||query.isError
 useEffect(()=>{onPendingChange?.(busy||held);return()=>onPendingChange?.(false)},[busy,held,onPendingChange])
 useRegisteredChanges(held||!!comment,msg('Leaving will discard unsaved policy changes.'),{label:msg('Policy source'),busy,kind:'edit'})
 const refresh=async()=>{await cache.invalidateQueries({queryKey:[...POLICIES_KEY,id,'source-sync']});await cache.invalidateQueries({queryKey:[...POLICIES_KEY,id,'source-status']});await cache.invalidateQueries({queryKey:[...POLICIES_KEY,id],exact:true});await cache.invalidateQueries({queryKey:POLICIES_KEY,exact:true})}
 async function act(action:string,extra:Record<string,unknown>={}){
  if(active.current||!data||!journal.ready)return
  active.current=true;setBusy(true);setError(undefined)
  const operationId=crypto.randomUUID(),operation:PolicyWrite={path:'/'+encodeURIComponent(id)+'/source-sync',method:'POST',body:{operationId,action,baseRevision:policy.revision,baseSourceRevision:data.revision,...extra}}
  let dispatched=false,committed=false
  try{journal.store(operation);dispatched=true;const result=await writePolicy(operation);if(!sourceResultMatches(result,operation,id))throw new PolicyRequestError('delivery-unknown',true);committed=true;journal.clear();setChosen(undefined);setReview(undefined);setComment('');await refresh()}
  catch(cause){const failure=cause instanceof PolicyRequestError?cause:new PolicyRequestError('unavailable');setError(failure);if(!(dispatched&&(failure.uncertain||committed))){journal.clear();await refresh()}}
  finally{active.current=false;setBusy(false)}
 }
 async function recover(){if(!pending||active.current)return;active.current=true;setBusy(true);setError(undefined);try{const result=await recoverPolicyOperation(String(pending.body.operationId));if(!sourceResultMatches(result,pending,id))throw new PolicyRequestError('delivery-unknown',true);journal.clear();setChosen(undefined);setReview(undefined);setComment('');await refresh()}catch(cause){setError(cause instanceof PolicyRequestError?cause:new PolicyRequestError('unavailable'))}finally{active.current=false;setBusy(false)}}
 function choose(opener:HTMLElement){connections.open({provider:'google-drive',purpose:'policy-source',opener,selection:{limit:1,onSelect:async(items,_signal,metadata)=>{if(items.length!==1||!metadata)throw new Error(msg('Choose one source again.'));setChosen({resourceId:items[0]!.resourceId,selectionContext:metadata.selectionContext,title:metadata.title})}}})}
 const date=(value?:string)=>value?new Date(value).toLocaleString(locale):msg('Never')
 return <SettingsStack>
  <QueryStatusNotice id={'policy-source:'+id} label={msg('Policy source unavailable')} error={query.error} retrying={query.isFetching} onRetry={()=>query.refetch()}/>
  {query.isPending?<LoadingState>{msg('Loading policy source…')}</LoadingState>:<>
   <SettingsSection level={2} variant="plain" title={msg('Connected policy source')} description={msg('Google Drive checks retain source snapshots and open updates for review. Your draft, approvals, publications and jobs keep their existing content.')}>
    <div className={styles.sourceDetails}>
    {!data?.supported&&<p role="status">{msg('The installed Gateway does not support automatic policy source checks.')}</p>}
    {state&&<><p role="status">{consentRequired?msg('Historical source authorization · Fresh consent required'):stateLabel(state.state)}</p><dl className={styles.facts}><dt>{msg('File')}</dt><dd>{latest?.name??state.grant.resourceId}</dd><dt>{msg('Check frequency')}</dt><dd>{msg('Every {{minutes}} minutes',{minutes:state.frequencyMinutes})}</dd><dt>{msg('Last successful check')}</dt><dd>{date(state.lastSuccess)}</dd><dt>{msg('Last attempted check')}</dt><dd>{date(state.lastAttempt)}</dd><dt>{msg('Next check')}</dt><dd>{state.state==='active'||state.state==='binding'?date(state.nextCheck):msg('Paused')}</dd><dt>{msg('Source lifecycle')}</dt><dd>{msg('Snapshot only; provider approval and withdrawal are unavailable.')}</dd></dl>
     {state.gap&&<p role="status">{msg('There is a gap in source freshness. The last retained snapshot remains available.')}</p>}
     {state.errorCode&&<p role="alert">{msg('The source check did not complete. Review the connection and try again.')} <code>{state.errorCode}</code></p>}
     <div className={styles.actions}>{!consentRequired&&(state.state==='active'||state.state==='binding')&&<><Button disabled={locked} onClick={()=>void act('check-now')}>{msg('Check now')}</Button><Button disabled={locked} onClick={()=>void act('pause')}>{msg('Pause checks')}</Button></>}{!consentRequired&&state.state==='paused'&&<Button disabled={locked||!data?.supported} onClick={()=>void act('resume')}>{msg('Resume checks')}</Button>}{!consentRequired&&state.state!=='revoked'&&<Button disabled={locked} onClick={()=>void act('revoke')}>{msg('Revoke source access')}</Button>}</div>
    </>}
    {(consentRequired||!state||state.state==='revoked'||state.state==='authorization-ended')&&<Button disabled={locked||!data?.supported} onClick={event=>choose(event.currentTarget)}>{msg('Choose Google Drive source')}</Button>}
    {consentRequired&&<p role="status">{msg('Retained source snapshots remain readable as imported history. Choosing a source records new consent; keeping the current candidate is only a local review of retained text.')}</p>}
    {busy&&<LoadingState inline>{msg('Working…')}</LoadingState>}{error&&<p role="alert">{systemMessage(error.message)}</p>}{held&&<Button disabled={busy} onClick={()=>void recover()}>{msg('Check operation result')}</Button>}
    </div>
   </SettingsSection>
   {state&&<SettingsSection level={2} variant="plain" title={msg('Source updates')} description={msg('Review the latest retained source against the saved candidate. Keeping the candidate records this exact source update, candidate and workflow; future changes require another decision.')}>
    <div className={styles.sourceGroups}>
    {!currentObservation&&<p>{msg('Waiting for the first retained source snapshot.')}</p>}
    {data?.pending&&<p role="status">{msg('Resolve the latest source update before publication.')}</p>}
    {!!state.observations.length?<><div className={styles.tableScroll}><CollectionTable className={styles.table} aria-label={msg('Source updates')}><thead><tr><th scope="col">{msg('File')}</th><th scope="col">{msg('Source revision')}</th><th scope="col">{msg('Time')}</th><th scope="col">{msg('Details')}</th></tr></thead><tbody>{[...state.observations].reverse().map(item=><tr key={item.id}><th scope="row">{item.name}</th><td className={styles.digest}>{item.sourceRevision}</td><td>{date(item.observedAt)}</td><td><Button size="small" variant="quiet" onClick={()=>setSelected(item.id)}>{msg('Inspect')}</Button>{previousSourceObservation(state.observations,item)&&<Button size="small" variant="quiet" onClick={()=>setComparison({request:{before:{kind:'observation',observationId:previousSourceObservation(state.observations,item)!.id},after:{kind:'observation',observationId:item.id}},latestId:latest?.id,candidateId:policy.document.candidate.revisionId})}>{msg('Compare previous source')}</Button>}<Button size="small" variant="quiet" onClick={()=>setComparison({request:{before:{kind:'observation',observationId:item.id},after:{kind:'revision',revisionId:policy.document.candidate.revisionId}},latestId:latest?.id,candidateId:policy.document.candidate.revisionId})}>{msg('Compare saved candidate')}</Button></td></tr>)}</tbody></CollectionTable></div>
     {comparison&&<PolicyComparison key={JSON.stringify(comparison)} policyId={id} request={comparison.request} newerState={newerPolicyState||comparison.latestId!==latest?.id||comparison.candidateId!==policy.document.candidate.revisionId} onDiscuss={onDiscuss} onClose={()=>setComparison(undefined)}/>}
     {!previousSourceObservation(state.observations,state.observations.find(item=>item.id===selected)??latest!)&&<p role="status">{msg('No earlier retained snapshot exists for this source binding.')}</p>}
     <SourceObservation id={id} observation={state.observations.find(item=>item.id===selected)??latest!}/>
     <SettingsSection level={3} variant="plain" title={msg('Current saved policy text')}><pre className={styles.snapshot}>{policy.document.candidate.narrative}</pre></SettingsSection>
     <Button disabled={locked||editing||!data?.pending||!currentObservation} onClick={()=>{if(latest&&data){setSelected(latest.id);setReview({sourceRevision:data.revision,policyRevision:policy.revision,observation:latest,candidate:policy.document.candidate,workflowDigest:policy.document.workflowDigest})}}}>{msg('Review keeping current candidate')}</Button>
     {editing&&<p>{msg('Save draft before resolving this source update.')}</p>}
     {state.resolutions.at(-1)&&<p>{msg('Last source review')} · {state.resolutions.at(-1)!.actor.subject} · {date(state.resolutions.at(-1)!.at)}</p>}
    </>:<p>{msg('Waiting for the first retained source snapshot.')}</p>}
    </div>
   </SettingsSection>}
  </>}
  <Dialog open={!!chosen} onOpenChange={open=>{if(!open&&!busy)setChosen(undefined)}} title={msg('Allow automatic policy source checks?')} description={msg('Desk will read this Google Drive file at the chosen frequency while Desk is running. Checks continue after ordinary sign-out and resume after restarting Desk. Pause or revoke them here; changing sign-in settings or disconnecting Drive ends this authorization.')} footer={<DialogActions><Button disabled={busy} onClick={()=>setChosen(undefined)}>{msg('Cancel')}</Button><Button variant="primary" disabled={locked||!chosen} onClick={()=>void act('bind',{...chosen,frequencyMinutes:Number(frequency),consent:'recurring-read-after-sign-out',title:undefined})}>{msg('Allow automatic checks')}</Button></DialogActions>}>
   <div className={styles.sourceDetails}><p>{chosen?.title}</p><Field label={msg('Check frequency')}>{wiring=><Select {...wiring} value={frequency} onValueChange={setFrequency} disabled={busy} options={[5,15,60].map(n=>({value:String(n),label:msg('Every {{minutes}} minutes',{minutes:n})}))}/>}</Field></div>
  </Dialog>
  <Dialog open={!!review} onOpenChange={open=>{if(!open)setReview(undefined)}} title={msg('Keep the current candidate?')} description={msg('Confirm that you reviewed the latest source snapshot and choose to keep the exact saved candidate. This resolves this source update only; it does not approve or publish the policy.')} footer={<DialogActions><Button disabled={busy} onClick={()=>setReview(undefined)}>{msg('Cancel')}</Button><Button variant="primary" disabled={locked||editing||!review||reviewStale} onClick={()=>{if(review&&!reviewStale)void act('keep-current',{baseRevision:review.policyRevision,baseSourceRevision:review.sourceRevision,observationId:review.observation.id,candidateDigest:review.candidate.digest,workflowDigest:review.workflowDigest,comment})}}>{msg('Keep current candidate')}</Button></DialogActions>}>
   <div className={styles.sourceGroups}>{consentRequired&&<p>{msg('This reviews an imported historical source snapshot locally. It does not contact the provider, restore source consent or establish current source approval.')}</p>}<dl className={styles.facts}><dt>{msg('Source revision')}</dt><dd>{review?.observation.sourceRevision}</dd><dt>{msg('Source digest')}</dt><dd className={styles.digest}>{review?.observation.content.bytesDigest}</dd><dt>{msg('Candidate digest')}</dt><dd className={styles.digest}>{review?.candidate.digest}</dd></dl>{reviewStale&&<p role="alert">{msg('The policy or source changed. Review the latest source state before continuing.')}</p>}{review&&<><PolicyComparison policyId={id} request={{before:{kind:'observation',observationId:review.observation.id},after:{kind:'revision',revisionId:review.candidate.revisionId}}} newerState={newerPolicyState||reviewStale}/><Button size="small" variant="quiet" onClick={()=>void downloadPolicyObservation(id,review.observation).catch(()=>setError(new PolicyRequestError('unavailable')))}>{msg('Download original')}</Button></>}<Field label={msg('Review comment')}>{wiring=><TextArea {...wiring} rows={3} maxLength={8192} value={comment} onChange={event=>setComment(event.target.value)} disabled={busy}/>}</Field></div>
  </Dialog>
 </SettingsStack>
}
function SourceObservation({id,observation}:{id:string;observation:PolicySourceObservation}){
 const [error,setError]=useState(false),query=useQuery({queryKey:[...POLICIES_KEY,id,'source-observation',observation.id],queryFn:async({signal})=>{const result=await readPolicyObservation(id,observation.id,signal);return readDocumentRecord(result.document)},retry:false,staleTime:0,refetchOnMount:'always'})
 return <SettingsSection level={3} variant="plain" title={msg('Retained source snapshot')}>
  <div className={styles.sourceDetails}>
  <QueryStatusNotice id={'source-observation:'+observation.id} label={msg('Policy source unavailable')} error={query.error} retrying={query.isFetching} onRetry={()=>query.refetch()}/>
  <dl className={styles.facts}><dt>{msg('Source digest')}</dt><dd className={styles.digest}>{observation.content.bytesDigest}</dd><dt>{msg('Extraction digest')}</dt><dd className={styles.digest}>{observation.extraction.content.bytesDigest}</dd></dl>
  {query.isPending?<LoadingState>{msg('Loading original…')}</LoadingState>:!query.isError&&query.data&&<>{needsPartialConsent(query.data)&&<p role="status">{msg('The source extraction is incomplete. Review the retained original before deciding.')}</p>}{usablePages(query.data).length?<pre className={styles.snapshot}>{usablePages(query.data).map(page=>page.text).join('\n\n')}</pre>:<p role="status">{msg('No readable source pages are available. This does not mean the source is empty.')}</p>}</>}
  <Button onClick={()=>void downloadPolicyObservation(id,observation).catch(()=>setError(true))}>{msg('Download original')}</Button>{error&&<p role="alert">{msg('Policy source unavailable')}</p>}
  </div>
 </SettingsSection>
}
