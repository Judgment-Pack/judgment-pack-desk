import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {PageBindingControls} from './PageBindingControls'
import {portableResearchAPI} from './portableResearch'

vi.mock('./portableResearch',()=>({portableResearchAPI:vi.fn()}))
const prefixes=['https://Docs.EXAMPLE:/guides/%7e','https://second.example/notes']
const review={revision:'review-digest',descriptor:{adapter:{name:'adapter-web',version:'fixture',digest:'sha256:adapter'}},profile:{endpointScope:prefixes}}
function view() {
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}}),onBind=vi.fn()
 const ui=(context='draft-a',disabled=false)=><QueryClientProvider client={client}><PageBindingControls context={context} disabled={disabled} onBind={onBind}/></QueryClientProvider>
 const rendered=render(ui())
 fireEvent.click(screen.getByText('Create page profile'))
 return {...rendered,ui,onBind}
}
beforeEach(()=>vi.mocked(portableResearchAPI).mockResolvedValue(review))
afterEach(()=>{cleanup();vi.resetAllMocks()})
it('reviews exact ordered prefixes before explicit immutable registration and binding',async()=>{
 const {onBind}=view()
 expect(portableResearchAPI).not.toHaveBeenCalled()
 fireEvent.change(screen.getByLabelText('Allowed page prefixes'),{target:{value:prefixes.join('\n')}})
 fireEvent.click(screen.getByRole('button',{name:'Review'}))
 await screen.findByText(prefixes[0])
 expect(portableResearchAPI).toHaveBeenCalledWith('/page-binding',{endpointScope:prefixes})
 expect(onBind).not.toHaveBeenCalled()
 vi.mocked(portableResearchAPI).mockResolvedValue({...review,entry:{profile:{id:'scoped-pages'},digest:'profile-digest'}})
 const register=screen.getByRole('button',{name:'Use page profile'})
 expect(register.className).toContain('small')
 fireEvent.click(register)
 await waitFor(()=>expect(onBind).toHaveBeenCalledWith({profile:'scoped-pages',profileDigest:'profile-digest'}))
 expect(portableResearchAPI).toHaveBeenLastCalledWith('/page-binding',{endpointScope:prefixes,reviewedDigest:'review-digest'})
 expect(vi.mocked(portableResearchAPI).mock.calls.every(([path])=>path==='/page-binding')).toBe(true)
})
it('requires an explicit review for the broad wildcard',async()=>{
 vi.mocked(portableResearchAPI).mockResolvedValue({...review,profile:{endpointScope:['https://*']}})
 view()
 fireEvent.change(screen.getByLabelText('Allowed page prefixes'),{target:{value:'https://*'}})
 expect(screen.queryByRole('button',{name:'Use page profile'})).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Review'}))
 await screen.findByText('This scope allows any page in the supported HTTPS form.')
 expect(portableResearchAPI).toHaveBeenCalledWith('/page-binding',{endpointScope:['https://*']})
})
it('invalidates review when scope bytes or order change',async()=>{
 view()
 const input=screen.getByLabelText('Allowed page prefixes')
 fireEvent.change(input,{target:{value:prefixes.join('\n')}})
 fireEvent.click(screen.getByRole('button',{name:'Review'}))
 await screen.findByRole('button',{name:'Use page profile'})
 fireEvent.change(input,{target:{value:[...prefixes].reverse().join('\n')}})
 expect(screen.queryByRole('button',{name:'Use page profile'})).toBeNull()
 expect(portableResearchAPI).toHaveBeenCalledTimes(1)
})
it('discards a late preview when its draft context changes',async()=>{
 let resolve!:(value:unknown)=>void
 vi.mocked(portableResearchAPI).mockImplementation(()=>new Promise(r=>{resolve=r}))
 const {rerender,ui,onBind}=view()
 fireEvent.change(screen.getByLabelText('Allowed page prefixes'),{target:{value:prefixes.join('\n')}})
 fireEvent.click(screen.getByRole('button',{name:'Review'}))
 rerender(ui('draft-b'))
 await act(async()=>resolve(review))
 expect(screen.queryByRole('button',{name:'Use page profile'})).toBeNull()
 expect(onBind).not.toHaveBeenCalled()
})
it('does not bind a completed registration after its draft is changed or locked',async()=>{
 const {rerender,ui,onBind}=view()
 fireEvent.change(screen.getByLabelText('Allowed page prefixes'),{target:{value:prefixes.join('\n')}})
 fireEvent.click(screen.getByRole('button',{name:'Review'}))
 await screen.findByRole('button',{name:'Use page profile'})
 let resolve!:(value:unknown)=>void
 vi.mocked(portableResearchAPI).mockImplementation(()=>new Promise(r=>{resolve=r}))
 fireEvent.click(screen.getByRole('button',{name:'Use page profile'}))
 rerender(ui('draft-b',true))
 await act(async()=>resolve({...review,entry:{profile:{id:'scoped-pages'},digest:'profile-digest'}}))
 expect(onBind).not.toHaveBeenCalled()
 expect((screen.getByRole('button',{name:'Review'}) as HTMLButtonElement).disabled).toBe(true)
})
it('does not bind a registration Runner lists as set aside, and says why',async()=>{
 const {onBind}=view()
 fireEvent.change(screen.getByLabelText('Allowed page prefixes'),{target:{value:prefixes[0]}})
 fireEvent.click(screen.getByRole('button',{name:'Review'}))
 await screen.findByRole('button',{name:'Use page profile'})
 vi.mocked(portableResearchAPI).mockResolvedValue({...review,entry:{profile:{id:'old-pages'},digest:'profile-digest',state:'set-aside',reason:'its scope is refused'}})
 fireEvent.click(screen.getByRole('button',{name:'Use page profile'}))
 await screen.findByText('The input profile old-pages was set aside by Runner and cannot be bound. Runner’s reason: its scope is refused. Choose another profile for this source.')
 expect(onBind).not.toHaveBeenCalled()
})
