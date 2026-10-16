import {cleanup,fireEvent,render,screen,within,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {afterEach,expect,it,vi} from 'vitest'
import {ReviewRecords} from './ReviewRecords'
import {jobsAPI} from '../jobs/client'
import {dialectAPI,prepareRecords,type PreparationReply,type DialectEvaluation} from './client'
vi.mock('../jobs/client',async original=>({...await original<typeof import('../jobs/client')>(),jobsAPI:vi.fn()}))
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn(),prepareRecords:vi.fn()}))
vi.mock('../jobs/ReleaseReadiness',()=>({ReleaseReadiness:()=>null}))
vi.mock('../jobs/ReleaseStanding',()=>({ReleaseStanding:()=>null}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
const record={id:'row',name:'One',values:{Active:'false'},sourceId:'source',row:1,class:'asserted' as const,status:'provided' as const}
const saved:PreparationReply={sha256:'saved',document:{version:1,id:'prep',name:'Dialects',recipe:{intent:'Review',qualification:{kind:'pack',key:'pack',digest:'sha256:pack',pack:JSON.stringify({id:'pack',version:'1',decision:{},rules:[{when:{op:'fact',path:'/active',value:true}},{when:{op:'fact',path:'/missing',value:'yes'}}],evidenceRequirements:[{id:'proof'}]}),mapping:{version:2,case:{facts:[{target:'/active',source:'/values/Active'}],evidence:[]},sources:[]}}},recipeVersion:2,records:[record],imports:[{id:'source',kind:'csv',name:'provided.csv',content:'Active\nfalse',digest:'sha256:source',at:'date',recipeVersion:1}],recipes:[],activity:[],createdAt:'date',updatedAt:'date'}}
const evaluation={id:'eval',release:{title:'Pack',packVersion:'1',tests:'passed',preview:{disposition:{kind:'outcome',outcomeId:'ready'}}},requireTested:true,projectReviewed:false,recipeVersion:2} as DialectEvaluation
function show(){
 vi.mocked(prepareRecords).mockResolvedValue({rows:[{id:'row',original:record.values,prepared:{'/active':false},issues:[],input:{facts:{active:false}}}]})
 vi.mocked(jobsAPI).mockResolvedValue({input:{facts:{active:false}},factsText:'{"active":false}',evidenceText:''})
 const onEvaluated=vi.fn(),onClose=vi.fn(),client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 const element=(value:PreparationReply)=><QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter><ReviewRecords saved={value} records={[record]} opener={{current:null}} onClose={onClose} onEvaluated={onEvaluated}/></MemoryRouter></Tooltip.Provider></QueryClientProvider>
 const view=render(element(saved));return {...view,onEvaluated,onClose,element}
}
it('shows original text beside explicit false and preserves unknowns without creating a release',async()=>{
 show();const active=(await screen.findByRole('cell',{name:'active'})).closest('tr')!
 expect(within(active).getByRole('cell',{name:'"false"'})).toBeTruthy()
 expect(within(active).getByRole('cell',{name:'false'})).toBeTruthy()
 expect(within(active).getByRole('cell',{name:'Supplied'})).toBeTruthy()
 expect(within(screen.getByRole('cell',{name:'missing'}).closest('tr')!).getByRole('cell',{name:'Unknown'})).toBeTruthy()
 expect(dialectAPI).not.toHaveBeenCalled()
})
it('requires explicit confirmation and reports a stale server review without submitting a new review',async()=>{
 show();vi.mocked(dialectAPI).mockResolvedValue(evaluation)
 await waitFor(()=>expect((screen.getByRole('button',{name:'Review evaluation'}) as HTMLButtonElement).disabled).toBe(false));fireEvent.click(screen.getByRole('button',{name:'Review evaluation'}))
 await screen.findByRole('dialog',{name:'Evaluate 1 record'})
 const submit=screen.getByRole('button',{name:'Evaluate 1 record'}) as HTMLButtonElement
 expect(submit.disabled).toBe(true)
 fireEvent.click(screen.getByRole('checkbox',{name:'I reviewed the selected inputs and this release’s test status.'}))
 vi.mocked(dialectAPI).mockRejectedValue(Error('The project changed. Review evaluation again.'))
 fireEvent.click(submit)
 await screen.findByText('The project changed. Review evaluation again.')
 expect(vi.mocked(dialectAPI).mock.calls).toEqual([['/prep/evaluations','POST',{recordIds:['row']},'saved'],['/prep/evaluations/eval/confirm','POST',{reviewed:true},'saved']])
})
it('withdraws confirmation when a background refresh changes the recipe',async()=>{
 const view=show();vi.mocked(dialectAPI).mockResolvedValue(evaluation)
 await waitFor(()=>expect((screen.getByRole('button',{name:'Review evaluation'}) as HTMLButtonElement).disabled).toBe(false));fireEvent.click(screen.getByRole('button',{name:'Review evaluation'}));await screen.findByRole('checkbox',{name:'I reviewed the selected inputs and this release’s test status.'});fireEvent.click(screen.getByRole('checkbox',{name:'I reviewed the selected inputs and this release’s test status.'}))
 view.rerender(view.element({...saved,sha256:'changed'}))
 expect((screen.getByRole('button',{name:'Evaluate 1 record'}) as HTMLButtonElement).disabled).toBe(true)
 expect(screen.getByText('Dialects changed. Close this dialog and review the selection again.')).toBeTruthy()
})
it('blocks the whole selection when a conversion fails instead of skipping the record',async()=>{
 show();vi.mocked(prepareRecords).mockResolvedValue({rows:[{id:'row',original:record.values,prepared:{},issues:[{target:'/active',message:'Does not match.'}],input:{}}]})
 // The query settles after render; force a separate render with a fresh client.
 cleanup();const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<QueryClientProvider client={client}><Tooltip.Provider><ReviewRecords saved={saved} records={[record]} opener={{current:null}} onClose={()=>{}}/></Tooltip.Provider></QueryClientProvider>)
 await screen.findByText('Does not match.')
 expect((screen.getByRole('button',{name:'Review evaluation'}) as HTMLButtonElement).disabled).toBe(true)
 expect(dialectAPI).not.toHaveBeenCalled()
})
it('labels generated origin and keeps human readiness separate from the pack result',async()=>{
 const generated={...record,class:'generated' as const,review:{recordId:'row',recordDigest:'digest',recipeVersion:2,decision:'accepted' as const,note:'Human note',at:'2026-10-12T10:00:00Z',reviewer:{kind:'local-access' as const}}}
 const current={...saved,document:{...saved.document,records:[generated]}}
 vi.mocked(prepareRecords).mockResolvedValue({rows:[{id:'row',origin:'generated',original:record.values,prepared:{'/active':false},issues:[],input:{}}]})
 vi.mocked(jobsAPI).mockResolvedValue({input:{facts:{active:false},evidence:{}}})
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter><ReviewRecords saved={current} records={[generated]} opener={{current:null}} onClose={()=>{}}/></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 const row=(await screen.findByRole('cell',{name:'active'})).closest('tr')!
 expect(within(row).getByRole('cell',{name:'AI research'})).toBeTruthy()
 expect(within(row).getByRole('cell',{name:'false'})).toBeTruthy()
 expect(screen.getByText('1 selected · 1 ready')).toBeTruthy()
 expect(dialectAPI).not.toHaveBeenCalled()
})

it('saves automatic configuration paused and keeps enablement a separate reviewed action',async()=>{
 show();vi.mocked(dialectAPI).mockResolvedValue(evaluation)
 await waitFor(()=>expect((screen.getByRole('button',{name:'Review evaluation'}) as HTMLButtonElement).disabled).toBe(false))
 fireEvent.click(screen.getByRole('button',{name:'Review evaluation'}));await screen.findByRole('dialog',{name:'Evaluate 1 record'})
 fireEvent.click(screen.getByRole('checkbox',{name:'Connect these Dialects to an incremental job'}))
 expect((screen.getByRole('button',{name:'Save paused'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('checkbox',{name:'Include existing eligible records when enabled'}))
 fireEvent.click(screen.getByRole('checkbox',{name:'I reviewed the selected inputs and this release’s test status.'}))
 fireEvent.click(screen.getByRole('button',{name:'Save paused'}))
 await waitFor(()=>expect(dialectAPI).toHaveBeenCalledWith('/prep/evaluations/eval/confirm','POST',{reviewed:true,subscription:{mode:'new',includeExisting:true}},'saved'))
 expect(vi.mocked(dialectAPI).mock.calls.some(c=>c[0]?.endsWith('/subscription-state'))).toBe(false)
})

it('reviews an immutable subscription replacement and saves paused without creating another job',async()=>{
 vi.mocked(prepareRecords).mockResolvedValue({rows:[{id:'row',original:record.values,prepared:{'/active':false},issues:[],input:{facts:{active:false}}}]})
 vi.mocked(jobsAPI).mockResolvedValue({input:{facts:{active:false}}})
 const source={id:'old'} as DialectEvaluation,candidate={...evaluation,releaseId:'new-release',subscriptionUpgrade:{baseReleaseId:'old-release',mode:'changed'}} as DialectEvaluation
 vi.mocked(dialectAPI).mockResolvedValue(candidate)
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}}),done=vi.fn()
 render(<QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter><ReviewRecords saved={saved} records={[record]} opener={{current:null}} onClose={()=>{}} onEvaluated={done} upgradeSource={source}/></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 await waitFor(()=>expect((screen.getByRole('button',{name:'Review evaluation'}) as HTMLButtonElement).disabled).toBe(false))
 fireEvent.click(screen.getByRole('button',{name:'Review evaluation'}));await screen.findByText('new-release')
 expect(screen.queryByRole('checkbox',{name:'Include existing eligible records when enabled'})).toBeNull()
 expect((screen.getByRole('button',{name:'Apply upgrade'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('checkbox',{name:'I reviewed the selected inputs and this release’s test status.'}));fireEvent.click(screen.getByRole('button',{name:'Apply upgrade'}))
 await waitFor(()=>expect(done).toHaveBeenCalled())
 expect(vi.mocked(dialectAPI).mock.calls).toEqual([['/prep/evaluations','POST',{recordIds:['row'],subscriptionId:'old'},'saved'],['/prep/evaluations/eval/subscription-upgrade','POST',{reviewed:true,action:'apply'},'saved']])
})
