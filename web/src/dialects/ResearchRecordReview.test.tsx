import {useState} from 'react'
import {act,cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter,Route,Routes} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {afterEach,expect,it,vi} from 'vitest'
import {ResearchRecordReview} from './ResearchRecordReview'
import {DialectsContent} from './DialectsView'
import {dialectAPI,exportDialect,type DialectRecord,type PreparationReply,type RecordReview} from './client'
import {currentRecordReview,researchReviewLabel,recordsGuidance} from './recordReview'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
vi.mock('../shell/UnsavedChanges',async original=>({...await original<typeof import('../shell/UnsavedChanges')>(),useConfirmDiscard:()=>()=>Promise.resolve(true)}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
const record={id:'row',name:'Example',values:{country:'Canada'},sourceId:'run',row:0,class:'generated' as const,status:'researched' as const,research:{runId:'run',recipeVersion:1,entity:{id:'entity',name:'Example',kind:'company' as const,url:'https://example.com/',fields:{country:{value:'Canada',status:'supported' as const,sources:[{sourceId:'source',quote:'Located in Canada.'}]}}}}}
const saved:PreparationReply={sha256:'first',document:{version:1,id:'one',name:'Research',recipe:{intent:'Find companies'},recipeVersion:1,createdAt:'2026-01-01',updatedAt:'2026-01-01',records:[record],imports:[],activity:[]}}
const review:RecordReview={recordId:'row',recordDigest:'digest',recipeVersion:1,decision:'accepted',note:'Worth checking',at:'2026-10-12T14:00:00Z',reviewer:{kind:'session',subject:'local user'}}
const reviewed:PreparationReply={sha256:'reviewed',document:{...saved.document,records:[{...record,review}],reviews:[{review,record}]}}
const sources={sources:[{id:'source',url:'https://example.com/',at:'2026-10-12T13:00:00Z'}],companies:[]}
function wrap(children:React.ReactNode){return <QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Tooltip.Provider><MemoryRouter>{children}</MemoryRouter></Tooltip.Provider></QueryClientProvider>}
function mock(){vi.mocked(dialectAPI).mockImplementation(async(path='',method)=>method==='POST'?reviewed as any:path==='/one/research/run'?sources as any:path==='/one/research'||path==='/one/evaluations'?{items:[]} as any:saved as any)}
function choose(){fireEvent.keyDown(screen.getByRole('combobox',{name:'Review decision'}),{key:'ArrowDown'});fireEvent.click(screen.getByRole('option',{name:'Ready for evaluation'}))}
it('requires an explicit choice, retains quotes, saves against the reviewed revision and does not evaluate',async()=>{
 mock();const onSaved=vi.fn(),onClose=vi.fn()
 function Harness(){const [current,setCurrent]=useState(saved);return <ResearchRecordReview saved={current} recordIds={['row']} opener={{current:null}} onClose={onClose} onReload={async()=>current} onSaved={async next=>{onSaved(next);setCurrent(next)}}/>}
 render(wrap(<Harness/>))
 const save=screen.getByRole('button',{name:'Save review'}) as HTMLButtonElement
 expect(save.disabled).toBe(true);expect(screen.getByText('Choose a review decision')).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Details for Country'}));await screen.findByText('Located in Canada.')
 choose();fireEvent.click(screen.getByRole('button',{name:'Add note'}));fireEvent.change(screen.getByRole('textbox',{name:'Review note'}),{target:{value:'Worth checking'}});fireEvent.click(save)
 await screen.findByText('Review saved')
 expect(dialectAPI).toHaveBeenCalledWith('/one/record-reviews','POST',{recordId:'row',decision:'accepted',note:'Worth checking'},'first')
 expect(onSaved).toHaveBeenCalledWith(reviewed);expect(save.disabled).toBe(true)
 expect(vi.mocked(dialectAPI).mock.calls.filter(([,method])=>method==='POST')).toHaveLength(1)
 expect(record.class).toBe('generated');expect(record.research.entity.fields.country.sources[0].quote).toBe('Located in Canada.')
 fireEvent.click(screen.getByRole('button',{name:'Back to records'}));await waitFor(()=>expect(onClose).toHaveBeenCalledOnce())
})
it('refuses a stale UI review without overwriting unsaved notes and reloads only on request',async()=>{
 mock();const onReload=vi.fn().mockResolvedValue({...saved,sha256:'new',document:{...saved.document,recipeVersion:2}})
 let update!:(next:PreparationReply)=>void
 function Harness(){const[current,setCurrent]=useState(saved);update=setCurrent;return <ResearchRecordReview saved={current} recordIds={['row']} opener={{current:null}} onClose={vi.fn()} onSaved={vi.fn()} onReload={onReload}/>}
 render(wrap(<Harness/>));choose();fireEvent.click(screen.getByRole('button',{name:'Add note'}));fireEvent.change(screen.getByRole('textbox',{name:'Review note'}),{target:{value:'Unsaved note'}})
 act(()=>update({...saved,sha256:'new'}))
 expect(screen.getByText('Dialects changed. Reload the record before saving your review.')).toBeTruthy()
 expect((screen.getByRole('button',{name:'Save review'}) as HTMLButtonElement).disabled).toBe(true)
 expect((screen.getByRole('textbox',{name:'Review note'}) as HTMLTextAreaElement).value).toBe('Unsaved note');expect(onReload).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Reload record'}));await waitFor(()=>expect(onReload).toHaveBeenCalledOnce())
 await waitFor(()=>expect((screen.getByRole('textbox',{name:'Review note'}) as HTMLTextAreaElement).value).toBe(''))
 expect(vi.mocked(dialectAPI).mock.calls.some(([,method])=>method==='POST')).toBe(false)
})
it('keeps failed save edits visible and offers reload instead of claiming success',async()=>{
 mock();vi.mocked(dialectAPI).mockImplementation(async(_path,method)=>{if(method==='POST')throw Error('Dialects changed. Reload before saving.');return sources as any})
 render(wrap(<ResearchRecordReview saved={saved} recordIds={['row']} opener={{current:null}} onClose={vi.fn()} onSaved={vi.fn()} onReload={async()=>saved}/>));choose();fireEvent.click(screen.getByRole('button',{name:'Save review'}))
 await screen.findByText('Dialects changed. Reload before saving.');expect(screen.queryByText('Review saved')).toBeNull();expect(screen.getByRole('button',{name:'Reload record'})).toBeTruthy()
})
it('distinguishes recipe changes and includes selected review history and research recipe in exports',()=>{
 expect(currentRecordReview(reviewed.document.records[0],2)).toBeUndefined();expect(researchReviewLabel(reviewed.document.records[0],2)).toBe('Review again — recipe changed')
 expect(recordsGuidance([record])).toContain('Open a record');expect(recordsGuidance([record])).not.toContain('importing')
 const doc={...reviewed.document,recipeVersion:2,recipes:[{version:1,name:'Research',recipe:saved.document.recipe,at:'2026-01-01'}],reviews:[{review,record},{review:{...review,recordId:'private'},record:{...record,id:'private',name:'Unselected record'}}]}
 const exported=exportDialect(doc,[record]);expect(exported.reviews).toEqual([{review,record}]);expect(exported.recipes).toHaveLength(1);expect(JSON.stringify(exported)).not.toContain('Unselected record')
})
it('opens research review from Records and disables generated evaluation before any prepare call',async()=>{
 mock();const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter initialEntries={['/dialects/one']}><Routes><Route path="/dialects/:preparationId" element={<DialectsContent/>}/></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 fireEvent.click(await screen.findByRole('button',{name:'Example'}));await screen.findByRole('region',{name:'Review research'})
 fireEvent.click(screen.getByRole('button',{name:'Back to records'}));await waitFor(()=>expect(screen.queryByRole('region',{name:'Review research'})).toBeNull())
 fireEvent.click(screen.getByRole('checkbox',{name:'Select Example'}))
 fireEvent.pointerDown(screen.getByRole('button',{name:'1 selected'}),{button:0,ctrlKey:false})
 expect((await screen.findByRole('menuitem',{name:'Evaluate…'})).getAttribute('aria-disabled')).toBe('true')
 fireEvent.click(screen.getByRole('menuitem',{name:'Review research'}));await screen.findByRole('region',{name:'Review research'})
 expect(vi.mocked(dialectAPI).mock.calls.some(([path])=>path?.endsWith('/prepare'))).toBe(false)
})

it('updates the saved row in Records immediately and preserves the other selected records',async()=>{
 const other={...record,id:'other',name:'Other company'}
 let current:PreparationReply={...saved,document:{...saved.document,records:[record,other]}}
 vi.mocked(dialectAPI).mockImplementation(async(path='',method)=>{
  if(method==='POST'){
   current={...current,sha256:'reviewed',document:{...current.document,records:[{...record,review},other],reviews:[{review,record}]}}
   return current as any
  }
  return path==='/one/research/run'?sources as any:path==='/one/research'||path==='/one/evaluations'?{items:[]} as any:current as any
 })
 const client=new QueryClient({defaultOptions:{queries:{retry:false,staleTime:Infinity,refetchOnWindowFocus:false}}})
 render(<QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter initialEntries={['/dialects/one']}><Routes><Route path="/dialects/:preparationId" element={<DialectsContent/>}/></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 fireEvent.click(await screen.findByRole('checkbox',{name:'Select Example'}))
 fireEvent.click(screen.getByRole('checkbox',{name:'Select Other company'}))
 fireEvent.pointerDown(screen.getByRole('button',{name:'2 selected'}),{button:0,ctrlKey:false})
 fireEvent.click(await screen.findByRole('menuitem',{name:'Review research'}))
 const dialog=await screen.findByRole('region',{name:'Review research'})
 choose();fireEvent.click(within(dialog).getByRole('button',{name:'Save and next'}))
 await screen.findByText('Review saved')
 fireEvent.click(within(dialog).getByRole('button',{name:'Back to records'}))
 await waitFor(()=>expect(screen.queryByRole('region',{name:'Review research'})).toBeNull())
 const row=screen.getByRole('button',{name:'Example'}).closest('tr')!
 expect(within(row).getByText('Ready for evaluation')).toBeTruthy()
 const unchanged=screen.getByRole('button',{name:'Other company'}).closest('tr')!
 expect(within(unchanged).getByText('Research needs review')).toBeTruthy()
 expect(screen.getByRole('button',{name:'2 selected'})).toBeTruthy()
 expect(vi.mocked(dialectAPI).mock.calls.filter(([,method])=>method==='POST')).toHaveLength(1)
 cleanup();client.clear()
})


it.each([null,undefined,[]])('opens current and historical fields with empty citations (%s) without changing evidence',async empty=>{
 const unquoted:DialectRecord={...record,research:{...record.research,entity:{...record.research.entity,fields:{
  country:record.research.entity.fields.country,
  employee_count:{status:'missing',sources:empty,note:'No public employee count was retained.'},
  intake_channels:{status:'inaccessible',sources:empty,value:null},
  fit_signal:{status:'interpretation',sources:empty,value:'Needs a buyer conversation'}
 }}}}
 const document:PreparationReply={...reviewed,document:{...reviewed.document,records:[{...unquoted,review}],reviews:[{review,record:unquoted}]}}
 const before=JSON.stringify(document)
 vi.mocked(dialectAPI).mockResolvedValue(sources as any)
 const onSaved=vi.fn()
 render(wrap(<ResearchRecordReview saved={document} recordIds={['row']} opener={{current:null}} onClose={vi.fn()} onSaved={onSaved} onReload={async()=>document}/>))
 expect(screen.getAllByText('Missing')).toHaveLength(2)
 expect(screen.getAllByText('Source unavailable')).toHaveLength(2)
 expect(screen.getAllByText('Unknown')).toHaveLength(2)
 expect(screen.getAllByText('null')).toHaveLength(2)
 expect(screen.getAllByText('Needs a buyer conversation')).toHaveLength(2)
 fireEvent.click(screen.getAllByRole('button',{name:'Details for Country'})[0])
 expect(screen.getAllByText('Located in Canada.')).toHaveLength(1)
 fireEvent.click(screen.getByText('Review history',{exact:true}))
 fireEvent.click(screen.getByText('Reviewed record',{exact:true}))
 for(const button of screen.getAllByRole('button',{name:'Details for Employee count'}))fireEvent.click(button)
 expect(screen.getAllByText('No public employee count was retained.')).toHaveLength(2)
 expect(screen.getAllByText('Missing')).toHaveLength(2)
 expect(JSON.stringify(document)).toBe(before)
 expect(onSaved).not.toHaveBeenCalled()
 expect(vi.mocked(dialectAPI).mock.calls.some(([,method])=>method==='POST'||method==='PUT')).toBe(false)
})

it('retains saved quotations when a legacy run has null source and company lists',async()=>{
 const person={...record,research:{...record.research,entity:{...record.research.entity,companyId:'company-1'}}}
 const document={...saved,document:{...saved.document,records:[person]}}
 vi.mocked(dialectAPI).mockResolvedValue({sources:null,companies:null} as any)
 render(wrap(<ResearchRecordReview saved={document} recordIds={['row']} opener={{current:null}} onClose={vi.fn()} onSaved={vi.fn()} onReload={async()=>document}/>))
 await waitFor(()=>expect(dialectAPI).toHaveBeenCalledWith('/one/research/run'))
 fireEvent.click(screen.getByRole('button',{name:'Details for Country'}))
 expect(screen.getByText('Located in Canada.')).toBeTruthy()
 expect(screen.getByText('Selected company')).toBeTruthy()
})

it('preserves per-record drafts and saves only the current record before advancing',async()=>{
 const other={...record,id:'other',name:'Other company'}
 const start={...saved,document:{...saved.document,records:[record,other]}}
 const next={...reviewed,document:{...reviewed.document,records:[reviewed.document.records[0],other]}}
 vi.mocked(dialectAPI).mockImplementation(async(_path='',method)=>method==='POST'?next as any:sources as any)
 function Harness(){const[current,setCurrent]=useState<PreparationReply>(start);return <ResearchRecordReview saved={current} recordIds={['row','other']} onClose={vi.fn()} onReload={async()=>current} onSaved={async next=>setCurrent(next)}/>}
 render(wrap(<Harness/>))
 choose();fireEvent.click(screen.getByRole('button',{name:'Add note'}));fireEvent.change(screen.getByRole('textbox',{name:'Review note'}),{target:{value:'First note'}})
 fireEvent.click(screen.getByRole('button',{name:'Next record'}))
 expect(screen.getByRole('heading',{name:'Other company'})).toBeTruthy()
 choose();fireEvent.click(screen.getByRole('button',{name:'Add note'}));fireEvent.change(screen.getByRole('textbox',{name:'Review note'}),{target:{value:'Second note'}})
 fireEvent.click(screen.getByRole('button',{name:'Previous record'}))
 expect((screen.getByRole('textbox',{name:'Review note'}) as HTMLTextAreaElement).value).toBe('First note')
 expect(vi.mocked(dialectAPI).mock.calls.some(([,method])=>method==='POST')).toBe(false)
 fireEvent.click(screen.getByRole('button',{name:'Save and next'}))
 await screen.findByRole('heading',{name:'Other company'})
 expect((screen.getByRole('textbox',{name:'Review note'}) as HTMLTextAreaElement).value).toBe('Second note')
 expect(vi.mocked(dialectAPI).mock.calls.filter(([,method])=>method==='POST')).toEqual([['/one/record-reviews','POST',{recordId:'row',decision:'accepted',note:'First note'},'first']])
})

it('restores the selected review and current record from its URL without saving anything',async()=>{
 const other={...record,id:'other',name:'Other company'}
 vi.mocked(dialectAPI).mockImplementation(async(path='')=>path==='/one/research/run'?sources as any:path==='/one/research'||path==='/one/evaluations'?{items:[]} as any:{...saved,document:{...saved.document,records:[record,other]}} as any)
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Tooltip.Provider><MemoryRouter initialEntries={['/dialects/one?review=row,other&record=other']}><Routes><Route path="/dialects/:preparationId" element={<DialectsContent/>}/></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 await screen.findByRole('heading',{name:'Other company'})
 expect(screen.queryByRole('dialog')).toBeNull()
 expect(screen.getByText('2 of 2')).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Back to records'}))
 await screen.findByRole('checkbox',{name:'Select Other company'})
 expect(vi.mocked(dialectAPI).mock.calls.some(([,method])=>method==='POST')).toBe(false)
})
