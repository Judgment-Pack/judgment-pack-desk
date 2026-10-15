import {useState} from 'react'
import {msg,systemMessage} from '../../i18n'
import {Button} from '../../ui/Button'
import {Disclosure} from '../../ui/Disclosure'
import type {Chat,ChatStore} from '../../chat/store'
import {acceptPlan,currentChat} from './tools'
import styles from './PlanCard.module.css'
export function PlanCard({chat,store,disabled,onStart,onRevise,history=false}:{chat:Chat;store?:ChatStore|null;disabled?:boolean;onStart?:()=>boolean;onRevise?:()=>void;history?:boolean}){
 const [busy,setBusy]=useState(false),[error,setError]=useState('')
 const plan=chat.plans?.at(-1)
 if(!plan)return null
 const start=async()=>{
  if(!store||!onStart||busy||disabled||store.running)return
  setBusy(true);setError('')
  try{
   await acceptPlan(store,chat,plan,new AbortController().signal)
   if(!onStart())throw Error('Work could not start. Send your next instruction in this chat to continue the accepted plan.')
   const current=currentChat(store,chat.id)
   if(current?.plans?.at(-1)?.id===plan.id)store.update(chat.id,{plans:current.plans.map(p=>p.id===plan.id?{...p,status:'started'}:p)})
   await store.checkpoint(new AbortController().signal)
  }
  catch(e){setError((e as Error).message)}finally{setBusy(false)}
 }
 const revise=()=>{if(!store)return;const current=currentChat(store,chat.id);if(!current)return;store.update(chat.id,{interaction:'plan',composer:current.composer.trim()?current.composer:msg('Revise plan {{revision}}:',{revision:plan.revision})+' '});onRevise?.()}
 return <section className={styles.plan} aria-label={msg('Plan')}>
  <div className={styles.header}><strong>{msg('Plan {{revision}}',{revision:plan.revision})}</strong><span>{plan.status==='started'?msg('Work started'):plan.status==='accepted'?msg('Approved'):msg('Review required')}</span></div>
  <p>{plan.objective}</p>
  <Disclosure title={msg('{{count}} steps',{count:plan.steps.length})}><ol>{plan.steps.map((step,i)=><li key={i}>{step}</li>)}</ol></Disclosure>
  {!!plan.questions.length&&<Disclosure title={msg('Open questions')} open><ul>{plan.questions.map((q,i)=><li key={i}>{q}</li>)}</ul></Disclosure>}
  {!!plan.references.length&&<Disclosure title={msg('Referenced objects')}><ul>{plan.references.map(r=><li key={r.kind+r.id}>{r.id}</li>)}</ul></Disclosure>}
  {store&&!history&&<div className={styles.actions}><Button variant="quiet" disabled={disabled||busy} onClick={revise}>{msg('Revise')}</Button>{plan.status==='proposed'&&<Button disabled={disabled||busy||!!plan.questions.length} onClick={()=>void start()}>{busy?msg('Checking plan…'):msg('Start work')}</Button>}</div>}
  {plan.status==='proposed'&&<small>{plan.questions.length?msg('Resolve open questions before starting.'):msg('Existing approvals and limits still apply.')}</small>}
  {plan.status==='accepted'&&<small>{msg('Send your next instruction in this chat to continue the approved plan.')}</small>}
  {error&&<p role="alert">{systemMessage(error)}</p>}
  {history&&chat.plans!.length>1&&<Disclosure title={msg('History')}>{chat.plans!.slice(0,-1).map(p=><Disclosure key={p.id} title={msg('Plan {{revision}}',{revision:p.revision})}><p>{p.objective}</p><ol>{p.steps.map((s,i)=><li key={i}>{s}</li>)}</ol></Disclosure>)}</Disclosure>}
 </section>
}
