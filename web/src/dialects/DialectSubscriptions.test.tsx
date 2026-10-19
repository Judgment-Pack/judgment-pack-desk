import {cleanup,fireEvent,render,screen} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {afterEach,expect,it,vi} from 'vitest'
import {DialectSubscriptions} from './DialectSubscriptions'
import {dialectAPI,type DialectEvaluation} from './client'
import {DeskStatusNotice} from '../shell/DeskNotices'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
vi.mock('../shell/QueryStatusNotice',()=>({QueryStatusNotice:()=>null}))
vi.mock('../shell/DeskNotices',()=>({DeskStatusNotice:vi.fn(()=>null)}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
it('keeps receipt recovery reachable after the source completes but the replacement save is interrupted',async()=>{
 const source={id:'source',preparationId:'prep',state:'superseded',name:'Old subscription',subscription:{mode:'new',sourceClass:'asserted',enabled:false},successors:[{candidateId:'next',state:'complete'}]} as DialectEvaluation
 const candidate={id:'next',revision:'frozen',name:'Retained replacement',upgrade:{phase:'submitted',result:{}},releaseId:'new',subscriptionUpgrade:{baseReleaseId:'old'}} as DialectEvaluation
 vi.mocked(dialectAPI).mockResolvedValue(candidate)
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter><DialectSubscriptions evaluations={[source]}/></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 expect((screen.getByRole('button',{name:'Review and enable'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Review upgrade'}));await screen.findByText('Retained replacement')
 expect(screen.getByRole('button',{name:'Check result'})).toBeTruthy();expect(dialectAPI).toHaveBeenCalledWith('/prep/evaluations/next')
})
it('shows capacity waiting without a failure warning and keeps Pause available',()=>{
 const subscription={id:'source',preparationId:'prep',state:'queued',name:'Backfill',subscription:{mode:'changed',sourceClass:'asserted',enabled:true,capacityBackoff:256,capacityRetryAt:'2026-10-17T12:04:16Z'}} as DialectEvaluation
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 const {rerender}=render(<QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter><DialectSubscriptions evaluations={[subscription]}/></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 expect(screen.getByText(/Waiting for Runner capacity/)).toBeTruthy()
 expect((screen.getByRole('button',{name:'Pause'}) as HTMLButtonElement).disabled).toBe(false)
 expect(DeskStatusNotice).not.toHaveBeenCalled()
 rerender(<QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter><DialectSubscriptions evaluations={[{...subscription,state:'watching',subscription:{...subscription.subscription!,capacityBackoff:undefined,capacityRetryAt:undefined}}]}/></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 expect(screen.queryByText(/Waiting for Runner capacity/)).toBeNull()
 expect(screen.getByText(/Watching reviewed records/)).toBeTruthy()
})
it('keeps ordinary refusal warnings visible',()=>{
 const subscription={id:'source',preparationId:'prep',state:'blocked',name:'Backfill',problem:'Runner refused this request',subscription:{mode:'changed',sourceClass:'asserted',enabled:true,failures:5}} as DialectEvaluation
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter><DialectSubscriptions evaluations={[subscription]}/></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 expect(screen.getByText(/Needs attention/)).toBeTruthy()
 expect(screen.queryByText(/Waiting for Runner capacity/)).toBeNull()
 expect(DeskStatusNotice).toHaveBeenCalledWith(expect.objectContaining({tone:'warning',children:'Runner refused this request'}),undefined)
})
