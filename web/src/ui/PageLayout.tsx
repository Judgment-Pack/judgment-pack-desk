import { Link } from 'react-router-dom'
import { VisuallyHidden } from 'radix-ui'
import type { ReactNode } from 'react'
import { OverflowTooltip } from './Tooltip'
import styles from './PageLayout.module.css'

/** Full-width page chrome, independent of the reading measure below it. */
export function PageHeader({ title, titleHref, context, leading, meta, actions, description, navigation, variant = 'context' }: {
  leading?: ReactNode
  title: string
  titleHref?: string
  context?: string
  meta?: ReactNode
  actions?: ReactNode
  description?: ReactNode
  navigation?: ReactNode
  variant?: 'context' | 'title' | 'collection'
}) {
  if (variant === 'collection') return <header role="presentation" data-page-header className={styles.collectionHeader}>
    <VisuallyHidden.Root asChild><h1>{title}</h1></VisuallyHidden.Root>
    {navigation}
    {actions && <div className={styles.actions} data-page-actions>{actions}</div>}
  </header>
  // Route chrome is not a second banner landmark; the shell owns that role.
  return <header role="presentation" data-page-header className={variant === 'title' ? styles.documentHeader : styles.header}>
    <div className={variant === 'title' ? styles.documentHeading : styles.heading}>
    {leading}
    <h1 className={styles.title}>{titleHref ? <Link className={styles.titleLink} to={titleHref}>{title}</Link> : title}{context && <>
      <span className={styles.separator} aria-hidden="true">/</span>
      <OverflowTooltip><span className={styles.context} data-page-context>{context}</span></OverflowTooltip>
    </>}{meta !== undefined && <span className={styles.meta}>{meta}</span>}</h1>
    {actions && <div className={styles.actions} data-page-actions>{actions}</div>}
    {description && <div className={styles.description}>{description}</div>}
    </div>
    {navigation}
  </header>
}

/** A fixed gutter with a bounded, left-aligned reading area. */
export function PageBody({ children, width = 'form', fill = false }: {
  children: ReactNode
  width?: 'form' | 'wide' | 'full'
  fill?: boolean
}) {
  return <div className={styles.body} data-fill={fill || undefined} data-page-scroll>
    <div className={styles.content} data-width={width}>{children}</div>
  </div>
}
