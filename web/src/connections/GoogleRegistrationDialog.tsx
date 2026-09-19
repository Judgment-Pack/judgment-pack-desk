import { useEffect, useRef, useState, type RefObject } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { msg, useLocale } from '../i18n'
import { Alert } from '../ui/Alert'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { connectionCall, CONNECTIONS_KEY, useDriveStatus, type ConnectionProvider } from './client'
import { googleRegistration } from './registration'

/** Admin owns application setup; the chat dialog owns personal consent. */
export function GoogleRegistrationDialog({ provider, available, openerRef, onClose }: {
  provider: ConnectionProvider; available: boolean; openerRef: RefObject<HTMLElement | null>; onClose: () => void
}) {
  useLocale()
  const query = useDriveStatus(available, provider), client = useQueryClient()
  const state = available && !query.isError ? query.data?.state : 'unavailable'
  const editable = state === 'setup-required' || state === 'not-connected'
  const [busy, setBusy] = useState(false), [error, setError] = useState(''), [saved, setSaved] = useState(false)
  const file = useRef<HTMLInputElement>(null), active = useRef<AbortController | null>(null)
  useEffect(() => () => { active.current?.abort(); active.current = null }, [provider, available])
  function close() { active.current?.abort(); active.current = null; onClose() }
  async function configure(chosen?: File) {
    if (!chosen || !editable || active.current) return
    const task = new AbortController(); active.current = task; setBusy(true); setError(''); setSaved(false)
    const current = () => active.current === task && !task.signal.aborted
    try {
      if (chosen.size > 16_384) throw new SyntaxError()
      const registration = googleRegistration(await chosen.text())
      if (!current()) return
      await connectionCall('configure', registration, task.signal, provider)
      await client.invalidateQueries({ queryKey: CONNECTIONS_KEY })
      if (current()) setSaved(true)
    } catch (cause) {
      if (current()) setError(cause instanceof SyntaxError ? msg('Choose the credentials JSON for a Google Desktop app.') : (cause as Error).message)
    } finally {
      if (current()) { active.current = null; setBusy(false) }
      if (file.current) file.current.value = ''
    }
  }
  return <Dialog open onOpenChange={next => { if (!next) close() }} openerRef={openerRef}
    title={msg('{{provider}} registration', { provider: provider === 'gmail' ? msg('Gmail') : msg('Google Drive') })}
    description={msg('Personal · This computer')}
    footer={<DialogActions><Button variant="quiet" onClick={close}>{busy ? msg('Cancel') : msg('Close')}</Button></DialogActions>}>
    {editable ? <>
      <p>{provider === 'gmail' ? msg('Register a Desktop app in Google Cloud and enable Gmail API with read-only access. Select its downloaded credentials JSON.') : msg('Register a Desktop app in Google Cloud and enable the Drive and Picker APIs. Select its downloaded credentials JSON.')}</p>
      <p><a href="https://developers.google.com/identity/protocols/oauth2/native-app" target="_blank" rel="noreferrer">{msg('Guide')}</a></p>
      <Button disabled={busy} onClick={() => file.current?.click()}>{msg('Choose credentials file')}</Button>
      <input ref={file} type="file" accept=".json,application/json" hidden onChange={event => void configure(event.target.files?.[0])} />
    </> : <p>{state === 'connected' ? msg('Disconnect this account in My connections before changing its registration.') : state === undefined ? msg('Loading…') : msg('Unavailable')}</p>}
    {busy && <p role="status">{msg('Working…')}</p>}
    {saved && <p role="status">{msg('Registration saved. Connect your account from the chat attachment menu.')}</p>}
    {error && <Alert>{error}</Alert>}
  </Dialog>
}
