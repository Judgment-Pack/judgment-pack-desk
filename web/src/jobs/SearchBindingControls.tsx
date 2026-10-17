import {useEffect,useRef,useState} from 'react'
import {useQueryClient} from '@tanstack/react-query'
import {msg} from '../i18n'
import {useSearchConnections} from '../search/connections'
import {Button} from '../ui/Button'
import {Field} from '../ui/Field'
import {Select} from '../ui/Select'
import {Notice} from '../ui/Notice'
import {LoadingState} from '../ui/LoadingState'
import type {ProfileBinding} from '../dialect/types'
import type {ProfileEntry} from './dialectTypes'
import {portableResearchAPI} from './portableResearch'
import styles from './JobsView.module.css'

interface BindingReview {
 revision:string
 descriptor:{endpoint:string;adapter:{name:string;version:string;digest:string};fixedArguments:{connection:string;revision:string}}
 entry?:ProfileEntry
}

/** Registration is separate from reviewing and running a portable research turn. */
export function SearchBindingControls({disabled,context,onBind}:{disabled:boolean;context:string;onBind:(pin:ProfileBinding)=>void}){
 const connections=useSearchConnections(),cache=useQueryClient()
 const [selected,setSelected]=useState(''),[review,setReview]=useState<BindingReview>(),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const current=connections.data?.connections.find(c=>c.id===selected),epoch=useRef(0),inflight=useRef(false)
 const latest=useRef({disabled,context,connection:current?.id,revision:current?.revision})
 latest.current={disabled,context,connection:current?.id,revision:current?.revision}
 useEffect(()=>{epoch.current++;setReview(undefined);setError('')},[context,current?.id,current?.revision])
 useEffect(()=>()=>{epoch.current++},[])
 async function act(save:boolean){
  if(!current||disabled||inflight.current||save&&!review)return
  const token=epoch.current,choice={connection:current.id,revision:current.revision},scope=context
  inflight.current=true;setBusy(true);setError('')
  try{
   const result=await portableResearchAPI<BindingReview>('/search-binding',{choice,...(save?{reviewedDigest:review!.revision}:{})})
   if(save)await cache.invalidateQueries({queryKey:['job-input-profiles']})
   const now=latest.current
   if(epoch.current!==token||now.disabled||now.context!==scope||now.connection!==choice.connection||now.revision!==choice.revision)return
   if(save){if(!result.entry)throw new Error(msg('The request could not be completed.'));onBind({profile:result.entry.profile.id,profileDigest:result.entry.digest});setReview(undefined)}else setReview(result)
  }catch(e){if(epoch.current===token)setError(e instanceof Error?e.message:msg('The request could not be completed.'))}
  finally{inflight.current=false;setBusy(false)}
 }
 return <div className={styles.fields}>
  {connections.loading?<LoadingState>{msg('Search connections…')}</LoadingState>:<Field label={msg('Search connection')}>{props=><Select {...props} value={selected} options={(connections.data?.connections??[]).filter(c=>!c.timeoutRefused&&c.research?.capabilities.includes('search')).map(c=>({value:c.id,label:c.name}))} disabled={disabled||busy||!connections.available} placeholder={msg('Choose a connection')} onValueChange={setSelected}/>}</Field>}
  {connections.error&&<Notice tone="warning" actions={<Button size="small" onClick={()=>void connections.refetch()}>{msg('Retry')}</Button>}>{msg('Search connections could not be loaded.')}</Notice>}
  {review&&<dl className={styles.properties}><div><dt>{msg('Endpoint')}</dt><dd>{review.descriptor.endpoint}</dd></div><div><dt>{msg('Adapter')}</dt><dd>{review.descriptor.adapter.name} · {review.descriptor.adapter.version}</dd></div></dl>}
  <div className={styles.actions}><Button size="small" disabled={disabled||busy||!current} onClick={()=>void act(false)}>{msg('Review')}</Button>{review&&<Button size="small" disabled={disabled||busy||!current} onClick={()=>void act(true)}>{msg('Use connection')}</Button>}</div>
  {error&&<Notice tone="warning">{error}</Notice>}
 </div>
}
