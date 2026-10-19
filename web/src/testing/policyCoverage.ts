import type {PolicyReply,PolicyWrite} from '../policies/client'
import type {CoverageOptions,CoverageOutcome,CoverageReply,CoverageSummary} from '../policies/coverage'
export const coveragePolicyId='policy_'+'1'.repeat(48),coverageRecordId='coverage_'+'2'.repeat(48),coverageCandidateId='coverage_candidate_'+'3'.repeat(48)
export const coverageAt='2026-10-18T12:00:00Z',coverageActor={kind:'local',subject:'administrator'}
const content={sha256:'a'.repeat(64),bytes:200},policyRevision='b'.repeat(64),publicationId='publication_'+'4'.repeat(48)
export const coveragePack={publicationId:'5'.repeat(64),artifactId:'https://example.invalid/packs/eligibility',version:'1.0.0',content:{sha256:'c'.repeat(64),bytes:400}}
export const coverageSource={version:1 as const,policyId:coveragePolicyId,revisionId:'revision_'+'6'.repeat(48),publicationId,candidateDigest:content.sha256,content,title:'Eligibility policy',artifactVersion:'1.0.0',narrative:'# Eligibility\r\nCafé applicants require manual review.\r\n'}
export const coveragePolicy:PolicyReply={revision:policyRevision,document:{id:coveragePolicyId,managementMode:'desk-managed',createdAt:coverageAt,updatedAt:coverageAt,candidate:{title:coverageSource.title,narrative:coverageSource.narrative,artifactVersion:coverageSource.artifactVersion,digest:content.sha256,revisionId:coverageSource.revisionId,content},workflow:{independentReviewRequired:false},workflowDigest:'d'.repeat(64),reviewState:'approved',revisions:[],reviews:[],publications:[{id:publicationId,sequence:1,candidateRevisionId:coverageSource.revisionId,at:coverageAt,actor:coverageActor,artifactVersion:'1.0.0',candidateDigest:content.sha256,workflowDigest:'d'.repeat(64),reviewId:'review_'+'7'.repeat(48),content}]}}
export const coverageOptions:CoverageOptions={policyRevision,policyPublications:[{publicationId,revisionId:coverageSource.revisionId,candidateDigest:content.sha256,artifactVersion:'1.0.0',title:coverageSource.title,content,at:coverageAt}],packPublications:[{...coveragePack,alias:'Eligibility'}]}
export function coverageReply():CoverageReply{
 const candidate={id:coverageCandidateId,digest:'e'.repeat(64),content:{sha256:'e'.repeat(64),bytes:800},source:coverageSource,pack:{publicationId:coveragePack.publicationId,document:{kind:'pack' as const,alias:'Eligibility',id:coveragePack.artifactId,version:coveragePack.version,path:'packs/eligibility.json',content:coveragePack.content}},scope:{kind:'whole-narrative' as const},statement:{status:'manual' as const,handoff:'Administrator checks the exact published narrative.'}}
 return {revision:'f'.repeat(64),policyRevision,document:{schemaVersion:1,id:coverageRecordId,policyId:coveragePolicyId,createdAt:coverageAt,updatedAt:coverageAt,candidate,reviewState:'unreviewed',candidates:[{id:candidate.id,sequence:1,digest:candidate.digest,content:candidate.content,policyPublicationId:publicationId,pack:coveragePack,status:candidate.statement.status,at:coverageAt,actor:coverageActor}],reviews:[]},inventory:{rules:['eligible'],exceptions:['manual-review'],applicability:true,fallback:true},availability:{packPublication:'present'}}
}
export function coverageSummary(reply=coverageReply()):CoverageSummary{return {id:reply.document.id,policyId:coveragePolicyId,revision:reply.revision,candidateId:reply.document.candidate.id,candidateDigest:reply.document.candidate.digest,policyPublicationId:publicationId,policyVersion:'1.0.0',pack:coveragePack,status:reply.document.candidate.statement.status,reviewState:reply.document.reviewState,updatedAt:coverageAt}}
export function coverageOutcome(operation:PolicyWrite,original=coverageReply()):CoverageOutcome{
 const reply=structuredClone(original)
 reply.revision='8'.repeat(64)
 if(operation.body.action==='save'){
  reply.document.candidate.statement=operation.body.statement as CoverageReply['document']['candidate']['statement']
  reply.document.candidate.id='coverage_candidate_'+'9'.repeat(48)
  reply.document.candidate.digest='9'.repeat(64);reply.document.candidate.content.sha256=reply.document.candidate.digest
  reply.document.candidates.push({...reply.document.candidates[0]!,id:reply.document.candidate.id,digest:reply.document.candidate.digest,content:reply.document.candidate.content,status:reply.document.candidate.statement.status,sequence:2})
  reply.document.reviewState='unreviewed'
 }else{
  const review={id:'coverage_review_'+'0'.repeat(48),sequence:2,candidateId:reply.document.candidate.id,candidateDigest:reply.document.candidate.digest,policyRevision,decision:operation.body.decision as 'approve'|'request-changes',at:coverageAt,actor:coverageActor,...(operation.body.comment?{comment:String(operation.body.comment)}:{})}
  reply.document.reviews.push(review);reply.document.currentReviewId=review.id;reply.document.reviewState=review.decision==='approve'?'reviewed':'changes-requested'
 }
 return {outcomeKind:'coverage',delivery:'committed',policyId:coveragePolicyId,policyRevision,receipt:{operationId:String(operation.body.operationId),kind:'coverage-'+operation.body.action,policyId:coveragePolicyId,revision:policyRevision,coverageId:reply.document.id,coverageRevision:reply.revision,coverageCandidateId:reply.document.candidate.id,candidateDigest:reply.document.candidate.digest,...(reply.document.currentReviewId?{reviewId:reply.document.currentReviewId}:{})},coverage:reply}
}
