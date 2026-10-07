import { useEffect, useLayoutEffect, useImperativeHandle, useRef, type Ref } from 'react'
import { Annotation, Compartment, EditorState } from '@codemirror/state'
import { EditorView, keymap, lineNumbers, highlightActiveLineGutter, drawSelection, highlightActiveLine } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
import { bracketMatching, codeFolding, foldGutter, foldKeymap, indentOnInput, syntaxHighlighting, HighlightStyle } from '@codemirror/language'
import { json, jsonParseLinter } from '@codemirror/lang-json'
import { searchKeymap, search, openSearchPanel, highlightSelectionMatches } from '@codemirror/search'
import { linter, lintGutter } from '@codemirror/lint'
import { tags } from '@lezer/highlight'
import { msg, useLocale } from '../i18n'
import styles from './CodeEditor.module.css'
import { glyphDOM } from '../shell/glyph'
import { editorSearch } from './editorSearch'
import { DOMTooltips } from '../ui/Tooltip'

export interface CodeEditorHandle { find:()=>void; focus:()=>void }
export interface CodeEditorProps { ref?:Ref<CodeEditorHandle>; id:string; path:string; value:string; wrap:boolean; readOnly?:boolean; describedBy?:string; onChange:(value:string)=>void; onFormat:()=>void; onSave:()=>void }
const external=Annotation.define<boolean>()
const colors=HighlightStyle.define([
 {tag:tags.propertyName,color:'var(--decision-cyan)'},
 {tag:tags.string,color:'var(--decision-green)'},
 {tag:[tags.number,tags.bool,tags.null],color:'var(--decision-violet)'},
 {tag:tags.punctuation,color:'var(--ink-soft)'}
])
const theme=EditorView.theme({
 '&':{height:'100%',backgroundColor:'var(--bg)',color:'var(--ink)',fontSize:'var(--text-sm)'},
 '&.cm-focused':{outline:'none'},
 '.cm-scroller':{overflow:'auto',fontFamily:'ui-monospace, SFMono-Regular, Consolas, monospace',lineHeight:'1.65'},
 '.cm-content':{padding:'12px 0',caretColor:'var(--ink)'},
 '.cm-line':{padding:'0 12px'},
 '.cm-gutters':{backgroundColor:'var(--bg)',color:'var(--ink-faint)',borderRight:'1px solid var(--border)'},
 '.cm-activeLineGutter, .cm-activeLine':{backgroundColor:'var(--accent-soft)'},
 '.cm-cursor':{borderLeftColor:'var(--ink)'},
 '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection':{backgroundColor:'var(--accent-soft)'},
 '.cm-panels':{backgroundColor:'var(--surface)',color:'var(--ink)',borderColor:'var(--border)'},
 '.cm-textfield, .cm-button':{background:'var(--surface-raised)',color:'var(--ink)',border:'1px solid var(--border)',borderRadius:'var(--radius-sm)'},
 '.cm-tooltip':{backgroundColor:'var(--surface-raised)',color:'var(--ink)',border:'1px solid var(--border)'},
 '&.cm-light .cm-searchMatch, &.cm-dark .cm-searchMatch':{backgroundColor:'var(--accent-soft)'},
 '&.cm-light .cm-searchMatch-selected, &.cm-dark .cm-searchMatch-selected':{backgroundColor:'var(--accent-soft)',outline:'1px solid var(--accent)'},
 '.cm-diagnostic-error':{borderLeftColor:'var(--danger)'},
 '.cm-foldPlaceholder':{background:'var(--surface-raised)',color:'var(--ink-soft)',borderColor:'var(--border)'}
})
/** A view of the same byte buffer; validation is advisory and never gates Save. */
export default function CodeEditor(props:CodeEditorProps){
 const locale=useLocale()
 const host=useRef<HTMLDivElement>(null),view=useRef<EditorView>(null),latest=useRef(props)
 latest.current=props
 const access=useRef(new Compartment()),wrapping=useRef(new Compartment()),description=useRef(new Compartment()),lineEnding=useRef(new Compartment())
 useImperativeHandle(props.ref,()=>({find:()=>{if(view.current){openSearchPanel(view.current)}},focus:()=>view.current?.focus()}),[])
 useLayoutEffect(()=>{
  if(!host.current)return
  const jsonFile=/\.json$/i.test(props.path),jsonLines=/\.jsonl$/i.test(props.path)
  const editor=new EditorView({parent:host.current,state:EditorState.create({doc:props.value,extensions:[
   access.current.of([EditorState.readOnly.of(Boolean(props.readOnly)),EditorView.editable.of(!props.readOnly)]),
   lineEnding.current.of(EditorState.lineSeparator.of(props.value.includes('\r\n')?'\r\n':'\n')),
   lineNumbers(),highlightActiveLineGutter(),history(),drawSelection(),highlightActiveLine(),indentOnInput(),bracketMatching(),foldGutter({markerDOM:open=>{
    const marker=document.createElement('span')
    marker.className=styles.foldMarker
    marker.dataset.tooltip=open?msg('Fold line'):msg('Unfold line')
    marker.dataset.fold=open?'open':'closed'
    marker.append(glyphDOM(open?'down':'right'))
    return marker
   }}),codeFolding({placeholderDOM:(_view,unfold)=>{
    const marker=document.createElement('span')
    marker.className='cm-foldPlaceholder'
    marker.textContent='…'
    marker.dataset.tooltip=msg('Unfold line')
    marker.setAttribute('aria-label',msg('Unfold line'))
    marker.onclick=unfold
    return marker
   }}),search({top:true,createPanel:editorSearch}),highlightSelectionMatches(),
   syntaxHighlighting(colors),theme,
   ...(jsonFile||jsonLines?[json()]:[]),...(jsonFile?[linter(jsonParseLinter(),{delay:600}),lintGutter()]:[]),
   wrapping.current.of(props.wrap?EditorView.lineWrapping:[]),
   description.current.of(EditorView.contentAttributes.of({'aria-label':msg('File contents'),tabindex:'0','aria-readonly':String(Boolean(props.readOnly)),id:props.id,...(props.describedBy?{'aria-describedby':props.describedBy}:{})})),
   keymap.of([{key:'Mod-s',run:()=>{latest.current.onSave();return true}},{key:'Alt-Shift-f',run:()=>{latest.current.onFormat();return true}},...searchKeymap,...defaultKeymap,...historyKeymap,...foldKeymap,indentWithTab]),
   EditorView.updateListener.of(update=>{if(update.docChanged&&!update.transactions.some(t=>t.annotation(external)))latest.current.onChange(update.state.sliceDoc())})
  ]})})
  view.current=editor
  return()=>{view.current=null;editor.destroy()}
 },[])
 // A value from outside (Reload, Discard, Format, Undo format) keeps its exact
 // bytes: the separator is chosen first, and the value is split on that
 // separator only, so a lone LF in a CRLF file stays a lone LF when the next
 // keystroke reads the document back.
 useEffect(()=>{
  const editor=view.current
  if(!editor||editor.state.sliceDoc()===props.value)return
  editor.dispatch({effects:lineEnding.current.reconfigure(EditorState.lineSeparator.of(props.value.includes('\r\n')?'\r\n':'\n')),annotations:external.of(true)})
  editor.dispatch({changes:{from:0,to:editor.state.doc.length,insert:editor.state.toText(props.value)},annotations:external.of(true)})
 },[props.value])
 useEffect(()=>{view.current?.dispatch({effects:wrapping.current.reconfigure(props.wrap?EditorView.lineWrapping:[])})},[props.wrap])
 useEffect(()=>{view.current?.dispatch({effects:description.current.reconfigure(EditorView.contentAttributes.of({'aria-label':msg('File contents'),tabindex:'0','aria-readonly':String(Boolean(props.readOnly)),id:props.id,...(props.describedBy?{'aria-describedby':props.describedBy}:{})}))})},[props.id,props.describedBy,locale,props.readOnly])
 useEffect(()=>{view.current?.dispatch({effects:access.current.reconfigure([EditorState.readOnly.of(Boolean(props.readOnly)),EditorView.editable.of(!props.readOnly)])})},[props.readOnly])
 return <><div ref={host} className={styles.editor}/><DOMTooltips container={host}/></>
}
