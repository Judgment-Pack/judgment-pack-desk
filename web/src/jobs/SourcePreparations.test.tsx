import {cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {afterEach,expect,it,vi} from 'vitest'
import {SourceProgress,SourcePreparations} from './SourcePreparations'
import type {Occurrence} from './triggerTypes'
import {jobsAPI} from './client'
vi.mock('./client',()=>({jobsAPI:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
const occurrence={id:'occ_one',jobId:'job_one',state:'waiting',receivedAt:new Date().toISOString(),preparation:{startedAt:new Date().toISOString(),deadline:new Date(Date.now()+60000).toISOString(),tasks:[{id:'source_op',name:'Vendor screening',state:'running',startedAt:new Date().toISOString()}]}} as Occurrence
function show(children:React.ReactNode){return render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Tooltip.Provider><MemoryRouter>{children}</MemoryRouter></Tooltip.Provider></QueryClientProvider>)}
it('shows pending source work before any evaluation run exists',async()=>{
 vi.mocked(jobsAPI).mockResolvedValue({items:[occurrence],next:0});show(<SourcePreparations jobId="job_one"/>);
 expect(await screen.findByText('Waiting for sources')).toBeTruthy()
 expect(screen.queryByText('Passed')).toBeNull();expect(screen.queryByRole('link')).toBeNull()
 fireEvent.click(screen.getByText('Sources · 1'))
 expect(await screen.findByText('Vendor screening')).toBeTruthy();expect(screen.getByText('Source deadline')).toBeTruthy()
})
it('requires typed confirmation before cancel and makes a single request',async()=>{
 vi.mocked(jobsAPI).mockResolvedValue({});show(<SourceProgress occurrence={occurrence}/>);
 fireEvent.click(screen.getByText('Sources · 1'));fireEvent.click(screen.getByRole('button',{name:'Cancel'}))
 const dialog=await screen.findByRole('dialog',{name:'Cancel source preparation?'})
 const confirm=within(dialog).getByRole('button',{name:'Confirm'}) as HTMLButtonElement
 expect(confirm.disabled).toBe(true);expect(jobsAPI).not.toHaveBeenCalled()
 fireEvent.change(within(dialog).getByRole('textbox'),{target:{value:'Yes'}});fireEvent.click(confirm)
 await waitFor(()=>expect(jobsAPI).toHaveBeenCalledWith('occurrences/occ_one/cancel',{}))
 expect(jobsAPI).toHaveBeenCalledTimes(1)
})
it('checks the original operation without offering a blind retry',async()=>{
 vi.mocked(jobsAPI).mockResolvedValue({});show(<SourceProgress occurrence={{...occurrence,state:'needs-attention'}}/>);
 fireEvent.click(screen.getByText('Sources · 1'));fireEvent.click(screen.getByRole('button',{name:'Check status'}))
 await waitFor(()=>expect(jobsAPI).toHaveBeenCalledWith('occurrences/occ_one/reconcile',{}))
 expect(screen.queryByRole('button',{name:/retry/i})).toBeNull()
})
