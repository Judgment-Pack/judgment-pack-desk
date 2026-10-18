import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {TemplateConfiguration} from './TemplateConfiguration'
import {jobsAPI} from '../jobs/client'
import {saveJobDraft} from '../jobs/drafts'
import {portableResearchAPI} from '../jobs/portableResearch'
import {exchangeAPI,type Candidate,type ImportRecord} from './client'
const mocks=vi.hoisted(()=>({navigate:vi.fn(),clear:vi.fn()}))
vi.mock('react-router-dom',async original=>({...await original<object>(),useNavigate:()=>mocks.navigate}))
vi.mock('../mcp/queries',()=>({usePacks:()=>({data:{packs:[{id:'classification-pack'}]}}),usePack:(id:string)=>({data:id?{raw:JSON.stringify({id,version:'1.0.0'}),document:{id,version:'1.0.0',rules:[],outcomes:[]}}:undefined})}))
vi.mock('../jobs/client',()=>({jobsAPI:vi.fn()}))
vi.mock('./client',()=>({exchangeAPI:vi.fn()}))
vi.mock('../jobs/drafts',()=>({saveJobDraft:vi.fn()}))
vi.mock('../jobs/portableResearch',()=>({portableResearchAPI:vi.fn()}))
vi.mock('../shell/UnsavedChanges',()=>({useRegisteredChanges:()=>mocks.clear}))
vi.mock('../ui/Select',()=>({Select:({options,onValueChange,...props}:any)=><select {...props} onChange={e=>onValueChange(e.target.value)}><option value=""/>{options.map((o:any)=><option key={o.value} value={o.value}>{o.label}</option>)}</select>}))
const digest='sha256:'+'a'.repeat(64)
const origin={id:'b'.repeat(64)} as ImportRecord
const source={name:'classification',kind:'research',maxAge:300,model:'model-a',question:'Classify a dataset',constraints:{},evidence:[],fields:[{name:'classification',type:'text',required:true,description:'Dataset classification'}],capabilities:['answer'],read:{copy:{facts:[],evidence:[]}}}
const candidate=(sources:unknown[]):Candidate=>({row:{id:'dataset',entryDigest:digest,dialectDigest:digest},entry:{title:'Dataset governance',author:'Example',licence:'Apache-2.0',sourceKinds:['research']},files:{},document:JSON.stringify({version:3,case:{facts:[],evidence:[]},sources}),digest})
let cache:QueryClient
beforeEach(()=>{vi.resetAllMocks();vi.mocked(exchangeAPI).mockResolvedValue({status:'checked'});cache=new QueryClient({defaultOptions:{queries:{retry:false}}});vi.mocked(jobsAPI).mockResolvedValue([{digest,profile:{id:'local-ai',class:'generated',research:{capabilities:['answer'],model:'model-a'}}}]);vi.mocked(saveJobDraft).mockImplementation(async(id,values)=>({draft:{id,values}} as any))})
afterEach(()=>{cleanup();cache.clear()})
const show=(value:Candidate)=>render(<MemoryRouter><QueryClientProvider client={cache}><TemplateConfiguration candidate={value} origin={origin}/></QueryClientProvider></MemoryRouter>)
it('configures a non-GTM research preset without rewriting its model or requested fields',async()=>{
 const value=candidate([source]);show(value)
 fireEvent.change(screen.getByLabelText('Pack'),{target:{value:'classification-pack'}})
 await screen.findByRole('option',{name:'local-ai'})
 fireEvent.change(screen.getByLabelText('classification · answer'),{target:{value:'local-ai'}})
 const save=screen.getByRole('button',{name:'Create job draft'}) as HTMLButtonElement
 await waitFor(()=>expect(save.disabled).toBe(false));fireEvent.click(save)
 await waitFor(()=>expect(saveJobDraft).toHaveBeenCalledTimes(1))
 const values=vi.mocked(saveJobDraft).mock.calls[0][1]
 expect(exchangeAPI).toHaveBeenCalledWith('imports/'+origin.id+'/check',{pack:JSON.stringify({id:'classification-pack',version:'1.0.0'}),document:value.document})
 expect(values.mapped?.text).toBe(value.document)
 expect(values.mapped?.exchangeImportId).toBe(origin.id)
 const imported=JSON.parse(values.mapped!.importText!)
 expect(imported.dialect).toEqual(JSON.parse(value.document))
 expect(imported.binding).toEqual({dialect:digest,sources:{classification:{profile:'local-ai',profileDigest:digest,webProfiles:{}}}})
 expect(jobsAPI).toHaveBeenCalledTimes(1)
 expect(mocks.clear).toHaveBeenCalled();expect(mocks.navigate).toHaveBeenCalledWith(expect.stringMatching(/^\/jobs\/new\?draft=/))
})
it('keeps local-file-only definitions frozen without inventing an import binding',async()=>{
 show(candidate([{name:'records',kind:'selected-file',provider:'local-file',read:{copy:{facts:[],evidence:[]}}}]))
 fireEvent.change(screen.getByLabelText('Pack'),{target:{value:'classification-pack'}})
 const save=screen.getByRole('button',{name:'Create job draft'}) as HTMLButtonElement
 await waitFor(()=>expect(save.disabled).toBe(false));fireEvent.click(save)
 await waitFor(()=>expect(saveJobDraft).toHaveBeenCalledTimes(1))
 const values=vi.mocked(saveJobDraft).mock.calls[0][1]
 expect(values.mapped).not.toHaveProperty('importText');expect(values.mapped?.exchangeImportId).toBe(origin.id)
})
it('does not offer a mismatched model or create a runnable draft for unsupported kinds',async()=>{
 show(candidate([{...source,model:'model-b'}]))
 fireEvent.change(screen.getByLabelText('Pack'),{target:{value:'classification-pack'}})
 await waitFor(()=>expect(jobsAPI).toHaveBeenCalled())
 expect(screen.queryByRole('option',{name:'local-ai'})).toBeNull()
 expect((screen.getByRole('button',{name:'Create job draft'}) as HTMLButtonElement).disabled).toBe(true)
 cleanup();show(candidate([{name:'sql',kind:'sql',read:{copy:{facts:[],evidence:[]}}}]))
 expect(screen.getByText(/These source kinds are not supported/)).toBeTruthy()
 expect((screen.getByRole('button',{name:'Create job draft'}) as HTMLButtonElement).disabled).toBe(true)
})

it('does not save a draft or navigate when the authoritative pack check refuses it',async()=>{
 vi.mocked(exchangeAPI).mockRejectedValue(Error('Pack version does not match'))
 show(candidate([{name:'records',kind:'selected-file',provider:'local-file',read:{copy:{facts:[],evidence:[]}}}]))
 fireEvent.change(screen.getByLabelText('Pack'),{target:{value:'classification-pack'}})
 const save=screen.getByRole('button',{name:'Create job draft'}) as HTMLButtonElement
 await waitFor(()=>expect(save.disabled).toBe(false));fireEvent.click(save)
 await screen.findByText('Pack version does not match')
 expect(saveJobDraft).not.toHaveBeenCalled();expect(mocks.navigate).not.toHaveBeenCalled()
})
it('keeps an already bound research source pinned instead of asking for another connection',async()=>{
 show(candidate([{...source,profile:'pinned-ai',profileDigest:digest,webProfiles:{}}]))
 fireEvent.change(screen.getByLabelText('Pack'),{target:{value:'classification-pack'}})
 const save=screen.getByRole('button',{name:'Create job draft'}) as HTMLButtonElement
 await waitFor(()=>expect(save.disabled).toBe(false))
 expect(screen.queryByLabelText('classification · answer')).toBeNull()
 fireEvent.click(save);await waitFor(()=>expect(saveJobDraft).toHaveBeenCalledTimes(1))
 expect(vi.mocked(saveJobDraft).mock.calls[0][1].mapped).not.toHaveProperty('importText')
})

it('registers and binds a scoped page profile while preserving the imported definition',async()=>{
 const value=candidate([{...source,capabilities:['answer','page']}])
 const preview={revision:'page-review',descriptor:{adapter:{name:'adapter-web',version:'fixture'}},profile:{endpointScope:['https://docs.example/guides']}}
 vi.mocked(portableResearchAPI).mockResolvedValue(preview as any)
 show(value)
 fireEvent.change(screen.getByLabelText('Pack'),{target:{value:'classification-pack'}})
 await screen.findByRole('option',{name:'local-ai'})
 fireEvent.change(screen.getByLabelText('classification · answer'),{target:{value:'local-ai'}})
 fireEvent.change(screen.getByLabelText('Allowed page prefixes'),{target:{value:'https://docs.example/guides'}})
 fireEvent.click(screen.getByRole('button',{name:'Review'}))
 await screen.findByRole('button',{name:'Use page profile'})
 const existing=await vi.mocked(jobsAPI).mock.results[0].value
 vi.mocked(jobsAPI).mockResolvedValue([...existing,{digest:'page-digest',profile:{id:'page-profile',class:'generated',research:{capabilities:['page']}}}])
 vi.mocked(portableResearchAPI).mockResolvedValue({...preview,entry:{profile:{id:'page-profile'},digest:'page-digest'}} as any)
 fireEvent.click(screen.getByRole('button',{name:'Use page profile'}))
 const save=screen.getByRole('button',{name:'Create job draft'}) as HTMLButtonElement
 await waitFor(()=>expect(save.disabled).toBe(false))
 fireEvent.click(save)
 await waitFor(()=>expect(saveJobDraft).toHaveBeenCalledTimes(1))
 const mapped=vi.mocked(saveJobDraft).mock.calls[0][1].mapped!
 expect(mapped.text).toBe(value.document)
 const imported=JSON.parse(mapped.importText!)
 expect(imported.dialect).toEqual(JSON.parse(value.document))
 expect(imported.binding.sources.classification.webProfiles.page).toEqual({profile:'page-profile',profileDigest:'page-digest'})
 expect(vi.mocked(portableResearchAPI).mock.calls.every(([path])=>path==='/page-binding')).toBe(true)
})

it('keeps a newer page-picker choice when an earlier registration completes late',async()=>{
 const ai={digest,profile:{id:'local-ai',class:'generated',research:{capabilities:['answer'],model:'model-a'}}}
 const narrower={digest:'narrow-digest',profile:{id:'narrow-pages',class:'generated',research:{capabilities:['page']}}}
 vi.mocked(jobsAPI).mockResolvedValue([ai,narrower])
 const preview={revision:'page-review',descriptor:{adapter:{name:'adapter-web',version:'fixture'}},profile:{endpointScope:['https://docs.example/guides']}}
 vi.mocked(portableResearchAPI).mockResolvedValue(preview as any)
 show(candidate([{...source,capabilities:['answer','page']}]))
 fireEvent.change(screen.getByLabelText('Pack'),{target:{value:'classification-pack'}})
 await screen.findByRole('option',{name:'local-ai'})
 fireEvent.change(screen.getByLabelText('classification · answer'),{target:{value:'local-ai'}})
 fireEvent.change(screen.getByLabelText('Allowed page prefixes'),{target:{value:'https://docs.example/guides'}})
 fireEvent.click(screen.getByRole('button',{name:'Review'}))
 await screen.findByRole('button',{name:'Use page profile'})
 let finish!:(value:unknown)=>void
 vi.mocked(portableResearchAPI).mockImplementation(()=>new Promise(resolve=>{finish=resolve}))
 fireEvent.click(screen.getByRole('button',{name:'Use page profile'}))
 fireEvent.change(screen.getByLabelText('classification · page'),{target:{value:'narrow-pages'}})
 vi.mocked(jobsAPI).mockResolvedValue([ai,narrower,{digest:'late-digest',profile:{id:'late-pages',class:'generated',research:{capabilities:['page']}}}])
 await act(async()=>finish({...preview,entry:{profile:{id:'late-pages'},digest:'late-digest'}}))
 expect((screen.getByLabelText('classification · page') as HTMLSelectElement).value).toBe('narrow-pages')
 const save=screen.getByRole('button',{name:'Create job draft'}) as HTMLButtonElement
 await waitFor(()=>expect(save.disabled).toBe(false))
 fireEvent.click(save)
 await waitFor(()=>expect(saveJobDraft).toHaveBeenCalledTimes(1))
 const imported=JSON.parse(vi.mocked(saveJobDraft).mock.calls[0][1].mapped!.importText!)
 expect(imported.binding.sources.classification.webProfiles.page).toEqual({profile:'narrow-pages',profileDigest:'narrow-digest'})
})

it('shows the exact scope of the page profile chosen for a research source, and warns for https://*',async()=>{
 const ai={digest,profile:{id:'local-ai',class:'generated',research:{capabilities:['answer'],model:'model-a'}}}
 const page=(id:string,endpointScope:string[])=>({digest:'sha256:'+id,profile:{id,class:'generated',source:'web',shape:'http',endpoint:null,endpointScope,research:{capabilities:['page']}}})
 vi.mocked(jobsAPI).mockResolvedValue([ai,page('guides',['https://docs.example/guides','https://second.example/notes']),page('anything',['https://*'])])
 show(candidate([{...source,capabilities:['answer','page']}]))
 fireEvent.change(screen.getByLabelText('Pack'),{target:{value:'classification-pack'}})
 await screen.findByRole('option',{name:'guides'})
 expect(screen.queryByRole('list',{name:'Page scope'})).toBeNull()
 fireEvent.change(screen.getByLabelText('classification · page'),{target:{value:'guides'}})
 expect(screen.getByRole('list',{name:'Page scope'}).textContent).toBe('https://docs.example/guideshttps://second.example/notes')
 expect(screen.queryByText('This scope includes https://*: any HTTPS page may be read.')).toBeNull()
 fireEvent.change(screen.getByLabelText('classification · page'),{target:{value:'anything'}})
 expect(screen.getByRole('list',{name:'Page scope'}).textContent).toBe('https://*')
 expect(screen.getByText('This scope includes https://*: any HTTPS page may be read.')).toBeTruthy()
 expect(portableResearchAPI).not.toHaveBeenCalled()
})
