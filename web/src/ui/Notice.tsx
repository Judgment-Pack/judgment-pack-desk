import type { ReactNode } from 'react'
import { msg } from '../i18n'
import { IconCheck, IconException, IconInfo } from '../shell/icons'
import styles from './Notice.module.css'

/** Persistent feedback at the affected action; diagnostics never lead the message. */
export function Notice({ children, title, tone = 'info', role, detail, detailLabel, actions, className }: {
  children?: ReactNode; title?: ReactNode; tone?: 'info' | 'success' | 'warning' | 'error'
  role?: 'status' | 'alert'; detail?: ReactNode; detailLabel?: string; actions?: ReactNode; className?: string
}) {
  return <div className={[styles.notice, className].filter(Boolean).join(' ')} data-tone={tone} role={role ?? (tone === 'error' ? 'alert' : 'status')}>
    <div className={styles.icon} aria-hidden="true">{tone === 'success' ? <IconCheck/> : tone === 'warning' || tone === 'error' ? <IconException/> : <IconInfo/>}</div>
    <div className={styles.content}>
      {title && <p className={styles.title}>{title}</p>}
      {children && <div className={styles.message}>{children}</div>}
      {detail !== undefined && detail !== '' && <details className={styles.details}>
        <summary>{detailLabel ?? msg('Technical details')}</summary>
        <div className={styles.detail}>{detail}</div>
      </details>}
      {actions && <div className={styles.actions}>{actions}</div>}
    </div>
  </div>
}
