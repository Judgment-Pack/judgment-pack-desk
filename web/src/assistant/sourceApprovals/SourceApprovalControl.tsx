import {useEffect,useRef,useState} from 'react'
import {normalizeFailure,type AIFailure} from '@judgment-pack/harness/failures'
import {ChatAction} from '../../chat/ChatActions'
import {Button} from '../../ui/Button'
import {Dialog,DialogActions} from '../../ui/Dialog'
import {CodeBlock} from '../../ui/CodeBlock'
import {Disclosure} from '../../ui/Disclosure'
import {Notice} from '../../ui/Notice'
import {FailureDetails} from '../FailureDetails'
import {msg} from '../../i18n'
import {whenSessionEnds} from '../../mcp/session'
import {sourceReviewClient,type SourceReviewClient} from './client'
import type {SourceCatalog,SourceReview,SourceReviewStatus,SourceRevocation} from './catalog'
import {sourceReviewJSON} from './display'

/** The scope object is a UI lifetime (the owning ChatStore), never authority. */
export function SourceApprovalControl({chatId,scope,disabled=false}:{chatId:string;scope:object;disabled?:boolean}) {
 const owner=useRef({chatId,scope,key:0})
 if(owner.current.chatId!==chatId||owner.current.scope!==scope)owner.current={chatId,scope,key:owner.current.key+1}
 return <ScopedControl key={owner.current.key} chatId={chatId} disabled={disabled}/>
}
function statusLabel(status:SourceReviewStatus) {
 switch(status){
  case 'setup-required':return msg('Setup required')
  case 'review-required':return msg('Review required')
  case 'approved':return msg('Approved')
  case 'changed':return msg('Changed')
  case 'revoked':return msg('Revoked')
 }
}
interface View {catalog?:SourceCatalog;displays?:string[];stale:boolean;busy:boolean;failure?:AIFailure;receipt?:SourceRevocation}
function ScopedControl({chatId,disabled}:{chatId:string;disabled:boolean}) {
 const opener=useRef<HTMLButtonElement>(null),client=useRef<SourceReviewClient|undefined>(undefined)
 const lifecycle=useRef({epoch:0,alive:true,busy:false,abort:undefined as AbortController|undefined})
 const [open,setOpen]=useState(false),[view,setView]=useState<View>({stale:true,busy:false})
 useEffect(()=>{
  const held=lifecycle.current;held.alive=true
  const clear=()=>{held.epoch++;held.abort?.abort();held.abort=undefined;held.busy=false;client.current=undefined}
  const unsubscribe=whenSessionEnds(()=>{clear();setView({stale:true,busy:false})})
  return()=>{held.alive=false;clear();unsubscribe()}
 },[])
 const act=async(kind:'review'|'approve'|'revoke',row?:SourceReview)=>{
  const held=lifecycle.current
  if(disabled||held.busy||kind!=='review'&&(view.stale||!row))return
  held.busy=true;const epoch=held.epoch,abort=new AbortController();held.abort=abort
  const current=()=>held.alive&&held.epoch===epoch&&!abort.signal.aborted
  setView(previous=>({...previous,busy:true,stale:true,failure:undefined,receipt:undefined}))
  try {
   const transport=client.current??await sourceReviewClient(chatId)
   if(!current())return
   client.current=transport
   if(kind==='revoke'){
    if(row?.approval?.revision!==1)return
    const receipt=await transport.revoke({approvalId:row.approval.id,approvalRevision:1},abort.signal)
    if(current())setView(previous=>({...previous,stale:true,receipt}))
   }else{
    if(kind==='approve'&&(!row?.reviewId||row.reviewRevision!==1||row.snapshot.status!=='review-required'))return
    const catalog=kind==='review'?await transport.review(abort.signal):await transport.approve({reviewId:row!.reviewId!,reviewRevision:1},abort.signal)
    // All review text must be available before any selector becomes actionable.
    const displays=catalog.sources.map(sourceReviewJSON)
    if(current())setView({catalog,displays,stale:false,busy:true})
   }
  }catch(cause){if(current())setView(previous=>({...previous,stale:true,failure:normalizeFailure(cause,'desk')}))}
  finally{if(current()){held.busy=false;held.abort=undefined;setView(previous=>({...previous,busy:false}))}}
 }
 const blocked=disabled||view.busy||view.stale
 return <><ChatAction ref={opener} disabled={disabled||view.busy} onClick={()=>setOpen(true)}>{msg('Source approvals')}</ChatAction>
 <Dialog open={open} onOpenChange={setOpen} title={msg('Source approvals')} size="wide" openerRef={opener} footer={<DialogActions><Button onClick={()=>setOpen(false)}>{msg('Close')}</Button></DialogActions>}>
  <p>{msg('Source approval covers the observed configuration. It does not start work, approve actions or grant AI requests.')}</p>
  <p>{msg('Research workflow source consent is separate from its action approval and compute allowance.')}</p>
  <Button disabled={disabled||view.busy} onClick={()=>void act('review')}>{view.catalog?msg('Refresh source review'):msg('Review sources')}</Button>
  {view.busy&&<p role="status">{msg('Checking…')}</p>}
  {view.stale&&view.catalog&&<Notice tone="warning">{msg('Source review is stale. Refresh before deciding.')}</Notice>}
  {view.failure&&<Notice tone="warning"><p role="alert">{msg('Source review unavailable. Refresh before deciding.')}</p><FailureDetails failure={view.failure}/></Notice>}
  {view.receipt&&<><p role="status">{msg('Revoked')}</p><CodeBlock label={msg('Source approval receipt')} text={JSON.stringify(view.receipt)} verbatim/></>}
  {view.catalog?.sources.map((row,index)=>{
   const declaration=row.snapshot.completeDeclarations[0]!
   return <section key={row.snapshot.adapterId} aria-label={row.snapshot.adapterId}>
    <h3>{row.snapshot.adapterId}</h3><p>{statusLabel(row.status)}</p>
    <p>{row.snapshot.identity?.label}</p><p>{row.snapshot.configuration.target}</p>
    <dl><dt>{msg('Operation')}</dt><dd>{declaration.taxonomy.operation}</dd><dt>{msg('Effect')}</dt><dd>{declaration.taxonomy.effect}</dd><dt>{msg('Delegation')}</dt><dd>{declaration.delegation||msg('Not declared')}</dd><dt>{msg('Action approval')}</dt><dd>{declaration.approval}</dd></dl>
    <Disclosure title={msg('Complete source snapshot')}><CodeBlock label={msg('Complete source snapshot')} text={view.displays![index]!} verbatim/></Disclosure>
    {row.reviewId&&row.reviewRevision===1&&row.snapshot.status==='review-required'&&<Button disabled={blocked} onClick={()=>void act('approve',row)}>{msg('Approve source')}</Button>}
    {row.approval?.revision===1&&<Button disabled={blocked} onClick={()=>void act('revoke',row)}>{msg('Revoke source approval')}</Button>}
   </section>
  })}
 </Dialog></>
}
