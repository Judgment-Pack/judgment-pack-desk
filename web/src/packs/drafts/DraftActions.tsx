import { createContext, useContext, useRef, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { useChats } from '../../chat/ChatProvider'
import { Button } from '../../ui/Button'
import { Input } from '../../ui/Input'
import { Field } from '../../ui/Field'
import { Alert } from '../../ui/Alert'
import { Dialog, DialogActions } from '../../ui/Dialog'
import { Menu, OptionsMenu } from '../../ui/Menu'
import { DefinitionJsonDialog } from '../../definitions/DefinitionJsonDialog'
import { IconCheck, IconPencil, IconTrash, IconDetails } from '../../shell/icons'
import { msg, systemMessage, useLocale } from '../../i18n'
import { DeleteDraftsDialog, type BrowserItem } from '../CollectionActions'
import { MoveToFolderItem } from '../folders/FolderBrowser'
import type { PackDraft } from './model'

type Operation = {kind:'rename'; draft:PackDraft; opener?:HTMLElement} | {kind:'delete'; item:BrowserItem; opener?:HTMLElement; onDeleted?:()=>void}
const Operations = createContext<((operation:Operation)=>void) | undefined>(undefined)
export const useDraftActions = () => useContext(Operations)

/** Outlives the document: a delete or failed save can remove its route content. */
export function DraftActionsProvider({children}:{children:ReactNode}) {
 useLocale()
 const [operation,setOperation]=useState<Operation>(), navigate=useNavigate()
 return <Operations.Provider value={setOperation}>{children}
  {operation?.kind==='rename' && <RenameDraftDialog draft={operation.draft} opener={operation.opener} onClose={()=>setOperation(undefined)}/>}
  {operation?.kind==='delete' && <DeleteDraftsDialog items={[operation.item]} opener={operation.opener} onClose={()=>setOperation(undefined)} onDeleted={()=>{operation.onDeleted?.();navigate('/packs')}}/>}
 </Operations.Provider>
}

export function DraftActions({draft,recheck,sourceText}:{draft:PackDraft;sourceText?:string;recheck?:{run:()=>void;disabled:boolean}}) {
 useLocale()
 const {store}=useChats(), request=useDraftActions(), trigger=useRef<HTMLButtonElement>(null), handoff=useRef(false)
 const [showJson,setShowJson]=useState(false)
 const text=sourceText ?? draft.checkpoint.state.candidates.at(-1)?.text
 return <><OptionsMenu label={msg('Pack options')} triggerRef={trigger} onOpenChange={open=>{if(open)handoff.current=false}} onCloseAutoFocus={event=>{if(handoff.current)event.preventDefault()}}>
  <Menu.Item icon={<IconPencil/>} disabled={!store || !request} onSelect={()=>{handoff.current=true;request?.({kind:'rename',draft,opener:trigger.current??undefined})}}>{msg('Rename…')}</Menu.Item>
  <MoveToFolderItem id={draft.id} opener={trigger} onSelect={()=>{handoff.current=true}}/>
  <Menu.Item icon={<IconDetails/>} disabled={text === undefined} onSelect={()=>{handoff.current=true;setShowJson(true)}}>{msg('View JSON')}</Menu.Item>
  {recheck && <><Menu.Separator/><Menu.Item icon={<IconCheck/>} disabled={recheck.disabled} onSelect={recheck.run}>{msg('Run checks')}</Menu.Item></>}
  <Menu.Separator/>
  <Menu.Item icon={<IconTrash/>} destructive disabled={!store || !request} onSelect={()=>{handoff.current=true;request?.({kind:'delete',opener:trigger.current??undefined,item:{id:draft.id,title:draft.title,kind:'pack',status:'draft',href:''}})}}>{msg('Delete draft…')}</Menu.Item>
 </OptionsMenu><DefinitionJsonDialog open={showJson} onOpenChange={setShowJson} text={text ?? ''} title={msg('Pack JSON')} openerRef={trigger}/></>
}

function RenameDraftDialog({draft,opener:trigger,onClose}:{draft:PackDraft;opener?:HTMLElement;onClose:()=>void}) {
 const {store}=useChats(), opener=useRef(trigger??null)
 const [name,setName]=useState(draft.title), [busy,setBusy]=useState(false), [error,setError]=useState('')
 const save=async()=>{
  if(!store || busy || !name.trim())return
  setBusy(true);setError('')
  try {
   const current=store.getSnapshot().packDrafts.find(item=>item.id===draft.id)
   if(!current || current.finalized)throw new Error(msg('This draft is no longer available.'))
   store.renameDraft(draft.id,name)
   const settle=async()=>{if(store.getSnapshot().saving)await new Promise<void>(resolve=>{
    const unsubscribe=store.subscribe(()=>{if(!store.getSnapshot().saving){unsubscribe();resolve()}})
   })}
   await settle()
   if(store.getSnapshot().error){store.retrySave();await settle()}
   if(!await store.flush())throw new Error(store.getSnapshot().error || msg('Draft changes could not be saved. Try again.'))
   onClose()
  }catch(cause){setError(cause instanceof Error?systemMessage(cause.message):msg('Draft changes could not be saved. Try again.'))}
  finally{setBusy(false)}
 }
 return <Dialog open title={msg('Rename draft')} openerRef={opener} onOpenChange={open=>{if(!open&&!busy)onClose()}}>
  <form onSubmit={event=>{event.preventDefault();void save()}}>
   <Field label={msg('Name')}>{w=><Input {...w} autoFocus value={name} maxLength={500} disabled={busy} onFocus={event=>event.currentTarget.select()} onChange={event=>setName(event.target.value)}/>}</Field>
   {error&&<Alert>{error}</Alert>}
   <DialogActions><Button variant="quiet" disabled={busy} onClick={onClose}>{msg('Cancel')}</Button><Button type="submit" variant="primary" disabled={!store||busy||!name.trim()||(!error&&name.trim()===draft.title)}>{busy?msg('Saving…'):msg('Rename')}</Button></DialogActions>
  </form>
 </Dialog>
}
