import { useRef, useState, type RefObject } from 'react'
import { msg, useLocale } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { SettingRow } from '../ui/SettingRow'
import { GoogleConnectionDialog } from './GoogleConnectionDialog'
import { useDriveStatus, type ConnectionProvider } from './client'

/** Local OS-account connections. This is not an organization policy surface. */
export function PersonalConnections({ open, onOpenChange, openerRef }: {
  open: boolean; onOpenChange: (open: boolean) => void; openerRef: RefObject<HTMLElement | null>
}) {
  useLocale()
  const effective = useEffectiveConfig()
  const available = effective.desk?.localGateway?.status === 'ready' && !effective.desk?.decoded?.values?.research?.gateway
  const context = JSON.stringify([available, effective.config.research.gateway])
  return <PersonalConnectionsContent key={context} open={open} onOpenChange={onOpenChange} openerRef={openerRef} available={available} />
}

function PersonalConnectionsContent({ open, onOpenChange, openerRef, available }: {
  open: boolean; onOpenChange: (open: boolean) => void; openerRef: RefObject<HTMLElement | null>; available: boolean
}) {
  const [provider, setProvider] = useState<ConnectionProvider | null>(null)
  const drive = useRef<HTMLButtonElement>(null), gmail = useRef<HTMLButtonElement>(null)
  return <>
    <Dialog open={open} onOpenChange={next => {
      // During a nested portal's mount/unmount, Escape can reach the outer
      // layer. One dismissal must leave the user in their connections list.
      if (!next && provider) setProvider(null)
      else onOpenChange(next)
    }} title={msg('My connections')} description={msg('Personal · This computer')} openerRef={openerRef}
      footer={<DialogActions><Button onClick={() => onOpenChange(false)}>{msg('Done')}</Button></DialogActions>}>
      <ConnectionRow available={available && open} provider="google-drive" opener={drive} onOpen={() => setProvider('google-drive')} />
      <ConnectionRow available={available && open} provider="gmail" opener={gmail} onOpen={() => setProvider('gmail')} />
    </Dialog>
    {provider && <GoogleConnectionDialog provider={provider} available={available} open={open} onOpenChange={() => setProvider(null)} openerRef={provider === 'gmail' ? gmail : drive} />}
  </>
}

function ConnectionRow({ available, provider, opener, onOpen }: {
  available: boolean; provider: ConnectionProvider; opener: RefObject<HTMLButtonElement | null>; onOpen: () => void
}) {
  const query = useDriveStatus(available, provider)
  const state = available && !query.isError ? query.data?.state : 'unavailable'
  return <SettingRow title={provider === 'gmail' ? msg('Gmail') : msg('Google Drive')}
    description={state === 'connected' ? query.data?.account?.email : state === undefined ? msg('Loading…') : state === 'unavailable' || state === 'blocked' ? msg('Unavailable') : msg('Not connected')}
    action={<Button ref={opener} onClick={onOpen} disabled={!state || state === 'unavailable' || state === 'blocked'}>{state === 'connected' ? msg('Manage') : msg('Connect')}</Button>} />
}
