import {act, cleanup, fireEvent, render, screen} from '@testing-library/react'
import {QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {afterEach, expect, it, vi} from 'vitest'
import {McpContext} from '../mcp/McpProvider'
import {connected, stubClient, testQueryClient} from '../testing/harness'
import {QuickSwitcher} from './QuickSwitcher'

// Keep the actual runtime query hooks: a capability-disabled TanStack query
// stays pending with fetchStatus idle, which differs from an in-flight read.
vi.mock('../files/queries',()=>({useFileListing:()=>({data:{root:'/fixture'}})}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({ready:true,chats:[],packDrafts:[]})}))
vi.mock('../packs/folders/client',()=>({FOLDERS_KEY:['pack-folders'],loadFolders:async()=>({document:{version:1,folders:[],assignments:{}}})}))
vi.mock('../dialects/queries',()=>({dialectListQuery:()=>({queryKey:['input-preparations'],queryFn:async()=>({items:[]})})}))
vi.mock('../dialects/drafts',()=>({dialectDraftListQuery:()=>({queryKey:['dialect-drafts'],queryFn:async()=>({items:[]})})}))
afterEach(()=>{cleanup();localStorage.clear()})

function mount(graphInventorySupported:boolean,graphRead=async()=>({text:'{"graphs":[]}'})) {
 const client=testQueryClient()
 const runtime=stubClient({list_packs:()=>({text:'{"packs":[]}'}),experimental_list_graphs:graphRead})
 render(<QueryClientProvider client={client}><McpContext.Provider value={connected({client:runtime.client,graphInventorySupported})}><MemoryRouter><QuickSwitcher/></MemoryRouter></McpContext.Provider></QueryClientProvider>)
 fireEvent.click(screen.getByRole('button',{name:'Search and switch'}))
 return {client,runtime}
}

it('finishes an empty switcher when graph inventory is unavailable on this runtime',async()=>{
 const {client,runtime}=mount(false)
 await screen.findByText('No matching items.')
 expect(client.getQueryState(['experimental_list_graphs'])?.status).toBe('pending')
 expect(client.getQueryState(['experimental_list_graphs'])?.fetchStatus).toBe('idle')
 expect(screen.queryByText('Loading items…')).toBeNull()
 expect(runtime.calls.some(call=>call.name==='experimental_list_graphs')).toBe(false)
})

it('waits for an available graph inventory while its first read is actually in flight',async()=>{
 let resolve!:(value:{text:string})=>void
 const pending=new Promise<{text:string}>(done=>{resolve=done})
 const {client}=mount(true,()=>pending)
 await screen.findByText('Loading items…')
 expect(client.getQueryState(['experimental_list_graphs'])?.fetchStatus).toBe('fetching')
 await act(async()=>resolve({text:'{"graphs":[]}'}))
 await screen.findByText('No matching items.')
 expect(screen.queryByText('Loading items…')).toBeNull()
})
