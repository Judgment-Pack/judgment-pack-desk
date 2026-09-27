import { useState } from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { RouterProvider, createMemoryRouter, Outlet, useNavigate } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useDirtyGuard } from './useDirtyGuard'
import { UnsavedChangesProvider } from './UnsavedChanges'

afterEach(() => { cleanup(); vi.restoreAllMocks(); document.title = 'Desk' })
function Editor({initial=true, name, saveDraft, busy=false}: {initial?:boolean;name?:string;saveDraft?:()=>Promise<void>;busy?:boolean}) {
 const [dirty,setDirty]=useState(initial),navigate=useNavigate()
 useDirtyGuard(dirty,'Leave anyway?',{name,saveDraft,busy})
 return <><button onClick={()=>navigate('/packs/a?edit=1')}>toggle edit</button><button onClick={()=>navigate('/elsewhere')}>leave</button><button onClick={()=>setDirty(false)}>saved</button></>
}
function draw(props:Parameters<typeof Editor>[0]={}) {
 document.title='Desk'
 const router=createMemoryRouter([{element:<UnsavedChangesProvider><Outlet/></UnsavedChangesProvider>,children:[{path:'/packs/:id',element:<Editor {...props}/>},{path:'/elsewhere',element:<p>elsewhere</p>}]}],{initialEntries:['/packs/a']})
 render(<RouterProvider router={router}/>);return router
}
function type(value:string){fireEvent.change(screen.getByRole('textbox'),{target:{value}})}
describe('unsaved-work protection',()=>{
 it('retains query-only navigation and leaves clean editors without prompting',async()=>{
  const router=draw();fireEvent.click(screen.getByText('toggle edit'))
  await waitFor(()=>expect(router.state.location.search).toBe('?edit=1'));expect(screen.queryByRole('dialog')).toBeNull()
  fireEvent.click(screen.getByText('saved'));fireEvent.click(screen.getByText('leave'))
  await waitFor(()=>expect(router.state.location.pathname).toBe('/elsewhere'))
 })
 it('requires the entity name and cancels without losing work',async()=>{
  const native=vi.spyOn(window,'confirm'),router=draw({name:'Example job'})
  fireEvent.click(screen.getByText('leave'));await screen.findByRole('dialog',{name:'Unsaved changes'})
  type('Yes');expect(screen.getByRole('button',{name:'Discard changes'}).hasAttribute('disabled')).toBe(true)
  fireEvent.click(screen.getByText('Keep editing'));await waitFor(()=>expect([...router.state.blockers.values()].every(b=>b.state==='unblocked')).toBe(true));expect(router.state.location.pathname).toBe('/packs/a');expect(document.title).toBe('* Desk')
  fireEvent.click(screen.getByText('leave'));await screen.findByRole('dialog');type('Example job');fireEvent.click(screen.getByText('Discard changes'))
  await waitFor(()=>expect(router.state.location.pathname).toBe('/elsewhere'));await waitFor(()=>expect(document.title).toBe('Desk'));expect(native).not.toHaveBeenCalled()
 })
 it('uses Yes for unnamed changes and keeps work after a failed draft save',async()=>{
  const saveDraft=vi.fn().mockRejectedValueOnce(Error('Disk full')).mockResolvedValueOnce(undefined),router=draw({saveDraft})
  fireEvent.click(screen.getByText('leave'));await screen.findByLabelText('Type Yes to confirm.')
  fireEvent.click(screen.getByText('Save draft and leave'));await screen.findByText('Disk full')
  expect(router.state.location.pathname).toBe('/packs/a');expect(document.title).toBe('* Desk')
  fireEvent.click(screen.getByText('Save draft and leave'));await waitFor(()=>expect(router.state.location.pathname).toBe('/elsewhere'));expect(saveDraft).toHaveBeenCalledTimes(2)
 })
 it('blocks navigation during a write without offering a discard',async()=>{
  const router=draw({initial:false,busy:true});fireEvent.click(screen.getByText('leave'))
  await waitFor(()=>expect(router.state.blockers.size===0||[...router.state.blockers.values()].every(b=>b.state==='unblocked')).toBe(true))
  expect(router.state.location.pathname).toBe('/packs/a');expect(screen.queryByRole('dialog')).toBeNull()
 })
 it('aggregates multiple editors, removes the tab marker only when all are clean, and guards tab closing',()=>{
  function Form({label}:{label:string}){const [dirty,setDirty]=useState(true);useDirtyGuard(dirty,'Leave?');return <button onClick={()=>setDirty(false)}>{label}</button>}
  document.title='Desk'
  render(<UnsavedChangesProvider><Form label="one"/><Form label="two"/></UnsavedChangesProvider>)
  expect(document.title).toBe('* Desk');fireEvent.click(screen.getByText('one'));expect(document.title).toBe('* Desk')
  const event=new Event('beforeunload',{cancelable:true});window.dispatchEvent(event);expect(event.defaultPrevented).toBe(true)
  fireEvent.click(screen.getByText('two'));expect(document.title).toBe('Desk')
  const clean=new Event('beforeunload',{cancelable:true});window.dispatchEvent(clean);expect(clean.defaultPrevented).toBe(false)
 })
 it('cancels on Escape and restores focus to the navigation control',async()=>{
  const router=draw(),button=screen.getByText('leave');button.focus();fireEvent.click(button);await screen.findByRole('dialog')
  fireEvent.keyDown(screen.getByRole('dialog'),{key:'Escape'});await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
  expect(router.state.location.pathname).toBe('/packs/a');expect(document.activeElement).toBe(button)
 })
})
