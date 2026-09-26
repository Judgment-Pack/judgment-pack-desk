import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { usePacks, useGraphInventory } from '../mcp/queries'
import type { LoadedPack } from '../mcp/types'
import { useFileListing } from '../files/queries'
import { useChats } from '../chat/ChatProvider'
import { chatTitle } from '../chat/navigation'
import { draftHref } from '../packs/drafts/model'
import { FOLDERS_KEY, loadFolders } from '../packs/folders/client'
import { folderPath } from '../packs/folders/model'
import { msg } from '../i18n'
import { Button } from '../ui/Button'
import { Dialog } from '../ui/Dialog'
import { Input } from '../ui/Input'
import { Tooltip } from '../ui/Tooltip'
import { IconSearch } from './icons'
import { isTypingTarget } from './shortcuts'
import { readRecent, searchItems, switchHref, type SwitchItem } from './quickSwitch'
import styles from './QuickSwitcher.module.css'
export function QuickSwitcher() {
  const [open,setOpen] = useState(false)
  const opener = useRef<HTMLElement|null>(null), button = useRef<HTMLButtonElement|null>(null)
  const root = useFileListing().data?.root, location = useLocation()
  const key = root ? `jpack.quick-switch.v1:${root}` : undefined
  useEffect(() => {
    const href = switchHref(location.pathname, location.search)
    if (key && href) try { localStorage.setItem(key,JSON.stringify([href,...readRecent(key).filter(x => x !== href)].slice(0,20))) } catch { /* Optional preference. */ }
  },[key,location.pathname,location.search])
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.repeat || event.isComposing || event.ctrlKey === event.metaKey || event.altKey || event.shiftKey || event.key.toLowerCase() !== 'k' || isTypingTarget(event.target) || document.querySelector('[data-modal-surface]')) return
      event.preventDefault(); opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : button.current; setOpen(true)
    }
    document.addEventListener('keydown',onKey);return () => document.removeEventListener('keydown',onKey)
  },[])
  return <>
    <Tooltip content={msg('Search and switch · Ctrl/Cmd+K')}><Button ref={button} size="icon" variant="quiet" aria-label={msg('Search and switch')} onClick={event => {opener.current=event.currentTarget;setOpen(true)}}><IconSearch/></Button></Tooltip>
    <Dialog open={open} onOpenChange={setOpen} title={msg('Search and switch')} openerRef={opener}>
      {open && <SwitchResults recentKey={key} close={() => setOpen(false)}/>}
    </Dialog>
  </>
}
function SwitchResults({ recentKey,close }: { recentKey?:string;close:()=>void }) {
  const packs=usePacks(), graphs=useGraphInventory(), chats=useChats(), queryClient=useQueryClient(), navigate=useNavigate()
  const folders=useQuery({queryKey:FOLDERS_KEY,queryFn:({signal})=>loadFolders(signal),retry:false})
  const [query,setQuery]=useState(''), [selected,setSelected]=useState(0)
  const id=useId(), input=useRef<HTMLInputElement|null>(null)
  const items=useMemo(() => {
    const rows:SwitchItem[]=[]
    for (const pack of packs.data?.packs??[]) {
      const loaded=queryClient.getQueryData<LoadedPack>(['get_pack',pack.id])
      const draft=chats.packDrafts.find(d=>d.finalized?.id===pack.id)
      rows.push({id:`pack:${pack.id}`,title:String(loaded?.document?.title??draft?.title??pack.id),kind:msg('Pack'),detail:`${pack.id} ${pack.description??''}`,href:`/packs/${encodeURIComponent(pack.id)}`})
    }
    for (const draft of chats.packDrafts.filter(d=>!d.finalized)) rows.push({id:draft.id,title:draft.title,kind:msg('Draft'),detail:'',href:draftHref(draft.id)})
    for (const graph of graphs.data?.graphs??[]) rows.push({id:`graph:${graph.id}`,title:graph.id,kind:msg('Graph'),detail:graph.description??'',href:`/graphs/${encodeURIComponent(graph.id)}`})
    for (const chat of chats.chats) rows.push({id:`chat:${chat.id}`,title:chatTitle(chat),kind:chat.archived?msg('Archived chat'):msg('Chat'),detail:'',href:`/chats/${encodeURIComponent(chat.id)}`})
    if(folders.data) for(const folder of folders.data.document.folders) rows.push({id:`folder:${folder.id}`,title:folder.name,kind:msg('Folder'),detail:folderPath(folders.data.document,folder.id),href:`/packs?folder=${encodeURIComponent(folder.id)}`})
    return rows
  },[packs.data,graphs.data,chats.packDrafts,chats.chats,folders.data,queryClient])
  const results=searchItems(items,query,recentKey?readRecent(recentKey):[])
  const index=Math.min(selected,Math.max(0,results.length-1))
  useEffect(()=>{input.current?.focus()},[])
  useEffect(()=>{document.getElementById(`${id}-${index}`)?.scrollIntoView?.({block:'nearest'})},[index,id])
  const choose=(item:SwitchItem)=>{close();navigate(item.href)}
  return <div className={styles.root}>
    <Input ref={input} role="combobox" aria-label={msg('Search packs, folders, drafts, graphs and chats')} aria-controls={id} aria-expanded="true" aria-autocomplete="list" aria-activedescendant={results.length?`${id}-${index}`:undefined}
      placeholder={msg('Search packs, folders, drafts, graphs and chats')} value={query}
      onChange={e=>{setQuery(e.target.value);setSelected(0)}} onKeyDown={e=>{
        if(e.nativeEvent.isComposing)return
        if(e.key==='ArrowDown'||e.key==='ArrowUp'){e.preventDefault();setSelected((index+(e.key==='ArrowDown'?1:-1)+results.length)%Math.max(1,results.length))}
        if(e.key==='Enter'&&results[index]){e.preventDefault();choose(results[index])}
      }}/>
    <p className={styles.hint}>{query?msg('Matching items'):msg('Recently visited items appear first.')}</p>
    {(packs.error||graphs.error||folders.error||chats.error) && <p role="status">{msg('Some items could not be loaded. Available results are shown.')} <Button variant="inline" onClick={()=>{void packs.refetch();void graphs.refetch();void folders.refetch();void chats.store?.load()}}>{msg('Retry')}</Button></p>}
    <div id={id} role="listbox" aria-label={msg('Search results')} className={styles.results}>
      {results.map((item,i)=><button key={item.id} id={`${id}-${i}`} role="option" aria-selected={index===i} tabIndex={-1} type="button" className={styles.item} onClick={()=>choose(item)} onMouseMove={()=>setSelected(i)}>
        <span><strong>{item.title}</strong><small>{item.detail}</small></span><span className={styles.kind}>{item.kind}</span>
      </button>)}
    </div>
    {!results.length && <p role="status">{packs.isPending||!chats.ready?msg('Loading items…'):msg('No matching items.')}</p>}
    <Button variant="quiet" onClick={close}>{msg('Close')}</Button>
  </div>
}
