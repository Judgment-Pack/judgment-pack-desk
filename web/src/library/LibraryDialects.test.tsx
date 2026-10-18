import {act, cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react'
import {QueryClientProvider} from '@tanstack/react-query'
import {createMemoryRouter, RouterProvider} from 'react-router-dom'
import {afterEach, expect, it, vi} from 'vitest'
import {PacksPane} from '../packs/PacksPane'
import {PacksLayout} from '../routes/PacksLayout'
import {McpContext} from '../mcp/McpProvider'
import {connected, stubClient, testQueryClient} from '../testing/harness'
import {DeskNotices, DeskNoticesProvider} from '../shell/DeskNotices'
import {FOLDERS_KEY} from '../packs/folders/client'
import {applyFolderAction, defaultFolders, HOME_FOLDER} from '../packs/folders/model'

afterEach(()=>{cleanup();vi.unstubAllGlobals();localStorage.clear()})
const dialect={id:'shared',name:'Vendor records',records:2,updatedAt:'2026-10-17T12:00:00Z'}
const draft={id:'draft_one',name:'Refund mapping',updatedAt:'2026-10-17T12:00:00Z'}
function mount({path='/library',folders=false,draftItems=[draft],read=async()=>({items:[dialect]})}:{path?:string;folders?:boolean;draftItems?:typeof draft[];read?:()=>Promise<{items:typeof dialect[]}>}={}) {
 const document=applyFolderAction(defaultFolders(),{type:'create',id:'finance',name:'Finance',parentId:HOME_FOLDER})
 const fetch=vi.fn(async(input:RequestInfo|URL,init?:RequestInit)=>{
  if(init?.method&&init.method!=='GET')throw new Error('The index cannot write fixture storage.')
  const url=String(input)
  if(url==='/api/input-preparations')return Response.json(await read())
  if(url==='/api/dialect-drafts')return Response.json({items:draftItems})
  if(url==='/api/files')return Response.json({root:'/fixture',files:[]})
  if(url.startsWith('/api/file?'))return Response.json({path:'jpack-folders.json',content:JSON.stringify(document),sha256:'folders',bytes:100})
  throw new Error('Unexpected fixture request: '+url)
 })
 vi.stubGlobal('fetch',fetch)
 const client=testQueryClient()
 if(folders)client.setQueryData(FOLDERS_KEY,{document,digest:'folders'})
 const stub=stubClient({list_packs:()=>({text:JSON.stringify({status:'valid',packs:[{id:'shared',packVersion:'1.0.0'}]})}),experimental_list_graphs:()=>({text:JSON.stringify({status:'valid',graphs:[{id:'flow',graphVersion:'1.0.0'}]})})})
 const element=<McpContext.Provider value={connected({client:stub.client,graphInventorySupported:true})}><DeskNoticesProvider>{folders?<PacksLayout/>:<PacksPane/>}<footer><DeskNotices/></footer></DeskNoticesProvider></McpContext.Provider>
 const router=createMemoryRouter([{path:'*',element}],{initialEntries:[path]})
 render(<QueryClientProvider client={client}><RouterProvider router={router}/></QueryClientProvider>)
 return {client,router,fetch,document}
}
it('discovers every current kind without conflating equal artifact IDs or relocating private work',async()=>{
 const {router,fetch}=mount()
 const dialectLink=await screen.findByRole('link',{name:/Vendor records/})
 expect(dialectLink.getAttribute('href')).toBe('/dialects/shared')
 expect((await screen.findByRole('link',{name:/Refund mapping/})).getAttribute('href')).toBe('/dialects/drafts/draft_one')
 expect(screen.getByRole('link',{name:/shared/}).getAttribute('href')).toBe('/packs/shared')
 expect(screen.getByRole('link',{name:/flow/}).getAttribute('href')).toBe('/graphs/flow')
 fireEvent.click(screen.getByRole('checkbox',{name:'Select Vendor records'}))
 expect((screen.getByRole('checkbox',{name:'Select shared'}) as HTMLInputElement).checked).toBe(false)
 fireEvent.click(screen.getByRole('checkbox',{name:'Select shared'}))
 expect(screen.getByText('2 selected')).toBeTruthy()
 fireEvent.keyDown(screen.getByRole('button',{name:'Item type'}),{key:'Enter'})
 fireEvent.click(await screen.findByRole('menuitemradio',{name:'Dialects'}))
 await waitFor(()=>expect(router.state.location.search).toContain('type=dialect'))
 expect(screen.queryByRole('link',{name:/shared/})).toBeNull()
 expect(screen.queryByRole('link',{name:/flow/})).toBeNull()
 expect(screen.getByRole('link',{name:/Refund mapping/})).toBeTruthy()
 expect(screen.getByText('1 selected')).toBeTruthy()
 expect(fetch.mock.calls.every(([,init])=>!init||(init as RequestInit).method===undefined||(init as RequestInit).method==='GET')).toBe(true)
})
it('retains cached dialects on failed refresh and distinguishes missing data from an empty collection',async()=>{
 let fail=false
 const {client}=mount({path:'/library?type=dialect',read:async()=>{if(fail)throw new Error('Fixture storage unavailable');return {items:[dialect]}}})
 await screen.findByRole('link',{name:/Vendor records/})
 fail=true
 await act(()=>client.invalidateQueries({queryKey:['input-preparations']}))
 expect(screen.getByRole('link',{name:/Vendor records/})).toBeTruthy()
 expect(await screen.findByRole('button',{name:'Dialects unavailable'})).toBeTruthy()
 expect(screen.queryByText('No matching library items')).toBeNull()
})
it('keeps loading visible until the private index resolves',async()=>{
 let resolve!:(value:{items:typeof dialect[]})=>void
 const pending=new Promise<{items:typeof dialect[]}>(done=>{resolve=done})
 mount({path:'/library?type=dialect',draftItems:[],read:()=>pending})
 await screen.findByText('Loading library…')
 expect(screen.queryByText('No matching library items')).toBeNull()
 await act(async()=>resolve({items:[dialect]}))
 await screen.findByRole('link',{name:/Vendor records/})
})
it('shows unassigned dialects in Home/all without using legacy pack folder assignments',async()=>{
 const {router,client,document}=mount({path:'/library?type=dialect',folders:true})
 await screen.findByRole('link',{name:/Vendor records/})
 expect(screen.queryByRole('button',{name:'Options for Vendor records'})).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Expand folders'}))
 const folders=screen.getByRole('navigation',{name:'Folder navigation'})
 fireEvent.click(within(folders).getByRole('button',{name:'Finance'}))
 await waitFor(()=>expect(screen.queryByRole('link',{name:/Vendor records/})).toBeNull())
 expect(router.state.location.search).toContain('type=dialect')
 await act(()=>router.navigate('/library?folder=all&type=dialect'))
 await screen.findByRole('link',{name:/Vendor records/})
 expect(client.getQueryData<{document:unknown}>(FOLDERS_KEY)?.document).toEqual(document)
 await act(()=>router.navigate('/library'))
 expect(screen.getByRole('button',{name:'Item type'}).textContent).toContain('Dialects')
 expect(screen.queryByRole('link',{name:/shared/})).toBeNull()
})
it('reports a failed initial dialect listing without claiming the Library is empty',async()=>{
 mount({path:'/library?type=dialect',draftItems:[],read:async()=>{throw new Error('Fixture unavailable')}})
 await screen.findByRole('button',{name:'Dialects unavailable'})
 expect(screen.queryByText('No matching library items')).toBeNull()
 expect(screen.queryByText('Loading library…')).toBeNull()
})
it('filters saved and draft Dialects without claiming approval or publication',async()=>{
 mount({path:'/library?type=dialect'})
 const saved=await screen.findByRole('link',{name:/Vendor records/})
 expect(within(saved).getByText('Ready to review')).toBeTruthy()
 expect(within(await screen.findByRole('link',{name:/Refund mapping/})).getByText('Draft')).toBeTruthy()
 fireEvent.keyDown(screen.getByRole('button',{name:'Status'}),{key:'Enter'})
 fireEvent.click(await screen.findByRole('menuitemradio',{name:'Saved'}))
 expect(screen.getByRole('link',{name:/Vendor records/})).toBeTruthy()
 expect(screen.queryByRole('link',{name:/Refund mapping/})).toBeNull()
 expect(screen.queryByText(/approved|published|finalized/i)).toBeNull()
 fireEvent.keyDown(screen.getByRole('button',{name:'Status'}),{key:'Enter'})
 fireEvent.click(await screen.findByRole('menuitemradio',{name:'Draft'}))
 expect(screen.queryByRole('link',{name:/Vendor records/})).toBeNull()
 expect(screen.getByRole('link',{name:/Refund mapping/})).toBeTruthy()
})
