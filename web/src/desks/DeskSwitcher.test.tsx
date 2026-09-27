import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, MemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { DeskSwitcher } from './DeskSwitcher'
import { deskFetch } from '../files/client'
import { openDesk } from './scope'
import { UnsavedChangesProvider, useRegisteredChanges } from '../shell/UnsavedChanges'
const {flush}=vi.hoisted(()=>({flush:vi.fn()}))
vi.mock('../files/client',()=>({deskFetch:vi.fn()}))
vi.mock('../files/queries',()=>({useFileListing:()=>({data:{root:'/projects/startup'}})}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({config:{organization:{name:'Unveil'}}})}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({store:{flush,running:false},dirty:true})}))
vi.mock('./scope',()=>({activeDeskId:'',openDesk:vi.fn()}))
const next={id:'a'.repeat(32),name:'Review',folder:'/desks/review',managed:true}
const directory={current:{id:'',name:'Operations',folder:'/projects/startup',managed:false},desks:[{id:'',name:'Operations',folder:'/projects/startup',managed:false},next],location:'/desks'}
beforeEach(()=>{flush.mockResolvedValue(true);vi.mocked(deskFetch).mockImplementation(async(_url,init)=>new Response(JSON.stringify(init?.method==='POST'?next:directory),{status:200}));document.title='Original'})
afterEach(()=>{cleanup();vi.clearAllMocks()})
function Dirty(){useRegisteredChanges(true,'Discard this job?',{name:'Unsaved job'});return null}
function show(dirty=false){render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><MemoryRouter><UnsavedChangesProvider>{dirty&&<Dirty/>}<DeskSwitcher/></UnsavedChangesProvider></MemoryRouter></QueryClientProvider>)}
async function menu(){await screen.findByText('Operations');fireEvent.keyDown(screen.getByRole('button',{name:'Switch desk'}),{key:'Enter'})}
it('shows the named desk, updates the title, and flushes chats before switching',async()=>{
 show();await menu();await waitFor(()=>expect(document.title).toBe('Operations · Unveil'))
 fireEvent.click(await screen.findByRole('menuitem',{name:'Review'}));await waitFor(()=>expect(openDesk).toHaveBeenCalledWith(next.id));expect(flush).toHaveBeenCalled()
})
it('creates a named desk through the server before opening it',async()=>{
 show();await menu();fireEvent.click(await screen.findByRole('menuitem',{name:'Create desk…'}))
 fireEvent.change(await screen.findByLabelText('Desk name'),{target:{value:'Review'}})
 fireEvent.click(screen.getByRole('button',{name:'Create desk'}))
 await waitFor(()=>expect(openDesk).toHaveBeenCalledWith(next.id))
 expect(deskFetch).toHaveBeenCalledWith('/api/desks',expect.objectContaining({method:'POST',body:'{"name":"Review"}'}))
})
it('preserves the asterisk and requires the item name before abandoning edits',async()=>{
 show(true);await menu();expect(document.title).toBe('* Operations · Unveil')
 fireEvent.click(await screen.findByRole('menuitem',{name:'Review'}));await screen.findByRole('dialog',{name:'Unsaved changes'})
 expect(openDesk).not.toHaveBeenCalled()
 fireEvent.change(screen.getByLabelText('Type Unsaved job to confirm.'),{target:{value:'Yes'}})
 expect((screen.getByRole('button',{name:'Discard changes'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Keep editing'}));expect(openDesk).not.toHaveBeenCalled()
})
it('stays in the current desk if saving the conversation fails',async()=>{
 flush.mockResolvedValue(false);show();await menu();fireEvent.click(await screen.findByRole('menuitem',{name:'Review'}))
 await screen.findByText('Save the conversation before switching desks.');expect(openDesk).not.toHaveBeenCalled()
})

it('keeps the home link separate from its dropdown and puts Project files inside the menu',async()=>{
 show();const home=await screen.findByRole('link',{name:'Operations'})
 expect(home.getAttribute('href')).toBe('/')
 expect(home.querySelector('button')).toBeNull()
 expect(screen.queryByRole('link',{name:'Project files'})).toBeNull()
 await menu()
 expect(screen.getByRole('menuitem',{name:'Project files'}).getAttribute('href')).toBe('/author')
})

it('the desk home link asks before leaving unsaved work', async () => {
 const router=createMemoryRouter([{path:'*',element:<UnsavedChangesProvider><Dirty/><DeskSwitcher/></UnsavedChangesProvider>}],{initialEntries:['/jobs/new']})
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><RouterProvider router={router}/></QueryClientProvider>)
 fireEvent.click(await screen.findByRole('link',{name:/Operations/}))
 await screen.findByRole('dialog',{name:'Unsaved changes'})
 expect(router.state.location.pathname).toBe('/jobs/new')
 fireEvent.click(screen.getByRole('button',{name:'Keep editing'}))
 expect(router.state.location.pathname).toBe('/jobs/new')
})
