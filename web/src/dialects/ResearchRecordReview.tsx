import {useEffect,useId,useRef,useState,type RefObject} from 'react'
import {Link} from 'react-router-dom'
import {msg,formatDate} from '../i18n'
import {Button} from '../ui/Button'
import {Disclosure} from '../ui/Disclosure'
import {Select} from '../ui/Select'
import {SearchInput} from '../ui/SearchInput'
import {SettingsFeedback} from '../ui/SettingsFeedback'
import {PageHeader} from '../ui/PageLayout'
import {IconChevronLeft,IconChevronRight} from '../shell/icons'
import {useConfirmDiscard,useRegisteredChanges} from '../shell/UnsavedChanges'
import {dialectAPI,type PreparationReply,type RecordReview} from './client'
import {currentRecordReview,researchReviewLabel,reviewDecisionLabel} from './recordReview'
import {ResearchRecordDetails} from './ResearchRecordDetails'
import {RecordReviewDecision} from './RecordReviewDecision'
import styles from './ResearchRecordReview.module.css'

type Draft={decision?:RecordReview['decision'];note:string}
export function ResearchRecordReview({saved,recordIds,initialRecordId,opener,onClose,onSaved,onReload,onRecordChange}:{saved:PreparationReply;recordIds:string[];initialRecordId?:string;opener?:RefObject<HTMLElement|null>;onClose:()=>void;onSaved:(saved:PreparationReply)=>Promise<void>;onReload:()=>Promise<PreparationReply>;onRecordChange?:(id:string)=>void}) {
 const confirm=useConfirmDiscard(),selectId=useId()
 const [base,setBase]=useState(saved),[recordId,setRecordId]=useState(initialRecordId&&recordIds.includes(initialRecordId)?initialRecordId:recordIds[0])
 const [drafts,setDrafts]=useState<Record<string,Draft>>({}),[busy,setBusy]=useState(false),[error,setError]=useState(''),[success,setSuccess]=useState(''),[search,setSearch]=useState('')
 const heading=useRef<HTMLHeadingElement>(null),viewport=useRef<HTMLDivElement>(null)
 const record=base.document.records.find(record=>record.id===recordId)
 const initial=record?currentRecordReview(record,base.document.recipeVersion):undefined
 const draft=drafts[recordId]??{decision:initial?.decision,note:initial?.note??''}
 const dirty=draft.decision!==initial?.decision||draft.note!==(initial?.note??'')
 const changedDraftIds=new Set(Object.entries(drafts).filter(([id,value])=>{const row=base.document.records.find(r=>r.id===id),review=row?currentRecordReview(row,base.document.recipeVersion):undefined;return value.decision!==review?.decision||value.note!==(review?.note??'')}).map(([id])=>id))
 const anyDirty=changedDraftIds.size>0
 const clearGuard=useRegisteredChanges(anyDirty,msg('Discard unsaved review changes?'),{busy,label:msg('Research reviews'),shouldBlock:({currentLocation,nextLocation})=>currentLocation.pathname!==nextLocation.pathname||new URLSearchParams(currentLocation.search).get('review')!==new URLSearchParams(nextLocation.search).get('review')})
 const stale=saved.sha256!==base.sha256
 const history=(base.document.reviews??[]).filter(event=>event.review.recordId===recordId).slice().reverse()
 const index=recordIds.indexOf(recordId),multiple=recordIds.length>1
 const rows=recordIds.flatMap(id=>{const row=base.document.records.find(r=>r.id===id);return row?[row]:[]})
 const mapped=base.document.recipe.qualification?.mapping.case?.facts?.map(f=>f.source.replace(/^\/values\//,'').replace(/~1/g,'/').replace(/~0/g,'~'))??[]
 useEffect(()=>{heading.current?.focus();viewport.current?.scrollTo?.({top:0})},[recordId])
 useEffect(()=>{if(initialRecordId&&recordIds.includes(initialRecordId)&&!busy)setRecordId(initialRecordId)},[initialRecordId,recordIds,busy])
 const update=(change:Partial<Draft>)=>{setDrafts(current=>({...current,[recordId]:{...draft,...change}}));setSuccess('')}
 const changeRecord=(id:string)=>{if(busy)return;setRecordId(id);onRecordChange?.(id);setSuccess('')}
 const close=async()=>{if(!busy&&(!anyDirty||await confirm(msg('Discard unsaved review changes?')))){clearGuard();onClose();requestAnimationFrame(()=>opener?.current?.focus())}}
 const reload=async()=>{
  if(busy||anyDirty&&!await confirm(msg('Discard unsaved review changes?')))return
  setBusy(true);setError('')
  try{const next=await onReload();setBase(next);setDrafts({});setSuccess('');clearGuard()}catch(e){setError((e as Error).message)}finally{setBusy(false)}
 }
 const save=async(nextRecord=false)=>{
  if(!draft.decision||busy||stale||!dirty||!record?.research)return
  setBusy(true);setError('');setSuccess('')
  try{
   const next=await dialectAPI<PreparationReply>('/'+base.document.id+'/record-reviews','POST',{recordId,decision:draft.decision,note:draft.note},base.sha256)
   await onSaved(next);setBase(next);setDrafts(current=>{const result={...current};delete result[recordId];return result});setSuccess(msg('Review saved'))
   if(nextRecord&&index<recordIds.length-1){setRecordId(recordIds[index+1]);onRecordChange?.(recordIds[index+1])}
  }catch(e){setError((e as Error).message)}finally{setBusy(false)}
 }
 return <section className={styles.root} aria-label={msg('Review research')}>
  <PageHeader title={msg('Review research')} variant="collection" navigation={<nav className={styles.breadcrumb} aria-label={msg('Breadcrumb')}><Link to="/dialects">{msg('Dialects')}</Link><IconChevronRight/><Button variant="quiet" onClick={()=>void close()}>{base.document.name}</Button><IconChevronRight/><span>{msg('Review research')}</span></nav>} actions={<Button variant="quiet" disabled={busy} onClick={()=>void close()}>{msg('Back to records')}</Button>}/>
  <div className={styles.workspace} data-multiple={multiple||undefined}>
   {multiple&&<aside className={styles.queue} aria-label={msg('Review queue')}><SearchInput aria-label={msg('Search selected records')} value={search} onChange={e=>setSearch(e.target.value)}/><p>{msg('{{reviewed}} of {{total}} reviewed',{reviewed:rows.filter(row=>currentRecordReview(row,base.document.recipeVersion)).length,total:recordIds.length})}</p><nav aria-label={msg('Selected records')}>{rows.filter(row=>row.name.toLocaleLowerCase().includes(search.toLocaleLowerCase())).map(row=><Button key={row.id} variant="quiet" disabled={busy} aria-current={row.id===recordId?'true':undefined} onClick={()=>changeRecord(row.id)}><span>{row.name}<small>{changedDraftIds.has(row.id)?msg('Unsaved review'):researchReviewLabel(row,base.document.recipeVersion)}</small></span></Button>)}</nav></aside>}
   <div className={styles.content}>
    <div className={styles.recordHeader}><div><h2 ref={heading} tabIndex={-1}>{record?.name??msg('Record unavailable')}</h2><span>{record?researchReviewLabel(record,base.document.recipeVersion):msg('Reload the selected records.')}</span></div>{multiple&&<div className={styles.navigation}><Button variant="quiet" size="icon" aria-label={msg('Previous record')} disabled={busy||index===0} onClick={()=>changeRecord(recordIds[index-1])}><IconChevronLeft/></Button><span>{msg('{{current}} of {{total}}',{current:index+1,total:recordIds.length})}</span><Button variant="quiet" size="icon" aria-label={msg('Next record')} disabled={busy||index===recordIds.length-1} onClick={()=>changeRecord(recordIds[index+1])}><IconChevronRight/></Button></div>}</div>
    {multiple&&<div className={styles.recordSelector}><Select id={selectId} aria-label={msg('Record')} value={recordId} disabled={busy} options={rows.map(row=>({value:row.id,label:row.name}))} onValueChange={changeRecord}/></div>}
    <div ref={viewport} className={styles.scroll} data-review-scroll>
     {record?.research&&<ResearchRecordDetails record={record} preparationId={base.document.id} mappedFields={mapped} previous={history[0]?.record}/>}
     {history.length>0&&<Disclosure title={msg('Review history')} size="small"><div className={styles.history}>{history.map((event,i)=><div key={event.review.at+i}><p>{reviewDecisionLabel(event.review.decision)} · {formatDate(new Date(event.review.at),{dateStyle:'medium',timeStyle:'short'})}</p><p>{msg('Recipe version {{version}}',{version:event.review.recipeVersion})} · {event.review.reviewer.name||event.review.reviewer.subject||msg('Local access')}</p>{event.review.note&&<p>{event.review.note}</p>}<Disclosure title={msg('Reviewed record')} size="small"><ResearchRecordDetails record={event.record} preparationId={base.document.id}/></Disclosure></div>)}</div></Disclosure>}
    </div>
    <footer className={styles.footer}>
     <div className={styles.decisions}><RecordReviewDecision key={recordId} decision={draft.decision} note={draft.note} disabled={busy||!record?.research} onDecision={decision=>update({decision})} onNote={note=>update({note})}/><div className={styles.actions}>{multiple&&index<recordIds.length-1&&<Button variant="quiet" disabled={busy} onClick={()=>changeRecord(recordIds[index+1])}>{msg('Skip')}</Button>}<Button variant="primary" disabled={busy||stale||!dirty||!draft.decision||!record?.research} onClick={()=>void save(multiple&&index<recordIds.length-1)}>{multiple&&index<recordIds.length-1?msg('Save and next'):msg('Save review')}</Button></div></div>
     <div className={styles.feedback}><SettingsFeedback tone={stale||error?'warning':success?'success':'neutral'} pending={busy} actions={(stale||error)&&<Button size="small" disabled={busy} onClick={()=>void reload()}>{msg('Reload record')}</Button>}>{stale?msg('Dialects changed. Reload the record before saving your review.'):error||success||(anyDirty?msg('Unsaved review changes'):msg('Saving a review does not verify claims or start evaluation.'))}</SettingsFeedback></div>
    </footer>
   </div>
  </div>
 </section>
}
