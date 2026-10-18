import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {PortableResearchControls} from './PortableResearchControls'
import {portableResearchAPI,type PortableResearchRun} from './portableResearch'
import type {ResearchSource} from '../dialect/types'
vi.mock('./portableResearch',async original=>({...await original<object>(),portableResearchAPI:vi.fn(),registerResearchModel:vi.fn()}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({})}))
vi.mock('../assistant/AIConnectionPicker',()=>({AIConnectionPicker:()=>null}))
vi.mock('../assistant/aiConnections',()=>({resolveAIConnection:()=>({connection:{id:'connection',revision:'revision'},assistant:{engine:'vercel',endpoint:{models:['generic-model']}}})}))
const source:ResearchSource={name:'procurement',kind:'research',model:'generic-model',question:'Assess the record',constraints:{},maxAge:3600,evidence:[],fields:[{name:'eligible',type:'boolean',required:true}],capabilities:['answer'],read:{copy:{facts:[],evidence:[]}}}
const reference={id:'research_'+'a'.repeat(32),context:'b'.repeat(64)}
function run(state:PortableResearchRun['state']='review'):PortableResearchRun{return {id:reference.id,revision:'revision',state,createdAt:'2026-10-16T00:00:00Z',choice:{connection:'connection',revision:'revision',model:'generic-model'},limits:{records:1,modelCalls:8,pages:0,searches:0},plan:{name:source.name,kind:'research',arguments:{question:'Assess this procurement record',constraints:{},fields:source.fields,capabilities:['answer']}},inputText:'{}',responseText:state==='completed'?'{}':'',activity:{id:reference.id,state,updatedAt:'2026-10-16T00:00:00Z',recordCount:0,stopRequested:false,steps:[]}}}
function view(context=reference.context){const cache=new QueryClient({defaultOptions:{queries:{retry:false}}});const review=vi.fn();return {review,...render(<QueryClientProvider client={cache}><PortableResearchControls source={source} reference={reference} currentContext={context} profiles={[]} disabled={false} onBind={vi.fn()} onReview={review}/></QueryClientProvider>)}}
beforeEach(()=>vi.mocked(portableResearchAPI).mockResolvedValue(run()))
afterEach(()=>{cleanup();vi.resetAllMocks()})
it('restores a saved review after remount, and starts only on its explicit compact action',async()=>{
 const first=view();await screen.findByText('Assess this procurement record');first.unmount()
 view();await screen.findByText('Assess this procurement record')
 expect(vi.mocked(portableResearchAPI).mock.calls.every(call=>call[1]===undefined)).toBe(true)
 const button=screen.getByRole('button',{name:'Run research'})
 expect(button.className).toContain('small')
 vi.mocked(portableResearchAPI).mockResolvedValue(run('queued'))
 fireEvent.click(button)
 await waitFor(()=>expect(portableResearchAPI).toHaveBeenCalledWith('/'+reference.id+'/confirm',{},'revision'))
 expect(await screen.findByRole('button',{name:'Stop'})).toBeTruthy()
})
it('keeps old run history readable but refuses execution against changed draft inputs',async()=>{
 view('c'.repeat(64));await screen.findByText('Assess this procurement record')
 expect((screen.getByRole('button',{name:'Run research'}) as HTMLButtonElement).disabled).toBe(true)
 expect(screen.getByText('Research was reviewed for different inputs. Review research again.')).toBeTruthy()
 expect(vi.mocked(portableResearchAPI).mock.calls.every(call=>call[1]===undefined)).toBe(true)
})
it('reloads a completed result without queueing or replaying it',async()=>{
 vi.mocked(portableResearchAPI).mockResolvedValue(run('completed'))
 view();await screen.findByText('Result retained. Read sources and preview to verify mapped values.')
 expect(screen.queryByRole('button',{name:'Run research'})).toBeNull()
 expect(vi.mocked(portableResearchAPI).mock.calls.every(call=>call[1]===undefined)).toBe(true)
})
it('shows the exact proposed allowance before approving a paused run',async()=>{
 const paused=run('paused')
 paused.activity.budgetRequest={id:'budget-a',state:'pending',createdAt:paused.createdAt,reason:'More requests needed',current:paused.limits,proposed:{...paused.limits,modelCalls:14},used:{records:0,modelCalls:8,pages:0,searches:0}}
 vi.mocked(portableResearchAPI).mockResolvedValue(paused)
 view();await screen.findByRole('table',{name:'Research request allowance'})
 expect(vi.mocked(portableResearchAPI).mock.calls.every(call=>call[1]===undefined)).toBe(true)
 vi.mocked(portableResearchAPI).mockResolvedValue(run('queued'))
 fireEvent.click(screen.getByRole('button',{name:'Approve and continue'}))
 await waitFor(()=>expect(portableResearchAPI).toHaveBeenCalledWith('/'+reference.id+'/budget-decision',{requestId:'budget-a',decision:'approve'},'revision'))
})

it('offers the shared page producer and binds its registered profile without starting research',async()=>{
 const onBind=vi.fn(),onReview=vi.fn(),cache=new QueryClient({defaultOptions:{queries:{retry:false}}})
 const pageSource:ResearchSource={...source,capabilities:['answer','page']}
 const preview={revision:'page-review',descriptor:{adapter:{name:'adapter-web',version:'fixture'}},profile:{endpointScope:['https://docs.example/guides']}}
 vi.mocked(portableResearchAPI).mockResolvedValue(preview as any)
 render(<QueryClientProvider client={cache}><PortableResearchControls source={pageSource} currentContext="page-context" profiles={[]} disabled={false} onBind={onBind} onReview={onReview}/></QueryClientProvider>)
 fireEvent.change(screen.getByLabelText('Allowed page prefixes'),{target:{value:'https://docs.example/guides'}})
 fireEvent.click(screen.getByRole('button',{name:'Review'}))
 await screen.findByRole('button',{name:'Use page profile'})
 vi.mocked(portableResearchAPI).mockResolvedValue({...preview,entry:{profile:{id:'page-profile'},digest:'page-digest'}} as any)
 fireEvent.click(screen.getByRole('button',{name:'Use page profile'}))
 await waitFor(()=>expect(onBind).toHaveBeenCalledWith({profile:'page-profile',profileDigest:'page-digest'},'page'))
 expect(onReview).not.toHaveBeenCalled()
 expect(pageSource.model).toBe('generic-model')
 expect(pageSource.capabilities).toEqual(['answer','page'])
 expect(vi.mocked(portableResearchAPI).mock.calls.every(([path])=>path==='/page-binding')).toBe(true)
})

it('discards late page registration when the current page pin changes before the input context hash',async()=>{
 const onBind=vi.fn(),cache=new QueryClient({defaultOptions:{queries:{retry:false}}})
 const preview={revision:'page-review',descriptor:{adapter:{name:'adapter-web',version:'fixture'}},profile:{endpointScope:['https://docs.example/guides']}}
 vi.mocked(portableResearchAPI).mockResolvedValue(preview as any)
 const ui=(pin?:{profile:string;profileDigest:string})=><QueryClientProvider client={cache}><PortableResearchControls source={{...source,capabilities:['answer','page'],webProfiles:pin?{page:pin}:{}}} currentContext="same-input-hash" profiles={[]} disabled={false} onBind={onBind} onReview={vi.fn()}/></QueryClientProvider>
 const view=render(ui())
 fireEvent.change(screen.getByLabelText('Allowed page prefixes'),{target:{value:'https://docs.example/guides'}})
 fireEvent.click(screen.getByRole('button',{name:'Review'}))
 await screen.findByRole('button',{name:'Use page profile'})
 let finish!:(value:unknown)=>void
 vi.mocked(portableResearchAPI).mockImplementation(()=>new Promise(resolve=>{finish=resolve}))
 fireEvent.click(screen.getByRole('button',{name:'Use page profile'}))
 view.rerender(ui({profile:'newer-pages',profileDigest:'newer-digest'}))
 await act(async()=>finish({...preview,entry:{profile:{id:'late-pages'},digest:'late-digest'}}))
 expect(onBind).not.toHaveBeenCalled()
})

const scoped=(id:string,endpointScope:string[])=>({digest:'sha256:'+id,profile:{id,class:'generated',source:'web',shape:'http',endpoint:null,endpointScope,research:{capabilities:['page']}}}) as any
const pagePin=(id:string)=>({profile:id,profileDigest:'sha256:'+id}) as any
function pageView(pin:any,profiles:any[],context=reference.context){const cache=new QueryClient({defaultOptions:{queries:{retry:false}}});return render(<QueryClientProvider client={cache}><PortableResearchControls source={{...source,capabilities:['answer','page'],webProfiles:pin?{page:pin}:{}}} reference={reference} currentContext={context} profiles={profiles} disabled={false} onBind={vi.fn()} onReview={vi.fn()}/></QueryClientProvider>)}
const scopeList=()=>screen.getAllByRole('list',{name:'Page scope'})
const reviewedList=()=>screen.getByRole('list',{name:'Page scope of this reviewed run'})
it('shows the chosen page profile\'s exact scope where it is selected, and warns for https://*',async()=>{
 vi.mocked(portableResearchAPI).mockResolvedValue(run('completed'))
 const view=pageView(pagePin('narrow'),[scoped('narrow',['https://Docs.EXAMPLE:/guides/%7e','https://second.example/notes']),scoped('wide',['https://*'])])
 await screen.findByText('Result retained. Read sources and preview to verify mapped values.')
 expect(scopeList()).toHaveLength(1)
 expect(scopeList()[0].textContent).toBe('https://Docs.EXAMPLE:/guides/%7ehttps://second.example/notes')
 expect(screen.queryByText('This scope includes https://*: any HTTPS page may be read.')).toBeNull()
 view.unmount()
 pageView(pagePin('wide'),[scoped('narrow',['https://docs.example/guides']),scoped('wide',['https://*'])])
 await screen.findByText('Result retained. Read sources and preview to verify mapped values.')
 expect(scopeList()[0].textContent).toBe('https://*')
 expect(screen.getByText('This scope includes https://*: any HTTPS page may be read.')).toBeTruthy()
})
it('matches the chosen page profile by digest, and names an exact endpoint where there is no scope',async()=>{
 vi.mocked(portableResearchAPI).mockResolvedValue(run('completed'))
 const view=pageView({profile:'narrow',profileDigest:'sha256:other'},[scoped('narrow',['https://docs.example/guides'])])
 await screen.findByText('The scope of this page profile cannot be shown: it is not among the installed profiles.')
 expect(screen.queryByRole('list',{name:'Page scope'})).toBeNull()
 view.unmount()
 pageView(pagePin('legacy'),[{digest:'sha256:legacy',profile:{id:'legacy',class:'generated',shape:'http',endpoint:'https://legacy.example/final',research:{capabilities:['page']}}}])
 await screen.findByText('No page scope. Runner accepts a page only at this exact endpoint: https://legacy.example/final')
})
it('shows the scope frozen into the reviewed run before it is confirmed',async()=>{
 const frozen=(inputText:string)=>{const held=run('review');held.plan.arguments.capabilities=['answer','page'];held.limits.pages=3;held.inputText=inputText;return held}
 const profiles=[scoped('reviewed',['https://*']),scoped('current',['https://docs.example/guides'])]
 // The reviewed run froze a wildcard profile; the draft now names another.
 vi.mocked(portableResearchAPI).mockResolvedValue(frozen(JSON.stringify({source:{mapping:{sources:[{name:source.name,webProfiles:{page:pagePin('reviewed')}}]}}})))
 const view=pageView(pagePin('current'),profiles,reference.context)
 await screen.findByRole('button',{name:'Run research'})
 expect(scopeList().map(list=>list.textContent)).toEqual(['https://docs.example/guides'])
 expect(reviewedList().textContent).toBe('https://*')
 expect(screen.getByText('This scope includes https://*: any HTTPS page may be read.')).toBeTruthy()
 view.unmount()
 // An imported definition's binding is read as the Desk reads it.
 vi.mocked(portableResearchAPI).mockResolvedValue(frozen(JSON.stringify({source:{import:{binding:{sources:{[source.name]:{webProfiles:{page:pagePin('current')}}}}},mapping:{sources:[{name:source.name,webProfiles:{page:pagePin('reviewed')}}]}}})))
 pageView(pagePin('current'),profiles,reference.context)
 await screen.findByRole('button',{name:'Run research'})
 expect(scopeList().map(list=>list.textContent)).toEqual(['https://docs.example/guides'])
 expect(reviewedList().textContent).toBe('https://docs.example/guides')
 expect(screen.queryByText('This scope includes https://*: any HTTPS page may be read.')).toBeNull()
 expect(vi.mocked(portableResearchAPI).mock.calls.every(call=>call[1]===undefined)).toBe(true)
})
it('labels the reviewed run\'s frozen scope apart from the picker\'s while it waits for confirmation',async()=>{
 const held=run('review');held.plan.arguments.capabilities=['answer','page'];held.limits.pages=3
 held.inputText=JSON.stringify({source:{mapping:{sources:[{name:source.name,webProfiles:{page:pagePin('reviewed')}}]}}})
 vi.mocked(portableResearchAPI).mockResolvedValue(held)
 pageView(pagePin('current'),[scoped('reviewed',['https://*']),scoped('current',['https://docs.example/guides'])])
 const confirm=await screen.findByRole('button',{name:'Run research'})
 expect((confirm as HTMLButtonElement).disabled).toBe(false)
 expect(screen.getByText('Page scope of this reviewed run')).toBeTruthy()
 expect(screen.getByRole('list',{name:'Page scope'}).textContent).toBe('https://docs.example/guides')
 expect(reviewedList().textContent).toBe('https://*')
 expect(screen.getAllByRole('list',{name:/^Page scope/})).toHaveLength(2)
})
it('shows the cannot-be-shown notice, not an empty list, for an empty scope',async()=>{
 vi.mocked(portableResearchAPI).mockResolvedValue(run('completed'))
 pageView(pagePin('empty'),[scoped('empty',[])])
 await screen.findByText('The scope of this page profile cannot be shown: it is not among the installed profiles.')
 expect(screen.queryByRole('list',{name:'Page scope'})).toBeNull()
 expect(screen.queryByText('Requests outside these prefixes are refused.')).toBeNull()
})
it('shows the cannot-be-shown notice, not a blank endpoint, for a profile with neither scope nor endpoint',async()=>{
 vi.mocked(portableResearchAPI).mockResolvedValue(run('completed'))
 pageView(pagePin('bare'),[{digest:'sha256:bare',profile:{id:'bare',class:'generated',shape:'http',endpoint:null,research:{capabilities:['page']}}}])
 await screen.findByText('The scope of this page profile cannot be shown: it is not among the installed profiles.')
 expect(screen.queryByText(/exact endpoint/)).toBeNull()
 expect(screen.queryByRole('list',{name:'Page scope'})).toBeNull()
})

const setAsideReason='the input profile old-pages: an endpointScope entry is not an endpoint prefix in the constrained page form, nor the wildcard https://* (input-malformed)'
const setAside=(id:string,endpointScope:string[])=>({...scoped(id,endpointScope),state:'set-aside',reason:setAsideReason})
const setAsideRefusal='The input profile old-pages was set aside by Runner and cannot be bound: '+setAsideReason+'. Choose another profile for this source, or upgrade the job to a release that does not name it.'
it('shows a set-aside page profile in the picker with Runner\'s reason, and does not let it be chosen',async()=>{
 vi.mocked(portableResearchAPI).mockResolvedValue(run('completed'))
 const onBind=vi.fn(),cache=new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<QueryClientProvider client={cache}><PortableResearchControls source={{...source,capabilities:['answer','page']}} reference={reference} currentContext={reference.context} profiles={[scoped('current-pages',['https://docs.example/guides']),setAside('old-pages',['https://a.example/api/v4/projects/group%2Fproject'])]} disabled={false} onBind={onBind} onReview={vi.fn()}/></QueryClientProvider>)
 await screen.findByText('Result retained. Read sources and preview to verify mapped values.')
 fireEvent.click(screen.getByRole('combobox',{name:'Page profile'}))
 const aside=await screen.findByRole('option',{name:'old-pages · Set aside by Runner: '+setAsideReason})
 expect(aside.getAttribute('aria-disabled')).toBe('true')
 fireEvent.click(aside)
 expect(onBind).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('option',{name:'current-pages'}))
 await waitFor(()=>expect(onBind).toHaveBeenCalledWith({profile:'current-pages',profileDigest:'sha256:current-pages'},'page'))
 expect(onBind).toHaveBeenCalledTimes(1)
})
it('shows Runner\'s reason beside the scope where the bound or the frozen page pin names a set-aside profile',async()=>{
 const held=run('review');held.plan.arguments.capabilities=['answer','page'];held.limits.pages=3
 held.inputText=JSON.stringify({source:{mapping:{sources:[{name:source.name,webProfiles:{page:pagePin('old-pages')}}]}}})
 vi.mocked(portableResearchAPI).mockResolvedValue(held)
 const view=pageView(pagePin('current-pages'),[scoped('current-pages',['https://docs.example/guides']),setAside('old-pages',['https://a.example/api/v4/projects/group%2Fproject'])])
 await screen.findByRole('button',{name:'Run research'})
 // The reviewed run's frozen scope, with the reason it is set aside beside it.
 const frozen=reviewedList().closest('div')!
 expect(reviewedList().textContent).toBe('https://a.example/api/v4/projects/group%2Fproject')
 expect(frozen.textContent).toContain(setAsideRefusal)
 // The draft's installed pin shows its scope with no such reason.
 expect(screen.getByRole('list',{name:'Page scope'}).closest('div')!.textContent).not.toContain('set aside')
 expect(screen.getAllByText(setAsideRefusal)).toHaveLength(1)
 view.unmount()
 // The draft's own pin naming it shows the same reason where it is chosen.
 vi.mocked(portableResearchAPI).mockResolvedValue(run('completed'))
 pageView(pagePin('old-pages'),[setAside('old-pages',['https://a.example/api/v4/projects/group%2Fproject'])])
 await screen.findByText('Result retained. Read sources and preview to verify mapped values.')
 expect(screen.getByRole('list',{name:'Page scope'}).closest('div')!.textContent).toContain(setAsideRefusal)
})
