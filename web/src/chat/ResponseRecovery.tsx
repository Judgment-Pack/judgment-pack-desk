import {useEffect,useState} from 'react'
import { msg, systemMessage, useLocale } from '../i18n'
import {IconException} from '../shell/icons'
import {RetryResponseButton} from './RetryResponseButton'
import {FailureDetails} from '../assistant/FailureDetails'
import {failureMessage,failureRecovery,retryReady,type AIFailure} from '../assistant/failures'
import styles from './ResponseRecovery.module.css'
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
