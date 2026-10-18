import {useRef,useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {msg,systemMessage,useLocale} from '../i18n'
import {Button} from '../ui/Button'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Disclosure} from '../ui/Disclosure'
import {Field,FieldGroup} from '../ui/Field'
import {Input} from '../ui/Input'
import {SettingsSection} from '../ui/SettingsSection'
import {SettingsFeedback,SettingsActions} from '../ui/SettingsFeedback'
import {LoadingState} from '../ui/LoadingState'
import {useVerifiedSession} from '../auth/VerifiedSession'
import {QueryStatusNotice} from '../shell/QueryStatusNotice'
import {TypedConfirmation,useConfirmDiscard,useRegisteredChangesGuard} from '../shell/UnsavedChanges'
import {formatStorageBytes} from './chatStorage'
import {policyRecoveryQuery,downloadPolicyRecovery,inspectPolicyRecovery,restorePolicyRecovery,recoverPolicyRestore,policyRestoreResultMatches,PolicyRecoveryError,type PolicyRecoveryCounts,type PolicyRecoveryStatus,type PolicyRecoveryPreview,type PolicyRestoreOutcome,type PolicyRestoreRequest} from './policyRecovery'
import {usePolicyRecoveryJournal} from './policyRecoveryJournal'
import styles from './PolicyRecoverySettings.module.css'

function Scope(){return <><p>{msg('Includes Policy drafts, reviews, publications, lifecycle and source status history, operation receipts, and retained Policy originals and extractions.')}</p><p>{msg('Excludes chats, Pack and Graph drafts, memory, project files, jobs, credentials, source consent and browser journals.')}</p></>}
function Counts({counts}:{counts:PolicyRecoveryCounts}){return <dl className={styles.facts}><dt>{msg('Policies')}</dt><dd>{counts.policies}</dd><dt>{msg('History')}</dt><dd>{msg('{{revisions}} revisions · {{reviews}} reviews · {{publications}} local publications · {{operations}} receipts',{...counts})}</dd><dt>{msg('Sources')}</dt><dd>{msg('{{sourceObservations}} observations · {{externalVersions}} external versions',{...counts})}</dd><dt>{msg('Retained bytes')}</dt><dd>{formatStorageBytes(counts.blobBytes)} · {msg('{{count}} blobs',{count:counts.blobs})}</dd></dl>}
function Hold(){return <p>{msg('Archived approvals and source status are imported assertions with unknown currentness. Fresh review and publication are required for local release authority; source reads require new consent. Recovery grants no permission to run jobs or actions.')}</p>}
export function PolicyRecoverySettings({active=true}:{active?:boolean}){
 const locale=useLocale(),session=useVerifiedSession(),query=useQuery(policyRecoveryQuery(active)),status=query.data,journal=usePolicyRecoveryJournal(status?.projectId),pending=journal.pending,confirmDiscard=useConfirmDiscard()
 const [mode,setMode]=useState<'download'|'restore'>(),[exportReview,setExportReview]=useState<PolicyRecoveryStatus>(),[file,setFile]=useState<File>(),[destination,setDestination]=useState(''),[review,setReview]=useState<{preview:PolicyRecoveryPreview;file:File}>(),[confirmed,setConfirmed]=useState('')
 const [busy,setBusy]=useState(false),[error,setError]=useState<PolicyRecoveryError>(),[notice,setNotice]=useState(''),[outcome,setOutcome]=useState<PolicyRestoreOutcome>()
 const flight=useRef(false),opener=useRef<HTMLButtonElement|null>(null)
 const {clearGuard,otherPending}=useRegisteredChangesGuard(!!mode&&(!!file||!!destination||!!review)||!!pending,msg('Leave this Policy recovery setup? A pending restore keeps its exact recovery record.'),{label:msg('Policy recovery archive'),busy,kind:'edit'})
 const blocked=busy||otherPending||query.isError||!status||!journal.ready||!!pending
 const exportStale=!!exportReview&&(exportReview.projectId!==status?.projectId||exportReview.revision!==status?.revision)
 const knownPending=!!pending&&outcome?.receipt.operationId===pending.operationId
 const reviewStale=!!review&&review.preview.projectId!==status?.projectId
 const tooLarge=!!file&&!!status&&file.size>status.limits.archiveBytes
 async function close(){if(busy)return;if((file||destination||review)&&!await confirmDiscard(msg('Discard this Policy recovery setup?')))return;setMode(undefined);setFile(undefined);setDestination('');setReview(undefined);setExportReview(undefined);setConfirmed('');setError(undefined);if(!pending)clearGuard()}
 function open(kind:'download'|'restore',button:HTMLButtonElement){if(blocked)return;opener.current=button;setMode(kind);setExportReview(kind==='download'?structuredClone(status):undefined);setFile(undefined);setDestination('');setReview(undefined);setConfirmed('');setOutcome(undefined);setError(undefined);setNotice('')}
 async function download(){
  if(blocked||!exportReview?.revision||exportStale||flight.current)return
  flight.current=true;setBusy(true);setError(undefined)
  try{await downloadPolicyRecovery(exportReview.revision);setMode(undefined);setExportReview(undefined);clearGuard();setNotice(msg('Policy archive download started.'))}catch(cause){setError(cause instanceof PolicyRecoveryError?cause:new PolicyRecoveryError('unavailable'));void query.refetch()}finally{flight.current=false;setBusy(false)}
 }
 async function inspect(){
  if(blocked||!status?.canInspect||!file||!destination.trim()||tooLarge||flight.current)return
  flight.current=true;setBusy(true);setError(undefined)
  const selectedFile=file,selectedDestination=destination.trim(),projectId=status.projectId
  try{const preview=await inspectPolicyRecovery(selectedFile,selectedDestination);if(preview.projectId!==projectId)throw new PolicyRecoveryError('recovery-conflict');setReview({preview,file:selectedFile});setConfirmed('')}catch(cause){setError(cause instanceof PolicyRecoveryError?cause:new PolicyRecoveryError('unavailable'))}finally{flight.current=false;setBusy(false)}
 }
 async function deliver(settings:PolicyRestoreRequest,recover=false){
  if(flight.current||!recover&&(blocked||!review||reviewStale||confirmed.trim()!==msg('Yes')||!status?.canRestore))return
  flight.current=true;setBusy(true);setError(undefined)
  let dispatched=recover,committed=false
  try{
   if(!recover){journal.store(settings);dispatched=true}
   const result=recover?await recoverPolicyRestore(settings.operationId):await restorePolicyRecovery(settings,review!.file)
   if(!policyRestoreResultMatches(result,settings,session?{kind:session.issuer===null?'local':'oidc',subject:session.subject,...(session.issuer===null?{}:{issuer:session.issuer})}:undefined))throw new PolicyRecoveryError('delivery-unknown',true)
   committed=true;setOutcome(result);setMode(undefined);setFile(undefined);setReview(undefined);setDestination('');setConfirmed('');setNotice(msg('Policy recovery completed in a new inactive project folder.'));journal.clear();clearGuard()
  }catch(cause){
   if(committed){setError(new PolicyRecoveryError('journal-clear-unavailable'));return}
   const failure=cause instanceof PolicyRecoveryError?cause:new PolicyRecoveryError(dispatched?'delivery-unknown':'unavailable',dispatched);setError(failure)
   // Only a proven pre-publication refusal releases the frozen command. GET
   // absence and uncertain/wrong receipts never authorize a second restore.
   if(!recover&&!failure.uncertain){if(dispatched)journal.clear();setReview(undefined);setConfirmed('')}
  }finally{flight.current=false;setBusy(false)}
 }
 function restore(){if(!review)return;const v=review.preview;void deliver({operationId:crypto.randomUUID(),projectId:v.projectId,archiveDigest:v.archiveDigest,archiveBytes:v.archiveBytes,ledgerDigest:v.ledgerDigest,destination:v.destination,destinationRevision:v.destinationRevision})}
 return <SettingsSection title={msg('Policy recovery archive for this project')} variant="plain" description={msg('Recover exact saved Policy history and its retained bytes into a new inactive project. This is separate from chat backup.')}>
  <div className={styles.stack}>
   <QueryStatusNotice id="policy-recovery" label={msg('Policy recovery unavailable')} error={query.error} retrying={query.isFetching} onRetry={()=>query.refetch()}/>
   {query.isPending?<LoadingState>{msg('Reading Policy recovery scope…')}</LoadingState>:status&&<>
    <dl className={styles.facts}><dt>{msg('Current project')}</dt><dd>{status.projectPath}</dd></dl><Scope/>{status.counts&&<Counts counts={status.counts}/>}
    {!status.available&&<SettingsFeedback tone="warning">{msg('Current Policy export is unavailable. A valid archive can still be inspected for recovery into a new project.')}</SettingsFeedback>}
    <p className={styles.caption}>{msg('Archives are unencrypted and can contain private Policy text and originals. Keep them in a private location.')}</p>
    <p className={styles.caption}>{msg('Archive limit: {{archive}}. Expanded data limit: {{expanded}}.',{archive:formatStorageBytes(status.limits.archiveBytes),expanded:formatStorageBytes(status.limits.expandedBytes)})}</p>
    <SettingsActions><Button disabled={blocked||!status.available||!status.revision} onClick={event=>open('download',event.currentTarget)}>{msg('Download Policy archive…')}</Button><Button disabled={blocked||!status.canInspect||!status.canRestore} onClick={event=>open('restore',event.currentTarget)}>{msg('Restore Policy archive…')}</Button></SettingsActions>
   </>}
   {otherPending&&<SettingsFeedback>{msg('Save or stop other pending work before continuing Policy recovery.')}</SettingsFeedback>}
   {error&&!mode&&<SettingsFeedback tone="error">{systemMessage(error.message)}</SettingsFeedback>}{notice&&<SettingsFeedback tone="success">{notice}</SettingsFeedback>}
   {pending&&<><dl className={styles.facts}><dt>{msg('Pending destination')}</dt><dd>{pending.destination}</dd></dl><p>{knownPending?msg('The restore is confirmed. This browser still holds its recovery record; checking reads the receipt without repeating restore.'):msg('The restore result is unresolved. Its archive and destination remain frozen; checking reads the exact receipt without repeating the restore.')}</p><Button disabled={busy||!journal.ready} onClick={()=>void deliver(pending,true)}>{msg('Check restore result')}</Button></>}
   {outcome&&<><dl className={styles.facts}><dt>{msg('Recovered project folder')}</dt><dd>{outcome.receipt.destination}</dd></dl><Hold/><p>{msg('Your current project remains open. To use recovered Policies, explicitly open the new folder in a separate Desk session. Restore does not change startup settings or restart Desk.')}</p><Disclosure title={msg('Technical details')}><dl className={styles.facts}><dt>{msg('Snapshot')}</dt><dd>{outcome.receipt.snapshotId}</dd><dt>{msg('Archive digest')}</dt><dd>{outcome.receipt.archiveDigest}</dd><dt>{msg('Restore operation')}</dt><dd>{outcome.receipt.operationId}</dd></dl></Disclosure></>}
  </div>
  <Dialog open={!!mode} openerRef={opener} onOpenChange={value=>{if(!value)void close()}} title={mode==='download'?msg('Review Policy archive download'):review?msg('Review Policy recovery'):msg('Inspect Policy recovery archive')} description={mode==='download'?msg('Download a complete, integrity-checked snapshot of this project’s saved Policy records. Checksums detect corruption; they do not authenticate approval or current authority.'):msg('Restore only the reviewed Policy snapshot into a new private project folder. Existing projects and the chat store remain available.')} footer={<DialogActions><Button disabled={busy} onClick={()=>void close()}>{msg('Close')}</Button>{review&&!pending&&<Button disabled={busy} onClick={()=>{setReview(undefined);setConfirmed('');setError(undefined)}}>{msg('Back')}</Button>}{mode==='download'?<Button variant="primary" disabled={blocked||exportStale} onClick={()=>void download()}>{msg('Download Policy archive')}</Button>:pending?<Button disabled={busy} onClick={()=>void deliver(pending,true)}>{msg('Check restore result')}</Button>:review?<Button variant="primary" disabled={blocked||reviewStale||confirmed.trim()!==msg('Yes')||!status?.canRestore} onClick={restore}>{msg('Restore into new project')}</Button>:<Button variant="primary" disabled={blocked||!file||!destination.trim()||tooLarge||!status?.canInspect} onClick={()=>void inspect()}>{msg('Inspect archive')}</Button>}</DialogActions>}>
   <div className={styles.stack}>
    {mode==='download'&&exportReview&&<><dl className={styles.facts}><dt>{msg('Project')}</dt><dd>{exportReview.projectPath}</dd></dl><Scope/>{exportReview.counts&&<Counts counts={exportReview.counts}/>}<Disclosure title={msg('Technical details')}><dl className={styles.facts}><dt>{msg('Policy ledger revision')}</dt><dd>{exportReview.revision}</dd></dl></Disclosure></>}
    {mode==='restore'&&!review&&!pending&&<FieldGroup><Field label={msg('Policy recovery archive')} error={tooLarge?msg('This archive exceeds the supported size.'):undefined}>{wiring=><Input {...wiring} type="file" accept=".zip,application/zip" disabled={busy} onChange={event=>{setFile(event.target.files?.[0]);setReview(undefined);setError(undefined)}}/>}</Field>{file&&<p>{msg('Selected archive: {{name}}',{name:file.name})}</p>}<Field label={msg('New project destination')} hint={msg('Enter a new absolute path on the computer running Desk. The folder must not exist; its private parent folder must already exist.')}>{wiring=><Input {...wiring} value={destination} autoComplete="off" spellCheck={false} disabled={busy} onChange={event=>{setDestination(event.target.value);setReview(undefined);setError(undefined)}}/>}</Field><Scope/><Hold/></FieldGroup>}
    {review&&<><dl className={styles.facts}><dt>{msg('Archive')}</dt><dd>{review.file.name} · {formatStorageBytes(review.preview.archiveBytes)}</dd><dt>{msg('Captured')}</dt><dd>{new Date(review.preview.capturedAt).toLocaleString(locale)}</dd><dt>{msg('New project destination')}</dt><dd>{review.preview.destination}</dd></dl><Counts counts={review.preview.counts}/><Scope/><Hold/><p>{msg('The new folder contains a minimal empty project configuration and the recovered Policy store. Pack files, graphs, jobs and chats are not restored. It remains inactive until you explicitly open it.')}</p><Disclosure title={msg('Technical details')}><dl className={styles.facts}><dt>{msg('Archive digest')}</dt><dd>{review.preview.archiveDigest}</dd><dt>{msg('Policy ledger digest')}</dt><dd>{review.preview.ledgerDigest}</dd><dt>{msg('Destination identity')}</dt><dd>{review.preview.destinationRevision}</dd><dt>{msg('Current project identity')}</dt><dd>{review.preview.projectId}</dd></dl></Disclosure>{!pending&&<TypedConfirmation value={confirmed} onChange={setConfirmed} disabled={busy}/>}</>}
    {(exportStale||reviewStale)&&<SettingsFeedback tone="warning">{msg('The reviewed project changed. Close this review and start again.')}</SettingsFeedback>}{error&&<SettingsFeedback tone="error">{systemMessage(error.message)}</SettingsFeedback>}{busy&&<LoadingState>{msg('Verifying Policy recovery…')}</LoadingState>}{pending&&<p>{msg('The restore result is unresolved. Check its exact receipt; the archive will not be uploaded again.')}</p>}
   </div>
  </Dialog>
 </SettingsSection>
}
