import { PaneToggle } from '../../shell/PaneToggle'
import { useEffect, useId, useRef, useState, type ReactNode, type CSSProperties } from 'react'
import { Dialog as Drawer, DropdownMenu, VisuallyHidden } from 'radix-ui'
import { msg, useLocale } from '../../i18n'
import { PaneDivider } from '../../ui/PaneDivider'
import { useInspectorControls } from '../../shell/InspectorSlot'
import { Button } from '../../ui/Button'
import { Input } from '../../ui/Input'
import { Select } from '../../ui/Select'
import { Tooltip, OverflowTooltip } from '../../ui/Tooltip'
import { Alert } from '../../ui/Alert'
import { Dialog, DialogActions } from '../../ui/Dialog'
import { IconChevronRight, IconChevronDown, IconMore, IconPlus, IconFolder } from '../../shell/icons'
import { ALL_PACKS, HOME_FOLDER, WORKSPACE_ROOT, folderTrail, folderPath, inFolder, type PackFolder } from './model'
import { usePackFolders, type FolderState } from './FolderContext'
import styles from './FolderBrowser.module.css'

export const FolderIcon = IconFolder
export function ShowFolders() {
 const state=usePackFolders()
 return state && <PaneToggle compact label={msg('Expand folders')} expanded={false} data-show-folders
   onClick={event=>{state.drawerOpener.current=event.currentTarget;state.preferences({collapsed:false});state.setOverlay(true)}} />
}
export function NewFolderButton() {
 const state=usePackFolders()
 return state && <Button variant="quiet" disabled={!state.query.data||state.query.isError||state.mutation.isPending} onClick={event=>{state.drawerOpener.current=event.currentTarget;state.edit({kind:'create',parentId:state.selected===ALL_PACKS?null:state.selected},event.currentTarget)}}>{msg('New folder')}</Button>
}
export function FolderFrame({children}:{children:ReactNode}) {
 useLocale()
 const state=usePackFolders()!, frame=useRef<HTMLDivElement>(null)
 const [room,setRoom]=useState(0), paneId=useId()
 const slot=useInspectorControls()
 const requiredMain=Math.max(560,slot.minimumMainWidth??0)
 const docked=room>=state.prefs.width+requiredMain&&!state.prefs.collapsed
 const maxWidth=Math.min(320,Math.max(180,room-requiredMain))
 useEffect(()=>slot.requestLeadingWidth?.(docked?state.prefs.width:0),[slot.requestLeadingWidth,docked,state.prefs.width])
 const collapse=()=>{state.preferences({collapsed:true});state.setOverlay(false);requestAnimationFrame(()=>Array.from(frame.current?.querySelectorAll<HTMLButtonElement>('[data-show-folders]')??[]).find(button=>!button.closest('[hidden]'))?.focus())}
 useEffect(()=>{
  const element=frame.current
  if(!element)return
  const measure=()=>setRoom(element.getBoundingClientRect().width)
  measure(); const observer=new ResizeObserver(measure);observer.observe(element);return ()=>observer.disconnect()
 },[])
 useEffect(()=>{if(docked)state.setOverlay(false);else if(state.editing?.kind==='create'||state.editing?.kind==='rename')state.setOverlay(true)},[docked,state.editing])
 const contents=<FolderTree state={state} onClose={()=>{if(docked)collapse();else state.setOverlay(false)}}/>
 return <div className={styles.frame} ref={frame} data-folder-frame>
  {docked&&<aside id={paneId} aria-label={msg('Pack folders')} className={styles.sidebar} style={{width:`var(--folder-preview, ${state.prefs.width}px)`}}>{contents}
   <PaneDivider paneSide="start" label={msg('Resize folder pane')} controls={paneId} value={state.prefs.width} min={180} max={maxWidth}
    preview={{element:frame.current,property:'--folder-preview'}} onChange={width=>state.preferences({width})} onReset={()=>state.preferences({width:Math.min(220,maxWidth)})} onCollapse={collapse}/>
  </aside>}
  <div className={styles.content} data-folder-docked={docked||undefined}>{children}</div>
  {!docked&&<Drawer.Root open={state.overlay} onOpenChange={state.setOverlay}><Drawer.Portal><Drawer.Overlay className="desk-overlay"/><Drawer.Content className={`desk-drawer desk-pane-drawer ${styles.drawer}`} aria-describedby={undefined} onCloseAutoFocus={event=>{event.preventDefault();state.drawerOpener.current?.focus()}}><VisuallyHidden.Root><Drawer.Title>{msg('Pack folders')}</Drawer.Title></VisuallyHidden.Root>{contents}</Drawer.Content></Drawer.Portal></Drawer.Root>}
  <FolderEditor state={state} inline={false}/>
 </div>
}
function FolderTree({state,onClose}:{state:FolderState;onClose:()=>void}) {
 const busy=state.mutation.isPending, usable=!!state.query.data&&!state.query.isError
 const children=(parentId:string|null)=>state.document.folders.filter(folder=>folder.parentId===parentId).sort((a,b)=>a.id===HOME_FOLDER?-1:b.id===HOME_FOLDER?1:a.name.localeCompare(b.name))
 const node=(folder:PackFolder,depth:number):ReactNode=>{
  const nested=children(folder.id), expanded=state.prefs.expanded.includes(folder.id)
  return <li key={folder.id}><div className={styles.treeRow} data-selected={state.activeFolder===folder.id||undefined} style={{'--depth':depth} as CSSProperties}>
   <button type="button" className={styles.folder} data-folder={folder.id} aria-current={state.activeFolder===folder.id?'location':undefined} title={folder.name} onClick={()=>state.select(folder.id)}><FolderIcon/><span>{folder.name}</span></button>
   {nested.length>0&&<button className={styles.expand} type="button" aria-expanded={expanded} aria-label={expanded?msg('Collapse {{name}}',{name:folder.name}):msg('Expand {{name}}',{name:folder.name})} onClick={()=>state.preferences({expanded:expanded?state.prefs.expanded.filter(id=>id!==folder.id):[...state.prefs.expanded,folder.id]})}>{expanded?<IconChevronDown/>:<IconChevronRight/>}</button>}
   <DropdownMenu.Root><DropdownMenu.Trigger asChild><button type="button" data-folder-actions={folder.id} className={`desk-icon-button ${styles.more}`} disabled={!usable||busy} aria-label={msg('Actions for {{name}}',{name:folder.name})}><IconMore/></button></DropdownMenu.Trigger><DropdownMenu.Portal><DropdownMenu.Content className="desk-menu" align="start" sideOffset={4} onCloseAutoFocus={event=>{if(state.editing)event.preventDefault()}}>
    <DropdownMenu.Item className="desk-menu-item" onSelect={()=>state.edit({kind:'create',parentId:folder.id},window.document.querySelector<HTMLElement>(`[data-folder-actions="${folder.id}"]`)??undefined)}>{msg('New subfolder')}</DropdownMenu.Item>
    {folder.id!==HOME_FOLDER&&<><DropdownMenu.Item className="desk-menu-item" onSelect={()=>state.edit({kind:'rename',id:folder.id},window.document.querySelector<HTMLElement>(`[data-folder-actions="${folder.id}"]`)??undefined)}>{msg('Rename')}</DropdownMenu.Item><DropdownMenu.Item className="desk-menu-item" onSelect={()=>state.edit({kind:'move',id:folder.id},window.document.querySelector<HTMLElement>(`[data-folder-actions="${folder.id}"]`)??undefined)}>{msg('Move to…')}</DropdownMenu.Item><DropdownMenu.Separator className="desk-menu-separator"/><DropdownMenu.Item className="desk-menu-item" disabled={nested.length>0||Object.values(state.document.assignments).includes(folder.id)} onSelect={()=>void state.mutation.mutateAsync({action:{type:'delete',id:folder.id}}).catch(()=>{})}>{msg('Delete empty folder')}</DropdownMenu.Item></>}
   </DropdownMenu.Content></DropdownMenu.Portal></DropdownMenu.Root>
  </div>{expanded&&nested.length>0&&<ul>{nested.map(child=>node(child,depth+1))}</ul>}</li>
 }
 return <>
  <div className={`desk-pane-head ${styles.heading}`}><PaneToggle compact label={msg('Collapse folders')} expanded onClick={onClose} /><div className="desk-pane-heading"><span>{msg('Folders')}</span></div><div className={styles.headActions}><Tooltip content={msg('New workspace folder')} openOnFocus={false}><button type="button" className="desk-icon-button" aria-label={msg('New workspace folder')} disabled={!usable||busy} onClick={event=>state.edit({kind:'create',parentId:null},event.currentTarget)}><IconPlus/></button></Tooltip></div></div>
  <FolderEditor state={state} inline/>
  <nav className={styles.tree} aria-label={msg('Folder navigation')}>
   <button type="button" className={styles.all} aria-current={state.activeFolder===ALL_PACKS?'location':undefined} onClick={()=>state.select(ALL_PACKS)}>{msg('All packs')}</button>
   <ul>{children(null).map(folder=>node(folder,0))}</ul>
  </nav>
 </>
}
/** Location stays visible independently of the folder browser. Earlier ancestors
 * move into a menu as the working pane narrows; the current folder never does. */
export function FolderLocation({folderId}:{folderId?:string}={}) {
 const state=usePackFolders(), nav=useRef<HTMLElement>(null)
 const [compact,setCompact]=useState(false)
 useEffect(()=>{
  const element=nav.current
  if(!element)return
  const measure=()=>{
   const width=element.getBoundingClientRect().width
   if(width>0)setCompact(width<560)
  }
  measure()
  const observer=new ResizeObserver(measure)
  observer.observe(element)
  return ()=>observer.disconnect()
 },[Boolean(state)])
 if(!state)return null
 const current=folderId??state.selected
 const trail=current===ALL_PACKS?[]:folderTrail(state.document,current)
 const visibleCount=compact?1:2
 const hidden=trail.slice(0,Math.max(0,trail.length-visibleCount))
 const visible=trail.slice(hidden.length)
 return <nav ref={nav} className={styles.breadcrumb} aria-label={msg('Folder location')}>
  <button className={styles.pathRoot} type="button" aria-current={current===ALL_PACKS?'location':undefined} onClick={()=>state.select(ALL_PACKS)}>{msg('All packs')}</button>
  {hidden.length>0&&<span className={styles.pathOverflow}>
   <IconChevronRight/>
   <DropdownMenu.Root>
    <DropdownMenu.Trigger asChild><button className={styles.pathMenuTrigger} type="button" aria-label={msg('Show parent folders')}><IconMore/></button></DropdownMenu.Trigger>
    <DropdownMenu.Portal><DropdownMenu.Content className={`desk-menu ${styles.pathMenu}`} align="start" sideOffset={4}>
     {hidden.map(folder=><DropdownMenu.Item key={folder.id} className={`desk-menu-item ${styles.pathMenuItem}`} onSelect={()=>state.select(folder.id)}>
      <span>{folder.name}</span><small>{folderPath(state.document,folder.id)}</small>
     </DropdownMenu.Item>)}
    </DropdownMenu.Content></DropdownMenu.Portal>
   </DropdownMenu.Root>
  </span>}
  {visible.map(folder=><span key={folder.id} className={folder.id===current?styles.pathCurrent:styles.pathAncestor}>
   <IconChevronRight/>
   <OverflowTooltip content={folder.name}><button type="button" aria-current={folder.id===current?'location':undefined} onClick={()=>state.select(folder.id)}>{folder.name}</button></OverflowTooltip>
  </span>)}
 </nav>
}
export function FolderFeedback() {
 const state=usePackFolders()
 if(!state)return null
 return state.query.isError?<div className={styles.feedback}><Alert>{msg('Folder organization is unavailable. Packs are shown without folder filtering.')}</Alert><Button variant="quiet" onClick={()=>void state.reload()}>{msg('Reload folders')}</Button></div>:state.message&&!state.editing?<div className={styles.feedback}><Alert>{state.message}</Alert><Button variant="quiet" onClick={()=>{state.setMessage('');void state.reload()}}>{msg('Reload folders')}</Button></div>:null
}
export function SubfolderRows({query}:{query:string}) {
 const state=usePackFolders()
 if(!state||!state.query.data||state.query.isError||state.selected===ALL_PACKS)return null
 const folders=state.document.folders.filter(folder=>folder.id!==state.selected && (query ? inFolder(state.document,folder.id,state.selected,true)&&folder.name.toLocaleLowerCase().includes(query.toLocaleLowerCase().trim()) : folder.parentId===state.selected)).sort((a,b)=>a.name.localeCompare(b.name))
 return folders.length?<ul className={styles.subfolders} aria-label={msg('Subfolders')}>{folders.map(folder=><li key={folder.id}><button type="button" onClick={()=>state.select(folder.id)}><FolderIcon/><span>{folder.name}</span>{query&&<small>{folderPath(state.document,folder.id)}</small>}<IconChevronRight/></button></li>)}</ul>:null
}
export function MovePackButton({id}:{id:string}) {
 const state=usePackFolders()
 return state&&<Tooltip content={msg('Move {{name}} to folder',{name:id})}><button className="desk-icon-button" type="button" disabled={!state.query.data||state.query.isError||state.mutation.isPending} aria-label={msg('Move {{name}} to folder',{name:id})} onClick={event=>state.edit({kind:'pack',id},event.currentTarget)}><FolderIcon/></button></Tooltip>
}
function FolderEditor({state,inline}:{state:FolderState;inline:boolean}) {
 const edit=state.editing, {name,destination,filter}=state.editorFields
 const setName=(name:string)=>state.setEditorFields(value=>({...value,name}))
 const setDestination=(destination:string)=>state.setEditorFields(value=>({...value,destination}))
 const setFilter=(filter:string)=>state.setEditorFields(value=>({...value,filter}))
 const input=useRef<HTMLInputElement>(null), selectId=useId()
 if(!edit || inline !== (edit.kind==='create'||edit.kind==='rename'))return null
 const moving=edit.kind==='move'||edit.kind==='pack'
 const title=edit.kind==='create'?msg('New folder'):edit.kind==='rename'?msg('Rename folder'):edit.kind==='pack'?msg('Move pack'):msg('Move folder')
 const options=state.document.folders.filter(folder=>edit.kind!=='move'||!inFolder(state.document,folder.id,edit.id!,true)).map(folder=>({value:folder.id,label:folderPath(state.document,folder.id)})).sort((a,b)=>a.label.localeCompare(b.label))
 if(edit.kind!=='pack')options.unshift({value:WORKSPACE_ROOT,label:msg('Workspace folders')})
 const visible=options.filter(option=>option.value===destination||option.label.toLocaleLowerCase().includes(filter.toLocaleLowerCase()))
 const close=()=>{if(!state.mutation.isPending){state.setEditing(null);state.setMessage('');state.opener.current?.focus()}}
 const submit=async(event:React.FormEvent)=>{
  event.preventDefault(); if(state.mutation.isPending)return
  try {
   const id=crypto.randomUUID()
   const action=edit.kind==='create'?{type:'create' as const,id,name,parentId:edit.parentId??null}:edit.kind==='rename'?{type:'rename' as const,id:edit.id!,name}:edit.kind==='move'?{type:'move' as const,id:edit.id!,parentId:destination===WORKSPACE_ROOT?null:destination}:{type:'assign' as const,packId:edit.id!,folderId:destination}
   await state.saveEdit(action)
   if(edit.kind==='create'){state.preferences({selected:id,expanded:[...new Set([...state.prefs.expanded,...(edit.parentId?folderTrail(state.document,edit.parentId).map(folder=>folder.id):[])])]});state.select(id)}
   state.setEditing(null)
   state.opener.current?.focus()
  }catch{/* Feedback retains the entered name/destination. */}
 }
 const form=<form aria-label={title} onKeyDown={event=>{if(event.key==='Escape'&&inline){event.stopPropagation();close();state.opener.current?.focus()}}} onSubmit={event=>void submit(event)} className={`${styles.editor} ${inline?styles.inlineEditor:''}`}>
  {inline&&<strong>{title}</strong>}
  {moving?<><Input aria-label={msg('Find a folder')} placeholder={msg('Find a folder')} value={filter} onChange={event=>setFilter(event.target.value)}/><label htmlFor={selectId}>{msg('Destination folder')}</label><Select id={selectId} options={visible} value={destination} onValueChange={setDestination} disabled={state.mutation.isPending}/></>:<><label htmlFor={selectId}>{msg('Folder name')}</label><Input id={selectId} ref={input} autoFocus value={name} maxLength={240} onChange={event=>setName(event.target.value)} disabled={state.mutation.isPending}/><p>{edit.kind==='create'?(edit.parentId?(state.document.folders.some(folder=>folder.id===edit.parentId)?folderPath(state.document,edit.parentId):msg('This folder no longer exists. Choose another folder.')):msg('Workspace folders')):''}</p></>}
  {state.message&&<Alert>{state.message}<Button variant="quiet" onClick={()=>void state.reload()}>{msg('Reload folders')}</Button></Alert>}
  <DialogActions><Button variant="quiet" disabled={state.mutation.isPending} onClick={close}>{msg('Cancel')}</Button><Button variant="primary" type="submit" disabled={state.mutation.isPending||!state.query.data||state.query.isError||(moving?!options.some(option=>option.value===destination):!name.trim())}>{state.mutation.isPending?msg('Saving…'):moving?msg('Move'):edit.kind==='create'?msg('Create folder'):msg('Save')}</Button></DialogActions>
 </form>
 return inline?form:<Dialog open title={title} openerRef={state.opener} onOpenChange={open=>{if(!open)close()}}>{form}</Dialog>
}
