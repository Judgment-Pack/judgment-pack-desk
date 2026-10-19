import { msg, systemMessage, useLocale } from '../i18n'
import { Button, ButtonLink } from '../ui/Button'
import { Notice } from '../ui/Notice'
import { Popover } from '../ui/Popover'
import { useMcp, connectionSays } from '../mcp/McpProvider'
import { StatusTrigger } from './StatusTrigger'
import styles from './ConnectionNotices.module.css'

/** Connection recovery belongs to the permanent status strip, not the document header. */
export function ConnectionStatus() {
  useLocale()
  const { status, known } = useMcp()
  if (status === 'ready' && known) return <ButtonLink data-connection-status variant="inline" className="desk-strip-connection" to="/help" aria-label={`${msg('Help & About')}: ${connectionSays(status)}`}>{connectionSays(status)}</ButtonLink>
  const label = status === 'ready' ? msg('Limited connection') : connectionSays(status)
  return <div className={styles.status}>
    <Popover variant="status" title={msg('Connection status')} dismissible trigger={<StatusTrigger data-tone={status === 'connecting' || status === 'reconnecting' ? 'info' : 'warning'} data-connection-status aria-label={msg('Connection status: {{status}}', {status: label})}
      icon={<span className={styles.dot} data-pending={status === 'connecting' || status === 'reconnecting'} aria-hidden="true"/>}>
      <span role="status" aria-live="polite">{label}</span>
    </StatusTrigger>}>
      <div className={styles.details}><ConnectionNotices/></div>
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
  return <Notice className={styles.notice} tone={connecting ? 'info' : 'warning'}
    detail={detail || attempt > 0 ? <>{detail && <p>{systemMessage(detail.message)}</p>}{attempt > 0 && <p>{msg('Connection attempts: {{count}}', {count: attempt})}</p>}</> : undefined}
    actions={<><Button onClick={retryNow}>{msg('Retry connection')}</Button><ButtonLink variant="quiet" to="/help">{msg('Help & About')}</ButtonLink></>}>
    {limited ? msg('Some features are unavailable because Desk could not load runtime capabilities.')
      : connecting ? msg('Reconnecting to Desk…') : msg('Desk is not connected. Check that it is running on this computer.')}
  </Notice>
}
