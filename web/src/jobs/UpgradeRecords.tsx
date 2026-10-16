import {useEffect,useRef,useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {Link} from 'react-router-dom'
import {msg} from '../i18n'
import {Button} from '../ui/Button'
import {Field} from '../ui/Field'
import {Select} from '../ui/Select'
import {Notice} from '../ui/Notice'
import {LoadingState} from '../ui/LoadingState'
import {privateQuery} from '../data/privateQuery'
import {dialectListQuery} from '../dialects/queries'
import {dialectAPI,type DialectEvaluation,type PreparationReply} from '../dialects/client'
import {currentRecordReview} from '../dialects/recordReview'
import type {SourceV2} from './dialectTypes'
import styles from './JobsView.module.css'

/** A generated source must be re-acquired by the host's existing reviewed
 * record boundary. Importing old handoff grants is never a substitute. */
export function UpgradeRecords({packText,disabled,onChange,onChecked}:{packText:string;disabled:boolean;onChange:(value:SourceV2|undefined)=>void;onChecked:(evaluation:DialectEvaluation,signal:AbortSignal)=>Promise<void>}){
 const [id,setId]=useState(''),[recordId,setRecordId]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState<unknown>()
 const operation=useRef<AbortController|null>(null)
 useEffect(()=>{setBusy(false);return()=>{operation.current?.abort();operation.current=null}},[packText,id,recordId])
 const list=useQuery(dialectListQuery())
 const saved=useQuery({...privateQuery,queryKey:['upgrade-records',id],enabled:!!id,queryFn:()=>dialectAPI<PreparationReply>('/'+id),retry:false,refetchOnWindowFocus:false})
 const records=saved.data?.document.records.filter(record=>record.class==='generated'&&currentRecordReview(record,saved.data!.document.recipeVersion)?.decision==='accepted')??[]
 const matching=!saved.error&&saved.data?.document.recipe.qualification?.pack===packText
 const problem=error??saved.error??list.error
 async function prepare(){
  if(!saved.data||!matching||busy)return
  const record=records.find(row=>row.id===recordId);if(!record)return
  const active=new AbortController();operation.current=active;setBusy(true);setError(undefined);onChange(undefined)
  try{
   const evaluation=await dialectAPI<DialectEvaluation>('/'+id+'/evaluations','POST',{recordIds:[record.id]},saved.data.sha256,active.signal)
   active.signal.throwIfAborted()
   await onChecked(evaluation,active.signal)
  }catch(error){if(!active.signal.aborted)setError(error)}finally{if(operation.current===active){operation.current=null;setBusy(false)}}
 }
 return <div className={styles.stack}>
  <p>{msg('Choose a currently reviewed record to prepare a fresh sample. Old source grants are not reused.')}</p>
  <Field label={msg('Dialects')}>{props=><Select {...props} disabled={disabled||busy} value={id} options={(list.data?.items??[]).map(row=>({value:row.id,label:row.name}))} onValueChange={value=>{setId(value);setRecordId('');onChange(undefined);setError(undefined)}}/>}</Field>
  {id&&saved.isPending&&<LoadingState>{msg('Loading records…')}</LoadingState>}
  {saved.data&&!matching&&<Notice tone="warning">{msg('Update this dialect’s pack binding to the selected saved pack before preparing the upgrade.')} <Link to={'/dialects/'+id+'?tab=recipe'}>{msg('Open recipe')}</Link></Notice>}
  {matching&&<Field label={msg('Record')}>{props=><Select {...props} disabled={disabled||busy} value={recordId} options={records.map(row=>({value:row.id,label:row.name}))} onValueChange={value=>{setRecordId(value);onChange(undefined)}}/>}</Field>}
  {Boolean(error||saved.error||list.error)&&<Notice tone="error">{problem instanceof Error?problem.message:msg('The inputs could not be mapped.')}</Notice>}
  <div><Button disabled={disabled||busy||!matching||!recordId} onClick={()=>void prepare()}>{busy?msg('Checking inputs…'):msg('Preview dialect')}</Button></div>
 </div>
}
