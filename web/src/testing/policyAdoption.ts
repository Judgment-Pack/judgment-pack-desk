import {digestBytes} from '../dialect/canon'
import {coverageActor,coverageAt,coverageSource} from './policyCoverage'
import {adoptionSelection,type AdoptionChoice,type AdoptionCommand,type AdoptionDependency,type AdoptionOptions,type AdoptionOutcome,type AdoptionReply,type AdoptionSummary} from '../policies/adoption'
import type {CoveragePackSelector} from '../policies/coverage'

export const adoptionRaw='{"id":"https://example.invalid/packs/eligibility","version":"1.0.0","threshold":9007199254740993}\r\n'
export const adoptionId='adoption_'+'1'.repeat(48)
export async function adoptionFixtures(){
 const pack:CoveragePackSelector={publicationId:'2'.repeat(64),artifactId:'https://example.invalid/packs/eligibility',version:'1.0.0',content:{sha256:(await digestBytes(new TextEncoder().encode(adoptionRaw))).slice(7),bytes:new TextEncoder().encode(adoptionRaw).length}}
 const choices:AdoptionChoice[]=['3','4'].map((value,index)=>({policyId:'policy_'+value.repeat(48),coverageId:'coverage_'+value.repeat(48),revision:value.repeat(64),candidateId:'coverage_candidate_'+value.repeat(48),reviewId:'coverage_review_'+value.repeat(48),policyPublicationId:'publication_'+value.repeat(48),policyVersion:'1.0.0',title:index?'Reporting policy':'Eligibility policy',scope:{kind:'whole-narrative'},statement:{status:'manual',handoff:index?'Report each decision.':'Administrator reviews the narrative.'},pack}))
 const dependencies:AdoptionDependency[]=choices.map(row=>({selection:adoptionSelection(row),candidateDigest:'5'.repeat(64),coverageContent:{sha256:'5'.repeat(64),bytes:200},coverageReview:{id:row.reviewId,sequence:1,candidateId:row.candidateId,candidateDigest:'5'.repeat(64),policyRevision:'6'.repeat(64),decision:'approve',at:coverageAt,actor:coverageActor,check:{sha256:'5'.repeat(64),bytes:200}},policyRevision:'6'.repeat(64),lifecycleRevision:'7'.repeat(64),sourceRevision:'8'.repeat(64),authorityRevision:'9'.repeat(64),policyPublicationId:row.policyPublicationId,source:{...coverageSource,policyId:row.policyId,publicationId:row.policyPublicationId,title:row.title},scope:row.scope,statement:row.statement}))
 const ref={sha256:'a'.repeat(64),bytes:500}
 const reply:AdoptionReply={revision:'b'.repeat(64),document:{schemaVersion:1,id:adoptionId,createdAt:coverageAt,updatedAt:coverageAt,candidate:{id:'adoption_candidate_'+'c'.repeat(48),digest:ref.sha256,content:ref,portableId:'https://example.invalid/adoptions/one',formatVersion:'1.0.0',basisDigest:'d'.repeat(64),pack,dependencies,document:ref,materials:[ref],check:ref},reviewState:'unreviewed',reviews:[]},readiness:{status:'ready'},eligibility:{status:'effective'}}
 const options:AdoptionOptions={packPublications:[pack],coverages:choices}
 return {pack,choices,reply,options}
}
export function adoptionSummary(reply:AdoptionReply):AdoptionSummary{return {id:reply.document.id,revision:reply.revision,candidateDigest:reply.document.candidate.digest,pack:reply.document.candidate.pack,dependencies:reply.document.candidate.dependencies.map(row=>row.selection),reviewState:reply.document.reviewState,updatedAt:coverageAt}}
export function adoptionOutcome(command:AdoptionCommand,initial:AdoptionReply):AdoptionOutcome{
 const reply=structuredClone(initial)
 if(command.action==='prepare'){reply.document.candidate.dependencies=reply.document.candidate.dependencies.filter(row=>command.coverages.some(selection=>selection.policyId===row.selection.policyId));reply.document.candidate.pack=command.pack}
 else{
  reply.revision='e'.repeat(64)
  const review={id:'adoption_review_'+'f'.repeat(48),sequence:1,candidateId:reply.document.candidate.id,candidateDigest:reply.document.candidate.digest,basisDigest:reply.document.candidate.basisDigest,decision:command.decision,at:coverageAt,actor:coverageActor,...(command.comment?{comment:command.comment}:{})}
  reply.document.reviews.push(review);reply.document.currentReviewId=review.id;reply.document.reviewState=command.decision==='approve'?'reviewed':'changes-requested'
 }
 const policyId=reply.document.candidate.dependencies[0]!.selection.policyId,basisDigest=reply.document.candidate.basisDigest
 return {outcomeKind:'adoption',delivery:'committed',policyId,policyRevision:basisDigest,adoption:reply,receipt:{operationId:command.operationId,kind:'adoption-'+command.action,policyId,revision:basisDigest,adoptionId:reply.document.id,adoptionRevision:reply.revision,adoptionCandidateId:reply.document.candidate.id,candidateDigest:reply.document.candidate.digest,...(reply.document.currentReviewId?{reviewId:reply.document.currentReviewId}:{})}}
}
