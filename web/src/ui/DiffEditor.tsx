import {useEffect, useRef} from 'react'
import {EditorState} from '@codemirror/state'
import {EditorView, lineNumbers} from '@codemirror/view'
import {syntaxHighlighting} from '@codemirror/language'
import {json} from '@codemirror/lang-json'
import {MergeView, unifiedMergeView} from '@codemirror/merge'
import {codeTheme, codeColors} from '../files/codeTheme'
import {msg, useLocale} from '../i18n'
import styles from './DiffEditor.module.css'
export default function DiffEditor({before,after,split,unchanged}:{before:string;after:string;split:boolean;unchanged:boolean}) {
  const host=useRef<HTMLDivElement>(null),locale=useLocale()
  useEffect(()=>{
    if(!host.current)return
    const extensions=[codeTheme,syntaxHighlighting(codeColors),json(),lineNumbers(),EditorView.lineWrapping,EditorState.readOnly.of(true),EditorView.editable.of(false)]
    const access=(label:string)=>EditorView.contentAttributes.of({'aria-label':label,role:'region',tabindex:'0'})
    const collapseUnchanged=unchanged?undefined:{margin:3,minSize:6}
    const view=split ? new MergeView({parent:host.current,a:{doc:before,extensions:[...extensions,access(msg('Before'))]},b:{doc:after,extensions:[...extensions,access(msg('Proposed'))]},collapseUnchanged,highlightChanges:true,gutter:true})
      : new EditorView({parent:host.current,state:EditorState.create({doc:after,extensions:[...extensions,access(msg('Proposed changes')),unifiedMergeView({original:before,mergeControls:false,collapseUnchanged,highlightChanges:true,gutter:true})]})})
    return ()=>view.destroy()
  },[before,after,split,unchanged,locale])
  return <div ref={host} className={styles.editor} data-split={split||undefined}/>
}
