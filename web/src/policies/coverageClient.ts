import {queryOptions} from '@tanstack/react-query'
import {privateQuery} from '../data/privateQuery'
import {deskFetch} from '../files/client'
import {NoSession} from '../mcp/session'
import {sourceMessage} from '../i18n/source'
import {PolicyRequestError,type PolicyWrite} from './client'
import {validPolicyAuthoringSource} from './authoringSource'
import {validCoverageId,validCoverageCandidateId,type CoverageCheck,type CoverageOptions,type CoverageOutcome,type CoveragePackSelector,type CoverageReply,type CoverageResolved,type CoverageSummary} from './coverage'

export const coverageKey=(policyId:string)=>['policies',policyId,'coverage'] as const
async function request<T>(path:string,init:RequestInit={},write=false):Promise<T>{
 let response:Response,body:unknown
 try{response=await deskFetch(path,init)}catch(cause){throw new PolicyRequestError(cause instanceof NoSession?'unauthorized':write?'delivery-unknown':'unavailable',write&&!(cause instanceof NoSession))}
 try{body=await response.json()}catch{throw new PolicyRequestError(write?'delivery-unknown':'unavailable',write)}
 if(!response.ok){
  const value=body as {code?:string;outcome?:string}|null,uncertain=write&&(value?.outcome==='unknown'||response.status>=500&&value?.outcome!=='not-committed')
  const failure=new PolicyRequestError(uncertain?'delivery-unknown':response.headers.get('X-Jpack-Desk-Refusal')||value?.code||(response.status===401||response.status===403?'access-denied':'unavailable'),uncertain)
  if(failure.code==='revision-conflict')failure.message=sourceMessage('The coverage record or Policy changed. Your draft is preserved; resolve the Policy editor and reload this coverage record before continuing.')
  throw failure
 }
 return body as T
}
const base=(policyId:string)=>'/api/policies/'+encodeURIComponent(policyId)
function exactReply(reply:CoverageReply,policyId:string,id:string,candidateId?:string){
 const document=reply?.document,candidate=document?.candidate,pack=candidate?.pack?.document,inventory=reply?.inventory
 if(!document||document.schemaVersion!==1||document.policyId!==policyId||document.id!==id||!validCoverageId(id)||!candidate||!validCoverageCandidateId(candidate.id)||candidateId&&candidate.id!==candidateId||!validPolicyAuthoringSource(candidate.source)||candidate.source.policyId!==policyId||!candidate.source.publicationId||!candidate.content||candidate.content.sha256!==candidate.digest||!/^[a-f0-9]{64}$/.test(candidate.digest)||!pack||pack.kind!=='pack'||typeof pack.id!=='string'||typeof pack.version!=='string'||!pack.content||!/^[a-f0-9]{64}$/.test(pack.content.sha256)||!Number.isSafeInteger(pack.content.bytes)||!inventory||!Array.isArray(inventory.rules)||!inventory.rules.every(id=>typeof id==='string')||!Array.isArray(inventory.exceptions)||!inventory.exceptions.every(id=>typeof id==='string')||typeof inventory.applicability!=='boolean'||typeof inventory.fallback!=='boolean'||!reply.availability||!['present','missing','unavailable'].includes(reply.availability.packPublication)||!Array.isArray(document.candidates)||!Array.isArray(document.reviews))throw new PolicyRequestError('coverage-invalid-response')
 return reply
}
export async function readCoverage(policyId:string,id:string,signal?:AbortSignal){return exactReply(await request<CoverageReply>(base(policyId)+'/coverage/'+encodeURIComponent(id),{signal}),policyId,id)}
export async function readCoverageCandidate(policyId:string,id:string,candidate:string,signal?:AbortSignal){return exactReply(await request<CoverageReply>(base(policyId)+'/coverage/'+encodeURIComponent(id)+'/candidates/'+encodeURIComponent(candidate),{signal}),policyId,id,candidate)}
export const coverageListQuery=(policyId:string,enabled=true)=>queryOptions({...privateQuery,enabled,queryKey:coverageKey(policyId),queryFn:({signal})=>request<{items:CoverageSummary[]}>(base(policyId)+'/coverage',{signal})})
export const coverageOptionsQuery=(policyId:string,enabled=true)=>queryOptions({...privateQuery,enabled,queryKey:[...coverageKey(policyId),'options'],queryFn:({signal})=>request<CoverageOptions>(base(policyId)+'/coverage-options',{signal})})
export const coverageQuery=(policyId:string,id?:string,enabled=true)=>queryOptions({...privateQuery,enabled:!!id&&enabled,queryKey:[...coverageKey(policyId),id],queryFn:({signal})=>readCoverage(policyId,id!,signal)})
export const writeCoverage=(operation:PolicyWrite)=>request<CoverageOutcome>('/api/policies'+operation.path,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(operation.body)},true)
export const recoverCoverageOperation=(operationId:string)=>request<CoverageOutcome>('/api/policy-operations/'+encodeURIComponent(operationId))
/** An explicit command. It is never a privateQuery and is never retried. */
export const checkCoverage=(policyId:string,id:string,body:{baseRevision:string;candidateDigest:string;policyRevision:string})=>request<CoverageCheck>(base(policyId)+'/coverage/'+encodeURIComponent(id)+'/check',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
export const resolveCoverage=(policyId:string,body:{policyPublicationId:string;pack:CoveragePackSelector})=>request<CoverageResolved>(base(policyId)+'/coverage/resolve',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
