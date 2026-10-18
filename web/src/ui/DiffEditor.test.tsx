import {cleanup,render,screen} from '@testing-library/react'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {EditorView} from '@codemirror/view'
import {EditorState} from '@codemirror/state'
import {language} from '@codemirror/language'
import {getOriginalDoc} from '@codemirror/merge'
import DiffEditor from './DiffEditor'
beforeEach(()=>{vi.stubGlobal('requestAnimationFrame',vi.fn(()=>0));vi.stubGlobal('cancelAnimationFrame',vi.fn())})
afterEach(()=>{cleanup();vi.unstubAllGlobals()})
it('keeps JSON proposal editor defaults read-only',()=>{
 render(<DiffEditor before='{"a":1}' after='{"a":2}' split={false} unchanged={false}/>);
 const content=screen.getByRole('region',{name:'Proposed changes'}),view=EditorView.findFromDOM(content)!
 expect(view.state.facet(EditorState.readOnly)).toBe(true);expect(content.getAttribute('contenteditable')).toBe('false')
 expect(view.state.facet(language)?.name).toBe('json')
})
it('retains the full old and new documents beyond 128 KiB in unified plaintext view',()=>{
 const prefix='x'.repeat(128*1024),before=prefix+'old suffix',after=prefix+'changed suffix'
 render(<DiffEditor before={before} after={after} split={false} unchanged={false} language="text" ariaLabel="Policy text changes"/>);
 const view=EditorView.findFromDOM(screen.getByRole('region',{name:'Policy text changes'}))!
 expect(view.state.doc.toString()).toBe(after);expect(getOriginalDoc(view.state).toString()).toBe(before)
 expect(view.state.facet(language)).toBeNull();expect(view.state.facet(EditorState.readOnly)).toBe(true)
})
it('uses actual accessible labels and retains complete text in expanded split view',()=>{
 const prefix='x'.repeat(128*1024),before=prefix+'old suffix',after=prefix+'changed suffix'
 render(<DiffEditor before={before} after={after} split unchanged={false} language="text" beforeLabel="Earlier source" afterLabel="Retained source"/>);
 const left=EditorView.findFromDOM(screen.getByRole('region',{name:'Earlier source'}))!,right=EditorView.findFromDOM(screen.getByRole('region',{name:'Retained source'}))!
 expect(left.state.doc.toString()).toBe(before);expect(right.state.doc.toString()).toBe(after)
 expect(left.state.facet(EditorState.readOnly)).toBe(true);expect(right.state.facet(EditorState.readOnly)).toBe(true)
})
