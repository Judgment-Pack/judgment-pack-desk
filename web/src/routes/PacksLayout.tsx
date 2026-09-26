/** Retain collection controls while a pack is open. Folders organize the project without moving its files. */
import { useEffect, useRef, useState } from 'react'
import { Outlet, useParams, useLocation, useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { PacksPane } from '../packs/PacksPane'
import styles from './PacksLayout.module.css'
import { PackFoldersProvider, usePackFolders } from '../packs/folders/FolderContext'
import { FolderFrame, FolderLocation, FolderFeedback, MovePackButton, ShowFolders } from '../packs/folders/FolderBrowser'
import { packFolder } from '../packs/folders/model'
import { assignCreatedPack, FOLDERS_KEY } from '../packs/folders/client'
import { Alert } from '../ui/Alert'
import { Button } from '../ui/Button'
import { msg, systemMessage, useLocale } from '../i18n'

export function PacksLayout() {
  return <div className={styles.layout} data-measure="full" data-layout="page"><PackFoldersProvider><FolderFrame><PacksContent/></FolderFrame></PackFoldersProvider></div>
}
function PacksContent() {
 useLocale()
 const {packId,draftId}=useParams(), folders=usePackFolders()!
 const location=useLocation(), open=Boolean(packId||draftId||location.pathname==='/packs/new')
 const selected=folders.query.data?packFolder(folders.document,packId??''):undefined
 return <>
  <div className={styles.collection} hidden={open}><PacksPane active={!open}/></div>
  <div className={styles.main} hidden={!open}>
   {packId&&<><div className={styles.location}><ShowFolders/><FolderLocation folderId={selected}/><MovePackButton id={packId}/></div><FolderFeedback/><AssignmentRecovery packId={packId}/></>}
   <div className={styles.document}><Outlet/></div>
  </div>
 </>
}
function AssignmentRecovery({packId}:{packId:string}) {
 const location=useLocation(), navigate=useNavigate(), client=useQueryClient()
 const currentLocation=useRef<string|null>(location.key)
 useEffect(()=>{currentLocation.current=location.key;return ()=>{currentLocation.current=null}},[location.key])
 const [busy,setBusy]=useState(false), [error,setError]=useState('')
 const request=location.state?.folderAssignment as {packId?:unknown;folderId?:unknown}|undefined
 const folderId=request?.folderId
 if(request?.packId!==packId||typeof folderId!=='string'||!/^[a-zA-Z0-9-]{1,80}$/.test(folderId))return null
 const dismiss=()=>{if(currentLocation.current!==location.key)return;const state={...location.state};delete state.folderAssignment;navigate(`${location.pathname}${location.search}${location.hash}`,{replace:true,state})}
 const retry=async()=>{
  setBusy(true);setError('')
  try {client.setQueryData(FOLDERS_KEY,await assignCreatedPack(packId,folderId));dismiss()}
  catch(cause){setError(cause instanceof Error?systemMessage(cause.message):msg('Could not save folder.'))}
  finally{setBusy(false)}
 }
 return <div className={styles.recovery}><Alert reason={error||undefined}>{msg('Pack created, but its folder could not be saved. It is available in All packs.')}</Alert><Button disabled={busy} onClick={()=>void retry()}>{busy?msg('Saving…'):msg('Retry folder assignment')}</Button><Button variant="quiet" disabled={busy} onClick={dismiss}>{msg('Dismiss')}</Button></div>
}
