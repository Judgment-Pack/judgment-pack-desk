import { useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { useChats } from '../../chat/ChatProvider'
import { Button } from '../../ui/Button'
import { Input } from '../../ui/Input'
import { Popover } from '../../ui/Popover'
import { IconMore } from '../../shell/icons'
import { msg, useLocale } from '../../i18n'
import type { PackDraft } from './model'

export function DraftActions({draft,children,onChat}:{draft:PackDraft;children?:ReactNode;onChat:()=>void}) {
 useLocale()
 const {store}=useChats(),navigate=useNavigate()
 const [name,setName]=useState(draft.title),[confirm,setConfirm]=useState(false),[open,setOpen]=useState(false)
 return <Popover title={msg('Draft')} open={open} onOpenChange={value=>{setOpen(value);setName(draft.title);setConfirm(false)}} trigger={<button type="button" className="desk-icon-button" aria-label={msg('Draft actions')}><IconMore/></button>}>
  <div><Button variant="quiet" onClick={()=>{setOpen(false);onChat()}}>{msg("Chat")}</Button>{children}</div>
  <form onSubmit={event=>{event.preventDefault();store?.renameDraft(draft.id,name);setOpen(false)}}>
   <label htmlFor={`draft-title-${draft.id}`}>{msg('Name')}</label>
   <Input id={`draft-title-${draft.id}`} value={name} maxLength={500} onChange={event=>setName(event.target.value)}/>
   <Button type="submit" disabled={!name.trim()}>{msg('Rename')}</Button>
  </form>
  {confirm ? <><p>{msg('Delete this draft? Its conversations will remain.')}</p><Button variant="danger" onClick={()=>{if(store?.removeDraft(draft.id)){setOpen(false);navigate('/packs')}}}>{msg('Delete draft')}</Button><Button onClick={()=>setConfirm(false)}>{msg('Cancel')}</Button></> : <Button variant="quiet" onClick={()=>setConfirm(true)}>{msg('Delete draft')}</Button>}
 </Popover>
}
