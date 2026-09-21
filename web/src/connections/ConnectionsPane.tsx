import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useQueryClient } from '@tanstack/react-query'
import { msg, useLocale } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useChats } from '../chat/ChatProvider'
import { useChatAttachments } from '../chat/useChatAttachments'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { Alert } from '../ui/Alert'
import { Disclosure } from '../ui/Disclosure'
import { GoogleRegistrationSetup } from './GoogleRegistrationSetup'
import { ProviderIcon } from './ProviderIcon'
import { CONNECTION_PROVIDERS, providerName, providerDescription } from './registry'
import { authorizeDrive, connectionCall, CONNECTIONS_KEY, useDriveStatus, type ConnectionProvider, type ConnectionStatus, type MailSearch, type MailSelection, type SourceSearch, type SourceSelection } from './client'
import type { ConnectionPaneRequest } from './ConnectionPaneContext'
import styles from './ConnectionsPane.module.css'

type ResultRow = { id: string; title: string; metadata?: string; context: string }

/** Controller stays outside the portal: a dock/drawer swap must not cancel
 * sign-in, lose source selections, or reset a typed query. */
export function ConnectionsPane({ request, target, onProvider, onClose, onBusy, onAttached = onClose }: {
 request: ConnectionPaneRequest; target: HTMLElement | null
 onAttached?: () => void; onProvider: (provider: ConnectionProvider) => void; onClose: () => void; onBusy: (chatId?: string) => void
}) {
 useLocale()
 const effective = useEffectiveConfig(), client = useQueryClient()
 const available = effective.desk?.localGateway?.status === 'ready' && !effective.desk?.decoded?.values?.research?.gateway
 const drive = useDriveStatus(available), gmail = useDriveStatus(available, 'gmail')
 const notion = useDriveStatus(available, 'notion'), obsidian = useDriveStatus(available, 'obsidian')
 const statuses = { 'google-drive': drive, gmail, notion, obsidian }
 const provider = request.provider, status = provider ? statuses[provider] : undefined
 const state = !available || status?.isError ? 'unavailable' : status?.data?.state
 const { store, chats, drafts, bindings } = useChats()
 const chat = [...chats, ...drafts].find(item => item.id === request.chatId)
 const running = Boolean(request.chatId && bindings.get(request.chatId)?.run?.running)
 const upload = useChatAttachments(store, request.chatId ?? '', running, effective.config.research)
 const capacity = Math.max(0, Math.min(4, status?.data?.maxFiles ?? 4) - (chat?.attachments?.length ?? 0))
 const [query, setQuery] = useState(''), [vault, setVault] = useState('')
 const [rows, setRows] = useState<ResultRow[] | null>(null)
 const [selected, setSelected] = useState<ResultRow[]>([])
 const [nextPage, setNextPage] = useState<string>(), [submitted, setSubmitted] = useState('')
 const [more, setMore] = useState(false), [busy, setBusy] = useState(false)
 const [error, setError] = useState(''), [notice, setNotice] = useState('')
 const active = useRef<AbortController | null>(null)
 const panel = useRef<HTMLElement>(null)
 const context = JSON.stringify([available, effective.config.research.gateway, effective.config.research.documents])
 const cancel = () => { active.current?.abort(); active.current = null; setBusy(false) }
 useEffect(() => {
  setRows(null); setSelected([]); setNextPage(undefined); setError(''); setBusy(false)
  return () => { active.current?.abort(); active.current = null; if (upload.isReading()) upload.cancel() }
 }, [provider, status?.data?.account?.id, context])
 useEffect(() => { if (request.chatId && (!chat || running)) onClose() }, [request.chatId, Boolean(chat), running, onClose])
 useEffect(() => { onBusy(upload.reading ? request.chatId : undefined); return () => onBusy(undefined) }, [upload.reading, request.chatId, onBusy])
 useEffect(() => { (provider ? panel.current : panel.current?.querySelector('input'))?.focus({ preventScroll: true }) }, [target, provider])
 function begin() {
  if (active.current || upload.isReading()) return
  const task = new AbortController(); active.current = task; setBusy(true); setError(''); setNotice(''); return task
 }
 const current = (task: AbortController) => active.current === task && !task.signal.aborted
 const finish = (task: AbortController) => { if (active.current === task) { active.current = null; setBusy(false) } }
 async function connect() {
  if (!provider) return
  const task = begin(); if (!task) return
  try {
   if (provider === 'obsidian') await connectionCall('configure', { path: vault.trim() }, task.signal, provider)
   else await authorizeDrive('connect', task.signal, provider)
   if (current(task)) await client.invalidateQueries({ queryKey: CONNECTIONS_KEY })
  } catch (cause) { if (current(task)) setError((cause as Error).message) }
  finally { finish(task) }
 }
 async function disconnect() {
  if (!provider) return
  const task = begin(); if (!task) return
  try {
   const result = await connectionCall<{ revoked: boolean }>('disconnect', {}, task.signal, provider)
   if (current(task)) {
    setRows(null); setSelected([])
    if (!result.revoked && provider !== 'obsidian') setNotice(provider === 'notion' ? msg('Disconnected here. Remove the connection in Notion settings to revoke access there.') : msg('Disconnected here. Remove access in your Google account to finish revoking access.'))
    await client.invalidateQueries({ queryKey: CONNECTIONS_KEY })
   }
  } catch (cause) { if (current(task)) setError((cause as Error).message) }
  finally { finish(task) }
 }
 async function search(pageToken?: string) {
  if (!provider || provider === 'google-drive') return
  const task = begin(); if (!task) return
  setRows(null); setNextPage(undefined)
  if (!pageToken) { setSelected([]); setSubmitted(query) }
  try {
   const answer = await connectionCall<MailSearch | SourceSearch>('search', { query: pageToken ? submitted : query, ...(pageToken ? { pageToken } : {}) }, task.signal, provider)
   if (!current(task)) return
   if ('messages' in answer) {
    setRows(answer.messages.map(row => ({ id: row.id, title: row.subject || msg('No subject'), metadata: [row.from, row.date].filter(Boolean).join(' · '), context: answer.selectionContext })))
    setNextPage(answer.nextPageToken); setMore(false)
   } else {
    setRows(answer.items.map(row => ({ id: row.id, title: row.title, metadata: row.description, context: answer.selectionContext })))
    setMore(answer.more)
   }
  } catch (cause) { if (current(task)) setError((cause as Error).message) }
  finally { finish(task) }
 }
 async function attach() {
  if (!provider || !request.chatId || !selected.length || selected.length > capacity) return
  const task = begin(); if (!task) return
  try {
   // Each page's grant binds exactly that page's results. Keep selections
   // across Gmail pages without applying the newest page's context to old IDs.
   const contexts = [...new Set(selected.map(row => row.context))]
   const mail: MailSelection[] = [], sources: SourceSelection[] = []
   for (const selectionContext of contexts) {
    if (!current(task)) return
    const ids = selected.filter(row => row.context === selectionContext).map(row => row.id)
    if (provider === 'gmail') mail.push(...await connectionCall<MailSelection[]>('select', { messageIds: ids, selectionContext }, task.signal, provider))
    else sources.push(...await connectionCall<SourceSelection[]>('select', { resourceIds: ids, selectionContext }, task.signal, provider))
   }
   if (!current(task)) return
   const attached = provider === 'gmail' ? await upload.attachGmail(mail) : provider === 'google-drive' ? false : await upload.attachSource(provider, sources)
   if (current(task) && attached) onAttached()
  } catch (cause) { if (current(task)) setError((cause as Error).message) }
  finally { finish(task) }
 }
 const unavailable = state === 'unavailable' || state === 'blocked'
 const working = busy || upload.reading
 const canAttach = Boolean(chat && effective.config.research.documents?.enabled)
 // Opening a source picker is already a browse gesture. Notion requires a
 // query; Gmail and a local vault can show their first bounded page directly.
 useEffect(() => {
  if (canAttach && state === 'connected' && (provider === 'gmail' || provider === 'obsidian')) void search()
 }, [canAttach, state, provider, status?.data?.account?.id, context])
 const content = !provider ? <section ref={panel} tabIndex={-1} className={styles.pane} aria-label={msg('Connections')}>
  <div className={styles.body}>
   <Input autoFocus value={query} onChange={event => setQuery(event.target.value)} placeholder={msg('Search connections…')} aria-label={msg('Search connections…')} />
   {(['connected', 'available'] as const).map(group => {
    const providers = CONNECTION_PROVIDERS.filter(item => (statuses[item].data?.state === 'connected' && available) === (group === 'connected') && providerName(item).toLocaleLowerCase().includes(query.toLocaleLowerCase()))
    return providers.length ? <section key={group} aria-label={group === 'connected' ? msg('Connected') : msg('Available')}>
     <h2 className={styles.group}>{group === 'connected' ? msg('Connected') : msg('Available')}</h2>
     {providers.map(item => <button key={item} type="button" className={styles.provider} onClick={() => onProvider(item)}>
      <ProviderIcon provider={item} /><span className={styles.copy}><span>{providerName(item)}</span><span className={styles.description}>{providerDescription(item)}</span></span>
      <span className={styles.hint}>{statusLabel(available && !statuses[item].isError ? statuses[item].data?.state : 'unavailable')}</span>
     </button>)}
    </section> : null
   })}
   {!CONNECTION_PROVIDERS.some(item => providerName(item).toLocaleLowerCase().includes(query.toLocaleLowerCase())) && <p>{msg('No connections found.')}</p>}
  </div>
 </section> : state === 'setup-required' && (provider === 'google-drive' || provider === 'gmail') ?
  <GoogleRegistrationSetup provider={provider} available={available} instructionsInitiallyOpen onClose={onClose} contextual /> :
 <section ref={panel} tabIndex={-1} className={styles.pane} aria-label={providerName(provider)}>
  <div className={styles.body}>
   <p className={styles.scope}>{status?.data?.account?.email || status?.data?.account?.name || msg('Personal · This computer')}</p>
   <p>{provider === 'obsidian' ? msg('Search and attach notes from a local vault. Your notes stay unchanged.') : provider === 'notion' ? msg('Search and attach Notion pages. Desk cannot change your workspace.') : provider === 'gmail' ? msg('Choose up to four emails. Message text is attached; mail attachments are excluded.') : msg('Choose the files you want to attach to this chat.')}</p>
   {unavailable ? <p>{state === 'blocked' ? msg('Managed by your organization') : msg('Local processing is unavailable. Check the details in Admin → Storage & data.')}</p> : state === undefined ? <p role="status">{msg('Loading…')}</p> : state !== 'connected' ? <>
    {provider === 'obsidian' && <label>{msg('Vault folder')}<Input value={vault} onChange={event => setVault(event.target.value)} autoComplete="off" spellCheck={false} disabled={working} placeholder={msg('Absolute path to your Obsidian vault')} /></label>}
    <Disclosure title={msg('How it works')}>
     <p>{provider === 'obsidian' ? msg('In Obsidian, open Manage vaults and copy the folder path shown below your vault name. Paste that full path here. No plugin is needed.') : msg('Connect your account, then choose the sources to attach. Connecting does not add anything to your chat.')}</p>
    </Disclosure>
   </> : request.chatId ? <>
    {!canAttach ? <p>{msg('Enable document processing in Admin → Storage & data before attaching sources.')}</p> : provider !== 'google-drive' && <>
     <form className={styles.search} onSubmit={event => { event.preventDefault(); void search() }}>
      <Input value={query} onChange={event => setQuery(event.target.value)} disabled={working} maxLength={1024} aria-label={msg('Search {{provider}}', { provider: providerName(provider) })} placeholder={msg('Search {{provider}}', { provider: providerName(provider) })} />
      <Button type="submit" disabled={working || provider === 'notion' && !query.trim()}>{msg('Search')}</Button>
     </form>
     {rows?.map(row => <label key={row.id} className={styles.row}>
      <input type="checkbox" checked={selected.some(item => item.id === row.id)} disabled={working || !selected.some(item => item.id === row.id) && selected.length >= capacity} onChange={event => setSelected(items => event.target.checked ? [...items, row] : items.filter(item => item.id !== row.id))} />
      <span className={styles.copy}><span>{row.title}</span>{row.metadata && <span className={styles.metadata}>{row.metadata}</span>}</span>
     </label>)}
     {rows?.length === 0 && <p>{msg('No results found.')}</p>}
     {more && <p className={styles.description}>{msg('More results are available. Refine your search.')}</p>}
     {nextPage && <Button variant="quiet" disabled={working} onClick={() => void search(nextPage)}>{msg('Next page')}</Button>}
    </>}
   </> : <p>{msg('Connected. Choose sources from the chat attachment menu.')}</p>}
   {provider === 'notion' && <p className={styles.description}>{msg('Notion may include connected workspace sources in search. Only Notion pages you select are attached to chat.')}</p>}
   {state === 'connected' && <Disclosure title={msg('Connection settings')}><p>{msg('Disconnecting does not delete documents already attached to chats.')}</p><Button variant="quiet" disabled={working} onClick={() => void disconnect()}>{msg('Disconnect')}</Button></Disclosure>}
  </div>
  <footer className={styles.footer}>
   {selected.length > 0 && <p className={styles.description}>{selected.map(row => row.title).join(' · ')}</p>}
   {(error || upload.error) && <Alert>{error || upload.error}</Alert>}
   {notice && <p role="status">{notice}</p>}
   {working && <p role="status">{upload.reading ? upload.progress : state === 'connected' || provider === 'obsidian' ? msg('Loading…') : provider === 'notion' ? msg('Continue in the Notion sign-in window.') : msg('Continue in the Google sign-in window.')}</p>}
   <div className={styles.actions}>
    {working ? <Button variant="quiet" onClick={() => { cancel(); if (upload.reading) upload.cancel() }}>{msg('Cancel')}</Button> : unavailable ? <Button onClick={() => void status?.refetch()} disabled={!available}>{msg('Retry')}</Button> : state === 'not-connected' ?
     <Button variant="primary" disabled={provider === 'obsidian' && !vault.trim()} onClick={() => void connect()}>{provider === 'obsidian' ? msg('Connect vault') : provider === 'notion' ? msg('Continue with Notion') : msg('Continue with Google')}</Button> : state === 'connected' && request.chatId &&
     <Button variant="primary" disabled={!canAttach || capacity === 0 || provider !== 'google-drive' && (selected.length === 0 || selected.length > capacity)} onClick={() => { if (provider === 'google-drive') void upload.attachDrive().then(done => { if (done) onAttached() }); else void attach() }}>{provider === 'google-drive' ? msg('Choose files') : msg('Attach {{count}} items', { count: selected.length })}</Button>}
   </div>
  </footer>
 </section>
 return target ? createPortal(content, target) : null
}

function statusLabel(state: ConnectionStatus['state'] | undefined) {
 switch (state) {
 case 'connected': return msg('Connected')
 case 'setup-required': return msg('Set up')
 case 'not-connected': return msg('Connect')
 case 'blocked': return msg('Managed by your organization')
 case 'unavailable': return msg('Unavailable')
 default: return msg('Loading…')
 }
}
