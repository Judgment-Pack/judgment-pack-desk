import { cleanup, render, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { INITIAL_STATE } from '../research/run'
import { ChatProvider } from './ChatProvider'

const m=vi.hoisted(()=>({preference:{} as any,connections:{} as any,catalog:{} as any,report:vi.fn(),snapshot:{} as any,binding:{} as any,options:{} as any}))
vi.mock('../files/queries',()=>({useFileListing:()=>({data:{root:'/fixture'}})}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({config:{research:{gateway:{authority:'test'},documents:{enabled:true},sources:{}}},desk:{localGateway:{status:'ready'}}})}))
vi.mock('../connections/catalog',()=>({useConnections:()=>m.catalog}))
vi.mock('../search/connections',()=>({useSearchPreference:()=>m.preference,useSearchConnections:()=>m.connections}))
vi.mock('../research/useResearchRun',()=>({useResearchRun:(options:unknown)=>{m.options=options;return m.binding}}))
vi.mock('./store',()=>({ChatStore:class {
 load=async()=>{};retain=()=>()=>{};subscribe=()=>()=>{};getSnapshot=()=>m.snapshot;report=m.report
}}))
beforeEach(()=>{
 m.report.mockClear()
 m.snapshot={active:['chat'],chats:[{id:'chat',mode:'web-research'}],drafts:[],dirty:false}
 m.binding={run:null,state:INITIAL_STATE,ledger:null,sources:[],blocked:'',model:'fixture',researchConfigured:true}
 m.preference={isPending:true,isError:false}
 m.connections={available:true,loading:true,isError:false}
 m.catalog={web:true,discovery:true,loading:false,isError:false}
})
afterEach(cleanup)
it('holds the first chat turn until its saved search connection is known',async()=>{
 const view=render(<ChatProvider><span>Desk</span></ChatProvider>)
 await waitFor(()=>expect(m.report.mock.lastCall?.[1].blocked).toBe('Loading search settings…'))
 m.preference={isPending:false,isError:false,data:{value:{mode:'auto',connection:'google'}}}
 view.rerender(<ChatProvider><span>Desk</span></ChatProvider>)
 await waitFor(()=>expect(m.report.mock.lastCall?.[1].blocked).toBe('Checking search connections…'))
 m.connections={available:true,loading:false,isError:false,data:{connections:[{id:'google',revision:'ab'.repeat(32),provider:'google-grounding'}]}}
 view.rerender(<ChatProvider><span>Desk</span></ChatProvider>)
 await waitFor(()=>expect(m.report.mock.lastCall?.[1].blocked).toBe(''))
 expect(m.options.researchPolicy()).toContain('configured search connection is google-grounding')
})
it('does not turn a failed settings read into an unconfigured search claim',async()=>{
 m.preference={isPending:false,isError:true}
 render(<ChatProvider><span>Desk</span></ChatProvider>)
 await waitFor(()=>expect(m.report.mock.lastCall?.[1].blocked).toContain('could not be read'))
})
it('keeps provided-sources-only chat usable without waiting for search',async()=>{
 m.snapshot.chats[0].researchMode='provided'
 render(<ChatProvider><span>Desk</span></ChatProvider>)
 await waitFor(()=>expect(m.report.mock.lastCall?.[1].blocked).toBe(''))
 expect(m.options.researchPolicy()).toContain('Use only sources supplied')
})
