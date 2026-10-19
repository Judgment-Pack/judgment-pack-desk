import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {PolicyComparison} from './PolicyComparison'
import {resolvePolicyComparison,type PolicyComparison as Comparison,type PolicyComparisonRequest} from './comparison'
import {forgetSession} from '../mcp/session'
vi.mock('./comparison',()=>({resolvePolicyComparison:vi.fn()}))
vi.mock('../ui/CodeDiff',()=>({CodeDiff:(props:{before:string;after:string;beforeLabel:string;afterLabel:string;language:string})=><div role="region" aria-label="Diff" data-language={props.language}><span>{props.beforeLabel}</span><pre>{props.before}</pre><span>{props.afterLabel}</span><pre>{props.after}</pre></div>}))
const first:PolicyComparisonRequest={before:{kind:'revision',revisionId:'old'},after:{kind:'publication',publicationId:'published'}}
function comparison(request=first):Comparison{
 const side={identity:{content:{sha256:'hash',bytes:10}},text:'Café 中文\r\n',metadata:{kind:'narrative' as const,title:'Expenses',artifactVersion:'1.0.0',completeness:'complete' as const}}
 return {policyId:'policy',before:{...side,selector:request.before},after:{...side,selector:request.after,text:'Café 中文\n'},differences:[{field:'title',before:'Old title',after:'Expenses'},{field:'workflowDigest',before:'old-workflow',after:'new-workflow'}],limitations:['informational-only']}
}
beforeEach(()=>{vi.mocked(resolvePolicyComparison).mockReset();vi.mocked(resolvePolicyComparison).mockResolvedValue(comparison())})
afterEach(cleanup)
it('authenticates every opening and keeps the exact pair when newer state is discovered',async()=>{
 const discuss=vi.fn(),view=render(<PolicyComparison policyId="policy" request={first} onDiscuss={discuss}/>)
 const diff=await screen.findByRole('region',{name:'Diff'})
 expect(diff.getAttribute('data-language')).toBe('text')
 expect(diff.querySelector('pre')?.textContent).toBe('Café 中文\r\n')
 expect(await screen.findByText(/Stored line endings differ/)).toBeTruthy()
 view.rerender(<PolicyComparison policyId="policy" request={structuredClone(first)} newerState onDiscuss={discuss}/>)
 expect(screen.getByText(/keeps the exact selected pair/)).toBeTruthy()
 expect(resolvePolicyComparison).toHaveBeenCalledTimes(1)
 fireEvent.click(screen.getByRole('button',{name:'Discuss changes'}));expect(discuss).toHaveBeenCalledWith(first)
 view.unmount();render(<PolicyComparison policyId="policy" request={first}/>);await screen.findByRole('region',{name:'Diff'})
 expect(resolvePolicyComparison).toHaveBeenCalledTimes(2)
})
it('discards an old asynchronous result after the selected pair changes',async()=>{
 let complete!:(result:Comparison)=>void
 vi.mocked(resolvePolicyComparison).mockImplementationOnce(()=>new Promise(resolve=>{complete=resolve}))
 const view=render(<PolicyComparison policyId="policy" request={first}/>)
 const next:PolicyComparisonRequest={before:{kind:'revision',revisionId:'second'},after:{kind:'revision',revisionId:'third'}}
 const result=comparison(next);result.after.text='Selected new text';vi.mocked(resolvePolicyComparison).mockResolvedValueOnce(result)
 view.rerender(<PolicyComparison policyId="policy" request={next}/>);await screen.findByText('Selected new text')
 await act(()=>complete(comparison()))
 expect(screen.getByText('Selected new text')).toBeTruthy();expect(screen.queryByText('Local publication published')).toBeNull()
 const signal=vi.mocked(resolvePolicyComparison).mock.calls[0][2];expect(signal?.aborted).toBe(true)
})
it('does not project unavailable extraction as empty source deletion or unchanged text',async()=>{
 const result=comparison();result.before.text=null;result.before.metadata={kind:'extracted-source',completeness:'unavailable'};result.limitations=['unavailable-extraction','mixed-projections']
 vi.mocked(resolvePolicyComparison).mockResolvedValue(result)
 render(<PolicyComparison policyId="policy" request={first}/>)
 expect(await screen.findByText(/Unavailable content is not shown as an empty deletion/)).toBeTruthy()
 expect(screen.queryByRole('region',{name:'Diff'})).toBeNull()
 expect(screen.queryByText(/Visible text is identical/)).toBeNull()
})
it('shows partial equal readable text with an incomplete comparison notice',async()=>{
 const result=comparison();result.before.metadata={kind:'extracted-source',completeness:'partial'};result.before.text=result.after.text;result.limitations=['incomplete-extraction']
 vi.mocked(resolvePolicyComparison).mockResolvedValue(result)
 render(<PolicyComparison policyId="policy" request={first}/>)
 expect(await screen.findByText(/Readable text is identical; extraction is incomplete/)).toBeTruthy()
 expect(screen.queryByText(/Visible text is identical/)).toBeNull()
})
it('clears completed text after session end and retries only the same read pair',async()=>{
 render(<PolicyComparison policyId="policy" request={first}/>);await screen.findByRole('region',{name:'Diff'})
 act(()=>forgetSession())
 expect(screen.queryByRole('region',{name:'Diff'})).toBeNull()
 expect(screen.getByText('Policy comparison unavailable')).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Retry'}))
 await waitFor(()=>expect(resolvePolicyComparison).toHaveBeenCalledTimes(1))
})
it('shows read failure without stale successful text and retries fresh authenticated reads',async()=>{
 vi.mocked(resolvePolicyComparison).mockRejectedValueOnce(new Error('raw failure details'))
 render(<PolicyComparison policyId="policy" request={first}/>)
 expect(await screen.findByText('Policy comparison unavailable')).toBeTruthy()
 expect(screen.queryByText('raw failure details')).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Retry'}));await screen.findByRole('region',{name:'Diff'})
 expect(resolvePolicyComparison).toHaveBeenCalledTimes(2)
})

it.each(['\r\n','\r'])('discloses stored line-ending-only differences without normalizing retained text (%j)',async ending=>{
 const result=comparison();result.before.text='Café 中文'+ending;result.after.text='Café 中文\n'
 vi.mocked(resolvePolicyComparison).mockResolvedValue(result)
 render(<PolicyComparison policyId="policy" request={first}/>);
 expect(await screen.findByText(/Stored line endings differ/)).toBeTruthy()
 expect(screen.getByRole('region',{name:'Diff'}).querySelector('pre')?.textContent).toBe('Café 中文'+ending)
})
