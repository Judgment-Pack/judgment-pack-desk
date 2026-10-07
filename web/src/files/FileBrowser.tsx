import { useLayoutEffect, useMemo, useRef, type CSSProperties, type KeyboardEvent } from 'react'
import { msg } from '../i18n'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { OverflowTooltip } from '../ui/Tooltip'
import { IconChevronRight, IconChevronDown, IconFolder, IconDetails } from '../shell/icons'
import type { FileEntry } from './client'
import { fileTree, type FileNode } from './browserState'
import styles from './FileBrowser.module.css'

export function FileBrowser({files,selected,dirty,expanded,search,scroll,onSearch,onExpand,onChoose,onScroll}:{
 files:readonly FileEntry[];selected?:string;dirty:boolean;expanded:readonly string[];search:string;scroll:number
 onSearch:(value:string)=>void;onExpand:(paths:string[])=>void;onChoose:(path:string)=>void;onScroll:(value:number)=>void
}){
 const host=useRef<HTMLDivElement>(null),scroller=useRef<HTMLDivElement>(null)
 const tree=useMemo(()=>fileTree(files),[files])
 const query=search.trim().toLocaleLowerCase()
 const matches=useMemo(()=>files.filter(file=>file.path.toLocaleLowerCase().includes(query)).sort((a,b)=>a.path.localeCompare(b.path,undefined,{numeric:true})),[files,query])
 const toggle=(path:string)=>onExpand(expanded.includes(path)?expanded.filter(item=>item!==path):[...expanded,path])
 useLayoutEffect(()=>{if(scroller.current)scroller.current.scrollTop=scroll},[scroll])
 const keyboard=(event:KeyboardEvent)=>{
  const target=event.target as HTMLElement
  if(!target.matches('button[data-file-row]'))return
  const buttons=[...host.current!.querySelectorAll<HTMLButtonElement>('button[data-file-row]')],index=buttons.indexOf(target as HTMLButtonElement)
  let next:number|undefined
  if(event.key==='ArrowDown')next=Math.min(index+1,buttons.length-1)
  else if(event.key==='ArrowUp')next=Math.max(0,index-1)
  else if(event.key==='Home')next=0
  else if(event.key==='End')next=buttons.length-1
  else if(event.key==='ArrowRight'&&target.dataset.directory){
   if(target.getAttribute('aria-expanded')==='false')toggle(target.dataset.fileRow!);else next=Math.min(index+1,buttons.length-1)
  }else if(event.key==='ArrowLeft'&&!query){
   if(target.getAttribute('aria-expanded')==='true')toggle(target.dataset.fileRow!)
   else {const parent=target.dataset.fileRow!.split('/').slice(0,-1).join('/');next=buttons.findIndex(button=>button.dataset.fileRow===parent)}
  }else return
  event.preventDefault();if(next!==undefined&&next>=0)buttons[next]?.focus()
 }
 const fileRow=(path:string,name:string,depth:number,parent?:string)=><OverflowTooltip selector="[data-file-label]" content={path}>
  <button type="button" data-file-row={path} className={styles.row} style={{'--depth':depth} as CSSProperties} aria-label={path} aria-current={selected===path?true:undefined} onClick={()=>onChoose(path)}>
   <IconDetails/><span className={styles.text}><span data-file-label>{name}</span>{parent&&<small data-file-label>{parent}</small>}</span>
   {dirty&&selected===path&&<span className={styles.dirty} role="img" aria-label={msg('unsaved changes')}>●</span>}
  </button>
 </OverflowTooltip>
 const node=(item:FileNode,depth:number):React.ReactNode=><li key={item.path}>
  {item.directory?<><OverflowTooltip selector="[data-file-label]" content={item.path}>
   <button type="button" data-file-row={item.path} data-directory className={styles.row} style={{'--depth':depth} as CSSProperties} aria-label={item.path} aria-expanded={expanded.includes(item.path)} onClick={()=>toggle(item.path)}>
    <IconFolder/><span className={styles.text} data-file-label>{item.name}</span><span className={styles.disclosure}>{expanded.includes(item.path)?<IconChevronDown/>:<IconChevronRight/>}</span>
   </button></OverflowTooltip>{expanded.includes(item.path)&&<ul>{item.children.map(child=>node(child,depth+1))}</ul>}</>:fileRow(item.path,item.name,depth)}
 </li>
 return <div className={styles.browser} ref={host}>
  <div className={styles.search}><Input type="search" aria-label={msg('Search files')} placeholder={msg('Search files…')} value={search} onChange={event=>onSearch(event.target.value)} onKeyDown={event=>{if(event.key==='Escape'){event.preventDefault();onSearch('')}else if(event.key==='ArrowDown'){event.preventDefault();host.current?.querySelector<HTMLButtonElement>('[data-file-row]')?.focus()}}}/></div>
  {query&&<div className={styles.results}><span role="status">{msg('{{matches}} of {{total}} files',{matches:matches.length,total:files.length})}</span><Button variant="inline" onClick={()=>onSearch('')}>{msg('Clear')}</Button></div>}
  <div ref={scroller} className={styles.scroll} onScroll={event=>onScroll(event.currentTarget.scrollTop)}>
   <nav aria-label={msg('Files')} onKeyDown={keyboard}><ul className={styles.list}>{query?matches.map(file=><li key={file.path}>{fileRow(file.path,file.path.split('/').at(-1)!,0,file.path.includes('/')?file.path.split('/').slice(0,-1).join('/'):undefined)}</li>):tree.map(item=>node(item,0))}</ul></nav>
   {query&&!matches.length&&<p className={styles.empty}>{msg('No matching files.')}</p>}
  </div>
 </div>
}
