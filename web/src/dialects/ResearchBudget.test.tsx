import {act,cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClient,QueryClientProvider,useQuery} from '@tanstack/react-query'
import {afterEach,expect,it,vi} from 'vitest'
import {ResearchBudgetActions} from './ResearchBudget'
import {ResearchProgress} from './ResearchRun'
import {dialectAPI} from './client'
import {usePublishDialectEditing} from './recipeEditing'
import type {ResearchBudgetRequest,ResearchBudgetReview,ResearchSummary} from './research'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
const limits={records:10,searches:10,pages:20,modelCalls:40}
const request:ResearchBudgetRequest={id:'budget-one',state:'pending',createdAt:'2026-10-13T10:00:00Z',reason:'More page reads are needed.',current:limits,proposed:{...limits,pages:25,modelCalls:55},used:{records:6,searches:8,pages:20,modelCalls:39}}
const run:ResearchSummary={id:'run',state:'paused',recordCount:6,sourceCount:20,updatedAt:'2026-10-13T10:00:00Z',stopRequested:false,budgetRequest:request,canIncrease:true}
const review:ResearchBudgetReview={runId:'run',revision:'reviewed',recipeVersion:2,recordCount:6,sourceCount:20,request}
function show(children:React.ReactNode){const client=new QueryClient({defaultOptions:{queries:{retry:false,staleTime:Infinity}}});return {client,...render(<QueryClientProvider client={client}>{children}</QueryClientProvider>)}}
function mock(){vi.mocked(dialectAPI).mockImplementation(async(path='')=>{
 if(path==='/one')return {sha256:'reviewed'} as any
 if(path.endsWith('/budget-review'))return review as any
 if(path.endsWith('/budget-decision'))return {...run,state:'queued',budgetRequests:[{...request,state:'approved'}]} as any
 throw new Error(path)
})}
it('reviews exact allowances without granting them and cancels back to the pending decision',async()=>{
 mock();show(<ResearchBudgetActions preparationId="one" run={run}/>)
 const opener=screen.getByRole('button',{name:'Review increase'});fireEvent.click(opener)
 const dialog=await screen.findByRole('dialog',{name:'Review research increase'})
 expect(within(dialog).getByText('6 of 10 records saved')).toBeTruthy()
 expect(within(dialog).getAllByRole('row')).toHaveLength(4)
 expect(within(dialog).getByText('+15')).toBeTruthy()
 expect(dialectAPI).toHaveBeenCalledWith('/one/research/run/budget-review','POST',{},'reviewed')
 expect(vi.mocked(dialectAPI).mock.calls.some(([p])=>p?.endsWith('budget-decision'))).toBe(false)
 fireEvent.click(within(dialog).getByRole('button',{name:'Cancel'}))
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 await waitFor(()=>expect(document.activeElement).toBe(opener))
 expect(screen.getByRole('button',{name:'Keep 6 records'})).toBeTruthy()
})
it('applies the server request once with its reviewed revision and updates both surfaces',async()=>{
 mock()
 function Surface(){const q=useQuery({queryKey:['input-research','one'],queryFn:async()=>({items:[run]}),enabled:false});return <ResearchProgress preparationId="one" run={q.data?.items[0]} busy={false} onStop={()=>{}}/>}
 const {client}=show(<><Surface/><Surface/></>)
 act(()=>client.setQueryData(['input-research','one'],{items:[run]}))
 await waitFor(()=>expect(screen.getAllByRole('button',{name:'Review increase'})).toHaveLength(2))
 fireEvent.click(screen.getAllByRole('button',{name:'Review increase'})[0])
 const approve=await screen.findByRole('button',{name:'Approve and continue'})
 fireEvent.click(approve);fireEvent.click(approve)
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 expect(vi.mocked(dialectAPI).mock.calls.filter(([p])=>p?.endsWith('/budget-decision'))).toEqual([['/one/research/run/budget-decision','POST',{requestId:'budget-one',decision:'approve'},'reviewed']])
 expect(screen.queryByRole('button',{name:'Review increase'})).toBeNull()
 expect(screen.getAllByRole('button',{name:'Stop'})).toHaveLength(2)
})
it('requires a refreshed review after a rejected stale approval',async()=>{
 mock();const implementation=vi.mocked(dialectAPI).getMockImplementation()!
 vi.mocked(dialectAPI).mockImplementation(async(...args)=>{
  if(args[0]?.endsWith('/budget-decision'))throw new Error('Dialects changed after this review.')
  return implementation(...args)
 })
 show(<ResearchBudgetActions preparationId="one" run={run}/>)
 fireEvent.click(screen.getByRole('button',{name:'Review increase'}))
 fireEvent.click(await screen.findByRole('button',{name:'Approve and continue'}))
 await screen.findByText('Dialects changed after this review.')
 expect((screen.getByRole('button',{name:'Approve and continue'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Reload review'}))
 await waitFor(()=>expect((screen.getByRole('button',{name:'Approve and continue'}) as HTMLButtonElement).disabled).toBe(false))
 expect(vi.mocked(dialectAPI).mock.calls.filter(([p])=>p?.endsWith('/budget-decision'))).toHaveLength(1)
})
it('keeps results even with unsaved recipe edits without granting allowance or saving the recipe',async()=>{
 mock()
 function Editing(){usePublishDialectEditing('one',true);return <ResearchBudgetActions preparationId="one" run={run} disabled/>}
 show(<Editing/>)
 expect((screen.getByRole('button',{name:'Save recipe before continuing'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Keep 6 records'}))
 await waitFor(()=>expect(dialectAPI).toHaveBeenCalledExactlyOnceWith('/one/research/run/budget-decision','POST',{requestId:'budget-one',decision:'decline'},undefined))
})

it('makes finishing a zero-record run explicit',async()=>{
 mock();show(<ResearchBudgetActions preparationId="one" run={{...run,recordCount:0}}/>)
 fireEvent.click(screen.getByRole('button',{name:'Finish without records'}))
 await waitFor(()=>expect(dialectAPI).toHaveBeenCalledWith('/one/research/run/budget-decision','POST',{requestId:'budget-one',decision:'decline'},undefined))
})
