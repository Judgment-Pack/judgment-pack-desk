import {HighlightStyle} from '@codemirror/language'
import {EditorView} from '@codemirror/view'
import {tags} from '@lezer/highlight'
export const codeColors=HighlightStyle.define([
 {tag:tags.propertyName,color:'var(--decision-cyan)'},
 {tag:tags.string,color:'var(--decision-green)'},
 {tag:[tags.number,tags.bool,tags.null],color:'var(--decision-violet)'},
 {tag:tags.punctuation,color:'var(--ink-soft)'}
])
export const codeTheme=EditorView.theme({
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
