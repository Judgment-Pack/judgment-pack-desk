import {useEffect,useRef,useState} from 'react'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {msg,systemMessage,useLocale} from '../i18n'
import {Button} from '../ui/Button'
import {Field} from '../ui/Field'
import {Input} from '../ui/Input'
import {Select} from '../ui/Select'
import {TextArea} from '../ui/TextArea'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Disclosure} from '../ui/Disclosure'
import {SettingsSection,SettingsStack} from '../ui/SettingsSection'
import {LoadingState} from '../ui/LoadingState'
import {QueryStatusNotice} from '../shell/QueryStatusNotice'
import {useConfirmDiscard,useRegisteredChanges} from '../shell/UnsavedChanges'
import {readDocumentRecord,usablePages,needsPartialConsent} from '../documents/record'
import {POLICIES_KEY,PolicyRequestError,policySourceStatusQuery,readPolicySourceStatus,writePolicySourceStatus,recoverPolicySourceStatusOperation,readPolicyObservation,readExternalPolicyVersion,downloadPolicyObservation,type PolicyReply,type PolicyWrite,type PolicySourceObservation,type SourceStatusReply,type SourceStatusCommand,type SourceAuthorityOutcome,type ExternalPolicyVersion} from './client'
import {usePolicyPendingJournal} from './pendingOperation'
import styles from './PolicyPage.module.css'

type Action=SourceStatusCommand['action']
type FallbackStatus='draft'|'published'
type Review={action:Action;snapshot:SourceStatusReply;policyTitle:string;sourceName:string;mode:'desk-managed'|'externally-managed';fallback:FallbackStatus;partial:boolean;allowPartial:boolean;observation?:PolicySourceObservation;draft:{title:string;narrative:string;artifactVersion:string}}
const ownershipLabel=(mode:string)=>mode==='externally-managed'?msg('Externally managed'):msg('Desk-managed')
export const sourceStatusLabel=(status?:string)=>status==='published'?msg('Published'):status==='draft'?msg('Draft'):status==='withdrawn'?msg('Withdrawn'):msg('Unknown source status')
export const sourceBasisLabel=(basis?:string)=>basis==='source-provided'?msg('Source provided'):basis==='admin-configured'?msg('Administrator configured'):msg('Not reviewed')
function actionLabel(action:Action){switch(action){case 'configure':return msg('Review fallback configuration');case 'set-ownership':return msg('Review ownership');case 'apply-external':return msg('Review retaining external version');case 'import-draft':return msg('Review import as Draft')}}
function eventLabel(action:Action){switch(action){case 'configure':return msg('Fallback configured');case 'set-ownership':return msg('Ownership reviewed');case 'apply-external':return msg('External version retained');case 'import-draft':return msg('Desk Draft imported')}}
function availabilityLabel(state:string){switch(state){case 'active':case 'present':case 'available':return msg('Available');case 'revoked':return msg('Source access revoked');case 'authorization-ended':return msg('Source authorization ended');case 'paused':return msg('Source checks paused');case 'deleted':return msg('Source deleted');case 'inaccessible':return msg('Source inaccessible');case 'unbound':return msg('No connected source');case 'binding':return msg('Connecting source…');default:return msg('Source unavailable')}}
function reasonLabel(reason?:string){switch(reason){
 case 'restored-source-consent-required':return msg('Archived source authorization is inactive. Choose the source again and review new consent before any provider access.')
 case 'configuration-required':return msg('Review a source status configuration first.')
 case 'source-unbound':return msg('Connect a policy source first.')
 case 'source-binding':case 'source-no-observation':return msg('Wait for a verified source snapshot before continuing.')
 case 'source-paused':return msg('Resume source checks and obtain a current snapshot before continuing.')
 case 'source-revoked':case 'source-authorization-ended':return msg('Reconnect the source with current administrator consent.')
 case 'source-gap':return msg('There is a gap in source freshness. The last reviewed status remains retained.')
 case 'source-binding-changed':case 'configuration-binding-changed':return msg('The source binding changed. Review its status configuration again.')
 case 'external-ownership-required':return msg('Review external ownership before retaining an external version.')
 case 'already-applied':return msg('This exact source observation and configuration have already been retained.')
 case 'import-draft-required':return msg('Draft status requires an explicit import into a new Desk Draft.')
 case 'unknown-source-status':return msg('The source status is not mapped. Review it before continuing.')
 default:return msg('Current source status is unavailable. The last reviewed status remains retained.')
}}
export function sourceAuthorityResultMatches(result:SourceAuthorityOutcome,operation:PolicyWrite,id:string){
 const receipt=result?.receipt,snapshot=result?.sourceStatus,event=snapshot?.events?.find(item=>item.id===receipt?.authorityEventId),body=operation.body
 if(result?.outcomeKind!=='source-authority'||result.policyId!==id||result.policyRevision!==snapshot?.policyRevision||!receipt||receipt.operationId!==body.operationId||receipt.policyId!==id||receipt.kind!=='authority-'+body.action||receipt.authorityRevision!==snapshot?.revision||!event||event.kind!==body.action)return false
 if(body.action==='set-ownership')return event.managementMode===body.managementMode
 if(body.action==='configure')return event.configuration?.id===receipt.configurationId&&event.configuration?.bindingId===body.bindingId&&event.configuration?.fallbackStatus===body.fallbackStatus
 const observation=event.externalVersion?.observation??event.importedDraft?.observation,configuration=event.externalVersion?.configurationId??event.importedDraft?.configurationId
 return receipt.observationId===body.observationId&&receipt.configurationId===body.configurationId&&observation?.id===body.observationId&&configuration===body.configurationId&&(body.action!=='apply-external'||event.externalVersion?.id===receipt.externalVersionId)
}
export function PolicySourceStatus({policy,active,editing,disabled,onPendingChange,sourceName,onCommitted,onCompareLatest}:{policy:PolicyReply;active:boolean;editing:boolean;disabled:boolean;onPendingChange:(held:boolean)=>void;sourceName?:{bindingId:string;name:string};onCommitted?:(outcome:SourceAuthorityOutcome,action:string)=>Promise<boolean>;onCompareLatest?:()=>void}){
 const locale=useLocale(),cache=useQueryClient(),confirmDiscard=useConfirmDiscard(),running=useRef(false),id=policy.document.id
 const journal=usePolicyPendingJournal('source-status',id),pending=journal.pending
 const query=useQuery(policySourceStatusQuery(id,journal.ready&&(active||!!pending))),data=query.data
 const [review,setReview]=useState<Review>(),[rationale,setRationale]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState<PolicyRequestError>(),[notice,setNotice]=useState('')
 const [inspected,setInspected]=useState<string>()
 const [needsDraftRefresh,setNeedsDraftRefresh]=useState(false),submittedReview=useRef<Review|undefined>(undefined)
 const [refreshFailed,setRefreshFailed]=useState(false)
 const opener=useRef<HTMLButtonElement|null>(null)
 const stale=!!review&&(review.snapshot.revision!==data?.revision||review.snapshot.policyRevision!==data?.policyRevision||review.snapshot.sourceRevision!==data?.sourceRevision||review.snapshot.policyRevision!==policy.revision)
 const locked=disabled||editing||busy||!!pending||!journal.ready||query.isError||refreshFailed||!data||data.policyRevision!==policy.revision
 useEffect(()=>{onPendingChange(busy||!!pending);return()=>onPendingChange(false)},[busy,pending,onPendingChange])
 useRegisteredChanges(!!review||!!pending,msg('Leaving will discard unsaved source configuration changes.'),{label:msg('Policy source status'),busy,kind:'edit'})
 const date=(value?:string)=>value?new Date(value).toLocaleString(locale):msg('Never')
 async function closeReview(){if(busy||pending)return;if(review&&!await confirmDiscard(msg('Discard this source configuration review?')))return;setReview(undefined);setRationale('')}
 async function choose(action:Action,button:HTMLButtonElement){
  if(locked||!data)return
  opener.current=button;setError(undefined);setNotice('')
  const snapshot=structuredClone(data),draft={title:policy.document.candidate.title,narrative:policy.document.candidate.narrative,artifactVersion:policy.document.candidate.artifactVersion}
  let partial=false,observation:PolicySourceObservation|undefined
  if(action==='import-draft'){
   setBusy(true)
   try{
    const reply=await readPolicyObservation(id,snapshot.currentPreview.observationId!),record=readDocumentRecord(reply.document)
    if(reply.observation.id!==snapshot.currentPreview.observationId)throw new PolicyRequestError('authority-conflict')
    const pages=usablePages(record);if(!pages.length)throw new PolicyRequestError('source-status-extraction-unavailable')
    partial=needsPartialConsent(record);observation=reply.observation
    draft.narrative=pages.map(page=>page.text).join('\n\n')
   }catch(cause){setError(cause instanceof PolicyRequestError?cause:new PolicyRequestError('unavailable'));return}finally{setBusy(false)}
  }
  setRationale('');setReview({action,snapshot,policyTitle:policy.document.candidate.title,sourceName:observation?.name??(sourceName?.bindingId===snapshot.binding?.bindingId?sourceName?.name:undefined)??snapshot.binding?.source.resourceId??msg('No connected source'),mode:snapshot.ownership.mode,fallback:snapshot.currentConfig?.fallbackStatus??'draft',partial,allowPartial:false,observation,draft})
 }
 async function refresh(){
  const current=await readPolicySourceStatus(id);cache.setQueryData([...POLICIES_KEY,id,'source-status'],current)
  void cache.invalidateQueries({queryKey:[...POLICIES_KEY,id],exact:true})
  void cache.invalidateQueries({queryKey:[...POLICIES_KEY,id,'source-sync']})
  void cache.invalidateQueries({queryKey:POLICIES_KEY,exact:true})
 }
 async function retryRefresh(){setBusy(true);try{await refresh();setRefreshFailed(false);setError(undefined)}catch{setError(new PolicyRequestError('source-status-refresh-unavailable'))}finally{setBusy(false)}}
 async function deliver(operation:PolicyWrite,recover=false){
  if(running.current)return
  running.current=true;setBusy(true);setError(undefined)
  let dispatched=recover,committed=false
  try{
   if(!recover){journal.store(operation);submittedReview.current=review;setReview(undefined);dispatched=true}
   const result=recover?await recoverPolicySourceStatusOperation(String(operation.body.operationId)):await writePolicySourceStatus(operation)
   if(!sourceAuthorityResultMatches(result,operation,id))throw new PolicyRequestError('delivery-unknown',true)
   committed=true;cache.setQueryData([...POLICIES_KEY,id,'source-status'],result.sourceStatus);journal.clear();setReview(undefined);setRationale('')
   setNotice(operation.body.action==='import-draft'?msg('New Desk Draft saved. Review and publication are separate actions.'):msg('Source decision recorded.'))
   try{await refresh();setRefreshFailed(false)}catch{setRefreshFailed(true);setError(new PolicyRequestError('source-status-refresh-unavailable'))}
   if(onCommitted){try{setNeedsDraftRefresh(!await onCommitted(result,String(operation.body.action)))}catch{setNeedsDraftRefresh(true)}}
  }catch(cause){
   const failure=cause instanceof PolicyRequestError?cause:new PolicyRequestError(dispatched?'delivery-unknown':'unavailable',dispatched)
   setError(recover&&failure.code==='not-found'?new PolicyRequestError('operation-unresolved'):failure)
   if(!(recover||committed||dispatched&&failure.uncertain)){journal.clear();setReview(submittedReview.current);void query.refetch()}
  }finally{running.current=false;setBusy(false)}
 }
 function submit(){
  if(!review||locked||stale||!rationale.trim()||review.partial&&!review.allowPartial)return
  const {snapshot,action}=review,base={operationId:crypto.randomUUID(),baseRevision:snapshot.policyRevision,baseSourceRevision:snapshot.sourceRevision,baseAuthorityRevision:snapshot.revision,rationale:rationale.trim()}
  const body:SourceStatusCommand=action==='configure'?{...base,action,bindingId:snapshot.binding!.bindingId,fallbackStatus:review.fallback}:action==='set-ownership'?{...base,action,managementMode:review.mode}:action==='import-draft'?{...base,action,observationId:snapshot.currentPreview.observationId!,configurationId:snapshot.currentPreview.configurationId!,draft:review.draft}:{...base,action,observationId:snapshot.currentPreview.observationId!,configurationId:snapshot.currentPreview.configurationId!}
  void deliver({path:'/'+encodeURIComponent(id)+'/source-status',method:'POST',body})
 }
 const command=(action:Action,key:keyof SourceStatusReply['actions'])=><Button disabled={locked||!data?.actions[key].allowed} onClick={event=>void choose(action,event.currentTarget)}>{actionLabel(action)}</Button>
 return <SettingsStack>
  <QueryStatusNotice id={'policy-source-status:'+id} label={msg('Policy source status unavailable')} error={query.error} retrying={query.isFetching} onRetry={()=>query.refetch()}/>
  <SettingsSection level={2} variant="plain" title={msg('Ownership and source status')} description={msg('Source status and administrator assertions are separate from local reviews, publications and release schedules.')}>
   <div className={styles.sourceDetails}>
    {!data?query.isPending?<LoadingState>{msg('Loading source status…')}</LoadingState>:<p>{msg('Source status unavailable')}</p>:<>
     <dl className={styles.facts}><dt>{msg('Ownership')}</dt><dd>{ownershipLabel(data.ownership.mode)}{data.ownership.inherited?<p className={styles.note}>{msg('Inherited Desk-managed default. Review a selection to record it explicitly.')}</p>:<p className={styles.note}>{msg('Reviewed')} · {data.ownership.actor?.subject??data.ownership.actor?.kind} · {date(data.ownership.at)}</p>}</dd><dt>{msg('Capability')}</dt><dd>{data.capability.kind==='not-exposed'?msg('Source lifecycle not exposed'):msg('Source capability unavailable')}</dd><dt>{msg('Availability')}</dt><dd>{availabilityLabel(data.availability.state)}</dd>{data.binding&&<><dt>{msg('Resource')}</dt><dd>{data.binding.source.provider} · {data.binding.source.resourceId}</dd></>}<dt>{msg('Last successful check')}</dt><dd>{date(data.availability.lastSuccess)}</dd></dl>
     {command('set-ownership','setOwnership')}
     <p className={styles.note}>{data.currentConfig?msg('Configured fallback: {{status}}',{status:sourceStatusLabel(data.currentConfig.fallbackStatus)}):data.capability.kind==='not-exposed'?msg('Default Draft; no fallback configuration has been reviewed.'):msg('No source status configuration reviewed. Unavailable capability does not activate fallback.')}</p>
     {data.capability.kind==='not-exposed'&&command('configure','configure')}
     {data.currentConfig&&<Disclosure title={msg('Current configuration')}><dl className={styles.facts}><dt>{msg('Configuration')}</dt><dd className={styles.digest}>{data.currentConfig.id}</dd><dt>{msg('Basis')}</dt><dd>{msg('Administrator configured')}</dd><dt>{msg('Reviewed')}</dt><dd>{data.currentConfig.actor.subject??data.currentConfig.actor.kind} · {date(data.currentConfig.at)}</dd></dl></Disclosure>}
     <SettingsSection level={3} variant="plain" title={msg('Current source evaluation')} description={msg('Current evaluation and reviewed external versions are separate records. Configuration changes do not reinterpret earlier reviewed versions.')}>
      <div className={styles.sourceDetails}><p>{data.currentPreview.status?sourceStatusLabel(data.currentPreview.status):msg('No current source status')} · {sourceBasisLabel(data.currentPreview.provenance)}</p>{data.currentPreview.reason&&<p role="status">{reasonLabel(data.currentPreview.reason)}</p>}{data.currentPreview.rawStatus&&<p>{msg('Raw source status')}: {data.currentPreview.rawStatus}</p>}{data.currentPreview.observationId&&<><p className={styles.note}>{msg('Source revision')}: {data.currentPreview.sourceRevision}</p><Disclosure title={msg('Technical details')}><dl className={styles.facts}><dt>{msg('Observation')}</dt><dd className={styles.digest}>{data.currentPreview.observationId}</dd><dt>{msg('Configuration')}</dt><dd className={styles.digest}>{data.currentPreview.configurationId}</dd><dt>{msg('Source digest')}</dt><dd className={styles.digest}>{data.currentPreview.content?.bytesDigest}</dd></dl></Disclosure></>}<div className={styles.actions}>{command('apply-external','applyExternal')}{command('import-draft','importDraft')}</div></div>
     </SettingsSection>
     {data.availability.gap&&<p role="status">{msg('There is a gap in source freshness. The last reviewed status remains retained.')}</p>}
     {!data.availability.usable&&<p role="status">{msg('New source decisions are unavailable. The last reviewed status remains retained.')}</p>}
     <SettingsSection level={3} variant="plain" title={msg('Last reviewed external version')}><div className={styles.sourceDetails}>{data.lastApplied?<>{data.recovery?.currentnessUnknown&&<p className={styles.note}>{msg('Retained source assertions may belong to imported history; recovery does not establish current provider approval.')}</p>}<p>{sourceStatusLabel(data.lastApplied.status)} · {sourceBasisLabel(data.lastApplied.provenance)}</p><p className={styles.note}>{date(data.lastApplied.at)} · {data.lastApplied.observation.name} · {msg('Source revision')}: {data.lastApplied.observation.sourceRevision}</p><Button onClick={()=>setInspected(data.lastApplied!.id)}>{msg('Inspect retained external version')}</Button></>:<p>{msg('No external version retained.')}</p>}</div></SettingsSection>
     <p className={styles.note}>{msg('These source records do not establish verified external approval, governing adoption or permission to execute.')}</p>
    </>}
    {busy&&<LoadingState inline>{msg('Working…')}</LoadingState>}{error&&<p role="alert">{systemMessage(error.message)}</p>}{notice&&<p role="status">{notice}</p>}{refreshFailed&&<Button disabled={busy} onClick={()=>void retryRefresh()}>{msg('Reload source status')}</Button>}{needsDraftRefresh&&onCompareLatest&&<Button disabled={busy} onClick={onCompareLatest}>{msg('Compare latest saved draft')}</Button>}{pending&&<Button disabled={busy} onClick={()=>void deliver(pending,true)}>{msg('Check operation result')}</Button>}
   </div>
  </SettingsSection>
  {data&&<Disclosure title={msg('Source decision history')}><div className={styles.sourceDetails}>{data.events.length?[...data.events].reverse().map(event=><div key={event.id}><p>{eventLabel(event.kind)} · {event.sequence} · {event.actor.subject??event.actor.kind} · {date(event.at)}</p><p>{event.rationale}</p>{event.externalVersion&&<Button variant="quiet" onClick={()=>setInspected(event.externalVersion!.id)}>{msg('Inspect retained external version')}</Button>}</div>):<p>{msg('No source decisions recorded.')}</p>}</div></Disclosure>}
  <Dialog open={!!review} onOpenChange={open=>{if(!open)void closeReview()}} openerRef={opener} title={review?actionLabel(review.action):msg('Review source decision')} description={review?.action==='import-draft'?msg('Replace the saved head with a new Desk Draft from this exact source observation. Earlier draft bytes, reviews, publications and releases remain history. Review and publication are separate actions.'):review?.action==='apply-external'?msg('Retain this exact source observation and configuration as an external version. This creates no local approval or publication.'):review?.action==='set-ownership'?msg('Record explicit Policy ownership. Changing ownership does not transfer approvals. Returning to Desk management requires fresh review before publication; existing records remain history.'):msg('Confirm this administrator decision against the exact saved Policy, source binding and configuration. Existing content and reviewed versions remain unchanged.')} footer={<DialogActions><Button disabled={busy||!!pending} onClick={()=>void closeReview()}>{msg('Cancel')}</Button><Button variant="primary" disabled={locked||stale||!rationale.trim()||review?.partial&&!review.allowPartial} onClick={submit}>{msg('Confirm source decision')}</Button></DialogActions>}>
   {review&&<div className={styles.sourceDetails}><dl className={styles.facts}><dt>{msg('Policy')}</dt><dd>{review.policyTitle}</dd><dt>{msg('Source')}</dt><dd>{review.sourceName}</dd><dt>{msg('Ownership')}</dt><dd>{ownershipLabel(review.snapshot.ownership.mode)}</dd>{review.snapshot.currentPreview.status&&<><dt>{msg('Source status')}</dt><dd>{sourceStatusLabel(review.snapshot.currentPreview.status)} · {sourceBasisLabel(review.snapshot.currentPreview.provenance)}</dd><dt>{msg('Source revision')}</dt><dd>{review.snapshot.currentPreview.sourceRevision}</dd></>}</dl><Disclosure title={msg('Technical details')}><dl className={styles.facts}><dt>{msg('Policy revision')}</dt><dd className={styles.digest}>{review.snapshot.policyRevision}</dd><dt>{msg('Authority revision')}</dt><dd className={styles.digest}>{review.snapshot.revision}</dd><dt>{msg('Tracking revision')}</dt><dd className={styles.digest}>{review.snapshot.sourceRevision}</dd><dt>{msg('Configuration')}</dt><dd className={styles.digest}>{review.snapshot.currentConfig?.id}</dd>{review.snapshot.currentPreview.observationId&&<><dt>{msg('Observation')}</dt><dd className={styles.digest}>{review.snapshot.currentPreview.observationId}</dd><dt>{msg('Source digest')}</dt><dd className={styles.digest}>{review.snapshot.currentPreview.content?.bytesDigest}</dd></>}</dl></Disclosure>{stale&&<p role="alert">{msg('The Policy, source or configuration changed. Close this review and inspect the latest state before continuing.')}</p>}{error&&<p role="alert">{systemMessage(error.message)}</p>}
    {review.action==='set-ownership'&&<Field label={msg('Ownership')}>{wiring=><Select {...wiring} value={review.mode} onValueChange={value=>setReview({...review,mode:value as Review['mode']})} options={[{value:'desk-managed',label:msg('Desk-managed')},{value:'externally-managed',label:msg('Externally managed')}]}/>}</Field>}
    {review.action==='configure'&&<><Field label={msg('Fallback status')} hint={msg('Used only when this source genuinely lacks lifecycle capability. Read failures never activate fallback.')}>{wiring=><Select {...wiring} value={review.fallback} onValueChange={value=>setReview({...review,fallback:value as FallbackStatus})} options={[{value:'draft',label:msg('Draft')},{value:'published',label:msg('Published')}]}/>}</Field>{review.fallback==='published'&&<p>{msg('Published will be an administrator-configured assertion. This source provides no approval or sign-off.')}</p>}</>}
    {review.action==='apply-external'&&<p>{sourceStatusLabel(review.snapshot.currentPreview.status)} · {sourceBasisLabel(review.snapshot.currentPreview.provenance)}</p>}
    {review.action==='import-draft'&&<><Field label={msg('Draft title')}>{wiring=><Input {...wiring} value={review.draft.title} maxLength={500} onChange={event=>setReview({...review,draft:{...review.draft,title:event.target.value}})}/>}</Field><Field label={msg('Draft artifact version')}>{wiring=><Input {...wiring} value={review.draft.artifactVersion} maxLength={256} onChange={event=>setReview({...review,draft:{...review.draft,artifactVersion:event.target.value}})}/>}</Field><Field label={msg('Imported draft text')}>{wiring=><TextArea {...wiring} value={review.draft.narrative} rows={8} onChange={event=>setReview({...review,draft:{...review.draft,narrative:event.target.value}})}/>}</Field>{review.partial&&<><p role="status">{msg('Some pages are missing or unreadable. Review the retained original and edit the readable text before importing.')}</p><label><input type="checkbox" checked={review.allowPartial} onChange={event=>setReview({...review,allowPartial:event.target.checked})}/>{msg('Use readable pages despite missing content.')}</label></>}{review.observation&&<Button variant="quiet" onClick={()=>void downloadPolicyObservation(id,review.observation!).catch(()=>setError(new PolicyRequestError('unavailable')))}>{msg('Download original')}</Button>}</>}
    <Field label={msg('Decision rationale')}>{wiring=><TextArea {...wiring} value={rationale} maxLength={8192} rows={3} onChange={event=>setRationale(event.target.value)}/>}</Field>
   </div>}
  </Dialog>
  {inspected&&<ExternalVersionDetail id={id} versionId={inspected} onClose={()=>setInspected(undefined)}/>}
 </SettingsStack>
}
function ExternalVersionFacts({version}:{version:ExternalPolicyVersion}){
 const locale=useLocale()
 return <dl className={styles.facts}><dt>{msg('Retained status')}</dt><dd>{sourceStatusLabel(version.status)} · {sourceBasisLabel(version.provenance)}</dd><dt>{msg('Version')}</dt><dd className={styles.digest}>{version.id}</dd><dt>{msg('Reviewed')}</dt><dd>{version.actor.subject??version.actor.kind} · {new Date(version.at).toLocaleString(locale)}</dd><dt>{msg('Configuration')}</dt><dd className={styles.digest}>{version.configurationId}</dd><dt>{msg('Resource')}</dt><dd>{version.observation.source.provider} · {version.observation.source.resourceId}</dd><dt>{msg('Observation')}</dt><dd className={styles.digest}>{version.observation.id}</dd><dt>{msg('Source revision')}</dt><dd className={styles.digest}>{version.observation.sourceRevision}</dd><dt>{msg('Source digest')}</dt><dd className={styles.digest}>{version.observation.content.bytesDigest}</dd>{version.rawStatus&&<><dt>{msg('Raw source status')}</dt><dd>{version.rawStatus}</dd></>}{version.evidence&&<><dt>{msg('Retained evidence')}</dt><dd className={styles.digest}>{version.evidence.sha256}</dd></>}</dl>
}
function ExternalVersionDetail({id,versionId,onClose}:{id:string;versionId:string;onClose:()=>void}){
 const [downloadError,setDownloadError]=useState(false)
 const query=useQuery({queryKey:[...POLICIES_KEY,id,'external-version',versionId],queryFn:async({signal})=>{const result=await readExternalPolicyVersion(id,versionId,signal);if(result.version.id!==versionId||result.configuration.id!==result.version.configurationId)throw new PolicyRequestError('invalid-policy');return {...result,record:readDocumentRecord(result.document)}},retry:false,staleTime:Infinity})
 return <Dialog open onOpenChange={open=>{if(!open)onClose()}} title={msg('Retained external version')} footer={<DialogActions><Button onClick={onClose}>{msg('Close')}</Button></DialogActions>}>{query.isPending?<LoadingState>{msg('Loading retained version…')}</LoadingState>:query.error?<p role="alert">{msg('Retained external version unavailable')}</p>:query.data&&<div className={styles.sourceDetails}><ExternalVersionFacts version={query.data.version}/>{needsPartialConsent(query.data.record)&&<p role="status">{msg('Some pages are missing or unreadable. Review the retained original alongside the readable text.')}</p>}<pre className={styles.snapshot}>{usablePages(query.data.record).map(page=>page.text).join('\n\n')}</pre><Button onClick={()=>void downloadPolicyObservation(id,query.data!.version.observation).catch(()=>setDownloadError(true))}>{msg('Download original')}</Button>{downloadError&&<p role="alert">{msg('Retained external version unavailable')}</p>}</div>}</Dialog>
}
