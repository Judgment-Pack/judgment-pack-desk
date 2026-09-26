import { useChats } from '../../chat/ChatProvider'
import { createContext, useContext, useRef, useState, useEffect, type ReactNode } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useLocation, useParams } from 'react-router-dom'
import { useFileListing } from '../../files/queries'
import { msg, systemMessage } from '../../i18n'
import { FOLDERS_KEY, loadFolders, saveFolderAction, type FolderSnapshot } from './client'
import { ALL_PACKS, defaultFolders, HOME_FOLDER, WORKSPACE_ROOT, packFolder, folderTrail, type FolderAction } from './model'
interface Preferences { selected: string; expanded: string[]; width: number; collapsed: boolean }
const defaults: Preferences = { selected: HOME_FOLDER, expanded: [HOME_FOLDER], width: 220, collapsed: false }
function read(key: string | undefined): Preferences {
 try {
  const raw = key && localStorage.getItem(key)
  if (!raw || raw.length>100000) return defaults
  const value=JSON.parse(raw)
  return { selected: typeof value.selected==='string' ? value.selected : HOME_FOLDER, expanded: Array.isArray(value.expanded) ? value.expanded.filter((id:unknown)=>typeof id==='string').slice(0,1000) : [HOME_FOLDER], width: Number.isFinite(value.width) ? Math.max(180,Math.min(320,value.width)) : 220, collapsed: value.collapsed === true }
 } catch { return defaults }
}
function useFolderState(storageKey?: string) {
 const client=useQueryClient(), navigate=useNavigate(), location=useLocation()
 const query=useQuery({queryKey:FOLDERS_KEY,queryFn:({signal})=>loadFolders(signal),retry:false,staleTime:0,refetchOnWindowFocus:true})
 const {packDrafts,deletedDrafts}=useChats()
 const raw=query.data?.document ?? defaultFolders()
 const assignments={...raw.assignments}
 for(const draft of packDrafts) {
  const folder=raw.folders.some(item=>item.id===draft.folderId)?draft.folderId:HOME_FOLDER
  if(!Object.hasOwn(assignments,draft.id))assignments[draft.id]=folder
  if(draft.finalized && !Object.hasOwn(assignments,draft.finalized.id))assignments[draft.finalized.id]=assignments[draft.id]!
 }
 for(const id of deletedDrafts)delete assignments[id]
 const document={...raw,assignments}
 const [settings,setSettings]=useState(()=>({key:storageKey,value:read(storageKey)})), [message,setMessage]=useState('')
 if(settings.key!==storageKey)setSettings({key:storageKey,value:read(storageKey)})
 const prefs=settings.value
 const [editorFields,setEditorFields]=useState({name:'',destination:WORKSPACE_ROOT,filter:''})
 const [editing,setEditing]=useState<{kind:'create'|'rename'|'move'|'pack'; id?:string; parentId?:string|null}|null>(null)
 const [overlay,setOverlay]=useState(false)
 const opener=useRef<HTMLElement|null>(null), drawerOpener=useRef<HTMLElement|null>(null)
 const editorBase=useRef<FolderSnapshot|undefined>(undefined)
 const mutation=useMutation({mutationFn:async({action,before}:{action:FolderAction;before?:FolderSnapshot})=> {
  if (!query.data || query.isError) throw new Error(msg('Reload folders before making changes.'))
  return saveFolderAction(before??{...query.data,document},action)
 },onSuccess: snapshot=>{client.setQueryData(FOLDERS_KEY,snapshot);void client.invalidateQueries({queryKey:['desk-files']});setMessage('')},onError:error=>setMessage(systemMessage(error.message))})
 const selected=!document.folders.some(folder=>folder.id===prefs.selected) && prefs.selected!==ALL_PACKS ? HOME_FOLDER : prefs.selected
 const preferences=(patch:Partial<Preferences>)=>setSettings(current=> {
  const next={...current.value,...patch}
  try { if(storageKey)localStorage.setItem(storageKey,JSON.stringify(next)) } catch { /* Optional layout preference. */ }
  return {key:storageKey,value:next}
 })
 useEffect(()=>{
  if(location.pathname!=='/packs'||!query.data)return
  const id=new URLSearchParams(location.search).get('folder')
  if(id && id===ALL_PACKS)preferences({selected:id});else if(id && document.folders.some(f=>f.id===id))preferences({selected:id,expanded:[...new Set([...prefs.expanded,...folderTrail(document,id).map(f=>f.id)])]})
 },[location.pathname,location.search,query.data])
 const select=(id:string)=>{preferences({selected:id});setOverlay(false);if(location.pathname!=='/packs'||new URLSearchParams(location.search).has('folder'))navigate(`/packs?folder=${encodeURIComponent(id)}`)}
 const edit=(request:NonNullable<typeof editing>,element?:HTMLElement)=>{
  opener.current=element??documentElement();editorBase.current=query.data?{...query.data,document}:undefined;setMessage('')
  const folder=document.folders.find(folder=>folder.id===request.id)
  setEditorFields({name:request.kind==='rename'?folder?.name??'':'',destination:request.kind==='pack'?packFolder(document,request.id!):request.kind==='move'?folder?.parentId??WORKSPACE_ROOT:request.parentId??WORKSPACE_ROOT,filter:''})
  setEditing(request)
 }
 const reload=async()=>{const result=await query.refetch();if(result.data&&!result.isError)editorBase.current=result.data}
 const saveEdit=(action:FolderAction)=>mutation.mutateAsync({action,before:editorBase.current})
 const {packId:finalId,draftId}=useParams()
 const packId=finalId??draftId
 const activeFolder=packId&&query.data?packFolder(document,packId):selected
 useEffect(()=>{if(packId&&query.data)preferences({expanded:[...new Set([...prefs.expanded,...folderTrail(document,activeFolder).map(folder=>folder.id)])]})},[packId,activeFolder,settings.key])
 return {query,document,prefs,selected,activeFolder,editorFields,setEditorFields,drawerOpener,reload,saveEdit,preferences,select,editing,setEditing,edit,mutation,message,setMessage,overlay,setOverlay,opener}
}
function documentElement() { return window.document.activeElement instanceof HTMLElement ? window.document.activeElement : null }
export type FolderState=ReturnType<typeof useFolderState>
const Context=createContext<FolderState|null>(null)
export const usePackFolders=()=>useContext(Context)
function Scoped({children,storageKey}:{children:ReactNode;storageKey?:string}) { const value=useFolderState(storageKey);return <Context.Provider value={value}>{children}</Context.Provider> }
export function PackFoldersProvider({children}:{children:ReactNode}) {
 const root=useFileListing().data?.root
 const key=root ? `jpack.pack-folders.view.v1:${root}` : undefined
 return <Scoped storageKey={key}>{children}</Scoped>
}
