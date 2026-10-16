import {saveDialectRecipe} from './saveRecipe'
import {LoadingState} from '../ui/LoadingState'
import {useState,useRef,type RefObject} from 'react'
import {useQuery} from '@tanstack/react-query'
import {msg} from '../i18n'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Button} from '../ui/Button'
import {Notice} from '../ui/Notice'
import {useConfirmDiscard,useRegisteredChanges} from '../shell/UnsavedChanges'
import {DialectEditor} from './DialectEditor'
import {ConversionPreview} from './ConversionPreview'
import {buildDialect,readDialectPack} from './dialect'
import {type DialectPackBinding,type PreparationReply} from './client'
import type {DialectChatAction} from './chatActions'
import {boundPack} from './qualification'
import styles from './DialectsView.module.css'

export function DialectProposalReview({saved,action,blocked,opener,onClose,onSaved}:{saved:PreparationReply;action:DialectChatAction;blocked?:boolean;opener:RefObject<HTMLButtonElement|null>;onClose:()=>void;onSaved:(next:PreparationReply)=>void}) {
 const proposal=action.dialect!
 const query=useQuery({queryKey:['dialect-proposal',action.id],queryFn:async({signal})=>buildDialect(saved,proposal,await readDialectPack(proposal.packKey,signal)),retry:false,refetchOnWindowFocus:false,staleTime:0,refetchOnMount:'always'})
 return query.data?<DialectReviewForm key={action.id} saved={saved} action={action} initial={query.data} blocked={blocked||!!query.error||query.isFetching} opener={opener} onClose={onClose} onSaved={onSaved}/>:<Dialog open title={msg('Review dialect')} size="form" openerRef={opener} onOpenChange={open=>{if(!open)onClose()}} footer={<DialogActions><Button onClick={onClose}>{msg('Close')}</Button></DialogActions>}>
  {query.error?<Notice tone="warning" actions={<Button onClick={()=>void query.refetch()}>{msg('Retry')}</Button>}>{query.error.message}</Notice>:<LoadingState>{msg('Loading…')}</LoadingState>}
 </Dialog>
}
function DialectReviewForm({saved,action,initial,blocked,opener,onClose,onSaved}:{saved:PreparationReply;action:DialectChatAction;initial:DialectPackBinding;blocked?:boolean;opener:RefObject<HTMLButtonElement|null>;onClose:()=>void;onSaved:(next:PreparationReply)=>void}) {
 const [value,setValue]=useState(initial),[checked,setChecked]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState(''),inFlight=useRef(false)
 const confirm=useConfirmDiscard(),dirty=JSON.stringify(value)!==JSON.stringify(initial),identity=JSON.stringify([saved.sha256,value])
 const question=msg('Discard changes to this dialect review? The proposal stays in chat.')
 const clear=useRegisteredChanges(dirty,question,{busy})
 const close=async()=>{if(!inFlight.current&&(!dirty||await confirm(question))){clear();onClose()}}
 const pack=boundPack(value),previewRequired=saved.document.records.length>0&&checked!==identity
 const save=async()=>{
  if(inFlight.current||blocked||previewRequired)return
  inFlight.current=true;setBusy(true);setError('')
  try{
   const current=await readDialectPack(value.key)
   if(current.digest!==value.digest)throw Error(msg('The pack changed. Request a new dialect proposal before saving.'))
   const next=await saveDialectRecipe(saved,saved.document.name,{...saved.document.recipe,qualification:value})
   onSaved(next);clear();onClose()
  }catch(e){setError((e as Error).message)}finally{inFlight.current=false;setBusy(false)}
 }
 return <Dialog open title={msg('Review dialect')} size="form" openerRef={opener} onOpenChange={open=>{if(!open)void close()}} footer={<div className={styles.stack}>{error&&<Notice tone="warning">{error}</Notice>}<DialogActions><Button disabled={busy} onClick={()=>void close()}>{msg('Cancel')}</Button><Button variant="primary" disabled={busy||blocked||previewRequired} onClick={()=>void save()}>{busy?msg('Saving…'):msg('Save dialect')}</Button></DialogActions></div>}><div className={styles.stack}>
  <strong>{saved.document.name} → {pack.title??value.key}</strong>
  <p className={styles.note}>{msg('Recipe version {{version}}',{version:saved.document.recipeVersion})} · {pack.version}</p>
  <p className={styles.reviewNote}>{action.dialect!.summary}</p>
  {blocked&&<Notice tone="warning">{msg('The dialect changed or has unsaved edits. Close this review and request a fresh proposal before saving.')}</Notice>}
  <p className={styles.note}>{msg('Review the complete dialect below. Unassigned facts stay unknown. Saving replaces the previous bindings; it does not run decisions or enable Jobs.')}</p>
  <DialectEditor value={value} records={saved.document.records} research={saved.document.recipe.research} disabled={busy||blocked===true} onChange={setValue}/>
  <ConversionPreview saved={saved} value={value} disabled={busy||blocked===true} onChecked={setChecked} expanded/>
  {previewRequired&&<p className={styles.note}>{msg('Preview conversions before saving this dialect.')}</p>}
  {!saved.document.records.length&&<p className={styles.note}>{msg('No records are available for preview yet. Review conversions after records have been prepared.')}</p>}
 </div></Dialog>
}
