import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import { cleanup, render as baseRender, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { INITIAL_STATE } from '../research/run'
import { deskFetch } from '../files/client'
import { ChatProvider } from './ChatProvider'

const render=(node:React.ReactNode)=>{const client=new QueryClient({defaultOptions:{queries:{retry:false}}});return baseRender(node,{wrapper:({children})=><QueryClientProvider client={client}>{children}</QueryClientProvider>})}

const m=vi.hoisted(()=>({preference:{} as any,connections:{} as any,catalog:{} as any,report:vi.fn(),update:vi.fn(),snapshot:{} as any,binding:{} as any,options:{} as any}))
vi.mock('../files/client', async original=>({...await original(), readFile:vi.fn(), deskFetch:vi.fn()}))
vi.mock('../files/queries',()=>({useFileListing:()=>({data:{root:'/fixture'}}),useFileContent:()=>({data:undefined})}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({config:{research:{gateway:{authority:'test'},documents:{enabled:true},sources:{}}},desk:{localGateway:{status:'ready'}}})}))
vi.mock('../connections/catalog',()=>({useConnections:()=>m.catalog}))
vi.mock('../search/connections',()=>({useSearchPreference:()=>m.preference,useSearchConnections:()=>m.connections}))
vi.mock('../assistant/serverRuns/useSavedChatRun',()=>({useSavedChatRun:(_store:unknown,chat:unknown)=>{m.options=chat;return m.binding}}))
vi.mock('./store',()=>({ChatStore:class {
 load=async()=>{};checkpoint=async()=>{};retain=()=>()=>{};subscribe=()=>()=>{};getSnapshot=()=>m.snapshot;report=m.report;update=m.update
}}))
beforeEach(()=>{
 m.report.mockClear();m.update.mockClear();vi.mocked(deskFetch).mockClear()
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
 expect(m.options).toBe(m.snapshot.chats[0])
 expect(deskFetch).not.toHaveBeenCalled()
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
 expect(m.options.researchMode).toBe('provided')
})

it('passes exact saved controls, linked source and Policy context as data to the canonical owner',async()=>{
 Object.assign(m.snapshot.chats[0],{researchMode:'provided',aiConnection:'ai-111111111111111111111111',model:'model-one',interaction:'plan',pack:{id:'one',path:'one.json',digest:'a'.repeat(64)},policySources:[{narrative:'Exact retained source'}],attachments:[{id:'selected',text:'Original file',name:'file.txt'}]})
 render(<ChatProvider><span>Desk</span></ChatProvider>)
 await waitFor(()=>expect(m.report).toHaveBeenCalled())
 expect(m.options).toBe(m.snapshot.chats[0]);expect(m.options).not.toHaveProperty('draftTools');expect(m.options).not.toHaveProperty('executionContext')
 expect(vi.mocked(deskFetch).mock.calls.every(([path])=>String(path).startsWith('/api/assistant-approvals/'))).toBe(true);expect(m.update).not.toHaveBeenCalled()
})
