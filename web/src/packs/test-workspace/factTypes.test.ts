import {expect,it,vi} from 'vitest'
import {factCheckRows, factTypeReviewFindings} from './factTypes'
import {proposalCases, validateTestProposal, type TestProposal} from './proposals'
const document={matrixVersion:'3',cases:[{id:'fit',facts:{age:12,zero:0,flag:false,unknown:null},expectedDisposition:{kind:'outcome',outcomeId:'yes',reasons:[],handoff:{state:'none'}}}]}
const finding={code:'FACT-NOT-COMPARABLE',path:'/age',operator:'less-than',factType:'number',expectedTypes:['decimal-string'],message:'"/age" is a number; less-than requires a decimal string'}
const checks=[{index:0,id:'fit',status:'complete' as const,findings:[finding]}]
const report={contractVersion:'1',specVersion:'0.2.0-draft',status:'valid',findings:[],results:[{index:0,id:'fit',status:'valid',findings:[]}],factCheckVersion:'1',factChecks:checks}
const reason={caseId:'fit',path:'/age',reason:'Deliberately checks the unknown result for a JSON number.'}
it('blocks accidental fact encoding using runtime findings without coercing any inputs',async()=>{
 const call=vi.fn(async (_name: string, _args: Record<string, unknown>)=>({structuredContent:structuredClone(report)})), original=structuredClone(document)
 const errors=await validateTestProposal(document,[],call,new AbortController().signal,'pack text')
 expect(errors).toMatchObject([{code:'FACT-TYPE-REVIEW',path:'/cases/0/facts/age'}])
 expect(errors[0]?.message).toContain('Case "fit"')
 expect(document).toEqual(original)
 expect(call).toHaveBeenCalledWith('experimental_validate_test_matrix',{pack:'pack text',matrix:JSON.stringify(document)})
})
it('preserves a deliberately wrong-type case with reviewable reasons outside the runtime matrix',async()=>{
 const call=vi.fn(async (_name: string, _args: Record<string, unknown>)=>({structuredContent:structuredClone(report)}))
 const proposed={...document,intentionalFactTypes:[reason]}
 expect(await validateTestProposal(proposed,[],call,new AbortController().signal,'pack')).toEqual([])
 expect(JSON.parse(String(call.mock.calls[0]![1].matrix))).toEqual(document)
 const cases=proposalCases({state:'ready',sources:[],attempts:[{document:proposed}]} as unknown as TestProposal)
 expect(cases[0]?.row.facts).toEqual(document.cases[0]!.facts)
 expect(cases[0]?.intentionalFactTypes).toEqual([reason])
})
it('does not allow blanket, stale, duplicate or blank intentional-type explanations',()=>{
 for(const entries of [[{...reason,reason:' '}],[{...reason,path:'/other'}],[reason,reason],null])
  expect(factTypeReviewFindings(checks,{...document,intentionalFactTypes:entries}).length).toBeGreaterThan(0)
 expect(factTypeReviewFindings(checks,{...document,intentionalFactTypes:[reason]})).toEqual([])
})
it('fails closed for older or incomplete runtime responses and unavailable checks',()=>{
 for(const value of [{...report,factCheckVersion:undefined},{...report,factChecks:[]},{...report,factChecks:[{...checks[0],id:'another'}]}])
  expect(()=>factCheckRows(value,document)).toThrow()
 expect(factTypeReviewFindings([{index:0,id:'fit',status:'unavailable',message:'Limit exceeded',findings:[]}],document)[0]?.code).toBe('FACT-CHECK-UNAVAILABLE')
})
it('accepts empty diagnostics without inferring that missing, false, zero or null should change',()=>{
 expect(factTypeReviewFindings([{...checks[0]!,findings:[]}],document)).toEqual([])
})
