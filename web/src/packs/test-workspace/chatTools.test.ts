import { expect, it, vi } from 'vitest'
import type { McpToolResult } from '@judgment-pack/harness/engine'
import { digestOf } from '../../research/checkCandidate'
import { emptySuite, importMatrix } from './model'
import { testChatTools, testReviewHref, type TestChatPorts } from './chatTools'
const outcome = {kind:'outcome',outcomeId:'yes',reasons:[],handoff:{state:'none'}}
const document = {matrixVersion:'3',cases:[{id:'fit',facts:{size:{ageMonths:'12',lower:'100',upper:'200',conflict:false},zero:0,unknown:null},expectedDisposition:outcome}]}
const data = (reply: McpToolResult) => JSON.parse(reply.content![0]!.text!)
function fixture() {
  const target = {owner:'pack',title:'Pack',text:'{"id":"pack"}',draft:false}
  const suite = emptySuite(), save = vi.fn().mockResolvedValue(undefined)
  const call = vi.fn(async (name: string, args: Record<string,unknown>): Promise<McpToolResult> => {
    const envelope = {contractVersion:'1',specVersion:'0.2.0-draft'}
    if (name === 'experimental_get_test_matrix_contract') return {structuredContent:{...envelope,contract:{matrixVersion:'3',rowMembers:['id','facts','expectedDisposition']}}}
    const doc = JSON.parse(String(args.matrix))
    return {structuredContent:{...envelope,status:'valid',findings:[],...(args.pack ? {factCheckVersion:'1',factChecks:doc.cases.map((c:{id:string},index:number)=>({index,id:c.id,status:'complete',findings:[]}))} : {}),results:doc.cases.map((c:{id:string},index:number)=>({index,id:c.id,status:'valid',findings:[]}))}}
  })
  const ports: TestChatPorts = {chatId:'existing-chat',target:vi.fn(async()=>target),suite:async()=>suite,call,guidance:vi.fn(async()=> 'Runtime guidance: ordered comparisons use decimal strings.'),sources:()=>[],request:()=> 'Design cases',model:()=> 'model',save}
  const tools = testChatTools(ports), signal = new AbortController().signal
  const read = (args:Record<string,unknown>={}) => tools[0]!.execute(args,signal)
  const propose = (value:unknown=document, args:Record<string,unknown>={}) => tools[1]!.execute({document:JSON.stringify(value),summary:'Documented fit',...args},signal)
  return {target,suite,ports,call,save,tools,read,propose,signal}
}
it('reads runtime instructions and existing observations without running tests',async()=>{
 const f=fixture(); const reply=data(await f.read())
 expect(reply.instruction).toContain('Answer the current question')
 expect(reply.instruction).toContain('decimal strings for operands AND facts')
 expect(reply.runtimeGuidance).toContain('Runtime guidance')
 expect(f.ports.guidance).toHaveBeenCalledWith(f.target.text,f.signal)
 expect(f.call.mock.calls.map(c=>c[0])).toEqual(['experimental_get_test_matrix_contract'])
 expect(f.save).not.toHaveBeenCalled()
})
it('requires a current read before proposing and refuses a changed pack',async()=>{
 const f=fixture();await expect(f.propose()).rejects.toThrow('Read the current pack')
 await f.read();f.ports.target=vi.fn(async()=>({...f.target,text:'{"id":"changed"}'}))
 await expect(f.propose()).rejects.toThrow('Read the current pack');expect(f.save).not.toHaveBeenCalled()
})
it('retains a validated proposal in the same session without saving cases or changing inputs',async()=>{
 const f=fixture();await f.read();const reply=data(await f.propose())
 expect(reply.status).toBe('needs-review')
 expect(f.save).toHaveBeenCalledTimes(1)
 const proposal=f.save.mock.calls[0]![1]
 expect(proposal).toMatchObject({chatId:'existing-chat',request:'Design cases',state:'ready',model:'model'})
 expect(proposal.attempts[0].document).toEqual(document)
 expect(f.suite.cases).toEqual([])
 expect(reply.reviewUrl).toBe('/packs/pack/matrix?chat=existing-chat&testProposal='+proposal.id)
 expect(f.call.mock.calls.map(c=>c[0])).toEqual(['experimental_get_test_matrix_contract','experimental_validate_test_matrix'])
})
it('returns rejected matrix findings to the normal chat loop without retaining an invalid proposal',async()=>{
 const f=fixture();await f.read();f.call.mockResolvedValueOnce({structuredContent:{contractVersion:'1',specVersion:'0.2.0-draft',status:'invalid',findings:[{code:'MATRIX',path:'/cases',message:'Missing expectations'}],results:[]}})
 const reply=await f.propose();expect(reply.isError).toBe(true);expect(data(reply).findings[0].message).toBe('Missing expectations');expect(f.save).not.toHaveBeenCalled()
})
it('keeps missing-tests proposals additions-only against the exact coverage snapshot',async()=>{
 const f=fixture();f.suite.cases=importMatrix(document,'manual')
 f.suite.runs=[{id:'run',packDigest:await digestOf(f.target.text),cases:structuredClone(f.suite.cases),report:{packs:[]}} as never]
 await f.read({coverageRunId:'run'});const reply=await f.propose()
 expect(reply.isError).toBe(true);expect(data(reply).findings[0].code).toBe('EXISTING-CASE');expect(f.save).not.toHaveBeenCalled()
})
it('refuses stale coverage and never invents an attachment reference',async()=>{
 const f=fixture();await expect(f.read({coverageRunId:'old'})).rejects.toThrow('Coverage changed')
 await f.read();const reply=await f.propose({...document,sourceMappings:{fit:{'/size':'missing-source'}}})
 expect(reply.isError).toBe(true);expect(f.save).not.toHaveBeenCalled()
})
it('rechecks the pack after validation and respects cancellation before retaining work',async()=>{
 const f=fixture();await f.read();let reads=0
 f.ports.target=vi.fn(async()=>++reads===1?f.target:{...f.target,text:'changed'})
 await expect(f.propose()).rejects.toThrow('changed during validation');expect(f.save).not.toHaveBeenCalled()
 const stopped=new AbortController();stopped.abort()
 await expect(f.tools[1]!.execute({document:JSON.stringify(document),summary:'Case'},stopped.signal)).rejects.toBeDefined();expect(f.save).not.toHaveBeenCalled()
})
it('routes draft reviews back to their existing session',()=>{
 expect(testReviewHref({owner:'draft-one',draft:true},'chat','proposal')).toBe('/packs/drafts/draft-one?chat=chat&testProposal=proposal')
})
