import { useEffect, useRef, useState, type RefObject } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { msg, useLocale } from '../i18n'
import { Alert } from '../ui/Alert'
import { Button, ButtonLink } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { useLocation } from 'react-router-dom'
import { authorizeDrive, connectionCall, CONNECTIONS_KEY, useDriveStatus, type ConnectionProvider, type DriveSelection } from './client'

/** Personal consent, shared by the composer and account settings. Provider
 * registration and tokens stay in the gateway; no credentials enter a chat. */
export function GoogleConnectionDialog({ open, onOpenChange, provider, available, openerRef, onSelected }: {
  open: boolean; onOpenChange: (open: boolean) => void; provider: ConnectionProvider; available: boolean
  openerRef: RefObject<HTMLElement | null>; onSelected?: (files: DriveSelection[]) => void
}) {
  useLocale()
  const location = useLocation()
  const query = useDriveStatus(open && available, provider), client = useQueryClient()
  const state = available && !query.isError ? query.data?.state : 'unavailable'
  const [busy, setBusy] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState('')
  const active = useRef<AbortController | null>(null)
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
        : usable && <Button variant="primary" disabled={busy} onClick={() => void connect()}>{msg('Continue with Google')}</Button>}
    </DialogActions>}>
    {state === 'connected' ? <><p>{query.data?.account?.email}</p>{!onSelected && <><p>{msg('Disconnecting does not delete documents already attached to chats.')}</p><p>{msg('Google may also disconnect other connections using the same Cloud project.')}</p></>}</>
      : state === 'setup-required' ? <p>{msg('Configure Google registration in Admin → Connections before signing in.')}</p>
      : state === 'not-connected' ? <p>{msg('Continue to Google to choose your account and review access. Your chat stays here.')}</p>
      : <p>{state === undefined ? msg('Loading…') : msg('Unavailable')}</p>}
    {state === 'setup-required' && <ButtonLink to="/admin#connections" state={{ returnTo: location.pathname + location.search + location.hash }} onClick={close}>{msg('Set up in Admin')}</ButtonLink>}
    {busy && <p role="status">{msg('Working…')}</p>}
    {notice && <p role="status">{notice}</p>}
    {error && <Alert>{error}</Alert>}
  </Dialog>
}
