import {cleanup, render, screen} from '@testing-library/react'
import {afterEach, expect, it} from 'vitest'
import {UnsavedChangesProvider,useRegisteredChanges,useDeskChangeState,type UnsavedOptions} from './UnsavedChanges'
afterEach(()=>{cleanup();document.title='Desk'})
function Change({changed,...options}:UnsavedOptions & {changed:boolean}) { useRegisteredChanges(changed,'Leave without saving?',options);return null }
function Status(){const state=useDeskChangeState();return <output>{JSON.stringify(state)}</output>}
function frame(children:React.ReactNode){return <UnsavedChangesProvider>{children}<Status/></UnsavedChangesProvider>}
function state(){return JSON.parse(screen.getByRole('status').textContent!)}
function protectedExit(){const event=new Event('beforeunload',{cancelable:true});window.dispatchEvent(event);return event.defaultPrevented}
it('keeps repeated successful autosaves quiet without losing tab-close protection',()=>{
 document.title='Desk'
 const view=render(frame(<Change changed={false} kind="autosave" label="Chat history"/>))
 for(let n=0;n<3;n++) {
  view.rerender(frame(<Change changed kind="autosave" label="Chat history"/>))
  expect(state()).toEqual({dirty:false,pending:true,labels:[]});expect(document.title).toBe('Desk');expect(protectedExit()).toBe(true)
  view.rerender(frame(<Change changed={false} kind="autosave" label="Chat history"/>))
  expect(state().pending).toBe(false);expect(document.title).toBe('Desk');expect(protectedExit()).toBe(false)
 }
})
it('holds a failed-save warning through retries until persistence succeeds',()=>{
 document.title='Desk'
 const view=render(frame(<Change changed kind="autosave" saveFailed label="Chat history"/>))
 expect(state()).toEqual({dirty:true,pending:true,labels:['Chat history']});expect(document.title).toBe('* Desk')
 view.rerender(frame(<Change changed kind="autosave" saveFailed={false} busy label="Chat history"/>))
 expect(state().dirty).toBe(true);expect(protectedExit()).toBe(true);expect(document.title).toBe('* Desk')
 view.rerender(frame(<Change changed kind="autosave" saveFailed label="Chat history"/>))
 expect(state().dirty).toBe(true)
 view.rerender(frame(<Change changed={false} kind="autosave" label="Chat history"/>))
 expect(state()).toEqual({dirty:false,pending:false,labels:[]});expect(document.title).toBe('Desk')
})
it('does not let autosaves or another editor clear remaining manual edits',()=>{
 document.title='Desk'
 const content=(chat:boolean,pack:boolean,job:boolean)=>frame(<><Change changed={chat} kind="autosave"/><Change changed={pack} name="Pack"/><Change changed={job} name="Job"/></>)
 const view=render(content(true,true,true));expect(state().labels).toEqual(['Pack','Job'])
 view.rerender(content(false,true,true));expect(document.title).toBe('* Desk')
 view.rerender(content(true,false,true));expect(state().labels).toEqual(['Job'])
 view.rerender(content(true,false,false));expect(document.title).toBe('Desk');expect(protectedExit()).toBe(true)
 view.unmount();expect(protectedExit()).toBe(false)
})
it('protects active writes and activity without presenting them as edits',()=>{
 document.title='Desk'
 const view=render(frame(<Change changed={false} busy/>))
 expect(state()).toEqual({dirty:false,pending:true,labels:[]});expect(document.title).toBe('Desk');expect(protectedExit()).toBe(true)
 view.rerender(frame(<Change changed kind="activity"/>))
 expect(state().dirty).toBe(false);expect(protectedExit()).toBe(true)
 view.rerender(frame(<Change changed={false} kind="activity"/>))
 expect(protectedExit()).toBe(false)
})
