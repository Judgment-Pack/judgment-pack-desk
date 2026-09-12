import type { ReactNode } from 'react'
import styles from './PageLayout.module.css'

/** Full-width page chrome, independent of the reading measure below it. */
export function PageHeader({ title, context, actions, description, navigation, variant = 'context' }: {
  title: string
  context?: string
  actions?: ReactNode
  description?: ReactNode
  navigation?: ReactNode
  variant?: 'context' | 'title'
}) {
  return <header className={variant === 'title' ? styles.documentHeader : styles.header}>
    <div className={variant === 'title' ? styles.documentHeading : styles.heading}>
    <h1 className={styles.title}>{title}{context && <>
      <span className={styles.separator} aria-hidden="true">/</span>
      <span className={styles.context} title={context}>{context}</span>
    </>}</h1>
    {actions && <div className={styles.actions}>{actions}</div>}
    {description && <div className={styles.description}>{description}</div>}
    </div>
    {navigation}
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
