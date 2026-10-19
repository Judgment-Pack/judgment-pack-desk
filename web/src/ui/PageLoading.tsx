import type {ReactNode} from 'react'
import {LoadingState} from './LoadingState'
import styles from './PageLoading.module.css'

/** Initial main-pane load. Keep page chrome outside; retain content during refresh. */
export function PageLoading({children}: {children?: ReactNode}) {
 return <div className={styles.root} data-page-loading><LoadingState>{children}</LoadingState></div>
}
