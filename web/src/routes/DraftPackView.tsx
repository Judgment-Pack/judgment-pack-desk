import { useEffect, useRef, useState } from 'react'
import { Navigate, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useChats } from '../chat/ChatProvider'
import { restoreLedger } from '../chat/checkpoint'
import { draftHref, latestText, type PackDraft } from '../packs/drafts/model'
import { useResearchRun } from '../research/useResearchRun'
import { DraftWorkspace } from './ChatWorkspace'
import { Button } from '../ui/Button'
import { msg, systemMessage, useLocale } from '../i18n'
import { ChatPanel } from '../chat/ChatPanel'
import { useInspectorPortal } from '../shell/InspectorSlot'
import { useInspectorPresentation } from '../shell/InspectorPresentation'
import { useMemo } from 'react'
import { PageHeader } from '../ui/PageLayout'
import { usePackFolders } from '../packs/folders/FolderContext'
import { HOME_FOLDER } from '../packs/folders/model'
import { useShellState } from '../shell/paneState'
import styles from '../chat/ChatWorkspace.module.css'

export function DraftPackView() {
  useLocale()
  const {draftId} = useParams(), {packDrafts,ready,error,store,dirty,saving} = useChats()
  const artifact = packDrafts.find(item=>item.id===draftId)
  if(!ready || error) return <div className={styles.blank} role="status">{error ? systemMessage(error) : msg('Loading packs…')}{error&&<Button onClick={()=>ready?store?.retrySave():void store?.load()}>{msg('Retry')}</Button>}</div>
  if(!artifact) return <div className={styles.blank}><p>{msg('This draft is no longer available.')}</p><Button onClick={()=>window.history.back()}>{msg('Back')}</Button></div>
  if(artifact.finalized && !dirty && !saving) return <Navigate replace to={`/packs/${encodeURIComponent(artifact.finalized.id)}`} />
  return <DraftReader key={`${artifact.id}:${latestText(artifact)}:${JSON.stringify(artifact.checkpoint.state.cases)}:${JSON.stringify(artifact.checkpoint.state.expectationIssues)}`} artifact={artifact}/>
}
function DraftReader({artifact}:{artifact:PackDraft}) {
  const {store,chats,drafts}=useChats(), [params]=useSearchParams()
  const fallback=useResearchRun({mode:artifact.mode})
  const restored=useRef(false), [problem,setProblem]=useState('')
  const available=[...chats,...drafts].filter(chat=>chat.draftId===artifact.id && !chat.archived)
  const selected=available.find(chat=>chat.id===params.get('chat')) ?? available.find(chat=>chat.draftGeneration===artifact.generation && chat.checkpoint?.state.candidates.at(-1)?.text===latestText(artifact))
  useEffect(()=>{
    if(restored.current || !fallback.run || !fallback.ledger)return
    restored.current=true
    try { restoreLedger(fallback.ledger,artifact.checkpoint.sources); void fallback.run.restore(artifact.checkpoint.state).catch(error=>setProblem(error.message)) }
    catch(error){setProblem((error as Error).message)}
  },[fallback.run,fallback.ledger,artifact])
  useEffect(()=>{
    if(!store)return
    if(selected)store.activate(selected.id)
    else store.startChat(undefined,artifact.mode,true,artifact.id)
  },[store,selected?.id,artifact.id,artifact.mode])
  if(problem)return <p role="alert">{systemMessage(problem)}</p>
  if(!selected)return <p role="status">{msg('Loading chats…')}</p>
  return <DraftWorkspace chat={selected} artifact={artifact} fallback={fallback}/>
}

/** Opening Create is ephemeral; only a produced candidate becomes a draft. */
export function NewPackView() {
  const locale=useLocale(), {store,ready,chats,drafts}=useChats(), [params]=useSearchParams(), navigate=useNavigate()
  const folders=usePackFolders(), shell=useShellState()
  const folderId=params.get('folder') ?? (folders?.selected==='all'?HOME_FOLDER:folders?.selected) ?? HOME_FOLDER
  const mode=params.get('mode')==='research'?'research':'draft'
  const created=useRef<string|null>(null), [id,setId]=useState<string|null>(null), [open,setOpen]=useState(true)
  useEffect(()=>{
    if(!store||!ready||created.current)return
    const chat=store.startChat(undefined,mode,true)
    created.current=chat.id;store.update(chat.id,{targetFolderId:folderId});setId(chat.id)
  },[store,ready,folderId,mode])
  const chat=[...chats,...drafts].find(item=>item.id===id)
  useEffect(()=>{if(chat?.draftId)navigate(draftHref(chat.draftId),{replace:true})},[chat?.draftId,navigate])
  const presentation=useMemo(()=>({title:msg('Assistant'),workspaceTools:true,available:true,open,onOpenChange:setOpen,width:shell.inspectorWidth??400,onResize:shell.resizeInspector,onReset:shell.resetInspectorWidth,minimumMainWidth:480,maximumWidth:640}),[open,shell.inspectorWidth,shell.resizeInspector,shell.resetInspectorWidth,locale])
  useInspectorPresentation(presentation)
  const portal=useInspectorPortal(chat?<ChatPanel chat={chat} placement="pane"/>:null)
  return <><PageHeader title={msg('Create pack')}/><section className={styles.blank}><h2>{msg('What should this pack decide?')}</h2><p>{msg('Describe the decision, the information it needs, and the possible outcomes.')}</p><p>{msg('Your draft will appear in Packs after the first proposal.')}</p></section>{portal}</>
}
