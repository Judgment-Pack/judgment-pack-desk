import {privateQuery} from '../data/privateQuery'
import { useDirtyGuard } from '../shell/useDirtyGuard'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Menu as DropdownMenu } from '../ui/Menu'
import { useRef, useState, type RefObject } from 'react'
import { Link } from 'react-router-dom'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useChats } from '../chat/ChatProvider'
import { deskFetch } from '../files/client'
import { useFileListing } from '../files/queries'
import { msg, systemMessage, useLocale } from '../i18n'
import { sourceMessage } from '../i18n/source'
import { IconFolder, IconPlus } from '../shell/icons'
import { useConfirmDeskExit, useConfirmDiscard, useDeskTitle, useDeskChangeState } from '../shell/UnsavedChanges'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { Tooltip } from '../ui/Tooltip'
import { DeskIdentity } from './DeskIdentity'
import { activeDeskId, openDesk } from './scope'

type Desk = { id: string; name: string; folder: string; managed: boolean; mark?: string | null }
type Directory = { current: Desk; desks: Desk[]; location: string; startup?: string }
/** A creation's answer: the desk, and what it says where the desk was made with less than a new desk has. */
type Created = Desk & { notice?: string }

/**
 * The paragraphs a creation's notice is made of, as the chassis writes them
 * (`chooseDeskGates`, `unsignedByRuntime` and `unsignedByCustody` in
 * `internal/desk/desks.go`), so that `systemMessage` shows each in the owner's
 * language. A reason Desk's custody gives stays as the chassis wrote it.
 */
export const CREATION_NOTICES = [
  sourceMessage('The runtime this Desk runs (jpack {{version}}) reads configuration versions {{versions}}, not 5. This desk was created at configVersion 4, without requireComparableFacts, so a fact of a type no comparison can match is not refused. A runtime of 0.25.0 or later creates desks with it.'),
  sourceMessage('This desk is not signed: a desk names its signing key at configVersion 6, and the runtime this Desk runs (jpack {{version}}) does not read it. A runtime of {{floor}} or later creates desks signed.'),
  sourceMessage('This desk is not signed, because Desk could not keep a signing key for it: {{reason}}. It was created at configVersion 5, which names no signing key.')
] as const
export async function desksAPI<T>(name?: string): Promise<T> {
  const response = await deskFetch('/api/desks', name === undefined ? {} : {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name})})
  if (!response.ok) throw Error(msg('Desks could not be loaded or saved. Please try again.'))
  const value: unknown = await response.json()
  const object = (item: unknown): item is Record<string, unknown> => !!item && typeof item === 'object' && !Array.isArray(item)
  const record = (item: unknown): boolean => object(item) && typeof item.id === 'string' && (item.id === '' || /^[a-f0-9]{32}$/.test(item.id)) && typeof item.name === 'string' && !!item.name.trim() && typeof item.folder === 'string' && typeof item.managed === 'boolean' && (item.mark === undefined || item.mark === null || typeof item.mark === 'string')
  if (name === undefined ? !object(value) || !record(value.current) || !Array.isArray(value.desks) || !value.desks.every(record) || typeof value.location !== 'string' : !record(value) || !object(value) || value.notice !== undefined && typeof value.notice !== 'string') throw Error(msg('Desks could not be loaded or saved. Please try again.'))
  return value as T
}
export function DeskSwitcher() {
  useLocale()
  const listing = useFileListing(), {config} = useEffectiveConfig()
  const directory = useQuery({...privateQuery,queryKey:['desks'],queryFn:()=>desksAPI<Directory>()})
  const fallback = listing.data?.root.split(/[/\\]/).filter(Boolean).at(-1) ?? msg('Desk')
  const label = directory.data?.current.name ?? fallback
  useDeskTitle(label)
  const changes = useDeskChangeState()
  const dirty = changes.dirty
  const opener = useRef<HTMLButtonElement>(null)
  const management = useDeskManagement(opener)
  return <>
    <DeskIdentity name={label} mark={directory.data ? directory.data.current.mark : config.organization.mark} value={activeDeskId}
      desks={directory.data?.desks ?? []} startup={directory.data?.startup} onSelect={id=>void management.switchTo(id)}
      dirty={dirty} dirtyLabels={changes.labels} opener={opener} home={mark=><Tooltip content={msg('Desk home')} side="bottom"><Link className="desk-home" to="/" aria-label={msg('Desk home')}>{mark}</Link></Tooltip>}>
      {management.items}
    </DeskIdentity>
    {management.dialogs}
  </>
}

/** Dialogs stay mounted outside the desk menu portal. */
function useDeskManagement(opener: RefObject<HTMLButtonElement | null>) {
  const directory = useQuery({...privateQuery,queryKey:['desks'],queryFn:()=>desksAPI<Directory>()})
  const client = useQueryClient(), {store, dirty:chatDirty} = useChats()
  const leave = useConfirmDeskExit(), discard = useConfirmDiscard()
  const [creating,setCreating] = useState(false), [name,setName] = useState(''), [busy,setBusy] = useState(false), [error,setError] = useState('')
  const [created,setCreated] = useState<Created>()
  const clearCreateGuard = useDirtyGuard(creating && !!name.trim(), msg('Discard this desk name?'), {name:name.trim(), busy:creating&&busy})
  async function switchTo(id: string) {
    if (id === activeDeskId) return
    setError('')
    try {
      if (store?.running) throw Error(msg('Wait for the assistant to finish before switching desks.'))
      if (store && chatDirty && !(await store.flush())) throw Error(msg('Save the conversation before switching desks.'))
      // Let successful autosaves unregister their dirty state before asking.
      await new Promise<void>(resolve => requestAnimationFrame(() => resolve()))
      if (await leave()) openDesk(id)
    } catch (e) {setError(e instanceof Error ? e.message : msg('The desk could not be opened.'))}
  }
  async function create() {
    setBusy(true); setError('')
    try {
      const desk = await desksAPI<Created>(name.trim())
      await client.invalidateQueries({...privateQuery,queryKey:['desks']})
      clearCreateGuard(); setCreating(false); setName('')
      // A desk made with less than a new desk has, unsigned among them, says
      // so before it opens.
      if (desk.notice) setCreated(desk)
      else await switchTo(desk.id)
    } catch (e) {setError(e instanceof Error ? e.message : msg('The desk could not be created.'))}
    finally {setBusy(false)}
  }
  async function close() {
    if (busy) return
    if (name.trim() && !(await discard(msg('Discard this desk name?'),{name:name.trim()}))) return
    setCreating(false);setName('');setError('')
  }
  const items = <>
        {!directory.data&&activeDeskId&&<DropdownMenu.Item onSelect={()=>void switchTo('')}>{msg('Open startup desk')}</DropdownMenu.Item>}
        <DropdownMenu.Separator/>
        <DropdownMenu.Item asChild icon={<IconFolder/>}><Link to="/author">{msg('Project files')}</Link></DropdownMenu.Item>
        <DropdownMenu.Item icon={<IconPlus/>} disabled={!directory.data} onSelect={()=>{setError('');setCreating(true)}}>{msg('Create desk…')}</DropdownMenu.Item>
        {directory.error&&<DropdownMenu.Label>{msg('Desk management is unavailable. Restart Desk after updating.')}</DropdownMenu.Label>}
    </>
  const dialogs = <>
    <Dialog open={creating} onOpenChange={open=>{if(!open)void close()}} openerRef={opener} title={msg('Create desk')} description={msg('Keep packs, sources, conversations and jobs together in one desk folder.')}
      footer={<DialogActions><Button disabled={busy} onClick={()=>void close()}>{msg('Cancel')}</Button><Button variant="primary" disabled={busy||!name.trim()} onClick={()=>void create()}>{busy?msg('Creating…'):msg('Create desk')}</Button></DialogActions>}>
      <Field label={msg('Desk name')}>{wiring=><Input {...wiring} value={name} maxLength={80} autoComplete="off" onChange={event=>setName(event.target.value)}/>}</Field>
      <p className="quiet">{msg('A new folder will be created in:')}<br/><code>{directory.data?.location}</code></p>
      {error&&<p role="alert">{error}</p>}
    </Dialog>
    <Dialog open={!!created} onOpenChange={open=>{if(!open)setCreated(undefined)}} openerRef={opener} title={msg('Desk created')}
      footer={<DialogActions><Button onClick={()=>setCreated(undefined)}>{msg('Close')}</Button><Button variant="primary" onClick={()=>{const id=created?.id;setCreated(undefined);if(id)void switchTo(id)}}>{msg('Open desk')}</Button></DialogActions>}>
      {created?.notice?.split('\n\n').map((paragraph,index)=><p key={index}>{systemMessage(paragraph)}</p>)}
    </Dialog>
    <Dialog open={!creating&&!!error} onOpenChange={open=>{if(!open)setError('')}} openerRef={opener} title={msg('Could not switch desk')} description={error} footer={<DialogActions><Button onClick={()=>setError('')}>{msg('Close')}</Button></DialogActions>}><span/></Dialog>
  </>
  return { items, dialogs, switchTo }
}
