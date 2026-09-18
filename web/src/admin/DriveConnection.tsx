import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { msg, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Alert } from '../ui/Alert'
import { SettingRow } from '../ui/SettingRow'
import { authorizeDrive, connectionCall, CONNECTIONS_KEY, useDriveStatus, type ConnectionProvider } from '../connections/client'

export function DriveConnection({ available }: { available: boolean }) { return <GoogleConnection available={available} provider="google-drive" /> }
export function GoogleConnection({ available, provider }: { available: boolean; provider: ConnectionProvider }) {
 useLocale()
 const query = useDriveStatus(available, provider), client = useQueryClient()
 const [open, setOpen] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState('')
 const opener = useRef<HTMLButtonElement>(null), file = useRef<HTMLInputElement>(null), operation = useRef<AbortController | null>(null)
 useEffect(() => () => operation.current?.abort(), [available, provider])
 const state = available ? query.data?.state ?? 'unavailable' : 'unavailable'
 const refresh = () => client.invalidateQueries({ queryKey: CONNECTIONS_KEY })
 async function configure(chosen?: File) {
  if (!chosen || busy) return
  setBusy(true); setError('')
  try {
   if (chosen.size > 16_384) throw new Error(msg('Choose the credentials JSON for a Google Desktop app.'))
   const data = JSON.parse(await chosen.text()).installed
   if (!data || typeof data.client_id !== 'string' || typeof data.client_secret !== 'string') throw new Error(msg('Choose the credentials JSON for a Google Desktop app.'))
   await connectionCall('configure', { clientId: data.client_id, clientSecret: data.client_secret }, undefined, provider)
   await refresh(); setOpen(false)
  } catch (cause) { setError(cause instanceof SyntaxError ? msg('Choose the credentials JSON for a Google Desktop app.') : (cause as Error).message) }
  finally { setBusy(false); if (file.current) file.current.value = '' }
 }
 async function connect() {
  if (busy) return
  const controller = new AbortController(); operation.current = controller; setBusy(true); setError(''); setNotice('')
  try { await authorizeDrive('connect', controller.signal, provider); await refresh() }
  catch (cause) { if (!controller.signal.aborted) setError((cause as Error).message) }
  finally { operation.current = null; setBusy(false) }
 }
 async function disconnect() {
  setBusy(true); setError('')
  try { const result = await connectionCall<{revoked: boolean}>('disconnect', {}, undefined, provider); await refresh(); setOpen(false); if (!result.revoked) setNotice(msg('Disconnected here. Remove access in your Google account to finish revoking access.')) }
  catch (cause) { setError((cause as Error).message) }
  finally { setBusy(false) }
 }
 const description = state === 'connected' ? query.data?.account?.email : state === 'setup-required' ? msg('Not configured') : state === 'not-connected' ? msg('Not connected') : state === 'blocked' ? msg('Managed by your organization') : msg('Unavailable')
 return <>
  <SettingRow title={provider === 'gmail' ? msg('Gmail') : msg('Google Drive')} description={description} action={state === 'unavailable' || state === 'blocked' ? undefined : <Button ref={opener} disabled={busy} onClick={() => { setError(''); setOpen(true) }}>{state === 'setup-required' ? msg('Set up') : msg('Manage')}</Button>} />
  {busy && operation.current && <p role="status">{msg('Continue in the Google sign-in window.')} <Button variant="quiet" onClick={() => operation.current?.abort()}>{msg('Cancel')}</Button></p>}
  {error && !open && <Alert>{error}</Alert>}{notice && <p role="status">{notice}</p>}
  <Dialog open={open} onOpenChange={next => { if (!busy) setOpen(next) }} title={provider === 'gmail' ? msg('Gmail') : msg('Google Drive')} openerRef={opener}
   description={provider === 'gmail' ? msg('Gmail grants read access to your mailbox. Only emails you select are attached to chat. Credentials stay with the gateway.') : msg('Only files you select are used. Google credentials stay with the gateway.')}
   footer={<DialogActions><Button variant="quiet" disabled={busy} onClick={() => setOpen(false)}>{msg('Close')}</Button>{state === 'connected' ? <Button disabled={busy} onClick={() => void disconnect()}>{msg('Disconnect')}</Button> : state === 'not-connected' ? <Button disabled={busy} onClick={() => { setOpen(false); void connect() }}>{msg('Connect')}</Button> : <Button disabled={busy} onClick={() => file.current?.click()}>{msg('Choose credentials file')}</Button>}</DialogActions>}>
   {state === 'connected' ? <><p>{query.data?.account?.email}</p><p>{msg('Disconnecting does not delete documents already attached to chats.')}</p><p>{msg('Google may also disconnect other connections using the same Cloud project.')}</p></> : <p>{provider === 'gmail' ? msg('Register a Desktop app in Google Cloud and enable Gmail API with read-only access. Select its downloaded credentials JSON.') : msg('Register a Desktop app in Google Cloud and enable the Drive and Picker APIs. Select its downloaded credentials JSON.')}</p>}
   <input ref={file} type="file" accept=".json,application/json" hidden onChange={event => void configure(event.target.files?.[0])} />
   {state !== 'connected' && <p><a href={provider === 'gmail' ? 'https://developers.google.com/workspace/gmail/api/auth/scopes' : 'https://developers.google.com/workspace/drive/picker/guides/desktop-mobile-picker'} target="_blank" rel="noreferrer">{msg('Guide')}</a></p>}
   {state === 'not-connected' && <Button variant="quiet" disabled={busy} onClick={() => file.current?.click()}>{msg('Choose credentials file')}</Button>}
   {error && <Alert>{error}</Alert>}
  </Dialog>
 </>
}
