import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { msg, useLocale } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { GoogleRegistrationSetup } from '../connections/GoogleRegistrationSetup'
import { useInspectorPortal } from '../shell/InspectorSlot'
import { useInspectorPresentation } from '../shell/InspectorPresentation'
import { type ConnectionProvider } from '../connections/client'
import { useConnections } from '../connections/catalog'
import { useConnectionsPane } from '../connections/ConnectionPaneContext'
import { ConnectionDirectory, connectionState } from '../connections/ConnectionDirectory'
import { ButtonLink } from '../ui/Button'
import { SettingsSection } from '../ui/SettingsSection'

export function ConnectionSettings() {
  useLocale()
  const effective = useEffectiveConfig()
  const requestedReturn = useLocation().state?.returnTo
  const returnTo = typeof requestedReturn === 'string' && /^\/(?:$|(?:chats|packs)(?:\/|$))/.test(requestedReturn) && !requestedReturn.includes('\\') ? requestedReturn : undefined
  const available = effective.desk?.localGateway?.status === 'ready' && !effective.desk?.decoded?.values?.research?.gateway
  const context = JSON.stringify([available, effective.config.research.gateway])
  return <SettingsSection title={msg('Connections')} level={2} variant="standalone"
    description={msg('Personal · This computer')}>
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
    <ConnectionDirectory {...catalog} available={available} mode="admin" retry={() => void catalog.refetch()} onSelect={(entry, button) => {
      if (entry.descriptor.registration === 'google-desktop') {
        connectionsPane.close?.({ restoreFocus: false })
        opener.current = button
        setSelection({ provider: entry.descriptor.id, instructionsOpen: connectionState(entry) === 'setup-required' })
      } else {
        setSelection(null)
        connectionsPane.open({ provider: entry.descriptor.id, descriptor: entry.descriptor, opener: button })
      }
    }} />
    {setup}
  </>
}
