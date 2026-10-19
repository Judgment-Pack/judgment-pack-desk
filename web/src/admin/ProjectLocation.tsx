import {LoadingState} from '../ui/LoadingState'
import { useEffect, useRef, useState } from 'react'
import { useChats } from '../chat/ChatProvider'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { answer, deskFetch, FileRequestError, StaleWrite } from '../files/client'
import { msg, systemMessage, useLocale } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { Button, ButtonLink } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { FieldGroup } from '../ui/Field'
import { IconFolder } from '../shell/icons'
import { SettingsSection } from '../ui/SettingsSection'
import { SettingsFeedback } from '../ui/SettingsFeedback'
import { CardField } from './SourceCard'
import { DeskStatusNotice } from '../shell/DeskNotices'
import { PopoverClose } from '../ui/Popover'
import { useHasPendingChanges, useConfirmDiscard } from '../shell/UnsavedChanges'
import { useDirtyGuard } from '../shell/useDirtyGuard'
import styles from './ProjectLocation.module.css'

export interface ProjectLocationStatus { path: string; defaultDestination?: string; moving?: boolean; restartRequired: boolean; movedTo?: string; problem?: string }
interface Preview { project: string; destination: string; token: string; createParent?: boolean }
export const PROJECT_LOCATION_KEY = ['project-location'] as const
async function readLocation(): Promise<ProjectLocationStatus> {
  const data = await answer<ProjectLocationStatus>(await deskFetch('/api/project-location'))
  if (typeof data.path !== 'string' || typeof data.restartRequired !== 'boolean') throw new Error('Project location could not be read.')
  return data
}
export function useProjectLocation() {
  return useQuery({ queryKey: PROJECT_LOCATION_KEY, queryFn: readLocation, retry: false, staleTime: 5_000, refetchInterval: 10_000 })
}

/** Installation scope: keep visible even while a named desk is selected. */
export function ProjectLocationNotice() {
  useLocale()
  const { data } = useProjectLocation()
  if (data?.restartRequired) return <DeskStatusNotice id="project-location" priority="restart" tone="warning" label={msg('Restart required · Project moved')} title={msg('Project moved')}>
    <p>{msg('Stop Desk, then start it with the new project folder. Refreshing this page does not restart Desk.')}</p>
    <p className={styles.path}>{data.movedTo}</p>
    {data.problem && <p>{systemMessage(data.problem)}</p>}
  </DeskStatusNotice>
  if (data?.problem) return <DeskStatusNotice id="project-history-move" tone="warning" label={msg('Project history needs attention')}
    actions={<PopoverClose><ButtonLink to="/admin#general">{msg('Review project location')}</ButtonLink></PopoverClose>}>
    {systemMessage(data.problem)}
  </DeskStatusNotice>
  return null
}

export function ProjectLocationSettings() {
  useLocale()
  const client = useQueryClient(), location = useProjectLocation(), opener = useRef<HTMLButtonElement>(null)
  const unsaved = useHasPendingChanges(), confirmDiscard = useConfirmDiscard()
  const [open, setOpen] = useState(false), [destination, setDestination] = useState('')
  const [preview, setPreview] = useState<Preview | null>(null), [busy, setBusy] = useState(false)
  const [picking, setPicking] = useState(false)
  const picker = useRef<AbortController | null>(null), chooser = useRef<HTMLButtonElement>(null)
  useEffect(() => () => picker.current?.abort(), [])
  const working = busy || picking
  const [error, setError] = useState(''), [uncertain, setUncertain] = useState(false)
  const clearGuard = useDirtyGuard(open && (!!destination || picking), msg('Discard this move setup?'), { busy: working })
  const status = location.data
  const { desk } = useEffectiveConfig()
  const currentPath = desk?.chassis?.projectDir
  const chats = useChats()
  const chatBlocked = Boolean(chats.store?.running || chats.saving || chats.dirty || chats.error)
  async function close() {
    if (!busy && (!destination || await confirmDiscard(msg('Discard this move setup?')))) { picker.current?.abort(); picker.current = null; setPicking(false); clearGuard(); setOpen(false) }
  }
  async function chooseFolder() {
    if (working || uncertain) return
    const controller = new AbortController()
    picker.current = controller
    setPicking(true); setError('')
    try {
      const result = await answer<{ destination?: string; canceled?: boolean }>(await deskFetch('/api/project-location/choose-folder', {
        method: 'POST', signal: controller.signal
      }))
      if (picker.current !== controller || result.canceled) return
      if (typeof result.destination !== 'string' || !result.destination) throw new Error(msg('The selected folder could not be read. Choose a folder again.'))
      setDestination(result.destination); setPreview(null)
    } catch (cause) {
      if (!controller.signal.aborted) setError((cause as Error).message)
    } finally {
      if (picker.current === controller) {
        picker.current = null; setPicking(false)
        requestAnimationFrame(() => chooser.current?.focus())
      }
    }
  }
  async function reload() {
    setBusy(true)
    try {
      const latest = await readLocation()
      client.setQueryData(PROJECT_LOCATION_KEY, latest)
      if (latest.restartRequired) { clearGuard(); setOpen(false) }
      else if (!latest.moving) { setUncertain(false); setPreview(null); setError('') }
    } catch (cause) { setError((cause as Error).message) }
    finally { setBusy(false) }
  }
  async function submit() {
    if (working || uncertain || chatBlocked || !destination) return
    setBusy(true); setError('')
    const confirming = preview !== null
    let submitted = false
    try {
      if (confirming && chats.store && !await chats.store.flush()) throw new Error(msg('Save or recover unsaved chat changes before moving the project.'))
      submitted = true
      const response = await deskFetch(`/api/project-location/${confirming ? 'move' : 'preview'}`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ destination, token: preview?.token ?? '' })
      })
      if (confirming) {
        const result = await answer<ProjectLocationStatus>(response)
        if (!result.restartRequired || !result.movedTo) throw new Error('The move result could not be confirmed.')
        client.setQueryData<ProjectLocationStatus>(PROJECT_LOCATION_KEY, result)
        clearGuard(); setOpen(false)
      } else setPreview(await answer<Preview>(response))
    } catch (cause) {
      setPreview(null)
      if (confirming && submitted && !(cause instanceof StaleWrite) && !(cause instanceof FileRequestError && cause.source === 'chassis' && cause.status >= 400 && cause.status < 500)) {
        setUncertain(true); setError(msg('Desk could not confirm whether the move finished. Check the project location before trying again.'))
      } else setError((cause as Error).message)
    } finally { setBusy(false) }
  }
  return <SettingsSection title={msg('Project location')} variant="plain" description={msg('The current Desk and the startup project can use different folders.')}>
    <CardField label={msg('Current Desk folder')} rule={msg('The project folder for the Desk you are viewing.')}><span className={styles.path}>{currentPath === status?.path ? status?.movedTo ?? currentPath ?? (location.isPending ? <LoadingState inline>{msg('Loading…')}</LoadingState> : msg('Unavailable')) : currentPath ?? (location.isPending ? <LoadingState inline>{msg('Loading…')}</LoadingState> : msg('Unavailable'))}</span></CardField>
    <CardField label={msg('Startup project folder')} rule={msg('Move to… moves only the startup project. Managed Desks and their data stay in their current locations.')} action={<Button ref={opener} disabled={!status || busy || unsaved || chatBlocked || status.restartRequired || status.moving || !!status.problem}
      onClick={() => { setDestination(''); setPreview(null); setError(''); setUncertain(false); setOpen(true) }}>{msg('Move to…')}</Button>}>
      <span className={styles.path}>{status?.movedTo ?? status?.path ?? (location.isPending ? <LoadingState inline>{msg('Loading…')}</LoadingState> : msg('Unavailable'))}</span>
    </CardField>
    {(unsaved || chatBlocked) && !open && <SettingsFeedback>{msg('Save or discard changes before moving the project.')}</SettingsFeedback>}
    {location.isError && <SettingsFeedback tone="error" actions={<Button onClick={() => void location.refetch()}>{msg('Reload location')}</Button>}>{msg('Project location could not be read.')}</SettingsFeedback>}
    {status?.problem && <SettingsFeedback tone="warning">{systemMessage(status.problem)}</SettingsFeedback>}
    <Dialog open={open} onOpenChange={value => { if (!value) void close() }} openerRef={opener} title={preview ? msg('Review project move') : msg('Move startup project')}
      footer={<DialogActions><Button disabled={busy} onClick={() => void close()}>{msg('Cancel')}</Button>
        {preview && <Button disabled={busy} onClick={() => setPreview(null)}>{msg('Back')}</Button>}
        <Button variant="primary" disabled={working || uncertain || chatBlocked || !destination} onClick={() => void submit()}>{busy ? msg('Working…') : preview ? msg('Move project') : msg('Review move')}</Button>
      </DialogActions>}>
      <FieldGroup>
        <CardField label={msg('Current folder')}><span className={styles.path}>{preview?.project ?? status?.path}</span></CardField>
        {preview ? <CardField label={msg('New folder')}><span className={styles.path}>{preview.destination}</span></CardField>
          : <div className={styles.destination}>
            <div className={styles.destinationHeading}>
              <span className={styles.destinationLabel}>{msg('New folder')}</span>
              <Button variant="quiet" disabled={working || uncertain || !status?.defaultDestination || destination === status.defaultDestination} onClick={() => { setDestination(status!.defaultDestination!); setError('') }}>{msg('Use default location')}</Button>
            </div>
            <div className={styles.destinationChoice}>
              <span className={styles.path} role="status">{destination || msg('No folder selected')}</span>
              <Button ref={chooser} disabled={working || uncertain} onClick={() => void chooseFolder()}><IconFolder/>{picking ? msg('Choosing…') : msg('Choose folder…')}</Button>
            </div>
            <p className={styles.caption}>{destination && destination === status?.defaultDestination ? msg('Desk creates any missing default folders only after you confirm the move.') : msg('Choose a containing folder. Desk keeps the project folder name and shows its full destination here.')}</p>
            {picking && <SettingsFeedback>{msg('Choose a folder in the file explorer window, or cancel to return here.')}</SettingsFeedback>}
          </div>}
        <p className={styles.explanation}>{msg('Move the project and keep its saved history linked. The destination must be unused and on the same filesystem. Desk must restart after the move.')}</p>
        <p className={styles.explanation}>{msg('Managed Desks and their data remain in their current locations.')}</p>
        {preview?.createParent && <p className={styles.explanation}>{msg('Desk will create the missing default folders when you confirm.')}</p>}
        {preview && <p className={styles.explanation}>{msg('Save any changes in other browser tabs before moving. Stop Desk afterward, then start it with the new folder. The saved startup project will also use this location.')}</p>}
        {error && <SettingsFeedback tone="error" actions={uncertain ? <Button disabled={busy} onClick={() => void reload()}>{msg('Check project location')}</Button> : undefined}>{systemMessage(error)}</SettingsFeedback>}
      </FieldGroup>
    </Dialog>
  </SettingsSection>
}
