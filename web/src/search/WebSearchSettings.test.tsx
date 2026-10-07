import { afterEach,expect,it,vi } from 'vitest'
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { testQueryClient } from '../testing/harness'
import { WebSearchSettings } from './WebSearchSettings'
const config=vi.hoisted(()=>({timeout:undefined as undefined|{defaultSeconds:number;minSeconds:number;maxSeconds:number}}))
const preference=vi.hoisted(()=>({data:{value:{version:1,connection:null,mode:'auto'}},save:vi.fn(async()=>{}),refetch:vi.fn(),saving:false,isError:false,saveError:null as Error|null}))
const call=vi.hoisted(()=>vi.fn(async()=>({saved:true})))
vi.mock('../connections/client',async original=>({...await original<typeof import('../connections/client')>(),connectionCall:call}))
vi.mock('../shell/InspectorPresentation',()=>({useInspectorPresentation:()=>{}}))
vi.mock('../shell/InspectorSlot',()=>({useInspectorPortal:(node:unknown)=>node}))
vi.mock('./connections',()=>({SEARCH_CONNECTIONS_KEY:['web-search-connections'],useSearchPreference:()=>preference,useSearchConnections:()=>({available:true,data:{version:1,providers:[{id:'tavily',name:'Tavily',fields:['api-key'],docs:'https://docs.tavily.com'},{id:'google-grounding',name:'Google Cloud',fields:['project','location','model','service-account-json'],docs:'https://cloud.google.com'}],connections:[],timeout:config.timeout}})}))
afterEach(()=>{cleanup();vi.clearAllMocks();config.timeout=undefined;preference.saving=false;preference.isError=false;preference.saveError=null})
const settings=()=> <MemoryRouter><QueryClientProvider client={testQueryClient()}><WebSearchSettings/></QueryClientProvider></MemoryRouter>
it('saves a named provider through Gateway and clears its credential when the pane closes',async()=>{
 render(settings())
 fireEvent.click(screen.getByRole('button',{name:'Add connection'}))
 fireEvent.change(screen.getByLabelText('Name'),{target:{value:'Public research'}})
 fireEvent.change(screen.getByLabelText('API key'),{target:{value:'test-only-secret'}})
 fireEvent.click(screen.getByRole('button',{name:'Save'}))
 await waitFor(()=>expect(call).toHaveBeenCalledTimes(1))
 expect(call.mock.calls[0]).toEqual(['configure',expect.objectContaining({name:'Public research',provider:'tavily',credential:'test-only-secret',dailyLimit:100}),undefined,'web-search'])
 await waitFor(()=>expect(screen.queryByLabelText('API key')).toBeNull())
 expect(document.body.textContent).not.toContain('test-only-secret')
 expect((call.mock.calls[0] as unknown as [string,Record<string,unknown>])[1]).not.toHaveProperty('timeoutSeconds')
})

it('shows supported timeout bounds and sends the chosen value only after save',async()=>{
 config.timeout={defaultSeconds:45,minSeconds:10,maxSeconds:120}
 render(settings())
 fireEvent.click(screen.getByRole('button',{name:'Add connection'}))
 fireEvent.click(screen.getByText('Advanced settings'))
 const timeout=screen.getByLabelText('Search timeout (seconds)') as HTMLInputElement
 expect(timeout.value).toBe('45');expect(timeout.max).toBe('120');expect(timeout.min).toBe('10')
 fireEvent.change(timeout,{target:{value:'121'}});expect(timeout.validity.rangeOverflow).toBe(true)
 fireEvent.change(timeout,{target:{value:'75'}})
 fireEvent.change(screen.getByLabelText('Name'),{target:{value:'Public search'}})
 fireEvent.change(screen.getByLabelText('API key'),{target:{value:'test-only-secret'}})
 expect(call).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Save'}))
 await waitFor(()=>expect(call).toHaveBeenCalledWith('configure',expect.objectContaining({timeoutSeconds:75}),undefined,'web-search'))
})

it('sends a timeout at either advertised bound and refuses one past it before anything is sent',async()=>{
 config.timeout={defaultSeconds:45,minSeconds:10,maxSeconds:120}
 for(const [value,sent] of [['120',true],['10',true],['121',false],['9',false],['45.5',false]] as const){
  const view=render(settings())
  fireEvent.click(screen.getByRole('button',{name:'Add connection'}))
  fireEvent.click(screen.getByText('Advanced settings'))
  fireEvent.change(screen.getByLabelText('Name'),{target:{value:'Bounded search'}})
  fireEvent.change(screen.getByLabelText('Search timeout (seconds)'),{target:{value}})
  fireEvent.submit(screen.getByRole('button',{name:'Save'}).closest('form')!)
  if(sent)await waitFor(()=>expect(call).toHaveBeenCalledWith('configure',expect.objectContaining({timeoutSeconds:Number(value)}),undefined,'web-search'))
  else{
   expect((await screen.findByRole('alert')).textContent).toBe('Choose a search timeout from 10 to 120 seconds.')
   expect(call).not.toHaveBeenCalled()
  }
  view.unmount();call.mockClear()
 }
})

it('distinguishes unread settings from an unsuccessful save and offers reload',()=>{
 preference.isError=true
 const view=render(settings())
 expect(screen.getByRole('alert').textContent).toContain('Search settings could not be read.')
 fireEvent.click(screen.getByRole('button',{name:'Reload'}));expect(preference.refetch).toHaveBeenCalledTimes(1)
 preference.isError=false;preference.saveError=new Error('conflict');view.rerender(settings())
 expect(screen.getByRole('alert').textContent).toContain('Search settings could not be saved.')
})
