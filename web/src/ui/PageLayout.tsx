import type { ReactNode } from 'react'
import styles from './PageLayout.module.css'

/** Full-width page chrome, independent of the reading measure below it. */
export function PageHeader({ title, context, actions }: {
  title: string
  context?: string
  actions?: ReactNode
}) {
  return <header className={styles.header}>
    <h1 className={styles.title}>{title}{context && <>
      <span className={styles.separator} aria-hidden="true">/</span>
      <span className={styles.context} title={context}>{context}</span>
    </>}</h1>
    {actions && <div className={styles.actions}>{actions}</div>}
  </header>
}

/** A fixed gutter with a bounded, left-aligned reading area. */
export function PageBody({ children, width = 'form' }: {
  children: ReactNode
  width?: 'form' | 'wide' | 'full'
}) {
  return <div className={styles.body}>
    <div className={styles.content} data-width={width}>{children}</div>
  </div>
}
