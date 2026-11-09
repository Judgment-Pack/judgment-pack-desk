import { validSearchReference, type SearchReference } from './results'

/** Host-recorded invocation details; never inferred from a response's reference order. */
export interface SearchStep {
 query: string
 provider?: string
 submitted?: true
 reference?: SearchReference
}
export function validSearchStep(value:unknown):value is SearchStep {
 const v=value as SearchStep|undefined
 return !!v && typeof v.query==='string' && v.query.trim().length>0 && v.query.length<=2000
  && (v.provider===undefined || typeof v.provider==='string' && /^[a-z][a-z0-9-]{0,47}$/.test(v.provider))
  && (v.submitted===undefined || v.submitted===true)
  && (v.reference===undefined || v.submitted===true && validSearchReference(v.reference) && Object.keys(v.reference).every(k=>['id','digest','request'].includes(k)) && v.reference.request.query===v.query)
  && Object.keys(v).every(k=>['query','provider','submitted','reference'].includes(k))
}
export const searchReferenceKey=(ref:SearchReference)=>JSON.stringify([ref.id,ref.digest,ref.request.connection,ref.request.revision,ref.request.query,ref.request.maxResults])

/** Host-authored physical refusal, separate from canonical tool results. */
export interface ExternalControl {
 readonly version:1
 readonly kind:'research-choice'|'setup-required'|'provided-only'
 readonly hostOperationId:string
 readonly sourceBindingDigest:string
 readonly requestId:string
 readonly query?:string
 readonly provider?:string
 readonly reason:'choice-required'|'configuration-required'|'connection-required'|'verifier-required'|'provided-only'
}
export function validExternalControl(value:unknown):value is ExternalControl {
 if(!value||typeof value!=='object'||Array.isArray(value))return false
 const c=value as ExternalControl
 const finite=(v:unknown,max:number)=>typeof v==='string'&&v.length>0&&new TextEncoder().encode(v).length<=max&&!/[\r\n\0]|[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(v)
 return Object.keys(c).every(k=>['version','kind','hostOperationId','sourceBindingDigest','requestId','query','provider','reason'].includes(k))
  && c.version===1&&finite(c.hostOperationId,128)&&typeof c.sourceBindingDigest==='string'&&/^[a-f0-9]{64}$/.test(c.sourceBindingDigest)&&finite(c.requestId,128)
  && (c.kind==='research-choice'&&c.reason==='choice-required'||c.kind==='provided-only'&&c.reason==='provided-only'||c.kind==='setup-required'&&['configuration-required','connection-required','verifier-required'].includes(c.reason))
  && (c.query===undefined||validSearchStep({query:c.query})&&!/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(c.query))
  && (c.provider===undefined||typeof c.provider==='string'&&/^[a-z][a-z0-9-]{0,47}$/.test(c.provider))
}
export function externalControlMessage(c:ExternalControl):string {
 if(c.kind==='research-choice')return 'Choose research to search external sources. No research started for this operation.'
 if(c.kind==='provided-only')return 'This request uses only the conversation and supplied sources. No research started for this operation.'
 return 'External sources need configuration. No research started for this operation.'
}

/** Shape/tuple reader only. Live authority remains the guarded server owner. */
export function validExternalControlForTool(value:unknown,tool:unknown,args:unknown):value is ExternalControl {
 if(!validExternalControl(value))return false
 if(tool==='search_sources')return !!args&&typeof args==='object'&&!Array.isArray(args)&&typeof (args as {query?:unknown}).query==='string'&&(args as {query:string}).query.trim()===value.query
 return (tool==='read_link'||tool==='explore_website')&&value.query===undefined&&value.provider===undefined
}
