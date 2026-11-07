import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {DeskNoticesProvider} from './DeskNotices'
import {StatusStrip} from './StatusStrip'
import {decodeStorageBudgetStatus, type StorageBudgetStatus} from '../admin/storageBudgetModel'
import {storageStatusFixture} from '../admin/storageBudgetModel.fixture'
const storage=vi.hoisted(()=>({data:undefined as StorageBudgetStatus | undefined}))
vi.mock('../admin/useStorageBudgetStatus',()=>({useStorageBudgetStatus:()=>storage}))
const state=vi.hoisted(()=>({ready:false,saving:false,error:'History read failed',store:{canCreate:true,load:vi.fn(),retrySave:vi.fn()}}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>state}))
vi.mock('../admin/DeskHomeSettings',()=>({useDeskHome:()=>({}),DeskHomeNotice:()=>null}))
vi.mock('../admin/ProjectLocation',()=>({useProjectLocation:()=>({}),ProjectLocationNotice:()=>null}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({problems:[]})}))
vi.mock('../mcp/McpProvider',()=>({useMcp:()=>({status:'ready',known:true})}))
vi.mock('../packs/upgrade/UpgradeNote',()=>({ProjectSafeguardsStatus:()=>null}))
vi.mock('./ConnectionNotices',()=>({ConnectionStatus:()=> <button data-connection-status>connected</button>}))
afterEach(()=>{cleanup();vi.clearAllMocks();storage.data=undefined})
/** The contract fixture is over its limits; this one is within them. */
function withinLimits(){
 const value=storageStatusFixture()
 for(const row of [value.snapshot.installation,value.snapshot.currentWorkspace,...value.snapshot.categories,...value.snapshot.currentWorkspaceCategories]) row.overBudget=false
 return value
}
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

it('coalesces child low-space and actual refusal into the existing volatile storage notice',async()=>{
 state.error='';state.ready=true
 const childObservation={coverage:'selected-child-roots-only',enforcement:'warning-only',state:'stale',observedAt:'2026-11-01T12:00:00Z',lowSpaceThresholdBytes:'1073741824',roots:[{component:'runner',role:'workspace',state:'measured',reason:'none',logicalBytes:'5',files:'1',directories:'1',availableBytes:'0',lowSpace:true}]}
 storage.data=decodeStorageBudgetStatus({...storageStatusFixture(),availability:'unavailable',snapshot:null,reason:'storage-busy',childObservation})
 const client=new QueryClient()
 const page=()=> <QueryClientProvider client={client}><MemoryRouter><DeskNoticesProvider><StatusStrip/></DeskNoticesProvider></MemoryRouter></QueryClientProvider>
 const view=render(page())
 fireEvent.click(screen.getByRole('button',{name:'Storage needs attention'}))
 expect(screen.getByText(/A child storage observation reports low filesystem space/)).toBeTruthy()
 expect(screen.queryByText(/A storage refusal was reported/)).toBeNull()
 expect(screen.getAllByRole('link',{name:'Review storage'})).toHaveLength(1)
 storage.data=decodeStorageBudgetStatus({...storageStatusFixture(),childObservation,pressure:{active:true,codes:['storage-disk-full'],firstSeenAt:'2026-11-01T12:00:00Z',lastSeenAt:'2026-11-01T12:00:00Z',durableState:'unconfirmed'}})
 view.rerender(page())
 expect(screen.getByText(/A storage refusal was reported/)).toBeTruthy()
 expect(screen.getAllByRole('link',{name:'Review storage'})).toHaveLength(1)
 storage.data=decodeStorageBudgetStatus({...withinLimits(),childObservation:{...childObservation,state:'partial',roots:[{component:'runner',role:'workspace',state:'unavailable',reason:'unknown',logicalBytes:null,files:null,directories:null,availableBytes:null,lowSpace:null}]}})
 view.rerender(page())
 await waitFor(()=>expect(screen.queryByRole('button',{name:'Storage needs attention'})).toBeNull())
 client.clear()
})

it('reports folders storage measures but has not registered in the same storage notice',async()=>{
 state.error='';state.ready=true
 storage.data=decodeStorageBudgetStatus({...storageStatusFixture(),registration:{enforcement:'warning-only',folders:['/projects/one/.desk-private']}})
 const client=new QueryClient()
 render(<QueryClientProvider client={client}><MemoryRouter><DeskNoticesProvider><StatusStrip/></DeskNoticesProvider></MemoryRouter></QueryClientProvider>)
 fireEvent.click(screen.getByRole('button',{name:'Storage needs attention'}))
 expect(screen.getByText(/Storage measures folders Desk made that are not registered with it yet/)).toBeTruthy()
 expect(screen.queryByText(/A storage refusal was reported/)).toBeNull()
 expect(screen.getAllByRole('link',{name:'Review storage'})).toHaveLength(1)
 client.clear()
})

it('warns, in the same storage notice, that storage is over a limit and Desk keeps working',async()=>{
 state.error='';state.ready=true
 storage.data=decodeStorageBudgetStatus(withinLimits())
 const client=new QueryClient()
 const page=()=> <QueryClientProvider client={client}><MemoryRouter><DeskNoticesProvider><StatusStrip/></DeskNoticesProvider></MemoryRouter></QueryClientProvider>
 const view=render(page())
 expect(screen.queryByRole('button',{name:'Storage needs attention'})).toBeNull()
 const over=withinLimits();over.snapshot.categories[4].overBudget=true
 storage.data=decodeStorageBudgetStatus(over)
 view.rerender(page())
 fireEvent.click(screen.getByRole('button',{name:'Storage needs attention'}))
 expect(screen.getByText(/In this release limits are warnings: Desk keeps working and refuses no work for them/)).toBeTruthy()
 expect(screen.queryByText(/A storage refusal was reported/)).toBeNull()
 expect(screen.getAllByRole('link',{name:'Review storage'})).toHaveLength(1)
 client.clear()
})
