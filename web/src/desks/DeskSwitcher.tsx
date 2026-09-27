import { useAuthorDirty } from '../shell/authorBridge'
import { useDirtyGuard } from '../shell/useDirtyGuard'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { DropdownMenu } from 'radix-ui'
import { useRef, useState, type RefObject } from 'react'
import { Link } from 'react-router-dom'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useChats } from '../chat/ChatProvider'
import { deskFetch } from '../files/client'
import { useFileListing } from '../files/queries'
import { msg, useLocale } from '../i18n'
import { IconChevronDown, IconFolder } from '../shell/icons'
import { useConfirmDeskExit, useConfirmDiscard, useDeskTitle, useHasUnsavedChanges } from '../shell/UnsavedChanges'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { Tooltip } from '../ui/Tooltip'
import { BrandMark } from '../ui/BrandMark'
import { activeDeskId, openDesk } from './scope'

type Desk = { id: string; name: string; folder: string; managed: boolean }
type Directory = { current: Desk; desks: Desk[]; location: string }
export async function desksAPI<T>(name?: string): Promise<T> {
  const response = await deskFetch('/api/desks', name === undefined ? {} : {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name})})
  if (!response.ok) throw Error(msg('Desks could not be loaded or saved. Please try again.'))
  const value: unknown = await response.json()
  const object = (item: unknown): item is Record<string, unknown> => !!item && typeof item === 'object' && !Array.isArray(item)
  const record = (item: unknown): boolean => object(item) && typeof item.id === 'string' && (item.id === '' || /^[a-f0-9]{32}$/.test(item.id)) && typeof item.name === 'string' && !!item.name.trim() && typeof item.folder === 'string' && typeof item.managed === 'boolean'
  if (name === undefined ? !object(value) || !record(value.current) || !Array.isArray(value.desks) || !value.desks.every(record) || typeof value.location !== 'string' : !record(value)) throw Error(msg('Desks could not be loaded or saved. Please try again.'))
  return value as T
}
export function DeskSwitcher() {
  useLocale()
  const listing = useFileListing(), {config} = useEffectiveConfig()
  const directory = useQuery({queryKey:['desks'],queryFn:()=>desksAPI<Directory>()})
  const fallback = listing.data?.root.split(/[/\\]/).filter(Boolean).at(-1) ?? msg('Desk')
  const label = directory.data?.current.name ?? fallback
  useDeskTitle(`${label} · ${config.organization.name ?? 'Unveil'}`)
  const registeredDirty = useHasUnsavedChanges(), authorDirty = useAuthorDirty()
  const dirty = registeredDirty || authorDirty
  const opener = useRef<HTMLButtonElement>(null)
  const management = useDeskManagement(opener)
  return <>
    <div className="desk-identity">
      <BrandMark mark={config.organization.mark} className="desk-orgmark" />
      <Link className="desk-chip" to="/">
        <span className="desk-chip-name">{label}</span>{dirty&&<span className="desk-dirty" aria-label={msg('unsaved changes')} role="img"/>}
      </Link>
      <DropdownMenu.Root>
        <Tooltip content={msg('Switch desk')}>
          <DropdownMenu.Trigger ref={opener} className="desk-icon-button desk-menu-trigger" aria-label={msg('Switch desk')}><IconChevronDown /></DropdownMenu.Trigger>
        </Tooltip>
        <DropdownMenu.Portal><DropdownMenu.Content className="desk-menu desk-header-menu" align="start" sideOffset={6} collisionPadding={16}>
          <DropdownMenu.Item asChild className="desk-menu-item"><Link to="/author"><IconFolder />{msg('Project files')}</Link></DropdownMenu.Item>
          <DropdownMenu.Separator className="desk-menu-separator" />
          {management.items}
        </DropdownMenu.Content></DropdownMenu.Portal>
      </DropdownMenu.Root>
    </div>
    {management.dialogs}
  </>
}

/** Dialogs stay mounted outside the desk menu portal. */
function useDeskManagement(opener: RefObject<HTMLButtonElement | null>) {
  const directory = useQuery({queryKey:['desks'],queryFn:()=>desksAPI<Directory>()})
  const client = useQueryClient(), {store, dirty:chatDirty} = useChats()
  const leave = useConfirmDeskExit(), discard = useConfirmDiscard()
  const [creating,setCreating] = useState(false), [name,setName] = useState(''), [busy,setBusy] = useState(false), [error,setError] = useState('')
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
      const desk = await desksAPI<Desk>(name.trim())
      await client.invalidateQueries({queryKey:['desks']})
      clearCreateGuard(); setCreating(false); setName('')
      await switchTo(desk.id)
    } catch (e) {setError(e instanceof Error ? e.message : msg('The desk could not be created.'))}
    finally {setBusy(false)}
  }
  async function close() {
    if (busy) return
    if (name.trim() && !(await discard(msg('Discard this desk name?'),{name:name.trim()}))) return
    setCreating(false);setName('');setError('')
  }
  const items = <>
        <DropdownMenu.Label className="desk-menu-note">{msg('Desks')}</DropdownMenu.Label>
        {directory.data?.desks.map(desk => <DropdownMenu.Item className="desk-menu-item" key={desk.id} disabled={desk.id===activeDeskId} onSelect={()=>void switchTo(desk.id)}>{desk.name}{desk.id===activeDeskId?' ✓':''}</DropdownMenu.Item>)}
        {!directory.data&&activeDeskId&&<DropdownMenu.Item className="desk-menu-item" onSelect={()=>void switchTo('')}>{msg('Open startup desk')}</DropdownMenu.Item>}
        <DropdownMenu.Item className="desk-menu-item" disabled={!directory.data} onSelect={()=>{setError('');setCreating(true)}}>{msg('Create desk…')}</DropdownMenu.Item>
        <DropdownMenu.Separator className="desk-menu-separator"/>
        {directory.data&&<DropdownMenu.Label className="desk-menu-note"><code>{directory.data.current.folder}</code></DropdownMenu.Label>}
        {directory.error&&<DropdownMenu.Label className="desk-menu-note">{msg('Desk management is unavailable. Restart Desk after updating.')}</DropdownMenu.Label>}
    </>
  const dialogs = <>
    <Dialog open={creating} onOpenChange={open=>{if(!open)void close()}} openerRef={opener} title={msg('Create desk')} description={msg('Keep packs, sources, conversations and jobs together in one desk folder.')}
      footer={<DialogActions><Button disabled={busy} onClick={()=>void close()}>{msg('Cancel')}</Button><Button variant="primary" disabled={busy||!name.trim()} onClick={()=>void create()}>{busy?msg('Creating…'):msg('Create desk')}</Button></DialogActions>}>
      <Field label={msg('Desk name')}>{wiring=><Input {...wiring} value={name} maxLength={80} autoComplete="off" onChange={event=>setName(event.target.value)}/>}</Field>
      <p className="quiet">{msg('A new folder will be created in:')}<br/><code>{directory.data?.location}</code></p>
      {error&&<p role="alert">{error}</p>}
    </Dialog>
    <Dialog open={!creating&&!!error} onOpenChange={open=>{if(!open)setError('')}} openerRef={opener} title={msg('Could not switch desk')} description={error} footer={<DialogActions><Button onClick={()=>setError('')}>{msg('Close')}</Button></DialogActions>}><span/></Dialog>
  </>
  return { items, dialogs }
}
