import {act,cleanup,render,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {Tooltip} from 'radix-ui'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import type {ComponentProps} from 'react'
import type {PortableResearchControls} from './PortableResearchControls'
import {MappedInputFields} from './MappedInputFields'
import {jobsAPI} from './client'
import {prepareMappedInputs,PortableResearchRequired} from './mappedInputs'
import {reviewPortableResearch} from './portableResearch'
import {RetainedJSON} from './wire'
import type {MappedDraft} from './drafts'
import type {ExecutableDialect} from './dialectTypes'
import type {PackDocument} from '../mcp/types'
const held=vi.hoisted(()=>({props:undefined as ComponentProps<typeof PortableResearchControls>|undefined}))
vi.mock('./PortableResearchControls',()=>({PortableResearchControls:(props:ComponentProps<typeof PortableResearchControls>)=>{held.props=props;return null}}))
vi.mock('./client',()=>({jobsAPI:vi.fn()}))
vi.mock('./mappedInputs',async original=>({...await original<object>(),prepareMappedInputs:vi.fn()}))
vi.mock('./portableResearch',async original=>({...await original<object>(),reviewPortableResearch:vi.fn()}))
vi.mock('../dialect/authoring',()=>({checkDialect:vi.fn().mockResolvedValue({status:'complete',diagnostics:[]})}))
const mapping:ExecutableDialect={version:3,case:{parameters:{},facts:[],evidence:[]},sources:[{name:'research',kind:'research',question:'Assess',constraints:{},model:'model',maxAge:3600,evidence:[],fields:[],capabilities:['answer'],read:{copy:{facts:[],evidence:[]}}}]}
const imported={dialect:mapping,binding:{dialect:'sha256:'+'c'.repeat(64),sources:{}}}
const draft:MappedDraft={text:JSON.stringify(mapping,null,2),caseText:'{}',importText:JSON.stringify(imported)}
beforeEach(()=>vi.mocked(jobsAPI).mockResolvedValue([]))
afterEach(()=>{cleanup();held.props=undefined;vi.resetAllMocks()})
async function view(fixed?:ExecutableDialect,persist=vi.fn().mockResolvedValue(undefined)){
 const changed=vi.fn()
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false,gcTime:0}}})}><Tooltip.Provider><MappedInputFields doc={{rules:[],evidenceRequirements:[]} as unknown as PackDocument} packText="{}" fixed={fixed} draft={draft} disabled={false} onChange={vi.fn()} onDraftChange={changed} onPersistResearchDraft={persist}/></Tooltip.Provider></QueryClientProvider>)
 await waitFor(()=>expect(held.props?.currentContext).toBeTruthy())
 return {changed,persist}
}
it('binds an answer-only import with the required empty web pins, keeping the definition unchanged',async()=>{
 const {changed}=await view()
 act(()=>held.props!.onBind({profile:'local-model',profileDigest:'sha256:'+'a'.repeat(64)},'answer'))
 const saved=changed.mock.calls.at(-1)![0] as MappedDraft
 const parsed=JSON.parse(saved.importText!)
 expect(parsed.dialect).toEqual(imported.dialect)
 expect(parsed.binding.sources.research).toEqual({profile:'local-model',profileDigest:'sha256:'+'a'.repeat(64),webProfiles:{}})
})
it('refuses fixed release rebinding even through direct dispatch',async()=>{
 const {changed}=await view(mapping);changed.mockClear()
 expect(held.props!.allowBinding).toBe(false)
 act(()=>held.props!.onBind({profile:'different',profileDigest:'sha256:'+'a'.repeat(64)},'answer'))
 expect(changed).not.toHaveBeenCalled()
})
it('does not expose a runnable reference until its durable draft checkpoint succeeds',async()=>{
 let finish!:()=>void
 const persist=vi.fn((_draft:MappedDraft,_signal:AbortSignal)=>new Promise<void>(resolve=>{finish=resolve}))
 vi.mocked(prepareMappedInputs).mockRejectedValue(new PortableResearchRequired('research',new RetainedJSON('{}')))
 vi.mocked(reviewPortableResearch).mockResolvedValue({id:'research_'+'a'.repeat(32)} as never)
 await view(undefined,persist)
 let task!:Promise<void>
 act(()=>{task=held.props!.onReview({connection:'connection',revision:'revision',model:'model'},{records:1,modelCalls:8,pages:0,searches:0})})
 await waitFor(()=>expect(persist).toHaveBeenCalledTimes(1))
 expect(held.props!.reference).toBeUndefined()
 await act(async()=>{finish();await task})
 expect(held.props!.reference?.id).toBe('research_'+'a'.repeat(32))
 expect(persist.mock.calls[0]![0].researchRuns!.research!.id).toBe(held.props!.reference!.id)
})
