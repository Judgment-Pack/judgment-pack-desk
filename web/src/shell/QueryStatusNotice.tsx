import type { ReactNode } from 'react'
import { msg, systemMessage } from '../i18n'
import { Button } from '../ui/Button'
import { DeskStatusNotice } from './DeskNotices'

/** Background reads use the strip. Retry only refreshes the read; it never replays work. */
export function QueryStatusNotice({ id, label, context, error, retrying = false, retryLabel, onRetry, children }: {
  id: string
  label: string
  context?: string
  error: unknown
  retrying?: boolean
  retryLabel?: string
  onRetry: () => unknown
  children?: ReactNode
}) {
  if (!error) return null
  const detail = error instanceof Error ? error.message : typeof error === 'string' ? error : undefined
  return <DeskStatusNotice id={id} label={label} title={context ? `${label} · ${context}` : label} tone="warning"
    detail={detail ? systemMessage(detail) : undefined}
    actions={<Button disabled={retrying} onClick={() => { void onRetry() }}>{retrying ? msg('Retrying…') : retryLabel ?? msg('Retry')}</Button>}>
    {children ?? label}
  </DeskStatusNotice>
}
