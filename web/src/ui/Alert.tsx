import type { ReactNode } from 'react'
import { Notice } from './Notice'
import styles from './Alert.module.css'

/** Form failures share the compact notice; technical reasons stay available on demand. */
export function Alert({ children, reason }: { children: ReactNode; reason?: ReactNode }) {
  return <Notice tone="error" className={styles.alert} detail={reason}>{children}</Notice>
}
