import type { ComponentProps, ReactNode } from 'react'
import styles from './StatusTrigger.module.css'

/** Compact status details: the same focus, spacing and truncation in every cue. */
export function StatusTrigger({ icon, children, ...props }: ComponentProps<'button'> & { icon: ReactNode }) {
  return <button {...props} type="button" className={styles.trigger}>
    {icon}<span>{children}</span>
  </button>
}
