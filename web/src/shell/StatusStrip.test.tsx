import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {DeskNoticesProvider} from './DeskNotices'
import {StatusStrip} from './StatusStrip'
const state=vi.hoisted(()=>({ready:false,saving:false,error:'History read failed',store:{canCreate:true,load:vi.fn(),retrySave:vi.fn()}}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>state}))
vi.mock('../admin/DeskHomeSettings',()=>({useDeskHome:()=>({}),DeskHomeNotice:()=>null}))
vi.mock('../admin/ProjectLocation',()=>({useProjectLocation:()=>({}),ProjectLocationNotice:()=>null}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({problems:[]})}))
vi.mock('../mcp/McpProvider',()=>({useMcp:()=>({status:'ready',known:true})}))
vi.mock('../packs/upgrade/UpgradeNote',()=>({ProjectSafeguardsStatus:()=>null}))
vi.mock('./ConnectionNotices',()=>({ConnectionStatus:()=> <button data-connection-status>connected</button>}))
afterEach(()=>{cleanup();vi.clearAllMocks()})
it('reports chat storage once across pages and keeps read and save recovery distinct',async()=>{
 const client=new QueryClient()
 const page=()=> <QueryClientProvider client={client}><MemoryRouter><DeskNoticesProvider><main>Chat content</main><StatusStrip/></DeskNoticesProvider></MemoryRouter></QueryClientProvider>
 const view=render(page())
 fireEvent.click(screen.getByRole('button',{name:'Chat history unavailable'}))
 expect(screen.getByText('Saved chats and drafts could not be loaded.')).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Retry'}))
 expect(state.store.load).toHaveBeenCalledOnce();expect(state.store.retrySave).not.toHaveBeenCalled()
 state.ready=true;state.error='Chat write failed';view.rerender(page())
 expect(screen.getByText('Chat changes could not be saved. Keep this window open until saving succeeds.')).toBeTruthy()
 expect(screen.queryByRole('button',{name:'Retry'})).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Retry saving'}))
 expect(state.store.retrySave).toHaveBeenCalledOnce()
 state.error='';view.rerender(page())
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 expect(screen.getByText('Chat content')).toBeTruthy()
})
