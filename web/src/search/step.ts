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
