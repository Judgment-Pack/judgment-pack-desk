import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { msg, useLocale } from '../i18n'
import { Alert } from '../ui/Alert'
import { Button } from '../ui/Button'
import { Disclosure } from '../ui/Disclosure'
import { useInspectorControls } from '../shell/InspectorSlot'
import { GoogleRegistrationGuide } from './GoogleRegistrationGuide'
import styles from './GoogleRegistrationSetup.module.css'
import { connectionCall, CONNECTIONS_KEY, useDriveStatus, type ConnectionProvider } from './client'
import { googleRegistration } from './registration'

/** Admin owns application setup; the chat dialog owns personal consent. */
export function GoogleRegistrationSetup({ provider, available, instructionsInitiallyOpen, onClose, contextual = false }: {
  provider: ConnectionProvider; available: boolean; instructionsInitiallyOpen: boolean; onClose: () => void; contextual?: boolean
}) {
  useLocale()
  const query = useDriveStatus(available, provider), client = useQueryClient()
  const state = available && !query.isError ? query.data?.state : 'unavailable'
  const editable = state === 'setup-required' || state === 'not-connected'
  const [busy, setBusy] = useState(false), [error, setError] = useState(''), [saved, setSaved] = useState(false)
  const file = useRef<HTMLInputElement>(null), active = useRef<AbortController | null>(null)
  const [instructionsOpen, setInstructionsOpen] = useState(instructionsInitiallyOpen)
  const panel = useRef<HTMLElement>(null)
  const { open } = useInspectorControls()
  useEffect(() => { if (open) panel.current?.focus({ preventScroll: true }) }, [open])
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
  return <section ref={panel} tabIndex={-1} className={styles.setup}
    aria-label={msg('{{provider}} registration', { provider: provider === 'gmail' ? msg('Gmail') : msg('Google Drive') })}>
    <div className={styles.body}>
      <p className={styles.scope}>{msg('Personal · This computer')}</p>
      <p>{msg('Set up a Google app once on this computer, then connect your account.')}</p>
      {!editable && <p>{state === 'connected' ? msg('Disconnect this account in My connections before changing its registration.') : state === undefined ? msg('Loading…') : msg('Unavailable')}</p>}
      <Disclosure title={msg('Setup instructions')} open={instructionsOpen} onToggle={event => setInstructionsOpen(event.currentTarget.open)}>
        <GoogleRegistrationGuide provider={provider} />
      </Disclosure>
    </div>
    <footer className={styles.footer}>
      {busy && <p role="status">{msg('Working…')}</p>}
      {saved && !contextual && <p role="status">{msg('Registration saved. Connect your account from the chat attachment menu.')}</p>}
      {error && <Alert>{error}</Alert>}
      <div className={styles.actions}>
        {editable && <>
          <Button variant="primary" disabled={busy} onClick={() => file.current?.click()}>{msg('Choose credentials file')}</Button>
          <input ref={file} type="file" accept=".json,application/json" hidden onChange={event => void configure(event.target.files?.[0])} />
        </>}
        {(!contextual || busy) && <Button variant="quiet" onClick={close}>{busy ? msg('Cancel') : msg('Done')}</Button>}
      </div>
    </footer>
  </section>
}
