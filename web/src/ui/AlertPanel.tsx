import type { ReactNode } from 'react'
import { Notice } from './Notice'
import styles from './AlertPanel.module.css'

/** A failed action, its recovery controls, and optional technical details. */
export function AlertPanel({ heading, children, detailLabel, detail, actions }: {
  heading: string; children: ReactNode; detailLabel?: string; detail?: ReactNode; actions?: ReactNode
}) {
  return <Notice className={styles.root} tone="error" title={heading} detailLabel={detailLabel} detail={detail} actions={actions}>{children}</Notice>
}
