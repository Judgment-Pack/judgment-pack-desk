import {useEffect,useState} from 'react'
import { msg, systemMessage, useLocale } from '../i18n'
import {IconException} from '../shell/icons'
import {RetryResponseButton} from './RetryResponseButton'
import {FailureDetails} from '../assistant/FailureDetails'
import {failureMessage,failureRecovery,retryReady,type AIFailure} from '@judgment-pack/harness/failures'
import styles from './ResponseRecovery.module.css'
import type {DialectReadRecoveryView} from '../assistant/serverRuns/types'

/** Backing observations are separate from original response completion. */
export function DialectReadRecovery({view,failed,busy,disabled,onInspect,onRefresh,storageLineOff=false}:{view?:DialectReadRecoveryView;failed:boolean;busy:boolean;disabled:boolean;onInspect?:(operationId:string)=>void;onRefresh?:()=>void;storageLineOff?:boolean}){
 useLocale()
 if(!failed&&!view?.reads.length)return null
 const message=(state:string)=>state==='present-valid'?msg('The retained snapshot was found. The earlier read outcome is unchanged.'):state==='absent'?msg('The retained snapshot is missing. The earlier read was not restarted.'):state==='corrupt'?msg('The retained snapshot could not be verified. The earlier read was not restarted.'):state==='unavailable'?msg('The retained snapshot is unavailable. Saved history is kept.'):msg('The retained snapshot has not been inspected.')
 return <article className={styles.recovery} aria-label={msg('Retained read status')}>
  {failed&&<><p role="status">{msg('Saved read status is unavailable. Retained work is unchanged.')}</p>{onRefresh&&<button type="button" disabled={disabled||busy} onClick={onRefresh}>{msg('Reload saved read status')}</button>}</>}
  {view?.reads.map(row=><div key={row.operationId}>
   <p role="status">{message(row.backingState)}</p>
   {!storageLineOff&&row.reason==='budget-unresolved'&&<p className={styles.caption}>{msg('Storage recovery admission is required before a new observation can be saved.')}</p>}
   {!storageLineOff&&row.canInspect&&onInspect&&<button type="button" disabled={disabled||busy} onClick={()=>onInspect(row.operationId)}>{busy?msg('Inspecting retained read…'):msg('Inspect retained read')}</button>}
  </div>)}
 </article>
}
export function ResponseRecovery({detail,failure,disabled,onRetry,additionalRequests}: {detail:string;failure?:AIFailure;disabled:boolean;onRetry?:()=>void;additionalRequests?:number}) {
 useLocale()
 const [now,setNow]=useState(Date.now)
 useEffect(()=>{if(!failure?.retryAt)return;const delay=Date.parse(failure.retryAt)-Date.now();if(delay<=0)return;const timer=setTimeout(()=>setNow(Date.now()),Math.min(delay+10,86400000));return()=>clearTimeout(timer)},[failure?.retryAt])
 const waiting=!retryReady(failure,now),recovery=failure&&failureRecovery(failure)
 const title=failure?.code==='repeated-work'?msg('Stopped repeating work'):msg('Response interrupted')
 return <article className={styles.recovery} data-role="assistant" aria-label={title}>
  <div className={styles.header}><span className={styles.title}><IconException/>{title}</span>
   {onRetry&&recovery!=='narrow'&&<RetryResponseButton disabled={disabled||waiting} onClick={onRetry} additionalRequests={additionalRequests}/>}
  </div>
  <p role="alert">{systemMessage(failure?failureMessage(failure):detail)}</p>
  <p className={styles.caption}>{failure?.code==='repeated-work'?msg('Review the completed and refused steps before retrying, or send a different instruction.'):recovery==='configure'?msg('Check the AI connection settings before retrying.'):recovery==='storage'?msg('Resolve the saving problem before retrying.'):recovery==='narrow'?msg('Send a smaller request to continue.'):recovery==='reload'?msg('Reload the current state before retrying.'):msg('Completed steps and partial responses are kept. Review any interrupted actions before trying again.')}</p>
  {waiting&&<p className={styles.caption}>{msg('Waiting for the provider’s retry window.')}</p>}
  {additionalRequests&&<p className={styles.caption}>{msg('The request allowance is used up. Retrying adds {{count}} requests.',{count:additionalRequests})}</p>}
  <FailureDetails size="small" failure={failure}/>
 </article>
}
