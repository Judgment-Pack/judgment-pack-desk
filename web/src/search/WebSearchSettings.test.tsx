import { afterEach,expect,it,vi } from 'vitest'
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { testQueryClient } from '../testing/harness'
import { WebSearchSettings } from './WebSearchSettings'
const call=vi.hoisted(()=>vi.fn(async()=>({saved:true})))
vi.mock('../connections/client',()=>({connectionCall:call}))
vi.mock('../shell/InspectorPresentation',()=>({useInspectorPresentation:()=>{}}))
vi.mock('../shell/InspectorSlot',()=>({useInspectorPortal:(node:unknown)=>node}))
vi.mock('./connections',()=>({SEARCH_CONNECTIONS_KEY:['web-search-connections'],useSearchPreference:()=>({data:{value:{version:1,connection:null,mode:'auto'}},save:vi.fn()}),useSearchConnections:()=>({available:true,data:{version:1,providers:[{id:'tavily',name:'Tavily',fields:['api-key'],docs:'https://docs.tavily.com'},{id:'google-grounding',name:'Google Cloud',fields:['project','location','model','service-account-json'],docs:'https://cloud.google.com'}],connections:[]}})}))
afterEach(()=>{cleanup();vi.clearAllMocks()})
it('saves a named provider through Gateway and clears its credential when the pane closes',async()=>{
 render(<MemoryRouter><QueryClientProvider client={testQueryClient()}><WebSearchSettings/></QueryClientProvider></MemoryRouter>)
 fireEvent.click(screen.getByRole('button',{name:'Add connection'}))
 fireEvent.change(screen.getByLabelText('Name'),{target:{value:'Public research'}})
 fireEvent.change(screen.getByLabelText('API key'),{target:{value:'test-only-secret'}})
 fireEvent.click(screen.getByRole('button',{name:'Save'}))
 await waitFor(()=>expect(call).toHaveBeenCalledTimes(1))
 expect(call.mock.calls[0]).toEqual(['configure',expect.objectContaining({name:'Public research',provider:'tavily',credential:'test-only-secret',dailyLimit:100}),undefined,'web-search'])
 await waitFor(()=>expect(screen.queryByLabelText('API key')).toBeNull())
 expect(document.body.textContent).not.toContain('test-only-secret')
})
