import {validPolicyAuthoringSource,type PolicyAuthoringSource,type PolicySourceSelection} from './authoringSource'
import {queryOptions} from '@tanstack/react-query'
import {deskFetch} from '../files/client'
import {NoSession} from '../mcp/session'
import {privateQuery} from '../data/privateQuery'
import {sourceMessage} from '../i18n/source'

export interface PolicyActor {kind:string;subject?:string;issuer?:string;name?:string}
export interface PolicySource {name:string;mediaType:string;origin:'user-upload';content:{sha256:string;bytes:number}}
export interface PolicyImport {name:string;mediaType:string;data:string}
export interface PolicyCandidate {source?:PolicySource;title:string;narrative:string;artifactVersion:string;digest:string;revisionId:string;content:{sha256:string;bytes:number}}
export interface PolicyWorkflow {independentReviewRequired:boolean}
export interface PolicyRevision {source?:PolicySource;id:string;sequence:number;at:string;actor:PolicyActor;digest:string;title:string;artifactVersion:string;content:{sha256:string;bytes:number};workflow:PolicyWorkflow;workflowDigest:string}
export interface PolicyRevisionReply {sequence:number;candidate:PolicyCandidate;workflow:PolicyWorkflow;workflowDigest:string;at:string;actor:PolicyActor}
export interface PolicyReview {id:string;sequence:number;candidateRevisionId:string;at:string;actor:PolicyActor;candidateDigest:string;workflowDigest:string;decision:'approve'|'request-changes';comment?:string}
export interface PolicyPublication {id:string;sequence:number;candidateRevisionId:string;at:string;actor:PolicyActor;artifactVersion:string;candidateDigest:string;workflowDigest:string;reviewId:string;content:{sha256:string;bytes:number}}
export interface PolicyDocument {id:string;managementMode:'desk-managed';createdAt:string;updatedAt:string;candidate:PolicyCandidate;workflow:PolicyWorkflow;workflowDigest:string;reviewState:'unreviewed'|'approved'|'changes-requested';currentReviewId?:string;revisions:PolicyRevision[];reviews:PolicyReview[];publications:PolicyPublication[]}
export interface PolicyReply {revision:string;document:PolicyDocument}
export interface PolicySummary {id:string;candidateDigest:string;currentCandidatePublished:boolean;title:string;artifactVersion:string;updatedAt:string;reviewState:PolicyDocument['reviewState'];publicationCount:number;latestPublication?:PolicyPublication}
export interface PolicyMutationReply {delivery:'committed'|'recovered';receipt:unknown;policy:PolicyReply;sourceSync?:PolicySourceSync}
export interface PolicyWrite {path:string;method:'POST'|'PUT';body:Record<string,unknown>}
export class PolicyRequestError extends Error {
 constructor(readonly code:string,readonly uncertain=false){super(policyFailureMessage(code));this.name='PolicyRequestError'}
}
function policyFailureMessage(code:string){
 switch(code){
 case 'invalid-policy-source':return sourceMessage('Import a UTF-8 .txt or .md file up to 128 KiB with a simple filename.')
 case 'source-update-unresolved':return sourceMessage('Review the latest connected source update before publishing this candidate.')
 case 'source-conflict':return sourceMessage('The policy or source changed. Review the latest source state before continuing.')
 case 'source-unsupported':return sourceMessage('The installed Gateway does not support automatic policy source checks.')
 case 'source-authorization-ended':return sourceMessage('Reconnect this source with current administrator consent.')
 case 'revision-conflict':return sourceMessage('The saved policy changed. Your edits are still here. Compare the latest saved draft before saving again.')
 case 'access-denied':case 'session-required':case 'unauthorized':case 'forbidden':return sourceMessage('Your session cannot access this policy. Sign in again to continue.')
 case 'review-incomplete':return sourceMessage('Approve the exact saved candidate before publishing.')
 case 'independent-review-required':case 'unsupported-workflow':return sourceMessage('This workflow requires a different reviewer. Independent review is unavailable in this local installation.')
 case 'version-conflict':return sourceMessage('Publish with a version greater than the latest publication.')
 case 'invalid-policy':case 'invalid-request':case 'bad-request':case 'invalid-version':return sourceMessage('Check the title, policy text and artifact version, then try again.')
 case 'idempotency-conflict':return sourceMessage('The operation identity does not match its original request. Your edits are still here.')
 case 'operation-unresolved':return sourceMessage('No completed operation was found yet. Its result is still unresolved. Check again before continuing.')
 case 'delivery-unknown':return sourceMessage('The operation response was interrupted. Recover its exact result before making another change.')
 default:return sourceMessage('The policy operation could not be completed. Your edits are still here.')
 }
}
async function request<T>(path:string,init:RequestInit={},root='/api/policies'):Promise<T>{
 let response:Response
 try{response=await deskFetch(root+path,init)}catch(cause){if(cause instanceof NoSession)throw new PolicyRequestError('unauthorized',!!init.method&&init.method!=='GET');throw new PolicyRequestError(init.method&&init.method!=='GET'?'delivery-unknown':'unavailable',!!init.method&&init.method!=='GET')}
 let body:unknown
 try{body=await response.json()}catch{throw new PolicyRequestError(init.method&&init.method!=='GET'?'delivery-unknown':'unavailable',!!init.method&&init.method!=='GET')}
 if(!response.ok){
  const code=response.headers.get('X-Jpack-Desk-Refusal')||(body as {code?:string}|null)?.code
  const outcome=(body as {outcome?:string}|null)?.outcome
  const uncertain=!!init.method&&init.method!=='GET'&&(outcome==='unknown'||response.status>=500&&outcome!=='not-committed')
  throw new PolicyRequestError(uncertain?'delivery-unknown':typeof code==='string'?code:response.status===401||response.status===403?'access-denied':'unavailable',uncertain)
 }
 return body as T
}
export const POLICIES_KEY=['policies'] as const
export const policyHref=(id:string)=>'/policies/'+encodeURIComponent(id)
export const readPolicy=(id:string,signal?:AbortSignal)=>request<PolicyReply>('/'+encodeURIComponent(id),{signal})
export const policyListQuery=()=>queryOptions({...privateQuery,queryKey:POLICIES_KEY,queryFn:({signal})=>request<{items:PolicySummary[]}>('',{signal})})
export const policyQuery=(id:string)=>queryOptions({...privateQuery,queryKey:[...POLICIES_KEY,id],queryFn:({signal})=>readPolicy(id,signal)})
/** New commands are explicit writes. Recovery uses the read-only receipt route. */
export const writePolicy=(operation:PolicyWrite)=>request<PolicyMutationReply>(operation.path,{method:operation.method,headers:{'Content-Type':'application/json'},body:JSON.stringify(operation.body)})

export const recoverPolicyOperation=(operationId:string)=>request<PolicyMutationReply>('/'+encodeURIComponent(operationId),{},'/api/policy-operations')
export const readPolicyRevision=(id:string,revisionId:string,signal?:AbortSignal)=>request<PolicyRevisionReply>('/'+encodeURIComponent(id)+'/revisions/'+encodeURIComponent(revisionId),{signal})

export async function readPolicySource(id:string):Promise<Uint8Array>{
 let response:Response
 try{response=await deskFetch('/api/policies/'+encodeURIComponent(id)+'/source')}catch(cause){throw new PolicyRequestError(cause instanceof NoSession?'unauthorized':'unavailable')}
 if(!response.ok)throw new PolicyRequestError(response.status===401||response.status===403?'access-denied':'unavailable')
 return new Uint8Array(await response.arrayBuffer())
}

export interface PolicySourceObservation {id:string;bindingId:string;source:{provider:string;accountId:string;resourceId:string};sourceRevision:string;observedAt:string;retainedAt:string;name:string;availability:'present';coverage:'snapshot-only';lifecycle:{kind:'not-exposed'};content:{bytesDigest:string;byteLength:number;mediaType:string};extraction:{content:{bytesDigest:string;byteLength:number;mediaType:string};profile:string;processor:string;processorVersion:string;configurationDigest:string};original:{sha256:string;bytes:number};document:{sha256:string;bytes:number};semanticDigest:string}
export interface PolicySourceResolution {observationId:string;candidateRevisionId:string;candidateDigest:string;workflowDigest:string;at:string;actor:PolicyActor;comment:string}
export interface PolicySourceSync {revision:string;supported:boolean;pending:boolean;state?:{version:1;generation:number;state:'binding'|'active'|'paused'|'revoked'|'authorization-ended';frequencyMinutes:number;lastAttempt?:string;lastSuccess?:string;nextCheck?:string;errorCode?:string;gap:boolean;grant:{actor:PolicyActor;at:string;resourceId:string};binding?:{bindingId:string;source:{provider:string;accountId:string;resourceId:string}};observations:PolicySourceObservation[];resolutions:PolicySourceResolution[]}}
export const readPolicySourceSync=(id:string,signal?:AbortSignal)=>request<PolicySourceSync>('/'+encodeURIComponent(id)+'/source-sync',{signal})
export const policySourceSyncQuery=(id?:string)=>queryOptions({...privateQuery,enabled:!!id,queryKey:[...POLICIES_KEY,id,'source-sync'],queryFn:({signal})=>readPolicySourceSync(id!,signal)})
export const readPolicyObservation=(id:string,observationId:string,signal?:AbortSignal)=>request<{observation:PolicySourceObservation;document:unknown}>('/'+encodeURIComponent(id)+'/source-sync/observations/'+encodeURIComponent(observationId),{signal})
export async function downloadPolicyObservation(id:string,observation:PolicySourceObservation){
 const response=await deskFetch('/api/policies/'+encodeURIComponent(id)+'/source-sync/observations/'+encodeURIComponent(observation.id)+'/original')
 if(!response.ok)throw new PolicyRequestError('unavailable')
 const blob=await response.blob(),url=URL.createObjectURL(blob),link=document.createElement('a');link.href=url;link.download=observation.name;link.click();URL.revokeObjectURL(url)
}

export async function readPolicyAuthoringSource(selection:PolicySourceSelection,signal?:AbortSignal):Promise<PolicyAuthoringSource>{
 const query=new URLSearchParams(selection.publicationId?{publicationId:selection.publicationId}:{revisionId:selection.revisionId!})
 const source=await request<unknown>('/'+encodeURIComponent(selection.policyId)+'/authoring-source?'+query,{signal})
 if(!validPolicyAuthoringSource(source)||source.policyId!==selection.policyId||(selection.publicationId?source.publicationId!==selection.publicationId:source.revisionId!==selection.revisionId||source.publicationId!==undefined))throw new PolicyRequestError('invalid-policy')
 return source
}
