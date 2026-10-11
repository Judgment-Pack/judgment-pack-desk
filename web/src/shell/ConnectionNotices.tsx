import { msg, systemMessage, useLocale } from '../i18n'
import { Button, ButtonLink } from '../ui/Button'
import { Notice } from '../ui/Notice'
import { Popover } from '../ui/Popover'
import { useMcp, connectionSays } from '../mcp/McpProvider'
import styles from './ConnectionNotices.module.css'

/** Connection recovery belongs to the permanent status strip, not the document header. */
export function ConnectionStatus() {
  useLocale()
  const { status, known } = useMcp()
  if (status === 'ready' && known) return <ButtonLink variant="inline" className="desk-strip-connection" to="/help" aria-label={`${msg('Help & About')}: ${connectionSays(status)}`}>{connectionSays(status)}</ButtonLink>
  const label = status === 'ready' ? msg('Limited connection') : connectionSays(status)
  return <div className={styles.status}>
    <Popover title={msg('Connection status')} dismissible trigger={<button type="button" className={styles.trigger} aria-label={msg('Connection status: {{status}}', {status: label})}>
      <span className={styles.dot} data-pending={status === 'connecting' || status === 'reconnecting'} aria-hidden="true"/>
      <span role="status" aria-live="polite">{label}</span>
    </button>}>
      <ConnectionNotices/>
    </Popover>
  </div>
}

/** One sentence, one recovery action; attempts and server diagnostics are secondary. */
export function ConnectionNotices() {
  useLocale()
  const { status, error, attempt, retryNow, known, capabilitiesError } = useMcp()
  const limited = status === 'ready' && !known
  const connecting = status === 'connecting' || status === 'reconnecting'
  const detail = limited ? capabilitiesError : error
  return <Notice tone={connecting ? 'info' : 'warning'}
    detail={detail || attempt > 0 ? <>{detail && <p>{systemMessage(detail.message)}</p>}{attempt > 0 && <p>{msg('Connection attempts: {{count}}', {count: attempt})}</p>}</> : undefined}
    actions={<><Button onClick={retryNow}>{msg('Retry connection')}</Button><ButtonLink variant="quiet" to="/help">{msg('Help & About')}</ButtonLink></>}>
    {limited ? msg('Some features are unavailable because Desk could not load runtime capabilities.')
      : connecting ? msg('Reconnecting to Desk…') : msg('Desk is not connected. Check that it is running on this computer.')}
  </Notice>
}

/** Preserve a connected workspace through a temporary outage. */
export function useBlockingError(): Error | null {
  const { status, error, everConnected } = useMcp()
  const blocked = status === 'failed' || (status === 'reconnecting' && !everConnected)
  return blocked && error ? error : null
}

export function BlockedNotice({ error }: { error: Error }) {
  useLocale()
  const { status, retryNow } = useMcp()
  return <div className={styles.blocked}><Notice tone="error" title={msg('Not connected to the runtime')} detail={status === 'reconnecting' ? systemMessage(error.message) : undefined}
    actions={status === 'reconnecting' ? <Button onClick={retryNow}>{msg('Retry connection')}</Button> : undefined}>
    {status === 'reconnecting' ? msg('Desk is reconnecting automatically. Check that it is running on this computer.') : systemMessage(error.message)}
  </Notice></div>
}
