import { useId } from 'react'
import { Link } from 'react-router-dom'
import { msg, useLocale } from '../i18n'
import { Select } from '../ui/Select'
import type { Chat } from '../chat/store'
import { useSearchConnections, useSearchPreference } from './connections'
import styles from './WebSearchSettings.module.css'
export function ChatSearchOptions({chat,disabled,onChange}:{chat:Chat;disabled:boolean;onChange:(patch:Partial<Chat>)=>void}){
 useLocale()
 const id=useId(),connections=useSearchConnections(),preference=useSearchPreference()
 const ready=preference.data&&!preference.isError
 const mode=chat.researchMode??preference.data?.value.mode??'provided'
 const connection=chat.searchConnection??preference.data?.value.connection
 return <section className={styles.chatOptions}>
  <div className={styles.field}><label htmlFor={`${id}-mode`}>{msg('Research')}</label><Select id={`${id}-mode`} value={chat.researchMode??'default'} disabled={disabled||!ready} options={[{value:'default',label:msg('Desk default')},{value:'auto',label:msg('Auto')},{value:'provided',label:msg('Provided sources only')}]} onValueChange={v=>onChange({researchMode:v==='default'?undefined:v as 'auto'|'provided'})}/></div>
  {mode==='auto'&&<><div className={styles.field}><label htmlFor={`${id}-connection`}>{msg('Search connection')}</label><Select id={`${id}-connection`} value={chat.searchConnection??'default'} disabled={disabled||!connections.available||connections.isError} options={[{value:'default',label:msg('Desk default')},...(connections.data?.connections??[]).map(c=>({value:c.id,label:c.name})),...(chat.searchConnection&&!connections.data?.connections.some(c=>c.id===chat.searchConnection)?[{value:chat.searchConnection,label:msg('Connection unavailable')}]:[])]} onValueChange={v=>onChange({searchConnection:v==='default'?undefined:v})}/></div>
  {!connections.data?.connections.some(c=>c.id===connection)&&<p className={styles.caption}>{msg('Web search is not configured. Supplied links can still be read.')} <Link to="/admin#storage">{msg('Configure web search')}</Link></p>}</>}
 </section>
}
