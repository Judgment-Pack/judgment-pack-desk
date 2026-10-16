import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {Tooltip} from 'radix-ui'
import {afterEach,expect,it,vi} from 'vitest'
import {SubscriptionUpgradeRecovery} from './SubscriptionUpgradeRecovery'
import {dialectAPI,type DialectEvaluation} from './client'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
it('restores submitted intent without writing and separates receipt checks from an explicitly authorized exact retry',async()=>{
 const candidate={id:'eval',revision:'frozen',name:'Governance',upgrade:{phase:'submitted'},releaseId:'new-release',subscriptionUpgrade:{baseReleaseId:'old-release',reviewedAt:'2026-10-15T09:00:00Z',mode:'changed',sourceClass:'generated'}} as DialectEvaluation
 vi.mocked(dialectAPI).mockImplementation(async(_path,method)=>{if(!method)return candidate;throw Error('The upgrade result is still unknown.')})
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<QueryClientProvider client={client}><Tooltip.Provider><SubscriptionUpgradeRecovery id="eval" preparationId="prep" opener={{current:null}} onClose={()=>{}} onSaved={()=>{}}/></Tooltip.Provider></QueryClientProvider>)
 await screen.findByText('Governance');expect(screen.getByText('New and changed records')).toBeTruthy();expect(screen.getByText('AI research')).toBeTruthy();expect(document.querySelector('time')?.dateTime).toBe('2026-10-15T09:00:00Z');expect(vi.mocked(dialectAPI).mock.calls).toEqual([['/prep/evaluations/eval']])
 const retry=screen.getByRole('button',{name:'Retry exact upgrade'}) as HTMLButtonElement;expect(retry.disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Check result'}))
 await screen.findByText('The upgrade result is still unknown.')
 expect(dialectAPI).toHaveBeenCalledWith('/prep/evaluations/eval/subscription-upgrade','POST',{reviewed:true,action:'recover'},'frozen')
 expect(vi.mocked(dialectAPI).mock.calls.filter(c=>c[1]==='POST')).toHaveLength(1)
 fireEvent.click(screen.getByRole('checkbox',{name:'I reviewed this fixed release and paused subscription upgrade.'}))
 await waitFor(()=>expect(retry.disabled).toBe(false));fireEvent.click(retry)
 await waitFor(()=>expect(dialectAPI).toHaveBeenCalledWith('/prep/evaluations/eval/subscription-upgrade','POST',{reviewed:true,action:'retry'},'frozen'))
 expect(vi.mocked(dialectAPI).mock.calls.filter(c=>c[1]==='POST')).toHaveLength(2)
})
it('keeps an authoritative refusal visible and permits reconciliation of a partially retained refusal',async()=>{
 const candidate={id:'eval',revision:'frozen',name:'Governance',upgrade:{phase:'refused',problem:'Another upgrade won.'},releaseId:'new-release',subscriptionUpgrade:{baseReleaseId:'old-release'}} as DialectEvaluation
 vi.mocked(dialectAPI).mockResolvedValue(candidate)
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}}),onClose=vi.fn(),onSaved=vi.fn()
 render(<QueryClientProvider client={client}><Tooltip.Provider><SubscriptionUpgradeRecovery id="eval" preparationId="prep" opener={{current:null}} onClose={onClose} onSaved={onSaved}/></Tooltip.Provider></QueryClientProvider>)
 await screen.findByText('Another upgrade won.');fireEvent.click(screen.getByRole('button',{name:'Check result'}))
 await waitFor(()=>expect(onSaved).toHaveBeenCalled());expect(onClose).not.toHaveBeenCalled();expect(screen.getByText('Another upgrade won.')).toBeTruthy()
 expect(dialectAPI).toHaveBeenCalledWith('/prep/evaluations/eval/subscription-upgrade','POST',{reviewed:true,action:'recover'},'frozen')
})
