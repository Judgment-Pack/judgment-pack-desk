import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {TemplateConfiguration} from './TemplateConfiguration'
import {jobsAPI} from '../jobs/client'
import {saveJobDraft} from '../jobs/drafts'
import type {Candidate,ImportRecord} from './client'
const mocks=vi.hoisted(()=>({navigate:vi.fn(),clear:vi.fn()}))
vi.mock('react-router-dom',async original=>({...await original<object>(),useNavigate:()=>mocks.navigate}))
vi.mock('../mcp/queries',()=>({usePacks:()=>({data:{packs:[{id:'classification-pack'}]}}),usePack:(id:string)=>({data:id?{document:{id,version:'1.0.0',rules:[],outcomes:[]}}:undefined})}))
vi.mock('../jobs/client',()=>({jobsAPI:vi.fn()}))
vi.mock('../jobs/drafts',()=>({saveJobDraft:vi.fn()}))
vi.mock('../shell/UnsavedChanges',()=>({useRegisteredChanges:()=>mocks.clear}))
vi.mock('../ui/Select',()=>({Select:({options,onValueChange,...props}:any)=><select {...props} onChange={e=>onValueChange(e.target.value)}><option value=""/>{options.map((o:any)=><option key={o.value} value={o.value}>{o.label}</option>)}</select>}))
const digest='sha256:'+'a'.repeat(64)
const origin={id:'b'.repeat(64)} as ImportRecord
const source={name:'classification',kind:'research',maxAge:300,model:'model-a',question:'Classify a dataset',constraints:{},evidence:[],fields:[{name:'classification',type:'text',required:true,description:'Dataset classification'}],capabilities:['answer'],read:{copy:{facts:[],evidence:[]}}}
const candidate=(sources:unknown[]):Candidate=>({row:{id:'dataset',entryDigest:digest,dialectDigest:digest},entry:{title:'Dataset governance',author:'Example',licence:'Apache-2.0',sourceKinds:['research']},files:{},document:JSON.stringify({version:3,case:{facts:[],evidence:[]},sources}),digest})
let cache:QueryClient
beforeEach(()=>{vi.resetAllMocks();cache=new QueryClient({defaultOptions:{queries:{retry:false}}});vi.mocked(jobsAPI).mockResolvedValue([{digest,profile:{id:'local-ai',class:'generated',research:{capabilities:['answer'],model:'model-a'}}}]);vi.mocked(saveJobDraft).mockImplementation(async(id,values)=>({draft:{id,values}} as any))})
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
