import { useId, type ReactNode } from 'react'
import styles from './SettingsSection.module.css'

/** A settings group with its own heading, fields, and optional action footer. */
export function SettingsSection({
  title,
  id,
  description,
  children,
  footer,
  variant = 'card',
  level = 3
}: {
  title: string
  id?: string
  description?: ReactNode
  children: ReactNode
  footer?: ReactNode
  level?: 2 | 3
  variant?: 'card' | 'plain' | 'standalone'
}) {
  const generatedId = useId()
  const titleId = id ? `${id}-title` : generatedId
  const Heading = level === 2 ? 'h2' : 'h3'
  return (
    <section className={styles.section} data-variant={variant} aria-labelledby={titleId}>
      <header className={styles.head}>
        <Heading id={titleId}>{title}</Heading>
        {description !== undefined && <p>{description}</p>}
      </header>
      <div className={styles.content}>{children}</div>
      {footer !== undefined && <footer className={styles.footer}>{footer}</footer>}
    </section>
  )
}

/** One owner for the spacing and separators between settings groups. */
export function SettingsStack({children}:{children:ReactNode}) {
  return <div className={styles.stack}>{children}</div>
}
