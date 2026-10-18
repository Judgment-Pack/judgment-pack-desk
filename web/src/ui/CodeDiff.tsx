import {lazy, Suspense, useRef, useState} from 'react'
import {msg, useLocale} from '../i18n'
import {Button} from './Button'
import {Dialog, DialogActions, DialogClose} from './Dialog'
import {LoadingState} from './LoadingState'
import {Tooltip} from './Tooltip'
import {IconFocus} from '../shell/icons'
import styles from './CodeDiff.module.css'
const DiffEditor = lazy(() => import('./DiffEditor'))
export interface CodeDiffProps {before:string;after:string;beforeLabel?:string;afterLabel?:string;title?:string;ariaLabel?:string;language?:'json'|'text'}
/** Read-only comparison. Formatting is presentation; save always uses retained bytes. */
export function CodeDiff({before,after,beforeLabel,afterLabel,title,ariaLabel,language='json'}:CodeDiffProps) {
  useLocale()
  const [unchanged,setUnchanged]=useState(false),[open,setOpen]=useState(false),[split,setSplit]=useState(true),opener=useRef<HTMLButtonElement>(null)
  const left=beforeLabel??msg('Before'),right=afterLabel??msg('Proposed'),label=ariaLabel??msg('Proposed changes')
  const editor=(sideBySide:boolean)=><Suspense fallback={<LoadingState>{msg('Loading changes…')}</LoadingState>}><DiffEditor before={before} after={after} split={sideBySide} unchanged={unchanged} language={language} beforeLabel={left} afterLabel={right} ariaLabel={label}/></Suspense>
  return <div className={styles.root}>
    <div className={styles.toolbar}><span className={styles.legend}><span className={styles.removed}>− {left}</span> / <span className={styles.added}>+ {right}</span></span><div className={styles.actions}><Button size="small" variant="quiet" aria-pressed={unchanged} onClick={()=>setUnchanged(!unchanged)}>{msg('Show unchanged')}</Button><Tooltip content={msg('Expand diff')}><Button ref={opener} size="small-icon" variant="quiet" aria-label={msg('Expand diff')} onClick={()=>setOpen(true)}><IconFocus/></Button></Tooltip></div></div>
    {editor(false)}
    <Dialog open={open} onOpenChange={setOpen} title={title??msg('Review proposed changes')} size="wide" openerRef={opener}
      footer={<DialogActions><DialogClose asChild><Button>{msg('Close')}</Button></DialogClose></DialogActions>}>
      <div className={styles.toolbar}><span>{beforeLabel===undefined&&afterLabel===undefined?msg('Before → Proposed'):left+' → '+right}</span><Button size="small" variant="quiet" aria-pressed={split} onClick={()=>setSplit(!split)}>{msg('Side by side')}</Button></div>
      {open && editor(split)}
    </Dialog>
  </div>
}
