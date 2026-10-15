import {act,cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {DeskNotices,DeskNoticesProvider} from '../shell/DeskNotices'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter,Route,Routes} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {afterEach,expect,it,vi} from 'vitest'
import {DialectsContent} from './DialectsView'
import {setLanguage,languageReady,msg} from '../i18n'
import {dialectAPI,type PreparationReply} from './client'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
const saved:PreparationReply={sha256:'original',document:{version:1,id:'one',name:'Suppliers',recipe:{intent:'Original instructions'},recipeVersion:1,createdAt:'2026-01-01',updatedAt:'2026-01-01',records:[],imports:[],activity:[]}}
function show(){
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 vi.mocked(dialectAPI).mockResolvedValue(saved)
 render(<QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter initialEntries={['/dialects/one?tab=recipe']}><Routes><Route path="/dialects/:preparationId" element={<DialectsContent/>}/></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 return client
}
it('keeps unsaved instructions and their starting revision when a background refresh finds an edit',async()=>{
 const client=show(),field=await screen.findByRole('textbox',{name:'What records do you need?'})
 fireEvent.change(field,{target:{value:'My unsaved instructions'}})
 act(()=>client.setQueryData(['input-preparations','one'],{...saved,sha256:'concurrent',document:{...saved.document,recipeVersion:2,recipe:{intent:'Other editor'}}}))
 expect((field as HTMLTextAreaElement).value).toBe('My unsaved instructions')
 vi.mocked(dialectAPI).mockRejectedValue(Error('Dialects changed. Reload before saving.'))
 fireEvent.click(screen.getByRole('button',{name:'Save recipe'}))
 await screen.findByText('Dialects changed. Reload before saving.')
 expect(vi.mocked(dialectAPI).mock.calls.at(-1)).toEqual(['/one','PUT',{name:'Suppliers',recipe:{intent:'My unsaved instructions'}},'original'])
 expect((field as HTMLTextAreaElement).value).toBe('My unsaved instructions')
})
it('adopts a newer saved recipe when the form has no pending edits',async()=>{
 const client=show(),field=await screen.findByRole('textbox',{name:'What records do you need?'})
 act(()=>client.setQueryData(['input-preparations','one'],{...saved,sha256:'updated',document:{...saved.document,recipeVersion:2,recipe:{intent:'Updated instructions'}}}))
 await waitFor(()=>expect((field as HTMLTextAreaElement).value).toBe('Updated instructions'))
 expect((screen.getByRole('button',{name:'Save recipe'}) as HTMLButtonElement).disabled).toBe(true)
})

it('updates collection dates with Desk language and preserves rows with an invalid timestamp',async()=>{
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 vi.mocked(dialectAPI).mockResolvedValue({items:[{id:'one',name:'Suppliers',records:12,updatedAt:'2026-01-01T12:00:00Z'},{id:'two',name:'Missing date',records:0,updatedAt:'invalid'}]})
 render(<QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter initialEntries={['/dialects']}><Routes><Route path="/dialects" element={<DialectsContent/>}/></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 const link=await screen.findByRole('link',{name:'Suppliers'})
 const updated=link.closest('tr')!.lastElementChild!
 const before=updated.textContent
 try{
  await act(async()=>{setLanguage('fr');await languageReady()})
  expect(updated.textContent).not.toBe(before)
  expect(updated.textContent).toContain('janv.')
  expect(screen.getByRole('link',{name:'Missing date'}).closest('tr')!.lastElementChild!.textContent).toBe(msg('Not recorded'))
 }finally{await act(async()=>{setLanguage('en');await languageReady()})}
})

it('groups read failures in the strip, preserves cached records, and clears each notice after recovery',async()=>{
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 const content:PreparationReply={...saved,document:{...saved.document,records:[{id:'r1',name:'Company one',values:{},sourceId:'s1',row:1,class:'generated',status:'researched'}]}}
 client.setQueryData(['input-preparations','one'],content)
 let inputFailed=false,researchFailed=true,evaluationsFailed=true
 vi.mocked(dialectAPI).mockImplementation(async(path)=>{
  if(path==='/one/research'){if(researchFailed)throw Error('research-read-failed');return {items:[]} as never}
  if(path==='/one/evaluations'){if(evaluationsFailed)throw Error('evaluation-read-failed');return {items:[]} as never}
  if(inputFailed)throw Error('input-refresh-failed')
  return content as never
 })
 render(<QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter initialEntries={['/dialects/one']}><DeskNoticesProvider><main><Routes><Route path="/dialects/:preparationId" element={<DialectsContent/>}/></Routes></main><footer><DeskNotices/></footer></DeskNoticesProvider></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 const trigger=await screen.findByRole('button',{name:/Research status unavailable · 2 notices/})
 const record=screen.getByRole('button',{name:'Company one'})
 expect(record.closest('tr')?.textContent).toContain('Unavailable')
 expect(within(screen.getByRole('main')).queryByText(/could not be/)).toBeNull()
 fireEvent.click(trigger)
 let panel=screen.getByRole('dialog',{name:'Desk notices'})
 expect(within(panel).getByText('Research status could not be refreshed. The last known status is shown; the run may still be active.')).toBeTruthy()
 expect(within(panel).getByText('Evaluation history could not be loaded. Existing results are not changed.')).toBeTruthy()
 researchFailed=false
 fireEvent.click(within(panel).getAllByRole('button',{name:'Retry'})[0]!)
 await waitFor(()=>expect(screen.queryByText(/Research status could not be refreshed/)).toBeNull())
 expect(screen.getByRole('button',{name:'Company one'})).toBeTruthy()
 evaluationsFailed=false
 panel=screen.getByRole('dialog')
 fireEvent.click(within(panel).getByRole('button',{name:'Retry'}))
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 expect(record.closest('tr')?.textContent).toContain('Not run')
 expect(vi.mocked(dialectAPI).mock.calls.every(call=>!call[1]||call[1]==='GET')).toBe(true)
 inputFailed=true
 await act(async()=>{await client.invalidateQueries({queryKey:['input-preparations','one']})})
 expect(await screen.findByRole('button',{name:'Dialect unavailable'})).toBeTruthy()
 expect(screen.getByRole('button',{name:'Company one'})).toBeTruthy()
})

it('reprocesses exactly the checked rows from the selection menu and freezes the reviewed snapshot',async()=>{
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 const recipe={version:1 as const,target:'companies' as const,fields:[{name:'country',description:'Country',type:'string' as const}],criteria:[],limits:{records:10,searches:3,pages:6,modelCalls:15},ai:{connection:'ai',revision:'rev',model:'model'}}
 const content:PreparationReply={...saved,document:{...saved.document,recipe:{...saved.document.recipe,research:recipe},records:['one','two','three'].map(name=>({id:'row-'+name,name:'Company '+name,values:{},sourceId:'research-old',row:1,class:'generated' as const,status:'researched',research:{runId:'research-old',recipeVersion:1,entity:{id:name,kind:'company' as const,name:'Company '+name,url:'https://example.com/',fields:{country:{status:'missing' as const}}}}}))}}
 const reviewed={id:'reviewed',state:'review',reprocess:true,recipe:{...recipe,limits:{...recipe.limits,records:2,searches:0,pages:0}},recipeVersion:1,selection:content.document.records.slice(0,2).map(row=>({recordId:row.id,entity:row.research!.entity})),records:[],sources:[],companies:[],attempts:[]}
 vi.mocked(dialectAPI).mockImplementation(async(path,method)=>{
  if(path==='/one/research'&&method==='POST')return reviewed as never
  if(path==='/one/research'||path==='/one/evaluations')return {items:[]} as never
  if(path==='/one/research/reviewed/confirm')return {...reviewed,state:'queued'} as never
  return content as never
 })
 render(<QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter initialEntries={['/dialects/one']}><Routes><Route path="/dialects/:preparationId" element={<DialectsContent/>}/></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 await screen.findByRole('button',{name:'Company one'})
 fireEvent.click(screen.getByRole('checkbox',{name:'Select Company one'}));fireEvent.click(screen.getByRole('checkbox',{name:'Select Company two'}))
 fireEvent.keyDown(screen.getByRole('button',{name:'2 selected'}),{key:'ArrowDown'})
 fireEvent.click(await screen.findByRole('menuitem',{name:'Reprocess from saved sources…'}))
 const dialog=await screen.findByRole('dialog',{name:'Reprocess 2 records'})
 await within(dialog).findByText('Current saved recipe · Version 1')
 expect(dialectAPI).toHaveBeenCalledWith('/one/research','POST',{recordIds:['row-one','row-two']},'original')
 expect(vi.mocked(dialectAPI).mock.calls.some(([path])=>path?.endsWith('/confirm'))).toBe(false)
 await act(async()=>{client.setQueryData(['input-preparations','one'],{...content,sha256:'newer'})})
 fireEvent.click(within(dialog).getByRole('button',{name:'Reprocess 2 records'}))
 await waitFor(()=>expect(dialectAPI).toHaveBeenCalledWith('/one/research/reviewed/confirm','POST',{}))
 expect(vi.mocked(dialectAPI).mock.calls.filter(([,method])=>method==='POST')).toHaveLength(2)
})

it('refreshes a cached empty collection when returning from a chat or another window',async()=>{
 const client=new QueryClient({defaultOptions:{queries:{staleTime:Infinity,retry:false,refetchOnWindowFocus:false}}})
 client.setQueryData(['input-preparations'],{items:[]})
 vi.mocked(dialectAPI).mockResolvedValue({items:[{id:'new',name:'Saved in another window',records:2,updatedAt:'2026-10-14'}]})
 render(<QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter initialEntries={['/dialects']}><Routes><Route path="/dialects" element={<DialectsContent/>}/></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 expect(await screen.findByRole('link',{name:'Saved in another window'})).toBeTruthy()
})

it('distinguishes retained research sources from saved records and shows why none were saved',async()=>{
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 vi.mocked(dialectAPI).mockImplementation(async(path)=>{
  if(path==='/one/research')return {items:[{id:'run',state:'completed-with-gaps',updatedAt:'2026-10-14',recordCount:0,sourceCount:17,stopRequested:false,explanation:'The notices were closed, not open opportunities.'}]} as never
  if(path==='/one/evaluations')return {items:[]} as never
  return saved as never
 })
 render(<QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter initialEntries={['/dialects/one']}><Routes><Route path="/dialects/:preparationId" element={<DialectsContent/>}/></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 expect(await screen.findByText('No records saved')).toBeTruthy()
 expect(screen.getByText('17 sources retained. Sources are research material, not saved records.')).toBeTruthy()
 expect(screen.getByText('AI explanation: The notices were closed, not open opportunities.')).toBeTruthy()
 expect(screen.getByRole('button',{name:'View steps'})).toBeTruthy()
})
