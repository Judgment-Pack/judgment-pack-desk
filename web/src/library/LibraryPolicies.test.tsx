import {act,cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClientProvider} from '@tanstack/react-query'
import {createMemoryRouter,RouterProvider} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {PacksPane} from '../packs/PacksPane'
import {McpContext} from '../mcp/McpProvider'
import {connected,stubClient,testQueryClient} from '../testing/harness'
import {DeskNotices,DeskNoticesProvider} from '../shell/DeskNotices'
import {type PolicySummary} from '../policies/client'

const row:PolicySummary={id:'shared',title:'Travel policy',artifactVersion:'1.0.0',updatedAt:'2026-10-17T12:00:00Z',candidateDigest:'current-candidate',currentCandidatePublished:false,reviewState:'approved',publicationCount:1}
afterEach(()=>{cleanup();vi.unstubAllGlobals();localStorage.clear()})
function mount({path='/library',read=async()=>({items:[row]})}:{path?:string;read?:()=>Promise<{items:PolicySummary[]}>}={}){
 const fetch=vi.fn(async(input:RequestInfo|URL,init?:RequestInit)=>{
  if(init?.method&&init.method!=='GET')throw new Error('Library must not mutate policy storage.')
  if(String(input)==='/api/policies')return Response.json(await read())
  if(String(input)==='/api/input-preparations'||String(input)==='/api/dialect-drafts')return Response.json({items:[]})
  throw new Error('Unexpected request: '+input)
 });vi.stubGlobal('fetch',fetch)
 const stub=stubClient({list_packs:()=>({text:JSON.stringify({status:'valid',packs:[{id:'shared',packVersion:'1.0.0'}]})}),experimental_list_graphs:()=>({text:JSON.stringify({status:'valid',graphs:[]})})})
 const cache=testQueryClient(),router=createMemoryRouter([{path:'*',element:<McpContext.Provider value={connected({client:stub.client,graphInventorySupported:true})}><DeskNoticesProvider><PacksPane/><footer><DeskNotices/></footer></DeskNoticesProvider></McpContext.Provider>}],{initialEntries:[path]})
 render(<QueryClientProvider client={cache}><RouterProvider router={router}/></QueryClientProvider>);return {cache,router,fetch}
}
it('discovers Policies beside equal pack identities, with truthful draft/review and retained publication state',async()=>{
 const app=mount();const link=await screen.findByRole('link',{name:/Travel policy/})
 expect(link.getAttribute('href')).toBe('/policies/shared')
 expect(within(link).getByText('Draft · Approved')).toBeTruthy()
 expect(within(link).getByText('Publications: 1')).toBeTruthy()
 expect(screen.getByRole('link',{name:/shared/}).getAttribute('href')).toBe('/packs/shared')
 fireEvent.click(screen.getByRole('checkbox',{name:'Select Travel policy'}))
 expect((screen.getByRole('checkbox',{name:'Select shared'}) as HTMLInputElement).checked).toBe(false)
 expect(screen.queryByRole('button',{name:'Options for Travel policy'})).toBeNull()
 fireEvent.keyDown(screen.getByRole('button',{name:'Item type'}),{key:'Enter'})
 fireEvent.click(await screen.findByRole('menuitemradio',{name:'Policies'}))
 await waitFor(()=>expect(app.router.state.location.search).toContain('type=policy'))
 expect(screen.queryByRole('link',{name:/shared/})).toBeNull()
 expect(screen.getByRole('link',{name:/Travel policy/})).toBeTruthy()
 fireEvent.keyDown(screen.getByRole('button',{name:'Create'}),{key:'Enter'})
 expect((await screen.findByRole('menuitem',{name:'New policy'})).getAttribute('href')).toBe('/policies/new')
 expect(app.fetch.mock.calls.every(([,init])=>!init?.method||init.method==='GET')).toBe(true)
})
it('keeps saved Policies visible after a refresh failure and avoids claiming an unavailable index is empty',async()=>{
 let fail=false
 const app=mount({path:'/library?type=policy',read:async()=>{if(fail)throw new Error('Fixture private storage unavailable');return {items:[row]}}})
 await screen.findByRole('link',{name:/Travel policy/});fail=true
 await act(()=>app.cache.invalidateQueries({queryKey:['policies']}))
 await screen.findByRole('button',{name:'Policies unavailable'})
 expect(screen.getByRole('link',{name:/Travel policy/})).toBeTruthy()
 expect(screen.queryByText('No matching library items')).toBeNull()
})
it('shows Policy loading until its private index resolves, including exact current publication status',async()=>{
 let resolve!:(value:{items:PolicySummary[]})=>void
 const pending=new Promise<{items:PolicySummary[]}>(done=>{resolve=done})
 mount({path:'/library?type=policy',read:()=>pending})
 await screen.findByText('Loading library…')
 expect(screen.queryByText('No matching library items')).toBeNull()
 await act(async()=>resolve({items:[{...row,currentCandidatePublished:true}]}))
 const link=await screen.findByRole('link',{name:/Travel policy/})
 expect(within(link).getByText('Published · Approved')).toBeTruthy()
})
