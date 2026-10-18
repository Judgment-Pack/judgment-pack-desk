import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {SearchBindingControls} from './SearchBindingControls'
import {portableResearchAPI} from './portableResearch'
vi.mock('./portableResearch',()=>({portableResearchAPI:vi.fn()}))
vi.mock('../search/connections',()=>({useSearchConnections:()=>({available:true,loading:false,data:{connections:[{id:'local-search',revision:'revision-1',name:'Local search',research:{capabilities:['search']}}]}})}))
vi.mock('../ui/Select',()=>({Select:({options,onValueChange,...props}:any)=><select {...props} onChange={e=>onValueChange(e.target.value)}><option value=""/>{options.map((o:any)=><option key={o.value} value={o.value}>{o.label}</option>)}</select>}))
const review={revision:'review-digest',descriptor:{endpoint:'https://search.example.org',adapter:{name:'adapter-sources',version:'1',digest:'digest'},fixedArguments:{connection:'local-search',revision:'revision-1'}}}
function view(){const client=new QueryClient({defaultOptions:{queries:{retry:false}}}),onBind=vi.fn();const ui=(context='a',disabled=false)=><QueryClientProvider client={client}><SearchBindingControls context={context} disabled={disabled} onBind={onBind}/></QueryClientProvider>;return {...render(ui()),ui,onBind}}
beforeEach(()=>vi.mocked(portableResearchAPI).mockResolvedValue(review))
afterEach(()=>{cleanup();vi.resetAllMocks()})
it('reviews the exact search binding before explicit registration and never starts research',async()=>{
 const {onBind}=view();expect(portableResearchAPI).not.toHaveBeenCalled()
 fireEvent.change(screen.getByRole('combobox'),{target:{value:'local-search'}})
 fireEvent.click(screen.getByRole('button',{name:'Review'}))
 await screen.findByText(review.descriptor.endpoint)
 expect(onBind).not.toHaveBeenCalled()
 expect(portableResearchAPI).toHaveBeenCalledWith('/search-binding',{choice:{connection:'local-search',revision:'revision-1'}})
 vi.mocked(portableResearchAPI).mockResolvedValue({...review,entry:{profile:{id:'profile-id'},digest:'profile-digest'}})
 const button=screen.getByRole('button',{name:'Use connection'});expect(button.className).toContain('small');fireEvent.click(button)
 await waitFor(()=>expect(onBind).toHaveBeenCalledWith({profile:'profile-id',profileDigest:'profile-digest'}))
 expect(portableResearchAPI).toHaveBeenLastCalledWith('/search-binding',{choice:{connection:'local-search',revision:'revision-1'},reviewedDigest:'review-digest'})
})
it('discards late reviews when the draft context changes',async()=>{
 let resolve!:(v:unknown)=>void
 vi.mocked(portableResearchAPI).mockImplementation(()=>new Promise(r=>{resolve=r}))
 const {rerender,ui,onBind}=view()
 fireEvent.change(screen.getByRole('combobox'),{target:{value:'local-search'}});fireEvent.click(screen.getByRole('button',{name:'Review'}))
 rerender(ui('other'))
 await act(async()=>resolve(review))
 expect(screen.queryByRole('button',{name:'Use connection'})).toBeNull();expect(onBind).not.toHaveBeenCalled()
 expect((screen.getByRole('button',{name:'Review'}) as HTMLButtonElement).disabled).toBe(false)
})
it('does not apply a completed registration to a different draft or a locked review',async()=>{
 const {rerender,ui,onBind}=view()
 fireEvent.change(screen.getByRole('combobox'),{target:{value:'local-search'}});fireEvent.click(screen.getByRole('button',{name:'Review'}));await screen.findByRole('button',{name:'Use connection'})
 let resolve!:(v:unknown)=>void;vi.mocked(portableResearchAPI).mockImplementation(()=>new Promise(r=>{resolve=r}))
 fireEvent.click(screen.getByRole('button',{name:'Use connection'}));rerender(ui('other',true))
 await act(async()=>resolve({...review,entry:{profile:{id:'profile-id'},digest:'profile-digest'}}))
 expect(onBind).not.toHaveBeenCalled()
 expect((screen.getByRole('button',{name:'Review'}) as HTMLButtonElement).disabled).toBe(true)
})
it('does not bind a registration Runner lists as set aside, and says why',async()=>{
 const {onBind}=view()
 fireEvent.change(screen.getByRole('combobox'),{target:{value:'local-search'}});fireEvent.click(screen.getByRole('button',{name:'Review'}))
 await screen.findByRole('button',{name:'Use connection'})
 vi.mocked(portableResearchAPI).mockResolvedValue({...review,entry:{profile:{id:'old-search'},digest:'profile-digest',state:'set-aside',reason:'its form is refused'}})
 fireEvent.click(screen.getByRole('button',{name:'Use connection'}))
 await screen.findByText('The input profile old-search was set aside by Runner and cannot be bound: its form is refused. Choose another profile for this source, or upgrade the job to a release that does not name it.')
 expect(onBind).not.toHaveBeenCalled()
})
