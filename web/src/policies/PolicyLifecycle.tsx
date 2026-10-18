import {useEffect,useRef,useState} from 'react'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {msg,systemMessage,useLocale} from '../i18n'
import {Button} from '../ui/Button'
import {Field} from '../ui/Field'
import {Input} from '../ui/Input'
import {Select} from '../ui/Select'
import {TextArea} from '../ui/TextArea'
import {Dialog,DialogActions} from '../ui/Dialog'
import {SettingsSection,SettingsStack} from '../ui/SettingsSection'
import {LoadingState} from '../ui/LoadingState'
import {CollectionTable} from '../ui/SelectionTable'
import {QueryStatusNotice} from '../shell/QueryStatusNotice'
import {useRegisteredChanges} from '../shell/UnsavedChanges'
import {POLICIES_KEY,PolicyRequestError,policyLifecycleQuery,readPolicyLifecycle,recoverPolicyLifecycleOperation,writePolicyLifecycle,type PolicyReply,type PolicyPublication,type PolicyWrite,type PolicyLifecycleOutcome,type PolicyReleaseLifecycle} from './client'
import styles from './PolicyPage.module.css'
import {usePolicyPendingJournal} from './pendingOperation'

function resultMatches(result:PolicyLifecycleOutcome,operation:PolicyWrite,id:string){
 const receipt=result.receipt as {operationId?:unknown;kind?:unknown;policyId?:unknown;publicationId?:unknown;lifecycleRevision?:unknown;lifecycleEventId?:unknown}|null,event=result.lifecycle?.events.at(-1)
 return result.outcomeKind==='lifecycle'&&result.policyId===id&&result.policyRevision===result.lifecycle?.policyRevision&&!!receipt&&receipt.operationId===operation.body.operationId&&receipt.kind==='lifecycle-'+operation.body.action&&receipt.policyId===id&&receipt.publicationId===operation.body.publicationId&&receipt.lifecycleRevision===result.lifecycle?.revision&&!!event&&event.id===receipt.lifecycleEventId&&event.publicationId===operation.body.publicationId&&event.action===operation.body.action
}
function statusLabel(status:PolicyReleaseLifecycle['status']){
 switch(status){case 'effective':return msg('Effective');case 'superseded':return msg('Superseded');case 'expired':return msg('Expired');case 'withdrawn':return msg('Withdrawn');default:return msg('Published')}
}
type Selection={publication:PolicyPublication;action:'activate'|'withdraw';policyRevision:string;lifecycleRevision:string}
export function PolicyLifecycle({policy,active,editing,disabled,onPendingChange}:{policy:PolicyReply;active:boolean;editing:boolean;disabled:boolean;onPendingChange:(held:boolean)=>void}){
 const locale=useLocale(),cache=useQueryClient(),running=useRef(false),id=policy.document.id
 const journal=usePolicyPendingJournal('lifecycle',id),pending=journal.pending
 const [busy,setBusy]=useState(false),[error,setError]=useState<PolicyRequestError>(),[selection,setSelection]=useState<Selection>(),[effectiveAt,setEffectiveAt]=useState(''),[expiresAt,setExpiresAt]=useState(''),[supersedes,setSupersedes]=useState('none'),[rationale,setRationale]=useState('')
 const query=useQuery(policyLifecycleQuery(id,journal.ready&&(active||!!pending))),data=query.data
 const stale=!!selection&&(selection.policyRevision!==policy.revision||selection.lifecycleRevision!==data?.revision||selection.policyRevision!==data?.policyRevision)
 const locked=disabled||editing||busy||!!pending||!journal.ready||query.isError||!data||data.policyRevision!==policy.revision
 useEffect(()=>{onPendingChange(busy||!!pending);return()=>onPendingChange(false)},[busy,pending,onPendingChange])
 useRegisteredChanges(!!pending||!!selection,msg('Leaving will discard unsaved policy changes.'),{label:msg('Policy lifecycle'),busy,kind:'edit'})
 const date=(value?:string)=>value?new Date(value).toLocaleString(locale):msg('Not configured')
 async function refresh(){
  const lifecycle=await readPolicyLifecycle(id)
  cache.setQueryData([...POLICIES_KEY,id,'lifecycle'],lifecycle)
  void cache.invalidateQueries({queryKey:[...POLICIES_KEY,id],exact:true})
  void cache.invalidateQueries({queryKey:POLICIES_KEY,exact:true})
 }
 async function deliver(operation:PolicyWrite,recover=false){
  if(running.current)return
  running.current=true;setBusy(true);setError(undefined)
  let dispatched=recover,committed=false
  try{
   if(!recover){journal.store(operation);setSelection(undefined);dispatched=true}
   const result=recover?await recoverPolicyLifecycleOperation(String(operation.body.operationId)):await writePolicyLifecycle(operation)
   if(!resultMatches(result,operation,id))throw new PolicyRequestError('delivery-unknown',true)
   committed=true
   // A retained receipt is historical. Refetch clock-derived current state before
   // clearing the journal; a failed refresh never authorizes a replacement write.
   await refresh()
   journal.clear();setSelection(undefined);setRationale('')
  }catch(cause){
   const failure=cause instanceof PolicyRequestError?cause:new PolicyRequestError(dispatched?'delivery-unknown':'unavailable',dispatched)
   setError(recover&&failure.code==='not-found'?new PolicyRequestError('operation-unresolved'):failure)
   if(!(recover||committed||dispatched&&failure.uncertain)){journal.clear();void query.refetch()}
  }finally{running.current=false;setBusy(false)}
 }
 function choose(publication:PolicyPublication,action:'activate'|'withdraw'){
  if(locked||!data)return
  setEffectiveAt('');setExpiresAt('');setSupersedes('none');setRationale('');setError(undefined)
  setSelection({publication,action,policyRevision:policy.revision,lifecycleRevision:data.revision})
 }
 function confirm(){
  if(locked||stale||!selection||selection.action==='withdraw'&&!rationale.trim())return
  const operation:PolicyWrite={path:'/'+encodeURIComponent(id)+'/lifecycle',method:'POST',body:{operationId:crypto.randomUUID(),action:selection.action,baseRevision:selection.policyRevision,baseLifecycleRevision:selection.lifecycleRevision,publicationId:selection.publication.id,...(selection.action==='activate'?{...(effectiveAt?{effectiveAt}:{}),...(expiresAt?{expiresAt}:{}),...(supersedes!=='none'?{supersedesPublicationId:supersedes}:{})}:{rationale})}}
  void deliver(operation)
 }
 if(!active&&!pending)return null
 return <SettingsStack>
  <QueryStatusNotice id={'policy-lifecycle:'+id} label={msg('Policy lifecycle unavailable')} error={query.error} retrying={query.isFetching} onRetry={()=>query.refetch()}/>
  <SettingsSection level={2} variant="plain" title={msg('Publication lifecycle')} description={msg('Publication retains an approved version. Activate it separately, now or at a future time, and optionally set expiry. Existing publications have no activation until you record one.')}>
   <div className={styles.sourceGroups}>
    <p>{msg('These local lifecycle records do not enforce job or action admission. Existing execution pins stay unchanged.')}</p>
    {query.isPending?<LoadingState>{msg('Loading lifecycle…')}</LoadingState>:data&&<>
     <p className={styles.note}>{msg('Status observed at')} {date(data.observedAt)}</p>
     {editing&&<p>{msg('Save draft before changing lifecycle.')}</p>}
     {data.policyRevision!==policy.revision&&<p role="status">{msg('The policy or lifecycle changed. Review its current state before continuing.')}</p>}
     {data.releases.length?<div className={styles.tableScroll}><CollectionTable className={styles.table} aria-label={msg('Publication lifecycle')}><thead><tr><th scope="col">{msg('Artifact version')}</th><th scope="col">{msg('Status')}</th><th scope="col">{msg('Effective time')}</th><th scope="col">{msg('Expiry')}</th><th scope="col">{msg('Actions')}</th></tr></thead><tbody>{data.releases.map(release=>{
      const publication=policy.document.publications.find(item=>item.id===release.publicationId)
      return publication&&<tr key={release.publicationId}><th scope="row">{publication.artifactVersion}</th><td>{statusLabel(release.status)}{!release.activationId&&release.status==='published'&&<p className={styles.note}>{msg('No activation recorded')}</p>}</td><td>{date(release.effectiveAt)}</td><td>{date(release.expiresAt)}</td><td><div className={styles.actions}>{!release.activationId&&release.status!=='withdrawn'&&<Button size="small" disabled={locked} onClick={()=>choose(publication,'activate')}>{msg('Activate release')}</Button>}{release.status!=='withdrawn'&&<Button size="small" disabled={locked} onClick={()=>choose(publication,'withdraw')}>{msg('Withdraw release')}</Button>}</div></td></tr>
     })}</tbody></CollectionTable></div>:<p>{msg('Publish a version before configuring its lifecycle.')}</p>}
    </>}
    {busy&&<LoadingState inline>{msg('Working…')}</LoadingState>}{error&&<p role="alert">{systemMessage(error.message)}</p>}{pending&&<Button disabled={busy} onClick={()=>void deliver(pending,true)}>{msg('Check operation result')}</Button>}
   </div>
  </SettingsSection>
  {data&&<SettingsSection level={2} variant="plain" title={msg('Lifecycle history')} description={msg('Administrator decisions are retained in commit order. Effective and expired views are calculated from their recorded schedule; no historical activation is invented.')}>
   <div className={styles.sourceGroups}>{data.events.length?[...data.events].reverse().map(event=><div key={event.id}><p>{event.action==='activate'?msg('Activation recorded'):msg('Withdrawal recorded')} · {event.sequence} · {event.actor.subject} · {date(event.at)}</p><p className={styles.note}>{msg('Administrator configured')} · {policy.document.publications.find(item=>item.id===event.publicationId)?.artifactVersion}</p>{event.effectiveAt&&<p>{msg('Effective time')}: {date(event.effectiveAt)}</p>}{event.expiresAt&&<p>{msg('Expiry')}: {date(event.expiresAt)}</p>}{event.supersedesPublicationId&&<p>{msg('Supersedes')}: {policy.document.publications.find(item=>item.id===event.supersedesPublicationId)?.artifactVersion}</p>}{event.rationale&&<p>{event.rationale}</p>}</div>):<p>{msg('No lifecycle decisions recorded')}</p>}</div>
  </SettingsSection>}
  <Dialog open={!!selection} onOpenChange={open=>{if(!open&&!busy&&!pending)setSelection(undefined)}} title={selection?.action==='withdraw'?msg('Withdraw this release?'):msg('Activate this release?')} description={selection?.action==='withdraw'?msg('Record withdrawal of this exact published release. This records the release’s status; it does not block dependent jobs or external actions. Withdrawal cannot be undone here.'):msg('Choose when this exact published release becomes effective. Leave the effective time blank to activate now. Use UTC dates such as 2026-10-18T12:00:00Z.')} footer={<DialogActions><Button disabled={busy||!!pending} onClick={()=>setSelection(undefined)}>{msg('Cancel')}</Button><Button variant="primary" disabled={locked||stale||selection?.action==='withdraw'&&!rationale.trim()} onClick={confirm}>{selection?.action==='withdraw'?msg('Withdraw release'):msg('Activate release')}</Button></DialogActions>}>
   <div className={styles.sourceGroups}><dl className={styles.facts}><dt>{msg('Artifact version')}</dt><dd>{selection?.publication.artifactVersion}</dd><dt>{msg('Candidate digest')}</dt><dd className={styles.digest}>{selection?.publication.candidateDigest}</dd></dl>{stale&&<p role="alert">{msg('The policy or lifecycle changed. Review its current state before continuing.')}</p>}
    {selection?.action==='activate'?<><Field label={msg('Effective time (UTC)')}>{wiring=><Input {...wiring} value={effectiveAt} maxLength={20} disabled={busy||!!pending} onChange={event=>setEffectiveAt(event.target.value)}/>}</Field><Field label={msg('Expiry (UTC, optional)')}>{wiring=><Input {...wiring} value={expiresAt} maxLength={20} disabled={busy||!!pending} onChange={event=>setExpiresAt(event.target.value)}/>}</Field><Field label={msg('Supersedes (optional)')} hint={msg('Supersession records replacement intent; it does not withdraw the older release or change job pins.')}>{wiring=><Select {...wiring} value={supersedes} onValueChange={setSupersedes} disabled={busy||!!pending} options={[{value:'none',label:msg('None')},...policy.document.publications.filter(item=>item.sequence<selection.publication.sequence).map(item=>({value:item.id,label:item.artifactVersion}))]}/>}</Field></>:<Field label={msg('Withdrawal rationale')}>{wiring=><TextArea {...wiring} value={rationale} maxLength={8192} disabled={busy||!!pending} onChange={event=>setRationale(event.target.value)}/>}</Field>}
   </div>
  </Dialog>
 </SettingsStack>
}
