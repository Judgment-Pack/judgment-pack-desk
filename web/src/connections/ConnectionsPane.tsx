import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useQueryClient } from '@tanstack/react-query'
import { msg, systemMessage, useLocale } from '../i18n'
import { formatStorageBytes } from '../admin/chatStorage'
import { readResourcePage, RESOURCE_MAX_BYTES } from './resourceProtocol'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useChats } from '../chat/ChatProvider'
import { useChatAttachments } from '../chat/useChatAttachments'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { Alert } from '../ui/Alert'
import { Disclosure } from '../ui/Disclosure'
import { GoogleRegistrationSetup } from './GoogleRegistrationSetup'
import { ConnectionDirectory } from './ConnectionDirectory'
import { pinConnection, rememberConnection, SHORTCUT_LIMIT, useConnectionPreferences } from './preferences'
import { providerName } from './registry'
import { ConnectionRequestError, connectionError, authorizeDrive, connectionCall, CONNECTIONS_KEY, type ConnectionProvider, type MailSearch, type MailSelection, type SourceSearch, type SourceSelection } from './client'
import { useConnections, localized, type ConnectionDescriptor } from './catalog'
import type { ConnectionPaneRequest } from './ConnectionPaneContext'
import styles from './ConnectionsPane.module.css'

type ResultRow = { id: string; title: string; metadata?: string; context: string; unavailable?: string }

/** Controller stays outside the portal: a dock/drawer swap must not cancel
 * sign-in, lose source selections, or reset a typed query. */
export function ConnectionsPane({ request, target, onProvider, onClose, onBusy, onAttached = onClose }: {
 request: ConnectionPaneRequest; target: HTMLElement | null
 onAttached?: () => void; onProvider: (provider: ConnectionProvider, descriptor?: ConnectionDescriptor) => void; onClose: () => void; onBusy: (chatId?: string) => void
}) {
 useLocale()
 const effective = useEffectiveConfig(), client = useQueryClient()
 const available = effective.desk?.localGateway?.status === 'ready' && !effective.desk?.decoded?.values?.research?.gateway
 const catalog = useConnections(available)
 const preferences = useConnectionPreferences()
 const pinned = Boolean(request.provider && preferences.pinned.includes(request.provider))
 const pinLimit = preferences.pinned.filter(id => catalog.entries.some(entry => entry.descriptor.id === id)).length >= SHORTCUT_LIMIT
 const provider = request.provider, entry = catalog.entries.find(item => item.descriptor.id === provider)
 const status = entry?.status, descriptor = entry?.descriptor
 const generic = descriptor?.source?.record === 'resource-v1', prefix = descriptor?.queryMode === 'prefix'
 const fileLimit = Math.min(status?.data?.maxFileBytes ?? RESOURCE_MAX_BYTES, effective.config.research.documents?.maxFileBytes ?? RESOURCE_MAX_BYTES)
 const state = !available || catalog.isError || (!catalog.loading && !entry && Boolean(provider)) || status?.isError ? 'unavailable' : status?.data?.state
 // Also promote a service first connected from this pane, after setup succeeds.
 useEffect(() => { if (provider && hasDestination && state === 'connected') rememberConnection(provider) }, [provider, request.chatId, state])
 const { store, chats, drafts, bindings } = useChats()
 const chat = [...chats, ...drafts].find(item => item.id === request.chatId)
 const hasDestination = Boolean(request.chatId || request.destination)
 const running = Boolean(request.chatId && bindings.get(request.chatId)?.run?.running)
 const upload = useChatAttachments(store, request.chatId ?? '', running, effective.config.research, request.destination)
 const capacity = Math.max(0, Math.min(4, status?.data?.maxFiles ?? 4) - (request.destination?.current()?.length ?? chat?.attachments?.length ?? 0))
 const [query, setQuery] = useState(''), [vault, setVault] = useState('')
 const [setup, setSetup] = useState<Record<string,string>>({})
 const needsSetup = descriptor?.registration === 'form' && (state === 'setup-required' || descriptor.auth !== 'oauth')
 const missingSetup = needsSetup && (descriptor?.setup?.some(field => field.required && !setup[field.key]?.trim()) ?? false)
 const unsupported = (catalog.unsupported ?? []).find(item => item.id === provider)
 const [rows, setRows] = useState<ResultRow[] | null>(null)
 const [selected, setSelected] = useState<ResultRow[]>([])
 const [nextPage, setNextPage] = useState<string>(), [submitted, setSubmitted] = useState('')
 const [more, setMore] = useState(false), [busy, setBusy] = useState(false)
 const [error, setError] = useState(''), [notice, setNotice] = useState('')
 const [reconnect, setReconnect] = useState(false)
 const active = useRef<AbortController | null>(null)
 const autoBrowse = useRef<string | undefined>(undefined)
 const visitedPages = useRef(new Set<string>())
 const panel = useRef<HTMLElement>(null)
 const context = JSON.stringify([available, descriptor, effective.config.research.gateway, effective.config.research.documents, status?.data?.resource?.id])
 const cancel = () => { active.current?.abort(); active.current = null; setBusy(false) }
 useEffect(() => {
  setRows(null); setSelected([]); setNextPage(undefined); setError(''); setBusy(false)
  return () => { active.current?.abort(); active.current = null; if (upload.isReading()) upload.cancel() }
 }, [provider, status?.data?.account?.id, context])
 useEffect(() => { if (request.chatId && (!chat || running)) onClose() }, [request.chatId, Boolean(chat), running, onClose])
 useEffect(() => { onBusy(upload.reading ? request.chatId : undefined); return () => onBusy(undefined) }, [upload.reading, request.chatId, onBusy])
 useEffect(() => { if (provider) panel.current?.focus({ preventScroll: true }) }, [target, provider])
 function begin() {
  if (active.current || upload.isReading()) return
  const task = new AbortController(); active.current = task; setBusy(true); setError(''); setNotice(''); return task
 }
 const reportFailure = (cause: unknown) => {
  setError((cause as Error).message)
  if (cause instanceof ConnectionRequestError && cause.reconnectRequired) {
   setReconnect(true); setRows(null); setSelected([]); setNextPage(undefined)
   void client.invalidateQueries({ queryKey: CONNECTIONS_KEY })
  }
 }
 useEffect(() => { setReconnect(false); upload.clearError(); setSetup({}); setVault('') }, [provider, context])
 useEffect(() => { if (upload.connectionFailure && upload.connectionFailure.provider === provider) reportFailure(upload.connectionFailure) }, [upload.connectionFailure, provider])
 const current = (task: AbortController) => active.current === task && !task.signal.aborted
 const finish = (task: AbortController) => { if (active.current === task) { active.current = null; setBusy(false) } }
 async function connect() {
  if (!provider) return
  const task = begin(); if (!task) return
  try {
   if (needsSetup) {
    await connectionCall('configure', setup, task.signal, provider)
    if (current(task)) setSetup({})
   } else if (provider === 'obsidian') await connectionCall('configure', { path: vault.trim() }, task.signal, provider)
   else if (descriptor?.authorizationEndpoints) await authorizeDrive('connect', task.signal, provider, descriptor.authorizationEndpoints)
   else await authorizeDrive('connect', task.signal, provider)
   if (current(task)) { setReconnect(false); upload.clearError(); await client.invalidateQueries({ queryKey: CONNECTIONS_KEY }) }
  } catch (cause) { if (current(task)) reportFailure(cause) }
  finally { finish(task) }
 }
 async function disconnect() {
  if (!provider) return
  const task = begin(); if (!task) return
  try {
   const result = await connectionCall<{ disconnected?: boolean; revoked?: boolean }>('disconnect', {}, task.signal, provider)
   if (current(task)) {
    if (descriptor?.protocol && result.disconnected !== true) throw new Error(msg('The connection did not confirm disconnection. Try again.'))
    setRows(null); setSelected([])
    if (result.revoked !== true && descriptor?.auth !== 'local-folder' && provider !== 'obsidian') setNotice(descriptor?.auth === 'credentials' ? msg('Disconnected here. Provider access may still be active.') : !['google-drive','gmail','notion'].includes(provider) ? msg('Disconnected here. Remove access in the provider settings to finish revoking access.') : provider === 'notion' ? msg('Disconnected here. Remove the connection in Notion settings to revoke access there.') : msg('Disconnected here. Remove access in your Google account to finish revoking access.'))
    await client.invalidateQueries({ queryKey: CONNECTIONS_KEY })
   }
  } catch (cause) { if (current(task)) reportFailure(cause) }
  finally { finish(task) }
 }
 async function search(pageToken?: string) {
  if (!provider || !descriptor || descriptor.selection === 'browser-picker' || descriptor.queryRequired && !(pageToken ? submitted : query).trim()) return
  const task = begin(); if (!task) return
  setRows(null); setNextPage(undefined)
  if (!pageToken) { setSelected([]); setSubmitted(query); visitedPages.current.clear() }
  try {
   const response = await connectionCall<MailSearch | SourceSearch>('search', { query: pageToken ? submitted : query, ...(pageToken ? { pageToken } : {}) }, task.signal, provider)
   if (!current(task)) return
   const answer = generic ? readResourcePage(response) : response
   if (generic && answer.nextPageToken && (answer.nextPageToken === pageToken || visitedPages.current.has(answer.nextPageToken))) throw new Error(msg('The connection returned an invalid response. Try again.'))
   if (pageToken) visitedPages.current.add(pageToken)
   setSelected(items => items.filter(item => item.context === answer.selectionContext))
   if ('messages' in answer) {
    setRows(answer.messages.map(row => ({ id: row.id, title: row.subject || msg('No subject'), metadata: [row.from, row.date].filter(Boolean).join(' · '), context: answer.selectionContext })))
    setNextPage(answer.nextPageToken); setMore(false)
   } else {
    const nextRows = answer.items.map(row => ({ id: row.id, title: row.title, metadata: [row.description, row.sizeBytes === undefined ? undefined : formatStorageBytes(row.sizeBytes)].filter(Boolean).join(' · '), context: answer.selectionContext, unavailable: row.unavailableReason || (row.sizeBytes !== undefined && row.sizeBytes > fileLimit ? 'file-too-large' : undefined) }))
    setRows(nextRows)
    setSelected(items => items.filter(item => !nextRows.some(row => row.id === item.id && row.unavailable)))
    setMore(answer.more && !answer.nextPageToken); setNextPage(answer.nextPageToken)
   }
  } catch (cause) { if (current(task)) reportFailure(cause) }
  finally { finish(task) }
 }
 async function attach() {
  if (!provider || !hasDestination || !selected.length || selected.length > capacity) return
  const task = begin(); if (!task) return
  try {
   // The context is the connection epoch, shared across Gmail pages.
   // An epoch change clears earlier selections before they can be attached.
   const contexts = [...new Set(selected.map(row => row.context))]
   const mail: MailSelection[] = [], sources: SourceSelection[] = []
   for (const selectionContext of contexts) {
    if (!current(task)) return
    const ids = selected.filter(row => row.context === selectionContext).map(row => row.id)
    if (provider === 'gmail') mail.push(...await connectionCall<MailSelection[]>('select', { messageIds: ids, selectionContext }, task.signal, provider))
    else sources.push(...await connectionCall<SourceSelection[]>('select', { resourceIds: ids, selectionContext }, task.signal, provider))
   }
   if (!current(task)) return
   const attached = provider === 'gmail' ? await upload.attachGmail(mail) : provider === 'google-drive' ? false : await (descriptor?.source?.record === 'resource-v1' ? upload.attachSource(provider, sources, descriptor) : upload.attachSource(provider, sources))
   if (current(task) && attached) onAttached()
  } catch (cause) { if (current(task)) reportFailure(cause) }
  finally { finish(task) }
 }
 const unavailable = state === 'unavailable' || state === 'blocked'
 const working = busy || upload.reading
 const canAttach = Boolean((chat || request.destination?.current()) && effective.config.research.documents?.enabled)
 // Opening a source picker is already a browse gesture. Notion requires a
 // query; Gmail and a local vault can show their first bounded page directly.
 useEffect(() => {
  if (state !== 'connected' || reconnect) { autoBrowse.current = undefined; return }
  const browseContext = JSON.stringify([provider, request.chatId, request.destination?.id, status?.data?.account?.id, context])
  // Configuration can refresh status before its request finishes. Browse once
  // it finishes, without restarting a canceled or failed search automatically.
  if (!working && canAttach && descriptor && descriptor.selection !== 'browser-picker' && !descriptor.queryRequired && autoBrowse.current !== browseContext) {
   autoBrowse.current = browseContext
   void search()
  }
 }, [working, canAttach, reconnect, state, provider, request.chatId, status?.data?.account?.id, context])
 const content = !provider ? <section ref={panel} tabIndex={-1} className={styles.pane} aria-label={msg('Connections')}>
  <div className={styles.body}>
   <ConnectionDirectory {...catalog} available={available} mode="picker" retry={() => void catalog.refetch()} onSelect={item => onProvider(item.descriptor.id, item.descriptor)} />
  </div>
 </section> : state === 'setup-required' && descriptor?.registration === 'google-desktop' && (provider === 'google-drive' || provider === 'gmail') ?
  <GoogleRegistrationSetup provider={provider} available={available} instructionsInitiallyOpen onClose={onClose} contextual /> :
 <section ref={panel} tabIndex={-1} className={styles.pane} aria-label={providerName(provider, descriptor ?? unsupported)}>
  <div className={styles.body}>
   <p className={styles.scope}>{[status?.data?.resource?.name, status?.data?.account?.email || status?.data?.account?.name].filter(Boolean).join(' · ') || (generic ? providerName(provider, descriptor) : msg('Personal · This computer'))}</p>
   <p>{descriptor?.presentation ? localized(descriptor.presentation.description) : provider === 'obsidian' ? msg('Search and attach notes from a local vault. Your notes stay unchanged.') : provider === 'notion' ? msg('Search and attach Notion pages. Desk cannot change your workspace.') : provider === 'gmail' ? msg('Choose up to four emails. Message text is attached; mail attachments are excluded.') : msg('Choose the files you want to attach to this chat.')}</p>
   {unavailable ? <p>{unsupported ? msg('Update Desk to use this connection.') : state === 'blocked' ? msg('Managed by your organization') : !available ? msg('Local processing is unavailable. Check the details in Admin → Storage & data.') : msg('This connection is unavailable in the current gateway.')}</p> : state === undefined ? <p role="status">{msg('Loading…')}</p> : state !== 'connected' || reconnect ? <>
    {needsSetup ? descriptor?.setup?.map(field => <label key={field.key} className={styles.setupField}>{localized(field.label)}<Input type={field.type} value={setup[field.key] ?? ''} onChange={event => setSetup(values => ({...values, [field.key]:event.target.value}))} autoComplete="off" spellCheck={false} disabled={working} maxLength={4096} required={field.required} /></label>) : provider === 'obsidian' && <label>{msg('Vault folder')}<Input value={vault} onChange={event => setVault(event.target.value)} autoComplete="off" spellCheck={false} disabled={working} placeholder={msg('Absolute path to your Obsidian vault')} /></label>}
    <Disclosure title={msg('How it works')}>
     <p>{descriptor?.presentation ? localized(descriptor.presentation.instructions) : provider === 'obsidian' ? msg('In Obsidian, open Manage vaults and copy the folder path shown below your vault name. Paste that full path here. No plugin is needed.') : msg('Connect your account, then choose the sources to attach. Connecting does not add anything to your chat.')}</p>
    </Disclosure>
   </> : hasDestination ? <>
    <p className={styles.description}>{msg('Up to {{count}} files · {{size}} per file', {count: Math.min(4, status?.data?.maxFiles ?? 4), size: formatStorageBytes(fileLimit)})}</p>
    {!canAttach ? <p>{msg('Enable document processing in Admin → Storage & data before attaching sources.')}</p> : provider !== 'google-drive' && <>
     <form className={styles.search} onSubmit={event => { event.preventDefault(); void search() }}>
      <Input value={query} onChange={event => setQuery(event.target.value)} disabled={working} maxLength={1024} aria-label={prefix ? msg('Path starts with…') : msg('Search {{provider}}', { provider: providerName(provider, descriptor ?? unsupported) })} placeholder={prefix ? msg('Path starts with…') : msg('Search {{provider}}', { provider: providerName(provider, descriptor ?? unsupported) })} />
      <Button type="submit" disabled={working || descriptor?.queryRequired && !query.trim()}>{prefix ? msg('Browse') : msg('Search')}</Button>
     </form>
     {rows?.map(row => <label key={row.id} className={styles.row}>
      <input type="checkbox" checked={selected.some(item => item.id === row.id)} disabled={working || Boolean(row.unavailable) || !selected.some(item => item.id === row.id) && selected.length >= capacity} onChange={event => { if (row.unavailable) return; setSelected(items => event.target.checked ? [...items, row] : items.filter(item => item.id !== row.id)) }} />
      <span className={styles.copy}><span>{row.title}</span>{row.metadata && <span className={styles.metadata}>{row.metadata}</span>}{row.unavailable && <span className={styles.metadata}>{connectionError(['archived','permission-required','not-downloadable','file-too-large','unsupported-file'].includes(row.unavailable) ? row.unavailable : 'unsupported-file', provider)}</span>}</span>
     </label>)}
     {rows?.length === 0 && <p>{msg('No results found.')}</p>}
     {more && <p className={styles.description}>{msg('More results are available. Refine your search.')}</p>}
     {nextPage && <Button variant="quiet" disabled={working} onClick={() => void search(nextPage)}>{msg('Next page')}</Button>}
    </>}
   </> : <p>{msg('Connected. Choose sources from the chat attachment menu.')}</p>}
   {!descriptor?.presentation && provider === 'notion' && <p className={styles.description}>{msg('Notion may include connected workspace sources in search. Only Notion pages you select are attached to chat.')}</p>}
   {(state === 'connected' || pinned) && <div className={styles.pin}>
    <Button variant="quiet" aria-pressed={pinned} disabled={!pinned && pinLimit} onClick={() => pinConnection(provider, !pinned, catalog.entries.map(item => item.descriptor.id))}>{pinned ? msg('Unpin from composer') : msg('Pin to composer')}</Button>
    {!pinned && pinLimit && <p className={styles.description}>{msg('You can pin up to five connections.')}</p>}
   </div>}
   {state === 'connected' && <Disclosure title={msg('Connection settings')}><p>{msg('Disconnecting does not delete documents already attached to chats.')}</p><Button variant="quiet" disabled={working} onClick={() => void disconnect()}>{msg('Disconnect')}</Button></Disclosure>}
  </div>
  <footer className={styles.footer}>
   {selected.length > 0 && <p className={styles.description}>{selected.map(row => row.title).join(' · ')}</p>}
   {(error || upload.error) && <Alert>{systemMessage(error || upload.error)}</Alert>}
   {notice && <p role="status">{notice}</p>}
   {working && <p role="status">{upload.reading ? systemMessage(upload.progress) : state === 'connected' || descriptor?.auth !== 'oauth' ? msg('Loading…') : descriptor?.protocol ? msg('Continue in the sign-in window.') : provider === 'notion' ? msg('Continue in the Notion sign-in window.') : msg('Continue in the Google sign-in window.')}</p>}
   <div className={styles.actions}>
    {working ? <Button variant="quiet" onClick={() => { cancel(); if (upload.reading) upload.cancel() }}>{msg('Cancel')}</Button> : unavailable ? <Button onClick={() => { void catalog.refetch(); void status?.refetch() }} disabled={!available}>{msg('Retry')}</Button> : reconnect ? <Button variant="primary" disabled={descriptor?.registration === 'form' ? missingSetup : provider === 'obsidian' && !vault.trim()} onClick={() => void connect()}>{descriptor?.auth === 'credentials' ? msg('Update credentials') : msg('Reconnect')}</Button> : state === 'not-connected' || state === 'setup-required' && descriptor?.registration === 'form' ?
     <Button variant="primary" disabled={descriptor?.registration === 'form' ? missingSetup : provider === 'obsidian' && !vault.trim()} onClick={() => void connect()}>{descriptor?.protocol ? state === 'setup-required' ? msg('Save') : descriptor.auth === 'oauth' ? msg('Continue with {{provider}}', {provider:providerName(provider,descriptor)}) : msg('Connect') : provider === 'obsidian' ? msg('Connect vault') : provider === 'notion' ? msg('Continue with Notion') : msg('Continue with Google')}</Button> : state === 'connected' && hasDestination &&
     <Button variant="primary" disabled={!canAttach || capacity === 0 || provider !== 'google-drive' && (selected.length === 0 || selected.length > capacity)} onClick={() => { if (provider === 'google-drive') void upload.attachDrive().then(done => { if (done) onAttached() }); else void attach() }}>{provider === 'google-drive' ? msg('Choose files') : msg('Attach {{count}} items', { count: selected.length })}</Button>}
   </div>
  </footer>
 </section>
 return target ? createPortal(content, target) : null
}
