import { useId, useLayoutEffect, useRef, useState } from 'react'
import { VisuallyHidden } from 'radix-ui'
import { msg, useLocale, formatNumber } from '../i18n'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import { SegmentedControl } from '../ui/SegmentedControl'
import { Alert } from '../ui/Alert'
import { ProviderIcon } from './ProviderIcon'
import { providerDescription, providerName } from './registry'
import type { ConnectionDescriptor } from './catalog'
import type { ConnectionStatus } from './client'
import styles from './ConnectionDirectory.module.css'

export interface DirectoryEntry { descriptor: ConnectionDescriptor; status: { data?: Pick<ConnectionStatus, 'state'>; isError?: boolean } }
type View = 'all' | 'connected'
type Filter = 'all' | 'setup-required' | 'not-connected' | 'unavailable'
interface DirectoryView { view: View; filter: Filter; query: string; scroll: number; selected?: string }
const initial: DirectoryView = { view: 'all', filter: 'all', query: '', scroll: 0 }
function readView(key: string): DirectoryView {
 try {
  const raw = sessionStorage.getItem(key)
  if (!raw || raw.length > 2048) return initial
  const value = JSON.parse(raw)
  if (!value || !['all', 'connected'].includes(value.view) || !['all','setup-required','not-connected','unavailable'].includes(value.filter) || typeof value.query !== 'string') return initial
  return { view: value.view, filter: value.filter, query: value.query.slice(0,256), scroll: Number.isFinite(value.scroll) ? Math.max(0,Math.min(value.scroll,1000000)) : 0, selected: typeof value.selected === 'string' ? value.selected.slice(0,48) : undefined }
 } catch { return initial }
}
function normalized(value: string) { return value.normalize('NFD').replace(/\p{M}/gu, '').toLocaleLowerCase() }
export function connectionState(entry: DirectoryEntry) { return entry.status.isError ? 'unavailable' : entry.status.data?.state }
function stateLabel(state: ConnectionStatus['state'] | undefined) {
 switch (state) {
 case 'connected': return msg('Connected')
 case 'setup-required': return msg('Setup required')
 case 'not-connected': return msg('Not connected')
 case 'blocked': return msg('Managed by your organization')
 case 'unavailable': return msg('Unavailable')
 default: return msg('Loading…')
 }
}

/** One searchable directory for settings and the contextual connection pane. */
export function ConnectionDirectory({ entries, unsupported = [], available, loading, isError, retry, mode, onSelect }: {
 entries: DirectoryEntry[]; unsupported?: ConnectionDescriptor[]; available: boolean; loading: boolean; isError: boolean; retry: () => void
 mode: 'admin' | 'picker'; onSelect: (entry: DirectoryEntry, opener: HTMLButtonElement) => void
}) {
 const locale = useLocale(), id = useId(), root = useRef<HTMLDivElement>(null), search = useRef<HTMLInputElement>(null)
 const key = `jpack-desk.connection-directory.v1:${mode}`
 const [view, setView] = useState(() => readView(key)), current = useRef(view)
 const scrollElement = useRef<HTMLElement | null>(null), restored = useRef(false)
 function save(next: DirectoryView) { current.current = next; try { sessionStorage.setItem(key, JSON.stringify(next)) } catch { /* Optional view memory. */ } }
 function interacted() {
  if (restored.current) return
  restored.current = true
  save({ ...current.current, selected: undefined, scroll: scrollElement.current?.scrollTop ?? 0 })
 }
 function change(patch: Partial<DirectoryView>) {
  restored.current = true
  const next = { ...current.current, ...patch, scroll: 0, selected: undefined }
  save(next); setView(next)
  if (scrollElement.current) scrollElement.current.scrollTop = 0
 }
 useLayoutEffect(() => {
  // Admin scrolls its route; the picker scrolls its existing pane body.
  let scroller: HTMLElement | null = root.current?.parentElement ?? null
  while (scroller && !/(auto|scroll)/.test(getComputedStyle(scroller).overflowY)) scroller = scroller.parentElement
  if (!scroller) return
  scrollElement.current = scroller
  const remember = () => { if (restored.current) save({ ...current.current, scroll: scroller!.scrollTop }) }
  scroller.addEventListener('scroll', remember, { passive: true })
  const gestures = ['wheel', 'touchstart', 'pointerdown', 'keydown'] as const
  gestures.forEach(event => scroller!.addEventListener(event, interacted, { passive: true }))
  return () => {
   scroller?.removeEventListener('scroll', remember)
   gestures.forEach(event => scroller?.removeEventListener(event, interacted))
   scrollElement.current = null
  }
 }, [])
 useLayoutEffect(() => { if (mode === 'picker' && !current.current.selected) search.current?.focus({ preventScroll: true }) }, [mode])
 const ready = !loading && (!available || isError || entries.every(entry => entry.status.data || entry.status.isError))
 useLayoutEffect(() => {
  // Restore only after asynchronous status filters have produced their rows.
  if (!ready || restored.current) return
  restored.current = true
  if (scrollElement.current) scrollElement.current.scrollTop = current.current.scroll
  const selected = [...(root.current?.querySelectorAll<HTMLButtonElement>('[data-provider]') ?? [])].find(button => button.dataset.provider === current.current.selected)
  if (mode === 'picker') (selected ?? search.current)?.focus({ preventScroll: true })
 }, [ready, mode])
 const usableEntries = available && !isError ? entries : [], others = available && !isError ? unsupported : []
 const connected = usableEntries.filter(entry => connectionState(entry) === 'connected').length
 const terms = normalized(view.query).split(/\s+/).filter(Boolean)
 const matches = (descriptor: ConnectionDescriptor) => {
  const content = normalized([providerName(descriptor.id, descriptor), providerDescription(descriptor.id, descriptor), descriptor.id, descriptor.id.replaceAll('-', ' ')].join(' '))
  return terms.every(term => content.includes(term))
 }
 const rows = [
  ...usableEntries.map(entry => ({ ...entry, unsupported: false })),
  ...others.map(descriptor => ({ descriptor, status: { data: { state: 'unavailable' as const } }, unsupported: true }))
 ].filter(entry => matches(entry.descriptor) && (view.view !== 'connected' || connectionState(entry) === 'connected') && (view.view === 'connected' || view.filter === 'all' || (view.filter === 'unavailable' ? ['unavailable','blocked'].includes(connectionState(entry) ?? '') : connectionState(entry) === view.filter)))
 .sort((a,b) => providerName(a.descriptor.id,a.descriptor).localeCompare(providerName(b.descriptor.id,b.descriptor),locale))
 return <div ref={root} className={styles.directory} onPointerDownCapture={interacted} onKeyDownCapture={interacted} onFocusCapture={event => { if (event.target !== search.current) interacted() }}>
  <div className={styles.controls}>
   <SegmentedControl label={msg('Connections')} value={view.view} onValueChange={value => change({ view: value as View })} segments={[
    { value: 'all', label: `${msg('Browse all')} · ${formatNumber(usableEntries.length + others.length)}` },
    { value: 'connected', label: `${msg('Connected')} · ${formatNumber(connected)}` }
   ]} />
   <div className={styles.search}>
    <Input ref={search} value={view.query} maxLength={256} onChange={event => change({ query: event.target.value })} placeholder={msg('Search connections…')} aria-label={msg('Search connections…')} />
    {view.view === 'all' && <div className={styles.filter}><VisuallyHidden.Root asChild><label htmlFor={`${id}-status`}>{msg('Connection status')}</label></VisuallyHidden.Root><Select id={`${id}-status`} value={view.filter} onValueChange={value => change({ filter: value as Filter })} options={[
     {value:'all',label:msg('All statuses')}, {value:'setup-required',label:msg('Setup required')}, {value:'not-connected',label:msg('Not connected')}, {value:'unavailable',label:msg('Unavailable')}
    ]} /></div>}
   </div>
  </div>
  <ul className={styles.list} aria-label={msg('Connections')}>
   {rows.map(entry => {
    const { descriptor } = entry, title = providerName(descriptor.id, descriptor), state = connectionState(entry)
    const registration = mode === 'admin' && descriptor.registration === 'google-desktop'
    const disabled = state === undefined || registration && !['setup-required','not-connected','connected'].includes(state)
    const action = state === 'setup-required' ? msg('Set up') : state === 'not-connected' && !registration ? msg('Connect') : state === 'connected' && mode === 'picker' ? msg('Open') : msg('Manage')
    return <li key={descriptor.id} className={styles.row}>
     <ProviderIcon provider={descriptor.id} descriptor={descriptor} />
     <div className={styles.copy}><h3>{title}</h3><p>{providerDescription(descriptor.id, descriptor)}</p></div>
     <div className={styles.end}><span className={styles.status}>{entry.unsupported ? msg('Update Desk to use this connection.') : stateLabel(state)}</span>
      {!entry.unsupported && <Button data-provider={descriptor.id} className={styles.action} variant="quiet" disabled={disabled}
       aria-label={msg('{{action}}: {{provider}}', { action, provider: registration ? msg('{{provider}} registration', { provider: title }) : title })}
       onClick={event => { save({ ...current.current, selected: descriptor.id }); onSelect(entry, event.currentTarget) }}>{action}</Button>}
     </div>
    </li>
   })}
  </ul>
  {!ready ? <p role="status">{msg('Loading…')}</p> : !available ? <p>{msg('Local processing is unavailable. Check the details in Admin → Storage & data.')}</p> : isError ? <><Alert>{msg('Connections could not be loaded.')}</Alert><Button onClick={retry}>{msg('Retry')}</Button></> : !rows.length && <div role="status"><p>{msg('No connections found.')}</p>{(view.query || view.filter !== 'all' || view.view !== 'all') && <Button variant="quiet" onClick={() => { change(initial); search.current?.focus() }}>{msg('Clear filters')}</Button>}</div>}
 </div>
}
