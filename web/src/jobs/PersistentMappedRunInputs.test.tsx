import {cleanup,render,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {afterEach,expect,it,vi} from 'vitest'
import type {ComponentProps} from 'react'
import {PersistentMappedRunInputs,runInputDraftPath} from './PersistentMappedRunInputs'
import {readFile,writeFile,FileRequestError} from '../files/client'
import type {Job,Release} from './client'
import type {MappedInputFields} from './MappedInputFields'
const held=vi.hoisted(()=>({props:undefined as ComponentProps<typeof MappedInputFields>|undefined}))
vi.mock('./MappedInputFields',()=>({MappedInputFields:(props:ComponentProps<typeof MappedInputFields>)=>{held.props=props;return null}}))
vi.mock('../files/client',async original=>({...await original<object>(),readFile:vi.fn(),writeFile:vi.fn()}))
const job={id:'job_'+'a'.repeat(32),name:'Generic intake'} as Job
const mapping={version:3 as const,case:{parameters:{},facts:[],evidence:[]},sources:[]}
const release={id:'release_'+'b'.repeat(32),inputMapping:mapping,pack:'{}'} as unknown as Release
function view(){return render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false,gcTime:0}}})}><PersistentMappedRunInputs job={job} release={release} mapping={mapping} disabled={false} onChange={vi.fn()} onWorkChange={vi.fn()}/></QueryClientProvider>)}
afterEach(()=>{cleanup();held.props=undefined;vi.resetAllMocks()})
it('stores run references before admission and restores them for a fixed release after refresh',async()=>{
 vi.mocked(readFile).mockRejectedValue(new FileRequestError(404,'not found','chassis'))
 let file:Awaited<ReturnType<typeof writeFile>>|undefined
 vi.mocked(writeFile).mockImplementation(async input=>(file={path:input.path,content:input.content,sha256:'saved',bytes:input.content.length}))
 const first=view();await waitFor(()=>expect(held.props).toBeDefined())
 const draft={text:JSON.stringify(mapping,null,2),caseText:'{"record":"procurement"}',researchRuns:{review:{id:'research_'+'c'.repeat(32),context:'d'.repeat(64)}}}
 await held.props!.onPersistResearchDraft!(draft,new AbortController().signal)
 expect(writeFile).toHaveBeenCalledWith(expect.objectContaining({path:runInputDraftPath(job.id,release.id),baseSha256:''}))
 first.unmount();held.props=undefined
 vi.mocked(readFile).mockResolvedValue(file!)
 view();await waitFor(()=>expect(held.props?.draft).toEqual(draft))
 expect(held.props!.fixed).toEqual(mapping)
 expect(writeFile).toHaveBeenCalledTimes(1)
})
it('does not mount an actionable form when retained inputs cannot be read',async()=>{
 vi.mocked(readFile).mockRejectedValue(new FileRequestError(503,'busy','chassis'))
 const screen=view();await screen.findByText('Saved run inputs could not be loaded.')
 expect(held.props).toBeUndefined();expect(writeFile).not.toHaveBeenCalled()
})
it('refuses a path escape in a server resource identity',()=>{
 expect(()=>runInputDraftPath('../outside',release.id)).toThrow('Invalid')
 expect(()=>runInputDraftPath(job.id,'../outside')).toThrow('Invalid')
})
