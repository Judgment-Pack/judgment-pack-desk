import { useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { msg, useLocale } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { GoogleRegistrationDialog } from '../connections/GoogleRegistrationDialog'
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
    <RegistrationRow key={`${context}-drive`} provider="google-drive" available={available} />
    <RegistrationRow key={`${context}-gmail`} provider="gmail" available={available} />
    {returnTo && <ButtonLink to={returnTo}>{msg('Return to chat')}</ButtonLink>}
    {!available && <ButtonLink to="/admin#storage">{msg('Manage gateway')}</ButtonLink>}
  </SettingsSection>
}

function RegistrationRow({ provider, available }: { provider: ConnectionProvider; available: boolean }) {
  const query = useDriveStatus(available, provider)
  const state = available && !query.isError ? query.data?.state : 'unavailable'
  const [open, setOpen] = useState(false), opener = useRef<HTMLButtonElement>(null)
  const title = provider === 'gmail' ? msg('Gmail') : msg('Google Drive')
  const usable = state === 'setup-required' || state === 'not-connected' || state === 'connected'
  return <>
    <SettingRow title={title} description={state === undefined ? msg('Loading…') : !usable ? msg('Unavailable') : state === 'setup-required' ? msg('Not configured') : msg('Configured')}
      action={<Button ref={opener} disabled={!usable} aria-label={msg('{{provider}} registration', { provider: title })} onClick={() => setOpen(true)}>{state === 'setup-required' ? msg('Set up') : msg('Manage')}</Button>} />
    {open && <GoogleRegistrationDialog provider={provider} available={available} openerRef={opener} onClose={() => setOpen(false)} />}
  </>
}
