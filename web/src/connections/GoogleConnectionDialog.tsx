import { useEffect, useRef, useState, type RefObject } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { msg, useLocale } from '../i18n'
import { Alert } from '../ui/Alert'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Disclosure } from '../ui/Disclosure'
import { authorizeDrive, connectionCall, CONNECTIONS_KEY, useDriveStatus, type ConnectionProvider, type DriveSelection } from './client'

/** Personal consent, shared by the composer and account settings. Provider
 * registration and tokens stay in the gateway; no credentials enter a chat. */
export function GoogleConnectionDialog({ open, onOpenChange, provider, available, openerRef, onSelected }: {
  open: boolean; onOpenChange: (open: boolean) => void; provider: ConnectionProvider; available: boolean
  openerRef: RefObject<HTMLElement | null>; onSelected?: (files: DriveSelection[]) => void
}) {
  useLocale()
  const query = useDriveStatus(open && available, provider), client = useQueryClient()
  const state = available && !query.isError ? query.data?.state : 'unavailable'
  const [busy, setBusy] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState('')
  const file = useRef<HTMLInputElement>(null), active = useRef<AbortController | null>(null)
  useEffect(() => {
    setBusy(false); setError(''); setNotice('')
    return () => { active.current?.abort(); active.current = null }
  }, [open, available, provider])
  const refresh = () => client.invalidateQueries({ queryKey: CONNECTIONS_KEY })
  function operation() {
    if (active.current) return
    const task = new AbortController(); active.current = task; setBusy(true); setError(''); setNotice('')
    return task
  }
  const current = (task: AbortController) => active.current === task && !task.signal.aborted
  function finish(task: AbortController) { if (active.current === task) { active.current = null; setBusy(false) } }
  function close() { active.current?.abort(); active.current = null; setBusy(false); onOpenChange(false) }
  async function configure(chosen?: File) {
    if (!chosen) return
    const task = operation(); if (!task) return
    try {
      if (chosen.size > 16_384) throw new SyntaxError()
      const data = JSON.parse(await chosen.text())?.installed
      if (!data || typeof data.client_id !== 'string' || !data.client_id.trim() || (data.client_secret !== undefined && typeof data.client_secret !== 'string')) throw new SyntaxError()
      if (!current(task)) return
      await connectionCall('configure', { clientId: data.client_id, clientSecret: data.client_secret ?? '' }, task.signal, provider)
      await refresh()
    } catch (cause) {
      if (current(task)) setError(cause instanceof SyntaxError ? msg('Choose the credentials JSON for a Google Desktop app.') : (cause as Error).message)
    } finally { finish(task); if (file.current) file.current.value = '' }
  }
  async function connect() {
    const task = operation(); if (!task) return
    try {
      // The browser must open synchronously from this click, not after a fetch.
      const selections = await authorizeDrive(onSelected && provider === 'google-drive' ? 'pick' : 'connect', task.signal, provider)
      await refresh()
      if (current(task) && onSelected) { onSelected(selections); onOpenChange(false) }
    } catch (cause) { if (current(task)) setError((cause as Error).message) }
    finally { finish(task) }
  }
  async function disconnect() {
    const task = operation(); if (!task) return
    try {
      const result = await connectionCall<{ revoked: boolean }>('disconnect', {}, task.signal, provider)
      await refresh()
      if (current(task) && !result.revoked) setNotice(msg('Disconnected here. Remove access in your Google account to finish revoking access.'))
    } catch (cause) { if (current(task)) setError((cause as Error).message) }
    finally { finish(task) }
  }
  const usable = state === 'not-connected' || state === 'connected'
  return <Dialog open={open} onOpenChange={next => { if (!next) close() }} openerRef={openerRef}
    title={provider === 'gmail' ? msg('Gmail') : msg('Google Drive')}
    description={provider === 'gmail' ? msg('Gmail grants read access to your mailbox. Only emails you select are attached to chat. Credentials stay with the gateway.') : msg('Only files you select are used. Google credentials stay with the gateway.')}
    footer={<DialogActions><Button variant="quiet" onClick={close}>{busy ? msg('Cancel') : msg('Close')}</Button>
      {state === 'connected' && !onSelected ? <Button disabled={busy} onClick={() => void disconnect()}>{msg('Disconnect')}</Button>
        : usable && <Button variant="primary" disabled={busy} onClick={() => void connect()}>{msg('Continue')}</Button>}
    </DialogActions>}>
    {state === 'connected' ? <><p>{query.data?.account?.email}</p>{!onSelected && <><p>{msg('Disconnecting does not delete documents already attached to chats.')}</p><p>{msg('Google may also disconnect other connections using the same Cloud project.')}</p></>}</>
      : state === 'setup-required' ? <p>{msg('Google sign-in is not configured in this build. You can use your own Google app below.')}</p>
      : state === 'not-connected' ? <p>{msg('Continue to Google to choose your account and review access. Your chat stays here.')}</p>
      : <p>{state === undefined ? msg('Loading…') : msg('Unavailable')}</p>}
    {(state === 'setup-required' || state === 'not-connected') && <Disclosure title={msg('Use your own Google app')}>
      <p>{provider === 'gmail' ? msg('Register a Desktop app in Google Cloud and enable Gmail API with read-only access. Select its downloaded credentials JSON.') : msg('Register a Desktop app in Google Cloud and enable the Drive and Picker APIs. Select its downloaded credentials JSON.')}</p>
      <p><a href={provider === 'gmail' ? 'https://developers.google.com/workspace/gmail/api/auth/scopes' : 'https://developers.google.com/workspace/drive/picker/guides/desktop-mobile-picker'} target="_blank" rel="noreferrer">{msg('Guide')}</a></p>
      <Button disabled={busy} onClick={() => file.current?.click()}>{msg('Choose credentials file')}</Button>
      <input ref={file} type="file" accept=".json,application/json" hidden onChange={event => void configure(event.target.files?.[0])} />
    </Disclosure>}
    {busy && <p role="status">{msg('Working…')}</p>}
    {notice && <p role="status">{notice}</p>}
    {error && <Alert>{error}</Alert>}
  </Dialog>
}
