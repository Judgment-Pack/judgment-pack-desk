import {cleanup,fireEvent,render,screen} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {afterEach,expect,it,vi} from 'vitest'
import {DialectSubscriptions} from './DialectSubscriptions'
import {dialectAPI,type DialectEvaluation} from './client'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
vi.mock('../shell/QueryStatusNotice',()=>({QueryStatusNotice:()=>null}))
vi.mock('../shell/DeskNotices',()=>({DeskStatusNotice:()=>null}))
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
