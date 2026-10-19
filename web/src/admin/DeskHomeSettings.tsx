import {LoadingState} from '../ui/LoadingState'
import { useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { answer, deskFetch, FileRequestError, StaleWrite } from '../files/client'
import { useChats } from '../chat/ChatProvider'
import { msg, systemMessage, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { SettingsSection } from '../ui/SettingsSection'
import { SettingsFeedback } from '../ui/SettingsFeedback'
import { FieldGroup } from '../ui/Field'
import { IconFolder } from '../shell/icons'
import { DeskStatusNotice } from '../shell/DeskNotices'
import { useConfirmDiscard, useHasPendingChanges } from '../shell/UnsavedChanges'
import { useDirtyGuard } from '../shell/useDirtyGuard'
import { CardField } from './SourceCard'
import styles from './DeskHomeSettings.module.css'

interface DeskFolder { id: string; name: string; folder: string }
interface HomeStatus { path: string; defaultPath: string; current: DeskFolder; desks: DeskFolder[]; canMove: boolean; restartRequired: boolean; destination?: string }
interface HomePreview { destination: string; moves: {label: string; from: string; to: string}[]; blockers: string[]; token: string }
export const DESK_HOME_KEY = ['desk-home'] as const
// Bound the whole operation, including session bootstrap and response-body reads.
// A late bootstrap receives an already aborted signal and cannot submit a move.
async function homeRequest<T>(path: string, body?: unknown): Promise<T> {
  const controller = new AbortController()
  let timer: ReturnType<typeof setTimeout>
  const expired = new Promise<never>((_, reject) => {
    timer = setTimeout(() => { controller.abort(); reject(new Error(msg('Desk locations could not be read.'))) }, 30_000)
  })
  try {
    const request = async () => answer<T>(await deskFetch(path, {signal:controller.signal, ...(body === undefined ? {} : {method:'POST', headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})}))
    return await Promise.race([request(), expired])
  } finally { clearTimeout(timer!) }
}
async function readHome() { return homeRequest<HomeStatus>('/api/desk-home') }
export function useDeskHome() { return useQuery({queryKey: DESK_HOME_KEY, queryFn: readHome, retry: false, staleTime: 5_000, refetchInterval: query => query.state.data?.restartRequired ? false : 10_000}) }
export function DeskHomeNotice() {
  useLocale()
  const {data} = useDeskHome()
  if (!data?.restartRequired) return null
  return <DeskStatusNotice id="desk-home-move" priority="restart" tone="warning" label={msg('Restart required · Desk home move')} title={msg('Desk home move')}>
    <p>{msg('Desk is closing its connections and moving the reviewed folders. Start Desk again when the process finishes. Refreshing this page does not restart Desk.')}</p>
    <p className={styles.path}>{data.destination}</p>
  </DeskStatusNotice>
}
export function DeskHomeSettings() {
  useLocale()
  const home = useDeskHome(), client = useQueryClient(), chats = useChats()
  const opener = useRef<HTMLButtonElement>(null), chooser = useRef<HTMLButtonElement>(null), picker = useRef<AbortController | null>(null)
  const unsaved = useHasPendingChanges(), discard = useConfirmDiscard()
  const [open,setOpen] = useState(false), [destination,setDestination] = useState(''), [preview,setPreview] = useState<HomePreview | null>(null)
  const [busy,setBusy] = useState(false), [picking,setPicking] = useState(false), [error,setError] = useState(''), [uncertain,setUncertain] = useState(false)
  const working = busy || picking
  const chatBlocked = Boolean(chats.store?.running || chats.saving || chats.dirty || chats.error)
  const clearGuard = useDirtyGuard(open && Boolean(destination || picking),msg('Discard this move setup?'),{busy:working})
  useEffect(() => () => picker.current?.abort(),[])
  async function close() {
    if (busy || destination && !await discard(msg('Discard this move setup?'))) return
    picker.current?.abort(); picker.current = null; setPicking(false); clearGuard(); setOpen(false)
  }
  async function choose() {
    if (working) return
    const controller = new AbortController(); picker.current = controller; setPicking(true); setError('')
    try {
      const result = await answer<{destination?:string;canceled?:boolean}>(await deskFetch('/api/desk-home/choose-folder',{method:'POST',signal:controller.signal}))
      if (picker.current !== controller || result.canceled) return
      if (!result.destination) throw new Error(msg('The selected folder is unavailable.'))
      setDestination(result.destination);setPreview(null)
    } catch(cause) { if (!controller.signal.aborted) setError((cause as Error).message) }
    finally { if (picker.current === controller) {picker.current=null;setPicking(false);requestAnimationFrame(() => chooser.current?.focus())} }
  }
  async function openFolder(scope:'home'|'current') {
    setError('')
    try { await answer(await deskFetch('/api/desk-home/open-folder',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({scope})})) }
    catch(cause) {setError((cause as Error).message)}
  }
  async function submit() {
    if (working || uncertain || chatBlocked || !destination || preview?.blockers.length) return
    setBusy(true);setError('')
    const confirming = preview !== null
    let submitted = false
    try {
      if (confirming && chats.store) {
        const latest = chats.store.getSnapshot()
        if (!latest.ready || latest.saving || latest.dirty || latest.error || chats.store.running) throw new Error(msg('Save or recover unsaved chat changes before moving.'))
      }
      submitted = true
      const response = await homeRequest<HomePreview | {restartRequired:boolean;destination:string}>(`/api/desk-home/${confirming?'move':'preview'}`,{destination,token:preview?.token??''})
      if (confirming) {
        const result = response as {restartRequired:boolean;destination:string}
        if (!result.restartRequired || result.destination!==destination) throw new Error(msg('The move result could not be confirmed.'))
        client.setQueryData<HomeStatus>(DESK_HOME_KEY,previous => previous?{...previous,...result}:previous)
        clearGuard();setOpen(false)
      } else setPreview(response as HomePreview)
    } catch(cause) {
      setPreview(null)
      if (confirming && submitted && !(cause instanceof StaleWrite) && !(cause instanceof FileRequestError && cause.status>=400 && cause.status<500)) {
        setUncertain(true);setError(msg('Desk could not confirm whether the move started. Check the move status before trying again.'))
      } else setError((cause as Error).message)
    } finally {setBusy(false)}
  }
  async function checkStatus() {
    setBusy(true)
    try {
      const latest=await readHome();client.setQueryData(DESK_HOME_KEY,latest)
      if (latest.restartRequired){clearGuard();setOpen(false)}else{setUncertain(false);setError('')}
    }catch{setError(msg('Desk is unavailable. Check the local Desk process before restarting or attempting another move.'))}
    finally{setBusy(false)}
  }
  const status=home.data
  return <SettingsSection title={msg('Desk home')} variant="plain" description={msg('Keep shared settings and connections together, with each Desk and its saved history in its own folder.')}>
    <CardField label={msg('Desk home')} action={<div className={styles.actions}>
      {status?.path && <Button disabled={status.restartRequired} onClick={() => void openFolder('home')}><IconFolder/>{msg('Open folder')}</Button>}
      <Button ref={opener} disabled={!status?.canMove || status.restartRequired || working || unsaved || chatBlocked} onClick={() => {setDestination('');setPreview(null);setError('');setUncertain(false);setOpen(true)}}>{msg('Move…')}</Button>
    </div>}><span className={styles.path}>{status ? status.path || msg('Files use separate locations') : (home.isPending ? <LoadingState inline>{msg('Loading…')}</LoadingState> : msg('Unavailable'))}</span></CardField>
    <CardField label={msg('Current Desk')} action={<Button disabled={!status?.current || status.restartRequired} onClick={() => void openFolder('current')}><IconFolder/>{msg('Open folder')}</Button>}>
      <span>{status?.current?.name ?? (home.isPending ? <LoadingState inline>{msg('Loading…')}</LoadingState> : msg('Unavailable'))}</span><span className={styles.location}>{status?.current?.folder}</span>
    </CardField>
    {(unsaved || chatBlocked) && <SettingsFeedback>{msg('Save or discard changes before moving Desk home.')}</SettingsFeedback>}
    {home.isError && <SettingsFeedback tone="error" actions={<Button onClick={() => void home.refetch()}>{msg('Reload locations')}</Button>}>{msg('Desk locations could not be read.')}</SettingsFeedback>}
    {error && !open && <SettingsFeedback tone="error">{systemMessage(error)}</SettingsFeedback>}
    <Dialog open={open} onOpenChange={value => {if(!value)void close()}} openerRef={opener} title={preview?msg('Review Desk home move'):msg('Move Desk home')} size={preview?'wide':'default'} footer={<DialogActions>
      <Button disabled={busy} onClick={() => void close()}>{msg('Cancel')}</Button>
      {preview && <Button disabled={working} onClick={() => setPreview(null)}>{msg('Back')}</Button>}
      <Button variant="primary" disabled={working || uncertain || !destination || chatBlocked || Boolean(preview?.blockers.length)} onClick={() => void submit()}>{busy?msg('Working…'):preview?msg('Move Desk home'):msg('Review move')}</Button>
    </DialogActions>}>
      <FieldGroup>
        {preview ? <>
          <CardField label={msg('New Desk home')}><span className={styles.path}>{preview.destination}</span></CardField>
          <div className={styles.moves}>{preview.moves.map(move => <div className={styles.move} key={move.from}><span>{systemMessage(move.label)}</span><span className={styles.path}>{move.from}</span><span className={styles.path}>→ {move.to}</span></div>)}</div>
          {preview.blockers.map(blocker => <SettingsFeedback key={blocker} tone="warning">{systemMessage(blocker)}</SettingsFeedback>)}
        </> : <div className={styles.destination}>
          <div className={styles.destinationHeading}><span>{msg('New Desk home')}</span><Button variant="quiet" disabled={working || uncertain || !status || destination===status.defaultPath} onClick={() => {setDestination(status!.defaultPath);setError('')}}>{msg('Use default location')}</Button></div>
          <div className={styles.destinationChoice}><span className={styles.path} role="status">{destination || msg('No folder selected')}</span><Button ref={chooser} disabled={working || uncertain} onClick={() => void choose()}><IconFolder/>{picking?msg('Choosing…'):msg('Choose folder…')}</Button></div>
          <p className={styles.caption}>{msg('Choose a containing folder. Desk adds a jpack-desk folder and shows the exact destination before moving.')}</p>
          {picking && <SettingsFeedback>{msg('Choose a folder in the file explorer window, or cancel to return here.')}</SettingsFeedback>}
        </div>}
        <p className={styles.explanation}>{msg('Move shared settings, connections and all Desks with their saved history. The destination must be on the same filesystem.')}</p>
        <p className={styles.explanation}>{msg('Finish active work and save changes in other windows first. Desk closes to move the files; start it again afterward.')}</p>
        {error && <SettingsFeedback tone="error" actions={uncertain?<Button disabled={busy} onClick={() => void checkStatus()}>{msg('Check move status')}</Button>:undefined}>{systemMessage(error)}</SettingsFeedback>}
      </FieldGroup>
    </Dialog>
  </SettingsSection>
}
