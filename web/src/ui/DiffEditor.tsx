import {useEffect, useRef, useState} from 'react'
import {EditorState} from '@codemirror/state'
import {EditorView, lineNumbers} from '@codemirror/view'
import {syntaxHighlighting} from '@codemirror/language'
import {json} from '@codemirror/lang-json'
import {MergeView, unifiedMergeView,getChunks} from '@codemirror/merge'
import {codeTheme, codeColors} from '../files/codeTheme'
import {msg, useLocale} from '../i18n'
import styles from './DiffEditor.module.css'
export default function DiffEditor({before,after,split,unchanged,language='json',beforeLabel,afterLabel,ariaLabel}:{before:string;after:string;split:boolean;unchanged:boolean;language?:'json'|'text';beforeLabel?:string;afterLabel?:string;ariaLabel?:string}) {
  const host=useRef<HTMLDivElement>(null),locale=useLocale(),[coarse,setCoarse]=useState(false)
  useEffect(()=>{
    if(!host.current)return
    const extensions=[codeTheme,syntaxHighlighting(codeColors),...(language==='json'?[json()]:[]),lineNumbers(),EditorView.lineWrapping,EditorState.readOnly.of(true),EditorView.editable.of(false)]
    const access=(label:string)=>EditorView.contentAttributes.of({'aria-label':label,role:'region',tabindex:'0'})
    // Bound algorithm work while retaining the complete source in the virtualized editor.
    const diffConfig={scanLimit:500,timeout:100}
    const collapseUnchanged=unchanged?undefined:{margin:3,minSize:6}
    const view=split ? new MergeView({parent:host.current,a:{doc:before,extensions:[...extensions,access(beforeLabel??msg('Before'))]},b:{doc:after,extensions:[...extensions,access(afterLabel??msg('Proposed'))]},collapseUnchanged,diffConfig,highlightChanges:true,gutter:true})
      : new EditorView({parent:host.current,state:EditorState.create({doc:after,extensions:[...extensions,access(ariaLabel??msg('Proposed changes')),unifiedMergeView({original:before,mergeControls:false,collapseUnchanged,diffConfig,highlightChanges:true,gutter:true})]})})
    const chunks=view instanceof MergeView?view.chunks:getChunks(view.state)?.chunks
    setCoarse(chunks?.some(chunk=>!chunk.precise)??false)
    return ()=>view.destroy()
  },[before,after,split,unchanged,locale,language,beforeLabel,afterLabel,ariaLabel])
  return <>{coarse&&<p className={styles.notice} role="status">{msg('Change highlighting is approximate for this large comparison. Complete text remains available.')}</p>}<div ref={host} className={styles.editor} data-split={split||undefined}/></>
}
