import {lazy, Suspense, useRef, useState} from 'react'
import {msg, useLocale} from '../i18n'
import {Button} from './Button'
import {Dialog, DialogActions, DialogClose} from './Dialog'
import {LoadingState} from './LoadingState'
import {Tooltip} from './Tooltip'
import {IconFocus} from '../shell/icons'
import styles from './CodeDiff.module.css'
const DiffEditor = lazy(() => import('./DiffEditor'))
/** Read-only comparison. Formatting is presentation; save always uses the retained proposal. */
export function CodeDiff({before, after}: {before:string;after:string}) {
  useLocale()
  const [unchanged,setUnchanged]=useState(false),[open,setOpen]=useState(false),[split,setSplit]=useState(true),opener=useRef<HTMLButtonElement>(null)
  const editor=(sideBySide:boolean)=><Suspense fallback={<LoadingState>{msg('Loading changes…')}</LoadingState>}><DiffEditor before={before} after={after} split={sideBySide} unchanged={unchanged}/></Suspense>
  return <div className={styles.root}>
    <div className={styles.toolbar}><span className={styles.legend}><span className={styles.removed}>− {msg('Before')}</span> / <span className={styles.added}>+ {msg('Proposed')}</span></span><div className={styles.actions}><Button size="small" variant="quiet" aria-pressed={unchanged} onClick={()=>setUnchanged(!unchanged)}>{msg('Show unchanged')}</Button><Tooltip content={msg('Expand diff')}><Button ref={opener} size="small-icon" variant="quiet" aria-label={msg('Expand diff')} onClick={()=>setOpen(true)}><IconFocus/></Button></Tooltip></div></div>
    {editor(false)}
    <Dialog open={open} onOpenChange={setOpen} title={msg('Review proposed changes')} size="wide" openerRef={opener}
      footer={<DialogActions><DialogClose asChild><Button>{msg('Close')}</Button></DialogClose></DialogActions>}>
      <div className={styles.toolbar}><span>{msg('Before → Proposed')}</span><Button size="small" variant="quiet" aria-pressed={split} onClick={()=>setSplit(!split)}>{msg('Side by side')}</Button></div>
      {open && editor(split)}
    </Dialog>
  </div>
}
