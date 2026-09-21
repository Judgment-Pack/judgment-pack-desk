import { useEffect, useRef, useState, type RefObject } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { msg, useLocale } from '../i18n'
import { Alert } from '../ui/Alert'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Input } from '../ui/Input'
import { authorizeDrive, connectionCall, CONNECTIONS_KEY, useDriveStatus, type SourceProvider, type SourceSearch, type SourceSelection } from './client'
import styles from './GmailPicker.module.css'

/** Shared source selection; connection credentials and vault paths never enter chat. */
export function SourceConnection({ provider, available, open, onOpenChange, openerRef, onSelect }: {
 provider: SourceProvider; available: boolean; open: boolean; onOpenChange: (open: boolean) => void
 openerRef: RefObject<HTMLElement | null>; onSelect?: (items: SourceSelection[]) => void
}) {
 useLocale()
 const queryClient = useQueryClient(), status = useDriveStatus(open && available, provider)
 const state = available && !status.isError ? status.data?.state : 'unavailable'
 const [query, setQuery] = useState(''), [vault, setVault] = useState('')
 const [result, setResult] = useState<SourceSearch | null>(null), [selected, setSelected] = useState<string[]>([])
 const [busy, setBusy] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState('')
 const active = useRef<AbortController | null>(null)
 const label = provider === 'notion' ? 'Notion' : 'Obsidian'
 useEffect(() => {
  setResult(null); setSelected([]); setError(''); setNotice(''); setBusy(false); setQuery(''); setVault('')
  return () => { active.current?.abort(); active.current = null }
 }, [open, available, provider, status.data?.account?.id])
 function begin() { if (active.current) return; const task = new AbortController(); active.current = task; setBusy(true); setError(''); setNotice(''); return task }
 const current = (task: AbortController) => active.current === task && !task.signal.aborted
 const finish = (task: AbortController) => { if (active.current === task) { active.current = null; setBusy(false) } }
 function close() { active.current?.abort(); active.current = null; setBusy(false); onOpenChange(false) }
 async function connect() {
  const task = begin(); if (!task) return
  try {
   if (provider === 'notion') await authorizeDrive('connect', task.signal, 'notion')
   else await connectionCall('configure', { path: vault.trim() }, task.signal, 'obsidian')
   if (current(task)) await queryClient.invalidateQueries({ queryKey: CONNECTIONS_KEY })
  } catch (cause) { if (current(task)) setError((cause as Error).message) }
  finally { finish(task) }
 }
 async function search() {
  const task = begin(); if (!task) return
  // Immediately invalidate previous results so failed searches cannot attach an old selection.
  setSelected([]); setResult(null)
  try { const next = await connectionCall<SourceSearch>('search', { query }, task.signal, provider); if (current(task)) setResult(next) }
  catch (cause) { if (current(task)) setError((cause as Error).message) }
  finally { finish(task) }
 }
 async function attach() {
  const task = begin(); if (!task || !result || !onSelect) return
  try {
   const items = await connectionCall<SourceSelection[]>('select', { resourceIds: selected, selectionContext: result.selectionContext }, task.signal, provider)
   if (current(task)) { onSelect(items); close() }
  } catch (cause) { if (current(task)) setError((cause as Error).message) }
  finally { finish(task) }
 }
 async function disconnect() {
  const task = begin(); if (!task) return
  try {
   const reply = await connectionCall<{ revoked: boolean }>('disconnect', {}, task.signal, provider)
   if (current(task)) { await queryClient.invalidateQueries({ queryKey: CONNECTIONS_KEY }); if (!reply.revoked) setNotice(msg('Disconnected here. Remove the connection in Notion settings to revoke access there.')) }
  } catch (cause) { if (current(task)) setError((cause as Error).message) }
  finally { finish(task) }
 }
 const connected = state === 'connected'
 return <Dialog open={open} onOpenChange={next => { if (!next) close() }} title={label} openerRef={openerRef}
  description={provider === 'notion' ? msg('Search and attach Notion pages. Desk cannot change your workspace.') : msg('Search and attach notes from a local vault. Your notes stay unchanged.')}
  footer={<DialogActions><Button variant="quiet" onClick={close}>{busy ? msg('Cancel') : msg('Close')}</Button>
   {connected ? onSelect ? <Button variant="primary" disabled={busy || !selected.length || !result} onClick={() => void attach()}>{msg('Attach selected')}</Button>
    : <Button disabled={busy} onClick={() => void disconnect()}>{msg('Disconnect')}</Button>
    : state === 'not-connected' && <Button variant="primary" disabled={busy || provider === 'obsidian' && !vault.trim()} onClick={() => void connect()}>{provider === 'notion' ? msg('Continue with Notion') : msg('Connect vault')}</Button>}
  </DialogActions>}>
  {connected && onSelect ? <>
   <form className={styles.search} onSubmit={event => { event.preventDefault(); void search() }}>
    <Input value={query} onChange={event => setQuery(event.target.value)} placeholder={msg('Search notes and pages…')} aria-label={msg('Search notes and pages…')} maxLength={1024} disabled={busy} />
    <Button type="submit" disabled={busy || provider === 'notion' && !query.trim()}>{msg('Search')}</Button>
   </form>
   <div className={styles.results} aria-busy={busy}>
    {result?.items.map(item => <label className={styles.row} key={item.id}>
     <input type="checkbox" checked={selected.includes(item.id)} disabled={busy || !selected.includes(item.id) && selected.length >= 4} onChange={event => setSelected(old => event.target.checked ? [...old, item.id] : old.filter(id => id !== item.id))} />
     <span className={styles.copy}><span>{item.title}</span><small>{item.description || item.url}</small></span>
    </label>)}
    {!busy && !error && (result ? result.items.length === 0 && <p>{msg('No matching sources found.')}</p> : <p>{msg('Search, then choose up to four sources to attach.')}</p>)}
   </div>
   {result?.more && <p>{msg('More sources are available. Refine your search to narrow the results.')}</p>}
  </> : connected ? <><p>{status.data?.account?.name}</p><p>{msg('Disconnecting does not delete documents already attached to chats.')}</p></>
   : state === 'not-connected' ? provider === 'notion' ? <p>{msg('Continue to Notion to choose a workspace and review access. Your chat stays here.')}</p>
    : <><label htmlFor="source-vault-folder">{msg('Vault folder')}</label><Input id="source-vault-folder" value={vault} onChange={event => setVault(event.target.value)} disabled={busy} autoComplete="off" spellCheck={false} />
       <details><summary>{msg('How to find your vault folder')}</summary><p>{msg('In Obsidian, open Manage vaults and copy the folder path shown below your vault name. Paste that full path here. No plugin is needed.')}</p></details></>
   : <p>{state === undefined ? msg('Loading…') : msg('Unavailable')}</p>}
  {provider === 'notion' && <p>{msg('Notion may include connected workspace sources in search. Only Notion pages you select are attached to chat.')}</p>}
  {busy && <p role="status">{msg('Working…')}</p>}
  {notice && <p role="status">{notice}</p>}{error && <><Alert>{error}</Alert>{connected && <Button variant="quiet" disabled={busy} onClick={() => void (provider === 'notion' ? connect() : disconnect())}>{provider === 'notion' ? msg('Reconnect') : msg('Disconnect')}</Button>}</>}
 </Dialog>
}
