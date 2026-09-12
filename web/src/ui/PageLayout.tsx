import type { ReactNode } from 'react'
import { OverflowTooltip } from './Tooltip'
import styles from './PageLayout.module.css'

/** Full-width page chrome, independent of the reading measure below it. */
export function PageHeader({ title, context, meta, actions, description, navigation, variant = 'context' }: {
  title: string
  context?: string
  meta?: ReactNode
  actions?: ReactNode
  description?: ReactNode
  navigation?: ReactNode
  variant?: 'context' | 'title'
}) {
  // Route chrome is not a second banner landmark; the shell owns that role.
  return <header role="presentation" className={variant === 'title' ? styles.documentHeader : styles.header}>
    <div className={variant === 'title' ? styles.documentHeading : styles.heading}>
    <h1 className={styles.title}>{title}{context && <>
      <span className={styles.separator} aria-hidden="true">/</span>
      <OverflowTooltip><span className={styles.context}>{context}</span></OverflowTooltip>
    </>}{meta !== undefined && <span className={styles.meta}>{meta}</span>}</h1>
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
