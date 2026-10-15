import {ChatAction} from './ChatActions'
import { useLayoutEffect, useMemo, useRef, useState, type RefObject } from 'react'
import type { Turn } from '../research/run'
import { msg, useLocale } from '../i18n'
import { IconMessageList } from '../shell/icons'
import { Popover } from '../ui/Popover'
import { SearchInput } from '../ui/SearchInput'
import { useMessageClock } from './MessageTime'
import { messageTimeFormatter } from './timestamps'
import styles from './MessageNavigator.module.css'

export function messageEntries(turns: readonly Turn[]) {
 return turns.flatMap((turn,index) => turn.role === 'user' && turn.kind !== 'note'
  ? [{turn, id:turn.id ?? `message-${index}`, text:turn.text.trim() || turn.attachments?.map(file=>file.name).join(', ') || msg('Attachment')}] : [])
}
export function MessageNavigator({ turns, open, onOpenChange, onJump, currentId, threadRef, disabled }: {
 turns: readonly Turn[]; open: boolean; onOpenChange: (open:boolean)=>void; onJump:(id:string)=>void
 currentId?:string; threadRef:RefObject<HTMLDivElement|null>; disabled?:boolean
}) {
 useLocale()
 const entries=useMemo(()=>messageEntries(turns),[turns])
 const [query,setQuery]=useState('')
 const input=useRef<HTMLInputElement>(null)
 const [list,setList]=useState<HTMLOListElement|null>(null)
 const pending=useRef<string|null>(null)
 const clock=useMessageClock(), format=useMemo(()=>messageTimeFormatter(clock.locale,clock.timeZone),[clock.locale,clock.timeZone])
 const shown=entries.map((entry,index)=>({...entry,number:index+1})).filter(entry=>entry.text.toLocaleLowerCase().includes(query.toLocaleLowerCase()))
 const [width,setWidth]=useState<number>()
 useLayoutEffect(()=>{
  if(!open)return
  const resize=()=>setWidth(threadRef.current ? Math.max(240,threadRef.current.clientWidth-24) : undefined)
  resize(); const observer=new ResizeObserver(resize)
  if(threadRef.current)observer.observe(threadRef.current)
  return ()=>observer.disconnect()
 },[open,threadRef])
 // Every opening starts at the newest message. Searching starts at the first
 // match; clearing search returns to the bottom. Browsing never scrolls chat.
 useLayoutEffect(()=>{
  if(!open||!list)return
  const position=()=>{list.scrollTop=query?0:list.scrollHeight}
  position()
  // Radix applies collision limits after the portal mounts. Follow that size
  // change as well, so the first opening is positioned using its final height.
  const observer=new ResizeObserver(position)
  observer.observe(list)
  return ()=>observer.disconnect()
 },[open,query,entries.length,list])
 return <Popover title={msg('Your messages')} variant="list" dismissible open={open} contentStyle={{maxWidth:width}}
  triggerTooltip={msg('Jump to message')} onOpenChange={next=>{if(next)setQuery('');onOpenChange(next)}}
  onOpenAutoFocus={event=>{event.preventDefault();input.current?.focus({preventScroll:true});if(list)list.scrollTop=list.scrollHeight}}
  onCloseAutoFocus={event=>{if(pending.current){event.preventDefault();const id=pending.current;pending.current=null;onJump(id)}}}
  onEscapeKeyDown={event=>event.stopPropagation()}
  trigger={<ChatAction variant="quiet" size="icon" type="button" aria-label={msg('Jump to message')} aria-pressed={open} disabled={disabled||!entries.length}><IconMessageList/></ChatAction>}>
  <div className={styles.search}><SearchInput ref={input} aria-label={msg('Find a message…')} placeholder={msg('Find a message…')} value={query} onChange={event=>setQuery(event.target.value)}/></div>
  <ol ref={setList} className={styles.list} aria-label={msg('Your messages')}>
   {shown.map(({turn,id,text,number})=>{const time=format(turn.at);return <li key={id}><button type="button" aria-current={currentId===id?'location':undefined} onClick={()=>{pending.current=id;onOpenChange(false)}}>
    <span className={styles.number}>{number.toString().padStart(2,'0')}</span>{' '}<span className={styles.item}><span className={styles.excerpt}>{text}</span>{' '}{time&&<time dateTime={turn.at} title={time.full}>{time.day} · {time.short}</time>}</span>
   </button></li>})}
  </ol>
  {!shown.length&&<p className={styles.empty}>{msg('No messages match this search.')}</p>}
 </Popover>
}
