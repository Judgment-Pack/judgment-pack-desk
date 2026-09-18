import { useEffect, useRef, useState, type RefObject } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { msg, useLocale } from '../i18n'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { Alert } from '../ui/Alert'
import { authorizeDrive, connectionCall, CONNECTIONS_KEY, type MailSearch, type MailSelection } from './client'
import styles from './GmailPicker.module.css'

export function GmailPicker({ open, onOpenChange, state, accountId, onSelect, openerRef }: {
  open: boolean; onOpenChange: (open: boolean) => void; state?: string; accountId?: string
  onSelect: (items: MailSelection[]) => void; openerRef: RefObject<HTMLElement | null>
}) {
  useLocale()
  const client = useQueryClient()
  const [submittedQuery, setSubmittedQuery] = useState('')
  const [query, setQuery] = useState(''), [result, setResult] = useState<MailSearch>({ selectionContext: '', messages: [] })
  const [selected, setSelected] = useState<string[]>([]), [busy, setBusy] = useState(false), [error, setError] = useState('')
  const active = useRef<AbortController | null>(null)
  const searchInput = useRef<HTMLInputElement>(null)
  function operation() { active.current?.abort(); const next = new AbortController(); active.current = next; setBusy(true); setError(''); return next }
  async function search(text: string, pageToken?: string) {
    const task = operation()
    try {
      const next = await connectionCall<MailSearch>('search', { query: text, ...(pageToken ? { pageToken } : {}) }, task.signal, 'gmail')
      if (active.current === task && !task.signal.aborted) { setResult(next); setSelected([]); setSubmittedQuery(text) }
    } catch (cause) { if (!task.signal.aborted) setError((cause as Error).message) }
    finally { if (active.current === task) { active.current = null; setBusy(false) } }
  }
  useEffect(() => {
    setQuery(''); setResult({ selectionContext: '', messages: [] }); setSelected([]); setError(''); setBusy(false)
    if (open && state === 'connected') void search('')
    return () => { active.current?.abort(); active.current = null }
  }, [open, state, accountId])
  async function connect() {
    const task = operation()
    try { await authorizeDrive('connect', task.signal, 'gmail'); if (!task.signal.aborted) await client.invalidateQueries({ queryKey: CONNECTIONS_KEY }) }
    catch (cause) { if (!task.signal.aborted) setError((cause as Error).message) }
    finally { if (active.current === task) { active.current = null; setBusy(false) } }
  }
  async function attach() {
    const task = operation()
    try {
      const choices = await connectionCall<MailSelection[]>('select', { messageIds: selected, selectionContext: result.selectionContext }, task.signal, 'gmail')
      if (active.current === task && !task.signal.aborted) { onSelect(choices); onOpenChange(false) }
    } catch (cause) { if (!task.signal.aborted) setError((cause as Error).message) }
    finally { if (active.current === task) { active.current = null; setBusy(false) } }
  }
  return <Dialog open={open} onOpenChange={onOpenChange} title={msg('Gmail')} openerRef={openerRef}
    description={msg('Choose up to four emails. Message text is attached; mail attachments are excluded.')}
    footer={<DialogActions><Button variant="quiet" onClick={() => onOpenChange(false)}>{msg('Cancel')}</Button>{state === 'connected'
      ? <Button variant="primary" disabled={busy || !selected.length || !result.selectionContext} onClick={() => void attach()}>{msg('Attach selected emails')}</Button>
      : <Button variant="primary" disabled={busy || state !== 'not-connected'} onClick={() => void connect()}>{msg('Connect')}</Button>}</DialogActions>}>
    {state === 'connected' ? <>
      <form className={styles.search} onSubmit={event => { event.preventDefault(); void search(query) }}>
        <Input ref={searchInput} value={query} onChange={event => setQuery(event.target.value)} placeholder={msg('Search Gmail…')} aria-label={msg('Search Gmail…')} maxLength={1024} disabled={busy} />
        <Button type="submit" disabled={busy}>{msg('Search')}</Button>
      </form>
      <div className={styles.results} aria-busy={busy}>
        {result.messages.map(message => <label className={styles.row} key={message.id}>
          <input type="checkbox" checked={selected.includes(message.id)} disabled={busy || !selected.includes(message.id) && selected.length >= 4}
            onChange={event => setSelected(items => event.target.checked ? [...items, message.id] : items.filter(id => id !== message.id))} />
          <span className={styles.copy}><span>{message.subject || msg('No subject')}</span><small>{message.from}</small><small>{message.date}</small></span>
        </label>)}
        {!busy && !error && result.messages.length === 0 && <p className={styles.empty}>{msg('No emails found.')}</p>}
      </div>
      {result.nextPageToken && <Button variant="quiet" disabled={busy} onClick={() => void search(submittedQuery, result.nextPageToken)}>{msg('Next page')}</Button>}
    </> : <p>{msg('Gmail grants read access to your mailbox. Only emails you select are attached to chat. Credentials stay with the gateway.')}</p>}
    {busy && <p role="status">{state === 'connected' ? msg('Loading…') : msg('Continue in the Google sign-in window.')}</p>}
    {error && <Alert>{error}</Alert>}
  </Dialog>
}
