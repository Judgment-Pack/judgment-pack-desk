import {useEffect,useRef,useState} from 'react'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {useNavigate,useSearchParams} from 'react-router-dom'
import {msg,systemMessage,useLocale} from '../i18n'
import {Button} from '../ui/Button'
import {Field} from '../ui/Field'
import {Select} from '../ui/Select'
import {TextArea} from '../ui/TextArea'
import {SettingsSection,SettingsStack} from '../ui/SettingsSection'
import {CollectionTable} from '../ui/SelectionTable'
import {Disclosure} from '../ui/Disclosure'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Notice} from '../ui/Notice'
import {LoadingState} from '../ui/LoadingState'
import {QueryStatusNotice} from '../shell/QueryStatusNotice'
import {useConfirmDiscard,useRegisteredChanges} from '../shell/UnsavedChanges'
import {useInspectorControls} from '../shell/InspectorSlot'
import {useChats} from '../chat/ChatProvider'
import {ChatAction,ChatActions} from '../chat/ChatActions'
import {PolicySourceIdentity} from './PolicySourceIdentity'
import {PolicyRequestError,type PolicyReply,type PolicyWrite} from './client'
import {usePolicyPendingJournal} from './pendingOperation'
import {coverageElementKey,coverageElements,type CoverageCheck,type CoverageElement,type CoverageOutcome,type CoveragePackSelector,type CoverageReply,type CoverageResolved,type CoverageSelection,type CoverageStatement} from './coverage'
import {checkCoverage,coverageKey,coverageListQuery,coverageOptionsQuery,coverageQuery,readCoverage,readCoverageCandidate,recoverCoverageOperation,resolveCoverage,writeCoverage} from './coverageClient'
import {findPolicyChat} from './comparisonChat'
import {prepareCoverageChat} from './coverageChat'
import styles from './PolicyPage.module.css'

type Buffer={policyPublicationId:string;pack?:CoveragePackSelector;status:CoverageStatement['status'];text:string;elements:CoverageElement[]}
const empty=():Buffer=>({policyPublicationId:'',status:'awaiting-interpretation',text:'',elements:[]})
const packOf=(reply:CoverageReply):CoveragePackSelector=>{const pack=reply.document.candidate.pack;return {publicationId:pack.publicationId,artifactId:pack.document.id,version:pack.document.version,content:pack.document.content}}
const bufferOf=(reply:CoverageReply):Buffer=>{const candidate=reply.document.candidate,statement=candidate.statement;return {policyPublicationId:candidate.source.publicationId!,pack:packOf(reply),status:statement.status,text:statement.status==='implemented'?statement.basis:statement.status==='manual'?statement.handoff:statement.status==='awaiting-interpretation'?statement.explanation:statement.rationale,elements:statement.status==='implemented'?statement.consumerElements:[]}}
const statementOf=(value:Buffer):CoverageStatement=>value.status==='implemented'?{status:value.status,consumerElements:value.elements,basis:value.text}:value.status==='manual'?{status:value.status,handoff:value.text}:value.status==='awaiting-interpretation'?{status:value.status,explanation:value.text}:{status:value.status,rationale:value.text}
const equal=(a:unknown,b:unknown)=>JSON.stringify(a)===JSON.stringify(b)
const validBuffer=(value:Buffer)=>!!value.pack&&!!value.policyPublicationId&&!!value.text.trim()&&new TextEncoder().encode(value.text).length<=4096&&(value.status!=='implemented'||value.elements.length>0&&value.elements.length<=64)
const elementLabel=(element:CoverageElement)=>element.kind==='rule'?msg('Rule: {{id}}',{id:element.id}):element.kind==='exception'?msg('Exception: {{id}}',{id:element.id}):element.kind==='applicability'?msg('Applicability'):msg('Fallback')
export const coverageStatusLabel=(status:CoverageStatement['status'])=>status==='implemented'?msg('Implemented in pack'):status==='manual'?msg('Manual handoff'):status==='awaiting-interpretation'?msg('Awaiting interpretation'):msg('Out of scope')
const reviewLabel=(state:CoverageReply['document']['reviewState'])=>state==='reviewed'?msg('Review recorded'):state==='changes-requested'?msg('Changes requested'):msg('Unreviewed')
function matchesOutcome(result:CoverageOutcome,operation:PolicyWrite,policyId:string){
 const receipt=result.receipt,reply=result.coverage,candidate=reply?.document?.candidate
 if(result.outcomeKind!=='coverage'||result.policyId!==policyId||result.policyRevision!==reply?.policyRevision||reply?.document?.policyId!==policyId||!receipt||receipt.operationId!==operation.body.operationId||receipt.kind!=='coverage-'+operation.body.action||receipt.policyId!==policyId||receipt.revision!==result.policyRevision||receipt.coverageId!==reply.document.id||receipt.coverageRevision!==reply.revision||receipt.coverageCandidateId!==candidate?.id||receipt.candidateDigest!==candidate?.digest||candidate.content.sha256!==candidate.digest||operation.body.coverageId&&operation.body.coverageId!==reply.document.id)return false
 if(operation.body.action==='save')return candidate.source.publicationId===operation.body.policyPublicationId&&equal(packOf(reply),operation.body.pack)&&equal(candidate.scope,operation.body.scope)&&equal(candidate.statement,operation.body.statement)
 const review=reply.document.reviews.find(row=>row.id===receipt.reviewId)
 return operation.body.action==='review'&&operation.body.candidateDigest===candidate.digest&&reply.policyRevision===operation.body.policyRevision&&!!review&&review.candidateId===candidate.id&&review.candidateDigest===candidate.digest&&review.decision===operation.body.decision&&review.policyRevision===operation.body.policyRevision
}

/** Coverage owns its frozen draft and delivery recovery; chat only explains it. */
export function PolicyCoverage({policy,active,editing,disabled,onPendingChange,onEditingChange}:{policy:PolicyReply;active:boolean;editing:boolean;disabled:boolean;onPendingChange:(pending:boolean)=>void;onEditingChange:(dirty:boolean)=>void}){
 const locale=useLocale(),id=policy.document.id,cache=useQueryClient(),confirm=useConfirmDiscard(),running=useRef(false)
 const journal=usePolicyPendingJournal('coverage',id),pending=journal.pending
 const [saved,setSaved]=useState<CoverageReply>(),[value,setValue]=useState<Buffer>(empty),[busy,setBusy]=useState(false),[error,setError]=useState<PolicyRequestError>(),[notice,setNotice]=useState(''),[check,setCheck]=useState<CoverageCheck>(),[comment,setComment]=useState(''),[historical,setHistorical]=useState<CoverageReply>(),[resolved,setResolved]=useState<CoverageResolved>()
 const list=useQuery(coverageListQuery(id,active||!!pending)),options=useQuery(coverageOptionsQuery(id,active||!!pending)),observed=useQuery(coverageQuery(id,saved?.document.id,active||!!pending))
 const dirty=!equal(value,saved?bufferOf(saved):empty()),stale=!!saved&&!!observed.data&&saved.revision!==observed.data.revision
 const policyStale=!!saved&&(saved.policyRevision!==policy.revision||!!observed.data&&saved.policyRevision!==observed.data.policyRevision),commentInvalid=new TextEncoder().encode(comment).length>8192
 const locked=disabled||editing||busy||!!pending||!journal.ready
 const writable=!locked&&!stale&&!policyStale&&!observed.isError,canSave=writable&&dirty&&validBuffer(value)
 const {store,ready:chatsReady}=useChats(),inspector=useInspectorControls(),navigate=useNavigate(),[params]=useSearchParams(),[append,setAppend]=useState<CoverageSelection>()
 const packChoice=options.data?.packPublications.find(pack=>equal(selectorOf(pack),value.pack)),inventory=resolved?.inventory??(saved&&equal(packOf(saved),value.pack)?saved.inventory:undefined)
 const elements=coverageElements(inventory)
 const clear=useRegisteredChanges(dirty||!!comment,msg('Leaving will discard unsaved coverage changes.'),{label:msg('Policy coverage'),busy:busy||!!pending,saveFailed:!!error,saveDraft:canSave&&!comment?async()=>{if(!await save())throw new PolicyRequestError('coverage-save-incomplete')}:undefined})
 useEffect(()=>{onPendingChange(busy||!!pending);return()=>onPendingChange(false)},[busy,pending,onPendingChange])
 useEffect(()=>{onEditingChange(dirty||!!comment);return()=>onEditingChange(false)},[dirty,comment,onEditingChange])
 function update(next:Buffer){if(next.policyPublicationId!==value.policyPublicationId||!equal(next.pack,value.pack))setResolved(undefined);setValue(next);setCheck(undefined);setNotice('')}
 function load(reply:CoverageReply){setSaved(reply);setValue(bufferOf(reply));setResolved(undefined);setCheck(undefined);setComment('');setHistorical(undefined);setError(undefined);setNotice('');cache.setQueryData([...coverageKey(id),reply.document.id],reply)}
 async function open(coverageId?:string){
  if(running.current||pending||disabled)return
  if((dirty||comment)&&!await confirm(msg('Discard unsaved coverage changes?')))return
  if(!coverageId){setSaved(undefined);setValue(empty());setResolved(undefined);setCheck(undefined);setComment('');setHistorical(undefined);setError(undefined);setNotice('');return}
  running.current=true;setBusy(true);setError(undefined)
  try{const reply=await readCoverage(id,coverageId);if(reply.document.policyId!==id||reply.document.id!==coverageId)throw new PolicyRequestError('unavailable');load(reply)}catch(cause){setError(cause instanceof PolicyRequestError?cause:new PolicyRequestError('unavailable'))}finally{running.current=false;setBusy(false)}
 }
 async function resolve(){
  if(!value.pack||!value.policyPublicationId||locked||running.current)return
  running.current=true;setBusy(true);setError(undefined)
  try{const result=await resolveCoverage(id,{policyPublicationId:value.policyPublicationId,pack:value.pack});if(result.source.policyId!==id||result.source.publicationId!==value.policyPublicationId||!equal({publicationId:result.pack.publicationId,artifactId:result.pack.document.id,version:result.pack.document.version,content:result.pack.document.content},value.pack))throw new PolicyRequestError('unavailable');setResolved(result)}catch(cause){setError(cause instanceof PolicyRequestError?cause:new PolicyRequestError('unavailable'))}finally{running.current=false;setBusy(false)}
 }
 async function deliver(operation:PolicyWrite,recover=false){
  if(running.current)return false
  running.current=true;setBusy(true);setError(undefined);setNotice('')
  let dispatched=recover,committed=false
  const retainedComment=comment
  try{
   if(!recover){journal.store(operation);dispatched=true}
   const result=recover?await recoverCoverageOperation(String(operation.body.operationId)):await writeCoverage(operation)
   if(!matchesOutcome(result,operation,id))throw new PolicyRequestError('delivery-unknown',true)
   committed=true
   const current=await readCoverage(id,result.coverage.document.id)
   if(current.document.id!==result.coverage.document.id||current.document.policyId!==id)throw new PolicyRequestError('delivery-unknown',true)
   // Receipt is exact historical completion; a later head is never substituted.
   load(result.coverage);if(operation.body.action==='save'&&retainedComment)setComment(retainedComment);cache.setQueryData([...coverageKey(id),current.document.id],current)
   void cache.invalidateQueries({queryKey:coverageKey(id),exact:true})
   journal.clear();setNotice(current.revision!==result.coverage.revision?msg('The operation completed, then this coverage record changed. Your exact candidate is preserved. Load the latest record before another change.'):operation.body.action==='save'?msg('Coverage draft saved'):msg('Coverage review recorded'));if(operation.body.action!=='save'||!retainedComment)clear();return true
  }catch(cause){
   const failure=cause instanceof PolicyRequestError?cause:new PolicyRequestError(dispatched?'delivery-unknown':'unavailable',dispatched)
   setError(recover&&failure.code==='not-found'?new PolicyRequestError('operation-unresolved'):failure)
   if(!(recover||committed||dispatched&&failure.uncertain)){journal.clear();void observed.refetch();void options.refetch()}
   return false
  }finally{running.current=false;setBusy(false)}
 }
 async function save(){
  if(!canSave)return false
  const operation:PolicyWrite={path:'/'+encodeURIComponent(id)+'/coverage',method:'POST',body:{action:'save',operationId:crypto.randomUUID(),...(saved?{coverageId:saved.document.id,baseRevision:saved.revision}:{}),policyPublicationId:value.policyPublicationId,pack:value.pack,scope:{kind:'whole-narrative'},statement:statementOf(value)}}
  return deliver(operation)
 }
 async function runCheck(){
  if(!saved||!writable||dirty||running.current)return
  running.current=true;setBusy(true);setError(undefined);setCheck(undefined)
  try{const result=await checkCoverage(id,saved.document.id,{baseRevision:saved.revision,candidateDigest:saved.document.candidate.digest,policyRevision:policy.revision});if(result.coverageRevision!==saved.revision||result.candidateDigest!==saved.document.candidate.digest||result.policyRevision!==policy.revision||!['available','unavailable','unsupported'].includes(result.availability))throw new PolicyRequestError('unavailable');setCheck(result)}catch(cause){setError(cause instanceof PolicyRequestError?cause:new PolicyRequestError('unavailable'));void observed.refetch()}finally{running.current=false;setBusy(false)}
 }
 async function review(decision:'approve'|'request-changes'){
  if(!saved||!writable||dirty||commentInvalid||decision==='approve'&&(!referencesValid(check)||saved.availability.packPublication!=='present'))return
  await deliver({path:'/'+encodeURIComponent(id)+'/coverage',method:'POST',body:{action:'review',operationId:crypto.randomUUID(),coverageId:saved.document.id,baseRevision:saved.revision,candidateDigest:saved.document.candidate.digest,policyRevision:policy.revision,decision,...(comment?{comment}:{})}})
 }
 async function discuss(selection:CoverageSelection,allowAppend=false){
  if(!store||!chatsReady||busy)return
  const preferred=params.get('chat')??undefined,chat=findPolicyChat(store,id,preferred)
  if(chat?.composer&&!allowAppend){setAppend(selection);return}
  setAppend(undefined);setError(undefined)
  try{const chat=prepareCoverageChat(store,id,selection,preferred);await store.checkpoint(new AbortController().signal);const next=new URLSearchParams(params);next.set('chat',chat.id);navigate({search:next.toString()},{replace:true});inspector.reveal()}catch{setError(new PolicyRequestError('coverage-chat-unavailable'))}
 }
 async function inspect(candidateId:string){
  if(!saved||busy||running.current)return
  running.current=true;setBusy(true);setError(undefined)
  try{const exact=await readCoverageCandidate(id,saved.document.id,candidateId);if(exact.document.id!==saved.document.id||exact.document.policyId!==id||exact.document.candidate.id!==candidateId)throw new PolicyRequestError('unavailable');setHistorical(exact)}catch(cause){setError(cause instanceof PolicyRequestError?cause:new PolicyRequestError('unavailable'))}finally{running.current=false;setBusy(false)}
 }
 if(!active&&!pending)return null
 const selected=saved?.document.candidate
 const policyChoices=options.data?.policyPublications??[],packChoices=options.data?.packPublications??[]
 const selectedPublication=policyChoices.find(item=>item.publicationId===value.policyPublicationId)
 const policySelect=[...policyChoices.map(item=>({value:item.publicationId,label:item.title+' · '+item.artifactVersion+' · '+item.publicationId.slice(-8)})),...(!selectedPublication&&saved?[{value:saved.document.candidate.source.publicationId!,label:saved.document.candidate.source.title+' · '+saved.document.candidate.source.artifactVersion+' · '+saved.document.candidate.source.publicationId!.slice(-8)}]:[])]
 const packSelect=[...packChoices.map(item=>({value:packKey(item),label:(item.alias??item.artifactId)+' · '+item.version+' · '+item.publicationId.slice(-8)})),...(!packChoice&&value.pack?[{value:packKey(value.pack),label:value.pack.artifactId+' · '+value.pack.version+' · '+value.pack.publicationId.slice(-8)}]:[])]
 return <SettingsStack>
  <QueryStatusNotice id={'policy-coverage:'+id} label={msg('Policy coverage unavailable')} error={list.error} retrying={list.isFetching} onRetry={()=>list.refetch()}/>
  <QueryStatusNotice id={'policy-coverage-options:'+id} label={msg('Coverage publication choices unavailable')} error={options.error} retrying={options.isFetching} onRetry={()=>options.refetch()}/>
  <QueryStatusNotice id={'policy-coverage-record:'+id+':'+(saved?.document.id??'none')} label={msg('Coverage record unavailable')} error={observed.error} retrying={observed.isFetching} onRetry={()=>observed.refetch()}/>
  <SettingsSection level={2} variant="plain" title={msg('Policy coverage')} description={msg('Review a declaration against one exact local Policy publication and one retained Pack publication. Scope is the whole published narrative; headings do not define requirements.')}>
   <div className={styles.sourceDetails}>
    <p className={styles.note}>{msg('This local review records an administrator’s declaration. It does not establish complete semantic coverage, governing adoption, publication or permission to run actions.')}</p>
    {list.isPending?<LoadingState>{msg('Loading coverage records…')}</LoadingState>:list.data?.items.length?<div className={styles.tableScroll}><CollectionTable className={styles.table} aria-label={msg('Saved coverage records')}><thead><tr><th scope="col">{msg('Policy version')}</th><th scope="col">{msg('Pack')}</th><th scope="col">{msg('Declaration')}</th><th scope="col">{msg('Review')}</th><th scope="col">{msg('Actions')}</th></tr></thead><tbody>{list.data.items.map(row=><tr key={row.id}><th scope="row">{row.policyVersion} · {row.policyPublicationId.slice(-8)}</th><td>{row.pack.artifactId} · {row.pack.version} · {row.pack.publicationId.slice(-8)}</td><td>{coverageStatusLabel(row.status)}</td><td>{reviewLabel(row.reviewState)}</td><td><Button size="small" variant="quiet" disabled={busy||!!pending||disabled} onClick={()=>void open(row.id)}>{msg('Open coverage record')}</Button></td></tr>)}</tbody></CollectionTable></div>:list.data?<p>{msg('No coverage reviews saved.')}</p>:<p>{msg('Policy coverage unavailable')}</p>}
    {saved&&<Button size="small" disabled={busy||!!pending||disabled} onClick={()=>void open()}>{msg('New coverage draft')}</Button>}
   </div>
  </SettingsSection>
  {(policyChoices.length>0||saved)&&<SettingsSection level={2} variant="plain" title={msg('Coverage declaration')} description={msg('Choose exact publications, declare how the narrative is handled, and save this draft before checking or recording a review.')}>
   <div className={styles.sourceDetails}>
    {(saved?.recovery??options.data?.recovery)&&<p role="status">{msg('Imported coverage reviews are historical assertions with unknown currentness. A fresh local review requires the original exact Pack publication to be present.')}</p>}
    <Field label={msg('Policy publication')}>{wiring=><Select {...wiring} value={value.policyPublicationId} onValueChange={publicationId=>update({...value,policyPublicationId:publicationId})} options={policySelect} placeholder={msg('Choose a Policy publication')} disabled={locked}/>}</Field>
    <Field label={msg('Pack publication')}>{wiring=><Select {...wiring} value={value.pack?packKey(value.pack):''} onValueChange={key=>{const pack=packChoices.find(item=>packKey(item)===key);if(pack)update({...value,pack:selectorOf(pack),elements:[]})}} options={packSelect} placeholder={msg('Choose a retained Pack publication')} disabled={locked}/>}</Field>
    <Button size="small" disabled={locked||!value.policyPublicationId||!value.pack} onClick={()=>void resolve()}>{msg('Inspect selected publications')}</Button>
    {resolved&&<Disclosure title={msg('Selected publication details')} open><div className={styles.sourceDetails}><PolicySourceIdentity source={resolved.source}/><pre className={styles.snapshot}>{resolved.source.narrative}</pre><pre className={styles.snapshot}>{JSON.stringify(resolved.inventory,null,2)}</pre></div></Disclosure>}
    {!packChoices.length&&options.data&&<p className={styles.note}>{msg('No retained Pack publications are available for a new declaration.')}</p>}
    {options.data?.truncated&&<p role="status" className={styles.note}>{msg('Publication choices are incomplete because the retained history exceeds this view’s limit.')}</p>}
    <p className={styles.note}>{msg('Scope: Whole published narrative')}</p>
    <Field label={msg('Declared handling')}>{wiring=><Select {...wiring} value={value.status} onValueChange={status=>update({...value,status:status as Buffer['status']})} options={(['implemented','manual','awaiting-interpretation','out-of-scope'] as const).map(status=>({value:status,label:coverageStatusLabel(status)}))} disabled={locked}/>}</Field>
    {value.status==='implemented'&&<fieldset className={styles.elementChoices} disabled={locked}><legend>{msg('Pack elements')}</legend>{inventory?elements.length?elements.map(element=><label key={coverageElementKey(element)}><input type="checkbox" disabled={locked||value.elements.length>=64&&!value.elements.some(item=>equal(item,element))} checked={value.elements.some(item=>equal(item,element))} onChange={event=>update({...value,elements:event.target.checked?[...value.elements,element]:value.elements.filter(item=>!equal(item,element))})}/>{elementLabel(element)}</label>):<p>{msg('This publication has no selectable Pack elements.')}</p>:<p className={styles.note}>{msg('Inspect selected publications to choose exact Pack elements.')}</p>}</fieldset>}
    <Field label={value.status==='implemented'?msg('Implementation basis'):value.status==='manual'?msg('Manual handoff'):value.status==='awaiting-interpretation'?msg('Interpretation needed'):msg('Out-of-scope rationale')} hint={msg('Describe your declaration in up to 4096 UTF-8 bytes.')}>{wiring=><TextArea {...wiring} rows={3} maxLength={4096} disabled={locked} value={value.text} onChange={event=>update({...value,text:event.target.value})}/>}</Field>
    {(stale||policyStale||editing)&&<p role="status">{stale?msg('The coverage record changed. Your draft is preserved. Load the latest record before saving or reviewing.'):policyStale?msg('The Policy changed. Resolve the Policy editor and reload this coverage record before checking or reviewing.'):msg('Save or resolve the Policy editor before changing coverage.')}</p>}
    {stale||policyStale?<Button disabled={busy||!!pending||disabled} onClick={()=>void open(saved!.document.id)}>{msg('Load latest coverage record')}</Button>:null}
    <div className={styles.actions}><Button variant="primary" disabled={!canSave} onClick={()=>void save()}>{msg('Save coverage draft')}</Button><Button disabled={!saved||!writable||dirty} onClick={()=>void runCheck()}>{msg('Check declaration')}</Button></div>
    {saved&&<><p className={styles.note}>{reviewLabel(saved.document.reviewState)}</p><CoverageBasis reply={saved}/>{saved.availability.packPublication!=='present'&&<p role="status">{msg('The original Pack publication is unavailable. Retained bytes remain inspectable; a new approved review requires the exact publication.')}</p>}</>}
    {check&&<Notice tone={referencesValid(check)?'info':'warning'} title={check.availability==='unsupported'?msg('Coverage checking unsupported'):check.availability==='unavailable'?msg('Coverage checking unavailable'):referencesValid(check)?msg('Declared references are structurally valid'):msg('Declaration needs attention')} detail={JSON.stringify(check,null,2)}>{check.availability==='available'?msg('A structural check does not establish complete semantic coverage or grant permission.'):msg('The connected Runtime cannot check this exact experimental Policy profile. Saved drafts and history remain available.')}</Notice>}
    {saved&&<><Field label={msg('Coverage review comment')} error={commentInvalid?msg('Keep the review comment within 8192 UTF-8 bytes.'):undefined}>{wiring=><TextArea {...wiring} rows={2} maxLength={8192} disabled={locked} value={comment} onChange={event=>setComment(event.target.value)}/>}</Field><div className={styles.actions}><Button disabled={!writable||dirty||commentInvalid||!referencesValid(check)||saved.availability.packPublication!=='present'} onClick={()=>void review('approve')}>{msg('Record coverage review')}</Button><Button disabled={!writable||dirty||commentInvalid} onClick={()=>void review('request-changes')}>{msg('Request coverage changes')}</Button></div><p className={styles.note}>{msg('Recording a review repeats the check on the server and records this administrator and exact candidate.')}</p><ChatActions><ChatAction disabled={!chatsReady||busy} onClick={()=>void discuss({coverageId:saved.document.id,candidateId:selected!.id})}>{msg('Discuss review')}</ChatAction></ChatActions></>}
   </div>
  </SettingsSection>}
  {!policyChoices.length&&!saved&&options.data&&<p>{msg('Publish a Policy version to review coverage.')}</p>}
  {busy&&<LoadingState inline>{msg('Working…')}</LoadingState>}{error&&<Notice tone="error">{systemMessage(error.message)}</Notice>}{notice&&<p role="status">{notice}</p>}
  {pending&&<Notice tone="warning" actions={<Button disabled={busy} onClick={()=>void deliver(pending,true)}>{msg('Check operation result')}</Button>}>{msg('A coverage operation needs recovery. Check its result before another write; the operation will not be replayed.')}</Notice>}
  {saved&&<SettingsSection level={2} variant="plain" title={msg('Coverage history')}><div className={styles.sourceDetails}><div className={styles.tableScroll}><CollectionTable className={styles.table} aria-label={msg('Coverage candidate history')}><thead><tr><th scope="col">{msg('Candidate')}</th><th scope="col">{msg('Time')}</th><th scope="col">{msg('Actor')}</th><th scope="col">{msg('Actions')}</th></tr></thead><tbody>{[...saved.document.candidates].reverse().map(candidate=><tr key={candidate.id}><th scope="row">{candidate.id.slice(-8)}</th><td><time dateTime={candidate.at}>{new Date(candidate.at).toLocaleString(locale)}</time></td><td>{candidate.actor.subject??candidate.actor.kind}</td><td><Button size="small" variant="quiet" disabled={busy} onClick={()=>void inspect(candidate.id)}>{msg('Inspect candidate')}</Button></td></tr>)}</tbody></CollectionTable></div>{saved.document.reviews.length>0&&<Disclosure title={msg('Recorded coverage decisions')}>{[...saved.document.reviews].reverse().map(review=><p key={review.id}>{review.decision==='approve'?msg('Review recorded'):msg('Changes requested')} · {review.candidateId.slice(-8)} · {review.actor.subject??review.actor.kind} · {new Date(review.at).toLocaleString(locale)}{review.comment?' · '+review.comment:''}</p>)}</Disclosure>}</div></SettingsSection>}
  <Dialog open={!!historical} onOpenChange={open=>{if(!open)setHistorical(undefined)}} title={msg('Exact saved coverage candidate')} footer={<DialogActions><Button onClick={()=>setHistorical(undefined)}>{msg('Close')}</Button><ChatAction disabled={!chatsReady||busy} onClick={()=>{if(historical){const selection={coverageId:historical.document.id,candidateId:historical.document.candidate.id};setHistorical(undefined);void discuss(selection)}}}>{msg('Discuss review')}</ChatAction></DialogActions>}>{historical&&<div className={styles.sourceDetails}><CoverageBasis reply={historical}/><p>{reviewLabel(historical.document.reviewState)}</p><pre className={styles.snapshot}>{JSON.stringify(historical.document.candidate.statement,null,2)}</pre></div>}</Dialog>
  <Dialog open={!!append} onOpenChange={open=>{if(!open)setAppend(undefined)}} title={msg('Append coverage request?')} description={msg('Your Assistant composer already contains text. Append an unsent request with these exact saved coverage references to your existing message.')} footer={<DialogActions><Button onClick={()=>setAppend(undefined)}>{msg('Cancel')}</Button><Button onClick={()=>{if(append)void discuss(append,true)}}>{msg('Append request')}</Button></DialogActions>}><pre className={styles.snapshot}>{append?JSON.stringify(append,null,2):''}</pre></Dialog>
 </SettingsStack>
}
function selectorOf(pack:CoveragePackSelector):CoveragePackSelector{return {publicationId:pack.publicationId,artifactId:pack.artifactId,version:pack.version,content:pack.content}}
const packKey=(pack:CoveragePackSelector)=>JSON.stringify(selectorOf(pack))
export function referencesValid(check?:CoverageCheck){return check?.availability==='available'&&!!check.report&&typeof check.report==='object'&&(check.report as {status?:unknown}).status==='references-valid'}
function CoverageBasis({reply}:{reply:CoverageReply}){const candidate=reply.document.candidate;return <Disclosure title={msg('Exact retained review basis')}><div className={styles.sourceDetails}><PolicySourceIdentity source={candidate.source}/><dl className={styles.facts}><dt>{msg('Coverage candidate')}</dt><dd className={styles.digest}>{candidate.id}</dd><dt>{msg('Candidate digest')}</dt><dd className={styles.digest}>{candidate.digest}</dd><dt>{msg('Pack publication')}</dt><dd className={styles.digest}>{candidate.pack.publicationId}</dd><dt>{msg('Pack')}</dt><dd>{candidate.pack.document.id} · {candidate.pack.document.version}</dd><dt>{msg('Pack content digest')}</dt><dd className={styles.digest}>{candidate.pack.document.content.sha256}</dd></dl><Disclosure title={msg('Published Policy narrative')}><pre className={styles.snapshot}>{candidate.source.narrative}</pre></Disclosure><Disclosure title={msg('Exact Pack element inventory')}><pre className={styles.snapshot}>{JSON.stringify(reply.inventory,null,2)}</pre></Disclosure></div></Disclosure>}
