import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter,useLocation} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {DialectRecipeNotice,RecipeReference} from './DialectRecipeArtifacts'
import {retainDialectProposal} from './drafts'
import type {DialectRecipeProposal} from './proposals'
vi.mock('./drafts',async original=>({...await original<typeof import('./drafts')>(),retainDialectProposal:vi.fn()}))
const proposal:DialectRecipeProposal={version:1,draftId:'draft-1',createdAt:'2026-01-01',name:'Research brief',intent:'Find records.',target:'records',fields:[{name:'description',description:'A description',type:'string'}],criteria:[],limits:{records:25,searches:0,pages:30,modelCalls:30}}
beforeEach(()=>{vi.mocked(retainDialectProposal).mockReset()})
afterEach(cleanup)
function Location(){const location=useLocation();return <output>{location.pathname+location.search}</output>}
function show(){render(<QueryClientProvider client={new QueryClient()}><Tooltip.Provider><MemoryRouter><DialectRecipeNotice chatId="chat-1" drafts={[proposal]}/><Location/></MemoryRouter></Tooltip.Provider></QueryClientProvider>)}
it('opens a durable full-page draft without a review dialog or runnable preparation',async()=>{
 let finish!:(value:any)=>void;vi.mocked(retainDialectProposal).mockImplementation(()=>new Promise(resolve=>{finish=resolve}))
 show();fireEvent.click(screen.getByRole('button',{name:'Review recipe'}));expect(screen.queryByRole('dialog')).toBeNull()
 expect(vi.mocked(retainDialectProposal).mock.calls).toEqual([['chat-1',proposal]])
 expect((screen.getByRole('button',{name:'Opening…'}) as HTMLButtonElement).disabled).toBe(true)
 await act(async()=>finish({sha256:'revision',document:{id:'draft_one',origin:{chatId:'chat-1'}}}))
 await waitFor(()=>expect(screen.getByRole('status').textContent).toBe('/dialects/drafts/draft_one?chat=chat-1'))
})
it('reopening a promoted draft goes to its existing dialect',async()=>{
 vi.mocked(retainDialectProposal).mockResolvedValue({sha256:'revision',document:{id:'draft_one',savedInputId:'prep_one',origin:{chatId:'chat-1',draftId:'draft-1'}}} as any)
 show();fireEvent.click(screen.getByRole('button',{name:'Review recipe'}))
 await waitFor(()=>expect(screen.getByRole('status').textContent).toBe('/dialects/prep_one?tab=recipe&chat=chat-1'))
})
it('opens older saved recipes together with the originating chat',()=>{
 const saved={...proposal,savedInputId:'prep_'+'b'.repeat(32)}
 render(<QueryClientProvider client={new QueryClient()}><Tooltip.Provider><MemoryRouter><RecipeReference chatId="chat-1" draft={saved}/></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 expect(screen.getByRole('link',{name:'Open dialect'}).getAttribute('href')).toBe('/dialects/'+saved.savedInputId+'?tab=recipe&chat=chat-1')
})
it('omits the saved proposal from the conversation tail',()=>{
 render(<DialectRecipeNotice chatId="chat-1" drafts={[proposal,{...proposal,savedInputId:'prep_one'}]}/>);expect(screen.queryByRole('button')).toBeNull()
})
