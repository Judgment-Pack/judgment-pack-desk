import {readPolicyAuthoringSource,readPolicyRevision,readPolicyObservation,PolicyRequestError,type PolicyWorkflow,type PolicySource,type PolicyConnectedIdentity,type RetainedArtifactRef,type PolicySourceObservation} from './client'
import {validPolicyAuthoringSource,validPolicyRevisionId,validPolicyPublicationId} from './authoringSource'
import {validPolicyId} from './chat'
import {readDocumentRecord,usablePages,needsPartialConsent,type DocumentPage,type DocumentRecord} from '../documents/record'
import {canonicalize,parseJsonText} from '../research/verify/canon'
import {sha256Hex} from '../research/verify/receipt'
import {sessionBearer} from '../mcp/session'
import {base64} from '../documents/client'

export type PolicyComparisonSelector={kind:'revision';revisionId:string}|{kind:'publication';publicationId:string}|{kind:'observation';observationId:string}
export type PolicyComparisonRequest={before:PolicyComparisonSelector;after:PolicyComparisonSelector}
export interface PolicyComparisonSide {
 selector:PolicyComparisonSelector
 identity:{revisionId?:string;publicationId?:string;observationId?:string;candidateDigest?:string;content:RetainedArtifactRef;bindingId?:string;sourceRevision?:string}
 text:string|null
 metadata:{kind:'narrative'|'extracted-source';title?:string;artifactVersion?:string;workflow?:PolicyWorkflow;workflowDigest?:string;source?:PolicySource|PolicyConnectedIdentity;original?:RetainedArtifactRef;extraction?:PolicySourceObservation['extraction'];observedAt?:string;retainedAt?:string;processing?:{status:DocumentRecord['processing']['status'];pageCount:number;truncated:boolean;pages:Omit<DocumentPage,'text'>[]};completeness:'complete'|'partial'|'unavailable'}
}
export type PolicyComparisonLimitation='informational-only'|'snapshot-only'|'incomplete-extraction'|'unavailable-extraction'|'mixed-projections'|'different-source-binding'|'equal-extracted-text'
export interface PolicyComparison {policyId:string;before:PolicyComparisonSide;after:PolicyComparisonSide;differences:{field:string;before:unknown;after:unknown}[];limitations:PolicyComparisonLimitation[]}
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)
const hash=(v:unknown):v is string=>typeof v==='string'&&/^[a-f0-9]{64}$/.test(v)
const prefixedHash=(v:unknown)=>typeof v==='string'&&/^sha256:[a-f0-9]{64}$/.test(v)
const bytes=(text:string)=>new TextEncoder().encode(text).length
const text=(v:unknown,max:number):v is string=>typeof v==='string'&&bytes(v)>0&&bytes(v)<=max&&!/[\x00-\x1f\x7f]/.test(v)
const ref=(v:unknown,max:number,min=1):v is RetainedArtifactRef=>object(v)&&hash(v.sha256)&&Number.isSafeInteger(v.bytes)&&(v.bytes as number)>=min&&(v.bytes as number)<=max
const retainedRef=(value:RetainedArtifactRef):RetainedArtifactRef=>({sha256:value.sha256,bytes:value.bytes})
const time=(v:unknown)=>typeof v==='string'&&Number.isFinite(Date.parse(v))
const fail=():never=>{throw new PolicyRequestError('invalid-policy')}
export function validPolicyComparisonSelector(value:unknown):value is PolicyComparisonSelector {
 if(!object(value)||Object.keys(value).length!==2)return false
 return value.kind==='revision'&&validPolicyRevisionId(value.revisionId)||value.kind==='publication'&&validPolicyPublicationId(value.publicationId)||value.kind==='observation'&&hash(value.observationId)
}
export function validPolicyComparisonRequest(value:unknown):value is PolicyComparisonRequest {
 return object(value)&&Object.keys(value).length===2&&validPolicyComparisonSelector(value.before)&&validPolicyComparisonSelector(value.after)
}
function validSource(value:unknown):value is PolicySource {
 return object(value)&&value.origin==='user-upload'&&text(value.name,255)&&['text/plain','text/markdown'].includes(value.mediaType as string)&&ref(value.content,131072,0)
}
async function savedSide(policyId:string,selector:Exclude<PolicyComparisonSelector,{kind:'observation'}>,signal?:AbortSignal):Promise<PolicyComparisonSide>{
 // The exact authoring-source boundary verifies retained bytes and resolves a
 // publication to its own revision. Never inspect or substitute the head.
 const source=await readPolicyAuthoringSource(selector.kind==='publication'?{policyId,publicationId:selector.publicationId}:{policyId,revisionId:selector.revisionId},signal)
 signal?.throwIfAborted()
 const revision=await readPolicyRevision(policyId,source.revisionId,signal)
 signal?.throwIfAborted()
 const c=revision?.candidate,w=revision?.workflow
 if(!c||!validPolicyAuthoringSource(source)||source.policyId!==policyId||(selector.kind==='publication'?source.publicationId!==selector.publicationId:source.revisionId!==selector.revisionId||source.publicationId!==undefined)||c.revisionId!==source.revisionId||c.digest!==source.candidateDigest||c.content?.sha256!==source.content.sha256||c.content?.bytes!==source.content.bytes||c.title!==source.title||c.artifactVersion!==source.artifactVersion||c.narrative!==source.narrative||!object(w)||Object.keys(w).length!==1||typeof w.independentReviewRequired!=='boolean'||!hash(revision.workflowDigest)||c.source!==undefined&&!validSource(c.source))fail()
 return {selector,identity:{revisionId:source.revisionId,...(source.publicationId?{publicationId:source.publicationId}:{}),candidateDigest:c.digest,content:retainedRef(c.content)},text:c.narrative,metadata:{kind:'narrative',title:c.title,artifactVersion:c.artifactVersion,workflow:{independentReviewRequired:w.independentReviewRequired},workflowDigest:revision.workflowDigest,...(c.source?{source:{name:c.source.name,mediaType:c.source.mediaType,origin:c.source.origin,content:retainedRef(c.source.content)},original:retainedRef(c.source.content)}:{}),completeness:'complete'}}
}
async function observationSide(policyId:string,selector:Extract<PolicyComparisonSelector,{kind:'observation'}>,signal?:AbortSignal):Promise<PolicyComparisonSide>{
 const reply=await readPolicyObservation(policyId,selector.observationId,signal)
 signal?.throwIfAborted()
 const o=reply?.observation
 if(!o||o.id!==selector.observationId||!hash(o.bindingId)||!object(o.source)||!text(o.source.provider,48)||!text(o.source.accountId,4096)||!text(o.source.resourceId,4096)||!text(o.sourceRevision,4096)||!time(o.observedAt)||!time(o.retainedAt)||!text(o.name,255)||!hash(o.semanticDigest)||o.availability!=='present'||o.coverage!=='snapshot-only'||o.lifecycle?.kind!=='not-exposed'||!ref(o.original,4<<20)||!ref(o.document,16<<20)||o.content?.bytesDigest!=='sha256:'+o.original.sha256||o.content.byteLength!==o.original.bytes||o.extraction?.content?.bytesDigest!=='sha256:'+o.document.sha256||o.extraction.content.byteLength!==o.document.bytes||o.extraction.content.mediaType!=='application/vnd.jpack.attachment+json'||o.extraction.profile!=='attachment-record-v1/canon'||!prefixedHash(o.extraction.configurationDigest)||!text(o.extraction.processorVersion,256))fail()
 const record=readDocumentRecord(reply.document),p=record.provenance
 if(record.document.id!==o.content.bytesDigest||record.document.size!==o.content.byteLength||record.document.mediaType!==o.content.mediaType||record.document.version!==o.sourceRevision||record.document.name!==o.name||p.processor!==o.extraction.processor||p.adapter.version!==o.extraction.processorVersion||p.source.kind!=='google-drive'||o.source.provider!=='google-drive'||p.source.fileId!==o.source.resourceId||p.source.version!==o.sourceRevision||record.original.retention!=='inline'||record.original.encoding!=='base64'||record.original.bytes===null)fail()
 // Reuse the canonical document verifier; base64 stays outside the comparison.
 const original=Uint8Array.from(atob(record.original.bytes!),c=>c.charCodeAt(0)),canonical=canonicalize(parseJsonText(JSON.stringify(reply.document)))
 if(base64(original)!==record.original.bytes||original.length!==o.original.bytes||canonical.length!==o.document.bytes||await sha256Hex(original)!==o.original.sha256||await sha256Hex(canonical)!==o.document.sha256)fail()
 signal?.throwIfAborted()
 const pages=usablePages(record),completeness=record.processing.status==='failed'||!pages.length?'unavailable':needsPartialConsent(record)?'partial':'complete'
 return {selector,identity:{observationId:o.id,bindingId:o.bindingId,sourceRevision:o.sourceRevision,content:retainedRef(o.document)},text:pages.length?pages.map(page=>page.text).join('\n\n'):null,metadata:{kind:'extracted-source',title:o.name,source:{provider:o.source.provider,accountId:o.source.accountId,resourceId:o.source.resourceId},original:retainedRef(o.original),extraction:{content:{bytesDigest:o.extraction.content.bytesDigest,byteLength:o.extraction.content.byteLength,mediaType:o.extraction.content.mediaType},profile:o.extraction.profile,processor:o.extraction.processor,processorVersion:o.extraction.processorVersion,configurationDigest:o.extraction.configurationDigest},observedAt:o.observedAt,retainedAt:o.retainedAt,processing:{status:record.processing.status,pageCount:record.content.pageCount,truncated:record.content.truncated,pages:record.content.pages.map(({number,status,extraction,chars,unmapped})=>({number,status,extraction,chars,unmapped}))},completeness}}
}
/** Each invocation performs current authenticated reads. No immutable cache or
 * partial success survives cancellation, refusal, or inconsistent identity. */
export async function resolvePolicyComparison(policyId:string,request:PolicyComparisonRequest,signal?:AbortSignal):Promise<PolicyComparison>{
 if(!validPolicyId(policyId)||!validPolicyComparisonRequest(request))fail()
 signal?.throwIfAborted()
 const selection=structuredClone(request),session=await sessionBearer()
 const resolve=async(selector:PolicyComparisonSelector)=>{try{signal?.throwIfAborted();return await (selector.kind==='observation'?observationSide(policyId,selector,signal):savedSide(policyId,selector,signal))}catch(error){signal?.throwIfAborted();throw error}}
 const before=await resolve(selection.before)
 if(await sessionBearer()!==session)fail()
 signal?.throwIfAborted()
 const after=await resolve(selection.after)
 if(await sessionBearer()!==session)fail()
 signal?.throwIfAborted()
 const differences:PolicyComparison['differences']=[],add=(field:string,a:unknown,b:unknown)=>{if(JSON.stringify(a)!==JSON.stringify(b))differences.push({field,before:a??null,after:b??null})}
 if(before.text!==after.text)differences.push({field:'text',before:before.text===null?null:{bytes:bytes(before.text)},after:after.text===null?null:{bytes:bytes(after.text)}})
 for(const field of ['candidateDigest','content','bindingId','sourceRevision'] as const)add(field,before.identity[field],after.identity[field])
 for(const field of ['title','artifactVersion','workflow','workflowDigest','source','original','extraction','observedAt','retainedAt','processing','completeness'] as const)add(field,before.metadata[field],after.metadata[field])
 const limitations:PolicyComparisonLimitation[]=['informational-only']
 if([before,after].some(s=>s.metadata.kind==='extracted-source'))limitations.push('snapshot-only')
 if([before,after].some(s=>s.metadata.completeness==='partial'))limitations.push('incomplete-extraction')
 if([before,after].some(s=>s.metadata.completeness==='unavailable'))limitations.push('unavailable-extraction')
 if(before.metadata.kind!==after.metadata.kind)limitations.push('mixed-projections')
 if(before.identity.observationId&&after.identity.observationId&&(before.identity.bindingId!==after.identity.bindingId||JSON.stringify(before.metadata.source)!==JSON.stringify(after.metadata.source)))limitations.push('different-source-binding')
 if(before.metadata.kind==='extracted-source'&&after.metadata.kind==='extracted-source'&&before.text!==null&&before.text===after.text)limitations.push('equal-extracted-text')
 return {policyId,before,after,differences,limitations}
}
