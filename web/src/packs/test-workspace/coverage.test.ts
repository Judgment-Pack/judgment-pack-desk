import { expect,it } from 'vitest'
import { currentCoverage, latestCoverage, missingProbes, missingTestsRequest, additionFindings } from './coverage'
import { emptySuite, importMatrix, type TestRun } from './model'
import { saveReviewedCases, type TestProposal } from './proposals'
import { jsonIdentity } from '../../research/checkCandidate'
const cases=importMatrix({matrixVersion:'3',cases:[{id:'accept',facts:{ready:true},expectedDisposition:{kind:'outcome',outcomeId:'accept',reasons:[],handoff:{state:'none'}}}]},'manual')
const run={id:'run',packDigest:'digest',packText:'{}',packVersion:'1',at:'2026-09-24',cases,origin:'tests',report:{packs:[{id:'p',coverage:[{probe:'reason:unknown',status:'missing',detail:'No row asserts unknown.'},{probe:'outcome:accept',status:'covered'}]}]}} as TestRun
it('offers gaps only for the exact executable suite and pack, independent of order',()=>{
 expect(currentCoverage(run,cases,'digest')).toBe(true)
 expect(currentCoverage(run,cases,'other')).toBe(false)
 expect(currentCoverage(run,[{...cases[0]!,row:{...cases[0]!.row,facts:{ready:false}}}],'digest')).toBe(false)
 expect(currentCoverage(run,[],'digest')).toBe(false)
 expect(currentCoverage({...run,error:'offline'},cases,'digest')).toBe(false)
 expect(latestCoverage({...emptySuite(),cases,runs:[run]},'digest')).toBe(run)
 expect(missingProbes(run)).toEqual([run.report!.packs![0]!.coverage![0]])
 expect(missingTestsRequest(run)).toContain('reason:unknown')
 expect(missingTestsRequest(run)).toContain('Keep every existing case unchanged')
})
it('refuses gap proposals replacing existing cases at the final persistence boundary',()=>{
 const proposal={id:'proposal',state:'ready',packDigest:'digest',additionsOnly:true,caseSnapshots:{accept:jsonIdentity(cases[0])},attempts:[{document:{matrixVersion:'3',cases:cases.map(c=>c.row)},message:'',findings:[],unknowns:[]}],sources:[]} as unknown as TestProposal
 const suite={...emptySuite(),cases,proposals:[proposal]}
 expect(additionFindings(proposal.attempts[0]!.document,['accept'])).toHaveLength(1)
 expect(()=>saveReviewedCases(suite,proposal,cases,'digest')).toThrow('cannot replace')
 expect(suite.cases).toBe(cases)
})
