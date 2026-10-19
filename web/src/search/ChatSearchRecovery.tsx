import {LoadingState} from '../ui/LoadingState'
import { useRef, useState } from 'react'
import type { Chat } from '../chat/store'
import type { WorkItem } from '../chat/responseHistory'
import { msg, systemMessage, useLocale } from '../i18n'
import { Button, ButtonLink } from '../ui/Button'
import { Notice } from '../ui/Notice'
import { useSearchConnections, useSearchPreference } from './connections'
import { searchFailureMessage } from './failures'
import { ConfigureSearch } from './ConfigureSearch'

/** Only host-recorded tool failures can offer recovery. Assistant prose, page
 * contents and a merely unconfigured (but unused) capability cannot show it. */
export function ChatSearchRecovery({ chat, failure, disabled, onChange, onContinue }: {
 chat: Chat; failure: WorkItem; disabled: boolean; onChange: (patch: Partial<Chat>) => void; onContinue: (query: string) => void
}) {
 useLocale()
 const connections = useSearchConnections(), preference = useSearchPreference()
 const [open, setOpen] = useState(false), [configured, setConfigured] = useState(false)
 const opener = useRef<HTMLElement | null>(null)
 if (!failure.failure || !failure.search || !preference.data || preference.isError || (chat.researchMode ?? preference.data.value.mode) !== 'auto') return null
 const selected = chat.searchConnection ?? preference.data.value.connection ?? undefined
 const connection = connections.available && connections.data?.connections.find(c => c.id === selected)
 const ready = Boolean(connection && !connections.isError && !connections.loading)
 const setup = ['setup-required', 'connect-required'].includes(failure.failure)
 const repair = failure.failure === 'search-invalid-request' || failure.failure === 'credentials-required' || failure.failure === 'search-budget-exhausted'
 const retry = ['source-changed', 'search-temporary', 'search-timeout', 'search-not-grounded', 'provider-unavailable', 'rate-limited', 'search-storage-failed'].includes(failure.failure)
 const canContinue = ready && (configured || setup || retry)
 const configure = (element: HTMLElement) => { opener.current = element; setOpen(true) }
 const message = connections.loading ? msg('Checking search connections…') : !connections.available ? msg('Web search requires an updated local gateway.') : ready && (configured || setup) ? msg('Web search is ready. Continue when you’re ready.')
  : setup && !ready ? msg('Set up web search to find sources online.')
  : systemMessage(searchFailureMessage(failure.failure)!)
 return <>
  {connections.loading && !connections.isError ? <LoadingState>{message}</LoadingState> : <Notice actions={connections.loading ? undefined : connections.isError ? <Button variant="inline" disabled={disabled} onClick={() => void connections.refetch()}>{msg('Retry')}</Button>
   : !connections.available || failure.failure === 'gateway-unavailable' ? <ButtonLink variant="inline" to="/admin#gateway">{msg('Manage gateway')}</ButtonLink>
   : canContinue ? <Button variant="inline" disabled={disabled} onClick={() => onContinue(failure.search!.query)}>{retry && !configured ? msg('Retry search') : msg('Continue request')}</Button>
   : setup || repair ? <Button variant="inline" disabled={disabled} onClick={event => configure(event.currentTarget)}>{repair ? msg('Check connection') : msg('Set up web search')}</Button> : undefined}>
   {connections.isError ? msg('Search connections could not be loaded.') : message}
  </Notice>}
  <ConfigureSearch open={open} onClose={() => setOpen(false)} openerRef={opener} selected={selected} repair={repair} onSelect={id => { onChange({ searchConnection: id }); setConfigured(true); setOpen(false) }}/>
 </>
}
