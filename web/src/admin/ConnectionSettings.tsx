import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { msg, useLocale } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { GoogleRegistrationSetup } from '../connections/GoogleRegistrationSetup'
import { useInspectorPortal } from '../shell/InspectorSlot'
import { useInspectorPresentation } from '../shell/InspectorPresentation'
import { type ConnectionProvider, type ConnectionStatus } from '../connections/client'
import { useConnections } from '../connections/catalog'
import { useConnectionsPane } from '../connections/ConnectionPaneContext'
import { providerName, providerDescription } from '../connections/registry'
import { Alert } from '../ui/Alert'
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
    description={msg('Connect sources for chats and research.')}>
    <p>{msg('Personal · This computer')}</p>
    <ConnectionCatalog key={context} available={available} />
    {returnTo && <ButtonLink to={returnTo}>{msg('Return to chat')}</ButtonLink>}
    {!available && <ButtonLink to="/admin#storage">{msg('Manage gateway')}</ButtonLink>}
  </SettingsSection>
}

function ConnectionCatalog({ available }: { available: boolean }) {
  const locale = useLocale()
  const catalog = useConnections(available)
  const connectionsPane = useConnectionsPane()
  const [selection, setSelection] = useState<{ provider: ConnectionProvider; instructionsOpen: boolean } | null>(null)
  const [width, setWidth] = useState(480)
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
  // A catalog refresh can withdraw a capability while its setup is open.
  const selectedEntry = catalog.entries.find(entry => entry.descriptor.id === selection?.provider && entry.descriptor.registration === 'google-desktop')
  useEffect(() => { if (selection && !selectedEntry) close() }, [selection, selectedEntry, close])
  const setup = useInspectorPortal(selection && selectedEntry ? <GoogleRegistrationSetup key={selection.provider} provider={selection.provider}
    available={available} instructionsInitiallyOpen={selection.instructionsOpen} onClose={close} /> : null)
  return <>
    {catalog.entries.map(({ descriptor, status }) => {
      const title = providerName(descriptor.id, descriptor)
      const state = status.isError ? 'unavailable' : status.data?.state
      const registration = descriptor.registration === 'google-desktop'
      const usable = state === 'setup-required' || state === 'not-connected' || state === 'connected'
      const action = state === 'setup-required' ? msg('Set up') : state === 'not-connected' && !registration ? msg('Connect') : msg('Manage')
      return <SettingRow key={descriptor.id} title={title} description={providerDescription(descriptor.id, descriptor)} status={statusLabel(state)}
        action={<Button disabled={state === undefined || registration && !usable}
          aria-label={msg('{{action}}: {{provider}}', { action, provider: registration ? msg('{{provider}} registration', { provider: title }) : title })}
          onClick={event => {
            if (registration) {
              // The registration pane takes focus; the old utility must not
              // send it back to a previous provider's button on the next frame.
              connectionsPane.close?.({ restoreFocus: false })
              opener.current = event.currentTarget
              setSelection({ provider: descriptor.id, instructionsOpen: state === 'setup-required' })
            } else {
              setSelection(null)
              connectionsPane.open({ provider: descriptor.id, descriptor, opener: event.currentTarget })
            }
          }}>{action}</Button>} />
    })}
    {catalog.unsupported.map(descriptor => <SettingRow key={descriptor.id} title={providerName(descriptor.id, descriptor)}
      description={providerDescription(descriptor.id, descriptor)} status={msg('Update Desk to use this connection.')} />)}
    {catalog.loading ? <p role="status">{msg('Loading…')}</p> : !available ? <p>{msg('Local processing is unavailable. Check the details in Admin → Storage & data.')}</p> : catalog.isError ? <>
      <Alert>{msg('Connections could not be loaded.')}</Alert><Button onClick={() => void catalog.refetch()}>{msg('Retry')}</Button>
    </> : catalog.entries.length === 0 && catalog.unsupported.length === 0 && <p>{msg('No connections found.')}</p>}
    {setup}
  </>
}

function statusLabel(state: ConnectionStatus['state'] | undefined) {
  switch (state) {
    case 'connected': return msg('Connected')
    case 'not-connected': return msg('Not connected')
    case 'setup-required': return msg('Not configured')
    case 'blocked': return msg('Managed by your organization')
    case 'unavailable': return msg('Unavailable')
    default: return msg('Loading…')
  }
}
