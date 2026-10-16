import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {answer} from '../files/client'
import {TemplatesPage} from './TemplatesPage'
import {exchangeAPI,type CatalogReview,type Candidate} from './client'
const queryState=vi.hoisted(()=>({failure:false}))
vi.mock('./client',async original=>{const actual=await original<typeof import('./client')>();return {...actual,exchangeAPI:vi.fn(),exchangeImportsQuery:()=>({queryKey:['exchange-imports'],queryFn:async()=>({items:[],revision:'base'})}),exchangeImportQuery:(id:string)=>({queryKey:['exchange-imports',id],queryFn:async()=>{if(queryState.failure)throw Error('Refresh unavailable');return {import:{id,tag:'v1.0.0',commit:'a'.repeat(40)},candidate}}})}})
vi.mock('../shell/QueryStatusNotice',()=>({QueryStatusNotice:({error,onRetry}:{error?:Error;onRetry:()=>void})=>error?<div role="alert">{error.message}<button onClick={onRetry}>Retry</button></div>:null}))
const candidate:Candidate={row:{id:'example.non-gtm',entryDigest:'sha256:file',dialectDigest:'sha256:dialect'},entry:{title:'Dataset classification',licence:'Apache-2.0',author:'Example',sourceKinds:['research']},files:{'dialect.json':'{"version":3}', 'entry.json':'{}','README.md':'Instructions'},document:'{"version":3}',digest:'sha256:canonical'}
const checked:CatalogReview={previous:{},review:{id:'review',base:'base',release:{location:'catalog',tag:'v1.0.0',commit:'a'.repeat(40),candidates:[candidate],unavailable:[]}},changes:{}}
let cache:QueryClient
beforeEach(()=>{vi.resetAllMocks();queryState.failure=false;cache=new QueryClient({defaultOptions:{queries:{retry:false}}});vi.mocked(exchangeAPI).mockImplementation(async path=>path==='review'?checked as any:{id:'saved'} as any)})
afterEach(()=>{cleanup();cache.clear()})
function show(){return render(<MemoryRouter><QueryClientProvider client={cache}><Tooltip.Provider><TemplatesPage/></Tooltip.Provider></QueryClientProvider></MemoryRouter>)}
async function check(){fireEvent.change(screen.getByLabelText('Release tag'),{target:{value:'v1.0.0'}});fireEvent.click(screen.getByRole('button',{name:'Review release'}));await screen.findByRole('button',{name:'Import template'})}
it('imports only exact checked identity on explicit action, with no binding or run request',async()=>{
 show();await check();expect(vi.mocked(exchangeAPI).mock.calls).toHaveLength(1)
 fireEvent.click(screen.getByRole('button',{name:'Import template'}));await screen.findByText('Template imported')
 expect(vi.mocked(exchangeAPI).mock.calls[1]).toEqual(['imports',{reviewId:'review',entryId:'example.non-gtm',base:'base',reviewed:true}])
 expect(screen.queryByRole('button',{name:'Import template'})).toBeNull()
 expect(vi.mocked(exchangeAPI).mock.calls.map(([path])=>path)).toEqual(['review','imports'])
 expect(screen.getByText('Configuring and running imported presets is not available yet.')).toBeTruthy()
})
it('invalidates review when release selection changes and retains no import authority on reload',async()=>{
 const page=show();await check();fireEvent.change(screen.getByLabelText('Release tag'),{target:{value:'v1.1.0'}})
 expect(screen.queryByRole('button',{name:'Import template'})).toBeNull();page.unmount();show()
 expect(screen.queryByRole('button',{name:'Import template'})).toBeNull()
 expect(vi.mocked(exchangeAPI).mock.calls).toHaveLength(1)
})
it('reconciles an uncertain import only after explicit retry with the original checked request',async()=>{
 let calls=0;vi.mocked(exchangeAPI).mockImplementation(async path=>{if(path==='review')return checked as any;if(calls++===0)throw Error('Connection interrupted');return {id:'saved'} as any})
 show();await check();fireEvent.click(screen.getByRole('button',{name:'Import template'}));await screen.findByText('Connection interrupted')
 expect(vi.mocked(exchangeAPI).mock.calls).toHaveLength(2)
 fireEvent.click(screen.getByRole('button',{name:'Retry'}));await screen.findByText('Template imported')
 const writes=vi.mocked(exchangeAPI).mock.calls.filter(([path])=>path==='imports');expect(writes).toHaveLength(2);expect(writes[0][1]).toEqual(writes[1][1])
})
it('shows unavailable entries without offering them or deleting retained imports',async()=>{
 vi.mocked(exchangeAPI).mockResolvedValue({...checked,review:{...checked.review,release:{...checked.review.release,candidates:[],unavailable:[{id:'example.unsupported',reason:'Fixture replay unavailable'}]}},changes:{'old.entry':{withdrawn:true,files:[]}}})
 show();fireEvent.change(screen.getByLabelText('Release tag'),{target:{value:'v1.0.0'}});fireEvent.click(screen.getByRole('button',{name:'Review release'}));await screen.findByText(/Withdrawn from this release/)
 expect(screen.queryByRole('button',{name:'Import template'})).toBeNull();expect(vi.mocked(exchangeAPI).mock.calls).toHaveLength(1)
})

it('retires a consumed multi-entry review and requires a fresh base for the next import',async()=>{
 const two={...checked,review:{...checked.review,release:{...checked.review.release,candidates:[candidate,{...candidate,row:{...candidate.row,id:'example.second'},entry:{...candidate.entry,title:'Second template'}}]}}}
 vi.mocked(exchangeAPI).mockImplementation(async path=>path==='review'?two as any:{id:'saved'} as any)
 show();await check();fireEvent.click(screen.getByRole('button',{name:'Import template'}));await screen.findByText('Template imported')
 expect(screen.queryByLabelText('Template')).toBeNull();expect(screen.queryByRole('button',{name:'Import template'})).toBeNull()
 expect(screen.getByRole('button',{name:'Review release'})).toBeTruthy()
})
it('a definitive stale-base refusal offers re-review instead of repeating the refused write',async()=>{
 vi.mocked(exchangeAPI).mockImplementation(async path=>{if(path==='review')return checked as any;return answer(new Response(JSON.stringify({error:'Installed templates changed',code:'bad-request'}),{status:409}))})
 show();await check();fireEvent.click(screen.getByRole('button',{name:'Import template'}));await screen.findByText('Installed templates changed')
 expect(screen.queryByRole('button',{name:'Import template'})).toBeNull();expect(screen.queryByRole('button',{name:'Retry'})).toBeNull()
 fireEvent.click(screen.getAllByRole('button',{name:'Review release'})[0]);await screen.findByRole('button',{name:'Import template'})
 expect(vi.mocked(exchangeAPI).mock.calls.map(([path])=>path)).toEqual(['review','imports','review'])
})
it('keeps retained details visible after a background refresh fails',async()=>{
 show();await check();fireEvent.click(screen.getByRole('button',{name:'Import template'}));await screen.findByText('Template imported')
 await screen.findByRole('heading',{name:'Dataset classification'})
 queryState.failure=true;await cache.invalidateQueries({queryKey:['exchange-imports','saved']})
 await screen.findByText('Refresh unavailable')
 expect(screen.getByRole('heading',{name:'Dataset classification'})).toBeTruthy()
})
it('keeps the prior diff available after failed refresh while blocking another import',async()=>{
 vi.mocked(exchangeAPI).mockResolvedValue({...checked,previous:{'example.non-gtm':'prior'},changes:{'example.non-gtm':{files:[],withdrawn:false}}})
 show();await check();await screen.findByText('Review proposed changes')
 queryState.failure=true;await cache.invalidateQueries({queryKey:['exchange-imports','prior']})
 await screen.findByText('Refresh unavailable');expect(screen.getByText('Review proposed changes')).toBeTruthy()
 await waitFor(()=>expect((screen.getByRole('button',{name:'Import template'}) as HTMLButtonElement).disabled).toBe(true))
})

it('keeps exact retry identity when a successful response has an unreadable body',async()=>{
 let attempts=0;vi.mocked(exchangeAPI).mockImplementation(async path=>{if(path==='review')return checked as any;if(attempts++===0)return answer(new Response('interrupted',{status:201}));return {id:'saved'} as any})
 show();await check();fireEvent.click(screen.getByRole('button',{name:'Import template'}));await screen.findByRole('button',{name:'Retry'})
 fireEvent.click(screen.getByRole('button',{name:'Retry'}));await screen.findByText('Template imported')
 const writes=vi.mocked(exchangeAPI).mock.calls.filter(([path])=>path==='imports');expect(writes).toHaveLength(2);expect(writes[0][1]).toEqual(writes[1][1])
})
