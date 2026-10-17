import {useEffect,useRef,useState} from 'react'
import {useQueryClient} from '@tanstack/react-query'
import {msg} from '../i18n'
import type {InputProfile,ProfileBinding} from '../dialect/types'
import {Button} from '../ui/Button'
import {CodeArea} from '../ui/CodeArea'
import {Disclosure} from '../ui/Disclosure'
import {Field} from '../ui/Field'
import {LoadingState} from '../ui/LoadingState'
import {Notice} from '../ui/Notice'
import {portableResearchAPI} from './portableResearch'
import type {ProfileEntry} from './dialectTypes'
import styles from './JobsView.module.css'

interface PageBindingReview {
 revision:string
 descriptor:{adapter:InputProfile['adapter']}
 profile:InputProfile
 entry?:ProfileEntry
}

/** One producer for manual installation bindings; review never acquires a page. */
export function PageBindingControls({disabled,context,chatId,onBind}:{disabled:boolean;context:string;chatId?:string;onBind:(pin:ProfileBinding)=>void}) {
 const cache=useQueryClient()
 const [scopeText,setScopeText]=useState(''),[review,setReview]=useState<PageBindingReview>(),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const epoch=useRef(0),flight=useRef(false),latest=useRef({disabled,context,chatId,scopeText})
 latest.current={disabled,context,chatId,scopeText}
 useEffect(()=>{epoch.current++;setReview(undefined);setError('')},[context,chatId,scopeText])
 useEffect(()=>()=>{epoch.current++},[])
 async function act(save:boolean) {
  if(disabled||flight.current||!scopeText||save&&!review)return
  const token=epoch.current,scope=context,text=scopeText,chat=chatId
  flight.current=true;setBusy(true);setError('')
  try {
   // Each entry is submitted exactly as typed; the shared server policy owns
   // form and bounds. Trimming or URL normalization could widen a scope.
   const result=await portableResearchAPI<PageBindingReview>('/page-binding',{endpointScope:text.split('\n'),...(chat?{chatId:chat}:{}),...(save?{reviewedDigest:review!.revision}:{})})
   if(save)await cache.invalidateQueries({queryKey:['job-input-profiles']})
   const now=latest.current
   if(epoch.current!==token||now.disabled||now.context!==scope||now.chatId!==chat||now.scopeText!==text)return
   if(save) {
    if(!result.entry)throw Error(msg('The request could not be completed.'))
    onBind({profile:result.entry.profile.id,profileDigest:result.entry.digest});setReview(undefined)
   } else setReview(result)
  } catch(e) {if(epoch.current===token)setError(e instanceof Error?e.message:msg('The request could not be completed.'))}
  finally {flight.current=false;setBusy(false)}
 }
 return <Disclosure title={msg('Create page profile')}><div className={styles.fields}>
  <Field label={msg('Allowed page prefixes')} hint={msg('Enter one HTTPS prefix per line, up to 32. Use https://* only to allow any supported HTTPS page.')}>
   {props=><CodeArea {...props} rows={3} value={scopeText} disabled={disabled||busy} onChange={event=>setScopeText(event.target.value)}/>}
  </Field>
  {review&&<>
   <dl className={styles.properties}><div><dt>{msg('Adapter')}</dt><dd>{review.descriptor.adapter.name} · {review.descriptor.adapter.version}</dd></div></dl>
   <ul>{review.profile.endpointScope?.map(prefix=><li key={prefix}><code>{prefix}</code></li>)}</ul>
   {review.profile.endpointScope?.includes('https://*')&&<Notice tone="warning">{msg('This scope allows any page in the supported HTTPS form.')}</Notice>}
   <p className={styles.note}>{msg('Review the exact scope before registering this page profile. Page reads start only with a separately reviewed research run.')}</p>
  </>}
  <div className={styles.actions}><Button size="small" disabled={disabled||busy||!scopeText} onClick={()=>void act(false)}>{msg('Review')}</Button>{review&&<Button size="small" disabled={disabled||busy} onClick={()=>void act(true)}>{msg('Use page profile')}</Button>}</div>
  {busy&&<LoadingState inline>{msg('Checking page profile…')}</LoadingState>}
  {error&&<Notice tone="warning">{error}</Notice>}
 </div></Disclosure>
}
