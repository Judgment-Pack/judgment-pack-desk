import {useEffect,useState} from 'react'
import {msg} from '../i18n'
import {Field,FieldGroup} from '../ui/Field'
import {Input} from '../ui/Input'
import {Select} from '../ui/Select'
import type {ResearchRetryPolicy,ResearchPendingRetry} from './research'
import styles from './DialectsView.module.css'

export function ResearchRetrySettings({value,onChange,disabled}:{value?:ResearchRetryPolicy;onChange:(value:ResearchRetryPolicy)=>void;disabled:boolean}){
 const policy=value??{mode:'off',maxRetries:2,searchTimeouts:false},enabled=policy.mode==='temporary'
 return <FieldGroup>
  <Field label={msg('Automatic retries')} hint={msg('Retries confirmed temporary failures. Unclear failures and AI model requests are not retried.')}>{props=><Select {...props} value={policy.mode} disabled={disabled} options={[{value:'off',label:msg('Off')},{value:'temporary',label:msg('Temporary failures')}]} onValueChange={mode=>onChange({...policy,mode:mode as ResearchRetryPolicy['mode'],maxRetries:policy.maxRetries||2,searchTimeouts:mode==='off'?false:policy.searchTimeouts})}/>}</Field>
  {enabled&&<>
   <Field label={msg('Maximum retries')} hint={msg('Up to 3 retries per failed operation, with increasing waits.')}>{props=><Input {...props} type="number" min={1} max={3} disabled={disabled} value={policy.maxRetries} onChange={event=>onChange({...policy,maxRetries:Number(event.target.value)})}/>}</Field>
   <Field label={msg('Retry timed-out searches')} hint={msg('Timed-out searches may already have incurred provider usage.')}>{props=><Select {...props} disabled={disabled} value={policy.searchTimeouts?'on':'off'} options={[{value:'off',label:msg('Off')},{value:'on',label:msg('On')}]} onValueChange={choice=>onChange({...policy,searchTimeouts:choice==='on'})}/>}</Field>
  </>}
  <p className={styles.note}>{msg('Retries count toward the saved search/page limits. Search timeout remains in Admin → Connections → Web search.')}</p>
  <p className={styles.note}>{msg('Temporary failures include search rate limits and server errors. Invalid requests, unusable responses and unspecified failures are not retried.')}</p>
 </FieldGroup>
}
export function ResearchRetryReview({policy}:{policy?:ResearchRetryPolicy}){
 return <div className={styles.stack}><dl className={styles.detail}><dt>{msg('Automatic retries')}</dt><dd>{policy?.mode==='temporary'?msg('Temporary failures · Retry limit: {{count}}',{count:policy.maxRetries}):msg('Off')}</dd>{policy?.mode==='temporary'&&<><dt>{msg('Retry timed-out searches')}</dt><dd>{policy.searchTimeouts?msg('On'):msg('Off')}</dd></>}</dl>{policy?.mode==='temporary'&&<p className={styles.note}>{msg('Each retry uses the saved request limits and may incur provider usage. Successful operations are preserved.')}</p>}</div>
}
export function ResearchRetryCountdown({pending}:{pending:ResearchPendingRetry}){
 const [now,setNow]=useState(Date.now())
 useEffect(()=>{setNow(Date.now());const timer=setInterval(()=>setNow(Date.now()),1000);return()=>clearInterval(timer)},[pending.at])
 const seconds=Math.max(0,Math.ceil((Date.parse(pending.at)-now)/1000))
 return <span className={styles.note} aria-live="off">{seconds>0?msg('Retrying in {{seconds}}s',{seconds}):msg('Waiting to retry…')} · {msg('Attempt {{number}} of {{total}}',{number:pending.number,total:pending.maxAttempts})}</span>
}
