import { IconInfo } from '../shell/icons'
import { msg, useLocale } from '../i18n'
import type { ReactNode } from 'react'
import { Button } from './Button'
import { Popover } from './Popover'
import styles from './InfoHelp.module.css'

/** Optional explanations with a real click/tap target and managed keyboard focus. */
export function InfoHelp({ title, children }: { title: string; children: ReactNode }) {
  useLocale()
  return <Popover title={title} trigger={<Button size="icon" variant="quiet" className={styles.trigger} aria-label={msg("About {{value0}}", { value0: title.toLowerCase() })}>
    <IconInfo />
  </Button>}><p className={styles.description}>{children}</p></Popover>
}
