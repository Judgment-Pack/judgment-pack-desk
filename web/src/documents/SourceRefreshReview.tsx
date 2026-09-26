import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { msg } from '../i18n'
import { useChats } from '../chat/ChatProvider'
import type { ChatAttachment } from '../chat/store'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useConnectionsPane } from '../connections/ConnectionPaneContext'
import type { AttachmentDestination } from '../chat/useChatAttachments'
import { readTests } from '../packs/test-workspace/store'
import { Button } from '../ui/Button'
import { Disclosure } from '../ui/Disclosure'
import { CodeBlock } from '../ui/CodeBlock'
import { RunStatus } from '../ui/RunStatus'
import { ingestDocument, ingestLink, ingestSiteLink, loadDocument, type DocumentReference, type VerifiedDocument } from './client'
import { needsPartialConsent } from './record'
import { readReviews, saveReview, sameSource, sourceChanges, sourceImpacts, type SourceReview } from './refresh'
import styles from './SourceReader.module.css'
export function SourceRefreshReview({ before, reference, onUse }: {before:VerifiedDocument;reference:DocumentReference;onUse?:(file:ChatAttachment)=>void}) {
 const config=useEffectiveConfig().config.research, connections=useConnectionsPane(), chats=useChats()
 const [attempt,setAttempt]=useState(0)
 const [reviews,setReviews]=useState<SourceReview[]>([]),[selected,setSelected]=useState<SourceReview>(),[after,setAfter]=useState<VerifiedDocument>()
 const [project,setProject]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState(''),[phase,setPhase]=useState(''),[stored,setStored]=useState(true),[partial,setPartial]=useState(false)
 const [impacts,setImpacts]=useState<ReturnType<typeof sourceImpacts>>([]),[impactError,setImpactError]=useState('')
 const active=useRef<AbortController|null>(null),mounted=useRef(true),fileInput=useRef<HTMLInputElement|null>(null)
 const kind=before.record.provenance.source.kind
 const operation=()=>{active.current?.abort();const controller=new AbortController();active.current=controller;return controller}
 useEffect(()=>{mounted.current=true;const controller=operation();setBusy(true)
  void readReviews(controller.signal).then(reply=>{if(!controller.signal.aborted){setProject(reply.project);setReviews(reply.content.reviews.filter(r=>r.before.id===reference.id&&r.before.digest===reference.digest))}},e=>{if(!controller.signal.aborted)setError(e.message)}).finally(()=>{if(!controller.signal.aborted)setBusy(false)})
  void readTests().then(reply=>{if(!controller.signal.aborted)setImpacts(sourceImpacts(reference,reply.content,chats.packDrafts))},()=>{if(!controller.signal.aborted)setImpactError(msg('Affected test cases could not be loaded. Review them before using new evidence.'))})
  return ()=>{mounted.current=false;active.current?.abort()}
 },[reference.id,reference.digest,attempt])
 async function retain(file:ChatAttachment, value:VerifiedDocument,signal:AbortSignal) {
  if(!file.document||!sameSource(before,value))throw Error(msg('Choose the same source to compare its latest snapshot.'))
  const review:SourceReview={id:crypto.randomUUID(),name:value.record.document.name,before:reference,after:file.document,checkedAt:new Date().toISOString()}
  signal.throwIfAborted();setSelected(review);setAfter(value);setPartial(false);setStored(false)
  const reply=await saveReview(review,project,signal);signal.throwIfAborted();setStored(true);setReviews(reply.content.reviews.filter(r=>r.before.id===reference.id&&r.before.digest===reference.digest))
 }
 async function refresh(file?:File) {
  if(busy||!project)return
  const controller=operation();setBusy(true);setError('');setPhase(msg('Reading latest source…'))
  try {
   const web=before.object.proof?.web
   const result=file?await ingestDocument(file,config,controller.signal,setPhase):web?.site?await ingestSiteLink({url:web.url,site:web.site},config,controller.signal,setPhase):web?await ingestLink({url:web.url},config,controller.signal,setPhase):undefined
   if(!result)throw Error(msg('Choose the latest file from its connection.'))
   await retain({id:result.reference.id,name:result.document.record.document.name,text:'',document:result.reference},result.document,controller.signal)
  }catch(e){if(!controller.signal.aborted)setError((e as Error).message)}finally{if(!controller.signal.aborted){setBusy(false);setPhase('');if(!project)setError(msg('Source review loading stopped. Retry to continue.'))}}
 }
 function chooseConnected(element:HTMLElement) {
  const controller=operation()
  const source=before.record.provenance.source
  const provider=source.kind==='google-drive'?'google-drive':source.kind==='gmail'?'gmail':source.provider
  const destination:AttachmentDestination={kind:'source-refresh',id:`refresh:${reference.id}:${crypto.randomUUID()}`,
   current:()=>mounted.current&&!controller.signal.aborted?[]:undefined,
   append:async files=>{
    if(!mounted.current||controller.signal.aborted)return
    if(files.length!==1||!files[0]?.document||!config.gateway)throw Error(msg('Choose one source snapshot to compare.'))
    const file=files[0],value=await loadDocument(file.document!,config.gateway,controller.signal)
    if(!mounted.current||controller.signal.aborted)return
    await retain(file,value,controller.signal)
   }}
  connections.open({opener:element,provider,destination})
 }
 async function openReview(review:SourceReview) {
  if(!config.gateway||busy)return
  const controller=operation();setBusy(true);setError('');setAfter(undefined);setSelected(review);setPartial(false);setStored(true)
  try{const value=await loadDocument(review.after,config.gateway,controller.signal);controller.signal.throwIfAborted();if(!sameSource(before,value))throw Error(msg('The refreshed snapshot does not match this source.'));setAfter(value)}catch(e){if(!controller.signal.aborted)setError((e as Error).message)}finally{if(!controller.signal.aborted)setBusy(false)}
 }
 async function markReviewed(use=false) {
  if(!selected||!after||busy)return
  const controller=operation();setBusy(true);setError('')
  try{const next={...selected,reviewedAt:new Date().toISOString()};const reply=await saveReview(next,project,controller.signal);controller.signal.throwIfAborted();setStored(true);setSelected(next);setReviews(reply.content.reviews.filter(r=>r.before.id===reference.id&&r.before.digest===reference.digest))
   if(use)onUse?.({id:next.after.id,name:next.name,text:'',document:{...next.after,allowPartial:partial,needsReview:false},...(after.record.provenance.source.kind==='web'?{link:{url:after.record.provenance.source.requestedUrl!}}:{})})
  }catch(e){if(!controller.signal.aborted)setError((e as Error).message)}finally{if(!controller.signal.aborted)setBusy(false)}
 }
 const changes=after?sourceChanges(before,after):undefined
 return <section aria-label={msg('Source refresh review')}>
  <p>{msg('Refresh creates a new verified snapshot. Existing citations, packs and test history keep their original evidence.')}</p>
  <input hidden ref={fileInput} type="file" accept=".pdf,.txt,.md,.json,.csv" onChange={e=>{if(e.target.files?.[0])void refresh(e.target.files[0]);e.target.value=''}}/>
  <Button disabled={busy||!project} onClick={e=>kind==='web'?void refresh():kind==='inline'?fileInput.current?.click():chooseConnected(e.currentTarget)}>{kind==='web'?msg('Check for source changes'):kind==='inline'?msg('Choose replacement file'):msg('Choose latest from connection')}</Button>
  {busy&&<Button variant="quiet" onClick={()=>{active.current?.abort();setBusy(false);setPhase('');if(!project)setError(msg('Source review loading stopped. Retry to continue.'))}}>{msg('Stop')}</Button>}
  {phase&&<RunStatus running={busy}>{phase}</RunStatus>}{error&&<p role="alert">{error}{!project&&<Button variant="inline" onClick={()=>{setError('');setAttempt(n=>n+1)}}>{msg('Retry')}</Button>}</p>}
  {impactError&&<p role="status">{impactError}</p>}
  <Disclosure title={msg('Affected cases and retained packs')}>
    <p className={styles.meta}>{msg('Direct snapshot references only. Review authored source links and factual inputs separately.')}</p>
    {impacts.length?<ul>{impacts.map(i=><li key={i.id}><Link to={i.href}>{i.title}</Link><span className={styles.meta}> · {i.detail}</span></li>)}</ul>:<p>{msg('No direct references found in saved cases or retained packs.')}</p>}
  </Disclosure>
  {reviews.length>0&&<Disclosure title={msg('Previous source checks')}><ul>{[...reviews].reverse().map(r=><li key={r.id}><Button variant="inline" disabled={busy} onClick={()=>void openReview(r)}>{new Date(r.checkedAt).toLocaleString()} · {r.reviewedAt?msg('Reviewed'):msg('Needs review')}</Button></li>)}</ul></Disclosure>}
  {selected&&after&&changes&&<>
    <h3>{changes.changedPages.length?msg('Source text changed'):changes.bytesChanged||changes.extractionChanged?msg('Source bytes or extraction changed'):msg('Source content is unchanged.')}</h3>
    <p className={styles.meta}>{msg('Latest acquisition {{date}}',{date:new Date(after.record.provenance.observedAt).toLocaleString()})}</p>
    {!stored&&<p role="alert">{msg('The new snapshot is retained, but its review has not been saved. Retry saving below before leaving.')}</p>}
    {changes.changedPages.map(n=><Disclosure key={n} title={msg('Page {{number}}',{number:n})}><CodeBlock label={msg('Earlier source text')} text={before.record.content.pages.find(p=>p.number===n)?.text??msg('Page absent')}/><CodeBlock label={msg('Latest source text')} text={after.record.content.pages.find(p=>p.number===n)?.text??msg('Page absent')}/></Disclosure>)}
    {needsPartialConsent(after.record)&&<label><input type="checkbox" checked={partial} onChange={e=>setPartial(e.target.checked)}/>{msg('Use readable pages despite missing content.')}</label>}
    <p>{msg('Review affected facts and expectations before rerunning tests. Receipt verification does not establish source accuracy.')}</p>
    <div className={styles.refreshActions}><Button disabled={busy} onClick={()=>void markReviewed()}>{stored?msg('Mark reviewed'):msg('Save refresh review')}</Button>
      {onUse&&<Button variant="primary" disabled={busy||!selected.after.pages.length||needsPartialConsent(after.record)&&!partial} onClick={()=>void markReviewed(true)}>{msg('Use snapshot in this case')}</Button>}</div>
  </>}
 </section>
}
