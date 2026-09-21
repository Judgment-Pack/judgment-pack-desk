import { useCallback, useMemo, useRef, useState, type RefObject } from 'react'
import { useLocation } from 'react-router-dom'
import { msg, useLocale } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { GoogleRegistrationSetup } from '../connections/GoogleRegistrationSetup'
import { useInspectorPortal } from '../shell/InspectorSlot'
import { useInspectorPresentation } from '../shell/InspectorPresentation'
import { useDriveStatus, type ConnectionProvider } from '../connections/client'
import { Button, ButtonLink } from '../ui/Button'
import { SettingRow } from '../ui/SettingRow'
import { SettingsSection } from '../ui/SettingsSection'

export function ConnectionSettings() {
  useLocale()
  const effective = useEffectiveConfig()
  const requestedReturn = useLocation().state?.returnTo
  const returnTo = typeof requestedReturn === 'string' && /^\/(?:$|(?:chats|packs)(?:\/|$))/.test(requestedReturn) && !requestedReturn.includes('\\') ? requestedReturn : undefined
  const available = effective.desk?.localGateway?.status === 'ready' && !effective.desk?.decoded?.values?.research?.gateway
  const context = JSON.stringify([available, effective.config.research.gateway])
  return <SettingsSection title={msg('Connections')} level={2} variant="standalone"
    description={msg('Use your own Google app to connect Drive or Gmail on this computer.')}>
    <p>{msg('Personal · This computer')}</p>
    <Registrations key={context} available={available} />
    {returnTo && <ButtonLink to={returnTo}>{msg('Return to chat')}</ButtonLink>}
    {!available && <ButtonLink to="/admin#storage">{msg('Manage gateway')}</ButtonLink>}
  </SettingsSection>
}

function Registrations({ available }: { available: boolean }) {
  const locale = useLocale()
  const [selection, setSelection] = useState<{ provider: ConnectionProvider; instructionsOpen: boolean } | null>(null)
  const [width, setWidth] = useState(480)
  const driveOpener = useRef<HTMLButtonElement>(null), gmailOpener = useRef<HTMLButtonElement>(null)
  // Keep the initiating control through the drawer's asynchronous close.
  const opener = useRef<HTMLButtonElement>(null)
  const close = useCallback(() => { setSelection(null); opener.current?.focus() }, [])
  const reset = useCallback(() => setWidth(480), [])
  const onOpenChange = useCallback((open: boolean) => { if (!open) close() }, [close])
  const title = msg('{{provider}} registration', { provider: selection?.provider === 'gmail' ? msg('Gmail') : msg('Google Drive') })
  const presentation = useMemo(() => ({ title, available: !!selection, open: !!selection,
    onOpenChange, width, onResize: setWidth, onReset: reset, minimumMainWidth: 560, maximumWidth: 560, closeOnEscape: true, restoreFocusRef: opener
  }), [title, selection, onOpenChange, width, reset, locale, opener])
  useInspectorPresentation(presentation)
  const setup = useInspectorPortal(selection ? <GoogleRegistrationSetup key={selection.provider} provider={selection.provider}
    available={available} instructionsInitiallyOpen={selection.instructionsOpen} onClose={close} /> : null)
  return <>
    <RegistrationRow provider="google-drive" available={available} opener={driveOpener} onOpen={instructionsOpen => { opener.current = driveOpener.current; setSelection({ provider: 'google-drive', instructionsOpen }) }} />
    <RegistrationRow provider="gmail" available={available} opener={gmailOpener} onOpen={instructionsOpen => { opener.current = gmailOpener.current; setSelection({ provider: 'gmail', instructionsOpen }) }} />
    {setup}
  </>
}

function RegistrationRow({ provider, available, opener, onOpen }: { provider: ConnectionProvider; available: boolean; opener: RefObject<HTMLButtonElement | null>; onOpen: (instructionsOpen: boolean) => void }) {
  const query = useDriveStatus(available, provider)
  const state = available && !query.isError ? query.data?.state : 'unavailable'
  const title = provider === 'gmail' ? msg('Gmail') : msg('Google Drive')
  const usable = state === 'setup-required' || state === 'not-connected' || state === 'connected'
  return <>
    <SettingRow title={title} description={state === undefined ? msg('Loading…') : !usable ? msg('Unavailable') : state === 'setup-required' ? msg('Not configured') : msg('Configured')}
      action={<Button ref={opener} disabled={!usable} aria-label={msg('{{provider}} registration', { provider: title })} onClick={() => onOpen(state === 'setup-required')}>{state === 'setup-required' ? msg('Set up') : msg('Manage')}</Button>} />
  </>
}
