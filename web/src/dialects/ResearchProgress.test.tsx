import {act,cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter,Route,Routes} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {afterEach,expect,it,vi} from 'vitest'
import {DialectsContent} from './DialectsView'
import {ResearchProgress,ResearchReview} from './ResearchRun'
import {dialectAPI,type PreparationReply} from './client'
import {visibleResearchRun,type ResearchSummary} from './research'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
const recipe={version:1 as const,target:'companies' as const,fields:[{name:'description',description:'Business',type:'string' as const}],criteria:[],ai:{connection:'legacy-api',revision:'revision',model:'model'},limits:{records:3,searches:3,pages:6,modelCalls:15}}
const saved:PreparationReply={sha256:'original',document:{version:1,id:'one',name:'ICP',recipe:{intent:'Find companies',research:recipe},recipeVersion:1,createdAt:'2026-01-01',updatedAt:'2026-01-01',records:[],imports:[],activity:[]}}
const running:ResearchSummary={id:'run',state:'running',updatedAt:'2026-01-01',recordCount:0,stopRequested:false}
function wrapper(children:React.ReactNode){const client=new QueryClient({defaultOptions:{queries:{retry:false}}});return render(<QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter initialEntries={['/dialects/one?tab=recipe']}><Routes><Route path="/dialects/:preparationId" element={children}/></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)}
it('keeps one status and Stop outside all scrolling panels and updates after Stop',async()=>{
 let run=running
 vi.mocked(dialectAPI).mockImplementation(async(path='',method='GET')=>{
  if(path==='/one/research/run/stop'&&method==='POST'){run={...running,state:'stopped',updatedAt:'2026-01-02'};return run as any}
  if(path==='/one/research')return {items:[run]} as any
  if(path==='/one/evaluations')return {items:[]} as any
  return saved as any
 })
 wrapper(<DialectsContent/>);await screen.findByText('Researching… · 0 saved this run')
 for(const name of ['Recipe','Activity','Records']){
  fireEvent.click(screen.getByRole('tab',{name}))
  expect(screen.getAllByText('Researching… · 0 saved this run')).toHaveLength(1)
  expect(screen.getAllByRole('button',{name:'Stop'})).toHaveLength(1)
  expect(screen.getByText('Researching… · 0 saved this run').closest('[role="tabpanel"]')).toBeNull()
 }
 fireEvent.click(screen.getByRole('button',{name:'Stop'}))
 await screen.findByText('Research stopped · 0 saved this run')
 expect(screen.queryByRole('button',{name:'Stop'})).toBeNull()
 expect(dialectAPI).toHaveBeenCalledWith('/one/research/run/stop','POST',{})
})
it('keeps an older active run visible when a newer review or stopped review exists',()=>{
 expect(visibleResearchRun([{...running,id:'review',state:'review'},{...running,id:'cancelled',state:'stopped'},running])?.id).toBe('run')
 expect(visibleResearchRun([{...running,state:'review'}])).toBeUndefined()
})
it('distinguishes interruption and exposes the retained reason without offering Stop',()=>{
 render(<ResearchProgress run={{...running,state:'interrupted',problem:'The source request was refused (HTTP 400).'}} onStop={vi.fn()} busy={false}/>)
 expect(screen.getByText('Research interrupted · 0 saved this run')).toBeTruthy()
 expect(screen.getByText('The source request was refused (HTTP 400).')).toBeTruthy()
 expect(screen.queryByRole('button',{name:'Stop'})).toBeNull()
})
it('uses the confirmed run immediately instead of waiting for the next status poll',async()=>{
 const run={...running,version:1,preparationId:'one',recipeVersion:1,createdAt:'2026-01-01',intent:'Find companies',recipe,records:[],companies:[],sources:[],attempts:[],state:'review'}
 let confirm!:(value:unknown)=>void
 vi.mocked(dialectAPI).mockImplementation(async path=>path?.endsWith('/confirm')?new Promise(resolve=>{confirm=resolve}):run as any)
 const started=vi.fn(),close=vi.fn()
 wrapper(<ResearchReview saved={saved} opener={{current:null}} onClose={close} onStarted={started}/>)
 const dialog=await screen.findByRole('dialog',{name:'Review research'})
 await waitFor(()=>expect((within(dialog).getByRole('button',{name:'Start research'}) as HTMLButtonElement).disabled).toBe(false))
 fireEvent.click(within(dialog).getByRole('button',{name:'Start research'}))
 await screen.findByRole('button',{name:'Starting research…'})
 const accepted={...run,state:'queued'};await act(async()=>confirm(accepted))
 expect(started).toHaveBeenCalledWith(accepted);expect(close).toHaveBeenCalledOnce()
})
it('requires saving recipe edits before research and Save does not start a run',async()=>{
 vi.mocked(dialectAPI).mockImplementation(async(path='',method='GET',body)=>{
  if(path==='/one/research'||path==='/one/evaluations')return {items:[]} as any
  if(method==='PUT')return {...saved,sha256:'updated',document:{...saved.document,...body as object}} as any
  return saved as any
 })
 wrapper(<DialectsContent/>);const field=await screen.findByRole('textbox',{name:'What records do you need?'})
 fireEvent.change(field,{target:{value:'Unsaved research instructions'}})
 expect(screen.getByText('Unsaved changes')).toBeTruthy()
 fireEvent.pointerDown(screen.getByRole('button',{name:'Prepare records'}),{button:0,ctrlKey:false})
 const disabled=await screen.findByRole('menuitem',{name:'Save recipe before research'})
 expect(disabled.getAttribute('aria-disabled')).toBe('true')
 fireEvent.keyDown(disabled,{key:'Escape'})
 fireEvent.click(screen.getByRole('button',{name:'Save recipe'}))
 await screen.findByText('All changes saved')
 expect(vi.mocked(dialectAPI).mock.calls.some(([,method])=>method==='PUT')).toBe(true)
 expect(vi.mocked(dialectAPI).mock.calls.some(([path,method])=>method==='POST'&&path?.includes('/research'))).toBe(false)
})

it('summarizes the run and keeps detailed source gaps behind View steps',()=>{
 const steps=vi.fn(),stop=vi.fn()
 const view=render(<ResearchProgress run={{...running,recordCount:2,unavailableSources:1}} onStop={stop} onSteps={steps} busy={false}/>)
 expect(screen.getByRole('status').textContent).toBe('Researching… · 2 saved this run')
 fireEvent.click(screen.getByRole('button',{name:'View steps'}));expect(steps).toHaveBeenCalledOnce()
 expect(screen.getByRole('button',{name:'Stop'})).toBeTruthy()
 view.rerender(<ResearchProgress run={{...running,state:'completed-with-gaps',recordCount:2,unavailableSources:1,problem:'Review missing fields and source coverage.'}} onStop={stop} onSteps={steps} busy={false}/>)
 expect(screen.getByRole('status').textContent).toBe('Research finished with gaps · 2 saved this run')
 expect(screen.queryByText('Review missing fields and source coverage.')).toBeNull()
 expect(screen.getByRole('button',{name:'View steps'})).toBeTruthy()
 expect(screen.queryByRole('button',{name:'Stop'})).toBeNull()
 expect(stop).not.toHaveBeenCalled()
})

it('shows connection waiting separately from a submitted AI request and keeps Stop available',()=>{
 const stop=vi.fn(),run:ResearchSummary={...running,currentStep:{id:'model',kind:'model',state:'waiting',startedAt:new Date().toISOString()}}
 const view=render(<ResearchProgress run={run} onStop={stop} busy={false}/>)
 expect(screen.getByRole('status').textContent).toBe('Waiting for AI connection… · 0 saved this run')
 fireEvent.click(screen.getByRole('button',{name:'Stop'}));expect(stop).toHaveBeenCalledOnce()
 view.rerender(<ResearchProgress run={{...run,currentStep:{...run.currentStep!,state:'started'}}} onStop={stop} busy={false}/>)
 expect(screen.getByRole('status').textContent).toBe('Waiting for AI response… · 0 saved this run')
})

it('prioritizes a running operation over a newer queued run',()=>{
 expect(visibleResearchRun([{...running,id:'queued',state:'queued'},running])?.id).toBe('run')
})
it('shows the save phase without losing Stop or connection waiting',()=>{
 const view=render(<ResearchProgress run={{...running,phase:'finalize'}} onStop={vi.fn()} busy={false}/>)
 expect(screen.getByRole('status').textContent).toBe('Saving research records… · 0 saved this run')
 expect(screen.getByRole('button',{name:'Stop'})).toBeTruthy()
 view.rerender(<ResearchProgress run={{...running,phase:'finalize',currentStep:{id:'one',kind:'model',state:'waiting',startedAt:new Date().toISOString()}}} onStop={vi.fn()} busy={false}/>)
 expect(screen.getByRole('status').textContent).toBe('Waiting for AI connection… · 0 saved this run')
})
it('reviews an explicit selection without starting until confirmation',async()=>{
 const selected=[{recordId:'r1',entity:{id:'e1',name:'Example',kind:'company',url:'https://example.com',fields:{}}}]
 const run={...running,version:1,preparationId:'one',recipeVersion:1,createdAt:'2026-01-01',intent:'Find companies',recipe:{...recipe,limits:{...recipe.limits,records:1,searches:0,pages:0}},records:[],companies:[],sources:[{id:'source'}],attempts:[],state:'review',reprocess:true,selection:selected,phase:'finalize'}
 vi.mocked(dialectAPI).mockImplementation(async(path)=>path?.endsWith('/confirm')?{...run,state:'queued'} as any:run as any)
 const close=vi.fn(),started=vi.fn()
 wrapper(<ResearchReview saved={saved} recordIds={['r1']} opener={{current:null}} onClose={close} onStarted={started}/>)
 const dialog=await screen.findByRole('dialog',{name:'Reprocess 1 record'})
 await within(dialog).findByText('Update only the selected records using their saved evidence. No new searches or website reads.')
 expect(dialectAPI).toHaveBeenCalledWith('/one/research','POST',{recordIds:['r1']},saved.sha256)
 expect(vi.mocked(dialectAPI).mock.calls.some(([path])=>path?.endsWith('/confirm'))).toBe(false)
 fireEvent.click(within(dialog).getByRole('button',{name:'Reprocess 1 record'}))
 await waitFor(()=>expect(dialectAPI).toHaveBeenCalledWith('/one/research/run/confirm','POST',{}))
 expect(started).toHaveBeenCalledOnce();expect(close).toHaveBeenCalledOnce()
})
it('never exposes whole-run continuation in progress, even for historical runs',()=>{
 wrapper(<ResearchProgress preparationId="one" editsPending run={{...running,state:'interrupted',canContinue:true}} onStop={vi.fn()} busy={false}/>)
 expect(screen.queryByRole('button',{name:/Continue|Reprocess/})).toBeNull()
 expect(dialectAPI).not.toHaveBeenCalled()
})
it('shows publication outcomes without counting retained pending records as saved',()=>{
 wrapper(<ResearchProgress run={{...running,state:'completed',reprocess:true,selectionCount:3,recordCount:3,outcomes:{added:0,updated:1,unchanged:1,needsCorrection:0,pending:1}}} onStop={vi.fn()} busy={false}/>)
 expect(screen.getByRole('status').textContent).toBe('Reprocessing complete · 1 updated · 1 unchanged · 1 awaiting save')
})
