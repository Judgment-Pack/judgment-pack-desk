import {deskFetch} from '../files/client'
import { SelectionActions, type BrowserItem } from './CollectionActions'
import { useState, useSyncExternalStore } from 'react'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, MemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { ChatStore, type Chat, type ChatPersistence } from '../chat/store'
import { INITIAL_STATE } from '../research/run'
import { memoryDraftPersistence } from '../testing/draftPersistence'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { McpContext } from '../mcp/McpProvider'
import { PacksPane } from './PacksPane'

vi.mock('../files/client',async original=>({...await original(),deskFetch:vi.fn()}))
const fixture=vi.hoisted(()=>({store:null as ChatStore|null}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({...useSyncExternalStore(fixture.store!.subscribe,fixture.store!.getSnapshot),store:fixture.store})}))
afterEach(()=>{cleanup();fixture.store?.dispose();fixture.store=null;sessionStorage.clear()})
async function setup(){
 let graphRecord={version:1 as const,drafts:[],deleted:[]} as import('../graphs/drafts').GraphDraftDocument
 vi.mocked(deskFetch).mockImplementation(async(url,init)=>{if(String(url)==='/api/draft-graphs'){if(init?.body)graphRecord=JSON.parse(init.body as string);return Response.json({project:'/test',sha256:'graph',content:graphRecord})}return Response.json({items:[]})})
 const common={title:'Conversation',pinned:false,archived:false,updatedAt:'2026-10-10T12:00:00Z',composer:'',model:'',mode:'draft' as const,view:'draft' as const}
 let chats:Chat[]=[
  {...common,id:'pack-chat',checkpoint:{sources:[],state:{...INITIAL_STATE,candidates:[{document:{title:'Pending pack'},text:'{"title":"Pending pack"}',digest:'test',revision:1,producedBy:'conversation'}]}}},
  {...common,id:'graph-chat',graph:{id:'pending-graph',path:'pending-graph.json',workspace:'pending'},graphDrafts:[{id:'pending-graph',path:'pending-graph.json',content:'{}',draftId:'pending',createdAt:'2026-10-10T12:00:00Z'}]}
 ]
 const write=vi.fn<ChatPersistence['write']>(async content=>{chats=structuredClone(content.chats);return {project:'/test',content,sha256:'saved'}})
 const io:ChatPersistence={read:async()=>({project:'/test',content:{version:1,chats},sha256:'saved'}),write}
 const store=new ChatStore('/test',io,memoryDraftPersistence('/test'));fixture.store=store;await store.load();await store.flush();write.mockClear()
 const stub=stubClient({list_packs:()=>({text:JSON.stringify({status:'valid',packs:[{id:'alpha',packVersion:'1.0.0'}]})}),experimental_list_graphs:()=>({text:JSON.stringify({status:'valid',graphs:[{id:'flow',graphVersion:'2.0.0'}]})})})
 const router=createMemoryRouter([{path:'*',element:<McpContext.Provider value={connected({client:stub.client,graphInventorySupported:true})}><PacksPane/></McpContext.Provider>}],{initialEntries:['/library']})
 render(<QueryClientProvider client={testQueryClient()}><RouterProvider router={router}/></QueryClientProvider>)
 await screen.findByRole('checkbox',{name:'Select alpha'});await screen.findByRole('checkbox',{name:'Select flow'})
 return {router,store,write}
}
async function selectionMenu() {
 fireEvent.keyDown(screen.getByRole('button',{name:/^\d+ selected$/}),{key:'Enter'})
 return await screen.findByRole('menu')
}
async function closeMenu() {
 fireEvent.keyDown(screen.getByRole('menu'),{key:'Escape'})
 await waitFor(()=>expect(screen.queryByRole('menu')).toBeNull())
}
it('selects every object type from the leading column and places draft state with its version',async()=>{
 const {router}=await setup()
 expect(screen.getAllByRole('checkbox')).toHaveLength(5)
 const checkbox=screen.getByRole('checkbox',{name:'Select Pending pack'})
 const row=checkbox.closest('li')!
 expect(row.firstElementChild?.contains(checkbox)).toBe(true)
 const link=within(row).getByRole('link')
 expect(link.lastElementChild?.textContent).toBe('Draft')
 expect(within(row).getByRole('img',{name:'Pack'})).toBeTruthy()
 expect(within(screen.getByRole('checkbox',{name:'Select pending-graph'}).closest('li')!).getByRole('img',{name:'Graph'})).toBeTruthy()
 fireEvent.click(checkbox)
 expect(router.state.location.pathname).toBe('/library')
 await selectionMenu()
 expect(screen.getByRole('menuitem',{name:'Compose graph'}).getAttribute('aria-disabled')).toBe('true')
 fireEvent.click(screen.getByRole('menuitem',{name:'Compose graph'}))
 expect(router.state.location.pathname).toBe('/library')
 await closeMenu()
 fireEvent.click(screen.getByRole('checkbox',{name:'Select alpha'}))
 await selectionMenu()
 expect(screen.getByRole('menuitem',{name:'Delete drafts'}).getAttribute('aria-disabled')).toBe('true')
 fireEvent.click(screen.getByRole('menuitem',{name:'Delete drafts'}))
 expect(screen.queryByRole('dialog')).toBeNull()
})
it('selects only visible rows and removes filtered-out items from selection',async()=>{
 await setup()
 const all=screen.getByRole('checkbox',{name:'Select all matches'}) as HTMLInputElement
 fireEvent.click(screen.getByRole('checkbox',{name:'Select alpha'}))
 expect(all.indeterminate).toBe(true)
 fireEvent.click(all)
 expect(screen.getByText('4 selected')).toBeTruthy()
 expect(all.checked).toBe(true)
 expect(all.indeterminate).toBe(false)
 expect(all.getAttribute('aria-label')).toBe('Clear selection')
 fireEvent.click(all)
 expect(screen.queryByRole('group',{name:'Selected item actions'})).toBeNull()
 expect(all.checked).toBe(false)
 fireEvent.click(all)
 fireEvent.change(screen.getByRole('searchbox',{name:'Search library'}),{target:{value:'pending'}})
 await waitFor(()=>expect(screen.getByText('2 selected')).toBeTruthy())
 expect(screen.getAllByRole('checkbox')).toHaveLength(3)
 fireEvent.click(screen.getByRole('button',{name:'Clear selection'}))
 fireEvent.click(all)
 expect(screen.getByText('2 selected')).toBeTruthy()
 fireEvent.keyDown(screen.getByRole('checkbox',{name:'Select Pending pack'}),{key:'Escape'})
 expect(screen.queryByRole('group',{name:'Selected item actions'})).toBeNull()
})
it('confirms draft deletion, retains conversations, and retries a failed save without deleting twice',async()=>{
 const {store,write}=await setup()
 fireEvent.click(screen.getByRole('checkbox',{name:'Select Pending pack'}));fireEvent.click(screen.getByRole('checkbox',{name:'Select pending-graph'}))
 await selectionMenu()
 fireEvent.click(screen.getByRole('menuitem',{name:'Delete drafts'}))
 const dialog=screen.getByRole('dialog',{name:'Delete selected drafts?'})
 expect(store.getSnapshot().packDrafts).toHaveLength(1)
 write.mockRejectedValueOnce(new Error('Disk unavailable'))
 fireEvent.click(within(dialog).getByRole('button',{name:'Delete drafts'}))
 await within(dialog).findByText(/Disk unavailable/)
 expect(store.getSnapshot().chats).toHaveLength(2)
 expect(store.getSnapshot().packDrafts).toHaveLength(0)
 fireEvent.click(within(dialog).getByRole('button',{name:'Retry saving'}))
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 expect(store.getSnapshot().dirty).toBe(false)
 expect(screen.getByRole('checkbox',{name:'Select alpha'})).toBeTruthy()
 expect(screen.getByRole('checkbox',{name:'Select flow'})).toBeTruthy()
 await act(async()=>store.load())
 expect(store.getSnapshot().packDrafts).toHaveLength(0)
 expect(store.getSnapshot().chats.find(chat=>chat.id==='graph-chat')?.graphDrafts).toEqual([])
})
it('cancels row deletion and restores focus to the row options control',async()=>{
 const {store}=await setup()
 const opener=screen.getByRole('button',{name:'Options for Pending pack'});opener.focus()
 fireEvent.keyDown(opener,{key:'Enter'})
 await screen.findByRole('menuitem',{name:'Delete draft'})
 expect(screen.queryByRole('menuitem',{name:'Open'})).toBeNull()
 fireEvent.click(screen.getByRole('menuitem',{name:'Delete draft'}))
 fireEvent.click(within(screen.getByRole('dialog')).getByRole('button',{name:'Cancel'}))
 await waitFor(()=>expect(document.activeElement).toBe(opener))
 expect(store.getSnapshot().packDrafts).toHaveLength(1)
})

it('returns focus to select-all when clearing from the compact selection menu', async()=>{
 function CompactSelection() {
  const [items,setItems]=useState<BrowserItem[]>([{key:'pack:pack',id:'pack',title:'Pack',kind:'pack',status:'finalized',href:'/packs/pack'}])
  return <><input type="checkbox" data-select-all aria-label="Select all"/><SelectionActions items={items} clear={()=>setItems([])} onDelete={()=>{}} compact/></>
 }
 render(<MemoryRouter><CompactSelection/></MemoryRouter>)
 await selectionMenu()
 fireEvent.click(screen.getByRole('menuitem',{name:'Clear selection'}))
 await waitFor(()=>expect(document.activeElement).toBe(screen.getByRole('checkbox',{name:'Select all'})))
 expect(screen.queryByRole('menu')).toBeNull()
})
