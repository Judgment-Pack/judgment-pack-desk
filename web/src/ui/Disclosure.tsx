import type { ComponentPropsWithoutRef, ReactNode } from 'react'
import { IconChevronRight } from '../shell/icons'
import styles from './Disclosure.module.css'

/** In-place expansion with a native keyboard/touch target and one shared visual. */
export function Disclosure({ title, children, className, indicator, size = 'default', ...props }: {
  title: ReactNode
  size?: 'default' | 'small'
  indicator?: ReactNode
  children: ReactNode
} & Omit<ComponentPropsWithoutRef<'details'>, 'title'>) {
  return <details {...props} className={[styles.root, size === 'small' && styles.small, className].filter(Boolean).join(' ')}>
    <summary className={styles.summary}>{indicator === undefined ? <IconChevronRight /> : <span className={styles.indicator}>{indicator}</span>}<span>{title}</span></summary>
    <div className={styles.content}>{children}</div>
  </details>
}
