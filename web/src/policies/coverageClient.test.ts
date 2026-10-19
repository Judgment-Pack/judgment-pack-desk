import {afterEach,expect,it,vi} from 'vitest'
import {checkCoverage,coverageListQuery,coverageOptionsQuery,readCoverageCandidate,resolveCoverage,writeCoverage} from './coverageClient'
import {coverageCandidateId,coveragePack,coveragePolicy,coveragePolicyId,coverageRecordId,coverageReply} from '../testing/policyCoverage'

afterEach(()=>vi.unstubAllGlobals())
it('reads only the selected exact retained candidate with authenticated scope and refuses substituted identities',async()=>{
 const reply=coverageReply(),fetch=vi.fn(async(_input:RequestInfo|URL,_init?:RequestInit)=>Response.json(reply));vi.stubGlobal('fetch',fetch)
 const signal=new AbortController().signal
 expect((await readCoverageCandidate(coveragePolicyId,coverageRecordId,coverageCandidateId,signal)).document.candidate.source.narrative).toContain('Café')
 expect(fetch.mock.calls[0]?.[0]).toBe('/api/policies/'+coveragePolicyId+'/coverage/'+coverageRecordId+'/candidates/'+coverageCandidateId)
 reply.document.candidate.id='coverage_candidate_'+'0'.repeat(48)
 await expect(readCoverageCandidate(coveragePolicyId,coverageRecordId,coverageCandidateId)).rejects.toMatchObject({code:'coverage-invalid-response'})
 reply.document.candidate.id=coverageCandidateId;reply.document.policyId='policy_'+'a'.repeat(48)
 await expect(readCoverageCandidate(coveragePolicyId,coverageRecordId,coverageCandidateId)).rejects.toMatchObject({code:'coverage-invalid-response'})
})
it('keeps checks and exact publication resolution explicit and distinct from unknown durable delivery',async()=>{
 const fetch=vi.fn(async()=>{throw Error('offline')});vi.stubGlobal('fetch',fetch)
 const body={baseRevision:coverageReply().revision,candidateDigest:coverageReply().document.candidate.digest,policyRevision:coveragePolicy.revision}
 await expect(checkCoverage(coveragePolicyId,coverageRecordId,body)).rejects.toMatchObject({code:'unavailable',uncertain:false})
 await expect(resolveCoverage(coveragePolicyId,{policyPublicationId:coverageReply().document.candidate.source.publicationId!,pack:coveragePack})).rejects.toMatchObject({code:'unavailable',uncertain:false})
 await expect(writeCoverage({method:'POST',path:'/'+coveragePolicyId+'/coverage',body:{action:'save',operationId:'exact-operation'}})).rejects.toMatchObject({code:'delivery-unknown',uncertain:true})
 expect(fetch).toHaveBeenCalledTimes(3)
 expect(coverageListQuery(coveragePolicyId).retry).toBe(false);expect(coverageOptionsQuery(coveragePolicyId).refetchInterval).toBe(10000)
})
it('preserves typed refusals and known not-committed failures without exposing backend prose',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>Response.json({code:'coverage-conflict',outcome:'not-committed',error:'private backend trace'},{status:409})))
 await expect(writeCoverage({method:'POST',path:'/'+coveragePolicyId+'/coverage',body:{action:'save'}})).rejects.toMatchObject({code:'coverage-conflict',uncertain:false})
 try{await writeCoverage({method:'POST',path:'/'+coveragePolicyId+'/coverage',body:{}})}catch(error){expect(String(error)).not.toContain('private backend trace')}
})
