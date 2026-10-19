import type {ReactNode} from 'react'
import {msg, useLocale} from '../i18n'
import {RunStatus} from './RunStatus'
import styles from './LoadingState.module.css'

/** One loading treatment for pages, panes, dialogs and inline controls. */
export function LoadingState({children, inline = false, inset = false, className, id}: {
 children?: ReactNode; inline?: boolean; inset?: boolean; className?: string; id?: string
}) {
 useLocale()
 return <RunStatus running inline={inline} id={id} className={[inset && styles.inset, inline && styles.inline, className].filter(Boolean).join(' ')}>{children ?? msg('Loading…')}</RunStatus>
}
