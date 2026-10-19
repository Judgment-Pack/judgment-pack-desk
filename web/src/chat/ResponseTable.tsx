import {ChatAction} from './ChatActions'
import { useRef, useState, type ReactNode } from 'react'
import { msg, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import { Tooltip } from '../ui/Tooltip'
import { IconFocus } from '../shell/icons'
import { Dialog, DialogActions, DialogClose } from '../ui/Dialog'
import styles from './ResponseTable.module.css'

/** Keep wide model output inside a keyboard-scrollable surface, including in a pane. */
export function ResponseTable({ children }: { children: ReactNode }) {
  useLocale()
  const [open, setOpen] = useState(false)
  const opener = useRef<HTMLButtonElement>(null)
  const table = (expanded = false) => <div className={styles.scroll} data-expanded={expanded || undefined} role="region" aria-label={msg('Response table')} tabIndex={0}>
    <table>{children}</table>
  </div>
  return <div className={styles.root}>
    <div className={styles.toolbar}><span>{msg('Table')}</span><Tooltip content={msg('Show table')} disabled={open}><ChatAction variant="quiet" size="icon" ref={opener} type="button" aria-label={msg('Show table')} aria-haspopup="dialog" onClick={() => setOpen(true)}><IconFocus /></ChatAction></Tooltip></div>
    {table()}
    <Dialog open={open} onOpenChange={setOpen} title={msg('Response table')} size="wide" openerRef={opener}
      footer={<DialogActions><DialogClose asChild><Button>{msg('Close')}</Button></DialogClose></DialogActions>}>
      {table(true)}
    </Dialog>
  </div>
}
