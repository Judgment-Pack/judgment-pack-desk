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
import { msg, systemMessage, useLocale } from '../i18n'
import { sourceMessage } from '../i18n/source'
import { IconCheck, IconFolder, IconPlus } from '../shell/icons'
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
  const record = (item: unknown): boolean => object(item) && typeof item.id === 'string' && (item.id === '' || /^[a-f0-9]{32}$/.test(item.id)) && typeof item.name === 'string' && !!item.name.trim() && typeof item.folder === 'string' && typeof item.managed === 'boolean'
  if (name === undefined ? !object(value) || !record(value.current) || !Array.isArray(value.desks) || !value.desks.every(record) || typeof value.location !== 'string' : !record(value) || !object(value) || value.notice !== undefined && typeof value.notice !== 'string') throw Error(msg('Desks could not be loaded or saved. Please try again.'))
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
  const [menuOpen, setMenuOpen] = useState(false)
  return <>
    <div className="desk-identity">
      <Tooltip content={msg('Desk home')} side="bottom" disabled={menuOpen}>
        <Link className="desk-home" to="/" aria-label={msg('Desk home')}>
          <BrandMark mark={config.organization.mark} className="desk-orgmark" />
        </Link>
      </Tooltip>
      <DropdownMenu.Root open={menuOpen} onOpenChange={setMenuOpen}>
        <Tooltip content={`${label} · ${msg('Switch desk')}`} side="bottom" disabled={menuOpen}>
          <DropdownMenu.Trigger ref={opener} className="desk-chip" aria-label={`${label} · ${msg('Switch desk')}`}>
            <span className="desk-chip-name">{label}</span>{dirty&&<span className="desk-dirty" aria-label={msg('unsaved changes')} role="img"/>}
          </DropdownMenu.Trigger>
        </Tooltip>
        <DropdownMenu.Portal><DropdownMenu.Content className="desk-menu desk-switcher-menu" align="start" alignOffset={-32} sideOffset={6} collisionPadding={12}>
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
      await client.invalidateQueries({queryKey:['desks']})
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
  const desks = [...(directory.data?.desks ?? [])].sort((a, b) =>
    Number(b.id === activeDeskId) - Number(a.id === activeDeskId) || a.name.localeCompare(b.name))
  const items = <>
        <DropdownMenu.Label className="desk-menu-note">{msg('Switch desk')}</DropdownMenu.Label>
        <DropdownMenu.RadioGroup value={activeDeskId} onValueChange={id=>void switchTo(id)} className="desk-switcher-list">
          {desks.map(desk => <DropdownMenu.RadioItem className="desk-menu-item desk-switcher-choice" key={desk.id} value={desk.id}>
            <span className="desk-switcher-name">{desk.name}</span>
            <span className="desk-switcher-check"><DropdownMenu.ItemIndicator><IconCheck /></DropdownMenu.ItemIndicator></span>
          </DropdownMenu.RadioItem>)}
        </DropdownMenu.RadioGroup>
        {!directory.data&&activeDeskId&&<DropdownMenu.Item className="desk-menu-item" onSelect={()=>void switchTo('')}>{msg('Open startup desk')}</DropdownMenu.Item>}
        <DropdownMenu.Separator className="desk-switcher-separator" />
        <DropdownMenu.Item asChild className="desk-menu-item"><Link to="/author"><IconFolder />{msg('Project files')}</Link></DropdownMenu.Item>
        <DropdownMenu.Item className="desk-menu-item" disabled={!directory.data} onSelect={()=>{setError('');setCreating(true)}}><IconPlus />{msg('Create desk…')}</DropdownMenu.Item>
        {directory.error&&<DropdownMenu.Label className="desk-menu-note">{msg('Desk management is unavailable. Restart Desk after updating.')}</DropdownMenu.Label>}
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
  return { items, dialogs }
}
