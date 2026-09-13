import type { ReactNode } from 'react'
import { Button } from './Button'
import { Popover } from './Popover'
import styles from './InfoHelp.module.css'

/** Optional explanations with a real click/tap target and managed keyboard focus. */
export function InfoHelp({ title, children }: { title: string; children: ReactNode }) {
  return <Popover title={title} trigger={<Button variant="quiet" className={styles.trigger} aria-label={`About ${title.toLowerCase()}`}>
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true">
      <circle cx="12" cy="12" r="9" /><path d="M12 11v6M12 7v1" />
    </svg>
  </Button>}><p className={styles.description}>{children}</p></Popover>
}
