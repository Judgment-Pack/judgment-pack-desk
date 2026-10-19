import {cleanup,fireEvent,render,screen,waitFor,within,act} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {ResearchActivityDialog,ResearchWebRecovery} from './ResearchActivity'
import {ResearchProgress} from './ResearchRun'
import {dialectAPI} from './client'
import type {ResearchActivity} from './research'
const m=vi.hoisted(()=>({data:{local:true,supported:true,enabled:false,managed:false,revision:'first'},error:false,apply:vi.fn(),refetch:vi.fn()}))
vi.mock('../connections/webAccess',()=>({WEB_ACCESS_KEY:['web-access'],useWebAccess:()=>({data:m.data,isError:m.error,isPending:false,refetch:m.refetch}),applyWebAccess:m.apply}))
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
beforeEach(()=>{m.data={local:true,supported:true,enabled:false,managed:false,revision:'first'};m.error=false;m.apply.mockImplementation(async (enabled:boolean)=>{m.data={...m.data,enabled,revision:'second'};return m.data})})
afterEach(()=>{cleanup();vi.clearAllMocks()})
function show(children:React.ReactNode){const client=new QueryClient({defaultOptions:{queries:{retry:false,staleTime:Infinity}}});return{client,...render(<QueryClientProvider client={client}>{children}</QueryClientProvider>)}}
const run:ResearchActivity={id:'run',state:'running',createdAt:'2026-01-01T00:00:00Z',updatedAt:'2026-01-01T00:00:10Z',recipeVersion:1,recordCount:0,stopRequested:false,steps:[{id:'one',kind:'model',state:'completed',startedAt:'2026-01-01T00:00:00Z',finishedAt:'2026-01-01T00:00:05Z',result:'read'},{id:'two',kind:'page',state:'started',startedAt:'2026-01-01T00:00:05Z',url:'https://example.com/'}]}
it('shows actual steps, source links and later uncertainty without moving focus',async()=>{
 vi.mocked(dialectAPI).mockResolvedValue(run)
 const {client}=show(<ResearchActivityDialog preparationId="one" runId="run" opener={{current:null}} onClose={vi.fn()}/>)
 await screen.findByText('Read a website');const list=screen.getByRole('list',{name:'Research steps'})
 expect(within(list).getAllByRole('listitem')).toHaveLength(2)
 fireEvent.click(screen.getByText('Read a website'))
 expect(screen.getByRole('link',{name:'https://example.com/'}).getAttribute('href')).toBe('https://example.com/')
 const focus=screen.getByRole('button',{name:'Close'});focus.focus()
 act(()=>client.setQueryData(['input-research-activity','one','run'],{...run,state:'interrupted',steps:[run.steps[0],{...run.steps[1],state:'uncertain',problem:'Read refused.',finishedAt:'2026-01-01T00:00:10Z'}]}))
 expect(await screen.findByText('Outcome uncertain')).toBeTruthy();expect(screen.getByText('Read refused.')).toBeTruthy()
 expect(document.activeElement).toBe(focus)
 expect(dialectAPI).toHaveBeenCalledWith('/one/research/run/activity')
})
it('offers recovery only for the supported saved policy and never starts research after Apply',async()=>{
 show(<ResearchProgress run={{...run,state:'interrupted',problem:'Blocked.',recovery:'google-grounding'}} onStop={vi.fn()} busy={false} onSteps={vi.fn()}/>)
 expect(screen.queryByText('Website read was blocked.')).toBeNull()
 expect(screen.getByRole('button',{name:'View steps'})).toBeTruthy()
 const opener=screen.getByRole('button',{name:'Review web access'});fireEvent.click(opener)
 const dialog=screen.getByRole('dialog',{name:'Web access'});expect(within(dialog).getAllByRole('heading',{name:'Web access'})).toHaveLength(1)
 expect(within(dialog).getByText('Research won’t restart automatically.')).toBeTruthy()
 fireEvent.click(screen.getByRole('switch',{name:'Read websites linked by Google search'}))
 expect(m.apply).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Apply changes'}))
 await waitFor(()=>expect(m.apply).toHaveBeenCalledExactlyOnceWith(true,'first'))
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 await waitFor(()=>expect(document.activeElement).toBe(opener))
 expect(screen.getByRole('button',{name:'Review web access'})).toBe(opener)
 expect(dialectAPI).not.toHaveBeenCalled()
})
it('keeps enabled settings inspectable while managed policies explain who controls access',()=>{
 m.data.managed=true
 const v=show(<ResearchWebRecovery/>);expect(screen.queryByRole('button',{name:'Review web access'})).toBeNull();expect(screen.getByText(/managed by your organization/)).toBeTruthy()
 m.data={...m.data,managed:false,enabled:true};v.rerender(<QueryClientProvider client={v.client}><ResearchWebRecovery/></QueryClientProvider>)
 fireEvent.click(screen.getByRole('button',{name:'Review web access'}))
 expect(screen.getByRole('switch').getAttribute('aria-checked')).toBe('true')
 expect((screen.getByRole('button',{name:'Apply changes'}) as HTMLButtonElement).disabled).toBe(true)
})
it('uses the real current step in the status without announcing every elapsed second',()=>{
 show(<ResearchProgress run={{...run,currentStep:run.steps[1]}} onStop={vi.fn()} busy={false} onSteps={vi.fn()}/>)
 expect(screen.getByRole('status').textContent).toBe('Reading a website… · 0 saved this run')
 expect(screen.getByLabelText(/Elapsed time:/).getAttribute('aria-live')).toBe('off')
 expect(screen.getByRole('button',{name:'View steps'})).toBeTruthy()
})

it('refreshes a cached terminal run when activity is reopened',async()=>{
 const client=new QueryClient({defaultOptions:{queries:{retry:false,staleTime:Infinity}}})
 client.setQueryData(['input-research-activity','one','run'],{...run,state:'interrupted',steps:[]})
 vi.mocked(dialectAPI).mockResolvedValue(run)
 render(<QueryClientProvider client={client}><ResearchActivityDialog preparationId="one" runId="run" opener={{current:null}} onClose={vi.fn()}/></QueryClientProvider>)
 expect(await screen.findByText('Read a website')).toBeTruthy()
 expect(screen.getByText('Running')).toBeTruthy()
})

it('shows retained source failure recovery without treating the source as completed evidence',async()=>{
 vi.mocked(dialectAPI).mockResolvedValue({...run,state:'completed-with-gaps',unavailableSources:1,recordCount:2,steps:[run.steps[0],{...run.steps[1],state:'uncertain',continued:true,problemCode:'web-over-limit',finishedAt:'2026-01-01T00:00:06Z',problem:'The page exceeded its limit.'}]})
 show(<ResearchActivityDialog preparationId="one" runId="run" opener={{current:null}} onClose={vi.fn()}/>)
 await screen.findByText('Too large')
 expect(screen.getByRole('status').textContent).toBe('Research finished with gaps · 2 saved this run · 1 source unavailable')
 expect(screen.queryByText('Outcome uncertain')).toBeNull()
 fireEvent.click(screen.getByText('Read a website'))
 expect(screen.getByText('This source was left out of the research. Saved sources and records were kept; this request was not repeated after this step.')).toBeTruthy()
 expect(screen.getByRole('link',{name:'https://example.com/'})).toBeTruthy()
 expect(vi.mocked(dialectAPI).mock.calls.every(([,method])=>!method||method==='GET')).toBe(true)
})

it('distinguishes waiting and stopped-before-admission from uncertain provider outcomes',async()=>{
 const waiting={id:'wait',kind:'model' as const,state:'waiting' as const,startedAt:'2026-01-01T00:00:05Z'}
 vi.mocked(dialectAPI).mockResolvedValue({...run,steps:[waiting],currentStep:waiting})
 const {client}=show(<ResearchActivityDialog preparationId="one" runId="run" opener={{current:null}} onClose={vi.fn()}/>)
 await screen.findByText('Waiting')
 fireEvent.click(screen.getByText('AI request'))
 expect(screen.getByText('Waiting for the AI connection to become available. This request has not been sent.')).toBeTruthy()
 expect(screen.queryByText('Outcome uncertain')).toBeNull()
 act(()=>client.setQueryData(['input-research-activity','one','run'],{...run,state:'stopped',steps:[{...waiting,state:'not-started',finishedAt:'2026-01-01T00:00:10Z',problem:'Research stopped while waiting for the AI connection. No AI request was sent.'}]}))
 expect(await screen.findByText('Not started')).toBeTruthy()
 expect(screen.queryByText('Outcome uncertain')).toBeNull()
})

it('shows a declined model action separately from a completed page read',async()=>{
 vi.mocked(dialectAPI).mockResolvedValue({...run,state:'completed',steps:[{...run.steps[0],actionProblem:'The source reference did not match. No page read was sent.'},{...run.steps[1],state:'completed',finishedAt:'2026-01-01T00:00:10Z',result:'source-saved'}]})
 show(<ResearchActivityDialog preparationId="one" runId="run" opener={{current:null}} onClose={vi.fn()}/>)
 await screen.findByText('Needs correction')
 fireEvent.click(screen.getByText('AI request'))
 expect(screen.getByText('The source reference did not match. No page read was sent.')).toBeTruthy()
 expect(screen.getByText('Completed')).toBeTruthy()
 expect(screen.queryByText('Outcome uncertain')).toBeNull()
})

it('shows parallel page reads until the last page completes, then saving',()=>{
 const active={...run,phase:'save',activePageReads:3,currentStep:run.steps[1]}
 const {rerender}=render(<ResearchProgress run={active} onStop={vi.fn()} busy={false}/>)
 expect(screen.getByRole('status').textContent).toBe('Reading 3 websites… · 0 saved this run')
 rerender(<ResearchProgress run={{...active,activePageReads:1}} onStop={vi.fn()} busy={false}/>)
 expect(screen.getByRole('status').textContent).toBe('Reading a website… · 0 saved this run')
 rerender(<ResearchProgress run={{...active,activePageReads:0,currentStep:undefined}} onStop={vi.fn()} busy={false}/>)
 expect(screen.getByRole('status').textContent).toBe('Saving research records… · 0 saved this run')
})
