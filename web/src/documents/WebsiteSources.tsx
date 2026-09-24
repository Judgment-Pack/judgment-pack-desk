import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { ReadingDetails } from '../chat/ReadingDetails'
import { useChats } from '../chat/ChatProvider'
import type { ChatAttachment } from '../chat/store'
import { msg } from '../i18n'
import { Button } from '../ui/Button'
import { SourceReader } from './SourceReader'
import styles from './SourceReader.module.css'
import { loadWebsite, type WebsiteReference } from './website'

export function WebsiteSources({reference,documents:initialDocuments,chatId,onRead}:{reference:WebsiteReference;documents:readonly ChatAttachment[];chatId:string;onRead:(content:ReactNode,opener:HTMLElement)=>void}){
 const {chats,drafts}=useChats()
 const documents=[...chats,...drafts].find(chat=>chat.id===chatId)?.documents??initialDocuments
 const pin=useEffectiveConfig().config.research.gateway
 const query=useQuery({queryKey:['website-discovery',reference,pin],enabled:Boolean(pin),retry:false,queryFn:({signal})=>loadWebsite(reference,pin!,signal)})
 return <ReadingDetails title={msg('Website sources')}>
  <p className={styles.identity}>{reference.seed}</p>
  <p>{msg('Discovery lists pages. Saved text does not mean every page was fully read.')}</p>
  {!pin&&<p role="alert">{msg('Website discovery could not be verified; no links were authorized.')}</p>}
  {pin&&query.isPending&&<p role="status">{msg('Loading…')}</p>}
  {query.isError&&<p role="alert">{msg('Website discovery could not be verified; no links were authorized.')}</p>}
  {query.data&&<>
   <p>{msg('Pages discovered: {{count}}',{count:query.data.discovery.pages.filter(p=>p.status==='discovered').length})}</p>
   {query.data.discovery.stopReason!=='finished'&&<p>{msg('Website exploration stopped at its configured limit or a source restriction.')}</p>}
   <ul style={{listStyle:'none',padding:0}}>{query.data.discovery.pages.map(page=>{
    const document=[...documents].reverse().find(d=>d.link?.url===page.url&&d.document)
    const status=document?msg('Text saved'):page.status==='discovered'?msg('Not read'):page.status==='blocked'?msg('Blocked'):page.status==='failed'?msg('Failed'):msg('Skipped')
    return <li key={page.url} style={{padding:'12px 0',borderBottom:'1px solid var(--border-subtle)',overflowWrap:'anywhere'}}>
     {document?<Button variant="inline" onClick={event=>onRead(<SourceReader name={document.name} reference={document.document!} link={document.link}/>,event.currentTarget)}>{page.title||page.url}</Button>:<strong>{page.title||page.url}</strong>}
     <div className={styles.meta}>{status}</div>
     {page.title&&<div style={{fontSize:12}}>{page.url}</div>}
    </li>
   })}</ul>
  </>}
 </ReadingDetails>
}
