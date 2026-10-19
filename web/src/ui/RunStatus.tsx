import type { ReactNode } from 'react'
import styles from './RunStatus.module.css'
import { Spinner } from './Spinner'

/** A single readable live region; motion never changes its accessible text. */
export function RunStatus({
  children,
  id,
  running = false,
  error = false,
  className,
  inline = false,
}: {
  children: ReactNode
  id?: string
  running?: boolean
  error?: boolean
  className?: string
  inline?: boolean
}) {
  const Element = inline ? 'span' : 'div'
  return (
    <Element
      id={id}
      className={[styles.status, inline && styles.inline, className].filter(Boolean).join(' ')}
      data-running={(running && !error) || undefined}
      role={error ? 'alert' : 'status'}
      aria-atomic="true"
    >
      {running && !error && <Spinner/>}
      <span className={styles.label}>{children}</span>
    </Element>
  )
}
