import type { ReactNode } from 'react'
import styles from './SettingRow.module.css'

/** A quiet settings summary with a separate, explicitly labelled action. */
export function SettingRow({ title, description, status, action }: {
  title: string
  description: ReactNode
  status?: ReactNode
  action?: ReactNode
}) {
  return <div className={styles.row}>
    <div className={styles.summary}><h3>{title}</h3><p>{description}</p>{status && <p>{status}</p>}</div>
    {action && <div className={styles.action}>{action}</div>}
  </div>
}
