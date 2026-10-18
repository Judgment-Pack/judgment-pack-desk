import {queryOptions} from '@tanstack/react-query'
import {privateQuery} from '../data/privateQuery'
import {deskFetch} from '../files/client'
import {NoSession} from '../mcp/session'
import {sourceMessage} from '../i18n/source'
import type {PolicyActor} from '../policies/client'

export const POLICY_RECOVERY_KEY=['policy-recovery'] as const
export const POLICY_RECOVERY_INCLUDED=['policy-history','policy-operation-receipts','retained-policy-originals','retained-policy-extractions','policy-coverage-history','retained-coverage-pack-bytes'] as const
export const POLICY_RECOVERY_EXCLUDED=['chat-history','pack-graph-drafts','memory','project-files','jobs','credentials','source-consent','browser-journals'] as const
export interface PolicyRecoveryCounts {policies:number;revisions:number;reviews:number;publications:number;operations:number;sourceObservations:number;externalVersions:number;coverages:number;coverageCandidates:number;coverageReviews:number;blobs:number;blobBytes:number}
interface RecoveryScope {scope:'project-policies';included:readonly string[];excluded:readonly string[]}
export interface PolicyRecoveryStatus extends RecoveryScope {projectId:string;projectPath:string;revision?:string;available:boolean;canInspect:boolean;canRestore:boolean;reason?:string;counts?:PolicyRecoveryCounts;limits:{archiveBytes:number;expandedBytes:number;entries:number;manifestBytes:number}}
interface RecoveryAuthority {currentnessUnknown:true;freshReviewRequired:true;sourceConsentRequired:true}
export interface PolicyRecoveryPreview extends RecoveryScope {projectId:string;projectPath:string;archiveDigest:string;archiveBytes:number;ledgerDigest:string;schemaVersion:2;archiveVersion:1;capturedAt:string;counts:PolicyRecoveryCounts;destination:string;destinationRevision:string;authority:RecoveryAuthority;restartRequired:true}
export interface PolicyRestoreRequest {operationId:string;projectId:string;archiveDigest:string;archiveBytes:number;ledgerDigest:string;destination:string;destinationRevision:string}
export interface PolicyRestoreOutcome {delivery:'committed'|'recovered';outcomeKind:'policy-recovery';projectId:string;receipt:PolicyRestoreRequest&{kind:'policy-recovery-restore';requestDigest:string;snapshotId:string;at:string;actor:PolicyActor};restartRequired:true;authority:RecoveryAuthority}
export class PolicyRecoveryError extends Error {
 constructor(readonly code:string,readonly uncertain=false){super(recoveryFailureMessage(code));this.name='PolicyRecoveryError'}
}
function recoveryFailureMessage(code:string){switch(code){
 case 'unauthorized':case 'forbidden':return sourceMessage('Your session cannot access Policy recovery. Sign in again to continue.')
 case 'archive-integrity':return sourceMessage('The Policy archive is incomplete or inconsistent. Keep the original and choose a valid archive.')
 case 'archive-unsupported':return sourceMessage('This file is not a supported Policy recovery archive. Chat backups use their separate restore action.')
 case 'archive-capacity':case 'too-large':return sourceMessage('The Policy archive exceeds the supported recovery limits.')
 case 'recovery-conflict':return sourceMessage('The project or reviewed archive basis changed. Inspect the archive again before restoring.')
 case 'destination-conflict':return sourceMessage('The destination is no longer available or its parent changed. Choose a new destination and inspect again.')
 case 'idempotency-conflict':return sourceMessage('This restore operation does not match its original request. Check its exact result before continuing.')
 case 'not-found':case 'recovery-unresolved':return sourceMessage('The restore result is still unresolved. Check again; do not repeat the restore or remove its destination.')
 case 'delivery-unknown':return sourceMessage('Desk could not confirm the restore outcome. Check its exact result; the restore will not be repeated.')
 case 'journal-clear-unavailable':return sourceMessage('Restore was confirmed, but this browser could not clear its recovery record. Checking remains read-only; do not repeat restore.')
 case 'bad-request':return sourceMessage('Choose a Policy archive and a new absolute destination with an existing private parent folder.')
 default:return sourceMessage('Policy recovery is unavailable. Your current project remains selected; preserve the archive and destination.')
}}
async function responseError(response:Response,mutation=false):Promise<never>{
 let body:{code?:unknown;outcome?:unknown}|undefined
 try{body=await response.json()}catch{/* Only authored codes, never arbitrary response prose. */}
 const code=response.headers.get('X-Jpack-Desk-Refusal')??body?.code
 const uncertain=mutation&&body?.outcome!=='not-committed'
 throw new PolicyRecoveryError(uncertain?'delivery-unknown':typeof code==='string'?code:response.status===401||response.status===403?'unauthorized':'unavailable',uncertain)
}
async function request<T>(path:string,init:RequestInit={},mutation=false):Promise<T>{
 let response:Response
 try{response=await deskFetch('/api/policy-recovery'+path,init)}catch(cause){throw new PolicyRecoveryError(cause instanceof NoSession?'unauthorized':mutation?'delivery-unknown':'unavailable',mutation)}
 if(!response.ok)return responseError(response,mutation)
 try{return await response.json() as T}catch{throw new PolicyRecoveryError(mutation?'delivery-unknown':'unavailable',mutation)}
}
function scopeMatches(value:RecoveryScope){return value?.scope==='project-policies'&&JSON.stringify(value.included)===JSON.stringify(POLICY_RECOVERY_INCLUDED)&&JSON.stringify(value.excluded)===JSON.stringify(POLICY_RECOVERY_EXCLUDED)}
const string=(value:unknown):value is string=>typeof value==='string'&&value.length>0
const digest=(value:unknown):value is string=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value)
function validActor(value:PolicyActor){
 if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).some(key=>!['kind','subject','issuer'].includes(key))||!string(value.subject)||new TextEncoder().encode(value.subject).length>1024)return false
 return value.kind==='local'?(value.issuer===undefined||value.issuer===''):value.kind==='oidc'&&string(value.issuer)&&new TextEncoder().encode(value.issuer).length<=4096
}
function validTime(value:unknown){return typeof value==='string'&&/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(value)&&Number.isFinite(Date.parse(value))&&new Date(value).toISOString().replace('.000Z','Z')===value}
function countsValid(value?:PolicyRecoveryCounts){return !!value&&['policies','revisions','reviews','publications','operations','sourceObservations','externalVersions','coverages','coverageCandidates','coverageReviews','blobs','blobBytes'].every(key=>Number.isSafeInteger(value[key as keyof PolicyRecoveryCounts])&&value[key as keyof PolicyRecoveryCounts]>=0)}
export async function readPolicyRecovery(signal?:AbortSignal){
 const value=await request<PolicyRecoveryStatus>('',{signal})
 if(!scopeMatches(value)||!string(value.projectId)||!string(value.projectPath)||typeof value.available!=='boolean'||typeof value.canInspect!=='boolean'||typeof value.canRestore!=='boolean'||value.available&&(!string(value.revision)||!countsValid(value.counts))||!value.limits||!Object.values(value.limits).every(n=>Number.isSafeInteger(n)&&n>0))throw new PolicyRecoveryError('unavailable')
 return value
}
// Status verifies a bounded archive closure; it is not a cheap mutable snapshot.
export const policyRecoveryQuery=(enabled=true)=>queryOptions({...privateQuery,enabled,refetchInterval:false,queryKey:POLICY_RECOVERY_KEY,queryFn:({signal})=>readPolicyRecovery(signal)})
function upload(settings:unknown,file:File){const body=new FormData();body.append('settings',JSON.stringify(settings));body.append('archive',file);return body}
export async function inspectPolicyRecovery(file:File,destination:string){
 const value=await request<PolicyRecoveryPreview>('/inspect',{method:'POST',body:upload({destination},file)})
 if(!scopeMatches(value)||!string(value.projectId)||!string(value.projectPath)||!string(value.archiveDigest)||value.archiveBytes!==file.size||!string(value.ledgerDigest)||!string(value.destination)||!string(value.destinationRevision)||value.archiveVersion!==1||value.schemaVersion!==2||!countsValid(value.counts)||!string(value.capturedAt)||value.authority?.currentnessUnknown!==true||value.authority.freshReviewRequired!==true||value.authority.sourceConsentRequired!==true||value.restartRequired!==true)throw new PolicyRecoveryError('archive-unsupported')
 return value
}
export const restorePolicyRecovery=(settings:PolicyRestoreRequest,file:File)=>request<PolicyRestoreOutcome>('/restore',{method:'POST',body:upload(settings,file)},true)
export const recoverPolicyRestore=(operationId:string)=>request<PolicyRestoreOutcome>('/operations/'+encodeURIComponent(operationId))
export function policyRestoreResultMatches(result:PolicyRestoreOutcome,request:PolicyRestoreRequest,actor?:PolicyActor){
 const receipt=result?.receipt
 return result?.outcomeKind==='policy-recovery'&&['committed','recovered'].includes(result.delivery)&&result.projectId===request.projectId&&result.restartRequired===true&&result.authority?.currentnessUnknown===true&&result.authority.freshReviewRequired===true&&result.authority.sourceConsentRequired===true&&receipt?.kind==='policy-recovery-restore'&&digest(receipt.snapshotId)&&digest(receipt.requestDigest)&&validTime(receipt.at)&&validActor(receipt.actor)&&(!actor||receipt.actor.kind===actor.kind&&receipt.actor.subject===actor.subject&&(receipt.actor.issuer??'')===(actor.issuer??''))&&['operationId','projectId','archiveDigest','archiveBytes','ledgerDigest','destination','destinationRevision'].every(key=>receipt[key as keyof PolicyRestoreRequest]===request[key as keyof PolicyRestoreRequest])
}
export async function downloadPolicyRecovery(revision:string){
 let response:Response
 try{response=await deskFetch('/api/policy-recovery/archive',{headers:{'If-Match':revision}})}catch(cause){throw new PolicyRecoveryError(cause instanceof NoSession?'unauthorized':'unavailable')}
 if(!response.ok)return responseError(response)
 const digest=response.headers.get('X-Policy-Archive-Digest');if(!string(digest))throw new PolicyRecoveryError('archive-integrity')
 const blob=await response.blob(),url=URL.createObjectURL(blob),link=document.createElement('a');link.href=url;link.download='jpack-policy-recovery.zip';link.click();setTimeout(()=>URL.revokeObjectURL(url),1000)
 return digest
}
