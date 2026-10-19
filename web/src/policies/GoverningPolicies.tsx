import {useEffect,useMemo,useRef,useState} from 'react'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {msg,systemMessage,useLocale} from '../i18n'
import {digestBytes} from '../dialect/canon'
import {Button} from '../ui/Button'
import {Field} from '../ui/Field'
import {Select} from '../ui/Select'
import {TextArea} from '../ui/TextArea'
import {Disclosure} from '../ui/Disclosure'
import {Notice} from '../ui/Notice'
import {LoadingState} from '../ui/LoadingState'
import {SettingsSection} from '../ui/SettingsSection'
import {CollectionTable} from '../ui/SelectionTable'
import {Toolbar,ToolbarItem} from '../ui/Toolbar'
import {QueryStatusNotice} from '../shell/QueryStatusNotice'
import {useConfirmDiscard,useRegisteredChanges} from '../shell/UnsavedChanges'
import {PolicySourceIdentity} from './PolicySourceIdentity'
import type {CoveragePackSelector,CoverageStatement} from './coverage'
import {ADOPTIONS_KEY,AdoptionRequestError,adoptionListQuery,adoptionOptionsQuery,adoptionQuery,matchesAdoptionOutcome,readAdoption,recoverAdoptionOperation,writeAdoption} from './adoptionClient'
import {adoptionSelection,adoptionSelectionKey,samePublication,type AdoptionChoice,type AdoptionReply,type PendingAdoption} from './adoption'
import {useAdoptionPendingJournal} from './adoptionPending'
import styles from './PolicyPage.module.css'

const packKey=(pack:CoveragePackSelector)=>JSON.stringify(pack)
const reviewLabel=(state:AdoptionReply['document']['reviewState'])=>state==='reviewed'?msg('Adoption reviewed'):state==='changes-requested'?msg('Changes requested'):msg('Awaiting adoption review')
const handlingLabel=(statement:CoverageStatement)=>statement.status==='implemented'?msg('Implemented in pack'):statement.status==='manual'?msg('Manual handoff'):statement.status==='out-of-scope'?msg('Out of scope'):msg('Awaiting interpretation')
const handlingText=(statement:CoverageStatement)=>statement.status==='implemented'?statement.basis:statement.status==='manual'?statement.handoff:statement.status==='out-of-scope'?statement.rationale:statement.explanation

/** Manual administrator lifecycle. The saved whole set is immutable; no AI
 * grant or client-supplied dependency subset participates in review. */
export function GoverningPolicies({owner,content,disabled=false}:{owner:string;content:string;disabled?:boolean}){
 const locale=useLocale(),cache=useQueryClient(),confirm=useConfirmDiscard(),running=useRef(false)
 const journal=useAdoptionPendingJournal(owner),pending=journal.pending
 const [fingerprint,setFingerprint]=useState<{text:string;digest:string;bytes:number}>(),[pack,setPack]=useState<CoveragePackSelector>(),[selected,setSelected]=useState<AdoptionChoice[]>([]),[saved,setSaved]=useState<AdoptionReply>(),[comment,setComment]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState<AdoptionRequestError>(),[notice,setNotice]=useState('')
 const options=useQuery(adoptionOptionsQuery(owner)),list=useQuery(adoptionListQuery()),observed=useQuery(adoptionQuery(saved?.document.id))
 useEffect(()=>{let live=true;void digestBytes(new TextEncoder().encode(content)).then(digest=>{if(live)setFingerprint({text:content,digest:digest.slice(7),bytes:new TextEncoder().encode(content).length})}).catch(()=>{if(live)setError(new AdoptionRequestError('unavailable'))});return()=>{live=false}},[content])
 const raw=fingerprint?.text===content?fingerprint:undefined
 // Parsed identity scopes readable history only. Mutations always require the
 // exact raw-byte fingerprint and server-owned publication selectors below.
 const historyArtifactId=useMemo(()=>{try{const value=JSON.parse(content);return typeof value?.id==='string'?value.id:undefined}catch{return undefined}},[content])
 const matchesRaw=(value:CoveragePackSelector)=>!!raw&&value.content.sha256===raw.digest&&value.content.bytes===raw.bytes
 const publications=(options.data?.packPublications??[]).filter(matchesRaw)
 const choices=pack?(options.data?.coverages??[]).filter(row=>samePublication(row.pack,pack)):[]
 const choiceChanged=selected.some(row=>!choices.some(current=>adoptionSelectionKey(row)===adoptionSelectionKey(current)))
 const sameObserved=!!saved&&!!observed.data&&saved.document.candidate.id===observed.data.document.candidate.id&&samePublication(saved.document.candidate.pack,observed.data.document.candidate.pack)
 const stale=!!saved&&!!observed.data&&(!sameObserved||saved.revision!==observed.data.revision)
 const current=sameObserved&&observed.data?.revision===saved?.revision?observed.data:saved
 const differentPublication=!!saved&&!matchesRaw(saved.document.candidate.pack)
 const candidateChoicesChanged=!!saved&&!!options.data&&!differentPublication&&saved.document.candidate.dependencies.some(row=>!options.data!.coverages.some(choice=>samePublication(choice.pack,saved.document.candidate.pack)&&adoptionSelectionKey(choice)===adoptionSelectionKey(row.selection)))
 const dirty=!!comment||!saved&&(!!pack||selected.length>0)
 const locked=disabled||busy||!!pending||!journal.ready
 const readsFailed=options.isError||list.isError||!!saved&&observed.isError
 const detailUnavailable=observed.isError||!sameObserved
 const canPrepare=!locked&&!readsFailed&&!!pack&&matchesRaw(pack)&&publications.some(row=>samePublication(row,pack))&&selected.length>0&&selected.length<=16&&!choiceChanged
 const canReview=!locked&&!readsFailed&&!!saved&&!stale&&!differentPublication&&!candidateChoicesChanged&&current?.readiness.status==='ready'&&current.eligibility.status!=='ineligible'&&current.eligibility.status!=='unavailable'&&!saved.recovery?.freshReviewRequired
 const commentInvalid=new TextEncoder().encode(comment).length>8192
 const clear=useRegisteredChanges(dirty,msg('Leaving will discard unsaved governing-policy selections or review comments.'),{label:msg('Governing policies'),busy:busy||!!pending,shouldBlock:({currentLocation,nextLocation})=>currentLocation.pathname!==nextLocation.pathname||new URLSearchParams(currentLocation.search).get('view')!==new URLSearchParams(nextLocation.search).get('view')})
 function load(reply:AdoptionReply){setSaved(reply);setPack(reply.document.candidate.pack);setSelected([]);setComment('');setError(undefined);setNotice('');cache.setQueryData([...ADOPTIONS_KEY,reply.document.id],reply);clear()}
 async function open(id:string,publication:CoveragePackSelector){
  if(locked||running.current)return
  if(dirty&&!await confirm(msg('Discard unsaved governing-policy selections or review comments?')))return
  running.current=true;setBusy(true);setError(undefined)
  try{load(await readAdoption(id,publication))}catch(cause){setError(cause instanceof AdoptionRequestError?cause:new AdoptionRequestError('unavailable'))}finally{running.current=false;setBusy(false)}
 }
 async function reset(){if(locked||running.current)return;if(dirty&&!await confirm(msg('Discard unsaved governing-policy selections or review comments?')))return;setSaved(undefined);setPack(undefined);setSelected([]);setComment('');setError(undefined);setNotice('');clear()}
 async function deliver(operation:PendingAdoption,recover=false){
  if(running.current||disabled&&!recover)return
  running.current=true;setBusy(true);setError(undefined);setNotice('')
  let dispatched=recover,committed=false
  try{
   if(!recover){journal.store(operation);dispatched=true}
   const result=recover?await recoverAdoptionOperation(operation.command.operationId):await writeAdoption(operation.command)
   if(!matchesAdoptionOutcome(result,operation))throw new AdoptionRequestError('delivery-unknown',true)
   committed=true
   const latest=await readAdoption(result.adoption.document.id,operation.pack)
   // The receipt retains exact completion, even if the mutable head changed.
   load(result.adoption);cache.setQueryData([...ADOPTIONS_KEY,latest.document.id],latest);journal.clear()
   void cache.invalidateQueries({queryKey:ADOPTIONS_KEY,exact:true});void options.refetch()
   setNotice(latest.revision!==result.adoption.revision?msg('The operation completed, then this adoption changed. Its exact result is retained; load the latest adoption before another review.'):operation.command.action==='prepare'?msg('Adoption candidate prepared. Review the complete saved dependency set before adopting it.'):operation.command.decision==='approve'?msg('Governing-policy adoption recorded. Run protection is not connected yet.'):msg('Adoption changes requested.'))
  }catch(cause){
   const failure=cause instanceof AdoptionRequestError?cause:new AdoptionRequestError(dispatched?'delivery-unknown':'unavailable',dispatched)
   setError(recover&&failure.code==='not-found'?new AdoptionRequestError('operation-unresolved'):failure)
   if(!(recover||committed||dispatched&&failure.uncertain)){if(dispatched)journal.clear();void options.refetch();if(saved)void observed.refetch()}
  }finally{running.current=false;setBusy(false)}
 }
 async function prepare(){if(!canPrepare||!pack)return;await deliver({pack,command:{action:'prepare',operationId:crypto.randomUUID(),pack,coverages:selected.map(adoptionSelection)}})}
 async function review(decision:'approve'|'request-changes'){
  if(!saved||locked||stale||differentPublication||commentInvalid||(decision==='approve'?!canReview:detailUnavailable))return
  const candidate=saved.document.candidate
  await deliver({pack:candidate.pack,command:{action:'review',operationId:crypto.randomUUID(),adoptionId:saved.document.id,baseRevision:saved.revision,candidateDigest:candidate.digest,basisDigest:candidate.basisDigest,decision,...(comment?{comment}:{})}})
 }
 const history=(list.data?.items??[]).filter(row=>matchesRaw(row.pack)||!!historyArtifactId&&row.pack.artifactId===historyArtifactId)
 const publicationChoices=publications.map(row=>({value:packKey(row),label:row.artifactId+' · '+row.version+' · '+row.publicationId.slice(-8)}))
 if(pack&&!publications.some(row=>samePublication(row,pack)))publicationChoices.push({value:packKey(pack),label:pack.artifactId+' · '+pack.version+' · '+pack.publicationId.slice(-8)})
 return <SettingsSection level={2} variant="plain" title={msg('Governing policies')} description={msg('Prepare and explicitly review the complete set of exact Policy publications governing one saved Pack publication.')}>
  <div className={styles.sourceDetails}>
   <p className={styles.note}>{msg('Saved adoption retains reviewed dependencies. It does not yet protect runs; Runner admission is not connected. Tests, citations and chat do not adopt policies.')}</p>
   <QueryStatusNotice id={'adoption-options:'+owner} label={msg('Governing-policy choices unavailable')} error={options.error} retrying={options.isFetching} onRetry={()=>options.refetch()}/>
   <QueryStatusNotice id={'adoption-history:'+owner} label={msg('Governing-policy history unavailable')} error={list.error} retrying={list.isFetching} onRetry={()=>list.refetch()}/>
   <QueryStatusNotice id={'adoption:'+owner+':'+(saved?.document.id??'none')} label={msg('Saved adoption unavailable')} error={observed.error} retrying={observed.isFetching} onRetry={()=>observed.refetch()}/>
   {!options.data&&options.isPending?<LoadingState>{msg('Loading governing-policy choices…')}</LoadingState>:!options.data?<p>{msg('Governing-policy choices unavailable')}</p>:<>
    <Field label={msg('Governing Pack publication')} hint={msg('Only retained publications matching the exact saved Pack bytes are available for a new adoption.')}>{wiring=><Select {...wiring} value={pack?packKey(pack):''} options={publicationChoices} placeholder={msg('Choose an exact saved Pack publication')} disabled={locked||!!saved} onValueChange={key=>{const next=publications.find(row=>packKey(row)===key);if(next){setPack(next);setSelected([]);setError(undefined);setNotice('')}}}/>}</Field>
    {!publications.length&&<p className={styles.note}>{msg('No retained publication matches these exact saved Pack bytes. Publish the saved Pack before preparing adoption.')}</p>}
    {options.data.truncated&&<Notice tone="warning">{msg('The retained publication or coverage inventory exceeds this view’s limit. These choices are incomplete.')}</Notice>}
    {!saved&&pack&&<>
     <fieldset className={styles.elementChoices} disabled={locked}><legend>{msg('Reviewed Policy coverage · select up to 16')}</legend>
      {choices.length?choices.map(row=>{const key=adoptionSelectionKey(row),checked=selected.some(item=>adoptionSelectionKey(item)===key),duplicate=selected.some(item=>item.policyId===row.policyId&&!checked);return <label key={key}><input type="checkbox" checked={checked} disabled={locked||!checked&&(selected.length>=16||duplicate)} onChange={event=>{setSelected(event.target.checked?[...selected,row]:selected.filter(item=>adoptionSelectionKey(item)!==key));setError(undefined);setNotice('')}}/>{row.title} · {row.policyVersion} · {row.policyPublicationId.slice(-8)} · {handlingLabel(row.statement)}</label>}):<p>{msg('No current reviewed coverage matches this exact Pack publication.')}</p>}
     </fieldset>
     {selected.length>0&&<Disclosure title={msg('Selected complete dependency set')}><div className={styles.sourceDetails}>{selected.map(row=><div key={adoptionSelectionKey(row)}><p>{row.title} · {row.policyVersion} · {handlingLabel(row.statement)}</p><p className={styles.note}>{handlingText(row.statement)}</p><pre className={styles.snapshot}>{JSON.stringify({selection:adoptionSelection(row),policyPublicationId:row.policyPublicationId,scope:row.scope,pack:row.pack},null,2)}</pre></div>)}</div></Disclosure>}
     {choiceChanged&&<Notice tone="warning">{msg('Selected coverage changed or is no longer available. Your exact selection is preserved. Clear the selection and review the current choices before preparing adoption.')}</Notice>}
     <Toolbar label={msg('Prepare governing-policy adoption')}><ToolbarItem><Button variant="primary" disabled={!canPrepare} onClick={()=>void prepare()}>{msg('Prepare adoption candidate')}</Button></ToolbarItem>{selected.length>0&&<ToolbarItem><Button disabled={locked} onClick={()=>setSelected([])}>{msg('Clear policy selection')}</Button></ToolbarItem>}</Toolbar>
    </>}
   </>}
   {disabled&&<p role="status" className={styles.note}>{msg('Save or resolve the Pack editor and finish pending Pack work before changing governing policies.')}</p>}
   {saved&&<>
    <p>{reviewLabel(saved.document.reviewState)}</p>
    <div>{saved.document.candidate.dependencies.map(row=><p key={adoptionSelectionKey(row.selection)} className={styles.note}>{row.source.title} · {row.source.artifactVersion} · {handlingLabel(row.statement)}</p>)}</div>
    <AdoptionBasis reply={saved}/>
    {differentPublication&&<Notice tone="warning">{msg('This adoption belongs to an earlier Pack publication. Its exact result remains readable; choose the current publication for new work.')}</Notice>}
    {saved.recovery&&<Notice tone="warning">{msg('Restored adoption decisions are historical and inactive. Prepare and explicitly review a new candidate in this destination.')}</Notice>}
    {stale&&<Notice tone="warning">{msg('The saved adoption changed. Your review comment and exact basis are preserved. Load the latest adoption before reviewing.')}</Notice>}
    {(current?.readiness.status!=='ready'||candidateChoicesChanged)&&<Notice tone="warning">{msg('The complete saved adoption basis is stale or unavailable. Prepare a new candidate after reviewing current coverage; retained history remains readable.')}</Notice>}
    {(current?.eligibility.status==='ineligible'||current?.eligibility.status==='unavailable')&&<p role="status" className={styles.note}>{msg('Current Policy release eligibility is unavailable or does not allow adoption. Retained review history does not grant execution permission.')}</p>}
    {current?.eligibility.status==='not-yet-effective'&&<p className={styles.note}>{msg('One or more reviewed Policy releases are not effective yet. Adoption does not make them effective or authorize runs.')}</p>}
    <Field label={msg('Adoption review comment')} error={commentInvalid?msg('Keep the review comment within 8192 UTF-8 bytes.'):undefined}>{wiring=><TextArea {...wiring} rows={2} maxLength={8192} disabled={locked} value={comment} onChange={event=>setComment(event.target.value)}/>}</Field>
    <Toolbar label={msg('Review complete governing-policy adoption')}><ToolbarItem><Button variant="primary" disabled={!canReview||commentInvalid} onClick={()=>void review('approve')}>{msg('Adopt complete policy set')}</Button></ToolbarItem><ToolbarItem><Button disabled={locked||detailUnavailable||stale||differentPublication||commentInvalid} onClick={()=>void review('request-changes')}>{msg('Request adoption changes')}</Button></ToolbarItem></Toolbar>
    <p className={styles.note}>{msg('Adoption records this administrator’s explicit review of every retained dependency together. The server checks the complete basis again.')}</p>
    <Toolbar label={msg('Saved adoption actions')}>{stale&&<ToolbarItem><Button disabled={locked} onClick={()=>void open(saved.document.id,saved.document.candidate.pack)}>{msg('Load latest adoption')}</Button></ToolbarItem>}<ToolbarItem><Button disabled={locked} onClick={()=>void reset()}>{msg('Prepare another adoption')}</Button></ToolbarItem></Toolbar>
    {saved.document.reviews.length>0&&<Disclosure title={msg('Recorded adoption decisions')}>{[...saved.document.reviews].reverse().map(row=><p key={row.id}>{row.decision==='approve'?msg('Adoption reviewed'):msg('Changes requested')} · {row.actor.subject??row.actor.kind} · <time dateTime={row.at}>{new Date(row.at).toLocaleString(locale)}</time>{row.comment?' · '+row.comment:''}</p>)}</Disclosure>}
   </>}
   {busy&&<LoadingState inline>{msg('Working on governing policies…')}</LoadingState>}
   {error&&error.code!=='adoption-journal-unavailable'&&<Notice tone="error" title={error.code==='unsupported-contract'||error.code==='runtime-unavailable'?msg('Adoption capability unavailable'):undefined}>{systemMessage(error.message)}</Notice>}
   {notice&&<p role="status">{notice}</p>}
   {journal.error&&<Notice tone="error" actions={<Button disabled={busy} onClick={()=>journal.retry()}>{msg('Retry pending adoption recovery')}</Button>}>{systemMessage(journal.error.message)}</Notice>}
   {pending&&<Notice tone="warning" actions={<Button disabled={busy||!journal.ready} onClick={()=>void deliver(pending,true)}>{msg('Check adoption operation result')}</Button>}>{msg('An adoption operation needs recovery. Check its exact result before another write; the operation will not be replayed.')}</Notice>}
   <Disclosure title={msg('Governing-policy history')}>
    {list.isPending?<LoadingState>{msg('Loading governing-policy history…')}</LoadingState>:list.data?history.length?<div className={styles.tableScroll}><CollectionTable className={styles.table} aria-label={msg('Saved governing-policy adoptions')}><thead><tr><th scope="col">{msg('Publication')}</th><th scope="col">{msg('Policies')}</th><th scope="col">{msg('Review')}</th><th scope="col">{msg('Actions')}</th></tr></thead><tbody>{history.map(row=><tr key={row.id}><th scope="row">{row.pack.version} · {row.pack.publicationId.slice(-8)}{!matchesRaw(row.pack)&&<p className={styles.note}>{msg('Earlier Pack publication')}</p>}</th><td>{row.dependencies.length}</td><td>{reviewLabel(row.reviewState)}</td><td><Button size="small" variant="quiet" disabled={locked} onClick={()=>void open(row.id,row.pack)}>{msg('Open adoption')}</Button></td></tr>)}</tbody></CollectionTable></div>:<p>{msg('No governing-policy adoption is saved for this Pack.')}</p>:<p>{msg('Governing-policy history unavailable')}</p>}
   </Disclosure>
  </div>
 </SettingsSection>
}

function AdoptionBasis({reply}:{reply:AdoptionReply}){
 const candidate=reply.document.candidate
 return <Disclosure title={msg('Exact retained adoption basis')}><div className={styles.sourceDetails}>
  <dl className={styles.facts}><dt>{msg('Pack publication')}</dt><dd className={styles.digest}>{candidate.pack.publicationId}</dd><dt>{msg('Pack')}</dt><dd>{candidate.pack.artifactId} · {candidate.pack.version}</dd><dt>{msg('Pack content digest')}</dt><dd className={styles.digest}>{candidate.pack.content.sha256}</dd><dt>{msg('Adoption candidate')}</dt><dd className={styles.digest}>{candidate.id}</dd><dt>{msg('Candidate digest')}</dt><dd className={styles.digest}>{candidate.digest}</dd><dt>{msg('Complete basis digest')}</dt><dd className={styles.digest}>{candidate.basisDigest}</dd><dt>{msg('Candidate format')}</dt><dd>{candidate.formatVersion}</dd></dl>
  {candidate.dependencies.map(row=><Disclosure key={adoptionSelectionKey(row.selection)} title={row.source.title+' · '+row.source.artifactVersion+' · '+handlingLabel(row.statement)}><div className={styles.sourceDetails}><PolicySourceIdentity source={row.source}/><p>{handlingText(row.statement)}</p><Disclosure title={msg('Retained Policy narrative')}><pre className={styles.snapshot}>{row.source.narrative}</pre></Disclosure><pre className={styles.snapshot}>{JSON.stringify({selection:row.selection,scope:row.scope,statement:row.statement,policyRevision:row.policyRevision,lifecycleRevision:row.lifecycleRevision,sourceRevision:row.sourceRevision,authorityRevision:row.authorityRevision,coverageReview:row.coverageReview,coverageRecovery:row.coverageRecovery},null,2)}</pre></div></Disclosure>)}
  <Disclosure title={msg('Retained adoption material references')}><pre className={styles.snapshot}>{JSON.stringify({portableId:candidate.portableId,document:candidate.document,materials:candidate.materials,check:candidate.check,recoveryBoundary:reply.recoveryBoundary},null,2)}</pre></Disclosure>
 </div></Disclosure>
}
