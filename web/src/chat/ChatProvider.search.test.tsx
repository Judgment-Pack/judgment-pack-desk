import { cleanup, render, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { INITIAL_STATE } from '../research/run'
import { readFile, deskFetch } from '../files/client'
import type { HostTool } from '../assistant/engine'
import { ChatProvider } from './ChatProvider'

const m=vi.hoisted(()=>({preference:{} as any,connections:{} as any,catalog:{} as any,report:vi.fn(),update:vi.fn(),snapshot:{} as any,binding:{} as any,options:{} as any}))
vi.mock('../files/client', async original=>({...await original(), readFile:vi.fn(), deskFetch:vi.fn()}))
vi.mock('../files/queries',()=>({useFileListing:()=>({data:{root:'/fixture'}})}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({config:{research:{gateway:{authority:'test'},documents:{enabled:true},sources:{}}},desk:{localGateway:{status:'ready'}}})}))
vi.mock('../connections/catalog',()=>({useConnections:()=>m.catalog}))
vi.mock('../search/connections',()=>({useSearchPreference:()=>m.preference,useSearchConnections:()=>m.connections}))
vi.mock('../research/useResearchRun',()=>({useResearchRun:(options:unknown)=>{m.options=options;return m.binding}}))
vi.mock('./store',()=>({ChatStore:class {
 load=async()=>{};retain=()=>()=>{};subscribe=()=>()=>{};getSnapshot=()=>m.snapshot;report=m.report;update=m.update
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

it('offers graph proposals and saved rehearsals through the main conversation worker',async()=>{
 m.snapshot.chats[0].mode='draft';m.snapshot.chats[0].researchMode='provided';m.catalog.web=false
 vi.mocked(readFile).mockResolvedValue({path:'jpack.json',content:'{"packs":{"alpha":{"path":"alpha.json"}}}',sha256:'a'.repeat(64),bytes:45})
 render(<ChatProvider><span>Desk</span></ChatProvider>)
 await waitFor(()=>expect(m.options.draftTools).toBeTypeOf('function'))
 const tools:HostTool[]=m.options.draftTools({turns:()=>[]})
 expect(tools.map(tool=>tool.name)).toEqual(expect.arrayContaining(['list_decisions','read_decision','get_graph_authoring_instructions','graph_validate','graph_explain','propose_graph','graph_rehearse']))
 expect(tools.map(tool=>tool.name)).not.toContain('graph_write')
 const content='{"formatVersion":"1","id":"flow","version":"1.0.0","nodes":{"a":{"pack":"alpha"}},"edges":[],"result":"a"}'
 await tools.find(tool=>tool.name==='propose_graph')!.execute({id:'flow',content},new AbortController().signal)
 expect(m.update).toHaveBeenCalledWith('chat',{graphDrafts:[expect.objectContaining({id:'flow',content,path:'flow.graph.json'})]})
 expect(deskFetch).not.toHaveBeenCalled()
})
