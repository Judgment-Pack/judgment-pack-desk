import {LoadingState} from './LoadingState'
import type { ReactNode } from 'react'
import { IconCheck, IconException } from '../shell/icons'
import styles from './SettingsFeedback.module.css'

/** Compact action feedback. Keep recovery controls outside the live region. */
export function SettingsFeedback({ children, tone = 'neutral', actions, pending = false }: {
  children?: ReactNode
  tone?: 'neutral' | 'success' | 'error' | 'warning'
  actions?: ReactNode
  pending?: boolean
}) {
  return <div className={styles.feedback} data-tone={tone}>
    <div className={styles.message}>
      {children && tone !== 'neutral' && <span className={styles.icon} aria-hidden="true">{tone === 'success' ? <IconCheck /> : <IconException />}</span>}
      {pending && tone === 'neutral' ? <LoadingState>{children}</LoadingState> : <span role={tone === 'error' ? 'alert' : 'status'} aria-atomic="true">{children}</span>}
    </div>
    {actions && <div className={styles.recovery}>{actions}</div>}
  </div>
}

/** Feedback before a right-aligned group of settings actions, wrapping on narrow panes. */
export function SettingsActions({ children, feedback, hint }: {
  children: ReactNode
  feedback?: ReactNode
  hint?: ReactNode
}) {
  return <div className={styles.footer}>
    {hint && <div className={styles.hint}>{hint}</div>}
    <div className={styles.row}>
      {feedback && <div className={styles.result}>{feedback}</div>}
      <div className={styles.actions}>{children}</div>
    </div>
  </div>
}
