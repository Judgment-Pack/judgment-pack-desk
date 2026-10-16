import {artifactImpactLabel} from '../artifacts/ArtifactImpactFields'
import {useRef,useState,type RefObject} from 'react'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {useChats} from '../chat/ChatProvider'
import {ChatAction} from '../chat/ChatActions'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Disclosure} from '../ui/Disclosure'
import {CodeBlock} from '../ui/CodeBlock'
import {Notice} from '../ui/Notice'
import {LoadingState} from '../ui/LoadingState'
import {privateQuery} from '../data/privateQuery'
import {msg} from '../i18n'
import {actionApprovalKey} from '../assistant/approvals/client'
import {useDialectRecipeEditing} from './recipeEditing'
import {readPublicationResult} from './publicationProposal'
import {publicationSaveGuard,retainPublishedAction,savePublication} from './savePublication'
import type {DialectChatAction} from './chatActions'
import styles from './DialectsView.module.css'
/** Recovery is independent of current recipe availability, phase or editor state. */
export function DialectPublicationReview({chatId,action,opener,onClose,disabled=false}:{chatId:string;action:DialectChatAction;opener:RefObject<HTMLButtonElement|null>;onClose:()=>void;disabled?:boolean}){
 const {store}=useChats(),cache=useQueryClient(),flight=useRef(false),editing=useDialectRecipeEditing(action.inputId)
 const [busy,setBusy]=useState(false),[error,setError]=useState('')
 const p=action.publication!
 const receipt=useQuery({...privateQuery,queryKey:['dialect-publication-result',action.inputId,p.answer.publication.dialect.request],queryFn:({signal})=>readPublicationResult(action,signal),retry:false,staleTime:0,refetchOnMount:'always'})
 let blocked=disabled||editing||!store||receipt.isPending||!!receipt.error||receipt.isFetching
 let reason=''
 if(store){try{publicationSaveGuard(store,chatId,action)}catch(e){blocked=true;reason=(e as Error).message}}
 const act=async(recover=false)=>{
  if(!store||flight.current||!recover&&blocked)return
  flight.current=true;setBusy(true);setError('')
  try{
   if(recover){const saved=await readPublicationResult(action);if(!saved)throw Error(msg('No saved publication was found. Review before publishing.'));await retainPublishedAction(store,chatId,action)}
   else await savePublication({store,chatId,action})
   await Promise.allSettled([['dialect-publications',action.inputId],['dialect-publication-result'],actionApprovalKey(chatId)].map(queryKey=>cache.invalidateQueries({queryKey})))
   onClose()
  }catch(e){setError((e as Error).message);void receipt.refetch()}finally{flight.current=false;setBusy(false)}
 }
 return <Dialog open title={msg('Review publication')} openerRef={opener} onOpenChange={open=>{if(!open&&!busy)onClose()}} footer={<DialogActions><ChatAction disabled={busy} onClick={onClose}>{msg('Close')}</ChatAction>{receipt.data?<ChatAction disabled={busy} onClick={()=>void act(true)}>{msg('Confirm saved result')}</ChatAction>:<ChatAction disabled={busy||blocked} onClick={()=>void act()}>{busy?msg('Publishing…'):msg('Publish version')}</ChatAction>}</DialogActions>}>
 <div className={styles.stack}>
  <p>{action.name} · {p.version} · {p.variant==='generated'?msg('Reviewed research'):msg('Imported records')}</p>
  <p>{msg('Compatibility impact')}: {artifactImpactLabel(p.impact.classification)}</p><p>{p.impact.rationale}</p>
  <p className={styles.note}>{msg('Publish this checked mapping version. Existing jobs keep their reviewed version.')}</p>
  {receipt.isFetching&&<LoadingState inline>{msg('Checking saved result…')}</LoadingState>}
  {receipt.data&&<Notice>{msg('Version published')}</Notice>}
  {(error||receipt.error)&&<Notice tone="warning" actions={<ChatAction disabled={busy||receipt.isFetching} onClick={()=>void receipt.refetch()}>{msg('Check saved result')}</ChatAction>}>{error||receipt.error?.message}</Notice>}
  {reason&&!receipt.data&&!editing&&<p className={styles.note}>{reason}</p>}
  {editing&&<p className={styles.note}>{msg('Save or discard Recipe edits before reviewing this action.')}</p>}
  <Disclosure title={msg('Review proposed changes')}><CodeBlock text={p.answer.document} label={msg('Dialect')}/><p className={styles.note}>{p.answer.digest}</p></Disclosure>
 </div></Dialog>
}
