import {useState,type RefObject} from 'react'
import {msg} from '../i18n'
import {useQueryClient} from '@tanstack/react-query'
import {StaleWrite} from '../files/client'
import {Button} from '../ui/Button'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Disclosure} from '../ui/Disclosure'
import {FieldGroup} from '../ui/Field'
import {RecordReviewDecision} from './RecordReviewDecision'
import {SettingsFeedback} from '../ui/SettingsFeedback'
import {useConfirmDiscard} from '../shell/UnsavedChanges'
import {dialectAPI,type PreparationReply,type RecordReview} from './client'
import styles from './DialectsView.module.css'

export function BulkRecordReview({saved,recordIds,opener,onClose,onSaved}:{saved:PreparationReply;recordIds:string[];opener:RefObject<HTMLElement|null>;onClose:()=>void;onSaved:(saved:PreparationReply)=>Promise<void>}) {
 // Retain exactly the selection and revision the user opened. A refresh must
 // never silently expand the batch or approve changed research.
 const [base]=useState(saved),[ids]=useState(()=>[...recordIds])
 const [decision,setDecision]=useState<RecordReview['decision']>(),[note,setNote]=useState('')
 const [busy,setBusy]=useState(false),[error,setError]=useState(''),[conflict,setConflict]=useState(false)
 const cache=useQueryClient()
 const confirm=useConfirmDiscard(),dirty=!!decision||!!note
 const records=ids.map(id=>base.document.records.find(record=>record.id===id))
 const valid=ids.length>0&&new Set(ids).size===ids.length&&records.every(record=>record?.class==='generated'&&record.research)
 const stale=conflict||saved.sha256!==base.sha256||recordIds.length!==ids.length||recordIds.some((id,i)=>id!==ids[i])
 const close=async()=>{if(!busy&&(!dirty||await confirm(msg('Discard unsaved review changes?'))))onClose()}
 const save=async()=>{
  if(!decision||busy||stale||!valid)return
  setBusy(true);setError('')
  try{
   const next=await dialectAPI<PreparationReply>('/'+base.document.id+'/record-reviews','POST',{recordIds:ids,decision,note},base.sha256)
   await onSaved(next);onClose()
  }catch(e){
   if(e instanceof StaleWrite&&e.code==='stale'){
    setConflict(true)
    void cache.invalidateQueries({queryKey:['input-preparations',base.document.id],exact:true})
   }else setError((e as Error).message)
  }finally{setBusy(false)}
 }
 return <Dialog open onOpenChange={open=>{if(!open)void close()}} title={msg('Set review decision · {{count}} records',{count:ids.length})} size="default" openerRef={opener} footer={<div className={styles.stack}>
  {(stale||!valid||error)&&<SettingsFeedback tone="warning">{stale?msg('Dialects changed. Close this dialog and review the selection again.'):!valid?msg('Choose AI-researched records to review. No records were changed.'):error}</SettingsFeedback>}
  <div className={styles.reviewSummary}><SettingsFeedback>{busy?msg('Saving…'):dirty?msg('Unsaved changes'):undefined}</SettingsFeedback><DialogActions><Button disabled={busy} onClick={()=>void close()}>{msg('Cancel')}</Button><Button variant="primary" disabled={busy||stale||!valid||!decision} onClick={()=>void save()}>{ids.length===1?msg('Save this review'):msg('Save {{count}} reviews',{count:ids.length})}</Button></DialogActions></div>
 </div>}><FieldGroup>
  <Disclosure title={msg('Selected records ({{count}})',{count:ids.length})}><ul>{records.map((record,i)=><li key={ids[i]}>{record?.name??ids[i]}</li>)}</ul></Disclosure>
  <RecordReviewDecision decision={decision} note={note} onDecision={setDecision} onNote={setNote} disabled={busy} batch/>
  <p className={styles.note}>{msg('Applies the same decision and note to all {{count}} selected records.',{count:ids.length})} {msg('Saving does not verify claims or start evaluation.')}</p>
 </FieldGroup></Dialog>
}
