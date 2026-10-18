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
export interface PolicyDocument {id:string;managementMode:SourceOwnership['mode'];createdAt:string;updatedAt:string;candidate:PolicyCandidate;workflow:PolicyWorkflow;workflowDigest:string;reviewState:'unreviewed'|'approved'|'changes-requested';currentReviewId?:string;revisions:PolicyRevision[];reviews:PolicyReview[];publications:PolicyPublication[]}
export interface PolicyRecoveryInfo {snapshotId:string;restoredAt:string;currentnessUnknown:true;freshReviewRequired:boolean;sourceConsentRequired:boolean}
export interface PolicyReply {recovery?:PolicyRecoveryInfo;revision:string;document:PolicyDocument;sourceAuthority?:SourceAuthoritySummary;currentCandidatePublished?:boolean}
export interface PolicySummary {recovery?:PolicyRecoveryInfo;id:string;candidateDigest:string;currentCandidatePublished:boolean;title:string;artifactVersion:string;updatedAt:string;reviewState:PolicyDocument['reviewState'];publicationCount:number;latestPublication?:PolicyPublication;managementMode?:SourceOwnership['mode'];sourceAuthority?:SourceAuthoritySummary}
export interface PolicyMutationReply {delivery:'committed'|'recovered';receipt:unknown;policy:PolicyReply;sourceSync?:PolicySourceSync;lifecycle?:PolicyLifecycle}
export interface PolicyWrite {path:string;method:'POST'|'PUT';body:Record<string,unknown>}
export class PolicyRequestError extends Error {
 constructor(readonly code:string,readonly uncertain=false){super(policyFailureMessage(code));this.name='PolicyRequestError'}
}
function policyFailureMessage(code:string){
 switch(code){
 case 'restored-source-consent-required':return sourceMessage('Archived source authorization is inactive. Choose the source again and review new consent before provider access.')
 case 'restored-review-required':return sourceMessage('This restored Policy needs fresh review and a new publication before release activation.')
 case 'authority-conflict':return sourceMessage('The Policy, source or configuration changed. Review the latest state before continuing.')
 case 'source-status-unavailable':return sourceMessage('A current usable source snapshot is required. The last reviewed source status remains retained.')
 case 'external-ownership':return sourceMessage('The Policy’s ownership does not allow this action. Review its ownership in Source.')
 case 'ownership-review-required':return sourceMessage('This publication belongs to an earlier ownership period. Review the Policy’s current ownership before continuing.')
 case 'already-applied':return sourceMessage('This exact source observation and configuration have already been retained.')
 case 'import-draft-required':return sourceMessage('Draft source status requires an explicit import into a new Desk Draft.')
 case 'unsupported-lifecycle-mapping':return sourceMessage('This source does not expose a supported lifecycle mapping.')
 case 'source-status-extraction-unavailable':return sourceMessage('No readable source pages are available to import. Review the retained original.')
 case 'source-status-refresh-unavailable':return sourceMessage('The source decision was recorded. Current status could not be refreshed; reload source status before another change.')
 case 'lifecycle-conflict':return sourceMessage('The policy or lifecycle changed. Review its current state before continuing.')
 case 'invalid-lifecycle-time':return sourceMessage('Use UTC whole-second dates with expiry after effectiveness; activation cannot be backdated.')
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
export interface PolicyLifecycleEvent {id:string;sequence:number;action:'activate'|'withdraw';publicationId:string;candidateDigest:string;at:string;actor:PolicyActor;provenance:'admin-configured';effectiveAt?:string;expiresAt?:string;supersedesPublicationId?:string;rationale?:string}
export interface PolicyReleaseLifecycle {activationAllowed?:boolean;publicationId:string;status:'published'|'effective'|'superseded'|'expired'|'withdrawn';effectiveAt?:string;expiresAt?:string;supersededBy?:string;activationId?:string;withdrawalId?:string}
export interface PolicyLifecycle {recovery?:PolicyRecoveryInfo;revision:string;policyRevision:string;observedAt:string;releases:PolicyReleaseLifecycle[];events:PolicyLifecycleEvent[]}
export interface PolicyLifecycleOutcome {outcomeKind:'lifecycle';delivery:'committed'|'recovered';policyId:string;policyRevision:string;receipt:unknown;lifecycle:PolicyLifecycle}
export const writePolicyLifecycle=(operation:PolicyWrite)=>request<PolicyLifecycleOutcome>(operation.path,{method:operation.method,headers:{'Content-Type':'application/json'},body:JSON.stringify(operation.body)})
export const recoverPolicyLifecycleOperation=(operationId:string)=>request<PolicyLifecycleOutcome>('/'+encodeURIComponent(operationId),{},'/api/policy-operations')
export const readPolicyLifecycle=(id:string,signal?:AbortSignal)=>request<PolicyLifecycle>('/'+encodeURIComponent(id)+'/lifecycle',{signal})
export const policyLifecycleQuery=(id:string,enabled=true)=>queryOptions({...privateQuery,enabled,queryKey:[...POLICIES_KEY,id,'lifecycle'],queryFn:({signal})=>readPolicyLifecycle(id,signal)})
export const readPolicyRevision=(id:string,revisionId:string,signal?:AbortSignal)=>request<PolicyRevisionReply>('/'+encodeURIComponent(id)+'/revisions/'+encodeURIComponent(revisionId),{signal})

export async function readPolicySource(id:string):Promise<Uint8Array>{
 let response:Response
 try{response=await deskFetch('/api/policies/'+encodeURIComponent(id)+'/source')}catch(cause){throw new PolicyRequestError(cause instanceof NoSession?'unauthorized':'unavailable')}
 if(!response.ok)throw new PolicyRequestError(response.status===401||response.status===403?'access-denied':'unavailable')
 return new Uint8Array(await response.arrayBuffer())
}

export interface PolicySourceObservation {id:string;bindingId:string;source:{provider:string;accountId:string;resourceId:string};sourceRevision:string;observedAt:string;retainedAt:string;name:string;availability:'present';coverage:'snapshot-only';lifecycle:{kind:'not-exposed'};content:{bytesDigest:string;byteLength:number;mediaType:string};extraction:{content:{bytesDigest:string;byteLength:number;mediaType:string};profile:string;processor:string;processorVersion:string;configurationDigest:string};original:{sha256:string;bytes:number};document:{sha256:string;bytes:number};semanticDigest:string}
export interface PolicySourceResolution {observationId:string;candidateRevisionId:string;candidateDigest:string;workflowDigest:string;at:string;actor:PolicyActor;comment:string}
export interface PolicySourceSync {recovery?:PolicyRecoveryInfo;revision:string;supported:boolean;pending:boolean;state?:{version:1;generation:number;state:'binding'|'active'|'paused'|'revoked'|'authorization-ended';frequencyMinutes:number;lastAttempt?:string;lastSuccess?:string;nextCheck?:string;errorCode?:string;gap:boolean;grant:{actor:PolicyActor;at:string;resourceId:string};binding?:{bindingId:string;source:{provider:string;accountId:string;resourceId:string}};observations:PolicySourceObservation[];resolutions:PolicySourceResolution[]}}
export const readPolicySourceSync=(id:string,signal?:AbortSignal)=>request<PolicySourceSync>('/'+encodeURIComponent(id)+'/source-sync',{signal})
export const policySourceSyncQuery=(id?:string)=>queryOptions({...privateQuery,enabled:!!id,queryKey:[...POLICIES_KEY,id,'source-sync'],queryFn:({signal})=>readPolicySourceSync(id!,signal)})
export interface PolicyConnectedIdentity {provider:string;accountId:string;resourceId:string}
export interface PolicySourceContent {bytesDigest:string;byteLength:number;mediaType:string}
export interface RetainedArtifactRef {sha256:string;bytes:number}
export interface PolicySourceBinding {version:number;operationId:string;bindingId:string;source:PolicyConnectedIdentity;createdAt:string;state:string;coverage:string;lifecycle:{kind:string}}
export interface SourceOwnership {mode:'desk-managed'|'externally-managed';inherited:boolean;eventId?:string;at?:string;actor?:PolicyActor}
export interface SourceStatusConfiguration {id:string;bindingId:string;source:PolicyConnectedIdentity;capability:'not-exposed';fallbackStatus:'draft'|'published';at:string;actor:PolicyActor}
export interface SourceStatusPreview {eligible:boolean;reason?:string;status?:string;provenance?:'source-provided'|'admin-configured';configurationId?:string;observationId?:string;bindingId?:string;sourceRevision?:string;source?:PolicyConnectedIdentity;content?:PolicySourceContent;rawStatus?:string;evidence?:RetainedArtifactRef}
export interface ExternalPolicyVersion {id:string;eventId:string;configurationId:string;observation:PolicySourceObservation;status:string;provenance:'source-provided'|'admin-configured';rawStatus?:string;evidence?:RetainedArtifactRef;at:string;actor:PolicyActor}
export interface SourceAuthoritySummary {revision:string;ownership:SourceOwnership;externalVersionCount:number;lastApplied?:ExternalPolicyVersion}
export interface SourceAuthorityEvent {id:string;sequence:number;kind:'configure'|'set-ownership'|'apply-external'|'import-draft';at:string;actor:PolicyActor;rationale:string;policySequence:number;configuration?:SourceStatusConfiguration;managementMode?:SourceOwnership['mode'];externalVersion?:ExternalPolicyVersion;importedDraft?:{configurationId:string;observation:PolicySourceObservation;candidateRevisionId:string;candidateDigest:string;workflowDigest:string}}
export interface SourceStatusReply {recovery?:PolicyRecoveryInfo;revision:string;policyRevision:string;sourceRevision:string;candidateDigest:string;candidateRevisionId:string;ownership:SourceOwnership;binding?:PolicySourceBinding;capability:{kind:'not-exposed'|'unavailable';mappingSupported:boolean};availability:{state:string;usable:boolean;gap:boolean;errorCode?:string;lastSuccess?:string};currentConfig?:SourceStatusConfiguration;currentPreview:SourceStatusPreview;lastApplied?:ExternalPolicyVersion;configurations:SourceStatusConfiguration[];externalVersions:ExternalPolicyVersion[];events:SourceAuthorityEvent[];actions:Record<'configure'|'setOwnership'|'applyExternal'|'importDraft',{allowed:boolean;reason?:string}>}
export type SourceStatusCommand={operationId:string;baseRevision:string;baseSourceRevision:string;baseAuthorityRevision:string;rationale:string}&(
 {action:'configure';bindingId:string;fallbackStatus:'draft'|'published'}|{action:'set-ownership';managementMode:SourceOwnership['mode']}|{action:'apply-external';observationId:string;configurationId:string}|{action:'import-draft';observationId:string;configurationId:string;draft:{title:string;narrative:string;artifactVersion:string}})
export interface SourceAuthorityOutcome {outcomeKind:'source-authority';delivery:'committed'|'recovered';policyId:string;policyRevision:string;sourceStatus:SourceStatusReply;receipt:{operationId:string;kind:string;policyId:string;authorityRevision:string;authorityEventId:string;configurationId?:string;observationId?:string;externalVersionId?:string}}
export const readPolicySourceStatus=(id:string,signal?:AbortSignal)=>request<SourceStatusReply>('/'+encodeURIComponent(id)+'/source-status',{signal})
export const policySourceStatusQuery=(id:string,enabled=true)=>queryOptions({...privateQuery,enabled,queryKey:[...POLICIES_KEY,id,'source-status'],queryFn:({signal})=>readPolicySourceStatus(id,signal)})
export const writePolicySourceStatus=(operation:PolicyWrite)=>request<SourceAuthorityOutcome>(operation.path,{method:operation.method,headers:{'Content-Type':'application/json'},body:JSON.stringify(operation.body)})
export const recoverPolicySourceStatusOperation=(operationId:string)=>request<SourceAuthorityOutcome>('/'+encodeURIComponent(operationId),{},'/api/policy-operations')
export const readExternalPolicyVersion=(id:string,versionId:string,signal?:AbortSignal)=>request<{version:ExternalPolicyVersion;configuration:SourceStatusConfiguration;document:unknown}>('/'+encodeURIComponent(id)+'/source-status/versions/'+encodeURIComponent(versionId),{signal})
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
