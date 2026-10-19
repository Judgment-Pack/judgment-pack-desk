import {LoadingState} from '../ui/LoadingState'
import type {ReactNode} from 'react'
import {msg, useLocale} from '../i18n'
import {RunStatus} from '../ui/RunStatus'
import styles from './ChatPlaceholder.module.css'

/** History errors and Retry live in the shell status strip; never leave a failed read spinning. */
export function ChatPlaceholder({loading = false, error = false, children}: {loading?: boolean; error?: boolean; children?: ReactNode}) {
 useLocale()
 return <div className={styles.placeholder}>
  {loading && !error ? <LoadingState>{msg('Loading chats…')}</LoadingState> : <RunStatus>{error ? msg('Chat history unavailable') : children}</RunStatus>}
 </div>
}
