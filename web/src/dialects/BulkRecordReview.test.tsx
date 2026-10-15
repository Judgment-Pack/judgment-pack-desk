import {cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter,Route,Routes} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {BulkRecordReview} from './BulkRecordReview'
import {StaleWrite} from '../files/client'
import {DialectsContent} from './DialectsView'
import {dialectAPI,type DialectRecord,type PreparationReply} from './client'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
vi.mock('../shell/UnsavedChanges',async original=>({...await original<typeof import('../shell/UnsavedChanges')>(),useConfirmDiscard:()=>()=>Promise.resolve(true)}))
const record=(id:string):DialectRecord=>({id,name:id,values:{},sourceId:'run',row:0,class:'generated',status:'researched',research:{runId:'run',recipeVersion:1,entity:{id,name:id,kind:'company',url:'https://example.com/',fields:{}}}})
const saved:PreparationReply={sha256:'first',document:{version:1,id:'one',name:'Research',recipe:{intent:'Find companies'},recipeVersion:1,createdAt:'2026-01-01',updatedAt:'2026-01-01',records:[record('First'),record('Second'),record('Unselected')],imports:[],activity:[]}}
const reviewed:PreparationReply={...saved,sha256:'reviewed',document:{...saved.document,records:saved.document.records.map(r=>r.id==='Unselected'?r:{...r,review:{recordId:r.id,recordDigest:'digest',recipeVersion:1,decision:'accepted',note:'Checked sources',at:'2026-10-12T19:00:00Z',reviewer:{kind:'local-access'}}})}}
let cache:QueryClient
beforeEach(()=>{vi.resetAllMocks();cache=new QueryClient({defaultOptions:{queries:{retry:false,staleTime:Infinity,refetchOnWindowFocus:false}}})})
afterEach(()=>{cleanup();cache.clear()})
const wrap=(node:React.ReactNode)=><QueryClientProvider client={cache}><Tooltip.Provider><MemoryRouter>{node}</MemoryRouter></Tooltip.Provider></QueryClientProvider>
const choose=()=>{fireEvent.keyDown(screen.getByRole('combobox',{name:'Review decision'}),{key:'ArrowDown'});fireEvent.click(screen.getByRole('option',{name:'Ready for evaluation'}))}
it('requires a new explicit decision and saves exactly the reviewed selection in one request',async()=>{
 vi.mocked(dialectAPI).mockResolvedValue(reviewed)
 const close=vi.fn(),onSaved=vi.fn().mockResolvedValue(undefined)
 render(wrap(<BulkRecordReview saved={saved} recordIds={['First','Second']} opener={{current:null}} onClose={close} onSaved={onSaved}/>))
 const save=screen.getByRole('button',{name:'Save 2 reviews'}) as HTMLButtonElement
 expect(save.disabled).toBe(true)
 fireEvent.click(screen.getByText('Selected records (2)'))
 expect(screen.getByText('First')).toBeTruthy();expect(screen.getByText('Second')).toBeTruthy();expect(screen.queryByText('Unselected')).toBeNull()
 choose();fireEvent.click(screen.getByRole('button',{name:'Add note'}));fireEvent.change(screen.getByRole('textbox',{name:'Review note'}),{target:{value:'Checked sources'}})
 fireEvent.click(save)
 await waitFor(()=>expect(close).toHaveBeenCalledOnce())
 expect(dialectAPI).toHaveBeenCalledExactlyOnceWith('/one/record-reviews','POST',{recordIds:['First','Second'],decision:'accepted',note:'Checked sources'},'first')
 expect(onSaved).toHaveBeenCalledWith(reviewed)
})
it('holds the reviewed revision and note when the recipe or selection changes',async()=>{
 const props={saved,recordIds:['First','Second'],opener:{current:null},onClose:vi.fn(),onSaved:vi.fn()}
 const view=render(wrap(<BulkRecordReview {...props}/>))
 choose();fireEvent.click(screen.getByRole('button',{name:'Add note'}));fireEvent.change(screen.getByRole('textbox',{name:'Review note'}),{target:{value:'My review'}})
 view.rerender(wrap(<BulkRecordReview {...props} saved={{...saved,sha256:'changed',document:{...saved.document,recipeVersion:2}}}/>))
 expect(screen.getByText('Dialects changed. Close this dialog and review the selection again.')).toBeTruthy()
 expect((screen.getByRole('button',{name:'Save 2 reviews'}) as HTMLButtonElement).disabled).toBe(true)
 expect((screen.getByRole('textbox',{name:'Review note'}) as HTMLTextAreaElement).value).toBe('My review')
 view.rerender(wrap(<BulkRecordReview {...props} recordIds={['First']}/>))
 expect((screen.getByRole('button',{name:'Save 2 reviews'}) as HTMLButtonElement).disabled).toBe(true)
 expect(dialectAPI).not.toHaveBeenCalled()
})
it('retains the decision and note on a refused save without reporting completion',async()=>{
 vi.mocked(dialectAPI).mockRejectedValue(new Error('Dialects changed. Reload before saving.'))
 const close=vi.fn(),onSaved=vi.fn()
 render(wrap(<BulkRecordReview saved={saved} recordIds={['First','Second']} opener={{current:null}} onClose={close} onSaved={onSaved}/>))
 choose();fireEvent.click(screen.getByRole('button',{name:'Add note'}));fireEvent.change(screen.getByRole('textbox',{name:'Review note'}),{target:{value:'Keep this'}})
 fireEvent.click(screen.getByRole('button',{name:'Save 2 reviews'}))
 await screen.findByText('Dialects changed. Reload before saving.')
 expect((screen.getByRole('textbox',{name:'Review note'}) as HTMLTextAreaElement).value).toBe('Keep this')
 expect(close).not.toHaveBeenCalled();expect(onSaved).not.toHaveBeenCalled()
})
it('updates all saved rows immediately, keeps unselected rows and enables the selected review flow',async()=>{
 let current=saved
 vi.mocked(dialectAPI).mockImplementation(async(path='',method)=>{
  if(method==='POST'){current=reviewed;return current as any}
  return path.endsWith('/research')||path.endsWith('/evaluations')?{items:[]} as any:current as any
 })
 render(<QueryClientProvider client={cache}><Tooltip.Provider><MemoryRouter initialEntries={['/dialects/one']}><Routes><Route path="/dialects/:preparationId" element={<DialectsContent/>}/></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 fireEvent.click(await screen.findByRole('checkbox',{name:'Select First'}));fireEvent.click(screen.getByRole('checkbox',{name:'Select Second'}))
 fireEvent.pointerDown(screen.getByRole('button',{name:'2 selected'}),{button:0,ctrlKey:false})
 fireEvent.click(await screen.findByRole('menuitem',{name:'Set review decision…'}))
 await screen.findByRole('dialog',{name:'Set review decision · 2 records'})
 choose();fireEvent.click(screen.getByRole('button',{name:'Save 2 reviews'}))
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 for(const name of ['First','Second'])expect(within(screen.getByRole('button',{name}).closest('tr')!).getByText('Ready for evaluation')).toBeTruthy()
 expect(within(screen.getByRole('button',{name:'Unselected'}).closest('tr')!).getByText('Research needs review')).toBeTruthy()
 fireEvent.pointerDown(screen.getByRole('button',{name:'2 selected'}),{button:0,ctrlKey:false})
 expect((await screen.findByRole('menuitem',{name:'Review records'})).getAttribute('aria-disabled')).not.toBe('true')
 expect(vi.mocked(dialectAPI).mock.calls.filter(([,method])=>method==='POST')).toEqual([['/one/record-reviews','POST',{recordIds:['First','Second'],decision:'accepted',note:''},'first']])
})

it('refreshes the current selection after a stale server refusal but never reuses that approval',async()=>{
 vi.mocked(dialectAPI).mockRejectedValue(new StaleWrite({code:'stale',error:'Dialects changed.'}))
 const invalidate=vi.spyOn(cache,'invalidateQueries'),close=vi.fn()
 render(wrap(<BulkRecordReview saved={saved} recordIds={['First','Second']} opener={{current:null}} onClose={close} onSaved={vi.fn()}/>))
 choose();fireEvent.click(screen.getByRole('button',{name:'Save 2 reviews'}))
 await screen.findByText('Dialects changed. Close this dialog and review the selection again.')
 expect((screen.getByRole('button',{name:'Save 2 reviews'}) as HTMLButtonElement).disabled).toBe(true)
 expect(invalidate).toHaveBeenCalledWith({queryKey:['input-preparations','one'],exact:true})
 expect(dialectAPI).toHaveBeenCalledTimes(1);expect(close).not.toHaveBeenCalled()
})
