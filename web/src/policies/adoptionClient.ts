import {queryOptions} from '@tanstack/react-query'
import {privateQuery} from '../data/privateQuery'
import {deskFetch} from '../files/client'
import {NoSession} from '../mcp/session'
import {sourceMessage} from '../i18n/source'
import {validPolicyAuthoringSource,validPolicyPublicationId} from './authoringSource'
import {validPolicyId} from './chat'
import {PolicyRequestError} from './client'
import {validCoverageCandidateId,validCoverageId,type CoveragePackSelector,type CoverageStatement} from './coverage'
import {samePublication,sameSelections,type AdoptionCommand,type AdoptionOptions,type AdoptionOutcome,type AdoptionReply,type AdoptionSelection,type AdoptionSummary,type PendingAdoption} from './adoption'

export class AdoptionRequestError extends PolicyRequestError {
 constructor(code:string,uncertain=false){super(code,uncertain);this.message=adoptionFailure(code)}
}
function adoptionFailure(code:string){
 switch(code){
 case 'runtime-unavailable':return sourceMessage('Update the connected Runtime to support official Policy draft policy-v1.0.0-draft.1 (format 1.0.0). Your selection and saved governing-policy history remain available.')
 case 'unsupported-contract':return sourceMessage('The connected Runtime cannot prepare or review this candidate adoption format. Saved governing-policy history remains available.')
 case 'revision-conflict':case 'adoption-basis-changed':case 'coverage-stale':case 'adoption-stale':return sourceMessage('The adoption or its complete coverage basis changed. Your selection is preserved; reload and review the latest basis before continuing.')
 case 'adoption-invalid-response':return sourceMessage('The exact adoption result could not be verified. Keep the saved selection and recover or reload before continuing.')
 case 'delivery-unknown':return sourceMessage('The adoption response was interrupted. Check its exact operation result before another write.')
 case 'operation-unresolved':case 'not-found':return sourceMessage('No completed adoption operation was found yet. Check again before continuing.')
 case 'adoption-journal-unavailable':return sourceMessage('Pending adoption recovery could not be read or saved. Keep this tab open and retry recovery before preparing or reviewing another adoption.')
 case 'unauthorized':case 'forbidden':case 'access-denied':return sourceMessage('Your session cannot access governing policies. Sign in again to continue.')
 default:return sourceMessage('The governing-policy operation could not be completed. Your selection and saved history are preserved.')
 }
}
async function request<T>(path:string,init:RequestInit={},write=false):Promise<T>{
 let response:Response,body:unknown
 try{response=await deskFetch(path,init)}catch(cause){throw new AdoptionRequestError(cause instanceof NoSession?'unauthorized':write?'delivery-unknown':'unavailable',write&&!(cause instanceof NoSession))}
 try{body=await response.json()}catch{throw new AdoptionRequestError(write?'delivery-unknown':'unavailable',write)}
 if(!response.ok){const value=body as {code?:string;outcome?:string}|null,uncertain=write&&(value?.outcome==='unknown'||response.status>=500&&value?.outcome!=='not-committed');throw new AdoptionRequestError(uncertain?'delivery-unknown':response.headers.get('X-Jpack-Desk-Refusal')||value?.code||(response.status===401||response.status===403?'access-denied':'unavailable'),uncertain)}
 return body as T
}
const digest=(value:unknown):value is string=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value)
const identity=(value:unknown,prefix:string)=>typeof value==='string'&&new RegExp('^'+prefix+'_[a-f0-9]{48}$').test(value)
const ref=(value:any)=>!!value&&digest(value.sha256)&&Number.isSafeInteger(value.bytes)&&value.bytes>0
export const validAdoptionPack=(value:any):value is CoveragePackSelector=>!!value&&digest(value.publicationId)&&typeof value.artifactId==='string'&&!!value.artifactId&&new TextEncoder().encode(value.artifactId).length<=4096&&typeof value.version==='string'&&!!value.version&&new TextEncoder().encode(value.version).length<=256&&ref(value.content)
export const validAdoptionSelection=(value:any):value is AdoptionSelection=>!!value&&validPolicyId(value.policyId)&&validCoverageId(value.coverageId)&&digest(value.revision)&&validCoverageCandidateId(value.candidateId)&&identity(value.reviewId,'coverage_review')
const actor=(value:any)=>!!value&&typeof value.kind==='string'&&['subject','issuer','name'].every(key=>value[key]===undefined||typeof value[key]==='string')
const selections=(rows:any)=>Array.isArray(rows)&&rows.length>0&&rows.length<=16&&rows.every(validAdoptionSelection)&&new Set(rows.map(row=>row.policyId)).size===rows.length
function statement(value:any):value is CoverageStatement{return !!value&&(value.status==='manual'?typeof value.handoff==='string':value.status==='out-of-scope'?typeof value.rationale==='string':value.status==='implemented'?typeof value.basis==='string'&&Array.isArray(value.consumerElements)&&value.consumerElements.length>0:false)}
export function validAdoptionReply(reply:any):reply is AdoptionReply {
 const document=reply?.document,candidate=document?.candidate
 return !!document&&document.schemaVersion===1&&identity(document.id,'adoption')&&digest(reply.revision)&&!!candidate&&identity(candidate.id,'adoption_candidate')&&digest(candidate.digest)&&ref(candidate.content)&&candidate.content.sha256===candidate.digest&&candidate.formatVersion==='1.0.0'&&typeof candidate.portableId==='string'&&!!candidate.portableId&&digest(candidate.basisDigest)&&validAdoptionPack(candidate.pack)&&Array.isArray(candidate.dependencies)&&selections(candidate.dependencies.map((row:any)=>row?.selection))&&candidate.dependencies.every((row:any)=>digest(row.candidateDigest)&&ref(row.coverageContent)&&validPolicyAuthoringSource(row.source)&&row.source.policyId===row.selection.policyId&&row.source.publicationId===row.policyPublicationId&&validPolicyPublicationId(row.policyPublicationId)&&digest(row.policyRevision)&&digest(row.lifecycleRevision)&&digest(row.sourceRevision)&&digest(row.authorityRevision)&&row.scope?.kind==='whole-narrative'&&statement(row.statement)&&row.coverageReview?.id===row.selection.reviewId&&row.coverageReview?.candidateId===row.selection.candidateId&&row.coverageReview?.candidateDigest===row.candidateDigest&&row.coverageReview?.decision==='approve'&&row.coverageReview.policyRevision===row.policyRevision&&ref(row.coverageReview.check))&&ref(candidate.document)&&ref(candidate.check)&&Array.isArray(candidate.materials)&&candidate.materials.length<=16&&candidate.materials.every(ref)&&['unreviewed','reviewed','changes-requested'].includes(document.reviewState)&&Array.isArray(document.reviews)&&document.reviews.length<=512&&document.reviews.every((row:any)=>!!row&&identity(row.id,'adoption_review')&&row.candidateId===candidate.id&&row.candidateDigest===candidate.digest&&row.basisDigest===candidate.basisDigest&&['approve','request-changes'].includes(row.decision)&&typeof row.at==='string'&&Number.isFinite(Date.parse(row.at))&&actor(row.actor))&&['ready','stale','unavailable'].includes(reply.readiness?.status)&&['effective','not-yet-effective','ineligible','unavailable'].includes(reply.eligibility?.status)
}
function exactReply(reply:unknown,id?:string,pack?:CoveragePackSelector){if(!validAdoptionReply(reply)||id&&reply.document.id!==id||pack&&!samePublication(reply.document.candidate.pack,pack))throw new AdoptionRequestError('adoption-invalid-response');return reply}
export const ADOPTIONS_KEY=['policy-adoptions'] as const
export const adoptionOptionsQuery=(owner:string)=>queryOptions({...privateQuery,queryKey:[...ADOPTIONS_KEY,'options',owner],queryFn:async({signal})=>{
 const reply=await request<AdoptionOptions>('/api/policy-adoptions/options?'+new URLSearchParams({pack:owner}),{signal})
 if(!reply||!Array.isArray(reply.packPublications)||reply.packPublications.length>256||!reply.packPublications.every(validAdoptionPack)||!Array.isArray(reply.coverages)||reply.coverages.length>256||!reply.coverages.every(row=>!!row&&validAdoptionSelection(row)&&validPolicyPublicationId(row.policyPublicationId)&&typeof row.title==='string'&&typeof row.policyVersion==='string'&&row.scope?.kind==='whole-narrative'&&statement(row.statement)&&validAdoptionPack(row.pack)))throw new AdoptionRequestError('adoption-invalid-response')
 return {...reply,packPublications:reply.packPublications.map(({publicationId,artifactId,version,content})=>({publicationId,artifactId,version,content}))}
}})
export const adoptionListQuery=()=>queryOptions({...privateQuery,queryKey:ADOPTIONS_KEY,queryFn:async({signal})=>{
 const reply=await request<{items:AdoptionSummary[]}>('/api/policy-adoptions',{signal})
 if(!reply||!Array.isArray(reply.items)||reply.items.length>256||!reply.items.every(row=>!!row&&identity(row.id,'adoption')&&digest(row.revision)&&digest(row.candidateDigest)&&validAdoptionPack(row.pack)&&selections(row.dependencies)&&['unreviewed','reviewed','changes-requested'].includes(row.reviewState)))throw new AdoptionRequestError('adoption-invalid-response')
 return reply
}})
export async function readAdoption(id:string,pack?:CoveragePackSelector,signal?:AbortSignal){return exactReply(await request<unknown>('/api/policy-adoptions/'+encodeURIComponent(id),{signal}),id,pack)}
export const adoptionQuery=(id?:string)=>queryOptions({...privateQuery,enabled:!!id,queryKey:[...ADOPTIONS_KEY,id],queryFn:({signal})=>readAdoption(id!,undefined,signal)})
/** Runtime preparation/checks are commands, never polling queries or retries. */
export const writeAdoption=(command:AdoptionCommand)=>request<AdoptionOutcome>('/api/policy-adoptions',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(command)},true)
export const recoverAdoptionOperation=(operationId:string)=>request<AdoptionOutcome>('/api/policy-operations/'+encodeURIComponent(operationId))
export function matchesAdoptionOutcome(result:AdoptionOutcome,pending:PendingAdoption){
 const {command,pack}=pending,reply=result?.adoption,receipt=result?.receipt
 if(!validAdoptionReply(reply)||result.outcomeKind!=='adoption'||!['committed','recovered'].includes(result.delivery)||!receipt||receipt.operationId!==command.operationId||receipt.kind!=='adoption-'+command.action||receipt.adoptionId!==reply.document.id||receipt.adoptionRevision!==reply.revision||receipt.adoptionCandidateId!==reply.document.candidate.id||receipt.candidateDigest!==reply.document.candidate.digest||result.policyRevision!==reply.document.candidate.basisDigest||receipt.revision!==result.policyRevision||result.policyId!==receipt.policyId||!samePublication(pack,reply.document.candidate.pack))return false
 const candidate=reply.document.candidate
 if(command.action==='prepare')return samePublication(command.pack,candidate.pack)&&sameSelections(command.coverages,candidate.dependencies.map(row=>row.selection))&&result.policyId===command.coverages[0]?.policyId&&reply.document.reviewState==='unreviewed'
 const review=reply.document.reviews.find(row=>row.id===receipt.reviewId)
 return command.adoptionId===reply.document.id&&command.candidateDigest===candidate.digest&&command.basisDigest===candidate.basisDigest&&!!review&&review.decision===command.decision&&(review.comment??'')===(command.comment??'')
}
