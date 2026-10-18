import { useSyncExternalStore } from 'react'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { ChatStore, type Chat, type ChatPersistence } from '../../chat/store'
import { useChats } from '../../chat/ChatProvider'
import { memoryDraftPersistence } from '../../testing/draftPersistence'
import { testQueryClient } from '../../testing/harness'
import { INITIAL_STATE } from '../../research/run'
import { DraftActions, DraftActionsProvider } from './DraftActions'

const fixture=vi.hoisted(()=>({store:null as ChatStore|null}))
vi.mock('../../chat/ChatProvider',()=>({useChats:()=>({...useSyncExternalStore(fixture.store!.subscribe,fixture.store!.getSnapshot),store:fixture.store})}))
afterEach(()=>{cleanup();fixture.store?.dispose();fixture.store=null})
async function setup(recheck?:{run:()=>void;disabled:boolean}) {
 const chats:Chat[]=[{id:'chat',title:'Conversation',pinned:false,archived:false,updatedAt:'2026-10-10',composer:'',model:'',mode:'draft',view:'draft',checkpoint:{sources:[],state:{...INITIAL_STATE,candidates:[{document:{title:'Example pack'},text:'{"title":"Example pack"}',digest:'test',revision:1,producedBy:'conversation'}]}}}]
 const write=vi.fn<ChatPersistence['write']>(async content=>({project:'/test',content,sha256:'saved'}))
 const store=new ChatStore('/test',{read:async()=>({project:'/test',content:{version:1,chats},sha256:'saved'}),write},memoryDraftPersistence('/test'))
 fixture.store=store;await store.load();await store.flush();write.mockClear()
 function Document() {
  const {packDrafts,error}=useChats()
  return error || !packDrafts[0] ? <p>Document unavailable</p> : <DraftActions draft={packDrafts[0]} recheck={recheck}/>
 }
 const router=createMemoryRouter([{path:'*',element:<DraftActionsProvider><Document/></DraftActionsProvider>}],{initialEntries:['/packs/drafts/example']})
 render(<QueryClientProvider client={testQueryClient()}><RouterProvider router={router}/></QueryClientProvider>)
 return {store,write,router}
}
async function openMenu() {
 fireEvent.keyDown(screen.getByRole('button',{name:'Pack options'}),{key:'Enter'})
 return screen.findByRole('menu')
}
it('uses command rows, opens a rename dialog, and returns focus on cancel without writing',async()=>{
 const {store,write}=await setup();const original=store.getSnapshot().packDrafts[0]!.title
 const menu=await openMenu();expect(within(menu).queryByRole('textbox')).toBeNull()
 expect(within(menu).getAllByRole('menuitem').map(item=>item.textContent)).toEqual(['Rename…','View JSON','Delete draft…'])
 fireEvent.click(screen.getByRole('menuitem',{name:'Rename…'}))
 const dialog=await screen.findByRole('dialog',{name:'Rename draft'})
 expect(screen.queryByRole('menu')).toBeNull()
 await waitFor(()=>expect(document.activeElement).toBe(within(dialog).getByRole('textbox',{name:'Name'})))
 fireEvent.change(within(dialog).getByRole('textbox'),{target:{value:'Unsaved name'}})
 fireEvent.click(within(dialog).getByRole('button',{name:'Cancel'}))
 await waitFor(()=>expect(document.activeElement).toBe(screen.getByRole('button',{name:'Pack options'})))
 expect(store.getSnapshot().packDrafts[0]!.title).toBe(original);expect(write).not.toHaveBeenCalled()
})
it('keeps rename errors and typed text in the dialog even if the document disappears, then retries',async()=>{
 const {store,write}=await setup();await openMenu();fireEvent.click(screen.getByRole('menuitem',{name:'Rename…'}))
 const dialog=await screen.findByRole('dialog',{name:'Rename draft'});const input=within(dialog).getByRole('textbox')
 write.mockRejectedValueOnce(new Error('Disk unavailable'))
 fireEvent.change(input,{target:{value:'New name'}});fireEvent.click(within(dialog).getByRole('button',{name:'Rename'}))
 await within(dialog).findByRole('alert');expect(input).toHaveProperty('value','New name')
 expect(screen.getByText('Document unavailable')).toBeTruthy()
 fireEvent.click(within(dialog).getByRole('button',{name:'Rename'}))
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 expect(store.getSnapshot().packDrafts[0]!.title).toBe('New name');expect(store.getSnapshot().dirty).toBe(false)
})
it('keeps delete confirmation mounted after removal and navigates only after saving succeeds',async()=>{
 const {store,write,router}=await setup();await openMenu();fireEvent.click(screen.getByRole('menuitem',{name:'Delete draft…'}))
 const dialog=await screen.findByRole('dialog',{name:'Delete draft?'})
 expect(store.getSnapshot().packDrafts).toHaveLength(1)
 write.mockRejectedValueOnce(new Error('Disk unavailable'))
 fireEvent.click(within(dialog).getByRole('button',{name:'Delete draft'}))
 await within(dialog).findByRole('alert');expect(router.state.location.pathname).toBe('/packs/drafts/example')
 expect(store.getSnapshot().chats).toHaveLength(1)
 fireEvent.click(within(dialog).getByRole('button',{name:'Retry saving'}))
 await waitFor(()=>expect(router.state.location.pathname).toBe('/library'))
 expect(screen.queryByRole('dialog')).toBeNull();expect(store.getSnapshot().packDrafts).toHaveLength(0)
 expect(store.getSnapshot().chats).toHaveLength(1);expect(store.getSnapshot().dirty).toBe(false)
})


it('runs draft checks only from an explicit menu command',async()=>{
 const run=vi.fn();await setup({run,disabled:false})
 expect(run).not.toHaveBeenCalled()
 const menu=await openMenu()
 expect(within(menu).getAllByRole('menuitem').map(item=>item.textContent)).toEqual(['Rename…','View JSON','Run checks','Delete draft…'])
 fireEvent.click(within(menu).getByRole('menuitem',{name:'Run checks'}))
 expect(run).toHaveBeenCalledOnce();expect(screen.queryByRole('menu')).toBeNull()
 expect(within(await openMenu()).getByRole('menuitem',{name:'Run checks'})).toBeTruthy()
})
it('does not start another check while it is unavailable',async()=>{
 const run=vi.fn();await setup({run,disabled:true});await openMenu()
 const command=screen.getByRole('menuitem',{name:'Run checks'})
 expect(command.getAttribute('data-disabled')).not.toBeNull()
 fireEvent.click(command);expect(run).not.toHaveBeenCalled()
})
