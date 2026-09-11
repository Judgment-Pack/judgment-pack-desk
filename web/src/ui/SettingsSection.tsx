import { useId, type ReactNode } from 'react'
import styles from './SettingsSection.module.css'

/** A settings group with its own heading, fields, and optional action footer. */
export function SettingsSection({
  title,
  description,
  children,
  footer,
  level = 3
}: {
  title: string
  description?: ReactNode
  children: ReactNode
  footer?: ReactNode
  level?: 2 | 3
}) {
  const titleId = useId()
  const Heading = level === 2 ? 'h2' : 'h3'
  return (
    <section className={styles.section} aria-labelledby={titleId}>
      <header className={styles.head}>
        <Heading id={titleId}>{title}</Heading>
        {description !== undefined && <p>{description}</p>}
      </header>
      <div className={styles.content}>{children}</div>
      {footer !== undefined && <footer className={styles.footer}>{footer}</footer>}
    </section>
  )
}
