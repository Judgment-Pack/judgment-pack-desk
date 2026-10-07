import { afterEach, expect, it, vi } from 'vitest'
import { act, cleanup, render } from '@testing-library/react'
import { EditorView } from '@codemirror/view'
import CodeEditor from './CodeEditor'
afterEach(cleanup)
// The editor itself (the page tests stand it in): exact bytes through a value
// from outside, read back by the next keystroke (review round 1, finding 2).
const props=(value:string,onChange:(value:string)=>void,path='notes/mixed.txt')=>({id:'editor',path,value,wrap:false,onChange,onFormat:()=>{},onSave:()=>{}})
const viewOf=(container:HTMLElement)=>EditorView.findFromDOM(container.querySelector('.cm-editor') as HTMLElement)!
const type=(view:EditorView,text:string)=>act(()=>{view.dispatch({changes:{from:view.state.doc.length,insert:text},userEvent:'input.type'})})

it('keeps a lone LF in a CRLF file when a value from outside replaces the document and a key is typed',()=>{
 const onChange=vi.fn()
 const {container,rerender}=render(<CodeEditor {...props('first\n',onChange)}/>)
 rerender(<CodeEditor {...props('a\r\nb\nc',onChange)}/>)
 const view=viewOf(container)
 expect(view.state.sliceDoc()).toBe('a\r\nb\nc')
 expect(onChange).not.toHaveBeenCalled()
 type(view,'d')
 expect(onChange).toHaveBeenLastCalledWith('a\r\nb\ncd')
})

it('keeps each file its own line endings, as opened and as replaced from outside',()=>{
 const onChange=vi.fn()
 const {container,rerender}=render(<CodeEditor {...props('x\r\ny\nz',onChange)}/>)
 const view=viewOf(container)
 type(view,'!')
 expect(onChange).toHaveBeenLastCalledWith('x\r\ny\nz!')
 rerender(<CodeEditor {...props('one\ntwo\r',onChange)}/>)
 expect(view.state.sliceDoc()).toBe('one\ntwo\r')
 type(view,'?')
 expect(onChange).toHaveBeenLastCalledWith('one\ntwo\r?')
 rerender(<CodeEditor {...props('p\r\nq\r\n',onChange)}/>)
 type(view,'.')
 expect(onChange).toHaveBeenLastCalledWith('p\r\nq\r\n.')
})
