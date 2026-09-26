import type { ReactNode } from 'react'
import styles from './RunStatus.module.css'

/** A single readable live region; motion never changes its accessible text. */
export function RunStatus({
  children,
  running = false,
  error = false,
  className,
}: {
  children: ReactNode
  running?: boolean
  error?: boolean
  className?: string
}) {
  return (
    <div
      className={[styles.status, className].filter(Boolean).join(' ')}
      data-running={(running && !error) || undefined}
      role={error ? 'alert' : 'status'}
      aria-atomic="true"
    >
      <span className={styles.label}>{children}</span>
    </div>
  )
}
